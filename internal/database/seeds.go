package database

import (
	"context"
	"encoding/json"
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
	AccessType  string
}

type seedUser struct {
	Username    string
	Email       string
	Status      string
	Permissions []string
}

type seedRole struct {
	Code        string
	Name        string
	Description string
	Weight      int
	Permissions []string
	Denials     []string
}

var seedPermissions = []seedPermission{
	{Code: "*", Module: "security", Name: "All permissions", Description: "Internal wildcard used by fail-closed security roles.", AccessType: "security"},
	{Code: "admin.*", Module: "admin", Name: "All administration permissions", Description: "Grants access to every administration capability."},
	{Code: "admin.access", Module: "admin", Name: "Access administration", Description: "Allows access to the administration console."},
	{Code: "admin.config.read", Module: "settings", Name: "Read system configuration", Description: "Allows reading system configuration."},
	{Code: "admin.config.write", Module: "settings", Name: "Write system configuration", Description: "Allows modifying system configuration."},
	{Code: "security.anti-abuse.read", Module: "security", Name: "Read anti-abuse events", Description: "Allows viewing anti-abuse summaries, rules and redacted events."},
	{Code: "security.anti-abuse.write", Module: "security", Name: "Manage anti-abuse controls", Description: "Allows changing anti-abuse rules and restrictions."},
	{Code: "security.anti-abuse.sensitive", Module: "security", Name: "Read sensitive anti-abuse identifiers", Description: "Allows viewing complete hashed network and device identifiers."},
	{Code: "security.anti-abuse.rate_multiplier.<num>", Module: "security", Name: "Anti-abuse rate limit allowance", Description: "Numeric permission; replace <num> with the percentage of the normal request-count allowance."},
	{Code: "security.anti-abuse.rate_multiplier.100", Module: "security", Name: "Anti-abuse rate limit allowance: 100%", Description: "Default request-count allowance for registered users."},
	{Code: "security.anti-abuse.rate_multiplier.review_submit.<num>", Module: "security", Name: "Review submission rate limit allowance", Description: "Numeric permission; replace <num> with the percentage allowance for review.submit requests."},

	{Code: "user.read", Module: "user", Name: "Read users", Description: "Allows reading user accounts and login information."},
	{Code: "user.write", Module: "user", Name: "Manage users", Description: "Allows modifying user accounts, status and security settings."},
	{Code: "user.follow.create", Module: "user", Name: "Follow users", Description: "Allows following another user."},
	{Code: "user.follow.receive", Module: "user", Name: "Receive followers", Description: "Allows the account to be followed."},
	{Code: "user.message.send", Module: "user", Name: "Send direct messages", Description: "Allows sending direct messages."},
	{Code: "user.message.receive", Module: "user", Name: "Receive direct messages", Description: "Allows receiving direct messages."},
	{Code: "user.avatar.update", Module: "user", Name: "Update avatar", Description: "Allows changing the account avatar."},
	{Code: "user.avatar.animated", Module: "user", Name: "Use animated avatar", Description: "Allows GIF or APNG avatars."},
	{Code: "user.ai.daily_token_limit.<num>", Module: "user", Name: "Daily AI token limit", Description: "Numeric permission; replace <num> with the daily token allowance."},
	{Code: "user.ai.daily_token_limit.20000", Module: "user", Name: "Daily AI token limit: 20,000", Description: "Default daily AI translation allowance for registered users."},
	{Code: "user.file.daily_limit.<num>", Module: "user", Name: "Daily upload limit", Description: "Numeric permission; replace <num> with bytes allowed per day."},
	{Code: "user.file.total_limit.<num>", Module: "user", Name: "Total upload limit", Description: "Numeric permission; replace <num> with total stored bytes."},
	{Code: "user.file.single_limit.<num>", Module: "user", Name: "Single file limit", Description: "Numeric permission; replace <num> with maximum bytes per file."},
	{Code: "user.draft.retention_seconds.<num>", Module: "user", Name: "Draft retention period", Description: "Numeric permission; replace <num> with the number of seconds that each autosaved draft remains available."},
	{Code: "user.draft.retention_seconds.2592000", Module: "user", Name: "Draft retention: 30 days", Description: "Keeps registered users' autosaved drafts for 2,592,000 seconds."},

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
	{Code: "economy.balance.write", Module: "economy", Name: "Adjust user balances", Description: "Allows changing a user's currency balance while recording an immutable ledger entry."},
	{Code: "economy.checkin", Module: "economy", Name: "Check in", Description: "Allows claiming the configured daily check-in reward."},
	{Code: "economy.transfer", Module: "economy", Name: "Transfer currency", Description: "Allows transferring currency to another user."},

	{Code: "shop.read", Module: "shop", Name: "Read shop settings", Description: "Allows reading all shop items and configuration."},
	{Code: "shop.write", Module: "shop", Name: "Manage shop settings", Description: "Allows creating and modifying shop items."},
	{Code: "shop.purchase", Module: "shop", Name: "Purchase shop items", Description: "Allows purchasing an item from the site shop."},
	{Code: "shop.use", Module: "shop", Name: "Use shop items", Description: "Allows using an owned shop item."},
	{Code: "shop.profile_background.purchase", Module: "shop", Name: "Purchase profile background", Description: "Allows purchasing the profile background item."},
	{Code: "shop.profile_background.use", Module: "shop", Name: "Use profile background", Description: "Allows applying a custom profile background."},
	{Code: "shop.project_heat_boost.purchase", Module: "shop", Name: "Purchase project heat boost", Description: "Allows purchasing a project heat boost item."},
	{Code: "shop.project_heat_boost.use", Module: "shop", Name: "Use project heat boost", Description: "Allows applying an owned heat boost to an editable project."},
	{Code: "shop.server_heat_boost.purchase", Module: "shop", Name: "Purchase server heat boost", Description: "Allows purchasing a server heat boost item."},
	{Code: "shop.server_heat_boost.use", Module: "shop", Name: "Use server heat boost", Description: "Allows applying an owned heat boost to an editable server."},

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
	{Code: "global_resource.list", Module: "content", Name: "List global resources", Description: "Allows browsing the administration-only global resource directory."},
	{Code: "global_resource.view", Module: "content", Name: "View global resources", Description: "Allows reading administration-only global resource technical details."},
	{Code: "global_resource.binding.view", Module: "content", Name: "View global resource bindings", Description: "Allows reading the projects and data versions bound to a global resource."},
	{Code: "log_share.list", Module: "log_share", Name: "List log shares", Description: "Allows administrators to list sanitized log shares."},
	{Code: "log_share.view", Module: "log_share", Name: "View log shares", Description: "Allows administrators to inspect sanitized log-share metadata and content."},
	{Code: "log_share.moderate", Module: "log_share", Name: "Moderate log shares", Description: "Allows administrators to moderate sanitized log shares."},
	{Code: "log_share.delete", Module: "log_share", Name: "Delete log shares", Description: "Allows administrators to invalidate another user's log share."},
	{Code: "log_share.redaction_reprocess", Module: "log_share", Name: "Reprocess log redaction", Description: "Allows administrators to request redaction with the current rule version."},
	{Code: "rating.create", Module: "rating", Name: "Rate projects", Description: "Allows submitting, updating and removing the account's own project and server ratings."},
	{Code: "rating.read", Module: "rating", Name: "Read rating details", Description: "Allows opening the individual ratings and messages submitted by other users."},
	{Code: "community.tutorial.create", Module: "community", Name: "Publish tutorials", Description: "Allows submitting tutorials."},
	{Code: "community.issue.create", Module: "community", Name: "Publish issue reports", Description: "Allows submitting bug and feature reports."},
	{Code: "community.news.create", Module: "community", Name: "Publish news", Description: "Allows submitting news articles."},
	{Code: "community.discussion.create", Module: "community", Name: "Publish questions", Description: "Allows submitting questions and discussion posts."},
	{Code: "community.edit", Module: "community", Name: "Edit community posts", Description: "Allows editing all community publications."},
	{Code: "community.no-review", Module: "community", Name: "Bypass community review", Description: "Allows publishing community posts without review."},
	{Code: "modpack.create", Module: "modpack", Name: "Submit modpacks", Description: "Allows creating modpack projects."},
	{Code: "reference.unresolved.read", Module: "content", Name: "Read unresolved references", Description: "Allows viewing the site-wide list of referenced projects and resources that have not been collected yet."},

	{Code: "server.create", Module: "server", Name: "Submit servers", Description: "Allows submitting a reachable Minecraft server for review."},
	{Code: "server.create.no-review", Module: "server", Name: "Publish servers without review", Description: "Allows submitted Minecraft servers to be published immediately."},
	{Code: "server.review", Module: "server", Name: "Review servers", Description: "Allows reviewing server submissions and their proof files."},
	{Code: "server.edit.<serverID>", Module: "server", Name: "Edit one server", Description: "Variable permission; replace <serverID> with a server public ID."},

	{Code: "project.create", Module: "project", Name: "Create projects", Description: "Allows creating mods and other projects."},
	{Code: "project.create.plugin", Module: "project", Name: "Submit plugins", Description: "Allows creating plugin projects."},
	{Code: "project.create.map", Module: "project", Name: "Submit maps", Description: "Allows creating map projects."},
	{Code: "project.create.resource_pack", Module: "project", Name: "Submit resource packs", Description: "Allows creating resource-pack projects."},
	{Code: "project.create.shader_pack", Module: "project", Name: "Submit shader packs", Description: "Allows creating shader-pack projects."},
	{Code: "project.create.datapack", Module: "project", Name: "Submit data packs", Description: "Allows creating data-pack projects."},
	{Code: "project.create.addon", Module: "project", Name: "Submit add-on resources", Description: "Allows creating add-on resource projects."},
	{Code: "project.edit", Module: "project", Name: "Edit projects and project content", Description: "Allows editing every project and all content scoped to those projects."},
	{Code: "project.review", Module: "project", Name: "Review projects", Description: "Allows reviewing project changes."},
	{Code: "project.no-review", Module: "project", Name: "Bypass project review", Description: "Allows project changes to publish without review."},
	{Code: "project.edit.<projectID>", Module: "project", Name: "Edit one project and its content", Description: "Variable permission; replace <projectID> with a project unique ID to edit that project and all content scoped to it."},
	{Code: "project.review.<projectID>", Module: "project", Name: "Project review template", Description: "Variable permission; replace <projectID> with a project unique ID."},
	{Code: "project.delete.<projectID>", Module: "project", Name: "Project delete template", Description: "Variable permission; replace <projectID> with a project unique ID."},
	{Code: "project.no-review.<projectID>", Module: "project", Name: "Project review bypass template", Description: "Variable permission; replace <projectID> with a project unique ID."},
	{Code: "project.download.upload.<projectID>", Module: "project", Name: "Project file upload template", Description: "Variable permission; replace <projectID> with a project unique ID to upload and manage on-site download files."},

	{Code: "comment.create", Module: "comment", Name: "Publish comments", Description: "Allows publishing comments and replies."},
	{Code: "comment.edit.own", Module: "comment", Name: "Edit own comments", Description: "Allows editing comments authored by the current user."},
	{Code: "comment.delete.own", Module: "comment", Name: "Delete own comments", Description: "Allows deleting comments authored by the current user."},
	{Code: "comment.react", Module: "comment", Name: "React to comments", Description: "Allows adding and removing reactions on comments."},
	{Code: "report.create", Module: "moderation", Name: "Create reports", Description: "Allows reporting supported site content, comments and profiles.", AccessType: "write"},
	{Code: "report.view_own", Module: "moderation", Name: "View own reports", Description: "Allows reading reports created by the current user."},
	{Code: "report.review", Module: "moderation", Name: "Review reports", Description: "Allows claiming and resolving the report queue.", AccessType: "moderation"},
	{Code: "report.snapshot.view", Module: "moderation", Name: "View report snapshots", Description: "Allows viewing immutable reported-content snapshots.", AccessType: "moderation"},
	{Code: "report.evidence.view", Module: "moderation", Name: "View report evidence", Description: "Allows issuing short-lived access to private evidence.", AccessType: "security"},
	{Code: "report.action.delete", Module: "moderation", Name: "Delete reported content", Description: "Allows applying the normal deletion flow from a report.", AccessType: "moderation"},
	{Code: "report.action.ban", Module: "moderation", Name: "Ban reported users", Description: "Allows creating ban records from a valid report.", AccessType: "moderation"},
	{Code: "report.action.reopen", Module: "moderation", Name: "Reopen reports", Description: "Allows reopening a resolved report.", AccessType: "moderation"},
	{Code: "ban.create", Module: "moderation", Name: "Create bans", Description: "Allows temporary and permanent account bans.", AccessType: "moderation"},
	{Code: "ban.revoke", Module: "moderation", Name: "Revoke bans", Description: "Allows ending an active account ban.", AccessType: "moderation"},
	{Code: "ban.view_internal", Module: "moderation", Name: "View internal ban notes", Description: "Allows reading non-public moderation notes.", AccessType: "security"},
	{Code: "account.banned", Module: "security", Name: "Account is banned", Description: "Marker permission used by the mutation firewall.", AccessType: "security"},
	{Code: "site_affairs.about.manage", Module: "site_affairs", Name: "Manage about page", Description: "Allows editing localized about-site content.", AccessType: "administration"},
	{Code: "site_affairs.changelog.manage", Module: "site_affairs", Name: "Manage site changelog", Description: "Allows publishing localized site changelog entries.", AccessType: "administration"},
	{Code: "seed_crawler.view", Module: "automation", Name: "View seed crawler", Description: "Allows viewing crawler configuration and runs."},
	{Code: "seed_crawler.configure", Module: "automation", Name: "Configure seed crawler", Description: "Allows changing seed crawler policy.", AccessType: "administration"},
	{Code: "seed_crawler.run", Module: "automation", Name: "Run seed crawler", Description: "Allows dry-running or scheduling the seed crawler.", AccessType: "administration"},
	{Code: "project.auto_update.view", Module: "automation", Name: "View project auto update", Description: "Allows viewing update settings and runs."},
	{Code: "project.auto_update.configure", Module: "automation", Name: "Configure project auto update", Description: "Allows configuring updates for an authorized project.", AccessType: "write"},
	{Code: "project.auto_update.run", Module: "automation", Name: "Run project auto update", Description: "Allows checking an authorized project immediately.", AccessType: "write"},
	{Code: "project.auto_update.view_logs", Module: "automation", Name: "View project update logs", Description: "Allows reading update execution logs."},
	{Code: "project.auto_update.redistribution_override", Module: "automation", Name: "Override redistribution policy", Description: "Allows documented license overrides.", AccessType: "administration"},
	{Code: "project.external_source.bind", Module: "automation", Name: "Bind external project source", Description: "Allows binding a verified external source.", AccessType: "write"},
	{Code: "comment.watch", Module: "comment", Name: "Watch comments", Description: "Allows watching comment branches for replies."},
	{Code: "comment.moderate", Module: "comment", Name: "Moderate all comments", Description: "Allows editing and deleting comments on every target."},
	{Code: "comment.pin", Module: "comment", Name: "Pin all comments", Description: "Allows pinning root comments on every target."},
	{Code: "project.comment.moderate.<projectID>", Module: "comment", Name: "Moderate project comments", Description: "Variable permission; replace <projectID> with a project unique ID to moderate its comments."},
	{Code: "project.comment.pin.<projectID>", Module: "comment", Name: "Pin project comments", Description: "Variable permission; replace <projectID> with a project unique ID to pin its root comments."},
	{Code: "project.comment.role.owner.<projectID>", Module: "comment", Name: "Show project owner badge", Description: "Variable permission; controls the project owner badge beside this project's comments."},
	{Code: "project.comment.role.editor.<projectID>", Module: "comment", Name: "Show project editor badge", Description: "Variable permission; controls the project editor badge beside this project's comments."},
}

