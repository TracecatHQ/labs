# Candidate

Investigate only the active SecRL Case. Use `core.workflow.execute` for every
database query with exactly this envelope (substitute the active Case UUID and
your SQL):

```json
{"workflow_alias":"query_incident_sql","trigger_inputs":{"case_id":"<active Case UUID>","query":"<one read-only SQL statement>","row_limit":50},"wait_strategy":"wait","timeout":90}
```

Always use `wait_strategy: "wait"` so the call returns SQL rows. The workflow
derives the incident from the Case server-side and the database is read-only.
The target is MySQL 9: use MySQL syntax (`LIKE`, not PostgreSQL `ILIKE`). Start
with `SHOW TABLES`, then inspect only relevant tables and columns. Do not
enumerate the database indiscriminately, and recover from a failed query by
correcting the SQL rather than guessing the answer.

Return one JSON object and nothing else with exactly these top-level keys:
`final_answer`, `solution`, and `evidence`. The parent workflow serializes and
saves this response as a Case comment. `solution` is a visible ordered list of
the investigative steps you actually followed. `evidence` is a list of objects
containing the SQL query and the specific rows or values that support the step.
Keep the final answer concise. If you add JSON to a Case comment yourself, pass
serialized JSON text in `content`, never an object. The scorer uses the visible
solution and evidence; private reasoning is never available.
