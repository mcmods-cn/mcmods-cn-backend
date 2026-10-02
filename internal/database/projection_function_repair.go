package database

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const projectionRepairGeneration = 168

var projectionFunctionNames = []string{
	"enqueue_search_servers_for_mod",
	"enqueue_search_servers_for_mod_parent",
	"record_changelog_popularity_event",
	"refresh_popularity_from_comment",
}

// ProjectionFunctionBackup contains executable database definitions. It is an
// operator-owned recovery artifact, never an HTTP request or public response.
type ProjectionFunctionBackup struct {
	Version         int               `json:"version"`
	Database        string            `json:"database"`
	Generation      int               `json:"generation"`
	Functions       map[string]string `json:"functions"`
	CommentTriggers map[string]string `json:"comment_triggers"`
}

// InspectProjectionFunctions performs no DDL or business-data writes.
func InspectProjectionFunctions(ctx context.Context, pool *pgxpool.Pool) (ProjectionFunctionBackup, int, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return ProjectionFunctionBackup{}, 0, fmt.Errorf("begin function inspection: %w", err)
	}
	defer tx.Rollback(context.Background())
	backup, changed, err := inspectProjectionFunctions(ctx, tx)
	if err != nil {
		return ProjectionFunctionBackup{}, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectionFunctionBackup{}, 0, fmt.Errorf("commit function inspection: %w", err)
	}
	return backup, changed, nil
}

