package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/quickvote/quickvote/internal/store"
)

const (
	wsWriteWait  = 10 * time.Second
	wsPingPeriod = 30 * time.Second
	wsSendBuffer = 8
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// The room state is not sensitive across origins in a way that matters
	// for this self-hosted app, and the frontend is served from the same
	// origin as the API in production; allow all origins so local dev
	// (Vite on a different port, proxying /api) keeps working too.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// wsConn is a single upgraded WebSocket connection registered with the hub.
// send is a small buffered channel; a slow or dead client is dropped rather
// than allowed to block broadcasts to everyone else.
type wsConn struct {
	conn  *websocket.Conn
	send  chan []byte
	slug  string
	token string // session token presented on connect; "" for spectators
}

// hub tracks live WebSocket connections per vote slug so that state changes
// can be broadcast as personalized snapshots.
type hub struct {
	mu    sync.Mutex
	rooms map[string]map[*wsConn]struct{}
}

func newHub() *hub {
	return &hub{rooms: make(map[string]map[*wsConn]struct{})}
}

func (h *hub) add(c *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns, ok := h.rooms[c.slug]
	if !ok {
		conns = make(map[*wsConn]struct{})
		h.rooms[c.slug] = conns
	}
	conns[c] = struct{}{}
}

func (h *hub) remove(c *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns, ok := h.rooms[c.slug]
	if !ok {
		return
	}
	delete(conns, c)
	if len(conns) == 0 {
		delete(h.rooms, c.slug)
	}
}

// connsFor returns a snapshot slice of the live connections for slug, safe
// to range over after the hub lock is released.
func (h *hub) connsFor(slug string) []*wsConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := h.rooms[slug]
	if len(conns) == 0 {
		return nil
	}
	out := make([]*wsConn, 0, len(conns))
	for c := range conns {
		out = append(out, c)
	}
	return out
}

// handleWS implements GET /api/votes/{slug}/ws. The session token is passed
// as the ?token= query parameter (WebSocket upgrade requests can't carry a
// custom Authorization header from a browser EventSource-style API); a
// missing or invalid token connects the caller as a spectator. On connect
// and on every subsequent room change, the caller's personalized snapshot is
// pushed as a single JSON text message.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, err := s.store.GetVote(slug); err != nil {
		writeError(w, http.StatusNotFound, "vote not found")
		return
	}

	token := r.URL.Query().Get("token")

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote an error response.
		return
	}

	c := &wsConn{
		conn:  conn,
		send:  make(chan []byte, wsSendBuffer),
		slug:  slug,
		token: token,
	}
	s.hub.add(c)

	go s.wsWritePump(c)
	go s.wsReadPump(c)

	s.pushSnapshot(c)
}

// wsReadPump does nothing with incoming messages (the protocol is
// server-push only) but must keep reading so control frames (close, pong)
// are processed and the connection's death is detected promptly.
func (s *Server) wsReadPump(c *wsConn) {
	defer func() {
		s.hub.remove(c)
		close(c.send)
		c.conn.Close()
	}()
	c.conn.SetReadLimit(4096)
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

// wsWritePump is the sole writer goroutine for c.conn, serializing snapshot
// pushes and periodic pings. It exits (closing the connection) when send is
// closed or a write fails.
func (s *Server) wsWritePump(c *wsConn) {
	ticker := time.NewTicker(wsPingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// pushSnapshot builds c's personalized room-state snapshot and enqueues it
// for delivery. If the connection's send buffer is full (a stuck client),
// the connection is dropped rather than blocking the caller.
func (s *Server) pushSnapshot(c *wsConn) {
	data, ok := s.buildSnapshotJSON(c.slug, c.token)
	if !ok {
		return
	}
	select {
	case c.send <- data:
	default:
		log.Printf("ws: dropping slow connection for vote %s", c.slug)
		s.hub.remove(c)
		close(c.send)
	}
}

// buildSnapshotJSON loads the current room state for slug, personalized for
// the participant identified by token (spectator if token is empty or
// invalid), and marshals it to JSON.
func (s *Server) buildSnapshotJSON(slug, token string) ([]byte, bool) {
	v, err := s.store.GetVote(slug)
	if err != nil {
		return nil, false
	}
	parts, err := s.store.Participants(slug)
	if err != nil {
		return nil, false
	}
	opts, err := s.store.Options(slug)
	if err != nil {
		return nil, false
	}
	ballots, err := s.store.Ballots(slug)
	if err != nil {
		return nil, false
	}

	var requester *store.ParticipantRow
	if token != "" {
		if p, err := s.store.ParticipantByToken(slug, token); err == nil {
			requester = &p
		}
	}

	state := BuildRoomState(v, parts, opts, ballots, requester)
	data, err := json.Marshal(state)
	if err != nil {
		return nil, false
	}
	return data, true
}

// broadcast pushes a fresh personalized snapshot to every connection
// currently registered for slug. Wired as the Server's onChange hook in
// New, so every call to s.changed(slug) fans out to live WebSocket clients.
func (s *Server) broadcast(slug string) {
	for _, c := range s.hub.connsFor(slug) {
		s.pushSnapshot(c)
	}
}
