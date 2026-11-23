# Quality debt baseline

`docs/quality-baseline.json` is the machine-readable record. CI fails when any
rule's count rises, separately for first-party and vendored code. New and
changed code gets no allowance: it must be clean.

Recorded on 2026-11-23, after the safe autofixes. Lint totals are
913 first-party and 1426 vendored findings. The `jscpd` row counts duplicated lines.

## Keeping it current

- When debt falls, run `make quality-baseline` and commit the lower numbers. That
  target can only lower counts.
- Never raise a count to get a change through. A deliberate rise, such as an
  upstream re-sync of vendored code, needs `go run ./scripts/quality/ratchet init`
  and code owner review.

## Debt per rule

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
| `jscpd/duplicated-lines` | 46 | 269 |
| `semgrep/slop-error-assigned-to-blank` | 40 | 8 |
| `golangci/gosec` | 30 | 72 |
| `golangci/nestif` | 19 | 26 |
| `golangci/noctx` | 17 | 16 |
| `golangci/exhaustive` | 13 | 57 |
| `golangci/forbidigo` | 13 | 19 |
| `golangci/staticcheck` | 10 | 2 |
| `golangci/unused` | 9 | 69 |
| `golangci/unparam` | 5 | 36 |
| `deadcode/unreachable-func` | 4 | 113 |
| `golangci/revive` | 4 | 45 |
| `golangci/contextcheck` | 3 | 0 |
| `golangci/prealloc` | 2 | 31 |
| `golangci/errorlint` | 2 | 0 |
| `golangci/perfsprint` | 1 | 91 |
| `golangci/gochecknoinits` | 1 | 2 |
| `semgrep/slop-floating-goroutine` | 1 | 1 |
| `golangci/ineffassign` | 1 | 0 |
| `semgrep/slop-background-context-in-request-path` | 1 | 0 |
| `golangci/testifylint` | 0 | 51 |
| `golangci/dupl` | 0 | 8 |
| `golangci/godox` | 0 | 6 |
| `golangci/nolintlint` | 0 | 4 |
| `golangci/thelper` | 0 | 3 |
| `golangci/nilerr` | 0 | 1 |

## Known debt outside the counts

- `internal/session` and `internal/egress` leak goroutines in their existing
  tests, so they have no `goleak` check yet. `internal/cluster` and
  `internal/dashboard` do.
- 32 first-party files hold the functions over the complexity limits. They
  are covered by the `cyclop`, `gocognit`, and `funlen` rows.

## Recommended payoff order

Highest risk first. Each step is its own small pull request.

1. **Model state returned beside a mutating call.** Fix the 8 `return m, m.cmd()`
   sites where `m` is a value and the method mutates it, such as
   `internal/dashboard/update.go:62`. They work today only because of an
   evaluation order the Go spec does not promise.
2. **External commands with no deadline** (`noctx`, 6 production sites). This is
   the class that stalled clones in `82ea704`.
3. **Goroutine leaks** in `internal/session` and `internal/egress`, then add
   `goleak` to both.
4. **Unchecked errors in production** (`errcheck`, 61 sites), starting with
   `internal/egress` and `internal/cluster`.
5. **Security findings in production** (`gosec`), starting with the egress proxy.
   Most test findings are deliberate fixtures.
6. **Context and wrapping** (`contextcheck`, `errorlint`, `wrapcheck`).
7. **Complexity**, only when a change already touches the function.
8. **Dead code and duplication**: the 4 first-party dead functions, then the
   33-line clone in `internal/repoview/update.go`.
9. **Style debt** (`goconst`, `gochecknoglobals`, remaining `gocritic`).
10. **Vendored code**: leave it, and fix findings upstream when re-syncing.
