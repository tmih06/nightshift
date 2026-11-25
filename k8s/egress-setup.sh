#!/usr/bin/env bash
# Set up Nightshift's lightweight Go egress + credential-injection proxy on
# any Kubernetes cluster (kind, k3s, EKS, GKE, AKS, ...). Generates an ECDSA
# CA in nightshift-egress/egress-ca, mirrors ONLY the public cert into
# nightshift/egress-ca, loads real credentials from your host environment
# (never stored in the repo) into nightshift-egress/egress-secrets, gets the
# proxy image onto the cluster, and applies k8s/egress.yaml.
#
# Env:  NIGHTSHIFT_CONTEXT  kubeconfig context to use (default kind-agent-sandbox;
#                           KUBE_CONTEXT is still read)
#       EGRESS_IMAGE        proxy image. Unset on a kind context: build
#                           nightshift-egress-proxy:local and load it into kind.
#                           Unset elsewhere: ghcr.io/tmih06/nightshift-egress-proxy:latest.
#       EGRESS_BUILD=1      build EGRESS_IMAGE here and push it to its registry
#                           (needs docker login) instead of pulling a published one
#       KIND_CLUSTER        kind cluster name (default: the context minus "kind-")
set -euo pipefail

CONTEXT="${NIGHTSHIFT_CONTEXT:-${KUBE_CONTEXT:-kind-agent-sandbox}}"
EGRESS_NS="nightshift-egress"
SANDBOX_NS="nightshift"
case "${CONTEXT}" in
  kind-*) IS_KIND=1; CLUSTER="${KIND_CLUSTER:-${CONTEXT#kind-}}" ;;
  *)      IS_KIND=0; CLUSTER="" ;;
esac
if [ -n "${EGRESS_IMAGE:-}" ]; then
  IMAGE="${EGRESS_IMAGE}"
elif [ "${IS_KIND}" = 1 ]; then
  IMAGE="nightshift-egress-proxy:local"
else
  IMAGE="ghcr.io/tmih06/nightshift-egress-proxy:latest"
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

k() {
  kubectl --context "${CONTEXT}" "$@"
}

if [ "${IS_KIND}" = 1 ] && [ -z "${EGRESS_IMAGE:-}" ]; then
  echo "1/4 Building ${IMAGE} and loading into kind cluster ${CLUSTER}..."
  docker build -t "${IMAGE}" -f "${REPO_ROOT}/images/egress/Dockerfile" "${REPO_ROOT}"
  kind load docker-image "${IMAGE}" --name "${CLUSTER}"
elif [ "${EGRESS_BUILD:-0}" = 1 ]; then
  echo "1/4 Building and pushing ${IMAGE}..."
  docker build -t "${IMAGE}" -f "${REPO_ROOT}/images/egress/Dockerfile" "${REPO_ROOT}"
  docker push "${IMAGE}"
else
  echo "1/4 Using proxy image ${IMAGE} (the cluster pulls it)."
fi

echo "2/4 Ensuring namespaces ${EGRESS_NS} and ${SANDBOX_NS}..."
k create namespace "${EGRESS_NS}" --dry-run=client -o yaml | k apply -f -
k create namespace "${SANDBOX_NS}" --dry-run=client -o yaml | k apply -f -

echo "3/4 Provisioning proxy CA and host-sourced Secrets..."
if ! k -n "${EGRESS_NS}" get secret egress-ca >/dev/null 2>&1; then
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "${tmp_dir}"' EXIT
  openssl ecparam -name prime256v1 -genkey -noout -out "${tmp_dir}/ca.key"
  openssl req -new -x509 -sha256 -days 365 \
    -key "${tmp_dir}/ca.key" \
    -out "${tmp_dir}/ca.crt" \
    -subj "/CN=Nightshift Egress Proxy CA/O=Nightshift" \
    -addext "basicConstraints=critical,CA:TRUE,pathlen:0" \
    -addext "keyUsage=critical,keyCertSign,cRLSign,digitalSignature"
  k -n "${EGRESS_NS}" create secret generic egress-ca \
    --from-file=ca.crt="${tmp_dir}/ca.crt" \
    --from-file=ca.key="${tmp_dir}/ca.key"
  k -n "${SANDBOX_NS}" create configmap egress-ca \
    --from-file=ca.crt="${tmp_dir}/ca.crt" \
    --dry-run=client -o yaml | k apply -f -
else
  # Keep the public cert mirror in nightshift in sync.
  tmp_crt="$(mktemp)"
  trap 'rm -f "${tmp_crt}"' EXIT
  k -n "${EGRESS_NS}" get secret egress-ca -o jsonpath='{.data.ca\.crt}' | base64 -d > "${tmp_crt}"
  k -n "${SANDBOX_NS}" create configmap egress-ca \
    --from-file=ca.crt="${tmp_crt}" \
    --dry-run=client -o yaml | k apply -f -
fi

# Seed real credentials from host env or `gh auth token` into nightshift-egress only.
gh_token="${GITHUB_TOKEN:-}"
if [ -z "${gh_token}" ] && command -v gh >/dev/null 2>&1; then
  gh_token="$(gh auth token 2>/dev/null || true)"
fi

secret_args=()
[ -n "${ANTHROPIC_API_KEY:-}" ] && secret_args+=(--from-literal="REAL_ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}")
[ -n "${OPENAI_API_KEY:-}" ] && secret_args+=(--from-literal="REAL_OPENAI_API_KEY=${OPENAI_API_KEY}")
[ -n "${GEMINI_API_KEY:-}" ] && secret_args+=(--from-literal="REAL_GEMINI_API_KEY=${GEMINI_API_KEY}")
[ -n "${gh_token}" ] && secret_args+=(--from-literal="REAL_GITHUB_TOKEN=${gh_token}")

if [ "${#secret_args[@]}" -gt 0 ]; then
  k -n "${EGRESS_NS}" create secret generic egress-secrets \
    "${secret_args[@]}" \
    --dry-run=client -o yaml | k apply -f -
fi

echo "4/4 Applying k8s/egress.yaml..."
# The manifest names the local kind image; point it at the chosen one. It pins
# no clusterIP: service CIDRs differ per provider, so the address is read back
# below and written into sandbox-egress-env.
sed -E "s|^([[:space:]]*image:) nightshift-egress-proxy:local\$|\\1 ${IMAGE}|" \
  "${SCRIPT_DIR}/egress.yaml" | k apply -f -
k -n "${EGRESS_NS}" rollout status deployment/egress-proxy --timeout=120s

# Sandbox pods use external DNS resolvers, so they reach the proxy by IP.
# Rewrite the shared env ConfigMap whenever the live ClusterIP differs.
ip="$(k -n "${EGRESS_NS}" get service egress-proxy -o jsonpath='{.spec.clusterIP}')"
if [ -n "${ip}" ]; then
  proxy_url="http://${ip}:8080"
  pairs=""
  for v in HTTP_PROXY HTTPS_PROXY http_proxy https_proxy; do
    pairs="${pairs}\"${v}\":\"${proxy_url}\","
  done
  patch="{\"data\":{${pairs%,}}}"
  k -n "${SANDBOX_NS}" patch configmap sandbox-egress-env \
    --type=merge -p "${patch}" >/dev/null
  echo "Done. Egress proxy is live at ${proxy_url} (service DNS is unusable from sandboxes)."
else
  echo "Done, but no ClusterIP found; sandbox-egress-env not reconciled." >&2
fi
