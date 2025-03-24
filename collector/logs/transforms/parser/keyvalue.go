package parser

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/Azure/adx-mon/collector/logs/types"
)

const ParserTypeKeyValue ParserType = "keyvalue"

type KeyValueParserConfig struct{}

type KeyValueParser struct {
	parsed       map[string]interface{}
	tokens       []string
	currentToken strings.Builder
}

func NewKeyValueParser(config KeyValueParserConfig) (*KeyValueParser, error) {
	return &KeyValueParser{
		parsed:       make(map[string]interface{}),
		tokens:       make([]string, 0),
		currentToken: strings.Builder{},
	}, nil
}

// Parse parses a space-separated string of key-value pairs and standalone keys
// and adds them to the log.Body map.
func (p *KeyValueParser) Parse(log *types.Log, msg string) error {
	clear(p.parsed)

	msg = strings.TrimSpace(msg)
	if msg == "" {
		return nil
	}

	tokens := p.tokenize(msg)

	for _, token := range tokens {
		parts := strings.SplitN(token, "=", 2)
		if len(parts) == 2 {
			key, value := parts[0], unquote(parts[1])
			log.SetBodyValue(key, reflectValue(value))
		} else {
			log.SetBodyValue(parts[0], "")
		}
	}

	return nil
}

// tokenize splits a string into tokens, preserving quoted values
func (p *KeyValueParser) tokenize(s string) []string {
	// Reset tokens slice while preserving capacity
	p.tokens = p.tokens[:0]
	// Reset the current token
	p.currentToken.Reset()

	inQuotes := false
	var quoteChar rune

	for i, char := range s {
		switch {
		// Handle quote characters, toggling the inQuotes state
		case (char == '"' || char == '\'') && (i == 0 || s[i-1] != '\\'):
			if inQuotes && char == quoteChar {
				inQuotes = false // Closing quote found
			} else if !inQuotes {
				inQuotes = true  // Opening quote found
				quoteChar = char // Remember the type of quote used
			}
			p.currentToken.WriteRune(char)
		// Token boundary detected (whitespace outside quotes)
		case unicode.IsSpace(char) && !inQuotes:
			if p.currentToken.Len() > 0 {
				p.tokens = append(p.tokens, p.currentToken.String())
				p.currentToken.Reset()
			}
		// Regular character, append to current token
		default:
			p.currentToken.WriteRune(char)
		}
	}

	// Append the last token if present
	if p.currentToken.Len() > 0 {
		p.tokens = append(p.tokens, p.currentToken.String())
	}

	return p.tokens
}

// unquote removes surrounding quotes from a string, if present
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// reflectValue attempts to convert a string to an appropriate Go type
// Optimized to reduce allocations and improve performance
func reflectValue(value string) interface{} {
	if len(value) == 0 {
		return ""
	}

	// Fast path for boolean detection based on first character and length
	first := value[0]
	if (first == 't' || first == 'T') && len(value) == 4 {
		if strings.EqualFold(value, "true") {
			return true
		}
	} else if (first == 'f' || first == 'F') && len(value) == 5 {
		if strings.EqualFold(value, "false") {
			return false
		}
	}

	// Quickly determine if the string represents a numeric value
	isNumber := true
	isFloat := false

	for i := 0; i < len(value); i++ {
		c := value[i]
		// Allow leading sign characters
		if i == 0 && (c == '+' || c == '-') {
			continue
		}
		// Check for decimal point to identify floats
		if c == '.' {
			if isFloat { // Multiple decimal points invalidate number
				isNumber = false
				break
			}
			isFloat = true
			continue
		}
		// Non-digit character invalidates number
		if c < '0' || c > '9' {
			isNumber = false
			break
		}
	}

	// Attempt numeric conversion if valid number detected
	if isNumber {
		if isFloat {
			if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
				return floatVal
			}
		} else {
			if intVal, err := strconv.Atoi(value); err == nil {
				return intVal
			}
		}
	}

	// Default to returning the original string if no conversion succeeded
	return value
}
