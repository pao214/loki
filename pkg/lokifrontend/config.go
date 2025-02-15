package lokifrontend

import (
	"flag"

	"github.com/pao214/loki/pkg/lokifrontend/frontend/transport"
	v1 "github.com/pao214/loki/pkg/lokifrontend/frontend/v1"
	v2 "github.com/pao214/loki/pkg/lokifrontend/frontend/v2"
)

type Config struct {
	Handler    transport.HandlerConfig `yaml:",inline"`
	FrontendV1 v1.Config               `yaml:",inline"`
	FrontendV2 v2.Config               `yaml:",inline"`

	CompressResponses bool   `yaml:"compress_responses"`
	DownstreamURL     string `yaml:"downstream_url"`

	TailProxyURL string `yaml:"tail_proxy_url"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Handler.RegisterFlags(f)
	cfg.FrontendV1.RegisterFlags(f)
	cfg.FrontendV2.RegisterFlags(f)

	f.BoolVar(&cfg.CompressResponses, "querier.compress-http-responses", false, "Compress HTTP responses.")
	f.StringVar(&cfg.DownstreamURL, "frontend.downstream-url", "", "URL of downstream Prometheus.")

	f.StringVar(&cfg.TailProxyURL, "frontend.tail-proxy-url", "", "URL of querier for tail proxy.")
}
