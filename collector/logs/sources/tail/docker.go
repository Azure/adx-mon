package tail

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
)

// DockerLog is the format from docker json logs
type DockerLog struct {
	Log    string `json:"log"`
	Stream string `json:"stream"`
	Time   string `json:"time"`
}

func parseDockerLog(line string, log *types.Log) error {
	parsed := DockerLog{}
	err := json.Unmarshal([]byte(line), &parsed)
	if err != nil {
		return fmt.Errorf("parseDockerLog: %w", err)
	}

	parsedTime, err := time.Parse(time.RFC3339Nano, parsed.Time)
	if err != nil {
		parsedTime = time.Now()
	}
	log.Timestamp = uint64(parsedTime.UnixNano())
	log.ObservedTimestamp = uint64(time.Now().UnixNano())
	log.Body["message"] = parsed.Log
	log.Body["stream"] = parsed.Stream

	return nil
}
