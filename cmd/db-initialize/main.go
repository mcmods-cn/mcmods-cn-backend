// Command db-initialize installs or checks the supported database baseline.
package main

import (
	"context"
	"flag"
	"log"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func main() {
	bootstrapAdmin := flag.Bool("bootstrap-admin", false, "create the initial administrator if it has never been bootstrapped")
	flag.Parse()
	if flag.NArg() != 0 {
		log.Fatal("db-initialize does not accept positional arguments")
	}
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid application configuration: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := database.Connect(ctx, cfg)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer db.Close()
	if *bootstrapAdmin {
		err = database.BootstrapAdmin(ctx, db)
	} else {
		err = database.Initialize(ctx, db)
	}
	if err != nil {
		log.Fatalf("initialize database: %v", err)
	}
	log.Print("database baseline is ready")
}
