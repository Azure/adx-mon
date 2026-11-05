package parser

import (
	jsonv1 "encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"strings"

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
	parsed map[string]any
	reader strings.Reader
}

func NewJsonParser(config JsonParserConfig) (*JsonParser, error) {
	return &JsonParser{parsed: make(map[string]any)}, nil
}

// Attempts to parse the message as JSON and adds the parsed fields to the log body.
// Not safe for concurrent use.
func (p *JsonParser) Parse(log *types.Log, msg string) error {
	if len(msg) == 0 || msg[0] != '{' {
		return ErrNotJson
	}

	clear(p.parsed)
	p.reader.Reset(msg)
	// Preserve legacy values, but avoid the error-compatibility prescan: failed
	// parses are never published, and callers only receive ErrNotJson.
	err := jsonv2.UnmarshalRead(&p.reader, &p.parsed,
		jsonv1.DefaultOptionsV1(), jsonv1.ReportErrorsWithLegacySemantics(false))
	p.reader.Reset("")
	if err != nil {
		return ErrNotJson
	}

	for k, v := range p.parsed {
		log.SetBodyValue(k, v)
	}

	return nil
}
