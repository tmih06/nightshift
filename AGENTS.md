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
- Stay scoped to Nightshift's cluster resources. Touch only the
  `kind-agent-sandbox` cluster and Nightshift's namespaces (`nightshift` and
  `nightshift-egress`), and clean up any claim you create.
- Ask before anything hard to undo: deleting cluster resources you did not
  create, force-pushing, or rewriting history.
- Keep build output, binaries, and credentials out of the repo. Never bake
  secrets into the sandbox image.

## Quality gate

Run `make quality-tools` once, then `make gate` before calling work done. It
builds, lints changed packages, and runs the diff rules in seconds. CI runs
`make gate-full`, which adds race tests, Semgrep, coverage, the debt ratchet,
and vulnerability scans.

- Build and test: `go build ./...`, `go test ./...`.
- One linter: `.tools/bin/golangci-lint run --enable-only errcheck ./internal/tui/app/...`.
- Semgrep rules: `semgrep scan --metrics=off --config .semgrep/slop.yml internal`.
- Debt fell: `make quality-baseline`, then commit the lower counts.

A human's explicit instruction takes priority over every rule in this file, including this gate. These rules bind agents acting on their own: never bypass the gate, never add `//nolint`, `t.Skip`, or disable a rule to make it pass, and never edit gate config to loosen it. If a rule seems wrong, stop and ask the human.

## Go conventions

- Wrap errors with `%w` and surface them as UI status (`internal/services/cluster/volume.go`).
- Keep stdout clear: it belongs to the TUI. Log through `log`, which
  `cmd/nightshift/main.go` discards unless `NIGHTSHIFT_DEBUG` names a file.
- Take `ctx` first. Commands with no parent start from `context.WithTimeout`
  (`internal/tui/repoview/model.go`).
- Respect the layers: `internal/services` owns the cluster and `internal/tui` reaches
  it only through `api/v1` and `api/client`. Inside the services, `cluster`,
  `egress`, and `manifest` are leaves and `session` imports only `cluster`. Tabs
  talk through `internal/tui/app`, and vendored code imports nothing
  first-party. `depguard` and `internal/architecture_test.go` enforce this.
- Give every goroutine an owner: a `sync.WaitGroup`, done channel, or context
  (`internal/tui/dashboard/model.go`).
- Write first-party tests with the standard library and plain `t.Errorf`
  (`internal/tui/dashboard/dashboard_test.go`).
- Reuse `internal/services/cluster` for `kubectl`, `internal/services/session`
  for Herdr, `internal/tui/dialog` for dialogs, and gh-dash's `tui/theme` for styling.

## Guardrails

### Root-cause fixes must leave a guardrail

Whenever you fix a bug or defect by finding its root cause, you MUST also add a rule that prevents the same class of problem from recurring, in the same PR. Do this in order of preference:

1. **Mechanical enforcement**: a new or tightened golangci-lint setting, a Semgrep rule (with positive and negative fixtures), a forbidigo/depguard entry, or a regression test that fails without the fix.
2. **A written rule** in the "Learned rules" section below, when it cannot be enforced mechanically.

Every entry in "Learned rules" must follow this format, be specific and testable, and be checked for duplicates first (merge with or sharpen an existing rule instead of adding near-copies):

- **Rule**: one imperative sentence.
- **Why**: the incident or bug that caused it (PR, commit, or issue link).
- **Enforced by**: the lint rule, Semgrep id, or test name, or "manual" if none.

A fix PR that adds no rule must say why in its description (for example, "one-off typo, no recurring class"). Do not add vague rules like "be careful with errors". If a rule can't be made specific enough to check, don't add it.

### Maintaining the rules

When a learned rule becomes mechanically enforced, update its "Enforced by".
Prune rules that no longer apply. Past about 200 lines, move older detail into
`docs/rules/` and link to it.

## Learned rules

- **Rule**: Give every external command a deadline.
  - **Why**: A `git clone` stalled forever on a hung stream (`82ea704`).
  - **Enforced by**: `noctx`.
- **Rule**: Assign a mutating model command to a variable before `return m, cmd`.
  - **Why**: Go leaves the order unspecified, so 8 sites may return stale state (`docs/quality-discovery.md`).
  - **Enforced by**: `gocritic` `evalOrder`.
- **Rule**: Build render output in one `strings.Builder` pass, never by repeated joins.
  - **Why**: An O(N^2) join made the commits tab lag (`4b3e2f4`).
  - **Enforced by**: `perfsprint`, `BenchmarkPRCommitsTab`.
- **Rule**: Send all input to the shell while it holds focus.
  - **Why**: Wheel and click events leaked past the focused shell (`28619be`).
  - **Enforced by**: `TestNoInputLeaksFromShell`.
- **Rule**: Drain shell input as one ordered batch at a time.
  - **Why**: Concurrent sends reordered fast typing (`b18d72a`).
  - **Enforced by**: `TestShellInputQueue`.
- **Rule**: Scale usage graphs against the sandbox limit, not the window's peak.
  - **Why**: A 45% load drew as a full graph (`e14c24f`).
  - **Enforced by**: `TestBrailleGraph`.
- **Rule**: Resolve gate package lists through `go list ./...`.
  - **Why**: The fast gate linted Semgrep fixtures as a package (`a413a9b`).
  - **Enforced by**: manual.
- **Rule**: Keep `k8s/` YAML lines within 80 columns.
  - **Why**: A long comment failed Lint on `main` (`9ee64a4`).
  - **Enforced by**: `make lint`.

- **Rule**: Forward the API in `with-api` when `NIGHTSHIFT_API` names its own
  local port.
  - **Why**: An exported address from an earlier session made `make tui` skip
    the forward and fail with connection refused (`13a8f3e`).
  - **Enforced by**: `TestWithAPIForwardsALeftoverLocalAddress`.

- **Rule**: Type into a terminal through the sender's own viewer, and never
  attach one at a made-up size.
  - **Why**: Keys went through a second viewer at 160x40, so typing resized the
    terminal away from the one on screen and garbled it (`02b65ed`).
  - **Enforced by**: `TestTypingKeepsTheTerminalsSize`.
- **Rule**: Share one in-flight cluster read between requests, and start none
  per key press or screen read of a known claim.
  - **Why**: Concurrent screen reads stacked `kubectl` processes until
    `nightshiftd` was throttled and OOM-killed (`cbf342a`).
  - **Enforced by**: `TestClaimedSharesOneClusterRead`,
    `TestTheServicesHaveRoomForTheirKubectls`.

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
