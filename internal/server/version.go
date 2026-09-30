package server

import (
	"net/http"
	"strings"

	"github.com/NurramoX/thoughts/internal/api"
)

// setVersion exposes a Version as the ETag.
func setVersion(w http.ResponseWriter, version int64) {
	w.Header().Set("ETag", api.ETag(version))
}

// precondition reads If-Match: `*` forces, a single entity tag names a
// Version, and anything else is a 400. When required, a missing If-Match is a
// 428. On failure it has already answered.
func precondition(w http.ResponseWriter, r *http.Request, required bool) (api.Precondition, bool) {
	v := strings.TrimSpace(r.Header.Get("If-Match"))
	switch {
	case v == "" && required:
		fail(w, http.StatusPreconditionRequired, `If-Match is required: send the version you last saw, or "*" to overwrite`)
		return api.Precondition{}, false
	case v == "":
		return api.Precondition{}, true
	case v == "*":
		return api.Precondition{Force: true}, true
	}
	version, ok := api.ParseETag(v)
	if !ok {
		fail(w, http.StatusBadRequest, `malformed If-Match: want a version such as "7", or "*"`)
		return api.Precondition{}, false
	}
	return api.Precondition{Version: version}, true
}

// notModified reports whether If-None-Match names the current Version (weak
// comparison, lists and `*` allowed) and, if so, answers 304 with the ETag.
// Entity tags it cannot read never match.
func notModified(w http.ResponseWriter, r *http.Request, version int64) bool {
	inm := r.Header.Get("If-None-Match")
	if inm == "" {
		return false
	}
	for _, tag := range strings.Split(inm, ",") {
		tag = strings.TrimPrefix(strings.TrimSpace(tag), "W/")
		if v, ok := api.ParseETag(tag); tag == "*" || ok && v == version {
			setVersion(w, version)
			w.WriteHeader(http.StatusNotModified)
			return true
		}
	}
	return false
}
