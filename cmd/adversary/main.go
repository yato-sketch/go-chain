package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/fairchain/fairchain/internal/crypto"
	"github.com/fairchain/fairchain/internal/types"
)

func main() {
	attack := flag.String("attack", "", "Attack type: bad-nonce, bad-merkle, duplicate, time-warp-future, time-warp-past, orphan-flood, inflated-coinbase, empty-block")
	rpc := flag.String("rpc", "http://127.0.0.1:31000", "Target node RPC address")
	count := flag.Int("count", 1, "Number of attack payloads to send (for flood attacks)")
	flag.Parse()

	if *attack == "" {
		fmt.Fprintln(os.Stderr, "Usage: fairchain-adversary -attack <type> -rpc <addr>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	var results []attackResult
	var err error

	switch *attack {
	case "bad-nonce":
		results, err = attackBadNonce(*rpc)
	case "bad-merkle":
		results, err = attackBadMerkle(*rpc)
	case "duplicate":
		results, err = attackDuplicate(*rpc)
	case "time-warp-future":
		results, err = attackTimeWarp(*rpc, true)
	case "time-warp-past":
		results, err = attackTimeWarp(*rpc, false)
	case "orphan-flood":
		results, err = attackOrphanFlood(*rpc, *count)
	case "inflated-coinbase":
		results, err = attackInflatedCoinbase(*rpc)
	case "empty-block":
		results, err = attackEmptyBlock(*rpc)
	default:
		fmt.Fprintf(os.Stderr, "Unknown attack: %s\n", *attack)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "attack setup error: %v\n", err)
		os.Exit(2)
	}

	out, _ := json.Marshal(results)
	fmt.Println(string(out))
}

type attackResult struct {
	Attack   string `json:"attack"`
	Rejected bool   `json:"rejected"`
	Error    string `json:"error,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

func fetchBlockByHeight(rpc string, height int) (*blockInfo, error) {
	resp, err := http.Get(fmt.Sprintf("%s/getblockbyheight?height=%d", rpc, height))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var info blockInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

type blockInfo struct {
	Hash      string `json:"hash"`
	Height    int    `json:"height"`
	Version   uint32 `json:"version"`
	PrevBlock string `json:"prev_block"`
	Merkle    string `json:"merkle_root"`
	Timestamp uint32 `json:"timestamp"`
	Bits      string `json:"bits"`
	Nonce     uint32 `json:"nonce"`
	TxCount   int    `json:"tx_count"`
}

type chainInfo struct {
	Height  int    `json:"blocks"`
	BestHash string `json:"best_block_hash"`
	Bits    string `json:"bits"`
}

func fetchChainInfo(rpc string) (*chainInfo, error) {
	resp, err := http.Get(fmt.Sprintf("%s/getblockchaininfo", rpc))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var info chainInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func submitBlock(rpc string, block *types.Block) (bool, string) {
	data, err := block.SerializeToBytes()
	if err != nil {
		return false, fmt.Sprintf("serialize error: %v", err)
	}

	resp, err := http.Post(rpc+"/submitblock", "application/octet-stream", bytes.NewReader(data))
	if err != nil {
		return false, fmt.Sprintf("http error: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return true, string(body)
	}
	return false, string(body)
}

func makeCoinbaseTx(height uint32, value uint64) types.Transaction {
	heightBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(heightBytes, height)
	return types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{{
			PreviousOutPoint: types.CoinbaseOutPoint,
			SignatureScript:  heightBytes,
			Sequence:         0xFFFFFFFF,
		}},
		Outputs: []types.TxOutput{{
			Value:    value,
			PkScript: []byte("adversary-test"),
		}},
		LockTime: 0,
	}
}

func buildBlockOnTip(rpc string) (*types.Block, uint32, error) {
	ci, err := fetchChainInfo(rpc)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch chain info: %w", err)
	}

	prevHash, err := types.HashFromReverseHex(ci.BestHash)
	if err != nil {
		return nil, 0, fmt.Errorf("parse best hash: %w", err)
	}

	var bits uint32
	fmt.Sscanf(ci.Bits, "%x", &bits)
	newHeight := uint32(ci.Height) + 1

	cb := makeCoinbaseTx(newHeight, 5000000000)

	block := &types.Block{
		Header: types.BlockHeader{
			Version:   1,
			PrevBlock: prevHash,
			Timestamp: uint32(time.Now().Unix()),
			Bits:      bits,
			Nonce:     0,
		},
		Transactions: []types.Transaction{cb},
	}

	merkle, err := crypto.ComputeMerkleRoot(block.Transactions)
	if err != nil {
		return nil, 0, err
	}
	block.Header.MerkleRoot = merkle

	return block, newHeight, nil
}

// Attack 1: Valid block structure but nonce doesn't satisfy PoW
func attackBadNonce(rpc string) ([]attackResult, error) {
	block, _, err := buildBlockOnTip(rpc)
	if err != nil {
		return nil, err
	}

	block.Header.Nonce = 0xDEADBEEF

	rejected, detail := submitBlock(rpc, block)
	return []attackResult{{
		Attack:   "bad-nonce",
		Rejected: rejected,
		Detail:   detail,
	}}, nil
}

// Attack 2: Valid block but merkle root is corrupted
func attackBadMerkle(rpc string) ([]attackResult, error) {
	block, _, err := buildBlockOnTip(rpc)
	if err != nil {
		return nil, err
	}

	block.Header.MerkleRoot[0] ^= 0xFF
	block.Header.MerkleRoot[15] ^= 0xAA

	rejected, detail := submitBlock(rpc, block)
	return []attackResult{{
		Attack:   "bad-merkle",
		Rejected: rejected,
		Detail:   detail,
	}}, nil
}

// Attack 3: Resubmit block at height 1 (already accepted)
func attackDuplicate(rpc string) ([]attackResult, error) {
	ci, err := fetchChainInfo(rpc)
	if err != nil {
		return nil, err
	}
	if ci.Height < 1 {
		return []attackResult{{Attack: "duplicate", Rejected: false, Detail: "chain too short"}}, nil
	}

	info, err := fetchBlockByHeight(rpc, 1)
	if err != nil {
		return nil, err
	}

	prevHash, _ := types.HashFromReverseHex(info.PrevBlock)
	merkle, _ := types.HashFromReverseHex(info.Merkle)
	var bits uint32
	fmt.Sscanf(info.Bits, "%x", &bits)

	block := &types.Block{
		Header: types.BlockHeader{
			Version:    info.Version,
			PrevBlock:  prevHash,
			MerkleRoot: merkle,
			Timestamp:  info.Timestamp,
			Bits:       bits,
			Nonce:      info.Nonce,
		},
		Transactions: []types.Transaction{makeCoinbaseTx(1, 5000000000)},
	}

	rejected, detail := submitBlock(rpc, block)
	return []attackResult{{
		Attack:   "duplicate",
		Rejected: rejected,
		Detail:   detail,
	}}, nil
}

// Attack 4: Block with timestamp far in the future or before parent
func attackTimeWarp(rpc string, future bool) ([]attackResult, error) {
	block, _, err := buildBlockOnTip(rpc)
	if err != nil {
		return nil, err
	}

	if future {
		block.Header.Timestamp = uint32(time.Now().Unix()) + 7200 + 1 // >2h ahead
	} else {
		block.Header.Timestamp = 1 // way before any parent
	}

	merkle, _ := crypto.ComputeMerkleRoot(block.Transactions)
	block.Header.MerkleRoot = merkle

	rejected, detail := submitBlock(rpc, block)

	label := "time-warp-future"
	if !future {
		label = "time-warp-past"
	}
	return []attackResult{{
		Attack:   label,
		Rejected: rejected,
		Detail:   detail,
	}}, nil
}

// Attack 5: Flood with blocks referencing random nonexistent parents
func attackOrphanFlood(rpc string, count int) ([]attackResult, error) {
	var results []attackResult
	for i := 0; i < count; i++ {
		var fakeParent types.Hash
		rand.Read(fakeParent[:])

		cb := makeCoinbaseTx(uint32(i+99999), 5000000000)
		block := &types.Block{
			Header: types.BlockHeader{
				Version:   1,
				PrevBlock: fakeParent,
				Timestamp: uint32(time.Now().Unix()),
				Bits:      0x1e0fffff,
				Nonce:     uint32(i),
			},
			Transactions: []types.Transaction{cb},
		}
		merkle, _ := crypto.ComputeMerkleRoot(block.Transactions)
		block.Header.MerkleRoot = merkle

		rejected, detail := submitBlock(rpc, block)
		results = append(results, attackResult{
			Attack:   "orphan-flood",
			Rejected: rejected,
			Detail:   detail,
		})
	}
	return results, nil
}

// Attack 6: Block with inflated coinbase (more than allowed subsidy)
func attackInflatedCoinbase(rpc string) ([]attackResult, error) {
	block, newHeight, err := buildBlockOnTip(rpc)
	if err != nil {
		return nil, err
	}

	block.Transactions[0].Outputs[0].Value = 999999999999999

	merkle, _ := crypto.ComputeMerkleRoot(block.Transactions)
	block.Header.MerkleRoot = merkle

	target := crypto.CompactToHash(block.Header.Bits)
	found, _ := (&powSealer{}).seal(&block.Header, target, 5000000)
	detail := ""
	if !found {
		detail = "could not find PoW (expected for high-diff); submitting anyway"
	}

	rejected, submitDetail := submitBlock(rpc, block)
	return []attackResult{{
		Attack:   "inflated-coinbase",
		Rejected: rejected,
		Detail:   detail + " | " + submitDetail,
		Error:    fmt.Sprintf("attempted %d sats at height %d", block.Transactions[0].Outputs[0].Value, newHeight),
	}}, nil
}

// Attack 7: Block with zero transactions (no coinbase)
func attackEmptyBlock(rpc string) ([]attackResult, error) {
	ci, err := fetchChainInfo(rpc)
	if err != nil {
		return nil, err
	}

	prevHash, _ := types.HashFromReverseHex(ci.BestHash)
	var bits uint32
	fmt.Sscanf(ci.Bits, "%x", &bits)

	block := &types.Block{
		Header: types.BlockHeader{
			Version:   1,
			PrevBlock: prevHash,
			Timestamp: uint32(time.Now().Unix()),
			Bits:      bits,
			Nonce:     0,
		},
		Transactions: []types.Transaction{},
	}

	rejected, detail := submitBlock(rpc, block)
	return []attackResult{{
		Attack:   "empty-block",
		Rejected: rejected,
		Detail:   detail,
	}}, nil
}

type powSealer struct{}

func (p *powSealer) seal(header *types.BlockHeader, target types.Hash, maxIter uint64) (bool, error) {
	for i := uint64(0); i < maxIter; i++ {
		hash := crypto.HashBlockHeader(header)
		if hash.LessOrEqual(target) {
			return true, nil
		}
		header.Nonce++
	}
	return false, nil
}
