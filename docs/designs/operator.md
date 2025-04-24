# adx-mon Operator Design

## Overview

The `adx-mon-operator` is a Kubernetes operator responsible for managing the lifecycle of an adx-mon cluster, including its core components (collector, ingestor, alerter) and required Azure infrastructure. The operator uses ARM templates for declarative management of Azure resources such as ADX (Azure Data Explorer) clusters and databases.

The operator aims to provide a simple, production-ready bootstrap experience for adx-mon clusters, while supporting advanced customization for complex and federated deployments.

---

## Responsibilities

- **Azure Infrastructure Management:**  
  - Declaratively create and manage Azure resources using ARM templates
  - Built-in resource provider registration and validation
  - Automatic resource group creation and management
  - Wait for Azure resources to reach ready state before proceeding

- **adx-mon Component Management:**  
  - Generate and apply manifests for adx-mon components (collector, ingestor, alerter) as Kubernetes resources (e.g., StatefulSets, Deployments, Services).
  - Support default images for each component, with the ability to override via CRD spec.
  - Allow configuration of the number of ingestor instances (replicas).
  - Support further customization of component manifests via CRD fields.
  - Support deployment of only a subset of components (e.g., only collectors, or only ingestors) to enable federated/multi-cluster topologies.

- **Reconciliation and Drift Detection:**  
  - Watch the manifests for adx-mon components.
  - If a user manually changes a managed resource (e.g., increases ingestor replicas), detect the change and update the operator CRD's status/spec to reflect the new state.
  - Ensure the operator's desired state is always in sync with the actual cluster state, supporting both declarative and imperative workflows.

- **Incremental, Granular Reconciliation:**  
  - The operator proceeds in highly granular steps, maintaining subresource conditions for each phase (e.g., ADX cluster ready, database ready, ingestor ready, collector ready).
  - Each phase is only attempted after all dependencies are successfully reconciled.
  - Status and conditions are updated at each step to reflect progress and issues.

- **Resource Cleanup on Deletion:**  
  - When an Operator CRD is deleted, the operator will also delete all managed resources, including Azure resources (for ADX clusters and databases) and adx-mon component manifests (ingestor, collector, alerter), ensuring a clean teardown of the stack.

---

## Multi-Cluster and Federation Support

- The operator supports deployment in different Kubernetes clusters for federated scenarios:
  - **Central Ingestor Cluster:**  
    - Deploys ingestor components (and optionally ADX resources).
    - Receives events from multiple remote collector clusters.
  - **Collector Clusters:**  
    - Deploys only collector components.
    - Forwards events to a central ingestor cluster.
    - Requires configuration of ingestor endpoints and authentication.

- The CRD spec determines the deployment mode:
  - If only `collector` is specified, the operator assumes this is a collector-only cluster and requires `ingestor` connection details.
  - If `ingestor` and `adx` are specified, the operator will bootstrap the full stack and expose endpoints for remote collectors.

---

## CRD Design

The `Operator` CRD is the entry point for managing an adx-mon cluster. It should support:

- **Minimal configuration for quick bootstrap:**  
  - Defaults for all images:  
    - `ghcr.io/azure/adx-mon/collector:latest`
    - `ghcr.io/azure/adx-mon/ingestor:latest`
    - `ghcr.io/azure/adx-mon/alerter:latest`
  - Default to a single ADX cluster and database.
  - Default to 1 replica for each component.

- **Customizable fields:**  
  - Images for each component.
  - Number of ingestor replicas.
  - ADX cluster/database names and configuration.
  - Support for multiple ADX clusters, each with multiple databases.
  - Each database specifies a telemetry type: `Logs`, `Metrics`, or `Traces`.
  - Cluster connection info, including endpoint and MSI client-id if using managed identity.
  - Optional: advanced overrides for component manifests.
  - **Federation fields:**  
    - For collector-only clusters: specify `ingestor` connection details (endpoint, authentication).
    - For ingestor clusters: expose endpoints for remote collectors.

