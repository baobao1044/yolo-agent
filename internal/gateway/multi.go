package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// MultiTransport runs multiple messaging transports together.
type MultiTransport struct {
	transports []Transport
	logger     *slog.Logger
	wg         sync.WaitGroup
}

// NewMultiTransport creates a multi-transport gateway.
func NewMultiTransport(transports []Transport, logger *slog.Logger) *MultiTransport {
	if logger == nil {
		logger = slog.Default()
	}
	return &MultiTransport{
		transports: transports,
		logger:     logger,
	}
}

// Start starts all enabled transports in parallel.
func (m *MultiTransport) Start(ctx context.Context, handler Handler) error {
	if len(m.transports) == 0 {
		m.logger.Info("No messaging transports configured")
		return nil
	}

	for _, t := range m.transports {
		t := t // capture range variable
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			if err := t.Start(ctx, handler); err != nil {
				m.logger.Error("transport start error", "transport", t.Name(), "error", err)
			}
		}()
	}

	return nil
}

// Send sends a message using the transport matching msg.Source.
func (m *MultiTransport) Send(ctx context.Context, msg *Envelope) error {
	for _, t := range m.transports {
		if t.Name() == msg.Source {
			return t.Send(ctx, msg)
		}
	}
	return fmt.Errorf("no transport for source %s", msg.Source)
}

// Stop stops all transports.
func (m *MultiTransport) Stop(ctx context.Context) error {
	var firstErr error
	for _, t := range m.transports {
		if err := t.Stop(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("stop %s: %w", t.Name(), err)
		}
	}
	m.wg.Wait()
	return firstErr
}

// Name returns the combined name.
func (m *MultiTransport) Name() string {
	return "multi"
}
