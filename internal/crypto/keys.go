// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"github.com/bams-repo/fairchain/internal/types"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"golang.org/x/crypto/ripemd160"
)

const (
	PrivKeySize       = 32
	CompressedPubSize = 33
	PubKeyHashSize    = 20
)

// GenerateKeyPair creates a new random secp256k1 private key and returns
// the private key bytes and compressed public key bytes.
func GenerateKeyPair() (privKey []byte, pubKey []byte, err error) {
	key, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return nil, nil, fmt.Errorf("generate secp256k1 key: %w", err)
	}
	return key.Serialize(), key.PubKey().SerializeCompressed(), nil
}

// PrivKeyFromBytes reconstructs a secp256k1 private key from raw bytes.
func PrivKeyFromBytes(b []byte) (*secp256k1.PrivateKey, error) {
	if len(b) != PrivKeySize {
		return nil, fmt.Errorf("private key must be %d bytes, got %d", PrivKeySize, len(b))
	}
	key := secp256k1.PrivKeyFromBytes(b)
	return key, nil
}

// PubKeyFromBytes parses a compressed secp256k1 public key from bytes.
func PubKeyFromBytes(b []byte) (*secp256k1.PublicKey, error) {
	if len(b) != CompressedPubSize {
		return nil, fmt.Errorf("compressed pubkey must be %d bytes, got %d", CompressedPubSize, len(b))
	}
	return secp256k1.ParsePubKey(b)
}

// Hash160 computes RIPEMD160(SHA256(data)), the standard Bitcoin address hash.
func Hash160(data []byte) [PubKeyHashSize]byte {
	sha := sha256.Sum256(data)
	rip := ripemd160.New()
	rip.Write(sha[:])
	var out [PubKeyHashSize]byte
	copy(out[:], rip.Sum(nil))
	return out
}

// PubKeyHash returns the Hash160 of a compressed public key.
// This is the 20-byte identifier used in P2PKH locking scripts.
func PubKeyHash(compressedPubKey []byte) [PubKeyHashSize]byte {
	return Hash160(compressedPubKey)
}

// MakeP2PKHScript builds a standard P2PKH locking script:
//
//	OP_DUP OP_HASH160 <20-byte pubkey hash> OP_EQUALVERIFY OP_CHECKSIG
//
// This is the Bitcoin-standard output script format.
func MakeP2PKHScript(pubKeyHash [PubKeyHashSize]byte) []byte {
	script := make([]byte, 25)
	script[0] = OpDup
	script[1] = OpHash160
	script[2] = 0x14 // push 20 bytes
	copy(script[3:23], pubKeyHash[:])
	script[23] = OpEqualVerify
	script[24] = OpCheckSig
	return script
}

// MakeP2PKHScriptFromPubKey is a convenience that derives the P2PKH script
// directly from a compressed public key.
func MakeP2PKHScriptFromPubKey(compressedPubKey []byte) []byte {
	hash := PubKeyHash(compressedPubKey)
	return MakeP2PKHScript(hash)
}

// MakeP2PKHSignatureScript builds a standard P2PKH unlocking script:
//
//	<signature> <compressed pubkey>
//
// The signature must include the sighash type byte appended.
func MakeP2PKHSignatureScript(sig []byte, compressedPubKey []byte) []byte {
	script := make([]byte, 0, 1+len(sig)+1+len(compressedPubKey))
	script = append(script, byte(len(sig)))
	script = append(script, sig...)
	script = append(script, byte(len(compressedPubKey)))
	script = append(script, compressedPubKey...)
	return script
}

// IsP2PKHScript returns true if the script matches the standard P2PKH pattern:
//
//	OP_DUP OP_HASH160 <20 bytes> OP_EQUALVERIFY OP_CHECKSIG
func IsP2PKHScript(script []byte) bool {
	return len(script) == 25 &&
		script[0] == OpDup &&
		script[1] == OpHash160 &&
		script[2] == 0x14 &&
		script[23] == OpEqualVerify &&
		script[24] == OpCheckSig
}

// ExtractP2PKHHash extracts the 20-byte pubkey hash from a P2PKH script.
// Returns nil if the script is not P2PKH.
func ExtractP2PKHHash(script []byte) []byte {
	if !IsP2PKHScript(script) {
		return nil
	}
	return script[3:23]
}

// GenerateRandomBytes returns n random bytes from crypto/rand.
func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

// Opcodes used in P2PKH scripts.
const (
	OpDup         = 0x76
	OpHash160     = 0xa9
	OpEqualVerify = 0x88
	OpCheckSig    = 0xac
)

// IsUnspendableScript returns true if the script is provably unspendable.
// Currently recognizes OP_RETURN (0x6a) prefix and legacy burn markers.
func IsUnspendableScript(script []byte) bool {
	if len(script) == 0 {
		return true
	}
	if script[0] == 0x6a { // OP_RETURN
		return true
	}
	return false
}

// MakeOpReturnScript creates an OP_RETURN script with the given data.
// OP_RETURN outputs are provably unspendable.
func MakeOpReturnScript(data []byte) []byte {
	if len(data) > 80 {
		data = data[:80]
	}
	script := make([]byte, 0, 2+len(data))
	script = append(script, 0x6a) // OP_RETURN
	script = append(script, byte(len(data)))
	script = append(script, data...)
	return script
}

// SigHashAll is the sighash type that signs all inputs and outputs.
const SigHashAll = 0x01

// ComputeSigHash computes the hash that is signed for input inputIdx of tx.
// Uses SIGHASH_ALL: all inputs and outputs are committed.
// The algorithm matches Bitcoin's original sighash:
//  1. Copy the transaction
//  2. Clear all input scripts
//  3. Set the script of the input being signed to the subscript (prevout's PkScript)
//  4. Serialize and double-SHA256
func ComputeSigHash(tx *types.Transaction, inputIdx int, subscript []byte) (types.Hash, error) {
	if inputIdx < 0 || inputIdx >= len(tx.Inputs) {
		return types.ZeroHash, fmt.Errorf("input index %d out of range [0, %d)", inputIdx, len(tx.Inputs))
	}

	txCopy := types.Transaction{
		Version:  tx.Version,
		Inputs:   make([]types.TxInput, len(tx.Inputs)),
		Outputs:  make([]types.TxOutput, len(tx.Outputs)),
		LockTime: tx.LockTime,
	}

	for i := range tx.Inputs {
		txCopy.Inputs[i] = types.TxInput{
			PreviousOutPoint: tx.Inputs[i].PreviousOutPoint,
			Sequence:         tx.Inputs[i].Sequence,
		}
		if i == inputIdx {
			txCopy.Inputs[i].SignatureScript = subscript
		}
	}

	for i := range tx.Outputs {
		txCopy.Outputs[i] = tx.Outputs[i]
	}

	data, err := txCopy.SerializeToBytes()
	if err != nil {
		return types.ZeroHash, fmt.Errorf("serialize sighash copy: %w", err)
	}

	// Append sighash type as 4-byte LE (Bitcoin convention).
	var hashTypeBuf [4]byte
	hashTypeBuf[0] = SigHashAll
	data = append(data, hashTypeBuf[:]...)

	return DoubleSHA256(data), nil
}
