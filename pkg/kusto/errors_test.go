package kusto

import (
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
}

// stringError is a simple error type for testing
type stringError struct {
	msg string
}

func (e *stringError) Error() string {
	return e.msg
}