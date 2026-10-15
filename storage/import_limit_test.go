package storage_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
)

// A transfer larger than the destination's entire budget can never be admitted
// by draining, so it is reported as permanently oversized and the body is not
// buffered beyond the limit.
func TestImportRejectsOversizedPayload(t *testing.T) {
	budget := wal.NewAdmission(wal.AdmissionOpts{Enforce: true, Default: wal.Usage{Bytes: 100, Segments: 10}})
	s := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), Admission: budget})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, s.Open(ctx))
	defer s.Close()

	body := bytes.NewReader(make([]byte, 10000))
	var oversized *wal.SegmentTooLargeError
	n, err := s.Import("DB_A_s_123.wal", io.NopCloser(body))
	require.ErrorAs(t, err, &oversized)
	require.Zero(t, n)
	require.EqualValues(t, 100, oversized.Limit, "the acceptable size is reported to the sender")
	require.EqualValues(t, 101, body.Size()-int64(body.Len()), "the body is not buffered past the limit")
	require.Equal(t, wal.Usage{}, budget.Usage())
}
