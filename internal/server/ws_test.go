package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/HendersonT/quick-vote/internal/server"
	"github.com/HendersonT/quick-vote/internal/store"
)

// newWSTestServer builds a server backed by a real httptest.Server, since
// gorilla's websocket.Dialer needs to dial an actual network listener.
func newWSTestServer(t *testing.T) (*server.Server, *httptest.Server) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s := server.New(st, nil)
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return s, ts
}

func dialWS(t *testing.T, ts *httptest.Server, slug, token string) *websocket.Conn {
	t.Helper()
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	u.Scheme = "ws"
	u.Path = "/api/votes/" + slug + "/ws"
	if token != "" {
		q := u.Query()
		q.Set("token", token)
		u.RawQuery = q.Encode()
	}
	conn, resp, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("dial ws: unexpected status %d", resp.StatusCode)
	}
	return conn
}

func readSnapshot(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode snapshot %q: %v", data, err)
	}
	return out
}

func TestWebSocketInitialSnapshots(t *testing.T) {
	s, ts := newWSTestServer(t)

	slug, _, sessionToken, _ := createVote(t, s, nil)

	aliceConn := dialWS(t, ts, slug, sessionToken)
	spectatorConn := dialWS(t, ts, slug, "")

	aliceSnap := readSnapshot(t, aliceConn)
	if aliceSnap["you"] == nil {
		t.Fatalf("alice snapshot: expected you to be set, got %v", aliceSnap["you"])
	}

	spectatorSnap := readSnapshot(t, spectatorConn)
	if spectatorSnap["you"] != nil {
		t.Fatalf("spectator snapshot: expected you to be nil, got %v", spectatorSnap["you"])
	}
}

// TestWebSocketConcurrentBroadcastNoPanic hammers a room with many concurrent
// broadcasts while a client stops reading (filling its send buffer so it gets
// dropped). Before the teardown was single-sourced through c.done, this
// triggered "close of closed channel" / "send on closed channel" panics in the
// unrecovered pump/broadcast goroutines and crashed the whole process.
func TestWebSocketConcurrentBroadcastNoPanic(t *testing.T) {
	s, ts := newWSTestServer(t)

	slug, _, sessionToken, _ := createVote(t, s, nil)

	// A client that connects but never reads: its send buffer fills, so
	// pushSnapshot takes the drop-and-close path.
	stuck := dialWS(t, ts, slug, sessionToken)
	defer stuck.Close()

	// Fan out many concurrent broadcasts for the same slug, mirroring what
	// happens when multiple HTTP mutations hit the same room at once.
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				s.Broadcast(slug)
			}
		}()
	}
	wg.Wait()

	// If any pump/broadcast goroutine had panicked on a closed channel, the
	// test binary would have already crashed. Reaching here means teardown is
	// panic-free; give lingering goroutines a moment to settle.
	time.Sleep(50 * time.Millisecond)
}

func TestWebSocketBroadcastsOnChange(t *testing.T) {
	s, ts := newWSTestServer(t)

	slug, _, sessionToken, _ := createVote(t, s, nil)

	aliceConn := dialWS(t, ts, slug, sessionToken)
	spectatorConn := dialWS(t, ts, slug, "")

	// Drain the initial snapshots sent on connect.
	readSnapshot(t, aliceConn)
	readSnapshot(t, spectatorConn)

	rec, _ := suggest(t, s, slug, sessionToken, "Catan")
	if rec.Code != http.StatusOK {
		t.Fatalf("suggest: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	for _, conn := range []*websocket.Conn{aliceConn, spectatorConn} {
		found := false
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			snap := readSnapshot(t, conn)
			opts, _ := snap["options"].([]any)
			for _, o := range opts {
				om, _ := o.(map[string]any)
				if title, _ := om["title"].(string); strings.EqualFold(title, "Catan") {
					found = true
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Fatalf("expected to receive a snapshot containing the new option within 2s")
		}
	}
}
