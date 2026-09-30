package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/NurramoX/thoughts/internal/api"
)

// createRequest is api.CreateRequest with the title's presence visible.
type createRequest struct {
	Title      *string           `json:"title"`
	Body       string            `json:"body"`
	Tags       []string          `json:"tags"`
	Attributes map[string]string `json:"attributes"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Title == nil {
		fail(w, http.StatusUnprocessableEntity, "title is required")
		return
	}
	thought, err := h.store.Create(r.Context(), api.CreateRequest{
		Title: *req.Title, Body: req.Body, Tags: req.Tags, Attributes: req.Attributes,
	})
	if err != nil {
		storeError(w, r, err)
		return
	}
	h.changed(thought.ID, thought.Version)
	w.Header().Set("Location", "/thoughts/"+strconv.FormatInt(thought.ID, 10))
	setVersion(w, thought.Version)
	writeJSON(w, http.StatusCreated, thought)
}

// patchRequest is api.Patch with each member left raw, so an explicit null
// can be told from an absent member.
type patchRequest struct {
	Title      json.RawMessage `json:"title"`
	Body       json.RawMessage `json:"body"`
	Tags       json.RawMessage `json:"tags"`
	Attributes json.RawMessage `json:"attributes"`
}

// patch reads req into an api.Patch. In a merge patch null removes a member,
// but title, body, tags and the attribute set cannot be removed, so a null
// there is a 422; a null attribute value removes that key. A member of the
// wrong JSON type is a 400. On failure it has already answered.
func (req patchRequest) patch(w http.ResponseWriter) (api.Patch, bool) {
	var p api.Patch
	for _, m := range []struct {
		name string
		raw  json.RawMessage
		dst  any
	}{
		{"title", req.Title, &p.Title},
		{"body", req.Body, &p.Body},
		{"tags", req.Tags, &p.Tags},
		{"attributes", req.Attributes, &p.Attributes},
	} {
		if m.raw == nil {
			continue
		}
		if string(m.raw) == "null" {
			fail(w, http.StatusUnprocessableEntity, m.name+" cannot be null")
			return api.Patch{}, false
		}
		if err := json.Unmarshal(m.raw, m.dst); err != nil {
			fail(w, http.StatusBadRequest, "malformed "+m.name+": "+err.Error())
			return api.Patch{}, false
		}
	}
	return p, true
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	pre, ok := precondition(w, r, true)
	if !ok {
		return
	}
	var req patchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	p, ok := req.patch(w)
	if !ok {
		return
	}
	thought, err := h.store.Patch(r.Context(), id, pre, p)
	if err != nil {
		storeError(w, r, err)
		return
	}
	h.changed(id, thought.Version)
	out := api.PatchedThought{Meta: thought.Meta}
	if p.Body != nil {
		out.Body = &thought.Body
	}
	setVersion(w, thought.Version)
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	pre, ok := precondition(w, r, true)
	if !ok {
		return
	}
	if err := h.store.Delete(r.Context(), id, pre); err != nil {
		storeError(w, r, err)
		return
	}
	h.changes.publish(api.Change{ID: id, Deleted: true})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	thought, err := h.store.Get(r.Context(), id)
	if err != nil {
		storeError(w, r, err)
		return
	}
	if notModified(w, r, thought.Version) {
		return
	}
	setVersion(w, thought.Version)
	writeJSON(w, http.StatusOK, thought)
}
