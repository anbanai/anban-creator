#!/usr/bin/env bash
set -euo pipefail

test_root=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$test_root/deploy.sh"

assert_contains() {
  local haystack=$1
  local needle=$2
  if [[ "$haystack" != *"$needle"* ]]; then
    printf 'expected output to contain %q\nactual output:\n%s\n' "$needle" "$haystack" >&2
    exit 1
  fi
}

run_missing_admin_email_test() {
  local output status
  set +e
  output=$(ARCHESTRA_ADMIN_PASSWORD=test-password DRY_RUN=true bash "$script" 2>&1)
  status=$?
  set -e
  [[ $status -ne 0 ]] || { echo "missing admin email should fail" >&2; exit 1; }
  assert_contains "$output" "ARCHESTRA_ADMIN_EMAIL"
}

run_missing_admin_password_test() {
  local output status
  set +e
  output=$(ARCHESTRA_ADMIN_EMAIL=admin@example.com DRY_RUN=true bash "$script" 2>&1)
  status=$?
  set -e
  [[ $status -ne 0 ]] || { echo "missing admin password should fail" >&2; exit 1; }
  assert_contains "$output" "ARCHESTRA_ADMIN_PASSWORD"
}

run_missing_helm_test() {
  local output status
  set +e
  output=$(
    PATH=/usr/bin:/bin \
      ARCHESTRA_ADMIN_EMAIL=admin@example.com \
      ARCHESTRA_ADMIN_PASSWORD=test-password \
      DRY_RUN=true \
      /bin/bash "$script" 2>&1
  )
  status=$?
  set -e
  [[ $status -ne 0 ]] || { echo "missing helm should fail" >&2; exit 1; }
  assert_contains "$output" "helm is required"
}

run_missing_kubectl_test() {
  local fake_bin output status
  fake_bin=$(mktemp -d)
  trap 'rm -rf "$fake_bin"' RETURN
  printf '#!/usr/bin/env bash\nexit 0\n' >"$fake_bin/helm"
  chmod +x "$fake_bin/helm"

  set +e
  output=$(
    PATH="$fake_bin:/usr/bin:/bin" \
      ARCHESTRA_ADMIN_EMAIL=admin@example.com \
      ARCHESTRA_ADMIN_PASSWORD=test-password \
      DRY_RUN=true \
      /bin/bash "$script" 2>&1
  )
  status=$?
  set -e
  [[ $status -ne 0 ]] || { echo "missing kubectl should fail" >&2; exit 1; }
  assert_contains "$output" "kubectl is required"
}

run_dry_run_command_test() {
  local fake_bin output log
  fake_bin=$(mktemp -d)
  log=$(mktemp)
  trap 'rm -rf "$fake_bin" "$log"' RETURN

  cat >"$fake_bin/helm" <<'EOF'
#!/usr/bin/env bash
printf 'helm %s\n' "$*" >>"$TEST_COMMAND_LOG"
exit 0
EOF
  cat >"$fake_bin/kubectl" <<'EOF'
#!/usr/bin/env bash
printf 'kubectl %s\n' "$*" >>"$TEST_COMMAND_LOG"
if [[ "${1:-}" == "--context" && "${3:-}" == "config" && "${4:-}" == "current-context" ]] ||
   [[ "${1:-}" == "config" && "${2:-}" == "current-context" ]]; then
  echo test-context
fi
exit 0
EOF
  chmod +x "$fake_bin/helm" "$fake_bin/kubectl"

  output=$(
    PATH="$fake_bin:$PATH" \
      TEST_COMMAND_LOG="$log" \
      ARCHESTRA_ADMIN_EMAIL=admin@example.com \
      ARCHESTRA_ADMIN_PASSWORD=test-password \
      ARCHESTRA_FRONTEND_URL=https://archestra.example.com \
      ARCHESTRA_API_BASE_URL='https://api.example.com,https://api.internal.example.com' \
      KUBE_CONTEXT=ack-test \
      DRY_RUN=true \
      bash "$script"
  )

  assert_contains "$output" "DRY_RUN=true"
  assert_contains "$(<"$log")" "helm --kube-context ack-test upgrade --install archestra-platform"
  assert_contains "$(<"$log")" "--version 1.4.12"
  assert_contains "$(<"$log")" "--namespace archestra"
  assert_contains "$(<"$log")" "archestra.imageTag=1.4.12"
  assert_contains "$(<"$log")" "--create-namespace"
  assert_contains "$(<"$log")" "--wait"
  assert_contains "$(<"$log")" "--timeout 15m"
  assert_contains "$(<"$log")" "--kube-context ack-test"
  assert_contains "$(<"$log")" "archestra.env.ARCHESTRA_FRONTEND_URL=https://archestra.example.com"
  assert_contains "$(<"$log")" "archestra.env.ARCHESTRA_API_BASE_URL=https://api.example.com"
  assert_contains "$(<"$log")" "https://api.internal.example.com"
  if grep -q "kubectl create secret\|kubectl apply" "$log"; then
    echo "dry-run must not mutate Kubernetes" >&2
    exit 1
  fi
}

run_real_mode_secret_test() {
  local fake_bin output log secret_manifest
  fake_bin=$(mktemp -d)
  log=$(mktemp)
  secret_manifest=$(mktemp)
  trap 'rm -rf "$fake_bin" "$log" "$secret_manifest"' RETURN

  cat >"$fake_bin/helm" <<'EOF'
#!/usr/bin/env bash
printf 'helm %s\n' "$*" >>"$TEST_COMMAND_LOG"
exit 0
EOF
  cat >"$fake_bin/kubectl" <<'EOF'
#!/usr/bin/env bash
printf 'kubectl %s\n' "$*" >>"$TEST_COMMAND_LOG"
if [[ "${1:-}" == "config" && "${2:-}" == "current-context" ]]; then
  echo test-context
elif [[ "${1:-}" == "-n" && "${2:-}" == "archestra" && "${3:-}" == "apply" ]]; then
  cat >"$TEST_SECRET_MANIFEST"
fi
exit 0
EOF
  chmod +x "$fake_bin/helm" "$fake_bin/kubectl"

  output=$(
    PATH="$fake_bin:$PATH" \
      TEST_COMMAND_LOG="$log" \
      TEST_SECRET_MANIFEST="$secret_manifest" \
      ARCHESTRA_ADMIN_EMAIL=admin@example.com \
      ARCHESTRA_ADMIN_PASSWORD='secret value' \
      DRY_RUN=false \
      bash "$script"
  )

  assert_contains "$output" "Archestra deployed"
  assert_contains "$(<"$secret_manifest")" "name: archestra-bootstrap"
  assert_contains "$(<"$secret_manifest")" "ARCHESTRA_AUTH_ADMIN_PASSWORD: c2VjcmV0IHZhbHVl"
  assert_contains "$(<"$log")" "kubectl -n archestra rollout status deployment/archestra-platform --timeout=15m"
  assert_contains "$(<"$log")" "kubectl -n archestra rollout status deployment/archestra-platform-worker --timeout=15m"
  assert_contains "$(<"$log")" "kubectl -n archestra wait --for=condition=complete job/archestra-platform-migrate --timeout=15m"
  assert_contains "$(<"$log")" "--namespace archestra"
  if grep -q "secret value" "$log"; then
    echo "admin password must not appear in kubectl arguments" >&2
    exit 1
  fi
}

run_missing_admin_email_test
run_missing_admin_password_test
run_missing_helm_test
run_missing_kubectl_test
run_dry_run_command_test
run_real_mode_secret_test
echo "deploy.sh tests passed"
