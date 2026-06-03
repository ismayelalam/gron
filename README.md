# Gron

A lightweight, **zero-dependency** cron job scheduler for Go. Built for production with context-aware execution, concurrency control, intelligent retries, misfire recovery, and full observability hooks.

```go
// Works out of the box with your preferred syntax:
j, err := s.NewJob(
    func(ctx context.Context) (any, error) {
        return "task output", nil
    },
    gron.JobConfig{
        Name:        "data-sync",
        Schedule:    "*/10 * * * *",
        Timeout:     2 * time.Minute,
        Concurrency: gron.ConcurrencyForbid,
        OnSuccess: func(info gron.JobInfo, out any) {
            fmt.Printf("Run #%d | Output: %v\n", info.RunNumber, out)
        },
    },
)
```

---

## Features

| Category                | Capabilities                                                                 |
| ----------------------- | ---------------------------------------------------------------------------- |
| **Zero Dependencies**   | Pure Go standard library. No external modules, lightweight footprint         |
| **Context-Aware**       | `context.Context` passed to tasks. Supports timeouts & graceful cancellation |
| **Concurrency Control** | `Allow`, `Forbid` (skip), or `Replace` (cancel previous & restart)           |
| **Smart Retries**       | Configurable attempts + `ExponentialBackoff` or custom delay strategies      |
| **Misfire Recovery**    | `Skip`, `RunOnce`, or `RunAll` after downtime                                |
| **Jitter & Windows**    | Random execution delay (`Jitter`) + `StartDate`/`EndDate` scheduling         |
| **Lifecycle Hooks**     | `OnStart`, `OnSuccess`, `OnError`, `OnFinish` with rich `JobInfo`            |
| **Error Filtering**     | Ignore specific errors so they don't trigger retries                         |
| **Observability**       | Plug in `Logger` & `Metrics` interfaces without external dependencies        |
| **Thread-Safe**         | Mutex-protected state, non-blocking execution, panic recovery                |

---

## Installation

```bash
go get github.com/ismayelalam/gron
```

_Requires Go 1.24+_

---

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ismayelalam/gron"
)

