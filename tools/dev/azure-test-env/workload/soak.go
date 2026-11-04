package main

// Soak traffic: a steady numbered stream on every ingestion path, slow scrape
// targets, and background remote-write/OTLP load. Latency is computed in Kusto
// from each row's emitted_ms, so this file only has to emit and account.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	logspb "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	metricspb "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/metrics/v1"
	commonpb "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	logdata "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	metricdata "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/metrics/v1"
	"github.com/golang/snappy"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// Paths a stream signal is sent on. pod_logs is a stdout line and cannot fail;
// scraped_metrics is sampled by the scraper and is not sent.
var streamPaths = []string{"otlp_logs", "queued_control", "otlp_metrics", "remote_write"}

type streamStatus struct {
	RunID   string           `json:"run_id"`
	Rate    float64          `json:"rate"`
	Chunked bool             `json:"chunked"`
	Started int64            `json:"started_ms"`
	Emitted int              `json:"emitted"`
	Done    bool             `json:"done"`
	Failed  map[string][]int `json:"failed"`
	Errors  map[string]int   `json:"errors"`
}

var (
	streamMu sync.Mutex
	current  *streamStatus
)

type label struct{ name, value string }

// writeRequest encodes a Prometheus remote-write WriteRequest by hand so the
// workload needs no Prometheus packages: timeseries=1{labels=1{name=1,value=2},
// samples=2{value=1,timestamp=2}}.
func writeRequest(series [][]label, value float64, ms int64) []byte {
	var out []byte
	for _, labels := range series {
		var ts []byte
		for _, l := range labels {
			var lb []byte
			lb = protowire.AppendTag(lb, 1, protowire.BytesType)
			lb = protowire.AppendString(lb, l.name)
			lb = protowire.AppendTag(lb, 2, protowire.BytesType)
			lb = protowire.AppendString(lb, l.value)
			ts = protowire.AppendTag(ts, 1, protowire.BytesType)
			ts = protowire.AppendBytes(ts, lb)
		}
		var sample []byte
		sample = protowire.AppendTag(sample, 1, protowire.Fixed64Type)
		sample = protowire.AppendFixed64(sample, math.Float64bits(value))
		sample = protowire.AppendTag(sample, 2, protowire.VarintType)
		sample = protowire.AppendVarint(sample, uint64(ms))
		ts = protowire.AppendTag(ts, 2, protowire.BytesType)
		ts = protowire.AppendBytes(ts, sample)
		out = protowire.AppendTag(out, 1, protowire.BytesType)
		out = protowire.AppendBytes(out, ts)
	}
	return out
}

