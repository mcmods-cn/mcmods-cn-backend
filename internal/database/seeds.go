package database

import (
	"context"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/security"
)

type seedPermission struct {
	Code        string
	Module      string
	Name        string
	Description string
}

type seedUser struct {
	Username    string
	Email       string
	DisplayName string
	Status      string
	Permissions []string
}

var seedPermissions = []seedPermission{
	{Code: "admin.*", Module: "admin", Name: "All administration permissions", Description: "Grants access to every administration capability."},
	{Code: "admin.access", Module: "admin", Name: "Access administration", Description: "Allows access to the administration console."},
	{Code: "admin.config.read", Module: "settings", Name: "Read system configuration", Description: "Allows reading system configuration."},
	{Code: "admin.config.write", Module: "settings", Name: "Write system configuration", Description: "Allows modifying system configuration."},

	{Code: "user.read", Module: "user", Name: "Read users", Description: "Allows reading user accounts and login information."},
	{Code: "user.write", Module: "user", Name: "Manage users", Description: "Allows modifying user accounts, status and security settings."},
	{Code: "user.follow.create", Module: "user", Name: "Follow users", Description: "Allows following another user."},
	{Code: "user.follow.receive", Module: "user", Name: "Receive followers", Description: "Allows the account to be followed."},
	{Code: "user.message.send", Module: "user", Name: "Send direct messages", Description: "Allows sending direct messages."},
	{Code: "user.message.receive", Module: "user", Name: "Receive direct messages", Description: "Allows receiving direct messages."},
	{Code: "user.avatar.update", Module: "user", Name: "Update avatar", Description: "Allows changing the account avatar."},
	{Code: "user.avatar.animated", Module: "user", Name: "Use animated avatar", Description: "Allows GIF or APNG avatars."},
	{Code: "user.ai.daily_token_limit.<num>", Module: "user", Name: "Daily AI token limit", Description: "Numeric permission; replace <num> with the daily token allowance."},
	{Code: "user.file.daily_limit.<num>", Module: "user", Name: "Daily upload limit", Description: "Numeric permission; replace <num> with bytes allowed per day."},
	{Code: "user.file.total_limit.<num>", Module: "user", Name: "Total upload limit", Description: "Numeric permission; replace <num> with total stored bytes."},
	{Code: "user.file.single_limit.<num>", Module: "user", Name: "Single file limit", Description: "Numeric permission; replace <num> with maximum bytes per file."},

	{Code: "skin.library.upload", Module: "skin", Name: "Upload skin library assets", Description: "Allows uploading skins and capes to the user's library."},
	{Code: "skin.library.limit.<num>", Module: "skin", Name: "Skin library asset limit", Description: "Numeric permission; replace <num> with the maximum number of active skin library assets."},
	{Code: "skin.profile.create", Module: "skin", Name: "Create player profiles", Description: "Allows creating Minecraft player profiles."},
	{Code: "skin.profile.write", Module: "skin", Name: "Manage player profiles", Description: "Allows editing owned Minecraft player profiles and their textures."},
	{Code: "skin.launcher.login", Module: "skin", Name: "Use launcher login", Description: "Allows authenticating through the third-party Yggdrasil service."},
	{Code: "skin.profile.limit.<num>", Module: "skin", Name: "Player profile limit", Description: "Numeric permission; replace <num> with the maximum number of player profiles."},
	{Code: "skin.admin", Module: "skin", Name: "Manage the skin service", Description: "Allows moderating skin assets, player profiles and launcher sessions."},

	{Code: "permission.read", Module: "permission", Name: "Read permissions", Description: "Allows reading permission groups, nodes and assignments."},
	{Code: "permission.write", Module: "permission", Name: "Manage permissions", Description: "Allows changing permission groups, nodes and assignments."},

	{Code: "creator.create", Module: "creator", Name: "Create authors and teams", Description: "Allows creating author or team profiles."},
	{Code: "creator.edit", Module: "creator", Name: "Edit authors and teams", Description: "Allows submitting changes to author or team profiles."},
	{Code: "creator.claim", Module: "creator", Name: "Claim authors and teams", Description: "Allows submitting ownership claims."},
	{Code: "creator.claim.review", Module: "creator", Name: "Review creator claims", Description: "Allows approving or rejecting author and team claims."},
	{Code: "creator.role.write", Module: "creator", Name: "Manage team roles", Description: "Allows creating custom team role definitions."},

	{Code: "activity.read", Module: "activity", Name: "Read user activity", Description: "Allows searching the batched user operation log."},

	{Code: "economy.read", Module: "economy", Name: "Read economy settings", Description: "Allows reading currencies, balances and economy configuration."},
	{Code: "economy.write", Module: "economy", Name: "Manage economy settings", Description: "Allows modifying currencies and economy configuration."},
	{Code: "economy.checkin", Module: "economy", Name: "Check in", Description: "Allows claiming the configured daily check-in reward."},
	{Code: "economy.transfer", Module: "economy", Name: "Transfer currency", Description: "Allows transferring currency to another user."},

	{Code: "shop.read", Module: "shop", Name: "Read shop settings", Description: "Allows reading all shop items and configuration."},
	{Code: "shop.write", Module: "shop", Name: "Manage shop settings", Description: "Allows creating and modifying shop items."},
	{Code: "shop.purchase", Module: "shop", Name: "Purchase shop items", Description: "Allows purchasing an item from the site shop."},
	{Code: "shop.use", Module: "shop", Name: "Use shop items", Description: "Allows using an owned shop item."},
	{Code: "shop.profile_background.purchase", Module: "shop", Name: "Purchase profile background", Description: "Allows purchasing the profile background item."},
	{Code: "shop.profile_background.use", Module: "shop", Name: "Use profile background", Description: "Allows applying a custom profile background."},

	{Code: "level.read", Module: "level", Name: "Read level settings", Description: "Allows reading level thresholds and role-track binding."},
	{Code: "level.write", Module: "level", Name: "Manage level settings", Description: "Allows modifying level thresholds and role-track binding."},
	{Code: "task.read", Module: "task", Name: "Read task settings", Description: "Allows reading all task definitions and progress rules."},
	{Code: "task.write", Module: "task", Name: "Manage task settings", Description: "Allows creating and modifying task definitions."},

	{Code: "mail.read", Module: "mail", Name: "Read mail configuration", Description: "Allows reading mail service configuration and templates."},
	{Code: "mail.write", Module: "mail", Name: "Manage mail configuration", Description: "Allows modifying SMTP settings, templates and sending tests."},
	{Code: "oss.read", Module: "oss", Name: "Read OSS", Description: "Allows reading OSS settings, files, scans and statistics."},
	{Code: "oss.write", Module: "oss", Name: "Manage OSS", Description: "Allows modifying OSS settings and managing private objects."},
	{Code: "log.read", Module: "log", Name: "Read logs", Description: "Allows searching system, user, security, API and task logs."},
	{Code: "log.write", Module: "log", Name: "Manage logs", Description: "Allows modifying log retention and cleanup policies."},
	{Code: "ai.read", Module: "ai", Name: "Read AI system", Description: "Allows reading AI providers, models, tasks and usage."},
	{Code: "ai.write", Module: "ai", Name: "Manage AI system", Description: "Allows modifying AI providers, models and task bindings."},
	{Code: "ai.task.enqueue", Module: "ai", Name: "Enqueue AI tasks", Description: "Allows enqueueing asynchronous AI work."},
	{Code: "ai.task.consume", Module: "ai", Name: "Consume AI tasks", Description: "Allows workers to consume asynchronous AI work."},
	{Code: "notification.system.publish", Module: "notification", Name: "Publish system notifications", Description: "Allows publishing a system notification to users."},
	{Code: "notification.translate", Module: "notification", Name: "Translate notifications", Description: "Allows AI translation of received notifications."},
	{Code: "content.review", Module: "review", Name: "Review content", Description: "Allows reviewing content changes, files and reports."},
	{Code: "content.write", Module: "content", Name: "Manage content", Description: "Allows creating and editing site content."},
	{Code: "content.no-review", Module: "content", Name: "Bypass content review", Description: "Allows catalog and localized content changes to publish without review."},
	{Code: "content.translate", Module: "content", Name: "Translate content", Description: "Allows requesting quota-backed AI translations for unsupported content languages."},

	{Code: "project.create", Module: "project", Name: "Create projects", Description: "Allows creating mods and other projects."},
	{Code: "project.edit", Module: "project", Name: "Edit projects", Description: "Allows editing project metadata."},
	{Code: "project.review", Module: "project", Name: "Review projects", Description: "Allows reviewing project changes."},
	{Code: "project.no-review", Module: "project", Name: "Bypass project review", Description: "Allows project changes to publish without review."},
	{Code: "project.edit.<projectID>", Module: "project", Name: "Project edit template", Description: "Variable permission; replace <projectID> with a project unique ID."},
	{Code: "project.review.<projectID>", Module: "project", Name: "Project review template", Description: "Variable permission; replace <projectID> with a project unique ID."},
	{Code: "project.delete.<projectID>", Module: "project", Name: "Project delete template", Description: "Variable permission; replace <projectID> with a project unique ID."},
	{Code: "project.no-review.<projectID>", Module: "project", Name: "Project review bypass template", Description: "Variable permission; replace <projectID> with a project unique ID."},
	{Code: "project.download.upload.<projectID>", Module: "project", Name: "Project file upload template", Description: "Variable permission; replace <projectID> with a project unique ID to upload and manage on-site download files."},
}

