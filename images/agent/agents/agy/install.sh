#!/usr/bin/env bash
# Installs Antigravity CLI $VERSION from its update feed, checksum-verified.
set -euo pipefail
case "${TARGETARCH:-}" in
  amd64) platform=linux_amd64 ;;
  arm64) platform=linux_arm64 ;;
  *) echo "agy: unsupported TARGETARCH '${TARGETARCH:-}'" >&2; exit 1 ;;
esac
manifest=$(curl -fsSL "https://antigravity-cli-auto-updater-974169037036.us-central1.run.app/manifests/${platform}.json")
[ "$(echo "$manifest" | jq -er .version)" = "$VERSION" ]
url=$(echo "$manifest" | jq -er .url)
sha512=$(echo "$manifest" | jq -er .sha512)
curl -fsSL "$url" -o /tmp/agy.tar.gz
echo "$sha512  /tmp/agy.tar.gz" | sha512sum -c -
tar -xzf /tmp/agy.tar.gz -C /usr/local/bin antigravity
mv /usr/local/bin/antigravity /usr/local/bin/agy
chmod +x /usr/local/bin/agy
rm /tmp/agy.tar.gz
