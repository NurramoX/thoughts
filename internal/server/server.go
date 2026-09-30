// Package server is the HTTP API (spec §5) over a store.Store.
package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/store"
)

// Handler is the API.
type Handler struct {
	store   store.Store
	now     func() time.Time
	mux     http.Handler
	changes *hub
}

// New returns the API handler. now is the clock used to resolve relative
// dates in filters, in its own location.
func New(s store.Store, now func() time.Time) *Handler {
	h := &Handler{store: s, now: now, changes: newHub()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.service)
	mux.HandleFunc("GET /events", h.events)
	mux.HandleFunc("GET /thoughts", h.list)
	mux.HandleFunc("POST /thoughts", h.create)
	mux.HandleFunc("GET /thoughts/{id}", h.get)
	mux.HandleFunc("PATCH /thoughts/{id}", h.patch)
	mux.HandleFunc("DELETE /thoughts/{id}", h.delete)
	mux.HandleFunc("GET /thoughts/{id}/body", h.getBody)
	mux.HandleFunc("PUT /thoughts/{id}/body", h.putBody)
	mux.HandleFunc("PUT /thoughts/{id}/tags/{tag}", h.putTag)
	mux.HandleFunc("DELETE /thoughts/{id}/tags/{tag}", h.deleteTag)
	mux.HandleFunc("PUT /thoughts/{id}/attributes/{key}", h.putAttribute)
	mux.HandleFunc("DELETE /thoughts/{id}/attributes/{key}", h.deleteAttribute)
	mux.HandleFunc("GET /tags", h.tags)
	mux.HandleFunc("GET /attributes", h.attributes)
	mux.HandleFunc("GET /attributes/{key}", h.attributeValues)
	h.mux = problems(mux)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// Close ends every GET /events stream and refuses new ones. An
// http.Server's Shutdown waits for handlers to return, and a stream never
// does on its own, so register Close with RegisterOnShutdown.
func (h *Handler) Close() { h.changes.close() }

func (h *Handler) service(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, api.Service{Service: "thoughts", API: api.APIVersion})
}

// writeJSON sends v as an application/json body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