var seedRoles = []seedRole{
	{
		Code: "registered", Name: "Registered user", Description: "Default permissions granted to a registered account.", Weight: 10,
		Permissions: []string{"comment.create", "comment.edit.own", "comment.delete.own", "comment.react", "comment.watch", "report.create", "report.view_own", "content.translate", "rating.create", "rating.read", "shop.read", "shop.purchase", "shop.use", "shop.project_heat_boost.purchase", "shop.project_heat_boost.use", "shop.server_heat_boost.purchase", "shop.server_heat_boost.use", "user.ai.daily_token_limit.20000", "user.draft.retention_seconds.2592000", "security.anti-abuse.rate_multiplier.100", "community.tutorial.create", "community.issue.create", "community.discussion.create", "modpack.create"},
	},
	{
		Code: "project_owner.[ProjectID]", Name: "Project owner", Description: "Default project-scoped owner permissions.", Weight: 100,
		Permissions: []string{"project.edit.<projectID>", "project.review.<projectID>", "project.comment.moderate.<projectID>", "project.comment.pin.<projectID>", "project.comment.role.owner.<projectID>"},
	},
	{
		Code: "project_editor.[ProjectID]", Name: "Project editor", Description: "Default project-scoped editor permissions.", Weight: 50,
		Permissions: []string{"project.edit.<projectID>", "project.review.<projectID>", "project.comment.role.editor.<projectID>"},
	},
	{Code: "banned", Name: "Banned account", Description: "High-priority fail-closed denial role applied without removing existing roles.", Weight: 2_000_000_000, Permissions: []string{"account.banned"}, Denials: []string{"*"}},
}

