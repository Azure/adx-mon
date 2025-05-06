package parser

import (
	"strings"

	"github.com/Azure/adx-mon/collector/logs/types"
)

const ParserTypeKlog ParserType = "klog"

type KlogParserConfig struct{}

type KlogParser struct{}

// NewKlogParser creates a parser for the klog format.
// https://kubernetes.io/docs/concepts/cluster-administration/system-logs
func NewKlogParser(config KlogParserConfig) (*KlogParser, error) {
	return &KlogParser{}, nil
}

func (p *KlogParser) Parse(log *types.Log, msg string) error {
	// Trim leading/trailing whitespace
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return nil
	}

	// Split the log into header, message, and key-value pairs
	headerEnd := strings.Index(msg, "]")
	if headerEnd == -1 {
		log.SetBodyValue("message", msg)
		return nil // Gracefully handle unstructured logs
	}

	header := strings.TrimSpace(msg[:headerEnd])
	messageAndPairs := strings.TrimSpace(msg[headerEnd+1:])

	// Extract metadata from the header
	fields := strings.Fields(header)
	if len(fields) >= 4 {
		log.SetBodyValue("timestamp", fields[1])
		filenameAndLine := strings.Split(fields[3], ":")
		if len(filenameAndLine) == 2 {
			log.SetBodyValue("filename", filenameAndLine[0])
			log.SetBodyValue("line_number", filenameAndLine[1])
		}
	}

	// Extract the message and key-value pairs
	messageParts := strings.SplitN(messageAndPairs, "\"", 3)
	if len(messageParts) < 3 {
		log.SetBodyValue("message", messageAndPairs)
		return nil // Gracefully handle unstructured logs
	}

	log.SetBodyValue("message", messageParts[1])

	// Parse key-value pairs
	pairs := strings.Fields(messageParts[2])
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			key := kv[0]
			value := unquoteKlog(kv[1])
			log.SetBodyValue(key, value)
		}
	}

	return nil
}

// unquoteKlog removes surrounding quotes from a string, if present
func unquoteKlog(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
