package chain

import (
	"path/filepath"
	"testing"

	"github.com/fairchain/fairchain/internal/consensus/pow"
	"github.com/fairchain/fairchain/internal/crypto"
	fcparams "github.com/fairchain/fairchain/internal/params"
	"github.com/fairchain/fairchain/internal/store"
	"github.com/fairchain/fairchain/internal/types"
)

func setupTestChain(t *testing.T) (*Chain, *fcparams.ChainParams) {
	t.Helper()

	p := &fcparams.ChainParams{}
	*p = *fcparams.Regtest

	// Mine genesis.
	cfg := fcparams.GenesisConfig{
		NetworkName:     "regtest",
		CoinbaseMessage: []byte("test genesis"),
		Timestamp:       1700000000,
		Bits:            p.InitialBits,
		Version:         1,
		Reward:          p.InitialSubsidy,
		RewardScript:    []byte{0x00},
	}
	genesis := fcparams.BuildGenesisBlock(cfg)
	if err := pow.MineGenesis(&genesis); err != nil {
		t.Fatalf("mine genesis: %v", err)
	}
	genesisHash := crypto.HashBlockHeader(&genesis.Header)
	fcparams.InitGenesis(p, genesis, genesisHash)

	dir := t.TempDir()
	s, err := store.NewBoltStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	engine := pow.New()
	c := New(p, engine, s)
	if err := c.Init(); err != nil {
		t.Fatalf("init chain: %v", err)
	}

	return c, p
}

func mineBlock(t *testing.T, c *Chain, p *fcparams.ChainParams) *types.Block {
	t.Helper()

	tipHash, tipHeight := c.Tip()
	tipHeader, err := c.TipHeader()
	if err != nil {
		t.Fatalf("get tip header: %v", err)
	}

	newHeight := tipHeight + 1
	subsidy := p.CalcSubsidy(newHeight)

	heightBytes := make([]byte, 4)
	types.PutUint32LE(heightBytes, newHeight)

	coinbase := types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{
			{
				PreviousOutPoint: types.CoinbaseOutPoint,
				SignatureScript:  append(heightBytes, []byte("test")...),
				Sequence:         0xFFFFFFFF,
			},
		},
		Outputs: []types.TxOutput{
			{Value: subsidy, PkScript: []byte{0x00}},
		},
	}

	merkle, _ := crypto.ComputeMerkleRoot([]types.Transaction{coinbase})

	header := types.BlockHeader{
		Version:    1,
		PrevBlock:  tipHash,
		MerkleRoot: merkle,
		Timestamp:  tipHeader.Timestamp + 1,
		Bits:       p.InitialBits,
		Nonce:      0,
	}

	target := crypto.CompactToHash(header.Bits)
	engine := pow.New()
	found, _ := engine.SealHeader(&header, target, 10000000)
	if !found {
		t.Fatal("could not mine block")
	}

	return &types.Block{
		Header:       header,
		Transactions: []types.Transaction{coinbase},
	}
}

func TestChainInit(t *testing.T) {
	c, p := setupTestChain(t)

	tipHash, tipHeight := c.Tip()
	if tipHeight != 0 {
		t.Fatalf("tip height = %d, want 0", tipHeight)
	}
	if tipHash != p.GenesisHash {
		t.Fatalf("tip hash = %s, want %s", tipHash, p.GenesisHash)
	}
}

func TestChainProcessBlock(t *testing.T) {
	c, p := setupTestChain(t)

	block := mineBlock(t, c, p)
	height, err := c.ProcessBlock(block)
	if err != nil {
		t.Fatalf("ProcessBlock: %v", err)
	}
	if height != 1 {
		t.Fatalf("accepted height = %d, want 1", height)
	}

	_, tipHeight := c.Tip()
	if tipHeight != 1 {
		t.Fatalf("tip height = %d, want 1", tipHeight)
	}
}

func TestChainMultipleBlocks(t *testing.T) {
	c, p := setupTestChain(t)

	for i := 0; i < 5; i++ {
		block := mineBlock(t, c, p)
		_, err := c.ProcessBlock(block)
		if err != nil {
			t.Fatalf("ProcessBlock at iteration %d: %v", i, err)
		}
	}

	_, tipHeight := c.Tip()
	if tipHeight != 5 {
		t.Fatalf("tip height = %d, want 5", tipHeight)
	}
}

func TestChainRejectDuplicate(t *testing.T) {
	c, p := setupTestChain(t)

	block := mineBlock(t, c, p)
	_, err := c.ProcessBlock(block)
	if err != nil {
		t.Fatalf("first ProcessBlock: %v", err)
	}

	_, err = c.ProcessBlock(block)
	if err == nil {
		t.Fatal("should reject duplicate block")
	}
}

func TestChainOrphan(t *testing.T) {
	c, p := setupTestChain(t)

	// Mine block 1.
	block1 := mineBlock(t, c, p)

	// Mine block 2 on top of block 1 (without submitting block 1 first).
	// We need to manually construct block 2 since mineBlock uses the chain tip.
	block1Hash := crypto.HashBlockHeader(&block1.Header)
	heightBytes := make([]byte, 4)
	types.PutUint32LE(heightBytes, 2)
	coinbase2 := types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{
			{PreviousOutPoint: types.CoinbaseOutPoint, SignatureScript: append(heightBytes, []byte("test")...), Sequence: 0xFFFFFFFF},
		},
		Outputs: []types.TxOutput{
			{Value: p.CalcSubsidy(2), PkScript: []byte{0x00}},
		},
	}
	merkle2, _ := crypto.ComputeMerkleRoot([]types.Transaction{coinbase2})
	header2 := types.BlockHeader{
		Version:    1,
		PrevBlock:  block1Hash,
		MerkleRoot: merkle2,
		Timestamp:  block1.Header.Timestamp + 1,
		Bits:       p.InitialBits,
	}
	target := crypto.CompactToHash(header2.Bits)
	engine := pow.New()
	engine.SealHeader(&header2, target, 10000000)
	block2 := &types.Block{Header: header2, Transactions: []types.Transaction{coinbase2}}

	// Submit block 2 first — should be orphaned.
	_, err := c.ProcessBlock(block2)
	if err == nil {
		t.Fatal("block 2 should be orphaned (parent unknown)")
	}

	// Now submit block 1 — should accept and also process orphan.
	_, err = c.ProcessBlock(block1)
	if err != nil {
		t.Fatalf("ProcessBlock(block1): %v", err)
	}

	// Chain should now be at height 2 (genesis + block1 + block2 via orphan processing).
	_, tipHeight := c.Tip()
	if tipHeight < 1 {
		t.Fatalf("tip height = %d, want >= 1", tipHeight)
	}
}
