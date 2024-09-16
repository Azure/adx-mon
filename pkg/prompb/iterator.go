package prompb

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/valyala/fastjson/fastfloat"
)

type Iterator struct {
	r       io.ReadCloser
	scanner *bufio.Scanner
	current string

	buf []byte
}

func NewIterator(r io.ReadCloser) *Iterator {
	return &Iterator{
		r:       r,
		scanner: bufio.NewScanner(r),
		buf:     make([]byte, 0, 1024),
	}
}

func (i *Iterator) Next() bool {
	if i.scanner.Scan() {
		i.current = i.scanner.Text()
		if i.isComment(i.current) || i.isSpace(i.current) {
			return i.Next()
		}
		return true
	}
	return false
}

func (i *Iterator) Value() string {
	return i.current
}

func (i *Iterator) TimeSeries() (TimeSeries, error) {
	if len(i.current) == 0 {
		return TimeSeries{}, fmt.Errorf("no current value")
	}
	return i.ParseTimeSeries(i.current)
}

func (i *Iterator) TimeSeriesInto(ts *TimeSeries) (*TimeSeries, error) {
	if len(i.current) == 0 {
		return nil, fmt.Errorf("no current value")
	}
	return i.ParseTimeSeriesInto(ts, i.current)
}

func (i *Iterator) Close() error {
	return i.r.Close()
}

func (i *Iterator) Err() error {
	return i.scanner.Err()
}

func (i *Iterator) Reset(r io.ReadCloser) {
	i.r = r
	i.scanner = bufio.NewScanner(r)
	i.current = i.current[:0]
}

func (i *Iterator) isComment(s string) bool {
	for j := 0; j < len(s); j++ {
		if isSpace(s[j]) {
			continue
		}

		if s[j] == '#' {
			return true
		} else {
			return false
		}
	}
	return false
}

func (i *Iterator) isSpace(s string) bool {
	for j := 0; j < len(s); j++ {
		if !isSpace(s[j]) {
			return false
		}
	}
	return true
}

func (i *Iterator) ParseTimeSeriesInto(ts *TimeSeries, line string) (*TimeSeries, error) {
	var (
		name string
		err  error
	)
	name, line = parseName(line)

	if cap(ts.Labels) > len(ts.Labels) {
		ts.Labels = ts.Labels[:len(ts.Labels)+1]
	} else {
		ts.Labels = append(ts.Labels, Label{})
	}

	ts.Labels[len(ts.Labels)-1].Name = nameBytes

	if cap(ts.Labels[len(ts.Labels)-1].Value) >= len(name) {
		ts.Labels[len(ts.Labels)-1].Value = append(ts.Labels[len(ts.Labels)-1].Value[:0], name...)
	} else {
		ts.Labels[len(ts.Labels)-1].Value = make([]byte, len(name))
		copy(ts.Labels[len(ts.Labels)-1].Value, name)
	}

	ts.Labels, line, err = i.parseLabels(ts.Labels, line)
	if err != nil {
		return nil, err
	}

	v, line := parseValue(line)

	t, line := parseTimestamp(line)

	value, err := fastfloat.Parse(v)
	if err != nil {
		return nil, fmt.Errorf("invalid value: %v", err)
	}

	var timestamp int64
	if len(t) > 0 {
		timestamp = fastfloat.ParseInt64BestEffort(t)
	}

	if cap(ts.Samples) > len(ts.Samples) {
		ts.Samples = ts.Samples[:len(ts.Samples)+1]
	} else {
		ts.Samples = append(ts.Samples, Sample{})
	}
	ts.Samples[len(ts.Samples)-1].Value = value
	ts.Samples[len(ts.Samples)-1].Timestamp = timestamp

	return ts, nil
}

