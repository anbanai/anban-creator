#!/usr/bin/env bash

POLL_DEADLINE_SECONDS="${ANBAN_RUNTIME_SMOKE_POLL_DEADLINE_SECONDS:-900}"
POLL_INTERVAL_SECONDS="${ANBAN_RUNTIME_SMOKE_POLL_INTERVAL_SECONDS:-2}"
CLEANUP_MAX_ATTEMPTS="${ANBAN_RUNTIME_SMOKE_CLEANUP_MAX_ATTEMPTS:-5}"
CLEANUP_INTERVAL_SECONDS="${ANBAN_RUNTIME_SMOKE_CLEANUP_INTERVAL_SECONDS:-1}"

fail() {
  printf 'runtime smoke FAILED: %s\n' "$*" >&2
  return 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command is unavailable: $1"
}

compose() {
  docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" "$@"
}

api_post() {
  local path="$1" body="$2"
  if [[ -n "${TOKEN:-}" ]]; then
    curl -fsS -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d "$body" "$SERVER_URL$path"
  else
    curl -fsS -H 'Content-Type: application/json' -d "$body" "$SERVER_URL$path"
  fi
}

api_get() {
  curl -fsS -H "Authorization: Bearer $TOKEN" "$SERVER_URL$1"
}

extract_project_id() {
  jq -er 'if .code == 0 and (.data.project.id | type) == "string" and (.data.project.id | length) > 0 then .data.project.id else error("project response is missing data.project.id") end'
}

create_project() {
  local profile="$1" marker="$2" response project_id
  response="$(api_post /api/v1/projects "$(jq -cn --arg profile "$profile" --arg marker "$marker" '{platform:$profile,name:("Runtime smoke " + $profile),instructions:("Managed runtime smoke only. Do not perform the normal content workflow. Use the Bash tool to write the exact marker " + $marker + " to /workspace/output/runtime-smoke.txt, call submit_agent_feedback once, then stop successfully. On a resume request append its exact RESUMED marker to the same file and stop successfully."),enable_publishing:false}')")" || return
  project_id="$(extract_project_id <<<"$response")" || {
    fail "project create response did not match the public API schema"
    return 1
  }
  PROJECT_IDS="${PROJECT_IDS:-} $project_id"
  CREATED_PROJECT_ID="$project_id"
}

create_task() {
  local profile="$1" project_id="$2" marker="$3" response task_id body
  if [[ "$profile" == "montage" ]]; then
    body="$(jq -cn --arg project "$project_id" --arg marker "$marker" '{project_id:$project,prompt:("Execute only the managed runtime smoke marker: " + $marker),quantity:1,execution_profile:"effective",montage_input:{brief:("Runtime smoke marker " + $marker),pipeline_key:"cinematic",preferences:{duration_seconds:5}}}')"
  else
    body="$(jq -cn --arg project "$project_id" --arg marker "$marker" '{project_id:$project,prompt:("Execute only the managed runtime smoke marker: " + $marker),quantity:1,execution_profile:"effective",has_content_image:false,has_tail_image:false,article_with_cover:false,article_with_content_images:false}')"
  fi
  response="$(api_post /api/v1/tasks "$body")" || return
  task_id="$(jq -er 'if .code == 0 and (.data.id | type) == "string" and (.data.id | length) > 0 then .data.id else error("task response is missing data.id") end' <<<"$response")" || {
    fail "task create response did not match the public API schema"
    return 1
  }
  TASK_IDS="${TASK_IDS:-} $task_id"
  CREATED_TASK_ID="$task_id"
}

