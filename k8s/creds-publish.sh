#!/usr/bin/env bash
# creds-publish — log in once, then every sandbox boots logged in.
#
# Reads the master logins of every agent in k8s/creds-providers.json, swaps
# each real access token for a placeholder (k8s/creds-stage.sh), packs the
# placeholder copies into the agent-creds Secret that sandbox templates untar
# into $HOME at boot, and writes the REAL access tokens into
# nightshift-egress's egress-secrets. The egress proxy swaps the placeholder
# for the real token on each agent's API host and re-reads the files without a
# restart. No real credential ever enters a sandbox, and no refresh token
# leaves the master. It also keeps a warm sandbox only for agents that have a
# login.
#
# The master is whatever $HOME this runs in: your laptop, or the
# creds-refresher pod (k8s/creds.yaml), which runs it in a loop inside the
# cluster. Inside a pod it uses the pod's service account.
#
# Usage:  k8s/creds-publish.sh [--exercise] [--watch SECONDS]
#         k8s/creds-publish.sh --import PATH...
#   --exercise     run each agent once first so an expiring access token is
#                  refreshed before it is published
#   --watch N      publish again every N seconds, exercising first (implies
#                  --exercise). Access tokens expire (Antigravity's in about
#                  an hour, Claude's in about eight), so keep this running.
#   --import PATH  copy login files from this machine into the creds-refresher
#                  pod's home (paths must be under $HOME), then stop
#
# Env:  NIGHTSHIFT_CONTEXT  kubeconfig context for any Kubernetes provider
#                           (default kind-agent-sandbox; CTX and KUBE_CONTEXT
#                           are still read). Unset inside a pod.
#       NS                  sandbox namespace (default nightshift)
#       REFRESHER           pod to --import into (default deploy/creds-refresher)
set -euo pipefail

NS=${NS:-nightshift}
EGRESS_NS=nightshift-egress
REFRESHER=${REFRESHER:-deploy/creds-refresher}
HERE=$(cd "$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")" && pwd)
REGISTRY=${REGISTRY:-$HERE/creds-providers.json}

CTX_ARGS=()
if [ -n "${NIGHTSHIFT_CONTEXT:-${CTX:-${KUBE_CONTEXT:-}}}" ]; then
  CTX_ARGS=(--context "${NIGHTSHIFT_CONTEXT:-${CTX:-${KUBE_CONTEXT:-}}}")
elif [ -z "${KUBERNETES_SERVICE_HOST:-}" ]; then
  CTX_ARGS=(--context kind-agent-sandbox)
fi
K=(kubectl "${CTX_ARGS[@]}" -n "$NS")
EK=(kubectl "${CTX_ARGS[@]}" -n "$EGRESS_NS")

