package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/fairchain/fairchain/internal/chain"
	"github.com/fairchain/fairchain/internal/crypto"
	"github.com/fairchain/fairchain/internal/mempool"
	"github.com/fairchain/fairchain/internal/p2p"
	"github.com/fairchain/fairchain/internal/params"
	"github.com/fairchain/fairchain/internal/types"
)

// Server provides a minimal local HTTP JSON API for node status and control.
type Server struct {
	chain   *chain.Chain
	mempool *mempool.Mempool
	p2p     *p2p.Manager
	params  *params.ChainParams
	server  *http.Server
}

// New creates a new RPC server.
func New(addr string, c *chain.Chain, mp *mempool.Mempool, pm *p2p.Manager, p *params.ChainParams) *Server {
	s := &Server{
		chain:   c,
		mempool: mp,
		p2p:     pm,
		params:  p,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/getinfo", s.handleGetInfo)
	mux.HandleFunc("/getblockcount", s.handleGetBlockCount)
	mux.HandleFunc("/getbestblockhash", s.handleGetBestBlockHash)
	mux.HandleFunc("/getpeerinfo", s.handleGetPeerInfo)
	mux.HandleFunc("/getblock", s.handleGetBlock)
	mux.HandleFunc("/getblockbyheight", s.handleGetBlockByHeight)
	mux.HandleFunc("/submitblock", s.handleSubmitBlock)
	mux.HandleFunc("/getmempoolinfo", s.handleGetMempoolInfo)
	mux.HandleFunc("/getblockchaininfo", s.handleGetBlockchainInfo)

	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return s
}

// Start begins serving RPC requests.
func (s *Server) Start() error {
	log.Printf("[rpc] listening on %s", s.server.Addr)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[rpc] server error: %v", err)
		}
	}()
	return nil
}

// Stop gracefully shuts down the RPC server.
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) handleGetInfo(w http.ResponseWriter, r *http.Request) {
	tipHash, tipHeight := s.chain.Tip()
	resp := map[string]interface{}{
		"network":     s.params.Name,
		"height":      tipHeight,
		"best_hash":   tipHash.ReverseString(),
		"peers":       s.p2p.PeerCount(),
		"mempool_size": s.mempool.Count(),
	}
	writeJSON(w, resp)
}

func (s *Server) handleGetBlockCount(w http.ResponseWriter, r *http.Request) {
	_, height := s.chain.Tip()
	writeJSON(w, map[string]interface{}{"height": height})
}

func (s *Server) handleGetBestBlockHash(w http.ResponseWriter, r *http.Request) {
	hash, _ := s.chain.Tip()
	writeJSON(w, map[string]interface{}{"hash": hash.ReverseString()})
}

func (s *Server) handleGetPeerInfo(w http.ResponseWriter, r *http.Request) {
	addrs := s.p2p.PeerAddrs()
	writeJSON(w, map[string]interface{}{"peers": addrs, "count": len(addrs)})
}

func (s *Server) handleGetBlock(w http.ResponseWriter, r *http.Request) {
	hashStr := r.URL.Query().Get("hash")
	if hashStr == "" {
		writeError(w, http.StatusBadRequest, "missing hash parameter")
		return
	}
	hash, err := types.HashFromReverseHex(hashStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid hash: %v", err))
		return
	}
	block, err := s.chain.GetBlock(hash)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("block not found: %v", err))
		return
	}
	blockHash := crypto.HashBlockHeader(&block.Header)
	resp := map[string]interface{}{
		"hash":        blockHash.ReverseString(),
		"version":     block.Header.Version,
		"prev_block":  block.Header.PrevBlock.ReverseString(),
		"merkle_root": block.Header.MerkleRoot.ReverseString(),
		"timestamp":   block.Header.Timestamp,
		"bits":        fmt.Sprintf("%08x", block.Header.Bits),
		"nonce":       block.Header.Nonce,
		"tx_count":    len(block.Transactions),
	}
	writeJSON(w, resp)
}

func (s *Server) handleGetBlockByHeight(w http.ResponseWriter, r *http.Request) {
	heightStr := r.URL.Query().Get("height")
	if heightStr == "" {
		writeError(w, http.StatusBadRequest, "missing height parameter")
		return
	}
	var height uint32
	if _, err := fmt.Sscanf(heightStr, "%d", &height); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid height: %v", err))
		return
	}
	block, blockHash, err := s.chain.GetBlockByHeight(height)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("block not found: %v", err))
		return
	}
	resp := map[string]interface{}{
		"hash":        blockHash.ReverseString(),
		"height":      height,
		"version":     block.Header.Version,
		"prev_block":  block.Header.PrevBlock.ReverseString(),
		"merkle_root": block.Header.MerkleRoot.ReverseString(),
		"timestamp":   block.Header.Timestamp,
		"bits":        fmt.Sprintf("%08x", block.Header.Bits),
		"nonce":       block.Header.Nonce,
		"tx_count":    len(block.Transactions),
	}
	writeJSON(w, resp)
}

func (s *Server) handleSubmitBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var block types.Block
	if err := block.Deserialize(r.Body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid block: %v", err))
		return
	}
	height, err := s.chain.ProcessBlock(&block)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("rejected: %v", err))
		return
	}
	blockHash := crypto.HashBlockHeader(&block.Header)
	writeJSON(w, map[string]interface{}{
		"accepted": true,
		"hash":     blockHash.ReverseString(),
		"height":   height,
	})
}

func (s *Server) handleGetMempoolInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"size": s.mempool.Count(),
	})
}

func (s *Server) handleGetBlockchainInfo(w http.ResponseWriter, r *http.Request) {
	info := s.chain.GetChainInfo()
	resp := map[string]interface{}{
		"chain":                  info.Network,
		"blocks":                 info.Height,
		"best_block_hash":        info.BestHash.ReverseString(),
		"genesis_block_hash":     info.GenesisHash.ReverseString(),
		"bits":                   fmt.Sprintf("%08x", info.Bits),
		"difficulty":             info.Difficulty,
		"chainwork":              fmt.Sprintf("%064x", info.Chainwork),
		"median_time_past":       info.MedianTimePast,
		"retarget_interval":      info.RetargetInterval,
		"retarget_epoch":         info.RetargetEpoch,
		"epoch_progress":         info.EpochProgress,
		"epoch_blocks_remaining": info.EpochBlocksLeft,
		"verification_progress":  info.VerificationProg,
		"peers":                  s.p2p.PeerCount(),
		"mempool_size":           s.mempool.Count(),
	}
	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
