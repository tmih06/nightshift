package dashboard

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/cluster"
)

// updateInspectKeys handles keyboard scrolling while the warm sandbox's
// config/template viewer has focus.
func (m model) updateInspectKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.isWarmSelected() {
		m.focus = focusNav
		return m, nil
	}
	page := max(m.inspectPageSize()/2, 1)
	switch msg.String() {
	case "esc", "tab":
		m.focus = focusNav
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.scrollInspect(-1)
	case "down", "j":
		m.scrollInspect(1)
	case "pgup", "ctrl+u":
		m.scrollInspect(-page)
	case "pgdown", "ctrl+d":
		m.scrollInspect(page)
	case "g", "home":
		m.inspectScroll = 0
	case "G", "end":
		m.scrollInspect(1 << 20)
	case "c":
		m.focus = focusNav
		m.flash("claiming from the warm pool…")
		return m, claimCmd()
	case "r":
		m.status, m.statusTTL = "", 0
		return m, fetchCmd()
	}
	return m, nil
}

// inspectPageSize returns the number of visible content rows inside the
// warm sandbox viewer (excluding borders and the 1-row pane header).
func (m model) inspectPageSize() int {
	l := m.layout()
	return max(l.shellH-3, 1)
}

// scrollInspect moves the warm viewer's scroll offset and clamps it to
// the rendered document bounds.
func (m *model) scrollInspect(delta int) {
	l := m.layout()
	innerW := max(l.colW-4, 1)
	total := len(m.warmInspectLines(innerW))
	maxOffset := max(total-m.inspectPageSize(), 0)
	m.inspectScroll = clamp(m.inspectScroll+delta, 0, maxOffset)
}

// warmInspectView renders the warm sandbox's lower-right pane: a 1-row
// header with title and scroll position, followed by the visible slice
// of the styled configs and templates document.
func (m model) warmInspectView() []string {
	l := m.layout()
	innerW := max(l.colW-4, 1)
	pageH := m.inspectPageSize()

	doc := m.warmInspectLines(innerW)
	total := len(doc)
	maxOffset := max(total-pageH, 0)
	offset := clamp(m.inspectScroll, 0, maxOffset)

	end := min(offset+pageH, total)
	pos := ""
	if total > pageH {
		pos = fmt.Sprintf(" %d-%d/%d", offset+1, end, total)
	}

	var title string
	if m.focus == focusInspect {
		title = cursorStyle.Render("▶ configs & templates") + dim(pos+" · j/k/pgup/pgdn scroll · esc leaves")
	} else {
		title = dim("configs & templates" + pos + " · scroll or click to focus")
	}

	lines := make([]string, 0, pageH+1)
	lines = append(lines, title)
	if offset < total {
		lines = append(lines, doc[offset:end]...)
	}
	return lines
}

// warmInspectLines builds the full scrollable document for the selected
// warm sandbox: a Markdown overview followed by each Kubernetes
// template/config YAML separated by a full-width divider line.
func (m model) warmInspectLines(width int) []string {
	if width < 1 {
		width = 1
	}
	b := m.selectedBox()
	if b == nil {
		return []string{dim("nothing selected")}
	}
	md := buildWarmMarkdown(m.pool, b)
	return renderMarkdown(md, width)
}

// buildWarmMarkdown assembles the warm sandbox's pool summary,
// template, warm pool manifest, sandbox manifest, and pod config as a
// Markdown document with horizontal dividers between sections.
func buildWarmMarkdown(p cluster.Pool, b *cluster.Sandbox) string {
	var sections []string

	var intro strings.Builder
	if p.Name != "" {
		fmt.Fprintf(&intro, "# %s · warm pool\n", p.Name)
		fmt.Fprintf(&intro, "- **template** `%s`\n", p.Template)
		fmt.Fprintf(&intro, "- **ready** `%d/%d`\n", p.Ready, p.Desired)
		if p.UpdateStrategy != "" {
			fmt.Fprintf(&intro, "- **update** `%s`\n", p.UpdateStrategy)
		}
	} else {
		intro.WriteString("# warm pool\n- pool unavailable\n")
	}
	intro.WriteString("\nPress `c` to claim a warm sandbox and start its bash.")
	sections = append(sections, intro.String())

	if p.TemplateYAML != "" {
		title := "SandboxTemplate"
		if p.Template != "" {
			title += " · " + p.Template
		}
		sections = append(sections, "## "+title+"\n```yaml\n"+p.TemplateYAML+"\n```")
	}
	if p.ManifestYAML != "" {
		title := "SandboxWarmPool"
		if p.Name != "" {
			title += " · " + p.Name
		}
		sections = append(sections, "## "+title+"\n```yaml\n"+p.ManifestYAML+"\n```")
	}
	if b.ManifestYAML != "" {
		sections = append(sections, "## Sandbox · "+b.Name+"\n```yaml\n"+b.ManifestYAML+"\n```")
	}
	if b.PodYAML != "" {
		sections = append(sections, "## Pod · "+b.Name+"\n```yaml\n"+b.PodYAML+"\n```")
	}

	return strings.Join(sections, "\n\n---\n\n")
}

