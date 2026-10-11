# Real Azure ADX-Mon end-to-end environment

Two independent entry points: create the **paid Azure resources once**, then
build/deploy/test any local branch as many times as necessary. These scripts do
not use the published `latest` images or the remotely downloaded quick-start
bundle. Run them from a checkout of the branch you want to test.

## Prerequisites

* Python 3.9+, Azure CLI (`az login` and `az extension add -n kusto`),
`kubectl`, `curl`, Git, and network access to Azure. Docker is **optional**: if its
  daemon is not running, `test.sh` uses remote ACR Tasks builds.
* Permission to create a resource group, AKS, ACR, Kusto, and a Kusto cluster
  principal assignment in the chosen subscription. The Azure CLI user must
  be able to query and alter the new Kusto databases.
* Available VM and ADX quota in the region. AKS defaults to one node: prefer
  D4s_v3, then D4s_v4/v5/v6; if those are restricted or out of quota, choose
  another available D/E-series VM with at least four cores and 16 GiB (for
  example, D4ads_v6). Selection checks regional SKU restrictions and both
  family and regional vCPU quotas. ACR is Basic; ADX prefers L8as_v3, then
  other storage-optimized sizes, at their minimum supported **two-node**
  capacity. ADX is billed for both nodes.
* The resource group must not already exist. ACR and Kusto names are generated
  from the group name; use a unique group name in each subscription.

## One-time setup

```bash
tools/dev/azure-test-env/cluster.sh create \
  --subscription '<subscription-id>' --region eastus --resource-group adxmon-e2e-jw
tools/dev/azure-test-env/cluster.sh status --resource-group adxmon-e2e-jw
```

The AKS kubelet identity gets Kusto **AllDatabasesAdmin**, and AKS gets ACR
pull permission via `--attach-acr`. Kusto starts with streaming disabled. The
CLI prints the selected sizes before creating anything. Provisioning can take
many minutes; resources keep accruing charges until deleted.

Optional `create` flags: `--aks-sku Standard_D4ads_v6`, `--kusto-sku`.
An explicitly supplied AKS size is still checked for regional restrictions,
minimum memory/cores, and quota, with the specific reason shown if it cannot
be used. Failures preserve the resource group and state record. Re-run the
**same `create` command** to resume: it verifies and reuses the recorded AKS
and ACR, then continues with Kusto and any remaining setup. It will not adopt
an existing resource group without a matching state record or resize existing
clusters. For the specific Canada Central example, `--aks-sku
Standard_D4ads_v6` selects the available four-core fallback.

## Repeatable branch test

```bash
git switch realtime-ingestion        # or any branch under test
tools/dev/azure-test-env/test.sh run --resource-group adxmon-e2e-jw

# Or run just one phase after a fix:
tools/dev/azure-test-env/test.sh baseline --resource-group adxmon-e2e-jw
tools/dev/azure-test-env/test.sh realtime --resource-group adxmon-e2e-jw
```

To A/B test pod-tail latency using the **same newly built image tag**, first
run `test.sh realtime --resource-group ... --realtime-log-batch-wait-ms 1000`
(the former one-second behavior) on a warmed cluster. Repeat `realtime` with
`--realtime-log-batch-wait-ms 100 --reuse-tag <first-run-tag>` to avoid a
rebuild. Compare `pod_logs` p50/p95/max, queued control, scrape latency and
component errors. Use a fresh build, **not** the old 5e45ace9 image tag.

The default `--build-mode auto` uses local Docker when its daemon is running;
otherwise it uploads the local checkout as an ACR build context and builds
`linux/amd64` images remotely. Choose `--build-mode acr` to force daemon-free
remote builds, or `--build-mode docker` to require local Docker. ACR Tasks
builds may incur additional charges. `.dockerignore` excludes local state,
reports, Git metadata and cache files from both build contexts.
If a run fails after its images were pushed (for example, during Kusto setup),
use `--reuse-tag <tag>` to skip all three builds on the next attempt. The
runner verifies that each tagged image exists in ACR and writes a new report
directory; **omit this option after changing branch code or the workload** so
new images are built. The 100 ms tail batching change and bounded scrape
sample lifetime require fresh collector/workload images.

`run` builds **collector, ingestor and the small signal workload** from the
current checkout, pushes commit/run-tagged images to the environment's ACR,
deploys the checked-in Kubernetes manifests with those images, then:

