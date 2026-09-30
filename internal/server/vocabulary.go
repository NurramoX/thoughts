package server

import (
	"net/http"

	"github.com/NurramoX/thoughts/internal/api"
)

func (h *Handler) tags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.store.Tags(r.Context())
	vocabulary(w, r, tags, err)
}

func (h *Handler) attributes(w http.ResponseWriter, r *http.Request) {
	keys, err := h.store.Attributes(r.Context())
	vocabulary(w, r, keys, err)
}

func (h *Handler) attributeValues(w http.ResponseWriter, r *http.Request) {
	values, err := h.store.AttributeValues(r.Context(), r.PathValue("key"))
	vocabulary(w, r, values, err)
}

// vocabulary answers a vocabulary endpoint with a JSON array, never null.
func vocabulary[T api.TagCount | api.KeyCount | api.ValueCount](w http.ResponseWriter, r *http.Request, items []T, err error) {
	if err != nil {
		storeError(w, r, err)
		return
	}
	if items == nil {
		items = []T{}
	}
	writeJSON(w, http.StatusOK, items)
}
