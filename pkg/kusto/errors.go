package kusto

import (
	"errors"

	kustoerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
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

	var kustoerr *kustoerrors.HttpError
	if errors.As(err, &kustoerr) {
		decoded := kustoerr.UnmarshalREST()
		if errMap, ok := decoded["error"].(map[string]interface{}); ok {
			if errMsgVal, ok := errMap["@message"].(string); ok {
				errMsg = errMsgVal
			}
		}
	}

	if len(errMsg) > MaxErrorMessageLength {
		errMsg = errMsg[:MaxErrorMessageLength] + "..."
	}
	return errMsg
}