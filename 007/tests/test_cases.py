from __future__ import annotations

import importlib.util
import json
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CJK = re.compile(r"[\u3400-\u9fff]")


def load_scorer():
    spec = importlib.util.spec_from_file_location("lab007_scorer", ROOT / "evals" / "scorer.py")
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


def load_workflow_main():
    path = ROOT / "tracecat" / "workflows" / "score-mcq-answer.yml"
    lines = path.read_text(encoding="utf-8").splitlines()
    start = next(index for index, line in enumerate(lines) if line.strip() == "script: |") + 1
    end = next(index for index in range(start, len(lines)) if lines[index].startswith("  returns:"))
    source = "\n".join(line[10:] if line.startswith("          ") else line for line in lines[start:end])
    namespace = {}
    exec(compile(source, str(path), "exec"), namespace)
    return namespace["main"]


class SevenLLMFixtureTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.scorer = load_scorer()
        cls.workflow_main = staticmethod(load_workflow_main())
        cls.cases = [json.loads(line) for line in (ROOT / "evals" / "cases.ndjson").read_text(encoding="utf-8").splitlines()]

    def test_exactly_fifty_english_mcq_cases(self) -> None:
        self.assertEqual(len(self.cases), 50)
        self.assertEqual([item["case"]["payload"]["source_id"] for item in self.cases], list(range(1201, 1251)))
        for item in self.cases:
            payload = item["case"]["payload"]
            self.assertFalse(CJK.search(json.dumps(item["case"], ensure_ascii=False)))
            self.assertEqual([choice["letter"] for choice in payload["choices"]], list("ABCD"))
            self.assertTrue(all(choice["text"] for choice in payload["choices"]))

    def test_no_thought_or_reference_answer_leaks_into_visible_case(self) -> None:
        for item in self.cases:
            visible = json.dumps(item["case"], ensure_ascii=False).lower()
            self.assertNotIn('"thought"', visible)
            self.assertNotIn('"output"', visible)
            expected = item["oracle"]["criteria"]["correct-option"]["expected"]
            self.assertNotIn(f'"correct-option"', visible)
            self.assertIn(expected, "ABCD")

    def test_exact_option_scoring(self) -> None:
        self.assertEqual(self.scorer.score(" A\n", "A")["correct"], 1)
        self.assertEqual(self.scorer.score("a", "A")["correct"], 1)
        for invalid in ("A.", "A B", "answer: A", "", "AA", "E", None):
            self.assertEqual(self.scorer.score(invalid, "A")["correct"], 0)
        self.assertEqual(self.scorer.score("B", "A")["correct"], 0)

    def test_profile_is_binary_and_complete(self) -> None:
        profile = json.loads((ROOT / "evals" / "rubric.json").read_text(encoding="utf-8"))
        criterion = profile["criteria"][0]
        self.assertEqual(criterion["criterion_id"], "correct-option")
        self.assertEqual(criterion["type"], "binary")
        self.assertEqual(criterion["weight"], 100)
        self.assertEqual(profile["metrics"][0]["metric_id"], "accuracy")

    def test_workflow_enforces_exact_answer(self) -> None:
        oracle = {"criteria": {"correct-option": {"expected": "A"}}}
        correct = self.workflow_main({"final_answer": " a\n", "oracle": oracle})
        ambiguous = self.workflow_main({"final_answer": "A or B", "oracle": oracle})
        encoded_comment = self.workflow_main({
            "final_answer": "A",
            "submission": {"comments": [{"content": '"A"'}]},
            "oracle": oracle,
        })
        self.assertEqual(correct["criteria"][0]["value"], 1)
        self.assertEqual(correct["metrics"], [{"metric_id": "accuracy", "value": 1}])
        self.assertEqual(ambiguous["criteria"][0]["value"], 0)
        self.assertEqual(encoded_comment["criteria"][0]["value"], 1)


if __name__ == "__main__":
    unittest.main()
