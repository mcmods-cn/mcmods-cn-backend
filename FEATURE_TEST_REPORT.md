# 治理、站务、爬虫与自动更新测试报告

## 环境

- 最后更新：2026-08-18；
- Windows / PowerShell；Go SDK `D:\System\SDK\go\go1.26.4`；
- Next.js 16.2.11，项目自带 TypeScript 与 ESLint；
- 明确授权的开发 PostgreSQL 数据库 `mcmods`；
- `/ready` 实测：PostgreSQL ready，Redis disabled，NATS degraded，搜索 disabled。

## 已执行命令与结果

```text
go test -count=1 ./internal/httpapi ./internal/database
go test -count=1 ./...
MCMODS_RUN_DB_INTEGRATION=1 go test -count=1 ./internal/database
node node_modules/typescript/bin/tsc --noEmit
node node_modules/eslint/bin/eslint.js .
node node_modules/next/dist/bin/next build
APP_ENV=development DB_RESET_ON_START=true DB_RESET_CONFIRM="RESET mcmods" go run ./cmd/db-reset
go run .
```

截至本报告更新，以上测试均实际通过：后端全部包通过；启用数据库集成开关的 database 包通过；前端类型检查和 Lint 无输出错误；生产构建完成 59 个静态页面；空库重置、后端初始化、管理员 Session 登录和受权限保护的爬虫配置读取通过。

公开接口实测：`/live` 为 alive；`/ready` HTTP 200；关于本站 zh-CN HTTP 200；服务器举报理由返回 12 项且包含 `commercial_as_public` 与 `other`；空库小黑屋为 0 项。启动后未再出现旧的 `mismatched param and argument count` 封禁过期清理错误。

管理员 Session 登录实测通过；带管理员 Session 的常用封禁理由接口返回 12 项（含 `spam_bot` 和 `other`），未登录访问同一接口返回 HTTP 401。

## `autobot` 与项目维护状态补充验证

本轮实际执行：

```text
go test -count=1 ./...
APP_ENV=development DB_RESET_ON_START=true DB_RESET_CONFIRM="RESET mcmods" go run ./cmd/db-reset
MCMODS_RUN_DB_INTEGRATION=1 go test -count=1 ./internal/database ./internal/httpapi
```

结果：全部 Go 包单元回归通过；开发库重置并安装 generation 82；最终 database 集成包通过（8.661s），httpapi 集成包通过（66.306s）。真实 PostgreSQL 验证了 `autobot@mcmods.cn`、不可交互密码、`project.no-review/content.no-review`、全局及 `review.submit` 的 1000% 限流权限；验证了自动状态从 active 进入 lowFrequency、随后进入 discontinued、发现新上游活动后恢复 active，以及人工覆盖后保持人工状态。测试未调用真实生产提供方，因此半年/一年判断使用可控 UTC 时间和真实数据库事务，外部 API 时间戳解析仍由现有本地 HTTP 提供方测试覆盖。

## 覆盖

单元测试覆盖所有举报目标理由、非法理由隔离、理由接口、路由类型适配、举报反滥用动作、公开封禁状态、爬虫类型规范化、严格下载门槛、随机分页范围/查询参数、自动更新周期与来源哈希、Schema generation 和关键约束。已有 Outbox、权限、反滥用、缓存和评论测试随全包回归执行。

数据库集成测试在重置后的真实开发库上验证 generation 82、当前表/索引/触发器、`autobot` 服务账号及其免审/1000% 限流权限种子。并发可靠性主要由唯一约束、事务行锁、advisory lock、`SKIP LOCKED` 与已有 Outbox 集成测试覆盖。

## 2026-08-18：通知、收藏夹导出、MODID、表情和项目关注

### 实际命令

```text
go test ./internal/httpapi ./internal/database ./internal/app ./internal/queue
go test ./...
MCMODS_REAL_MODRINTH_TEST=1 go test ./internal/httpapi -run TestBuildMRPackWithRealModrinthMetadata -v -count=1
DB_RESET_ON_START=true DB_RESET_CONFIRM="RESET mcmods" go run ./cmd/db-reset
MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run 'Test(CurrentUserFeaturesIntegration|EveryForeignKeyHasLeadingIndex)$' -v -count=1
MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'Test(AdminDashboardContractIntegration|StickerMarkdownUsageQueryIntegration|FavoriteExportExhaustedLeaseRecoveryIntegration|ProjectUpdateEventsMergeWithinDeliveryWindowIntegration)$' -v -count=1
MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run TestSystemNotificationTranslationIsRejectedIntegration -v -count=1
go test -race ./internal/httpapi ./internal/queue
node node_modules/eslint/bin/eslint.js .
node node_modules/typescript/bin/tsc --noEmit
node node_modules/next/dist/bin/next build
MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database ./internal/httpapi
git diff --check
```

### 结果

