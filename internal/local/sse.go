package local

import "sync"

type Hub struct {
	mu   sync.Mutex
	subs map[chan []byte]bool
}

func NewHub() *Hub { return &Hub{subs: map[chan []byte]bool{}} }

func (h *Hub) Subscribe() chan []byte {
	ch := make(chan []byte, 8)
	h.mu.Lock()
	h.subs[ch] = true
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

func (h *Hub) BroadcastRaw(frame []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- frame:
		default:
			// A slow tab misses this frame; the next state still arrives.
		}
	}
}

func (h *Hub) BroadcastFrame(event string, data []byte) {
	frame := make([]byte, 0, len(event)+len(data)+16)
	frame = append(frame, "event: "...)
	frame = append(frame, event...)
	frame = append(frame, "\ndata: "...)
	frame = append(frame, data...)
	frame = append(frame, "\n\n"...)
	h.BroadcastRaw(frame)
}
