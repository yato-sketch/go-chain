package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fairchain/fairchain/internal/chain"
	"github.com/fairchain/fairchain/internal/config"
	"github.com/fairchain/fairchain/internal/consensus/pow"
	"github.com/fairchain/fairchain/internal/crypto"
	"github.com/fairchain/fairchain/internal/logging"
	"github.com/fairchain/fairchain/internal/mempool"
	"github.com/fairchain/fairchain/internal/metrics"
	"github.com/fairchain/fairchain/internal/miner"
	"github.com/fairchain/fairchain/internal/p2p"
	fcparams "github.com/fairchain/fairchain/internal/params"
	"github.com/fairchain/fairchain/internal/rpc"
	"github.com/fairchain/fairchain/internal/store"
	"github.com/fairchain/fairchain/internal/types"
)

func main() {
	configPath := flag.String("config", "", "Path to config file")
	network := flag.String("network", "", "Override network (mainnet/testnet/regtest)")
	dataDir := flag.String("datadir", "", "Override data directory")
	listen := flag.String("listen", "", "Override P2P listen address")
	rpcAddr := flag.String("rpc", "", "Override RPC listen address")
	mine := flag.Bool("mine", false, "Enable mining")
	seedPeers := flag.String("seed-peers", "", "Comma-separated seed peer addresses (ip:port,ip:port)")
	logLevel := flag.String("log-level", "info", "Log level: debug, info, warn, error")
	flag.Parse()

	logging.Init(*logLevel)
	log := logging.L

	// Load config.
	var cfg *config.Config
	var err error
	if *configPath != "" {
		cfg, err = config.LoadConfig(*configPath)
		if err != nil {
			log.Error("failed to load config", "error", err)
			os.Exit(1)
		}
	} else {
		cfg = config.DefaultConfig()
	}

	// Apply CLI overrides.
	if *network != "" {
		cfg.Network = *network
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	if *listen != "" {
		cfg.ListenAddr = *listen
	}
	if *rpcAddr != "" {
		cfg.RPCAddr = *rpcAddr
	}
	if *mine {
		cfg.MiningEnabled = true
	}
	if *seedPeers != "" {
		cfg.SeedPeers = strings.Split(*seedPeers, ",")
	}

	// Resolve chain params.
	params := fcparams.NetworkByName(cfg.Network)
	if params == nil {
		log.Error("unknown network", "network", cfg.Network)
		os.Exit(1)
	}

	// Mine and set genesis for the network.
	initNetworkGenesis(params)

	// Ensure data directory exists.
	if err := cfg.EnsureDataDir(); err != nil {
		log.Error("failed to create data dir", "error", err)
		os.Exit(1)
	}

	log.Info("starting fairchain node", "network", cfg.Network, "datadir", cfg.DataDir)

	// Open block store.
	blockStore, err := store.NewBoltStore(cfg.DBPath())
	if err != nil {
		log.Error("failed to open block store", "error", err)
		os.Exit(1)
	}

	// Open peer store.
	peerStore, err := store.NewBoltStore(cfg.PeerDBPath())
	if err != nil {
		blockStore.Close()
		log.Error("failed to open peer store", "error", err)
		os.Exit(1)
	}

	// Create consensus engine.
	engine := pow.New()

	// Create blockchain.
	bc := chain.New(params, engine, blockStore)
	if err := bc.Init(); err != nil {
		peerStore.Close()
		blockStore.Close()
		log.Error("failed to initialize chain", "error", err)
		os.Exit(1)
	}

	tipHash, tipHeight := bc.Tip()
	log.Info("chain initialized", "tip", tipHash.ReverseString(), "height", tipHeight)

	// Create mempool.
	mp := mempool.New(params)

	// Context for graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())

	// Start P2P manager.
	p2pMgr := p2p.NewManager(params, bc, mp, peerStore, cfg.ListenAddr, cfg.MaxInbound, cfg.MaxOutbound, cfg.SeedPeers)
	if err := p2pMgr.Start(ctx); err != nil {
		cancel()
		peerStore.Close()
		blockStore.Close()
		log.Error("failed to start P2P", "error", err)
		os.Exit(1)
	}

	// Start RPC server (with metrics endpoint).
	rpcServer := rpc.New(cfg.RPCAddr, bc, mp, p2pMgr, params)
	if err := rpcServer.Start(); err != nil {
		cancel()
		p2pMgr.Stop()
		peerStore.Close()
		blockStore.Close()
		log.Error("failed to start RPC", "error", err)
		os.Exit(1)
	}

	// Start miner if enabled.
	if cfg.MiningEnabled {
		rewardScript := []byte(cfg.MiningAddr)
		if len(rewardScript) == 0 {
			rewardScript = []byte{0x00}
		}
		m := miner.New(bc, engine, mp, params, rewardScript, func(block *types.Block) {
			height, err := bc.ProcessBlock(block)
			if err != nil {
				log.Warn("mined block rejected", "error", err)
				return
			}
			blockHash := crypto.HashBlockHeader(&block.Header)
			metrics.Global.BlocksMined.Add(1)
			log.Info("mined block accepted", "hash", blockHash.ReverseString(), "height", height)
			p2pMgr.BroadcastBlock(blockHash, block)
		})
		go m.Run(ctx)
	}

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Info("received shutdown signal", "signal", sig)

	// Ordered shutdown: cancel context first (stops miner, P2P loops),
	// then tear down services in reverse startup order.
	cancel()

	log.Info("stopping RPC server...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := rpcServer.Stop(shutdownCtx); err != nil {
		log.Warn("RPC shutdown error", "error", err)
	}

	log.Info("stopping P2P manager...")
	p2pMgr.Stop()

	log.Info("closing peer store...")
	if err := peerStore.Close(); err != nil {
		log.Warn("peer store close error", "error", err)
	}

	log.Info("closing block store...")
	if err := blockStore.Close(); err != nil {
		log.Warn("block store close error", "error", err)
	}

	log.Info("shutdown complete")
}

// initNetworkGenesis mines the genesis block for the given params if not already set.
func initNetworkGenesis(p *fcparams.ChainParams) {
	if !p.GenesisHash.IsZero() {
		return
	}

	cfg := fcparams.GenesisConfig{
		NetworkName:     p.Name,
		CoinbaseMessage: []byte(fmt.Sprintf("fairchain %s genesis", p.Name)),
		Timestamp:       1773212462,
		Bits:            p.InitialBits,
		Version:         1,
		Reward:          p.InitialSubsidy,
		RewardScript:    []byte{0x00},
	}

	block := fcparams.BuildGenesisBlock(cfg)
	if err := pow.MineGenesis(&block); err != nil {
		logging.L.Error("failed to mine genesis", "error", err)
		os.Exit(1)
	}

	hash := crypto.HashBlockHeader(&block.Header)
	fcparams.InitGenesis(p, block, hash)
	logging.L.Info("genesis block", "hash", hash.ReverseString(), "nonce", block.Header.Nonce)
}
