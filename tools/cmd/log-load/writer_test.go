package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	"google.golang.org/protobuf/proto"
)

type testOutput struct {
	bytes.Buffer
	writeFunc func([]byte) (int, error)
	closeErr  error
	closed    bool
}

func (o *testOutput) Write(p []byte) (int, error) {
	if o.writeFunc != nil {
		return o.writeFunc(p)
	}
	return o.Buffer.Write(p)
}

func (o *testOutput) Close() error {
	o.closed = true
	return o.closeErr
}

func TestFileWriterPreservesBodyAndFlushesEveryBatch(t *testing.T) {
	batch, err := testGenerator(1).Read()
	if err != nil {
		t.Fatal(err)
	}
	body := batch.ResourceLogs[0].ScopeLogs[0].LogRecords[0].Body.GetKvlistValue()
	body.Values = append(body.Values,
		stringValue("escaped", "quote\" slash\\ line\n雪"),
		&commonv1.KeyValue{Key: "nested", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_KvlistValue{
			KvlistValue: &commonv1.KeyValueList{Values: []*commonv1.KeyValue{stringValue("key", "value")}},
		}}},
		&commonv1.KeyValue{Key: "array", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_ArrayValue{
			ArrayValue: &commonv1.ArrayValue{Values: []*commonv1.AnyValue{
				{Value: &commonv1.AnyValue_IntValue{IntValue: 42}},
				{Value: &commonv1.AnyValue_BoolValue{BoolValue: true}},
				{Value: &commonv1.AnyValue_DoubleValue{DoubleValue: 1.25}},
				{Value: &commonv1.AnyValue_BytesValue{BytesValue: []byte{1, 2, 3}}},
				{},
			}},
		}}},
	)
	output := &testOutput{}
	writer := newFileWriter(output)
	result, err := writer.WriteBatch(context.Background(), batch)
	if err != nil {
		t.Fatal(err)
	}
	if result.records != 1 || result.bytes != int64(output.Len()) || output.Len() == 0 {
		t.Fatalf("batch not flushed: %+v bytes=%d", result, output.Len())
	}
	if bytes.Count(output.Bytes(), []byte{'\n'}) != 1 {
		t.Fatalf("expected one physical JSON line: %s", output.Bytes())
	}
	var got map[string]any
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"message": "something worth logging", "kusto.database": "DB", "kusto.table": "Logs",
		"escaped": "quote\" slash\\ line\n雪", "nested": map[string]any{"key": "value"},
		"array": []any{float64(42), true, 1.25, "AQID", nil},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wrong JSON body: got %#v, want %#v", got, want)
	}
	closeResult, err := writer.Close()
	if err != nil || closeResult != (writeResult{}) || !output.closed {
		t.Fatalf("close: result=%+v err=%v closed=%v", closeResult, err, output.closed)
	}
}

func TestFileWriterPartialFlushFailure(t *testing.T) {
	flushErr, closeErr := errors.New("disk full"), errors.New("close failure")
	output := &testOutput{closeErr: closeErr}
	output.writeFunc = func(p []byte) (int, error) {
		// One full record plus a partial next record reached the file.
		n := bytes.IndexByte(p, '\n') + 4
		if n < 4 || n > len(p) {
			t.Fatalf("unexpected flush buffer of %d bytes", len(p))
		}
		_, _ = output.Buffer.Write(p[:n])
		return n, flushErr
	}
	writer := newFileWriter(output)
	batch, err := testGenerator(4).Read()
	if err != nil {
		t.Fatal(err)
	}
	result, err := writer.WriteBatch(context.Background(), batch)
	if !errors.Is(err, flushErr) || result.records != 1 || result.bytes != int64(output.Len()) {
		t.Fatalf("partial flush not reported: result=%+v err=%v", result, err)
	}
	closeResult, err := writer.Close()
	if !errors.Is(err, flushErr) || !errors.Is(err, closeErr) || closeResult != (writeResult{}) || !output.closed {
		t.Fatalf("close did not retain errors or duplicated output: result=%+v err=%v", closeResult, err)
	}
}

func TestFileWriterWriteFailureCancelsPipeline(t *testing.T) {
	writeErr := errors.New("write failure")
	output := &testOutput{writeFunc: func([]byte) (int, error) { return 0, writeErr }}
	writer := newFileWriter(output)
	gen := testGenerator(0)
	gen.batchSize = 2500 // Force an automatic buffer flush during JSON encoding.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runPipeline(ctx, gen, []batchWriter{writer}, &stats{}, false) }()
	if err := awaitResult(t, done); !errors.Is(err, writeErr) || !strings.Contains(err.Error(), "write JSON line") {
		t.Fatalf("expected write failure: %v", err)
	}
	if !output.closed {
		t.Fatal("output was not closed on write failure")
	}
}

