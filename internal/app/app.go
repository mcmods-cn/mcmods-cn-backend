package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/httpapi"
	"mcmods-cn-backend/internal/queue"
)

func Run() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Connect(ctx, cfg)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer db.Close()

	if err := database.Migrate(ctx, db); err != nil {
		log.Fatalf("migrate database: %v", err)
	}
	if err := database.SeedRBAC(ctx, db); err != nil {
		log.Fatalf("seed permissions: %v", err)
	}

	natsCfg, err := database.LoadNATSConfig(ctx, db, cfg.NATS)
	if err != nil {
		log.Printf("load NATS config: %v", err)
		natsCfg = cfg.NATS
	}
	queueClient := queue.New(ctx, natsCfg)
	defer queueClient.Close()
	aiWorker := httpapi.NewAIWorker(db, queueClient)
	if err := aiWorker.Start(ctx); err != nil {
		log.Printf("ai queue worker unavailable: %v", err)
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.NewServer(cfg, db, queueClient),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("mcmods-cn backend listening on %s", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("http server shutdown: %v", err)
	}
}
