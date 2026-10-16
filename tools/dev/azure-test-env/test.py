#!/usr/bin/env python3
"""Repeatable branch-image, four-path queued/realtime E2E tests against cluster.sh resources."""
import argparse
import concurrent.futures
import datetime as dt
import json
import os
import re
import statistics
import subprocess
import sys
import time
import urllib.parse
import urllib.error
import urllib.request
import uuid
from pathlib import Path

from common import ROOT, STATE_DIR, az_json, kusto, load, output, run, save

HERE = Path(__file__).resolve().parent
TABLES = {"pod_logs": ("Logs", "E2ePodLogs"),
          "otlp_logs": ("Logs", "E2eOtlpLogs"),
          "otlp_metrics": ("Metrics", "E2eOtlpSample"),
          "scraped_metrics": ("Metrics", "E2eScrapedSample"),
          "queued_control": ("Logs", "E2eQueuedControl")}
REALTIME_PATHS = tuple(path for path in TABLES if path != "queued_control")
# The isolation phase deliberately sheds large logs at the collector while
# healthy records in the same request and other paths continue to Kusto.
ISOLATION_NOISY = "otlp_logs"
ISOLATION_HEALTHY = tuple(path for path in TABLES if path != ISOLATION_NOISY)
ISOLATION_TABLE_BYTES = 512
LOG_ERROR = re.compile(r'"(?:lvl|level)"\s*:\s*"(?:ERR|ERROR|FATAL)"|\b(?:ERROR|FATAL)\b', re.I)
IMDS_STARTUP_ERROR = re.compile(r'"msg":"HTTP status=400 method=GET url=http://169\.254\.169\.254/metadata/identity/oauth2/token')


def image(state, component, tag):
    return f"{state['names']['acr']}.azurecr.io/adx-mon/{component}:{tag}"


def kube(state):
    run("az", "aks", "get-credentials", "-g", state["resource_group"], "-n",
        state["names"]["aks"], "--subscription", state["subscription"], "--overwrite-existing")
    context = output("kubectl", "config", "current-context")
    cluster = az_json("aks", "show", "-g", state["resource_group"], "-n",
                      state["names"]["aks"], "--subscription", state["subscription"])
    if cluster["identityProfile"]["kubeletidentity"]["clientId"] != state["kubelet_client_id"]:
        raise RuntimeError("AKS kubelet identity changed; refusing to deploy")
    return context


def docker_available():
    try:
        return subprocess.run(["docker", "info"], stdout=subprocess.DEVNULL,
                              stderr=subprocess.DEVNULL, timeout=10).returncode == 0
    except (FileNotFoundError, subprocess.TimeoutExpired):
        return False


def build(state, tag, mode="auto"):
    if mode == "auto":
        mode = "docker" if docker_available() else "acr"
    if mode == "docker" and not docker_available():
        raise RuntimeError("Docker daemon is unavailable; start it or use --build-mode acr")
    print(f"Building current checkout using {'local Docker' if mode == 'docker' else 'remote ACR Tasks'}", flush=True)
    if mode == "docker":
        run("az", "acr", "login", "-n", state["names"]["acr"],
            "--subscription", state["subscription"])
    commit = output("git", "-C", ROOT, "rev-parse", "HEAD")
    for component in ("ingestor", "collector", "workload"):
        dockerfile = (HERE / "Dockerfile.workload" if component == "workload" else
                      ROOT / "build" / "images" / f"Dockerfile.{component}")
        ref = image(state, component, tag)
        build_args = []
        if component != "workload":
            build_args = ["--build-arg", f"VERSION=e2e-{tag}", "--build-arg",
                          f"GIT_COMMIT={commit}"]
        if mode == "docker":
            run("docker", "build", "--platform", "linux/amd64", "-f", dockerfile,
                "-t", ref, *build_args, ROOT)
            run("docker", "push", ref)
        else:
            # ACR builds the uploaded *local checkout* and pushes the tag. No
            # Docker daemon or registry login is needed on the caller's Mac.
            relative_file = dockerfile.relative_to(ROOT).as_posix()
            command = ["az", "acr", "build", "--registry", state["names"]["acr"],
                       "--resource-group", state["resource_group"],
                       "--subscription", state["subscription"], "--platform", "linux/amd64",
                       "--file", relative_file, "--image", f"adx-mon/{component}:{tag}",
                       *build_args, str(ROOT)]
            print("+", " ".join(command), flush=True)
            # Stream remote-build progress and errors; this operation can take minutes.
            subprocess.run(command, check=True)


def verify_images(state, tag):
    for component in ("ingestor", "collector", "workload"):
        tags = az_json("acr", "repository", "show-tags", "--name", state["names"]["acr"],
                       "--repository", f"adx-mon/{component}",
                       "--subscription", state["subscription"])
        if tag not in tags:
            raise RuntimeError(f"Missing {image(state, component, tag)}; omit --reuse-tag to build")


