package main

import (
	"context"
	"log/slog"
	"time"

	"slaude/internal/inbox"
	"slaude/internal/protocol"
)

const (
	// deliverTimeout is how long sending one message to a session may take.
	deliverTimeout = 10 * time.Second
	// inboxReplyWait is how long to wait for anything the session writes back.
	inboxReplyWait = 100 * time.Millisecond
)

// slackNote goes before each Slack reply, to direct Claude to answer in the session.
const slackNote = "[Slaude] The user sent this from Slack. Answer here, in this session: " +
	"your final message is posted back to their Slack thread. " +
	"Don't use SendMessage to reply; no Claude session sent this.\n\n"

func slackPrompt(text string) string { return slackNote + text }

// inboxSender writes text into the inbox socket at path.
type inboxSender func(ctx context.Context, path, text string) error

// sendToInbox wraps inbox.Send, so tests can pass a fake.
func sendToInbox(ctx context.Context, path, text string) error {
	_, err := inbox.Send(ctx, path, text, inboxReplyWait)
	return err
}

// deliver writes a Slack reply into its Claude session, one at a time to keep the order.
func deliver(ctx context.Context, sessions *sessionTable, msg protocol.Message, send inboxSender, log *slog.Logger) {
	path, ok := sessions.get(msg.SessionID)
	if !ok {
		// e.g. the daemon restarted and lost its table.
		log.Warn("reply not delivered: unknown session", "session", msg.SessionID)
		return
	}

	ctx, cancel := context.WithTimeout(ctx, deliverTimeout)
	defer cancel()
	if err := send(ctx, path, slackPrompt(msg.Text)); err != nil {
		log.Error("reply not delivered", "session", msg.SessionID, "inbox", path, "err", err)
		return
	}
	log.Info("reply delivered to session", "session", msg.SessionID, "inbox", path)
}
