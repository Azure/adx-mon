package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	"google.golang.org/protobuf/proto"
)

type writeResult struct {
	records int64
	bytes   int64
}

type batchWriter interface {
	WriteBatch(context.Context, *v1.ExportLogsServiceRequest) (writeResult, error)
	Close() (writeResult, error)
}

func openWriters(target destination, concurrency int) ([]batchWriter, error) {
	writers := make([]batchWriter, 0, concurrency)
	fileInfos := make([]os.FileInfo, 0, concurrency)
	closeOpenedWriters := func(setupErr error) error {
		for j, writer := range writers {
			_, closeErr := writer.Close()
			if closeErr != nil {
				setupErr = errors.Join(setupErr, fmt.Errorf("close writer %d: %w", j, closeErr))
			}
		}
		return setupErr
	}
	if target.directory != "" {
		if err := os.MkdirAll(target.directory, 0755); err != nil {
			return nil, fmt.Errorf("create output directory: %w", err)
		}
	}
	for i := range concurrency {
		if target.endpoint != "" {
			writers = append(writers, &httpWriter{endpoint: target.endpoint, client: &http.Client{
				Timeout: 30 * time.Second,
				CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}})
			continue
		}
		path := writerPath(target.directory, i)
		file, err := openWriterFile(path)
		if err != nil {
			return nil, closeOpenedWriters(fmt.Errorf("open %s: %w", path, err))
		}
		info, err := file.Stat()
		if err != nil {
			closeErr := file.Close()
			setupErr := errors.Join(fmt.Errorf("stat %s: %w", path, err), closeErr)
			return nil, closeOpenedWriters(setupErr)
		}
		for j, existing := range fileInfos {
			if os.SameFile(existing, info) {
				closeErr := file.Close()
				setupErr := errors.Join(fmt.Errorf("%s and %s refer to the same file", writerPath(target.directory, j), path), closeErr)
				return nil, closeOpenedWriters(setupErr)
			}
		}
		fileInfos = append(fileInfos, info)
		writers = append(writers, newFileWriter(file))
	}
	return writers, nil
}

func openWriterFile(path string) (*os.File, error) {
	// Do not follow symlinks, including dangling links or links to FIFOs. Apart
	// from blocking on a FIFO, aliases could give two writers the same file.
	info, err := os.Lstat(path)
	if err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("writer destination must be a regular file, not %s (symlinks are not supported)", info.Mode().Type())
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// On Unix, nonblocking/no-follow flags also protect against replacement by
	// a FIFO or symlink between Lstat and OpenFile. Verify the opened descriptor
	// before handing it to a writer, even if opening a special file succeeded.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND|writerOpenFlags, 0644)
	if err != nil {
		return nil, err
	}
	info, err = file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("opened writer destination is not a regular file: %s", info.Mode().Type())
	}
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

// countingOutput measures bytes actually accepted by the file, including partial
// writes on failure. A complete newline marks a successfully written record.
type countingOutput struct {
	output io.WriteCloser
	writeResult
}

func (w *countingOutput) Write(p []byte) (int, error) {
	n, err := w.output.Write(p)
	w.bytes += int64(n)
	w.records += int64(bytes.Count(p[:n], []byte{'\n'}))
	return n, err
}

type fileWriter struct {
	output *countingOutput
	buffer *bufio.Writer
}

func newFileWriter(output io.WriteCloser) *fileWriter {
	counter := &countingOutput{output: output}
	return &fileWriter{output: counter, buffer: bufio.NewWriterSize(counter, 64*1024)}
}

func (w *fileWriter) WriteBatch(_ context.Context, batch *v1.ExportLogsServiceRequest) (result writeResult, err error) {
	before := w.output.writeResult
	defer func() { result = w.delta(before) }()
	encoder := json.NewEncoder(w.buffer)
	for _, resource := range batch.GetResourceLogs() {
		for _, scope := range resource.GetScopeLogs() {
			for _, record := range scope.GetLogRecords() {
				body, ok := jsonValue(record.GetBody()).(map[string]any)
				if !ok {
					return result, fmt.Errorf("file record body must be an object")
				}
				if err := encoder.Encode(body); err != nil {
					return result, fmt.Errorf("write JSON line: %w", err)
				}
			}
		}
	}
	if err := w.buffer.Flush(); err != nil {
		return result, fmt.Errorf("flush file batch: %w", err)
	}
	return result, nil
}

