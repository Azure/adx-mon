#!/usr/bin/env python3
"""A/B soak tests against cluster.sh resources.

build    builds ingestor/collector images from a git ref (and the workload from
         this checkout), tagged by commit, so two refs can be compared.
soak     deploys one tag, streams numbered signals on six ingestion paths at a
         fixed rate under background load, injects a fault, and computes each
         row's end-to-end latency in Kusto as ingestion_time() - emitted_ms.
compare  renders two or more soak results side by side.
"""
import argparse
import contextlib
import datetime as dt
import socket
import subprocess
import hashlib
import json
import math
import re
import sys
import threading
import time
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

import common
import test as harness
from common import ROOT, STATE_DIR, az_json, kusto, load, output, run, save

PATHS = tuple(harness.SOAK_TABLES)
# The scraper samples whichever signal is current, so scraped seqs are a subset
# and a seq can be scraped twice; neither is a loss or a duplicate.
SAMPLED = {"scraped_metrics"}
SENT = ("otlp_logs", "queued_control", "otlp_metrics", "remote_write")
SCENARIOS = ("steady", "ingestor-kill", "ingestor-rollout", "ingestor-stall",
             "slow-target", "collector-restart", "chunked-otlp")
COUNTER = re.compile(r"(error|fail|drop|reject|retr|timeout|shed|skip|panic|evict|throttl)", re.I)
WORKLOAD_SOURCES = ("workload/main.go", "workload/soak.go", "Dockerfile.workload")


def workload_tag():
    digest = hashlib.sha256()
    for name in (*WORKLOAD_SOURCES, "../../../go.sum"):
        digest.update((harness.HERE / name).read_bytes())
    return "wl-" + digest.hexdigest()[:12]


def acr_tags(state, component):
    result = run("az", "acr", "repository", "show-tags", "--name", state["names"]["acr"],
                 "--repository", f"adx-mon/{component}", "--subscription", state["subscription"],
                 "-o", "json", check=False)
    return set(json.loads(result.stdout)) if result.returncode == 0 else set()


def build_ref(state, ref, mode):
    sha = output("git", "-C", ROOT, "rev-parse", ref + "^{commit}")
    tag = sha[:12]
    if all(tag in acr_tags(state, c) for c in ("ingestor", "collector")):
        print(f"{ref} already built as {tag}")
    else:
        tree = STATE_DIR / "worktrees" / tag
        if not tree.exists():
            run("git", "-C", ROOT, "worktree", "add", "--detach", tree, sha)
        harness.build(state, tag, mode, root=tree, components=("ingestor", "collector"))
    wl = workload_tag()
    if wl not in acr_tags(state, "workload"):
        harness.build(state, wl, mode, components=("workload",))
    return tag, wl


def percentile(ordered, p):
    """Nearest-rank percentile of an ascending list."""
    if not ordered:
        return None
    return ordered[max(0, min(len(ordered), math.ceil(len(ordered) * p / 100)) - 1)]


def soak_query(table, path, run_id, since):
    record = {"pod_logs": "parse_json(tostring(Body))", "otlp_logs": "Attributes",
              "queued_control": "Attributes"}.get(path, "Labels")
    # OTLP metric labels are rendered as protobuf text (string_value:"..."), so
    # match run ids with contains and extract the digits of seq/emitted_ms.
    return (f"{table} | where ingestion_time() > datetime({since}) | extend R = {record} "
            f"| where tostring(R.run_id) contains '{run_id}' "
            "| summarize Rows=count(), Ingested=min(ingestion_time()) "
            "by Seq=toint(extract('[0-9]+', 0, tostring(R.seq))), "
            "Emitted=tolong(extract('[0-9]+', 0, tostring(R.emitted_ms))) "
            "| project Seq, Emitted, Rows, IngestedMs=datetime_diff('millisecond', Ingested, datetime(1970-01-01))")