func main() {
	// 1. Create scheduler with global options
	s, err := gron.NewScheduler(gron.SchedulerConfig{
	    CheckInterval:     500 * time.Millisecond,
	    MaxConcurrentJobs: 50,
	})
	if err != nil {
		panic(err)
	}

	// 2. Register a job
	j, err := s.NewJob(
		func(ctx context.Context) (any, error) {
			select {
			case <-time.After(1 * time.Second):
				return map[string]string{"status": "ok"}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		gron.JobConfig{
			Name:        "health-check",
			Schedule:    "@every 10s",
			Timeout:     30 * time.Second,
			Concurrency: gron.ConcurrencyForbid,
			Retry:       3,
			Backoff: &gron.ExponentialBackoff{
				Base: 1 * time.Second, Max: 10 * time.Second, Factor: 2.0,
			},
			StartDate: time.Now(),
			EndDate:   time.Now().AddDate(0, 6, 0),
			Tags:      map[string]string{"env": "prod", "team": "platform"},
			Jitter:    2 * time.Second,
			OnStart: func(info gron.JobInfo) {
				fmt.Printf("Starting run #%d\n", info.RunNumber)
			},
			OnSuccess: func(out any, info gron.JobInfo) {
				fmt.Printf("Success | Duration: %v | Output: %v\n",
					info.CompletedAt.Sub(info.StartedAt), out)
			},
			OnError: func(err error, info gron.JobInfo) {
				fmt.Printf("Failed | Run #%d | %v\n", info.RunNumber, err)
			},
			OnFinish: func(err error, info gron.JobInfo) {
				fmt.Println("Job finished")
			},
			ErrorFilter: func(err error) bool {
				return err != nil && err.Error() == "context deadline exceeded"
			},
			RunCount: 5, // Auto-stop after 5 runs
		},
	)
	if err != nil {
		panic(err)
	}

	// 3. Start & keep alive
	if err := s.Start(); err != nil {
		panic(err)
	}
	fmt.Printf("Scheduler running. Job ID: %s\n", j.ID)

	// In production: use os/signal for graceful shutdown
	<-make(chan struct{})
}
```

## test

## Configuration Reference

### Scheduler Options

```go
s, _ := gron.NewScheduler(gron.SchedulerConfig{
    CheckInterval:     1 * time.Second,   // Tick frequency
    DefaultTimezone:   "UTC",             // Fallback TZ
    MaxConcurrentJobs: 100,               // Global worker pool
    Logger:            myLogger,          // Implements Logger interface
    Metrics:           myMetrics,         // Implements Metrics interface
    Misfire:           gron.MisfireSkip,  // Default misfire behavior
    Concurrency:       gron.ConcurrencyForbid,  // Default concurrency forbid behavior
})
```

### JobConfig

| Field                   | Type                   | Purpose                                              |
| ----------------------- | ---------------------- | ---------------------------------------------------- |
| `Name`                  | `string`               | Unique identifier (used for lookup/removal)          |
| `Schedule`              | `string`               | Cron expression or `@every` descriptor               |
| `Timeout`               | `time.Duration`        | Max execution time. Cancels context if exceeded      |
| `Concurrency`           | `ConcurrencyPolicy`    | `Allow` / `Forbid` (default) / `Replace`             |
| `Retry`                 | `int`                  | Max retry attempts on error                          |
| `Backoff`               | `BackoffStrategy`      | Custom delay calculator (e.g., `ExponentialBackoff`) |
| `RetryDelay`            | `time.Duration`        | Fixed delay fallback if `Backoff` is nil             |
| `StartDate` / `EndDate` | `time.Time`            | Schedule only within this window                     |
| `Tags`                  | `map[string]string`    | Metadata for filtering/metrics                       |
| `Misfire`               | `MisfirePolicy`        | `Skip` / `RunOnce` / `RunAll` after restart          |
| `Jitter`                | `time.Duration`        | Random ±delay to prevent thundering herd             |
| `OnStart`               | `func(JobInfo)`        | Called right before execution                        |
| `OnSuccess`             | `func(JobInfo, any)`   | Called on success, receives task output              |
| `OnError`               | `func(JobInfo, error)` | Called on failure, receives error                    |
| `OnFinish`              | `func(JobInfo, error)` | Always called (success or failure)                   |
| `ErrorFilter`           | `func(error) bool`     | Return `true` to ignore error & skip retry           |
| `Timezone`              | `string`               | IANA timezone (defaults to scheduler TZ)             |
| `RunCount`              | `int`                  | Max executions. `0` = unlimited                      |

### JobInfo

Passed to all callbacks. Contains execution context:

```go
type JobInfo struct {
    ID          string      // Unique job ID
    Name        string      // Job name
    RunNumber   int         // Current execution count
    StartedAt   time.Time   // Start timestamp
    CompletedAt time.Time   // End timestamp
    Error       error       // Last error (if any)
    Output      any         // Value returned by the task
    Tags        map[string]string // Job metadata
    Ctx         context.Context   // Execution context
}
```

---

## Task Signatures

`NewJob` automatically adapts your function to the internal engine:

| Signature                            | Use Case                       | Output in `OnSuccess`            |
| ------------------------------------ | ------------------------------ | -------------------------------- |
| `func()`                             | Simple fire-and-forget         | `nil`                            |
| `func() any`                         | Returns a result               | Returned value                   |
| `func(context.Context) (any, error)` | **Recommended** for production | Returned value (if `err == nil`) |

**Example:**

```go
s.NewJob(
    func() { fmt.Println("hello") },
    gron.JobConfig{ Schedule: "@every 5s" },
)
```

---

## Advanced Patterns

### Graceful Shutdown

```go
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit
s.Stop() // Cancels active contexts & waits for running jobs
```

### Custom Backoff Strategy

```go
type LinearBackoff struct { Base, Step time.Duration }

func (l *LinearBackoff) Delay(attempt int) time.Duration {
    return l.Base + l.Step*time.Duration(attempt)
}
```

### Observability Interfaces

```go
type Logger interface {
    Info(msg string, args ...any)
    Error(msg string, args ...any)
    Debug(msg string, args ...any)
}

type Metrics interface {
    RecordJobStart(name string)
    RecordJobSuccess(name string, duration time.Duration)
    RecordJobFailure(name string, duration time.Duration, err error)
}
```

---

## Supported Cron Syntax

Standard 5-field format: `minute hour day-of-month month day-of-week`

| Syntax      | Example                                      | Meaning            |
| ----------- | -------------------------------------------- | ------------------ |
| `*`         | `* * * * *`                                  | Every minute       |
| `*/n`       | `*/15 * * * *`                               | Every 15 minutes   |
| `n`         | `30 9 * * *`                                 | At 09:30 daily     |
| `n-m`       | `0 9-17 * * 1-5`                             | 9am–5pm, Mon–Fri   |
| `n,m`       | `0,30 * * * *`                               | At minute 0 and 30 |
| Descriptors | `@hourly`, `@daily`, `@weekly`, `@every 10s` | Readable aliases   |

---

## API Reference

| Method                                                      | Description                                                                      |
| ----------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `NewScheduler(cfgs ...SchedulerConfig) (*Scheduler, error)` | Creates a scheduler with global config (pass struct or leave empty for defaults) |
| `s.NewJob(task any, cfg JobConfig) (*Job, error)`           | Registers a new job                                                              |
| `s.Start() error`                                           | Begins the scheduling loop & handles startup misfire                             |
| `s.Stop()`                                                  | Gracefully shuts down, cancels contexts, waits for workers                       |
| `s.RemoveJob(idOrName string) bool`                         | Removes a job by identifier                                                      |
| `s.ListJobs() []string`                                     | Returns registered job identifiers                                               |

---

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing`)
3. Commit changes (`git commit -m 'Add amazing feature'`)
4. Push & open a Pull Request

_Include tests for new cron edge cases, concurrency scenarios, or context cancellation paths._

---

## License

MIT License. See `LICENSE` for details.

---

> **Production Tips**:
>
> - Prefer `func(context.Context) (any, error)` to leverage timeouts & cancellation
> - Use `ConcurrencyForbid` for stateful jobs, `ConcurrencyReplace` for real-time syncs
> - Implement `Metrics` to feed Prometheus/DataDog without adding direct deps
> - Pair with `os/signal` + `context.WithCancel` for clean process termination
> - The scheduler scans every `CheckInterval`. Set to `500ms` for high-precision sub-minute scheduling

`gron` is designed with extension points. Open an issue to discuss!