def collector_config(text, realtime, daemonset, realtime_log_batch_wait_ms=100, table_limits=()):
    text = text.replace("scrape-interval = 30", "scrape-interval = 1")
    text = text.replace("scrape-timeout = 25", "scrape-timeout = 1")
    # Keep-metrics only takes effect when default-drop-metrics is true.  Without
    # both settings the one-second scraper forwards thousands of unrelated
    # series per cycle and dominates the latency experiment.
    text = text.replace("default-drop-metrics = false", "default-drop-metrics = true", 1)
    text = text.replace("keep-metrics = []", "keep-metrics = ['^e2e_scraped_sample$']", 1)
    if daemonset and table_limits:
        settings = "enforce-table-limits = true\n"
        for db, table, max_bytes in table_limits:
            settings += (f"\n[[table-limits]]\ndatabase = '{db}'\ntable = '{table}'\n"
                         f"max-bytes = {max_bytes}\n")
        marker = "[add-labels]"
        assert marker in text
        text = text.replace(marker, settings + "\n" + marker, 1)
    # Explicit OTLP metric receiver; the checked-in quick-start only defines OTLP logs.
    if daemonset:
        text += "\n[[otel-metric]]\ndatabase = 'Metrics'\npath = '/v1/otlpmetrics'\n"
    if realtime:
        text += ("\n[realtime]\nmax-segment-age-ms = 100\nmax-batch-latency-ms = 100\n"
                 f"log-batch-wait-ms = {realtime_log_batch_wait_ms}\n")
        for path in REALTIME_PATHS:
            db, table = TABLES[path]
            text += f"\n[[realtime.tables]]\ndatabase = '{db}'\ntable = '{table}'\n"
    return text


def deploy(state, tag, realtime, realtime_log_batch_wait_ms=100, table_limits=(), collector_limits=()):
    vars = {"ADX_URL": state["endpoint"], "CLIENT_ID": state["kubelet_client_id"],
            "CLUSTER": state["names"]["aks"], "REGION": state["region"]}
    def render(file):
        text = (ROOT / "build" / "k8s" / file).read_text()
        for key, value in vars.items():
            text = text.replace("$" + key, value)
        return text
    ingestor = render("ingestor.yaml").replace(
        "ghcr.io/azure/adx-mon/ingestor:latest", image(state, "ingestor", tag))
    if realtime:
        flags = "".join(f'            - "--realtime-table={db}.{table}"\n'
                        for path in REALTIME_PATHS for db, table in (TABLES[path],))
        flags += f'            - "--realtime-streaming-budget={state["endpoint"]}=3"\n'
        flags += '            - "--realtime-max-segment-age=100ms"\n'
        flags += '            - "--realtime-max-batch-latency=100ms"\n'
        marker = f'            - "--logs-kusto-endpoints=Logs={state["endpoint"]}"\n'
        assert marker in ingestor
        ingestor = ingestor.replace(marker, marker + flags)
    if table_limits:
        flags = '            - "--enforce-table-limits"\n'
        for db, table, max_bytes in table_limits:
            limit = json.dumps({"database": db, "table": table, "max-bytes": max_bytes})
            flags += f"            - '--table-limit={limit}'\n"
        marker = f'            - "--logs-kusto-endpoints=Logs={state["endpoint"]}"\n'
        assert marker in ingestor
        ingestor = ingestor.replace(marker, marker + flags)
    for name in ("summaryrules", "managementcommands"):
        run("kubectl", "apply", "-f", ROOT / "kustomize" / "bases" / f"{name}_crd.yaml")
    run("kubectl", "apply", "-f", "-", input=ingestor)
    collector = render("collector.yaml").replace(
        "ghcr.io/azure/adx-mon/collector:latest", image(state, "collector", tag))
    # The checked-in daemonset and singleton both mount /mnt/collector on the
    # same node in a one-node AKS test. Give the singleton its own WAL to avoid
    # concurrent access, recovery storms and OOMs during repeat runs.
    deployment_start = collector.index("kind: Deployment")
    daemonset, deployment = collector[:deployment_start], collector[deployment_start:]
    old_volume = "        - name: storage\n          hostPath:\n            path: /mnt/collector\n"
    assert old_volume in deployment
    deployment = deployment.replace(old_volume, "        - name: storage\n          hostPath:\n"
                                    "            path: /mnt/collector-singleton\n"
                                    "            type: DirectoryOrCreate\n", 1)
    collector = daemonset + deployment
    # A dedicated service must select only the daemonset. The singleton shares
    # its adxmon=collector label but has no OTLP-metric receiver.
    template_labels = "      labels:\n        adxmon: collector\n"
    assert template_labels in daemonset
    collector = collector.replace(template_labels,
                                  "      labels:\n        adxmon: collector\n"
                                  "        adxmon-e2e-role: node-collector\n", 1)
    # Modify only the two ConfigMap bodies; keep all other checked-in resources intact.
    for marker, daemonset in (("---\napiVersion: apps/v1\nkind: DaemonSet", True),
                              ("---\napiVersion: apps/v1\nkind: Deployment", False)):
        start = collector.index("  config.toml: |", collector.index("name: collector-config" if daemonset else "name: collector-singleton-config"))
        end = collector.index(marker, start)
        raw = collector[start + len("  config.toml: |\n"):end]
        # Strip the YAML block indent before appending TOML; re-indent afterwards.
        config = "\n".join(line[4:] if line.startswith("    ") else line for line in raw.rstrip("\n").splitlines()) + "\n"
        replacement = "  config.toml: |\n" + "".join(
            "    " + line + "\n" for line in collector_config(
                config, realtime, daemonset, realtime_log_batch_wait_ms, collector_limits).splitlines())
        collector = collector[:start] + replacement + collector[end:]
    run("kubectl", "apply", "-f", "-", input=collector)
    service = {"apiVersion": "v1", "kind": "Service", "metadata": {
        "name": "adx-mon-e2e-collector", "namespace": "adx-mon"},
        "spec": {"selector": {"adxmon-e2e-role": "node-collector"},
                 "ports": [{"port": 3100, "targetPort": 8080}]}}
    run("kubectl", "apply", "-f", "-", input=json.dumps(service))
    run("kubectl", "apply", "-f", ROOT / "build/k8s/ksm.yaml")
    for kind in ("statefulset/ingestor", "daemonset/collector", "deployment/collector-singleton"):
        # ConfigMap changes require a rollout even when the image tag is unchanged.
        run("kubectl", "-n", "adx-mon", "rollout", "restart", kind)
        run("kubectl", "-n", "adx-mon", "rollout", "status", kind, "--timeout=8m")
    endpoints = json.loads(output("kubectl", "-n", "adx-mon", "get", "endpoints",
                                  "adx-mon-e2e-collector", "-o", "json"))
    if not any(subset.get("addresses") for subset in endpoints.get("subsets", [])):
        raise RuntimeError("Collector E2E Service has no ready daemonset endpoint")


