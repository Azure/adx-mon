package partial

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func counterValue(c prometheus.Counter) float64 {
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		panic(err)
	}
	return m.GetCounter().GetValue()
}

func TestAssemblyLimitsAndReporting(t *testing.T) {
	a := New("cri")
	now := time.Unix(1, 0)
	sizes := metrics.LogsCollectorPartialMessagesDropped.WithLabelValues("cri", "size")
	before := counterValue(sizes)
	_, pending := a.Add("stdout", strings.Repeat("a", MaxMessageBytes), true, now)
	require.True(t, pending)
	require.Equal(t, MaxMessageBytes, a.RetainedBytes())
	_, pending = a.Add("stdout", "overflow", true, now)
	require.True(t, pending)
	require.Zero(t, a.RetainedBytes())
	_, pending = a.Add("stdout", "final", false, now)
	require.True(t, pending)
	require.Zero(t, a.Len())
	require.Equal(t, before+1, counterValue(sizes), "one visible size drop per assembly")

	for i := range MaxStreams + 50 {
		a.Add(fmt.Sprint(i), "x", true, now)
		require.LessOrEqual(t, a.Len(), MaxStreams)
	}
	a.Reset()
	for i := range 20 {
		a.Add(fmt.Sprint(i), strings.Repeat("x", MaxMessageBytes), true, now)
		require.LessOrEqual(t, a.RetainedBytes(), MaxRetainedBytes)
	}
	a.Expire(now.Add(IdleTimeout + SweepInterval))
	require.Zero(t, a.RetainedBytes())
	require.Zero(t, a.Len())
}

func TestAssemblyTimeoutAndRecovery(t *testing.T) {
	a := New("journald")
	now := time.Unix(1, 0)
	timeouts := metrics.LogsCollectorPartialMessagesDropped.WithLabelValues("journald", "timeout")
	before := counterValue(timeouts)
	a.Add("abandoned", "old ", true, now)
	a.Expire(now.Add(IdleTimeout - SweepInterval))
	require.Equal(t, 1, a.Len())
	a.Expire(now.Add(IdleTimeout))
	require.Zero(t, a.Len())
	require.Zero(t, a.RetainedBytes())
	require.Equal(t, before+1, counterValue(timeouts))
	a.Add("abandoned", "new ", true, now.Add(IdleTimeout))
	msg, pending := a.Add("abandoned", "message", false, now.Add(IdleTimeout))
	require.False(t, pending)
	require.Equal(t, "new message", msg)
	// The ID is bounded independently of payload length.
	a.Add(strings.Repeat("s", MaxStreamIDBytes+1), "x", true, now)
	require.Zero(t, a.Len())
	// Complete messages have no idle ownership and keep their original behavior.
	msg, pending = a.Add("complete", "unsplit", false, now)
	require.False(t, pending)
	require.Equal(t, "unsplit", msg)
}