// renderMarkdown converts a Markdown document (headings, dividers,
// lists, inline bold/code, and fenced YAML/code blocks) into styled
// terminal lines fitted to width.
func renderMarkdown(md string, width int) []string {
	if width < 1 {
		return nil
	}
	rawLines := strings.Split(md, "\n")
	var out []string
	inFence := false
	fenceLang := ""
	var fenceBuf []string

	flushFence := func() {
		src := strings.Join(fenceBuf, "\n")
		if fenceLang == "yaml" || fenceLang == "yml" {
			out = append(out, renderYAML(src, width)...)
		} else {
			for _, ln := range fenceBuf {
				out = append(out, wrapPlain(ln, width, mdCodeStyle)...)
			}
		}
		fenceBuf = nil
		fenceLang = ""
	}

	for _, raw := range rawLines {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "```") {
			if inFence {
				flushFence()
				inFence = false
			} else {
				inFence = true
				fenceLang = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "```")))
			}
			continue
		}
		if inFence {
			fenceBuf = append(fenceBuf, raw)
			continue
		}

		switch {
		case trimmed == "":
			out = append(out, "")
		case isDividerLine(trimmed):
			out = append(out, dim(strings.Repeat("─", width)))
		case strings.HasPrefix(trimmed, "### "):
			out = append(out, truncateANSI(mdH3Style.Render(strings.TrimPrefix(trimmed, "### ")), width))
		case strings.HasPrefix(trimmed, "## "):
			out = append(out, truncateANSI(mdH2Style.Render(strings.TrimPrefix(trimmed, "## ")), width))
		case strings.HasPrefix(trimmed, "# "):
			out = append(out, truncateANSI(mdH1Style.Render(strings.TrimPrefix(trimmed, "# ")), width))
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			body := trimmed[2:]
			out = append(out, wrapInlineMarkdown(body, width, dim("• "), "  ")...)
		default:
			out = append(out, wrapInlineMarkdown(trimmed, width, "", "")...)
		}
	}
	if inFence && len(fenceBuf) > 0 {
		flushFence()
	}
	return out
}

func isDividerLine(s string) bool {
	if len(s) < 3 {
		return false
	}
	ch := s[0]
	if ch != '-' && ch != '*' && ch != '_' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != ch && s[i] != ' ' {
			return false
		}
	}
	return true
}

// wrapInlineMarkdown wraps plain text to width before applying inline
// **bold** and `code` styles, keeping every line within width.
func wrapInlineMarkdown(s string, width int, firstPrefix, contPrefix string) []string {
	firstW := max(width-lipgloss.Width(firstPrefix), 1)
	contW := max(width-lipgloss.Width(contPrefix), 1)

	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{firstPrefix}
	}

	var lines []string
	cur := ""
	curLimit := firstW
	prefix := firstPrefix
	for _, w := range words {
		cand := w
		if cur != "" {
			cand = cur + " " + w
		}
		if lipgloss.Width(renderInlineMarkdown(cand)) <= curLimit || cur == "" {
			cur = cand
			continue
		}
		lines = append(lines, truncateANSI(prefix+renderInlineMarkdown(cur), width))
		cur = w
		prefix = contPrefix
		curLimit = contW
	}
	if cur != "" {
		lines = append(lines, truncateANSI(prefix+renderInlineMarkdown(cur), width))
	}
	return lines
}

