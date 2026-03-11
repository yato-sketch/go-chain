package types

import (
	"bytes"
	"testing"
)

func FuzzBlockDeserialize(f *testing.F) {
	// Seed with a valid block.
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
				Inputs:  []TxInput{{PreviousOutPoint: CoinbaseOutPoint, SignatureScript: []byte("cb"), Sequence: 0xFFFFFFFF}},
				Outputs: []TxOutput{{Value: 5000000000, PkScript: []byte{0x00}}},
			},
		},
	}
	data, _ := block.SerializeToBytes()
	f.Add(data)

	// Seed with empty data and truncated variants.
	f.Add([]byte{})
	f.Add(data[:40])
	f.Add(data[:80])

	f.Fuzz(func(t *testing.T, input []byte) {
		var b Block
		err := b.Deserialize(bytes.NewReader(input))
		if err != nil {
			return
		}
		// If deserialization succeeded, re-serialization must not panic.
		_, _ = b.SerializeToBytes()
	})
}

func FuzzTransactionDeserialize(f *testing.F) {
	tx := Transaction{
		Version: 1,
		Inputs:  []TxInput{{PreviousOutPoint: CoinbaseOutPoint, SignatureScript: []byte("test"), Sequence: 0xFFFFFFFF}},
		Outputs: []TxOutput{{Value: 100, PkScript: []byte{0x01, 0x02}}},
	}
	data, _ := tx.SerializeToBytes()
	f.Add(data)
	f.Add([]byte{})
	f.Add(data[:4])

	f.Fuzz(func(t *testing.T, input []byte) {
		var tx2 Transaction
		err := tx2.Deserialize(bytes.NewReader(input))
		if err != nil {
			return
		}
		_, _ = tx2.SerializeToBytes()
	})
}

func FuzzBlockHeaderDeserialize(f *testing.F) {
	hdr := BlockHeader{Version: 1, Timestamp: 1700000000, Bits: 0x1d00ffff, Nonce: 42}
	data := hdr.SerializeToBytes()
	f.Add(data)
	f.Add([]byte{})
	f.Add(make([]byte, 80))

	f.Fuzz(func(t *testing.T, input []byte) {
		var h BlockHeader
		err := h.Deserialize(bytes.NewReader(input))
		if err != nil {
			return
		}
		_ = h.SerializeToBytes()
	})
}

func FuzzVarIntRoundtrip(f *testing.F) {
	f.Add([]byte{0x00})
	f.Add([]byte{0xFC})
	f.Add([]byte{0xFD, 0x00, 0x01})
	f.Add([]byte{0xFE, 0x00, 0x01, 0x00, 0x00})
	f.Add([]byte{0xFF, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})

	f.Fuzz(func(t *testing.T, input []byte) {
		val, err := ReadVarInt(bytes.NewReader(input))
		if err != nil {
			return
		}
		var buf bytes.Buffer
		if err := WriteVarInt(&buf, val); err != nil {
			t.Fatalf("WriteVarInt failed for decoded value %d: %v", val, err)
		}
		val2, err := ReadVarInt(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("ReadVarInt failed on re-encoded data: %v", err)
		}
		if val != val2 {
			t.Fatalf("VarInt roundtrip mismatch: %d != %d", val, val2)
		}
	})
}
