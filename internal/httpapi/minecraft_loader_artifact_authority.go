package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	errMinecraftLoaderArtifactUnavailable          = errors.New("no synchronized Minecraft loader artifact is available")
	errMinecraftLoaderArtifactAuthorityUnavailable = errors.New("Minecraft loader artifact authority is unavailable")
	errMinecraftVersionNotAuthoritative            = errors.New("Minecraft version is not enabled for the selected loader")
)

type minecraftLoaderArtifactSnapshot struct {
	MinecraftVersion string
	Loader           string
	LoaderVersion    string
	SourceURL        string
	ObservedAt       time.Time
}

type minecraftVersionCatalogIdentity struct {
	Loaders []minecraftLoaderCatalogEntry `json:"loaders"`
}

type minecraftLoaderCatalogEntry struct {
	Code     string   `json:"code"`
	Versions []string `json:"versions"`
}

func minecraftVersionCatalogHash(config minecraftVersionConfig) (string, error) {
	normalized, err := normalizeMinecraftVersionConfig(config)
	if err != nil {
		return "", err
	}
	identity := minecraftVersionCatalogIdentity{Loaders: make([]minecraftLoaderCatalogEntry, 0, 3)}
	for _, loader := range normalized.Loaders {
		loaderCode := strings.ToLower(loader.Code)
		if !isMRPackLoader(loaderCode) {
			continue
		}
		versions := append([]string(nil), loader.Versions...)
		sort.Strings(versions)
		identity.Loaders = append(identity.Loaders, minecraftLoaderCatalogEntry{
			Code: loaderCode, Versions: versions,
		})
	}
	sort.Slice(identity.Loaders, func(i, j int) bool { return identity.Loaders[i].Code < identity.Loaders[j].Code })
	raw, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return fmt.Sprintf("%x", digest), nil
}

func normalizedMinecraftVersionConfigJSON(config minecraftVersionConfig) (minecraftVersionConfig, string, string, error) {
	normalized, err := normalizeMinecraftVersionConfig(config)
	if err != nil {
		return minecraftVersionConfig{}, "", "", err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return minecraftVersionConfig{}, "", "", err
	}
	hash, err := minecraftVersionCatalogHash(normalized)
	if err != nil {
		return minecraftVersionConfig{}, "", "", err
	}
	return normalized, string(raw), hash, nil
}

func saveMinecraftVersionConfigAndInvalidateStaleArtifacts(ctx context.Context, db *pgxpool.Pool, config minecraftVersionConfig) error {
	_, raw, catalogHash, err := normalizedMinecraftVersionConfigJSON(config)
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = upsertMinecraftVersionConfig(ctx, tx, raw); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `delete from minecraft_loader_artifact_versions where catalog_hash<>$1`, catalogHash); err != nil {
		return fmt.Errorf("invalidate stale Minecraft loader artifacts: %w", err)
	}
	return tx.Commit(ctx)
}

