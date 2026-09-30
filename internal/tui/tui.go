// Package tui is the Review TUI (spec §8).
package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/NurramoX/thoughts/internal/client"
)

// DefaultFilter is the open thoughts, prefilled when no Filter is given.
const DefaultFilter = "status:raw,active"

// Run shows the Review TUI on the terminal, starting from filter ("" means
// DefaultFilter). It returns after the terminal is restored. aborted holds
// the user's text from every editor round-trip aborted after a 412, which the
// caller prints to stdout. The list follows changes made elsewhere while it
// runs.
func Run(ctx context.Context, c client.Client, filter string) (aborted [][]byte, err error) {
	m := newModel(ctx, c, filter)
	p := tea.NewProgram(m, tea.WithContext(ctx))
	wctx, stop := context.WithCancel(ctx)
	defer stop()
	go watch(wctx, c, p.Send, watchRetry)
	_, err = p.Run()
	m.abortEdit()
	return m.aborted, err
}

// watchRetry is how often a lost change stream is reopened.
const watchRetry = time.Second
