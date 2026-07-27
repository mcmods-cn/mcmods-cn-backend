package domain

import "time"

type User struct {
	ID            int64      `json:"-"`
	PublicID      string     `json:"id"`
	Username      string     `json:"username"`
	Email         string     `json:"email"`
	DisplayName   string     `json:"displayName"`
	EmailVerified bool       `json:"emailVerified"`
	Status        string     `json:"status"`
	Roles         []string   `json:"roles"`
	Permissions   []string   `json:"permissions"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastLoginAt   *time.Time `json:"lastLoginAt,omitempty"`
	AvatarURL     string     `json:"avatarUrl,omitempty"`
	Signature     string     `json:"signature,omitempty"`
}

type Role struct {
	Code              string                `json:"code"`
	Name              string                `json:"name"`
	Description       string                `json:"description"`
	Translations      LocalizedTexts        `json:"translations"`
	Weight            int                   `json:"weight"`
	Parents           []string              `json:"parents"`
	Permissions       []string              `json:"permissions"`
	PermissionEntries []RolePermissionEntry `json:"permissionEntries"`
}

type RolePermissionEntry struct {
	Code      string `json:"code"`
	Allow     bool   `json:"allow"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

type Permission struct {
	Code         string         `json:"code"`
	Module       string         `json:"module"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Translations LocalizedTexts `json:"translations"`
}

type LocalizedText struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type LocalizedTexts map[string]LocalizedText
