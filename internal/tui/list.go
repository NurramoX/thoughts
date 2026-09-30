package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
	"github.com/NurramoX/thoughts/internal/filter"
	"github.com/NurramoX/thoughts/internal/hint"
)

// ordering is one step of the `o` cycle.
type ordering struct {
	name  string
	sort  string // "" leaves the order to the server, then shuffles
	order string
}

var orders = []ordering{
	{name: "updated", sort: "updated", order: "desc"},
	{name: "created", sort: "created", order: "desc"},
	{name: "random"},
}

// listMsg is the answer to a list request: a requery, which replaces the
// snapshot, or a refresh, which merges into it.
type listMsg struct {
	seq    int
	params client.ListParams
	merge  bool
	list   api.List
	err    error
}

// hintMsg is the vocabulary hint for an empty result.
type hintMsg struct {
	seq   int
	lines []string
}

// requery lists the filter in the bar, in the current order.
func (m *model) requery() tea.Cmd {
	o := orders[m.order]
	p := client.ListParams{Filter: m.bar.Value(), Sort: o.sort, Order: o.order}
	m.lastQuery = p.Filter
	return m.list(p, false)
}

// refresh lists the snapshot's query again and merges the answer in. While
// another list request is out it waits for that answer, which may predate
// the changes being caught up with.
func (m *model) refresh() tea.Cmd {
	if m.listSeq != m.answered {
		m.resync = true
		return nil
	}
	if m.snap == nil {
		return nil
	}
	return m.list(*m.snap, true)
}

// list sends a list request. Only the answer to the latest one is applied.
func (m *model) list(p client.ListParams, merge bool) tea.Cmd {
	m.listSeq++
	seq := m.listSeq
	ctx, c := m.ctx, m.c
	return func() tea.Msg {
		l, err := c.List(ctx, p)
		return listMsg{seq: seq, params: p, merge: merge, list: l, err: err}
	}
}

// listed applies a list request's answer, then catches up with changes that
// arrived while it was out.
func (m *model) listed(msg listMsg) tea.Cmd {
	if msg.seq != m.listSeq {
		return nil
	}
	m.answered = msg.seq
	var cmd tea.Cmd
	switch {
	case msg.err != nil:
		m.listFailed(msg)
	case msg.merge:
		cmd = m.merge(msg)
	default:
		cmd = m.replace(msg)
	}
	if m.resync {
		m.resync = false
		cmd = tea.Batch(cmd, m.refresh())
	}
	return cmd
}

func (m *model) listFailed(msg listMsg) {
	if pos, detail, ok := client.FilterError(msg.err); ok && !msg.merge {
		m.filterError = filter.Caret(msg.params.Filter, pos) + "\n" + detail
		return
	}
	m.setError(msg.err)
}

// replace makes a requery's answer the snapshot, keeping the selected thought
// when it is still present.
func (m *model) replace(msg listMsg) tea.Cmd {
	m.snap = &msg.params
	m.filterError = ""
	m.hint = nil
	keep, hadSel := m.selectedID()
	m.rows = make([]row, len(msg.list.Thoughts))
	for i, meta := range msg.list.Thoughts {
		m.rows[i] = row{Meta: meta}
	}
	if msg.params.Sort == "" {
		m.shuffle(m.rows)
	}
	m.total = msg.list.Total
	m.sel, m.offset = 0, 0
	if hadSel {
		if i := m.indexOf(keep); i >= 0 {
			m.sel = i
		}
	}
	if len(m.rows) == 0 {
		return m.vocabularyHint(msg.seq, msg.params.Filter)
	}
	return m.loadPreview()
}

// merge folds a refresh into the snapshot without reordering it or removing
// anything: listed thoughts take their new metadata in place, and a thought new
// to the snapshot goes in after the one the server lists before it, or at
// the top. The selected thought stays selected, and the rows in view stay put
// unless the new ones land among them.
func (m *model) merge(msg listMsg) tea.Cmd {
	m.total = msg.list.Total
	hadRows := len(m.rows) > 0
	var cmd tea.Cmd
	at := 0 // where the next new thought goes
	for _, meta := range msg.list.Thoughts {
		if i := m.indexOf(meta.ID); i >= 0 {
			if meta.Version > m.rows[i].Version && m.isShown(meta.ID) {
				cmd = m.loadPreview()
			}
			if meta.Version >= m.rows[i].Version {
				m.rows[i].Meta = meta
			}
			at = i + 1
			continue
		}
		m.rows = slices.Insert(m.rows, at, row{Meta: meta})
		if hadRows && at <= m.sel {
			m.sel++
		}
		if at < m.offset {
			m.offset++
		}
		at++
	}
	if hadRows || len(m.rows) == 0 {
		return cmd
	}
	m.hint = nil
	return m.loadPreview()
}

func (m *model) vocabularyHint(seq int, f string) tea.Cmd {
	ctx, c := m.ctx, m.c
	return func() tea.Msg {
		lines, _ := hint.Vocabulary(ctx, c, f)
		return hintMsg{seq: seq, lines: lines}
	}
}

// selectedID is the id of the selected row.
func (m *model) selectedID() (int64, bool) {
	if m.sel < 0 || m.sel >= len(m.rows) {
		return 0, false
	}
	return m.rows[m.sel].ID, true
}

// isShown reports whether id is the selected thought.
func (m *model) isShown(id int64) bool {
	sel, ok := m.selectedID()
	return ok && sel == id
}

func (m *model) selectedRow() *row {
	if m.sel < 0 || m.sel >= len(m.rows) {
		return nil
	}
	return &m.rows[m.sel]
}

func (m *model) indexOf(id int64) int {
	for i, r := range m.rows {
		if r.ID == id {
			return i
		}
	}
	return -1
}

// updateRow changes the row of id in place, if the snapshot has it.
func (m *model) updateRow(id int64, f func(*row)) {
	if i := m.indexOf(id); i >= 0 {
		f(&m.rows[i])
	}
}

// selectRow moves the selection to i, clamped, and loads its preview.
func (m *model) selectRow(i int) tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	i = max(0, min(i, len(m.rows)-1))
	if i == m.sel {
		return nil
	}
	m.sel = i
	return m.loadPreview()
}

// advance moves the selection to the next row.
func (m *model) advance() tea.Cmd { return m.selectRow(m.sel + 1) }
