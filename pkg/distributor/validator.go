package distributor

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/pao214/loki/pkg/logproto"
	"github.com/pao214/loki/pkg/validation"
)

const (
	timeFormat = time.RFC3339
)

type Validator struct {
	Limits
}

func NewValidator(l Limits) (*Validator, error) {
	if l == nil {
		return nil, errors.New("nil Limits")
	}
	return &Validator{l}, nil
}

type validationContext struct {
	rejectOldSample       bool
	rejectOldSampleMaxAge int64
	creationGracePeriod   int64

	maxLineSize         int
	maxLineSizeTruncate bool

	maxLabelNamesPerSeries int
	maxLabelNameLength     int
	maxLabelValueLength    int

	userID string
}

func (v Validator) getValidationContextForTime(now time.Time, userID string) validationContext {
	return validationContext{
		userID:                 userID,
		rejectOldSample:        v.RejectOldSamples(userID),
		rejectOldSampleMaxAge:  now.Add(-v.RejectOldSamplesMaxAge(userID)).UnixNano(),
		creationGracePeriod:    now.Add(v.CreationGracePeriod(userID)).UnixNano(),
		maxLineSize:            v.MaxLineSize(userID),
		maxLineSizeTruncate:    v.MaxLineSizeTruncate(userID),
		maxLabelNamesPerSeries: v.MaxLabelNamesPerSeries(userID),
		maxLabelNameLength:     v.MaxLabelNameLength(userID),
		maxLabelValueLength:    v.MaxLabelValueLength(userID),
	}
}

// ValidateEntry returns an error if the entry is invalid
func (v Validator) ValidateEntry(ctx validationContext, labels string, entry logproto.Entry) error {
	ts := entry.Timestamp.UnixNano()

	// Makes time string on the error message formatted consistently.
	formatedEntryTime := entry.Timestamp.Format(timeFormat)
	formatedRejectMaxAgeTime := time.Unix(0, ctx.rejectOldSampleMaxAge).Format(timeFormat)

	if ctx.rejectOldSample && ts < ctx.rejectOldSampleMaxAge {
		validation.DiscardedSamples.WithLabelValues(validation.GreaterThanMaxSampleAge, ctx.userID).Inc()
		validation.DiscardedBytes.WithLabelValues(validation.GreaterThanMaxSampleAge, ctx.userID).Add(float64(len(entry.Line)))
		return httpgrpc.Errorf(http.StatusBadRequest, validation.GreaterThanMaxSampleAgeErrorMsg, labels, formatedEntryTime, formatedRejectMaxAgeTime)
	}

	if ts > ctx.creationGracePeriod {
		validation.DiscardedSamples.WithLabelValues(validation.TooFarInFuture, ctx.userID).Inc()
		validation.DiscardedBytes.WithLabelValues(validation.TooFarInFuture, ctx.userID).Add(float64(len(entry.Line)))
		return httpgrpc.Errorf(http.StatusBadRequest, validation.TooFarInFutureErrorMsg, labels, formatedEntryTime)
	}

	if maxSize := ctx.maxLineSize; maxSize != 0 && len(entry.Line) > maxSize {
		// I wish we didn't return httpgrpc errors here as it seems
		// an orthogonal concept (we need not use ValidateLabels in this context)
		// but the upstream cortex_validation pkg uses it, so we keep this
		// for parity.
		validation.DiscardedSamples.WithLabelValues(validation.LineTooLong, ctx.userID).Inc()
		validation.DiscardedBytes.WithLabelValues(validation.LineTooLong, ctx.userID).Add(float64(len(entry.Line)))
		return httpgrpc.Errorf(http.StatusBadRequest, validation.LineTooLongErrorMsg, maxSize, labels, len(entry.Line))
	}

	return nil
}

// Validate labels returns an error if the labels are invalid
func (v Validator) ValidateLabels(ctx validationContext, ls labels.Labels, stream logproto.Stream) error {
	if len(ls) == 0 {
		validation.DiscardedSamples.WithLabelValues(validation.MissingLabels, ctx.userID).Inc()
		return httpgrpc.Errorf(http.StatusBadRequest, validation.MissingLabelsErrorMsg)
	}
	numLabelNames := len(ls)
	if numLabelNames > ctx.maxLabelNamesPerSeries {
		validation.DiscardedSamples.WithLabelValues(validation.MaxLabelNamesPerSeries, ctx.userID).Inc()
		bytes := 0
		for _, e := range stream.Entries {
			bytes += len(e.Line)
		}
		validation.DiscardedBytes.WithLabelValues(validation.MaxLabelNamesPerSeries, ctx.userID).Add(float64(bytes))
		return httpgrpc.Errorf(http.StatusBadRequest, validation.MaxLabelNamesPerSeriesErrorMsg, stream.Labels, numLabelNames, ctx.maxLabelNamesPerSeries)
	}

	lastLabelName := ""
	for _, l := range ls {
		if len(l.Name) > ctx.maxLabelNameLength {
			updateMetrics(validation.LabelNameTooLong, ctx.userID, stream)
			return httpgrpc.Errorf(http.StatusBadRequest, validation.LabelNameTooLongErrorMsg, stream.Labels, l.Name)
		} else if len(l.Value) > ctx.maxLabelValueLength {
			updateMetrics(validation.LabelValueTooLong, ctx.userID, stream)
			return httpgrpc.Errorf(http.StatusBadRequest, validation.LabelValueTooLongErrorMsg, stream.Labels, l.Value)
		} else if cmp := strings.Compare(lastLabelName, l.Name); cmp == 0 {
			updateMetrics(validation.DuplicateLabelNames, ctx.userID, stream)
			return httpgrpc.Errorf(http.StatusBadRequest, validation.DuplicateLabelNamesErrorMsg, stream.Labels, l.Name)
		}
		lastLabelName = l.Name
	}
	return nil
}

func updateMetrics(reason, userID string, stream logproto.Stream) {
	validation.DiscardedSamples.WithLabelValues(reason, userID).Inc()
	bytes := 0
	for _, e := range stream.Entries {
		bytes += len(e.Line)
	}
	validation.DiscardedBytes.WithLabelValues(reason, userID).Add(float64(bytes))
}
