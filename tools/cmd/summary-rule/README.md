# SummaryRule Monitor Tool

A live monitoring tool for ADX-Mon SummaryRules that displays real-time information about rule execution, timing, and status.

## Usage

```bash
./summary-rule -namespace <namespace> -name <name> [-kubeconfig <path>]
```

### Arguments

- `-namespace`: Required. The namespace where the SummaryRule is deployed
- `-name`: Required. The name of the SummaryRule to monitor
- `-kubeconfig`: Optional. Path to kubeconfig file. If not specified, will try in-cluster config first, then default kubeconfig

### Examples

```bash
# Monitor a SummaryRule in the default namespace
./summary-rule -namespace default -name my-summary-rule

# Monitor with custom kubeconfig
./summary-rule -namespace adx-mon -name hourly-metrics -kubeconfig ~/.kube/config
```

## Features

The tool displays:

1. **Rule Configuration**: Database, table, and execution interval
2. **Execution Timing**: Last execution time, time since last execution, and next execution time
3. **Cluster Labels**: Active cluster labels from the ingestor StatefulSet
4. **Rendered Query**: The actual KQL query with time windows and cluster labels substituted
5. **Outstanding Operations**: Any active async operations with their time windows
6. **Rule Status**: Current condition status and messages

## Live Updates

The tool refreshes every 10 seconds automatically. Press `Ctrl+C` to exit.

## Building

```bash
go build -o summary-rule main.go
```

## Requirements

- Access to a Kubernetes cluster with ADX-Mon deployed
- Appropriate RBAC permissions to read SummaryRules and StatefulSets
- Go 1.21+ for building from source
