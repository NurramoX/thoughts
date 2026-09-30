package tui

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/NurramoX/thoughts/internal/api"
)

var (
	dim        = lipgloss.NewStyle().Faint(true)
	bold       = lipgloss.NewStyle().Bold(true)
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Red)
	selStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.BrightCyan)
	goneStyle  = lipgloss.NewStyle().Faint(true).Strikethrough(true)
	statusLine = lipgloss.NewStyle().Reverse(true)

	statusStyles = map[string]lipgloss.Style{
		api.StatusRaw:     lipgloss.NewStyle().Foreground(lipgloss.Yellow),
		api.StatusActive:  lipgloss.NewStyle().Foreground(lipgloss.Green),
		api.StatusDone:    lipgloss.NewStyle().Foreground(lipgloss.Blue),
		api.StatusDropped: lipgloss.NewStyle().Faint(true),
	}
	statusLetters = map[string]string{
		api.StatusRaw: "r", api.StatusActive: "a", api.StatusDone: "d", api.StatusDropped: "x",
	}
)

// minWide is the narrowest terminal that shows both panes.
const minWide = 80

func (m *model) wide() bool { return m.width >= minWide }

// paneWidths are the list's and the preview's widths; in a narrow terminal
// each pane takes the whole width.
func (m *model) paneWidths() (list, preview int) {
	if !m.wide() {
		return m.width, m.width
	}
	list = max(30, m.width*2/5)
	return list, m.width - list - 1
}

// listHeight is the number of rows the list shows.
func (m *model) listHeight() int {
	return max(1, m.height-2-m.filterErrorLines())
}

func (m *model) filterErrorLines() int {
	if m.filterError == "" {
		return 0
	}
	return strings.Count(m.filterError, "\n") + 1
}

// layout sizes the panes and brings the list offset and the preview's
// content up to date. Update calls it after every message.
func (m *model) layout() {
	if m.width == 0 {
		return
	}
	h := m.listHeight()
	if m.sel < m.offset {
		m.offset = m.sel
	}
	if m.sel >= m.offset+h {
		m.offset = m.sel - h + 1
	}

	lw, pw := m.paneWidths()
	m.bar.SetWidth(max(1, lw-lipgloss.Width(m.bar.Prompt)-1))
	m.tags.SetWidth(max(1, m.width/2))
	m.vp.SetWidth(pw)
	m.vp.SetHeight(max(1, m.height-1-len(m.previewHeader(pw))))

	id, _ := m.selectedID()
	key := renderKey{id: id, width: pw, raw: m.raw, dark: m.glam.dark}
	var body string
	if i, ok := m.cache[id]; ok {
		key.version, body = i.Version, i.Body
	} else if msg, ok := m.missing[id]; ok {
		body = msg
	} else if len(m.rows) > 0 {
		body = "loading…"
	}
	key.body = body
	if key == m.shownKey {
		return
	}
	switch {
	case m.raw:
		m.vp.SetContent(body)
	default:
		m.vp.SetContent(m.glam.render(body, pw))
	}
	if id != m.shownID {
		m.vp.GotoTop()
	}
	m.shownKey, m.shownID = key, id
}

func (m *model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m *model) render() string {
	if m.width == 0 {
		return ""
	}
	if m.mode == modeHelp {
		return helpView()
	}
	lw, pw := m.paneWidths()
	h := m.height - 1
	var panes string
	switch {
	case m.wide():
		sep := strings.TrimSuffix(strings.Repeat("│\n", h), "\n")
		panes = lipgloss.JoinHorizontal(lipgloss.Top, m.listPane(lw, h), dim.Render(sep), m.previewPane(pw, h))
	case m.showPreview:
		panes = m.previewPane(pw, h)
	default:
		panes = m.listPane(lw, h)
	}
	return panes + "\n" + m.statusLine()
}

// fit truncates or pads s to exactly w columns.
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

// block makes lines exactly w wide and h high.
func block(lines []string, w, h int) string {
	out := make([]string, h)
	for i := range out {
		if i < len(lines) {
			out[i] = fit(lines[i], w)
		} else {
			out[i] = strings.Repeat(" ", w)
		}
	}
	return strings.Join(out, "\n")
}

func (m *model) listPane(w, h int) string {
	lines := []string{m.bar.View()}
	if m.filterError != "" {
		for _, l := range strings.Split(m.filterError, "\n") {
			lines = append(lines, errStyle.Render(l))
		}
	}
	if len(m.rows) == 0 {
		lines = append(lines, dim.Render("no thoughts"))
		for _, l := range m.hint {
			lines = append(lines, dim.Render(l))
		}
		return block(lines, w, h)
	}
	end := min(len(m.rows), m.offset+m.listHeight())
	for i := m.offset; i < end; i++ {
		lines = append(lines, m.rowLine(i, w))
	}
	return block(lines, w, h)
}

