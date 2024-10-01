package transform

import (
	"bytes"
	"encoding/csv"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/schema"
	"github.com/cespare/xxhash"
	fflib "github.com/pquerna/ffjson/fflib/v1"

	adxcsv "github.com/Azure/adx-mon/pkg/csv"
)

type MetricsCSVWriter struct {
	w   *bytes.Buffer
	buf *strings.Builder
	enc *csv.Writer

	labelsBuf   *bytes.Buffer
	seriesIdBuf *bytes.Buffer
	line        []byte
	columns     [][]byte

	lifted Fields

	headerWritten bool
}

// NewMetricsCSVWriter returns a new CSVWriter that writes to the given buffer.  The columns, if specified, are
// label keys that will be promoted to columns.
func NewMetricsCSVWriter(w *bytes.Buffer, lifted Fields) *MetricsCSVWriter {
	writer := &MetricsCSVWriter{
		w:           w,
		buf:         &strings.Builder{},
		seriesIdBuf: bytes.NewBuffer(make([]byte, 0, 1024)),
		labelsBuf:   bytes.NewBuffer(make([]byte, 0, 1024)),
		enc:         csv.NewWriter(w),
		line:        make([]byte, 0, 4096),
		columns:     make([][]byte, 0, len(lifted)),
		lifted:      lifted,
	}

	columns := make([]string, 0, len(lifted))
	for _, v := range lifted {
		columns = append(columns, v.Source)
	}
	writer.InitColumns(columns)
	return writer
}

func (w *MetricsCSVWriter) MarshalCSV(ts *prompb.TimeSeries) error {
	if !w.headerWritten {
		line := w.line[:0]
		buf := w.labelsBuf
		for _, v := range schema.DefaultMetricsMapping {
			buf.Reset()
			buf.WriteString(v.Column)
			buf.Write([]byte(":"))
			buf.WriteString(v.DataType)
			if len(line) == 0 {
				line = append(line, buf.Bytes()...)
			} else {
				line = adxcsv.Append(line, buf.Bytes())
			}
		}

		line = adxcsv.AppendNewLine(line)

		if n, err := w.w.Write(line); err != nil {
			return err
		} else if n != len(line) {
			return errors.New("short write")
		}
		w.headerWritten = true
	}

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

func (w *MetricsCSVWriter) Reset() {
	w.w.Reset()
	w.buf.Reset()
	w.headerWritten = false
}

func (w *MetricsCSVWriter) Bytes() []byte {
	return w.w.Bytes()
}

// InitColumns initializes the labels that will be promoted to columns in the CSV file.  This can be done
// once on the *Writer and subsequent calls are no-ops.
func (w *MetricsCSVWriter) InitColumns(columns []string) {
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
