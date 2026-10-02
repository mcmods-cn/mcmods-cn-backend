package httpapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type publicIdentity struct {
	InternalID int64
	PublicID   string
	EntityType string
}

type revisionQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Server) resolvePublicIdentity(ctx context.Context, publicID string, allowedTypes ...string) (publicIdentity, error) {
	var identity publicIdentity
	publicID = strings.ToLower(strings.TrimSpace(publicID))
	if publicID == "" {
		return identity, pgx.ErrNoRows
	}
	err := s.db.QueryRow(
		ctx,
		`select internal_id,public_id,entity_type
		 from public_routes
		 where public_id=$1 and (cardinality($2::text[])=0 or entity_type=any($2::text[]))`,
		publicID,
		allowedTypes,
	).Scan(&identity.InternalID, &identity.PublicID, &identity.EntityType)
	return identity, err
}

func (s *Server) publicIDForInternal(ctx context.Context, entityType string, internalID int64) (string, error) {
	var publicID string
	err := s.db.QueryRow(
		ctx,
		`select public_id from public_routes where entity_type=$1 and internal_id=$2`,
		entityType,
		internalID,
	).Scan(&publicID)
	if err != nil {
		return "", fmt.Errorf("resolve %s public id: %w", entityType, err)
	}
	return publicID, nil
}

func resolveRevisionPublicID(ctx context.Context, query revisionQuery, publicID *string) (*int64, error) {
	if publicID == nil {
		return nil, nil
	}
	value := strings.ToLower(strings.TrimSpace(*publicID))
	if !validCatalogPublicID(value) {
		return nil, fmt.Errorf("invalid revision public id")
	}
	var internalID int64
	if err := query.QueryRow(ctx, `select id from content_revisions where public_id=$1`, value).Scan(&internalID); err != nil {
		return nil, err
	}
	return &internalID, nil
}

func revisionPublicIDForInternal(ctx context.Context, query revisionQuery, internalID *int64) (*string, error) {
	if internalID == nil {
		return nil, nil
	}
	var publicID string
	if err := query.QueryRow(ctx, `select public_id from content_revisions where id=$1`, *internalID).Scan(&publicID); err != nil {
		return nil, err
	}
	return &publicID, nil
}

func revisionPublicIDValue(ctx context.Context, query revisionQuery, internalID *int64) (*string, error) {
	return revisionPublicIDForInternal(ctx, query, internalID)
}

func ossFilePublicIDForInternal(ctx context.Context, query revisionQuery, internalID *int64) (*string, error) {
	if internalID == nil {
		return nil, nil
	}
	var publicID string
	if err := query.QueryRow(ctx, `select public_id from oss_files where id=$1`, *internalID).Scan(&publicID); err != nil {
		return nil, err
	}
	return &publicID, nil
}
