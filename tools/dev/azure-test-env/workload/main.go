// A deliberately tiny, deterministic four-path E2E signal source.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	logspb "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	metricspb "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/metrics/v1"
	commonpb "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	logdata "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	metricdata "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/metrics/v1"
	"google.golang.org/protobuf/proto"
)

type signal struct {
	RunID     string `json:"run_id"`
	Seq       int    `json:"seq"`
	EmittedMS int64  `json:"emitted_ms"`
}

var mu sync.Mutex
var latest signal
var client = &http.Client{Timeout: 5 * time.Second}

func kv(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
}

func post(url string, msg proto.Message) error {
	b, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	resp, err := client.Post(url, "application/x-protobuf", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%s: %s: %s", url, resp.Status, body)
	}
	return nil
}

func emit(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("run")
	seq, err := strconv.Atoi(r.URL.Query().Get("seq"))
	if id == "" || err != nil || seq < 1 {
		http.Error(w, "run and positive seq required", 400)
		return
	}
	s := signal{RunID: id, Seq: seq, EmittedMS: time.Now().UnixMilli()}
	mu.Lock()
	latest = s
	mu.Unlock()
	// Native pod log line; flush immediately (the Go log writer is unbuffered).
	line, _ := json.Marshal(s)
	fmt.Println(string(line))
	base := "http://" + os.Getenv("HOST_IP") + ":3100"
	attrs := []*commonpb.KeyValue{kv("kusto.database", "Logs"), kv("kusto.table", "E2eOtlpLogs"), kv("run_id", id), kv("seq", strconv.Itoa(seq)), kv("emitted_ms", strconv.FormatInt(s.EmittedMS, 10))}
	logs := &logspb.ExportLogsServiceRequest{ResourceLogs: []*logdata.ResourceLogs{{ScopeLogs: []*logdata.ScopeLogs{{LogRecords: []*logdata.LogRecord{{TimeUnixNano: uint64(s.EmittedMS) * 1e6, ObservedTimeUnixNano: uint64(s.EmittedMS) * 1e6, Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: string(line)}}, Attributes: attrs}}}}}}}
	points := []*commonpb.KeyValue{kv("run_id", id), kv("seq", strconv.Itoa(seq)), kv("emitted_ms", strconv.FormatInt(s.EmittedMS, 10))}
	metrics := &metricspb.ExportMetricsServiceRequest{ResourceMetrics: []*metricdata.ResourceMetrics{{ScopeMetrics: []*metricdata.ScopeMetrics{{Metrics: []*metricdata.Metric{{Name: "e2e_otlp_sample", Data: &metricdata.Metric_Gauge{Gauge: &metricdata.Gauge{DataPoints: []*metricdata.NumberDataPoint{{TimeUnixNano: uint64(s.EmittedMS) * 1e6, Attributes: points, Value: &metricdata.NumberDataPoint_AsInt{AsInt: 1}}}}}}}}}}}}
	if err := post(base+"/v1/logs", logs); err != nil {
		log.Printf("OTLP logs: %v", err)
		http.Error(w, err.Error(), 502)
		return
	}
	attrs[1] = kv("kusto.table", "E2eQueuedControl")
	if err := post(base+"/v1/logs", logs); err != nil {
		log.Printf("OTLP control logs: %v", err)
		http.Error(w, err.Error(), 502)
		return
	}
	if err := post(base+"/v1/otlpmetrics", metrics); err != nil {
		log.Printf("OTLP metrics: %v", err)
		http.Error(w, err.Error(), 502)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

func scrape(w http.ResponseWriter, _ *http.Request) {
	mu.Lock()
	s := latest
	mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	// Expose a new sample long enough for the one-second scraper to see it,
	// but do not send the last sample forever while queued control data drains.
	if s.RunID != "" && time.Since(time.UnixMilli(s.EmittedMS)) < 2*time.Second {
		fmt.Fprintf(w, "e2e_scraped_sample{run_id=%q,seq=%q,emitted_ms=%q} 1\n", s.RunID, strconv.Itoa(s.Seq), strconv.FormatInt(s.EmittedMS, 10))
	}
}

func main() {
	http.HandleFunc("/emit", emit)
	http.HandleFunc("/metrics", scrape)
	log.Fatal(http.ListenAndServe(":8088", nil))
}
