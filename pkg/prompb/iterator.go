package prompb

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
)

type Iterator struct {
	r       io.ReadCloser
	scanner *bufio.Scanner
	current string
}

func NewIterator(r io.ReadCloser) *Iterator {
	return &Iterator{
		r:       r,
		scanner: bufio.NewScanner(r),
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
	return ParseTimeSeries(i.current)
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
		if unicode.IsSpace(rune(s[j])) {
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
		if !unicode.IsSpace(rune(s[j])) {
			return false
		}
	}
	return true
}

func ParseTimeSeries(line string) (TimeSeries, error) {
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

	labels, line, err = parseLabels(labels, line)
	if err != nil {
		return TimeSeries{}, err
	}

	v, line := parseValue(line)

	ts, line := parseTimestamp(line)

	value, err := strconv.ParseFloat(string(v), 64)
	if err != nil {
		return TimeSeries{}, fmt.Errorf("invalid value: %v", err)
	}

	var timestamp int64
	if len(ts) > 0 {
		timestamp, err = strconv.ParseInt(string(ts), 10, 64)
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

func parseLabels(labels Labels, line string) (Labels, string, error) {
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

			idx := strings.Index(line, "=")
			if idx == -1 {
				return nil, "", fmt.Errorf("invalid label: no =: %s", orig)
			}

			key := line[:idx]
			line = line[idx+1:]

			if len(line) == 0 {
				return nil, "", fmt.Errorf("invalid label: no opening \": %s", orig)
			}
			if line[0] == '"' {
				line = line[1:]
			}

			value := make([]byte, 0, 64)
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

			labels = append(labels, Label{
				Name:  []byte(key),
				Value: value,
			})

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
		if unicode.IsSpace(rune(s[i])) {
			continue
		}
		return s[i:]
	}
	return s
}

func isSpace(c byte) bool {
	return unicode.IsSpace(rune(c))
}
