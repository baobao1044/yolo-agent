package gateway

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bwmarrin/discordgo"
)

// DiscordConfig configures the Discord gateway.
type DiscordConfig struct {
	Enabled bool   `yaml:"enabled"`
	Token   string `yaml:"token"`
}

// DiscordTransport implements Transport for Discord.
type DiscordTransport struct {
	session *discordgo.Session
	config  DiscordConfig
	logger  *slog.Logger
	handler Handler
}

// NewDiscordTransport creates a new Discord transport.
func NewDiscordTransport(config DiscordConfig, logger *slog.Logger) *DiscordTransport {
	if logger == nil {
		logger = slog.Default()
	}
	return &DiscordTransport{config: config, logger: logger}
}

func (d *DiscordTransport) Name() string {
	return "discord"
}

// Start opens the Discord session and listens for messages.
func (d *DiscordTransport) Start(ctx context.Context, handler Handler) error {
	if !d.config.Enabled || d.config.Token == "" {
		d.logger.Info("Discord gateway disabled")
		return nil
	}

	session, err := discordgo.New("Bot " + d.config.Token)
	if err != nil {
		return fmt.Errorf("discord create session: %w", err)
	}
	d.session = session
	d.handler = handler

	session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore own messages
		if m.Author.ID == s.State.User.ID {
			return
		}

		msg := &Envelope{
			ID:         m.ID,
			Source:     "discord",
			SenderID:   m.Author.ID,
			SenderName: m.Author.Username,
			ChatID:     m.ChannelID,
			Text:       m.Content,
			ReplyTo:    m.Reference().MessageID,
		}

		resp, err := handler(ctx, msg)
		if err != nil {
			d.logger.Error("discord handler error", "error", err)
			return
		}
		if resp != nil {
			_ = d.Send(ctx, resp)
		}
	})

	if err := session.Open(); err != nil {
		return fmt.Errorf("discord open session: %w", err)
	}

	d.logger.Info("Discord bot started")

	<-ctx.Done()
	return d.Stop(ctx)
}

// Send sends a message to Discord.
func (d *DiscordTransport) Send(ctx context.Context, msg *Envelope) error {
	if d.session == nil {
		return fmt.Errorf("discord session not started")
	}
	_, err := d.session.ChannelMessageSend(msg.ChatID, msg.Text)
	return err
}

// Stop closes the Discord session.
func (d *DiscordTransport) Stop(ctx context.Context) error {
	if d.session != nil {
		return d.session.Close()
	}
	return nil
}
