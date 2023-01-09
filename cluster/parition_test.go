package cluster

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPartitioner(t *testing.T) {
	p, err := NewPartition()
	require.NoError(t, err)
	println(p.Owner([]byte("kube_node_status_condition")))
}
