# Concepts

## Overview

ADX-Mon is a fully managed observability solution that supports metrics, logs and traces in a unified stack.
The entrypoint to ADX-Mon is the `collector` which is deployed as a daemonset in your Kubernetes cluster.

The collector is responsible for collecting metrics, logs and traces from your Kubernetes cluster and sending
them to the `ingestor` endpoint which handles the ingestion of data into Azure Data Explorer (ADX).

All collected data is translated to ADX tables.  Each table has a consistent schema that can be extended through
[`update policies`](https://learn.microsoft.com/en-us/azure/data-explorer/kusto/management/updatepolicy) to pull
commonly used labels and attributes up to top level columns.

These tables are all queried with [KQL](https://learn.microsoft.com/en-us/azure/data-explorer/kusto/query/).  KQL
queries are used for analysis, alerting and visualization.

## Components

### Collector
### Ingestor
### Alerter
### Azure Data Explorer
### Grafana


## Telemetry

### Metrics

Metrics track a numeric value over time with associated labels to identify series.  Metrics are collected from
Kubernetes via the [Prometheus](https://prometheus.io/) scrape protocol as well as received via prometheus
remote write protocol and [OTLP](https://opentelemetry.io/docs/specs/otlp/) metrics protocol.

Metrics are translated to a distinct table per metric.  Each metric table has the following columns:

* `Timestamp` - The timestamp of the metric.
* `Value` - The value of the metric.
* `Labels` - A dynamic column that contains all labels associated with the metric.
* `SeriesId` - A unique ID for the metric series that comprises the `Labels` and metric name.

Labels may have common identifying attributes that can be pulled up to top level columns via update policies.  For
example, the `pod` label may be common to all metrics and can be pulled up to a top level `Pod` column.

### Logs

### Traces

### Continuous Profiling

### Alerts

Alerts are defined through a Kubernetes CRD called `AlertRule`.  This CRD defines the alerting criteria and the
notification channels that should be used when the alert is triggered.

Alerts are triggered when the alerting criteria is met.  The alerting criteria is defined as a KQL query that is
executed against the ADX cluster.  The query is executed on a schedule and if the query returns any results, the
alert triggers.  Each row of the result translates into an alert notification.

Below is a sample alert on a metric.

```yaml
---
apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: unique-alert-name
  namespace: alert-namespace
spec:
  database: SomeDatabase
  interval: 5m
  query: |
    let _from=_startTime-1h;
    let _to=_endTime;
    KubePodContainerStatusWaitingReason
    | where Timestamp between (_from .. _to)
    | where ...
    | extend Container=tostring(Labels.container), Namespace=tostring(Labels.namespace), Pod=tostring(Labels.pod)
    | extend Severity=3
    | extend Title="Alert tittle"
    | extend Summary="Alert summary details"
    | extend CorrelationId="Unique ID to correlate alerts"
  autoMitigateAfter: 1h
  destination: "alerting provider destination"
  criteria:
    cloud:
      - AzureCloud
```

All must have the following fields:

* `database` - The ADX database to execute the query.
* `interval` - The interval at which the query should be executed.
* `query` - The KQL query to execute.
* `destination` - The destination to send the alert to.  This is provider specific.

The query must return a table with the following columns:

* `Severity` - The severity of the alert.  This is used to determine the priority of the alert.
* `Title` - The title of the alert.
* `Summary` - The summary of the alert.
* `CorrelationId` - A unique ID to correlate alerts.  A correlation ID is necessary to prevent duplicate alerts from
being sent to the destination.  If one is not specified, a new alert will be created each interval.

Optionally, the query can return the following fields:

* `autoMitigateAfter` - The amount of time after the alert is triggered that it should be automatically mitigated if it
has not correlated.  If a `CorrelationId` is specified, this field is ignored.
* `criteria` - A list of criteria that must be met for the alert to trigger.  If not specified, the alert will trigger
in all environments.  This is useful for alerts that should only trigger in a specific cloud or region.  The available
criteria options are determined by the `alerter` tag settings.
