package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

type publicIdentity struct {
	InternalID int64
	PublicID   string
}

type revisionQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Server) pathUserIdentity(w http.ResponseWriter, r *http.Request) (publicIdentity, bool) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "用户 ID 不正确")
		return publicIdentity{}, false
	}
	var identity publicIdentity
	err := s.db.QueryRow(r.Context(), `select internal_id, public_id from public_routes
		where entity_type='user' and public_id=$1`, publicID).Scan(&identity.InternalID, &identity.PublicID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "用户 ID 不正确")
		return publicIdentity{}, false
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "暂时无法读取用户，请稍后重试")
		return publicIdentity{}, false
	}
	return identity, true
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

func revisionPublicIDValue(ctx context.Context, query revisionQuery, internalID *int64) (*string, error) {
	if internalID == nil {
		return nil, nil
	}
	var publicID string
	if err := query.QueryRow(ctx, `select public_id from content_revisions where id=$1`, *internalID).Scan(&publicID); err != nil {
		return nil, err
	}
	return &publicID, nil
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
