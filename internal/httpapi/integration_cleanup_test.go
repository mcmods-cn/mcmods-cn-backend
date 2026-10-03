package httpapi

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Defer immediately after Begin, passing the transaction value rather than a
// mutable variable. A failed assertion must release the borrowed connection
// before pool cleanup, even when the test's work deadline has already expired.
func rollbackIntegrationTransaction(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
