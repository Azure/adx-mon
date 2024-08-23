# Quick Start

This guide will deploy ADX-Mon on an Azure Kubernetes Service (AKS) cluster and send collected telemetry
to an Azure Data Explorer cluster.  It will deploy all components within the cluster and demonstrate 
how to enable telemetry collection on a pod and query it from Azure Data Explorer.

## Pre-Requisites

You will need the following to complete this guide.

* An AKS cluster
* An Azure Data Explorer cluster
* A Linux environment with Azure CLI installed

These clusters should be in the same region for this guid.  You should have full admin access to both clusters.

## Deploy ADX-Mon

```sh
bash <(curl -s  https://raw.githubusercontent.com/Azure/adx-mon/main/build/k8s/bundle.sh)
```

This script will prompt you for the name or you AKS and ADX cluster and configure them to accept telemetry from ADX-Mon
components. 

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


### Setup Dashboards

Any ADX compatible visualization tool can be used to visualize collected telemetry.  ADX Dashboards is a simple solution
that is native to ADX.  You can also use Azure Managed Grafana with the Azure Data Explorer datasource to leverge Grafana
powerful visualization capabilities.
