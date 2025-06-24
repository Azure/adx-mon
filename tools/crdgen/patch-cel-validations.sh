#!/bin/bash

# Script to automatically patch generated CRDs with CEL validations
# This script parses Go struct annotations and adds corresponding x-kubernetes-validations to CRD YAML files

set -e

# Function to extract CEL validation rules from Go files
extract_cel_validations() {
    local go_file="$1"
    local output_file="$2"
    
    # Create temporary file for storing validation rules
    > "$output_file"
    
    # Parse Go file for kubebuilder XValidation annotations
    while IFS= read -r line; do
        if [[ "$line" =~ ^\s*//\s*\+kubebuilder:validation:XValidation:rule=\"([^\"]+)\",message=\"([^\"]+)\" ]]; then
            rule="${BASH_REMATCH[1]}"
            message="${BASH_REMATCH[2]}"
            
            # Get the field name from the next non-comment line
            field_line=""
            while IFS= read -r next_line; do
                if [[ ! "$next_line" =~ ^\s*// ]]; then
                    field_line="$next_line"
                    break
                fi
            done
            
            if [[ "$field_line" =~ ([A-Za-z]+)[[:space:]]+[^[:space:]]+[[:space:]]+\`json:\"([^\"]+)\" ]]; then
                field_name="${BASH_REMATCH[2]}"
                echo "$field_name|$rule|$message" >> "$output_file"
            fi
        fi
    done < "$go_file"
}

# Function to patch CRD YAML with CEL validations
patch_crd_yaml() {
    local crd_file="$1"
    local validations_file="$2"
    
    if [[ ! -f "$validations_file" || ! -s "$validations_file" ]]; then
        return 0
    fi
    
    # Create temporary file for modified CRD
    local temp_file=$(mktemp)
    
    # Process the CRD file
    python3 -c "
import yaml
import sys

def patch_crd_with_validations(crd_file, validations_file, output_file):
    # Read validations
    validations = {}
    with open(validations_file, 'r') as f:
        for line in f:
            line = line.strip()
            if line:
                parts = line.split('|', 2)
                if len(parts) == 3:
                    field_name, rule, message = parts
                    validations[field_name] = {'rule': rule, 'message': message}
    
    # Read and parse CRD YAML
    with open(crd_file, 'r') as f:
        crd = yaml.safe_load(f)
    
    # Navigate to properties in the CRD spec
    if 'spec' in crd and 'versions' in crd['spec']:
        for version in crd['spec']['versions']:
            if 'schema' in version and 'openAPIV3Schema' in version['schema']:
                properties = version['schema']['openAPIV3Schema'].get('properties', {})
                if 'spec' in properties and 'properties' in properties['spec']:
                    spec_properties = properties['spec']['properties']
                    
                    # Add validations to matching fields
                    for field_name, validation in validations.items():
                        if field_name in spec_properties:
                            if 'x-kubernetes-validations' not in spec_properties[field_name]:
                                spec_properties[field_name]['x-kubernetes-validations'] = []
                            
                            # Check if validation already exists
                            existing_rules = [v.get('rule') for v in spec_properties[field_name]['x-kubernetes-validations']]
                            if validation['rule'] not in existing_rules:
                                spec_properties[field_name]['x-kubernetes-validations'].append({
                                    'rule': validation['rule'],
                                    'message': validation['message']
                                })
    
    # Write modified CRD
    with open(output_file, 'w') as f:
        yaml.dump(crd, f, default_flow_style=False, sort_keys=False)

patch_crd_with_validations('$crd_file', '$validations_file', '$temp_file')
"
    
    # Replace original file with patched version
    mv "$temp_file" "$crd_file"
}

# Main function
main() {
    local api_dir="${1:-./api/v1}"
    local crd_dir="${2:-./config/crd/bases}"
    
    echo "Patching CRDs with CEL validations..."
    
    # Create temporary file for validations
    local validations_file=$(mktemp)
    
    # Extract validations from all Go files in api directory
    for go_file in "$api_dir"/*.go; do
        if [[ -f "$go_file" ]]; then
            extract_cel_validations "$go_file" "$validations_file"
        fi
    done
    
    # Patch all CRD files
    for crd_file in "$crd_dir"/*.yaml; do
        if [[ -f "$crd_file" ]]; then
            echo "Patching $(basename "$crd_file")"
            patch_crd_yaml "$crd_file" "$validations_file"
        fi
    done
    
    # Clean up
    rm -f "$validations_file"
    
    echo "CEL validation patching complete."
}

# Run main function with provided arguments
main "$@"