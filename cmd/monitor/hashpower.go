package main

import (
	"errors"

	"github.com/emirpasic/gods/sets/hashset"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

var (
	mevTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "polygon_mev_total",
		Help: "Latest polygon blocknum polled periodically every 10s",
	})
)

type HashpowerConfig struct {
	Whitelist []string `toml:"whitelist"`
}

func GetDefaultHashpowerConfig() *HashpowerConfig {
	return &HashpowerConfig{
		Whitelist: nil,
	}
}

// Update the block counter every time we encounter a block produced by a validator running mev polygon
func RunMevBlockDetector(cfg *HashpowerConfig, authorCh chan string, logger *zap.Logger) (func(), error) {
	if cfg.Whitelist == nil {
		return nil, errors.New("Please configure hashpower.whitelist")
	}
	whitelist := hashset.New()
	for _, val := range cfg.Whitelist {
		whitelist.Add(val)
	}

	stopCh := make(chan struct{})
	stop := func() {
		stopCh <- struct{}{}
	}

	go func() {
		select {
		case author := <-authorCh:
			if whitelist.Contains(author) {
				mevTotal.Inc()
			}
		case <-stopCh:
			return
		}
	}()

	return stop, nil
}
