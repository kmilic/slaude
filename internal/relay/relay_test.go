package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"slaude/internal/protocol"
	"slaude/internal/slackthreads"
)

// Shared fakes and helpers for the relay tests: OnEvent tests are in
// event_test.go, OnReply tests in reply_test.go.

type post struct{ channel, threadTS, text string }

// fakePoster records posts and returns ts "1", "2", ... like Slack would.
// failNext makes the next post fail.
type fakePoster struct {
	posts    []post
	failNext bool
}

func (f *fakePoster) Post(_ context.Context, channel, threadTS, text string) (string, error) {
	if f.failNext {
		f.failNext = false
		return "", errors.New("slack down")
	}
	f.posts = append(f.posts, post{channel, threadTS, text})
	return fmt.Sprint(len(f.posts)), nil
}

// fakeSender records what the relay sends to daemons; err is returned by Send.
type fakeSender struct {
	sent []sentMsg
	err  error
}

type sentMsg struct {
	userID string
	msg    protocol.Message
}

func (f *fakeSender) Send(_ context.Context, userID string, msg protocol.Message) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, sentMsg{userID, msg})
	return nil
}

func newRelay() (*Relay, *fakePoster, *slackthreads.Table) {
	r, p, threads, _ := newRelayWithSender()
	return r, p, threads
}

func newRelayWithSender() (*Relay, *fakePoster, *slackthreads.Table, *fakeSender) {
	p := &fakePoster{}
	s := &fakeSender{}
	threads := slackthreads.New()
	return New(p, s, threads, "D1", slog.New(slog.NewTextHandler(io.Discard, nil))), p, threads, s
}

func event(session, name, text string) protocol.Message {
	return protocol.Message{Type: protocol.TypeEvent, SessionID: session, Event: name, Text: text, Cwd: "/proj"}
}

var ctx = context.Background()
