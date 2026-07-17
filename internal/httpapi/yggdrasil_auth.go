package httpapi

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

type yggdrasilAuthenticateRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	ClientToken string `json:"clientToken"`
	RequestUser bool   `json:"requestUser"`
	Agent       struct {
		Name    string `json:"name"`
		Version int    `json:"version"`
	} `json:"agent"`
}

type yggdrasilRefreshRequest struct {
	AccessToken     string            `json:"accessToken"`
	ClientToken     string            `json:"clientToken"`
	RequestUser     bool              `json:"requestUser"`
	SelectedProfile *yggdrasilProfile `json:"selectedProfile"`
}

type yggdrasilTokenRequest struct {
	AccessToken string `json:"accessToken"`
	ClientToken string `json:"clientToken"`
}

type yggdrasilSignoutRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type yggdrasilAuthenticationResponse struct {
	AccessToken       string             `json:"accessToken"`
	ClientToken       string             `json:"clientToken"`
	AvailableProfiles []yggdrasilProfile `json:"availableProfiles"`
	SelectedProfile   *yggdrasilProfile  `json:"selectedProfile,omitempty"`
	User              *yggdrasilUser     `json:"user,omitempty"`
}

type yggdrasilRefreshResponse struct {
	AccessToken     string            `json:"accessToken"`
	ClientToken     string            `json:"clientToken"`
	SelectedProfile *yggdrasilProfile `json:"selectedProfile,omitempty"`
	User            *yggdrasilUser    `json:"user,omitempty"`
}

var yggdrasilDummyPasswordHash = func() string {
	hash, _ := security.HashPassword("mcmods-yggdrasil-invalid-account-placeholder")
	return hash
}()

var errYggdrasilCredentialChanged = errors.New("Yggdrasil launcher credential changed")

type yggdrasilAccountRecord struct {
	UserID             int64
	AccountUUID        string
	LauncherPassword   string
	PreferredLanguage  string
	RequestedProfileID sql.NullInt64
}

type yggdrasilProfileRecord struct {
	InternalID int64
	UUID       string
	Name       string
}

func (profile yggdrasilProfileRecord) publicProfile() yggdrasilProfile {
	id, _ := unsignedYggdrasilUUID(profile.UUID)
	return yggdrasilProfile{ID: id, Name: profile.Name}
}

func (account yggdrasilAccountRecord) publicUser() yggdrasilUser {
	id, _ := unsignedYggdrasilUUID(account.AccountUUID)
	properties := make([]yggdrasilProperty, 0, 1)
	if language := strings.TrimSpace(account.PreferredLanguage); language != "" {
		properties = append(properties, yggdrasilProperty{Name: "preferredLanguage", Value: language})
	}
	return yggdrasilUser{ID: id, Properties: properties}
}

