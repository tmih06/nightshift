package repoview

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/ghdash/config"
	"github.com/tmih06/nightshift/internal/ghdash/tui/constants"
	ghctx "github.com/tmih06/nightshift/internal/ghdash/tui/context"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
)

type uiStyles struct {
	faint           lipgloss.Style
	faintBold       lipgloss.Style
	faintItalic     lipgloss.Style
	faintPadLeft    lipgloss.Style
	secondary       lipgloss.Style
	primary         lipgloss.Style
	primaryBold     lipgloss.Style
	success         lipgloss.Style
	successBold     lipgloss.Style
	warning         lipgloss.Style
	errStyle        lipgloss.Style
	openPR          lipgloss.Style
	openPRBold      lipgloss.Style
	mergedPR        lipgloss.Style
	closedPR        lipgloss.Style
	faintBorder     lipgloss.Style
	primaryBorder   lipgloss.Style
	secondaryBorder lipgloss.Style
	mutedPill       lipgloss.Style
	pillSuccess     lipgloss.Style
	pillWarning     lipgloss.Style
	pillError       lipgloss.Style
	pillFaint       lipgloss.Style
	pillMerged      lipgloss.Style
	pillClosed      lipgloss.Style
	selectedBg      lipgloss.Style

	footerMetaPrimary   lipgloss.Style
	footerMetaSecondary lipgloss.Style
	footerMetaDot       string
	footerMetaSep       string
	footerHintFaint     lipgloss.Style
	footerHintErr       lipgloss.Style
	footerHintSuccess   lipgloss.Style

	// Pre-rendered constant glyphs & widths.
	divDrag     string
	divActive   string
	divInactive string

	dotSuccess        string
	dotSuccessW       int
	lockWarning       string
	branchIcon        string
	branchDefaultIcon string
	fileIcon          string
	dirIcon           string
	parentDirIcon     string
	dirMeta           string
	parentMeta        string
	slashActive       string
	fileSep           string

	prOpenIcon        string
	prDraftIcon       string
	prMergedIcon      string
	prClosedIcon      string
	issueOpenIcon     string
	issueClosedIcon   string
	checksSuccessIcon string
	checksFailureIcon string
	checksWaitingIcon string
	runWaitingIcon    string
	runSuccessIcon    string
	runFailureIcon    string
	runCancelledIcon  string
	runActionReqIcon  string

	chevClosedFaint string
	chevOpenFaint   string
	chevClosedFocus string
	chevOpenFocus   string
	chevW           int

	// Repo visibility glyphs indexed by: 0=public, 1=private, 2=fork, 3=archived.
	repoGlyphNorm  [4]string
	repoGlyphFocus [4]string
	repoGlyphW     [4]int

	// Scope icons and pre-rendered scope tab strips indexed by Scope.
	scopeIconFaint   [5]string
	scopeIconSuccess [5]string
	scopeIconW       [5]int
	scopeTabStrip    [5]string
	scopeTabSpans    [5][]scopeTabSpan

	// Pre-rendered pane header badges indexed by 1..3.
	paneBadgeFocus [4]string
	paneBadgeBlur  [4]string
	paneBadgeW     [4]int

	// Pre-rendered static footer parts & hit-test widths.
	footerPrefix  string
	footerPrefixW int
	footerHelpInd string
	footerHelpW   int
	footerNotifW  int
	footerSepW    int
	footerPRsW    int
	footerIssuesW int
	footerReposW  int
}