- **Status fields:**  
  - Reflect the current state and subresource conditions (e.g., ADX cluster ready, database ready, ingestor ready, collector ready).

---

## ADX Cluster Management

### Auto-Provisioning and Zero-Config Support

The operator supports both fully specified and zero-config deployments:

1. **Zero-Config Mode:**
   - Uses Azure IMDS to automatically detect:
     - Region
     - Subscription ID
     - Resource Group
     - AKS cluster name (for naming)
   - Automatically selects optimal SKU based on regional availability
   - Creates default databases for metrics and logs
   - Configures standard retention policies

2. **Infrastructure Preparation:**
   - Automatically registers the Microsoft.Kusto resource provider if needed
   - Creates resource group if it doesn't exist
   - Validates SKU availability in the target region

3. **Default Configuration:**
   - Default databases: Metrics and Logs
   - Standard hot cache period: P30D
   - Standard soft delete period: P30D
   - Public network access: Disabled
   - Streaming ingest: Disabled

### ADX Deployment Strategy

The operator currently supports ARM template-based deployment with plans for Azure Service Operator (ASO) integration:

1. **Current: ARM Template-Based Deployment:**
   - Uses embedded ARM templates for consistent deployment
   - Supports incremental deployment mode
   - Dynamic template rendering with Go templates
   - Parameter inlining for simplified deployment
   - Built-in output handling for cluster FQDN

2. **Future: ASO-Based Deployment:**
   - Planned integration with Azure Service Operator
   - Will use ASO's Kusto CRDs for declarative management
   - Benefits:
     - Native Kubernetes resource model
     - Simplified dependency management
     - Integration with ASO's identity management
   - Implementation plan:
     - Add factory interface for deployment strategy
     - Support configurable choice between ARM and ASO
     - Maintain backwards compatibility with ARM templates

3. **Database Configuration (Both Strategies):**
   - Databases created as part of deployment
   - Each database configured with:
     - Kind: ReadWrite
     - Soft delete period: P30D (30 days)
     - Hot cache period: P30D (30 days)
   - Copy-based deployment for multiple databases

4. **Strategy Selection:**
   - ARM templates: Current default implementation
   - ASO: Future option via configuration flag
   - Factory pattern will allow runtime selection
   - Seamless migration path between strategies

### Managed Identity Integration

The operator supports managed identity configuration:

1. **Identity Assignment:**
   - System-assigned identity for ADX clusters
   - Optional user-assigned identity integration

2. **Role Management:**
   - Automatic role assignment for specified managed identities
   - Kusto Database Admin role assignment for cluster access

### SKU Selection Strategy

The operator implements a sophisticated SKU selection strategy:

1. **Preferred SKUs (in order):**
   - Standard_L8as_v3
   - Standard_L16as_v3
   - Standard_L32as_v3

2. **Fallback Behavior:**
   - Validates SKU availability in target region
   - Falls back to first available Standard tier SKU if preferred not available

### Resource Group Management

The operator handles resource group lifecycle:

1. **Resource Group Creation:**
   - Checks for resource group existence
   - Creates if not found using target region
   - Supports both existing and new resource groups

---

### Status Management

The operator implements a comprehensive status tracking system:

1. **Condition Types:**
   - InitCondition: Basic CRD setup and validation
   - ADXClusterCondition: ADX infrastructure state
   - IngestorClusterCondition: Ingestor deployment state
   - CollectorClusterCondition: Collector deployment state
   - AlerterClusterCondition: Alerter deployment state (when enabled)

2. **Status Reason Enumeration:**
   - NotInstalled: Component needs installation
   - Installing: Component is being installed/configured
   - Installed: Component is ready and running
   - Drifted: Configuration has deviated from spec
   - TerminalError: Unrecoverable error occurred
   - NotReady: Component is installed but not ready
   - Unknown: Status cannot be determined

3. **State Transitions:**

