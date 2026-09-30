package store

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/filter"
)

var now = time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

func day(month time.Month, d int) time.Time { return time.Date(2026, month, d, 10, 0, 0, 0, time.UTC) }

// corpus creates six thoughts, one after the other (so id order is creation
// order), and leaves the clock at now.
func corpus(t *testing.T) (Store, *clock) {
	t.Helper()
	s, c := open(t)
	add := func(at time.Time, req api.CreateRequest) {
		c.Set(at)
		create(t, s, req)
	}
	add(day(1, 10), api.CreateRequest{
		Title:      "Borrow checker ergonomics",
		Body:       "Lifetimes and the borrow checker in Rust.",
		Tags:       []string{"rust", "lang"},
		Attributes: map[string]string{"status": "active", "effort": "Small"},
	})
	add(day(3, 5), api.CreateRequest{
		Title: "cooking pasta",
		Body:  "Borrow a pan from the neighbour.",
		Tags:  []string{"food"},
	})
	add(day(6, 1), api.CreateRequest{
		Title:      "Straße planning",
		Body:       `Roads, key=value pairs and foo-bar; he said "hello world".`,
		Attributes: map[string]string{"status": "done", "city": "Straße"},
	})
	add(day(9, 1), api.CreateRequest{
		Title:      "Istanbul trip",
		Attributes: map[string]string{"city": "İstanbul"},
	})
	add(day(9, 15), api.CreateRequest{
		Title:      "Myth",
		Body:       "Rolling the stone.",
		Attributes: map[string]string{"status": "dropped", "hero": "ΣΊΣΥΦΟΣ"},
	})
	add(day(9, 20), api.CreateRequest{Title: "Another thought", Tags: []string{"food"}})
	c.Set(now)
	return s, c
}

func list(t *testing.T, s Store, q Query) api.List {
	t.Helper()
	l, err := s.List(ctx, q)
	if err != nil {
		t.Fatalf("List(%+v): %v", q, err)
	}
	return l
}

func idsOf(l api.List) []int64 {
	ids := []int64{}
	for _, m := range l.Thoughts {
		ids = append(ids, m.ID)
	}
	return ids
}

