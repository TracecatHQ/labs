#!/usr/bin/env bash
# Provision the restricted Lab 001 Splunk identity and mint its encrypted MCP token.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_file="${1:-$root/.env}"
compose=(
  docker compose
  --project-directory "$root/001"
  --env-file "$env_file"
  -p lab-001
  -f "$root/001/compose.yml"
)

"${compose[@]}" ps --status running --services | grep -qx splunk || {
  echo "Lab 001 Splunk is not running; start it with: just up 001" >&2
  exit 2
}

token_response="$(${compose[@]} exec -T splunk bash -s <<'SPLUNK'
set -euo pipefail
base_url="http://127.0.0.1:8089"
candidate_role="lab_001_candidate"
candidate_user="lab_001_candidate"
candidate_password="$(/opt/splunk/bin/python3 -c 'import secrets; print(secrets.token_hex(32))')"

splunk_api() {
  curl -fsS -u "admin:$SPLUNK_PASSWORD" "$@"
}

configure_role() {
  local endpoint="$1"
  shift
  splunk_api -o /dev/null -X POST "$endpoint" "$@" \
    --data-urlencode "capabilities=search" \
    --data-urlencode "capabilities=get_metadata" \
    --data-urlencode "capabilities=rest_properties_get" \
    --data-urlencode "capabilities=mcp_tool_execute" \
    --data-urlencode "srchIndexesAllowed=investigation" \
    --data-urlencode "srchIndexesDefault=investigation"
}

if splunk_api -o /dev/null "$base_url/services/authorization/roles/$candidate_role?output_mode=json" 2>/dev/null; then
  configure_role "$base_url/services/authorization/roles/$candidate_role"
else
  configure_role "$base_url/services/authorization/roles" --data-urlencode "name=$candidate_role"
fi

if splunk_api -o /dev/null "$base_url/services/authentication/users/$candidate_user?output_mode=json" 2>/dev/null; then
  splunk_api -o /dev/null -X POST "$base_url/services/authentication/users/$candidate_user" \
    --data-urlencode "roles=$candidate_role"
else
  splunk_api -o /dev/null -X POST "$base_url/services/authentication/users" \
    --data-urlencode "name=$candidate_user" \
    --data-urlencode "password=$candidate_password" \
    --data-urlencode "roles=$candidate_role"
fi

splunk_api "$base_url/services/mcp_token?username=$candidate_user&output_mode=json"
SPLUNK
)"

token="$(jq -er '.token | select(type == "string" and length > 0)' <<<"$token_response")"
authorization="Bearer $token"

python3 - "$env_file" "$authorization" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
value = sys.argv[2]
line = f"SPLUNK_MCP_AUTHORIZATION={value!r}\n"
lines = path.read_text().splitlines(keepends=True) if path.exists() else []
for index, existing in enumerate(lines):
    if existing.startswith("SPLUNK_MCP_AUTHORIZATION="):
        lines[index] = line
        break
else:
    lines.append(line)
path.write_text("".join(lines))
PY

echo "Provisioned the restricted Lab 001 Splunk MCP identity and refreshed its encrypted token."