func remoteWrite(url string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(snappy.Encode(nil, body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	return do(req)
}

// postBody sends a protobuf; chunked hides the length so the request uses
// Transfer-Encoding: chunked, as streaming OTLP exporters do.
func postBody(url string, msg proto.Message, chunked bool) error {
	b, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	var body io.Reader = bytes.NewReader(b)
	if chunked {
		body = io.MultiReader(body) // not a *bytes.Reader, so ContentLength stays unknown
	}
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	return do(req)
}

func do(req *http.Request) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

// sendSignal emits one numbered signal on every path and returns the failures.
func sendSignal(s signal, chunked bool) map[string]error {
	line, _ := json.Marshal(s)
	fmt.Println(string(line))
	mu.Lock()
	latest = s
	mu.Unlock()
	base := "http://" + os.Getenv("HOST_IP") + ":3100"
	seq, emitted := strconv.Itoa(s.Seq), strconv.FormatInt(s.EmittedMS, 10)
	nanos := uint64(s.EmittedMS) * 1e6
	logsFor := func(table string) *logspb.ExportLogsServiceRequest {
		attrs := []*commonpb.KeyValue{kv("kusto.database", "Logs"), kv("kusto.table", table), kv("run_id", s.RunID), kv("seq", seq), kv("emitted_ms", emitted)}
		return &logspb.ExportLogsServiceRequest{ResourceLogs: []*logdata.ResourceLogs{{ScopeLogs: []*logdata.ScopeLogs{{LogRecords: []*logdata.LogRecord{{
			TimeUnixNano: nanos, ObservedTimeUnixNano: nanos, Attributes: attrs,
			Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: string(line)}}}}}}}}}
	}
	points := []*commonpb.KeyValue{kv("run_id", s.RunID), kv("seq", seq), kv("emitted_ms", emitted)}
	metrics := &metricspb.ExportMetricsServiceRequest{ResourceMetrics: []*metricdata.ResourceMetrics{{ScopeMetrics: []*metricdata.ScopeMetrics{{Metrics: []*metricdata.Metric{{
		Name: "e2e_otlp_sample", Data: &metricdata.Metric_Gauge{Gauge: &metricdata.Gauge{DataPoints: []*metricdata.NumberDataPoint{{
			TimeUnixNano: nanos, Attributes: points, Value: &metricdata.NumberDataPoint_AsInt{AsInt: 1}}}}}}}}}}}}
	rw := writeRequest([][]label{{{"__name__", "e2e_remote_write"}, {"emitted_ms", emitted}, {"run_id", s.RunID}, {"seq", seq}}}, 1, s.EmittedMS)

	sends := map[string]func() error{
		"otlp_logs":      func() error { return postBody(base+"/v1/logs", logsFor("E2eOtlpLogs"), chunked) },
		"queued_control": func() error { return postBody(base+"/v1/logs", logsFor("E2eQueuedControl"), chunked) },
		"otlp_metrics":   func() error { return postBody(base+"/v1/otlpmetrics", metrics, false) },
		"remote_write":   func() error { return remoteWrite(base+"/receive", rw) },
	}
	var wg sync.WaitGroup
	var fmu sync.Mutex
	failed := map[string]error{}
	for path, send := range sends {
		wg.Add(1)
		go func(path string, send func() error) {
			defer wg.Done()
			if err := send(); err != nil {
				fmu.Lock()
				failed[path] = err
				fmu.Unlock()
			}
		}(path, send)
	}
	wg.Wait()
	return failed
}

// stream starts emitting rate signals per second for duration seconds. Each
// tick is sent concurrently so a stalled collector lowers neither the rate nor
// the accounting: every emitted seq is either delivered or listed as failed.
func stream(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("run")
	rate, err1 := strconv.ParseFloat(q.Get("rate"), 64)
	duration, err2 := strconv.Atoi(q.Get("duration"))
	if id == "" || err1 != nil || err2 != nil || rate <= 0 || duration < 1 {
		http.Error(w, "run, positive rate and duration required", http.StatusBadRequest)
		return
	}
	st := &streamStatus{RunID: id, Rate: rate, Chunked: q.Get("chunked") == "1",
		Started: time.Now().UnixMilli(), Failed: map[string][]int{}, Errors: map[string]int{}}
	streamMu.Lock()
	if current != nil && !current.Done {
		streamMu.Unlock()
		http.Error(w, "a stream is already running", http.StatusConflict)
		return
	}
	current = st
	streamMu.Unlock()
	go runStream(st, rate, time.Duration(duration)*time.Second)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(st)
}

func runStream(st *streamStatus, rate float64, duration time.Duration) {
	ticker := time.NewTicker(time.Duration(float64(time.Second) / rate))
	defer ticker.Stop()
	end := time.Now().Add(duration)
	var wg sync.WaitGroup
	for seq := 1; time.Now().Before(end); seq++ {
		s := signal{RunID: st.RunID, Seq: seq, EmittedMS: time.Now().UnixMilli()}
		streamMu.Lock()
		st.Emitted = seq
		streamMu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path, err := range sendSignal(s, st.Chunked) {
				streamMu.Lock()
				st.Failed[path] = append(st.Failed[path], s.Seq)
				st.Errors[path+": "+errorClass(err)]++
				streamMu.Unlock()
			}
		}()
		<-ticker.C
	}
	wg.Wait()
	streamMu.Lock()
	st.Done = true
	streamMu.Unlock()
}

// errorClass drops digits so per-request details aggregate into a few classes.
func errorClass(err error) string {
	msg := err.Error()
	if len(msg) > 160 {
		msg = msg[:160]
	}
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return '#'
		}
		return r
	}, msg)
}