def workload(state, tag):
    ref = image(state, "workload", tag)
    manifest = {"apiVersion": "v1", "kind": "Pod", "metadata": {
        "name": "adx-mon-e2e-workload", "namespace": "adx-mon",
        "annotations": {"adx-mon/scrape": "true", "adx-mon/port": "8088",
                        "adx-mon/path": "/metrics", "adx-mon/log-destination": "Logs:E2ePodLogs",
                        "adx-mon/log-parsers": "json"}, "labels": {"app": "adx-mon-e2e-workload"}},
        "spec": {"containers": [{"name": "workload", "image": ref, "imagePullPolicy": "Always",
                                "ports": [{"containerPort": 8088}], "env": [{"name": "HOST_IP",
                                "value": "adx-mon-e2e-collector.adx-mon.svc.cluster.local"}]}]}}
    # The existing workload image constructs http://$HOST_IP:3100. Point it
    # at the service's port 3100 to avoid rebuilding just for hostPort routing.
    # Recreate the test pod to guarantee the intended image, log cursor and discovery.
    run("kubectl", "-n", "adx-mon", "delete", "pod", "adx-mon-e2e-workload", "--ignore-not-found", "--wait=true")
    run("kubectl", "apply", "-f", "-", input=json.dumps(manifest))
    run("kubectl", "-n", "adx-mon", "wait", "--for=condition=Ready", "pod/adx-mon-e2e-workload", "--timeout=5m")


