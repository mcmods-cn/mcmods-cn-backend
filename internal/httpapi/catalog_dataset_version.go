package httpapi

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func bumpCatalogDatasetVersionTx(ctx context.Context, tx pgx.Tx) error {
	if tx == nil {
		return fmt.Errorf("catalog dataset version transaction is required")
	}
	tag, err := tx.Exec(ctx, `update catalog_dataset_state set version=version+1,updated_at=now() where singleton`)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("catalog dataset version singleton is missing")
	}
	return nil
}
