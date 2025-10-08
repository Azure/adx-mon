# Ingestor Autoscaler Design

## Background
The Ingestor operator currently provisions StatefulSets and companion resources from a rendered manifest, applies updates when the custom resource changes, and tracks readiness. Replica counts are taken directly from `IngestorSpec.Replicas`, and any adjustments must be made manually. Production clusters experience highly variable ingestion load, so replicas should expand and contract automatically based on host utilization metrics. This document describes an autoscaling control loop that runs inside the operator and continuously reconciles the StatefulSet replica count to meet workload demand while preserving safe shutdown semantics.

## Goals
- Automatically adjust the ingestor StatefulSet replicas within safe bounds based on CPU utilization of ingestor pods.
- Preserve the existing reconciliation responsibilities of `IngestorReconciler` and integrate autoscaling without regressions.
- Provide predictable scale-up behavior with capped growth per interval and rate-limited scale-down that respects graceful shutdown annotations.
- Expose configuration through the `Ingestor` CR so operators can tune thresholds, intervals, and feature flags per deployment.
- Emit observable status so automation and SREs can understand scaling decisions.

## Non-goals
- Implement horizontal pod autoscaling for components other than ingestor.
- Replace existing manual override paths; the static replica field should remain available and take precedence when autoscaling is disabled.
- Introduce external dependencies beyond Kubernetes APIs and existing operator packages.

## Proposed Architecture
Introduce a dedicated autoscaler subsystem owned by the operator:
- A new package `operator/autoscaler` encapsulates metric collection, decision logic, and StatefulSet patching.
- `IngestorReconciler` creates an `Autoscaler` instance when autoscaling is enabled in the CR spec and delegates periodic checks through a long-running goroutine registered in `SetupWithManager`.
- The control loop runs on a configurable cadence (default 1 minute), fetches current metrics, and reconciles the desired replica count before returning.
- Scaling operations use Kubernetes API clients already available to the operator (controller-runtime client for StatefulSets and core Pods). Patches are applied using `client.Patch` with strategic merge semantics to minimize conflicts.

### Control Loop Steps
1. Load the current `Ingestor` resource and associated StatefulSet.
2. Short-circuit if the CR disables autoscaling, if the StatefulSet is not aligned with spec (pending reconciliation), or if the cluster lacks the required metrics API.
3. Compute average CPU utilization for ingestor nodes over the requested time window (5 or 15 minutes).
4. Ensure the StatefulSet replicas reported in status equal the spec replicas before issuing any scaling action.
5. Apply scale-up or scale-down decisions based on thresholds, interval guards, and min/max limits defined in the CR.
6. Persist status updates (`AutoscalerStatus`) with decision details (current CPU, action taken, next eligible scale time).

## API Additions
Extend `IngestorSpec` with an optional `Autoscaler` block:

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `Enabled` | `bool` | `false` | Turns the autoscaler on when true. |
| `MinReplicas` | `int32` | current `Spec.Replicas` | Lower bound. |
| `MaxReplicas` | `int32` | current `Spec.Replicas` | Upper bound. |
| `ScaleUpCPUThreshold` | `float64` | `70` | Average CPU percentage required to trigger scale-up. |
| `ScaleDownCPUThreshold` | `float64` | `40` | Average CPU percentage below which scale-down is considered. |
| `ScaleInterval` | `metav1.Duration` | `5m` | Minimum time between successive scaling actions. |
| `CPUWindow` | `metav1.Duration` | `15m` | Rolling window for CPU calculation (supports `5m` and `15m`). |
| `ScaleUpBasePercent` | `float64` | `0.25` | Fractional growth multiplier for scale-up. |
| `ScaleUpCapPerCycle` | `int32` | `5` | Maximum additional replicas in a single scale-up. |
| `DryRun` | `bool` | `false` | Log intent without modifying replicas. |
| `CollectMetrics` | `bool` | `true` | Toggle periodic metric publishing. |

Add `IngestorAutoscalerStatus` to `IngestorStatus`:
- `LastAction` (enum: `None`, `ScaleUp`, `ScaleDown`, `Skip`)
- `LastActionTime`
- `LastObservedCPU`
- `Reason`

Update CRDs, deepcopy, defaults, and validation to support the new struct.

## Scaling Algorithm
### Scale-Up
1. Calculate average CPU utilization across ingestor pods.
2. If utilization exceeds `ScaleUpCPUThreshold`, check that `time.Since(LastScaleTime) >= ScaleInterval`.
3. Determine candidate growth: `ceil(currentReplicas * ScaleUpBasePercent)`.
4. Cap growth to `ScaleUpCapPerCycle` and to `MaxReplicas`.
5. Patch the StatefulSet `spec.replicas` when not in dry-run mode.
6. Update status and `LastScaleTime`.

