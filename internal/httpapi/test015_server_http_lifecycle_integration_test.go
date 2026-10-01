package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/serverprobe"
)

type test015ServerFixture struct {
	test013Fixture
	server *Server
	probes *atomic.Int64
}

func newTEST015ServerFixture(t *testing.T) test015ServerFixture {
	t.Helper()
	f := test015ServerFixture{test013Fixture: newTEST013Fixture(t), probes: new(atomic.Int64)}
	f.server = f.origin.Config.Handler.(*Server)
	// Only the already-existing submission dependency is controlled here. Real
	// status/configuration networking is exercised separately over owned TCP.
	f.server.serverProbe = func(_ context.Context, address string) (serverprobe.Result, error) {
		f.probes.Add(1)
		if strings.Contains(address, "offline.invalid") {
			return serverprobe.Result{}, errors.New("TEST015 owned offline transport")
		}
		return test015ProbeResult(address), nil
	}
	for _, token := range []string{f.editor, f.otherEditor} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "server.create")
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "server.review")
	review := defaultReviewConfig()
	review.ServerCreate = true
	raw, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `update system_settings set value=$2 where key=$1`, reviewConfigSettingKey, string(raw)); err != nil {
		t.Fatal(err)
	}
	ossCfg := defaultOSSConfig()
	ossCfg.Enabled, ossCfg.UseCName = true, true
	ossCfg.Bucket, ossCfg.Region = "test015-owned-proof", "cn-test"
	ossCfg.Endpoint, ossCfg.PublicEndpoint = "https://proof.example.invalid", "https://proof.example.invalid"
	ossCfg.AccessKeyID, ossCfg.AccessKeySecret = "test015-synthetic-key", "test015-synthetic-secret"
	sealed, err := f.server.sealSystemSetting(ossCfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)`, sealed); err != nil {
		t.Fatal(err)
	}
	return f
}

func test015ProbeResult(address string) serverprobe.Result {
	return serverprobe.Result{Address: address, NormalizedAddress: strings.ToLower(address), HandshakeHost: "server.example.invalid",
		ConnectHost: "1.1.1.1", ConnectPort: 25565, Online: true, LatencyMS: 2, PlayersOnline: 3, PlayersMax: 20,
		MinecraftVersion: "1.21.1", Protocol: 767, MOTD: "TEST015 controlled submission status", Modded: true, Loader: "forge",
		ModListComplete: true, Mods: []serverprobe.Mod{{ID: "test015_machine", Version: "observed", Source: "forge_status", Confidence: "exact"}}}
}

func (f test015ServerFixture) proof(t *testing.T, token, status, scan string, sourceBytes int64) string {
	t.Helper()
	var id string
	if err := f.db.QueryRow(f.ctx, `insert into oss_files(object_key,uploader_id,bucket,endpoint,region,original_name,
		content_type,size_bytes,source_size_bytes,status,scan_status)
		values($1,$2,'test015-owned-proof','https://proof.example.invalid','cn-test','proof.txt','text/plain',100,$3,$4,$5)
		returning public_id`, "test015/proof/"+randomCatalogPublicID()+".txt", f.userIDs[token], sourceBytes, status, scan).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func test015Submission(address, proof string) createMinecraftServerRequest {
	return createMinecraftServerRequest{Address: address, Name: "TEST015 HTTP server", ShortDescription: "public summary",
		BodyMarkdown: "BODY_PRIVATE_TEST015 full server introduction", MinecraftVersions: []string{"1.21.1"}, Languages: []string{"en-US"},
		PrimaryTag: "survival", OnlineMode: true, ProofText: "PROOF_PRIVATE_TEST015 ownership explanation", ProofFileIDs: []string{proof},
		Links: []createServerLinkRequest{{Kind: "website", Label: "official", URL: "https://server.example.invalid"}},
		Mods:  []createServerModRequest{{ID: "test015_machine", Version: "declared"}, {ID: "test015_manual", Version: "manual-v1"}}}
}

func (f test015ServerFixture) create(t *testing.T, token string, request createMinecraftServerRequest) (int64, string) {
	t.Helper()
	var response struct {
		Data struct {
			ID           string `json:"id"`
			ReviewStatus string `json:"reviewStatus"`
			Published    bool   `json:"published"`
		} `json:"data"`
	}
	raw := f.require(t, token, http.MethodPost, "/api/v1/servers", request, http.StatusCreated)
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if !validCatalogPublicID(response.Data.ID) || response.Data.ReviewStatus != "pending" || response.Data.Published {
		t.Fatalf("HTTP creation bypassed independent pending review: %s", raw)
	}
	var internalID, submittedBy int64
	var status string
	var unpublished bool
	var samples, proofs, links int
	if err := f.db.QueryRow(f.ctx, `select id,submitted_by,review_status,published_at is null,
		(select count(*) from minecraft_server_status_samples where server_id=server.id),
		(select count(*) from minecraft_server_proof_files where server_id=server.id),
		(select count(*) from minecraft_server_links where server_id=server.id)
		from minecraft_servers server where public_id=$1`, response.Data.ID).
		Scan(&internalID, &submittedBy, &status, &unpublished, &samples, &proofs, &links); err != nil {
		t.Fatal(err)
	}
	if submittedBy != f.userIDs[token] || status != "pending" || !unpublished || samples != 1 || proofs != len(request.ProofFileIDs) || links != len(request.Links) {
		t.Fatalf("creation durable facts actor=%d status=%s unpublished=%t samples/proofs/links=%d/%d/%d", submittedBy, status, unpublished, samples, proofs, links)
	}
	return internalID, response.Data.ID
}

func (f test015ServerFixture) serverFacts(t *testing.T) map[string]string {
	t.Helper()
	facts := make(map[string]string)
	for _, table := range []string{"minecraft_servers", "minecraft_server_links", "minecraft_server_mods", "minecraft_server_mod_evidence",
		"minecraft_server_proof_files", "minecraft_server_status_samples", "unresolved_references", "oss_files", "oss_download_stats",
		"oss_object_deletion_outbox", "oss_user_quota_usage", "oss_user_daily_quota_usage", "public_routes", "notifications", "nats_outbox",
		"search_index_queue", "audit_events", "permission_audit_logs", "user_activity_events", "activity_event_outbox"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, "select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from "+pgx.Identifier{table}.Sanitize()+" fact").Scan(&raw); err != nil {
			t.Fatalf("snapshot server facts %s: %v", table, err)
		}
		facts[table] = raw
	}
	return facts
}

func (f test015ServerFixture) serverUnchanged(t *testing.T, before map[string]string) {
	t.Helper()
	after := f.serverFacts(t)
	if !reflect.DeepEqual(before, after) {
		for table, previous := range before {
			if previous != after[table] {
				t.Errorf("failed/rejected server operation partially changed %s", table)
			}
		}
		t.FailNow()
	}
}

func TestTEST015ServerSubmissionProofPrivacyAndObjectPermissionsHTTPIntegration(t *testing.T) {
	f := newTEST015ServerFixture(t)
	proofA := f.proof(t, f.editor, "active", "clean", 100)
	proofB := f.proof(t, f.otherEditor, "active", "clean", 100)
	for _, sample := range []struct {
		name, token, file string
		status            int
	}{
		{"cross-user-proof", f.editor, proofB, 400},
		{"pending-scan", f.editor, f.proof(t, f.editor, "active", "pending", 100), 400},
		{"rejected-scan", f.editor, f.proof(t, f.editor, "active", "rejected", 100), 400},
		{"deleted-proof", f.editor, f.proof(t, f.editor, "deleted", "clean", 100), 400},
		{"quarantined-proof", f.editor, f.proof(t, f.editor, "quarantined", "clean", 100), 400},
		{"source-byte-budget", f.editor, f.proof(t, f.editor, "active", "clean", (10<<20)+1), 400},
		{"unauthenticated", "", proofA, 401},
		{"missing-create-permission", f.denied, proofA, 403},
	} {
		t.Run(sample.name, func(t *testing.T) {
			before := f.serverFacts(t)
			f.require(t, sample.token, http.MethodPost, "/api/v1/servers", test015Submission("reject-"+sample.name+".invalid:25565", sample.file), sample.status)
			f.serverUnchanged(t, before)
		})
	}
	before := f.serverFacts(t)
	f.require(t, f.editor, http.MethodPost, "/api/v1/servers", test015Submission("offline.invalid:25565", proofA), 400)
	f.serverUnchanged(t, before)
	requestA := test015Submission("owned-a.invalid:25565", proofA)
	idA, publicA := f.create(t, f.editor, requestA)
	idB, publicB := f.create(t, f.otherEditor, test015Submission("owned-b.invalid:25565", proofB))
	assertBUG031Evidence(t, f.ctx, f.db, idA, "test015_machine", map[string]string{"forge_status": "observed", "manual": "declared"})
	assertBUG031Evidence(t, f.ctx, f.db, idA, "test015_manual", map[string]string{"manual": "manual-v1"})
	for _, token := range []string{"", f.otherEditor, f.denied} {
		f.require(t, token, http.MethodGet, "/api/v1/servers/"+publicA, nil, 404)
	}
	for _, token := range []string{f.editor, f.reviewer} {
		raw := f.require(t, token, http.MethodGet, "/api/v1/servers/"+publicA, nil, 200)
		if bytes.Contains(raw, []byte("PROOF_PRIVATE_TEST015")) || bytes.Contains(raw, []byte(proofA)) {
			t.Fatalf("public detail leaked private review proof to ordinary submitter/reviewer path: %s", raw)
		}
	}
	f.require(t, f.denied, http.MethodGet, "/api/v1/admin/server-reviews", nil, 403)
	f.require(t, f.editor, http.MethodGet, "/api/v1/admin/server-reviews/"+publicA, nil, 403)
	raw := f.require(t, f.reviewer, http.MethodGet, "/api/v1/admin/server-reviews?status=pending&limit=1", nil, 200)
	var page struct {
		Data struct {
			Items      []minecraftServerReviewSummary `json:"items"`
			HasMore    bool                           `json:"hasMore"`
			NextCursor string                         `json:"nextCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data.Items) != 1 || !page.Data.HasMore || page.Data.NextCursor == "" || page.Data.Items[0].ProofFileCount != 1 || page.Data.Items[0].LinkCount != 1 || page.Data.Items[0].ModCount != 2 || bytes.Contains(raw, []byte("PRIVATE_TEST015")) {
		t.Fatalf("review summary failed bounded projection/counters/cursor: %s", raw)
	}
	firstID := page.Data.Items[0].ID
	raw = f.require(t, f.reviewer, http.MethodGet, "/api/v1/admin/server-reviews?status=pending&limit=1&cursor="+url.QueryEscape(page.Data.NextCursor), nil, 200)
	if err := json.Unmarshal(raw, &page); err != nil || len(page.Data.Items) != 1 || page.Data.Items[0].ID == firstID || page.Data.HasMore {
		t.Fatalf("review cursor skipped or duplicated a real pending item: %s err=%v", raw, err)
	}
	raw = f.require(t, f.reviewer, http.MethodGet, "/api/v1/admin/server-reviews/"+publicA, nil, 200)
	if !bytes.Contains(raw, []byte("PROOF_PRIVATE_TEST015")) || !bytes.Contains(raw, []byte(proofA)) {
		t.Fatalf("authorized review detail omitted private proof: %s", raw)
	}
	for _, token := range []string{f.editor, f.otherEditor, f.denied} {
		before = f.serverFacts(t)
		f.require(t, token, http.MethodPost, "/api/v1/admin/server-reviews/"+publicA+"/attachments/"+proofA+"/presign", nil, 403)
		f.serverUnchanged(t, before)
	}
	before = f.serverFacts(t)
	f.require(t, f.reviewer, http.MethodPost, "/api/v1/admin/server-reviews/"+publicB+"/attachments/"+proofA+"/presign", nil, 404)
	f.serverUnchanged(t, before)
	raw = f.require(t, f.reviewer, http.MethodPost, "/api/v1/admin/server-reviews/"+publicA+"/attachments/"+proofA+"/presign", nil, 200)
	var signed struct {
		Data struct {
			URL       string    `json:"url"`
			Mode      string    `json:"downloadUrlMode"`
			ExpiresAt time.Time `json:"expiresAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &signed); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(signed.Data.URL)
	if err != nil || parsed.Host != "proof.example.invalid" || parsed.Query().Get("x-oss-signature") == "" || signed.Data.Mode != ossDownloadModePresigned || !signed.Data.ExpiresAt.After(time.Now()) || signed.Data.ExpiresAt.After(time.Now().Add(time.Hour)) || strings.Contains(signed.Data.URL, "synthetic-secret") {
		t.Fatalf("bound proof access was not a finite signed URL: %s err=%v", raw, err)
	}
	before = f.serverFacts(t)
	f.require(t, f.editor, http.MethodPost, "/api/v1/servers", requestA, 409)
	f.serverUnchanged(t, before)
	update := updateMinecraftServerRequest{Name: "authorized update", ShortDescription: "updated summary", BodyMarkdown: "updated body", MinecraftVersions: []string{"1.21.1"}, Languages: []string{"en-US"}, PrimaryTag: "survival", OnlineMode: true,
		Links: []createServerLinkRequest{{Kind: "website", URL: "https://updated.example.invalid"}}, Mods: []createServerModRequest{{ID: "test015_new_manual", Version: "v2"}}}
	// Submitting a server is not automatic project-edit authorization.
	before = f.serverFacts(t)
	f.require(t, f.editor, http.MethodPatch, "/api/v1/servers/"+publicA, update, 403)
	f.serverUnchanged(t, before)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+publicA)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.edit."+publicB)
	for _, token := range []string{f.otherEditor, f.denied} {
		before = f.serverFacts(t)
		f.require(t, token, http.MethodPatch, "/api/v1/servers/"+publicA, update, 403)
		f.serverUnchanged(t, before)
	}
	forged := map[string]any{"name": update.Name, "shortDescription": update.ShortDescription, "bodyMarkdown": update.BodyMarkdown,
		"minecraftVersions": update.MinecraftVersions, "languages": update.Languages, "primaryTag": update.PrimaryTag,
		"mods": []map[string]string{{"id": "test015_forged", "source": "agent", "confidence": "exact"}}}
	before = f.serverFacts(t)
	f.require(t, f.editor, http.MethodPatch, "/api/v1/servers/"+publicA, forged, 400)
	f.serverUnchanged(t, before)
	if _, err := f.db.Exec(f.ctx, `alter table minecraft_server_links add constraint test015_reject_update_link check(url<>'https://updated.example.invalid') not valid`); err != nil {
		t.Fatal(err)
	}
	before = f.serverFacts(t)
	f.require(t, f.editor, http.MethodPatch, "/api/v1/servers/"+publicA, update, 500)
	f.serverUnchanged(t, before)
	if _, err := f.db.Exec(f.ctx, `alter table minecraft_server_links drop constraint test015_reject_update_link`); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodPatch, "/api/v1/servers/"+publicA, update, 200)
	assertBUG031Evidence(t, f.ctx, f.db, idA, "test015_machine", map[string]string{"forge_status": "observed"})
	assertBUG031Evidence(t, f.ctx, f.db, idA, "test015_manual", nil)
	assertBUG031Evidence(t, f.ctx, f.db, idA, "test015_new_manual", map[string]string{"manual": "v2"})
	assertBUG031Evidence(t, f.ctx, f.db, idB, "test015_manual", map[string]string{"manual": "manual-v1"})
	if _, err := f.db.Exec(f.ctx, `delete from user_permissions where user_id=$1 and permission_id=(select id from permissions where code=$2)`, f.userIDs[f.editor], "project.edit."+publicA); err != nil {
		t.Fatal(err)
	}
	before = f.serverFacts(t)
	f.require(t, f.editor, http.MethodPatch, "/api/v1/servers/"+publicA, update, 403)
	f.serverUnchanged(t, before)
	// A permissions-only revocation keeps the existing session usable for reads.
	f.require(t, f.editor, http.MethodGet, "/api/v1/servers/"+publicA, nil, 200)
	for _, token := range []string{"", f.denied} {
		want := 403
		if token == "" {
			want = 401
		}
		f.require(t, token, http.MethodPost, "/api/v1/servers/probe", map[string]string{"address": "127.0.0.1:25565"}, want)
	}
	before = f.serverFacts(t)
	f.require(t, f.editor, http.MethodPost, "/api/v1/servers/probe", map[string]string{"address": "127.0.0.1:25565"}, 400)
	f.serverUnchanged(t, before)
	if f.probes.Load() < 3 {
		t.Fatal("HTTP submissions did not re-probe independently")
	}
}

func TestTEST015ServerReviewConcurrentTransitionAndNotificationRollbackHTTPIntegration(t *testing.T) {
	f := newTEST015ServerFixture(t)
	proof := f.proof(t, f.editor, "active", "clean", 100)
	id, publicID := f.create(t, f.editor, test015Submission("owned-review.invalid:25565", proof))
	path := "/api/v1/admin/server-reviews/" + publicID
	for _, token := range []string{f.editor, f.denied} {
		before := f.serverFacts(t)
		f.require(t, token, http.MethodPatch, path, map[string]string{"status": "approved"}, 403)
		f.serverUnchanged(t, before)
	}
	before := f.serverFacts(t)
	f.require(t, f.reviewer, http.MethodPatch, path, map[string]string{"status": "rejected"}, 400)
	f.serverUnchanged(t, before)
	// Failure to persist the notification intent must roll back the review,
	// publication pointer and search projection, making the same review retryable.
	if _, err := f.db.Exec(f.ctx, `alter table nats_outbox add constraint test015_reject_review_notification
		check(coalesce(payload->>'templateKey','') not in ('review_approved','review_rejected')) not valid`); err != nil {
		t.Fatal(err)
	}
	before = f.serverFacts(t)
	f.require(t, f.reviewer, http.MethodPatch, path, map[string]string{"status": "approved"}, 500)
	f.serverUnchanged(t, before)
	if _, err := f.db.Exec(f.ctx, `alter table nats_outbox drop constraint test015_reject_review_notification`); err != nil {
		t.Fatal(err)
	}
	codes := make(chan int, 2)
	failures := make(chan error, 2)
	var group sync.WaitGroup
	for _, state := range []string{"approved", "rejected"} {
		group.Add(1)
		go func(state string) {
			defer group.Done()
			raw, err := json.Marshal(map[string]string{"status": state, "note": "TEST015 independent concurrent review"})
			if err != nil {
				failures <- err
				return
			}
			request, err := http.NewRequestWithContext(f.ctx, http.MethodPatch, f.origin.URL+path, bytes.NewReader(raw))
			if err != nil {
				failures <- err
				return
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+f.reviewer)
			response, err := f.origin.Client().Do(request)
			if err != nil {
				failures <- err
				return
			}
			defer response.Body.Close()
			codes <- response.StatusCode
		}(state)
	}
	group.Wait()
	close(codes)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	seen := make(map[int]int)
	for code := range codes {
		seen[code]++
	}
	if seen[200] != 1 || seen[409] != 1 {
		t.Fatalf("concurrent review must resolve exactly once: %v", seen)
	}
	var state string
	var reviewer int64
	var reviewed, publicationMatches bool
	var notifications int
	if err := f.db.QueryRow(f.ctx, `select review_status,reviewed_by,reviewed_at is not null,
		(published_at is not null)=(review_status='approved'),
		(select count(*) from nats_outbox where payload->'data'->>'serverId'=$2 and payload->>'templateKey' in ('review_approved','review_rejected'))
		from minecraft_servers where id=$1`, id, publicID).Scan(&state, &reviewer, &reviewed, &publicationMatches, &notifications); err != nil {
		t.Fatal(err)
	}
	if reviewer != f.userIDs[f.reviewer] || !reviewed || !publicationMatches || notifications != 1 {
		t.Fatalf("review durable facts state=%s reviewer=%d reviewed=%t published=%t notification intents=%d", state, reviewer, reviewed, publicationMatches, notifications)
	}
	before = f.serverFacts(t)
	f.require(t, f.reviewer, http.MethodPatch, path, map[string]string{"status": "approved"}, 409)
	f.serverUnchanged(t, before)
	want := 404
	if state == "approved" {
		want = 200
	}
	f.require(t, "", http.MethodGet, "/api/v1/servers/"+publicID, nil, want)
	_, rejectedID := f.create(t, f.editor, test015Submission("owned-rejected.invalid:25565", proof))
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/admin/server-reviews/"+rejectedID, map[string]string{"status": "rejected", "note": "proof insufficient"}, 200)
	f.require(t, "", http.MethodGet, "/api/v1/servers/"+rejectedID, nil, 404)
	_, approvedID := f.create(t, f.editor, test015Submission("owned-approved.invalid:25565", proof))
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/admin/server-reviews/"+approvedID, map[string]string{"status": "approved"}, 200)
	f.require(t, "", http.MethodGet, "/api/v1/servers/"+approvedID, nil, 200)
	raw := f.require(t, "", http.MethodGet, "/api/v1/servers?limit=1", nil, 200)
	if bytes.Contains(raw, []byte("PRIVATE_TEST015")) || bytes.Contains(raw, []byte("bodyMarkdown")) || bytes.Contains(raw, []byte("proofText")) {
		t.Fatalf("public catalog leaked full/private data: %s", raw)
	}
}

func TestTEST015ServerCreationLateWriteFailureRollsBackEveryBusinessFactHTTPIntegration(t *testing.T) {
	f := newTEST015ServerFixture(t)
	proof := f.proof(t, f.editor, "active", "clean", 100)
	if _, err := f.db.Exec(f.ctx, `alter table minecraft_server_status_samples add constraint test015_reject_sample check(false) not valid`); err != nil {
		t.Fatal(err)
	}
	before := f.serverFacts(t)
	request := test015Submission("owned-late-failure.invalid:25565", proof)
	f.require(t, f.editor, http.MethodPost, "/api/v1/servers", request, 500)
	f.serverUnchanged(t, before)
	if _, err := f.db.Exec(f.ctx, `alter table minecraft_server_status_samples drop constraint test015_reject_sample`); err != nil {
		t.Fatal(err)
	}
	f.create(t, f.editor, request)
}

func (f test015ServerFixture) seedServer(t *testing.T, address, status string) int64 {
	t.Helper()
	var id int64
	if err := f.db.QueryRow(f.ctx, `insert into minecraft_servers(slug,address,normalized_address,handshake_host,connect_host,connect_port,
		name,minecraft_versions,languages,primary_tag,submitted_by,review_status,next_probe_at)
		values($1,$1,$1,'owned.invalid','1.1.1.1',25565,'TEST015 scheduler',array['1.21.1'],array['en-US'],'survival',$2,$3,now()-interval '1 minute') returning id`, address, f.userIDs[f.editor], status).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTEST015SchedulerConcurrentClaimsPersistSnapshotsAndRespectConcurrencyIntegration(t *testing.T) {
	f := newTEST015ServerFixture(t)
	ids := make([]int64, 48)
	for index := range ids {
		ids[index] = f.seedServer(t, fmt.Sprintf("owned-scheduled-%02d.invalid:25565", index), "approved")
	}
	pending := f.seedServer(t, "owned-pending.invalid:25565", "pending")
	future := f.seedServer(t, "owned-future.invalid:25565", "approved")
	if _, err := f.db.Exec(f.ctx, `update minecraft_servers set next_probe_at=now()+interval '1 hour' where id=$1`, future); err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if err = insertMinecraftServerMods(f.ctx, tx, ids[0], []trustedServerModEvidence{
		{ID: "test015_mixed", Version: "declared", Source: "manual", Confidence: "declared"},
		{ID: "test015_mixed", Version: "old-observed", Source: "configuration", Confidence: "inferred"},
		{ID: "test015_stale_machine", Source: "forge_status", Confidence: "exact"},
	}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	var active, peak atomic.Int64
	var signaled atomic.Bool
	var lock sync.Mutex
	seen := make(map[string]int)
	ready, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseAll)
	probe := func(ctx context.Context, address string) (serverprobe.Result, error) {
		lock.Lock()
		seen[address]++
		lock.Unlock()
		current := active.Add(1)
		defer active.Add(-1)
		for prior := peak.Load(); current > prior; prior = peak.Load() {
			if peak.CompareAndSwap(prior, current) {
				break
			}
		}
		if current == 32 && signaled.CompareAndSwap(false, true) {
			close(ready)
		}
		select {
		case <-release:
		case <-ctx.Done():
			return serverprobe.Result{}, ctx.Err()
		}
		if strings.Contains(address, "scheduled-47") {
			return serverprobe.Result{}, errors.New(strings.Repeat("owned offline failure;", 200))
		}
		result := test015ProbeResult(address)
		result.Mods = []serverprobe.Mod{{ID: "test015_current_machine", Version: "current", Source: "agent", Confidence: "exact"}}
		return result, nil
	}
	done := make(chan struct{}, 2)
	var schedulers sync.WaitGroup
	schedulers.Add(2)
	t.Cleanup(func() { releaseAll(); schedulers.Wait() })
	for range 2 {
		go func() {
			defer schedulers.Done()
			probeDueMinecraftServersWithProbe(f.ctx, f.db, probe)
			done <- struct{}{}
		}()
	}
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler never exercised its full existing 32-connection concurrency budget")
	}
	if peak.Load() != 32 || active.Load() != 32 {
		t.Fatalf("scheduler changed concurrency ceiling active=%d peak=%d", active.Load(), peak.Load())
	}
	releaseAll()
	for range 2 {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("owned concurrent scheduler did not finish")
		}
	}
	lock.Lock()
	if len(seen) != len(ids) {
		t.Fatalf("claims visited %d addresses, want %d", len(seen), len(ids))
	}
	for address, count := range seen {
		if count != 1 {
			t.Errorf("cross-instance duplicate claim %s count=%d", address, count)
		}
	}
	lock.Unlock()
	var samples, online, offline, futureClaims, excluded int
	var boundedError bool
	if err := f.db.QueryRow(f.ctx, `select
		(select count(*) from minecraft_server_status_samples where server_id=any($1::bigint[])),
		(select count(*) from minecraft_servers where id=any($1::bigint[]) and last_online and last_checked_at is not null),
		(select count(*) from minecraft_servers where id=any($1::bigint[]) and not last_online and last_checked_at is not null),
		(select count(*) from minecraft_servers where id=any($1::bigint[]) and next_probe_at between now()+interval '4 minutes' and now()+interval '5 minutes'),
		(select count(*) from minecraft_server_status_samples where server_id=any($2::bigint[])),
		(select length(last_error)=2000 from minecraft_servers where id=$3)`, ids, []int64{pending, future}, ids[47]).Scan(&samples, &online, &offline, &futureClaims, &excluded, &boundedError); err != nil {
		t.Fatal(err)
	}
	if samples != 48 || online != 47 || offline != 1 || futureClaims != 48 || excluded != 0 || !boundedError || peak.Load() > 32 || active.Load() != 0 {
		t.Fatalf("scheduler durable/limit facts samples=%d online=%d offline=%d claimed=%d excluded=%d boundedError=%t active=%d peak=%d", samples, online, offline, futureClaims, excluded, boundedError, active.Load(), peak.Load())
	}
	assertBUG031Evidence(t, f.ctx, f.db, ids[0], "test015_mixed", map[string]string{"manual": "declared"})
	assertBUG031Evidence(t, f.ctx, f.db, ids[0], "test015_stale_machine", nil)
	assertBUG031Evidence(t, f.ctx, f.db, ids[0], "test015_current_machine", map[string]string{"agent": "current"})
	var staleReferences int
	if err := f.db.QueryRow(f.ctx, `select count(*) from unresolved_references where source_type='minecraft_server_mod' and raw_identifier='test015_stale_machine'`).Scan(&staleReferences); err != nil {
		t.Fatal(err)
	}
	if staleReferences != 0 {
		t.Fatalf("complete scheduler snapshot retained %d stale unresolved references", staleReferences)
	}
	before := f.serverFacts(t)
	probeDueMinecraftServersWithProbe(f.ctx, f.db, probe)
	f.serverUnchanged(t, before)
}

func TestTEST015ProbePersistenceRollbackRetryIncompleteSnapshotAndRetentionIntegration(t *testing.T) {
	f := newTEST015ServerFixture(t)
	id := f.seedServer(t, "owned-persistence.invalid:25565", "approved")
	result := test015ProbeResult("owned-persistence.invalid:25565")
	if err := persistMinecraftServerProbe(f.ctx, f.db, id, result, nil); err != nil {
		t.Fatal(err)
	}
	// Reconciliation fails after sample insert and latest-status update. Every
	// database fact must roll back, including trigger-driven search projections.
	if _, err := f.db.Exec(f.ctx, `alter table minecraft_server_mod_evidence add constraint test015_reject_observation check(version<>'reject-observation') not valid`); err != nil {
		t.Fatal(err)
	}
	before := f.serverFacts(t)
	result.PlayersOnline = 17
	result.Mods[0].Version = "reject-observation"
	if err := persistMinecraftServerProbe(f.ctx, f.db, id, result, nil); err == nil || !strings.Contains(err.Error(), "test015_reject_observation") {
		t.Fatalf("late persistence fault was swallowed: %v", err)
	}
	f.serverUnchanged(t, before)
	if _, err := f.db.Exec(f.ctx, `alter table minecraft_server_mod_evidence drop constraint test015_reject_observation`); err != nil {
		t.Fatal(err)
	}
	result.Mods[0].Version = "recovered"
	if err := persistMinecraftServerProbe(f.ctx, f.db, id, result, nil); err != nil {
		t.Fatal(err)
	}
	assertBUG031Evidence(t, f.ctx, f.db, id, "test015_machine", map[string]string{"forge_status": "recovered"})
	result.ModListComplete = false
	result.Mods = []serverprobe.Mod{{ID: "test015_partial", Source: "configuration", Confidence: "inferred"}}
	if err := persistMinecraftServerProbe(f.ctx, f.db, id, result, nil); err != nil {
		t.Fatal(err)
	}
	assertBUG031Evidence(t, f.ctx, f.db, id, "test015_machine", map[string]string{"forge_status": "recovered"})
	assertBUG031Evidence(t, f.ctx, f.db, id, "test015_partial", map[string]string{"configuration": ""})
	if _, err := f.db.Exec(f.ctx, `insert into minecraft_server_status_samples(server_id,online,checked_at) values
		($1,false,now()-interval '91 days'),($1,true,now()-interval '89 days')`, id); err != nil {
		t.Fatal(err)
	}
	cleanupMinecraftServerSamples(f.ctx, f.db)
	var old, current int
	if err := f.db.QueryRow(f.ctx, `select count(*) filter(where checked_at<now()-interval '90 days'),count(*) from minecraft_server_status_samples where server_id=$1`, id).Scan(&old, &current); err != nil {
		t.Fatal(err)
	}
	if old != 0 || current != 4 {
		t.Fatalf("actual 90-day retention old=%d remaining=%d", old, current)
	}
	var publicID string
	if err := f.db.QueryRow(f.ctx, `select public_id from minecraft_servers where id=$1`, id).Scan(&publicID); err != nil {
		t.Fatal(err)
	}
	raw := f.require(t, "", http.MethodGet, "/api/v1/servers/"+publicID+"/history?range=90d", nil, 200)
	var history struct {
		Data struct {
			Points []json.RawMessage `json:"points"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &history); err != nil || len(history.Data.Points) != 4 {
		t.Fatalf("full public history lost retained samples: %s err=%v", raw, err)
	}
	f.require(t, "", http.MethodGet, "/api/v1/servers/"+publicID+"/history?range=unbounded", nil, 400)
}

