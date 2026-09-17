#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_file="$root/.env"
env_example="$root/.env.example"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/tracecat-labs-setup.XXXXXX")"
cookie_jar="$tmp_dir/cookies"
response_file="$tmp_dir/response"
chmod 700 "$tmp_dir"
touch "$cookie_jar" "$response_file"
chmod 600 "$cookie_jar" "$response_file"

cleanup() {
  unset tracecat_password provider_secret encoded_password api_key
  rm -f "$cookie_jar" "$response_file" "$tmp_dir/legacy-workspaces"
  rmdir "$tmp_dir" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

die() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

for command_name in curl docker git jq just openssl terraform; do
  require_command "$command_name"
done

if [[ ! -f "$env_file" ]]; then
  cp "$env_example" "$env_file"
fi
chmod 600 "$env_file"

env_value() {
  local key="$1"
  awk -F= -v key="$key" '$1 == key {sub(/^[^=]*=/, ""); print; exit}' "$env_file"
}

set_env() {
  local key="$1" value="$2" line found=0
  local destination="$tmp_dir/env"
  [[ "$value" != *$'\n'* && "$value" != *$'\r'* ]] || die "invalid newline in $key"
  : > "$destination"
  chmod 600 "$destination"
  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" == "$key="* ]]; then
      printf '%s=%s\n' "$key" "$value" >> "$destination"
      found=1
    else
      printf '%s\n' "$line" >> "$destination"
    fi
  done < "$env_file"
  if [[ "$found" == 0 ]]; then
    printf '%s=%s\n' "$key" "$value" >> "$destination"
  fi
  mv "$destination" "$env_file"
  chmod 600 "$env_file"
}

# Preserve the Compose identity of existing checkouts. Fresh checkouts use the
# names in .env.example; legacy checkouts gain the matching network explicitly.
compose_project="$(env_value COMPOSE_PROJECT_NAME)"
if [[ -z "$compose_project" || "$compose_project" == replace-with-* ]]; then
  compose_project=tracecat-labs
  set_env COMPOSE_PROJECT_NAME "$compose_project"
fi
tracecat_network="$(env_value TRACECAT_DOCKER_NETWORK)"
if [[ -z "$tracecat_network" || "$tracecat_network" == replace-with-* ]]; then
  set_env TRACECAT_DOCKER_NETWORK "${compose_project}_core"
fi
unset compose_project tracecat_network

# Record old per-lab workspace resources before selecting a workspace. The
# workspace identity is retained, but resources must be rebuilt into the single
# workspace state. Setup does not delete resources or modify legacy states.
legacy_workspace_file="$tmp_dir/legacy-workspaces"
: > "$legacy_workspace_file"
for terraform_dir in "$root"/[0-9][0-9][0-9]/terraform; do
  [[ -d "$terraform_dir" ]] || continue
  state_json="$(terraform -chdir="$terraform_dir" state pull 2>/dev/null || true)"
  [[ -n "$state_json" ]] || continue
  legacy_workspace_id="$(jq -r '
    .resources[]?
    | select(.module == "module.lab" and .type == "tracecat_workspace" and .name == "lab")
    | .instances[0].attributes.id // empty
  ' <<<"$state_json")"
  if [[ -n "$legacy_workspace_id" ]]; then
    printf '%s\t%s\n' "$(basename "$(dirname "$terraform_dir")")" "$legacy_workspace_id" >> "$legacy_workspace_file"
  fi
done
unset state_json legacy_workspace_id terraform_dir

random_hex() {
  openssl rand -hex "$1"
}

ensure_generated() {
  local key="$1" value
  value="$(env_value "$key")"
  if [[ -z "$value" || "$value" == replace-with-* ]]; then
    set_env "$key" "$(random_hex 32)"
  fi
}

fernet_key="$(env_value TRACECAT__DB_ENCRYPTION_KEY)"
if [[ -z "$fernet_key" || "$fernet_key" == replace-with-* ]]; then
  fernet_key="$(openssl rand -base64 32 | tr '+/' '-_')"
  set_env TRACECAT__DB_ENCRYPTION_KEY "$fernet_key"
fi
unset fernet_key

for key in TRACECAT__SERVICE_KEY TRACECAT__SIGNING_SECRET USER_AUTH_SECRET \
  TRACECAT__POSTGRES_PASSWORD TEMPORAL__POSTGRES_PASSWORD MINIO_ROOT_PASSWORD \
  SPLUNK_ADMIN_PASSWORD BOTSV3_SECRET_KEY N8N_ENCRYPTION_KEY \
  BUNKERWEB_DB_PASSWORD; do
  ensure_generated "$key"
done

# BunkerWeb 1.6 requires its API password to contain upper- and lowercase
# letters, a number, and a special character. Rotate only generated or
# non-conforming local values; the API credential is re-provisioned by apply.
bunkerweb_api_token="$(env_value BUNKERWEB_API_TOKEN)"
if [[ -z "$bunkerweb_api_token" || "$bunkerweb_api_token" == replace-with-* ||
      ! "$bunkerweb_api_token" =~ [A-Z] || ! "$bunkerweb_api_token" =~ [a-z] ||
      ! "$bunkerweb_api_token" =~ [0-9] || ! "$bunkerweb_api_token" =~ [^a-zA-Z0-9] ]]; then
  set_env BUNKERWEB_API_TOKEN "Aa1!$(random_hex 28)"
fi
unset bunkerweb_api_token

postgres_password="$(env_value TRACECAT__POSTGRES_PASSWORD)"
set_env TRACECAT__DB_URI "postgresql+psycopg://postgres:${postgres_password}@postgres_db:5432/postgres"
unset postgres_password

splunk_authorization="$(env_value SPLUNK_MCP_AUTHORIZATION)"
if [[ -z "$splunk_authorization" || "$splunk_authorization" == *replace-with-* ]]; then
  set_env SPLUNK_MCP_AUTHORIZATION "\"Bearer $(random_hex 32)\""
fi

default_email="$(env_value TRACECAT__AUTH_SUPERADMIN_EMAIL)"
default_email="${default_email:-admin@tracecat.com}"
read -r -p "Tracecat admin email [$default_email]: " tracecat_email
tracecat_email="${tracecat_email:-$default_email}"
[[ "$tracecat_email" == *@* ]] || die "enter a valid email address"
set_env TRACECAT__AUTH_SUPERADMIN_EMAIL "$tracecat_email"

api_url="$(env_value TRACECAT_API_URL)"
case "$api_url" in
  https://*|http://127.0.0.1:*|http://localhost:*) ;;
  *) die "TRACECAT_API_URL must use HTTPS unless it points to loopback" ;;