func TestFilters(t *testing.T) {
	s, _ := corpus(t)
	text := func(p string) filter.Text { return filter.Text{Phrase: p} }
	attr := func(k, v string) filter.Attr { return filter.Attr{Key: k, Value: v} }
	and := func(es ...filter.Expr) filter.And { return filter.And{Terms: es} }
	or := func(es ...filter.Expr) filter.Or { return filter.Or{Terms: es} }
	not := func(e filter.Expr) filter.Not { return filter.Not{X: e} }
	untouched := and(
		or(attr("status", "raw"), attr("status", "active")),
		not(filter.Date{Field: filter.Updated, From: now.AddDate(0, 0, -90)}), // -updated:90d..
	)
	for _, tc := range []struct {
		name   string
		filter filter.Expr
		want   []int64
	}{
		{"empty filter", nil, []int64{1, 2, 3, 4, 5, 6}},
		{"empty and", and(), []int64{1, 2, 3, 4, 5, 6}},

		// Text.
		{"text over title and body", text("borrow"), []int64{1, 2}},
		{"text is stemmed", text("Borrowing"), []int64{1, 2}},
		{"phrase", text("borrow checker"), []int64{1}},
		{"phrase out of order", text("checker borrow"), nil},
		{"inner quotes", text(`said "hello`), []int64{3}},
		{"MATCH syntax is literal: key=value", text("key=value"), []int64{3}},
		{"MATCH syntax is literal: foo-bar", text("foo-bar"), []int64{3}},
		{"MATCH syntax is literal: AND", text("AND"), []int64{1, 3}},
		{"MATCH syntax is literal: star", text("bor*"), nil},
		{"empty phrase matches nothing", text(""), nil},
		{"no tokens matches nothing", text("--"), nil},
		{"text keeps ß apart from ss", text("strasse"), nil},
		{"title only", filter.Text{Field: filter.TextTitle, Phrase: "borrow"}, []int64{1}},
		{"body only", filter.Text{Field: filter.TextBody, Phrase: "borrow"}, []int64{1, 2}},
		{"body only, title word", filter.Text{Field: filter.TextBody, Phrase: "ergonomics"}, nil},
		{"two ranked terms", and(text("borrow"), text("pan")), []int64{2}},
		{"nested ranked terms", and(text("borrow"), and(filter.Text{Field: filter.TextTitle, Phrase: "checker"})), []int64{1}},
		{"text under or", or(text("pan"), text("stone")), []int64{2, 5}},
		{"text under not", not(text("borrow")), []int64{3, 4, 5, 6}},
		{"ranked and negated text", and(text("borrow"), not(text("pan"))), []int64{1}},
		{"title text under not", not(filter.Text{Field: filter.TextTitle, Phrase: "borrow"}), []int64{2, 3, 4, 5, 6}},
		{"double negation", not(not(text("pan"))), []int64{2}},

		// Attributes, under simple case folding.
		{"status", attr("status", "RAW"), []int64{2, 4, 6}},
		{"any-of", or(attr("status", "done"), attr("status", "dropped")), []int64{3, 5}},
		{"value folds case", attr("effort", "SMALL"), []int64{1}},
		{"ß is not ss", attr("city", "strasse"), nil},
		{"ß equals ẞ", attr("city", "STRAẞE"), []int64{3}},
		{"İ is not I", attr("city", "istanbul"), nil},
		{"İ equals İ", attr("city", "İSTANBUL"), []int64{4}},
		{"final sigma", attr("hero", "σίσυφος"), []int64{5}},
		{"value is compared whole", attr("effort", "Smal"), nil},
		{"unknown key", attr("nope", "x"), nil},
		{"values are bound, not interpolated", attr("effort", "' OR 1=1 --"), nil},
		{"unknown key negated", not(attr("nope", "x")), []int64{1, 2, 3, 4, 5, 6}},

		// Tags and ids.
		{"tag", filter.Tag{Tag: "food"}, []int64{2, 6}},
		{"unknown tag", filter.Tag{Tag: "nope"}, nil},
		{"id", filter.ID{ID: 2}, []int64{2}},
		{"unknown id", filter.ID{ID: 99}, nil},

		// has:
		{"has:tag", filter.Has{Key: "tag"}, []int64{1, 2, 6}},
		{"has:created", filter.Has{Key: "created"}, []int64{1, 2, 3, 4, 5, 6}},
		{"has:updated", filter.Has{Key: "updated"}, []int64{1, 2, 3, 4, 5, 6}},
		{"has:status", filter.Has{Key: "status"}, []int64{1, 2, 3, 4, 5, 6}},
		{"has:attribute", filter.Has{Key: "city"}, []int64{3, 4}},
		{"has:unknown", filter.Has{Key: "nope"}, nil},

		// Dates: [From, To).
		{"created in September", filter.Date{Field: filter.Created, From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}, []int64{4, 5, 6}},
		{"From is inclusive", filter.Date{Field: filter.Created, From: day(9, 1)}, []int64{4, 5, 6}},
		{"To is exclusive", filter.Date{Field: filter.Created, To: day(9, 1)}, []int64{1, 2, 3}},
		{"sub-millisecond From rounds up", filter.Date{Field: filter.Created, From: day(9, 1).Add(time.Nanosecond)}, []int64{5, 6}},
		{"sub-millisecond To rounds up", filter.Date{Field: filter.Created, To: day(9, 1).Add(time.Nanosecond)}, []int64{1, 2, 3, 4}},
		{"other time zones", filter.Date{Field: filter.Created, From: day(9, 1).In(time.FixedZone("X", -7*3600))}, []int64{4, 5, 6}},
		{"updated", filter.Date{Field: filter.Updated, From: day(9, 15)}, []int64{5, 6}},
		{"unbounded date", filter.Date{Field: filter.Updated}, []int64{1, 2, 3, 4, 5, 6}},
		{"negated date", not(filter.Date{Field: filter.Updated, From: day(8, 1)}), []int64{1, 2, 3}},
		{"open and untouched for 90 days", untouched, []int64{1, 2}},

		// Combinations.
		{"and", and(filter.Tag{Tag: "food"}, attr("status", "raw"), filter.Date{Field: filter.Created, To: day(9, 1)}), []int64{2}},
		{"or of ands", or(and(filter.Tag{Tag: "rust"}, attr("effort", "small")), filter.ID{ID: 5}), []int64{1, 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := list(t, s, Query{Filter: tc.filter, Sort: SortCreated})
			want := tc.want
			if want == nil {
				want = []int64{}
			}
			if got := idsOf(l); !reflect.DeepEqual(got, want) {
				t.Errorf("ids = %v, want %v", got, want)
			}
			if l.Total != len(want) {
				t.Errorf("total = %d, want %d", l.Total, len(want))
			}
			ranked := len(filter.Ranked(tc.filter)) > 0
			for _, m := range l.Thoughts {
				if (m.Snippet != "") != ranked {
					t.Errorf("thought %d: snippet %q with ranked=%v", m.ID, m.Snippet, ranked)
				}
			}
		})
	}
}

