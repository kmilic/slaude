package hook

import "slaude/internal/protocol"

// Hook event names the daemon forwards.
const (
	EventSessionStart = "SessionStart"
	EventStop         = "Stop"
	EventNotification = "Notification"
)

// notificationIdle is the "Claude is waiting for your input" reminder that
// fires ~60 s after every Stop. Posting it would duplicate every answer.
const notificationIdle = "idle_prompt"

// ToProtocol turns a hook event into the event message the daemon sends to
// the service. ok is false for events that shouldn't reach Slack.
func (e Event) ToProtocol() (msg protocol.Message, ok bool) {
	msg = protocol.Message{
		Type:      protocol.TypeEvent,
		SessionID: e.SessionID,
		Event:     e.HookEventName,
		Cwd:       e.Cwd, // lets the service open a thread for a session it hasn't seen
	}
	switch e.HookEventName {
	case EventSessionStart:
		return msg, true
	case EventStop:
		msg.Text = e.LastAssistantMessage
		return msg, true
	case EventNotification:
		if e.NotificationType == notificationIdle {
			return protocol.Message{}, false
		}
		msg.Text = e.Message
		return msg, true
	default:
		return protocol.Message{}, false // events Slaude doesn't use
	}
}