esac

printf '\nStarting Tracecat and enabling API access...\n'
just --justfile "$root/Justfile" tracecat-up

read_secret_twice() {
  local prompt="$1" first second
  while true; do
    IFS= read -r -s -p "$prompt: " first </dev/tty
    printf '\n'
    IFS= read -r -s -p "Confirm $prompt: " second </dev/tty
    printf '\n'
    if [[ "$first" == "$second" && ${#first} -ge 12 ]]; then
      printf -v "$2" '%s' "$first"
      return
    fi
    printf 'Values must match and contain at least 12 characters. Try again.\n' >&2
  done
}

read_secret_twice "Tracecat admin password" tracecat_password

login() {
  local encoded_email status
  encoded_email="$(printf '%s' "$tracecat_email" | jq -sRr @uri)"
  encoded_password="$(printf '%s' "$tracecat_password" | jq -sRr @uri)"
  status="$({ printf 'username=%s&password=%s' "$encoded_email" "$encoded_password"; } |
    curl -sS -o "$response_file" -w '%{http_code}' -c "$cookie_jar" -b "$cookie_jar" \
      -H 'Content-Type: application/x-www-form-urlencoded' --data-binary @- \
      "$api_url/auth/login")"
  unset encoded_password
  [[ "$status" == 204 || "$status" == 200 ]]
}

if ! login; then
  printf 'No matching local user session; registering the configured admin...\n'
  status="$(printf '%s' "$tracecat_password" |
    jq -Rsc --arg email "$tracecat_email" '{email:$email,password:.}' |
    curl -sS -o "$response_file" -w '%{http_code}' -H 'Content-Type: application/json' \
      --data-binary @- "$api_url/auth/register")"
  if [[ "$status" != 201 ]]; then
    detail="$(jq -r '.detail // "registration failed"' "$response_file" 2>/dev/null || printf 'registration failed')"
    die "$detail (HTTP $status)"
  fi
  login || die "registered the user but could not establish a session"
fi

api_get() {
  local path="$1"
  if [[ -n "$org_id" ]]; then
    curl -fsS -o "$response_file" -c "$cookie_jar" -b "$cookie_jar" \
      --cookie "tracecat:active-org-id=$org_id" "$api_url$path"
  else
    curl -fsS -o "$response_file" -c "$cookie_jar" -b "$cookie_jar" \
      "$api_url$path"
  fi
}

api_json() {
  local method="$1" path="$2"
  if [[ -n "$org_id" ]]; then
    curl -fsS -o "$response_file" -c "$cookie_jar" -b "$cookie_jar" \
      --cookie "tracecat:active-org-id=$org_id" \
      -X "$method" -H 'Content-Type: application/json' --data-binary @- "$api_url$path"
  else
    curl -fsS -o "$response_file" -c "$cookie_jar" -b "$cookie_jar" \
      -X "$method" -H 'Content-Type: application/json' --data-binary @- "$api_url$path"
  fi
}

legacy_workspace_count="$(cut -f2 "$legacy_workspace_file" | sort -u | awk 'NF {count++} END {print count+0}')"
if [[ "$legacy_workspace_count" -gt 1 ]]; then
  legacy_workspaces="$(awk -F '\t' '{print $1 "=" $2}' "$legacy_workspace_file" | paste -sd ', ' -)"
  die "multiple legacy lab workspaces found ($legacy_workspaces); migrate one lab deployment at a time"
fi
legacy_workspace_id="$(cut -f2 "$legacy_workspace_file" | head -n 1)"

org_id=""
api_get /organization/memberships
organization_memberships="$(<"$response_file")"

if [[ -n "$legacy_workspace_id" ]]; then
  legacy_org_id=""
  while IFS= read -r membership_org_id; do
    org_id="$membership_org_id"
    api_get /workspaces
    if jq -e --arg id "$legacy_workspace_id" 'any(.[]; .id == $id)' "$response_file" >/dev/null; then
      legacy_org_id="$org_id"
      break
    fi
  done < <(jq -r '.[].id' <<<"$organization_memberships")
  org_id="$legacy_org_id"
  [[ -n "$org_id" ]] || die "legacy lab workspace $legacy_workspace_id was not found in the signed-in user's organizations"
  unset legacy_org_id membership_org_id
else
  org_id="$(jq -r 'map(select(.name == "Tracecat Labs"))[0].id // empty' <<<"$organization_memberships")"
fi
unset organization_memberships

if [[ -z "$org_id" ]]; then
  api_get /admin/organizations
  org_id="$(jq -r 'map(select(.slug == "tracecat-labs"))[0].id // empty' "$response_file")"
  if [[ -z "$org_id" ]]; then
    printf '%s' '{"name":"Tracecat Labs","slug":"tracecat-labs"}' | api_json POST /admin/organizations
    org_id="$(jq -er .id "$response_file")"
  fi

  api_get /organization/invitations/pending/me
  invitation_token="$(jq -r --arg org_id "$org_id" 'map(select(.organization_id == $org_id))[0].token // empty' "$response_file")"
  if [[ -z "$invitation_token" ]]; then
    jq -nc --arg email "$tracecat_email" '{email:$email,role_slug:"organization-owner"}' |
      api_json POST "/admin/organizations/$org_id/invitations"
    invitation_token="$(jq -er .token "$response_file")"
  fi
  jq -nc --arg token "$invitation_token" '{token:$token}' |
    api_json POST /organization/invitations/accept
  unset invitation_token
fi

# Tracecat requires every organization to retain a workspace. Reuse that
# deployment-owned workspace so the labs do not need multi_workspace access.
api_get /workspaces
configured_workspace_id="$(env_value TRACECAT_WORKSPACE_ID)"
if [[ -n "$configured_workspace_id" && -n "$legacy_workspace_id" && "$configured_workspace_id" != "$legacy_workspace_id" ]]; then
  die "TRACECAT_WORKSPACE_ID does not match the retained legacy lab workspace $legacy_workspace_id"
fi
if [[ -n "$legacy_workspace_id" ]]; then
  workspace_id="$(jq -r --arg id "$legacy_workspace_id" 'map(select(.id == $id))[0].id // empty' "$response_file")"
  [[ -n "$workspace_id" ]] || die "legacy lab workspace $legacy_workspace_id is not available in Tracecat"
elif [[ -n "$configured_workspace_id" ]]; then
  workspace_id="$(jq -r --arg id "$configured_workspace_id" 'map(select(.id == $id))[0].id // empty' "$response_file")"
else
  workspace_id=""
fi
if [[ -z "$workspace_id" ]]; then
  workspace_count="$(jq 'length' "$response_file")"
  [[ "$workspace_count" == 1 ]] ||
    die "expected exactly one Tracecat workspace; set TRACECAT_WORKSPACE_ID in .env to select one"
  workspace_id="$(jq -er '.[0].id' "$response_file")"
fi
set_env TRACECAT_WORKSPACE_ID "$workspace_id"
unset configured_workspace_id legacy_workspace_count legacy_workspace_id legacy_workspaces workspace_count workspace_id

required_scopes='[
  "org:read", "org:workspace:read", "org:secret:read",
  "workspace:read",
  "workflow:read", "workflow:create", "workflow:update", "workflow:delete", "workflow:execute",
  "integration:read", "integration:create", "integration:update", "integration:delete",
  "table:read", "table:create", "table:update", "table:delete",
  "agent:read", "agent:create", "agent:update", "agent:delete",
  "case:read", "case:delete",
  "tag:read", "tag:create", "tag:update", "tag:delete",
  "secret:read", "secret:create", "secret:update", "secret:delete",
  "action:core.*:execute", "action:ai.preset_agent:execute"
]'
api_get /organization/service-accounts/scopes
missing_scopes="$(jq -r --argjson required "$required_scopes" '([.items[].name] as $available | $required - $available) | join(", ")' "$response_file")"
[[ -z "$missing_scopes" ]] || die "Tracecat did not offer required service-account scopes: $missing_scopes"
scope_ids="$(jq -c --argjson required "$required_scopes" '[.items[] | select(.name as $name | $required | index($name)) | .id]' "$response_file")"

