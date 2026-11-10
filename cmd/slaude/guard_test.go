package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// decision decodes guard's output and returns its hookSpecificOutput.
func decision(t *testing.T, out []byte) map[string]string {
	t.Helper()
	var got struct {
		HookSpecificOutput map[string]string `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %q: %v", out, err)
	}
	return got.HookSpecificOutput
}

// A real SendMessage payload (Claude Code 2.1.316), with made-up IDs.
func realPayload(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/pretooluse_sendmessage.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGuardDeniesRealSendMessage(t *testing.T) {
	var out bytes.Buffer
	if err := guard(bytes.NewReader(realPayload(t)), &out, nil); err != nil {
		t.Fatal(err)
	}

	d := decision(t, out.Bytes())
	if d["hookEventName"] != "PreToolUse" || d["permissionDecision"] != "deny" {
		t.Errorf("decision = %v, want a PreToolUse deny", d)
	}
	if d["permissionDecisionReason"] != guardReason {
		t.Errorf("reason = %q", d["permissionDecisionReason"])
	}
}

// The guard must deny whatever it receives: it never depends on the payload.
func TestGuardDeniesAnyInput(t *testing.T) {
	inputs := map[string]string{
		"empty":           "",
		"not JSON":        "this is not json",
		"other shape":     `{"tool_input":{"recipient":"x","body":"renamed field"}}`,
		"truncated JSON":  `{"tool_name":"SendMessage","tool_inp`,
		"unexpected type": `["a","b"]`,
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			_ = guard(strings.NewReader(in), &out, nil)
			if d := decision(t, out.Bytes()); d["permissionDecision"] != "deny" {
				t.Errorf("decision = %v, want deny", d)
			}
		})
	}
}

// failingReader fails on Read, like a broken stdin.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestGuardDeniesBeforeReading(t *testing.T) {
	var out bytes.Buffer
	err := guard(failingReader{}, &out, nil)

	if err == nil {
		t.Error("want the read error reported")
	}
	if d := decision(t, out.Bytes()); d["permissionDecision"] != "deny" {
		t.Errorf("the deny must be written before reading; got %v", d)
	}
}

// The guard can't know where a request came from, so the reason must not claim it.
func TestGuardReasonIsNeutral(t *testing.T) {
	for _, claim := range []string{"from the user via Slack", "sent this from Slack", "not from another Claude session"} {
		if strings.Contains(guardReason, claim) {
			t.Errorf("reason claims the request's source: %q", claim)
		}
	}
}

func TestGuardDebugLogRecordsOnePayloadPerLine(t *testing.T) {
	var out, debug bytes.Buffer
	in := append(realPayload(t), '\n') // Claude Code's input ends with a newline

	if err := guard(bytes.NewReader(in), &out, &debug); err != nil {
		t.Fatal(err)
	}

	logged := debug.Bytes()
	if !bytes.HasSuffix(logged, []byte("}\n")) || bytes.HasSuffix(logged, []byte("\n\n")) {
		t.Errorf("want exactly one trailing newline, got ...%q", logged[max(0, len(logged)-10):])
	}
	if !json.Valid(logged) {
		t.Error("the logged payload must be valid JSON")
	}
}

func TestGuardWithoutDebugLogsNothing(t *testing.T) {
	var out bytes.Buffer
	// A nil debug writer must be fine: nothing to record, still a deny.
	if err := guard(bytes.NewReader(realPayload(t)), &out, nil); err != nil {
		t.Fatal(err)
	}
	if d := decision(t, out.Bytes()); d["permissionDecision"] != "deny" {
		t.Errorf("decision = %v", d)
	}
}

func TestDebugHooksSwitch(t *testing.T) {
	for value, want := range map[string]bool{"": false, "0": false, "true": false, "1": true} {
		t.Setenv(debugHooksEnv, value)
		if got := debugHooks(); got != want {
			t.Errorf("%s=%q: debugHooks() = %v, want %v", debugHooksEnv, value, got, want)
		}
	}
}
