package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestLogShareScopeAndRedactionConstraintsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	db := newBaselineTestDatabase(t, ctx)
	t.Setenv("SEED_ADMIN_PASSWORD", "")
	if err := Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	var user, mod, file int64
	if err := db.QueryRow(ctx, `insert into users(username,email,password_hash)
		values('log-scope','log-scope@example.test','synthetic-only') returning id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,submitted_by)
		values('lscope001','log-scope','Log fixture',$1) returning id`, user).Scan(&mod); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,uploader_id)
		values('fixture','https://oss.example.test','fixture','log-scope', $1) returning id`, user).Scan(&file); err != nil {
		t.Fatal(err)
	}
	var comments [2]int64
	for i := range comments {
		if err := db.QueryRow(ctx, `insert into comments(target_type,target_id,author_id,body)
			values('mod',$1,$2,'synthetic comment') returning id`, mod, user).Scan(&comments[i]); err != nil {
			t.Fatal(err)
		}
	}
	insertShare := func(code string, version int, scope *int64) error {
		_, err := db.Exec(ctx, `insert into log_shares(public_code,owner_user_id,source_type,source_file_id,comment_scope_id,status,redaction_version,expires_at)
			values($1,$2,'file',$3,$4,'ready',$5,now()+interval '1 day')`, code, user, file, scope, version)
		return err
	}
	for _, input := range []struct {
		code    string
		version int
		scope   *int64
	}{
		{"logstandalone1", 1, nil},
		{"logstandalone2", 2, nil},
		{"logcommentscope1", 1, &comments[0]},
		{"logcommentscope2", 1, &comments[1]},
	} {
		if err := insertShare(input.code, input.version, input.scope); err != nil {
			t.Fatal(err)
		}
	}
	for _, scope := range []*int64{nil, &comments[0]} {
		var duplicate *pgconn.PgError
		if err := insertShare("logduplicate1", 1, scope); !errors.As(err, &duplicate) || duplicate.Code != "23505" || duplicate.ConstraintName != "idx_log_shares_active_source_file" {
			t.Fatalf("duplicate active share scope accepted: %v", err)
		}
	}
	var badValue *pgconn.PgError
	if _, err := db.Exec(ctx, `update log_shares set redaction_applied_version=0 where public_code='logstandalone1'`); !errors.As(err, &badValue) || badValue.ConstraintName != "log_shares_redaction_applied_version_check" {
		t.Fatalf("invalid applied redactor version accepted: %v", err)
	}
	if _, err := db.Exec(ctx, `delete from comments where id=$1`, comments[0]); err != nil {
		t.Fatal(err)
	}
	var surviving, removed int
	if err := db.QueryRow(ctx, `select count(*),count(*) filter(where public_code='logcommentscope1') from log_shares`).Scan(&surviving, &removed); err != nil || surviving != 3 || removed != 0 {
		t.Fatal("physical comment deletion orphaned a scoped short link or removed an unrelated share")
	}
}
