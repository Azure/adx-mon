package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/urfave/cli/v2"
)

func TestNewAlerterAppRegistersBacktest(t *testing.T) {
	app := newAlerterApp()
	if app.Command("lint") == nil {
		t.Fatal("lint command is not registered")
	}
	if app.Command("backtest") == nil {
		t.Fatal("backtest command is not registered")
	}
}

func TestBacktestCommandRequiredFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "rule", args: []string{"--start", "2026-07-01T00:00:00Z", "--end", "2026-07-01T01:00:00Z", "--kusto-endpoint", "Events=https://example.test"}, want: "rule"},
		{name: "start", args: []string{"--rule", "rule.yaml", "--end", "2026-07-01T01:00:00Z", "--kusto-endpoint", "Events=https://example.test"}, want: "start"},
		{name: "end", args: []string{"--rule", "rule.yaml", "--start", "2026-07-01T00:00:00Z", "--kusto-endpoint", "Events=https://example.test"}, want: "end"},
		{name: "endpoint", args: []string{"--rule", "rule.yaml", "--start", "2026-07-01T00:00:00Z", "--end", "2026-07-01T01:00:00Z"}, want: "kusto-endpoint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			deps := testBacktestDeps(func(context.Context, *alerter.AlerterOpts, string, alerter.BacktestOptions) (*alerter.BacktestReport, error) {
				called = true
				return completedBacktestReport(), nil
			})
			err := runBacktestCommand(t, deps, tt.args...)
			if err == nil || !bytes.Contains([]byte(err.Error()), []byte(tt.want)) {
				t.Fatalf("error = %v, want required flag %q", err, tt.want)
			}
			if called {
				t.Fatal("runner called for missing required flag")
			}
		})
	}
}

func TestBacktestCommandTranslatesOptions(t *testing.T) {
	var gotOpts *alerter.AlerterOpts
	var gotRule string
	var gotBacktestOpts alerter.BacktestOptions
	deps := testBacktestDeps(func(_ context.Context, opts *alerter.AlerterOpts, rule string, backtestOpts alerter.BacktestOptions) (*alerter.BacktestReport, error) {
		gotOpts = opts
		gotRule = rule
		gotBacktestOpts = backtestOpts
		return completedBacktestReport(), nil
	})

	err := runBacktestCommand(t, deps,
		"--rule", "rule.yaml",
		"--start", "2026-07-01T01:30:00+01:30",
		"--end", "2026-07-01T04:00:00+02:00",
		"--kusto-endpoint", "Events=https://events.test",
		"--kusto-endpoint", "Metrics=https://metrics.test",
		"--auth-msi-id", "msi-id",
		"--auth-token", "token",
		"--region", "eastus",
		"--cloud", "public",
		"--tag", "team=platform",
		"--tag", "region=overridden",
		"--tag", "cloud=overridden",
		"--concurrency", "7",
		"--max-results-per-window", "11",
		"--query-timeout", "17s",
		"--max-windows", "23",
	)
	if err != nil {
		t.Fatalf("runBacktestCommand() error = %v", err)
	}

	wantOpts := &alerter.AlerterOpts{
		KustoEndpoints: map[string]string{
			"Events":  "https://events.test",
			"Metrics": "https://metrics.test",
		},
		Region:     "eastus",
		Cloud:      "public",
		Tags:       map[string]string{"team": "platform", "region": "eastus", "cloud": "public"},
		MSIID:      "msi-id",
		KustoToken: "token",
	}
	if !reflect.DeepEqual(gotOpts, wantOpts) {
		t.Fatalf("AlerterOpts = %#v, want %#v", gotOpts, wantOpts)
	}
	if gotRule != "rule.yaml" {
		t.Fatalf("rule = %q, want rule.yaml", gotRule)
	}
	wantBacktestOpts := alerter.BacktestOptions{
		Start:               time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		End:                 time.Date(2026, 7, 1, 2, 0, 0, 0, time.UTC),
		Concurrency:         7,
		MaxResultsPerWindow: 11,
		QueryTimeout:        17 * time.Second,
		MaxWindows:          23,
	}
	if !reflect.DeepEqual(gotBacktestOpts, wantBacktestOpts) {
		t.Fatalf("BacktestOptions = %#v, want %#v", gotBacktestOpts, wantBacktestOpts)
	}
	if gotBacktestOpts.Start.Location() != time.UTC || gotBacktestOpts.End.Location() != time.UTC {
		t.Fatalf("times were not normalized to UTC: start=%v end=%v", gotBacktestOpts.Start.Location(), gotBacktestOpts.End.Location())
	}
}

