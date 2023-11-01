package rules

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type fileStore struct {
	rules []*Rule
}

func FromPath(path, region string) (*fileStore, error) {
	s := &fileStore{}
	// walk files in directory
	err := filepath.WalkDir(path, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open file '%s': %w", path, err)
		}
		defer f.Close()
		logger.Infof("reading file %q", path)
		err = s.fromStream(f, region)
		if err != nil {
			return fmt.Errorf("failed to read file '%s': %w", path, err)
		}
		return nil
	})
	return s, err

}

func (s *fileStore) fromStream(file io.Reader, region string) error {
	contents, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("fromStream failed to read file: %w", err)
	}

	chunks := bytes.Split(contents, []byte("---"))
	for _, chunk := range chunks {
		whitespaceTrimmed := strings.TrimSpace(string(chunk))
		if len(whitespaceTrimmed) == 0 {
			continue
		}

		genericStructure := make(map[string]interface{})
		err = yaml.Unmarshal(chunk, &genericStructure)
		if err != nil {
			return fmt.Errorf("fromStream failed to unmarshal yaml: %w", err)
		}
		// check if this is a rule
		kind, ok := genericStructure["kind"]
		if !ok || kind != "AlertRule" {
			logger.Warn("found non-rule yaml, skipping")
			continue
		}

		// create new spec here
		rule := alertrulev1.AlertRule{}

		// pass a reference to spec reference
		err = yaml.UnmarshalStrict(chunk, &rule)
		if err != nil {
			return fmt.Errorf("fromStream failed to unmarshal yaml: %w", err)
		}

		logger.Infof("found rule %q", rule.ObjectMeta.Name)
		r, err := toRule(rule, region)
		if err != nil {
			return fmt.Errorf("fromStream failed to convert rule: %w", err)
		}
		s.rules = append(s.rules, r)
	}
	return nil
}

func (s *fileStore) Rules() []*Rule {
	return s.rules
}
