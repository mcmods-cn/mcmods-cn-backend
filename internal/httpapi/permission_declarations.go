package httpapi

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// Request mutations may declare permission names in their business transaction;
// startup route registration uses the same statement without changing grants.
func registerDeclaredPermissionWithQueryer(ctx context.Context, query interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, permission string) error {
	permission = normalizeCode(permission)
	if permission == "" {
		return nil
	}
	module := permission
	if index := strings.Index(permission, "."); index > 0 {
		module = permission[:index]
	}
	_, err := query.Exec(ctx, `insert into permissions (code, module, name, description)
 values ($1, $2, $1, 'Permission declared by application code')
 on conflict (code) do nothing`, permission, module)
	return err
}
