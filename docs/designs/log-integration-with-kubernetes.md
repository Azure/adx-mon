# Log Integration with Kubernetes

## Summary

Collector is designed to consume container logs based on annotations set on their containing pod (e.g. `adx-mon/scrape: true`, `adx-mon/log-database`, `adx-mon/log-table`). Collector is also intended to enrich logs from containers with the Kubernetes annotations and labels on a pod for use in transforms. This design attempts to hep reconciling the different states that can be observed from the Kubernetes Control Plane and the host while maintaining performance.

## Problem definitions

### Dynamic pod metadata
Metadata associated with pods, and its containers, is dynamic. We need ways to communicate this data to the log enrichment process in a timely manner without impacting the hot path of reading and batching logs. Maintaining a shared data structure and synchronizing with mutexes is too slow for this system.

### Cleaning tailers and on-disk state without dropping end-of-process logs
The set of containers on a host is also highly dynamic, making it important to dispose of on-disk storage and retire tailers to not consume resources when containers no longer exist. However, due to the distributed nature of Kubernetes, cleaning this state at the time of container deletion in the control plane makes it likely to miss log messages from these containers on the host.

The Kubernetes control plane maintains the metadata associated with a pod, but the logs themselves reside solely on the disk where the pod is running. The process of creating, appending to, and deleting these log files is asynchronous from the control-plane mechanisms of Kubernetes. Pods can take some time to fully shut down and CRI impementations may take some time to fully flush logs to disk. The source of truth for logs is on the host, but the source of truth for information about the source of the logs is maintained in the control plane.

``` mermaid
flowchart LR
  subgraph Kubernetes Control Plane
  api[API Server]
  end

  subgraph Kubernetes Node
  kubelet[Kubelet] -- CRUD containers --> cri[CRI Driver]
  cri -- CRUD log files --> log[Log Files]
  collector[Collector]
  end

  api -- CRUD pod --> api

  collector -- pod metadata --> api
  collector -- tail --> log
  api -- start/stop pod --> kubelet
```

Some concrete situations we are attempting to compensate for:

1. The gap in time between a pod being deleted in Kubernetes and the logs from the pod fully flushing to disk and being consumed by Collector.

2. Pods are commonly created and removed from hosts. It is important that we clean state related to deleted pods after we are done with that state, in particular tail cursor files and any other on-disk persistance we use.

3. Collector instances may restart while other pods are also shutting down. Best effort we should be able to consume the last logs of a pod that was shut down even while losing in-memory state or the ability to get pod metadata from the Kubernetes control plane.

## Approaches

### Handling updates to pod metadata
Pod annotations can be dynamically modified which can affect the attributes we expect to apply to logs, including the expected destination ADX database and table. It can also cause us to start scraping new container logs or to exclude containers from being scraped.

We will immediately delete our container metadata and notify `TailSource` to remove a target when a container is made non-scrapable (e.g. the `adx-mon/scrape` annotation is removed or set to false), but the container still exists.

For changes in container metadata that are included in logs that do not change scrapability, we will send via a channel a new updated set of metadata that should be applied to logs from this container. This channel will be consumed asynchronously by the hot-loop within the tailing process to update its own local state. This avoids the need for mutexes in the hot path, but still allows the tailer to receive these changes in near real-time. This extends the actor metaphor for the tailer, where it consumes 3 types of messages within a single goroutine:

1. A new log line is ready.
2. We are being shut down.
3. We have gotten a new set of fields we should apply to the log.

### Delayed cleaning of state
To account for this skew, we will preserve Pod metadata locally on disk (like log file cursors). This will allow us to access this metadata at any time, even when the Kubernetes API server no longer preserves this information. To clean these files, we will utilize an expiration time placed within this pod metadata file that will be reaped in a polling fashion. This expiration date will be set with a future timestamp (e.g. 10m) by a few flows:

1. When we recieve a notification from the Kubernetes API server that a container has been deleted.
2. On startup, after inspecting all of the on-disk container metadata we observe that the container itself no longer exists.

When our periodic garbage collection process discovers that a piece of container metadata is past expiration time, it will delete the file and notify `TailSource` to stop the tailing process for that file and to remove the cursor file. This gap between preserving an expiration timestamp and removing the tailer and its state gives collector time to consume the rest of the logs from a container on a best-effort basis, even in the face of restarts.
