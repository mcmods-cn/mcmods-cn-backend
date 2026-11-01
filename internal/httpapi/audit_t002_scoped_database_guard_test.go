package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These helpers belong only to tests in this package. Each caller retains its
// own name prefix, pool configuration, migration and cleanup registration.
type auditT002DatabaseScope struct{ Prefix string }

type auditT002DatabaseIdentity struct {
	Name   string
	OID    uint32
	Owner  string
	Marker string
}

type auditT002DatabaseAdmin interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

var auditT002DatabasePrefixPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}_$`)
var auditT002DatabaseSuffixPattern = regexp.MustCompile(`^[0-9]+$`)
var auditT002DatabaseNoncePattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var errAuditT002DatabaseOwnership = errors.New("refuse test database cleanup: original ownership is unknown or changed")

func (scope auditT002DatabaseScope) markerPrefix() string {
	return "mcmods-testdb-owned:" + scope.Prefix + ":"
}

func (scope auditT002DatabaseScope) validIdentity(identity auditT002DatabaseIdentity) bool {
	if !auditT002DatabasePrefixPattern.MatchString(scope.Prefix) || identity.OID == 0 || identity.Owner == "" {
		return false
	}
	if len(identity.Name) > 63 || !strings.HasPrefix(identity.Name, scope.Prefix) || !auditT002DatabaseSuffixPattern.MatchString(strings.TrimPrefix(identity.Name, scope.Prefix)) {
		return false
	}
	return strings.HasPrefix(identity.Marker, scope.markerPrefix()) && auditT002DatabaseNoncePattern.MatchString(strings.TrimPrefix(identity.Marker, scope.markerPrefix()))
}

func readAuditT002DatabaseIdentity(ctx context.Context, admin auditT002DatabaseAdmin, name string) (auditT002DatabaseIdentity, bool, error) {
	identity := auditT002DatabaseIdentity{Name: name}
	err := admin.QueryRow(ctx, `select oid,pg_catalog.pg_get_userbyid(datdba),coalesce(pg_catalog.shobj_description(oid,'pg_database'),'') from pg_catalog.pg_database where datname=$1`, name).Scan(&identity.OID, &identity.Owner, &identity.Marker)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity, false, nil
	}
	return identity, err == nil, err
}

func createAuditT002OwnedDatabase(ctx context.Context, admin auditT002DatabaseAdmin, scope auditT002DatabaseScope) (auditT002DatabaseIdentity, error) {
	identity := auditT002DatabaseIdentity{}
	if !auditT002DatabasePrefixPattern.MatchString(scope.Prefix) {
		return identity, errAuditT002DatabaseOwnership
	}
	identity.Name = fmt.Sprintf("%s%d", scope.Prefix, time.Now().UnixNano())
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return identity, err
	}
	identity.Marker = scope.markerPrefix() + hex.EncodeToString(nonce[:])
	if err := admin.QueryRow(ctx, `select current_user::pg_catalog.text`).Scan(&identity.Owner); err != nil {
		return identity, err
	}
	if _, err := admin.Exec(ctx, "create database "+pgx.Identifier{identity.Name}.Sanitize()); err != nil {
		return identity, err
	}
	actual, exists, err := readAuditT002DatabaseIdentity(ctx, admin, identity.Name)
	if err != nil || !exists || actual.Owner != identity.Owner || actual.OID == 0 || actual.Marker != "" {
		// No later cleanup is authorized by a name or a partial CREATE receipt.
		return identity, fmt.Errorf("created test database but original identity was not recorded: %w", errors.Join(errAuditT002DatabaseOwnership, err))
	}
	identity.OID = actual.OID
	// The prefix and hex nonce above are validated, internally generated values.
	if _, err = admin.Exec(ctx, "comment on database "+pgx.Identifier{identity.Name}.Sanitize()+" is '"+identity.Marker+"'"); err != nil {
		return identity, fmt.Errorf("created test database but its ownership nonce was not persisted: %w", err)
	}
	actual, exists, err = readAuditT002DatabaseIdentity(ctx, admin, identity.Name)
	if err != nil || !exists || actual != identity || !scope.validIdentity(identity) {
		return identity, fmt.Errorf("created test database but complete ownership could not be confirmed: %w", errors.Join(errAuditT002DatabaseOwnership, err))
	}
	return identity, nil
}

func dropAuditT002OwnedDatabase(ctx context.Context, admin auditT002DatabaseAdmin, scope auditT002DatabaseScope, expected auditT002DatabaseIdentity) error {
	if !scope.validIdentity(expected) {
		return errAuditT002DatabaseOwnership
	}
	actual, exists, err := readAuditT002DatabaseIdentity(ctx, admin, expected.Name)
	if err != nil {
		return fmt.Errorf("%w: %w", errAuditT002DatabaseOwnership, err)
	}
	if !exists || actual != expected {
		return errAuditT002DatabaseOwnership
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// A live connection is an explicit failure; never terminate it for cleanup.
	if _, err = admin.Exec(ctx, "drop database "+pgx.Identifier{expected.Name}.Sanitize()); err != nil {
		return err
	}
	_, exists, err = readAuditT002DatabaseIdentity(ctx, admin, expected.Name)
	if err != nil || exists {
		return fmt.Errorf("ordinary test database removal was not confirmed: %w", errors.Join(errAuditT002DatabaseOwnership, err))
	}
	return nil
}

func closeAuditT002OwnedPool(ctx context.Context, pool interface{ Close() }) error {
	closed := make(chan struct{})
	go func() { pool.Close(); close(closed) }()
	select {
	case <-closed:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("owned test pool did not close: %w", ctx.Err())
	}
}

func cleanupAuditT002OwnedDatabase(ctx context.Context, admin auditT002DatabaseAdmin, scope auditT002DatabaseScope, expected auditT002DatabaseIdentity, closeOwn func(context.Context) error) error {
	if closeOwn != nil {
		if err := closeOwn(ctx); err != nil {
			return err
		}
	}
	return dropAuditT002OwnedDatabase(ctx, admin, scope, expected)
}
