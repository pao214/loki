package main

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

const (
	promShutdownTimeout = 10 * time.Second
)

type PromConfig struct {
	Host *string `toml:"host,omitempty"`
}

func GetDefaultPromConfig() *PromConfig {
	promHost := ":2112"
	return &PromConfig{
		Host: &promHost,
	}
}

// Export the prometheus end point on the configured cfg.Host
// Publishes an error on the error channel if the server crashed with an error
// Also returns a stopping routine to be used to shutdown the server
//   the server is explicitly shutdown in the scenarios where there are issues with other modules
func RunPromMetrics(cfg *PromConfig, logger *zap.Logger) (chan error, func()) {
	errorCh := make(chan error)

	http.Handle("/metrics", promhttp.Handler())
	server := &http.Server{Addr: *cfg.Host}

	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), promShutdownTimeout)
		defer cancel()
		err := server.Shutdown(ctx)
		if err != nil {
			logger.Debug("Failed to shutdown prometheus server", zap.Error(err))
		}
	}

	go func() {
		logger.Debug("Exporting prometheus metrics", zap.String("addr", *cfg.Host))
		err := server.ListenAndServe()
		if err != nil {
			errorCh <- err
		}
	}()

	return errorCh, stop
}
