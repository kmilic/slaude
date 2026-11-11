package hook

import (
	"encoding/json"
	"testing"

	"slaude/internal/protocol"
)

func TestToProtocol(t *testing.T) {
	base := func(name string) Event {
		return Event{SessionID: "s1", HookEventName: name, Cwd: "/proj"}
	}
	stop := base(EventStop)
	stop.LastAssistantMessage = "All tests pass."
	need := base(EventNotification)
	need.Message, need.NotificationType = "Claude needs your permission to use Bash", "permission_prompt"

	tests := []struct {
		name string
		in   Event
		want protocol.Message
	}{
		{"session start", base(EventSessionStart),
			protocol.Message{Type: protocol.TypeEvent, SessionID: "s1", Event: EventSessionStart, Cwd: "/proj"}},
		{"stop carries the answer", stop,
			protocol.Message{Type: protocol.TypeEvent, SessionID: "s1", Event: EventStop, Cwd: "/proj", Text: "All tests pass."}},
		{"notification carries the message", need,
			protocol.Message{Type: protocol.TypeEvent, SessionID: "s1", Event: EventNotification, Cwd: "/proj", Text: "Claude needs your permission to use Bash"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.in.ToProtocol()
			if !ok {
				t.Fatal("want ok")
			}
			if got != tt.want {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestToProtocolSkips(t *testing.T) {
	idle := Event{SessionID: "s1", HookEventName: EventNotification, NotificationType: "idle_prompt", Message: "Claude is waiting for your input"}
	other := Event{SessionID: "s1", HookEventName: "PreToolUse"}

	for name, ev := range map[string]Event{"idle reminder": idle, "unused event": other} {
		if _, ok := ev.ToProtocol(); ok {
			t.Errorf("%s: want skipped", name)
		}
	}
}

// Decodes a real Stop payload logged on 2026-11-09 (shortened), so the JSON
// field names are checked, not just the struct.
func TestToProtocolFromRealStopPayload(t *testing.T) {
	payload := `{"session_id":"abcd1234","transcript_path":"/t.jsonl","cwd":"/private/tmp/slaude-test",
		"hook_event_name":"Stop","stop_hook_active":false,"last_assistant_message":"hi","background_tasks":[]}`

	ev, err := Envelope{Payload: json.RawMessage(payload)}.Event()
	if err != nil {
		t.Fatal(err)
	}
	msg, ok := ev.ToProtocol()
	if !ok || msg.Text != "hi" || msg.SessionID != "abcd1234" || msg.Cwd != "/private/tmp/slaude-test" {
		t.Errorf("got %+v ok=%v", msg, ok)
	}
}
