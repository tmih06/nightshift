package repoview

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/zenocto/tui/paint"
)

// View renders the 3-pane repository workspace, footer, and active modal.
func (m *Model) View() tea.View {
	if m.width <= 0 || m.height <= 0 {
		return tea.NewView("")
	}

	canCacheFrame := m.modal == ModalNone && m.searchMode == SearchNone
	var fKey frameCacheKey
	if canCacheFrame {
		fKey = m.currentFrameCacheKey()
		if m.frameCache.valid && m.frameCache.key == fKey {
			return tea.NewView(m.frameCache.content)
		}
	}

	pl := m.computeLayout()

	var navLines, listLines, detLines []string
	if pl.showNav {
		navLines = m.renderNavPane(pl.navW, pl.bodyH)
	}
	if pl.showList {
		listLines = m.renderListPane(pl.listW, pl.bodyH)
	}
	if pl.showDetails {
		detLines = m.renderDetailsPane(pl.detailsW, pl.bodyH)
	}

	div1 := m.renderDividerGlyph(1, pl)
	div2 := m.renderDividerGlyph(2, pl)

	var blankNav, blankList, blankDet string
	sumW := 0
	if pl.showNav {
		sumW += pl.navW
		if len(navLines) < pl.bodyH {
			blankNav = strings.Repeat(" ", pl.navW)
		}
	}
	if pl.div1X >= 0 {
		sumW++
	}
	if pl.showList {
		sumW += pl.listW
		if len(listLines) < pl.bodyH {
			blankList = strings.Repeat(" ", pl.listW)
		}
	}
	if pl.div2X >= 0 {
		sumW++
	}
	if pl.showDetails {
		sumW += pl.detailsW
		if len(detLines) < pl.bodyH {
			blankDet = strings.Repeat(" ", pl.detailsW)
		}
	}
	exactWidth := sumW == m.width

	var fb strings.Builder
	fb.Grow(m.height * (m.width + 64))
	writtenRows := 0

	if exactWidth {
		for row := 0; row < pl.bodyH; row++ {
			if writtenRows > 0 {
				fb.WriteByte('\n')
			}
			if pl.showNav {
				if row < len(navLines) {
					fb.WriteString(navLines[row])
				} else {
					fb.WriteString(blankNav)
				}
			}
			if pl.div1X >= 0 {
				fb.WriteString(div1)
			}
			if pl.showList {
				if row < len(listLines) {
					fb.WriteString(listLines[row])
				} else {
					fb.WriteString(blankList)
				}
			}
			if pl.div2X >= 0 {
				fb.WriteString(div2)
			}
			if pl.showDetails {
				if row < len(detLines) {
					fb.WriteString(detLines[row])
				} else {
					fb.WriteString(blankDet)
				}
			}
			writtenRows++
		}
	} else {
		var rowBuf strings.Builder
		for row := 0; row < pl.bodyH; row++ {
			if writtenRows > 0 {
				fb.WriteByte('\n')
			}
			rowBuf.Reset()
			if pl.showNav {
				rowBuf.WriteString(lineOrBlank(navLines, row, pl.navW))
			}
			if pl.div1X >= 0 {
				rowBuf.WriteString(div1)
			}
			if pl.showList {
				rowBuf.WriteString(lineOrBlank(listLines, row, pl.listW))
			}
			if pl.div2X >= 0 {
				rowBuf.WriteString(div2)
			}
			if pl.showDetails {
				rowBuf.WriteString(lineOrBlank(detLines, row, pl.detailsW))
			}
			fb.WriteString(padLine(rowBuf.String(), m.width))
			writtenRows++
		}
	}

	footerLines := m.renderFooterLines(m.width)
	for _, fl := range footerLines {
		if writtenRows >= m.height {
			break
		}
		if writtenRows > 0 {
			fb.WriteByte('\n')
		}
		fb.WriteString(fl)
		writtenRows++
	}
	if writtenRows < m.height {
		blankFull := strings.Repeat(" ", m.width)
		for writtenRows < m.height {
			if writtenRows > 0 {
				fb.WriteByte('\n')
			}
			fb.WriteString(blankFull)
			writtenRows++
		}
	}

	frame := fb.String()
	if m.modal != ModalNone {
		frame = m.renderModalOverlay(frame)
		frame = normalizeFrame(frame, m.width, m.height)
	} else if canCacheFrame {
		// Recompute key after render in case clamping updated navScroll/listScroll/detailsScroll.
		m.frameCache = frameCacheState{
			valid:   true,
			key:     m.currentFrameCacheKey(),
			content: frame,
		}
	}

	return tea.NewView(frame)
}

