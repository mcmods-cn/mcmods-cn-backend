package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func auditImportReviewProjectionFacts(t *testing.T, f auditFixGRFixture) map[string]string {
	t.Helper()
	facts := auditImportActivityFacts(t, f.test013Fixture)
	for _, table := range []string{"catalog_dataset_state", "recipe_types", "recipes", "recipe_type_import_snapshots", "recipe_template_import_snapshots", "recipe_import_snapshots", "recipe_import_bindings", "recipe_import_binding_candidates", "catalog_import_media"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}

type auditWithdrawalGenerationFailure struct{ reached chan struct{} }

func (trace *auditWithdrawalGenerationFailure) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

func (trace *auditWithdrawalGenerationFailure) TraceQueryEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	var pgerr *pgconn.PgError
	if errors.As(data.Err, &pgerr) && pgerr.Code == "XX001" && pgerr.ConstraintName == "audit_withdrawal_dataset_generation" {
		select {
		case trace.reached <- struct{}{}:
		default:
		}
	}
}

func TestAuditGR002WithdrawalCatalogGenerationFailureRollsBackDecisionHTTPPostgresIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	_, revision := f.imported(t, auditRecipeNegativePackage(t, "valid"))
	f.reviewGR(t, revision, "approved")
	review := defaultReviewConfig()
	review.CatalogDelete = true
	raw, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, reviewConfigSettingKey, raw); err != nil {
		t.Fatal(err)
	}
	withdrawal := decodeTEST022Data[modContentMutationResult](t, f.require(t, f.editor, http.MethodDelete, "/api/v1/mods/audit-gr-"+f.modCode+"/content-versions/"+f.versionPublic, nil, 200))
	if withdrawal.ReviewStatus != "pending" {
		t.Fatal("ordinary withdrawal bypassed independent review")
	}
	if _, err := f.db.Exec(f.ctx, `create function audit_withdrawal_generation_fault() returns trigger language plpgsql as $$
        begin raise exception 'AUDIT_WITHDRAWAL_DATASET_GENERATION_UNAVAILABLE' using errcode='XX001',constraint='audit_withdrawal_dataset_generation';end $$;
        create trigger audit_withdrawal_generation_fault before update on catalog_dataset_state for each row execute function audit_withdrawal_generation_fault()`); err != nil {
		t.Fatal(err)
	}
	trace := &auditWithdrawalGenerationFailure{reached: make(chan struct{}, 1)}
	f.server.db = auditImportReviewPool(t, f, trace)
	before := auditImportReviewProjectionFacts(t, f)
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+withdrawal.RevisionID, map[string]string{"status": "approved"}, 500)
	select {
	case <-trace.reached:
	default:
		t.Fatal("withdrawal did not reach exact XX001/audit_withdrawal_dataset_generation failure")
	}
	auditFixRequireProjectFactsUnchanged(t, before, auditImportReviewProjectionFacts(t, f))
	if _, err := f.db.Exec(f.ctx, `drop trigger audit_withdrawal_generation_fault on catalog_dataset_state;drop function audit_withdrawal_generation_fault()`); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+withdrawal.RevisionID, map[string]string{"status": "approved"}, 200)
	if f.number(t, `select count(*) from mod_content_versions where id=$1 and status='archived'`, f.versionID) != 1 ||
		f.number(t, `select count(*) from catalog_import_revisions where id=$1 and not is_active and status='superseded'`, revision) != 1 {
		t.Fatal("recovered independent withdrawal did not retire the exact source")
	}
	t.Log("precise XX001 cache-generation failure rolls back source/version/review/audit/outbox rows, recovery commits one independent withdrawal")
}

