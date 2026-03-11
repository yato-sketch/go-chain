package store

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/bams-repo/fairchain/internal/types"
	bolt "go.etcd.io/bbolt"
)

// Bucket names for bbolt storage.
var (
	blocksBucket     = []byte("blocks")
	headersBucket    = []byte("headers")
	heightIndexBucket = []byte("heightindex") // height(uint32 BE) -> hash
	hashIndexBucket  = []byte("hashindex")    // hash -> height(uint32 BE)
	chainStateBucket = []byte("chainstate")
	peersBucket      = []byte("peers")
)

var chainTipKey = []byte("tip")

// BoltStore implements BlockStore and PeerStore using bbolt.
type BoltStore struct {
	db *bolt.DB
}

var _ BlockStore = (*BoltStore)(nil)
var _ PeerStore = (*BoltStore)(nil)

// NewBoltStore opens or creates a bbolt database at the given path.
func NewBoltStore(path string) (*BoltStore, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{})
	if err != nil {
		return nil, fmt.Errorf("open bolt db: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range [][]byte{
			blocksBucket, headersBucket, heightIndexBucket,
			hashIndexBucket, chainStateBucket, peersBucket,
		} {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init buckets: %w", err)
	}

	return &BoltStore{db: db}, nil
}

func (s *BoltStore) Close() error {
	return s.db.Close()
}

func (s *BoltStore) HasBlock(hash types.Hash) (bool, error) {
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(blocksBucket)
		found = b.Get(hash[:]) != nil
		return nil
	})
	return found, err
}

func (s *BoltStore) GetBlock(hash types.Hash) (*types.Block, error) {
	var block types.Block
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(blocksBucket).Get(hash[:])
		if data == nil {
			return fmt.Errorf("block %s not found", hash)
		}
		return block.Deserialize(bytes.NewReader(data))
	})
	if err != nil {
		return nil, err
	}
	return &block, nil
}

func (s *BoltStore) GetHeader(hash types.Hash) (*types.BlockHeader, error) {
	var header types.BlockHeader
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(headersBucket).Get(hash[:])
		if data == nil {
			return fmt.Errorf("header %s not found", hash)
		}
		return header.Deserialize(bytes.NewReader(data))
	})
	if err != nil {
		return nil, err
	}
	return &header, nil
}

func (s *BoltStore) PutBlock(hash types.Hash, block *types.Block) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		blockData, err := block.SerializeToBytes()
		if err != nil {
			return err
		}
		if err := tx.Bucket(blocksBucket).Put(hash[:], blockData); err != nil {
			return err
		}
		headerData := block.Header.SerializeToBytes()
		return tx.Bucket(headersBucket).Put(hash[:], headerData)
	})
}

func (s *BoltStore) GetBlockByHeight(height uint32) (types.Hash, error) {
	var hash types.Hash
	err := s.db.View(func(tx *bolt.Tx) error {
		key := heightToBytes(height)
		data := tx.Bucket(heightIndexBucket).Get(key)
		if data == nil {
			return fmt.Errorf("no block at height %d", height)
		}
		if len(data) != types.HashSize {
			return fmt.Errorf("corrupt height index at %d", height)
		}
		copy(hash[:], data)
		return nil
	})
	return hash, err
}

func (s *BoltStore) PutBlockIndex(hash types.Hash, height uint32) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		key := heightToBytes(height)
		if err := tx.Bucket(heightIndexBucket).Put(key, hash[:]); err != nil {
			return err
		}
		return tx.Bucket(hashIndexBucket).Put(hash[:], key)
	})
}

func (s *BoltStore) GetChainTip() (types.Hash, uint32, error) {
	var hash types.Hash
	var height uint32
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(chainStateBucket).Get(chainTipKey)
		if data == nil {
			return fmt.Errorf("chain tip not set")
		}
		if len(data) != types.HashSize+4 {
			return fmt.Errorf("corrupt chain tip data")
		}
		copy(hash[:], data[:types.HashSize])
		height = binary.BigEndian.Uint32(data[types.HashSize:])
		return nil
	})
	return hash, height, err
}

func (s *BoltStore) PutChainTip(hash types.Hash, height uint32) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		data := make([]byte, types.HashSize+4)
		copy(data[:types.HashSize], hash[:])
		binary.BigEndian.PutUint32(data[types.HashSize:], height)
		return tx.Bucket(chainStateBucket).Put(chainTipKey, data)
	})
}

// PeerStore methods

func (s *BoltStore) GetPeers() ([]string, error) {
	var peers []string
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(peersBucket)
		return b.ForEach(func(k, _ []byte) error {
			peers = append(peers, string(k))
			return nil
		})
	})
	return peers, err
}

func (s *BoltStore) PutPeer(addr string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(peersBucket).Put([]byte(addr), []byte{1})
	})
}

func (s *BoltStore) RemovePeer(addr string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(peersBucket).Delete([]byte(addr))
	})
}

func heightToBytes(h uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, h)
	return b
}
