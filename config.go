package gron

import (
	"context"
	"time"
)

// TaskFunc defines the standard signature for scheduled tasks.
//
// Tasks receive a context.Context for timeout and cancellation support.
// Return (output, error) to enable rich callback handling:
//   - OnSuccess receives the output value
//   - OnError receives the error
//   - If error is non-nil, output may be partial or nil
type TaskFunc func(ctx context.Context) (any, error)

// ConcurrencyPolicy defines how the scheduler handles overlapping job executions.
type ConcurrencyPolicy int

const (
	// ConcurrencyUnset indicates the policy is not explicitly set.
	// The job will inherit the global SchedulerConfig.Concurrency,
	// defaulting to ConcurrencyForbid if the global config is also unset.
	ConcurrencyUnset ConcurrencyPolicy = iota

	// ConcurrencyForbid skips execution if the job is already running.
	// This is the default policy and prevents overlapping runs.
	ConcurrencyForbid

	// ConcurrencyAllow permits multiple concurrent executions of the same job.
	// Use for stateless, idempotent tasks.
	ConcurrencyAllow

	// ConcurrencyReplace cancels the currently running execution (via context)
	// and starts a new one. Use for real-time sync jobs where only the latest
	// execution matters.
	ConcurrencyReplace
)

// MisfirePolicy defines behavior when the scheduler restarts after downtime
// and detects jobs that should have run while stopped.
type MisfirePolicy int

const (
	// MisfireUnset indicates the policy is not explicitly set.
	// The job will inherit the global SchedulerConfig.Misfire,
	// defaulting to MisfireSkip if the global config is also unset.
	MisfireUnset MisfirePolicy = iota

	// MisfireSkip ignores missed executions. The job resumes on its next
	// scheduled time. This is the default policy.
	MisfireSkip

	// MisfireRunOnce executes the job once immediately after restart if it
	// missed its scheduled time, then resumes normal scheduling.
	MisfireRunOnce

	// MisfireRunAll attempts to catch up all missed executions sequentially.
	// Use with caution: may cause a burst of executions after long downtime.
	MisfireRunAll
)

// BackoffStrategy calculates the delay between retry attempts.
//
// Implement this interface to provide custom retry logic. The scheduler
// calls Delay(attempt) where attempt starts at 0 for the first retry.
type BackoffStrategy interface {
	// Delay returns the duration to wait before retry attempt number 'attempt'.
	// Attempt 0 is the first retry after initial failure.
	Delay(attempt int) time.Duration
}

// ExponentialBackoff implements BackoffStrategy with exponential growth.
//
// Delay = Base * Factor^attempt, capped at Max.
// Example: Base=1s, Factor=2, Max=30s → delays: 1s, 2s, 4s, 8s, 16s, 30s, 30s...
type ExponentialBackoff struct {
	// Base is the initial delay duration.
	Base time.Duration

	// Max is the maximum delay duration (cap).
	Max time.Duration

	// Factor is the multiplier applied per attempt (e.g., 2.0 for doubling).
	Factor float64
}

// Delay implements BackoffStrategy for ExponentialBackoff.
// Returns Base * Factor^attempt, capped at Max.
func (e *ExponentialBackoff) Delay(attempt int) time.Duration {
	if attempt <= 0 {
		return e.Base
	}
	delay := time.Duration(float64(e.Base) * e.Factor)
	for i := 1; i < attempt; i++ {
		delay = time.Duration(float64(delay) * e.Factor)
	}
	if delay > e.Max {
		delay = e.Max
	}
	return delay
}

// Logger interface for structured logging.
//
// Implement this interface to plug in your preferred logging framework
// (e.g., log/slog, zap, zerolog) without adding external dependencies to gron.
type Logger interface {
	// Info logs informational messages with optional key-value pairs.
	Info(msg string, args ...any)

	// Error logs error-level messages with optional key-value pairs.
	Error(msg string, args ...any)

	// Debug logs debug-level messages with optional key-value pairs.
	// May be suppressed in production builds.
	Debug(msg string, args ...any)
}

// Metrics interface for observability and monitoring.
//
// Implement this interface to export job metrics to Prometheus, DataDog,
// or custom monitoring systems. All methods are called synchronously during
// job execution; keep implementations fast or delegate to background workers.
type Metrics interface {
	// RecordJobStart is called when a job begins execution.
	RecordJobStart(name string)

	// RecordJobSuccess is called when a job completes successfully.
	// Duration is the elapsed time from start to completion.
	RecordJobSuccess(name string, duration time.Duration)

	// RecordJobFailure is called when a job fails after all retries.
	// Duration is the total execution time including retries.
	RecordJobFailure(name string, duration time.Duration, err error)
}

