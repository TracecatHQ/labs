set shell := ["bash", "-eu", "-o", "pipefail", "-c"]
set dotenv-load := true

root := justfile_directory()
provider_version := "0.1.0"

default:
    @just --list

# Securely initialize Tracecat, its Terraform service account, and a model provider.
setup:
    @bash "{{ root }}/scripts/setup.sh"

provider:
    #!/usr/bin/env bash
    set -euo pipefail
    os="$(go env GOOS)"
    arch="$(go env GOARCH)"
    destination="{{ root }}/.terraform.d/plugins/registry.terraform.io/tracecathq/tracecat/{{ provider_version }}/${os}_${arch}"
    mkdir -p "$destination"
    cd "{{ root }}/terraform-provider-tracecat"
    GOCACHE="{{ root }}/.cache/go-build" go build -o "$destination/terraform-provider-tracecat_v{{ provider_version }}" .

# Build the local provider and initialize the single workspace state.
init: provider
    @rm -f "{{ root }}/terraform/workspace/.terraform.lock.hcl"
    @terraform -chdir="{{ root }}/terraform/workspace" init -upgrade -plugin-dir="{{ root }}/.terraform.d/plugins"

# Cache and start the exact public Tracecat release, then enable API access.
tracecat-up:
    #!/usr/bin/env bash
    set -euo pipefail
    version="${TRACECAT_VERSION:?set TRACECAT_VERSION in .env}"
    checkout="{{ root }}/.cache/tracecat"
    if [[ ! -d "$checkout/.git" ]]; then
      mkdir -p "{{ root }}/.cache"
      git clone --depth 1 --branch "$version" https://github.com/TracecatHQ/tracecat.git "$checkout"
    fi
    test "$(git -C "$checkout" describe --tags --exact-match)" = "$version" || { echo "Cached Tracecat checkout does not match $version; remove .cache/tracecat to change versions." >&2; exit 2; }
    compose=(docker compose --project-directory "$checkout" --env-file "$checkout/.env.example" --env-file "{{ root }}/.env" -f "$checkout/docker-compose.yml")
    "${compose[@]}" up --wait --wait-timeout 180
    entitlements="$(
      "${compose[@]}" exec -T postgres_db psql --username postgres --dbname postgres --quiet --tuples-only --no-align --set ON_ERROR_STOP=1 <<'SQL'
    DO $seed$
    BEGIN
      IF (SELECT count(*) FROM tier WHERE is_default IS TRUE AND is_active IS TRUE) <> 1 THEN
        RAISE EXCEPTION 'expected exactly one active default tier';
      END IF;
      UPDATE tier
      SET entitlements = '{"service_accounts": true, "agent_addons": true}'::jsonb
      WHERE is_default IS TRUE AND is_active IS TRUE;
    END
    $seed$;
    SELECT entitlements::text
    FROM tier
    WHERE is_default IS TRUE AND is_active IS TRUE;
    SQL
    )"
    test "$entitlements" = '{"agent_addons": true, "service_accounts": true}' || { echo "Failed to seed the Labs entitlements" >&2; exit 2; }

tracecat-down:
    #!/usr/bin/env bash
    set -euo pipefail
    checkout="{{ root }}/.cache/tracecat"
    test -f "$checkout/docker-compose.yml" || exit 0
    docker compose --project-directory "$checkout" --env-file "$checkout/.env.example" --env-file "{{ root }}/.env" -f "$checkout/docker-compose.yml" down

# Start a lab's target services. Tracecat must already be running.
# Lab 003 accepts target=<Vulhub directory>; its default is n8n.
up lab target="":
    #!/usr/bin/env bash
    set -euo pipefail
    selected="{{ target }}"
    selected="${selected#target=}"
    if [[ "{{ lab }}" == "003" ]]; then
      bash "{{ root }}/scripts/lab-003-target.sh" up "${selected:-n8n}"
    else
      [[ -z "$selected" ]] || { echo "The target argument is supported only by Lab 003." >&2; exit 2; }
      compose=(docker compose --project-directory "{{ root }}/{{ lab }}" --env-file "{{ root }}/.env" -p "lab-{{ lab }}" -f "{{ root }}/{{ lab }}/compose.yml")
      if [[ -z "$("${compose[@]}" config --services)" ]]; then
        echo "Lab {{ lab }} has no target services."
        exit 0
      fi
      "${compose[@]}" up -d --wait --wait-timeout 900
    fi

