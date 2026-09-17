# Contributing a lab

Every lab uses the shared Tracecat evaluation loop. A lab contributes Case
Templates, one scoring profile, a Candidate preset, and only the scorer
workflow, target services, or Judge preset it needs. Deterministic functions run
with `core.script.run_python` inside Tracecat workflows. Do not add a lab CLI,
provisioning plugin, copied Tracecat checkout, Python control runtime, or a
standalone scoring service.

## Naming

| Item | Convention |
|---|---|
| Lab directory | Next zero-padded number: `NNN/` |
| Candidate preset | Name `Lab NNN Candidate`, slug `NNN-candidate` |
| Judge preset, when used | Name `Lab NNN Judge`, slug `NNN-judge` |
| Evaluation entrypoint | `NNN Run evaluation — Run first` / `NNN_run_evaluation` |
| Candidate workflow | `NNN Candidate — Modify` / `NNN_candidate` |
| Judge entrypoint | `NNN Judge — Run second` / `NNN_judge` |
| Shared runtime workflows | `run_evaluation`, `run_candidate`, `judge_run`, and their internal Trial subflows |
| Helper workflow file | Kebab case, such as `validate-firewall-rule.yml` |
| Helper workflow alias | Snake case, such as `validate_firewall_rule` |
| Case Template ID | Stable kebab case |
| Profile, criterion, and metric IDs | Stable kebab case |

The Case is the Candidate's unit of work. Candidate-visible analysis, evidence,
timelines, and answers belong in the Case description or comments. Candidate
Run also freezes the Candidate's exact final return as the Submission. The
Oracle contains hidden expected facts. The Rubric describes what matters; its
Scorer workflow applies that policy. Platform-captured Evidence contains
ordered tool calls, tool results, errors, and visible assistant text. There is
no private-reasoning or “COT” object.

## Required layout

```text
NNN/
├── README.md
├── compose.yml                 # target services only
├── evals/
│   ├── cases.ndjson            # one Case Template per line
│   └── rubric.json             # one versioned scoring profile
├── assets/                     # source data, optional
├── target/                     # target configuration, optional
├── tracecat/
│   ├── tracecat.json           # workspace manifest
│   ├── agent_presets/
│   │   ├── candidate.md
│   │   └── judge.md             # only for model/hybrid scoring
│   └── workflows/              # scorer and environment helpers
└── terraform/main.tf           # shared module invocation
```

Use only `README.md` for lab documentation. Put source attribution next to the
asset it describes or in the README; do not add `IMPLEMENTATION.md` or a
lab-level `PROVENANCE.md`.

## Create a lab

1. Choose the next `NNN` directory and copy
   [`templates/lab-readme.md`](templates/lab-readme.md) to `NNN/README.md`.
2. Add Case Templates and the scoring profile under `NNN/evals/`. Populate relevant
   `case.tags` and, for cases evaluating a specific target,
   `case.payload.target_id` according to the Case Template contract.
3. Add Candidate instructions and, for model or hybrid scoring, Judge
   instructions under `NNN/tracecat/agent_presets/`.
4. Add the minimal `tracecat.json` manifest. Omit empty optional sections.
5. Add only target services to `compose.yml`. Implement deterministic scoring
   in a source-owned Tracecat workflow using `core.script.run_python`; add a
   target verifier service only when the scorer must exercise a live target.
6. Copy the closest existing `terraform/main.tf`, change `lab_id`, and retain
   only the credential variables the lab uses.
7. Add target variables to the root `.env.example`; do not create a per-lab
   environment file.
8. Add the lab to the root README table and run `just check`.

The root Justfile discovers directories matching `NNN/` automatically. Adding a
lab must not require another CLI command or a lab-specific Just recipe.

## Case Template contract

`evals/cases.ndjson` contains one complete evaluation unit per line:

```json
{"schema_version":1,"case_id":"stable-case-id","case":{"title":"Candidate-visible title","description":"Candidate-visible question","priority":"medium","severity":"medium","tags":[],"fields":{},"dropdowns":{},"payload":{}},"oracle":{"criteria":{"criterion-id":{"expected":"hidden expected result"}},"metrics":{}}}
```

Use `case.tags` as an array of strings for candidate-visible technologies,
behaviors, and CVEs where applicable. Use `case.payload.target_id` for a stable
target identifier: the full Vulhub directory such as `bash/CVE-2014-6271`, or a
built-in ID such as `n8n`. Do not infer CVEs from a product name or put hidden
Oracle answers in tags. For cases without target identity, omit `target_id`;
empty tags are valid. Labs 001 and 002 currently use empty tags and no target ID.

