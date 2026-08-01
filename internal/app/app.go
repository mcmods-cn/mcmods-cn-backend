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
	"mcmods-cn-backend/internal/httpapi"
)

func Run() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	if cfg.DB.ResetOnStart && cfg.Env != "development" {
		log.Fatal("DB_RESET_ON_START is only allowed when APP_ENV=development")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	availability := newAvailabilityHandler(cfg)
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           availability,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		log.Printf("mcmods-cn backend listening on %s; health endpoint is available while dependencies initialize", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	runtime, _, ready := waitForApplicationRuntime(ctx, cfg, availability)
	if !ready {
		shutdownHTTPServer(server)
		return
	}
	availability.setHandler(httpapi.NewServer(cfg, runtime.db, runtime.queue, runtime.activity))
	availability.resolveIssue("database")
	availability.resolveIssue("startup")
	log.Print("backend initialization completed; API traffic is enabled")
	go monitorDatabase(ctx, runtime.db, availability)

	<-ctx.Done()
	shutdownHTTPServer(server)
	runtime.close()
}

func shutdownHTTPServer(server *http.Server) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("http server shutdown: %v", err)
	}
}
