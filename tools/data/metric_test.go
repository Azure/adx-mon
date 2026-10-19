package data

import (
	"os/exec"
	"testing"
)

// TestMetricLiteralsAreKeyed fails when go vet reports composites warnings
// for this package, i.e. when a prompb.Label is built with unkeyed fields.
func TestMetricLiteralsAreKeyed(t *testing.T) {
	out, err := exec.Command("go", "vet", "-composites=true", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go vet reports problems in tools/data:\n%s", out)
	}
}
