#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture_dir="$(mktemp -d)"
trap 'rm -rf "$fixture_dir"' EXIT
cat > "$fixture_dir/curl" <<'MOCK'
#!/usr/bin/env bash
case " $* " in
  *' cursor=next '*) cat "$MODEL_FIXTURE/second.json" ;;
  *) cat "$MODEL_FIXTURE/first.json" ;;
esac
MOCK
chmod +x "$fixture_dir/curl"
export PATH="$fixture_dir:$PATH" MODEL_FIXTURE="$fixture_dir"
export TRACECAT_API_URL=http://example.invalid TRACECAT_API_KEY=test
resolve() { bash "$root/scripts/resolve-model.sh" openai test-model; }
cat > "$fixture_dir/first.json" <<'JSON'
{"items":[{"id":"custom","model_provider":"openai","model_name":"test-model","custom_provider_id":"custom-provider"}],"next_cursor":"next"}
JSON
cat > "$fixture_dir/second.json" <<'JSON'
{"items":[{"id":"builtin","model_provider":"openai","model_name":"test-model","custom_provider_id":null}],"next_cursor":null}
JSON
[[ "$(resolve | jq -r .catalog_id)" == builtin ]]
printf '%s\n' '{"items":[],"next_cursor":null}' > "$fixture_dir/first.json"
if resolve >/dev/null 2>&1; then echo 'Missing model accepted' >&2; exit 1; fi
printf '%s\n' '{"items":[{"id":"a","model_provider":"openai","model_name":"test-model"},{"id":"b","model_provider":"openai","model_name":"test-model"}],"next_cursor":null}' > "$fixture_dir/first.json"
if resolve >/dev/null 2>&1; then echo 'Ambiguous model accepted' >&2; exit 1; fi
printf 'Model catalog resolver tests passed.\n'
