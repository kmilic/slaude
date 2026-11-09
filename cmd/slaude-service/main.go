// Command slaude-service is the shared Slaude service. It holds the only Slack
// connection (Socket Mode) and, from step 4 on, the daemons' WebSockets.
//
// Step 2: it echoes DMs from SLAUDE_SLACK_USER.
// Step 4: it accepts daemons on ws://127.0.0.1:8080/daemon (SLAUDE_LISTEN to change).
// Step 6: it posts each Claude session's events to its own thread in the user's DM.
//
//	slaude-env                      # loads the tokens from Keychain
//	go run ./cmd/slaude-service
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"slaude/internal/hub"
	"slaude/internal/relay"
	"slaude/internal/slackbot"
	"slaude/internal/slackthreads"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	appToken := mustEnv(log, "SLAUDE_SLACK_APP_TOKEN")
	botToken := mustEnv(log, "SLAUDE_SLACK_BOT_TOKEN")
	allowedUser := mustEnv(log, "SLAUDE_SLACK_USER")
	pairingToken := mustEnv(log, "SLAUDE_PAIRING_TOKEN")
	listen := os.Getenv("SLAUDE_LISTEN")
	if listen == "" {
		listen = "127.0.0.1:8080" // localhost only: not reachable from other machines
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	api := slack.New(botToken, slack.OptionAppLevelToken(appToken))

	// Checks the bot token and gives us the bot's own user ID.
	auth, err := api.AuthTestContext(ctx)
	if err != nil {
		log.Error("bot token check failed", "err", err)
		os.Exit(1)
	}
	log.Info("bot token ok", "bot_user", auth.UserID, "team", auth.Team)

	// The user's DM with the bot: opened if needed, otherwise the existing one.
	dm, _, _, err := api.OpenConversationContext(ctx, &slack.OpenConversationParameters{Users: []string{allowedUser}})
	if err != nil {
		log.Error("open DM with user failed", "user", allowedUser, "err", err)
		os.Exit(1)
	}
	log.Info("DM with user ready", "user", allowedUser, "channel", dm.ID)

	slackPoster := poster{api: api}
	daemons := hub.New(pairingToken, allowedUser, log)
	sessionRelay := relay.New(slackPoster, daemons, slackthreads.New(), dm.ID, log)
	daemons.OnEvent = sessionRelay.OnEvent
	mux := http.NewServeMux()
	mux.Handle("/daemon", daemons)
	srv := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info("listening for daemons", "addr", "ws://"+listen+"/daemon")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("daemon listener failed", "err", err)
			stop()
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	handler := &slackbot.Handler{
		Poster:      slackPoster,
		BotUserID:   auth.UserID,
		AllowedUser: allowedUser,
		Log:         log,
	}

	sm := socketmode.New(api)
	go handleEvents(ctx, sm, handler, log)

	// Opens the Socket Mode connection and reconnects when Slack drops it.
	if err := sm.RunContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("socket mode stopped", "err", err)
		return // deferred shutdown still runs
	}
	log.Info("stopped")
}

func handleEvents(ctx context.Context, sm *socketmode.Client, h *slackbot.Handler, log *slog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-sm.Events:
			switch evt.Type {
			case socketmode.EventTypeConnecting:
				log.Info("connecting to Slack")
			case socketmode.EventTypeConnected:
				log.Info("connected to Slack")
			case socketmode.EventTypeConnectionError:
				log.Warn("connection error, retrying", "data", evt.Data)
			case socketmode.EventTypeEventsAPI:
				// Ack first: Slack resends events not acked within ~3 seconds.
				if evt.Request != nil {
					sm.Ack(*evt.Request)
				}
				outer, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				if msg, ok := outer.InnerEvent.Data.(*slackevents.MessageEvent); ok {
					h.OnMessage(ctx, slackbot.Message{
						User:        msg.User,
						Text:        msg.Text,
						Channel:     msg.Channel,
						ChannelType: msg.ChannelType,
						ThreadTS:    msg.ThreadTimeStamp,
						SubType:     msg.SubType,
						BotID:       msg.BotID,
					})
				}
			}
		}
	}
}

// poster adapts the Slack client to slackbot.Poster.
type poster struct{ api *slack.Client }

func (p poster) Post(ctx context.Context, channel, threadTS, text string) (string, error) {
	opts := []slack.MsgOption{slack.MsgOptionText(text, false)}
	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}
	_, ts, err := p.api.PostMessageContext(ctx, channel, opts...)
	return ts, err
}

func mustEnv(log *slog.Logger, key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Error("missing environment variable (run slaude-env first)", "key", key)
		os.Exit(1)
	}
	return v
}
