package adx

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Planner determines the rollup execution order or tables to rollup.
type Planner struct {
	mu        sync.RWMutex
	summaries []*Summary
	plan      []*Summary
}

func (p *Planner) AddSummary(s *Summary) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.plan = p.plan[:0]
	p.summaries = append(p.summaries, s)
}

// Plan recalculates the rollup plan based on the managed summaries.
func (p *Planner) Plan() []*Summary {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.plan) > 0 {
		return p.plan
	}

	sort.Slice(p.summaries, func(i, j int) bool {
		return p.summaries[i].Lag() > p.summaries[j].Lag()
	})

	p.plan = p.plan[:0]
	for _, v := range p.summaries {
		if v.WindowReady() {
			p.plan = append(p.plan, v)
		}
	}
	return p.plan
}

// Size returns the size of current plan.
func (p *Planner) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.plan)
}

func (p *Planner) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.plan = p.plan[:0]
	p.summaries = p.summaries[:0]
}

type Summary struct {
	Interval              time.Duration
	RawTable, RollupTable string
	MinRawTime, MaxRawTime,
	MinRollupTime, MaxRollupTime time.Time
}

func NewSummary(table string, interval time.Duration) *Summary {
	return &Summary{
		Interval:    interval,
		RawTable:    table,
		RollupTable: fmt.Sprintf("%s1m", table),
	}
}

// NextWindow returns the start and end time of the next interval that can be rolled up.
func (s *Summary) NextWindow() (time.Time, time.Time) {
	if !s.WindowReady() {
		return time.Time{}, time.Time{}
	}

	// If the MaxRollupTime is before we the raw data, clamp to the lower interval of the min raw time.
	if s.MaxRollupTime.Before(s.MinRawTime) {
		start := s.MinRawTime.Truncate(time.Hour)
		return start, start.Add(s.Interval)
	}

	return s.MaxRollupTime, s.MaxRollupTime.Add(s.Interval)
}

// WindowReady returns true if the next window is ready for rollup.
func (s *Summary) WindowReady() bool {
	return s.MaxRawTime.Sub(s.MaxRollupTime) >= s.Interval
}

// Lag is the difference in time from the last rollup time to last raw time.
func (s *Summary) Lag() time.Duration {
	return s.MaxRawTime.Sub(s.MaxRollupTime)
}
