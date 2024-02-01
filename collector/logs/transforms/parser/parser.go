package parser

import (
	"fmt"

	"github.com/Azure/adx-mon/collector/logs/types"
)

// Parser is the interface for parsing log messages.
type Parser interface {
	Parse(*types.Log) error
}

type ParserType string

// ParserConfig is a structure used in configs for creating parsers instances.
type ParserConfig struct {
	Type ParserType
}

// NewParser creates a new parser instance.
func NewParser(parserType ParserType) (Parser, error) {
	switch parserType {
	case ParserTypeJson:
		return NewJsonParser(JsonParserConfig{})
	default:
		return nil, fmt.Errorf("unknown parser type: %s", parserType)
	}
}

func NewParsers(parserTypes []string) ([]Parser, error) {
	parsers := make([]Parser, len(parserTypes))
	for i, parserType := range parserTypes {
		parser, err := NewParser(ParserType(parserType))
		if err != nil {
			return nil, err
		}
		parsers[i] = parser
	}
	return parsers, nil
}

func IsValidParser(parserType string) bool {
	switch parserType {
	case string(ParserTypeJson):
		return true
	default:
		return false
	}
}
