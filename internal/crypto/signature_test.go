// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package crypto

import (
	"testing"

	"github.com/bams-repo/fairchain/internal/types"
)

func makeTestTx(compressedPubKey []byte) types.Transaction {
	return types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{{
			PreviousOutPoint: types.OutPoint{
				Hash:  DoubleSHA256([]byte("prev-tx")),
				Index: 0,
			},
			Sequence: 0xFFFFFFFF,
		}},
		Outputs: []types.TxOutput{{
			Value:    50000,
			PkScript: MakeP2PKHScriptFromPubKey(compressedPubKey),
		}},
		LockTime: 0,
	}
}

func TestSignAndVerify(t *testing.T) {
	privBytes, pubBytes, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privKey, err := PrivKeyFromBytes(privBytes)
	if err != nil {
		t.Fatal(err)
	}

	pkScript := MakeP2PKHScriptFromPubKey(pubBytes)
	tx := makeTestTx(pubBytes)

	sig, err := SignTransaction(&tx, 0, pkScript, privKey)
	if err != nil {
		t.Fatalf("SignTransaction: %v", err)
	}

	if sig[len(sig)-1] != SigHashAll {
		t.Fatalf("last byte should be SIGHASH_ALL, got 0x%02x", sig[len(sig)-1])
	}

	pubKey, err := PubKeyFromBytes(pubBytes)
	if err != nil {
		t.Fatal(err)
	}

	if err := VerifySignature(&tx, 0, pkScript, sig, pubKey); err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
}

func TestSignAndVerifyWrongKey(t *testing.T) {
	privBytes, pubBytes, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privKey, err := PrivKeyFromBytes(privBytes)
	if err != nil {
		t.Fatal(err)
	}

	pkScript := MakeP2PKHScriptFromPubKey(pubBytes)
	tx := makeTestTx(pubBytes)

	sig, err := SignTransaction(&tx, 0, pkScript, privKey)
	if err != nil {
		t.Fatal(err)
	}

	// Verify with a different key — should fail.
	_, wrongPubBytes, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	wrongPubKey, err := PubKeyFromBytes(wrongPubBytes)
	if err != nil {
		t.Fatal(err)
	}

	if err := VerifySignature(&tx, 0, pkScript, sig, wrongPubKey); err == nil {
		t.Fatal("expected verification to fail with wrong key")
	}
}

func TestSignAndVerifyTamperedTx(t *testing.T) {
	privBytes, pubBytes, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privKey, err := PrivKeyFromBytes(privBytes)
	if err != nil {
		t.Fatal(err)
	}

	pkScript := MakeP2PKHScriptFromPubKey(pubBytes)
	tx := makeTestTx(pubBytes)

	sig, err := SignTransaction(&tx, 0, pkScript, privKey)
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with the transaction output value.
	tx.Outputs[0].Value = 99999

	pubKey, err := PubKeyFromBytes(pubBytes)
	if err != nil {
		t.Fatal(err)
	}

	if err := VerifySignature(&tx, 0, pkScript, sig, pubKey); err == nil {
		t.Fatal("expected verification to fail after tx tampering")
	}
}

func TestSignInput(t *testing.T) {
	privBytes, pubBytes, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privKey, err := PrivKeyFromBytes(privBytes)
	if err != nil {
		t.Fatal(err)
	}

	pkScript := MakeP2PKHScriptFromPubKey(pubBytes)
	tx := makeTestTx(pubBytes)

	sigScript, err := SignInput(&tx, 0, pkScript, privKey)
	if err != nil {
		t.Fatalf("SignInput: %v", err)
	}

	// The signature script should contain: <len><sig><len><pubkey>
	if len(sigScript) < 2+CompressedPubSize {
		t.Fatalf("signature script too short: %d bytes", len(sigScript))
	}

	// Extract pubkey from the end.
	pubLen := sigScript[len(sigScript)-CompressedPubSize-1]
	if int(pubLen) != CompressedPubSize {
		t.Fatalf("pubkey push length: got %d, want %d", pubLen, CompressedPubSize)
	}
}

func TestVerifySignatureInvalidSighashType(t *testing.T) {
	privBytes, pubBytes, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privKey, err := PrivKeyFromBytes(privBytes)
	if err != nil {
		t.Fatal(err)
	}

	pkScript := MakeP2PKHScriptFromPubKey(pubBytes)
	tx := makeTestTx(pubBytes)

	sig, err := SignTransaction(&tx, 0, pkScript, privKey)
	if err != nil {
		t.Fatal(err)
	}

	// Change sighash type to something unsupported.
	sig[len(sig)-1] = 0x82

	pubKey, err := PubKeyFromBytes(pubBytes)
	if err != nil {
		t.Fatal(err)
	}

	if err := VerifySignature(&tx, 0, pkScript, sig, pubKey); err == nil {
		t.Fatal("expected error for unsupported sighash type")
	}
}
