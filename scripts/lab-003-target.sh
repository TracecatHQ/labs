#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
catalog="$root/003/targets.json"
cache="$root/.cache/lab-003"
vulhub="$root/.cache/vulhub"
runtime_env="$cache/runtime.env"
state_file="$cache/active-target.json"
network="lab-003_application"
vulhub_url="https://github.com/vulhub/vulhub.git"

usage() {
  cat <<'EOF'
Usage: scripts/lab-003-target.sh up [target]
       scripts/lab-003-target.sh down
       scripts/lab-003-target.sh list

The default target is n8n. A target may be any Vulhub directory containing a
docker-compose.yml; the eight catalogued targets additionally support scoring.
EOF
}

base_compose() {
  docker compose \
    --project-directory "$root/003" \
    --env-file "$root/.env" \
    --env-file "$runtime_env" \
    -p lab-003 \
    -f "$root/003/compose.yml" "$@"
}

ensure_vulhub() {
  local commit cloned=false
  commit="$(jq -r .vulhub_commit "$catalog")"
  mkdir -p "$root/.cache"
  if [[ ! -d "$vulhub/.git" ]]; then
    git clone --filter=blob:none --no-checkout "$vulhub_url" "$vulhub"
    cloned=true
  fi
  if [[ "$cloned" == false && -n "$(git -C "$vulhub" status --porcelain)" ]]; then
    echo "The cached Vulhub checkout has local changes: $vulhub" >&2
    exit 2
  fi
  if ! git -C "$vulhub" cat-file -e "$commit^{commit}" 2>/dev/null; then
    git -C "$vulhub" fetch --depth 1 origin "$commit"
  fi
  git -C "$vulhub" checkout --quiet --detach "$commit"
}

target_override_path() {
  printf '%s/target-override.yml\n' "$cache"
}

target_compose() {
  local target="$1"
  shift
  local target_dir="$vulhub/$target"
  docker compose \
    --project-directory "$target_dir" \
    -p lab-003-target \
    -f "$target_dir/docker-compose.yml" \
    -f "$(target_override_path)" "$@"
}

write_runtime_env() {
  local target="$1" port="$2" ready_path="$3"
  mkdir -p "$cache"
  umask 077
  {
    printf 'LAB_003_TARGET_ID=%s\n' "$target"
    printf 'LAB_003_TARGET_URL=http://target:%s\n' "$port"
    printf 'LAB_003_READY_PATH=%s\n' "$ready_path"
  } > "$runtime_env.tmp"
  mv "$runtime_env.tmp" "$runtime_env"
}

write_target_override() {
  local compose_json="$1" main_service="$2" platform="$3"
  local override
  override="$(target_override_path)"
  mkdir -p "$cache"
  {
    echo "services:"
    while IFS= read -r service; do
      printf '  %s:\n' "$service"
      if [[ -n "$platform" ]]; then
        printf '    platform: %s\n' "$platform"
      fi
      if jq -e --arg service "$service" '.services[$service].ports | length > 0' <<<"$compose_json" >/dev/null; then
        echo "    ports: !reset []"
      fi
      if [[ "$service" == "$main_service" ]]; then
        echo "    networks:"
        echo "      default:"
        echo "        aliases: [target]"
      fi
    done < <(jq -r '.services | keys[]' <<<"$compose_json")
    echo "networks:"
    echo "  default:"
    echo "    name: $network"
    echo "    external: true"
  } > "$override.tmp"
  mv "$override.tmp" "$override"
}

