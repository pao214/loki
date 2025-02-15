package stdin

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/targetgroup"

	"github.com/pao214/loki/clients/pkg/logentry/stages"
	"github.com/pao214/loki/clients/pkg/promtail/api"
	"github.com/pao214/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/pao214/loki/clients/pkg/promtail/targets/target"

	"github.com/pao214/loki/pkg/logproto"
)

// bufferSize is the size of the buffered reader
const bufferSize = 8096

// file is an interface allowing us to abstract a file.
type file interface {
	Stat() (os.FileInfo, error)
	io.Reader
}

var (
	// stdIn is os.Stdin but can be replaced for testing purpose.
	stdIn       file = os.Stdin
	hostName, _      = os.Hostname()
	// defaultStdInCfg is the default config for stdin target if none provided.
	defaultStdInCfg = scrapeconfig.Config{
		JobName: "stdin",
		ServiceDiscoveryConfig: scrapeconfig.ServiceDiscoveryConfig{
			StaticConfigs: []*targetgroup.Group{
				{Labels: model.LabelSet{"job": "stdin"}},
				{Labels: model.LabelSet{"hostname": model.LabelValue(hostName)}},
			},
		},
	}
)

type Shutdownable interface {
	Shutdown()
}

// nolint:revive
type StdinTargetManager struct {
	*readerTarget
	app Shutdownable
}

func NewStdinTargetManager(reg prometheus.Registerer, log log.Logger, app Shutdownable, client api.EntryHandler, configs []scrapeconfig.Config) (*StdinTargetManager, error) {
	reader, err := newReaderTarget(reg, log, stdIn, client, getStdinConfig(log, configs))
	if err != nil {
		return nil, err
	}
	stdinManager := &StdinTargetManager{
		readerTarget: reader,
		app:          app,
	}
	// when we're done flushing our stdin we can shutdown the app.
	go func() {
		<-reader.ctx.Done()
		app.Shutdown()
	}()
	return stdinManager, nil
}

func getStdinConfig(log log.Logger, configs []scrapeconfig.Config) scrapeconfig.Config {
	cfg := defaultStdInCfg
	// if we receive configs we use the first one.
	if len(configs) > 0 {
		if len(configs) > 1 {
			level.Warn(log).Log("msg", fmt.Sprintf("too many scrape configs, skipping %d configs.", len(configs)-1))
		}
		cfg = configs[0]
	}
	return cfg
}

func (t *StdinTargetManager) Ready() bool {
	return t.ctx.Err() == nil
}
func (t *StdinTargetManager) Stop()                                     { t.cancel() }
func (t *StdinTargetManager) ActiveTargets() map[string][]target.Target { return nil }
func (t *StdinTargetManager) AllTargets() map[string][]target.Target    { return nil }

type readerTarget struct {
	in     *bufio.Reader
	out    api.EntryHandler
	lbs    model.LabelSet
	logger log.Logger

	cancel context.CancelFunc
	ctx    context.Context
}

func newReaderTarget(reg prometheus.Registerer, logger log.Logger, in io.Reader, client api.EntryHandler, cfg scrapeconfig.Config) (*readerTarget, error) {
	pipeline, err := stages.NewPipeline(log.With(logger, "component", "pipeline"), cfg.PipelineStages, &cfg.JobName, reg)
	if err != nil {
		return nil, err
	}
	lbs := model.LabelSet{}
	for _, static := range cfg.ServiceDiscoveryConfig.StaticConfigs {
		if static != nil && static.Labels != nil {
			lbs = lbs.Merge(static.Labels)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t := &readerTarget{
		in:     bufio.NewReaderSize(in, bufferSize),
		out:    pipeline.Wrap(client),
		cancel: cancel,
		ctx:    ctx,
		lbs:    lbs,
		logger: log.With(logger, "component", "reader"),
	}
	go t.read()

	return t, nil
}

func (t *readerTarget) read() {
	defer t.cancel()
	defer t.out.Stop()

	entries := t.out.Chan()
	for {
		if t.ctx.Err() != nil {
			return
		}
		line, err := t.in.ReadString('\n')
		if err != nil && err != io.EOF {
			level.Warn(t.logger).Log("msg", "error reading buffer", "err", err)
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if err == io.EOF {
				return
			}
			continue
		}
		entries <- api.Entry{
			Labels: t.lbs.Clone(),
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      line,
			},
		}
		if err == io.EOF {
			return
		}
	}
}
