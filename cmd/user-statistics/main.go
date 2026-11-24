package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/userstats"
)

func parseScope(flags *flag.FlagSet, args []string) (userstats.Options, error) {
	var options userstats.Options
	flags.Func("user", "public user ID; omit to recalibrate all users", func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("an explicit user ID cannot be empty")
		}
		options.UserPublicID = value
		return nil
	})
	flags.IntVar(&options.BatchSize, "batch-size", 200, "number of users loaded per batch")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, fmt.Errorf("unexpected positional argument; select one user with -user <public ID>")
	}
	return options, nil
}

func main() {
	options, err := parseScope(flag.CommandLine, os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	cfg := config.Load()
	db, err := database.Connect(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	processed, err := userstats.Reconcile(ctx, db, options)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("reconciled %d user(s)\n", processed)
}
