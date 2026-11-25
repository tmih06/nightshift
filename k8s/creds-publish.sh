#!/usr/bin/env bash
# creds-publish — harvest agent credential files from the creds holder pod,
# strip rotating refresh tokens, and pack them into the agent-creds Secret
# that sandbox templates untar into $HOME at boot.
#
# Usage:  k8s/creds-publish.sh [--exercise] [--from-host PATH ...]
#   (no flags)     harvest the known credential files from the creds pod
#   --exercise     run a trivial `claude -p` in the creds pod first, so an
#                  expired access token is refreshed before harvesting
#   --from-host P  import host file P into the creds pod master first, at the
#                  same home-relative path (e.g. --from-host ~/.codex/auth.json)
#
# Claude gets special treatment: copies carry the placeholder
# nightshift:secret:CLAUDE_ACCESS_TOKEN, a far-future expiry, and no refresh
# token, while the REAL access token is patched into nightshift-egress's
# egress-secrets and the egress proxy swaps it in on api.anthropic.com. No
# real credential ever enters a sandbox. Claude rotates refresh tokens on
# every use, so the master chain never leaves the creds pod. Access tokens
# live about 8h; re-run with --exercise when they expire (the exercise makes
# the master refresh first). Other agents' files pass through unchanged.
#
# Env overrides: CTX (default kind-agent-sandbox), NS (default nightshift),
#                CREDS_POD (default: deployment/creds)
set -euo pipefail

CTX=${CTX:-kind-agent-sandbox}
NS=${NS:-nightshift}
CREDS_POD=${CREDS_POD:-deployment/creds}

K=(kubectl -n "$NS" --context "$CTX")

# Credential files harvested relative to the sandbox home, space-separated.
# Each entry is a path; entries are skipped silently when absent.
KNOWN_PATHS=".claude/.credentials.json .codex/auth.json .gemini/oauth_creds.json .config/gh/hosts.yml .local/share/opencode/auth.json"

EXERCISE=0
HOST_FILES=()
while [ $# -gt 0 ]; do
  case "$1" in
    --exercise)  EXERCISE=1; shift ;;
    --from-host) HOST_FILES+=("$2"); shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

pod_name() {
  "${K[@]}" get "$CREDS_POD" -o jsonpath='{.status.readyReplicas}' >/dev/null 2>&1 \
    || { echo "creds pod not ready; apply k8s/creds.yaml first" >&2; exit 1; }
  "${K[@]}" get pods -l app=creds -o jsonpath='{.items[0].metadata.name}'
}

POD=$(pod_name)
[ -n "$POD" ] || { echo "no creds pod found" >&2; exit 1; }

# Stage 0: import host files into the master, keeping their home-relative path.
for f in ${HOST_FILES[@]+"${HOST_FILES[@]}"}; do
  rel=${f/#"$HOME"/}
  rel=${rel#/}
  [ "$rel" != "$f" ] || { echo "--from-host path must be under \$HOME: $f" >&2; exit 2; }
  [ -f "$f" ] || { echo "host file not found: $f" >&2; exit 2; }
  echo "importing $f -> creds pod $rel"
  "${K[@]}" exec "$POD" -- mkdir -p "/home/node/$(dirname "$rel")"
  cat "$f" | "${K[@]}" exec -i "$POD" -- sh -c "cat > /home/node/$rel && chmod 600 /home/node/$rel"
done

if [ "$EXERCISE" = 1 ]; then
  echo "exercising the master credentials (claude refresh if due)..."
  "${K[@]}" exec "$POD" -- timeout 120 claude -p "ping" --model haiku >/dev/null 2>&1 \
    || echo "warning: exercise failed; harvesting whatever is present" >&2
fi

# Stage inside the pod: copy known files into a home-relative tree, stripping
# Claude's refreshToken (jq is in the base image). Then read the tar out.
read -ra paths <<< "$KNOWN_PATHS"
echo "harvesting credentials from $POD..."
"${K[@]}" exec -i "$POD" -- sh -s -- "${paths[@]}" <<'EOF'
set -eu
stage=/tmp/credsdist
rm -rf "$stage"
mkdir -p "$stage"
strip_claude() {
  src=$1 dst=$2
  if jq -e '.claudeAiOauth.accessToken' "$src" >/dev/null 2>&1; then
    jq -r '.claudeAiOauth.accessToken' "$src" > /tmp/claude-access-token
    chmod 600 /tmp/claude-access-token
    jq '.claudeAiOauth.accessToken = "nightshift:secret:CLAUDE_ACCESS_TOKEN"
        | .claudeAiOauth.expiresAt = 1999999999999
        | del(.claudeAiOauth.refreshToken)' "$src" > "$dst"
  else
    cp "$src" "$dst"
  fi
}
for rel in "$@"; do
  src=/home/node/$rel
  [ -f "$src" ] || continue
  mkdir -p "$stage/$(dirname "$rel")"
  case "$rel" in
    .claude/.credentials.json) strip_claude "$src" "$stage/$rel" ;;
    *)                         cp "$src" "$stage/$rel" ;;
  esac
  chmod 600 "$stage/$rel"
done
rm -f /tmp/creds.tar
tar -cf /tmp/creds.tar -C "$stage" .
EOF

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
"${K[@]}" exec "$POD" -- cat /tmp/creds.tar > "$tmp"

if [ "$(tar -tf "$tmp" 2>/dev/null | grep -c .)" -le 1 ]; then
  echo "no credential files found in the creds pod; nothing published" >&2
  echo "log in first: kubectl -n $NS --context $CTX exec -it $POD -- claude login" >&2
  exit 1
fi

echo "publishing agent-creds secret..."
"${K[@]}" create secret generic agent-creds \
  --from-file=creds.tar="$tmp" \
  --dry-run=client -o yaml | "${K[@]}" apply -f -

token=$("${K[@]}" exec "$POD" -- sh -c 'cat /tmp/claude-access-token 2>/dev/null' || true)
token=$(printf '%s' "$token" | tr -d '[:space:]')
if [ -n "$token" ]; then
  echo "publishing real Claude access token to egress-secrets..."
  EK=(kubectl -n nightshift-egress --context "$CTX")
  b64=$(printf '%s' "$token" | base64)
  if "${EK[@]}" get secret egress-secrets >/dev/null 2>&1; then
    "${EK[@]}" patch secret egress-secrets \
      --type=merge -p "{\"data\":{\"REAL_CLAUDE_ACCESS_TOKEN\":\"$b64\"}}" >/dev/null
  else
    "${EK[@]}" create secret generic egress-secrets \
      --from-literal="REAL_CLAUDE_ACCESS_TOKEN=$token"
  fi
  "${EK[@]}" rollout restart deployment/egress-proxy >/dev/null
  echo "egress proxy restarted with the new token"
fi

echo "done. Files published:"
tar -tf "$tmp" | grep -v '/$' | sed 's|^\./||' | sed 's/^/  ~\//'
echo
echo "New sandbox pods pick these up at boot. Existing warm pods do not;"
echo "delete unclaimed pool pods by name so the pools restock fresh ones, e.g.:"
echo "  kubectl -n $NS --context $CTX delete pod pi-pool-XXXXX"
