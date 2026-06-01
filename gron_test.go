package gron

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_Lifecycle(t *testing.T) {
	s, err := NewScheduler(SchedulerConfig{
		CheckInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err == nil {
		t.Error("second Start() should return error")
	}

	// Test that stopping multiple times does not panic (verifies sync.Once fix)
	s.Stop()
	s.Stop()
	s.Stop()
}

func TestScheduler_JobRegistration(t *testing.T) {
	s, _ := NewScheduler()

	_, err := s.NewJob(func() {}, JobConfig{Name: "a", Schedule: "* * * * *"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.NewJob(func() {}, JobConfig{Name: "a", Schedule: "* * * * *"})
	if err == nil {
		t.Error("duplicate job name should fail")
	}

	_, err = s.NewJob(func() {}, JobConfig{Schedule: "* * * * *"})
	if err != nil {
		t.Fatal("auto-generated name registration failed")
	}

	if len(s.ListJobs()) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(s.ListJobs()))
	}

	if removed := s.RemoveJob("a"); !removed {
		t.Error("expected removal to return true")
	}
	if len(s.ListJobs()) != 1 {
		t.Errorf("expected 1 job after removal, got %d", len(s.ListJobs()))
	}
}

// TestScheduler_ConcurrencyForbid is a true integration test.
// It schedules a job that takes 50ms to run, but triggers every 10ms.
// The scheduler should skip the overlapping triggers, ensuring max concurrency is 1.
func TestScheduler_ConcurrencyForbid(t *testing.T) {
	var runs atomic.Int32
	var maxConcurrent atomic.Int32
	var currentConcurrent atomic.Int32

	s, _ := NewScheduler(SchedulerConfig{CheckInterval: 5 * time.Millisecond})

	s.NewJob(func(ctx context.Context) (any, error) {
		runs.Add(1)
		cur := currentConcurrent.Add(1)

		// Thread-safe update of max concurrent
		for {
			old := maxConcurrent.Load()
			if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
				break
			}
		}

		time.Sleep(50 * time.Millisecond) // Simulate long-running task
		currentConcurrent.Add(-1)
		return nil, nil
	}, JobConfig{Name: "forbid", Schedule: "@every 10ms", Concurrency: ConcurrencyForbid})

	s.Start()
	time.Sleep(250 * time.Millisecond) // Let it run a few cycles
	s.Stop()

	if maxConcurrent.Load() > 1 {
		t.Errorf("ConcurrencyForbid violated: max concurrent was %d", maxConcurrent.Load())
	}
	if runs.Load() == 0 {
		t.Error("expected at least one run")
	}
}

// TestScheduler_ConcurrencyReplace verifies that a new trigger cancels the old context
// and starts a fresh execution. We use a blocking channel for the first run to ensure
// it doesn't finish before being cancelled, and the second run finishes immediately.
func TestScheduler_ConcurrencyReplace(t *testing.T) {
	var runNum atomic.Int32
	var firstCancelled atomic.Bool
	var secondFinished atomic.Bool

	s, _ := NewScheduler(SchedulerConfig{CheckInterval: 5 * time.Millisecond})

	s.NewJob(func(ctx context.Context) (any, error) {
		n := runNum.Add(1)
		if n == 1 {
			// First run blocks until cancelled
			<-ctx.Done()
			firstCancelled.Store(true)
			return nil, ctx.Err()
		}
		// Second run finishes immediately
		secondFinished.Store(true)
		return nil, nil
	}, JobConfig{Name: "replace", Schedule: "@every 20ms", Concurrency: ConcurrencyReplace})

	s.Start()
	// t=0: Run 1 starts and blocks.
	// t=20: Run 2 triggers. Scheduler cancels Run 1, starts Run 2.
	// Run 2 finishes immediately.
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	if !firstCancelled.Load() {
		t.Error("first job should have been cancelled by Replace policy")
	}
	if !secondFinished.Load() {
		t.Error("second job should have finished")
	}
}

// TestScheduler_TaskAdaptation executes the adapted functions
// and verifies their outputs/errors via the OnSuccess/OnError callbacks.
func TestScheduler_TaskAdaptation(t *testing.T) {
	s, _ := NewScheduler()

	// Test func()
	var ran1 bool
	j1, err := s.NewJob(func() { ran1 = true }, JobConfig{Name: "t1", Schedule: "* * * * *"})
	if err != nil {
		t.Fatal(err)
	}
	j1.executeWithContext()
	if !ran1 {
		t.Error("func() not executed")
	}

	// Test func() any
	var out2 any
	j2, err := s.NewJob(func() any { return 42 }, JobConfig{
		Name: "t2", Schedule: "* * * * *",
		OnSuccess: func(output any, info JobInfo) { out2 = output },
	})
	if err != nil {
		t.Fatal(err)
	}
	j2.executeWithContext()
	if out2 != 42 {
		t.Errorf("func() any output wrong: %v", out2)
	}

	// Test func(context.Context) (any, error) - Success
	var out3 any
	var err3 error
	j3, err := s.NewJob(func(ctx context.Context) (any, error) { return "hello", nil }, JobConfig{
		Name: "t3", Schedule: "* * * * *",
		OnSuccess: func(output any, info JobInfo) { out3 = output },
		OnError:   func(err error, info JobInfo) { err3 = err },
	})
	if err != nil {
		t.Fatal(err)
	}
	j3.executeWithContext()
	if out3 != "hello" || err3 != nil {
		t.Errorf("context task success wrong: out=%v, err=%v", out3, err3)
	}

	// Test func(context.Context) (any, error) - Error
	var err4 error
	j4, err := s.NewJob(func(ctx context.Context) (any, error) { return nil, fmt.Errorf("fail") }, JobConfig{
		Name: "t4", Schedule: "* * * * *",
		OnError: func(err error, info JobInfo) { err4 = err },
	})
	if err != nil {
		t.Fatal(err)
	}
	j4.executeWithContext()
	if err4 == nil || err4.Error() != "fail" {
		t.Errorf("context task error wrong: %v", err4)
	}
}
