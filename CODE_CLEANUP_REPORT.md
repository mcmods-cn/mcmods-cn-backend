# MCMods 代码整理报告

日期：2026-08-16

## 范围与基线

本轮检查覆盖后端认证、权限、反滥用、缓存、活动事件、Outbox/NATS、列表排序与筛选、健康检查、未读计数，以及前端站点外壳、消息中心、项目编辑器、Minecraft 版本选择器、通用列表与 API 调用。检查方式包括真实引用搜索、路由和动态导入核对、Go 编译/静态检查、TypeScript 严格未使用符号检查、生产构建和 PostgreSQL 集成测试。

两个仓库在开始前已有大量未提交的功能修改；本轮把它们视为用户现有工作，只处理能够通过真实调用链确认的整理项，没有回滚或覆盖其他改动。

修改前基线：

- 后端 `go build ./...`、`go test ./... -count=1` 均通过。
- 后端 PostgreSQL 集成测试通过：`internal/database` 3.083 秒，`internal/httpapi` 42.982 秒。
- 前端 `pnpm lint`、`pnpm typecheck`、`pnpm build` 均通过；构建生成 53 个静态路由。
- 前端没有配置独立测试脚本，现有脚本只有 `dev`、`build`、`start`、`lint`、`typecheck` 和 `check`。
- 本轮执行的基线中没有失败测试，因此没有删除、跳过或改写失败测试。

## 已清理和合并的实现

### 前端

- 删除 `catalog-state.ts` 中已经没有调用方的 `matchesCatalogUpdatedRange` 和 `normalizeCatalogSearch`。筛选继续以 URL 和后端查询为事实来源，不再保留旧的前端本地假筛选工具。
- 将 Mod、整合包和通用大型项目编辑器中的三份站内项目 ID 规范化逻辑合并到 `project-identifiers.ts`。字符白名单、大小写和 100 字符上限保持不变。
- 将 Minecraft 版本配置的请求、请求合并和模块级缓存集中到 `minecraft-version-api.ts`。选择器、Mod 编辑器、整合包编辑器和资料工作区共享同一读取入口；管理端首次加载可强制刷新，保存或同步后更新同一缓存。
- 消息中心内部调用改为权威入口 `/api/v1/me/unread-summary`。旧 `/api/v1/notifications/unread` 仅保留在后端路由边界供旧客户端使用。
- 将全局资源目录内部的链接式分页组件直接命名为 `GlobalCatalogPagination`，避免与受控列表分页组件同名。两者生命周期和路由语义不同，因此没有错误合并。
- 使用 `tsc --noUnusedLocals --noUnusedParameters` 复查真实源码，没有发现剩余的未使用局部符号。Next.js 路由默认导出虽然只有框架调用，均已保留。

### 后端

- 活动监控原有默认参数构造器没有生产或测试调用方；删除该转发后，将唯一的内部构造器直接命名为 `NewMonitor`，并更新运行时和集成测试。没有保留 `WithOptions` 别名或桥接函数。
- 删除 `settings_cache.go` 中无调用方的 `settingMissing` 辅助函数及其无用导入。
- 将反滥用账户状态、机器人规则和设置缓存 Key 集中到反滥用 Service。Handler 不再自行拼接同一 Key 或复制失效规则。
- 管理员修改用户风控状态、创建限制或解除限制时，仅失效目标用户的账户风险缓存。解除限制通过 `UPDATE ... RETURNING user_id` 获取准确目标，不再清空所有用户缓存。
- 机器人规则和反滥用设置使用精确删除；仅账户状态因包含网络和设备变体而使用限定到单一用户的前缀失效。
- 新增测试证明失效一个账户不会删除另一个账户的缓存。
- 执行 `go mod tidy`：`miniredis` 和 `x/text` 按真实源码引用归入直接依赖，`gopher-lua` 保持间接依赖并补全校验和。没有发现能够安全删除的 Go 模块。

## 权威实现与保留差异

- 项目列表的排序、方向、列表参数白名单和稳定次级排序继续由后端 `catalog_sort.go` 统一负责。
- Minecraft 版本配置的前端权威读取入口为 `loadMinecraftVersionConfig`；管理写入后的缓存同步入口为 `cacheMinecraftVersionConfig`。
- 项目站内 ID 的前端输入规范化入口为 `normalizeProjectSiteIdInput`。
- 反滥用缓存失效由 `antiabuse.Service` 负责；Handler 只传递具体业务对象。
- PostgreSQL 继续是认证、权限、消息、审核、统计和事件 Outbox 的事实来源；没有为了“统一”删除现有可靠队列或引入第二套业务状态。
- 全局目录链接分页与客户端受控分页没有合并，因为前者生成可分享 URL，后者依赖回调和本地状态。
- Mod 的专用列表与可配置的大型项目列表没有强行合成万能组件；当前筛选字段、查询索引和业务卡片结构仍有实质差异。

## 兼容层

