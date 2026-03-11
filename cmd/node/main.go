package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/fairchain/fairchain/internal/chain"
	"github.com/fairchain/fairchain/internal/config"
	"github.com/fairchain/fairchain/internal/consensus/pow"
	"github.com/fairchain/fairchain/internal/crypto"
	"github.com/fairchain/fairchain/internal/mempool"
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
	flag.Parse()

	// Load config.
	var cfg *config.Config
	var err error
	if *configPath != "" {
		cfg, err = config.LoadConfig(*configPath)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
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
		log.Fatalf("Unknown network: %s", cfg.Network)
	}

	// Mine and set genesis for the network.
	initNetworkGenesis(params)

	// Ensure data directory exists.
	if err := cfg.EnsureDataDir(); err != nil {
		log.Fatalf("Failed to create data dir: %v", err)
	}

	log.Printf("Starting fairchain node (network=%s, datadir=%s)", cfg.Network, cfg.DataDir)

	// Open block store.
	blockStore, err := store.NewBoltStore(cfg.DBPath())
	if err != nil {
		log.Fatalf("Failed to open block store: %v", err)
	}
	defer blockStore.Close()

	// Open peer store.
	peerStore, err := store.NewBoltStore(cfg.PeerDBPath())
	if err != nil {
		log.Fatalf("Failed to open peer store: %v", err)
	}
	defer peerStore.Close()

	// Create consensus engine.
	engine := pow.New()

	// Create blockchain.
	bc := chain.New(params, engine, blockStore)
	if err := bc.Init(); err != nil {
		log.Fatalf("Failed to initialize chain: %v", err)
	}

	tipHash, tipHeight := bc.Tip()
	log.Printf("Chain initialized: tip=%s height=%d", tipHash.ReverseString(), tipHeight)

	// Create mempool.
	mp := mempool.New(params)

	// Context for graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start P2P manager.
	p2pMgr := p2p.NewManager(params, bc, mp, peerStore, cfg.ListenAddr, cfg.MaxInbound, cfg.MaxOutbound, cfg.SeedPeers)
	if err := p2pMgr.Start(ctx); err != nil {
		log.Fatalf("Failed to start P2P: %v", err)
	}
	defer p2pMgr.Stop()

	// Start RPC server.
	rpcServer := rpc.New(cfg.RPCAddr, bc, mp, p2pMgr, params)
	if err := rpcServer.Start(); err != nil {
		log.Fatalf("Failed to start RPC: %v", err)
	}
	defer rpcServer.Stop(ctx)

	// Start miner if enabled.
	if cfg.MiningEnabled {
		rewardScript := []byte(cfg.MiningAddr)
		if len(rewardScript) == 0 {
			rewardScript = []byte{0x00}
		}
		m := miner.New(bc, engine, mp, params, rewardScript, func(block *types.Block) {
			height, err := bc.ProcessBlock(block)
			if err != nil {
				log.Printf("[node] mined block rejected: %v", err)
				return
			}
			blockHash := crypto.HashBlockHeader(&block.Header)
			log.Printf("[node] mined block accepted: %s height=%d", blockHash.ReverseString(), height)
			p2pMgr.BroadcastBlock(blockHash, block)
		})
		go m.Run(ctx)
	}

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("Received signal %v, shutting down...", sig)
	cancel()
}

// initNetworkGenesis mines the genesis block for the given params if not already set.
func initNetworkGenesis(p *fcparams.ChainParams) {
	if !p.GenesisHash.IsZero() {
		return
	}

	cfg := fcparams.GenesisConfig{
		NetworkName:     p.Name,
		CoinbaseMessage: []byte(fmt.Sprintf("fairchain %s genesis", p.Name)),
		Timestamp:       1773212462, // Fixed timestamp for reproducibility: 2026-03-11T07:01:02Z
		Bits:            p.InitialBits,
		Version:         1,
		Reward:          p.InitialSubsidy,
		RewardScript:    []byte{0x00},
	}

	block := fcparams.BuildGenesisBlock(cfg)
	if err := pow.MineGenesis(&block); err != nil {
		log.Fatalf("Failed to mine genesis: %v", err)
	}

	hash := crypto.HashBlockHeader(&block.Header)
	fcparams.InitGenesis(p, block, hash)
	log.Printf("Genesis block: %s (nonce=%d)", hash.ReverseString(), block.Header.Nonce)
}
