package cron

import (
	"log/slog"

	"github.com/robfig/cron/v3"
)

// Scheduler manages scheduled tasks.
type Scheduler struct {
	cron   *cron.Cron
	logger *slog.Logger
	jobs   map[string]cron.EntryID
}

// NewScheduler creates a new task scheduler.
func NewScheduler(logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		cron:   cron.New(cron.WithSeconds()),
		logger: logger,
		jobs:   make(map[string]cron.EntryID),
	}
}

// Start starts the scheduler.
func (s *Scheduler) Start() {
	s.cron.Start()
	s.logger.Info("cron scheduler started")
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() {
	s.cron.Stop()
	s.logger.Info("cron scheduler stopped")
}

// AddJob adds a scheduled job.
// spec is a cron expression (e.g., "0 * * * * *" for every minute).
// name is a unique identifier for the job.
// cmd is the function to execute.
func (s *Scheduler) AddJob(spec, name string, cmd func()) error {
	if _, exists := s.jobs[name]; exists {
		s.logger.Warn("job already exists, removing old", "name", name)
		s.RemoveJob(name)
	}

	id, err := s.cron.AddFunc(spec, func() {
		s.logger.Info("running scheduled job", "name", name)
		cmd()
	})
	if err != nil {
		return err
	}

	s.jobs[name] = id
	s.logger.Info("scheduled job added", "name", name, "spec", spec)
	return nil
}

// RemoveJob removes a scheduled job.
func (s *Scheduler) RemoveJob(name string) {
	if id, ok := s.jobs[name]; ok {
		s.cron.Remove(id)
		delete(s.jobs, name)
		s.logger.Info("removed scheduled job", "name", name)
	}
}

// ListJobs returns all job names.
func (s *Scheduler) ListJobs() []string {
	names := make([]string, 0, len(s.jobs))
	for name := range s.jobs {
		names = append(names, name)
	}
	return names
}
