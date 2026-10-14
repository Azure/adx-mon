"""Offline rendering and reporting tests: python3 -m unittest discover -s tools/dev/azure-test-env -p 'test_harness.py'."""
import json
import contextlib
import io
import subprocess
import tempfile
import time
import unittest
from argparse import Namespace
from pathlib import Path
from unittest.mock import patch

import test as harness
import cluster
import common


STATE = {"endpoint": "https://example.eastus.kusto.windows.net", "kubelet_client_id": "client",
         "region": "eastus", "resource_group": "rg", "subscription": "sub",
         "names": {"aks": "aks", "acr": "testacr"}}


class HarnessTest(unittest.TestCase):
    def test_poll_records_visibility_window_per_path(self):
        attempts = {path: 0 for path in harness.TABLES}
        records = {path: {} for path in harness.TABLES}
        def rows(_state, path, _run):
            attempts[path] += 1
            return {} if attempts[path] == 1 else {1: 1}
        with patch.object(harness, "rows_for", side_effect=rows), \
             patch.object(harness.time, "sleep"):
            missing = harness.poll(STATE, "run", [{"seq": 1}], time.monotonic() + 5, records)
        self.assertFalse(missing)
        for path in harness.TABLES:
            window = records[path][1]
            self.assertIsNotNone(window["last_negative_ms"])
            self.assertLessEqual(window["last_negative_ms"], window["first_visible_ms"])

    def test_log_audit_reads_full_pod_logs_with_selector(self):
        commands = []
        def fake_run(*args, **kwargs):
            commands.append(args)
            return subprocess.CompletedProcess(args, 0, "", "")
        with tempfile.TemporaryDirectory() as tmp, \
             patch.object(harness, "run", side_effect=fake_run), \
             patch.object(harness, "output", return_value='{"items":[]}'):
            self.assertEqual(harness.log_errors("2026-10-10T00:00:00Z", Path(tmp)), [])
        log_commands = [args for args in commands if "logs" in args]
        self.assertEqual(len(log_commands), 2)
        self.assertTrue(all("--tail=-1" in args for args in log_commands))

    def test_workload_routes_existing_image_to_collector_service(self):
        commands = []
        with patch.object(harness, "run", side_effect=lambda *a, **kw: commands.append((a, kw))):
            harness.workload(STATE, "old-tag")
        pod = json.loads(next(kw["input"] for a, kw in commands if a[-2:] == ("-f", "-")))
        container = pod["spec"]["containers"][0]
        self.assertEqual(container["image"], "testacr.azurecr.io/adx-mon/workload:old-tag")
        self.assertEqual(container["env"], [{"name": "HOST_IP",
            "value": "adx-mon-e2e-collector.adx-mon.svc.cluster.local"}])

    def test_reuse_tag_requires_all_three_images(self):
        with patch.object(harness, "az_json", side_effect=[["tag"], ["tag"], ["tag"]]) as lookup:
            harness.verify_images(STATE, "tag")
        self.assertEqual(lookup.call_count, 3)
        with patch.object(harness, "az_json", side_effect=[["tag"], []]):
            with self.assertRaisesRegex(RuntimeError, "Missing"):
                harness.verify_images(STATE, "tag")

    def test_kusto_request_uses_verified_curl_without_logging_token(self):
        frame = {"TableKind": "PrimaryResult", "Columns": [{"ColumnName": "Answer"}],
                 "Rows": [[1]]}
        with patch.object(common, "_token", "secret-token"), \
             patch.object(common, "_token_deadline", float("inf")), \
             patch.object(common.subprocess, "run", return_value=subprocess.CompletedProcess(
                 [], 0, json.dumps([frame]), "")) as request:
            result = common.kusto("https://example.kusto.windows.net", "Metrics", "print Answer=1")
        self.assertEqual(result, [{"Answer": 1}])
        args = request.call_args.args[0]
        self.assertIn("--fail-with-body", args)
        self.assertNotIn("--insecure", args)
        self.assertIn("--http1.1", args)
        self.assertEqual(args[args.index("--tls-max") + 1], "1.2")
        self.assertEqual(args[-1], "https://example.kusto.windows.net/v2/rest/query")

    def test_kusto_retries_only_tls_handshake_failures(self):
        ok = subprocess.CompletedProcess([], 0, json.dumps([{
            "TableKind": "PrimaryResult", "Columns": [], "Rows": []}]), "")
        failed = subprocess.CompletedProcess([], 35, "", "TLS handshake failed")
        with patch.object(common, "_token", "secret-token"), \
             patch.object(common, "_token_deadline", float("inf")), \
             patch.object(common.time, "sleep"), \
             patch.object(common.subprocess, "run", side_effect=[failed, ok]) as request:
            self.assertEqual(common.kusto("https://example", "", ".show version", True), [])
        self.assertEqual(request.call_count, 2)

    def test_kusto_parses_management_table_zero(self):
        frame = {"TableName": "Table_0", "Columns": [{"ColumnName": "TableName"}],
                 "Rows": [["E2ePodLogs"]]}
        with patch.object(common, "_token", "secret-token"), \
             patch.object(common, "_token_deadline", float("inf")), \
             patch.object(common.subprocess, "run", return_value=subprocess.CompletedProcess(
                 [], 0, json.dumps({"Tables": [frame]}), "")):
            self.assertEqual(common.kusto("https://example", "Logs", ".show tables", True),
                             [{"TableName": "E2ePodLogs"}])

    def test_warmup_waits_for_all_test_tables_without_spamming_errors(self):
        attempts = {path: 0 for path in harness.TABLES}

        def rows(_state, path, _run):
            attempts[path] += 1
            if attempts[path] == 1:
                raise RuntimeError("Table not found")
            return {1: 1}

        with patch.object(harness, "emit"), patch.object(harness, "rows_for", side_effect=rows), \
             patch.object(harness.time, "sleep"), contextlib.redirect_stdout(io.StringIO()):
            self.assertTrue(harness.warm_tables(STATE, object()).startswith("warm-"))
        self.assertEqual(set(attempts), set(harness.TABLES))
        self.assertTrue(all(count == 2 for count in attempts.values()))

    def test_queries_match_real_otlp_row_shapes(self):
        queries = {}
        def fake_kusto(_endpoint, _db, csl):
            queries[csl.split(" |")[0]] = csl
            return [{"Seq": 1, "Rows": 1}]
        with patch.object(harness, "kusto", side_effect=fake_kusto):
            for path in harness.TABLES:
                self.assertEqual(harness.rows_for(STATE, path, "warm-test"), {1: 1})
        self.assertIn("Attributes.run_id", queries["E2eOtlpLogs"])
        self.assertIn("Attributes.run_id", queries["E2eQueuedControl"])
        self.assertIn("extract('[0-9]+'", queries["E2eOtlpSample"])
        self.assertIn("Body", queries["E2ePodLogs"])

    def test_acr_build_uses_local_checkout_without_docker_login(self):
        commands = []
        with patch.object(harness, "output", return_value="commit"), \
             patch.object(harness, "run") as local_run, \
             patch.object(harness.subprocess, "run", side_effect=lambda a, **kw: commands.append(a)), \
             contextlib.redirect_stdout(io.StringIO()):
            harness.build(STATE, "tag", "acr")
        local_run.assert_not_called()
        self.assertEqual(len(commands), 3)
        for component, command in zip(("ingestor", "collector", "workload"), commands):
            self.assertEqual(command[:3], ["az", "acr", "build"])
            self.assertEqual(command[command.index("--image") + 1], f"adx-mon/{component}:tag")
            self.assertEqual(command[-1], str(harness.ROOT))

    def test_auto_build_falls_back_when_daemon_is_unavailable(self):
        with patch.object(harness, "docker_available", return_value=False), \
             patch.object(harness, "output", return_value="commit"), \
             patch.object(harness, "run") as local_run, \
             patch.object(harness.subprocess, "run") as remote_run, \
             contextlib.redirect_stdout(io.StringIO()):
            harness.build(STATE, "tag")
        self.assertEqual(remote_run.call_count, 3)
        local_run.assert_not_called()

    def test_aks_sku_falls_back_from_restricted_dsv3_to_quota_available_family(self):
        def sku(name, family, restrictions=None):
            return {"name": name, "resourceType": "virtualMachines", "family": family,
                    "restrictions": restrictions or [],
                    "capabilities": [{"name": "vCPUs", "value": "4"},
                                     {"name": "MemoryGB", "value": "16"}]}
        catalog = [sku("Standard_D4s_v3", "standardDSv3Family", [{"reasonCode": "NotAvailableForSubscription"}]),
                   sku("Standard_D4s_v5", "standardDSv5Family"),
                   sku("Standard_D4ads_v6", "standardDadv6Family")]
        quotas = [{"name": {"value": name}, "currentValue": current, "limit": 100}
                  for name, current in (("standardDSv3Family", 0), ("standardDSv5Family", 100),
                                        ("standardDadv6Family", 10), ("cores", 50))]
        with patch.object(cluster, "az_json", side_effect=[quotas, catalog]):
            self.assertEqual(cluster.aks_sku("canadacentral", "sub"), "Standard_D4ads_v6")
        with patch.object(cluster, "az_json", side_effect=[quotas, catalog]):
            self.assertEqual(cluster.aks_sku("canadacentral", "sub", "standard_d4ads_v6"),
                             "Standard_D4ads_v6")
        with patch.object(cluster, "az_json", side_effect=[quotas, catalog]):
            with self.assertRaisesRegex(RuntimeError, "restricted"):
                cluster.aks_sku("canadacentral", "sub", "Standard_D4s_v3")

    def test_resume_reuses_existing_aks_and_acr_and_requests_two_kusto_nodes(self):
        state = {"subscription": "sub", "tenant": "tenant", "region": "canadacentral",
                 "resource_group": "rg", "names": cluster.names("rg", "sub"),
                 "node_sku": "Standard_D4ads_v6", "kubelet_client_id": "client", "owned": True}
        identity = {"clientId": "client", "objectId": "object"}
        resources = iter([{"name": "rg"}, {"name": "acr"},
                          {"agentPoolProfiles": [{"vmSize": "Standard_D4ads_v6"}],
                           "identityProfile": {"kubeletidentity": identity}},
                          None, None, None, None])
        calls = []

        def fake_run(*args, **kwargs):
            calls.append(args)
            return subprocess.CompletedProcess(args, 0, "", "")

        def fake_az_json(*args):
            if args[0] == "account":
                return {"id": "sub", "tenantId": "tenant"}
            if args[1] == "cluster":
                return {"uri": "https://cluster", "sku": {"name": "Standard_L8as_v3"}}
            return {"role": "AllDatabasesAdmin", "principalId": "object"}

        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "state.json"
            path.write_text(json.dumps(state))
            with patch.object(cluster, "state_path", return_value=path), \
                 patch.object(cluster, "load", return_value=state), \
                 patch.object(cluster, "save"), \
                 patch.object(cluster, "existing", side_effect=lambda *a: next(resources)), \
                 patch.object(cluster, "output", return_value=""), \
                 patch.object(cluster, "run", side_effect=fake_run), \
                 patch.object(cluster, "az_json", side_effect=fake_az_json), \
                 patch.object(cluster, "kusto_skus", return_value=["Standard_L8as_v3"]), \
                 contextlib.redirect_stdout(io.StringIO()):
                cluster.create(Namespace(resource_group="rg", subscription="sub",
                                         region="canadacentral", aks_sku=None, kusto_sku=None))
        self.assertFalse(any(a[:3] in (("az", "aks", "create"), ("az", "acr", "create"),
                                       ("az", "group", "create")) for a in calls))
        kusto_create = next(a for a in calls if a[:4] == ("az", "kusto", "cluster", "create"))
        self.assertIn("capacity=2", kusto_create)
        database_creates = [a for a in calls if a[:4] == ("az", "kusto", "database", "create")]
        self.assertEqual(len(database_creates), 2)
        for command in database_creates:
            self.assertNotIn("--location", command)
            self.assertIn("location=canadacentral", command)
            self.assertNotIn("kind=ReadWrite", command)

    def rendered(self, realtime, wait_ms=100):
        commands = []
        with patch.object(harness, "run", side_effect=lambda *a, **kw: commands.append((a, kw))), \
             patch.object(harness, "output", return_value=json.dumps({"subsets": [{"addresses": [{"ip": "10.0.0.1"}]}]})):
            harness.deploy(STATE, "sha-run", realtime, wait_ms)
        return [kw["input"] for a, kw in commands if a[-2:] == ("-f", "-")]

    def test_queued_has_four_receivers_but_no_realtime_flags(self):
        ingestor, collector, service = self.rendered(False)
        self.assertIn("testacr.azurecr.io/adx-mon/ingestor:sha-run", ingestor)
        self.assertIn("testacr.azurecr.io/adx-mon/collector:sha-run", collector)
        self.assertIn("path = '/v1/otlpmetrics'", collector)
        self.assertIn("scrape-interval = 1", collector)
        self.assertIn("default-drop-metrics = true", collector)
        self.assertIn("keep-metrics = ['^e2e_scraped_sample$']", collector)
        self.assertNotIn("--realtime-table", ingestor)
        self.assertNotIn("[[realtime.tables]]", collector)
        self.assertIn("adxmon-e2e-role: node-collector", collector)
        self.assertIn("/mnt/collector-singleton", collector)
        self.assertEqual(json.loads(service)["spec"]["selector"],
                         {"adxmon-e2e-role": "node-collector"})

    def test_realtime_excludes_queued_control(self):
        ingestor, collector, _ = self.rendered(True)
        self.assertIn("log-batch-wait-ms = 100", collector)
        for path in harness.REALTIME_PATHS:
            db, table = harness.TABLES[path]
            self.assertIn(f"--realtime-table={db}.{table}", ingestor)
            self.assertIn(f"table = '{table}'", collector)
        self.assertNotIn("--realtime-table=Logs.E2eQueuedControl", ingestor)
        self.assertNotIn("table = 'E2eQueuedControl'", collector)

    def test_realtime_tail_batch_wait_can_be_ab_tested(self):
        _, collector, _ = self.rendered(True, 1000)
        self.assertIn("log-batch-wait-ms = 1000", collector)

    def test_report_fails_missing_and_over_two_seconds(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "result.json"
            path.write_text(json.dumps({"mode": "realtime", "run_id": "test", "git_commit": "sha",
                "signals": [{"seq": 1, "emitted_ms": 1000000000000}],
                "observed": {key: {"1": {"rows": 1, "first_visible_ms": 1000000003000}}
                             for key in harness.TABLES}, "log_errors": []}))
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertFalse(harness.report(path))
                self.assertFalse(harness.report(Path(tmp)))


if __name__ == "__main__":
    unittest.main()
