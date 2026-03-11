package pow

import (
	"fmt"
	"math/big"
	"time"

	"github.com/fairchain/fairchain/internal/consensus"
	"github.com/fairchain/fairchain/internal/crypto"
	"github.com/fairchain/fairchain/internal/params"
	"github.com/fairchain/fairchain/internal/types"
)

// Engine implements the baseline Nakamoto-style proof-of-work consensus.
type Engine struct{}

var _ consensus.Engine = (*Engine)(nil)

func New() *Engine { return &Engine{} }

func (e *Engine) Name() string { return "pow" }

// ValidateHeader checks PoW-specific header rules:
//   - previous block hash matches parent
//   - PoW hash meets target
//   - bits match expected difficulty
func (e *Engine) ValidateHeader(header *types.BlockHeader, parent *types.BlockHeader, height uint32, p *params.ChainParams) error {
	parentHash := crypto.HashBlockHeader(parent)
	if header.PrevBlock != parentHash {
		return fmt.Errorf("prev block hash mismatch: header=%s expected=%s", header.PrevBlock, parentHash)
	}

	headerHash := crypto.HashBlockHeader(header)
	if err := crypto.ValidateProofOfWork(headerHash, header.Bits); err != nil {
		return fmt.Errorf("PoW validation failed at height %d: %w", height, err)
	}

	return nil
}

// ValidateBlock delegates to the shared structural validation.
func (e *Engine) ValidateBlock(block *types.Block, height uint32, p *params.ChainParams) error {
	return consensus.ValidateBlockStructure(block, height, p)
}

// CalcNextBits computes the difficulty for the next block.
// If NoRetarget is set, returns the current bits unchanged.
// Otherwise, at each RetargetInterval boundary, adjusts based on actual vs target timespan.
func (e *Engine) CalcNextBits(tip *types.BlockHeader, tipHeight uint32, getAncestor func(height uint32) *types.BlockHeader, p *params.ChainParams) uint32 {
	if p.NoRetarget {
		return p.InitialBits
	}

	nextHeight := tipHeight + 1
	if nextHeight%p.RetargetInterval != 0 {
		return tip.Bits
	}

	// Get the block at the start of this retarget window.
	windowStart := tipHeight - (p.RetargetInterval - 1)
	firstHeader := getAncestor(windowStart)
	if firstHeader == nil {
		return tip.Bits
	}

	actualTimespan := int64(tip.Timestamp) - int64(firstHeader.Timestamp)
	targetTimespan := int64(p.TargetTimespan / time.Second)

	// Clamp to [targetTimespan/4, targetTimespan*4] to prevent extreme swings.
	if actualTimespan < targetTimespan/4 {
		actualTimespan = targetTimespan / 4
	}
	if actualTimespan > targetTimespan*4 {
		actualTimespan = targetTimespan * 4
	}

	// newTarget = oldTarget * actualTimespan / targetTimespan
	oldTarget := crypto.CompactToBig(tip.Bits)
	newTarget := new(big.Int).Mul(oldTarget, big.NewInt(actualTimespan))
	newTarget.Div(newTarget, big.NewInt(targetTimespan))

	// Clamp to minimum difficulty (maximum target).
	maxTarget := crypto.CompactToBig(p.MinBits)
	if newTarget.Cmp(maxTarget) > 0 {
		newTarget = maxTarget
	}

	return crypto.BigToCompact(newTarget)
}

// PrepareHeader sets the difficulty bits on a new block header being built for mining.
func (e *Engine) PrepareHeader(header *types.BlockHeader, parent *types.BlockHeader, parentHeight uint32, getAncestor func(height uint32) *types.BlockHeader, p *params.ChainParams) error {
	header.Bits = e.CalcNextBits(parent, parentHeight, getAncestor, p)
	return nil
}

// SealHeader iterates the nonce to find a valid PoW solution.
// Returns true if found within maxIterations.
func (e *Engine) SealHeader(header *types.BlockHeader, target types.Hash, maxIterations uint64) (bool, error) {
	for i := uint64(0); i < maxIterations; i++ {
		hash := crypto.HashBlockHeader(header)
		if hash.LessOrEqual(target) {
			return true, nil
		}
		header.Nonce++
		if header.Nonce == 0 {
			// Nonce wrapped around; caller should update timestamp or extra nonce.
			return false, nil
		}
	}
	return false, nil
}

// MineGenesis mines a genesis block by iterating the nonce until the header hash
// is below the target defined by the block's Bits field.
func MineGenesis(block *types.Block) error {
	merkle, err := crypto.ComputeMerkleRoot(block.Transactions)
	if err != nil {
		return fmt.Errorf("compute merkle root: %w", err)
	}
	block.Header.MerkleRoot = merkle

	target := crypto.CompactToHash(block.Header.Bits)

	for {
		hash := crypto.HashBlockHeader(&block.Header)
		if hash.LessOrEqual(target) {
			return nil
		}
		block.Header.Nonce++
		if block.Header.Nonce == 0 {
			return fmt.Errorf("nonce space exhausted without finding valid genesis")
		}
	}
}

