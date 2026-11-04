"""Shared, dependency-free Azure/Kusto helpers for the disposable E2E environment."""
import json
import subprocess
import urllib.parse
import threading
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
STATE_DIR = ROOT / ".adx-mon-test-env"
KUSTO_CONNECT_TO = ""  # set by soak.py --kusto-via-aks
KUSTO_TENANT = ""  # set by load() so tokens match the environment's tenant
_token = None
_token_deadline = 0
_token_lock = threading.Lock()


def run(*args, input=None, check=True):
    print("+", " ".join(map(str, args)), flush=True)
    proc = subprocess.run(list(map(str, args)), input=input, text=True,
                          capture_output=True)
    if check and proc.returncode:
        raise RuntimeError(f"Command failed ({proc.returncode}): {' '.join(map(str, args))}\n{proc.stderr}")
    return proc


def output(*args):
    return run(*args).stdout.strip()


def az_json(*args):
    return json.loads(output("az", *args, "-o", "json"))


def kusto(endpoint, db, csl, management=False):
    # Reusing the CLI token matters for sub-second polling; Azure CLI startup
    # for every Kusto request would dominate the latency being measured.
    global _token, _token_deadline
    with _token_lock:
        if not _token or time.monotonic() >= _token_deadline:
            tenant = ("--tenant", KUSTO_TENANT) if KUSTO_TENANT else ()
            _token = output("az", "account", "get-access-token", "--resource",
                            "https://api.kusto.windows.net", *tenant, "--query", "accessToken", "-o", "tsv")
            _token_deadline = time.monotonic() + 1800
        token = _token
    url = endpoint.rstrip("/") + ("/v1/rest/mgmt" if management else "/v2/rest/query")
    # TLS 1.3 negotiation to this ADX endpoint intermittently ends in EOF on
    # macOS; TLS 1.2 + HTTP/1.1 succeeds. Keep certificate verification ON.
    # Never log the Authorization header or its token.
    command = ["curl", "--silent", "--show-error", "--fail-with-body", "--max-time", "35",
               "--http1.1", "--tlsv1.2", "--tls-max", "1.2",
               "--header", "Authorization: Bearer " + token,
               "--header", "Content-Type: application/json", "--data-binary", "@-", url]
    # Opt-in tunnel (host:port), keeping the real host name for TLS.
    if KUSTO_CONNECT_TO:
        host = urllib.parse.urlparse(endpoint).hostname
        command[1:1] = ["--connect-to", f"{host}:443:{KUSTO_CONNECT_TO}"]
    for attempt in range(3):
        response = subprocess.run(command, input=json.dumps({"db": db, "csl": csl}),
                                  text=True, capture_output=True)
        # curl 35 is a TLS handshake failure, before the request was sent.
        # Do not retry timeouts or HTTP errors for management commands; their
        # side effects might already have succeeded.
        if response.returncode != 35 or attempt == 2:
            break
        time.sleep(0.2 * (attempt + 1))
    if response.returncode:
        raise RuntimeError(f"Kusto {'management' if management else 'query'} request failed "
                           f"for {db or '<cluster>'} ({response.returncode}): "
                           f"{response.stderr.strip()} {response.stdout[:1500]}")
    payload = json.loads(response.stdout)
    # v2 returns a list of frames; v1 returns a Tables object.
    tables = payload.get("Tables", []) if isinstance(payload, dict) else payload
    for table in tables:
        if (table.get("TableKind") == "PrimaryResult" or table.get("TableName") == "PrimaryResult"
                or (management and table.get("TableName") == "Table_0")):
            columns = [c["ColumnName"] for c in table["Columns"]]
            return [dict(zip(columns, row)) for row in table["Rows"]]
    return []


def save(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2) + "\n")


def state_path(group):
    return STATE_DIR / (group + ".json")


def load(group):
    global KUSTO_TENANT
    state = json.loads(state_path(group).read_text())
    # The CLI's default account may be in another tenant than the environment.
    KUSTO_TENANT = state.get("tenant", "")
    return state
