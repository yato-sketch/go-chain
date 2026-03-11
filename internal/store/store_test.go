package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fairchain/fairchain/internal/types"
)

func TestBoltStoreBlockRoundtrip(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := NewBoltStore(dbPath)
	if err != nil {
		t.Fatalf("NewBoltStore: %v", err)
	}
	defer s.Close()
	defer os.Remove(dbPath)

	block := types.Block{
		Header: types.BlockHeader{
			Version:   1,
			Timestamp: 1700000000,
			Bits:      0x207fffff,
			Nonce:     42,
		},
		Transactions: []types.Transaction{
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.CoinbaseOutPoint, SignatureScript: []byte("test"), Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{
					{Value: 5000000000, PkScript: []byte{0x00}},
				},
			},
		},
	}

	hash := types.Hash{0x01, 0x02, 0x03}

	// Store block.
	if err := s.PutBlock(hash, &block); err != nil {
		t.Fatalf("PutBlock: %v", err)
	}

	// Check existence.
	has, err := s.HasBlock(hash)
	if err != nil {
		t.Fatalf("HasBlock: %v", err)
	}
	if !has {
		t.Fatal("block should exist after PutBlock")
	}

	// Retrieve block.
	got, err := s.GetBlock(hash)
	if err != nil {
		t.Fatalf("GetBlock: %v", err)
	}
	if got.Header.Nonce != 42 {
		t.Fatalf("nonce = %d, want 42", got.Header.Nonce)
	}
	if len(got.Transactions) != 1 {
		t.Fatalf("tx count = %d, want 1", len(got.Transactions))
	}

	// Retrieve header.
	hdr, err := s.GetHeader(hash)
	if err != nil {
		t.Fatalf("GetHeader: %v", err)
	}
	if hdr.Nonce != 42 {
		t.Fatalf("header nonce = %d, want 42", hdr.Nonce)
	}
}

func TestBoltStoreChainTip(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := NewBoltStore(dbPath)
	if err != nil {
		t.Fatalf("NewBoltStore: %v", err)
	}
	defer s.Close()

	hash := types.Hash{0xAA, 0xBB}
	height := uint32(100)

	if err := s.PutChainTip(hash, height); err != nil {
		t.Fatalf("PutChainTip: %v", err)
	}

	gotHash, gotHeight, err := s.GetChainTip()
	if err != nil {
		t.Fatalf("GetChainTip: %v", err)
	}
	if gotHash != hash {
		t.Fatalf("tip hash = %s, want %s", gotHash, hash)
	}
	if gotHeight != height {
		t.Fatalf("tip height = %d, want %d", gotHeight, height)
	}
}

func TestBoltStoreBlockIndex(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := NewBoltStore(dbPath)
	if err != nil {
		t.Fatalf("NewBoltStore: %v", err)
	}
	defer s.Close()

	hash := types.Hash{0x11, 0x22}
	height := uint32(5)

	if err := s.PutBlockIndex(hash, height); err != nil {
		t.Fatalf("PutBlockIndex: %v", err)
	}

	gotHash, err := s.GetBlockByHeight(height)
	if err != nil {
		t.Fatalf("GetBlockByHeight: %v", err)
	}
	if gotHash != hash {
		t.Fatalf("hash at height %d = %s, want %s", height, gotHash, hash)
	}
}

func TestBoltStorePeers(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := NewBoltStore(dbPath)
	if err != nil {
		t.Fatalf("NewBoltStore: %v", err)
	}
	defer s.Close()

	if err := s.PutPeer("127.0.0.1:19333"); err != nil {
		t.Fatalf("PutPeer: %v", err)
	}
	if err := s.PutPeer("192.168.1.1:19333"); err != nil {
		t.Fatalf("PutPeer: %v", err)
	}

	peers, err := s.GetPeers()
	if err != nil {
		t.Fatalf("GetPeers: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("peer count = %d, want 2", len(peers))
	}

	if err := s.RemovePeer("127.0.0.1:19333"); err != nil {
		t.Fatalf("RemovePeer: %v", err)
	}

	peers, err = s.GetPeers()
	if err != nil {
		t.Fatalf("GetPeers after remove: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("peer count after remove = %d, want 1", len(peers))
	}
}

func TestBoltStoreNonExistent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := NewBoltStore(dbPath)
	if err != nil {
		t.Fatalf("NewBoltStore: %v", err)
	}
	defer s.Close()

	has, err := s.HasBlock(types.Hash{0xFF})
	if err != nil {
		t.Fatalf("HasBlock: %v", err)
	}
	if has {
		t.Fatal("non-existent block should not exist")
	}

	_, err = s.GetBlock(types.Hash{0xFF})
	if err == nil {
		t.Fatal("GetBlock should error for non-existent block")
	}

	_, _, err = s.GetChainTip()
	if err == nil {
		t.Fatal("GetChainTip should error when not set")
	}
}
