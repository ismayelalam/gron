package gron

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestJob_ContextTimeout(t *testing.T) {
	cfg := JobConfig{Name: "timeout", Schedule: "* * * * *", Timeout: 50 * time.Millisecond}
	s, _ := NewScheduler()

	done := make(chan struct{})
	j, _ := newJob(func(ctx context.Context) (any, error) {
		select {
		case <-time.After(5 * time.Second):
			return nil, nil
		case <-ctx.Done():
			close(done)
			return nil, ctx.Err()
		}
	}, cfg, &s.config)

	go j.executeWithContext()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout did not cancel context within expected time")
	}
}

func TestJob_RetryAndBackoff(t *testing.T) {
	var attempts atomic.Int32
	cfg := JobConfig{
		Name:     "retry",
		Schedule: "* * * * *",
		Retry:    2,
		Backoff:  &ExponentialBackoff{Base: 50 * time.Millisecond, Max: 200 * time.Millisecond, Factor: 2},
	}
	j, _ := newJob(func(ctx context.Context) (any, error) {
		attempts.Add(1)
		return nil, errors.New("transient")
	}, cfg, &SchedulerConfig{})

	start := time.Now()
	j.executeWithContext()

	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts (1 + 2 retries), got %d", attempts.Load())
	}
	// Base(50) + Base*Factor(100) = 150ms total delay
	if time.Since(start) < 100*time.Millisecond {
		t.Errorf("backoff delay too short: %v", time.Since(start))
	}
}

// TestJob_RetryContextCancellation verifies that if the scheduler is stopped
// while a job is sleeping between retries, the sleep is interrupted immediately
// rather than blocking for the full retry delay.
func TestJob_RetryContextCancellation(t *testing.T) {
	var attempts atomic.Int32
	cfg := JobConfig{
		Name:       "retry-cancel",
		Schedule:   "* * * * *",
		Retry:      5,
		RetryDelay: 2 * time.Second, // Intentionally long delay
	}

	s, _ := NewScheduler()
	j, _ := newJob(func(ctx context.Context) (any, error) {
		attempts.Add(1)
		return nil, errors.New("fail")
	}, cfg, &s.config)

	done := make(chan struct{})
	go func() {
		j.executeWithContext()
		close(done)
	}()

	// Wait for the first attempt to finish and enter the retry sleep
	time.Sleep(50 * time.Millisecond)

	// Cancel the job context (simulating Scheduler.Stop())
	j.cancel()

	select {
	case <-done:
		// Success: exited quickly
	case <-time.After(500 * time.Millisecond):
		t.Fatal("job did not exit quickly after context cancellation during retry sleep")
	}

	if attempts.Load() != 1 {
		t.Errorf("expected 1 attempt before cancellation, got %d", attempts.Load())
	}
}

func TestJob_PanicRecovery(t *testing.T) {
	var errCaptured error
	cfg := JobConfig{
		Name:     "panic",
		Schedule: "* * * * *",
		OnError:  func(err error, info JobInfo) { errCaptured = err },
	}
	j, _ := newJob(func(ctx context.Context) (any, error) {
		panic("simulated crash")
	}, cfg, &SchedulerConfig{})

	j.executeWithContext()

	if errCaptured == nil {
		t.Fatal("OnError callback not called after panic")
	}
	if errCaptured.Error() != "panic recovered: simulated crash" {
		t.Errorf("unexpected error message: %v", errCaptured)
	}
}

func TestJob_ErrorFilter(t *testing.T) {
	var successCalled atomic.Bool
	cfg := JobConfig{
		Name:        "filter",
		Schedule:    "* * * * *",
		Retry:       2,
		ErrorFilter: func(err error) bool { return err.Error() == "ignore" },
		OnSuccess:   func(out any, info JobInfo) { successCalled.Store(true) },
	}
	j, _ := newJob(func(ctx context.Context) (any, error) {
		return nil, errors.New("ignore")
	}, cfg, &SchedulerConfig{})

	j.executeWithContext()
	if !successCalled.Load() {
		t.Error("ErrorFilter should suppress error and trigger OnSuccess")
	}
}

func TestJob_RunCountLimit(t *testing.T) {
	var runs atomic.Int32
	cfg := JobConfig{Name: "limit", Schedule: "* * * * *", RunCount: 3}
	j, _ := newJob(func(ctx context.Context) (any, error) {
		runs.Add(1)
		return nil, nil
	}, cfg, &SchedulerConfig{})

	for range 5 {
		if !j.shouldContinue() {
			break
		}
		j.executeWithContext()
	}

	if runs.Load() != 3 {
		t.Errorf("expected 3 runs due to RunCount limit, got %d", runs.Load())
	}
}

func TestJob_DateWindow(t *testing.T) {
	now := time.Now()
	cfg := JobConfig{
		Name:      "window",
		Schedule:  "* * * * *",
		StartDate: now.Add(1 * time.Hour),
		EndDate:   now.Add(2 * time.Hour),
	}
	j, _ := newJob(func(ctx context.Context) (any, error) { return nil, nil }, cfg, &SchedulerConfig{})

	if j.shouldRun(now) {
		t.Error("job should not run before StartDate")
	}
	if j.shouldRun(now.Add(3 * time.Hour)) {
		t.Error("job should not run after EndDate")
	}
}
