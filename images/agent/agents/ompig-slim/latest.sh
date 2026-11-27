#!/usr/bin/env bash
# Prints ompig-slim's latest GitHub release, with GH_TOKEN when it is set.
set -euo pipefail
auth=()
[ -n "${GH_TOKEN:-}" ] && auth=(-H "Authorization: Bearer $GH_TOKEN")
curl --fail --silent --show-error --retry 3 ${auth[@]+"${auth[@]}"} \
  "https://api.github.com/repos/tmih06/ompig-slim/releases/latest" |
  jq -er '.tag_name | sub("^v";"")'
