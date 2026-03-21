// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package crypto

import (
	"bytes"
	"testing"
)

func TestGenerateKeyPair(t *testing.T) {
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if len(priv) != PrivKeySize {
		t.Fatalf("private key size: got %d, want %d", len(priv), PrivKeySize)
	}
	if len(pub) != CompressedPubSize {
		t.Fatalf("public key size: got %d, want %d", len(pub), CompressedPubSize)
	}
	if pub[0] != 0x02 && pub[0] != 0x03 {
		t.Fatalf("compressed pubkey prefix: got 0x%02x, want 0x02 or 0x03", pub[0])
	}
}

func TestKeyPairDeterminism(t *testing.T) {
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	key, err := PrivKeyFromBytes(priv)
	if err != nil {
		t.Fatal(err)
	}
	pub2 := key.PubKey().SerializeCompressed()
	if !bytes.Equal(pub, pub2) {
		t.Fatal("reconstructed pubkey doesn't match original")
	}
}

func TestPubKeyFromBytes(t *testing.T) {
	_, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := PubKeyFromBytes(pub)
	if err != nil {
		t.Fatalf("PubKeyFromBytes: %v", err)
	}
	if !bytes.Equal(parsed.SerializeCompressed(), pub) {
		t.Fatal("parsed pubkey doesn't match original")
	}
}

func TestPubKeyFromBytesInvalid(t *testing.T) {
	_, err := PubKeyFromBytes([]byte{0x00, 0x01})
	if err == nil {
		t.Fatal("expected error for invalid pubkey length")
	}
}

func TestHash160Deterministic(t *testing.T) {
	data := []byte("test data for hash160")
	h1 := Hash160(data)
	h2 := Hash160(data)
	if h1 != h2 {
		t.Fatal("Hash160 is not deterministic")
	}
}

func TestHash160DifferentInputs(t *testing.T) {
	h1 := Hash160([]byte("input1"))
	h2 := Hash160([]byte("input2"))
	if h1 == h2 {
		t.Fatal("different inputs should produce different Hash160")
	}
}

func TestPubKeyHash(t *testing.T) {
	_, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	hash := PubKeyHash(pub)
	if len(hash) != PubKeyHashSize {
		t.Fatalf("PubKeyHash size: got %d, want %d", len(hash), PubKeyHashSize)
	}

	expected := Hash160(pub)
	if hash != expected {
		t.Fatal("PubKeyHash doesn't match Hash160 of pubkey")
	}
}

func TestMakeP2PKHScript(t *testing.T) {
	_, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	hash := PubKeyHash(pub)
	script := MakeP2PKHScript(hash)

	if len(script) != 25 {
		t.Fatalf("P2PKH script length: got %d, want 25", len(script))
	}
	if script[0] != OpDup {
		t.Fatalf("script[0]: got 0x%02x, want OP_DUP (0x76)", script[0])
	}
	if script[1] != OpHash160 {
		t.Fatalf("script[1]: got 0x%02x, want OP_HASH160 (0xa9)", script[1])
	}
	if script[2] != 0x14 {
		t.Fatalf("script[2]: got 0x%02x, want 0x14 (push 20 bytes)", script[2])
	}
	if !bytes.Equal(script[3:23], hash[:]) {
		t.Fatal("pubkey hash not embedded correctly")
	}
	if script[23] != OpEqualVerify {
		t.Fatalf("script[23]: got 0x%02x, want OP_EQUALVERIFY (0x88)", script[23])
	}
	if script[24] != OpCheckSig {
		t.Fatalf("script[24]: got 0x%02x, want OP_CHECKSIG (0xac)", script[24])
	}
}

func TestIsP2PKHScript(t *testing.T) {
	_, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	hash := PubKeyHash(pub)
	script := MakeP2PKHScript(hash)

	if !IsP2PKHScript(script) {
		t.Fatal("valid P2PKH script not recognized")
	}
	if IsP2PKHScript([]byte{0x00}) {
		t.Fatal("single byte should not be P2PKH")
	}
	if IsP2PKHScript(nil) {
		t.Fatal("nil should not be P2PKH")
	}
}

func TestExtractP2PKHHash(t *testing.T) {
	_, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	hash := PubKeyHash(pub)
	script := MakeP2PKHScript(hash)

	extracted := ExtractP2PKHHash(script)
	if !bytes.Equal(extracted, hash[:]) {
		t.Fatal("extracted hash doesn't match original")
	}

	if ExtractP2PKHHash([]byte{0x00}) != nil {
		t.Fatal("non-P2PKH should return nil")
	}
}

func TestMakeP2PKHSignatureScript(t *testing.T) {
	sig := []byte{0x30, 0x44, 0x01} // fake DER sig
	pub := make([]byte, 33)
	pub[0] = 0x02

	sigScript := MakeP2PKHSignatureScript(sig, pub)

	if sigScript[0] != byte(len(sig)) {
		t.Fatalf("sig push byte: got %d, want %d", sigScript[0], len(sig))
	}
	if !bytes.Equal(sigScript[1:1+len(sig)], sig) {
		t.Fatal("sig not embedded correctly")
	}
	pubStart := 1 + len(sig)
	if sigScript[pubStart] != byte(len(pub)) {
		t.Fatalf("pub push byte: got %d, want %d", sigScript[pubStart], len(pub))
	}
	if !bytes.Equal(sigScript[pubStart+1:], pub) {
		t.Fatal("pubkey not embedded correctly")
	}
}

func TestMakeOpReturnScript(t *testing.T) {
	data := []byte("burn:testnet:premine:v1")
	script := MakeOpReturnScript(data)
	if script[0] != 0x6a {
		t.Fatalf("first byte should be OP_RETURN (0x6a), got 0x%02x", script[0])
	}
	if script[1] != byte(len(data)) {
		t.Fatalf("push byte: got %d, want %d", script[1], len(data))
	}
	if !bytes.Equal(script[2:], data) {
		t.Fatal("data not embedded correctly")
	}
}

func TestIsUnspendableScript(t *testing.T) {
	if !IsUnspendableScript(nil) {
		t.Fatal("empty script should be unspendable")
	}
	if !IsUnspendableScript([]byte{0x6a, 0x04, 0xde, 0xad}) {
		t.Fatal("OP_RETURN script should be unspendable")
	}
	if IsUnspendableScript([]byte{0x76, 0xa9}) {
		t.Fatal("P2PKH prefix should not be unspendable")
	}
}

func TestComputeSigHash(t *testing.T) {
	_, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pkScript := MakeP2PKHScriptFromPubKey(pub)

	tx := makeTestTx(pub)

	hash1, err := ComputeSigHash(&tx, 0, pkScript)
	if err != nil {
		t.Fatalf("ComputeSigHash: %v", err)
	}
	hash2, err := ComputeSigHash(&tx, 0, pkScript)
	if err != nil {
		t.Fatalf("ComputeSigHash: %v", err)
	}
	if hash1 != hash2 {
		t.Fatal("ComputeSigHash is not deterministic")
	}
	if hash1.IsZero() {
		t.Fatal("sighash should not be zero")
	}
}

func TestComputeSigHashDifferentInputs(t *testing.T) {
	_, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pkScript := MakeP2PKHScriptFromPubKey(pub)

	tx := makeTestTx(pub)

	hash1, err := ComputeSigHash(&tx, 0, pkScript)
	if err != nil {
		t.Fatal(err)
	}

	hash2, err := ComputeSigHash(&tx, 0, []byte{0x00})
	if err != nil {
		t.Fatal(err)
	}

	if hash1 == hash2 {
		t.Fatal("different subscripts should produce different sighashes")
	}
}
