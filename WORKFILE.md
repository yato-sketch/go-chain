# WORKFILE — Fairchain Blockchain POC

## 1. Project Purpose

Fairchain is a Go blockchain proof-of-concept designed as a modular foundation for fairness-oriented consensus research. The immediate goal is a robust baseline chain with deterministic behavior and clear upgrade boundaries, so future agents can swap consensus components (identity-bound tickets, VRF eligibility, sequential memory-hard proofs) without rewriting the node.

The baseline uses Nakamoto-style proof-of-work. All consensus-critical code is explicit, auditable, and free of hidden state.

## 2. Current Architecture Overview

Single-binary node architecture with clean package separation:

```
cmd/           → Entrypoints (node, genesis tool, CLI)
internal/      → All library code (not importable externally)
  types/       → Consensus structs, canonical serialization
  crypto/      → Hashing, merkle, target math
  params/      → Chain parameters, genesis config, network definitions
  consensus/   → Engine interface, structural validation, timestamp rules
  consensus/pow/ → Baseline PoW engine
  chain/       → Blockchain state manager
  store/       → Abstract storage + bbolt implementation
  mempool/     → Transaction pool
  miner/       → Block template building, mining loop
  p2p/         → Peer manager, gossip, sync
  p2p/discovery/ → Peer discovery
  protocol/    → Wire message definitions, binary encoding
  rpc/         → Local HTTP JSON API
  config/      → Config loading
  util/        → Non-consensus helpers
```

**Key design principle**: Consensus code has zero knowledge of networking. Network code calls into consensus only through the `consensus.Engine` interface and the chain manager.

## 3. Directory Tree

```
fairchain/
├── cmd/
│   ├── node/main.go          # Full node entrypoint
│   ├── genesis/main.go        # Genesis mining tool
│   └── cli/main.go            # CLI query tool
├── internal/
│   ├── types/
│   │   ├── hash.go            # Hash type, comparison, encoding
│   │   ├── transaction.go     # Transaction, TxInput, TxOutput, OutPoint, VarInt
│   │   ├── block.go           # BlockHeader, Block
│   │   └── types_test.go      # Serialization roundtrip tests
│   ├── crypto/
│   │   ├── hash.go            # DoubleSHA256, HashBlockHeader, HashTransaction
│   │   ├── merkle.go          # MerkleRoot, ComputeMerkleRoot
│   │   ├── target.go          # CompactToBig, BigToCompact, CalcWork, ValidatePoW
│   │   └── crypto_test.go     # Hash, merkle, bits, work tests
│   ├── params/
│   │   ├── params.go          # ChainParams struct, CalcSubsidy
│   │   ├── genesis.go         # GenesisConfig, BuildGenesisBlock
│   │   └── networks.go        # Mainnet, Testnet, Regtest definitions
│   ├── consensus/
│   │   ├── engine.go          # consensus.Engine interface
│   │   ├── validation.go      # ValidateBlockStructure, ValidateHeaderTimestamp
│   │   ├── validation_test.go # Block validation tests
│   │   └── pow/
│   │       ├── engine.go      # PoW engine: validate, retarget, seal, mine genesis
│   │       └── pow_test.go    # PoW-specific tests
│   ├── chain/
│   │   ├── chain.go           # Chain state manager
│   │   └── chain_test.go      # Chain integration tests
│   ├── store/
│   │   ├── store.go           # BlockStore, PeerStore interfaces
│   │   ├── bolt.go            # bbolt implementation
│   │   └── store_test.go      # Storage roundtrip tests
│   ├── mempool/
│   │   └── mempool.go         # Thread-safe mempool
│   ├── miner/
│   │   └── miner.go           # Mining loop, template builder
│   ├── p2p/
│   │   ├── peer.go            # Peer connection wrapper
│   │   ├── manager.go         # P2P manager, handshake, message routing
│   │   └── discovery/
│   │       └── discovery.go   # Peer discovery
│   ├── protocol/
│   │   ├── messages.go        # Wire message types and encoding
│   │   ├── checksum.go        # Message checksum helper
│   │   └── protocol_test.go   # Message encode/decode tests
│   ├── rpc/
│   │   └── server.go          # HTTP JSON API
│   ├── config/
│   │   └── config.go          # Config struct, loading, defaults
│   └── util/
│       └── util.go            # Non-consensus helpers
├── go.mod
├── go.sum
├── Makefile
├── config.sample.json
├── .gitignore
├── README.md
├── TODO.md
└── WORKFILE.md
```

## 4. Consensus-Critical Invariants

These rules MUST be preserved by any future changes:

1. **Canonical serialization**: All consensus types use explicit little-endian binary encoding. No reflection, no JSON, no encoding/gob for consensus data.

2. **Deterministic hashing**: Block header hash = DoubleSHA256(80-byte canonical header). Transaction hash = DoubleSHA256(canonical tx bytes). These must produce identical results on all machines.

3. **Merkle root**: Bitcoin-style binary merkle tree. Odd levels duplicate the last hash. Order is transaction order in the block.

