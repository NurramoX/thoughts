package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/filter"
	"github.com/NurramoX/thoughts/internal/store"
)

// defaultDesc is each sort's default order: best first for rank, newest
// first for the dates, A to Z for title.
var defaultDesc = map[string]bool{
	store.SortUpdated: true,
	store.SortCreated: true,
	store.SortTitle:   false,
	store.SortRank:    true,
}

// list serves GET /thoughts. Every parameter is optional and may appear once;
// an unknown parameter is a 400. limit is at least 1 and offset at least 0.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q, problem := h.query(r.URL.Query())
	if problem != nil {
		writeProblem(w, *problem)
		return
	}
	l, err := h.store.List(r.Context(), q)
	if err != nil {
		storeError(w, r, err)
		return
	}
	if l.Thoughts == nil {
		l.Thoughts = []api.Meta{}
	}
	writeJSON(w, http.StatusOK, l)
}

// query turns the parameters of GET /thoughts into a store.Query with the
// defaults resolved.
func (h *Handler) query(params url.Values) (store.Query, *api.Problem) {
	bad := func(format string, args ...any) (store.Query, *api.Problem) {
		return store.Query{}, &api.Problem{Status: http.StatusBadRequest, Detail: fmt.Sprintf(format, args...)}
	}
	for name, vs := range params {
		switch name {
		case "filter", "sort", "order", "limit", "offset":
		default:
			return bad("unknown parameter %q", name)
		}
		if len(vs) > 1 {
			return bad("parameter %q given %d times", name, len(vs))
		}
	}

	var q store.Query
	if src := params.Get("filter"); src != "" {
		expr, err := filter.Parse(src, h.now())
		if err != nil {
			p := api.Problem{Status: http.StatusBadRequest, Detail: "filter: " + err.Error()}
			var perr *filter.Error
			if errors.As(err, &perr) {
				p.Position = perr.Position
			}
			return store.Query{}, &p
		}
		q.Filter = expr
	}
	ranked := filter.Ranked(q.Filter) != nil

	q.Sort = params.Get("sort")
	switch {
	case q.Sort == "" && ranked:
		q.Sort = store.SortRank
	case q.Sort == "":
		q.Sort = store.SortUpdated
	case q.Sort == store.SortRank && !ranked:
		return bad("sort=rank needs a text term that is not under `or` or `-`")
	}
	desc, known := defaultDesc[q.Sort]
	if !known {
		return bad("unknown sort %q: want updated, created, title or rank", q.Sort)
	}
	switch order := params.Get("order"); order {
	case "":
		q.Desc = desc
	case "asc", "desc":
		q.Desc = order == "desc"
	default:
		return bad("unknown order %q: want asc or desc", order)
	}

	var err error
	if q.Limit, err = number(params, "limit", 1); err != nil {
		return bad("%v", err)
	}
	if q.Offset, err = number(params, "offset", 0); err != nil {
		return bad("%v", err)
	}
	return q, nil
}

// number reads an optional integer parameter of at least min; absent is 0.
func number(params url.Values, name string, min int) (int, error) {
	s := params.Get(name)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < min {
		return 0, fmt.Errorf("%s must be an integer of at least %d, got %q", name, min, s)
	}
	return n, nil
}
