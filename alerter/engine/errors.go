package engine

import (
	"fmt"
	"strings"
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
