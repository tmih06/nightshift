#!/usr/bin/env bash
# Prints the Antigravity CLI's current release from its update feed.
set -euo pipefail
curl --fail --silent --show-error --retry 3 \
  "https://antigravity-cli-auto-updater-974169037036.us-central1.run.app/manifests/linux_amd64.json" |
  jq -er .version