以下代码有真实外部契约责任，已保留并限制在边界；完整清单见 `COMPATIBILITY_BOUNDARIES.md`：

- `/health` 到 `/ready` 的弃用别名；
- `/api/v1/notifications/unread` 到统一未读摘要 Handler 的旧客户端路由；
- `latest`、`oldest`、`created`、`nameAsc`、`nameDesc` 等旧列表排序参数；
- 旧 Core NATS 原始负载的解码兼容；
- 旧 Blueprint/Schematic 文件格式读取；
- 已发布 URL、JSON 字段、数据库值、事件类型、环境变量和迁移历史。

仓库无法证明所有生产探针、旧客户端、书签和第三方导入方已经迁移，因此这些边界不能安全删除。需要生产访问日志、客户端最低版本和第三方集成清单后才能制定移除期限。

## 依赖、配置和数据库

- 前端依赖逐项搜索后均有真实引用；`fflate` 和 `nbt-ts` 由导出器使用，没有删除。
- 后端没有删除依赖；`go mod tidy` 只修正直接/间接分类并补全 `go.sum`。
- 没有发现能够在不知道生产部署配置的情况下安全删除的 Feature Flag 或环境变量。
- 本轮不需要数据库结构变更，没有新增迁移，也没有删除或重写任何既有迁移。
- 没有删除审计、操作日志、权限历史、交易数据或后台任务表。

## 最终验证

实际执行命令：

```powershell
go test ./internal/antiabuse ./internal/httpapi ./internal/activity -count=1
go test ./internal/activity ./internal/app -count=1
go vet ./...
go build ./...
go test ./... -count=1
$env:MCMODS_RUN_DB_INTEGRATION='1'; go test ./internal/database ./internal/httpapi -run 'Integration' -count=1
pnpm lint
pnpm typecheck
pnpm exec tsc --noEmit --noUnusedLocals --noUnusedParameters
pnpm build
git diff --check
```

结果：

- 后端全部包编译、Vet 和单元测试通过。
- 最终 PostgreSQL 集成测试通过：`internal/database` 2.731 秒，`internal/httpapi` 42.311 秒。
- 前端 Lint、类型检查、严格未使用符号检查和生产构建通过；53 个静态路由全部生成。
- 运行中的 Dev 服务只读冒烟通过：`/live`、`/ready`、Minecraft 版本配置、按热度排序的 Mod 列表和按更新时间排序的新闻列表均返回 HTTP 200。
- `git diff --check` 没有空白错误；输出中的 LF/CRLF 信息是现有 Git 行尾提示，不是差异错误。

本轮源码统计从后端 342 个 Go 文件/89,236 行、前端 297 个源码文件/50,579 行，变为后端 342 个 Go 文件/89,276 行、前端 299 个源码文件/50,589 行。净增加来自共享权威模块和缓存隔离测试；三份重复规范化、重复版本请求状态、两个死工具函数和旧构造转发已经删除。代码行数不是本轮唯一目标，调用入口和失效规则的数量实际减少。

## 重要修改文件

前端：

- `app/_lib/project-identifiers.ts`
- `app/_lib/minecraft-version-api.ts`
- `app/_lib/catalog-state.ts`
- `app/_components/minecraft-version-picker.tsx`
- `app/_components/mod-editor.tsx`
- `app/_components/modpack-editor.tsx`
- `app/_components/simple-project-editor.tsx`
- `app/_components/mod-content-workspace.tsx`
- `app/_components/admin-mod-panels.tsx`
- `app/_components/messages-center.tsx`
- `app/_components/global-catalog.tsx`

后端：

- `internal/activity/monitor.go`
- `internal/activity/postgres_store_integration_test.go`
- `internal/app/runtime.go`
- `internal/antiabuse/service.go`
- `internal/antiabuse/crawler.go`
- `internal/antiabuse/service_test.go`
- `internal/httpapi/anti_abuse_handlers.go`
- `internal/httpapi/settings_cache.go`
- `go.mod`
- `go.sum`

## 尚存技术债务与后续建议

- `internal/httpapi` 仍然是大型包，部分 Handler 同时承担参数解析和 SQL 投影；建议按真实业务域逐步抽取查询对象，不应一次性建立反射式通用仓储。
- 前端尚未配置组件级测试框架；目前依赖 TypeScript、ESLint、生产构建和后端集成测试。可单独引入一套轻量测试方案，优先覆盖 Minecraft 版本缓存、URL 筛选恢复和统一排序参数。
- 部分对外兼容层没有生产使用率数据，不能在本轮安全删除。
- CSS 只有一个全局样式入口，且包含 Markdown 动态生成类；没有仅凭文本搜索删除样式。
- 本轮没有进行新的压力测试，因为整理没有改变数据库模型或公开接口；既有负载测试资料保留，若要评估版本配置请求合并的浏览器收益，建议在真实多组件页面上用浏览器网络记录单独量化。
