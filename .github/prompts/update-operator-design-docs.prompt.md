# Prompt: Update Operator Design Docs to Match CRD State

## Task

Update the design document at `docs/designs/operator.md` to accurately reflect the current state of the Custom Resource Definitions (CRDs) as defined in the following files:

- `api/v1/adxcluster_types.go`
- `api/v1/alerter_types.go`
- `api/v1/collector_types.go`
- `api/v1/ingestor_types.go`

## Instructions

1.  **Read the CRD Definitions:** Parse the Go structs and field tags in the `api/v1/` files listed above to determine the current schema, field names, types, and descriptions for each CRD.

2.  **Update the CRD Sections in the Design Doc:**
    *   Revise any CRD examples, field lists, or YAML snippets in `docs/designs/operator.md` to match the actual fields and structure found in the Go source.
    *   Update any descriptive language or explanations to reflect the current CRD state, including new fields, removed fields, or changes in field types or semantics.
    *   Ensure that all examples are valid and up-to-date with the Go source.

3.  **Analyze Operator Logic:** Examine the Go code for the operators located under the `operator/` directory (e.g., `operator/ingestor.go`, etc.).

4.  **Add/Update Operator Documentation Sections:** For each operator corresponding to a CRD:
    *   Create or update its dedicated section within `docs/designs/operator.md`.
    *   **Fundamental Tasks:** Document the core responsibilities and actions performed by the operator during its reconciliation loop.
    *   **Terminal Conditions:** Identify conditions within the operator code that are considered terminal (i.e., reconciliation stops or enters a final error state). Analyze the code to explain *why* these conditions are terminal based on the intended logic.
    *   **Requeue Logic:** Document instances where `requeue-after` is used. Specify the operation triggering the requeue, the reason for waiting, and the duration of the requeue delay.
    *   **CRD Lifecycle Flow:** Describe the typical sequence of operations and status updates the operator performs from the initial creation of a CRD instance until it reaches a completed or stable state. Include major milestones.
    *   **CRD Update Flow:** Describe how the operator handles updates to existing CRD instances, including the sequence of operations and status changes.

5.  **Be Comprehensive:** Check for all CRDs and their fields, including nested and optional fields. Ensure the documentation for both CRDs and operators is accurate and complete.

6.  **Formatting:** Preserve the formatting and style of the original design document. Only update the relevant sections.

7.  **No Manual Edits:** All changes should be based strictly on the Go source files listed above and the operator logic found in the `operator/` directory.

## Output
The result should be an updated `docs/designs/operator.md` that is fully synchronized with the current CRD definitions and operator logic.
