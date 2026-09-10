# adx-mon Helm Chart

> [!WARNING]
> This chart is a work-in-progress. It has been validated in specific production environments but does not yet cover all common deployment scenarios. Contributions for typical use cases are welcome — please open a pull request or issue at [Azure/adx-mon](https://github.com/Azure/adx-mon).

Deploys [adx-mon](https://github.com/Azure/adx-mon) — a Kubernetes-native observability platform — to an AKS cluster. The chart includes the collector, ingestor, alerter, and operator components, along with all required CRDs.

## Prerequisites

- Kubernetes 1.25+
- Helm 3.10+
- An Azure Data Explorer (ADX/Kusto) cluster
- Workload identity configured on your AKS cluster

## Installation

```bash
helm install adx-mon oci://ghcr.io/azure/adx-mon/charts/adx-mon \
  --namespace adx-mon \
  --create-namespace \
  --set aks_cluster_name=my-cluster \
  --set region=eastus \
  --set environment=prod \
  --set adx.name=my-adx-cluster \
  --set adx.url=https://my-adx-cluster.eastus.kusto.windows.net \
  --set adx_hub_url=https://my-hub-cluster.eastus.kusto.windows.net \
  --set ingestor.client_id=<workload-identity-client-id>
```

Or with a values file:

```bash
helm install adx-mon oci://ghcr.io/azure/adx-mon/charts/adx-mon \
  --namespace adx-mon \
  --create-namespace \
  -f my-values.yaml
```

## Configuration

### Required values

| Key | Description |
|-----|-------------|
| `aks_cluster_name` | Name of the AKS cluster, added as a label to all metrics and logs |
| `region` | Azure region identifier (e.g. `eastus`) |
| `environment` | Environment tag (e.g. `prod`, `staging`) |
| `adx.name` | Name of the ADX cluster |
| `adx.url` | ADX cluster endpoint URL (e.g. `https://mycluster.eastus.kusto.windows.net`) |
| `adx_hub_url` | ADX hub/federation cluster endpoint URL |
| `ingestor.client_id` | Azure Workload Identity client ID for the ingestor. The corresponding identity must have the **Database Admin** role on each pre-existing database in the ADX cluster. |
| `alerter.auth_msi_id` | Managed identity ID for the alerter (required when `alerter.enabled=true`) |
| `alerter.alerter_address` | URL the alerter forwards alerts to (required when `alerter.enabled=true`) |

### Container images

Each component's image is configurable via `<component>.image.repository`, `<component>.image.tag`, and `<component>.image.pullPolicy`. When `repository` is null (default), it resolves to `<image_registry>/<component>`.

| Key | Default | Description |
|-----|---------|-------------|
| `image_registry` | `ghcr.io/azure/adx-mon` | Default registry/repository prefix used when a component's `image.repository` is unset |
| `<component>.image.repository` | `null` (uses `<image_registry>/<component>`) | Override image repository per component |
| `<component>.image.tag` | `latest` | Image tag |
| `<component>.image.pullPolicy` | `IfNotPresent` (`Always` for operator) | Pull policy |

### Component resources

Each component exposes a Kubernetes-style `resources` block. The collector also has a separate `singleton_resources` block for its singleton Deployment (cluster-wide scrape):

```yaml
collector:
  resources:
    requests:
      cpu: 50m
      memory: 100Mi
    limits:
      cpu: 500m
      memory: 2000Mi
  singleton_resources:
    requests:
      cpu: 50m
      memory: 100Mi
    limits:
      cpu: 500m
      memory: 2000Mi
```

### Log destinations

Each component routes its own logs to a configurable database/table via the `<component>.log_destination` value (format: `<database>:<table>`):

| Key | Default |
|-----|---------|
| `collector.log_destination` | `Logs:Collector` |
| `ingestor.log_destination` | `Logs:Ingestor` |
| `alerter.log_destination` | `Logs:Alerter` |
| `operator.log_destination` | `Logs:Operator` |

### Collector config tunables

A subset of collector config options are exposed under `collector.config`:

| Key | Default | Description |
|-----|---------|-------------|
| `collector.config.max_connections` | `100` | Maximum number of connections to accept |
| `collector.config.max_batch_size` | `10000` | Maximum number of samples per batch |
| `collector.config.wal_flush_interval_ms` | `1000` | WAL flush interval (ms) |
| `collector.config.metrics_database` | `Metrics` | Database for prometheus-scrape, prometheus-remote-write, and otel-metric |
| `collector.config.logs_database` | `Logs` | Database for journal log targets |

For deeper customization, replace `templates/collector-config.yaml` in your own values overlay or fork.

### Other optional values

| Key | Default | Description |
|-----|---------|-------------|
| `cloud` | `azure` | Cloud identifier (e.g. `azure`, `azureusgovernment`) |
| `adx.databases` | Logs + Metrics | Databases to route telemetry to, by type. Each entry has `name`, `telemetryType` (`Logs` or `Metrics`), and optional `retentionInDays` (default `30`). |
| `collector.enabled` | `true` | Deploy the collector DaemonSet |
| `collector.ingestor_endpoint` | `https://ingestor.<release-namespace>.svc.cluster.local` | Ingestor endpoint the collector forwards telemetry to. Leave unset to use the in-namespace default. |
| `collector.insecure_skip_verify` | `true` | Skip TLS verification on the collector→ingestor connection. |
| `ingestor.enabled` | `true` | Deploy the ingestor StatefulSet |
| `ingestor.replicas` | `1` | Number of ingestor replicas |
| `ingestor.insecure_skip_verify` | `true` | Skip TLS verification on the ingestor→ADX connection. |
| `alerter.enabled` | `false` | Deploy the alerter |
| `operator.enabled` | `false` | Deploy the operator |
| `operator.client_id` | `null` | Azure Workload Identity client ID for the operator. Required when `operator.enabled=true`. |

### ADX database routing

Telemetry is routed to ADX databases based on the `adx.databases` list. Each entry specifies a database `name` and a `telemetryType` of either `Logs` or `Metrics`. Multiple databases of the same type are supported.

```yaml
adx:
  name: my-adx-cluster
  url: https://my-adx-cluster.eastus.kusto.windows.net
  databases:
    - name: Logs
      telemetryType: Logs
      retentionInDays: 30
    - name: Metrics
      telemetryType: Metrics
      retentionInDays: 30
```

### Node affinity

The ingestor, alerter, and operator all support optional node affinity via their respective `affinity` blocks:

```yaml
ingestor:
  affinity:
    required: true   # true = requiredDuringScheduling, false = preferred
    key: agentpool
    value: system
```

## CRDs

The chart installs the following CRDs:

- `adxclusters.adx-mon.azure.com`
- `alerters.adx-mon.azure.com`
- `alertrules.adx-mon.azure.com`
- `collectors.adx-mon.azure.com`
- `functions.adx-mon.azure.com`
- `ingestors.adx-mon.azure.com`
- `managementcommands.adx-mon.azure.com`
- `metricsexporters.adx-mon.azure.com`
- `summaryrules.adx-mon.azure.com`

Each CRD carries the `helm.sh/resource-policy: keep` annotation, so `helm uninstall` leaves them (and any custom resources of those kinds) intact. To fully remove them, delete each CRD manually after uninstalling the release, e.g.:

```bash
kubectl delete crd \
  adxclusters.adx-mon.azure.com \
  alerters.adx-mon.azure.com \
  alertrules.adx-mon.azure.com \
  collectors.adx-mon.azure.com \
  functions.adx-mon.azure.com \
  ingestors.adx-mon.azure.com \
  managementcommands.adx-mon.azure.com \
  metricsexporters.adx-mon.azure.com \
  summaryrules.adx-mon.azure.com
```

## Contributing

This chart covers the core deployment scenario but contributions for additional use cases (custom TLS, additional ingress configurations, multi-cluster setups, etc.) are welcome. Please see [CONTRIBUTING.md](../../CONTRIBUTING.md) or open an issue at [Azure/adx-mon](https://github.com/Azure/adx-mon/issues).
