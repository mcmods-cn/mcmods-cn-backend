package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExpiredBlueprintUploadsAndLocalizationsArePrunedIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify expired blueprint upload cleanup")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create temporary table blueprints(
		id bigint primary key,owner_id bigint not null,original_object_key text not null default '',status text not null,upload_expires_at timestamptz);
		create index idx_blueprints_upload_expiry on blueprints(upload_expires_at,id)
			where status='uploading' and upload_expires_at is not null;
		create temporary table content_localizations(subject_type text not null,subject_id bigint not null,locale text not null);
		create temporary table content_subjects(subject_type text not null,subject_id bigint not null);
		insert into blueprints(id,owner_id,original_object_key,status,upload_expires_at) values
			(790,700,'blueprints/790.nbt','uploading',now()-interval '1 minute'),
			(791,700,'blueprints/791.nbt','uploading',now()-interval '2 minutes'),
			(792,700,'blueprints/792.nbt','uploading',now()+interval '1 hour'),
			(793,700,'blueprints/793.nbt','queued',now()-interval '1 minute'),
			(794,700,'blueprints/794.nbt','uploading',now()+interval '1 hour'),
			(795,700,'blueprints/795.nbt','uploading',now()+interval '1 hour'),
			(796,700,'blueprints/796.nbt','uploading',now()+interval '1 hour');
		insert into content_localizations(subject_type,subject_id,locale) values
			('blueprint',790,'en-US'),('blueprint',791,'en-US'),('blueprint',792,'en-US'),('blueprint',793,'en-US'),
			('blueprint',794,'en-US'),('blueprint',795,'en-US'),('blueprint',796,'en-US'),
			('mod',790,'en-US');
		insert into content_subjects(subject_type,subject_id) values
			('blueprint',790),('blueprint',791),('blueprint',792),('blueprint',793),
			('blueprint',794),('blueprint',795),('blueprint',796),('mod',790)`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	if err = server.discardPendingBlueprintUpload(ctx, 700, 794, ""); err != nil {
		t.Fatalf("discard pending blueprint by ID: %v", err)
	}
	if err = server.discardPendingBlueprintUpload(ctx, 700, 0, "blueprints/795.nbt"); err != nil {
		t.Fatalf("discard pending blueprint by object key: %v", err)
	}
	if err = server.discardPendingBlueprintUpload(ctx, 701, 796, ""); err != nil {
		t.Fatalf("wrong-owner discard should be an idempotent no-op: %v", err)
	}
	NewMaintenanceWorker(pool, nil).prune(ctx)

	var blueprintIDs, localizationIDs, subjectIDs []int64
	if err = pool.QueryRow(ctx, `select coalesce(array_agg(id order by id),'{}') from blueprints`).Scan(&blueprintIDs); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select coalesce(array_agg(subject_id order by subject_id),'{}')
		from content_localizations where subject_type='blueprint'`).Scan(&localizationIDs); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select coalesce(array_agg(subject_id order by subject_id),'{}')
		from content_subjects where subject_type='blueprint'`).Scan(&subjectIDs); err != nil {
		t.Fatal(err)
	}
	if !equalInt64Slices(blueprintIDs, []int64{792, 793, 796}) ||
		!equalInt64Slices(localizationIDs, []int64{792, 793, 796}) ||
		!equalInt64Slices(subjectIDs, []int64{792, 793, 796}) {
		t.Fatalf("remaining blueprints/localizations/subjects = %v/%v/%v, want [792 793 796] in each", blueprintIDs, localizationIDs, subjectIDs)
	}
	var unrelated int
	if err = pool.QueryRow(ctx, `select count(*) from content_localizations where subject_type='mod' and subject_id=790`).Scan(&unrelated); err != nil || unrelated != 1 {
		t.Fatalf("unrelated localization count = %d, error = %v", unrelated, err)
	}
}

func equalInt64Slices(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
