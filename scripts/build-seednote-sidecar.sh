#!/bin/sh

set -eu

source_repo=${SEEDNOTE_SIDECAR_SOURCE_REPO:-https://github.com/xpzouying/xiaohongshu-mcp.git}
source_commit=${SEEDNOTE_SIDECAR_SOURCE_COMMIT:-}
target_image=${SEEDNOTE_SIDECAR_IMAGE:-}
target_platform=${SEEDNOTE_SIDECAR_PLATFORM:-linux/amd64}
push_image=${SEEDNOTE_SIDECAR_PUSH:-0}

case "$source_commit" in
  *[!0-9a-f]* | "")
    echo "error: SEEDNOTE_SIDECAR_SOURCE_COMMIT must be a lowercase 40-character Git commit" >&2
    exit 1
    ;;
esac
if [ "${#source_commit}" -ne 40 ]; then
  echo "error: SEEDNOTE_SIDECAR_SOURCE_COMMIT must be a lowercase 40-character Git commit" >&2
  exit 1
fi
if [ -z "$target_image" ]; then
  echo "error: SEEDNOTE_SIDECAR_IMAGE is required" >&2
  exit 1
fi
image_name=${target_image##*/}
case "$image_name" in
  *:latest)
    echo "error: SEEDNOTE_SIDECAR_IMAGE must not use latest" >&2
    exit 1
    ;;
  *:*) ;;
  *)
    echo "error: SEEDNOTE_SIDECAR_IMAGE must include an explicit non-latest tag" >&2
    exit 1
    ;;
esac
case "$push_image" in
  0 | 1) ;;
  *)
    echo "error: SEEDNOTE_SIDECAR_PUSH must be 0 or 1" >&2
    exit 1
    ;;
esac

build_dir=$(mktemp -d)
trap 'rm -rf "$build_dir"' EXIT HUP INT TERM

git -C "$build_dir" init -q
git -C "$build_dir" remote add origin "$source_repo"
git -C "$build_dir" fetch --depth 1 origin "$source_commit"
git -C "$build_dir" checkout -q --detach FETCH_HEAD

actual_commit=$(git -C "$build_dir" rev-parse HEAD)
if [ "$actual_commit" != "$source_commit" ]; then
  echo "error: fetched Seednote commit $actual_commit, expected $source_commit" >&2
  exit 1
fi

docker build \
  --platform "$target_platform" \
  --build-arg "VERSION=$source_commit" \
  --label "org.opencontainers.image.source=$source_repo" \
  --label "org.opencontainers.image.revision=$source_commit" \
  -t "$target_image" \
  "$build_dir"

if [ "$push_image" -eq 1 ]; then
  push_output=$build_dir/docker-push.out
  docker push "$target_image" | tee "$push_output"
  digest=$(sed -n 's/^.*digest: \(sha256:[0-9a-f]\{64\}\).*$/\1/p' "$push_output" | tail -1)
  if [ -z "$digest" ]; then
    echo "error: docker push did not return an image digest" >&2
    exit 1
  fi
  printf '%s@%s\n' "${target_image%:*}" "$digest"
else
  printf 'Built %s from %s at %s\n' "$target_image" "$source_repo" "$source_commit"
fi
