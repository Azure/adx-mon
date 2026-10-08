package engine

import (
	"errors"
	"fmt"
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
