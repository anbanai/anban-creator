#!/usr/bin/env bash

set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
catalog_path=${AGENT_PACK_CATALOG_PATH:-"$repository_root/server/agentpack/catalog.generated.json"}
runtime_catalog_path=${AGENT_RUNTIME_CATALOG_PATH:-"$repository_root/harness/agent-pack-catalog.json"}
manifest_path=${AGENT_RUNTIME_RELEASE_MANIFEST:-"$repository_root/release/agent-runtime-manifest.json"}

usage() {
  cat >&2 <<'EOF'
Usage: scripts/agent-runtime-release.sh [--output PATH]

Image references are supplied through SERVER_IMAGE and one AGENT_IMAGE_* variable
for every managed runtime profile. Tags and digests are both accepted.
EOF
}

while (($# > 0)); do
  case "$1" in
    --output)
      (($# >= 2)) || { usage; exit 2; }
      manifest_path=$2
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

command -v jq >/dev/null 2>&1 || {
  echo "jq is required to write the release manifest" >&2
  exit 1
}

catalog_digest=$(sha256sum "$catalog_path" | awk '{print $1}')
runtime_catalog_digest=$(sha256sum "$runtime_catalog_path" | awk '{print $1}')
if [[ "$catalog_digest" != "$runtime_catalog_digest" ]]; then
  echo "Agent Pack Catalog files differ: server=$catalog_digest runtime=$runtime_catalog_digest" >&2
  exit 1
fi
if [[ ! "$catalog_digest" =~ ^[0-9a-f]{64}$ ]]; then
  echo "invalid Agent Pack Catalog digest: $catalog_digest" >&2
  exit 1
fi

: "${SERVER_IMAGE:?SERVER_IMAGE must be set}"

image_entries=(
  "server=$SERVER_IMAGE"
  "wechat-article=${AGENT_IMAGE_WECHAT_ARTICLE:?AGENT_IMAGE_WECHAT_ARTICLE must be set}"
  "wechat-picture=${AGENT_IMAGE_WECHAT_PICTURE:?AGENT_IMAGE_WECHAT_PICTURE must be set}"
  "seednote=${AGENT_IMAGE_SEEDNOTE:?AGENT_IMAGE_SEEDNOTE must be set}"
  "montage=${AGENT_IMAGE_MONTAGE:?AGENT_IMAGE_MONTAGE must be set}"
  "moments=${AGENT_IMAGE_MOMENTS:?AGENT_IMAGE_MOMENTS must be set}"
  "ecommerce=${AGENT_IMAGE_ECOMMERCE:?AGENT_IMAGE_ECOMMERCE must be set}"
  "profile-analysis=${AGENT_IMAGE_PROFILE_ANALYSIS:?AGENT_IMAGE_PROFILE_ANALYSIS must be set}"
  "feedback-analysis=${AGENT_IMAGE_FEEDBACK_ANALYSIS:?AGENT_IMAGE_FEEDBACK_ANALYSIS must be set}"
  "whiteboard-animation=${AGENT_IMAGE_WHITEBOARD_ANIMATION:?AGENT_IMAGE_WHITEBOARD_ANIMATION must be set}"
  "hypit=${AGENT_IMAGE_HYPIT:?AGENT_IMAGE_HYPIT must be set}"
)

images_json='{}'
for entry in "${image_entries[@]}"; do
  profile=${entry%%=*}
  image=${entry#*=}
  images_json=$(jq --arg key "$profile" --arg value "$image" '. + {($key): $value}' <<<"$images_json")
done

mkdir -p "$(dirname "$manifest_path")"
jq -n \
  --argjson schema_version 1 \
  --arg catalog_digest "$catalog_digest" \
  --argjson images "$images_json" \
  '{schema_version: $schema_version, catalog_digest: $catalog_digest, images: $images}' \
  >"$manifest_path"

echo "Agent runtime release manifest written: $manifest_path"
echo "Catalog digest: $catalog_digest"
