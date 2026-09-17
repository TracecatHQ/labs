"""Deterministic exact-option scorer for SEvenLLM English MCQ Trials."""

from __future__ import annotations

from typing import Any


def score(answer: Any, expected: Any) -> dict[str, Any]:
    normalized = str(answer).strip().upper() if answer is not None else ""
    valid = len(normalized) == 1 and normalized in "ABCD"
    correct = int(valid and normalized == str(expected).strip().upper())
    return {"normalized": normalized if valid else None, "correct": correct}
