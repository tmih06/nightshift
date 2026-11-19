# AGENTS.md

This directory holds the demo for kubernetes-sigs/agent-sandbox. The demo runs on the kind cluster `kind-agent-sandbox` in the namespace `sandbox-demo`.

## What lives here

- `demo/warmpool.yaml` creates the namespace `sandbox-demo` and the standing pool: one `SandboxTemplate` and one `SandboxWarmPool` that keeps two warm sandboxes, each pod capped at 500m CPU and 256Mi memory.
- `demo/claim.yaml` claims one sandbox from the pool. Apply it, work, then delete it.
- `demo/sbx.sh` wraps that loop in one command. It is installed as `sbx` through a symlink in `~/.local/bin`.
- `ARCHITECTURE.md` explains how the parts connect, with diagrams.
- The Go module holds a Bubble Tea dashboard for the sandboxes. Run `go run .` to open it, `go run . -once` to print one frame. It is pinned to `go 1.27.4`, the `go` command downloads that toolchain on demand, and the repo needs no other Go config.

## Verify

- Run `bash -n demo/sbx.sh` to check that the script parses.
- With the cluster up, run `SBX_NO_EXEC=1 sbx` to check the claim and destroy loop end to end. The script removes the claim it creates.
- Run `go test ./...` and `go vet ./...` to check the Go module, and `go run . -once` to print one dashboard frame against the live cluster. Build with `go build -o <path> .` to keep the binary out of the repo.

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
