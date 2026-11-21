# gh-dash, vendored

This directory is a copy of the `internal/` tree of
[dlvhdr/gh-dash](https://github.com/dlvhdr/gh-dash) at commit
`2665f68d235c0b6dd0e73760c521788960c8761a` (after v4.26.3), under the MIT license in `LICENSE.txt`. It is
the GitHub tab, and its theme and styles paint the Sandboxes tab.

Go does not let one module import another module's `internal` packages, so
the code lives here with its import paths rewritten from
`github.com/dlvhdr/gh-dash/v4/internal` to
`github.com/tmih06/nightshift/internal/ghdash`, then run through `gofmt`.

Local changes, kept small so an upstream sync stays a copy and a rewrite:

- `tui/ui.go`: `Model.Capturing` is exported, so the host knows when a search
  bar, prompt, or comment box needs `tab`.
- `tui/common/styles.go`: `RenderPreviewTitle` drops the blank rows above
  and below the preview title.
- `tui/components/footer/footer.go`, `tui/ui.go`: the footer's donate button
  and its click handler are removed. To support gh-dash, see
  [its sponsors page](https://github.com/sponsors/dlvhdr).
- `tui/ui.go`, `tui/nightshift.go`: `enter` on a pull request row sends
  `OpenPullRequestMsg`, and nightshift opens that pull request in zen-octo's
  view (`internal/zenocto`). gh-dash binds nothing to `enter` there.
- `tui/keys/prKeys.go`, `tui/keys/issueKeys.go`, `tui/ui.go`, `tui/nightshift.go`:
  `S` (the `sandbox` builtin, rebindable) sends `SandboxPullRequestMsg` on a
  pull request row or `SandboxIssueMsg` on an issue row, and nightshift checks
  it out in a new sandbox.
- `tui/update_mouse.go`, `tui/ui.go`, and hit-test/scroll helpers in
  `tui/components/{carousel,tabs,listviewport,table,section,sidebar,prview,footer}`:
  native mouse support matching the Sandboxes tab — clicking section tabs
  (including overflow arrows and the new-section button), table rows
  (double-clicking a pull request opens it in zen-octo), the search bar, PR
  preview tabs, and footer view-switcher/help controls; wheel scrolling over
  the table and sidebar; and dragging the sidebar divider in right or bottom
  layout.
- `tui/ui.go`, `tui/markdown/markdownRenderer.go`, and
  `tui/components/{carousel,cmpcontroller,footer,issueview,listviewport,notificationssection,prrow,prssection,prview,section,sidebar,table,tabs}`:
  keep PR list navigation fast and avoid idle/spinner CPU spikes — sync
  program context only on layout/view/theme changes, skip unused `zone.Scan`
  and `lipgloss.Compositor` when no completion popup is open, cache rendered
  table rows (lazily rendering visible rows, joining fixed-width columns and
  precomputed separators directly, and updating only selection-dependent
  cells on cursor moves), replace `bubbles/viewport` in the sidebar with
  direct line slicing, pre-trim folded PR summaries before glamour rendering,
  fix O(N²) vertical joins on the commits tab, cache chrome (`tabs`,
  `footer`, unfocused `search`), PR previews, and glamour markdown renderers,
  fast-path spinner ticks, and debounce/deduplicate PR enrichment requests.

To sync, copy upstream `internal/` over this directory, rerun the import
rewrite and `gofmt -w`, and reapply the changes above.
