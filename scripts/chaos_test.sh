#!/usr/bin/env bash
set -uo pipefail

# ==========================================================================
# FAIRCHAIN 10-NODE CHAOS TEST
#
# Architecture (mirrors real networks):
#   Nodes 0,1  = SEED nodes (relay-only, no mining — network backbone)
#   Nodes 2-9  = MINER nodes (connect to seeds, subject to chaos)
#
# Seed nodes bootstrap the network. Miners connect to seeds, discover the
# chain state from them, and mine. Blocks propagate through seeds to all
# miners. Shallow reorgs are natural and expected; convergence happens
# via longest-chain rule.
#
# Testnet params: 5s target blocks, retarget every 20 blocks.
# ==========================================================================

BIN="$(cd "$(dirname "$0")/.." && pwd)/bin/fairchain-node"
BASEDIR="/tmp/fairchain-chaos"
NUM_NODES=10
SEED_NODES=(0 1)
MINER_NODES=(2 3 4 5 6 7 8 9)
BASE_P2P_PORT=30000
BASE_RPC_PORT=31000
PIDS=()

SEED_ADDRS="127.0.0.1:$((BASE_P2P_PORT + 0)),127.0.0.1:$((BASE_P2P_PORT + 1))"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log()   { echo -e "${CYAN}[chaos]${NC} $*"; }
pass()  { echo -e "${GREEN}[PASS]${NC}  $*"; }
fail()  { echo -e "${RED}[FAIL]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
header(){ echo -e "\n${BOLD}━━━ $* ━━━${NC}"; }

cleanup() {
    log "Cleaning up all nodes..."
    for i in $(seq 0 $((NUM_NODES - 1))); do
        kill "${PIDS[$i]:-}" 2>/dev/null || true
    done
    sleep 2
    pkill -9 -f "fairchain-node.*fairchain-chaos" 2>/dev/null || true
    sleep 1
}
trap cleanup EXIT

# ── RPC helpers ──────────────────────────────────────────────

get_info() {
    curl -s --connect-timeout 2 --max-time 3 "http://127.0.0.1:${1}/getblockchaininfo" 2>/dev/null
}

get_field() {
    local port=$1 field=$2
    get_info "$port" | python3 -c "import sys,json;print(json.load(sys.stdin)['$field'])" 2>/dev/null || echo "ERR"
}

get_height()     { get_field "$1" blocks; }
get_hash()       { get_field "$1" best_block_hash; }
get_bits()       { get_field "$1" bits; }
get_difficulty() { get_info "$1" | python3 -c "import sys,json;print(f\"{json.load(sys.stdin)['difficulty']:.4f}\")" 2>/dev/null || echo "ERR"; }
get_peers()      { get_field "$1" peers; }
get_epoch()      { get_info "$1" | python3 -c "import sys,json;d=json.load(sys.stdin);print(f\"epoch={d['retarget_epoch']} prog={d['epoch_progress']}/{d['retarget_interval']}\")" 2>/dev/null || echo "ERR"; }

# ── Node management ─────────────────────────────────────────

start_node() {
    local idx=$1
    local do_mine=${2:-false}
    local p2p_port=$((BASE_P2P_PORT + idx))
    local rpc_port=$((BASE_RPC_PORT + idx))
    local datadir="${BASEDIR}/node${idx}"
    mkdir -p "$datadir"

    local mine_flag=""
    if [ "$do_mine" = "true" ]; then
        mine_flag="--mine"
    fi

    "$BIN" \
        --network testnet \
        --datadir "$datadir" \
        --listen "127.0.0.1:${p2p_port}" \
        --rpc "127.0.0.1:${rpc_port}" \
        --seed-peers "$SEED_ADDRS" \
        ${mine_flag} \
        > "${datadir}/stdout.log" 2>&1 &

    PIDS[$idx]=$!
    local role="miner"
    [ "$do_mine" = "false" ] && role="SEED"
    log "  Node $idx [$role] started (pid=${PIDS[$idx]}, p2p=:${p2p_port}, rpc=:${rpc_port})"
}

stop_node() {
    local idx=$1
    if [ -n "${PIDS[$idx]:-}" ] && kill -0 "${PIDS[$idx]}" 2>/dev/null; then
        kill "${PIDS[$idx]}" 2>/dev/null || true
        wait "${PIDS[$idx]}" 2>/dev/null || true
        log "  Node $idx stopped (pid=${PIDS[$idx]})"
        PIDS[$idx]=""
    fi
}

is_alive() {
    local idx=$1
    [ -n "${PIDS[$idx]:-}" ] && kill -0 "${PIDS[$idx]}" 2>/dev/null
}

# ── Status / Checks ─────────────────────────────────────────

print_cluster_status() {
    local label=$1
    echo ""
    log "=== Cluster Status: $label ==="
    printf "  %-8s %-8s %-8s %-10s %-10s %-30s %s\n" "Node" "Height" "Peers" "Bits" "Diff" "Epoch" "Hash(prefix)"
    printf "  %-8s %-8s %-8s %-10s %-10s %-30s %s\n" "--------" "------" "-----" "--------" "--------" "-----" "------------"
    for i in $(seq 0 $((NUM_NODES - 1))); do
        local rpc=$((BASE_RPC_PORT + i))
        local role="miner"
        [[ " ${SEED_NODES[*]} " == *" $i "* ]] && role="SEED "
        if is_alive "$i"; then
            local h=$(get_height "$rpc")
            local p=$(get_peers "$rpc")
            local b=$(get_bits "$rpc")
            local d=$(get_difficulty "$rpc")
            local e=$(get_epoch "$rpc")
            local hash=$(get_hash "$rpc")
            printf "  %-8s %-8s %-8s %-10s %-10s %-30s %.20s...\n" "[$i]$role" "$h" "$p" "$b" "$d" "$e" "$hash"
        else
            printf "  %-8s %-8s\n" "[$i]$role" "DOWN"
        fi
    done
    echo ""
}

wait_for_height() {
    local min_height=$1
    local timeout=$2
    local label=$3
    local deadline=$((SECONDS + timeout))

    log "Waiting up to ${timeout}s for height >= $min_height ($label)..."
    while [ $SECONDS -lt $deadline ]; do
        for i in $(seq 0 $((NUM_NODES - 1))); do
            if is_alive "$i"; then
                local rpc=$((BASE_RPC_PORT + i))
                local h=$(get_height "$rpc")
                if [ "$h" != "ERR" ] && [ "$h" -ge "$min_height" ] 2>/dev/null; then
                    pass "$label: height $h >= $min_height reached"
                    return 0
                fi
            fi
        done
        sleep 3
    done
    warn "$label: height $min_height not reached within ${timeout}s"
    return 1
}

wait_for_convergence() {
    local timeout=$1
    local label=$2
    local tolerance=${3:-2}
    local deadline=$((SECONDS + timeout))

    log "Waiting up to ${timeout}s for convergence ($label, tolerance=$tolerance)..."
    while [ $SECONDS -lt $deadline ]; do
        local heights=()
        local max_h=0
        local min_h=999999
        local total_alive=0

        for i in $(seq 0 $((NUM_NODES - 1))); do
            if is_alive "$i"; then
                local rpc=$((BASE_RPC_PORT + i))
                local h=$(get_height "$rpc")
                [ "$h" = "ERR" ] || [ "$h" = "-1" ] && continue
                ((total_alive++))
                heights+=("$h")
                [ "$h" -gt "$max_h" ] && max_h=$h
                [ "$h" -lt "$min_h" ] && min_h=$h
            fi
        done

        if [ ${#heights[@]} -ge 2 ]; then
            local spread=$((max_h - min_h))
            if [ "$spread" -le "$tolerance" ]; then
                pass "$label: ${#heights[@]} nodes converged (spread=$spread, range=[${min_h}..${max_h}])"
                return 0
            fi
        fi
        sleep 3
    done

    warn "$label: Convergence timeout"
    return 1
}

check_consensus() {
    local label=$1
    local heights=()
    local max_h=0
    local min_h=999999

    for i in $(seq 0 $((NUM_NODES - 1))); do
        if is_alive "$i"; then
            local rpc=$((BASE_RPC_PORT + i))
            local h=$(get_height "$rpc")
            [ "$h" = "ERR" ] || [ "$h" = "-1" ] && continue
            heights+=("$h")
            [ "$h" -gt "$max_h" ] && max_h=$h
            [ "$h" -lt "$min_h" ] && min_h=$h
        fi
    done

    if [ ${#heights[@]} -lt 2 ]; then
        warn "$label: fewer than 2 live nodes"
        return 0
    fi

    local spread=$((max_h - min_h))
    if [ "$spread" -le 3 ]; then
        pass "$label: ${#heights[@]} nodes, range=[${min_h}..${max_h}] spread=$spread — consensus healthy"
        return 0
    else
        fail "$label: DIVERGENCE — ${#heights[@]} nodes, range=[${min_h}..${max_h}] spread=$spread"
        return 1
    fi
}

check_retarget() {
    for i in $(seq 0 $((NUM_NODES - 1))); do
        if is_alive "$i"; then
            local rpc=$((BASE_RPC_PORT + i))
            local bits=$(get_bits "$rpc")
            local diff=$(get_difficulty "$rpc")
            local height=$(get_height "$rpc")
            local epoch=$(get_epoch "$rpc")
            log "Retarget check at height $height: bits=$bits difficulty=$diff $epoch"
            if [ "$bits" != "1e0fffff" ] && [ "$bits" != "ERR" ]; then
                pass "Difficulty retargeted: 1e0fffff → $bits (diff=$diff)"
                return 0
            else
                warn "Difficulty still at initial"
                return 1
            fi
        fi
    done
}

FAILURES=0
run_check() { if ! "$@"; then ((FAILURES++)); fi; }

# ============================================================
# MAIN TEST SEQUENCE
# ============================================================

echo ""
echo "════════════════════════════════════════════════════"
echo " FAIRCHAIN 10-NODE CHAOS TEST"
echo " 2 Seed Nodes (relay) + 8 Miners · 5s blocks · retarget/20"
echo "════════════════════════════════════════════════════"

# ── Phase 0: Clean slate ────────────────────────────────────
header "Phase 0: Clean Environment"
rm -rf "$BASEDIR"
mkdir -p "$BASEDIR"

# ── Phase 1: Launch seed nodes (relay-only, no mining) ──────
header "Phase 1a: Launch Seed Nodes (relay-only)"
start_node 0 false
start_node 1 false
log "Waiting 4s for seeds to peer with each other..."
sleep 4

header "Phase 1b: Launch Miner Nodes"
for i in "${MINER_NODES[@]}"; do
    start_node "$i" true
    sleep 0.2
done
log "Waiting 12s for mesh formation and initial mining..."
sleep 12

print_cluster_status "Initial Launch"
run_check check_consensus "Phase 1"

# ── Phase 2: Mine through first retarget ────────────────────
header "Phase 2: Mine Through First Retarget (height ≥ 25)"
wait_for_height 25 120 "Phase 2"
wait_for_convergence 30 "Phase 2 convergence" 3
print_cluster_status "After First Retarget"
run_check check_consensus "Phase 2"
run_check check_retarget

# ── Phase 3: Kill 3 miner nodes ────────────────────────────
header "Phase 3: CHAOS — Kill miners 3, 5, 7"
stop_node 3; stop_node 5; stop_node 7
sleep 2
print_cluster_status "After Killing 3 Miners"

log "Letting survivors mine for 20s..."
sleep 20

wait_for_convergence 20 "Phase 3 survivors"
print_cluster_status "Survivors Mining"
run_check check_consensus "Phase 3 (5 miners + 2 seeds)"

# ── Phase 4: Restart killed miners (fresh sync) ────────────
header "Phase 4: Restart Killed Miners (fresh sync from seeds)"
for i in 3 5 7; do
    rm -rf "${BASEDIR}/node${i}"
    start_node "$i" true
done

log "Waiting for restarted miners to sync and converge..."
wait_for_convergence 45 "Phase 4 sync" 3
print_cluster_status "After Restart & Sync"
run_check check_consensus "Phase 4 (all 10)"

# ── Phase 5: Kill one seed ──────────────────────────────────
header "Phase 5: CHAOS — Kill SEED 0 (network runs on single seed)"
stop_node 0
sleep 2
log "Running with 1 seed for 20s..."
sleep 20

wait_for_convergence 20 "Phase 5"
print_cluster_status "One Seed Down"
run_check check_consensus "Phase 5 (1 seed)"

# ── Phase 6: Restore seed 0, kill seed 1 ───────────────────
header "Phase 6: Seed Swap — restore seed 0, kill seed 1"
rm -rf "${BASEDIR}/node0"
start_node 0 false
sleep 4
stop_node 1
sleep 2
log "Running with swapped seed for 25s..."
sleep 25

wait_for_convergence 30 "Phase 6"
print_cluster_status "Seed Swap"
run_check check_consensus "Phase 6 (seed swap)"

# ── Phase 7: Restore seed 1, kill majority of miners ───────
header "Phase 7: Restore seed 1, kill 5 miners (2,4,6,8,9)"
rm -rf "${BASEDIR}/node1"
start_node 1 false
sleep 3
for i in 2 4 6 8 9; do stop_node "$i"; done
sleep 2

log "Minority (seeds + miners 3,5,7) mining for 20s..."
sleep 20

wait_for_convergence 20 "Phase 7 minority"
print_cluster_status "Minority Mining"
run_check check_consensus "Phase 7 (5 nodes)"

# ── Phase 8: Restore all miners ────────────────────────────
header "Phase 8: Restore all killed miners (fresh sync)"
for i in 2 4 6 8 9; do
    rm -rf "${BASEDIR}/node${i}"
    start_node "$i" true
    sleep 0.2
done

wait_for_convergence 60 "Phase 8 full restore" 3
print_cluster_status "Full Restoration"
run_check check_consensus "Phase 8 (all 10)"

# ── Phase 9: Rapid kill/restart chaos ──────────────────────
header "Phase 9: CHAOS — Rapid kill/restart (5 rounds, miners only)"
for round in $(seq 1 5); do
    victim=${MINER_NODES[$((RANDOM % ${#MINER_NODES[@]}))]}
    log "  Round $round: kill miner $victim"
    stop_node "$victim"
    sleep 4
    log "  Round $round: restart miner $victim (fresh)"
    rm -rf "${BASEDIR}/node${victim}"
    start_node "$victim" true
    sleep 6
done

wait_for_convergence 30 "Phase 9"
print_cluster_status "After Rapid Chaos"
run_check check_consensus "Phase 9"

# ── Phase 10: Final summary ────────────────────────────────
header "Phase 10: Final Retarget & Consensus Verification"
for i in "${SEED_NODES[@]}"; do
    if is_alive "$i"; then
        rpc=$((BASE_RPC_PORT + i))
        echo ""
        log "Full chain info from SEED node $i:"
        curl -s "http://127.0.0.1:${rpc}/getblockchaininfo" 2>/dev/null | python3 -m json.tool || true
        run_check check_retarget
        break
    fi
done

run_check check_consensus "FINAL"

echo ""
echo "════════════════════════════════════════════════════"
if [ "$FAILURES" -eq 0 ]; then
    echo -e " ${GREEN}ALL CHECKS PASSED${NC}"
else
    echo -e " ${RED}$FAILURES CHECK(S) FAILED${NC}"
fi
echo "════════════════════════════════════════════════════"
echo ""

exit "$FAILURES"