func TestBacktestCommandRejectsInvalidFlagsBeforeRunner(t *testing.T) {
	valid := []string{
		"--rule", "rule.yaml",
		"--start", "2026-07-01T00:00:00Z",
		"--end", "2026-07-01T01:00:00Z",
		"--kusto-endpoint", "Events=https://events.test",
	}
	tests := []struct {
		name    string
		replace []string
		args    []string
	}{
		{name: "empty rule", replace: []string{"--rule", ""}},
		{name: "empty endpoint", replace: []string{"--kusto-endpoint", ""}},
		{name: "empty endpoint name", replace: []string{"--kusto-endpoint", "=https://events.test"}},
		{name: "empty endpoint value", replace: []string{"--kusto-endpoint", "Events="}},
		{name: "malformed endpoint", replace: []string{"--kusto-endpoint", "Events"}},
		{name: "malformed tag", args: []string{"--tag", "team"}},
		{name: "empty tag key", args: []string{"--tag", "=platform"}},
		{name: "whitespace tag key", args: []string{"--tag", "   =platform"}},
		{name: "invalid format", args: []string{"--format", "yaml"}},
		{name: "uppercase format", args: []string{"--format", "JSON"}},
		{name: "zero concurrency", args: []string{"--concurrency", "0"}},
		{name: "negative concurrency", args: []string{"--concurrency", "-1"}},
		{name: "invalid concurrency", args: []string{"--concurrency", "many"}},
		{name: "zero result limit", args: []string{"--max-results-per-window", "0"}},
		{name: "negative result limit", args: []string{"--max-results-per-window", "-1"}},
		{name: "invalid result limit", args: []string{"--max-results-per-window", "many"}},
		{name: "zero query timeout", args: []string{"--query-timeout", "0s"}},
		{name: "negative query timeout", args: []string{"--query-timeout", "-1s"}},
		{name: "invalid query timeout", args: []string{"--query-timeout", "later"}},
		{name: "zero max windows", args: []string{"--max-windows", "0"}},
		{name: "negative max windows", args: []string{"--max-windows", "-1"}},
		{name: "invalid max windows", args: []string{"--max-windows", "many"}},
		{name: "equal range", replace: []string{"--end", "2026-07-01T00:00:00Z"}},
		{name: "reversed range", replace: []string{"--end", "2026-06-30T23:00:00Z"}},
		{name: "invalid start", replace: []string{"--start", "yesterday"}},
		{name: "empty start", replace: []string{"--start", ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			deps := testBacktestDeps(func(context.Context, *alerter.AlerterOpts, string, alerter.BacktestOptions) (*alerter.BacktestReport, error) {
				called = true
				return completedBacktestReport(), nil
			})
			args := append([]string(nil), valid...)
			if len(tt.replace) != 0 {
				for i := 0; i < len(args)-1; i++ {
					if args[i] == tt.replace[0] {
						args[i+1] = tt.replace[1]
						break
					}
				}
			}
			args = append(args, tt.args...)
			if err := runBacktestCommand(t, deps, args...); err == nil {
				t.Fatal("runBacktestCommand() error = nil")
			}
			if called {
				t.Fatal("runner called for invalid flags")
			}
		})
	}
}

func TestBacktestCommandAllowsEmptyTagValue(t *testing.T) {
	called := false
	deps := testBacktestDeps(func(_ context.Context, opts *alerter.AlerterOpts, _ string, _ alerter.BacktestOptions) (*alerter.BacktestReport, error) {
		called = true
		if value, ok := opts.Tags["team"]; !ok || value != "" {
			t.Fatalf("team tag = %q, %v; want present empty value", value, ok)
		}
		return completedBacktestReport(), nil
	})

	err := runBacktestCommand(t, deps, append(validBacktestArgs(), "--tag", "team=")...)
	if err != nil {
		t.Fatalf("runBacktestCommand() error = %v", err)
	}
	if !called {
		t.Fatal("runner was not called")
	}
}

