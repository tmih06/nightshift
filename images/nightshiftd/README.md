# Nightshift services image

`nightshiftd`, the API every frontend uses (`docs/api.md`), and the `kubectl`
it drives the cluster with, on a distroless base that runs as a non-root user.

On kind, `k8s/nightshiftd-setup.sh` builds it and loads it into the cluster.
On any other cluster it pulls `ghcr.io/tmih06/nightshiftd:latest`, which nothing
publishes yet. Build and push your own and point the setup at it:

```sh
NIGHTSHIFTD_IMAGE=registry.example.com/nightshiftd:1 NIGHTSHIFTD_BUILD=1 \
  NIGHTSHIFT_CONTEXT=my-cluster k8s/nightshiftd-setup.sh
```

For a registry that supports several architectures, use
`docker buildx build --platform linux/amd64,linux/arm64 --push` with the same
Dockerfile and context (the repository root) instead.
