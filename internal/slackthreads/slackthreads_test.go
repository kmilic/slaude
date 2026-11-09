package slackthreads

import (
	"fmt"
	"sync"
	"testing"
)

func TestLinkAndLookupBothWays(t *testing.T) {
	tb := New()
	tb.Link("s1", "100.1")

	if ts, ok := tb.ThreadForSession("s1"); !ok || ts != "100.1" {
		t.Errorf("ThreadForSession(s1) = %q, %v", ts, ok)
	}
	if id, ok := tb.SessionForThread("100.1"); !ok || id != "s1" {
		t.Errorf("SessionForThread(100.1) = %q, %v", id, ok)
	}
}

func TestUnknown(t *testing.T) {
	tb := New()
	if _, ok := tb.ThreadForSession("nope"); ok {
		t.Error("unknown session must not be found")
	}
	if _, ok := tb.SessionForThread("nope"); ok {
		t.Error("unknown thread must not be found")
	}
}

func TestTwoSessionsTwoThreads(t *testing.T) {
	tb := New()
	tb.Link("s1", "100.1")
	tb.Link("s2", "200.2")

	if id, _ := tb.SessionForThread("200.2"); id != "s2" {
		t.Errorf("SessionForThread(200.2) = %q, want s2", id)
	}
	if ts, _ := tb.ThreadForSession("s1"); ts != "100.1" {
		t.Errorf("ThreadForSession(s1) = %q, want 100.1", ts)
	}
}

func TestRelinkRemovesOldThread(t *testing.T) {
	tb := New()
	tb.Link("s1", "100.1")
	tb.Link("s1", "300.3")

	if _, ok := tb.SessionForThread("100.1"); ok {
		t.Error("old thread must no longer map to the session")
	}
	if id, _ := tb.SessionForThread("300.3"); id != "s1" {
		t.Errorf("SessionForThread(300.3) = %q, want s1", id)
	}
}

// Run with -race: many goroutines reading and writing at once.
func TestConcurrentUse(t *testing.T) {
	tb := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, ts := fmt.Sprintf("s%d", i), fmt.Sprintf("%d.0", i)
			tb.Link(id, ts)
			tb.ThreadForSession(id)
			tb.SessionForThread(ts)
		}(i)
	}
	wg.Wait()

	for i := 0; i < 50; i++ {
		if ts, _ := tb.ThreadForSession(fmt.Sprintf("s%d", i)); ts != fmt.Sprintf("%d.0", i) {
			t.Fatalf("s%d -> %q", i, ts)
		}
	}
}
