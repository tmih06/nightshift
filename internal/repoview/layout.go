package repoview

const (
	wideBreakpoint   = 110
	mediumBreakpoint = 75

	defaultNavFrac  = 0.22
	defaultListFrac = 0.36

	minNavWidth     = 18
	minListWidth    = 24
	minDetailsWidth = 28
)

type layoutMode int

const (
	layoutWide layoutMode = iota
	layoutMedium
	layoutNarrow
)

type paneLayout struct {
	mode layoutMode

	showNav     bool
	showList    bool
	showDetails bool

	navX, navW         int
	div1X              int // -1 when absent
	listX, listW       int
	div2X              int // -1 when absent
	detailsX, detailsW int

	bodyH   int
	footerH int
}

func (m *Model) computeLayout() paneLayout {
	w := max(0, m.width)
	h := max(0, m.height)
	fh := m.footerHeight()
	if fh > h {
		fh = h
	}
	bodyH := max(0, h-fh)

	pl := paneLayout{
		div1X:   -1,
		div2X:   -1,
		bodyH:   bodyH,
		footerH: fh,
	}

	if w == 0 {
		return pl
	}

	switch {
	case w >= wideBreakpoint:
		pl.mode = layoutWide
		pl.showNav = !m.hideNav
		pl.showList = !m.zoomDetails
		pl.showDetails = !m.hideDetails || m.zoomDetails
	case w >= mediumBreakpoint:
		pl.mode = layoutMedium
		if m.zoomDetails {
			pl.showDetails = true
		} else if m.activePane == PaneNav && !m.hideNav {
			pl.showNav = true
			pl.showList = true
		} else {
			pl.showList = true
			pl.showDetails = !m.hideDetails
			if !pl.showDetails && !m.hideNav {
				pl.showNav = true
			}
		}
	default:
		pl.mode = layoutNarrow
		if m.zoomDetails {
			pl.showDetails = true
		} else {
			switch m.activePane {
			case PaneNav:
				pl.showNav = true
			case PaneDetails:
				pl.showDetails = true
			default:
				pl.showList = true
			}
		}
	}

	// Ensure at least one pane is visible.
	if !pl.showNav && !pl.showList && !pl.showDetails {
		pl.showList = true
	}

	// Distribute widths cleanly so visible panes + 1-col dividers == w.
	count := 0
	if pl.showNav {
		count++
	}
	if pl.showList {
		count++
	}
	if pl.showDetails {
		count++
	}

	switch count {
	case 1:
		switch {
		case pl.showNav:
			pl.navX, pl.navW = 0, w
		case pl.showList:
			pl.listX, pl.listW = 0, w
		case pl.showDetails:
			pl.detailsX, pl.detailsW = 0, w
		}
	case 2:
		avail := max(0, w-1)
		switch {
		case pl.showNav && pl.showList:
			navW := int(float64(avail) * clampFloat(m.navFrac/(m.navFrac+m.listFrac), 0.20, 0.55))
			navW = clampPaneWidth(navW, minNavWidth, avail-minListWidth)
			listW := max(0, avail-navW)
			pl.navX, pl.navW = 0, navW
			pl.div1X = navW
			pl.listX, pl.listW = navW+1, listW
		case pl.showList && pl.showDetails:
			detailsFrac := max(0.20, 1.0-m.navFrac-m.listFrac)
			listW := int(float64(avail) * clampFloat(m.listFrac/(m.listFrac+detailsFrac), 0.25, 0.65))
			listW = clampPaneWidth(listW, minListWidth, avail-minDetailsWidth)
			detW := max(0, avail-listW)
			pl.listX, pl.listW = 0, listW
			pl.div2X = listW
			pl.detailsX, pl.detailsW = listW+1, detW
		case pl.showNav && pl.showDetails:
			navW := int(float64(avail) * clampFloat(m.navFrac, 0.18, 0.45))
			navW = clampPaneWidth(navW, minNavWidth, avail-minDetailsWidth)
			detW := max(0, avail-navW)
			pl.navX, pl.navW = 0, navW
			pl.div1X = navW
			pl.detailsX, pl.detailsW = navW+1, detW
		}
	case 3:
		avail := max(0, w-2)
		navW := int(float64(avail) * clampFloat(m.navFrac, 0.14, 0.40))
		navW = clampPaneWidth(navW, minNavWidth, avail-minListWidth-minDetailsWidth)
		rem := max(0, avail-navW)
		listW := int(float64(avail) * clampFloat(m.listFrac, 0.20, 0.55))
		listW = clampPaneWidth(listW, minListWidth, rem-minDetailsWidth)
		detW := max(0, rem-listW)

		pl.navX, pl.navW = 0, navW
		pl.div1X = navW
		pl.listX, pl.listW = navW+1, listW
		pl.div2X = navW + 1 + listW
		pl.detailsX, pl.detailsW = pl.div2X+1, detW
	}

	return pl
}