func TestBacktestCommandOutcomeAndRunnerErrors(t *testing.T) {
	runnerErr := errors.New("sensitive runner detail")
	tests := []struct {
		name       string
		outcome    alerter.BacktestRuleOutcome
		runErr     error
		wantErr    bool
		wantCause  error
		wantRender bool
	}{
		{name: "completed", outcome: alerter.BacktestRuleOutcomeCompleted, wantRender: true},
		{name: "skipped", outcome: alerter.BacktestRuleOutcomeSkipped, wantRender: true},
		{name: "invalid", outcome: alerter.BacktestRuleOutcomeInvalid, wantErr: true, wantCause: errBacktestOutcome, wantRender: true},
		{name: "partial", outcome: alerter.BacktestRuleOutcomePartial, wantErr: true, wantCause: errBacktestOutcome, wantRender: true},
		{name: "completed with runner error", outcome: alerter.BacktestRuleOutcomeCompleted, runErr: runnerErr, wantErr: true, wantCause: runnerErr, wantRender: true},
		{name: "partial with runner error", outcome: alerter.BacktestRuleOutcomePartial, runErr: runnerErr, wantErr: true, wantCause: runnerErr, wantRender: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := false
			deps := testBacktestDeps(func(context.Context, *alerter.AlerterOpts, string, alerter.BacktestOptions) (*alerter.BacktestReport, error) {
				return &alerter.BacktestReport{Rule: alerter.BacktestRuleResult{Outcome: tt.outcome}}, tt.runErr
			})
			deps.render = func(io.Writer, string, *alerter.BacktestReport) error {
				rendered = true
				return nil
			}
			err := runBacktestCommand(t, deps, validBacktestArgs()...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				if exitErr, ok := err.(cli.ExitCoder); !ok || exitErr.ExitCode() != 1 {
					t.Fatalf("error = %T %v, want exit code 1", err, err)
				}
				if err.Error() != "backtest failed" {
					t.Fatalf("safe error = %q, want %q", err.Error(), "backtest failed")
				}
				if tt.wantCause != nil && !errors.Is(err, tt.wantCause) {
					t.Fatalf("error does not preserve cause %v", tt.wantCause)
				}
			}
			if rendered != tt.wantRender {
				t.Fatalf("rendered = %v, want %v", rendered, tt.wantRender)
			}
		})
	}
}

func TestBacktestCommandPreservesRenderError(t *testing.T) {
	renderErr := errors.New("write failed")
	deps := testBacktestDeps(func(context.Context, *alerter.AlerterOpts, string, alerter.BacktestOptions) (*alerter.BacktestReport, error) {
		return completedBacktestReport(), nil
	})
	deps.render = func(io.Writer, string, *alerter.BacktestReport) error { return renderErr }

	err := runBacktestCommand(t, deps, validBacktestArgs()...)
	if err == nil || !errors.Is(err, renderErr) {
		t.Fatalf("error = %v, want preserved render error", err)
	}
}

func TestBacktestCommandCombinesRunnerAndRenderErrors(t *testing.T) {
	runnerErr := errors.New("runner failed")
	renderErr := errors.New("render failed")
	deps := testBacktestDeps(func(context.Context, *alerter.AlerterOpts, string, alerter.BacktestOptions) (*alerter.BacktestReport, error) {
		return &alerter.BacktestReport{Rule: alerter.BacktestRuleResult{Outcome: alerter.BacktestRuleOutcomePartial}}, runnerErr
	})
	deps.render = func(io.Writer, string, *alerter.BacktestReport) error { return renderErr }

	err := runBacktestCommand(t, deps, validBacktestArgs()...)
	if err == nil || !errors.Is(err, runnerErr) || !errors.Is(err, renderErr) || !errors.Is(err, errBacktestOutcome) {
		t.Fatalf("error = %v, want runner, render, and outcome causes", err)
	}
}

func TestBacktestCommandInjectedRunnerWritesPureJSON(t *testing.T) {
	stdout := new(bytes.Buffer)
	report := backtestRendererReport()
	report.Rule.Outcome = alerter.BacktestRuleOutcomeCompleted
	deps := testBacktestDeps(func(context.Context, *alerter.AlerterOpts, string, alerter.BacktestOptions) (*alerter.BacktestReport, error) {
		return report, nil
	})
	deps.render = renderBacktestReport

	args := append(validBacktestArgs(),
		"--format", "json",
		"--auth-token", "token-secret",
		"--auth-msi-id", "msi-secret",
	)
	err := runBacktestCommandWithWriters(deps, stdout, io.Discard, args...)
	if err != nil {
		t.Fatalf("backtest command error = %v", err)
	}
	for _, forbidden := range []string{"token-secret", "msi-secret"} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("stdout contains credential %q: %s", forbidden, stdout.String())
		}
	}
	var document map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout.String())
	}
	if document["version"] != float64(alerter.BacktestReportVersion) {
		t.Fatalf("version = %#v", document["version"])
	}
}

