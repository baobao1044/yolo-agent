# Gateways

YOLO Agent can be reached through multiple channels using a unified message envelope.

## Supported Gateways

| Gateway | Status | Notes |
|---|---|---|
| TUI | ✅ | Bubble Tea terminal UI |
| HTTP API | ✅ | `/chat`, `/webhook`, `/status`, `/health` |
| Telegram | ✅ | Long-polling bot |
| Discord | ✅ | RTM bot |
| Slack | ✅ | RTM bot |
| Email | 🟡 | SMTP send only; IMAP receive planned |

## Unified Envelope

All gateways convert messages to:

```go
type Envelope struct {
    ID          string
    Source      string // telegram, discord, slack, email, api
    SenderID    string
    SenderName  string
    ChatID      string
    Text        string
    Attachments []Attachment
    ReplyTo     string
    Metadata    map[string]string
}
```

## Configuration

```yaml
# HTTP gateway
gateway:
  enabled: true
  port: 8080

# Telegram
telegram:
  enabled: true
  token: "YOUR_BOT_TOKEN"

# Discord
discord:
  enabled: true
  token: "YOUR_BOT_TOKEN"

# Slack
slack:
  enabled: true
  token: "YOUR_BOT_TOKEN"

# Email (send-only in current version)
email:
  enabled: true
  smtp_host: smtp.gmail.com
  smtp_port: 587
  smtp_user: your@email.com
  smtp_pass: "app-password"
```

## API Example

```bash
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{"payload": {"message": "Hello"}}'
```

## Adding a New Gateway

To add a new messaging transport:

1. Implement `gateway.Transport`
2. Add config struct
3. Wire it in `cmd/agent/main.go`

```go
type Transport interface {
    Start(ctx context.Context, handler Handler) error
    Send(ctx context.Context, msg *Envelope) error
    Stop(ctx context.Context) error
    Name() string
}
```
