package httpapi

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
)

type permissionDeclarationStore interface {
	databaseQuery
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// A conflict-only INSERT would still fire the statement-level RBAC generation
// trigger. A warm startup only reads the registered names. A shop mutation
// passes its own transaction, so creation of new names follows the item commit.
func registerDeclaredPermissions(ctx context.Context, db permissionDeclarationStore, codes []string) error {
	codes = normalizeCodes(codes)
	if len(codes) == 0 {
		return nil
	}
	var missing []string
	if err := db.QueryRow(ctx, `select array(select c.code from unnest($1::text[]) as c(code)
		where not exists(select 1 from permissions p where p.code=c.code) order by c.code)`, codes).Scan(&missing); err != nil {
		return err
	}
	if len(missing) == 0 {
		return nil
	}
	_, err := db.Exec(ctx, `insert into permissions(code,module,name,description)
		select code,split_part(code,'.',1),code,'Permission declared by application code'
		from unnest($1::text[]) as c(code) order by code on conflict(code) do nothing`, missing)
	return err
}