def soak_rows(state, path, run_id, since):
    """{seq: [emitted_ms, ingested_ms, rows]} for one path."""
    db, table = harness.SOAK_TABLES[path]
    found = {}
    for row in kusto(state["endpoint"], db, soak_query(table, path, run_id, since)):
        if row.get("Seq") is None or row.get("Emitted") is None:
            continue
        seq = int(row["Seq"])
        previous = found.get(seq)
        rows = int(row["Rows"]) + (previous[2] if previous else 0)
        ingested = row.get("IngestedMs")
        if previous and previous[1] is not None and (ingested is None or previous[1] < ingested):
            ingested = previous[1]
        found[seq] = [int(row["Emitted"]), ingested, rows]
    return found


def expected_seqs(path, status):
    """Seqs that must reach Kusto on path: all emitted, minus failed sends."""
    if path in SAMPLED:
        return set()
    failed = set(status.get("failed", {}).get(path, [])) if path in SENT else set()
    return set(range(1, status["emitted"] + 1)) - failed


def path_stats(path, rows, status, windows):
    expected = expected_seqs(path, status)
    latencies = sorted(r[1] - r[0] for r in rows.values() if r[1] is not None)
    stats = {"samples": len(latencies),
             "missing": len(expected - set(rows)),
             "duplicates": 0 if path in SAMPLED else sum(1 for r in rows.values() if r[2] > 1),
             "failed_sends": len(status.get("failed", {}).get(path, [])),
             "no_ingestion_time": sum(1 for r in rows.values() if r[1] is None)}
    for p in (50, 95, 99, 99.9):
        stats[f"p{p:g}"] = percentile(latencies, p)
    stats["max"] = latencies[-1] if latencies else None
    stats["windows"] = {}
    for name, (start, end) in windows.items():
        inside = sorted(r[1] - r[0] for r in rows.values()
                        if r[1] is not None and start <= r[0] < end)
        stats["windows"][name] = {"samples": len(inside), "p50": percentile(inside, 50),
                                  "p99": percentile(inside, 99),
                                  "max": inside[-1] if inside else None}
    return stats


