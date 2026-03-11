package chain

import (
	"bytes"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/bams-repo/fairchain/internal/consensus"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/metrics"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/store"
	"github.com/bams-repo/fairchain/internal/types"
)

// MaxOrphanBlocks limits the orphan block holding area.
const MaxOrphanBlocks = 100

// Chain manages the blockchain state: tip tracking, block acceptance, and reorgs.
type Chain struct {
	mu sync.RWMutex

	params  *params.ChainParams
	engine  consensus.Engine
	store   store.BlockStore

	tipHash   types.Hash
	tipHeight uint32
	tipWork   *big.Int

	// In-memory index: hash -> height for the main chain.
	heightByHash map[types.Hash]uint32
	hashByHeight map[uint32]types.Hash

	// Orphan blocks waiting for their parent.
	orphans map[types.Hash]*types.Block
}

// New creates a new Chain instance. The caller must call Init() to load or create the genesis.
func New(p *params.ChainParams, engine consensus.Engine, s store.BlockStore) *Chain {
	return &Chain{
		params:       p,
		engine:       engine,
		store:        s,
		tipWork:      big.NewInt(0),
		heightByHash: make(map[types.Hash]uint32),
		hashByHeight: make(map[uint32]types.Hash),
		orphans:      make(map[types.Hash]*types.Block),
	}
}

// Init loads the chain state from storage, or initializes with the genesis block.
func (c *Chain) Init() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	tipHash, tipHeight, err := c.store.GetChainTip()
	if err != nil {
		return c.initGenesis()
	}

	c.tipHash = tipHash
	c.tipHeight = tipHeight

	// Rebuild in-memory index by walking from genesis to tip.
	for h := uint32(0); h <= tipHeight; h++ {
		hash, err := c.store.GetBlockByHeight(h)
		if err != nil {
			return fmt.Errorf("rebuild index at height %d: %w", h, err)
		}
		c.heightByHash[hash] = h
		c.hashByHeight[h] = hash
	}

	// Compute cumulative work.
	c.tipWork = big.NewInt(0)
	for h := uint32(0); h <= tipHeight; h++ {
		header, err := c.store.GetHeader(c.hashByHeight[h])
		if err != nil {
			return fmt.Errorf("load header at height %d: %w", h, err)
		}
		c.tipWork.Add(c.tipWork, crypto.CalcWork(header.Bits))
	}

	return nil
}

func (c *Chain) initGenesis() error {
	genesisHash := crypto.HashBlockHeader(&c.params.GenesisBlock.Header)
	if !c.params.GenesisHash.IsZero() && genesisHash != c.params.GenesisHash {
		return fmt.Errorf("genesis hash mismatch: computed=%s expected=%s", genesisHash, c.params.GenesisHash)
	}

	if err := c.store.PutBlock(genesisHash, &c.params.GenesisBlock); err != nil {
		return fmt.Errorf("store genesis block: %w", err)
	}
	if err := c.store.PutBlockIndex(genesisHash, 0); err != nil {
		return fmt.Errorf("store genesis index: %w", err)
	}
	if err := c.store.PutChainTip(genesisHash, 0); err != nil {
		return fmt.Errorf("store genesis tip: %w", err)
	}

	c.tipHash = genesisHash
	c.tipHeight = 0
	c.tipWork = crypto.CalcWork(c.params.GenesisBlock.Header.Bits)
	c.heightByHash[genesisHash] = 0
	c.hashByHeight[0] = genesisHash

	return nil
}

// Tip returns the current best chain tip hash and height.
func (c *Chain) Tip() (types.Hash, uint32) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tipHash, c.tipHeight
}

// TipHeader returns the header of the current best chain tip.
func (c *Chain) TipHeader() (*types.BlockHeader, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.store.GetHeader(c.tipHash)
}

// GetHeaderByHeight returns the header at the given main-chain height.
func (c *Chain) GetHeaderByHeight(height uint32) (*types.BlockHeader, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	hash, ok := c.hashByHeight[height]
	if !ok {
		return nil, fmt.Errorf("no block at height %d", height)
	}
	return c.store.GetHeader(hash)
}

// GetBlockByHeight returns the full block at the given main-chain height.
func (c *Chain) GetBlockByHeight(height uint32) (*types.Block, types.Hash, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	hash, ok := c.hashByHeight[height]
	if !ok {
		return nil, types.ZeroHash, fmt.Errorf("no block at height %d", height)
	}
	block, err := c.store.GetBlock(hash)
	if err != nil {
		return nil, types.ZeroHash, err
	}
	return block, hash, nil
}

