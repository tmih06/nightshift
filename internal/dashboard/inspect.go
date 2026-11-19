package dashboard

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/cluster"
)

func (m model) inspectPageSize() int {
	return max(m.layout().shellH-3, 1)
}

func (m *model) refreshInspectCache() {
	if !m.isWarmSelected() {
		m.inspect = inspectCache{}
		return
	}
	innerW := max(m.layout().colW-4, 1)
	if m.inspect.lines != nil && m.inspect.pod == m.selected && m.inspect.width == innerW && m.inspect.rev == m.dataRev {
		return
	}
	m.inspect = inspectCache{
		pod:   m.selected,
		width: innerW,
		rev:   m.dataRev,
		lines: m.warmInspectLines(innerW),
	}
}

func (m model) cachedInspectLines(innerW int) []string {
	if m.inspect.lines != nil && m.inspect.pod == m.selected && m.inspect.width == innerW && m.inspect.rev == m.dataRev {
		return m.inspect.lines
	}
	return m.warmInspectLines(innerW)
}

func (m *model) scrollInspect(delta int) {
	m.refreshInspectCache()
	maxOffset := max(len(m.inspect.lines)-m.inspectPageSize(), 0)
	m.inspectScroll = clamp(m.inspectScroll+delta, 0, maxOffset)
}

func (m model) warmInspectView(l layout) []string {
	innerW := max(l.colW-4, 1)
	pageH := max(l.shellH-3, 1)

	doc := m.cachedInspectLines(innerW)
	total := len(doc)
	offset := clamp(m.inspectScroll, 0, max(total-pageH, 0))
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

func (m model) warmInspectLines(width int) []string {
	if width < 1 {
		width = 1
	}
	b := m.selectedBox()
	if b == nil {
		return []string{dim("nothing selected")}
	}
	return renderMarkdown(buildWarmMarkdown(m.pool, b), width)
}

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

	addYAML := func(kind, name, body string) {
		if body == "" {
			return
		}
		if name != "" {
			kind += " · " + name
		}
		sections = append(sections, "## "+kind+"\n```yaml\n"+body+"\n```")
	}
	addYAML("SandboxTemplate", p.Template, p.TemplateYAML)
	addYAML("SandboxWarmPool", p.Name, p.ManifestYAML)
	addYAML("Sandbox", b.Name, b.ManifestYAML)
	addYAML("Pod", b.Name, b.PodYAML)

	return strings.Join(sections, "\n\n---\n\n")
}

func renderMarkdown(md string, width int) []string {
	if width < 1 {
		return nil
	}
	divider := dim(strings.Repeat("─", width))
	var out []string
	var fenceBuf []string
	inFence, fenceLang := false, ""

	flushFence := func() {
		if fenceLang == "yaml" || fenceLang == "yml" {
			for _, ln := range fenceBuf {
				out = append(out, highlightYAMLLine(ln, width)...)
			}
		} else {
			for _, ln := range fenceBuf {
				for _, ch := range splitWidth(ln, width, width) {
					out = append(out, truncateANSI(mdCodeStyle.Render(ch), width))
				}
			}
		}
		fenceBuf, fenceLang = nil, ""
	}

	for _, raw := range strings.Split(md, "\n") {
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
			out = append(out, divider)
		case strings.HasPrefix(trimmed, "### "):
			out = append(out, truncateANSI(mdH3Style.Render(trimmed[4:]), width))
		case strings.HasPrefix(trimmed, "## "):
			out = append(out, truncateANSI(mdH2Style.Render(trimmed[3:]), width))
		case strings.HasPrefix(trimmed, "# "):
			out = append(out, truncateANSI(mdH1Style.Render(trimmed[2:]), width))
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			out = append(out, wrapInlineMarkdown(trimmed[2:], width, dim("• "), "  ")...)
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
	if len(s) < 3 || (s[0] != '-' && s[0] != '*' && s[0] != '_') {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] && s[i] != ' ' {
			return false
		}
	}
	return true
}