def per_minute(rows, start_ms):
    buckets = {}
    for emitted, ingested, _ in rows.values():
        if ingested is not None:
            buckets.setdefault((emitted - start_ms) // 60000, []).append(ingested - emitted)
    return {str(m): {"p50": percentile(sorted(v), 50), "p99": percentile(sorted(v), 99),
                     "max": max(v), "samples": len(v)} for m, v in sorted(buckets.items())}


def parse_counters(text):
    """Sum each error-like counter family over its labels."""
    totals = {}
    for line in text.splitlines():
        if not line or line.startswith("#"):
            continue
        name = line.split("{", 1)[0].split(" ", 1)[0]
        if name.endswith(("_bucket", "_sum")) or not COUNTER.search(name):
            continue
        try:
            totals[name] = totals.get(name, 0.0) + float(line.rsplit(" ", 1)[-1])
        except ValueError:
            pass
    return totals


def pods(selector):
    items = json.loads(output("kubectl", "-n", "adx-mon", "get", "pods", "-l", selector, "-o", "json"))["items"]
    return {p["metadata"]["name"]: p for p in items}


def snapshot_counters():
    snapshot = {}
    # The collector serves /metrics on its listener; the ingestor on 9091.
    for selector, port in (("app=ingestor", 9091), ("adxmon=collector", 8080)):
        for name in pods(selector):
            try:
                with harness.Forward("pod/" + name, port) as forward:
                    with urllib.request.urlopen(forward.url + "/metrics", timeout=15) as response:
                        snapshot[name] = parse_counters(response.read().decode())
            except Exception as e:  # a pod mid-restart has no metrics
                print(f"Metrics unavailable for {name}: {e}", flush=True)
    return snapshot


def counter_delta(before, after):
    """Per-family increase summed over pods; a pod new since before counts from zero."""
    delta = {}
    for name, families in after.items():
        for family, value in families.items():
            increase = value - before.get(name, {}).get(family, 0.0)
            if increase < 0:  # restarted in place
                increase = value
            delta[family] = delta.get(family, 0.0) + increase
    return {k: v for k, v in sorted(delta.items()) if v}


def component(pod):
    if pod.startswith("ingestor"):
        return "ingestor"
    if pod.startswith("collector-singleton"):
        return "collector-singleton"
    return "collector" if pod.startswith("collector") else None


def parse_quantity(value):
    if value.endswith("m"):
        return float(value[:-1])
    if value.endswith("Mi"):
        return float(value[:-2])
    if value.endswith("Gi"):
        return float(value[:-2]) * 1024
    if value.endswith("Ki"):
        return float(value[:-2]) / 1024
    return float(value)


def sample_top(samples, stop):
    while not stop.wait(30):
        result = run("kubectl", "top", "pods", "-n", "adx-mon", "--no-headers", check=False)
        for line in result.stdout.splitlines():
            fields = line.split()
            if len(fields) >= 3 and component(fields[0]):
                samples.append({"pod": fields[0], "cpu_m": parse_quantity(fields[1]),
                                "mem_mi": parse_quantity(fields[2]), "ms": int(time.time() * 1000)})


def resource_summary(samples):
    out = {}
    for name in sorted({component(s["pod"]) for s in samples}):
        mine = [s for s in samples if component(s["pod"]) == name]
        out[name] = {"cpu_m_avg": sum(s["cpu_m"] for s in mine) / len(mine),
                     "cpu_m_max": max(s["cpu_m"] for s in mine),
                     "mem_mi_avg": sum(s["mem_mi"] for s in mine) / len(mine),
                     "mem_mi_max": max(s["mem_mi"] for s in mine)}
    return out


def error_classes(lines):
    """Group log error lines by component and message with numbers masked."""
    classes = {}
    for line in lines:
        pod = re.match(r"\[pod/([^/\]]+)", line)
        name = component(pod.group(1)) if pod else None
        body = line.split("] ", 1)[-1]
        try:
            message = json.loads(body).get("msg", body)
        except (ValueError, AttributeError):
            message = body
        # Mask every token containing a digit: ids, counts, durations, addresses.
        message = re.sub(r"[\w.:/=-]*\d[\w.:/=-]*", "#", str(message))[:140]
        key = f"{name or 'other'}: {message}"
        classes[key] = classes.get(key, 0) + 1
    return dict(sorted(classes.items(), key=lambda kv: -kv[1]))


def management_rows(state, db, command):
    try:
        return kusto(state["endpoint"], db, command, True)
    except RuntimeError as e:
        return [{"error": str(e)[:300]}]


def kubectl_apply(manifest):
    run("kubectl", "apply", "-f", "-", input=json.dumps(manifest))


def workload_node():
    return json.loads(output("kubectl", "-n", "adx-mon", "get", "pod", "adx-mon-e2e-workload",
                             "-o", "json"))["spec"]["nodeName"]


def deploy_load(state, tag, a):
    if a.load_replicas == 0:
        run("kubectl", "-n", "adx-mon", "delete", "deployment", "adx-mon-e2e-load", "--ignore-not-found")
        return
    env = {"HOST_IP": "adx-mon-e2e-collector.adx-mon.svc.cluster.local",
           "LOAD_TABLES": a.load_tables, "LOAD_SERIES": a.load_series,
           "LOAD_LOG_TABLES": a.load_log_tables, "LOAD_LOGS": a.load_logs, "LOAD_INTERVAL": "1s"}
    labels = {"app": "adx-mon-e2e-load"}
    kubectl_apply({"apiVersion": "apps/v1", "kind": "Deployment",
                   "metadata": {"name": "adx-mon-e2e-load", "namespace": "adx-mon"},
                   "spec": {"replicas": a.load_replicas, "selector": {"matchLabels": labels},
                            "template": {"metadata": {"labels": labels}, "spec": {
                                "topologySpreadConstraints": [{
                                    "maxSkew": 1, "topologyKey": "kubernetes.io/hostname",
                                    "whenUnsatisfiable": "ScheduleAnyway",
                                    "labelSelector": {"matchLabels": labels}}],
                                "containers": [{"name": "load", "image": harness.image(state, "workload", tag),
                                                "env": [{"name": k, "value": str(v)} for k, v in env.items()]}]}}}})
    run("kubectl", "-n", "adx-mon", "rollout", "status", "deployment/adx-mon-e2e-load", "--timeout=5m")


def deploy_slow_targets(state, tag, count, delay):
    run("kubectl", "-n", "adx-mon", "delete", "pod", "-l", "app=adx-mon-e2e-slow", "--ignore-not-found", "--wait=true")
    if not count:
        return
    node = workload_node()
    for i in range(count):
        # Same node as the workload, so the same collector scrapes both.
        kubectl_apply({"apiVersion": "v1", "kind": "Pod", "metadata": {
            "name": f"adx-mon-e2e-slow-{i}", "namespace": "adx-mon", "labels": {"app": "adx-mon-e2e-slow"},
            "annotations": {"adx-mon/scrape": "true", "adx-mon/port": "8088", "adx-mon/path": "/slow-metrics"}},
            "spec": {"nodeName": node, "containers": [{"name": "slow", "image": harness.image(state, "workload", tag),
                                                       "env": [{"name": "SLOW_DELAY", "value": delay},
                                                               {"name": "HOST_IP", "value": "unused.invalid"}]}]}})
    run("kubectl", "-n", "adx-mon", "wait", "--for=condition=Ready", "pod", "-l", "app=adx-mon-e2e-slow", "--timeout=5m")


def stall_manifest(node, seconds, image):
    # SIGSTOP from the host PID namespace: a signal from inside the container's
    # own namespace cannot stop its PID 1. The kernel keeps accepting TCP
    # connections into the listen backlog, which is exactly a stalled peer.
    script = (f"pid=$(pgrep -x ingestor); [ -n \"$pid\" ] || exit 3; kill -STOP $pid; "
              f"echo stopped $pid; sleep {seconds}; kill -CONT $pid; echo resumed $pid")
    return {"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "adx-mon-e2e-stall", "namespace": "adx-mon"},
            "spec": {"nodeName": node, "hostPID": True, "restartPolicy": "Never",
                     "containers": [{"name": "stall", "image": image, "command": ["sh", "-c", script],
                                     "securityContext": {"privileged": True}}]}}


def inject(scenario, a, events, stop):
    def event(kind, **detail):
        events.append({"ms": int(time.time() * 1000), "event": kind, **detail})
        print(f"Fault: {kind} {detail}", flush=True)

    if stop.wait(a.fault_at):
        return
    if scenario == "ingestor-kill":
        run("kubectl", "-n", "adx-mon", "delete", "pod", a.fault_pod, "--wait=false")
        event("deleted", pod=a.fault_pod)
    elif scenario == "ingestor-rollout":
        run("kubectl", "-n", "adx-mon", "rollout", "restart", "statefulset/ingestor")
        event("rollout-started")
        run("kubectl", "-n", "adx-mon", "rollout", "status", "statefulset/ingestor", "--timeout=40m", check=False)
        event("rollout-finished")
    elif scenario == "ingestor-stall":
        node = json.loads(output("kubectl", "-n", "adx-mon", "get", "pod", a.fault_pod, "-o", "json"))["spec"]["nodeName"]
        kubectl_apply(stall_manifest(node, a.fault_duration, a.debug_image))
        event("stall-requested", pod=a.fault_pod, node=node, seconds=a.fault_duration)
        run("kubectl", "-n", "adx-mon", "wait", "pod/adx-mon-e2e-stall",
            "--for=jsonpath={.status.phase}=Succeeded", f"--timeout={a.fault_duration + 300}s", check=False)
        event("stall-finished", log=output("kubectl", "-n", "adx-mon", "logs", "adx-mon-e2e-stall"))
    elif scenario == "collector-restart":
        node = workload_node()
        end = time.monotonic() + a.fault_duration
        while time.monotonic() < end and not stop.is_set():
            for name, pod in pods("adxmon-e2e-role=node-collector").items():
                if pod["spec"]["nodeName"] == node:
                    run("kubectl", "-n", "adx-mon", "delete", "pod", name, "--wait=false")
                    event("deleted", pod=name)
            stop.wait(a.fault_interval)


@contextlib.contextmanager
def kusto_via_aks(state, tag, port=18443):
    """Opt-in: reach Kusto through a TCP relay pod in AKS, for clusters that
    only accept connections from Azure networks. curl --connect-to keeps the
    real host name, so TLS and certificate checks are unchanged."""
    host = urllib.parse.urlparse(state["endpoint"]).hostname
    kubectl_apply({"apiVersion": "v1", "kind": "Pod", "metadata": {
        "name": "adx-mon-e2e-kusto-relay", "namespace": "adx-mon"},
        "spec": {"containers": [{"name": "relay", "image": harness.image(state, "workload", tag),
                                 "env": [{"name": "FORWARD_TO", "value": f"{host}:443"},
                                         {"name": "HOST_IP", "value": "unused.invalid"}]}]}})
    run("kubectl", "-n", "adx-mon", "wait", "--for=condition=Ready", "pod/adx-mon-e2e-kusto-relay", "--timeout=5m")
    stop = threading.Event()

    def supervise():
        # Restart the port-forward whenever kubectl exits during long runs.
        while not stop.is_set():
            proc = subprocess.Popen(["kubectl", "-n", "adx-mon", "port-forward", "pod/adx-mon-e2e-kusto-relay",
                                     f"{port}:8443"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            while proc.poll() is None and not stop.wait(1):
                pass
            if proc.poll() is None:
                proc.terminate()
                proc.wait(timeout=10)
            stop.wait(1)

    thread = threading.Thread(target=supervise, daemon=True)
    thread.start()
    deadline = time.monotonic() + 30
    while True:
        try:
            socket.create_connection(("127.0.0.1", port), timeout=2).close()
            break
        except OSError:
            if time.monotonic() > deadline:
                stop.set()
                raise RuntimeError("Kusto relay port-forward did not start")
            time.sleep(.5)
    common.KUSTO_CONNECT_TO = f"127.0.0.1:{port}"
    print(f"Kusto traffic for {host} relayed through pod/adx-mon-e2e-kusto-relay", flush=True)
    try:
        yield
    finally:
        common.KUSTO_CONNECT_TO = ""
        stop.set()
        thread.join(timeout=15)
        run("kubectl", "-n", "adx-mon", "delete", "pod", "adx-mon-e2e-kusto-relay", "--ignore-not-found",
            "--wait=false", check=False)


def http_json(forward, path, **params):
    url = forward.url + path + ("?" + urllib.parse.urlencode(params) if params else "")
    with urllib.request.urlopen(url, timeout=15) as response:
        return json.load(response)


# Long soaks outlive brief local network or API server outages.
RETRIES, RETRY_DELAY = 60, 10


def stream_status():
    for attempt in range(RETRIES):
        try:
            with harness.Forward() as forward:
                return http_json(forward, "/status")
        except Exception as e:
            if attempt == RETRIES - 1:
                raise
            print(f"Workload status unavailable ({str(e).strip()[:200]}); retrying", flush=True)
            time.sleep(RETRY_DELAY)


def warm(state, since, deadline_seconds):
    """Stream briefly until every path's table has a row for this warm-up id."""
    warm_id = f"warm-{uuid.uuid4().hex[:10]}"
    with harness.Forward() as forward:
        for attempt in range(15):
            try:
                http_json(forward, "/stream", run=warm_id, rate=1, duration=30)
                break
            except Exception as e:
                if attempt == 14:
                    raise
                print(f"Waiting for workload stream ({e})", flush=True)
                time.sleep(4)
    pending, deadline = set(PATHS), time.monotonic() + deadline_seconds
    while pending and time.monotonic() < deadline:
        for path in sorted(pending):
            try:
                if soak_rows(state, path, warm_id, since):
                    pending.discard(path)
                    print(f"Warm-up ready: {path}", flush=True)
            except RuntimeError:
                pass  # the table may not exist yet
        if pending:
            time.sleep(10)
    if pending:
        raise RuntimeError(f"Warm-up timed out for {sorted(pending)}")
    while not stream_status().get("done"):
        time.sleep(5)


def query_rows(state, run_id, since, attempts=RETRIES):
    """soak_rows for every path, retrying transient Kusto or relay failures."""
    for attempt in range(attempts):
        try:
            return {path: soak_rows(state, path, run_id, since) for path in PATHS}
        except RuntimeError as e:
            if attempt == attempts - 1:
                raise
            print(f"Kusto query failed ({str(e)[:200]}); retrying", flush=True)
            time.sleep(RETRY_DELAY)


def collect(state, run_id, since, status, drain):
    """Query until every expected seq arrived or the drain deadline passed."""
    deadline = time.monotonic() + drain
    while True:
        rows = query_rows(state, run_id, since)
        missing = {p: len(expected_seqs(p, status) - set(rows[p])) for p in PATHS}
        if not any(missing.values()) or time.monotonic() >= deadline:
            return rows
        print(f"Draining; missing rows {missing}", flush=True)
        time.sleep(15)


def soak(state, a):
    tag, wl = a.tag, a.workload_tag or workload_tag()
    stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    directory = STATE_DIR / "results" / a.resource_group / "soak" / f"{a.label}-{a.scenario}-{stamp}"
    directory.mkdir(parents=True)
    batching = str(dt.timedelta(seconds=a.batching_seconds)).zfill(8)
    for db in ("Metrics", "Logs"):
        kusto(state["endpoint"], "", f'.alter database {db} policy ingestionbatching '
              f'```{{"MaximumBatchingTimeSpan":"{batching}","MaximumNumberOfItems":500,'
              '"MaximumRawDataSizeMB":1024}```', True)
    since = dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")
    run("kubectl", "-n", "adx-mon", "delete", "pod", "adx-mon-e2e-stall", "--ignore-not-found")
    toml = [f"max-segment-age-seconds = {a.collector_max_segment_age}", *a.collector_toml]
    harness.deploy(state, tag, False, ingestor_replicas=a.ingestor_replicas,
                   ingestor_args=a.ingestor_arg, keep_metrics=("^adxmon_",),
                   collector_prefix="".join(line + "\n" for line in toml))
    harness.workload(state, wl)
    deploy_load(state, wl, a)
    deploy_slow_targets(state, wl, a.slow_targets if a.scenario == "slow-target" else 0, a.slow_delay)
    try:
        warm(state, since, a.warmup_timeout)
    except Exception as e:
        harness.log_errors(since, directory)
        raise RuntimeError(f"{e}; component logs saved in {directory}") from e
    before = snapshot_counters()
    run_id = f"soak-{uuid.uuid4().hex[:12]}"
    with harness.Forward() as forward:
        started = http_json(forward, "/stream", run=run_id, rate=a.rate, duration=a.duration,
                            chunked=1 if a.scenario == "chunked-otlp" else 0)
    start_ms = started["started_ms"]
    print(f"Streaming {run_id} at {a.rate}/s for {a.duration}s", flush=True)
    stop, events, top = threading.Event(), [], []
    threads = [threading.Thread(target=sample_top, args=(top, stop), daemon=True),
               threading.Thread(target=inject, args=(a.scenario, a, events, stop), daemon=True)]
    for thread in threads:
        thread.start()
    try:
        time.sleep(a.duration)
        while not (status := stream_status()).get("done"):
            time.sleep(10)
        rows = collect(state, run_id, since, status, a.drain)
    finally:
        stop.set()
    threads[1].join(timeout=60)
    after = snapshot_counters()
    errors = harness.log_errors(since, directory)
    fault_ms = start_ms + a.fault_at * 1000
    windows = {"before_fault": (start_ms, fault_ms),
               "fault": (fault_ms, fault_ms + (a.fault_duration + 60) * 1000),
               "after_fault": (fault_ms + (a.fault_duration + 60) * 1000, start_ms + a.duration * 1000)}
    if a.scenario in ("steady", "slow-target", "chunked-otlp"):
        windows = {"all": (start_ms, start_ms + a.duration * 1000)}
    result = {
        "label": a.label, "scenario": a.scenario, "run_id": run_id, "since": since,
        "start_ms": start_ms, "tag": tag, "workload_tag": wl,
        "images": {c: harness.image(state, c, tag) for c in ("ingestor", "collector")},
        "params": {k: v for k, v in vars(a).items() if k not in ("func", "resource_group")},
        "stream": {k: v for k, v in status.items() if k != "failed"},
        "failed_sends": {p: len(v) for p, v in status.get("failed", {}).items()},
        "events": events,
        "stats": {p: path_stats(p, rows[p], status, windows) for p in PATHS},
        "per_minute": {p: per_minute(rows[p], start_ms) for p in PATHS},
        "counters": counter_delta(before, after),
        "resources": resource_summary(top) if top else {},
        "log_errors": len(errors), "error_classes": error_classes(errors),
        "ingestion_failures": [row for db in ("Metrics", "Logs") for row in management_rows(state, db, (
            f".show ingestion failures | where FailedOn > datetime({since}) "
            "| summarize Count=count() by Database, Table, FailureKind, ErrorCode"))],
        "create_merge_commands": management_rows(state, "Metrics", (
            f".show commands | where StartedOn > datetime({since}) "
            "| where Text startswith '.create-merge' | summarize Count=count() by Database, State")),
    }
    save(directory / "result.json", result)
    save(directory / "rows.json", {p: rows[p] for p in PATHS})
    (directory / "report.md").write_text(render([result]))
    print(render([result]))
    print(f"Results: {directory}")
    return result


def fmt(value):
    if value is None:
        return "—"
    if isinstance(value, float):
        return f"{value:,.1f}" if abs(value) < 100 else f"{value:,.0f}"
    return f"{value:,}"


def render(results):
    """Markdown for one or more soak results; columns are results in order."""
    names = [f"{r['label']} ({r['tag'][:8]})" for r in results]
    head = "| | " + " | ".join(names) + " |\n|---|" + "---:|" * len(results) + "\n"
    lines = [f"# Soak: {', '.join(sorted({r['scenario'] for r in results}))}", ""]
    lines += ["| Run | " + " | ".join(names) + " |", "|---|" + "---|" * len(results),
              "| Run id | " + " | ".join(r["run_id"] for r in results) + " |",
              "| Signals emitted | " + " | ".join(fmt(r["stream"]["emitted"]) for r in results) + " |",
              "| Rate (/s) | " + " | ".join(fmt(float(r["params"]["rate"])) for r in results) + " |",
              "| Ingestor args | " + " | ".join(" ".join(r["params"]["ingestor_arg"]) or "—" for r in results) + " |",
              "| Fault events | " + " | ".join(str(len(r["events"])) for r in results) + " |", ""]
    for path in PATHS:
        lines += [f"## {path} — end-to-end latency (ms)", "", head.rstrip("\n")]
        for key in ("samples", "p50", "p95", "p99", "p99.9", "max", "missing", "duplicates", "failed_sends"):
            lines.append(f"| {key} | " + " | ".join(fmt(r["stats"][path].get(key)) for r in results) + " |")
        windows = list(results[0]["stats"][path]["windows"])
        for window in windows:
            for key in ("p50", "p99", "max"):
                lines.append(f"| {window} {key} | " + " | ".join(
                    fmt(r["stats"][path]["windows"].get(window, {}).get(key)) for r in results) + " |")
        lines.append("")
    families = sorted({k for r in results for k in r["counters"]})
    if families:
        lines += ["## Error-like counter increases (all pods)", "", head.rstrip("\n")]
        lines += [f"| `{f}` | " + " | ".join(fmt(r["counters"].get(f)) for r in results) + " |" for f in families]
        lines.append("")
    classes = sorted({k for r in results for k in r["error_classes"]})
    lines += ["## Log errors by class", "", head.rstrip("\n"),
              "| total | " + " | ".join(fmt(r["log_errors"]) for r in results) + " |"]
    lines += [f"| {c.replace('|', '/')} | " + " | ".join(fmt(r["error_classes"].get(c)) for r in results) + " |"
              for c in classes[:40]]
    lines.append("")
    comps = sorted({c for r in results for c in r["resources"]})
    if comps:
        lines += ["## Resources (kubectl top, per pod)", "", head.rstrip("\n")]
        for c in comps:
            for key in ("cpu_m_avg", "cpu_m_max", "mem_mi_avg", "mem_mi_max"):
                lines.append(f"| {c} {key} | " + " | ".join(fmt(r["resources"].get(c, {}).get(key)) for r in results) + " |")
        lines.append("")
    lines += ["## Kusto", "", head.rstrip("\n"),
              "| ingestion failures | " + " | ".join(fmt(sum(int(x.get("Count", 0)) for x in r["ingestion_failures"])) for r in results) + " |",
              "| `.create-merge` commands | " + " | ".join(fmt(sum(int(x.get("Count", 0)) for x in r["create_merge_commands"])) for r in results) + " |", ""]
    return "\n".join(lines)


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="command", required=True)
    b = sub.add_parser("build")
    b.add_argument("--resource-group", required=True)
    b.add_argument("--ref", action="append", required=True, help="Git ref to build; repeatable")
    b.add_argument("--build-mode", choices=("auto", "docker", "acr"), default="auto")
    s = sub.add_parser("soak")
    s.add_argument("--resource-group", required=True)
    s.add_argument("--tag", required=True, help="Ingestor/collector image tag from build")
    s.add_argument("--workload-tag", help="Defaults to the hash of this checkout's workload")
    s.add_argument("--label", required=True, help="Name for this side of the comparison, e.g. main")
    s.add_argument("--scenario", choices=SCENARIOS, default="steady")
    s.add_argument("--duration", type=int, default=1200, help="Seconds of measured streaming")
    s.add_argument("--rate", type=float, default=2, help="Signals per second on every path")
    s.add_argument("--fault-at", type=int, default=300, help="Seconds into the stream")
    s.add_argument("--fault-duration", type=int, default=90)
    s.add_argument("--fault-interval", type=int, default=120, help="collector-restart period")
    s.add_argument("--fault-pod", default="ingestor-1")
    s.add_argument("--debug-image", default="mcr.microsoft.com/cbl-mariner/busybox:2.0")
    s.add_argument("--ingestor-replicas", type=int, default=3)
    s.add_argument("--ingestor-arg", action="append", default=[])
    s.add_argument("--collector-toml", action="append", default=[], help="Top-level daemonset TOML line")
    s.add_argument("--collector-max-segment-age", type=int, default=5,
                   help="Collector WAL segment age in seconds; the built-in default is 30")
    s.add_argument("--load-replicas", type=int, default=3)
    s.add_argument("--load-tables", type=int, default=200)
    s.add_argument("--load-series", type=int, default=20)
    s.add_argument("--load-log-tables", type=int, default=5)
    s.add_argument("--load-logs", type=int, default=50)
    s.add_argument("--slow-targets", type=int, default=2)
    s.add_argument("--slow-delay", default="8s")
    s.add_argument("--batching-seconds", type=int, default=10, help="ADX queued batching time")
    s.add_argument("--warmup-timeout", type=int, default=900)
    s.add_argument("--drain", type=int, default=600, help="Seconds to wait for stragglers")
    s.add_argument("--kusto-via-aks", action="store_true",
                   help="Relay Kusto calls through a pod in AKS (for ADX clusters unreachable from this machine)")
    c = sub.add_parser("compare")
    c.add_argument("results", nargs="+", type=Path, help="Soak result directories, A first")
    c.add_argument("--out", type=Path)
    a = p.parse_args()
    try:
        if a.command == "compare":
            text = render([json.loads((d / "result.json").read_text()) for d in a.results])
            if a.out:
                a.out.write_text(text)
            print(text)
            return
        state = load(a.resource_group)
        harness.kube(state)
        if a.command == "build":
            for ref in a.ref:
                tag, wl = build_ref(state, ref, a.build_mode)
                print(f"{ref}: --tag {tag} --workload-tag {wl}")
        else:
            if a.scenario.startswith("ingestor-") and a.ingestor_replicas < 2:
                p.error(f"{a.scenario} needs --ingestor-replicas >= 2")
            relay = (kusto_via_aks(state, a.workload_tag or workload_tag()) if a.kusto_via_aks
                     else contextlib.nullcontext())
            with relay:
                soak(state, a)
    except (RuntimeError, FileNotFoundError) as e:
        sys.exit(str(e))


if __name__ == "__main__":
    main()
