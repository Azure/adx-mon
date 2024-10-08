package prompb

import (
	"bufio"
	"fmt"
	"io"

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
		if i.scanner.Err() != nil {
			return false
		}

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

func (i *Iterator) TimeSeriesInto(ts *TimeSeries) (*TimeSeries, error) {
	if len(i.current) == 0 {
		return nil, fmt.Errorf("no current value")
	}
	ts, err := i.ParseTimeSeriesInto(ts, i.current)
	if err != nil {
		return nil, fmt.Errorf("error parsing time series: %w: %s", err, i.current)
	}
	return ts, err
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
	i.buf = i.buf[:0]
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

	ts.AppendLabelString(lableName, name)

	ts, line, err = i.parseLabels(ts, line)
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
		timestamp, err = fastfloat.ParseInt64(t)
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp: %v", err)
		}
	}

	ts.AppendSample(timestamp, value)

	return ts, nil
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

func (i *Iterator) parseLabels(ts *TimeSeries, line string) (*TimeSeries, string, error) {
	orig := line
	line = trimSpacePrefix(line)
	if len(line) == 0 {
		return nil, "", nil
	}

	if line[0] == '{' {
		line = line[1:]
		for len(line) > 0 {
			if line[0] == '}' {
				return ts, line[1:], nil
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

			key := line[:idx]
			if len(key) == 0 {
				return nil, "", fmt.Errorf("invalid label: no key: %s", orig)
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

			ts.AppendLabel([]byte(key), value)

			if len(line) == 0 {
				return nil, "", fmt.Errorf("invalid labels: no closing }: %s", orig)
			}

			if line[0] == '}' {
				return ts, line[1:], nil
			}

			if line[0] == ',' {
				line = line[1:]
			}
		}
	}
	return ts, line, nil
}

func parseName(line string) (string, string) {
	line = trimSpacePrefix(line)
	for i := 0; i < len(line); i++ {
		if line[i] == '{' || isSpace(line[i]) {
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

const lableName = "__name__"

var nameBytes = []byte(lableName)
