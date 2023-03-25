package rules

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/logger"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type fileStore struct {
	rules []*Rule
}

func FromPath(path, region string) (*fileStore, error) {
	s := &fileStore{}
	//walk files in directory
	err := filepath.WalkDir(path, func(path string, info os.DirEntry, err error) error {
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open file '%s': %w", path, err)
		}
		err = s.fromStream(f, region)
		if err != nil {
			return fmt.Errorf("failed to read file '%s': %w", path, err)
		}
		return nil
	})
	return s, err

}

func (s *fileStore) fromStream(file io.Reader, region string) error {
	d := yaml.NewYAMLOrJSONDecoder(file, 4096)
	for {
		// create new spec here
		rule := alertrulev1.AlertRule{}

		// pass a reference to spec reference
		err := d.Decode(&rule)
		// break the loop in case of EOF
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		logger.Info("found rule '%s'", rule.ObjectMeta.Name)
		r, err := toRule(rule, region)
		if err != nil {
			return err
		}
		s.rules = append(s.rules, r)
	}
	return nil
}

func (s *fileStore) Rules() []*Rule {
	return s.rules
}
