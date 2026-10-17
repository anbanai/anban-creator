#!/usr/bin/env bash

# Keep DSH deployment inputs immutable and syntactically valid before they are
# embedded in a release manifest or Kubernetes YAML. The Server repeats this
# check with github.com/distribution/reference during configuration loading.
is_valid_dsh_image_reference() {
  local image=$1
  local component='[a-z0-9]+(([._]|__|-+)[a-z0-9]+)*'
  local domain='[a-z0-9]+([.-][a-z0-9]+)*(:[0-9]+)?'
  local name="(${domain}/)?${component}(/${component})*"
  local tag='(:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?'

  [[ "$image" =~ ^${name}${tag}@sha256:[a-f0-9]{64}$ && ! "$image" =~ [[:space:]] ]]
}
