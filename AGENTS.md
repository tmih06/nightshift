# AGENTS.md

Rules for working in this repository. Read `CONTEXT.md` first: it holds the
project context, the layout, and the verify commands.

## Working principles

- Read before you write. Check `CONTEXT.md`, `ARCHITECTURE.md`, and the code
  you are about to touch before changing it.
- Keep each change small and focused on one concern. Do not refactor or
  reformat code the task does not need.
- Verify before you call work done. Run the checks under "Verify" in
  `CONTEXT.md` that cover your change, and report failures as they are.
- Keep the docs true. When a change alters behavior, layout, or commands,
  update `CONTEXT.md`, `ARCHITECTURE.md`, or `docs/` in the same change. When
  it ships a roadmap item, tick it in `README.md`.
- Keep `README.md` free of technical detail. It speaks to people who want to
  know what Nightshift does, not how.
- Stay inside the demo. Touch only the `kind-agent-sandbox` cluster and the
  `sandbox-demo` namespace, and clean up any claim you create.
- Ask before anything hard to undo: deleting cluster resources you did not
  create, force-pushing, or rewriting history.
- Keep build output, binaries, and credentials out of the repo. Never bake
  secrets into the sandbox image.

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
