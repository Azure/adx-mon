package main_test

import (
	"os/exec"
	"testing"

	"github.com/Azure/adx-mon/cmd/collector/config"
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
