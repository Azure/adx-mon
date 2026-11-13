package sourceparse

import (
	"fmt"
	"strings"
	"time"

	"github.com/Azure/adx-mon/collector/logs/sources/partial"
	"github.com/Azure/adx-mon/collector/logs/types"
)

type CriParser struct {
	streamPartials partial.Assembler
}

func NewCriParser() *CriParser {
	return &CriParser{
		streamPartials: partial.New("cri"),
	}
}

// Not safe for concurrent use.
func (p *CriParser) Parse(line string, log *types.Log) (message string, isPartial bool, err error) {
	before, after, ok := strings.Cut(line, " ")
	if !ok {
		return "", false, fmt.Errorf("parseCriLog: invalid log format - timestamp not found")
	}
	timestamp := before
	rest := after

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

	currentLogMsg, isPartial = p.streamPartials.Add(stream, currentLogMsg, isPartial, time.Now())
	if isPartial {
		return "", true, nil
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

func (p *CriParser) ExpirePartials(now time.Time) { p.streamPartials.Expire(now) }
