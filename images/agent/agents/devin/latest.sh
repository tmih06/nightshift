#!/usr/bin/env bash
# Prints Devin CLI's current release from Cognition's release manifest.
set -euo pipefail
curl --fail --silent --show-error --retry 3 \
  "https://static.devin.ai/cli/current/manifest.json" | jq -er .version
