// Copyright (c) 2026 Bao Bui Gia / YOLO Agent contributors
// SPDX-License-Identifier: MIT

package background

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// RunFunc executes an agent turn given an instruction and returns the result.
type RunFunc func(ctx context.Context, instruction string) (string, error)

// Runner executes background tasks within their permission envelopes.
type Runner struct {
	store    *Store
	notifier Notifier
	runs     map[string]*runContext
	mu       sync.RWMutex
	logger   *slog.Logger
}

// runContext holds runtime state for an active envelope.
type runContext struct {
	cancel  context.CancelFunc
	env     *Envelope
	started time.Time
}

// NewRunner creates a background task runner.
func NewRunner(store *Store, notifier Notifier, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		store:    store,
		notifier: notifier,
		runs:     make(map[string]*runContext),
		logger:   logger,
	}
}

// Start triggers a single execution of the given envelope using runFunc.
func (r *Runner) Start(ctx context.Context, env *Envelope, runFunc RunFunc) error {
	if err := env.CanRun(); err != nil {
		return err
	}

	r.mu.Lock()
	if _, exists := r.runs[env.ID]; exists {
		r.mu.Unlock()
		return fmt.Errorf("envelope %s is already running", env.ID)
	}

	execCtx, cancel := context.WithCancel(ctx)
	r.runs[env.ID] = &runContext{
		cancel:  cancel,
		env:     env,
		started: time.Now().UTC(),
	}
	r.mu.Unlock()

	go r.execute(execCtx, env, runFunc)
	return nil
}

// execute runs the agent inside the envelope and enforces limits at runtime.
// This is the core of YOLO Mode: every tool call and LLM turn is bounded.
func (r *Runner) execute(ctx context.Context, env *Envelope, runFunc RunFunc) {
	defer func() {
		r.mu.Lock()
		delete(r.runs, env.ID)
		r.mu.Unlock()
	}()

	now := time.Now().UTC()
	env.SetStatus(StatusRunning)
	env.LastRunAt = &now
	env.RunCount++
	if err := r.store.Save(env); err != nil {
		r.logger.Error("background: failed to save running envelope", "id", env.ID, "error", err)
	}

	if env.Notify.OnStart {
		r.notify(ctx, env, "YOLO Mode started", fmt.Sprintf("Task %q is now running in the background.", env.Name))
	}

	deadline := env.Budget.MaxDuration
	if deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(deadline)*time.Second)
		defer cancel()
	}

	result, err := runFunc(ctx, env.Instruction)

	ended := time.Now().UTC()
	env.UpdatedAt = ended

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			env.RecordError(fmt.Sprintf("killed: exceeded max duration %ds", env.Budget.MaxDuration))
			env.SetStatus(StatusKilled)
			r.notify(ctx, env, "YOLO Mode killed", fmt.Sprintf("Task %q exceeded max duration of %ds.", env.Name, env.Budget.MaxDuration))
		} else {
			env.RecordError(err.Error())
			env.SetStatus(StatusFailed)
			r.notify(ctx, env, "YOLO Mode failed", fmt.Sprintf("Task %q failed: %v", env.Name, err))
		}
		r.logger.Error("background: task failed", "id", env.ID, "error", err)
	} else {
		env.SetStatus(StatusCompleted)
		if env.Notify.OnFinish {
			r.notify(ctx, env, "YOLO Mode finished", fmt.Sprintf("Task %q completed successfully.\n\nResult:\n%s", env.Name, truncate(result, 400)))
		}
	}

	if err := r.store.Save(env); err != nil {
		r.logger.Error("background: failed to persist envelope", "id", env.ID, "error", err)
	}
}

// Kill stops a running background task immediately.
func (r *Runner) Kill(id string) error {
	r.mu.RLock()
	run, ok := r.runs[id]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("envelope %s is not running", id)
	}

	run.cancel()
	run.env.SetStatus(StatusKilled)
	if err := r.store.Save(run.env); err != nil {
		return fmt.Errorf("save killed envelope: %w", err)
	}
	return nil
}

// Pause marks an envelope as paused so it will not be scheduled again.
func (r *Runner) Pause(id string) (*Envelope, error) {
	env, err := r.store.Get(id)
	if err != nil {
		return nil, err
	}
	env.SetStatus(StatusPaused)
	if err := r.store.Save(env); err != nil {
		return nil, err
	}
	return env, nil
}

// Resume marks a paused envelope as pending.
func (r *Runner) Resume(id string) (*Envelope, error) {
	env, err := r.store.Get(id)
	if err != nil {
		return nil, err
	}
	env.SetStatus(StatusPending)
	if err := r.store.Save(env); err != nil {
		return nil, err
	}
	return env, nil
}

// Active returns the IDs of currently running envelopes.
func (r *Runner) Active() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.runs))
	for id := range r.runs {
		ids = append(ids, id)
	}
	return ids
}

// notify sends a notification if a notifier is configured.
func (r *Runner) notify(ctx context.Context, env *Envelope, title, message string) {
	if r.notifier == nil {
		return
	}
	if err := r.notifier.Notify(ctx, env.ID, title, message, env.Notify.Channels); err != nil {
		r.logger.Error("background: notifier failed", "id", env.ID, "error", err)
	}
}

// truncate shortens a string to maxLen characters with an ellipsis.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
