// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package digishield

import (
	"math/big"
	"time"

	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

// Retargeter implements DigiShield v3, the per-block difficulty adjustment
// algorithm created by the DigiByte team. DigiShield is used in production
// by Dogecoin, Zcash, and DigiByte — making it one of the most widely
// deployed per-block retargeting algorithms.
//
// The key innovation is asymmetric dampening: difficulty increases are
// dampened more aggressively than decreases. This protects against
// "pool-hopping" attacks where large miners temporarily point hash rate
// at a coin, mine easy blocks, then leave — causing a difficulty spike
// that strands honest miners on a chain that is now too hard.
//
// Algorithm (per Dogecoin/Zcash production code):
//  1. Look back 1 block to get the previous target and solve time.
//  2. Compute the actual solve time for the most recent block.
//  3. Apply asymmetric dampening via the Kimoto-style "nActualTimespan"
//     formula: nActualTimespan = ((N-1) * T + actualSolveTime) / N
//     where N is the dampening factor (default 4). This smooths the
//     response and biases toward the target spacing.
//  4. Clamp the dampened timespan to [T/4, T*4] to bound adjustments.
//  5. Adjust: nextTarget = prevTarget * dampenedTimespan / T.
//  6. Clamp to MinBits.
//
// The dampening factor N=4 means:
//   - If blocks are 2x fast: adjustment = (3*T + T/2) / 4 = 0.875*T → ~12.5% harder
//   - If blocks are 2x slow: adjustment = (3*T + 2*T) / 4 = 1.25*T  → ~25% easier
//   This asymmetry (easier to drop difficulty than raise it) is the core
//   defense against multipooling.
//
// Reference: DigiByte Core, Dogecoin Core src/dogecoin.cpp, Zcash src/pow.cpp
//
// The target solve time T is taken from ChainParams.TargetBlockSpacing.
// RetargetInterval is not used as an epoch size — DigiShield always adjusts
// every block. It is used only as the dampening factor N.
type Retargeter struct{}

func New() *Retargeter { return &Retargeter{} }

func (r *Retargeter) Name() string { return "digishield" }

// CalcNextBits computes the compact target for the next block using DigiShield v3.
func (r *Retargeter) CalcNextBits(
	tip *types.BlockHeader,
	tipHeight uint32,
	getAncestor func(height uint32) *types.BlockHeader,
	p *params.ChainParams,
) uint32 {
	if p.NoRetarget {
		return p.InitialBits
	}

	// Need at least 1 prior block to compute a solve time.
	if tipHeight < 1 {
		return p.InitialBits
	}

	T := int64(p.TargetBlockSpacing / time.Second)

	// Dampening factor. DigiShield uses 4 in production (Dogecoin, Zcash).
	// We use RetargetInterval for configurability; default should be 4.
	N := int64(p.RetargetInterval)
	if N < 2 {
		N = 4
	}

	// Get the previous block for solve time computation.
	prev := getAncestor(tipHeight - 1)
	if prev == nil {
		logging.L.Error("nil ancestor in DigiShield — possible data corruption",
			"component", "difficulty", "height", tipHeight-1)
		return tip.Bits
	}

	// Actual solve time of the most recent block.
	actualSolveTime := int64(tip.Timestamp) - int64(prev.Timestamp)

	// Floor: solve time must be at least 1 second to prevent division
	// issues and handle out-of-order timestamps.
	if actualSolveTime < 1 {
		actualSolveTime = 1
	}

	// Asymmetric dampening (Kimoto-style averaging):
	//   dampenedTimespan = ((N-1) * T + actualSolveTime) / N
	//
	// This blends the target spacing with the actual solve time, weighted
	// heavily toward the target. The result is that a single fast or slow
	// block only moves difficulty by ~1/N of the full adjustment.
	dampenedTimespan := ((N - 1) * T + actualSolveTime) / N

	// Clamp to [T/4, T*4] to prevent extreme single-block adjustments.
	// This matches Dogecoin/Zcash production bounds.
	if dampenedTimespan < T/4 {
		dampenedTimespan = T / 4
	}
	if dampenedTimespan > T*4 {
		dampenedTimespan = T * 4
	}

	// nextTarget = tipTarget * dampenedTimespan / T
	tipTarget := crypto.CompactToBig(tip.Bits)
	nextTarget := new(big.Int).Mul(tipTarget, big.NewInt(dampenedTimespan))
	nextTarget.Div(nextTarget, big.NewInt(T))

	// Floor: target must be at least 1.
	if nextTarget.Sign() <= 0 {
		nextTarget.SetInt64(1)
	}

	// Clamp to minimum difficulty (maximum target).
	maxTarget := crypto.CompactToBig(p.MinBits)
	if nextTarget.Cmp(maxTarget) > 0 {
		nextTarget = maxTarget
	}

	return crypto.BigToCompact(nextTarget)
}
