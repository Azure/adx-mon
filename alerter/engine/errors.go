package engine

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	kerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
)

const maxDisplayedDatabases = 10

type UnknownDBError struct {
	DB                   string
	AvailableDatabases   []string
	CaseInsensitiveMatch string
}

func (e *UnknownDBError) Error() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "no client for database %q", e.DB)

	// Suggest case-insensitive match if found
	if e.CaseInsensitiveMatch != "" {
		fmt.Fprintf(&sb, "; did you mean %q? (database names are case-sensitive)", e.CaseInsensitiveMatch)
	}

	// List available databases
	if len(e.AvailableDatabases) > 0 {
		sb.WriteString("; configured databases via --kusto-endpoint: [")
		if len(e.AvailableDatabases) <= maxDisplayedDatabases {
			sb.WriteString(strings.Join(e.AvailableDatabases, ", "))
		} else {
			sb.WriteString(strings.Join(e.AvailableDatabases[:maxDisplayedDatabases], ", "))
			fmt.Fprintf(&sb, ", ... and %d more", len(e.AvailableDatabases)-maxDisplayedDatabases)
		}
		sb.WriteString("]")
	} else {
		sb.WriteString("; no databases configured via --kusto-endpoint")
	}

	return sb.String()
}

func isTransientFailedRequest(err error) bool {
	if err == nil {
		return false
	}

	var kerr *kerrors.HttpError
	if !errors.As(err, &kerr) {
		return false
	}

	if kerr.StatusCode != http.StatusBadRequest {
		return false
	}
	if isTransientRemoteSchemaCalloutBlockedError(kerr) {
		return true
	}
	if isTransientRemoteEntityResolutionError(kerr) {
		return true
	}

	return false
}

func isTransientRemoteEntityResolutionError(kerr *kerrors.HttpError) bool {
	lowerErr := strings.ToLower(kerr.Error())
	if strings.Contains(lowerErr, "sem0056") &&
		strings.Contains(lowerErr, "resolving remote entities") &&
		strings.Contains(lowerErr, "failed to resolve name or pattern") {
		return !strings.Contains(lowerErr, "obo token is required for cross-cluster communication") &&
			!strings.Contains(lowerErr, "is not authorized to") &&
			!strings.Contains(lowerErr, "access denied") &&
			!strings.Contains(lowerErr, "is not allowed by the callout policy")
	}

	return false
}

func isTransientRemoteSchemaCalloutBlockedError(kerr *kerrors.HttpError) bool {
	const transientMessage = "host failed loopback link local check: 'uri.idnhost cannot be resolved into an ip address: no such host is known'"

	restError := kerr.UnmarshalREST()
	errorDetails, ok := restError["error"].(map[string]interface{})
	if !ok {
		return false
	}

	errorType, _ := errorDetails["@type"].(string)
	if errorType != "Kusto.DataNode.Exceptions.RemoteSchemaCalloutBlockedException" {
		return false
	}

	message, _ := errorDetails["@message"].(string)
	return strings.Contains(strings.ToLower(message), transientMessage)
}

type retriableError struct {
	initialErr error
	retryErr   error
}

func (e *retriableError) Error() string {
	return fmt.Sprintf("query retry failed: initial error: %v; retry error: %v", e.initialErr, e.retryErr)
}

func (e *retriableError) Unwrap() error {
	return e.retryErr
}

func isRetriableError(err error) bool {
	var retryErr *retriableError
	return errors.As(err, &retryErr)
}

func isUserError(err error) bool {
	if err == nil {
		return false
	}

	// User specified a database in their CRD that adx-mon does not have configured.
	var unknownDB *UnknownDBError
	if errors.As(err, &unknownDB) {
		return true
	}

	// User's query results are missing a required column, or they are the wrong type.
	var validationErr *NotificationValidationError
	if errors.As(err, &validationErr) {
		return true
	}

	// Look to see if a kusto query error is specific to how the query was defined and not due to problems with adx-mon itself.
	var kerr *kerrors.HttpError
	if errors.As(err, &kerr) {
		if kerr.Kind == kerrors.KClientArgs {
			return true
		}
		lowerErr := strings.ToLower(kerr.Error())
		if strings.Contains(lowerErr, "sem0001") || strings.Contains(lowerErr, "semantic error") || strings.Contains(lowerErr, "request is invalid") {
			return true
		}
	}

	return false
}
