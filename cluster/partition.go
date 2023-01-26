package cluster

import "sort"

// Partitioner manages the distribution of metrics across nodes.  It uses rendezvous hashing to distribute metrics
// roughly evenly.  When nodes are added or removed, the distribution of metrics will change, but only by a proportional
// amount for each node.  For example, if four nodes exists and a fifth is added, only 20% of the metrics will be reassigned
// to the new node.
type Partitioner struct {
	rv    *Hash
	nodes []string
	addrs map[string]string
}

func NewPartition(nodes map[string]string) (*Partitioner, error) {
	owners := make([]string, 0, len(nodes))
	for k := range nodes {
		owners = append(owners, k)
	}
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