The shared grader extracts this metadata automatically from frozen submitted
Cases. Updating fixtures affects future runs, not frozen Trials. Run Evaluation
loads Case Templates with cursor pagination; 200 is a page size, not a suite
size limit.

Terraform stores the Case Template, Oracle, and resolved scoring profile in the
`case_templates` table. Run Evaluation creates the Trial Case from `case` only;
the Candidate preset has no table access. At the submission cutoff it freezes
the exact final answer, the Case and comments, and platform-captured Evidence.
Evidence events are ordered and joined by `tool_call_id`; calls store tool names
and inputs, results store returned content or errors, and visible assistant text
may be retained for trajectory scoring. None of this is private model reasoning.

The Oracle is generic structured reference data. Put expected criterion facts
under `oracle.criteria` and benchmark-specific aggregate references under
`oracle.metrics` when needed. Oracle fields need not mirror the Rubric exactly,
but the selected Scorer must validate all reference data it consumes. Keep
executable verifier fixtures in source-owned workflows or target services.

## Scoring profile contract

`evals/rubric.json` is the versioned scoring profile for the lab:

```json
{
  "schema_version": 2,
  "profile_id": "lab-NNN-purpose",
  "profile_version": 1,
  "scorer": {
    "kind": "deterministic",
    "workflow_alias": "score_trial"
  },
  "criteria": [
    {
      "criterion_id": "criterion-id",
      "label": "Human-readable criterion",
      "type": "numeric",
      "weight": 100,
      "hard_gate": false,
      "pass_threshold": 1,
      "range": {"min": 0, "max": 1},
      "judge_instruction": "What evidence makes this met or missed."
    }
  ],
  "metrics": [
    {"metric_id":"coverage","label":"Coverage","type":"numeric","source_criterion_id":"criterion-id"}
  ]
}
```

Criteria have type `binary` or `numeric`. Binary scorer values must be 0 or 1.
Numeric values must fall within their declared range and are normalized before
points are calculated. Non-gate weights total 100. Hard gates are binary, have
weight 0, and force the Trial score to 0 when their threshold is missed.

The scorer kind is `deterministic`, `model`, or `hybrid`. Model and hybrid
profiles name `scorer.judge_preset`; deterministic profiles do not need a Judge
preset. A scorer returns every criterion exactly once with `criterion_id`,
numeric `value`, `reason`, and `evidence_refs`, plus every declared independent
metric. Classification metrics declare `labels`; numeric, boolean, and text
metrics retain their native values. The reserved classification label
`__abstain__` represents a missing prediction.

Scorer workflows receive only frozen Trial data. Use `core.script.run_python`
for deterministic functions, passing Candidate content through `inputs`, never
by interpolating it into executable source. Use an LLM Judge only for semantic
assessment. Infrastructure failures and missing Oracle data are evaluation
errors, while invalid Candidate answers receive the benchmark-defined score.

## Tracecat manifest contract

The shared module creates the workspace tables, Run Evaluation, Run Candidate,
Judge Run, and their Trial subflows. Run Evaluation dispatches durable Trial
child workflows in bounded batches. Run Candidate invokes the lab's modifiable
Candidate workflow, which owns the prompt and invokes the Candidate preset.
Judge Run invokes the scorer workflow named by the frozen profile. The lab
manifest declares its Candidate preset, an optional Judge preset, scorer and
helper workflows, and optional integrations or secrets:

```json
{
  "schema_version": 1,
  "agent_presets": [
    {
      "name": "Lab NNN Candidate",
      "slug": "NNN-candidate",
      "description": "Candidate task.",
      "instructions_file": "agent_presets/candidate.md",
      "actions": ["core.cases.get_case", "core.cases.update_case"]
    },
    {
      "name": "Lab NNN Judge",
      "slug": "NNN-judge",
      "description": "Grades the captured Work Product.",
      "instructions_file": "agent_presets/judge.md",
      "actions": [],
      "retries": 1
    }
  ],
  "workflows": [
    {"alias":"score_trial","file":"workflows/score-trial.yml"}
  ]
}
```

Candidate presets must not have table access. Judge presets receive hidden
material only through their scorer workflow; grant them only the helper action
they require. A deterministic lab may omit the Judge preset entirely.

The generated `NNN Candidate — Modify` workflow is the supported UI experiment
surface. It accepts a native Case ID and must return `output`, `session_id`,
`duration`, and `message_history`. Commit edits before running an evaluation.
Candidate workflows may change prompts, skills, budgets, and agent orchestration,
but must not read evaluation tables or hidden Oracle data. This is a cooperative
rule because Tracecat does not provide a per-workflow action sandbox.