// JobInfo carries runtime context about a job execution.
//
// Passed to all lifecycle callbacks (OnStart, OnSuccess, OnError, OnFinish).
// Fields are populated by the scheduler; do not modify.
type JobInfo struct {
	// ID is the unique identifier for the job instance.
	ID string

	// Name is the user-provided job name (if any).
	Name string

	// RunNumber is the sequential execution count for this job (1-based).
	RunNumber int

	// StartedAt is the timestamp when execution began.
	StartedAt time.Time

	// CompletedAt is the timestamp when execution finished (success or failure).
	CompletedAt time.Time

	// Error is the last error encountered, or nil if execution succeeded.
	Error error

	// Output is the value returned by the task function, or nil if none.
	Output any

	// Tags is the map of metadata key-value pairs attached to the job.
	Tags map[string]string

	// Ctx is the context.Context for this execution, respecting Timeout
	// and cancellation signals.
	Ctx context.Context
}

// JobConfig defines the configuration for a scheduled job.
//
// All fields are optional except Schedule. Zero values use sensible defaults.
// For production use, prefer TaskFunc signature func(context.Context) (any, error)
// to leverage timeout and cancellation features.
type JobConfig struct {
	// Name is a unique identifier for the job. Used for lookup, removal,
	// and metrics labeling. If empty, an auto-generated ID is used.
	Name string

	// Schedule is a standard 5-field cron expression or descriptor.
	// Examples: "0 0 * * *" (midnight daily), "*/5 * * * *" (every 5 min),
	// "@every 10s", "@hourly". See package docs for full syntax.
	Schedule string

	// Timeout is the maximum duration a job is allowed to run. If exceeded,
	// the context passed to the task is cancelled. Zero means no timeout.
	Timeout time.Duration

	// Concurrency controls behavior when a job trigger occurs while the
	// previous execution is still running. Default: ConcurrencyForbid.
	Concurrency ConcurrencyPolicy

	// Retry is the maximum number of retry attempts after a task returns
	// a non-nil error. Zero means no retries (fail fast).
	Retry int

	// Backoff is the strategy for calculating delay between retries.
	// If nil, RetryDelay is used as a fixed interval.
	Backoff BackoffStrategy

	// RetryDelay is the fixed delay between retries when Backoff is nil.
	// Default: 5 seconds.
	RetryDelay time.Duration

	// StartDate is the earliest time the job should execute. Zero means
	// no start constraint. Times are evaluated in the job's Timezone.
	StartDate time.Time

	// EndDate is the latest time the job should execute. Zero means no
	// end constraint. Times are evaluated in the job's Timezone.
	EndDate time.Time

	// Tags is a map of metadata key-value pairs for filtering, grouping,
	// or labeling in logs and metrics.
	Tags map[string]string

	// Misfire defines behavior when the scheduler restarts after downtime
	// and detects this job missed its scheduled time. Default: MisfireSkip.
	Misfire MisfirePolicy

	// Jitter adds random delay ± this duration to the scheduled execution
	// time, preventing thundering herd when many jobs trigger simultaneously.
	Jitter time.Duration

	// OnSuccess is called when a job completes without error. Receives
	// JobInfo context and the task's output value.
	OnSuccess func(output any, info JobInfo)

	// OnError is called when a job fails after all retries are exhausted.
	// Receives JobInfo context and the final error.
	OnError func(err error, info JobInfo)

	// OnStart is called immediately before task execution begins. Receives
	// JobInfo with RunNumber and StartedAt populated.
	OnStart func(info JobInfo)

	// OnFinish is always called when execution completes (success or failure).
	// Receives JobInfo and final error (nil if success).
	OnFinish func(err error, info JobInfo)

	// ErrorFilter is called for each error returned by the task. Return true
	// to treat the error as non-fatal (skip retry, trigger OnSuccess).
	// Optional; default is to treat all errors as fatal.
	ErrorFilter func(err error) bool

	// Timezone is the IANA timezone name for evaluating the schedule and
	// date windows. Default: "UTC". Example: "America/New_York".
	Timezone string

	// RunCount is the maximum number of times the job should execute.
	// Zero means unlimited. Once reached, the job is automatically disabled.
	RunCount int
}

// SchedulerConfig defines global settings for the Scheduler.
// Zero values use sensible defaults.
type SchedulerConfig struct {
	// CheckInterval is how often the scheduler scans for due jobs.
	// Default: 1 second. Reduce for sub-minute precision; increase to
	// reduce CPU usage for low-frequency jobs.
	CheckInterval time.Duration

	// DefaultTimezone is the fallback timezone for jobs that don't specify
	// one in JobConfig. Default: "UTC".
	DefaultTimezone string

	// MaxConcurrentJobs limits the total number of jobs executing
	// simultaneously across the scheduler. Default: 1000. Use to bound
	// resource usage under load.
	MaxConcurrentJobs int

	// Logger is the structured logger for scheduler events. Optional;
	// if nil, logging is suppressed.
	Logger Logger

	// Metrics is the observability adapter for job metrics. Optional;
	// if nil, metrics collection is disabled.
	Metrics Metrics

	// Concurrency is the default concurrency policy for jobs that don't specify
	// one in JobConfig. Default: ConcurrencyForbid.
	Concurrency ConcurrencyPolicy

	// Misfire is the default misfire policy for jobs that don't specify
	// one in JobConfig. Default: MisfireSkip.
	Misfire MisfirePolicy
}
