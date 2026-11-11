package main

import (
	"strings"
	"testing"
)

// jsonOfSize returns a valid JSON object exactly n bytes long.
func jsonOfSize(n int) string {
	const wrap = `{"x":""}`
	return `{"x":"` + strings.Repeat("a", n-len(wrap)) + `"}`
}

func TestReadHookInputAcceptsJSON(t *testing.T) {
	in := `{"session_id":"s1","hook_event_name":"Stop"}`

	got, err := readHookInput(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != in {
		t.Errorf("payload = %q, want %q", got, in)
	}
}

func TestReadHookInputAcceptsInputAtTheCap(t *testing.T) {
	if _, err := readHookInput(strings.NewReader(jsonOfSize(maxHookInput))); err != nil {
		t.Errorf("input of exactly maxHookInput bytes must be accepted: %v", err)
	}
}

func TestReadHookInputRejects(t *testing.T) {
	tests := []struct {
		name, in, wantErr string
	}{
		{"too large", jsonOfSize(maxHookInput + 1), "larger than 10 MiB"},
		{"not JSON", "this is not json", "not JSON"},
		{"empty", "", "not JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readHookInput(strings.NewReader(tt.in))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}
