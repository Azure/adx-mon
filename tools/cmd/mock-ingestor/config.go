package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"time"
)

type config struct {
	listenAddr      string
	acceptRate      float64
	disconnectRate  float64
	overloadScope   string
	retryAfter      time.Duration
	responseDelay   time.Duration
	seed            int64
	statsInterval   time.Duration
	shutdownTimeout time.Duration
	verbose         bool
}

func parseConfig(args []string, output io.Writer) (config, error) {
	c := config{}
	f := flag.NewFlagSet("mock-ingestor", flag.ContinueOnError)
	f.SetOutput(output)
	f.StringVar(&c.listenAddr, "listen-addr", "127.0.0.1:9090", "Plain HTTP/1.1 listen address (local use only)")
	f.Float64Var(&c.acceptRate, "accept-rate", 1, "Per-attempt acceptance probability [0,1]")
	f.Float64Var(&c.disconnectRate, "disconnect-rate", 0, "Per-attempt abrupt disconnect probability [0,1]; sum with accept-rate must be <= 1")
	f.StringVar(&c.overloadScope, "overload-scope", "instance", "Overload scope: instance or table")
	f.DurationVar(&c.retryAfter, "retry-after", 5*time.Second, "Positive overload retry duration, rounded up to seconds (client table retry clamps to 300s)")
	f.DurationVar(&c.responseDelay, "response-delay", 0, "Delay after reading accept/reject bodies; does not throttle uploads")
	f.Int64Var(&c.seed, "seed", time.Now().UnixNano(), "Random policy seed (logged at startup)")
	f.DurationVar(&c.statsInterval, "stats-interval", 10*time.Second, "Positive interval between transfer summaries")
	f.DurationVar(&c.shutdownTimeout, "shutdown-timeout", 5*time.Second, "Positive HTTP shutdown deadline")
	f.BoolVar(&c.verbose, "verbose", false, "Log each valid transfer's filename and request ID (may reduce throughput)")
	if err := f.Parse(args); err != nil {
		return c, err
	}
	if f.NArg() != 0 {
		return c, fmt.Errorf("unexpected positional arguments: %v", f.Args())
	}
	return c, c.validate()
}

func (c config) validate() error {
	for name, rate := range map[string]float64{"accept-rate": c.acceptRate, "disconnect-rate": c.disconnectRate} {
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 || rate > 1 {
			return fmt.Errorf("%s must be finite and between 0 and 1", name)
		}
	}
	if c.acceptRate+c.disconnectRate > 1 {
		return fmt.Errorf("accept-rate + disconnect-rate must be <= 1")
	}
	if c.overloadScope != "instance" && c.overloadScope != "table" {
		return fmt.Errorf("overload-scope must be instance or table")
	}
	if c.retryAfter <= 0 || c.responseDelay < 0 || c.statsInterval <= 0 || c.shutdownTimeout <= 0 {
		return fmt.Errorf("retry-after, stats-interval, and shutdown-timeout must be positive; response-delay must be nonnegative")
	}
	_, port, err := net.SplitHostPort(c.listenAddr)
	if err != nil {
		return fmt.Errorf("listen-addr: %w", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("listen-addr must have a numeric port between 0 and 65535")
	}
	return nil
}

func (c config) retrySeconds() string {
	// Avoid overflowing a duration when rounding values near MaxInt64.
	return strconv.FormatInt(int64((c.retryAfter-1)/time.Second+1), 10)
}
