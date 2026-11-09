// Package relay translates between Claude sessions and Slack threads. It is the
// only place that decides which thread an event goes to and which session a
// reply goes to. The hub knows only daemons; slackbot knows only Slack.
//
//   - event.go: OnEvent, Claude -> Slack
//   - reply.go: OnReply, Slack -> Claude
package relay

import (
	"context"
	"log/slog"

	"slaude/internal/protocol"
	"slaude/internal/slackthreads"
)

// Poster posts a message to Slack and returns its ts. threadTS may be empty
// for a top-level message.
type Poster interface {
	Post(ctx context.Context, channel, threadTS, text string) (ts string, err error)
}

// Sender sends a message to a user's daemon. *hub.Hub implements it; it
// returns hub.ErrOffline when no daemon is connected.
type Sender interface {
	Send(ctx context.Context, userID string, msg protocol.Message) error
}

// Relay posts Claude session events to Slack threads, and Slack thread
// replies to Claude sessions.
type Relay struct {
	poster    Poster
	daemons   Sender
	threads   *slackthreads.Table
	dmChannel string // POC: the one user's DM with the bot. Later: userID -> channel.
	log       *slog.Logger
}

func New(poster Poster, daemons Sender, threads *slackthreads.Table, dmChannel string, log *slog.Logger) *Relay {
	return &Relay{poster: poster, daemons: daemons, threads: threads, dmChannel: dmChannel, log: log}
}
