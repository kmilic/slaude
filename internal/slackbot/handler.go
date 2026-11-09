// Package slackbot decides what the Slaude bot does with incoming Slack
// messages. It has no Slack dependency, so it can be tested with a fake Poster.
package slackbot

import (
	"context"
	"log/slog"
)

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

// Handler echoes DMs from the allowed user. In later steps, this is where a
// thread reply is routed to the user's daemon instead.
type Handler struct {
	Poster      Poster
	BotUserID   string // the bot's own user ID, so it never answers itself
	AllowedUser string // the only Slack user who may talk to the bot
	Log         *slog.Logger
}

// OnMessage handles one message event. Errors are logged, never returned, so
// one failed post doesn't stop the event loop.
func (h *Handler) OnMessage(ctx context.Context, m Message) {
	if !h.accept(m) {
		return
	}
	if _, err := h.Poster.Post(ctx, m.Channel, m.ThreadTS, "echo: "+m.Text); err != nil {
		h.Log.Error("post echo", "channel", m.Channel, "err", err)
	}
}

func (h *Handler) accept(m Message) bool {
	switch {
	case m.ChannelType != "im":
		return false // only DMs with the bot
	case m.SubType != "":
		return false // edits, deletions and bot messages are not new user messages
	case m.BotID != "", m.User == "", m.User == h.BotUserID:
		return false // never answer a bot, including ourselves (echo loop)
	case m.User != h.AllowedUser:
		h.Log.Warn("ignored message from user not allowed", "user", m.User)
		return false
	}
	return true
}
