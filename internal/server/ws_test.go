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

	"github.com/HendersonT/quick-vote/internal/clock"
	"github.com/HendersonT/quick-vote/internal/server"
	"github.com/HendersonT/quick-vote/internal/store"
)

// newWSTestServer builds a server backed by a real httptest.Server, since
// gorilla's websocket.Dialer needs to dial an actual network listener. The
// WS auth timeout is shortened so no-auth tests finish quickly.
func newWSTestServer(t *testing.T) (*server.Server, *httptest.Server) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s := server.NewWithConfig(st, nil, server.Config{WSAuthTimeout: 200 * time.Millisecond})
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return s, ts
}

// dialRaw opens the vote's WebSocket without sending the auth message.
func dialRaw(t *testing.T, ts *httptest.Server, slug string) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/votes/" + slug + "/ws"
	conn, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("dial ws: unexpected status %d", resp.StatusCode)
	}
	return conn
}

// dialWS opens the vote's WebSocket and authenticates with token ("" for a
// spectator) as the first message, as the web client does.
func dialWS(t *testing.T, ts *httptest.Server, slug, token string) *websocket.Conn {
	t.Helper()
	conn := dialRaw(t, ts, slug)
	if err := conn.WriteJSON(map[string]string{"type": "auth", "token": token}); err != nil {
		t.Fatalf("send auth: %v", err)
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
	// Reading the initial snapshot proves the connection has authenticated
	// and joined the hub, so the broadcasts below actually target it.
	readSnapshot(t, stuck)

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

func TestWebSocketNoAuthMessageIsClosed(t *testing.T) {
	s, ts := newWSTestServer(t)
	slug, _, _, _ := createVote(t, s, nil)
	conn := dialRaw(t, ts, slug)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := conn.ReadMessage()
	if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
		t.Fatalf("want policy-violation close without auth, got %v", err)
	}
}

func TestWebSocketGarbageFirstMessageIsClosed(t *testing.T) {
	s, ts := newWSTestServer(t)
	slug, _, _, _ := createVote(t, s, nil)
	for _, msg := range []string{`{"type":"hello"}`, `not json`} {
		conn := dialRaw(t, ts, slug)
		_ = conn.WriteMessage(websocket.TextMessage, []byte(msg))
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, err := conn.ReadMessage()
		if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
			t.Fatalf("first message %q: want policy-violation close, got %v", msg, err)
		}
	}
}

func TestWebSocketQueryTokenIgnored(t *testing.T) {
	s, ts := newWSTestServer(t)
	slug, _, tok, _ := createVote(t, s, nil)
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/votes/" + slug + "/ws?token=" + url.QueryEscape(tok)
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]string{"type": "auth"})
	if snap := readSnapshot(t, conn); snap["you"] != nil {
		t.Fatal("a ?token= query parameter must not authenticate")
	}
}

// TestWebSocketMessagesAfterAuthIgnored checks that once authenticated, later
// client messages (including a second auth attempt) neither change the
// connection's identity nor kill it.
func TestWebSocketMessagesAfterAuthIgnored(t *testing.T) {
	s, ts := newWSTestServer(t)
	slug, _, tok, _ := createVote(t, s, nil)
	conn := dialWS(t, ts, slug, "")
	if snap := readSnapshot(t, conn); snap["you"] != nil {
		t.Fatal("spectator snapshot must not carry you")
	}
	if err := conn.WriteJSON(map[string]string{"type": "auth", "token": tok}); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("garbage")); err != nil {
		t.Fatal(err)
	}
	// The server reads frames in order and answers a ping from its read
	// loop, so its pong proves both messages above were already read (and
	// ignored). Only then broadcast: the snapshot that follows shows the
	// connection's identity after those messages, and that it survived.
	conn.SetPongHandler(func(string) error {
		s.Broadcast(slug)
		return nil
	})
	if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if snap := readSnapshot(t, conn); snap["you"] != nil {
		t.Fatal("a second auth message must not upgrade a spectator")
	}
}

// TestWebSocketClosedWhenVoteGoneAtAuth: a vote pruned between the upgrade
// and the auth message has nothing to show; the connection must be closed
// rather than left open and silent.
func TestWebSocketClosedWhenVoteGoneAtAuth(t *testing.T) {
	s, ts := newWSTestServer(t)
	slug, _, _, _ := createVote(t, s, nil)
	conn := dialRaw(t, ts, slug)
	if n, err := s.PruneExpired(-time.Hour); err != nil || n != 1 {
		t.Fatalf("prune: n=%d err=%v", n, err)
	}
	if err := conn.WriteJSON(map[string]string{"type": "auth", "token": ""}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := conn.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		t.Fatalf("want a normal close for a vote that is gone, got %v", err)
	}
}

// TestPruneRefreshesUnlinkedSource: pruning a follow-up vote unlinks its
// still-active source, and sockets open on the source must hear about it —
// otherwise they keep a "moved on" banner pointing at a vote that is gone.
func TestPruneRefreshesUnlinkedSource(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	s := server.NewWithConfig(st, nil, server.Config{Clock: fc, WSAuthTimeout: 200 * time.Millisecond})
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)

	slug, ct, aliceTok, _ := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	if rec, _ := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/next", map[string]any{"title": "R2"}, aliceTok, ct); rec.Code != http.StatusCreated {
		t.Fatalf("next: %d", rec.Code)
	}
	fc.Advance(100 * 24 * time.Hour)
	suggest(t, s, slug, aliceTok, "still here") // keeps only the source active

	bob := dialWS(t, ts, slug, bobTok)
	if snap := readSnapshot(t, bob); snap["next"] == nil {
		t.Fatal("precondition: source should still show its successor")
	}
	if n, err := s.PruneExpired(90 * 24 * time.Hour); err != nil || n != 1 {
		t.Fatalf("pruned n=%d err=%v, want 1", n, err)
	}
	if snap := readSnapshot(t, bob); snap["next"] != nil {
		t.Fatalf("source socket still shows the pruned successor: %v", snap["next"])
	}
}
