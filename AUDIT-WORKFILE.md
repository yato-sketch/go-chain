# Deterministic Consensus Audit — Agent Workfile

## Purpose

This workfile divides the Fairchain consensus codebase into 8 independent audit paths. Each path traces one complete consensus-critical flow from POINT A (entry) to POINT Z (final state change or output). An agent assigned to a path audits ONLY the files in that path and produces a verdict on whether every step is fully deterministic.

## What "Deterministic" Means for This Audit

A function is deterministic if: given identical inputs, it produces identical outputs on every machine, every run, regardless of goroutine scheduling, OS, or hardware. The following are non-determinism sources that MUST be flagged:

| Source | Why it breaks consensus |
|--------|------------------------|
| `map` iteration without sorted keys | Go maps randomize iteration order |
| `time.Now()` in consensus logic | Different nodes see different wall clocks |
| Floating-point arithmetic | IEEE 754 rounding varies across platforms |
| Goroutine scheduling dependency | Race conditions produce different outcomes |
| `math/rand` without fixed seed | Random output varies per run |
| Unsorted slice from map values | Order depends on map iteration |
| Platform-dependent integer sizes | `int` is 32-bit on some, 64-bit on others |
| Endianness assumptions without explicit encoding | ARM vs x86 byte order |

## How to Use This Workfile

1. Each agent reads ONLY their assigned path section.
2. Read every file listed, in order, following the function call chain.
3. For each function, answer: "Can this produce different output given identical input?" If yes, document exactly where and why.
4. Check every prior audit finding listed in the path — verify whether it is fixed or still present.
5. Produce a verdict: PASS (fully deterministic) or FAIL (with specific locations and descriptions).
6. Do NOT modify any code. This is a read-only audit.

## Reference Documents

- `WORKFILE.md` — Architecture overview, invariants (section 4), known bugs (section 11)
- `DOCS/audits/mainnet-consensus-audit-2026-03-12.md` — 6 confirmed findings from prior audit

---

# PATH 1: Canonical Serialization & Hashing

**Scope**: Verify that all consensus types serialize to a single canonical byte sequence, and that hashing those bytes is deterministic.

**Point A**: A `types.BlockHeader`, `types.Transaction`, or `types.Block` struct is populated with data.
**Point Z**: `crypto.HashBlockHeader()` or `crypto.HashTransaction()` returns a `types.Hash`.

## Files (read in order)

| # | File | Lines | What to audit |
|---|------|-------|---------------|
| 1 | `internal/types/hash.go` | 121 | Hash type, `IsZero`, `Less`, comparison semantics, `ReverseBytes` |
| 2 | `internal/types/transaction.go` | 397 | `Transaction.Serialize`, `TxInput.Serialize`, `TxOutput.Serialize`, `OutPoint.Serialize`, `WriteVarInt`, `ReadVarInt`, `WriteVarBytes`, `ReadVarBytes` |
| 3 | `internal/types/block.go` | 115 | `BlockHeader.serializeInto`, `BlockHeader.SerializeToBytes`, `Block.Serialize`, `Block.Deserialize` |
| 4 | `internal/crypto/hash.go` | 29 | `DoubleSHA256`, `HashBlockHeader`, `HashTransaction` |
| 5 | `internal/crypto/merkle.go` | 51 | `MerkleRoot`, `ComputeMerkleRoot` |

## Function call chain

```
BlockHeader.SerializeToBytes()
  → BlockHeader.serializeInto(buf)     [80 bytes, LE fields]
  → crypto.HashBlockHeader(&header)
    → DoubleSHA256(header.SerializeToBytes())
      → sha256(sha256(bytes))

Transaction.SerializeToBytes()
  → Transaction.Serialize(writer)
    → WriteVarInt (input count)
    → for each input: TxInput.Serialize
      → OutPoint.Serialize [32-byte hash + 4-byte LE index]
      → WriteVarBytes (sigScript)
      → binary.Write(Sequence, LE)
    → WriteVarInt (output count)
    → for each output: TxOutput.Serialize
      → binary.Write(Value, LE)
      → WriteVarBytes (pkScript)
    → binary.Write(LockTime, LE)
  → crypto.HashTransaction(&tx)
    → DoubleSHA256(tx.SerializeToBytes())

ComputeMerkleRoot(txs)
  → for each tx: HashTransaction → hashes[]
  → MerkleRoot(hashes)
    → iterative pairing, odd-level duplication, DoubleSHA256 of concatenated pairs
```

## Determinism checklist

- [x] All `binary.Write` calls use `binary.LittleEndian` explicitly — PASS: No `binary.Write` calls exist; all encoding uses `binary.LittleEndian.PutUint*` (no reflection)
- [x] `WriteVarInt` produces exactly one encoding for each value (canonical) — PASS: Strict boundary checks in `PutVarInt` (transaction.go:68-86)
- [x] `ReadVarInt` rejects non-minimal encodings (prior audit Finding 2) — PASS: Both `ReadVarInt` and `ReadVarIntFromBytes` enforce minimality checks
- [x] No `encoding/json`, `encoding/gob`, or reflection in serialization — PASS: Zero matches across all 5 files
- [x] `MerkleRoot` processes hashes in slice order (not map order) — PASS: Input is `[]types.Hash`, iterated by integer index
- [x] `DoubleSHA256` uses `crypto/sha256` from stdlib (deterministic) — PASS: `sha256.Sum256` called twice
- [x] `Hash.Less()` comparison is byte-by-byte, not platform-dependent — PASS: Iterates from index 31 down to 0, comparing individual bytes
- [x] No float math anywhere in these files — PASS: Zero matches for `float32`, `float64`, `math.`

