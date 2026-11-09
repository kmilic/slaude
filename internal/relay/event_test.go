package relay

import (
	"strings"
	"testing"
)

// Tests for OnEvent (Claude -> Slack). Shared fakes are in relay_test.go.

func TestSessionStartCreatesThread(t *testing.T) {
	r, p, threads := newRelay()
	r.OnEvent(ctx, "U1", event("361d82a9-5e4c", "SessionStart", ""))

	if len(p.posts) != 1 {
		t.Fatalf("want 1 post, got %+v", p.posts)
	}
	got := p.posts[0]
	if got.channel != "D1" || got.threadTS != "" || got.text != "🟢 Session `361d82a9` started in `/proj`" {
		t.Errorf("first post = %+v", got)
	}
	if ts, ok := threads.ThreadForSession("361d82a9-5e4c"); !ok || ts != "1" {
		t.Errorf("thread = %q, %v; want linked to 1", ts, ok)
	}
}

func TestStopPostsAnswerInThread(t *testing.T) {
	r, p, _ := newRelay()
	r.OnEvent(ctx, "U1", event("s1", "SessionStart", ""))
	r.OnEvent(ctx, "U1", event("s1", "Stop", "All tests pass."))

	if len(p.posts) != 2 {
		t.Fatalf("want 2 posts, got %+v", p.posts)
	}
	want := post{channel: "D1", threadTS: "1", text: "All tests pass."}
	if p.posts[1] != want {
		t.Errorf("answer post = %+v, want %+v", p.posts[1], want)
	}
}

func TestOneThreadPerSession(t *testing.T) {
	r, p, _ := newRelay()
	r.OnEvent(ctx, "U1", event("s1", "SessionStart", ""))
	r.OnEvent(ctx, "U1", event("s1", "Stop", "one"))
	r.OnEvent(ctx, "U1", event("s1", "Stop", "two"))

	topLevel := 0
	for _, po := range p.posts {
		if po.threadTS == "" {
			topLevel++
		} else if po.threadTS != "1" {
			t.Errorf("post in wrong thread: %+v", po)
		}
	}
	if topLevel != 1 {
		t.Errorf("want 1 thread, got %d top-level posts", topLevel)
	}
}

func TestUnknownSessionGetsThreadFirst(t *testing.T) {
	r, p, _ := newRelay() // e.g. the daemon started after Claude: no SessionStart seen
	r.OnEvent(ctx, "U1", event("s1", "Stop", "done"))

	if len(p.posts) != 2 || p.posts[0].threadTS != "" || p.posts[1].threadTS != "1" {
		t.Fatalf("want thread then answer in it, got %+v", p.posts)
	}
}

func TestTwoSessionsTwoThreads(t *testing.T) {
	r, p, threads := newRelay()
	r.OnEvent(ctx, "U1", event("s1", "SessionStart", ""))
	r.OnEvent(ctx, "U1", event("s2", "SessionStart", ""))
	r.OnEvent(ctx, "U1", event("s2", "Stop", "for s2"))

	t2, _ := threads.ThreadForSession("s2")
	if last := p.posts[len(p.posts)-1]; last.threadTS != t2 || t2 == "1" {
		t.Errorf("s2's answer went to thread %q, s2's thread is %q", last.threadTS, t2)
	}
}

func TestSessionResumedInExistingThread(t *testing.T) {
	r, p, _ := newRelay()
	r.OnEvent(ctx, "U1", event("s1", "SessionStart", ""))
	r.OnEvent(ctx, "U1", event("s1", "SessionStart", ""))

	if len(p.posts) != 2 || p.posts[1].text != "🔄 Session resumed" || p.posts[1].threadTS != "1" {
		t.Errorf("posts = %+v", p.posts)
	}
}

func TestNotificationPrefix(t *testing.T) {
	r, p, _ := newRelay()
	r.OnEvent(ctx, "U1", event("s1", "Notification", "Claude needs your permission to use Bash"))

	if got := p.posts[len(p.posts)-1].text; got != "⚠️ Claude needs you: Claude needs your permission to use Bash" {
		t.Errorf("text = %q", got)
	}
}

func TestLongAnswerTruncated(t *testing.T) {
	r, p, _ := newRelay()
	long := strings.Repeat("é", maxAnswerRunes+10) // multi-byte rune: must not be cut in half
	r.OnEvent(ctx, "U1", event("s1", "Stop", long))

	got := p.posts[len(p.posts)-1].text
	if !strings.HasSuffix(got, "…(truncated, full answer in the terminal)") {
		t.Errorf("missing truncation note: ...%q", got[len(got)-40:])
	}
	if !strings.HasPrefix(got, strings.Repeat("é", maxAnswerRunes)+"\n") {
		t.Error("want exactly maxAnswerRunes characters before the note")
	}
}

func TestFailedThreadIsRetriedOnNextEvent(t *testing.T) {
	r, p, threads := newRelay()
	p.failNext = true
	r.OnEvent(ctx, "U1", event("s1", "SessionStart", ""))

	if _, ok := threads.ThreadForSession("s1"); ok {
		t.Fatal("a failed first post must not link a thread")
	}

	r.OnEvent(ctx, "U1", event("s1", "Stop", "done"))
	if _, ok := threads.ThreadForSession("s1"); !ok || len(p.posts) != 2 {
		t.Errorf("next event should create the thread and post; posts = %+v", p.posts)
	}
}
