# Tracecat labs

Security-agent evaluations provisioned as Tracecat configuration. The
`tracecat-labs` Compose project is the shared control plane, while `lab-NNN`
Compose projects are target services for individual scenarios. Terraform owns
lab resources in Tracecat's deployment workspace, Docker Compose owns only
scenario targets, and `just`
provides the public lifecycle interface. The repository has no separate
provisioning CLI, Python control runtime, or copied Tracecat source tree.

Each evaluation follows the same path:

```text
Case Template → Candidate Run → Trial Case + durable Evaluation Run → Judge Run → grade report
```

The Case is the Candidate's unit of work. Visible analysis, evidence, timelines,
and final answers belong on that Case. Hidden expected facts are the Oracle;
the versioned grading contract is the Rubric. Private model reasoning is not a
stored or graded artifact.

## Quick start

> [!IMPORTANT]
> These labs require the Tracecat Enterprise `service_accounts` entitlement for
> API access. They are intended only for training, experimentation, and
> evaluation.

Requirements: Terraform 1.11+, Go, Docker, `just`, `jq`, `curl`, `openssl`, and
Git LFS.

Run the interactive setup. It creates a mode-`0600` `.env`, generates local
infrastructure secrets, starts Tracecat, provisions an organization service
account for Terraform, selects the deployment workspace, and sends one
model-provider credential directly to Tracecat:

```bash
just setup
```

Password and API-key input is hidden. The Tracecat login password and model
credential are never written to `.env`; only the generated Terraform service
account key is stored there. Startup configures the default tier with only the
`service_accounts` entitlement needed for API access. Then run:

```bash
just init 001
just up 001
just plan 001
just apply 001
just run 001
just status 001 RUN_ID=<candidate-run-id>
just judge 001 RUN_ID=<candidate-run-id>
just status 001 RUN_ID=<judge-run-id>
just grade 001 RUN_ID=<candidate-run-id>
```

`just plan` and `just apply` resolve the configured Candidate and Judge models
to built-in organization catalog entries. The presets use those catalog IDs
to access organization model credentials. The organization service account
requires `org:secret:read` for agent preparation.

### Upgrading an existing checkout

`just setup` preserves an existing Compose project name and adds its matching
Tracecat network to `.env`. If the previous Terraform state owns a per-lab
workspace, setup finds its owning organization through your memberships,
selects that same workspace, and tells you to run:

```bash
just migrate-workspace 001
just plan 001
```

Migration first verifies the selected workspace ID, writes a mode-`0600` state
backup under `.cache/terraform-migrations/`, and forgets only Terraform's
ownership of the workspace container. The tables, workflows, presets, retained
runs, and scores remain in place. `just plan` and `just apply` refuse to proceed
while unmigrated legacy workspace state is present.

`just setup` configures the shared control plane and deployment workspace; it is
not lab-specific. Apply only one lab configuration to that shared workspace at
a time. The `lab-NNN` target projects remain separate from the shared
`tracecat-labs` control plane. Tracecat 1.0.0-rc.1 requires the
`multi_workspace` entitlement to create another workspace, and the labs
intentionally do not enable it.

Both triggers return a native Tracecat workflow execution ID and run
asynchronously. Wait for Candidate Run to complete before starting Judge Run,
then wait for Judge Run before grading. `just grade` does not trigger or wait
for judging; incomplete results include guidance to run `just judge`. A valid
low score still exits successfully. Completed Candidate Runs are retained in
the Tracecat `evaluation_runs` table for later judging. Results are written to
`NNN/results/<candidate-run-id>/scores.csv` and `summary.json`. Historical runs
without trial details still receive the generic rubric and workflow-timing
report, with a notice that classification and agent-latency metrics require a
re-judge. Candidate latency absent from the frozen snapshot requires a new
Candidate Run and is reported as unavailable after re-judging. Grading is implemented as a standard-library Go command; it reads
Terraform outputs and calls Tracecat directly, without Ruby or a grading-only
toolchain.

Each summary includes `tags`, `target_ids`, and `target_id`, extracted from the
run's frozen Cases. A single target produces `target_id`; multiple targets
produce a sorted, deduplicated `target_ids` list and `target_id: null`. Vulhub
IDs retain the full directory, such as `bash/CVE-2014-6271`. Missing metadata
produces empty arrays and `target_id: null` (currently labs 001 and 002).
Metadata is supplied by lab authors, not inferred by the grader; fixture
changes do not backfill historical runs.

## Labs

| Lab | Candidate task | Score |
|---|---|---|
| [001](001/) | Investigate one EventBridge alert and write an evidence-backed incident timeline | True-positive hard gate plus 16 weighted findings |
| [002](002/) | Classify 20 BOTSv3 alerts from bounded evidence objects | Exact-evidence hard gate; determination 50; incident relevance 50 |
| [003](003/) | Mitigate one of eight unauthenticated vulnerability targets with a deployable ModSecurity ruleset | Deployability hard gate; five malicious and five benign fixtures per target |

## Adding a lab

See [CONTRIBUTING.md](CONTRIBUTING.md) for naming, required files, evaluation
contracts, the README template, and the validation checklist.
