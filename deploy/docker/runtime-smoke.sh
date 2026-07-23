#!/usr/bin/env bash
set -euo pipefail

DOCKER_CLI="${DOCKER_CLI:-docker}"
if ! command -v "$DOCKER_CLI" >/dev/null 2>&1; then
  printf 'SKIP: Docker-compatible CLI is not installed (%s)\n' "$DOCKER_CLI"
  exit 0
fi

docker() {
  command "$DOCKER_CLI" "$@"
}

fail() {
  printf 'runtime smoke FAILED: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command is unavailable: $1"
}

[[ -n "${CLAUDE_CODE_AUTH_TOKEN:-}" ]] || fail "missing required configuration: CLAUDE_CODE_AUTH_TOKEN"
for command_name in curl git jq mktemp; do
  require_command "$command_name"
done
docker compose version >/dev/null 2>&1 || fail "Docker Compose v2 is required"
docker info >/dev/null 2>&1 || fail "Docker daemon is unavailable"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
POLL_DEADLINE_SECONDS="${ANBAN_RUNTIME_SMOKE_POLL_DEADLINE_SECONDS:-900}"
case "$POLL_DEADLINE_SECONDS" in
  ''|*[!0-9]*) fail "ANBAN_RUNTIME_SMOKE_POLL_DEADLINE_SECONDS must be a positive integer" ;;
  0) fail "ANBAN_RUNTIME_SMOKE_POLL_DEADLINE_SECONDS must be a positive integer" ;;
esac

if [[ "${ANBAN_RUNTIME_SMOKE_SKIP_IMAGE_BUILD:-0}" != "1" ]]; then
  git -C "$REPO_ROOT" submodule update --init --recursive \
    third_party/claude-agent-sdk-go third_party/Agent-Reach third_party/OpenMontage
  docker build -f "$REPO_ROOT/deploy/docker/Dockerfile.agent-article" -t creator-agent-article:latest "$REPO_ROOT"
  docker build -f "$REPO_ROOT/deploy/docker/Dockerfile.agent-seednote" -t creator-agent-seednote:latest "$REPO_ROOT"
  docker build -f "$REPO_ROOT/deploy/docker/Dockerfile.agent-montage" -t creator-agent-montage:latest "$REPO_ROOT"
fi

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

compose() {
  docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" "$@"
}

cleanup_labeled_resources() {
  local id resource label
  set +e
  for id in $TASK_IDS; do
    for resource in $(docker container ls -aq --filter "label=anban.ai/task-id=$id"); do
      label="$(docker container inspect --format '{{ index .Config.Labels "anban.ai/task-id" }}' "$resource" 2>/dev/null)"
      [[ "$label" == "$id" ]] && docker container rm -f "$resource" >/dev/null 2>&1
    done
  done
  for id in $PROJECT_IDS; do
    for resource in $(docker volume ls -q --filter "label=anban.ai/project-id=$id"); do
      label="$(docker volume inspect --format '{{ index .Labels "anban.ai/project-id" }}' "$resource" 2>/dev/null)"
      [[ "$label" == "$id" ]] && docker volume rm "$resource" >/dev/null 2>&1
    done
  done
  set -e
}

