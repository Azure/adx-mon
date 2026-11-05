package main

import (
	"io"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testConfig(t *testing.T) config {
	t.Helper()
	c, err := parseConfig(nil, io.Discard)
	require.NoError(t, err)
	return c
}

func TestConfig(t *testing.T) {
	c := testConfig(t)
	require.Equal(t, "127.0.0.1:9090", c.listenAddr)
	require.Equal(t, 1.0, c.acceptRate)
	require.Zero(t, c.disconnectRate)
	require.Zero(t, c.responseDelay)
	require.Equal(t, "5", c.retrySeconds())
	for _, args := range [][]string{
		{"-accept-rate", "-0.1"}, {"-accept-rate", "1.1"}, {"-accept-rate", "NaN"}, {"-accept-rate", "+Inf"},
		{"-disconnect-rate", "-0.1"}, {"-disconnect-rate", "1.1"}, {"-disconnect-rate", "NaN"}, {"-disconnect-rate", "-Inf"},
		{"-accept-rate", "0.9", "-disconnect-rate", "0.2"},
		{"-overload-scope", "invalid"}, {"-retry-after", "0"}, {"-retry-after", "-1s"},
		{"-response-delay", "-1ns"}, {"-stats-interval", "0"}, {"-shutdown-timeout", "-1s"},
		{"-listen-addr", "localhost"}, {"-listen-addr", ":-1"}, {"-listen-addr", ":65536"}, {"-listen-addr", ":http"},
		{"-unknown"}, {"extra"},
	} {
		t.Run(args[0]+"/"+args[len(args)-1], func(t *testing.T) {
			_, err := parseConfig(args, io.Discard)
			require.Error(t, err)
		})
	}
	for _, tc := range []struct {
		duration time.Duration
		seconds  string
	}{{time.Nanosecond, "1"}, {time.Second, "1"}, {time.Second + time.Nanosecond, "2"}, {time.Duration(math.MaxInt64), "9223372037"}} {
		c.retryAfter = tc.duration
		require.Equal(t, tc.seconds, c.retrySeconds())
	}
	c, err := parseConfig([]string{"-listen-addr", "[::1]:0", "-accept-rate", "0", "-disconnect-rate", "1", "-seed", "42", "-retry-after", "1ns", "-verbose"}, io.Discard)
	require.NoError(t, err)
	require.EqualValues(t, 42, c.seed)
	require.True(t, c.verbose)
}

func TestPolicyBoundariesAndReproducibility(t *testing.T) {
	for _, tc := range []struct {
		x, acceptRate, disconnectRate float64
		want                          outcome
	}{
		{0, 0, 0, reject}, {.999999, 1, 0, accept}, {0, 0, 1, disconnect},
		{.499999, .5, .25, accept}, {.5, .5, .25, disconnect}, {.749999, .5, .25, disconnect}, {.75, .5, .25, reject},
	} {
		require.Equal(t, tc.want, chooseOutcome(tc.x, tc.acceptRate, tc.disconnectRate))
	}
	c := testConfig(t)
	c.acceptRate, c.disconnectRate, c.seed, c.responseDelay = .5, .25, 42, time.Millisecond
	a, b := newRandomPolicy(c), newRandomPolicy(c)
	counts := make(map[outcome]int)
	for range 1000 {
		d := a.decide(metadata{})
		require.Equal(t, b.decide(metadata{}), d)
		require.Equal(t, time.Millisecond, d.delay)
		counts[d.outcome]++
	}
	for _, o := range []outcome{accept, reject, disconnect} {
		require.Positive(t, counts[o])
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 1000 {
				a.decide(metadata{})
			}
		})
	}
	wg.Wait()
}
