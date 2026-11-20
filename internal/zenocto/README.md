# zen-octo's pull request view, vendored

This directory is the pull request view of
[praxis-labs-io/zen-octo](https://github.com/praxis-labs-io/zen-octo) at commit
`3e1d4155b6c86801ba5ef65e01384b34d1b7a631`, under the MIT license in `LICENSE`. It
opens when you press `enter` on a pull request in the GitHub tab.

Only what the view needs is here: `gh` (the GitHub client), `store` (its
cache), `link`, and `tui/prview`, `tui/comp`, `tui/keys`, `tui/paint`,
`tui/syntax`, `tui/theme`, with `tui/app` cut down to the host of one pull
request. zen-octo's list, sections, config, update check, mock mode, and
command are left out: gh-dash is the list. Import paths are rewritten from
`github.com/praxis-labs-io/zen-octo/internal` to
`github.com/tmih06/nightshift/internal/zenocto`, then run through `gofmt`.
Go does not let one module import another module's `internal` packages.

Local changes:

- **The look is gh-dash's.** `tui/comp/pane.go` draws panes flat, the way
  gh-dash draws its views: a heading bar on the selected background, one rule
  between side-by-side panes, cards marked by an accent bar and closed by a
  faint rule, and square frames for overlays. The rows and columns are
  zen-octo's, so cursors and mouse hits land where they did.
  `tui/prview/prview.go` draws gh-dash's preview head (the location bar,
  the title, the branches, the section tabs, and the rule), and
  `tui/comp/statusbar.go` gh-dash's footer bar. `tui/paint/background.go`
  keeps a bar's background through every style reset. nightshift maps
  gh-dash's theme onto zen-octo's in `internal/app/pr.go`.
- `comp.ClassicChrome` draws zen-octo's original boxes in the same geometry.
  Only zen-octo's own tests set it, because they find panes by their
  box-drawing glyphs; the gh-dash look has its own tests in
  `tui/comp/pane_look_test.go`.
- `tui/app`: `New` takes the client, theme, and syntax rather than zen-octo's
  config; `esc` and `q` close the pull request with `ClosedMsg` instead of
  quitting; `Capturing` and `Opening` are exported for the host. Its tests
  open pull requests through a stand-in for the list
  (`host_shim_test.go`); tests of the list, sections, polling of sections,
  and config notices were removed with those features.
- `tui/keys`: stepping the file, commit, or check column moved from
  `tab`/`shift+tab` to `ctrl+n`/`ctrl+p`, so `tab` keeps switching
  nightshift's tabs. Forms still use `tab` between fields.
- `store`: `SectionSpec` replaces `config.Section`, so the cache does not
  pull in zen-octo's config package.

To sync, copy the packages above from upstream `internal/`, rerun the import
rewrite and `gofmt -w`, and reapply the changes.
