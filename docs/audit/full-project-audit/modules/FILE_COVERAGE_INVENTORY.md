# 文件覆盖清单

本清单由只读工具生成路径和行数。工具不会自动把源码标记为“已完成”；只有本轮实际进行过人工调用链审查的文件才列为已完成，其余第一方源码保持“审查中”。

## backend

| 文件 | 模块 | 行数 | 审查状态 | 主要职责 | 发现问题 |
| --- | --- | ---: | --- | --- | --- |
| `.env.example` | .env.example | 164 | 已完成 | 运行时、数据库、缓存、队列、外部服务与安全限制示例配置 | OPS-021；123个示例键均有读取方，遗漏DATABASE_URL和观察器参数 |
| `.gitignore` | .gitignore | 8 | 已完成 | 后端本地配置、构建产物和IDE目录排除 | DEAD-015 |
| `.idea/go.imports.xml` | .idea/go.imports.xml | 10 | 已完成 | GoLand导入设置 | DEAD-015 |
| `.idea/mcmods-cn-backend.iml` | .idea/mcmods-cn-backend.iml | 4 | 已完成 | GoLand模块元数据 | DEAD-015 |
| `.idea/vcs.xml` | .idea/vcs.xml | 6 | 已完成 | GoLand VCS映射 | DEAD-015 |
| `ADMIN_INFORMATION_ARCHITECTURE.md` | ADMIN_INFORMATION_ARCHITECTURE.md | 21 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `ANTI_BOT_CONFIGURATION.md` | ANTI_BOT_CONFIGURATION.md | 55 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `ANTI_BOT_DESIGN.md` | ANTI_BOT_DESIGN.md | 60 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `ANTI_BOT_LOAD_TEST_REPORT.md` | ANTI_BOT_LOAD_TEST_REPORT.md | 67 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `ANTI_BOT_TEST_REPORT.md` | ANTI_BOT_TEST_REPORT.md | 61 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `AUTH_AND_PERMISSION_VERSIONING.md` | AUTH_AND_PERMISSION_VERSIONING.md | 53 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `BACKEND_TEST_REPORT.md` | BACKEND_TEST_REPORT.md | 174 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `BAN_AND_BLACKROOM_DESIGN.md` | BAN_AND_BLACKROOM_DESIGN.md | 21 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `CACHE_KEYS_AND_INVALIDATION.md` | CACHE_KEYS_AND_INVALIDATION.md | 40 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `CODE_CLEANUP_REPORT.md` | CODE_CLEANUP_REPORT.md | 133 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `COMMENT_FLOOR_DESIGN.md` | COMMENT_FLOOR_DESIGN.md | 53 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `COMMENT_MARKDOWN_EDITOR.md` | COMMENT_MARKDOWN_EDITOR.md | 56 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `COMPATIBILITY_BOUNDARIES.md` | COMPATIBILITY_BOUNDARIES.md | 54 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `DEVELOPMENT_SCHEMA_RESET.md` | DEVELOPMENT_SCHEMA_RESET.md | 27 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `FAVORITE_MODPACK_EXPORT.md` | FAVORITE_MODPACK_EXPORT.md | 38 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `FEATURE_TEST_REPORT.md` | FEATURE_TEST_REPORT.md | 103 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `FEATURE_UPDATE_IMPLEMENTATION.md` | FEATURE_UPDATE_IMPLEMENTATION.md | 85 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `FEATURE_UPDATE_TEST_REPORT.md` | FEATURE_UPDATE_TEST_REPORT.md | 60 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `IMPLEMENTATION_PLAN.md` | IMPLEMENTATION_PLAN.md | 101 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `LOAD_TEST_REPORT.md` | LOAD_TEST_REPORT.md | 203 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `LOG_SHARE_SECURITY.md` | LOG_SHARE_SECURITY.md | 50 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `MOD_IMPORT_MODID_VALIDATION.md` | MOD_IMPORT_MODID_VALIDATION.md | 17 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `OPERATION_LOG_RETENTION.md` | OPERATION_LOG_RETENTION.md | 73 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `OUTBOX_JETSTREAM_DESIGN.md` | OUTBOX_JETSTREAM_DESIGN.md | 58 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `PRESENCE_AND_UNREAD_DESIGN.md` | PRESENCE_AND_UNREAD_DESIGN.md | 41 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `PROJECT_AUTO_UPDATE_DESIGN.md` | PROJECT_AUTO_UPDATE_DESIGN.md | 39 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `PROJECT_FOLLOW_NOTIFICATIONS.md` | PROJECT_FOLLOW_NOTIFICATIONS.md | 23 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `PROJECT_PERMISSION_MODEL.md` | PROJECT_PERMISSION_MODEL.md | 64 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `PROJECT_PERMISSION_TEST_REPORT.md` | PROJECT_PERMISSION_TEST_REPORT.md | 82 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `PROJECT_ROLE_ASSIGNMENT_AUDIT.md` | PROJECT_ROLE_ASSIGNMENT_AUDIT.md | 36 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `RECIPE_VERSION_BINDING_DESIGN.md` | RECIPE_VERSION_BINDING_DESIGN.md | 61 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `REDIS_NATS_OPTIMIZATION_PLAN.md` | REDIS_NATS_OPTIMIZATION_PLAN.md | 54 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `REDIS_NATS_TEST_REPORT.md` | REDIS_NATS_TEST_REPORT.md | 67 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `REPORT_AND_MODERATION_DESIGN.md` | REPORT_AND_MODERATION_DESIGN.md | 36 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `SEED_CRAWLER_DESIGN.md` | SEED_CRAWLER_DESIGN.md | 23 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `SESSION_INVALIDATION_FIX_REPORT.md` | SESSION_INVALIDATION_FIX_REPORT.md | 24 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `SITE_AFFAIRS_DESIGN.md` | SITE_AFFAIRS_DESIGN.md | 15 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `STICKER_SYSTEM_DESIGN.md` | STICKER_SYSTEM_DESIGN.md | 23 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `SYSTEM_NOTIFICATION_LOCALIZATION.md` | SYSTEM_NOTIFICATION_LOCALIZATION.md | 24 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `USER_BLOCKING.md` | USER_BLOCKING.md | 40 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `USER_STATISTICS_DESIGN.md` | USER_STATISTICS_DESIGN.md | 83 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `cmd/db-reset/main.go` | cmd/db-reset | 47 | 已完成 | 开发数据库显式确认重置入口 | 无新增问题；环境、库名和确认短语三重保护 |
| `cmd/load-observer/main.go` | cmd/load-observer | 53 | 已完成 | PostgreSQL活动连接定时采样与JSON输出 | OPS-021；STYLE-001 |
| `cmd/user-statistics/main.go` | cmd/user-statistics | 30 | 已完成 | 用户统计批量校准命令入口 | 无新增问题 |
| `docs/ACTIVITY_INGESTION.md` | docs/ACTIVITY_INGESTION.md | 53 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `docs/database-naming.md` | docs/database-naming.md | 42 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `docs/dual-id-architecture.md` | docs/dual-id-architecture.md | 79 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `docs/oss-object-layout.md` | docs/oss-object-layout.md | 124 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `docs/typesense-search.md` | docs/typesense-search.md | 29 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `go.mod` | go.mod | 32 | 已完成 | 后端模块、工具链与直接依赖声明 | 无新增问题 |
| `go.sum` | go.sum | 60 | 排除 | 依赖锁文件 | 依赖一致性检查覆盖 |
| `internal/activity/monitor.go` | internal/activity | 572 | 已完成 | 内存浏览事件批处理、PostgreSQL可靠活动Outbox、重试/指标与Markdown差异计算 | SEC-007 |
| `internal/activity/monitor_test.go` | internal/activity | 219 | 已完成 | 活动监控批处理、重试、关闭排空、队列满和小文本差异测试 | 未覆盖8MiB级差异输入；SEC-007；TEST-002 |
| `internal/activity/postgres_store.go` | internal/activity | 310 | 已完成 | 活动Outbox入队/并发排空、批量COPY、路由解析、站点与成长投影 | ARCH-006 |
| `internal/activity/postgres_store_integration_test.go` | internal/activity | 211 | 已完成 | 多Worker可靠活动Outbox恰好一次排空与并发负载集成测试 | 未覆盖投影失败后的重放；ARCH-006 |
| `internal/antiabuse/challenge.go` | internal/antiabuse | 162 | 已完成 | 表单Token、一次性人机挑战、算术证明与Turnstile验证 | BUG-008 |
| `internal/antiabuse/challenge_integration_test.go` | internal/antiabuse | 129 | 已完成 | 表单/挑战绑定、单次消费、并发消费和Turnstile故障降级集成 | 未注入proof metadata写入失败；BUG-008 |
| `internal/antiabuse/crawler.go` | internal/antiabuse | 233 | 已完成 | 机器人规则、搜索引擎双向DNS认证、分类限流与事件记录 | ARCH-007；规则读取行错误/缓存加载失败会部分或完全失去自定义封禁 |
| `internal/antiabuse/crawler_test.go` | internal/antiabuse | 61 | 已完成 | 搜索爬虫双向DNS、防伪后缀和未知爬虫预算测试 | 未覆盖规则数据库错误及全局DNS缓存跨分类语义；TEST-002 |
| `internal/antiabuse/normalize.go` | internal/antiabuse | 100 | 已完成 | 内容Unicode/URL/标记规范化、精确哈希、三元组SimHash与相似度 | 大文本分配风险由入口长度共同决定，继续在调用方复核 |
| `internal/antiabuse/normalize_test.go` | internal/antiabuse | 26 | 已完成 | 规范化追踪参数与近似相似度测试 | 未覆盖超长/畸形Markdown和资源上限；TEST-002 |
| `internal/antiabuse/query_plan_integration_test.go` | internal/antiabuse | 54 | 已完成 | 反滥用三类查询EXPLAIN输出集成 | 只断言计划非空，不验证索引/成本/大数据；TEST-007 |
| `internal/antiabuse/service.go` | internal/antiabuse | 854 | 已完成 | 风险评分、限流、重复内容、账号限制、事件/指纹持久化与设置 | SEC-008；ARCH-007 |
| `internal/antiabuse/service_test.go` | internal/antiabuse | 141 | 已完成 | 阈值、信任、权限限流倍率、限制分类和缓存失效测试 | 未覆盖多条并存限制与持久化失败；SEC-008；ARCH-007 |
| `internal/antiabuse/types.go` | internal/antiabuse | 127 | 已完成 | 反滥用决策、策略、评估、挑战和爬虫类型契约 | 无新增问题 |
| `internal/app/app.go` | internal/app | 68 | 已完成 | 进程日志安装、配置验证、先启动探针HTTP、运行时切换和优雅关机 | 无新增问题 |
| `internal/app/availability.go` | internal/app | 145 | 已完成 | 启动/数据库故障期间live-ready响应、CORS安全头和API流量门控 | 无新增问题；/live就绪后交由正式Handler保持liveness语义 |
| `internal/app/availability_test.go` | internal/app | 87 | 已完成 | 启动readiness、故障门控/恢复与重试退避测试 | 未覆盖并发状态切换和正式/live转发，风险较低 |
| `internal/app/runtime.go` | internal/app | 300 | 已完成 | 数据库/Schema/RBAC/缓存/NATS/Worker/Search/活动运行时装配、恢复监控与关闭 | OPS-001；PERF-004；多数Worker启动失败只降级记录，需逐Worker确认数据库扫描兜底 |
| `internal/config/activity_config_test.go` | internal/config | 64 | 已完成 | 多副本Redis、活动队列容量和生产反滥用秘密配置验证测试 | 无新增问题 |
| `internal/config/config.go` | internal/config | 666 | 已完成 | 基础设施/通用 | runtime config |
| `internal/config/database_reset_config_test.go` | internal/config | 41 | 已完成 | 开发清库精确确认、URL数据库名与生产名称拒绝测试 | 无新增问题 |
| `internal/database/activity_schema_test.go` | internal/database | 154 | 已完成 | 活动、贡献、收藏、搜索和热度DDL字符串测试 | TEST-028；公式/列名存在性不能验证行为和规模 |
| `internal/database/admin_dashboard_schema.go` | internal/database | 132 | 已完成 | 后台总览日统计、计数器、事件批量入库与索引 | BUG-055；UTC事件日期与current_date统计口径混用 |
| `internal/database/admin_dashboard_schema_test.go` | internal/database | 28 | 已完成 | 后台总览Schema关键结构测试 | TEST-027；仅做DDL字符串断言 |
| `internal/database/anti_abuse_schema.go` | internal/database | 146 | 已完成 | 反滥用状态、限制、挑战、事件、指纹、机器人规则和日汇总Schema | SEC-008；允许同账号多条有效限制但读取层只选一条 |
| `internal/database/anti_abuse_schema_test.go` | internal/database | 24 | 已完成 | 反滥用表/索引存在性与敏感字段排除静态测试 | 未验证多限制语义、触发器或约束行为；TEST-002 |
| `internal/database/autobot_seed_test.go` | internal/database | 55 | 已完成 | autobot种子与高限流/RBAC语义字符串测试 | TEST-027；未验证数据库约束和幂等执行 |
| `internal/database/catalog_editor_schema.go` | internal/database | 204 | 已完成 | 目录编辑权威层、本地化、资源定义、标签成员、配方模板/槽位/定义Schema | DB-004；recipe_layout_templates显式索引与唯一约束重复 |
| `internal/database/catalog_schema.go` | internal/database | 434 | 已完成 | 全局目录实体、导入快照、资源/标签/配方类型模板绑定候选与未解析引用Schema | DB-004；存在与唯一约束完全重复的快照索引 |
| `internal/database/changelog_schema.go` | internal/database | 107 | 已完成 | 项目更新日志/分类/多语言表、索引、公共路由和热度事件触发器 | 版本仅text数组缺少权威约束并由BUG-033记录；索引支持目标时间序 |
| `internal/database/changelog_schema_test.go` | internal/database | 25 | 已完成 | 更新日志Schema关键字段与索引测试 | TEST-027；仅字符串检查 |
| `internal/database/comment_schema.go` | internal/database | 224 | 已完成 | 树评论、闭包、反应、关注与热度刷新队列Schema | 无新增独立问题；楼层约束由feature_update扩展 |
| `internal/database/community_post_schema.go` | internal/database | 125 | 已完成 | 教程/问题/新闻/讨论、悬赏、引用与翻译Schema | 无新增独立问题；accepted_comment归属由Handler继续核对 |
| `internal/database/community_schema.go` | internal/database | 518 | 已完成 | 作者团队、认领、活动、经济、商城、经验与任务Schema | 无新增独立问题；作者/团队权限关系符合当前模型 |
| `internal/database/content_metrics_schema.go` | internal/database | 183 | 已完成 | 内容指标累计表、日表、刷新队列和原子领取函数 | 无新增问题；并发重新入队通过attempts与locked_at保护 |
| `internal/database/content_metrics_schema_test.go` | internal/database | 29 | 已完成 | 内容指标Schema关键DDL测试 | TEST-027；未执行真实并发和查询计划 |
| `internal/database/database.go` | internal/database | 51 | 已完成 | PostgreSQL主连接池、活动连接池与会话超时配置 | BUG-055；连接未固定UTC时区，日桶口径依赖数据库会话时区 |
| `internal/database/draft_schema.go` | internal/database | 37 | 已完成 | 用户草稿Schema、所有者隔离与更新时间索引 | 无新增问题 |
| `internal/database/engagement_export_schema.go` | internal/database | 176 | 已完成 | 收藏夹导出、表情包、项目关注、更新事件及通知任务Schema | MAP-006；DEAD-003/011；PERF-025；BUG-068；导出历史被收藏夹级联删除 |
| `internal/database/engagement_export_schema_test.go` | internal/database | 70 | 已完成 | 导出、表情、关注与更新Schema关键片段及权限种子字符串测试 | TEST-018；仅验证DDL文本存在，不验证约束行为、Worker与查询计划 |
| `internal/database/feature_update_schema.go` | internal/database | 109 | 已完成 | 合成表版本、评论楼层、日志分享与相关约束 | DB-008；DEAD-007；log_shares重复索引及未到达source取值 |
| `internal/database/feature_update_schema_test.go` | internal/database | 30 | 已完成 | 功能更新Schema关键DDL字符串测试 | TEST-027；仅覆盖关键字存在性 |
| `internal/database/governance_automation_schema.go` | internal/database | 341 | 已完成 | 举报、封禁、站务、爬虫和项目自动更新Schema | 无新增独立问题；重新审查变化后版本 |
| `internal/database/governance_automation_schema_test.go` | internal/database | 42 | 已完成 | 爬虫项目类型和自动化actor/维护所有权DDL片段测试 | TEST-019；字符串存在性不验证约束、状态机和真实Worker行为 |
| `internal/database/governance_automation_seeds.go` | internal/database | 75 | 已完成 | 治理默认封禁原因、站务多语言、爬虫配置、许可证策略和权限访问类型推导 | 无新增独立问题；权限访问类型仍为命名启发式，待seeds.go完整审查统一判断 |
| `internal/database/infrastructure_schema.go` | internal/database | 95 | 已完成 | 数据库/Schema | LEGACY-002 |
| `internal/database/internal_identity_schema_test.go` | internal/database | 77 | 已完成 | 内部身份与权限关系Schema语义测试 | TEST-027；覆盖范围为选定DDL片段 |
| `internal/database/migrations.go` | internal/database | 1147 | 已完成 | 数据库/Schema | LEGACY-002 |
| `internal/database/migrator.go` | internal/database | 437 | 已完成 | generation 85空库安装、开发重置、外键索引补齐与修订历史Schema | DB-008；部分索引被错误排除；开发重置调用方有配置防护 |
| `internal/database/migrator_test.go` | internal/database | 29 | 已完成 | 迁移器默认配置单元测试 | TEST-027；未覆盖执行、锁与失败恢复 |
| `internal/database/mod_content_document_schema.go` | internal/database | 181 | 已完成 | 各资料类型权威可编辑字段、导入别名与引用类型的内置文档Schema | 无新增问题；SQL辅助函数仅接收编译期常量 |
| `internal/database/mod_content_schema.go` | internal/database | 413 | 已完成 | Mod资料版本、内置/自定义模板、分类树、资源放置、逐版本详情与公开路由Schema | BUG-009；DB-004；空库初始化仍含同generation路由回填语句，待迁移器确认LEGACY归类 |
| `internal/database/mod_content_schema_test.go` | internal/database | 157 | 已完成 | 内置资料模板字段结构、引用字段和可编辑性Schema测试 | 未覆盖分类树深度、唯一约束和路由回填；BUG-009 |
| `internal/database/mod_content_template_integration_test.go` | internal/database | 41 | 已完成 | 远程数据库内置战利品表/游戏设置模板存在性测试 | 仅验证两个模板的基础存在性，未验证全部模板JSON结构 |
| `internal/database/modpack_schema.go` | internal/database | 151 | 已完成 | 整合包项目、加载器兼容、标签、链接、图库和包含模组关系Schema | 无新增问题 |
| `internal/database/permission_version_schema_test.go` | internal/database | 55 | 已完成 | 权限版本/认证版本触发器与提交者字段Schema约束测试 | 无新增问题 |
| `internal/database/project_access_integration_test.go` | internal/database | 348 | 已完成 | 派生权限多来源、认领唯一性、版本拆分、撤销与并发审批数据库集成测试 | 无新增问题；依赖显式远程隔离数据库开关 |
| `internal/database/project_access_schema.go` | internal/database | 120 | 已完成 | 已发布项目路由、编辑员/已认领个人作者及团队派生项目权限视图与版本触发器 | 无新增问题；提交者不参与权限，作者/团队关系使用project_acl全局版本 |
| `internal/database/project_access_schema_test.go` | internal/database | 54 | 已完成 | 派生项目权限仅保留编辑员和个人作者/团队线路的Schema文本测试 | 测试为字符串存在性断言；关键行为由集成测试补充 |
| `internal/database/project_file_schema.go` | internal/database | 54 | 已完成 | 项目文件表、过滤索引、父项目复合外键和公共路由触发器 | DB-005；自路由canonical path未注册且软删除不移除route |
| `internal/database/rating_schema.go` | internal/database | 451 | 已完成 | 评分、浏览、热度累计、趋势事件与刷新触发器 | BUG-055/057；PERF-034/035；MAP-007；DEAD-008 |
| `internal/database/rating_schema_integration_test.go` | internal/database | 57 | 已完成 | 空项目热度刷新数据库集成测试 | TEST-028；只证明空项目可生成统计行 |
| `internal/database/schema_quality_integration_test.go` | internal/database | 71 | 已完成 | 外键列索引覆盖集成测试 | DB-008；TEST-027；错误排除部分索引导致强制冗余索引 |
| `internal/database/search_schema.go` | internal/database | 165 | 已完成 | Typesense持久投影队列与业务表触发入队 | 无新增独立问题；数据库队列为事实来源且更新可合并 |
| `internal/database/seeds.go` | internal/database | 418 | 已完成 | 权限、角色、默认用户与autobot种子 | SEC-021；每次启动覆盖默认账号安全状态 |
| `internal/database/server_schema.go` | internal/database | 151 | 已完成 | 服务器目录、审核证据、模组引用、探测状态历史和公开路由Schema | 公开路由ON CONFLICT会被public_id_registry前置触发器截断，疑似不可达兼容分支；待迁移器交叉确认 |
| `internal/database/settings.go` | internal/database | 70 | 已完成 | 站点设置、NATS配置加载与环境默认值合并 | LEGACY-012；旧字段缺失回填掩盖保存端字段遗漏 |
| `internal/database/simple_project_schema.go` | internal/database | 150 | 已完成 | 插件/地图/材质/光影/数据包/附属资源共享项目Schema、路由和父级引用 | 无新增问题；版本/加载器数组与权威版本关系需结合Handler继续检查 |
| `internal/database/skin_schema.go` | internal/database | 177 | 已完成 | Yggdrasil账号/Token、皮肤资产、衣柜与玩家档案Schema | 无新增独立问题；软删除路由生命周期待Handler交叉确认 |
| `internal/database/user_block_schema.go` | internal/database | 18 | 已完成 | 方向性用户拉黑关系、唯一性与反向查询索引 | 无新增问题 |
| `internal/database/user_block_schema_test.go` | internal/database | 52 | 已完成 | 拉黑Schema及评论热度函数变量消歧测试 | 无新增问题 |
| `internal/database/user_feature_integration_test.go` | internal/database | 77 | 已完成 | 当前Schema关键表、在线隐私与autobot种子集成测试 | 仅检查对象存在，未触发统计写入；纳入TEST-002 |
| `internal/database/user_feature_schema.go` | internal/database | 196 | 已完成 | 用户在线状态、活动Outbox、统计累计和内容创建事实Schema | BUG-005；BUG-006 |
| `internal/database/user_feature_schema_test.go` | internal/database | 44 | 已完成 | 用户功能Schema静态结构与保留语义测试 | 未验证触发器真实行为，未发现submitted_by失配；纳入TEST-002 |
| `internal/domain/models.go` | internal/domain | 49 | 已完成 | 用户、角色、权限和本地化文本的内部/API领域模型 | 无新增问题；User明确隔离内部ID与公开ID |
| `internal/httpapi/activity_ingestion_handlers.go` | internal/httpapi | 25 | 已完成 | 活动摄取内存/Outbox/连接池健康快照接口 | 无新增问题；路由权限待server注册交叉核对 |
| `internal/httpapi/activity_middleware.go` | internal/httpapi | 405 | 已完成 | 请求完成后的用户操作分类、对象解析、浏览节流与可靠活动入队 | ARCH-006；SEC-007 |
| `internal/httpapi/activity_middleware_test.go` | internal/httpapi | 150 | 已完成 | 活动动作/对象推断、高频排除、注解优先级与Markdown差异测试 | 未覆盖持久入队失败及大文本资源上限；TEST-002 |
| `internal/httpapi/activity_retention_handlers.go` | internal/httpapi | 628 | 已完成 | 活动保留策略、强确认预览、分批手动删除和单实例自动清理Worker | MAP-003；PERF-005；ARCH-008 |
| `internal/httpapi/activity_retention_handlers_test.go` | internal/httpapi | 57 | 已完成 | 保留配置、参数化删除条件、UTC边界、确认Token和枚举拼写测试 | 仅抽查一个action ID，未覆盖百万级预览、部分失败与全部枚举漂移；TEST-002 |
| `internal/httpapi/admin_dashboard_handlers.go` | internal/httpapi | 278 | 已完成 | 后台总览、OSS统计、项目列表与项目趋势 | PERF-036；深OFFSET、精确COUNT与前后通配搜索 |
| `internal/httpapi/admin_dashboard_integration_test.go` | internal/httpapi | 167 | 已完成 | 后台总览及混放的表情、导出租约和通知翻译数据库集成测试 | TEST-029；STYLE-005 |
| `internal/httpapi/admin_economy_handlers.go` | internal/httpapi | 135 | 已完成 | 管理员余额读取和带理由调整 | SEC-039；TEST-044；行锁与流水同事务 |
| `internal/httpapi/admin_handlers.go` | internal/httpapi | 1471 | 已完成 | 后台配置、角色/权限目录与用户手工授权 | SEC-004/044；BUG-050/122；ARCH-033；PERF-062；TEST-049；LEGACY-020；MAP-011；DEAD-014 |
| `internal/httpapi/admin_handlers_test.go` | internal/httpapi | 95 | 已完成 | 角色模板匹配、变量应用与允许/拒绝优先级纯函数测试 | 无新增Finding；未覆盖管理HTTP写入、版本失效和并发授权 |
| `internal/httpapi/admin_mod_content_attribute_handlers.go` | internal/httpapi | 216 | 已完成 | 后台读取与更新全站内建Mod资料属性模板、本地化和稳定字段Schema约束 | BUG-024；更新允许直接删除仍被历史资料entry_type_code引用的整个条目类型，且没有数据级引用检查，既有资料后续校验、编辑和引用同步会失败 |
| `internal/httpapi/admin_user_details.go` | internal/httpapi | 129 | 已完成 | 后台用户详情、角色绑定与有效权限读取 | 无新增问题 |
| `internal/httpapi/ai_handlers.go` | internal/httpapi | 693 | 已完成 | AI配置、任务创建、Worker状态机、统计与日志 | BUG-094；ARCH-026；TEST-043；通用创建入口正确支持Outbox |
| `internal/httpapi/ai_translation.go` | internal/httpapi | 332 | 已完成 | AI供应商请求、提示构造、结果解析、计费和通知翻译持久化 | ARCH-026；TEST-043 |
| `internal/httpapi/ai_translation_test.go` | internal/httpapi | 65 | 已完成 | AI任务配置、JSON结果、提示和端点纯函数测试 | BUG-094；ARCH-026；TEST-043；未覆盖Worker和供应商故障 |
| `internal/httpapi/anti_abuse_handlers.go` | internal/httpapi | 362 | 已完成 | 表单Token、反滥用后台配置/总览/事件/用户状态/限制/机器人规则接口 | ARCH-007；MAP-002；列表分页和读取错误处理不完整 |
| `internal/httpapi/anti_abuse_middleware.go` | internal/httpapi | 305 | 已完成 | 受保护变更的封禁门、动作分类、请求内容抽取、风险决策与成功记录 | PERF-004；服务错误统一软放行需结合入口风险继续复核 |
| `internal/httpapi/anti_abuse_middleware_test.go` | internal/httpapi | 106 | 已完成 | 动作/RateScope/变量限流权限/封禁标记和请求体恢复测试 | 未覆盖成功高并发、DB阻塞和软放行；PERF-004；TEST-002 |
| `internal/httpapi/auth_cache.go` | internal/httpapi | 240 | 已完成 | 认证/权限 | auth/RBAC cache |
| `internal/httpapi/auth_cache_integration_test.go` | internal/httpapi | 273 | 已完成 | Session、permission_version与rbac_version缓存集成测试 | 测试覆盖权限变化不注销Session和密码变化撤销Session；无新增问题 |
| `internal/httpapi/auth_cache_test.go` | internal/httpapi | 52 | 已完成 | Session/RBAC缓存Key和认证限流维度测试 | 无新增问题 |
| `internal/httpapi/auth_handlers.go` | internal/httpapi | 604 | 已完成 | 注册、密码/邮箱验证码登录、Session签发、当前用户权限响应 | 无新增问题 |
| `internal/httpapi/auth_handlers_test.go` | internal/httpapi | 56 | 已完成 | 用户名和权限拒绝规则单元测试 | TEST-002（认证主流程集成覆盖薄弱） |
| `internal/httpapi/auth_rate_limit.go` | internal/httpapi | 92 | 已完成 | 登录、注册和验证码的多维限流策略 | SEC-003 |
| `internal/httpapi/auth_session.go` | internal/httpapi | 43 | 已完成 | 认证Cookie生成、清除与代理TLS判断 | 无新增问题 |
| `internal/httpapi/automation_actor.go` | internal/httpapi | 29 | 已完成 | 基础设施/通用 | autobot |
| `internal/httpapi/automation_actor_integration_test.go` | internal/httpapi | 41 | 已完成 | autobot种子身份、真实RBAC权限和1000%审核限流集成测试 | 无新增独立问题；有效证明不再拼接管理员Claims |
| `internal/httpapi/blueprint_cache_test.go` | internal/httpapi | 31 | 已完成 | 蓝图公开/私有响应缓存策略测试 | TEST-036；覆盖已批准公开缓存与私有no-store，未覆盖转换和任务恢复 |
| `internal/httpapi/blueprint_codec.go` | internal/httpapi | 933 | 已完成 | Vanilla/Sponge/Litematic/Legacy蓝图解码、规范文档、转换和材料聚合 | SEC-026；BUG-076；PERF-042；TEST-036 |
| `internal/httpapi/blueprint_codec_test.go` | internal/httpapi | 51 | 已完成 | 蓝图基础NBT往返与空气方块过滤测试 | TEST-036；未覆盖恶意调色板、实体保真和各格式边界 |
| `internal/httpapi/blueprint_cover_render.go` | internal/httpapi | 146 | 已完成 | 蓝图方块集合的等距PNG封面渲染 | PERF-042；输入可达数百万方块并排序/建图，缺少共享渲染预算 |
| `internal/httpapi/blueprint_cover_render_test.go` | internal/httpapi | 39 | 已完成 | 蓝图封面尺寸和非空像素基础测试 | TEST-036；未覆盖极限规模、超大坐标和资源预算 |
| `internal/httpapi/blueprint_handlers.go` | internal/httpapi | 958 | 已完成 | 蓝图上传绑定、目录详情、材料、编辑审核、转换重试和下载 | SEC-027；BUG-077/078；ARCH-019；LEGACY-013；PERF-043；TEST-036 |
| `internal/httpapi/blueprint_worker.go` | internal/httpapi | 380 | 已完成 | 蓝图规范化/转换Worker、OSS衍生物、材料和通知 | BUG-075/076；OPS-014/015；PERF-042；TEST-036 |
| `internal/httpapi/catalog_asset_handlers.go` | internal/httpapi | 150 | 已完成 | 公开目录资源图标、合成表背景图与渲染资产的受限重定向 | 无新增问题；仅允许活动实体、活动版本和已通过安全状态的光栅图像 |
| `internal/httpapi/catalog_editor_handlers.go` | internal/httpapi | 1717 | 已完成 | 目录编辑列表、详情、创建/修改/归档、导入回退、配方绑定和资源装饰Handler | PERF-006；PERF-008；ARCH-010；多处详情辅助查询错误被降级为零值 |
| `internal/httpapi/catalog_editor_handlers_test.go` | internal/httpapi | 73 | 已完成 | 导入本地化回退、名称解析和公开ID校验测试 | 测试范围窄，未覆盖Handler权限、分页、缓存及公开渲染路径 |
| `internal/httpapi/catalog_editor_service.go` | internal/httpapi | 1131 | 已完成 | 目录编辑提交事务、审核快照发布、本地化、资源/标签/模板/配方权威物化和引用校验 | BUG-010；本文件发布手工配方但不生成导入快照，公开读取模型未覆盖 |
| `internal/httpapi/catalog_editor_types.go` | internal/httpapi | 347 | 已完成 | 目录编辑请求、快照、模板几何与合成绑定的输入验证模型 | 无新增问题；本地化、数值范围、槽位唯一性和候选资源规则均有明确校验 |
| `internal/httpapi/catalog_editor_types_test.go` | internal/httpapi | 175 | 已完成 | 目录编辑本地化、模板、配方绑定、可选字段与活动标识验证测试 | 覆盖主要验证分支；未覆盖公开查询与手工发布的整合链路 |
| `internal/httpapi/catalog_identity.go` | internal/httpapi | 235 | 已完成 | 目录实体确定性身份、随机公开ID、Mod ID别名归一与导入资源种类分类 | BUG-011；未知注册表统一降为import.document，可合并不同资源语义 |
| `internal/httpapi/catalog_identity_alias_test.go` | internal/httpapi | 22 | 已完成 | Mod ID别名共享资源身份及不同资源种类隔离测试 | 未覆盖不同未知注册表的身份隔离；BUG-011 |
| `internal/httpapi/catalog_identity_test.go` | internal/httpapi | 21 | 已完成 | 已知注册表到资源种类映射测试 | 只覆盖已知白名单，未覆盖未知注册表碰撞；BUG-011 |
| `internal/httpapi/catalog_import.go` | internal/httpapi | 273 | 已完成 | 目录资源导入临时表、批量COPY、实体/别名/快照Upsert及未解析引用回填 | BUG-012；导入无条件把已归档目录实体重新激活 |
| `internal/httpapi/catalog_import_test.go` | internal/httpapi | 134 | 已完成 | 目录导入复用既有权威身份、合并快照字段和别名集成测试 | 未覆盖已归档实体重导入、未知注册表与冲突并发；BUG-011；BUG-012 |
| `internal/httpapi/catalog_mod_filter_integration_test.go` | internal/httpapi | 70 | 已完成 | 整合包包含模组的全部匹配语义远程数据库测试 | 覆盖单项和缺项，不含多重重复、别名与分页 |
| `internal/httpapi/catalog_resource_definition.go` | internal/httpapi | 265 | 已完成 | 目录资源定义的大小/深度、物理、工具、渲染、流体和扩展字段校验 | 无新增问题；未知扩展字段有总大小与深度边界 |
| `internal/httpapi/catalog_resource_definition_test.go` | internal/httpapi | 37 | 已完成 | 资源定义已知字段合法/非法与Mod扩展属性测试 | 未覆盖总大小、最大深度、NaN/Inf和渲染向量边界 |
| `internal/httpapi/catalog_sort.go` | internal/httpapi | 242 | 已完成 | 全项目目录排序、方向、列表/标量/布尔/版本范围过滤解析及安全ORDER BY构造 | LEGACY-005；仍同时接受latest/oldest/created/nameAsc/nameDesc旧模型 |
| `internal/httpapi/catalog_sort_integration_test.go` | internal/httpapi | 103 | 已完成 | 远程数据库各项目目录核心排序和包含模组筛选集成冒烟 | 只断言HTTP 200，不验证顺序或结果内容 |
| `internal/httpapi/catalog_sort_test.go` | internal/httpapi | 159 | 已完成 | 排序注入边界、稳定排序、过滤白名单和旧排序链接测试 | LEGACY-005；测试继续锁定开发期旧排序别名 |
| `internal/httpapi/comment_handlers.go` | internal/httpapi | 1772 | 已完成 | 评论目标解析、楼层/树、编辑删除、表态插眼、附件和权限注解 | SEC-031/032/033；BUG-082/083/084/085；ARCH-022；PERF-045/046；LEGACY-016；TEST-038 |
| `internal/httpapi/comment_handlers_test.go` | internal/httpapi | 43 | 已完成 | CY插眼命令、摘要清理和日志附件文件名测试 | TEST-038；只有纯函数正常路径 |
| `internal/httpapi/comment_idempotency_test.go` | internal/httpapi | 19 | 已完成 | 评论幂等事务锁SQL和键作用域测试 | TEST-038；未执行并发重复请求验证 |
| `internal/httpapi/comment_project_role_test.go` | internal/httpapi | 59 | 已完成 | 评论作者项目身份徽章权限推导测试 | 无新增Finding；管理员通配符不伪造作者身份的边界正确 |
| `internal/httpapi/comment_tree_integration_test.go` | internal/httpapi | 187 | 已完成 | 评论树、楼层、闭包、插眼、拉黑过滤和附件归属数据库集成测试 | TEST-038；覆盖核心原子树但未覆盖目标IDOR、无界线程和生命周期故障 |
| `internal/httpapi/community_post_bounty_handlers.go` | internal/httpapi | 253 | 已完成 | 讨论悬赏冻结、退款、答案接受和自行解决事务 | 无新增Finding；资产变更与问题状态处于同一事务并使用行锁 |
| `internal/httpapi/community_post_catalog_test.go` | internal/httpapi | 51 | 已完成 | 社区分类、项目类型白名单和项目筛选解析测试 | TEST-042；仅覆盖纯函数白名单 |
| `internal/httpapi/community_post_handlers.go` | internal/httpapi | 1071 | 已完成 | 教程、问题/特性、新闻和讨论的目录、修订、引用、翻译与可见性 | SEC-037；BUG-090/091；ARCH-025；PERF-052/053；TEST-042 |
| `internal/httpapi/community_post_handlers_test.go` | internal/httpapi | 78 | 已完成 | 社区内容语言启发式、问题必填项、新闻字段清理和悬赏配对测试 | BUG-091；TEST-042；未覆盖数据库、权限、并发、审核和资产结算 |
| `internal/httpapi/compression.go` | internal/httpapi | 132 | 已完成 | gzip协商、可压缩响应Writer、Flush和Vary处理 | BUG-061；非法q值被当作允许 |
| `internal/httpapi/compression_test.go` | internal/httpapi | 52 | 已完成 | JSON gzip与二进制跳过测试 | TEST-032；q=0只配合二进制测试，未验证协商拒绝 |
| `internal/httpapi/content_history_handlers.go` | internal/httpapi | 155 | 已完成 | 社区内容与Mod资料资源的人工/导入修订历史和可见性 | PERF-054；TEST-043 |
| `internal/httpapi/content_language_settings.go` | internal/httpapi | 79 | 已完成 | 用户主/次内容语言读取更新和操作记录 | STYLE-003；英文直接错误仍存在，事务保证设置与活动记录同提交 |
| `internal/httpapi/content_locale.go` | internal/httpapi | 247 | 已完成 | 内容语言标签规范化、可编辑语言集合与回退解析 | BUG-093；TEST-043 |
| `internal/httpapi/content_locale_test.go` | internal/httpapi | 133 | 已完成 | 内容语言规范化、回退和自动翻译判定测试 | BUG-093；TEST-043；未覆盖Accept-Language权重 |
| `internal/httpapi/content_localization_handlers.go` | internal/httpapi | 765 | 已完成 | 统一内容本地化读取、编辑、AI翻译入队、配额和结果查询 | SEC-038；BUG-092/093；ARCH-026；LEGACY-019；OPS-019；TEST-043 |
| `internal/httpapi/content_localization_worker.go` | internal/httpapi | 153 | 已完成 | AI内容翻译结果写入统一修订审核流 | TEST-043；消费状态门保证单任务重复投递不重复执行 |
| `internal/httpapi/content_metrics_handlers.go` | internal/httpapi | 447 | 已完成 | 公开内容统计、浏览写入、近期访问者与项目关系展示 | SEC-022/023；公开近期访问者绕过隐私；任意pageKey且明确绕过反滥用 |
| `internal/httpapi/content_review_handlers.go` | internal/httpapi | 760 | 已完成 | 各内容聚合的审核批准驳回、基线冲突与发布投影 | 既有ARCH-002；TEST-043；人工审核路径均在锁内比较基线 |
| `internal/httpapi/content_review_store.go` | internal/httpapi | 379 | 已完成 | 统一内容修订、变更请求、审计事件与JSON差异存储 | PERF-055；BUG-090根因边界；TEST-043 |
| `internal/httpapi/content_review_store_test.go` | internal/httpapi | 60 | 已完成 | JSON Pointer差异和等价性纯函数测试 | PERF-055；TEST-043；未覆盖事务、并发和大快照 |
| `internal/httpapi/creator_handlers.go` | internal/httpapi | 1399 | 已完成 | 基础设施/通用 | LEGACY-001 |
| `internal/httpapi/creator_handlers_test.go` | internal/httpapi | 35 | 已完成 | 作者名称、链接、声明与成员输入规范化测试 | 无新增问题 |
| `internal/httpapi/creator_import_handlers.go` | internal/httpapi | 480 | 已完成 | Modrinth/CurseForge作者与团队导入、头像镜像和角色映射 | SEC-006；DB-003 |
| `internal/httpapi/creator_import_handlers_test.go` | internal/httpapi | 155 | 已完成 | 导入URL、角色映射与HTML资料解析测试 | TEST-005（锁定未知角色授予developer的风险规则） |
| `internal/httpapi/creator_revision_integration_test.go` | internal/httpapi | 69 | 已完成 | 作者修订、发布修订与快照创建集成测试 | 无新增问题 |
| `internal/httpapi/draft_handlers.go` | internal/httpapi | 336 | 已完成 | 用户草稿保存、恢复、完成状态与清理 | SEC-043；PERF-061；BUG-120 |
| `internal/httpapi/draft_handlers_test.go` | internal/httpapi | 59 | 已完成 | Markdown草稿DTO规范化、保留权限与Payload边界测试 | BUG-003；未区分无记录与数据库故障 |
| `internal/httpapi/economy_handlers.go` | internal/httpapi | 1149 | 已完成 | 货币、签到、转账、商店、道具、下载奖励与余额事务 | SEC-039；BUG-095/096/097；ARCH-027；TEST-044 |
| `internal/httpapi/favorite_handlers.go` | internal/httpapi | 365 | 已完成 | 收藏夹创建管理、目标成员关系、公开/本人收藏内容装配 | SEC-035/036；BUG-089；ARCH-023/024；PERF-051；LEGACY-018；TEST-041 |
| `internal/httpapi/favorite_modpack_export.go` | internal/httpapi | 474 | 已完成 | 收藏夹MRPack预检、任务创建、权限与反滥用限制 | SEC-024；BUG-065/066/067/070/072；PERF-040；TEST-034 |
| `internal/httpapi/favorite_modpack_export_query.go` | internal/httpapi | 127 | 已完成 | MRPack任务历史、详情、下载与OSS访问边界 | BUG-071；PERF-041；ARCH-018；TEST-034 |
| `internal/httpapi/favorite_modpack_export_worker.go` | internal/httpapi | 271 | 已完成 | MRPack持久任务租约、Worker生成、过期与OSS生命周期 | BUG-066/068/069；OPS-013；ARCH-018；TEST-034 |
| `internal/httpapi/feature_localization_sticker_test.go` | internal/httpapi | 117 | 已完成 | 通知模板、表情code/语言和功能限制混合纯函数测试 | TEST-045；未覆盖图片、OSS、权限、生命周期、并发和AST |
| `internal/httpapi/feature_update_handlers_test.go` | internal/httpapi | 27 | 已完成 | 合成表适用版本排序与未知分组纯函数测试 | TEST-033；未覆盖绑定写入、并发导入和公开查询 |
| `internal/httpapi/global_catalog_handlers.go` | internal/httpapi | 500 | 已完成 | 公开合成类型、合成列表、渲染结果、本地化资源装饰和缓存响应 | BUG-010；PERF-006；PERF-007；ARCH-009 |
| `internal/httpapi/global_resource_admin_handlers.go` | internal/httpapi | 129 | 已完成 | 公开资源名称展示与后台全局资源技术绑定分页查询 | 后台路由权限由注册层保护；模糊搜索和深分页性能待与统一分页策略合并评估 |
| `internal/httpapi/governance_handlers.go` | internal/httpapi | 874 | 已完成 | 统一举报、证据快照、审核处置、封禁与小黑屋公开/后台边界 | SEC-042；BUG-001/109/110/111/112/113；ARCH-030；PERF-060；DEAD-013；TEST-048；扩展LEGACY-001/016 |
| `internal/httpapi/governance_handlers_test.go` | internal/httpapi | 111 | 已完成 | 举报原因注册、路由类型规范化、反滥用动作和公开封禁状态纯函数测试 | TEST-048；未覆盖数据库、权限、可见性、处置事务、到期和并发 |
| `internal/httpapi/health_test.go` | internal/httpapi | 40 | 已完成 | 存活/就绪探针和开发期旧health路由删除回归 | 无新增Finding |
| `internal/httpapi/infrastructure_metrics.go` | internal/httpapi | 126 | 已完成 | 管理员基础设施指标、死信列表与事务化重放 | ARCH-015；PERF-038；TEST-032 |
| `internal/httpapi/location_handlers.go` | internal/httpapi | 146 | 已完成 | 可信代理位置头、X-Forwarded-For和客户端IP规范化 | 无新增Finding；失败关闭且右向左剥离可信跳 |
| `internal/httpapi/location_handlers_test.go` | internal/httpapi | 64 | 已完成 | 位置解析代理头反伪造单元测试 | 覆盖不可信/可信对端、伪造左侧hop和畸形链 |
| `internal/httpapi/log_handlers.go` | internal/httpapi | 539 | 已完成 | 运行日志读取、API访问日志、后台审计日志查询与保留配置 | BUG-051/052/053/054；SEC-020；PERF-032/033；LEGACY-010；ARCH-013 |
| `internal/httpapi/log_handlers_test.go` | internal/httpapi | 24 | 已完成 | 日志保留类别纯函数测试 | TEST-025；未覆盖中间件、敏感参数、数据库故障与清理调度 |
| `internal/httpapi/log_share_handlers.go` | internal/httpapi | 562 | 已完成 | 文件/OSS/日志 | MAP-001 |
| `internal/httpapi/log_share_handlers_test.go` | internal/httpapi | 85 | 已完成 | 日志脱敏、短码、保留期与ZIP基础安全纯函数测试 | TEST-037/038；未覆盖权限、配额、到期、下载和完整ZIP/源文件生命周期 |
| `internal/httpapi/login_security_test.go` | internal/httpapi | 16 | 已完成 | 登录假密码哈希抗用户枚举测试 | 无新增问题 |
| `internal/httpapi/maintenance_worker.go` | internal/httpapi | 206 | 已完成 | 举报证据、封禁、日志分享、草稿、Yggdrasil与Presence分批过期清理 | BUG-111结论已修正；OPS-020；TEST-048 |
| `internal/httpapi/markdown_handlers.go` | internal/httpapi | 136 | 已完成 | 基础设施/通用 | SEC-001 |
| `internal/httpapi/message_handlers.go` | internal/httpapi | 317 | 已完成 | 私聊会话创建/列表、消息读写、未读与聊天活跃状态 | BUG-027/045；PERF-030；LEGACY-009；无历史方向分页且邮件绕过本地化可靠边界 |
| `internal/httpapi/middleware.go` | internal/httpapi | 181 | 已完成 | 基础设施/通用 | auth state |
| `internal/httpapi/middleware_test.go` | internal/httpapi | 123 | 已完成 | Cookie来源校验与可选认证失效信号测试 | 无新增问题 |
| `internal/httpapi/minecraft_loader_version_sources.go` | internal/httpapi | 481 | 已完成 | Minecraft及四类加载器远端目录同步、回退源、解析与排序 | BUG-062/064；OPS-012；ARCH-016；MAP-009；PERF-039 |
| `internal/httpapi/minecraft_loader_version_sources_live_test.go` | internal/httpapi | 54 | 已完成 | Mojang及四类加载器真实远端来源可用性测试 | TEST-033；显式环境开关，未覆盖同步持久化和多实例 |
| `internal/httpapi/minecraft_version_handlers.go` | internal/httpapi | 341 | 已完成 | Minecraft版本公开/后台配置、规范化、默认目录与每日同步调度 | BUG-062/064；ARCH-016；OPS-012 |
| `internal/httpapi/minecraft_version_handlers_test.go` | internal/httpapi | 119 | 已完成 | 版本分类、远端解码、解析排序和失败保留纯函数测试 | TEST-033；无HTTP/数据库/调度并发测试 |
| `internal/httpapi/mod_content_advancement_integration_test.go` | internal/httpapi | 200 | 已完成 | 真实PostgreSQL下进度资料创建、布局坐标/父子保存及重复无变化保存测试 | TEST-013；待审创建通过直接SQL改active而非真实审核发布，因而未验证修订、审核事件与发布事务一致性 |
| `internal/httpapi/mod_content_definition.go` | internal/httpapi | 721 | 已完成 | Mod资料模板继承解析、条目类型选择、字段规范化、导入类型推断及维度/群系与战利品派生字段处理 | BUG-024；条目类型选择对已删除或停用类型返回引用错误，证明后台无引用检查删除会使既有entry_type_code失去可解析Schema |
| `internal/httpapi/mod_content_definition_test.go` | internal/httpapi | 402 | 已完成 | 资料定义规范化、别名、只读字段、派生引用、导入推断、Schema稳定性和停用类型单元测试 | TEST-012；测试明确要求整类删除被接受，却没有构造数据库中的既有资料引用或验证删除后可读写性，锁定BUG-024 |
| `internal/httpapi/mod_content_handlers.go` | internal/httpapi | 2427 | 已完成 | Mod资料版本、模板、板块、资源详情的校验、公开读取、修订提交、审核发布、归档及引用同步主链 | BUG-024；BUG-025；BUG-026；BUG-027；PERF-015；PERF-016；LEGACY-007；自定义模板可破坏Schema，旧板块PUT可跨版本拆裂子树且拒绝合法全局资源，多列表漏rows.Err，公共资料查询和逐引用SQL扩展性不足 |
| `internal/httpapi/mod_content_layout_handlers.go` | internal/httpapi | 1035 | 已完成 | 整版资料目录树规范化、资源/相似组/进度关系校验、批量发布、归档差异和单资源移动 | PERF-017；权威整版布局路径正确使用mod_resource_bindings并批量写资源，但最多1000分类/10000相似组的ID生成和每分类本地化仍采用大量顺序SQL；与旧单板块PUT并存 |
| `internal/httpapi/mod_content_resource_integration_test.go` | internal/httpapi | 342 | 已完成 | 真实PostgreSQL下资源列表、板块树、分页详情、布局移动、进度定义和版本详情隔离测试 | TEST-013；覆盖常规同属Mod资源和布局，但不覆盖模板失效、跨版本板块、owner_mod_id为空的全局资源及大数据查询；仍直接测试无前端调用的集合GET |
| `internal/httpapi/mod_content_review_test.go` | internal/httpapi | 298 | 已完成 | 资料修订聚合键、物品方块布局去重、进度父子循环、本地化来源及不可编辑语言保护单元测试 | 无新增问题；纯函数边界较完整，但不能覆盖事务级模板、版本树和全局资源关系问题 |
| `internal/httpapi/mod_content_safety_integration_test.go` | internal/httpapi | 359 | 已完成 | 真实PostgreSQL下待审资源预览作用域、板块回传、归档详情复用、板块树归档和版本导入失效测试 | BUG-022；TEST-013；只测试全局content.review，不覆盖项目作用域审核者；归档测试未验证模板/子树版本不变量、全局资源和板块删除后直接资源可见性 |
| `internal/httpapi/mod_content_similar_groups_test.go` | internal/httpapi | 41 | 已完成 | 相似资料组同分类、单成员移除和跨分类拒绝单元测试 | 无新增问题；覆盖规范化核心分支 |
| `internal/httpapi/mod_content_system_categories.go` | internal/httpapi | 62 | 已完成 | 物品/方块模板首次建立时幂等创建系统分类并写入三种核心语言名称 | 无新增独立问题；由唯一约束保证并发幂等，调用方仅为已审查的资料根创建事务 |
| `internal/httpapi/mod_embedded_icon_import.go` | internal/httpapi | 857 | 已完成 | IconRenderer/LetMeSeeSee/IRR JSON上传、任务、MODID确认、PNG规范化、目录持久化与资料激活 | SEC-009；SEC-010；OPS-003；复用可变全局SHA包和分批OSS写入问题，PNG在尺寸校验前完整解码且8并发可放大解压内存 |
| `internal/httpapi/mod_embedded_icon_import_test.go` | internal/httpapi | 186 | 已完成 | 三种图标目录协议别名、JSONL/IRR变体、实体图标构建及固定画布渲染测试 | SEC-010；仅测试小型合法PNG，未覆盖超大尺寸、压缩炸弹、并发内存和失败清理 |
| `internal/httpapi/mod_export_asset_batch.go` | internal/httpapi | 71 | 已完成 | 文本/JSON资产的临时表COPY和修订路径唯一合并 | 无新增问题；JSON转jsonb在SQL边界失败关闭 |
| `internal/httpapi/mod_export_block_bindings.go` | internal/httpapi | 340 | 已完成 | 方块与物品关系、模型/纹理引用图遍历、资源快照绑定推导与批量持久化 | BUG-013；未知命名空间仍被静默跳过；无独立自动化测试，复杂模型图仅由可选导出合同测试覆盖 |
| `internal/httpapi/mod_export_block_entities.go` | internal/httpapi | 288 | 已完成 | 方块实体模型/变体/网格合同校验、资源关联及临时表批量持久化 | BUG-013；未知命名空间模型静默跳过；结构边界严格但无专门边界测试 |
| `internal/httpapi/mod_export_content_sync.go` | internal/httpapi | 334 | 已完成 | 将导入快照同步到Mod资料版本、自动建立板块分类/本地化/资源布局及活动记录 | BUG-017；BUG-018；REUSE-001；并发ordinal读取插入可冲突，重导入不会移除新快照已不存在的旧资源，种类与战利品分类CASE重复 |
| `internal/httpapi/mod_export_contract.go` | internal/httpapi | 123 | 已完成 | Mod导出manifest、capability合同校验与按revision批量写入 | 无新增问题；必填项、环境一致性、状态白名单和重复ID均有校验 |
| `internal/httpapi/mod_export_contract_test.go` | internal/httpapi | 205 | 已完成 | 真实导出ZIP的manifest、capability、block绑定、JEI模板/配方合同可选验证 | 依赖MCMODS_EXPORT_TEST_DIR而默认跳过；不含数据库和OSS持久化 |
| `internal/httpapi/mod_export_derived.go` | internal/httpapi | 57 | 已完成 | 导入修订的注册表/文档/资产/配方/标签/capability派生统计刷新 | 无新增问题；按本次少量revision一次物化而非读时重算 |
| `internal/httpapi/mod_export_documents.go` | internal/httpapi | 459 | 已完成 | 导出文档合同校验、资源投影、维度/群系/建筑/战利品引用提取与目录入队 | BUG-013；多命名空间时无修订匹配的条目被静默丢弃；DEAD-002的ordinal无效局部变量 |
| `internal/httpapi/mod_export_documents_test.go` | internal/httpapi | 63 | 已完成 | 维度类型合并、生物群系引用和建筑群系标签拆分测试 | 未覆盖合同错误、命名空间不匹配和静默跳过；BUG-013 |
| `internal/httpapi/mod_export_entry_handlers.go` | internal/httpapi | 394 | 已完成 | Mod导出资源详情、本地化内容、模型、版本与配方关联查询及知识页发布 | BUG-015；MAP-004；ARCH-011；缺省locale覆盖用户语言偏好，模型JSON错误被静默降级 |
| `internal/httpapi/mod_export_handlers.go` | internal/httpapi | 2101 | 已完成 | Mod导出包上传、任务创建/重试、ZIP校验、异步导入、资源持久化、激活同步与结果通知主链 | BUG-012；BUG-013；BUG-019；SEC-009；OPS-003；ARCH-011；Outbox模式重试/恢复任务不重新入队，SHA去重包可被跨用户覆盖源指针，事务失败会遗留OSS对象/文件记录 |
| `internal/httpapi/mod_export_handlers_test.go` | internal/httpapi | 771 | 已完成 | 导出协议解析、规范化、ZIP路径、Tag归属、合成模板和资源绑定单元测试 | TEST-008；未覆盖任务重试/恢复、Outbox再入队、跨用户包SHA冲突和OSS失败清理 |
| `internal/httpapi/mod_export_icon_catalog.go` | internal/httpapi | 155 | 已完成 | 从标准图标目录物化缺失注册表资源、翻译名与最佳尺寸图标 | 无新增问题；仅支持明确mob_effects协议目录，路径清理和确定性排序有效 |
| `internal/httpapi/mod_export_icon_catalog_test.go` | internal/httpapi | 22 | 已完成 | 图标目录资源识别、非支持目录忽略和32/256尺寸优先级测试 | 未覆盖异常命名空间、非标准尺寸与大小写扩展名 |
| `internal/httpapi/mod_export_job_lifecycle.go` | internal/httpapi | 172 | 已完成 | 导入任务租约、心跳、失败、停滞恢复、直接/Outbox分发及事务辅助 | LEGACY-006；关闭Outbox时仍存在Core NATS直接发布与进程内goroutine双路径 |
| `internal/httpapi/mod_export_latest_integration_test.go` | internal/httpapi | 849 | 已完成 | 基于真实导出ZIP和PostgreSQL的目录投影、模板推广、资料同步及同包重建集成测试 | TEST-008；测试依赖可选ZIP环境变量且在单一回滚事务内直接调用内部函数，不覆盖HTTP/Outbox/OSS/Worker生命周期 |
| `internal/httpapi/mod_export_locale_bundles.go` | internal/httpapi | 257 | 已完成 | 导出语言包压缩、OSS元数据持久化、受限解压读取、缓存与名称装饰 | 无新增问题；区域语言回退为同基础语言字典且选择顺序为字典序，需产品确认 |
| `internal/httpapi/mod_export_locale_bundles_test.go` | internal/httpapi | 87 | 已完成 | 冷门语言字典保留、修订别名去重和区域语言对象隔离测试 | 未覆盖OSS读取、16MiB解压上限、缓存及同locale冲突 |
| `internal/httpapi/mod_export_locales.go` | internal/httpapi | 86 | 已完成 | Minecraft语言代码到站内BCP-47的外部协议适配及嵌套本地化字段筛选 | 无新增问题；该映射承担真实外部协议边界，不是无意义别名 |
| `internal/httpapi/mod_export_query_handlers.go` | internal/httpapi | 679 | 已完成 | Mod导出修订摘要、注册表/文档/资产/结构查询、审核激活与修订可见性边界 | BUG-016；MAP-004；PERF-010；PERF-011；ARCH-011；审核note被忽略、多个行迭代错误未检查 |
| `internal/httpapi/mod_export_raw_fallback.go` | internal/httpapi | 113 | 已完成 | 局部规范化导出的诊断源文档保留路径提取与重复JSON剥离 | 无新增问题；只对partial条目保留实际存在的归档路径 |
| `internal/httpapi/mod_export_recipe_batch.go` | internal/httpapi | 521 | 已完成 | 配方/绑定/候选资源的临时表COPY、批量Upsert、版本绑定和未解析引用写入 | BUG-012；配方类型与配方导入均会重新激活已归档实体 |
| `internal/httpapi/mod_export_recipe_batch_test.go` | internal/httpapi | 38 | 已完成 | 配方候选资源去重与身份冲突拒绝测试 | 仅覆盖纯函数，未覆盖导入归档治理语义和批量SQL |
| `internal/httpapi/mod_export_recipe_import.go` | internal/httpapi | 1072 | 已完成 | JEI类型/模板/配方解析、规范化、批量入队、模板推广与并发ZIP JSON解码 | BUG-012；BUG-013；PERF-009；模板推广按模板逐次查询槽位 |
| `internal/httpapi/mod_export_resources.go` | internal/httpapi | 600 | 已完成 | 导出资源的批量跨修订/手工资源解析、类型规范化、战利品与定义引用装饰 | BUG-014；MAP-004；ARCH-011；首选revision排序优先于资源种类，可解析为同ID错误种类 |
| `internal/httpapi/mod_export_resources_test.go` | internal/httpapi | 190 | 已完成 | 资源种类规范化、本地化、战利品/附魔引用与完整详情投影测试 | MAP-004；测试显式固化entityId/publicId同值；未测跨种类同canonical ID解析顺序 |
| `internal/httpapi/mod_export_tag_handlers.go` | internal/httpapi | 135 | 已完成 | 指定导入修订的Tag列表、详情、成员装饰和可见性边界 | MAP-004；PERF-010；ARCH-011；entityId/publicId实际同值、all=1上限10000且详情成员无界、行迭代错误未检查 |
| `internal/httpapi/mod_gallery_handlers.go` | internal/httpapi | 36 | 已完成 | 按Mod与图片公开ID校验待审可见性并重定向到已激活OSS画廊对象 | BUG-022；项目级审核者缺少待审Mod画廊读取能力，提交者/全局审核与普通公开边界正确 |
| `internal/httpapi/mod_handlers.go` | internal/httpapi | 2014 | 已完成 | Mod创建、公开列表/详情、输入规范化、作者/图片/关系/兼容性持久化、未解析引用回填与ID生成主链 | SEC-013；PERF-014；OPS-003；编辑当前Mod可删除和改写其他已发布Mod拥有的入向关系并全局清理空组，详情关系分组N+1；导入媒体在项目事务外落OSS/文件表 |
| `internal/httpapi/mod_handlers_test.go` | internal/httpapi | 275 | 已完成 | Mod站内ID、请求归一化、GitHub同步、方向关系、MODID版本与基础差异测试 | TEST-011；测试明确接受“从目标Mod编辑其他来源Mod的入向关系”但不验证来源项目授权，锁定SEC-013行为 |
| `internal/httpapi/mod_icon_mirror.go` | internal/httpapi | 253 | 已完成 | 外部项目图标/作者头像HTTPS白名单下载、图像解码验证、OSS去重持久化与站内对象复用 | SEC-012；站内OSS URL复用仅凭对象Key和active/clean状态，不校验对象类别、所有者或请求用途，已知Key可跨边界复用 |
| `internal/httpapi/mod_icon_mirror_test.go` | internal/httpapi | 84 | 已完成 | 外部图标Host白名单、PNG/JPEG/GIF真解码和OSS端点路径安全测试 | SEC-012；未覆盖站内对象跨用户/跨类别复用、OSS写入后数据库失败清理和重定向下载 |
| `internal/httpapi/mod_import_config.go` | internal/httpapi | 218 | 已完成 | Modrinth/CurseForge/GitHub导入源、超时与密钥的加密设置读取、规范化、脱敏和后台保存 | SEC-011；允许admin.config.write把保留的既有密钥与任意公共HTTPS BaseURL组合 |
| `internal/httpapi/mod_import_handlers.go` | internal/httpapi | 300 | 已完成 | 各项目类型外部元数据导入任务创建、用户隔离查询、来源URL白名单及Outbox入队 | LEGACY-006；关闭Outbox仍直接Core NATS发布；项目类型/来源校验和任务所有者边界有效 |
| `internal/httpapi/mod_import_worker.go` | internal/httpapi | 802 | 已完成 | 外部元数据任务扫描/消费、SSRF安全HTTP、三平台转换、状态与许可/兼容性启发式 | BUG-020；SEC-006；SEC-011；ARCH-012；LEGACY-006；neoforge文本同时命中forge，次要API错误静默丢字段，默认Outbox仍并行Core NATS扫描 |
| `internal/httpapi/mod_import_worker_test.go` | internal/httpapi | 151 | 已完成 | 来源URL、头像解码、环境/许可规范化、配置脱敏、私网与跨Host重定向测试 | BUG-020；SEC-011；ARCH-012；未覆盖neoforge文本误判、BaseURL换Host保留密钥及次要API失败降级 |
| `internal/httpapi/mod_localization_publish_integration_test.go` | internal/httpapi | 101 | 已完成 | Mod已批准修订发布后本地化投影的PostgreSQL集成测试 | TEST-043；成功路径有效，并发、失败和审核边界未覆盖 |
| `internal/httpapi/mod_permission_test.go` | internal/httpapi | 54 | 已完成 | 验证全局、项目作用域与通配编辑权限以及非权限角色标签不会放行 | 无新增问题；覆盖核心canEditMod正反例，但不覆盖待审读取与审核权限一致性 |
| `internal/httpapi/mod_resource_image_spec_test.go` | internal/httpapi | 110 | 已完成 | 模组资料图片类型、尺寸和JPEG拒绝规则测试 | 无新增Finding；规则覆盖与实现一致 |
| `internal/httpapi/mod_review_queue_handlers.go` | internal/httpapi | 311 | 已完成 | 统一内容审核队列的跨业务UNION、权限预过滤、搜索筛选、分面与分页响应 | BUG-021；PERF-012；数据库先按最早时间截断2000条，之后才在内存搜索、筛选和分页，后续待审项及分面/总数会被静默排除 |
| `internal/httpapi/mod_revision_handlers.go` | internal/httpapi | 618 | 已完成 | Mod修订提交、乐观并发基线、历史/比较、项目审核发布、快照落库和审核审计通知 | BUG-022；BUG-023；PERF-013；项目级审核者可审核却看不到待审历史/比较，提交后回读错误被吞并返回空成功体，历史无分页且每行多次相关子查询 |
| `internal/httpapi/modid_validation.go` | internal/httpapi | 231 | 已完成 | 导入命名空间统计、候选归一化、多候选确认暂停、用户绑定确认和Outbox重新入队 | LEGACY-006；确认提交后同时写Outbox并主动dispatch；确认哈希绑定任务/模组/创建用户且24小时失效边界有效 |
| `internal/httpapi/modid_validation_test.go` | internal/httpapi | 34 | 已完成 | MODID匹配、不匹配、缺配置、多主要候选、排除minecraft与比例归一化单元测试 | TEST-010；未覆盖确认HTTP、任务/用户/数据哈希绑定、过期、重放、Outbox与各导入入口 |
| `internal/httpapi/modpack_handlers.go` | internal/httpapi | 1045 | 已完成 | 整合包目录、创建修订、兼容性/标签/作者/包含模组/图库关联及历史读取 | BUG-022；BUG-027；BUG-028；BUG-029；PERF-018；PERF-019；PERF-020；提交者未作为编辑权限来源 |
| `internal/httpapi/modpack_import_worker.go` | internal/httpapi | 487 | 已完成 | Modrinth/CurseForge整合包元数据与索引下载、解析和站内草稿映射 | SEC-025；BUG-073/074；ARCH-012；TEST-035 |
| `internal/httpapi/modpack_import_worker_test.go` | internal/httpapi | 83 | 已完成 | Modrinth整合包索引模组/环境/兼容性提取、外部URL类型和站内分类映射测试 | 无新增独立问题；只覆盖导入纯函数，不覆盖Handler分类校验、持久化或异步任务 |
| `internal/httpapi/mrpack_generator.go` | internal/httpapi | 183 | 已完成 | 规范MRPack根索引、依赖字段、文件路径/哈希/下载源校验 | BUG-070；已确认根目录结构、加载器依赖键与文件安全边界 |
| `internal/httpapi/mrpack_generator_test.go` | internal/httpapi | 81 | 已完成 | MRPack ZIP结构、依赖键与非法文件元数据单元测试 | TEST-034；未覆盖完整任务与报告生命周期 |
| `internal/httpapi/mrpack_loader_version.go` | internal/httpapi | 122 | 已完成 | MRPack三种加载器具体版本解析、稳定版本选择和NeoForge前缀映射 | BUG-063；PERF-039；TEST-033 |
| `internal/httpapi/mrpack_loader_version_test.go` | internal/httpapi | 19 | 已完成 | Forge/NeoForge加载器版本选择纯函数测试 | TEST-033；未覆盖1.21无补丁版本、26.x和非beta预发布 |
| `internal/httpapi/mrpack_real_integration_test.go` | internal/httpapi | 75 | 已完成 | 显式启用的真实Modrinth文件元数据及MRPack根索引集成测试 | TEST-034；默认跳过且只验证根索引 |
| `internal/httpapi/nats_handlers.go` | internal/httpapi | 96 | 已完成 | 管理员NATS配置读取、保存和运行时重配置 | BUG-056；保存遗漏关键开关且重配置失败仍报告成功 |
| `internal/httpapi/notification_handlers.go` | internal/httpapi | 502 | 已完成 | 系统广播发布/删除、通知列表与已读、未读汇总和普通通知AI翻译 | BUG-027；PERF-028；BUG-043/044；LEGACY-008；system通知翻译前后端均正确禁止 |
| `internal/httpapi/notification_handlers_test.go` | internal/httpapi | 52 | 已完成 | 关注者中文文案、ID排序、AI余额减法和read-all路由注册测试 | TEST-021；未覆盖通知数据库、可见性、分页、翻译与已读行为；LEGACY-001旧文案证据 |
| `internal/httpapi/notification_template_handlers.go` | internal/httpapi | 472 | 已完成 | 通知/通信 | ARCH-001 |
| `internal/httpapi/notification_worker.go` | internal/httpapi | 407 | 已完成 | 异步任务/队列 | LEGACY-001 |
| `internal/httpapi/oauth_handlers.go` | internal/httpapi | 799 | 已完成 | OAuth配置、状态校验、四平台协议适配和账号关联 | 无新增问题 |
| `internal/httpapi/oauth_next_test.go` | internal/httpapi | 44 | 已完成 | OAuth登录后站内跳转安全测试 | 无新增问题 |
| `internal/httpapi/oss_access.go` | internal/httpapi | 212 | 已完成 | OSS对象访问解析、私有下载、公开内联与栅格重定向 | SEC-029；ARCH-021；TEST-037 |
| `internal/httpapi/oss_access_test.go` | internal/httpapi | 73 | 已完成 | OSS访问URL模式及非法配置基础测试 | SEC-029；TEST-037；未覆盖ESA链接重放和私有对象外泄边界 |
| `internal/httpapi/oss_deletion_outbox.go` | internal/httpapi | 227 | 已完成 | OSS删除Outbox、租约领取、重试与文件墓碑生命周期 | BUG-081；OPS-016；TEST-037 |
| `internal/httpapi/oss_deletion_outbox_test.go` | internal/httpapi | 37 | 已完成 | OSS删除重试退避和错误截断测试 | OPS-016；TEST-037；明确验证无限退避上限但未验证终止失败态 |
| `internal/httpapi/oss_file_binding.go` | internal/httpapi | 124 | 已完成 | 受信任栅格OSS文件绑定查询与上传者边界 | 无新增Finding；调用方仍需核对AllowAnyUploader授权来源 |
| `internal/httpapi/oss_file_binding_test.go` | internal/httpapi | 196 | 已完成 | OSS栅格绑定SQL安全条件和上传者约束单元测试 | TEST-037；仅验证查询形状，未覆盖真实数据库与跨调用方授权 |
| `internal/httpapi/oss_handlers.go` | internal/httpapi | 2357 | 已完成 | OSS配置、直传/分片完成、额度、图片与举报证据校验、目录/扫描/下载统计 | SEC-028；BUG-079/080；ARCH-020；PERF-044；既有SEC-012/BUG-032证据链 |
| `internal/httpapi/oss_multipart.go` | internal/httpapi | 163 | 已完成 | OSS分片会话创建、逐片签名和完成后对象校验 | OPS-018；TEST-037 |
| `internal/httpapi/oss_multipart_test.go` | internal/httpapi | 31 | 已完成 | OSS分片阈值、旧客户端opt-in和upload ID测试 | LEGACY-015；OPS-018；TEST-037 |
| `internal/httpapi/oss_object_paths.go` | internal/httpapi | 190 | 已完成 | OSS项目、资料、头像及通用资源的权威对象路径生成 | 无新增Finding；路径分段规范化和遍历防护合理 |
| `internal/httpapi/oss_paths_test.go` | internal/httpapi | 81 | 已完成 | OSS路径布局、遍历字符和异常公共ID规范化测试 | 无新增Finding；覆盖核心路径安全分支 |
| `internal/httpapi/oss_raster_safety_test.go` | internal/httpapi | 96 | 已完成 | 栅格解码失败关闭和蓝图封面同步测试 | SEC-030；TEST-037；未覆盖GIF后续帧与总解码预算 |
| `internal/httpapi/oss_rehome.go` | internal/httpapi | 172 | 已完成 | 模组资料图片对象迁址与旧对象延迟删除 | LEGACY-014；OPS-017；TEST-037 |
| `internal/httpapi/owned_content_handlers.go` | internal/httpapi | 60 | 已完成 | 用户自有内容计数与分类跳转响应 | 无新增问题 |
| `internal/httpapi/permission_comparison_handlers.go` | internal/httpapi | 222 | 已完成 | 当前用户与角色权限规则对比API | 登录用户可枚举角色权限属于产品边界，需结合前端入口后最终判定 |
| `internal/httpapi/permission_defaults_handlers.go` | internal/httpapi | 168 | 已完成 | 默认注册/封禁角色和用户安全状态管理 | BUG-002；SEC-004 |
| `internal/httpapi/permission_rules.go` | internal/httpapi | 455 | 已完成 | 认证/权限 | project access |
| `internal/httpapi/permission_runtime_test.go` | internal/httpapi | 174 | 已完成 | 权限、OSS转换、APNG与角色轨道混合单元测试 | STYLE-004 |
| `internal/httpapi/popularity_worker.go` | internal/httpapi | 265 | 已完成 | 热度和评论热度持久队列轮询、时间衰减入队及站点统计刷新 | OPS-010；失败无限重试无终止状态；PERF-034 |
| `internal/httpapi/presence_handlers.go` | internal/httpapi | 75 | 已完成 | 游客/登录用户站点在线心跳、用户会话快照和公开在线隐私映射 | SEC-017；游客自报visitor可伪造统计并无界放大本地/Redis集合 |
| `internal/httpapi/presence_handlers_test.go` | internal/httpapi | 38 | 已完成 | 公开在线三态隐私与默认数据库快照间隔单元测试 | TEST-022；未覆盖端点、伪造访客、Redis回退和数据库快照 |
| `internal/httpapi/profile_handlers.go` | internal/httpapi | 313 | 已完成 | 公开用户资料、粉丝/关注分页、关注事务与通知设置 | PERF-003 |
| `internal/httpapi/profile_settings_handlers.go` | internal/httpapi | 424 | 已完成 | 用户档案设置、头像/背景文件绑定与修订发布 | SEC-004（补充权限版本刷新失败证据） |
| `internal/httpapi/profile_showcase_handlers.go` | internal/httpapi | 399 | 已完成 | 公开作品陈列、认领作者、上传内容与贡献历史 | PERF-002；SEC-005 |
| `internal/httpapi/profile_showcase_handlers_test.go` | internal/httpapi | 50 | 已完成 | 贡献年份/日期范围纯函数测试 | 未覆盖公开可见性和项目查询计划，纳入TEST-002 |
| `internal/httpapi/progression_handlers.go` | internal/httpapi | 499 | 已完成 | 用户任务、等级配置、任务配置和活动事件后台 | SEC-040；ARCH-027；PERF-056；既有BUG-059/060；TEST-044 |
| `internal/httpapi/progression_handlers_test.go` | internal/httpapi | 22 | 已完成 | 任务条件单一正常路径纯函数测试 | TEST-044；未覆盖经济、权限、事务或配置重算 |
| `internal/httpapi/project_authorship_handlers.go` | internal/httpapi | 301 | 已完成 | 项目作者/团队敏感关系同步、待审核列表与审核 | DB-002 |
| `internal/httpapi/project_automation_handlers.go` | internal/httpapi | 358 | 已完成 | 项目外部来源绑定、三类自动更新设置、手动运行和运行记录接口 | SEC-015；ARCH-013；DEAD-004；GET初始化会写入并默认启用任务 |
| `internal/httpapi/project_automation_worker.go` | internal/httpapi | 832 | 已完成 | 项目自动更新调度、租约重试、三方兼容/日志同步、文件镜像与扫描提升 | ARCH-013；PERF-026；BUG-035、BUG-038；DB-006；BUG-033/034和OPS-003证据扩展 |
| `internal/httpapi/project_changelog_handlers.go` | internal/httpapi | 599 | 已完成 | 统一项目更新日志创建/修订/历史、多语言分类、审核快照和项目更新事件 | BUG-022、BUG-033、BUG-034、PERF-024；项目权限不以提交者身份放行 |
| `internal/httpapi/project_changelog_handlers_test.go` | internal/httpapi | 66 | 已完成 | 更新日志快照、语言、分类输入和目标类型规范化测试 | TEST-017；未覆盖HTTP、数据库、审核、自动同步和分页 |
| `internal/httpapi/project_editor_application_handlers.go` | internal/httpapi | 432 | 已完成 | 项目/资料 | LEGACY-001, PERF-001 |
| `internal/httpapi/project_file_handlers.go` | internal/httpapi | 817 | 已完成 | 项目站内/Modrinth/CurseForge文件列表、上传绑定、删除、下载和供应商缓存 | BUG-032、BUG-033、PERF-023、REUSE-003；权限和内部下载clean检查有效 |
| `internal/httpapi/project_file_handlers_test.go` | internal/httpapi | 83 | 已完成 | 项目文件类型/扩展名/外部兼容文本/URL/版本排序纯函数测试 | TEST-016；没有HTTP、数据库、扫描状态、权限、外部供应商与Outbox测试 |
| `internal/httpapi/project_follow_handlers.go` | internal/httpapi | 179 | 已完成 | 项目关注创建、取消、状态和本人关注列表 | BUG-027；BUG-036；DEAD-003；隐藏目标无法取消且通知开关无修改入口 |
| `internal/httpapi/project_maintenance_automation.go` | internal/httpapi | 232 | 已完成 | 基于外部活动时间自动切换低频/停更、人工覆盖与恢复 | BUG-039；状态直接改主表，仅activity记录autobot且无修订/更新事件/应用日志 |
| `internal/httpapi/project_maintenance_automation_integration_test.go` | internal/httpapi | 109 | 已完成 | 自动维护低频/停更/恢复与人工覆盖数据库转换测试 | TEST-019；验证changed_by但未验证修订、事件、通知和操作日志缺失 |
| `internal/httpapi/project_maintenance_automation_test.go` | internal/httpapi | 41 | 已完成 | 维护状态日历阈值和无序provider活动时间纯函数测试 | TEST-019；不覆盖任务、发布修订和审计事件 |
| `internal/httpapi/project_permission_model_test.go` | internal/httpapi | 71 | 已完成 | 项目权限模型结构约束与后台手工授权保留测试 | 无新增问题 |
| `internal/httpapi/project_review_permissions.go` | internal/httpapi | 7 | 已完成 | 项目免审权限组合判断 | 无新增问题 |
| `internal/httpapi/project_review_permissions_test.go` | internal/httpapi | 19 | 已完成 | 项目免审权限组合单元测试 | 无新增问题 |
| `internal/httpapi/project_role_assignment_source_test.go` | internal/httpapi | 117 | 已完成 | 项目创建函数禁止直接写角色绑定的AST测试 | TEST-004 |
| `internal/httpapi/project_update_events.go` | internal/httpapi | 147 | 已完成 | 公开项目实质更新事件合并、通知任务和Outbox事务写入 | BUG-027；MAP-006；30秒同操作者发布批次合并使用项目级事务锁 |
| `internal/httpapi/project_update_integration_test.go` | internal/httpapi | 70 | 已完成 | 项目更新事件时间窗合并、通知任务和Outbox事务集成测试 | TEST-018；未运行Worker、偏好、locale、重试或隐藏目标路径 |
| `internal/httpapi/project_update_notification_worker.go` | internal/httpapi | 246 | 已完成 | 项目更新通知任务领取、关注者分页、本地化模板、幂等通知和未读广播 | OPS-004；BUG-027；BUG-037；PERF-025；失败次数回滚导致无限重试 |
| `internal/httpapi/public_identity.go` | internal/httpapi | 92 | 已完成 | 公开ID解析、内部ID反查与修订ID边界转换 | ARCH-003 |
| `internal/httpapi/rating_handlers.go` | internal/httpapi | 452 | 已完成 | 评分目标解析、评分增删改、维度校验、汇总和评价列表 | MAP-010；PERF-050；LEGACY-017；TEST-040 |
| `internal/httpapi/rating_handlers_test.go` | internal/httpapi | 100 | 已完成 | 评分维度、目标类型别名和请求校验纯函数测试 | MAP-010；LEGACY-017；TEST-040；未覆盖数据库、权限、触发器和路由 |
| `internal/httpapi/realtime_handlers.go` | internal/httpapi | 132 | 已完成 | SSE用户事件流、进程内Hub及Core NATS跨实例广播 | SEC-018；BUG-046；OPS-007；同实例回声重复、无连接预算且订阅失败不恢复 |
| `internal/httpapi/recipe_render_layout.go` | internal/httpapi | 262 | 已完成 | 合成表导入快照的批量布局/槽位/候选项API装配 | ARCH-032；两条集合查询避免了逐合成表N+1 |
| `internal/httpapi/reliable_notification.go` | internal/httpapi | 33 | 已完成 | 通知/通信 | outbox |
| `internal/httpapi/resource_versions.go` | internal/httpapi | 82 | 已完成 | 目录资源批量装饰全部Mod内容版本、详情、名称与OSS文件 | DEAD-010；hasManualDetail扫描后未使用；受PERF-010无界调用方影响 |
| `internal/httpapi/response.go` | internal/httpapi | 82 | 已完成 | 统一JSON成功/错误响应、后端标识头与8MiB严格JSON解码 | SEC-007；响应写入错误普遍忽略但连接已开始后无法可靠改写 |
| `internal/httpapi/review_attachment_security.go` | internal/httpapi | 97 | 已完成 | 审核附件所有权/所属关系和安全扫描状态查询 | 无新增问题 |
| `internal/httpapi/review_attachment_security_test.go` | internal/httpapi | 109 | 已完成 | 四类审核附件安全查询结构测试 | SQL结构测试不能替代真实IDOR集成；纳入TEST-002 |
| `internal/httpapi/review_config_test.go` | internal/httpapi | 29 | 已完成 | 项目类型审核开关与未知类型失败关闭测试 | 无新增Finding；未知类型默认要求审核 |
| `internal/httpapi/review_lock_handlers.go` | internal/httpapi | 212 | 已完成 | 基础设施/通用 | LEGACY-001 |
| `internal/httpapi/review_permissions.go` | internal/httpapi | 145 | 已完成 | 全局/项目级审核权限、项目ID解析与自审限制 | 失败关闭；项目类型范围需结合各审核队列继续交叉验证 |
| `internal/httpapi/review_permissions_test.go` | internal/httpapi | 88 | 已完成 | 审核自审阻止、队列过滤和项目权限解析测试 | 无新增问题 |
| `internal/httpapi/review_queue_integration_test.go` | internal/httpapi | 52 | 已完成 | 在可选PostgreSQL环境执行完整审核队列UNION并验证列扫描契约 | TEST-009；只验证SQL可执行和列形状，不覆盖超过2000条、搜索分页完整性或权限边界结果 |
| `internal/httpapi/review_resolution_integration_test.go` | internal/httpapi | 52 | 已完成 | 审核结论写入的PostgreSQL调用烟雾测试 | TEST-009；未验证状态、事件、幂等和权限语义 |
| `internal/httpapi/role_track_handlers.go` | internal/httpapi | 294 | 已完成 | 权限组线路配置与用户线路升降级 | BUG-004；SEC-004（权限版本刷新） |
| `internal/httpapi/search_index.go` | internal/httpapi | 200 | 已完成 | Typesense各领域查询、可见性过滤与PostgreSQL回退选择 | 无新增独立问题；公开查询均添加状态/提交者边界 |
| `internal/httpapi/seed_crawler_handlers.go` | internal/httpapi | 154 | 已完成 | 填充爬虫配置、手工run、run历史和候选后台接口 | BUG-027；PERF-027；DEAD-005；run/candidate游标缺结束错误且配置并发无执行方 |
| `internal/httpapi/seed_crawler_worker.go` | internal/httpapi | 513 | 已完成 | Modrinth随机候选发现、元数据导入、AI翻译、autobot草稿/自动提交和来源绑定 | SEC-016；OPS-005/006；BUG-040/041/042；ARCH-013；多步状态和预算非原子 |
| `internal/httpapi/seed_crawler_worker_test.go` | internal/httpapi | 93 | 已完成 | 爬虫项目类型、严格下载阈值、随机窗口和Modrinth搜索请求单元测试 | TEST-020；未覆盖数据库任务、租约、AI预算、autobot创建和多步提交 |
| `internal/httpapi/server.go` | internal/httpapi | 670 | 已完成 | 基础设施/通用 | API inventory |
| `internal/httpapi/server_catalog_handlers.go` | internal/httpapi | 1020 | 已完成 | 服务器设置、主动探测、提交/编辑、模组与证明附件关联、公开目录/详情及在线历史 | BUG-030；PERF-021；探测委托serverprobe公网地址保护；提交者未作为编辑权限来源 |
| `internal/httpapi/server_catalog_handlers_test.go` | internal/httpapi | 124 | 已完成 | 服务器提交/更新规范化、证明要求、探测证据优先和模组筛选解析单元测试 | TEST-015；未覆盖数据库事务、权限、客户端伪造证据和大分页 |
| `internal/httpapi/server_cors_test.go` | internal/httpapi | 78 | 已完成 | CORS写请求头、凭据和认证/RBAC响应头暴露测试 | 聚焦边界有效；不替代真实跨域浏览器与失效Session集成测试 |
| `internal/httpapi/server_mod_save_integration_test.go` | internal/httpapi | 116 | 已完成 | 服务器模组解析/未解析关系批量保存和读取集成测试 | TEST-015；覆盖保存形状但未覆盖来源伪造、冲突来源和探测快照删除 |
| `internal/httpapi/server_probe_scheduler.go` | internal/httpapi | 163 | 已完成 | 五分钟服务器探测领取、32路并发执行、状态持久化与历史清理 | BUG-031；完整清单不会移除旧机器模组且领取遍历未检查rows.Err |
| `internal/httpapi/server_review_handlers.go` | internal/httpapi | 207 | 已完成 | 服务器审核队列、审核状态转换、证明附件读取和受权预签名 | PERF-022；列表逐项读取附件/链接/模组且携带完整正文，权限由server.review路由强制 |
| `internal/httpapi/server_review_handlers_integration_test.go` | internal/httpapi | 58 | 已完成 | 服务器审核列表SQL形状集成测试 | TEST-015；仅在现有库执行查询，不创建断言数据、不走HTTP权限/审核流程 |
| `internal/httpapi/settings_cache.go` | internal/httpapi | 54 | 已完成 | 公开系统设置的持久化版本化Redis缓存与失效 | 无新增问题；白名单Key、数据库版本事实和错误回源边界明确 |
| `internal/httpapi/simple_project_catalog_integration_test.go` | internal/httpapi | 45 | 已完成 | 六类简单项目目录COUNT与列表SQL的远程PostgreSQL语法烟雾测试 | TEST-014；未构造业务数据，未验证过滤/权限/大响应/关联读取失败和分页语义 |
| `internal/httpapi/simple_project_handlers.go` | internal/httpapi | 1092 | 已完成 | 插件/地图/材质包/光影/数据包/附属项目目录、创建修订、关联资料、图库、历史及导入资产处理 | BUG-027；BUG-028；PERF-018；PERF-019；MAP-005；项目提交者未作为编辑权限来源 |
| `internal/httpapi/simple_project_import_worker.go` | internal/httpapi | 393 | 已完成 | 简单项目Modrinth/CurseForge导入与结构化草稿映射 | ARCH-012；BUG-121；REUSE-004 |
| `internal/httpapi/simple_project_import_worker_test.go` | internal/httpapi | 147 | 已完成 | 简单项目外部导入URL、分类、版本、链接和草稿规范化纯函数测试 | 待与生产Worker结论合并；未覆盖HTTP、数据库、队列、重试和外部请求边界 |
| `internal/httpapi/site_affairs_handlers.go` | internal/httpapi | 293 | 已完成 | 关于本站与站点更新日志公开读取、语言回退和后台写入 | BUG-102/103/104；ARCH-029；PERF-058；TEST-046 |
| `internal/httpapi/site_settings_handlers.go` | internal/httpapi | 88 | 已完成 | 公开品牌配置、后台更新和Logo上传权限探测 | OPS-008/SEC-019调用边界；后端严格限制Logo公开路径并版本化失效缓存 |
| `internal/httpapi/site_settings_handlers_test.go` | internal/httpapi | 36 | 已完成 | 品牌名称与Logo路径规范化单元测试 | TEST-024；未覆盖数据库、权限、缓存和前端上传 |
| `internal/httpapi/skin_handlers.go` | internal/httpapi | 1930 | 已完成 | 皮肤库、衣柜、玩家档案、纹理装备与启动器凭据/Session管理 | SEC-034；BUG-086/087/088；PERF-047/048/049；既有ARCH-021；TEST-039 |
| `internal/httpapi/skin_handlers_test.go` | internal/httpapi | 92 | 已完成 | 纹理Patch语义、API契约别名清理和路由注册测试 | TEST-039；未覆盖权限、审核、私有纹理和生命周期 |
| `internal/httpapi/skin_texture.go` | internal/httpapi | 314 | 已完成 | Minecraft PNG纹理解码重编码、内容寻址OSS存储和UUID生成 | BUG-088；既有ARCH-021/BUG-081；TEST-039 |
| `internal/httpapi/skin_texture_test.go` | internal/httpapi | 117 | 已完成 | 皮肤/披风PNG规范化、尺寸拒绝、哈希和UUID测试 | TEST-039；未覆盖OSS/数据库故障和共享Blob所有权 |
| `internal/httpapi/sticker_handlers.go` | internal/httpapi | 626 | 已完成 | 表情目录、后台增改删、OSS图片验证和Markdown引用检查 | SEC-041；BUG-098/099/100；ARCH-028；PERF-057；TEST-045 |
| `internal/httpapi/system_settings.go` | internal/httpapi | 27 | 已完成 | 系统敏感设置JSON的统一加密与解密边界 | 无新增问题；复用AES-GCM设置加密且错误向上传递 |
| `internal/httpapi/timezone.go` | internal/httpapi | 20 | 已完成 | IANA用户时区标准化与内置tzdata验证 | 无新增问题；注册和设置入口复用同一校验 |
| `internal/httpapi/timezone_test.go` | internal/httpapi | 16 | 已完成 | 常用IANA时区与非法路径/Local拒绝测试 | 无新增问题 |
| `internal/httpapi/unread_reconciliation.go` | internal/httpapi | 117 | 已完成 | 按用户游标周期校准通知/私信未读缓存及管理员定向校准 | PERF-029；游标结束错误检查正确，但每用户两个相关COUNT会全站循环 |
| `internal/httpapi/unread_reconciliation_integration_test.go` | internal/httpapi | 37 | 已完成 | 远程PostgreSQL与miniredis未读对账查询烟雾测试 | TEST-021；不构造数据、不断言真值/漂移/游标循环和故障恢复 |
| `internal/httpapi/unresolved_reference_handlers.go` | internal/httpapi | 103 | 已完成 | 两套未解析引用事实的后台合并、过滤、总数和分页 | BUG-105/108；PERF-059；TEST-047 |
| `internal/httpapi/user_block_handlers.go` | internal/httpapi | 203 | 已完成 | 拉黑关系、评论过滤和资源所有者评论阻止规则 | 与既定Mod/资料及教程/讨论边界一致；无新增问题 |
| `internal/httpapi/user_block_integration_test.go` | internal/httpapi | 178 | 已完成 | 拉黑、关注清理、评论可见性与发言边界集成测试 | 无新增问题 |
| `internal/httpapi/user_card_handlers.go` | internal/httpapi | 213 | 已完成 | 公开/私有用户卡片、在线隐私、统计聚合与缓存 | 需结合大数据查询计划继续验证；无新增确定问题 |
| `internal/httpapi/user_card_handlers_test.go` | internal/httpapi | 30 | 已完成 | 用户卡片TTL边界测试 | 测试范围很窄，纳入TEST-002总体覆盖缺口 |
| `internal/httpapi/user_handlers.go` | internal/httpapi | 289 | 已完成 | Markdown草稿、用户OSS文件列表/额度/签名访问与删除 | BUG-003；ARCH-004 |
| `internal/httpapi/user_statistics_handlers.go` | internal/httpapi | 361 | 已完成 | 公开/私有用户统计、活跃连续天数、内容/社区/审核/经验聚合 | BUG-005；BUG-006；ARCH-005 |
| `internal/httpapi/user_statistics_handlers_test.go` | internal/httpapi | 32 | 已完成 | 统计时间范围和连续活跃纯函数测试 | 未覆盖公开隐私、SQL聚合与错误路径；纳入TEST-002 |
| `internal/httpapi/yggdrasil_auth.go` | internal/httpapi | 703 | 已完成 | 启动器认证、Token签发/刷新/验证/撤销、登录限流与并发凭据检查 | BUG-007；未发现网站Session与启动器Token错误混用 |
| `internal/httpapi/yggdrasil_profiles.go` | internal/httpapi | 515 | 已完成 | 启动器档案查询、签名纹理属性、授权纹理上传/删除和OSS公开读取 | 无新增问题；OSS对象回滚/删除生命周期待皮肤存储模块复核 |
| `internal/httpapi/yggdrasil_service.go` | internal/httpapi | 472 | 已完成 | Yggdrasil路由、端点/限额/代理校验、RSA签名、Token和协议响应基础层 | 无新增问题；SHA-1仅协议签名兼容 |
| `internal/httpapi/yggdrasil_service_test.go` | internal/httpapi | 228 | 已完成 | UUID/Token/RSA/端点/响应/元数据/纹理URL纯函数测试 | 缺少authenticate-refresh-join完整数据库集成及BUG-007失败路径；TEST-002 |
| `internal/httpapi/yggdrasil_session.go` | internal/httpapi | 137 | 已完成 | Yggdrasil服务器join/hasJoined会话建立、IP约束和短TTL校验 | BUG-007 |
| `internal/httpapi/yggdrasil_settings_handlers.go` | internal/httpapi | 282 | 已完成 | Yggdrasil后台配置校验、密钥轮换/密封持久化和运行时原子替换 | 无新增问题；路由权限需与server注册继续交叉核对 |
| `internal/httpapi/yggdrasil_settings_handlers_test.go` | internal/httpapi | 64 | 已完成 | Yggdrasil配置反序列化、规范化和RSA密钥往返测试 | 未覆盖后台权限、持久化失败及密钥轮换影响；TEST-002 |
| `internal/mailer/mailer.go` | internal/mailer | 126 | 已完成 | SMTP纯文本邮件、邮箱/标题校验、STARTTLS/Auth和超时 | BUG-050调用配置开关边界；底层防Header注入并要求配置TLS时失败关闭 |
| `internal/mailer/mailer_test.go` | internal/mailer | 28 | 已完成 | 邮箱解析与Header注入单元测试 | TEST-024；未覆盖SMTP/TLS/Auth/超时和Enabled行为 |
| `internal/progression/service.go` | internal/progression | 469 | 已完成 | 活动批次驱动任务进度、经验/货币奖励、等级和角色轨道同步 | BUG-059/060；ARCH-014；MAP-003 |
| `internal/progression/service_test.go` | internal/progression | 54 | 已完成 | 任务条件匹配与用户时区周期键单元测试 | TEST-031；未覆盖数据库奖励、幂等、并发和手工角色保护 |
| `internal/querycache/cache.go` | internal/querycache | 678 | 已完成 | 基础设施/通用 | SEC-003 |
| `internal/querycache/cache_test.go` | internal/querycache | 86 | 已完成 | 本地缓存、固定窗口限流、游客Presence和节流基础测试 | SEC-017测试缺口延续；未覆盖游客集合容量和Redis故障多实例放大 |
| `internal/querycache/shared_version_test.go` | internal/querycache | 65 | 已完成 | 跨实例permission version指针与无Redis权威回源测试 | 无新增问题；验证不使用进程本地权限版本 |
| `internal/querycache/state.go` | internal/querycache | 426 | 已完成 | 基础设施/通用 | cache failure |
| `internal/querycache/state_test.go` | internal/querycache | 96 | 已完成 | 多Session Presence、未读缓存校准和命名空间测试 | TEST-022既有缺口；未覆盖端点伪造、Redis失败和多实例漂移 |
| `internal/queue/events.go` | internal/queue | 37 | 已完成 | 队列事件Envelope、上下文事件ID和Payload解包 | LEGACY-011；保留裸JSON双协议兼容 |
| `internal/queue/events_test.go` | internal/queue | 39 | 已完成 | Envelope往返、裸Payload兼容与随机事件ID测试 | LEGACY-011；测试锁定内部旧协议 |
| `internal/queue/nats.go` | internal/queue | 608 | 已完成 | 异步任务/队列 | OPS-001 |
| `internal/queue/nats_integration_test.go` | internal/queue | 97 | 已完成 | JetStream显式ACK重投、消费幂等和死信集成测试 | TEST-026；默认环境变量门控，未覆盖发布侧故障/重连 |
| `internal/queue/nats_test.go` | internal/queue | 58 | 已完成 | NATS配置默认任务、限制和去重单元测试 | 无新增问题；未验证运行连接 |
| `internal/queue/outbox.go` | internal/queue | 203 | 已完成 | 事务Outbox入队、SKIP LOCKED领取、发布重试和死信 | OPS-009；失败状态持久化错误被忽略且指标提前成功 |
| `internal/queue/outbox_integration_test.go` | internal/queue | 128 | 已完成 | Outbox回滚、成功发布和消费者死信集成测试 | TEST-026；未隔离通用claim且缺发布失败/并发/租约恢复 |
| `internal/runtimelog/store.go` | internal/runtimelog | 130 | 已完成 | 标准库日志的有界进程内环形缓冲 | BUG-054；仅捕获log而遗漏slog |
| `internal/runtimelog/store_test.go` | internal/runtimelog | 31 | 已完成 | 运行日志环形缓冲与级别推断测试 | TEST-025；未覆盖slog和完整HTTP链 |
| `internal/searchindex/schema.go` | internal/searchindex | 87 | 已完成 | Typesense五类集合Schema与投影版本 | MAP-008；集合/文档类型注册分散 |
| `internal/searchindex/typesense.go` | internal/searchindex | 318 | 已完成 | Typesense HTTP客户端、别名、批量导入、删除和搜索协议 | 无新增独立问题；响应有8MiB读取上限且失败降低ready |
| `internal/searchindex/typesense_test.go` | internal/searchindex | 88 | 已完成 | Typesense搜索/分面、批量失败和可用状态单元测试 | TEST-030；未覆盖重建与并发代次 |
| `internal/searchindex/worker.go` | internal/searchindex | 544 | 已完成 | 搜索集合重建、别名切换、持久队列同步与五类投影加载 | BUG-058；OPS-011；PERF-037；MAP-008；DEAD-009 |
| `internal/searchindex/worker_integration_test.go` | internal/searchindex | 34 | 已完成 | 服务器搜索投影空ID数据库集成测试 | TEST-030；仅验证空选择不全表加载 |
| `internal/security/password.go` | internal/security | 147 | 已完成 | Argon2id密码/验证码哈希、旧PBKDF2验证和参数上限 | LEGACY-004 |
| `internal/security/password_test.go` | internal/security | 29 | 已完成 | Argon2id生成验证与恶意高资源参数拒绝测试 | 未覆盖PBKDF2残留和参数边界全组合；纳入TEST-002 |
| `internal/security/settings.go` | internal/security | 82 | 已完成 | 系统敏感设置AES-GCM信封加密与主密钥派生 | 无新增问题 |
| `internal/security/settings_test.go` | internal/security | 31 | 已完成 | 设置加解密往返与明文拒绝测试 | 无新增问题 |
| `internal/security/token.go` | internal/security | 154 | 已完成 | HS256 Session Token签发解析、Claims校验与Bearer提取 | TEST-006 |
| `internal/serverprobe/configuration.go` | internal/serverprobe | 586 | 已完成 | 现代Minecraft Configuration登录探测、压缩包帧、NeoForge/Fabric通道与注册表命名空间提取 | SEC-014；每包/总字节/包数有界，但计数可触发远高于输入的预分配 |
| `internal/serverprobe/configuration_fabric.go` | internal/serverprobe | 142 | 已完成 | Fabric分块注册表同步解析、命名空间展开和计数校验 | SEC-014；64MiB输入可派生最多500万字符串并产生高堆峰值 |
| `internal/serverprobe/configuration_nbt.go` | internal/serverprobe | 158 | 已完成 | Configuration动态注册表与有深度/长度限制的NBT跳过器 | SEC-014；动态注册表按未与剩余字节关联的一百万计数预分配 |
| `internal/serverprobe/configuration_test.go` | internal/serverprobe | 141 | 已完成 | 代理协议回退、NeoForge/Fabric注册表与命名空间过滤测试 | TEST-015；没有恶意计数、近64MiB、包数/压缩/内存预算测试 |
| `internal/serverprobe/forge.go` | internal/serverprobe | 246 | 已完成 | Forge legacy/compact状态JSON、UTF-16优化编码和Mod/网络通道解析 | 外层2MiB与解码4MiB限制使处理有界；无新增独立问题 |
| `internal/serverprobe/forge_test.go` | internal/serverprobe | 75 | 已完成 | Forge UTF-16、优化位流、Mod通道和FML3字符串版本测试 | 未覆盖近4MiB、畸形计数和截断输入，但由统一TEST-015记录协议模糊测试缺口 |
| `internal/serverprobe/probe.go` | internal/serverprobe | 498 | 已完成 | Minecraft状态探测、地址/SRV解析、公网IP校验、协议帧、状态JSON与Forge摘要整合 | 公网IP全解析后固定IP拨号可防DNS重绑定；SEC-014由Configuration解析资源上限产生 |
| `internal/serverprobe/probe_test.go` | internal/serverprobe | 76 | 已完成 | 地址规范化、私网拒绝、SRV握手主机、favicon和空Forge状态测试 | TEST-015；未覆盖混合公私DNS、多保留IPv6、重绑定与真实恶意服务端 |
| `internal/systemactor/autobot.go` | internal/systemactor | 6 | 已完成 | autobot权威用户名和邮箱常量 | 无新增问题；避免各自动化模块重复硬编码身份 |
| `internal/userstats/reconcile.go` | internal/userstats | 165 | 已完成 | 按用户批量重建内容创建事实和活动累计统计 | BUG-006 |
| `internal/userstats/reconcile_test.go` | internal/userstats | 30 | 已完成 | 统计重算参数及SQL片段测试 | 测试名称称exact但未验证action_counts一致性；BUG-006 |
| `load-results/anti-abuse-comment-aggregated.json` | load-results/anti-abuse-comment-aggregated.json | 49 | 已完成 | 最终50并发评论反滥用负载结果 | TEST-050 |
| `load-results/anti-abuse-comment-load.json` | load-results/anti-abuse-comment-load.json | 61 | 已完成 | 反滥用阶梯评论负载原始结果 | TEST-050 |
| `load-results/anti-abuse-comment-optimized.json` | load-results/anti-abuse-comment-optimized.json | 48 | 已完成 | 优化中间态50并发评论负载结果 | TEST-050 |
| `load-results/anti-abuse-comment-overlap.json` | load-results/anti-abuse-comment-overlap.json | 46 | 已完成 | 优化前重叠观察窗口评论负载结果 | TEST-050 |
| `load-results/anti-abuse-comment-peak.json` | load-results/anti-abuse-comment-peak.json | 46 | 已完成 | 峰值评论反滥用负载结果 | TEST-050 |
| `load-results/anti-abuse-crawler-load.json` | load-results/anti-abuse-crawler-load.json | 55 | 已完成 | 爬虫识别与限流负载结果 | TEST-050 |
| `load-results/anti-abuse-db-metrics-overlap.json` | load-results/anti-abuse-db-metrics-overlap.json | 10 | 已完成 | 反滥用重叠阶段数据库活动采样 | 无新增问题 |
| `load-results/anti-abuse-db-metrics-peak.json` | load-results/anti-abuse-db-metrics-peak.json | 10 | 已完成 | 反滥用峰值阶段数据库活动采样 | 无新增问题 |
| `load-results/anti-abuse-db-metrics.json` | load-results/anti-abuse-db-metrics.json | 10 | 已完成 | 反滥用阶梯阶段数据库活动采样 | 无新增问题 |
| `main.go` | main.go | 7 | 已完成 | 后端主程序到app.Run的唯一入口 | 无新增问题 |
| `scripts/anti-abuse-query-analysis.sql` | scripts/anti-abuse-query-analysis.sql | 17 | 已完成 | 反滥用关键查询计划辅助脚本 | TEST-050 |
| `scripts/load-test.mjs` | scripts/load-test.mjs | 316 | 已完成 | 目录、评论、风控、爬虫和基础设施HTTP负载器 | TEST-050 |
| `scripts/process-observer.ps1` | scripts/process-observer.ps1 | 30 | 已完成 | Windows后端进程CPU和工作集峰值观察器 | 无新增问题 |
| `scripts/query-analysis.sql` | scripts/query-analysis.sql | 101 | 已完成 | 目录、统计、Presence、活动和Outbox查询计划脚本 | TEST-050 |
| `test-results/load/catalogs-baseline.json` | test-results/load | 44 | 已完成 | 2并发空数据目录负载结果 | TEST-050 |
| `test-results/load/catalogs-high.json` | test-results/load | 44 | 已完成 | 25并发空数据目录负载结果 | TEST-050 |
| `test-results/load/catalogs-normal.json` | test-results/load | 44 | 已完成 | 10并发空数据目录负载结果 | TEST-050 |
| `test-results/load/catalogs-peak.json` | test-results/load | 44 | 已完成 | 50并发空数据目录负载结果 | TEST-050 |
| `test-results/load/catalogs-sustained.json` | test-results/load | 44 | 已完成 | 20并发持续空数据目录负载结果 | TEST-050 |
| `test-results/load/comments-create-idempotent-fixed.json` | test-results/load | 42 | 已完成 | 幂等锁修复后评论创建负载结果 | 无新增问题 |
| `test-results/load/comments-create-idempotent.json` | test-results/load | 43 | 已完成 | 幂等锁修复前缺陷复现结果 | TEST-050 |
| `test-results/load/comments-create-rate-limit.json` | test-results/load | 43 | 已完成 | 评论创建限流并发结果 | TEST-050 |
| `test-results/load/comments-mutation-baseline.json` | test-results/load | 42 | 已完成 | 评论读写5并发基线结果 | 无新增问题 |
| `test-results/load/comments-mutation-high.json` | test-results/load | 42 | 已完成 | 评论读写15并发结果 | 无新增问题 |
| `test-results/load/comments-read-baseline.json` | test-results/load | 42 | 已完成 | 评论树2并发读取结果 | TEST-050 |
| `test-results/load/comments-read-high.json` | test-results/load | 42 | 已完成 | 评论树10并发读取结果 | TEST-050 |
| `test-results/load/comments-read-peak.json` | test-results/load | 42 | 已完成 | 评论树25并发读取结果 | TEST-050 |
| `test-results/load/comments-read-sustained.json` | test-results/load | 42 | 已完成 | 评论树10并发持续读取结果 | TEST-050 |
| `test-results/load/mixed-auth-baseline.json` | test-results/load | 55 | 已完成 | 认证和后台混合5并发结果 | TEST-050 |
| `test-results/load/mixed-auth-high.json` | test-results/load | 55 | 已完成 | 认证和后台混合15并发结果 | TEST-050 |
| `test-results/load/redis-nats-db-observer.json` | test-results/load | 10 | 已完成 | 基础设施负载数据库活动采样 | 无新增问题 |
| `test-results/load/redis-nats-infrastructure-no-ready.json` | test-results/load | 50 | 已完成 | 去除高频ready后的基础设施峰值结果 | TEST-050 |
| `test-results/load/redis-nats-infrastructure-normal.json` | test-results/load | 50 | 已完成 | 基础设施正常阶梯结果 | 无新增问题 |
| `test-results/load/redis-nats-infrastructure-sustained.json` | test-results/load | 52 | 已完成 | 含ready的基础设施峰值结果 | TEST-050 |
| `test-results/load/redis-nats-infrastructure.json` | test-results/load | 54 | 已完成 | 初始基础设施混合峰值结果 | TEST-050 |