func (w *fileWriter) delta(before writeResult) writeResult {
	return writeResult{records: w.output.records - before.records, bytes: w.output.bytes - before.bytes}
}

func (w *fileWriter) Close() (writeResult, error) {
	before := w.output.writeResult
	flushErr := w.buffer.Flush()
	closeErr := w.output.output.Close()
	if flushErr != nil {
		flushErr = fmt.Errorf("flush on close: %w", flushErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close file: %w", closeErr)
	}
	return w.delta(before), errors.Join(flushErr, closeErr)
}

// jsonValue encodes the application body, not the OTLP/protobuf JSON envelope.
// Keep it separate from generation so a future CRI wrapper can reuse the body.
func jsonValue(value *commonv1.AnyValue) any {
	switch v := value.GetValue().(type) {
	case *commonv1.AnyValue_StringValue:
		return v.StringValue
	case *commonv1.AnyValue_BoolValue:
		return v.BoolValue
	case *commonv1.AnyValue_IntValue:
		return v.IntValue
	case *commonv1.AnyValue_DoubleValue:
		return v.DoubleValue
	case *commonv1.AnyValue_BytesValue:
		return v.BytesValue
	case *commonv1.AnyValue_ArrayValue:
		result := make([]any, 0, len(v.ArrayValue.GetValues()))
		for _, item := range v.ArrayValue.GetValues() {
			result = append(result, jsonValue(item))
		}
		return result
	case *commonv1.AnyValue_KvlistValue:
		result := make(map[string]any, len(v.KvlistValue.GetValues()))
		for _, entry := range v.KvlistValue.GetValues() {
			result[entry.GetKey()] = jsonValue(entry.GetValue())
		}
		return result
	default:
		return nil
	}
}

const maxOTLPResponseBytes = 64 * 1024

type httpWriter struct {
	endpoint string
	client   *http.Client
}

func (w *httpWriter) WriteBatch(ctx context.Context, batch *v1.ExportLogsServiceRequest) (writeResult, error) {
	payload, err := proto.Marshal(batch)
	if err != nil {
		return writeResult{}, fmt.Errorf("marshal OTLP: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.endpoint, bytes.NewReader(payload))
	if err != nil {
		return writeResult{}, fmt.Errorf("create OTLP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	resp, err := w.client.Do(req)
	if err != nil {
		return writeResult{}, fmt.Errorf("send OTLP: %w", err)
	}
	// Read the complete acknowledgment before counting records: a 2xx response
	// can still report rejected records. Read one extra byte to detect an
	// oversized response instead of silently decoding a truncated prefix.
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxOTLPResponseBytes+1))
	closeErr := resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return writeResult{}, errors.Join(fmt.Errorf("OTLP response: %s", resp.Status), readErr, closeErr)
	}
	if readErr != nil {
		return writeResult{}, errors.Join(fmt.Errorf("read OTLP response: %w", readErr), closeErr)
	}
	if len(body) > maxOTLPResponseBytes {
		return writeResult{}, errors.Join(fmt.Errorf("OTLP response exceeds %d bytes", maxOTLPResponseBytes), closeErr)
	}
	var response v1.ExportLogsServiceResponse
	if err := proto.Unmarshal(body, &response); err != nil {
		return writeResult{}, errors.Join(fmt.Errorf("decode OTLP response: %w", err), closeErr)
	}
	total := recordCount(batch)
	partial := response.GetPartialSuccess()
	rejected := partial.GetRejectedLogRecords()
	if rejected < 0 || rejected > total {
		return writeResult{}, errors.Join(fmt.Errorf("invalid OTLP rejected log record count %d for %d records", rejected, total), closeErr)
	}
	// Payload bytes cannot be attributed to individual accepted/rejected records;
	// they count the complete request acknowledged by a valid response.
	result := writeResult{records: total - rejected, bytes: int64(len(payload))}
	if rejected > 0 {
		return result, errors.Join(fmt.Errorf("OTLP partial success: rejected %d of %d log records: %q", rejected, total, partial.GetErrorMessage()), closeErr)
	}
	return result, closeErr
}

func (w *httpWriter) Close() (writeResult, error) {
	w.client.CloseIdleConnections()
	return writeResult{}, nil
}
