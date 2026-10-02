# 审计覆盖范围与方法

## 仓库与版本

| 仓库 | 路径 | 当前分支 | 审计时状态 |
| --- | --- | --- | --- |
| 前端 | `D:\Project\WebStorm\mcmods-cn-frontend` | `codex/unified-catalogs-user-systems` | 存在用户未提交修改，已保留 |
| 后端 | `D:\Project\GoLand\mcmods-cn-backend` | `codex/unified-catalogs-user-systems` | 存在用户未提交修改，已保留 |

- 前端审计提交：`4f82c9075636cc4f1f6d3eb07de37a3fffd1de58`。
- 后端审计提交：`bd50afd0ef92192c40c5152ea56e27a3fdbb657f`。
- Go SDK launcher：1.26.4；`go.mod` toolchain：1.26.5。
- Node.js：24.19.0；pnpm：11.19.0。
- 远程测试 PostgreSQL：18.0。
- 本机 NATS：2.14.3，Core 连接成功，JetStream 未启用。
- Redis：本机 6379 无响应，`.env` 未配置 Redis；相关行为只完成静态/本地 fallback 测试。
- 配置环境：开发/测试；`.env` 仅读取键名和通过应用配置加载连接，报告未写入主机、用户或凭据。

审计没有回滚、覆盖或格式化用户现有修改。本任务只新增审计文档，没有重构业务代码。

## 覆盖层级

| 层级 | 方法 | 覆盖结论 |
| --- | --- | --- |
| 全部跟踪文件 | `rg --files`、扩展名/行数统计、Git 状态 | 前端 393、后端 491 个文件全部进入清单 |
| 可编译代码 | Go 全量测试/构建/vet；TS 类型检查/Lint/Next 构建 | 全部编译入口被工具链读取 |
| 路由与页面 | 解析后端路由注册、Next 生产构建路由表 | 489 个 API 路由、119 个页面 |
| 数据库 | Schema/触发器/索引人工审查；远程数据库集成复跑 | 从原后端 `.env` 安全加载远程测试库配置，显式关闭数据库重置后，全量 PostgreSQL 集成复跑通过 |
| 高风险调用链 | 人工跟踪认证、权限、通知、文件、评论、日志、队列、自动化 | 已完成语义审查 |
| 旧实现 | 关键词、真实调用方、Git 历史、替代实现对照 | 只记录有证据的残留 |
| 视觉/静态资源 | 构建与引用扫描 | 未逐像素或逐行人工审查 SVG/图片 |

逐文件路径、行数、模块、严格审查状态和问题 ID 见 [modules/FILE_COVERAGE_INVENTORY.md](modules/FILE_COVERAGE_INVENTORY.md)。该清单没有把工具扫描自动算作人工完成。

## 严格人工覆盖统计

| 指标 | 数量 |
| --- | ---: |
| 两仓库跟踪文件 | 884 |
| 第一方代码/配置文件 | 791 |
| 已完成人工调用链审查文件 | 791 |
| 排除文件 | 93 |
| 第一方代码/配置行数 | 169,449 |
| 已完成人工审查行数 | 169,449 |
| 严格按行覆盖率 | 100.00%（169,449 / 169,449） |

因此当前791个第一方代码/配置文件均已完成逐文件逐行人工语义审查；未开始、审查中和无法确认均为0。93个依赖锁文件、静态资源和生成物按清单理由排除。

## 前端模块清单

- `app/_components`：管理后台、项目/资料编辑器、Markdown、评论、文件拖放、版本选择器、通知和用户组件。
- `app/_lib`：统一 API、认证状态、API normalizer、编辑器 API、类型和工具函数。
- `app/_locales`：站内本地化注册和文案。
- 页面域：Mod、整合包、插件、地图、光影、材质包、数据包、附属、服务器、教程、新闻、讨论、BUG、蓝图、皮肤、作者/团队、后台、日志工具等。
- `public`：站点图标、manifest 与静态资源。

