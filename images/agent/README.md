# Coding-agent sandbox images

`images/base` provides Herdr Core (`hc` 0.1.21), `fd`, `rg`, Node.js,
Python, Git, SSH, curl, jq, and basic shell tools. Herdr's release download
is checked against pinned SHA-256 hashes for Linux amd64 and arm64. The base
runs as the unprivileged `node` user in `/home/node/workspace`.

`images/agent` builds on that base and installs one agent at an exact version:

| `AGENT` | npm package | Command |
| --- | --- | --- |
| `pi` | `@earendil-works/pi-coding-agent` | `pi` |
| `codex` | `@openai/codex` | `codex` |
| `claude-code` | `@anthropic-ai/claude-code` | `claude` |
| `opencode` | `opencode-ai` | `opencode` |

The build verifies the agent's version by running its command. Agent packages
may execute their npm install scripts at build time. No credentials are baked
into the images; supply provider credentials or log in at runtime.

## Demo build

Build and load the default Pi image into the demo's kind cluster **before**
applying the warm pool manifest:

```sh
docker build -t nightshift-base:local images/base
docker build -t nightshift-agent:0.1.0 images/agent
kind load docker-image nightshift-agent:0.1.0 --name agent-sandbox
kubectl --context kind-agent-sandbox apply -f demo/warmpool.yaml
```

The defaults are Pi 1.5.2 and the locally built base. To build another agent,
pass `--build-arg AGENT=codex --build-arg AGENT_VERSION=<stable-version>` to
the second build, and give the image an appropriate tag. `BASE_IMAGE` can
also select a different local or published base. Build for the node's
architecture. The manifest uses `IfNotPresent` and its local demo tag;
changing images requires updating the template and replacing the pool's pods.
The workspace, agent configuration, and Herdr state are ephemeral by default.
The demo's 256Mi memory limit may need to be raised for real agent workloads.

## Published images and release watcher

`.github/workflows/images.yml` builds and publishes Linux amd64 and arm64
images to GHCR. For this repository the names are:

```text
ghcr.io/tmih06/nightshift-base:latest
ghcr.io/tmih06/nightshift-pi:<agent-version>
ghcr.io/tmih06/nightshift-codex:<agent-version>
ghcr.io/tmih06/nightshift-claude-code:<agent-version>
ghcr.io/tmih06/nightshift-opencode:<agent-version>
```

Each agent also has a `latest` tag. The workflow checks the npm registry's
`latest` distribution tag for each package daily, accepts only plain stable
`major.minor.patch` versions, and builds an agent image when that version tag
is absent. It builds only the current `latest`, not every intermediate release
or prerelease. A failed build is retried on the next run. Changes to image
files on `main`, or a manual workflow dispatch on `main`, rebuild all current
agent versions against the new base. Consequently version tags are mutable;
pin an image digest for reproducible deployments. A base refresh on the daily
schedule alone does not rebuild already published agent versions.

Publishing requires Actions to have package write access, and the GHCR
packages must be made public or the cluster must authenticate to pull them.
Forks publish under their own lowercase repository name. On a remote cluster,
set the sandbox template's image to an accessible published tag or digest.
Loading a local image on one kind node does not make it available on others.