class Forward:
    def __init__(self, resource="pod/adx-mon-e2e-workload", port=8088):
        self.resource, self.port = resource, port

    def __enter__(self):
        self.proc = subprocess.Popen(["kubectl", "-n", "adx-mon", "port-forward",
                                      self.resource, ":" + str(self.port)],
                                     stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        line = self.proc.stdout.readline()
        match = re.search(r"127\.0\.0\.1:(\d+)", line)
        if not match:
            self.proc.terminate()
            raise RuntimeError(f"port forward failed: {line}")
        self.url = f"http://127.0.0.1:{match.group(1)}"
        return self

    def __exit__(self, *_):
        self.proc.terminate()
        self.proc.wait(timeout=10)
        self.proc.stdout.close()


def emit(forward, run_id, seq):
    url = f"{forward.url}/emit?" + urllib.parse.urlencode({"run": run_id, "seq": seq})
    try:
        with urllib.request.urlopen(url, timeout=12) as response:
            return json.load(response)
    except urllib.error.HTTPError as e:
        raise RuntimeError(f"Test workload rejected signal {run_id}/{seq} (HTTP {e.code}): "
                           f"{e.read(2048).decode(errors='replace')}") from e


def streaming_counts():
    with Forward("pod/ingestor-0", 9091) as forward:
        with urllib.request.urlopen(forward.url + "/metrics", timeout=10) as response:
            text = response.read().decode()
    counts = {}
    for line in text.splitlines():
        if not line.startswith("adxmon_ingestor_realtime_batches_total{"):
            continue
        labels = dict(re.findall(r'(\w+)="([^"]*)"', line.split("}")[0]))
        key = (labels.get("database"), labels.get("table"), labels.get("outcome"))
        counts[key] = float(line.rsplit(" ", 1)[-1])
    return counts


def collector_metrics():
    with Forward("service/adx-mon-e2e-collector", 3100) as forward:
        with urllib.request.urlopen(forward.url + "/metrics", timeout=10) as response:
            return response.read().decode()


def mixed_emit(forward, run_id, seq):
    url = f"{forward.url}/mixed?" + urllib.parse.urlencode({"run": run_id, "seq": seq})
    with urllib.request.urlopen(url, timeout=12) as response:
        return json.load(response)


def samples(text, name):
    """Parse one Prometheus metric family into {label tuple: value}."""
    found = {}
    for line in text.splitlines():
        if not line.startswith(name + "{"):
            continue
        labels = dict(re.findall(r'(\w+)="([^"]*)"', line.split("}")[0]))
        found[(labels.get("database"), labels.get("table"))] = float(line.rsplit(" ", 1)[-1])
    return found


def table_budgets(text):
    """Per-destination budget state as {(db, table): {throttled, rejected}}."""
    throttled = samples(text, "adxmon_table_backlog_throttled")
    # The quota counter is aggregated by component/kind/reason, not table.
    # The backlog gauge carries the destination labels needed here.
    rejected = samples(text, "adxmon_table_backlog_rejected_records")
    state = {}
    for key in set(throttled) | set(rejected):
        state[key] = {"throttled": throttled.get(key, 0.0), "rejected": rejected.get(key, 0.0)}
    return state


def pod_ready(name):
    pod = json.loads(output("kubectl", "-n", "adx-mon", "get", "pod", name, "-o", "json"))
    return any(c["type"] == "Ready" and c["status"] == "True"
               for c in pod.get("status", {}).get("conditions", []))


def validate_isolation(records, pulses, mixed_records, noisy_rows, ready_checks, healthy):
    """Require selective shedding, accurate counts, and healthy delivery."""
    errors = []
    if not pulses:
        errors.append("No mixed requests were sent")
    if not ready_checks or not all(ready_checks):
        errors.append("Ingestor became unready during table shedding")
    for pulse in pulses:
        total, rejected = pulse.get("noisy_records", 0), pulse.get("rejected_records", 0)
        if not 0 < rejected <= total:
            errors.append(f"Mixed request {pulse.get('seq')} did not selectively reject noisy records: {pulse}")
        accepted = total - rejected
        actual = noisy_rows.get(pulse.get("seq"), noisy_rows.get(str(pulse.get("seq")), 0))
        if actual != accepted:
            errors.append(f"Mixed request {pulse.get('seq')} reported {rejected}/{total} rejected, "
                          f"but {actual} noisy rows reached Kusto (expected {accepted})")
        rows = mixed_records.get("queued_control", {})
        row = rows.get(pulse.get("seq"), rows.get(str(pulse.get("seq")), {}))
        if not row.get("first_visible_ms"):
            errors.append(f"Healthy control in mixed request {pulse.get('seq')} never reached Kusto")
    for path in healthy:
        visible = [seq for seq, row in records.get(path, {}).items() if row.get("first_visible_ms")]
        if not visible:
            errors.append(f"Healthy path {path} delivered no rows during table shedding")
    return errors


def wait_noisy_rows(state, run_id, pulses, deadline):
    """Wait for acknowledged noisy records, not intentionally rejected ones."""
    expected = {p["seq"]: p["noisy_records"] - p["rejected_records"] for p in pulses}
    if not any(expected.values()):
        # A fresh cluster need not even have a table when every row is shed.
        return {}
    found = {}
    while time.monotonic() < deadline:
        try:
            found = rows_for(state, ISOLATION_NOISY, run_id)
        except Exception:
            time.sleep(5)  # Schema creation may still be in progress.
            continue
        if all(found.get(seq, 0) >= count for seq, count in expected.items()):
            break
        time.sleep(5)
    return found


def rows_for(state, path, run_id):
    db, table = TABLES[path]
    # The OTLP writer currently renders AnyValue labels as protobuf text
    # (string_value:"..."); scraped metric labels are plain strings.
    if path.endswith("metrics"):
        csl = (f"{table} | where tostring(Labels.run_id) contains '{run_id}' "
               "| summarize Rows=count() by Seq=toint(extract('[0-9]+', 0, tostring(Labels.seq)))")
    elif path in ("otlp_logs", "queued_control"):
        csl = (f"{table} | where tostring(Attributes.run_id) == '{run_id}' "
               "| summarize Rows=count() by Seq=toint(Attributes.seq)")
    else:
        csl = (f"{table} | extend E2e=parse_json(tostring(Body)) "
               f"| where tostring(E2e.run_id) == '{run_id}' "
               "| summarize Rows=count() by Seq=toint(E2e.seq)")
    return {int(row["Seq"]): int(row["Rows"]) for row in kusto(state["endpoint"], db, csl)
            if row.get("Seq") is not None}


def warm_tables(state, forward, deadline_seconds=600, pending=None):
    warm_id = f"warm-{uuid.uuid4().hex[:10]}"
    for attempt in range(15):
        try:
            emit(forward, warm_id, 1)
            break
        except RuntimeError as e:
            if "connection refused" not in str(e).lower() or attempt == 14:
                raise
            print("Waiting for collector OTLP listener to accept connections", flush=True)
            time.sleep(2)
    pending = set(TABLES) if pending is None else set(pending)
    deadline = time.monotonic() + deadline_seconds
    last_error = {}
    last_notice = 0
    while pending and time.monotonic() < deadline:
        for path in list(pending):
            try:
                if rows_for(state, path, warm_id).get(1, 0):
                    pending.remove(path)
                    print(f"Warm-up ready: {path}", flush=True)
            except Exception as e:
                last_error[path] = str(e)
        now = time.monotonic()
        if pending and now - last_notice >= 30:
            print("Waiting for Kusto tables and queued ingestion: " + ", ".join(sorted(pending)), flush=True)
            last_notice = now
        if pending:
            time.sleep(5)
    if pending:
        detail = "\n".join(f"  {path}: {last_error.get(path, 'no matching row yet')}" for path in sorted(pending))
        raise RuntimeError(f"Warm-up timed out; test tables have not ingested the probe rows:\n{detail}\n"
                           "Check ingestor/collector logs and WAL backlog before measuring latency.")
    return warm_id


def poll(state, phase, signals, deadline, records, paths=None):
    # Each table polls independently.  A slow query on the queued control must
    # not delay the first-visibility timestamp of a realtime table.
    def poll_path(path):
        remaining = {s["seq"] for s in signals}
        last_missing = {}
        failures = 0
        while remaining and time.monotonic() < deadline:
            query_started = int(time.time() * 1000)
            try:
                found = rows_for(state, path, phase)
            except Exception as e:
                failures += 1
                if failures >= 3:
                    raise RuntimeError(f"Kusto query for {path} failed after warm-up: {e}") from e
                time.sleep(.25)
                continue
            failures = 0
            observed = int(time.time() * 1000)
            for seq, count in found.items():
                if seq in remaining:
                    records[path][seq] = {"rows": count, "first_visible_ms": observed,
                                          "first_positive_query_started_ms": query_started,
                                          "last_negative_ms": last_missing.get(seq)}
                    remaining.remove(seq)
                elif seq in records[path]:
                    records[path][seq]["rows"] = count
            for seq in remaining:
                last_missing[seq] = observed
            if remaining:
                time.sleep(.25)
        return {(path, seq) for seq in remaining}

    selected = tuple(paths or TABLES)
    with concurrent.futures.ThreadPoolExecutor(max_workers=len(selected)) as pool:
        futures = [pool.submit(poll_path, path) for path in selected]
        return set().union(*(future.result() for future in futures))


def log_errors(since, report_dir):
    errors = []
    for label in ("app=ingestor", "adxmon=collector"):
        log_file = report_dir / ("ingestor.log" if label.startswith("app") else "collector.log")
        result = run("kubectl", "-n", "adx-mon", "logs", "-l", label,
                     "--all-containers=true", "--prefix=true", "--since-time=" + since,
                     "--max-log-requests=20", "--tail=-1", check=False)
        log_file.write_text(result.stdout + result.stderr)
        if result.returncode:
            errors.append(f"Unable to read {label} logs: {result.stderr}")
        errors += [line for line in result.stdout.splitlines() if LOG_ERROR.search(line)]
        pods = json.loads(output("kubectl", "-n", "adx-mon", "get", "pods", "-l", label, "-o", "json"))
        for pod in pods["items"]:
            for container in pod.get("status", {}).get("containerStatuses", []):
                if not container.get("restartCount"):
                    continue
                errors.append(f"Container {pod['metadata']['name']}/{container['name']} "
                              f"restarted {container['restartCount']} time(s) "
                              f"({container.get('lastState', {})})")
                previous = run("kubectl", "-n", "adx-mon", "logs", pod["metadata"]["name"],
                               "-c", container["name"], "--previous", "--since-time=" + since,
                               check=False)
                with log_file.open("a") as log:
                    log.write("\n# previous container " + pod["metadata"]["name"] + "\n" + previous.stdout + previous.stderr)
                if previous.returncode:
                    errors.append(f"Cannot read restarted {pod['metadata']['name']}/{container['name']}: {previous.stderr}")
                errors += [line for line in previous.stdout.splitlines() if LOG_ERROR.search(line)]
    return errors


def phase_run(state, mode, tag, report_dir, count, timeout, warmup_timeout=600,
              realtime_log_batch_wait_ms=100):
    realtime = mode == "realtime"
    if realtime:
        run("az", "kusto", "cluster", "update", "-g", state["resource_group"],
            "-n", state["names"]["kusto"], "--enable-streaming-ingest", "true",
            "--subscription", state["subscription"])
    else:
        for db in ("Metrics", "Logs"):
            kusto(state["endpoint"], "", f'.alter database {db} policy ingestionbatching '
                  '```{"MaximumBatchingTimeSpan":"00:01:00","MaximumNumberOfItems":500,'
                  '"MaximumRawDataSizeMB":1024}```', True)
    since = dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")
    deploy(state, tag, realtime, realtime_log_batch_wait_ms)
    workload(state, tag)
    try:
        with Forward() as forward:
            warm_id = warm_tables(state, forward, warmup_timeout)
    except Exception as e:
        errors = log_errors(since, report_dir)
        raise RuntimeError(f"{e}\nComponent logs saved in {report_dir}. "
                           f"Unexpected log errors: {errors[:5]}") from e
    if realtime:
        # A queryable table alone is not evidence of streaming; verify each
        # selected table has streamed before taking the measured counter delta.
        warm_deadline = time.monotonic() + 420
        while time.monotonic() < warm_deadline:
            counters = streaming_counts()
            if all(counters.get((*TABLES[path], "streamed"), 0) > 0 for path in REALTIME_PATHS):
                break
            with Forward() as forward:
                emit(forward, warm_id, 2)
            time.sleep(8)
        else:
            raise RuntimeError("Streaming never succeeded on all four tables; inspect ingestor metrics and logs")
        before = streaming_counts()
    else:
        before = {}
    run_id = f"e2e-{uuid.uuid4().hex[:12]}"
    signals = []
    records = {path: {} for path in TABLES}
    with Forward() as forward:
        # Poll concurrently with emission. Waiting until the last sample would
        # make the first-visibility timestamp meaningless for earlier samples.
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as executor:
            signals.append(emit(forward, run_id, 1))
            expected = [{"seq": seq} for seq in range(1, count + 1)]
            future = executor.submit(poll, state, run_id, expected,
                                     time.monotonic() + timeout, records)
            for seq in range(2, count + 1):
                time.sleep(3)  # multiple opportunities for the one-second scrape
                signals.append(emit(forward, run_id, seq))
            missing = future.result()
    # The last scraped sample can be collected again after first visibility.
    # Capture final row counts after the queued control has drained without
    # moving any path's original first-visibility timestamp.
    for path in TABLES:
        for seq, count in rows_for(state, path, run_id).items():
            if seq in records[path]:
                records[path][seq]["rows"] = count
    errors = log_errors(since, report_dir)
    after = streaming_counts() if realtime else {}
    counter_delta = {f"{db}.{table}.{outcome}": value - before.get((db, table, outcome), 0)
                     for (db, table, outcome), value in after.items()}
    if realtime:
        for path in REALTIME_PATHS:
            db, table = TABLES[path]
            if counter_delta.get(f"{db}.{table}.streamed", 0) < 1:
                errors.append(f"No streamed batches for {db}.{table} during measured run")
        errors += [f"Unexpected realtime fallback: {key} +{value}" for key, value in counter_delta.items()
                   if ".fallback_" in key and value > 0]
    result = {"mode": mode, "run_id": run_id, "git_commit": output("git", "rev-parse", "HEAD"),
              "realtime_log_batch_wait_ms": realtime_log_batch_wait_ms if realtime else None,
              "images": {c: image(state, c, tag) for c in ("collector", "ingestor", "workload")},
              "signals": signals, "observed": records, "missing": list(map(list, sorted(missing))),
              "log_errors": errors, "streaming_counter_delta": counter_delta}
    save(report_dir / "result.json", result)
    return result


def phase_isolation(state, tag, report_dir, count, timeout, warmup_timeout=600):
    """Shed noisy logs in mixed requests while every healthy path keeps flowing."""
    for db in ("Metrics", "Logs"):
        kusto(state["endpoint"], "", f'.alter database {db} policy ingestionbatching '
              '```{"MaximumBatchingTimeSpan":"00:01:00","MaximumNumberOfItems":500,'
              '"MaximumRawDataSizeMB":1024}```', True)
    noisy_db, noisy_table = TABLES[ISOLATION_NOISY]
    since = dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")
    deploy(state, tag, False, collector_limits=((noisy_db, noisy_table, ISOLATION_TABLE_BYTES),))
    workload(state, tag)

    # Warm-up uses the same starved budget, so the noisy table may never appear.
    try:
        with Forward() as forward:
            warm_tables(state, forward, warmup_timeout, pending=set(ISOLATION_HEALTHY))
    except Exception as e:
        errors = log_errors(since, report_dir)
        raise RuntimeError(f"{e}\nComponent logs saved in {report_dir}. "
                           f"Unexpected log errors: {errors[:5]}") from e

    run_id = f"e2e-{uuid.uuid4().hex[:12]}"
    mixed_id = f"mixed-{uuid.uuid4().hex[:12]}"
    signals, records = [], {path: {} for path in TABLES}
    pulses, ready_checks = [], []
    mixed_records = {"queued_control": {}}
    with Forward() as forward:
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
            pulses.append(mixed_emit(forward, mixed_id, 1))
            ready_checks.append(pod_ready("ingestor-0"))
            signals.append(emit(forward, run_id, 1))
            expected = [{"seq": seq} for seq in range(1, count + 1)]
            deadline = time.monotonic() + timeout
            future = executor.submit(poll, state, run_id, expected, deadline, records, paths=ISOLATION_HEALTHY)
            mixed_future = executor.submit(poll, state, mixed_id, expected, deadline, mixed_records,
                                           paths=("queued_control",))
            for seq in range(2, count + 1):
                time.sleep(3)
                pulses.append(mixed_emit(forward, mixed_id, seq))
                ready_checks.append(pod_ready("ingestor-0"))
                signals.append(emit(forward, run_id, seq))
            missing = future.result()
            mixed_missing = mixed_future.result()
            noisy_rows = wait_noisy_rows(state, mixed_id, pulses, deadline)

    budgets = table_budgets(collector_metrics())
    ready_checks.append(pod_ready("ingestor-0"))
    errors = log_errors(since, report_dir)
    # AKS IMDS can transiently return 400 during pod startup. Do not hide a
    # lasting auth failure: readiness and Kusto delivery are checked below.
    startup_errors = [line for line in errors if IMDS_STARTUP_ERROR.search(line)]
    errors = [line for line in errors if not IMDS_STARTUP_ERROR.search(line)]
    errors += validate_isolation(records, pulses, mixed_records, noisy_rows, ready_checks, ISOLATION_HEALTHY)
    result = {"mode": "isolation", "run_id": run_id, "git_commit": output("git", "rev-parse", "HEAD"),
              "images": {c: image(state, c, tag) for c in ("collector", "ingestor", "workload")},
              "signals": signals, "observed": records, "missing": list(map(list, sorted(missing))),
              "log_errors": errors, "streaming_counter_delta": {},
              "unmeasured_paths": [ISOLATION_NOISY], "mixed_run_id": mixed_id,
              "mixed_pulses": pulses, "mixed_control": mixed_records["queued_control"],
              "mixed_noisy_rows": noisy_rows,
              "mixed_missing": list(map(list, sorted(mixed_missing))),
              "ignored_startup_errors": startup_errors,
              "table_budgets": {f"{db}.{table}": v for (db, table), v in budgets.items()},
              "ingestor_ready": all(ready_checks)}
    save(report_dir / "result.json", result)
    return result


def report(directory):
    if directory.is_dir():
        direct = directory / "result.json"
        results = [direct] if direct.exists() else sorted(directory.glob("*/result.json"))
    else:
        results = [directory]
    if not results:
        raise RuntimeError(f"No completed E2E result found in {directory}")
    lines = ["# ADX-Mon Azure E2E results", ""]
    passed = True
    for file in results:
        data = json.loads(file.read_text())
        lines += [f"## {data['mode']} — {data['run_id']}", "", f"Commit: `{data['git_commit']}`", ""]
        lines += ["", "| Path | Expected signals | Observed signals | Kusto rows | Median / p95 / max (ms) |",
                  "|---|---:|---:|---:|---|"]
        for path in TABLES:
            observed = data["observed"][path]
            if path in data.get("unmeasured_paths", []):
                lines.append(f"| {path} | — | not polled | — | — |")
                continue
            latencies = [v["first_visible_ms"] - next(s["emitted_ms"] for s in data["signals"] if s["seq"] == int(seq))
                         for seq, v in observed.items() if v.get("first_visible_ms")]
            ordered = sorted(latencies)
            p95 = ordered[max(0, (len(ordered) * 95 + 99) // 100 - 1)] if ordered else None
            summary = (f"{statistics.median(ordered):.0f} / {p95} / {max(ordered)}" if ordered else "—")
            lines.append(f"| {path} | {len(data['signals'])} | {len(ordered)} | "
                         f"{sum(v.get('rows', 0) for v in observed.values())} | {summary} |")
        lines += ["", "| Path | Sequence | Emitted (UTC) | Rows | First visible (UTC) | Latency (ms) |",
                  "|---|---:|---|---:|---|---:|"]
        for path in TABLES:
            if path in data.get("unmeasured_paths", []):
                continue
            for signal in data["signals"]:
                seq = signal["seq"]
                row = data["observed"][path].get(str(seq), {})
                emitted = signal["emitted_ms"]
                visible = row.get("first_visible_ms")
                latency = visible - emitted if visible else None
                fmt = lambda ms: dt.datetime.fromtimestamp(ms / 1000, dt.timezone.utc).isoformat() if ms else "—"
                lines.append(f"| {path} | {seq} | {fmt(emitted)} | {row.get('rows', 0)} | {fmt(visible)} | {latency if latency is not None else '—'} |")
                expected_missing = data.get("expected_missing", [])
                if path in expected_missing:
                    continue
                if not visible or (data["mode"] == "realtime" and path in REALTIME_PATHS and latency > 2000):
                    passed = False
        if data.get("mixed_pulses"):
            lines += ["", "| Mixed request | Noisy records | OTLP rejected | Noisy rows in Kusto | Healthy control in Kusto |",
                      "|---:|---:|---:|---:|---:|"]
            for pulse in data["mixed_pulses"]:
                seq = pulse["seq"]
                control = data.get("mixed_control", {}).get(str(seq), {})
                noisy = data.get("mixed_noisy_rows", {}).get(str(seq), 0)
                lines.append(f"| {seq} | {pulse['noisy_records']} | {pulse['rejected_records']} | {noisy} | "
                             f"{control.get('rows', 0)} |")
            lines += ["", f"Ingestor Ready during shedding: {data.get('ingestor_ready')}",
                      f"Ignored transient IMDS startup errors: {len(data.get('ignored_startup_errors', []))}", ""]
        if data["mode"] == "isolation":
            validation = validate_isolation(data["observed"], data.get("mixed_pulses", []),
                                            {"queued_control": data.get("mixed_control", {})},
                                            data.get("mixed_noisy_rows", {}),
                                            [data.get("ingestor_ready", False)], ISOLATION_HEALTHY)
            if len(data.get("mixed_pulses", [])) != len(data["signals"]):
                validation.append("Mixed request count did not match measured signal count")
            if validation:
                passed = False
                lines += ["", "Isolation validation: " + "; ".join(validation), ""]
        if data.get("table_budgets"):
            lines += ["", "| Table | Throttled | Rejected records |", "|---|---:|---:|"]
            for name, budget in sorted(data["table_budgets"].items()):
                lines.append(f"| {name} | {budget['throttled']:.0f} | {budget['rejected']:.0f} |")
            lines += ["", f"Ingestor ready: {data.get('ingestor_ready')}", ""]
        lines += ["", "Streaming outcomes: `" + json.dumps(data.get("streaming_counter_delta", {})) + "`", "",
                  f"Unexpected component log errors: {len(data['log_errors'])}", ""]
        if data["log_errors"]:
            passed = False
            lines += ["```", *data["log_errors"], "```", ""]
    lines += [f"**Result: {'PASS' if passed else 'FAIL'}**", ""]
    summary = "\n".join(lines)
    out = directory / "report.md" if directory.is_dir() else directory.with_suffix(".md")
    out.write_text(summary)
    print(summary)
    return passed


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("command", choices=("run", "baseline", "realtime", "isolation", "report"))
    p.add_argument("--resource-group")
    p.add_argument("--results", type=Path, help="Existing results directory for report")
    p.add_argument("--count", type=int, default=5)
    p.add_argument("--timeout", type=int, default=240, help="Query deadline per phase (seconds)")
    p.add_argument("--warmup-timeout", type=int, default=600,
                   help="Maximum seconds to wait for all test tables before measuring")
    p.add_argument("--realtime-log-batch-wait-ms", type=int, default=100,
                   help="Pod-tail batch wait for realtime tables; use 1000 for a 1-second A/B control")
    p.add_argument("--build-mode", choices=("auto", "docker", "acr"), default="auto",
                   help="auto uses local Docker if running, otherwise remote ACR Tasks")
    p.add_argument("--reuse-tag", help="Use an existing tag for all three ACR images, without rebuilding")
    a = p.parse_args()
    if a.command == "report":
        if not a.results: p.error("report requires --results")
        sys.exit(0 if report(a.results) else 1)
    if not a.resource_group: p.error("--resource-group is required")
    if a.count < 1: p.error("--count must be positive")
    if a.realtime_log_batch_wait_ms < 1: p.error("--realtime-log-batch-wait-ms must be positive")
    state = load(a.resource_group)
    if not state.get("ready"): p.error("cluster.sh create has not finished")
    kube(state)
    stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    tag = a.reuse_tag or output("git", "rev-parse", "--short", "HEAD") + "-" + stamp.lower()
    directory = STATE_DIR / "results" / a.resource_group / tag
    if directory.exists():
        directory = directory.with_name(tag + "-rerun-" + uuid.uuid4().hex[:8])
    directory.mkdir(parents=True)
    save(directory / "environment.json", state)
    try:
        if a.reuse_tag:
            verify_images(state, tag)
        else:
            build(state, tag, a.build_mode)
        for mode in (("baseline", "realtime") if a.command == "run" else (a.command,)):
            destination = directory / mode
            destination.mkdir()
            if mode == "isolation":
                phase_isolation(state, tag, destination, a.count, a.timeout, a.warmup_timeout)
            else:
                phase_run(state, mode, tag, destination, a.count, a.timeout,
                          a.warmup_timeout, a.realtime_log_batch_wait_ms)
            if not report(destination):
                raise RuntimeError(f"{mode} validation failed; see {destination / 'report.md'}")
    finally:
        if list(directory.glob("*/result.json")):
            report(directory)
        print(f"Results: {directory}")


if __name__ == "__main__":
    main()
