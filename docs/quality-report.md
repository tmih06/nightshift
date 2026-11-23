# Quality gate report

Set up on 2026-11-23 on branch `chore/quality-gate`. The branch is not pushed,
so the GitHub workflows have not run yet, and branch protection is not applied.

## What was set up

| Phase | Result | Commits |
| --- | --- | --- |
| 1. Discovery | `docs/quality-discovery.md` | `862cf1a` |
| 2. Rules | golangci-lint, Semgrep, diff rules, CODEOWNERS, goleak | `747e19a`, `5c9712e` |
| 3. Baseline | Safe autofixes, per-rule debt, ratchet | `f984390`, `dfac9b8` |
| 4. Enforcement | `make gate`, Stop hook, CI workflows | `44aee44`, `a413a9b` |
| 5. `AGENTS.md` | Conventions, root-cause rule, 7 learned rules | `ca7b96c` |
| 6. Verification | 11 violations caught, tool tests added | `eeaa644` |

Local and CI checks differ on purpose, at the maintainer's request:

- `make gate` and the Stop hook run build, lint on changed packages, and the diff
  rules.
- CI runs `make gate-full`, which adds vet, Semgrep, race tests, changed-line
  coverage, duplication, the debt ratchet, govulncheck, and `go mod tidy` drift.

## Signal, measurement, threshold

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

## Rules

35 linters are enabled, each with its reason in `.golangci.yml`. Five Semgrep rules
in `.semgrep/slop.yml` have positive and negative fixtures.

| Left off | Reason |
| --- | --- |
| `mnd` | 251 hits, 192 in layout code where cell offsets are the domain |
| `gocritic hugeParam` | 202 hits on Bubble Tea's value-receiver models, which are the architecture |
| `rowserrcheck`, `sqlclosecheck` | The repo has no SQL |
| `goleak` on `session`, `egress` | Their tests leak today, recorded as debt |
| Complexity rules in tests | Table-driven tests are long by design |

## Debt baseline

Lint debt is 913 first-party and 1426 vendored findings. The full table and the
payoff order are in `docs/quality-baseline.md`. The largest first-party rules:

| Rule | First-party | Vendored |
| --- | --- | --- |
| `golangci/errcheck` | 199 | 86 |
| `golangci/gocritic` | 190 | 326 |
| `golangci/goconst` | 100 | 66 |
| `golangci/gochecknoglobals` | 74 | 122 |
| `golangci/cyclop` | 59 | 78 |
| `golangci/funlen` | 57 | 71 |
| `golangci/gocognit` | 55 | 56 |
| `golangci/wrapcheck` | 49 | 82 |
| `semgrep/slop-error-assigned-to-blank` | 40 | 8 |
| `golangci/gosec` | 30 | 72 |

Pay down in this order: value-model state races, commands with no deadline,
goroutine leaks, production `errcheck`, `gosec`, context and wrapping, then
complexity only when a change already touches the function.

## New dependencies and tools

| Tool | Version | Why |
| --- | --- | --- |
| `go.uber.org/goleak` | v1.3.0 | Test-only dependency that fails tests leaking goroutines |
| golangci-lint | v2.15.0 | Main lint engine |
| govulncheck | v1.10.0 | Finds vulnerabilities the code actually calls |
| deadcode | x/tools v0.53.0 | Finds unreachable functions |
| benchstat | x/perf 2026-11-17 | Compares render benchmarks, since latency is the top bug class |
| gremlins | v0.6.0 | Weekly mutation testing on `egress`, `session`, `cluster` |
| Semgrep | 1.180.0 | Repo-specific rules, run with metrics off |
| jscpd | 5.4.3 | Cross-file duplication |

## Gate runtime

| Step | Local | CI profile |
| --- | --- | --- |
| Build | 3.0-7.2s | 2.0s |
| golangci-lint | 1.1-4.0s | 1.6s cached |
| Diff rules | 0.2s | 0.1s |
| Vet | not run | 0.5s |
| Semgrep | not run | 12.9s |
| Race tests | not run | 622.1s |
| Changed-line coverage | not run | 0.1s |
| Duplication and ratchet | not run | 7.7s |
| govulncheck | not run | 9.2s |
| `go mod tidy` drift | not run | 0.1s |
| **Total** | **4-10s** | **656s** |

## Verification

Each violation was committed on a throwaway branch, since deleted.

| Violation | Caught by | Runs locally |
| --- | --- | --- |
| Swallowed error | `errcheck` | Yes |
| Bare `//nolint` | `nolintlint`, `diff/no-nolint` | Yes |
| `time.Sleep` | `forbidigo` | Yes |
| Layering violation | `depguard` | Yes |
| Skipped test | `diff/no-skip` | Yes |
| Removed assertion | `diff/assertion-removed` | Yes |
| Deleted test file | `diff/test-deleted` | Yes |
| Loosened lint config | `diff/protected-path` | Yes |
| Raised debt baseline | `diff/protected-path` | Yes |
| Floating goroutine | `slop-floating-goroutine` | CI only |
| Duplicated block | jscpd ratchet | CI only |

A headless Claude Code run was blocked twice by the Stop hook, then allowed to stop
with a warning for a human. The full gate passed on `eeaa644`. After `a413a9b`,
each moved check was rerun on its own except the vendored race tests.

## Known gaps

- The gate cannot judge design quality or a fix that is subtly wrong. Humans still
  review those.
- Local runs no longer run tests, so an agent can finish with failing tests until
  CI reports them.
- A pull request runs its own copy of the workflow and gate scripts, so only
  required code owner review stops a PR from rewriting them.
- Function-level limits apply when the signature line changes. Growing an existing
  long function is not flagged by `funlen`, `cyclop`, or `gocognit`.
- `AGENTS.md` learned rules marked by tests only protect the behavior those tests
  cover.

## Open questions

1. **How should gate changes get approved?** `tmih06` is the only collaborator and
   GitHub blocks approving your own PR, so any PR touching protected files will fail
   `diff/protected-path`. Running agents under a separate bot account would let
   `tmih06` approve them. Adding a second reviewer also works.
2. **Should branch protection go on now?** Requiring review with no admin bypass
   would block every merge until question 1 is settled. Requiring only the
   `Quality gate` check is safe to enable today.
3. **When should the fix-guardrail check block?** It warns for now. Set
   `GATE_GUARDRAIL_BLOCK=1` in the workflow to enforce it.
