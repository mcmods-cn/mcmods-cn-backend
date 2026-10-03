package httpapi

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// Follow and block mutations serialize on the unordered user pair. The lock
// survives until transaction completion, including the notification outbox.
func lockUserRelationshipPairTx(ctx context.Context, tx pgx.Tx, first, second int64) error {
	low, high := orderedUserIDs(first, second)
	key := "user-relationship:" + strconv.FormatInt(low, 10) + ":" + strconv.FormatInt(high, 10)
	_, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1,0))`, key)
	return err
}
