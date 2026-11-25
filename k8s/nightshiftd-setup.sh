#!/usr/bin/env bash
# nightshiftd-setup — run the Nightshift services in a cluster, on any provider.
#
# Deploys nightshiftd (the API every frontend uses, docs/api.md) with the least
# privilege it needs, makes its token, and prints how to reach it. Run
# k8s/egress-setup.sh and k8s/creds-setup.sh first: it reads the egress proxy
# and CLIProxyAPI. You need rights to create ClusterRoles (cluster-admin).
#
# Env:  NIGHTSHIFT_CONTEXT   kubeconfig context (default kind-agent-sandbox;
#                            KUBE_CONTEXT is still read)
#       NIGHTSHIFTD_IMAGE    services image. Unset on a kind context: build
#                            nightshiftd:local and load it into kind. Unset
#                            elsewhere: ghcr.io/tmih06/nightshiftd:latest.
#       NIGHTSHIFTD_BUILD=1  build NIGHTSHIFTD_IMAGE here and push it to its
#                            registry (needs docker login) instead of pulling
#       LOG_NAMESPACES       namespaces whose pod logs it may read, for the fleet
#                            view (default: nightshift nightshift-egress
#                            agent-sandbox-system kube-system
#                            local-path-storage, those that exist). Fewer is
#                            fine; the logs of the others then show an error.
#       KIND_CLUSTER         kind cluster name (default: the context minus
#                            "kind-")
set -euo pipefail

CONTEXT="${NIGHTSHIFT_CONTEXT:-${KUBE_CONTEXT:-kind-agent-sandbox}}"
NS=nightshift
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
case "${CONTEXT}" in
  kind-*) IS_KIND=1; CLUSTER="${KIND_CLUSTER:-${CONTEXT#kind-}}" ;;
  *)      IS_KIND=0; CLUSTER="" ;;
esac
if [ -n "${NIGHTSHIFTD_IMAGE:-}" ]; then
  IMAGE="${NIGHTSHIFTD_IMAGE}"
elif [ "${IS_KIND}" = 1 ]; then
  IMAGE="nightshiftd:local"
else
  IMAGE="ghcr.io/tmih06/nightshiftd:latest"
fi

k() { kubectl --context "${CONTEXT}" "$@"; }

if [ "${IS_KIND}" = 1 ] && [ -z "${NIGHTSHIFTD_IMAGE:-}" ]; then
  echo "1/5 Building ${IMAGE} and loading it into kind cluster ${CLUSTER}..."
  docker build -f "${REPO_ROOT}/images/nightshiftd/Dockerfile" -t "${IMAGE}" "${REPO_ROOT}"
  kind load docker-image "${IMAGE}" --name "${CLUSTER}"
elif [ "${NIGHTSHIFTD_BUILD:-0}" = 1 ]; then
  echo "1/5 Building and pushing ${IMAGE}..."
  docker build -f "${REPO_ROOT}/images/nightshiftd/Dockerfile" -t "${IMAGE}" "${REPO_ROOT}"
  docker push "${IMAGE}"
else
  echo "1/5 Using services image ${IMAGE} (the cluster pulls it)."
fi

echo "2/5 Creating the API token if it does not exist..."
if ! k -n "${NS}" get secret nightshiftd-token >/dev/null 2>&1; then
  k -n "${NS}" create secret generic nightshiftd-token \
    --from-literal=token="$(openssl rand -hex 24)"
fi

echo "3/5 Deploying nightshiftd..."
sed -E "s|^([[:space:]]*image:) ghcr.io/tmih06/nightshiftd:latest\$|\\1 ${IMAGE}|" \
  "${SCRIPT_DIR}/nightshiftd.yaml" | k apply -f -

echo "4/5 Letting it read pod logs where the fleet view shows pods..."
# Bound per namespace, and only in the ones this cluster has: kind runs a
# local-path provisioner, a managed cluster usually does not.
for ns in ${LOG_NAMESPACES:-nightshift nightshift-egress agent-sandbox-system kube-system local-path-storage}; do
  if k get namespace "${ns}" >/dev/null 2>&1; then
    k -n "${ns}" create rolebinding nightshiftd-logs \
      --clusterrole=nightshiftd-logs --serviceaccount="${NS}:nightshiftd" \
      --dry-run=client -o yaml | k apply -f - >/dev/null
  fi
done

# A locally built image keeps its tag, so a pod already running it must restart
# to pick up a rebuild.
if [ "${IS_KIND}" = 1 ] && [ -z "${NIGHTSHIFTD_IMAGE:-}" ]; then
  k -n "${NS}" rollout restart deploy/nightshiftd
fi
k -n "${NS}" rollout status deploy/nightshiftd --timeout=180s

echo "5/5 Done. Point a frontend at it:"
cat <<EOF

  kubectl --context ${CONTEXT} -n ${NS} port-forward svc/nightshiftd 7777:7777

  NIGHTSHIFT_API    http://127.0.0.1:7777
  NIGHTSHIFT_TOKEN  kubectl --context ${CONTEXT} -n ${NS} get secret \\
                      nightshiftd-token -o jsonpath='{.data.token}' | base64 -d

Set both in the environment of \`nightshift\` (or of any other frontend).

For other machines, put your provider's ingress, with TLS, in front of
svc/nightshiftd (port 7777). The token is required for everything.
EOF
