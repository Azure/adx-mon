#!/usr/bin/env python3
"""Long-lived AKS, ACR and ADX resources. Never deletes an existing resource group."""
import argparse
import hashlib
import json
import re
import sys
from common import az_json, kusto, load, output, run, save, state_path


def names(group, subscription):
    slug = re.sub('[^a-z0-9]', '', group.lower())[:12]
    suffix = hashlib.sha256(f"{subscription}:{group}".encode()).hexdigest()[:8]
    return {"aks": f"{slug}-{suffix}-aks", "kusto": f"{slug}{suffix}adx",
            "acr": f"{slug}{suffix}acr"[:50]}


def aks_sku(region, subscription, requested=None):
    # list-skus --size alone cannot select a different family when a SKU is
    # restricted for this subscription. Inspect all regional SKUs once, using
    # Azure's reported family and vCPU count to check actual remaining quota.
    usage = az_json("vm", "list-usage", "--location", region, "--subscription", subscription)
    by_name = {u["name"]["value"].lower(): u for u in usage}
    skus = az_json("vm", "list-skus", "--location", region, "--all",
                   "--subscription", subscription)

    def capability(sku, name):
        return next((c["value"] for c in sku.get("capabilities", []) if c["name"] == name), None)

    def reason(sku):
        if sku.get("resourceType") != "virtualMachines":
            return "not a VM SKU"
        if sku.get("restrictions"):
            return "restricted: " + json.dumps(sku["restrictions"])
        cores = int(capability(sku, "vCPUs") or 0)
        memory = float(capability(sku, "MemoryGB") or 0)
        if cores < 4 or memory < 16:
            return "requires at least 4 vCPUs and 16 GiB for the test workloads"
        for quota_name in (sku.get("family", "").lower(), "cores"):
            quota = by_name.get(quota_name)
            if quota is None:
                return f"quota for {quota_name} was not reported"
            if int(quota["limit"]) - int(quota["currentValue"]) < cores:
                return f"insufficient {quota_name} quota for {cores} vCPUs"
        return None

    if requested:
        match = next((s for s in skus if s.get("name", "").lower() == requested.lower()), None)
        if match is None:
            raise RuntimeError(f"Requested AKS SKU {requested} is not listed in {region}")
        if problem := reason(match):
            raise RuntimeError(f"Requested AKS SKU {match['name']} cannot be used: {problem}")
        return match["name"]

    eligible = [s for s in skus if not reason(s) and
                re.match(r"Standard_[DE][0-9]", s.get("name", ""))]
    preferred = [f"Standard_D4s_v{v}" for v in (3, 4, 5, 6)]
    # Prefer the requested Ds generations; otherwise favor 4-core D variants
    # with local disks, then other D/E families. Don't pick GPU or huge VMs.
    def rank(s):
        name = s["name"]
        cores = int(capability(s, "vCPUs"))
        order = preferred.index(name) if name in preferred else len(preferred)
        family = 0 if name.startswith("Standard_D") else 1
        return (order, cores, family, 0 if "ds_" in name.lower() else 1, name)

    if eligible:
        chosen = min(eligible, key=rank)["name"]
        print(f"Selected AKS node {chosen} (available in {region} with family and regional quota)")
        return chosen
    raise RuntimeError("No suitable 4+ vCPU, 16+ GiB D/E VM SKU available with quota in " + region +
                       "; specify --aks-sku to inspect a particular size")


def kusto_skus(region, subscription):
    available = az_json("kusto", "cluster", "list-sku", "--subscription", subscription)
    preferred = ["Standard_L8as_v3", "Standard_L8s_v3", "Standard_E8as_v5",
                 "Standard_E8s_v5", "Standard_L16as_v3", "Standard_L16s_v3"]
    # The CLI's list-sku response is a list of {name, locations, restrictions}.
    return [sku for sku in preferred if any(
        s.get("name") == sku and not s.get("restrictions") and
        region.lower() in [str(x).lower() for x in s.get("locations", [])]
        for s in available)]