// GetAncestor returns the header at the given height on the main chain.
// Used as the getAncestor callback for consensus engine methods.
func (c *Chain) GetAncestor(height uint32) *types.BlockHeader {
	c.mu.RLock()
	defer c.mu.RUnlock()
	hash, ok := c.hashByHeight[height]
	if !ok {
		return nil
	}
	h, err := c.store.GetHeader(hash)
	if err != nil {
		return nil
	}
	return h
}

// HasBlock checks if a block is known (stored or orphan).
func (c *Chain) HasBlock(hash types.Hash) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if _, ok := c.heightByHash[hash]; ok {
		return true
	}
	if _, ok := c.orphans[hash]; ok {
		return true
	}
	has, _ := c.store.HasBlock(hash)
	return has
}

// ProcessBlock validates and potentially accepts a new block.
// Returns the height at which the block was accepted, or an error.
func (c *Chain) ProcessBlock(block *types.Block) (uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	blockHash := crypto.HashBlockHeader(&block.Header)

	// Already known?
	if _, ok := c.heightByHash[blockHash]; ok {
		metrics.Global.BlocksRejected.Add(1)
		return 0, fmt.Errorf("block %s already in chain", blockHash)
	}
	if _, ok := c.orphans[blockHash]; ok {
		metrics.Global.BlocksRejected.Add(1)
		return 0, fmt.Errorf("block %s already in orphan pool", blockHash)
	}

	// Find parent.
	parentHeight, parentKnown := c.heightByHash[block.Header.PrevBlock]
	if !parentKnown {
		// Orphan: parent not yet known.
		if len(c.orphans) >= MaxOrphanBlocks {
			return 0, fmt.Errorf("orphan pool full, rejecting %s", blockHash)
		}
		c.orphans[blockHash] = block
		metrics.Global.OrphansReceived.Add(1)
		return 0, fmt.Errorf("orphan block %s (parent %s unknown)", blockHash, block.Header.PrevBlock)
	}

	newHeight := parentHeight + 1

	// Get parent header for validation.
	parentHeader, err := c.store.GetHeader(block.Header.PrevBlock)
	if err != nil {
		return 0, fmt.Errorf("load parent header: %w", err)
	}

	// Validate header (consensus engine checks).
	nowUnix := uint32(time.Now().Unix())
	if err := c.engine.ValidateHeader(&block.Header, parentHeader, newHeight, c.getAncestorUnsafe, c.params); err != nil {
		return 0, fmt.Errorf("validate header at height %d: %w", newHeight, err)
	}

	// Validate timestamp.
	if err := consensus.ValidateHeaderTimestamp(&block.Header, parentHeader, nowUnix, c.getAncestorUnsafe, parentHeight, c.params); err != nil {
		return 0, fmt.Errorf("validate timestamp: %w", err)
	}

	// Validate block structure.
	if err := c.engine.ValidateBlock(block, newHeight, c.params); err != nil {
		return 0, fmt.Errorf("validate block at height %d: %w", newHeight, err)
	}

	// Compute new cumulative work by walking the actual parent chain.
	blockWork := crypto.CalcWork(block.Header.Bits)
	parentWork := c.workForParentChain(block.Header.PrevBlock)
	newWork := new(big.Int).Add(parentWork, blockWork)

	// Store the block.
	if err := c.store.PutBlock(blockHash, block); err != nil {
		return 0, fmt.Errorf("store block: %w", err)
	}

	// Determine if this extends the best chain or causes a reorg.
	if block.Header.PrevBlock == c.tipHash {
		// Simple extension of the best chain.
		if err := c.extendChain(blockHash, newHeight, newWork); err != nil {
			return 0, err
		}
	} else if newWork.Cmp(c.tipWork) > 0 || (newWork.Cmp(c.tipWork) == 0 && bytes.Compare(blockHash[:], c.tipHash[:]) < 0) {
		// Side chain with more work, or equal work with lower hash (deterministic tie-breaker) — reorg.
		if err := c.reorg(blockHash, newHeight, newWork); err != nil {
			return 0, fmt.Errorf("reorg to %s: %w", blockHash, err)
		}
	} else {
		// Side chain with less (or equal) work — track in memory only.
		c.heightByHash[blockHash] = newHeight
	}

	// Process any orphans that depended on this block.
	c.processOrphans(blockHash)

	metrics.Global.BlocksAccepted.Add(1)
	return newHeight, nil
}

func (c *Chain) extendChain(hash types.Hash, height uint32, work *big.Int) error {
	if err := c.store.PutBlockIndex(hash, height); err != nil {
		return err
	}
	if err := c.store.PutChainTip(hash, height); err != nil {
		return err
	}
	c.heightByHash[hash] = height
	c.hashByHeight[height] = hash
	c.tipHash = hash
	c.tipHeight = height
	c.tipWork = work
	return nil
}

