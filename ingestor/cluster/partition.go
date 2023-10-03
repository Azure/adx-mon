package cluster

import (
	"os"
	"sort"

	"github.com/cespare/xxhash"
)

type MetricPartitioner interface {
	Owner([]byte) (string, string)
}

// Partitioner manages the distribution of metrics across nodes.  It uses rendezvous hashing to distribute metrics
// roughly evenly.  When nodes are added or removed, the distribution of metrics will change, but only by a proportional
// amount for each node.  For example, if four nodes exists and a fifth is added, only 20% of the metrics will be reassigned
// to the new node.
type Partitioner struct {
	rv    *Hash
	nodes []string
	addrs map[string]string
}

func NewPartition(nodes map[string]string, hostname string, groupSize int) (*Partitioner, error) {
	if groupSize < 1 {
		groupSize = 1
	}

	// Calculate a rough group size.  This may not be ideal if number of nodes is not evenly divisible by groupSize
	// which could lead to imbalanced partitions.
	totalGroups := uint64(len(nodes) / groupSize)
	if totalGroups < 1 {
		totalGroups = 1
	}

	// See if we can find a more optimal group size that balances the partitions better.
	for i := 1; i <= len(nodes)/2; i++ {
		if len(nodes)%i == 0 && len(nodes)/i <= groupSize {
			totalGroups = uint64(i)
		}
	}

	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	myHash := xxhash.Sum64String(hostname)
	groups := make(map[uint64][]string)

	for k := range nodes {
		x := xxhash.Sum64String(k) % totalGroups
		groups[x] = append(groups[x], k)
	}

	owners := groups[myHash%totalGroups]
	sort.Strings(owners)
	rv := NewRendezvous(owners...)
	return &Partitioner{rv: rv, nodes: owners, addrs: nodes}, nil
}

// Owner returns the hostname and address of the node that owns the given key and the address of that node.
func (p *Partitioner) Owner(b []byte) (string, string) {
	v := p.rv.Get(string(b))
	if v == "" {
		return "", ""
	}
	return v, p.addrs[v]
}
