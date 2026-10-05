package agent

import (
	"context"
	"sync"
	"time"

	"github.com/jub0t/mechon/internal/proto"
)

const (
	logFlushEvery  = 100 * time.Millisecond
	logBatchMax    = 500   // lines per log frame
	logBufferMax   = 10000 // lines buffered per bot before the oldest are dropped
	logTailMax     = 5000
	logTailDefault = 200
)

// logHub holds the panel's log subscriptions for the current connection (one per bot; the
// panel fans out to browsers). Lines are batched and flushed every logFlushEvery.
type logHub struct {
	a    *Agent
	mu   sync.Mutex
	subs map[string]*logSub
}

type logSub struct {
	cancel context.CancelFunc
	lines  []proto.LogLine
}

func newLogHub(a *Agent) *logHub { return &logHub{a: a, subs: map[string]*logSub{}} }

// subscribe replies to the request with the last Tail lines, then follows the bot's output
// (across container restarts and recreations) until unsubscribed or disconnected.
func (h *logHub) subscribe(reqID string, req proto.LogsSubscribe) {
	tail := req.Tail
	if tail <= 0 {
		tail = logTailDefault
	}
	tail = min(tail, logTailMax)

	ctx, cancel := context.WithCancel(h.a.ctx)
	h.mu.Lock()
	if old := h.subs[req.BotID]; old != nil {
		old.cancel()
	}
	sub := &logSub{cancel: cancel}
	h.subs[req.BotID] = sub
	h.mu.Unlock()

	batch := proto.LogBatch{BotID: req.BotID, Lines: []proto.LogLine{}}
	var since time.Time
	tctx, tcancel := context.WithTimeout(ctx, 10*time.Second)
	ch, err := h.a.rt.Logs(tctx, req.BotID, tail, time.Time{}, false)
	if err == nil {
		for l := range ch {
			batch.Lines = append(batch.Lines, l.Line())
			since = l.Time
		}
	}
	tcancel()
	if since.IsZero() {
		since = time.Now()
	}
	// No container yet is not an error: the subscription stays open and picks up the first one.
	h.a.reply(reqID, nil, batch)

	go func() {
		for ctx.Err() == nil {
			ch, err := h.a.rt.Logs(ctx, req.BotID, 0, since, true)
			if err == nil {
				for l := range ch {
					since = l.Time
					h.add(req.BotID, sub, l.Line())
				}
			}
			// Stream ended: container stopped, restarted or replaced. Look again shortly.
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}()
}

func (h *logHub) add(botID string, sub *logSub, l proto.LogLine) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[botID] != sub {
		return
	}
	sub.lines = append(sub.lines, l)
	if n := len(sub.lines); n > logBufferMax {
		sub.lines = append(sub.lines[:0:0], sub.lines[n-logBufferMax:]...)
	}
}

// system injects a lifecycle line ("starting", "crashed with exit code 1", …) into the bot's
// open subscription, if any.
func (h *logHub) system(botID, text string) {
	h.mu.Lock()
	sub := h.subs[botID]
	h.mu.Unlock()
	if sub != nil {
		h.add(botID, sub, proto.LogLine{T: time.Now().UnixMilli(), Stream: "system", Text: text})
	}
}

func (h *logHub) unsubscribe(botID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if sub := h.subs[botID]; sub != nil {
		sub.cancel()
		delete(h.subs, botID)
	}
}

func (h *logHub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, sub := range h.subs {
		sub.cancel()
		delete(h.subs, id)
	}
}

func (h *logHub) flushLoop(ctx context.Context) {
	t := time.NewTicker(logFlushEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		var out []proto.LogBatch
		h.mu.Lock()
		for id, sub := range h.subs {
			for len(sub.lines) > 0 {
				n := min(len(sub.lines), logBatchMax)
				out = append(out, proto.LogBatch{BotID: id, Lines: sub.lines[:n:n]})
				sub.lines = sub.lines[n:]
			}
			sub.lines = nil
		}
		h.mu.Unlock()
		for _, b := range out {
			h.a.send(proto.TypeLog, b)
		}
	}
}
