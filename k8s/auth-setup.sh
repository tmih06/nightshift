#!/usr/bin/env bash
# auth-setup — install nightshift-auth, Nightshift's login service, on any
# Kubernetes cluster. Run k8s/egress-setup.sh first: the proxy and its
# namespace must exist. Then sign accounts in from the Accounts tab.
#
# Env:  NIGHTSHIFT_CONTEXT  kubeconfig context (default kind-agent-sandbox;
#                           KUBE_CONTEXT is still read)
#       AUTH_IMAGE          image. Unset on a kind context: build
#                           nightshift-auth:local and load it into kind.
#                           Unset elsewhere: ghcr.io/tmih06/nightshift-auth:latest.
#       AUTH_BUILD=1        build AUTH_IMAGE here and push it (needs docker
#                           login) instead of pulling it
set -euo pipefail

CONTEXT="${NIGHTSHIFT_CONTEXT:-${KUBE_CONTEXT:-kind-agent-sandbox}}"
NS=nightshift
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
case "${CONTEXT}" in
  kind-*) IS_KIND=1; CLUSTER="${KIND_CLUSTER:-${CONTEXT#kind-}}" ;;
  *)      IS_KIND=0; CLUSTER="" ;;
esac
if [ -n "${AUTH_IMAGE:-}" ]; then
  IMAGE="${AUTH_IMAGE}"
elif [ "${IS_KIND}" = 1 ]; then
  IMAGE="nightshift-auth:local"
else
  IMAGE="ghcr.io/tmih06/nightshift-auth:latest"
fi

k() { kubectl --context "${CONTEXT}" "$@"; }

if [ "${IS_KIND}" = 1 ] && [ -z "${AUTH_IMAGE:-}" ]; then
  echo "1/3 Building ${IMAGE} and loading it into kind cluster ${CLUSTER}..."
  docker build -f "${REPO_ROOT}/images/auth/Dockerfile" -t "${IMAGE}" "${REPO_ROOT}"
  kind load docker-image "${IMAGE}" --name "${CLUSTER}"
elif [ "${AUTH_BUILD:-0}" = 1 ]; then
  echo "1/3 Building and pushing ${IMAGE}..."
  docker build -f "${REPO_ROOT}/images/auth/Dockerfile" -t "${IMAGE}" "${REPO_ROOT}"
  docker push "${IMAGE}"
else
  echo "1/3 Using image ${IMAGE} (the cluster pulls it)."
fi

echo "2/3 Creating the API key if it does not exist..."
if ! k -n "${NS}" get secret nightshift-auth-key >/dev/null 2>&1; then
  k -n "${NS}" create secret generic nightshift-auth-key \
    --from-literal=key="$(openssl rand -hex 32)"
fi

echo "3/3 Deploying nightshift-auth..."
sed -E "s|^([[:space:]]*image:) ghcr.io/tmih06/nightshift-auth:latest\$|\\1 ${IMAGE}|" \
  "${SCRIPT_DIR}/auth.yaml" | k apply -f -
# A locally built image keeps its tag, so a pod already running it must
# restart to pick up a rebuild.
if [ "${IS_KIND}" = 1 ] && [ -z "${AUTH_IMAGE:-}" ]; then
  k -n "${NS}" rollout restart deploy/nightshift-auth
fi
k -n "${NS}" rollout status deploy/nightshift-auth --timeout=180s
echo "Done. Sign accounts in from the Accounts tab (make tui, then a)."
