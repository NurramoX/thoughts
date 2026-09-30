package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
	"github.com/NurramoX/thoughts/internal/filter"
)

// fail reports err and returns its exit code. A problem is shown as
// `thought: <title>: <detail>`, or verbatim under --json.
func (a *app) fail(err error) int {
	var pe *client.ProblemError
	var ue *client.UnreachableError
	switch {
	case errors.As(err, &pe):
		a.problem(pe)
		return statusCode(pe.Problem.Status)
	case errors.As(err, &ue):
		a.errorf("%v", ue)
		return exitUnreachable
	}
	a.errorf("%v", err)
	return exitInternal
}

func (a *app) problem(pe *client.ProblemError) {
	if a.json {
		raw := pe.Raw
		if len(raw) == 0 {
			raw, _ = json.Marshal(pe.Problem)
		}
		a.stderr.Write(raw)
		return
	}
	a.errorf("%v", pe)
}

// failWrite reports the error of a write guarded by pre, spelling out a 412.
func (a *app) failWrite(id int64, pre api.Precondition, err error) int {
	if cur, ok := client.Stale(err); ok && !a.json {
		a.changed(id, pre, cur)
		return exitStale
	}
	return a.fail(err)
}

// changed is the 412 message.
func (a *app) changed(id int64, pre api.Precondition, current int64) {
	fmt.Fprintf(a.stderr, "thought %d changed: you had version %d, it is now %d\n", id, pre.Version, current)
}

// failFilter reports the error of a request carrying the Filter src, with a
// caret under the position of a parse error.
func (a *app) failFilter(src string, err error) int {
	code := a.fail(err)
	if pos, _, ok := client.FilterError(err); ok && !a.json {
		fmt.Fprintln(a.stderr, filter.Caret(src, pos))
	}
	return code
}

// statusCode maps an HTTP status to an exit code.
func statusCode(status int) int {
	switch status {
	case http.StatusBadRequest:
		return exitMalformed
	case http.StatusNotFound:
		return exitNotFound
	case http.StatusPreconditionFailed:
		return exitStale
	case http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return exitRejected
	}
	return exitInternal
}
