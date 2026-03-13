package crypto

import (
	"fmt"

	"github.com/bams-repo/fairchain/internal/types"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// SignTransaction signs a specific input of a transaction using SIGHASH_ALL.
// Returns the DER-encoded signature with the sighash type byte appended.
func SignTransaction(tx *types.Transaction, inputIdx int, subscript []byte, privKey *secp256k1.PrivateKey) ([]byte, error) {
	sigHash, err := ComputeSigHash(tx, inputIdx, subscript)
	if err != nil {
		return nil, fmt.Errorf("compute sighash: %w", err)
	}

	sig := ecdsa.Sign(privKey, sigHash[:])
	derSig := sig.Serialize()

	// Append SIGHASH_ALL type byte.
	return append(derSig, SigHashAll), nil
}

// VerifySignature verifies a DER-encoded signature (with sighash type byte)
// against the computed sighash for the given input.
func VerifySignature(tx *types.Transaction, inputIdx int, subscript []byte, sigWithHashType []byte, pubKey *secp256k1.PublicKey) error {
	if len(sigWithHashType) < 2 {
		return fmt.Errorf("signature too short: %d bytes", len(sigWithHashType))
	}

	hashType := sigWithHashType[len(sigWithHashType)-1]
	if hashType != SigHashAll {
		return fmt.Errorf("unsupported sighash type: 0x%02x (only SIGHASH_ALL=0x01 supported)", hashType)
	}

	derSig := sigWithHashType[:len(sigWithHashType)-1]
	sig, err := ecdsa.ParseDERSignature(derSig)
	if err != nil {
		return fmt.Errorf("parse DER signature: %w", err)
	}

	sigHash, err := ComputeSigHash(tx, inputIdx, subscript)
	if err != nil {
		return fmt.Errorf("compute sighash: %w", err)
	}

	if !sig.Verify(sigHash[:], pubKey) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

// SignInput is a convenience that signs a transaction input and builds the
// complete P2PKH signature script (sig + pubkey).
func SignInput(tx *types.Transaction, inputIdx int, prevPkScript []byte, privKey *secp256k1.PrivateKey) ([]byte, error) {
	sig, err := SignTransaction(tx, inputIdx, prevPkScript, privKey)
	if err != nil {
		return nil, err
	}
	pubKey := privKey.PubKey().SerializeCompressed()
	return MakeP2PKHSignatureScript(sig, pubKey), nil
}
