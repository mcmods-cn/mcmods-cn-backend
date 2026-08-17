package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/httpapi"
	"mcmods-cn-backend/internal/progression"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/searchindex"
)

type applicationRuntime struct {
	db         *pgxpool.Pool
	activityDB *pgxpool.Pool
	queue      *queue.Client
	activity   *activity.Monitor
	search     *searchindex.Client
	cache      *querycache.Cache
}

type runtimeInitializationError struct {
	Component     string
	Code          string
	PublicMessage string
	Err           error
}

func (e *runtimeInitializationError) Error() string {
	return e.Code + ": " + e.Err.Error()
}

func (e *runtimeInitializationError) Unwrap() error {
	return e.Err
}

func waitForApplicationRuntime(ctx context.Context, cfg config.Config, availability *availabilityHandler) (*applicationRuntime, int, bool) {
	startedAt := time.Now()
	failedAttempts := 0
	for {
		runtime, err := initializeApplicationRuntime(ctx, cfg)
		if err == nil {
			if failedAttempts > 0 {
				duration := time.Since(startedAt)
				log.Printf("backend dependencies recovered after %d failed initialization attempt(s) in %s", failedAttempts, duration.Round(time.Millisecond))
				recordRuntimeEvent(ctx, runtime.db, "warning", "backend_initialization_recovered", map[string]any{
					"failedAttempts": failedAttempts,
					"durationMs":     duration.Milliseconds(),
				})
			}
			return runtime, failedAttempts, true
		}
		failedAttempts++
		failure := runtimeInitializationFailure(err)
		availability.reportIssue(failure.Component, failure.Code, failure.PublicMessage)
		delay := runtimeRetryDelay(failedAttempts)
		log.Printf("backend initialization attempt %d failed: %v; retrying in %s", failedAttempts, err, delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, failedAttempts, false
		case <-timer.C:
		}
	}
}

func runtimeInitializationFailure(err error) *runtimeInitializationError {
	if failure, ok := err.(*runtimeInitializationError); ok {
		return failure
	}
	return &runtimeInitializationError{
		Component: "backend", Code: "backend_initialization_failed",
		PublicMessage: "backend initialization failed", Err: err,
	}
}

func initializeApplicationRuntime(ctx context.Context, cfg config.Config) (*applicationRuntime, error) {
	db, err := database.Connect(ctx, cfg)
	if err != nil {
		return nil, &runtimeInitializationError{
			Component: "database", Code: "database_unavailable",
			PublicMessage: "database is unavailable", Err: fmt.Errorf("connect database: %w", err),
		}
	}
	initialized := false
	var activityDB *pgxpool.Pool
	defer func() {
		if !initialized {
			if activityDB != nil {
				activityDB.Close()
			}
			db.Close()
		}
	}()

	if cfg.DB.ResetOnStart {
		if err = database.ResetDevelopmentSchema(ctx, db); err != nil {
			return nil, databaseInitializationError("reset development database", err)
		}
		log.Print("development database schema reset completed")
	}
	if err = database.Migrate(ctx, db); err != nil {
		return nil, databaseInitializationError("migrate database", err)
	}
	if err = database.SeedRBAC(ctx, db); err != nil {
		return nil, databaseInitializationError("seed permissions", err)
	}
	activityDB, err = database.ConnectActivity(ctx, cfg)
	if err != nil {
		return nil, &runtimeInitializationError{
			Component: "database", Code: "activity_database_unavailable",
			PublicMessage: "activity database writer is unavailable", Err: fmt.Errorf("connect activity database pool: %w", err),
		}
	}
	sharedCache := querycache.New(cfg.Redis)
	if cfg.Redis.Required {
		redisCtx, redisCancel := context.WithTimeout(ctx, 3*time.Second)
		err = sharedCache.Ping(redisCtx)
		redisCancel()
		if err != nil {
			sharedCache.Close()
			return nil, &runtimeInitializationError{
				Component: "redis", Code: "redis_required_unavailable",
				PublicMessage: "required cache service is unavailable", Err: err,
			}
		}
	}
	httpapi.StartUnreadReconciliation(ctx, db, sharedCache, cfg.Redis.UnreadCounterEnabled, cfg.Redis.UnreadReconcileInterval, cfg.Redis.UnreadReconcileBatchSize)

	httpapi.StartMinecraftVersionSyncScheduler(ctx, db)
	httpapi.StartMinecraftServerProbeScheduler(ctx, db)
	httpapi.StartPopularityRefreshScheduler(ctx, db)
	httpapi.NewOSSDeletionWorker(cfg, db).Start(ctx)
	httpapi.NewMaintenanceWorker(db).Start(ctx)
	httpapi.NewSeedCrawlerWorker(cfg, db).Start(ctx)
	httpapi.NewProjectAutomationWorker(cfg, db).Start(ctx)
	httpapi.NewActivityRetentionWorker(db).Start(ctx)
	progressionService := progression.NewService(db)
	activityMonitor := activity.NewMonitor(activityDB, progressionService.ProcessActivityBatch, activity.Options{
		BatchSize: cfg.Activity.BatchSize, QueueCapacity: cfg.Activity.QueueCapacity,
		FlushInterval: cfg.Activity.FlushInterval, RetryMinDelay: cfg.Activity.RetryMinDelay,
		RetryMaxDelay: cfg.Activity.RetryMaxDelay, WriteTimeout: cfg.Activity.WriteTimeout,
		DurableEnqueueTimeout: cfg.Activity.DurableEnqueueTimeout,
	})
	natsCfg, loadErr := database.LoadNATSConfig(ctx, db, cfg.NATS, cfg.SettingsEncryptionKey)
	if loadErr != nil {
		log.Printf("load NATS config: %v", loadErr)
		natsCfg = cfg.NATS
	}
	queueClient := queue.New(ctx, natsCfg)
	queueClient.SetDeadLetterSink(queue.NewPostgresDeadLetterSink(db))
	aiWorker := httpapi.NewAIWorker(db, queueClient, cfg.SettingsEncryptionKey)
	if err = aiWorker.Start(ctx); err != nil {
		log.Printf("ai queue worker unavailable: %v", err)
	}
	notificationWorker := httpapi.NewNotificationWorker(db, queueClient, sharedCache, cfg.SMTP, cfg.SettingsEncryptionKey)
	if err = notificationWorker.Start(); err != nil {
		log.Printf("notification queue worker unavailable: %v", err)
	}
	modExportWorker := httpapi.NewModExportWorker(cfg, db, queueClient)
	if err = modExportWorker.Start(); err != nil {
		log.Printf("mod catalog import worker unavailable; API fallback remains enabled: %v", err)
	}
	modMetadataWorker := httpapi.NewModMetadataImportWorker(cfg, db, queueClient)
	if err = modMetadataWorker.Start(ctx); err != nil {
		log.Printf("mod metadata import worker unavailable: %v", err)
	}
	blueprintWorker := httpapi.NewBlueprintWorker(cfg, db, queueClient)
	if err = blueprintWorker.Start(ctx); err != nil {
		log.Printf("blueprint worker unavailable; queued jobs remain recoverable: %v", err)
	}
	outboxDispatcher := queue.NewOutboxDispatcher(db, queueClient, natsCfg.OutboxEnabled)
	outboxDispatcher.Start(ctx)
	searchClient := searchindex.New(cfg.Typesense)
	searchindex.NewWorker(db, searchClient).Start(ctx)

	initialized = true
	return &applicationRuntime{db: db, activityDB: activityDB, queue: queueClient, activity: activityMonitor, search: searchClient, cache: sharedCache}, nil
}