cleanup() {
  local status="$?"
  set +e
  cleanup_labeled_resources
  if [[ -f "$COMPOSE_FILE" ]]; then
    compose down -v --remove-orphans >/dev/null 2>&1
  fi
  rm -rf "$SMOKE_DIR"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

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
  provider: "volcengine_ark"
  base_url: "https://ark.cn-beijing.volces.com/api/compatible"
  auth_token: "${CLAUDE_CODE_AUTH_TOKEN}"
  models:
    default: "doubao-seed-evolving"
    opus: "doubao-seed-evolving"
    fable: "doubao-seed-evolving"
    sonnet: "doubao-seed-2-1-pro-260628"
    haiku: "doubao-seed-2-1-turbo-260628"
  model_usage_aliases:
    doubao-seed-evolving-latest-version: "doubao-seed-evolving"
  executor: docker
  runtime_images:
    article: "creator-agent-article:latest"
    seednote: "creator-agent-seednote:latest"
    montage: "creator-agent-montage:latest"
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
      CLAUDE_CODE_AUTH_TOKEN:
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

api_post() {
  local path="$1" body="$2"
  if [[ -n "$TOKEN" ]]; then
    curl -fsS -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d "$body" "$SERVER_URL$path"
  else
    curl -fsS -H 'Content-Type: application/json' -d "$body" "$SERVER_URL$path"
  fi
}

api_get() {
  curl -fsS -H "Authorization: Bearer $TOKEN" "$SERVER_URL$1"
}

registration="$(api_post /api/v1/auth/register '{"email":"runtime-smoke@example.invalid","password":"runtime-smoke-password","nickname":"Runtime Smoke"}')"
TOKEN="$(jq -er '.data.token' <<<"$registration")"
USER_ID="$(jq -er '.data.user.id' <<<"$registration")"
catalog="$(api_get /api/v1/billing/catalog)"
CATALOG_ID="$(jq -er '.data.catalog_id' <<<"$catalog")"
TASK_ADMISSION_CREDITS="$(jq -er '[.data.skus[] | select(.operation == "task.article" or .operation == "task.seednote" or .operation == "task.montage") | .price_credits] | if length == 3 then add else error("managed task billing SKUs are incomplete") end' <<<"$catalog")"
TOP_UP_CREDITS=$((TASK_ADMISSION_CREDITS + 1000))
TOP_UP_SOURCE="runtime-smoke-$USER_ID"
top_up_body="$(jq -cn \
  --arg user_id "$USER_ID" --argjson credits "$TOP_UP_CREDITS" --arg catalog_id "$CATALOG_ID" --arg source "$TOP_UP_SOURCE" \
  '{user_id:$user_id,credits:$credits,external_source_type:"runtime_smoke",external_source_id:$source,catalog_id:$catalog_id,request_fingerprint:"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",idempotency_scope:"runtime-smoke",idempotency_key:$source}')"
top_up="$(curl -fsS -H 'Content-Type: application/json' -H 'X-Admin-API-Key: runtime-smoke-admin-key' -d "$top_up_body" "$SERVER_URL/api/admin/billing/topups")"
[[ "$(jq -er '.code' <<<"$top_up")" == "0" ]] || fail "disposable smoke user top-up failed"

create_project() {
  local profile="$1" marker="$2" response project_id
  response="$(api_post /api/v1/projects "$(jq -cn --arg profile "$profile" --arg marker "$marker" '{platform:$profile,name:("Runtime smoke " + $profile),instructions:("Managed runtime smoke only. Do not perform the normal content workflow. Use the Bash tool to create output/ and write the exact marker " + $marker + " to output/runtime-smoke.txt, call submit_agent_feedback once, then stop successfully. On a resume request append its exact RESUMED marker to the same file and stop successfully."),enable_publishing:false}')")"
  project_id="$(jq -er '.data.id' <<<"$response")"
  PROJECT_IDS="$PROJECT_IDS $project_id"
  CREATED_PROJECT_ID="$project_id"
}

create_task() {
  local profile="$1" project_id="$2" marker="$3" response task_id body
  if [[ "$profile" == "montage" ]]; then
    body="$(jq -cn --arg project "$project_id" --arg marker "$marker" '{project_id:$project,prompt:("Execute only the managed runtime smoke marker: " + $marker),quantity:1,montage_input:{brief:("Runtime smoke marker " + $marker),pipeline_key:"cinematic",preferences:{duration_seconds:5}}}')"
  else
    body="$(jq -cn --arg project "$project_id" --arg marker "$marker" '{project_id:$project,prompt:("Execute only the managed runtime smoke marker: " + $marker),quantity:1,has_content_image:false,has_tail_image:false,article_with_cover:false,article_with_content_images:false}')"
  fi
  response="$(api_post /api/v1/tasks "$body")"
  task_id="$(jq -er '.data.id' <<<"$response")"
  TASK_IDS="$TASK_IDS $task_id"
  CREATED_TASK_ID="$task_id"
}

query_execution() {
  local task_id="$1"
  compose exec -T -e MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql \
    mysql -uroot --batch --skip-column-names anban_creator \
    -e "SELECT CONCAT_WS('|',id,attempt,COALESCE(parent_execution_id,''),runtime_profile,runtime_image,runtime_scope,runtime_workload,COALESCE(runtime_instance_id,''),status) FROM task_executions WHERE task_id='$task_id' ORDER BY attempt DESC LIMIT 1"
}

poll_execution_identity() {
  local task_id="$1" deadline row workload instance
  deadline=$((SECONDS + POLL_DEADLINE_SECONDS))
  while (( SECONDS < deadline )); do
    row="$(query_execution "$task_id")"
    workload="$(cut -d'|' -f7 <<<"$row")"
    instance="$(cut -d'|' -f8 <<<"$row")"
    if [[ -n "$workload" && -n "$instance" ]]; then
      printf '%s' "$row"
      return 0
    fi
    sleep 2
  done
  fail "task $task_id did not persist runtime_workload and runtime instance before the bounded deadline"
}

inspect_running_container() {
  local row="$1" task_id="$2" project_id="$3" workload instance
  workload="$(cut -d'|' -f7 <<<"$row")"
  instance="$(cut -d'|' -f8 <<<"$row")"
  [[ "$(docker container inspect --format '{{.Id}}' "$workload")" == "$instance" ]] || fail "persisted runtime instance does not match Docker inspect"
  [[ "$(docker container inspect --format '{{ index .Config.Labels "anban.ai/task-id" }}' "$workload")" == "$task_id" ]] || fail "runtime container task cleanup label mismatch"
  [[ "$(docker container inspect --format '{{ index .Config.Labels "anban.ai/project-id" }}' "$workload")" == "$project_id" ]] || fail "runtime container project cleanup label mismatch"
  [[ "$(docker container inspect --format '{{ index .Config.Labels "anban.ai/execution-id" }}' "$workload")" == "$(cut -d'|' -f1 <<<"$row")" ]] || fail "runtime container execution cleanup label mismatch"
}

verify_persisted_runtime() {
  local row="$1" profile="$2" image="$3"
  [[ "$(cut -d'|' -f4 <<<"$row")" == "$profile" ]] || fail "persisted runtime profile mismatch: want $profile"
  [[ "$(cut -d'|' -f5 <<<"$row")" == "$image" ]] || fail "persisted runtime image mismatch: want $image"
  [[ "$(cut -d'|' -f6 <<<"$row")" == "docker" ]] || fail "persisted runtime scope is not docker"
  [[ -n "$(cut -d'|' -f7 <<<"$row")" && -n "$(cut -d'|' -f8 <<<"$row")" ]] || fail "persisted runtime workload identity is incomplete"
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
    'find /workspace -path "*/output/runtime-smoke.txt" -type f -exec grep -F -- "$1" {} \; | grep -F -- "$1" >/dev/null' smoke "$marker"
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
  done
  fail "terminal execution container $workload was not removed before the bounded deadline"
}

ARTICLE_MARKER="ARTICLE_RUNTIME_SMOKE_$$"
SEEDNOTE_MARKER="SEEDNOTE_RUNTIME_SMOKE_$$"
MONTAGE_MARKER="MONTAGE_RUNTIME_SMOKE_$$"
create_project article "$ARTICLE_MARKER"
ARTICLE_PROJECT="$CREATED_PROJECT_ID"
create_project seednote "$SEEDNOTE_MARKER"
SEEDNOTE_PROJECT="$CREATED_PROJECT_ID"
create_project montage "$MONTAGE_MARKER"
MONTAGE_PROJECT="$CREATED_PROJECT_ID"
create_task article "$ARTICLE_PROJECT" "$ARTICLE_MARKER"
ARTICLE_TASK="$CREATED_TASK_ID"
create_task seednote "$SEEDNOTE_PROJECT" "$SEEDNOTE_MARKER"
SEEDNOTE_TASK="$CREATED_TASK_ID"
create_task montage "$MONTAGE_PROJECT" "$MONTAGE_MARKER"
MONTAGE_TASK="$CREATED_TASK_ID"

ARTICLE_EXECUTION="$(poll_execution_identity "$ARTICLE_TASK")"
SEEDNOTE_EXECUTION="$(poll_execution_identity "$SEEDNOTE_TASK")"
MONTAGE_EXECUTION="$(poll_execution_identity "$MONTAGE_TASK")"
verify_persisted_runtime "$ARTICLE_EXECUTION" article creator-agent-article:latest
verify_persisted_runtime "$SEEDNOTE_EXECUTION" seednote creator-agent-seednote:latest
verify_persisted_runtime "$MONTAGE_EXECUTION" montage creator-agent-montage:latest
inspect_running_container "$ARTICLE_EXECUTION" "$ARTICLE_TASK" "$ARTICLE_PROJECT"
inspect_running_container "$SEEDNOTE_EXECUTION" "$SEEDNOTE_TASK" "$SEEDNOTE_PROJECT"
inspect_running_container "$MONTAGE_EXECUTION" "$MONTAGE_TASK" "$MONTAGE_PROJECT"
ARTICLE_RESOLVED_IMAGE="$(docker container inspect --format '{{.Image}}' "$(cut -d'|' -f7 <<<"$ARTICLE_EXECUTION")")"

poll_task "$ARTICLE_TASK"
poll_task "$SEEDNOTE_TASK"
poll_task "$MONTAGE_TASK"
ARTICLE_VOLUME="$(task_volume "$ARTICLE_TASK")"
SEEDNOTE_VOLUME="$(task_volume "$SEEDNOTE_TASK")"
MONTAGE_VOLUME="$(task_volume "$MONTAGE_TASK")"
verify_output "$ARTICLE_VOLUME" creator-agent-article:latest "$ARTICLE_MARKER"
verify_output "$SEEDNOTE_VOLUME" creator-agent-seednote:latest "$SEEDNOTE_MARKER"
verify_output "$MONTAGE_VOLUME" creator-agent-montage:latest "$MONTAGE_MARKER"
poll_container_removed "$(cut -d'|' -f7 <<<"$ARTICLE_EXECUTION")"
poll_container_removed "$(cut -d'|' -f7 <<<"$SEEDNOTE_EXECUTION")"
poll_container_removed "$(cut -d'|' -f7 <<<"$MONTAGE_EXECUTION")"

RESUME_MARKER="ARTICLE_RESUMED_RUNTIME_SMOKE_$$"
api_post "/api/v1/tasks/$ARTICLE_TASK/resume" "$(jq -cn --arg marker "$RESUME_MARKER" '{prompt:("Append the exact RESUMED marker " + $marker + " to output/runtime-smoke.txt, call submit_agent_feedback once, then stop successfully."),input_attachments:[]}')" >/dev/null
RESUME_EXECUTION="$(poll_execution_identity "$ARTICLE_TASK")"
verify_persisted_runtime "$RESUME_EXECUTION" article creator-agent-article:latest
[[ "$(cut -d'|' -f2 <<<"$RESUME_EXECUTION")" == "2" ]] || fail "resume did not create execution attempt 2"
[[ "$(cut -d'|' -f3 <<<"$RESUME_EXECUTION")" == "$(cut -d'|' -f1 <<<"$ARTICLE_EXECUTION")" ]] || fail "resume parent execution identity mismatch"
[[ "$(cut -d'|' -f5 <<<"$RESUME_EXECUTION")" == "$(cut -d'|' -f5 <<<"$ARTICLE_EXECUTION")" ]] || fail "resume did not inherit the original runtime image"
[[ "$(task_volume "$ARTICLE_TASK")" == "$ARTICLE_VOLUME" ]] || fail "resume did not reuse the same Docker workspace volume"
inspect_running_container "$RESUME_EXECUTION" "$ARTICLE_TASK" "$ARTICLE_PROJECT"
[[ "$(docker container inspect --format '{{.Image}}' "$(cut -d'|' -f7 <<<"$RESUME_EXECUTION")")" == "$ARTICLE_RESOLVED_IMAGE" ]] || fail "resume did not inherit the original resolved Docker image"
poll_task "$ARTICLE_TASK"
verify_output "$ARTICLE_VOLUME" creator-agent-article:latest "$RESUME_MARKER"
poll_container_removed "$(cut -d'|' -f7 <<<"$RESUME_EXECUTION")"

printf 'PASS: Docker runtime smoke completed article, seednote, montage, and article resume (%s)\n' "$COMPOSE_PROJECT"
