package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/NurramoX/thoughts/internal/api"
)

func (c *httpClient) Changes(ctx context.Context) (ChangeStream, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/events", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		var ue *UnreachableError
		if errors.As(err, &ue) {
			return nil, ue
		}
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		return nil, problem(resp.StatusCode, data)
	}
	return &changeStream{body: resp.Body, r: bufio.NewReader(resp.Body)}, nil
}

// changeStream reads server-sent events whose data is one api.Change.
type changeStream struct {
	body io.ReadCloser
	r    *bufio.Reader
}

func (s *changeStream) Next() (api.Change, error) {
	var data strings.Builder
	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			return api.Change{}, err
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		switch {
		case line == "" && data.Len() > 0:
			var c api.Change
			if err := json.Unmarshal([]byte(data.String()), &c); err != nil {
				return api.Change{}, fmt.Errorf("malformed event %q: %w", data.String(), err)
			}
			return c, nil
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
		// Blank lines between events, comments and other fields are skipped.
	}
}

func (s *changeStream) Close() error { return s.body.Close() }
