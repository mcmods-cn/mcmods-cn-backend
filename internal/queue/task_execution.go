package queue

import (
	"context"
	"sync/atomic"
	"time"
)

type taskRunCounters struct {
	active   atomic.Int64
	finished atomic.Uint64
	failed   atomic.Uint64
	millis   atomic.Uint64
}

// RunTask applies one per-instance execution budget to subscriptions, local
// outbox delivery and database recovery. Reconfiguration affects new starts;
// already-running work retains its original deadline.
func (c *Client) RunTask(ctx context.Context, code string, operation func(context.Context) error) error {
	runs, timeout, err := c.acquireTaskSlot(ctx, code)
	if err != nil {
		return err
	}
	defer func() {
		c.mu.Lock()
		runs.active.Add(-1)
		c.notifyCapacityLocked()
		c.mu.Unlock()
	}()
	work, cancel := context.WithTimeout(ctx, timeout)
	stop := context.AfterFunc(c.ctx, cancel)
	defer stop()
	defer cancel()
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if err := work.Err(); err != nil {
		return err
	}
	start := time.Now()
	err = operation(work)
	if err != nil {
		runs.failed.Add(1)
	}
	runs.millis.Add(uint64(time.Since(start).Milliseconds()))
	runs.finished.Add(1)
	return err
}

func (c *Client) acquireTaskSlot(ctx context.Context, code string) (*taskRunCounters, time.Duration, error) {
	if c == nil {
		return nil, 0, ErrUnavailable
	}
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, 0, ErrUnavailable
		}
		task, exists := findTask(c.cfg.Tasks, code)
		if !exists || !task.Enabled {
			c.mu.Unlock()
			return nil, 0, ErrTaskDisabled
		}
		runs := c.taskRuns[task.Code]
		if runs == nil {
			runs = &taskRunCounters{}
			c.taskRuns[task.Code] = runs
		}
		if runs.active.Load() < int64(task.MaxConcurrent) {
			runs.active.Add(1)
			c.mu.Unlock()
			return runs, time.Duration(task.TimeoutSeconds) * time.Second, nil
		}
		if c.capacityChanged == nil {
			c.capacityChanged = make(chan struct{})
		}
		changed := c.capacityChanged
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		case <-c.ctx.Done():
			return nil, 0, c.ctx.Err()
		case <-changed:
		}
	}
}

func (c *Client) notifyCapacityLocked() {
	if c.capacityChanged != nil {
		close(c.capacityChanged)
		c.capacityChanged = nil
	}
}

type TaskRuns struct {
	Active        int64  `json:"active"`
	Finished      uint64 `json:"finished"`
	Failed        uint64 `json:"failed"`
	ElapsedMillis uint64 `json:"elapsedMillis"`
}

func (c *Client) TaskRuns() map[string]TaskRuns {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]TaskRuns, len(c.taskRuns))
	for code, runs := range c.taskRuns {
		result[code] = TaskRuns{Active: runs.active.Load(), Finished: runs.finished.Load(),
			Failed: runs.failed.Load(), ElapsedMillis: runs.millis.Load()}
	}
	return result
}