func (i *Iterator) ParseTimeSeries(line string) (TimeSeries, error) {
	var (
		name string
		err  error
	)
	name, line = parseName(line)
	n := strings.Count(line, ",")
	labels := make([]Label, 0, n+1)
	labels = append(labels, Label{
		Name:  []byte("__name__"),
		Value: []byte(name),
	})

	labels, line, err = i.parseLabels(labels, line)
	if err != nil {
		return TimeSeries{}, err
	}

	v, line := parseValue(line)

	ts, line := parseTimestamp(line)

	value, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return TimeSeries{}, fmt.Errorf("invalid value: %v", err)
	}

	var timestamp int64
	if len(ts) > 0 {
		timestamp, err = strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return TimeSeries{}, fmt.Errorf("invalid timestamp: %v", err)
		}
	}

	return TimeSeries{
		Labels: labels,
		Samples: []Sample{
			{
				Value:     value,
				Timestamp: timestamp,
			},
		},
	}, nil
}

func parseTimestamp(line string) (string, string) {
	line = trimSpacePrefix(line)
	for i := 0; i < len(line); i++ {
		if isSpace(line[i]) {
			return line[:i], line[i:]
		}
	}
	return line, ""
}

func parseValue(line string) (string, string) {
	line = trimSpacePrefix(line)
	for i := 0; i < len(line); i++ {
		if isSpace(line[i]) {
			return line[:i], line[i:]
		}
	}
	return line, ""
}

func (i *Iterator) parseLabels(labels Labels, line string) (Labels, string, error) {
	orig := line
	line = trimSpacePrefix(line)
	if len(line) == 0 {
		return labels, "", nil
	}

	if line[0] == '{' {
		line = line[1:]
		for len(line) > 0 {
			if line[0] == '}' {
				return labels, line[1:], nil
			}

			var idx = -1
			for i := 0; i < len(line); i++ {
				if line[i] == '=' {
					idx = i
					break
				}
			}
			if idx == -1 {
				return nil, "", fmt.Errorf("invalid label: no =: %s", orig)
			}

			var l Label
			if cap(labels) > len(labels) {
				labels = labels[:len(labels)+1]
			} else {
				labels = append(labels, Label{})
			}
			l = labels[len(labels)-1]

			key := line[:idx]
			if cap(l.Name) >= len(key) {
				l.Name = append(l.Name[:0], key...)
			} else {
				n := make([]byte, len(key))
				copy(n, key)
				l.Name = n
			}
			line = line[idx+1:]

			if len(line) == 0 {
				return nil, "", fmt.Errorf("invalid label: no opening \": %s", orig)
			}
			if line[0] == '"' {
				line = line[1:]
			}

			value := i.buf[:0]
			var j int
			for j < len(line) {
				if line[j] == '\\' {
					switch line[j+1] {
					case '\\':
						value = append(value, '\\')
					case '"':
						value = append(value, '"')
					case 'n':
						value = append(value, '\n')
					default:
						value = append(value, '\\', line[j])
					}
					j += 2
					continue
				}

				if line[j] == '"' {
					line = line[j+1:]
					break
				}
				value = append(value, line[j])
				j += 1
			}

			if cap(l.Value) >= len(value) {
				l.Value = append(l.Value[:0], value...)
			} else {
				v := make([]byte, len(value))
				copy(v, value)
				l.Value = v
			}
			labels[len(labels)-1] = l

			if len(line) == 0 {
				return nil, "", fmt.Errorf("invalid labels: no closing }: %s", orig)
			}

			if line[0] == '}' {
				return labels, line[1:], nil
			}

			if line[0] == ',' {
				line = line[1:]
			}
		}
	}
	return labels, line, nil
}

func parseName(line string) (string, string) {
	line = trimSpacePrefix(line)
	for i := 0; i < len(line); i++ {
		if line[i] == '{' || unicode.IsSpace(rune(line[i])) {
			return line[:i], line[i:]
		}
	}
	return line, ""
}

func trimSpacePrefix(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			continue
		}
		return s[i:]
	}
	return s
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t'
}

var nameBytes = []byte("__name__")
