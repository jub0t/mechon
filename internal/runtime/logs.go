//go:build linux

package runtime

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Logs streams the bot container's output. Each line carries Docker's timestamp; lines at or
// before since are dropped, so callers can resume a stream without duplicates.
func (d *Docker) Logs(ctx context.Context, botID string, tail int, since time.Time, follow bool) (<-chan LogEntry, error) {
	if !validBotID(botID) {
		return nil, fmt.Errorf("invalid bot id %q", botID)
	}
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"}}
	if tail > 0 {
		q.Set("tail", strconv.Itoa(tail))
	} else if since.IsZero() {
		q.Set("tail", "0")
	}
	if !since.IsZero() {
		q.Set("since", fmt.Sprintf("%d.%09d", since.Unix(), since.Nanosecond()))
	}
	if follow {
		q.Set("follow", "1")
	}
	rc, err := d.api.containerLogs(ctx, containerName(botID), q)
	if err != nil {
		return nil, err
	}
	ch := make(chan LogEntry, 256)
	go func() {
		defer close(ch)
		defer rc.Close()
		_ = demuxLines(rc, func(stream string, line []byte) {
			l := LogEntry{Stream: stream}
			ts, text, ok := bytes.Cut(line, []byte(" "))
			t, err := time.Parse(time.RFC3339Nano, string(ts))
			if ok && err == nil {
				if !since.IsZero() && !t.After(since) {
					return
				}
				l.Time, l.Text = t, string(text)
			} else {
				l.Time, l.Text = time.Now(), string(line)
			}
			select {
			case ch <- l:
			case <-ctx.Done():
			}
		})
	}()
	return ch, nil
}
