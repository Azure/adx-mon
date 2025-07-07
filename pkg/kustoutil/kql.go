package kustoutil

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ApplySubstitutions applies time and cluster label substitutions to a KQL query body
func ApplySubstitutions(body, startTime, endTime string, clusterLabels map[string]string) string {
	// Build the wrapped query with let statements, with direct value substitution
	var letStatements []string

	// Add time parameter definitions with direct datetime substitution
	letStatements = append(letStatements, fmt.Sprintf("let _startTime=datetime(%s);", startTime))
	letStatements = append(letStatements, fmt.Sprintf("let _endTime=datetime(%s);", endTime))

	// Add cluster label parameter definitions with direct value substitution
	// Sort keys to ensure deterministic output
	var keys []string
	for k := range clusterLabels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := clusterLabels[k]
		// Escape any double quotes in the value
		escapedValue := strconv.Quote(v)
		// Add underscore prefix for template substitution
		templateKey := k
		if !strings.HasPrefix(templateKey, "_") {
			templateKey = "_" + templateKey
		}
		letStatements = append(letStatements, fmt.Sprintf("let %s=%s;", templateKey, escapedValue))
	}

	// Construct the full query with let statements
	query := fmt.Sprintf("%s\n%s",
		strings.Join(letStatements, "\n"),
		strings.TrimSpace(body))

	return query
}
