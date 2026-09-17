"""Rebuild the single Case from a pinned Simbian checkout.

Usage: python3 004/scripts/import_sample.py /path/to/cyber_defense_benchmark
"""

from __future__ import annotations

import json
import sys
import zipfile
from pathlib import Path

PIN = "e8b86d01ccefe338d455e61505ca285943635b27"


def main() -> None:
    source = Path(sys.argv[1]).resolve()
    archive = source / "datasets" / "sample.zip"
    with zipfile.ZipFile(archive) as bundle:
        public_bytes = bundle.read("sample.json")
        public = json.loads(public_bytes)
        oracle = json.loads(bundle.read("sample_flags.json"))
    oracle["criteria"] = {
        "hierarchical-coverage": {"expected": "Maximize hierarchical tactic coverage"}
    }
    oracle["metrics"] = {
        "coverage_overall": 1.0,
        **{f"coverage_{tactic.lower()}": 1.0 for tactic in oracle["tactic_ids"]},
        "flags_detected": len(oracle["flags"]),
        "flags_total": len(oracle["flags"]),
        "flags_percent": 1.0,
    }
    case = {
        "schema_version": 1,
        "case_id": "simbian-public-sample-seed-176",
        "case": {
            "title": "Threat hunt: identify malicious events in the Simbian public sample",
            "description": public["briefing"],
            "priority": "high",
            "severity": "high",
            "tags": ["simbian", "threat-hunting", "mitre-attack"],
            "fields": {},
            "dropdowns": {},
            "payload": {
                "target_id": "simbian-public-sample",
                "seed": public["seed"],
                "event_count": len(public["logs"]),
                "query_workflow": "query_threat_logs",
            },
        },
        "oracle": oracle,
    }
    if public["seed"] != oracle["seed"]:
        raise SystemExit("sample and flags seeds differ")
    destination = Path(__file__).resolve().parents[1] / "evals" / "cases.ndjson"
    destination.write_text(json.dumps(case, separators=(",", ":")) + "\n", encoding="utf-8")
    target_archive = Path(__file__).resolve().parents[1] / "assets" / "sample-data.zip"
    member = zipfile.ZipInfo("sample.json", date_time=(1980, 1, 1, 0, 0, 0))
    member.compress_type = zipfile.ZIP_DEFLATED
    member.external_attr = 0o100644 << 16
    with zipfile.ZipFile(target_archive, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as bundle:
        bundle.writestr(member, public_bytes, compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
    print(f"wrote {destination} from {PIN}: {len(public['logs'])} logs, {len(oracle['flags'])} flags")


if __name__ == "__main__":
    main()
