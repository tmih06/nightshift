# AGENTS.md

This directory holds the demo for kubernetes-sigs/agent-sandbox. The demo runs on the kind cluster `kind-agent-sandbox` in the namespace `sandbox-demo`.

## What lives here

- `demo/` holds the Kubernetes side: `warmpool.yaml` creates the namespace `sandbox-demo` and the standing pool, one `SandboxTemplate` and one `SandboxWarmPool` that keeps two warm sandboxes, each pod capped at 500m CPU and 256Mi memory. `claim.yaml` claims one sandbox from the pool. `sbx.sh` wraps that loop in one command and is installed as `sbx` through a symlink in `~/.local/bin`.
- `cmd/nightshift` is the entrypoint of the Go program, a Bubble Tea dashboard for the sandboxes. `internal/cluster` reads the fleet from the cluster, `internal/dashboard` renders the TUI, `internal/session` owns the tmux sessions, and `internal/manifest` pins the demo manifests' resource budget in its tests.
- The TUI is a lazygit-style layout: the left pane fits its content by default and resizes by dragging its splitter. The right column is a bento row: two separate boxes for info (sized to its text, never cut) and usage with btop-style history charts, then the shell pane below, embedding the sandbox's bash through tmux capture-pane so tmux never takes over the terminal. `c` claims a sandbox and pre-creates its bash, staying in navigation until `enter` attaches, warm pool sandboxes refuse bash, `x` twice destroys a claim, `F` fullscreens. While the shell has focus the status bar fills and no keybind escapes: C-b then q is the only way out. See `docs/session-manager.md` for why tmux. The bash sessions need tmux installed.
- `docs/` holds research notes. `docs/software-factory.md` traces the term "software factory" through its definitions from 1968 to 2026.
- `ARCHITECTURE.md` explains how the parts connect, with diagrams.
- Run the program with `go run ./cmd/nightshift`, print one frame with `go run ./cmd/nightshift -once`. The module is pinned to `go 1.27.4`, the `go` command downloads that toolchain on demand, and the repo needs no other Go config.

## Verify

- Run `bash -n demo/sbx.sh` to check that the script parses.
- With the cluster up, run `SBX_NO_EXEC=1 sbx` to check the claim and destroy loop end to end. The script removes the claim it creates.
- Run `go test ./...` and `go vet ./...` to check the Go module, and `go run ./cmd/nightshift -once` to print one dashboard frame against the live cluster. Build with `go build -o <path> ./cmd/nightshift` to keep the binary out of the repo.

## Commit messages

Every commit message follows [Conventional Commits 1.0.0](https://www.conventionalcommits.org/). The format is `type(scope): subject`.

- Pick the type from this list: feat, fix, docs, chore, refactor, ci.
- Set the scope to the area you changed: sbx, warmpool, claim, docs, or go. Omit the scope when the commit spans areas.
- Write the subject in the imperative mood and lowercase. Keep it to 50 characters or fewer, with no trailing period.
- Mark a breaking change with `!` after the type. Add a `BREAKING CHANGE:` footer that names what breaks.
- Wrap the body at 72 characters. The body explains why. The diff shows what.

Examples:

- `feat(sbx): name each claim after its run`
- `fix(claim): default shutdownPolicy to Delete`
- `docs: add the pool and claim diagrams`
