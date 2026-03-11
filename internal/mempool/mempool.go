package mempool

import (
	"fmt"
	"sync"

	"github.com/fairchain/fairchain/internal/crypto"
	"github.com/fairchain/fairchain/internal/params"
	"github.com/fairchain/fairchain/internal/types"
)

// Mempool holds unconfirmed transactions awaiting inclusion in a block.
// Thread-safe for concurrent access from P2P and RPC handlers.
type Mempool struct {
	mu   sync.RWMutex
	txs  map[types.Hash]*types.Transaction
	p    *params.ChainParams
}

// New creates a new empty mempool.
func New(p *params.ChainParams) *Mempool {
	return &Mempool{
		txs: make(map[types.Hash]*types.Transaction),
		p:   p,
	}
}

// AddTx validates and adds a transaction to the mempool.
// Returns the transaction hash if accepted.
func (m *Mempool) AddTx(tx *types.Transaction) (types.Hash, error) {
	if tx.IsCoinbase() {
		return types.ZeroHash, fmt.Errorf("coinbase transactions cannot enter mempool")
	}

	txHash, err := crypto.HashTransaction(tx)
	if err != nil {
		return types.ZeroHash, fmt.Errorf("hash transaction: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.txs[txHash]; exists {
		return txHash, fmt.Errorf("transaction %s already in mempool", txHash)
	}

	if uint32(len(m.txs)) >= m.p.MaxMempoolSize {
		return types.ZeroHash, fmt.Errorf("mempool full (%d transactions)", len(m.txs))
	}

	// TODO: Add fee validation, input validation, double-spend checks.
	// For the POC, we accept any non-coinbase transaction that serializes correctly.

	m.txs[txHash] = tx
	return txHash, nil
}

// GetTx retrieves a transaction by hash.
func (m *Mempool) GetTx(hash types.Hash) (*types.Transaction, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tx, ok := m.txs[hash]
	return tx, ok
}

// HasTx checks if a transaction is in the mempool.
func (m *Mempool) HasTx(hash types.Hash) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.txs[hash]
	return ok
}

// RemoveTx removes a transaction from the mempool (e.g., after block inclusion).
func (m *Mempool) RemoveTx(hash types.Hash) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.txs, hash)
}

// RemoveTxs removes multiple transactions (e.g., all txs in a newly accepted block).
func (m *Mempool) RemoveTxs(hashes []types.Hash) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, h := range hashes {
		delete(m.txs, h)
	}
}

// GetAll returns copies of all transactions in the mempool.
// The returned slice is ordered deterministically by hash for block template building.
func (m *Mempool) GetAll() []*types.Transaction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect hashes and sort for determinism.
	hashes := make([]types.Hash, 0, len(m.txs))
	for h := range m.txs {
		hashes = append(hashes, h)
	}
	sortHashes(hashes)

	txs := make([]*types.Transaction, len(hashes))
	for i, h := range hashes {
		txs[i] = m.txs[h]
	}
	return txs
}

// Count returns the number of transactions in the mempool.
func (m *Mempool) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.txs)
}

// sortHashes sorts a slice of hashes lexicographically for deterministic ordering.
func sortHashes(hashes []types.Hash) {
	// Simple insertion sort — mempool is typically small enough.
	for i := 1; i < len(hashes); i++ {
		key := hashes[i]
		j := i - 1
		for j >= 0 && hashGreater(hashes[j], key) {
			hashes[j+1] = hashes[j]
			j--
		}
		hashes[j+1] = key
	}
}

func hashGreater(a, b types.Hash) bool {
	for i := types.HashSize - 1; i >= 0; i-- {
		if a[i] > b[i] {
			return true
		}
		if a[i] < b[i] {
			return false
		}
	}
	return false
}