4. **Target comparison**: Hash is treated as a 256-bit little-endian integer. Block is valid if hash ≤ target.

5. **Compact bits**: Uses Bitcoin nBits encoding. `CompactToBig` and `BigToCompact` must round-trip correctly.

6. **Subsidy**: `subsidy = InitialSubsidy >> (height / SubsidyHalvingInterval)`. Pure integer arithmetic, no floats.

7. **Retargeting**: `newTarget = oldTarget * actualTimespan / targetTimespan`, clamped to [1/4, 4x]. Integer arithmetic only.

8. **Coinbase**: First transaction in every block must be coinbase (zero-hash outpoint, index 0xFFFFFFFF). No other transaction may be coinbase.

9. **No map iteration in consensus**: Any code that iterates over maps in a consensus-critical path must sort keys first.

10. **No float math in consensus**: All target, work, subsidy, and timing calculations use integer arithmetic.

11. **Explicit endianness**: All multi-byte values use little-endian encoding in wire/storage format, except height-to-bytes in the store index (big-endian for lexicographic ordering).

## 5. Network Protocol Overview

Wire messages use a 24-byte header: magic(4) + command(12, null-padded) + length(4 LE) + checksum(4, first 4 bytes of DoubleSHA256 of payload).

| Command | Purpose |
|---------|---------|
| version | Initial handshake, exchange height/version/nonce |
| verack | Handshake acknowledgment |
| ping/pong | Liveness check with nonce echo |
| inv | Announce inventory (blocks/txs) by hash |
| getdata | Request specific inventory items |
| block | Full block payload |
| tx | Full transaction payload |
| getblocks | Request block hashes from locator |
| addr | Gossip peer addresses |

Self-connection detection uses a random nonce in the version message.

## 6. Current Chain Params

### Regtest (primary development network)
- Magic: `FA 1C C0 FF`
- Port: 19444
- Block spacing: 1 second
- Initial bits: `0x207fffff` (very easy)
- No retarget
- Subsidy: 50 coins (5,000,000,000 units)
- Halving interval: 150 blocks
- Coinbase maturity: 1 block

### Testnet
- Magic: `FA 1C C0 02`
- Port: 19334
- Block spacing: 1 minute
- Initial bits: `0x1e0fffff`

### Mainnet
- Magic: `FA 1C C0 01`
- Port: 19333
- Block spacing: 2 minutes
- Initial bits: `0x1d00ffff`

## 7. Genesis Block Details

Genesis blocks are mined at node startup if not already cached. The mining uses a fixed timestamp (1741651200 = 2025-03-11T00:00:00Z) and fixed coinbase message for reproducibility.

Genesis config structure:
- NetworkName, CoinbaseMessage, Timestamp, Bits, Version, Reward, RewardScript
- `BuildGenesisBlock()` constructs the block with zero nonce
- `MineGenesis()` iterates nonce until hash ≤ target
- Same inputs always produce the same genesis block

## 8. What Is Fully Implemented

- All core types with canonical serialization and deserialization
- Double-SHA256 hashing for blocks and transactions
- Merkle root computation
- Compact bits ↔ target conversion
- Chainwork calculation
- PoW validation
- Genesis block building and mining
- Chain params for 3 networks
- Subsidy schedule
- Block structure validation (7 rules)
- Timestamp validation (2 modes)
- Difficulty retargeting with clamping
- Pluggable consensus.Engine interface
- Baseline PoW consensus engine
- Chain state manager with orphan pool and reorg
- bbolt persistent storage
- Thread-safe mempool
- Mining loop with template building
- P2P peer management with handshake
- Inventory-based gossip protocol
- Block and transaction propagation
- Initial block sync
- Peer address gossip and persistence
- Local HTTP JSON RPC API (7 endpoints)
- CLI query tool
- 43 passing tests

## 9. What Is Stubbed but Planned

- **UTXO validation**: Data structures support full UTXO model, but only coinbase transactions are validated. Regular spend validation is not implemented. The `TxInput.SignatureScript` and `TxOutput.PkScript` fields exist but are not interpreted.

- **Headers-first sync**: Current sync uses `getblocks` to request block hashes, then fetches full blocks. The protocol supports a headers-first approach but it's not implemented.

- **Peer scoring**: The peer struct has infrastructure for tracking behavior but no scoring/banning logic.

- **Advanced mempool policy**: Fee-based prioritization, eviction, and double-spend detection are not implemented.

- **Activation heights**: `ChainParams.ActivationHeights` map exists but no activation logic uses it yet.

## 10. Known Limitations

1. **No spend validation**: Only coinbase transactions are validated. Any non-coinbase transaction that serializes correctly is accepted into the mempool and blocks.

2. **No UTXO set**: There is no UTXO set tracking. This means no double-spend detection at the chain level.

3. **Reorg is basic**: The reorg logic walks the chain but doesn't revert UTXO state (since there is none). It re-indexes heights only.

4. **Genesis mined at startup**: Each network's genesis is mined on first run. For regtest this is instant; for mainnet/testnet it could be slow. Pre-computed genesis hashes should be hardcoded for production.