// reorg performs a chain reorganization to a new tip with more work.
// Walks back from the new tip to find the fork point on the current main chain,
// then re-indexes from the fork point forward along the new chain.
func (c *Chain) reorg(newTipHash types.Hash, newTipHeight uint32, newWork *big.Int) error {
	newChain := []types.Hash{newTipHash}
	current := newTipHash

	for {
		block, err := c.store.GetBlock(current)
		if err != nil {
			return fmt.Errorf("load block %s during reorg: %w", current, err)
		}
		prevHash := block.Header.PrevBlock

		// Check if parent is on the main chain: it must be in heightByHash
		// AND the hash at that height must match (not just a stored side-chain block).
		if parentHeight, ok := c.heightByHash[prevHash]; ok {
			if c.hashByHeight[parentHeight] == prevHash {
				break
			}
		}

		newChain = append(newChain, prevHash)
		current = prevHash
	}

	forkBlock, _ := c.store.GetBlock(newChain[len(newChain)-1])
	forkParentHeight := c.heightByHash[forkBlock.Header.PrevBlock]

	oldTipHeight := c.tipHeight
	reorgDepth := oldTipHeight - forkParentHeight
	logging.L.Warn("chain reorg", "component", "chain", "fork_height", forkParentHeight, "old_tip", oldTipHeight, "new_tip", newTipHeight, "depth", reorgDepth)
	metrics.Global.Reorgs.Add(1)
	metrics.Global.ReorgDepthTotal.Add(uint64(reorgDepth))

	// Remove old main chain entries above fork.
	for h := forkParentHeight + 1; h <= c.tipHeight; h++ {
		if hash, ok := c.hashByHeight[h]; ok {
			delete(c.heightByHash, hash)
			delete(c.hashByHeight, h)
		}
	}

	// Add new chain entries.
	for i := len(newChain) - 1; i >= 0; i-- {
		h := forkParentHeight + uint32(len(newChain)-i)
		c.heightByHash[newChain[i]] = h
		c.hashByHeight[h] = newChain[i]
		if err := c.store.PutBlockIndex(newChain[i], h); err != nil {
			return err
		}
	}

	c.tipHash = newTipHash
	c.tipHeight = newTipHeight
	c.tipWork = newWork
	return c.store.PutChainTip(newTipHash, newTipHeight)
}

// orphanEntry pairs a block hash with its block for deterministic sorting.
type orphanEntry struct {
	hash  types.Hash
	block *types.Block
}

func (c *Chain) processOrphans(parentHash types.Hash) {
	// Iteratively resolve orphans whose parents are now known.
	// Uses a queue so that newly connected blocks can unblock further orphans.
	queue := []types.Hash{parentHash}

	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]

		// Collect orphans referencing this parent.
		var toProcess []orphanEntry
		for hash, orphan := range c.orphans {
			if orphan.Header.PrevBlock == parent {
				toProcess = append(toProcess, orphanEntry{hash: hash, block: orphan})
				delete(c.orphans, hash)
			}
		}

		// Sort by block hash for deterministic processing order.
		// All nodes must process competing orphans in the same sequence.
		sort.Slice(toProcess, func(i, j int) bool {
			return bytes.Compare(toProcess[i].hash[:], toProcess[j].hash[:]) < 0
		})

		for _, entry := range toProcess {
			blockHash := entry.hash
			orphan := entry.block

			parentHeight, ok := c.heightByHash[orphan.Header.PrevBlock]
			if !ok {
				continue
			}
			newHeight := parentHeight + 1
			parentHeader, err := c.store.GetHeader(orphan.Header.PrevBlock)
			if err != nil {
				continue
			}

			if err := c.engine.ValidateHeader(&orphan.Header, parentHeader, newHeight, c.getAncestorUnsafe, c.params); err != nil {
				logging.L.Debug("orphan failed header validation", "component", "chain", "hash", blockHash.ReverseString(), "error", err)
				continue
			}

			// Validate timestamp (same check as normal block processing).
			nowUnix := uint32(time.Now().Unix())
			if err := consensus.ValidateHeaderTimestamp(&orphan.Header, parentHeader, nowUnix, c.getAncestorUnsafe, parentHeight, c.params); err != nil {
				logging.L.Debug("orphan failed timestamp validation", "component", "chain", "hash", blockHash.ReverseString(), "error", err)
				continue
			}

			if err := c.engine.ValidateBlock(orphan, newHeight, c.params); err != nil {
				logging.L.Debug("orphan failed block validation", "component", "chain", "hash", blockHash.ReverseString(), "error", err)
				continue
			}

			blockWork := crypto.CalcWork(orphan.Header.Bits)
			parentWork := c.workForParentChain(orphan.Header.PrevBlock)
			newWork := new(big.Int).Add(parentWork, blockWork)

			_ = c.store.PutBlock(blockHash, orphan)

			if orphan.Header.PrevBlock == c.tipHash {
				if err := c.extendChain(blockHash, newHeight, newWork); err != nil {
					logging.L.Warn("orphan extend failed", "component", "chain", "hash", blockHash.ReverseString(), "error", err)
					continue
				}
			} else if newWork.Cmp(c.tipWork) > 0 || (newWork.Cmp(c.tipWork) == 0 && bytes.Compare(blockHash[:], c.tipHash[:]) < 0) {
				c.heightByHash[blockHash] = newHeight
				if err := c.reorg(blockHash, newHeight, newWork); err != nil {
					delete(c.heightByHash, blockHash)
					logging.L.Warn("orphan reorg failed", "component", "chain", "hash", blockHash.ReverseString(), "error", err)
					continue
				}
				logging.L.Info("reorg from orphan resolution", "component", "chain", "hash", blockHash.ReverseString(), "height", newHeight)
			} else {
				c.heightByHash[blockHash] = newHeight
			}

			queue = append(queue, blockHash)
		}
	}
}

