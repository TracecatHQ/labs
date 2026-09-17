# Lab 005 — SecRL / ExCyTIn-Bench

Investigate 589 security questions across eight isolated incident databases.
This imports exactly the corrected SecRL `o1/v1/test` split: incident 5 (98),
34 (82), 38 (11), 39 (98), 55 (100), 134 (57), 166 (87), and 322 (56).

## Task

Answer the Case's threat-investigation question from the matching incident's
read-only telemetry and leave a visible solution with query evidence.

## Target

The Candidate passes the active Case ID to `query_incident_sql`; the workflow
derives its incident server-side and queries only that database. It accepts one
read-only statement, rejects mutation keywords and multiple statements,
enforces a bounded terminal limit with a streaming cursor, returns at most 200
rows, and uses a MySQL account granted only `SELECT` and `SHOW VIEW`. All eight
MySQL services have no host-published ports.

## Agent access

The Candidate records a designated `final_answer`, a visible ordered
`solution`, and SQL `evidence`. Scoring first uses the shared Judge workflow for
semantic correctness and an explicit reflection pass. If that answer is not
confirmed, the Judge grades and reflects on each visible solution step. Python
then reproduces the upstream reverse-order discount (0.4), skipping the
final-answer step. `binary_success` and numeric `reward` are reported as
separate metrics.

## Scoring

The reflected semantic answer is reported as a zero-weight binary criterion;
the 0..1 discounted reward receives all 100 Trial points. This preserves the
upstream reward while reporting binary benchmark success separately.

## Run

Cases work offline from committed `evals/cases.ndjson`. The anonymized database
archive is 1.64 GB and expands substantially, so it is intentionally excluded:

```bash
./005/scripts/acquire_data.sh
just tracecat-up
just init
just up 005
just plan
just apply
just run 005
```

## Data provenance

Acquisition is pinned to Hugging Face dataset
`anandmudgerikar/excytin-bench` revision
`8bc9ce1f97f8a6880c815dfba76d88651ddb761a`. The archive size is
1,643,989,896 bytes and SHA-256 is
`61ecd48685e4eb34cac226a0651fd6443174f6efaa628aea3b06aef2cb088720`.
The script verifies both before extraction and creates deterministic MySQL init
files. The dataset declares CDLA-Permissive-2.0; its standard text is copied
to `licenses/CDLA-Permissive-2.0.txt`. The MySQL 9.0 target image is pinned to
manifest digest `sha256:92dc869678019f65d761155dacac660a904f6245bfe1b7997da0a73b2bfc68c9`.

Questions come from `microsoft/SecRL` commit
`a1b234f7a846133f08b7b5e1614c5c63b96f3b5c`, directory
`secgym/questions/o1/v1/test`, under the MIT license copied to
`licenses/SecRL-MIT.txt`. Candidate agents, training questions, older o1 data,
and o3/Opus variants are excluded.

## Validation

```bash
python3 005/scripts/import_cases.py /path/to/SecRL
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s 005/tests -v
python3 -c 'import json; r=[json.loads(x) for x in open("005/evals/cases.ndjson")]; assert len(r)==589 and len({x["case_id"] for x in r})==589'
```

Database acquisition is the only material external-data prerequisite. A full
Compose startup was not used for offline fixture validation because the archive
is deliberately not stored in Git.