var seedUsers = []seedUser{
	{
		Username:    "admin",
		Email:       "admin@mcmods.cn",
		DisplayName: "Administrator",
		Status:      "active",
		Permissions: []string{
			"admin.*", "admin.access", "admin.config.read", "admin.config.write",
			"user.read", "user.write", "permission.read", "permission.write",
			"creator.create", "creator.edit", "creator.claim", "creator.claim.review", "creator.role.write",
			"activity.read", "economy.read", "economy.write", "economy.checkin", "economy.transfer",
			"shop.read", "shop.write", "shop.purchase", "shop.use",
			"shop.profile_background.purchase", "shop.profile_background.use",
			"level.read", "level.write", "task.read", "task.write",
			"mail.read", "mail.write", "oss.read", "oss.write", "log.read", "log.write",
			"ai.read", "ai.write", "ai.task.enqueue", "ai.task.consume",
			"notification.system.publish", "notification.translate",
			"content.review", "content.write", "content.no-review", "content.translate",
			"project.create", "project.edit", "project.review",
		},
	},
	{
		Username:    "guest",
		Email:       "guest@mcmods.cn",
		DisplayName: "Guest",
		Status:      "active",
	},
	{
		Username:    "deleted_user",
		Email:       "deleted-user@mcmods.cn",
		DisplayName: "Deleted User",
		Status:      "deleted",
	},
}

