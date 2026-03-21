// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package digishield

import (
	"math/big"
	"testing"
	"time"

	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

func testParams(N uint32, T int, initialBits, minBits uint32) *params.ChainParams {
	return &params.ChainParams{
		InitialBits:        initialBits,
		MinBits:            minBits,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}
}

func makeChain(count int, T int, bits uint32, timeFactor float64) (map[uint32]*types.BlockHeader, func(uint32) *types.BlockHeader) {
	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := 0; i <= count; i++ {
		headers[uint32(i)] = &types.BlockHeader{
			Bits:      bits,
			Timestamp: baseTime + uint32(float64(i)*float64(T)*timeFactor),
		}
	}
	return headers, func(h uint32) *types.BlockHeader { return headers[h] }
}

func TestName(t *testing.T) {
	r := New()
	if r.Name() != "digishield" {
		t.Fatalf("expected 'digishield', got %q", r.Name())
	}
}

func TestNoRetarget(t *testing.T) {
	r := New()
	p := &params.ChainParams{
		InitialBits: 0x207fffff,
		NoRetarget:  true,
	}
	tip := &types.BlockHeader{Bits: 0x1e0fffff}
	bits := r.CalcNextBits(tip, 100, nil, p)
	if bits != p.InitialBits {
		t.Fatalf("NoRetarget: expected 0x%08x, got 0x%08x", p.InitialBits, bits)
	}
}

func TestGenesisBlock(t *testing.T) {
	r := New()
	p := testParams(4, 60, 0x1e0fffff, 0x1f7fffff)
	tip := &types.BlockHeader{Bits: p.InitialBits, Timestamp: 1700000000}
	bits := r.CalcNextBits(tip, 0, nil, p)
	if bits != p.InitialBits {
		t.Fatalf("genesis: expected 0x%08x, got 0x%08x", p.InitialBits, bits)
	}
}

func TestSteadyState(t *testing.T) {
	r := New()
	T := 60
	initialBits := uint32(0x1e0fffff)
	p := testParams(4, T, initialBits, 0x1f7fffff)

	headers, getAncestor := makeChain(20, T, initialBits, 1.0)
	bits := r.CalcNextBits(headers[20], 20, getAncestor, p)

	oldTarget := crypto.CompactToBig(initialBits)
	newTarget := crypto.CompactToBig(bits)

	ratio := new(big.Float).Quo(
		new(big.Float).SetInt(newTarget),
		new(big.Float).SetInt(oldTarget),
	)
	f, _ := ratio.Float64()
	if f < 0.95 || f > 1.05 {
		t.Fatalf("steady state: ratio %.4f too far from 1.0 (0x%08x -> 0x%08x)", f, initialBits, bits)
	}
}

func TestFastBlock_IncreaseDifficulty(t *testing.T) {
	r := New()
	T := 60
	initialBits := uint32(0x1e0fffff)
	p := testParams(4, T, initialBits, 0x1f7fffff)

	// Build a chain where the last block was mined in 1 second.
	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := 0; i <= 10; i++ {
		headers[uint32(i)] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: baseTime + uint32(i*T),
		}
	}
	// Override: block 10 arrives 1 second after block 9.
	headers[10].Timestamp = headers[9].Timestamp + 1
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	bits := r.CalcNextBits(headers[10], 10, getAncestor, p)

	oldTarget := crypto.CompactToBig(initialBits)
	newTarget := crypto.CompactToBig(bits)
	if newTarget.Cmp(oldTarget) >= 0 {
		t.Fatalf("fast block: target should decrease (harder), old=0x%08x new=0x%08x", initialBits, bits)
	}
}

func TestSlowBlock_DecreaseDifficulty(t *testing.T) {
	r := New()
	T := 60
	initialBits := uint32(0x1e0fffff)
	p := testParams(4, T, initialBits, 0x1f7fffff)

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := 0; i <= 10; i++ {
		headers[uint32(i)] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: baseTime + uint32(i*T),
		}
	}
	// Override: block 10 arrives 5 minutes late (5x target).
	headers[10].Timestamp = headers[9].Timestamp + uint32(T*5)
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	bits := r.CalcNextBits(headers[10], 10, getAncestor, p)

	oldTarget := crypto.CompactToBig(initialBits)
	newTarget := crypto.CompactToBig(bits)
	if newTarget.Cmp(oldTarget) <= 0 {
		t.Fatalf("slow block: target should increase (easier), old=0x%08x new=0x%08x", initialBits, bits)
	}
}