func saveSynchronizedMinecraftVersionConfig(ctx context.Context, db *pgxpool.Pool, config minecraftVersionConfig, artifacts []minecraftLoaderArtifactSnapshot) error {
	normalized, raw, catalogHash, err := normalizedMinecraftVersionConfigJSON(config)
	if err != nil {
		return err
	}
	validated, err := validateMinecraftLoaderArtifactSnapshots(normalized, artifacts)
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = upsertMinecraftVersionConfig(ctx, tx, raw); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `delete from minecraft_loader_artifact_versions`); err != nil {
		return fmt.Errorf("replace Minecraft loader artifact snapshot: %w", err)
	}
	rows := make([][]any, 0, len(validated))
	for _, artifact := range validated {
		rows = append(rows, []any{catalogHash, artifact.MinecraftVersion, artifact.Loader,
			artifact.LoaderVersion, artifact.SourceURL, artifact.ObservedAt})
	}
	if len(rows) > 0 {
		if _, err = tx.CopyFrom(ctx, pgx.Identifier{"minecraft_loader_artifact_versions"},
			[]string{"catalog_hash", "minecraft_version", "loader_type", "loader_version", "source_url", "observed_at"},
			pgx.CopyFromRows(rows)); err != nil {
			return fmt.Errorf("save Minecraft loader artifact snapshot: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func upsertMinecraftVersionConfig(ctx context.Context, tx pgx.Tx, raw string) error {
	_, err := tx.Exec(ctx, `insert into system_settings (key,value,updated_at) values ($1,$2::jsonb,now())
		on conflict (key) do update set value=excluded.value,updated_at=now()`, minecraftVersionsSettingKey, raw)
	if err != nil {
		return fmt.Errorf("save Minecraft version configuration: %w", err)
	}
	return nil
}

func validateMinecraftLoaderArtifactSnapshots(config minecraftVersionConfig, artifacts []minecraftLoaderArtifactSnapshot) ([]minecraftLoaderArtifactSnapshot, error) {
	required := make(map[string]bool)
	for _, loader := range config.Loaders {
		loaderCode := strings.ToLower(strings.TrimSpace(loader.Code))
		if !isMRPackLoader(loaderCode) {
			continue
		}
		for _, version := range loader.Versions {
			required[loaderCode+"\x00"+version] = false
		}
	}
	validated := make([]minecraftLoaderArtifactSnapshot, 0, len(artifacts))
	for _, artifact := range artifacts {
		artifact.MinecraftVersion = strings.TrimSpace(artifact.MinecraftVersion)
		artifact.Loader = strings.ToLower(strings.TrimSpace(artifact.Loader))
		artifact.LoaderVersion = strings.TrimSpace(artifact.LoaderVersion)
		artifact.SourceURL = strings.TrimSpace(artifact.SourceURL)
		key := artifact.Loader + "\x00" + artifact.MinecraftVersion
		seen, exists := required[key]
		if !exists {
			return nil, fmt.Errorf("loader artifact %s/%s is outside the synchronized catalog", artifact.Loader, artifact.MinecraftVersion)
		}
		if seen {
			return nil, fmt.Errorf("duplicate loader artifact %s/%s", artifact.Loader, artifact.MinecraftVersion)
		}
		if artifact.LoaderVersion == "" || len(artifact.LoaderVersion) > 160 || artifact.SourceURL == "" || len(artifact.SourceURL) > 2000 || artifact.ObservedAt.IsZero() {
			return nil, fmt.Errorf("loader artifact %s/%s has incomplete provenance", artifact.Loader, artifact.MinecraftVersion)
		}
		required[key] = true
		validated = append(validated, artifact)
	}
	for key, present := range required {
		if !present {
			parts := strings.SplitN(key, "\x00", 2)
			return nil, fmt.Errorf("synchronized catalog has no loader artifact for %s/%s", parts[0], parts[1])
		}
	}
	return validated, nil
}

func isMRPackLoader(loader string) bool {
	switch strings.ToLower(strings.TrimSpace(loader)) {
	case "neoforge", "fabric", "forge":
		return true
	default:
		return false
	}
}

func resolveSynchronizedMRPackLoaderVersion(ctx context.Context, db *pgxpool.Pool, minecraftVersion, loader string) (string, error) {
	minecraftVersion = strings.TrimSpace(minecraftVersion)
	loader = strings.ToLower(strings.TrimSpace(loader))
	if minecraftVersion == "" || !isMRPackLoader(loader) {
		return "", errMinecraftLoaderArtifactUnavailable
	}
	config, err := loadMinecraftVersionConfig(ctx, db)
	if err != nil {
		return "", err
	}
	if !minecraftVersionEnabledForLoader(config, minecraftVersion, loader) {
		return "", fmt.Errorf("%w: %w for %s/%s", errMinecraftVersionNotAuthoritative,
			errMinecraftLoaderArtifactUnavailable, loader, minecraftVersion)
	}
	catalogHash, err := minecraftVersionCatalogHash(config)
	if err != nil {
		return "", fmt.Errorf("%w: hash current catalog: %v", errMinecraftLoaderArtifactAuthorityUnavailable, err)
	}
	var loaderVersion string
	err = db.QueryRow(ctx, `select loader_version from minecraft_loader_artifact_versions
		where catalog_hash=$1 and minecraft_version=$2 and loader_type=$3`, catalogHash, minecraftVersion, loader).Scan(&loaderVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("%w for %s/%s", errMinecraftLoaderArtifactUnavailable, loader, minecraftVersion)
	}
	if err != nil {
		return "", fmt.Errorf("%w: read %s/%s: %v", errMinecraftLoaderArtifactAuthorityUnavailable, loader, minecraftVersion, err)
	}
	if loaderVersion = strings.TrimSpace(loaderVersion); loaderVersion == "" {
		return "", fmt.Errorf("%w: empty version for %s/%s", errMinecraftLoaderArtifactAuthorityUnavailable, loader, minecraftVersion)
	}
	return loaderVersion, nil
}

func minecraftVersionEnabledForLoader(config minecraftVersionConfig, minecraftVersion, loader string) bool {
	if !validMinecraftVersionCode(minecraftVersion) {
		return false
	}
	registered := false
	for _, version := range config.Versions {
		if version.Code == minecraftVersion {
			registered = true
			break
		}
	}
	if !registered {
		return false
	}
	for _, option := range config.Loaders {
		if !strings.EqualFold(option.Code, loader) {
			continue
		}
		for _, enabledVersion := range option.Versions {
			if enabledVersion == minecraftVersion {
				return true
			}
		}
		return false
	}
	return false
}
