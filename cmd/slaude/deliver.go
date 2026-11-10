package main

import (
	"context"
	"log/slog"
	"time"

	"slaude/internal/protocol"
)

const (
	// deliverTimeout bounds one write into a session's inbox socket.
	deliverTimeout = 10 * time.Second
	// inboxReplyWait is how long to wait for anything the session writes back.
	// It sends nothing, so keep it short: deliveries run one at a time.
	inboxReplyWait = 100 * time.Millisecond
)

// slackNote goes before every reply delivered into a session. Claude Code shows
// inbox messages as coming from "another Claude session", and without this
// note Claude answered that "session" with SendMessage, which delivered the
// answer to an unrelated session instead of the Slack thread. Slaude only
// posts Claude's final message of the turn (Stop hook), so the answer must be there.
const slackNote = "[Slaude] The user sent this from Slack. Answer here, in this session: " +
	"your final message is posted back to their Slack thread. " +
	"Don't use SendMessage to reply; no Claude session sent this.\n\n"

// slackPrompt is the text written into the session for a Slack reply.
func slackPrompt(text string) string { return slackNote + text }

// inboxSender writes text into the inbox socket at path: inbox.Send in the
// daemon, a fake in tests.
type inboxSender func(ctx context.Context, path, token, text string, replyWait time.Duration) (string, error)

// deliver writes a deliver message from the service into the Claude session it
// names. The reader loop calls it one message at a time, so replies reach a
// session in the order they were sent.
//
// No auth token is sent: the auth line is optional on macOS and Linux, and the
// message arrives as a peer message either way.
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
	if _, err := send(ctx, path, "", slackPrompt(msg.Text), inboxReplyWait); err != nil {
		log.Error("reply not delivered", "session", msg.SessionID, "inbox", path, "err", err)
		return
	}
	log.Info("reply delivered to session", "session", msg.SessionID, "inbox", path)
}
