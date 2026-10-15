package cluster

import (
	"context"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientScopedOverload(t *testing.T) {
	for _, scope := range []string{"table", "instance", "", "unknown"} {
		t.Run(scope, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(OverloadScopeHeader, scope)
				w.Header().Set(OverloadReasonHeader, "bytes")
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(429)
			}))
			defer s.Close()
			c, err := NewClient(ClientOpts{DisableGzip: true, Timeout: time.Second})
			require.NoError(t, err)
			err = c.Write(context.Background(), s.URL, "DB_Table_schema_123.wal", strings.NewReader("data"))
			if scope == "table" {
				var quota *wal.TableOverloadedError
				require.ErrorAs(t, err, &quota)
				require.Equal(t, wal.Destination{Database: "DB", Table: "Table"}, quota.Destination)
				require.Equal(t, 7*time.Second, quota.RetryAfter)
			} else {
				require.ErrorIs(t, err, ErrPeerOverloaded)
			}
		})
	}
}

func TestRetryDelayBounded(t *testing.T) {
	require.Equal(t, 5*time.Second, retryDelay("bad"))
	require.Equal(t, 5*time.Second, retryDelay("-1"))
	require.Equal(t, 300*time.Second, retryDelay("10000"))
}
