package engine

import (
	"fmt"
	"strings"
)

const maxDisplayedDatabases = 10

// ResultLimitExceededError reports that a query produced more primary result
// rows than the client was configured to retain and process through callbacks.
type ResultLimitExceededError struct {
	Limit         int // configured number of result rows retained
	RowsProcessed int // total primary result rows observed
}

func (e *ResultLimitExceededError) Error() string {
	return fmt.Sprintf("result limit of %d exceeded after processing %d primary result rows", e.Limit, e.RowsProcessed)
}

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
