# CONTEXT.md

Shared context for working in this repository. Read it before you change
anything. The rules for how to work live in `AGENTS.md`.

## Purpose

Nightshift is a software factory. It picks up a ticket, claims a sandbox, lets
a coding agent finish the task inside it, opens a pull request, and babysits
that pull request through merge conflicts, failing CI, and review. The
roadmap in `README.md` tracks what exists today.

The current code is the foundation: the sandbox fleet built on
kubernetes-sigs/agent-sandbox and a dashboard to watch and drive it, with
pull requests, issues, and notifications handled in the same terminal
through gh-dash.

## Cluster

Nightshift runs on the kind cluster `kind-agent-sandbox` in the namespace
`nightshift` (with the egress proxy isolated in `nightshift-egress`).

## What lives here

- `k8s/` holds the Kubernetes manifests and cluster scripts: `warmpool.yaml`
  creates the namespace `nightshift` and six template/pool pairs, each keeping
  one Pi, Codex, Claude Code, Antigravity (`agy`), OMPi slim
  (`ompig-slim`), or OpenCode sandbox warm. Each pod is capped at
  1000m CPU and 2Gi memory. `claim.yaml` claims a Pi sandbox by default.
  `sbx.sh` wraps that loop in one command and is installed as `sbx` through
  a symlink in `~/.local/bin`. `egress.yaml` and `egress-setup.sh` deploy the
  single-pod Go egress + credential-injection proxy in the separate
  `nightshift-egress` namespace with default-deny `NetworkPolicy` enforcement.
  The proxy records all usage in a SQLite database on the `egress-usage`
  volume.
  `creds-providers.json` lists the agents whose logins reach sandboxes;
  `creds-publish.sh` reads each master login, `creds-stage.sh` swaps its
  access token for a placeholder, and the real tokens go into the egress
  proxy's secrets while the placeholder copies go into the `agent-creds`
  secret every sandbox untars into `$HOME` at boot. Warm pools are sized to
  the logins that exist. `cpa.yaml` runs CLIProxyAPI, the page where accounts
  are signed in, and `creds.yaml` runs the `creds-refresher` that reads it
  (`creds-setup.sh` installs both); `--watch` does it from a laptop instead.
  `docs/login-surface.md` records why.
  `NIGHTSHIFT_CONTEXT` points every script and the TUI at any cluster.
  Sandboxes reach the proxy by its ClusterIP, which `egress-setup.sh` writes
  into the ConfigMap (their DNS is external, so service names never resolve), and the `sandbox-egress-env` ConfigMap wires the
  proxy, CA trust, and placeholders into every template through `envFrom`.
  The Claude Code and Codex templates use `imagePullPolicy:
  IfNotPresent` because their large images stall on slow registry networks.
- `images/base/` defines the common sandbox tools and Herdr; `images/agent/`
  adds one versioned Pi, Codex, Claude Code, Antigravity, OMPi slim, or
  OpenCode agent and stamps it into `/etc/nightshift-agent`; `images/egress/`
  builds the static distroless container for `cmd/egress-proxy`; `images/creds/`
  builds the credential refresher (the login-capable agent CLIs, `kubectl`, and the
  publish scripts). The image
  workflow watches stable npm releases and the agy and ompig-slim release
  feeds, and publishes to GHCR. Check
  registry access before applying `k8s/warmpool.yaml`; see
  `images/agent/README.md`.
- Nightshift is two programs with one contract between them
  (`docs/api.md`). **nightshift-services** (`cmd/nightshiftd`, under
  `internal/services`) owns everything that touches the cluster and serves
  the API. **nightshift-tui** (`cmd/nightshift`, under `internal/tui`) is one
  frontend: a Bubble Tea v2 TUI that reaches the cluster only through that API.
  `api/openapi.yaml` is the contract, `api/v1` its Go types, and `api/client`
  the Go client; they import nothing but the standard library. Anyone can write
  another frontend against them. `internal/architecture_test.go` keeps the two
  halves apart: a frontend may not import the services, the services may not
  import a frontend, and the HTTP layer may not import the cluster.
