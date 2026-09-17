from __future__ import annotations

import importlib.util
import io
import json
import os
import subprocess
import unittest
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).resolve().parents[1] / "candidate-reset-gate.py"
SPEC = importlib.util.spec_from_file_location("candidate_reset_gate", SCRIPT)
assert SPEC and SPEC.loader
gate = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(gate)


def workflow_change(alias: str, actions: list[str], *, before: bool = True) -> dict:
    return {
        "address": f'module.lab["{alias[:3]}"].tracecat_workflow.workflow["{alias}"]',
        "mode": "managed",
        "type": "tracecat_workflow",
        "name": "workflow",
        "change": {
            "actions": actions,
            "before": {"alias": alias} if before else None,
            "after": {"alias": alias} if "delete" not in actions or "create" in actions else None,
        },
    }


class CandidateChangesTests(unittest.TestCase):
    def test_excludes_new_candidate_workflow_creates(self) -> None:
        plan = {"resource_changes": [workflow_change("001_candidate", ["create"], before=False)]}
        self.assertEqual(gate.changed_candidate_labs(plan), [])

    def test_excludes_unrelated_changes(self) -> None:
        scorer = workflow_change("001_scorer", ["update"])
        folder = workflow_change("001_candidate", ["update"])
        folder["type"] = "tracecat_workflow_folder"
        plan = {"resource_changes": [scorer, folder]}
        self.assertEqual(gate.changed_candidate_labs(plan), [])

    def test_finds_update_delete_and_replacement_once_per_lab(self) -> None:
        plan = {
            "resource_changes": [
                workflow_change("007_candidate", ["delete"]),
                workflow_change("001_candidate", ["update"]),
                workflow_change("005_candidate", ["delete", "create"]),
                workflow_change("001_candidate", ["delete", "create"]),
            ]
        }
        self.assertEqual(gate.changed_candidate_labs(plan), ["001", "005", "007"])


class ApprovalTests(unittest.TestCase):
    def approve(
        self,
        *,
        answer: str = "",
        environ: dict[str, str] | None = None,
        interactive: bool = True,
    ) -> tuple[bool, str]:
        output = io.StringIO()
        approved = gate.approve_reset(
            ["001", "005"],
            environ=environ or {},
            input_stream=io.StringIO(answer),
            output_stream=output,
            interactive=interactive,
        )
        return approved, output.getvalue()

    def test_interactive_decline_is_default(self) -> None:
        approved, output = self.approve(answer="\n")
        self.assertFalse(approved)
        self.assertIn("  - 001", output)
        self.assertIn("  - 005", output)
        self.assertIn(gate.PROMPT, output)

    def test_interactive_yes_approves(self) -> None:
        approved, _ = self.approve(answer="yes\n")
        self.assertTrue(approved)

    def test_noninteractive_requires_candidate_reset_confirmation(self) -> None:
        approved, output = self.approve(interactive=False)
        self.assertFalse(approved)
        self.assertIn("CONFIRM_CANDIDATE_RESET=true", output)

    def test_candidate_reset_confirmation_approves_noninteractive(self) -> None:
        approved, _ = self.approve(
            environ={"CONFIRM_CANDIDATE_RESET": "true"}, interactive=False
        )
        self.assertTrue(approved)

    def test_auto_approve_does_not_bypass_noninteractive_gate(self) -> None:
        approved, _ = self.approve(environ={"AUTO_APPROVE": "true"}, interactive=False)
        self.assertFalse(approved)

    def test_no_candidate_changes_never_prompts(self) -> None:
        output = io.StringIO()
        approved = gate.approve_reset(
            [],
            environ={},
            input_stream=io.StringIO(""),
            output_stream=output,
            interactive=False,
        )
        self.assertTrue(approved)
        self.assertEqual(output.getvalue(), "")


class CliTests(unittest.TestCase):
    @mock.patch.object(gate.subprocess, "run")
    def test_load_plan_uses_terraform_show_json(self, run: mock.Mock) -> None:
        run.return_value = subprocess.CompletedProcess(
            args=[], returncode=0, stdout=json.dumps({"resource_changes": []})
        )
        plan = gate.load_plan(Path("saved.tfplan"), "/opt/bin/terraform")
        self.assertEqual(plan, {"resource_changes": []})
        saved_plan = Path("saved.tfplan").resolve()
        run.assert_called_once_with(
            [
                "/opt/bin/terraform",
                f"-chdir={saved_plan.parent}",
                "show",
                "-json",
                str(saved_plan),
            ],
            check=True,
            stdout=subprocess.PIPE,
            text=True,
        )

    @mock.patch.object(gate.subprocess, "run")
    def test_load_plan_uses_workspace_for_plan_in_dot_terraform(self, run: mock.Mock) -> None:
        run.return_value = subprocess.CompletedProcess(
            args=[], returncode=0, stdout=json.dumps({"resource_changes": []})
        )
        saved_plan = (Path("workspace") / ".terraform" / "saved.tfplan").resolve()
        gate.load_plan(saved_plan, "/opt/bin/terraform")
        run.assert_called_once_with(
            [
                "/opt/bin/terraform",
                f"-chdir={saved_plan.parent.parent}",
                "show",
                "-json",
                str(saved_plan),
            ],
            check=True,
            stdout=subprocess.PIPE,
            text=True,
        )

    @mock.patch.object(gate, "load_plan")
    def test_decline_has_distinct_exit_code(self, load_plan: mock.Mock) -> None:
        load_plan.return_value = {
            "resource_changes": [workflow_change("001_candidate", ["update"])]
        }
        with (
            mock.patch.object(gate.sys, "stdin", io.StringIO("")),
            mock.patch.object(gate.sys, "stdout", io.StringIO()),
            mock.patch.object(gate.sys, "stderr", io.StringIO()),
            mock.patch.dict(os.environ, {}, clear=True),
        ):
            self.assertEqual(gate.main(["plan.tfplan"]), gate.EXIT_DECLINED)


if __name__ == "__main__":
    unittest.main()
