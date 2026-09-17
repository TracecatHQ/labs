"""Rebuild the 50 English MCQ Cases from the pinned SEvenLLM checkout.

Usage: python3 007/scripts/import_cases.py /path/to/SEVENLLM-Dataset
"""

from __future__ import annotations

import json
import re
import subprocess
import sys
from pathlib import Path

PIN = "1de23ce55cadc984d3f3a7b52c4035a68c6cd5b0"
CJK = re.compile(r"[\u3400-\u9fff]")


def main() -> None:
    source = Path(sys.argv[1]).resolve()
    revision = subprocess.run(
        ["git", "-C", str(source), "rev-parse", "HEAD"],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    if revision != PIN:
        raise SystemExit(f"expected revision {PIN}, got {revision}")
    records = [json.loads(line) for line in (source / "test.jsonl").read_text(encoding="utf-8").splitlines()]
    selected = [record for record in records if 1201 <= int(record["id"]) <= 1250]
    if len(selected) != 50:
        raise SystemExit(f"expected 50 English MCQs, got {len(selected)}")

    cases = []
    for record in selected:
        instruction = record.get("instruction")
        if not isinstance(instruction, dict):
            raise SystemExit(f"record {record['id']} is not MCQ")
        choices = instruction.get("choice")
        if not isinstance(choices, dict) or list(choices) != list("ABCD"):
            raise SystemExit(f"record {record['id']} does not have ordered A-D choices")
        visible_text = "\n".join(
            [str(record["input"]), str(instruction["question"]), *map(str, choices.values())]
        )
        if CJK.search(visible_text):
            raise SystemExit(f"record {record['id']} contains CJK text")
        answer = str(record.get("output", "")).strip().upper()
        if len(answer) != 1 or answer not in "ABCD":
            raise SystemExit(f"record {record['id']} has invalid answer")
        description = "\n\n".join(
            [
                "Context:\n" + str(record["input"]),
                "Question:\n" + str(instruction["question"]),
                "Choices:\n" + "\n".join(f"{letter}. {choices[letter]}" for letter in "ABCD"),
                "Respond with exactly one letter: A, B, C, or D.",
            ]
        )
        case = {
            "schema_version": 1,
            "case_id": f"sevenllm-en-mcq-{record['id']}",
            "case": {
                "title": f"SEvenLLM English MCQ {record['id']}",
                "description": description,
                "priority": "medium",
                "severity": "medium",
                "tags": ["sevenllm", "english", "mcq", str(record["category"]).lower().replace(" ", "-")],
                "fields": {},
                "dropdowns": {},
                "payload": {
                    "source_id": record["id"],
                    "category": record["category"],
                    "context": record["input"],
                    "question": instruction["question"],
                    "choices": [{"letter": letter, "text": choices[letter]} for letter in "ABCD"],
                },
            },
            "oracle": {
                "criteria": {"correct-option": {"expected": answer}},
                "metrics": {"accuracy": 1},
            },
        }
        encoded_visible = json.dumps(case["case"], ensure_ascii=False)
        if "thought" in encoded_visible.lower() or record.get("thought") in encoded_visible:
            raise SystemExit(f"record {record['id']} leaked thought content")
        cases.append(case)

    destination = Path(__file__).resolve().parents[1] / "evals" / "cases.ndjson"
    destination.write_text(
        "".join(json.dumps(case, ensure_ascii=False, separators=(",", ":")) + "\n" for case in cases),
        encoding="utf-8",
    )
    print(f"wrote {len(cases)} cases to {destination} from {PIN}")


if __name__ == "__main__":
    main()
