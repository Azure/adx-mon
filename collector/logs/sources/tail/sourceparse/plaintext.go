package sourceparse

import (
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
)

type PlaintextParser struct{}

func (p *PlaintextParser) Parse(line string, log *types.Log) (string, bool, error) {
	log.Timestamp = uint64(time.Now().UnixNano())
	log.ObservedTimestamp = uint64(time.Now().UnixNano())

	return line, false, nil
}