// renderInlineMarkdown styles **bold** and `code` spans inline.
func renderInlineMarkdown(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "**") {
			if end := strings.Index(s[i+2:], "**"); end >= 0 {
				b.WriteString(nameStyle.Render(s[i+2 : i+2+end]))
				i += 2 + end + 2
				continue
			}
		}
		if s[i] == '`' {
			if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
				b.WriteString(mdCodeStyle.Render(s[i+1 : i+1+end]))
				i += 1 + end + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// renderYAML syntax-highlights YAML lines and wraps long values so they
// stay readable inside width without ever overflowing.
func renderYAML(src string, width int) []string {
	if width < 1 {
		return nil
	}
	var out []string
	for _, raw := range strings.Split(src, "\n") {
		out = append(out, highlightYAMLLine(raw, width)...)
	}
	return out
}

func highlightYAMLLine(raw string, width int) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []string{""}
	}
	if trimmed == "---" || trimmed == "..." {
		return []string{truncateANSI(dim(raw), width)}
	}

	indentLen := len(raw) - len(strings.TrimLeft(raw, " "))
	indent := raw[:indentLen]
	rest := raw[indentLen:]

	if strings.HasPrefix(rest, "#") {
		return wrapPlain(raw, width, dimStyle)
	}

	// Preserve leading list dash ("- ") and color the item or key after it.
	listPrefix := ""
	if strings.HasPrefix(rest, "- ") {
		listPrefix = dim("- ")
		rest = rest[2:]
	} else if rest == "-" {
		return []string{truncateANSI(indent+dim("-"), width)}
	}

	if colon := yamlKeyColon(rest); colon > 0 {
		key := rest[:colon]
		after := rest[colon+1:]
		styledKey := indent + listPrefix + yamlKeyStyle.Render(key) + dim(":")
		if strings.TrimSpace(after) == "" {
			return []string{truncateANSI(styledKey, width)}
		}
		val := strings.TrimSpace(after)
		line := styledKey + " " + styleYAMLValue(val)
		if lipgloss.Width(line) <= width {
			return []string{line}
		}
		// Wrap long scalar values cleanly onto continuation lines.
		contIndent := indent + "  "
		if listPrefix != "" {
			contIndent += "  "
		}
		avail := max(width-lipgloss.Width(styledKey)-1, 1)
		chunks := splitWidth(val, avail, max(width-len(contIndent), 1))
		lines := []string{truncateANSI(styledKey+" "+styleYAMLValue(chunks[0]), width)}
		for _, ch := range chunks[1:] {
			lines = append(lines, truncateANSI(contIndent+styleYAMLValue(ch), width))
		}
		return lines
	}

	// Plain list scalar value (e.g. "- /bin/bash").
	prefix := indent + listPrefix
	avail := max(width-lipgloss.Width(prefix), 1)
	contIndent := indent + "  "
	chunks := splitWidth(rest, avail, max(width-len(contIndent), 1))
	lines := []string{truncateANSI(prefix+styleYAMLValue(chunks[0]), width)}
	for _, ch := range chunks[1:] {
		lines = append(lines, truncateANSI(contIndent+styleYAMLValue(ch), width))
	}
	return lines
}

// yamlKeyColon finds the ":" separating a YAML mapping key from its
// value, ignoring colons inside quotes or values (like "debian:12-slim").
func yamlKeyColon(s string) int {
	if s == "" || s[0] == '"' || s[0] == '\'' {
		return -1
	}
	for i := 0; i < len(s); i++ {
		if s[i] == ':' && (i+1 == len(s) || s[i+1] == ' ') {
			return i
		}
	}
	return -1
}

func styleYAMLValue(val string) string {
	// Split trailing inline comment if present.
	comment := ""
	if idx := strings.Index(val, " #"); idx >= 0 {
		comment = " " + dim(val[idx+1:])
		val = strings.TrimSpace(val[:idx])
	}
	switch val {
	case "true", "false", "null", "~", "{}", "[]":
		return yamlBoolStyle.Render(val) + comment
	}
	if isYAMLNumberOrQuantity(val) {
		return yamlNumStyle.Render(val) + comment
	}
	return yamlStrStyle.Render(val) + comment
}

func isYAMLNumberOrQuantity(s string) bool {
	if s == "" {
		return false
	}
	// Trim Kubernetes unit suffixes like 500m, 256Mi, 1Gi, 100Ki, 30s.
	base := s
	for _, suf := range []string{"Mi", "Gi", "Ki", "m", "n", "s"} {
		if strings.HasSuffix(base, suf) && len(base) > len(suf) {
			base = strings.TrimSuffix(base, suf)
			break
		}
	}
	digits := 0
	for i, r := range base {
		if i == 0 && (r == '-' || r == '+') {
			continue
		}
		if r == '.' {
			continue
		}
		if !unicode.IsDigit(r) {
			return false
		}
		digits++
	}
	return digits > 0
}

func wrapPlain(s string, width int, style lipgloss.Style) []string {
	chunks := splitWidth(s, width, width)
	out := make([]string, 0, len(chunks))
	for _, ch := range chunks {
		out = append(out, truncateANSI(style.Render(ch), width))
	}
	return out
}

func splitWidth(s string, firstW, nextW int) []string {
	if s == "" {
		return []string{""}
	}
	runes := []rune(s)
	var out []string
	limit := max(firstW, 1)
	for len(runes) > 0 {
		if len(runes) <= limit {
			out = append(out, string(runes))
			break
		}
		out = append(out, string(runes[:limit]))
		runes = runes[limit:]
		limit = max(nextW, 1)
	}
	return out
}

func truncateANSI(s string, width int) string {
	if width < 1 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}
