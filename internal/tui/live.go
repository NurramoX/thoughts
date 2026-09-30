package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
)

// changedMsg is a Change made anywhere, this TUI's own writes included.
type changedMsg struct{ change api.Change }

// resyncMsg: the change stream (re)connected, and whatever changed while it
// was down was missed.
type resyncMsg struct{}

// syncTickMsg fires once no change has arrived for syncDelay.
type syncTickMsg struct{ seq int }

// watch follows the daemon's changes until ctx is done, sending them to the
// program. Each connection starts with a resyncMsg; a lost stream is retried
// every retry.
func watch(ctx context.Context, c client.Client, send func(tea.Msg), retry time.Duration) {
	for {
		if s, err := c.Changes(ctx); err == nil {
			send(resyncMsg{})
			for {
				ch, err := s.Next()
				if err != nil {
					break
				}
				send(changedMsg{change: ch})
			}
			s.Close()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(retry):
		}
	}
}

// changed collects a Change, to be caught up with once changes stop
// arriving for syncDelay, so that a burst costs one refresh.
func (m *model) changed(msg changedMsg) tea.Cmd {
	c := msg.change
	if prev, ok := m.changes[c.ID]; !ok || c.Deleted || (!prev.Deleted && c.Version > prev.Version) {
		m.changes[c.ID] = c
	}
	m.syncSeq++
	return after(m.syncDelay, syncTickMsg{seq: m.syncSeq})
}

// catchUp brings the snapshot up to date with the collected changes: a
// deleted row is marked, a row older than its change is fetched again, and
// a thought the snapshot lacks, which may now match the filter, refreshes it.
// This TUI's own writes arrive too; their rows are already up to date.
func (m *model) catchUp() tea.Cmd {
	var cmds []tea.Cmd
	refresh := false
	for id, c := range m.changes {
		i := m.indexOf(id)
		switch {
		case i < 0:
			refresh = refresh || !c.Deleted
		case c.Deleted:
			m.rows[i].deleted = true
			delete(m.cache, id)
			m.missing[id] = "deleted"
		case c.Version > m.rows[i].Version:
			cmds = append(cmds, m.fetch(id))
		}
	}
	clear(m.changes)
	if refresh {
		cmds = append(cmds, m.refresh())
	}
	return tea.Batch(cmds...)
}

// fetch gets id afresh, updating its row and its preview.
func (m *model) fetch(id int64) tea.Cmd {
	ctx, c := m.ctx, m.c
	return func() tea.Msg {
		i, err := c.Get(ctx, id)
		return previewMsg{id: id, thought: i, err: err}
	}
}
