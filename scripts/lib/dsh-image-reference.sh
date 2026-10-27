#!/usr/bin/env bash

# Keep DSH deployment inputs syntactically valid before they are embedded in a
# release manifest or Kubernetes YAML. Bare names, tags, and digests are all
# valid Docker references; the Server repeats this check with the Docker parser.
is_valid_dsh_image_reference() {
  local image=$1
  local component='[a-z0-9]+(([._]|__|-+)[a-z0-9]+)*'
  local domain='[a-z0-9]+([.-][a-z0-9]+)*(:[0-9]+)?'
  local name="(${domain}/)?${component}(/${component})*"
  local tag='(:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?'
  local digest='(@[A-Za-z][A-Za-z0-9]*([-_+.][A-Za-z][A-Za-z0-9]*)*:[A-Fa-f0-9]{32,})?'

  [[ "$image" =~ ^${name}${tag}${digest}$ && ! "$image" =~ [[:space:]] ]]
}