func TestRankingAndSnippets(t *testing.T) {
	s, _ := open(t)
	create(t, s, api.CreateRequest{Title: "Pans", Body: strings.Repeat("word ", 40) + "a borrowed pan, " + strings.Repeat("filler ", 40)})
	create(t, s, api.CreateRequest{Title: "Borrow", Body: "unrelated"})
	create(t, s, api.CreateRequest{Title: "Nothing here", Body: "none"})

	q := Query{Filter: filter.Text{Phrase: "borrow"}, Sort: SortRank, Desc: true}
	l := list(t, s, q)
	if got := idsOf(l); !reflect.DeepEqual(got, []int64{2, 1}) {
		t.Fatalf("best first = %v, want [2 1] (title outweighs body)", got)
	}
	if got := l.Thoughts[1].Snippet; !strings.Contains(got, "a borrowed pan") || !strings.Contains(got, "…") || strings.ContainsAny(got, "<>[]") {
		t.Errorf("snippet = %q", got)
	}
	if got := l.Thoughts[0].Snippet; got != "Borrow" {
		t.Errorf("title snippet = %q", got)
	}
	q.Desc = false
	if got := idsOf(list(t, s, q)); !reflect.DeepEqual(got, []int64{1, 2}) {
		t.Errorf("worst first = %v", got)
	}

	// Text under or ranks nothing: no snippet, and sort=rank is refused.
	unranked := filter.Or{Terms: []filter.Expr{filter.Text{Phrase: "borrow"}, filter.ID{ID: 3}}}
	if _, err := s.List(ctx, Query{Filter: unranked, Sort: SortRank}); err == nil {
		t.Error("sort=rank without a ranked term succeeded")
	}
	for _, m := range list(t, s, Query{Filter: unranked, Sort: SortUpdated}).Thoughts {
		if m.Snippet != "" {
			t.Errorf("thought %d has snippet %q", m.ID, m.Snippet)
		}
	}
}

func TestSorts(t *testing.T) {
	s, c := corpus(t)
	// Thought 6 gets a new title and the latest update.
	if _, err := s.Patch(ctx, 6, none, api.Patch{Title: str("apple")}); err != nil {
		t.Fatal(err)
	}
	c.Advance(time.Hour)
	if _, err := s.PutTag(ctx, 3, none, "late"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		sort string
		desc bool
		want []int64
	}{
		{SortUpdated, true, []int64{3, 6, 5, 4, 2, 1}},
		{SortUpdated, false, []int64{1, 2, 4, 5, 6, 3}},
		{SortCreated, true, []int64{6, 5, 4, 3, 2, 1}},
		{SortCreated, false, []int64{1, 2, 3, 4, 5, 6}},

		{SortTitle, false, []int64{6, 1, 2, 4, 5, 3}}, // apple, Borrow, cooking, Istanbul, Myth, Straße
		{SortTitle, true, []int64{3, 5, 4, 2, 1, 6}},
	} {
		got := idsOf(list(t, s, Query{Sort: tc.sort, Desc: tc.desc}))
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("sort %s desc=%v: %v, want %v", tc.sort, tc.desc, got, tc.want)
		}
	}
	if _, err := s.List(ctx, Query{Sort: "size"}); err == nil {
		t.Error("unknown sort succeeded")
	}
}

func TestTitleSortFoldsCaseBeyondASCII(t *testing.T) {
	s, _ := open(t)
	for _, title := range []string{"Σb", "σa", "b", "A"} {
		create(t, s, api.CreateRequest{Title: title})
	}
	// σa before Σb: SQLite's NOCASE, ASCII-only, would put Σb first.
	if got := idsOf(list(t, s, Query{Sort: SortTitle})); !reflect.DeepEqual(got, []int64{4, 3, 2, 1}) {
		t.Errorf("asc: %v, want A, b, σa, Σb", got)
	}
}

func TestTiesBreakOnID(t *testing.T) {
	s, _ := open(t) // the clock stands still: every timestamp ties
	for range 4 {
		create(t, s, api.CreateRequest{Title: "same"})
	}
	if got := idsOf(list(t, s, Query{Sort: SortUpdated, Desc: true})); !reflect.DeepEqual(got, []int64{4, 3, 2, 1}) {
		t.Errorf("desc: %v", got)
	}
	if got := idsOf(list(t, s, Query{Sort: SortTitle})); !reflect.DeepEqual(got, []int64{1, 2, 3, 4}) {
		t.Errorf("asc: %v", got)
	}
}

