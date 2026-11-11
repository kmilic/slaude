// Package slackbot decides what the Slaude bot does with incoming Slack
// messages. It knows only Slack: a reply in a thread is handed to OnReply, which
// main wires to the relay. Tested with a fake Poster and a fake OnReply.
package slackbot

import (
	"context"
	"log/slog"
)

// subtypeThreadBroadcast marks a thread reply the user also sent to the main
// conversation ("Also send to…"). It is still a reply.
const subtypeThreadBroadcast = "thread_broadcast"

// hintText answers a top-level DM: only thread replies reach a session.
const hintText = "👋 Reply in a session's thread to send a message to Claude."

// Message is the part of a Slack message event the bot acts on.
type Message struct {
	User        string
	Text        string
	Channel     string
	ChannelType string // "im" for a DM with the bot
	ThreadTS    string // set when the message is a reply in a thread
	SubType     string // set for edits, deletions, bot messages, ...
	BotID       string // set when a bot wrote the message
}

// Poster posts a message to Slack and returns its ts. threadTS may be empty for
// a top-level message.
type Poster interface {
	Post(ctx context.Context, channel, threadTS, text string) (ts string, err error)
}

// Handler accepts DMs from the allowed user. A reply in a thread goes to
// OnReply; a top-level DM gets a hint.
type Handler struct {
	Poster      Poster
	BotUserID   string // the bot's own user ID, so it never answers itself
	AllowedUser string // the only Slack user who may talk to the bot
	Log         *slog.Logger

	// OnReply is called for each accepted thread reply. main sets it to
	// relay.OnReply, so slackbot never needs to know about sessions or daemons.
	OnReply func(ctx context.Context, userID, threadTS, text string)
}

// OnMessage handles one message event. Errors are logged, never returned, so
// one failed post doesn't stop the event loop.
func (h *Handler) OnMessage(ctx context.Context, m Message) {
	if !h.accept(m) {
		return
	}
	if m.ThreadTS != "" {
		if h.OnReply == nil {
			h.Log.Error("thread reply dropped: OnReply not set", "thread", m.ThreadTS)
			return
		}
		h.OnReply(ctx, m.User, m.ThreadTS, m.Text)
		return
	}
	if _, err := h.Poster.Post(ctx, m.Channel, "", hintText); err != nil {
		h.Log.Error("post hint", "channel", m.Channel, "err", err)
	}
}

func (h *Handler) accept(m Message) bool {
	switch {
	case m.ChannelType != "im":
		return false // only DMs with the bot
	case m.SubType != "" && m.SubType != subtypeThreadBroadcast:
		return false // edits, deletions and bot messages are not new user messages
	case m.BotID != "", m.User == "", m.User == h.BotUserID:
		return false // never answer a bot, including ourselves (loop)
	case m.User != h.AllowedUser:
		h.Log.Warn("ignored message from user not allowed", "user", m.User)
		return false
	}
	return true
}
