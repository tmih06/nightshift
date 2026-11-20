package prview

import (
	"maps"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/zenocto/gh"
	"github.com/tmih06/nightshift/internal/zenocto/store"
)

const backgroundDiffLines = 150
const backgroundDiffBytes = 16 << 10

type blockWork struct {
	key   blockKey
	state blockState
	file  *gh.ChangedFile
}

type diffWork struct {
	tab    int
	rev    uint64
	path   string // selected file in Files; Commits is identified by rev
	blocks []blockWork
}

type diffRenderedMsg struct {
	work   *diffWork
	blocks map[blockKey]block
}

// Use a fixed scan budget. A single very long source line can also be slow
// to highlight, even with few lines in the patch.
func needsDiffWorker(work []blockWork) bool {
	n, bytes := 0, 0
	for _, w := range work {
		if w.file.Omitted != "" {
			continue
		}
		for _, h := range w.file.Hunks {
			n += len(h.Lines)
			if n >= backgroundDiffLines {
				return true
			}
			for _, line := range h.Lines {
				bytes += len(line.Content)
				if bytes >= backgroundDiffBytes {
					return true
				}
			}
		}
	}
	return false
}

func (m *Model) wantDiff(d *diffBody, blocks []blockWork) {
	if running := m.diffRunning; running != nil && running.tab == m.tab && running.rev == d.rev &&
		slices.EqualFunc(running.blocks, blocks, func(a, b blockWork) bool {
			return a.key == b.key && a.state == b.state
		}) {
		m.diffWanted = running
		return
	}
	m.diffWanted = &diffWork{tab: m.tab, rev: d.rev, path: m.shownPath, blocks: blocks}
}

// TakeDiffRender starts at most one worker. While it runs, syncContent keeps
// only the newest request, so key repeats and resize drags do not queue jobs.
func (m *Model) TakeDiffRender() tea.Cmd {
	if m.diffRunning != nil || m.diffWanted == nil {
		return nil
	}
	work := m.diffWanted
	m.diffRunning = work
	// The worker builds static code blocks; the UI still owns focus, thread
	// cards, viewport state, and the editor. Snapshot the worker's inputs.
	worker := Model{
		theme: m.theme, painter: m.painter, syntax: m.syntax,
		open: maps.Clone(m.open), opening: m.opening,
	}
	worker.inline.at = m.inline.at
	if work.tab == tabFiles {
		worker.detail = store.Detail{Detail: gh.PullRequestDetail{Threads: slices.Clone(m.detail.Detail.Threads)}}
	}
	return func() tea.Msg {
		blocks := make(map[blockKey]block, len(work.blocks))
		for _, w := range work.blocks {
			b := worker.fileBlock(*w.file, w.state.width, work.tab == tabFiles, w.key.heading, w.state.split)
			b.at = w.state
			blocks[w.key] = b
		}
		return diffRenderedMsg{work: work, blocks: blocks}
	}
}

func (m *Model) diffRendered(msg diffRenderedMsg) tea.Cmd {
	if m.diffRunning != msg.work {
		return nil
	}
	m.diffRunning = nil
	d := &m.diff
	if msg.work.tab == tabCommits {
		d = &m.commit.diff
	}
	if d.rev != msg.work.rev {
		return nil
	}
	if d.blocks == nil {
		d.blocks = make(map[blockKey]block)
	}
	// Keep results for another file in this patch, but don't overwrite a
	// newer layout unless it's the one still requested on screen.
	for k, b := range msg.blocks {
		if _, ok := d.blocks[k]; !ok || m.diffWanted == msg.work {
			d.blocks[k] = b
		}
	}
	if m.tab == msg.work.tab && m.diffWanted != nil {
		top := m.diffTop
		m.syncContent()
		if m.diffWanted == nil {
			m.view.SetYOffset(top)
			if m.tab == tabFiles {
				if m.diffCross != 0 {
					m.landAcrossFile(m.diffCross)
				}
				return m.finishJump()
			}
		}
	}
	return nil
}