Optional credential mappings keep the generic `just plan` and `just apply`
wrappers free of lab-specific branches. Map Tracecat field names to root `.env`
variable names in the object that consumes them:

```json
{
  "mcp_integrations": [{
    "catalog_slug": "example-mcp",
    "credentials_from_env": {"Authorization": "EXAMPLE_MCP_AUTHORIZATION"}
  }],
  "secrets": [{
    "name": "lab_NNN_target",
    "keys_from_env": {"API_TOKEN": "LAB_NNN_API_TOKEN"}
  }]
}
```

Terraform receives these values through ephemeral, sensitive variables; the
manifest stores only environment variable names.

Managed workflow YAML defines the source-controlled baseline. Candidate
workflows are the exception intended for committed Tracecat canvas experiments;
the provider detects their committed drift. A later `just apply` lists affected
labs and requires explicit approval before restoring the baseline. Edit all
other managed workflows locally and apply Terraform.

## Results contract

The shared module writes `case_templates`, `evaluation_runs`, independent
`evaluation_trials` and `evaluation_evidence`, `scoring_attempts`,
`evaluation_scores`, and `evaluation_metrics`. The Run Evaluation execution ID
is the Evaluation Run ID. Trials are checkpointed as they complete, so a large
run can resume without repeating successful Candidate attempts. Judge Run
creates a new scoring attempt over those immutable Trials.

`just grade NNN RUN_ID=...` validates execution completion, result coverage,
score arithmetic, metric types, and hard gates before writing `scores.csv` and
`metrics.csv`. The criterion CSV schema is:

```text
schema_version,lab_id,evaluation_run_id,trial_id,case_id,trial_number,candidate_session_id,scoring_run_execution_id,scoring_attempt_id,judge_session_id,profile_id,profile_version,scorer_kind,scorer_workflow_alias,criterion_id,criterion_type,criterion_weight,criterion_value,criterion_points,criterion_hard_gate,criterion_passed,trial_hard_failed,trial_score,reason,evidence_refs,candidate_completed_at,scored_at
```

Do not add lab-specific score columns. Put benchmark measurements in declared
metric rows and detail in criterion IDs, reasons, and evidence references.

The same directory receives `summary.json` with a sorted, deduplicated `tags`
array from the frozen submitted Cases (including CVE tags where present),
`target_ids` from their `payload.target_id` values, and `target_id` when there
is exactly one distinct target. Target IDs preserve the full Vulhub directory
(e.g. `bash/CVE-2014-6271`) or built-in ID (`n8n`). Missing target metadata
produces `target_ids: []` and `target_id: null`; multiple distinct targets
produce the full sorted, deduplicated array and `target_id: null`. The report
also includes score distribution, binary pass rates, numeric criterion means,
hard-gate rates, workflow and agent timing, and declared aggregate metrics when
available. Tags are informational Case metadata, not grading evidence, and
are an empty array when unavailable. They are never read from current lab
fixtures when grading frozen runs. Classification reports include confusion
matrices, accuracy, balanced accuracy, macro and support-weighted F1, MCC,
per-class metrics, and joint exact match for multiple outputs. Numeric metrics
report count, mean, and standard error. Undefined values are null. The
standalone `just export` recipe remains available for raw criterion export but
does not perform grading integrity checks. The grader uses only the Go standard
library and owns its Terraform-output decoding, Tracecat HTTP access,
validation, and report generation.

## README contract

Every lab README follows [`templates/lab-readme.md`](templates/lab-readme.md)
with exactly these sections:

1. Task
2. Scoring
3. Target
4. Agent access
5. Run

Keep implementation details in configuration files. The README should explain
the evaluation contract, prerequisites, access boundaries, and commands.

## Validate and run

```bash
just check
just tracecat-up
just init
just up NNN
just plan
just apply
just run NNN
just status NNN RUN_ID=<candidate-run-id>
just judge NNN RUN_ID=<candidate-run-id>
just status NNN RUN_ID=<judge-run-id>
just grade NNN RUN_ID=<candidate-run-id>
```

A lab is ready when Terraform can plan it, Run Evaluation creates fresh Trial
Cases and returns immutable Trials, Judge Run scores every Trial, the CSV
exports with the shared schema, and `just check` passes. Inspect the generated
`summary.json`: verify that `tags` and `target_ids` match the submitted Cases,
that full Vulhub IDs are preserved, and that `target_id` is populated only for
one distinct target. Cases without metadata should produce empty arrays and
`target_id: null`. This report inspection is part of validating every new lab.
Repeated measurements are separate Evaluation Runs, not repetitions inside one run.