```mermaid
stateDiagram-v2
    [*] --> NotInstalled : CRD Created/Empty Condition
    NotInstalled --> Installing : Valid spec & shouldInstall()
    Installing --> Unknown : Transient error
    Installing --> NotReady : Resources created but not ready
    Installing --> TerminalError : Unrecoverable error
    Installing --> Installed : All resources ready & IsReady()
    NotReady --> Installing : AutoRetry (1min)
    Installed --> Drifted : ObservedGeneration != Generation
    Drifted --> Installing : Reconciliation started
    Unknown --> NotInstalled : Next reconciliation
    Unknown --> TerminalError : Fatal validation error
    TerminalError --> [*] : CRD Deleted
    
    state Installing {
        [*] --> ValidatingProviders : Entry
        ValidatingProviders --> CreatingResources : Providers ready
        ValidatingProviders --> RetryingProviders : Provider not ready
        RetryingProviders --> ValidatingProviders : Retry timer
        CreatingResources --> WaitingForReady : Resources created
        WaitingForReady --> ConfiguringResources : ADX ready
        ConfiguringResources --> UpdatingStatus : Components created
        UpdatingStatus --> [*] : All resources ready
    }
    
    note right of Installing {
        Substates:
        - ValidatingProviders: Azure/Kusto provider registration
        - RetryingProviders: Wait for provider registration
        - CreatingResources: Deploy ADX/DB/components
        - WaitingForReady: Check ADX cluster readiness
        - ConfiguringResources: Deploy app components
        - UpdatingStatus: Update resource conditions
    }
    
    note left of Drifted {
        Triggered by:
        - ObservedGeneration != Generation
        - Manual resource modifications
        - UpdateSpec() returns true
        - Resource state drift detected
    }
    
    note right of NotReady {
        Conditions:
        - Resources created but not ready
        - StatefulSet replicas not ready
        - DaemonSet pods not ready
        - ADX cluster not ready
        Auto-retries every 1 minute
    }
    
    note right of TerminalError {
        Terminal states:
        - Invalid configuration
        - Permission/quota errors 
        - Unrecoverable Azure failures
        - Template rendering errors
        - Resource creation failures
        No further reconciliation
    }
```

**State Transition Triggers:**
- NotInstalled → Installing: Valid spec, all prerequisites met
- NotInstalled → TerminalError: Fatal validation or config error
- Installing → Installed: All managed resources report ready
- Installing → TerminalError: Unrecoverable error (e.g., Azure failure, invalid config)
- Installing → NotReady: Resources created but not yet ready (e.g., pods not running)
- NotReady → Installing: Automatic retry or manual reconciliation
- Installed → Drifted: Spec generation changes or manual resource edits detected
- Drifted → Installing: Reconciliation triggered by drift
- TerminalError: Only leaves this state if spec is updated or CRD is deleted
- Unknown: Used for indeterminate states, transitions to NotInstalled or TerminalError on next reconciliation

**OperatorServiceReason values reflected in states:**
- NotInstalled
- Installing
- Installed
- Drifted
- TerminalError
- NotReady
- Unknown

**Control Flow and Status Update Notes:**
- All state transitions must call setCondition to update status and reason
- Any return ctrl.Result{}, nil in a non-terminal state without a status update is a bug and should be fixed
- TerminalError must always be accompanied by a clear status update and message
- Retryable errors should use Requeue or RequeueAfter to ensure forward progress
- Generation tracking (ObservedGeneration) ensures reconciliation on spec changes

---

## Autoscaler Design

The operator includes support for automated scaling of both ADX clusters and Ingestor components through a CronJob-based autoscaler. This component periodically evaluates system metrics and adjusts resources accordingly.

### Autoscaler Architecture

1. **Monitored Metrics:**
   - Ingestion Volume: Rate of data ingestion
   - System Load: CPU and memory utilization
   - Queue Length: Backlog of pending data
   - Custom metrics support via weighting system

2. **Scaling Logic:**
   - Multi-metric evaluation with configurable weights
   - Independent scaling of ADX and Ingestor components
   - Cooldown periods to prevent oscillation
   - Respects min/max boundaries for nodes/replicas

