package kustoutil

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	azkustoerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	"github.com/stretchr/testify/require"
)

func TestParseError(t *testing.T) {
	t.Run("nil error returns empty string", func(t *testing.T) {
		result := ParseError(nil)
		require.Empty(t, result)
	})

	t.Run("regular error returns error message", func(t *testing.T) {
		err := io.EOF
		result := ParseError(err)
		require.Equal(t, err.Error(), result)
	})

	t.Run("long regular error is truncated", func(t *testing.T) {
		longMsg := strings.Repeat("a", 300)
		err := &stringError{msg: longMsg}
		result := ParseError(err)
		require.Equal(t, longMsg[:256], result)
		require.Len(t, result, 256)
	})

	t.Run("kusto http error extracts @message", func(t *testing.T) {
		body := `{"error":{"@message": "this function is invalid"}}`
		kustoErr := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		result := ParseError(kustoErr)
		require.Equal(t, "this function is invalid", result)
	})

	t.Run("long kusto http error @message is truncated", func(t *testing.T) {
		longMsg := strings.Repeat("b", 300)
		body := `{"error":{"@message": "` + longMsg + `"}}`
		kustoErr := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		result := ParseError(kustoErr)
		require.Equal(t, longMsg[:256], result)
		require.Len(t, result, 256)
	})

	t.Run("error with no message returns empty string", func(t *testing.T) {
		err := errors.New("")
		result := ParseError(err)
		require.Empty(t, result)
	})

	t.Run("error with message containing only whitespace is truncated", func(t *testing.T) {
		longMsg := strings.Repeat(" ", 300)
		err := &stringError{msg: longMsg}
		result := ParseError(err)
		require.Equal(t, longMsg[:256], result)
		require.Len(t, result, 256)
	})

	t.Run("nested errors are unwrapped and parsed", func(t *testing.T) {
		innerErr := &stringError{msg: "inner error"}
		err := fmt.Errorf("outer error: %w", innerErr)
		result := ParseError(err)
		require.Equal(t, "outer error: inner error", result)
	})

	t.Run("kusto error with additional details is parsed correctly", func(t *testing.T) {
		body := `{"error":{"@message": "function is invalid", "code": "InvalidFunction"}}`
		kustoErr := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		result := ParseError(kustoErr)
		require.Equal(t, "function is invalid", result)
	})

	t.Run("kusto error with malformed json", func(t *testing.T) {
		body := `{"error": malformed json}`
		kustoErr := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		result := ParseError(kustoErr)
		// Should fall back to the standard error message
		require.Contains(t, result, "bad request")
	})

	t.Run("kusto error with missing error field", func(t *testing.T) {
		body := `{"other": "field"}`
		kustoErr := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		result := ParseError(kustoErr)
		// Should fall back to the standard error message
		require.Contains(t, result, "bad request")
	})

	t.Run("kusto error with missing @message field", func(t *testing.T) {
		body := `{"error": {"code": "BadRequest"}}`
		kustoErr := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		result := ParseError(kustoErr)
		// Should fall back to the standard error message
		require.Contains(t, result, "bad request")
	})

	t.Run("kusto error with wrong @message type", func(t *testing.T) {
		body := `{"error": {"@message": 123}}`
		kustoErr := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		result := ParseError(kustoErr)
		// Should fall back to the standard error message
		require.Contains(t, result, "bad request")
	})

	t.Run("wrapped kusto error", func(t *testing.T) {
		body := `{"error": {"@message": "Invalid KQL query"}}`
		kustoErr := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")
		wrappedErr := fmt.Errorf("query failed: %w", kustoErr)

		result := ParseError(wrappedErr)
		require.Equal(t, "Invalid KQL query", result)
	})

	t.Run("empty kusto error message", func(t *testing.T) {
		body := `{"error": {"@message": ""}}`
		kustoErr := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		result := ParseError(kustoErr)
		// Should fall back to the standard error message since @message is empty
		require.Contains(t, result, "bad request")
	})

	t.Run("kusto error with complex nested structure", func(t *testing.T) {
		body := `{
			"error": {
				"@message": "Query execution failed",
				"@type": "Kusto.DataNode.Exceptions.QueryExecutionException",
				"@context": {
					"timestamp": "2024-01-01T00:00:00Z"
				}
			}
		}`
		kustoErr := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		result := ParseError(kustoErr)
		require.Equal(t, "Query execution failed", result)
	})

	t.Run("exact max length message", func(t *testing.T) {
		exactMessage := strings.Repeat("c", MaxErrorMessageLength)
		err := &stringError{msg: exactMessage}

		result := ParseError(err)
		require.Len(t, result, MaxErrorMessageLength)
		require.Equal(t, exactMessage, result)
	})
}

