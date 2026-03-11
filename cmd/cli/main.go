package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	rpcAddr := flag.String("rpc", "http://127.0.0.1:19445", "RPC server address")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	var endpoint string
	switch args[0] {
	case "getinfo", "status":
		endpoint = "/getinfo"
	case "getblockcount", "height":
		endpoint = "/getblockcount"
	case "getbestblockhash", "tip":
		endpoint = "/getbestblockhash"
	case "getpeerinfo", "peers":
		endpoint = "/getpeerinfo"
	case "getmempoolinfo", "mempool":
		endpoint = "/getmempoolinfo"
	case "getblock":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: cli getblock <hash>")
			os.Exit(1)
		}
		endpoint = "/getblock?hash=" + args[1]
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		printUsage()
		os.Exit(1)
	}

	resp, err := http.Get(*rpcAddr + endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "RPC request failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Read response failed: %v\n", err)
		os.Exit(1)
	}

	// Pretty-print JSON.
	var data interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		fmt.Println(string(body))
		return
	}
	pretty, _ := json.MarshalIndent(data, "", "  ")
	fmt.Println(string(pretty))
}

func printUsage() {
	fmt.Println("fairchain CLI - query a running fairchain node")
	fmt.Println()
	fmt.Println("Usage: fairchain-cli [--rpc URL] <command> [args]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  getinfo / status         Show node status")
	fmt.Println("  getblockcount / height    Show current block height")
	fmt.Println("  getbestblockhash / tip    Show best block hash")
	fmt.Println("  getpeerinfo / peers       Show connected peers")
	fmt.Println("  getmempoolinfo / mempool  Show mempool info")
	fmt.Println("  getblock <hash>           Show block details")
}