api_get '/organization/service-accounts?limit=100'
service_account_id="$(jq -r '.items | map(select(.name == "tracecat-labs-terraform"))[0].id // empty' "$response_file")"
if [[ -z "$service_account_id" ]]; then
  jq -nc --argjson scope_ids "$scope_ids" \
    '{name:"tracecat-labs-terraform",description:"Terraform automation for Tracecat Labs",scope_ids:$scope_ids,initial_key_name:"Primary"}' |
    api_json POST /organization/service-accounts
  service_account_id="$(jq -er .service_account.id "$response_file")"
  api_key="$(jq -er .issued_api_key.raw_key "$response_file")"
else
  jq -nc --argjson scope_ids "$scope_ids" '{scope_ids:$scope_ids}' |
    api_json PATCH "/organization/service-accounts/$service_account_id"
  existing_key="$(env_value TRACECAT_API_KEY)"
  if [[ -n "$existing_key" && "$existing_key" != replace-with-* ]] &&
    printf 'Authorization: Bearer %s\n' "$existing_key" |
      curl -fsS -o /dev/null -H @- "$api_url/organization"; then
    api_key="$existing_key"
  else
    printf '%s' '{"name":"Setup rotation"}' |
      api_json POST "/organization/service-accounts/$service_account_id/api-keys"
    api_key="$(jq -er .issued_api_key.raw_key "$response_file")"
  fi
  unset existing_key
