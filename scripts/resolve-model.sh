#!/usr/bin/env bash
set -euo pipefail

provider="${1:?model provider required}"
model="${2:?model name required}"
rows="[]"
cursor=""
while true; do
  query=(--get --data-urlencode "provider=$provider" --data-urlencode "model_name=$model" --data-urlencode 'limit=100')
  [[ -z "$cursor" ]] || query+=(--data-urlencode "cursor=$cursor")
  response="$(curl -fsS "${query[@]}" -H "Authorization: Bearer ${TRACECAT_API_KEY:?}" "${TRACECAT_API_URL:?}/organization/agent-catalog")"
  rows="$(jq -cn --argjson prior "$rows" --argjson page "$response" '$prior + $page.items')"
  next="$(jq -r '.next_cursor // empty' <<<"$response")"
  [[ -n "$next" ]] || break
  [[ "$next" != "$cursor" ]] || { echo 'Invalid model catalog pagination cursor' >&2; exit 2; }
  cursor="$next"
done
jq -ce --arg provider "$provider" --arg model "$model" '
  map(select(.model_provider == $provider and .model_name == $model and .custom_provider_id == null)) |
  if length == 1 then {provider:$provider,name:$model,catalog_id:.[0].id}
  else error("Expected one built-in catalog entry for " + $provider + "/" + $model + "; found " + (length|tostring)) end
' <<<"$rows"
