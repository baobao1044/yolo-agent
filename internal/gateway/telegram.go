package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TelegramConfig configures the Telegram gateway.
type TelegramConfig struct {
	Enabled bool   `yaml:"enabled"`
	Token   string `yaml:"token"`
}

// TelegramTransport implements Transport for Telegram.
type TelegramTransport struct {
	bot     *tgbotapi.BotAPI
	config  TelegramConfig
	logger  *slog.Logger
	handler Handler
}

// NewTelegramTransport creates a new Telegram transport.
func NewTelegramTransport(config TelegramConfig, logger *slog.Logger) *TelegramTransport {
	if logger == nil {
		logger = slog.Default()
	}
	return &TelegramTransport{config: config, logger: logger}
}

func (t *TelegramTransport) Name() string {
	return "telegram"
}

// Start connects to Telegram and starts long-polling.
func (t *TelegramTransport) Start(ctx context.Context, handler Handler) error {
	if !t.config.Enabled || t.config.Token == "" {
		t.logger.Info("Telegram gateway disabled")
		return nil
	}

	bot, err := tgbotapi.NewBotAPI(t.config.Token)
	if err != nil {
		return fmt.Errorf("telegram connect: %w", err)
	}
	bot.Debug = false
	t.bot = bot
	t.handler = handler

	t.logger.Info("Telegram bot started", "username", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case update := <-updates:
				if update.Message == nil {
					continue
				}
			msg := &Envelope{
				ID:         strconv.Itoa(update.Message.MessageID),
				Source:     "telegram",
				SenderID:   strconv.FormatInt(update.Message.From.ID, 10),
					SenderName: update.Message.From.UserName,
					ChatID:     strconv.FormatInt(update.Message.Chat.ID, 10),
					Text:       update.Message.Text,
				}

				resp, err := handler(ctx, msg)
				if err != nil {
					t.logger.Error("telegram handler error", "error", err)
					continue
				}
				if resp != nil {
					_ = t.Send(ctx, resp)
				}
			}
		}
	}()

	// Block until context cancelled
	<-ctx.Done()
	return nil
}

// Send sends a message to Telegram.
func (t *TelegramTransport) Send(ctx context.Context, msg *Envelope) error {
	if t.bot == nil {
		return fmt.Errorf("telegram bot not started")
	}

	chatID, err := strconv.ParseInt(msg.ChatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid telegram chat id: %w", err)
	}

	reply := tgbotapi.NewMessage(chatID, msg.Text)
	_, err = t.bot.Send(reply)
	return err
}

// Stop shuts down Telegram polling.
func (t *TelegramTransport) Stop(ctx context.Context) error {
	if t.bot != nil {
		t.bot.StopReceivingUpdates()
	}
	return nil
}
