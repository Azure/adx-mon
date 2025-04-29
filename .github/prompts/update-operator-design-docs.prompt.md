# Prompt: Update Operator Design Docs to Match CRD State

## Task

Update the design document at `docs/designs/operator.md` to accurately reflect the current state of the Custom Resource Definitions (CRDs) as defined in the following files:

- `api/v1/adxcluster_types.go`
- `api/v1/alerter_types.go`
- `api/v1/collector_types.go`
- `api/v1/ingestor_types.go`

## Instructions

1. **Read the CRD Definitions:** Parse the Go structs and field tags in the above files to determine the current schema, field names, types, and descriptions for each CRD.

2. **Update the Design Doc:**
   - Revise any CRD examples, field lists, or YAML snippets in `docs/designs/operator.md` to match the actual fields and structure found in the Go source.
   - Update any descriptive language or explanations to reflect the current CRD state, including new fields, removed fields, or changes in field types or semantics.
   - Ensure that all examples are valid and up-to-date with the Go source.

3. **Be Comprehensive:** Check for all CRDs and their fields, including nested and optional fields. Ensure the documentation is accurate and complete.

4. **Formatting:** Preserve the formatting and style of the original design document. Only update the relevant sections.

5. **No Manual Edits:** All changes should be based strictly on the Go source files listed above.

## Output

The result should be an updated `docs/designs/operator.md` that is fully synchronized with the current CRD definitions.