func TestFileWriterCloseFlushesAfterEncodingError(t *testing.T) {
	batch, err := testGenerator(2).Read()
	if err != nil {
		t.Fatal(err)
	}
	// The first valid record is buffered when the second record fails validation.
	batch.ResourceLogs[0].ScopeLogs[0].LogRecords[1].Body = stringValue("unused", "not an object").Value
	output := &testOutput{}
	writer := newFileWriter(output)
	result, err := writer.WriteBatch(context.Background(), batch)
	if err == nil || result != (writeResult{}) || output.Len() != 0 {
		t.Fatalf("unexpected write result=%+v err=%v", result, err)
	}
	result, err = writer.Close()
	if err != nil || result.records != 1 || result.bytes != int64(output.Len()) || !output.closed {
		t.Fatalf("close did not flush valid buffered record: result=%+v err=%v", result, err)
	}
}

func TestFileCancellationFinishesAndFlushesInflightBatch(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	output := &testOutput{}
	output.writeFunc = func(p []byte) (int, error) {
		close(started)
		<-release
		return output.Buffer.Write(p)
	}
	writer := newFileWriter(output)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &stats{}
	done := make(chan error, 1)
	go func() { done <- runPipeline(ctx, testGenerator(0), []batchWriter{writer}, s, false) }()
	awaitStarted(t, started)
	cancel()
	close(release)
	if err := awaitResult(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation: %v", err)
	}
	if !output.closed || s.written.Load() != 4 || s.bytes.Load() != int64(output.Len()) || bytes.Count(output.Bytes(), []byte{'\n'}) != 4 {
		t.Fatalf("in-flight batch not flushed and closed: closed=%v records=%d bytes=%d", output.closed, s.written.Load(), s.bytes.Load())
	}
}

