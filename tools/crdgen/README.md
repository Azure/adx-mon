# CEL Validation Automation

This directory contains tools to automatically apply CEL (Common Expression Language) validations to generated CRDs.

## Overview

The `patch_cel_validations.py` script automatically extracts CEL validation rules from kubebuilder annotations in Go struct fields and applies them to the corresponding CRD YAML files.

## How it works

1. **Extraction**: The script parses Go files in `api/v1/` looking for `+kubebuilder:validation:XValidation` annotations
2. **Mapping**: It maps the Go struct field names to their JSON field names using the `json:` tags
3. **Patching**: It adds the corresponding `x-kubernetes-validations` entries to the CRD YAML files

## Usage

### Automatic (integrated into CRD generation)
The CEL validation patching is automatically applied when using `make generate-crd`:

```bash
make generate-crd CMD=update
```

### Manual (standalone)
To apply CEL validations to existing CRDs without regenerating them:

```bash
make patch-cel-validations
```

Or run the script directly:

```bash
python3 tools/crdgen/patch_cel_validations.py api/v1 kustomize/bases
python3 tools/crdgen/patch_cel_validations.py api/v1 operator/manifests/crds
```

## Adding new validations

To add a CEL validation to a field:

1. Add the kubebuilder annotation above the field in the Go struct:
   ```go
   // +kubebuilder:validation:XValidation:rule="duration(self) >= duration('1m')",message="interval must be at least 1 minute"
   Interval metav1.Duration `json:"interval"`
   ```

2. Run `make generate-crd CMD=update` or `make patch-cel-validations`

The validation will be automatically applied to both `kustomize/bases/` and `operator/manifests/crds/` CRD files.

## Example

Given this Go struct field:
```go
// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('1m')",message="interval must be at least 1 minute"
Interval metav1.Duration `json:"interval"`
```

The script will add this to the CRD YAML:
```yaml
interval:
  description: Interval is the cadence at which the rule will be executed
  type: string
  x-kubernetes-validations:
  - rule: duration(self) >= duration('1m')
    message: interval must be at least 1 minute
```