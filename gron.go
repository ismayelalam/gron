package gron

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Scheduler manages and executes scheduled jobs.
//
// Create with NewScheduler, register jobs with NewJob, and start execution
// with Start. The scheduler is thread-safe and designed for long-running
// processes.
type Scheduler struct {
	jobs     map[string]*Job
	mu       sync.RWMutex
	config   SchedulerConfig
	ticker   *time.Ticker
	done     chan struct{}
	wg       sync.WaitGroup
	started  bool
	sem      chan struct{}
	stopOnce sync.Once
}

// NewScheduler creates a new Scheduler instance with optional configuration.
//
// You can pass a SchedulerConfig struct to customize global behavior.
// If no configuration is provided, sensible defaults will be used.
// Zero values inside the provided struct will also be replaced by defaults.
//
// Examples:
//
//	// 1. With default configuration
//	s, err := gron.NewScheduler()
//
//	// 2. With custom configuration
//	s, err := gron.NewScheduler(gron.SchedulerConfig{
//	    CheckInterval:     500 * time.Millisecond,
//	    MaxConcurrentJobs: 50,
//	})
func NewScheduler(args ...SchedulerConfig) (*Scheduler, error) {
	var cfg SchedulerConfig

	// If the user provided a config, use it. Otherwise, cfg remains a zero-value struct.
	if len(args) > 0 {
		cfg = args[0]
	}

	// Apply sensible defaults for any zero values
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 1 * time.Second
	}
	if cfg.DefaultTimezone == "" {
		cfg.DefaultTimezone = "UTC"
	}
	if cfg.MaxConcurrentJobs <= 0 {
		cfg.MaxConcurrentJobs = 1000
	}
	if cfg.Misfire == MisfireUnset {
		cfg.Misfire = MisfireSkip
	}
	if cfg.Concurrency == ConcurrencyUnset {
		cfg.Concurrency = ConcurrencyForbid
	}

	return &Scheduler{
		jobs:   make(map[string]*Job),
		config: cfg,
		done:   make(chan struct{}),
		sem:    make(chan struct{}, cfg.MaxConcurrentJobs),
	}, nil
}

// NewJob registers a new job with the scheduler.
//
// The task argument accepts multiple signatures for flexibility:
//   - func() — Simple tasks; output passed to OnSuccess is nil
//   - func() any — Tasks returning a value
//   - func(context.Context) (any, error) — Recommended; supports timeout/cancel
//
// JobConfig.Schedule is required. All other fields are optional with defaults.
// Returns an error if the schedule is invalid or job name conflicts.
func (s *Scheduler) NewJob(task any, cfg JobConfig) (*Job, error) {
	if cfg.Timezone == "" {
		cfg.Timezone = s.config.DefaultTimezone
	}

	if cfg.Misfire == MisfireUnset {
		if s.config.Misfire != MisfireUnset {
			cfg.Misfire = s.config.Misfire
		} else {
			cfg.Misfire = MisfireSkip // Fallback | default
		}
	}

	if cfg.Concurrency == ConcurrencyUnset {
		if s.config.Concurrency != ConcurrencyUnset {
			cfg.Concurrency = s.config.Concurrency
		} else {
			cfg.Concurrency = ConcurrencyForbid // Fallback | default
		}
	}

	var fn TaskFunc
	switch t := task.(type) {
	case func():
		fn = func(ctx context.Context) (any, error) { t(); return nil, nil }
	case func() any:
		fn = func(ctx context.Context) (any, error) { return t(), nil }
	case func(context.Context) (any, error):
		fn = t
	case TaskFunc:
		fn = t
	default:
		return nil, fmt.Errorf("task must be func(), func() any, func(context.Context)(any, error), or TaskFunc")
	}

	job, err := newJob(fn, cfg, &s.config)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := job.ID
	if cfg.Name != "" {
		if _, exists := s.jobs[cfg.Name]; exists {
			return nil, fmt.Errorf("job with name %q already exists", cfg.Name)
		}
		key = cfg.Name
	}

	s.jobs[key] = job
	return job, nil
}

