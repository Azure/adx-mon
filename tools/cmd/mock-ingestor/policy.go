package main

import (
	"math/rand"
	"sync"
	"time"
)

type outcome string

const (
	accept     outcome = "accept"
	reject     outcome = "reject"
	disconnect outcome = "disconnect"
)

// Metadata is deliberately independent of HTTP execution. Future policies can
// use filenames/destinations or phases without changing the transfer handler.
type metadata struct {
	filename  string
	requestID string
}

type decision struct {
	outcome outcome
	delay   time.Duration
}

// Implementations must be safe for concurrent calls and return promptly.
type policy interface {
	decide(metadata) decision
}

type randomPolicy struct {
	mu             sync.Mutex
	rng            *rand.Rand
	acceptRate     float64
	disconnectRate float64
	delay          time.Duration
}

func newRandomPolicy(c config) *randomPolicy {
	return &randomPolicy{rng: rand.New(rand.NewSource(c.seed)), acceptRate: c.acceptRate, disconnectRate: c.disconnectRate, delay: c.responseDelay}
}

func (p *randomPolicy) decide(_ metadata) decision {
	p.mu.Lock()
	x := p.rng.Float64()
	p.mu.Unlock()
	return decision{outcome: chooseOutcome(x, p.acceptRate, p.disconnectRate), delay: p.delay}
}

func chooseOutcome(x, acceptRate, disconnectRate float64) outcome {
	if x < acceptRate {
		return accept
	}
	if x < acceptRate+disconnectRate {
		return disconnect
	}
	return reject
}