func TestAsymmetricDampening(t *testing.T) {
	// Core DigiShield property: a block that is 2x fast should produce a
	// smaller adjustment magnitude than a block that is 2x slow.
	// With N=4, T=60:
	//   2x fast (30s): dampened = (3*60 + 30)/4 = 52.5 → ratio = 52.5/60 = 0.875 → 12.5% harder
	//   2x slow (120s): dampened = (3*60 + 120)/4 = 75  → ratio = 75/60  = 1.25  → 25% easier
	// |easier adjustment| > |harder adjustment|
	r := New()
	T := 60
	initialBits := uint32(0x1e0fffff)
	p := testParams(4, T, initialBits, 0x1f7fffff)

	baseTime := uint32(1700000000)

	// 2x fast block.
	fastHeaders := make(map[uint32]*types.BlockHeader)
	for i := 0; i <= 10; i++ {
		fastHeaders[uint32(i)] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: baseTime + uint32(i*T),
		}
	}
	fastHeaders[10].Timestamp = fastHeaders[9].Timestamp + uint32(T/2)
	fastBits := r.CalcNextBits(fastHeaders[10], 10, func(h uint32) *types.BlockHeader { return fastHeaders[h] }, p)

	// 2x slow block.
	slowHeaders := make(map[uint32]*types.BlockHeader)
	for i := 0; i <= 10; i++ {
		slowHeaders[uint32(i)] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: baseTime + uint32(i*T),
		}
	}
	slowHeaders[10].Timestamp = slowHeaders[9].Timestamp + uint32(T*2)
	slowBits := r.CalcNextBits(slowHeaders[10], 10, func(h uint32) *types.BlockHeader { return slowHeaders[h] }, p)

	oldTarget := crypto.CompactToBig(initialBits)
	fastTarget := crypto.CompactToBig(fastBits)
	slowTarget := crypto.CompactToBig(slowBits)

	// Magnitude of adjustment from baseline.
	fastDelta := new(big.Int).Sub(oldTarget, fastTarget)  // positive: target decreased
	slowDelta := new(big.Int).Sub(slowTarget, oldTarget)   // positive: target increased

	fastDelta.Abs(fastDelta)
	slowDelta.Abs(slowDelta)

	// The slow (easier) adjustment should be larger in magnitude.
	if slowDelta.Cmp(fastDelta) <= 0 {
		t.Fatalf("asymmetric dampening violated: slow delta %s should be > fast delta %s",
			slowDelta, fastDelta)
	}
}

func TestMinBitsClamp(t *testing.T) {
	r := New()
	T := 60
	initialBits := uint32(0x1e0fffff)
	minBits := initialBits
	p := testParams(4, T, initialBits, minBits)

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := 0; i <= 10; i++ {
		headers[uint32(i)] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: baseTime + uint32(i*T),
		}
	}
	// Extremely slow block.
	headers[10].Timestamp = headers[9].Timestamp + uint32(T*1000)
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	bits := r.CalcNextBits(headers[10], 10, getAncestor, p)

	maxTarget := crypto.CompactToBig(minBits)
	newTarget := crypto.CompactToBig(bits)
	if newTarget.Cmp(maxTarget) > 0 {
		t.Fatalf("min bits clamp: target exceeds max, bits=0x%08x minBits=0x%08x", bits, minBits)
	}
}

func TestTimespanClamp_FastBlock(t *testing.T) {
	// Even with an instantaneous block, the T/4 clamp limits the
	// difficulty increase to at most 4x per block.
	r := New()
	T := 60
	initialBits := uint32(0x1e0fffff)
	p := testParams(4, T, initialBits, 0x1f7fffff)

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := 0; i <= 5; i++ {
		headers[uint32(i)] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: baseTime + uint32(i*T),
		}
	}
	// Same timestamp as previous block.
	headers[5].Timestamp = headers[4].Timestamp
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	bits := r.CalcNextBits(headers[5], 5, getAncestor, p)

	oldTarget := crypto.CompactToBig(initialBits)
	newTarget := crypto.CompactToBig(bits)

	// With dampening N=4 and solve_time=1 (floored):
	//   dampened = (3*60 + 1)/4 = 45
	//   ratio = 45/60 = 0.75
	// Target should be ~75% of old (25% harder). Not 4x harder.
	ratio := new(big.Float).Quo(
		new(big.Float).SetInt(newTarget),
		new(big.Float).SetInt(oldTarget),
	)
	f, _ := ratio.Float64()
	if f < 0.20 || f > 0.80 {
		t.Fatalf("fast clamp: expected ratio ~0.75, got %.4f (0x%08x -> 0x%08x)", f, initialBits, bits)
	}
}

