package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type auditOSS001DraftToolProfile struct{ kind, creator, file, draft, reference, signed string }

func auditOSS001DraftToolSave(t *testing.T, f auditFixGRFixture, actor, key, kind string, payload map[string]any) string {
	t.Helper()
	result := decodeTEST022Data[struct {
		ID string `json:"id"`
	}](t, f.require(t, actor, http.MethodPost, "/api/v1/users/me/drafts", map[string]any{"draftKey": key, "projectKey": key, "kind": "creator", "title": "Owned avatar repair input", "editUrl": "/" + map[string]string{"author": "authors", "team": "teams"}[kind] + "/new", "payload": payload}, 200))
	if result.ID == "" {
		t.Fatal("actual private draft API did not return a public identity")
	}
	return result.ID
}

func auditOSS001DraftToolPayload(t *testing.T, f auditFixGRFixture, id string) map[string]any {
	t.Helper()
	var raw []byte
	if err := f.db.QueryRow(f.ctx, `select payload from user_drafts where public_id=$1`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal("owned draft JSON invalid")
	}
	return result
}

func auditOSS001DraftToolFacts(t *testing.T, f auditFixGRFixture, includeDrafts bool) []byte {
	t.Helper()
	var raw []byte
	query := `select jsonb_build_object(
		'creators',(select jsonb_agg(to_jsonb(c) order by id) from creators c),
		'files',(select jsonb_agg(to_jsonb(c) order by id) from oss_files c),
		'byteReceipts',(select jsonb_agg(to_jsonb(c) order by id) from oss_upload_byte_bindings c),
		'revisions',(select jsonb_agg(to_jsonb(c) order by id) from content_revisions c),
		'requests',(select jsonb_agg(to_jsonb(c) order by id) from change_requests c),
		'reviewEvents',(select jsonb_agg(to_jsonb(c) order by id) from review_events c),
		'audit',(select jsonb_agg(to_jsonb(c) order by id) from audit_events c),
		'permissions',(select jsonb_agg(to_jsonb(c) order by user_id,permission_id,source,source_key) from user_permissions c))`
	if includeDrafts {
		query += `||jsonb_build_object('drafts',(select jsonb_agg(to_jsonb(c) order by id) from user_drafts c))`
	}
	if err := f.db.QueryRow(f.ctx, query).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func auditOSS001DraftToolSetup(t *testing.T, f auditFixGRFixture, kinds ...string) (CreatorAvatarDraftRepairScope, []auditOSS001DraftToolProfile) {
	t.Helper()
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "creator.create", "user.draft.retention_seconds.3600")
	var scope CreatorAvatarDraftRepairScope
	if err := f.db.QueryRow(f.ctx, `select current_schema(),public_id from users where id=$1`, f.userIDs[f.editor]).Scan(&scope.Schema, &scope.OwnerPublicID); err != nil {
		t.Fatal(err)
	}
	profiles := []auditOSS001DraftToolProfile{}
	for index, kind := range kinds {
		_, fileID := auditOSS001Upload(t, f, f.editor, "mod_resource:audit-gr-"+f.modCode+":item:icon", auditOSS001PNG(t, uint8(50+index*110)))
		var key string
		if err := f.db.QueryRow(f.ctx, `select object_key from oss_files where public_id=$1`, fileID).Scan(&key); err != nil {
			t.Fatal(err)
		}
		cfg := f.server.ossConfigFromSettings(f.ctx)
		stable := ossStoredObjectURL(cfg, key)
		creator := auditOSS001CreateReviewedCreatorWithAvatar(t, f, kind, fileID, stable)
		access, err := f.server.resolveOSSObjectAccessWithConfig(f.ctx, cfg, key, ossObjectAccessOptions{})
		if err != nil {
			t.Fatal(err)
		}
		payload := map[string]any{"name": "Original private input " + kind, "avatarUrl": access.URL, "avatarFileId": fileID, "businessExtension": map[string]any{"quantity": 3, "probability": 0.25, "label": "retain verbatim"}}
		draft := auditOSS001DraftToolSave(t, f, f.editor, "creator:"+creator, kind, payload)
		scope.DraftIDs = append(scope.DraftIDs, draft)
		profiles = append(profiles, auditOSS001DraftToolProfile{kind, creator, fileID, draft, stable, access.URL})
	}
	return scope, profiles
}

