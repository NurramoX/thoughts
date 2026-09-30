package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
	"github.com/NurramoX/thoughts/internal/editor"
)

// editState is one editor round-trip on a thought's body (`e`).
type editState struct {
	id      int64
	version int64 // guards the write
	sess    *editor.Session
	text    []byte // the user's text, once the editor has closed
	current int64  // the Version the server reported on a 412
}

// editorDoneMsg: the editor exited.
type editorDoneMsg struct{ err error }

// bodyWrittenMsg is the answer to the body write.
type bodyWrittenMsg struct {
	version int64
	err     error
}

// theirsMsg is the other party's thought, fetched for a re-edit.
type theirsMsg struct {
	thought api.Thought
	err     error
}

// startEdit opens the selected thought's body in the editor. The preview's
// Version guards the write.
func (m *model) startEdit() tea.Cmd {
	r := m.selectedRow()
	if r == nil || r.deleted {
		return nil
	}
	thought, ok := m.guardingPreview(r)
	if !ok {
		return nil
	}
	sess, err := editor.Open(thought.ID, []byte(thought.Body))
	if err != nil {
		m.setError(err)
		return nil
	}
	m.edit = &editState{id: thought.ID, version: thought.Version, sess: sess}
	return m.runEditor()
}

func (m *model) runEditor() tea.Cmd {
	return m.exec(m.edit.sess.Command(), func(err error) tea.Msg { return editorDoneMsg{err: err} })
}

func (m *model) editorDone(msg editorDoneMsg) tea.Cmd {
	e := m.edit
	if e == nil {
		return nil
	}
	m.mode = modeList
	// On a failure, a re-edit still holds the text the user saved before:
	// abortEdit sends it to stdout.
	if msg.err != nil {
		m.abortEdit()
		m.setError(fmt.Errorf("editor: %w; nothing written", msg.err))
		return nil
	}
	text, changed, err := e.sess.Result()
	if err != nil {
		m.abortEdit()
		m.setError(err)
		return nil
	}
	if !changed {
		m.closeEdit()
		m.setStatus("unchanged, nothing written")
		return nil
	}
	e.text = text
	return m.writeBody(api.Precondition{Version: e.version})
}

func (m *model) writeBody(pre api.Precondition) tea.Cmd {
	ctx, c, id, text := m.ctx, m.c, m.edit.id, m.edit.text
	return func() tea.Msg {
		v, err := c.PutBody(ctx, id, pre, text)
		return bodyWrittenMsg{version: v, err: err}
	}
}

func (m *model) bodyWritten(msg bodyWrittenMsg) tea.Cmd {
	e := m.edit
	if e == nil {
		return nil
	}
	if msg.err != nil {
		if current, ok := client.Stale(msg.err); ok {
			e.current = current
			m.mode = modeConflict
			return nil
		}
		// The text must not be lost: it leaves on stdout after the TUI.
		m.abortEdit()
		m.setError(fmt.Errorf("%w; your text will be printed on exit", msg.err))
		return nil
	}
	id := e.id
	m.closeEdit()
	m.updateRow(id, func(r *row) { r.Version = msg.version })
	m.setStatus(fmt.Sprintf("saved thought %d", id))
	return m.reloadIfShown(id)
}

// conflictKey answers the 412 prompt.
func (m *model) conflictKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "o":
		m.mode = modeList
		return m.writeBody(api.Precondition{Force: true})
	case "r":
		m.mode = modeList
		ctx, c, id := m.ctx, m.c, m.edit.id
		return func() tea.Msg {
			i, err := c.Get(ctx, id)
			return theirsMsg{thought: i, err: err}
		}
	case "a", "esc":
		m.mode = modeList
		m.abortEdit()
		m.setStatus("aborted; your text will be printed on exit")
	}
	return nil
}

// theirs shows the other party's thought in the preview and waits for the user
// to reopen their text, guarded by the new Version.
func (m *model) theirs(msg theirsMsg) {
	if m.edit == nil {
		return
	}
	if msg.err != nil {
		m.abortEdit()
		m.setError(fmt.Errorf("%w; your text will be printed on exit", msg.err))
		return
	}
	m.showThought(msg.thought)
	m.edit.version = msg.thought.Version
	m.edit.sess.Rebase([]byte(msg.thought.Body))
	m.mode = modeTheirs
}

// theirsKey reads the other party's body in the preview until the user
// reopens their own text or aborts.
func (m *model) theirsKey(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if m.scrollPreview(s) {
		return nil
	}
	switch s {
	case "enter", "e":
		m.mode = modeList
		return m.runEditor()
	case "a", "esc":
		m.mode = modeList
		m.abortEdit()
		m.setStatus("aborted; your text will be printed on exit")
	}
	return nil
}

// abortEdit ends a round-trip whose text never reached the server: the text,
// if the editor gave one, goes to Run's aborted result.
func (m *model) abortEdit() {
	if m.edit == nil {
		return
	}
	if m.edit.text != nil {
		m.aborted = append(m.aborted, m.edit.text)
	}
	m.closeEdit()
}

func (m *model) closeEdit() {
	if m.edit == nil {
		return
	}
	_ = m.edit.sess.Close()
	m.edit = nil
	if m.mode == modeConflict || m.mode == modeTheirs {
		m.mode = modeList
	}
}
