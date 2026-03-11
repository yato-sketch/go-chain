package crypto

import (
	"crypto/sha256"

	"github.com/fairchain/fairchain/internal/types"
)

// DoubleSHA256 computes SHA256(SHA256(data)), the standard consensus hash.
func DoubleSHA256(data []byte) types.Hash {
	first := sha256.Sum256(data)
	second := sha256.Sum256(first[:])
	return types.HashFromBytes(second[:])
}

// HashBlockHeader computes the double-SHA256 of the canonical 80-byte header.
// This is the block's identity and PoW hash.
func HashBlockHeader(h *types.BlockHeader) types.Hash {
	return DoubleSHA256(h.SerializeToBytes())
}

// HashTransaction computes the double-SHA256 of the canonical transaction bytes.
func HashTransaction(tx *types.Transaction) (types.Hash, error) {
	data, err := tx.SerializeToBytes()
	if err != nil {
		return types.ZeroHash, err
	}
	return DoubleSHA256(data), nil
}
