# Coding-agent sandbox image

This image contains Pi (`@earendil-works/pi-coding-agent` 1.5.2), Herdr Core
(`hc` 0.1.21), `fd`, `rg`, Node.js, Python, Git, SSH, curl, jq, and basic
shell tools. It runs as the unprivileged `node` user in
`/home/node/workspace`. Herdr's release download is checked against pinned
SHA-256 hashes for Linux amd64 and arm64.

Build and load it into the demo's kind cluster **before** applying the warm
pool manifest:

```sh
docker build -t nightshift-agent:0.1.0 images/agent
kind load docker-image nightshift-agent:0.1.0 --name agent-sandbox
kubectl --context kind-agent-sandbox apply -f demo/warmpool.yaml
```

The manifest uses `IfNotPresent` and a local image tag. The image is not
published to a registry by this repository. For a remote cluster, push it to
an accessible registry and update the image reference in the template. Rebuild
for the node architecture; loading an image on one kind node does not make it
available on other nodes. Reusing a tag does not force existing pods to reload
it: use a new tag and update the template when changing the image.

Pi needs provider credentials or an interactive login before it can contact a
model. No credentials are baked into the image. The workspace, Pi configuration,
and Herdr state are ephemeral by default; deleting the sandbox deletes them.
The demo's 256Mi memory limit may need to be raised for real agent workloads.
