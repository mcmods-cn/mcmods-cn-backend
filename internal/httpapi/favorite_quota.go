package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

const (
	defaultFavoriteCollectionsPerUser = int64(100)
	defaultFavoriteItemsPerUser       = int64(10_000)
	hardFavoriteCollectionsPerUser    = int64(1_000)
	hardFavoriteItemsPerUser          = int64(100_000)
)

var (
	errFavoriteCollectionQuota = errors.New("favorite collection quota exceeded")
	errFavoriteItemQuota       = errors.New("favorite item quota exceeded")
)

type favoriteQuotaPolicy struct {
	MaximumCollections int64
	MaximumItems       int64
}

type favoriteMembershipTransaction struct {
	connection *pgxpool.Conn
	tx         pgx.Tx
	userID     int64
}

func normalizedFavoriteQuotaPolicy(value config.FavoriteConfig) favoriteQuotaPolicy {
	return favoriteQuotaPolicy{
		MaximumCollections: normalizedFavoriteQuotaLimit(value.MaxCollectionsPerUser,
			defaultFavoriteCollectionsPerUser, hardFavoriteCollectionsPerUser),
		MaximumItems: normalizedFavoriteQuotaLimit(value.MaxItemsPerUser,
			defaultFavoriteItemsPerUser, hardFavoriteItemsPerUser),
	}
}

func normalizedFavoriteQuotaLimit(value int, fallback, maximum int64) int64 {
	if value <= 0 {
		return fallback
	}
	if int64(value) > maximum {
		return maximum
	}
	return int64(value)
}

func (s *Server) favoriteQuotaPolicy() favoriteQuotaPolicy {
	return normalizedFavoriteQuotaPolicy(s.cfg.Favorite)
}

// Membership writes need one repeatable snapshot for the target-visibility
// decision and the mutation. Acquire the same quota key as a session lock
// before that transaction begins; otherwise a transaction waiting on an xact
// advisory lock could retain a snapshot from before the preceding commit.
func (s *Server) beginFavoriteMembershipTx(ctx context.Context, userID int64) (*favoriteMembershipTransaction, error) {
	if userID <= 0 {
		return nil, errors.New("invalid favorite quota owner")
	}
	connection, err := s.db.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	transaction := &favoriteMembershipTransaction{connection: connection, userID: userID}
	if _, err = connection.Exec(ctx, `select pg_advisory_lock(
		hashtextextended('favorite-stock-quota:'||$1::bigint::text,0))`, userID); err != nil {
		transaction.discardConnection()
		return nil, err
	}
	transaction.tx, err = connection.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		transaction.close()
		return nil, err
	}
	return transaction, nil
}

func (transaction *favoriteMembershipTransaction) close() {
	if transaction == nil || transaction.connection == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if transaction.tx != nil {
		_ = transaction.tx.Rollback(ctx)
	}
	var unlocked bool
	err := transaction.connection.QueryRow(ctx, `select pg_advisory_unlock(
		hashtextextended('favorite-stock-quota:'||$1::bigint::text,0))`, transaction.userID).Scan(&unlocked)
	if err != nil || !unlocked {
		transaction.discardConnection()
		return
	}
	transaction.connection.Release()
	transaction.connection = nil
}

func (transaction *favoriteMembershipTransaction) discardConnection() {
	if transaction == nil || transaction.connection == nil {
		return
	}
	connection := transaction.connection.Hijack()
	transaction.connection = nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = connection.Close(ctx)
}

// Every favorite stock increase for one user takes this transaction-scoped
// database lock before reading usage. The lock spans the subsequent insert and
// commit, so concurrent API replicas cannot all admit against a stale count.
func lockFavoriteStockQuotaTx(ctx context.Context, tx pgx.Tx, userID int64) error {
	if userID <= 0 {
		return errors.New("invalid favorite quota owner")
	}
	_, err := tx.Exec(ctx, `select pg_advisory_xact_lock(
		hashtextextended('favorite-stock-quota:'||$1::bigint::text,0))`, userID)
	return err
}

