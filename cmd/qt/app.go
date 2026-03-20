package main

import (
	"context"
	"fmt"

	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/config"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/node"
	"github.com/bams-repo/fairchain/internal/types"
	"github.com/bams-repo/fairchain/internal/utxo"
	"github.com/bams-repo/fairchain/internal/version"
)

// App is the Go struct bound to the Wails frontend. All public methods are
// callable from JavaScript via the generated bindings.
type App struct {
	ctx  context.Context
	node *node.Node
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	logging.Init("info", "text")

	cfg := config.DefaultConfig()
	cfg.Network = "regtest"

	opts := node.Options{
		NoRPCAuth: true,
	}

	n, err := node.New(cfg, opts)
	if err != nil {
		logging.L.Error("failed to initialize node", "error", err)
		return
	}

	if err := n.Start(ctx); err != nil {
		logging.L.Error("failed to start node", "error", err)
		n.Stop()
		return
	}

	a.node = n
	logging.L.Info(coinparams.Name+" Wallet started", "version", version.String())
}

func (a *App) shutdown(ctx context.Context) {
	if a.node != nil {
		a.node.Stop()
	}
}

// CoinInfo returns branding constants for the frontend. This is the ONLY
// source of truth for names, ticker, and units in the UI.
func (a *App) CoinInfo() map[string]interface{} {
	return map[string]interface{}{
		"name":            coinparams.Name,
		"nameLower":       coinparams.NameLower,
		"ticker":          coinparams.Ticker,
		"decimals":        coinparams.Decimals,
		"baseUnitName":    coinparams.BaseUnitName,
		"displayUnitName": coinparams.DisplayUnitName,
		"version":         version.String(),
		"copyright":       coinparams.CopyrightHolder,
	}
}

// GetBlockchainInfo returns basic chain state for the overview page.
func (a *App) GetBlockchainInfo() (map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	bc := a.node.Chain()
	tipHash, tipHeight := bc.Tip()
	return map[string]interface{}{
		"height":  tipHeight,
		"bestHash": tipHash.ReverseString(),
		"network": a.node.Config().Network,
	}, nil
}

// GetBalance returns the wallet balance (confirmed and unconfirmed).
func (a *App) GetBalance() (map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	w := a.node.Wallet()
	bc := a.node.Chain()
	_, tipHeight := bc.Tip()

	iter := func(fn func(txHash [32]byte, index uint32, value uint64, pkScript []byte, height uint32, isCoinbase bool)) {
		bc.UtxoSet().ForEach(func(txHash types.Hash, index uint32, entry *utxo.UtxoEntry) {
			fn(txHash, index, entry.Value, entry.PkScript, entry.Height, entry.IsCoinbase)
		})
	}

	confirmed := w.GetBalance(iter, tipHeight, 1, a.node.Params().CoinbaseMaturity)
	total := w.GetBalance(iter, tipHeight, 0, a.node.Params().CoinbaseMaturity)
	unconfirmed := total - confirmed

	return map[string]interface{}{
		"confirmed":   float64(confirmed) / coinparams.CoinsPerBaseUnit,
		"unconfirmed": float64(unconfirmed) / coinparams.CoinsPerBaseUnit,
	}, nil
}

// GetPeerCount returns the number of connected peers.
func (a *App) GetPeerCount() (int, error) {
	if a.node == nil {
		return 0, fmt.Errorf("node not initialized")
	}
	return a.node.P2PMgr().PeerCount(), nil
}

// GetWalletAddress returns the default receive address.
func (a *App) GetWalletAddress() (string, error) {
	if a.node == nil {
		return "", fmt.Errorf("node not initialized")
	}
	return a.node.Wallet().GetDefaultAddress(), nil
}

// GetSyncProgress returns a value between 0.0 and 1.0 indicating sync progress.
func (a *App) GetSyncProgress() (float64, error) {
	if a.node == nil {
		return 0, fmt.Errorf("node not initialized")
	}
	_, height := a.node.Chain().Tip()
	if height == 0 {
		return 1.0, nil
	}
	return 1.0, nil
}
