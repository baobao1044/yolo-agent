// Copyright (c) 2026 Bao Bui Gia / YOLO Agent contributors
// SPDX-License-Identifier: MIT

package background

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler wires cron schedules to the background Runner.
// BuildRunFunc creates a runnable agent turn for a given envelope.
type BuildRunFunc func(ctx context.Context, env *Envelope) RunFunc

type Scheduler struct {
	runner    *Runner
	store     *Store
	cron      *cron.Cron
	buildFunc BuildRunFunc
	mu        sync.Mutex
	entries   map[string]cron.EntryID
	logger    *slog.Logger
}

// NewScheduler creates a scheduler backed by the given store and runner.
func NewScheduler(store *Store, runner *Runner, buildFunc BuildRunFunc, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		runner:    runner,
		store:     store,
		cron:      cron.New(cron.WithSeconds()),
		buildFunc: buildFunc,
		entries:   make(map[string]cron.EntryID),
		logger:    logger,
	}
}

// Start loads persisted envelopes and begins scheduling.
func (s *Scheduler) Start(ctx context.Context) error {
	envs, err := s.store.List()
	if err != nil {
		return fmt.Errorf("list envelopes: %w", err)
	}

	for _, env := range envs {
		if env.Status == StatusPaused || env.Status == StatusKilled {
			continue
		}
		if err := s.Schedule(ctx, env); err != nil {
			s.logger.Error("background: failed to schedule envelope", "id", env.ID, "error", err)
		}
	}

	s.cron.Start()
	return nil
}

// Stop halts all scheduled tasks.
func (s *Scheduler) Stop() {
	s.cron.Stop()
}

// Schedule adds an envelope to the scheduler.
// If schedule is "@once" the envelope is executed immediately and not re-scheduled.
// If schedule is "@interval Ns|Nm|Nh" it is translated to cron syntax.
// Otherwise schedule is treated as a standard cron expression.
func (s *Scheduler) Schedule(ctx context.Context, env *Envelope) error {
	if err := env.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing entry if present.
	if id, ok := s.entries[env.ID]; ok {
		s.cron.Remove(id)
		delete(s.entries, env.ID)
	}

	if env.Status == StatusPaused || env.Status == StatusKilled {
		return nil
	}

	expr := normalizeSchedule(env.Schedule)

	// Immediate one-shot: run now and do not add to cron.
	if expr == "@once" {
		env.NextRunAt = nil
		_ = s.store.Save(env)
		go s.runner.Start(ctx, env, s.buildFunc(ctx, env))
		return nil
	}

	id, err := s.cron.AddFunc(expr, func() {
		s.runScheduled(ctx, env.ID)
	})
	if err != nil {
		return fmt.Errorf("invalid schedule %q: %w", env.Schedule, err)
	}

	s.entries[env.ID] = id
	next := s.cron.Entry(id).Next
	env.NextRunAt = &next
	if err := s.store.Save(env); err != nil {
		s.logger.Error("background: failed to save next run", "id", env.ID, "error", err)
	}

	return nil
}

// Resume sets a paused envelope back to pending and re-schedules it.
func (s *Scheduler) Resume(id string) (*Envelope, error) {
	env, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	env.SetStatus(StatusPending)
	if err := s.store.Save(env); err != nil {
		return nil, err
	}
	if err := s.Schedule(context.Background(), env); err != nil {
		return nil, err
	}
	return env, nil
}

// Unschedule removes a cron entry.
func (s *Scheduler) Unschedule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, ok := s.entries[id]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, id)
	}
	return nil
}

// runScheduled loads the latest envelope state and executes it.
func (s *Scheduler) runScheduled(ctx context.Context, id string) {
	env, err := s.store.Get(id)
	if err != nil {
		s.logger.Error("background: failed to load scheduled envelope", "id", id, "error", err)
		return
	}
	if env.Status == StatusPaused || env.Status == StatusKilled {
		return
	}

	now := time.Now().UTC()
	env.NextRunAt = &now
	if err := s.runner.Start(ctx, env, s.buildFunc(ctx, env)); err != nil {
		s.logger.Error("background: scheduled run failed", "id", id, "error", err)
	}
}

// normalizeSchedule converts friendly aliases into cron syntax.
func normalizeSchedule(schedule string) string {
	schedule = strings.TrimSpace(strings.ToLower(schedule))

	switch schedule {
	case "@once", "now":
		return "@once"
	case "@hourly":
		return "0 * * * * *"
	case "@daily":
		return "0 0 0 * * *"
	case "@weekly":
		return "0 0 0 * * 0"
	case "@monthly":
		return "0 0 0 1 * *"
	}

	if strings.HasPrefix(schedule, "@interval ") {
		d, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(schedule, "@interval ")))
		if err != nil {
			return schedule
		}
		// Convert seconds to cron with seconds field.
		secs := int(d.Seconds())
		if secs <= 0 {
			return schedule
		}
		if secs >= 60 && secs%60 == 0 {
			mins := secs / 60
			return fmt.Sprintf("0 */%d * * * *", mins)
		}
		return fmt.Sprintf("*/%d * * * * *", secs)
	}

	return schedule
}
