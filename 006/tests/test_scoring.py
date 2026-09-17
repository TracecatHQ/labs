import importlib.util
import json
from pathlib import Path
import textwrap
import unittest


PATH = Path(__file__).parents[1] / "scoring.py"
SPEC = importlib.util.spec_from_file_location("lab006_scoring", PATH)
assert SPEC and SPEC.loader
S = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(S)


class CTIRealmScoringTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        workflow = (PATH.parent / "tracecat" / "workflows" / "score-cti-realm.yml").read_text()
        script = workflow.split("        script: |\n", 1)[1].split("  returns:", 1)[0]
        namespace = {}
        exec(textwrap.dedent(script), namespace)
        cls.workflow_main = staticmethod(namespace["main"])

    def test_jaccard_and_aggregation_weights(self):
        self.assertEqual(S.jaccard({"T1"}, {"T1", "T2"}), 0.5)
        self.assertEqual(S.upstream_matched_jaccard({"T1", "EXTRA"}, {"T1", "T2"}), 0.5)
        self.assertAlmostEqual(S.aggregate(1, 1, 1, 1, 1, 1, 1), 1.0)

    def test_c0_requires_successful_tagged_report_retrieval(self):
        self.assertFalse(S.has_tagged_cti_retrieval([]))
        self.assertFalse(S.has_tagged_cti_retrieval([
            {"tool": "search_cti_reports", "tag": "linux", "success": True, "events": []},
        ]))
        self.assertTrue(S.has_tagged_cti_retrieval([
            {"tool": "search_cti_reports", "tag": "linux", "success": True, "events": [{"title": "Report"}]},
        ]))

    def test_c3_requires_distinct_successful_nonempty_sigma_executions(self):
        rule = "title: Test\ndetection:\n  condition: selection"
        calls = [
            {"tool": "execute_sigma_rule", "sigma_rule": rule, "data_source": "Device", "success": True, "events": [{"id": 1}]},
            {"tool": "execute_sigma_rule", "sigma_rule": rule, "data_source": "Device", "success": True, "events": [{"id": 1}]},
        ]
        self.assertFalse(S.c3_success(calls))
        calls.append({"tool": "execute_sigma_rule", "sigma_rule": rule + "\nlevel: high", "data_source": "Device", "success": True, "events": [{"id": 1}]})
        self.assertTrue(S.c3_success(calls))

    def test_submission_requires_exact_rule_source_and_recorded_event_values(self):
        rule = "title: Test\ndetection:\n  condition: selection"
        calls = [{
            "tool": "execute_sigma_rule", "sigma_rule": rule, "data_source": "Device",
            "success": True, "events": [{"Host": "a", "Pid": 7}, {"Host": "b", "Pid": 8}],
        }]
        self.assertTrue(S.verified_submission([{"Host": "a", "Pid": 7}], rule, "Device", calls))
        self.assertTrue(S.verified_submission([{"Host": "a"}], rule, "Device", calls))
        self.assertFalse(S.verified_submission([{}], rule, "Device", calls))
        self.assertFalse(S.verified_submission([{"Host": "a", "Pid": 8}], rule, "Device", calls))
        self.assertFalse(S.verified_submission([{"Host": "a", "invented": True}], rule, "Device", calls))
        self.assertFalse(S.verified_submission([{"Host": "a", "Pid": 7}], rule + "\n", "Device", calls))
        self.assertFalse(S.verified_submission([{"Host": "a", "Pid": 7}], rule, "Other", calls))

    def test_matched_event_f1_preserves_oracle_field_slot_definition(self):
        patterns = {"host": "^a$", "verb": "(?i)^create$"}
        self.assertEqual(S.matched_event_f1([{"host": "a", "verb": "CREATE"}], patterns), 1.0)
        self.assertAlmostEqual(S.matched_event_f1([{"host": "a", "verb": "no"}], patterns), 0.5)
        self.assertEqual(S.matched_event_f1([{"host": "b", "verb": "no"}], patterns), 0.0)

    def test_frozen_trial_provenance_and_tool_evidence_shape(self):
        rule = "title: Test"
        trial = {
            "case": {"payload": {"sample_id": "wrong"}},
            "submission": {"case": {"payload": {"sample_id": "linux_001"}}},
            "evidence": [
                {"evidence_type": "tool_call", "evidence": {"tool_call_id": "x", "tool_name": "core.workflow.execute", "input": {"workflow_alias": "execute_sigma_rule", "trigger_inputs": {"sigma_rule": rule, "data_source": "Device"}}}},
                {"evidence_type": "tool_result", "evidence": {"tool_call_id": "x", "success": True, "content": [{"type": "text", "text": "{\"data\":{\"matched_events\":[{\"id\":1}]}}"}]}},
            ],
        }
        self.assertEqual(S.sample_id_from_trial(trial), "linux_001")
        self.assertEqual(S.recorded_calls_from_trial(trial), [{
            "tool": "execute_sigma_rule", "sigma_rule": rule, "data_source": "Device",
            "tag": "", "success": True, "events": [{"id": 1}],
        }])

    def test_profile_is_named_as_derivative_and_declares_new_contract_metrics(self):
        profile = json.loads((PATH.parent / "evals" / "rubric.json").read_text())
        self.assertEqual(profile["profile_id"], "lab-006-cti-realm-sigma-derivative-50")
        criteria = {item["criterion_id"] for item in profile["criteria"]}
        self.assertIn("c3-sigma-execution", criteria)
        self.assertIn("c4-matched-event-f1", criteria)
        metrics = {item["metric_id"] for item in profile["metrics"]}
        self.assertIn("iterative_sigma_execution", metrics)
        self.assertIn("matched_event_f1", metrics)
        self.assertIn("matched_events_verified", metrics)

    def test_inline_workflow_enforces_sigma_contract_and_scores_verified_events(self):
        def evidence(alias, inputs, content, sequence):
            call_id = f"call-{sequence}"
            return [
                {"sequence": sequence, "evidence_type": "tool_call", "evidence": {"tool_call_id": call_id, "tool_name": "core.workflow.execute", "input": {"workflow_alias": alias, "trigger_inputs": inputs}}},
                {"sequence": sequence + 1, "evidence_type": "tool_result", "evidence": {"tool_call_id": call_id, "success": True, "content": content}},
            ]

        draft = "title: Draft\ndetection:\n  selection:\n    Host: a\n  condition: selection"
        final_rule = "title: Final\ndetection:\n  selection:\n    Host: a\n  condition: selection"
        final = {"sigma_rule": final_rule, "data_source": "Device", "matched_events": [{"Host": "a", "Verb": "CREATE"}]}
        frozen_evidence = []
        frozen_evidence += evidence("execute_sigma_rule", {"sigma_rule": draft, "data_source": "Device"}, {"matched_events": [{"Host": "a"}]}, 0)
        frozen_evidence += evidence("execute_sigma_rule", {"sigma_rule": final_rule, "data_source": "Device"}, {"matched_events": [{"Host": "a", "Verb": "CREATE"}]}, 2)
        frozen_evidence += evidence("get_event_source_schema", {"data_source": "Device"}, {"columns": ["Host", "Verb"]}, 4)
        frozen_evidence += evidence("sample_event_source", {"data_source": "Aux"}, {"matched_events": [{"Host": "sample"}]}, 6)
        frozen_evidence += evidence("search_cti_reports", {"tag": "linux"}, {"reports": [{"title": "Linux threat"}]}, 8)
        trial = {
            "trial_id": "trial-1",
            "submission": {"case": {"payload": {"sample_id": "linux_001"}}},
            "final_answer": final,
            "evidence": frozen_evidence,
            "oracle": {"criteria": {
                "c1-mitre-mapping": {"expected": []},
                "c2-data-exploration": {"expected": ["Device", "Aux"]},
                "c4-matched-event-f1": {"regex_patterns": {"Host": "^a$", "Verb": "(?i)^create$"}},
            }},
        }
        judge = {"criteria": [
            {"criterion_id": "c0-cti-alignment", "value": 0.8, "reason": "Relevant report.", "evidence_refs": ["evidence[8]"]},
            {"criterion_id": "c4-sigma-syntax", "value": 1, "reason": "Valid.", "evidence_refs": []},
            {"criterion_id": "c4-sigma-specificity", "value": 0.75, "reason": "Specific.", "evidence_refs": []},
        ], "evaluation_error": None, "judge_session_id": "judge-1"}
        result = self.workflow_main(trial, judge)
        by_criterion = {item["criterion_id"]: item["value"] for item in result["criteria"]}
        self.assertEqual(by_criterion["c3-sigma-execution"], 1)
        self.assertEqual(by_criterion["c4-matched-event-f1"], 1.0)
        self.assertEqual(by_criterion["c2-data-exploration"], 1.0)
        by_metric = {item["metric_id"]: item["value"] for item in result["metrics"]}
        profile_metrics = {item["metric_id"] for item in json.loads((PATH.parent / "evals" / "rubric.json").read_text())["metrics"]}
        self.assertEqual(set(by_metric), profile_metrics)
        self.assertTrue(by_metric["iterative_sigma_execution"])
        self.assertTrue(by_metric["matched_events_verified"])
        self.assertEqual(by_metric["matched_event_f1"], 1.0)
        self.assertAlmostEqual(by_metric["normalized_score"], 0.946875)

        invalid = dict(trial)
        invalid["final_answer"] = {"sigma_rule": final_rule, "kql_query": "Device | take 1", "query_results": []}
        error = self.workflow_main(invalid, judge)
        self.assertIn("exactly sigma_rule, data_source, and matched_events", error["evaluation_error"])


if __name__ == "__main__":
    unittest.main()