// workForParentChain computes cumulative work by walking backwards from blockHash
// through its actual parent chain, rather than assuming the main chain.
func (c *Chain) workForParentChain(blockHash types.Hash) *big.Int {
	work := big.NewInt(0)
	current := blockHash
	for {
		header, err := c.store.GetHeader(current)
		if err != nil {
			break
		}
		work.Add(work, crypto.CalcWork(header.Bits))
		if header.PrevBlock.IsZero() {
			break
		}
		current = header.PrevBlock
	}
	return work
}

// getAncestorUnsafe is used internally when the lock is already held.
func (c *Chain) getAncestorUnsafe(height uint32) *types.BlockHeader {
	hash, ok := c.hashByHeight[height]
	if !ok {
		return nil
	}
	h, err := c.store.GetHeader(hash)
	if err != nil {
		return nil
	}
	return h
}

// GetBlock retrieves a block by hash from storage.
func (c *Chain) GetBlock(hash types.Hash) (*types.Block, error) {
	return c.store.GetBlock(hash)
}

// ChainInfo holds aggregate statistics for the getblockchaininfo RPC.
type ChainInfo struct {
	Network          string
	Height           uint32
	BestHash         types.Hash
	GenesisHash      types.Hash
	Bits             uint32
	Difficulty       float64
	Chainwork        *big.Int
	MedianTimePast   uint32
	RetargetEpoch    uint32
	EpochProgress    uint32
	EpochBlocksLeft  uint32
	RetargetInterval uint32
	VerificationProg float64
}

// GetChainInfo computes aggregate chain statistics.
func (c *Chain) GetChainInfo() *ChainInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	info := &ChainInfo{
		Network:          c.params.Name,
		Height:           c.tipHeight,
		BestHash:         c.tipHash,
		GenesisHash:      c.params.GenesisHash,
		Chainwork:        new(big.Int).Set(c.tipWork),
		RetargetInterval: c.params.RetargetInterval,
	}

	tipHeader, err := c.store.GetHeader(c.tipHash)
	if err == nil {
		info.Bits = tipHeader.Bits
		target := crypto.CompactToBig(tipHeader.Bits)
		if target.Sign() > 0 {
			genesisTarget := crypto.CompactToBig(c.params.InitialBits)
			fDiff := new(big.Float).SetInt(genesisTarget)
			fDiff.Quo(fDiff, new(big.Float).SetInt(target))
			info.Difficulty, _ = fDiff.Float64()
		}
	}

	// Median time past (last 11 blocks).
	const medianCount = 11
	timestamps := make([]uint32, 0, medianCount)
	for i := uint32(0); i < medianCount && c.tipHeight >= i; i++ {
		h := c.tipHeight - i
		hash, ok := c.hashByHeight[h]
		if !ok {
			break
		}
		hdr, err := c.store.GetHeader(hash)
		if err != nil {
			break
		}
		timestamps = append(timestamps, hdr.Timestamp)
	}
	if len(timestamps) > 0 {
		for i := 1; i < len(timestamps); i++ {
			key := timestamps[i]
			j := i - 1
			for j >= 0 && timestamps[j] > key {
				timestamps[j+1] = timestamps[j]
				j--
			}
			timestamps[j+1] = key
		}
		info.MedianTimePast = timestamps[len(timestamps)/2]
	}

	if c.params.RetargetInterval > 0 {
		info.RetargetEpoch = c.tipHeight / c.params.RetargetInterval
		info.EpochProgress = c.tipHeight % c.params.RetargetInterval
		info.EpochBlocksLeft = c.params.RetargetInterval - info.EpochProgress
	}

	info.VerificationProg = 1.0

	return info
}
