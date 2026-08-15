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

## 数据模型调整（schema generation 69）

- `community_posts.category`：按板块合法类别筛选，并增加 `(kind, category, review_status, published_at, id)` 部分索引。
- `users.show_online_status`：默认 `false`；`public_card_stat_slots`：固定六槽且默认全空。
- `user_presence_sessions`：以认证会话指纹为主键，支持多标签/多设备和登出失效。
- `user_activity_events.markdown_deleted_bytes`；活动动作+时间索引。
- `user_statistics_daily`、`user_statistics_totals`：原始日志之外的日/累计投影。
- `user_content_creation_facts`：保存创建、审核、删除/恢复后的持久内容事实。
- `activity_cleanup_runs`：独立保存预览、执行和自动清理审计，清理活动表时不会被连带删除。

当前仍处于 Dev 阶段，generation 69 只支持全新空库安装：所有最终表、索引和触发器在同一事务中创建，`sync_user_content_creation_fact()` 直接使用不会与表列重名的 `object_identifier`。不再保留 67/68 到 69 的升级桥；检测到其他 generation 时应用会明确要求重置开发数据库，避免为未发布结构长期维护兼容迁移。

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
| PostgreSQL 空库安装集成、真实 EXPLAIN ANALYZE | generation 69 空库安装有门禁集成测试；无 psql，EXPLAIN ANALYZE 未执行 |
| 真实应用压力测试 | 已完成目录、鉴权混合和评论专项；详见 `LOAD_TEST_REPORT.md` |

## 风险与后续门禁

- 合并前应重置 Dev 数据库并执行 generation 69 空库安装；在带代表性测试数据的隔离 PostgreSQL 环境继续执行校准命令和 `scripts/query-analysis.sql`，确认实际数据量下的计划。
- 活动 monitor 沿用原有异步批量落库模型；进程被强杀时尚未 flush 的少量活动可能延迟到业务级校准之外。内容创建事实由业务表触发器保证，不受该风险影响。
- 热度排序索引已经存在于 `content_popularity_stats`，但项目表/路由连接在具体数据分布下是否成为瓶颈必须用真实执行计划判定。
- 合并/部署前需在有 GCC 的环境执行 Go race 测试，并在授权的预发布环境完成负载门禁。
