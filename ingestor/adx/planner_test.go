package adx

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSummary_NextWindow(t *testing.T) {
	s := NewSummary("RawTable", time.Hour)
	s.MinRawTime = time.Unix(0, 0)
	s.MaxRawTime = time.Unix(60, 0)
	s.MinRollupTime = time.Unix(0, 0)
	s.MaxRollupTime = time.Unix(0, 0)

	require.False(t, s.WindowReady())

	s.MaxRawTime = time.Unix(3600, 0)
	require.True(t, s.WindowReady())

	winStart, winEnd := s.NextWindow()
	require.Equal(t, time.Unix(0, 0), winStart)
	require.Equal(t, time.Unix(3600, 0), winEnd)

	s.MaxRawTime = time.Unix(3601, 0)
	s.MaxRollupTime = time.Unix(3600, 0)

	require.False(t, s.WindowReady())
	s.MaxRawTime = time.Unix(7200, 0)

	require.True(t, s.WindowReady())
	winStart, winEnd = s.NextWindow()
	require.Equal(t, time.Unix(3600, 0), winStart)
	require.Equal(t, time.Unix(7200, 0), winEnd)
}

func TestSummary_NextWindow_Bucket(t *testing.T) {
	s := NewSummary("RawTable", time.Hour)
	s.MinRawTime = time.Unix(3601, 0)
	s.MaxRawTime = time.Unix(7500, 0)
	s.MinRollupTime = time.Unix(0, 0)
	s.MaxRollupTime = time.Unix(0, 0)

	require.True(t, s.WindowReady())
	winStart, winEnd := s.NextWindow()
	require.Equal(t, time.Unix(3600, 0), winStart)
	require.Equal(t, time.Unix(7200, 0), winEnd)
}

func TestPlanner_Plan(t *testing.T) {
	s := NewSummary("RawTable", time.Hour)
	s.MinRawTime = time.Unix(0, 0)
	s.MaxRawTime = time.Unix(60, 0)
	s.MinRollupTime = time.Unix(0, 0)
	s.MaxRollupTime = time.Unix(0, 0)

	s1 := NewSummary("RawTable2", time.Hour)
	s1.MinRawTime = time.Unix(0, 0)
	s1.MaxRawTime = time.Unix(120, 0)
	s1.MinRollupTime = time.Unix(0, 0)
	s1.MaxRollupTime = time.Unix(0, 0)

	p := &Planner{}
	p.AddSummary(s)
	p.AddSummary(s1)
	next := p.Plan()

	require.Equal(t, 0, len(next))

	s1.MaxRawTime = time.Unix(3600, 0)
	next = p.Plan()

	require.NotNil(t, next)
	require.Equal(t, 1, len(next))

	require.Equal(t, "RawTable2", next[0].RawTable)
}
