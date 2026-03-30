// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license.

// gen_vectors generates 1000 sha256mem test vectors as hex pairs (input, expected_output).
// Run from repo root: go run ./internal/algorithms/sha256mem/c/gen_vectors.go
// Output: test_vectors.txt in the current working directory.
package main

import (
	"encoding/hex"
	"fmt"
	"os"

	"github.com/bams-repo/fairchain/internal/algorithms/sha256mem"
)

func main() {
	h := sha256mem.New()

	outPath := "test_vectors.txt"
	if len(os.Args) > 1 {
		outPath = os.Args[1]
	}

	f, err := os.Create(outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	for i := 0; i < 1000; i++ {
		input := make([]byte, i)
		for j := 0; j < i; j++ {
			input[j] = byte((i + j) & 0xFF)
		}

		result := h.PoWHash(input)

		fmt.Fprintf(f, "%s %s\n", hex.EncodeToString(input), hex.EncodeToString(result[:]))
	}

	fmt.Println("wrote 1000 test vectors to", outPath)
}
