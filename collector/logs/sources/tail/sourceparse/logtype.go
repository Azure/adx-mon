package sourceparse

import "github.com/Azure/adx-mon/collector/logs/types"

type Type string

const (
	// Log types for extracting the timestamp and message
	LogTypeDocker Type = "docker"
	LogTypePlain  Type = "plain"
)

// LogTypeParser is a function that parses a line of text from a file based on its format.
// Must assign log.Timestamp, log.ObservedTimestamp, and the types.BodyKeyMessage property of log.Body.
type LogTypeParser interface {
	// Parse parses a line of text from a file into a log.
	Parse(line string, log *types.Log) (isPartial bool, err error)
}

func GetLogTypeParser(logType Type) LogTypeParser {
	switch logType {
	case LogTypeDocker:
		return NewDockerParser()
	case LogTypePlain:
		return &PlaintextParser{}
	default:
		return &PlaintextParser{}
	}
}
