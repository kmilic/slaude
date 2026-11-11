// Package inbox posts messages into a running Claude Code session through its
// inbox socket (CLAUDE_CODE_MESSAGING_SOCKET).
//
// Wire format, taken from the help text in Claude Code 2.1.316: newline-delimited
// JSON, one user message per connection:
//
//	{"type":"user","message":{"role":"user","content":"hello"}}
//
// An auth line with CLAUDE_CODE_MESSAGING_TOKEN may come first, but it is
// optional on macOS and Linux, so Send doesn't write it. Claude Code closes a connection
// that sends no complete line within 30 seconds, so connect only when the
// message is ready.
package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

type userLine struct {
	Type    string      `json:"type"`
	Message userMessage `json:"message"`
}

type userMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Send writes text to the session listening on path and returns anything the
// session wrote back within replyWait. An empty reply is not an error: the
// reply format is not documented, so callers only print it.
//
// path may keep the "uds:" prefix that /status shows.
func Send(ctx context.Context, path, text string, replyWait time.Duration) (string, error) {
	path = strings.TrimPrefix(path, "uds:")
	if path == "" {
		return "", errors.New("socket path is empty")
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("message is empty")
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return "", fmt.Errorf("connect to %s: %w", path, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return "", fmt.Errorf("set write deadline: %w", err)
		}
	}

	// Encode appends '\n', which is the line framing the socket expects.
	msg := userLine{Type: "user", Message: userMessage{Role: "user", Content: text}}
	if err := json.NewEncoder(conn).Encode(msg); err != nil {
		return "", fmt.Errorf("write message: %w", err)
	}

	// Signal end of input, so the session doesn't wait for more lines.
	if uc, ok := conn.(*net.UnixConn); ok {
		if err := uc.CloseWrite(); err != nil {
			return "", fmt.Errorf("close write side: %w", err)
		}
	}

	if err := conn.SetReadDeadline(time.Now().Add(replyWait)); err != nil {
		return "", fmt.Errorf("set read deadline: %w", err)
	}
	reply, err := io.ReadAll(conn)
	if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
		return string(reply), fmt.Errorf("read reply: %w", err)
	}
	return string(reply), nil
}
