package httpapi

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type yggdrasilJoinRequest struct {
	AccessToken     string `json:"accessToken"`
	SelectedProfile string `json:"selectedProfile"`
	ServerID        string `json:"serverId"`
}

func (s *Server) yggdrasilJoin(w http.ResponseWriter, r *http.Request) {
	var request yggdrasilJoinRequest
	if err := decodeYggdrasilJSON(w, r, &request); err != nil || request.AccessToken == "" || len(request.AccessToken) > yggdrasilTokenInputMax || len(request.ServerID) == 0 || len(request.ServerID) > 255 {
		writeYggdrasilInvalidToken(w)
		return
	}
	selectedUUID, ok := unsignedYggdrasilUUID(request.SelectedProfile)
	if !ok {
		writeYggdrasilInvalidToken(w)
		return
	}

	accessTokenHash := hashYggdrasilToken(request.AccessToken)
	var userID int64
	err := s.db.QueryRow(r.Context(), `select user_id from yggdrasil_tokens
		where access_token_hash=$1 and status='active' and expires_at>now()`, accessTokenHash).Scan(&userID)
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

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	defer tx.Rollback(r.Context())

	var tokenID, lockedUserID, profileID int64
	var profileUUID string
	err = tx.QueryRow(r.Context(),
		`select t.id,t.user_id,t.player_profile_id,p.uuid::text
		 from yggdrasil_tokens t
		 join yggdrasil_accounts a on a.user_id=t.user_id and a.enabled=true
		 join users u on u.id=t.user_id and u.status='active'
		 join player_profiles p on p.id=t.player_profile_id and p.status='active'
		 where t.access_token_hash=$1 and t.status='active' and t.expires_at>now()
		 for update of t`, accessTokenHash,
	).Scan(&tokenID, &lockedUserID, &profileID, &profileUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeYggdrasilInvalidToken(w)
		return
	}
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	profileUnsigned, profileOK := unsignedYggdrasilUUID(profileUUID)
	if lockedUserID != userID || !profileOK || profileUnsigned != selectedUUID {
		writeYggdrasilInvalidToken(w)
		return
	}

	clientIP := s.yggdrasilClientLocation(r).IP
	expiresAt := time.Now().Add(s.yggdrasilJoinTTL())
	command, err := tx.Exec(r.Context(),
		`insert into yggdrasil_join_sessions
		 (server_id,token_id,player_profile_id,client_ip,expires_at,created_at)
		 values ($1,$2,$3,$4,$5,now())
		 on conflict (server_id) do update set
		 token_id=excluded.token_id,player_profile_id=excluded.player_profile_id,
		 client_ip=excluded.client_ip,expires_at=excluded.expires_at,created_at=now()
		 where yggdrasil_join_sessions.expires_at<=now()
		    or yggdrasil_join_sessions.token_id=excluded.token_id`,
		request.ServerID, tokenID, profileID, clientIP, expiresAt)
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	if command.RowsAffected() != 1 {
		writeYggdrasilInvalidToken(w)
		return
	}
	command, err = tx.Exec(r.Context(), `update yggdrasil_tokens set last_used_at=now()
		where id=$1 and status='active' and expires_at>now()`, tokenID)
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	if command.RowsAffected() != 1 {
		writeYggdrasilInvalidToken(w)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	writeYggdrasilNoContent(w)
}

func (s *Server) yggdrasilHasJoined(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	serverID := r.URL.Query().Get("serverId")
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	if ip != "" {
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			writeYggdrasilNoContent(w)
			return
		}
		ip = parsedIP.String()
	}
	if username == "" || serverID == "" || len(serverID) > 255 {
		writeYggdrasilNoContent(w)
		return
	}

	query := `select t.user_id,j.player_profile_id
	          from yggdrasil_join_sessions j
	          join yggdrasil_tokens t on t.id=j.token_id and t.status='active' and t.expires_at>now()
	          join yggdrasil_accounts a on a.user_id=t.user_id and a.enabled=true
	          join users u on u.id=t.user_id and u.status='active'
	          join player_profiles p on p.id=j.player_profile_id and p.status='active'
	          where j.server_id=$1 and j.expires_at>now() and p.name=$2`
	arguments := []any{serverID, username}
	if ip != "" {
		query += ` and j.client_ip=$3`
		arguments = append(arguments, ip)
	}
	var userID, profileID int64
	err := s.db.QueryRow(r.Context(), query, arguments...).Scan(&userID, &profileID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeYggdrasilNoContent(w)
		return
	}
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	if !s.yggdrasilUserAllowed(r.Context(), userID) {
		writeYggdrasilNoContent(w)
		return
	}
	profile, err := s.buildYggdrasilProfile(r.Context(), profileID, true)
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	writeYggdrasilJSON(w, http.StatusOK, profile)
}

func (s *Server) yggdrasilJoinTTL() time.Duration {
	yggdrasilConfig, _ := s.yggdrasilRuntimeSnapshot()
	if yggdrasilConfig.JoinTTL <= 0 {
		return 30 * time.Second
	}
	return yggdrasilConfig.JoinTTL
}
