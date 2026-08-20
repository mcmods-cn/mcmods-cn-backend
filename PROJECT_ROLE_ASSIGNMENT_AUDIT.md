# 项目权限来源审计

## 检查范围

全局检查了 `bindProjectRoleTx`、`assignRole`、`grantRole`、`user_role_bindings`、`auth_version`、`permission_version`、`created_by`、`submitted_by`、owner/developer/editor/maintainer、变量角色、Creator 认领、团队成员、作者关系及创建者比较。覆盖 Handler、Schema/触发器、种子、缓存、审核、导入/自动化、搜索、统计和前端入口。

创建入口覆盖 Mod、整合包、插件、地图、材质包、光影、数据包、附属资源、服务器、皮肤、蓝图、教程/新闻/问题/讨论的共享内容流程，以及 Modrinth/CurseForge、批量导入、早期填充、自动同步和后台代建。

## 删除的错误路径

- 删除项目创建后自动绑定开发者变量角色的调用；
- 删除 Mod、整合包和共享项目以创建者/提交者放行编辑的判断；
- 服务器由 `created_by` 表示所有者的旧例外已移除，现统一为 `submitted_by`；
- 删除只服务项目级开发者/所有者申请的旧表、接口、附件路径和前端弹窗；
- 删除把编辑员显示成作者/开发者的文案和徽标推断；
- 删除角色/直接权限变化递增 `auth_version` 的触发器；
- 删除创建流程专用的旧角色绑定辅助函数及兼容转发。

## 当前权威来源

1. `project_editor_assignments`：项目编辑员申请审核后的项目级来源；
2. `creator_claims + content_creator_bindings`：个人作者直接参与项目的开发者来源；
3. `creator_claims + creator_team_members + content_creator_bindings`：团队成员作者参与团队项目的开发者来源；
4. 管理员后台显式 `user_role_bindings` / `user_permissions`：应对特殊情况的手工来源。

前三类由 `effective_project_access` 动态派生，保留独立 `source_type/source_path`，不会复制成永久用户角色。第四类保留现有后台配置能力，受后台权限中间件保护并写 `permission_audit_logs`；它不是任何项目创建流程的自动动作。

## 作者与团队关系

项目页只保留编辑员申请。个人作者页提供认领入口；团队页没有认领入口。一个作者最多一个已通过用户，但一个用户可以认领多个作者。团队成员指向作者实体，不指向用户。

新项目作者/团队关系默认待审核；只有项目公开、Creator 已审核、关系已审核且角色 `permission_granting=true` 时才授予开发者权限。普通编辑员不能管理这些敏感授权关系。

## 权限旁路结论

公开目录项目的顶层提交字段使用 `submitted_by`。当前项目编辑判断只读取权限规则，不使用提交者身份。`created_by` 只保留在真正表示记录创建者、日志发起者或用户自有资产所有者的表中，没有机械全局改名。
