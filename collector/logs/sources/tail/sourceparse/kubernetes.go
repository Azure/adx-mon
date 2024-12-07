package sourceparse

import "github.com/Azure/adx-mon/collector/logs/types"

// KubernetesParser is a parser for Kubernetes logs.
// Discovers the log type and parses with the docker or cri parser.
// Not safe for concurrent use.
type KubernetesParser struct {
	parser LogTypeParser
}

// NewKubernetesParser creates a new KubernetesParser.
func NewKubernetesParser() *KubernetesParser {
	return &KubernetesParser{}
}

// Parse parses a line of text from a file into a log.
func (p *KubernetesParser) Parse(line string, log *types.Log) (message string, isPartial bool, err error) {
	if p.parser == nil {
		if isDockerLog(line) {
			p.parser = NewDockerParser()
		} else {
			p.parser = NewCriParser()
		}
	}

	return p.parser.Parse(line, log)
}

func isDockerLog(line string) bool {
	return line[0] == '{'
}
