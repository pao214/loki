package util

import (
	"context"
	"sync"

	"github.com/go-kit/log/level"

	"github.com/pao214/loki/pkg/storage/chunk"
	util_math "github.com/pao214/loki/pkg/util/math"
	"github.com/pao214/loki/pkg/util/spanlogger"
)

const maxQueriesPerGoroutine = 100

type TableQuerier interface {
	MultiQueries(ctx context.Context, queries []chunk.IndexQuery, callback chunk.QueryPagesCallback) error
}

// QueriesByTable groups and returns queries by tables.
func QueriesByTable(queries []chunk.IndexQuery) map[string][]chunk.IndexQuery {
	queriesByTable := make(map[string][]chunk.IndexQuery)
	for _, query := range queries {
		if _, ok := queriesByTable[query.TableName]; !ok {
			queriesByTable[query.TableName] = []chunk.IndexQuery{}
		}

		queriesByTable[query.TableName] = append(queriesByTable[query.TableName], query)
	}

	return queriesByTable
}

func DoParallelQueries(ctx context.Context, tableQuerier TableQuerier, queries []chunk.IndexQuery, callback chunk.QueryPagesCallback) error {
	if len(queries) == 0 {
		return nil
	}
	errs := make(chan error)

	id := NewIndexDeduper(callback)
	defer func() {
		logger := spanlogger.FromContext(ctx)
		level.Debug(logger).Log("msg", "done processing index queries", "table-name", queries[0].TableName,
			"query-count", len(queries), "num-entries-sent", id.numEntriesSent)
	}()

	if len(queries) <= maxQueriesPerGoroutine {
		return tableQuerier.MultiQueries(ctx, queries, id.Callback)
	}

	for i := 0; i < len(queries); i += maxQueriesPerGoroutine {
		q := queries[i:util_math.Min(i+maxQueriesPerGoroutine, len(queries))]
		go func(queries []chunk.IndexQuery) {
			errs <- tableQuerier.MultiQueries(ctx, queries, id.Callback)
		}(q)
	}

	var lastErr error
	for i := 0; i < len(queries); i += maxQueriesPerGoroutine {
		err := <-errs
		if err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// IndexDeduper should always be used on table level not the whole query level because it just looks at range values which can be repeated across tables
// Cortex anyways dedupes entries across tables
type IndexDeduper struct {
	callback        chunk.QueryPagesCallback
	seenRangeValues map[string]map[string]struct{}
	numEntriesSent  int
	mtx             sync.RWMutex
}

func NewIndexDeduper(callback chunk.QueryPagesCallback) *IndexDeduper {
	return &IndexDeduper{
		callback:        callback,
		seenRangeValues: map[string]map[string]struct{}{},
	}
}

func (i *IndexDeduper) Callback(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
	return i.callback(query, &filteringBatch{
		query:     query,
		ReadBatch: batch,
		isSeen:    i.isSeen,
	})
}

func (i *IndexDeduper) isSeen(hashValue string, rangeValue []byte) bool {
	i.mtx.RLock()

	// index entries are never modified during query processing so it should be safe to reference a byte slice as a string.
	rangeValueStr := GetUnsafeString(rangeValue)

	if _, ok := i.seenRangeValues[hashValue][rangeValueStr]; ok {
		i.mtx.RUnlock()
		return true
	}

	i.mtx.RUnlock()

	i.mtx.Lock()
	defer i.mtx.Unlock()

	// re-check if another concurrent call added the values already, if so do not add it again and return true
	if _, ok := i.seenRangeValues[hashValue][rangeValueStr]; ok {
		return true
	}

	// add the hashValue first if missing
	if _, ok := i.seenRangeValues[hashValue]; !ok {
		i.seenRangeValues[hashValue] = map[string]struct{}{}
	}

	// add the rangeValue
	i.seenRangeValues[hashValue][rangeValueStr] = struct{}{}
	i.numEntriesSent++
	return false
}

type isSeen func(hashValue string, rangeValue []byte) bool

type filteringBatch struct {
	query chunk.IndexQuery
	chunk.ReadBatch
	isSeen isSeen
}

func (f *filteringBatch) Iterator() chunk.ReadBatchIterator {
	return &filteringBatchIter{
		query:             f.query,
		ReadBatchIterator: f.ReadBatch.Iterator(),
		isSeen:            f.isSeen,
	}
}

type filteringBatchIter struct {
	query chunk.IndexQuery
	chunk.ReadBatchIterator
	isSeen isSeen
}

func (f *filteringBatchIter) Next() bool {
	for f.ReadBatchIterator.Next() {
		if f.isSeen(f.query.HashValue, f.ReadBatchIterator.RangeValue()) {
			continue
		}

		return true
	}

	return false
}
