# resources/

Non-Go reference code, test data, and standalone tools that support the
project but are **not** part of the Go build tree. Files here are never
compiled by `go build ./...`.

## sha256mem-c/

Standalone C reference implementation of the **sha256mem** memory-hard
proof-of-work algorithm. This is used to validate the Go implementation
(`internal/algorithms/sha256mem`) against an independent codebase and to
benchmark different optimization strategies.

| File | Purpose |
|------|---------|
| `sha256mem.h` | Public API header (hash parameters, function signature) |
| `sha256mem.c` | Portable C implementation using OpenSSL's `libcrypto` |
| `sha256mem_bare.c` | Self-contained implementation with no external dependencies |
| `sha256_bare.h` | Embedded SHA-256 primitives for the bare variant |
| `sha256mem_fast.c` | Optimized variant with manual loop unrolling |
| `sha256mem_asm.c` | Wrapper that calls hand-written x86-64 assembly |
| `sha256_asm.S` | x86-64 assembly SHA-256 block transform |
| `sha256mem_shani.c` | Intel SHA-NI hardware-accelerated variant |
| `sha256_shani.h` | SHA-NI intrinsics implementation |
| `test_sha256mem.c` | Test harness — reads vectors and verifies output |
| `bench_sha256mem.c` | Benchmark harness for comparing variants |
| `test_vectors.txt` | 1000 pre-computed test vectors (hex input/output pairs) |
| `gen_vectors.go.txt` | Go program that generated the test vectors (renamed from `.go` to keep this directory out of the Go build) |
| `Makefile` | Builds the C test/bench binaries (`make test` to run) |

### Building and running tests

```bash
cd resources/sha256mem-c
make test
```

Requires `gcc` and `libssl-dev` (OpenSSL) for the default variant.
