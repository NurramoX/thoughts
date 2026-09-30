package cli

import (
	"bytes"
	"os"

	"github.com/NurramoX/thoughts/internal/client"
)

// replaceRetries is how often replace re-reads and retries after a 412.
const replaceRetries = 3

func runReplace(a *app, in *input) int {
	v := in.verb
	oldFile, hasOld := in.value("--old-file")
	newFile, hasNew := in.value("--new-file")
	want := 3
	if hasOld {
		want--
	}
	if hasNew {
		want--
	}
	if len(in.args) != want {
		return a.usagef(v, "expected %d arguments, got %d", want, len(in.args))
	}
	id, err := parseID(in.args[0])
	if err != nil {
		return a.usagef(v, "%v", err)
	}
	rest := in.args[1:]
	text := func(has bool, path string) ([]byte, error) {
		if has {
			return os.ReadFile(path)
		}
		s := rest[0]
		rest = rest[1:]
		return []byte(s), nil
	}
	old, err := text(hasOld, oldFile)
	if err != nil {
		return a.fail(err)
	}
	repl, err := text(hasNew, newFile)
	if err != nil {
		return a.fail(err)
	}
	if len(old) == 0 {
		return a.usagef(v, "<old> must not be empty")
	}
	pre, err := in.precondition()
	if err != nil {
		return a.usagef(v, "%v", err)
	}
	all := in.bool("--all")
	if code := a.connect(); code != exitOK {
		return code
	}
	for attempt := 0; ; attempt++ {
		body, version, err := a.c.Body(a.ctx, id)
		if err != nil {
			return a.fail(err)
		}
		n := bytes.Count(body, old)
		if n == 0 || (n > 1 && !all) {
			a.errorf("the text to replace occurs %d times in thought %d; it must occur exactly once, or pass --all", n, id)
			return exitRejected
		}
		guard := pre
		if guard.IsZero() {
			guard.Version = version
		}
		version, err = a.c.PutBody(a.ctx, id, guard, bytes.ReplaceAll(body, old, repl))
		if err == nil {
			if a.json {
				a.written(id, version)
			}
			return exitOK
		}
		// Retrying only helps when the Version came from our own read.
		if _, ok := client.Stale(err); ok && pre.IsZero() && attempt < replaceRetries {
			continue
		}
		return a.failWrite(id, guard, err)
	}
}
