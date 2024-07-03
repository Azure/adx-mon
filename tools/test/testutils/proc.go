package testutils

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func Run(cmd *exec.Cmd) ([]byte, error) {
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s failed with error: (%v) %s", command, err, string(output))
	}

	return output, nil
}
