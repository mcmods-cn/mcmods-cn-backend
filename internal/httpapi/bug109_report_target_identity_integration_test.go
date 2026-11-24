package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReportModerationRecipientsDistinguishSubmittersFromDevelopersIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify report governance recipients")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `create temporary table effective_project_access(
		user_id bigint not null,project_type text not null,project_public_id text not null,access_level text not null
	);
	insert into effective_project_access values
		(10,'mod','mod000001','editor'),
		(20,'mod','mod000001','developer'),
		(20,'mod','mod000001','developer'),
		(21,'mod','mod000001','developer'),
		(22,'plugin','mod000001','developer'),
		(23,'mod','mod000002','developer')`); err != nil {
		t.Fatal(err)
	}

	submitterID := int64(10)
	recipients, err := reportModerationRecipientIDs(ctx, tx, "mod", "mod000001", reportTargetActor{
		ID: &submitterID, Role: reportTargetActorSubmitter,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recipients, []int64{20, 21}) {
		t.Fatalf("project moderation recipients = %v, want approved developers only", recipients)
	}

	ownerID := int64(30)
	recipients, err = reportModerationRecipientIDs(ctx, tx, "comment", "comment01", reportTargetActor{
		ID: &ownerID, Role: reportTargetActorAuthor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recipients, []int64{30}) {
		t.Fatalf("direct-content moderation recipients = %v, want the real author", recipients)
	}
}

func TestReportProjectTypesUseCanonicalAccessIdentities(t *testing.T) {
	t.Parallel()
	for reportType, projectType := range map[string]string{
		"mod": "mod", "modpack": "modpack", "plugin": "plugin", "map": "map", "shader": "shader_pack",
		"resource_pack": "resource_pack", "datapack": "datapack", "addon": "addon", "server": "minecraft_server",
	} {
		if actual, ok := reportProjectAccessType(reportType); !ok || actual != projectType {
			t.Fatalf("reportProjectAccessType(%q) = %q, %v; want %q, true", reportType, actual, ok, projectType)
		}
	}
	if _, ok := reportProjectAccessType("comment"); ok {
		t.Fatal("comment was treated as a relationship-backed project")
	}
}

func TestResolveProjectReportNotifiesDevelopersInsteadOfSubmitterIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "report.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "report.review", "report.action.delete")
	secondDeveloper, _, _ := createTEST044User(t, f.ctx, f.db, f.origin.Config.Handler.(*Server).cfg, "bug109-cocreator", "UTC")
	developers := []int64{f.userIDs[f.otherEditor], secondDeveloper}
	for _, userID := range developers {
		name := fmt.Sprintf("bug109-author-%d", userID)
		var creatorID int64
		if err := f.db.QueryRow(f.ctx, `insert into creators(kind,name,normalized_name,review_status)
			values('author',$1,$1,'approved') returning id`, name).Scan(&creatorID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec(f.ctx, `insert into creator_claims(creator_id,user_id,status,reviewed_at)
			values($1,$2,'approved',now())`, creatorID, userID); err != nil {
			t.Fatal(err)
		}
		tag, err := f.db.Exec(f.ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,role_id,
			name_snapshot,role_snapshot,status,permission_granting,approved_at)
			select 'mod',$1,$2,role.id,$3,role.name,'approved',true,now()
			from creator_role_definitions role where role.code='developer'`, f.modID, creatorID, name)
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("developer binding: %s, %v", tag, err)
		}
	}

	report := decodeTEST022Data[struct{ ID string }](t, f.require(t, f.denied, http.MethodPost, "/api/v1/reports",
		createUnifiedReportRequest{TargetType: "mod", TargetID: f.modCode, ReasonCode: "misleading_content"}, http.StatusCreated))
	path := "/api/v1/admin/reports/" + report.ID
	f.require(t, f.reviewer, http.MethodPost, path+"/claim", nil, http.StatusOK)
	f.require(t, f.reviewer, http.MethodPost, path+"/resolve", resolveUnifiedReportRequest{
		Conclusion: "valid", Note: "confirmed", DeleteTarget: true, IdempotencyKey: "bug109-resolution",
	}, http.StatusOK)

	var notified []int64
	if err := f.db.QueryRow(f.ctx, `select array_agg(aggregate_id::bigint order by aggregate_id::bigint)
		from nats_outbox where event_type='report.target_action'`).Scan(&notified); err != nil || !reflect.DeepEqual(notified, developers) {
		t.Fatalf("moderation recipients = %v, want actual developers %v (submitter %d), err=%v", notified, developers, f.userIDs[f.editor], err)
	}
	var after int
	if err := f.db.QueryRow(f.ctx, `select count(*) from effective_project_access where project_type='mod'
		and project_public_id=$1 and access_level='developer'`, f.modCode).Scan(&after); err != nil || after != 0 {
		t.Fatalf("hidden project retained live access rows: %d, err=%v", after, err)
	}
}