## frontend

| 文件 | 模块 | 行数 | 审查状态 | 主要职责 | 发现问题 |
| --- | --- | ---: | --- | --- | --- |
| `.gitignore` | .gitignore | 43 | 已完成 | 前端依赖、构建产物、环境与IDE排除规则 | OPS-022 |
| `AGENTS.md` | AGENTS.md | 5 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `CLAUDE.md` | CLAUDE.md | 1 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `README.md` | README.md | 41 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `app/[publicId]/page.tsx` | app/[publicId] | 25 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | BUG-124 |
| `app/_components/admin-activity-retention-panel.tsx` | app/_components | 177 | 已完成 | 后台管理 | no-new-finding |
| `app/_components/admin-anti-abuse-panel.tsx` | app/_components | 151 | 已完成 | 后台管理 | BUG-137,STYLE-003 |
| `app/_components/admin-community-panels.tsx` | app/_components | 1701 | 已完成 | 社区/评论 | 作者认领、活动记录、评论管理、经济与任务后台面板；PERF-072；BUG-149；BUG-096 |
| `app/_components/admin-console.tsx` | app/_components | 6092 | 已完成 | 后台管理 | ARCH-002 |
| `app/_components/admin-content-attribute-panel.tsx` | app/_components | 463 | 已完成 | 后台管理 | no-new-finding |
| `app/_components/admin-dashboard-panel.tsx` | app/_components | 424 | 已完成 | 后台管理 | BUG-138 |
| `app/_components/admin-governance-automation-panels.tsx` | app/_components | 109 | 已完成 | 举报/封禁/站务/爬虫/自动更新后台面板与异步表单状态 | BUG-104/110/114/115/116/117/118；ARCH-031；PERF-060；TEST-048 |
| `app/_components/admin-mod-panels.tsx` | app/_components | 283 | 已完成 | 后台管理 | PERF-067 |
| `app/_components/admin-project-authorship-panel.tsx` | app/_components | 84 | 已完成 | 认证/权限 | BUG-132 |
| `app/_components/admin-server-panels.tsx` | app/_components | 195 | 已完成 | 后台管理 | BUG-139,PERF-067 |
| `app/_components/admin-sticker-panel.tsx` | app/_components | 421 | 已完成 | 后台管理 | BUG-140 |
| `app/_components/admin-unresolved-references.tsx` | app/_components | 123 | 已完成 | 未解析引用后台搜索、类型状态筛选和分页表格 | BUG-106/107；PERF-059；TEST-047 |
| `app/_components/admin-yggdrasil-panel.tsx` | app/_components | 143 | 已完成 | 后台管理 | no-new-finding |
| `app/_components/anti-abuse-challenge.tsx` | app/_components | 85 | 已完成 | 人机挑战脚本装载与证明提交 | STYLE-003 |
| `app/_components/aspect-image-crop-dialog.tsx` | app/_components | 230 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/blueprint-detail.tsx` | app/_components | 156 | 已完成 | 前端页面/组件 | BUG-133-related |
| `app/_components/blueprint-library.tsx` | app/_components | 127 | 已完成 | 前端页面/组件 | BUG-141 |
| `app/_components/blueprint-upload.tsx` | app/_components | 118 | 已完成 | 前端页面/组件 | BUG-079-related |
| `app/_components/blueprint-viewer.tsx` | app/_components | 166 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/canonical-recipe-card.tsx` | app/_components | 49 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/catalog-contained-mod-filter.tsx` | app/_components | 60 | 已完成 | 项目/资料 | contained mod catalog filter |
| `app/_components/catalog-list-ui.tsx` | app/_components | 332 | 已完成 | 项目/资料 | catalog list filters and pagination |
| `app/_components/catalog-manual-editors.tsx` | app/_components | 541 | 已完成 | 项目/资料 | BUG-125-related |
| `app/_components/catalog-minecraft-version-filter.tsx` | app/_components | 81 | 已完成 | 项目/资料 | catalog Minecraft version URL filter |
| `app/_components/catalog-resource-catalog.tsx` | app/_components | 244 | 已完成 | 项目/资料 | no-new-finding |
| `app/_components/catalog-resource-icon.tsx` | app/_components | 155 | 已完成 | 项目/资料 | PERF-065 |
| `app/_components/comment-markdown-editor.tsx` | app/_components | 228 | 已完成 | 评论Markdown、表情与OSS附件上传 | PERF-063 |
| `app/_components/comment-section.tsx` | app/_components | 659 | 已完成 | 评论树、楼层、反应、关注与附件展示 | STYLE-003 |
| `app/_components/community-post-catalog.tsx` | app/_components | 282 | 已完成 | 社区内容目录、筛选与资源关联 | LEGACY-005 |
| `app/_components/community-post-detail.tsx` | app/_components | 108 | 已完成 | 社区正文、翻译、悬赏、关注与评论入口 | 无新增问题 |
| `app/_components/community-post-editor.tsx` | app/_components | 201 | 已完成 | 社区内容编辑、资源选择、封面与草稿 | BUG-126 |
| `app/_components/content-history.tsx` | app/_components | 40 | 已完成 | 通用内容修订历史展示 | 无新增问题 |
| `app/_components/content-language-preferences.tsx` | app/_components | 110 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/content-metrics-panel.tsx` | app/_components | 98 | 已完成 | 前端页面/组件 | STYLE-003 |
| `app/_components/creator-catalog.tsx` | app/_components | 116 | 已完成 | 项目/资料 | BUG-130 |
| `app/_components/creator-detail.tsx` | app/_components | 191 | 已完成 | 前端页面/组件 | REUSE-007 |
| `app/_components/creator-editor.tsx` | app/_components | 384 | 已完成 | 前端页面/组件 | BUG-131 |
| `app/_components/creator-identity.tsx` | app/_components | 35 | 已完成 | 前端页面/组件 | creator identity presentation |
| `app/_components/creator-picker.tsx` | app/_components | 252 | 已完成 | 前端页面/组件 | BUG-130 |
| `app/_components/custom-content-template-settings.tsx` | app/_components | 134 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/draft-autosave-status.tsx` | app/_components | 13 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/editor/content-language-switcher.tsx` | app/_components | 89 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/editor/content-translation-control.tsx` | app/_components | 179 | 已完成 | 前端页面/组件 | SEC-038,BUG-092-related |
| `app/_components/editor/editor-shell.tsx` | app/_components | 144 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/editor/localization-status-badge.tsx` | app/_components | 75 | 已完成 | 前端页面/组件 | BUG-134 |
| `app/_components/editor/loot-table-visual-editor.tsx` | app/_components | 564 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/editor/mod-resource-picker.tsx` | app/_components | 256 | 已完成 | 项目/资料 | BUG-143 |
| `app/_components/editor/recipe-editor.tsx` | app/_components | 580 | 已完成 | 前端页面/组件 | BUG-144 |
| `app/_components/editor/recipe-template-editor.tsx` | app/_components | 543 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/editor/resource-picker-dialog.tsx` | app/_components | 362 | 已完成 | 前端页面/组件 | PERF-068 |
| `app/_components/editor/review-status-panel.tsx` | app/_components | 61 | 已完成 | 前端页面/组件 | BUG-134 |
| `app/_components/editor/selected-resource-list.tsx` | app/_components | 70 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/favorite-modpack-export.tsx` | app/_components | 437 | 已完成 | 项目/资料 | PERF-039,PERF-040,PERF-041-related |
| `app/_components/favorite-picker-modal.tsx` | app/_components | 87 | 已完成 | 收藏夹选择、新建和目标成员关系保存对话框 | SEC-036；BUG-089；TEST-041 |
| `app/_components/file-drop-zone.tsx` | app/_components | 97 | 已完成 | 通用点击/拖放文件选择、accept过滤与禁用状态 | 无新增Finding；调用方仍须执行服务端类型和权限校验 |
| `app/_components/floating-tooltip.tsx` | app/_components | 63 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/global-catalog.tsx` | app/_components | 483 | 已完成 | 项目/资料 | BUG-010,BUG-134-related |
| `app/_components/global-recipe-card.tsx` | app/_components | 120 | 已完成 | 前端页面/组件 | BUG-010-related |
| `app/_components/home-page.tsx` | app/_components | 193 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/iconfont.tsx` | app/_components | 56 | 已完成 | 前端页面/组件 | SEC-047 |
| `app/_components/localized-asset-editor.tsx` | app/_components | 110 | 已完成 | 前端页面/组件 | BUG-136 |
| `app/_components/log-share-tool.tsx` | app/_components | 172 | 已完成 | 前端页面/组件 | BUG-142 |
| `app/_components/log-share-viewer.tsx` | app/_components | 40 | 已完成 | 前端页面/组件 | PERF-066,STYLE-003 |
| `app/_components/markdown-renderer.tsx` | app/_components | 1226 | 已完成 | 前端页面/组件 | Markdown security |
| `app/_components/messages-center.tsx` | app/_components | 485 | 已完成 | 通知分类/已读/AI翻译与私聊会话、消息、Presence、实时刷新界面 | BUG-047/048；PERF-031；跨会话迟到响应覆盖且AI余额无初始加载 |
| `app/_components/minecraft-language-picker.tsx` | app/_components | 249 | 已完成 | 前端页面/组件 | Minecraft language picker |
| `app/_components/minecraft-version-picker.tsx` | app/_components | 303 | 已完成 | 前端页面/组件 | BUG-129, STYLE-003 |
| `app/_components/mod-catalog-data.tsx` | app/_components | 471 | 已完成 | 项目/资料 | SEC-046-related |
| `app/_components/mod-catalog.tsx` | app/_components | 640 | 已完成 | 项目/资料 | BUG-135-related,PERF-051-existing |
| `app/_components/mod-content-layout-editor.tsx` | app/_components | 1325 | 已完成 | 项目/资料 | BUG-148;PERF-071 |
| `app/_components/mod-content-manager.tsx` | app/_components | 41 | 已完成 | 项目/资料 | no-new-finding |
| `app/_components/mod-content-resource-detail.tsx` | app/_components | 256 | 已完成 | 项目/资料 | BUG-145 |
| `app/_components/mod-content-resource-editor.tsx` | app/_components | 1376 | 已完成 | 项目/资料 | BUG-146 |
| `app/_components/mod-content-section-actions.tsx` | app/_components | 56 | 已完成 | 项目/资料 | no-new-finding |
| `app/_components/mod-content-section-page.tsx` | app/_components | 262 | 已完成 | 项目/资料 | BUG-145 |
| `app/_components/mod-content-workspace.tsx` | app/_components | 683 | 已完成 | 项目/资料 | no-new-finding |
| `app/_components/mod-detail-loader.tsx` | app/_components | 52 | 已完成 | 项目/资料 | BUG-133 |
| `app/_components/mod-detail.tsx` | app/_components | 183 | 已完成 | 项目/资料 | BUG-135 |
| `app/_components/mod-editor.tsx` | app/_components | 759 | 已完成 | 项目/资料 | BUG-146 |
| `app/_components/mod-history.tsx` | app/_components | 59 | 已完成 | Mod修订历史选择、两版本比较导航与字段差异展示 | BUG-022；PERF-013；项目审核队列链接到本页但后端只向编辑者/全局审核者返回待审项，且全量历史一次渲染；编辑入口未按权限隐藏但后端仍校验 |
| `app/_components/mod-resource-components.tsx` | app/_components | 829 | 已完成 | 项目/资料 | no-new-finding |
| `app/_components/mod-resource-indexes.tsx` | app/_components | 124 | 已完成 | 项目/资料 | no-new-finding |
| `app/_components/modid-confirmation-card.tsx` | app/_components | 60 | 已完成 | 项目/资料 | no-new-finding |
| `app/_components/modpack-detail.tsx` | app/_components | 115 | 已完成 | 项目/资料 | BUG-133,BUG-135 |
| `app/_components/modpack-editor.tsx` | app/_components | 235 | 已完成 | 项目/资料 | BUG-146 |
| `app/_components/page-feedback.tsx` | app/_components | 39 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/permission-comparison.tsx` | app/_components | 185 | 已完成 | 认证/权限 | no-new-finding |
| `app/_components/player-profile-detail.tsx` | app/_components | 91 | 已完成 | 文件/OSS/日志 | no-new-finding |
| `app/_components/project-auto-update-settings.tsx` | app/_components | 67 | 已完成 | 项目/资料 | STYLE-002 |
| `app/_components/project-changelog-editor.tsx` | app/_components | 154 | 已完成 | 多语言更新日志编辑、分类与草稿 | BUG-126 |
| `app/_components/project-changelog-entry.tsx` | app/_components | 30 | 已完成 | 单条更新日志详情与本地化正文 | 无新增问题 |
| `app/_components/project-changelog-history.tsx` | app/_components | 22 | 已完成 | 更新日志历史路由装配 | 无新增问题 |
| `app/_components/project-changelog.tsx` | app/_components | 89 | 已完成 | 项目更新日志分组、时间线与编辑入口 | 无新增问题 |
| `app/_components/project-downloads.tsx` | app/_components | 346 | 已完成 | 项目/资料 | PERF-023-existing |
| `app/_components/project-editor-application.tsx` | app/_components | 140 | 已完成 | 项目编辑员申请、证明与附件上传 | REUSE-007 |
| `app/_components/project-editor-fields.tsx` | app/_components | 29 | 已完成 | 项目/资料 | no-new-finding |
| `app/_components/project-follow-button.tsx` | app/_components | 49 | 已完成 | 项目关注状态与幂等切换 | 无新增问题 |
| `app/_components/project-follows-panel.tsx` | app/_components | 49 | 已完成 | 我的项目关注搜索、筛选与取消 | BUG-127 |
| `app/_components/project-submission-modal.tsx` | app/_components | 157 | 已完成 | 项目/资料 | no-new-finding |
| `app/_components/rating-panel.tsx` | app/_components | 256 | 已完成 | 评分摘要、维度、编辑与评论列表 | LEGACY-017 |
| `app/_components/recipe-edit-link.tsx` | app/_components | 28 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/recipe-resource-slot.tsx` | app/_components | 82 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/review-edit-lock.tsx` | app/_components | 88 | 已完成 | 待审变更锁与完成订阅 | 无新增问题 |
| `app/_components/rotating-resource.ts` | app/_components | 51 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/server-catalog.tsx` | app/_components | 386 | 已完成 | 项目/资料 | no-new-finding |
| `app/_components/server-detail.tsx` | app/_components | 327 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/server-submission-page.tsx` | app/_components | 40 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/server-submission-wizard.tsx` | app/_components | 559 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/simple-project-catalog.tsx` | app/_components | 482 | 已完成 | 项目/资料 | BUG-147 |
| `app/_components/simple-project-detail.tsx` | app/_components | 108 | 已完成 | 项目/资料 | BUG-133,BUG-135 |
| `app/_components/simple-project-editor.tsx` | app/_components | 227 | 已完成 | 项目/资料 | BUG-146 |
| `app/_components/site-affairs-pages.tsx` | app/_components | 164 | 已完成 | 前端页面/组件 | DEAD-001 |
| `app/_components/site-brand-provider.tsx` | app/_components | 68 | 已完成 | 公开品牌加载、Context和客户端文档标题同步 | BUG-049；MutationObserver持续覆盖页面级metadata标题 |
| `app/_components/site-login-panel.tsx` | app/_components | 277 | 已完成 | 密码、邮件、OAuth登录与注册表单 | 无新增问题 |
| `app/_components/site-shell.tsx` | app/_components | 611 | 已完成 | 全站Presence/SSE桥、后端状态、通知弹窗、导航认证菜单和未读徽标 | PERF-031；事件到全局DOM事件形成重复刷新；SSE客户端同ID有界去重 |
| `app/_components/skin-detail.tsx` | app/_components | 235 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/skin-library.tsx` | app/_components | 129 | 已完成 | 前端页面/组件 | PERF-048-related |
| `app/_components/skin-preview.tsx` | app/_components | 127 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/skin-upload.tsx` | app/_components | 159 | 已完成 | 前端页面/组件 | BUG-087-related |
| `app/_components/square-image-crop-dialog.tsx` | app/_components | 24 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/sticker-picker.tsx` | app/_components | 109 | 已完成 | 表情包目录、搜索与Markdown令牌插入 | 无新增问题 |
| `app/_components/theme-provider.tsx` | app/_components | 60 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/timezone-picker.tsx` | app/_components | 129 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/tools-index.tsx` | app/_components | 61 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/tools-plantuml.tsx` | app/_components | 132 | 已完成 | 前端页面/组件 | third-party-privacy-boundary-related |
| `app/_components/tools-playground.tsx` | app/_components | 1003 | 已完成 | 前端页面/组件 | 通用Markdown编辑/预览、媒体与草稿；BUG-150；BUG-151 |
| `app/_components/unified-recipe-card.tsx` | app/_components | 165 | 已完成 | 前端页面/组件 | BUG-010-related |
| `app/_components/unified-report-dialog.tsx` | app/_components | 204 | 已完成 | 统一举报对话框、原因加载、OSS证据拖放上传与提交 | BUG-113/119；TEST-048 |
| `app/_components/user-avatar.tsx` | app/_components | 223 | 已完成 | 用户头像、在线隐私三态和悬浮公开用户卡 | 无新增独立问题；纯文本/安全URL边界由API承担，卡片调用点身份稳定 |
| `app/_components/user-comment-watches-panel.tsx` | app/_components | 126 | 已完成 | 评论关注、未读与静音管理 | BUG-128 |
| `app/_components/user-drafts-panel.tsx` | app/_components | 139 | 已完成 | 前端页面/组件 | SEC-043,PERF-061,BUG-120 |
| `app/_components/user-economy-panel.tsx` | app/_components | 677 | 已完成 | 前端页面/组件 | SEC-039,BUG-096,BUG-097,PERF-056-related |
| `app/_components/user-home.tsx` | app/_components | 857 | 已完成 | 前端页面/组件 | PERF-051-related |
| `app/_components/user-network-list.tsx` | app/_components | 145 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_components/user-player-profiles-panel.tsx` | app/_components | 352 | 已完成 | 文件/OSS/日志 | PERF-047,PERF-049-related |
| `app/_components/user-profile-overview.tsx` | app/_components | 345 | 已完成 | 文件/OSS/日志 | PERF-051-related |
| `app/_components/user-profile.tsx` | app/_components | 181 | 已完成 | 文件/OSS/日志 | no-new-finding |
| `app/_components/user-statistics-panel.tsx` | app/_components | 145 | 已完成 | 前端页面/组件 | no-new-finding |
| `app/_lib/admin-content-attribute-api.ts` | app/_lib | 34 | 已完成 | 后台资料属性模板API契约 | 无新增问题 |
| `app/_lib/advancement-graph.ts` | app/_lib | 38 | 已完成 | 进度树无向连通分组算法 | 无新增问题 |
| `app/_lib/anti-abuse-api.ts` | app/_lib | 27 | 已完成 | 反滥用表单令牌与挑战响应边界 | 无新增问题 |
| `app/_lib/api-normalizers.ts` | app/_lib | 46 | 已完成 | 目录API通用unknown响应归一化 | BUG-125；REUSE-006 |
| `app/_lib/api.ts` | app/_lib | 146 | 已完成 | 前端页面/组件 | auth signal |
| `app/_lib/auth.ts` | app/_lib | 229 | 已完成 | 认证/权限 | auth state |
| `app/_lib/backend-status.ts` | app/_lib | 13 | 已完成 | 后端可用性进程内状态与浏览器事件 | 无新增问题 |
| `app/_lib/blueprint-api.ts` | app/_lib | 80 | 已完成 | 蓝图列表、详情、变体和材料API类型 | 无新增问题 |
| `app/_lib/catalog-editor-api.ts` | app/_lib | 225 | 已完成 | Tag与合成类型编辑API及本地化归一化 | BUG-125；REUSE-006 |
| `app/_lib/catalog-resource-api.ts` | app/_lib | 122 | 已完成 | 全局资源编辑与受权绑定列表API | BUG-125；REUSE-006 |
| `app/_lib/catalog-resource-identifiers.ts` | app/_lib | 34 | 已完成 | 外部模板资源种类兼容与注册表推导 | 外部格式边界必须保留 |
| `app/_lib/catalog-sort.ts` | app/_lib | 51 | 已完成 | 目录排序字段、方向及旧参数归一化 | LEGACY-005 |
| `app/_lib/catalog-state.ts` | app/_lib | 203 | 已完成 | 目录URL状态、偏好和筛选控制Hook | 无新增问题 |
| `app/_lib/comment-api.ts` | app/_lib | 219 | 已完成 | 评论、楼层、回复、表态、插眼与附件API | 无新增问题 |
| `app/_lib/community-api.ts` | app/_lib | 263 | 已完成 | 作者团队、经济、任务与活动领域类型/显示辅助 | 无新增问题 |
| `app/_lib/community-post-api.ts` | app/_lib | 156 | 已完成 | 教程问题新闻讨论目录与编辑API | 无新增问题 |
| `app/_lib/content-history-api.ts` | app/_lib | 19 | 已完成 | 内容修订历史API契约 | PERF-054 |
| `app/_lib/content-language-api.ts` | app/_lib | 22 | 已完成 | 用户内容语言偏好API | 无新增问题 |
| `app/_lib/content-language.ts` | app/_lib | 170 | 已完成 | 站内内容语言规范化、回退与本地化选择 | MAP-012 |
| `app/_lib/content-metrics-api.ts` | app/_lib | 72 | 已完成 | 内容统计读取与浏览记录写入API | SEC-022/023 |
| `app/_lib/draft-api.ts` | app/_lib | 79 | 已完成 | 用户草稿保存、完成、读取和恢复URL协议 | SEC-043；PERF-061；BUG-120 |
| `app/_lib/drawio.ts` | app/_lib | 25 | 已完成 | Draw.io跨窗口消息解析边界 | 无新增问题 |
| `app/_lib/editor-api.ts` | app/_lib | 219 | 已完成 | 统一内容、目录选择与翻译任务API归一化 | BUG-125；REUSE-006 |
| `app/_lib/editor-types.ts` | app/_lib | 184 | 已完成 | 内容编辑、本地化与目录资源公共类型 | 无新增问题 |
| `app/_lib/favorite-api.ts` | app/_lib | 199 | 已完成 | 收藏夹与Modrinth整合包导出API契约和下载辅助 | STYLE-006；LEGACY-018；无前端自动化测试 |
| `app/_lib/global-catalog-api.ts` | app/_lib | 151 | 已完成 | 全局Tag、合成类型和合成表公共API聚合 | PERF-006/008/010 |
| `app/_lib/i18n-provider.tsx` | app/_lib | 192 | 已完成 | 八语言UI字典、回退和本地覆盖Provider | MAP-012 |
| `app/_lib/log-share-api.ts` | app/_lib | 99 | 已完成 | 日志分享创建、历史、删除和下载API | 无新增问题 |
| `app/_lib/loot-table-model.ts` | app/_lib | 133 | 已完成 | 战利品表外部格式兼容与条目遍历 | 外部格式边界必须保留 |
| `app/_lib/markdown-config.ts` | app/_lib | 62 | 已完成 | Markdown功能开关与PlantUML配置归一化 | SEC-001 |
| `app/_lib/minecraft-languages.ts` | app/_lib | 205 | 已完成 | Minecraft语言元数据与代码规范化 | 无新增问题 |
| `app/_lib/minecraft-version-api.ts` | app/_lib | 28 | 已完成 | Minecraft版本配置请求去重与进程缓存 | 无新增问题 |
| `app/_lib/mod-api.ts` | app/_lib | 158 | 已完成 | Mod后端DTO与目录展示模型转换 | 无新增问题 |
| `app/_lib/mod-catalog-data.ts` | app/_lib | 179 | 已完成 | 大型项目目录展示模型与Mod筛选枚举 | 无新增问题 |
| `app/_lib/mod-content-api.ts` | app/_lib | 201 | 已完成 | Mod资料版本、板块、资源与布局API | PERF-064 |
| `app/_lib/mod-export-api.ts` | app/_lib | 349 | 已完成 | Mod导出包/嵌入目录上传、确认与任务轮询 | PERF-063；REUSE-005 |
| `app/_lib/mod-export-upload-store.ts` | app/_lib | 136 | 已完成 | Mod导入断点任务和文件IndexedDB持久化 | SEC-046 |
| `app/_lib/modpack-api.ts` | app/_lib | 97 | 已完成 | 整合包DTO、目录转换与图标URL | 无新增问题 |
| `app/_lib/navigation.ts` | app/_lib | 15 | 已完成 | 通知等站内目标URL的同源路径规范化 | 无新增问题；拒绝协议相对、反斜杠、过长、解码后危险路径 |
| `app/_lib/oss-upload.ts` | app/_lib | 430 | 已完成 | OSS预签名、哈希、单段/分片上传与完成协议 | PERF-063 |
| `app/_lib/project-changelog-api.ts` | app/_lib | 55 | 已完成 | 项目更新日志读取与编辑API | 无新增问题 |
| `app/_lib/project-download-api.ts` | app/_lib | 102 | 已完成 | 项目下载文件DTO与OSS上传 | PERF-063 |
| `app/_lib/project-follow-api.ts` | app/_lib | 36 | 已完成 | 项目关注状态、写入与本人列表API | 无新增问题 |
| `app/_lib/project-identifiers.ts` | app/_lib | 8 | 已完成 | 项目站点ID输入规范化 | 无新增问题 |
| `app/_lib/rating-api.ts` | app/_lib | 99 | 已完成 | 评分汇总、明细和增删API | PERF-050；LEGACY-017 |
| `app/_lib/recipe-editor-api.ts` | app/_lib | 345 | 已完成 | 合成类型/模板/合成表编辑API与unknown响应解析 | BUG-125；REUSE-006 |
| `app/_lib/recipe-editor-types.ts` | app/_lib | 137 | 已完成 | 合成模板、槽位、绑定与Mutation类型 | 无新增问题 |
| `app/_lib/resource-picker-loaders.ts` | app/_lib | 26 | 已完成 | Tag资源选择器分页适配 | 无新增问题 |
| `app/_lib/server-api.ts` | app/_lib | 194 | 已完成 | 服务器目录、探测、提交和历史API类型 | 无新增问题 |
| `app/_lib/similar-resource-groups.ts` | app/_lib | 20 | 已完成 | 相似资料稳定分组算法 | 无新增问题 |
| `app/_lib/simple-project-api.ts` | app/_lib | 178 | 已完成 | 六类简单项目DTO、配置、路径和本地化 | 无新增问题 |
| `app/_lib/site-affairs-api.ts` | app/_lib | 56 | 已完成 | 关于页、站点更新日志和小黑屋公开API类型与请求封装 | STYLE-007 |
| `app/_lib/site-notice.ts` | app/_lib | 8 | 已完成 | 全站通知浏览器事件与后端故障抑制 | 无新增问题 |
| `app/_lib/skin-api.ts` | app/_lib | 249 | 已完成 | 皮肤、衣柜、玩家档案和启动器凭据API | 无新增问题 |
| `app/_lib/sticker-api.ts` | app/_lib | 44 | 已完成 | 前端表情目录类型、请求缓存与Token构造 | BUG-101；DEAD-012 |
| `app/_lib/timezones.ts` | app/_lib | 87 | 已完成 | 时区发现、偏移、名称和搜索格式化 | 无新增问题 |
| `app/_lib/use-auto-draft.ts` | app/_lib | 148 | 已完成 | 九类编辑器共享自动草稿恢复与定时保存Hook | BUG-126 |
| `app/_lib/user-api.ts` | app/_lib | 74 | 已完成 | 用户公开资料、卡片与短期客户端缓存 | 无新增问题 |
| `app/_locales/de.ts` | app/_locales | 47 | 已完成 | 前端页面/组件 | 德语局部字典与英文回退；无新增问题 |
| `app/_locales/en-US.ts` | app/_locales | 4236 | 已完成 | 前端页面/组件 | 英文权威界面字典；4641个叶子键与中文占位符对齐；DEAD-016；LEGACY-022；STYLE-009 |
| `app/_locales/es.ts` | app/_locales | 47 | 已完成 | 前端页面/组件 | 西班牙语局部字典与英文回退；无新增问题 |
| `app/_locales/fr.ts` | app/_locales | 47 | 已完成 | 前端页面/组件 | 法语局部字典与英文回退；无新增问题 |
| `app/_locales/ja.ts` | app/_locales | 47 | 已完成 | 前端页面/组件 | 日语局部字典与英文回退；无新增问题 |
| `app/_locales/ru.ts` | app/_locales | 47 | 已完成 | 前端页面/组件 | 俄语局部字典与英文回退；无新增问题 |
| `app/_locales/zh-CN.ts` | app/_locales | 4235 | 已完成 | 前端页面/组件 | 简体中文权威界面字典；4641个叶子键与英文占位符对齐；DEAD-016；LEGACY-022；STYLE-009 |
| `app/_locales/zh-TW.ts` | app/_locales | 53 | 已完成 | 前端页面/组件 | 繁体中文局部字典与简体中文回退；无新增问题 |
| `app/addons/[siteId]/edit/page.tsx` | app/addons | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/addons/[siteId]/history/page.tsx` | app/addons | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/addons/[siteId]/page.tsx` | app/addons | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/addons/new/page.tsx` | app/addons | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/addons/page.tsx` | app/addons | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/admin/global-resources/page.tsx` | app/admin | 8 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/admin/page.tsx` | app/admin | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/api/site-logo/route.ts` | app/api | 55 | 已完成 | 管理员Logo权限转发、魔数识别和Next本地public写入 | SEC-019；OPS-008；绕过OSS/图片解码安全且多实例不可共享 |
| `app/authors/[publicId]/edit/page.tsx` | app/authors | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/authors/[publicId]/page.tsx` | app/authors | 10 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/authors/new/page.tsx` | app/authors | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/authors/page.tsx` | app/authors | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/blueprints/[publicId]/edit/page.tsx` | app/blueprints | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/blueprints/[publicId]/page.tsx` | app/blueprints | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/blueprints/page.tsx` | app/blueprints | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/blueprints/upload/page.tsx` | app/blueprints | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/catalog/resources/page.tsx` | app/catalog | 13 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | LEGACY-021 |
| `app/changelogs/[id]/edit/page.tsx` | app/changelogs | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/changelogs/[id]/history/page.tsx` | app/changelogs | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/changelogs/[id]/page.tsx` | app/changelogs | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/changelogs/new/page.tsx` | app/changelogs | 7 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/comments/[commentId]/page.tsx` | app/comments | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/datapacks/[siteId]/edit/page.tsx` | app/datapacks | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/datapacks/[siteId]/history/page.tsx` | app/datapacks | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/datapacks/[siteId]/page.tsx` | app/datapacks | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/datapacks/new/page.tsx` | app/datapacks | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/datapacks/page.tsx` | app/datapacks | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/discussions/[id]/edit/page.tsx` | app/discussions | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/discussions/[id]/history/page.tsx` | app/discussions | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/discussions/[id]/page.tsx` | app/discussions | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/discussions/new/page.tsx` | app/discussions | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/discussions/page.tsx` | app/discussions | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/favicon.ico` | app/favicon.ico | 31 | 排除 | 二进制/媒体资源 | 非可执行第一方源码 |
| `app/globals.css` | app/globals.css | 446 | 已完成 | 前端页面/组件 | 全局设计令牌、表单、Markdown/代码/嵌入/表情样式与深色主题；无新增问题 |
| `app/issues/[id]/edit/page.tsx` | app/issues | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/issues/[id]/history/page.tsx` | app/issues | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/issues/[id]/page.tsx` | app/issues | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/issues/new/page.tsx` | app/issues | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/issues/page.tsx` | app/issues | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/layout.tsx` | app/layout.tsx | 28 | 已完成 | 全站HTML、主题、i18n、品牌和Shell根布局 | BUG-123 |
| `app/log/s/[code]/page.tsx` | app/log | 9 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/login/page.tsx` | app/login | 10 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/maps/[siteId]/edit/page.tsx` | app/maps | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/maps/[siteId]/history/page.tsx` | app/maps | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/maps/[siteId]/page.tsx` | app/maps | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/maps/new/page.tsx` | app/maps | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/maps/page.tsx` | app/maps | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/messages/page.tsx` | app/messages | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/modpacks/[siteId]/edit/page.tsx` | app/modpacks | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/modpacks/[siteId]/history/page.tsx` | app/modpacks | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/modpacks/[siteId]/page.tsx` | app/modpacks | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/modpacks/new/page.tsx` | app/modpacks | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/modpacks/page.tsx` | app/modpacks | 7 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/mods-tag/page.tsx` | app/mods-tag | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/mods/[siteId]/compare/page.tsx` | app/mods | 7 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/mods/[siteId]/data/edit/page.tsx` | app/mods | 8 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/mods/[siteId]/data/sections/[sectionId]/arrange/page.tsx` | app/mods | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/mods/[siteId]/data/sections/[sectionId]/page.tsx` | app/mods | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/mods/[siteId]/edit/page.tsx` | app/mods | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/mods/[siteId]/history/page.tsx` | app/mods | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/mods/[siteId]/page.tsx` | app/mods | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/mods/[siteId]/resources/[resourceId]/edit/page.tsx` | app/mods | 18 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/mods/[siteId]/resources/[resourceId]/history/page.tsx` | app/mods | 15 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/mods/[siteId]/resources/[resourceId]/page.tsx` | app/mods | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/mods/[siteId]/resources/new/page.tsx` | app/mods | 17 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/mods/new/page.tsx` | app/mods | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/mods/page.tsx` | app/mods | 11 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/news/[id]/edit/page.tsx` | app/news | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/news/[id]/history/page.tsx` | app/news | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/news/[id]/page.tsx` | app/news | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/news/new/page.tsx` | app/news | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/news/page.tsx` | app/news | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/page.tsx` | app/page.tsx | 5 | 已完成 | 主页到HomePage的服务端路由入口 | 无新增问题 |
| `app/permissions/compare/page.tsx` | app/permissions | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/players/[publicId]/page.tsx` | app/players | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/plugins/[siteId]/edit/page.tsx` | app/plugins | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/plugins/[siteId]/history/page.tsx` | app/plugins | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/plugins/[siteId]/page.tsx` | app/plugins | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/plugins/new/page.tsx` | app/plugins | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/plugins/page.tsx` | app/plugins | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/recipe-types/page.tsx` | app/recipe-types | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/resource-packs/[siteId]/edit/page.tsx` | app/resource-packs | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/resource-packs/[siteId]/history/page.tsx` | app/resource-packs | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/resource-packs/[siteId]/page.tsx` | app/resource-packs | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/resource-packs/new/page.tsx` | app/resource-packs | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/resource-packs/page.tsx` | app/resource-packs | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/reviews/page.tsx` | app/reviews | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/robots.ts` | app/robots.ts | 26 | 已完成 | 搜索引擎允许、隐私路由与参数抓取规则 | OPS-022 |
| `app/servers/[serverId]/page.tsx` | app/servers | 11 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/servers/new/page.tsx` | app/servers | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/servers/page.tsx` | app/servers | 10 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/shaders/[siteId]/edit/page.tsx` | app/shaders | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/shaders/[siteId]/history/page.tsx` | app/shaders | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/shaders/[siteId]/page.tsx` | app/shaders | 3 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/shaders/new/page.tsx` | app/shaders | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/shaders/page.tsx` | app/shaders | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/site-affairs/about/page.tsx` | app/site-affairs | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/site-affairs/blackroom/[id]/page.tsx` | app/site-affairs | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/site-affairs/blackroom/page.tsx` | app/site-affairs | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/site-affairs/changelogs/[id]/page.tsx` | app/site-affairs | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/site-affairs/changelogs/page.tsx` | app/site-affairs | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/site-affairs/page.tsx` | app/site-affairs | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/site-assets/[fileName]/route.ts` | app/site-assets | 32 | 已完成 | 哈希Logo本地文件读取和不可变缓存响应 | OPS-008；路径正则防穿越但依赖当前实例本地文件 |
| `app/sitemap.ts` | app/sitemap.ts | 17 | 已完成 | 公共顶级静态路由站点地图 | OPS-022 |
| `app/skins/[publicId]/edit/page.tsx` | app/skins | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/skins/[publicId]/page.tsx` | app/skins | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/skins/page.tsx` | app/skins | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/skins/upload/page.tsx` | app/skins | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/teams/[publicId]/edit/page.tsx` | app/teams | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/teams/[publicId]/page.tsx` | app/teams | 10 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/teams/new/page.tsx` | app/teams | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/tools/logs/page.tsx` | app/tools | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/tools/page.tsx` | app/tools | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/tools/plantuml/page.tsx` | app/tools | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/tools/playground/page.tsx` | app/tools | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/tutorials/[id]/edit/page.tsx` | app/tutorials | 5 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/tutorials/[id]/history/page.tsx` | app/tutorials | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/tutorials/[id]/page.tsx` | app/tutorials | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/tutorials/new/page.tsx` | app/tutorials | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/tutorials/page.tsx` | app/tutorials | 2 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/user/[publicId]/blocked/page.tsx` | app/user | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/user/[publicId]/followers/page.tsx` | app/user | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/user/[publicId]/following/page.tsx` | app/user | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/user/[publicId]/page.tsx` | app/user | 7 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `app/user/page.tsx` | app/user | 6 | 已完成 | Next App Router页面入口，向权威页面组件传递路由参数 | 无新增问题 |
| `components/mcmods-exporter/BlockModelCanvas.tsx` | components/mcmods-exporter | 118 | 已完成 | 项目/资料 | no-new-finding |
| `components/mcmods-exporter/StructureCanvas.tsx` | components/mcmods-exporter | 161 | 已完成 | 项目/资料 | PERF-070 |
| `components/minecraft-skin/SkinViewerCanvas.tsx` | components/minecraft-skin | 290 | 已完成 | 基础设施/通用 | no-new-finding |
| `eslint.config.mjs` | eslint.config.mjs | 18 | 已完成 | Next Core Web Vitals和TypeScript lint配置 | 无新增问题 |
| `lib/mcmods-exporter/renderer/README.md` | lib/mcmods-exporter | 32 | 排除 | 文档 | 作为实现证据交叉阅读，不计第一方功能源码 |
| `lib/mcmods-exporter/renderer/StructureRenderer.ts` | lib/mcmods-exporter | 451 | 已完成 | 项目/资料 | PERF-069,PERF-070 |
| `lib/mcmods-exporter/renderer/blueprint.ts` | lib/mcmods-exporter | 378 | 已完成 | 项目/资料 | PERF-069 |
| `lib/mcmods-exporter/renderer/httpAssetSource.ts` | lib/mcmods-exporter | 87 | 已完成 | 项目/资料 | PERF-070 |
| `lib/mcmods-exporter/renderer/index.ts` | lib/mcmods-exporter | 6 | 已完成 | 项目/资料 | no-new-finding |
| `lib/mcmods-exporter/renderer/minecraftBlockModel.ts` | lib/mcmods-exporter | 832 | 已完成 | 项目/资料 | PERF-070-related |
| `lib/mcmods-exporter/renderer/structureScene.ts` | lib/mcmods-exporter | 256 | 已完成 | 项目/资料 | PERF-069,PERF-070 |
| `lib/mcmods-exporter/renderer/types.ts` | lib/mcmods-exporter | 20 | 已完成 | 项目/资料 | PERF-070 |
| `next.config.ts` | next.config.ts | 41 | 已完成 | 安全响应头、Yggdrasil发现头和图片域配置 | OPS-022 |
| `package-lock.json` | package-lock.json | 8807 | 排除 | 依赖锁文件 | 依赖一致性检查覆盖 |
| `package.json` | package.json | 52 | 已完成 | 前端脚本、运行依赖和安全覆盖版本 | TEST-001 |
| `postcss.config.mjs` | postcss.config.mjs | 7 | 已完成 | Tailwind PostCSS插件配置 | 无新增问题 |
| `proxy.ts` | proxy.ts | 68 | 已完成 | 全站nonce CSP生成及请求响应传播 | SEC-045 |
| `public/mc-icons/icon-armor-empty.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-armor-full.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-armor-half.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-exp.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-food-buff-hunger.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-food-buff-saturation.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-food-empty-hunger-level.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-food-empty-saturation-level-100.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-food-empty-saturation-level-25.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-food-empty-saturation-level-50.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-food-empty-saturation-level-75.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-food-full-hunger-level.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-food-half-hunger-level.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-empty.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-full-buff-poison-ex.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-full-buff-poison.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-full-buff-regeneration-ex.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-full-buff-regeneration.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-full-buff-wither-ex.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-full-buff-wither.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-full-ex.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-full-jockey.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-full.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-half-buff-poison-ex.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-half-buff-poison.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-half-buff-regeneration-ex.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-half-buff-regeneration.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-half-buff-wither-ex.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-half-buff-wither.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-half-ex.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-half-jockey.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-health-half.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-oxygen-empty.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-oxygen-full.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-toughness-diamond-empty.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-toughness-diamond-full.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-toughness-diamond-half.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-toughness-empty.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-toughness-full.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `public/mc-icons/icon-toughness-half.svg` | public/mc-icons | 3 | 排除 | 矢量静态资源 | 构建和引用覆盖；未逐路径人工审查 |
| `tsconfig.json` | tsconfig.json | 34 | 已完成 | 严格TypeScript、Bundler解析和Next类型配置 | 无新增问题 |
| `types/plantuml-encoder.d.ts` | types/plantuml-encoder.d.ts | 7 | 已完成 | plantuml-encoder最小模块声明 | 无新增问题 |

## 统计

| 指标 | 数量 |
| --- | ---: |
| 第一方文件 | 791 |
| 已完成人工审查 | 791 |
| 排除文件 | 93 |
| 无法确认 | 0 |
| 第一方代码/配置行数 | 169449 |
| 已完成人工审查行数 | 169449 |
| 按行人工覆盖率 | 100.00% |

清单中的全部第一方代码/配置文件均已完成逐文件人工语义审查；未开始、审查中和无法确认均为0。排除项按清单理由不计入第一方代码/配置基数。
