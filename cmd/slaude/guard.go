package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// guardReason is shown to Claude when its SendMessage call is blocked.
const guardReason = "SendMessage is disabled in sessions watched by Slaude, so " +
	"replies stay in this session and reach the user's Slack thread. Answer here " +
	"instead. If the user asked you to message another session, tell them " +
	"SendMessage is disabled in this session."

// denyDecision is the hook output that blocks the SendMessage call and shows
// Claude guardReason.
var denyDecision = map[string]any{
	"hookSpecificOutput": map[string]string{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": guardReason,
	},
}

// guardSend runs as the PreToolUse hook for SendMessage and always blocks it.
// On an error it exits with 2, which also tells Claude Code to block the call.
func guardSend() {
	if err := guard(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "slaude guard-send:", err)
		os.Exit(2)
	}
}

// guard denies every SendMessage call: Slaude posts Claude's answer from the
// Stop hook, so the answer must stay in this session.
func guard(in io.Reader, out io.Writer) error {
	if err := json.NewEncoder(out).Encode(denyDecision); err != nil {
		return fmt.Errorf("write decision: %w", err)
	}
	if _, err := io.Copy(io.Discard, in); err != nil {
		return fmt.Errorf("read input: %w", err)
	}
	return nil
}
