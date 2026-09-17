#!/usr/bin/env python3
"""Build Lab 005 Cases from the pinned SecRL o1/v1/test JSON files."""

import argparse
import json
from pathlib import Path

INCIDENTS = (5, 34, 38, 39, 55, 134, 166, 322)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=Path, help="SecRL checkout at a1b234f7...")
    parser.add_argument("--output", type=Path, default=Path(__file__).parents[1] / "evals/cases.ndjson")
    args = parser.parse_args()
    source = args.source / "secgym/questions/o1/v1/test"
    rows = []
    for incident in INCIDENTS:
        matches = sorted(source.glob(f"incident_{incident}_qa_*.json"))
        if len(matches) != 1:
            raise SystemExit(f"expected one question file for incident {incident}, found {matches}")
        questions = json.loads(matches[0].read_text())
        for index, question in enumerate(questions, 1):
            case_id = f"secrl-incident-{incident}-q-{index:03d}"
            rows.append({
                "schema_version": 1,
                "case_id": case_id,
                "case": {
                    "title": f"SecRL incident {incident}, question {index}",
                    "description": f"{question.get('context', '').strip()}\n\nQUESTION: {question['question'].strip()}",
                    "priority": "medium",
                    "severity": "medium",
                    "tags": ["secrl", f"incident-{incident}", "o1", "v1", "test"],
                    "fields": {},
                    "dropdowns": {},
                    "payload": {
                        "incident_id": incident,
                        "question_index": index,
                        "question": question["question"],
                        "context": question.get("context", ""),
                    },
                },
                "oracle": {
                    "criteria": {
                        "answer-correct": {"expected": question["answer"]},
                        "discounted-partial-reward": {
                            "expected_answer": question["answer"],
                            "solution_steps": question.get("solution", []),
                            "discount_factor": 0.4,
                        },
                    },
                    "metrics": {
                        "incident_id": str(incident),
                        "solution_step_count": len(question.get("solution", [])),
                        "source_metadata": {
                            key: question.get(key)
                            for key in ("start_alert", "end_alert", "start_entities", "end_entities", "shortest_alert_path")
                            if key in question
                        },
                    },
                },
            })
    if len(rows) != 589:
        raise SystemExit(f"expected 589 cases, built {len(rows)}")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text("".join(json.dumps(row, separators=(",", ":")) + "\n" for row in rows))


if __name__ == "__main__":
    main()