def existing(*args):
    result = run("az", *args, "-o", "json", check=False)
    if result.returncode == 0:
        return json.loads(result.stdout)
    if re.search(r"ResourceNotFound|ResourceGroupNotFound|NotFound|was not found|could not be found", result.stderr, re.I):
        return None
    raise RuntimeError(f"Unable to inspect Azure resource: {result.stderr}")


def create(a):
    p = state_path(a.resource_group)
    output("az", "account", "show", "-o", "json")
    sub = az_json("account", "show", "--subscription", a.subscription)
    n = names(a.resource_group, sub["id"])
    group = existing("group", "show", "-g", a.resource_group, "--subscription", a.subscription)
    if p.exists():
        state = load(a.resource_group)
        if not state.get("owned") or any((state.get("subscription") != sub["id"],
                                          state.get("region") != a.region,
                                          state.get("names") != n)):
            raise RuntimeError("Recorded environment differs from this subscription/region/names; refusing to modify it")
        if group is None:
            raise RuntimeError("Recorded resource group is missing; inspect state and recreate deliberately")
        if state.get("ready"):
            print(f"Environment already ready: {p}")
            return
        print(f"Resuming recorded partial environment: {p}", flush=True)
        if a.aks_sku and a.aks_sku.lower() != state.get("node_sku", "").lower():
            raise RuntimeError(f"AKS already selected as {state.get('node_sku')}; cannot override on resume")
        node = state["node_sku"]
    else:
        if group is not None:
            raise RuntimeError("Resource group exists without harness state; refusing to take ownership")
        node = aks_sku(a.region, sub["id"], a.aks_sku)
        print(f"Creating paid resources in {sub['id']}/{a.region}: {n}; AKS {node} x1. "
              "Kusto uses at least two nodes. Run cluster.sh delete when finished.", flush=True)
        run("az", "group", "create", "-n", a.resource_group, "-l", a.region,
            "--subscription", a.subscription)
        # Save ownership immediately so a partially failed creation can be resumed.
        state = dict(subscription=sub["id"], tenant=sub["tenantId"], region=a.region,
                     resource_group=a.resource_group, names=n, node_sku=node, owned=True)
        save(p, state)

    acr = existing("acr", "show", "-g", a.resource_group, "-n", n["acr"],
                   "--subscription", a.subscription)
    if acr is None:
        run("az", "acr", "create", "-g", a.resource_group, "-n", n["acr"],
            "--sku", "Basic", "--subscription", a.subscription)
    aks = existing("aks", "show", "-g", a.resource_group, "-n", n["aks"],
                   "--subscription", a.subscription)
    if aks is None:
        run("az", "aks", "create", "-g", a.resource_group, "-n", n["aks"],
            "--location", a.region, "--node-count", "1", "--node-vm-size", node,
            "--enable-managed-identity", "--attach-acr", n["acr"], "--generate-ssh-keys",
            "--subscription", a.subscription)
        aks = az_json("aks", "show", "-g", a.resource_group, "-n", n["aks"],
                      "--subscription", a.subscription)
    elif aks["agentPoolProfiles"][0]["vmSize"].lower() != node.lower():
        raise RuntimeError("Existing AKS node SKU does not match recorded harness state")
    kubelet = aks["identityProfile"]["kubeletidentity"]
    if state.get("kubelet_client_id") and state["kubelet_client_id"] != kubelet["clientId"]:
        raise RuntimeError("Existing AKS kubelet identity does not match recorded harness state")
    state["kubelet_client_id"] = kubelet["clientId"]
    save(p, state)
    cluster = existing("kusto", "cluster", "show", "-g", a.resource_group,
                       "-n", n["kusto"], "--subscription", a.subscription)
    if cluster is None:
        skus = [a.kusto_sku] if a.kusto_sku else kusto_skus(a.region, sub["id"])
        if not skus:
            raise RuntimeError("No storage-optimized ADX SKU reported for this region; check Kusto quota")
        print(f"Creating Kusto with capacity=2; trying {skus}", flush=True)
        failures = []
        for sku in skus:
            cmd = run("az", "kusto", "cluster", "create", "-g", a.resource_group,
                      "-n", n["kusto"], "--location", a.region,
                      "--sku", f"name={sku}", "tier=Standard", "capacity=2",
                      "--enable-streaming-ingest", "false", "--subscription", a.subscription,
                      check=False)
            if cmd.returncode == 0:
                state["kusto_sku"] = sku
                break
            failures.append(f"{sku}: {cmd.stderr}")
            # Only subscription quota/availability failures justify another SKU.
            if not re.search(r"quota|SkuNotAvailable|not available|insufficient", cmd.stderr, re.I):
                raise RuntimeError(cmd.stderr)
        else:
            raise RuntimeError("All storage-optimized Kusto SKUs failed:\n" + "\n".join(failures))
        cluster = az_json("kusto", "cluster", "show", "-g", a.resource_group,
                          "-n", n["kusto"], "--subscription", a.subscription)
    else:
        if a.kusto_sku and a.kusto_sku.lower() != cluster["sku"]["name"].lower():
            raise RuntimeError("Existing Kusto SKU differs from --kusto-sku; refusing to resize")
        state["kusto_sku"] = cluster["sku"]["name"]
    state["endpoint"] = cluster["uri"]
    save(p, state)
    for db in ("Metrics", "Logs"):
        if existing("kusto", "database", "show", "-g", a.resource_group,
                    "--cluster-name", n["kusto"], "--database-name", db,
                    "--subscription", a.subscription) is None:
            run("az", "kusto", "database", "create", "-g", a.resource_group,
                "--cluster-name", n["kusto"], "--database-name", db,
                "--read-write-database", f"location={a.region}",
                "soft-delete-period=P1D", "hot-cache-period=P1D", "--subscription", a.subscription)
    # Cluster-wide AllDatabasesAdmin is an ARM principal assignment (not a KQL .add role).
    # Grant the *node pool* identity, not the AKS control-plane identity.
    assignment_args = ("-g", a.resource_group, "--cluster-name", n["kusto"],
                       "--principal-assignment-name", "adxmon-kubelet-admin",
                       "--subscription", a.subscription)
    assignment = existing("kusto", "cluster-principal-assignment", "show", *assignment_args)
    if assignment is None:
        run("az", "kusto", "cluster-principal-assignment", "create",
            *assignment_args, "--principal-id", kubelet["objectId"], "--principal-type", "App",
            "--role", "AllDatabasesAdmin", "--tenant-id", sub["tenantId"])
        assignment = az_json("kusto", "cluster-principal-assignment", "show", *assignment_args)
    if assignment.get("role") != "AllDatabasesAdmin" or assignment.get("principalId", "").lower() != kubelet["objectId"].lower():
        raise RuntimeError("Kusto kubelet admin assignment was not confirmed")
    state["ready"] = True
    save(p, state)
    print(f"Ready. State: {p}\nNext: tools/dev/azure-test-env/test.sh run --resource-group {a.resource_group}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    c = sub.add_parser("create")
    c.add_argument("--subscription", required=True)
    c.add_argument("--region", required=True)
    c.add_argument("--resource-group", required=True)
    c.add_argument("--aks-sku")
    c.add_argument("--kusto-sku")
    for verb in ("status", "delete"):
        s = sub.add_parser(verb)
        s.add_argument("--resource-group", required=True)
        if verb == "delete":
            s.add_argument("--yes", action="store_true")
    a = parser.parse_args()
    try:
        if a.command == "create":
            create(a)
        elif a.command == "status":
            print(json.dumps(load(a.resource_group), indent=2))
        else:
            state = load(a.resource_group)
            if not state["owned"]:
                raise RuntimeError("Not owned by this harness")
            if not a.yes:
                raise RuntimeError("Pass --yes to delete the recorded resource group and ALL its resources")
            run("az", "group", "delete", "-n", a.resource_group, "--subscription",
                state["subscription"], "--yes")
            state_path(a.resource_group).unlink()
    except (RuntimeError, FileNotFoundError) as e:
        sys.exit(str(e))


if __name__ == "__main__":
    main()
