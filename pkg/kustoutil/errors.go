package kustoutil

import (
	"errors"
	"net/http"
	"strings"

	azkustoerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
)

const (
	// MaxErrorMessageLength defines the maximum length for error messages
	// to prevent excessively long messages in status conditions
	MaxErrorMessageLength   = 256
	missingEntityErrorCode  = "SEM0100"
	scriptAbortedErrorType  = "Kusto.Common.Svc.Exceptions.AdminCommandExecuteScriptAbortedException"
	scriptDetailsSeparator  = "'. Details: '"
	throttlingMessagePrefix = "The control command was aborted due to throttling."
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

// IsExplicitlyTransientError reports whether the REST error explicitly marks
// itself as non-permanent. Missing or malformed flags do not imply retryability.
func IsExplicitlyTransientError(err error) bool {
	errMap, ok := decodeRESTError(err)["error"].(map[string]any)
	if !ok {
		return false
	}
	permanent, ok := errMap["@permanent"].(bool)
	return ok && !permanent
}

// IsThrottlingError recognizes HTTP throttling and capacity failures wrapped by
// database scripts, even when Kusto labels the script failure as permanent.
func IsThrottlingError(err error) bool {
	if httpErr, ok := errors.AsType[*azkustoerrors.HttpError](err); ok && httpErr.IsThrottled() {
		return true
	}
	errMap, ok := decodeRESTError(err)["error"].(map[string]interface{})
	if !ok {
		return false
	}
	if code, ok := errMap["@failureCode"].(float64); ok && code == http.StatusTooManyRequests {
		return true
	}
	message, _ := errMap["@message"].(string)
	if strings.HasPrefix(message, throttlingMessagePrefix) {
		return true
	}
	return strings.HasPrefix(scriptErrorDetails(errMap), throttlingMessagePrefix)
}

// IsThrottlingMessage recognizes saved REST messages without their HTTP metadata.
// Script failures must retain the throttle in their details, not just the command.
func IsThrottlingMessage(message string) bool {
	if strings.HasPrefix(message, throttlingMessagePrefix) {
		return true
	}
	return strings.HasPrefix(message, "The command script was aborted") &&
		strings.HasPrefix(scriptMessageDetails(message), throttlingMessagePrefix)
}

func decodeRESTError(err error) map[string]interface{} {
	if azkustoErr, ok := errors.AsType[*azkustoerrors.HttpError](err); ok {
		return azkustoErr.UnmarshalREST()
	}

	return nil
}

func stringValueEquals(values map[string]interface{}, key, expected string) bool {
	value, ok := values[key].(string)
	return ok && value == expected
}

func hasWrappedMissingTableError(decoded map[string]interface{}) bool {
	errMap, ok := decoded["error"].(map[string]interface{})
	if !ok {
		return false
	}
	details := scriptErrorDetails(errMap)
	return strings.Contains(details, "Semantic error: "+missingEntityErrorCode+":") && isMissingTableMessage(details)
}

func scriptErrorDetails(errMap map[string]interface{}) string {
	if !stringValueEquals(errMap, "@type", scriptAbortedErrorType) {
		return ""
	}
	message, ok := errMap["@message"].(string)
	if !ok {
		return ""
	}

	return scriptMessageDetails(message)
}

func scriptMessageDetails(message string) string {
	separator := strings.LastIndex(message, scriptDetailsSeparator)
	if separator == -1 {
		return ""
	}
	return message[separator+len(scriptDetailsSeparator):]
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
