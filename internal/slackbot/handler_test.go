package slackbot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

type post struct{ channel, threadTS, text string }

type fakePoster struct {
	posts []post
	err   error
}

func (f *fakePoster) Post(_ context.Context, channel, threadTS, text string) (string, error) {
	f.posts = append(f.posts, post{channel, threadTS, text})
	return "1.0", f.err
}

func newHandler(p Poster) *Handler {
	return &Handler{
		Poster:      p,
		BotUserID:   "UBOT",
		AllowedUser: "UME",
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// userDM is a plain DM from the allowed user; tests change one field at a time.
func userDM() Message {
	return Message{User: "UME", Text: "hi", Channel: "D1", ChannelType: "im"}
}

func TestOnMessage_EchoesUserDM(t *testing.T) {
	p := &fakePoster{}
	newHandler(p).OnMessage(context.Background(), userDM())

	want := post{channel: "D1", threadTS: "", text: "echo: hi"}
	if len(p.posts) != 1 || p.posts[0] != want {
		t.Fatalf("posts = %+v, want [%+v]", p.posts, want)
	}
}

func TestOnMessage_ThreadReplyEchoesInThread(t *testing.T) {
	p := &fakePoster{}
	m := userDM()
	m.ThreadTS = "1731080000.1234"
	newHandler(p).OnMessage(context.Background(), m)

	if len(p.posts) != 1 || p.posts[0].threadTS != "1731080000.1234" {
		t.Fatalf("posts = %+v, want one post in the thread", p.posts)
	}
}

func TestOnMessage_Ignored(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Message)
	}{
		{"own bot message (echo loop)", func(m *Message) { m.User = "UBOT" }},
		{"any bot", func(m *Message) { m.BotID = "B1" }},
		{"edit", func(m *Message) { m.SubType = "message_changed" }},
		{"not a DM", func(m *Message) { m.ChannelType = "channel" }},
		{"other user", func(m *Message) { m.User = "USOMEONE" }},
		{"no user", func(m *Message) { m.User = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &fakePoster{}
			m := userDM()
			tt.change(&m)
			newHandler(p).OnMessage(context.Background(), m)
			if len(p.posts) != 0 {
				t.Fatalf("want no post, got %+v", p.posts)
			}
		})
	}
}

func TestOnMessage_PostErrorDoesNotPanic(t *testing.T) {
	p := &fakePoster{err: errors.New("slack down")}
	newHandler(p).OnMessage(context.Background(), userDM()) // logged, not returned
	if len(p.posts) != 1 {
		t.Fatalf("want one attempt, got %d", len(p.posts))
	}
}
