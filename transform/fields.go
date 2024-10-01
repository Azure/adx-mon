package transform

// Field defines a label or attribute that is lifted to a column.
type Field struct {
	Name   string
	Type   string
	Source string
}

type Fields []Field
