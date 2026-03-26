package kustoutil

import (
	"errors"

	azkustoerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	legacykustoerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
)

const (
	// MaxErrorMessageLength defines the maximum length for error messages
	// to prevent excessively long messages in status conditions
	MaxErrorMessageLength = 256
)

// ParseError extracts a clean error message from Kusto HttpError objects
// and truncates the message to a maximum length for consistent error handling.
// This utility is used across different CRD types that interact with Kusto.
func ParseError(err error) string {
	if err == nil {
		return ""
	}

	errMsg := err.Error()

	var azkustoErr *azkustoerrors.HttpError
	if errors.As(err, &azkustoErr) {
		if parsed, ok := extractRESTMessage(azkustoErr.UnmarshalREST()); ok {
			errMsg = parsed
		}
	}

	var legacyKustoErr *legacykustoerrors.HttpError
	if errors.As(err, &legacyKustoErr) {
		if parsed, ok := extractRESTMessage(legacyKustoErr.UnmarshalREST()); ok {
			errMsg = parsed
		}
	}

	// Truncate if necessary
	if len(errMsg) > MaxErrorMessageLength {
		errMsg = errMsg[:MaxErrorMessageLength]
	}

	return errMsg
}

func extractRESTMessage(decoded map[string]interface{}) (string, bool) {
	if decoded == nil {
		return "", false
	}

	errMap, ok := decoded["error"].(map[string]interface{})
	if !ok {
		return "", false
	}

	errMsg, ok := errMap["@message"].(string)
	if !ok || errMsg == "" {
		return "", false
	}

	return errMsg, true
}