## Prior findings to verify

- **Audit Finding 2 (varint malleability)**: **FIXED.** `ReadVarInt` (transaction.go:90-129) rejects non-minimal encodings: `0xFD` prefix rejects `val < 0xFD`, `0xFE` prefix rejects `val <= 0xFFFF`, `0xFF` prefix rejects `val <= 0xFFFFFFFF`. `ReadVarIntFromBytes` (transaction.go:133-168) enforces identical checks.

## PATH 1 VERDICT: **PASS** — Fully deterministic

---

# PATH 2: Target Math, Difficulty & Proof-of-Work

**Scope**: Verify that compact↔big conversion, work calculation, PoW validation, and difficulty retargeting are fully deterministic integer arithmetic.

**Point A**: A block header with a `Bits` field arrives for validation.
**Point Z**: `ValidateProofOfWork` returns true/false, or `CalcNextBits` returns the next difficulty.

## Files (read in order)

| # | File | Lines | What to audit |
|---|------|-------|---------------|
| 1 | `internal/crypto/target.go` | 114 | `CompactToBig`, `BigToCompact`, `CompactToHash`, `CalcWork`, `ValidateProofOfWork` |
| 2 | `internal/consensus/pow/engine.go` | 141 | `ValidateHeader`, `CalcNextBits`, `PrepareHeader`, `SealHeader` |
| 3 | `internal/params/params.go` | 68 | `ChainParams` struct, `CalcSubsidy` |
| 4 | `internal/params/networks.go` | 235 | Mainnet/Testnet/Regtest parameter values |
| 5 | `internal/store/store.go` | 66 | `CalcWork` delegation (verify it uses `crypto.CalcWork`) |

## Function call chain

```
ValidateHeader(header, parent, height, getAncestor, params)
  → CalcNextBits(parent, parentHeight, getAncestor, params)
    → if NoRetarget: return parent.Bits
    → if height % RetargetInterval != 0: return parent.Bits
    → getAncestor(retargetHeight) → firstHeader
    → actualTimespan = parent.Timestamp - firstHeader.Timestamp
    → clamp to [targetTimespan/4, targetTimespan*4]
    → CompactToBig(parent.Bits) → target
    → target = target * actualTimespan / targetTimespan
    → clamp to powLimit
    → BigToCompact(target)
  → crypto.ValidateProofOfWork(headerHash, header.Bits)
    → CompactToHash(bits) → target
    → bytes.Compare(hash[:], target[:]) <= 0

CalcSubsidy(height)
  → halvings = height / SubsidyHalvingInterval
  → if halvings >= 64: return 0
  → InitialSubsidy >> halvings
```

## Determinism checklist

- [x] `CompactToBig` handles negative sign bit (bit 23 of mantissa) — PASS: Bit 23 checked at target.go:17, result negated via `big.Int.Neg`. Matches Bitcoin Core's `SetCompact`.
- [x] `BigToCompact` roundtrips correctly: `BigToCompact(CompactToBig(x)) == x` for all valid x — PASS: Normalization at lines 56-59 ensures canonical form
- [x] `CalcNextBits` uses only integer arithmetic (no floats) — PASS: All arithmetic is `int64` or `big.Int`. `time.Duration / time.Second` is `int64 / int64`.
- [x] Timespan clamping uses integer comparison, not float — PASS: engine.go:75-79, all `int64` operations
- [x] `CalcWork` uses `big.Int` division (deterministic) — PASS: target.go:104, `new(big.Int).Div(oneLsh256, denominator)`
- [x] `CalcSubsidy` uses bit shift (>>) not division or float — PASS: params.go:67, `p.InitialSubsidy >> halvings`
- [x] `ValidateProofOfWork` compares hash as bytes, not as platform-dependent integers — PASS: Uses `Hash.LessOrEqual` → byte-by-byte comparison
- [x] `store.CalcWork` delegates to `crypto.CalcWork` (prior audit Finding 4) — PASS: store.go:65, `return crypto.CalcWork(bits)`
- [x] No `time.Now()` in any of these functions — PASS: Zero matches. All timestamps from block headers.
- [x] All params are constants, not derived at runtime — PASS: All fields are literal constants or compile-time expressions

## Prior findings to verify

- **Audit Finding 4 (duplicate CalcWork)**: **FIXED.** `store.CalcWork` at store.go:64-66 is a one-line delegation to `crypto.CalcWork`. No duplicate implementation.
- **WORKFILE invariant 7 (retargeting)**: **VERIFIED.** Clamping is `[targetTimespan/4, targetTimespan*4]` = `[1/4x, 4x]` using `int64` arithmetic.
- **WORKFILE invariant 5 (compact bits)**: **VERIFIED.** Roundtrip holds for all canonical compact values.

## PATH 2 VERDICT: **PASS** — Fully deterministic

---

# PATH 3: Block Validation Pipeline

**Scope**: Trace a block from arrival through every validation check to acceptance/rejection. Verify every check is deterministic.

**Point A**: `chain.ProcessBlock(block)` is called.
**Point Z**: Block is accepted (tip extended or reorg triggered) or rejected with error.

