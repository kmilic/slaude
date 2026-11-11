// Command slaude-send posts one message into a running Claude Code session
// through its inbox socket. It is the spike for Slaude's Slack-to-Claude path.
//
//	slaude-send -socket uds:/tmp/cc-socks/1234.sock "what changed since the last commit?"
//	echo "hello" | slaude-send -socket /tmp/cc-socks/1234.sock
//
// The socket path is the "Peer address" row in the session's /status. Inside
// the session (a hook or Bash command), -socket defaults to
// CLAUDE_CODE_MESSAGING_SOCKET.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"slaude/internal/inbox"
)

func main() {
	socket := flag.String("socket", os.Getenv("CLAUDE_CODE_MESSAGING_SOCKET"), "session inbox socket path (the uds: prefix is allowed)")
	wait := flag.Duration("wait", 2*time.Second, "how long to wait for a reply from the session")
	flag.Parse()

	text := strings.Join(flag.Args(), " ")
	if text == "" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fail("read stdin: %v", err)
		}
		text = strings.TrimRight(string(b), "\n")
	}

	// Stay well under Claude Code's 30-second limit for a complete line.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reply, err := inbox.Send(ctx, *socket, text, *wait)
	if err != nil {
		fail("%v", err)
	}
	fmt.Println("sent")
	if reply != "" {
		fmt.Printf("session replied: %s\n", strings.TrimSpace(reply))
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "slaude-send: "+format+"\n", args...)
	os.Exit(1)
}
