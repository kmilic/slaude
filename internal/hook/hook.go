// Package hook carries Claude Code hook events from `slaude notify` to the
// daemon over a local unix socket (~/.slaude/daemon.sock).
package hook

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Envelope is what notify sends to the daemon: the hook's JSON unchanged, plus
// the session's inbox socket path. The hook JSON doesn't contain the path;
// notify reads it from CLAUDE_CODE_MESSAGING_SOCKET in its environment.
type Envelope struct {
	InboxSocket string          `json:"inbox_socket"`
	Payload     json.RawMessage `json:"payload"`
}

// Event is the part of a hook payload the daemon uses.
type Event struct {
	SessionID      string `json:"session_id"`
	HookEventName  string `json:"hook_event_name"` // SessionStart, Notification, Stop
	Cwd            string `json:"cwd"`
	TranscriptPath string `json:"transcript_path"`

	LastAssistantMessage string `json:"last_assistant_message"` // Stop only: Claude's final answer
	Message              string `json:"message"`                // Notification only
	NotificationType     string `json:"notification_type"`      // Notification only, e.g. idle_prompt
}

// Event decodes the fields the daemon uses from the payload.
func (e Envelope) Event() (Event, error) {
	var ev Event
	if err := json.Unmarshal(e.Payload, &ev); err != nil {
		return Event{}, fmt.Errorf("decode hook payload: %w", err)
	}
	if ev.SessionID == "" || ev.HookEventName == "" {
		return Event{}, errors.New("hook payload has no session_id or hook_event_name")
	}
	return ev, nil
}

// DefaultSocketPath is where the daemon listens for hook events.
func DefaultSocketPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".slaude", "daemon.sock"), nil
}

// Forward sends env to the daemon as one JSON line. It gives up after timeout,
// so a missing or stuck daemon never slows Claude down.
func Forward(socketPath string, env Envelope, timeout time.Duration) error {
	conn, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	if err := json.NewEncoder(conn).Encode(env); err != nil {
		return fmt.Errorf("send to daemon: %w", err)
	}
	return nil
}

// Listen accepts envelopes on socketPath and calls handle for each one, until
// ctx is cancelled. handle may be called from several goroutines at once.
func Listen(ctx context.Context, socketPath string, handle func(Envelope), log *slog.Logger) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}
	// A socket file left by a previous run blocks Listen. POC: assume one daemon.
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove old socket: %w", err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", socketPath, err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil { // only this user can send events
		ln.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		go serve(conn, handle, log)
	}
}

func serve(conn net.Conn, handle func(Envelope), log *slog.Logger) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	r := bufio.NewReaderSize(conn, 64*1024)
	line, err := r.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		log.Warn("hook connection closed without data", "err", err)
		return
	}
	var env Envelope
	if err := json.Unmarshal(line, &env); err != nil {
		log.Warn("bad hook message", "err", err)
		return
	}
	handle(env)
}