func SeedRBAC(ctx context.Context, db *pgxpool.Pool) error {
	for _, permission := range seedPermissions {
		_, err := db.Exec(
			ctx,
			`insert into permissions (code, module, name, description)
			 values ($1, $2, $3, $4)
			 on conflict (code) do update
			 set module = excluded.module, name = excluded.name, description = excluded.description`,
			permission.Code,
			permission.Module,
			permission.Name,
			permission.Description,
		)
		if err != nil {
			return err
		}
	}
	return seedDefaultUsers(ctx, db)
}

func seedDefaultUsers(ctx context.Context, db *pgxpool.Pool) error {
	for _, user := range seedUsers {
		passwordHash, updatePassword, err := seedPasswordHash(user.Username)
		if err != nil {
			return err
		}

		var userID int64
		err = db.QueryRow(
			ctx,
			`insert into users (username, email, display_name, password_hash, email_verified, status)
			 values ($1, $2, $3, $4, true, $5)
			 on conflict (username) do update
			 set email = excluded.email,
			     display_name = excluded.display_name,
			     status = excluded.status,
			     password_hash = case when $6 then excluded.password_hash else users.password_hash end,
			     updated_at = now()
			 returning id`,
			user.Username,
			user.Email,
			user.DisplayName,
			passwordHash,
			user.Status,
			updatePassword,
		).Scan(&userID)
		if err != nil {
			return err
		}

		for _, permission := range user.Permissions {
			_, err = db.Exec(
				ctx,
				`insert into user_permissions (user_id, permission_id, allow, updated_at)
				 select $1, id, true, now() from permissions where code = $2
				 on conflict (user_id, permission_id) do update
				 set allow = true, updated_at = now()`,
				userID,
				permission,
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func seedPasswordHash(username string) (string, bool, error) {
	if username == "admin" {
		if password := strings.TrimSpace(os.Getenv("SEED_ADMIN_PASSWORD")); password != "" {
			hash, err := security.HashPassword(password)
			return hash, true, err
		}
	}
	return "password-login-disabled", false, nil
}
