package tui

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var testNow = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

// harness drives a model the way the tea runtime would, but synchronously:
// every command runs to completion and its messages are fed back in.
type harness struct {
	t    *testing.T
	m    *model
	fc   *fakeClient
	quit bool
}

func newHarness(t *testing.T, fc *fakeClient, filter string) *harness {
	t.Helper()
	m := newModel(context.Background(), fc, filter)
	m.now = func() time.Time { return testNow }
	m.filterDelay = 0
	m.previewDelay = 0
	m.syncDelay = 0
	m.exec = func(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
		return func() tea.Msg { return fn(c.Run()) }
	}
	h := &harness{t: t, m: m, fc: fc}
	// Run closes a round-trip left open when the TUI exits; so does the test.
	t.Cleanup(m.abortEdit)
	h.run(m.Init())
	h.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	return h
}

// send delivers msg and runs whatever it causes.
func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	_, cmd := h.m.Update(msg)
	h.run(cmd)
}

// step delivers msg without running its command, returning the messages the
// command would produce.
func (h *harness) step(msg tea.Msg) []tea.Msg {
	h.t.Helper()
	_, cmd := h.m.Update(msg)
	return collect(cmd)
}

func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collect(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

func (h *harness) run(cmd tea.Cmd) {
	h.t.Helper()
	for _, msg := range collect(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			h.quit = true
			continue
		}
		h.send(msg)
	}
}

// press sends each key in turn, named as tea's Key.String spells them:
// "j", "J", "enter", "esc", "space", "ctrl+n", "up".
func (h *harness) press(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		h.send(keyMsg(k))
	}
}

// typeText sends each rune of s as a key press.
func (h *harness) typeText(s string) {
	h.t.Helper()
	for _, r := range s {
		h.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

var namedKeys = map[string]rune{
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab,
	"up": tea.KeyUp, "down": tea.KeyDown, "backspace": tea.KeyBackspace,
	"space": tea.KeySpace,
}

func keyMsg(k string) tea.KeyPressMsg {
	if c, ok := namedKeys[k]; ok {
		if k == "space" {
			return tea.KeyPressMsg{Code: c, Text: " "}
		}
		return tea.KeyPressMsg{Code: c}
	}
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: []rune(rest)[0], Mod: tea.ModCtrl}
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

// view is the rendered screen without ANSI escapes.
func (h *harness) view() string {
	return ansi.Strip(h.m.View().Content)
}

// selected is the id of the selected row.
func (h *harness) selected() int64 {
	h.t.Helper()
	id, ok := h.m.selectedID()
	if !ok {
		h.t.Fatal("no row selected")
	}
	return id
}

// rowIDs lists the ids of the snapshot's rows in order.
func (h *harness) rowIDs() []int64 {
	var ids []int64
	for _, r := range h.m.rows {
		ids = append(ids, r.ID)
	}
	return ids
}
