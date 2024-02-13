package adx

import (
	"errors"
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

func TestSanitizerErrorString(t *testing.T) {
	err := errors.New("https://mystoragequeue.queue.core.windows.net/someaccount/myTable?se=2024-02-09T10%3A23%3A23Z&sig=SomeMagicalS3cr3tString%3D&sp=a&st=2024-02-08T22%3A18%3A23Z&sv=2022-11-02")
	require.Contains(t, sanitizeErrorString(err).Error(), "sig=REDACTED")
	require.NotContains(t, sanitizeErrorString(err).Error(), "SomeMagicalS3cr3tString")

	err = errors.New(`Failed to upload file: Op(OpFileIngest): Kind(KBlobstore): -> github.com/Azure/azure-pipeline-go/pipeline.NewError, /app/3rdparty/adx-mon/vendor/github.com/Azure/azure-pipeline-go/pipeline/error.go:157\nHTTP request failed\n\nPost \"https://mystoragequeue.queue.core.windows.net/mystorageaccount/myqueue?se=2024-02-09T10%3A23%3A23Z&sig=SomeS3cretThatIsnotPublic149%3D&sp=a&st=2024-02-08T22%3A18%3A23Z&sv=2022-11-02&visibilitytimeout=0\": dial tcp 20.60.109.47:443: connect: connection refused\n`)
	require.Contains(t, sanitizeErrorString(err).Error(), "sig=REDACTED")
	require.NotContains(t, sanitizeErrorString(err).Error(), "SomeS3cretThatIsnotPublic149")
}