// rowLine renders a row: the selection marker, the Status letter, the title,
// the dimmed tags and, right-aligned, the age of the sort field.
func (m *model) rowLine(i, w int) string {
	r := m.rows[i]
	sel := " "
	if i == m.sel {
		sel = "▸"
	}
	status := r.Attributes[api.StatusKey]
	letter := statusStyles[status].Render(statusLetters[status])
	age := m.rowAge(r)

	title := r.Title
	switch {
	case r.deleted:
		title = goneStyle.Render(title)
	case i == m.sel:
		title = selStyle.Render(title)
	}
	text := title
	if len(r.Tags) > 0 {
		text += " " + dim.Render(strings.Join(r.Tags, " "))
	}
	prefix := sel + letter + " "
	room := max(1, w-lipgloss.Width(prefix)-lipgloss.Width(age)-1)
	return prefix + fit(text, room) + " " + dim.Render(age)
}

// rowAge is the age of the row's current sort field; random shows updated.
func (m *model) rowAge(r row) string {
	if orders[m.order].name == "created" {
		return age(m.now(), r.CreatedAt.Time)
	}
	return age(m.now(), r.UpdatedAt.Time)
}

// age is a compact "how long ago": now, 5m, 3h, 12d, 6w, 4mo, 2y.
func age(now, t time.Time) string {
	d := now.Sub(t)
	day := 24 * time.Hour
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < day:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d < 14*day:
		return fmt.Sprintf("%dd", int(d/day))
	case d < 60*day:
		return fmt.Sprintf("%dw", int(d/(7*day)))
	case d < 365*day:
		return fmt.Sprintf("%dmo", int(d/(30*day)))
	}
	return fmt.Sprintf("%dy", int(d/(365*day)))
}

// previewMeta is what the preview header shows: the fetched thought, or the
// row while it loads.
func (m *model) previewMeta() (api.Meta, bool) {
	r := m.selectedRow()
	if r == nil {
		return api.Meta{}, false
	}
	if i, ok := m.cache[r.ID]; ok {
		return i.Meta, true
	}
	return r.Meta, true
}

// previewHeader is the title, id, status, tags, attributes, ages and Version.
func (m *model) previewHeader(w int) []string {
	meta, ok := m.previewMeta()
	if !ok {
		return nil
	}
	status := meta.Attributes[api.StatusKey]
	idLine := fmt.Sprintf("#%d · %s", meta.ID, statusStyles[status].Render(status))
	if len(meta.Tags) > 0 {
		idLine += " · " + dim.Render(strings.Join(meta.Tags, " "))
	}
	lines := []string{bold.Render(meta.Title), idLine}
	var attrs []string
	for _, k := range slices.Sorted(maps.Keys(meta.Attributes)) {
		if k != api.StatusKey {
			attrs = append(attrs, k+"="+meta.Attributes[k])
		}
	}
	if len(attrs) > 0 {
		lines = append(lines, strings.Join(attrs, " · "))
	}
	lines = append(lines,
		dim.Render(fmt.Sprintf("created %s · updated %s · v%d",
			age(m.now(), meta.CreatedAt.Time), age(m.now(), meta.UpdatedAt.Time), meta.Version)),
		dim.Render(strings.Repeat("─", w)))
	return lines
}

func (m *model) previewPane(w, h int) string {
	header := m.previewHeader(w)
	if len(header) == 0 {
		return block(nil, w, h)
	}
	lines := append(header, strings.Split(m.vp.View(), "\n")...)
	return block(lines, w, h)
}

func (m *model) statusLine() string {
	n := 0
	if len(m.rows) > 0 {
		n = m.sel + 1
	}
	left := fmt.Sprintf(" %d/%d · %s", n, m.total, orders[m.order].name)
	msg := m.status
	switch m.mode {
	case modeTags:
		msg = m.tags.View()
	case modeDelete:
		if r := m.selectedRow(); r != nil {
			msg = fmt.Sprintf("delete thought %d %q? [y/N]", r.ID, r.Title)
		}
	case modeConflict:
		msg = fmt.Sprintf("thought %d changed: you had version %d, it is now %d · [o]verwrite / [r]e-edit / [a]bort",
			m.edit.id, m.edit.version, m.edit.current)
	case modeTheirs:
		msg = fmt.Sprintf("their version %d is in the preview · enter: reopen your text · a: abort", m.edit.version)
	}
	if msg != "" {
		if m.isError && m.mode == modeList {
			msg = errStyle.Render(msg)
		}
		left += " · " + msg
	}
	right := "? help "
	room := max(0, m.width-lipgloss.Width(right))
	return statusLine.Render(fit(left, room) + right)
}

var helpKeys = [][2]string{
	{"j k ↑ ↓ g G", "move the selection"},
	{"J K ctrl-d ctrl-u", "scroll the preview"},
	{"space n", "next: advance"},
	{"a d x r", "status active / done / dropped / raw, advance"},
	{"t", "retag: edit the tag list, enter applies"},
	{"enter e", "edit the body in $EDITOR"},
	{"D", "delete, after y/N"},
	{"/ f", "focus the filter bar (enter/esc back; ctrl-n ctrl-p move)"},
	{"o", "cycle order: updated, created, random"},
	{"ctrl-r", "requery"},
	{"m", "rendered ↔ raw"},
	{"tab", "switch panes (narrow terminals)"},
	{"q ctrl-c", "quit"},
}

func helpView() string {
	var b strings.Builder
	b.WriteString(bold.Render("Review keys") + "\n\n")
	for _, k := range helpKeys {
		fmt.Fprintf(&b, "  %-20s %s\n", k[0], k[1])
	}
	b.WriteString("\n" + dim.Render("any key closes this help"))
	return b.String()
}
