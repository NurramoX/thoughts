package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/NurramoX/thoughts/internal/api"
)

// statusKeys are the keys that set a Status.
var statusKeys = map[string]string{
	"a": api.StatusActive,
	"d": api.StatusDone,
	"x": api.StatusDropped,
	"r": api.StatusRaw,
}

func (m *model) key(k tea.KeyPressMsg) tea.Cmd {
	if k.String() == "ctrl+c" {
		m.abortEdit()
		return tea.Quit
	}
	switch m.mode {
	case modeFilter:
		return m.filterKey(k)
	case modeTags:
		return m.tagsKey(k)
	case modeDelete:
		return m.deleteKey(k)
	case modeConflict:
		return m.conflictKey(k)
	case modeTheirs:
		return m.theirsKey(k)
	case modeHelp:
		m.mode = modeList
		return nil
	}
	return m.listKey(k)
}

// listKey handles a key with the focus on the list.
func (m *model) listKey(k tea.KeyPressMsg) tea.Cmd {
	m.setStatus("") // a message lasts until the next key
	s := k.String()
	if status, ok := statusKeys[s]; ok {
		return m.setThoughtStatus(status)
	}
	if m.scrollPreview(s) {
		return nil
	}
	switch s {
	case "j", "down":
		return m.selectRow(m.sel + 1)
	case "k", "up":
		return m.selectRow(m.sel - 1)
	case "g", "home":
		return m.selectRow(0)
	case "G", "end":
		return m.selectRow(len(m.rows) - 1)
	case "space", "n":
		return m.advance()
	case "t":
		return m.startRetag()
	case "enter", "e":
		return m.startEdit()
	case "D":
		return m.startDelete()
	case "/", "f":
		m.mode = modeFilter
		m.bar.CursorEnd()
		return m.bar.Focus()
	case "o":
		m.order = (m.order + 1) % len(orders)
		return m.requery()
	case "ctrl+r":
		return m.requery()
	case "m":
		m.raw = !m.raw
	case "tab":
		m.showPreview = !m.showPreview
	case "?":
		m.mode = modeHelp
	case "q":
		return tea.Quit
	}
	return nil
}

// scrollPreview handles the preview's scroll keys, reporting whether s was
// one.
func (m *model) scrollPreview(s string) bool {
	switch s {
	case "J":
		m.vp.ScrollDown(1)
	case "K":
		m.vp.ScrollUp(1)
	case "ctrl+d":
		m.vp.HalfPageDown()
	case "ctrl+u":
		m.vp.HalfPageUp()
	default:
		return false
	}
	return true
}

// filterKey handles a key while typing in the filter bar.
func (m *model) filterKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "ctrl+n", "down":
		return m.selectRow(m.sel + 1)
	case "ctrl+p", "up":
		return m.selectRow(m.sel - 1)
	case "enter", "esc":
		m.mode = modeList
		m.bar.Blur()
		m.filterSeq++ // cancel a pending debounce
		if m.bar.Value() != m.lastQuery {
			return m.requery()
		}
		return nil
	}
	before := m.bar.Value()
	var cmd tea.Cmd
	m.bar, cmd = m.bar.Update(k)
	if m.bar.Value() == before {
		return cmd
	}
	m.filterSeq++
	return tea.Batch(cmd, after(m.filterDelay, filterTickMsg{seq: m.filterSeq}))
}

// filterTickMsg fires once the filter bar has been still for filterDelay.
type filterTickMsg struct{ seq int }

// tagsKey handles a key while retagging.
func (m *model) tagsKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.mode = modeList
		m.tags.Blur()
		return nil
	case "enter":
		m.mode = modeList
		m.tags.Blur()
		return m.applyTags(strings.Fields(m.tags.Value()))
	}
	var cmd tea.Cmd
	m.tags, cmd = m.tags.Update(k)
	return cmd
}
