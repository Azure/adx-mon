// Package partial bounds incomplete log messages. Assemblers are owned by their
// reader goroutine; that owner must also call Expire during quiet periods.
package partial

import (
	"strings"
	"time"

	"github.com/Azure/adx-mon/metrics"
)

const (
	MaxMessageBytes  = 1 << 20
	MaxRetainedBytes = 8 << 20
	MaxStreams       = 128
	MaxStreamIDBytes = 256
	IdleTimeout      = time.Minute
	SweepInterval    = time.Second
)

type message struct {
	text     string
	updated  time.Time
	dropping bool
}

type Assembler struct {
	source    string
	streams   map[string]message
	bytes     int
	nextSweep time.Time
}

func New(source string) Assembler { return Assembler{source: source} }

func (a *Assembler) report(reason string) {
	metrics.LogsCollectorPartialMessagesDropped.WithLabelValues(a.source, reason).Inc()
}

// Add returns pending=true for buffered or dropped messages. An oversized
// assembly is dropped through its final fragment, using a zero-payload marker.
// Expiry/capacity eviction discards the incomplete prefix; subsequent fragments
// start fresh, since these formats provide no message ID to recover its boundary.
// Complete, unsplit messages are passed through without retaining their payload.
func (a *Assembler) Add(stream, fragment string, partial bool, now time.Time) (string, bool) {
	a.Expire(now)
	if len(stream) > MaxStreamIDBytes {
		a.report("stream_id")
		return "", true
	}
	m, exists := a.streams[stream]
	if !exists && !partial {
		return fragment, false
	}
	if !exists {
		if a.streams == nil {
			a.streams = make(map[string]message)
		}
		if len(a.streams) == MaxStreams {
			a.evictOldest()
		}
	}
	// CRI fields are substrings of an entire input line. Map updates can also
	// replace an equal key, so clone on updates as well as first insertion.
	stream = strings.Clone(stream)
	if m.dropping {
		if partial {
			m.updated = now
			a.streams[stream] = m
		} else {
			delete(a.streams, stream)
		}
		return "", true
	}
	if len(m.text)+len(fragment) > MaxMessageBytes || a.bytes+len(fragment) > MaxRetainedBytes {
		reason := "size"
		if len(m.text)+len(fragment) <= MaxMessageBytes {
			reason = "capacity"
		}
		a.report(reason)
		a.bytes -= len(m.text)
		if partial {
			a.streams[stream] = message{updated: now, dropping: true}
		} else {
			delete(a.streams, stream)
		}
		return "", true
	}
	if !partial {
		delete(a.streams, stream)
		a.bytes -= len(m.text)
		return m.text + fragment, false
	}
	if m.text == "" {
		// Do not retain the rest of the source line's allocation.
		m.text = strings.Clone(fragment)
	} else {
		m.text += fragment
	}
	m.updated = now
	a.bytes += len(fragment)
	a.streams[stream] = m
	return "", true
}

func (a *Assembler) evictOldest() {
	var key string
	var oldest time.Time
	for k, m := range a.streams {
		if oldest.IsZero() || m.updated.Before(oldest) || m.updated.Equal(oldest) && k < key {
			key, oldest = k, m.updated
		}
	}
	m := a.streams[key]
	if !m.dropping {
		a.report("capacity")
	}
	a.bytes -= len(m.text)
	delete(a.streams, key)
}

// Expire is bounded by MaxStreams. Rate limiting the scan also keeps the normal
// complete-message path cheap. The owning reader schedules calls every second.
func (a *Assembler) Expire(now time.Time) {
	if now.Before(a.nextSweep) {
		return
	}
	a.nextSweep = now.Add(SweepInterval)
	for k, m := range a.streams {
		if now.Sub(m.updated) >= IdleTimeout {
			if !m.dropping {
				a.report("timeout")
			}
			a.bytes -= len(m.text)
			delete(a.streams, k)
		}
	}
}

func (a *Assembler) Len() int           { return len(a.streams) }
func (a *Assembler) RetainedBytes() int { return a.bytes }

// Reset is used at reader discontinuities, where fragments cannot be joined.
func (a *Assembler) Reset() int {
	n := len(a.streams)
	a.streams, a.bytes = nil, 0
	return n
}
