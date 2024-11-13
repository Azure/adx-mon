package testutils

import (
	"bytes"
	"os/exec"
	"strings"
)

func GetGitRootDir() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}