// Start begins the scheduler's execution loop.
//
// The scheduler scans for due jobs at CheckInterval and executes them
// in separate goroutines. Returns an error if already started.
//
// In production, pair with os/signal for graceful shutdown:
//
//	quit := make(chan os.Signal, 1)
//	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
//	<-quit
//	s.Stop()
func (s *Scheduler) Start() error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("scheduler already started")
	}
	s.started = true
	s.mu.Unlock()

	s.ticker = time.NewTicker(s.config.CheckInterval)
	s.wg.Add(1)

	s.handleStartupMisfire()

	go s.run()
	return nil
}

func (s *Scheduler) run() {
	defer s.wg.Done()
	for {
		select {
		case now := <-s.ticker.C:
			s.checkJobs(now)
		case <-s.done:
			s.ticker.Stop()
			return
		}
	}
}

func (s *Scheduler) checkJobs(now time.Time) {
	s.mu.RLock()
	jobs := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	s.mu.RUnlock()

	for _, job := range jobs {
		job.mu.Lock()
		nowInTZ := now.In(job.location)
		isDue := !nowInTZ.Before(job.nextScheduled)

		if isDue {
			// Detect runtime misfire (e.g., system sleep)
			// If we missed the schedule by more than a single tick interval:
			if nowInTZ.Sub(job.nextScheduled) > s.config.CheckInterval {
				switch job.config.Misfire {
				case MisfireSkip:
					// Skip all missed runs, schedule for the next time AFTER now
					job.nextScheduled = job.parser.Next(nowInTZ)
					job.mu.Unlock()
					continue // Do not run the job now
				case MisfireRunOnce:
					// Run once now, then skip ahead to prevent catch-up loop
					job.nextScheduled = job.parser.Next(nowInTZ)
				case MisfireRunAll:
					// Let it catch up one by one (current behavior)
					job.nextScheduled = job.parser.Next(job.nextScheduled)
				}
			} else {
				// Normal execution, just advance by one interval
				job.nextScheduled = job.parser.Next(job.nextScheduled)
			}
		}
		job.mu.Unlock()

		if !isDue {
			continue
		}
		if !job.shouldRun(now) {
			continue
		}
		if job.isRunning() && job.config.Concurrency == ConcurrencyForbid {
			continue
		}
		if job.isRunning() && job.config.Concurrency == ConcurrencyReplace {
			job.cancelActive()
		}

		s.sem <- struct{}{}
		s.wg.Add(1)
		go func(j *Job) {
			defer s.wg.Done()
			defer func() { <-s.sem }()
			j.executeWithContext()
		}(job)
	}
}

func (s *Scheduler) handleStartupMisfire() {
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, job := range s.jobs {
		if job.config.Misfire == MisfireSkip || job.lastRun.IsZero() {
			continue
		}

		if now.Sub(job.lastRun) > 5*time.Minute {
			switch job.config.Misfire {
			case MisfireRunOnce:
				go job.executeWithContext()
			case MisfireRunAll:
				go func(j *Job) {
					for j.shouldRun(time.Now()) {
						j.executeWithContext()
						time.Sleep(100 * time.Millisecond)
					}
				}(job)
			}
		}
	}
}

// Stop gracefully shuts down the scheduler.
//
// Cancels all active job contexts, waits for running executions to complete,
// and stops the internal ticker. Safe to call multiple times.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.done)

		s.mu.Lock()
		for _, job := range s.jobs {
			job.cancel()
		}
		s.mu.Unlock()
		s.wg.Wait()
	})
}

// RemoveJob unregisters a job by name or ID.
//
// Returns true if the job was found and removed, false otherwise.
// Running jobs are cancelled via context but may complete before removal.
func (s *Scheduler) RemoveJob(identifier string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, exists := s.jobs[identifier]
	if !exists {
		return false
	}
	job.cancel()
	delete(s.jobs, identifier)
	return true
}

// ListJobs returns a slice of registered job identifiers (names or auto-generated IDs).
//
// The returned slice is a copy; modifications do not affect the scheduler.
// Useful for monitoring, debugging, or dynamic job management.
func (s *Scheduler) ListJobs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.jobs))
	for id := range s.jobs {
		ids = append(ids, id)
	}
	return ids
}