func TestLimitOffsetAndTotal(t *testing.T) {
	s, _ := corpus(t)
	for _, tc := range []struct {
		limit, offset int
		want          []int64
	}{
		{2, 0, []int64{1, 2}},
		{2, 2, []int64{3, 4}},
		{0, 4, []int64{5, 6}},
		{10, 5, []int64{6}},
		{3, 9, []int64{}},
	} {
		l := list(t, s, Query{Sort: SortCreated, Limit: tc.limit, Offset: tc.offset})
		if got := idsOf(l); !reflect.DeepEqual(got, tc.want) || l.Total != 6 {
			t.Errorf("limit %d offset %d: %v total %d, want %v total 6", tc.limit, tc.offset, got, l.Total, tc.want)
		}
	}
	l := list(t, s, Query{Filter: filter.Text{Phrase: "borrow"}, Sort: SortRank, Desc: true, Limit: 1})
	if len(l.Thoughts) != 1 || l.Total != 2 {
		t.Errorf("ranked: %d thoughts, total %d", len(l.Thoughts), l.Total)
	}
}

func TestListCarriesMetadata(t *testing.T) {
	s, _ := corpus(t)
	l := list(t, s, Query{Filter: filter.ID{ID: 1}, Sort: SortUpdated})
	want := api.Meta{
		ID:         1,
		Title:      "Borrow checker ergonomics",
		Tags:       []string{"lang", "rust"},
		Attributes: map[string]string{"effort": "Small", "status": "active"},
		Version:    1,
		CreatedAt:  api.Time{Time: day(1, 10)},
		UpdatedAt:  api.Time{Time: day(1, 10)},
	}
	if len(l.Thoughts) != 1 || !reflect.DeepEqual(l.Thoughts[0], want) {
		t.Errorf("List = %+v\nwant   %+v", l.Thoughts, want)
	}
	l = list(t, s, Query{Filter: filter.ID{ID: 4}, Sort: SortUpdated})
	if l.Thoughts[0].Tags == nil || len(l.Thoughts[0].Tags) != 0 {
		t.Errorf("tags of an untagged thought = %#v, want empty", l.Thoughts[0].Tags)
	}
	if empty := list(t, s, Query{Filter: filter.ID{ID: 99}, Sort: SortUpdated}); empty.Thoughts == nil {
		t.Error("an empty list is null")
	}
}

func TestVocabulary(t *testing.T) {
	s, _ := corpus(t)
	tags, err := s.Tags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantTags := []api.TagCount{{Tag: "food", Count: 2}, {Tag: "lang", Count: 1}, {Tag: "rust", Count: 1}}
	if !reflect.DeepEqual(tags, wantTags) {
		t.Errorf("Tags = %v, want %v", tags, wantTags)
	}
	keys, err := s.Attributes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantKeys := []api.KeyCount{{Key: "city", Count: 2}, {Key: "effort", Count: 1}, {Key: "hero", Count: 1}, {Key: "status", Count: 6}}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Errorf("Attributes = %v, want %v", keys, wantKeys)
	}
	values, err := s.AttributeValues(ctx, "STATUS")
	if err != nil {
		t.Fatal(err)
	}
	wantValues := []api.ValueCount{{Value: "active", Count: 1}, {Value: "done", Count: 1}, {Value: "dropped", Count: 1}, {Value: "raw", Count: 3}}
	if !reflect.DeepEqual(values, wantValues) {
		t.Errorf("AttributeValues(status) = %v, want %v", values, wantValues)
	}
	values, err = s.AttributeValues(ctx, "nope")
	if err != nil || values == nil || len(values) != 0 {
		t.Errorf("AttributeValues(nope) = %#v, %v", values, err)
	}

	// A tag or key exists exactly while a thought carries it.
	if _, err := s.DeleteTag(ctx, 1, none, "lang"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteAttribute(ctx, 1, none, "effort"); err != nil {
		t.Fatal(err)
	}
	tags, _ = s.Tags(ctx)
	keys, _ = s.Attributes(ctx)
	if len(tags) != 2 || len(keys) != 3 {
		t.Errorf("after removal: tags %v, keys %v", tags, keys)
	}
}

func TestEmptyVocabulary(t *testing.T) {
	s, _ := open(t)
	tags, err := s.Tags(ctx)
	if err != nil || tags == nil || len(tags) != 0 {
		t.Errorf("Tags = %#v, %v", tags, err)
	}
	keys, err := s.Attributes(ctx)
	if err != nil || keys == nil || len(keys) != 0 {
		t.Errorf("Attributes = %#v, %v", keys, err)
	}
}