func (s *Server) yggdrasilAuthenticate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var request yggdrasilAuthenticateRequest
	if err := decodeYggdrasilJSON(w, r, &request); err != nil {
		writeYggdrasilError(w, http.StatusBadRequest, "IllegalArgumentException", "Invalid request payload.", "")
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	location := s.yggdrasilClientLocation(r)
	userAgent := boundedYggdrasilUserAgent(r)
	if request.Username == "" || len(request.Username) > yggdrasilIdentifierMax || request.Password == "" || len(request.Password) > yggdrasilPasswordMax || len(request.ClientToken) > 512 {
		writeYggdrasilInvalidCredentials(w)
		return
	}

	account, lookupErr := s.findYggdrasilAccount(r.Context(), request.Username)
	if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
		writeYggdrasilInternalError(w)
		return
	}
	var loginUserID *int64
	if lookupErr == nil {
		loginUserID = &account.UserID
	}
	limited, err := s.yggdrasilLoginRateLimited(r.Context(), loginUserID, location.IP)
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	if limited {
		writeYggdrasilInvalidCredentials(w)
		return
	}
	passwordHash := account.LauncherPassword
	if lookupErr != nil {
		passwordHash = yggdrasilDummyPasswordHash
	}
	passwordValid, passwordChecked := verifyYggdrasilPassword(r.Context(), request.Password, passwordHash)
	if !passwordChecked {
		writeYggdrasilInternalError(w)
		return
	}
	permissionValid := lookupErr == nil && s.yggdrasilUserAllowed(r.Context(), account.UserID)
	if lookupErr != nil || !passwordValid || !permissionValid {
		if err = s.recordYggdrasilLogin(r.Context(), loginUserID, request.Username, location, userAgent, false, "yggdrasil_invalid_credentials"); err != nil {
			writeYggdrasilInternalError(w)
			return
		}
		writeYggdrasilInvalidCredentials(w)
		return
	}
	profiles, err := s.yggdrasilProfilesForUser(r.Context(), account.UserID)
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}

	var selected *yggdrasilProfileRecord
	if account.RequestedProfileID.Valid {
		for index := range profiles {
			if profiles[index].InternalID == account.RequestedProfileID.Int64 {
				selected = &profiles[index]
				break
			}
		}
	} else if len(profiles) == 1 {
		selected = &profiles[0]
	}
	clientToken := request.ClientToken
	if clientToken == "" {
		clientToken, err = newYggdrasilClientToken()
		if err != nil {
			writeYggdrasilInternalError(w)
			return
		}
	}
	accessToken, selected, err := s.issueYggdrasilToken(r.Context(), account.UserID, selected, account.LauncherPassword, clientToken, location.IP, userAgent)
	if err != nil {
		if errors.Is(err, errYggdrasilCredentialChanged) {
			writeYggdrasilInvalidCredentials(w)
			return
		}
		writeYggdrasilInternalError(w)
		return
	}

	response := yggdrasilAuthenticationResponse{
		AccessToken: accessToken,
		ClientToken: clientToken,
	}
	response.AvailableProfiles = make([]yggdrasilProfile, 0, len(profiles))
	for _, profile := range profiles {
		response.AvailableProfiles = append(response.AvailableProfiles, profile.publicProfile())
	}
	if selected != nil {
		public := selected.publicProfile()
		response.SelectedProfile = &public
	}
	if request.RequestUser {
		user := account.publicUser()
		response.User = &user
	}
	if err = s.recordYggdrasilLogin(r.Context(), &account.UserID, request.Username, location, userAgent, true, "yggdrasil_authenticate"); err != nil {
		log.Printf("record successful Yggdrasil authentication: %v", err)
	}
	writeYggdrasilJSON(w, http.StatusOK, response)
}

