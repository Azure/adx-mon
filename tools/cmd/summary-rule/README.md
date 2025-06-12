# SummaryRule Monitor Tool

A beautiful, live monitoring tool for ADX-Mon SummaryRules that displays real-time information about rule execution, timing, and status using a modern terminal UI.

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

## Modern Terminal UI

- **Responsive Design**: Automatically adapts to any terminal size
- **Beautiful Styling**: Colors, borders, and typography using Lipgloss
- **Live Updates**: Refreshes every 10 seconds with smooth transitions
- **Interactive Controls**: 
  - `q` or `Ctrl+C` to quit
  - `r` to manually refresh
- **Error Handling**: Graceful display of connection issues and errors

## Building

```bash
go build -mod=mod -o summary-rule main.go
```

### Quick Start

```bash
# Build the tool
go build -mod=mod -o summary-rule main.go

# Run with your SummaryRule
summary-rule NAMESPACE=default NAME=my-summary-rule

# Or with custom kubeconfig
summary-rule NAMESPACE=default NAME=my-summary-rule KUBECONFIG=~/.kube/config
```

## Technical Details

- **Built with**: [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [Lipgloss](https://github.com/charmbracelet/lipgloss)
- **Architecture**: Clean Elm-style architecture with message passing
- **Performance**: Efficient rendering with automatic screen size detection
- **Accessibility**: Works in any terminal that supports ANSI colors

## Requirements

- Access to a Kubernetes cluster with ADX-Mon deployed
- Appropriate RBAC permissions to read SummaryRules and StatefulSets
- Go 1.21+ for building from source
- Terminal with ANSI color support (most modern terminals)
