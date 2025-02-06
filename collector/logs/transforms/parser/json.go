package parser

import (
	"encoding/json"
	"errors"

	"github.com/Azure/adx-mon/collector/logs/types"
)

var (
	ErrNotString = errors.New("no string in log body")
	ErrNotJson   = errors.New("no json object in log body")
)

const ParserTypeJson ParserType = "json"

type JsonParserConfig struct {
}

type JsonParser struct {
	parsed map[string]interface{}
}

func NewJsonParser(config JsonParserConfig) (*JsonParser, error) {
	return &JsonParser{parsed: make(map[string]interface{})}, nil
}

// Attempts to parse the message as JSON and adds the parsed fields to the log body.
// Not safe for concurrent use.
func (p *JsonParser) Parse(log *types.Log, msg string) error {
	if len(msg) == 0 || msg[0] != '{' {
		return ErrNotJson
	}

	clear(p.parsed)
	if err := json.Unmarshal([]byte(msg), &p.parsed); err != nil {
		return ErrNotJson
	}

	for k, v := range p.parsed {
		log.Body[k] = v
	}

	return nil
}
