# Static Pods

## Background

In some scenarios, Pods cannot be annotated for log collection, particularly in cases involving static Pod manifests (such as kube-apiserver). This document describes a mechanism to configure log scraping for these Pods without requiring changes to their manifests.

### Proposed Solution

Introduce a configuration block that specifies which Pod log targets should be collected. When a target configuration matches a static Pod, the appropriate logs will be included in the overall log collection pipeline.

## Configuration

A sample configuration that aims to scrape kube-apiserver logs is provided below. The configuration itself remains unchanged:

```toml
[[host-log.static-pod-target]]
namespace = "kube-system"
label-targets = { "component" = "kube-apiserver" }
container = "apiserver"
log-type = "kubernetes"
database = "Logs"
table = "KubeAPIServer"
parsers = ["klog"]
```