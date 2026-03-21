// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package node

import (
	"fmt"
	"math/big"
	"os"

	"github.com/bams-repo/fairchain/internal/config"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/store"
)

// MigrateFromLegacy converts a legacy blocks.db to the new flat-file + LevelDB format.
func MigrateFromLegacy(cfg *config.Config, p *params.ChainParams) error {
	legacyPath := cfg.LegacyDBPath()
	if _, err := os.Stat(legacyPath); os.IsNotExist(err) {
		legacyPath = cfg.DBPath()
		if _, err := os.Stat(legacyPath); os.IsNotExist(err) {
			return fmt.Errorf("no legacy blocks.db found at %s or %s", cfg.LegacyDBPath(), cfg.DBPath())
		}
	}

	logging.L.Info("migrating from legacy format", "source", legacyPath)

	legacy, err := store.NewBoltStore(legacyPath)
	if err != nil {
		return fmt.Errorf("open legacy store: %w", err)
	}
	defer legacy.Close()

	if !legacy.LegacyHasData() {
		return fmt.Errorf("legacy store has no chain data")
	}

	tipHash, tipHeight, err := legacy.LegacyGetChainTip()
	if err != nil {
		return fmt.Errorf("get legacy chain tip: %w", err)
	}

	logging.L.Info("legacy chain", "tip", tipHash.ReverseString(), "height", tipHeight)

	newStore, err := store.NewFileStore(
		cfg.BlocksDir(),
		cfg.BlockIndexDir(),
		cfg.ChainstateDir(),
		p.NetworkMagic,
	)
	if err != nil {
		return fmt.Errorf("open new store: %w", err)
	}
	defer newStore.Close()

	cumulativeWork := store.CalcWork(p.GenesisBlock.Header.Bits)
	if tipHeight > 0 {
		cumulativeWork.SetInt64(0)
	}
	for h := uint32(0); h <= tipHeight; h++ {
		hash, err := legacy.LegacyGetBlockByHeight(h)
		if err != nil {
			return fmt.Errorf("get block hash at height %d: %w", h, err)
		}
		block, err := legacy.LegacyGetBlock(hash)
		if err != nil {
			return fmt.Errorf("get block at height %d: %w", h, err)
		}

		fileNum, offset, size, err := newStore.WriteBlock(hash, block)
		if err != nil {
			return fmt.Errorf("write block at height %d: %w", h, err)
		}

		blockWork := store.CalcWork(block.Header.Bits)
		cumulativeWork = new(big.Int).Add(cumulativeWork, blockWork)
		rec := &store.DiskBlockIndex{
			Header:    block.Header,
			Height:    h,
			Status:    store.StatusHaveData | store.StatusValidHeader | store.StatusValidTx,
			TxCount:   uint32(len(block.Transactions)),
			FileNum:   fileNum,
			DataPos:   offset,
			DataSize:  size,
			ChainWork: new(big.Int).Set(cumulativeWork),
		}

		undoBytes, undoErr := legacy.LegacyGetUndoData(hash)
		if undoErr == nil && len(undoBytes) > 0 {
			undoOffset, undoSize, wErr := newStore.WriteUndo(fileNum, undoBytes)
			if wErr == nil {
				rec.UndoFile = fileNum
				rec.UndoPos = undoOffset
				rec.UndoSize = undoSize
				rec.Status |= store.StatusHaveUndo
			}
		}

		if err := newStore.PutBlockIndex(hash, rec); err != nil {
			return fmt.Errorf("put block index at height %d: %w", h, err)
		}

		if h%1000 == 0 || h == tipHeight {
			logging.L.Info("migration progress", "height", h, "total", tipHeight)
		}
	}

	if err := newStore.PutChainTip(tipHash, tipHeight); err != nil {
		return fmt.Errorf("set chain tip: %w", err)
	}

	logging.L.Info("block migration complete, chain will rebuild UTXO set on next startup")
	return nil
}
