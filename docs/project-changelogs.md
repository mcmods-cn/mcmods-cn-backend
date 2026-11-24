# 项目更新日志与分类分页

`GET /api/v1/changelogs?targetType=mod&targetId=<公开ID>&locale=en-US` 返回项目更新日志
的现有 cursor 分页。首个条目页仍包含 `categories`，每页最多 100 个分类，另返回
`categoriesHasMore` 和 `categoriesNextCursor`。后续条目页不重复加载分类。
100 是单页读取预算，不是项目允许创建分类的总量；已有第 101 个及以后分类仍可使用。

条目列表接受可选的 `categoryId`（九位公开 ID）。筛选在翻页前生效，使用同一项目内生效且已审核的条目。有效但不属于该项目的分类返回空页，格式错误或换筛选后继续使用旧游标返回 400。分类短链转到父项目当前地址并打开 `?tab=changelog&categoryId=<公开ID>#changelog`。修改父项目的 slug 后，原短链仍能打开同一分类。

更多分类由 `GET /api/v1/changelogs/categories` 按需读取，查询参数为 `targetType`、
`targetId`、`locale` 和可选 `cursor`。响应 `data` 包含 `target`、`categories`、
`limit: 100`、`hasMore` 和 `nextCursor`。分类字段与原数组相同，按创建时间与内部 ID
稳定升序排列；不返回内部 ID。游标绑定目标项目、界面语言与分页类型，跨目标/语言及
无效游标返回 400。删除上一页的末项不使游标失效。

两个读取入口沿用现有目标解析权限：仅已批准的项目提供分类与更新日志集合；登录或编辑
权限不会让未批准目标变成公开可读。编辑界面按需加载更多分类，当前条目已经选中的分类
即使不在首个分类页，也保留名称和值；加载失败保留草稿，并允许重试。

分类分页沿用现有安装定义。查询先取最多 101 个分类，再仅加载
每项最多 9 条语言记录检查现有 8 语言不变量，避免整体列表和译文聚合无界增长。
更新失败的错误分类见 [查找错误说明](changelog-update-errors.md)。

## 上游同步与人工编辑

绑定 Modrinth 或 GitHub 来源，以及读取公开文件、版本和发行日志时，服务使用匿名供应商请求。Modrinth 支持已批准、归档和不进入目录的公开项目；可展示的版本状态为 `listed`、`archived` 和 `unlisted`。GitHub 只同步非草稿发行，包括已发布的预发行版。CurseForge 的 API 仍要求已配置供应商密钥。[Modrinth v2 数据定义](https://github.com/modrinth/code/blob/1cb7a7425fabc6e99c43f7c9fd23422db3293756/apps/labrinth/src/models/v2/projects.rs) · [GitHub 发行列表](https://docs.github.com/en/rest/releases/releases#list-releases)

手工创建和编辑共用提交流程。事务先共享锁定已发布的父项目，再锁定日志修订与条目。获得这些锁后重新核对当前会话、项目编辑权和免审权，再登记修订、活动和发布事件。审核通过也要求父项目仍已发布、条目仍活动。隐藏项目的待审日志可以驳回。分类名称上限为 80 个 Unicode 码点，版本名称 120 个，原因 500 个，每种语言正文 200,000 个。

手工编辑和上游同步共用修订聚合锁。同步在取得锁后读取条目、当前已发布修订和人工覆盖标志，再决定是否写入。待审修订沿既有审批流程处理，人工覆盖保留。[自动更新与来源](../PROJECT_AUTO_UPDATE_DESIGN.md)

generation 171 按项目、供应商和发行 ID 约束外部日志记录。外部来源换绑后，原项目的已发布记录仍归原项目。新项目可以同步同一个外部发行版本，重复同步在本项目内去重。[项目范围修复与验证](governance.md#2026-11-23-第三轮接续)
