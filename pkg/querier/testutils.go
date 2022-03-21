package querier

import (
	"github.com/grafana/dskit/flagext"

	"github.com/pao214/loki/v3/pkg/util/validation"
)

func DefaultLimitsConfig() validation.Limits {
	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	return limits
}