func (s *Server) yggdrasilRefresh(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var request yggdrasilRefreshRequest
	if err := decodeYggdrasilJSON(w, r, &request); err != nil || strings.TrimSpace(request.AccessToken) == "" || len(request.AccessToken) > yggdrasilTokenInputMax || len(request.ClientToken) > 512 {
		writeYggdrasilError(w, http.StatusBadRequest, "IllegalArgumentException", "Invalid request payload.", "")
		return
	}

	oldHash := hashYggdrasilToken(request.AccessToken)
	var userID int64
	var preflightProfileID sql.NullInt64
	err := s.db.QueryRow(r.Context(), `select user_id,player_profile_id from yggdrasil_tokens
		where access_token_hash=$1 and status in ('active','stale') and expires_at>now()`, oldHash).
		Scan(&userID, &preflightProfileID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeYggdrasilInvalidToken(w)
		return
	}
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	if !s.yggdrasilUserAllowed(r.Context(), userID) {
		writeYggdrasilInvalidToken(w)
		return
	}
	if request.SelectedProfile != nil && preflightProfileID.Valid {
		writeYggdrasilError(w, http.StatusBadRequest, "IllegalArgumentException", "Access token already has a profile assigned.", "")
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	defer tx.Rollback(r.Context())
	var accountUUID, preferredLanguage string
	err = tx.QueryRow(r.Context(), `select a.account_uuid::text,u.preferred_ui_language
		from yggdrasil_accounts a join users u on u.id=a.user_id and u.status='active'
		where a.user_id=$1 and a.enabled=true for update of a`, userID).Scan(&accountUUID, &preferredLanguage)
	if err != nil {
		writeYggdrasilInvalidToken(w)
		return
	}
	rateLimited, err := yggdrasilIssueRateLimitedTx(r.Context(), tx, userID)
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	if rateLimited {
		writeYggdrasilInvalidToken(w)
		return
	}

	selectedProfileID := preflightProfileID
	var selectedPublicProfile *yggdrasilProfile
	if request.SelectedProfile != nil {
		databaseUUID, ok := databaseYggdrasilUUID(request.SelectedProfile.ID)
		if !ok {
			writeYggdrasilInvalidToken(w)
			return
		}
		var selectedRecord yggdrasilProfileRecord
		err = tx.QueryRow(r.Context(), `select id,uuid::text,name from player_profiles
			where user_id=$1 and uuid=$2 and status='active' for update`, userID, databaseUUID).
			Scan(&selectedRecord.InternalID, &selectedRecord.UUID, &selectedRecord.Name)
		if err != nil {
			writeYggdrasilInvalidToken(w)
			return
		}
		selectedProfileID = sql.NullInt64{Int64: selectedRecord.InternalID, Valid: true}
		public := selectedRecord.publicProfile()
		selectedPublicProfile = &public
	} else if selectedProfileID.Valid {
		var selectedRecord yggdrasilProfileRecord
		err = tx.QueryRow(r.Context(), `select id,uuid::text,name from player_profiles
			where id=$1 and user_id=$2 and status='active' for update`, selectedProfileID.Int64, userID).
			Scan(&selectedRecord.InternalID, &selectedRecord.UUID, &selectedRecord.Name)
		if err != nil {
			writeYggdrasilInvalidToken(w)
			return
		}
		public := selectedRecord.publicProfile()
		selectedPublicProfile = &public
	}

	var oldID int64
	var boundProfileID sql.NullInt64
	var storedClientToken, status string
	var expiresAt time.Time
	err = tx.QueryRow(r.Context(),
		`select t.id,t.player_profile_id,t.client_token,t.status,t.expires_at
		 from yggdrasil_tokens t
		 where t.access_token_hash=$1 and t.user_id=$2
		 for update of t`, oldHash, userID,
	).Scan(&oldID, &boundProfileID, &storedClientToken, &status, &expiresAt)
	if err != nil || (status != "active" && status != "stale") || !expiresAt.After(time.Now()) || (request.ClientToken != "" && request.ClientToken != storedClientToken) {
		writeYggdrasilInvalidToken(w)
		return
	}
	if boundProfileID.Valid != preflightProfileID.Valid || (boundProfileID.Valid && boundProfileID.Int64 != preflightProfileID.Int64) {
		writeYggdrasilInvalidToken(w)
		return
	}

	plain, tokenHash, err := newYggdrasilToken()
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	ttl := s.yggdrasilTokenTTL()
	var newID int64
	err = tx.QueryRow(r.Context(),
		`insert into yggdrasil_tokens
		 (access_token_hash,user_id,player_profile_id,client_token,status,issued_at,expires_at,ip,user_agent)
		 values ($1,$2,$3,$4,'active',now(),$5,$6,$7)
		 returning id`,
		tokenHash, userID, nullableInt64Value(selectedProfileID), storedClientToken, time.Now().Add(ttl), s.yggdrasilClientLocation(r).IP, boundedYggdrasilUserAgent(r),
	).Scan(&newID)
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	command, err := tx.Exec(r.Context(),
		`update yggdrasil_tokens set status='revoked',revoked_at=now(),replaced_by_id=$2,last_used_at=now()
		 where id=$1 and status in ('active','stale')`, oldID, newID)
	if err != nil || command.RowsAffected() != 1 {
		writeYggdrasilInvalidToken(w)
		return
	}
	if err := s.revokeExcessYggdrasilTokensTx(r.Context(), tx, userID); err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	if err := s.pruneYggdrasilTokenHistoryTx(r.Context(), tx, userID); err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeYggdrasilInternalError(w)
		return
	}

	response := yggdrasilRefreshResponse{AccessToken: plain, ClientToken: storedClientToken}
	response.SelectedProfile = selectedPublicProfile
	if request.RequestUser {
		account := yggdrasilAccountRecord{AccountUUID: accountUUID, PreferredLanguage: preferredLanguage}
		user := account.publicUser()
		response.User = &user
	}
	writeYggdrasilJSON(w, http.StatusOK, response)
}

