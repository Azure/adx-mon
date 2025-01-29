package cluster

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestPartitioner(t *testing.T) {
	hostname, err := os.Hostname()
	require.NoError(t, err)
	p, err := NewPartition(map[string]string{"node": "http://127.0.0.1:9090/receive"})
	require.NoError(t, err)
	owner, _ := p.Owner([]byte("kube_node_status_condition"))
	require.NotEqual(t, hostname, owner)
	require.Equal(t, "node", owner)

}

func TestOwner_Empty(t *testing.T) {
	p, err := NewPartition(map[string]string{"node": "http://172.31.63.27:9090/receive"})
	require.NoError(t, err)

	errgroup, _ := errgroup.WithContext(context.Background())
	for i := 0; i < 10; i++ {
		errgroup.Go(func() error {
			for i := 0; i < 100000; i++ {
				if owner, _ := p.Owner([]byte("cpu")); owner == "" {
					return fmt.Errorf("owner is empty")
				}
			}
			return nil
		})
	}
	require.NoError(t, errgroup.Wait())
}

//
// func TestOwner_Balance(t *testing.T) {
//	p, err := NewPartition(map[string]string{"node1": "http://172.31.61.27:9090/receive", "node2": "http://172.31.62.42:9090/receive"})
//	require.NoError(t, err)
//
//	println(p.Owner([]byte("cpu")))
//	println(p.Owner([]byte("mem")))
//	println(p.Owner([]byte("net")))
//	println(p.Owner([]byte("disk")))
//
// }
