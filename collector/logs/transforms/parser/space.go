package parser

import (
	"fmt"
	"strings"

	"github.com/Azure/adx-mon/collector/logs/types"
)

const ParserTypeSpace ParserType = "space"

type SpaceParserConfig struct{}

type SpaceParser struct{}

func NewSpaceParser(config SpaceParserConfig) (*SpaceParser, error) {
	return &SpaceParser{}, nil
}

// Parse splits the message by whitespace and adds each field to the log body with a numeric index.
// For example, "hello world" becomes {"field0": "hello", "field1": "world"}
// This is useful for parsing generic log lines where fields are separated by one or more spaces.
// This parser uses strings.Fields, which splits on any whitespace, including tabs and newlines.
func (p *SpaceParser) Parse(log *types.Log, msg string) error {
	if len(msg) == 0 {
		return nil
	}

	fields := strings.Fields(msg)
	for i, field := range fields {
		log.SetBodyValue(fmt.Sprintf("field%d", i), field)
	}

	return nil
}