func TestTimespanClamp_SlowBlock(t *testing.T) {
	// Even with an absurdly slow block, the T*4 clamp limits the
	// difficulty decrease.
	r := New()
	T := 60
	initialBits := uint32(0x1e0fffff)
	p := testParams(4, T, initialBits, 0x207fffff)

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := 0; i <= 5; i++ {
		headers[uint32(i)] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: baseTime + uint32(i*T),
		}
	}
	// Block takes 100x the target time.
	headers[5].Timestamp = headers[4].Timestamp + uint32(T*100)
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	bits := r.CalcNextBits(headers[5], 5, getAncestor, p)

	oldTarget := crypto.CompactToBig(initialBits)
	newTarget := crypto.CompactToBig(bits)

	// The outer clamp (T*4) limits the dampened timespan.
	// dampened = (3*60 + 6000)/4 = 1545, clamped to 240
	// ratio = 240/60 = 4.0
	ratio := new(big.Float).Quo(
		new(big.Float).SetInt(newTarget),
		new(big.Float).SetInt(oldTarget),
	)
	f, _ := ratio.Float64()
	if f > 4.5 {
		t.Fatalf("slow clamp: ratio %.4f exceeds 4x bound (0x%08x -> 0x%08x)", f, initialBits, bits)
	}
}

func TestOutOfOrderTimestamps(t *testing.T) {
	r := New()
	T := 60
	initialBits := uint32(0x1e0fffff)
	p := testParams(4, T, initialBits, 0x1f7fffff)

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := 0; i <= 10; i++ {
		headers[uint32(i)] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: baseTime + uint32(i*T),
		}
	}
	// Block 10 has an earlier timestamp than block 9.
	headers[10].Timestamp = headers[9].Timestamp - 30
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	bits := r.CalcNextBits(headers[10], 10, getAncestor, p)
	if bits == 0 {
		t.Fatal("out-of-order timestamps: got zero bits")
	}
}

func TestGradualResponse(t *testing.T) {
	// DigiShield should respond gradually over multiple blocks, not in one
	// big jump. Simulate 5 consecutive fast blocks and verify each
	// adjustment is moderate.
	r := New()
	T := 60
	initialBits := uint32(0x1e0fffff)
	p := testParams(4, T, initialBits, 0x1f7fffff)

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	headers[0] = &types.BlockHeader{Bits: initialBits, Timestamp: baseTime}

	currentBits := initialBits
	for i := 1; i <= 5; i++ {
		// Each block arrives in 10 seconds (6x fast).
		headers[uint32(i)] = &types.BlockHeader{
			Bits:      currentBits,
			Timestamp: headers[uint32(i-1)].Timestamp + 10,
		}
		getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }
		currentBits = r.CalcNextBits(headers[uint32(i)], uint32(i), getAncestor, p)
		headers[uint32(i)].Bits = currentBits

		oldTarget := crypto.CompactToBig(initialBits)
		newTarget := crypto.CompactToBig(currentBits)
		ratio := new(big.Float).Quo(
			new(big.Float).SetInt(newTarget),
			new(big.Float).SetInt(oldTarget),
		)
		f, _ := ratio.Float64()

		// Each block should adjust moderately. After N fast blocks the
		// compound effect is ~0.75^N. Ensure no single step drops below
		// 0.20 (5x harder), which would indicate a broken algorithm.
		if f < 0.20 {
			t.Fatalf("block %d: single-step adjustment too aggressive, ratio=%.4f", i, f)
		}
	}

	// After 5 fast blocks, target should be lower than initial.
	finalTarget := crypto.CompactToBig(currentBits)
	initTarget := crypto.CompactToBig(initialBits)
	if finalTarget.Cmp(initTarget) >= 0 {
		t.Fatal("after 5 fast blocks, difficulty should have increased")
	}
}

func TestSmallDampeningFactor(t *testing.T) {
	// With N=2, DigiShield should be more responsive.
	r := New()
	T := 60
	initialBits := uint32(0x1e0fffff)
	p := testParams(2, T, initialBits, 0x1f7fffff)

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	headers[0] = &types.BlockHeader{Bits: initialBits, Timestamp: baseTime}
	headers[1] = &types.BlockHeader{Bits: initialBits, Timestamp: baseTime + uint32(T)}
	// Fast block.
	headers[2] = &types.BlockHeader{Bits: initialBits, Timestamp: baseTime + uint32(T) + 5}
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	bits := r.CalcNextBits(headers[2], 2, getAncestor, p)

	oldTarget := crypto.CompactToBig(initialBits)
	newTarget := crypto.CompactToBig(bits)
	if newTarget.Cmp(oldTarget) >= 0 {
		t.Fatalf("small N fast block: target should decrease, old=0x%08x new=0x%08x", initialBits, bits)
	}
}
