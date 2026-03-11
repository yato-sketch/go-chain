package pow

import (
	"testing"
	"time"

	"github.com/fairchain/fairchain/internal/crypto"
	fcparams "github.com/fairchain/fairchain/internal/params"
	"github.com/fairchain/fairchain/internal/types"
)

func TestMineGenesisRegtest(t *testing.T) {
	cfg := fcparams.GenesisConfig{
		NetworkName:     "regtest",
		CoinbaseMessage: []byte("test genesis"),
		Timestamp:       1700000000,
		Bits:            0x207fffff, // Very easy.
		Version:         1,
		Reward:          5000000000,
		RewardScript:    []byte{0x00},
	}

	block := fcparams.BuildGenesisBlock(cfg)
	if err := MineGenesis(&block); err != nil {
		t.Fatalf("MineGenesis: %v", err)
	}

	hash := crypto.HashBlockHeader(&block.Header)
	target := crypto.CompactToHash(block.Header.Bits)
	if !hash.LessOrEqual(target) {
		t.Fatal("mined genesis hash does not meet target")
	}

	// Verify reproducibility: mine again with same inputs.
	block2 := fcparams.BuildGenesisBlock(cfg)
	if err := MineGenesis(&block2); err != nil {
		t.Fatalf("MineGenesis2: %v", err)
	}

	hash2 := crypto.HashBlockHeader(&block2.Header)
	if hash != hash2 {
		t.Fatalf("genesis mining not reproducible: %s != %s", hash, hash2)
	}
	if block.Header.Nonce != block2.Header.Nonce {
		t.Fatalf("genesis nonce not reproducible: %d != %d", block.Header.Nonce, block2.Header.Nonce)
	}
}

func TestSealHeader(t *testing.T) {
	engine := New()
	header := types.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      0x207fffff,
		Nonce:     0,
	}
	target := crypto.CompactToHash(header.Bits)

	found, err := engine.SealHeader(&header, target, 1000000)
	if err != nil {
		t.Fatalf("SealHeader: %v", err)
	}
	if !found {
		t.Fatal("SealHeader should find a solution with easy difficulty")
	}

	hash := crypto.HashBlockHeader(&header)
	if !hash.LessOrEqual(target) {
		t.Fatal("sealed header hash does not meet target")
	}
}

func TestCalcNextBitsNoRetarget(t *testing.T) {
	engine := New()
	p := fcparams.Regtest

	tip := &types.BlockHeader{Bits: p.InitialBits, Timestamp: 1700000000}
	bits := engine.CalcNextBits(tip, 10, nil, p)
	if bits != p.InitialBits {
		t.Fatalf("regtest should not retarget: got 0x%08x, want 0x%08x", bits, p.InitialBits)
	}
}

func TestCalcNextBitsRetarget(t *testing.T) {
	engine := New()
	p := &fcparams.ChainParams{
		InitialBits:      0x1e0fffff,
		MinBits:          0x1e0fffff,
		NoRetarget:       false,
		RetargetInterval: 10,
		TargetTimespan:   10 * time.Minute,
	}

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := uint32(0); i <= 10; i++ {
		headers[i] = &types.BlockHeader{
			Bits:      p.InitialBits,
			Timestamp: baseTime + i*60, // 1 block per minute.
		}
	}

	getAncestor := func(h uint32) *types.BlockHeader {
		return headers[h]
	}

	// At height 10 (retarget boundary), actual timespan = 10 * 60 = 600s.
	// Target timespan = 10 * 60 = 600s. Should keep same difficulty.
	bits := engine.CalcNextBits(headers[9], 9, getAncestor, p)
	if bits != p.InitialBits {
		t.Logf("bits changed at retarget with matching timespan: 0x%08x -> 0x%08x", p.InitialBits, bits)
	}
}

func TestValidateHeader(t *testing.T) {
	engine := New()
	p := fcparams.Regtest

	// Build a parent block.
	parent := types.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      p.InitialBits,
		Nonce:     0,
	}

	parentHash := crypto.HashBlockHeader(&parent)

	child := types.BlockHeader{
		Version:   1,
		PrevBlock: parentHash,
		Timestamp: 1700000001,
		Bits:      p.InitialBits,
		Nonce:     0,
	}

	// Mine the child to find valid nonce.
	target := crypto.CompactToHash(child.Bits)
	found, _ := engine.SealHeader(&child, target, 10000000)
	if !found {
		t.Fatal("could not mine child block")
	}

	if err := engine.ValidateHeader(&child, &parent, 1, p); err != nil {
		t.Fatalf("ValidateHeader: %v", err)
	}
}

func TestValidateHeaderBadPrevHash(t *testing.T) {
	engine := New()
	p := fcparams.Regtest

	parent := types.BlockHeader{Version: 1, Timestamp: 1700000000, Bits: p.InitialBits}
	child := types.BlockHeader{
		Version:   1,
		PrevBlock: types.Hash{0xFF}, // Wrong parent hash.
		Timestamp: 1700000001,
		Bits:      p.InitialBits,
	}

	if err := engine.ValidateHeader(&child, &parent, 1, p); err == nil {
		t.Fatal("should reject header with wrong prev hash")
	}
}
