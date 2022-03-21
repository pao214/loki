package main

import (
	"bufio"
	"errors"
	"os"
	"os/signal"
	"syscall"

	toml "github.com/pelletier/go-toml"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
)

var (
	// Commannd line flags
	configFileFlag = &cli.StringFlag{
		Name:    "config",
		Aliases: []string{"c"},
		Usage:   "Load TOML based configuration from `FILE`",
	}

	// switch to true when debugging
	devMode = true
)

type Config struct {
	// Configures the /metrics endpoint that exposes prometheus metrics
	Prometheus *PromConfig `toml:"prometheus,omitempty"`

	// Configures the websocket client that connects to the locally running polygon node
	Node *NodeConfig `toml:"node,omitempty"`

	// Configures connection to the alchemy node running the polygon client
	Alchemy *AlchemyConfig `toml:"alchemy"`

	// List of known validators to compute hashpower
	Hashpower *HashpowerConfig `toml:"hashpower"`

	// Configures connection to the loki instance
	Loki *LokiConfig `toml:"loki,omitempty"`
}

func GetDefaultConfig() *Config {
	return &Config{
		Prometheus: GetDefaultPromConfig(),
		Node:       GetDefaultNodeConfig(),
		Alchemy:    GetDefaultAlchemyConfig(),
		Hashpower:  GetDefaultHashpowerConfig(),
		Loki:       GetDefaultLokiConfig(),
	}
}

func main() {
	logger := newLogger()
	// Lifecycle of the logger must extend over all the goroutines using this logger
	defer logger.Sync()

	flags := []cli.Flag{
		configFileFlag,
	}
	app := cli.App{
		Name:  "monitor",
		Usage: "Monitors Marlin MEV applications",
		Action: func(ctx *cli.Context) error {
			return monitor(ctx, logger)
		},
		Flags:   flags,
		Version: "v1",
	}
	if err := app.Run(os.Args); err != nil {
		logger.Panic("Application exiting with error ...", zap.Error(err))
	}
}

func monitor(ctx *cli.Context, logger *zap.Logger) error {
	// Load configuration file
	cfg, loadErr := loadConfig(ctx, logger)
	if loadErr != nil {
		return loadErr
	}

	// Export the metrics endpoint for prometheus
	promErrorCh, stopProm := RunPromMetrics(cfg.Prometheus, logger)
	defer stopProm()

	// Run websocket client to retrieve new blocks
	wsAuthorCh, wsBlockCh, wsErrorCh, stopWS, wsErr := RunWebsocketClient(cfg.Node, logger)
	if wsErr != nil {
		return wsErr
	}
	defer stopWS()

	// Periodically publish the latest polygon blockchain height
	// The data is retrieved using the alchemy API
	stopBlocknum, blocknumErr := RunBlocknumPublisher(cfg.Alchemy, logger)
	if blocknumErr != nil {
		return blocknumErr
	}
	defer stopBlocknum()

	// Publish count of mev blocks produced metric
	stopBlockDetector, whitelistErr := RunMevBlockDetector(cfg.Hashpower, wsAuthorCh, logger)
	if whitelistErr != nil {
		return whitelistErr
	}
	defer stopBlockDetector()

	// Check bundle inclusion
	stopBundleDetector, bundleErr := RunBundleDetector(cfg.Loki, wsBlockCh, logger)
	if bundleErr != nil {
		return bundleErr
	}
	defer stopBundleDetector()

	// Handle process interruption
	sigCh := make(chan os.Signal)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case promError := <-promErrorCh:
		return promError
	case wsError := <-wsErrorCh:
		return wsError
	case <-sigCh:
		return nil
	}
}

func newLogger() *zap.Logger {
	var loggerCfg zap.Config
	if devMode {
		loggerCfg = zap.NewDevelopmentConfig()
		// Uncomment below to output logs to a file
		// loggerCfg.OutputPaths = []string{
		// 	"logs/debug.log",
		// }
	} else {
		loggerCfg = zap.NewProductionConfig()
	}
	logger, logErr := loggerCfg.Build()
	if logErr != nil {
		panic(logErr)
	}

	return logger
}

// Extracts configuration required for monitoring
// options specified in the config file take preference over default options
func loadConfig(ctx *cli.Context, logger *zap.Logger) (*Config, error) {
	// Must specify a config file
	if !ctx.IsSet(configFileFlag.Name) {
		return nil, errors.New("Please specify -c <config file>!")
	}
	filepath := ctx.String(configFileFlag.Name)
	logger.Debug("Loading config file", zap.String("filepath", filepath))

	// Extract configuration options from the config file
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Decode toml config
	// 'Strict' disallows specification of extraneous config options
	cfg := GetDefaultConfig()
	if err = toml.NewDecoder(bufio.NewReader(file)).Strict(true).Decode(cfg); err != nil {
		return nil, err
	}

	// success
	return cfg, nil
}
