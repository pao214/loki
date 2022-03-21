package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

const (
	pollPeriod = 10 * time.Second
)

var (
	// Using gauge instead of counter since we do not explicitly increment the "height"
	latestBlock = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "polygon_blocknum",
		Help: "Latest polygon blocknum polled periodically every 10s",
	})
)

type AlchemyConfig struct {
	Host    *string `toml:"host,omitempty"`
	Version *string `toml:"version,omitempty"`
	ApiKey  *string `toml:"apikey"`
}

func GetDefaultAlchemyConfig() *AlchemyConfig {
	alchemyURL := "polygon-mainnet.g.alchemy.com"
	alchemyVersion := "v2"

	return &AlchemyConfig{
		Host:    &alchemyURL,
		Version: &alchemyVersion,
		ApiKey:  nil,
	}
}

// Jsonrpc is always 2.0
// Method is always eth_blockNumber
type Request struct {
	Jsonrpc string `json:"jsonrpc"`
	Method  string `json:"method"`
}

type Response struct {
	Jsonrpc interface{} `json:"jsonrpc"`
	Id      interface{} `json:id`
	Result  hexutil.Big `json:"result"`
}

// Publish the latest block number periodically
// Returns
// - error during setup
// - cancel function to stop the goroutine
func RunBlocknumPublisher(cfg *AlchemyConfig, logger *zap.Logger) (func(), error) {
	parsedURL, parseErr := getURL(cfg)
	if parseErr != nil {
		return nil, parseErr
	}

	reqBytes, reqErr := newRequest()
	if reqErr != nil {
		return nil, reqErr
	}

	stopCh := make(chan struct{})
	stop := func() {
		stopCh <- struct{}{}
	}

	go func() {
		for {
			// publish block number every 10 seconds
			publishErr := PublishBlocknum(parsedURL, reqBytes, logger)
			if publishErr != nil {
				// log error and continue
				logger.Debug("Failed to publish block", zap.Error(publishErr))
			}

			// Wait until
			// - either 10 seconds have passed
			// - or a stop signal was sent
			select {
			case <-time.After(pollPeriod):
				// continue
			case <-stopCh:
				// break out of the loop
				return
			}
		}
	}()

	return stop, nil
}

func getURL(cfg *AlchemyConfig) (string, error) {
	if cfg.ApiKey == nil {
		return "", errors.New("Must configure alchemy.apikey!")
	}

	// Append apiKey to URL for authentication
	authURL := fmt.Sprintf("https://%s/%s/%s", *cfg.Host, *cfg.Version, *cfg.ApiKey)
	parsedURL, parseErr := url.Parse(authURL)
	if parseErr != nil {
		return "", parseErr
	}

	return parsedURL.String(), nil
}

// Request to retrieve the latest block number from alchemy
func newRequest() ([]byte, error) {
	// Construct json request to retrieve latest block number
	jsonReq := Request{
		Jsonrpc: "2.0",
		Method:  "eth_blockNumber",
	}
	return json.Marshal(jsonReq)
}

// Update prometheus metric using the result from alchemy
func PublishBlocknum(parsedURL string, reqBytes []byte, logger *zap.Logger) error {
	// Post http request
	resp, respErr := http.Post(parsedURL, "application/json", bytes.NewReader(reqBytes))
	if respErr != nil {
		return respErr
	}
	defer resp.Body.Close()

	// Parse json response
	var jsonResp Response
	decErr := json.NewDecoder(io.LimitReader(resp.Body, 1000000)).Decode(&jsonResp)
	if decErr != nil {
		return decErr
	}

	// Update the latest block number prometheus metric
	blocknum := jsonResp.Result.ToInt().Uint64()
	logger.Debug("Prometheus polygon_blocknum metric update", zap.Uint64("blocknum", blocknum))
	latestBlock.Set(float64(blocknum))

	// No errors
	return nil
}