### Scale-Down
1. Ensure utilization is <= `ScaleDownCPUThreshold` and the replica count is above `MinReplicas`.
2. Validate that `ScaleInterval` has elapsed since the last scale-down.
3. Identify pods labeled `app=ingestor` in the target namespace.
4. If any pod already has the `shutdown-requested` annotation:
   - If `shutdown-completed` is absent, verify timeout (15m) and log wait.
   - If `shutdown-completed` is present, delete the pod and decrement replicas.
5. Otherwise, select the highest ordinal running pod, annotate it with `shutdown-requested=<timestamp>`, and exit.
6. Record scale-down progress in status and `LastScaleTime`.

### Synchronization Guards
- Skip scaling if `StatefulSet.Status.Replicas != *StatefulSet.Spec.Replicas` to avoid acting mid-rollout.
- Reset `LastScaleUpTime` when replicas are not yet synced to avoid premature re-entry.
- Handle API errors gracefully with exponential backoff via controller-runtime requeue.

## Implementation Plan

### 1. API & CRD Updates
- Modify `api/v1/ingestor_types.go` to include `AutoscalerSpec` and `AutoscalerStatus`.
- Add validation tags (e.g., min/max for thresholds) and defaulting logic in `api/v1/ingestor_defaults.go`.
- Regenerate CRDs (`make generate-crd CMD=update`) and commit resulting manifests.
- Update user-facing docs (`docs/ingestor.md`, `docs/config.md`) to introduce the new fields.

### 2. Metrics Abstraction
- Create `pkg/metrics/ingestor` with interfaces for CPU queries (e.g., `Collector` interface returning averages for configured windows).
- Implement a Kubernetes metrics-server-backed collector using `/apis/metrics.k8s.io/v1beta1/nodes` with aggregation across nodes hosting ingestor pods.
- Add a fallback Prometheus query client (if desired) guarded behind feature flags.
- Unit test collectors using fake clients to cover 5m and 15m windows.

### 3. Autoscaler Engine
- Add `operator/autoscaler/autoscaler.go` implementing:
  - `type Engine struct { client client.Client; metrics Collector; clock Clock; }`
  - `Run(ctx context.Context, ingestor *adxmonv1.Ingestor)` loop honoring tick interval and watch cancellation via context.
  - `evaluate(ctx, state)` returning desired action (`None`, `ScaleUp`, `ScaleDown`).
- Persist last-scale metadata in the CR status.
- Expose constructor `NewEngine(cfg AutoscalerSpec, deps ...)` for injection.

### 4. Operator Integration
- Update `IngestorReconciler`:
  - During `SetupWithManager`, add a `controller.Manager.Add` runnable that starts the autoscaler engine for each namespace.
  - On each reconcile, ensure autoscaler configuration is synced and restart the engine when spec changes.
  - Wire in dependency injection for metrics collector and clock to support testing.

### 5. Scale Execution Helpers
- Add helper methods to patch replicas (`applyReplicaPatch(ctx, sts, newCount)`), annotate pods, and select highest ordinal pod (reuse logic from existing operator utilities where possible).
- Ensure all write operations use controller-runtime clients for consistency.
- Add constants for annotation keys and scale timeout duration in a shared package.

### 6. Observability & Events
- Emit Kubernetes events on scale actions (Reasons: `IngestorScaleUp`, `IngestorScaleDown`, `IngestorScaleSkip`).
- Extend structured logging to include utilization, thresholds, and target replicas.
- Update metrics package to optionally publish Prometheus counters/histograms if `CollectMetrics` is enabled.

### 7. Testing & Validation
- Unit tests:
  - Autoscaler decision matrix across threshold boundaries, min/max limits, dry-run, and sync guards.
  - Pod selection for shutdown annotations and timeout handling.
- Integration tests (controller-runtime envtest):
  - Ensure CRD defaulting works.
  - Validate end-to-end scale-up and scale-down flows against a fake Kubernetes API.
- Document manual validation steps (`docs/cookbook.md`) using kind or AKS test clusters.

### 8. Rollout Strategy
- Gate the feature behind a CR flag; default off.
- Provide migration guidance for existing clusters (CR update in manifests or Helm charts).
- Monitor logs and status during canary rollout; verify no unexpected restarts or replica oscillations.

## Testing Strategy Summary
- `go test ./operator/autoscaler/...` for unit coverage.
- Envtest suite validating autoscaler integration with the controller-runtime manager.
- Optional e2e scenario in CI once metrics dependencies are mocked.

## Documentation & Follow-Up
- Update operator README and user docs with configuration examples and troubleshooting tips.
- Add release notes under `RELEASE.md` describing the new autoscaler.
- Capture future enhancements (e.g., memory-based triggers, predictive scaling) in the backlog once the initial CPU-driven control loop ships.
