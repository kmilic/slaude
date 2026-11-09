package hook

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// socketPath returns a short path: macOS caps unix socket paths at ~104 bytes.
func socketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "slaude")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "d.sock")
}

// startListener runs Listen and sends every envelope it handles to the returned channel.
func startListener(t *testing.T, path string) <-chan Envelope {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	got := make(chan Envelope, 10)
	go Listen(ctx, path, func(e Envelope) { got <- e }, discard())

	deadline := time.Now().Add(2 * time.Second)
	for {
		if c, err := net.Dial("unix", path); err == nil {
			c.Close() // the listener logs and ignores this empty connection
			return got
		}
		if time.Now().After(deadline) {
			t.Fatal("listener did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func receive(t *testing.T, got <-chan Envelope) Envelope {
	t.Helper()
	select {
	case e := <-got:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("no envelope received")
		return Envelope{}
	}
}

func TestForwardReachesListener(t *testing.T) {
	path := socketPath(t)
	got := startListener(t, path)

	payload := `{"session_id":"abc","hook_event_name":"Stop","cwd":"/proj","transcript_path":"/t.jsonl","extra":1}`
	sent := Envelope{InboxSocket: "/tmp/cc-socks/1.sock", Payload: json.RawMessage(payload)}
	if err := Forward(path, sent, time.Second); err != nil {
		t.Fatal(err)
	}

	env := receive(t, got)
	if env.InboxSocket != sent.InboxSocket {
		t.Errorf("inbox socket = %q", env.InboxSocket)
	}
	ev, err := env.Event()
	if err != nil {
		t.Fatal(err)
	}
	want := Event{SessionID: "abc", HookEventName: "Stop", Cwd: "/proj", TranscriptPath: "/t.jsonl"}
	if ev != want {
		t.Errorf("event = %+v, want %+v", ev, want)
	}
}

func TestForwardFailsFastWithoutDaemon(t *testing.T) {
	start := time.Now()
	err := Forward(filepath.Join(t.TempDir(), "none.sock"), Envelope{Payload: json.RawMessage(`{}`)}, 500*time.Millisecond)
	if err == nil {
		t.Fatal("want an error when no daemon is listening")
	}
	if time.Since(start) > time.Second {
		t.Errorf("Forward took %v, must give up quickly", time.Since(start))
	}
}

func TestListenerSurvivesBadMessage(t *testing.T) {
	path := socketPath(t)
	got := startListener(t, path)

	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	c.Write([]byte("not json\n"))
	c.Close()

	// A good message after the bad one still arrives.
	if err := Forward(path, Envelope{Payload: json.RawMessage(`{"session_id":"s","hook_event_name":"Stop"}`)}, time.Second); err != nil {
		t.Fatal(err)
	}
	if ev, _ := receive(t, got).Event(); ev.SessionID != "s" {
		t.Errorf("session = %q, want s", ev.SessionID)
	}
}

func TestEventRequiresSessionAndName(t *testing.T) {
	for _, payload := range []string{`{}`, `{"session_id":"s"}`, `{"hook_event_name":"Stop"}`, `not json`} {
		if _, err := (Envelope{Payload: json.RawMessage(payload)}).Event(); err == nil {
			t.Errorf("payload %s: want an error", payload)
		}
	}
}
