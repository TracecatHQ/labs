# Tracecat labs

Security-agent evaluations provisioned as Tracecat configuration. Terraform
owns lab resources in Tracecat's deployment workspace, Docker Compose owns only
scenario targets, and `just`
provides thin lifecycle and REST wrappers. The repository has no lab CLI,
Python control runtime, or copied Tracecat source tree.

Each evaluation follows the same path:

```text
Case Template → Candidate Run → Trial Case + durable Evaluation Run → Judge Run → scores.csv
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

Requirements: Terraform 1.11+, Go, Docker, `just`, `jq`, `curl`, `openssl`, Git
LFS, and Ruby.

Run the interactive setup. It creates a mode-`0600` `.env`, generates local
infrastructure secrets, starts Tracecat, provisions a least-privilege Terraform
service account, selects the deployment workspace, and sends one model-provider
credential directly to Tracecat:

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
just export 001 RUN_ID=<candidate-run-id>
```

Use one applied lab workspace per Tracecat deployment. Tracecat 1.0.0-rc.1
requires the `multi_workspace` entitlement to create another workspace, and the
labs intentionally do not enable it.

Both triggers return a native Tracecat workflow execution ID and run
asynchronously. Wait for Candidate Run to complete before starting Judge Run,
then wait for Judge Run before exporting. Completed Candidate Runs are retained
in the Tracecat `evaluation_runs` table for later judging. Results are written
to `NNN/results/<candidate-run-id>/scores.csv`.

## Labs

| Lab | Candidate task | Score |
|---|---|---|
| [001](001/) | Investigate one EventBridge alert and write an evidence-backed incident timeline | True-positive hard gate plus 16 weighted findings |
| [002](002/) | Classify 20 BOTSv3 alerts from bounded evidence objects | Exact-evidence hard gate; determination 50; incident relevance 50 |
| [003](003/) | Mitigate one of eight unauthenticated vulnerability targets with a deployable ModSecurity ruleset | Deployability hard gate; five malicious and five benign fixtures per target |

## Adding a lab

See [CONTRIBUTING.md](CONTRIBUTING.md) for naming, required files, evaluation
contracts, the README template, and the validation checklist.
