package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

type OSSByteMaintenancePlan struct {
	Version            int                       `json:"version"`
	Database           string                    `json:"database"`
	MaxDurationSeconds int                       `json:"maxDurationSeconds"`
	MaxFiles           int                       `json:"maxFiles"`
	MaxReadBytes       int64                     `json:"maxReadBytes"`
	MaxReferenceRows   int                       `json:"maxReferenceRows"`
	MaxReferenceCols   int                       `json:"maxReferenceColumns"`
	MaxReferenceBytes  int                       `json:"maxReferenceFieldBytes"`
	RetentionCutoff    time.Time                 `json:"retentionCutoff,omitempty"`
	Entries            []OSSByteMaintenanceEntry `json:"entries"`
}

type OSSByteMaintenanceEntry struct {
	ActionID                string `json:"actionId"`
	Operation               string `json:"operation"`
	FilePublicID            string `json:"fileId,omitempty"`
	BindingID               string `json:"bindingId,omitempty"`
	RawSourceKind           string `json:"rawSourceKind,omitempty"`
	ExpectedDisplayEndpoint string `json:"expectedDisplayEndpoint,omitempty"`
	OriginalSourceKey       string `json:"originalSourceKey,omitempty"`
	ExpectedSourceSHA       string `json:"expectedSourceSha256,omitempty"`
	ExpectedStoredSHA       string `json:"expectedStoredSha256,omitempty"`
	ExpectedSourceSize      int64  `json:"expectedSourceSize,omitempty"`
	ExpectedStoredSize      int64  `json:"expectedStoredSize,omitempty"`
}

type OSSByteMaintenanceOptions struct {
	Apply           bool
	BackupReference string
}

type OSSByteMaintenanceResult struct {
	ActionID          string `json:"actionId"`
	FilePublicID      string `json:"fileId,omitempty"`
	Operation         string `json:"operation"`
	Status            string `json:"status"`
	SourceObservedSHA string `json:"sourceObservedSha256,omitempty"`
	StoredObservedSHA string `json:"storedObservedSha256,omitempty"`
	SourceBytes       int64  `json:"sourceBytes,omitempty"`
	StoredBytes       int64  `json:"storedBytes,omitempty"`
	ReferenceRows     int64  `json:"referenceMatches"`
	ReferencesKnown   bool   `json:"referenceScanComplete"`
	HistoricalProof   bool   `json:"historicalIntentProven"`
}

type OSSByteMaintenanceReport struct {
	DryRun    bool                       `json:"dryRun"`
	ReadBytes int64                      `json:"readBytes"`
	Counts    map[string]int             `json:"counts"`
	Results   []OSSByteMaintenanceResult `json:"results"`
}

type ossMaintenanceFile struct {
	ID                 int64  `json:"id"`
	PublicID           string `json:"public_id"`
	Bucket             string `json:"bucket"`
	Endpoint           string `json:"endpoint"`
	Region             string `json:"region"`
	Key                string `json:"object_key"`
	Category           string `json:"category"`
	Source             string `json:"source"`
	OriginalName       string `json:"original_name"`
	SourceOriginalName string `json:"source_original_name"`
	ContentType        string `json:"content_type"`
	Size               int64  `json:"size_bytes"`
	SourceSize         int64  `json:"source_size_bytes"`
	SHA                string `json:"sha256"`
	StoredSHA          string `json:"stored_sha256"`
	Owner              *int64 `json:"uploader_id"`
	Status             string `json:"status"`
	Verified           bool   `json:"source_hash_verified"`
	snapshot           string
}

type ossMaintenanceRun struct {
	s          *Server
	plan       OSSByteMaintenancePlan
	opts       OSSByteMaintenanceOptions
	read       int64
	hasJournal bool
}

var ossMaintenanceUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validateOSSByteMaintenancePlan(plan OSSByteMaintenancePlan, opts OSSByteMaintenanceOptions) error {
	if plan.Version != 1 || strings.TrimSpace(plan.Database) == "" || plan.MaxDurationSeconds < 1 || plan.MaxDurationSeconds > 1800 || plan.MaxFiles < 1 || plan.MaxFiles > 16 || len(plan.Entries) < 1 || len(plan.Entries) > plan.MaxFiles || plan.MaxReadBytes < 1 || plan.MaxReadBytes > 128<<30 || plan.MaxReferenceRows < 1 || plan.MaxReferenceRows > 1000000 || plan.MaxReferenceCols < 1 || plan.MaxReferenceCols > 2048 || plan.MaxReferenceBytes < 1 || plan.MaxReferenceBytes > 4<<20 {
		return errors.New("explicit bounded file, byte and reference budgets are required")
	}
	if opts.Apply && strings.TrimSpace(opts.BackupReference) == "" {
		return errors.New("apply requires the operator's existing backup reference")
	}
	seen := map[string]bool{}
	for _, e := range plan.Entries {
		if !ossMaintenanceUUIDPattern.MatchString(e.ActionID) || seen[strings.ToLower(e.ActionID)] {
			return errors.New("unique explicit maintenance action UUIDs are required")
		}
		seen[strings.ToLower(e.ActionID)] = true
		if (e.ExpectedSourceSHA != "" && normalizeSHA256(e.ExpectedSourceSHA) == "") || (e.ExpectedStoredSHA != "" && normalizeSHA256(e.ExpectedStoredSHA) == "") || e.ExpectedSourceSize < 0 || e.ExpectedStoredSize < 0 || e.ExpectedSourceSize > maxOSSUploadBytes || e.ExpectedStoredSize > maxOSSUploadBytes {
			return errors.New("invalid explicit byte pins")
		}
		if e.Operation == "promote" {
			if !validCatalogPublicID(e.FilePublicID) || e.BindingID != "" || e.RawSourceKind != "" {
				return errors.New("promotion requires one exact public file ID")
			}
			if opts.Apply && (strings.TrimSpace(e.ExpectedDisplayEndpoint) == "" || normalizeSHA256(e.ExpectedSourceSHA) == "" || normalizeSHA256(e.ExpectedStoredSHA) == "" || e.ExpectedSourceSize < 1 || e.ExpectedStoredSize < 1 || e.ExpectedSourceSize > maxOSSUploadBytes || e.ExpectedStoredSize > maxOSSUploadBytes) {
				return errors.New("promotion apply requires explicit source/stored hash and byte-size pins")
			}
		} else if e.Operation == "cleanup_raw" {
			if !ossMaintenanceUUIDPattern.MatchString(e.BindingID) || plan.RetentionCutoff.IsZero() || e.FilePublicID != "" || e.OriginalSourceKey != "" {
				return errors.New("raw cleanup requires one exact receipt UUID and an explicit cutoff")
			}
			if e.ExpectedDisplayEndpoint != "" || e.ExpectedSourceSHA != "" || e.ExpectedStoredSHA != "" || e.ExpectedSourceSize != 0 || e.ExpectedStoredSize != 0 {
				return errors.New("raw cleanup uses receipt proofs rather than unrelated promotion pins")
			}
			if e.RawSourceKind != "" && e.RawSourceKind != "upload" && e.RawSourceKind != "legacy_original" {
				return errors.New("unsupported raw receipt source kind")
			}
		} else {
			return errors.New("unsupported maintenance operation")
		}
	}
	return nil
}

// RunOSSByteMaintenance never changes schema. Default dry-run performs bounded
// reads; apply has a separate exact-ID, backup, byte-proof and durable intent path.
func RunOSSByteMaintenance(ctx context.Context, pool *pgxpool.Pool, cfg config.Config, plan OSSByteMaintenancePlan, opts OSSByteMaintenanceOptions) (OSSByteMaintenanceReport, error) {
	report := OSSByteMaintenanceReport{DryRun: !opts.Apply, Counts: map[string]int{}, Results: []OSSByteMaintenanceResult{}}
	if err := validateOSSByteMaintenancePlan(plan, opts); err != nil {
		return report, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(plan.MaxDurationSeconds)*time.Second)
	defer cancel()
	var name string
	if err := pool.QueryRow(ctx, `select current_database()`).Scan(&name); err != nil || name != plan.Database {
		return report, errors.New("database does not match the explicit maintenance target")
	}
	if opts.Apply {
		if err := database.RequireOSSUploadByteBindingSchema(ctx, pool); err != nil {
			return report, err
		}
		if err := database.RequireOSSByteMaintenanceSchema(ctx, pool); err != nil {
			return report, err
		}
	}
	run := ossMaintenanceRun{s: &Server{db: pool, cfg: cfg}, plan: plan, opts: opts}
	if err := pool.QueryRow(ctx, `select to_regclass('public.oss_upload_maintenance_actions') is not null`).Scan(&run.hasJournal); err != nil {
		return report, err
	}
	for _, e := range plan.Entries {
		entryCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		var result OSSByteMaintenanceResult
		if e.Operation == "promote" {
			result = run.promote(entryCtx, e)
		} else {
			result = run.cleanupRaw(entryCtx, e)
		}
		cancel()
		report.Results = append(report.Results, result)
		report.Counts[result.Status]++
	}
	report.ReadBytes = run.read
	return report, nil
}

func ossMaintenancePlanSHA(e OSSByteMaintenanceEntry, plan OSSByteMaintenancePlan) string {
	// Bind this action and every global budget/target, without coupling its
	// idempotence to unrelated entries in the same operator batch.
	e.ActionID = strings.ToLower(e.ActionID)
	e.BindingID = strings.ToLower(e.BindingID)
	if e.ExpectedSourceSHA != "" {
		e.ExpectedSourceSHA = normalizeSHA256(e.ExpectedSourceSHA)
	}
	if e.ExpectedStoredSHA != "" {
		e.ExpectedStoredSHA = normalizeSHA256(e.ExpectedStoredSHA)
	}
	plan.Entries = []OSSByteMaintenanceEntry{e}
	data, _ := json.Marshal(plan)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
