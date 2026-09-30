package tui

import (
	"net/http"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
)

// previewTickMsg fires once the selection has rested on id for previewDelay.
type previewTickMsg struct {
	seq int
	id  int64
}

// previewMsg is the answer to a preview GET.
type previewMsg struct {
	id          int64
	thought     api.Thought
	notModified bool
	err         error
}

// loadPreview schedules a fetch of the selected thought's preview. A cached
// preview shows at once and is revalidated.
func (m *model) loadPreview() tea.Cmd {
	id, ok := m.selectedID()
	if !ok {
		return nil
	}
	m.previewSeq++
	return after(m.previewDelay, previewTickMsg{seq: m.previewSeq, id: id})
}

// reloadIfShown revalidates id's preview when it is the selected thought.
func (m *model) reloadIfShown(id int64) tea.Cmd {
	if m.isShown(id) {
		return m.loadPreview()
	}
	return nil
}

func (m *model) fetchPreview(msg previewTickMsg) tea.Cmd {
	if msg.seq != m.previewSeq {
		return nil
	}
	ctx, c, id := m.ctx, m.c, msg.id
	if cached, ok := m.cache[id]; ok {
		version := cached.Version
		return func() tea.Msg {
			i, nm, err := c.Revalidate(ctx, id, version)
			return previewMsg{id: id, thought: i, notModified: nm, err: err}
		}
	}
	return func() tea.Msg {
		i, err := c.Get(ctx, id)
		return previewMsg{id: id, thought: i, err: err}
	}
}

func (m *model) previewed(msg previewMsg) {
	switch {
	case msg.err != nil:
		delete(m.cache, msg.id)
		m.missing[msg.id] = msg.err.Error()
		if client.Status(msg.err) == http.StatusNotFound {
			m.missing[msg.id] = "deleted"
			m.updateRow(msg.id, func(r *row) { r.deleted = true })
		}
	case msg.notModified:
	default:
		m.showThought(msg.thought)
	}
}

// showThought caches a fetched thought and brings its row up to date, in place.
func (m *model) showThought(i api.Thought) {
	m.cache[i.ID] = i
	delete(m.missing, i.ID)
	m.updateRow(i.ID, func(r *row) {
		snippet := r.Snippet
		r.Meta = i.Meta
		r.Snippet = snippet
	})
}

// renderKey is what the rendered body in the viewport depends on.
type renderKey struct {
	id      int64
	version int64
	body    string
	width   int
	raw     bool
	dark    bool
}

// glamourRenderer renders markdown at a width, keeping one renderer per
// width and style.
type glamourRenderer struct {
	style string
	dark  bool
	width int
	r     *glamour.TermRenderer
}

func newGlamourRenderer() glamourRenderer {
	g := glamourRenderer{dark: true}
	g.setDark(true)
	return g
}

func (g *glamourRenderer) setDark(dark bool) {
	style := styles.LightStyle
	switch {
	case os.Getenv("NO_COLOR") != "":
		style = styles.NoTTYStyle
	case dark:
		style = styles.DarkStyle
	}
	if style != g.style {
		g.style, g.dark, g.r = style, dark, nil
	}
}

func (g *glamourRenderer) render(body string, width int) string {
	if g.r == nil || g.width != width {
		r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(g.style), glamour.WithWordWrap(width))
		if err != nil {
			return body
		}
		g.r, g.width = r, width
	}
	out, err := g.r.Render(body)
	if err != nil {
		return body
	}
	return strings.Trim(out, "\n")
}
