package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

type resolvedProjectCreatorBinding struct {
	ID                 int64
	CreatorID          int64
	RoleID             *int64
	CreatorKind        string
	NameSnapshot       string
	RoleSnapshot       string
	PermissionGranting bool
	Status             string
	DisplayOrder       int
}

type requestedProjectCreatorBinding struct {
	CreatorID          int64
	RoleID             *int64
	CreatorKind        string
	NameSnapshot       string
	RoleSnapshot       string
	PermissionGranting bool
}

type projectCreatorBindingMutation struct {
	CreatorID          int64
	RoleID             int64
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

const projectCreatorBindingsForUpdateSQL = `select binding.id,binding.creator_id,binding.role_id,creator.kind,binding.name_snapshot,
	binding.role_snapshot,binding.permission_granting,binding.status,binding.display_order
	from content_creator_bindings binding join creators creator on creator.id=binding.creator_id
	where binding.subject_type=$1 and binding.subject_id=$2
	order by binding.id for update of binding`

func projectRelationshipPermissions(claimsPermission func(string) bool) (bool, bool) {
	return claimsPermission("project.authorship.manage"), claimsPermission("project.team_relation.manage")
}

func projectRelationshipPermissionsForClaims(claims security.Claims) (bool, bool) {
	return projectRelationshipPermissions(func(permission string) bool {
		return claimsAllow(claims, permission) || claimsAllow(claims, "admin.*")
	})
}

// syncProjectCreatorBindingsTx changes relationships in place so their public
// audit identity and timestamps remain stable. Editors without the matching
// sensitive authority cannot remove any existing relationship state, including
// pending, rejected, and revoked rows that may be absent from their snapshot.
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
	rows, err := tx.Query(ctx, projectCreatorBindingsForUpdateSQL, subjectType, subjectID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item resolvedProjectCreatorBinding
		if err = rows.Scan(&item.ID, &item.CreatorID, &item.RoleID, &item.CreatorKind, &item.NameSnapshot,
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

	resolved, err := resolveRequestedProjectCreatorBindingsTx(ctx, tx, authors, actorID, r)
	if err != nil {
		return err
	}
	requested := make(map[string]struct{}, len(resolved))
	mutations := make([]projectCreatorBindingMutation, 0, len(resolved)+len(existing))
	for index, author := range resolved {
		key := projectCreatorBindingKey(author.CreatorID, author.RoleID)
		if _, duplicate := requested[key]; duplicate {
			return errors.New("duplicate project author or team relationship")
		}
		requested[key] = struct{}{}
		previous, found := existing[key]
		status := "pending"
		if found && previous.Status == "approved" {
			status = "approved"
		} else if projectApproved && relationshipChangeAllowed(author.CreatorKind, canManageAuthors, canManageTeams) {
			status = "approved"
		}
		if found && previous.NameSnapshot == author.NameSnapshot && previous.RoleSnapshot == author.RoleSnapshot &&
			previous.PermissionGranting == author.PermissionGranting && previous.Status == status && previous.DisplayOrder == index {
			continue
		}
		mutations = append(mutations, projectCreatorBindingMutation{
			CreatorID: author.CreatorID, RoleID: nullableProjectCreatorRoleID(author.RoleID),
			NameSnapshot: author.NameSnapshot, RoleSnapshot: author.RoleSnapshot,
			PermissionGranting: author.PermissionGranting, Status: status, DisplayOrder: index,
		})
	}

	for key, previous := range existing {
		if _, retained := requested[key]; retained {
			continue
		}
		if !relationshipChangeAllowed(previous.CreatorKind, canManageAuthors, canManageTeams) {
			continue
		}
		if previous.Status != "revoked" {
			mutations = append(mutations, projectCreatorBindingMutation{
				CreatorID: previous.CreatorID, RoleID: nullableProjectCreatorRoleID(previous.RoleID),
				NameSnapshot: previous.NameSnapshot, RoleSnapshot: previous.RoleSnapshot,
				PermissionGranting: previous.PermissionGranting, Status: "revoked", DisplayOrder: previous.DisplayOrder,
			})
		}
	}
	return applyProjectCreatorBindingMutationsTx(ctx, tx, subjectType, subjectID, actorID, mutations)
}

func resolveRequestedProjectCreatorBindingsTx(
	ctx context.Context,
	tx pgx.Tx,
	authors []modAuthorPayload,
	actorID int64,
	r *http.Request,
) ([]requestedProjectCreatorBinding, error) {
	creatorPublicIDs := make([]string, 0, len(authors))
	rolePublicIDs := make([]string, 0, len(authors))
	seenCreators := map[string]bool{}
	seenRoles := map[string]bool{}
	for _, author := range authors {
		if author.CreatorID != "" && !seenCreators[author.CreatorID] {
			seenCreators[author.CreatorID] = true
			creatorPublicIDs = append(creatorPublicIDs, author.CreatorID)
		}
		if author.RoleID != nil && !seenRoles[*author.RoleID] {
			seenRoles[*author.RoleID] = true
			rolePublicIDs = append(rolePublicIDs, *author.RoleID)
		}
	}
	creatorIDsByPublicID := make(map[string]int64, len(creatorPublicIDs))
	if len(creatorPublicIDs) > 0 {
		rows, err := tx.Query(ctx, `select public_id,id from creators where public_id=any($1)`, creatorPublicIDs)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var publicID string
			var creatorID int64
			if err = rows.Scan(&publicID, &creatorID); err != nil {
				rows.Close()
				return nil, err
			}
			creatorIDsByPublicID[publicID] = creatorID
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		if len(creatorIDsByPublicID) != len(creatorPublicIDs) {
			return nil, errors.New("selected author or team does not exist")
		}
	}
	type roleFacts struct {
		ID                 int64
		Name               string
		PermissionGranting bool
	}
	rolesByPublicID := make(map[string]roleFacts, len(rolePublicIDs))
	if len(rolePublicIDs) > 0 {
		rows, err := tx.Query(ctx, `select public_id,id,name,permission_granting
			from creator_role_definitions where public_id=any($1)`, rolePublicIDs)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var publicID string
			var role roleFacts
			if err = rows.Scan(&publicID, &role.ID, &role.Name, &role.PermissionGranting); err != nil {
				rows.Close()
				return nil, err
			}
			rolesByPublicID[publicID] = role
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		if len(rolesByPublicID) != len(rolePublicIDs) {
			return nil, errors.New("selected creator role does not exist")
		}
	}
	creatorIDs := make([]int64, len(authors))
	for index, author := range authors {
		if author.CreatorID != "" {
			creatorIDs[index] = creatorIDsByPublicID[author.CreatorID]
			continue
		}
		kind := author.Kind
		if kind == "" {
			kind = "author"
		}
		creatorID, _, _, err := ensureNamedCreatorSnapshotTx(ctx, tx, creatorSnapshot{
			Kind: kind, Name: author.Name, AvatarURL: author.AvatarURL,
			AvatarFileID: author.AvatarFileID, AvatarInternalID: author.AvatarInternalID,
		}, actorID, "pending", r)
		if err != nil {
			return nil, err
		}
		creatorIDs[index] = creatorID
	}
	type creatorFacts struct {
		Name string
		Kind string
	}
	creatorsByID := make(map[int64]creatorFacts, len(creatorIDs))
	if len(creatorIDs) > 0 {
		rows, err := tx.Query(ctx, `select id,name,kind from creators where id=any($1)`, creatorIDs)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var creatorID int64
			var creator creatorFacts
			if err = rows.Scan(&creatorID, &creator.Name, &creator.Kind); err != nil {
				rows.Close()
				return nil, err
			}
			creatorsByID[creatorID] = creator
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	result := make([]requestedProjectCreatorBinding, 0, len(authors))
	for index, author := range authors {
		creator, ok := creatorsByID[creatorIDs[index]]
		if !ok {
			return nil, errors.New("selected author or team does not exist")
		}
		value := requestedProjectCreatorBinding{
			CreatorID: creatorIDs[index], CreatorKind: creator.Kind, NameSnapshot: creator.Name,
			RoleSnapshot: author.Role,
		}
		if author.RoleID != nil {
			role := rolesByPublicID[*author.RoleID]
			value.RoleID = &role.ID
			value.RoleSnapshot = role.Name
			value.PermissionGranting = role.PermissionGranting
		}
		result = append(result, value)
	}
	return result, nil
}

func applyProjectCreatorBindingMutationsTx(
	ctx context.Context,
	tx pgx.Tx,
	subjectType string,
	subjectID, actorID int64,
	mutations []projectCreatorBindingMutation,
) error {
	if len(mutations) == 0 {
		return nil
	}
	creatorIDs := make([]int64, len(mutations))
	roleIDs := make([]int64, len(mutations))
	names := make([]string, len(mutations))
	roles := make([]string, len(mutations))
	permissionGranting := make([]bool, len(mutations))
	statuses := make([]string, len(mutations))
	displayOrders := make([]int, len(mutations))
	for index, mutation := range mutations {
		creatorIDs[index] = mutation.CreatorID
		roleIDs[index] = mutation.RoleID
		names[index] = mutation.NameSnapshot
		roles[index] = mutation.RoleSnapshot
		permissionGranting[index] = mutation.PermissionGranting
		statuses[index] = mutation.Status
		displayOrders[index] = mutation.DisplayOrder
	}
	tag, err := tx.Exec(ctx, `with input as (
		select * from unnest($3::bigint[],$4::bigint[],$5::text[],$6::text[],$7::boolean[],$8::text[],$9::integer[])
			as value(creator_id,role_id,name_snapshot,role_snapshot,permission_granting,status,display_order)
	)
	insert into content_creator_bindings(subject_type,subject_id,creator_id,role_id,name_snapshot,role_snapshot,
		status,permission_granting,approved_by,approved_at,display_order)
	select $1,$2,creator_id,nullif(role_id,0),name_snapshot,role_snapshot,status,permission_granting,
		case when status='approved' then $10::bigint else null end,
		case when status='approved' then now() else null end,display_order from input
	on conflict(subject_type,subject_id,creator_id,(coalesce(role_id,0))) do update set
		name_snapshot=excluded.name_snapshot,role_snapshot=excluded.role_snapshot,
		permission_granting=excluded.permission_granting,status=excluded.status,display_order=excluded.display_order,
		approved_by=case when excluded.status='approved' and content_creator_bindings.status<>'approved'
			then $10 else content_creator_bindings.approved_by end,
		approved_at=case when excluded.status='approved' and content_creator_bindings.status<>'approved'
			then now() else content_creator_bindings.approved_at end
	where (content_creator_bindings.name_snapshot,content_creator_bindings.role_snapshot,
		content_creator_bindings.permission_granting,content_creator_bindings.status,content_creator_bindings.display_order)
		is distinct from (excluded.name_snapshot,excluded.role_snapshot,excluded.permission_granting,excluded.status,excluded.display_order)`,
		subjectType, subjectID, creatorIDs, roleIDs, names, roles, permissionGranting, statuses, displayOrders, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != int64(len(mutations)) {
		return fmt.Errorf("creator binding mutation count mismatch: got %d want %d", tag.RowsAffected(), len(mutations))
	}
	return nil
}

func nullableProjectCreatorRoleID(roleID *int64) int64 {
	if roleID == nil {
		return 0
	}
	return *roleID
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
	request, err := parseProjectAuthorshipPageRequest(r.URL.Query(), claims.Subject, canManageAuthors, canManageTeams)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	arguments := []any{request.Status, canManageAuthors, canManageTeams}
	cursorPredicate := ""
	if request.Cursor != nil {
		cursorPredicate = `and (binding.created_at,binding.id)>($4,$5)`
		arguments = append(arguments, request.Cursor.CreatedAt, request.Cursor.ID)
	}
	arguments = append(arguments, request.Limit+1)
	limitParameter := len(arguments)
	rows, err := s.db.Query(r.Context(), fmt.Sprintf(`select binding.public_id,binding.subject_type,route.public_id,
		creator.public_id,creator.kind,creator.name,coalesce(role.name,binding.role_snapshot),binding.permission_granting,
		binding.status,binding.created_at,binding.id
		from content_creator_bindings binding
		join public_routes route on route.entity_type=binding.subject_type and route.internal_id=binding.subject_id
		join creators creator on creator.id=binding.creator_id
		left join creator_role_definitions role on role.id=binding.role_id
		where binding.status=$1
		  and ((creator.kind='author' and $2) or (creator.kind='team' and $3))
		  %s
		order by binding.created_at,binding.id limit $%d`, cursorPredicate, limitParameter), arguments...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project authorship relationships")
		return
	}
	defer rows.Close()
	type projectAuthorshipPageRow struct {
		Item      map[string]any
		CreatedAt time.Time
		ID        int64
	}
	pageRows := make([]projectAuthorshipPageRow, 0, request.Limit+1)
	for rows.Next() {
		var id, projectType, projectID, creatorID, creatorKind, creatorName, roleName, relationStatus string
		var permissionGranting bool
		var createdAt time.Time
		var internalID int64
		if err = rows.Scan(&id, &projectType, &projectID, &creatorID, &creatorKind, &creatorName, &roleName,
			&permissionGranting, &relationStatus, &createdAt, &internalID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to parse project authorship relationships")
			return
		}
		pageRows = append(pageRows, projectAuthorshipPageRow{Item: map[string]any{
			"id": id, "projectType": projectType, "projectId": projectID, "creatorId": creatorID,
			"creatorKind": creatorKind, "creatorName": creatorName, "roleName": roleName,
			"permissionGranting": permissionGranting, "status": relationStatus, "createdAt": createdAt,
		}, CreatedAt: createdAt, ID: internalID})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project authorship relationships")
		return
	}
	hasMore := len(pageRows) > request.Limit
	if hasMore {
		pageRows = pageRows[:request.Limit]
	}
	nextCursor := ""
	if hasMore {
		last := pageRows[len(pageRows)-1]
		nextCursor = encodeProjectAuthorshipPageCursor(projectAuthorshipPageCursor{
			Version: projectAuthorshipCursorVersion, Scope: request.Scope, CreatedAt: last.CreatedAt, ID: last.ID,
		})
	}
	items := make([]map[string]any, 0, len(pageRows))
	for _, row := range pageRows {
		items = append(items, row.Item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": hasMore, "nextCursor": nextCursor})
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
	if _, err = tx.Exec(r.Context(), `update content_creator_bindings set status=$2,approved_by=case when $2='approved' then $3::bigint else null end,
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
	if !s.requireSecurityVersionRefresh(w, r, "review_project_authorship", 0,
		s.refreshProjectACLVersion(r.Context())) {
		return
	}
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
