# adx-mon Operator Design

## Overview

The `adx-mon operator` is a Kubernetes operator responsible for managing the lifecycle of an adx-mon cluster, including its core components (collector, ingestor, alerter) and required Azure infrastructure. The operator uses the Azure SDK for Go to manage Azure resources such as ADX (Azure Data Explorer) clusters and databases.

The operator aims to provide a simple, production-ready bootstrap experience for adx-mon clusters, while supporting advanced customization for complex and federated deployments.

---

## Responsibilities

- **Azure Infrastructure Management:**  
  - Declaratively create and manage Azure resources using the Azure SDK
  - Built-in resource provider registration and validation
  - Automatic resource group creation and management
  - Wait for Azure resources to reach ready state before proceeding

- **adx-mon Component Management:**  
  - Generate and apply manifests for adx-mon components (collector, ingestor, alerter) as Kubernetes resources (e.g., StatefulSets, Deployments, Services).
  - Support default images for each component, with the ability to override via CRD spec.
  - Allow configuration of the number of ingestor instances (replicas).
  - Support further customization of component manifests via CRD fields.
  - Support deployment of only a subset of components (e.g., only collectors) to enable federated/multi-cluster topologies.

- **Reconciliation and Drift Detection:**  
  - Watch the manifests for adx-mon components.
  - If a managed resource (e.g., a StatefulSet for the ingestor) is manually changed in a way that overrides a setting specified in the corresponding CRD (such as replica count), detect the change and revert it to match the CRD's desired state.
  - Ensure the operator's desired state is always enforced in the actual cluster state, supporting declarative workflows.
  - **Note:** At this time, the operator does not attempt to reconcile drifted Azure resources (such as ADX clusters or databases). If Azure resources are modified outside the operator, the operator will not revert or update them to match the desired state.

- **Incremental, Granular Reconciliation:**  
  - The operator proceeds in highly granular steps, maintaining subresource conditions for each phase (e.g., ADX cluster ready, database ready, ingestor ready, collector ready).
  - Each phase is only attempted after all dependencies are successfully reconciled.
  - Status and conditions are updated at each step to reflect progress and issues.

- **Resource Cleanup on Deletion:**  
  - When a CRD for an adx-mon component (e.g., Collector, Ingestor, Alerter) is deleted, the operator will delete the corresponding managed Kubernetes resources (such as StatefulSets, Deployments, Services) to ensure a clean teardown of the adx-mon stack.
  - Azure infrastructure (e.g., ADX clusters and databases) will be left in place for now. In a future iteration, we may address safe teardown of these Azure resources.

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

- The CRD specs determine the deployment mode:
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
  - Default to a single ADX cluster with databases for _Metrics_ and _Logs_.
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

An important option for ADX clusters is to "bring your own" or utilize an existing cluster. This is accomplished by specifying the cluster's endpoint in the ADXCluster CRD. When an endpoint is provided, the operator will use the referenced existing ADX cluster rather than provisioning a new one.

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

The operator uses the Azure SDK for Go to manage the lifecycle of ADX clusters and databases:

1. **Azure SDK-Based Deployment:**
   - Uses the Azure SDK for Go to create, update, and delete ADX clusters and databases
   - Supports incremental deployment and updates
   - Handles output and status reporting for cluster endpoints and resource states

2. **Database Configuration:**
   - Databases created as part of deployment
   - Each database configured with:
     - Kind: ReadWrite
     - Soft delete period: P30D (30 days)
     - Hot cache period: P30D (30 days)
   - Copy-based deployment for multiple databases

3. **Strategy Selection:**
   - Azure SDK: Default and only implementation

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

### Example CRDs

The operator supports configurations ranging from minimal zero-config deployments to fully customized setups.

#### Minimal (Zero-Config) Example

This example deploys a collector-only setup with all defaults:

```yaml
apiVersion: adx-mon.azure.com/v1
kind: Collector
metadata:
  name: minimal-adx-collector
spec: {}
```

This minimal configuration:
- Uses default container images
- Deploys collector components with default settings
- Suitable for testing or development environments
- Can be expanded incrementally as needs grow

This example deploys a full adx-mon cluster with all defaults:

