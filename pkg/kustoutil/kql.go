package kustoutil

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ApplySubstitutions applies time and cluster label substitutions to a KQL query body.
// For _endTime, it subtracts 1 tick (100ns) to support inclusive 'between' syntax,
// while maintaining non-overlapping time windows.
func ApplySubstitutions(body, startTime, endTime string, clusterLabels map[string]string) string {
	// Build the wrapped query with let statements, with direct value substitution
	var letStatements []string

	// Add time parameter definitions with direct datetime substitution
	letStatements = append(letStatements, fmt.Sprintf("let _startTime=datetime(%s);", startTime))
	
	// Subtract 1 tick (100ns) from endTime to support inclusive 'between' syntax
	// This allows users to write: where PreciseTimeStamp between (_startTime .. _endTime)
	// instead of: where PreciseTimeStamp >= _startTime and PreciseTimeStamp < _endTime
	adjustedEndTime := subtractOneTick(endTime)
	letStatements = append(letStatements, fmt.Sprintf("let _endTime=datetime(%s);", adjustedEndTime))

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

// subtractOneTick subtracts 1 tick (100ns) from a time string in RFC3339Nano format.
// This enables the use of inclusive 'between' syntax in KQL while maintaining
// non-overlapping time windows.
func subtractOneTick(timeStr string) string {
	// Parse the time string
	t, err := time.Parse(time.RFC3339Nano, timeStr)
	if err != nil {
		// If parsing fails, return the original string to avoid breaking queries
		// This maintains backward compatibility
		return timeStr
	}
	
	// Subtract 1 tick (100 nanoseconds)
	adjustedTime := t.Add(-100 * time.Nanosecond)
	
	// Return in the same format
	return adjustedTime.Format(time.RFC3339Nano)
}

// AddOneTick adds 1 tick (100ns) to a time.Time value.
// A tick is the smallest time unit in Kusto/Azure Data Explorer (100 nanoseconds).
// This function compensates for the tick subtracted in ApplySubstitutions to ensure
// proper time window continuity between summary rule executions while avoiding gaps.
func AddOneTick(t time.Time) time.Time {
	return t.Add(100 * time.Nanosecond)
}
