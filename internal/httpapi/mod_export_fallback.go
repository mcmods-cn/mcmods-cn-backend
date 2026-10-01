package httpapi

import (
	"context"
	"errors"
	"time"

	"mcmods-cn-backend/internal/queue"
)

func (s *Server) queueTaskEnabled(code string) bool {
	tasks := queue.NormalizeConfig(s.cfg.NATS).Tasks
	if s.queue != nil {
		tasks = s.queue.Status().Tasks
	}
	for _, task := range tasks {
		if task.Code == code {
			return task.Enabled
		}
	}
	return false
}

func (s *Server) dispatchModExportJobContext(ctx context.Context, jobID string) {
	if ctx.Err() != nil || s.cfg.NATS.OutboxEnabled || !s.queueTaskEnabled(modExportTaskCode) {
		return
	}
	if s.queue != nil {
		err := s.queue.PublishTask(ctx, modExportTaskCode, modExportJobMessage{JobID: jobID})
		if err == nil {
			_, _ = s.db.Exec(ctx, `update nats_outbox set published_at=now() where aggregate_type='mod_export_job' and aggregate_id=$1 and published_at is null`, jobID)
			return
		}
		if errors.Is(err, queue.ErrTaskDisabled) || ctx.Err() != nil {
			return
		}
	}
	s.startModExportFallback(ctx, func(jobContext context.Context) {
		_ = s.importModExportJob(jobContext, jobID)
	})
}

// A full local pool leaves the durable queued row for the next dispatcher
// pass. Acquire before launching so neither goroutines nor large archives
// accumulate behind the concurrency bound.
func (s *Server) startModExportFallback(ctx context.Context, action func(context.Context)) bool {
	if ctx.Err() != nil || !s.queueTaskEnabled(modExportTaskCode) {
		return false
	}
	s.modExportFallbackOnce.Do(func() { s.modExportFallbackSlots = make(chan struct{}, 4) })
	select {
	case s.modExportFallbackSlots <- struct{}{}:
	case <-ctx.Done():
		return false
	default:
		return false
	}
	go func() {
		defer func() { <-s.modExportFallbackSlots }()
		jobContext, cancel := context.WithTimeout(ctx, 2*time.Hour)
		defer cancel()
		if jobContext.Err() == nil && s.queueTaskEnabled(modExportTaskCode) {
			action(jobContext)
		}
	}()
	return true
}
