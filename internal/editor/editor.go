// Package editor is the $EDITOR round-trip over a temp file (spec §7, "Edit
// round-trip"), shared by `thought edit`, `thought add --edit` and the Review TUI's
// `e`. It owns the temp file and the editor command; the caller runs the
// command (directly in the CLI, through tea.ExecProcess in the TUI), writes
// the result to the server and handles a 412 with a Resolution.
package editor

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
)

// Session is one round-trip. The temp file holds the body only, at
// $TMPDIR/<0700 dir>/thought-<id>-*.md with mode 0600.
type Session struct {
	dir, path string
	base      []byte
}

// Open writes body to a new temp file. id is 0 for a thought not created yet
// (`add --edit`). The file is removed by Close, and by CloseAll on SIGINT or
// SIGTERM.
func Open(id int64, body []byte) (*Session, error) {
	dir, err := os.MkdirTemp("", "thought-") // 0700
	if err != nil {
		return nil, err
	}
	s := &Session{dir: dir, base: bytes.Clone(body)}
	register(s)
	f, err := os.CreateTemp(dir, fmt.Sprintf("thought-%d-*.md", id)) // 0600
	if err != nil {
		s.Close()
		return nil, err
	}
	s.path = f.Name()
	_, err = f.Write(body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// Path is the temp file.
func (s *Session) Path() string { return s.path }

// Command is the editor on the temp file: $VISUAL, then $EDITOR, then vi,
// run through `sh -c` so the variable may carry arguments. Stdin, stdout and
// stderr are left unset for the caller.
func (s *Session) Command() *exec.Cmd {
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		ed = "vi"
	}
	return exec.Command("sh", "-c", ed+` "$1"`, "sh", s.path)
}

// Result reads the temp file back. changed is false when the text equals the
// base: the body given to Open, or the last Rebase. The caller writes
// nothing when changed is false or the editor exited non-zero.
func (s *Session) Result() (text []byte, changed bool, err error) {
	text, err = os.ReadFile(s.path)
	if err != nil {
		return nil, false, err
	}
	return text, !bytes.Equal(text, s.base), nil
}

// Rebase sets the base that Result compares against, for a re-edit after a
// 412: the file keeps the user's text and the base becomes the other party's
// current body.
func (s *Session) Rebase(base []byte) { s.base = bytes.Clone(base) }

// Close removes the temp file and its directory. It is safe to call twice.
func (s *Session) Close() error {
	unregister(s)
	return os.RemoveAll(s.dir)
}

// Resolution is the user's answer to a 412 after the editor.
type Resolution int

const (
	Overwrite Resolution = iota // [o]verwrite: write with If-Match: *
	ReEdit                      // [r]e-edit: show theirs on stderr, reopen mine, guard with the new Version
	Abort                       // [a]bort: print the user's text to stdout
)
