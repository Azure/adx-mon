package sourceparse

import (
	"fmt"
	"strings"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
)

type CriParser struct {
	streamPartials map[string]string
}

func NewCriParser() *CriParser {
	return &CriParser{
		streamPartials: make(map[string]string),
	}
}

// Not safe for concurrent use.
func (p *CriParser) Parse(line string, log *types.Log) (message string, isPartial bool, err error) {
	timestampEndIdx := strings.IndexByte(line, ' ')
	if timestampEndIdx == -1 {
		return "", false, fmt.Errorf("parseCriLog: invalid log format - timestamp not found")
	}
	timestamp := line[:timestampEndIdx]
	rest := line[timestampEndIdx+1:]

	streamEndIdx := strings.IndexByte(rest, ' ')
	if streamEndIdx == -1 {
		return "", false, fmt.Errorf("parseCriLog: invalid log format - stream not found")
	}
	stream := rest[:streamEndIdx]
	rest = rest[streamEndIdx+1:]

	tagEndIdx := strings.IndexByte(rest, ' ')
	if tagEndIdx == -1 {
		return "", false, fmt.Errorf("parseCriLog: invalid log format - tag not found")
	}
	tag := rest[:tagEndIdx]
	rest = rest[tagEndIdx+1:]

	isPartial = tag == "P"
	currentLogMsg := rest

	previousLog, hasPreviousLog := p.streamPartials[stream]
	if hasPreviousLog {
		currentLogMsg = previousLog + currentLogMsg
	}

	if isPartial {
		p.streamPartials[stream] = currentLogMsg
		return "", true, nil
	} else if hasPreviousLog {
		delete(p.streamPartials, stream)
	}

	parsedTimestamp, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return "", false, fmt.Errorf("parseCriLog time.Parse: %w", err)
	}

	log.SetTimestamp(uint64(parsedTimestamp.UnixNano()))
	log.SetObservedTimestamp(uint64(time.Now().UnixNano()))
	log.SetBodyValue("stream", stream)

	return currentLogMsg, false, nil
}
