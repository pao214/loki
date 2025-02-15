package client

import (
	"net/url"
	"testing"
	"time"

	cortexflag "github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/pao214/loki/clients/pkg/promtail/api"

	"github.com/pao214/loki/pkg/logproto"
	util_log "github.com/pao214/loki/pkg/util/log"
)

func TestNewLogger(t *testing.T) {
	_, err := NewLogger(nilMetrics, nil, util_log.Logger, []Config{}...)
	require.Error(t, err)

	l, err := NewLogger(nilMetrics, nil, util_log.Logger, []Config{{URL: cortexflag.URLValue{URL: &url.URL{Host: "string"}}}}...)
	require.NoError(t, err)
	l.Chan() <- api.Entry{Labels: model.LabelSet{"foo": "bar"}, Entry: logproto.Entry{Timestamp: time.Now(), Line: "entry"}}
	l.Stop()
}