// Exercise the supported withdrawal path: submit a version archive for an
// independent review. Changing an approved import's status to rejected remains
// invalid. A duplicate approval's audit policy is deliberately not decided here.
func TestAuditGR002RepeatedReviewAndIndependentlyReviewedWithdrawalHTTPPostgresIntegration(t *testing.T) {
	t.Run("approved-repeat-reject-withdraw", func(t *testing.T) {
		f := newAuditFixGRFixture(t)
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "content.review")
		_, revision := f.imported(t, auditRecipeNegativePackage(t, "valid"))
		var recipe, recipeType string
		if err := f.db.QueryRow(f.ctx, `select recipe_entity.public_id,type_entity.public_id from recipe_import_snapshots snapshot
			join catalog_entities recipe_entity on recipe_entity.id=snapshot.recipe_id
			join recipes recipe on recipe.entity_id=snapshot.recipe_id
			join catalog_entities type_entity on type_entity.id=recipe.recipe_type_id where snapshot.revision_id=$1`, revision).Scan(&recipe, &recipeType); err != nil {
			t.Fatal(err)
		}
		catalog := "/api/v1/recipe-types/" + recipeType + "/catalog"
		detail := "/api/v1/catalog/recipes/" + recipe
		public := func(t *testing.T) {
			t.Helper()
			f.require(t, "", http.MethodGet, "/api/v1/export-revisions/"+revision, nil, 200)
			f.require(t, "", http.MethodGet, "/api/v1/export-revisions/"+revision+"/assets", nil, 200)
			f.require(t, "", http.MethodGet, detail, nil, 200)
			f.require(t, "", http.MethodGet, detail+"/render", nil, 200)
			data := decodeTEST022Data[map[string]any](t, f.require(t, "", http.MethodGet, catalog, nil, 200))
			if data["total"] != float64(1) || len(data["recipes"].([]any)) != 1 {
				t.Fatal("published recipe list/detail disagree")
			}
			directory := decodeTEST022Data[map[string]any](t, f.require(t, "", http.MethodGet, "/api/v1/recipe-types?canonicalId=example:pressing", nil, 200))
			if directory["total"] != float64(1) || len(directory["items"].([]any)) != 1 {
				t.Fatal("published exact recipe-type directory disagrees")
			}

		}
		f.reviewGR(t, revision, "approved")
		public(t) // Warm the real server's catalog/render caches before transitions.
		f.reviewGR(t, revision, "approved")
		if f.number(t, `select count(*) from catalog_import_revisions where target_version_id=$1 and is_active`, f.versionID) != 1 ||
			f.number(t, `select count(*) from recipe_import_snapshots where revision_id=$1`, revision) != 1 {
			t.Fatal("duplicate approval changed the effective import or recipe cardinality")
		}
		for range 2 {
			before := auditImportReviewProjectionFacts(t, f)
			f.require(t, f.denied, http.MethodPatch, "/api/v1/admin/export-revisions/"+revision+"/activate", map[string]string{"status": "rejected"}, 409)
			auditFixRequireProjectFactsUnchanged(t, before, auditImportReviewProjectionFacts(t, f))
			public(t)
		}
		review := defaultReviewConfig()
		review.CatalogDelete = true
		raw, err := json.Marshal(review)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, reviewConfigSettingKey, raw); err != nil {
			t.Fatal(err)
		}
		withdrawal := decodeTEST022Data[modContentMutationResult](t, f.require(t, f.editor, http.MethodDelete, "/api/v1/mods/audit-gr-"+f.modCode+"/content-versions/"+f.versionPublic, nil, 200))
		if withdrawal.ReviewStatus != "pending" || withdrawal.RevisionID == "" {
			t.Fatal("ordinary archive did not require independent review")
		}
		public(t) // Submitting an archive must not withdraw it before approval.
		f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+withdrawal.RevisionID, map[string]string{"status": "approved", "note": "Independent version withdrawal"}, 200)
		if f.number(t, `select count(*) from mod_content_versions where id=$1 and status='archived'`, f.versionID) != 1 ||
			f.number(t, `select count(*) from catalog_import_revisions where id=$1 and not is_active and status='superseded'`, revision) != 1 {
			t.Fatal("approved withdrawal left an active version/import")
		}
		f.require(t, "", http.MethodGet, "/api/v1/export-revisions/"+revision, nil, 403)
		f.require(t, "", http.MethodGet, "/api/v1/export-revisions/"+revision+"/assets", nil, 403)
		f.require(t, "", http.MethodGet, detail, nil, 404)
		f.require(t, "", http.MethodGet, detail+"/render", nil, 404)
		// No independent manual parent exists in this fixture: the current
		// visibility contract returns404 for that now-unpublished type.
		f.require(t, "", http.MethodGet, catalog, nil, 404)
		data := decodeTEST022Data[map[string]any](t, f.require(t, "", http.MethodGet, "/api/v1/recipe-types?canonicalId=example:pressing", nil, 200))
		if data["total"] != float64(0) || len(data["items"].([]any)) != 0 {
			t.Fatal("approved withdrawal retained warmed public directory items/total")
		}

		t.Log("approved/repeat approve remain coherent; both rejected409 are exact zero-change; ordinary archive pending preserves public data; independent approved archive withdraws warmed anonymous list/detail/render/assets")
	})
	t.Run("pending-reject-repeat", func(t *testing.T) {
		f := newAuditFixGRFixture(t)
		_, revision := f.imported(t, auditRecipeNegativePackage(t, "valid"))
		f.reviewGR(t, revision, "rejected")
		if f.number(t, `select count(*) from catalog_import_revisions where id=$1 and status='rejected' and not is_active`, revision) != 1 ||
			f.number(t, `select count(*) from audit_events where aggregate_key=$1 and action='review_rejected'`, revision) != 1 {
			t.Fatal("pending rejection did not preserve one exact decision")
		}
		before := auditImportReviewProjectionFacts(t, f)
		f.require(t, f.reviewer, http.MethodPatch, "/api/v1/admin/export-revisions/"+revision+"/activate", map[string]string{"status": "rejected"}, 409)
		auditFixRequireProjectFactsUnchanged(t, before, auditImportReviewProjectionFacts(t, f))
		f.require(t, "", http.MethodGet, "/api/v1/export-revisions/"+revision, nil, 403)
		f.require(t, "", http.MethodGet, "/api/v1/export-revisions/"+revision+"/assets", nil, 403)
	})
}

