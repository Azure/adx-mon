package cluster

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPartitioner(t *testing.T) {
	p, err := NewPartition(map[string]string{"node": "http://127.0.0.1:9090/receive"})
	require.NoError(t, err)
	println(p.Owner([]byte("kube_node_status_condition")))
}

func TestOwner_Empty(t *testing.T) {
	p, err := NewPartition(map[string]string{"node": "http://172.31.63.27:9090/receive"})
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		go func() {
			for i := 0; i < 100000; i++ {
				if owner, _ := p.Owner([]byte("cpu")); owner == "" {
					t.Fatal("owner is empty")
				}
			}
		}()
	}
}

//
//func TestOwner_Balance(t *testing.T) {
//	p, err := NewPartition(map[string]string{"node1": "http://172.31.61.27:9090/receive", "node2": "http://172.31.62.42:9090/receive"})
//	require.NoError(t, err)
//
//	println(p.Owner([]byte("cpu")))
//	println(p.Owner([]byte("mem")))
//	println(p.Owner([]byte("net")))
//	println(p.Owner([]byte("disk")))
//
//}