fi
set_env TRACECAT_API_KEY "$api_key"
unset api_key scope_ids required_scopes

printf '\nModel provider:\n'
printf '  1) OpenAI\n  2) Anthropic\n  3) Gemini\n  4) Ollama\n  5) vLLM\n'
while true; do
  read -r -p 'Select [1-5]: ' provider_choice
  case "$provider_choice" in
    1) provider=openai; secret_name=OPENAI_API_KEY; url_name=OPENAI_BASE_URL; break ;;
    2) provider=anthropic; secret_name=ANTHROPIC_API_KEY; url_name=ANTHROPIC_BASE_URL; break ;;
    3) provider=gemini; secret_name=GEMINI_API_KEY; url_name=; break ;;
    4) provider=ollama; secret_name=OLLAMA_API_KEY; url_name=OLLAMA_BASE_URL; break ;;
    5) provider=vllm; secret_name=VLLM_API_KEY; url_name=VLLM_BASE_URL; break ;;
  esac
done

provider_url=""
if [[ -n "$url_name" ]]; then
  default_url=""
  [[ "$provider" == ollama ]] && default_url=http://host.docker.internal:11434
  if [[ -n "$default_url" ]]; then
    read -r -p "$url_name [$default_url]: " provider_url
    provider_url="${provider_url:-$default_url}"
  else
    read -r -p "$url_name (blank for provider default): " provider_url
  fi
