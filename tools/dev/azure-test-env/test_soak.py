"""Offline soak and cluster tests: python3 -m unittest discover -s tools/dev/azure-test-env -p 'test_*.py'."""
import contextlib
import io
import json
import subprocess
import tempfile
import unittest
from argparse import Namespace
from pathlib import Path
from unittest.mock import patch

import cluster
import common
import soak
import test as harness
from test_harness import STATE


def result(label, latencies, **extra):
    rows = {seq: [1000 * seq, 1000 * seq + ms, 1] for seq, ms in enumerate(latencies, 1)}
    status = {"emitted": len(latencies), "failed": {}}
    windows = {"all": (0, 10 ** 9)}
    stats = {p: soak.path_stats(p, rows, status, windows) for p in soak.PATHS}
    base = {"label": label, "tag": label * 4, "scenario": "steady", "run_id": "r-" + label,
            "stream": {"emitted": len(latencies)}, "params": {"rate": 2, "ingestor_arg": []},
            "events": [], "stats": stats, "counters": {}, "error_classes": {}, "log_errors": 0,
            "resources": {}, "ingestion_failures": [], "create_merge_commands": []}
    base.update(extra)
    return base


class SoakTest(unittest.TestCase):
    def test_percentile_is_nearest_rank(self):
        ordered = list(range(1, 1001))
        self.assertEqual(soak.percentile(ordered, 50), 500)
        self.assertEqual(soak.percentile(ordered, 99), 990)
        self.assertEqual(soak.percentile(ordered, 99.9), 999)
        self.assertEqual(soak.percentile([7], 99.9), 7)
        self.assertIsNone(soak.percentile([], 50))

    def test_missing_excludes_failed_sends_and_counts_duplicates(self):
        status = {"emitted": 5, "failed": {"otlp_logs": [2]}}
        rows = {1: [0, 100, 1], 3: [0, 300, 2], 5: [0, None, 1]}
        stats = soak.path_stats("otlp_logs", rows, status, {})
        self.assertEqual(stats["missing"], 1)  # seq 4; seq 2 was rejected at send
        self.assertEqual(stats["failed_sends"], 1)
        self.assertEqual(stats["duplicates"], 1)
        self.assertEqual(stats["no_ingestion_time"], 1)
        self.assertEqual(stats["samples"], 2)
        self.assertEqual(stats["max"], 300)

    def test_scraped_path_is_sampled_not_lost(self):
        stats = soak.path_stats("scraped_metrics", {3: [0, 900, 2]}, {"emitted": 10, "failed": {}}, {})
        self.assertEqual((stats["missing"], stats["duplicates"]), (0, 0))

    def test_pod_logs_cannot_fail_at_send(self):
        status = {"emitted": 3, "failed": {"pod_logs": [1, 2, 3]}}
        self.assertEqual(soak.expected_seqs("pod_logs", status), {1, 2, 3})

    def test_windows_split_latency_by_emit_time(self):
        rows = {1: [100, 200, 1], 2: [500, 5500, 1], 3: [900, 1000, 1]}
        stats = soak.path_stats("remote_write", rows, {"emitted": 3, "failed": {}},
                                {"before": (0, 400), "fault": (400, 800), "after": (800, 2000)})
        self.assertEqual(stats["windows"]["fault"]["max"], 5000)
        self.assertEqual(stats["windows"]["before"]["p99"], 100)
        self.assertEqual(stats["windows"]["after"]["samples"], 1)

    def test_queries_extract_seq_and_emitted_from_each_row_shape(self):
        for path, field in (("pod_logs", "parse_json(tostring(Body))"), ("otlp_logs", "Attributes"),
                            ("queued_control", "Attributes"), ("otlp_metrics", "Labels"),
                            ("scraped_metrics", "Labels"), ("remote_write", "Labels")):
            db, table = harness.SOAK_TABLES[path]
            csl = soak.soak_query(table, path, "soak-abc", "2026-11-04T00:00:00Z")
            self.assertTrue(csl.startswith(table + " | where ingestion_time() > datetime(2026-11-04T00:00:00Z)"))
            self.assertIn(f"extend R = {field} ", csl)
            self.assertIn("contains 'soak-abc'", csl)
            self.assertIn("tostring(R.emitted_ms)", csl)

    def test_rows_merge_split_groups_and_keep_earliest_ingestion(self):
        rows = [{"Seq": 1, "Emitted": 10, "Rows": 1, "IngestedMs": 50},
                {"Seq": 1, "Emitted": 10, "Rows": 1, "IngestedMs": 30},
                {"Seq": None, "Emitted": 1, "Rows": 1, "IngestedMs": 1}]
        with patch.object(soak, "kusto", return_value=rows):
            self.assertEqual(soak.soak_rows(STATE, "remote_write", "r", "t"), {1: [10, 30, 2]})

    def test_counters_sum_error_families_and_survive_restarts(self):
        text = ("# HELP x\nadxmon_ingestor_failed_uploads_total{db=\"a\"} 2\n"
                "adxmon_ingestor_failed_uploads_total{db=\"b\"} 3\n"
                "adxmon_ingestor_samples_stored_total 99\n"
                "adxmon_transfer_retries_bucket{le=\"1\"} 4\n")
        self.assertEqual(soak.parse_counters(text), {"adxmon_ingestor_failed_uploads_total": 5.0})
        before = {"ingestor-0": {"f_errors_total": 10.0}, "ingestor-1": {"f_errors_total": 7.0}}
        after = {"ingestor-0": {"f_errors_total": 12.0}, "ingestor-1": {"f_errors_total": 1.0},
                 "ingestor-2": {"f_errors_total": 4.0}}
        self.assertEqual(soak.counter_delta(before, after), {"f_errors_total": 7.0})

    def test_error_classes_mask_numbers_by_component(self):
        lines = ['[pod/ingestor-0/ingestor] {"lvl":"ERR","msg":"upload 123 failed after 5s"}',
                 '[pod/ingestor-2/ingestor] {"lvl":"ERR","msg":"upload 9 failed after 61s"}',
                 '[pod/collector-abcde/collector] ERROR plain text 42']
        self.assertEqual(soak.error_classes(lines),
                         {"ingestor: upload # failed after #": 2, "collector: ERROR plain text #": 1})
        imds = ['[pod/ingestor-%d/ingestor] {"msg":"HTTP status=400 method=GET '
                'url=http://169.254.169.254/metadata/identity/oauth2/token rid=%s"}' % (i, rid)
                for i, rid in enumerate(("0a1b-2c3d-4e5f", "99ff-aa11-b2c3"))]
        self.assertEqual(soak.error_classes(imds), {"ingestor: HTTP # method=GET # #": 2})

    def test_stall_stops_the_ingestor_from_the_host_pid_namespace(self):
        pod = soak.stall_manifest("node-1", 90, "busybox")
        spec = pod["spec"]
        self.assertEqual((spec["nodeName"], spec["hostPID"]), ("node-1", True))
        self.assertTrue(spec["containers"][0]["securityContext"]["privileged"])
        script = spec["containers"][0]["command"][2]
        self.assertIn("pgrep -x ingestor", script)
        self.assertLess(script.index("kill -STOP"), script.index("sleep 90"))
        self.assertLess(script.index("sleep 90"), script.index("kill -CONT"))

    def test_resource_summary_groups_pods_by_component(self):
        samples = [{"pod": "ingestor-0", "cpu_m": 100, "mem_mi": 200},
                   {"pod": "ingestor-1", "cpu_m": 300, "mem_mi": 400},
                   {"pod": "collector-singleton-x", "cpu_m": 5, "mem_mi": 6}]
        summary = soak.resource_summary(samples)
        self.assertEqual(summary["ingestor"], {"cpu_m_avg": 200, "cpu_m_max": 300,
                                               "mem_mi_avg": 300, "mem_mi_max": 400})
        self.assertIn("collector-singleton", summary)
        self.assertEqual(soak.parse_quantity("1536Mi"), 1536)
        self.assertEqual(soak.parse_quantity("2Gi"), 2048)
        self.assertEqual(soak.parse_quantity("250m"), 250)

    def test_compare_renders_one_column_per_result(self):
        text = soak.render([result("a", [100, 200, 300], counters={"x_errors_total": 3}),
                            result("b", [10, 20, 30], error_classes={"ingestor: boom": 2})])
        self.assertIn("| | a (aaaa) | b (bbbb) |", text)
        self.assertIn("| p99 | 300 | 30 |", text)
        self.assertIn("| `x_errors_total` | 3 | — |", text)
        self.assertIn("| ingestor: boom | — | 2 |", text)

    def test_workload_tag_tracks_workload_sources(self):
        self.assertRegex(soak.workload_tag(), r"^wl-[0-9a-f]{12}$")
        self.assertEqual(soak.workload_tag(), soak.workload_tag())

    def test_soak_deploy_scales_ingestors_and_passes_flags(self):
        commands = []
        with patch.object(harness, "run", side_effect=lambda *a, **kw: commands.append((a, kw))), \
             patch.object(harness, "output", return_value=json.dumps({"subsets": [{"addresses": [{"ip": "1"}]}]})):
            harness.deploy(STATE, "t", False, ingestor_replicas=3, ingestor_args=["--forward-transfers"],
                           keep_metrics=("^adxmon_",), collector_prefix="scrape-concurrency = 4\n")
        ingestor, collector, _ = [kw["input"] for a, kw in commands if a[-2:] == ("-f", "-")]
        self.assertIn("  replicas: 3\n", ingestor)
        self.assertIn('            - "--forward-transfers"\n', ingestor)
        self.assertIn("keep-metrics = ['^e2e_scraped_sample$', '^adxmon_']", collector)
        daemonset, singleton = collector.split("name: collector-singleton-config", 1)
        self.assertIn("  config.toml: |\n    scrape-concurrency = 4\n", daemonset)
        self.assertNotIn("scrape-concurrency", singleton)

    def test_build_ref_builds_components_from_the_ref_worktree(self):
        builds = []
        with tempfile.TemporaryDirectory() as tmp, \
             patch.object(soak, "STATE_DIR", Path(tmp)), \
             patch.object(soak, "output", return_value="0123456789abcdef"), \
             patch.object(soak, "acr_tags", return_value=set()), \
             patch.object(soak, "run") as run, \
             patch.object(harness, "build", side_effect=lambda *a, **kw: builds.append((a, kw))):
            tag, wl = soak.build_ref(STATE, "origin/main", "docker")
            tree = Path(tmp) / "worktrees" / "0123456789ab"
            run.assert_called_once_with("git", "-C", soak.ROOT, "worktree", "add", "--detach", tree,
                                        "0123456789abcdef")
        self.assertEqual(tag, "0123456789ab")
        self.assertEqual(builds[0][1], {"root": tree, "components": ("ingestor", "collector")})
        self.assertEqual(builds[1][0][1], wl)
        self.assertEqual(builds[1][1], {"components": ("workload",)})

    def test_kusto_tunnel_keeps_the_real_host_for_tls(self):
        commands = []
        def fake(command, **kwargs):
            commands.append(command)
            return subprocess.CompletedProcess(command, 0, '{"Tables": []}', "")
        with patch.object(common, "KUSTO_CONNECT_TO", "127.0.0.1:18443"), \
             patch.object(common, "_token", "t"), patch.object(common, "_token_deadline", float("inf")), \
             patch.object(common.subprocess, "run", side_effect=fake):
            common.kusto("https://c.region.kusto.windows.net", "db", "print 1")
        command = commands[0]
        self.assertEqual(command[command.index("--connect-to") + 1], "c.region.kusto.windows.net:443:127.0.0.1:18443")
        self.assertEqual(command[-1], "https://c.region.kusto.windows.net/v2/rest/query")
        self.assertNotIn("--insecure", command)
        self.assertNotIn("-k", command)

    def test_kusto_is_direct_by_default(self):
        commands = []
        with patch.object(common, "_token", "t"), patch.object(common, "_token_deadline", float("inf")), \
             patch.object(common.subprocess, "run", side_effect=lambda c, **k: commands.append(c) or
                          subprocess.CompletedProcess(c, 0, '{"Tables": []}', "")):
            common.kusto("https://c", "db", "print 1")
        self.assertNotIn("--connect-to", commands[0])

    def test_row_queries_retry_transient_failures(self):
        calls = []
        def flaky(state, path, run_id, since):
            calls.append(path)
            if len(calls) == 1:
                raise RuntimeError("curl: (28) Connection timed out")
            return {1: [0, 1, 1]}
        with patch.object(soak, "soak_rows", side_effect=flaky), patch.object(soak.time, "sleep"), \
             contextlib.redirect_stdout(io.StringIO()):
            rows = soak.query_rows(STATE, "r", "t")
        self.assertEqual(set(rows), set(soak.PATHS))
        with patch.object(soak, "soak_rows", side_effect=RuntimeError("down")), \
             patch.object(soak.time, "sleep"), contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaises(RuntimeError):
                soak.query_rows(STATE, "r", "t", attempts=2)


