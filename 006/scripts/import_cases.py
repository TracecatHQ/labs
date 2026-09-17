#!/usr/bin/env python3
"""Build the full CTI-REALM-50 Case suite from pinned HF JSONL files."""

import argparse
import json
from pathlib import Path


def load(path: Path) -> dict[str, dict]:
    return {row["id"]: row for row in (json.loads(line) for line in path.read_text().splitlines() if line.strip())}


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("objectives", type=Path, help="dataset_answers_stratified_50.jsonl (objective rows upstream)")
    parser.add_argument("ground_truth", type=Path, help="dataset_samples_stratified_50.jsonl (ground-truth rows upstream)")
    parser.add_argument("--output", type=Path, default=Path(__file__).parents[1] / "evals/cases.ndjson")
    args = parser.parse_args()
    objectives, truth = load(args.objectives), load(args.ground_truth)
    if set(objectives) != set(truth) or len(objectives) != 50:
        raise SystemExit(f"expected identical 50-id inputs, got {len(objectives)} and {len(truth)}")
    rows = []
    for sample_id in sorted(objectives):
        objective, expected = objectives[sample_id], truth[sample_id]
        platform = sample_id.split("_", 1)[0]
        gt = expected["ground_truth"]
        rows.append({
            "schema_version": 1,
            "case_id": f"cti-realm-{sample_id}",
            "case": {
                "title": f"CTI-REALM detection objective {sample_id}",
                "description": objective["detection_description"],
                "priority": "high",
                "severity": "high",
                "tags": ["cti-realm", platform, "full-50"],
                "fields": {}, "dropdowns": {},
                "payload": {"sample_id": sample_id, "platform": platform, "detection_description": objective["detection_description"]},
            },
            "oracle": {
                "criteria": {
                    "c0-cti-alignment": {"detection_description": objective["detection_description"]},
                    "c1-mitre-mapping": {"expected": gt.get("mitre_techniques", [])},
                    "c2-data-exploration": {"expected": gt.get("data_sources", [])},
                    "c3-sigma-execution": {"minimum_distinct_successful_nonempty_executions": 2},
                    "c4-matched-event-f1": {"regex_patterns": gt.get("regex_patterns", {})},
                    "c4-sigma-syntax": {"detection_description": objective["detection_description"]},
                    "c4-sigma-specificity": {"detection_description": objective["detection_description"]},
                },
                "metrics": {"sample_id": sample_id, "simulation": expected.get("simulation"), "platform": platform},
            },
        })
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text("".join(json.dumps(row, separators=(",", ":")) + "\n" for row in rows))


if __name__ == "__main__":
    main()
