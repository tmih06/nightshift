#!/bin/sh
# creds-stage — turn agent login files into sandbox-safe copies.
#
# Usage:  sh creds-stage.sh SRC_HOME STAGE_DIR TOKEN_DIR [REGISTRY]
#   SRC_HOME   home directory holding the master logins
#   STAGE_DIR  receives the sandbox-safe tree (packed into agent-creds)
#   TOKEN_DIR  receives one file per real access token, named after the
#              egress secret (CLAUDE_ACCESS_TOKEN, ...); creds-publish.sh
#              moves them into the egress proxy's secrets, never into a sandbox
#   REGISTRY   provider table (default: creds-providers.json beside this script)
#
# Every agent in the registry keeps its login's shape but carries a
# placeholder instead of the real access token, a far-future expiry so the
# agent never tries to refresh it, and no usable refresh token. The egress
# proxy swaps the placeholder for the real token on the agent's API hosts.
# Adding an agent is one registry entry; nothing here changes. Logins with no
# registry entry are never distributed. Needs jq.
set -eu

src=$1 stage=$2 tokens=$3
registry=${4:-$(dirname "$0")/creds-providers.json}
far='2099-01-01T00:00:00Z'
mkdir -p "$stage" "$tokens"
chmod 700 "$tokens"

# stage_provider ID: swap one registry entry's login, when it is present.
stage_provider() {
  entry=$(jq -c --arg id "$1" '.providers[] | select(.id == $id)' "$registry")
  field() { printf '%s' "$entry" | jq -r "$1"; }
  rel=$(field .file) secret=$(field .secret)
  in=$src/$rel out=$stage/$rel
  [ -f "$in" ] || return 0
  value=$(jq -r "$(field .access) // empty" "$in" 2>/dev/null || true)
  [ -n "$value" ] || return 0
  mkdir -p "$(dirname "$out")"
  (umask 077; printf '%s' "$value" > "$tokens/$secret")
  comp_json='{}'
  for ckey in $(printf '%s' "$entry" | jq -r '(.companions // {}) | keys[]'); do
    cpath=$(printf '%s' "$entry" | jq -r --arg k "$ckey" '.companions[$k]')
    cval=$(jq -r "$cpath // empty" "$in" 2>/dev/null || true)
    if [ -n "$cval" ]; then
      comp_json=$(printf '%s' "$comp_json" | jq -c --arg k "$ckey" --arg v "$cval" '.[$k] = $v')
    fi
  done
  if [ "$comp_json" != '{}' ]; then
    (umask 077; printf '%s' "$comp_json" > "$tokens/$secret.companions.json")
  fi
  jq --arg ph "nightshift:secret:$secret" --arg far "$far" "$(field .filter)" "$in" > "$out"
  chmod 600 "$out"
  # Extra files are static, not derived from the master: the sandbox-side
  # settings the proxy needs (for example, turning off WebSockets it cannot pass).
  for extra in $(printf '%s' "$entry" | jq -r '(.extra // {}) | keys[]'); do
    mkdir -p "$(dirname "$stage/$extra")"
    printf '%s' "$entry" | jq -r --arg k "$extra" '.extra[$k]' > "$stage/$extra"
    chmod 600 "$stage/$extra"
  done
}

# gh keeps its token as plain text in hosts.yml; the proxy swaps the
# placeholder for the real GITHUB_TOKEN on github.com and api.github.com.
stage_gh() {
  in=$src/.config/gh/hosts.yml out=$stage/.config/gh/hosts.yml
  [ -f "$in" ] || return 0
  mkdir -p "$(dirname "$out")"
  sed -E 's/^([[:space:]]*oauth_token:).*/\1 nightshift:secret:GITHUB_TOKEN/' "$in" > "$out"
  chmod 600 "$out"
}

for id in $(jq -r '.providers[].id' "$registry"); do
  stage_provider "$id"
done
stage_gh