## Files (read in order)

| # | File | Lines | What to audit |
|---|------|-------|---------------|
| 1 | `internal/chain/chain.go` | 1072 | `ProcessBlock`, `extendChain`, `reorg`, `buildAncestorLookup`, `workForParentChain`, `processOrphans`, `evictRandomOrphan`, `evictExpiredOrphans` |
| 2 | `internal/consensus/engine.go` | 47 | `Engine` interface (review contracts) |
| 3 | `internal/consensus/validation.go` | 140 | `ValidateBlockStructure`, `ValidateHeaderTimestamp` |
| 4 | `internal/consensus/pow/engine.go` | 141 | `ValidateHeader`, `ValidateBlock` |
| 5 | `internal/timeadjust/timeadjust.go` | 101 | `Now()`, `AddSample`, `recalcMedian` — time source for validation |

## Function call chain

```
ProcessBlock(block)
  → crypto.HashBlockHeader(&block.Header)
  → check heightByHash (already known?)
  → check orphan pool
  → check parent exists in heightByHash
    → if not: addOrphan → return ErrOrphanBlock
  → newHeight = parentHeight + 1
  → store.GetHeader(prevBlock) → parent header
  → buildAncestorLookup(prevBlock, parentHeight) → getAncestor func
  → engine.ValidateHeader(header, parent, height, getAncestor, params)
    → CalcNextBits (PATH 2)
    → ValidateProofOfWork (PATH 2)
  → consensus.ValidateHeaderTimestamp(header, getAncestor, timeSource, params)
    → median-of-11 check OR prev+1 check
    → future drift check: header.Timestamp <= timeSource.Now() + MaxFutureBlockTime
  → engine.ValidateBlock(block, height, params)
    → ValidateBlockStructure(block, height, params)
      → coinbase checks, merkle root, block size, duplicate txids, coinbase overflow
  → crypto.CalcWork(header.Bits) → blockWork
  → workForParentChain(prevBlock) → parentWork
  → newWork = parentWork + blockWork
  → if extends tip: extendChain(block, height, blockHash, newWork)
  → if newWork > tipWork: reorg(...)
  → else: store as side chain
  → processOrphans(blockHash)
```

## Determinism checklist

- [x] `buildAncestorLookup` walks the actual parent chain, not `hashByHeight` (WORKFILE RC-1) — PASS: chain.go:937-971 walks backwards from parentHash through store, falls through to main chain only below fork point
- [x] `workForParentChain` returns error on disk failure, not partial work (audit Finding 6) — PASS: chain.go:908-917 reads ChainWork from single `GetBlockIndex` call, returns explicit error on failure
- [x] `processOrphans` sorts orphans before processing (verify `sort.Slice` on hash) — PASS: chain.go:797-799, `sort.Slice` with `bytes.Compare` on hash ensures deterministic order
- [x] `evictRandomOrphan` map iteration is non-deterministic — verify it is NOT consensus-critical (eviction only) — PASS: chain.go:768-775, cache management only, evicted orphans can be re-requested
- [x] `ValidateHeaderTimestamp` uses `timeSource.Now()` not `time.Now()` directly — PASS: chain.go:400 passes `c.timeSource.Now()`, validation.go:88 receives as parameter
- [x] `timeadjust.Now()` — verify median calculation is deterministic (sorted samples) — PASS: recalcMedian copies offsets, sorts with `sort.Slice`, takes middle element
- [x] `ValidateBlockStructure` duplicate txid check uses map-as-set (order irrelevant) — PASS: validation.go:59-69, `map[types.Hash]struct{}` with presence checks only
- [x] Chain selection tiebreaker (`bytes.Compare` on hash) is deterministic — PASS: chain.go:480, deterministic lexicographic comparison
- [x] `DiskBlockIndex` is NOT written before validation completes (WORKFILE RC-3) — PASS: In extend-tip path, `PutBlockIndex` at line 461 is after all validation. Reorg path writes after header+structural validation.
- [x] No goroutine races in `ProcessBlock` (check mutex usage) — PASS: chain.go:363, `c.mu.Lock()` with `defer c.mu.Unlock()`, no goroutines spawned in critical section

## Prior findings to verify

- **WORKFILE RC-1**: **FIXED.** `buildAncestorLookup` (chain.go:937-971) walks backwards from parent hash through store, building side-chain height→header map. Falls through to main chain only below fork point.
- **WORKFILE RC-3**: **FIXED.** `PutBlockIndex` called only after validation succeeds in all paths.
- **WORKFILE RC-6**: **FIXED.** `workForParentChain` (chain.go:908-917) reads `ChainWork` from `DiskBlockIndex` via single `GetBlockIndex` call. O(1).
- **WORKFILE RC-8**: **FIXED.** `reorg()` snapshots all in-memory state before mutation (chain.go:569-580), restores on error via deferred function (chain.go:582-594). Incremental chainstate persistence is crash-safe via recovery logic in `Init()`.
- **Audit Finding 3**: **FIXED.** Single undo write location in post-connect loop (chain.go:722-739). Connect loop does not write undo data.
- **Audit Finding 6**: **FIXED.** `workForParentChain` returns explicit error on disk failure (chain.go:911). Caller in `ProcessBlock` propagates error.

## PATH 3 VERDICT: **PASS** — Fully deterministic

---

# PATH 4: UTXO State Transitions

