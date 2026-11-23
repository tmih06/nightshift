# Quality gate discovery

Measured on commit `3fc25ec` (2026-11-23), before any gate config existed.
Every number below comes from a command run against this repository.

"First-party" is `cmd/` plus `internal/` without the two vendored trees.
"Vendored" is `internal/ghdash` and `internal/zenocto`, copied from upstream
projects with local changes. Vendored code is 79% of the Go lines, so the two
are reported apart: its numbers would otherwise hide the code this team writes.

## 1. Stack

| Area | Finding |
| --- | --- |
| Go | `go 1.27.4` in `go.mod`. The system `go` is 1.22.2 and switches toolchains on demand. |
| Modules | One module, no `go.work`, 62 packages. |
| TUI | Bubble Tea v2, Lip Gloss v2, Bubbles v2, Glamour v2, Ultraviolet. |
| GitHub | `go-gh`, `githubv4`, and the `gh` CLI. |
| Cluster | No client-go. `kubectl` runs through `os/exec` in `internal/cluster` and `internal/session`. |
| HTTP | Standard library only, in `internal/egress`. |
| Data | No SQL, ORM, gRPC, or DI framework. |
| Generated code | None. No file has the standard `Code generated` header. The `mockup_*_test.go` files are handwritten. |
| Vendored code | `internal/ghdash` (36k lines), `internal/zenocto` (58k lines). |

## 2. Existing tooling

- `Makefile` has `lint` (shellcheck, yamllint, markdownlint), `fmt`, and
  `fmt-check`. The gate extends this Makefile.
- `.github/workflows/ci.yml` runs Lint, Go (test, vet, format), image builds,
  commit subject checks, and an end-to-end kind job.
- `.github/workflows/images.yml` publishes the sandbox images.
- There was no `.golangci.yml`, CODEOWNERS file, git hook, or `CLAUDE.md`.
- `AGENTS.md` holds working rules and the commit convention. `CONTEXT.md`
  holds the verify commands. `.claude` is a symlink to `.agents`.
- `main` has no branch protection, and `tmih06` is the only collaborator.

## 3. Baseline before any change

Measured on a 4-core arm64 host with `KUBECONFIG` pointed at a missing file, so
no test reached the live cluster.

| Check | Result | Duration |
| --- | --- | --- |
| `go build ./...` | pass | 5.0s |
| `go vet ./...` | pass | 1.4s |
| `go test ./...` with coverage | pass | 125.9s |
| `go test -race -shuffle=on -count=1 ./...` | pass | 564.4s |
| `gofmt -l .` | clean | 0.3s |
| `go mod tidy` drift | none | 0.5s |
| `govulncheck ./...` | 0 called vulnerabilities | under 10s |
| golangci-lint | never configured | not applicable |

Statement coverage is 63.6% overall and 70.6% for first-party code.

`internal/zenocto/tui/prview` takes 538s of the race run, so whole-tree race
testing belongs in CI and local runs must scope to changed packages.

Two tool traps affect every later step:

- Source-analysis tools must run with the 1.27.4 toolchain's `bin` first on
  `PATH`. With the system `go` first, `deadcode` reports false type errors.
- The `govulncheck` in `~/go/bin` was built with Go 1.26 and cannot load this
  module. The gate builds its own pinned copy with Go 1.27.4.

## 4. Conventions, with evidence

**Errors.** Wrap with `%w`: 38 first-party `Errorf` calls use it and none
format an error with `%v`. There are no sentinel errors. The one typed error is
`rpcError` in `internal/session/session.go:402`. Errors become UI status in the
Bubble Tea update loop. `log.Fatalf` appears only in `cmd/egress-proxy`.

**Logging.** The standard `log` package in `cmd/`, discarded in the TUI unless
`NIGHTSHIFT_DEBUG` names a file (`cmd/nightshift/main.go:64`). Printing to stdout
would corrupt the TUI.

**Context.** When a function takes a context it is the first parameter (20 of
20). Bubble Tea commands have no parent context, so they start from
`context.WithTimeout(context.Background(), apiTimeout)`, as in
`internal/repoview/model.go:686`. A bare `context.Background()` with no deadline
is the exception.

