package relay

import (
	"errors"
	"fmt"
	"testing"

	"slaude/internal/hub"
	"slaude/internal/protocol"
)

func TestReplyDeliveredToSession(t *testing.T) {
	r, p, threads, s := newRelayWithSender()
	threads.Link("s1", "100.1")

	r.OnReply(ctx, "U1", "100.1", "now add tests")

	want := sentMsg{userID: "U1", msg: protocol.Message{Type: protocol.TypeDeliver, SessionID: "s1", Text: "now add tests"}}
	if len(s.sent) != 1 || s.sent[0] != want {
		t.Fatalf("sent = %+v, want [%+v]", s.sent, want)
	}
	if len(p.posts) != 0 {
		t.Errorf("a delivered reply posts nothing, got %+v", p.posts)
	}
}

func TestReplyGoesToTheThreadsOwnSession(t *testing.T) {
	r, _, threads, s := newRelayWithSender()
	threads.Link("s1", "100.1")
	threads.Link("s2", "200.2")

	r.OnReply(ctx, "U1", "200.2", "for s2")

	if len(s.sent) != 1 || s.sent[0].msg.SessionID != "s2" {
		t.Errorf("sent = %+v, want session s2", s.sent)
	}
}

func TestReplyInUnknownThread(t *testing.T) {
	r, p, _, s := newRelayWithSender()

	r.OnReply(ctx, "U1", "999.9", "hello?")

	if len(s.sent) != 0 {
		t.Errorf("nothing must be sent, got %+v", s.sent)
	}
	if len(p.posts) != 1 || p.posts[0] != (post{"D1", "999.9", noticeUnknownThread}) {
		t.Errorf("posts = %+v, want the unknown-thread notice in the thread", p.posts)
	}
}

func TestReplyWhenDaemonOffline(t *testing.T) {
	r, p, threads, s := newRelayWithSender()
	threads.Link("s1", "100.1")
	s.err = fmt.Errorf("wrapped: %w", hub.ErrOffline) // errors.Is must see through wrapping

	r.OnReply(ctx, "U1", "100.1", "hi")

	if len(p.posts) != 1 || p.posts[0] != (post{"D1", "100.1", noticeOffline}) {
		t.Errorf("posts = %+v, want the offline notice in the thread", p.posts)
	}
}

func TestReplySendFails(t *testing.T) {
	r, p, threads, s := newRelayWithSender()
	threads.Link("s1", "100.1")
	s.err = errors.New("connection reset")

	r.OnReply(ctx, "U1", "100.1", "hi")

	if len(p.posts) != 1 || p.posts[0].text != noticeSendFailed {
		t.Errorf("posts = %+v, want the send-failed notice", p.posts)
	}
}

func TestReplyUnescapesSlackEntities(t *testing.T) {
	tests := []struct{ in, want string }{
		{"use a &lt; b &amp;&amp; c &gt; d", "use a < b && c > d"},
		{"&amp;lt; stays literal", "&lt; stays literal"}, // the user typed "&lt;"
		{"no entities", "no entities"},
	}
	for _, tt := range tests {
		r, _, threads, s := newRelayWithSender()
		threads.Link("s1", "100.1")

		r.OnReply(ctx, "U1", "100.1", tt.in)

		if len(s.sent) != 1 || s.sent[0].msg.Text != tt.want {
			t.Errorf("in %q: sent %+v, want text %q", tt.in, s.sent, tt.want)
		}
	}
}
