package httpapi

// Offline maintenance only. Nothing in this file registers an HTTP route or
// starts Server workers. Plans/recovery contain old capabilities and MUST be
// kept in owner-only private files; Summary intentionally has no URL/body.
import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
)

type CreatorAvatarDraftRepairScope struct {
	Schema        string   `json:"schema"`
	OwnerPublicID string   `json:"ownerPublicId"`
	DraftIDs      []string `json:"draftIds"`
}

type CreatorAvatarDraftRepairTarget struct {
	Database             string `json:"database"`
	DatabaseOID          string `json:"databaseOid"`
	Schema               string `json:"schema"`
	SchemaOID            string `json:"schemaOid"`
	ServerAddress        string `json:"serverAddress"`
	ServerPort           int    `json:"serverPort"`
	ClientAddressSHA256  string `json:"clientAddressSha256"`
	SealedSettingsSHA256 string `json:"sealedSettingsSha256"`
}

type CreatorAvatarDraftRepairEntry struct {
	DraftID         string `json:"draftId"`
	Status          string `json:"status"`
	CreatorID       string `json:"creatorId,omitempty"`
	PublishedID     string `json:"publishedRevisionId,omitempty"`
	FileID          string `json:"fileId,omitempty"`
	BeforeSHA256    string `json:"beforePayloadSha256,omitempty"`
	AfterSHA256     string `json:"afterPayloadSha256,omitempty"`
	BeforeVersion   string `json:"beforeRowVersion,omitempty"`
	AfterVersion    string `json:"afterRowVersion,omitempty"`
	ReferenceBefore string `json:"privateReferenceBefore,omitempty"`
	ReferenceAfter  string `json:"privateReferenceAfter,omitempty"`
}

type CreatorAvatarDraftRepairPlan struct {
	FormatVersion int                             `json:"formatVersion"`
	CreatedAt     time.Time                       `json:"createdAt"`
	Scope         CreatorAvatarDraftRepairScope   `json:"scope"`
	Target        CreatorAvatarDraftRepairTarget  `json:"target"`
	TargetSHA256  string                          `json:"targetSha256"`
	Entries       []CreatorAvatarDraftRepairEntry `json:"entries"`
	IntegrityHMAC string                          `json:"integrityHmac"`
}

type CreatorAvatarDraftRepairBackup struct {
	TargetSHA256 string `json:"targetSha256"`
	FileSHA256   string `json:"verifiedBackupFileSha256"`
}

type CreatorAvatarDraftRepairRecovery struct {
	FormatVersion int                             `json:"formatVersion"`
	CreatedAt     time.Time                       `json:"createdAt"`
	Operation     string                          `json:"operation"`
	CommitState   string                          `json:"commitState"`
	Scope         CreatorAvatarDraftRepairScope   `json:"scope"`
	Target        CreatorAvatarDraftRepairTarget  `json:"target"`
	TargetSHA256  string                          `json:"targetSha256"`
	Backup        CreatorAvatarDraftRepairBackup  `json:"backupDeclaration"`
	Entries       []CreatorAvatarDraftRepairEntry `json:"entries"`
	IntegrityHMAC string                          `json:"integrityHmac"`
}

type CreatorAvatarDraftRepairSummary struct {
	Operation    string            `json:"operation"`
	TargetSHA256 string            `json:"targetSha256"`
	Committed    bool              `json:"committed"`
	Requested    int               `json:"requested"`
	Changed      int               `json:"changed"`
	Eligible     int               `json:"eligible"`
	Already      int               `json:"alreadyAtTarget"`
	Held         int               `json:"held"`
	Items        map[string]string `json:"items"`
}

type CreatorAvatarDraftRepairTool struct{ server *Server }