func auditOSS001DraftToolRecoveryWriter(t *testing.T, path string, destination *CreatorAvatarDraftRepairRecovery) func(CreatorAvatarDraftRepairRecovery) error {
	t.Helper()
	return func(recovery CreatorAvatarDraftRepairRecovery) error {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		defer file.Close()
		if err = json.NewEncoder(file).Encode(recovery); err != nil {
			return err
		}
		if err = file.Sync(); err != nil {
			return err
		}
		*destination = recovery
		return nil
	}
}

func TestAuditOSS001DraftRepairDryRunApplyIdempotenceCASAndRollbackOnActualHTTPPostgresData(t *testing.T) {
	f := newAuditFixGRFixture(t)
	scope, profiles := auditOSS001DraftToolSetup(t, f, "author", "team")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "user.draft.retention_seconds.3600")
	newDraft := auditOSS001DraftToolSave(t, f, f.editor, "creator:new:author", "author", map[string]any{"avatarFileId": profiles[0].file, "avatarUrl": profiles[0].signed})
	unknown := auditOSS001DraftToolSave(t, f, f.editor, "creator:"+randomCatalogPublicID(), "author", map[string]any{"avatarFileId": profiles[0].file, "avatarUrl": profiles[0].signed})
	foreign := auditOSS001DraftToolSave(t, f, f.otherEditor, "creator:"+profiles[0].creator, "author", map[string]any{"avatarFileId": profiles[0].file, "avatarUrl": profiles[0].signed})
	scope.DraftIDs = append(scope.DraftIDs, newDraft, unknown, foreign, randomCatalogPublicID())
	tool := NewCreatorAvatarDraftRepairTool(f.server.cfg, f.db)
	before := auditOSS001DraftToolFacts(t, f, true)
	publicBefore := auditOSS001DraftToolFacts(t, f, false)
	plan, dry, err := tool.Plan(f.ctx, scope)
	if err != nil || dry.Eligible != 2 || dry.Held != 4 || dry.Changed != 0 || dry.Committed || !bytes.Equal(before, auditOSS001DraftToolFacts(t, f, true)) {
		t.Fatal("dry-run must read exact two eligible targets, hold four out-of-scope targets and write nothing", err)
	}
	if plan.Entries[4].ReferenceBefore != "" || plan.Entries[5].ReferenceBefore != "" {
		t.Fatal("wrong owner and missing draft must not expose private references even in a plan")
	}
	privateDirectory := t.TempDir()
	// Real selected-row snapshot bytes are verified privately. This is an owned
	// synthetic-copy backup declaration, not a claim of production pg_dump or
	// restore verification. The CLI additionally checks actual file ownership,
	// mode and bytes against its explicit operator declaration.
	backupPath := filepath.Join(privateDirectory, "selected-rows-backup.json")
	if err = os.WriteFile(backupPath, before, 0600); err != nil {
		t.Fatal(err)
	}
	backup := CreatorAvatarDraftRepairBackup{TargetSHA256: plan.TargetSHA256, FileSHA256: fmt.Sprintf("%x", sha256.Sum256(before))}
	if _, err = tool.Apply(f.ctx, plan, strings.Repeat("0", 64), backup, func(CreatorAvatarDraftRepairRecovery) error { return nil }); err == nil {
		t.Fatal("wrong reviewed target must fail closed")
	}
	tampered := plan
	tampered.Entries = append([]CreatorAvatarDraftRepairEntry(nil), plan.Entries...)
	tampered.Entries[0].ReferenceAfter = "https://untrusted.example.invalid/replacement"
	if _, err = tool.Apply(f.ctx, tampered, plan.TargetSHA256, backup, func(CreatorAvatarDraftRepairRecovery) error { return nil }); err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatal("modified private plan must not authorize an unreviewed replacement")
	}
	failed, err := tool.Apply(f.ctx, plan, plan.TargetSHA256, backup, func(CreatorAvatarDraftRepairRecovery) error {
		return errors.New("synthetic durable backup storage failure")
	})
	if err == nil || !strings.Contains(err.Error(), "recovery persistence") || failed.Committed || failed.Changed != 0 || !bytes.Equal(before, auditOSS001DraftToolFacts(t, f, true)) {
		t.Fatal("private rollback persistence failure must roll back the entire prepared batch")
	}
	var recovery CreatorAvatarDraftRepairRecovery
	applied, err := tool.Apply(f.ctx, plan, plan.TargetSHA256, backup, auditOSS001DraftToolRecoveryWriter(t, filepath.Join(privateDirectory, "apply-recovery.json"), &recovery))
	if err != nil || !applied.Committed || applied.Changed != 2 || applied.Held != 4 || len(recovery.Entries) != 2 {
		t.Fatal("exact eligible two-row batch was not committed with recovery", err)
	}
	for _, profile := range profiles {
		payload := auditOSS001DraftToolPayload(t, f, profile.draft)
		if payload["avatarUrl"] != profile.reference || payload["avatarFileId"] != profile.file || payload["name"] != "Original private input "+profile.kind || payload["businessExtension"].(map[string]any)["probability"] != 0.25 {
			t.Fatal("repair must change only registered avatarUrl and retain all other private business input")
		}
	}
	if !bytes.Equal(publicBefore, auditOSS001DraftToolFacts(t, f, false)) {
		t.Fatal("repair mutated creator, asset, permissions, byte proof or immutable audit/review history")
	}
	var ignored CreatorAvatarDraftRepairRecovery
	repeated, err := tool.Apply(f.ctx, plan, plan.TargetSHA256, backup, auditOSS001DraftToolRecoveryWriter(t, filepath.Join(privateDirectory, "idempotent-apply-recovery.json"), &ignored))
	if err != nil || repeated.Changed != 0 || repeated.Already != 2 || repeated.Held != 4 {
		t.Fatal("replaying original plan must be idempotent, without rewriting stable references", err)
	}
	rolled, err := tool.Rollback(f.ctx, recovery, plan.TargetSHA256, backup, auditOSS001DraftToolRecoveryWriter(t, filepath.Join(privateDirectory, "rollback-recovery.json"), &ignored))
	if err != nil || rolled.Changed != 2 || !rolled.Committed {
		t.Fatal("bounded rollback must restore exact original private field", err)
	}
	for _, profile := range profiles {
		if auditOSS001DraftToolPayload(t, f, profile.draft)["avatarUrl"] != profile.signed {
			t.Fatal("rollback lost the original private value")
		}
	}
	rolledAgain, err := tool.Rollback(f.ctx, recovery, plan.TargetSHA256, backup, auditOSS001DraftToolRecoveryWriter(t, filepath.Join(privateDirectory, "idempotent-rollback-recovery.json"), &ignored))
	if err != nil || rolledAgain.Changed != 0 || rolledAgain.Already != 2 {
		t.Fatal("repeated rollback must be idempotent", err)
	}
	// Deterministic barrier: exact plan read -> real same-payload autosave commit
	// -> apply. xmin must reject even when the payload hash itself is identical.
	versionPlan, _, err := tool.Plan(f.ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	payload := auditOSS001DraftToolPayload(t, f, profiles[0].draft)
	auditOSS001DraftToolSave(t, f, f.editor, "creator:"+profiles[0].creator, "author", payload)
	beforeCAS := auditOSS001DraftToolFacts(t, f, true)
	failed, err = tool.Apply(f.ctx, versionPlan, versionPlan.TargetSHA256, backup, func(CreatorAvatarDraftRepairRecovery) error {
		t.Error("must not persist successful recovery on CAS conflict")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "CAS conflict") || failed.Changed != 0 || !bytes.Equal(beforeCAS, auditOSS001DraftToolFacts(t, f, true)) {
		t.Fatal("same-payload row-version conflict must leave the entire batch untouched")
	}
	contentPlan, _, err := tool.Plan(f.ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	payload = auditOSS001DraftToolPayload(t, f, profiles[1].draft)
	payload["name"] = "User typed after plan"
	auditOSS001DraftToolSave(t, f, f.editor, "creator:"+profiles[1].creator, "team", payload)
	beforeCAS = auditOSS001DraftToolFacts(t, f, true)
	if _, err = tool.Apply(f.ctx, contentPlan, contentPlan.TargetSHA256, backup, func(CreatorAvatarDraftRepairRecovery) error { return nil }); err == nil || !strings.Contains(err.Error(), "CAS conflict") || !bytes.Equal(beforeCAS, auditOSS001DraftToolFacts(t, f, true)) {
		t.Fatal("new private input must survive a stale payload plan")
	}
	fresh, _, err := tool.Plan(f.ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	var latestRecovery CreatorAvatarDraftRepairRecovery
	if _, err = tool.Apply(f.ctx, fresh, fresh.TargetSHA256, backup, auditOSS001DraftToolRecoveryWriter(t, filepath.Join(privateDirectory, "fresh-apply-recovery.json"), &latestRecovery)); err != nil {
		t.Fatal(err)
	}
	payload = auditOSS001DraftToolPayload(t, f, profiles[1].draft)
	payload["businessExtension"].(map[string]any)["label"] = "New input after apply"
	auditOSS001DraftToolSave(t, f, f.editor, "creator:"+profiles[1].creator, "team", payload)
	beforeCAS = auditOSS001DraftToolFacts(t, f, true)
	if _, err = tool.Rollback(f.ctx, latestRecovery, fresh.TargetSHA256, backup, func(CreatorAvatarDraftRepairRecovery) error { return nil }); err == nil || !strings.Contains(err.Error(), "CAS conflict") || !bytes.Equal(beforeCAS, auditOSS001DraftToolFacts(t, f, true)) {
		t.Fatal("rollback must not overwrite legitimate edits made after repair")
	}
	t.Log("exact6 inputs:2eligible4held; read-only,private-recovery failure atomicity,apply2/reapply0/rollback2/re-rollback0,deterministic version+payload conflicts and stale rollback PASS; no public objects or audit/file/permission changes")
}

func TestAuditOSS001DraftRepairHoldsUnknownOriginsBusinessQueriesLegacyProofAndChangedProvider(t *testing.T) {
	f := newAuditFixGRFixture(t)
	scope, profiles := auditOSS001DraftToolSetup(t, f, "author")
	profile := profiles[0]
	tool := NewCreatorAvatarDraftRepairTool(f.server.cfg, f.db)
	original := auditOSS001DraftToolPayload(t, f, profile.draft)
	foreign, err := url.Parse(profile.signed)
	if err != nil {
		t.Fatal(err)
	}
	foreign.Scheme, foreign.Host = "https", "foreign.example.invalid"
	for name, reference := range map[string]string{"foreign_origin": foreign.String(), "unregistered_business_query": profile.signed + "&size=256"} {
		t.Run(name, func(t *testing.T) {
			payload := auditOSS001DraftToolPayload(t, f, profile.draft)
			payload["avatarUrl"] = reference
			auditOSS001DraftToolSave(t, f, f.editor, "creator:"+profile.creator, "author", payload)
			before := auditOSS001DraftToolFacts(t, f, true)
			plan, summary, err := tool.Plan(f.ctx, scope)
			if err != nil || summary.Eligible != 0 || summary.Held != 1 || plan.Entries[0].Status != "HOLD_UNPROVED_CURRENT_STORAGE_OR_BYTE_BINDING" || !bytes.Equal(before, auditOSS001DraftToolFacts(t, f, true)) {
				t.Fatal("unknown source/query must HOLD without stripping business data or accepting a URL as trust", err)
			}
		})
	}
	auditOSS001DraftToolSave(t, f, f.editor, "creator:"+profile.creator, "author", original)
	// Explicit old-data/fault fixture: a boolean is not a committed byte receipt.
	if _, err = f.db.Exec(f.ctx, `update oss_files set source_hash_verified=false where public_id=$1`, profile.file); err != nil {
		t.Fatal(err)
	}
	plan, summary, err := tool.Plan(f.ctx, scope)
	if err != nil || summary.Eligible != 0 || plan.Entries[0].Status != "HOLD_UNPROVED_CURRENT_STORAGE_OR_BYTE_BINDING" {
		t.Fatal("legacy missing source proof must not be promoted by a draft maintenance tool", err)
	}
	if _, err = f.db.Exec(f.ctx, `update oss_files set source_hash_verified=true where public_id=$1`, profile.file); err != nil {
		t.Fatal(err)
	}
	plan, _, err = tool.Plan(f.ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	cfg := f.server.ossConfigFromSettings(f.ctx)
	cfg.DownloadURLTTLMinutes++
	sealed, err := f.server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `update system_settings set value=$1::jsonb where key='oss.aliyun'`, sealed); err != nil {
		t.Fatal(err)
	}
	before := auditOSS001DraftToolFacts(t, f, true)
	backup := CreatorAvatarDraftRepairBackup{TargetSHA256: plan.TargetSHA256, FileSHA256: fmt.Sprintf("%x", sha256.Sum256(before))}
	if _, err = tool.Apply(f.ctx, plan, plan.TargetSHA256, backup, func(CreatorAvatarDraftRepairRecovery) error { return nil }); err == nil || !strings.Contains(err.Error(), "actual database/schema/provider settings differ") || !bytes.Equal(before, auditOSS001DraftToolFacts(t, f, true)) {
		t.Fatal("changed real provider settings must invalidate the reviewed target without any writes")
	}
	t.Log("foreign URL/business query/missing actual source proof and changed configured provider each HOLD/reject precisely; no self-authorized operator base, no network or scan promotion")
}
