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
Case Template → Run Evaluation → Candidate workflow → Candidate agent → Judge Run → grade report
```

The Case is the Candidate's unit of work. Visible analysis, evidence, timelines,
and final answers belong on that Case. Hidden expected facts are the Oracle;
the versioned grading contract is the Rubric. Private model reasoning is not a
stored or graded artifact.

## Evaluation model

Labs separates the task being evaluated from the method used to score it. A
task may be classification, question answering, investigation, or detection
engineering. Its scorer may use exact matching, deterministic calculations,
executable verification, an LLM Judge, or a combination of these methods. A
per-Trial score is distinct from a metric that aggregates results across many
Trials.

| Concept | Tracecat primitive | Responsibility |
|---|---|---|
| Case Template | Table row | Candidate-visible task, hidden reference data, and scoring-profile selection |
| Case | Native Case and comments | Candidate-visible task, analysis, evidence, and work product |
| Candidate workflow | Tracecat workflow | User-editable prompt, context, skills, and orchestration for one Trial |
| Candidate agent | Agent preset | Performs the task with only its permitted tools |
| Submission | Frozen Trial data | Exact final answer and the Case state at the submission cutoff |
| Evidence | Frozen Trial data | Ordered platform-captured tool activity and visible assistant text |
| Oracle | Hidden JSON frozen with the Trial | Expected answers and reference facts; it never executes tests or decides a score |
| Rubric | Versioned JSON frozen with the Trial | Criteria, weights, hard gates, and metric declarations |
| Scorer | Tracecat workflow | Applies deterministic functions and optional model grading to the frozen inputs |
| Judge | Optional agent preset used by a scorer | Assesses answers that cannot be graded deterministically |
| Evaluation Run | Workflow execution and durable table row | Groups selected Trials, provenance, and completion state |
| Trial | Child workflow execution and durable table row | One Candidate attempt on one Case Template |
| Metrics | Durable result rows and the Go report | Aggregate measurements such as accuracy, mean reward, or F1 |

The Oracle supplies hidden reference data; the Rubric states what matters; the
Scorer applies that policy. Judge Run is the orchestration step and does not
imply that every scorer uses an LLM. Deterministic scorers run as source-owned
Python actions inside Tracecat workflows. Environment-specific services remain
containerized targets reached through scoped actions.

Criteria may be binary or numeric. Binary criteria award either zero or their
full weight. Numeric criteria award partial credit in the range from zero to
their full weight. Hard gates remain binary and force the composite Trial score
to zero when missed. Independent measurements retain their native meaning and
are reported separately from the weighted composite score.

## Quick start

> [!IMPORTANT]
> These labs require the Tracecat Enterprise `service_accounts` entitlement for
> API access and `agent_addons` for Terraform-managed agent folders and tags.
> They are intended only for training, experimentation, and evaluation.

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
account key is stored there. Startup configures the default tier with the
`service_accounts` and `agent_addons` entitlements used by the workspace
baseline. Then run:

```bash
just up 001
just deploy
just run 001
just status 001 RUN_ID=<candidate-run-id>
just judge 001 RUN_ID=<candidate-run-id>
just status 001 RUN_ID=<judge-run-id>
just grade 001 RUN_ID=<candidate-run-id>
```

Terraform uses one state under `terraform/workspace/`. `just init`, `just plan`,
and `just apply` always operate on all seven labs. `just deploy` starts the Lab
001 Splunk MCP target before applying the workspace. To discard all Labs data
and rebuild while preserving the workspace and model credentials, run:

```bash
just reset-workspace CONFIRM_WORKSPACE_ID=$TRACECAT_WORKSPACE_ID
just deploy
```

In Tracecat, open a lab folder. Modify and commit `NNN Candidate — Modify` to
change its prompt, context, skills, budgets, or agent orchestration. The default
workflow invokes the editable `Lab NNN Candidate` preset under **Agents**. Run
`NNN Run evaluation — Run first`, then run `NNN Judge — Run second` with the
Evaluation Run ID. The `Agent tools`, `Scoring`, and `Utilities` subfolders
contain fixed evaluation infrastructure. Deterministic Labs 004 and 007 have
Judge workflows but no Judge agent preset.

Candidate workflows are a cooperative evaluation boundary. They may orchestrate
prompts, skills, and agents, but must not read the evaluation tables or hidden
Oracle data. Tracecat does not currently provide a per-workflow action sandbox.

### Candidate environment interfaces

The Candidate workflow and an agent-tool workflow serve different purposes.
`NNN Candidate — Modify` is the user-editable orchestration surface that invokes
the Candidate preset. An agent-tool workflow is fixed evaluation infrastructure
that gives the Candidate a narrow interface to a containerized environment.
Labs should use workflow-backed tools only when an MCP integration or a scoped
native Tracecat action does not already provide the required interface.

| Lab | Candidate environment interface | Workflow-backed Candidate tools |
|---|---|---|
| 001 | Splunk MCP integration | None; MCP exposes the investigation tools |
| 002 | Native `core.duckdb.execute_sql` action | None |
| 003 | Case content only | None; the Candidate writes a ModSecurity ruleset without deploying it |
| 004 | Containerized threat-log database | `query_threat_logs` |
| 005 | Eight incident-specific MySQL databases | `query_incident_sql` |
| 006 | CTI corpus, event data, and Sigma execution service | `list_event_sources`, `get_event_source_schema`, `sample_event_source`, `search_cti_reports`, `search_mitre_techniques`, `search_sigma_rules`, `validate_sigma_rule`, `execute_sigma_rule`, and `scratch_analysis` |
| 007 | Case context and ordered choices | None |

Judge-only verifiers and scorer workflows are not Candidate tools. For example,
Lab 003's Judge uses `validate_firewall_rule`, while deterministic and hybrid
scorers remain under `Scoring`. Utilities such as `cleanup_firewall_rule` also
remain internal. Folder placement and the `agent-tool`, `judge-tool`, `scorer`,
and `utility` categories document this separation; they do not currently
enforce it.

Today, Labs 004, 005, and 006 grant the Candidate preset the generic
`core.workflow.execute` action and name the supported workflow aliases in its
instructions. This is a cooperative boundary: a Candidate that knows another
workflow alias may be able to invoke it. It also makes tool discovery depend on
prompt text and hand-written invocation envelopes.

Tracecat's planned **attached workflows** support should use each manifest's
`agent_tool` entries as the Candidate preset's workflow allowlist. An attached
workflow should be presented to the agent with its description and input
schema, and the agent should be able to execute only the workflows attached to
that preset. `judge_tool`, `scorer`, and `utility` workflows must not be attached
to Candidate presets. This will provide least-privilege workflow execution and
remove brittle alias and envelope instructions while preserving the visible,
modifiable Candidate workflow as the evaluation's orchestration surface.

Large suites use cursor pagination and bounded batches. Adjust concurrency with
`BATCH_SIZE=<n>`. Pending trials are partitioned into batches of at most that
size. The shared `run_trial_batch` workflow runs each batch in parallel, and the
parent waits for it before starting the next batch. This avoids expression-based
loop batch sizes, which the pinned Tracecat runtime does not support. Failed
trials leave incomplete checkpoint coverage; resume retries only unfinished work. To resume a stopped Run Evaluation workflow from its completed Trial
checkpoints, rerun `just run NNN RUN_ID=<candidate-run-id>`; rerunning
`just judge` skips Trials with complete criterion coverage.

`just plan` and `just apply` resolve the configured Candidate and Judge models
to built-in organization catalog entries. The presets use those catalog IDs
to access organization model credentials. The organization service account
requires `org:secret:read` for agent preparation.

Terraform detects committed edits to a Candidate workflow. `just apply` warns
before restoring the source-controlled baseline and defaults to keeping the UI
edit. Noninteractive restoration requires `CONFIRM_CANDIDATE_RESET=true`;
`AUTO_APPROVE` does not bypass this safeguard.

`just setup` configures the shared control plane and deployment workspace; it is
not lab-specific. The `lab-NNN` target projects remain separate from the shared
`tracecat-labs` control plane.

New triggers return a native Tracecat workflow execution ID and run
asynchronously. A resumed Run Evaluation workflow also returns its stable
Evaluation Run ID and the new resume execution ID. Wait for Run Evaluation to complete before
starting Judge Run, then wait for Judge Run before grading. `just grade` does
not trigger or wait for judging; incomplete results include guidance to run
`just judge`. A valid low score still exits successfully. Completed Candidate
Evaluation Runs are retained in the Tracecat evaluation tables for later judging and
resume. Results are written to `NNN/results/<candidate-run-id>/scores.csv`,
`metrics.csv`, and `summary.json`. Grading is implemented as a standard-library
Go command; it reads Terraform outputs and calls Tracecat directly, without
Ruby or a grading-only toolchain.

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
| [004](004/) | Hunt malicious events in the public Simbian benchmark sample | Hierarchical narrative-step and tactic coverage |
| [005](005/) | Answer 589 SecRL questions against eight incident databases | Semantic answer success plus discounted solution-step reward |
| [006](006/) | Develop and test detections for 50 CTI-REALM objectives | Five weighted trajectory and outcome checkpoints |
| [007](007/) | Answer 50 English SEvenLLM multiple-choice questions | Deterministic exact-choice accuracy |

## Adding a lab

See [CONTRIBUTING.md](CONTRIBUTING.md) for naming, required files, evaluation
contracts, the README template, and the validation checklist.
