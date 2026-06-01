package gron

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// Job represents a scheduled task with its configuration and execution state.
//
// Created via Scheduler.NewJob. Most interaction is through callbacks
// (OnStart, OnSuccess, etc.) rather than direct method calls.
type Job struct {
	ID            string
	Name          string
	task          TaskFunc
	config        JobConfig
	parser        *CronParser
	location      *time.Location
	mu            sync.Mutex
	runCount      int
	runningCount  int
	activeExecID  uint64
	ctx           context.Context
	ctxCancel     context.CancelFunc
	activeCancel  context.CancelFunc
	lastRun       time.Time
	globalCfg     *SchedulerConfig
	nextScheduled time.Time
}

func newJob(task TaskFunc, cfg JobConfig, globalCfg *SchedulerConfig) (*Job, error) {
	if task == nil {
		return nil, fmt.Errorf("task function cannot be nil")
	}
	if cfg.Schedule == "" {
		return nil, fmt.Errorf("schedule cannot be empty")
	}

	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", cfg.Timezone, err)
	}

	parser, err := NewCronParser(cfg.Schedule)
	if err != nil {
		return nil, fmt.Errorf("invalid cron schedule %q: %w", cfg.Schedule, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now().In(loc)
	return &Job{
		ID:            fmt.Sprintf("job_%s_%d_%d", cfg.Name, time.Now().UnixNano(), rand.Int63()),
		Name:          cfg.Name,
		task:          task,
		config:        cfg,
		parser:        parser,
		location:      loc,
		ctx:           ctx,
		ctxCancel:     cancel,
		globalCfg:     globalCfg,
		nextScheduled: parser.Next(now),
	}, nil
}

func (j *Job) shouldRun(now time.Time) bool {
	if !j.config.StartDate.IsZero() && now.Before(j.config.StartDate) {
		return false
	}
	if !j.config.EndDate.IsZero() && now.After(j.config.EndDate) {
		return false
	}
	return j.shouldContinue()
}

func (j *Job) executeWithContext() {
	j.setRunning(true)
	defer j.setRunning(false)

	// Sleep for a random duration between 0 and Jitter
	if j.config.Jitter > 0 {
		time.Sleep(time.Duration(rand.Int63n(int64(j.config.Jitter))))
	}
	j.incrementRunCount()
	execCtx, cancel := context.WithCancel(j.ctx)
	j.mu.Lock()
	j.activeExecID++
	myID := j.activeExecID
	j.activeCancel = cancel
	j.mu.Unlock()
	defer func() {
		cancel()
		j.mu.Lock()
		if j.activeExecID == myID {
			j.activeCancel = nil
		}
		j.mu.Unlock()
		cancel()
	}()

	info := JobInfo{
		ID:        j.ID,
		Name:      j.Name,
		RunNumber: j.getRunCount(),
		StartedAt: time.Now(),
		Tags:      j.config.Tags,
		Ctx:       execCtx,
	}

	if j.config.Timeout > 0 {
		timeoutCtx, cancelTimeout := context.WithTimeout(execCtx, j.config.Timeout)
		defer cancelTimeout()
		execCtx = timeoutCtx
		info.Ctx = execCtx
	}

	j.log("debug", "job starting", "run", info.RunNumber)
	j.callMetrics("start")
	if j.config.OnStart != nil {
		j.config.OnStart(info)
	}

	var output any
	var lastErr error
	var attempt int

	for attempt <= j.config.Retry {
		func() {
			defer func() {
				if r := recover(); r != nil {
					lastErr = fmt.Errorf("panic recovered: %v", r)
				}
			}()
			output, lastErr = j.task(execCtx)
		}()

		if lastErr == nil {
			break
		}

		if j.config.ErrorFilter != nil && j.config.ErrorFilter(lastErr) {
			lastErr = nil
			break
		}

		delay := j.config.RetryDelay
		if j.config.Backoff != nil {
			delay = j.config.Backoff.Delay(attempt)
		}
		if delay > 0 && attempt < j.config.Retry {
			select {
			case <-time.After(delay): // Delay finished normally, proceed to next retry
			case <-execCtx.Done(): // Context was cancelled (e.g., scheduler stopped or job replaced)
				// Exit immediately without attempting further retries
				return
			}
		}
		attempt++
	}

	info.CompletedAt = time.Now()
	info.Error = lastErr
	info.Output = output

	if lastErr == nil {
		j.callMetrics("success", info)
	} else {
		j.callMetrics("failure", info)
	}

	if lastErr != nil && j.config.OnError != nil {
		j.config.OnError(lastErr, info)
	} else if lastErr == nil && j.config.OnSuccess != nil {
		j.config.OnSuccess(output, info)
	}
	if j.config.OnFinish != nil {
		j.config.OnFinish(lastErr, info)
	}

	j.log("debug", "job finished", "run", info.RunNumber, "err", lastErr)
	j.mu.Lock()
	j.lastRun = time.Now()
	j.mu.Unlock()
}

func (j *Job) getRunCount() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.runCount
}

func (j *Job) incrementRunCount() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.runCount++
}

func (j *Job) shouldContinue() bool {
	if j.config.RunCount == 0 {
		return true
	}
	return j.getRunCount() < j.config.RunCount
}

func (j *Job) isRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.runningCount > 0
}

func (j *Job) setRunning(running bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if running {
		j.runningCount++
	} else {
		j.runningCount--
	}
}

func (j *Job) cancelActive() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.activeCancel != nil {
		j.activeCancel()
	}
}

func (j *Job) cancel() {
	if j.ctxCancel != nil {
		j.ctxCancel()
	}
}

func (j *Job) log(level string, msg string, args ...any) {
	if j.globalCfg.Logger == nil {
		return
	}
	fullArgs := append([]any{"id", j.ID, "name", j.Name}, args...)
	switch level {
	case "debug":
		j.globalCfg.Logger.Debug(msg, fullArgs...)
	case "error":
		j.globalCfg.Logger.Error(msg, fullArgs...)
	default:
		j.globalCfg.Logger.Info(msg, fullArgs...)
	}
}

func (j *Job) callMetrics(event string, info ...JobInfo) {
	if j.globalCfg.Metrics == nil {
		return
	}
	metricName := j.Name
	if metricName == "" {
		metricName = j.ID
	}

	switch event {
	case "start":
		j.globalCfg.Metrics.RecordJobStart(metricName)
	case "success":
		i := info[0]
		j.globalCfg.Metrics.RecordJobSuccess(metricName, i.CompletedAt.Sub(i.StartedAt))
	case "failure":
		i := info[0]
		j.globalCfg.Metrics.RecordJobFailure(metricName, i.CompletedAt.Sub(i.StartedAt), i.Error)
	}
}
