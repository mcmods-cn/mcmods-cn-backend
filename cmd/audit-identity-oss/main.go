// Command audit-identity-oss reports OSS identity shape and unverified file counts.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func main() {
	target := flag.String("database", "", "verify this exact database name before inspection")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "audit-identity-oss does not accept positional arguments")
		os.Exit(2)
	}
	cfg := config.Load()
	name, err := cfg.DB.EffectiveName()
	if err != nil || (*target != "" && name != *target) {
		fmt.Fprintln(os.Stderr, "database configuration does not match the requested audit target")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := database.Connect(ctx, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not connect to the audit database")
		os.Exit(1)
	}
	defer pool.Close()
	state, err := database.InspectAuditIdentityOSS(ctx, pool)
	if err != nil || state.Database != name {
		fmt.Fprintln(os.Stderr, "identity and OSS inspection failed or reached a different database:", err)
		os.Exit(1)
	}
	if err = json.NewEncoder(os.Stdout).Encode(state); err != nil {
		os.Exit(1)
	}
}
