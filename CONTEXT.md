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

The demo runs on the kind cluster `kind-agent-sandbox` in the namespace
`sandbox-demo`.

## What lives here

- `demo/` holds the Kubernetes side: `warmpool.yaml` creates the namespace
  `sandbox-demo` and the standing pool, one `SandboxTemplate` and one
  `SandboxWarmPool` that keeps two warm sandboxes, each pod capped at 500m
  CPU and 256Mi memory. `claim.yaml` claims one sandbox from the pool.
  `sbx.sh` wraps that loop in one command and is installed as `sbx` through
  a symlink in `~/.local/bin`.
- `images/base/` defines the common sandbox tools and Herdr; `images/agent/`
  adds one versioned Pi, Codex, Claude Code, or OpenCode agent. The image
  workflow watches stable npm releases and publishes to GHCR. Build and load
  the default Pi image into kind before applying `demo/warmpool.yaml`; see
  `images/agent/README.md`.
- `cmd/nightshift` is the entrypoint of the Go program, a Bubble Tea v2
  TUI. `internal/app` is the root model: it draws the tab bar and routes
  input to the active tab. `internal/dashboard` is the sandbox tab,
  `internal/cluster` reads the fleet from the cluster, `internal/session`
  owns the Herdr terminal sessions, and `internal/manifest` pins the demo
  manifests' resource budget in its tests.
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
- `docs/` holds research notes. `docs/software-factory.md` traces the term
  "software factory" through its definitions from 1968 to 2026.
  `docs/session-manager.md` describes session behavior.
- `.agents/skills/` holds the agent skills for this repo. `.claude/` is a
  symlink to `.agents/`, so Claude Code reads the same skills.
- `ARCHITECTURE.md` explains how the parts connect, with diagrams.

## The dashboard

- A view switcher runs across the top in gh-dash's footer style: Sandboxes,
  with its count, and GitHub, the active one bold with a green icon,
  and the NIGHTSHIFT logo in the corner. `tab` and `shift+tab` cycle the
  tabs, and a click on a tab opens it. While a tab holds the keyboard for text
  (the embedded shell, or a gh-dash search bar, prompt, or comment box) `tab`
  belongs to that tab instead.
- The GitHub tab is gh-dash as it ships: pull requests, issues, and
  notifications, switched with its own view keys.
- `enter` on a pull request opens it in zen-octo's view, painted from
  gh-dash's theme: gh-dash's preview head over Conversation, Commits, Checks,
  and Files tabs, with a details rail. Everything zen-octo does on a pull
  request works there: threaded replies, reactions, resolving, inline review
  comments, reviews, labels, reviewers, assignees, base branch, state, job
  logs and reruns, and merging. `esc` or `q` goes back to gh-dash; `?`
  lists the keys. `tab` still switches nightshift's tabs, unless a form or
  box holds the keyboard; the file, commit, or check column steps with
  `ctrl+n` and `ctrl+p`. Issues stay in gh-dash's preview.
  `go run ./cmd/nightshift -pr owner/repo#123` opens straight onto one.
  The view only receives its own messages, as its model is large to copy. It rides on `gh`'s login,
  reads gh-dash's own config (`.gh-dash.yml` in the repo, `$GH_DASH_CONFIG`,
  or `~/.config/gh-dash/config.yml`), and scopes to the repository it was
  started in, as gh-dash does. A bad config stops nightshift before the TUI
  opens. gh-dash's log is discarded unless `NIGHTSHIFT_DEBUG` names a file.
- The Sandboxes tab is laid out like gh-dash and painted from gh-dash's
  theme. Section tabs (All, Warm, Claimed; `h`/`l` or a click) sit over a
  table with gh-dash's header row, two lines per sandbox (node and owner,
  then name), a session dot, CPU and memory with braille history, and age,
  each row ruled off and the selection highlighted. On the right, behind one
  rule, the preview sidebar heads the sandbox like a pull request, without
  gh-dash's blank rows around the title (namespace
  and owner, the name block, a Warm or Claimed pill with phase, node, and
  age, its usage as a two-row table with fixed columns or its spec, the Herdr session), then shows the shell for a
  claimed sandbox or the configs and manifests for a warm one. The footer
  bar carries key hints and the update time and position, or the latest
  message. `?` opens the full key list under the footer, as it does in
  gh-dash.
- The sidebar takes half the width and resizes by dragging its rule. Under
  90 columns it moves under the table, as gh-dash's bottom preview does.
- Usage refreshes every 2 seconds. A claimed sandbox's numbers come from its
  own cgroup counters, read with one `kubectl exec` per refresh: memory from
  the first read, CPU as the rate between two. Warm sandboxes use
  metrics-server, which the demo cluster samples every 15 seconds and
  averages over 15 to 30.
- A tab out of sight keeps refreshing, but the sandbox tab stops reading the
  shell's screen until it is shown again. Table rows and warm-sandbox
  documents are rendered once per change, not per frame.
- The shell embeds the sandbox's bash through Herdr screen reads, so Herdr
  never takes over the terminal.
- Focus is in the table during navigation and in the sidebar while bash or
  the configs have it; the sidebar's heading turns green and says how to
  leave, and its rule brightens.
- While the shell holds focus no input escapes: scrolling and clicks stay in
  the shell, only the splitter drag still works, and C-b then q is the only
  way out. A click in the table always returns to navigation.
- `c` claims a sandbox and pre-creates its bash, staying in navigation until
  `enter` attaches. `enter` or a click on a warm sandbox's sidebar
  focuses its configs, and `esc` leaves them. Warm pool sandboxes refuse
  bash. `x` twice destroys a claim. `F` fullscreens.
- Status notifications expire after 5 seconds; errors and armed
  confirmations stay.
- Herdr (`hc`) runs inside each claimed sandbox, not on the host. See
  `docs/session-manager.md`.

## Go toolchain

The module is pinned to `go 1.27.4`. The `go` command downloads that
toolchain on demand, and the repo needs no other Go config.

- Run the program with `go run ./cmd/nightshift`.
- Print one frame of the sandbox tab with `go run ./cmd/nightshift -once`.
- Open on one pull request with
  `go run ./cmd/nightshift -pr owner/repo#123` (or its URL).
- Build with `go build -o <path> ./cmd/nightshift` to keep the binary out of
  the repo.

## Verify

- Run `bash -n demo/sbx.sh` to check that the script parses.
- Build and load the sandbox image as documented in `images/agent/README.md`
  before testing against the cluster.
- With the cluster up, run `SBX_NO_EXEC=1 sbx` to check the claim and destroy
  loop end to end. The script removes the claim it creates.
- Run `go test ./...` and `go vet ./...` to check the Go module, and
  `go run ./cmd/nightshift -once` to print one dashboard frame against the
  live cluster.
- CI lints `*.md` and `docs/*.md` with markdownlint and checks commit
  subjects against `AGENTS.md`.