1. Applies a one-minute Kusto database ingestion batching policy and deploys
   collector/ingestor **without** realtime tables. Sends numbered samples
   through pod-tailed JSON logs, OTLP/HTTP protobuf logs, OTLP/HTTP protobuf
   metrics, and a one-second annotated Prometheus scrape target. In the test
   environment, the Prometheus scrape keeps only `e2e_scraped_sample` so a
   fresh cluster is not flooded with hundreds of irrelevant metric schemas
   (`default-drop-metrics` must be true for the keep rule to work). Each sample
   is exposed for two seconds rather than indefinitely.
   OTLP is sent to a ClusterIP Service selecting only the collector DaemonSet
   (the node hostPort is not reachable from pods on some AKS networks). The
   optional singleton collector has a separate hostPath WAL on one-node AKS,
   avoiding concurrent access to the DaemonSet collector's WAL.
2. Enables Kusto streaming, configures the same four destination tables on
   both components, waits for schema and streaming-policy warm-up, and runs
   fresh numbered samples. A fifth OTLP log table remains queued as a control.
3. Polls Kusto concurrently with emission. Each row has a run ID, sequence and
   pod-generated millisecond timestamp; the report records first query
   visibility from the machine running `test.sh`. Each path polls independently,
   recording its last negative and first positive query times in `result.json`.
   Host/pod clock skew and
   Kusto query round-trip time are part of the measured latency. Every selected
   realtime path must become visible in at most **2 seconds** and have a
   positive `streamed` outcome counter delta without unexpected fallback.
   Missing rows and unexpected ingestor/collector errors fail either phase.

The default is five signals, three seconds apart; adjust with `--count N` and
`--timeout SECONDS`. The scraper holds each gauge value until the next signal.
New tables and streaming mappings can take several minutes to become eligible;
both phases wait up to ten minutes for all five test tables to ingest a warm-up
sample before measuring. Warm-up traffic is **not** counted as measured traffic.
`--timeout` controls only the subsequent per-phase measurement deadline
(240 seconds by default); `--warmup-timeout` controls the warm-up limit. If
warm-up times out, inspect the saved ingestor/collector
logs and WAL backlog rather than treating missing tables as latency results.

Results go to `.adx-mon-test-env/results/<resource-group>/<commit>-<UTC>/`:
`environment.json`, phase `result.json`, `report.md`, `collector.log`, and
`ingestor.log`. Individual rows, missing/duplicate counts, per-path median,
p95 and maximum latency, and streaming counter changes are included. Reports
capture all collector/ingestor log lines in each phase (`kubectl logs --tail=-1`),
not just the ten-line default when using a selector. Results include final row
counts after the queued control is visible without changing the first-visibility
timestamps. Reports
can be regenerated offline:

```bash
tools/dev/azure-test-env/test.sh report --results .adx-mon-test-env/results/adxmon-e2e-jw/<run>
```

Re-running `run` restores queued-only **ADX-Mon configuration** for its first
phase, even if an earlier run left the Kusto cluster streaming feature enabled;
turning that cluster feature off can take hours and is unnecessary for a
queued-path test. The test script never recreates or deletes Azure resources.

### Scrape parallelism evaluation

The current test target is first in the collector's sorted target list. In
120 observed one-second cycles before the selective-drop fix, its scrape log
preceded the next target by about **0.4 ms**. Its scrape-to-send-complete time
was 208 ms median / 315 ms p95, including 108 ms median remote-write time;
roughly 98 ms median was spent fetching/parsing other targets before sending.
Each cycle still forwarded **8,158 unrelated series** because a keep rule
alone did not enable default dropping. The harness now sets both
`default-drop-metrics = true` and the keep rule. Parallel target fetching
cannot shorten a one-second wait for a target already fetched first, and at
most saves the relatively small time behind other targets. We have **not**
introduced concurrency into the production scraper on this evidence. Revisit
bounded parallelism if profiling with more/slow targets shows that target
ordering or cycle durations, rather than scrape cadence, dominate p95.

## Cleanup

```bash
tools/dev/azure-test-env/cluster.sh delete --resource-group adxmon-e2e-jw --yes
```

This deletes the **entire recorded resource group**, including AKS, ACR, Kusto
and their data. It does not delete local reports. Inspect `status` first.
