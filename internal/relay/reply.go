package relay

import (
	"context"
	"errors"
	"strings"

	"slaude/internal/hub"
	"slaude/internal/protocol"
)

// Notices posted in a thread when a reply can't be delivered.
const (
	noticeUnknownThread = "This thread isn't linked to a running session, so the message wasn't sent."
	noticeOffline       = "⚠️ Your machine is offline, message not delivered."
	noticeSendFailed    = "⚠️ Couldn't deliver the message to your machine. Try again."
)

// OnReply delivers a Slack thread reply to the Claude session shown in that
// thread. When it can't, it says why in the thread.
func (r *Relay) OnReply(ctx context.Context, userID, threadTS, text string) {
	sessionID, ok := r.threads.SessionForThread(threadTS)
	if !ok {
		// An old thread (the table is in memory, so a service restart empties
		// it) or a thread the bot didn't create.
		r.postNotice(ctx, threadTS, noticeUnknownThread)
		return
	}

	msg := protocol.Message{Type: protocol.TypeDeliver, SessionID: sessionID, Text: unescapeSlack(text)}
	err := r.daemons.Send(ctx, userID, msg)
	switch {
	case err == nil:
		r.log.Info("reply sent to daemon", "user", userID, "session", sessionID)
	case errors.Is(err, hub.ErrOffline):
		r.postNotice(ctx, threadTS, noticeOffline)
	default:
		r.log.Error("send reply to daemon", "user", userID, "session", sessionID, "err", err)
		r.postNotice(ctx, threadTS, noticeSendFailed)
	}
}

func (r *Relay) postNotice(ctx context.Context, threadTS, text string) {
	if _, err := r.poster.Post(ctx, r.dmChannel, threadTS, text); err != nil {
		r.log.Error("post notice", "thread", threadTS, "err", err)
	}
}

// slackEntities undoes the only escaping Slack applies to message text.
// One pass, so a literal "&lt;" the user typed (sent as "&amp;lt;") stays "&lt;".
var slackEntities = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&")

func unescapeSlack(s string) string { return slackEntities.Replace(s) }
