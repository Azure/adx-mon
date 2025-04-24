# verify-operator-state-transitions.prompt.md

Your task is to analyze the `/operator` directory to build an internal representation of the state model for the adx-mon operator. Focus on the flow progression of the CRD as the operator attempts to create Kusto clusters, Ingestor, and Collector instances.

**Instructions:**

1. **State Model Construction:**
   - Examine the CRD definition in `api/v1/operator_types.go`, especially the `OperatorServiceReason` enum and condition owner constants.
   - Map out all possible states and transitions for each managed component (Kusto, Ingestor, Collector), using the `OperatorServiceReason` values:
     - NotInstalled
     - Installing
     - Installed
     - Drifted
     - TerminalError
     - NotReady
     - Unknown
   - Reference the design documentation in `docs/designs/operator.md` for intended state transitions and triggers.
   - For each component, document dependencies on other components being in specific states.

2. **Reconciliation Flow:**
   - Trace the reconciliation logic in `/operator`, especially how `ctrl.Result` is returned in each handler (e.g., `handleAdxEvent`, `ResourceHandler.HandleEvent`, etc.).
   - Ensure that the operator always requeues (via `ctrl.Result{Requeue: true}` or `RequeueAfter`) unless a terminal error is encountered.
   - Identify how each subresource's `Reason` is set and how it drives the next phase.
   - Validate that state transitions between dependent components are properly sequenced.

3. **Cycle and Skipped Phase Detection:**
   - Check for cycles where an incorrect `Reason` causes infinite retries or prevents progress (e.g., stuck in `Installing` or skipping `Drifted`/`NotReady`).
   - Ensure that all phases are reachable and that transitions match the intended state machine.
   - Verify that component dependencies are respected (e.g., Ingestor must wait for ADX to be ready).

4. **Error and Halting Condition Analysis:**
   - Review all function returns in the codebase. Flag any empty `ctrl.Result{}` returns as potential bugs, as they signal to the controller-manager that reconciliation is complete.
   - **For any early return (such as when a required spec or configuration is missing, or after setting an initial or intermediate condition):**
     - The function MUST NOT return an empty `ctrl.Result{}` unless the state is truly terminal or all components are fully reconciled.
     - If a function sets a status condition (e.g., `InitConditionOwner`, `NotInstalled`, or any intermediate phase), it MUST return `ctrl.Result{Requeue: true}` (or `ctrl.Result{RequeueAfter: duration}` for waiting states) to ensure the next reconciliation phase is triggered.
     - Guard clauses and early-exit paths must be reviewed to ensure they do not prematurely halt reconciliation without proper state and status updates, and must not return empty `ctrl.Result{}` unless in a terminal or fully reconciled state.
     - Example: After setting `InitConditionOwner` in `handleInitEvent`, return `ctrl.Result{Requeue: true}, nil` instead of `ctrl.Result{}, nil`.
   - For each component state transition function:
     - When transitioning to `Installing` or `Drifted`, verify the function returns `ctrl.Result{Requeue: true}` to continue reconciliation
     - When in a waiting state, verify the function returns `ctrl.Result{RequeueAfter: duration}` with an appropriate duration
     - Empty `ctrl.Result{}` should ONLY occur when all components are fully reconciled or a terminal error is hit
   - Cross-reference each component's state with its dependencies to ensure reconciliation cannot halt prematurely:
     - ADX cluster state must be `Installed` before Ingestor reconciliation can complete
     - Ingestor state must be `Installed` before Collector reconciliation can complete
   - For any state transition that affects dependent components, verify that `ctrl.Result{Requeue: true}` is returned to trigger the dependent reconciliation
   - Check error handling paths to ensure they either:
     - Return the error with empty result for retryable errors: `return ctrl.Result{}, err`
     - Set terminal state and return empty result for non-retryable errors: `return ctrl.Result{}, nil`

5. **State Propagation Validation:**
   - Ensure that when a component enters the `Installed` state, it properly triggers reconciliation of dependent components.
   - Verify that `ctrl.Result` returns are appropriate for the current state and remaining work:
     - `ctrl.Result{Requeue: true}` when more components need reconciliation
     - `ctrl.Result{RequeueAfter: duration}` for waiting states
     - Empty `ctrl.Result{}` only when all components are fully reconciled
   - Check that observers of component state changes (e.g., Ingestor waiting for ADX) are properly notified.

6. **Component Dependency Graph:**
   - Document the dependency relationships between components:
     - ADX cluster must be ready before Ingestor
     - Ingestor must be ready before Collector
     - Any other implicit dependencies
   - Verify that the reconciliation logic respects these dependencies.
   - Check that status conditions reflect the correct dependency state.

7. **Reporting and Suggestions:**
   - For any issues found, categorize them by severity:
     - Critical: Empty `ctrl.Result{}` returns that break the reconciliation chain
     - High: Incorrect requeue settings that could delay or prevent reconciliation
     - Medium: Unclear state transitions or missing dependency checks
     - Low: Code clarity or documentation issues
   - For each issue provide:
     - File and function location
     - Current behavior and potential impact
     - Recommended fix with code example
   - Pay special attention to state propagation issues where a component's completion fails to trigger dependent components.

**Goal:**
- Ensure the operator's state machine is robust, all transitions are valid, and reconciliation logic is correct for all managed resources.
- Validate proper state propagation between dependent components.
- Verify that reconciliation continues until all components are fully reconciled.
- Provide actionable recommendations for any detected issues.