- The services: `internal/services/cluster` reads and changes the fleet with
  `kubectl`, `session` owns the Herdr terminal sessions, `accounts` is the
  coding-agent accounts' data layer (CLIProxyAPI client, port-forwards, the
  active-account ConfigMap, proxy usage), `checkout` is the pipeline that puts
  a pull request, issue, or branch on its own volume and a sandbox on it,
  `runs` hands a task to a coding agent in such a sandbox and records how it
  ended (the `/v1/runs` endpoints, for issue tracker adapters; see
  `docs/api.md`),
  `server` is the HTTP layer that implements the contract over interfaces,
  `live` answers it from the cluster, `daemon` runs it, and `egress` is the
  library behind `cmd/egress-proxy`, the standalone outbound egress +
  secret-injection proxy. `manifest` pins the cluster manifests' resource
  budget in its tests. The services log through charm's log.
- The frontend: `internal/tui/app` is the root model: it draws the tab bar and
  routes input to the active tab. `dashboard` is the Pods tab, `accountsview`
  the Accounts tab, `repoview` the Repo view, `dialog` the shared widgets, and
`fleet` what the TUI makes of the contract's types. `cmd/nightshift` is only
  a client: it needs `-api` (or `NIGHTSHIFT_API` and `NIGHTSHIFT_TOKEN`) and
  runs nothing else. The services run in the cluster as the `nightshiftd` pod
  (`k8s/nightshiftd.yaml`, `k8s/nightshiftd-setup.sh`). `NIGHTSHIFT_CONTEXT` picks the cluster,
  `NIGHTSHIFT_CONTEXT=-` means the one the services run in. The TUI logs
  through charm's log, never the standard one, which would draw over it.
