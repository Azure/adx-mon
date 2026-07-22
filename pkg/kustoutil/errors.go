package kustoutil

import (
	"errors"
	"strings"

	azkustoerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	legacykustoerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
)

const (
	// MaxErrorMessageLength defines the maximum length for error messages
	// to prevent excessively long messages in status conditions
	MaxErrorMessageLength  = 256
	missingEntityErrorCode = "SEM0100"
	scriptAbortedErrorType = "Kusto.Common.Svc.Exceptions.AdminCommandExecuteScriptAbortedException"
	scriptDetailsSeparator = "'. Details: '"
)

var missingTableMessageSignatures = [...]string{
	"Failed to resolve table expression named",
	"Failed to resolve table or column expression named",
}

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

// IsMissingTableError reports whether Kusto rejected a query because a referenced
// table could not be resolved. The table may be created later.
func IsMissingTableError(err error) bool {
	return hasWrappedMissingTableError(decodeRESTError(err))
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

func stringValueEquals(values map[string]interface{}, key, expected string) bool {
	value, ok := values[key].(string)
	return ok && value == expected
}

func hasWrappedMissingTableError(decoded map[string]interface{}) bool {
	errMap, ok := decoded["error"].(map[string]interface{})
	if !ok || !stringValueEquals(errMap, "@type", scriptAbortedErrorType) {
		return false
	}

	message, ok := errMap["@message"].(string)
	if !ok {
		return false
	}

	separator := strings.LastIndex(message, scriptDetailsSeparator)
	if separator == -1 {
		return false
	}
	details := message[separator+len(scriptDetailsSeparator):]
	return strings.Contains(details, "Semantic error: "+missingEntityErrorCode+":") && isMissingTableMessage(details)
}

func isMissingTableMessage(message string) bool {
	for _, signature := range missingTableMessageSignatures {
		if strings.Contains(message, signature) {
			return true
		}
	}
	return false
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
