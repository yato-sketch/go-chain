// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package bitcoin

import (
	"math/big"
	"time"

	"github.com/bams-repo/fairchain/internal/consensus"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

// Retargeter implements Nakamoto-style epoch-based difficulty adjustment,
// matching Bitcoin Core's GetNextWorkRequired logic.
type Retargeter struct{}

func New() *Retargeter { return &Retargeter{} }

func (r *Retargeter) Name() string { return "bitcoin" }

// CalcNextBits computes the difficulty for the next block.
// If NoRetarget is set, returns the current bits unchanged.
// Otherwise, at each RetargetInterval boundary, adjusts based on actual vs target timespan.
func (r *Retargeter) CalcNextBits(tip *types.BlockHeader, tipHeight uint32, getAncestor func(height uint32) *types.BlockHeader, p *params.ChainParams) uint32 {
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
		logging.L.Error("nil ancestor at retarget boundary — possible data corruption",
			"component", "difficulty", "height", windowStart)
		return tip.Bits
	}

	// Time-warp mitigation for window start: cap the first block's
	// timestamp at its MTP + MaxTimeFutureDrift. This prevents a majority
	// miner from setting an extreme future timestamp on the first block
	// of a retarget window to shrink the apparent timespan and inflate
	// difficulty on competitors.
	firstTS := int64(firstHeader.Timestamp)
	firstMTP := int64(consensus.CalcMedianTimePast(windowStart, getAncestor))
	maxFirstTS := firstMTP + int64(p.MaxTimeFutureDrift/time.Second)
	if firstTS > maxFirstTS {
		firstTS = maxFirstTS
	}

	// Time-warp mitigation: cap the tip timestamp used for retarget at
	// MTP + MaxTimeFutureDrift. This prevents a majority miner from
	// inflating the apparent timespan by setting an extreme future
	// timestamp on the last block of a retarget window.
	tipTS := int64(tip.Timestamp)
	mtp := int64(consensus.CalcMedianTimePast(tipHeight, getAncestor))
	maxRetargetTS := mtp + int64(p.MaxTimeFutureDrift/time.Second)
	if tipTS > maxRetargetTS {
		tipTS = maxRetargetTS
	}

	actualTimespan := tipTS - firstTS
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

	// Floor: target must be at least 1. A zero target is unsatisfiable.
	if newTarget.Sign() <= 0 {
		newTarget.SetInt64(1)
	}

	// Clamp to minimum difficulty (maximum target).
	maxTarget := crypto.CompactToBig(p.MinBits)
	if newTarget.Cmp(maxTarget) > 0 {
		newTarget = maxTarget
	}

	return crypto.BigToCompact(newTarget)
}