class AdoptTest(unittest.TestCase):
    def adopt(self, resources):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "rg.json"
            saved = {}
            with patch.object(cluster, "state_path", return_value=path), \
                 patch.object(cluster, "az_json", return_value={"id": "sub", "tenantId": "tenant"}), \
                 patch.object(cluster, "existing", side_effect=lambda *a: next(resources)), \
                 patch.object(cluster, "save", side_effect=lambda p, d: saved.update(d)), \
                 contextlib.redirect_stdout(io.StringIO()):
                cluster.adopt(Namespace(resource_group="rg", subscription="sub"))
        return saved

    def test_adopt_records_existing_resources_as_not_owned(self):
        aks = {"agentPoolProfiles": [{"vmSize": "Standard_D4ads_v6"}],
               "identityProfile": {"kubeletidentity": {"clientId": "client"}}}
        state = self.adopt(iter([{"location": "canadacentral"}, aks, {"name": "acr"},
                                 {"uri": "https://adx", "sku": {"name": "Standard_L8s_v3"}}]))
        self.assertFalse(state["owned"])
        self.assertTrue(state["ready"])
        self.assertEqual((state["region"], state["endpoint"], state["kubelet_client_id"]),
                         ("canadacentral", "https://adx", "client"))

    def test_adopt_refuses_missing_resources(self):
        with self.assertRaisesRegex(RuntimeError, "No harness-named kusto"):
            self.adopt(iter([{"location": "x"}, {"aks": 1}, {"acr": 1}, None]))

    def test_scale_resizes_the_first_pool(self):
        state = {"resource_group": "rg", "subscription": "sub", "names": {"aks": "aks"}}
        aks = {"agentPoolProfiles": [{"name": "nodepool1", "count": 1, "vmSize": "D4"}]}
        with patch.object(cluster, "load", return_value=state), \
             patch.object(cluster, "az_json", return_value=aks), \
             patch.object(cluster, "run") as run, contextlib.redirect_stdout(io.StringIO()):
            cluster.scale(Namespace(resource_group="rg", node_count=3))
        run.assert_called_once_with("az", "aks", "nodepool", "scale", "-g", "rg", "--cluster-name", "aks",
                                    "-n", "nodepool1", "--node-count", 3, "--subscription", "sub")


if __name__ == "__main__":
    unittest.main()