func enforceFavoriteCollectionGrowthTx(ctx context.Context, tx pgx.Tx, userID, growth int64, policy favoriteQuotaPolicy) error {
	if growth <= 0 {
		return nil
	}
	var current int64
	if err := tx.QueryRow(ctx, `select count(*) from (
		select collection.id from favorite_collections collection
		where collection.user_id=$1 limit $2
	) bounded_collections`, userID, policy.MaximumCollections+1).Scan(&current); err != nil {
		return err
	}
	if current >= policy.MaximumCollections || growth > policy.MaximumCollections-current {
		return errFavoriteCollectionQuota
	}
	return nil
}

func enforceFavoritePatchQuotaTx(ctx context.Context, tx pgx.Tx, userID int64, entityType string, entityID int64,
	addCollectionIDs, removeCollectionIDs []string, policy favoriteQuotaPolicy) error {
	current, err := loadBoundedFavoriteItemCountTx(ctx, tx, userID, policy.MaximumItems)
	if err != nil {
		return err
	}
	var existingAdds, existingRemovals int64
	if err = tx.QueryRow(ctx, `select
		count(*) filter(where collection.public_id=any($4::text[])),
		count(*) filter(where collection.public_id=any($5::text[]))
		from favorite_collection_items item
		join favorite_collections collection on collection.id=item.collection_id
		where collection.user_id=$1 and item.entity_type=$2 and item.entity_id=$3`,
		userID, entityType, entityID, addCollectionIDs, removeCollectionIDs).Scan(&existingAdds, &existingRemovals); err != nil {
		return err
	}
	delta := int64(len(addCollectionIDs)) - existingAdds - existingRemovals
	return evaluateFavoriteItemGrowth(current, delta, policy.MaximumItems)
}

func enforceFavoriteReplacementQuotaTx(ctx context.Context, tx pgx.Tx, userID int64, entityType string, entityID int64,
	desiredCollectionIDs []string, policy favoriteQuotaPolicy) error {
	current, err := loadBoundedFavoriteItemCountTx(ctx, tx, userID, policy.MaximumItems)
	if err != nil {
		return err
	}
	var existingForTarget int64
	if err = tx.QueryRow(ctx, `select count(*) from (
		select item.id from favorite_collection_items item
		join favorite_collections collection on collection.id=item.collection_id
		where collection.user_id=$1 and item.entity_type=$2 and item.entity_id=$3
		limit $4
	) bounded_target_items`, userID, entityType, entityID, int64(len(desiredCollectionIDs))+1).Scan(&existingForTarget); err != nil {
		return err
	}
	delta := int64(len(desiredCollectionIDs)) - existingForTarget
	return evaluateFavoriteItemGrowth(current, delta, policy.MaximumItems)
}

func loadBoundedFavoriteItemCountTx(ctx context.Context, tx pgx.Tx, userID, maximum int64) (int64, error) {
	var current int64
	err := tx.QueryRow(ctx, `select count(*) from (
		select item.id from favorite_collection_items item
		join favorite_collections collection on collection.id=item.collection_id
		where collection.user_id=$1 limit $2
	) bounded_items`, userID, maximum+1).Scan(&current)
	return current, err
}

func evaluateFavoriteItemGrowth(current, delta, maximum int64) error {
	// Existing over-limit inventories may still be made smaller. This also keeps
	// idempotent writes available after an operator lowers the configured limit.
	if delta <= 0 {
		return nil
	}
	if current >= maximum || delta > maximum-current {
		return errFavoriteItemQuota
	}
	return nil
}

func writeFavoriteQuotaError(w http.ResponseWriter, err error, policy favoriteQuotaPolicy) bool {
	switch {
	case errors.Is(err, errFavoriteCollectionQuota):
		writeAPIError(w, http.StatusConflict, "FAVORITE_COLLECTION_LIMIT",
			"favorite collection limit reached", 0, map[string]any{"limit": policy.MaximumCollections})
		return true
	case errors.Is(err, errFavoriteItemQuota):
		writeAPIError(w, http.StatusConflict, "FAVORITE_ITEM_LIMIT",
			"favorite item limit reached", 0, map[string]any{"limit": policy.MaximumItems})
		return true
	default:
		return false
	}
}
