// Package hub accepts daemon WebSocket connections on the service side and
// keeps one connection per user.
package hub

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"slaude/internal/protocol"
)

const (
	// helloTimeout is how long a new connection has to send its hello.
	helloTimeout = 5 * time.Second
	// sendTimeout bounds one write to a daemon, so a stuck connection can't
	// block the caller (the Slack event loop).
	sendTimeout = 5 * time.Second
)

// ErrOffline means the user has no daemon connected.
var ErrOffline = errors.New("daemon not connected")

// Hub holds the connected daemons.
//
// POC pairing: one fixed token maps to one Slack user.
type Hub struct {
	token  string
	userID string
	log    *slog.Logger

	// OnEvent is called for each event message from a daemon, one at a time
	// per daemon, in the order received. Set it before serving.
	OnEvent func(ctx context.Context, userID string, msg protocol.Message)

	mu    sync.Mutex
	conns map[string]*websocket.Conn // Slack user ID -> daemon connection
}

func New(token, userID string, log *slog.Logger) *Hub {
	return &Hub{token: token, userID: userID, log: log, conns: map[string]*websocket.Conn{}}
}

// Connected reports whether userID has a daemon connected.
func (h *Hub) Connected(userID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[userID] != nil
}

// Send writes msg to userID's daemon. It returns ErrOffline when no daemon is
// connected.
//
// The lock only covers the lookup, not the network write. The write can run
// while ServeHTTP reads the same connection; coder/websocket allows one reader
// and writers at the same time.
func (h *Hub) Send(ctx context.Context, userID string, msg protocol.Message) error {
	h.mu.Lock()
	conn := h.conns[userID]
	h.mu.Unlock()
	if conn == nil {
		return ErrOffline
	}

	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	if err := wsjson.Write(ctx, conn, msg); err != nil {
		return fmt.Errorf("send to daemon of %s: %w", userID, err)
	}
	return nil
}

// ServeHTTP upgrades the request to a WebSocket, checks the hello, registers
// the connection and keeps reading until the daemon disconnects.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.log.Warn("websocket accept failed", "err", err)
		return
	}
	defer conn.CloseNow()

	userID, ok := h.authenticate(r.Context(), conn)
	if !ok {
		return
	}
	h.register(userID, conn)
	defer h.unregister(userID, conn)

	// Read loop: handles the daemon's messages one at a time, in order. Ends
	// when the daemon disconnects.
	for {
		var msg protocol.Message
		if err := wsjson.Read(r.Context(), conn, &msg); err != nil {
			h.log.Info("daemon disconnected", "user", userID, "err", err)
			return
		}
		h.log.Info("message from daemon", "user", userID, "type", msg.Type, "event", msg.Event, "session", msg.SessionID, "text_len", len(msg.Text))
		if msg.Type == protocol.TypeEvent && h.OnEvent != nil {
			h.OnEvent(r.Context(), userID, msg)
		}
	}
}

// authenticate reads the first message, which must be a hello with the right token.
func (h *Hub) authenticate(ctx context.Context, conn *websocket.Conn) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, helloTimeout)
	defer cancel()

	var hello protocol.Message
	if err := wsjson.Read(ctx, conn, &hello); err != nil {
		h.log.Warn("no hello from daemon", "err", err)
		return "", false
	}
	// Constant-time compare, so the token can't be guessed from response timing.
	if hello.Type != protocol.TypeHello || subtle.ConstantTimeCompare([]byte(hello.Token), []byte(h.token)) != 1 {
		h.log.Warn("daemon rejected: invalid hello")
		_ = wsjson.Write(ctx, conn, protocol.Message{Type: protocol.TypeError, Message: "invalid token"})
		_ = conn.Close(websocket.StatusPolicyViolation, "invalid token")
		return "", false
	}
	return h.userID, true
}

// register stores conn for userID. A newer connection replaces an older one
// (reconnect after sleep, or a second laptop), and the old one is closed.
func (h *Hub) register(userID string, conn *websocket.Conn) {
	h.mu.Lock()
	old := h.conns[userID]
	h.conns[userID] = conn
	h.mu.Unlock()

	if old != nil {
		_ = old.Close(websocket.StatusNormalClosure, "replaced by a newer connection")
	}
	h.log.Info("daemon connected", "user", userID)
}

// unregister removes conn, unless a newer connection already replaced it.
func (h *Hub) unregister(userID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[userID] == conn {
		delete(h.conns, userID)
	}
}
