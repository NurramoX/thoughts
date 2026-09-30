package cli

import (
	"strings"

	"github.com/NurramoX/thoughts/internal/tui"
)

// runTUI is the Review TUI, replaced in tests.
var runTUI = tui.Run

func runReview(a *app, in *input) int {
	if code := a.connect(); code != exitOK {
		return code
	}
	aborted, err := runTUI(a.ctx, a.c, strings.Join(in.args, " "))
	for _, text := range aborted {
		a.stdout.Write(text)
	}
	if err != nil {
		return a.fail(err)
	}
	return exitOK
}
