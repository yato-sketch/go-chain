// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package dgw

import (
	"testing"
	"time"

	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

func TestName(t *testing.T) {
	r := New()
	if r.Name() != "dgw" {
		t.Fatalf("expected name 'dgw', got %q", r.Name())
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

func TestInsufficientHistory(t *testing.T) {
	r := New()
	p := &params.ChainParams{
		InitialBits:        0x1e0fffff,
		MinBits:            0x1e0fffff,
		NoRetarget:         false,
		RetargetInterval:   24,
		TargetBlockSpacing: 150 * time.Second,
	}
	tip := &types.BlockHeader{Bits: p.InitialBits, Timestamp: 1700000000}
	bits := r.CalcNextBits(tip, 10, nil, p)
	if bits != p.InitialBits {
		t.Fatalf("insufficient history: expected 0x%08x, got 0x%08x", p.InitialBits, bits)
	}
}

func makeHeaders(N uint32, T int, initialBits uint32, timeFactor float64) (map[uint32]*types.BlockHeader, func(uint32) *types.BlockHeader) {
	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := uint32(0); i <= N; i++ {
		headers[i] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: baseTime + uint32(float64(i)*float64(T)*timeFactor),
		}
	}
	return headers, func(h uint32) *types.BlockHeader { return headers[h] }
}

func TestSteadyState(t *testing.T) {
	r := New()
	N := uint32(24)
	T := 150
	initialBits := uint32(0x1e0fffff)

	p := &params.ChainParams{
		InitialBits:        initialBits,
		MinBits:            0x1f7fffff,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}

	headers, getAncestor := makeHeaders(N, T, initialBits, 1.0)
	bits := r.CalcNextBits(headers[N], N, getAncestor, p)

	oldTarget := crypto.CompactToBig(initialBits)
	newTarget := crypto.CompactToBig(bits)

	ratio := float64(newTarget.Int64()) / float64(oldTarget.Int64())
	if ratio < 0.95 || ratio > 1.05 {
		t.Fatalf("steady state: target ratio %.4f is too far from 1.0 (bits: 0x%08x -> 0x%08x)", ratio, initialBits, bits)
	}
}

func TestFastBlocks_IncreaseDifficulty(t *testing.T) {
	r := New()
	N := uint32(24)
	T := 150
	initialBits := uint32(0x1e0fffff)

	p := &params.ChainParams{
		InitialBits:        initialBits,
		MinBits:            0x1f7fffff,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}

	headers, getAncestor := makeHeaders(N, T, initialBits, 0.5)
	bits := r.CalcNextBits(headers[N], N, getAncestor, p)

	oldTarget := crypto.CompactToBig(initialBits)
	newTarget := crypto.CompactToBig(bits)
	if newTarget.Cmp(oldTarget) >= 0 {
		t.Fatalf("fast blocks: new target should be lower (harder), old=0x%08x new=0x%08x", initialBits, bits)
	}
}

func TestSlowBlocks_DecreaseDifficulty(t *testing.T) {
	r := New()
	N := uint32(24)
	T := 150
	initialBits := uint32(0x1e0fffff)

	p := &params.ChainParams{
		InitialBits:        initialBits,
		MinBits:            0x1f7fffff,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}

	headers, getAncestor := makeHeaders(N, T, initialBits, 2.0)
	bits := r.CalcNextBits(headers[N], N, getAncestor, p)

	oldTarget := crypto.CompactToBig(initialBits)
	newTarget := crypto.CompactToBig(bits)
	if newTarget.Cmp(oldTarget) <= 0 {
		t.Fatalf("slow blocks: new target should be higher (easier), old=0x%08x new=0x%08x", initialBits, bits)
	}
}

func TestMinBitsClamp(t *testing.T) {
	r := New()
	N := uint32(24)
	T := 150
	initialBits := uint32(0x1e0fffff)
	minBits := uint32(0x1e0fffff)

	p := &params.ChainParams{
		InitialBits:        initialBits,
		MinBits:            minBits,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}

	// Extremely slow blocks — target wants to go way up but is clamped.
	headers, getAncestor := makeHeaders(N, T, initialBits, 100.0)
	bits := r.CalcNextBits(headers[N], N, getAncestor, p)

	maxTarget := crypto.CompactToBig(minBits)
	newTarget := crypto.CompactToBig(bits)
	if newTarget.Cmp(maxTarget) > 0 {
		t.Fatalf("min bits clamp: new target exceeds max, bits=0x%08x minBits=0x%08x", bits, minBits)
	}
}

func TestTimespanClamp_PreventExtremeDecrease(t *testing.T) {
	// When blocks are absurdly fast (near-zero timespan), the 1/3 clamp
	// should prevent difficulty from spiking more than 3x.
	r := New()
	N := uint32(24)
	T := 150
	initialBits := uint32(0x1e0fffff)

	p := &params.ChainParams{
		InitialBits:        initialBits,
		MinBits:            0x1f7fffff,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}

	// All blocks at the same timestamp — actual timespan = 0, clamped to expected/3.
	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := uint32(0); i <= N; i++ {
		headers[i] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: baseTime,
		}
	}
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	bits := r.CalcNextBits(headers[N], N, getAncestor, p)

	oldTarget := crypto.CompactToBig(initialBits)
	newTarget := crypto.CompactToBig(bits)

	// With clamping at expected/3, the target should be ~1/3 of old (3x harder).
	// Allow some tolerance for compact encoding rounding.
	ratio := float64(newTarget.Int64()) / float64(oldTarget.Int64())
	if ratio < 0.25 || ratio > 0.40 {
		t.Fatalf("zero-timespan clamp: expected ratio ~0.33, got %.4f (bits: 0x%08x -> 0x%08x)", ratio, initialBits, bits)
	}
}

func TestTimespanClamp_PreventExtremeIncrease(t *testing.T) {
	// When blocks are absurdly slow, the 3x clamp should prevent difficulty
	// from dropping more than 3x per adjustment.
	r := New()
	N := uint32(24)
	T := 150
	initialBits := uint32(0x1e0fffff)

	p := &params.ChainParams{
		InitialBits:        initialBits,
		MinBits:            0x207fffff,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}

	// Blocks 100x slower than target.
	headers, getAncestor := makeHeaders(N, T, initialBits, 100.0)
	bits := r.CalcNextBits(headers[N], N, getAncestor, p)

	oldTarget := crypto.CompactToBig(initialBits)
	newTarget := crypto.CompactToBig(bits)

	// With clamping at expected*3, the target should be ~3x old (3x easier).
	ratio := float64(newTarget.Int64()) / float64(oldTarget.Int64())
	if ratio < 2.5 || ratio > 3.5 {
		t.Fatalf("slow-timespan clamp: expected ratio ~3.0, got %.4f (bits: 0x%08x -> 0x%08x)", ratio, initialBits, bits)
	}
}

func TestOutOfOrderTimestamps(t *testing.T) {
	r := New()
	N := uint32(24)
	T := 150
	initialBits := uint32(0x1e0fffff)

	p := &params.ChainParams{
		InitialBits:        initialBits,
		MinBits:            0x1f7fffff,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := uint32(0); i <= N; i++ {
		ts := baseTime + i*uint32(T)
		if i == 10 {
			ts = baseTime + 5*uint32(T) // jump backwards
		}
		if i == 15 {
			ts = baseTime + 12*uint32(T) // another backwards jump
		}
		headers[i] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: ts,
		}
	}
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	bits := r.CalcNextBits(headers[N], N, getAncestor, p)
	if bits == 0 {
		t.Fatal("out-of-order timestamps: got zero bits")
	}
}

func TestNegativeTimespan(t *testing.T) {
	// If the tip timestamp is before the window start (pathological case),
	// DGW should clamp and not panic.
	r := New()
	N := uint32(10)
	T := 60
	initialBits := uint32(0x1e0fffff)

	p := &params.ChainParams{
		InitialBits:        initialBits,
		MinBits:            0x1f7fffff,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := uint32(0); i <= N; i++ {
		// Tip has an earlier timestamp than window start.
		headers[i] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: baseTime - i*uint32(T),
		}
	}
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	bits := r.CalcNextBits(headers[N], N, getAncestor, p)
	if bits == 0 {
		t.Fatal("negative timespan: got zero bits")
	}
}

func TestVaryingDifficulty(t *testing.T) {
	// Blocks with different difficulty levels — DGW should average them.
	r := New()
	N := uint32(24)
	T := 150
	easyBits := uint32(0x1e0fffff)
	hardBits := uint32(0x1d0fffff)

	p := &params.ChainParams{
		InitialBits:        easyBits,
		MinBits:            0x1f7fffff,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := uint32(0); i <= N; i++ {
		bits := easyBits
		if i%2 == 0 {
			bits = hardBits
		}
		headers[i] = &types.BlockHeader{
			Bits:      bits,
			Timestamp: baseTime + i*uint32(T),
		}
	}
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	bits := r.CalcNextBits(headers[N], N, getAncestor, p)

	easyTarget := crypto.CompactToBig(easyBits)
	hardTarget := crypto.CompactToBig(hardBits)
	newTarget := crypto.CompactToBig(bits)

	// The result should be between the hard and easy targets.
	if newTarget.Cmp(hardTarget) < 0 {
		t.Fatalf("varying difficulty: result harder than hardest block, bits=0x%08x", bits)
	}
	if newTarget.Cmp(easyTarget) > 0 {
		t.Fatalf("varying difficulty: result easier than easiest block, bits=0x%08x", bits)
	}
}

func TestSingleBlockBoundary(t *testing.T) {
	// Exactly at the minimum window size (tipHeight == N).
	r := New()
	N := uint32(5)
	T := 60
	initialBits := uint32(0x1e0fffff)

	p := &params.ChainParams{
		InitialBits:        initialBits,
		MinBits:            0x1f7fffff,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}

	headers, getAncestor := makeHeaders(N, T, initialBits, 1.0)
	bits := r.CalcNextBits(headers[N], N, getAncestor, p)
	if bits == 0 {
		t.Fatal("boundary case: got zero bits")
	}
}
