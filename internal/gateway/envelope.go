package gateway

import "context"

// Envelope is a unified message across all gateways.
type Envelope struct {
	ID          string            `json:"id"`
	Source      string            `json:"source"` // telegram, discord, slack, email, api
	SenderID    string            `json:"sender_id"`
	SenderName  string            `json:"sender_name"`
	ChatID      string            `json:"chat_id"`
	Text        string            `json:"text"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	ReplyTo     string            `json:"reply_to,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Attachment represents a file attachment.
type Attachment struct {
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	DataURL  string `json:"data_url"`
}

// Handler processes incoming messages.
type Handler func(ctx context.Context, msg *Envelope) (*Envelope, error)

// Transport is the common interface for messaging gateways.
type Transport interface {
	// Start begins listening for incoming messages.
	Start(ctx context.Context, handler Handler) error

	// Send sends an outgoing message.
	Send(ctx context.Context, msg *Envelope) error

	// Stop shuts down the transport.
	Stop(ctx context.Context) error

	// Name returns the transport name.
	Name() string
}
