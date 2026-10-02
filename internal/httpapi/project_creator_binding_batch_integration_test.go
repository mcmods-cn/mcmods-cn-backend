package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

type projectCreatorBindingQueryCounter struct {
	active atomic.Bool
	count  atomic.Int64
}

func (counter *projectCreatorBindingQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	if counter.active.Load() {
		counter.count.Add(1)
	}
	return ctx
}

func (*projectCreatorBindingQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestProjectCreatorBindingSyncBatchesChangesAndCoalescesSideEffectsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify batched creator-binding sync")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	counter := &projectCreatorBindingQueryCounter{}
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	poolConfig.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var actorID, projectID, roleID int64
	var rolePublicID string
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('perf019_actor','perf019@example.invalid','test-only',true) returning id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into simple_projects(project_type,slug,primary_name,review_status,submitted_by)
		values('plugin','perf019-plugin','PERF019 Plugin','approved',$1) returning id`, actorID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id,public_id from creator_role_definitions where code='developer'`).Scan(&roleID, &rolePublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into creators(public_id,kind,name,normalized_name,review_status)
		select 'a'||lpad(value::text,8,'0'),'author','PERF019 Author '||value,'perf019-author-'||value,'approved'
		from generate_series(1,64) value`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into content_creator_bindings(
		subject_type,subject_id,creator_id,role_id,name_snapshot,role_snapshot,status,permission_granting,
		approved_by,approved_at,display_order)
		select 'plugin',$1,creator.id,$2,creator.name,'Developer','approved',true,$3,now(),
			row_number() over(order by creator.public_id)-1
		from creators creator where creator.public_id like 'a%'`, projectID, roleID, actorID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `create temporary table perf019_search_writes(count bigint not null);
		insert into perf019_search_writes values(0)`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `create or replace function pg_temp.perf019_count_search_write() returns trigger as $$
		begin update perf019_search_writes set count=count+1; return new; end;
		$$ language plpgsql`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `create trigger trg_perf019_search_write after insert or update on search_index_queue
		for each row execute function pg_temp.perf019_count_search_write()`); err != nil {
		t.Fatal(err)
	}

	authors := make([]modAuthorPayload, 64)
	for index := range authors {
		publicID := fmt.Sprintf("a%08d", index+1)
		authors[index] = modAuthorPayload{CreatorID: publicID, RoleID: &rolePublicID}
	}
	request := httptest.NewRequest("PUT", "/api/v1/content-projects/plugin/perf019-plugin", nil)

	resetProjectCreatorBindingEffects(t, ctx, pool)
	aclBefore := projectACLRuntimeVersion(t, ctx, pool)
	noOpQueries := runProjectCreatorBindingSync(t, ctx, pool, counter, projectID, actorID, authors, request)
	aclAfter := projectACLRuntimeVersion(t, ctx, pool)
	if aclAfter != aclBefore {
		t.Fatalf("unchanged relationships bumped project ACL version: before=%d after=%d", aclBefore, aclAfter)
	}
	if writes := projectCreatorBindingSearchWrites(t, ctx, pool); writes != 0 {
		t.Fatalf("unchanged relationships wrote search queue %d times", writes)
	}
	if noOpQueries > 5 {
		t.Fatalf("unchanged 64-author sync used %d SQL statements, want <=5", noOpQueries)
	}

	for left, right := 0, len(authors)-1; left < right; left, right = left+1, right-1 {
		authors[left], authors[right] = authors[right], authors[left]
	}
	resetProjectCreatorBindingEffects(t, ctx, pool)
	aclBefore = projectACLRuntimeVersion(t, ctx, pool)
	changedQueries := runProjectCreatorBindingSync(t, ctx, pool, counter, projectID, actorID, authors, request)
	aclAfter = projectACLRuntimeVersion(t, ctx, pool)
	if aclAfter != aclBefore+1 {
		t.Fatalf("one relationship batch changed project ACL version by %d, want 1", aclAfter-aclBefore)
	}
	if writes := projectCreatorBindingSearchWrites(t, ctx, pool); writes != 1 {
		t.Fatalf("64 changed relationships wrote search queue %d times, want 1", writes)
	}
	if changedQueries > 6 {
		t.Fatalf("changed 64-author sync used %d SQL statements, want <=6", changedQueries)
	}
	rows, err := pool.Query(ctx, `select creator.public_id,binding.display_order,binding.status,binding.approved_by
		from content_creator_bindings binding join creators creator on creator.id=binding.creator_id
		where binding.subject_type='plugin' and binding.subject_id=$1 order by binding.display_order`, projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var publicID, status string
		var displayOrder int
		var approvedBy *int64
		if err = rows.Scan(&publicID, &displayOrder, &status, &approvedBy); err != nil {
			t.Fatal(err)
		}
		if publicID != authors[index].CreatorID || displayOrder != index || status != "approved" || approvedBy == nil || *approvedBy != actorID {
			t.Fatalf("binding %d did not preserve ordered approved identity: id=%s order=%d status=%s actor=%v", index, publicID, displayOrder, status, approvedBy)
		}
		index++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if index != len(authors) {
		t.Fatalf("loaded %d bindings, want %d", index, len(authors))
	}

	resetProjectCreatorBindingEffects(t, ctx, pool)
	aclBefore = projectACLRuntimeVersion(t, ctx, pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, displayOrder := range []int{0, 1} {
		if _, err = tx.Exec(ctx, `update content_creator_bindings set status=status
			where subject_type='plugin' and subject_id=$1 and display_order=$2`, projectID, displayOrder); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	aclAfter = projectACLRuntimeVersion(t, ctx, pool)
	if aclAfter != aclBefore+1 {
		t.Fatalf("two relationship statements in one transaction changed project ACL version by %d, want 1", aclAfter-aclBefore)
	}
	t.Logf("64-author no-op queries=%d changed queries=%d", noOpQueries, changedQueries)
}

func runProjectCreatorBindingSync(t *testing.T, ctx context.Context, pool *pgxpool.Pool, counter *projectCreatorBindingQueryCounter, projectID, actorID int64, authors []modAuthorPayload, request *http.Request) int64 {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counter.count.Store(0)
	counter.active.Store(true)
	err = syncProjectCreatorBindingsTx(ctx, tx, "plugin", projectID, authors, actorID, true, true, true, request)
	counter.active.Store(false)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return counter.count.Load()
}

func resetProjectCreatorBindingEffects(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `truncate search_index_queue; update perf019_search_writes set count=0`); err != nil {
		t.Fatal(err)
	}
}

func projectACLRuntimeVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	var version int64
	if err := pool.QueryRow(ctx, `select version from runtime_versions where name='project_acl'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func projectCreatorBindingSearchWrites(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(ctx, `select count from perf019_search_writes`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
