# 项目身份与权限模型

## 身份边界

MCMods 明确区分以下身份：

- **提交者（`submitted_by`）**：把项目录入本站的人，只用于提交记录、审核进度、通知和审计，不是项目权限来源。
- **个人作者**：现实中的个人开发者资料，可由站内用户提交证明并认领。
- **团队**：个人作者的集合，成员关系是 `team_id -> author_id`，团队本身不能被用户认领。
- **本站编辑员**：经某个项目的编辑员申请审核后取得，只作用于该项目，不显示为作者或团队成员。
- **已认证开发者**：用户成功认领个人作者后，由有效作者/团队项目关系动态推导。
- **管理员手工授权**：处理特殊情况的显式后台 RBAC 授权；它不是创建、提交或导入项目的副作用，必须经过后台权限校验并写权限审计日志。

管理员通过全局权限或显式手工 RBAC 管理内容。普通用户、管理员、`autobot`、爬虫、外部导入和批量任务创建项目时均不会自动得到项目角色。

## 权限取得方式

普通业务只有两条正式申请线路：

1. 在项目页申请本站编辑员；审核通过后产生 `project_editor_assignments`。
2. 在个人作者页认领作者；审核通过后，根据已审核且 `permission_granting=true` 的直接作者关系或团队关系推导开发者权限。

另外保留管理员后台的显式手工授权作为特殊情况处理入口。后台可创建/绑定 `project_editor.<ProjectID>`、`project_developer.<ProjectID>`，也可配置项目级直接权限；所有变更写入 `permission_audit_logs`，只改变 `permission_version`，不会注销 Session。

不存在项目开发者申请、项目所有者申请、项目认领或团队认领。创建、转载、导入、第一次编辑、审核通过、收藏、关注以及填写作者名称都不会自动授权。

## 编辑员

编辑员申请由 `user_id + target_route_id` 定位，同一用户同一项目最多一个待审核申请。审核通过才建立活动 assignment；重复审核幂等。编辑员可以执行角色模板明确允许的资料维护操作，但默认不能管理作者认领、团队成员、项目作者/团队授权关系、项目成员或所有权。

编辑员权限来源在 `effective_project_access` 中标记为 `editor_assignment`。撤销 assignment 后权限立即消失，Session 保持有效。

## 作者认领

`creator_claims` 只接受 `creators.kind='author'`。数据库触发器拒绝团队认领；部分唯一索引保证每个作者最多一条 `approved` 认领。一个用户可以分别认领多个作者，每个认领独立提交证据和审核。

身份认领始终先进入 `pending`，审核人必须与认领人是不同账号；`creator.claim.review` 或 `admin.*` 不允许本人审核自己的认领。本人审核返回 403 和 `CREATOR_CLAIM_INDEPENDENT_REVIEW_REQUIRED`，认领状态与派生开发者权限保持不变。该独立身份要求不改变项目或普通内容的既有全局审核规则；具体流程见 [作者认领](docs/creator-identity-claims.md)。

待审核、驳回或撤销的认领不授予权限。认领通过后，系统动态读取：

- 已审核个人作者与公开项目的可授权关系；
- 已审核个人作者的团队成员关系，以及团队与公开项目的可授权关系。

因此后续新增已审核项目关系或团队项目时会自动生效，不需要逐项目复制永久角色。

## 团队与敏感关系

团队成员保存在 `creator_team_members(team_id, member_creator_id)`，两个外键都指向 Creator；数据库触发器要求前者是团队、后者是个人作者。未被认领的成员仍可公开展示，但不产生任何站内用户权限。

项目作者关系和项目团队关系保存在 `content_creator_bindings`。只有项目、Creator、关系状态均已审核且角色定义允许授权时，才进入权限计算。普通资料编辑产生的新关系保持 `pending`；修改作者关系需要 `project.authorship.manage`，修改团队关系需要 `project.team_relation.manage`，团队成员管理另需 `team.members.manage` / `team.members.review`。审核和变更写管理员操作日志。

关系的 `permission_granting` 将现实开发者/维护者与翻译、鸣谢等展示署名分开，避免非管理型署名意外提权。

## 多来源与撤销

`effective_project_access` 为每条来源保留 `source_type`、`source_id` 和 `source_path`：

- `editor_assignment`
- `author_claim`
- `author_team_relation`

同一用户可以同时从多个来源取得同一项目权限。撤销某个作者认领、团队成员、项目关系或编辑员 assignment 只移除对应来源；其余来源继续有效。后台手工 RBAC 独立存放在 `user_role_bindings` / `user_permissions`，不会被派生关系撤销误删。

## 提交者权限

提交者可以查看自己的待审核提交、审核记录和结果，并按提交状态补充或撤回材料。项目发布后，如果没有编辑员、作者派生开发者、管理员全局权限或管理员显式手工授权，则不能编辑项目、管理版本/文件、审核资料、管理成员或删除项目。后端不使用 `created_by == current_user` 或 `submitted_by == current_user` 作为项目管理旁路。

权限组线路列表在一个数据库查询中加载线路及按 position 排序的权限组，保留空线路为 `roles: []`。列表不会在持有线路查询连接时再领取连接，因此单连接测试池仍可完成读取；升降级、过期时间、授权审计和权限版本刷新继续使用既有事务流程。
