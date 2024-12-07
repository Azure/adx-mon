package sourceparse

import "github.com/Azure/adx-mon/collector/logs/types"

type Type string

const (
	// Log types for extracting the timestamp and message

	// Docker logs are the json formatted logs from the docker daemon
	LogTypeDocker Type = "docker"
	// Cri logs are the logs from the container runtime interface
	LogTypeCri Type = "cri"
	// Kubernetes logs can be in either docker or cri format
	LogTypeKubernetes Type = "kubernetes"
	// Plain logs are logs that are not in a known format
	LogTypePlain Type = "plain"
)

// LogTypeParser is a function that parses a line of text from a file based on its format.
// Must assign log.Timestamp, log.ObservedTimestamp, and the types.BodyKeyMessage property of log.Body.
type LogTypeParser interface {
	// Parse parses a line of text from a file into a log.
	Parse(line string, log *types.Log) (message string, isPartial bool, err error)
}

func GetLogTypeParser(logType Type) LogTypeParser {
	switch logType {
	case LogTypeDocker:
		return NewDockerParser()
	case LogTypeCri:
		return NewCriParser()
	case LogTypeKubernetes:
		return NewKubernetesParser()
	case LogTypePlain:
		return &PlaintextParser{}
	default:
		return &PlaintextParser{}
	}
}
