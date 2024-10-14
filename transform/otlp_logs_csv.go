package transform

import (
	"bytes"
	"encoding/csv"
	"sort"
	"strconv"
	"strings"
	"time"

	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	"github.com/Azure/adx-mon/pkg/otlp"
	fflib "github.com/pquerna/ffjson/fflib/v1"
)

var (
	// Logs often contain unespcaped newlines, particularly at the end of a log line
	// but also in case of stacktraces.
	newlineReplacer = strings.NewReplacer("\r\n", "%0D%0A", "\n", "%0A")
)

type CSVWriter struct {
	w   *bytes.Buffer
	buf *strings.Builder
	enc *csv.Writer

	labelsBuf   *bytes.Buffer
	seriesIdBuf *bytes.Buffer
	line        []byte
	columns     [][]byte
	fields      []string
}

// NewCSVWriter returns a new CSVWriter that writes to the given buffer.  The columns, if specified, are
// label keys that will be promoted to columns.
func NewCSVWriter(w *bytes.Buffer, columns []string) *CSVWriter {
	writer := &CSVWriter{
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

func (w *CSVWriter) MarshalLog(logs *otlp.Logs) error {
	// See ingestor/storage/schema::NewLogsSchema
	// we're writing a ExportLogsServiceRequest as a CSV

	// There are 9 fields defined in an OTLP log schema
	fields := make([]string, 0, 9)
	// Convert log records to CSV
	// see samples at https://opentelemetry.io/docs/specs/otel/protocol/file-exporter/#examples
	for _, l := range logs.Logs {
		// Reset fields
		fields = fields[:0]
		// Timestamp
		fields = append(fields, otlpTSToUTC(int64(l.GetTimeUnixNano())))
		// ObservedTimestamp
		if v := l.GetObservedTimeUnixNano(); v > 0 {
			// Some clients don't set this value.
			fields = append(fields, otlpTSToUTC(int64(l.GetObservedTimeUnixNano())))
		} else {
			fields = append(fields, time.Now().UTC().Format(time.RFC3339Nano))
		}
		// TraceId
		fields = append(fields, string(l.GetTraceId()))
		// SpanId
		fields = append(fields, string(l.GetSpanId()))
		// SeverityText
		fields = append(fields, l.GetSeverityText())
		// SeverityNumber
		fields = append(fields, l.GetSeverityNumber().String())
		// Body
		buf := w.buf
		serializeAnyValue(buf, l.GetBody(), 0)
		fields = append(fields, buf.String())
		// Resource
		buf.Reset()
		buf.WriteByte('{')
		for _, r := range logs.Resources {
			if buf.String()[buf.Len()-1] != '{' {
				buf.WriteByte(',')
			}
			fflib.WriteJson(buf, []byte(r.GetKey()))
			buf.WriteByte(':')
			fflib.WriteJson(buf, []byte(r.GetValue().GetStringValue()))
		}
		buf.WriteByte('}')
		fields = append(fields, buf.String())
		// Attributes
		buf.Reset()
		buf.WriteByte('{')
		for _, a := range l.GetAttributes() {
			if buf.String()[buf.Len()-1] != '{' {
				buf.WriteByte(',')
			}
			fflib.WriteJson(buf, []byte(a.GetKey()))
			buf.WriteByte(':')
			fflib.WriteJson(buf, []byte(a.GetValue().GetStringValue()))
		}
		buf.WriteByte('}')
		fields = append(fields, buf.String())
		// Serialize
		if err := w.enc.Write(fields); err != nil {
			return err
		}

		w.enc.Flush()
	}

	return w.enc.Error()
}

func (w *CSVWriter) Reset() {
	w.w.Reset()
	w.buf.Reset()
}

func (w *CSVWriter) Bytes() []byte {
	return w.w.Bytes()
}

// InitColumns initializes the labels that will be promoted to columns in the CSV file.  This can be done
// once on the *Writer and subsequent calls are no-ops.
func (w *CSVWriter) InitColumns(columns []string) {
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

const maxNestedDepth = 100

func serializeAnyValue(buf *strings.Builder, v *commonv1.AnyValue, depth int) {
	if depth == 0 {
		buf.Reset()
	}
	if depth > maxNestedDepth {
		buf.WriteString("...")
		return
	}

	switch v.GetValue().(type) {

	case *commonv1.AnyValue_StringValue:
		// In the case of unstructured text, the top-level Body object
		// is just a simple string, so there is no need to WriteJson
		if depth == 0 {
			buf.WriteString(v.GetStringValue())
			return
		}
		fflib.WriteJson(buf, []byte(v.GetStringValue()))
	case *commonv1.AnyValue_BoolValue:
		fflib.WriteJson(buf, []byte(strconv.FormatBool(v.GetBoolValue())))
	case *commonv1.AnyValue_IntValue:
		fflib.WriteJson(buf, []byte(strconv.FormatInt(v.GetIntValue(), 10)))
	case *commonv1.AnyValue_DoubleValue:
		fflib.WriteJson(buf, []byte(strconv.FormatFloat(v.GetDoubleValue(), 'f', -1, 64)))

	case *commonv1.AnyValue_KvlistValue:
		buf.WriteByte('{')
		for i, kv := range v.GetKvlistValue().GetValues() {
			if i != 0 {
				buf.WriteByte(',')
			}
			fflib.WriteJson(buf, []byte(kv.GetKey()))
			buf.WriteByte(':')
			serializeAnyValue(buf, kv.GetValue(), depth+1)
		}
		buf.WriteByte('}')

	case *commonv1.AnyValue_ArrayValue:
		buf.WriteByte('[')
		for i, v := range v.GetArrayValue().GetValues() {
			if i != 0 {
				buf.WriteByte(',')
			}
			serializeAnyValue(buf, v, depth+1)
		}
		buf.WriteByte(']')

	default:
		fflib.WriteJson(buf, []byte(v.GetStringValue()))
	}
}
