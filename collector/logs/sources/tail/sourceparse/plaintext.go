package sourceparse

import (
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
)

type PlaintextParser struct{}

func (p *PlaintextParser) Parse(line string, log *types.Log) (string, bool, error) {
	now := uint64(time.Now().UnixNano())
	log.SetTimestamp(now)
	log.SetObservedTimestamp(now)

	return line, false, nil
}
