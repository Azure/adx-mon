package transform

import (
	"bytes"
	"encoding/csv"
	"github.com/Azure/adx-mon/pool"
	"github.com/Azure/adx-mon/prompb"
	"github.com/cespare/xxhash"
	fflib "github.com/pquerna/ffjson/fflib/v1"
	"io"
	"strconv"
	"time"
	"unicode"
)

var (
	buffPool = pool.NewGeneric(128, func(sz int) interface{} {
		return bytes.NewBuffer(make([]byte, 0, sz))
	})
)

type CSVWriter struct {
	Fd  io.Writer
	enc *csv.Writer
}

func NewCSVWriter(w io.Writer) *CSVWriter {
	return &CSVWriter{
		Fd:  w,
		enc: csv.NewWriter(w),
	}
}

func (w *CSVWriter) MarshalCSV(ts prompb.TimeSeries) error {
	buf := buffPool.Get(4096).(*bytes.Buffer)
	buf.Reset()
	defer buffPool.Put(buf)

	// Marshal the labels as JSON and avoid allocations since this code is in the hot path.
	buf.WriteByte('{')
	for i, v := range ts.Labels {
		fflib.WriteJson(buf, v.Name)
		buf.WriteByte(':')
		fflib.WriteJson(buf, v.Value)
		if i < len(ts.Labels)-1 {
			buf.WriteByte(',')
		}
	}
	buf.WriteByte('}')
	b := buf.Bytes()

	fields := make([]string, 0, 4)
	for _, v := range ts.Samples {
		fields = fields[:0]

		// Timestamp
		tm := time.Unix(v.Timestamp/1000, (v.Timestamp%1000)*int64(time.Millisecond)).UTC().Format(time.RFC3339Nano)
		fields = append(fields, tm)

		seriesId := xxhash.Sum64(b)
		fields = append(fields, strconv.FormatInt(int64(seriesId), 10))

		// labels
		fields = append(fields, string(b))

		// Values
		fields = append(fields, strconv.FormatFloat(v.Value, 'f', 9, 64))

		if err := w.enc.Write(fields); err != nil {
			return err
		}
	}

	w.enc.Flush()
	return nil
}

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
