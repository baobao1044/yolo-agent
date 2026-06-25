package gateway

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/slack-go/slack"
)

// SlackConfig configures the Slack gateway.
type SlackConfig struct {
	Enabled bool   `yaml:"enabled"`
	Token   string `yaml:"token"`
}

// SlackTransport implements Transport for Slack RTM.
type SlackTransport struct {
	client *slack.Client
	rtm    *slack.RTM
	config SlackConfig
	logger *slog.Logger
	handler Handler
}

// NewSlackTransport creates a new Slack transport.
func NewSlackTransport(config SlackConfig, logger *slog.Logger) *SlackTransport {
	if logger == nil {
		logger = slog.Default()
	}
	return &SlackTransport{config: config, logger: logger}
}

func (s *SlackTransport) Name() string {
	return "slack"
}

// Start connects to Slack RTM and listens for messages.
func (s *SlackTransport) Start(ctx context.Context, handler Handler) error {
	if !s.config.Enabled || s.config.Token == "" {
		s.logger.Info("Slack gateway disabled")
		return nil
	}

	client := slack.New(s.config.Token)
	s.client = client
	rtm := client.NewRTM()
	s.rtm = rtm
	s.handler = handler

	go rtm.ManageConnection()

	go func() {
		for {
			select {
			case <-ctx.Done():
				rtm.Disconnect()
				return
			case msg := <-rtm.IncomingEvents:
				s.handleEvent(ctx, msg.Data)
			}
		}
	}()

	s.logger.Info("Slack RTM started")
	<-ctx.Done()
	return nil
}

// handleEvent processes Slack RTM events.
func (s *SlackTransport) handleEvent(ctx context.Context, ev any) {
	switch msg := ev.(type) {
	case *slack.MessageEvent:
		// Ignore messages from the bot itself
		if msg.User == "" {
			return
		}

		env := &Envelope{
			ID:         msg.Timestamp,
			Source:     "slack",
			SenderID:   msg.User,
			ChatID:     msg.Channel,
			Text:       msg.Text,
			ReplyTo:    msg.ThreadTimestamp,
		}

		resp, err := s.handler(ctx, env)
		if err != nil {
			s.logger.Error("slack handler error", "error", err)
			return
		}
		if resp != nil {
			_ = s.Send(ctx, resp)
		}

	case *slack.RTMError:
		s.logger.Error("slack RTM error", "error", msg.Error())
	case *slack.InvalidAuthEvent:
		s.logger.Error("slack invalid auth")
	}
}

// Send sends a message to Slack.
func (s *SlackTransport) Send(ctx context.Context, msg *Envelope) error {
	if s.rtm == nil {
		return fmt.Errorf("slack RTM not started")
	}

	s.rtm.SendMessage(s.rtm.NewOutgoingMessage(msg.Text, msg.ChatID))
	return nil
}

// Stop closes the Slack RTM.
func (s *SlackTransport) Stop(ctx context.Context) error {
	if s.rtm != nil {
		s.rtm.Disconnect()
	}
	return nil
}
