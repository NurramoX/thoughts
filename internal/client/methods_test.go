package client_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
)

// seen is one request as the server received it.
type seen struct {
	Method, URI, IfMatch, IfNoneMatch, ContentType, Body string
}

// record serves one canned answer and records the request.
func record(t *testing.T, status int, header map[string]string, body string) (client.Client, *seen) {
	t.Helper()
	got := new(seen)
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*got = seen{
			Method:      r.Method,
			URI:         r.RequestURI,
			IfMatch:     r.Header.Get("If-Match"),
			IfNoneMatch: r.Header.Get("If-None-Match"),
			ContentType: r.Header.Get("Content-Type"),
			Body:        string(b),
		}
		for k, v := range header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	})
	return c, got
}

func check(t *testing.T, got *seen, want seen) {
	t.Helper()
	if *got != want {
		t.Errorf("request\n got %+v\nwant %+v", *got, want)
	}
}

const envelope = `{"id":42,"title":"T","tags":["a"],"attributes":{"status":"raw"},"body":"b\n","version":3,` +
	`"created_at":"2026-09-22T14:03:07.412Z","updated_at":"2026-09-22T14:03:07.412Z"}`

var ctx = context.Background()

func TestCreate(t *testing.T) {
	c, got := record(t, 201, map[string]string{"ETag": `"1"`, "Location": "/thoughts/42"}, envelope)
	thought, err := c.Create(ctx, api.CreateRequest{Title: "T", Tags: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, seen{Method: "POST", URI: "/thoughts", ContentType: "application/json", Body: `{"title":"T","tags":["a"]}`})
	if thought.ID != 42 || thought.Body != "b\n" || thought.Version != 3 || thought.Attributes["status"] != "raw" {
		t.Errorf("got %+v", thought)
	}
}

func TestGet(t *testing.T) {
	c, got := record(t, 200, map[string]string{"ETag": `"3"`}, envelope)
	thought, err := c.Get(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, seen{Method: "GET", URI: "/thoughts/42"})
	if thought.Title != "T" {
		t.Errorf("got %+v", thought)
	}
}

func TestRevalidate(t *testing.T) {
	t.Run("not modified", func(t *testing.T) {
		c, got := record(t, 304, map[string]string{"ETag": `"3"`}, "")
		thought, nm, err := c.Revalidate(ctx, 42, 3)
		if err != nil {
			t.Fatal(err)
		}
		check(t, got, seen{Method: "GET", URI: "/thoughts/42", IfNoneMatch: `"3"`})
		if !nm || thought.ID != 0 {
			t.Errorf("got %+v %v", thought, nm)
		}
	})
	t.Run("modified", func(t *testing.T) {
		c, _ := record(t, 200, map[string]string{"ETag": `"3"`}, envelope)
		thought, nm, err := c.Revalidate(ctx, 42, 2)
		if err != nil || nm || thought.ID != 42 {
			t.Errorf("got %+v %v %v", thought, nm, err)
		}
	})
}

func TestList(t *testing.T) {
	c, got := record(t, 200, nil, `{"thoughts":[],"total":0}`)
	l, err := c.List(ctx, client.ListParams{Filter: "tag:rust borrow", Sort: "rank", Order: "asc", Limit: 5, Offset: 10})
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, seen{Method: "GET", URI: "/thoughts?filter=tag%3Arust+borrow&limit=5&offset=10&order=asc&sort=rank"})
	if l.Thoughts == nil || l.Total != 0 {
		t.Errorf("got %+v", l)
	}

	c, got = record(t, 200, nil, `{"thoughts":[],"total":0}`)
	if _, err := c.List(ctx, client.ListParams{}); err != nil {
		t.Fatal(err)
	}
	check(t, got, seen{Method: "GET", URI: "/thoughts"})
}

