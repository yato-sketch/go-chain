package protocol

import "crypto/sha256"

// doubleSHA256Checksum computes SHA256(SHA256(data)) and returns the first 4 bytes.
// This is used for wire message checksums only — not consensus hashing.
func doubleSHA256Checksum(data []byte) [4]byte {
	first := sha256.Sum256(data)
	second := sha256.Sum256(first[:])
	var checksum [4]byte
	copy(checksum[:], second[:4])
	return checksum
}
