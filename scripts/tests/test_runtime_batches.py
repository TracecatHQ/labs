"""Exercise the deployed loader scripts and the workflow scheduling contract."""
import json
from pathlib import Path
import subprocess
import sys
import types
import unittest
from unittest.mock import Mock, patch

ROOT = Path(__file__).resolve().parents[2]
RUNTIME = ROOT / "terraform/modules/runtime/workflows"


def read_workflow(name):
    result = subprocess.run(
        ["terraform", "console"], cwd=ROOT, text=True, capture_output=True,
        input=f'jsonencode(yamldecode(file("{RUNTIME / name}")))\n', check=True,
    )
    return json.loads(json.loads(result.stdout))["definition"]


def loader(workflow, tables):
    namespace = {}
    module = types.ModuleType("tracecat_registry")
    module.ctx = types.SimpleNamespace(tables=tables)
    with patch.dict(sys.modules, tracecat_registry=module):
        exec(workflow["actions"][0]["args"]["script"], namespace)
    return namespace


class RuntimeBatchesTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.candidate = read_workflow("run-evaluation.yml")
        cls.judge = read_workflow("judge-run.yml")
        cls.batch = read_workflow("run-trial-batch.yml")

    def test_candidate_batch_sizes_and_checkpoint_filter(self):
        templates = [dict(case_id=str(i), lab_id="007", enabled=True,
                          schema_version=1, case={}, oracle={}, profile={},
                          profile_id="p", profile_version=1) for i in range(11)]
        for size in (1, 2, 4, 7, 20):
            with self.subTest(size=size):
                tables = Mock()
                tables.lookup.return_value = None
                tables.search_rows.side_effect = lambda table, **kw: {
                    "case_templates": templates,
                    "evaluation_trials": [dict(evaluation_run_id="run", case_id="0", status="completed")],
                }[table]
                work = loader(self.candidate, tables)["main"]([], "007", "", "run", size, "007_candidate")
                batches = work["batches"]
                self.assertEqual([len(b) for b in batches],
                                 [min(size, 10-i) for i in range(0, 10, size)])
                items = [item for batch in batches for item in batch]
                self.assertEqual({i["template"]["case_id"] for i in items}, {str(i) for i in range(1, 11)})
                self.assertEqual(len(items), 10)
                self.assertTrue(all(i["evaluation_run_id"] == "run" and i["candidate_workflow_alias"] == "007_candidate" for i in items))

    def judge_loader(self, complete):
        tables = Mock()
        tables.lookup.return_value = dict(status="completed", expected_trial_count=11, lab_id="005", profile={})
        rows = {
            "evaluation_trials": [dict(evaluation_run_id="run", status="completed", trial={"trial_id": str(i)}) for i in range(11)],
            "evaluation_evidence": [dict(evaluation_run_id="run", trial_id=str(i), sequence=j, evidence_type="test", evidence={}) for i in range(11) for j in range(3)],
        }
        tables.search_rows.side_effect = lambda table, **kw: rows[table]
        namespace = loader(self.judge, tables)
        namespace["complete_trials"] = lambda *args: complete
        return namespace["main"]

    def test_judge_batch_sizes_and_checkpoint_filter(self):
        for size in (1, 2, 4, 7, 20):
            with self.subTest(size=size):
                work = self.judge_loader({"0"})("run", size, "judge-run")
                self.assertEqual([len(b) for b in work["batches"]], [min(size, 10-i) for i in range(0, 10, size)])
                items = [item for batch in work["batches"] for item in batch]
                self.assertEqual([i["trial"]["trial_id"] for i in items], [str(i) for i in range(1, 11)])
                self.assertTrue(all(i["scoring_run_execution_id"] == "judge-run" and i["evaluation_run_id"] == "run" and i["lab_id"] == "005" for i in items))
                self.assertTrue(all(len(i["trial"]["evidence"]) == 3 for i in items))

    def test_no_batches_when_scoring_is_complete(self):
        self.assertEqual(self.judge_loader({str(i) for i in range(11)})("run", 2, "judge")["batches"], [])

    def test_invalid_sizes_fail_before_reading_tables(self):
        for size in (0, -1):
            for workflow, args in ((self.candidate, ([], "007", "", "run", size, "007_candidate")),
                                   (self.judge, ("run", size, "judge"))):
                tables = Mock()
                with self.assertRaisesRegex(ValueError, "batch_size must be positive"):
                    loader(workflow, tables)["main"](*args)
                tables.lookup.assert_not_called()

    def test_batches_wait_sequentially_and_children_run_in_parallel(self):
        for workflow, ref, alias in ((self.candidate, "run_candidate_trials", "candidate_trial"),
                                     (self.judge, "run_scoring_trials", "scoring_trial")):
            action = next(a for a in workflow["actions"] if a["ref"] == ref)
            self.assertTrue(action["for_each"].endswith(".batches }}"))
            args = action["args"]
            self.assertEqual(args["loop_strategy"], "sequential")
            self.assertEqual(args["wait_strategy"], "wait")
            self.assertIsInstance(args["batch_size"], int)
            self.assertEqual(args["workflow_alias"], "run_trial_batch")
            self.assertEqual(args["trigger_inputs"], {"workflow_alias": alias, "items": "${{ var.batch }}"})
        args = self.batch["actions"][0]["args"]
        self.assertEqual(args["loop_strategy"], "parallel")
        self.assertEqual(args["wait_strategy"], "wait")
        self.assertEqual(args["fail_strategy"], "isolated")
        self.assertEqual(args["trigger_inputs"], "${{ var.inputs }}")


if __name__ == "__main__":
    unittest.main()
