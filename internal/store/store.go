package store

import (
	"github.com/bams-repo/fairchain/internal/types"
)

// BlockStore abstracts persistent storage for blocks, headers, and chain state.
// Implementations must be safe for concurrent read access.
// Write operations are expected to be serialized by the caller (chain manager).
type BlockStore interface {
	// HasBlock returns true if a block with the given hash is stored.
	HasBlock(hash types.Hash) (bool, error)

	// GetBlock retrieves a full block by its header hash.
	GetBlock(hash types.Hash) (*types.Block, error)

	// GetHeader retrieves a block header by its hash.
	GetHeader(hash types.Hash) (*types.BlockHeader, error)

	// PutBlock stores a full block. The header hash is used as the key.
	PutBlock(hash types.Hash, block *types.Block) error

	// GetBlockByHeight retrieves a block hash at the given height on the main chain.
	GetBlockByHeight(height uint32) (types.Hash, error)

	// PutBlockIndex stores the hash-to-height and height-to-hash mappings.
	PutBlockIndex(hash types.Hash, height uint32) error

	// GetChainTip returns the hash and height of the current best chain tip.
	GetChainTip() (types.Hash, uint32, error)

	// PutChainTip updates the stored chain tip.
	PutChainTip(hash types.Hash, height uint32) error

	// Close releases storage resources.
	Close() error
}

// PeerStore abstracts persistent storage for peer addresses.
type PeerStore interface {
	// GetPeers returns known peer addresses.
	GetPeers() ([]string, error)

	// PutPeer stores a peer address.
	PutPeer(addr string) error

	// RemovePeer removes a peer address.
	RemovePeer(addr string) error

	// Close releases storage resources.
	Close() error
}