query_execution() {
  local task_id="$1"
  if [[ ! "$task_id" =~ ^[[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12}$ ]]; then
    fail "task ID is not a canonical UUID"
    return 1
  fi
  compose exec -T -e MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql \
    mysql -uroot --batch --skip-column-names anban_creator \
    -e "SELECT CONCAT_WS('|',id,attempt,COALESCE(parent_execution_id,''),runtime_profile,runtime_image,runtime_scope,runtime_workload,COALESCE(runtime_instance_id,''),status) FROM task_executions WHERE task_id='$task_id' ORDER BY attempt DESC LIMIT 1"
}

# Returns the resolved image ID only when one inspect snapshot proves that the
# expected persisted instance is running and carries all ownership labels.
inspect_runtime_container() {
  local row="$1" task_id="$2" project_id="$3" workload instance execution_id inspected
  execution_id="$(cut -d'|' -f1 <<<"$row")"
  workload="$(cut -d'|' -f7 <<<"$row")"
  instance="$(cut -d'|' -f8 <<<"$row")"
  inspected="$(docker container inspect "$workload" 2>/dev/null)" || return 1
  jq -er \
    --arg instance "$instance" --arg execution "$execution_id" --arg task "$task_id" --arg project "$project_id" \
    '.[0] | select(.Id == $instance and .State.Running == true and .Config.Labels["anban.ai/execution-id"] == $execution and .Config.Labels["anban.ai/task-id"] == $task and .Config.Labels["anban.ai/project-id"] == $project) | .Image' \
    <<<"$inspected"
}

poll_execution_identity() {
  local task_id="$1" expected_attempt="$2" expected_task_id="$3" project_id="$4"
  local deadline row attempt workload instance resolved_image
  deadline=$((SECONDS + POLL_DEADLINE_SECONDS))
  while (( SECONDS < deadline )); do
    row="$(query_execution "$task_id" 2>/dev/null || true)"
    attempt="$(cut -d'|' -f2 <<<"$row")"
    workload="$(cut -d'|' -f7 <<<"$row")"
    instance="$(cut -d'|' -f8 <<<"$row")"
    if [[ "$attempt" == "$expected_attempt" && -n "$workload" && -n "$instance" ]]; then
      if resolved_image="$(inspect_runtime_container "$row" "$expected_task_id" "$project_id")"; then
        printf '%s|%s' "$row" "$resolved_image"
        return 0
      fi
    fi
    sleep "$POLL_INTERVAL_SECONDS"
  done
  fail "task $task_id attempt $expected_attempt was not observed with an active verified container before the bounded deadline"
}

verify_persisted_runtime() {
  local row="$1" profile="$2" image="$3"
  [[ "$(cut -d'|' -f4 <<<"$row")" == "$profile" ]] || fail "persisted runtime profile mismatch: want $profile"
  [[ "$(cut -d'|' -f5 <<<"$row")" == "$image" ]] || fail "persisted runtime image mismatch: want $image"
  [[ "$(cut -d'|' -f6 <<<"$row")" == "docker" ]] || fail "persisted runtime scope is not docker"
  [[ -n "$(cut -d'|' -f7 <<<"$row")" && -n "$(cut -d'|' -f8 <<<"$row")" ]] || fail "persisted runtime workload identity is incomplete"
  [[ -n "$(cut -d'|' -f10 <<<"$row")" ]] || fail "resolved Docker image identity is incomplete"
}

poll_task() {
  local task_id="$1" deadline response status
  deadline=$((SECONDS + POLL_DEADLINE_SECONDS))
  while (( SECONDS < deadline )); do
    response="$(api_get "/api/v1/tasks/$task_id")"
    status="$(jq -er '.data.status' <<<"$response")"
    case "$status" in
      completed)
        jq -e '.data.result != null and .data.result != ""' <<<"$response" >/dev/null || fail "task $task_id completed without persisted output result"
        return 0
        ;;
      failed|cancelled)
        fail "task $task_id reached terminal status $status: $(jq -r '.data.error_message // "no error"' <<<"$response")"
        ;;
    esac
    sleep 3
  done
  fail "task $task_id did not become terminal before the bounded polling deadline"
}

