# Prompt: Update documentation

## Problem

Update the documentation found at `docs` to reflect the actual state of the codebase.

This codebase includes several components, their entries are at:
- `cmd/alerter`
- `cmd/collector`
- `cmd/ingestor`
- `cmd/operator`

We make heavy use of CRDs, their definitions can be found at `api/v1`.

## Task

1. **Analyze each component**:
   - Read the code in each component to understand its purpose, functionality, and how it interacts with other components.
   - Identify the key features, configurations, and usage patterns for each component.
   - Take note of our storage mechanism, which can be found in `pkg/wal`, `storage`, `ingestor/storage` and how these are utilized, like in `ingestor/cluster`. These are critical for understanding how data is stored and retrieved in the system. **You must document the binary format for WAL files, including field layout, encoding, and any versioning or compatibility notes.** Diagrams are useful here.

2. **Update the documentation**:
   - For each component, update the relevant sections in the `docs` directory to accurately reflect its functionality and usage.
   - Ensure that the documentation is clear, concise, and easy to understand for users who may not be familiar with the codebase.
   - Include examples, configuration options, diagrams, and any other relevant information that would help users understand how to use each component effectively.
   - Ensure that the documentation is consistent in style and format across all components.
   - Mermaid diagrams should be updated to reflect the current state of the codebase. This includes updating any diagrams that illustrate the architecture, data flow, or interactions between components.
   - Documentation should be updated to include any new features, changes in functionality, or updates to the configuration options.
   - Update all relevant sections of the documentation to ensure that they accurately reflect the current state of the codebase. This includes updating any sections that describe the installation process, configuration options, and usage examples.
   - **Review and update all configuration documentation in `docs/config.md` as needed.** If no changes are required, explicitly validate and state this in your output.

3. **Review and validate**:
   - After updating the documentation, review it to ensure accuracy and completeness.
   - Validate that the examples provided work as intended and that all configurations are correctly described.
   - Ensure that the documentation is up-to-date with the latest changes in the codebase.
   - **If you are unable to determine the binary format of the WAL or any other required detail, clearly state this in your output and explain why.**

4. **Optimize**:
    - The entire context might be too large. Instead of trying to execute all tasks before making documentation changes, iterate and solve. Execute the prompt one component at a time, starting with `cmd/alerter` and finishing with `cmd/operator`, updating documentation as you go.
    - This will help you to focus on one component at a time and ensure that the documentation is accurate and up-to-date for each component before moving on to the next one.
    - Once all components have been updated, review the entire documentation to ensure consistency and completeness.
