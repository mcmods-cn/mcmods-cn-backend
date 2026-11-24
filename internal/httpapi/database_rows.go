package httpapi

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type rowsQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type checkedRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

// finishRows is the single successful-loop boundary for database row streams.
// Callers still close immediately on Scan failure; a completed loop must pass
// through here before any partial result can become externally visible.
func finishRows(rows checkedRows) error {
	err := rows.Err()
	rows.Close()
	return err
}
