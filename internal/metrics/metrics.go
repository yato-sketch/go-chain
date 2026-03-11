package metrics

import (
	"encoding/json"
	"sync/atomic"
)

// NodeMetrics holds lightweight atomic counters for node activity.
// Designed for low-overhead inline instrumentation; no external dependencies.
type NodeMetrics struct {
	BlocksAccepted   atomic.Uint64
	BlocksRejected   atomic.Uint64
	BlocksMined      atomic.Uint64
	OrphansReceived  atomic.Uint64
	Reorgs           atomic.Uint64
	ReorgDepthTotal  atomic.Uint64
	PeersConnected   atomic.Int64
	PeersDisconnects atomic.Uint64
	TxsAccepted      atomic.Uint64
	TxsRejected      atomic.Uint64
	BytesReceived    atomic.Uint64
	BytesSent        atomic.Uint64
}

// Global is the process-wide metrics instance.
var Global = &NodeMetrics{}

// Snapshot returns a JSON-serializable snapshot of all counters.
func (m *NodeMetrics) Snapshot() map[string]uint64 {
	return map[string]uint64{
		"blocks_accepted":    m.BlocksAccepted.Load(),
		"blocks_rejected":    m.BlocksRejected.Load(),
		"blocks_mined":       m.BlocksMined.Load(),
		"orphans_received":   m.OrphansReceived.Load(),
		"reorgs":             m.Reorgs.Load(),
		"reorg_depth_total":  m.ReorgDepthTotal.Load(),
		"peers_connected":    uint64(m.PeersConnected.Load()),
		"peers_disconnects":  m.PeersDisconnects.Load(),
		"txs_accepted":       m.TxsAccepted.Load(),
		"txs_rejected":       m.TxsRejected.Load(),
		"bytes_received":     m.BytesReceived.Load(),
		"bytes_sent":         m.BytesSent.Load(),
	}
}

// JSON returns the snapshot as a JSON byte slice.
func (m *NodeMetrics) JSON() ([]byte, error) {
	return json.Marshal(m.Snapshot())
}