func NewCreatorAvatarDraftRepairTool(cfg config.Config, db *pgxpool.Pool) *CreatorAvatarDraftRepairTool {
	// This literal intentionally avoids NewServer: dry-run cannot start queue,
	// recovery, notification or other background business workers.
	return &CreatorAvatarDraftRepairTool{server: &Server{cfg: cfg, db: db}}
}

func creatorAvatarDraftRepairHash(value []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(value))
}

func (tool *CreatorAvatarDraftRepairTool) integrity(kind string, value any) string {
	raw, _ := json.Marshal(value)
	mac := hmac.New(sha256.New, []byte(tool.server.cfg.SettingsEncryptionKey))
	mac.Write([]byte("mcmods/oss001/private-avatar-draft-repair/" + kind + "/v1\x00"))
	mac.Write(raw)
	return fmt.Sprintf("%x", mac.Sum(nil))
}

func validateCreatorAvatarDraftRepairScope(scope CreatorAvatarDraftRepairScope) error {
	if !regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`).MatchString(scope.Schema) || !validCatalogPublicID(scope.OwnerPublicID) || len(scope.DraftIDs) < 1 || len(scope.DraftIDs) > 100 {
		return errors.New("require exact schema, owner public ID and 1..100 explicit draft public IDs")
	}
	seen := map[string]bool{}
	for _, id := range scope.DraftIDs {
		if !validCatalogPublicID(id) || seen[id] {
			return errors.New("draft public IDs must be valid and distinct")
		}
		seen[id] = true
	}
	return nil
}

func (tool *CreatorAvatarDraftRepairTool) begin(ctx context.Context, schema string, write bool) (pgx.Tx, CreatorAvatarDraftRepairTarget, ossConfigPayload, error) {
	var target CreatorAvatarDraftRepairTarget
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	if write {
		options = pgx.TxOptions{IsoLevel: pgx.ReadCommitted}
	}
	tx, err := tool.server.db.BeginTx(ctx, options)
	if err != nil {
		return nil, target, ossConfigPayload{}, err
	}
	fail := func(err error) (pgx.Tx, CreatorAvatarDraftRepairTarget, ossConfigPayload, error) {
		tx.Rollback(context.Background())
		return nil, target, ossConfigPayload{}, err
	}
	if _, err = tx.Exec(ctx, `select set_config('search_path',$1,true),set_config('statement_timeout','10s',true),set_config('lock_timeout','1s',true)`, "pg_catalog,"+pgx.Identifier{schema}.Sanitize()+",pg_temp"); err != nil {
		return fail(err)
	}
	if write {
		if _, err = tx.Exec(ctx, `select 1 from system_settings where key='oss.aliyun' for share`); err != nil {
			return fail(err)
		}
	}
	cfg, err := tool.server.ossConfigFromSettingsStrictWithQueryer(ctx, tx)
	if err != nil {
		return fail(err)
	}
	if !cfg.Enabled || cfg.Bucket == "" || cfg.Endpoint == "" || !validHTTPURL(cfg.Endpoint) || (cfg.PublicEndpoint != "" && !validHTTPURL(cfg.PublicEndpoint)) {
		return fail(errors.New("configured existing OSS storage is required; operator URLs cannot create trust"))
	}
	var sealed []byte
	err = tx.QueryRow(ctx, `select current_database(),(select oid::text from pg_database where datname=current_database()),$1::text,
		(select oid::text from pg_namespace where nspname=$1),coalesce(inet_server_addr()::text,'local-socket'),coalesce(inet_server_port(),0),
		(select value::text from system_settings where key='oss.aliyun')`, schema).Scan(&target.Database, &target.DatabaseOID, &target.Schema, &target.SchemaOID, &target.ServerAddress, &target.ServerPort, &sealed)
	if err != nil {
		return fail(err)
	}
	connection := tool.server.db.Config().ConnConfig
	address, _ := json.Marshal(struct {
		Host, Database, User string
		Port                 uint16
		TLS                  bool
	}{connection.Host, connection.Database, connection.User, connection.Port, connection.TLSConfig != nil})
	target.ClientAddressSHA256 = creatorAvatarDraftRepairHash(address)
	target.SealedSettingsSHA256 = creatorAvatarDraftRepairHash(sealed)
	return tx, target, cfg, nil
}

func creatorAvatarDraftRepairTargetHash(target CreatorAvatarDraftRepairTarget) string {
	raw, _ := json.Marshal(target)
	return creatorAvatarDraftRepairHash(raw)
}

func (tool *CreatorAvatarDraftRepairTool) inspect(ctx context.Context, tx pgx.Tx, cfg ossConfigPayload, scope CreatorAvatarDraftRepairScope, id string) (CreatorAvatarDraftRepairEntry, error) {
	entry := CreatorAvatarDraftRepairEntry{DraftID: id, Status: "HOLD_NOT_OWNED_ACTIVE_CREATOR_DRAFT"}
	var key string
	var raw []byte
	err := tx.QueryRow(ctx, `select draft.draft_key,draft.payload::text,draft.xmin::text,encode(sha256(convert_to(draft.payload::text,'UTF8')),'hex')
		from user_drafts draft join users actor on actor.id=draft.user_id and actor.public_id=$2 and actor.status='active'
		where draft.public_id=$1 and draft.kind='creator' and draft.submitted_at is null and draft.expires_at>clock_timestamp()`, id, scope.OwnerPublicID).Scan(&key, &raw, &entry.BeforeVersion, &entry.BeforeSHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return entry, nil
	}
	if err != nil {
		return entry, err
	}
	entry.CreatorID = strings.TrimPrefix(key, "creator:")
	if !strings.HasPrefix(key, "creator:") || !validCatalogPublicID(entry.CreatorID) {
		entry.Status = "HOLD_NEW_OR_UNKNOWN_TARGET"
		return entry, nil
	}
	var payload struct {
		URL    string `json:"avatarUrl"`
		FileID string `json:"avatarFileId"`
	}
	if json.Unmarshal(raw, &payload) != nil || !validCatalogPublicID(payload.FileID) {
		entry.Status = "HOLD_UNKNOWN_REFERENCE_FORMAT"
		return entry, nil
	}
	entry.ReferenceBefore, entry.FileID = payload.URL, payload.FileID
	var currentReference, objectKey, endpoint, bucket, contentType, state, scan string
	var proof, verified bool
	err = tx.QueryRow(ctx, `select creator.avatar_url,published.public_id,file.object_key,file.endpoint,file.bucket,file.content_type,file.status,file.scan_status,file.source_hash_verified,`+ossCommittedSourceHashProofSQL+`
		from creators creator join content_revisions published on published.id=creator.published_revision_id
		join oss_files file on file.id=creator.avatar_file_id and file.public_id=$2
		where creator.public_id=$1 and creator.kind in ('author','team') and creator.review_status='approved'`, entry.CreatorID, entry.FileID).Scan(&currentReference, &entry.PublishedID, &objectKey, &endpoint, &bucket, &contentType, &state, &scan, &verified, &proof)
	if errors.Is(err, pgx.ErrNoRows) {
		entry.Status = "HOLD_NOT_SAME_CURRENT_PUBLISHED_FILE"
		return entry, nil
	}
	if err != nil {
		return entry, err
	}
	if state != "active" || !trustedOSSScanStatus(scan) || !safeRasterContentType(contentType) || !verified || !proof || bucket != cfg.Bucket || endpoint != cfg.displayEndpoint() ||
		currentReference != ossStoredObjectURL(cfg, objectKey) || temporaryCreatorAvatarReference(currentReference) || !creatorAvatarReferenceMatchesFile(cfg, payload.URL, objectKey) {
		entry.Status = "HOLD_UNPROVED_CURRENT_STORAGE_OR_BYTE_BINDING"
		return entry, nil
	}
	entry.ReferenceAfter = currentReference
	if err = tx.QueryRow(ctx, `select encode(sha256(convert_to(jsonb_set(payload,'{avatarUrl}',to_jsonb($2::text),false)::text,'UTF8')),'hex') from user_drafts where public_id=$1`, id, entry.ReferenceAfter).Scan(&entry.AfterSHA256); err != nil {
		return entry, err
	}
	if !temporaryCreatorAvatarReference(payload.URL) {
		entry.Status = "UNCHANGED_STABLE"
		return entry, nil
	}
	entry.Status = "ELIGIBLE"
	return entry, nil
}

func (tool *CreatorAvatarDraftRepairTool) Plan(ctx context.Context, scope CreatorAvatarDraftRepairScope) (CreatorAvatarDraftRepairPlan, CreatorAvatarDraftRepairSummary, error) {
	var plan CreatorAvatarDraftRepairPlan
	summary := CreatorAvatarDraftRepairSummary{Operation: "DRY_RUN", Requested: len(scope.DraftIDs), Items: map[string]string{}}
	if err := validateCreatorAvatarDraftRepairScope(scope); err != nil {
		return plan, summary, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, target, cfg, err := tool.begin(ctx, scope.Schema, false)
	if err != nil {
		return plan, summary, err
	}
	defer tx.Rollback(context.Background())
	plan = CreatorAvatarDraftRepairPlan{FormatVersion: 1, CreatedAt: time.Now().UTC(), Scope: scope, Target: target, TargetSHA256: creatorAvatarDraftRepairTargetHash(target)}
	summary.TargetSHA256 = plan.TargetSHA256
	for _, id := range scope.DraftIDs {
		entry, err := tool.inspect(ctx, tx, cfg, scope, id)
		if err != nil {
			return plan, summary, err
		}
		plan.Entries = append(plan.Entries, entry)
		summary.Items[id] = entry.Status
		if entry.Status != "ELIGIBLE" {
			summary.Held++
		} else {
			summary.Eligible++
		}
	}
	plan.IntegrityHMAC = tool.integrity("plan", plan)
	return plan, summary, tx.Commit(ctx)
}

func validateCreatorAvatarDraftRepairPlan(plan CreatorAvatarDraftRepairPlan) error {
	if err := validateCreatorAvatarDraftRepairScope(plan.Scope); err != nil {
		return err
	}
	if plan.FormatVersion != 1 || plan.Target.Schema != plan.Scope.Schema || creatorAvatarDraftRepairTargetHash(plan.Target) != plan.TargetSHA256 || len(plan.Entries) != len(plan.Scope.DraftIDs) {
		return errors.New("invalid private repair plan version or exact target")
	}
	for i, entry := range plan.Entries {
		if entry.DraftID != plan.Scope.DraftIDs[i] {
			return errors.New("plan entry mapping must preserve explicit input scope")
		}
	}
	return nil
}

// All rows commit atomically. persistRecovery MUST create+fsync a private0600
// recovery file before returning; failure aborts the transaction. Its recorded
// state remains PREPARED_BEFORE_COMMIT even after success, deliberately forcing
// rollback/retry to inspect actual CAS state rather than assume commit outcome.
func (tool *CreatorAvatarDraftRepairTool) Apply(ctx context.Context, plan CreatorAvatarDraftRepairPlan, expectedTarget string, backup CreatorAvatarDraftRepairBackup, persistRecovery func(CreatorAvatarDraftRepairRecovery) error) (CreatorAvatarDraftRepairSummary, error) {
	summary := CreatorAvatarDraftRepairSummary{Operation: "APPLY", Requested: len(plan.Entries), Items: map[string]string{}}
	if err := validateCreatorAvatarDraftRepairPlan(plan); err != nil {
		return summary, err
	}
	mac := plan.IntegrityHMAC
	plan.IntegrityHMAC = ""
	if !hmac.Equal([]byte(mac), []byte(tool.integrity("plan", plan))) {
		return summary, errors.New("private plan integrity or settings key mismatch")
	}
	return tool.mutate(ctx, plan, expectedTarget, backup, false, persistRecovery)
}

func (tool *CreatorAvatarDraftRepairTool) Rollback(ctx context.Context, recovery CreatorAvatarDraftRepairRecovery, expectedTarget string, backup CreatorAvatarDraftRepairBackup, persistRecovery func(CreatorAvatarDraftRepairRecovery) error) (CreatorAvatarDraftRepairSummary, error) {
	mac := recovery.IntegrityHMAC
	recovery.IntegrityHMAC = ""
	if !hmac.Equal([]byte(mac), []byte(tool.integrity("recovery", recovery))) {
		return CreatorAvatarDraftRepairSummary{}, errors.New("private recovery integrity or settings key mismatch")
	}
	plan := CreatorAvatarDraftRepairPlan{FormatVersion: recovery.FormatVersion, Scope: recovery.Scope, Target: recovery.Target, TargetSHA256: recovery.TargetSHA256, Entries: recovery.Entries}
	// A recovery contains only rows actually changed by one apply. Its explicit
	// scope is narrowed accordingly; zero-row idempotent applies are harmless.
	plan.Scope.DraftIDs = nil
	for _, entry := range plan.Entries {
		plan.Scope.DraftIDs = append(plan.Scope.DraftIDs, entry.DraftID)
	}
	if len(plan.Entries) == 0 || recovery.Operation != "APPLY" || recovery.CommitState != "PREPARED_BEFORE_COMMIT" {
		return CreatorAvatarDraftRepairSummary{}, errors.New("require a nonempty original apply recovery")
	}
	if err := validateCreatorAvatarDraftRepairPlan(plan); err != nil {
		return CreatorAvatarDraftRepairSummary{}, err
	}
	return tool.mutate(ctx, plan, expectedTarget, backup, true, persistRecovery)
}

func (tool *CreatorAvatarDraftRepairTool) mutate(ctx context.Context, plan CreatorAvatarDraftRepairPlan, expectedTarget string, backup CreatorAvatarDraftRepairBackup, rollback bool, persistRecovery func(CreatorAvatarDraftRepairRecovery) error) (CreatorAvatarDraftRepairSummary, error) {
	operation := "APPLY"
	if rollback {
		operation = "ROLLBACK"
	}
	summary := CreatorAvatarDraftRepairSummary{Operation: operation, Requested: len(plan.Entries), Items: map[string]string{}}
	if expectedTarget != plan.TargetSHA256 || backup.TargetSHA256 != expectedTarget || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(backup.FileSHA256) || persistRecovery == nil {
		return summary, errors.New("exact target fingerprint and verified backup declaration plus private durable recovery writer required")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, target, cfg, err := tool.begin(ctx, plan.Scope.Schema, true)
	if err != nil {
		return summary, err
	}
	defer tx.Rollback(context.Background())
	summary.TargetSHA256 = creatorAvatarDraftRepairTargetHash(target)
	if summary.TargetSHA256 != expectedTarget {
		return summary, errors.New("actual database/schema/provider settings differ from reviewed target")
	}
	entries := append([]CreatorAvatarDraftRepairEntry(nil), plan.Entries...)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatorID+entries[i].DraftID < entries[j].CreatorID+entries[j].DraftID
	})
	// Current creator and file must stay the same through the private-draft CAS.
	// Lock source rows before draft rows, following creator->file business order.
	for _, entry := range entries {
		if entry.Status == "ELIGIBLE" {
			if _, err = tx.Exec(ctx, `select 1 from creators where public_id=$1 for share`, entry.CreatorID); err != nil {
				return summary, err
			}
			if _, err = tx.Exec(ctx, `select 1 from oss_files where public_id=$1 for share`, entry.FileID); err != nil {
				return summary, err
			}
		}
	}
	recovery := CreatorAvatarDraftRepairRecovery{FormatVersion: 1, CreatedAt: time.Now().UTC(), Operation: operation, CommitState: "PREPARED_BEFORE_COMMIT", Scope: plan.Scope, Target: target, TargetSHA256: expectedTarget, Backup: backup}
	preparedChanges := 0
	for _, entry := range entries {
		if entry.Status != "ELIGIBLE" {
			summary.Held++
			summary.Items[entry.DraftID] = entry.Status
			continue
		}
		if _, err = tx.Exec(ctx, `select 1 from user_drafts where public_id=$1 and user_id=(select id from users where public_id=$2) for update`, entry.DraftID, plan.Scope.OwnerPublicID); err != nil {
			return summary, err
		}
		current, err := tool.inspect(ctx, tx, cfg, plan.Scope, entry.DraftID)
		if err != nil {
			return summary, err
		}
		if current.CreatorID != entry.CreatorID || current.FileID != entry.FileID || current.PublishedID != entry.PublishedID || current.ReferenceAfter != entry.ReferenceAfter {
			return summary, errors.New("CAS conflict: current creator, file or trusted storage proof changed; no batch committed")
		}
		beforeHash, afterHash, beforeVersion, destination := entry.BeforeSHA256, entry.AfterSHA256, entry.BeforeVersion, entry.ReferenceAfter
		if rollback {
			beforeHash, afterHash, beforeVersion, destination = entry.AfterSHA256, entry.BeforeSHA256, entry.AfterVersion, entry.ReferenceBefore
		}
		if current.BeforeSHA256 == afterHash {
			summary.Already++
			summary.Items[entry.DraftID] = "ALREADY_AT_TARGET"
			continue
		}
		if current.BeforeSHA256 != beforeHash || current.BeforeVersion != beforeVersion || (current.Status != "ELIGIBLE" && current.Status != "UNCHANGED_STABLE") {
			return summary, errors.New("CAS conflict: draft payload or row version changed; no batch committed")
		}
		if !rollback && (current.ReferenceBefore != entry.ReferenceBefore || current.AfterSHA256 != entry.AfterSHA256) {
			return summary, errors.New("reviewed exact replacement no longer matches; no batch committed")
		}
		var resultingHash, resultingVersion string
		err = tx.QueryRow(ctx, `update user_drafts set payload=jsonb_set(payload,'{avatarUrl}',to_jsonb($3::text),false),updated_at=clock_timestamp()
			where public_id=$1 and user_id=(select id from users where public_id=$2) and xmin::text=$4 and encode(sha256(convert_to(payload::text,'UTF8')),'hex')=$5
			returning encode(sha256(convert_to(payload::text,'UTF8')),'hex'),xmin::text`, entry.DraftID, plan.Scope.OwnerPublicID, destination, beforeVersion, beforeHash).Scan(&resultingHash, &resultingVersion)
		if err != nil || resultingHash != afterHash {
			return summary, errors.New("CAS conflict: exact payload replacement failed; no batch committed")
		}
		if !rollback {
			entry.AfterVersion = resultingVersion
		}
		recovery.Entries = append(recovery.Entries, entry)
		preparedChanges++
		summary.Items[entry.DraftID] = operation + "_PREPARED"
	}
	recovery.IntegrityHMAC = tool.integrity("recovery", recovery)
	if err = persistRecovery(recovery); err != nil {
		return summary, errors.New("private durable recovery persistence failed; no batch committed")
	}
	if err = tx.Commit(ctx); err != nil {
		return summary, errors.New("commit outcome must be checked using private recovery CAS; do not assume success")
	}
	summary.Committed = true
	summary.Changed = preparedChanges
	for id, state := range summary.Items {
		if state == operation+"_PREPARED" {
			summary.Items[id] = operation + "_COMMITTED"
		}
	}
	return summary, nil
}
