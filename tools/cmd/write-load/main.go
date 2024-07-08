package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/promremote"
	"github.com/Azure/adx-mon/tools/data"
)

type generator interface {
	Read() (*prompb.WriteRequest, error)
}

// This program generates remote write load against a target endpoint.
func main() {
	var (
		dataFile, database, target      string
		verbose, dryRun                 bool
		concurrency                     int
		batchSize, metrics, cardinality int
		totalSamples                    int64
	)

	flag.BoolVar(&verbose, "verbose", false, "Verbose logging")
	flag.StringVar(&dataFile, "data-file", "", "Data file input created from data-gen utility")
	flag.StringVar(&target, "target", "", "Remote write URL")
	flag.IntVar(&concurrency, "concurrency", 1, "Concurrent writers")

	flag.StringVar(&database, "database", "FakeDatabase", "Database name")
	flag.IntVar(&batchSize, "batch-size", 2500, "Batch size of requests")
	flag.IntVar(&metrics, "metrics", 100, "Numbe of distinct metrics")
	flag.IntVar(&cardinality, "cardinality", 100000, "Total cardinality per metric")
	flag.BoolVar(&dryRun, "dry-run", false, "Read data but don't send it")
	flag.Int64Var(&totalSamples, "total-samples", 0, "Total samples to send (0 = continuous)")

	flag.Parse()

	if database == "" {
		logger.Fatalf("database is required")
	}

	var dataGen generator

	generator := &continuousDataGenerator{
		set: data.NewDataSet(data.SetOptions{
			NumMetrics:  metrics,
			Cardinality: cardinality,
			Database:    database,
		}),
		startTime:    time.Now().UTC(),
		batchSize:    batchSize,
		totalSamples: totalSamples,
	}
	dataGen = generator

	if dataFile != "" {
		r, err := data.NewFileReader(dataFile)
		if err != nil {
			logger.Fatalf("open file: %s", err)
		}
		defer r.Close()
		dataGen = r
	}

	batches := make(chan *prompb.WriteRequest, 1000)

	stats := newStats()

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			writer(ctx, target, stats, batches)
		}()
	}

	go reportStats(ctx, stats, batches)

	stats.StartTimer()

	for {

		var err error
		batch, err := dataGen.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			fmt.Println(err.Error())
			return
		}

		if !dryRun {
			batches <- batch
		} else {
			for _, ts := range batch.Timeseries {
				stats.IncTotalSent(len(ts.Samples))
			}
		}
		stats.IncTotalSeries(len(batch.Timeseries))
	}

	for len(batches) > 0 {
		println(len(batches))
		time.Sleep(100 * time.Millisecond)

	}

	stats.StopTimer()
	cancel()
	wg.Wait()

	fmt.Printf("Total Metrics: %d\n", stats.TotalMetrics())
	fmt.Printf("Total Samples: %d\n", stats.TotalSeries())
	fmt.Printf("Duration: %s\n", stats.TimerDuration().String())
	fmt.Printf("Load samples per/sec: %0.2f\n", float64(stats.TotalSeries())/stats.TimerDuration().Seconds())
}

func reportStats(ctx context.Context, stats *stats, batches chan *prompb.WriteRequest) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var lastTotal int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sent := stats.TotalSent() - lastTotal
			fmt.Printf("Samples per/sec: %d (%0.2f samples/sec) (%d/%s) queued=%d/%d\n", sent, float64(stats.TotalSent())/stats.ElapsedDuration().Seconds(), stats.TotalSent(), stats.ElapsedDuration(), len(batches), stats.TotalSeries())
			lastTotal = stats.TotalSent()
		}
	}
}

func writer(ctx context.Context, endpoint string, stats *stats, ch chan *prompb.WriteRequest) {
	cli, err := promremote.NewClient(promremote.ClientOpts{
		InsecureSkipVerify: true,
		Timeout:            30 * time.Second,
	})
	if err != nil {
		logger.Fatalf("prom client: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case batch := <-ch:
			// ts := time.Now()
			for {
				err := cli.Write(context.Background(), endpoint, batch)
				if err == nil {
					break
				}
				logger.Warnf("write failed: %s %s.  Retrying...", endpoint, err)
				time.Sleep(1 * time.Second)
			}
			// println(time.Since(ts).String(), len(batch.Timeseries))
			for _, ts := range batch.Timeseries {
				stats.IncTotalSent(len(ts.Samples))
			}
		}
	}
}

type continuousDataGenerator struct {
	set          *data.Set
	startTime    time.Time
	batchSize    int
	totalSamples int64
	sent         int64
}

func (c *continuousDataGenerator) Read() (*prompb.WriteRequest, error) {
	if c.totalSamples > 0 && atomic.LoadInt64(&c.sent) >= c.totalSamples {
		return nil, io.EOF
	}
	wr := &prompb.WriteRequest{}
	for i := 0; i < c.batchSize; i++ {
		ts := c.set.Next(c.startTime)
		wr.Timeseries = append(wr.Timeseries, ts)
	}
	c.startTime = c.startTime.Add(time.Second)
	atomic.AddInt64(&c.sent, int64(c.batchSize))
	return wr, nil
}

type stats struct {
	totalSeries int64
	totalSent   int64

	metrics map[string]struct{}

	StartTime, StopTime time.Time
}

func newStats() *stats {
	return &stats{
		metrics: map[string]struct{}{},
	}
}

func (s *stats) IncTotalSeries(n int) {
	atomic.AddInt64(&s.totalSeries, int64(n))
}

func (s *stats) TotalSeries() int64 {
	return atomic.LoadInt64(&s.totalSeries)
}

func (s *stats) RecordMetric(name string) {
	s.metrics[name] = struct{}{}
}

func (s *stats) TotalMetrics() int {
	return len(s.metrics)
}

func (s *stats) StartTimer() {
	s.StartTime = time.Now()
}

func (s *stats) StopTimer() {
	s.StopTime = time.Now()
}

func (s *stats) ElapsedDuration() time.Duration {
	return time.Since(s.StartTime)
}

func (s *stats) TimerDuration() time.Duration {
	return s.StopTime.Sub(s.StartTime)
}

func (s *stats) IncTotalSent(n int) {
	atomic.AddInt64(&s.totalSent, int64(n))
}

func (s *stats) TotalSent() int64 {
	return atomic.LoadInt64(&s.totalSent)
}
