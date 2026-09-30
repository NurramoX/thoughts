package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/NurramoX/thoughts/internal/api"
)

// New returns a Client that dials the Unix socket at sock. It does not dial
// until the first call.
func New(sock string) Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			conn, err := d.DialContext(ctx, "unix", sock)
			if err != nil {
				return nil, &UnreachableError{Err: err}
			}
			return conn, nil
		},
	}
	return &httpClient{hc: &http.Client{Transport: tr}}
}

// base is the URL every request is relative to; the host is never resolved.
const base = "http://thoughts"

type httpClient struct{ hc *http.Client }

// request describes one call.
type request struct {
	method      string
	path        string // already escaped
	header      http.Header
	body        []byte
	contentType string
}

// do sends r and returns the response with its body read. Any status >= 300
// other than 304 is a *ProblemError.
func (c *httpClient) do(ctx context.Context, r request) (*http.Response, []byte, error) {
	var body io.Reader
	if r.body != nil {
		body = bytes.NewReader(r.body)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, base+r.path, body)
	if err != nil {
		return nil, nil, err
	}
	for k, vs := range r.header {
		req.Header[k] = vs
	}
	if r.contentType != "" {
		req.Header.Set("Content-Type", r.contentType)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		var ue *UnreachableError
		if errors.As(err, &ue) {
			return nil, nil, ue
		}
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotModified {
		return nil, nil, problem(resp.StatusCode, data)
	}
	return resp, data, nil
}

// problem builds the error for a non-2xx answer. A body that is not
// problem+json still yields a usable title and status.
func problem(status int, raw []byte) *ProblemError {
	e := &ProblemError{Raw: raw}
	_ = json.Unmarshal(raw, &e.Problem)
	if e.Problem.Status == 0 {
		e.Problem.Status = status
	}
	if e.Problem.Title == "" {
		e.Problem.Title = http.StatusText(status)
	}
	return e
}

// getJSON GETs path and decodes the answer into v.
func (c *httpClient) getJSON(ctx context.Context, path string, v any) error {
	_, data, err := c.do(ctx, request{method: http.MethodGet, path: path})
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// thoughtPath is /thoughts/{id} followed by the escaped segments.
func thoughtPath(id int64, segments ...string) string {
	p := "/thoughts/" + strconv.FormatInt(id, 10)
	for _, s := range segments {
		p += "/" + url.PathEscape(s)
	}
	return p
}

// ifMatch is the header for pre, none for the zero Precondition.
func ifMatch(pre api.Precondition) http.Header {
	if pre.IsZero() {
		return nil
	}
	return http.Header{"If-Match": {pre.Header()}}
}

// version reads the Version from the response's ETag, 0 when absent.
func version(resp *http.Response) int64 {
	v, _ := api.ParseETag(resp.Header.Get("ETag"))
	return v
}

func (c *httpClient) sendJSON(ctx context.Context, method, path string, h http.Header, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	_, data, err := c.do(ctx, request{method: method, path: path, header: h, body: body, contentType: "application/json"})
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// write sends a request whose answer carries only the new Version.
func (c *httpClient) write(ctx context.Context, r request) (int64, error) {
	resp, _, err := c.do(ctx, r)
	if err != nil {
		return 0, err
	}
	return version(resp), nil
}

func (c *httpClient) Ping(ctx context.Context) (api.Service, error) {
	var s api.Service
	err := c.getJSON(ctx, "/", &s)
	return s, err
}

func (c *httpClient) Create(ctx context.Context, req api.CreateRequest) (api.Thought, error) {
	var thought api.Thought
	err := c.sendJSON(ctx, http.MethodPost, "/thoughts", nil, req, &thought)
	return thought, err
}

func (c *httpClient) Get(ctx context.Context, id int64) (api.Thought, error) {
	var thought api.Thought
	err := c.getJSON(ctx, thoughtPath(id), &thought)
	return thought, err
}

func (c *httpClient) Revalidate(ctx context.Context, id int64, v int64) (api.Thought, bool, error) {
	h := http.Header{"If-None-Match": {api.ETag(v)}}
	resp, data, err := c.do(ctx, request{method: http.MethodGet, path: thoughtPath(id), header: h})
	if err != nil {
		return api.Thought{}, false, err
	}
	if resp.StatusCode == http.StatusNotModified {
		return api.Thought{}, true, nil
	}
	var thought api.Thought
	err = json.Unmarshal(data, &thought)
	return thought, false, err
}

func (c *httpClient) List(ctx context.Context, p ListParams) (api.List, error) {
	q := url.Values{}
	set := func(k, v string) {
		if v != "" {
			q.Set(k, v)
		}
	}
	set("filter", p.Filter)
	set("sort", p.Sort)
	set("order", p.Order)
	if p.Limit > 0 {
		set("limit", strconv.Itoa(p.Limit))
	}
	if p.Offset > 0 {
		set("offset", strconv.Itoa(p.Offset))
	}
	path := "/thoughts"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var l api.List
	err := c.getJSON(ctx, path, &l)
	return l, err
}

func (c *httpClient) Patch(ctx context.Context, id int64, pre api.Precondition, p api.Patch) (api.PatchedThought, error) {
	var thought api.PatchedThought
	err := c.sendJSON(ctx, http.MethodPatch, thoughtPath(id), ifMatch(pre), p, &thought)
	return thought, err
}

func (c *httpClient) Body(ctx context.Context, id int64) ([]byte, int64, error) {
	resp, data, err := c.do(ctx, request{method: http.MethodGet, path: thoughtPath(id, "body")})
	if err != nil {
		return nil, 0, err
	}
	return data, version(resp), nil
}

func (c *httpClient) PutBody(ctx context.Context, id int64, pre api.Precondition, body []byte) (int64, error) {
	return c.write(ctx, request{
		method: http.MethodPut, path: thoughtPath(id, "body"), header: ifMatch(pre),
		body: body, contentType: "text/markdown; charset=utf-8",
	})
}

func (c *httpClient) PutTag(ctx context.Context, id int64, pre api.Precondition, tag string) (int64, error) {
	return c.write(ctx, request{method: http.MethodPut, path: thoughtPath(id, "tags", tag), header: ifMatch(pre)})
}

func (c *httpClient) DeleteTag(ctx context.Context, id int64, pre api.Precondition, tag string) (int64, error) {
	return c.write(ctx, request{method: http.MethodDelete, path: thoughtPath(id, "tags", tag), header: ifMatch(pre)})
}

func (c *httpClient) PutAttribute(ctx context.Context, id int64, pre api.Precondition, key, value string) (int64, error) {
	return c.write(ctx, request{
		method: http.MethodPut, path: thoughtPath(id, "attributes", key), header: ifMatch(pre),
		body: []byte(value), contentType: "text/plain; charset=utf-8",
	})
}

func (c *httpClient) DeleteAttribute(ctx context.Context, id int64, pre api.Precondition, key string) (int64, error) {
	return c.write(ctx, request{method: http.MethodDelete, path: thoughtPath(id, "attributes", key), header: ifMatch(pre)})
}

func (c *httpClient) Delete(ctx context.Context, id int64, pre api.Precondition) error {
	_, err := c.write(ctx, request{method: http.MethodDelete, path: thoughtPath(id), header: ifMatch(pre)})
	return err
}

func (c *httpClient) Tags(ctx context.Context) ([]api.TagCount, error) {
	var v []api.TagCount
	err := c.getJSON(ctx, "/tags", &v)
	return v, err
}

func (c *httpClient) Attributes(ctx context.Context) ([]api.KeyCount, error) {
	var v []api.KeyCount
	err := c.getJSON(ctx, "/attributes", &v)
	return v, err
}

func (c *httpClient) AttributeValues(ctx context.Context, key string) ([]api.ValueCount, error) {
	var v []api.ValueCount
	err := c.getJSON(ctx, "/attributes/"+url.PathEscape(key), &v)
	return v, err
}