- 后端 `go test ./...` 全部包通过；新增通知模板、MODID、`.mrpack`、加载器键、表情校验和项目更新 section 单元测试通过。
- 开发数据库通过受保护重置命令从空库初始化为 generation 82；`TestCurrentUserFeaturesIntegration` 和全外键前导索引检查通过，新表、种子、约束和索引存在。
- 前端 ESLint、TypeScript 和 Next.js 16.2.11 生产构建通过；构建生成 59 个静态页面且所有动态路由完成收集。
- 真实 Modrinth 验证使用官方 API 项目 `P7dR8mSH` 的 Minecraft 1.21.1/Fabric 文件元数据：实际加入 1 个 Mod 文件、0 个自动依赖、0 个遗漏，使用真实 SHA-1、SHA-512、文件大小和官方 CDN URL生成 `.mrpack`，随后作为 ZIP 解压并确认根目录只有 `modrinth.index.json`。这是外部元数据/包结构集成测试，不是完整收藏夹 UI 端到端测试。
- `.mrpack` 单元测试还验证 Fabric=`fabric-loader`、Forge=`forge`、NeoForge=`neoforge`，并拒绝路径穿越、缺 SHA-512、非官方 CDN 和零字节文件。
- PostgreSQL 集成测试验证导出 Worker 过期租约达到上限后稳定失败；项目连续更新在 30 秒窗口内只生成 1 个事件、1 个任务和 1 条 Outbox，且详情与下载区段均保留。
- PostgreSQL 接口测试直接调用系统通知翻译端点，确认返回 `403 SYSTEM_NOTIFICATION_TRANSLATION_DISABLED`，不是只依赖前端隐藏按钮。
- 最终检查发现收藏夹条目原本没有独立主键，而导出快照错误地使用查询 `row_number()` 充当来源条目 ID；当前开发期权威 Schema 已改为真实 `bigserial id`，并以 `UNIQUE(collection_id,entity_type,entity_id)` 保持业务幂等，导出快照改存真实条目 ID。
- 修正后再次执行受保护的开发库重置并通过：日志为 `development database mcmods reset to the current schema and seed baseline`。最终完整 PostgreSQL 回归通过：`internal/database` 7.474 秒，`internal/httpapi` 43.524 秒；最终 `go test ./...` 全包通过。
- 通用 Markdown 表情 AST 跳过链接/代码子树；选择器同时接入评论、回复和 `ToolsPlayground`。后台替换图片会可靠清理旧 OSS 对象。
- 最终前端直接使用项目配置的 Node 运行时执行 ESLint、TypeScript 与 Next.js 构建，三项均通过。一次直接调用 `pnpm lint` 因当前 Shell 的 PATH 中没有 Node 而在启动 ESLint 前失败；改用同一捆绑运行时的 `node.exe` 后通过，这不是代码测试失败。
- `git diff --check` 前后端均通过；输出只有 Git 对 Windows 工作树未来 LF/CRLF 转换的提示，没有空白错误。
- Go race 命令已实际执行但未运行测试：当前 Windows Go 环境为 `CGO_ENABLED=0`，工具链返回 `-race requires cgo`。因此本轮并发正确性由真实 PostgreSQL advisory lock/唯一约束集成测试验证，不能把 race detector 写成通过。

### 未真实验证

- 当前环境未提供可写的真实 OSS 桶，未执行导出 Worker 上传、签名下载和到期对象删除的真实云端端到端；代码使用既有 OSS 文件表和删除 Outbox，并已通过编译/单元回归。
- 未提供正在运行的 JetStream 集群，因此未做真实断线、重投、死信和多 Worker 竞态演练；PostgreSQL 任务/Outbox、幂等唯一索引及定期扫描降级已实现。
- 未使用浏览器自动化框架，表情选择器、关注按钮、柔和红色及导出自动下载只完成类型、Lint、生产构建和代码级验证，仍需人工浏览器验收。
- 未用数万关注者做压力测试，批量大小与 SQL 索引已设置，但生产容量仍需预发布压测。

## 未能真实验证

- 当前 NATS/JetStream 未连接，因此只验证数据库事实来源与降级日志，未做真实消息重投/死信演练；
- Redis 未启用，不涉及本轮治理事实数据，但未做多实例缓存故障测试；
- 未使用真实 OSS 上传恶意/ZIP 证据，也未验证外部杀毒服务；
- 未消耗真实 AI 额度执行 8 语言翻译；
- 未调用带生产凭据的 CurseForge、GitHub 或大规模 Modrinth 抓取；这些路径使用本地 HTTP/单元测试验证请求、映射和随机分页；
- 未在生产级大数据量上做长时间压力测试；
- 未进行真实浏览器人工逐像素回归。

这些限制不影响 PostgreSQL 事实数据和空库启动，但上线前必须在隔离预发布环境补做外部依赖、并发、OSS 扫描和任务恢复验证。
