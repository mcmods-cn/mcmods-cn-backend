package database

import (
	"context"
	"errors"
)

// Existing rows receive an unknown historical mode. New enqueues must state
// their required mode; they receive review by default in test/data fixtures.
// No migration guesses past authorization or changes job/asset state.
func CatalogImportPolicySchemaStatements() []string {
	return []string{
		`alter table catalog_import_jobs add column if not exists requested_publication_mode text not null default 'legacy_unknown'`,
		`alter table catalog_import_jobs alter column requested_publication_mode set default 'review'`,
		`do $$ begin if not exists(select 1 from pg_constraint where conrelid='catalog_import_jobs'::regclass and conname='catalog_import_jobs_publication_mode_check') then alter table catalog_import_jobs add constraint catalog_import_jobs_publication_mode_check check(requested_publication_mode in ('legacy_unknown','review','automatic')); end if; end $$`,
	}
}
func RequireCatalogImportPolicySchema(ctx context.Context, query schemaQueryer) error {
	var ready bool
	if err := query.QueryRow(ctx, `select exists(select 1 from information_schema.columns where table_schema='public' and table_name='catalog_import_jobs' and column_name='requested_publication_mode' and data_type='text' and is_nullable='NO' and column_default=$1) and exists(select 1 from pg_constraint where conrelid='public.catalog_import_jobs'::regclass and conname='catalog_import_jobs_publication_mode_check' and contype='c' and convalidated)`, "'review'::text").Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return errors.New("catalog import policy extension required by the installed database baseline")
	}
	return nil
}