3. **Configuration Example:**
```yaml
spec:
  autoscaler:
    enabled: true
    schedule: "*/5 * * * *"
    adx:
      metrics:
        - type: IngestionVolume
          scaleUpThreshold: 75.0
          scaleDownThreshold: 25.0
          weight: 0.6
        - type: SystemLoad
          scaleUpThreshold: 80.0
          scaleDownThreshold: 30.0
          weight: 0.4
      minNodes: 2
      maxNodes: 10
      cooldownMinutes: 15
    ingestor:
      metrics:
        - type: QueueLength
          scaleUpThreshold: 1000
          scaleDownThreshold: 100
          weight: 0.7
        - type: MemoryUsage
          scaleUpThreshold: 85.0
          scaleDownThreshold: 40.0
          weight: 0.3
      minReplicas: 1
      maxReplicas: 10
      cooldownMinutes: 5
```

### Implementation Strategy

1. **Metric Collection:**
   - ADX cluster metrics via Azure Monitor
   - Ingestor metrics via Kubernetes metrics API
   - Custom metrics via Prometheus endpoints

2. **Scale Decision Algorithm:**
   ```
   For each component (ADX/Ingestor):
   1. Collect all configured metrics
   2. Calculate weighted score:
      score = Σ(metric_value * metric_weight)
   3. If score > scaleUpThreshold:
      - Scale up by 1 unit if cooldown period elapsed
   4. If score < scaleDownThreshold:
      - Scale down by 1 unit if cooldown period elapsed
   ```

3. **Safety Mechanisms:**
   - Gradual scaling (one unit at a time)
   - Cooldown periods between operations
   - Hard limits on min/max resources
   - Validation of scaling operations

4. **Status Reporting:**
   - Last scale operation timestamp
   - Current metric values
   - Scale decisions and reasons
   - Error conditions

The autoscaler provides a robust, configurable solution for maintaining optimal performance while managing resource costs effectively.

---

### Example CRDs

The operator supports configurations ranging from minimal zero-config deployments to fully customized setups.

#### Minimal (Zero-Config) Example

The simplest possible configuration deploys a collector-only setup with all defaults:

```yaml
apiVersion: adx-mon.azure.com/v1
kind: Operator
metadata:
  name: minimal-adx-monitor
spec:
  collector: {}  # Empty collector config uses all defaults
```

This minimal configuration:
- Uses default container images
- Deploys collector components with default settings
- Suitable for testing or development environments
- Can be expanded incrementally as needs grow

#### Complete (Fully Customized) Example

This example demonstrates all available configuration options:

```yaml
apiVersion: adx-mon.azure.com/v1
kind: Operator
metadata:
  name: complete-adx-monitor
spec:
  adx:
    clusters:
      - name: prod-metrics
        endpoint: "https://prod-metrics.kusto.windows.net"
        databases:
          - name: Metrics
            telemetryType: Metrics
          - name: Logs
            telemetryType: Logs
          - name: Traces
            telemetryType: Traces
        provision:
          subscriptionId: "00000000-0000-0000-0000-000000000000"
          resourceGroup: "adx-monitor-prod"
          region: "eastus2"
          sku: "Standard_L8as_v3"
          tier: "Standard"
          managedIdentityClientId: "11111111-1111-1111-1111-111111111111"

  ingestor:
    image: "ghcr.io/azure/adx-mon/ingestor:v1.0.0"
    replicas: 3

  collector:
    image: "ghcr.io/azure/adx-mon/collector:v1.0.0"
    ingestorEndpoint: "http://ingestor.monitoring.svc.cluster.local:8080"
    ingestorAuth:
      type: "token"
      tokenSecretRef:
        name: "ingestor-auth"
        key: "token"

  alerter:
    image: "ghcr.io/azure/adx-mon/alerter:v1.0.0"
```

The complete configuration demonstrates:
- Full ADX cluster configuration with provisioning details
- Multiple databases with different telemetry types
- Ingestor configuration with custom replica count
- Collector configuration with explicit ingestor connection
- Authentication configuration using Kubernetes secrets
- Alerter configuration with custom image

