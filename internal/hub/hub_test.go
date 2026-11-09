package hub

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"slaude/internal/protocol"
)

const (
	token = "secret-token"
	user  = "UME"
)

func startHub(t *testing.T) (*Hub, string) {
	t.Helper()
	h := New(token, user, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return h, "ws" + strings.TrimPrefix(srv.URL, "http")
}

func dialHello(t *testing.T, url, tok string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	if err := wsjson.Write(ctx, conn, protocol.Message{Type: protocol.TypeHello, Token: tok}); err != nil {
		t.Fatal(err)
	}
	return conn
}

// eventually polls cond, because registration happens on the server goroutine.
func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestValidHelloRegistersDaemon(t *testing.T) {
	h, url := startHub(t)
	dialHello(t, url, token)

	eventually(t, func() bool { return h.Connected(user) })
}

func TestInvalidTokenIsRejected(t *testing.T) {
	h, url := startHub(t)
	conn := dialHello(t, url, "wrong")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var msg protocol.Message
	if err := wsjson.Read(ctx, conn, &msg); err != nil {
		t.Fatalf("want an error message before close, got %v", err)
	}
	if msg.Type != protocol.TypeError {
		t.Errorf("type = %q, want error", msg.Type)
	}
	// Then the server closes the connection.
	if err := wsjson.Read(ctx, conn, &msg); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Errorf("want close with policy violation, got %v", err)
	}
	if h.Connected(user) {
		t.Error("rejected daemon must not be registered")
	}
}

func TestFirstMessageMustBeHello(t *testing.T) {
	h, url := startHub(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_ = wsjson.Write(ctx, conn, protocol.Message{Type: protocol.TypeEvent, Token: token})

	var msg protocol.Message
	_ = wsjson.Read(ctx, conn, &msg) // the error message
	if err := wsjson.Read(ctx, conn, &msg); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Errorf("want close with policy violation, got %v", err)
	}
	if h.Connected(user) {
		t.Error("must not register without a hello")
	}
}

func TestNewConnectionReplacesOld(t *testing.T) {
	h, url := startHub(t)
	first := dialHello(t, url, token)
	eventually(t, func() bool { return h.Connected(user) })

	dialHello(t, url, token)

	// The first connection is closed by the server.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var msg protocol.Message
	if err := wsjson.Read(ctx, first, &msg); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Errorf("want the old connection closed normally, got %v", err)
	}
	// The user stays connected through the new one.
	eventually(t, func() bool { return h.Connected(user) })
}

func TestEventsReachOnEventInOrder(t *testing.T) {
	h, url := startHub(t)
	got := make(chan protocol.Message, 10)
	h.OnEvent = func(_ context.Context, userID string, msg protocol.Message) {
		if userID != user {
			t.Errorf("userID = %q, want %q", userID, user)
		}
		got <- msg
	}
	conn := dialHello(t, url, token)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, name := range []string{"SessionStart", "Stop"} {
		if err := wsjson.Write(ctx, conn, protocol.Message{Type: protocol.TypeEvent, SessionID: "s1", Event: name}); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"SessionStart", "Stop"} {
		select {
		case msg := <-got:
			if msg.Event != want {
				t.Errorf("event = %q, want %q (order)", msg.Event, want)
			}
		case <-ctx.Done():
			t.Fatalf("event %s not delivered", want)
		}
	}
}

func TestSendReachesDaemon(t *testing.T) {
	h, url := startHub(t)
	conn := dialHello(t, url, token)
	eventually(t, func() bool { return h.Connected(user) })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sent := protocol.Message{Type: protocol.TypeDeliver, SessionID: "s1", Text: "now add tests"}
	if err := h.Send(ctx, user, sent); err != nil {
		t.Fatal(err)
	}

	var got protocol.Message
	if err := wsjson.Read(ctx, conn, &got); err != nil {
		t.Fatal(err)
	}
	if got != sent {
		t.Errorf("daemon got %+v, want %+v", got, sent)
	}
}

func TestSendWithoutDaemonIsOffline(t *testing.T) {
	h, _ := startHub(t)

	err := h.Send(context.Background(), user, protocol.Message{Type: protocol.TypeDeliver})
	if !errors.Is(err, ErrOffline) {
		t.Errorf("err = %v, want ErrOffline", err)
	}
}

func TestSendAfterDisconnectIsOffline(t *testing.T) {
	h, url := startHub(t)
	conn := dialHello(t, url, token)
	eventually(t, func() bool { return h.Connected(user) })

	conn.Close(websocket.StatusNormalClosure, "bye")
	eventually(t, func() bool { return !h.Connected(user) })

	err := h.Send(context.Background(), user, protocol.Message{Type: protocol.TypeDeliver})
	if !errors.Is(err, ErrOffline) {
		t.Errorf("err = %v, want ErrOffline", err)
	}
}

func TestDisconnectUnregisters(t *testing.T) {
	h, url := startHub(t)
	conn := dialHello(t, url, token)
	eventually(t, func() bool { return h.Connected(user) })

	conn.Close(websocket.StatusNormalClosure, "bye")

	eventually(t, func() bool { return !h.Connected(user) })
}
