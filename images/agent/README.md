# Coding-agent sandbox images

`images/base` provides Herdr Core (`hc` 0.1.21), `fd`, `rg`, Node.js,
Python, Git, SSH, curl, jq, and basic shell tools. Herdr's release download
is checked against pinned SHA-256 hashes for Linux amd64 and arm64. The base
runs as the unprivileged `node` user in `/home/node/workspace`.

`images/agent` builds on that base and installs one agent at an exact version:

| `AGENT` | Installs | Command |
| --- | --- | --- |
| `pi` | `@earendil-works/pi-coding-agent` from npm | `pi` |
| `codex` | `@openai/codex` from npm | `codex` |
| `claude-code` | `@anthropic-ai/claude-code` from npm | `claude` |
| `opencode` | `opencode-ai` from npm | `opencode` |
| `agy` | Antigravity CLI release download, checksum-verified | `agy` |
| `ompig-slim` | [tmih06/ompig-slim](https://github.com/tmih06/ompig-slim) release, checksum-verified | `ompig-slim` |
| `devin` | Devin CLI release bundle from `static.devin.ai`, checksum-verified, in `/opt/devin` | `devin` |

The build verifies the agent's version by running its command and stamps it
into `/etc/nightshift-agent` as `<agent> <semver>` for the dashboard to read.
npm packages may execute their install scripts at build time; the agy,
ompig-slim, and devin downloads are checked against the checksum each
release publishes. No credentials are baked into the images; supply provider
credentials or log in at runtime.

## Adding an agent

Each agent is a directory under `agents/`, named as its image, template, and
pool will be. `agent.sh` reads it; nothing else in the image build changes.

1. Write `agents/<name>/agent.env` with `COMMAND`, the CLI the image puts on
   `PATH`. For an npm package, add `NPM_PACKAGE`. Otherwise add an
   `install.sh` that installs `$VERSION` for `$TARGETARCH` and checks the
   published checksum, and a `latest.sh` that prints the latest stable
   release. `agent.env` may also set `PULL_POLICY` (default `Always`) and
   `MEMORY_REQUEST` (default `128Mi`) for its template.
2. Add a `boot.sh` beside it if the sandbox must prepare the agent at start,
   such as turning approvals off. `k8s/warmpool.sh` inlines it.
3. Add the agent to `internal/services/agent/catalogue.go`: its label, the
   words that name its pools, its models, and its unattended task command.
4. Run `make warmpool`, and allow the agent's API hosts in `k8s/egress.yaml`.

`go test ./internal/...` fails until the directory, the catalogue, and
`k8s/warmpool.yaml` agree. Both image workflows build every directory.

## Cluster deployment

The warm pool manifest references the published GHCR `latest` images and
keeps one warm pod per agent. Ensure the cluster can pull them, then apply:

```sh
kubectl --context kind-agent-sandbox apply -f k8s/warmpool.yaml
```

On an existing cluster this creates `pi-template` and updates `pi-pool`; it does
not remove the old `agent-bash-template` or `agent-pool`. Updating a pool can
replace its warm sandbox, but existing claims are not deleted. Select a warm
sandbox and press `c` in the dashboard to claim its pool, or use, for example,
`POOL=codex-pool sbx`. The default `sbx` pool is Pi's `pi-pool`.

The manifest uses `Always` to pull the current tag when a pod starts; already
running pods are not automatically replaced when an image is published. For
local builds, build `images/base` and `images/agent`, load each image into
kind, and change the corresponding template to use its local tag and
`IfNotPresent`. The agent Dockerfile defaults to Pi 1.5.2; set `AGENT` and
`AGENT_VERSION` for another agent, and `BASE_IMAGE` to select a base. Build
for the node's architecture.

The workspace, agent configuration, and Herdr state are ephemeral by default.
Each agent pod defaults to a 2Gi memory limit; heavier
workloads may need more.

## Published images and release watcher

`.github/workflows/images.yml` builds and publishes Linux amd64 and arm64
images to GHCR. For this repository the names are:

```text
ghcr.io/tmih06/nightshift-base:latest
ghcr.io/tmih06/nightshift-pi:<agent-version>
ghcr.io/tmih06/nightshift-codex:<agent-version>
ghcr.io/tmih06/nightshift-claude-code:<agent-version>
ghcr.io/tmih06/nightshift-agy:<agent-version>
ghcr.io/tmih06/nightshift-ompig-slim:<agent-version>
ghcr.io/tmih06/nightshift-opencode:<agent-version>
ghcr.io/tmih06/nightshift-devin:<agent-version>
```

Each agent also has a `latest` tag. The workflow checks the npm registry's
`latest` distribution tag for the npm agents and each binary agent's own
release feed daily, accepts only plain stable `major.minor.patch` versions,
and builds an agent image when that version tag is absent. It builds only the current `latest`, not every intermediate release
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
