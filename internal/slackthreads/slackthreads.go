// Package slackthreads maps Claude sessions to the Slack threads that show them.
// The service keeps it; the daemon never sees it.
package slackthreads

import "sync"

// Table links each Claude session to its Slack thread, in both directions:
//   - session -> thread: where to post a session's events
//   - thread -> session: which session a Slack reply belongs to (step 7)
//
// A thread is identified by its thread_ts, the ts of the thread's first message.
// Safe for concurrent use.
type Table struct {
	mu                sync.Mutex
	threadBySession   map[string]string // session_id -> thread_ts
	sessionByThreadTS map[string]string // thread_ts -> session_id
}

func New() *Table {
	return &Table{threadBySession: map[string]string{}, sessionByThreadTS: map[string]string{}}
}

// ThreadForSession returns the thread_ts of the thread that shows sessionID.
func (t *Table) ThreadForSession(sessionID string) (threadTS string, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	threadTS, ok = t.threadBySession[sessionID]
	return threadTS, ok
}

// SessionForThread returns the session shown in the thread with threadTS.
func (t *Table) SessionForThread(threadTS string) (sessionID string, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	sessionID, ok = t.sessionByThreadTS[threadTS]
	return sessionID, ok
}

// Link records that sessionID is shown in the thread with threadTS.
//
// Callers must not race to create a thread for the same session: the service
// handles each daemon's events one at a time, so check (ThreadForSession) and
// create (post, then Link) never interleave for one session.
func (t *Table) Link(sessionID, threadTS string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if old, ok := t.threadBySession[sessionID]; ok {
		delete(t.sessionByThreadTS, old) // keep both directions consistent
	}
	t.threadBySession[sessionID] = threadTS
	t.sessionByThreadTS[threadTS] = sessionID
}
