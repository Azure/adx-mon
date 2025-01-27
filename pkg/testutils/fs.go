package testutils

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/strings/slices"
	goyaml "sigs.k8s.io/yaml/goyaml.v2"
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

func CopyFile(src, dst string) error {
	// Open the source file
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Create the destination file
	destinationFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destinationFile.Close()

	// Copy the contents from source to destination
	_, err = io.Copy(destinationFile, sourceFile)
	if err != nil {
		return err
	}

	// Flush the contents to the destination file
	err = destinationFile.Sync()
	if err != nil {
		return err
	}

	return nil
}

// RelativePath attempts to find artifact by walking up the directory tree backwards
func RelativePath(artifact string) (string, bool) {
	var artifactPath string
	for iter := range 4 {
		relative := strings.Repeat("../", iter)
		artifactPath = filepath.Join(relative, artifact)
		if _, err := os.Stat(artifactPath); err == nil {
			break
		}
	}
	return artifactPath, artifactPath != ""
}

// ExtractManifests is meant to read in a concatenated manifest file and
// extract the individual manifest described by the isolateKind and write
// it to the to path.
//
// to: directory
// isolateKinds: kinds of manifests to extract
// from: path to concatenated manifest file, should be from the root, not relative. example - build/k8s/ingestor.yaml
//
// to will contain two files:
// - manifests.yaml: all manifests except for the isolateKind
// - {isolateKind}.yaml: the manifest described by the isolateKind
func ExtractManifests(to, from string, isolateKinds []string) error {
	manifestPath, ok := RelativePath(from)
	if !ok {
		return fmt.Errorf("failed to find manifest path")
	}

	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}

	manifests := filepath.Join(to, "manifests.yaml")
	decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(string(manifestData)), 4096)
	for {
		var manifest map[string]interface{}
		if err := decoder.Decode(&manifest); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to decode manifest: %w", err)
		}
		manifestBytes, err := goyaml.Marshal(manifest)
		if err != nil {
			return fmt.Errorf("failed to marshal manifest: %w", err)
		}

		isolateKind := manifest["kind"].(string)
		if slices.Contains(isolateKinds, isolateKind) {
			if err := os.WriteFile(filepath.Join(to, isolateKind+".yaml"), manifestBytes, 0644); err != nil {
				return fmt.Errorf("failed to write manifest: %w", err)
			}
		} else {
			f, err := os.OpenFile(manifests, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("failed to open file: %w", err)
			}
			defer f.Close()
			if _, err := f.Write(manifestBytes); err != nil {
				return fmt.Errorf("failed to write manifest: %w", err)
			}
			if _, err := f.WriteString("\n---\n"); err != nil {
				return fmt.Errorf("failed to write manifest: %w", err)
			}
		}
	}

	return nil
}