var seedUsers = []seedUser{
	{
		Username: "admin",
		Email:    "admin@mcmods.cn",
		Status:   "active",
		Permissions: []string{
			"admin.*", "admin.access", "admin.config.read", "admin.config.write",
			"user.read", "user.write", "permission.read", "permission.write",
			"creator.create", "creator.edit", "creator.claim", "creator.claim.review", "creator.role.write",
			"activity.read", "economy.read", "economy.write", "economy.balance.write", "economy.checkin", "economy.transfer",
			"shop.read", "shop.write", "shop.purchase", "shop.use",
			"shop.profile_background.purchase", "shop.profile_background.use",
			"shop.project_heat_boost.purchase", "shop.project_heat_boost.use",
			"shop.server_heat_boost.purchase", "shop.server_heat_boost.use",
			"level.read", "level.write", "task.read", "task.write",
			"mail.read", "mail.write", "oss.read", "oss.write", "log.read", "log.write",
			"ai.read", "ai.write", "ai.task.enqueue", "ai.task.consume",
			"notification.system.publish", "notification.translate",
			"content.review", "content.write", "content.no-review", "content.translate",
			"global_resource.list", "global_resource.view", "global_resource.binding.view",
			"log_share.list", "log_share.view", "log_share.moderate", "log_share.delete", "log_share.redaction_reprocess",
			"reference.unresolved.read",
			"server.create", "server.create.no-review", "server.review",
			"project.create", "project.edit", "project.review",
			"report.review", "report.snapshot.view", "report.evidence.view", "report.action.delete", "report.action.ban", "report.action.reopen",
			"ban.create", "ban.revoke", "ban.view_internal", "site_affairs.about.manage", "site_affairs.changelog.manage",
			"seed_crawler.view", "seed_crawler.configure", "seed_crawler.run",
			"project.auto_update.view", "project.auto_update.configure", "project.auto_update.run", "project.auto_update.view_logs", "project.auto_update.redistribution_override", "project.external_source.bind",
		},
	},
	{
		Username: "guest",
		Email:    "guest@mcmods.cn",
		Status:   "active",
	},
	{
		Username: "deleted_user",
		Email:    "deleted-user@mcmods.cn",
		Status:   "deleted",
	},
}

