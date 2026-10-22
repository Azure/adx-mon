package main

import (
	"os/exec"
	"testing"

	"github.com/Azure/adx-mon/cmd/collector/config"
	"github.com/Azure/adx-mon/collector/metadata"
	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
)

func TestMainFunction_Config(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "config")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run main function: %v\nOutput: %s", err, string(output))
	}

	var fileConfig config.Config
	require.NoError(t, toml.Unmarshal(output, &fileConfig))
}

// Ensure that the value returned from newLogLabeler is nil when assigned to the interface type metadata.LogLabeler.
// Needed to avoid segfaults.
func TestNewLogLabelerNilKubeNode(t *testing.T) {
	var labeler metadata.LogLabeler = newLogLabeler(nil)
	require.Nil(t, labeler)
}

// Ensure that the value returned from newLogLabeler is nil when assigned to the interface type metadata.LogLabeler.
// Needed to avoid segfaults.
func TestNewMetricLabelerNilKubeNode(t *testing.T) {
	var labeler metadata.MetricLabeler = newMetricLabeler(nil)
	require.Nil(t, labeler)
}
