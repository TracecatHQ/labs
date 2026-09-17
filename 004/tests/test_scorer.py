from __future__ import annotations

import importlib.util
import json
import math
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def load_scorer():
    spec = importlib.util.spec_from_file_location("lab004_scorer", ROOT / "evals" / "scorer.py")
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


def load_workflow_main():
    path = ROOT / "tracecat" / "workflows" / "score-threat-hunt.yml"
    lines = path.read_text(encoding="utf-8").splitlines()
    start = next(index for index, line in enumerate(lines) if line.strip() == "script: |") + 1
    end = next(index for index in range(start, len(lines)) if lines[index].startswith("  returns:"))
    source = "\n".join(line[10:] if line.startswith("          ") else line for line in lines[start:end])
    namespace = {}
    exec(compile(source, str(path), "exec"), namespace)
    return namespace["main"]


class SimbianScorerTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.scorer = load_scorer()
        cls.workflow_main = staticmethod(load_workflow_main())
        cls.case = json.loads((ROOT / "evals" / "cases.ndjson").read_text(encoding="utf-8"))
        cls.oracle = cls.case["oracle"]

    def test_golden_first_ten_flags(self) -> None:
        timestamps = [flag["value"] for flag in self.oracle["flags"][:10]]
        result = self.scorer.score(timestamps, self.oracle)
        self.assertEqual(result["flags_detected"], 10)
        self.assertEqual(result["flags_total"], 3912)
        self.assertTrue(math.isclose(result["overall"], 0.0271494708994709))
        self.assertTrue(math.isclose(result["per_tactic"]["TA0003"], 0.1))

    def test_empty_and_complete_submissions_bound_coverage(self) -> None:
        empty = self.scorer.score([], self.oracle)
        complete = self.scorer.score([flag["value"] for flag in self.oracle["flags"]], self.oracle)
        self.assertEqual(empty["overall"], 0)
        self.assertEqual(empty["flags_detected"], 0)
        self.assertEqual(complete["overall"], 1)
        self.assertEqual(complete["flags_detected"], 3912)

    def test_timestamp_normalization_and_no_false_positive_penalty(self) -> None:
        timestamp = self.oracle["flags"][0]["value"]
        equivalent = timestamp.removesuffix("Z") + "+00:00"
        baseline = self.scorer.score([timestamp], self.oracle)
        noisy = self.scorer.score([equivalent, equivalent, "not-a-timestamp", "2099-01-01T00:00:00Z"], self.oracle)
        self.assertEqual(noisy["overall"], baseline["overall"])
        self.assertEqual(noisy["flags_detected"], baseline["flags_detected"])

    def test_oracle_shape_matches_public_sample(self) -> None:
        self.assertEqual(self.oracle["seed"], 176)
        self.assertEqual(len(self.oracle["chains"]), 3)
        self.assertEqual(len(self.oracle["flags"]), 3912)
        self.assertEqual(len(self.oracle["tactic_ids"]), 9)
        self.assertEqual(self.case["case"]["payload"]["event_count"], 155350)

    def test_workflow_matches_reference_port(self) -> None:
        timestamps = [flag["value"] for flag in self.oracle["flags"][:10]]
        expected = self.scorer.score(timestamps, self.oracle)
        trial = {
            "submission": {"comments": [{"content": json.dumps({"timestamps": timestamps})}]},
            "oracle": self.oracle,
        }
        result = self.workflow_main(trial)
        self.assertIsNone(result["evaluation_error"])
        self.assertEqual(result["criteria"][0]["value"], expected["overall"])
        metrics = {item["metric_id"]: item["value"] for item in result["metrics"]}
        self.assertEqual(metrics["flags_detected"], expected["flags_detected"])
        self.assertEqual(metrics["flags_total"], expected["flags_total"])


if __name__ == "__main__":
    unittest.main()
