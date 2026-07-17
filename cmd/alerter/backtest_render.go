package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Azure/adx-mon/alerter"
)

func renderBacktestReport(w io.Writer, format string, report *alerter.BacktestReport) error {
	if w == nil {
		return errors.New("backtest output writer is nil")
	}
	if report == nil {
		return errors.New("backtest report is nil")
	}

	prepared := prepareBacktestReport(report)
	var rendered []byte
	var err error
	switch format {
	case backtestFormatJSON:
		rendered, err = json.MarshalIndent(prepared, "", "  ")
	case backtestFormatText:
		rendered = renderBacktestText(prepared)
	default:
		return fmt.Errorf("unsupported backtest format %q", format)
	}
	if err != nil {
		return err
	}
	rendered = append(bytes.TrimRight(rendered, "\n"), '\n')
	return writeBacktestBytes(w, rendered)
}

func prepareBacktestReport(report *alerter.BacktestReport) *alerter.BacktestReport {
	prepared := *report
	prepared.GeneratedAt = report.GeneratedAt.UTC()
	prepared.Range.Start = report.Range.Start.UTC()
	prepared.Range.End = report.Range.End.UTC()
	prepared.Context.KustoEndpoints = cloneBacktestStringMap(report.Context.KustoEndpoints)
	for name, endpoint := range prepared.Context.KustoEndpoints {
		prepared.Context.KustoEndpoints[name] = alerter.SanitizeBacktestEndpoint(endpoint)
	}
	prepared.Context.Tags = cloneBacktestStringMap(report.Context.Tags)

	if report.Rule.Windows != nil {
		prepared.Rule.Windows = make([]alerter.BacktestWindowResult, len(report.Rule.Windows))
		copy(prepared.Rule.Windows, report.Rule.Windows)
		for i := range prepared.Rule.Windows {
			window := &prepared.Rule.Windows[i]
			window.Start = window.Start.UTC()
			window.End = window.End.UTC()
			if report.Rule.Windows[i].Alerts != nil {
				window.Alerts = make([]alerter.BacktestAlert, len(report.Rule.Windows[i].Alerts))
				copy(window.Alerts, report.Rule.Windows[i].Alerts)
				for j := range window.Alerts {
					window.Alerts[j].CustomFields = cloneBacktestStringMap(report.Rule.Windows[i].Alerts[j].CustomFields)
				}
			}
		}
		sort.SliceStable(prepared.Rule.Windows, func(i, j int) bool {
			left, right := prepared.Rule.Windows[i], prepared.Rule.Windows[j]
			if !left.Start.Equal(right.Start) {
				return left.Start.Before(right.Start)
			}
			if !left.End.Equal(right.End) {
				return left.End.Before(right.End)
			}
			return left.Index < right.Index
		})
	}
	return &prepared
}

func cloneBacktestStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func renderBacktestText(report *alerter.BacktestReport) []byte {
	var output strings.Builder
	fmt.Fprintf(&output, "Rule: %s\n", backtestRuleIdentity(report.Rule.Namespace, report.Rule.Name))
	fmt.Fprintf(&output, "Outcome: %s\n", backtestValue(string(report.Rule.Outcome)))
	if strings.TrimSpace(report.Rule.Error) != "" {
		fmt.Fprintf(&output, "Rule error: %s\n", backtestValue(report.Rule.Error))
	}
	fmt.Fprintf(&output, "Rule file: %s\n", backtestValue(report.Context.RuleFile))
	fmt.Fprintf(&output, "Database: %s\n", backtestValue(report.Context.Database))
	fmt.Fprintf(&output, "Endpoints: %s\n", renderBacktestMap(report.Context.KustoEndpoints))
	fmt.Fprintf(&output, "Region: %s\n", backtestValue(report.Context.Region))
	fmt.Fprintf(&output, "Cloud: %s\n", backtestValue(report.Context.Cloud))
	fmt.Fprintf(&output, "Tags: %s\n", renderBacktestMap(report.Context.Tags))
	fmt.Fprintf(&output, "Auth: %s\n", backtestValue(string(report.Context.Authentication)))
	fmt.Fprintf(&output, "Execution: concurrency=%d, query-timeout=%s, max-results-per-window=%d, max-windows=%d\n",
		report.Context.Concurrency,
		backtestValue(report.Context.QueryTimeout),
		report.Context.MaxResultsPerWindow,
		report.Context.MaxWindows,
	)
	fmt.Fprintf(&output, "Range: %s to %s\n", backtestTime(report.Range.Start), backtestTime(report.Range.End))
	fmt.Fprintf(&output, "Summary: windows=%d, clear=%d, firing=%d, limit-exceeded=%d, error=%d, cancelled=%d, alerts=%d\n",
		report.Summary.TotalWindows,
		report.Summary.ClearWindows,
		report.Summary.FiringWindows,
		report.Summary.LimitExceededWindows,
		report.Summary.ErrorWindows,
		report.Summary.CancelledWindows,
		report.Summary.Alerts,
	)

	for _, window := range report.Rule.Windows {
		if window.Status == alerter.BacktestWindowStatusClear {
			continue
		}
		switch window.Status {
		case alerter.BacktestWindowStatusFiring,
			alerter.BacktestWindowStatusLimitExceeded,
			alerter.BacktestWindowStatusError,
			alerter.BacktestWindowStatusCancelled:
			fmt.Fprintf(&output, "%s..%s %s retained=%d duration=%s error=%s\n",
				backtestTime(window.Start),
				backtestTime(window.End),
				strings.ToUpper(string(window.Status)),
				window.ResultsRetained,
				backtestValue(window.QueryDuration),
				backtestOptionalValue(window.Error),
			)
		}
	}
	return []byte(output.String())
}

func backtestRuleIdentity(namespace, name string) string {
	namespace = backtestValue(namespace)
	name = backtestValue(name)
	if namespace == "(unknown)" && name == "(unknown)" {
		return "(unknown)"
	}
	return namespace + "/" + name
}

func renderBacktestMap(values map[string]string) string {
	if len(values) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, backtestValue(key)+"="+backtestValue(values[key]))
	}
	return strings.Join(parts, ", ")
}

func backtestValue(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return "(unknown)"
	}
	return value
}

func backtestOptionalValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(none)"
	}
	return backtestValue(value)
}

func backtestTime(value time.Time) string {
	if value.IsZero() {
		return "(unknown)"
	}
	return value.UTC().Format(time.RFC3339Nano)
}
