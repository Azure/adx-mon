package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter"
)

func TestRenderBacktestTextExactDeterministicAndNonMutating(t *testing.T) {
	report := backtestRendererReport()
	originalWindows := append([]alerter.BacktestWindowResult(nil), report.Rule.Windows...)

	const expected = `Rule: platform/high-cpu
Outcome: partial
Rule error: two failures need review
Rule file: rules/high-cpu.yaml
Database: Metrics
Endpoints: Logs=https://logs.test, Metrics=https://metrics.test
Region: eastus
Cloud: public
Tags: environment=production, team=platform
Auth: managed-identity
Execution: concurrency=4, query-timeout=5m0s, max-results-per-window=25, max-windows=1000
Range: 2026-07-01T00:00:00Z to 2026-07-01T00:25:00.123456789Z
Summary: windows=5, clear=1, firing=1, limit-exceeded=1, error=1, cancelled=1, alerts=5
2026-07-01T00:05:00Z..2026-07-01T00:10:00Z FIRING retained=2 duration=812ms error=(none)
2026-07-01T00:10:00Z..2026-07-01T00:15:00Z LIMIT-EXCEEDED retained=3 duration=1.2s error=result limit exceeded at 25
2026-07-01T00:15:00Z..2026-07-01T00:20:00Z ERROR retained=0 duration=7ms error=semantic error details
2026-07-01T00:20:00Z..2026-07-01T00:25:00Z CANCELLED retained=0 duration=(unknown) error=context canceled
`

	for range 3 {
		var output bytes.Buffer
		if err := renderBacktestReport(&output, backtestFormatText, report); err != nil {
			t.Fatalf("render error = %v", err)
		}
		if output.String() != expected {
			t.Fatalf("text output mismatch\ngot:\n%s\nwant:\n%s", output.String(), expected)
		}
	}
	if !reflect.DeepEqual(report.Rule.Windows, originalWindows) {
		t.Fatal("renderer mutated input window order")
	}
	if strings.Contains(expected, "clear detail must not print") {
		t.Fatal("clear window detail was rendered")
	}
	for _, line := range strings.Split(strings.TrimSuffix(expected, "\n"), "\n") {
		if strings.HasSuffix(line, " ") {
			t.Fatalf("line has trailing whitespace: %q", line)
		}
	}
}

