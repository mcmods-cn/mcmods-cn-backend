// Command repair-creator-avatar-drafts defaults to a strictly scoped read-only
// plan. Private state contains old access capabilities and never goes to stdout.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/httpapi"
)

type publicIDs []string

func (ids *publicIDs) String() string { return "explicit draft public IDs" }
func (ids *publicIDs) Set(value string) error {
	*ids = append(*ids, value)
	return nil
}

func privateFile(path string) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("private state/backup paths must be absolute")
	}
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 || parent.Mode().Perm()&0077 != 0 {
		return nil, errors.New("private parent directory must be owner-only and nonsymlink")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.New("cannot open private nonsymlink input")
	}
	info, err := file.Stat()
	stat, ok := infoSys(info).(*syscall.Stat_t)
	if err != nil || info == nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ok || stat.Uid != uint32(os.Geteuid()) {
		file.Close()
		return nil, errors.New("private input must be current-owner regular0600 file")
	}
	return file, nil
}

func infoSys(info os.FileInfo) any {
	if info == nil {
		return nil
	}
	return info.Sys()
}

func writePrivate(path string, value any) error {
	if !filepath.IsAbs(path) {
		return errors.New("private output path must be absolute")
	}
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 || parent.Mode().Perm()&0077 != 0 {
		return errors.New("private output directory must be owner-only")
	}
	stat, ok := parent.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("private output directory owner mismatch")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return errors.New("private output must be a new exclusive file")
	}
	defer file.Close()
	if err = json.NewEncoder(file).Encode(value); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func readPrivate(path string, target any) error {
	file, err := privateFile(path)
	if err != nil {
		return err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.Size() > 2<<20 {
		return errors.New("private plan exceeds2MiB budget")
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return errors.New("private plan format invalid")
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("private plan must contain one JSON value")
	}
	return nil
}

func verifiedBackup(path, expectedSHA, declaredTarget, actualTarget string) (httpapi.CreatorAvatarDraftRepairBackup, error) {
	backup := httpapi.CreatorAvatarDraftRepairBackup{TargetSHA256: declaredTarget, FileSHA256: expectedSHA}
	if declaredTarget != actualTarget || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(expectedSHA) {
		return backup, errors.New("explicit verified backup declaration must match exact reviewed target")
	}
	file, err := privateFile(path)
	if err != nil {
		return backup, err
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, file)
	if err != nil || size == 0 || fmt.Sprintf("%x", digest.Sum(nil)) != expectedSHA {
		return backup, errors.New("actual backup bytes do not match declared checksum")
	}
	return backup, nil
}

func run() error {
	schema := flag.String("schema", "public", "exact reviewed schema")
	owner := flag.String("owner-public-id", "", "required exact draft owner public identity in dry-run")
	var ids publicIDs
	flag.Var(&ids, "draft-id", "required exact draft public ID; repeat at most100 times, no scans")
	apply := flag.Bool("apply", false, "explicitly apply a reviewed private plan; default is read-only dry-run")
	rollback := flag.Bool("rollback", false, "explicitly reverse one private apply recovery using CAS")
	input := flag.String("private-input", "", "owner-only0600 plan/recovery input for explicit writes")
	output := flag.String("private-output", "", "new owner-only0600 plan/recovery path under private0700 directory")
	target := flag.String("expect-target-fingerprint", "", "explicit exact database/schema/provider fingerprint from reviewed dry-run")
	backupPath := flag.String("verified-backup-file", "", "owner-only0600 actual backup whose restore/scope the operator has verified")
	backupSHA := flag.String("verified-backup-sha256", "", "actual backup checksum")
	backupTarget := flag.String("verified-backup-target", "", "operator declaration: backup belongs to this exact reviewed target fingerprint")
	flag.Parse()
	if *apply && *rollback || *output == "" || flag.NArg() != 0 {
		return errors.New("select default dry-run or one explicit write mode and a new private output")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := config.Load()
	pool, err := pgxpool.New(ctx, cfg.DB.ConnString())
	if err != nil {
		return errors.New("database configuration unavailable")
	}
	defer pool.Close()
	tool := httpapi.NewCreatorAvatarDraftRepairTool(cfg, pool)
	var summary httpapi.CreatorAvatarDraftRepairSummary
	if !*apply && !*rollback {
		if *input != "" || *target != "" || *backupPath != "" || *backupSHA != "" || *backupTarget != "" {
			return errors.New("dry-run accepts only explicit scope/private output, no write inputs")
		}
		plan, result, err := tool.Plan(ctx, httpapi.CreatorAvatarDraftRepairScope{Schema: *schema, OwnerPublicID: *owner, DraftIDs: ids})
		if err != nil {
			return err
		}
		if err = writePrivate(*output, plan); err != nil {
			return err
		}
		summary = result
	} else {
		if *owner != "" || len(ids) > 0 {
			return errors.New("write scope comes only from the reviewed private input")
		}
		backup, err := verifiedBackup(*backupPath, *backupSHA, *backupTarget, *target)
		if err != nil {
			return err
		}
		persist := func(value httpapi.CreatorAvatarDraftRepairRecovery) error { return writePrivate(*output, value) }
		if *apply {
			var plan httpapi.CreatorAvatarDraftRepairPlan
			if err = readPrivate(*input, &plan); err != nil {
				return err
			}
			summary, err = tool.Apply(ctx, plan, *target, backup, persist)
		} else {
			var recovery httpapi.CreatorAvatarDraftRepairRecovery
			if err = readPrivate(*input, &recovery); err != nil {
				return err
			}
			summary, err = tool.Rollback(ctx, recovery, *target, backup, persist)
		}
		if err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(summary)
}

func main() {
	if err := run(); err != nil {
		// Never print a driver/URL/JSON error that could contain a capability or
		// connection credential. The hash correlates local failures safely.
		fmt.Fprintf(os.Stderr, "creator avatar draft repair failed; no success conclusion; errorType=%T errorSha256=%x\n", err, sha256.Sum256([]byte(err.Error())))
		os.Exit(1)
	}
}
