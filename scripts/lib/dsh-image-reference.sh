#!/usr/bin/env bash

# Use the same Docker reference parser as Server config validation before DSH
# deployment inputs are embedded in a release manifest or Kubernetes YAML.
is_valid_dsh_image_reference() {
  local image=$1
  local repository_root
  repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
  (cd "$repository_root/server" && go run ./cmd/validate-image-reference "$image") >/dev/null 2>&1
}
