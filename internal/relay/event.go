package relay

import (
	"context"
	"fmt"

	"slaude/internal/protocol"
)

// maxAnswerRunes keeps long answers readable on a phone. Claude's full answer
// stays in the terminal.
const maxAnswerRunes = 3000

// OnEvent posts one session event to the session's thread, creating the thread
// on the session's first event. The hub calls it one event at a time per
// daemon, so two events can't both create a thread for the same session.
// userID is unused in the POC (one user, one DM channel).
func (r *Relay) OnEvent(ctx context.Context, userID string, msg protocol.Message) {
	threadTS, existed := r.threads.ThreadForSession(msg.SessionID)
	if !existed {
		ts, err := r.poster.Post(ctx, r.dmChannel, "", startedText(msg))
		if err != nil {
			// Not linked, so the session's next event tries again.
			r.log.Error("create session thread", "session", msg.SessionID, "err", err)
			return
		}
		r.threads.Link(msg.SessionID, ts)
		threadTS = ts
	}

	text, ok := eventText(msg, existed)
	if !ok {
		return
	}
	if _, err := r.poster.Post(ctx, r.dmChannel, threadTS, text); err != nil {
		r.log.Error("post event", "session", msg.SessionID, "event", msg.Event, "err", err)
	}
}

// startedText is the thread's first message. The short session ID tells apart
// two sessions in the same folder.
func startedText(msg protocol.Message) string {
	if msg.Cwd == "" {
		return fmt.Sprintf("🟢 Session `%s` started", shortID(msg.SessionID))
	}
	return fmt.Sprintf("🟢 Session `%s` started in `%s`", shortID(msg.SessionID), msg.Cwd)
}

// eventText is what to post in the thread for msg. ok is false when there's
// nothing to add.
func eventText(msg protocol.Message, threadExisted bool) (text string, ok bool) {
	switch msg.Event {
	case "SessionStart":
		if threadExisted {
			return "🔄 Session resumed", true
		}
		return "", false // the thread's first message already says it started
	case "Stop":
		if msg.Text == "" {
			return "✅ Claude finished (no text answer)", true
		}
		return truncate(msg.Text, maxAnswerRunes), true
	case "Notification":
		return "⚠️ Claude needs you: " + msg.Text, true
	default:
		return "", false
	}
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// truncate cuts s to max runes (not bytes, so multi-byte characters stay whole).
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "\n…(truncated, full answer in the terminal)"
}
