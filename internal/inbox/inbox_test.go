package inbox

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeSession listens on a unix socket like a Claude Code session, records the
// lines it receives on one connection, then writes reply (if any) and closes.
// When holdOpen is true it never closes, to test a session that sends nothing back.
func fakeSession(t *testing.T, reply string, holdOpen bool) (path string, lines <-chan []string) {
	t.Helper()
	// macOS caps unix socket paths at ~104 bytes, and t.TempDir() can exceed that.
	dir, err := os.MkdirTemp("/tmp", "slaude")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path = filepath.Join(dir, "s.sock")

	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	out := make(chan []string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var got []string
		sc := bufio.NewScanner(conn)
		for sc.Scan() { // ends when the client closes its write side
			got = append(got, sc.Text())
		}
		out <- got
		if reply != "" {
			conn.Write([]byte(reply))
		}
		if holdOpen {
			time.Sleep(time.Second)
		}
	}()
	return path, out
}

func decode(t *testing.T, line string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("line is not JSON: %q: %v", line, err)
	}
	return m
}

func TestSend_WritesAuthThenUserLine(t *testing.T) {
	path, lines := fakeSession(t, "", false)

	_, err := Send(context.Background(), path, "tok-123", "hello", time.Second)
	if err != nil {
		t.Fatal(err)
	}

	got := <-lines
	if len(got) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(got), got)
	}
	auth := decode(t, got[0])
	if auth["type"] != "auth" || auth["token"] != "tok-123" {
		t.Errorf("auth line = %v", auth)
	}
	user := decode(t, got[1])
	msg, _ := user["message"].(map[string]any)
	if user["type"] != "user" || msg["role"] != "user" || msg["content"] != "hello" {
		t.Errorf("user line = %v", user)
	}
}

func TestSend_NoTokenSkipsAuthLine(t *testing.T) {
	path, lines := fakeSession(t, "", false)

	if _, err := Send(context.Background(), path, "", "hello", time.Second); err != nil {
		t.Fatal(err)
	}

	got := <-lines
	if len(got) != 1 || decode(t, got[0])["type"] != "user" {
		t.Fatalf("want only the user line, got %q", got)
	}
}

func TestSend_MultilineMessageStaysOneLine(t *testing.T) {
	path, lines := fakeSession(t, "", false)

	if _, err := Send(context.Background(), path, "", "line one\nline two", time.Second); err != nil {
		t.Fatal(err)
	}

	got := <-lines
	if len(got) != 1 {
		t.Fatalf("newline in the text must be escaped, got %d lines: %q", len(got), got)
	}
	msg := decode(t, got[0])["message"].(map[string]any)
	if msg["content"] != "line one\nline two" {
		t.Errorf("content = %q", msg["content"])
	}
}

func TestSend_AcceptsStatusPrefix(t *testing.T) {
	path, _ := fakeSession(t, "", false)

	if _, err := Send(context.Background(), "uds:"+path, "", "hello", time.Second); err != nil {
		t.Fatalf("path with uds: prefix should work: %v", err)
	}
}

func TestSend_ReturnsReply(t *testing.T) {
	path, _ := fakeSession(t, "ok\n", false)

	reply, err := Send(context.Background(), path, "", "hello", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "ok\n" {
		t.Errorf("reply = %q, want %q", reply, "ok\n")
	}
}

func TestSend_SilentSessionIsNotAnError(t *testing.T) {
	path, _ := fakeSession(t, "", true)

	reply, err := Send(context.Background(), path, "", "hello", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("a session that replies nothing must not be an error: %v", err)
	}
	if reply != "" {
		t.Errorf("reply = %q, want empty", reply)
	}
}

func TestSend_Errors(t *testing.T) {
	path, _ := fakeSession(t, "", false)

	tests := []struct {
		name, path, text string
	}{
		{"empty path", "", "hello"},
		{"empty message", path, "   "},
		{"no socket", "/tmp/slaude-does-not-exist.sock", "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Send(context.Background(), tt.path, "", tt.text, 100*time.Millisecond); err == nil {
				t.Error("want an error, got nil")
			}
		})
	}
}
