# Lab 006 — CTI-REALM Sigma-50 derivative

Turn each of 50 cyber-threat detection objectives into an executable Sigma rule.
This derivative uses the complete CTI-REALM-50 objective suite: 25 Linux, 17 AKS,
and 8 cloud objectives. It keeps the upstream CTI, MITRE, data-source, and Sigma
quality checkpoints while replacing KQL as the submitted artifact. Minimal and
seeded variants and upstream Candidate agents are excluded.

## Task

Use threat intelligence and real telemetry to produce one Sigma rule, identify
its data source, execute the rule, and return the events it matches.

## Target

The Candidate has bounded workflows for CTI reports, pinned MITRE ATT&CK, Sigma
references, telemetry source discovery, and Sigma execution. `execute_sigma_rule`
returns a bounded list of matched events. `scratch_analysis` runs fixed Python
utilities with network disabled. Telemetry and reference files are available only
on the Tracecat Docker network; no service publishes a host port.

## Agent access

The final output has exactly three keys: `sigma_rule`, `data_source`, and
`matched_events`. Sigma is judged directly without adaptation. Submitted events
qualify for outcome scoring only when every submitted field and value is an
exact projection of an event in a recorded successful `execute_sigma_rule`
result for the exact submitted rule and data source. C3 requires two distinct,
successful executions with nonempty matches,
so iterative rule development is observable.

## Scoring

The scorer runs in Tracecat. The shared Judge scores C0 CTI alignment and Sigma
syntax/specificity. Python gates C0 on recorded report retrieval, retains the
upstream C1/C2 matched-set Jaccard, records C3 iterative execution, verifies the
submitted event provenance, and computes C4 field-slot precision, recall, and F1
from the Oracle regex patterns over those matched events. The weights remain
0.125, 0.075, 0.100, 0.050, 0.500, 0.0375, and 0.1125. The two Sigma quality
weights preserve the 25/75 syntax/specificity split. The profile has a derivative
ID because its executable Sigma and matched-event contract differs from the
upstream KQL answer contract.

## Run

Cases and their structured Oracles are committed and work offline. Runtime
telemetry, CTI reports, Sigma index, and MITRE bundles are intentionally kept
out of Git:

```bash
./006/scripts/acquire_data.sh
just tracecat-up
just init
just up 006
just plan
just apply
just run 006
```

## Data provenance

The acquisition script downloads roughly 1.8 GiB of pinned JSONL telemetry for
12 sources and writes a local `runtime/SHA256SUMS` inventory. The Sigma runner
mounts those fixtures read-only, streams events without building a database,
and caps every response. Source discovery reads file metadata only.

The first image build fetches and compiles the pinned official Sigma Engine.
The resulting service is a small native container that builds and runs on both
`linux/arm64` and `linux/amd64`. It validates each submitted rule with Sigma
Engine, flattens scalar event fields for matching, and returns the complete
matching JSON events.

Sources are pinned as follows:

- `UKGovernmentBEIS/inspect_evals` commit
  `2999eaadad35edebada59bac4a687793c11d1c3c`, MIT license copied to
  `licenses/inspect-evals-MIT.txt`. The cases and structured Oracles are derived
  from this revision.
- Hugging Face dataset `arjun180-new/cti_realm` revision
  `0fa6744b0e1d8b5b65cad0d5356a8e968bd22b02`. Its pinned repository metadata
  has no license field or license file, so large runtime artifacts are fetched
  directly into the ignored local cache and are not redistributed here.
- MITRE CTI commit `68d2992ea01249fc72e966f569a0d0b663d98581`;
  all three bundle SHA-256 values are verified by the acquisition script.
- SigmaHQ `sigma_engine` commit
  `0b4d232c8c0560c87727bb91e49fade3aaa3374f` (MIT; license text copied to
  `licenses/sigma-engine-MIT.txt`).
- Nginx 1.27.4 Alpine reference-service image digest
  `sha256:4ff102c5d78d254a6f0da062b3cf39eaf07f01eec0927fd21e219d0af8bc0591`.

## Validation

```bash
python3 006/scripts/import_cases.py dataset_answers_stratified_50.jsonl dataset_samples_stratified_50.jsonl
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s 006/tests -v
python3 -c 'import json; r=[json.loads(x) for x in open("006/evals/cases.ndjson")]; assert len(r)==50 and len({x["case_id"] for x in r})==50'
docker build --platform linux/arm64 -f 006/target/sigma_runner/Dockerfile 006/target
```
