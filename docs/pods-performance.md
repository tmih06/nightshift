# Pods rendering performance

Review scope: `c15e45c`, `3d13eb5`, and `60200bf` (the essential-pod
reader, Pods tab, and its tests). All sections, sidebar views, log error
handling, follow/scroll behavior, and polling intervals are retained.

## Measurements

Go 1.27.4, linux/arm64, four logical CPUs; medians of six runs comparing
`60200bf` with this change. Both revisions use the same synthetic fixtures.
These measurements cover the UI thread, not Kubernetes/network latency or
terminal transport latency.

```sh
go test ./internal/tui/dashboard -run '^$' -bench BenchmarkPods -benchmem -count=6
```

| Workload | Before | After | Time reduction |
| --- | ---: | ---: | ---: |
| Navigate down/up with two frames, 8 pods | 4.17 ms | 1.26 ms | 70% |
| Navigate down/up with two frames, 80 pods | 26.12 ms | 1.30 ms | 95% |
| Refresh unchanged Configs, 8 pods, 240 YAML lines | 5.40 ms | 1.94 ms | 64% |
| Refresh unchanged Logs, 8 pods, 200 audit events | 10.89 ms | 2.07 ms | 81% |

Each refresh iteration delivers a fleet reply, an unchanged log reply, and
renders a frame. Navigation alternates between two pods after warming the
cache; it does not measure visiting every pod for the first time. All four
comparisons have `p=0.002` with benchstat. Allocation bytes fall by 50%, 89%,
49%, and 69%, respectively.

The CPU profile attributes 48% of sampled time to `tableView` (44% to
`renderRow`) and 30% to document caching/rendering across these workloads.
The table used to render every row on selection changes, then discard rows
outside the viewport. Documents used fleet-wide and log-wide revisions,
forcing unchanged YAML highlighting and audit JSON formatting on refreshes.

Separate six-run checks of the existing warm/shell frame and selection-only
benchmarks found no statistically significant time change. Frame allocation
bytes increase by about 4 KiB (4%) because the visible table body is assembled
on each frame. The new row cache can retain two rendered states per visited
pod until the next fleet revision; it does not render off-screen pods to
populate those states.

## Cache invariants and regression guardrails

- `dataRev` changes when fleet data or terminal membership changes. Row indices
  refer to the fleet at that revision. Width and theme also invalidate rows.
  `TestTableRendersOnlyVisibleRows`, `TestRowCacheTracksSessionAttach`, and
  `TestPodCachesTrackTheme` enforce these boundaries.
- Viewport slicing must produce exactly the old full-render-then-slice output,
  including partial rows and group headings, in every section and layout.
  `TestTableViewportMatchesFullRendering` compares both algorithms.
- Configs markdown is rebuilt once per fleet revision, but unchanged markdown
  reuses the highlighted document. Logs compare their own raw text, error,
  loading state, and pod role. Cache keys also include pod, mode, width, and
  theme. `TestInspectCacheUsesContent`,
  `TestLogCacheTracksLoadingErrorsAndRecovery`, and `TestPodCachesTrackTheme`
  cover reuse and invalidation.

The duplicated current-document cache metadata, global log revision, and
single-use inspector forwarding helpers are gone. Infra group definitions
are shared between All and Infra rather than maintained twice.

## Not applied

- **Hypothesis:** cache table ordering to avoid repeated grouping/sorting.
  It is secondary to row/document formatting; the 80-pod navigation workload
  is already about 1.3 ms for two frames, so another cache is not justified.
- **Hypothesis:** reduce or defer Kubernetes/config/log polling. This might
  reduce external process/API load but changes freshness and failure behavior;
  polling is intentionally unchanged. No cluster-latency improvement is claimed.
