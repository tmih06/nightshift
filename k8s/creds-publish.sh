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
# Where the master logins come from:
#   (default)    this machine's $HOME, where claude, codex, and agy logged in
#   --cpa URL    a CLIProxyAPI instance (k8s/cpa.yaml). You log accounts in
#                through its web page; it keeps the tokens fresh and this
#                script reads them over its management API.
#
# Usage:  k8s/creds-publish.sh [--exercise] [--watch SECONDS] [--cpa URL]
#         k8s/creds-publish.sh --import PATH...
#   --exercise     run each agent once first so an expiring access token is
#                  refreshed before it is published (not needed with --cpa)
#   --watch N      publish again every N seconds (implies --exercise unless
#                  --cpa). Access tokens expire (Antigravity's in about an
#                  hour, Claude's in about eight), so keep this running.
#   --import PATH  copy login files from this machine into the creds-refresher
#                  pod's home (paths must be under $HOME), then stop
#
# Env:  NIGHTSHIFT_CONTEXT  kubeconfig context for any Kubernetes provider
#                           (default kind-agent-sandbox; CTX and KUBE_CONTEXT
#                           are still read). Unset inside a pod.
#       PUBLISH_CONTEXTS    space-separated contexts to publish into, one
#                           login serving several clusters; "-" is the
#                           cluster this runs in. Default: the one above.
#       NS                  sandbox namespace (default nightshift)
#       REFRESHER           pod to --import into (default deploy/creds-refresher)
#       CPA_URL, CPA_MANAGEMENT_KEY, CPA_MANAGEMENT_KEY_FILE   for --cpa
set -euo pipefail

NS=${NS:-nightshift}
EGRESS_NS=nightshift-egress
REFRESHER=${REFRESHER:-deploy/creds-refresher}
HERE=$(cd "$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")" && pwd)
REGISTRY=${REGISTRY:-$HERE/creds-providers.json}

