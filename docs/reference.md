# Labs operations reference

Start with the [README quickstart](../README.md#quick-start).

## Setup and workspace

| Component | Responsibility |
|---|---|
| `tracecat-labs` Compose project | Shared Tracecat control plane |
| `lab-NNN` Compose projects | Scenario target services |
| Terraform | All seven labs in one deployment workspace |
| `terraform/workspace/` | Shared Terraform state |
| `just` | Setup, deployment, execution, and reporting commands |

`just setup` performs these steps:

1. Create `.env` with mode `0600` and generate infrastructure secrets.
2. Start Tracecat and enable `service_accounts` and `agent_addons` on the default tier.
3. Provision the organization service account used by Terraform.
4. Select the deployment workspace.
5. Send the model-provider credential directly to Tracecat.

Credential handling:

- Password and API-key prompts hide input.
- The login password and model credential are not saved in `.env`.
- The generated Terraform service-account key is saved in `.env` alongside infrastructure configuration.
- Agent preparation requires the organization scope `org:secret:read`.
- `just plan` and `just apply` resolve configured models to built-in organization catalog IDs; presets use those IDs to access model credentials.

| Command | Effect |
|---|---|
| `just init` | Build the local provider and initialize shared state |
| `just plan` | Preview changes across all seven labs |
| `just apply` | Apply changes across all seven labs |
| `just deploy` | Initialize, start/bootstrap Lab 001 Splunk MCP, ensure scopes, and apply |
| `just up NNN` | Start one lab's target services |
| `just down NNN` | Stop one lab's target services |

### Reset the workspace

**Destructive:** Reset deletes all Cases, workflows, agent presets, tables, and
workflow/agent folders in the selected workspace, plus Labs integrations,
secrets, and tags. It also removes local Terraform state and its backup.
The workspace and organization model credentials remain.

1. Copy `TRACECAT_WORKSPACE_ID` from `.env` and verify it identifies the workspace you intend to clear.
2. Replace `<workspace-id>` below with that value.

   ```bash
   just reset-workspace CONFIRM_WORKSPACE_ID=<workspace-id>
   ```

3. Rebuild the lab resources.

   ```bash
   just deploy
   ```

### Preserve Candidate experiments

- Commit Candidate workflow edits in Tracecat before evaluating them.
- Terraform detects committed Candidate workflow drift.
- `just apply` lists affected labs and asks before restoring the repository baseline.
- Declining stops the apply; it does not selectively apply other changes.
- Noninteractive restoration requires `CONFIRM_CANDIDATE_RESET=true`; automatic apply approval alone is insufficient.
- Change other managed workflows in repository source, then apply Terraform.

## Tools and access boundaries

The Candidate workflow orchestrates the experiment. Agent-tool workflows provide
fixed interfaces to environment services. Prefer MCP integrations or scoped
native actions when they already provide the needed interface.

| Lab | Interface | Workflow-backed Candidate tools |
|---|---|---|
| [001](../001/) | Splunk MCP | None |
| [002](../002/) | Native DuckDB action | None |
| [003](../003/) | Case content; Candidate writes rules without deploying them | None |
| [004](../004/) | Threat-log database | `query_threat_logs` |
| [005](../005/) | Eight incident MySQL databases | `query_incident_sql` |
| [007](../007/) | Case context and ordered choices | None |

Lab [006](../006/) tools:

| Purpose | Workflow alias |
|---|---|
| List event sources | `list_event_sources` |
| Inspect event schema | `get_event_source_schema` |
| Sample events | `sample_event_source` |
| Search CTI | `search_cti_reports` |
| Search MITRE techniques | `search_mitre_techniques` |
| Search Sigma rules | `search_sigma_rules` |
| Validate a Sigma rule | `validate_sigma_rule` |
| Execute a Sigma rule | `execute_sigma_rule` |
| Analyze data | `scratch_analysis` |

Current limitations:

- Labs 004–006 expose `core.workflow.execute`; supported aliases and invocation envelopes are supplied in the preset instructions.
- A Candidate that knows another workflow alias may be able to invoke it.
- Folders and categories document intended separation; they do not enforce access.
- Judge tools, scorers, and utilities are not intended for Candidate use. Lab 003 uses `validate_firewall_rule` for judging and `cleanup_firewall_rule` internally.
- Candidate workflows must not read evaluation tables or hidden Oracle data. There is no per-workflow action sandbox.

### Proposed: attached workflows

This design is **not implemented** in the current labs:

| Proposed behavior | Purpose |
|---|---|
| Use manifest `agent_tool` entries as the preset allowlist | Restrict execution to attached workflows |
| Expose each tool's description and input schema | Replace prompt-based discovery and handwritten envelopes |
| Exclude `judge_tool`, `scorer`, and `utility` workflows | Keep grading and maintenance private |
| Retain the editable Candidate workflow | Preserve experiment orchestration |

## Execution and checkpoints

### IDs and waiting

| Command | Returned ID | Use it for |
|---|---|---|
| New `just run` | `evaluation_run_id` | Status, judging, grading, and resume |
| Resumed `just run` | Original `evaluation_run_id` + new `resume_execution_id` | Original ID for judging/grading; new ID for status |
| `just judge` | `evaluation_run_id` + `judge_run_execution_id` | Judge execution ID for status |

1. Start or resume the evaluation.
2. Poll its execution with `just status` until it completes successfully.
3. Start judging with the stable Evaluation Run ID.
4. Poll the Judge execution until it completes successfully.
5. Grade using the stable Evaluation Run ID.

### Resume and concurrency

Replace `NNN` and ID placeholders. Empty arguments preserve the positional order
`lab`, `CASE_IDS`, `RUN_ID`, `BATCH_SIZE` for `just run`.

| Task | Command |
|---|---|
| Resume an evaluation | `just run NNN "" RUN_ID=<evaluation-run-id>` |
| Resume with eight concurrent trials | `just run NNN "" RUN_ID=<evaluation-run-id> BATCH_SIZE=8` |
| Run selected Cases | `just run NNN CASE_IDS=<case-a>,<case-b>` |
| Judge with eight concurrent trials | `just judge NNN RUN_ID=<evaluation-run-id> BATCH_SIZE=8` |

- Default batch size: **4**; provide a positive integer to change it.
- Table reads use cursor pagination.
- `run_trial_batch` runs each batch in parallel; the parent waits before starting the next batch.
- Explicit batches avoid expression-based loop batch sizes unsupported by the pinned runtime.
- Completed Trials are durable checkpoints. Resuming retries unfinished work.
- Rerunning judging skips Trials with complete criterion coverage.
- Retained evaluation data supports later judging and resume.

## Evaluation and reports

| Artifact | Stored as | Purpose |
|---|---|---|
| Case Template | Table row | Task, hidden references, scoring-profile selection |
| Case | Native Case and comments | Candidate-visible task and work |
| Submission | Frozen Trial data | Final answer and Case state at cutoff |
| Evidence | Frozen Trial data | Ordered tool activity and visible assistant text |
| Oracle | Frozen hidden JSON | Reference facts; does not execute tests or assign scores |
| Rubric | Frozen versioned JSON | Criteria, weights, gates, metric declarations |
| Scorer | Workflow | Apply deterministic checks and optional model grading |
| Judge | Optional agent preset | Assess answers requiring model judgment |
| Evaluation Run / Trial | Executions and durable rows | Run provenance and individual attempts |
| Metrics | Durable rows and report | Aggregate measurements |

- Judge Run orchestrates scoring; it does not require an LLM. Labs 004 and 007 have no Judge agent preset.
- Deterministic scorers run Python actions inside Tracecat workflows; environment services remain containerized targets.
- Independent measurements remain separate from the weighted composite score.
- `just grade` validates completeness and score integrity before reporting. Incomplete results include guidance to run judging; a valid low score still exits successfully.
- `just export NNN RUN_ID=<evaluation-run-id>` exports raw rows without grading integrity checks.
- Reporting uses a standard-library Go command that reads Terraform outputs and calls Tracecat directly.
- Summary metadata comes from frozen Cases. Editing current fixtures does not backfill historical results.

See the [scoring contract](../CONTRIBUTING.md#scoring-profile-contract) and
[results contract](../CONTRIBUTING.md#results-contract) for schemas, metric rules,
and `tags` / `target_ids` / `target_id` behavior.
