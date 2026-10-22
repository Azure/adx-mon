package cluster

import (
	"github.com/Azure/adx-mon/pkg/wal"
	"sync"
	"time"
)

type backoffKey struct {
	peer        string
	destination wal.Destination
}
type destinationBackoff struct {
	mu        sync.Mutex
	deadlines map[backoffKey]time.Time
}

func (b *destinationBackoff) deferUntil(peer string, d wal.Destination, deadline time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.deadlines == nil {
		b.deadlines = make(map[backoffKey]time.Time)
	}
	for k, t := range b.deadlines {
		if !t.After(time.Now()) {
			delete(b.deadlines, k)
		}
	}
	b.deadlines[backoffKey{peer, d}] = deadline
}

func (b *destinationBackoff) ready(peer string, d wal.Destination, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := backoffKey{peer, d}
	deadline := b.deadlines[key]
	if deadline.After(now) {
		return false
	}
	delete(b.deadlines, key)
	return true
}
