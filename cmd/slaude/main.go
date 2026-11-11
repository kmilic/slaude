// Command slaude runs on the user's machine.
//
//	slaude daemon      connect to the Slaude service and listen for hook events
//	slaude notify      run by Claude Code hooks: forward the hook's JSON to the daemon
//	slaude guard-send  run by the PreToolUse hook for SendMessage: always blocks it
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
	case "guard-send":
		guardSend()
	default:
		usage()
	}
}

// usage exits 1, not the usual 2: for a hook, exit 2 has a meaning (on Stop,
// it makes Claude keep working).
func usage() {
	fmt.Fprintln(os.Stderr, "usage: slaude daemon | slaude notify | slaude guard-send")
	os.Exit(1)
}

// notify must be fast and must never affect Claude: it always exits 0, even on a panic.
func notify() {
	defer os.Exit(0)

	payload, err := readHookInput(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "slaude notify: %v, skipped\n", err)
		return
	}
	path, err := hook.DefaultSocketPath()
	if err != nil {
		return
	}
	// Claude Code sets CLAUDE_CODE_MESSAGING_SOCKET to the session's inbox socket.
	env := hook.Envelope{
		InboxSocket: os.Getenv("CLAUDE_CODE_MESSAGING_SOCKET"),
		Payload:     payload,
	}
	if err := hook.Forward(path, env, time.Second); err != nil {
		fmt.Fprintln(os.Stderr, "slaude notify: daemon not reachable:", err)
	}
}

const mib = 1024 * 1024

// maxHookInput caps how much of a hook's input notify reads into memory.
const maxHookInput = 10 * mib

// readHookInput reads a hook's JSON input, up to maxHookInput bytes.
func readHookInput(r io.Reader) ([]byte, error) {
	// Read one byte past the cap, so input that is too large can be told apart
	// from input that is exactly at the cap.
	payload, err := io.ReadAll(io.LimitReader(r, maxHookInput+1))
	switch {
	case err != nil:
		return nil, fmt.Errorf("read hook input: %w", err)
	case len(payload) > maxHookInput:
		return nil, fmt.Errorf("hook input is larger than %d MiB", maxHookInput/mib)
	case !json.Valid(payload):
		return nil, errors.New("hook input is not JSON")
	}
	return payload, nil
}

func daemon() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	// Check the configuration before starting anything.
	serviceURL := mustEnv(log, "SLAUDE_SERVICE_URL")
	pairingToken := mustEnv(log, "SLAUDE_PAIRING_TOKEN")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sockPath, err := hook.DefaultSocketPath()
	if err != nil {
		log.Error("find home dir", "err", err)
		os.Exit(1)
	}
	sessions := &sessionTable{inbox: map[string]string{}}

	// events queues hook events for the service. Each hook event is handled in
	// its own goroutine, but only runServiceConn writes to the WebSocket, so the
	// hooks hand their events over here. Up to 100 can wait while it's busy;
	// beyond that, new events are dropped.
	events := make(chan protocol.Message, 100)

	go func() {
		log.Info("listening for hook events", "socket", sockPath)
		if err := hook.Listen(ctx, sockPath, func(env hook.Envelope) {
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

	if err := runServiceConn(ctx, log, serviceURL, pairingToken, events, sessions); err != nil && !errors.Is(err, context.Canceled) {
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

// runServiceConn connects to the service, sends queued events in order,
// delivers the service's replies into Claude sessions, and returns when the
// connection is lost or ctx is cancelled. No reconnect yet.
func runServiceConn(ctx context.Context, log *slog.Logger, url, token string, events <-chan protocol.Message, sessions *sessionTable) error {
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

	// Slack → Claude: the reader goroutine handles the service's messages one at
	// a time, so replies are delivered in order. readErr has one slot so the
	// reader can always send its final error and exit, even after the writer
	// has returned.
	readErr := make(chan error, 1)
	go func() {
		// refused keeps the service's error message, to explain why it then closes the connection.
		var refused string
		for {
			var msg protocol.Message
			if err := wsjson.Read(ctx, conn, &msg); err != nil {
				if refused != "" {
					readErr <- fmt.Errorf("service refused: %s", refused)
				} else {
					readErr <- fmt.Errorf("connection to service lost: %w", err)
				}
				return
			}
			switch msg.Type {
			case protocol.TypeDeliver:
				deliver(ctx, sessions, msg, sendToInbox, log)
			case protocol.TypeError:
				refused = msg.Message
				log.Warn("error from service", "message", msg.Message)
			default:
				log.Warn("unexpected message from service", "type", msg.Type)
			}
		}
	}()

	// Claude → Slack: the writer is the only goroutine that writes to the
	// WebSocket, one event at a time, in order. Fire-and-forget: the service
	// sends no reply, so an event written just before the connection drops can
	// be lost (POC trade-off).
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
		log.Error("missing environment variable", "key", key)
		os.Exit(1)
	}
	return v
}
