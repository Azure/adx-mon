package tail

import (
	"fmt"
	"strings"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/pquerna/ffjson/ffjson"
)

var (
	// At the end of the "log" field, this signifies that the log line is complete without further splits.
	dockerCompleteLogSuffix = "\n"

	// Length of the suffix
	dockerCompleteLogSuffixLength = len(dockerCompleteLogSuffix)
)

// DockerLog is the format from docker json logs
type DockerLog struct {
	Log    string `json:"log"`
	Stream string `json:"stream"`
	Time   string `json:"time"`
}

type DockerParser struct {
	streamPartials map[string]string
}

func NewDockerParser() *DockerParser {
	return &DockerParser{
		streamPartials: make(map[string]string),
	}
}

func (p *DockerParser) Parse(line string, log *types.Log) (isPartial bool, err error) {
	parsed := DockerLog{}
	err = ffjson.Unmarshal([]byte(line), &parsed)
	if err != nil {
		return false, fmt.Errorf("parseDockerLog: %w", err)
	}

	// Docker json logs are always terminated by a newline.
	// If they are not, the log is a partial one within that given stream. Docker splits logs at 16k.
	isPartial = !strings.HasSuffix(parsed.Log, dockerCompleteLogSuffix)
	currentLogMsg := parsed.Log

	previousLog, hasPreviousLog := p.streamPartials[parsed.Stream]
	if hasPreviousLog { //combine
		currentLogMsg = previousLog + currentLogMsg
	}

	if isPartial {
		p.streamPartials[parsed.Stream] = currentLogMsg
		return true, nil
	} else if hasPreviousLog {
		delete(p.streamPartials, parsed.Stream)
	}

	parsedTime, err := time.Parse(time.RFC3339Nano, parsed.Time)
	if err != nil {
		parsedTime = time.Now()
	}
	log.Timestamp = uint64(parsedTime.UnixNano())
	log.ObservedTimestamp = uint64(time.Now().UnixNano())
	log.Body["stream"] = parsed.Stream
	// Strip trailing newline
	log.Body[types.BodyKeyMessage] = currentLogMsg[:len(currentLogMsg)-dockerCompleteLogSuffixLength]

	return false, nil
}
