package transform

import (
	"bytes"
	"encoding/csv"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	logsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/cespare/xxhash"
	fflib "github.com/pquerna/ffjson/fflib/v1"

	adxcsv "github.com/Azure/adx-mon/pkg/csv"
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

func (w *CSVWriter) MarshalCSV(t interface{}) error {
	// Note the Benchmark for this function in csv_test.go
	switch t := t.(type) {
	case prompb.TimeSeries:
		return w.marshalTS(t)
	case *logsv1.ExportLogsServiceRequest:
		return w.marshalLog(t)
	default:
		return errors.New("unknown type")
	}
}

func (w *CSVWriter) marshalTS(ts prompb.TimeSeries) error {
	buf := w.labelsBuf
	buf.Reset()

	seriesIdBuf := w.seriesIdBuf
	seriesIdBuf.Reset()

	var j int

	// Marshal the labels as JSON and avoid allocations since this code is in the hot path.
	buf.WriteByte('{')
	for _, v := range ts.Labels {
		w.seriesIdBuf.Write(v.Name)
		w.seriesIdBuf.Write(v.Value)

		// Drop the __name__ label since it is implied that the name of the CSV file is the name of the metric.
		if bytes.Equal(v.Name, []byte("__name__")) {
			continue
		}

		// We need to drop the labels that have been promoted to columns to avoid duplicating the storage of the value
		// in both the Labels column the lifted column.  The columns are sorted so we can walk them in order and skip
		// any matches.
		var skip bool
		for j < len(w.columns) {
			cmp := compareLower(w.columns[j], v.Name)
			// The lifted column is less than the current label, we need move to the next column and check again.
			if cmp < 0 {
				j++
				continue
			} else if cmp == 0 {
				// The lifted column matches the current label, we need to skip it.
				j++
				skip = true
				break
			}

			// The lifted column is greater than the current label, we can stop looking.
			break
		}

		if skip {
			continue
		}

		if buf.Bytes()[buf.Len()-1] != '{' {
			buf.WriteByte(',')
		}
		fflib.WriteJson(buf, v.Name)
		buf.WriteByte(':')
		fflib.WriteJson(buf, v.Value)
	}

	buf.WriteByte('}')
	seriesId := xxhash.Sum64(seriesIdBuf.Bytes())

	for _, v := range ts.Samples {
		line := w.line[:0]

		// Timestamp
		line = time.Unix(v.Timestamp/1000, (v.Timestamp%1000)*int64(time.Millisecond)).UTC().AppendFormat(line, time.RFC3339Nano)

		// seriesID
		line = append(line, ',')
		line = strconv.AppendInt(line, int64(seriesId), 10)

		if len(w.columns) > 0 {
			var i, j int
			for i < len(ts.Labels) && j < len(w.columns) {
				cmp := compareLower(ts.Labels[i].Name, w.columns[j])
				if cmp == 0 {
					line = adxcsv.Append(line, ts.Labels[i].Value)
					j++
					i++
				} else if cmp > 0 {
					j++
					line = append(line, ',')
				} else {
					i++
				}
			}
		}

		// labels
		line = adxcsv.AppendQuoted(line, buf.Bytes())
		line = append(line, ',')

		// Values
		line = strconv.AppendFloat(line, v.Value, 'f', 9, 64)

		// End of line
		line = adxcsv.AppendNewLine(line)

		if n, err := w.w.Write(line); err != nil {
			return err
		} else if n != len(line) {
			return errors.New("short write")
		}
	}
	return nil
}

func (w *CSVWriter) marshalLog(log *logsv1.ExportLogsServiceRequest) error {
	// See ingestor/storage/schema::NewLogsSchema
	// we're writing a ExportLogsServiceRequest as a CSV

	// There are 9 fields defined in an OTLP log schema
	fields := make([]string, 0, 9)
	// Convert log records to CSV
	// see samples at https://opentelemetry.io/docs/specs/otel/protocol/file-exporter/#examples
	for _, r := range log.GetResourceLogs() {
		for _, s := range r.GetScopeLogs() {
			for _, l := range s.GetLogRecords() {
				// Reset fields
				fields = fields[:0]
				// Timestamp
				ts := int64(l.GetTimeUnixNano())
				fields = append(fields, time.Unix(ts/1000, (ts%1000)*int64(time.Millisecond)).UTC().Format(time.RFC3339Nano))
				// ObservedTimestamp
				ts = int64(l.GetObservedTimeUnixNano())
				fields = append(fields, time.Unix(ts/1000, (ts%1000)*int64(time.Millisecond)).UTC().Format(time.RFC3339Nano))
				// TraceId
				fields = append(fields, string(l.GetTraceId()))
				// SpanId
				fields = append(fields, string(l.GetSpanId()))
				// SeverityText
				fields = append(fields, l.GetSeverityText())
				// SeverityNumber
				fields = append(fields, l.GetSeverityNumber().String())
				// Body
				fields = append(fields, l.GetBody().GetStringValue())
				// Resource
				buf := w.buf
				buf.Reset()
				buf.WriteByte('{')
				for _, a := range r.GetResource().GetAttributes() {
					if buf.String()[buf.Len()-1] != '{' {
						buf.WriteByte(',')
					}
					fflib.WriteJson(buf, []byte(a.GetKey()))
					buf.WriteByte(':')
					fflib.WriteJson(buf, []byte(a.GetValue().GetStringValue()))
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
			}
		}
	}

	w.enc.Flush()
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

// Normalize converts a metrics name to a ProperCase table name
func Normalize(s []byte) []byte {
	var b bytes.Buffer
	for i := 0; i < len(s); i++ {

		// Skip _, but capitalize the first letter after an _
		if s[i] == '_' {
			if i+1 < len(s) {
				if s[i+1] >= 'a' && s[i+1] <= 'z' {
					b.Write([]byte{byte(unicode.ToUpper(rune(s[i+1])))})
					i += 1
					continue
				}
			}
			continue
		}

		// Capitalize the first letter
		if i == 0 {
			b.Write([]byte{byte(unicode.ToUpper(rune(s[i])))})
			continue
		}
		b.WriteByte(s[i])
	}
	return b.Bytes()
}

func compareLower(sa, sb []byte) int {
	for {
		rb, nb := utf8.DecodeRune(sb)
		ra, na := utf8.DecodeRune(sa)

		if na == 0 && nb > 0 {
			return -1
		} else if na > 0 && nb == 0 {
			return 1
		} else if na == 0 && nb == 0 {
			return 0
		}

		rb = unicode.ToLower(rb)
		ra = unicode.ToLower(ra)

		if ra < rb {
			return -1
		} else if ra > rb {
			return 1
		}

		// Trim rune from the beginning of each string.
		sa = sa[na:]
		sb = sb[nb:]
	}
}