func (s *Server) yggdrasilValidate(w http.ResponseWriter, r *http.Request) {
	var request yggdrasilTokenRequest
	if err := decodeYggdrasilJSON(w, r, &request); err != nil || request.AccessToken == "" || len(request.AccessToken) > yggdrasilTokenInputMax || len(request.ClientToken) > 512 {
		writeYggdrasilInvalidToken(w)
		return
	}
	query := `select t.id,t.user_id from yggdrasil_tokens t
	          join yggdrasil_accounts a on a.user_id=t.user_id and a.enabled=true
	          join users u on u.id=t.user_id and u.status='active'
	          where t.access_token_hash=$1 and t.status='active' and t.expires_at>now()`
	arguments := []any{hashYggdrasilToken(request.AccessToken)}
	if request.ClientToken != "" {
		query += ` and t.client_token=$2`
		arguments = append(arguments, request.ClientToken)
	}
	var tokenID, userID int64
	err := s.db.QueryRow(r.Context(), query, arguments...).Scan(&tokenID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeYggdrasilInvalidToken(w)
		return
	}
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	if !s.yggdrasilUserAllowed(r.Context(), userID) {
		writeYggdrasilInvalidToken(w)
		return
	}
	command, err := s.db.Exec(r.Context(),
		`update yggdrasil_tokens set last_used_at=now()
		 where id=$1 and status='active' and expires_at>now()`, tokenID)
	if err != nil || command.RowsAffected() != 1 {
		writeYggdrasilInvalidToken(w)
		return
	}
	writeYggdrasilNoContent(w)
}

