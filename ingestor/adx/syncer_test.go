package adx

import (
	"testing"

	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/stretchr/testify/require"
)

func TestSyncer_EnsureMapping(t *testing.T) {
	kcli := kusto.NewMockClient()

	s := NewSyncer(kcli, "db", storage.SchemaMapping{}, PromMetrics)
	name, err := s.EnsureMapping("Test")
	require.NoError(t, err)
	require.Equal(t, "Test_15745692345339290292", name)
}

func TestSyncer_EnsureTable(t *testing.T) {
	kcli := &fakeKustoMgmt{
		expectedQuery: ".create-merge table ['Test'] ()",
	}

	s := NewSyncer(kcli, "db", storage.SchemaMapping{}, PromMetrics)
	require.NoError(t, s.EnsureTable("Test"))
	kcli.Verify(t)
}
