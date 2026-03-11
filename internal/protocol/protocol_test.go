package protocol

import (
	"bytes"
	"testing"

	"github.com/bams-repo/fairchain/internal/types"
	"github.com/bams-repo/fairchain/internal/version"
)

func TestVersionMsgRoundtrip(t *testing.T) {
	msg := VersionMsg{
		Version:     1,
		Services:    1,
		Timestamp:   1700000000,
		AddrRecv:    "127.0.0.1:19333",
		AddrFrom:    "127.0.0.1:19334",
		Nonce:       12345,
		UserAgent:   version.UserAgent(),
		StartHeight: 100,
	}

	var buf bytes.Buffer
	if err := msg.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var msg2 VersionMsg
	if err := msg2.Decode(&buf); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if msg.Version != msg2.Version || msg.Services != msg2.Services ||
		msg.Timestamp != msg2.Timestamp || msg.AddrRecv != msg2.AddrRecv ||
		msg.AddrFrom != msg2.AddrFrom || msg.Nonce != msg2.Nonce ||
		msg.UserAgent != msg2.UserAgent || msg.StartHeight != msg2.StartHeight {
		t.Fatal("VersionMsg roundtrip mismatch")
	}
}

func TestInvMsgRoundtrip(t *testing.T) {
	msg := InvMsg{
		Inventory: []InvVector{
			{Type: InvTypeBlock, Hash: types.Hash{0x01}},
			{Type: InvTypeTx, Hash: types.Hash{0x02}},
		},
	}

	var buf bytes.Buffer
	if err := msg.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var msg2 InvMsg
	if err := msg2.Decode(&buf); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if len(msg2.Inventory) != 2 {
		t.Fatalf("expected 2 inv vectors, got %d", len(msg2.Inventory))
	}
	if msg2.Inventory[0].Type != InvTypeBlock || msg2.Inventory[0].Hash != msg.Inventory[0].Hash {
		t.Fatal("first inv vector mismatch")
	}
	if msg2.Inventory[1].Type != InvTypeTx || msg2.Inventory[1].Hash != msg.Inventory[1].Hash {
		t.Fatal("second inv vector mismatch")
	}
}

func TestGetBlocksMsgRoundtrip(t *testing.T) {
	msg := GetBlocksMsg{
		Version:            1,
		BlockLocatorHashes: []types.Hash{{0xAA}, {0xBB}},
		HashStop:           types.Hash{0xCC},
	}

	var buf bytes.Buffer
	if err := msg.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var msg2 GetBlocksMsg
	if err := msg2.Decode(&buf); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if msg2.Version != 1 || len(msg2.BlockLocatorHashes) != 2 || msg2.HashStop != msg.HashStop {
		t.Fatal("GetBlocksMsg roundtrip mismatch")
	}
}

func TestAddrMsgRoundtrip(t *testing.T) {
	msg := AddrMsg{Addresses: []string{"127.0.0.1:19333", "192.168.1.1:19334"}}

	var buf bytes.Buffer
	if err := msg.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var msg2 AddrMsg
	if err := msg2.Decode(&buf); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if len(msg2.Addresses) != 2 || msg2.Addresses[0] != "127.0.0.1:19333" {
		t.Fatal("AddrMsg roundtrip mismatch")
	}
}

func TestPingPongRoundtrip(t *testing.T) {
	ping := PingMsg{Nonce: 99999}
	var buf bytes.Buffer
	if err := ping.Encode(&buf); err != nil {
		t.Fatalf("Encode ping: %v", err)
	}

	var ping2 PingMsg
	if err := ping2.Decode(&buf); err != nil {
		t.Fatalf("Decode ping: %v", err)
	}
	if ping2.Nonce != 99999 {
		t.Fatal("ping nonce mismatch")
	}

	pong := PongMsg{Nonce: 99999}
	buf.Reset()
	pong.Encode(&buf)
	var pong2 PongMsg
	pong2.Decode(&buf)
	if pong2.Nonce != 99999 {
		t.Fatal("pong nonce mismatch")
	}
}

func TestMessageHeaderEncodeDecode(t *testing.T) {
	magic := [4]byte{0xFA, 0x1C, 0xC0, 0xFF}
	payload := []byte("test payload data")

	var buf bytes.Buffer
	if err := EncodeMessageHeader(&buf, magic, CmdVersion, payload); err != nil {
		t.Fatalf("EncodeMessageHeader: %v", err)
	}

	hdr, err := DecodeMessageHeader(&buf)
	if err != nil {
		t.Fatalf("DecodeMessageHeader: %v", err)
	}

	if hdr.Magic != magic {
		t.Fatal("magic mismatch")
	}
	if hdr.CommandString() != CmdVersion {
		t.Fatalf("command = %q, want %q", hdr.CommandString(), CmdVersion)
	}
	if hdr.Length != uint32(len(payload)) {
		t.Fatalf("length = %d, want %d", hdr.Length, len(payload))
	}
}

func TestMessageEncodingDeterministic(t *testing.T) {
	msg := VersionMsg{
		Version: 1, Services: 1, Timestamp: 1700000000,
		AddrRecv: "a", AddrFrom: "b", Nonce: 1,
		UserAgent: "test", StartHeight: 0,
	}

	var buf1, buf2 bytes.Buffer
	msg.Encode(&buf1)
	msg.Encode(&buf2)

	if !bytes.Equal(buf1.Bytes(), buf2.Bytes()) {
		t.Fatal("VersionMsg encoding is not deterministic")
	}
}
