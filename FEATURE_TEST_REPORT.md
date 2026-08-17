# 治理、站务、爬虫与自动更新测试报告

## 环境

- 日期：2026-08-17；
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

## 覆盖

单元测试覆盖所有举报目标理由、非法理由隔离、理由接口、路由类型适配、举报反滥用动作、公开封禁状态、爬虫类型规范化、严格下载门槛、随机分页范围/查询参数、自动更新周期与来源哈希、Schema generation 和关键约束。已有 Outbox、权限、反滥用、缓存和评论测试随全包回归执行。

数据库集成测试在重置后的真实开发库上验证 generation 80、当前表/索引/触发器与 RBAC 种子。并发可靠性主要由唯一约束、事务行锁、advisory lock、`SKIP LOCKED` 与已有 Outbox 集成测试覆盖。

## 未能真实验证

- 当前 NATS/JetStream 未连接，因此只验证数据库事实来源与降级日志，未做真实消息重投/死信演练；
- Redis 未启用，不涉及本轮治理事实数据，但未做多实例缓存故障测试；
- 未使用真实 OSS 上传恶意/ZIP 证据，也未验证外部杀毒服务；
- 未消耗真实 AI 额度执行 8 语言翻译；
- 未调用带生产凭据的 CurseForge、GitHub 或大规模 Modrinth 抓取；这些路径使用本地 HTTP/单元测试验证请求、映射和随机分页；
- 未在生产级大数据量上做长时间压力测试；
- 未进行真实浏览器人工逐像素回归。

这些限制不影响 PostgreSQL 事实数据和空库启动，但上线前必须在隔离预发布环境补做外部依赖、并发、OSS 扫描和任务恢复验证。
