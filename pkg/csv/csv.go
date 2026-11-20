package csv

import "bytes"

// Append CSV appends a record to the given buffer without quoting.
func Append(dst []byte, record []byte) []byte {
	dst = append(dst, ',')
	dst = append(dst, record...)
	return dst
}

// AppendField appends a field, quoting it only when it contains a comma, quote
// or line break.
func AppendField(dst []byte, field []byte) []byte {
	if bytes.ContainsAny(field, ",\"\r\n") {
		return AppendQuoted(dst, field)
	}
	return Append(dst, field)
}

// AppendQuota CSV appends a record to the given buffer with quoting.
func AppendQuoted(dst []byte, field []byte) []byte {
	dst = append(dst, ',')
	dst = append(dst, '"')
	for len(field) > 0 {
		// Encode the special character.
		if len(field) > 0 {
			switch field[0] {
			case '"':
				dst = append(dst, `""`...)
			case '\r':
				dst = append(dst, '\r')
			case '\n':
				dst = append(dst, '\n')
			default:
				dst = append(dst, field[0])
			}
			field = field[1:]
		}
	}
	dst = append(dst, '"')
	return dst
}

// AppendNewLine appends a new line to the given buffer.
func AppendNewLine(dst []byte) []byte {
	return append(dst, '\n')
}
