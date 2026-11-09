// Package protocol defines the JSON messages the daemon and the service
// exchange over the daemon's WebSocket. The daemon's first message must be a
// valid hello, or the service closes the connection.
package protocol

// Message types.
const (
	TypeHello   = "hello"   // daemon -> service, first message: Token
	TypeEvent   = "event"   // daemon -> service: SessionID, Event, Text, Cwd
	TypeDeliver = "deliver" // service -> daemon: SessionID, Text
	TypeError   = "error"   // service -> daemon: Message
)

// Message is one WebSocket message. Only the fields of its Type are set.
type Message struct {
	Type      string `json:"type"`
	Token     string `json:"token,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Event     string `json:"event,omitempty"`
	Text      string `json:"text,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Message   string `json:"message,omitempty"`
}
