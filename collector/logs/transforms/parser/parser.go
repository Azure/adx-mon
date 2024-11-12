package parser

import (
	"fmt"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
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

// newParser creates a new parser instance.
func newParser(parserType ParserType) (Parser, error) {
	switch parserType {
	case ParserTypeJson:
		return NewJsonParser(JsonParserConfig{})
	default:
		return nil, fmt.Errorf("unknown parser type: %s", parserType)
	}
}

// NewParsers creates a list of valid parser instances.
// Invalid parsers or those that cannot be created will be skipped with warning logs including the source string.
func NewParsers(parserTypes []string, source string) []Parser {
	parsers := make([]Parser, 0, len(parserTypes))
	for _, parserType := range parserTypes {
		parser, err := newParser(ParserType(parserType))
		if err != nil {
			logger.Warnf("Failed to create parser %s for %s: %v", parserType, source, err)
			continue
		}
		parsers = append(parsers, parser)
	}
	return parsers
}

func IsValidParser(parserType string) bool {
	switch parserType {
	case string(ParserTypeJson):
		return true
	default:
		return false
	}
}
