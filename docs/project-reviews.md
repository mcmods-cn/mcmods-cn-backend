# 待审项目预览

审核队列的理由与历史元数据不能代替待审内容。审核页可按需读取
`GET /api/v1/content-revisions/{revisionId}`；原有审核 `PATCH` 和 Mod 修订历史、比较接口保持兼容。

此接口只处理 `pending` 的 Mod、整合包及 plugin、map、resource_pack、shader_pack、datapack、addon
资料修订，以及 `project_changelog` 的待审更新日志。服务端核对修订、审核请求的实体身份、聚合类型/键，以及真实项目和公开路由仍存在。
调用者必须登录且能对该项目执行 `canReviewProjectSubmission`：全站 `project.review`、`content.review` 与管理权限沿用原有可自审政策，
项目范围审核员必须拥有精确 `project.review.<projectId>`，并且不能审核自己的提交。
普通用户、编辑权限本身、其他项目的审核权限不能读取此接口；普通私有项目详情的边界不扩大。

响应 `data` 为 `{id, entityType, projectId, status, snapshot}`。`snapshot` 按对应项目的提交 DTO
白名单解析，不返回未知 JSON 字段、内部数据库 ID、任务配置、审核 metadata 或凭据。
返回 `Cache-Control: private, no-store`。正文、链接及其他用户内容仍是不可信数据，客户端必须
作为文本或经现有安全渲染器处理，不直接注入 HTML。

更新日志响应的 `projectId` 是日志所归属项目的公开 ID，权限不会错误地绑定到日志 ID。
`snapshot` 包含拟审的版本、时间、Minecraft 版本、分类与各语言正文，而不是已发表的旧正文。
读取预览不写入 `project_changelog_localizations`；已删除日志、关闭审核请求和身份不一致返回 404。

站内主图标除活跃/扫描通过/栅格图片检查外，还要求该不可变修订的作者等于文件上传者、
快照引用确实匹配该文件，并核对读取者的目标项目权限。管理员也不能仅凭管理员身份给任意
私有文件签名。非法或已删除图标返回空字符串，可展示默认图标；外部 HTTP(S) 图标沿用原 URL。
Mod 修订历史、比较和提交/审核响应也采用相同约束。签名只修改响应副本，数据库快照、哈希和
比较的语义内容不变。批量读取先关闭业务查询结果，再最多执行一次普通图片授权与一次修订
授权查询；隔离 PostgreSQL 单连接回归覆盖 100 个预览输入，此结果不代表生产延迟。

错误码：400 `PROJECT_REVIEW_PREVIEW_INVALID`；404 `PROJECT_REVIEW_PREVIEW_NOT_FOUND`
（不存在、已结束、不支持或目标已不存在/不一致）；403 `PROJECT_REVIEW_PREVIEW_FORBIDDEN`；
503 `PROJECT_REVIEW_PREVIEW_UNAVAILABLE`；损坏的存储快照返回 500
`PROJECT_REVIEW_PREVIEW_INVALID_SNAPSHOT`。登录拒绝由现有认证中间件处理。
接口不执行真实 AI、对象存储下载、自动审核或生产迁移。

验证：`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run
'Test(ProjectRevisionPreviewUsesExactReviewScopeAndTypedSnapshotIntegration|StoredOSSImagesDoNotSignUnboundPrivateFilesIntegration)' -count=1`。
测试使用专用隔离 schema、合成数据和本地签名，覆盖八类项目、精确权限/自审/跨项目拒绝、
修订身份错配、上传者错配、终结任务及历史孤儿路由故障注入。正常硬删除受修订外键保护；
孤儿路由案例仅在自有可丢弃测试 schema 临时关闭用户触发器，未操作业务数据。