func TestIsExplicitlyTransientError(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "explicitly transient", body: `{"error":{"@permanent":false}}`, want: true},
		{name: "explicitly permanent", body: `{"error":{"@permanent":true}}`},
		{name: "absent flag", body: `{"error":{"@message":"Syntax error: SYN0002"}}`},
		{name: "string flag", body: `{"error":{"@permanent":"false"}}`},
		{name: "null flag", body: `{"error":{"@permanent":null}}`},
		{name: "missing error", body: `{}`},
		{name: "malformed JSON", body: `{"error":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "Bad Request", http.StatusBadRequest, io.NopCloser(strings.NewReader(tt.body)), "")
			require.Equal(t, tt.want, IsExplicitlyTransientError(fmt.Errorf("create function: %w", err)))
		})
	}
	require.False(t, IsExplicitlyTransientError(nil))
	require.False(t, IsExplicitlyTransientError(errors.New("temporary failure")))
}

func TestIsThrottlingMessage(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{name: "direct throttle", message: "The control command was aborted due to throttling. Retrying after some backoff might succeed.", want: true},
		{name: "script throttle", message: "The command script was aborted. Command: '.create-or-alter function fn() { print 1 }'. Details: 'The control command was aborted due to throttling.'", want: true},
		{name: "empty"},
		{name: "syntax error", message: "Syntax error: SYN0002"},
		{name: "permission error", message: "Access denied"},
		{name: "missing database", message: "Database not found"},
		{name: "signature in command only", message: "The command script was aborted. Command: '.create function fn() { print note=\"The control command was aborted due to throttling.\" }'. Details: 'Syntax error: SYN0002'"},
		{name: "truncated script", message: "The command script was aborted. Command: '" + strings.Repeat("x", MaxErrorMessageLength)},
		{name: "missing script prefix", message: "Command: 'fn'. Details: 'The control command was aborted due to throttling.'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := ParseError(errors.New(tt.message))
			require.Equal(t, tt.want, IsThrottlingMessage(message))
		})
	}
}

func TestIsThrottlingError(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{name: "HTTP 429 without JSON", status: http.StatusTooManyRequests, body: "Too many requests", want: true},
		{name: "HTTP 429 overrides permanent flag", status: http.StatusTooManyRequests, body: `{"error":{"@permanent":true}}`, want: true},
		{name: "REST failure code", status: http.StatusBadRequest, body: `{"error":{"@failureCode":429,"@permanent":true}}`, want: true},
		{name: "control command throttled", status: http.StatusBadRequest, body: `{"error":{"@message":"The control command was aborted due to throttling. Retrying after some backoff might succeed. CommandType: 'DatabaseScriptExecute', Capacity: 20, Origin: 'RequestRateLimitPolicy/WorkloadGroup/default'.","@permanent":true}}`, want: true},
		{name: "script wraps throttling", status: http.StatusBadRequest, body: `{"error":{"@type":"Kusto.Common.Svc.Exceptions.AdminCommandExecuteScriptAbortedException","@message":"The command script was aborted due to a failure in command number 1 (1-based). Command: '.create-or-alter function fn() { print 1 }'. Details: 'The control command was aborted due to throttling. Retrying after some backoff might succeed.'","@permanent":true}}`, want: true},
		{name: "syntax error", status: http.StatusBadRequest, body: `{"error":{"@message":"Syntax error: SYN0002","@permanent":true}}`},
		{name: "permission denied", status: http.StatusForbidden, body: `{"error":{"@message":"Access denied","@permanent":true}}`},
		{name: "database missing", status: http.StatusNotFound, body: `{"error":{"@message":"Database not found","@permanent":true}}`},
		{name: "malformed JSON", status: http.StatusBadRequest, body: `{"error":`},
		{name: "non-string message", status: http.StatusBadRequest, body: `{"error":{"@message":429}}`},
		{name: "signature only in command", status: http.StatusBadRequest, body: `{"error":{"@type":"Kusto.Common.Svc.Exceptions.AdminCommandExecuteScriptAbortedException","@message":"The command script was aborted. Command: '.create function fn() { print note=\"The control command was aborted due to throttling.\" }'. Details: 'Syntax error: SYN0002'","@permanent":true}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := azkustoerrors.HTTP(azkustoerrors.OpMgmt, http.StatusText(tt.status), tt.status, io.NopCloser(strings.NewReader(tt.body)), "")
			require.Equal(t, tt.want, IsThrottlingError(fmt.Errorf("create function: %w", err)))
		})
	}
	require.False(t, IsThrottlingError(nil))
	require.False(t, IsThrottlingError(errors.New("The control command was aborted due to throttling.")))
}

