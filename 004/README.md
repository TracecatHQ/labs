# Lab 004 — Simbian Cyber Defense Benchmark Public Sample

Hunt one public Windows-event sample and submit the exact timestamps of
malicious events.

## Task

The Candidate receives Simbian's generic threat-intelligence briefing and a
scoped SQLite query workflow over 155,350 events. It investigates the `logs`
table and returns one JSON object containing a `timestamps` array on the Trial
Case. This lab imports only the bundled public sample; the separately
distributed full benchmark is not included.

## Scoring

The deterministic `score_threat_hunt` workflow runs `core.script.run_python`
and ports upstream timestamp normalization and hierarchical coverage. It first
scores covered narrative steps within each attack-chain step, averages those
scores for every MITRE ATT&CK tactic, and averages the available tactic scores
for the 0–100 numeric criterion. Independent metrics report overall and
per-tactic coverage plus flags detected, flags total, and flag percent.

Extra timestamps have no penalty, matching upstream behavior. Invalid
timestamps do not match flags. The scorer does not use a model Judge.

## Target

`compose.yml` builds a read-only HTTP facade around a persistent SQLite import
of a logs-only derivative of the bundled `sample.zip`. The service is attached only to the shared
Tracecat Docker network and publishes no host port. SQL writes, attachment,
and pragmas are denied, responses are capped at 10 rows, and the full hidden
flag document is never copied into the target image.

The imported archive, schema, license, and provenance are under `assets/`.
They are pinned to Simbian's
`simbianai/cyber_defense_benchmark` commit
`e8b86d01ccefe338d455e61505ca285943635b27` (MIT). The single Case is tagged
`simbian`, `threat-hunting`, and `mitre-attack` and uses target ID
`simbian-public-sample`.

## Agent access

The Candidate has Case actions and `core.workflow.execute`; its only data path
is `Query Threat Logs`, which calls the internal target. The deterministic
scorer receives the frozen final answer and hidden Oracle through Judge Run.
The Oracle contains flags, attack chains, and tactic assignments and is never
candidate-accessible. Platform-captured workflow calls remain in the frozen
Trial evidence.

## Run

Run from the repository root:

```bash
just tracecat-up
just init
just up 004
just plan
just apply
just run 004
just status 004 RUN_ID=<candidate-run-id>
just judge 004 RUN_ID=<candidate-run-id>
just status 004 RUN_ID=<judge-run-id>
just grade 004 RUN_ID=<candidate-run-id>
```

`just grade` writes `scores.csv`, `metrics.csv`, and `summary.json` under
`004/results/<candidate-run-id>/`.
