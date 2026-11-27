#!/usr/bin/env bash
# Installs Devin CLI $VERSION from its versioned release manifest, checked
# against the SHA-256 it publishes. The bundle keeps its bin/ and libexec/
# layout under /opt/devin; only the CLI is linked onto PATH.
set -euo pipefail
case "${TARGETARCH:-}" in
  amd64) target=x86_64-unknown-linux ;;
  arm64) target=aarch64-unknown-linux ;;
  *) echo "devin: unsupported TARGETARCH '${TARGETARCH:-}'" >&2; exit 1 ;;
esac
manifest=$(curl -fsSL "https://static.devin.ai/cli/${VERSION}/manifest.json")
[ "$(echo "$manifest" | jq -er .version)" = "$VERSION" ]
url=$(echo "$manifest" | jq -er --arg t "$target" '.platforms[$t].url')
sha256=$(echo "$manifest" | jq -er --arg t "$target" '.platforms[$t].sha256')
curl -fsSL "$url" -o /tmp/devin.tar.gz
echo "$sha256  /tmp/devin.tar.gz" | sha256sum -c -
mkdir -p /opt/devin
tar --no-same-owner -xzf /tmp/devin.tar.gz -C /opt/devin
ln -sf /opt/devin/bin/devin /usr/local/bin/devin
rm /tmp/devin.tar.gz
