#!/usr/bin/env bash
# sbx — one-shot: claim a warm sandbox and drop into its bash.
#
# Usage:  sbx [claim-name] [--keep]
#   claim-name   name for the SandboxClaim (default: claimed-sandbox-<timestamp>-<pid>,
#                unique per run so every invocation gets a FRESH sandbox)
#   --keep       keep the sandbox after exiting bash (default: destroy it)
#
# Flow:  create a uniquely-named SandboxClaim -> pool hands you an
#        already-running sandbox (no cold start) -> interactive bash ->
#        on exit the claim is deleted, the sandbox is torn down, and the
#        pool restocks a fresh warm one. Run it twice = two different
#        sandboxes (even in parallel: each run claims its own).
#
# Env overrides: CTX (default kind-agent-sandbox), NS (default sandbox-demo),
#                POOL (default agent-pool), SBX_NO_EXEC=1 to skip the shell.
set -euo pipefail

CTX=${CTX:-kind-agent-sandbox}
NS=${NS:-sandbox-demo}
POOL=${POOL:-agent-pool}
NAME=claimed-sandbox-$(date +%Y%m%d%H%M%S)-$$
KEEP=0
CLAIM_CREATED=0
for a in "$@"; do
  case "$a" in
    --keep) KEEP=1 ;;
    *)      NAME=$a ;;
  esac
done

K="kubectl -n $NS --context $CTX"

# Safety net: if the script dies abnormally (Ctrl-C, closed terminal) the claim
# is still torn down, so uniquely-named claims can't pile up as orphans.
cleanup() {
  if [ "$KEEP" = 1 ] || [ "$CLAIM_CREATED" = 0 ]; then return; fi
  $K delete sandboxclaim "$NAME" --ignore-not-found >/dev/null 2>&1 || true
}
trap cleanup EXIT

# The factory must exist (apply demo-warmpool.yaml first, once).
if ! $K get sandboxwarmpool "$POOL" >/dev/null 2>&1; then
  echo "✗ no SandboxWarmPool '$POOL' in ns '$NS' — run: kubectl apply -f demo-warmpool.yaml" >&2
  exit 1
fi

# 1. Claim a fresh warm sandbox (unique name per run).
CLAIM_CREATED=1
$K apply -f - <<EOF
apiVersion: extensions.agents.x-k8s.io/v1beta1
kind: SandboxClaim
metadata:
  name: $NAME
  namespace: $NS
spec:
  warmPoolRef:
    name: $POOL
  lifecycle:
    shutdownPolicy: Delete
EOF

# 2. Wait for adoption (near-instant with a warm pool).
SBX=""
for _ in $(seq 1 60); do
  READY=$($K get sandboxclaim "$NAME" -o 'jsonpath={.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)
  SBX=$($K get sandboxclaim "$NAME" -o 'jsonpath={.status.sandbox.name}' 2>/dev/null || true)
  [ "$READY" = "True" ] && [ -n "$SBX" ] && break
  sleep 1
done
if [ -z "$SBX" ] || [ "$READY" != "True" ]; then
  echo "✗ claim '$NAME' not ready after 60s:" >&2
  $K describe sandboxclaim "$NAME" >&2
  exit 1
fi
echo "✓ claimed: $SBX (claim: $NAME, pool: $POOL)"
# (each run gets its own claim -> its own sandbox; re-running never jumps
# into a previous session's sandbox)

# 3. Shell.
if [ -n "${SBX_NO_EXEC:-}" ]; then
  echo "(SBX_NO_EXEC set — skipping shell)"
else
  $K exec -it "$SBX" -- bash
fi

# 4. Tear down (or keep).
if [ "$KEEP" = 1 ]; then
  echo "✓ kept — re-enter later with: kubectl -n $NS --context $CTX exec -it $SBX -- bash"
else
  $K delete sandboxclaim "$NAME"
  echo "✓ destroyed: sandbox + pod gone, pool is restocking a fresh warm one"
fi
