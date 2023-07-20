package journald

import "github.com/Azure/adx-mon/collector/logs"

// combines multiple lines from docker into a single log entry
type DockerMultiline struct {
	PartialLogs map[string]*logs.Log
}

func (d *DockerMultiline) Transform(log *logs.Log) ([]*logs.Log, error) {
	partialID, ok := log.Metadata["CONTAINER_PARTIAL_ID"]
	if !ok {
		return []*logs.Log{log}, nil
	}
	partialID = partialID
	//TODO TODO TODO
	return nil, nil
}
