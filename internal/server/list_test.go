package server

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/filter"
	"github.com/NurramoX/thoughts/internal/store"
)

// list sends GET /thoughts with the query and returns the Query the store got.
func list(t *testing.T, query string, code int) store.Query {
	t.Helper()
	ts, fs := newTestServer(t)
	resp := do(t, ts, "GET", "/thoughts?"+query, "")
	if code != http.StatusOK {
		wantProblem(t, resp, code)
		if fs.listed {
			t.Fatalf("store was queried for a rejected list %q", query)
		}
		return store.Query{}
	}
	wantStatus(t, resp, code)
	return fs.lastQuery
}

func TestListShape(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := do(t, ts, "GET", "/thoughts", "")
	wantStatus(t, resp, http.StatusOK)
	if got := strings.TrimSpace(readBody(t, resp)); got != `{"thoughts":[],"total":0}` {
		t.Fatalf("empty list %s", got)
	}

	create(t, ts, `{"title":"a","body":"secret body"}`)
	resp = do(t, ts, "GET", "/thoughts", "")
	raw := readBody(t, resp)
	if strings.Contains(raw, "body") || !strings.Contains(raw, `"total":1`) {
		t.Fatalf("list %s", raw)
	}
}

func TestListDefaultsAndParameters(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  store.Query
	}{
		{"", store.Query{Sort: "updated", Desc: true}},
		{"filter=", store.Query{Sort: "updated", Desc: true}},
		{"sort=created", store.Query{Sort: "created", Desc: true}},

		{"sort=title", store.Query{Sort: "title"}},
		{"sort=title&order=desc", store.Query{Sort: "title", Desc: true}},
		{"order=asc", store.Query{Sort: "updated"}},
		{"limit=5&offset=10", store.Query{Sort: "updated", Desc: true, Limit: 5, Offset: 10}},
		{"offset=0", store.Query{Sort: "updated", Desc: true}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := list(t, tc.query, http.StatusOK); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("query %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestListRejectsBadParameters(t *testing.T) {
	for _, q := range []string{
		"sort=size", "sort=Title", "order=up", "limit=0", "limit=-1", "limit=x",
		"offset=-1", "offset=1.5", "sort=rank", "colour=red", "sort=title&sort=created",
	} {
		t.Run(q, func(t *testing.T) { list(t, q, http.StatusBadRequest) })
	}
}

// The tests below need the real filter.Parse.

func TestListParsesTheFilterAgainstNow(t *testing.T) {
	got := list(t, "filter="+url.QueryEscape("tag:rust updated:..90d"), http.StatusOK)
	want, err := filter.Parse("tag:rust updated:..90d", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Filter, want) {
		t.Fatalf("filter %#v, want %#v", got.Filter, want)
	}
	if got.Sort != "updated" || !got.Desc {
		t.Fatalf("an unranked filter should default to updated desc, got %+v", got)
	}
}

func TestListFilterErrorCarriesPosition(t *testing.T) {
	ts, _ := newTestServer(t)
	p := wantProblem(t, do(t, ts, "GET", "/thoughts?filter="+url.QueryEscape("tag:rust (x"), ""), http.StatusBadRequest)
	if p.Position < 1 {
		t.Fatalf("problem %+v without position", p)
	}
}

func TestListRankedTextDefaultsToRank(t *testing.T) {
	got := list(t, "filter="+url.QueryEscape("borrow checker tag:rust"), http.StatusOK)
	if got.Sort != "rank" || !got.Desc {
		t.Fatalf("a ranked filter should default to rank, best first; got %+v", got)
	}
	got = list(t, "filter=borrow&sort=rank&order=asc", http.StatusOK)
	if got.Sort != "rank" || got.Desc {
		t.Fatalf("got %+v", got)
	}
	got = list(t, "filter=borrow&sort=title", http.StatusOK)
	if got.Sort != "title" || got.Desc {
		t.Fatalf("got %+v", got)
	}
	// Text under `-` is unranked.
	list(t, "filter="+url.QueryEscape("-borrow")+"&sort=rank", http.StatusBadRequest)
}

func TestVocabulary(t *testing.T) {
	ts, _ := newTestServer(t)
	for _, path := range []string{"/tags", "/attributes", "/attributes/effort"} {
		resp := do(t, ts, "GET", path, "")
		wantStatus(t, resp, http.StatusOK)
		if got := strings.TrimSpace(readBody(t, resp)); got != "[]" {
			t.Fatalf("empty %s = %s", path, got)
		}
	}

	create(t, ts, `{"title":"a","tags":["go","rust"],"attributes":{"effort":"small"}}`)
	create(t, ts, `{"title":"b","tags":["rust"],"attributes":{"effort":"small"}}`)
	if got := decode[[]api.TagCount](t, do(t, ts, "GET", "/tags", "")); !reflect.DeepEqual(got,
		[]api.TagCount{{Tag: "go", Count: 1}, {Tag: "rust", Count: 2}}) {
		t.Fatalf("tags %+v", got)
	}
	if got := decode[[]api.KeyCount](t, do(t, ts, "GET", "/attributes", "")); !reflect.DeepEqual(got,
		[]api.KeyCount{{Key: "effort", Count: 2}, {Key: "status", Count: 2}}) {
		t.Fatalf("attributes %+v", got)
	}
	if got := decode[[]api.ValueCount](t, do(t, ts, "GET", "/attributes/effort", "")); !reflect.DeepEqual(got,
		[]api.ValueCount{{Value: "small", Count: 2}}) {
		t.Fatalf("values %+v", got)
	}
}
