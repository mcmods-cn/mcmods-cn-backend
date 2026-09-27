package database

// catalogImportArtifactSchemaStatements registers every catalog-import object
// before it is written to OSS. The lineage row remains durable across worker
// crashes so recovery can tombstone the pending file and enqueue provider
// deletion before a retry starts.
func catalogImportArtifactSchemaStatements() []string {
	return []string{
		`create table catalog_import_job_artifacts (
			job_id text not null references catalog_import_jobs(id) on delete restrict,
			run_token text not null check(run_token<>''),
			object_key text not null,
			oss_file_id bigint not null unique references oss_files(id) on delete restrict,
			status text not null default 'planned',
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			primary key(job_id,run_token,object_key),
			check(status in ('planned','active','abandoned'))
		)`,
		`create index idx_catalog_import_job_artifacts_pending
			on catalog_import_job_artifacts(job_id,run_token,oss_file_id)
			where status='planned'`,
	}
}