**Layering**, from `go list` imports:

```text
cmd/nightshift    -> app, ghdash, zenocto
cmd/egress-proxy  -> egress
app               -> cluster, dashboard, dialog, repoview, ghdash, zenocto
dashboard         -> cluster, dialog, session, ghdash/tui
repoview          -> cluster, dialog, ghdash, zenocto/tui
session           -> cluster
dialog            -> ghdash/tui/theme
cluster, egress, manifest -> no internal packages
ghdash, zenocto   -> never import first-party packages
```

**Concurrency.** No `errgroup`. Fan-out uses `sync.WaitGroup`
(`internal/dashboard/model.go:305`). There are 15 first-party `go` statements.
Long-lived loops are owned by a connection type (`internal/session/session.go:239`).

**Tests.** First-party tests use the standard library with plain `t.Errorf` and
no testify, go-cmp, or `t.Parallel`. Vendored tests use testify and golden files
(54 fixture files). No build tags. `TestMain` exists only in two vendored
packages. Benchmarks exist for the render paths in `internal/app`,
`internal/dashboard`, `internal/repoview`, `internal/session`, and `prview`.

**Helpers to reuse.** `internal/dialog` for dialogs, gh-dash's
`tui/theme` and `tui/context` for styling, `internal/cluster` for every
`kubectl` call, and `internal/session` for Herdr connections.

## 5. History mining

57 commits: 29 feat, 10 fix, 6 chore, 5 refactor, 4 ci, 3 docs. No reverts or
hotfixes. GitHub has 4 pull requests with 0 review comments, so there is no
review feedback to mine.

| Bug class | Commits | Tool can catch it |
| --- | --- | --- |
| Render and input latency | `eaa825b`, `b27e48f`, `4b3e2f4`, `b18d72a` | Partly: benchstat, prealloc, perfsprint |
| External call with no deadline | `82ea704` (git clone stalled forever) | Yes: noctx, Semgrep |
| Focus and input state leaks | `28619be`, `03a170b` | Regression tests only |
| Wrong layout or scaling math | `e14c24f`, `03a170b` | Regression and golden tests only |
| Concurrent input reordering | `b18d72a` | Partly: race detector |
| Manifest and lint failures on main | `9ee64a4`, `0283029` | Yes: `make lint` |

## 6. Measurements

### Slop signals

Counts are production / test.

| Signal | First-party | Vendored |
| --- | --- | --- |
| `//nolint` | 0 / 0 | 2 / 2 |
| `t.Skip` | 0 / 2 | 0 / 23 |
| Statements assigning only to `_` | 38 / 51 | 16 / 44 |
| errcheck with blank and type-assert checks | 61 / 138 | 45 / 41 |
| Empty `if err != nil {}` | 0 / 0 | 0 / 0 |
| `panic(` | 1 / 0 | 35 / 0 (33 in one testdata stub) |
| `time.Sleep` | 3 / 8 | 0 / 17 |
| `fmt.Print*` | 1 / 0 | 0 / 0 |
| `TODO\|FIXME\|HACK\|XXX` | 0 / 0 | 6 / 0 |
| `any` | 82 / 45 | 65 / 58 |
| Package-level vars | 75 / 6 | 100 / 38 |
| `init()` | 1 / 0 | 0 / 2 |
| `go` statements (function literals) | 15 (14) / 1 (1) | 9 (4) / 6 (6) |
| `context.Background/TODO` | 23 / 0 | 29 / 204 |

### Function shape, first-party production (600 functions)

| Metric | p50 | p90 | p95 | p99 | max |
| --- | --- | --- | --- | --- | --- |
| Lines | 13 | 55 | not sampled | 164 | 369 |
| Statements | 7 | 32 | 45 | 83 | 276 |
| Cyclomatic complexity | 4 | 12 | 19 | 44 | 87 |
| Cognitive complexity | 3 | 14 | 27 | 58 | 146 |
| Parameters | 1 | 3 | not sampled | 5 | 6 |
| Nesting depth | 1 | 3 | not sampled | 5 | 6 |
| nestif complexity | 1 | 2 | 3 | 6 | 22 |

