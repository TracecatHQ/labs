"""Deterministic scorer for the public Simbian Cyber Defense Benchmark sample.

This is a faithful, dependency-free port of ``benchmark/scorer.py`` at
e8b86d01ccefe338d455e61505ca285943635b27. The workflow version lives in
``tracecat/workflows/score-threat-hunt.yml``.
"""

from __future__ import annotations

import math
from collections import defaultdict
from datetime import datetime, timezone
from typing import Any, Iterable


def canonical_timestamp(timestamp: Any) -> str | None:
    if timestamp is None:
        return None
    value = str(timestamp).strip()
    if not value:
        return None
    if value.endswith("Z"):
        value = value[:-1] + "+00:00"
    try:
        parsed = datetime.fromisoformat(value)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    else:
        parsed = parsed.astimezone(timezone.utc)
    normalized = parsed.isoformat().replace("+00:00", "Z")
    if "." not in normalized and normalized.endswith("Z"):
        normalized = normalized[:-1] + ".000000Z"
    return normalized


def _canonical_set(items: Iterable[Any]) -> set[str]:
    return {
        normalized
        for item in items
        if (normalized := canonical_timestamp(item)) is not None
    }


def score(timestamps: list[Any], oracle: dict[str, Any]) -> dict[str, Any]:
    flags = oracle.get("flags", [])
    chains = oracle.get("chains", [])
    tactics = list(oracle.get("tactic_ids", []))

    step_tactics: dict[tuple[int, int], list[str]] = {}
    for chain in chains:
        chain_index = int(chain["chain_idx"])
        for step in chain.get("steps", []):
            step_tactics[(chain_index, int(step["step_idx"]))] = list(
                step.get("tactics", [])
            )

    submitted = _canonical_set(timestamps or [])
    coverable: dict[tuple[int, int], set[int]] = defaultdict(set)
    detected: dict[tuple[int, int], set[int]] = defaultdict(set)
    flags_detected = 0
    for flag in flags:
        key = (int(flag["chain_idx"]), int(flag["step_idx"]))
        narrative_steps = {int(item) for item in flag.get("narrative_steps") or []}
        coverable[key].update(narrative_steps)
        if canonical_timestamp(flag.get("value")) in submitted:
            detected[key].update(narrative_steps)
            flags_detected += 1

    step_ratios = {
        key: len(detected.get(key, set()) & expected) / len(expected)
        for key, expected in coverable.items()
        if expected
    }
    tactic_scores: dict[str, float] = {}
    for tactic in tactics:
        values = [
            ratio
            for key, ratio in step_ratios.items()
            if tactic in step_tactics.get(key, [])
        ]
        tactic_scores[tactic] = sum(values) / len(values) if values else math.nan

    visited = [value for value in tactic_scores.values() if not math.isnan(value)]
    overall = sum(visited) / len(visited) if visited else math.nan
    flags_total = len(flags)
    return {
        "overall": overall,
        "per_tactic": tactic_scores,
        "flags_detected": flags_detected,
        "flags_total": flags_total,
        "flags_percent": flags_detected / flags_total if flags_total else 0.0,
        "submitted_count": len(submitted),
        "per_step": {f"{chain}:{step}": value for (chain, step), value in step_ratios.items()},
    }
