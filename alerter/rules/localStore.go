package rules

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
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

	knownRules := map[string]bool{}
	for _, rule := range s.Rules() {
		key := rule.Namespace + "/" + rule.Name
		if knownRules[key] {
			return nil, fmt.Errorf("duplicate rule %s", key)
		}
		knownRules[key] = true
	}
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

		// validate the rule passes generic k8s metadata checks
		if err := validateMetadata(&rule.ObjectMeta); err != nil {
			return fmt.Errorf("fromStream failed to validate metadata: %w", err)
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

// validateMetadata validates the metadata of a Kubernetes object
func validateMetadata(meta *metav1.ObjectMeta) error {
	var allErrs field.ErrorList

	// Validate Name
	if errs := validation.IsDNS1123Subdomain(meta.Name); len(errs) > 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("metadata").Child("name"), meta.Name, strings.Join(errs, ", ")))
	}

	// Validate namespace
	if errs := validation.IsDNS1123Subdomain(meta.Namespace); len(errs) > 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("metadata").Child("namespace"), meta.Namespace, strings.Join(errs, ", ")))
	}

	// Validate labels
	if err := validateKeyValuePairs(meta.Labels, true); err != nil {
		allErrs = append(allErrs, field.Invalid(field.NewPath("metadata").Child("labels"), meta.Labels, err.Error()))
	}

	// Validate annotations
	if err := validateKeyValuePairs(meta.Annotations, false); err != nil {
		allErrs = append(allErrs, field.Invalid(field.NewPath("metadata").Child("annotations"), meta.Annotations, err.Error()))
	}

	if len(allErrs) > 0 {
		return allErrs.ToAggregate()
	}
	return nil
}

var (
	keyNameRegex   = regexp.MustCompile(`^[a-z0-9A-Z]([a-z0-9A-Z\-\._]*[a-z0-9A-Z])?$`)
	keyPrefixRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
)

// validateKey validates a Kubernetes key (prefix/name) for labels or annotations.
func validateKey(key string) error {
	// Split key into prefix and name
	parts := strings.SplitN(key, "/", 2)

	if len(parts) == 2 {
		prefix := parts[0]
		name := parts[1]

		// Validate prefix
		if len(prefix) > 253 {
			return fmt.Errorf("key prefix is too long: %d characters (max 253)", len(prefix))
		}
		if !keyPrefixRegex.MatchString(prefix) {
			return fmt.Errorf("invalid key prefix: %q", prefix)
		}

		// Validate name
		if len(name) > 63 {
			return fmt.Errorf("key name is too long: %d characters (max 63)", len(name))
		}
		if !keyNameRegex.MatchString(name) {
			return fmt.Errorf("invalid key name: %q", name)
		}
	} else if len(parts) == 1 {
		name := parts[0]

		// Validate name only (no prefix)
		if len(name) > 63 {
			return fmt.Errorf("key name is too long: %d characters (max 63)", len(name))
		}
		if !keyNameRegex.MatchString(name) {
			return fmt.Errorf("invalid key name: %q", name)
		}
	} else {
		return fmt.Errorf("key cannot be empty")
	}

	return nil
}

// validateValue validates a Kubernetes value for labels or annotations.
func validateValue(value string, isLabel bool) error {
	// Labels have strict length limits (max 63 characters), annotations do not.
	if isLabel {
		if len(value) > 63 {
			return fmt.Errorf("label value is too long: %d characters (max 63)", len(value))
		}
		// Validate characters for label values
		if !keyNameRegex.MatchString(value) {
			return fmt.Errorf("invalid label value: %q", value)
		}
	} else {
		// Annotation values have no specific length restrictions but must be valid UTF-8.
		if value == "" {
			return nil // Empty annotation values are valid
		}
	}

	return nil
}

// validateKeyValuePairs validates a map of key-value pairs for labels or annotations.
func validateKeyValuePairs(pairs map[string]string, isLabel bool) error {
	for key, value := range pairs {
		if err := validateKey(key); err != nil {
			return fmt.Errorf("invalid key %q: %w", key, err)
		}

		if err := validateValue(value, isLabel); err != nil {
			return fmt.Errorf("invalid value for key %q: %w", key, err)
		}
	}
	return nil
}
