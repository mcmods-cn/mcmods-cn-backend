package httpapi

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type projectCreationTransactionResult struct {
	ProjectType      string
	ProjectID        int64
	ProjectPublicID  string
	SiteID           string
	ProjectTitle     string
	TargetURL        string
	ReviewStatus     string
	ChangeRequestID  int64
	ChangeRequestUID string
}

type projectCreationTransactionHook func(context.Context, pgx.Tx, projectCreationTransactionResult) error

type projectCreationTransactionHookContextKey struct{}

func withProjectCreationTransactionHook(ctx context.Context, hook projectCreationTransactionHook) context.Context {
	return context.WithValue(ctx, projectCreationTransactionHookContextKey{}, hook)
}

func runProjectCreationTransactionHook(ctx context.Context, tx pgx.Tx, result projectCreationTransactionResult) error {
	hook, _ := ctx.Value(projectCreationTransactionHookContextKey{}).(projectCreationTransactionHook)
	if hook == nil {
		return nil
	}
	return hook(ctx, tx, result)
}
