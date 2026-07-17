package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Azure/adx-mon/alerter"
	"github.com/urfave/cli/v2"
)

const (
	backtestFormatText = "text"
	backtestFormatJSON = "json"
)

var errBacktestOutcome = errors.New("backtest report has an unsuccessful outcome")

type backtestRunner func(context.Context, *alerter.AlerterOpts, string, alerter.BacktestOptions) (*alerter.BacktestReport, error)
type notifyContextFunc func(context.Context, ...os.Signal) (context.Context, context.CancelFunc)
type backtestRenderer func(io.Writer, string, *alerter.BacktestReport) error
type outputOpener func(string) (io.WriteCloser, error)

type backtestCommandDeps struct {
	run           backtestRunner
	notifyContext notifyContextFunc
	render        backtestRenderer
	openOutput    outputOpener
}

func defaultBacktestCommandDeps() backtestCommandDeps {
	return backtestCommandDeps{
		run:           alerter.Backtest,
		notifyContext: signal.NotifyContext,
		render:        renderBacktestReport,
		openOutput: func(path string) (io.WriteCloser, error) {
			return os.Create(path)
		},
	}
}

func NewBacktestCommand() *cli.Command {
	return newBacktestCommand(defaultBacktestCommandDeps())
}

func newBacktestCommand(deps backtestCommandDeps) *cli.Command {
	return &cli.Command{
		Name:  "backtest",
		Usage: "evaluate one local alert rule over a historical time range",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "rule", Usage: "Local AlertRule file", Required: true},
			&cli.TimestampFlag{Name: "start", Usage: "Inclusive range start in RFC3339 format", Layout: time.RFC3339, Required: true},
			&cli.TimestampFlag{Name: "end", Usage: "Exclusive range end in RFC3339 format", Layout: time.RFC3339, Required: true},
			&cli.StringSliceFlag{Name: "kusto-endpoint", Usage: "Kusto endpoint in the format of <name>=<endpoint>", Required: true},
			&cli.StringFlag{Name: "auth-msi-id", Usage: "MSI client ID for authentication to Kusto"},
			&cli.StringFlag{Name: "auth-token", Usage: "Application token for authentication to Kusto"},
			&cli.StringFlag{Name: "region", Usage: "Current region"},
			&cli.StringFlag{Name: "cloud", Usage: "Azure cloud"},
			&cli.StringSliceFlag{Name: "tag", Usage: "Tag in the format of <key>=<value> that applies to execution context"},
			&cli.IntFlag{Name: "concurrency", Value: 4, Usage: "Maximum number of windows to evaluate concurrently"},
			&cli.IntFlag{Name: "max-results-per-window", Value: 25, Usage: "Maximum number of alerts retained per window"},
			&cli.DurationFlag{Name: "query-timeout", Value: 5 * time.Minute, Usage: "Timeout for one window query"},
			&cli.IntFlag{Name: "max-windows", Value: 1000, Usage: "Maximum number of windows to evaluate"},
			&cli.StringFlag{Name: "format", Value: backtestFormatText, Usage: "Output format: text or json"},
			&cli.StringFlag{Name: "output", Usage: "Output file path; defaults to stdout"},
		},
		Action: func(ctx *cli.Context) error {
			return backtestMain(ctx, deps)
		},
	}
}

