package queryrange

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/pao214/loki/v3/pkg/logql"
	"github.com/pao214/loki/v3/pkg/querier/queryrange/queryrangebase"
)

type Metrics struct {
	*queryrangebase.InstrumentMiddlewareMetrics
	*queryrangebase.RetryMiddlewareMetrics
	*logql.ShardingMetrics
	*SplitByMetrics
	*LogResultCacheMetrics
}

func NewMetrics(registerer prometheus.Registerer) *Metrics {
	return &Metrics{
		InstrumentMiddlewareMetrics: queryrangebase.NewInstrumentMiddlewareMetrics(registerer),
		RetryMiddlewareMetrics:      queryrangebase.NewRetryMiddlewareMetrics(registerer),
		ShardingMetrics:             logql.NewShardingMetrics(registerer),
		SplitByMetrics:              NewSplitByMetrics(registerer),
		LogResultCacheMetrics:       NewLogResultCacheMetrics(registerer),
	}
}
