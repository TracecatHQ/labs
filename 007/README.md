# Lab 007 — SEvenLLM English Multiple Choice

Answer the 50 English multiple-choice items in the public SEvenLLM test split.

## Task

Each Candidate Trial receives one cybersecurity context, one question, and
four ordered choices labeled A through D. The Work Product is exactly one
option letter on the Trial Case. This import includes only English MCQ records;
it excludes Chinese records and every generative QA task.

## Scoring

The deterministic `score_mcq_answer` workflow runs
`core.script.run_python`. It trims surrounding whitespace, normalizes case,
and accepts only a single A–D letter. A valid letter equal to the hidden Oracle
answer earns the one binary criterion worth 100 points; any invalid, ambiguous,
or wrong response earns zero. There is no model Judge.

The `accuracy` metric is 1 or 0 per Trial. The shared report aggregates it as
accuracy with sample count and standard error across the 50 Cases.

## Target

No target service is needed. The 50 Cases derive from test records 1201–1250
at Hugging Face dataset revision
`1de23ce55cadc984d3f3a7b52c4035a68c6cd5b0`. The source dataset is published
by Multilingual-Multimodal-NLP under Apache-2.0. Provenance and a reproducible
import script live under `assets/` and `scripts/`.

Candidate-visible Cases contain only the category, context, question, and
ordered choices. Source `thought` fields and reference answers are discarded
from the visible content. Correct letters exist only in each hidden Oracle.

## Agent access

The Candidate can only read its Case. The deterministic scorer receives the
frozen final response and hidden Oracle through Judge Run. It has no network
or target access. Oracle answers and source thoughts are never
candidate-accessible.

## Run

Run from the repository root:

```bash
just tracecat-up
just init
just up 007
just plan
just apply
just run 007
just status 007 RUN_ID=<candidate-run-id>
just judge 007 RUN_ID=<candidate-run-id>
just status 007 RUN_ID=<judge-run-id>
just grade 007 RUN_ID=<candidate-run-id>
```

`just grade` writes `scores.csv`, `metrics.csv`, and `summary.json` under
`007/results/<candidate-run-id>/`.
