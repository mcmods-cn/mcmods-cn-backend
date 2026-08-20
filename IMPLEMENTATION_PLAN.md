# MCMods 列表、统计、在线状态与日志保留实施记录

更新日期：2026-08-15

## 现有实现分析

- 前端为 Next.js 16 App Router、React 19、TypeScript，状态以页面组件状态、URL 查询参数和少量 Context 为主；统一请求入口是 `app/_lib/api.ts`。
- 后端为 Go `net/http` ServeMux、pgx/PostgreSQL，按 HTTP handler、领域服务和数据库 schema statement 分层；Redis 已用于缓存和站点 presence 聚合，NATS 用于后台任务。
- Mod、整合包、插件/地图等轻量项目、服务器、教程/新闻/问题/BUG 分属不同业务表，但都通过 `public_routes` 获得统一外部身份，并共享 `content_popularity_stats` 热度投影。
- 原始活动记录是 `user_activity_events`，动作和对象类型使用数值外键；此前没有动作级保留策略，也没有不会随原始日志清理而消失的完整用户累计统计。
- 私聊是普通 HTTP + 轮询/短期会话 presence，不是 WebSocket/SSE。站点在线状态因此采用现有 HTTP 心跳、会话表和节流写入，没有引入新的基础设施。
- 原列表已有 `catalog-list-ui.tsx`、URL 状态工具和 Minecraft 版本配置接口，但部分页面仍保留专用筛选、硬编码版本和“后端取前 100 条后前端分页”的旧路径。

## 复用与抽象方案

- 新闻、问题、教程和 BUG 统一由 `CommunityPostCatalog` 配置 `kind`，复用 `CatalogFilterPanel`、`CatalogFilterSidebar`、`CatalogMobileFilterDrawer`、`CatalogPagination`、空/错/加载状态和通用 URL 状态，不复制四套页面。
- Mod、整合包、插件、地图、材质、光影、数据包、附属和服务器共用 `MinecraftVersionPicker`；版本、类型、名称和常用版本来自 `/api/v1/minecraft/versions`。同步层集中区分正式版、快照版、预发布版、候选发布版、愚人节版和远古版本。
- 所有项目排序通过后端 `parseCatalogSort` 与 `catalogOrderSQL` 白名单构造器；热度统一为 `coalesce(heat_score,0) desc, updated_at desc, id desc`。
- 在线圆点统一由 `UserAvatar`/`OnlineStatusDot` 渲染；评论悬浮卡、用户主页和私聊列表不再各自维护状态样式。
- 高频 `view` 活动复用现有 Redis/本地缓存做 5 分钟跨实例去重，操作监控继续批量落库，避免每个重复 GET 都生成数据库事件。
- 用户统计写入集中在数据库统计触发器、内容事实触发器和 `internal/userstats` 校准服务；控制器只读取或标注领域活动。
- 日志清理集中在 `activity_retention_handlers.go` 的策略、过滤、预览、确认、执行和自动 worker，不在管理页面拼 SQL。

## 数据模型调整（schema generation 71）

- `community_posts.category`：按板块合法类别筛选，并增加 `(kind, category, review_status, published_at, id)` 部分索引。
- `users.show_online_status`：默认 `false`；`public_card_stat_slots`：固定六槽且默认全空。
- `user_presence_sessions`：以认证会话指纹为主键，支持多标签/多设备和登出失效。
- `user_activity_events.markdown_deleted_bytes`；活动动作+时间索引。
- `user_statistics_daily`、`user_statistics_totals`：原始日志之外的日/累计投影。
- `user_content_creation_facts`：保存创建、审核、删除/恢复后的持久内容事实。
- `activity_cleanup_runs`：独立保存预览、执行和自动清理审计，清理活动表时不会被连带删除。

当前仍处于 Dev 阶段，generation 71 只支持全新空库安装：所有最终表、索引和触发器在同一事务中创建，`sync_user_content_creation_fact()` 直接使用不会与表列重名的 `object_identifier`。不再保留旧 generation 的升级桥；检测到其他 generation 时应用会明确要求重置开发数据库，避免为未发布结构长期维护兼容迁移。

玩家动作摄取新增 `activity_event_outbox`：除 `view` 外的动作先持久化到 Outbox，再由多实例安全的 `FOR UPDATE SKIP LOCKED` 消费器按固定批次写入 `user_activity_events`。`view` 使用严格有界的内存队列，满载时丢弃并累计指标，不再使用无上限 overflow。动作摄取使用独立 PostgreSQL 连接池、指数退避、写入超时和管理员可观测接口。

## API 调整

- `GET /api/v1/minecraft/versions`：增加统一 `commonVersions`。
- 项目列表继续兼容既有 `version/category/sort/page|offset/limit` 参数；后端新增严格筛选和稳定热度排序。
- `GET /api/v1/community/post-categories?kind=...`：返回该板块合法类别。
- `GET /api/v1/users/me/statistics`、`GET /api/v1/users/{id}/statistics`。
- `GET /api/v1/users/{id}/card`：一次返回公开基础信息、隐私映射后的在线状态、等级摘要和六槽统计。
- 既有个人设置读取/保存增加 `showOnlineStatus`、`publicCardStatSlots` 和后端白名单选项。
- `POST /api/v1/site/presence`：会话级、两分钟写节流的心跳。
- 管理端日志保留配置、清理预览和清理执行接口；全部使用写权限、POST/PUT 和服务端条件快照。