func TestBacktestCommandSubprocessKeepsLogsAndInvalidLevelOffJSONStdout(t *testing.T) {
	if os.Getenv("ADX_MON_BACKTEST_LOG_SUBPROCESS") == "1" {
		report := completedBacktestReport()
		deps := testBacktestDeps(func(context.Context, *alerter.AlerterOpts, string, alerter.BacktestOptions) (*alerter.BacktestReport, error) {
			logger.Info("runner operational message")
			return report, nil
		})
		deps.render = renderBacktestReport
		err := runBacktestCommandWithWriters(deps, os.Stdout, os.Stderr, append(validBacktestArgs(), "--format", "json")...)
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	command := exec.Command(os.Args[0], "-test.run=^TestBacktestCommandSubprocessKeepsLogsAndInvalidLevelOffJSONStdout$")
	command.Env = append(os.Environ(), "ADX_MON_BACKTEST_LOG_SUBPROCESS=1", "LOG_LEVEL=INVALID")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("subprocess error = %v\nstderr: %s", err, stderr.String())
	}
	var report alerter.BacktestReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("stdout is not exactly parseable JSON: %v\n%s", err, stdout.String())
	}
	if strings.Contains(stdout.String(), "runner operational message") || strings.Contains(stdout.String(), "Unknown log level") {
		t.Fatalf("stdout contains operational logs: %s", stdout.String())
	}
	for _, message := range []string{"runner operational message", "Unknown log level"} {
		if !strings.Contains(stderr.String(), message) {
			t.Fatalf("stderr missing %q: %s", message, stderr.String())
		}
	}
}

func TestRootAndLintLogsStayOnStderr(t *testing.T) {
	mode := os.Getenv("ADX_MON_ALERTER_LOG_SUBPROCESS")
	if mode != "" {
		var args []string
		switch mode {
		case "root":
			args = []string{"alerter", "--tag", "team=platform", "--kubeconfig", "/not/a/kubeconfig"}
		case "lint":
			args = []string{"alerter", "lint", "--lint-dir", "/not/a/rule/directory", "--tag", "team=platform"}
		default:
			os.Exit(2)
		}
		os.Exit(runAlerter(args, os.Stderr))
	}

	for _, mode := range []string{"root", "lint"} {
		t.Run(mode, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestRootAndLintLogsStayOnStderr$")
			command.Env = append(os.Environ(), "ADX_MON_ALERTER_LOG_SUBPROCESS="+mode)
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			err := command.Run()
			if err == nil {
				t.Fatal("subprocess succeeded, want command setup failure")
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout contains logs: %s", stdout.String())
			}
			if !strings.Contains(stderr.String(), "Using tag team=platform") {
				t.Fatalf("stderr missing operational log: %s", stderr.String())
			}
		})
	}
}

func TestFailedJSONBacktestSubprocessKeepsStdoutParseable(t *testing.T) {
	if os.Getenv("ADX_MON_BACKTEST_SUBPROCESS") == "1" {
		code := runAlerter([]string{
			"alerter", "backtest",
			"--rule", os.Getenv("ADX_MON_BACKTEST_RULE"),
			"--start", "2026-07-01T00:00:00Z",
			"--end", "2026-07-01T00:05:00Z",
			"--kusto-endpoint", "DB=https://cluster.example.test",
			"--concurrency", "1",
			"--format", "json",
		}, os.Stderr)
		os.Exit(code)
	}

	rulePath := writeCommandBacktestRule(t, "  interval: 0s\n")
	command := exec.Command(os.Args[0], "-test.run=^TestFailedJSONBacktestSubprocessKeepsStdoutParseable$")
	command.Env = append(os.Environ(),
		"ADX_MON_BACKTEST_SUBPROCESS=1",
		"ADX_MON_BACKTEST_RULE="+rulePath,
	)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		t.Fatal("subprocess succeeded, want exit code 1")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("subprocess error = %v, want exit code 1", err)
	}
	var report alerter.BacktestReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("stdout is not exactly parseable JSON: %v\n%s", err, stdout.String())
	}
	if report.Rule.Outcome != alerter.BacktestRuleOutcomeInvalid {
		t.Fatalf("outcome = %q, want invalid", report.Rule.Outcome)
	}
	if strings.Contains(stdout.String(), "backtest failed") {
		t.Fatalf("stdout contains failure text: %s", stdout.String())
	}
	if strings.Count(stderr.String(), "backtest failed") != 1 || !strings.HasSuffix(stderr.String(), "backtest failed\n") {
		t.Fatalf("stderr = %q, want one sanitized failure line", stderr.String())
	}
}