preflight_target() {
  local compose_json="$1" target_dir="$2"
  if jq -e '
    [.services[] | select(
      .privileged == true or
      .network_mode == "host" or .pid == "host" or .ipc == "host" or
      ((.devices // []) | length > 0) or
      ((.cap_add // []) | length > 0)
    )] | length > 0
  ' <<<"$compose_json" >/dev/null; then
    echo "Refusing a Vulhub target that requests privileged host access." >&2
    exit 2
  fi
  if jq -e --arg root "$target_dir/" '
    [.services[].volumes[]? |
      select(.type == "bind") |
      select((.source + "/" | startswith($root)) | not)
    ] | length > 0
  ' <<<"$compose_json" >/dev/null; then
    echo "Refusing a Vulhub target with a bind mount outside its target directory." >&2
    exit 2
  fi
}

down_stack() {
  local target=""
  if [[ -f "$state_file" ]]; then
    target="$(jq -r .target "$state_file")"
  fi
  if [[ -f "$runtime_env" ]]; then
    base_compose --profile n8n down --volumes --remove-orphans || true
  fi
  if [[ -n "$target" && "$target" != "n8n" && -f "$vulhub/$target/docker-compose.yml" && -f "$(target_override_path)" ]]; then
    target_compose "$target" down --volumes --remove-orphans || true
  fi
  docker network rm "$network" >/dev/null 2>&1 || true
  rm -f "$state_file" "$runtime_env" "$(target_override_path)"
}

write_state() {
  local target="$1" case_id="$2" scored="$3"
  jq -n \
    --arg target "$target" \
    --arg case_id "$case_id" \
    --argjson scored "$scored" \
    '{target:$target,case_id:$case_id,scored:$scored}' > "$state_file.tmp"
  mv "$state_file.tmp" "$state_file"
}

up_stack() {
  local requested="${1:-n8n}"
  local target="${requested#target=}"
  local record service port ready_path platform case_id scored target_dir compose_json
  [[ -n "$target" ]] || target="n8n"

  record="$(jq -c --arg target "$target" '.targets[] | select(.id == $target)' "$catalog")"
  scored=false
  case_id=""
  platform=""
  if [[ -n "$record" ]]; then
    scored=true
    service="$(jq -r .service <<<"$record")"
    port="$(jq -r .port <<<"$record")"
    ready_path="$(jq -r .ready_path <<<"$record")"
    platform="$(jq -r '.platform // ""' <<<"$record")"
    case_id="$(jq -r .case_id <<<"$record")"
  fi

  if [[ -f "$state_file" ]]; then
    local active
    active="$(jq -r .target "$state_file")"
    if [[ "$active" != "$target" ]]; then
      echo "Stopping active Lab 003 target $active..."
      down_stack
    fi
  fi

  mkdir -p "$cache"
  docker network inspect "$network" >/dev/null 2>&1 || docker network create --internal "$network" >/dev/null

  if [[ "$target" == "n8n" ]]; then
    write_runtime_env "$target" "$port" "$ready_path"
    write_state "$target" "$case_id" "$scored"
    trap 'down_stack; exit 130' INT TERM
    if ! base_compose --profile n8n up -d n8n-init n8n; then
      down_stack
      return 1
    fi
  else
    [[ "$target" != /* && "$target" != *".."* ]] || { echo "Invalid Vulhub target path: $target" >&2; exit 2; }
    ensure_vulhub
    target_dir="$vulhub/$target"
    [[ -f "$target_dir/docker-compose.yml" ]] || { echo "No Vulhub docker-compose.yml at $target" >&2; exit 2; }
    compose_json="$(docker compose --project-directory "$target_dir" -f "$target_dir/docker-compose.yml" config --format json)"
    preflight_target "$compose_json" "$target_dir"

    if [[ "$scored" != true ]]; then
      exposed=()
      while IFS= read -r exposed_service; do
        exposed+=("$exposed_service")
      done < <(jq -r '.services | to_entries[] | select(((.value.ports // []) | length > 0) or ((.value.expose // []) | length > 0)) | .key' <<<"$compose_json")
      [[ "${#exposed[@]}" == 1 ]] || { echo "Uncatalogued target must expose exactly one service; found ${#exposed[@]}." >&2; exit 2; }
      service="${exposed[0]}"
      port="$(jq -r --arg service "$service" '.services[$service] | ((.ports // [])[0].target // (.expose // [])[0])' <<<"$compose_json")"
      [[ "$port" =~ ^[0-9]+$ ]] || { echo "Could not derive a single target port for $target." >&2; exit 2; }
      ready_path="/"
    fi

    write_runtime_env "$target" "$port" "$ready_path"
    write_target_override "$compose_json" "$service" "$platform"
    write_state "$target" "$case_id" "$scored"
    trap 'down_stack; exit 130' INT TERM
    if ! target_compose "$target" up -d --build; then
      down_stack
      return 1
    fi
  fi

  if ! base_compose up -d --build --wait --wait-timeout 300 validator bw-db bunkerweb bw-scheduler bw-api; then
    down_stack
    return 1
  fi
  trap - INT TERM
  if [[ "$scored" == true ]]; then
    echo "Lab 003 is ready: $target (case $case_id)"
  else
    echo "Lab 003 is ready in unscored exploration mode: $target"
  fi
}

list_targets() {
  jq -r '.targets[] | [.id, .case_id, .title] | @tsv' "$catalog" | column -t -s $'\t'
}

case "${1:-}" in
  up) up_stack "${2:-n8n}" ;;
  down) down_stack ;;
  list) list_targets ;;
  *) usage; exit 2 ;;
esac
