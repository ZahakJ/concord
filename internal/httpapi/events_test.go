package httpapi

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A stream opens the moment it is dialled, before anything has happened to
// report: the page learns it is connected again from that, and fetches what
// it missed. Held until the first event, a redial into a quiet node stayed
// "connecting" indefinitely and the page never caught up.
func TestEventStreamAnswersBeforeAnyEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := New(ctx, false, WithAuthToken("tok"))
	ts := httptest.NewServer(http.HandlerFunc(s.HandleEvents))
	defer ts.Close()

	rctx, rcancel := context.WithTimeout(ctx, 3*time.Second)
	defer rcancel()
	req, _ := http.NewRequestWithContext(rctx, http.MethodGet, ts.URL+"/events?token=tok", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("no answer before the first event: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status %d, content type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || line[0] != ':' {
		t.Fatalf("first line %q (%v), want an SSE comment", line, err)
	}
}
