# Quick Start

This guide will walk you through the steps needed to deploy ADX-Mon on an Azure Kubernetes Service (AKS) cluster.  It
will deploy all components within the cluster and demonstrate how to enable telemetry collection on a pod and 
query it from Azure Data Explorer.

## Pre-Requisites

You will need the following to complete this guide.

* An AKS cluster
* An Azure Data Explorer cluster
* An MSI with admin permissions to the ADX cluster

## Setup Azure Data Explorer

```sh
# TODO: Add instructions for setting up ADX
```

## Setup AKS Cluster

```sh
````


## Deploy Collector

```sh

```

## Deploy Ingestor
```sh
# TODO: Add instructions for deploying the ingestor
```

## Annotate Your Pods

Telemetry can be ingested into ADX-Mon by annotating your pods with the appropriate annotations or shipping it through
OTEL endpoints.  The simplest model is to annotate your pods with the appropriate annotations.

### Metrics

ADX-Mon collector support scraping Prometheus style endpoints directly.  To enable this, annotate your pods with the
```yaml
adx-mon/metrics: "true"
adx-mon/port: "8080"
adx-mon/path: "/metrics"
```


```sh
# TODO: Add instructions for annotating pods with metrics
```

### Logs

```sh
# TODO: Add instructions for annotating pods with logs
```

### Query Your Data

```sh
# TODO: Add instructions for querying data
```

### Deploy Alerter

```sh
# TODO: Add instructions for deploying the alerter
```

### Setup Dashboards

Any ADX compatible visualization tool can be used to visualize collected telemetry.  ADX Dashboards is a simple solution
that is native to ADX.  You can also use Azure Managed Grafana with the Azure Data Explorer datasource to leverge Grafana
powerful visualization capabilities.
