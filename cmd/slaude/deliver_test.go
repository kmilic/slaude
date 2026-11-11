package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"slaude/internal/protocol"
)

type inboxCall struct{ path, text string }

// fakeInbox records each call and returns err.
func fakeInbox(calls *[]inboxCall, err error) inboxSender {
	return func(_ context.Context, path, text string) error {
		*calls = append(*calls, inboxCall{path, text})
		return err
	}
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func deliverMsg(session, text string) protocol.Message {
	return protocol.Message{Type: protocol.TypeDeliver, SessionID: session, Text: text}
}

func TestDeliverToKnownSession(t *testing.T) {
	sessions := &sessionTable{inbox: map[string]string{}}
	sessions.set("s1", "/tmp/cc-socks/1.sock")
	sessions.set("s2", "/tmp/cc-socks/2.sock")
	var calls []inboxCall

	deliver(context.Background(), sessions, deliverMsg("s2", "now add tests"), fakeInbox(&calls, nil), discard())

	want := inboxCall{path: "/tmp/cc-socks/2.sock", text: slackPrompt("now add tests")}
	if len(calls) != 1 || calls[0] != want {
		t.Fatalf("calls = %+v, want [%+v]", calls, want)
	}
}

// The note must come first and the user's text must arrive unchanged, so
// Claude answers in its own turn instead of messaging another session.
func TestSlackPromptTellsClaudeToAnswerHere(t *testing.T) {
	got := slackPrompt("what's the weather?")

	if !strings.HasPrefix(got, "[Slaude] The user sent this from Slack.") {
		t.Errorf("note must come first, got %q", got)
	}
	if !strings.Contains(got, "Don't use SendMessage") {
		t.Errorf("note must tell Claude not to use SendMessage, got %q", got)
	}
	if !strings.HasSuffix(got, "\n\nwhat's the weather?") {
		t.Errorf("user's text must follow unchanged, got %q", got)
	}
}

func TestDeliverToUnknownSessionSendsNothing(t *testing.T) {
	sessions := &sessionTable{inbox: map[string]string{}}
	var calls []inboxCall

	deliver(context.Background(), sessions, deliverMsg("nope", "hi"), fakeInbox(&calls, nil), discard())

	if len(calls) != 0 {
		t.Errorf("want no inbox write, got %+v", calls)
	}
}

func TestDeliverErrorDoesNotPanic(t *testing.T) {
	sessions := &sessionTable{inbox: map[string]string{}}
	sessions.set("s1", "/tmp/cc-socks/1.sock")
	var calls []inboxCall

	deliver(context.Background(), sessions, deliverMsg("s1", "hi"), fakeInbox(&calls, errors.New("connection refused")), discard())

	if len(calls) != 1 {
		t.Errorf("want one attempt, got %d", len(calls))
	}
}

func TestSessionTableKeepsKnownPathWhenEmpty(t *testing.T) {
	sessions := &sessionTable{inbox: map[string]string{}}
	sessions.set("s1", "/tmp/cc-socks/1.sock")
	sessions.set("s1", "") // a hook run without CLAUDE_CODE_MESSAGING_SOCKET

	if p, _ := sessions.get("s1"); p != "/tmp/cc-socks/1.sock" {
		t.Errorf("path = %q, want the known path kept", p)
	}
}
