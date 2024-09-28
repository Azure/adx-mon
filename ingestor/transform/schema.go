package transform

var (
	DefaultMetricsSchema = Schema{
		{Name: "Timestamp", Type: "datetime"},
		{Name: "SeriesId", Type: "long"},
		{Name: "Labels", Type: "dynamic"},
		{Name: "Value", Type: "real"},
	}
)

type Field struct {
	Name   string
	Type   string
	Source string
}

type Schema []Field
