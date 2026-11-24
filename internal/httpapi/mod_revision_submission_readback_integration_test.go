package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func revisionOutcomeInput() createModRequest {
	return createModRequest{
		SiteID: "test013-mod", PrimaryName: "Review outcome", DefaultLocale: "en-US",
		Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Review outcome", Summary: "Updated summary"}},
		Environment:   "bothRequired", PrimaryCategory: "utility", OfficialStatus: "active", SourceStatus: "open",
		License: "MIT", SubmissionMethod: "manual", ModIDs: []modIdentifierPayload{{Identifier: "outcome", Primary: true}},
		Compatibilities: []modLoaderCompatibilityPayload{{Loader: "neoforge", Versions: []string{"1.21.1"}}},
	}
}

func TestModRevisionSubmissionReadbackFailureLeavesNoCommittedRequestIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	if _, err := f.db.Exec(f.ctx, "alter table review_events rename column note to unavailable_note"); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/revisions",
		submitModRevisionRequest{Snapshot: revisionOutcomeInput(), ChangeReason: "readback check"}, http.StatusInternalServerError)
	var changes, revisions, activities int
	err := f.db.QueryRow(f.ctx, `select
		(select count(*) from change_requests where aggregate_type='mod' and aggregate_key=$1),
		(select count(*) from content_revisions where aggregate_type='mod' and aggregate_key=$1),
		(select count(*) from activity_event_outbox where user_id=$2)`, f.modCode, f.userIDs[f.editor]).Scan(&changes, &revisions, &activities)
	if err != nil || changes != 0 || revisions != 0 || activities != 0 {
		t.Fatalf("failed submission committed requests=%d, revisions=%d, activities=%d, err=%v", changes, revisions, activities, err)
	}
}

func TestModRevisionIconFailurePreservesReceiptAndDistinguishesMissingComparisonIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	s := f.origin.Config.Handler.(*Server)
	const objectKey = "fixture-mod-revision/approved.png"
	cfg := ossConfigPayload{Enabled: true, Bucket: "test-revisions", Endpoint: "https://oss.example.invalid", Region: "cn-test", UseCName: true}
	sealed, err := s.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)
		on conflict(key) do update set value=excluded.value`, sealed); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into oss_files(bucket,endpoint,object_key,uploader_id,source,status,scan_status,content_type)
		values($1,$2,$3,$4,'user','active','clean','image/png')`, cfg.Bucket, cfg.Endpoint, objectKey, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "project.review")
	snapshot := revisionOutcomeInput()
	snapshot.IconURL = cfg.Endpoint + "/" + objectKey
	const path = "/api/v1/mods/test013-mod/revisions"
	checkReceipt := func(raw []byte, status string) modRevisionResponse {
		t.Helper()
		var out struct {
			Data struct {
				modRevisionResponse
				IconUnavailable bool `json:"iconUnavailable"`
			}
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		if !out.Data.IconUnavailable || out.Data.Snapshot.IconURL != "" || out.Data.Status != status || out.Data.ID == "" || out.Data.ChangeRequestID == "" || bytes.Contains(raw, []byte(objectKey)) {
			t.Fatalf("missing committed revision outcome or contained the unresolvable icon: %s", raw)
		}
		return out.Data.modRevisionResponse
	}
	created := checkReceipt(f.require(t, f.editor, http.MethodPost, path, submitModRevisionRequest{Snapshot: snapshot, ChangeReason: "image unavailable"}, http.StatusCreated), "pending")
	reviewed := checkReceipt(f.require(t, f.reviewer, http.MethodPatch, path+"/"+created.ID, reviewModRevisionRequest{Status: "rejected"}, http.StatusOK), "rejected")
	if created.ID != reviewed.ID || created.ChangeRequestID != reviewed.ChangeRequestID {
		t.Fatalf("review changed receipt identity: %s vs %s", created.ID, reviewed.ID)
	}
	var ids []string
	if err = f.db.QueryRow(f.ctx, `select array_agg(revision.snapshot->>'iconUrl') from content_revisions revision
		where revision.entity_type='mod' and revision.entity_id=$1`, f.modID).Scan(&ids); err != nil || len(ids) != 1 || ids[0] != snapshot.IconURL {
		t.Fatalf("immutable revision icons: %v err=%v", ids, err)
	}

	f.require(t, f.reviewer, http.MethodGet, path+"/compare?before="+randomCatalogPublicID()+"&after="+created.ID, nil, http.StatusNotFound)
	if _, err = f.db.Exec(f.ctx, "alter table review_events rename column note to unavailable_note"); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.reviewer, http.MethodGet, path+"/compare?before="+created.ID+"&after="+created.ID, nil, http.StatusInternalServerError)
}

func TestModRevisionComparisonPresentsChangedIconsFromVisibleSnapshotsIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	s := f.origin.Config.Handler.(*Server)
	const endpoint = "https://oss.example.invalid"
	const prefix = "retired-mod-covers/"
	cfg := ossConfigPayload{Enabled: true, Bucket: "icons", Endpoint: endpoint, Region: "cn-test", UseCName: true}
	sealed, err := s.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)
		on conflict(key) do update set value=excluded.value`, sealed); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i, key := range []string{"old.png", "new.png"} {
		var revision string
		err = f.db.QueryRow(f.ctx, `with revision as (
			insert into content_revisions(entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by,source)
			values('mod',$1,'mod',$2,$3,jsonb_build_object('iconUrl',$4::text),$5,$6,'test') returning id,public_id
		), decision as (
			insert into change_requests(entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,submitted_by)
			select 'mod',$1,'mod',$2,id,'approved',$6 from revision
		) select public_id from revision`, f.modID, f.modCode, i+1, endpoint+"/"+prefix+key, key, f.userIDs[f.editor]).Scan(&revision)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, revision)
	}
	raw := f.require(t, "", http.MethodGet, "/api/v1/mods/test013-mod/revisions/compare?before="+ids[0]+"&after="+ids[1], nil, http.StatusOK)
	compared := decodeTEST022Data[modRevisionComparisonResponse](t, raw)
	if len(compared.Changes) != 1 || compared.Changes[0].Path != "/iconUrl" || compared.Changes[0].Operation != "replace" ||
		compared.Changes[0].Before != compared.Before.Snapshot.IconURL || compared.Changes[0].After != compared.After.Snapshot.IconURL ||
		len(compared.ChangedFields) != 1 || compared.ChangedFields[0] != "iconUrl" || bytes.Contains(raw, []byte(prefix)) {
		t.Fatalf("comparison should mark the changed icon with only its projected values: %s", raw)
	}
}
