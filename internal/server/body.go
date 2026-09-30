package server

import (
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/NurramoX/thoughts/internal/api"
)

const markdownType = "text/markdown; charset=utf-8"

func (h *Handler) getBody(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Type", markdownType)
	w.Header().Set("Content-Length", strconv.Itoa(len(thought.Body)))
	io.WriteString(w, thought.Body)
}

func (h *Handler) putBody(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	pre, ok := precondition(w, r, true)
	if !ok {
		return
	}
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/markdown" || !utf8Charset(params) {
		fail(w, http.StatusUnsupportedMediaType, "the body must be sent as "+markdownType)
		return
	}
	body, ok := readRaw(w, r)
	if !ok {
		return
	}
	version, err := h.store.PutBody(r.Context(), id, pre, body)
	if err != nil {
		storeError(w, r, err)
		return
	}
	h.changed(id, version)
	setVersion(w, version)
	w.WriteHeader(http.StatusNoContent)
}

// utf8Charset reports whether a media type's charset, if it names one, is
// UTF-8.
func utf8Charset(params map[string]string) bool {
	cs, ok := params["charset"]
	return !ok || strings.EqualFold(cs, "utf-8")
}

// readRaw reads a raw request body of at most api.MaxBody bytes (413 over)
// that must be valid UTF-8 (422). On failure it has already answered.
func readRaw(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	b, err := io.ReadAll(io.LimitReader(r.Body, api.MaxBody+1))
	switch {
	case err != nil:
		fail(w, http.StatusBadRequest, "reading the request body: "+err.Error())
	case len(b) > api.MaxBody:
		fail(w, http.StatusRequestEntityTooLarge, "request body over 10 MB")
	case !utf8.Valid(b):
		fail(w, http.StatusUnprocessableEntity, "request body is not valid UTF-8")
	default:
		return b, true
	}
	return nil, false
}
