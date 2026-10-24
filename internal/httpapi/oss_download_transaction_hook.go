package httpapi

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type ossDownloadTransactionHookContextKey struct{}
type ossDownloadTransactionHook func(context.Context, pgx.Tx) error

// Only server-side download handlers can bind a business settlement. The
// prepared URL is disclosed after this hook and the original activity commit.
// No HTTP field, provider URL or client capability can select a hook.
func withOSSDownloadTransactionHook(ctx context.Context, hook ossDownloadTransactionHook) context.Context {
	return context.WithValue(ctx, ossDownloadTransactionHookContextKey{}, hook)
}
func runOSSDownloadTransactionHook(ctx context.Context, tx pgx.Tx) error {
	hook, _ := ctx.Value(ossDownloadTransactionHookContextKey{}).(ossDownloadTransactionHook)
	if hook == nil {
		return nil
	}
	return hook(ctx, tx)
}
