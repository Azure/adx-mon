#!/usr/bin/env python3
"""
Script to automatically patch generated CRDs with CEL validations.
This script parses Go struct annotations and adds corresponding x-kubernetes-validations to CRD YAML files.
"""

import os
import re
import sys
import yaml
from pathlib import Path


def extract_cel_validations(go_file_path):
    """Extract CEL validation rules from Go file annotations."""
    validations = {}
    
    with open(go_file_path, 'r') as f:
        lines = f.readlines()
    
    i = 0
    while i < len(lines):
        line = lines[i].strip()
        
        # Look for kubebuilder XValidation annotation
        xval_pattern = r'//\s*\+kubebuilder:validation:XValidation:rule="([^"]+)",message="([^"]+)"'
        match = re.search(xval_pattern, line)
        
        if match:
            rule = match.group(1)
            message = match.group(2)
            
            # Find the next non-comment line to get the field name
            j = i + 1
            while j < len(lines):
                next_line = lines[j].strip()
                if not next_line.startswith('//') and next_line:
                    # Extract field name from struct field definition
                    field_pattern = r'(\w+)\s+[^`]+`json:"([^"]+)"'
                    field_match = re.search(field_pattern, next_line)
                    if field_match:
                        json_field_name = field_match.group(2)
                        validations[json_field_name] = {
                            'rule': rule,
                            'message': message
                        }
                    break
                j += 1
        
        i += 1
    
    return validations


def patch_crd_yaml(crd_file_path, validations):
    """Patch CRD YAML file with CEL validations."""
    if not validations:
        return
    
    with open(crd_file_path, 'r') as f:
        crd = yaml.safe_load(f)
    
    # Navigate to the spec properties in the CRD
    if 'spec' not in crd or 'versions' not in crd['spec']:
        return
    
    for version in crd['spec']['versions']:
        schema = version.get('schema', {}).get('openAPIV3Schema', {})
        properties = schema.get('properties', {})
        
        if 'spec' in properties and 'properties' in properties['spec']:
            spec_properties = properties['spec']['properties']
            
            # Add validations to matching fields
            for field_name, validation in validations.items():
                if field_name in spec_properties:
                    # Initialize x-kubernetes-validations if it doesn't exist
                    if 'x-kubernetes-validations' not in spec_properties[field_name]:
                        spec_properties[field_name]['x-kubernetes-validations'] = []
                    
                    # Check if this validation rule already exists
                    existing_rules = [
                        v.get('rule') for v in spec_properties[field_name]['x-kubernetes-validations']
                    ]
                    
                    if validation['rule'] not in existing_rules:
                        spec_properties[field_name]['x-kubernetes-validations'].append({
                            'rule': validation['rule'],
                            'message': validation['message']
                        })
    
    # Write the modified CRD back to file
    with open(crd_file_path, 'w') as f:
        yaml.dump(crd, f, default_flow_style=False, sort_keys=False)


def main():
    """Main function to process Go files and patch CRDs."""
    api_dir = sys.argv[1] if len(sys.argv) > 1 else './api/v1'
    crd_dir = sys.argv[2] if len(sys.argv) > 2 else './config/crd/bases'
    
    print("Extracting CEL validations from Go files...")
    
    # Collect all validations from Go files
    all_validations = {}
    for go_file in Path(api_dir).glob('*.go'):
        if go_file.is_file():
            validations = extract_cel_validations(go_file)
            all_validations.update(validations)
            if validations:
                print(f"Found {len(validations)} validation(s) in {go_file.name}")
    
    if not all_validations:
        print("No CEL validations found in Go files.")
        return
    
    print(f"Total validations found: {len(all_validations)}")
    for field, val in all_validations.items():
        print(f"  {field}: {val['rule']}")
    
    # Patch CRD files
    print("\nPatching CRD files...")
    crd_path = Path(crd_dir)
    if not crd_path.exists():
        print(f"CRD directory {crd_dir} does not exist.")
        return
    
    for crd_file in crd_path.glob('*.yaml'):
        if crd_file.is_file():
            print(f"Patching {crd_file.name}")
            patch_crd_yaml(crd_file, all_validations)
    
    print("CEL validation patching complete.")


if __name__ == '__main__':
    main()