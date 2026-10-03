package httpapi

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type mirrorPromotionPause struct {
	armed            atomic.Bool
	reached, release chan struct{}
}

func (pause *mirrorPromotionPause) TraceQueryStart(ctx context.Context, _ *pgx.Conn, query pgx.TraceQueryStartData) context.Context {
	if strings.Contains(query.SQL, "select status from mirrored_project_files where id=$1 for update") && pause.armed.CompareAndSwap(true, false) {
		close(pause.reached)
		select {
		case <-pause.release:
		case <-ctx.Done():
		}
	}
	return ctx
}
func (*mirrorPromotionPause) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestOCT02MirrorPromotionRechecksCompletedSecurityScanIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	base := newOCT02IsolatedDatabase(t, ctx)
	var mod, route int64
	if err := base.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values('mirrorace','mirror-race','Mirror race','approved') returning id`).Scan(&mod); err != nil {
		t.Fatal(err)
	}
	if err := base.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, mod).Scan(&route); err != nil {
		t.Fatal(err)
	}
	if err := saveMinecraftVersionConfigAndInvalidateStaleArtifacts(ctx, base, minecraftVersionConfig{Versions: []minecraftVersionOption{{Code: "1.21.1", Type: "release"}}, Loaders: []minecraftLoaderOption{{Code: "Fabric", Name: "Fabric", Versions: []string{"1.21.1"}}}}); err != nil {
		t.Fatal(err)
	}
	insertBUG035CleanMirror(t, ctx, base, route, "security-race-file")
	pause := &mirrorPromotionPause{reached: make(chan struct{}), release: make(chan struct{})}
	cfg := base.Config()
	cfg.ConnConfig.Tracer = pause
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		select {
		case <-pause.release:
		default:
			close(pause.release)
		}
		pool.Close()
	}()
	pause.armed.Store(true)
	done := make(chan error, 1)
	go func() { done <- (&ProjectAutomationWorker{server: &Server{db: pool}}).promoteCleanMirrors(ctx) }()
	select {
	case <-pause.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("mirror promotion did not reach its transaction barrier")
	}
	if _, err = base.Exec(ctx, `update oss_files set scan_status='rejected' where id=(select oss_file_id from mirrored_project_files where external_file_id='security-race-file')`); err != nil {
		t.Fatal(err)
	}
	close(pause.release)
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mirror promotion did not finish")
	}
	var files, events int
	var status string
	if err = base.QueryRow(ctx, `select (select count(*) from project_files where project_type='mod' and project_internal_id=$1),(select count(*) from project_update_events where project_route_id=$2),(select status from mirrored_project_files where external_file_id='security-race-file')`, mod, route).Scan(&files, &events, &status); err != nil || files != 0 || events != 0 || status != "failed" {
		t.Fatalf("late rejected scan publication files=%d events=%d mirror=%s error=%v", files, events, status, err)
	}
}
