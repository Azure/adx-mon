package transform

import (
	"bytes"
	"encoding/csv"
	"sort"
	"strings"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/pquerna/ffjson/ffjson"
	fflib "github.com/pquerna/ffjson/fflib/v1"
)

type NativeLogsCSVWriter struct {
	w   *bytes.Buffer
	buf *strings.Builder
	enc *csv.Writer

	labelsBuf   *bytes.Buffer
	seriesIdBuf *bytes.Buffer
	line        []byte
	columns     [][]byte
	fields      []string
}

// NewCSVNativeLogsCSVWriter returns a new CSVWriter that writes to the given buffer.  The columns, if specified, are
// label keys that will be promoted to columns.
func NewCSVNativeLogsCSVWriter(w *bytes.Buffer, columns []string) *NativeLogsCSVWriter {
	writer := &NativeLogsCSVWriter{
		w:           w,
		buf:         &strings.Builder{},
		seriesIdBuf: bytes.NewBuffer(make([]byte, 0, 1024)),
		labelsBuf:   bytes.NewBuffer(make([]byte, 0, 1024)),
		enc:         csv.NewWriter(w),
		line:        make([]byte, 0, 4096),
		columns:     make([][]byte, 0, len(columns)),
		fields:      make([]string, 0, 4+len(columns)),
	}

	writer.InitColumns(columns)
	return writer
}

func otlpTSToUTC(ts int64) string {
	// check for nanosecond precision
	if ts&0x1fffffffffffff == ts {
		return time.Unix(ts/1000, (ts%1000)*int64(time.Millisecond)).UTC().Format(time.RFC3339Nano)
	}
	return time.Unix(0, ts).UTC().Format(time.RFC3339Nano)
}

func (w *NativeLogsCSVWriter) MarshalNativeLog(log *types.Log) error {
	// There are 9 fields defined in an OTLP log schema
	fields := make([]string, 0, 9)
	// Convert log records to CSV
	// see samples at https://opentelemetry.io/docs/specs/otel/protocol/file-exporter/#examples
	// Reset fields
	fields = fields[:0]
	// Timestamp
	fields = append(fields, otlpTSToUTC(int64(log.Timestamp)))
	// ObservedTimestamp
	if v := log.ObservedTimestamp; v > 0 {
		// Some clients don't set this value.
		fields = append(fields, otlpTSToUTC(int64(log.ObservedTimestamp)))
	} else {
		fields = append(fields, time.Now().UTC().Format(time.RFC3339Nano))
	}
	// TraceId - we don't have this
	fields = append(fields, "")
	// SpanId - we don't have this
	fields = append(fields, "")
	// SeverityText - we don't have this
	fields = append(fields, "")
	// SeverityNumber - we don't have this
	fields = append(fields, "")
	// Body
	buf := w.buf
	buf.Reset()
	buf.WriteByte('{')
	hasPrevField := false
	for k, v := range log.Body {
		val, err := ffjson.Marshal(v)
		if err != nil {
			continue
		}

		if hasPrevField {
			buf.WriteByte(',')
		} else {
			hasPrevField = true
		}
		fflib.WriteJson(buf, []byte(k))
		buf.WriteByte(':')
		buf.Write(val) // Already marshalled into json. Don't escape it again.
		ffjson.Pool(val)
	}
	buf.WriteByte('}')
	fields = append(fields, buf.String())

	// Resource
	buf.Reset()
	buf.WriteByte('{')
	hasPrevField = false
	for k, v := range log.Resource {
		val, err := ffjson.Marshal(v)
		if err != nil {
			continue
		}

		if hasPrevField {
			buf.WriteByte(',')
		} else {
			hasPrevField = true
		}
		fflib.WriteJson(buf, []byte(k))
		buf.WriteByte(':')
		buf.Write(val) // Already marshalled into json. Don't escape it again.
		ffjson.Pool(val)
	}
	buf.WriteByte('}')
	fields = append(fields, buf.String())

	// Attributes
	buf.Reset()
	buf.WriteByte('{')
	hasPrevField = false
	for k, v := range log.Attributes {
		if strings.HasPrefix(k, "adxmon_") {
			continue
		}

		val, err := ffjson.Marshal(v)
		if err != nil {
			continue
		}

		if hasPrevField {
			buf.WriteByte(',')
		} else {
			hasPrevField = true
		}
		fflib.WriteJson(buf, []byte(k))
		buf.WriteByte(':')
		buf.Write(val) // Already marshalled into json. Don't escape it again.
		ffjson.Pool(val)
	}
	buf.WriteByte('}')
	fields = append(fields, buf.String())
	// Serialize
	if err := w.enc.Write(fields); err != nil {
		return err
	}

	w.enc.Flush()
	return w.enc.Error()
}

func (w *NativeLogsCSVWriter) Reset() {
	w.w.Reset()
	w.buf.Reset()
}

func (w *NativeLogsCSVWriter) Bytes() []byte {
	return w.w.Bytes()
}

// InitColumns initializes the labels that will be promoted to columns in the CSV file.  This can be done
// once on the *Writer and subsequent calls are no-ops.
func (w *NativeLogsCSVWriter) InitColumns(columns []string) {
	if len(w.columns) > 0 {
		return
	}

	sortLower := make([][]byte, len(columns))
	for i, v := range columns {
		sortLower[i] = []byte(strings.ToLower(v))
	}
	sort.Slice(sortLower, func(i, j int) bool {
		return bytes.Compare(sortLower[i], sortLower[j]) < 0
	})
	w.columns = sortLower
}
