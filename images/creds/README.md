# Credential refresher image

kubectl, jq, curl, and `k8s/creds-*.sh` on the sandbox base. It reads the
accounts signed in to CLIProxyAPI and publishes sandbox copies; see
`docs/login-surface.md`.

On kind, `k8s/creds-setup.sh` builds it and loads it into the cluster. On any
other cluster it pulls `ghcr.io/tmih06/nightshift-creds:latest`, which nothing
publishes yet. Build and push your own and point the setup at it:

```sh
docker build -f images/base/Dockerfile -t nightshift-base:local images/base
CREDS_IMAGE=registry.example.com/nightshift-creds:1 CREDS_BUILD=1 \
  NIGHTSHIFT_CONTEXT=my-cluster k8s/creds-setup.sh
```

For a registry that supports several architectures, use
`docker buildx build --platform linux/amd64,linux/arm64 --push` with the same
Dockerfile and context (the repository root) instead.