task_volume() {
  local task_id="$1" volumes count volume
  volumes="$(docker volume ls -q --filter "label=anban.ai/task-id=$task_id")"
  count="$(wc -w <<<"$volumes" | tr -d ' ')"
  [[ "$count" == "1" ]] || fail "task $task_id owns $count labeled workspace volumes, want one"
  volume="$volumes"
  docker volume inspect "$volume" >/dev/null
  [[ "$(docker volume inspect --format '{{ index .Labels "anban.ai/task-id" }}' "$volume")" == "$task_id" ]] || fail "workspace volume task label mismatch"
  printf '%s' "$volume"
}

verify_output() {
  local volume="$1" image="$2" marker="$3"
  docker run --rm --entrypoint /bin/sh -v "$volume:/workspace:ro" "$image" -c \
    'test -f /workspace/output/runtime-smoke.txt && grep -F -- "$1" /workspace/output/runtime-smoke.txt >/dev/null' smoke "$marker"
}

poll_container_removed() {
  local workload="$1" deadline error
  deadline=$((SECONDS + 120))
  while (( SECONDS < deadline )); do
    if error="$(docker container inspect "$workload" 2>&1 >/dev/null)"; then
      sleep 2
      continue
    fi
    if [[ "$error" == *"No such"* || "$error" == *"not found"* ]]; then
      return 0
    fi
    fail "docker container inspect failed unexpectedly for $workload: $error"
    return 1
  done
  fail "terminal execution container $workload was not removed before the bounded deadline"
}

list_labeled_resources() {
  local kind="$1" label_key="$2" owner_id="$3"
  if [[ "$kind" == "container" ]]; then
    docker container ls -aq --filter "label=anban.ai/$label_key-id=$owner_id"
  else
    docker volume ls -q --filter "label=anban.ai/$label_key-id=$owner_id"
  fi
}

inspect_resource_owner() {
  local kind="$1" label_key="$2" resource="$3"
  if [[ "$kind" == "container" ]]; then
    docker container inspect --format "{{ index .Config.Labels \"anban.ai/$label_key-id\" }}" "$resource"
  else
    docker volume inspect --format "{{ index .Labels \"anban.ai/$label_key-id\" }}" "$resource"
  fi
}

remove_owned_resource() {
  local kind="$1" resource="$2"
  if [[ "$kind" == "container" ]]; then
    docker container rm -f "$resource"
  else
    docker volume rm "$resource"
  fi
}

cleanup_labeled_resources() {
  local attempt kind label_key ids owner_id resource owner resources seen
  local found cleanup_step_status sweep_status empty_sweeps=0 last_status=1
  for (( attempt = 1; attempt <= CLEANUP_MAX_ATTEMPTS; attempt++ )); do
    found=0
    sweep_status=0
    seen=" "
    for kind in container volume; do
      for label_key in project task; do
        if [[ "$label_key" == "project" ]]; then
          ids="${PROJECT_IDS:-}"
        else
          ids="${TASK_IDS:-}"
        fi
        for owner_id in $ids; do
          resources="$(list_labeled_resources "$kind" "$label_key" "$owner_id" 2>/dev/null)"
          cleanup_step_status=$?
          if (( cleanup_step_status != 0 )); then
            found=1
            (( sweep_status == 0 )) && sweep_status=$cleanup_step_status
            continue
          fi
          for resource in $resources; do
            if [[ "$seen" == *" $kind:$resource "* ]]; then
              continue
            fi
            seen+="$kind:$resource "
            found=1
            owner="$(inspect_resource_owner "$kind" "$label_key" "$resource" 2>/dev/null)"
            cleanup_step_status=$?
            if (( cleanup_step_status != 0 )); then
              (( sweep_status == 0 )) && sweep_status=$cleanup_step_status
              continue
            fi
            if [[ "$owner" != "$owner_id" ]]; then
              (( sweep_status == 0 )) && sweep_status=1
              continue
            fi
            remove_owned_resource "$kind" "$resource" >/dev/null 2>&1
            cleanup_step_status=$?
            if (( sweep_status == 0 && cleanup_step_status != 0 )); then
              sweep_status=$cleanup_step_status
            fi
          done
        done
      done
    done
    if (( found == 0 && sweep_status == 0 )); then
      empty_sweeps=$((empty_sweeps + 1))
      if (( empty_sweeps >= 2 )); then
        return 0
      fi
    else
      empty_sweeps=0
    fi
    (( sweep_status != 0 )) && last_status=$sweep_status
    if (( attempt < CLEANUP_MAX_ATTEMPTS )); then
      sleep "$CLEANUP_INTERVAL_SECONDS"
    fi
  done
  fail "owned Docker resources did not settle after $CLEANUP_MAX_ATTEMPTS cleanup attempts"
  return "$last_status"
}

