package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/NurramoX/thoughts/internal/api"
)

func runAdd(a *app, in *input) int {
	v := in.verb
	title := strings.Join(in.args, " ")
	if title == "" {
		return a.usagef(v, "a title is required")
	}
	req := api.CreateRequest{Title: title, Tags: in.values("-t")}
	for _, s := range in.values("-s") {
		k, val, err := splitKV(s)
		if err != nil {
			return a.usagef(v, "%v", err)
		}
		if req.Attributes == nil {
			req.Attributes = map[string]string{}
		}
		req.Attributes[k] = val
	}
	var body []byte
	if path, ok := in.value("--body-file"); ok {
		var err error
		if body, err = a.readInput(path); err != nil {
			return a.fail(err)
		}
	}
	if code := a.connect(); code != exitOK {
		return code
	}
	edit := in.bool("--edit")
	if edit {
		text, code := a.compose(body)
		if code != exitOK {
			return code
		}
		body = text
	}
	req.Body = string(body)
	thought, err := a.c.Create(a.ctx, req)
	if err != nil {
		if edit {
			a.stdout.Write(body) // the user's text never gets lost
		}
		return a.fail(err)
	}
	if a.json {
		a.written(thought.ID, thought.Version)
	} else {
		fmt.Fprintln(a.stdout, thought.ID)
	}
	return exitOK
}

func runShow(a *app, in *input) int {
	ids, code := a.ids(in.verb, in.args)
	if code != exitOK {
		return code
	}
	if code := a.connect(); code != exitOK {
		return code
	}
	st := style{a.color()}
	shown := 0
	return a.each(ids, func(id int64) int {
		thought, err := a.c.Get(a.ctx, id)
		if err != nil {
			return a.fail(err)
		}
		if a.json {
			a.printJSON(thought)
		} else {
			if shown > 0 {
				fmt.Fprintln(a.stdout, "---")
			}
			a.render(thought, st)
		}
		shown++
		return exitOK
	})
}

func runBody(a *app, in *input) int {
	v := in.verb
	if len(in.args) != 1 {
		return a.usagef(v, "one id is required")
	}
	id, err := parseID(in.args[0])
	if err != nil {
		return a.usagef(v, "%v", err)
	}
	if code := a.connect(); code != exitOK {
		return code
	}
	body, _, err := a.c.Body(a.ctx, id)
	if err != nil {
		return a.fail(err)
	}
	a.stdout.Write(body)
	return exitOK
}

func runWrite(a *app, in *input) int {
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
	if pre.IsZero() {
		return a.usagef(v, "--version <n> or --force is required")
	}
	path, ok := in.value("--body-file")
	if !ok {
		path = "-"
	}
	body, err := a.readInput(path)
	if err != nil {
		return a.fail(err)
	}
	if code := a.connect(); code != exitOK {
		return code
	}
	version, err := a.c.PutBody(a.ctx, id, pre, body)
	if err != nil {
		return a.failWrite(id, pre, err)
	}
	if a.json {
		a.written(id, version)
	}
	return exitOK
}

func runTitle(a *app, in *input) int {
	v := in.verb
	if len(in.args) < 2 {
		return a.usagef(v, "an id and a title are required")
	}
	id, err := parseID(in.args[0])
	if err != nil {
		return a.usagef(v, "%v", err)
	}
	title := strings.Join(in.args[1:], " ")
	pre, err := in.precondition()
	if err != nil {
		return a.usagef(v, "%v", err)
	}
	if code := a.connect(); code != exitOK {
		return code
	}
	if pre.IsZero() {
		thought, err := a.c.Get(a.ctx, id)
		if err != nil {
			return a.fail(err)
		}
		pre.Version = thought.Version
	}
	thought, err := a.c.Patch(a.ctx, id, pre, api.Patch{Title: &title})
	if err != nil {
		return a.failWrite(id, pre, err)
	}
	if a.json {
		a.written(id, thought.Version)
	}
	return exitOK
}

func runRm(a *app, in *input) int {
	v := in.verb
	ids, code := a.ids(v, in.args)
	if code != exitOK {
		return code
	}
	pre, err := in.precondition()
	if err != nil {
		return a.usagef(v, "%v", err)
	}
	ask := !in.bool("-y")
	if ask && !isTerminal(a.stdin) {
		return a.usagef(v, "refusing to delete without a terminal to ask on; pass -y")
	}
	if code := a.connect(); code != exitOK {
		return code
	}
	return a.each(ids, func(id int64) int {
		guard := pre
		title := ""
		// GET first for the Version, and for the title to ask about;
		// --force skips it.
		if !guard.Force {
			thought, err := a.c.Get(a.ctx, id)
			if err != nil {
				return a.fail(err)
			}
			title = thought.Title
			if guard.IsZero() {
				guard.Version = thought.Version
			}
		}
		if ask {
			q := fmt.Sprintf("delete thought %d %q? [y/N] ", id, title)
			if title == "" {
				q = fmt.Sprintf("delete thought %d? [y/N] ", id)
			}
			answer, _ := a.prompt(q)
			if ans := strings.ToLower(answer); ans != "y" && ans != "yes" {
				return exitOK
			}
		}
		if err := a.c.Delete(a.ctx, id, guard); err != nil {
			return a.failWrite(id, guard, err)
		}
		return exitOK
	})
}

// ids reads the ids of a batch verb, at least one.
func (a *app) ids(v *verb, args []string) ([]int64, int) {
	if len(args) == 0 {
		return nil, a.usagef(v, "at least one id is required")
	}
	ids, err := parseIDs(args)
	if err != nil {
		return nil, a.usagef(v, "%v", err)
	}
	return ids, exitOK
}

// each runs f for every id, continuing past failures, and returns the code
// of the first failure.
func (a *app) each(ids []int64, f func(int64) int) int {
	first := exitOK
	for _, id := range ids {
		if code := f(id); code != exitOK && first == exitOK {
			first = code
		}
	}
	return first
}

// readInput reads a file, or stdin for "-".
func (a *app) readInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(a.stdin)
	}
	return os.ReadFile(path)
}

// printJSON prints v as one line of JSON.
func (a *app) printJSON(v any) {
	enc := json.NewEncoder(a.stdout)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

// written prints what a mutating verb prints under --json.
func (a *app) written(id, version int64) {
	a.printJSON(struct {
		ID      int64 `json:"id"`
		Version int64 `json:"version"`
	}{id, version})
}