## 数据迁移与回填

```powershell
go run ./cmd/user-statistics -batch-size 200
go run ./cmd/user-statistics -user <用户公开ID> -batch-size 200
```

回填按用户使用事务 advisory lock，使用 upsert/GREATEST 保留已累计值，可重复执行。内容事实和日统计分用户处理，不在一次长事务中锁住核心业务表。

## 实际完成状态

| 工作项 | 状态 |
| --- | --- |
| 四类社区列表统一结构、分类、URL 与后端筛选 | 完成 |
| 通用版本选择器和后端集中版本配置 | 完成 |
| Mod/整合包/轻量项目/服务器后端分页与稳定热度排序 | 完成 |
| 用户累计/日统计、内容事实、回填与统计页 | 完成 |
| 动作级自动保留和带预览/确认/审计的手动清理 | 完成 |
| 会话级在线判断、默认隐藏和统一状态圆点 | 完成 |
| 评论悬浮卡、等级摘要、六槽设置和缓存 | 完成 |
| Go 单测、静态检查、前端检查和生产构建 | 完成 |
| PostgreSQL 空库安装集成、真实 EXPLAIN | generation 71 空库安装、风控查询计划和 Outbox 并发消费已实跑；生产规模 EXPLAIN ANALYZE 仍需代表性数据 |
| 真实应用压力测试 | 已完成目录、鉴权混合和评论专项；详见 `LOAD_TEST_REPORT.md` |

## 风险与后续门禁

- Dev 数据库已重置并执行 generation 71 空库安装；仍需在带千万级代表性数据的隔离 PostgreSQL 环境执行查询分析脚本，确认真实数据分布下的计划。
- 非 `view` 动作已持久化到数据库 Outbox，可跨进程重启恢复；`view` 明确为可丢弃的 best-effort 信号。业务、安全和资金审计仍必须保留在各自业务事务/审计表中，不能只依赖通用动作流。
- 当前按动作保留周期不同，无法直接按月整分区删除；因此 generation 71 暂不强行分区。达到千万级事件前应以真实执行计划和 VACUUM/WAL 指标决定按时间+动作拆表或分区方案。
- 热度排序索引已经存在于 `content_popularity_stats`，但项目表/路由连接在具体数据分布下是否成为瓶颈必须用真实执行计划判定。
- 合并/部署前需在有 GCC 的环境执行 Go race 测试，并在授权的预发布环境完成负载门禁。
# 2026-08-15 分层反机器人与反滥用实施状态

- [x] 审计认证、权限、评论、审核、操作日志、统计、Redis、真实 IP、CSRF、数据库与压测调用链。
- [x] generation 71 新增风险事件、用户状态、限制、挑战、指纹、机器人规则和日聚合表。
- [x] 统一动作分类、多维限流、可信度、风险评分、重复/近似检测、挑战、审核与限制。
- [x] 评论接入幂等键、表单令牌、动态蜜罐、挑战恢复、冷却倒计时和 pending 提示。
- [x] 主要内容创建/编辑及审核提交接入统一保护，复用 change request 的唯一 pending 约束。
- [x] 双向 DNS 搜索爬虫验证、管理员只读机器人、独立读预算、robots.txt 与 sitemap。
- [x] 后台总览、规则、事件处置、人工限制、用户可信度 API 与 Bot 规则。
- [x] 有界异步风险事件写入、采样详情与准确日聚合。
- [x] 单元、数据库集成、并发、真实接口、安全、查询计划、前端构建及本地压力测试。

详见 `ANTI_BOT_DESIGN.md`、`ANTI_BOT_TEST_REPORT.md` 与 `ANTI_BOT_LOAD_TEST_REPORT.md`。

## 2026-08-16 审核队列细分与项目级审核

- `GET /api/v1/reviews/content` 作为登录审核者的统一内容队列入口，保留原管理员接口兼容；支持按审核类型、操作、新建/编辑/删除/导入、项目类型和关键词筛选，并提供权限过滤后的 facet 与分页。
- 队列分类覆盖模组、整合包、插件、地图、材质包、光影包、数据包、附属资源、模组资料版本/模板/分类/排列/条目、资料介绍、导入资料、全站资料目录、内容翻译、项目更新日志、蓝图、皮肤、作者/团队、教程、BUG/特性、新闻和问题/讨论。服务器继续使用已有的独立证明审核队列。
- `project_developer.[ProjectID]` 与 `project_editor.[ProjectID]` 模板包含各自的项目审核权限；开发者角色通常从已认证作者/团队关系派生，编辑员角色通常来自编辑员申请，管理员仍可在特殊情况下手工配置。
- 已认证开发者和项目编辑员只能处理权限模板允许的当前项目资料审核，不能自审，也不能访问其他项目或全站安全队列；`content.review`、`project.review` 和 `admin.*` 保留全局审核能力。
- 模组修订和通用内容修订的 PATCH 接口改为先认证、再在数据库事务中解析真实项目并执行对象级权限判断，权限不再依赖前端是否显示按钮。
- 新增审核队列查询集成测试，真实执行完整 UNION，防止某一内容表或聚合类型发生 schema 漂移后让整个审核页面报错。
