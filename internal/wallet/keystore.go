// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package wallet

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// KeyStore manages a single mining keypair on disk.
// The private key is stored as hex in a file with restricted permissions.
type KeyStore struct {
	dir     string
	privKey *secp256k1.PrivateKey
	pubKey  []byte // compressed
}

// LoadOrCreate loads an existing keypair from the data directory,
// or generates a new one if none exists.
func LoadOrCreate(dataDir string) (*KeyStore, error) {
	ks := &KeyStore{dir: dataDir}
	keyPath := ks.keyPath()

	data, err := os.ReadFile(keyPath)
	if err == nil && len(data) > 0 {
		privBytes, err := hex.DecodeString(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, fmt.Errorf("decode private key: %w", err)
		}
		priv, err := crypto.PrivKeyFromBytes(privBytes)
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		ks.privKey = priv
		ks.pubKey = priv.PubKey().SerializeCompressed()
		return ks, nil
	}

	privBytes, pubBytes, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("generate keypair: %w", err)
	}

	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create key directory: %w", err)
	}

	hexKey := hex.EncodeToString(privBytes)
	if err := os.WriteFile(keyPath, []byte(hexKey), 0600); err != nil {
		return nil, fmt.Errorf("write private key: %w", err)
	}

	priv, _ := crypto.PrivKeyFromBytes(privBytes)
	ks.privKey = priv
	ks.pubKey = pubBytes
	return ks, nil
}

// PrivateKey returns the secp256k1 private key.
func (ks *KeyStore) PrivateKey() *secp256k1.PrivateKey {
	return ks.privKey
}

// CompressedPubKey returns the 33-byte compressed public key.
func (ks *KeyStore) CompressedPubKey() []byte {
	return ks.pubKey
}

// PubKeyHash returns the Hash160 of the compressed public key.
func (ks *KeyStore) PubKeyHash() [crypto.PubKeyHashSize]byte {
	return crypto.PubKeyHash(ks.pubKey)
}

// P2PKHScript returns the standard P2PKH locking script for this key.
func (ks *KeyStore) P2PKHScript() []byte {
	hash := ks.PubKeyHash()
	return crypto.MakeP2PKHScript(hash)
}

func (ks *KeyStore) keyPath() string {
	return filepath.Join(ks.dir, "mining_key.hex")
}
