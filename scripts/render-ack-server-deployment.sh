#!/usr/bin/env bash

set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
manifest_path=${AGENT_RUNTIME_RELEASE_MANIFEST:-"$repository_root/release/agent-runtime-manifest.json"}
template_path=${ACK_SERVER_DEPLOYMENT_TEMPLATE:-"$repository_root/server/Deployment.yaml"}
output_path=${ACK_SERVER_DEPLOYMENT_OUTPUT:-}

usage() {
  cat >&2 <<'EOF'
Usage: scripts/render-ack-server-deployment.sh [--manifest PATH] [--output PATH]

Render server/Deployment.yaml from optional Agent runtime release metadata.
The caller must provide agent_service_account and normal ACK variables directly;
image tags and digests are both accepted.
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

command -v envsubst >/dev/null 2>&1 || { echo "envsubst is required" >&2; exit 1; }

[[ -f "$template_path" ]] || { echo "ACK deployment template not found: $template_path" >&2; exit 1; }

if [[ -f "$manifest_path" ]]; then
  command -v jq >/dev/null 2>&1 || { echo "jq is required when using a release manifest" >&2; exit 1; }
  jq empty "$manifest_path" >/dev/null || { echo "invalid Agent runtime release manifest: $manifest_path" >&2; exit 1; }
  image_repo=${image_repo:-$(jq -r '.images.server // empty' "$manifest_path")}
  wechat_article_agent_image_repo=${wechat_article_agent_image_repo:-$(jq -r '.images["wechat-article"] // empty' "$manifest_path")}
  wechat_picture_agent_image_repo=${wechat_picture_agent_image_repo:-$(jq -r '.images["wechat-picture"] // empty' "$manifest_path")}
  seednote_agent_image_repo=${seednote_agent_image_repo:-$(jq -r '.images.seednote // empty' "$manifest_path")}
  montage_agent_image_repo=${montage_agent_image_repo:-$(jq -r '.images.montage // empty' "$manifest_path")}
  moments_agent_image_repo=${moments_agent_image_repo:-$(jq -r '.images.moments // empty' "$manifest_path")}
  ecommerce_agent_image_repo=${ecommerce_agent_image_repo:-$(jq -r '.images.ecommerce // empty' "$manifest_path")}
  profile_analysis_agent_image_repo=${profile_analysis_agent_image_repo:-$(jq -r '.images["profile-analysis"] // empty' "$manifest_path")}
  feedback_analysis_agent_image_repo=${feedback_analysis_agent_image_repo:-$(jq -r '.images["feedback-analysis"] // empty' "$manifest_path")}
  whiteboard_animation_agent_image_repo=${whiteboard_animation_agent_image_repo:-$(jq -r '.images["whiteboard-animation"] // empty' "$manifest_path")}
  hypit_agent_image_repo=${hypit_agent_image_repo:-$(jq -r '.images.hypit // empty' "$manifest_path")}
fi

: "${image_repo:?image_repo must be set}"
: "${agent_service_account:?agent_service_account must be set (must match claude.kubernetes.service_account and the ACK RBAC manifest)}"

export image_repo
export agent_service_account
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