EXERCISE=0
WATCH=0
CPA_URL=${CPA_URL:-}
IMPORTS=()
while [ $# -gt 0 ]; do
  case "$1" in
    --host)      shift ;; # the master has always been this machine's $HOME
    --exercise)  EXERCISE=1; shift ;;
    --watch)     WATCH=$2; EXERCISE=1; shift 2 ;;
    --cpa)       CPA_URL=$2; shift 2 ;;
    --import)    shift; while [ $# -gt 0 ] && [[ $1 != --* ]]; do IMPORTS+=("$1"); shift; done ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
[ -z "$CPA_URL" ] || EXERCISE=0

# TARGETS are the clusters to publish into. "-" is the cluster this runs in
# (the pod's service account), or the kubeconfig's current context outside one.
TARGETS=()
if [ -n "${PUBLISH_CONTEXTS:-}" ]; then
  read -ra TARGETS <<<"$PUBLISH_CONTEXTS"
else
  ctx=${NIGHTSHIFT_CONTEXT:-${CTX:-${KUBE_CONTEXT:-}}}
  if [ -n "$ctx" ]; then
    TARGETS=("$ctx")
  elif [ -n "${KUBERNETES_SERVICE_HOST:-}" ]; then
    TARGETS=("-")
  else
    TARGETS=("kind-agent-sandbox")
  fi
fi

# use_target CONTEXT points K and EK at one cluster.
use_target() {
  local ctx_args=()
  if [ "$1" != "-" ]; then
    [ -z "${REMOTE_KUBECONFIG:-}" ] || ctx_args+=(--kubeconfig "$REMOTE_KUBECONFIG")
    ctx_args+=(--context "$1")
  fi
  K=(kubectl "${ctx_args[@]}" -n "$NS")
  EK=(kubectl "${ctx_args[@]}" -n "$EGRESS_NS")
}

if [ ${#IMPORTS[@]} -gt 0 ]; then
  use_target "${TARGETS[0]}"
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
    # stdin is the registry loop's: an agent that reads it would swallow the
    # remaining entries, so none after it would be exercised.
    # shellcheck disable=SC2086 # the registry command is a word list
    timeout 180 $cmd >/dev/null 2>&1 </dev/null || echo "warning: $(jq -r .id <<<"$entry") exercise failed" >&2
  done < <(jq -c '.providers[]' "$REGISTRY")
}

# cpa_home DIR rebuilds each agent's login file under DIR from the accounts
# logged in to CLIProxyAPI, so the usual staging runs on them. CLIProxyAPI
# keeps the tokens fresh; a token near expiry is refreshed first.
cpa_home() {
  local dir=$1 key list entry type name exp file cred active label secret
  key=${CPA_MANAGEMENT_KEY:-}
  [ -n "$key" ] || key=$(cat "${CPA_MANAGEMENT_KEY_FILE:-/dev/null}" 2>/dev/null || true)
  [ -n "$key" ] || { echo "--cpa needs CPA_MANAGEMENT_KEY or CPA_MANAGEMENT_KEY_FILE" >&2; return 1; }
  cpa() { curl -fsS -m 30 -H "Authorization: Bearer $key" "$@"; }
  list=$(cpa "$CPA_URL/v8/management/credentials") || { echo "cannot reach CLIProxyAPI at $CPA_URL" >&2; return 1; }
  # Which account each agent's sandboxes use is set in the Accounts tab and
  # kept in the nightshift-accounts ConfigMap ({"claude": "<credential name>"}).
  use_target "${TARGETS[0]}"
  active=$("${K[@]}" get configmap nightshift-accounts -o jsonpath='{.data.active}' 2>/dev/null || true)
  jq -e type >/dev/null 2>&1 <<<"$active" || active='{}'
  while IFS= read -r entry; do
    type=$(jq -r '.cpa.type // empty' <<<"$entry")
    [ -n "$type" ] || continue
    # The chosen account when it is still enabled, else the first enabled one.
    name=$(jq -r --arg t "$type" --argjson a "$active" '
      [.files[] | select(.provider == $t and (.disabled | not))] | sort_by(.name) as $ok
      | ([$ok[] | select(.name == $a[$t])] + $ok) | .[0].name // empty' <<<"$list")
    [ -n "$name" ] || continue
    exp=$(jq -r --arg n "$name" '.files[] | select(.name == $n) | .access_token_expires_at // empty' <<<"$list")
    if [ -n "$exp" ] && [ "$(date -d "$exp" +%s 2>/dev/null || echo 0)" -lt $(( $(date +%s) + 600 )) ]; then
      cpa -X POST -H 'Content-Type: application/json' -d "$(jq -cn --arg n "$name" '{name: $n}')" \
        "$CPA_URL/v8/management/credentials/refresh" >/dev/null || echo "warning: refreshing $name failed" >&2
    fi
    cred=$(cpa --get --data-urlencode "name=$name" "$CPA_URL/v8/management/credentials/download") \
      || { echo "warning: could not download $name" >&2; continue; }
    file=$dir/$(jq -r .file <<<"$entry")
    mkdir -p "$(dirname "$file")"
    jq "$(jq -r .cpa.build <<<"$entry")" <<<"$cred" > "$file"
    chmod 600 "$file"
    secret=$(jq -r .secret <<<"$entry")
    label=$(jq -r --arg n "$name" '.files[] | select(.name == $n) | (.email // .account // .name)' <<<"$list")
    mkdir -p "$work/labels"
    printf '%s' "$label" > "$work/labels/$secret"
    jq -cn --arg t "$type" --arg n "$name" --arg l "$label" --arg e "$exp" \
      '{type: $t, name: $n, label: $l, expires: $e}' >> "$work/selected.jsonl"
  done < <(jq -c '.providers[]' "$REGISTRY")
}

# scale_pools keeps a warm sandbox only for agents with a login to hand out.
# Every other pool is scaled to zero. Applying k8s/warmpool.yaml sets them
# back to one, so run this script after it.
scale_pools() {
  local pool want secret
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

# publish_to CONTEXT writes the staged logins into one cluster.
publish_to() {
  use_target "$1"
  echo "publishing to ${1/#-/this cluster}..."
  "${K[@]}" create secret generic agent-creds \
    --from-file=creds.tar="$work/creds.tar" \
    --dry-run=client -o yaml | "${K[@]}" apply -f - >/dev/null

  local pairs="" secret label
  while IFS= read -r secret; do
    [ -s "$work/tokens/$secret" ] || continue
    pairs="$pairs\"REAL_$secret\":\"$(base64 -w0 < "$work/tokens/$secret")\","
    # The proxy tags usage with this label; without CLIProxyAPI it is the machine.
    label=$(cat "$work/labels/$secret" 2>/dev/null || printf 'this machine')
    pairs="$pairs\"LABEL_$secret\":\"$(printf '%s' "$label" | base64 -w0)\","
    echo "  real $secret ($label) -> egress-secrets (proxy reloads it, no restart)"
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
  publish_status
}

# publish_status records what was published, for the Accounts tab.
publish_status() {
  local status
  status=$(jq -cs --arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '{published_at: $at, accounts: (map({key: .type, value: {name, label, expires}}) | from_entries)}' \
    "$work/selected.jsonl" 2>/dev/null) || status="{\"published_at\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"accounts\":{}}"
  if "${K[@]}" get configmap nightshift-accounts >/dev/null 2>&1; then
    "${K[@]}" patch configmap nightshift-accounts --type=merge \
      -p "$(jq -cn --arg s "$status" '{data: {status: $s}}')" >/dev/null
  else
    "${K[@]}" create configmap nightshift-accounts --from-literal=status="$status" >/dev/null
  fi
}

publish() {
  if [ "$(tar -tf "$work/creds.tar" 2>/dev/null | grep -c .)" -le 1 ]; then
    echo "no credential files found; nothing published" >&2
    echo "log in first (see k8s/creds-providers.json for the agents), then run this again" >&2
    return 1
  fi
  local target failed=0
  for target in "${TARGETS[@]}"; do
    publish_to "$target" || { echo "publishing to $target failed" >&2; failed=1; }
  done

  echo "done. Files published:"
  tar -tf "$work/creds.tar" | grep -v '/$' | sed 's|^\./||; s|^|  ~/|'
  echo
  echo "New sandbox pods pick these up at boot. Existing warm pods do not;"
  echo "delete unclaimed pool pods by name so the pools restock fresh ones."
  return "$failed"
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
chmod 700 "$work"

run_once() {
  local src=$HOME
  rm -rf "${work:?}/labels" "${work:?}/selected.jsonl" "${work:?}/tokens" "${work:?}/stage" "${work:?}/creds.tar"
  : > "$work/selected.jsonl"
  if [ -n "$CPA_URL" ]; then
    rm -rf "${work:?}/home"
    mkdir -p "$work/home"
    cpa_home "$work/home" || return 1
    src=$work/home
  elif [ "$EXERCISE" = 1 ]; then
    echo "exercising the logins (refresh if due)..."
    exercise
  fi
  sh "$HERE/creds-stage.sh" "$src" "$work/stage" "$work/tokens" "$REGISTRY"
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