- `internal/ghdash` is a vendored copy of
  [gh-dash](https://github.com/dlvhdr/gh-dash) (MIT), the GitHub tab.
  Its Go packages are copied with the import paths rewritten; see
  `internal/ghdash/README.md` for the upstream commit and the local change.
  Go forbids importing another module's `internal` packages, which is why it
  is vendored rather than required.
- `internal/zenocto` is the pull request view of
  [zen-octo](https://github.com/praxis-labs-io/zen-octo) (MIT), vendored the
  same way and repainted in gh-dash's look; see `internal/zenocto/README.md`
  for the commit, what was left out, and the local changes.
- `internal/tui/repoview` is Nightshift's 3-pane repository workspace for the
  GitHub tab (repository/scope tree, grouped Branches/PRs/Issues/Actions/Files
  list, and detail preview pane with active sandbox badges).
- `docs/` holds research notes. `docs/software-factory.md` traces the term
  "software factory" through its definitions from 1968 to 2026.
  `docs/session-manager.md` describes session behavior.
  `docs/sandbox-egress-and-secrets.md` surveys open-source network egress
  control and credential-isolation proxies for coding agent sandboxes.
- `scripts/quality/`, `.golangci.yml`, and `.semgrep/` are the quality gate,
  run with `make gate` locally, by the agent Stop hook in
  `.agents/settings.json`, and in `.github/workflows/quality-gate.yml`.
  `docs/quality-discovery.md` holds the measurements behind its thresholds and
  `docs/quality-baseline.md` the recorded debt.
- `.agents/skills/` holds the agent skills for this repo. `.claude/` is a
  symlink to `.agents/`, so Claude Code reads the same skills.
- `ARCHITECTURE.md` explains how the parts connect, with diagrams.

## The dashboard

- A view switcher runs across the top in gh-dash's footer style: Pods,
  with its count, GitHub, and Accounts with its count, the active one bold with a green icon,
  and the NIGHTSHIFT logo in the corner. `tab` and `shift+tab` cycle the
  tabs, and a click on a tab opens it. While a tab holds the keyboard for text
  (the embedded shell, or a gh-dash search bar, prompt, or comment box) `tab`
  belongs to that tab instead.
- The GitHub tab is gh-dash as it ships: pull requests, issues, and
  notifications, plus Nightshift's 3-pane Repo view (`internal/tui/repoview`),
  switched with its own view keys (`s`) or by clicking the footer view
  switcher. The Repo view shows a repository/scope tree, a grouped list
  (Branches, PRs, Issues, Actions, Files), and a detail preview pane with
  active sandbox badges (`b` jumps to a running sandbox in the Sandboxes tab;
  `enter` on a PR opens it in zen-octo and returns to the Repo view on `esc`).
  `go run ./cmd/nightshift -repos` opens straight onto the Repo view. Mouse
  support matches the Sandboxes tab: clicks select section tabs, table rows
  (double-clicking a pull request opens it in zen-octo), the search bar, PR
  preview tabs, and footer controls; the wheel moves through table rows or
  scrolls the preview sidebar under the pointer; and dragging the sidebar rule
  resizes the preview in both right and bottom layouts.
- `enter` (or a double-click) on a pull request opens it in zen-octo's view,
  painted from gh-dash's theme: gh-dash's preview head over Conversation,
  Commits, Checks, and Files tabs, with a details rail. Everything zen-octo
  does on a pull request works there: threaded replies, reactions, resolving,
  inline review comments, reviews, labels, reviewers, assignees, base branch,
  state, job logs and reruns, and merging. Mouse clicks switch its tabs, focus
  panes, select or fold files, commits, and checks, wheel scrolls the pane
  under the pointer, and dragging the rule resizes the side column or rail.
  `esc` or `q` goes back to gh-dash; `?` lists the keys. `tab` still switches
  nightshift's tabs, unless a form or box holds the keyboard; the file, commit,
  or check column steps with `ctrl+n` and `ctrl+p`. Issues stay in gh-dash's
  preview.
  `go run ./cmd/nightshift -pr owner/repo#123` opens straight onto one.
  Large diffs are prepared in the background so file and commit navigation
  stay responsive. The view only receives its own messages, as its model is large to copy. It rides on `gh`'s login,
  reads gh-dash's own config (`.gh-dash.yml` in the repo, `$GH_DASH_CONFIG`,
  or `~/.config/gh-dash/config.yml`), and scopes to the repository it was
  started in, as gh-dash does. A bad config stops nightshift before the TUI
  opens. gh-dash's log is discarded unless `NIGHTSHIFT_DEBUG` names a file.
- `S` on a pull request, issue, or repository/branch row checks it out on its
  own volume in a new sandbox. On a pull request, a dialog over gh-dash or the
  Repo view names the volume and lists the pools with their templates;
  `↑`/`↓` or `j`/`k` picks the pool, and `enter`, `→`/`l`, a number, or a click
  forks a cascading model submenu to the right of the selected pool for that
  harness (`claude`, `codex`, `pi`, or `opencode`). Inside the model submenu,
  `↑`/`↓`/`j`/`k` moves through models, `←`/`h`/`esc` returns to the pool list,
  and `enter`, a number, or a click starts the sandbox and launches the harness
  with `--model <selected>` (or the harness default). On an issue or
  repository/branch, the same dialog also includes a branch name input
  (defaulting to `issue-<n>` for an issue or the selected branch / `main` for a
  repository; `tab` switches between branch and pool, `↑`/`↓` still steps
  through pools while typing). The volume is a
  PersistentVolumeClaim named `pr-<owner>-<repo>-<n>` for a pull request,
  `issue-<owner>-<repo>-<n>` for an issue, or `repo-<owner>-<repo>-<branch>`
  for a repository branch. The first time, a Job on
  the agent's image clones the repository over https into it and checks out
  either the pull request's head ref (named after its head branch, or `pr-<n>`)
  or the chosen branch (tracking `origin/<branch>` if it exists remotely,
  otherwise creating it from HEAD). Then a Sandbox starts from the pool's
  template with the clone mounted at `~/workspace/<repo>`, and the Sandboxes
  tab opens with its shell focused there. A running pod cannot take a new
  volume, so this sandbox starts cold rather than from the warm pool. While it
  runs, a card in the Sandboxes tab's bottom right follows the volume, clone,
  sandbox, and shell steps. It shows git's phase with a bar, size, and speed,
  read from the clone Job's log, the pod's state while its image pulls, and each
  step's time. The tab bar carries the same in one line while the GitHub tab is
  open. One checkout runs at a time. Nothing owns the volume: `x` on the
  sandbox opens a dialog to keep the volume (the default) or destroy it too,
  and `S` on the same pull request, issue, or repository branch later mounts a
  kept volume with its work (switching to the requested branch on an issue or
  repository), or returns to the sandbox already on it. The image carries no
  `gh` and no credentials, so the clone only reaches public repositories.
- The Pods tab is laid out like gh-dash and painted from gh-dash's
  theme. Section tabs (`All`, `Claimed`, `Warm`, `Infra`; `h`/`l` or a click)
  sit over a grouped table showing Nightshift's essential cluster pods:
  claimed sandboxes, warm pool standbys, the `nightshift-egress` proxy pod,
  core cluster pods (`agent-sandbox-controller`, `metrics-server`,
  `local-path-provisioner`), and transient clone Jobs. The right side of the
  section bar summarizes running pods and egress proxy health. In `All` and
  `Infra`, pods are grouped under compact headings (`CLAIMED`, `WARM POOLS`,
  `EGRESS PROXY`, `CLUSTER CORE`, `JOBS & OTHER`). Each pod row shows a role
  icon and live health dot, the pod name and role-aware subtitle (session
  count/owner/template/version or namespace/IP/readiness/restarts), state and
  readiness (at medium/wide widths), CPU and memory with braille history, and
  age (at wide widths). A pod still starting up shows its pipeline stage in
  place of the subtitle — scheduling, pulling, starting, readying — as a
  spinner, a one-block-per-stage bar, and the stage's elapsed time; when the
  pull backs off the marker becomes a cross with the failure count. The
  sidebar heads such a pod with the stage checklist and the pull failure's
  cause, read from the pod's events. Events are fetched only while some pod
  is still starting; Kubernetes exposes no byte-level pull progress, so the
  stages are as deep as the display honestly goes. On the right, behind one
  rule, the preview sidebar
  heads the pod (namespace and owner, pod name, role pill with phase,
  readiness, restarts, Pod IP/node, and age, live CPU/MEM usage table, image
  and ports/volume spec, and for claimed sandboxes the Herdr session and
  window tab bar), followed by clickable sidebar view tabs (`Shell`, `Logs`,
  `Configs`; `v` cycles views, `L` jumps to Logs). Claimed sandboxes default
  to `Shell`, warm sandboxes default to `Configs` (pool, template, sandbox,
  and pod YAML), and egress/core/job pods default to `Logs` (formatting
  `egress-proxy` JSON audit events into colorized `ALLOW`/`DENY` lines, while
  `Configs` shows the `egress-policy` ConfigMap, `Service`, `NetworkPolicy`,
  and `Pod` YAML). The footer bar carries key hints and the update time and
  position, or the latest message. `?` opens the full key list under the
  footer, as it does in gh-dash.
- The sidebar takes half the width and resizes by dragging its rule. Under
  90 columns it moves under the table, as gh-dash's bottom preview does.
- Each account's status rows on the Accounts tab sit beside its provider's
  color logo, pixel art of one size (12 by 5 cells); a pane too narrow for a row
  beside it puts the logo above the rows.
- The Accounts tab marks as `active` the account the egress proxy is using. `u` or
  `enter` makes the selected account first for its agent, and `K`/`J` reorder
  the fallback chain. A change that moves traffic (these, or disabling or
  removing the active account) warns of the prompt cache miss and waits for
  `y`. Every change publishes itself and reaches the proxy within about two
  minutes; until then the new first account reads `pending`. The selected
  account's details list the models its agent's CLI offers. `a` signs one in,
  `d` disables or enables, `x` removes, `r` rereads everything including live
  quotas, and `t` renews a token.
- Usage refreshes every 2 seconds. A claimed sandbox's numbers come from its
  own cgroup counters, which the services read with one `kubectl exec` per
  refresh (the TUI asks for `include=live`): memory from
  the first read, CPU as the rate between two. Warm sandboxes use
  metrics-server, which the cluster samples every 15 seconds and
  averages over 15 to 30. Each running pod's coding CLI is asked its
  `--version` once (with `/etc/nightshift-agent` as fallback), so the table
  and the sidebar's image line show the real version, not `:latest`.
- A tab out of sight keeps refreshing, but the sandbox tab stops rendering the
  shell's screen until it is shown again. Its viewer still processes output so
  the screen stays current. Only visible table rows are rendered, with both
  selection states cached until fleet data, width, or theme changes.
  Configs and Logs documents are reformatted only when their content,
  width, or theme changes, not on unchanged refresh replies. See
  `docs/pods-performance.md` for benchmarks and cache guardrails.
- The shell embeds each sandbox's terminals over one persistent framed Herdr
  `hc connect` connection per pod and a local terminal emulator per attached
  window, preserving colors and styles. Right below the `● herdr live` row, a
  Herdr-style window tab bar lists every live terminal in the pod (including
  sub-terminals spawned inside the pod via `hc run`) with `+` to open a new
  window and `×` to close the active one. Clicking a tab switches windows and
  focuses the shell; middle-clicking a tab closes it; `C-b` prefix chords
  (`C-b c` new, `C-b n`/`p`/`h`/`l`/`1-9` switch, `C-b x` close, `C-b f`
  zoom, `C-b C-b` literal, `C-b q`/`d` leave) provide full keyboard
  control. Zoom gives the shell the whole window, drawn from the same screen
  API, so it works against a server on another machine.
- Focus is in the table during navigation and in the sidebar while bash or
  the configs have it; the sidebar's heading turns green and says how to
  leave, and its rule brightens.
- While the shell holds focus no keyboard input escapes: scrolling stays in the
  shell, clicks on the window tab bar and splitter drag still work, and `C-b q`
  (or `C-b d`) leaves focus. A click in the table always returns to navigation.
- `c` claims from the selected warm sandbox's pool (or `pi-pool` when no
  warm sandbox is selected) and pre-creates its bash, staying in navigation
  until `enter` attaches. `enter` or a click on a warm sandbox's sidebar
  focuses its configs, and `esc` leaves them. Warm pool sandboxes refuse
  bash. `x` twice destroys a claim; on a pull request's sandbox it asks
  whether the volume goes too. `F` zooms the shell to the whole window.
