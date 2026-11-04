package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	logspb "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	"github.com/golang/snappy"
	"google.golang.org/protobuf/encoding/protowire"
)

// fields decodes one protobuf message into field number -> raw values.
func fields(t *testing.T, b []byte) map[protowire.Number][][]byte {
	t.Helper()
	out := map[protowire.Number][][]byte{}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			t.Fatalf("bad tag: %v", protowire.ParseError(n))
		}
		b = b[n:]
		var v []byte
		switch typ {
		case protowire.BytesType:
			v, n = protowire.ConsumeBytes(b)
		case protowire.Fixed64Type:
			n = 8
			v = b[:8]
		case protowire.VarintType:
			_, n = protowire.ConsumeVarint(b)
			v = b[:n]
		default:
			t.Fatalf("unexpected wire type %v", typ)
		}
		if n < 0 {
			t.Fatalf("bad value: %v", protowire.ParseError(n))
		}
		out[num] = append(out[num], v)
		b = b[n:]
	}
	return out
}

func TestWriteRequestEncodesRemoteWriteSeries(t *testing.T) {
	b := writeRequest([][]label{{{"__name__", "m"}, {"seq", "7"}}}, 2.5, 1234)
	series := fields(t, b)[1]
	if len(series) != 1 {
		t.Fatalf("want 1 series, got %d", len(series))
	}
	ts := fields(t, series[0])
	if len(ts[1]) != 2 {
		t.Fatalf("want 2 labels, got %d", len(ts[1]))
	}
	l := fields(t, ts[1][1])
	if string(l[1][0]) != "seq" || string(l[2][0]) != "7" {
		t.Fatalf("label = %q=%q", l[1][0], l[2][0])
	}
	sample := fields(t, ts[2][0])
	if v := math.Float64frombits(binary.LittleEndian.Uint64(sample[1][0])); v != 2.5 {
		t.Fatalf("value = %v", v)
	}
	if ms, _ := protowire.ConsumeVarint(sample[2][0]); ms != 1234 {
		t.Fatalf("timestamp = %v", ms)
	}
}

func TestChunkedPostUsesChunkedTransferEncoding(t *testing.T) {
	var got []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.TransferEncoding
		io.Copy(io.Discard, r.Body)
	}))
	defer s.Close()
	if err := postBody(s.URL, &logspb.ExportLogsServiceRequest{}, true); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "chunked" {
		t.Fatalf("transfer encoding = %v", got)
	}
	if err := postBody(s.URL, &logspb.ExportLogsServiceRequest{}, false); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("unchunked transfer encoding = %v", got)
	}
}

// Every emitted seq is either delivered or listed as failed for each path.
func TestStreamAccountsForEveryFailedSend(t *testing.T) {
	var mu sync.Mutex
	received := map[string]int{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		received[r.URL.Path]++
		if r.URL.Path == "/receive" {
			if _, err := snappy.Decode(nil, body); err != nil {
				t.Errorf("remote write body is not snappy: %v", err)
			}
		}
		if r.URL.Path == "/v1/otlpmetrics" {
			http.Error(w, "rejected 42", http.StatusTooManyRequests)
		}
	}))
	defer s.Close()
	// sendSignal targets $HOST_IP:3100; dial the test server instead.
	t.Setenv("HOST_IP", "collector.invalid")
	addr := strings.TrimPrefix(s.URL, "http://")
	client = &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}}}
	defer func() { client = &http.Client{Timeout: 5 * time.Second} }()

	st := &streamStatus{RunID: "r", Failed: map[string][]int{}, Errors: map[string]int{}}
	runStream(st, 20, 300*time.Millisecond)
	if !st.Done || st.Emitted < 3 {
		t.Fatalf("stream did not finish: %+v", st)
	}
	if len(st.Failed["otlp_metrics"]) != st.Emitted {
		t.Fatalf("otlp_metrics failures %v for %d emitted", st.Failed["otlp_metrics"], st.Emitted)
	}
	for _, path := range []string{"otlp_logs", "queued_control", "remote_write"} {
		if len(st.Failed[path]) != 0 {
			t.Fatalf("%s failed: %v", path, st.Failed[path])
		}
	}
	if st.Errors["otlp_metrics: ### Too Many Requests: rejected ##"] != st.Emitted {
		t.Fatalf("error classes = %v", st.Errors)
	}
	mu.Lock()
	defer mu.Unlock()
	if received["/v1/logs"] != 2*st.Emitted || received["/receive"] != st.Emitted {
		t.Fatalf("received = %v for %d emitted", received, st.Emitted)
	}
	b, _ := json.Marshal(st)
	if !strings.Contains(string(b), `"failed"`) {
		t.Fatalf("status JSON = %s", b)
	}
}

func TestSlowMetricsAnswersAfterDelay(t *testing.T) {
	t.Setenv("SLOW_DELAY", "50ms")
	start := time.Now()
	w := httptest.NewRecorder()
	slowMetrics(w, httptest.NewRequest("GET", "/slow-metrics", nil))
	if time.Since(start) < 50*time.Millisecond || !strings.Contains(w.Body.String(), "e2e_slow_sample") {
		t.Fatalf("slow target answered %q after %s", w.Body.String(), time.Since(start))
	}
}
