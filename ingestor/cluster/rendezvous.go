// Package cluster implements rendezvous hashing (a.k.a. highest random
// weight hashing). See http://en.wikipedia.org/wiki/Rendezvous_hashing for
// more information.
// This is based off of https://github.com/tysonmote/rendezvous
package cluster

import (
	"bytes"
	"hash"
	"hash/crc32"
	"sort"

	"github.com/cespare/xxhash"
)

var crc32Table = crc32.MakeTable(crc32.Castagnoli)

type Hash struct {
	nodes  nodeScores
	hasher hash.Hash32
}

type nodeScore struct {
	node  []byte
	score uint64
}

// New returns a new Hash ready for use with the given nodes.
func NewRendezvous(nodes ...string) *Hash {
	hash := &Hash{
		hasher: crc32.New(crc32Table),
	}
	hash.Add(nodes...)
	return hash
}

// Add adds additional nodes to the Hash.
func (h *Hash) Add(nodes ...string) {
	for _, node := range nodes {
		h.nodes = append(h.nodes, nodeScore{node: []byte(node)})
	}
}

// Get returns the node with the highest score for the given key. If this Hash
// has no nodes, an empty string is returned.
func (h *Hash) Get(key string) string {
	var maxScore uint64
	var maxNode []byte

	keyBytes := []byte(key)

	for _, node := range h.nodes {
		score := h.hash(node.node, keyBytes)
		if score > maxScore || (score == maxScore && bytes.Compare(node.node, maxNode) < 0) {
			maxScore = score
			maxNode = node.node
		}
	}

	return string(maxNode)
}

// GetN returns no more than n nodes for the given key, ordered by descending
// score. GetN is not goroutine-safe.
func (h *Hash) GetN(n int, key string) []string {
	keyBytes := []byte(key)
	for i := 0; i < len(h.nodes); i++ {
		h.nodes[i].score = h.hash(h.nodes[i].node, keyBytes)
	}
	sort.Sort(h.nodes)

	if n > len(h.nodes) {
		n = len(h.nodes)
	}

	nodes := make([]string, n)
	for i := range nodes {
		nodes[i] = string(h.nodes[i].node)
	}
	return nodes
}

type nodeScores []nodeScore

func (s nodeScores) Len() int      { return len(s) }
func (s nodeScores) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s nodeScores) Less(i, j int) bool {
	if s[i].score == s[j].score {
		return bytes.Compare(s[i].node, s[j].node) < 0
	}
	return s[j].score < s[i].score // Descending
}

func (h *Hash) hash(node, key []byte) uint64 {
	return xxhash.Sum64(append(key, node...))
}
