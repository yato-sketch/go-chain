package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bams-repo/fairchain/internal/algorithms"
	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/types"
)

type chainInfo struct {
	Height   uint32 `json:"blocks"`
	BestHash string `json:"bestblockhash"`
	Bits     string `json:"bits"`
	Chain    string `json:"chain"`
}

type blockInfo struct {
	Hash      string `json:"hash"`
	Height    uint32 `json:"height"`
	Timestamp uint32 `json:"time"`
	Bits      string `json:"bits"`
	Nonce     uint32 `json:"nonce"`
}

var hasher algorithms.Hasher

func main() {
	rpcAddr := flag.String("rpc", "http://127.0.0.1:19335", "Node RPC address")
	workers := flag.Int("workers", runtime.NumCPU(), "Number of mining threads")
	flag.Parse()

	h, err := algorithms.GetHasher(coinparams.Algorithm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unsupported PoW algorithm %q: %v\n", coinparams.Algorithm, err)
		os.Exit(1)
	}
	hasher = h

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Fprintf(os.Stderr, "\nshutting down...\n")
		cancel()
	}()

	fmt.Printf("timewarp-miner: algo=%s workers=%d rpc=%s\n", coinparams.Algorithm, *workers, *rpcAddr)
	fmt.Printf("strategy: set timestamp = parent_timestamp + 1 on every block\n\n")

	var totalBlocks uint64
	startTime := time.Now()

	for ctx.Err() == nil {
		ci, err := fetchChainInfo(*rpcAddr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "rpc error: %v (retrying in 2s)\n", err)
			sleep(ctx, 2*time.Second)
			continue
		}

		tip, err := fetchBlockByHeight(*rpcAddr, ci.Height)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fetch tip error: %v\n", err)
			sleep(ctx, 1*time.Second)
			continue
		}

		prevHash, err := types.HashFromReverseHex(ci.BestHash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse hash error: %v\n", err)
			sleep(ctx, 1*time.Second)
			continue
		}

		var bits uint32
		fmt.Sscanf(ci.Bits, "%x", &bits)
		newHeight := ci.Height + 1

		// Timestamp manipulation: always parent + 1
		blockTimestamp := tip.Timestamp + 1

		subsidy := fetchSubsidy(ci.Chain, newHeight)
		cb := makeCoinbaseTx(newHeight, subsidy)

		block := &types.Block{
			Header: types.BlockHeader{
				Version:   1,
				PrevBlock: prevHash,
				Timestamp: blockTimestamp,
				Bits:      bits,
				Nonce:     0,
			},
			Transactions: []types.Transaction{cb},
		}

		merkle, err := crypto.ComputeMerkleRoot(block.Transactions)
		if err != nil {
			fmt.Fprintf(os.Stderr, "merkle error: %v\n", err)
			continue
		}
		block.Header.MerkleRoot = merkle

		target := crypto.CompactToHash(bits)
		work := crypto.CalcWork(bits)

		fmt.Printf("mining height %d  bits=0x%08x  expected_hashes=%s  ts=%d (parent+1)\n",
			newHeight, bits, work.Text(10), blockTimestamp)

		found, nonce, hashes, elapsed := mineBlock(ctx, &block.Header, target, *workers, *rpcAddr, ci.BestHash)
		if ctx.Err() != nil {
			break
		}
		if !found {
			fmt.Printf("  stale or exhausted after %d hashes (%.1fs)\n", hashes, elapsed.Seconds())
			continue
		}

		block.Header.Nonce = nonce
		blockHash := crypto.HashBlockHeader(&block.Header)

		fmt.Printf("  FOUND nonce=%d hashes=%d time=%.1fs rate=%.1f H/s\n",
			nonce, hashes, elapsed.Seconds(), float64(hashes)/elapsed.Seconds())

		rejected, detail := submitBlock(*rpcAddr, block)
		if rejected {
			fmt.Printf("  REJECTED: %s\n", detail)
			sleep(ctx, 500*time.Millisecond)
			continue
		}

		totalBlocks++
		elapsed_total := time.Since(startTime)
		fmt.Printf("  ACCEPTED hash=%s  height=%d  total_mined=%d  uptime=%s\n\n",
			blockHash.ReverseString()[:16], newHeight, totalBlocks, elapsed_total.Round(time.Second))
	}

	fmt.Printf("\ntimewarp-miner stopped. mined %d blocks in %s\n", totalBlocks, time.Since(startTime).Round(time.Second))
}