// ChangeProjectionFunctions explicitly repairs or restores four known trigger
// functions and their known comment bindings. The caller must persist the original definitions before
// any DDL; a persistence or DDL failure rolls the transaction back.
func ChangeProjectionFunctions(ctx context.Context, pool *pgxpool.Pool, confirmedDatabase string,
	restore *ProjectionFunctionBackup, persistBackup func(ProjectionFunctionBackup) error,
) error {
	if confirmedDatabase == "" || persistBackup == nil {
		return errors.New("function changes require an exact database confirmation and backup destination")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin function repair: %w", err)
	}
	defer tx.Rollback(context.Background())
	// Share the schema installation lock; this command does not increment or
	// bypass generation, and cannot race a cooperating schema installation.
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods-cn-schema-migrations'))`); err != nil {
		return fmt.Errorf("lock function repair: %w", err)
	}
	backup, _, err := inspectProjectionFunctions(ctx, tx)
	if err != nil {
		return err
	}
	if backup.Database != confirmedDatabase {
		return errors.New("function repair database confirmation does not match the connected target")
	}
	if err := validateProjectionBackup(backup, backup.Database); err != nil {
		return fmt.Errorf("current definitions cannot be restored by this repair tool: %w", err)
	}
	statements := map[string]string{
		projectionFunctionNames[0]: searchServersForModFunctionSQL,
		projectionFunctionNames[1]: searchServersForModParentFunctionSQL,
		projectionFunctionNames[2]: changelogPopularityFunctionSQL,
		projectionFunctionNames[3]: contentPopularityCommentRefreshFunctionStatement(),
	}
	if restore != nil {
		if err := validateProjectionBackup(*restore, backup.Database); err != nil {
			return err
		}
		statements = make(map[string]string, len(projectionFunctionNames))
		for _, name := range projectionFunctionNames {
			body, _ := projectionBackupBody(name, restore.Functions[name])
			// Reconstruct a single known function declaration. A backup cannot
			// append statements or escape its function body to run other DDL.
			statements[name] = "create or replace function public." + name + "() returns trigger as '" +
				strings.ReplaceAll(body, "'", "''") + "' language plpgsql"
		}
	}
	if err := persistBackup(backup); err != nil {
		return fmt.Errorf("persist original function definitions: %w", err)
	}
	if _, err := tx.Exec(ctx, `set local search_path=public,pg_catalog`); err != nil {
		return fmt.Errorf("set repair schema: %w", err)
	}
	for _, name := range projectionFunctionNames {
		if _, err := tx.Exec(ctx, statements[name]); err != nil {
			return fmt.Errorf("replace projection function %s: %w", name, err)
		}
	}
	for _, name := range commentPopularityTriggerNames {
		if _, err := tx.Exec(ctx, "drop trigger if exists "+name+" on public.comments"); err != nil {
			return fmt.Errorf("replace comment popularity bindings: %w", err)
		}
	}
	if restore != nil && len(restore.CommentTriggers) == 1 {
		if _, err := tx.Exec(ctx, legacyCommentPopularityTriggerSQL); err != nil {
			return fmt.Errorf("restore legacy comment popularity binding: %w", err)
		}
	} else {
		for _, name := range commentPopularityTriggerNames[1:] {
			if _, err := tx.Exec(ctx, commentPopularityTriggerDefinitions[name]); err != nil {
				return fmt.Errorf("install comment popularity binding %s: %w", name, err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit function repair: %w", err)
	}
	return nil
}

func inspectProjectionFunctions(ctx context.Context, tx pgx.Tx) (ProjectionFunctionBackup, int, error) {
	backup := ProjectionFunctionBackup{Version: 2, Functions: make(map[string]string, len(projectionFunctionNames)), CommentTriggers: make(map[string]string)}
	if err := tx.QueryRow(ctx, `select current_database(),generation from public.schema_metadata where singleton`).
		Scan(&backup.Database, &backup.Generation); err != nil {
		return backup, 0, fmt.Errorf("read function repair target and generation: %w", err)
	}
	if backup.Generation != projectionRepairGeneration {
		return backup, 0, fmt.Errorf("function repair requires schema generation %d, found %d", projectionRepairGeneration, backup.Generation)
	}
	desired := []string{searchServersForModFunctionSQL, searchServersForModParentFunctionSQL, changelogPopularityFunctionSQL,
		contentPopularityCommentRefreshFunctionStatement()}
	changed := 0
	for index, name := range projectionFunctionNames {
		var definition, body string
		if err := tx.QueryRow(ctx, `select pg_get_functiondef(proc.oid),proc.prosrc from pg_proc proc
			join pg_namespace namespace on namespace.oid=proc.pronamespace
			where namespace.nspname='public' and proc.proname=$1 and proc.pronargs=0 and proc.prorettype='trigger'::regtype`, name).
			Scan(&definition, &body); err != nil {
			return backup, 0, fmt.Errorf("inspect projection function %s: %w", name, err)
		}
		backup.Functions[name] = definition
		start := strings.Index(desired[index], "as $$") + len("as $$")
		end := strings.LastIndex(desired[index], "$$ language plpgsql")
		if strings.TrimSpace(body) != strings.TrimSpace(desired[index][start:end]) {
			changed++
		}
	}
	rows, err := tx.Query(ctx, `select trigger.tgname,pg_get_triggerdef(trigger.oid,true),trigger.tgenabled::text
		from pg_trigger trigger join pg_class relation on relation.oid=trigger.tgrelid
		join pg_namespace namespace on namespace.oid=relation.relnamespace
		where namespace.nspname='public' and relation.relname='comments' and not trigger.tgisinternal
		and (trigger.tgfoid='public.refresh_popularity_from_comment()'::regprocedure or trigger.tgname=any($1))`, commentPopularityTriggerNames)
	if err != nil {
		return backup, 0, fmt.Errorf("inspect comment popularity bindings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, definition, enabled string
		if err := rows.Scan(&name, &definition, &enabled); err != nil {
			return backup, 0, fmt.Errorf("read comment popularity binding: %w", err)
		}
		if enabled != "O" {
			return backup, 0, errors.New("comment popularity binding has unsupported enablement")
		}
		backup.CommentTriggers[name] = definition
	}
	if err := rows.Err(); err != nil {
		return backup, 0, fmt.Errorf("read comment popularity bindings: %w", err)
	}
	if err := validateCommentTriggerBackup(backup.CommentTriggers); err != nil {
		return backup, 0, err
	}
	return backup, changed, nil
}

func validateProjectionBackup(backup ProjectionFunctionBackup, databaseName string) error {
	if backup.Version != 2 || backup.Generation != projectionRepairGeneration || backup.Database != databaseName {
		return errors.New("restore backup format, generation or database does not match the connected target")
	}
	if len(backup.Functions) != len(projectionFunctionNames) {
		return errors.New("restore backup must contain exactly the four projection function definitions")
	}
	for _, name := range projectionFunctionNames {
		if _, ok := projectionBackupBody(name, backup.Functions[name]); !ok {
			return fmt.Errorf("restore backup has an invalid definition for %s", name)
		}
	}
	return validateCommentTriggerBackup(backup.CommentTriggers)
}

func projectionBackupBody(name, definition string) (string, bool) {
	pattern := `(?s)\ACREATE OR REPLACE FUNCTION public\.` + regexp.QuoteMeta(name) +
		`\(\)\n RETURNS trigger\n LANGUAGE plpgsql\nAS \$function\$(.*)\$function\$\n\z`
	match := regexp.MustCompile(pattern).FindStringSubmatch(definition)
	if len(match) != 2 || strings.Contains(match[1], "$function$") {
		return "", false
	}
	return match[1], true
}
