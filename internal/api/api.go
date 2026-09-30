// Package api holds the wire types of the HTTP API (spec §5), shared by the
// server, the store, the client, the CLI and the Review TUI.
package api

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// APIVersion is the api number reported by GET /. A client that sees another
// number exits 8.
const APIVersion = 1

// Service is the body of GET /.
type Service struct {
	Service string `json:"service"` // always "thoughts"
	API     int    `json:"api"`
}

// Status values (spec §2). Status is stored and sent as the attribute
// "status".
const (
	StatusKey     = "status"
	StatusRaw     = "raw"
	StatusActive  = "active"
	StatusDone    = "done"
	StatusDropped = "dropped"
)

// Statuses lists the Status values in their canonical order.
var Statuses = []string{StatusRaw, StatusActive, StatusDone, StatusDropped}

// ReservedKeys can never be attribute keys.
var ReservedKeys = []string{"id", "title", "body", "tag", "has", "created", "updated"}

// MaxBody is the largest body the server accepts, in bytes (413 over).
const MaxBody = 10 << 20

// Time is an instant that marshals as RFC 3339 UTC with millisecond precision
// (2026-09-22T14:03:07.412Z).
type Time struct{ time.Time }

// TimeLayout is the wire and storage layout of every timestamp.
const TimeLayout = "2006-01-02T15:04:05.000Z"

func (t Time) String() string { return t.UTC().Format(TimeLayout) }

func (t Time) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(t.String())), nil
}

func (t *Time) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return err
	}
	t.Time = v.UTC()
	return nil
}

// Meta is a thought without its body: what lists return.
type Meta struct {
	ID         int64             `json:"id"`
	Title      string            `json:"title"`
	Tags       []string          `json:"tags"`       // sorted, never null
	Attributes map[string]string `json:"attributes"` // always carries "status"
	Version    int64             `json:"version"`
	CreatedAt  Time              `json:"created_at"`
	UpdatedAt  Time              `json:"updated_at"`
	// Snippet is a plain-text excerpt, present only in list results whose
	// filter has a ranked text term.
	Snippet string `json:"snippet,omitempty"`
}

// Thought is the full envelope.
type Thought struct {
	Meta
	Body string `json:"body"`
}

// PatchedThought is the envelope a PATCH returns: the body is omitted unless the
// request sent one.
type PatchedThought struct {
	Meta
	Body *string `json:"body,omitempty"`
}

// List is the body of GET /thoughts.
type List struct {
	Thoughts []Meta `json:"thoughts"` // never null
	Total    int    `json:"total"`    // matches before limit/offset
}

// Change is one event of GET /events: the thought was created or written, and
// Version is its Version after the write, or it was deleted.
type Change struct {
	ID      int64 `json:"id"`
	Version int64 `json:"version,omitempty"` // 0 when Deleted
	Deleted bool  `json:"deleted,omitempty"`
}

// CreateRequest is the body of POST /thoughts.
type CreateRequest struct {
	Title      string            `json:"title"`
	Body       string            `json:"body,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Patch is the body of PATCH /thoughts/{id}, an RFC 7396 merge patch: a nil
// field is untouched, Tags replaces the whole set, and an attribute whose
// value is nil is removed.
type Patch struct {
	Title      *string            `json:"title,omitempty"`
	Body       *string            `json:"body,omitempty"`
	Tags       *[]string          `json:"tags,omitempty"`
	Attributes map[string]*string `json:"attributes,omitempty"`
}

// TagCount, KeyCount and ValueCount are the vocabulary endpoints' items.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

type KeyCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type ValueCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// Problem is an RFC 9457 application/problem+json document.
type Problem struct {
	Type   string `json:"type,omitempty"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
	// Position is the 1-based rune position of a filter parse error (400).
	Position int `json:"position,omitempty"`
	// CurrentVersion is the thought's Version on a stale If-Match (412).
	CurrentVersion int64 `json:"current_version,omitempty"`
}

// Precondition is what a writer says about the Version it last saw. The zero
// value sends no If-Match.
type Precondition struct {
	Version int64 // sent as If-Match: "<n>" when > 0
	Force   bool  // If-Match: *
}

// IsZero reports whether no If-Match is sent.
func (p Precondition) IsZero() bool { return !p.Force && p.Version == 0 }

// Header renders the If-Match value, "" for the zero value.
func (p Precondition) Header() string {
	switch {
	case p.Force:
		return "*"
	case p.Version > 0:
		return ETag(p.Version)
	}
	return ""
}

// ETag renders a Version as a strong entity tag.
func ETag(version int64) string { return `"` + strconv.FormatInt(version, 10) + `"` }

// ParseETag reads a Version from an ETag or If-Match value; ok is false for
// anything but a quoted (or bare) decimal.
func ParseETag(s string) (version int64, ok bool) {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	v, err := strconv.ParseInt(s, 10, 64)
	return v, err == nil && v > 0
}

// ParseID reads a thought id: a plain decimal number, at least 1, that fits an
// int64, written without leading zeros. Signs, spaces and anything else are
// rejected.
func ParseID(s string) (int64, bool) {
	if s == "" || s[0] == '0' {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil && id >= 1
}

// LowerLabel lowercases a tag, an attribute key or a Status value the way the
// server does: ASCII letters only. Anything else is left as written, and a
// label that is not ASCII fails validation anyway.
func LowerLabel(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, s)
}
