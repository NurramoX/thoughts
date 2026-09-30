package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
	"github.com/NurramoX/thoughts/internal/editor"
)

func runEdit(a *app, in *input) int {
	v := in.verb
	if len(in.args) != 1 {
		return a.usagef(v, "one id is required")
	}
	id, err := parseID(in.args[0])
	if err != nil {
		return a.usagef(v, "%v", err)
	}
	pre, err := in.precondition()
	if err != nil {
		return a.usagef(v, "%v", err)
	}
	if code := a.connect(); code != exitOK {
		return code
	}
	// Even with --force the body must be read: it is what gets edited.
	body, version, err := a.c.Body(a.ctx, id)
	if err != nil {
		return a.fail(err)
	}
	if pre.IsZero() {
		pre.Version = version
	}
	s, err := editor.Open(id, body)
	if err != nil {
		return a.fail(err)
	}
	defer s.Close()
	for {
		if err := a.runEditor(s); err != nil {
			a.errorf("%v; nothing written", err)
			return exitInternal
		}
		text, changed, err := s.Result()
		if err != nil {
			return a.fail(err)
		}
		if !changed {
			if a.json {
				a.written(id, version)
			}
			return exitOK
		}
		version, err = a.c.PutBody(a.ctx, id, pre, text)
		if err == nil {
			if a.json {
				a.written(id, version)
			}
			return exitOK
		}
		current, ok := client.Stale(err)
		if !ok {
			a.stdout.Write(text) // the user's text never gets lost
			return a.fail(err)
		}
		if a.json {
			a.fail(err)
		} else {
			a.changed(id, pre, current)
		}
		switch a.resolve() {
		case editor.Overwrite:
			pre = api.Precondition{Force: true}
			version, err = a.c.PutBody(a.ctx, id, pre, text)
			if err != nil {
				a.stdout.Write(text)
				return a.fail(err)
			}
			if a.json {
				a.written(id, version)
			}
			return exitOK
		case editor.ReEdit:
			theirs, v, err := a.c.Body(a.ctx, id)
			if err != nil {
				a.stdout.Write(text)
				return a.fail(err)
			}
			fmt.Fprintf(a.stderr, "--- thought %d, version %d, as it is now:\n", id, v)
			a.stderr.Write(theirs)
			if len(theirs) > 0 && theirs[len(theirs)-1] != '\n' {
				fmt.Fprintln(a.stderr)
			}
			s.Rebase(theirs)
			pre = api.Precondition{Version: v}
			version = v
		default:
			a.stdout.Write(text)
			return exitStale
		}
	}
}

// resolve asks what to do after a 412. Without a terminal the answer is
// abort.
func (a *app) resolve() editor.Resolution {
	if !isTerminal(a.stdin) {
		return editor.Abort
	}
	for {
		answer, ok := a.prompt("[o]verwrite, [r]e-edit, [a]bort? ")
		if !ok {
			return editor.Abort
		}
		switch strings.ToLower(answer) {
		case "o", "overwrite":
			return editor.Overwrite
		case "r", "re-edit", "reedit":
			return editor.ReEdit
		case "a", "abort":
			return editor.Abort
		}
	}
}

// compose writes a new thought's body in the editor, for `add --edit`. The
// code is non-zero when nothing should be created.
func (a *app) compose(seed []byte) ([]byte, int) {
	s, err := editor.Open(0, seed)
	if err != nil {
		return nil, a.fail(err)
	}
	defer s.Close()
	if err := a.runEditor(s); err != nil {
		a.errorf("%v; nothing created", err)
		return nil, exitInternal
	}
	text, changed, err := s.Result()
	if err != nil {
		return nil, a.fail(err)
	}
	if !changed {
		a.errorf("the body was not changed; nothing created")
		return nil, exitInternal
	}
	return text, exitOK
}

// runEditor runs the editor on the session's file, on the terminal when the
// CLI has one.
func (a *app) runEditor(s *editor.Session) error {
	cmd := s.Command()
	if f, ok := a.stdin.(*os.File); ok {
		cmd.Stdin = f
	}
	cmd.Stdout = fileOrNil(a.stdout)
	cmd.Stderr = fileOrNil(a.stderr)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor: %v", err)
	}
	return nil
}

// fileOrNil passes w to a child only when it is a file, so the editor never
// writes into captured output.
func fileOrNil(w io.Writer) io.Writer {
	if f, ok := w.(*os.File); ok {
		return f
	}
	return nil
}
