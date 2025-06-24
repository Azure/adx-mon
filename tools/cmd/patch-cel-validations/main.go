package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"
)

// CELValidation represents a CEL validation rule
type CELValidation struct {
	Rule    string `yaml:"rule"`
	Message string `yaml:"message"`
}

// ValidationMap holds field name to validation mappings
type ValidationMap map[string]CELValidation

func main() {
	var (
		apiDir string
		crdDir string
	)

	flag.StringVar(&apiDir, "api-dir", "./api/v1", "Directory containing Go API files")
	flag.StringVar(&crdDir, "crd-dir", "./config/crd/bases", "Directory containing CRD YAML files")
	flag.Parse()

	fmt.Println("Extracting CEL validations from Go files...")

	validations, err := extractValidationsFromGoFiles(apiDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error extracting validations: %v\n", err)
		os.Exit(1)
	}

	if len(validations) == 0 {
		fmt.Println("No CEL validations found in Go files.")
		return
	}

	fmt.Printf("Total validations found: %d\n", len(validations))
	for field, val := range validations {
		fmt.Printf("  %s: %s\n", field, val.Rule)
	}

	fmt.Println("\nPatching CRD files...")
	err = patchCRDFiles(crdDir, validations)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error patching CRD files: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("CEL validation patching complete.")
}

// extractValidationsFromGoFiles extracts CEL validations from all .go files in the directory
func extractValidationsFromGoFiles(apiDir string) (ValidationMap, error) {
	validations := make(ValidationMap)

	files, err := filepath.Glob(filepath.Join(apiDir, "*.go"))
	if err != nil {
		return nil, fmt.Errorf("failed to list Go files: %w", err)
	}

	for _, file := range files {
		fileValidations, err := extractValidationsFromFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to extract validations from %s: %w", file, err)
		}

		for field, validation := range fileValidations {
			validations[field] = validation
		}

		if len(fileValidations) > 0 {
			fmt.Printf("Found %d validation(s) in %s\n", len(fileValidations), filepath.Base(file))
		}
	}

	return validations, nil
}

// extractValidationsFromFile extracts CEL validations from a single Go file
func extractValidationsFromFile(filename string) (ValidationMap, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	validations := make(ValidationMap)
	scanner := bufio.NewScanner(file)
	lines := []string{}

	// Read all lines first
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Pattern to match kubebuilder XValidation annotation
	xvalPattern := regexp.MustCompile(`^\s*//\s*\+kubebuilder:validation:XValidation:rule="([^"]+)",message="([^"]+)"`)

	for i, line := range lines {
		matches := xvalPattern.FindStringSubmatch(line)
		if matches != nil {
			rule := matches[1]
			message := matches[2]

			// Find the next non-comment line to get the field name
			for j := i + 1; j < len(lines); j++ {
				nextLine := strings.TrimSpace(lines[j])
				if !strings.HasPrefix(nextLine, "//") && nextLine != "" {
					// Extract field name from struct field definition
					fieldPattern := regexp.MustCompile(`(\w+)\s+[^` + "`" + `]+` + "`" + `json:"([^"]+)"`)
					fieldMatches := fieldPattern.FindStringSubmatch(nextLine)
					if fieldMatches != nil {
						jsonFieldName := fieldMatches[2]
						validations[jsonFieldName] = CELValidation{
							Rule:    rule,
							Message: message,
						}
					}
					break
				}
			}
		}
	}

	return validations, nil
}

// patchCRDFiles patches all CRD YAML files in the directory with CEL validations
func patchCRDFiles(crdDir string, validations ValidationMap) error {
	if len(validations) == 0 {
		return nil
	}

	files, err := filepath.Glob(filepath.Join(crdDir, "*.yaml"))
	if err != nil {
		return fmt.Errorf("failed to list YAML files: %w", err)
	}

	for _, file := range files {
		fmt.Printf("Patching %s\n", filepath.Base(file))
		err := patchCRDFile(file, validations)
		if err != nil {
			return fmt.Errorf("failed to patch %s: %w", file, err)
		}
	}

	return nil
}

// patchCRDFile patches a single CRD YAML file with CEL validations
func patchCRDFile(filename string, validations ValidationMap) error {
	// Read the YAML file
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	// Parse YAML into a generic structure
	var crd map[string]interface{}
	err = yaml.Unmarshal(data, &crd)
	if err != nil {
		return err
	}

	// Navigate to the spec properties in the CRD
	spec, ok := crd["spec"].(map[string]interface{})
	if !ok {
		return nil // No spec found
	}

	versions, ok := spec["versions"].([]interface{})
	if !ok {
		return nil // No versions found
	}

	for _, version := range versions {
		versionMap, ok := version.(map[string]interface{})
		if !ok {
			continue
		}

		schema, ok := versionMap["schema"].(map[string]interface{})
		if !ok {
			continue
		}

		openAPIV3Schema, ok := schema["openAPIV3Schema"].(map[string]interface{})
		if !ok {
			continue
		}

		properties, ok := openAPIV3Schema["properties"].(map[string]interface{})
		if !ok {
			continue
		}

		specProp, ok := properties["spec"].(map[string]interface{})
		if !ok {
			continue
		}

		specProperties, ok := specProp["properties"].(map[string]interface{})
		if !ok {
			continue
		}

		// Add validations to matching fields
		for fieldName, validation := range validations {
			fieldProp, ok := specProperties[fieldName].(map[string]interface{})
			if !ok {
				continue
			}

			// Initialize x-kubernetes-validations if it doesn't exist
			var kubernetesValidations []interface{}
			if existing, exists := fieldProp["x-kubernetes-validations"]; exists {
				kubernetesValidations, _ = existing.([]interface{})
			}

			// Check if this validation rule already exists
			ruleExists := false
			for _, existing := range kubernetesValidations {
				if existingMap, ok := existing.(map[string]interface{}); ok {
					if existingRule, ok := existingMap["rule"].(string); ok && existingRule == validation.Rule {
						ruleExists = true
						break
					}
				}
			}

			if !ruleExists {
				newValidation := map[string]interface{}{
					"rule":    validation.Rule,
					"message": validation.Message,
				}
				kubernetesValidations = append(kubernetesValidations, newValidation)
				fieldProp["x-kubernetes-validations"] = kubernetesValidations
			}
		}
	}

	// Write the modified CRD back to file
	modifiedData, err := yaml.Marshal(&crd)
	if err != nil {
		return err
	}

	return os.WriteFile(filename, modifiedData, 0644)
}
