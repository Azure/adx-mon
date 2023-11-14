package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	otelv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	resourcev1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/resource/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/tools/data"
	"github.com/golang/protobuf/proto"
	"google.golang.org/protobuf/encoding/protojson"
)

type generator interface {
	Read() (*v1.ExportLogsServiceRequest, error)
}

// This program generates remote write load against a target endpoint.
func main() {
	var (
		target                          string
		verbose, dryRun                 bool
		concurrency                     int
		batchSize, metrics, cardinality int
		totalSamples                    int64
	)

	flag.BoolVar(&verbose, "verbose", false, "Verbose logging")
	flag.StringVar(&target, "target", "", "Remote write URL")
	flag.IntVar(&concurrency, "concurrency", 1, "Concurrent writers")

	flag.IntVar(&batchSize, "batch-size", 2500, "Batch size of requests")
	flag.BoolVar(&dryRun, "dry-run", false, "Read data but don't send it")
	flag.Int64Var(&totalSamples, "total-samples", 0, "Total samples to send (0 = continuous)")

	flag.Parse()

	var msg v1.ExportLogsServiceRequest
	protojson.Unmarshal(rawlog, &msg)

	b, err := proto.Marshal(&msg)
	if err != nil {
		logger.Fatalf("marshal: %s", err)
	}
	req, err := http.NewRequest("POST", target, bytes.NewReader(b))
	if err != nil {
		logger.Fatalf("new request: %s", err)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Fatalf("do: %s", err)
	}
	if resp.StatusCode != http.StatusOK {
		logger.Fatalf("status: %s", resp.Status)
	}
	defer resp.Body.Close()

	var dataGen generator

	generator := &continuousDataGenerator{
		set: data.NewDataSet(data.SetOptions{
			NumMetrics:  metrics,
			Cardinality: cardinality,
		}),
		startTime:    time.Now().UTC(),
		batchSize:    batchSize,
		totalSamples: totalSamples,
	}
	dataGen = generator

	batches := make(chan *v1.ExportLogsServiceRequest, 1000)

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
			for _, ts := range batch.ResourceLogs {
				for _, scope := range ts.ScopeLogs {
					stats.IncTotalSent(len(scope.LogRecords))
				}
			}
		}
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

func reportStats(ctx context.Context, stats *stats, batches chan *v1.ExportLogsServiceRequest) {
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

func writer(ctx context.Context, endpoint string, stats *stats, ch chan *v1.ExportLogsServiceRequest) {
	for {
		select {
		case <-ctx.Done():
			return
		case batch := <-ch:
			b, err := proto.Marshal(batch)
			if err != nil {
				logger.Fatalf("marshal: %s", err)
			}
			req, err := http.NewRequest("POST", endpoint, bytes.NewReader(b))
			if err != nil {
				logger.Fatalf("new request: %s", err)
			}
			req.Header.Set("Content-Type", "application/x-protobuf")

			// ts := time.Now()
			for {
				if err := func() error {
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						return err
					}
					defer resp.Body.Close()
					return nil
				}(); err != nil {
					logger.Warnf("write failed: %s.  Retrying...", err)
					time.Sleep(1 * time.Second)
					continue
				}
				break
			}
			// println(time.Since(ts).String(), len(batch.Timeseries))
			for _, ts := range batch.ResourceLogs {
				for _, scope := range ts.ScopeLogs {
					stats.IncTotalSent(len(scope.LogRecords))
				}
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

func (c *continuousDataGenerator) Read() (*v1.ExportLogsServiceRequest, error) {
	if c.totalSamples > 0 && atomic.LoadInt64(&c.sent) >= c.totalSamples {
		return nil, io.EOF
	}

	msg := newExportLogRequest()
	for i := 0; i < c.batchSize; i++ {
		msg.ResourceLogs[0].ScopeLogs[0].LogRecords = append(msg.ResourceLogs[0].ScopeLogs[0].LogRecords,
			&otelv1.LogRecord{
				TimeUnixNano:   uint64(time.Now().UnixNano()),
				SeverityNumber: 17,
				SeverityText:   "Error",
				Body: &commonv1.AnyValue{
					Value: &commonv1.AnyValue_KvlistValue{
						KvlistValue: &commonv1.KeyValueList{
							Values: []*commonv1.KeyValue{
								{
									Key: "message",
									Value: &commonv1.AnyValue{
										Value: &commonv1.AnyValue_StringValue{
											StringValue: "something worth logging",
										},
									},
								},
								{
									Key: "kusto.table",
									Value: &commonv1.AnyValue{
										Value: &commonv1.AnyValue_StringValue{
											StringValue: "IngestTest",
										},
									},
								},
								{
									Key: "kusto.database",
									Value: &commonv1.AnyValue{
										Value: &commonv1.AnyValue_StringValue{
											StringValue: "AKSinfra",
										},
									},
								},
							},
						},
					},
				},
				DroppedAttributesCount: 1,
				Flags:                  1,
			},
		)
	}
	c.startTime = c.startTime.Add(time.Second)
	atomic.AddInt64(&c.sent, int64(c.batchSize))
	return msg, nil
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

func newExportLogRequest() *v1.ExportLogsServiceRequest {
	return &v1.ExportLogsServiceRequest{
		ResourceLogs: []*otelv1.ResourceLogs{
			{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{
						{
							Key: "source",
							Value: &commonv1.AnyValue{
								Value: &commonv1.AnyValue_StringValue{
									StringValue: "hostname",
								},
							},
						},
					},
					DroppedAttributesCount: 1,
				},
				ScopeLogs: []*otelv1.ScopeLogs{
					{
						Scope: &commonv1.InstrumentationScope{
							Name:                   "name",
							Version:                "version",
							DroppedAttributesCount: 1,
						},
					},
				},
			},
		},
	}
}

var rawlog = []byte(`{
	"resourceLogs": [
		{
			"resource": {
				"attributes": [
					{
						"key": "source"
						"value": {
							"stringValue": "hostname"
						}
					}
				],
				"droppedAttributesCount": 1
			},
			"scopeLogs": [
				{
					"scope": {
						"name": "name",
						"version": "version",
						"droppedAttributesCount": 1
					},
					"logRecords": [
						{
							"timeUnixNano": "1669112524001",
							"observedTimeUnixNano": "1669112524001",
							"severityNumber": 17,
							"severityText": "Error",
							"body": {
								"kvlistValue": {
									"values": [
									  {
										"key": "message",
										"value": {
										  "stringValue": "something worth logging"
										}
									  },
									  {
										"key": "kusto.table",
										"value": {
										  "stringValue": "ATable"
										}
									  },
									  {
										"key": "kusto.database",
										"value": {
										  "stringValue": "ADatabase"
										}
									  }
									]
								  }
							},
							"droppedAttributesCount": 1,
							"flags": 1,
							"traceId": "",
							"spanId": ""
						}
					],
					"schemaUrl": "scope_schema"
				}
			],
			"schemaUrl": "resource_schema"
		}
	]
}`)
