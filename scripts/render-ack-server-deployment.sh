#!/usr/bin/env bash

set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source "$repository_root/scripts/lib/dsh-image-reference.sh"
manifest_path=${AGENT_RUNTIME_RELEASE_MANIFEST:-"$repository_root/release/agent-runtime-manifest.json"}
catalog_path=${AGENT_PACK_CATALOG_PATH:-"$repository_root/server/agentpack/catalog.generated.json"}
template_path=${ACK_SERVER_DEPLOYMENT_TEMPLATE:-"$repository_root/server/Deployment.yaml"}
rbac_template_path=${ACK_AGENT_RUNTIME_RBAC_TEMPLATE:-"$repository_root/deploy/k8s/ack-agent-runtime.yaml"}
output_path=${ACK_SERVER_DEPLOYMENT_OUTPUT:-}
server_only=false

usage() {
  cat >&2 <<'EOF'
Usage: scripts/render-ack-server-deployment.sh [--manifest PATH] [--output PATH] [--server-only]

Render one ACK release manifest from optional Agent runtime release metadata.
The output includes the cluster-scoped TokenReview RBAC required by the Server
and the namespace-scoped Server resources. Use --server-only only for an
existing workflow that applies deploy/k8s/ack-agent-runtime.yaml separately.
The caller must provide namespace, agent_service_account, and normal ACK
variables directly; managed Agent image tags and digests are both accepted,
but dsh_image_repo must include a sha256 digest.
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
    --server-only)
      server_only=true
      shift
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
if [[ "$server_only" != true ]]; then
  [[ -f "$rbac_template_path" ]] || { echo "ACK Agent RBAC template not found: $rbac_template_path" >&2; exit 1; }
fi

if [[ -f "$manifest_path" ]]; then
  command -v jq >/dev/null 2>&1 || { echo "jq is required when using a release manifest" >&2; exit 1; }
  jq empty "$manifest_path" >/dev/null || { echo "invalid Agent runtime release manifest: $manifest_path" >&2; exit 1; }
  [[ -f "$catalog_path" ]] || { echo "Server Agent Pack Catalog not found: $catalog_path" >&2; exit 1; }
  manifest_catalog_digest=$(jq -r '.catalog_digest // empty' "$manifest_path")
  [[ "$manifest_catalog_digest" =~ ^[0-9a-f]{64}$ ]] || {
    echo "release manifest has no valid catalog_digest: $manifest_path" >&2
    exit 1
  }
  server_catalog_digest=$(sha256sum "$catalog_path" | awk '{print $1}')
  if [[ "$manifest_catalog_digest" != "$server_catalog_digest" ]]; then
    echo "release manifest Catalog digest does not match Server Catalog: manifest=$manifest_catalog_digest server=$server_catalog_digest" >&2
    exit 1
  fi
  image_repo=${image_repo:-$(jq -r '.images.server // empty' "$manifest_path")}
  wechat_article_agent_image_repo=${wechat_article_agent_image_repo:-$(jq -r '.images["wechat-article"] // empty' "$manifest_path")}
  wechat_picture_agent_image_repo=${wechat_picture_agent_image_repo:-$(jq -r '.images["wechat-picture"] // empty' "$manifest_path")}
  seednote_agent_image_repo=${seednote_agent_image_repo:-$(jq -r '.images.seednote // empty' "$manifest_path")}
  montage_agent_image_repo=${montage_agent_image_repo:-$(jq -r '.images.montage // empty' "$manifest_path")}
  moments_agent_image_repo=${moments_agent_image_repo:-$(jq -r '.images.moments // empty' "$manifest_path")}
  ecommerce_agent_image_repo=${ecommerce_agent_image_repo:-$(jq -r '.images.ecommerce // empty' "$manifest_path")}
  content_planning_agent_image_repo=${content_planning_agent_image_repo:-$(jq -r '.images["content-planning"] // empty' "$manifest_path")}
  profile_analysis_agent_image_repo=${profile_analysis_agent_image_repo:-$(jq -r '.images["profile-analysis"] // empty' "$manifest_path")}
  feedback_analysis_agent_image_repo=${feedback_analysis_agent_image_repo:-$(jq -r '.images["feedback-analysis"] // empty' "$manifest_path")}
  whiteboard_animation_agent_image_repo=${whiteboard_animation_agent_image_repo:-$(jq -r '.images["whiteboard-animation"] // empty' "$manifest_path")}
  hypit_agent_image_repo=${hypit_agent_image_repo:-$(jq -r '.images.hypit // empty' "$manifest_path")}
  local_life_agent_image_repo=${local_life_agent_image_repo:-$(jq -r '.images["local-life"] // empty' "$manifest_path")}
  dsh_image_repo=${dsh_image_repo:-$(jq -r '.images.dsh // empty' "$manifest_path")}
fi

: "${image_repo:?image_repo must be set}"
: "${namespace:?namespace must be set}"
: "${agent_service_account:?agent_service_account must be set (must match claude.kubernetes.service_account and the ACK RBAC manifest)}"
: "${dsh_image_repo:?dsh_image_repo must be set when the Server config uses ANBAN_DSH_IMAGE}"

if ! is_valid_dsh_image_reference "$dsh_image_repo"; then
  echo "dsh_image_repo must be a valid immutable sha256 image reference without whitespace" >&2
  exit 1
fi

export image_repo
export namespace
export agent_service_account
export wechat_article_agent_image_repo wechat_picture_agent_image_repo
export seednote_agent_image_repo montage_agent_image_repo moments_agent_image_repo
export ecommerce_agent_image_repo profile_analysis_agent_image_repo
export feedback_analysis_agent_image_repo whiteboard_animation_agent_image_repo
export content_planning_agent_image_repo=${content_planning_agent_image_repo:-}
export hypit_agent_image_repo local_life_agent_image_repo
export dsh_image_repo

render_template() {
  local path=$1
  local body rendered template_variable
  body=$(<"$path")
  while IFS= read -r template_variable; do
    [[ -z "$template_variable" ]] && continue
    if [[ "$template_variable" == "content_planning_agent_image_repo" ]]; then continue; fi
    if [[ -z "${!template_variable:-}" ]]; then
      echo "ACK deployment variable is unset or empty: $template_variable" >&2
      return 1
    fi
  done < <(envsubst -v "$body")
  rendered=$(envsubst <<<"$body")
  if grep -Fq '${' <<<"$rendered"; then
    echo "ACK deployment still contains unresolved placeholders: $path" >&2
    return 1
  fi
  printf '%s\n' "$rendered"
}

server_rendered=$(render_template "$template_path")
if [[ "$server_only" == true || ( "$server_rendered" == *"kind: ClusterRole"* && "$server_rendered" == *"name: creator-server-tokenreview"* ) ]]; then
  rendered=$server_rendered
else
  rbac_rendered=$(render_template "$rbac_template_path")
  rendered=$(printf '%s\n---\n%s' "$rbac_rendered" "$server_rendered")
fi

if [[ -n "$output_path" ]]; then
  mkdir -p "$(dirname "$output_path")"
  printf '%s\n' "$rendered" >"$output_path"
  echo "ACK deployment rendered: $output_path"
else
  printf '%s\n' "$rendered"
fi