func databaseInitializationError(action string, err error) error {
	return &runtimeInitializationError{
		Component: "database", Code: "database_initialization_failed",
		PublicMessage: "database initialization failed", Err: fmt.Errorf("%s: %w", action, err),
	}
}

func runtimeRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := 2 * time.Second
	for current := 1; current < attempt && delay < 30*time.Second; current++ {
		delay *= 2
	}
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func monitorDatabase(ctx context.Context, db *pgxpool.Pool, availability *availabilityHandler) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var outageStarted time.Time
	var lastFailureLog time.Time
	var lastError string
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			err := db.Ping(pingCtx)
			cancel()
			if err != nil {
				lastError = err.Error()
				availability.reportIssue("database", "database_unavailable", "database is unavailable")
				if outageStarted.IsZero() {
					outageStarted = time.Now()
					lastFailureLog = outageStarted
					log.Printf("database health check failed; API entered degraded mode: %v", err)
				} else if time.Since(lastFailureLog) >= time.Minute {
					lastFailureLog = time.Now()
					log.Printf("database remains unavailable after %s: %v", time.Since(outageStarted).Round(time.Second), err)
				}
				continue
			}
			if outageStarted.IsZero() {
				continue
			}
			duration := time.Since(outageStarted)
			availability.resolveIssue("database")
			log.Printf("database connection recovered after %s; API traffic resumed", duration.Round(time.Millisecond))
			recordRuntimeEvent(ctx, db, "warning", "database_connection_recovered", map[string]any{
				"outageStartedAt": outageStarted.UTC(),
				"durationMs":      duration.Milliseconds(),
				"lastError":       lastError,
			})
			outageStarted = time.Time{}
			lastFailureLog = time.Time{}
			lastError = ""
		}
	}
}

func recordRuntimeEvent(ctx context.Context, db *pgxpool.Pool, level, action string, payload any) {
	raw, _ := json.Marshal(payload)
	writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := db.Exec(writeCtx, `insert into app_logs(category,level,action,target,payload)
		values('system',$1,$2,'backend_runtime',$3::jsonb)`, level, action, string(raw)); err != nil {
		log.Printf("record runtime event %s: %v", action, err)
	}
}

func (runtime *applicationRuntime) close() {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if runtime.activity != nil {
		if err := runtime.activity.Close(shutdownCtx); err != nil {
			log.Printf("activity monitor shutdown: %v", err)
		}
	}
	if runtime.activityDB != nil {
		runtime.activityDB.Close()
	}
	if runtime.queue != nil {
		runtime.queue.Close()
	}
	if runtime.cache != nil {
		runtime.cache.Close()
	}
	if runtime.db != nil {
		runtime.db.Close()
	}
}
