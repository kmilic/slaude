package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// debugHooksEnv turns on the raw payload logs (hooks.log in the daemon,
// guard.log in guard-send). Off by default: the payloads contain Claude's
// answers and message text, and the files grow without limit.
const debugHooksEnv = "SLAUDE_DEBUG_HOOKS"

func debugHooks() bool { return os.Getenv(debugHooksEnv) == "1" }

// guardReason is shown to Claude when its SendMessage call is blocked. The
// guard can't tell whether the request came from Slack or the terminal, so the
// reason must not claim either: it only says what's disabled and what to do.
const guardReason = "SendMessage is disabled in sessions watched by Slaude, so " +
	"replies stay in this session and reach the user's Slack thread. Answer here " +
	"instead. If the user asked you to message another session, tell them " +
	"SendMessage is disabled in this session."

// denyDecision is the PreToolUse hook output that blocks the tool call.
func denyDecision() []byte {
	out, _ := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]string{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": guardReason,
		},
	})
	return out
}

// guardSend runs as the PreToolUse hook for SendMessage and always blocks it.
// The hook is configured with onFailure "block", so a crash or timeout also
// blocks the call.
func guardSend() {
	defer os.Exit(0)

	var debug io.Writer
	if debugHooks() {
		if f := openDebugLog("guard.log"); f != nil {
			defer f.Close()
			debug = f
		}
	}
	if err := guard(os.Stdin, os.Stdout, debug); err != nil {
		fmt.Fprintln(os.Stderr, "slaude guard-send:", err)
	}
}

// guard writes the deny decision to out first, before reading the input, so a
// change in the payload format can never let a message through. It then reads
// the input (so Claude Code's write to stdin always completes) and, when debug
// is not nil, records it there, one payload per line.
//
// Claude restates the content in its own answer after the block, so the
// blocked text isn't forwarded to Slack.
func guard(in io.Reader, out, debug io.Writer) error {
	if _, err := out.Write(append(denyDecision(), '\n')); err != nil {
		return fmt.Errorf("write decision: %w", err)
	}
	payload, err := io.ReadAll(io.LimitReader(in, 10<<20))
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}
	if debug != nil {
		payload = bytes.TrimRight(payload, "\n") // the input already ends with one
		if _, err := debug.Write(append(payload, '\n')); err != nil {
			return fmt.Errorf("write debug log: %w", err)
		}
	}
	return nil
}

// openDebugLog opens ~/.slaude/<name> for appending, readable only by the user.
func openDebugLog(name string) *os.File {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(home, ".slaude", name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	return f
}
