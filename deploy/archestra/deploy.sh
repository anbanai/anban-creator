#!/usr/bin/env bash
set +x
set -euo pipefail

chart="oci://europe-west1-docker.pkg.dev/friendly-path-465518-r6/archestra-public/helm-charts/archestra-platform"
namespace="${ARCHESTRA_NAMESPACE:-archestra}"
release="${ARCHESTRA_RELEASE:-archestra-platform}"
version="${ARCHESTRA_VERSION:-1.4.12}"
wait_timeout="${ARCHESTRA_WAIT_TIMEOUT:-15m}"
admin_secret_name=archestra-bootstrap
dry_run="${DRY_RUN:-false}"
script_root=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
values_file="${ARCHESTRA_VALUES_FILE:-$script_root/values.yaml}"

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

require_non_empty() {
  local name=$1
  [[ -n "${!name:-}" ]] || fail "$name must be set (use a Yunxiao secure variable)"
}

require_non_empty ARCHESTRA_ADMIN_EMAIL
require_non_empty ARCHESTRA_ADMIN_PASSWORD
[[ -f "$values_file" ]] || fail "values file not found: $values_file"

command -v helm >/dev/null 2>&1 || fail "helm is required"
command -v kubectl >/dev/null 2>&1 || fail "kubectl is required"
command -v base64 >/dev/null 2>&1 || fail "base64 is required"

case "$dry_run" in
  true|false) ;;
  *) fail "DRY_RUN must be true or false" ;;
esac

kubectl_args=(kubectl)
helm_context_args=()
if [[ -n "${KUBE_CONTEXT:-}" ]]; then
  kubectl_args+=(--context "$KUBE_CONTEXT")
  helm_context_args+=(--kube-context "$KUBE_CONTEXT")
fi

current_context=$("${kubectl_args[@]}" config current-context 2>/dev/null) ||
  fail "kubectl has no usable current context"
[[ -n "$current_context" ]] || fail "kubectl current context is empty"

helm_set_args=(--set-string "archestra.imageTag=$version")
helm_set_value() {
  local value=$1
  value=${value//\\/\\\\}
  value=${value//,/\\,}
  printf '%s' "$value"
}
if [[ -n "${ARCHESTRA_FRONTEND_URL:-}" ]]; then
  helm_set_args+=(--set-string "archestra.env.ARCHESTRA_FRONTEND_URL=$(helm_set_value "$ARCHESTRA_FRONTEND_URL")")
fi
if [[ -n "${ARCHESTRA_API_BASE_URL:-}" ]]; then
  helm_set_args+=(--set-string "archestra.env.ARCHESTRA_API_BASE_URL=$(helm_set_value "$ARCHESTRA_API_BASE_URL")")
fi

helm_args=(helm)
if ((${#helm_context_args[@]} > 0)); then
  helm_args+=("${helm_context_args[@]}")
fi
helm_args+=(
  upgrade --install "$release" "$chart"
  --version "$version"
  --namespace "$namespace"
  --create-namespace
  --values "$values_file"
  "${helm_set_args[@]}"
  --wait
  --timeout "$wait_timeout"
)

if [[ "$dry_run" == "true" ]]; then
  "${helm_args[@]}" --dry-run=client --debug
  printf 'DRY_RUN=true: no Kubernetes resources were created. Context: %s\n' "$current_context"
  exit 0
fi

"${kubectl_args[@]}" create namespace "$namespace" --dry-run=client -o yaml |
  "${kubectl_args[@]}" apply -f - >/dev/null

admin_email_b64=$(printf '%s' "$ARCHESTRA_ADMIN_EMAIL" | base64 | tr -d '\n')
admin_password_b64=$(printf '%s' "$ARCHESTRA_ADMIN_PASSWORD" | base64 | tr -d '\n')
cat <<EOF | "${kubectl_args[@]}" -n "$namespace" apply -f - >/dev/null
apiVersion: v1
kind: Secret
metadata:
  name: $admin_secret_name
type: Opaque
data:
  ARCHESTRA_AUTH_ADMIN_EMAIL: $admin_email_b64
  ARCHESTRA_AUTH_ADMIN_PASSWORD: $admin_password_b64
EOF

"${helm_args[@]}"

deployment_name="$release"
worker_deployment_name="${release}-worker"
"${kubectl_args[@]}" -n "$namespace" rollout status "deployment/$deployment_name" --timeout="$wait_timeout"
if "${kubectl_args[@]}" -n "$namespace" get "deployment/$worker_deployment_name" >/dev/null 2>&1; then
  "${kubectl_args[@]}" -n "$namespace" rollout status "deployment/$worker_deployment_name" --timeout="$wait_timeout"
fi

migration_job_name="${release}-migrate"
if "${kubectl_args[@]}" -n "$namespace" get "job/$migration_job_name" >/dev/null 2>&1; then
  "${kubectl_args[@]}" -n "$namespace" wait --for=condition=complete "job/$migration_job_name" --timeout="$wait_timeout"
else
  printf 'Migration hook job %s is absent or already cleaned up by Helm.\n' "$migration_job_name"
fi

printf 'Archestra deployed: release=%s namespace=%s version=%s context=%s\n' \
  "$release" "$namespace" "$version" "$current_context"
"${kubectl_args[@]}" -n "$namespace" get pods,svc -l "app.kubernetes.io/instance=$release"
printf 'Local verification: kubectl --context %s -n %s port-forward svc/%s 3000:3000 9000:9000\n' \
  "$current_context" "$namespace" "$release"