func wrapInlineMarkdown(s string, width int, firstPrefix, contPrefix string) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{firstPrefix}
	}
	firstLimit := max(width-lipgloss.Width(firstPrefix), 1)
	contLimit := max(width-lipgloss.Width(contPrefix), 1)

	var lines []string
	var buf strings.Builder
	curW, limit, prefix := 0, firstLimit, firstPrefix
	for _, w := range words {
		ww := visibleInlineWidth(w)
		if curW > 0 && curW+1+ww > limit {
			lines = append(lines, truncateANSI(prefix+renderInlineMarkdown(buf.String()), width))
			buf.Reset()
			curW, limit, prefix = 0, contLimit, contPrefix
		}
		if curW > 0 {
			buf.WriteByte(' ')
			curW++
		}
		buf.WriteString(w)
		curW += ww
	}
	if buf.Len() > 0 {
		lines = append(lines, truncateANSI(prefix+renderInlineMarkdown(buf.String()), width))
	}
	return lines
}

// visibleInlineWidth returns the rendered width of an inline-markdown
// token without allocating styled strings during wrapping.
func visibleInlineWidth(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "**") {
			if end := strings.Index(s[i+2:], "**"); end >= 0 {
				n += lipgloss.Width(s[i+2 : i+2+end])
				i += end + 4
				continue
			}
		}
		if s[i] == '`' {
			if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
				n += lipgloss.Width(s[i+1 : i+1+end])
				i += end + 2
				continue
			}
		}
		n++
		i++
	}
	return n
}

func renderInlineMarkdown(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "**") {
			if end := strings.Index(s[i+2:], "**"); end >= 0 {
				b.WriteString(nameStyle.Render(s[i+2 : i+2+end]))
				i += end + 4
				continue
			}
		}
		if s[i] == '`' {
			if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
				b.WriteString(mdCodeStyle.Render(s[i+1 : i+1+end]))
				i += end + 2
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
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
	indent, rest := raw[:indentLen], raw[indentLen:]
	if strings.HasPrefix(rest, "#") {
		chunks := splitWidth(raw, width, width)
		out := make([]string, len(chunks))
		for i, ch := range chunks {
			out[i] = truncateANSI(dim(ch), width)
		}
		return out
	}

	listPrefix := ""
	listW := 0
	if strings.HasPrefix(rest, "- ") {
		listPrefix, listW, rest = dim("- "), 2, rest[2:]
	} else if rest == "-" {
		return []string{truncateANSI(indent+dim("-"), width)}
	}

	if colon := yamlKeyColon(rest); colon > 0 {
		key, after := rest[:colon], strings.TrimSpace(rest[colon+1:])
		styledKey := indent + listPrefix + yamlKeyStyle.Render(key) + dim(":")
		keyW := indentLen + listW + len(key) + 1
		if after == "" {
			return []string{truncateANSI(styledKey, width)}
		}
		if keyW+1+len(after) <= width {
			return []string{styledKey + " " + styleYAMLValue(after)}
		}
		contIndent := indent + strings.Repeat(" ", 2+listW)
		chunks := splitWidth(after, max(width-keyW-1, 1), max(width-len(contIndent), 1))
		lines := make([]string, 0, len(chunks))
		lines = append(lines, truncateANSI(styledKey+" "+styleYAMLValue(chunks[0]), width))
		for _, ch := range chunks[1:] {
			lines = append(lines, truncateANSI(contIndent+styleYAMLValue(ch), width))
		}
		return lines
	}

	prefixW := indentLen + listW
	contIndent := indent + "  "
	chunks := splitWidth(rest, max(width-prefixW, 1), max(width-len(contIndent), 1))
	lines := make([]string, 0, len(chunks))
	lines = append(lines, truncateANSI(indent+listPrefix+styleYAMLValue(chunks[0]), width))
	for _, ch := range chunks[1:] {
		lines = append(lines, truncateANSI(contIndent+styleYAMLValue(ch), width))
	}
	return lines
}

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
	base := s
	for _, suf := range []string{"Mi", "Gi", "Ki", "m", "n", "s"} {
		if strings.HasSuffix(base, suf) && len(base) > len(suf) {
			base = strings.TrimSuffix(base, suf)
			break
		}
	}
	digits := 0
	for i, r := range base {
		if (i == 0 && (r == '-' || r == '+')) || r == '.' {
			continue
		}
		if !unicode.IsDigit(r) {
			return false
		}
		digits++
	}
	return digits > 0
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
	// Fast path for plain ASCII strings: byte length equals cell width.
	plainASCII := true
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' || s[i] >= 0x80 {
			plainASCII = false
			break
		}
	}
	if plainASCII {
		if len(s) <= width {
			return s
		}
		if width == 1 {
			return "…"
		}
		return s[:width-1] + "…"
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}
