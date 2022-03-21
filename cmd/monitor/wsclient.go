package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap"
)

const (
	newHeadsChSize   = 100
	getAuthorTimeout = 10 * time.Second
	getBlockTimeout  = 10 * time.Second
)

type NodeConfig struct {
	// Address of the local polygon node to connect to
	Host *string `toml:"host"`
}

func GetDefaultNodeConfig() *NodeConfig {
	return &NodeConfig{
		Host: nil,
	}
}

// Connects to the local polygon node client to subscribe for the latest polygon blocks
// Returns
// - a channel to get notified of the author of the latest block
// - a channel to get notified of the constituent transactions
// - a channel to get notified of any subscription errors
// - a stop function to stop the goroutine (in the event of external errors)
// - an error in launching the service itself
func RunWebsocketClient(cfg *NodeConfig, logger *zap.Logger) (
	chan string,
	chan *types.Block,
	chan error,
	func(),
	error,
) {
	if cfg.Host != nil {
		return nil, nil, nil, nil, errors.New("Please configure node.host!")
	}

	// Connect to the specified polygon node
	clientAddr := fmt.Sprintf("ws://%v", *cfg.Host)
	client, clientErr := rpc.DialContext(context.Background(), clientAddr)
	if clientErr != nil {
		return nil, nil, nil, nil, clientErr
	}
	ethClient := ethclient.NewClient(client)
	logger.Debug("Connected to polygon node", zap.String("clientAddr", clientAddr))

	// Subscribe for new heads
	newHeadsCh := make(chan *types.Header, newHeadsChSize)
	newHeadsSub, subErr := ethClient.SubscribeNewHead(context.Background(), newHeadsCh)
	if subErr != nil {
		return nil, nil, nil, nil, subErr
	}

	stopCh := make(chan struct{})
	authorCh := make(chan string)
	blockCh := make(chan *types.Block)
	errorCh := make(chan error)

	stop := func() {
		stopCh <- struct{}{}
	}

	go func() {
		defer newHeadsSub.Unsubscribe()

		for {
			select {
			case header := <-newHeadsCh:
				// Retrieve the author
				number := header.Number.Int64()
				author, authorErr := getAuthor(client, number)
				if authorErr != nil {
					// log and ignore
					logger.Error(
						"Couldn't retrieve author of the block",
						zap.Error(authorErr),
						zap.Int64("number", number),
					)
					continue
				}

				// Publish the author to check if it exists in the whitelist
				authorCh <- author

				// Retrieve the new block
				hash := header.Hash()
				block, blockErr := getBlock(ethClient, hash)
				if blockErr != nil {
					// log and ignore
					logger.Error(
						"Couldn't retrieve block",
						zap.Error(blockErr),
						zap.String("hash", hash.String()),
					)
					continue
				}

				// Publish the block to check bundle inclusions
				blockCh <- block
			case headsSubErr := <-newHeadsSub.Err():
				errorCh <- headsSubErr
				return
			case <-stopCh:
				return
			}
		}
	}()

	return authorCh, blockCh, errorCh, stop, nil
}

// Retrieve the author of the block from the local polygon node
func getAuthor(client *rpc.Client, number int64) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), getAuthorTimeout)
	defer cancel()

	var author common.Address
	if err := client.CallContext(ctx, &author, "bor_getAuthor", rpc.BlockNumber(number)); err != nil {
		return "", err
	}
	return author.String(), nil
}

// Retrieve the constituent txns of the block from the local polygon node
func getBlock(client *ethclient.Client, hash common.Hash) (*types.Block, error) {
	ctx, cancel := context.WithTimeout(context.Background(), getBlockTimeout)
	defer cancel()
	return client.BlockByHash(ctx, hash)
}