func SeedRBAC(ctx context.Context, db *pgxpool.Pool) error {
	for _, permission := range seedPermissions {
		_, err := db.Exec(
			ctx,
			`insert into permissions (code, module, name, description, access_type)
			 values ($1, $2, $3, $4, $5)
			 on conflict (code) do update
			 set module = excluded.module, name = excluded.name, description = excluded.description, access_type=excluded.access_type`,
			permission.Code,
			permission.Module,
			permission.Name,
			permission.Description,
			seedPermissionAccessType(permission),
		)
		if err != nil {
			return err
		}
	}
	if err := seedDefaultRoles(ctx, db); err != nil {
		return err
	}
	if err := seedProjectReviewPermissions(ctx, db); err != nil {
		return err
	}
	if err := seedPermissionDefaults(ctx, db); err != nil {
		return err
	}
	if err := seedGovernanceAutomationDefaults(ctx, db); err != nil {
		return err
	}
	return seedDefaultUsers(ctx, db)
}

// Existing concrete project roles may predate a newly added template
// permission. Keep those role instances aligned with the owner/editor
// templates without requiring a destructive database reset.
func seedProjectReviewPermissions(ctx context.Context, db *pgxpool.Pool) error {
	if _, err := db.Exec(ctx, `insert into permissions(code,module,name,description)
		select distinct 'project.review.' || split_part(role.code,'.',2),'project','Review one project',
		       'Project-scoped permission generated from the owner/editor role template.'
		from roles role
		where (role.code like 'project_owner.%' or role.code like 'project_editor.%')
		  and split_part(role.code,'.',2) <> ''
		  and array_length(string_to_array(role.code,'.'),1)=2
		  and position('[' in role.code)=0 and position('<' in role.code)=0
		on conflict(code) do nothing`); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `insert into role_permissions(role_id,permission_id,allow,updated_at)
		select role.id,permission.id,true,now()
		from roles role
		join permissions permission on permission.code='project.review.' || split_part(role.code,'.',2)
		where (role.code like 'project_owner.%' or role.code like 'project_editor.%')
		  and split_part(role.code,'.',2) <> ''
		  and array_length(string_to_array(role.code,'.'),1)=2
		  and position('[' in role.code)=0 and position('<' in role.code)=0
		on conflict(role_id,permission_id) do nothing`)
	return err
}