func newUIStyles(th ghtheme.Theme, styles ghctx.Styles) uiStyles {
	base := lipgloss.NewStyle()
	footerBg := styles.Common.FooterStyle

	u := uiStyles{
		faint:           base.Foreground(th.FaintText),
		faintBold:       base.Foreground(th.FaintText).Bold(true),
		faintItalic:     base.Foreground(th.FaintText).Italic(true),
		faintPadLeft:    base.Foreground(th.FaintText).PaddingLeft(1),
		secondary:       base.Foreground(th.SecondaryText),
		primary:         base.Foreground(th.PrimaryText),
		primaryBold:     base.Foreground(th.PrimaryText).Bold(true),
		success:         base.Foreground(th.SuccessText),
		successBold:     base.Foreground(th.SuccessText).Bold(true),
		warning:         base.Foreground(th.WarningText),
		errStyle:        base.Foreground(th.ErrorText),
		openPR:          base.Foreground(styles.Colors.OpenPR),
		openPRBold:      base.Foreground(styles.Colors.OpenPR).Bold(true),
		mergedPR:        base.Foreground(styles.Colors.MergedPR),
		closedPR:        base.Foreground(styles.Colors.ClosedPR),
		faintBorder:     base.Foreground(th.FaintBorder),
		primaryBorder:   base.Foreground(th.PrimaryBorder),
		secondaryBorder: base.Foreground(th.SecondaryBorder),
		mutedPill:       base.Background(th.SelectedBackground).Foreground(th.SecondaryText).Padding(0, 1),
		pillSuccess:     base.Background(th.SuccessText).Foreground(th.InvertedText).Bold(true).Padding(0, 1),
		pillWarning:     base.Background(th.WarningText).Foreground(th.InvertedText).Bold(true).Padding(0, 1),
		pillError:       base.Background(th.ErrorText).Foreground(th.InvertedText).Bold(true).Padding(0, 1),
		pillFaint:       base.Background(th.FaintText).Foreground(th.InvertedText).Bold(true).Padding(0, 1),
		pillMerged:      base.Background(styles.Colors.MergedPR).Foreground(th.InvertedText).Bold(true).Padding(0, 1),
		pillClosed:      base.Background(styles.Colors.ClosedPR).Foreground(th.InvertedText).Bold(true).Padding(0, 1),
		selectedBg:      base.Background(th.SelectedBackground),

		footerMetaPrimary:   footerBg.Foreground(th.PrimaryText),
		footerMetaSecondary: footerBg.Foreground(th.SecondaryText),
		footerMetaDot:       footerBg.Foreground(th.FaintText).Render(" • "),
		footerMetaSep:       footerBg.Foreground(th.FaintBorder).Render(" │ "),
		footerHintFaint:     footerBg.Foreground(th.FaintText),
		footerHintErr:       footerBg.Foreground(th.ErrorText).Bold(true),
		footerHintSuccess:   footerBg.Foreground(th.SuccessText),
	}

	u.divDrag = u.success.Render("│")
	u.divActive = u.secondaryBorder.Render("│")
	u.divInactive = u.primaryBorder.Render("│")

	u.dotSuccess = u.success.Render("●")
	u.dotSuccessW = ansi.StringWidth("●")
	u.lockWarning = u.warning.Render("")
	u.branchIcon = u.secondary.Render("")
	u.branchDefaultIcon = u.success.Render("")
	u.fileIcon = u.faint.Render("")
	u.dirIcon = u.openPR.Render("")
	u.parentDirIcon = u.secondary.Render("")
	u.dirMeta = u.faint.Render("/")
	u.parentMeta = u.faint.Render("parent")
	u.slashActive = u.successBold.Render("/")
	u.fileSep = u.faintBorder.Render(" │ ")

	u.prOpenIcon = u.success.Render(constants.OpenIcon)
	u.prDraftIcon = u.faint.Render(constants.DraftIcon)
	u.prMergedIcon = u.mergedPR.Render(constants.MergedIcon)
	u.prClosedIcon = u.closedPR.Render(constants.ClosedIcon)
	u.issueOpenIcon = u.success.Render("")
	u.issueClosedIcon = u.mergedPR.Render("")
	u.checksSuccessIcon = u.success.Render(constants.SuccessIcon)
	u.checksFailureIcon = u.errStyle.Render(constants.FailureIcon)
	u.checksWaitingIcon = u.warning.Render(constants.WaitingIcon)
	u.runWaitingIcon = u.warning.Render(constants.WaitingIcon)
	u.runSuccessIcon = u.success.Render(constants.SuccessIcon)
	u.runFailureIcon = u.errStyle.Render(constants.FailureIcon)
	u.runCancelledIcon = u.faint.Render("⊘")
	u.runActionReqIcon = u.warning.Render(constants.ActionRequiredIcon)

	u.chevClosedFaint = u.faint.Render("▸")
	u.chevOpenFaint = u.faint.Render("▾")
	u.chevClosedFocus = u.successBold.Render("▸")
	u.chevOpenFocus = u.successBold.Render("▾")
	u.chevW = ansi.StringWidth("▸")

	rawRepoGlyphs := [4]string{"", "", "", ""}
	for i, g := range rawRepoGlyphs {
		u.repoGlyphNorm[i] = u.secondary.Render(g)
		u.repoGlyphFocus[i] = u.success.Render(g)
		u.repoGlyphW[i] = ansi.StringWidth(g)
	}

	for _, sc := range allScopes {
		icon := sc.Icon()
		u.scopeIconFaint[sc] = u.faint.Render(icon)
		u.scopeIconSuccess[sc] = u.success.Render(icon)
		u.scopeIconW[sc] = ansi.StringWidth(icon)
	}

	sep := u.faintBorder.Render(" │ ")
	sepW := ansi.StringWidth(" │ ")
	for _, active := range allScopes {
		pos := 1
		var b strings.Builder
		b.WriteByte(' ')
		spans := make([]scopeTabSpan, 0, len(allScopes))
		for i, sc := range allScopes {
			if i > 0 {
				b.WriteString(sep)
				pos += sepW
			}
			label := sc.ShortLabel()
			var rendered string
			if sc == active {
				rendered = u.successBold.Render(label)
			} else {
				rendered = u.faint.Render(label)
			}
			w := ansi.StringWidth(label)
			spans = append(spans, scopeTabSpan{scope: sc, start: pos, end: pos + w})
			b.WriteString(rendered)
			pos += w
		}
		u.scopeTabStrip[active] = b.String()
		u.scopeTabSpans[active] = spans
	}

	for i := 1; i <= 3; i++ {
		txt := fmt.Sprintf("[%d]", i)
		u.paneBadgeFocus[i] = u.successBold.Render(txt)
		u.paneBadgeBlur[i] = u.faint.Render(txt)
		u.paneBadgeW[i] = len(txt)
	}

	// Precompute static footer switcher prefix & hit-test widths.
	renderViewBtn := func(view config.ViewType) string {
		var icon, label string
		switch view {
		case config.NotificationsView:
			icon = ""
		case config.PRsView:
			icon = ""
			label = " PRs"
		case config.IssuesView:
			icon = ""
			label = " Issues"
		case config.RepoView:
			icon = ""
			label = " Repos"
		}
		if view == config.RepoView {
			activeStyle := base.
				Foreground(th.SuccessText).
				Background(styles.ViewSwitcher.ActiveView.GetBackground()).
				Bold(true)
			return activeStyle.Render(icon) + activeStyle.Render(label)
		}
		return styles.ViewSwitcher.InactiveView.Render(icon + label)
	}

	fSep := styles.ViewSwitcher.ViewsSeparator.Render(footerViewSeparator)
	fNotif := styles.ViewSwitcher.ViewsSeparator.PaddingLeft(1).Render(renderViewBtn(config.NotificationsView))
	fPRs := renderViewBtn(config.PRsView)
	fIssues := renderViewBtn(config.IssuesView)
	fRepos := renderViewBtn(config.RepoView)
	fCap := base.
		Background(footerBg.GetBackground()).
		Foreground(styles.ViewSwitcher.ViewsSeparator.GetBackground()).
		Render(" ")

	u.footerPrefix = fNotif + fSep + fPRs + fSep + fIssues + fSep + fRepos + fCap
	u.footerPrefixW = ansi.StringWidth(u.footerPrefix)
	u.footerHelpInd = base.
		Background(th.FaintText).
		Foreground(th.SelectedBackground).
		Padding(0, 1).
		Render("? help")
	u.footerHelpW = ansi.StringWidth(u.footerHelpInd)
	u.footerNotifW = ansi.StringWidth(fNotif)
	u.footerSepW = ansi.StringWidth(fSep)
	u.footerPRsW = ansi.StringWidth(fPRs)
	u.footerIssuesW = ansi.StringWidth(fIssues)
	u.footerReposW = ansi.StringWidth(fRepos)

	return u
}
