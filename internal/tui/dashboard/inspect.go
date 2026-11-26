package dashboard

import (
	"fmt"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tmih06/nightshift/internal/tui/fleet"
)

func (m model) inspectPageSize() int {
	return max(m.layout().docH, 1)
}

// cachedInspectLines reuses documents until their content, width, or theme
// changes. Fleet refreshes and other pods' log replies do not invalidate them.
func (m model) cachedInspectLines(innerW int) []string {
	b := m.selectedBox()
	if b == nil {
		return []string{dim("nothing selected")}
	}
	mode := m.effectivePane()
	cacheKey := inspectKey{pod: m.selected, mode: mode}
	cached, ok := m.inspectDocs[cacheKey]
	matchesSize := ok && cached.width == innerW && cached.theme == themeRevision
	if mode == paneConfigs && matchesSize && cached.rev == m.dataRev {
		return cached.lines
	}
	var content inspectContent
	if mode == paneLogs {
		content.text, content.loaded = m.logs[b.Name]
		content.logErr = m.logErr[b.Name]
		content.loading = !content.loaded && m.loadingLogs == b.Name
		content.role = b.PodRole()
	} else {
		content.text = buildPodMarkdown(m.pools[b.Owner], b)
	}
	if matchesSize && cached.content == content {
		cached.rev = m.dataRev
		m.inspectDocs[cacheKey] = cached
		return cached.lines
	}
	var lines []string
	if mode == paneLogs {
		lines = m.podLogLines(innerW)
	} else {
		lines = renderMarkdown(content.text, innerW)
	}
	if m.inspectDocs != nil {
		if len(m.inspectDocs) > 64 {
			clear(m.inspectDocs)
		}
		m.inspectDocs[cacheKey] = inspectCache{
			content: content, width: innerW, theme: themeRevision, rev: m.dataRev, lines: lines,
		}
	}
	return lines
}

func (m *model) scrollInspect(delta int) {
	var total int
	if m.showingDocPane() {
		total = len(m.cachedInspectLines(max(m.layout().docW, 1)))
	}
	maxOffset := max(total-m.inspectPageSize(), 0)
	if m.effectivePane() == paneLogs {
		if m.logFollow && delta < 0 {
			m.inspectScroll = maxOffset
			m.logFollow = false
		}
		m.inspectScroll = clamp(m.inspectScroll+delta, 0, maxOffset)
		if delta > 0 && m.inspectScroll >= maxOffset {
			m.logFollow = true
		}
		return
	}
	m.inspectScroll = clamp(m.inspectScroll+delta, 0, maxOffset)
}

// docInspectView returns the visible page of the pod's Configs or Logs view.
func (m model) docInspectView(l layout) []string {
	pageH := max(l.docH, 1)
	doc := m.cachedInspectLines(max(l.docW, 1))
	total := len(doc)
	maxOffset := max(total-pageH, 0)
	offset := clamp(m.inspectScroll, 0, maxOffset)
	if m.effectivePane() == paneLogs && m.logFollow {
		offset = maxOffset
	}
	if offset >= total {
		return nil
	}
	return doc[offset:min(offset+pageH, total)]
}

func buildPodMarkdown(p fleet.Pool, b *fleet.Sandbox) string {
	switch b.PodRole() {
	case fleet.RoleWarm:
		return buildWarmMarkdown(p, b)
	case fleet.RoleEgress:
		return buildEgressMarkdown(b)
	default:
		return buildGenericPodMarkdown(b)
	}
}

func appendYAMLSection(sections *[]string, kind, name, body string) {
	if body == "" {
		return
	}
	if name != "" {
		kind += " · " + name
	}
	*sections = append(*sections, "## "+kind+"\n```yaml\n"+body+"\n```")
}

func buildWarmMarkdown(p fleet.Pool, b *fleet.Sandbox) string {
	sections := make([]string, 0, 5)

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

	appendYAMLSection(&sections, "SandboxTemplate", p.Template, p.TemplateYAML)
	appendYAMLSection(&sections, "SandboxWarmPool", p.Name, p.ManifestYAML)
	appendYAMLSection(&sections, "Sandbox", b.Name, b.ManifestYAML)
	appendYAMLSection(&sections, "Pod", b.Name, b.PodYAML)

	return strings.Join(sections, "\n\n---\n\n")
}

func buildEgressMarkdown(b *fleet.Sandbox) string {
	sections := make([]string, 0, 3)
	var intro strings.Builder
	fmt.Fprintf(&intro, "# %s · egress proxy\n", b.Name)
	fmt.Fprintf(&intro, "- **namespace** `%s`\n", b.EffectiveNamespace())
	if b.PodIP != "" {
		fmt.Fprintf(&intro, "- **pod IP** `%s`\n", b.PodIP)
	}
	if len(b.Ports) > 0 {
		fmt.Fprintf(&intro, "- **ports** `%s`\n", strings.Join(b.Ports, ", "))
	}
	if b.Image != "" {
		fmt.Fprintf(&intro, "- **image** `%s`\n", b.Image)
	}
	intro.WriteString("\nHolds real API/GitHub credentials outside sandboxes and enforces default-deny egress rules. Switch to `Logs` (`L`) to inspect live ALLOW/DENY audit events.")
	sections = append(sections, intro.String())

	if b.ExtraYAML != "" {
		sections = append(sections, b.ExtraYAML)
	}
	appendYAMLSection(&sections, "Pod", b.Name, b.PodYAML)
	return strings.Join(sections, "\n\n---\n\n")
}

func buildGenericPodMarkdown(b *fleet.Sandbox) string {
	sections := make([]string, 0, 3)
	kind, _ := kindName(b)
	var intro strings.Builder
	fmt.Fprintf(&intro, "# %s · %s\n", b.Name, strings.ToLower(kind))
	fmt.Fprintf(&intro, "- **namespace** `%s`\n", b.EffectiveNamespace())
	if b.Owner != "" {
		owner := b.Owner
		if b.OwnerKind != "" {
			owner = b.OwnerKind + "/" + owner
		}
		fmt.Fprintf(&intro, "- **owner** `%s`\n", owner)
	}
	if b.PodIP != "" {
		fmt.Fprintf(&intro, "- **pod IP** `%s`\n", b.PodIP)
	}
	if b.NodeName != "" {
		fmt.Fprintf(&intro, "- **node** `%s`\n", b.NodeName)
	}
	if b.Image != "" {
		fmt.Fprintf(&intro, "- **image** `%s`\n", b.Image)
	}
	if len(b.Ports) > 0 {
		fmt.Fprintf(&intro, "- **ports** `%s`\n", strings.Join(b.Ports, ", "))
	}
	sections = append(sections, intro.String())

	appendYAMLSection(&sections, "Sandbox", b.Name, b.ManifestYAML)
	appendYAMLSection(&sections, "Pod", b.Name, b.PodYAML)
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
