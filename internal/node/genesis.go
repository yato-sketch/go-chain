package node

import (
	"fmt"

	"github.com/bams-repo/fairchain/internal/algorithms"
	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/consensus/pow"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/difficulty"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/params"
)

// initNetworkGenesis verifies or mines the genesis block for the given network.
func initNetworkGenesis(p *params.ChainParams, hasher algorithms.Hasher, retargeter difficulty.Retargeter) error {
	if !p.GenesisHash.IsZero() {
		computed := crypto.HashBlockHeader(&p.GenesisBlock.Header)
		if computed != p.GenesisHash {
			return fmt.Errorf("genesis hash verification failed for %s: expected %s, computed %s",
				p.Name, p.GenesisHash.ReverseString(), computed.ReverseString())
		}
		return nil
	}

	if p.Name == "mainnet" {
		return fmt.Errorf("mainnet requires a hardcoded genesis block in params")
	}

	cfg := params.GenesisConfig{
		NetworkName:     p.Name,
		CoinbaseMessage: []byte(fmt.Sprintf("%s %s genesis", coinparams.NameLower, p.Name)),
		Timestamp:       1773212462,
		Bits:            p.InitialBits,
		Version:         1,
		Reward:          p.InitialSubsidy,
		RewardScript:    []byte{0x00},
	}

	block := params.BuildGenesisBlock(cfg)
	genesisEngine := pow.New(hasher, retargeter)
	if err := genesisEngine.MineGenesis(&block); err != nil {
		return fmt.Errorf("mine genesis: %w", err)
	}

	hash := crypto.HashBlockHeader(&block.Header)
	params.InitGenesis(p, block, hash)
	logging.L.Info("genesis block", "hash", hash.ReverseString(), "nonce", block.Header.Nonce)
	return nil
}