down lab:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "{{ lab }}" == "003" ]]; then
      bash "{{ root }}/scripts/lab-003-target.sh" down
    else
      compose=(docker compose --project-directory "{{ root }}/{{ lab }}" --env-file "{{ root }}/.env" -p "lab-{{ lab }}" -f "{{ root }}/{{ lab }}/compose.yml")
      if [[ -z "$("${compose[@]}" config --services)" ]]; then
        echo "Lab {{ lab }} has no target services."
        exit 0
      fi
      "${compose[@]}" down
    fi

# List Lab 003 targets that include scored fixtures.
targets lab="003":
    @test "{{ lab }}" = "003" || { echo "Target catalogs are currently available only for Lab 003." >&2; exit 2; }
    @bash "{{ root }}/scripts/lab-003-target.sh" list

_terraform command AUTO_APPROVE="false":
    #!/usr/bin/env bash
    set -euo pipefail
    manifests=("{{ root }}"/[0-9][0-9][0-9]/tracecat/tracecat.json)
    mcp_credentials="$(jq -cs '
      reduce (.[] | .mcp_integrations[]? | select(.credentials_from_env)) as $integration ({};
        .[$integration.catalog_slug] = reduce ($integration.credentials_from_env | to_entries[]) as $credential ({};
          .[$credential.key] = (env[$credential.value] // error("set " + $credential.value))))
    ' "${manifests[@]}")"
    secret_values="$(jq -cs '
      reduce (.[] | .secrets[]? | select(.keys_from_env)) as $secret ({};
        .[$secret.name] = reduce ($secret.keys_from_env | to_entries[]) as $key ({};
          .[$key.key] = (env[$key.value] // error("set " + $key.value))))
    ' "${manifests[@]}")"
    candidate_model="$(bash "{{ root }}/scripts/resolve-model.sh" "$CANDIDATE_MODEL_PROVIDER" "$CANDIDATE_MODEL_NAME")"
    judge_model="$(bash "{{ root }}/scripts/resolve-model.sh" "$JUDGE_MODEL_PROVIDER" "$JUDGE_MODEL_NAME")"
    export TF_VAR_mcp_credentials="$mcp_credentials"
    export TF_VAR_secret_values="$secret_values"
    export TF_VAR_candidate_model="$candidate_model"
    export TF_VAR_judge_model="$judge_model"
    export TF_VAR_workspace_id="${TRACECAT_WORKSPACE_ID:?run just setup to select the Tracecat workspace}"
    if [[ "{{ command }}" != apply ]]; then
      terraform -chdir="{{ root }}/terraform/workspace" "{{ command }}"
      exit 0
    fi
    umask 077
    plan_file="$(mktemp "{{ root }}/terraform/workspace/.terraform/labs-apply.tfplan.XXXXXX")"
    trap 'rm -f "$plan_file"' EXIT
    set +e
    terraform -chdir="{{ root }}/terraform/workspace" plan -detailed-exitcode -out="$plan_file"
    plan_status=$?
    set -e
    if [[ "$plan_status" == 0 ]]; then
      echo "No Terraform changes to apply."
      exit 0
    fi
    [[ "$plan_status" == 2 ]] || exit "$plan_status"
    python3 "{{ root }}/scripts/candidate-reset-gate.py" "$plan_file"
    if [[ "{{ AUTO_APPROVE }}" != true ]]; then
      [[ -t 0 ]] || { echo "Noninteractive apply requires AUTO_APPROVE=true." >&2; exit 2; }
      read -r -p "Apply this Terraform plan? Only 'yes' will be accepted: " apply_answer
      [[ "$apply_answer" == yes ]] || { echo "Terraform apply cancelled." >&2; exit 2; }
    fi
    terraform -chdir="{{ root }}/terraform/workspace" apply "$plan_file"

plan:
    @just _terraform plan

apply AUTO_APPROVE="false":
    @just _terraform apply "{{ AUTO_APPROVE }}"

# Delete every Labs-managed resource while retaining the workspace and credentials.
reset-workspace CONFIRM_WORKSPACE_ID="":
    #!/usr/bin/env bash
    set -euo pipefail
    confirm="{{ CONFIRM_WORKSPACE_ID }}"
    confirm="${confirm#CONFIRM_WORKSPACE_ID=}"
    bash "{{ root }}/scripts/ensure-service-account-scopes.sh"
    python3 "{{ root }}/scripts/reset-workspace.py" --workspace-id "${TRACECAT_WORKSPACE_ID:?run just setup first}" --confirm "$confirm"

# Start Lab 001's Splunk MCP target and provision the complete workspace.
deploy: init
    @just up 001
    @bash "{{ root }}/scripts/bootstrap-splunk-mcp.sh"
    @bash "{{ root }}/scripts/ensure-service-account-scopes.sh"
    @just apply true

# Trigger or resume Run Evaluation asynchronously. Optional: CASE_IDS=a,b RUN_ID=id BATCH_SIZE=n.
run lab CASE_IDS="" RUN_ID="" BATCH_SIZE="4":
    #!/usr/bin/env bash
    set -euo pipefail
    workspace_id="$(terraform -chdir="{{ root }}/terraform/workspace" output -raw workspace_id)"
    workflow_id="$(terraform -chdir="{{ root }}/terraform/workspace" output -json lab_workflow_ids | jq -r --arg lab "{{ lab }}" '.[$lab][$lab + "_run_evaluation"]')"
    case_ids_value="{{ CASE_IDS }}"
    case_ids_value="${case_ids_value#CASE_IDS=}"
    run_id="{{ RUN_ID }}"
    run_id="${run_id#RUN_ID=}"
    batch_size="{{ BATCH_SIZE }}"
    batch_size="${batch_size#BATCH_SIZE=}"
    [[ "$batch_size" =~ ^[1-9][0-9]*$ ]] || { echo "BATCH_SIZE must be a positive integer." >&2; exit 2; }
    if [[ "{{ lab }}" == "003" ]]; then
      state="{{ root }}/.cache/lab-003/active-target.json"
      [[ -f "$state" ]] || { echo "Start a Lab 003 target with: just up 003 [target=<directory>]" >&2; exit 2; }
      active_case="$(jq -r '.case_id // ""' "$state")"
      scored="$(jq -r .scored "$state")"
      [[ "$scored" == true && -n "$active_case" ]] || { echo "The active target is exploratory and has no scored Case." >&2; exit 2; }
      if [[ -z "$case_ids_value" ]]; then
        case_ids_value="$active_case"
      elif [[ "$case_ids_value" != "$active_case" ]]; then
        echo "The active target supports only Case $active_case; got $case_ids_value." >&2
        exit 2
      fi
    fi
    case_ids="$(jq -cn --arg value "$case_ids_value" '$value | if length == 0 then [] else split(",") end')"
    payload="$(jq -cn --arg workflow_id "$workflow_id" --argjson case_ids "$case_ids" --arg run_id "$run_id" --argjson batch_size "$batch_size" '{workflow_id:$workflow_id,inputs:{case_ids:$case_ids,evaluation_run_id:$run_id,batch_size:$batch_size}}')"
    response="$(curl -fsS -H "Authorization: Bearer $TRACECAT_API_KEY" -H 'Content-Type: application/json' -d "$payload" "$TRACECAT_API_URL/workspaces/$workspace_id/workflow-executions")"
    if [[ -n "$run_id" ]]; then
      echo "$response" | jq --arg run_id "$run_id" '{evaluation_run_id:$run_id,resume_execution_id:.wf_exec_id}'
    else
      echo "$response" | jq '{evaluation_run_id:.wf_exec_id}'
    fi

# Trigger or resume Judge Run asynchronously for one Evaluation Run.
judge lab RUN_ID BATCH_SIZE="4":
    #!/usr/bin/env bash
    set -euo pipefail
    workspace_id="$(terraform -chdir="{{ root }}/terraform/workspace" output -raw workspace_id)"
    workflow_id="$(terraform -chdir="{{ root }}/terraform/workspace" output -json lab_workflow_ids | jq -r --arg lab "{{ lab }}" '.[$lab][$lab + "_judge"]')"
    run_id="{{ RUN_ID }}"
    run_id="${run_id#RUN_ID=}"
    batch_size="{{ BATCH_SIZE }}"
    batch_size="${batch_size#BATCH_SIZE=}"
    [[ "$batch_size" =~ ^[1-9][0-9]*$ ]] || { echo "BATCH_SIZE must be a positive integer." >&2; exit 2; }
    payload="$(jq -cn --arg workflow_id "$workflow_id" --arg run_id "$run_id" --argjson batch_size "$batch_size" '{workflow_id:$workflow_id,inputs:{evaluation_run_id:$run_id,batch_size:$batch_size}}')"
    response="$(curl -fsS -H "Authorization: Bearer $TRACECAT_API_KEY" -H 'Content-Type: application/json' -d "$payload" "$TRACECAT_API_URL/workspaces/$workspace_id/workflow-executions")"
    echo "$response" | jq --arg run_id "$run_id" '{evaluation_run_id:$run_id,judge_run_execution_id:.wf_exec_id}'

# Validate a completed Evaluation Run, export scores.csv, and write summary.json.
grade lab RUN_ID:
    #!/usr/bin/env bash
    set -euo pipefail
    run_id="{{ RUN_ID }}"
    run_id="${run_id#RUN_ID=}"
    cd "{{ root }}"
    GOCACHE="{{ root }}/.cache/go-build" go run ./cmd/labs-grade --root "{{ root }}" --lab "{{ lab }}" --run-id "$run_id"

status lab RUN_ID:
    #!/usr/bin/env bash
    set -euo pipefail
    workspace_id="$(terraform -chdir="{{ root }}/terraform/workspace" output -raw workspace_id)"
    run_id="{{ RUN_ID }}"
    run_id="${run_id#RUN_ID=}"
    workflow_id="${run_id%%/*}"
    execution_id="${run_id#*/}"
    test "$workflow_id" != "$execution_id" || { echo "RUN_ID must be a Tracecat workflow execution ID" >&2; exit 2; }
    curl -fsS -H "Authorization: Bearer $TRACECAT_API_KEY" "$TRACECAT_API_URL/workspaces/$workspace_id/workflows/$workflow_id/executions/$execution_id" | jq '{id,status,start_time,close_time}'

# Export raw criterion and metric rows without grading integrity checks.
export lab RUN_ID:
    #!/usr/bin/env bash
    set -euo pipefail
    workspace_id="$(terraform -chdir="{{ root }}/terraform/workspace" output -raw workspace_id)"
    table_ids="$(terraform -chdir="{{ root }}/terraform/workspace" output -json table_ids)"
    score_table_id="$(jq -r .evaluation_scores <<<"$table_ids")"
    metric_table_id="$(jq -r .evaluation_metrics <<<"$table_ids")"
    run_id="{{ RUN_ID }}"
    run_id="${run_id#RUN_ID=}"
    destination_dir="{{ root }}/{{ lab }}/results/$run_id"
    score_rows="$(mktemp)"
    metric_rows="$(mktemp)"
    score_csv="$(mktemp)"
    metric_csv="$(mktemp)"
    trap 'rm -f "$score_rows" "$metric_rows" "$score_csv" "$metric_csv"' EXIT
    fetch_rows() {
      local table_id="$1" output="$2" cursor="" response
      while true; do
        local query=(--get --data-urlencode "limit=200")
        if [[ -n "$cursor" ]]; then
          query+=(--data-urlencode "cursor=$cursor")
        fi
        response="$(curl -fsS "${query[@]}" -H "Authorization: Bearer $TRACECAT_API_KEY" "$TRACECAT_API_URL/workspaces/$workspace_id/tables/$table_id/rows")"
        jq -c '.items[]' <<<"$response" >> "$output"
        [[ "$(jq -r '.has_more' <<<"$response")" = true ]] || break
        cursor="$(jq -er '.next_cursor' <<<"$response")"
      done
    }
    fetch_rows "$score_table_id" "$score_rows"
    fetch_rows "$metric_table_id" "$metric_rows"
    if ! jq -se --arg run_id "$run_id" 'any(.evaluation_run_id == $run_id)' "$score_rows" >/dev/null; then
      echo "No completed Judge Run scores found for $run_id" >&2
      exit 2
    fi
    jq -sr --arg run_id "$run_id" '
      ["schema_version","lab_id","evaluation_run_id","trial_id","case_id","trial_number","candidate_session_id","scoring_run_execution_id","scoring_attempt_id","judge_session_id","profile_id","profile_version","scorer_kind","scorer_workflow_alias","criterion_id","criterion_type","criterion_weight","criterion_value","criterion_points","criterion_hard_gate","criterion_passed","trial_hard_failed","trial_score","reason","evidence_refs","candidate_completed_at","scored_at"],
      ((map(select(.evaluation_run_id == $run_id)) | sort_by(.case_id, .trial_number, .criterion_id))[] | [.schema_version,.lab_id,.evaluation_run_id,.trial_id,.case_id,.trial_number,.candidate_session_id,.scoring_run_execution_id,.scoring_attempt_id,.judge_session_id,.profile_id,.profile_version,.scorer_kind,.scorer_workflow_alias,.criterion_id,.criterion_type,.criterion_weight,.criterion_value,.criterion_points,.criterion_hard_gate,.criterion_passed,.trial_hard_failed,.trial_score,.reason,(.evidence_refs|tojson),.candidate_completed_at,.scored_at]) | @csv' "$score_rows" > "$score_csv"
    jq -sr --arg run_id "$run_id" '
      ["schema_version","lab_id","evaluation_run_id","trial_id","case_id","trial_number","scoring_run_execution_id","scoring_attempt_id","profile_id","profile_version","metric_id","metric_type","value","expected_value","scored_at"],
      ((map(select(.evaluation_run_id == $run_id)) | sort_by(.case_id, .trial_number, .metric_id))[] | [.schema_version,.lab_id,.evaluation_run_id,.trial_id,.case_id,.trial_number,.scoring_run_execution_id,.scoring_attempt_id,.profile_id,.profile_version,.metric_id,.metric_type,(.value|tojson),(.expected_value|tojson),.scored_at]) | @csv' "$metric_rows" > "$metric_csv"
    mkdir -p "$destination_dir"
    mv "$score_csv" "$destination_dir/scores.csv"
    mv "$metric_csv" "$destination_dir/metrics.csv"
    printf '%s\n' "$destination_dir/scores.csv" "$destination_dir/metrics.csv"

check:
    #!/usr/bin/env bash
    set -euo pipefail
    grep -Fxq 'TRACECAT_VERSION=1.0.0-rc.1' "{{ root }}/.env.example"
    grep -Fxq 'COMPOSE_PROJECT_NAME=tracecat-labs' "{{ root }}/.env.example"
    grep -Fxq 'TRACECAT_DOCKER_NETWORK=tracecat-labs_core' "{{ root }}/.env.example"
    ! grep -q '^TRACECAT__FEATURE_FLAGS=' "{{ root }}/.env.example"
    grep -Fxq 'TRACECAT__EE_MULTI_TENANT=true' "{{ root }}/.env.example"
    grep -Fxq 'TRACECAT__AUTH_SUPERADMIN_EMAIL=admin@tracecat.com' "{{ root }}/.env.example"
    bash -n "{{ root }}/scripts/setup.sh" "{{ root }}/scripts/resolve-model.sh"
    bash "{{ root }}/test/resolve-model.sh"
    PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s "{{ root }}/scripts/tests" -v
    found=0
    for lab_dir in "{{ root }}"/[0-9][0-9][0-9]; do
      [[ -f "$lab_dir/tracecat/tracecat.json" ]] || continue
      found=1
      lab="$(basename "$lab_dir")"
      jq -e --arg candidate "${lab}-candidate" --arg candidate_name "Lab ${lab} Candidate" '
        .schema_version == 1 and
        ([.agent_presets[].slug] | index($candidate)) != null and
        ([.agent_presets[] | select(.slug == $candidate and .name == $candidate_name)] | length) == 1
      ' "{{ root }}/$lab/tracecat/tracecat.json" >/dev/null
      jq -e . "{{ root }}/$lab/evals/rubric.json" >/dev/null
      jq -cs --arg lab "$lab" --slurpfile profile "{{ root }}/$lab/evals/rubric.json" --slurpfile manifest "{{ root }}/$lab/tracecat/tracecat.json" '
        (length > 0) and
        ($profile[0].schema_version == 2) and
        (($profile[0].profile_id | type) == "string" and ($profile[0].profile_id | length) > 0) and
        (($profile[0].profile_version | type) == "number") and
        (($profile[0].scorer.kind | IN("deterministic", "model", "hybrid"))) and
        (($profile[0].scorer.workflow_alias | type) == "string" and ($profile[0].scorer.workflow_alias | length) > 0) and
        (($profile[0].scorer.workflow_alias == "score_with_judge") or
         (([$manifest[0].workflows[]?.alias] | index($profile[0].scorer.workflow_alias)) != null)) and
        (all($manifest[0].workflows[]?; (.category | IN("agent_tool", "scorer", "judge_tool", "utility")))) and
        (([$manifest[0].agent_presets[] | select(.slug == ($lab + "-candidate"))] | length) == 1) and
        (if $profile[0].scorer.kind == "deterministic" then
          ($profile[0].scorer.judge_preset? == null) and
          ([$manifest[0].agent_presets[] | select(.slug | endswith("-judge"))] | length) == 0
        else
          (($profile[0].scorer.judge_preset | type) == "string") and
          ([$manifest[0].agent_presets[].slug] | index($profile[0].scorer.judge_preset)) != null and
          ([$manifest[0].agent_presets[] | select(.slug | endswith("-judge"))] | length) == 1
        end) and
        (($profile[0].criteria | length) > 0) and
        (($profile[0].criteria | type) == "array") and
        (($profile[0].metrics | type) == "array") and
        (($profile[0].criteria | map(.criterion_id) | length) == ($profile[0].criteria | map(.criterion_id) | unique | length)) and
        (($profile[0].metrics | map(.metric_id) | length) == ($profile[0].metrics | map(.metric_id) | unique | length)) and
        (map(.case_id) | length == (unique | length)) and
        (all(.[];
          .schema_version == 1 and
          (.case_id | type) == "string" and (.case_id | length) > 0 and
          (.case | type) == "object" and
          (.oracle | type) == "object" and
          (.oracle.criteria | type) == "object" and
          ((.oracle.metrics? == null) or ((.oracle.metrics | type) == "object"))
        )) and
        (all($profile[0].criteria[];
          (.criterion_id | type) == "string" and (.criterion_id | length) > 0 and
          (.label | type) == "string" and (.label | length) > 0 and
          (.judge_instruction | type) == "string" and (.judge_instruction | length) > 0 and
          (.type | IN("binary", "numeric")) and
          (.weight | type) == "number" and .weight >= 0 and
          (.hard_gate | type) == "boolean" and
          (.pass_threshold | type) == "number" and
          (if .type == "binary" then
            (.pass_threshold >= 0 and .pass_threshold <= 1 and ((.range? == null) or (.range == {"min":0,"max":1})))
          else
            ((.range | type) == "object" and (.range.min | type) == "number" and (.range.max | type) == "number" and
             .range.min < .range.max and .pass_threshold >= .range.min and .pass_threshold <= .range.max)
          end) and
          (if .hard_gate then .type == "binary" and .weight == 0 else true end)
        )) and
        (all($profile[0].metrics[]; . as $metric |
          (.metric_id | type) == "string" and (.metric_id | length) > 0 and
          (.label | type) == "string" and (.label | length) > 0 and
          (.type | IN("classification", "numeric", "boolean", "text")) and
          (($metric.source_criterion_id? == null) or ($profile[0].criteria | map(.criterion_id) | index($metric.source_criterion_id)) != null) and
          (if .type == "classification" then
            (.labels | type) == "array" and (.labels | length) >= 2 and
            (.labels | length) == (.labels | unique | length) and
            all(.labels[]; type == "string" and length > 0 and . != "__abstain__")
          else true end)
        )) and
        (($profile[0].criteria | map(select(.hard_gate == false) | .weight) | add) == 100)
      ' "{{ root }}/$lab/evals/cases.ndjson" >/dev/null
      for heading in "Task" "Scoring" "Target" "Agent access" "Run"; do
        grep -Fxq "## $heading" "{{ root }}/$lab/README.md"
      done
    done
    test "$found" = 1
    agent_slugs="$(jq -sr '[.[].agent_presets[].slug] | (length == (unique | length))' "{{ root }}"/[0-9][0-9][0-9]/tracecat/tracecat.json)"
    test "$agent_slugs" = true
    grep -Fq '"${var.lab_id}_run_evaluation"' "{{ root }}/terraform/modules/lab/main.tf"
    grep -Fq 'tags = ["modify"]' "{{ root }}/terraform/modules/lab/main.tf"
    grep -Fq 'title: __LAB_ID__ Run evaluation — Run first' "{{ root }}/terraform/modules/lab/workflows/lab-run-evaluation.yml"
    grep -Fq 'workflow_alias: run_evaluation' "{{ root }}/terraform/modules/lab/workflows/lab-run-evaluation.yml"
    grep -Fq 'title: __LAB_ID__ Candidate — Modify' "{{ root }}/terraform/modules/lab/workflows/lab-candidate.yml"
    grep -Fq 'action: ai.preset_agent' "{{ root }}/terraform/modules/lab/workflows/lab-candidate.yml"
    for field in output session_id duration message_history; do
      grep -Eq "^    $field:" "{{ root }}/terraform/modules/lab/workflows/lab-candidate.yml"
    done
    grep -Fq 'title: __LAB_ID__ Judge — Run second' "{{ root }}/terraform/modules/lab/workflows/lab-judge.yml"
    grep -Fq 'title: Run Candidate' "{{ root }}/terraform/modules/runtime/workflows/run-candidate.yml"
    grep -Fq 'action: core.workflow.execute' "{{ root }}/terraform/modules/runtime/workflows/run-candidate.yml"
    ! grep -Fq 'action: ai.preset_agent' "{{ root }}/terraform/modules/runtime/workflows/run-candidate.yml"
    grep -Fq 'action: ai.preset_agent' "{{ root }}/terraform/modules/runtime/workflows/score-with-judge.yml"
    for workflow in "{{ root }}"/terraform/modules/lab/workflows/*.yml "{{ root }}"/terraform/modules/runtime/workflows/*.yml "{{ root }}"/[0-9][0-9][0-9]/tracecat/workflows/*.yml; do
      [[ -f "$workflow" ]] || continue
      terraform -chdir="{{ root }}" console <<<"yamldecode(file(\"$workflow\"))" >/dev/null
    done
    terraform fmt -check -recursive "{{ root }}"
    cd "{{ root }}"
    GOCACHE="{{ root }}/.cache/go-build" go test ./...
    cd "{{ root }}/terraform-provider-tracecat"
    GOCACHE="{{ root }}/.cache/go-build" go test ./...