func TestPatch(t *testing.T) {
	c, got := record(t, 200, map[string]string{"ETag": `"4"`}, `{"id":42,"title":"New","tags":[],"attributes":{"status":"raw"},"version":4,`+
		`"created_at":"2026-09-22T14:03:07.412Z","updated_at":"2026-09-22T14:03:07.412Z"}`)
	title := "New"
	p, err := c.Patch(ctx, 42, api.Precondition{Version: 3}, api.Patch{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, seen{Method: "PATCH", URI: "/thoughts/42", IfMatch: `"3"`, ContentType: "application/json", Body: `{"title":"New"}`})
	if p.Version != 4 || p.Body != nil {
		t.Errorf("got %+v", p)
	}
}

func TestBody(t *testing.T) {
	c, got := record(t, 200, map[string]string{"ETag": `"7"`, "Content-Type": "text/markdown; charset=utf-8"}, "no newline")
	b, v, err := c.Body(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, seen{Method: "GET", URI: "/thoughts/42/body"})
	if string(b) != "no newline" || v != 7 {
		t.Errorf("got %q %d", b, v)
	}
}

func TestPutBody(t *testing.T) {
	c, got := record(t, 204, map[string]string{"ETag": `"8"`}, "")
	v, err := c.PutBody(ctx, 42, api.Precondition{Force: true}, []byte("x\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, seen{Method: "PUT", URI: "/thoughts/42/body", IfMatch: "*", ContentType: "text/markdown; charset=utf-8", Body: "x\n\n"})
	if v != 8 {
		t.Errorf("version %d", v)
	}
}

func TestPutBodyEmpty(t *testing.T) {
	c, got := record(t, 204, map[string]string{"ETag": `"8"`}, "")
	if _, err := c.PutBody(ctx, 42, api.Precondition{Version: 7}, nil); err != nil {
		t.Fatal(err)
	}
	check(t, got, seen{Method: "PUT", URI: "/thoughts/42/body", IfMatch: `"7"`, ContentType: "text/markdown; charset=utf-8"})
}

func TestTagsAndAttributes(t *testing.T) {
	cases := []struct {
		name string
		call func(client.Client) (int64, error)
		want seen
	}{
		{"put tag", func(c client.Client) (int64, error) {
			return c.PutTag(ctx, 42, api.Precondition{}, "a/b c")
		}, seen{Method: "PUT", URI: "/thoughts/42/tags/a%2Fb%20c"}},
		{"delete tag", func(c client.Client) (int64, error) {
			return c.DeleteTag(ctx, 42, api.Precondition{Version: 2}, "rust")
		}, seen{Method: "DELETE", URI: "/thoughts/42/tags/rust", IfMatch: `"2"`}},
		{"put attribute", func(c client.Client) (int64, error) {
			return c.PutAttribute(ctx, 42, api.Precondition{}, "effort", "Very small")
		}, seen{Method: "PUT", URI: "/thoughts/42/attributes/effort", ContentType: "text/plain; charset=utf-8", Body: "Very small"}},
		{"delete attribute", func(c client.Client) (int64, error) {
			return c.DeleteAttribute(ctx, 42, api.Precondition{}, "k?")
		}, seen{Method: "DELETE", URI: "/thoughts/42/attributes/k%3F"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, got := record(t, 204, map[string]string{"ETag": `"5"`}, "")
			v, err := tc.call(c)
			if err != nil {
				t.Fatal(err)
			}
			check(t, got, tc.want)
			if v != 5 {
				t.Errorf("version %d", v)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	c, got := record(t, 204, nil, "")
	if err := c.Delete(ctx, 42, api.Precondition{Version: 9}); err != nil {
		t.Fatal(err)
	}
	check(t, got, seen{Method: "DELETE", URI: "/thoughts/42", IfMatch: `"9"`})
}

func TestVocabulary(t *testing.T) {
	c, got := record(t, 200, nil, `[{"tag":"rust","count":2}]`)
	tags, err := c.Tags(ctx)
	if err != nil || !reflect.DeepEqual(tags, []api.TagCount{{Tag: "rust", Count: 2}}) {
		t.Errorf("got %+v %v", tags, err)
	}
	check(t, got, seen{Method: "GET", URI: "/tags"})

	c, got = record(t, 200, nil, `[{"key":"effort","count":1}]`)
	keys, err := c.Attributes(ctx)
	if err != nil || !reflect.DeepEqual(keys, []api.KeyCount{{Key: "effort", Count: 1}}) {
		t.Errorf("got %+v %v", keys, err)
	}
	check(t, got, seen{Method: "GET", URI: "/attributes"})

	c, got = record(t, 200, nil, `[{"value":"small","count":3}]`)
	vals, err := c.AttributeValues(ctx, "a b")
	if err != nil || !reflect.DeepEqual(vals, []api.ValueCount{{Value: "small", Count: 3}}) {
		t.Errorf("got %+v %v", vals, err)
	}
	check(t, got, seen{Method: "GET", URI: "/attributes/a%20b"})
}

func TestProblem(t *testing.T) {
	raw := `{"title":"Precondition Failed","status":412,"detail":"stale","current_version":9}`
	c, _ := record(t, 412, map[string]string{"Content-Type": "application/problem+json"}, raw)
	_, err := c.PutBody(ctx, 42, api.Precondition{Version: 7}, []byte("x"))
	var pe *client.ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("got %T %v", err, err)
	}
	if string(pe.Raw) != raw || pe.Problem.CurrentVersion != 9 || pe.Problem.Status != 412 {
		t.Errorf("got %+v", pe)
	}
	if pe.Error() != "Precondition Failed: stale" {
		t.Errorf("Error() = %q", pe.Error())
	}
}

func TestProblemWithoutDocument(t *testing.T) {
	c, _ := record(t, 404, nil, "")
	_, err := c.Get(ctx, 1)
	var pe *client.ProblemError
	if !errors.As(err, &pe) || pe.Problem.Status != 404 || pe.Problem.Title != "Not Found" {
		t.Fatalf("got %T %+v", err, err)
	}
}
