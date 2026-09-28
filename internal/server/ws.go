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
	// wsPongWait must exceed wsPingPeriod: each pong extends the read
	// deadline, so a half-open connection is reaped within one missed ping.
	wsPongWait   = 70 * time.Second
	wsSendBuffer = 8
)

// upgrader builds the WebSocket upgrader. Origins are checked (same-origin
// plus Config.AllowedOrigins) so another site can't open live connections
// from its visitors' browsers.
func (s *Server) upgrader() *websocket.Upgrader {
	return &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     s.checkOrigin,
	}
}

// wsConn is a single upgraded WebSocket connection registered with the hub.
// send is a small buffered channel; a slow or dead client is dropped rather
// than allowed to block broadcasts to everyone else.
//
// Teardown is single-sourced through done: c.send is NEVER closed (so no
// goroutine can ever send-on-closed or close-of-closed panic). Instead any
// owner that decides the connection is finished calls c.close(), which closes
// done exactly once; the write pump observes done and exits, and senders
// select on done so they never block on a dead connection.
type wsConn struct {
	conn      *websocket.Conn
	send      chan []byte
	slug      string
	token     string // session token presented on connect; "" for spectators
	ip        string // client IP, for the per-IP connection cap
	done      chan struct{}
	closeOnce sync.Once
}

// close signals that the connection is finished. It is safe to call from any
// goroutine any number of times; done is closed exactly once.
func (c *wsConn) close() {
	c.closeOnce.Do(func() { close(c.done) })
}

// hub tracks live WebSocket connections per vote slug so that state changes
// can be broadcast as personalized snapshots.
type hub struct {
	mu    sync.Mutex
	rooms map[string]map[*wsConn]struct{}
	perIP map[string]int
}

func newHub() *hub {
	return &hub{
		rooms: make(map[string]map[*wsConn]struct{}),
		perIP: make(map[string]int),
	}
}

// reserve claims a connection slot for (slug, ip), reporting false if either
// cap is already reached. A successful reserve must be paired with add (on
// upgrade success) or release (on failure).
func (h *hub) reserve(slug, ip string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.rooms[slug]) >= maxWSPerVote || h.perIP[ip] >= maxWSPerIP {
		return false
	}
	h.perIP[ip]++
	return true
}

// release returns a per-IP slot claimed by reserve.
func (h *hub) release(ip string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.releaseLocked(ip)
}

func (h *hub) releaseLocked(ip string) {
	if h.perIP[ip] <= 1 {
		delete(h.perIP, ip)
	} else {
		h.perIP[ip]--
	}
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

// remove unregisters c and frees its per-IP slot. Idempotent.
func (h *hub) remove(c *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns, ok := h.rooms[c.slug]
	if !ok {
		return
	}
	if _, ok := conns[c]; !ok {
		return
	}
	delete(conns, c)
	h.releaseLocked(c.ip)
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

	ip := s.clientIP(r)
	if !s.hub.reserve(slug, ip) {
		writeError(w, http.StatusTooManyRequests, "too many live connections")
		return
	}

	conn, err := s.upgrader().Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote an error response.
		s.hub.release(ip)
		return
	}

	c := &wsConn{
		conn:  conn,
		send:  make(chan []byte, wsSendBuffer),
		slug:  slug,
		token: token,
		ip:    ip,
		done:  make(chan struct{}),
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
		c.close()
		c.conn.Close()
	}()
	c.conn.SetReadLimit(4096)
	_ = c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

// wsWritePump is the sole writer goroutine for c.conn, serializing snapshot
// pushes and periodic pings. It exits (closing the connection) when c.done is
// signaled or a write fails.
func (s *Server) wsWritePump(c *wsConn) {
	ticker := time.NewTicker(wsPingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-c.done:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
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
	case <-c.done:
		// Connection is already tearing down; nothing to enqueue.
	default:
		log.Printf("ws: dropping slow connection for vote %s", c.slug)
		s.hub.remove(c)
		c.close()
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

// Broadcast forces a fresh personalized snapshot out to every live connection
// for slug. It is the exported form of the internal onChange fan-out.
func (s *Server) Broadcast(slug string) {
	s.broadcast(slug)
}
