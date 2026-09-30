package client

import (
	"errors"
	"net/http"
)

// Status is the HTTP status of a *ProblemError in err's chain, 0 otherwise.
func Status(err error) int {
	var pe *ProblemError
	if errors.As(err, &pe) {
		return pe.Problem.Status
	}
	return 0
}

// Stale reports whether err is a 412, with the thought's current Version.
func Stale(err error) (current int64, ok bool) {
	var pe *ProblemError
	if errors.As(err, &pe) && pe.Problem.Status == http.StatusPreconditionFailed {
		return pe.Problem.CurrentVersion, true
	}
	return 0, false
}

// FilterError reports whether err is a Filter parse error: a 400 carrying the
// 1-based rune position of the offending character.
func FilterError(err error) (position int, detail string, ok bool) {
	var pe *ProblemError
	if errors.As(err, &pe) && pe.Problem.Status == http.StatusBadRequest && pe.Problem.Position > 0 {
		return pe.Problem.Position, pe.Problem.Detail, true
	}
	return 0, "", false
}