func (m *Model) renderDividerGlyph(div int, pl paneLayout) string {
	active := m.draggingDiv == div
	if !active {
		switch div {
		case 1:
			active = m.activePane == PaneNav || (m.activePane == PaneList && pl.showNav)
		case 2:
			active = m.activePane == PaneDetails || (m.activePane == PaneList && pl.showDetails)
		}
	}
	if m.draggingDiv == div {
		return m.ui.divDrag
	}
	if active {
		return m.ui.divActive
	}
	return m.ui.divInactive
}

func (m *Model) renderPaneHeader(index int, title, right string, width int, focused bool) string {
	if width <= 0 {
		return ""
	}
	var badge string
	var badgeW int
	if index >= 1 && index <= 3 {
		if focused {
			badge = m.ui.paneBadgeFocus[index]
		} else {
			badge = m.ui.paneBadgeBlur[index]
		}
		badgeW = m.ui.paneBadgeW[index]
	}

	prefix := " " + badge + " "
	prefixW := 1 + badgeW + 1

	suffix := ""
	suffixW := 0
	if right != "" {
		suffix = " " + m.ui.faint.Render(right) + " "
		suffixW = 1 + ansi.StringWidth(right) + 1
	}

	avail := width - prefixW - suffixW
	if avail < 2 {
		suffix = ""
		suffixW = 0
		avail = max(0, width-prefixW)
	}

	truncTitle := title
	titleW := ansi.StringWidth(title)
	if titleW > avail {
		truncTitle = ansi.Truncate(title, avail, "…")
		titleW = ansi.StringWidth(truncTitle)
	}

	var lbl string
	if focused {
		lbl = m.ui.primaryBold.Render(truncTitle)
	} else {
		lbl = m.ui.secondary.Render(truncTitle)
	}

	totalW := prefixW + titleW + suffixW
	var line string
	if totalW <= width {
		line = prefix + lbl + strings.Repeat(" ", width-totalW) + suffix
	} else {
		line = padLine(prefix+lbl+suffix, width)
	}
	return paint.OnBackground(line, m.theme.SelectedBackground)
}

func (m *Model) paintRow(line string, width int, selected bool) string {
	line = padLine(line, width)
	if selected {
		return paint.OnBackground(line, m.theme.SelectedBackground)
	}
	return line
}

func (m *Model) paintExactRow(line string, selected bool) string {
	if selected {
		return paint.OnBackground(line, m.theme.SelectedBackground)
	}
	return line
}

func padLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := ansi.StringWidth(s)
	if w > width {
		return ansi.Truncate(s, width, "")
	}
	if w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

func lineOrBlank(lines []string, idx, width int) string {
	if idx >= 0 && idx < len(lines) {
		return padLine(lines[idx], width)
	}
	return strings.Repeat(" ", max(0, width))
}

func normalizeFrame(frame string, width, height int) string {
	raw := strings.Split(frame, "\n")
	out := make([]string, height)
	for i := 0; i < height; i++ {
		if i < len(raw) {
			out[i] = padLine(raw[i], width)
		} else {
			out[i] = strings.Repeat(" ", width)
		}
	}
	return strings.Join(out, "\n")
}

func clampScroll(scroll, cursor, total, height int) int {
	if height <= 0 || total <= height {
		return 0
	}
	if cursor < scroll {
		scroll = cursor
	}
	if cursor >= scroll+height {
		scroll = cursor - height + 1
	}
	maxScroll := max(0, total-height)
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	return scroll
}