func status(w http.ResponseWriter, _ *http.Request) {
	streamMu.Lock()
	defer streamMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if current == nil {
		w.Write([]byte("null\n"))
		return
	}
	json.NewEncoder(w).Encode(current)
}

// slowMetrics is a scrape target that answers only after SLOW_DELAY, so a
// collector that scrapes targets one at a time delays every target after it.
func slowMetrics(w http.ResponseWriter, r *http.Request) {
	delay, err := time.ParseDuration(os.Getenv("SLOW_DELAY"))
	if err != nil {
		delay = 8 * time.Second
	}
	select {
	case <-time.After(delay):
	case <-r.Context().Done():
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintln(w, "e2e_slow_sample 1")
}

// forward relays TCP from :8443 to FORWARD_TO. With soak.sh --kusto-via-aks
// the harness reaches Kusto through it; TLS stays end to end, so the caller
// still verifies the Kusto certificate.
func forward(target string) {
	ln, err := net.Listen("tcp", ":8443")
	if err != nil {
		log.Fatalf("forward: %v", err)
	}
	log.Printf("forwarding :8443 to %s", target)
	for {
		in, err := ln.Accept()
		if err != nil {
			log.Printf("forward accept: %v", err)
			continue
		}
		go func() {
			defer in.Close()
			out, err := net.DialTimeout("tcp", target, 10*time.Second)
			if err != nil {
				log.Printf("forward dial: %v", err)
				return
			}
			defer out.Close()
			go io.Copy(out, in)
			io.Copy(in, out)
		}()
	}
}

func envInt(name string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil {
		return v
	}
	return fallback
}

// load sends LOAD_TABLES metric names x LOAD_SERIES series by remote write and
// LOAD_LOG_TABLES x LOAD_LOGS records by OTLP every LOAD_INTERVAL, so WAL
// rotation, peer transfers and uploads run at a realistic table count.
func load() {
	tables, series := envInt("LOAD_TABLES", 0), envInt("LOAD_SERIES", 20)
	logTables, logs := envInt("LOAD_LOG_TABLES", 0), envInt("LOAD_LOGS", 50)
	interval, err := time.ParseDuration(os.Getenv("LOAD_INTERVAL"))
	if err != nil {
		interval = time.Second
	}
	if tables == 0 && logTables == 0 {
		return
	}
	host, _ := os.Hostname()
	base := "http://" + os.Getenv("HOST_IP") + ":3100"
	var all [][]label
	for t := 0; t < tables; t++ {
		for i := 0; i < series; i++ {
			all = append(all, []label{{"__name__", fmt.Sprintf("e2e_load_%03d", t)}, {"pod", host}, {"series", strconv.Itoa(i)}})
		}
	}
	const perRequest = 2000
	log.Printf("load: %d metric series, %d log tables x %d records every %s", len(all), logTables, logs, interval)
	for range time.Tick(interval) {
		now := time.Now().UnixMilli()
		for start := 0; start < len(all); start += perRequest {
			end := min(start+perRequest, len(all))
			if err := remoteWrite(base+"/receive", writeRequest(all[start:end], float64(now%1000), now)); err != nil {
				log.Printf("load remote write: %s", errorClass(err))
			}
		}
		for t := 0; t < logTables; t++ {
			records := make([]*logdata.LogRecord, logs)
			for i := range records {
				records[i] = &logdata.LogRecord{TimeUnixNano: uint64(now) * 1e6, ObservedTimeUnixNano: uint64(now) * 1e6,
					Body:       &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: strings.Repeat("load ", 40)}},
					Attributes: []*commonpb.KeyValue{kv("kusto.database", "Logs"), kv("kusto.table", fmt.Sprintf("E2eLoadLogs%02d", t)), kv("pod", host)}}
			}
			req := &logspb.ExportLogsServiceRequest{ResourceLogs: []*logdata.ResourceLogs{{ScopeLogs: []*logdata.ScopeLogs{{LogRecords: records}}}}}
			if err := postBody(base+"/v1/logs", req, false); err != nil {
				log.Printf("load logs: %s", errorClass(err))
			}
		}
	}
}
