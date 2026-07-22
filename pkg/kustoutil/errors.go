package kustoutil

import (
	"errors"

	azkustoerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	legacykustoerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
)

const (
	// MaxErrorMessageLength defines the maximum length for error messages
	// to prevent excessively long messages in status conditions
	MaxErrorMessageLength  = 256
	missingEntityErrorCode = "SEM0100"
)

// ParseError extracts a clean error message from Kusto HttpError objects
// and truncates the message to a maximum length for consistent error handling.
// This utility is used across different CRD types that interact with Kusto.
func ParseError(err error) string {
	if err == nil {
		return ""
	}

	errMsg := err.Error()

	if parsed, ok := extractRESTMessage(decodeRESTError(err)); ok {
		errMsg = parsed
	}

	// Truncate if necessary
	if len(errMsg) > MaxErrorMessageLength {
		errMsg = errMsg[:MaxErrorMessageLength]
	}

	return errMsg
}

// IsMissingEntityError reports whether Kusto rejected a query because a referenced
// table or column could not be resolved. These dependencies may be created later.
func IsMissingEntityError(err error) bool {
	return hasErrorCode(decodeRESTError(err), missingEntityErrorCode)
}

func decodeRESTError(err error) map[string]interface{} {
	var azkustoErr *azkustoerrors.HttpError
	if errors.As(err, &azkustoErr) {
		return azkustoErr.UnmarshalREST()
	}

	var legacyKustoErr *legacykustoerrors.HttpError
	if errors.As(err, &legacyKustoErr) {
		return legacyKustoErr.UnmarshalREST()
	}

	return nil
}

func hasErrorCode(decoded map[string]interface{}, code string) bool {
	current, ok := decoded["error"].(map[string]interface{})
	if !ok {
		return false
	}

	for {
		if stringValueEquals(current, "code", code) || stringValueEquals(current, "@errorCode", code) {
			return true
		}
		current, ok = current["innererror"].(map[string]interface{})
		if !ok {
			return false
		}
	}
}

func stringValueEquals(values map[string]interface{}, key, expected string) bool {
	value, ok := values[key].(string)
	return ok && value == expected
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
