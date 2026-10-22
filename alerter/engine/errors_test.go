package engine

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	kerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	"github.com/stretchr/testify/require"
)

func TestUnknownDBError_Basic(t *testing.T) {
	err := &UnknownDBError{
		DB:                 "cluster_state",
		AvailableDatabases: []string{"db1", "db2"},
	}

	errMsg := err.Error()
	require.Contains(t, errMsg, `no client for database "cluster_state"`)
	require.Contains(t, errMsg, "--kusto-endpoint")
	require.Contains(t, errMsg, "db1")
	require.Contains(t, errMsg, "db2")
}

func TestUnknownDBError_CaseInsensitiveMatch(t *testing.T) {
	err := &UnknownDBError{
		DB:                   "cluster_state",
		AvailableDatabases:   []string{"Cluster_State", "db2"},
		CaseInsensitiveMatch: "Cluster_State",
	}

	errMsg := err.Error()
	require.Contains(t, errMsg, `no client for database "cluster_state"`)
	require.Contains(t, errMsg, `did you mean "Cluster_State"?`)
	require.Contains(t, errMsg, "case-sensitive")
	require.Contains(t, errMsg, "--kusto-endpoint")
}

func TestUnknownDBError_NoDatabases(t *testing.T) {
	err := &UnknownDBError{
		DB:                 "cluster_state",
		AvailableDatabases: []string{},
	}

	errMsg := err.Error()
	require.Contains(t, errMsg, `no client for database "cluster_state"`)
	require.Contains(t, errMsg, "no databases configured via --kusto-endpoint")
}

func TestUnknownDBError_TruncateOver10(t *testing.T) {
	dbs := make([]string, 15)
	for i := 0; i < 15; i++ {
		dbs[i] = "db" + string(rune('a'+i))
	}

	err := &UnknownDBError{
		DB:                 "unknown",
		AvailableDatabases: dbs,
	}

	errMsg := err.Error()
	require.Contains(t, errMsg, `no client for database "unknown"`)
	require.Contains(t, errMsg, "--kusto-endpoint")

	// Should contain first 10 databases
	for i := 0; i < 10; i++ {
		require.Contains(t, errMsg, dbs[i])
	}

	// Should NOT contain databases 11-15
	for i := 10; i < 15; i++ {
		require.NotContains(t, errMsg, dbs[i])
	}

	// Should show "... and 5 more"
	require.Contains(t, errMsg, "... and 5 more")
}

func TestUnknownDBError_Exactly10Databases(t *testing.T) {
	dbs := make([]string, 10)
	for i := 0; i < 10; i++ {
		dbs[i] = "db" + string(rune('a'+i))
	}

	err := &UnknownDBError{
		DB:                 "unknown",
		AvailableDatabases: dbs,
	}

	errMsg := err.Error()

	// Should contain all 10 databases
	for i := 0; i < 10; i++ {
		require.Contains(t, errMsg, dbs[i])
	}

	// Should NOT have truncation message
	require.NotContains(t, errMsg, "... and")
	require.NotContains(t, errMsg, "more")
}

func TestUnknownDBError_FullMessage(t *testing.T) {
	err := &UnknownDBError{
		DB:                   "CLUSTER_STATE",
		AvailableDatabases:   []string{"cluster_state", "logs", "metrics"}, // Already sorted
		CaseInsensitiveMatch: "cluster_state",
	}

	errMsg := err.Error()
	// The message should be well-structured
	require.True(t, strings.HasPrefix(errMsg, `no client for database "CLUSTER_STATE"`))
	require.Contains(t, errMsg, `did you mean "cluster_state"? (database names are case-sensitive)`)
	require.Contains(t, errMsg, "configured databases via --kusto-endpoint: [cluster_state, logs, metrics]")
}

func remoteEntityResolutionError() error {
	message := "Request is invalid and cannot be processed: Semantic error: SEM0056: Errors occurred while resolving remote entities. Failed to resolve name or pattern 'ManagedClusterSnapshot' in one or more scopes"
	body := fmt.Sprintf(`{"error":{"code":"General_BadRequest","message":%q,"@permanent":true,"innererror":{"code":"SEM0056","message":%q}}}`, message, message)
	return fmt.Errorf("failed to execute kusto query: %w", kerrors.HTTP(
		kerrors.OpQuery,
		"Bad Request",
		http.StatusBadRequest,
		io.NopCloser(bytes.NewBufferString(body)),
		"error from Kusto endpoint",
	))
}

