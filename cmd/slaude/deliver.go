package main

import (
	"context"
	"log/slog"
	"time"

	"slaude/internal/inbox"
	"slaude/internal/protocol"
)

const (
	// deliverTimeout bounds one write into a session's inbox socket.
	deliverTimeout = 10 * time.Second
	// inboxReplyWait is how long to wait for anything the session writes back.
	// It sends nothing, so keep it short: deliveries run one at a time.
	inboxReplyWait = 100 * time.Millisecond
)

// slackNote goes before each Slack reply, so Claude answers in the session (posted by the Stop hook), not with SendMessage.
const slackNote = "[Slaude] The user sent this from Slack. Answer here, in this session: " +
	"your final message is posted back to their Slack thread. " +
	"Don't use SendMessage to reply; no Claude session sent this.\n\n"

func slackPrompt(text string) string { return slackNote + text }

// inboxSender writes text into the inbox socket at path: sendToInbox in the
// daemon, a fake in tests.
type inboxSender func(ctx context.Context, path, text string) error

// sendToInbox adapts inbox.Send to inboxSender, so tests can pass a fake.
func sendToInbox(ctx context.Context, path, text string) error {
	_, err := inbox.Send(ctx, path, text, inboxReplyWait)
	return err
}

// deliver writes a deliver message from the service into the Claude session it
// names. The reader loop calls it one message at a time, so replies reach a
// session in the order they were sent.
func deliver(ctx context.Context, sessions *sessionTable, msg protocol.Message, send inboxSender, log *slog.Logger) {
	path, ok := sessions.get(msg.SessionID)
	if !ok {
		// e.g. the daemon restarted and lost its table. The session's next hook
		// event tells the daemon its inbox socket again.
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
