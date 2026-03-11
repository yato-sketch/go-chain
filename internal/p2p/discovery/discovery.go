package discovery

import (
	"log"

	"github.com/fairchain/fairchain/internal/store"
)

// Discovery manages peer address discovery from multiple sources.
type Discovery struct {
	peerStore store.PeerStore
	seeds     []string
}

// New creates a new Discovery instance.
func New(ps store.PeerStore, seeds []string) *Discovery {
	return &Discovery{
		peerStore: ps,
		seeds:     seeds,
	}
}

// Bootstrap returns the initial set of peer addresses to connect to.
// Combines static seeds with persisted peers.
func (d *Discovery) Bootstrap() []string {
	var addrs []string
	addrs = append(addrs, d.seeds...)

	stored, err := d.peerStore.GetPeers()
	if err != nil {
		log.Printf("[discovery] failed to load stored peers: %v", err)
	} else {
		addrs = append(addrs, stored...)
	}

	return deduplicate(addrs)
}

// AddPeer persists a newly discovered peer address.
func (d *Discovery) AddPeer(addr string) {
	d.peerStore.PutPeer(addr)
}

// RemovePeer removes a peer address from the persistent store.
func (d *Discovery) RemovePeer(addr string) {
	d.peerStore.RemovePeer(addr)
}

func deduplicate(addrs []string) []string {
	seen := make(map[string]struct{}, len(addrs))
	result := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if _, ok := seen[a]; !ok {
			seen[a] = struct{}{}
			result = append(result, a)
		}
	}
	return result
}
