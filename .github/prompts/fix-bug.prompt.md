# adx-mon Bug Triage and Fix Prompt

This prompt is for use exclusively with the `adx-mon` project. It is designed to guide an autonomous agent in triaging and fixing bugs, regardless of the programming language or toolchain involved. The agent should follow this workflow rigorously, adapting commands and techniques as appropriate for the codebase and environment.

---

## Initial Step: Request Bug Description

**Upon execution, before proceeding, ask the user to describe the bug they want fixed.**

---

## High-Level Problem Solving Strategy

1. **Understand the Problem Deeply**
   - Carefully read the bug report or issue provided by the user.
   - Think critically about what is required and clarify the expected behavior.

2. **Investigate the Codebase**
   - Explore relevant files, directories, and modules.
   - Search for key functions, classes, or variables related to the issue.
   - Read and understand relevant code snippets.
   - Identify the root cause of the problem.
   - Continuously validate and update your understanding as you gather more context.

3. **Develop a Clear, Step-by-Step Plan**
   - Outline a specific, simple, and verifiable sequence of steps to fix the problem.
   - Break down the fix into small, incremental changes.

4. **Implement the Fix Incrementally**
   - Before editing, always read the relevant file contents or section to ensure complete context.
   - Make small, testable, incremental changes that logically follow from your investigation and plan.
   - Use the appropriate language, tools, and conventions for the affected code (e.g., Go, shell, YAML, etc.).

5. **Debug as Needed**
   - Make code changes only if you have high confidence they can solve the problem.
   - When debugging, try to determine the root cause rather than addressing symptoms.
   - Use print statements, logs, or temporary code to inspect program state, including descriptive statements or error messages to understand what's happening.
   - To test hypotheses, you can also add test statements or functions.
   - Revisit your assumptions if unexpected behavior occurs.

6. **Test Frequently**
   - Run tests after each change to verify correctness. Use the appropriate test runner for the affected code (e.g., `make test`, `go test ./...`, or other project-specific commands).
   - If tests fail, analyze failures and revise your patch.
   - Write additional tests if needed to capture important behaviors or edge cases.
   - Ensure all tests pass before finalizing.

7. **Iterate Until the Root Cause is Fixed and All Tests Pass**
   - Continue refining your solution until you are confident the fix is robust and comprehensive.

8. **Reflect and Validate Comprehensively**
   - Review your solution for logic correctness and robustness.
   - Think about potential edge cases or scenarios that may not be covered by existing tests.
   - Write additional tests that would need to pass to fully validate the correctness of your solution.
   - Run these new tests and ensure they all pass.
   - Be aware that there may be additional hidden tests that must also pass for the solution to be successful.
   - Do not assume the task is complete just because the visible tests pass; continue refining until you are confident the fix is robust and comprehensive.

---

## Additional Guidance

- This prompt is only for use with the `adx-mon` project. All investigation and fixes should be limited to this codebase.
- The agent should adapt commands and techniques to the language and tools used in the affected part of the codebase.
- Always prefer clarity, thoroughness, and incremental progress over large, risky changes.
- Document your reasoning and steps as you go, to ensure traceability and reproducibility.
- Only terminate your process when you are certain the problem is fully resolved and the fix is robust.
