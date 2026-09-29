package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/HendersonT/quick-vote/internal/store"
)

const (
	wsWriteWait  = 10 * time.Second
	wsPingPeriod = 30 * time.Second
	// wsPongWait must exceed wsPingPeriod: each pong extends the read
	// deadline, so a half-open connection is reaped within one missed ping.
	wsPongWait   = 70 * time.Second
	wsSendBuffer = 8
	// defaultWSAuthTimeout bounds how long an upgraded connection may sit
	// without sending its auth message before it is closed.
	defaultWSAuthTimeout = 5 * time.Second
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
	token     string // session token from the auth message; "" for spectators
	ip        string // client IP, for the per-IP connection cap
	done      chan struct{}
	closeOnce sync.Once

	// creator is set when the auth message also carried the vote's valid
	// creator token, which the creator's snapshot needs for creator secrets
	// (see BuildRoomState). Set before the connection joins the hub and
	// never changed, so broadcasts read it without locking.
	creator bool
}

// close signals that the connection is finished. It is safe to call from any
// goroutine any number of times; done is closed exactly once.
func (c *wsConn) close() {
	c.closeOnce.Do(func() { close(c.done) })
}

// hub tracks live WebSocket connections per vote slug so that state changes
// can be broadcast as personalized snapshots.
//
// pending counts per-slug slots reserved by connections that have upgraded
// but not yet sent their auth message. They are not in rooms (so they get no
// broadcasts) but must still count toward maxWSPerVote, otherwise a burst of
// silent dials could exceed the cap during the auth window.
type hub struct {
	mu      sync.Mutex
	rooms   map[string]map[*wsConn]struct{}
	pending map[string]int
	perIP   map[string]int
}

func newHub() *hub {
	return &hub{
		rooms:   make(map[string]map[*wsConn]struct{}),
		pending: make(map[string]int),
		perIP:   make(map[string]int),
	}
}

// reserve claims a connection slot for (slug, ip), reporting false if either
// cap is already reached. A successful reserve must be paired with add (once
// the connection authenticates) or release (on any failure before that).
func (h *hub) reserve(slug, ip string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.rooms[slug])+h.pending[slug] >= maxWSPerVote || h.perIP[ip] >= maxWSPerIP {
		return false
	}
	h.pending[slug]++
	h.perIP[ip]++
	return true
}

// release returns the per-vote and per-IP slots claimed by reserve for a
// connection that never made it into the hub.
func (h *hub) release(slug, ip string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unpendLocked(slug)
	h.releaseLocked(ip)
}

func (h *hub) unpendLocked(slug string) {
	if h.pending[slug] <= 1 {
		delete(h.pending, slug)
	} else {
		h.pending[slug]--
	}
}

func (h *hub) releaseLocked(ip string) {
	if h.perIP[ip] <= 1 {
		delete(h.perIP, ip)
	} else {
		h.perIP[ip]--
	}
}

// add registers an authenticated connection, converting the pending slot
// claimed by reserve into a room membership.
func (h *hub) add(c *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unpendLocked(c.slug)
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

// all returns a snapshot slice of every live connection across all rooms,
// safe to range over after the hub lock is released.
func (h *hub) all() []*wsConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*wsConn
	for _, conns := range h.rooms {
		for c := range conns {
			out = append(out, c)
		}
	}
	return out
}

// handleWS implements GET /api/votes/{slug}/ws. After the upgrade the
// client must send {"type":"auth","token":"...","creatorToken":"..."} as its
// first message (see wsAuth); an empty or invalid token connects the caller
// as a spectator, and creatorToken is optional. On
// auth and on every subsequent room change, the caller's personalized
// snapshot is pushed as a single JSON text message.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, err := s.store.GetVote(slug); err != nil {
		writeError(w, http.StatusNotFound, "vote not found")
		return
	}

	ip := s.clientIP(r)
	if !s.hub.reserve(slug, ip) {
		writeError(w, http.StatusTooManyRequests, "too many live connections")
		return
	}

	conn, err := s.upgrader().Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote an error response.
		s.hub.release(slug, ip)
		return
	}

	c := &wsConn{
		conn: conn,
		send: make(chan []byte, wsSendBuffer),
		slug: slug,
		ip:   ip,
		done: make(chan struct{}),
	}
	go s.wsAuth(c)
}

