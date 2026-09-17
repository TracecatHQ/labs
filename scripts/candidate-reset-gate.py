#!/usr/bin/env python3
"""Require explicit approval before Terraform restores Candidate workflows."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from collections.abc import Mapping
from pathlib import Path
from typing import TextIO


CANDIDATE_ALIAS = re.compile(r"^(?P<lab>[0-9]{3})_candidate$")
PROMPT = "Restore source-controlled Candidate workflow baseline and discard committed UI edits? [y/N] "
EXIT_DECLINED = 2


def changed_candidate_labs(plan: Mapping[str, object]) -> list[str]:
    """Return labs whose existing Candidate workflow will be changed or removed."""
    labs: set[str] = set()
    resource_changes = plan.get("resource_changes", [])
    if not isinstance(resource_changes, list):
        raise ValueError("Terraform plan JSON has an invalid resource_changes field")

    for resource in resource_changes:
        if not isinstance(resource, Mapping):
            continue
        if resource.get("mode", "managed") != "managed" or resource.get("type") != "tracecat_workflow":
            continue

        change = resource.get("change")
        if not isinstance(change, Mapping):
            continue
        actions = change.get("actions", [])
        if not isinstance(actions, list) or not ({"update", "delete"} & set(actions)):
            continue

        before = change.get("before")
        if not isinstance(before, Mapping):
            # A create has no existing UI workflow whose edits could be discarded.
            continue
        alias = before.get("alias")
        if isinstance(alias, str) and (match := CANDIDATE_ALIAS.fullmatch(alias)):
            labs.add(match.group("lab"))

    return sorted(labs)


def load_plan(saved_plan: Path, terraform_bin: str = "terraform") -> Mapping[str, object]:
    saved_plan = saved_plan.resolve()
    terraform_dir = (
        saved_plan.parent.parent if saved_plan.parent.name == ".terraform" else saved_plan.parent
    )
    result = subprocess.run(
        [terraform_bin, f"-chdir={terraform_dir}", "show", "-json", str(saved_plan)],
        check=True,
        stdout=subprocess.PIPE,
        text=True,
    )
    plan = json.loads(result.stdout)
    if not isinstance(plan, Mapping):
        raise ValueError("terraform show returned a non-object JSON document")
    return plan


def approve_reset(
    labs: list[str],
    *,
    environ: Mapping[str, str],
    input_stream: TextIO,
    output_stream: TextIO,
    interactive: bool,
) -> bool:
    if not labs:
        return True

    print("Candidate workflow baseline changes detected for labs:", file=output_stream)
    for lab in labs:
        print(f"  - {lab}", file=output_stream)

    if environ.get("CONFIRM_CANDIDATE_RESET", "").lower() == "true":
        print("Candidate workflow baseline restoration confirmed.", file=output_stream)
        return True

    if not interactive:
        print(
            "Refusing to restore Candidate workflows in a noninteractive session. "
            "Set CONFIRM_CANDIDATE_RESET=true to confirm.",
            file=output_stream,
        )
        return False

    print(PROMPT, end="", flush=True, file=output_stream)
    answer = input_stream.readline()
    return answer.strip().lower() in {"y", "yes"}


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Guard Terraform changes that restore source-controlled Candidate workflows."
    )
    parser.add_argument("saved_plan", type=Path, help="Terraform saved-plan file to inspect")
    parser.add_argument("--terraform-bin", default="terraform", help=argparse.SUPPRESS)
    args = parser.parse_args(argv)

    try:
        labs = changed_candidate_labs(load_plan(args.saved_plan, args.terraform_bin))
    except (OSError, subprocess.CalledProcessError, json.JSONDecodeError, ValueError) as error:
        print(f"Unable to inspect Terraform plan: {error}", file=sys.stderr)
        return 1

    approved = approve_reset(
        labs,
        environ=os.environ,
        input_stream=sys.stdin,
        output_stream=sys.stdout,
        interactive=sys.stdin.isatty(),
    )
    if not approved:
        print("Candidate workflow baseline restoration was not approved.", file=sys.stderr)
        return EXIT_DECLINED
    return 0


if __name__ == "__main__":
    sys.exit(main())
