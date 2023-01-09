package query

import (
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParse(t *testing.T) {
	p, err := parser.ParseExpr(`sum by (namespace) (irate(container_cpu_usage_seconds_total{namespace != ""}[60m]))`)
	require.NoError(t, err)
	println(parser.Tree(p))

	v := &KQLTranspiler{}
	require.NoError(t, v.Walk(p))
	println(v.String())

	p, err = parser.ParseExpr(`sum by (namespace) (rate(container_cpu_usage_seconds_total{namespace != ""}[5m]))`)
	require.NoError(t, err)
	println(parser.Tree(p))

	v = &KQLTranspiler{}
	require.NoError(t, v.Walk(p))
	println(v.String())

}

func TestParse_Expression(t *testing.T) {

	p, err := parser.ParseExpr(`sum(kube_pod_container_resource_limits{resource="cpu"}) - sum(kube_node_status_capacity_cpu_cores)`)
	require.NoError(t, err)
	println(parser.Tree(p))

	v := &KQLTranspiler{}
	require.NoError(t, v.Walk(p))
	println(v.String())

}
