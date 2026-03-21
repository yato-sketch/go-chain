// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package crypto

import (
	"github.com/bams-repo/fairchain/internal/types"
)

// MerkleRoot computes the merkle root of a list of transaction hashes.
// Uses the Bitcoin-style algorithm: if the number of hashes at any level is odd,
// the last hash is duplicated. An empty list returns ZeroHash.
//
// Consensus-critical: the order of hashes must be deterministic (tx order in block).
func MerkleRoot(hashes []types.Hash) types.Hash {
	if len(hashes) == 0 {
		return types.ZeroHash
	}

	// Work on a copy to avoid mutating the caller's slice.
	level := make([]types.Hash, len(hashes))
	copy(level, hashes)

	for len(level) > 1 {
		if len(level)%2 != 0 {
			level = append(level, level[len(level)-1])
		}
		next := make([]types.Hash, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			var combined [types.HashSize * 2]byte
			copy(combined[:types.HashSize], level[i][:])
			copy(combined[types.HashSize:], level[i+1][:])
			next[i/2] = DoubleSHA256(combined[:])
		}
		level = next
	}
	return level[0]
}

// ComputeMerkleRoot computes the merkle root from a slice of transactions.
func ComputeMerkleRoot(txs []types.Transaction) (types.Hash, error) {
	if len(txs) == 0 {
		return types.ZeroHash, nil
	}
	hashes := make([]types.Hash, len(txs))
	for i := range txs {
		h, err := HashTransaction(&txs[i])
		if err != nil {
			return types.ZeroHash, err
		}
		hashes[i] = h
	}
	return MerkleRoot(hashes), nil
}