func TestBacktestCommandOutputFileWritesExactFormatAndLeavesStdoutEmpty(t *testing.T) {
	stdout := new(bytes.Buffer)
	file := new(testWriteCloser)
	report := backtestRendererReport()
	report.Rule.Outcome = alerter.BacktestRuleOutcomeCompleted
	deps := testBacktestDeps(func(context.Context, *alerter.AlerterOpts, string, alerter.BacktestOptions) (*alerter.BacktestReport, error) {
		return report, nil
	})
	deps.render = renderBacktestReport
	deps.openOutput = func(path string) (io.WriteCloser, error) {
		if path != "report.json" {
			t.Fatalf("output path = %q", path)
		}
		return file, nil
	}

	err := runBacktestCommandWithWriters(deps, stdout, io.Discard, append(validBacktestArgs(), "--format", "json", "--output", "report.json")...)
	if err != nil {
		t.Fatalf("backtest command error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	var expected bytes.Buffer
	if err := renderBacktestReport(&expected, backtestFormatJSON, report); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(file.Bytes(), expected.Bytes()) {
		t.Fatalf("file output mismatch\ngot: %s\nwant: %s", file.String(), expected.String())
	}
	if !file.closed {
		t.Fatal("output file was not closed")
	}
}

func TestWriteBacktestReportRendersBeforeOpening(t *testing.T) {
	renderErr := errors.New("render failed")
	opened := false
	deps := testBacktestDeps(nil)
	deps.render = func(io.Writer, string, *alerter.BacktestReport) error { return renderErr }
	deps.openOutput = func(string) (io.WriteCloser, error) {
		opened = true
		return new(testWriteCloser), nil
	}

	err := writeBacktestReport(io.Discard, "report.json", backtestFormatJSON, completedBacktestReport(), deps)
	if !errors.Is(err, renderErr) {
		t.Fatalf("error = %v, want render error", err)
	}
	if opened {
		t.Fatal("output was opened before rendering completed")
	}
}

func TestWriteBacktestReportOutputFailures(t *testing.T) {
	openErr := errors.New("open failed")
	writeErr := errors.New("write failed")
	closeErr := errors.New("close failed")
	tests := []struct {
		name      string
		open      outputOpener
		want      []error
		wantClose bool
	}{
		{
			name: "open",
			open: func(string) (io.WriteCloser, error) { return nil, openErr },
			want: []error{openErr},
		},
		{
			name: "write and close",
			open: func(string) (io.WriteCloser, error) {
				return &testWriteCloser{writeErr: writeErr, closeErr: closeErr}, nil
			},
			want:      []error{writeErr, closeErr},
			wantClose: true,
		},
		{
			name: "short write",
			open: func(string) (io.WriteCloser, error) {
				return &testWriteCloser{shortWrite: true}, nil
			},
			want:      []error{io.ErrShortWrite},
			wantClose: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output *testWriteCloser
			deps := testBacktestDeps(nil)
			deps.render = func(w io.Writer, _ string, _ *alerter.BacktestReport) error {
				_, err := io.WriteString(w, "report\n")
				return err
			}
			deps.openOutput = func(path string) (io.WriteCloser, error) {
				opened, err := tt.open(path)
				if writer, ok := opened.(*testWriteCloser); ok {
					output = writer
				}
				return opened, err
			}

			err := writeBacktestReport(io.Discard, "report.txt", backtestFormatText, completedBacktestReport(), deps)
			for _, want := range tt.want {
				if !errors.Is(err, want) {
					t.Fatalf("error = %v, want %v", err, want)
				}
			}
			if tt.wantClose && (output == nil || !output.closed) {
				t.Fatal("output was not closed")
			}
		})
	}
}

func TestBacktestCommandCombinesRunnerAndOutputOpenErrors(t *testing.T) {
	runnerErr := errors.New("runner failed")
	openErr := errors.New("open failed")
	deps := testBacktestDeps(func(context.Context, *alerter.AlerterOpts, string, alerter.BacktestOptions) (*alerter.BacktestReport, error) {
		return &alerter.BacktestReport{Rule: alerter.BacktestRuleResult{Outcome: alerter.BacktestRuleOutcomePartial}}, runnerErr
	})
	deps.render = func(w io.Writer, _ string, _ *alerter.BacktestReport) error {
		_, err := io.WriteString(w, "report\n")
		return err
	}
	deps.openOutput = func(string) (io.WriteCloser, error) { return nil, openErr }

	err := runBacktestCommand(t, deps, append(validBacktestArgs(), "--output", "report.txt")...)
	if !errors.Is(err, runnerErr) || !errors.Is(err, openErr) || !errors.Is(err, errBacktestOutcome) {
		t.Fatalf("error = %v, want runner, open, and outcome errors", err)
	}
}

func TestBacktestCommandSignalContextAndCleanup(t *testing.T) {
	var gotSignals []os.Signal
	cleanupCalled := false
	deps := testBacktestDeps(func(ctx context.Context, _ *alerter.AlerterOpts, _ string, _ alerter.BacktestOptions) (*alerter.BacktestReport, error) {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("runner context error = %v, want context.Canceled", ctx.Err())
		}
		return &alerter.BacktestReport{Rule: alerter.BacktestRuleResult{Outcome: alerter.BacktestRuleOutcomePartial}}, context.Canceled
	})
	deps.notifyContext = func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		gotSignals = append([]os.Signal(nil), signals...)
		ctx, cancel := context.WithCancel(parent)
		cancel()
		return ctx, func() { cleanupCalled = true }
	}

	err := runBacktestCommand(t, deps, validBacktestArgs()...)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation cause", err)
	}
	wantSignals := []os.Signal{os.Interrupt, syscall.SIGTERM}
	if !reflect.DeepEqual(gotSignals, wantSignals) {
		t.Fatalf("signals = %#v, want %#v", gotSignals, wantSignals)
	}
	if !cleanupCalled {
		t.Fatal("signal cleanup was not called")
	}
}

