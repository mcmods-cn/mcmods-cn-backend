package httpapi

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestModExportFallbackIsBoundedAndShutdownReleasesClaimsIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	server := &Server{db: pool, cfg: cfg}
	var modID, versionID int64
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name) values('fbfixture','audit-fallback','synthetic fallback') returning id`).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,status) values($1,'synthetic','active') returning id`, modID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into catalog_import_packages(id,sha256,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest)
		values('fallback-package',$1,'synthetic.zip','fixture','fixture','1.20.1','forge','{}')`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 12)
	for index := range ids {
		ids[index] = fmt.Sprintf("fallback-job-%d", index)
		if _, err := pool.Exec(ctx, `insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version) values($1,$2,'fallback-package',$3,$4)`, ids[index], modID, versionID, fmt.Sprintf("fixture-%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var active, peak, launched atomic.Int32
	claimResults := make(chan error, 4)
	finished := make(chan struct{}, 4)
	var launchers sync.WaitGroup
	start := make(chan struct{})
	for _, id := range ids {
		launchers.Add(1)
		go func() {
			defer launchers.Done()
			<-start
			if server.startModExportFallback(workCtx, func(jobCtx context.Context) {
				now := active.Add(1)
				for {
					old := peak.Load()
					if now <= old || peak.CompareAndSwap(old, now) {
						break
					}
				}
				_, err := server.claimModExportJob(jobCtx, id)
				claimResults <- err
				<-jobCtx.Done()
				active.Add(-1)
				finished <- struct{}{}
			}) {
				launched.Add(1)
			}
		}()
	}
	close(start)
	launchers.Wait()
	if launched.Load() != 4 {
		t.Fatalf("fallback launched %d jobs, want 4", launched.Load())
	}
	for range 4 {
		if err := <-claimResults; err != nil {
			t.Fatal(err)
		}
	}
	var queued, processing int
	if err := pool.QueryRow(ctx, `select count(*) filter(where status='queued'),count(*) filter(where status='validating') from catalog_import_jobs`).Scan(&queued, &processing); err != nil {
		t.Fatal(err)
	}
	if queued != 8 || processing != 4 || peak.Load() != 4 {
		t.Fatalf("lost backlog or unbounded processor: queued=%d processing=%d peak=%d", queued, processing, peak.Load())
	}
	cancel()
	for range 4 {
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("shutdown did not cancel local processor")
		}
	}
	if server.startModExportFallback(workCtx, func(context.Context) { t.Error("cancelled task executed") }) {
		t.Fatal("cancelled context accepted a new processor")
	}
	server.cfg.NATS.Tasks = []config.NATSTaskConfig{{Code: modExportTaskCode, Enabled: false}}
	if server.startModExportFallback(ctx, func(context.Context) { t.Error("disabled task executed") }) {
		t.Fatal("disabled task accepted a local processor")
	}
}
