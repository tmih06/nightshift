#!/usr/bin/env bash
# creds-setup — install Nightshift's login surface on any Kubernetes cluster.
#
# Deploys CLIProxyAPI (the page where you sign accounts in) and the
# creds-refresher (which publishes them to sandboxes), then prints how to open
# the page. Run k8s/egress-setup.sh first: the proxy, its CA, and the
# sandbox-egress-env ConfigMap must exist.
#
# Env:  NIGHTSHIFT_CONTEXT  kubeconfig context (default kind-agent-sandbox;
#                           KUBE_CONTEXT is still read)
#       CPA_IMAGE           CLIProxyAPI image (default eceasy/cli-proxy-api:latest)
#       CREDS_IMAGE         refresher image. Unset on a kind context: build
#                           nightshift-creds:local and load it into kind.
#                           Unset elsewhere: ghcr.io/tmih06/nightshift-creds:latest.
#       CREDS_BUILD=1       build CREDS_IMAGE here and push it to its registry
#                           (needs docker login) instead of pulling it
set -euo pipefail

CONTEXT="${NIGHTSHIFT_CONTEXT:-${KUBE_CONTEXT:-kind-agent-sandbox}}"
NS=nightshift
CPA_IMAGE="${CPA_IMAGE:-eceasy/cli-proxy-api:latest}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
case "${CONTEXT}" in
  kind-*) IS_KIND=1; CLUSTER="${KIND_CLUSTER:-${CONTEXT#kind-}}" ;;
  *)      IS_KIND=0; CLUSTER="" ;;
esac
if [ -n "${CREDS_IMAGE:-}" ]; then
  IMAGE="${CREDS_IMAGE}"
elif [ "${IS_KIND}" = 1 ]; then
  IMAGE="nightshift-creds:local"
else
  IMAGE="ghcr.io/tmih06/nightshift-creds:latest"
fi

k() { kubectl --context "${CONTEXT}" "$@"; }

if [ "${IS_KIND}" = 1 ] && [ -z "${CREDS_IMAGE:-}" ]; then
  echo "1/4 Building ${IMAGE} and loading it into kind cluster ${CLUSTER}..."
  docker build -f "${REPO_ROOT}/images/creds/Dockerfile" -t "${IMAGE}" "${REPO_ROOT}"
  kind load docker-image "${IMAGE}" --name "${CLUSTER}"
elif [ "${CREDS_BUILD:-0}" = 1 ]; then
  echo "1/4 Building and pushing ${IMAGE}..."
  docker build -f "${REPO_ROOT}/images/creds/Dockerfile" -t "${IMAGE}" "${REPO_ROOT}"
  docker push "${IMAGE}"
else
  echo "1/4 Using refresher image ${IMAGE} (the cluster pulls it)."
fi

echo "2/4 Creating the CLIProxyAPI keys if they do not exist..."
if ! k -n "${NS}" get secret cpa-keys >/dev/null 2>&1; then
  k -n "${NS}" create secret generic cpa-keys \
    --from-literal=management="$(openssl rand -hex 24)" \
    --from-literal=client="$(openssl rand -hex 24)"
fi

echo "3/4 Deploying CLIProxyAPI and the refresher..."
sed -E "s|^([[:space:]]*image:) eceasy/cli-proxy-api:latest\$|\\1 ${CPA_IMAGE}|" \
  "${SCRIPT_DIR}/cpa.yaml" | k apply -f -
sed -E "s|^([[:space:]]*image:) ghcr.io/tmih06/nightshift-creds:latest\$|\\1 ${IMAGE}|" \
  "${SCRIPT_DIR}/creds.yaml" | k apply -f -
k -n "${NS}" rollout status deploy/cpa --timeout=180s
k -n "${NS}" rollout status deploy/creds-refresher --timeout=180s

echo "4/4 Done. Sign your accounts in:"
cat <<EOF

  kubectl --context ${CONTEXT} -n ${NS} port-forward svc/cpa 8317:8317

  open  http://127.0.0.1:8317/management.html
  key   kubectl --context ${CONTEXT} -n ${NS} get secret cpa-keys \\
          -o jsonpath='{.data.management}' | base64 -d

On the page, open "OAuth login", pick Claude, Codex, or Antigravity, and follow
the link. The browser then lands on a localhost address that will not load:
copy that full address and paste it into the page's callback box.

The refresher publishes new logins to sandboxes within five minutes. Existing
warm sandboxes keep their old logins; delete an unclaimed pool pod to restock it.
EOF