### Duplication, dead code, coverage

- jscpd (10 lines, 100 tokens, production code): 0.49%, 315 of 64,736 lines, in
  12 clones. 46 duplicated lines are first-party. The largest first-party clone
  is 33 lines in `internal/repoview/update.go:677` and `:733`. The largest overall
  is 50 lines in `ghdash/tui/components/prssection/prssection.go`.
- `deadcode -test`: 117 unreachable functions, 113 vendored. The first-party
  four are `sandboxCount`, `Model.listItemCount`, `Model.selectRepoScopeInNav`,
  and `clampLine`.
- 20 of 62 packages have no tests. Two are first-party: `cmd/egress-proxy` and
  `cmd/nightshift`.

| First-party package | Coverage |
| --- | --- |
| `internal/app` | 83.7% |
| `internal/dashboard` | 83.5% |
| `internal/dialog` | 75.6% |
| `internal/session` | 72.3% |
| `internal/egress` | 68.0% |
| `internal/repoview` | 65.4% |
| `internal/cluster` | 56.5% |
| `cmd/*` | 0.0% |

### Change size

Per commit, excluding vendored trees, fixtures, `go.sum`, and `.agents`: p50
238, p75 702, p90 1,525, p95 2,223, max 10,581 changed lines.

## 7. Signal, measurement, threshold

| Signal | Current measurement | Proposed threshold | Reason |
| --- | --- | --- | --- |
| Function lines | p90 55, p99 164 | 60 | Just above p90, so 9 in 10 existing functions already comply |
| Statements | p90 32, p95 45 | 35 | Just above p90 |
| Cyclomatic | p90 12, p95 19 | 12 | At p90; 59 functions exceed it and go in the baseline |
| Cognitive | p90 14, p95 27 | 15 | Just above p90; 55 functions exceed it |
| nestif | p95 3, p99 6 | 4 | Above p95, because deep `if` trees are rare here |
| Parameters | p99 5, max 6 | 5 | p99; only the outliers exceed it |
| Nesting depth | p99 5, max 6 | 5 | p99 |
| Duplication, dupl | 0 first-party hits at 150 tokens | 150 tokens | Keeps first-party at zero |
| Duplication, jscpd | 0.49% | 0.49%, ratchet only | May go down, never up |
| Changed-line coverage | 70.6% first-party | 70% | New code meets the current average |
| Diff size | p95 2,223 lines | 2,200 lines | Blocks only the top 5% of changes |
| `//nolint` in first-party | 0 | 0 new | Clean today, so keep it clean |
| `t.Skip` added | 2 first-party | 0 new | History has no legitimate new skip |
| Empty error block | 0 | 0 | Clean today |
| `time.Sleep`, `fmt.Print*`, `panic` | 3, 1, 1 in production | 0 new | Each corrupts the TUI or hides a race |
| Dead code | 117 functions | ratchet | May go down, never up |
| Called vulnerabilities | 0 | 0 | Clean today |
| Race detector | clean | clean | Clean today |

## 8. Lint noise found while calibrating

- `gocritic hugeParam` has 202 first-party hits. Bubble Tea's value-receiver
  `Model` makes these the architecture, not a defect.
- `gocritic evalOrder` has 100 first-party hits on `return m, m.cmd()`. In 89
  of them `m` is a pointer, so the order is harmless. In 11, `m` is a value and
  the method has a pointer receiver, and in 8 of those the method mutates `m`
  (for example `internal/dashboard/update.go:62`). The Go spec leaves that order
  unspecified, so the returned model may or may not include the mutation. A
  probe on Go 1.27.4 returned the mutated value with and without optimizations,
  so today's behavior is correct but not guaranteed. The gate keeps this check on and baselines the existing hits.
- `mnd` has 251 first-party production hits, 192 of them in the two layout
  packages, where cell offsets are the domain. Forcing names on them would add
  noise rather than clarity.
- golangci-lint's default `uniq-by-line` hides issues that share a line. The
  first count missed over half of them, so the gate turns it off.
