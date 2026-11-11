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

type reply struct{ userID, threadTS, text string }

// newHandler returns a handler whose OnReply records each call in replies.
func newHandler(p Poster) (*Handler, *[]reply) {
	var replies []reply
	h := &Handler{
		Poster:      p,
		BotUserID:   "UBOT",
		AllowedUser: "UME",
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		OnReply: func(_ context.Context, userID, threadTS, text string) {
			replies = append(replies, reply{userID, threadTS, text})
		},
	}
	return h, &replies
}

// userDM is a plain top-level DM from the allowed user; tests change one field at a time.
func userDM() Message {
	return Message{User: "UME", Text: "hi", Channel: "D1", ChannelType: "im"}
}

func threadReply() Message {
	m := userDM()
	m.Text, m.ThreadTS = "now add tests", "1731080000.1234"
	return m
}

func TestThreadReplyGoesToOnReply(t *testing.T) {
	p := &fakePoster{}
	h, replies := newHandler(p)

	h.OnMessage(context.Background(), threadReply())

	want := reply{userID: "UME", threadTS: "1731080000.1234", text: "now add tests"}
	if len(*replies) != 1 || (*replies)[0] != want {
		t.Fatalf("replies = %+v, want [%+v]", *replies, want)
	}
	if len(p.posts) != 0 {
		t.Errorf("a thread reply posts nothing itself, got %+v", p.posts)
	}
}

func TestBroadcastThreadReplyIsAReply(t *testing.T) {
	h, replies := newHandler(&fakePoster{})
	m := threadReply()
	m.SubType = "thread_broadcast" // "Also send to…" ticked

	h.OnMessage(context.Background(), m)

	if len(*replies) != 1 {
		t.Fatalf("want the broadcast reply routed, got %+v", *replies)
	}
}

func TestTopLevelDMGetsHint(t *testing.T) {
	p := &fakePoster{}
	h, replies := newHandler(p)

	h.OnMessage(context.Background(), userDM())

	want := post{channel: "D1", threadTS: "", text: hintText}
	if len(p.posts) != 1 || p.posts[0] != want {
		t.Fatalf("posts = %+v, want [%+v]", p.posts, want)
	}
	if len(*replies) != 0 {
		t.Errorf("a top-level DM must not reach OnReply, got %+v", *replies)
	}
}

func TestIgnored(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Message)
	}{
		{"own bot message (loop)", func(m *Message) { m.User = "UBOT" }},
		{"any bot, e.g. Claude's answer in the thread", func(m *Message) { m.BotID = "B1" }},
		{"edit", func(m *Message) { m.SubType = "message_changed" }},
		{"deletion", func(m *Message) { m.SubType = "message_deleted" }},
		{"not a DM", func(m *Message) { m.ChannelType = "channel" }},
		{"other user", func(m *Message) { m.User = "USOMEONE" }},
		{"no user", func(m *Message) { m.User = "" }},
	}
	for _, tt := range tests {
		for _, base := range []Message{userDM(), threadReply()} {
			t.Run(tt.name, func(t *testing.T) {
				p := &fakePoster{}
				h, replies := newHandler(p)
				m := base
				tt.change(&m)

				h.OnMessage(context.Background(), m)

				if len(p.posts) != 0 || len(*replies) != 0 {
					t.Fatalf("want ignored, got posts %+v, replies %+v", p.posts, *replies)
				}
			})
		}
	}
}

func TestReplyWithoutOnReplyDoesNotPanic(t *testing.T) {
	h, _ := newHandler(&fakePoster{})
	h.OnReply = nil

	h.OnMessage(context.Background(), threadReply()) // logged, not a panic
}

func TestHintPostErrorDoesNotPanic(t *testing.T) {
	p := &fakePoster{err: errors.New("slack down")}
	h, _ := newHandler(p)

	h.OnMessage(context.Background(), userDM()) // logged, not returned
	if len(p.posts) != 1 {
		t.Fatalf("want one attempt, got %d", len(p.posts))
	}
}
