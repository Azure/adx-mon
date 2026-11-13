package tail

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
	"github.com/tenebris-tech/tail"
)

type expiryParser struct {
	expirations int
	partial     bool
}

func (p *expiryParser) Parse(string, *types.Log) (string, bool, error) { return "", p.partial, nil }
func (p *expiryParser) ExpirePartials(time.Time)                       { p.expirations++ }

func TestReadLinesExpiresPartialsWhileQuiet(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		p := &expiryParser{}
		lines := make(chan *tail.Line)
		tailer := &Tailer{tail: &tail.Tail{Lines: lines}, logTypeParser: p}
		done := make(chan struct{})
		go func() { defer close(done); readLines(ctx, tailer, nil, make(chan *types.Log)) }()
		time.Sleep(2 * time.Minute)
		cancel()
		<-done
		require.Positive(t, p.expirations, "tail owner must sweep even when no lines arrive")
	})
}

func TestReadLinesExpiresPartialsWhileOutputBlocked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		p := &expiryParser{}
		lines := make(chan *tail.Line, 1)
		lines <- &tail.Line{Text: "complete"}
		tailer := &Tailer{tail: &tail.Tail{Lines: lines}, logTypeParser: p}
		done := make(chan struct{})
		go func() { defer close(done); readLines(ctx, tailer, nil, make(chan *types.Log)) }()
		time.Sleep(2 * time.Minute)
		cancel()
		<-done
		require.Positive(t, p.expirations)
	})
}
