#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
checkout="$root/.cache/tracecat"
workspace_id="${TRACECAT_WORKSPACE_ID:?run just setup first}"
test -f "$checkout/docker-compose.yml" || { echo "Tracecat checkout is missing; run just tracecat-up" >&2; exit 2; }

compose=(docker compose --project-directory "$checkout" --env-file "$checkout/.env.example" --env-file "$root/.env" -f "$checkout/docker-compose.yml")
granted="$(
  "${compose[@]}" exec -T postgres_db psql --username postgres --dbname postgres --quiet --tuples-only --no-align --set ON_ERROR_STOP=1 --set workspace_id="$workspace_id" <<'SQL'
INSERT INTO service_account_scope (service_account_id, scope_id)
SELECT sa.id, scope.id
FROM service_account AS sa
JOIN workspace AS ws ON ws.organization_id = sa.organization_id
CROSS JOIN scope
WHERE sa.name = 'tracecat-labs-terraform'
  AND ws.id = :'workspace_id'::uuid
  AND scope.name IN (
    'case:read', 'case:delete',
    'tag:read', 'tag:create', 'tag:update', 'tag:delete'
  )
ON CONFLICT DO NOTHING;

SELECT count(*)
FROM service_account_scope AS link
JOIN service_account AS sa ON sa.id = link.service_account_id
JOIN workspace AS ws ON ws.organization_id = sa.organization_id
JOIN scope ON scope.id = link.scope_id
WHERE sa.name = 'tracecat-labs-terraform'
  AND ws.id = :'workspace_id'::uuid
  AND scope.name IN (
    'case:read', 'case:delete',
    'tag:read', 'tag:create', 'tag:update', 'tag:delete'
  );
SQL
)"
test "$granted" = 6 || { echo "Failed to grant the Labs lifecycle scopes" >&2; exit 1; }
