# ADX-Mon AI Coding Instructions

## Architecture Overview
ADX-Mon is a Kubernetes-native observability platform with 5 core components that form a unified data pipeline:
- **Collector**: Scrapes metrics (Prometheus-compatible) and forwards to Ingestors
- **Ingestor**: Receives metrics/logs via remote write, stores in WAL segments, uploads to Azure Data Explorer (ADX)
- **Alerter**: Executes KQL queries against ADX based on AlertRule CRDs, sends notifications
- **Operator**: Manages lifecycle of all components via Kubernetes CRDs
- **ADX Exporter**: Exports data from ADX for external systems

## Core Patterns

### Service Architecture
All services implement the `pkg/service.Component` interface:
```go
type Component interface {
    Open(ctx context.Context) error
    Close() error
}
```
Main binaries follow the pattern: CLI parsing → service creation → graceful shutdown handling.

### CRD Workflow
- API types in `./api/v1/` define Kubernetes resources (AlertRule, Ingestor, Collector, etc.)
- **Critical**: After modifying `./api/v1/*.go`, run `make generate-crd CMD=update` to regenerate manifests
- CRD definitions live in both `./kustomize/bases/` and `./operator/manifests/crds/`
- The `tools/crdgen/` Docker-based system handles cross-platform CRD generation

### Data Flow Patterns
1. **Metrics**: Collector → Ingestor → ADX (via WAL segments for reliability)
2. **Alerts**: Alerter watches AlertRule CRDs → executes KQL queries → sends notifications
3. **Configuration**: All components use CLI flags with Kubernetes service account fallbacks

### WAL Segments (Critical for Reliability)
- **Purpose**: Write-Ahead Log segments provide durability and prevent data loss during outages
- **Location**: Stored locally on disk (`--storage-dir`) before upload to ADX
- **Lifecycle**: Ingestor creates segments → ages them → uploads to ADX → peer transfers for redundancy
- **Key Parameters**: `--max-segment-size`, `--max-segment-age`, `--max-transfer-age` control behavior
- **Backpressure**: System signals backpressure when `--max-segment-count` or `--max-disk-usage` exceeded
- **Implementation**: See `pkg/wal/` for core WAL logic and `ingestor/storage/` for segment management

### Database Naming Convention
Kusto endpoints use format: `<database>=<endpoint>` (case-sensitive database matching required)

## Development Workflows

### Testing
- Run all tests: `go test ./...`
- Integration tests: `make test` (includes integration tests with `INTEGRATION=1`). These take longer to run. You should only run these when asked.
- Individual packages: `go test ./pkg/logger` 
- Test environment needs Azure credentials for Kusto integration tests

### Build System
- All components: `make build` (creates `bin/` directory)
- Individual: `make build-alerter`, `make build-ingestor`, etc.
- Docker images: `make image` (uses `build/images/Dockerfile.*`)
- **Note**: Collector requires `CGO_ENABLED=1`, others use `CGO_ENABLED=0`

### K8s Bundle Management
- After changing `./build/k8s/`, run `make k8s-bundle` to regenerate deployment bundle
- The `build/scripts/generate-bundle.sh` creates deterministic bundles via Docker

## Code Conventions

### Go Standards
- Format all code with `go fmt` (excluding vendor/)
- Follow standard Go project layout with `cmd/`, `pkg/`, `api/` directories
- Use structured logging via `pkg/logger`

### Error Handling
- Services use context cancellation for graceful shutdown
- Background processes should respect context.Done() signals
- WAL segments provide durability during network/ADX outages
- Wrap errors with context using `fmt.Errorf` with `%w` verb for error chains
- Check errors immediately after function calls - don't ignore with `_`

### Concurrency Patterns
- Always ensure goroutines can exit (via context cancellation or channels)
- Use `context.WithCancel` pattern seen throughout main binaries
- Background services should respect `ctx.Done()` for graceful shutdown
- Components follow Open/Close lifecycle with proper cleanup

### Code Organization
- Keep functions focused and single-purpose - break apart complex functions rather than adding branching logic
- Avoid "sideways pyramids" of nested conditionals - extract helper functions instead
- When adding new functionality, consider if it belongs in a separate function rather than expanding existing ones
- Prefer multiple small, testable functions over large functions with many responsibilities
- Extract complex conditional logic into well-named boolean functions for readability

### Performance-Critical Hot Paths
- **Metrics ingestion**: `collector/metrics/handler.go` uses `pool.BytesBufferPool` for zero-allocation buffer reuse
- **WAL segment operations**: `pkg/wal/` segment writes are benchmarked and performance-critical
- **Protobuf marshaling**: `pkg/prompb/` marshal/unmarshal operations are in the hot path
- **Storage writes**: `storage/store.go` uses object pools to avoid allocations during writes
- When modifying these areas, maintain existing pool patterns and avoid introducing new allocations

### Testing
- Use `t.Helper()` for test utility functions
- Use `require` for tests (used in 94% of test files) - prefer over `assert` for cleaner failure handling
- Integration tests require `INTEGRATION=1` environment variable
- Azure credentials needed for Kusto integration tests

### Kusto Integration
- Connection strings use Azure DefaultCredential for production
- Database names are case-sensitive and must match AlertRule specs
- KQL queries support templating with cluster labels (prefixed with `_`)

## Key Files to Understand
- `api/v1/groupversion_info.go`: CRD group definition (`adx-mon.azure.com/v1`)
- `pkg/service/service.go`: Core service interface
- `pkg/wal/`: WAL segment implementation for data durability
- `transform/transformer.go`: Metrics filtering/transformation pipeline
- `Makefile`: Complete build and generation workflows
- `build/scripts/generate-bundle.sh`: K8s deployment bundling logic

## Documentation Structure
- `docs/`: User-facing documentation (architecture, concepts, guides)
- `docs/designs/`: Technical design documents for major features
- `README.md`: Project overview and quick start for testing AlertRules