EXERCISE=0
WATCH=0
IMPORTS=()
while [ $# -gt 0 ]; do
  case "$1" in
    --host)      shift ;; # the master has always been this machine's $HOME
    --exercise)  EXERCISE=1; shift ;;
    --watch)     WATCH=$2; EXERCISE=1; shift 2 ;;
    --import)    shift; while [ $# -gt 0 ] && [[ $1 != --* ]]; do IMPORTS+=("$1"); shift; done ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ ${#IMPORTS[@]} -gt 0 ]; then
  for f in "${IMPORTS[@]}"; do
    rel=${f/#"$HOME"/}
    rel=${rel#/}
    [ "$rel" != "$f" ] || { echo "--import path must be under \$HOME: $f" >&2; exit 2; }
    [ -f "$f" ] || { echo "file not found: $f" >&2; exit 2; }
    echo "importing $f -> $REFRESHER:~/$rel"
    remote="mkdir -p \"\$HOME/\$(dirname \"\$1\")\" && cat > \"\$HOME/\$1\" && chmod 600 \"\$HOME/\$1\""
    "${K[@]}" exec -i "$REFRESHER" -- sh -c "$remote" _ "$rel" < "$f"
  done
  exit 0
fi

# jwt_expiring TOKEN: true when TOKEN's exp is within an hour, or unreadable.
jwt_expiring() {
  local payload exp
  payload=$(printf '%s' "$1" | cut -d. -f2 | tr '_-' '/+')
  while [ $(( ${#payload} % 4 )) -ne 0 ]; do payload="$payload="; done
  exp=$(printf '%s' "$payload" | base64 -d 2>/dev/null | jq -r '.exp // empty' 2>/dev/null || true)
  [ -z "$exp" ] || [ "$exp" -lt $(( $(date +%s) + 3600 )) ]
}

# exercise refreshes whichever logins are close to expiring.
exercise() {
  local entry file cmd jwt tok
  while IFS= read -r entry; do
    file=$HOME/$(jq -r .file <<<"$entry")
    cmd=$(jq -r '.exercise // empty' <<<"$entry")
    [ -f "$file" ] || continue
    [ -n "$cmd" ] || continue
    command -v "${cmd%% *}" >/dev/null 2>&1 || continue
    jwt=$(jq -r '.exercise_when_jwt_expiring // empty' <<<"$entry")
    if [ -n "$jwt" ]; then
      tok=$(jq -r "$jwt // empty" "$file" 2>/dev/null || true)
      jwt_expiring "$tok" || continue
    fi
    # shellcheck disable=SC2086 # the registry command is a word list
    # stdin is the registry loop's: an agent that reads it would swallow the
    # remaining entries, so none after it would be exercised.
    timeout 180 $cmd >/dev/null 2>&1 </dev/null || echo "warning: $(jq -r .id <<<"$entry") exercise failed" >&2
  done < <(jq -c '.providers[]' "$REGISTRY")
}

# scale_pools keeps a warm sandbox only for agents with a login to hand out.
# Every other pool is scaled to zero. Applying k8s/warmpool.yaml sets them
# back to one, so run this script after it.
scale_pools() {
  local pool want
  while IFS= read -r pool; do
    want=0
    while IFS= read -r secret; do
      [ -s "$work/tokens/$secret" ] && want=1
    done < <(jq -r --arg p "$pool" '.providers[] | select(.pool == $p) | .secret' "$REGISTRY")
    "${K[@]}" patch sandboxwarmpool "$pool" --type=merge \
      -p "{\"spec\":{\"replicas\":$want}}" >/dev/null
    echo "  $pool: $want warm"
  done < <("${K[@]}" get sandboxwarmpool -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
}

publish() {
  if [ "$(tar -tf "$work/creds.tar" 2>/dev/null | grep -c .)" -le 1 ]; then
    echo "no credential files found; nothing published" >&2
    echo "log in first (see k8s/creds-providers.json for the agents), then run this again" >&2
    return 1
  fi

  echo "publishing agent-creds secret..."
  "${K[@]}" create secret generic agent-creds \
    --from-file=creds.tar="$work/creds.tar" \
    --dry-run=client -o yaml | "${K[@]}" apply -f - >/dev/null

  local pairs="" secret
  while IFS= read -r secret; do
    [ -s "$work/tokens/$secret" ] || continue
    pairs="$pairs\"REAL_$secret\":\"$(base64 -w0 < "$work/tokens/$secret")\","
    echo "  real $secret -> egress-secrets (proxy reloads it, no restart)"
  done < <(jq -r '.providers[].secret' "$REGISTRY")
  if [ -n "$pairs" ]; then
    # Merge into the existing Secret so the API keys setup seeded survive.
    if "${EK[@]}" get secret egress-secrets >/dev/null 2>&1; then
      "${EK[@]}" patch secret egress-secrets --type=merge \
        -p "{\"data\":{${pairs%,}}}" >/dev/null
    else
      "${EK[@]}" apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: egress-secrets
  namespace: $EGRESS_NS
data: {${pairs%,}}
EOF
    fi
  fi
  scale_pools

  echo "done. Files published:"
  tar -tf "$work/creds.tar" | grep -v '/$' | sed 's|^\./||; s|^|  ~/|'
  echo
  echo "New sandbox pods pick these up at boot. Existing warm pods do not;"
  echo "delete unclaimed pool pods by name so the pools restock fresh ones."
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
chmod 700 "$work"

run_once() {
  if [ "$EXERCISE" = 1 ]; then
    echo "exercising the logins (refresh if due)..."
    exercise
  fi
  rm -rf "$work/tokens" "$work/stage" "$work/creds.tar"
  sh "$HERE/creds-stage.sh" "$HOME" "$work/stage" "$work/tokens" "$REGISTRY"
  tar -cf "$work/creds.tar" -C "$work/stage" .
  publish
}

if [ "$WATCH" -gt 0 ]; then
  while true; do
    run_once || echo "publish failed; retrying in ${WATCH}s" >&2
    sleep "$WATCH"
  done
else
  run_once
fi