func testBacktestDeps(run backtestRunner) backtestCommandDeps {
	return backtestCommandDeps{
		run: run,
		notifyContext: func(ctx context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
			return ctx, func() {}
		},
		render: func(io.Writer, string, *alerter.BacktestReport) error { return nil },
		openOutput: func(string) (io.WriteCloser, error) {
			return nil, errors.New("unexpected output file")
		},
	}
}

func runBacktestCommand(t *testing.T, deps backtestCommandDeps, args ...string) error {
	t.Helper()
	return runBacktestCommandWithWriters(deps, io.Discard, io.Discard, args...)
}

func runBacktestCommandWithWriters(deps backtestCommandDeps, stdout, stderr io.Writer, args ...string) error {
	app := &cli.App{
		Name:           "alerter",
		Commands:       []*cli.Command{newBacktestCommand(deps)},
		Writer:         stdout,
		ErrWriter:      stderr,
		ExitErrHandler: func(*cli.Context, error) {},
	}
	return app.RunContext(context.Background(), append([]string{"alerter", "backtest"}, args...))
}

func validBacktestArgs() []string {
	return []string{
		"--rule", "rule.yaml",
		"--start", "2026-07-01T00:00:00Z",
		"--end", "2026-07-01T01:00:00Z",
		"--kusto-endpoint", "Events=https://events.test",
	}
}

func completedBacktestReport() *alerter.BacktestReport {
	return &alerter.BacktestReport{Rule: alerter.BacktestRuleResult{Outcome: alerter.BacktestRuleOutcomeCompleted}}
}

func writeCommandBacktestRule(t *testing.T, interval string) string {
	t.Helper()
	path := t.TempDir() + "/rule.yaml"
	contents := "apiVersion: adx-mon.azure.com/v1\nkind: AlertRule\nmetadata:\n  name: rule\n  namespace: namespace\nspec:\n  database: DB\n" + interval + "  query: Events\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type testWriteCloser struct {
	bytes.Buffer
	writeErr   error
	closeErr   error
	shortWrite bool
	closed     bool
}

func (w *testWriteCloser) Write(data []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	if w.shortWrite && len(data) != 0 {
		return len(data) - 1, nil
	}
	return w.Buffer.Write(data)
}

func (w *testWriteCloser) Close() error {
	w.closed = true
	return w.closeErr
}
