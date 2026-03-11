package consensus

import (
	"github.com/fairchain/fairchain/internal/params"
	"github.com/fairchain/fairchain/internal/types"
)

// Engine defines the pluggable consensus interface.
// The baseline implementation is PoW. Future implementations may include
// identity-bound ticket-based sequential-work consensus.
//
// Design boundary: the Engine is responsible for:
//   - validating that a header satisfies the consensus challenge
//   - validating block-level consensus rules
//   - computing retarget adjustments
//   - preparing block templates (challenge parameters)
//
// The Engine is NOT responsible for:
//   - transaction validation beyond coinbase rules
//   - chain selection (that's the chain manager's job)
//   - networking
type Engine interface {
	// ValidateHeader checks that the header satisfies consensus rules
	// (e.g., PoW target, timestamp bounds) given the parent header and params.
	ValidateHeader(header *types.BlockHeader, parent *types.BlockHeader, height uint32, p *params.ChainParams) error

	// ValidateBlock checks block-level consensus rules: coinbase, merkle root,
	// transaction ordering, size limits, etc.
	ValidateBlock(block *types.Block, height uint32, p *params.ChainParams) error

	// CalcNextBits computes the difficulty bits for the next block given
	// the chain state at the current tip.
	CalcNextBits(tip *types.BlockHeader, tipHeight uint32, getAncestor func(height uint32) *types.BlockHeader, p *params.ChainParams) uint32

	// PrepareHeader fills in consensus-specific fields on a new block header
	// being constructed for mining (e.g., sets bits).
	PrepareHeader(header *types.BlockHeader, parent *types.BlockHeader, parentHeight uint32, getAncestor func(height uint32) *types.BlockHeader, p *params.ChainParams) error

	// SealHeader attempts to find a valid nonce for the header.
	// Returns true if a valid nonce was found within maxIterations.
	// The header's Nonce field is updated in place.
	SealHeader(header *types.BlockHeader, target types.Hash, maxIterations uint64) (bool, error)

	// Name returns the consensus engine name for logging/identification.
	Name() string
}
