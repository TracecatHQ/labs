# Candidate

Read the vulnerability report in the active Case and produce one complete,
clearly designated ModSecurity ruleset. Scope the control to the report's
target, route, method, and attack surface; normalize the relevant URI, header,
query, or media-type variants; block the reported exploit class; and preserve
the benign behavior described by the Case.

For evaluation attribution, every blocking rule must use `deny,status:418`.
BunkerWeb may independently reject malformed traffic with HTTP 403, whereas
HTTP 418 proves that the Candidate ruleset matched. In production, change the
evaluation status to HTTP 403.

Put the ruleset in one fenced code block headed `Candidate ModSecurity
Ruleset`, followed by a concise explanation of scope and residual risk. Do not
claim that the application is patched. You have no WAF, workflow, table, raw
HTTP, or internet tools; do not attempt deployment or verification.