func (s *Server) yggdrasilInvalidate(w http.ResponseWriter, r *http.Request) {
	var request yggdrasilTokenRequest
	if err := decodeYggdrasilJSON(w, r, &request); err == nil && request.AccessToken != "" && len(request.AccessToken) <= yggdrasilTokenInputMax {
		tx, err := s.db.Begin(r.Context())
		if err != nil {
			writeYggdrasilInternalError(w)
			return
		}
		defer tx.Rollback(r.Context())
		var tokenID int64
		err = tx.QueryRow(r.Context(), `select id from yggdrasil_tokens where access_token_hash=$1 for update`, hashYggdrasilToken(request.AccessToken)).Scan(&tokenID)
		if err == nil {
			if _, err = tx.Exec(r.Context(), `delete from yggdrasil_join_sessions where token_id=$1`, tokenID); err != nil {
				writeYggdrasilInternalError(w)
				return
			}
			if _, err = tx.Exec(r.Context(), `update yggdrasil_tokens set status='revoked',revoked_at=coalesce(revoked_at,now())
				where id=$1 and status<>'revoked'`, tokenID); err != nil {
				writeYggdrasilInternalError(w)
				return
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			writeYggdrasilInternalError(w)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeYggdrasilInternalError(w)
			return
		}
	}
	writeYggdrasilNoContent(w)
}

func (s *Server) yggdrasilSignout(w http.ResponseWriter, r *http.Request) {
	var request yggdrasilSignoutRequest
	if err := decodeYggdrasilJSON(w, r, &request); err != nil {
		writeYggdrasilInvalidCredentials(w)
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	location := s.yggdrasilClientLocation(r)
	userAgent := boundedYggdrasilUserAgent(r)
	if request.Username == "" || len(request.Username) > yggdrasilIdentifierMax || request.Password == "" || len(request.Password) > yggdrasilPasswordMax {
		writeYggdrasilInvalidCredentials(w)
		return
	}
	account, lookupErr := s.findYggdrasilAccount(r.Context(), request.Username)
	if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
		writeYggdrasilInternalError(w)
		return
	}
	var loginUserID *int64
	if lookupErr == nil {
		loginUserID = &account.UserID
	}
	limited, err := s.yggdrasilLoginRateLimited(r.Context(), loginUserID, location.IP)
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	if limited {
		writeYggdrasilInvalidCredentials(w)
		return
	}
	passwordHash := account.LauncherPassword
	if lookupErr != nil {
		passwordHash = yggdrasilDummyPasswordHash
	}
	passwordValid, passwordChecked := verifyYggdrasilPassword(r.Context(), request.Password, passwordHash)
	if !passwordChecked {
		writeYggdrasilInternalError(w)
		return
	}
	if lookupErr != nil || !passwordValid {
		if err = s.recordYggdrasilLogin(r.Context(), loginUserID, request.Username, location, userAgent, false, "yggdrasil_signout_invalid_credentials"); err != nil {
			writeYggdrasilInternalError(w)
			return
		}
		writeYggdrasilInvalidCredentials(w)
		return
	}
	if err := s.revokeYggdrasilTokensForUser(r.Context(), account.UserID, account.LauncherPassword); err != nil {
		if errors.Is(err, errYggdrasilCredentialChanged) {
			writeYggdrasilInvalidCredentials(w)
			return
		}
		writeYggdrasilInternalError(w)
		return
	}
	if err = s.recordYggdrasilLogin(r.Context(), &account.UserID, request.Username, location, userAgent, true, "yggdrasil_signout"); err != nil {
		log.Printf("record successful Yggdrasil signout: %v", err)
	}
	writeYggdrasilNoContent(w)
}

func (s *Server) findYggdrasilAccount(ctx context.Context, identifier string) (yggdrasilAccountRecord, error) {
	var account yggdrasilAccountRecord
	err := s.db.QueryRow(ctx,
		`select a.user_id,a.account_uuid::text,a.launcher_password_hash,u.preferred_ui_language,p.id
		 from yggdrasil_accounts a
		 join users u on u.id=a.user_id and u.status='active'
		 left join player_profiles p on p.user_id=a.user_id and p.status='active' and lower(p.name)=lower($1)
		 where a.enabled=true
		   and (lower(u.email)=lower($1) or p.id is not null)
		 order by (p.id is not null) desc
		 limit 1`, identifier,
	).Scan(&account.UserID, &account.AccountUUID, &account.LauncherPassword, &account.PreferredLanguage, &account.RequestedProfileID)
	return account, err
}

func (s *Server) yggdrasilProfilesForUser(ctx context.Context, userID int64) ([]yggdrasilProfileRecord, error) {
	rows, err := s.db.Query(ctx,
		`select id,uuid::text,name from player_profiles
		 where user_id=$1 and status='active'
		 order by is_default desc,created_at,id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := make([]yggdrasilProfileRecord, 0)
	for rows.Next() {
		var profile yggdrasilProfileRecord
		if err := rows.Scan(&profile.InternalID, &profile.UUID, &profile.Name); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func (s *Server) yggdrasilProfileRecordByID(ctx context.Context, id int64) (yggdrasilProfileRecord, error) {
	var profile yggdrasilProfileRecord
	err := s.db.QueryRow(ctx, `select id,uuid::text,name from player_profiles where id=$1 and status='active'`, id).
		Scan(&profile.InternalID, &profile.UUID, &profile.Name)
	return profile, err
}

func (s *Server) issueYggdrasilToken(ctx context.Context, userID int64, profile *yggdrasilProfileRecord, verifiedPasswordHash, clientToken, ip, userAgent string) (string, *yggdrasilProfileRecord, error) {
	plain, tokenHash, err := newYggdrasilToken()
	if err != nil {
		return "", nil, err
	}
	if !s.yggdrasilUserAllowed(ctx, userID) {
		return "", nil, errors.New("Yggdrasil launcher permission denied")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback(ctx)
	var currentPasswordHash string
	if err := tx.QueryRow(ctx, `select launcher_password_hash from yggdrasil_accounts where user_id=$1 and enabled=true for update`, userID).Scan(&currentPasswordHash); err != nil {
		return "", nil, err
	}
	if subtle.ConstantTimeCompare([]byte(currentPasswordHash), []byte(verifiedPasswordHash)) != 1 {
		return "", nil, errYggdrasilCredentialChanged
	}
	if limited, err := yggdrasilIssueRateLimitedTx(ctx, tx, userID); err != nil {
		return "", nil, err
	} else if limited {
		return "", nil, errors.New("Yggdrasil token issuance rate exceeded")
	}
	var profileID any
	var selected *yggdrasilProfileRecord
	if profile != nil {
		var current yggdrasilProfileRecord
		if err = tx.QueryRow(ctx, `select id,uuid::text,name from player_profiles
			where id=$1 and user_id=$2 and status='active' for update`, profile.InternalID, userID).
			Scan(&current.InternalID, &current.UUID, &current.Name); err != nil {
			return "", nil, err
		}
		profileID = current.InternalID
		selected = &current
	}
	_, err = tx.Exec(ctx,
		`insert into yggdrasil_tokens
		 (access_token_hash,user_id,player_profile_id,client_token,status,issued_at,expires_at,ip,user_agent)
		 values ($1,$2,$3,$4,'active',now(),$5,$6,$7)`,
		tokenHash, userID, profileID, clientToken, time.Now().Add(s.yggdrasilTokenTTL()), ip, userAgent)
	if err != nil {
		return "", nil, err
	}
	if err := s.revokeExcessYggdrasilTokensTx(ctx, tx, userID); err != nil {
		return "", nil, err
	}
	if err := s.pruneYggdrasilTokenHistoryTx(ctx, tx, userID); err != nil {
		return "", nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", nil, err
	}
	return plain, selected, nil
}

func (s *Server) revokeExcessYggdrasilTokensTx(ctx context.Context, tx pgx.Tx, userID int64) error {
	maximum := s.cfg.Yggdrasil.MaxTokens
	if maximum <= 0 {
		maximum = 10
	}
	_, err := tx.Exec(ctx,
		`update yggdrasil_tokens set status='revoked',revoked_at=coalesce(revoked_at,now())
		 where id in (
		   select id from yggdrasil_tokens
		   where user_id=$1 and status in ('active','stale') and expires_at>now()
		   order by issued_at desc,id desc offset $2
		 )`, userID, maximum)
	return err
}

func yggdrasilIssueRateLimitedTx(ctx context.Context, tx pgx.Tx, userID int64) (bool, error) {
	var recent int
	err := tx.QueryRow(ctx,
		`select count(*) from yggdrasil_tokens
		 where user_id=$1 and issued_at>now()-interval '1 minute'`, userID).Scan(&recent)
	return recent >= yggdrasilIssuesPerMinute, err
}

func (s *Server) pruneYggdrasilTokenHistoryTx(ctx context.Context, tx pgx.Tx, userID int64) error {
	limit := s.cfg.Yggdrasil.MaxTokens * 5
	if limit < 100 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	_, err := tx.Exec(ctx,
		`delete from yggdrasil_tokens where id in (
		   select id from yggdrasil_tokens
		   where user_id=$1 and (status='revoked' or expires_at<=now())
		   order by issued_at desc,id desc offset $2
		 )`, userID, limit)
	return err
}

func (s *Server) yggdrasilTokenTTL() time.Duration {
	if s.cfg.Yggdrasil.TokenTTL <= 0 {
		return 15 * 24 * time.Hour
	}
	return s.cfg.Yggdrasil.TokenTTL
}

func (s *Server) revokeYggdrasilTokensForUser(ctx context.Context, userID int64, expectedPasswordHash string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var currentPasswordHash string
	if err = tx.QueryRow(ctx,
		`select launcher_password_hash from yggdrasil_accounts where user_id=$1 and enabled=true for update`, userID,
	).Scan(&currentPasswordHash); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(currentPasswordHash), []byte(expectedPasswordHash)) != 1 {
		return errYggdrasilCredentialChanged
	}
	if _, err = tx.Exec(ctx, `delete from yggdrasil_join_sessions
		where token_id in (select id from yggdrasil_tokens where user_id=$1)`, userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx,
		`update yggdrasil_tokens set status='revoked',revoked_at=coalesce(revoked_at,now())
		 where user_id=$1 and status<>'revoked'`, userID); err != nil {
		return err
	}
	if err = s.pruneYggdrasilTokenHistoryTx(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Server) yggdrasilLoginRateLimited(ctx context.Context, userID *int64, ip string) (bool, error) {
	var failures int
	err := s.db.QueryRow(ctx,
		`select count(*) from user_login_logs
		 where created_at>now()-interval '15 minutes' and success=false
		   and reason like 'yggdrasil_%'
		   and (($1::bigint is not null and user_id=$1) or ($1::bigint is null and $2<>'' and ip=$2))`, userID, ip).Scan(&failures)
	return failures >= 10, err
}

func (s *Server) recordYggdrasilLogin(ctx context.Context, userID *int64, account string, location clientLocation, userAgent string, success bool, reason string) error {
	_, err := s.db.Exec(ctx,
		`insert into user_login_logs (user_id,account,ip,country_code,city,user_agent,success,reason)
		 values ($1,$2,$3,$4,$5,$6,$7,$8)`, userID, account, location.IP, location.CountryCode,
		location.City, userAgent, success, reason)
	return err
}

func nullableInt64Value(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

func writeYggdrasilInvalidCredentials(w http.ResponseWriter) {
	writeYggdrasilError(w, http.StatusForbidden, "ForbiddenOperationException", "Invalid credentials. Invalid username or password.", "")
}

func writeYggdrasilInvalidToken(w http.ResponseWriter) {
	writeYggdrasilError(w, http.StatusForbidden, "ForbiddenOperationException", "Invalid token.", "")
}

func writeYggdrasilInternalError(w http.ResponseWriter) {
	writeYggdrasilError(w, http.StatusServiceUnavailable, "ServiceUnavailableException", "Authentication service is unavailable.", "")
}
