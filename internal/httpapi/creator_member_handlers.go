package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

const maximumCreatorTeamMembers = 500

type creatorTeamMemberAuditFact struct {
	CreatorID    string `json:"creatorId"`
	RoleID       string `json:"roleId"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	DisplayOrder int    `json:"displayOrder"`
}

func (s *Server) updateCreatorMembers(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	if !claimsAllow(claims, "admin.*") && !claimsAllow(claims, "team.members.manage") {
		writeError(w, http.StatusForbidden, "team member management permission is required")
		return
	}
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "invalid team creator ID")
		return
	}
	var request struct {
		Members []creatorMemberPayload `json:"members"`
	}
	if err := decodeJSON(r, &request); err != nil || request.Members == nil {
		writeError(w, http.StatusBadRequest, "invalid team members payload")
		return
	}
	members, err := normalizeCreatorTeamMembers(request.Members)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update team members")
		return
	}
	defer tx.Rollback(r.Context())
	var creatorID int64
	var kind string
	err = tx.QueryRow(r.Context(), `select id,kind from creators where public_id=$1 for update`, publicID).Scan(&creatorID, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "creator not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creator")
		return
	}
	if kind != "team" {
		writeError(w, http.StatusConflict, "only teams have managed members")
		return
	}
	before, err := creatorTeamMemberAuditSnapshotTx(r.Context(), tx, creatorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit current team members")
		return
	}
	if err = replaceCreatorTeamMembersTx(r.Context(), tx, creatorID, members, "approved", true, claims.Subject); err != nil {
		writeError(w, http.StatusBadRequest, "failed to replace team members")
		return
	}
	after, err := creatorTeamMemberAuditSnapshotTx(r.Context(), tx, creatorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit updated team members")
		return
	}
	payload, _ := json.Marshal(map[string]any{"creatorId": publicID, "before": before, "after": after})
	if _, err = tx.Exec(r.Context(), `insert into permission_audit_logs(operator_id,action,payload)
		values($1,'creator.team_members.replace',$2::jsonb)`, claims.Subject, payload); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record team member audit")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit team members")
		return
	}
	if !s.requireSecurityVersionRefresh(w, r, "replace_creator_team_members", 0,
		s.refreshProjectACLVersion(r.Context())) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "updated", "count": len(members)})
}

func normalizeCreatorTeamMembers(input []creatorMemberPayload) ([]creatorMemberPayload, error) {
	if len(input) > maximumCreatorTeamMembers {
		return nil, errors.New("team member count exceeds the configured limit")
	}
	result := make([]creatorMemberPayload, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, member := range input {
		member.CreatorID = strings.ToLower(strings.TrimSpace(member.CreatorID))
		member.RoleID = strings.ToLower(strings.TrimSpace(member.RoleID))
		member.Title = strings.TrimSpace(member.Title)
		if !validCatalogPublicID(member.CreatorID) || !validCatalogPublicID(member.RoleID) || len([]byte(member.Title)) > 160 {
			return nil, errors.New("team member identity, role, or title is invalid")
		}
		key := member.CreatorID + "\x00" + member.RoleID
		if _, duplicate := seen[key]; duplicate {
			return nil, errors.New("duplicate team author membership")
		}
		seen[key] = struct{}{}
		result = append(result, member)
	}
	return result, nil
}

func creatorTeamMemberAuditSnapshotTx(ctx context.Context, tx pgx.Tx, creatorID int64) ([]creatorTeamMemberAuditFact, error) {
	rows, err := tx.Query(ctx, `select member.public_id,role.public_id,relation.title,relation.status,relation.display_order
		from creator_team_members relation
		join creators member on member.id=relation.member_creator_id
		join creator_role_definitions role on role.id=relation.role_id
		where relation.team_id=$1
		order by relation.display_order,relation.member_creator_id,relation.role_id`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]creatorTeamMemberAuditFact, 0)
	for rows.Next() {
		var item creatorTeamMemberAuditFact
		if err = rows.Scan(&item.CreatorID, &item.RoleID, &item.Title, &item.Status, &item.DisplayOrder); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
