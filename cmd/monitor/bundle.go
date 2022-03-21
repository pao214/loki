package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	strftime "github.com/itchyny/timefmt-go"
	"github.com/pao214/loki/pkg/logcli/client"
	"github.com/pao214/loki/pkg/logcli/output"
	"github.com/pao214/loki/pkg/logcli/query"
	"github.com/prometheus/common/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NOTE: Reverting txns are not handled

const (
	windowPeriod = 5 * time.Minute
)

type LokiConfig struct {
	Host      *string `toml:"host"`
	OutputDir *string `toml:"output_dir"`
	Username  *string `toml:"username"`
	Password  *string `toml:"password"`
}

func GetDefaultLokiConfig() *LokiConfig {
	defaultLokiHost := "localhost:3100"
	return &LokiConfig{
		Host:      &defaultLokiHost,
		OutputDir: nil,
	}
}

type LogEntry struct {
	BundleHash string   `json:"bundle_hash"`
	Txns       []string `json:"txns"`
}

func RunBundleDetector(cfg *LokiConfig, blockCh chan *types.Block, logger *zap.Logger) (func(), error) {
	lokiLogger, logErr := newLokiLogger(cfg)
	if logErr != nil {
		return nil, logErr
	}

	queryClient, clientErr := newQueryClient(cfg)
	if clientErr != nil {
		return nil, clientErr
	}

	stopCh := make(chan struct{})
	stop := func() {
		stopCh <- struct{}{}
	}

	go func() {
		defer lokiLogger.Sync()

		for {
			select {
			case block := <-blockCh:
				LogIncludedBundles(lokiLogger, queryClient, block, logger)
			case <-stopCh:
				return
			}
		}
	}()

	return stop, nil
}

func newLokiLogger(cfg *LokiConfig) (*zap.Logger, error) {
	filename, fileErr := getOutputPath(cfg)
	if fileErr != nil {
		return nil, fileErr
	}

	// Do not include level and message keys in the output
	loggerCfg := zap.NewProductionConfig()
	loggerCfg.OutputPaths = []string{filename}
	loggerCfg.EncoderConfig.MessageKey = zapcore.OmitKey
	loggerCfg.EncoderConfig.LevelKey = zapcore.OmitKey

	return loggerCfg.Build()
}

// log directory format - base_dir/YYMMDD
func getOutputPath(cfg *LokiConfig) (string, error) {
	if cfg.OutputDir != nil {
		return "", errors.New("Please configure loki.output_dir!")
	}
	// today's date for filenames
	todays_date := strftime.Format(time.Now(), "%Y%m%d")
	logDir := fmt.Sprintf("%v/%v", *cfg.OutputDir, todays_date)

	// create the directory if it doesn't exist
	err := os.MkdirAll(logDir, 0775)
	if err != nil {
		return "", err
	}

	outputPath := path.Join(logDir, "bundles.log")
	return outputPath, nil
}

func newQueryClient(cfg *LokiConfig) (client.Client, error) {
	client := &client.DefaultClient{
		TLSConfig: config.TLSConfig{},
	}

	if cfg.Host == nil {
		return nil, errors.New("Please configure loki.host!")
	}

	urlObj, urlErr := url.Parse(*cfg.Host)
	if urlErr != nil {
		return nil, urlErr
	}
	client.TLSConfig.ServerName = urlObj.Host

	if cfg.Username != nil {
		client.Username = *cfg.Username
	}
	if cfg.Password != nil {
		client.Password = *cfg.Password
	}

	return client, nil
}

func LogIncludedBundles(
	lokiLogger *zap.Logger,
	queryClient client.Client,
	block *types.Block,
	logger *zap.Logger,
) {
	// query bundles
	blocknum := block.NumberU64()
	logBytes, logErr := queryBundles(queryClient, blocknum, logger)
	if logErr != nil {
		return
	}
	logReader := bufio.NewReader(bytes.NewReader(logBytes))
	txns := block.Transactions()

	// Compute txn hashes
	txnHashes := []string{}
	for _, txn := range txns {
		txnHashes = append(txnHashes, txn.Hash().String())
	}

	// Read them line-by-line
	for {
		lineBytes, lineErr := logReader.ReadBytes('\n')
		if lineErr != nil && lineErr != io.EOF {
			break
		}
		if lineErr != io.EOF {
			// Remove the trailing '\n'
			lineBytes = lineBytes[:len(lineBytes)-1]
		}
		if len(lineBytes) == 0 {
			break
		}

		// Decode json output
		logEntry := &LogEntry{}
		decErr := json.Unmarshal(lineBytes, logEntry)
		if decErr != nil {
			logger.Debug("Failed to unmarshal loki log entry", zap.Error(decErr))
		}

		if isBundleIncluded(logEntry.Txns, txnHashes) {
			// Output all included bundles
			// message ignored in log
			lokiLogger.Info("",
				zap.Uint64("blocknum", blocknum),
				zap.String("bundle_hash", logEntry.BundleHash),
			)
		}
	}
}

func queryBundles(queryClient client.Client, blocknum uint64, logger *zap.Logger) ([]byte, error) {
	bundleQuery := newQuery(blocknum)

	jsonRespBytes := new(bytes.Buffer)
	outputOptions := &output.LogOutputOptions{
		Timezone:      time.Local,
		NoLabels:      true,
		ColoredOutput: false,
	}
	out, outErr := output.NewLogOutput(jsonRespBytes, "jsonl", outputOptions)
	if outErr != nil {
		logger.Debug("Error creating new json output")
		return nil, outErr
	}

	bundleQuery.DoQuery(queryClient, out, false)
	return jsonRespBytes.Bytes(), nil
}

func newQuery(blocknum uint64) *query.Query {
	// Look for the bundles in the specified window
	end := time.Now()
	start := end.Add(-windowPeriod)

	// Construct the query
	q := &query.Query{}
	q.Limit = 50
	q.QueryString = fmt.Sprintf(`{blocknum=%v}`, blocknum)
	q.Start = start
	q.End = end
	q.Quiet = true

	return q
}

func isBundleIncluded(bundleTxns []string, blockTxns []string) bool {
	numBlockTxns := len(blockTxns)
	numBundleTxns := len(bundleTxns)

	for blockTxnIdx := 0; blockTxnIdx+numBundleTxns <= numBlockTxns; blockTxnIdx++ {
		bundleIncluded := true
		for bundleTxnIdx := 0; bundleTxnIdx < numBundleTxns; bundleTxnIdx++ {
			if bundleTxns[bundleTxnIdx] != blockTxns[blockTxnIdx+bundleTxnIdx] {
				bundleIncluded = false
				break
			}
		}

		if bundleIncluded {
			// Found a bundle included in the txn
			return true
		}
	}

	return false
}
