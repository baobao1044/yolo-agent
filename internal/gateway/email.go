package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"time"
)

// EmailConfig configures the email gateway.
type EmailConfig struct {
	Enabled  bool   `yaml:"enabled"`
	SMTPHost string `yaml:"smtp_host"`
	SMTPPort int    `yaml:"smtp_port"`
	SMTPUser string `yaml:"smtp_user"`
	SMTPPass string `yaml:"smtp_pass"`
}

// EmailTransport implements Transport for SMTP email sending.
// Receiving email via IMAP is intentionally minimal in this version.
type EmailTransport struct {
	config  EmailConfig
	logger  *slog.Logger
	handler Handler
}

// NewEmailTransport creates a new email transport.
func NewEmailTransport(config EmailConfig, logger *slog.Logger) *EmailTransport {
	if logger == nil {
		logger = slog.Default()
	}
	return &EmailTransport{config: config, logger: logger}
}

func (e *EmailTransport) Name() string {
	return "email"
}

// Start polls an IMAP inbox if configured. Currently a placeholder.
func (e *EmailTransport) Start(ctx context.Context, handler Handler) error {
	if !e.config.Enabled {
		e.logger.Info("Email gateway disabled")
		return nil
	}
	e.handler = handler

	// TODO: implement IMAP polling using go-imap v2 once adapter is stable.
	e.logger.Info("Email gateway started (send-only in this version)")

	<-ctx.Done()
	return nil
}

// pollInbox placeholder for future IMAP implementation.
func (e *EmailTransport) pollInbox(ctx context.Context) error {
	// IMAP polling will be implemented with go-imap v2 in a follow-up.
	return nil
}

// Send sends an email via SMTP.
func (e *EmailTransport) Send(ctx context.Context, msg *Envelope) error {
	if e.config.SMTPHost == "" || e.config.SMTPUser == "" {
		return fmt.Errorf("SMTP not fully configured")
	}

	subject := "Re: YOLO Agent"
	body := msg.Text

	emailMsg := []byte("Subject: " + subject + "\r\n" +
		"\r\n" +
		body + "\r\n")

	addr := fmt.Sprintf("%s:%d", e.config.SMTPHost, e.config.SMTPPort)
	auth := smtp.PlainAuth("", e.config.SMTPUser, e.config.SMTPPass, e.config.SMTPHost)

	return smtp.SendMail(addr, auth, e.config.SMTPUser, []string{msg.ChatID}, emailMsg)
}

// Stop would close any open connections.
func (e *EmailTransport) Stop(ctx context.Context) error {
	return nil
}

// tick is unused but kept for future polling logic.
func tick() {
	time.NewTicker(30 * time.Second)
}
