# Candidate

Develop one detection for the active CTI-REALM objective. Invoke every helper
through `core.workflow.execute` using its exact alias, `wait_strategy: "wait"`,
and `timeout: 90`. The event workflows are `list_event_sources`,
`get_event_source_schema`, `sample_event_source`, `validate_sigma_rule`, and
`execute_sigma_rule`. For example, execute a rule with this envelope:

```json
{"workflow_alias":"execute_sigma_rule","trigger_inputs":{"data_source":"DeviceProcessEvents","sigma_rule":"<Sigma YAML>","limit":10},"wait_strategy":"wait","timeout":90}
```

Use the CTI report, MITRE, Sigma reference, event-source schema/sample, Sigma
validation/execution, and isolated scratch workflows as needed. Show your
analysis in Case comments: the scorer uses visible analysis and recorded tool
evidence because frozen Trials do not expose private reasoning.

Finish with exactly one JSON object containing all and only these top-level
keys: `sigma_rule`, `data_source`, and `matched_events`. `sigma_rule` is Sigma
YAML as a JSON string and is judged directly. `data_source` must be an exact
source name returned by `list_event_sources`. Validate the rule, then execute
the exact submitted rule and source through `execute_sigma_rule`; copy only
events from that recorded result's `matched_events` field into your final
`matched_events`. You may omit irrelevant fields, but every submitted field and
value must be copied exactly from one returned event; do not invent or alter
values. Run at least two distinct successful Sigma executions
returning nonempty matches so iterative rule
development is observable. If you add structured content to a Case comment
yourself, serialize it to text first.

Before finishing, confirm the recorded evidence contains: a tagged CTI report
search with results; the ATT&CK technique in a visible comment; schema and sample
calls for the chosen source; one successful draft execution; and one successful
execution of the exact final rule and source whose returned event values you use.