func TestIsMissingTableError(t *testing.T) {
	wrappedMissingTableBody := `{
		"error": {
			"code": "General_BadRequest",
			"message": "Request is invalid and cannot be executed.",
			"@type": "Kusto.Common.Svc.Exceptions.AdminCommandExecuteScriptAbortedException",
			"@message": "The command script was aborted due to a failure in command number 1 (1-based). Command: '.create-or-alter function MissingEntityProbe() { MissingTable | take 1 }'. Details: 'Request is invalid and cannot be processed: Semantic error: SEM0100: 'take' operator: Failed to resolve table or column expression named 'MissingTable''",
			"@failureCode": 400,
			"@permanent": true
		}
	}`

	t.Run("azkusto wrapped missing table error", func(t *testing.T) {
		err := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "BadRequest", 400, io.NopCloser(strings.NewReader(wrappedMissingTableBody)), "")
		require.True(t, IsMissingTableError(fmt.Errorf("create function: %w", err)))
	})

	t.Run("other permanent kusto error", func(t *testing.T) {
		body := `{"error":{"code":"General_BadRequest","@permanent":true,"innererror":{"code":"SYN0002","@errorCode":"SYN0002"}}}`
		err := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "BadRequest", 400, io.NopCloser(strings.NewReader(body)), "")
		require.False(t, IsMissingTableError(err))
	})

	t.Run("message without structured code", func(t *testing.T) {
		body := `{"error":{"@message":"Semantic error SEM0100: Failed to resolve table MissingTable"}}`
		err := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "BadRequest", 400, io.NopCloser(strings.NewReader(body)), "")
		require.False(t, IsMissingTableError(err))
	})

	t.Run("wrapped unresolved column", func(t *testing.T) {
		body := `{"error":{"@type":"Kusto.Common.Svc.Exceptions.AdminCommandExecuteScriptAbortedException","@message":"The command script was aborted due to a failure in command number 1 (1-based). Command: '.create function f() { MissingTable | project MissingColumn }'. Details: 'Request is invalid and cannot be processed: Semantic error: SEM0100: 'project' operator: Failed to resolve scalar expression named 'MissingColumn''"}}`
		err := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "BadRequest", 400, io.NopCloser(strings.NewReader(body)), "")
		require.False(t, IsMissingTableError(err))
	})

	t.Run("wrapped unresolved function", func(t *testing.T) {
		body := `{"error":{"@type":"Kusto.Common.Svc.Exceptions.AdminCommandExecuteScriptAbortedException","@message":"The command script was aborted due to a failure in command number 1 (1-based). Command: '.create function f() { MissingFunction() }'. Details: 'Request is invalid and cannot be processed: Semantic error: SEM0100: 'take' operator: Failed to resolve tabular function named 'MissingFunction''"}}`
		err := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "BadRequest", 400, io.NopCloser(strings.NewReader(body)), "")
		require.False(t, IsMissingTableError(err))
	})

	t.Run("signature in wrapped command text", func(t *testing.T) {
		body := `{"error":{"@type":"Kusto.Common.Svc.Exceptions.AdminCommandExecuteScriptAbortedException","@message":"The command script was aborted due to a failure in command number 1 (1-based). Command: '.create function f() { print note=\"Semantic error: SEM0100: Failed to resolve table expression named\" }'. Details: 'Request is invalid due to another permanent error'"}}`
		err := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "BadRequest", 400, io.NopCloser(strings.NewReader(body)), "")
		require.False(t, IsMissingTableError(err))
	})

	t.Run("non-string error codes", func(t *testing.T) {
		body := `{"error":{"code":{"unexpected":"SEM0100"},"@errorCode":["SEM0100"],"innererror":{"code":100,"@errorCode":true}}}`
		err := azkustoerrors.HTTP(azkustoerrors.OpMgmt, "BadRequest", 400, io.NopCloser(strings.NewReader(body)), "")
		require.False(t, IsMissingTableError(err))
	})
}

// stringError is a simple error type for testing
type stringError struct {
	msg string
}

func (e *stringError) Error() string {
	return e.msg
}
