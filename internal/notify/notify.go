// Package notify is a tiny in-process pub/sub used to wake long-poll readers
// (the MCP wait_for_message tool and the HTTP /wait endpoint) the moment a
// message lands in an inbox, instead of busy-polling the database.
//
// It is intentionally best-effort: subscribers that miss a signal (e.g. they
// subscribed a microsecond too late) fall back to a database poll, so a dropped
// notification only costs latency, never correctness.
package notify

import "sync"

// Hub fans out per-inbox arrival signals to any waiting subscribers.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan string]struct{} // inbox -> set of subscriber channels
}

// NewHub returns a ready Hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[chan string]struct{})}
}

// Subscribe registers interest in an inbox. The returned channel receives the
// message ID of the next arrival; cancel must be called to release resources.
// The channel is buffered (size 1) so Publish never blocks on a slow reader.
func (h *Hub) Subscribe(inbox string) (ch <-chan string, cancel func()) {
	c := make(chan string, 1)
	h.mu.Lock()
	set, ok := h.subs[inbox]
	if !ok {
		set = make(map[chan string]struct{})
		h.subs[inbox] = set
	}
	set[c] = struct{}{}
	h.mu.Unlock()

	return c, func() {
		h.mu.Lock()
		if set, ok := h.subs[inbox]; ok {
			delete(set, c)
			if len(set) == 0 {
				delete(h.subs, inbox)
			}
		}
		h.mu.Unlock()
	}
}

// Publish signals every current subscriber of inbox that message msgID arrived.
// Non-blocking: a subscriber whose buffer is full simply keeps its earlier
// (still-unread) signal.
func (h *Hub) Publish(inbox, msgID string) {
	h.mu.Lock()
	set := h.subs[inbox]
	chans := make([]chan string, 0, len(set))
	for c := range set {
		chans = append(chans, c)
	}
	h.mu.Unlock()

	for _, c := range chans {
		select {
		case c <- msgID:
		default:
		}
	}
}
