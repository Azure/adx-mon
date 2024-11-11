package log

import (
	"fmt"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

type Logger struct {
	T *testing.T
}

func (tcl Logger) Printf(format string, v ...interface{}) {
	line := strings.TrimSpace(fmt.Sprintf(format, v...))
	tcl.T.Log(line)
}

func (tcl Logger) Accept(log testcontainers.Log) {
	line := strings.TrimSpace(string(log.Content))
	if len(line) == 0 {
		return // empty lines are just junk..
	}
	tcl.T.Logf("[%s,testcontainer] %s", log.LogType, line)
}