fi
[[ "$provider" != vllm || -n "$provider_url" ]] || die "VLLM_BASE_URL is required"

secret_requirement="required"
[[ "$provider" == ollama || "$provider" == vllm ]] && secret_requirement="optional"
IFS= read -r -s -p "$secret_name ($secret_requirement; input hidden): " provider_secret </dev/tty
printf '\n'
[[ "$secret_requirement" == optional || -n "$provider_secret" ]] || die "$secret_name is required"

printf '%s' "$provider_secret" |
  jq -Rsc --arg provider "$provider" --arg secret_name "$secret_name" \
    --arg url_name "$url_name" --arg provider_url "$provider_url" '
      {provider:$provider,credentials:({} +
        (if length > 0 then {($secret_name):.} else {} end) +
        (if $provider_url != "" then {($url_name):$provider_url} else {} end))}' |
  api_json POST /agent/credentials
unset provider_secret

current_provider="$(env_value CANDIDATE_MODEL_PROVIDER)"
current_candidate=""
if [[ "$current_provider" == "$provider" ]]; then
  current_candidate="$(env_value CANDIDATE_MODEL_NAME)"
fi
if [[ -n "$current_candidate" ]]; then
  read -r -p "Candidate model ID [$current_candidate]: " candidate_model
  candidate_model="${candidate_model:-$current_candidate}"
else
  read -r -p "Candidate model ID: " candidate_model
fi
current_judge=""
if [[ "$(env_value JUDGE_MODEL_PROVIDER)" == "$provider" ]]; then
  current_judge="$(env_value JUDGE_MODEL_NAME)"
fi
judge_default="${current_judge:-$candidate_model}"
read -r -p "Judge model ID [$judge_default]: " judge_model
judge_model="${judge_model:-$judge_default}"
[[ -n "$candidate_model" && -n "$judge_model" ]] || die "model IDs cannot be empty"
set_env CANDIDATE_MODEL_PROVIDER "$provider"
set_env CANDIDATE_MODEL_NAME "$candidate_model"
set_env JUDGE_MODEL_PROVIDER "$provider"
set_env JUDGE_MODEL_NAME "$judge_model"

unset tracecat_password
printf '\nSetup complete. Terraform API access is in %s (mode 0600); the model credential exists only in Tracecat.\n' "$env_file"
if [[ -s "$legacy_workspace_file" ]]; then
  printf '%s\n' 'Legacy per-lab Terraform state detected. Resource-preserving migration to the central state is not supported.'
  printf '%s\n' 'Back up any Labs data you need before rebuilding. Reset deletes Labs cases, runs, and resources; it preserves the workspace and model credentials.'
  printf '%s\n' 'Do not plan, apply, or destroy the retired per-lab Terraform roots.'
  printf 'Rebuild explicitly: just reset-workspace CONFIRM_WORKSPACE_ID=%s && just deploy\n' "$(env_value TRACECAT_WORKSPACE_ID)"
fi