// Both actual HTTP reviewers are observed waiting on the same real advisory
// lock before it is released. Two different pending revisions serialize: the
// second publication supersedes the first, without an unhandled unique error.
func TestAuditGR006DifferentPendingRevisionsSerializeIndependentApprovalsHTTPPostgresIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	_, first := f.imported(t, auditRecipeNegativePackage(t, "valid"))
	_, second := f.imported(t, auditRecipeNegativePackage(t, "valid-second"))
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "content.review")
	pool := auditImportReviewPool(t, f, nil)
	f.server.db = pool
	holder, err := pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.Background())
	if _, err := lockImportedContentVersionTx(f.ctx, holder, f.versionID); err != nil {
		t.Fatal(err)
	}
	approveFirst := auditCompletionAsyncJSON(f.test013Fixture, f.reviewer, http.MethodPatch, "/api/v1/admin/export-revisions/"+first+"/activate", map[string]string{"status": "approved"})
	auditImportReviewWaiters(t, f, 1)
	approveSecond := auditCompletionAsyncJSON(f.test013Fixture, f.denied, http.MethodPatch, "/api/v1/admin/export-revisions/"+second+"/activate", map[string]string{"status": "approved"})
	auditImportReviewWaiters(t, f, 2)
	if err := holder.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	left, right := <-approveFirst, <-approveSecond
	if left.err != nil || right.err != nil || left.status != 200 || right.status != 200 {
		t.Fatalf("serialized independent reviews first=%d/%v second=%d/%v", left.status, left.err, right.status, right.err)
	}
	if f.number(t, `select count(*) from catalog_import_revisions where target_version_id=$1 and is_active and status='ready'`, f.versionID) != 1 ||
		f.number(t, `select count(*) from catalog_import_revisions where id=$1 and is_active and status='ready'`, second) != 1 ||
		f.number(t, `select count(*) from catalog_import_revisions where id=$1 and not is_active and status='superseded'`, first) != 1 ||
		f.number(t, `select count(*) from audit_events where aggregate_key=any($1::text[]) and action='review_approved'`, []string{first, second}) != 2 ||
		f.number(t, `select count(*) from mod_resource_version_details where import_revision_id=$1 and status='active'`, first) != 0 ||
		f.number(t, `select count(*) from mod_resource_version_details where import_revision_id=$1 and status='active'`, second) == 0 {
		t.Fatal("serialized decisions disagree with effective source, audit or resource projection")
	}
	var recipeType string
	if err := f.db.QueryRow(f.ctx, `select entity.public_id from recipe_type_import_snapshots snapshot join catalog_entities entity on entity.id=snapshot.recipe_type_id where snapshot.revision_id=$1`, second).Scan(&recipeType); err != nil {
		t.Fatal(err)
	}
	data := decodeTEST022Data[map[string]any](t, f.require(t, "", http.MethodGet, "/api/v1/recipe-types/"+recipeType+"/catalog", nil, 200))
	if data["total"] != float64(1) || len(data["recipes"].([]any)) != 1 {
		t.Fatal("two approved attempts produced duplicate public recipes")
	}
	t.Log("two real database waiters -> ordered200/200 -> second active/first superseded ->2 decision audits ->one public recipe, no500; no NATS/browser transport claim")
}
