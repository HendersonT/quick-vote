package server_test

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestServerCloseClosesWebSockets checks that shutdown sends live clients a
// proper close frame (so browsers reconnect cleanly) and that Close is
// idempotent.
func TestServerCloseClosesWebSockets(t *testing.T) {
	s, ts := newWSTestServer(t)
	slug, _, tok, _ := createVote(t, s, nil)
	conn := dialWS(t, ts, slug, tok)
	readSnapshot(t, conn)

	s.Close()
	s.Close() // idempotent

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := conn.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		t.Fatalf("expected a normal close frame, got %v", err)
	}
}
