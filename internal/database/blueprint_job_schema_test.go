package database

import (
	"strings"
	"testing"
)

func TestBlueprintJobsHaveBoundedLeaseAndActiveOperationInvariant(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("unexpected schema generation %d", schemaGeneration)
	}
	definition := strings.ToLower(strings.Join(schemaInstallationStatements(), "\n"))
	for _, required := range []string{
		"max_attempts integer not null default 3",
		"locked_by text not null default ''",
		"lease_expires_at timestamptz",
		"check (max_attempts between 1 and 20)",
		"idx_blueprint_jobs_recovery",
		"on blueprint_jobs(lease_expires_at,id) where status='processing'",
		"idx_blueprint_jobs_active_operation",
		"on blueprint_jobs(blueprint_id,operation,target_format)",
		"where status in ('queued','processing')",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("blueprint job schema is missing %q", required)
		}
	}
}

func TestBlueprintGeneratedArtifactsHavePendingActivationAndCompensationFacts(t *testing.T) {
	definition := strings.ToLower(strings.Join(schemaInstallationStatements(), "\n"))
	for _, required := range []string{
		"check(status in ('pending','active','deleted','quarantined'))",
		"normalized_file_id bigint references oss_files(id) on delete set null",
		"create table blueprint_job_artifacts",
		"job_id bigint references blueprint_jobs(id) on delete set null",
		"blueprint_id bigint references blueprints(id) on delete set null",
		"file_id bigint not null unique references oss_files(id) on delete restrict",
		"unique(job_id,attempt,role)",
		"check(role in ('normalized','cover','conversion'))",
		"check(status in ('pending','active','abandoned'))",
		"idx_blueprint_job_artifacts_pending",
		"on blueprint_job_artifacts(id,job_id,attempt) where status='pending'",
		"idx_blueprint_job_artifacts_active_orphan",
		"where status='active' and blueprint_id is null",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("blueprint artifact schema is missing %q", required)
		}
	}
}