func TestFileSetupErrors(t *testing.T) {
	t.Run("directory is a file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := openWriters(destination{directory: path}, 1); err == nil {
			t.Fatal("expected directory creation error")
		}
	})
	t.Run("writer open fails after first file", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.Mkdir(writerPath(directory, 1), 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := openWriters(destination{directory: directory}, 3); err == nil {
			t.Fatal("expected file open error")
		}
		if info, err := os.Stat(writerPath(directory, 0)); err != nil || info.Size() != 0 {
			t.Fatalf("generation started before all files were open: info=%v err=%v", info, err)
		}
		if _, err := os.Stat(writerPath(directory, 2)); !os.IsNotExist(err) {
			t.Fatalf("setup continued after error: %v", err)
		}
	})
	t.Run("hard-linked writer files", func(t *testing.T) {
		directory := t.TempDir()
		first, second := writerPath(directory, 0), writerPath(directory, 1)
		if err := os.WriteFile(first, []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(first, second); err != nil {
			t.Skipf("hard links unavailable: %v", err)
		}
		if _, err := openWriters(destination{directory: directory}, 2); err == nil || !strings.Contains(err.Error(), "refer to the same file") {
			t.Fatalf("expected duplicate file rejection: %v", err)
		}
		content, err := os.ReadFile(first)
		if err != nil || string(content) != "preserve" {
			t.Fatalf("writer files changed during setup: %q, err=%v", content, err)
		}
	})
	for _, targetType := range []string{"regular", "directory", "missing"} {
		t.Run("symlink to "+targetType, func(t *testing.T) {
			directory := t.TempDir()
			target := filepath.Join(directory, "link-target")
			switch targetType {
			case "regular":
				if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, writerPath(directory, 0)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if _, err := openWriters(destination{directory: directory}, 1); err == nil || !strings.Contains(err.Error(), "regular file") {
				t.Fatalf("expected symlink rejection: %v", err)
			}
			if targetType == "regular" {
				content, err := os.ReadFile(target)
				if err != nil || string(content) != "untouched" {
					t.Fatalf("symlink target changed: %q, err=%v", content, err)
				}
			} else if targetType == "missing" {
				if _, err := os.Stat(target); !os.IsNotExist(err) {
					t.Fatalf("dangling symlink target was created: %v", err)
				}
			}
		})
	}
}

func TestHTTPOutputRegression(t *testing.T) {
	for _, useTLS := range []bool{false, true} {
		for _, base := range []struct {
			name, suffix, decodedPath, requestURI string
		}{
			{"literal path", "/prefix/?key=value", "/prefix/v1/logs", "/prefix/v1/logs?key=value"},
			{"trailing escaped slash", "/prefix%2F?key=value", "/prefix//v1/logs", "/prefix%2F/v1/logs?key=value"},
			{"escaped and literal slashes", "/prefix%2f//?key=value", "/prefix//v1/logs", "/prefix%2f/v1/logs?key=value"},
		} {
			t.Run(map[bool]string{false: "HTTP", true: "HTTPS"}[useTLS]+"/"+base.name, func(t *testing.T) {
				var receivedRecords, receivedBytes atomic.Int64
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodPost || r.URL.Path != base.decodedPath || r.RequestURI != base.requestURI || r.URL.RawQuery != "key=value" || r.Header.Get("Content-Type") != "application/x-protobuf" {
						t.Errorf("unexpected request: %s %s uri=%q type=%q", r.Method, r.URL, r.RequestURI, r.Header.Get("Content-Type"))
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					payload, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					var batch v1.ExportLogsServiceRequest
					if err := proto.Unmarshal(payload, &batch); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if batch.ResourceLogs[0].Resource.Attributes[0].GetValue().GetStringValue() != "hostname" || batch.ResourceLogs[0].ScopeLogs[0].Scope.Name != "name" {
						t.Error("OTLP resource or scope changed")
					}
					for _, record := range batch.ResourceLogs[0].ScopeLogs[0].LogRecords {
						body := jsonValue(record.Body).(map[string]any)
						if record.TimeUnixNano == 0 || record.SeverityNumber != 17 || record.SeverityText != "Error" || body["kusto.database"] != "DB" || body["kusto.table"] != "Logs" {
							t.Errorf("unexpected OTLP record: %v", record)
						}
					}
					receivedRecords.Add(recordCount(&batch))
					receivedBytes.Add(int64(len(payload)))
					w.WriteHeader(http.StatusOK)
				})
				server := httptest.NewUnstartedServer(handler)
				if useTLS {
					server.StartTLS()
				} else {
					server.Start()
				}
				defer server.Close()
				target, err := parseTarget(server.URL + base.suffix)
				if err != nil {
					t.Fatal(err)
				}
				writers := []batchWriter{
					&httpWriter{endpoint: target.endpoint, client: server.Client()},
					&httpWriter{endpoint: target.endpoint, client: server.Client()},
				}
				s := &stats{}
				if err := runPipeline(context.Background(), testGenerator(11), writers, s, false); err != nil {
					t.Fatal(err)
				}
				if receivedRecords.Load() != 11 || s.written.Load() != 11 || s.generated.Load() != 11 || s.bytes.Load() != receivedBytes.Load() || s.bytes.Load() == 0 {
					t.Fatalf("HTTP stats do not match server: generated=%d written=%d bytes=%d received=%d bytes=%d",
						s.generated.Load(), s.written.Load(), s.bytes.Load(), receivedRecords.Load(), receivedBytes.Load())
				}
			})
		}
	}
}

