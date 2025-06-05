package kusto

import (
	ERRS "errors"

	"github.com/Azure/azure-kusto-go/kusto/data/errors"
)

// ParseError extracts a clean error message from Kusto HttpError objects
// and truncates the message to a maximum length for consistent error handling.
// This utility is used across different CRD types that interact with Kusto.
func ParseError(err error) string {
	if err == nil {
		return ""
	}

	errMsg := err.Error()

	var kustoerr *errors.HttpError
	if ERRS.As(err, &kustoerr) {
		decoded := kustoerr.UnmarshalREST()
		if errMap, ok := decoded["error"].(map[string]interface{}); ok {
			if errMsgVal, ok := errMap["@message"].(string); ok {
				errMsg = errMsgVal
			}
		}
	}

	if len(errMsg) > 256 {
		errMsg = errMsg[:256]
	}
	return errMsg
}