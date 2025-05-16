# Prompt: Update documentation

## Problem

Update the documentation found at `docs` to reflect the actual state of the codebase.

**Audience:**
- The primary audience is engineers who want to adopt adx-mon, including both users and potential contributors.
- Documentation should provide high-level overviews for new users and deep technical details (including CRDs, storage, and internals) for contributors.
- Diagrams and graphs illustrating how things work are highly encouraged.

**Documentation Ownership and Structure:**
- You are the author and maintainer of the documentation. You have full agency to restructure, add, remove, or rename files and directories within `docs` as needed to achieve the best clarity, usability, and maintainability.
- Organize the documentation in a way that best serves the audience, including splitting or merging documents, creating new guides, or removing outdated content.
- Ensure that navigation, cross-references, and the overall structure are logical and easy to follow.

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
   - Take note of our storage mechanism, which can be found in `pkg/wal`, `storage`, `ingestor/storage` and how these are utilized, like in `ingestor/cluster`. These are critical for understanding how data is stored and retrieved in the system.
   - **You must document the binary format for WAL files, including field layout, encoding, and any versioning or compatibility notes.** If the format is not documented, reverse-engineer it from the code and present your findings clearly. Use diagrams, tables, or images as appropriate to aid understanding. If you are unable to determine the format, state this and explain why.

2. **Update the documentation**:
   - For each component, update or create the relevant sections in the `docs` directory to accurately reflect its functionality and usage.
   - Ensure that the documentation is clear, concise, and easy to understand for engineers who may not be familiar with the codebase.
   - Include examples, configuration options, diagrams (Mermaid or images), and any other relevant information that would help users and contributors understand how to use and extend each component effectively.
   - Ensure that the documentation is consistent in style and format across all components.
   - Update or add diagrams (preferably Mermaid, but images are also acceptable) to reflect the current state of the codebase, including architecture, data flow, and component interactions.
   - Documentation should be updated to include any new features, changes in functionality, or updates to the configuration options.
   - Update all relevant sections of the documentation to ensure that they accurately reflect the current state of the codebase. This includes updating any sections that describe the installation process, configuration options, and usage examples.
   - **Review and update all configuration documentation in `docs/config.md` as needed.** Validate against config structs. If no changes are required, explicitly validate and state this in your output.
   - You may add, remove, or reorganize files and directories within `docs` as you see fit to improve clarity and usability.
   - **Provide additional documentation for the CRDs, including example YAML for each CRD, field explanations, and intended use cases.**

3. **Review and validate**:
   - After updating the documentation, review it to ensure accuracy and completeness.
   - Validate that the examples provided work as intended and that all configurations are correctly described (code-level review is sufficient).
   - Ensure that the documentation is up-to-date with the latest changes in the codebase.
   - **If you are unable to determine the binary format of the WAL or any other required detail, clearly state this in your output and explain why.**

4. **Optimize**:
    - The entire context might be too large. Instead of trying to execute all tasks before making documentation changes, iterate and solve. You may parallelize component analysis and documentation updates as resources allow.
    - This will help you to focus on one component at a time and ensure that the documentation is accurate and up-to-date for each component before moving on to the next one.
    - Once all components have been updated, review the entire documentation to ensure consistency and completeness.

**Change tracking:**
- No need to log or document changes beyond what git provides.
