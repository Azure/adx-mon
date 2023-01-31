package rules

import (
	"github.com/prometheus/client_golang/prometheus"
	"testing"
)

func TestRules(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	if err := VerifyRules("some-region"); err != nil {
		t.Fatal(err)
	}
}
