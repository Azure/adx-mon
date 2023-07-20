package logs

import "fmt"

type Log struct {
	Message  string
	Metadata map[string]string
}

func (l *Log) String() string {
	return fmt.Sprintf("labels: %v message: %s", l.Metadata, l.Message)
}

type Transform interface {
	Transform(log *Log) ([]*Log, error)
}
