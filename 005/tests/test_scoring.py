import unittest
import importlib.util
from pathlib import Path

MODULE_PATH = Path(__file__).parents[1] / "scoring.py"
SPEC = importlib.util.spec_from_file_location("lab005_scoring", MODULE_PATH)
assert SPEC and SPEC.loader
SCORING = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SCORING)
discounted_partial_reward = SCORING.discounted_partial_reward
score = SCORING.score
incident_id_from_trial = SCORING.incident_id_from_trial


class SecRLScoringTest(unittest.TestCase):
    def test_correct_answer_is_binary_success_and_full_reward(self):
        self.assertEqual(score(True, True, [], []), (1, 1.0))

    def test_reflection_can_overturn_first_answer_judgment(self):
        self.assertEqual(score(True, False, [True, True], [True, True]), (0, 0.4))

    def test_discount_runs_backward_and_skips_final_step(self):
        self.assertAlmostEqual(discounted_partial_reward([True, True, False, False]), 0.16 + 0.064)

    def test_step_reflection_must_confirm_partial_credit(self):
        self.assertEqual(score(False, False, [True, True], [False, True]), (0, 0.0))

    def test_incident_provenance_uses_frozen_submission(self):
        trial = {"case":{"payload":{"incident_id":999}}, "submission":{"case":{"payload":{"incident_id":55}}}}
        self.assertEqual(incident_id_from_trial(trial), "55")


if __name__ == "__main__":
    unittest.main()