func TestHTTPResponseAccounting(t *testing.T) {
	for _, tc := range []struct {
		name             string
		total            int64
		partial          *v1.ExportLogsPartialSuccess
		rawResponse      []byte
		truncated        bool
		written          int64
		wantPayloadBytes bool
		wantError        string
	}{
		{name: "full success", total: 4, written: 4, wantPayloadBytes: true},
		{name: "empty partial success", total: 4, partial: &v1.ExportLogsPartialSuccess{}, written: 4, wantPayloadBytes: true},
		{
			name: "warning without rejection", total: 4,
			partial: &v1.ExportLogsPartialSuccess{ErrorMessage: "warning only"},
			written: 4, wantPayloadBytes: true,
		},
		{
			name: "some records rejected", total: 4,
			partial: &v1.ExportLogsPartialSuccess{RejectedLogRecords: 2, ErrorMessage: "table quota"},
			written: 2, wantPayloadBytes: true,
			wantError: `OTLP partial success: rejected 2 of 4 log records: "table quota"`,
		},
		{
			name: "single record rejected", total: 1,
			partial:          &v1.ExportLogsPartialSuccess{RejectedLogRecords: 1, ErrorMessage: "table quota"},
			wantPayloadBytes: true,
			wantError:        `OTLP partial success: rejected 1 of 1 log records: "table quota"`,
		},
		{
			name: "rejection without message", total: 4,
			partial: &v1.ExportLogsPartialSuccess{RejectedLogRecords: 1},
			written: 3, wantPayloadBytes: true, wantError: "rejected 1 of 4 log records",
		},
		{
			name: "negative rejection count", total: 4,
			partial:   &v1.ExportLogsPartialSuccess{RejectedLogRecords: -1},
			wantError: "invalid OTLP rejected log record count -1",
		},
		{
			name: "excessive rejection count", total: 4,
			partial:   &v1.ExportLogsPartialSuccess{RejectedLogRecords: 5},
			wantError: "invalid OTLP rejected log record count 5",
		},
		{name: "malformed response", total: 4, rawResponse: []byte{0xff}, wantError: "decode OTLP response"},
		{
			name: "oversized response", total: 4,
			partial:   &v1.ExportLogsPartialSuccess{RejectedLogRecords: 1, ErrorMessage: strings.Repeat("x", 64*1024+1)},
			wantError: "OTLP response exceeds",
		},
		{
			name: "incomplete response", total: 4,
			partial: &v1.ExportLogsPartialSuccess{RejectedLogRecords: 1}, truncated: true,
			wantError: "read OTLP response",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := tc.rawResponse
			if wire == nil {
				var err error
				wire, err = proto.Marshal(&v1.ExportLogsServiceResponse{PartialSuccess: tc.partial})
				if err != nil {
					t.Fatal(err)
				}
			}
			var requests, requestBytes atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				payload, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				requests.Add(1)
				requestBytes.Add(int64(len(payload)))
				w.Header().Set("Content-Type", "application/x-protobuf")
				if tc.truncated {
					w.Header().Set("Content-Length", strconv.Itoa(len(wire)+1))
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(wire)
			}))
			defer server.Close()
			var output bytes.Buffer
			err := runCLI(context.Background(), []string{
				"-target", server.URL, "-concurrency", "1",
				"-batch-size", strconv.FormatInt(tc.total, 10),
				"-total-samples", strconv.FormatInt(tc.total, 10),
			}, &output, io.Discard)
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected error containing %q, got %v", tc.wantError, err)
			}
			if requests.Load() != 1 {
				t.Fatalf("expected one request without retries, got %d", requests.Load())
			}
			var wantBytes int64
			if tc.wantPayloadBytes {
				wantBytes = requestBytes.Load()
			}
			for _, line := range []string{
				"Generated records: " + strconv.FormatInt(tc.total, 10) + "\n",
				"Written records: " + strconv.FormatInt(tc.written, 10) + "\n",
				"Written bytes: " + strconv.FormatInt(wantBytes, 10) + "\n",
			} {
				if !strings.Contains(output.String(), line) {
					t.Fatalf("missing %q in stats:\n%s", line, &output)
				}
			}
		})
	}
}

func TestHTTPFailuresAndDryRun(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusTooManyRequests, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		opts := validOptions("")
		opts.target = server.URL
		var output bytes.Buffer
		err := run(context.Background(), opts, &output, io.Discard)
		server.Close()
		if err == nil || !strings.Contains(err.Error(), "OTLP response:") || !strings.Contains(output.String(), "Written records: 0\nWritten bytes: 0\n") {
			t.Fatalf("HTTP error was not surfaced or counted as success: err=%v\n%s", err, &output)
		}
	}
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1) }))
	defer server.Close()
	opts := validOptions("")
	opts.target, opts.dryRun = server.URL, true
	if err := run(context.Background(), opts, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("dry-run made HTTP requests")
	}
}

func TestHTTPRedirectIsNotFollowed(t *testing.T) {
	var destinationRequests atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		destinationRequests.Add(1)
	}))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer redirect.Close()

	opts := validOptions("")
	opts.target, opts.concurrency, opts.totalSamples = redirect.URL, 1, 1
	var output bytes.Buffer
	err := run(context.Background(), opts, &output, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "OTLP response: 302 Found") {
		t.Fatalf("expected redirect response error, got %v", err)
	}
	if destinationRequests.Load() != 0 {
		t.Fatalf("redirect was followed to destination %d times", destinationRequests.Load())
	}
	if !strings.Contains(output.String(), "Written records: 0\nWritten bytes: 0\n") {
		t.Fatalf("redirect was counted as successful output:\n%s", &output)
	}
}

func TestHTTPCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Consume the request so the server can notice the canceled connection.
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opts := validOptions("")
	opts.target, opts.concurrency, opts.totalSamples = server.URL, 1, 1
	done := make(chan error, 1)
	go func() { done <- run(ctx, opts, io.Discard, io.Discard) }()
	awaitStarted(t, started)
	cancel()
	if err := awaitResult(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation: %v", err)
	}
}
