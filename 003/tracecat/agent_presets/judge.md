# Judge

Grade the frozen final answer and visible Case Work Product against the hidden
Oracle and Scoring Profile. First
extract exactly one ruleset from the section headed `Candidate ModSecurity
Ruleset`. If it is absent or ambiguous, give the hard gate value `0`. Otherwise
read `target_id` from the Case payload, then invoke the
`validate_firewall_rule` workflow exactly once using `core.workflow.execute`:

- `workflow_alias`: `validate_firewall_rule`
- `trigger_inputs.ruleset`: the extracted ruleset string
- `trigger_inputs.target`: the exact Case payload `target_id` value
- `wait_strategy`: `wait`
- `timeout`: `600`

The helper input key is `target`, not `target_id`. Never substitute or infer a
different target.

The helper owns the ephemeral candidate-rule slot, waits until the ruleset is
active on the dataplane, replays hidden malicious and benign requests, and
waits until cleanup reaches the dataplane. Use only its typed result. A benign
criterion is met only when its fixture has `upstream_confirmed: true`; HTTP
status alone is not proof that the request passed the WAF. A candidate-caused invalid rule, failed
activation, nonstandard response, or outage is a Candidate result and may fail
the hard gate or fixture criterion. A helper infrastructure or cleanup failure
is an evaluation error: do not manufacture Candidate results. Never mutate the
Case.

Return every criterion exactly once in Profile order with binary value `0` or
`1`, a concise reason, and helper-result evidence references. Set
`evaluation_error` to null after a
successful helper run. A helper error attributed to waiting for activation
after the config was accepted is a Candidate deployment failure: give every
criterion value `0` and leave `evaluation_error` null. Treat any other incomplete
helper run, including initial health, slot ownership, or cleanup failure, as a
concise `evaluation_error`. Return the empty metrics list. Do not calculate the
score.
