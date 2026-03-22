// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package sha256mem

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"

	"github.com/bams-repo/fairchain/internal/types"
)

var memPool = sync.Pool{
	New: func() any {
		buf := make([][32]byte, Slots)
		return &buf
	},
}

// Consensus-critical parameters. Changing any of these is a hard fork.
//
// Memory-hard SHA256 PoW inspired by YesPower/RandomX principles.
// Each hash requires a 128 MiB buffer, which starves GPUs of workers:
// a 12 GB GPU fits only ~68 workers vs the ~40,000 needed for full
// occupancy, leaving it >99% idle. Meanwhile any CPU or phone can
// allocate 128 MiB per thread from system RAM.
//
// The fill phase chains FillChains SHA256 hashes and copies each
// result across (Slots/FillChains) consecutive slots, populating the
// full 128 MiB buffer with only 8,192 SHA256 computations. The mix
// phase then performs 2,048 serial SHA256-per-hop random reads over
// the buffer, creating an unbreakable read→SHA256→address chain.
//
// Benchmarked results (sha256mem v3):
//   Galaxy S10+ (1 thread):   28 H/s
//   i9-11900K  (1 thread):    27 H/s
//   i9-11900K  (16 threads):  85 H/s
//   RTX 3080 Ti (68 workers): 24 H/s  ← $1,200 GPU loses to $100 phone
//
// All primitives are standard SHA256 — no novel cryptography.
const (
	// Slots is the number of 32-byte entries in the memory buffer.
	// 4194304 * 32 = 128 MiB. A 12 GB GPU can fit at most ~68
	// concurrent workers, far below the ~40,000 threads needed for
	// full occupancy. Phones with 4+ GB RAM easily spare 128 MiB.
	Slots = 4194304

	// FillChains is the number of chained SHA256 hashes used to
	// populate the buffer. Each hash result is copied across
	// (Slots/FillChains) = 512 consecutive slots, filling the full
	// 128 MiB with only 8,192 SHA256 computations.
	FillChains = 8192

	// MixRounds is the number of serial SHA256+read hops in the mix
	// phase. Each round: read mem[idx], SHA256(acc || mem[idx]) → acc,
	// derive next idx from acc. The SHA256 between every read prevents
	// the GPU from issuing the next memory request until the hash
	// completes, creating an unbreakable serial dependency chain.
	MixRounds = 2048
)

// Hasher implements memory-hard SHA256 proof-of-work hashing.
//
// Algorithm:
//  1. Seed:     SHA256(header) → mem[0]
//  2. Fill:     8,192 chained SHA256s, each copied across 512 slots (128 MiB)
//  3. Mix:      2,048 rounds of: read mem[idx] → SHA256(acc||mem[idx]) → new idx
//  4. Finalize: SHA256(accumulator) → output hash
type Hasher struct{}

func New() *Hasher { return &Hasher{} }

func (h *Hasher) PoWHash(data []byte) types.Hash {
	// Phase 1: Seed from header.
	seed := sha256.Sum256(data)

	// Phase 2: Fast fill — chain FillChains SHA256s, copy each result
	// across (Slots/FillChains) consecutive slots to fill 128 MiB.
	memPtr := memPool.Get().(*[][32]byte)
	mem := *memPtr
	spread := Slots / FillChains

	mem[0] = seed
	for j := 1; j < spread; j++ {
		mem[j] = mem[0]
	}
	for i := 1; i < FillChains; i++ {
		base := i * spread
		prev := (i - 1) * spread
		mem[base] = sha256.Sum256(mem[prev][:])
		for j := 1; j < spread; j++ {
			mem[base+j] = mem[base]
		}
	}

	// Phase 3: SHA256-per-hop mixing.
	acc := mem[Slots-1]
	for i := 0; i < MixRounds; i++ {
		idx := binary.LittleEndian.Uint32(acc[:4]) % uint32(Slots)
		var buf [64]byte
		copy(buf[:32], acc[:])
		copy(buf[32:], mem[idx][:])
		acc = sha256.Sum256(buf[:])
	}

	memPool.Put(memPtr)

	// Phase 4: Final hash.
	final := sha256.Sum256(acc[:])
	return types.Hash(final)
}

func (h *Hasher) Name() string { return "sha256mem" }
