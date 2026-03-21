// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package difficulty

import (
	"fmt"

	"github.com/bams-repo/fairchain/internal/difficulty/bitcoin"
	"github.com/bams-repo/fairchain/internal/difficulty/dgw"
	"github.com/bams-repo/fairchain/internal/difficulty/digishield"
	"github.com/bams-repo/fairchain/internal/difficulty/lwma"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

// Retargeter computes the next difficulty target for a blockchain.
// Implementations must be deterministic: same chain state always produces the
// same result. Implementations must be safe for concurrent use.
type Retargeter interface {
	// CalcNextBits computes the compact target (nBits) for the next block
	// given the current tip, its height, an ancestor lookup function, and
	// the chain parameters.
	CalcNextBits(tip *types.BlockHeader, tipHeight uint32, getAncestor func(height uint32) *types.BlockHeader, p *params.ChainParams) uint32

	// Name returns the algorithm identifier (e.g., "bitcoin", "lwma", "digishield").
	Name() string
}

// GetRetargeter returns a Retargeter for the named difficulty algorithm.
// Adding a new algorithm requires a new sub-package and a new case here.
func GetRetargeter(name string) (Retargeter, error) {
	switch name {
	case "bitcoin":
		return bitcoin.New(), nil
	case "lwma":
		return lwma.New(), nil
	case "dgw":
		return dgw.New(), nil
	case "digishield":
		return digishield.New(), nil
	default:
		return nil, fmt.Errorf("unknown difficulty algorithm: %q", name)
	}
}
