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
}

func NewJsonParser(config JsonParserConfig) (*JsonParser, error) {
	return &JsonParser{}, nil
}

func (p *JsonParser) Parse(log *types.Log) error {
	msg, ok := log.Body[types.BodyKeyMessage].(string)
	if !ok {
		return ErrNotString
	}

	if msg[0] != '{' {
		return nil
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(msg), &parsed); err != nil {
		return ErrNotJson
	}

	for k, v := range parsed {
		log.Body[k] = v
	}

	return nil
}
