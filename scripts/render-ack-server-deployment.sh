#!/usr/bin/env bash

set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
manifest_path=${AGENT_RUNTIME_RELEASE_MANIFEST:-"$repository_root/release/agent-runtime-manifest.json"}
template_path=${ACK_SERVER_DEPLOYMENT_TEMPLATE:-"$repository_root/server/Deployment.yaml"}
output_path=${ACK_SERVER_DEPLOYMENT_OUTPUT:-}

usage() {
  cat >&2 <<'EOF'
Usage: scripts/render-ack-server-deployment.sh [--manifest PATH] [--output PATH]

Render server/Deployment.yaml from one immutable Agent runtime release
manifest. The caller must provide the normal ACK variables and image_repo;
image_repo must equal the Server digest recorded in the manifest.
EOF
}

while (($# > 0)); do
  case "$1" in
    --manifest)
      (($# >= 2)) || { usage; exit 2; }
      manifest_path=$2
      shift 2
      ;;
    --output)
      (($# >= 2)) || { usage; exit 2; }
      output_path=$2
      shift 2
      ;;
    -h|--help)
      usage >&1
      exit 0
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
command -v envsubst >/dev/null 2>&1 || { echo "envsubst is required" >&2; exit 1; }

[[ -f "$manifest_path" ]] || { echo "release manifest not found: $manifest_path" >&2; exit 1; }
[[ -f "$template_path" ]] || { echo "ACK deployment template not found: $template_path" >&2; exit 1; }

schema_version=$(jq -r '.schema_version // empty' "$manifest_path")
catalog_digest=$(jq -r '.catalog_digest // empty' "$manifest_path")
manifest_server_image=$(jq -r '.images.server // empty' "$manifest_path")
if [[ "$schema_version" != 1 || ! "$catalog_digest" =~ ^[0-9a-f]{64}$ || ! "$manifest_server_image" =~ @sha256:[0-9a-f]{64}$ ]]; then
  echo "invalid Agent runtime release manifest: $manifest_path" >&2
  exit 1
fi

: "${image_repo:?image_repo must be set to the Server image digest}"
if [[ "$image_repo" != "$manifest_server_image" ]]; then
  echo "ACK Server image differs from release manifest: image_repo=$image_repo manifest=$manifest_server_image" >&2
  exit 1
fi
if [[ ! "$image_repo" =~ @sha256:[0-9a-f]{64}$ ]]; then
  echo "image_repo must use image@sha256:<64 lowercase hex characters>: $image_repo" >&2
  exit 1
fi

wechat_article_agent_image_repo=$(jq -r '.images["wechat-article"] // empty' "$manifest_path")
wechat_picture_agent_image_repo=$(jq -r '.images["wechat-picture"] // empty' "$manifest_path")
seednote_agent_image_repo=$(jq -r '.images.seednote // empty' "$manifest_path")
montage_agent_image_repo=$(jq -r '.images.montage // empty' "$manifest_path")
moments_agent_image_repo=$(jq -r '.images.moments // empty' "$manifest_path")
ecommerce_agent_image_repo=$(jq -r '.images.ecommerce // empty' "$manifest_path")
profile_analysis_agent_image_repo=$(jq -r '.images["profile-analysis"] // empty' "$manifest_path")
feedback_analysis_agent_image_repo=$(jq -r '.images["feedback-analysis"] // empty' "$manifest_path")
whiteboard_animation_agent_image_repo=$(jq -r '.images["whiteboard-animation"] // empty' "$manifest_path")
hypit_agent_image_repo=$(jq -r '.images.hypit // empty' "$manifest_path")
for profile in wechat_article wechat_picture seednote montage moments ecommerce profile_analysis feedback_analysis whiteboard_animation hypit; do
  var_name="${profile}_agent_image_repo"
  image=${!var_name}
  if [[ ! "$image" =~ @sha256:[0-9a-f]{64}$ ]]; then
    echo "release manifest image $profile is missing or mutable: $image" >&2
    exit 1
  fi
done

export image_repo
export wechat_article_agent_image_repo wechat_picture_agent_image_repo
export seednote_agent_image_repo montage_agent_image_repo moments_agent_image_repo
export ecommerce_agent_image_repo profile_analysis_agent_image_repo
export feedback_analysis_agent_image_repo whiteboard_animation_agent_image_repo
export hypit_agent_image_repo

template_body=$(<"$template_path")
while IFS= read -r template_variable; do
  [[ -z "$template_variable" ]] && continue
  if [[ -z "${!template_variable:-}" ]]; then
    echo "ACK deployment variable is unset or empty: $template_variable" >&2
    exit 1
  fi
done < <(envsubst -v "$template_body")

if [[ -n "$output_path" ]]; then
  mkdir -p "$(dirname "$output_path")"
  envsubst <"$template_path" >"$output_path"
  if grep -Fq '${' "$output_path"; then
    echo "ACK deployment still contains unresolved placeholders: $output_path" >&2
    exit 1
  fi
  echo "ACK deployment rendered: $output_path"
else
  rendered=$(envsubst <"$template_path")
  if grep -Fq '${' <<<"$rendered"; then
    echo "ACK deployment still contains unresolved placeholders" >&2
    exit 1
  fi
  printf '%s\n' "$rendered"
fi
