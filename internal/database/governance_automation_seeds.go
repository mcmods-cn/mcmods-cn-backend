package database

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedPermissionAccessType(permission seedPermission) string {
	if permission.AccessType != "" {
		return permission.AccessType
	}
	code := strings.ToLower(permission.Code)
	switch {
	case strings.Contains(code, "write"), strings.Contains(code, "create"), strings.Contains(code, "edit"), strings.Contains(code, "delete"), strings.Contains(code, "publish"), strings.Contains(code, "upload"), strings.Contains(code, "run"):
		return "write"
	case strings.Contains(code, "review"), strings.Contains(code, "moderate"):
		return "moderation"
	case strings.HasPrefix(code, "admin."):
		return "administration"
	case strings.HasPrefix(code, "security."):
		return "security"
	default:
		return "read"
	}
}

func seedGovernanceAutomationDefaults(ctx context.Context, db *pgxpool.Pool) error {
	if _, err := db.Exec(ctx, `insert into ban_reasons(code,translations,sort_order) values
		('alt_accounts','{"zh-CN":"批量注册的小号","en-US":"Mass-created alternate accounts"}',10),
		('advertising_bot','{"zh-CN":"广告机","en-US":"Advertising bot"}',20),
		('spam_bot','{"zh-CN":"垃圾信息机器人","en-US":"Spam bot"}',30),
		('malicious_spam','{"zh-CN":"恶意灌水","en-US":"Malicious flooding"}',40),
		('harassment','{"zh-CN":"多次人身攻击","en-US":"Repeated harassment"}',50),
		('fraud','{"zh-CN":"欺诈或钓鱼","en-US":"Fraud or phishing"}',60),
		('malware','{"zh-CN":"发布恶意文件","en-US":"Malicious files"}',70),
		('impersonation','{"zh-CN":"冒充他人","en-US":"Impersonation"}',80),
		('copyright','{"zh-CN":"多次侵权或抄袭","en-US":"Repeated infringement"}',90),
		('malicious_reports','{"zh-CN":"恶意举报","en-US":"Malicious reporting"}',100),
		('ban_evasion','{"zh-CN":"绕过此前封禁","en-US":"Ban evasion"}',110),
		('other','{"zh-CN":"其他","en-US":"Other"}',999)
		on conflict(code) do nothing`); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `insert into site_pages(code,status,published_revision) values('about','published',1) on conflict(code) do nothing`); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `insert into site_page_translations(page_id,locale,title,body_markdown,status)
		select page.id,translation.locale,translation.title,translation.body,'published'
		from site_pages page cross join (values
			('zh-CN','关于本站',E'# 关于 MCMods\n\n这里将介绍本站、社区规则与维护团队。'),
			('zh-TW','關於本站',E'# 關於 MCMods\n\n這裡將介紹本站、社群規則與維護團隊。'),
			('en-US','About MCMods',E'# About MCMods\n\nThis page introduces the site, community rules, and maintainers.'),
			('ja-JP','MCMods について',E'# MCMods について\n\nサイト、コミュニティルール、運営チームを紹介します。'),
			('de-DE','Über MCMods',E'# Über MCMods\n\nInformationen über die Website, Community-Regeln und das Team.'),
			('fr-FR','À propos de MCMods',E'# À propos de MCMods\n\nPrésentation du site, des règles communautaires et de l’équipe.'),
			('es-ES','Acerca de MCMods',E'# Acerca de MCMods\n\nInformación del sitio, las reglas y el equipo responsable.'),
			('ru-RU','О MCMods',E'# О MCMods\n\nИнформация о сайте, правилах сообщества и команде поддержки.')
		) translation(locale,title,body) where page.code='about'
		on conflict(page_id,locale) do update
		set body_markdown=excluded.body_markdown,revision=site_page_translations.revision+1,updated_at=now()
		where site_page_translations.revision=1 and site_page_translations.updated_by is null
		  and site_page_translations.status='published' and site_page_translations.title=excluded.title
		  and site_page_translations.body_markdown=replace(excluded.body_markdown,E'\n',E'\\n')`); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `insert into seed_crawler_configs(id) values(true) on conflict(id) do nothing`); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `insert into license_policies(spdx_id,redistribution_allowed,notes) values
		('MIT',true,'OSI-approved permissive license'),('Apache-2.0',true,'OSI-approved permissive license'),
		('BSD-2-Clause',true,'OSI-approved permissive license'),('BSD-3-Clause',true,'OSI-approved permissive license'),
		('GPL-2.0-only',true,'Copyleft redistribution allowed'),('GPL-3.0-only',true,'Copyleft redistribution allowed'),
		('LGPL-2.1-only',true,'Library copyleft redistribution allowed'),('LGPL-3.0-only',true,'Library copyleft redistribution allowed'),
		('ARR',false,'All rights reserved'),('UNKNOWN',false,'Unknown or custom license')
		on conflict(spdx_id) do nothing`)
	return err
}