func seedDefaultRoles(ctx context.Context, db *pgxpool.Pool) error {
	for _, role := range seedRoles {
		var roleID int64
		if _, err := db.Exec(ctx, `insert into roles(code,name,description,weight,status,updated_at)
			values($1,$2,$3,$4,'active',now())
			on conflict(code) do nothing`, role.Code, role.Name, role.Description, role.Weight); err != nil {
			return err
		}
		if err := db.QueryRow(ctx, `select id from roles where code=$1`, role.Code).Scan(&roleID); err != nil {
			return err
		}
		for _, permission := range role.Permissions {
			if _, err := db.Exec(ctx, `insert into role_permissions(role_id,permission_id,allow,updated_at)
				select $1,id,true,now() from permissions where code=$2
				on conflict(role_id,permission_id) do nothing`, roleID, permission); err != nil {
				return err
			}
		}
		for _, permission := range role.Denials {
			if _, err := db.Exec(ctx, `insert into role_permissions(role_id,permission_id,allow,updated_at)
				select $1,id,false,now() from permissions where code=$2
				on conflict(role_id,permission_id) do update set allow=false,updated_at=now()`, roleID, permission); err != nil {
				return err
			}
		}
	}
	return nil
}

func seedPermissionDefaults(ctx context.Context, db *pgxpool.Pool) error {
	value, err := json.Marshal(map[string]string{
		"registeredRole": "registered",
		"developerRole":  "project_owner.[ProjectID]",
		"editorRole":     "project_editor.[ProjectID]",
	})
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `insert into system_settings(key,value,updated_at)
		values('permission.default_roles',$1::jsonb,now()) on conflict(key) do nothing`, string(value))
	return err
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
			`insert into users (username, email, password_hash, email_verified, status)
			 values ($1, $2, $3, true, $4)
			 on conflict (username) do update
			 set email = excluded.email,
			     status = excluded.status,
			     password_hash = case when $5 then excluded.password_hash else users.password_hash end,
			     updated_at = now()
			 returning id`,
			user.Username,
			user.Email,
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
