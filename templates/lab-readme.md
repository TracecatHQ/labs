# Lab NNN — Title

One sentence describing the Candidate's outcome.

## Task

Describe what the Candidate receives, what it must decide or produce, and where
the Work Product belongs on the Trial Case.

## Scoring

Describe the scoring profile, scorer workflow, binary or numeric criteria,
hard gates, independent metrics, and any model-graded portions. State what is
intentionally not scored and identify deliberate deviations from an upstream
benchmark.

## Target

Describe the target services, local datasets or assets, and required variables
in the root `.env`. Include any manual setup that must happen before
`just apply`. Document the Case tags and any `payload.target_id` values
that identify evaluated targets. Preserve full Vulhub directory IDs; omit
target identity where it does not apply.

## Agent access

List the Candidate's allowed actions or integrations, the scorer workflow's
actions, any Judge-agent access, and the important access boundaries. Explain
which evidence is captured by the platform and confirm that Oracle material is
not candidate-accessible.

## Run

Run from the repository root:

```bash
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

For large suites, set `BATCH_SIZE=<n>`. Resume a stopped Run Evaluation workflow with
`just run NNN RUN_ID=<candidate-run-id>`; rerunning Judge Run skips Trials that
already have complete criterion coverage.

`just grade` writes `scores.csv`, `metrics.csv`, and `summary.json` under
`NNN/results/<candidate-run-id>/`. Check that the summary's `tags`, `target_ids`,
and single-target `target_id` match the frozen submitted Cases. Empty metadata
is reported as empty arrays and `target_id: null`; multiple distinct targets
also yield `target_id: null`.
