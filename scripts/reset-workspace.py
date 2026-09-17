#!/usr/bin/env python3
"""Delete Labs-managed data from one Tracecat workspace.

The workspace, organization, service account, API key, and model credentials are
deliberately retained. Re-running this command after a completed reset is safe.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
LAB_TAGS = {
    "run-first", "modify", "run-second", "agent-under-test", "judge", "internal",
    "agent-tool", "scorer", "judge-tool", "utility", "deterministic", "model", "hybrid",
}


class Tracecat:
    def __init__(self, base_url: str, api_key: str, workspace_id: str) -> None:
        self.base_url = base_url.rstrip("/")
        self.workspace_id = workspace_id
        self.headers = {
            "Authorization": f"Bearer {api_key}",
            "Accept": "application/json",
            "Content-Type": "application/json",
            "x-tracecat-role-workspace-id": workspace_id,
        }

    def request(self, method: str, path: str, body: object | None = None) -> object | None:
        separator = "&" if "?" in path else "?"
        url = f"{self.base_url}{path}{separator}workspace_id={urllib.parse.quote(self.workspace_id)}"
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(url, data=data, method=method, headers=self.headers)
        try:
            with urllib.request.urlopen(request, timeout=120) as response:
                raw = response.read()
        except urllib.error.HTTPError as error:
            if error.code == 404:
                return None
            raise RuntimeError(f"{method} {path} returned {error.code}: {error.read().decode()}") from error
        return json.loads(raw) if raw else None

    def list_all(self, path: str) -> list[dict]:
        result = self.request("GET", path)
        if isinstance(result, list):
            return result
        if isinstance(result, dict):
            return result.get("items", [])
        return []


def manifest_owned_names() -> tuple[set[str], set[str]]:
    secret_names: set[str] = set()
    integration_slugs: set[str] = set()
    for manifest_path in sorted(ROOT.glob("[0-9][0-9][0-9]/tracecat/tracecat.json")):
        manifest = json.loads(manifest_path.read_text())
        secret_names.update(item["name"] for item in manifest.get("secrets", []))
        integration_slugs.update(item["catalog_slug"] for item in manifest.get("mcp_integrations", []))
    return secret_names, integration_slugs


def collect_folders(api: Tracecat, endpoint: str, parent: str = "/", paginated: bool = False) -> list[dict]:
    query = urllib.parse.urlencode({"parent_path": parent, "limit": 100} if paginated else {"parent_path": parent})
    children = api.list_all(f"{endpoint}?{query}")
    result: list[dict] = []
    for child in children:
        result.extend(collect_folders(api, endpoint, child["path"], paginated))
        result.append(child)
    return result


def delete_each(api: Tracecat, endpoint: str, items: list[dict], label: str, body: object | None = None) -> int:
    deleted = 0
    for item in items:
        item_id = item.get("id")
        if not item_id:
            continue
        api.request("DELETE", f"{endpoint}/{item_id}", body)
        deleted += 1
    print(f"Deleted {deleted} {label}")
    return deleted


def delete_paginated(api: Tracecat, endpoint: str, label: str) -> int:
    total = 0
    while items := api.list_all(f"{endpoint}?limit=100"):
        total += delete_each(api, endpoint, items, label)
    return total


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--workspace-id", required=True)
    parser.add_argument("--confirm", required=True)
    args = parser.parse_args()
    if args.confirm != args.workspace_id:
        parser.error("--confirm must exactly match --workspace-id")

    api_url = os.environ.get("TRACECAT_API_URL")
    api_key = os.environ.get("TRACECAT_API_KEY")
    if not api_url or not api_key:
        parser.error("TRACECAT_API_URL and TRACECAT_API_KEY are required")
    api = Tracecat(api_url, api_key, args.workspace_id)
    secret_names, integration_slugs = manifest_owned_names()

    delete_paginated(api, "/cases", "cases")
    delete_paginated(api, "/workflows", "workflows")
    delete_paginated(api, "/agent/presets", "agent presets")

    integrations = [item for item in api.list_all("/mcp-integrations") if item.get("slug") in integration_slugs]
    delete_each(api, "/mcp-integrations", integrations, "Labs MCP integrations")
    secrets = [item for item in api.list_all("/secrets") if item.get("name") in secret_names]
    delete_each(api, "/secrets", secrets, "Labs secrets")
    delete_each(api, "/tables", api.list_all("/tables"), "evaluation tables")

    tags = [item for item in api.list_all("/tags") if item.get("name") in LAB_TAGS]
    delete_each(api, "/tags", tags, "Labs tags")
    agent_tags = [item for item in api.list_all("/agent-tags?limit=100") if item.get("name") in LAB_TAGS]
    delete_each(api, "/agent-tags", agent_tags, "Labs agent tags")
    delete_each(api, "/folders", collect_folders(api, "/folders"), "workflow folders", {"recursive": False})
    delete_each(api, "/agent-folders", collect_folders(api, "/agent-folders", paginated=True), "agent folders", {"recursive": False})

    state = ROOT / "terraform/workspace/terraform.tfstate"
    backup = ROOT / "terraform/workspace/terraform.tfstate.backup"
    state.unlink(missing_ok=True)
    backup.unlink(missing_ok=True)
    print(f"Workspace {args.workspace_id} is ready for a clean Terraform apply")
    return 0


if __name__ == "__main__":
    sys.exit(main())
