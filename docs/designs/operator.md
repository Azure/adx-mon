# adx-mon Operator Design

## Overview

The `adx-mon-operator` is a Kubernetes operator responsible for managing the lifecycle of an adx-mon cluster, including its core components (collector, ingestor, alerter) and required Azure infrastructure. The operator leverages [Azure Service Operator (ASO)](https://azure.github.io/azure-service-operator/) to declaratively manage Azure resources such as ADX (Azure Data Explorer) clusters, databases, and other dependencies.

The operator aims to provide a simple, production-ready bootstrap experience for adx-mon clusters, while supporting advanced customization for complex and federated deployments.

---

## Responsibilities

- **Azure Infrastructure Management:**  
  - Declaratively create and manage Azure resources (e.g., ADX clusters) using ASO CRDs.
  - Wait for ASO-managed resources to reach a ready state before proceeding with dependent adx-mon components.

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
  - When an Operator CRD is deleted, the operator will also delete all managed resources, including ASO CRDs (for ADX clusters and databases) and adx-mon component manifests (ingestor, collector, alerter), ensuring a clean teardown of the stack.

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

### Example CRD

```yaml
apiVersion: adx-mon.azure.com/v1
kind: Operator
metadata:
  name: adx-mon
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
            telemetryType: Logs      # enum: Logs, Metrics, Traces
          - name: metricsdb
            telemetryType: Metrics
  ingestor:
    image: ghcr.io/azure/adx-mon/ingestor:latest
    replicas: 3
  collector:
    image: ghcr.io/azure/adx-mon/collector:latest
    # If this is a collector-only cluster, specify ingestor connection:
    ingestorEndpoint: "http://central-ingestor.svc.cluster.local:8080"
    ingestorAuth:
      type: "token"
      tokenSecretRef:
        name: "ingestor-auth"
        key: "token"
  alerter:
    image: ghcr.io/azure/adx-mon/alerter:latest
```

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
     - Upsert ASO-managed ADX clusters.
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
- ASO CRDs are the source of truth for Azure resources; the operator should not attempt to manage Azure resources directly.
- The operator should be idempotent and safe to reapply.
- Subresource conditions should be used to provide granular status and progress reporting.
- Future versions may support more advanced customization, validation, and upgrade strategies.

---

## References

- [Azure Service Operator](https://azure.github.io/azure-service-operator/)
- [adx-mon](https://github.com/Azure/adx-mon)
- [`build/k8s/setup.sh`](../build/k8s/setup.sh) (reference for manual bootstrap steps)