func TestRenderBacktestTextEmptyContext(t *testing.T) {
	report := &alerter.BacktestReport{Rule: alerter.BacktestRuleResult{Windows: []alerter.BacktestWindowResult{}}}
	var output bytes.Buffer
	if err := renderBacktestReport(&output, backtestFormatText, report); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Rule: (unknown)\n",
		"Outcome: (unknown)\n",
		"Rule file: (unknown)\n",
		"Database: (unknown)\n",
		"Endpoints: (none)\n",
		"Region: (unknown)\n",
		"Cloud: (unknown)\n",
		"Tags: (none)\n",
		"Auth: (unknown)\n",
		"Range: (unknown) to (unknown)\n",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestRenderBacktestJSONExactAndSafe(t *testing.T) {
	report := &alerter.BacktestReport{
		Version:     1,
		GeneratedAt: time.Date(2026, 7, 1, 3, 4, 5, 6, time.FixedZone("offset", 2*60*60)),
		Range: alerter.BacktestRange{
			Start: time.Date(2026, 7, 1, 1, 0, 0, 0, time.FixedZone("offset", 60*60)),
			End:   time.Date(2026, 7, 1, 2, 0, 0, 0, time.UTC),
		},
		Context: alerter.BacktestContext{
			RuleFile:       "rule.yaml",
			KustoEndpoints: map[string]string{},
			Tags:           map[string]string{},
			Authentication: alerter.BacktestAuthenticationToken,
			QueryTimeout:   "5m0s",
		},
		Rule: alerter.BacktestRuleResult{
			Outcome: alerter.BacktestRuleOutcomeCompleted,
			Windows: []alerter.BacktestWindowResult{},
		},
	}
	const expected = `{
  "version": 1,
  "generatedAt": "2026-07-01T01:04:05.000000006Z",
  "range": {
    "start": "2026-07-01T00:00:00Z",
    "end": "2026-07-01T02:00:00Z"
  },
  "context": {
    "ruleFile": "rule.yaml",
    "database": "",
    "kustoEndpoints": {},
    "region": "",
    "cloud": "",
    "tags": {},
    "authentication": "token",
    "concurrency": 0,
    "queryTimeout": "5m0s",
    "maxResultsPerWindow": 0,
    "maxWindows": 0
  },
  "summary": {
    "totalWindows": 0,
    "clearWindows": 0,
    "firingWindows": 0,
    "errorWindows": 0,
    "limitExceededWindows": 0,
    "cancelledWindows": 0,
    "alerts": 0
  },
  "rule": {
    "namespace": "",
    "name": "",
    "outcome": "completed",
    "windows": []
  }
}
`

	var output bytes.Buffer
	if err := renderBacktestReport(&output, backtestFormatJSON, report); err != nil {
		t.Fatal(err)
	}
	if output.String() != expected {
		t.Fatalf("JSON output mismatch\ngot:\n%s\nwant:\n%s", output.String(), expected)
	}
	if bytes.Count(output.Bytes(), []byte("\n")) == 0 || !bytes.HasSuffix(output.Bytes(), []byte("\n")) || bytes.HasSuffix(output.Bytes(), []byte("\n\n")) {
		t.Fatalf("JSON output does not have exactly one trailing newline: %q", output.String())
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(document) != 6 {
		t.Fatalf("top-level fields = %d, want 6", len(document))
	}
	for _, forbidden := range []string{"rendered-query-secret", "selectedEndpoint", "resolvedEndpoint", "credentials", "msiId", "authDiagnostics", "token-secret", "msi-secret"} {
		if strings.Contains(strings.ToLower(output.String()), strings.ToLower(forbidden)) {
			t.Fatalf("JSON contains forbidden value %q: %s", forbidden, output.String())
		}
	}
}

func TestRenderBacktestReportSanitizesEndpointCredentials(t *testing.T) {
	report := completedBacktestReport()
	report.Context.KustoEndpoints = map[string]string{
		"UserInfo": "https://credential-user:userinfo-secret@cluster.example.test/path-secret",
		"Query":    "https://cluster.example.test/path-token?token=query-secret&sig=sig-secret#fragment-secret",
		"Port":     "https://cluster.example.test:8443/port-path-secret",
		"Opaque":   "cluster-alias opaque-secret",
	}

	for _, format := range []string{backtestFormatText, backtestFormatJSON} {
		t.Run(format, func(t *testing.T) {
			var output bytes.Buffer
			if err := renderBacktestReport(&output, format, report); err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"credential-user", "userinfo-secret", "path-secret", "path-token", "port-path-secret", "query-secret", "sig-secret", "fragment-secret", "opaque-secret"} {
				if strings.Contains(output.String(), secret) {
					t.Fatalf("output contains %q: %s", secret, output.String())
				}
			}
			if !strings.Contains(output.String(), "https://cluster.example.test") || !strings.Contains(output.String(), "https://cluster.example.test:8443") || !strings.Contains(output.String(), "(non-URL endpoint)") {
				t.Fatalf("output does not contain safe endpoint representations: %s", output.String())
			}
		})
	}

	if report.Context.KustoEndpoints["UserInfo"] != "https://credential-user:userinfo-secret@cluster.example.test/path-secret" {
		t.Fatal("renderer mutated endpoint context")
	}
}

func TestRenderBacktestReportValidationAndWriterErrors(t *testing.T) {
	report := completedBacktestReport()
	writeErr := errors.New("write failed")
	tests := []struct {
		name   string
		writer io.Writer
		format string
		report *alerter.BacktestReport
		want   error
	}{
		{name: "nil writer", writer: nil, format: backtestFormatText, report: report},
		{name: "nil report", writer: io.Discard, format: backtestFormatText},
		{name: "invalid format", writer: io.Discard, format: "yaml", report: report},
		{name: "writer error", writer: errorWriter{err: writeErr}, format: backtestFormatJSON, report: report, want: writeErr},
		{name: "short writer", writer: shortWriter{}, format: backtestFormatText, report: report, want: io.ErrShortWrite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := renderBacktestReport(tt.writer, tt.format, tt.report)
			if err == nil {
				t.Fatal("error = nil")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func backtestRendererReport() *alerter.BacktestReport {
	window := func(index, minute int, status alerter.BacktestWindowStatus) alerter.BacktestWindowResult {
		return alerter.BacktestWindowResult{
			BacktestWindow: alerter.BacktestWindow{
				Index: index,
				Start: time.Date(2026, 7, 1, 0, minute, 0, 0, time.UTC),
				End:   time.Date(2026, 7, 1, 0, minute+5, 0, 0, time.UTC),
			},
			Status: status,
			Alerts: []alerter.BacktestAlert{},
		}
	}
	clear := window(0, 0, alerter.BacktestWindowStatusClear)
	clear.Error = "clear detail must not print"
	firing := window(1, 5, alerter.BacktestWindowStatusFiring)
	firing.QueryDuration = "812ms"
	firing.ResultsRetained = 2
	limit := window(2, 10, alerter.BacktestWindowStatusLimitExceeded)
	limit.QueryDuration = "1.2s"
	limit.ResultsRetained = 3
	limit.Error = "result limit exceeded\n at 25"
	failed := window(3, 15, alerter.BacktestWindowStatusError)
	failed.QueryDuration = "7ms"
	failed.Error = "semantic error\n\tdetails"
	cancelled := window(4, 20, alerter.BacktestWindowStatusCancelled)
	cancelled.Error = "context canceled"

	return &alerter.BacktestReport{
		Version:     alerter.BacktestReportVersion,
		GeneratedAt: time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC),
		Range: alerter.BacktestRange{
			Start: time.Date(2026, 7, 1, 1, 0, 0, 0, time.FixedZone("plus-one", 60*60)),
			End:   time.Date(2026, 7, 1, 0, 25, 0, 123456789, time.UTC),
		},
		Context: alerter.BacktestContext{
			RuleFile:            "rules/high-cpu.yaml",
			Database:            "Metrics",
			KustoEndpoints:      map[string]string{"Metrics": "https://metrics.test", "Logs": "https://logs.test"},
			Region:              "eastus",
			Cloud:               "public",
			Tags:                map[string]string{"team": "platform", "environment": "production"},
			Authentication:      alerter.BacktestAuthenticationManagedIdentity,
			Concurrency:         4,
			QueryTimeout:        "5m0s",
			MaxResultsPerWindow: 25,
			MaxWindows:          1000,
		},
		Summary: alerter.BacktestSummary{
			TotalWindows:         5,
			ClearWindows:         1,
			FiringWindows:        1,
			ErrorWindows:         1,
			LimitExceededWindows: 1,
			CancelledWindows:     1,
			Alerts:               5,
		},
		Rule: alerter.BacktestRuleResult{
			Namespace: "platform",
			Name:      "high-cpu",
			Outcome:   alerter.BacktestRuleOutcomePartial,
			Error:     "two failures\n need review",
			Windows:   []alerter.BacktestWindowResult{cancelled, failed, clear, limit, firing},
		},
	}
}

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }
