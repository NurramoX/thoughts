package tui

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
)

// statusMsg is the answer to a Status change.
type statusMsg struct {
	id      int64
	status  string
	version int64
	err     error
}

// setThoughtStatus sets the selected thought's Status and advances.
func (m *model) setThoughtStatus(status string) tea.Cmd {
	r := m.selectedRow()
	if r == nil || r.deleted {
		return nil
	}
	ctx, c, id := m.ctx, m.c, r.ID
	set := func() tea.Msg {
		v, err := c.PutAttribute(ctx, id, api.Precondition{}, api.StatusKey, status)
		if err != nil {
			return statusMsg{id: id, err: err}
		}
		return statusMsg{id: id, status: status, version: v}
	}
	return tea.Batch(set, m.advance())
}

// statusSet updates the row and, when the thought is still selected (the last
// row does not advance), its preview.
func (m *model) statusSet(msg statusMsg) tea.Cmd {
	if msg.err != nil {
		m.setError(msg.err)
		return nil
	}
	m.updateRow(msg.id, func(r *row) {
		r.Attributes = withAttr(r.Attributes, api.StatusKey, msg.status)
		r.Version = msg.version
	})
	return m.reloadIfShown(msg.id)
}

// withAttr returns a copy of attrs with key set to value.
func withAttr(attrs map[string]string, key, value string) map[string]string {
	out := make(map[string]string, len(attrs)+1)
	for k, v := range attrs {
		out[k] = v
	}
	out[key] = value
	return out
}

// startRetag opens the inline tag editor on the selected thought's tags.
func (m *model) startRetag() tea.Cmd {
	r := m.selectedRow()
	if r == nil || r.deleted {
		return nil
	}
	m.mode = modeTags
	m.tags.SetValue(strings.Join(r.Tags, " "))
	m.tags.CursorEnd()
	return m.tags.Focus()
}

// tagsMsg is the answer to a retag: the tags as far as the calls got.
type tagsMsg struct {
	id      int64
	tags    []string
	version int64 // 0 when nothing was written
	err     error
}

// applyTags makes the selected thought's tags the given list, with one PUT per
// added tag and one DELETE per removed tag, in order, stopping at the first
// failure.
func (m *model) applyTags(want []string) tea.Cmd {
	r := m.selectedRow()
	if r == nil {
		return nil
	}
	for i, t := range want {
		want[i] = api.LowerLabel(t) // the server lowercases; compare as it will
	}
	have := r.Tags
	var add, del []string
	for _, t := range want {
		if !slices.Contains(have, t) && !slices.Contains(add, t) {
			add = append(add, t)
		}
	}
	for _, t := range have {
		if !slices.Contains(want, t) {
			del = append(del, t)
		}
	}
	if len(add) == 0 && len(del) == 0 {
		return nil
	}
	ctx, c, id, tags := m.ctx, m.c, r.ID, slices.Clone(have)
	return func() tea.Msg {
		msg := tagsMsg{id: id}
		for _, t := range add {
			v, err := c.PutTag(ctx, id, api.Precondition{}, t)
			if err != nil {
				msg.err = err
				break
			}
			tags, msg.version = append(tags, t), v
		}
		for _, t := range del {
			if msg.err != nil {
				break
			}
			v, err := c.DeleteTag(ctx, id, api.Precondition{}, t)
			if err != nil {
				msg.err = err
				break
			}
			tags, msg.version = slices.DeleteFunc(tags, func(x string) bool { return x == t }), v
		}
		slices.Sort(tags)
		msg.tags = tags
		return msg
	}
}

func (m *model) retagged(msg tagsMsg) tea.Cmd {
	if msg.err != nil {
		m.setError(msg.err)
	}
	if msg.version == 0 {
		return nil
	}
	m.updateRow(msg.id, func(r *row) {
		r.Tags = msg.tags
		r.Version = msg.version
	})
	return m.reloadIfShown(msg.id)
}

// startDelete asks y/N before deleting the selected thought. It needs the
// preview: the Version it shows guards the delete.
func (m *model) startDelete() tea.Cmd {
	r := m.selectedRow()
	if r == nil || r.deleted {
		return nil
	}
	if _, ok := m.guardingPreview(r); !ok {
		return nil
	}
	m.mode = modeDelete
	return nil
}

// guardingPreview is the preview whose Version guards `e` and `D`. It is not
// ready while loading, or while older than a write this session made.
func (m *model) guardingPreview(r *row) (api.Thought, bool) {
	i, ok := m.cache[r.ID]
	if !ok || i.Version < r.Version {
		m.setStatus("the preview is still loading")
		return api.Thought{}, false
	}
	return i, true
}

// deletedMsg is the answer to a delete.
type deletedMsg struct {
	id  int64
	err error
}

func (m *model) deleteKey(k tea.KeyPressMsg) tea.Cmd {
	m.mode = modeList
	if k.String() != "y" {
		m.setStatus("not deleted")
		return nil
	}
	r := m.selectedRow()
	if r == nil {
		return nil
	}
	ctx, c, id, version := m.ctx, m.c, r.ID, m.cache[r.ID].Version
	return func() tea.Msg {
		return deletedMsg{id: id, err: c.Delete(ctx, id, api.Precondition{Version: version})}
	}
}

func (m *model) deleted(msg deletedMsg) tea.Cmd {
	if msg.err != nil {
		if client.Status(msg.err) == http.StatusPreconditionFailed {
			m.setError(fmt.Errorf("thought %d changed, not deleted", msg.id))
			return m.reloadIfShown(msg.id)
		}
		m.setError(msg.err)
		return nil
	}
	m.updateRow(msg.id, func(r *row) { r.deleted = true })
	delete(m.cache, msg.id)
	m.missing[msg.id] = "deleted"
	m.setStatus(fmt.Sprintf("deleted thought %d", msg.id))
	if id, ok := m.selectedID(); ok && id == msg.id {
		return m.advance()
	}
	return nil
}