- Status notifications expire after 5 seconds; errors and armed
  confirmations stay.
- Herdr (`hc`) runs inside each claimed sandbox, not on the host. See
  `docs/session-manager.md`.

## Running it

`make` lists every command.

- `make tui` opens the dashboard. `scripts/dev/with-api` reads the token and,
  if nothing answers on `PORT` (7777), port-forwards the API until the
  dashboard closes. `make once` prints one frame the same way. Pass flags in
  `ARGS`, as in `make tui ARGS=-repos`.
- `make redeploy` rebuilds the egress, refresher, and services images from
  the checkout, loads them into kind, and restarts them. It leaves secrets,
  replica counts, and warm pools alone, and applies
  `k8s/egress.yaml` and `k8s/nightshiftd.yaml`.
- `make up` installs or updates everything after the kind cluster and the
  agent-sandbox controller exist. It runs the three setup scripts and applies
  `k8s/warmpool.yaml`, so it copies API keys from your environment into the
  egress proxy, starts the credential refresher, and resets warm pool sizes.
  Use it for a first install or when those manifests change.
- `make status`, `make logs`, and `make forward` show the fleet, follow the
  services' log, and keep the API forwarded for another frontend.
- `CONTEXT` picks the cluster; it defaults to `NIGHTSHIFT_CONTEXT`, then
  `kind-agent-sandbox`.

