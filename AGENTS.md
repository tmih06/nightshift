# AGENTS.md

This directory holds the demo for kubernetes-sigs/agent-sandbox. The demo runs on the kind cluster `kind-agent-sandbox` in the namespace `sandbox-demo`.

## What lives here

- `demo-warmpool.yaml` creates the standing pool: one `SandboxTemplate` and one `SandboxWarmPool` that keeps two warm sandboxes.
- `demo-claim.yaml` claims one sandbox from the pool. Apply it, work, then delete it.
- `sbx.sh` wraps that loop in one command. It is installed as `sbx` through a symlink in `~/.local/bin`.
- `ARCHITECTURE.md` explains how the parts connect, with diagrams.

## Verify

- Run `bash -n sbx.sh` to check that the script parses.
- With the cluster up, run `SBX_NO_EXEC=1 sbx` to check the claim and destroy loop end to end. The script removes the claim it creates.

## Commit messages

Every commit message follows Conventional Commits 1.0.0. The spec lives at https://www.conventionalcommits.org/. The format is `type(scope): subject`.

- Pick the type from this list: feat, fix, docs, chore, refactor.
- Set the scope to the area you changed: sbx, warmpool, claim, or docs. Omit the scope when the commit spans areas.
- Write the subject in the imperative mood and lowercase. Keep it to 50 characters or fewer, with no trailing period.
- Mark a breaking change with `!` after the type. Add a `BREAKING CHANGE:` footer that names what breaks.
- Wrap the body at 72 characters. The body explains why. The diff shows what.

Examples:

- `feat(sbx): name each claim after its run`
- `fix(claim): default shutdownPolicy to Delete`
- `docs: add the pool and claim diagrams`
