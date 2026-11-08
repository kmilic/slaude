**Work in progress:** Slaude is in early development, the features and design below are planned and may change.

# Slaude

Slaude bridges Slack and a local Claude Code session. Prompt your local Claude from Slack on your phone or anywhere, and get its updates and questions back in the same thread.

## What it does

- Posts Claude's status (session started, needs input, finished) to a Slack thread.
- Sends your thread replies to the running Claude session as prompts.
- Lets you approve or deny tool permission requests with buttons in Slack.
- Keeps track of all active sessions
- Creates one thread per Claude session, so several sessions can run side by side.
- All your skills, MCPs, agents etc... available through slack

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
| **Claude Code hooks** | each user's machine | Send session events (started, needs input, finished, permission request) to the local daemon. |

- **Claude to Slack:** hooks send events to the local daemon, the daemon forwards them to the service, and the service posts them to that user's Slack thread.
- **Slack to Claude:** the service routes your reply to your daemon, which writes it to the session's local inbox socket.
- **Multiple users:** each daemon is tied to one Slack user, so messages and sessions never cross between users.
- **Local and outbound only:** Claude Code stays on the user's machine, and daemons only make outbound connections, so no inbound ports are needed on laptops.

## Requirements

- Claude Code, installed and authenticated locally
- Slaude Slack agent with Socket Mode and Interactivity enabled