cleanup() {
  local status="${1:-$?}" smoke_basename cleanup_status=0 cleanup_step_status
  trap - EXIT INT TERM
  set +e
  if [[ -n "${COMPOSE_FILE:-}" && -f "$COMPOSE_FILE" ]]; then
    compose stop server >/dev/null 2>&1
    cleanup_step_status=$?
    if (( cleanup_step_status != 0 )); then
      cleanup_status=$cleanup_step_status
    fi
  fi
  cleanup_labeled_resources
  cleanup_step_status=$?
  if (( cleanup_status == 0 && cleanup_step_status != 0 )); then
    cleanup_status=$cleanup_step_status
  fi
  if [[ -n "${COMPOSE_FILE:-}" && -f "$COMPOSE_FILE" ]]; then
    compose down -v --remove-orphans >/dev/null 2>&1
    cleanup_step_status=$?
    if (( cleanup_status == 0 && cleanup_step_status != 0 )); then
      cleanup_status=$cleanup_step_status
    fi
  fi
  if [[ -n "${SMOKE_DIR:-}" && -d "$SMOKE_DIR" ]]; then
    smoke_basename="$(basename "$SMOKE_DIR")"
    if [[ "$smoke_basename" == anban-runtime-smoke.* ]]; then
      rm -rf -- "$SMOKE_DIR"
      cleanup_step_status=$?
      if (( cleanup_status == 0 && cleanup_step_status != 0 )); then
        cleanup_status=$cleanup_step_status
      fi
    else
      printf 'runtime smoke cleanup refused unexpected temp path: %s\n' "$SMOKE_DIR" >&2
      if (( cleanup_status == 0 )); then
        cleanup_status=1
      fi
    fi
  fi
  if (( status == 0 && cleanup_status != 0 )); then
    status=$cleanup_status
  fi
  exit "$status"
}

