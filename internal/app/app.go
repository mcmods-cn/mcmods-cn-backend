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
	"mcmods-cn-backend/internal/runtimelog"
)

func Run() {
	runtimelog.Install()
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
		log.Printf("mcmods-cn backend listening on %s; live and ready probes are available while dependencies initialize", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	runtime, _, ready := waitForApplicationRuntime(ctx, cfg, availability)
	if !ready {
		shutdownHTTPServer(server)
		return
	}
	apiServer := httpapi.NewServer(ctx, cfg, runtime.db, runtime.queue, runtime.cache, runtime.activity, runtime.search)
	availability.setHandler(apiServer)
	availability.resolveIssue("database")
	availability.resolveIssue("startup")
	log.Print("backend initialization completed; API traffic is enabled")
	go monitorDatabase(ctx, runtime.db, availability)

	<-ctx.Done()
	shutdownHTTPServer(server)
	workerShutdownCtx, workerShutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := apiServer.Shutdown(workerShutdownCtx); err != nil {
		log.Printf("API worker shutdown: %v", err)
	}
	workerShutdownCancel()
	runtime.close()
}

func shutdownHTTPServer(server *http.Server) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("http server shutdown: %v", err)
	}
}