func remoteSchemaCalloutBlockedError(errorType, message string) error {
	body := fmt.Sprintf(`{"error":{"code":"BadRequest_CalloutBlockedByPolicy","message":"Request is invalid and cannot be executed.","@type":%q,"@message":%q,"@failureCode":400,"@permanent":false}}`, errorType, message)
	return fmt.Errorf("failed to execute kusto query: %w", kerrors.HTTP(
		kerrors.OpQuery,
		"Bad Request",
		http.StatusBadRequest,
		io.NopCloser(bytes.NewBufferString(body)),
		"error from Kusto endpoint",
	))
}

func TestIsTransientFailedRequest_RemoteEntityResolution(t *testing.T) {
	const message = "Semantic error: SEM0056: Errors occurred while resolving remote entities. Failed to resolve name or pattern 'ManagedClusterSnapshot'"
	requestError := func(status int, message string) error {
		body := fmt.Sprintf(`{"error":{"message":%q}}`, message)
		return fmt.Errorf("failed to execute kusto query: %w", kerrors.HTTP(
			kerrors.OpQuery,
			http.StatusText(status),
			status,
			io.NopCloser(bytes.NewBufferString(body)),
			"error from Kusto endpoint",
		))
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
		},
		{
			name: "non-Kusto error",
			err:  fmt.Errorf("%s", message),
		},
		{
			name: "wrapped remote entity resolution error",
			err:  remoteEntityResolutionError(),
			want: true,
		},
		{
			name: "case insensitive message",
			err:  requestError(http.StatusBadRequest, strings.ToUpper(message)),
			want: true,
		},
		{
			name: "not a bad request",
			err:  requestError(http.StatusInternalServerError, message),
		},
		{
			name: "different semantic error code",
			err:  requestError(http.StatusBadRequest, strings.ReplaceAll(message, "SEM0056", "SEM0001")),
		},
		{
			name: "missing remote entity resolution message",
			err:  requestError(http.StatusBadRequest, strings.ReplaceAll(message, "resolving remote entities", "resolving entities")),
		},
		{
			name: "missing name resolution message",
			err:  requestError(http.StatusBadRequest, strings.ReplaceAll(message, "Failed to resolve name or pattern", "Unknown entity")),
		},
		{
			name: "OBO token required",
			err:  requestError(http.StatusBadRequest, message+": OBO token is required for cross-cluster communication"),
		},
		{
			name: "unauthorized",
			err:  requestError(http.StatusBadRequest, message+": Caller is not authorized to access the remote cluster"),
		},
		{
			name: "access denied",
			err:  requestError(http.StatusBadRequest, message+": Access denied"),
		},
		{
			name: "callout policy rejection",
			err:  requestError(http.StatusBadRequest, message+": Remote cluster is not allowed by the callout policy"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isTransientFailedRequest(tt.err))
		})
	}
}

func TestIsTransientFailedRequest_CalloutPolicy(t *testing.T) {
	const exceptionType = "Kusto.DataNode.Exceptions.RemoteSchemaCalloutBlockedException"

	tests := []struct {
		name      string
		errorType string
		message   string
		want      bool
	}{
		{
			name:      "loopback link local hostname check",
			errorType: exceptionType,
			message:   "The remote cluster target IP can not be evaluated: Host failed loopback link local check: 'uri.IdnHost cannot be resolved into an IP address: No such host is known'",
			want:      true,
		},
		{
			name:      "generic target IP evaluation failure",
			errorType: exceptionType,
			message:   "The remote cluster target IP cannot be evaluated",
		},
		{
			name:      "ordinary callout policy rejection",
			errorType: exceptionType,
			message:   "The remote cluster is not allowed by the callout policy",
		},
		{
			name:      "different exception type",
			errorType: "Kusto.DataNode.Exceptions.CalloutBlockedException",
			message:   "The target IP can not be evaluated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := remoteSchemaCalloutBlockedError(tt.errorType, tt.message)
			require.Equal(t, tt.want, isTransientFailedRequest(err))
		})
	}
}
