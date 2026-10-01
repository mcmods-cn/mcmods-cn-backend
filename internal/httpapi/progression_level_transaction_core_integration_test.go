package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/security"
)

func TestLevelConfigurationExperienceStreamFailureRollsBackCoreIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	var actorID int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash)
		values('level-transaction-fixture','level-transaction@example.invalid','synthetic') returning id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	roles := make([]int64, 2)
	for index, code := range []string{"synthetic_level_start", "synthetic_level_next"} {
		if err := pool.QueryRow(ctx, `insert into roles(code,name) values($1,$1) returning id`, code).Scan(&roles[index]); err != nil {
			t.Fatal(err)
		}
	}
	exec(`insert into permission_role_tracks(code,name) values('synthetic_level_track','Synthetic level track')`)
	for index, roleID := range roles {
		exec(`insert into permission_role_track_roles(track_code,role_id,position) values('synthetic_level_track',$1,$2)`, roleID, index)
	}
	exec(`insert into user_role_bindings(user_id,role_id) values($1,$2)`, actorID, roles[0])
	exec(`insert into user_experience(user_id,experience,level) values($1,150,0)`, actorID)
	var originalConfigTime, originalExperienceTime time.Time
	var originalPermissionVersion int64
	if err := pool.QueryRow(ctx, `select updated_at from level_system_config where singleton`).Scan(&originalConfigTime); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select updated_at from user_experience where user_id=$1`, actorID).Scan(&originalExperienceTime); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select permission_version from users where id=$1`, actorID).Scan(&originalPermissionVersion); err != nil {
		t.Fatal(err)
	}
	// Preserve the real table, FKs, checks and triggers in this randomly owned
	// database. The volatile projection fails during result iteration after
	// updateLevelConfig has already changed configuration in its transaction.
	exec(`create function audit_level_experience_failure() returns bigint
		language plpgsql volatile as $$ begin raise exception 'synthetic experience stream failure'; end $$;
		alter table user_experience rename to audit_preserved_experience;
		create view user_experience as select user_id,audit_level_experience_failure() experience,
		level,updated_at from audit_preserved_experience`)
	probe, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := probe.Query(ctx, `select user_id,experience from user_experience for update`)
	if err != nil {
		_ = probe.Rollback(ctx)
		t.Fatalf("fixture failed before result iteration: %v", err)
	}
	for rows.Next() {
	}
	streamErr := rows.Err()
	rows.Close()
	if err := probe.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if streamErr == nil {
		t.Fatal("fixture did not produce a real PostgreSQL result iteration error")
	}
	requestContext := context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actorID})
	server := &Server{db: pool, cfg: cfg}
	update := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/levels/config",
			strings.NewReader(`{"roleTrackCode":"synthetic_level_track","levelThresholds":[0,100]}`)).WithContext(requestContext)
		response := httptest.NewRecorder()
		server.updateLevelConfig(response, request)
		return response
	}
	failed := update()
	if failed.Code != http.StatusInternalServerError || strings.Contains(failed.Body.String(), "synthetic experience stream failure") {
		t.Fatalf("failed stream status=%d body=%s", failed.Code, failed.Body.String())
	}
	var track *string
	var thresholds []int64
	var updatedBy *int64
	var configTime time.Time
	if err := pool.QueryRow(ctx, `select role_track_code,level_thresholds,updated_by,updated_at
		from level_system_config where singleton`).Scan(&track, &thresholds, &updatedBy, &configTime); err != nil {
		t.Fatal(err)
	}
	if track != nil || len(thresholds) != 0 || updatedBy != nil || !configTime.Equal(originalConfigTime) {
		t.Fatalf("failed transaction changed configuration: track=%v thresholds=%v actor=%v timestampChanged=%v", track, thresholds, updatedBy, !configTime.Equal(originalConfigTime))
	}
	var experience int64
	var level int
	var experienceTime time.Time
	if err := pool.QueryRow(ctx, `select experience,level,updated_at from audit_preserved_experience
		where user_id=$1`, actorID).Scan(&experience, &level, &experienceTime); err != nil {
		t.Fatal(err)
	}
	if experience != 150 || level != 0 || !experienceTime.Equal(originalExperienceTime) {
		t.Fatalf("failed transaction changed user experience/level: experience=%d level=%d", experience, level)
	}
	var boundRoles []int64
	var permissionVersion int64
	if err := pool.QueryRow(ctx, `select array_agg(role_id order by role_id) from user_role_bindings
		where user_id=$1`, actorID).Scan(&boundRoles); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select permission_version from users where id=$1`, actorID).Scan(&permissionVersion); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(boundRoles, roles[:1]) || permissionVersion != originalPermissionVersion {
		t.Fatalf("failed transaction changed permissions: roles=%v version=%d", boundRoles, permissionVersion)
	}
	// Restoring the preserved table makes the same request succeed, including
	// experience recomputation and the real role-binding permission triggers.
	exec(`drop view user_experience; alter table audit_preserved_experience rename to user_experience`)
	healthy := update()
	if healthy.Code != http.StatusOK {
		t.Fatalf("restored configuration update status=%d body=%s", healthy.Code, healthy.Body.String())
	}
	if err := pool.QueryRow(ctx, `select role_track_code,level_thresholds,updated_by from level_system_config
		where singleton`).Scan(&track, &thresholds, &updatedBy); err != nil {
		t.Fatal(err)
	}
	if track == nil || *track != "synthetic_level_track" || !slices.Equal(thresholds, []int64{0, 100}) || updatedBy == nil || *updatedBy != actorID {
		t.Fatal("healthy update did not persist the requested configuration")
	}
	if err := pool.QueryRow(ctx, `select level from user_experience where user_id=$1`, actorID).Scan(&level); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select array_agg(role_id order by role_id) from user_role_bindings
		where user_id=$1`, actorID).Scan(&boundRoles); err != nil {
		t.Fatal(err)
	}
	if level != 2 || !slices.Equal(boundRoles, roles[1:]) {
		t.Fatalf("healthy update did not recalculate level/role: level=%d roles=%v", level, boundRoles)
	}
}