## Go toolchain

The module is pinned to `go 1.27.4`. The `go` command downloads that
toolchain on demand, and the repo needs no other Go config.

- Run the program with `go run ./cmd/nightshift`.
- Print one frame of the sandbox tab with `go run ./cmd/nightshift -once`.
- Open on one pull request with
  `go run ./cmd/nightshift -pr owner/repo#123` (or its URL).
- Open on the GitHub Repo view with `go run ./cmd/nightshift -repos`.
- Build with `go build -o <path> ./cmd/nightshift` to keep the binary out of
  the repo.

## Verify

- Run `bash -n k8s/sbx.sh k8s/egress-setup.sh` to check that the scripts parse.
- Ensure the published sandbox images are accessible as documented in
  `images/agent/README.md` before testing against the cluster.
- With the cluster up, run `SBX_NO_EXEC=1 sbx` to check the claim and destroy
  loop end to end. The script removes the claim it creates.
- Run `go test ./...` and `go vet ./...` to check the Go module, and
  `go run ./cmd/nightshift -once` to print one dashboard frame against the
  live cluster.
- Run `make lint` for shellcheck, yamllint on `k8s/`, and markdownlint on
  `*.md` and `docs/*.md`. It requires shellcheck, pipx, and npx; CI runs the
  same target and also checks workflow files with actionlint.
- Run `make quality-tools` once, then `make gate` for the fast local checks.
  CI runs `make gate-full`, which takes about ten minutes for its race tests.
- Run `make fmt` to format Go files or `make fmt-check` to check formatting
  without changing files. CI also checks commit subjects against `AGENTS.md`.
