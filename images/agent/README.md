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

The demo manifest references the four published GHCR `latest` images and
keeps one warm pod per agent. Ensure the cluster can pull them, then apply:

```sh
kubectl --context kind-agent-sandbox apply -f demo/warmpool.yaml
```

On an existing demo this creates `pi-pool`; applying the manifest does not
remove the old `agent-pool` or its warm sandbox. Existing claims are not deleted. Select a warm sandbox and
press `c` in the dashboard to claim its pool, or use, for example,
`POOL=codex-pool sbx`. The default `sbx` pool is Pi's `pi-pool`.

The manifest uses `Always` to pull the current tag when a pod starts; already
running pods are not automatically replaced when an image is published. For
local builds, build `images/base` and `images/agent`, load each image into
kind, and change the corresponding template to use its local tag and
`IfNotPresent`. The agent Dockerfile defaults to Pi 1.5.2; set `AGENT` and
`AGENT_VERSION` for another agent, and `BASE_IMAGE` to select a base. Build
for the node's architecture.

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
Forks publish under their own lowercase repository name; update the templates
accordingly. To pin versions, set each template's image to an accessible tag
or digest.
Loading a local image on one kind node does not make it available on others.
