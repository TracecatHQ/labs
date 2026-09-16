# Lab 003 — Vulnerability Firewall Mitigation

Turn an unauthenticated vulnerability report into a deployable ModSecurity
ruleset. n8n is the default target; seven pinned Vulhub environments provide
additional scored cases.

## Task

The Candidate writes exactly one clearly designated ruleset plus its rationale
on the Trial Case. Judge Run extracts that ruleset and invokes the Judge-only
`Validate Firewall Rule` workflow against the active target.

## Scoring

- Deployable, activatable, healthy, and cleaned-up ruleset: hard gate
- Five target-specific malicious request variants blocked: 10 points each
- Five target-specific benign requests preserved: 10 points each

A Candidate-caused syntax, activation, or outage failure misses the hard gate.
A target, target-selection, helper, or cleanup failure fails the evaluation
instead of creating a score. Evaluation rules use HTTP 418 for attributable
blocks because BunkerWeb may independently return HTTP 403; production rules
should use HTTP 403. An internal proxy confirms that allowed traffic reached
the target.

## Target

List the scored catalog with:

```bash
just targets 003
```

| `target` | Vulnerability |
|---|---|
| `n8n` | Supplier intake arbitrary file read (default) |
| `bash/CVE-2014-6271` | Shellshock header command injection |
| `httpd/CVE-2021-41773` | Apache path traversal and CGI RCE |
| `python/CVE-2024-23334` | aiohttp directory traversal |
| `httpd/CVE-2021-40438` | Apache mod_proxy SSRF |
| `langflow/CVE-2025-3248` | Langflow validate/code pre-auth RCE |
| `metabase/CVE-2023-38646` | Metabase pre-auth JDBC RCE |
| `cmsms/CVE-2019-9053` | CMS Made Simple unauthenticated SQL injection |

Vulhub is checked out at the commit recorded in
[`targets.json`](targets.json). Only one intentionally vulnerable target runs
at a time. It has no published port and is attached to an internal Docker
network; BunkerWeb is exposed only on `127.0.0.1`. Targets use their native
platform except legacy images that require `linux/amd64` emulation on ARM.

An uncatalogued Vulhub directory can be selected in exploration mode when its
Compose file exposes exactly one application service:

```bash
just up 003 target=<application>/<scenario>
```

The launcher removes published ports and rejects host networking, privileged
containers, added devices/capabilities, and bind mounts outside the selected
Vulhub directory. Exploration targets have no Case or score until fixtures are
reviewed and added to the catalog.

## Run

Run the default n8n case:

```bash
just tracecat-up
just init 003
just up 003
just plan 003
just apply 003
just run 003
just status 003 RUN_ID=<candidate-run-id>
just judge 003 RUN_ID=<candidate-run-id>
just status 003 RUN_ID=<judge-run-id>
just export 003 RUN_ID=<candidate-run-id>
```

Select another curated target before Candidate Run:

```bash
just up 003 target=langflow/CVE-2025-3248
just run 003
```

`just run 003` automatically selects the active target's Case. It rejects an
explicit `CASE_IDS` value that does not match that target. Switching targets
stops the previous target first; rerunning the same target is idempotent.

Use `just down 003` to stop the WAF, validator, and active vulnerable target.
Lab 003 is ephemeral: stopping or switching targets also removes its Docker
volumes so vulnerable application and candidate-rule state cannot leak between
cases.

## Agent access

The Candidate has Case actions only. The Judge receives the captured Submission
and may invoke the validation helper exactly once. Only the helper receives the
BunkerWeb secret and target-network access. The validator owns executable
fixtures and confirms that benign responses crossed the WAF and reached the
selected upstream.
