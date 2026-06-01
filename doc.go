// Package gron provides a lightweight, zero-dependency cron job scheduler for Go.
//
// # Overview
//
// gron enables you to schedule and execute recurring tasks using standard cron
// expressions or human-readable descriptors like "@every 10s". It is designed
// for production use with features like context-aware execution, concurrency
// control, exponential backoff retries, misfire recovery, jitter, and full
// observability hooks—all without external dependencies.
//
// # Quick Start
//
//	s, err := gron.NewScheduler()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Register a simple job
//	s.NewJob(
//	    func() { fmt.Println("Hello, world!") },
//	    gron.JobConfig{
//	        Name:     "greeting",
//	        Schedule: "@every 5s",
//	        RunCount: 10,
//	    },
//	)
//
//	// Start the scheduler
//	if err := s.Start(); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Keep alive (use os/signal in production)
//	select {}
//
// # Task Signatures
//
// NewJob accepts functions with these signatures, automatically adapting them
// to the internal execution engine:
//
//   - func() — Simple fire-and-forget tasks; output passed to OnSuccess is nil
//   - func() any — Tasks returning a value; output passed to OnSuccess
//   - func(context.Context) (any, error) — Recommended for production; supports
//     timeouts, cancellation, and explicit error handling
//
// # Cron Expression Syntax
//
// gron supports standard 5-field cron expressions:
//
//	┌───────────── minute (0–59)
//	│ ┌───────────── hour (0–23)
//	│ │ ┌───────────── day of month (1–31)
//	│ │ │ ┌───────────── month (1–12)
//	│ │ │ │ ┌───────────── day of week (0–6, Sunday=0)
//	│ │ │ │ │
//	* * * * *
//
// Supported operators:
//   - * : any value
//   - */n : step values (e.g., */15 = every 15 minutes)
//   - n/m : start/step values (e.g., 5/15 = every 15 minutes starting at minute 5)
//   - n-m : ranges (e.g., 9-17 = 9am to 5pm)
//   - n,m : lists (e.g., 0,30 = minute 0 and 30)
//
// Descriptors: @yearly, @annually, @monthly, @weekly, @daily, @midnight, @hourly, @every <duration>
//
// # Configuration
//
// You can pass a SchedulerConfig struct to customize global behavior.
// If no configuration is provided, sensible defaults will be used.
// Zero values inside the provided struct will also be replaced by defaults.
//
// Examples:
//
//  1. With default configuration
//
//     s, err := gron.NewScheduler()
//
//  2. With custom configuration
//
//     s, err := gron.NewScheduler(gron.SchedulerConfig{
//     CheckInterval:     500 * time.Millisecond,
//     MaxConcurrentJobs: 50,
//     })
//
// # Concurrency & Safety
//
// Jobs execute in separate goroutines. The scheduler is thread-safe and uses
// mutex-protected state. Use ConcurrencyPolicy to control overlapping executions:
//
//   - ConcurrencyForbid: Skip if already running (DEFAULT; prevents overlap)
//   - ConcurrencyAllow: Run concurrently (for independent, idempotent jobs)
//   - ConcurrencyReplace: Cancel previous run, start new one (for real-time sync)
//
// # Reliability Features
//
//   - Timeouts & Context: Tasks receive a context.Context. Use JobConfig.Timeout
//     to automatically cancel tasks that run too long.
//   - Retries & Backoff: Configure JobConfig.Retry and JobConfig.Backoff (e.g.,
//     ExponentialBackoff) to automatically retry failed tasks.
//   - Misfire Recovery: Handle scheduler downtime with MisfirePolicy. Options
//     include MisfireSkip (default), MisfireRunOnce, and MisfireRunAll.
//   - Jitter: Add JobConfig.Jitter to prevent thundering herd problems when
//     many jobs are scheduled at the exact same time.
//
// # Observability
//
// Implement the Logger and Metrics interfaces to plug in structured logging
// and monitoring without adding external dependencies:
//
//	type Logger interface {
//	    Info(msg string, args ...any)
//	    Error(msg string, args ...any)
//	    Debug(msg string, args ...any)
//	}
//
//	type Metrics interface {
//	    RecordJobStart(name string)
//	    RecordJobSuccess(name string, duration time.Duration)
//	    RecordJobFailure(name string, duration time.Duration, err error)
//	}
//
// # Lifecycle Callbacks
//
// JobConfig provides hooks for execution lifecycle events:
// OnStart, OnSuccess, OnError, and OnFinish. These callbacks receive a
// JobInfo struct containing execution metadata (run number, duration, tags, etc.).
//
// # Examples
//
// See example_test.go for runnable examples that appear on pkg.go.dev.
//
// # Resources
//
//   - README: https://github.com/ismayelalam/gron#readme
//   - Issues: https://github.com/ismayelalam/gron/issues
//   - GoDoc: https://pkg.go.dev/github.com/ismayelalam/gron
package gron
