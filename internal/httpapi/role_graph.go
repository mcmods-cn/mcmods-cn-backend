package httpapi

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

func lockRoleGraphMutationTx(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods-cn-role-graph'))`)
	return err
}

func validateRoleGraphTx(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `select code,parents from roles order by code`)
	if err != nil {
		return err
	}
	defer rows.Close()
	graph := make(map[string][]string)
	for rows.Next() {
		var code string
		var parents []string
		if err = rows.Scan(&code, &parents); err != nil {
			return err
		}
		graph[code] = parents
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return validateRoleGraph(graph)
}

func validateRoleGraph(graph map[string][]string) error {
	for code, parents := range graph {
		for _, parent := range parents {
			if _, exists := graph[parent]; !exists {
				return fmt.Errorf("role %s references missing parent %s", code, parent)
			}
		}
	}
	const (
		unvisited = iota
		visiting
		visited
	)
	states := make(map[string]int, len(graph))
	var visit func(string) error
	visit = func(code string) error {
		switch states[code] {
		case visiting:
			return fmt.Errorf("role inheritance cycle includes %s", code)
		case visited:
			return nil
		}
		states[code] = visiting
		for _, parent := range graph[code] {
			if err := visit(parent); err != nil {
				return err
			}
		}
		states[code] = visited
		return nil
	}
	for code := range graph {
		if err := visit(code); err != nil {
			return err
		}
	}
	return nil
}

func roleDeletionBlockersTx(ctx context.Context, tx pgx.Tx, roleID int64, code string) ([]string, error) {
	blockers := make([]string, 0)
	if code == "admin" || code == "registered" || code == "banned" {
		blockers = append(blockers, "protected built-in role")
	}
	var defaultRole bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from system_settings
		where key='permission.default_roles' and (value->>'registeredRole'=$1 or value->>'bannedRole'=$1))`, code).
		Scan(&defaultRole); err != nil {
		return nil, err
	}
	if defaultRole {
		blockers = append(blockers, "default role configuration")
	}
	parentRows, err := tx.Query(ctx, `select code from roles where $1=any(parents) order by code`, code)
	if err != nil {
		return nil, err
	}
	for parentRows.Next() {
		var child string
		if err = parentRows.Scan(&child); err != nil {
			parentRows.Close()
			return nil, err
		}
		blockers = append(blockers, "parent role "+child)
	}
	if err = parentRows.Err(); err != nil {
		parentRows.Close()
		return nil, err
	}
	parentRows.Close()
	trackRows, err := tx.Query(ctx, `select track_code from permission_role_track_roles
		where role_id=$1 order by track_code`, roleID)
	if err != nil {
		return nil, err
	}
	for trackRows.Next() {
		var trackCode string
		if err = trackRows.Scan(&trackCode); err != nil {
			trackRows.Close()
			return nil, err
		}
		blockers = append(blockers, "role track "+trackCode)
	}
	if err = trackRows.Err(); err != nil {
		trackRows.Close()
		return nil, err
	}
	trackRows.Close()
	var userBindings bool
	if err = tx.QueryRow(ctx, `select exists(select 1 from user_role_bindings where role_id=$1)`, roleID).
		Scan(&userBindings); err != nil {
		return nil, err
	}
	if userBindings {
		blockers = append(blockers, "user bindings")
	}
	sort.Strings(blockers)
	return blockers, nil
}