write_server_config() {
  cat >"$CONFIG_FILE" <<'YAML'
server:
  host: "0.0.0.0"
  port: 8080
logging:
  level: info
database:
  dsn: "root:runtime-smoke-db@tcp(mysql:3306)/anban_creator?charset=utf8mb4&parseTime=True&loc=UTC"
redis:
  addr: "redis:6379"
jwt:
  secret_key: "runtime-smoke-jwt-secret-not-for-production"
  access_expiry: "1h"
  refresh_expiry: "2h"
storage:
  provider: local
  local_data_dir: "/app/data/files"
billing_runtime:
  config_dir: "/app/conf/billing"
  admin_api_key: "runtime-smoke-admin-key"
montage:
  enabled: true
  submodule_path: "/opt/montage-template"
  default_pipeline: "cinematic"
  allowed_pipelines: ["cinematic"]
  max_duration_seconds: 60
  max_assets: 1
  timeout_minutes: 10
  execution_targets: ["cloud"]
  default_execution_target: "cloud"
claude:
  execution_profiles:
    effective:
      description: "Runtime smoke DeepSeek V4 Flash"
      provider: "deepseek"
      envs:
        ANTHROPIC_BASE_URL: "${ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL}"
        ANTHROPIC_AUTH_TOKEN: "${ANBAN_DEEPSEEK_API_KEY}"
        ANTHROPIC_MODEL: "deepseek-v4-flash"
        ANTHROPIC_DEFAULT_OPUS_MODEL: "deepseek-v4-flash"
        ANTHROPIC_DEFAULT_FABLE_MODEL: "deepseek-v4-flash"
        ANTHROPIC_DEFAULT_SONNET_MODEL: "deepseek-v4-flash"
        ANTHROPIC_DEFAULT_HAIKU_MODEL: "deepseek-v4-flash"
        CLAUDE_CODE_EFFORT_LEVEL: "low"
      model_usage_aliases:
        deepseek-v4-flash: "deepseek-v4-flash"
  executor: docker
  runtime_images:
    article: "${ARTICLE_RUNTIME_IMAGE:-creator-agent-article:latest}"
    seednote: "${SEEDNOTE_RUNTIME_IMAGE:-creator-agent-seednote:latest}"
    montage: "${MONTAGE_RUNTIME_IMAGE:-creator-agent-montage:latest}"
  execution_token_secret: "runtime-smoke-execution-token-secret-32-bytes-minimum"
  agent_server_url: "http://server:8080"
  plugin_dir: "/anbanai"
  max_turns:
    article: 10
    seednote: 10
    montage: 10
  docker:
    network: "${ANBAN_RUNTIME_SMOKE_NETWORK}"
    cpu_cores: 1
    memory_mb: 4096
    pids_limit: 512
    timeout_sec: 900
asynq:
  concurrency: 3
  content_generate_timeout: 15m
  persist_timeout: 2m
invitation:
  enabled: false
memory:
  enabled: false
ilink:
  enabled: false
YAML
}

write_compose_config() {
  cat >"$COMPOSE_FILE" <<YAML
services:
  mysql:
    image: mysql:8.4
    environment:
      MYSQL_ROOT_PASSWORD: "$MYSQL_ROOT_PASSWORD"
      MYSQL_DATABASE: anban_creator
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost", "-p$MYSQL_ROOT_PASSWORD"]
      interval: 2s
      timeout: 3s
      retries: 30
    labels:
      anban.ai/runtime-smoke-project: "$COMPOSE_PROJECT"
  redis:
    image: redis:7-alpine
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 2s
      timeout: 3s
      retries: 30
    labels:
      anban.ai/runtime-smoke-project: "$COMPOSE_PROJECT"
  server:
    build:
      context: "$REPO_ROOT"
      dockerfile: deploy/docker/Dockerfile.server
    image: anban-creator-server:runtime-smoke
    depends_on:
      mysql:
        condition: service_healthy
      redis:
        condition: service_healthy
    environment:
      ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL:
      ANBAN_DEEPSEEK_API_KEY:
      ANBAN_RUNTIME_SMOKE_NETWORK: "$SMOKE_NETWORK"
    ports:
      - "127.0.0.1::8080"
    volumes:
      - "$CONFIG_FILE:/app/conf/config.yaml:ro"
      - runtime-files:/app/data/files
      - /var/run/docker.sock:/var/run/docker.sock
    group_add:
      - "$DOCKER_SOCKET_GID"
    labels:
      anban.ai/runtime-smoke-project: "$COMPOSE_PROJECT"
volumes:
  runtime-files:
    labels:
      anban.ai/runtime-smoke-project: "$COMPOSE_PROJECT"
networks:
  default:
    name: "$SMOKE_NETWORK"
    labels:
      anban.ai/runtime-smoke-project: "$COMPOSE_PROJECT"
YAML
}