func clampPaneWidth(v, lo, hi int) int {
	if hi < lo {
		if hi < 1 {
			return max(1, v)
		}
		return hi
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (pl paneLayout) dividerAt(x int) int {
	if pl.div1X >= 0 && x == pl.div1X {
		return 1
	}
	if pl.div2X >= 0 && x == pl.div2X {
		return 2
	}
	return 0
}

func (pl paneLayout) paneAt(x int) (Pane, bool) {
	if pl.showNav && x >= pl.navX && x < pl.navX+pl.navW {
		return PaneNav, true
	}
	if pl.showList && x >= pl.listX && x < pl.listX+pl.listW {
		return PaneList, true
	}
	if pl.showDetails && x >= pl.detailsX && x < pl.detailsX+pl.detailsW {
		return PaneDetails, true
	}
	return PaneNav, false
}

func (m *Model) resizeDivider(div int, x int) {
	pl := m.computeLayout()
	if m.width < 10 {
		return
	}
	if div == 1 && pl.div1X >= 0 {
		if pl.showNav && pl.showList && pl.showDetails {
			avail := float64(max(1, m.width-2))
			newNavW := clampPaneWidth(x, minNavWidth, max(minNavWidth, m.width-2-minListWidth-minDetailsWidth))
			m.navFrac = clampFloat(float64(newNavW)/avail, 0.12, 0.45)
		} else if pl.showNav && pl.showList {
			avail := float64(max(1, m.width-1))
			newNavW := clampPaneWidth(x, minNavWidth, max(minNavWidth, m.width-1-minListWidth))
			ratio := clampFloat(float64(newNavW)/avail, 0.18, 0.55)
			sum := m.navFrac + m.listFrac
			if sum <= 0 {
				sum = defaultNavFrac + defaultListFrac
			}
			m.navFrac = ratio * sum
			m.listFrac = (1 - ratio) * sum
		} else if pl.showNav && pl.showDetails {
			avail := float64(max(1, m.width-1))
			newNavW := clampPaneWidth(x, minNavWidth, max(minNavWidth, m.width-1-minDetailsWidth))
			m.navFrac = clampFloat(float64(newNavW)/avail, 0.15, 0.50)
		}
	} else if div == 2 && pl.div2X >= 0 {
		if pl.showNav && pl.showList && pl.showDetails {
			avail := float64(max(1, m.width-2))
			newListW := clampPaneWidth(x-pl.listX, minListWidth, max(minListWidth, m.width-2-pl.navW-minDetailsWidth))
			m.listFrac = clampFloat(float64(newListW)/avail, 0.18, 0.60)
		} else if pl.showList && pl.showDetails {
			avail := float64(max(1, m.width-1))
			newListW := clampPaneWidth(x, minListWidth, max(minListWidth, m.width-1-minDetailsWidth))
			ratio := clampFloat(float64(newListW)/avail, 0.22, 0.68)
			rem := max(0.20, 1.0-m.navFrac)
			m.listFrac = ratio * rem
		}
	}
}
