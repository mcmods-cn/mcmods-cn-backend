package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

type resolvedProjectCreatorBinding struct {
	CreatorID          int64
	RoleID             *int64
	CreatorKind        string
	NameSnapshot       string
	RoleSnapshot       string
	PermissionGranting bool
	Status             string
	DisplayOrder       int
}

type projectAuthorshipReviewRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

func projectRelationshipPermissions(claimsPermission func(string) bool) (bool, bool) {
	return claimsPermission("project.authorship.manage"), claimsPermission("project.team_relation.manage")
}

func projectRelationshipPermissionsForClaims(claims security.Claims) (bool, bool) {
	return projectRelationshipPermissions(func(permission string) bool {
		return claimsAllow(claims, permission) || claimsAllow(claims, "admin.*")
	})
}

// syncProjectCreatorBindingsTx keeps approved relationships stable when an
// editor without authorship authority publishes an unrelated content change.
// New relationships stay pending until a dedicated relationship reviewer
// approves them; removing an approved relationship also requires the matching
// sensitive permission.
func syncProjectCreatorBindingsTx(
	ctx context.Context,
	tx pgx.Tx,
	subjectType string,
	subjectID int64,
	authors []modAuthorPayload,
	actorID int64,
	projectApproved bool,
	canManageAuthors bool,
	canManageTeams bool,
	r *http.Request,
) error {
	existing := map[string]resolvedProjectCreatorBinding{}
	rows, err := tx.Query(ctx, `select binding.creator_id,binding.role_id,creator.kind,binding.name_snapshot,
		binding.role_snapshot,binding.permission_granting,binding.status,binding.display_order
		from content_creator_bindings binding join creators creator on creator.id=binding.creator_id
		where binding.subject_type=$1 and binding.subject_id=$2`, subjectType, subjectID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item resolvedProjectCreatorBinding
		if err = rows.Scan(&item.CreatorID, &item.RoleID, &item.CreatorKind, &item.NameSnapshot,
			&item.RoleSnapshot, &item.PermissionGranting, &item.Status, &item.DisplayOrder); err != nil {
			rows.Close()
			return err
		}
		existing[projectCreatorBindingKey(item.CreatorID, item.RoleID)] = item
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	next := make([]resolvedProjectCreatorBinding, 0, len(authors)+len(existing))
	requested := make(map[string]struct{}, len(authors))
	for index, author := range authors {
		creatorID, name, role, resolveErr := resolveProjectAuthorForCreateTx(ctx, tx, author, actorID, r)
		if resolveErr != nil {
			return resolveErr
		}
		roleID, resolveErr := creatorRoleInternalIDTx(ctx, tx, author.RoleID)
		if resolveErr != nil {
			return resolveErr
		}
		var creatorKind string
		if resolveErr = tx.QueryRow(ctx, `select kind from creators where id=$1`, creatorID).Scan(&creatorKind); resolveErr != nil {
			return resolveErr
		}
		permissionGranting, resolveErr := creatorRolePermissionGrantingTx(ctx, tx, roleID)
		if resolveErr != nil {
			return resolveErr
		}
		key := projectCreatorBindingKey(creatorID, roleID)
		if _, duplicate := requested[key]; duplicate {
			return errors.New("duplicate project author or team relationship")
		}
		requested[key] = struct{}{}
		status := "pending"
		if previous, found := existing[key]; found && previous.Status == "approved" {
			status = "approved"
		} else if projectApproved && relationshipChangeAllowed(creatorKind, canManageAuthors, canManageTeams) {
			status = "approved"
		}
		next = append(next, resolvedProjectCreatorBinding{
			CreatorID: creatorID, RoleID: roleID, CreatorKind: creatorKind, NameSnapshot: name,
			RoleSnapshot: role, PermissionGranting: permissionGranting, Status: status, DisplayOrder: index,
		})
	}

	for key, previous := range existing {
		if previous.Status != "approved" {
			continue
		}
		if _, retained := requested[key]; retained {
			continue
		}
		if relationshipChangeAllowed(previous.CreatorKind, canManageAuthors, canManageTeams) {
			continue
		}
		previous.DisplayOrder = len(next)
		next = append(next, previous)
	}

	if _, err = tx.Exec(ctx, `delete from content_creator_bindings where subject_type=$1 and subject_id=$2`, subjectType, subjectID); err != nil {
		return err
	}
	for _, item := range next {
		if _, err = tx.Exec(ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,role_id,
			name_snapshot,role_snapshot,status,permission_granting,approved_by,approved_at,display_order)
			values($1,$2,$3,$4,$5,$6,$7,$8,case when $7='approved' then $9 else null end,
			case when $7='approved' then now() else null end,$10)`, subjectType, subjectID, item.CreatorID, item.RoleID,
			item.NameSnapshot, item.RoleSnapshot, item.Status, item.PermissionGranting, actorID, item.DisplayOrder); err != nil {
			return err
		}
	}
	return nil
}

func relationshipChangeAllowed(kind string, canManageAuthors, canManageTeams bool) bool {
	if kind == "team" {
		return canManageTeams
	}
	return canManageAuthors
}

func projectCreatorBindingKey(creatorID int64, roleID *int64) string {
	role := int64(0)
	if roleID != nil {
		role = *roleID
	}
	return strconv.FormatInt(creatorID, 10) + ":" + strconv.FormatInt(role, 10)
}

func (s *Server) adminProjectAuthorshipRelations(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	canManageAuthors, canManageTeams := projectRelationshipPermissionsForClaims(claims)
	if !canManageAuthors && !canManageTeams {
		writeError(w, http.StatusForbidden, "project relationship review permission is required")
		return
	}
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "approved" && status != "rejected" && status != "revoked" {
		writeError(w, http.StatusBadRequest, "relationship status is invalid")
		return
	}
	rows, err := s.db.Query(r.Context(), `select binding.public_id,binding.subject_type,route.public_id,
		creator.public_id,creator.kind,creator.name,coalesce(role.name,binding.role_snapshot),binding.permission_granting,
		binding.status,binding.created_at
		from content_creator_bindings binding
		join public_routes route on route.entity_type=binding.subject_type and route.internal_id=binding.subject_id
		join creators creator on creator.id=binding.creator_id
		left join creator_role_definitions role on role.id=binding.role_id
		where binding.status=$1
		  and ((creator.kind='author' and $2) or (creator.kind='team' and $3))
		order by binding.created_at,binding.id limit 200`, status, canManageAuthors, canManageTeams)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project authorship relationships")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, projectType, projectID, creatorID, creatorKind, creatorName, roleName, relationStatus string
		var permissionGranting bool
		var createdAt time.Time
		if err = rows.Scan(&id, &projectType, &projectID, &creatorID, &creatorKind, &creatorName, &roleName,
			&permissionGranting, &relationStatus, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to parse project authorship relationships")
			return
		}
		items = append(items, map[string]any{"id": id, "projectType": projectType, "projectId": projectID,
			"creatorId": creatorID, "creatorKind": creatorKind, "creatorName": creatorName, "roleName": roleName,
			"permissionGranting": permissionGranting, "status": relationStatus, "createdAt": createdAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) reviewProjectAuthorshipRelation(w http.ResponseWriter, r *http.Request) {
	var request projectAuthorshipReviewRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "request body is invalid")
		return
	}
	request.Status = strings.ToLower(strings.TrimSpace(request.Status))
	request.Note = strings.TrimSpace(request.Note)
	if request.Status != "approved" && request.Status != "rejected" && request.Status != "revoked" {
		writeError(w, http.StatusBadRequest, "review status is invalid")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start relationship review")
		return
	}
	defer tx.Rollback(r.Context())
	var bindingID int64
	var subjectType, projectID, creatorKind string
	var subjectID int64
	err = tx.QueryRow(r.Context(), `select binding.id,binding.subject_type,binding.subject_id,route.public_id,creator.kind
		from content_creator_bindings binding
		join public_routes route on route.entity_type=binding.subject_type and route.internal_id=binding.subject_id
		join creators creator on creator.id=binding.creator_id
		where binding.public_id=$1 for update of binding`, r.PathValue("id")).
		Scan(&bindingID, &subjectType, &subjectID, &projectID, &creatorKind)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project authorship relationship does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project authorship relationship")
		return
	}
	claims := currentClaims(r)
	required := "project.authorship.manage"
	if creatorKind == "team" {
		required = "project.team_relation.manage"
	}
	if !claimsAllow(claims, required) && !claimsAllow(claims, "admin.*") {
		writeError(w, http.StatusForbidden, "project relationship review permission is required")
		return
	}
	if request.Status == "approved" {
		approved, approveErr := projectTargetPublishedTx(r.Context(), tx, subjectType, subjectID)
		if approveErr != nil || !approved {
			writeError(w, http.StatusConflict, "project must be public before this relationship can grant access")
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `update content_creator_bindings set status=$2,approved_by=case when $2='approved' then $3 else null end,
		approved_at=case when $2='approved' then now() else null end where id=$1`, bindingID, request.Status, claims.Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to review project authorship relationship")
		return
	}
	payload, _ := json.Marshal(map[string]any{"relationshipId": r.PathValue("id"), "projectType": subjectType,
		"projectId": projectID, "status": request.Status, "note": request.Note})
	if _, err = tx.Exec(r.Context(), `insert into permission_audit_logs(operator_id,action,payload)
		values($1,'project.authorship.review',$2::jsonb)`, claims.Subject, payload); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record project relationship review")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project relationship review")
		return
	}
	_ = s.refreshProjectACLVersion(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "status": request.Status})
}

func projectTargetPublishedTx(ctx context.Context, tx pgx.Tx, projectType string, projectID int64) (bool, error) {
	var query string
	switch projectType {
	case "mod":
		query = `select review_status='approved' from mods where id=$1`
	case "modpack":
		query = `select review_status='approved' from modpacks where id=$1`
	case "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		query = `select review_status='approved' from simple_projects where id=$1 and project_type=$2`
	default:
		return false, nil
	}
	var approved bool
	var err error
	if strings.Contains(query, "$2") {
		err = tx.QueryRow(ctx, query, projectID, projectType).Scan(&approved)
	} else {
		err = tx.QueryRow(ctx, query, projectID).Scan(&approved)
	}
	return approved, err
}