func backtestMain(ctx *cli.Context, deps backtestCommandDeps) error {
	if err := validateBacktestDependencies(deps); err != nil {
		return newBacktestCommandError("backtest command is not configured", err)
	}

	rulePath := ctx.String("rule")
	if strings.TrimSpace(rulePath) == "" {
		return cli.Exit("rule must not be empty", 1)
	}

	endpointArgs := ctx.StringSlice("kusto-endpoint")
	if len(endpointArgs) == 0 {
		return cli.Exit("at least one kusto-endpoint must be specified", 1)
	}
	for _, endpoint := range endpointArgs {
		if strings.TrimSpace(endpoint) == "" {
			return cli.Exit("kusto-endpoint must not be empty", 1)
		}
	}
	endpoints, err := parseKustoEndpoints(endpointArgs)
	if err != nil {
		return err
	}
	for name, endpoint := range endpoints {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(endpoint) == "" {
			return cli.Exit("kusto-endpoint name and endpoint must not be empty", 1)
		}
	}

	tags, err := parseTags(ctx.StringSlice("tag"))
	if err != nil {
		return err
	}
	for key := range tags {
		if strings.TrimSpace(key) == "" {
			return cli.Exit("tag key must not be empty", 1)
		}
	}
	region := ctx.String("region")
	cloud := ctx.String("cloud")
	addExecutionTags(tags, region, cloud)

	format := ctx.String("format")
	if format != backtestFormatText && format != backtestFormatJSON {
		return cli.Exit("format must be text or json", 1)
	}

	start := ctx.Timestamp("start")
	end := ctx.Timestamp("end")
	if start == nil || end == nil {
		return cli.Exit("start and end must be specified in RFC3339 format", 1)
	}
	backtestOpts := alerter.BacktestOptions{
		Start:               start.UTC(),
		End:                 end.UTC(),
		Concurrency:         ctx.Int("concurrency"),
		MaxResultsPerWindow: ctx.Int("max-results-per-window"),
		QueryTimeout:        ctx.Duration("query-timeout"),
		MaxWindows:          ctx.Int("max-windows"),
	}
	if err := alerter.ValidateBacktestOptions(backtestOpts); err != nil {
		return cli.Exit(err.Error(), 1)
	}

	opts := &alerter.AlerterOpts{
		KustoEndpoints: endpoints,
		Region:         region,
		Cloud:          cloud,
		Tags:           tags,
		MSIID:          ctx.String("auth-msi-id"),
		KustoToken:     ctx.String("auth-token"),
	}

	runCtx, stop := deps.notifyContext(ctx.Context, os.Interrupt, syscall.SIGTERM)
	defer stop()
	report, runErr := deps.run(runCtx, opts, rulePath, backtestOpts)

	var outputErr error
	if report != nil {
		outputErr = writeBacktestReport(ctx.App.Writer, ctx.String("output"), format, report, deps)
	}

	causes := make([]error, 0, 3)
	if runErr != nil {
		causes = append(causes, runErr)
	}
	if outputErr != nil {
		causes = append(causes, outputErr)
	}
	if report == nil {
		if runErr == nil {
			causes = append(causes, errors.New("backtest runner returned no report"))
		}
		return newBacktestCommandError("backtest failed", causes...)
	}
	if report.Rule.Outcome != alerter.BacktestRuleOutcomeCompleted && report.Rule.Outcome != alerter.BacktestRuleOutcomeSkipped {
		causes = append(causes, errBacktestOutcome)
	}
	if len(causes) != 0 {
		return newBacktestCommandError("backtest failed", causes...)
	}
	return nil
}

func validateBacktestDependencies(deps backtestCommandDeps) error {
	if deps.run == nil || deps.notifyContext == nil || deps.render == nil || deps.openOutput == nil {
		return errors.New("backtest command dependency is nil")
	}
	return nil
}

func writeBacktestReport(stdout io.Writer, outputPath, format string, report *alerter.BacktestReport, deps backtestCommandDeps) error {
	var rendered bytes.Buffer
	if err := deps.render(&rendered, format, report); err != nil {
		return err
	}

	if outputPath == "" {
		return writeBacktestBytes(stdout, rendered.Bytes())
	}

	output, err := deps.openOutput(outputPath)
	if err != nil {
		return err
	}
	if output == nil {
		return errors.New("output opener returned a nil writer")
	}
	writeErr := writeBacktestBytes(output, rendered.Bytes())
	closeErr := output.Close()
	return errors.Join(writeErr, closeErr)
}

func writeBacktestBytes(w io.Writer, data []byte) error {
	if w == nil {
		return errors.New("backtest output writer is nil")
	}
	n, err := w.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

type backtestCommandError struct {
	message string
	causes  []error
}

func newBacktestCommandError(message string, causes ...error) error {
	nonNilCauses := make([]error, 0, len(causes))
	for _, cause := range causes {
		if cause != nil {
			nonNilCauses = append(nonNilCauses, cause)
		}
	}
	return &backtestCommandError{message: message, causes: nonNilCauses}
}

func (e *backtestCommandError) Error() string   { return e.message }
func (e *backtestCommandError) ExitCode() int   { return 1 }
func (e *backtestCommandError) Unwrap() []error { return e.causes }
