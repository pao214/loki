package queryrange

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"

	jsoniter "github.com/json-iterator/go"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/common/model"

	"github.com/pao214/loki/pkg/loghttp"
	"github.com/pao214/loki/pkg/logqlmodel/stats"
	"github.com/pao214/loki/pkg/querier/queryrange/queryrangebase"
)

var (
	jsonStd   = jsoniter.ConfigCompatibleWithStandardLibrary
	extractor = queryrangebase.PrometheusResponseExtractor{}
)

// PrometheusExtractor implements Extractor interface
type PrometheusExtractor struct{}

// Extract wraps the original prometheus cache extractor
func (PrometheusExtractor) Extract(start, end int64, from queryrangebase.Response) queryrangebase.Response {
	response := extractor.Extract(start, end, from.(*LokiPromResponse).Response)
	return &LokiPromResponse{
		Response: response.(*queryrangebase.PrometheusResponse),
	}
}

// ResponseWithoutHeaders wraps the original prometheus caching without headers
func (PrometheusExtractor) ResponseWithoutHeaders(resp queryrangebase.Response) queryrangebase.Response {
	response := extractor.ResponseWithoutHeaders(resp.(*LokiPromResponse).Response)
	return &LokiPromResponse{
		Response: response.(*queryrangebase.PrometheusResponse),
	}
}

// encode encodes a Prometheus response and injects Loki stats.
func (p *LokiPromResponse) encode(ctx context.Context) (*http.Response, error) {
	sp := opentracing.SpanFromContext(ctx)
	var (
		b   []byte
		err error
	)
	if p.Response.Data.ResultType == loghttp.ResultTypeVector {
		b, err = p.marshalVector()
	} else {
		b, err = p.marshalMatrix()
	}
	if err != nil {
		return nil, err
	}

	if sp != nil {
		sp.LogFields(otlog.Int("bytes", len(b)))
	}

	resp := http.Response{
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:       ioutil.NopCloser(bytes.NewBuffer(b)),
		StatusCode: http.StatusOK,
	}
	return &resp, nil
}

func (p *LokiPromResponse) marshalVector() ([]byte, error) {
	vec := make(loghttp.Vector, len(p.Response.Data.Result))
	for i, v := range p.Response.Data.Result {
		lbs := make(model.LabelSet, len(v.Labels))
		for _, v := range v.Labels {
			lbs[model.LabelName(v.Name)] = model.LabelValue(v.Value)
		}
		vec[i] = model.Sample{
			Metric:    model.Metric(lbs),
			Timestamp: model.Time(v.Samples[0].TimestampMs),
			Value:     model.SampleValue(v.Samples[0].Value),
		}
	}
	return jsonStd.Marshal(struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string         `json:"resultType"`
			Result     loghttp.Vector `json:"result"`
			Statistics stats.Result   `json:"stats,omitempty"`
		} `json:"data,omitempty"`
		ErrorType string `json:"errorType,omitempty"`
		Error     string `json:"error,omitempty"`
	}{
		Error: p.Response.Error,
		Data: struct {
			ResultType string         `json:"resultType"`
			Result     loghttp.Vector `json:"result"`
			Statistics stats.Result   `json:"stats,omitempty"`
		}{
			ResultType: loghttp.ResultTypeVector,
			Result:     vec,
			Statistics: p.Statistics,
		},
		ErrorType: p.Response.ErrorType,
		Status:    p.Response.Status,
	})
}

func (p *LokiPromResponse) marshalMatrix() ([]byte, error) {
	// embed response and add statistics.
	return jsonStd.Marshal(struct {
		Status string `json:"status"`
		Data   struct {
			queryrangebase.PrometheusData
			Statistics stats.Result `json:"stats,omitempty"`
		} `json:"data,omitempty"`
		ErrorType string `json:"errorType,omitempty"`
		Error     string `json:"error,omitempty"`
	}{
		Error: p.Response.Error,
		Data: struct {
			queryrangebase.PrometheusData
			Statistics stats.Result `json:"stats,omitempty"`
		}{
			PrometheusData: p.Response.Data,
			Statistics:     p.Statistics,
		},
		ErrorType: p.Response.ErrorType,
		Status:    p.Response.Status,
	})
}
