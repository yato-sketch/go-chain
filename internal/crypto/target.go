package crypto

import (
	"fmt"
	"math/big"

	"github.com/fairchain/fairchain/internal/types"
)

// CompactToBig converts a compact "bits" representation to a full 256-bit target.
// Format: the top byte is the exponent, the lower 3 bytes are the mantissa.
// target = mantissa * 2^(8*(exponent-3))
//
// This matches the Bitcoin nBits encoding.
func CompactToBig(compact uint32) *big.Int {
	mantissa := compact & 0x007fffff
	negative := compact&0x00800000 != 0
	exponent := compact >> 24

	var target big.Int
	if exponent <= 3 {
		mantissa >>= 8 * (3 - exponent)
		target.SetInt64(int64(mantissa))
	} else {
		target.SetInt64(int64(mantissa))
		target.Lsh(&target, 8*(uint(exponent)-3))
	}

	if negative {
		target.Neg(&target)
	}
	return &target
}

// BigToCompact converts a big.Int target back to compact "bits" form.
func BigToCompact(target *big.Int) uint32 {
	if target.Sign() == 0 {
		return 0
	}

	negative := target.Sign() < 0
	t := new(big.Int).Abs(target)

	// Determine the byte length.
	byteLen := uint32((t.BitLen() + 7) / 8)
	var compact uint32

	if byteLen <= 3 {
		compact = uint32(t.Int64()) << (8 * (3 - byteLen))
	} else {
		shifted := new(big.Int).Rsh(t, 8*(uint(byteLen)-3))
		compact = uint32(shifted.Int64())
	}

	// Normalize: if the high bit of the mantissa is set, shift up.
	if compact&0x00800000 != 0 {
		compact >>= 8
		byteLen++
	}

	compact |= byteLen << 24
	if negative {
		compact |= 0x00800000
	}
	return compact
}

// CompactToHash converts compact bits to a Hash representing the target.
// The hash is in little-endian internal byte order.
func CompactToHash(compact uint32) types.Hash {
	target := CompactToBig(compact)
	b := target.Bytes() // big-endian
	var h types.Hash
	// Write into hash in little-endian order.
	for i, j := 0, len(b)-1; j >= 0; i, j = i+1, j-1 {
		if i >= types.HashSize {
			break
		}
		h[i] = b[j]
	}
	return h
}

// HashToBig converts a hash (little-endian) to a big.Int for arithmetic.
func HashToBig(h types.Hash) *big.Int {
	// Reverse to big-endian for big.Int.
	var be [types.HashSize]byte
	for i := 0; i < types.HashSize; i++ {
		be[i] = h[types.HashSize-1-i]
	}
	return new(big.Int).SetBytes(be[:])
}

// CalcWork computes the work represented by a given compact target.
// work = 2^256 / (target + 1)
func CalcWork(bits uint32) *big.Int {
	target := CompactToBig(bits)
	if target.Sign() <= 0 {
		return big.NewInt(0)
	}
	// 2^256
	oneLsh256 := new(big.Int).Lsh(big.NewInt(1), 256)
	denominator := new(big.Int).Add(target, big.NewInt(1))
	return new(big.Int).Div(oneLsh256, denominator)
}

// ValidateProofOfWork checks that the block header hash is <= the target defined by bits.
func ValidateProofOfWork(headerHash types.Hash, bits uint32) error {
	target := CompactToHash(bits)
	if !headerHash.LessOrEqual(target) {
		return fmt.Errorf("block hash %s exceeds target %s", headerHash, target)
	}
	return nil
}