These examples can be used as templates and adjusted according to specific requirements. Many fields have default values and are optional.

#### Auto-Provisioning Example

This example demonstrates how the operator can automatically provision ADX clusters when only basic information is provided:

```yaml
apiVersion: adx-mon.azure.com/v1
kind: Operator
metadata:
  name: auto-provision-adx-monitor
spec:
  adx:
    clusters:
      - provision:
          subscriptionId: "00000000-0000-0000-0000-000000000000"
          resourceGroup: "adx-monitor-prod"
          region: "eastus2"
  
  ingestor:
    replicas: 2
  
  collector:
    image: "ghcr.io/azure/adx-mon/collector:v1.0.0"
```

In this configuration:
- The operator will automatically:
  - Generate a unique cluster name
  - Create the ADX cluster in the specified resource group and region
  - Set up default databases for logs, metrics, and traces
  - Configure networking and security settings
  - Apply recommended production-grade defaults for the cluster
- You only need to specify the Azure subscription details
- The operator handles all the complexity of ADX cluster provisioning
- Additional cluster settings can be added later through CRD updates

This approach is ideal for:
- Getting started quickly with production workloads
- Teams who want infrastructure-as-code without managing all the details
- Environments where standard configurations are preferred

#### Collector-Only Cluster Example

```yaml
apiVersion: adx-mon.azure.com/v1
kind: Operator
metadata:
  name: adx-mon-collector
spec:
  collector:
    image: ghcr.io/azure/adx-mon/collector:latest
    ingestorEndpoint: "http://central-ingestor.svc.cluster.local:8080"
    ingestorAuth:
      type: "token"
      tokenSecretRef:
        name: "ingestor-auth"
        key: "token"
```

#### Ingestor-Only (Central) Cluster Example

```yaml
apiVersion: adx-mon.azure.com/v1
kind: Operator
metadata:
  name: adx-mon-ingestor
spec:
  adx:
    clusters:
      - name: adx-prod
        endpoint: "https://adx-prod.eastus.kusto.windows.net"
        connection:
          type: MSI
          clientId: "12345678-1234-1234-1234-123456789abc"
        databases:
          - name: logsdb
            telemetryType: Logs
  ingestor:
    image: ghcr.io/azure/adx-mon/ingestor:latest
    replicas: 5
```

---

## Operator Workflow

1. **Granular Reconciliation:**  
   - The operator proceeds in incremental steps, updating subresource conditions for each phase:
     - Upsert Azure-managed ADX clusters.
     - Wait for ADX clusters to be ready.
     - Upsert ADX databases.
     - Wait for databases to be ready.
     - Create ingestor StatefulSet (parameterized with ADX connection strings).
     - Wait for ingestor to be ready.
     - Create collector DaemonSet (parameterized with ingestor endpoints).
     - Wait for collector to be ready.
     - Create alerter if specified.
   - Each step is only attempted after all dependencies are ready.

2. **Watch Managed Resources:**  
   - Monitor StatefulSets/Deployments for adx-mon components.
   - If a managed resource is changed outside the operator (e.g., replica count updated), update the Operator CRD to reflect the new state.

3. **Sync Desired and Actual State:**  
   - If the Operator CRD is updated, reconcile the manifests and Azure resources as needed.
   - If the actual state drifts from the desired state, update the CRD status/spec accordingly.

---

## Implementation Notes

- The operator should use controller-runtime's watches and informers to efficiently detect changes to both its own CRD and the managed resources.
- Azure resources are the source of truth for infrastructure; the operator should not attempt to manage Azure resources directly.
- The operator should be idempotent and safe to reapply.
- Subresource conditions should be used to provide granular status and progress reporting.
- Future versions may support more advanced customization, validation, and upgrade strategies.

---

## References

- [Azure Service Operator](https://azure.github.io/azure-service-operator/)
- [adx-mon](https://github.com/Azure/adx-mon)
- [`build/k8s/setup.sh`](../build/k8s/setup.sh) (reference for manual bootstrap steps)