func TestTEST015SchedulerPersistenceFailureRetainsIntervalClaimAndRetriesNextDueIntegration(t *testing.T) {
	f := newTEST015ServerFixture(t)
	id := f.seedServer(t, "owned-claim-retry.invalid:25565", "approved")
	if _, err := f.db.Exec(f.ctx, `alter table minecraft_server_status_samples add constraint test015_reject_scheduler_sample check(false) not valid`); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	probe := func(_ context.Context, address string) (serverprobe.Result, error) {
		calls.Add(1)
		return test015ProbeResult(address), nil
	}
	probeDueMinecraftServersWithProbe(f.ctx, f.db, probe)
	var claimed, unchangedStatus bool
	var samples int
	if err := f.db.QueryRow(f.ctx, `select next_probe_at>now()+interval '4 minutes',last_checked_at is null and not last_online,
		(select count(*) from minecraft_server_status_samples where server_id=$1) from minecraft_servers where id=$1`, id).
		Scan(&claimed, &unchangedStatus, &samples); err != nil {
		t.Fatal(err)
	}
	if !claimed || !unchangedStatus || samples != 0 || calls.Load() != 1 {
		t.Fatalf("failed sample must not falsify latest status, or revoke its separately committed interval claim: claimed=%t untouched=%t samples=%d calls=%d", claimed, unchangedStatus, samples, calls.Load())
	}
	if _, err := f.db.Exec(f.ctx, `alter table minecraft_server_status_samples drop constraint test015_reject_scheduler_sample`); err != nil {
		t.Fatal(err)
	}
	before := f.serverFacts(t)
	probeDueMinecraftServersWithProbe(f.ctx, f.db, probe)
	f.serverUnchanged(t, before)
	if calls.Load() != 1 {
		t.Fatal("retry improperly bypassed the five-minute interval claim")
	}
	// Advance only this owned row's due time; this is a scheduling-state fixture,
	// not a claim that the test waited five minutes or changed production clocks.
	if _, err := f.db.Exec(f.ctx, `update minecraft_servers set next_probe_at=now()-interval '1 second' where id=$1`, id); err != nil {
		t.Fatal(err)
	}
	probeDueMinecraftServersWithProbe(f.ctx, f.db, probe)
	if err := f.db.QueryRow(f.ctx, `select last_online and last_checked_at is not null,
		(select count(*) from minecraft_server_status_samples where server_id=$1) from minecraft_servers where id=$1`, id).
		Scan(&claimed, &samples); err != nil {
		t.Fatal(err)
	}
	if !claimed || samples != 1 || calls.Load() != 2 {
		t.Fatalf("next-due retry failed state=%t samples=%d probes=%d", claimed, samples, calls.Load())
	}
}
