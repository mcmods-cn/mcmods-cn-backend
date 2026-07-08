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
	{Code: "admin.*", Module: "admin", Name: "后台全部权限", Description: "允许访问和管理所有后台功能"},
	{Code: "admin.access", Module: "admin", Name: "访问后台", Description: "允许进入后台管理界面"},
	{Code: "admin.config.read", Module: "settings", Name: "读取系统配置", Description: "查看系统、登录、邮件、OSS 等配置"},
	{Code: "admin.config.write", Module: "settings", Name: "修改系统配置", Description: "修改系统、登录、邮件、OSS 等配置"},
	{Code: "user.read", Module: "user", Name: "查看用户", Description: "查看用户列表和登录记录"},
	{Code: "user.write", Module: "user", Name: "管理用户", Description: "修改用户状态、权限和安全策略"},
	{Code: "permission.read", Module: "permission", Name: "查看权限", Description: "查看权限组、权限节点和授权关系"},
	{Code: "permission.write", Module: "permission", Name: "管理权限", Description: "调整权限组、用户权限和临时授权"},
	{Code: "mail.read", Module: "mail", Name: "查看邮件配置", Description: "查看邮件服务配置和模板"},
	{Code: "mail.write", Module: "mail", Name: "管理邮件配置", Description: "修改 SMTP、模板并发送测试邮件"},
	{Code: "oss.read", Module: "oss", Name: "查看 OSS", Description: "查看 OSS 配置、文件目录、上传记录、查杀记录和下载统计"},
	{Code: "oss.write", Module: "oss", Name: "管理 OSS", Description: "修改 OSS 配置并上传、管理私有桶文件"},
	{Code: "log.read", Module: "log", Name: "查看日志", Description: "查看系统运行、用户交互、后台操作、安全登录、API 访问等日志"},
	{Code: "log.write", Module: "log", Name: "管理日志", Description: "修改日志保留周期并执行滚动清理"},
	{Code: "content.review", Module: "review", Name: "内容审核", Description: "处理内容、文件、举报和申诉审核"},
	{Code: "content.write", Module: "content", Name: "内容管理", Description: "创建、编辑、归档站内内容"},
	{Code: "user.file.daily_limit.<num>", Module: "user", Name: "每日上传限制", Description: "数值权限：每日上传文件总量，授权时将 <num> 替换为具体数值"},
	{Code: "project.create", Module: "project", Name: "创建项目", Description: "创建 Mod、插件、整合包等项目"},
	{Code: "project.edit", Module: "project", Name: "编辑项目", Description: "编辑项目基础资料"},
	{Code: "project.review", Module: "project", Name: "审核项目", Description: "审核项目内容"},
	{Code: "project.no_review", Module: "project", Name: "项目免审", Description: "项目级免审核权限"},
	{Code: "project.edit.<projectID>", Module: "project", Name: "项目编辑模板", Description: "变量权限：编辑指定项目，授权时将 <projectID> 替换为具体项目 ID"},
	{Code: "project.review.<projectID>", Module: "project", Name: "项目审核模板", Description: "变量权限：审核指定项目，授权时将 <projectID> 替换为具体项目 ID"},
	{Code: "project.delete.<projectID>", Module: "project", Name: "项目删除模板", Description: "变量权限：删除指定项目，授权时将 <projectID> 替换为具体项目 ID"},
	{Code: "project.no_review.<projectID>", Module: "project", Name: "项目免审模板", Description: "变量权限：指定项目免审核，授权时将 <projectID> 替换为具体项目 ID"},
}

var seedUsers = []seedUser{
	{
		Username:    "admin",
		Email:       "admin@mcmods.cn",
		DisplayName: "管理员",
		Status:      "active",
		Permissions: []string{
			"admin.*", "admin.access", "admin.config.read", "admin.config.write",
			"user.read", "user.write", "permission.read", "permission.write",
			"mail.read", "mail.write", "oss.read", "oss.write", "log.read", "log.write",
			"content.review", "content.write",
		},
	},
	{
		Username:    "guest",
		Email:       "guest@mcmods.cn",
		DisplayName: "游客",
		Status:      "active",
	},
	{
		Username:    "deleted_user",
		Email:       "deleted-user@mcmods.cn",
		DisplayName: "注销的用户",
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
