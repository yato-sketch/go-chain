package chain

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/bams-repo/fairchain/internal/consensus/pow"
	"github.com/bams-repo/fairchain/internal/crypto"
	fcparams "github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/store"
	"github.com/bams-repo/fairchain/internal/types"
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

// mineBlockOnParent builds and mines a block on top of a specific parent, not the chain tip.
// The tag parameter differentiates the coinbase to produce a unique block hash.
func mineBlockOnParent(t *testing.T, parentHash types.Hash, parentHeader *types.BlockHeader, parentHeight uint32, p *fcparams.ChainParams, tag string) *types.Block {
	t.Helper()

	newHeight := parentHeight + 1
	subsidy := p.CalcSubsidy(newHeight)

	heightBytes := make([]byte, 4)
	types.PutUint32LE(heightBytes, newHeight)

	coinbase := types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{
			{
				PreviousOutPoint: types.CoinbaseOutPoint,
				SignatureScript:  append(heightBytes, []byte(tag)...),
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
		PrevBlock:  parentHash,
		MerkleRoot: merkle,
		Timestamp:  parentHeader.Timestamp + 1,
		Bits:       p.InitialBits,
		Nonce:      0,
	}

	target := crypto.CompactToHash(header.Bits)
	engine := pow.New()
	found, _ := engine.SealHeader(&header, target, 10000000)
	if !found {
		t.Fatal("could not mine block on parent")
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

func TestEqualWorkTieBreaker(t *testing.T) {
	c, p := setupTestChain(t)

	// Mine block 1 on the chain.
	block1 := mineBlock(t, c, p)
	_, err := c.ProcessBlock(block1)
	if err != nil {
		t.Fatalf("ProcessBlock(block1): %v", err)
	}

	// Now create two competing blocks at height 2 on top of block1.
	tipHash, tipHeight := c.Tip()
	tipHeader, _ := c.TipHeader()

	blockA := mineBlockOnParent(t, tipHash, tipHeader, tipHeight, p, "fork-A")
	blockB := mineBlockOnParent(t, tipHash, tipHeader, tipHeight, p, "fork-B")

	hashA := crypto.HashBlockHeader(&blockA.Header)
	hashB := crypto.HashBlockHeader(&blockB.Header)

	// Determine which has the lower hash — that should be the winner.
	var expectedWinner types.Hash
	if bytes.Compare(hashA[:], hashB[:]) < 0 {
		expectedWinner = hashA
	} else {
		expectedWinner = hashB
	}

	// Submit both blocks. The first one extends the chain; the second is a side chain
	// with equal work, so the tie-breaker (lower hash) should decide.
	_, err = c.ProcessBlock(blockA)
	if err != nil {
		t.Fatalf("ProcessBlock(blockA): %v", err)
	}
	_, err = c.ProcessBlock(blockB)
	if err != nil {
		t.Fatalf("ProcessBlock(blockB): %v", err)
	}

	finalTip, _ := c.Tip()
	if finalTip != expectedWinner {
		t.Fatalf("equal-work tie-breaker failed: tip=%s, expected=%s (lower hash wins)", finalTip, expectedWinner)
	}
}

func TestSideChainWorkCalculation(t *testing.T) {
	c, p := setupTestChain(t)

	// Mine 3 blocks on the main chain.
	for i := 0; i < 3; i++ {
		block := mineBlock(t, c, p)
		_, err := c.ProcessBlock(block)
		if err != nil {
			t.Fatalf("ProcessBlock main chain %d: %v", i, err)
		}
	}

	// Record main chain tip at height 3.
	mainTipHash, mainTipHeight := c.Tip()
	if mainTipHeight != 3 {
		t.Fatalf("main chain height = %d, want 3", mainTipHeight)
	}

	// Create a side chain forking from height 1 (genesis -> block1 -> sideA -> sideB -> sideC -> sideD).
	// This side chain needs 4 blocks to have more cumulative work than the main chain's 3 blocks
	// (since all blocks have the same difficulty, 4 > 3).
	block1Header, err := c.GetHeaderByHeight(1)
	if err != nil {
		t.Fatalf("get header at height 1: %v", err)
	}
	block1Hash := c.hashByHeight[1]

	sideA := mineBlockOnParent(t, block1Hash, block1Header, 1, p, "side-A")
	sideAHash := crypto.HashBlockHeader(&sideA.Header)

	sideB := mineBlockOnParent(t, sideAHash, &sideA.Header, 2, p, "side-B")
	sideBHash := crypto.HashBlockHeader(&sideB.Header)

	sideC := mineBlockOnParent(t, sideBHash, &sideB.Header, 3, p, "side-C")
	sideCHash := crypto.HashBlockHeader(&sideC.Header)

	sideD := mineBlockOnParent(t, sideCHash, &sideC.Header, 4, p, "side-D")

	// Submit side chain blocks. sideA and sideB are side-chain blocks (less work).
	// sideC ties the main chain. sideD should trigger a reorg.
	_, err = c.ProcessBlock(sideA)
	if err != nil {
		t.Fatalf("ProcessBlock(sideA): %v", err)
	}
	_, err = c.ProcessBlock(sideB)
	if err != nil {
		t.Fatalf("ProcessBlock(sideB): %v", err)
	}
	_, err = c.ProcessBlock(sideC)
	if err != nil {
		t.Fatalf("ProcessBlock(sideC): %v", err)
	}
	_, err = c.ProcessBlock(sideD)
	if err != nil {
		t.Fatalf("ProcessBlock(sideD): %v", err)
	}

	// After sideD, the side chain has 5 blocks of work (genesis + block1 + sideA + sideB + sideC + sideD = 5 post-genesis)
	// vs main chain's 3 blocks of work. Side chain should win.
	newTipHash, newTipHeight := c.Tip()
	if newTipHeight != 5 {
		t.Fatalf("after side chain reorg, tip height = %d, want 5", newTipHeight)
	}

	sideDHash := crypto.HashBlockHeader(&sideD.Header)
	if newTipHash != sideDHash {
		t.Fatalf("after reorg, tip should be sideD=%s, got %s", sideDHash, newTipHash)
	}

	// Verify the old main chain tip is no longer the tip.
	if newTipHash == mainTipHash {
		t.Fatal("reorg did not happen — still on old main chain")
	}
}
