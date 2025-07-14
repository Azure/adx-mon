package kustoutil

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	kustoerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
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
		kustoErr := kustoerrors.HTTP(kustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		result := ParseError(kustoErr)
		require.Equal(t, "this function is invalid", result)
	})

	t.Run("long kusto http error @message is truncated", func(t *testing.T) {
		longMsg := strings.Repeat("b", 300)
		body := `{"error":{"@message": "` + longMsg + `"}}`
		kustoErr := kustoerrors.HTTP(kustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

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
		kustoErr := kustoerrors.HTTP(kustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		result := ParseError(kustoErr)
		require.Equal(t, "function is invalid", result)
	})

	t.Run("kusto error with malformed json", func(t *testing.T) {
		body := `{"error": malformed json}`
		kustoErr := kustoerrors.HTTP(kustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		result := ParseError(kustoErr)
		// Should fall back to the standard error message
		require.Contains(t, result, "bad request")
	})

	t.Run("kusto error with missing error field", func(t *testing.T) {
		body := `{"other": "field"}`
		kustoErr := kustoerrors.HTTP(kustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		result := ParseError(kustoErr)
		// Should fall back to the standard error message
		require.Contains(t, result, "bad request")
	})

	t.Run("kusto error with missing @message field", func(t *testing.T) {
		body := `{"error": {"code": "BadRequest"}}`
		kustoErr := kustoerrors.HTTP(kustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		result := ParseError(kustoErr)
		// Should fall back to the standard error message
		require.Contains(t, result, "bad request")
	})

	t.Run("kusto error with wrong @message type", func(t *testing.T) {
		body := `{"error": {"@message": 123}}`
		kustoErr := kustoerrors.HTTP(kustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		result := ParseError(kustoErr)
		// Should fall back to the standard error message
		require.Contains(t, result, "bad request")
	})

	t.Run("wrapped kusto error", func(t *testing.T) {
		body := `{"error": {"@message": "Invalid KQL query"}}`
		kustoErr := kustoerrors.HTTP(kustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")
		wrappedErr := fmt.Errorf("query failed: %w", kustoErr)

		result := ParseError(wrappedErr)
		require.Equal(t, "Invalid KQL query", result)
	})

	t.Run("empty kusto error message", func(t *testing.T) {
		body := `{"error": {"@message": ""}}`
		kustoErr := kustoerrors.HTTP(kustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

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
		kustoErr := kustoerrors.HTTP(kustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

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

// stringError is a simple error type for testing
type stringError struct {
	msg string
}

func (e *stringError) Error() string {
	return e.msg
}