5. **No wallet**: No key generation, address derivation, or transaction signing.

6. **Single-threaded mining**: The miner uses a single goroutine. No parallel nonce search.

7. **No checkpoints**: No checkpoint validation for fast sync.

8. **Memory-resident chain index**: The height↔hash index is rebuilt from storage on startup. For very long chains this could be slow.

## 11. Exact Next Recommended Tasks

### Immediate (Phase 4 completion)
1. **Multi-node simulation harness**: Write a test that starts 2-3 nodes in-process, connects them, mines on one, and verifies propagation.
2. **Pre-compute genesis hashes**: Run the genesis miner for each network and hardcode the results in `networks.go`.
3. **Structured logging**: Replace `log.Printf` with a leveled logger (slog from stdlib).

### Short-term (Phase 5)
4. **UTXO set**: Implement an in-memory UTXO set backed by a bbolt bucket. Track creates/spends per block for reorg rollback.
5. **Input validation**: Verify that each input references an existing unspent output.
6. **Coinbase maturity**: Enforce that coinbase outputs cannot be spent until `CoinbaseMaturity` blocks deep.
7. **Fee calculation**: Compute fees as input_sum - output_sum.

### Medium-term (Phase 6)
8. **Identity registration tx type**: Add a new transaction version or type for registering a mining identity (pubkey + collateral deposit).
9. **Consensus engine swap**: Implement a second `consensus.Engine` that uses identity-based eligibility instead of PoW.
10. **Epoch seed interface**: Define how epoch seeds are computed from chain history.

## 12. Safe Extension Points

These areas are designed for extension and can be modified without breaking existing consensus:

- **`consensus.Engine` interface**: Add new implementations freely. The chain manager calls through this interface.
- **`ChainParams.ActivationHeights`**: Use this to gate new consensus rules by block height.
- **`Transaction.Version`**: Use new version numbers for new transaction types (identity registration, etc.).
- **`TxOutput.PkScript`**: Currently a raw byte placeholder. Can be extended to support real locking scripts.
- **`TxInput.SignatureScript`**: Currently unused for validation. Can be extended for signature verification.
- **`BlockHeader.Version`**: Use for signaling consensus upgrades.
- **Mempool admission**: Add validation rules in `mempool.AddTx()` without affecting consensus.
- **RPC endpoints**: Add new endpoints to `rpc/server.go` freely.
- **Peer message types**: Add new command strings to the protocol. Unknown commands are logged and ignored.

## 13. Unsafe Areas Where Changes Can Break Consensus

**DO NOT modify these without extreme care:**

- **`types/hash.go`**: Hash comparison order affects PoW validation and chain selection.
- **`types/transaction.go` serialization**: Any change to Serialize/Deserialize changes transaction hashes and merkle roots.
- **`types/block.go` serialization**: Any change to header serialization changes block hashes.
- **`crypto/hash.go`**: DoubleSHA256 is the identity function for blocks and transactions.
- **`crypto/merkle.go`**: Merkle root algorithm must match exactly or blocks will fail validation.
- **`crypto/target.go`**: CompactToBig/BigToCompact must match Bitcoin's nBits encoding or difficulty is wrong.
- **`consensus/validation.go`**: Block structure rules are consensus-critical.
- **`consensus/pow/engine.go` CalcNextBits**: Retarget logic must be identical on all nodes.
- **`params/params.go` CalcSubsidy**: Subsidy calculation is consensus-critical.

**Rule of thumb**: If a change affects the output of `HashBlockHeader`, `HashTransaction`, `ComputeMerkleRoot`, `CompactToBig`, `CalcSubsidy`, or `CalcNextBits`, it is a consensus-breaking change and requires a hard fork.

## 14. Suggested Workflow for Future Agents

1. **Read this WORKFILE first.** Understand the architecture before making changes.
2. **Run `make test`** before and after every change.
3. **Check invariants in section 4** before modifying any consensus code.
4. **Use the `consensus.Engine` interface** for new consensus mechanisms. Don't modify the PoW engine; create a new engine.
5. **Use `ChainParams.ActivationHeights`** to gate new rules by height.
6. **Use new `Transaction.Version` values** for new transaction types.
7. **Keep consensus code free of network concerns.** If you need network info in consensus, pass it as a parameter.
8. **Test determinism explicitly.** Any new consensus function should have a test that runs it twice with the same inputs and asserts identical output.
9. **Don't use maps in consensus-critical iteration** without sorting keys first.
10. **Don't use floats in consensus code.** Ever.

## 15. Commands

```bash
# Build all binaries
make build

# Run all tests
make test

# Mine a genesis block (informational output)
make mine-genesis

# Run a regtest node with mining
make run-regtest

# Run a second regtest node
make run-regtest2

# Query node status
make status

# Or directly:
bin/fairchain-cli --rpc http://127.0.0.1:19445 getinfo
bin/fairchain-cli --rpc http://127.0.0.1:19445 getblockcount
bin/fairchain-cli --rpc http://127.0.0.1:19445 peers
bin/fairchain-cli --rpc http://127.0.0.1:19445 getblock <hash>
```
