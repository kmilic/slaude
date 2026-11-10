# Slaude

Slaude bridges Slack and a local Claude Code session. Prompt your local Claude from Slack on your phone or anywhere, and get its updates and questions back in the same thread.

> **Work in progress:** Slaude is an early proof of concept, not ready for everyday use. The core loop works (see [Status](#status)); the rest of the features below are planned and may change.

## What it does

- Creates one thread per Claude session in your DM with the Slaude bot, so several sessions can run side by side.
- Posts Claude's status (session started, needs input) and its answers to the session's thread.
- Sends your thread replies to the running Claude session.
- *Planned:* approve or deny tool permission requests with buttons in Slack.

## How it works

Slaude is client-server. One shared **service** talks to Slack, and each user runs a **daemon** on their own machine next to Claude Code.

```
                                 +--> Slaude daemon (user A) <--> Claude Code sessions
Slack <--> Slaude service <------+
                                 +--> Slaude daemon (user B) <--> Claude Code sessions
```

| Component | Runs on | Role |
|-----------|---------|------|
| **Slaude service** | shared server | Holds the Slack connection (Socket Mode), authenticates daemons, and routes messages between each Slack user and that user's daemon. |
| **Slaude daemon** | each user's machine | Connects outbound to the service over a WebSocket. Receives Claude Code hook events and delivers Slack replies to local sessions. |
| **Claude Code hooks** | each user's machine | Send session events (started, needs input, finished) to the local daemon, and block `SendMessage` (see below). |

- **Claude to Slack:** hooks send events to the local daemon, the daemon forwards them to the service, and the service posts them to that user's Slack thread.
- **Slack to Claude:** the service routes your reply to your daemon, which writes it to the session's local inbox socket.
- **Multiple users:** each daemon is tied to one Slack user, so messages and sessions never cross between users.
- **Local and outbound only:** Claude Code stays on the user's machine, and daemons only make outbound connections, so no inbound ports are needed on laptops.
- **Slack replies arrive as peer messages:** the daemon delivers them through the session's inbox socket, so Claude treats them as a teammate's request, not as the user typing. They can't approve permission prompts or change configuration. Approvals will use Approve and Deny buttons instead.
- **`SendMessage` is blocked in Slaude sessions:** because a Slack reply looks like a message from another Claude session, Claude sometimes answered it with `SendMessage`, which sent the answer to an unrelated session instead of Slack. Slaude adds a short note to each delivered reply ("answer here, in this session"), and a `PreToolUse` hook (`slaude guard-send`) blocks `SendMessage` as a guarantee. The hook denies before reading its input and runs with `onFailure: "block"`, so it fails closed.

## Status

Tested on macOS with Claude Code 2.1.316, signed in with a claude.ai account:

- ✅ A Claude session shows up as a Slack thread with its start, notifications and answers.
- ✅ A reply in the thread reaches the session, which answers back in the same thread, including from an idle session.
- ⬜ Approving permission prompts from Slack (planned). For now, the thread shows "Claude needs you", and the prompt is answered at the terminal.
- ⬜ Reconnects, heartbeats to detect sleeping laptops, and delivery acknowledgements.

### Limitations

- **One user and one workspace** per service for now (a fixed pairing token).
- **`SendMessage` is disabled** in Slaude sessions, including messages to running subagents and agent-team teammates. Allowing those targets is planned.
- **Amazon Bedrock is not verified.** Hooks and same-machine messaging are supported on Bedrock from Claude Code 2.1.248, so it should work, but it hasn't been tested.
- **Long term:** Claude Code's Channels feature is designed for chat bridges like this one and would remove the need for the `SendMessage` guard. It is a research preview that requires claude.ai or Console authentication, so Slaude keeps the inbox-socket path, which also works on Bedrock.

## Requirements

- Go 1.23 or later
- Claude Code, installed and authenticated locally
- A Slack app with Socket Mode and Interactivity enabled
