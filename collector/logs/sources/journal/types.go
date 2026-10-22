package journal

import (
	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/transforms/parser"
)

type JournalTargetConfig struct {
	// Array of match strings, like "SYSLOG_IDENTIFIER=sshd"
	Matches  []string
	Database string
	Table    string
	// LogLineParsers is a list of parsers to apply to each log line.
	LogLineParsers []parser.Parser
	// JournalFields is a list of journal fields to include in the log.
	JournalFields []string
	// JournalPath is an optional directory to read journal files from. When
	// empty, the local system journal is opened, which is scoped to the
	// current machine ID. Set this to read a journal that belongs to a
	// different machine ID, such as a host journal mounted into a container.
	JournalPath string
}

type SourceConfig struct {
	Targets         []JournalTargetConfig
	CursorDirectory string
	WorkerCreator   engine.WorkerCreatorFunc
}
