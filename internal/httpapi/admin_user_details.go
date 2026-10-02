package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/domain"
	"mcmods-cn-backend/internal/security"
)

type adminUserDetailsResponse struct {
	User                    domain.User               `json:"user"`
	Country                 string                    `json:"country"`
	Timezone                string                    `json:"timezone"`
	PrimaryLanguage         string                    `json:"primaryLanguage"`
	SecondaryLanguage       string                    `json:"secondaryLanguage"`
	RegistrationIP          string                    `json:"registrationIp"`
	RegistrationCountryCode string                    `json:"registrationCountryCode"`
	RegistrationCity        string                    `json:"registrationCity"`
	OAuthProviders          []string                  `json:"oauthProviders"`
	LastLogin               *adminUserLoginDetails    `json:"lastLogin,omitempty"`
	RootPermissions         []security.PermissionRule `json:"rootPermissions"`
}

type adminUserLoginDetails struct {
	At          string `json:"at"`
	IP          string `json:"ip"`
	CountryCode string `json:"countryCode"`
	City        string `json:"city"`
	UserAgent   string `json:"userAgent"`
}

func (s *Server) adminUserDetails(w http.ResponseWriter, r *http.Request) {
	identity, err := s.resolvePublicIdentity(r.Context(), r.PathValue("id"), "user")
	if err != nil {
		writeError(w, http.StatusBadRequest, "用户 ID 不正确")
		return
	}

	details, err := s.loadAdminUserDetails(r.Context(), identity.InternalID)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "用户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取用户详情失败")
		return
	}
	writeJSON(w, http.StatusOK, details)
}

func (s *Server) loadAdminUserDetails(ctx context.Context, userID int64) (adminUserDetailsResponse, error) {
	var result adminUserDetailsResponse
	result.OAuthProviders = []string{}
	err := s.db.QueryRow(
		ctx,
		`select id, public_id, username, email, email_verified, status, created_at, last_login_at,
		        country, timezone, preferred_content_language, secondary_content_language,
		        registration_ip, registration_country_code, registration_city
		 from users where id = $1`,
		userID,
	).Scan(
		&result.User.ID,
		&result.User.PublicID,
		&result.User.Username,
		&result.User.Email,
		&result.User.EmailVerified,
		&result.User.Status,
		&result.User.CreatedAt,
		&result.User.LastLoginAt,
		&result.Country,
		&result.Timezone,
		&result.PrimaryLanguage,
		&result.SecondaryLanguage,
		&result.RegistrationIP,
		&result.RegistrationCountryCode,
		&result.RegistrationCity,
	)
	if err != nil {
		return result, err
	}

	roles, permissions, err := s.resolveUserRootPermissions(ctx, userID)
	if err != nil {
		return result, err
	}
	result.User.RoleCodes = roles
	result.RootPermissions = permissions

	providerRows, err := s.db.Query(ctx, `select provider from oauth_accounts where user_id = $1 order by provider`, userID)
	if err != nil {
		return result, err
	}
	for providerRows.Next() {
		var provider string
		if err := providerRows.Scan(&provider); err != nil {
			providerRows.Close()
			return result, err
		}
		result.OAuthProviders = append(result.OAuthProviders, provider)
	}
	if err := providerRows.Err(); err != nil {
		providerRows.Close()
		return result, err
	}
	providerRows.Close()

	var login adminUserLoginDetails
	var loginAt time.Time
	err = s.db.QueryRow(
		ctx,
		`select created_at, ip, country_code, city, user_agent
		 from user_login_logs
		 where user_id = $1 and success = true
		 order by created_at desc
		 limit 1`,
		userID,
	).Scan(&loginAt, &login.IP, &login.CountryCode, &login.City, &login.UserAgent)
	if err == nil {
		login.At = loginAt.Format(time.RFC3339)
		result.LastLogin = &login
	} else if err != pgx.ErrNoRows {
		return result, err
	}
	return result, nil
}
