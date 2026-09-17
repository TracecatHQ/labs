"""Deterministic helpers for the CTI-REALM Sigma derivative scorer."""

import json
import re
from typing import Any


def jaccard(found: set[str], expected: set[str]) -> float:
    if not found and not expected:
        return 1.0
    return len(found & expected) / len(found | expected) if found and expected else 0.0


def normalize_events(value: Any) -> list[dict[str, Any]]:
    """Extract an event list from the supported execution-result envelopes."""
    if isinstance(value, str):
        try:
            value = json.loads(value)
        except json.JSONDecodeError:
            return []
    if isinstance(value, dict):
        for key in ("matched_events", "events", "rows", "Rows", "data", "results"):
            if key in value:
                return normalize_events(value[key])
        tables = value.get("Tables")
        if isinstance(tables, list) and tables:
            table = tables[0]
            columns = [c.get("ColumnName", c.get("name")) for c in table.get("Columns", [])]
            return [dict(zip(columns, row)) for row in table.get("Rows", [])]
    if isinstance(value, list) and all(isinstance(event, dict) for event in value):
        decoded: list[dict[str, Any]] = []
        for block in value:
            if block.get("type") == "text" and isinstance(block.get("text"), str):
                decoded.extend(normalize_events(block["text"]))
        return decoded or value
    return []


# Compatibility name for callers that consume Kusto-shaped execution envelopes.
normalize_rows = normalize_events


def sample_id_from_trial(trial: dict[str, Any]) -> str:
    return str(trial.get("submission", {}).get("case", {}).get("payload", {}).get("sample_id", ""))


def recorded_calls_from_trial(trial: dict[str, Any]) -> list[dict[str, Any]]:
    """Pair frozen tool-call and tool-result evidence into scorer inputs."""
    pending: dict[Any, dict[str, Any]] = {}
    calls: list[dict[str, Any]] = []
    for event in trial.get("evidence", []):
        evidence = event.get("evidence", {}) if isinstance(event, dict) else {}
        if event.get("evidence_type") == "tool_call":
            inputs = evidence.get("input", {}) if isinstance(evidence.get("input"), dict) else {}
            name = inputs.get("workflow_alias") or inputs.get("alias") or evidence.get("tool_name", "")
            trigger = inputs.get("trigger_inputs", inputs)
            pending[evidence.get("tool_call_id")] = {
                "tool": str(name).replace("-", "_").split(".")[-1],
                "sigma_rule": trigger.get("sigma_rule") if isinstance(trigger, dict) else None,
                "data_source": trigger.get("data_source") if isinstance(trigger, dict) else None,
                "tag": trigger.get("tag", "") if isinstance(trigger, dict) else "",
                "success": False,
                "events": [],
            }
        elif event.get("evidence_type") == "tool_result" and evidence.get("tool_call_id") in pending:
            call = pending[evidence["tool_call_id"]]
            call["success"] = bool(evidence.get("success"))
            call["events"] = normalize_events(evidence.get("content"))
            calls.append(call)
    return calls


def _canonical(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"), default=str)


def _is_recorded_projection(proposed: dict[str, Any], recorded: dict[str, Any]) -> bool:
    """Require every submitted field and value to occur in one recorded event."""
    return bool(proposed) and all(
        key in recorded and _canonical(value) == _canonical(recorded[key])
        for key, value in proposed.items()
    )


def verified_submission(
    submitted_events: Any,
    submitted_rule: Any,
    submitted_source: Any,
    calls: list[dict[str, Any]],
) -> bool:
    """Verify events are exact key/value projections of a recorded execution."""
    proposed = normalize_events(submitted_events)
    if not proposed:
        return False
    for call in calls:
        if (
            call.get("tool") == "execute_sigma_rule"
            and call.get("success")
            and call.get("sigma_rule") == submitted_rule
            and call.get("data_source") == submitted_source
        ):
            recorded = normalize_events(call.get("events", call.get("result")))
            if all(any(_is_recorded_projection(event, row) for row in recorded) for event in proposed):
                return True
    return False


def c3_success(calls: list[dict[str, Any]]) -> bool:
    """Require two distinct, successful Sigma executions with nonempty matches."""
    executions = {
        (_canonical(call.get("sigma_rule")), _canonical(call.get("data_source")))
        for call in calls
        if call.get("tool") == "execute_sigma_rule"
        and call.get("success")
        and normalize_events(call.get("events", call.get("result")))
    }
    return len(executions) >= 2


def upstream_matched_jaccard(found: set[str], expected: set[str]) -> float:
    """Retain CTI-REALM C1/C2 behavior, which scores expected-set matches."""
    return jaccard(found & expected, expected)


def has_tagged_cti_retrieval(calls: list[dict[str, Any]]) -> bool:
    return any(
        call.get("tool") == "search_cti_reports"
        and call.get("success")
        and str(call.get("tag", "")).strip()
        and normalize_events(call.get("events", call.get("result")))
        for call in calls
    )


def matched_event_f1(events: Any, patterns: dict[str, str]) -> float:
    """Compute field-slot F1 over verified matched events and Oracle regexes."""
    rows = normalize_events(events)
    if not rows or not patterns:
        return 0.0
    matched, fields = 0, set()
    for row in rows:
        lower = {str(key).lower(): value for key, value in row.items()}
        for field, pattern in patterns.items():
            if field.lower() in lower and re.search(pattern, str(lower[field.lower()])):
                matched += 1
                fields.add(field)
    precision = matched / (len(patterns) * len(rows))
    recall = len(fields) / len(patterns)
    return 2 * precision * recall / (precision + recall) if precision + recall else 0.0


def aggregate(
    c0: float,
    c1: float,
    c2: float,
    c3: float,
    event_f1: float,
    sigma_syntax: float,
    sigma_specificity: float,
) -> float:
    return (
        0.125 * c0
        + 0.075 * c1
        + 0.1 * c2
        + 0.05 * c3
        + 0.5 * event_f1
        + 0.0375 * sigma_syntax
        + 0.1125 * sigma_specificity
    )
