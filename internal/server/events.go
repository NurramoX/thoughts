package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/NurramoX/thoughts/internal/api"
)

// streamBuffer is how many Changes a stream may fall behind by before it is
// cut off.
const streamBuffer = 64

// hub fans each Change out to the open event streams. Publishing never
// waits: a stream whose buffer is full is closed instead, and its client
// reconnects and relists.
type hub struct {
	mu     sync.Mutex
	subs   map[chan api.Change]struct{}
	closed bool
}

func newHub() *hub { return &hub{subs: map[chan api.Change]struct{}{}} }

// subscribe opens a stream; ok is false once the hub is closed.
func (b *hub) subscribe() (ch chan api.Change, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, false
	}
	ch = make(chan api.Change, streamBuffer)
	b.subs[ch] = struct{}{}
	return ch, true
}

// unsubscribe closes ch, unless the hub already has.
func (b *hub) unsubscribe(ch chan api.Change) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.drop(ch)
}

func (b *hub) publish(c api.Change) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- c:
		default:
			b.drop(ch)
		}
	}
}

// close ends every stream and refuses new ones.
func (b *hub) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for ch := range b.subs {
		b.drop(ch)
	}
}

// drop closes ch if it is still open; b.mu is held.
func (b *hub) drop(ch chan api.Change) {
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
}

// changed tells the event streams about a successful write.
func (h *Handler) changed(id, version int64) {
	h.changes.publish(api.Change{ID: id, Version: version})
}

// events streams every Change, one server-sent event each, until the client
// goes away, falls behind, or the server shuts down.
func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	ch, ok := h.changes.subscribe()
	if !ok {
		fail(w, http.StatusServiceUnavailable, "shutting down")
		return
	}
	defer h.changes.unsubscribe(ch)
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if rc.Flush() != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case c, open := <-ch:
			if !open {
				return
			}
			data, err := json.Marshal(c)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}
