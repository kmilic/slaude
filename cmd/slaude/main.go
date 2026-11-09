// Command slaude runs on the user's machine.
//
//	slaude daemon    connect to the Slaude service and listen for hook events
//	slaude notify    run by Claude Code hooks: forward the hook's JSON to the daemon
//
// The daemon remembers each session's inbox socket and forwards hook events to
// the service. Delivering Slack replies into sessions comes in step 7.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"slaude/internal/hook"
	"slaude/internal/protocol"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "notify":
		notify()
	case "daemon":
		daemon()
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: slaude daemon | slaude notify")
	os.Exit(2)
}

// notify must be fast and must never break Claude: it always exits 0.
func notify() {
	defer os.Exit(0)

	payload, err := io.ReadAll(io.LimitReader(os.Stdin, 10<<20))
	if err != nil || !json.Valid(payload) {
		fmt.Fprintln(os.Stderr, "slaude notify: hook input is not JSON, skipped")
		return
	}
	path, err := hook.DefaultSocketPath()
	if err != nil {
		return
	}
	env := hook.Envelope{
		InboxSocket: os.Getenv("CLAUDE_CODE_MESSAGING_SOCKET"),
		Payload:     payload,
	}
	if err := hook.Forward(path, env, time.Second); err != nil {
		fmt.Fprintln(os.Stderr, "slaude notify: daemon not reachable:", err)
	}
}

func daemon() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sockPath, err := hook.DefaultSocketPath()
	if err != nil {
		log.Error("find home dir", "err", err)
		os.Exit(1)
	}
	sessions := &sessionTable{inbox: map[string]string{}}
	rawLog := openRawLog(log)

	// Hook goroutines put events here; runServiceConn is the only reader and
	// the only writer on the WebSocket. 100 slots absorb bursts, e.g. two
	// sessions finishing at once, while the writer is busy with one send.
	events := make(chan protocol.Message, 100)

	go func() {
		log.Info("listening for hook events", "socket", sockPath)
		if err := hook.Listen(ctx, sockPath, func(env hook.Envelope) {
			if rawLog != nil {
				rawLog.Write(append(append([]byte{}, env.Payload...), '\n'))
			}
			ev, err := env.Event()
			if err != nil {
				log.Warn("ignored hook event", "err", err)
				return
			}
			sessions.set(ev.SessionID, env.InboxSocket)
			log.Info("hook event", "event", ev.HookEventName, "session", ev.SessionID, "cwd", ev.Cwd, "inbox", env.InboxSocket)

			msg, ok := ev.ToProtocol()
			if !ok {
				return // e.g. the idle reminder
			}
			// Non-blocking: a hook goroutine never waits on the network.
			select {
			case events <- msg:
			default:
				log.Warn("event dropped: queue full, service busy or down", "event", ev.HookEventName, "session", ev.SessionID)
			}
		}, log); err != nil {
			log.Error("hook listener stopped", "err", err)
			stop()
		}
	}()

	if err := runServiceConn(ctx, log, events); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("daemon stopped", "err", err)
		os.Exit(1)
	}
	log.Info("stopped")
}

// sessionTable maps session_id to the session's inbox socket path. Only the
// daemon knows these paths; the service never sees them.
type sessionTable struct {
	mu    sync.Mutex
	inbox map[string]string
}

func (s *sessionTable) set(sessionID, inboxSocket string) {
	if inboxSocket == "" {
		return // keep a path we already know
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inbox[sessionID] = inboxSocket
}

func (s *sessionTable) get(sessionID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.inbox[sessionID]
	return p, ok
}

// openRawLog appends every raw hook payload to ~/.slaude/hooks.log, to see the
// real payload fields Claude Code sends. Local, owner-only.
func openRawLog(log *slog.Logger) *os.File {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(home, ".slaude", "hooks.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.Warn("raw hook log disabled", "err", err)
		return nil
	}
	return f
}

// runServiceConn connects to the service, sends queued events in order and
// returns when the connection is lost or ctx is cancelled. No reconnect yet (step 10).
func runServiceConn(ctx context.Context, log *slog.Logger, events <-chan protocol.Message) error {
	url := mustEnv(log, "SLAUDE_SERVICE_URL")
	token := mustEnv(log, "SLAUDE_PAIRING_TOKEN")

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, url, nil)
	if err != nil {
		return fmt.Errorf("connect to service %s: %w", url, err)
	}
	defer conn.CloseNow()

	if err := wsjson.Write(dialCtx, conn, protocol.Message{Type: protocol.TypeHello, Token: token}); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}
	log.Info("connected to service", "url", url)

	// Reader: receives every message from the service (deliver from step 7 on)
	// and handles each one in this loop. Read blocks until a message arrives, so
	// it runs in its own goroutine, separate from the writer below.
	//
	// readErr carries only errors, never messages: the reader sends at most one
	// (connection lost, or an error message from the service) and then exits.
	// One slot lets that send complete even after the writer has returned, so the
	// reader never blocks forever (no goroutine leak).
	readErr := make(chan error, 1)
	go func() {
		for {
			var msg protocol.Message
			if err := wsjson.Read(ctx, conn, &msg); err != nil {
				readErr <- fmt.Errorf("connection to service lost: %w", err)
				return
			}
			if msg.Type == protocol.TypeError {
				readErr <- fmt.Errorf("service refused: %s", msg.Message)
				return
			}
			log.Info("message from service", "type", msg.Type, "session", msg.SessionID) // step 7: deliver
		}
	}()

	// Writer: the only goroutine that writes to the WebSocket, one event at a
	// time, in order. Fire-and-forget: the service sends no reply, so an event
	// written just before the connection drops can be lost (POC trade-off).
	for {
		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "daemon stopping")
			return ctx.Err()
		case err := <-readErr:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		case msg := <-events:
			if err := wsjson.Write(ctx, conn, msg); err != nil {
				return fmt.Errorf("send event: %w", err)
			}
			log.Info("event sent to service", "event", msg.Event, "session", msg.SessionID)
		}
	}
}

func mustEnv(log *slog.Logger, key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Error("missing environment variable (run slaude-env first)", "key", key)
		os.Exit(1)
	}
	return v
}
