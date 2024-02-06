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

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/cespare/xxhash"
	"github.com/pquerna/ffjson/ffjson"
	fflib "github.com/pquerna/ffjson/fflib/v1"

	adxcsv "github.com/Azure/adx-mon/pkg/csv"
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

func (w *CSVWriter) MarshalTS(ts prompb.TimeSeries) error {
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
		if bytes.Equal(v.Name, []byte("__name__")) || bytes.HasPrefix(v.Name, []byte("adxmon_")) {
			continue
		}

		// We need to drop the labels that have been promoted to columns to avoid duplicating the storage of the value
		// in both the Labels column the lifted column.  The columns are sorted so we can walk them in order and skip
		// any matches.
		var skip bool
		for j < len(w.columns) {
			cmp := prompb.CompareLower(w.columns[j], v.Name)
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
				cmp := prompb.CompareLower(ts.Labels[i].Name, w.columns[j])
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

			for j < len(w.columns) {
				line = append(line, ',')
				j++
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

func otlpTSToUTC(ts int64) string {
	// check for nanosecond precision
	if ts&0x1fffffffffffff == ts {
		return time.Unix(ts/1000, (ts%1000)*int64(time.Millisecond)).UTC().Format(time.RFC3339Nano)
	}
	return time.Unix(0, ts).UTC().Format(time.RFC3339Nano)
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
		if v := l.GetBody().GetKvlistValue(); v != nil {
			buf.Reset()
			buf.WriteByte('{')
			for _, kv := range v.GetValues() {
				if buf.String()[buf.Len()-1] != '{' {
					buf.WriteByte(',')
				}
				fflib.WriteJson(buf, []byte(kv.GetKey()))
				buf.WriteByte(':')
				fflib.WriteJson(buf, []byte(kv.GetValue().GetStringValue()))
			}
			buf.WriteByte('}')
			fields = append(fields, newlineReplacer.Replace(buf.String()))
		} else {
			fields = append(fields, newlineReplacer.Replace(l.GetBody().GetStringValue()))
		}
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
	}

	w.enc.Flush()
	return w.enc.Error()
}

func (w *CSVWriter) MarshalNativeLog(log *types.Log) error {
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
	fields = append(fields, newlineReplacer.Replace(buf.String()))

	// Resource - we don't have this
	fields = append(fields, "{}")
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
	return AppendNormalize(make([]byte, 0, len(s)), s)
}

// AppendNormalize converts a metrics name to a ProperCase table name and appends it to dst.
func AppendNormalize(dst, s []byte) []byte {
	for i := 0; i < len(s); i++ {
		// Skip any non-alphanumeric characters, but capitalize the first letter after it
		allowedChar := s[i] >= 'a' && s[i] <= 'z' || s[i] >= '0' && s[i] <= '9' || s[i] >= 'A' && s[i] <= 'Z'
		if !allowedChar {
			if i+1 < len(s) {
				if s[i+1] >= 'a' && s[i+1] <= 'z' {
					dst = append(dst, byte(unicode.ToUpper(rune(s[i+1]))))
					i += 1
					continue
				}
			}
			continue
		}

		// Capitalize the first letter
		if i == 0 {
			dst = append(dst, byte(unicode.ToUpper(rune(s[i]))))
			continue
		}
		dst = append(dst, s[i])
	}
	return dst
}