人工重点阅读了最大组件和公共边界。`admin-console.tsx` 约 5,749 行，是当前最明显的前端维护热点。

## 后端模块清单

- `cmd`：API、Worker、迁移/初始化和运维工具入口。
- `internal/httpapi`：路由、中间件、Handler、权限规则、缓存及 HTTP 集成测试。
- `internal/database`：权威 Schema、各业务 Schema 段、数据库测试。
- `internal/queue`：NATS、Outbox、消费者、重试和死信。
- `internal/antiabuse`、`security`：限流、来源校验、密码和安全策略。
- `internal/querycache`：Redis/进程缓存与失效版本。
- `internal/runtimelog`、`activity`、`userstats`：日志、用户操作和统计。
- `internal/serverprobe`、`mailer`、`progression` 等外部或独立领域服务。

## 实际命令

```text
go test ./...
go vet ./...
go build ./...
MCMODS_RUN_DB_INTEGRATION=1 go test -count=1 ./internal/database ./internal/httpapi
go test -count=1 -coverprofile coverage.out ./...
go mod tidy -diff
pnpm typecheck
pnpm lint
next build --webpack
pnpm exec tsc --noEmit --noUnusedLocals --noUnusedParameters --pretty false
```

还执行了带 NATS/JetStream 环境变量的队列集成测试，以及 `go test -race`、依赖漏洞扫描的可用性探测。失败/未执行项均在报告中如实记录。

| 命令 | 目录 | 退出码 | 结论 |
| --- | --- | ---: | --- |
| `go test ./...` | 后端 | 0 | 通过 |
| `go vet ./...` | 后端 | 0 | 通过 |
| `go build ./...` | 后端 | 0 | 通过 |
| `MCMODS_RUN_DB_INTEGRATION=1 go test -count=1 ./...` | 后端 | 0 | 通过；远程测试库连接、迁移校验及数据库/HTTP API/活动/反滥用/搜索集成测试均成功 |
| `go test -count=1 -coverprofile coverage.out ./...` | 后端 | 0 | 通过；18.4% |
| `go mod tidy -diff` | 后端 | 0 | 无差异 |
| JetStream/Outbox 指定测试 | 后端 | 非 0 | NATS 可连接但 JetStream 无 responder |
| `go test -race ...` | 后端 | 非 0 | 缺少 C 编译器；测试未运行 |
| `pnpm typecheck` | 前端 | 0 | 通过 |
| `pnpm lint` | 前端 | 0 | 通过 |
| `next build --webpack` | 前端 | 0 | 通过；59/59静态页面生成成功 |
| 严格 unused TypeScript 检查 | 前端 | 非 0 | 发现 1 个未使用参数 |

最终复跑中默认构建、测试和远程 PostgreSQL 集成没有源码失败。JetStream 未启用；race 缺少 C 编译器；`govulncheck` 未安装，前端仓库使用 `package-lock.json` 但当前环境没有 npm CLI，pnpm 也拒绝在无 pnpm lockfile 时执行审计。这些未完成项均按实际环境限制记录，未写成通过。

## 证据分级

- **确认事实**：由代码、数据库约束、实际测试输出或 Git 历史直接证明。
- **风险推断**：实现存在明确失效窗口，但本地环境不足以复现生产故障。
- **尚未验证**：需要 JetStream、Redis 多实例、真实 OSS/CDN、C 编译器或依赖审计工具。

## 限制

791 个纳入第一方基数的代码/配置文件均已完整打开并完成人工语义审查；工具只用于辅助统计、调用定位和校验，不作为完成状态来源。93 个排除项是文档、锁文件、二进制/静态美术资源或矢量路径资源，均在清单中单独列明原因。没有生产流量、真实数据规模、生产 Redis/JetStream 集群和 CDN 配置，因此 100% 代码审查不能替代生产容量或外部客户端兼容性验证。
