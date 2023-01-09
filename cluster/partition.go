package cluster

import (
	"github.com/tysonmote/rendezvous"
)

type Partitioner struct {
	rv *rendezvous.Hash
}

func NewPartition(nodes []string) (*Partitioner, error) {
	rv := rendezvous.New(nodes...)
	return &Partitioner{rv: rv}, nil
}

func (p *Partitioner) Owner(b []byte) string {
	return p.rv.Get(string(b))
}