**Scope**: Trace how the UTXO set changes when a block is connected or disconnected. Verify every mutation is deterministic and reversible.

**Point A**: `utxoSet.ConnectBlock(block, height, params)` or `utxoSet.DisconnectBlock(block, undoData)` is called.
**Point Z**: UTXO set is updated, undo data is produced (connect) or consumed (disconnect).

## Files (read in order)

| # | File | Lines | What to audit |
|---|------|-------|---------------|
| 1 | `internal/utxo/utxo.go` | 372 | `UtxoSet`, `UtxoEntry`, `ConnectBlock`, `ConnectGenesis`, `DisconnectBlock`, `SerializeUndoData`, `DeserializeUndoData`, `ForEach`, `Clone` |
| 2 | `internal/chain/chain.go` | 1072 | `extendChain` (lines ~420-470), `reorg` (lines ~470-640), `persistUtxoChanges`, `persistUtxoDisconnect`, `flushUtxoSetToChainstate` |
| 3 | `internal/store/chainstate.go` | 135 | `PutUtxo`, `GetUtxo`, `DeleteUtxo`, `ChainstateWriteBatch`, `FlushUtxoBatch` |

## Function call chain

```
ConnectBlock(block, height, params)
  → for each tx in block.Transactions (in order):
    → if not coinbase:
      → for each input:
        → lookup UTXO by outpoint
        → verify exists (return error if not)
        → check coinbase maturity
        → record in undo data (SpentOutput)
        → mark for deletion
    → for each output:
      → create UtxoEntry{Value, PkScript, Height, IsCoinbase}
      → add to set
  → apply: delete spent, add created
  → return BlockUndoData

DisconnectBlock(block, undoData)
  → for each tx in REVERSE order:
    → delete outputs created by this tx
    → if not coinbase:
      → restore spent UTXOs from undo data
  → return nil

SerializeUndoData(undo) → []byte
  → varint(count) + for each SpentOutput: varint(value) + varint(height) + byte(isCoinbase) + varint(pkScriptLen) + pkScript

DeserializeUndoData([]byte) → BlockUndoData
  → inverse of above
```

## Determinism checklist