```yaml
apiVersion: adx-mon.azure.com/v1
kind: Collector
metadata:
  name: minimal-adx-collector
spec: {}

---

apiVersion: adx-mon.azure.com/v1
kind: Ingestor
metadata:
  name: minimal-adx-ingestor
spec:
  adxClusterSelector:
    matchLabels:
      # label selector for ADXCluster
      app: adx-mon

---

apiVersion: adx-mon.azure.com/v1
kind: Alerter
metadata:
  name: minimal-adx-alerter
spec:
  notificationEndpoint: "http://alerter-endpoint"
  adxClusterSelector:
    matchLabels:
      app: adx-mon

---

apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: minimal-adx-cluster
spec:
  clusterName: minimal-adx-cluster
```

---

#### Complete (Fully Customized) Example

This example demonstrates all available configuration options for each CRD:

```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: prod-adx-cluster
spec:
  clusterName: prod-metrics
  endpoint: "https://prod-metrics.kusto.windows.net"
  provision:
    subscriptionId: "00000000-0000-0000-0000-000000000000"
    resourceGroup: "adx-monitor-prod"
    location: "eastus2"
    skuName: "Standard_L8as_v3"
    tier: "Standard"
    managedIdentityClientId: "11111111-1111-1111-1111-111111111111"

---

apiVersion: adx-mon.azure.com/v1
kind: Ingestor
metadata:
  name: prod-ingestor
spec:
  image: "ghcr.io/azure/adx-mon/ingestor:v1.0.0"
  replicas: 3
  endpoint: "http://prod-ingestor.monitoring.svc.cluster.local:8080"
  exposeExternally: false
  adxClusterSelector:
    matchLabels:
      app: adx-mon

---

apiVersion: adx-mon.azure.com/v1
kind: Collector
metadata:
  name: prod-collector
spec:
  image: "ghcr.io/azure/adx-mon/collector:v1.0.0"
  ingestorEndpoint: "http://prod-ingestor.monitoring.svc.cluster.local:8080"

---

apiVersion: adx-mon.azure.com/v1
kind: Alerter
metadata:
  name: prod-alerter
spec:
  image: "ghcr.io/azure/adx-mon/alerter:v1.0.0"
  notificationEndpoint: "http://alerter-endpoint"
  adxClusterSelector:
    matchLabels:
      app: adx-mon
```

This configuration demonstrates:
- Full ADX cluster configuration with provisioning details
- Ingestor configuration with custom replica count, endpoint, and ADXCluster reference
- Collector configuration with explicit ingestor endpoint and custom image
- Alerter configuration with custom image, notification endpoint, and ADXCluster selector

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
   - If a managed resource is changed outside the operator (e.g., replica count updated), the operator will reconcile the resource back to the value specified in the CRD. The CRD spec is always the source of truth for fields like replica count; the operator does not perform two-way sync. Status fields in the CRD are updated to reflect the current state and any reconciliation actions taken.

3. **Sync Desired and Actual State:**  
   - If the Operator CRD is updated, reconcile the manifests and Azure resources as needed.
   - If the actual state drifts from the desired state, update the actual state to match the desired state.

---

## Implementation Notes

- The operator should use controller-runtime's watches and informers to efficiently detect changes to both its own CRD and the managed resources.
- Azure resources are the source of truth for infrastructure; the operator should not attempt to reconcile drifted Azure resources directly.
- The operator should be idempotent and safe to reapply.
- Subresource conditions should be used to provide granular status and progress reporting.
- Future versions may support more advanced customization, validation, and upgrade strategies.

---

## Future Enhancements

### Ingestor Auto-Scaler
- Implement an auto-scaler for the Ingestor component that dynamically adjusts the number of replicas based on workload or custom metrics.
- Add an option in the Ingestor CRD to enable or configure auto-scaling behavior (e.g., min/max replicas, scaling thresholds).

### ADX Cluster Federation
- Support for ADX cluster federation, enabling partitioning of data across multiple ADX clusters.
- Introduce a central ADX cluster that can federate queries across all partitioned clusters, providing a single pane of glass for querying and analytics.
- Enhance CRDs and operator logic to manage federated cluster relationships and query routing.