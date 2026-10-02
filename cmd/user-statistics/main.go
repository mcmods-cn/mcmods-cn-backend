package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/userstats"
)

func main() {
	userID := flag.String("user", "", "optional public user ID; empty recalibrates all users")
	batchSize := flag.Int("batch-size", 200, "number of users loaded per batch")
	flag.Parse()
	ctx := context.Background()
	cfg := config.Load()
	db, err := database.Connect(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	processed, err := userstats.Reconcile(ctx, db, userstats.Options{UserPublicID: *userID, BatchSize: *batchSize})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("reconciled %d user(s)\n", processed)
}