// wsAuthTimeout is how long wsAuth waits for the auth message.
func (s *Server) wsAuthTimeout() time.Duration {
	if s.cfg.WSAuthTimeout > 0 {
		return s.cfg.WSAuthTimeout
	}
	return defaultWSAuthTimeout
}

// wsAuth waits for the client's auth message before the connection joins the
// hub. Tokens travel in a message rather than the URL so they never land in
// proxy or tunnel access logs. A missing, malformed or wrong-typed first
// message closes the connection with a policy-violation code.
func (s *Server) wsAuth(c *wsConn) {
	c.conn.SetReadLimit(4096)
	_ = c.conn.SetReadDeadline(time.Now().Add(s.wsAuthTimeout()))
	var msg struct {
		Type         string `json:"type"`
		Token        string `json:"token"`
		CreatorToken string `json:"creatorToken"`
	}
	if err := c.conn.ReadJSON(&msg); err != nil || msg.Type != "auth" {
		_ = c.conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "auth required"),
			time.Now().Add(wsWriteWait))
		c.conn.Close()
		s.hub.release(c.slug, c.ip)
		return
	}
	c.token = msg.Token
	if msg.CreatorToken != "" {
		if v, err := s.store.GetVote(c.slug); err == nil {
			c.creator = creatorTokenValid(msg.CreatorToken, v)
		}
	}
	s.hub.add(c)
	go s.wsWritePump(c)
	// Close may have swept the hub while this connection was still pending.
	// Checking stop after add closes that gap: either Close's sweep saw c, or
	// stop is already closed here.
	select {
	case <-s.stop:
		s.hub.remove(c)
		c.close()
	default:
		s.pushSnapshot(c)
	}
	s.wsReadPump(c)
}

// wsReadPump does nothing with incoming messages after auth (the protocol is
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
			// A normal-closure frame (rather than an empty payload) lets the
			// client tell a deliberate close — shutdown, prune — from a
			// dropped network and reconnect accordingly.
			_ = c.conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "closing"))
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
// for delivery. It is used for the initial push on connect; room changes go
// through broadcast, which shares one room load across all connections.
//
// If the vote is gone (pruned after the upgrade's existence check), the
// connection is closed: PruneExpired's sweep only saw registered
// connections, and this one would otherwise sit open with nothing to show.
func (s *Server) pushSnapshot(c *wsConn) {
	d, err := s.loadRoom(c.slug)
	if errors.Is(err, store.ErrNotFound) {
		s.hub.remove(c)
		c.close()
		return
	}
	if err != nil {
		return
	}
	data, err := json.Marshal(d.stateFor(c.token, c.creator))
	if err != nil {
		return
	}
	s.enqueue(c, data)
}

// enqueue hands a marshaled snapshot to c's write pump. If the connection's
// send buffer is full (a stuck client), the connection is dropped rather
// than blocking the caller.
func (s *Server) enqueue(c *wsConn, data []byte) {
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

// broadcast pushes a fresh personalized snapshot to every connection
// currently registered for slug. Wired as the Server's onChange hook in
// New, so every call to s.changed(slug) fans out to live WebSocket clients.
// Room data is loaded once and personalized in memory per connection, so the
// store cost of a broadcast doesn't grow with the number of watchers.
func (s *Server) broadcast(slug string) {
	conns := s.hub.connsFor(slug)
	if len(conns) == 0 {
		return
	}
	d, err := s.loadRoom(slug)
	if err != nil {
		return
	}
	for _, c := range conns {
		data, err := json.Marshal(d.stateFor(c.token, c.creator))
		if err != nil {
			continue
		}
		s.enqueue(c, data)
	}
}

// Broadcast forces a fresh personalized snapshot out to every live connection
// for slug. It is the exported form of the internal onChange fan-out.
func (s *Server) Broadcast(slug string) {
	s.broadcast(slug)
}
