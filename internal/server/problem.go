package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/store"
)

// writeProblem sends an RFC 9457 problem document whose title is the
// status text.
func writeProblem(w http.ResponseWriter, p api.Problem) {
	if p.Title == "" {
		p.Title = http.StatusText(p.Status)
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	json.NewEncoder(w).Encode(p)
}

// fail sends a problem with the given status and detail.
func fail(w http.ResponseWriter, status int, detail string) {
	writeProblem(w, api.Problem{Status: status, Detail: detail})
}

// storeError maps a store error to its problem: ErrNotFound 404 naming the
// id, so a batch of several ids tells which one is missing,
// *InvalidError 422, ErrTooLarge 413, *StaleError 412 carrying the current
// Version (also sent as ETag), anything else 500.
func storeError(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *store.InvalidError
	var stale *store.StaleError
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "thought "+r.PathValue("id")+" not found")
	case errors.Is(err, store.ErrTooLarge):
		fail(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.As(err, &invalid):
		fail(w, http.StatusUnprocessableEntity, invalid.Msg)
	case errors.As(err, &stale):
		w.Header().Set("ETag", api.ETag(stale.Current))
		writeProblem(w, api.Problem{
			Status:         http.StatusPreconditionFailed,
			Detail:         err.Error(),
			CurrentVersion: stale.Current,
		})
	default:
		fail(w, http.StatusInternalServerError, err.Error())
	}
}

// problemRecorder stands in for the ResponseWriter of the mux's own 404 and
// 405 handlers, keeping only the status and headers so the answer can be
// resent as a problem document.
type problemRecorder struct {
	header http.Header
	status int
}

func (r *problemRecorder) Header() http.Header         { return r.header }
func (r *problemRecorder) Write(b []byte) (int, error) { return len(b), nil }
func (r *problemRecorder) WriteHeader(status int)      { r.status = status }

// problems wraps mux so that requests it has no route for (404, 405) get a
// problem document instead of its plain-text answer. Its redirects pass
// through untouched.
func problems(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		rec := &problemRecorder{header: http.Header{}, status: http.StatusOK}
		h.ServeHTTP(rec, r)
		if rec.status < 400 {
			h.ServeHTTP(w, r)
			return
		}
		if allow := rec.header.Get("Allow"); allow != "" {
			w.Header().Set("Allow", allow)
		}
		fail(w, rec.status, "")
	})
}
