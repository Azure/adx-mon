// log-load generates OTLP HTTP load or application JSON lines for a file tailer.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
)

type options struct {
	target               string
	concurrency          int
	batchSize            int
	totalSamples         int64
	database             string
	table                string
	collectorLogDir      string
	printCollectorConfig bool
	dryRun               bool
	verbose              bool
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runCLI(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "log-load: %v\n", err)
		os.Exit(1)
	}
}

func runCLI(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var opts options
	flags := flag.NewFlagSet("log-load", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&opts.target, "target", "", "HTTP/HTTPS base URL or file:///absolute/directory")
	flags.IntVar(&opts.concurrency, "concurrency", 1, "Concurrent writers (one file per writer for file targets)")
	flags.IntVar(&opts.batchSize, "batch-size", 2500, "Maximum records per batch")
	flags.Int64Var(&opts.totalSamples, "total-samples", 0, "Total records to generate (0 = continuous)")
	flags.StringVar(&opts.database, "database", "AKSinfra", "Destination database")
	flags.StringVar(&opts.table, "table", "IngestTest", "Destination table")
	flags.StringVar(&opts.collectorLogDir, "collector-log-dir", "", "Absolute collector-visible file directory for printed config")
	flags.BoolVar(&opts.printCollectorConfig, "print-collector-config", false, "Print a collector TOML fragment for a file target, without generating load")
	flags.BoolVar(&opts.dryRun, "dry-run", false, "Generate and count records without opening files or making HTTP requests")
	flags.BoolVar(&opts.verbose, "verbose", false, "Report progress to stderr once per second")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	return run(ctx, opts, stdout, stderr)
}

func run(ctx context.Context, opts options, stdout, stderr io.Writer) error {
	target, err := opts.validate()
	if err != nil {
		return err
	}
	if opts.printCollectorConfig {
		return printCollectorConfig(stdout, opts, target)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	var writers []batchWriter
	if !opts.dryRun {
		writers, err = openWriters(target, opts.concurrency)
		if err != nil {
			return err
		}
	}
	gen := &continuousDataGenerator{
		batchSize: opts.batchSize, totalSamples: opts.totalSamples,
		database: opts.database, table: opts.table,
	}
	s := &stats{}
	start := time.Now()
	reportCtx, stopReport := context.WithCancel(context.Background())
	var reportWG sync.WaitGroup
	if opts.verbose {
		reportWG.Go(func() { reportStats(reportCtx, stderr, s, start) })
	}
	err = runPipeline(ctx, gen, writers, s, opts.dryRun)
	stopReport()
	reportWG.Wait()
	elapsed := time.Since(start)
	_, printErr := fmt.Fprintf(stdout,
		"Generated records: %d\nWritten records: %d\nWritten bytes: %d\nDry-run records: %d\nDuration: %s\nWritten records/sec: %.2f\nWritten bytes/sec: %.2f\n",
		s.generated.Load(), s.written.Load(), s.bytes.Load(), s.dryRun.Load(), elapsed,
		float64(s.written.Load())/elapsed.Seconds(), float64(s.bytes.Load())/elapsed.Seconds())
	if target.directory != "" && !opts.dryRun {
		_, noteErr := fmt.Fprintln(stdout, "File statistics measure generator output, not collector consumption or downstream delivery.")
		printErr = errors.Join(printErr, noteErr)
	}
	return errors.Join(err, printErr)
}

type generator interface {
	Read() (*v1.ExportLogsServiceRequest, error)
}

// runPipeline closes the queue when production ends and waits for every writer,
// including its final flush/close. Cancellation stops queued work, but file
// writers finish their current batch and flush their owned buffers.
func runPipeline(parent context.Context, gen generator, writers []batchWriter, s *stats, dryRun bool) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	batches := make(chan *v1.ExportLogsServiceRequest, max(1, 2*len(writers)))
	errs := make(chan error, len(writers))
	var wg sync.WaitGroup
	for i, writer := range writers {
		wg.Go(func() {
			var writeErr error
			defer func() {
				result, closeErr := writer.Close()
				s.add(result)
				if err := errors.Join(writeErr, closeErr); err != nil {
					cancel()
					errs <- fmt.Errorf("writer %d: %w", i, err)
				}
			}()
			for {
				if ctx.Err() != nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case batch, ok := <-batches:
					if !ok || ctx.Err() != nil {
						return
					}
					result, err := writer.WriteBatch(ctx, batch)
					s.add(result)
					if err != nil {
						writeErr = err
						cancel()
						return
					}
				}
			}
		})
	}
	var generationErr error
produce:
	for ctx.Err() == nil {
		batch, err := gen.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			generationErr = err
			cancel()
			break
		}
		n := recordCount(batch)
		s.generated.Add(n)
		if dryRun {
			s.dryRun.Add(n)
			continue
		}
		select {
		case batches <- batch:
		case <-ctx.Done():
			break produce
		}
	}
	close(batches)
	wg.Wait()
	close(errs)
	result := errors.Join(generationErr, parent.Err())
	for err := range errs {
		result = errors.Join(result, err)
	}
	return result
}

type stats struct {
	generated atomic.Int64
	written   atomic.Int64
	bytes     atomic.Int64
	dryRun    atomic.Int64
}

func (s *stats) add(result writeResult) {
	s.written.Add(result.records)
	s.bytes.Add(result.bytes)
}

func reportStats(ctx context.Context, output io.Writer, s *stats, start time.Time) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fmt.Fprintf(output, "generated=%d written=%d bytes=%d dry-run=%d elapsed=%s records/sec=%.2f\n",
				s.generated.Load(), s.written.Load(), s.bytes.Load(), s.dryRun.Load(), time.Since(start),
				float64(s.written.Load())/time.Since(start).Seconds())
		}
	}
}
