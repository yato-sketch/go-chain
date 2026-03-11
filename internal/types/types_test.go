package types

import (
	"bytes"
	"testing"
)

func TestHashFromBytesRoundtrip(t *testing.T) {
	data := make([]byte, HashSize)
	for i := range data {
		data[i] = byte(i)
	}
	h := HashFromBytes(data)
	if !bytes.Equal(h[:], data) {
		t.Fatal("HashFromBytes did not preserve bytes")
	}
}

func TestHashFromHex(t *testing.T) {
	hexStr := "000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f"
	h, err := HashFromHex(hexStr)
	if err != nil {
		t.Fatalf("HashFromHex error: %v", err)
	}
	if h.String() != hexStr {
		t.Fatalf("String() = %s, want %s", h.String(), hexStr)
	}
}

func TestHashIsZero(t *testing.T) {
	if !ZeroHash.IsZero() {
		t.Fatal("ZeroHash should be zero")
	}
	h := Hash{1}
	if h.IsZero() {
		t.Fatal("non-zero hash reported as zero")
	}
}

func TestHashLess(t *testing.T) {
	a := Hash{}
	b := Hash{}
	b[31] = 1 // b is larger (most significant byte in LE is at index 31)

	if !a.Less(b) {
		t.Fatal("a should be less than b")
	}
	if b.Less(a) {
		t.Fatal("b should not be less than a")
	}
	if a.Less(a) {
		t.Fatal("a should not be less than itself")
	}
}

func TestVarIntRoundtrip(t *testing.T) {
	values := []uint64{0, 1, 0xFC, 0xFD, 0xFFFF, 0x10000, 0xFFFFFFFF, 0x100000000, 0xFFFFFFFFFFFFFFFF}
	for _, v := range values {
		var buf bytes.Buffer
		if err := WriteVarInt(&buf, v); err != nil {
			t.Fatalf("WriteVarInt(%d): %v", v, err)
		}
		got, err := ReadVarInt(&buf)
		if err != nil {
			t.Fatalf("ReadVarInt for %d: %v", v, err)
		}
		if got != v {
			t.Fatalf("VarInt roundtrip: got %d, want %d", got, v)
		}
	}
}

func TestVarIntSize(t *testing.T) {
	tests := []struct {
		val  uint64
		size int
	}{
		{0, 1}, {0xFC, 1}, {0xFD, 3}, {0xFFFF, 3},
		{0x10000, 5}, {0xFFFFFFFF, 5}, {0x100000000, 9},
	}
	for _, tt := range tests {
		if got := VarIntSize(tt.val); got != tt.size {
			t.Errorf("VarIntSize(%d) = %d, want %d", tt.val, got, tt.size)
		}
	}
}

func TestTransactionSerializeRoundtrip(t *testing.T) {
	tx := Transaction{
		Version: 1,
		Inputs: []TxInput{
			{
				PreviousOutPoint: CoinbaseOutPoint,
				SignatureScript:  []byte("test coinbase"),
				Sequence:         0xFFFFFFFF,
			},
		},
		Outputs: []TxOutput{
			{
				Value:    5000000000,
				PkScript: []byte{0x00, 0x14, 0xAB, 0xCD},
			},
		},
		LockTime: 0,
	}

	data, err := tx.SerializeToBytes()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}

	var tx2 Transaction
	if err := tx2.Deserialize(bytes.NewReader(data)); err != nil {
		t.Fatalf("Deserialize: %v", err)
	}

	data2, err := tx2.SerializeToBytes()
	if err != nil {
		t.Fatalf("Serialize2: %v", err)
	}

	if !bytes.Equal(data, data2) {
		t.Fatal("Transaction serialization is not deterministic")
	}
}

func TestTransactionIsCoinbase(t *testing.T) {
	cb := Transaction{
		Version: 1,
		Inputs: []TxInput{
			{PreviousOutPoint: CoinbaseOutPoint, SignatureScript: []byte("cb"), Sequence: 0xFFFFFFFF},
		},
		Outputs: []TxOutput{{Value: 100, PkScript: []byte{0x00}}},
	}
	if !cb.IsCoinbase() {
		t.Fatal("expected coinbase")
	}

	regular := Transaction{
		Version: 1,
		Inputs: []TxInput{
			{PreviousOutPoint: OutPoint{Hash: Hash{1}, Index: 0}, SignatureScript: []byte("sig"), Sequence: 0xFFFFFFFF},
		},
		Outputs: []TxOutput{{Value: 50, PkScript: []byte{0x00}}},
	}
	if regular.IsCoinbase() {
		t.Fatal("regular tx should not be coinbase")
	}
}

func TestBlockHeaderSerializeRoundtrip(t *testing.T) {
	hdr := BlockHeader{
		Version:    1,
		PrevBlock:  Hash{0x01, 0x02, 0x03},
		MerkleRoot: Hash{0xAA, 0xBB},
		Timestamp:  1700000000,
		Bits:       0x1d00ffff,
		Nonce:      42,
	}

	data := hdr.SerializeToBytes()
	if len(data) != BlockHeaderSize {
		t.Fatalf("header size = %d, want %d", len(data), BlockHeaderSize)
	}

	var hdr2 BlockHeader
	if err := hdr2.Deserialize(bytes.NewReader(data)); err != nil {
		t.Fatalf("Deserialize: %v", err)
	}

	data2 := hdr2.SerializeToBytes()
	if !bytes.Equal(data, data2) {
		t.Fatal("BlockHeader serialization is not deterministic")
	}

	if hdr2.Version != hdr.Version || hdr2.Timestamp != hdr.Timestamp ||
		hdr2.Bits != hdr.Bits || hdr2.Nonce != hdr.Nonce ||
		hdr2.PrevBlock != hdr.PrevBlock || hdr2.MerkleRoot != hdr.MerkleRoot {
		t.Fatal("BlockHeader fields mismatch after roundtrip")
	}
}

func TestBlockSerializeRoundtrip(t *testing.T) {
	block := Block{
		Header: BlockHeader{
			Version:   1,
			Timestamp: 1700000000,
			Bits:      0x207fffff,
			Nonce:     123,
		},
		Transactions: []Transaction{
			{
				Version: 1,
				Inputs: []TxInput{
					{PreviousOutPoint: CoinbaseOutPoint, SignatureScript: []byte("genesis"), Sequence: 0xFFFFFFFF},
				},
				Outputs: []TxOutput{
					{Value: 5000000000, PkScript: []byte{0x00}},
				},
			},
		},
	}

	data, err := block.SerializeToBytes()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}

	var block2 Block
	if err := block2.Deserialize(bytes.NewReader(data)); err != nil {
		t.Fatalf("Deserialize: %v", err)
	}

	data2, err := block2.SerializeToBytes()
	if err != nil {
		t.Fatalf("Serialize2: %v", err)
	}

	if !bytes.Equal(data, data2) {
		t.Fatal("Block serialization is not deterministic")
	}
}