runtime_smoke_main() {
  if ! type -P -- docker >/dev/null 2>&1; then
    printf 'SKIP: Docker CLI is not installed\n'
    return 0
  fi
  [[ -n "${ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL:-}" ]] || fail "missing required configuration: ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL"
  [[ -n "${ANBAN_DEEPSEEK_API_KEY:-}" ]] || fail "missing required configuration: ANBAN_DEEPSEEK_API_KEY"
  local command_name deadline registration catalog top_up_body top_up
  for command_name in curl git jq mktemp; do
    require_command "$command_name"
  done
  docker compose version >/dev/null 2>&1 || fail "Docker Compose v2 is required"
  docker info >/dev/null 2>&1 || fail "Docker daemon is unavailable"
  case "$POLL_DEADLINE_SECONDS" in
    ''|*[!0-9]*) fail "ANBAN_RUNTIME_SMOKE_POLL_DEADLINE_SECONDS must be a positive integer" ;;
    0) fail "ANBAN_RUNTIME_SMOKE_POLL_DEADLINE_SECONDS must be a positive integer" ;;
  esac
  case "$CLEANUP_MAX_ATTEMPTS" in
    ''|*[!0-9]*) fail "ANBAN_RUNTIME_SMOKE_CLEANUP_MAX_ATTEMPTS must be a positive integer" ;;
    0) fail "ANBAN_RUNTIME_SMOKE_CLEANUP_MAX_ATTEMPTS must be a positive integer" ;;
  esac
  case "$CLEANUP_INTERVAL_SECONDS" in
    ''|*[!0-9]*) fail "ANBAN_RUNTIME_SMOKE_CLEANUP_INTERVAL_SECONDS must be a non-negative integer" ;;
  esac

  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
  ARTICLE_RUNTIME_IMAGE=creator-agent-article:latest
  SEEDNOTE_RUNTIME_IMAGE=creator-agent-seednote:latest
  MONTAGE_RUNTIME_IMAGE=creator-agent-montage:latest
  ARTICLE_DOCKERFILE=Dockerfile.agent-article
  SEEDNOTE_DOCKERFILE=Dockerfile.agent-seednote
  MONTAGE_DOCKERFILE=Dockerfile.agent-montage
  git -C "$REPO_ROOT" submodule update --init --recursive \
    third_party/OpenMontage
  docker build -f "$REPO_ROOT/deploy/docker/$ARTICLE_DOCKERFILE" -t "$ARTICLE_RUNTIME_IMAGE" "$REPO_ROOT"
  docker build -f "$REPO_ROOT/deploy/docker/$SEEDNOTE_DOCKERFILE" -t "$SEEDNOTE_RUNTIME_IMAGE" "$REPO_ROOT"
  docker build -f "$REPO_ROOT/deploy/docker/$MONTAGE_DOCKERFILE" -t "$MONTAGE_RUNTIME_IMAGE" "$REPO_ROOT"

  SMOKE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/anban-runtime-smoke.XXXXXX")"
  COMPOSE_PROJECT="anban-runtime-smoke-$(date +%s)-$$"
  SMOKE_NETWORK="${COMPOSE_PROJECT}-network"
  COMPOSE_FILE="$SMOKE_DIR/compose.yaml"
  CONFIG_FILE="$SMOKE_DIR/config.yaml"
  MYSQL_ROOT_PASSWORD="runtime-smoke-db"
  DOCKER_SOCKET_GID="$(stat -L -c '%g' /var/run/docker.sock 2>/dev/null || stat -L -f '%g' /var/run/docker.sock 2>/dev/null || printf '0')"
  TASK_IDS=""
  PROJECT_IDS=""
  SERVER_URL=""
  TOKEN=""
  CREATED_PROJECT_ID=""
  CREATED_TASK_ID=""
  trap 'cleanup "$?"' EXIT
  trap 'exit 130' INT TERM

  write_server_config
  printf 'runtime smoke profile: provider=deepseek model=deepseek-v4-flash\n'
  write_compose_config
  chmod 0755 "$SMOKE_DIR"
  chmod 0644 "$CONFIG_FILE" "$COMPOSE_FILE"

  compose up -d --build mysql redis server
  HOST_PORT="$(compose port server 8080 | awk -F: '{print $NF}')"
  [[ -n "$HOST_PORT" ]] || fail "Compose did not publish the server port"
  SERVER_URL="http://127.0.0.1:$HOST_PORT"

  deadline=$((SECONDS + 120))
  while (( SECONDS < deadline )); do
    if curl -fsS "$SERVER_URL/health" >/dev/null 2>&1; then
      break
    fi
    sleep 2
  done
  curl -fsS "$SERVER_URL/health" >/dev/null || fail "server did not become healthy before the bounded deadline"

  registration="$(api_post /api/v1/auth/register '{"email":"runtime-smoke@example.invalid","password":"runtime-smoke-password","nickname":"Runtime Smoke"}')"
  TOKEN="$(jq -er '.data.token' <<<"$registration")"
  USER_ID="$(jq -er '.data.user.id' <<<"$registration")"
  catalog="$(api_get /api/v1/billing/catalog)"
  CATALOG_ID="$(jq -er '.data.catalog_id' <<<"$catalog")"
  TASK_ADMISSION_CREDITS="$(jq -er '[.data.skus[] | select((.operation == "task.article" or .operation == "task.seednote" or .operation == "task.montage") and .execution_profile == "effective") | .price_credits] | if length == 3 then add else error("effective managed task billing SKUs are incomplete") end' <<<"$catalog")"
  TOP_UP_CREDITS=$((TASK_ADMISSION_CREDITS + 1000))
  TOP_UP_SOURCE="runtime-smoke-$USER_ID"
  top_up_body="$(jq -cn \
    --arg user_id "$USER_ID" --argjson credits "$TOP_UP_CREDITS" --arg catalog_id "$CATALOG_ID" --arg source "$TOP_UP_SOURCE" \
    '{user_id:$user_id,credits:$credits,external_source_type:"runtime_smoke",external_source_id:$source,catalog_id:$catalog_id,request_fingerprint:"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",idempotency_scope:"runtime-smoke",idempotency_key:$source}')"
  top_up="$(curl -fsS -H 'Content-Type: application/json' -H 'X-Admin-API-Key: runtime-smoke-admin-key' -d "$top_up_body" "$SERVER_URL/api/admin/billing/topups")"
  [[ "$(jq -er '.code' <<<"$top_up")" == "0" ]] || fail "disposable smoke user top-up failed"

  ARTICLE_MARKER="ARTICLE_RUNTIME_SMOKE_$$"
  SEEDNOTE_MARKER="SEEDNOTE_RUNTIME_SMOKE_$$"
  MONTAGE_MARKER="MONTAGE_RUNTIME_SMOKE_$$"
  create_project article "$ARTICLE_MARKER"
  ARTICLE_PROJECT="$CREATED_PROJECT_ID"
  create_task article "$ARTICLE_PROJECT" "$ARTICLE_MARKER"
  ARTICLE_TASK="$CREATED_TASK_ID"
  ARTICLE_EXECUTION="$(poll_execution_identity "$ARTICLE_TASK" 1 "$ARTICLE_TASK" "$ARTICLE_PROJECT")"
  verify_persisted_runtime "$ARTICLE_EXECUTION" article "$ARTICLE_RUNTIME_IMAGE"

  create_project seednote "$SEEDNOTE_MARKER"
  SEEDNOTE_PROJECT="$CREATED_PROJECT_ID"
  create_task seednote "$SEEDNOTE_PROJECT" "$SEEDNOTE_MARKER"
  SEEDNOTE_TASK="$CREATED_TASK_ID"
  SEEDNOTE_EXECUTION="$(poll_execution_identity "$SEEDNOTE_TASK" 1 "$SEEDNOTE_TASK" "$SEEDNOTE_PROJECT")"
  verify_persisted_runtime "$SEEDNOTE_EXECUTION" seednote "$SEEDNOTE_RUNTIME_IMAGE"

  create_project montage "$MONTAGE_MARKER"
  MONTAGE_PROJECT="$CREATED_PROJECT_ID"
  create_task montage "$MONTAGE_PROJECT" "$MONTAGE_MARKER"
  MONTAGE_TASK="$CREATED_TASK_ID"
  MONTAGE_EXECUTION="$(poll_execution_identity "$MONTAGE_TASK" 1 "$MONTAGE_TASK" "$MONTAGE_PROJECT")"
  verify_persisted_runtime "$MONTAGE_EXECUTION" montage "$MONTAGE_RUNTIME_IMAGE"

  poll_task "$ARTICLE_TASK"
  poll_task "$SEEDNOTE_TASK"
  poll_task "$MONTAGE_TASK"
  ARTICLE_VOLUME="$(task_volume "$ARTICLE_TASK")"
  SEEDNOTE_VOLUME="$(task_volume "$SEEDNOTE_TASK")"
  MONTAGE_VOLUME="$(task_volume "$MONTAGE_TASK")"
  verify_output "$ARTICLE_VOLUME" "$ARTICLE_RUNTIME_IMAGE" "$ARTICLE_MARKER"
  verify_output "$SEEDNOTE_VOLUME" "$SEEDNOTE_RUNTIME_IMAGE" "$SEEDNOTE_MARKER"
  verify_output "$MONTAGE_VOLUME" "$MONTAGE_RUNTIME_IMAGE" "$MONTAGE_MARKER"
  poll_container_removed "$(cut -d'|' -f7 <<<"$ARTICLE_EXECUTION")"
  poll_container_removed "$(cut -d'|' -f7 <<<"$SEEDNOTE_EXECUTION")"
  poll_container_removed "$(cut -d'|' -f7 <<<"$MONTAGE_EXECUTION")"

  RESUME_MARKER="ARTICLE_RESUMED_RUNTIME_SMOKE_$$"
  api_post "/api/v1/tasks/$ARTICLE_TASK/resume" "$(jq -cn --arg marker "$RESUME_MARKER" '{prompt:("Append the exact RESUMED marker " + $marker + " to /workspace/output/runtime-smoke.txt, call submit_agent_feedback once, then stop successfully."),input_attachments:[]}')" >/dev/null
  RESUME_EXECUTION="$(poll_execution_identity "$ARTICLE_TASK" 2 "$ARTICLE_TASK" "$ARTICLE_PROJECT")"
  verify_persisted_runtime "$RESUME_EXECUTION" article "$ARTICLE_RUNTIME_IMAGE"
  [[ "$(cut -d'|' -f3 <<<"$RESUME_EXECUTION")" == "$(cut -d'|' -f1 <<<"$ARTICLE_EXECUTION")" ]] || fail "resume parent execution identity mismatch"
  [[ "$(cut -d'|' -f5 <<<"$RESUME_EXECUTION")" == "$(cut -d'|' -f5 <<<"$ARTICLE_EXECUTION")" ]] || fail "resume did not inherit the original runtime image"
  [[ "$(cut -d'|' -f10 <<<"$RESUME_EXECUTION")" == "$(cut -d'|' -f10 <<<"$ARTICLE_EXECUTION")" ]] || fail "resume did not inherit the original resolved Docker image"
  [[ "$(task_volume "$ARTICLE_TASK")" == "$ARTICLE_VOLUME" ]] || fail "resume did not reuse the same Docker workspace volume"
  poll_task "$ARTICLE_TASK"
  verify_output "$ARTICLE_VOLUME" "$ARTICLE_RUNTIME_IMAGE" "$RESUME_MARKER"
  poll_container_removed "$(cut -d'|' -f7 <<<"$RESUME_EXECUTION")"

  printf 'PASS: Docker runtime smoke completed article, seednote, montage, and article resume (%s)\n' "$COMPOSE_PROJECT"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  set -euo pipefail
  runtime_smoke_main "$@"
fi