- [x] `ConnectBlock` iterates `block.Transactions` in slice order (not map) — PASS: `for txIdx := range block.Transactions` (utxo.go:229)
- [x] Input iteration within each tx is in slice order — PASS: `for _, in := range tx.Inputs` (utxo.go:238)
- [x] Output iteration within each tx is in slice order — PASS: `for outIdx, out := range tx.Outputs` (utxo.go:277)
- [x] `DisconnectBlock` iterates transactions in REVERSE slice order — PASS: `for txIdx := len(block.Transactions) - 1; txIdx >= 0; txIdx--` (utxo.go:308)
- [x] Undo data serialization is canonical (same struct → same bytes) — PASS: Fixed-width LE integers + canonical varint + raw bytes, no padding, no maps
- [x] Undo data deserialization roundtrips: `Deserialize(Serialize(x)) == x` — PASS: Symmetric format, varint rejects non-canonical encodings, fuzz tested
- [x] UTXO entries stored in a map — verify no consensus-critical iteration over the map — PASS: All consensus paths use key-based Get/Add/Remove. Map iteration only in `ForEach` (persistence) and `TotalValue` (commutative sum)
- [x] `Clone()` copies all entries (map iteration order irrelevant since it's a copy) — PASS: `cloneUtxoSet` (chain.go:744-754) iterates snapshot, inserts into new map, order-independent
- [x] `ForEach` used only for persistence flushing (not consensus ordering) — PASS: Called in `flushUtxoSetToChainstate` only, LevelDB writes are order-independent
- [x] Coinbase maturity check uses integer comparison: `height - utxo.Height < CoinbaseMaturity` — PASS: All `uint32` arithmetic with underflow guard (txvalidation.go:81)
- [x] No float math in value calculations — PASS: All value arithmetic is `uint64` with overflow checks
- [x] `persistUtxoChanges` writes are key-value (order irrelevant for DB) — PASS: LevelDB batch ops applied atomically (chain.go:506-530)
- [x] Undo data is written exactly ONCE per block during reorg (audit Finding 3) — PASS: Written in post-connect loop (chain.go:722-739) only. `persistUtxoChanges` does not write undo.

## Prior findings to verify

- **Audit Finding 3 (double undo write)**: **FIXED.** `reorg()` writes undo data exactly once per block in the post-connect loop (chain.go:722-739). `persistUtxoChanges` writes only UTXO puts/deletes and best-block pointer.
- **WORKFILE RC-8**: **FIXED.** `reorg()` clones UTXO set at line 580 as rollback safety net. Disconnects old chain, connects new chain against live UTXO set. If any step fails, deferred `restoreSnapshot()` restores the clone. Matches Bitcoin Core's approach.

## PATH 4 VERDICT: **PASS** — Fully deterministic

---

# PATH 5: Transaction Validation & Script Execution

**Scope**: Trace a transaction from mempool admission through block-level validation, including script execution. Verify all checks are deterministic.

**Point A**: A raw transaction arrives (via P2P `handleTx` or block inclusion).
**Point Z**: Transaction is accepted into mempool, or validated as part of a block.

## Files (read in order)

| # | File | Lines | What to audit |
|---|------|-------|---------------|
| 1 | `internal/consensus/txvalidation.go` | 273 | `ValidateTransactionInputs`, `ValidateSingleTransaction`, `validateTxScript` |
| 2 | `internal/script/script.go` | 292 | Script interpreter: `Execute`, `evalScript`, opcode implementations (OP_DUP, OP_HASH160, OP_EQUALVERIFY, OP_CHECKSIG, OP_RETURN) |
| 3 | `internal/crypto/keys.go` | 207 | `Hash160`, `P2PKHScript`, `ComputeSigHash`, `ExtractPubKeyHash` |
| 4 | `internal/crypto/signature.go` | 65 | `Sign`, `VerifySignature` |
| 5 | `internal/mempool/mempool.go` | 393 | `AddTx`, `BlockTemplate`, `RemoveTxs`, double-spend tracking |

## Function call chain — Block validation path

```
ValidateTransactionInputs(block, utxoSet, height, params)
  → for each tx (skip coinbase):
    → for each input:
      → utxoSet.Get(outpoint) → entry
      → coinbase maturity check
      → accumulate inputSum
    → for each output: accumulate outputSum
    → verify inputSum >= outputSum
    → totalFees += inputSum - outputSum
    → if script validation activated:
      → validateTxScript(tx, inputIdx, utxo)
        → script.Execute(sigScript, pkScript, tx, inputIdx, flags)
          → evalScript(sigScript) → stack
          → evalScript(pkScript) using stack from sig
          → OP_DUP: duplicate top
          → OP_HASH160: hash160(top)
          → OP_EQUALVERIFY: pop two, compare
          → OP_CHECKSIG: ComputeSigHash → VerifySignature
  → verify coinbase value <= subsidy + totalFees
```

## Function call chain — Mempool admission path

```
mempool.AddTx(tx, utxoSet, tipHeight, params)
  → reject if coinbase
  → reject if empty inputs/outputs
  → crypto.HashTransaction(tx) → txHash
  → check duplicate in pool
  → check double-spend vs spentOutpoints map
  → ValidateSingleTransaction(tx, utxoSet, tipHeight+1, params)
    → same UTXO/script checks as block path but for single tx
  → calculate fee, feeRate
  → add to pool, update spentOutpoints
  → evict if over capacity (lowest fee-rate)
```

## Determinism checklist

- [x] `ValidateTransactionInputs` iterates txs in block order (slice) — PASS: `for txIdx := range block.Transactions`, slice iteration
- [x] `spentInBlock` map used as set only (presence check, not iteration) — PASS: Only `_, alreadySpent := spentInBlock[opKey]` and insertions, never iterated
- [x] `seenInputs` map used as set only — PASS: Only presence checks and insertions, never iterated
- [x] Script `Execute` is a pure function of (sigScript, pkScript, tx, inputIdx, flags) — PASS: Creates local `engine` struct, no global state, no I/O, no randomness
- [x] `evalScript` stack operations are deterministic (push/pop on a slice) — PASS: `stack.items` is `[][]byte`, standard deterministic LIFO
- [x] `OP_CHECKSIG` → `ComputeSigHash` → deterministic serialization of tx for signing — PASS: Deep-copies tx, clears scripts, serializes via canonical `Serialize`, appends hashtype, double-SHA256
- [x] `VerifySignature` uses `ecdsa.Verify` from `crypto/ecdsa` (deterministic) — PASS: Uses `dcrd/secp256k1/v4/ecdsa` `sig.Verify(sigHash[:], pubKey)`, pure mathematical verification
- [x] `Hash160` = RIPEMD160(SHA256(x)) — both are deterministic — PASS: `sha256.Sum256(data)` then `ripemd160.New().Write(sha[:]).Sum(nil)`
- [x] Fee calculation is pure integer subtraction — PASS: `fee := totalIn - totalOut`, pure `uint64` with overflow checks
- [x] Mempool `BlockTemplate` sorts by fee-rate then hash (deterministic order) — PASS: `sort.Slice` with FeeRate descending, `hashLess` tiebreaker (total order)
- [x] Mempool map iterations that feed into `BlockTemplate` are followed by `sort.Slice` — PASS: `BlockTemplate`, `GetAllEntries`, `GetTxHashes` all collect from map then sort
- [x] No float math in fee calculations — PASS: All fee calculations use `uint64` integer arithmetic
- [x] Activation height gating uses integer comparison — PASS: `height >= scriptActivation` where both are `uint32`

## Prior findings to verify

- **Audit Finding 5 (side-chain mempool corruption)**: **FIXED.** `handleBlock` (manager.go:692-694) returns immediately upon `ErrSideChain` before reaching mempool operations at lines 711-720.

## PATH 5 VERDICT: **PASS** — Fully deterministic

---

# PATH 6: Chain Parameters & Genesis

**Scope**: Verify that chain parameters are compile-time constants and that genesis blocks are protocol-pinned, not runtime-derived.

**Point A**: Node starts up and loads chain parameters.
**Point Z**: Genesis block is validated and chain is initialized at height 0.

## Files (read in order)

| # | File | Lines | What to audit |
|---|------|-------|---------------|
| 1 | `internal/params/params.go` | 68 | `ChainParams` struct definition, `CalcSubsidy` |
| 2 | `internal/params/networks.go` | 235 | `Mainnet`, `Testnet`, `Regtest` variable definitions, `InitGenesis` |
| 3 | `internal/params/genesis.go` | 52 | `GenesisConfig`, `BuildGenesisBlock` |
| 4 | `cmd/node/main.go` | 412 | `main`, `initNetworkGenesis`, startup sequence |
| 5 | `internal/chain/chain.go` | 1072 | `Chain.Init` — genesis handling, tip restoration |

## Determinism checklist

- [x] `Mainnet.GenesisBlock` and `Mainnet.GenesisHash` are hardcoded non-zero values (audit Finding 1) — PASS: Fully hardcoded in networks.go:25-68 with non-zero GenesisHash
- [x] `Testnet.GenesisBlock` and `Testnet.GenesisHash` are hardcoded non-zero values — PASS: Fully hardcoded in networks.go:97-146 with non-zero GenesisHash
- [x] `initNetworkGenesis` does NOT mine genesis at runtime for mainnet — PASS: Non-zero GenesisHash triggers verify-only path; explicit fatal guard at line 328-331
- [x] `BuildGenesisBlock` is deterministic given fixed config (timestamp, bits, reward, script) — PASS: Pure function in genesis.go:22-52, no randomness, no I/O, no time calls
- [x] `Chain.Init` verifies stored genesis matches params genesis — PASS: chain.go:240-241 computes hash from header and compares to `c.params.GenesisHash`, returns error on mismatch
- [x] All `ChainParams` fields are constants (no runtime derivation) — PASS: All fields are literal values or compile-time expressions
- [x] `CalcSubsidy` uses only integer arithmetic — PASS: params.go:59-68, `uint64` division and bit-shift only
- [x] `ActivationHeights` map is read-only after initialization — PASS: Initialized as empty map, only read via `p.ActivationHeights["script_validation"]`, no write paths after init
- [x] `InitGenesis` only mutates params during startup, not during consensus — PASS: Only called from `initNetworkGenesis` in cmd/node/main.go, runs once before chain.Init()
- [x] No environment variables or config files influence consensus parameters — PASS: Zero `os.Getenv` in params package. Config struct contains only operational fields.

## Prior findings to verify

- **Audit Finding 1 (runtime genesis)**: **FIXED.** Mainnet genesis fully hardcoded in networks.go:25-68. Two layers of protection: non-zero hash triggers verify-only path, and explicit fatal guard prevents runtime mining for mainnet.

## PATH 6 VERDICT: **PASS** — Fully deterministic

---

# PATH 7: P2P Block/Transaction Delivery & Consensus Boundary

**Scope**: Trace how blocks and transactions arrive from the network and enter the consensus pipeline. Verify the P2P layer cannot corrupt consensus state.

**Point A**: A TCP message arrives on a peer connection.
**Point Z**: `chain.ProcessBlock` or `mempool.AddTx` is called with deserialized data.

## Files (read in order)

| # | File | Lines | What to audit |
|---|------|-------|---------------|
| 1 | `internal/protocol/messages.go` | 428 | Message encoding/decoding, header parsing, all `Cmd*` handlers |
| 2 | `internal/protocol/checksum.go` | 13 | Message checksum computation |
| 3 | `internal/p2p/peer.go` | 207 | `Peer` struct, `readLoop`, `writeLoop`, `Send` |
| 4 | `internal/p2p/manager.go` | 840 | `handleMessage`, `handleBlock`, `handleTx`, `handleInv`, `handleGetData`, `handleGetBlocks`, `syncLoop`, `seenBlocks` |
| 5 | `internal/p2p/discovery/discovery.go` | 59 | Peer discovery (non-consensus, but verify no state leakage) |

## Determinism checklist

- [x] Message deserialization uses the same canonical format as serialization (PATH 1) — PASS: All message types use symmetric Encode/Decode with identical field order, byte widths, and LittleEndian encoding
- [x] Checksum is DoubleSHA256 first-4-bytes (deterministic) — PASS: checksum.go:7-13, `SHA256(SHA256(data))[:4]`, verified independently in peer.go
- [x] `handleBlock` deserializes block, then calls `chain.ProcessBlock` — no mutation between — PASS: Only hash computation and seenBlocks check intervene, neither mutates the block
- [x] `handleTx` deserializes tx, then calls `mempool.AddTx` — no mutation between — PASS: Only hash computation and seenTxs check intervene, neither mutates the tx
- [x] `handleInv` does NOT skip blocks that are only known as orphans (WORKFILE RC-2) — PASS: manager.go:636 uses `HasBlockOnChain` (active chain only), orphans not in `heightByHash`
- [x] `seenBlocks` does NOT cache rejected blocks permanently (WORKFILE RC-2) — PASS: manager.go:704 calls `m.seenBlocks.Remove(blockHash)` for rejected blocks
- [x] `handleBlock` returns early for `ErrSideChain` before updating mempool (audit Finding 5) — PASS: manager.go:692-695, returns before mempool operations at lines 712-720
- [x] `syncLoop` uses dynamically updated peer heights (WORKFILE RC-7) — PASS: `handleBlock` calls `peer.SetStartHeightIfGreater(height)` after successful processing, syncLoop reads live heights
- [x] No consensus decisions are made in P2P layer (all delegated to chain/mempool) — PASS: All validation delegated to `chain.ProcessBlock` and `mempool.AddTx`
- [x] Goroutine safety: `handleBlock` holds appropriate locks before calling `ProcessBlock` — PASS: Does not hold P2P locks during `ProcessBlock` (which acquires its own `c.mu.Lock()`), avoiding deadlock
- [x] `handleGetBlocks` returns blocks in height order (not map order) — PASS: manager.go:781, sequential height iteration `for h := startHeight + 1; h <= tipHeight`
- [x] Inventory deduplication uses `sync.Map` (thread-safe, no ordering dependency) — PASS: Uses `boundedHashSet` with `sync.Mutex` (superior to sync.Map — bounded memory with FIFO eviction)

## Prior findings to verify

- **WORKFILE RC-2**: **FIXED.** `seenBlocks.Remove(blockHash)` called for rejected blocks (manager.go:704). `handleInv` uses `HasBlockOnChain` (active chain only) instead of `HasBlock` (manager.go:636).
- **WORKFILE RC-4**: **FIXED.** `CmdAddr` handler stores received peer addresses (manager.go:610-618). `AddrMsg` type with encode/decode in protocol. `reconnectLoop` loads stored peers.
- **WORKFILE RC-7**: **FIXED.** `peer.SetStartHeightIfGreater` monotonically updates peer height (peer.go:89-98). Called after every successful block acceptance (manager.go:723). `syncLoop` reads live heights each tick.
- **Audit Finding 5**: **FIXED.** `ErrSideChain` triggers early return (manager.go:692-695) before mempool operations.

## PATH 7 VERDICT: **PASS** — Fully deterministic

---

# PATH 8: Storage Layer & Persistence Integrity

**Scope**: Verify that all consensus-critical data written to disk can be read back identically, and that write ordering cannot corrupt state.

**Point A**: Consensus code calls a `BlockStore` method to persist data.
**Point Z**: Data is retrievable via the corresponding read method with identical content.

## Files (read in order)

| # | File | Lines | What to audit |
|---|------|-------|---------------|
| 1 | `internal/store/store.go` | 66 | `BlockStore` interface, `DiskBlockIndex` struct, `CalcWork` |
| 2 | `internal/store/filestore.go` | 188 | `FileStore` — composite store, `Init`, `Close` |
| 3 | `internal/store/blockfile.go` | 239 | `BlockFileStore` — `blk*.dat` / `rev*.dat` flat file I/O |
| 4 | `internal/store/blockindex.go` | 228 | `BlockIndex` — LevelDB block index, `PutBlockIndex`, `GetBlockIndex`, `ForEachBlockIndex`, serialization |
| 5 | `internal/store/chainstate.go` | 135 | `ChainstateDB` — LevelDB UTXO, `PutUtxo`, `GetUtxo`, `DeleteUtxo`, write batches |
| 6 | `internal/store/bolt.go` | 205 | `BoltStore` — peer storage (non-consensus, verify isolation) |

## Determinism checklist

- [x] `DiskBlockIndex` serialization is canonical (explicit field order, LE encoding) — PASS: Fixed field order with explicit LE encoding via `binary.LittleEndian.PutUint*`, `big.Int` bytes with length prefix
- [x] `DiskBlockIndex` deserialization roundtrips: `Deserialize(Serialize(x)) == x` — PASS: Symmetric format, tested in store_test.go
- [x] Block flat file writes are append-only (no in-place mutation) — PASS: `WriteBlock` seeks to end of file, writes magic+size frame then block bytes
- [x] Undo flat file writes are append-only — PASS: `WriteUndo` seeks to end of file, writes frame with DoubleSHA256 checksum
- [x] Block index writes use sync options (WORKFILE RC-5) — PASS: `PutBlockIndex` at blockindex.go:143 uses `&opt.WriteOptions{Sync: true}`
- [x] Chainstate write batches are atomic (LevelDB WriteBatch) — PASS: `ChainstateWriteBatch` wraps `leveldb.Batch`, flushed atomically with `Sync: true`
- [x] `GetBlock` reads using (fileNum, offset, size) from index — verify offset/size correctness — PASS: Reads at exact offset, validates magic bytes and size frame
- [x] `ReadUndo` reads using (fileNum, offset, size) — verify correctness after reorg (audit Finding 3) — PASS: Validates magic bytes and DoubleSHA256 checksum. Undo written once per block, offsets correct.
- [x] `ForEachBlockIndex` iteration order does not affect consensus (used only for init/rebuild) — PASS: Called only in chain.go:113 during Init() to populate `heightByHash` map. All entries with StatusHaveData are added regardless of order.
- [x] `HasBlock` checks index, not flat file — verify this doesn't return true for orphaned blocks (WORKFILE RC-3) — PASS (with note): `HasBlock` checks LevelDB index. P2P layer uses `HasBlockOnChain` (active chain only) for inventory filtering, so orphaned blocks in index don't prevent re-requesting.
- [x] No consensus data stored in BoltStore (peers only) — PASS: `BoltStore` satisfies only `PeerStore`. Legacy methods used exclusively by migration tool.
- [x] File locking prevents concurrent node instances from corrupting data — PASS: `lockfile.go` uses `syscall.Flock` (Unix) / `windows.LockFileEx` (Windows) for exclusive lock
- [x] Height-to-bytes in block index uses big-endian for lexicographic ordering (WORKFILE invariant 11) — PASS: `heightToBytes` uses `binary.BigEndian.PutUint32`

## Prior findings to verify

- **WORKFILE RC-3**: **FIXED.** `PutBlockIndex` called only after validation succeeds. P2P uses `HasBlockOnChain` for inventory filtering.
- **WORKFILE RC-5**: **FIXED.** `PutBlockIndex` at blockindex.go:143 uses `&opt.WriteOptions{Sync: true}`. All other consensus-critical LevelDB writes also use Sync.
- **Audit Finding 3**: **FIXED.** Undo data written exactly once per block in post-connect loop (chain.go:722-738). No double-write.
- **Audit Finding 4**: **FIXED.** `store.CalcWork` at store.go:64-66 is a one-line delegation: `return crypto.CalcWork(bits)`.

## PATH 8 VERDICT: **PASS** — Fully deterministic

---

# Cross-Path Verification Matrix

All 8 paths audited. Cross-path invariants verified:

| Invariant | Paths | Status | Evidence |
|-----------|-------|--------|----------|
| Block hash identity | 1, 3, 7, 8 | **PASS** | `crypto.HashBlockHeader` is the sole block hash function across all call sites (chain.go, manager.go, pow/engine.go, miner.go, rpc). No alternative computation exists. |
| Tx hash identity | 1, 5, 7 | **PASS** | `crypto.HashTransaction` is the sole tx hash function across txvalidation.go, mempool.go, manager.go, utxo.go, merkle.go. No alternative computation exists. |
| Merkle root | 1, 3 | **PASS** | `crypto.ComputeMerkleRoot` used identically in `ValidateBlockStructure` (validation.go:41) and miner (miner.go:95). Same function, same inputs, same output. |
| UTXO consistency | 3, 4, 8 | **PASS** | `ConnectBlock` produces `BlockUndoData` → serialized → written to rev file → read back → deserialized → consumed by `DisconnectBlock`. Symmetric format, roundtrip verified. |
| Undo roundtrip | 4, 8 | **PASS** | `SerializeUndoData` → `WriteUndo` (append-only with checksum) → `ReadUndo` (validates magic + checksum) → `DeserializeUndoData`. Fuzz tested in utxo_test.go. |
| Work calculation | 2, 3, 8 | **PASS** | `crypto.CalcWork` is the sole implementation. `store.CalcWork` delegates directly (store.go:65). No duplicate `compactToBig` exists. |
| Subsidy | 2, 4, 5 | **PASS** | `p.CalcSubsidy(height)` used identically in miner (miner.go:83), block validation (txvalidation.go:139), and tx validation. Same function, same params, same height. |
| Genesis | 6, 3, 8 | **PASS** | Mainnet/Testnet genesis hardcoded in networks.go. Verified at startup by `initNetworkGenesis` and `Chain.Init`. Stored to flat files and block index on first run. |

---

# Summary of Findings — Final Status

All findings from the prior audit and WORKFILE have been verified:

| ID | Finding | Verified in Path | Status |
|----|---------|-------------------|--------|
| Audit-F1 | Runtime genesis for mainnet | PATH 6 | **FIXED** — Mainnet genesis hardcoded in networks.go:25-68 with fatal guard against runtime mining |
| Audit-F2 | Varint malleability | PATH 1 | **FIXED** — `ReadVarInt` and `ReadVarIntFromBytes` reject non-minimal encodings |
| Audit-F3 | Double undo write in reorg | PATH 4, PATH 8 | **FIXED** — Single undo write in post-connect loop (chain.go:722-739) |
| Audit-F4 | Duplicate CalcWork | PATH 2, PATH 8 | **FIXED** — `store.CalcWork` delegates to `crypto.CalcWork` (store.go:65) |
| Audit-F5 | Side-chain mempool corruption | PATH 5, PATH 7 | **FIXED** — `handleBlock` returns early on `ErrSideChain` (manager.go:692-695) |
| Audit-F6 | Silent partial work | PATH 3 | **FIXED** — `workForParentChain` reads stored ChainWork, returns explicit error (chain.go:908-917) |
| RC-1 | Side-chain ancestor lookup | PATH 3 | **FIXED** — `buildAncestorLookup` walks actual parent chain (chain.go:937-971) |
| RC-2 | seenBlocks cache poisoning | PATH 7 | **FIXED** — Rejected blocks removed from seenBlocks; `handleInv` uses `HasBlockOnChain` |
| RC-3 | Premature DiskBlockIndex write | PATH 3, PATH 8 | **FIXED** — `PutBlockIndex` called only after validation. P2P uses `HasBlockOnChain` for inventory. |
| RC-4 | No addr gossip | PATH 7 | **FIXED** — `CmdAddr` handler implemented, `reconnectLoop` loads stored peers |
| RC-5 | Block index sync writes | PATH 8 | **FIXED** — `PutBlockIndex` uses `&opt.WriteOptions{Sync: true}` |
| RC-6 | O(n) workForParentChain | PATH 3 | **FIXED** — O(1) lookup via stored `ChainWork` in `DiskBlockIndex` |
| RC-7 | Static peer heights in sync | PATH 7 | **FIXED** — `SetStartHeightIfGreater` called after each block acceptance, syncLoop reads live heights |
| RC-8 | Unsafe reorg (disconnect before validate) | PATH 3, PATH 4 | **FIXED** — Snapshot-and-restore pattern with deferred rollback on failure |

---

# AUDIT COMPLETE — OVERALL VERDICT: **PASS**

All 8 paths pass the deterministic consensus audit. All 6 prior audit findings and all 8 RC issues are confirmed fixed. All 8 cross-path invariants are verified. No consensus-critical non-determinism was found in the codebase.