func mineBlock(ctx context.Context, header *types.BlockHeader, target types.Hash, numWorkers int, rpcAddr string, tipHash string) (found bool, nonce uint32, totalHashes uint64, elapsed time.Duration) {
	start := time.Now()
	rangeSize := uint64(0x100000000) / uint64(numWorkers)

	type result struct {
		nonce  uint32
		hashes uint64
	}

	mineCtx, mineCancel := context.WithCancel(ctx)
	defer mineCancel()

	// Stale-tip detector runs in a single goroutine to avoid hammering RPC.
	go func() {
		for {
			select {
			case <-mineCtx.Done():
				return
			case <-time.After(3 * time.Second):
				ci, err := fetchChainInfo(rpcAddr)
				if err == nil && ci.BestHash != "" && ci.BestHash != tipHash {
					mineCancel()
					return
				}
			}
		}
	}()

	resultCh := make(chan result, 1)
	var hashCount atomic.Uint64
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		startNonce := uint64(w) * rangeSize
		endNonce := startNonce + rangeSize
		if w == numWorkers-1 {
			endNonce = 0x100000000
		}

		go func(wHeader types.BlockHeader, sn, en uint64) {
			defer wg.Done()
			wHeader.Nonce = uint32(sn)

			for pos := sn; pos < en; pos++ {
				select {
				case <-mineCtx.Done():
					return
				default:
				}

				h := hasher.PoWHash(wHeader.SerializeToBytes())
				hashCount.Add(1)

				if h.LessOrEqual(target) {
					select {
					case resultCh <- result{nonce: wHeader.Nonce}:
					default:
					}
					mineCancel()
					return
				}

				wHeader.Nonce++
			}
		}(*header, startNonce, endNonce)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case res, ok := <-resultCh:
			elapsed = time.Since(start)
			if !ok {
				return false, 0, hashCount.Load(), elapsed
			}
			return true, res.nonce, hashCount.Load(), elapsed
		case <-ticker.C:
			h := hashCount.Load()
			dt := time.Since(start).Seconds()
			if dt > 0 {
				fmt.Printf("  ... %d hashes, %.1f H/s, %.0fs elapsed\n", h, float64(h)/dt, dt)
			}
		case <-ctx.Done():
			mineCancel()
			wg.Wait()
			return false, 0, hashCount.Load(), time.Since(start)
		}
	}
}

func makeCoinbaseTx(height uint32, subsidy uint64) types.Transaction {
	pushLen := minimalHeightPushLen(height)
	heightBytes := make([]byte, 4)
	types.PutUint32LE(heightBytes, height)

	msg := make([]byte, 0, 1+pushLen+len("timewarp"))
	msg = append(msg, byte(pushLen))
	msg = append(msg, heightBytes[:pushLen]...)
	msg = append(msg, []byte("timewarp")...)

	return types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{{
			PreviousOutPoint: types.CoinbaseOutPoint,
			SignatureScript:  msg,
			Sequence:         0xFFFFFFFF,
		}},
		Outputs: []types.TxOutput{{
			Value:    subsidy,
			PkScript: []byte{0x00},
		}},
		LockTime: 0,
	}
}

func minimalHeightPushLen(height uint32) int {
	switch {
	case height <= 0xFF:
		return 1
	case height <= 0xFFFF:
		return 2
	case height <= 0xFFFFFF:
		return 3
	default:
		return 4
	}
}

func fetchSubsidy(chain string, height uint32) uint64 {
	var initial uint64
	var halving uint32
	switch chain {
	case "testnet":
		initial = 50_0000_00
		halving = 21_000_000
	case "mainnet":
		initial = 50_0000_0000
		halving = 210_000
	default:
		initial = 50_0000_0000
		halving = 150
	}
	halvings := height / halving
	if halvings >= 64 {
		return 0
	}
	return initial >> halvings
}

func fetchChainInfo(rpc string) (*chainInfo, error) {
	resp, err := http.Get(rpc + "/getblockchaininfo")
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

func fetchBlockByHeight(rpc string, height uint32) (*blockInfo, error) {
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

func submitBlock(rpc string, block *types.Block) (rejected bool, detail string) {
	data, err := block.SerializeToBytes()
	if err != nil {
		return true, fmt.Sprintf("serialize error: %v", err)
	}

	resp, err := http.Post(rpc+"/submitblock", "application/octet-stream", bytes.NewReader(data))
	if err != nil {
		return true, fmt.Sprintf("http error: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return true, string(body)
	}
	return false, string(body)
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
