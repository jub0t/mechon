package panel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// botStream is a Server-Sent Events feed for one bot: an initial snapshot, then state changes,
// stats every few seconds, log lines as they happen and deploy progress.
func (s *Server) botStream(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadBot(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, fmt.Errorf("streaming unsupported"))
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // let nginx pass events through unbuffered
	w.WriteHeader(http.StatusOK)

	events, buffered, cancel := s.hub.watch(r.Context(), row.Bot.NodeID, row.Bot.ID)
	defer cancel()

	send := func(ev streamEvent) error {
		b, err := json.Marshal(ev.Data)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, b); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	_, online := s.hub.online(row.Bot.NodeID)
	if send(streamEvent{"hello", map[string]any{"bot": s.getBotView(row), "nodeOnline": online}}) != nil {
		return
	}
	if len(buffered) > 0 && send(streamEvent{"logs.reset", buffered}) != nil {
		return
	}

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-events:
			if send(ev) != nil {
				return
			}
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
