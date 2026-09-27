# 验证日志

## 2026-08-21：工作区与审计基线

| 检查 | 结果 |
| --- | --- |
| 后端分支 / HEAD | `codex/unified-catalogs-user-systems` / `bd50afd0ef92192c40c5152ea56e27a3fdbb657f` |
| 前端分支 / HEAD | `codex/unified-catalogs-user-systems` / `4f82c9075636cc4f1f6d3eb07de37a3fffd1de58` |
| 基线差异 | 两仓 HEAD 均等于审计基准；存在开始前用户未提交修改，已登记保护 |
| 全审计目录 ID 扫描 | 449 unique；BUG 151、SEC 47、PERF 72、DB 8、ARCH 33、REUSE 7、TEST 50、OPS 22、STYLE 9、MAP 12、LEGACY 22、DEAD 16 |

后续每条记录必须写实际命令、环境、开始/结束时间或持续时间、退出码、通过/失败、覆盖的 Finding 和未验证限制。不得把审计历史命令当成本轮通过结果。

## 2026-08-21：治理台账与审计附录完整性

环境：Windows PowerShell；`D:\System\SDK\go\go1.26.4\bin\go.exe`；后端工作区根目录。

| 检查 | 实际命令/方法 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 开放期 Finding 集合校验 | `go run ./tools/remediation/verify_findings -allow-open` | 949 ms | 0 | PASS：审计与台账均为 449 unique；12 类计数完全一致；当前 449 OPEN |
| 最终严格关闭门禁 | `go run ./tools/remediation/verify_findings` | 670 ms | 1 | 预期 FAIL：622 个未满足项；449 OPEN，且 172 项严重级别仍为 `UNRESOLVED` |
| 文件覆盖清单 | 逐行解析 `modules/FILE_COVERAGE_INVENTORY.md` 的 Markdown 数据行与状态列 | <1 s | 0 | 884 行：791 `已完成`、93 `排除`，与汇总一致 |
| API 路由清单 | 逐行解析 `modules/API_ROUTE_INVENTORY.md` 的方法列 | <1 s | 0 | 489：GET 244、POST 125、PUT 71、DELETE 35、PATCH 14 |
| Schema 目录 | 按章节解析 `database/SCHEMA_CATALOG.md` 的表格行和项目符号 | <1 s | 0 | 列 2450、约束 3469、索引 978、触发器 137、视图 3、序列 119、函数 123，均与汇总一致 |

说明：严格门禁失败是当前真实基线，不是环境阻塞，也不会被改写为通过。其用途是保证只有所有 Finding 均具有合法严重级别、关闭状态、代码证据、测试与验证结果时才能完成本目标。

## 2026-08-21：SEC-011 供应商凭据 origin

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前定向红测 | `go test ./internal/httpapi -run 'Test(PrepareModImportConfigUpdate&#124;NormalizeModImportConfigRejectsCredentialsForNonOfficialOrigin&#124;ProviderCredentialHeadersFailClosedForWrongOrigin)$' -count=1` | 2.6 s | 1 | 预期 FAIL：三个安全边界函数尚不存在 |
| 修复后定向测试 | `go test ./internal/httpapi -run 'Test(PrepareModImportConfigUpdateDoesNotCarryCredentialsAcrossOrigins&#124;NormalizeModImportConfigRejectsCredentialsForNonOfficialOrigin&#124;ProviderCredentialHeadersFailClosedForWrongOrigin&#124;NormalizeModImportConfigRequiresCurseForgeKey&#124;RedactModImportConfig&#124;ProviderClientRejectsCrossHostRedirect)$' -count=1` | 8.0 s | 0 | PASS（Go 报告 1.265s） |
| `internal/httpapi` 包全量 | `go test ./internal/httpapi -count=1` | 7064 ms | 0 | PASS（Go 报告 1.559s） |
| 格式/空白 | `gofmt`（全部受影响 Go 文件）；`git diff --check` | <1 s | 0 | PASS；仅显示仓库既有 CRLF 转换提示，无空白错误 |

第一次包全量复跑曾因既有 `TestImportCurseForgeAuthor` 仍断言自定义测试 origin 会收到 API Key 而失败；该测试已改为相反的泄漏防护断言，随后包全量通过。这是安全协议更新所需的测试迁移，不是忽略失败。

## 2026-08-21：SEC-013 Mod 关系所有权

| 检查 | 实际命令 | 环境/耗时 | 退出码 | 结果 |
| --- | --- | --- | ---: | --- |
| 纯函数红测 | `go test ./internal/httpapi -run 'TestNormalizeModRequestTreatsIncomingRelationshipsAsReadOnly$' -count=1` | Go 1.26.4 / 2.6 s（与首轮 DB 命令同批） | 1 | 预期 FAIL：`incoming` 仍进入权威请求 snapshot |
| PostgreSQL 安全红测 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'Test(IncomingModRelationshipSnapshotCannotRewriteSourceProject&#124;SymmetricModRelationshipDoesNotDeleteTargetOwnedReverse&#124;OutgoingModRelationshipRejectsUnpublishedTarget)$' -count=1` | 远程测试 PostgreSQL / 10.0 s | 1 | 预期 FAIL：原关系被删且伪造关系=1；对方反向声明被删；待审目标被接受 |
| PostgreSQL 修复验证 | 同上 | 11.1 s；Go 报告 1.929s | 0 | PASS；每个测试独立事务并回滚，无持久测试数据 |
| 后端包全量 | `go test ./internal/httpapi -count=1` | 2730 ms；Go 报告 0.529s | 0 | PASS |
| 前端类型检查 | bundled Node 运行 `node_modules/typescript/bin/tsc --noEmit --pretty false` | 13.0 s | 0 | PASS |
| 前端定向 Lint | bundled Node 运行 `node_modules/eslint/bin/eslint.js app/_components/mod-editor.tsx` | 7.9 s | 0 | PASS |
| 两仓差异空白检查 | `git diff --check` | <1 s/仓库 | 0 | PASS；仅既有 CRLF 转换提示 |

首轮 DB 红测的夹具遗漏远程 Schema 所需的非空 `minecraft_versions`，先在夹具阶段失败；补齐空数组后重新运行才得到上述三个有效安全红测结果。未把夹具失败计作漏洞复现或通过证据。

## 2026-08-21：SEC-039 经济整数边界与守恒

环境：Go 1.26.4；Windows PowerShell；真实 PostgreSQL 用例在独立事务内回滚；未重置现有数据库。

| 检查 | 实际命令 | 环境/耗时 | 退出码 | 结果 |
| --- | --- | --- | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi -run 'Test(CalculateTransferAmounts&#124;CalculateShopTotal&#124;CheckedCurrencyBalance&#124;NormalizeEconomyConfig)' -count=1` | Go 编译阶段；首轮未单独计时 | 1 | 预期 FAIL：有界金额常量及 checked arithmetic 函数尚不存在 |
| 算术与配置定向测试 | 同上 | Go 报告 1.205s | 0 | PASS：税额分解、最大合法值、溢出输入、余额上下界与超域奖励均覆盖 |
| Schema 源契约 | `go test ./internal/database -run 'Test(EconomySchema&#124;UserFeatureSchema)' -count=1` | Go 报告 0.857s | 0 | PASS：generation 86 及金额/购买/悬赏约束存在 |
| PostgreSQL 拒绝原子性 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCurrencyBalanceRangeFailureDoesNotWriteLedger$' -count=1` | 远程 PostgreSQL；Go 报告 0.896s | 0 | PASS：越界入账后余额仍为 50，流水仍为 1；测试事务回滚 |
| 后端包回归 | `go test ./internal/httpapi ./internal/database -count=1` | Go 分别报告 0.439s / 0.107s | 0 | PASS |
| 后端全仓回归 | `go test ./... -count=1` | 9.5s 命令批次内；最慢包 2.422s | 0 | PASS |
| 静态与构建门禁 | `go vet ./...`；`go build ./...` | 先前完整复跑分别 2.807s / 5.528s；最终代码后再次通过 | 0 | PASS |
| 差异空白 | `git diff --check` | <1s | 0 | PASS；仅既有 CRLF 转换提示 |
| 隔离空库 generation 86 动态安装 | 随机名临时库工具两次尝试，工具随后删除 | 2.636s / 2.684s | 1 / 1 | 环境限制：维护库被 `pg_hba` 拒绝，允许连接的业务库账号又无 `CREATEDB`；两次均在 CREATE 成功前退出，没有创建、重置或修改任何数据库。Schema 源契约通过，但最终空库动态门禁仍待隔离数据库权限 |

首轮修复后测试曾暴露一条测试期望写错（`9999/9999bps` 并不会留下正净额）；改用 `10000/9999bps` 验证净额 1 后通过。该失败未被计作安全修复通过证据。

## 2026-08-21：SEC-040 / SEC-044 授权来源模型

环境：Go 1.26.4；Windows PowerShell；PostgreSQL 用例创建 session-local 临时表并在事务内回滚，不改动永久 Schema 或业务数据。

| 检查 | 实际命令 | 环境/耗时 | 退出码 | 结果 |
| --- | --- | --- | ---: | --- |
| Schema 红测 | `go test ./internal/database -run '^TestAuthorizationBindingsHaveExplicitIndependentSources$' -count=1` | Go 报告 0.853s | 1 | 预期 FAIL：旧 Schema 没有 source/source_key 与多来源主键 |
| 后台保存红测 | `go test ./internal/httpapi -run '^TestReplaceManualAuthorizationPreservesSystemSourcesAndExpiry$' -count=1` | 编译阶段 | 1 | 预期 FAIL：manual-only 替换 helper 与 grant 类型不存在 |
| 等级切换红测 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/progression -run '^TestSyncTrackRoleReplacesOnlyLevelSourceAcrossTrackSwitchAndClear$' -count=1` | 真实 PostgreSQL；Go 报告 1.479s | 1 | 预期 FAIL：manual:10 被删、旧 level_track:12 保留、新角色错误写成 manual:11 |
| Schema/解析/路由/锁门禁 | `go test ./internal/database ./internal/httpapi ./internal/progression -run 'Test(AuthorizationBindingsHaveExplicitIndependentSources&#124;NormalizeManualAuthorizationEntries&#124;LegacyUserRoleReplacementRouteIsRemoved&#124;RoleTrackMutationLocksUserBeforeReadingManualBindings&#124;ShiftRoleTrackRoles)' -count=1` | 最终定向运行各包均通过；HTTP 源门禁 Go 报告 1.139s | 0 | PASS |
| 后台来源并存 PostgreSQL | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestReplaceManualAuthorizationPreservesSystemSourcesAndExpiry$' -count=1` | Go 报告 1.491s | 0 | PASS：manual 替换保留 level、preference 和到期时间；默认封禁角色按稳定来源替换/清空 |
| 等级来源 PostgreSQL | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/progression -run '^TestSyncTrackRoleReplacesOnlyLevelSourceAcrossTrackSwitchAndClear$' -count=1` | Go 报告 0.858s | 0 | PASS：切换和清空均撤销旧 level 来源，manual 不变 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 完整最终批次 23.2s；全仓最慢包 0.760s | 0 | PASS |
| 前端类型与 Lint | bundled Node 运行 `node_modules/typescript/bin/tsc --noEmit --pretty false`；ESLint `admin-console.tsx`、`mod-editor.tsx` | 9.6s | 0 | PASS |
| 两仓差异空白 | `git diff --check` | <1s/仓 | 0 | PASS；仅既有 CRLF 转换提示 |
| BUG-122 初始红测 | `go test ./internal/httpapi -run 'Test(ValidateRoleGraphRejectsMissingParentsAndCycles&#124;RoleDeletionBlockersCoverGlobalAuthorizationDependencies)' -count=1` | 2.5s | 1 | 预期 FAIL：DAG 与删除依赖函数不存在；首轮同时发现测试 helper 与既有名称冲突，改名后复跑确认仅缺实现 |
| 角色 DAG | 同上（无 DB 环境时依赖用例 skip） | Go 报告 1.290s | 0 | PASS：缺父级和 A→B→C→A 被拒绝，合法 DAG 通过 |
| 删除依赖 PostgreSQL | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestRoleDeletionBlockersCoverGlobalAuthorizationDependencies$' -count=1` | Go 报告 1.016s | 0 | PASS：默认配置、子角色、用户绑定和轨道全部成为 blocker；无引用角色可删 |
| BUG-122 后全仓 / Vet / 构建 | PostgreSQL 定向后运行 `go test ./... -count=1`、`go vet ./...`、`go build ./...`、`git diff --check` | 20.6s 完整批次；全仓最慢包 0.966s | 0 | PASS |

generation 87 空库动态安装继承上一批环境限制：远程账号只能连接指定业务库且没有 `CREATEDB`。本轮没有重试破坏性操作；Schema 源契约通过，最终隔离空库动态门禁仍保留。

## 2026-08-21：BUG-095 / BUG-096 / BUG-097 可执行经济配置

环境：Go 1.26.4；Windows PowerShell；PostgreSQL 定向用例仅创建 session-local 临时表并由事务回滚；前端使用 bundled Node 和现有 `package-lock.json` 安装树。

| 检查 | 实际命令 | 环境/耗时 | 退出码 | 结果 |
| --- | --- | --- | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi ./internal/database -run 'Test(SupportedShopItemTypesMatchExecutableUsePaths&#124;EconomySchemaEnforcesExecutableItemsAndAtomicPromotionSequences&#124;EconomyCurrencyReferencesRequireActiveCurrencies&#124;HeatPromotionSequenceUsesAtomicCounter)' -count=1` | 3.5s | 1 | 预期 FAIL：商品闭集、active 引用、原子 counter helper 均不存在，Schema 仍为 generation 87 |
| 纯函数/Schema/源门禁 | `go test ./internal/httpapi ./internal/database -run 'Test(SupportedShopItemTypesMatchExecutableUsePaths&#124;EconomySchemaEnforcesExecutableItemsAndAtomicPromotionSequences&#124;EffectivePromotionPowerDiminishesRepeatedUse&#124;NormalizeEconomyConfigRejectsOutOfDomainRewards&#124;EconomyHandlersUseTransactionalConfigurationInvariants)' -count=1` | 最终复跑包含于定向批次；Go 包均通过 | 0 | PASS：三值闭集、generation 88、counter/唯一约束、旧 count 查询删除和运行时锁后读配置均受门禁 |
| PostgreSQL 配置与 counter | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'Test(EconomyCurrencyReferencesRequireActiveCurrencies&#124;HeatPromotionSequenceUsesAtomicCounter)' -count=1` | 10.9s 定向批次；Go 报告 1.264s | 0 | PASS：disabled 奖励货币拒绝，active 后接受；签到/active 商品 blocker 存在；同一目标返回序号 1/2/3；事务回滚 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 19.4s；全仓最慢 HTTP 1.818s | 0 | PASS |
| 前端类型与定向 Lint | bundled Node 运行 `tsc --noEmit --pretty false`；ESLint `admin-community-panels.tsx`、`community-api.ts` | 9.9s | 0 | PASS：API 类型闭集与管理员 select 一致 |
| 两仓差异空白 | `git diff --check` | <1s/仓 | 0 | PASS；仅既有 CRLF 转换提示 |

generation 88 空库动态安装继承已登记的远程 `CREATEDB` 限制。本轮没有重试建库、重置或执行任何永久 DDL；Schema 源契约通过，最终隔离空库动态安装与 EXPLAIN 仍是 Goal 级门禁。

## 2026-08-21：SEC-025 / SEC-047 供应商身份与脚本供应链

环境：Go 1.26.4；bundled Node 24.19.0；Next.js 16.2.11；Windows PowerShell。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| SEC-025 修复前红测 | `go test ./internal/httpapi -run 'Test(ModrinthDownloadIdentityRequiresOfficialConsistentCDNPaths&#124;ModrinthIndexModsDoesNotBindIdentityFromUntrustedURL&#124;BuildMRPackRejectsUnsafeOrIncompleteFiles)' -count=1` | 2.3s | 1 | 预期 FAIL：统一身份解析和一致镜像函数不存在 |
| SEC-025 定向测试 | `go test ./internal/httpapi -run 'Test(ModrinthDownloadIdentityRequiresOfficialConsistentCDNPaths&#124;ModrinthIndexModsDoesNotBindIdentityFromUntrustedURL&#124;ModrinthIndexModsExtractsProviderReferences&#124;BuildMRPackRejectsUnsafeOrIncompleteFiles&#124;BuildMRPackUsesRootIndexAndLoaderDependency)' -count=1` | 6.7s；Go 报告 1.106s | 0 | PASS：官方身份保留，攻击域、相似域、HTTP、凭据、端口、query/fragment、嵌套路径与冲突镜像全部失败关闭 |
| SEC-047 URL 红测 | bundled Node `--test app/_lib/iconfont-url.test.mts` | 0.7s | 1 | 预期 FAIL：安全 URL 模块尚不存在 |
| SEC-047 SRI 红测 | 同命令，加入配置完整性用例后复跑 | 0.7s | 1 | 预期 FAIL：`resolveIconfontConfig` 尚未导出 |
| SEC-047 测试/类型/Lint | Node test；`tsc --noEmit --pretty false`；ESLint `layout.tsx`、`iconfont.tsx`、URL 模块与测试 | 9.4s | 0 | PASS：3 tests；精确 URL、非法组合和 SHA-384 启动契约通过 |
| 前端生产构建 | bundled Node 运行 `node_modules/next/dist/bin/next build` | 22.8s | 0 | PASS：编译、TypeScript、31 workers 页面数据与 59 个静态页完成 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 12.7s；全仓最慢 app 1.235s | 0 | PASS |
| 两仓差异空白 | `git diff --check` | <1s/仓 | 0 | PASS；仅既有 CRLF 转换提示 |

前端生产构建在 Iconfont 完全未配置的合法 fallback 模式执行；配置 URL/SRI 的接受与非法/半配置抛错由同一启动函数的 Node 测试直接覆盖。构建未下载或执行任何远程 Iconfont 脚本。

## 2026-08-21：SEC-042 举报目标可见性

环境：Go 1.26.4；真实 PostgreSQL 用例使用 session-local 临时表并在事务内回滚，不修改永久 Schema 或业务数据。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi -run '^TestReportTargetVisibilityReusesProjectAndCommentAuthorization$' -count=1` | 2.0s | 1 | 预期 FAIL：举报可见性门不存在 |
| PostgreSQL 可见性 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestReportTargetVisibilityReusesProjectAndCommentAuthorization$' -count=1` | 3.6s；Go 报告 1.200s | 0 | PASS：pending Mod 与其 published 评论对普通用户均返回不可见，owner 可见；审批后普通用户可见；pending 评论始终拒绝 |
| 事务/复用源门禁 | `go test ./internal/httpapi -run 'Test(UnifiedReportSnapshotUsesOneRepeatableReadVisibilityBoundary&#124;ReportReasonRegistryCoversEveryTargetAndOther)' -count=1` | 9.4s 定向批次内；Go 报告 1.096s | 0 | PASS：Repeatable Read、claims 传递、两类解析器复用和上下文状态过滤均受守护 |
| HTTP 包最终回归 | `go test ./internal/httpapi -count=1` | 6.7s；Go 报告 1.401s | 0 | PASS |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 12.4s；全仓最慢 app 1.158s | 0 | PASS |
| 差异空白 | `git diff --check` | <1s | 0 | PASS；仅既有 CRLF 转换提示 |

严重度由 `UNRESOLVED` 逐项复核为 High：漏洞可让登录用户跨审核/隐私边界复制完整目标快照并触发治理留存，影响正文、服务器地址和关系等数据；利用仍要求 `report.create`、目标随机 ID 和一次写请求，因此未使用审计中不存在的 Critical 档。

## 2026-08-21：SEC-035 收藏目标可见性

环境：Go 1.26.4；真实 PostgreSQL 用例使用 session-local 临时表并在事务内回滚，不修改永久 Schema 或业务数据。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/database ./internal/httpapi -run 'TestFavorite(CollectionsHaveExplicitVisibility&#124;TargetsRevalidateVisibilityForEveryViewer)' -count=1` | 3.2s | 1 | 预期 FAIL：统一收藏目标解析、可见摘要/列表 loader 不存在，Schema 无类型闭集 |
| PostgreSQL 可见性 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestFavoriteTargetsRevalidateVisibilityForEveryViewer$' -count=1 -v` | 7.9s；Go 报告 2.347s | 0 | PASS：集合 owner 仅见当前有权 4 项，普通访客仅见公开 2 项；他人 pending Mod、他人 processing 蓝图、plugin 与类型错配拒绝；事务回滚 |
| Schema/事务/旁路源门禁 | `go test ./internal/httpapi ./internal/database -run 'TestFavorite' -count=1` | 6.7s；Go 包分别报告 1.146s / 0.085s | 0 | PASS：三值 CHECK、Repeatable Read 写入、摘要/列表共享谓词和 MRPack 预检 claims 过滤受守护 |
| HTTP/数据库包回归 | `go test ./internal/httpapi ./internal/database -count=1` | Go 分别报告 0.458s / 0.101s | 0 | PASS |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 最终批次全仓最慢 app 1.943s；构建 4.4s | 0 | PASS；首次构建命令误写不存在的 `cmd/api`/`cmd/worker` 而退出 1，随后按仓库真实入口 `go build ./...` 通过，不计作代码失败 |

generation 89 空库动态安装继承已登记的远程 `CREATEDB` 限制。本轮没有建库、重置或执行任何永久 DDL；Schema 源契约通过，最终隔离空库动态安装与 EXPLAIN 仍是 Goal 级门禁。

## 2026-08-21：SEC-037 社区项目引用可见性

环境：Go 1.26.4；bundled Node 24.19.0；Next.js 16.2.11；真实 PostgreSQL 用例使用 session-local 临时表并事务回滚。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi -run '^TestCommunityPostProjectReferencesRevalidateVisibilityForEveryViewer$' -count=1` | 2.7s | 1 | 预期 FAIL：claims 化保存解析、批量可见引用 loader 与 `unavailable` 占位不存在 |
| PostgreSQL 写入/读取可见性 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCommunityPostProjectReferencesRevalidateVisibilityForEveryViewer$' -count=1 -v` | 7.3s；Go 报告 1.950s | 0 | PASS：他人 pending 保存拒绝、owner 保存允许；普通 viewer 的隐藏 Mod/插件及他人 pending 均为零元数据占位，approved 后动态恢复；事务回滚 |
| 统一解析/筛选源门禁 | `go test ./internal/httpapi -run 'Test(CommunityPostReferencePathsShareCurrentViewerVisibility&#124;CommunityPostProjectReferencesRevalidateVisibilityForEveryViewer&#124;FavoriteTargetsRevalidateVisibilityForEveryViewer&#124;ReportTargetVisibilityReusesProjectAndCommentAuthorization)' -count=1` | 6.3s；Go 报告 1.122s | 0 | PASS：保存、详情、批量装配、目录筛选、收藏与举报共享当前 viewer 边界；旧目标元数据直联被删除 |
| 前端类型与 Lint | bundled Node 运行 `tsc --noEmit --pretty false`；ESLint `community-post-api.ts`、`community-post-detail.tsx` | 5.9s | 0 | PASS：可选占位协议和安全展示通过 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 与前端构建并行批次 25.7s；最慢 database 2.092s | 0 | PASS |
| 前端测试 / 生产构建 | Node Iconfont 3 tests；Next production build | 同批次；编译 8.5s、TypeScript 10.9s、59 页 476ms | 0 | PASS；无远程脚本执行 |

本项不修改 Schema，权威版本保持 generation 89。用户开始前已有的 `community_post_handlers.go` / test 修改（issue 必须有项目但允许没有小资源）在差异中完整保留。

## 2026-08-21：BUG-098 / BUG-099 / BUG-100 / ARCH-028 / PERF-057 表情完整性

环境：Go 1.26.4；真实 PostgreSQL 测试只创建 session-local 临时表/触发器，公共函数 DDL 位于测试事务并整体回滚；没有修改永久 Schema 或业务数据。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 引用/事务红测 | `go test ./internal/database ./internal/httpapi -run 'Test(StickerReferenceSchemaDefinesIndexedTransactionalProjection&#124;ParseStickerTokenRequiresOneExactCanonicalToken&#124;StickerLifecycleUsesStructuredReferencesAndTransactionLocks)' -count=1` | 2.3s | 1 | 预期 FAIL：generation 90 引用 Schema 与 exact Token 解析尚不存在 |
| 目录预算红测 | `go test ./internal/httpapi -run 'Test(FeatureLimitDefaultsAndOverrides&#124;StickerLifecycleUsesStructuredReferencesAndTransactionLocks)' -count=1` | 2.2s | 1 | 预期 FAIL：目录包/每包/总量配置字段与预算锁尚不存在 |
| Schema/生命周期定向测试 | `go test ./internal/database ./internal/httpapi -run 'Test(EngagementExportSchemaDefinesCurrentContracts&#124;StickerReferenceSchemaDefinesIndexedTransactionalProjection&#124;ParseStickerTokenRequiresOneExactCanonicalToken&#124;StickerLifecycleUsesStructuredReferencesAndTransactionLocks&#124;StickerMutationDatabaseErrorsDistinguishImageOwnership&#124;StickerPackCreateErrorsOnlyClassifyItsCodeConstraintAsConflict&#124;FeatureLimitDefaultsAndOverrides)' -count=1` | 最终各包通过，HTTP Go 报告 1.18s | 0 | PASS：图片唯一所有权、精确错误分类、父行/OSS/token/预算锁和目录硬上限均受守护 |
| PostgreSQL 触发器与索引 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database ./internal/httpapi -run 'Test(StickerReferenceProjectionIntegration&#124;StickerMarkdownUsageQueryIntegration)' -count=1 -v` | 6.5s；database/httpapi 分别报告 3.396s / 2.303s | 0 | PASS：发现 20 个公开 Markdown/历史字段；文本与 JSON 插入、去重、更新、删除正确；第二事务被同 token 锁阻塞；`EXPLAIN` 命中 token 索引 |
| 数据库/HTTP 包回归 | `go test ./internal/database ./internal/httpapi -count=1` | Go 分别报告 0.091s / 0.513s | 0 | PASS |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行批次 18.8s；全仓最慢 app 7.624s | 0 | PASS |
| 开放期 Finding 校验 | `go run ./tools/remediation/verify_findings -allow-open` | 1.4s 并行批次内 | 0 | PASS：449 unique；CLOSED 24 / OPEN 425；High 85 / Medium 151 / Low 47 / UNRESOLVED 166 |
| 两仓差异空白 | `git diff --check` | 同批次 <1.5s | 0 | PASS；仅既有 CRLF 转换提示 |

真实 PostgreSQL 测试把引用表和源表建为 session-local 临时表；同一事务内临时替换引用函数并在回滚时恢复，因此未留下函数、触发器、表或测试数据。generation 90 的完整隔离空库安装仍继承远程账号无 `CREATEDB`、维护库被 `pg_hba` 拒绝的既有 Goal 级门禁；本轮没有重置或执行永久 DDL。

## 2026-08-21：SEC-041 / BUG-101 / TEST-045 表情净化、缓存与回归网

环境：Go 1.26.4；bundled Node 24.19.0；Next.js 16.2.11；真实 PostgreSQL 用例只创建 session-local 临时表并由事务回滚；没有调用真实 OSS 写接口或修改永久数据库。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 图片净化红测 | `go test ./internal/httpapi -run 'TestSanitizeStickerImage' -count=1` | 2.2s | 1 | 预期 FAIL：净化函数和派生图片元数据尚不存在，测试在编译阶段失败 |
| 客户端缓存/AST 红测 | bundled Node `--test app/_lib/sticker-catalog-cache.test.mts app/_lib/sticker-markdown.test.mts` | <1s | 1 | 预期 FAIL：两个纯模块尚不存在；5 个目标测试中对应模块加载失败 |
| 图片净化与权限定向 | `go test ./internal/httpapi -run 'Test(SanitizeStickerImage&#124;OnlySanitizedStickerDerivativesArePublicInlineFiles&#124;StickerRoutesKeepReadManageAndUploadPermissionsSeparate)' -count=1` | 最终包含于 HTTP 包复跑 | 0 | PASS：PNG tEXt、GIF comment、帧预算、MIME 错配、截断输入、派生来源与三种权限边界均通过 |
| OSS 生命周期 PostgreSQL | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestStickerImageTombstoneRequiresNoRemainingOwner$' -count=1 -v` | 8.1s；Go 报告 2.302s | 0 | PASS：仍绑定时保持 active/不入队，解绑后 deleted/入队一次，重试不重复入队；事务回滚 |
| 引用投影 PostgreSQL | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestStickerReferenceProjectionIntegration$' -count=1 -v` | 3.5s；Go 报告 1.812s | 0 | PASS：20 个 Markdown/历史字段、严格 Token、索引和竞争锁继续通过 |
| 后端全仓 / Vet / 构建 | `go test ./...`；`go vet ./...`；`go build ./...` | 14.2s；全仓测试 7.7s，静态/构建 6.5s | 0 | PASS |
| 前端行为测试 / 类型 | `pnpm test`；`pnpm exec tsc --noEmit` | 3.5s；Node 198.7ms | 0 | PASS：Iconfont 3 项及表情缓存/AST 5 项，共 8 tests |
| 前端 Lint / 生产构建 | ESLint 7 个贴纸变更文件；`pnpm run build` | 28.0s | 0 | PASS：编译 7.9s、TypeScript 9.8s、59 个静态页 542ms |
| 开放期 Finding 校验 | `go run ./tools/remediation/verify_findings -allow-open` | 1.3s（含后端差异检查） | 0 | PASS：449 unique；CLOSED 27 / OPEN 422；High 85 / Medium 154 / Low 47 / UNRESOLVED 163 |
| 两仓差异空白 | 两仓 `git diff --check` | <1s/仓 | 0 | PASS；仅既有 CRLF 转换提示 |

OSS 上传失败后的直接对象删除由共享写入边界和源契约守护；本轮没有可安全使用的隔离 OSS 测试桶，因此没有伪造“真实 OSS 集成通过”。图片字节净化、数据库绑定/删除和浏览器缓存分别在无外部副作用的层级做了直接行为验证。完整 generation 90 空库安装继续保留已登记的远程权限限制。

## 2026-08-21：SEC-007 Markdown 差异资源预算

环境：Go 1.26.4；Windows PowerShell；纯内存行为测试，不访问数据库或外部服务。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| frontier 红测 | `go test ./internal/activity -run '^TestMarkdownDiffFrontierIsBoundedByExactDistanceBudget$' -count=1` | 1.4s | 1 | 预期 FAIL：固定距离预算与可验证 frontier 尺寸函数尚不存在 |
| 业务入口预算红测 | `go test ./internal/httpapi -run '^TestMarkdownDeltaCallersEnforceBusinessInputBudgets$' -count=1` | 2.4s | 1 | 预期 FAIL：评论/服务器命名预算常量与共享评论尺寸谓词尚不存在 |
| 差异/入口定向测试 | `go test ./internal/activity ./internal/httpapi -run 'Test(MarkdownDelta&#124;MarkdownDiff&#124;AddedMarkdown&#124;MarkdownDeltaCallersEnforceBusinessInputBudgets)' -count=1` | 6.9s；Go 分别报告 0.092s / 1.193s | 0 | PASS：小编辑精确、8 MiB 完全重写保守、2051-entry frontier 与三入口限制通过 |
| 后端全仓 / Vet / 构建 | `go test ./...`；`go vet ./...`；`go build ./...` | 12.8s | 0 | PASS：全仓最慢 HTTP 0.592s |

8 MiB 行为用例在测试进程内构造两个完全不同的字符串，不通过 HTTP 或写入数据库。它直接覆盖审计描述的最坏输入形态；修复后该调用在 Go 报告的 0.00s 精度内完成，而精确小差异结果保持不变。

## 2026-08-21：SEC-038 公共 GET 的 AI 费用边界

环境：Go 1.26.4；bundled Node 24.19.0；没有配置或调用真实 AI 供应商，验证的是任务创建前的 HTTP/领域边界。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi -run 'Test(ResolveContentLocaleChinesePairBeforeEnglish&#124;CatalogContentReadCannotEnqueueAIWork&#124;ContentTranslationConcurrencyKeyTracksSourceAndActor)' -count=1` | 2.0s | 1 | 预期 FAIL：并发键仍要求可选 quota 标志，公共 GET 仍有 enqueue/publish/无限额度路径 |
| 本地化/副作用定向测试 | 同上，修复后 `-v` | 6.8s；Go 报告 1.165s | 0 | PASS：支持语言也标记显式请求；GET 段无三个 AI 写标记；源修订/actor 都进入任务键 |
| HTTP 包全量 | `go test ./internal/httpapi -count=1` | 7.4s；Go 报告 1.653s | 0 | PASS |
| 后端全仓 / Vet / 构建 | `go test ./...`；`go vet ./...`；`go build ./...` | 12.5s | 0 | PASS：全仓最慢 HTTP 0.481s |
| 前端消费者类型/Lint | `pnpm exec tsc --noEmit`；ESLint `editor-api.ts`、`editor-types.ts`、`content-translation-control.tsx` | 6.4s | 0 | PASS：既有 request_required 显式请求控件可直接消费新语义 |

本项没有把“未访问真实供应商”记录成供应商集成通过。安全目标是在任何供应商调用之前保证公共读取不可创建任务；该调用图边界和显式配额语义由直接源契约及纯行为测试覆盖。

## 2026-08-21：SEC-010 嵌入图标解码预算

环境：Go 1.26.4；测试只在内存构造 PNG/header，不创建导入任务、不写数据库或 OSS。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi -run 'TestEmbeddedIcon(DecodeBudgetsRejectHugeHeaderBeforeFullDecode&#124;BuildConcurrencyIsProcessWideAndBounded)' -count=1` | 2.0s | 1 | 预期 FAIL：预解码像素/边长常量和全进程构建槽尚不存在 |
| 嵌入图标定向回归 | `go test ./internal/httpapi -run 'Test(EmbeddedIcon&#124;DecodeEmbeddedIcon&#124;RenderEmbeddedIcon&#124;DecodeIRR&#124;DecodeIconRenderer&#124;BuildIRR)' -count=1` | 6.8s；Go 报告 1.199s | 0 | PASS：伪造 2.5B 像素 IHDR 拒绝、第三槽超时、合法 PNG/三尺寸/两导入格式均通过 |
| 后端全仓 / Vet / 构建 | `go test ./...`；`go vet ./...`；`go build ./...` | 13.3s | 0 | PASS：全仓最慢 app 1.205s，HTTP 0.567s |

巨幅用例只修改合法 1×1 PNG 的 IHDR 与 CRC；修复路径在 `DecodeConfig` 后拒绝，不会请求 50,000×50,000 像素缓冲区。并发用例实际占满两个进程级槽，第三个带 20ms context 的申请按 deadline 失败后释放全部令牌。

## 2026-08-21：SEC-017 / SEC-018 Presence 与 SSE 预算

环境：Go 1.26.4；bundled Node 24.19.0。Redis lease 的本地失败安全实现做直接并发/状态行为测试；当前环境没有独立可清空 Redis 测试 namespace，因此没有把 Lua 路径记录为真实 Redis 集成通过。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi ./internal/querycache -run 'Test(AnonymousPresenceIdentity&#124;LocalPresenceCardinality&#124;RealtimeSSE&#124;RealtimeLease)' -count=1` | 两批各约 2.0s | 1 | 预期 FAIL：服务端匿名身份、Presence 两级 cap、实时 lease API 和 SSE 分层常量均不存在 |
| Presence 定向测试 | `go test ./internal/httpapi ./internal/querycache -run 'Test(AnonymousPresenceIdentity&#124;LocalPresence&#124;MapPublicOnline&#124;PresenceSnapshot)' -count=1 -v` | 7.2s；Go 1.144s / 0.557s | 0 | PASS：HMAC 身份、secret 绑定、4,096 cap、过期清理与隐私状态通过 |
| SSE/lease 定向测试 | `go test ./internal/httpapi ./internal/querycache -run 'Test(RealtimeSSE&#124;RealtimeLease&#124;AnonymousPresenceIdentity&#124;LocalPresenceCardinality)' -count=1 -v` | 7.5s；Go 1.266s / 0.677s | 0 | PASS：session/user/total 拒绝、刷新、释放复用和有限寿命源契约通过 |
| 前端 Presence 消费者 | `pnpm exec tsc --noEmit`；ESLint `site-shell.tsx` | 6.8s | 0 | PASS：浏览器保存服务端 token，60 秒节奏不变 |
| 相关包 / 后端全仓 / Vet / 构建 | 包全量后 `go test ./...`、`go vet ./...`、`go build ./...` | 16.4s | 0 | PASS：HTTP/querycache 包 0.529s/0.198s；全仓最慢 app 1.519s |

共享 Redis Lua 与本地实现使用同样的三层计数和 TTL 语义，但本轮只对本地实现执行了直接行为测试；最终 Redis 故障/多实例门禁仍必须使用隔离 namespace 验证 admission、续租、崩溃 TTL 回收与原子并发。当前实现即使 Redis 故障也不会回退为无界连接或 Presence map。

## 2026-08-21：SEC-023 浏览页面事实闭集

环境：Go 1.26.4；真实 PostgreSQL 用例只创建 session-local 临时表并由事务整体回滚；没有修改永久 Schema 或业务数据。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi -run 'Test(MetricPageKey&#124;MetricViewPath)' -count=1` | 约 2s | 1 | 预期 FAIL：闭集解析、非法页错误和数据库归属解析尚不存在 |
| 闭集/预算定向测试 | `go test ./internal/httpapi -run 'Test(MetricPageKeyUsesClosedServerRegistry&#124;MetricViewPathHasSourceAndDailyViewerBudgets)' -count=1 -v` | 7.3s；Go 报告 1.182s | 0 | PASS：只接受 detail/合法 version 形态，且写路径包含来源预算、24h 去重与权威解析 |
| PostgreSQL 版本归属 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestMetricVersionPageMustBelongToTargetResource$' -count=1 -v` | 3.1s；Go 报告 0.550s | 0 | PASS：当前资源 active 版本接受，另一资源版本拒绝；测试事务回滚 |
| 后端全仓 / Vet / 构建 | `go test ./...`；`go vet ./...`；`go build ./...` | 13.9s | 0 | PASS：全仓最慢 HTTP 0.615s |
| 后端差异空白 | `git diff --check` | 包含于同一批次 | 0 | PASS；仅既有 CRLF 转换提示 |

本项不修改 Schema，权威版本保持 generation 90。24 小时去重和限流使用既有 Redis 共享实现及有界本地回退；真实 PostgreSQL 测试直接覆盖最关键的跨资源版本归属，不把临时表测试描述成空库初始化。

## 2026-08-21：SEC-043 草稿存量与写入预算

环境：Go 1.26.4；真实 PostgreSQL 测试使用 session-local 临时 `user_drafts`，双事务锁测试不写业务表；所有测试回滚。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前确认 | 当前源码/Schema 复核 | — | — | CONFIRMED：仅对象 JSON 校验和通用请求上限；无单条业务字节、条数、总量、写速率或同用户事务锁 |
| 单元与 Schema 定向 | `go test ./internal/httpapi ./internal/database -run 'Test(DraftPayloadAndPerUserQuotaBudgets&#124;DraftWritePathHasRateAndAtomicStockBudgets&#124;DraftSchemaHasIndependentPayloadBudget)' -count=1 -v` | 10.8s 批次中的前段；Go 1.171s/0.090s | 0 | PASS：512 KiB、条数/字节差额、专用 rate budget、锁和 generation 91 CHECK 受守护 |
| PostgreSQL 存量/并发 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestDraftQuota(CountsAndReclaimsAtomically&#124;AdvisoryLockSerializesOneUser)$' -count=1 -v` | 3.3s 首次发现参数类型红测；修正后 8.6s，最终独立批次 Go 报告 1.587s | 0 | PASS：过期回收、第三 active/history 拒绝、替换允许；第二事务不能取得同用户 lock，提交后可复用 |
| Database/HTTP 包全量 | `go test ./internal/database ./internal/httpapi -count=1` | 7.5s | 0 | PASS：database 0.097s、HTTP 1.549s |
| 后端全仓 / Vet / 构建 | `go test ./...`；`go vet ./...`；`go build ./...` | 15.0s | 0 | PASS：全仓测试最慢 app 1.531s；HTTP 1.352s |
| 后端差异空白 | `git diff --check` | 包含于全仓批次 | 0 | PASS；仅既有 CRLF 转换提示 |

首次 PostgreSQL 红测暴露 `$1::text` 无法编码 int64，修正为 `$1::bigint::text` 后两项直接行为测试通过。完整 generation 91 空库安装仍受既有远程账号无 `CREATEDB`、维护库被 `pg_hba` 拒绝的环境门禁；本轮没有把临时表建表描述成空库初始化，也没有执行永久 DDL。

## 2026-08-21：SEC-014 Minecraft Configuration 探测预算

环境：Go 1.26.4；纯内存协议测试，不连接任何公网或用户服务器，不写数据库。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/serverprobe -run 'TestConfiguration(RegistryCountCannotPreallocateBeyondPayload&#124;ProbeConcurrencyIsProcessWide)&#124;TestFabricRegistryAndEvidenceBudgets' -count=1` | 1.3s | 1 | 预期 FAIL：进程槽、共享 identifier/namespace 常量与 admission 函数不存在 |
| 预算初次回归 | `go test ./internal/serverprobe -run 'Test(Configuration&#124;ParseFabric&#124;ParseConfiguration&#124;FabricRegistry)' -count=1 -v` | 10.8s；Go 报告 9.152s | 0 | PASS：百万 count 分配、4 槽 admission 和既有 Frozen/Dynamic/Fabric 合法协议通过 |
| Serverprobe 全包 | `go test ./internal/serverprobe -count=1 -v` | 10.6s；Go 报告 8.916s | 0 | PASS：新增跨包共享预算、零字节 NBT list、立即容量拒绝以及全部既有探测/地址/Forge 用例通过 |
| 后端全仓 / Vet / 构建 | `go test ./...`；`go vet ./...`；`go build ./...` | 18.6s | 0 | PASS：serverprobe 2.942s；HTTP 1.604s；全仓通过 |
| 后端差异空白 | `git diff --check` | 包含于全仓批次 | 0 | PASS；仅既有 CRLF 转换提示 |

畸形 Frozen/Dynamic 用例实际编码注册表名和 count=1,000,000，但不提供条目；`testing.Benchmark` 逐次调用解析器并断言 `<1 MiB allocated/op`，修复前路径会仅凭 count 分配约 16 MiB backing array。该结果是本机测试进程内分配证据，不是生产 RSS 或公网负载实测。

## 2026-08-21：PERF-004 反滥用成功记录固定并发

环境：Go 1.26.4；故障用例使用已关闭 pgxpool 和 `BeforeConnect` 等待 context 的本地 pool 配置，不连接真实数据库、不写业务数据。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/antiabuse ./internal/httpapi -run 'Test(SuccessQueue&#124;ProtectedMutationUsesBoundedSuccessQueue)' -count=1` | 7.1s | 1 | 预期 FAIL：成功队列/指标 API 不存在，middleware 仍含 `go s.antiAbuse.RecordSuccess` |
| 队列/生命周期定向 | `go test ./internal/antiabuse ./internal/httpapi ./internal/app -run 'Test(SuccessQueue&#124;SuccessWorkers&#124;ProtectedMutationUsesBoundedSuccessQueue&#124;Availability)' -count=1 -v` | 3.6s | 0 | PASS：容量、20k snapshot、100 条关闭池失败排空、middleware/availability 回归通过 |
| 慢数据库截止 | `go test ./internal/antiabuse -run 'TestSuccess' -count=1 -v` | 6.3s；Go 报告 3.934s | 0 | PASS：BeforeConnect 等待 context；关闭 3.00s 返回，未处理项 dropped，队列深度 0，processed+dropped=queued |
| 相关包全量 | `go test ./internal/antiabuse ./internal/httpapi ./internal/app -count=1` | 3.2s | 0 | PASS |
| 后端全仓 / Vet / 构建 | `go test ./...`；`go vet ./...`；`go build ./...` | 17.4s | 0 | PASS：antiabuse 3.116s、HTTP 1.620s、app 1.290s |
| 后端差异空白 | `git diff --check` | 包含于全仓批次 | 0 | PASS；仅既有 CRLF 转换提示 |

慢数据库测试没有把网络失败当成数据库集成：pool 的 `BeforeConnect` 明确阻塞到 worker context 取消，用来验证 2 秒单项和 3 秒 shutdown 边界；另一个已关闭 pool 用例验证快速失败时 100 个已入队项可完整处理并计 failed。没有声称反滥用记录在队列满时可靠持久化；丢弃策略通过管理员指标显式可见。

## 2026-08-21：PERF-061 / BUG-120 草稿分页与完成权威

环境：Go 1.26.4、bundled Node 24.19.0、Next.js 16.2.11。真实 PostgreSQL 测试只创建 session-local 临时表并由事务回滚；没有修改永久 Schema 或业务数据。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi ./internal/database -run 'Test(CompleteUserDraft&#124;UserDraftListRequest&#124;DraftSchemaRequires)' -count=1` | 3.5s | 1 | 预期 FAIL：类别 cursor/解析器不存在，完成仍允许无目标/双目标，Schema 无互斥权威约束 |
| 单元与 Schema 定向 | 同上修复后；另含既有 Draft payload/quota 用例 | 7.8s 批次；最终 Go 0.144s / 0.108s | 0 | PASS：类别/页上限/cursor scope、唯一权威、payload/quota 与 generation 92 Schema 守护通过 |
| PostgreSQL keyset / 权威归属 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'TestDraft(PagesUse&#124;CompletionAuthorityComes)' -count=1 -v` | 3.6s；Go 1.085s | 0 | PASS：同时间戳按 ID 分两页无重复；active/completed、用户隔离；本人审核单/服务器接受、跨用户拒绝 |
| 前端定向 | `pnpm exec tsc --noEmit`；ESLint 13 个草稿 API/消费者/locale 文件 | 9.8s / 7.0s | 0 | PASS：所有完成调用删除 `reviewStatus`，首屏双类别与 cursor 增量消费通过类型和规则检查 |
| 后端全仓 / Vet | `go test ./...`；`go vet ./...` | 14.5s 并行批次 | 0 | PASS：HTTP 1.473s、database 0.976s、antiabuse 4.167s |
| 前端测试 / Lint / Typecheck | `pnpm test`；`pnpm lint`；`pnpm typecheck` | 25.7s 并行批次 | 0 | PASS：8/8 Node 测试，ESLint 与 TypeScript 全仓通过 |
| 生产构建 | `go build ./...`；`pnpm build` | 后端 6.0s；前端 25.4s | 0 | PASS：Next 编译、类型检查及 59 个静态页面生成通过 |
| 双仓差异空白 | `git diff --check` | 0.8s | 0 | PASS；仅既有 LF/CRLF 转换提示 |

cursor 的真实 PostgreSQL 用例使用三个相同 `statusAt` 的 active 行强制执行 ID tie-break，并逐页断言 3/2 后 1；completed 同时验证 pending/approved 的服务器派生展示。测试不是完整 generation 92 空库安装；既有账号仍无建库权限，本轮没有将临时表行为冒充空库门禁。

## 2026-08-21：SEC-009 导入包不可变来源

环境：Go 1.26.4。真实 PostgreSQL 用例只创建 session-local 临时 package/job 表并回滚；相关 catalog resource 集成同样在事务内准备夹具，没有执行永久 DDL。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi ./internal/database -run 'TestCatalogImport' -count=1` | 约 3s | 1 | 预期 FAIL：不可变 package helper/verified 标记不存在，Schema 仍允许全局 SHA 冲突覆盖来源 |
| 单元、Schema 与顺序守护 | `go test ./internal/httpapi ./internal/database -run 'TestCatalogImport(CreationDoesNotOverwritePackagesByDeclaredHash&#124;SameSHAKeepsDistinctUserSources&#124;PackageSourceIsImmutableAndNotGloballyDeduplicated)$' -count=1` | 7.5s；Go 1.183s/0.089s | 0 | PASS：两个创建路径无 SHA 覆盖、同文件幂等、来源锁、实际哈希后 verified 和 generation 93 DDL 均受守护 |
| PostgreSQL 跨用户来源 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCatalogImportSameSHAKeepsDistinctUserSources$' -count=1 -v` | 约 2.4s；Go 0.795s | 0 | PASS：两用户不同文件声明相同 SHA 得到两条 package；错误 file/SHA 三元组不能标记 verified；活动 job 锁住删除 |
| 相关持久化集成 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'Test(CatalogImportSameSHAKeepsDistinctUserSources&#124;PersistCatalogResourcesReusesCanonicalIdentityIntegration)$' -count=1 -v` | 约 3.8s；Go 2.245s | 0 | PASS：来源隔离与既有 canonical resource 身份复用同时通过，夹具已迁移到 NOT NULL 来源归属 |
| 后端全仓 / Vet / 构建 | `go test ./...`；`go vet ./...`；`go build ./...` | 16.8s（代码完成批次） | 0 | PASS：database 0.887s、HTTP 1.333s、antiabuse 3.958s、app 1.518s |
| 来源写入面与差异空白 | `rg 'insert into catalog_import_packages' internal -g '*.go'`；`git diff --check` | 包含于最终批次 | 0 | PASS：生产写入只经不可变 helper，其他命中均为已迁移测试夹具；仅既有 CRLF 转换提示 |

Worker 顺序测试同时守护 ZIP 路径的 streamed hash compare 在 verified 写之前，以及嵌入图标下载函数必须先完成内部 hash compare 才返回并标记。这里没有把客户端 OSS 元数据摘要当成实际内容证明。完整 generation 93 空库初始化仍受既有远程账号无 `CREATEDB`、维护库被 `pg_hba` 拒绝的环境门禁；临时表结果不冒充该门禁。

## 2026-08-21：BUG-019 Mod 导入可靠重入队

环境：Go 1.26.4。数据库用例在一条保留连接上建立 session-local 临时 job/outbox 表；约束故障、状态变更与事件均局限于该 Session，无永久 DDL 或业务写入。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi -run 'Test(ModExportRetryAndRecoveryCreateAtomicOutboxAttempts&#124;DuplicateModExportDeliveryIsAcknowledged)$' -count=1` | 2.2s | 1 | 预期 FAIL：事务 retry/recovery helper 与重复投递结果函数均不存在 |
| 入口与包定向 | `go test ./internal/httpapi -run 'Test(ModExportAttemptEntrypointsUseTransactionalOutbox&#124;ModExportRetryAndRecoveryCreateAtomicOutboxAttempts&#124;DuplicateModExportDeliveryIsAcknowledged)$' -count=1` | 7.2s；Go 1.238s | 0 | PASS：四类生产入口统一 helper、重复 lease 消息 ACK；未启用集成开关时数据库用例明确 skip |
| PostgreSQL 原子重入 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'Test(ModExportRetryAndRecoveryCreateAtomicOutboxAttempts&#124;DuplicateModExportDeliveryIsAcknowledged)$' -count=1 -v` | 8.6s；Go 2.653s | 0 | PASS：retry/recovery 各一条新事件、重复操作零新增、新鲜租约不恢复；注入 Outbox CHECK 失败后 job 保持 failed |
| HTTP / Queue 包 | `go test ./internal/httpapi ./internal/queue -count=1` | 3.1s | 0 | PASS：HTTP 0.545s、queue 0.108s |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟 12.7s | 0 | PASS：全仓测试 12.5s，HTTP 1.077s、queue 0.431s、antiabuse 3.147s；Vet 9.7s；Build 10.8s |
| 任务写入面 / 差异空白 | `rg 'insert into nats_outbox'`（四个 Mod 导入入口）；`git diff --check` | 包含于最终批次 | 0 | PASS：生产入口均复用 `queue.EnqueueTx`，不再手写任务事件；仅既有 CRLF 转换提示 |

测试将 failed job 与 retry 事件提交后再次重试，证明 queued 行不会重复生成尝试；将 stale/fresh 租约同时恢复，证明只命中过期行；最后让 Outbox 表拒绝特定 aggregate，证明 event 失败不会留下 queued 状态。重复消费测试不把任意 Worker 错误吞掉，只把权威 CAS 已失效的 `errModExportLeaseLost` 当作幂等完成。未运行 JetStream 网络故障注入，本项复用已存在的 Outbox dispatcher 测试；全局可靠队列门禁仍在最终目标内。

## 2026-08-21：LEGACY-006 删除 Mod 导入双任务路径

环境：Go 1.26.4。离线 fallback 测试构造 `NATS Enabled=false` 的内存 queue client，不打开网络；真实 PostgreSQL 重入测试继续使用 session-local 临时表。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi ./internal/queue -run 'Test(ModExportWorkerHasNoCoreNATSOrGoroutineTaskPath&#124;TaskSubscriptionRemainsAvailableToPostgresFallbackWithoutNATS)$' -count=1` | 7.2s | 1 | 预期 FAIL：Worker 仍读取 Outbox 开关并保留 Core publish/启动扫描/goroutine；离线本地注册行为本身已通过 |
| 路径与本地 fallback | `go test ./internal/httpapi ./internal/queue ./internal/app -run 'Test(ModExportWorkerHasNoCoreNATSOrGoroutineTaskPath&#124;TaskSubscriptionRemainsAvailableToPostgresFallbackWithoutNATS&#124;ModExportAttemptEntrypointsUseTransactionalOutbox&#124;DuplicateModExportDeliveryIsAcknowledged&#124;Availability)$' -count=1` | 10.5s | 0 | PASS：Mod 生产文件无旧符号；应用强制 dispatcher；离线订阅注册后可由 `HandleLocally` 精确执行 |
| PostgreSQL 重入回归 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'Test(ModExportRetryAndRecoveryCreateAtomicOutboxAttempts&#124;ModExportWorkerHasNoCoreNATSOrGoroutineTaskPath&#124;DuplicateModExportDeliveryIsAcknowledged)$' -count=1 -v` | 4.3s；Go 1.551s | 0 | PASS：删除旧唤醒路径后 retry/recovery 事件、回滚和重复 ACK 仍通过 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟 9.6s | 0 | PASS：全仓测试 9.4s，HTTP 0.821s、queue 0.374s、serverprobe 4.329s；Vet 5.5s；Build 6.5s |
| 旧符号 / 差异空白 | `rg 'dispatchModExportJob&#124;OutboxEnabled&#124;PublishTask&#124;go func\(id string\)'`（Mod 导入生产文件）；`git diff --check` | 包含于最终批次 | 0 | PASS：Mod 导入无双执行入口；仅既有 CRLF 转换提示 |

本项没有声称 NATS 故障实测等同于 JetStream 故障注入：离线用例证明订阅注册与同步本地 handler 边界，已有 Outbox dispatcher 负责数据库 claim/失败回退，最终目标仍要求在可用 JetStream 环境执行 ACK、重投和断连门禁。其他任务族仍使用旧开关或 Core NATS 的事实保留在各自 Finding 中。

## 2026-08-21：LEGACY-019 内容翻译统一 AI Outbox

环境：Go 1.26.4。PostgreSQL 用例在保留连接的 session-local `ai_tasks/nats_outbox` 表中执行，失败约束和数据随 Session 释放；未写永久表。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi -run 'Test(ContentTranslationUsesSharedTransactionalAITaskOutbox&#124;AITaskAndOutboxCommitOrRollbackTogether)$' -count=1` | 2.3s | 1 | 预期 FAIL：共享 `enqueueAITaskTx` 不存在，内容任务仍在事务提交后发布 |
| 定向与安全回归 | `go test ./internal/httpapi -run 'Test(ContentTranslationUsesSharedTransactionalAITaskOutbox&#124;AITaskAndOutboxCommitOrRollbackTogether&#124;CatalogContentReadCannotEnqueueAIWork&#124;ContentTranslationConcurrencyKeyTracksSourceAndActor)$' -count=1` | 7.1s；Go 1.216s | 0 | PASS：目录/社区/通用入口单一 helper；公开 GET 无副作用；来源 revision、actor 和 locale 继续进入去重键 |
| PostgreSQL 原子提交 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'Test(ContentTranslationUsesSharedTransactionalAITaskOutbox&#124;AITaskAndOutboxCommitOrRollbackTogether)$' -count=1 -v` | 3.3s；Go 0.815s | 0 | PASS：task 与带 trace/payload 的事件共同提交；Outbox CHECK 拒绝时两者均为零 |
| HTTP / Queue / App 包 | `go test ./internal/httpapi ./internal/queue ./internal/app -count=1` | 6.8s | 0 | PASS：HTTP 0.644s、queue 0.117s、app 1.396s |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟 8.0s | 0 | PASS：全仓测试 7.8s，HTTP 0.888s、queue 0.330s、antiabuse 3.140s；Vet 5.0s；Build 6.1s |
| 旧入口 / 差异空白 | `rg -e publishContentTranslationTask -e OutboxEnabled -e PublishTask`（三类 AI 入口）；`git diff --check` | 包含于最终批次 | 0 | PASS：无命中；仅既有 CRLF 转换提示 |

约束故障用例先在同一事务插入 AI 行，再调用统一 helper；事件失败后显式回滚并从同一 Session 断言 task/event 都不存在。它验证原子边界，不声称覆盖真实供应商调用或 JetStream 网络；后两者继续由 AI Worker/最终可靠队列门禁覆盖。该批次先只关闭 LEGACY-019；紧随其后的 LEGACY-008 批次再迁移通知翻译。

## 2026-08-21：LEGACY-008 通知翻译可靠入队

环境：Go 1.26.4。继续使用 LEGACY-019 的 session-local AI/Outbox 夹具，并增加 notification event/trace 断言；不连接 NATS 或供应商。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi -run '^TestNotificationTranslationUsesSharedTransactionalAITaskOutbox$' -count=1` | 7.1s | 1 | 预期 FAIL：通知 Handler 仍包含 `PublishTask`、NATS failure 回写与 published 日志 |
| 定向入口 | `go test ./internal/httpapi -run 'Test(NotificationTranslationUsesSharedTransactionalAITaskOutbox&#124;ContentTranslationUsesSharedTransactionalAITaskOutbox&#124;AITaskAndOutboxCommitOrRollbackTogether)$' -count=1` | 7.3s；Go 1.272s | 0 | PASS：通知与内容使用同一 helper；无 direct publish/基础设施业务失败语义 |
| PostgreSQL 两类事件 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'Test(NotificationTranslationUsesSharedTransactionalAITaskOutbox&#124;AITaskAndOutboxCommitOrRollbackTogether)$' -count=1 -v` | 8.4s；Go 2.454s | 0 | PASS：content 与 notification 事件分别携带正确 task UID/trace；故障 aggregate 仍整笔回滚 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟 12.4s | 0 | PASS：全仓测试 12.2s，HTTP 1.016s、queue 0.398s、antiabuse 3.157s；Vet 9.1s；Build 10.0s |
| 旧入口 / 差异空白 | 通知函数范围检查 `PublishTask`、`NATS unavailable`、`published to NATS`；`git diff --check` | 包含于最终批次 | 0 | PASS：三类旧语义均不存在；仅既有 CRLF 转换提示 |

通知用例没有把“事件已提交”描述为“已到 NATS”：日志明确为 reliable outbox，客户端继续看到业务任务 queued。真实 JetStream ACK/断连门禁仍待统一环境执行；本项只关闭数据库 commit 后易失直发这一唯一 Finding，不提前关闭 BUG-044 或 OPS-019。

## 2026-08-21：OPS-019 AI 任务恢复扫描

环境：Go 1.26.4；真实 PostgreSQL 行为用 session-local 表，规模计划用临时表及临时索引；没有改变远程永久 Schema/业务数据。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi ./internal/database -run 'Test(AITaskRecoveryRequeuesOrphansAndStaleRunsOnce&#124;AITaskRecoveryHasMatchingIndexes)$' -count=1` | 3.5s | 1 | 预期 FAIL：恢复/claim helper 不存在，generation 93 无两个匹配索引 |
| 定向行为与 Schema | `go test ./internal/httpapi ./internal/database -run 'Test(AITaskRecoveryRequeuesOrphansAndStaleRunsOnce&#124;AITaskRecoveryPlanUsesBoundedIndexes&#124;AITaskRecoveryHasMatchingIndexes)$' -count=1` | 7.1s；Go 1.202s/0.091s | 0 | PASS：orphan/stale/重复/CAS 和 generation 94 DDL 契约通过；未开集成时 PG 用例 skip |
| PostgreSQL 恢复原子性 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'Test(AITaskRecoveryRequeuesOrphansAndStaleRunsOnce&#124;AITaskAndOutboxCommitOrRollbackTogether)$' -count=1 -v` | 4.9s；Go 2.436s | 0 | PASS：queued/retrying orphan 与 stale running 精确三项、第二次零项；fresh/covered 不动；Outbox 失败回滚；claim 1/0 |
| PostgreSQL 规模计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestAITaskRecoveryPlanUsesBoundedIndexes$' -count=1 -v` | 9.5s；Go 3.546s | 0 | PASS：100k completed + 500 active、100k 无关 event 下，EXPLAIN ANALYZE 同时命中 active recovery 与 aggregate 索引 |
| 相关包 | `go test ./internal/httpapi ./internal/database ./internal/queue ./internal/app -count=1` | 6.8s | 0 | PASS：HTTP 0.579s、database 0.160s、queue 0.122s、app 1.207s |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟 12.0s | 0 | PASS：全仓测试 11.8s，HTTP 0.988s、app 1.366s、antiabuse 3.167s；Vet 8.8s；Build 10.1s |
| 差异空白 | `git diff --check` | 包含于最终批次 | 0 | PASS；仅既有 CRLF 转换提示 |

恢复用例给 covered queued 保留既有事件、给 stale running 保留旧事件：前者不重复，后者产生新的恢复 attempt；这区分了“缺失分发事实”和“旧执行已超时”。dead event 不由扫描自动复活，避免错误任务每分钟重试。generation 94 的完整空库安装仍受既有 CREATEDB/pg_hba 外部门禁；规模临时表与静态 DDL 测试没有冒充该门禁。

## 2026-08-21：LEGACY-013 蓝图任务单一 Outbox 入口

环境：Go 1.26.4；真实 PostgreSQL 用 session-local 临时 `blueprint_jobs` / `nats_outbox`，没有写永久表或连接 NATS。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi -run '^TestBlueprintJobsUseOneTransactionalOutboxPath$' -count=1` | 2.2s；Go 2.0s | 1 | 预期 FAIL：共享 tx helper 不存在，上传仍复制 INSERT/直发 |
| PostgreSQL 原子性 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestBlueprintJobAndOutboxCommitOrRollbackTogether$' -count=1 -v` | 8.0s；Go 2.084s | 0 | PASS：正常提交为 1 job/1 event 且 payload ID 一致；第二事件 CHECK 故障后 job 为 0 |
| 蓝图与分发相关包 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^(TestBlueprintJobsUseOneTransactionalOutboxPath&#124;TestBlueprintJobAndOutboxCommitOrRollbackTogether&#124;TestBlueprint)' -count=1`；`go test ./internal/queue ./internal/app -count=1` | 8.3s | 0 | PASS：HTTP 1.053s、queue 0.089s、app 0.127s |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟 8.2s | 0 | PASS：全仓 8.0s，HTTP 0.980s、app 0.221s、antiabuse 3.132s；Vet 5.1s；Build 6.2s |
| 台账精确性 / 差异空白 | `go run ./tools/remediation/verify_findings`；`git diff --check` | 1.3s | 1 / 0 | 449 唯一 ID、分类计数精确；45 CLOSED / 404 OPEN，High 89 / Medium 154 / Low 47 / unresolved 159；总体验证按设计因剩余 404 项退出 1。diff 无空白错误，仅既有 CRLF 提示 |

源码契约要求 `blueprint_handlers.go` 完全不存在 `OutboxEnabled`/`PublishTask`，且 `insert into blueprint_jobs` 只出现一次；两个调用入口都必须在 commit 前调用 helper。它不关闭 OPS-014 的执行租约或 BUG-078 的 retry 主体状态原子性，也不把 session-local 原子测试描述为空库 generation 安装。

## 2026-08-21：OPS-009 Outbox 状态与指标一致性

环境：Go 1.26.4；真实 PostgreSQL 使用 MaxConns=1 的 session-local 临时 Outbox/死信表，保证 dispatcher 的 pool Exec 命中同一临时 Schema；没有连接 NATS 或写永久表。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/queue -run '^TestOutboxStateTransitionsDoNotIgnorePersistenceResults$' -count=1` | 1.6s；Go 1.397s | 1 | 预期 build FAIL：`fail` 无返回值，五个故障断言无法观察状态持久化错误 |
| PostgreSQL 故障状态机 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/queue -run '^TestOutbox(StateTransitionsDoNotIgnorePersistenceResults&#124;FailureMetricsRequirePersistedTransitions&#124;StaleLeaseRecoveryErrorsAreReturned&#124;PublishedMetricRequiresPersistedTransition)$' -count=1 -v` | 5.0s；Go 2.862s | 0 | PASS：正常 retry/dead；重复 owner、retry CHECK、dead-letter CHECK、stale reset CHECK、published CHECK 均返回错误且指标不虚增 |
| 相关包 | `go test ./internal/queue ./internal/app ./internal/httpapi -count=1` | 10.4s | 0 | PASS：queue 0.082s、app 0.131s、HTTP 1.572s |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟 9.2s | 0 | PASS：全仓 9.0s，queue 0.383s、HTTP 0.996s、antiabuse 3.168s；Vet 5.4s；Build 6.3s |
| Race 环境探测 | `go test -race ./internal/queue -count=1`；`CGO_ENABLED=1 go test -race ./internal/queue -count=1` | 0.7s / 2.3s | 1 / 1 | BLOCKED：默认 CGO disabled；显式开启后本机无 `gcc`。未把该环境阻塞记录成 PASS，最终 Race 总门禁仍待具备 C toolchain 的环境 |

四个真实 PG 测试分别建立独立临时表；故障 CHECK 使目标 SQL 真正由 PostgreSQL 拒绝，而不是 mock 返回预设值。dead CTE 的 INSERT 失败后 outbox 仍为 publishing，证明状态与死信不会半提交。TEST-026 的多 dispatcher、进程重启与真实 JetStream 故障仍独立开放。

## 2026-08-21：LEGACY-009 私聊本地化可靠邮件

环境：Go 1.26.4；原子性使用真实 PostgreSQL session-local message/conversation/outbox 表；不发送真实邮件、不连接 NATS、不写永久业务表。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/httpapi -run '^TestDirectMessageEmailUsesLocalizedTransactionalOutbox$' -count=1` | 2.2s；Go 2.051s | 1 | 预期 build FAIL：事务消息 helper 不存在，旧 Handler/Worker 仍不满足源码契约 |
| 源码与 locale 模板 | `go test ./internal/httpapi -run '^(TestDirectMessageEmailUsesLocalizedTransactionalOutbox&#124;TestDirectMessageEmailTemplateIsLocalized)$' -count=1` | 7.5s；Go 1.401s | 0 | PASS：Handler 无固定中文/拆分 Wrapper，通知生产与 Worker 无 Core Publish；en-US/zh-CN 参数渲染正确 |
| PostgreSQL 原子性 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestDirectMessageAndEmailOutboxCommitOrRollbackTogether$' -count=1 -v` | 3.6s；Go 1.132s | 0 | PASS：成功 1 message/1 event、message aggregate/trace/template 精确；event CHECK 失败后 message 0、会话时间不变 |
| 相关门禁 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^Test(DirectMessageEmailUsesLocalizedTransactionalOutbox&#124;DirectMessageEmailTemplateIsLocalized&#124;DirectMessageAndEmailOutboxCommitOrRollbackTogether&#124;DefaultNotificationTemplatesCoverEveryEnabledLocale&#124;NotificationTemplate)' -count=1`；`go test ./internal/queue ./internal/app -count=1` | 6.8s | 0 | PASS：HTTP 1.065s、queue 1.827s、app 0.122s |
| 既有整库集成探测 | 带 `MCMODS_RUN_DB_INTEGRATION=1` 误扩到 `TestUserBlockRelationshipAndOwnedCommentTargetsIntegration` | 7.2s | 1 | BLOCKED：远程测试库仍为 generation 85，代码要求 94；未重置/修改远程库，随后用 session-local目标集重新验证通过 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟 7.9s | 0 | PASS：全仓 7.7s，HTTP 0.901s、queue 0.349s、antiabuse 3.135s；Vet 4.9s；Build 5.8s |

Outbox 故障通过 payload 模板参数 CHECK 由数据库真实拒绝；测试不仅断言消息回滚，也保存首笔会话时间并证明失败事务没有推进它。邮件内容没有实际发送，SMTP/JetStream 网络故障与聊天 IDOR/并发矩阵仍按最终总门禁和 TEST-022 验证。

## 2026-08-21：LEGACY-011 删除裸 task JSON 协议

环境：Go 1.26.4；生产者清点覆盖 `internal/httpapi` 全部非测试 Go 文件；Mod metadata 恢复使用真实 PostgreSQL session-local job/outbox 表，不连接 NATS 或写永久表。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前红测 | `go test ./internal/queue -run '^(TestRawCoreMessageIsRejected&#124;TestTaskProducersUseOnlyEventEnvelopes)$' -count=1` | 1.6s；Go 1.405s | 1 | 预期 build FAIL：`UnwrapEvent` 仍为无 error 双返回，无法表达裸协议拒绝 |
| 严格 Envelope / 生产者清点 | `go test ./internal/queue -run '^Test(EventEnvelopeRoundTrip&#124;RawCoreMessageIsRejected&#124;EventEnvelopeRequiresCompleteVersionOneIdentity&#124;LocalTaskHandlerRejectsBarePayload&#124;TaskProducersUseOnlyEventEnvelopes)$' -count=1` | 3.2s；Go 0.953s | 0 | PASS：完整 v1 往返；裸/7 类缺字段拒绝；Handler 未调用；生产源码和 Client 无 PublishTask |
| PostgreSQL metadata 恢复 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestModMetadataImportRecoveryUsesOnlyTransactionalEnvelopes$' -count=1 -v` | 8.5s；Go 2.262s | 0 | PASS：orphan queued + stale running 恢复 2；recent covered/fresh 不动；二次 0；event CHECK 使 running 状态回滚 |
| 相关包 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/queue ./internal/httpapi ./internal/app -run 'Test(EventEnvelope&#124;RawCore&#124;LocalTask&#124;TaskProducers&#124;TaskSubscription&#124;ModMetadataImportRecovery&#124;BlueprintJobsUseOne&#124;DirectMessage&#124;Outbox)' -count=1` | 5.7s | 0 | PASS：queue 2.592s、HTTP 3.019s、app 0.132s |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟 9.0s | 0 | PASS：全仓 8.8s，HTTP 1.093s、queue 0.485s、antiabuse 3.360s；Vet 5.3s；Build 6.2s |

生产源码清点命令 `rg 'PublishTask' --glob '*.go'` 最终只命中防回归测试字符串；不存在可调用方法或生产调用方。真实 NATS/JetStream 的非法 Envelope dead-letter 行为仍需 OPS-001/TEST-026 环境门禁，本项只证明统一解析/本地 fallback 与生产协议迁移。

## 2026-08-21：BUG-056 / LEGACY-012 NATS 设置失败安全热切换

环境：Go 1.26.4；前端为 bundled Node 24.19.0。运行时测试使用进程内真实 TCP 监听器实现 NATS INFO/CONNECT/PING/SUB 协议，并可在 SUB 时发送权限错误及断连；它验证 Core NATS 连接/订阅切换，不冒充完整 nats-server 或 JetStream。持久设置测试使用远程 PostgreSQL 的单连接 session-local 临时表，无永久 DDL/业务写入。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 完整 DTO / 单一持久语义 | `go test ./internal/database ./internal/httpapi ./internal/queue -run 'Test(PersistedNATSConfigIsCompleteAuthority&#124;NATS&#124;ReconfigureStagesAllSubscriptionsAndKeepsLastGoodRuntime)' -count=1` | 7.4s | 0 | PASS：遗漏 Outbox/JetStream 字段拒绝；秘密保留/清除互斥；响应字段/秒单位完整；false/空秘密/duration JSON 往返 |
| 候选运行时故障与恢复 | `go test ./internal/queue -run 'Test(ReconfigureStagesAllSubscriptionsAndKeepsLastGoodRuntime&#124;BroadcastSubscriptionRegisteredWhileRealtimeIsDisabled)' -count=10` | 3.3s；Go 1.086s | 0 | PASS：SUB 权限拒绝时持久回调未调用且旧连接保持；注入保存错误后候选关闭；成功时任务+广播订阅先到位再保存/切换；关闭时注册的广播后来恢复 |
| PostgreSQL 加密设置权威 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestLoadNATSConfigKeepsStoredFalseAndClearedSecretsIntegration$' -count=1` | 3.6s；Go 1.230s | 0 | PASS：fallback 为 true 且含环境密码/Token 时，存储的 false、空凭据、9s ACK 与 2s publish timeout 均原样恢复 |
| 前端请求契约 | bundled Node `--test` 四个纯模块；`tsc --noEmit`；定向 ESLint | 10.1s + 8.2s | 0 | PASS：9/9 tests；payload 总含 Outbox/Realtime/JetStream 与 clear 字段；TypeScript 和受影响文件 Lint 通过 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 23.4s 组合墙钟 | 0 | PASS：全仓所有包通过；Vet/Build 无输出且退出 0 |
| 前端全量门禁 | bundled Node：9 tests；`eslint .`；`tsc --noEmit`；`next build` | 47.1s | 0 | PASS：Lint、TypeScript、9/9 tests、Next 16.2.11 生产构建；59 个静态页面生成完成 |
| 台账精确性 / 差异空白 | `go run ./tools/remediation/verify_findings`；`git diff --check` | 1.5s | 1 / 0 | 449 唯一 ID 与分类精确；50 CLOSED / 399 OPEN，High 91 / Medium 157 / Low 47 / unresolved 154；总门禁按设计因剩余 Finding 和最终严重度未齐退出 1。diff 无空白错误，仅既有 CRLF 提示 |

候选订阅测试会在持久回调内读取 `Status` 并确认仍是旧 URL，证明保存发生在 swap 前；订阅拒绝时 callback 次数为零。保存故障时新 TCP 连接回到零，旧连接仍 connected。该批次没有运行真实 JetStream server：本机没有 Docker 或 `nats-server`，OPS-001/TEST-026 仍要求后续可复现 JetStream 总门禁，不能用这里的协议级测试提前关闭。

## 2026-08-21：OPS-001 / TEST-026 JetStream 与 Outbox 故障恢复总门禁

环境：Windows amd64、Go 1.26.4；测试直接链接官方 `github.com/nats-io/nats-server/v2 v2.14.3`，每例在随机 loopback 端口和 `t.TempDir()` 启动 file-backed JetStream，不要求 Docker/预装服务。数据库用远程 PostgreSQL 连接的 MaxConns=1 session-local 临时表，不执行永久 DDL/清理共享 Outbox。前端使用 bundled Node 24.19.0。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| JetStream ACK/重投/重启 | `go test ./internal/queue -run '^(TestJetStreamExplicitAckRedeliveryAndDeduplicationIntegration&#124;TestJetStreamDurableConsumerResumesBacklogAfterRestartIntegration&#124;TestJetStreamReconnectsAfterServerRestartIntegration&#124;TestJetStreamBackoffIsExponentialAndBounded)$' -count=1 -v` | Go 4.298s | 0 | PASS：实际 consumer config 为 ExplicitAck/MaxDeliver=3/200ms→400ms BackOff；同 Msg-Id 去重；三次失败进死信；离线 backlog 恢复且旧 ACK 不重放；file store Server 停启后旧 Client 自动重连/重订阅 |
| Outbox 事务与 JetStream | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/queue -run '^TestTransactionalOutboxRollbackAndDispatchIntegration$' -count=1 -v` | Go 2.146s | 0 | PASS：回滚 0 event；提交后 PubAck、消费和 published；consumer MaxDeliver 后真实临时 PG 死信 attempts=3 |
| lease / 双 dispatcher | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/queue -run '^(TestOutboxStaleLeaseIsRecoveredAfterDispatcherRestart&#124;TestTwoOutboxDispatchersDoNotProcessTheSameClaim)$' -count=1 -v` | Go 1.295s | 0 | PASS：停止实例留下的十分钟 publishing 被新 dispatcher 恢复；第一个 handler 阻塞时第二 dispatcher 取得另一行，两 lease 各发布一次 |
| JetStream 断连恢复 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/queue -run '^TestOutboxRetriesAfterJetStreamDisconnectIntegration$' -count=1 -v` | Go 3.897s | 0 | PASS：Server 停止后同一 event 持久化 failed/last_error 且 Failed=1/Retried=1；同端口/store 重启、旧 Client reconnect 后同一行发布并消费，Published=1 |
| 死信 replay 修复前红测 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestAdminDeadLetterReplayRestoresPublishAndConsumerFailuresIntegration$' -count=1 -v` | 7.6s；Go 1.736s | 1 | 预期 FAIL：真实 PG 返回 `42P18 could not determine data type of parameter $2`，管理员 replay 500 并回滚，证明原“可恢复”路径不可用 |
| 死信恢复闭环 | 同上（显式 cast 修复后） | 9.0s；Go 2.643s | 0 | PASS：publish dead 复位原 Outbox 状态/lease/attempt；consumer dead 新建完整身份的新 event；`replayed_at` 和 actor/original/new 审计同事务；第二次 replay 409 |
| 真实 PG + JetStream 相关总组 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/queue ./internal/httpapi -run '^(TestJetStream&#124;TestTransactionalOutbox&#124;TestOutbox&#124;TestTwoOutbox&#124;TestAdminDeadLetter).*' -count=1` | 13.3s | 0 | PASS：queue 10.498s、HTTP 1.731s；同时包含 OPS-009 的 retry/dead/published/lease SQL 故障注入 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟 17.9s | 0 | PASS：默认全仓实际启动 JetStream；queue 6.094s、HTTP 3.292s、antiabuse 4.188s；Vet/Build 无输出 |
| 依赖 / 前端 / 差异 | `go mod tidy -diff`；bundled Node 下 `pnpm test`、`pnpm lint`、`pnpm typecheck`；`git diff --check` | 20.3s（前端组合墙钟） | 0 | PASS：tidy 无 diff；Node 9/9、全量 ESLint、TypeScript；diff 无空白错误，仅既有 CRLF 提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings` | 1.4s | 1 | 449 唯一 ID、分类计数精确；52 CLOSED / 397 OPEN，High 92 / Medium 157 / Low 47 / unresolved 153；总门禁按设计因剩余 397 项与最终严重度未齐退出 1 |

前端门禁第一次调用 fallback `pnpm.cmd` 时 bundled Node 未进入 PATH，三个进程均在运行测试前报 `node is not recognized`（退出 1）；显式加入已配置的 bundled Node bin 后原命令全部通过。该环境启动失败未记录为代码 PASS/FAIL。Race 没有在此批重新冒充执行：既有探测仍是默认 CGO disabled、开启后本机缺 GCC，最终 Race 门禁继续开放。

`MCMODS_RUN_NATS_INTEGRATION=1` 现在仅用于选择显式外部 URL；没有该变量时三项 JetStream 测试也真实运行。单节点嵌入式证据关闭仓库默认/本机可靠性 Finding，但不声称验证生产 ACL、多节点副本、跨 AZ 网络分区或容量。远程业务库 generation 85 未升级，代码权威 generation 94 的空库安装仍由独立门禁开放。

## 2026-08-21：BUG-075 / BUG-078 / OPS-014 蓝图 owner lease 与原子重试

环境：Windows amd64、Go 1.26.4。行为测试在远程 PostgreSQL 连接中使用 MaxConns=1 的 session-local 临时 `blueprints/blueprint_jobs/nats_outbox`，不读取或写入永久蓝图/Outbox；Schema 权威代次由 94 提升到 95，远程 development 库仍为 generation 85 且未重置。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前源码复现 | `git show HEAD:internal/httpapi/blueprint_worker.go` / `blueprint_handlers.go` / `internal/database/migrations.go` 后以 `rg` 清点 claim、忽略 Exec、主体 failed 与 lease 字段 | 0.8s | 0 | 基线确认：queued→processing 只有 status CAS；completion/failure/progress 全部忽略结果；任意 operation 无条件主体 failed；retry 独立忽略主体 UPDATE 后再开事务；Schema 无 owner/expiry/max/唯一 active |
| Schema 契约 | `go test ./internal/database -run '^TestBlueprintJobsHaveBoundedLeaseAndActiveOperationInvariant$' -count=1 -v` | Go 0.085s（与行为组并行） | 0 | PASS：generation 95 的 max attempts/范围、locked_by、lease expiry、processing recovery index 与 active operation/format 部分唯一索引均存在 |
| PostgreSQL lease / retry / operation 隔离 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi ./internal/database -run '^(TestBlueprintJob&#124;TestBlueprintRetry)' -count=1 -v` | 12.4s；HTTP Go 6.523s | 0 | PASS：stale recovered=1/exhausted=1/active 不动，二扫 0；Outbox CHECK 回滚；heartbeat/token CAS；过期接管 attempts=2；convert 两次失败主体仍 ready；normalize terminal 主体 failed；retry 1 job/1 event 与失败 0 job |
| 最终目标组复核 | 同上（非 verbose） | 并行墙钟 9.2s | 0 | PASS：HTTP 5.834s、database 0.135s；包含既有蓝图单一 Outbox 源码/事务测试，无跳过目标 PG 用例 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟 9.7s | 0 | PASS：全仓 9.5s，HTTP 0.926s、database 0.203s、默认 JetStream queue 3.620s；Vet 4.3s、Build 5.7s |
| 依赖 / 差异 | `go mod tidy -diff`；`git diff --check` | 1.5s | 0 | PASS：模块图无变化；diff 无空白错误，仅既有 CRLF 提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings` | 1.2s | 1 | 449 唯一 ID、分类计数精确；55 CLOSED / 394 OPEN，High 93 / Medium 159 / Low 47 / unresolved 150；总门禁按设计因剩余 Finding/严重度未齐退出 1 |

目标用例显式令 recovery Outbox CHECK 失败并确认 job/主体仍 processing，证明恢复状态和新唤醒事实同事务；另令 retry event CHECK 失败并确认主体仍 failed、job 为 0。第二 Worker 在 DB lease 未过期时得到 `errBlueprintJobLeaseActive`，强制消息 NAK；过期后新 token 可 claim，旧 token 的 completion 和 failure 都返回 lease lost。

本批当时没有把 heartbeat 的单元 CAS 冒充长时 OSS 故障补偿；该窗口已随后由 OPS-015 / generation 96 的预登记与持久补偿关闭。前端没有代码变化，沿用本轮稍早已通过的 9/9 Node、全量 ESLint 与 TypeScript 门禁。Race 仍因 Windows CGO/GCC 环境开放；完整空库安装仍受 CREATEDB/维护库 pg_hba 外部门禁，临时表不冒充完整安装。

## 2026-08-21：OPS-015 蓝图派生 OSS 预登记、原子激活与持久补偿

环境：Windows amd64、Go 1.26.4。行为测试继续使用远程 PostgreSQL 连接上的 MaxConns=1 session-local 临时表，不读取或修改永久蓝图、OSS 文件或删除 Outbox；Schema 权威代次由 95 提升到 96，远程 development 库仍为 generation 85 且未重置。没有可用真实 OSS 隔离桶，因此不把网络 Put/Delete 冒充执行；跨系统窗口由“Put 前持久登记”顺序、真实 PG 状态机/故障注入及现有已验证 deletion Outbox Worker 共同证明。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前调用链复核 | `rg -n 'writeGeneratedOSSObject|randomObjectName|normalized_object_key|blueprint_variants' internal/httpapi/blueprint_worker.go internal/database/migrations.go` 并对照原 Finding | 0.7s | 0 | 基线确认：normalize/cover/convert 均先 PutObject 后写业务事务；normalized file ID 被 `_ = fileID` 丢弃；cover/convert key 随机；没有 pending lineage 或删除事件 |
| generation 96 Schema 契约 | `go test ./internal/database -run '^TestBlueprint' -count=1` | 3.7s；Go 0.965s | 0 | PASS：pending file 状态、normalized FK、artifact lineage/状态/唯一约束、SET NULL/RESTRICT 与两个有界恢复索引全部存在 |
| PostgreSQL artifact/lease 故障组 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi ./internal/database -run '^(TestBlueprintArtifact&#124;TestBlueprintOrphanArtifact&#124;TestBlueprintJobLeaseRecovery&#124;TestBlueprintJobLeaseOwnership&#124;TestBlueprintJobRetryBudget&#124;TestBlueprintRetry&#124;TestBlueprintJobsHaveBounded)' -count=1` | 15.0s；HTTP Go 10.959s | 0 | PASS：激活+Job 完成同成；激活 CHECK 失败全回滚；failure/lease 补偿恰好一条 delete；missing/completed job 与 deleted blueprint active artifact 恢复 3/0；活跃 attempt 不动；Outbox CHECK 故障全回滚 |
| 10 万行恢复计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestBlueprintOrphanArtifactRecoveryPlanUsesBoundedIndexesIntegration$' -count=1 -v` | 10.9s；Go 3.445s | 0 | PASS：100,000 个非 orphan active lineage + 2 个目标下，`EXPLAIN (ANALYZE,BUFFERS)` 同时使用 pending 与 active-orphan 两个部分索引；扫描仍由 limit 100 限界 |
| 确定性 key | `TestBlueprintArtifactObjectKeyIsAttemptIdempotent`（包含于上组）及 `rg -n 'writeGeneratedOSSObject|randomObjectName' internal/httpapi/blueprint_worker.go` | <0.1s | 0 | PASS：同 job/attempt/role 得到同 key，新 attempt 不同；蓝图 Worker 不再调用先上传后 active helper或随机 key |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟 15.1s | 0 | PASS：全仓 10.6s，HTTP 1.092s、database 0.197s、默认 JetStream queue 3.676s；Vet 6.8s、Build 6.6s |
| 依赖 / 差异 | `go mod tidy -diff`；`git diff --check` | tidy 1.1s；diff 0.9s | 0 | PASS：模块图无变化；diff 无空白错误，仅既有 CRLF 提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings` | 1.5s | 1 | 449 唯一 ID、分类计数精确；56 CLOSED / 393 OPEN，High 94 / Medium 159 / Low 47 / unresolved 149；总门禁按设计因剩余 Finding/严重度未齐退出 1 |

`writePendingBlueprintArtifact` 在执行 OSS PUT 前提交 pending file 与 artifact，且保存上传时的 origin 删除目标；Put 失败也保留可补偿身份。成功路径只有在 normalized/cover/variant 引用、两层 active 状态和当前 token Job completion 同一事务提交后才可见。普通错误、lease expiry、job 缺失/终态及 blueprint 删除均有持久恢复入口；补偿 Outbox 写失败时文件、artifact、job、主体和恢复事件不会部分提交。

本批未改变前端代码，沿用本轮已实际通过的前端 9/9 测试、全量 ESLint 与 TypeScript 门禁。Race 仍因 Windows 缺 GCC、generation 96 完整空库仍因账号无 CREATEDB/维护库 pg_hba 拒绝而开放；临时表行为测试不冒充这两项总门禁，也未访问未知远程永久业务表。

## 2026-08-21：BUG-081 / OPS-016 OSS 删除有界 dead 与审计重放

环境：Windows amd64、Go 1.26.4。行为和计划测试使用 MaxConns=1 的 session-local PostgreSQL 临时文件、删除 Outbox 与应用日志表，不执行永久 DDL、不触碰远程业务对象；Schema 权威由 generation 96 提升到 97，远程 development 库仍为 generation 85。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前状态机复核 | 对照原 Finding，检查 `oss_deletion_outbox.go` 与 generation 96 DDL 的 completed 冲突、retry、claim、失败状态和管理路由 | <1s | 0 | 基线确认：唯一目标命中 completed 时只 `DO NOTHING`，Key 新生命周期可能永远不删；失败始终回 pending、无 max/dead/owner token/稳定分类/管理员 replay |
| 错误分类与路由契约 | `go test ./internal/httpapi -run '^TestOSSDeletion' -count=1` | Go <1s | 0 | PASS：配置、认证、授权和无效目标为 permanent；限流、5xx、网络、timeout 和 unknown 按稳定规则分类；管理员路由分别要求 `admin.config.read/write` |
| PostgreSQL 状态机目标组 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi ./internal/database -run '^(TestOSSDeletion&#124;TestTruncate)' -count=1` | 最终墙钟 15.2s；HTTP 7.151s、database 0.097s | 0 | PASS：暂时失败按预算 retry/dead，永久失败立即 dead，告警 CHECK 失败整笔回滚；stale exhausted 归类 worker_lost；claim/reclaim 与旧 token CAS；不同 file ID 的 completed Key 新周期接管当前 lineage/region，重复 deleted tombstone、管理员列表/重放/二次 dead 均通过 |
| PostgreSQL 规模计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestOSSDeletionPlansUseBoundedIndexesIntegration$' -count=1 -v` | 墙钟 10.6s；Go 2.934s | 0 | PASS：100,000 个 completed 历史下，耗尽恢复和 dead 列表分别使用 generation 97 的 exhausted/dead 部分索引 |
| 蓝图/贴纸/删除相关回归 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^(TestBlueprintArtifact&#124;TestBlueprintOrphanArtifact&#124;TestBlueprintJobLeaseRecovery&#124;TestStickerLifecycle&#124;TestOSSDeletion)' -count=1` | 墙钟 14.3s；Go 11.388s | 0 | PASS：generation 97 删除表兼容蓝图补偿与贴纸生命周期；各目标真实 PG 用例未 skip |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 最终并行墙钟 14.9s | 0 | PASS：全仓 14.677s，HTTP 1.007s、database 0.378s、默认 JetStream queue 3.721s；Vet 10.750s、Build 12.101s |
| 依赖 / 差异 | `go mod tidy -diff`；`git diff --check` | 0.922s / 0.866s | 0 | PASS：模块图无变化；diff 无空白错误，仅既有 CRLF 转换提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings` | 1.233s | 1 | 449 唯一 ID、分类计数精确；58 CLOSED / 391 OPEN，High 94 / Medium 161 / Low 47 / unresolved 147；总门禁按设计因剩余 Finding/严重度未齐退出 1 |

第一次组合 PostgreSQL 重跑曾在建立远程连接时返回一次 `unexpected EOF`，没有把它记录为业务 PASS；随后显式隔离重跑成功（Go 0.895s），最终目标组和蓝图/贴纸/删除相关总组也均成功。这里证明数据库状态机、事务和查询计划，不声称真实 OSS 供应商的鉴权、限流或网络错误均已注入。

新周期只在新的文件删除生命周期进入 enqueue 时复位 completed/dead；对同一已 deleted 文件重复 tombstone 仍幂等，因此同时避免“旧墓碑吞新删除”和“旧请求无限复活”。dead 状态与安全日志在同一事务，管理员每次 replay 都保留 actor/time/count 并写审计；普通用户 API 无变化，前端无代码变化。Race 仍因本机默认 CGO disabled、显式开启后缺 GCC 而开放；generation 97 完整空库仍因账号无 CREATEDB、维护库被 pg_hba 拒绝而开放，临时表不冒充该门禁。

## 2026-08-21：OPS-017 / LEGACY-014 资料图片迁址持久任务

环境：Windows amd64、Go 1.26.4。真实 PostgreSQL 用 MaxConns=1 的 session-local `mods/oss_rehome_jobs/app_logs`，不读取或修改永久模组/OSS 文件；对象处理由可注入 processor 验证 Worker 重试，未连接真实 OSS。Schema 权威从 generation 97 提升到 98，远程 development 库仍为 generation 85。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前源码复核 | `oss_rehome.go` 与三个调用点：channel、`sync.Map`、`sync.Once`、满队列 default、提交后 schedule、失败仅日志 | <1s | 0 | 基线确认：任务无数据库事实，满 64/进程退出/复制失败均可永久丢失，且多实例互不协调 |
| 新测试夹具首跑 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi ./internal/database -run '^TestOSSRehome' -count=1 -v` | 6.2s | 1 | 新夹具暴露两个测试假设：rollback 后 sequence 不回退，bigint 被直接绑定到 text 参数；均修正为使用实际 job ID 和 `bigint::text`，未把该轮记为业务 PASS |
| generation 98 Schema / 源码契约 | `go test ./internal/httpapi ./internal/database -run '^TestOSSRehome' -count=1`（未开 PG 时集成用例 skip）及生产 `rg` 旧符号 | 9.0s | 0 | PASS：表/字段/CHECK/四索引完整；三个调用点恰好事务 enqueue；runtime 启动持久 Worker；生产 channel/Map/Once/schedule 符号零命中；管理员路由权限完整 |
| PostgreSQL 状态机目标组 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi ./internal/database -run '^TestOSSRehome' -count=1` | 墙钟 14.2s；HTTP 6.916s、database 0.488s | 0 | PASS：enqueue rollback 0/commit 1；新 generation 令旧完成重新 queued；旧 token 拒绝；stale lease 被新 owner 接管；processor 失败持久 retry 后完成；预算/永久/worker_lost dead、告警回滚、列表/重放/audit 均精确 |
| 100k 查询计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestOSSRehomePlansUseBoundedIndexesIntegration$' -count=1 -v` | 墙钟 10.5s；Go 3.043s，用例 1.63s | 0 | PASS：100,000 completed 历史下，生产等价 claim 同时命中 ready/recovery；耗尽和 dead 扫描分别命中 exhausted/dead 部分索引 |
| OSS 删除 + 迁址总组 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi ./internal/database -run '^(TestOSS(Rehome&#124;Deletion)&#124;TestTruncate)' -count=1` | 墙钟 14.3s；HTTP 11.744s、database 0.116s | 0 | PASS：共享稳定错误分类未回归 generation 97 删除状态机；两类 dead/重放/计划均真实 PG 通过 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟 11.1s | 0 | PASS：全仓 10.893s，HTTP 1.750s、database 0.175s、默认 JetStream queue 4.542s；Vet 6.363s、Build 8.770s |
| 依赖 / 差异 | `go mod tidy -diff`；`git diff --check` | 0.942s / 0.911s | 0 | PASS：模块图无变化；diff 无空白错误，仅既有 CRLF 转换提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings` | 1.307s | 1 | 449 唯一 ID、分类计数精确；60 CLOSED / 389 OPEN，High 94 / Medium 163 / Low 47 / unresolved 145；总门禁按设计因剩余 Finding/严重度未齐退出 1；TEST-037 保持开放 |

处理器测试证明“复制调用返回错误”实际经过 drain→claim→持久 retry→再次 claim→completed；它不声称模拟阿里云 OSS 的真实 CopyObject 字节或 ACL。迁址函数仍使用确定性目标 key、源 key CAS 和同事务旧对象 deletion Outbox，部分成功可在下一 generation/attempt 跳过并继续。真实 OSS 故障语料仍属于开放的 TEST-037，其 ESA、额度、分片放弃和 GIF 后续帧也未被本批提前关闭。

本批没有前端代码变化，沿用本轮先前已通过的前端门禁。Race 仍因本机 CGO/GCC 环境开放；generation 98 完整空库仍因账号无 CREATEDB、维护库 pg_hba 拒绝而开放，session-local 表不冒充完整安装。

## 2026-08-21：LEGACY-015 / LEGACY-016 删除未发布兼容协议

环境：Windows amd64、Go 1.26.4；前端 bundled Node 24.19.0、Next 16.2.11。不连接 PostgreSQL、OSS 或 NATS；两项均为 DTO/路由/选择逻辑删除，Schema generation 保持 98。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 后端定向契约 | `go test ./internal/httpapi -run '^(TestOSSMultipart&#124;TestValidOSSMultipart&#124;TestLegacyMultipart)' -count=1` | 墙钟 8.8s；Go 1.484s | 0 | PASS：阈值 -1 不分片、阈值强制分片；旧 bool 参数/字段、评论 DTO/function/route 均由源码门禁拒绝 |
| 前端字段迁移 | 全仓 `rg -n 'preferMultipart&#124;comments/.*/reports'`；bundled Node `pnpm test`、`pnpm lint`、`pnpm typecheck` | tests 1.582s；Lint 25.519s；TypeScript 12.688s | 0 | PASS：9/9 tests；ESLint、TypeScript 通过；六处请求字段删除后全项目零命中 |
| 前端生产构建 | bundled Node `pnpm build` | 30.0s | 0 | PASS：Next 编译、TypeScript、31 workers page data 与 59/59 静态页面全部完成 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟 15.0s | 0 | PASS：全仓 14.747s，HTTP 1.050s、database 0.307s、默认 JetStream queue 3.795s；Vet 10.887s、Build 12.145s |
| 依赖 / 差异 / 台账 | `go mod tidy -diff`；前后端 `git diff --check`；`go run ./tools/remediation/verify_findings` | tidy 0.955s；后端 diff 0.938s、前端 diff 0.600s；台账 1.483s | 0 / 1 | 模块图与 diff 无错误，仅既有 CRLF 提示；449 唯一 ID/分类精确，62 CLOSED / 387 OPEN，High 94 / Medium 163 / Low 49 / unresolved 143；验证器仅因剩余 Finding/最终严重度未齐退出 1 |

本批没有把“服务端强制 multipart”描述为“分片生命周期完成”：客户端崩溃后的持久 session、到期主动 Abort 和 Bucket 生命周期验证仍是 OPS-018；TEST-037 的跨用户重放、并发额度与 GIF 等矩阵也保持开放。评论举报仍由统一目标可见性、原因闭集与快照事务负责，本批只删除无调用方的第二入口。

## 2026-08-21：OPS-018 持久分片会话与到期清理

环境：Windows amd64、Go 1.26.4。Schema 权威从 generation 98 提升到 99；真实 PostgreSQL 测试仅建立 session-local 临时 `oss_multipart_sessions/app_logs` 和部分索引，MaxConns=1，未修改远程 generation 85 永久 Schema。完成复核使用本机 `httptest` 加真实阿里云 OSS v2 SDK 的 HeadObject/header 解析；没有发送供应商凭据或访问真实 Bucket。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前代码复核 | initiation/complete/abort、`oss_multipart.go`、runtime 与 Bucket lifecycle API | <1s | 0 | 基线确认：ticket 只含 upload ID/签名，进程无 session/expiry/cancel 事实；崩溃后没有主动 Abort，Bucket lifecycle 既未读取也非部署硬前提 |
| 夹具迭代 | 首轮真实 PG 100k 计划；本机 SDK Head 测试首轮 | 计划隔离重跑墙钟 10.459s；SDK 首轮 8.7s | 1 → 0 | 两个失败均为夹具：参数化 multi-command 不能作为 prepared statement，拆分 insert/analyze；自定义 endpoint 的实际路径是 `/bucket/object.zip` 而非 `/object.zip`，按 SDK 行为修正。未把失败轮记为业务 PASS |
| generation 99 Schema / 线路契约 | `go test ./internal/httpapi ./internal/database ./internal/app -run '^TestOSSMultipart' -count=1`；源码路由/Worker 门禁 | 最终墙钟 12.6s；HTTP 1.453s、database 0.123s | 0 | PASS：表、状态/预算/失败 CHECK、五个部分索引完整；initiation 必须登记；complete/abort 必须 begin/finish session；admin read/write 路由与 runtime lifecycle hard gate 保持接线 |
| PostgreSQL 会话/Worker/admin/规模目标组 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi ./internal/database -run '^Test(OSSMultipart&#124;CompletedOSSMultipart&#124;VerifyCompletedOSSMultipart)' -count=1 -v` | 墙钟 9.289s；HTTP 6.364s、database 0.110s | 0 | PASS：跨用户/篡改拒绝、active token 冲突、complete/abort 终态幂等；到期 active Abort、新鲜 active 不动、永久授权错误 dead、stale 耗尽 lease worker_lost；dead 告警 CHECK 失败状态全回滚；列表/重放 actor/count/audit；真实 SDK size/SHA；五类 100k 索引均命中 |
| OSS 可靠性总组 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi ./internal/database -run '^TestOSS' -count=1` | 墙钟 20.322s；HTTP 17.544s、database 0.113s | 0 | PASS：generation 97 删除、generation 98 迁址与 generation 99 分片状态机/管理员/计划同时通过；共享失败分类未回归 |
| 后端全仓（不启用共享 DB 集成） | `go test ./... -count=1` | 最终并行墙钟 10.103s | 0 | PASS：HTTP 1.029s、database 0.381s、默认 JetStream queue 3.689s；全部包通过或无测试 |
| 后端 Vet / 构建 / 依赖 | `go vet ./...`；`go build ./...`；`go mod tidy` | 最终 Vet 5.875s；Build 7.577s；tidy 1.170s | 0 | PASS：静态检查、全构建和模块整理成功 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 最终后端 0.959s；前端 0.757s | 0 | PASS：无空白错误，仅工作区既有 LF→CRLF 提示；本批无前端代码变化 |
| 共享数据库全集成边界 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./... -count=1` | HTTP 94.702s 后结束 | 1 | 未记为 PASS：共享远程库仍为 generation 85，generation 99 preflight 明确拒绝；尚未重建的 `user_permissions.source` 等列也缺失。失败均发生于已记录的外部完整空库门禁，不是新 session-local 目标组失败，也未重置未知库 |
| 台账精确性 | `go run ./tools/remediation/verify_findings` | 最终 1.374s | 1 | 449 唯一 ID/分类精确；63 CLOSED / 386 OPEN，High 94 / Medium 164 / Low 49 / unresolved 142；仅因剩余 Finding 与最终严重度未齐按设计失败；TEST-037 保持开放 |

完成状态不再先于对象真相：Complete 成功或 `NoSuchUpload` 都要先 HeadObject 且 size/SHA metadata 精确匹配，才把 session durable 标为 completed；验证失败恢复 active。因此丢失成功响应可安全重试，而供应商生命周期已 Abort 的 upload 不会被误记完成。Worker 的 dead 状态与告警同事务，显式测试证明告警约束失败时会话仍保留 aborting lease 供恢复。

本批没有声称真实阿里云 Bucket 已配置：代码把读取到符合条件的 1..7 天 AbortMultipartUpload rule 作为 OSS 启动硬门禁，但当前环境没有授权的真实 Bucket 验证或故障注入。该广域供应商/额度/并发矩阵仍由 TEST-037 开放追踪。Race 仍因本机 CGO/GCC 环境开放；generation 99 完整空库仍因账号无 CREATEDB、维护库 pg_hba 拒绝而开放。前端无新代码变化，沿用本轮 D-041 后已通过的 9 tests/Lint/TypeScript/59 页构建。

## 2026-08-21：BUG-001 / ARCH-001 / LEGACY-001 系统通知模板收口

环境：Windows amd64、Go 1.26.4。无 Schema 变化，权威 generation 保持 99。真实 PostgreSQL 用 MaxConns=1 的 session-local `users/system_settings/notifications/actors/receipts/nats_outbox/review subscriptions` 临时表；未读取或修改远程 generation 85 的永久通知数据，也不连接 NATS、Redis 或 SMTP。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 当前代码复核 | BUG-001、ARCH-001、LEGACY-001 详细证据；八类生产者、模板 loader/render、Notification Worker 与 Outbox helper 全调用图 | <1s | 0 | 基线确认：编辑员审核/认领撤销英文却标 `zh-CN`，审核完成/关注/治理固定中文；template render/INSERT 失败无返回；旧 direct INSERT fallback 仍被调用 |
| 模板闭集与旧路径门禁 | `go test ./internal/httpapi -run 'Test(MigratedSystemNotifications&#124;SystemNotificationMigration&#124;FollowerNotification&#124;DirectMessageEmail&#124;NotificationTemplate)' -count=1 -v` | 墙钟 9.7s；Go 1.530s | 0 | PASS：默认模板配置通过完整 locale 验证；新增 key 的 zh-CN/en-US 精确选取；八类生产文件包含 key，旧 direct helper/治理 Wrapper/follower body 零残留 |
| PostgreSQL 故障与事务目标组 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'Test(NotificationTemplateFailuresAreExplicitIntegration&#124;TemplatedNotificationOutboxCommitsOrRollsBackIntegration&#124;ReviewCompletionNotificationFactsCommitAtomicallyIntegration&#124;FollowerWorkerPersistsLocalizedTemplateSnapshotIntegration)$' -count=1 -v` | 最终墙钟 12.4s；Go 4.883s | 0 | PASS：malformed JSON、设置查询列和 recipient locale 错误均返回；business/event 1/1 且 CHECK 失败 business=0；review 2 subscriptions/2 events，拒绝时 `notified_at`=null；英文 follower notification/email title/body 相同 |
| 结构化故障信号 | 上述 Outbox CHECK 注入调用 `sendTemplatedNotification` | 包含于 PG 目标组 | 0 | PASS：sender 返回 SQLSTATE 23514 并输出 recipient_id/template_key/error 的结构化 ERROR；没有记录 template values；事务事实未提交 |
| HTTP / Queue / App 包 | `go test ./internal/httpapi ./internal/queue ./internal/app -count=1` | 墙钟 10.3s | 0 | PASS：HTTP 0.652s、queue 3.472s、app 1.470s |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟 9.9s；Test 9.747s、Vet 6.126s、Build 7.113s | 0 | PASS：全仓默认测试、静态检查和构建全部通过；HTTP 1.097s、queue 3.706s、serverprobe 4.152s |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.949s；前端 0.744s | 0 | PASS：无空白错误；仅既有 LF→CRLF 转换提示；本批无前端代码变化 |
| 台账精确性 | `go run ./tools/remediation/verify_findings` | 1.356s | 1 | 449 唯一 Finding/分类精确；66 CLOSED / 383 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；仅因剩余 OPEN/严重度按设计非零；本批三项均独立关闭 |

模板值只在 Worker 取得收件人语言后渲染，持久通知保存实际 title/body 与 template snapshot；系统通知不进入 AI 翻译。配置暂时损坏会让 durable event 重试/死信，不再被 Worker ACK 为成功。评论回复/监听等用户生成摘要仍可携带明确 `source_locale`，但也已删除直接数据库 fallback 并只写 Outbox；它们不是本批八类后台系统模板，未伪报为模板化完成。

本批不改变 generation 99 外部门禁、Race/GCC、真实 OSS 或 TEST-037 状态，也不将 session-local PostgreSQL 夹具冒充完整空库安装。前端没有代码或协议变化，因此不重复运行上一批已通过的前端构建，只执行最终前端差异空白门禁。

## 2026-08-21：BUG-010 / PERF-006 / PERF-007 / ARCH-009 公开合成权威与 O(1) 缓存代次

环境：Windows amd64、Go 1.26.4。行为与计划测试使用远程 PostgreSQL 单连接 session-local 临时表，不读写永久 catalog 数据；Schema 权威由 generation 99 升至 100，远程 development 库仍为 generation 85 且未升级。性能证据包含 100,000 行合成 revision history 的查询计划，不把未执行的 100 万/1,000 万行插入写成实测。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 公开权威与严格解码目标组 | `go test ./internal/httpapi ./internal/database -run 'Test(CatalogDataset&#124;InvalidImportedCatalysts&#124;GlobalRecipeTypeList&#124;PublicRecipeSelection)' -count=1` | 最终墙钟 4.945s；HTTP Go 1.601s、database 0.167s | 0 | PASS：canonical definition 与 observation 同在时只返回 canonical；import-only 明确回退；canonical render 为 1 slot/1 candidate；损坏 catalyst 结构携带 revision ID 返回错误 |
| 催化剂批量查询 | `TestGlobalRecipeTypeListUsesBatchedCatalystQueries` 与上述真实 PG selection case | 包含于目标组 | 0 | PASS：页内 type IDs 一次 canonical `ANY(bigint[])`，只对缺失 ID 一次 fallback；资源 ID/export 装饰同样批量，查询数不随类型数线性增加 |
| 缓存代次事务与失败关闭 | `TestCatalogDatasetVersionCommitRollbackAndPlanIntegration`、`TestCatalogDatasetVersionReplacesRevisionAggregation` | 包含于目标组 | 0 | PASS：commit 令 version 1→2；rollback 保持 1；singleton 缺失返回错误；发布、归档、localization 与三类 import activation/rejection 路径均在原事务 bump |
| 100k 合成历史查询计划 | `TestCatalogDatasetVersionCommitRollbackAndPlanIntegration` 中 `EXPLAIN (ANALYZE, BUFFERS)` | 包含于目标组 | 0 | PASS：100,000 行 dummy revision history 存在时，版本查询只访问单行 `catalog_dataset_state`，计划不引用 revision history；结构上不再具有历史规模输入 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | Test 墙钟 12.8s；Vet 4.974s；Build 6.285s | 0 | PASS：全仓所有包通过；HTTP 1.770s；Vet 与 Build 无错误 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.852s；前端 0.905s | 0 | PASS：无空白错误；仅既有 LF→CRLF 转换提示；本批无前端代码变化 |
| 台账精确性 | `go run ./tools/remediation/verify_findings` | 0.772s | 1 | 449 唯一 ID 与分类精确；70 CLOSED / 379 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；总门禁仅因其余 Finding 与最终严重度仍开放而按设计退出 1，本批四项均独立关闭 |

详情、计数、列表和 render 共用 `publicRecipeSelectionCTE`：active canonical definition 优先，只有该实体无 definition 时才读取最新 active import observation。公共响应在资源装饰后删除内部 entity/authority 标记，保持既有 DTO；手工发布不再需要伪造 import snapshot。催化剂 JSON 必须为对象数组，损坏 observation 不再伪装成空列表并进入缓存。

`writeCachedCatalog` 现在只读取 generation 100 的 singleton version，不再对全部 revisions 做 `string_agg` 或时间聚合。版本递增是各发布/激活事务的组成部分，失败会回滚业务事实，避免新数据配旧缓存代次。本批没有执行 100 万或 1,000 万行合成数据，因此只记录已完成的 100k 计划与 O(1) 查询结构；完整 generation 100 空库安装仍受远程账号无 CREATEDB、维护库 pg_hba 拒绝的外部门禁，Race 仍受本机 CGO/GCC 环境限制。

## 2026-08-21：BUG-011 / BUG-012 / BUG-013 / BUG-014 目录导入失败关闭边界

环境：Windows amd64、Go 1.26.4，权威 Schema 保持 generation 100。archive/unknown-registry 行为测试在远程 generation 85 现有业务表中开启事务、创建唯一测试事实并最终 rollback，不留下永久数据；跨 kind resolver 使用 MaxConns=1 session-local 临时表。没有执行 DDL、数据库重置、OSS/NATS/Redis 调用或远程治理状态修改。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 身份、归属与入口闭集 | `go test ./internal/httpapi -run 'Test(UnknownRegistryKinds&#124;AutomaticCatalogImports&#124;MaterializedImportEntries&#124;ExportRevisionRequiresExactNamespace&#124;ImportQueuesRejectUnknownNamespaces)' -count=1 -v` | Go 1.284s | 0 | PASS：未知 registry kind 固定长度且不泄露原文；同 registry 规范化稳定、不同 registry 不同 identity；空/未知 namespace 即使只有一个 revision 也报错；document/recipe queue 与全部物化入口均传播错误 |
| PostgreSQL archive 与未知 registry | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestPersistCatalogResourcesReusesCanonicalIdentityIntegration$' -count=1 -v`（并入最终目标组） | 最终 case 3.15s | 0 | PASS：archived resource/tag 重导入后 status/archived_at 不变但 observation 各为 1；同 canonical ID 的两个 unknown registry 为 2 resource/2 snapshot，kind family 都是 document |
| PostgreSQL 跨 kind 解析 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestExportResourceResolutionRequiresRequestedKindBeforeRevisionPreferenceIntegration$' -count=1 -v`（并入最终目标组） | 最终 case 0.43s | 0 | PASS：preferred revision 只有 block、其他 active revision 有 item 时选择 item；manual content block/item fallback 也只选择 item |
| 最终目标组合 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'Test(UnknownRegistryKinds&#124;AutomaticCatalogImports&#124;MaterializedImportEntries&#124;PersistCatalogResourcesReusesCanonicalIdentityIntegration&#124;ExportRevisionRequiresExactNamespace&#124;ImportQueuesRejectUnknownNamespacesInsteadOfDroppingEntries&#124;ExportResourceResolutionRequiresRequestedKindBeforeRevisionPreferenceIntegration)' -count=1 -v` | 墙钟 6.459s；Go 3.746s | 0 | PASS：七项全部运行，无 skip；包含真实 PG rollback fixture 与 session-local resolver fixture |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | Test 14.771s；Vet 10.865s；Build 12.066s | 0 | PASS：全仓所有包通过；HTTP 0.958s、queue 3.758s、antiabuse 3.169s；Vet/Build 无错误 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.616s；前端 0.340s | 0 | PASS：无空白错误；既有 CRLF 转换提示被静默过滤；本批无前端代码变化 |
| 台账精确性 | `go run ./tools/remediation/verify_findings` | 1.025s | 1 | 449 唯一 ID 与分类精确；74 CLOSED / 375 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；仅因剩余 Finding/最终严重度按设计退出 1，本批四个 High 均独立关闭 |

自动导入的七个 active conflict 路径统一只允许当前 `placeholder` 晋升；`archived` observation 仍保存，但公开治理状态不变，显式 catalog publication 保持唯一恢复入口。未知 registry 通过规范化 registry 的 96-bit digest 进入 kind identity，不保留旧共享 `import.document` 双读；站点仍在开发期，已有开发数据应随权威 Schema 整库重建。

namespace 错误现在携带 asset/entry 或 recipe type 上下文并令导入 Job 失败。通用非物化附属资产仍允许按既有规则忽略，但 document、registry resource、block binding/entity、JEI category/index/template/recipe collection 均不能再静默丢项。引用 resolver 的 import 与 manual 两条路径都将 kind family 作为必要条件，无同 kind 候选就保持未解析。本批不改变 Race/GCC、generation 100 完整空库、真实 OSS 或 TEST-037 外部门禁。

## 2026-08-21：PERF-010 / PERF-011 / ARCH-011 / DEAD-002 Mod 导出公开查询硬边界

环境：Windows amd64、Go 1.26.4，权威 Schema 保持 generation 100。分页行为和规模计划使用远程 PostgreSQL 的单连接 session-local 临时表；实际创建 100,000 后扩展至 1,000,000 条 Tag 成员和 1,000,000 条 text asset，并在会话结束时自动删除，不读取或修改永久 catalog 数据。没有执行 DDL、数据库重置、OSS/NATS/Redis 调用或远程审核/激活操作。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| Cursor、严格 JSON 与小页行为 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'Test(ModExportPageCursor&#124;ModExportJSONDecoding&#124;ModExportTagAndAssetPages&#124;ModExportPublicTagAndAsset&#124;ModExportQueryPaths)' -count=1`（并入最终目标组） | 包含于最终墙钟 15.291s | 0 | PASS：cursor 绑定 revision/filter 且拒绝畸形/超长值；5 Tag 与 5 member 以 limit 2 完整遍历无跳过/重复；完整资产保留 text/binary 同路径两行，pathsOnly 去重；JSON array names 携带 member ID 报错 |
| 100k / 1M 合成计划与硬页界 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestModExportTagAndAssetScalePlansUseCursorIndexesIntegration$' -count=1 -v` | 独立墙钟 13.6s；case 10.73s；Go 10.877s | 0 | PASS：先装载/ANALYZE 各 100k，再实际扩展/ANALYZE 至各 1M；Tag 两规模命中 `tag_import_members_pkey`，资产两规模命中 `catalog_import_text_assets_pkey`；900k cursor 后两 loader 均只返回 100 且 hasMore=true |
| 最终目标组合 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'Test(ModExportPageCursor&#124;ModExportJSONDecoding&#124;ModExportTagAndAssetPages&#124;ModExportTagAndAssetScalePlans&#124;ModExportPublicTagAndAsset&#124;ModExportQueryPaths)' -count=1` | 墙钟 15.291s；Go 11.894s | 0 | PASS：规模 case 与源码闭集全部运行，无 skip；Tag/成员上限 200、资产上限 500，公开 all/10000 和审计点名的忽略式解码零残留 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | Test 墙钟 8.969s；Vet 4.048s；Build 5.585s | 0 | PASS：全仓所有包通过；HTTP 0.897s、queue 3.685s、antiabuse 3.180s、serverprobe 3.008s；Vet/Build 无错误 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.533s；前端 0.306s | 0 | PASS：无空白错误；既有换行转换提示被静默过滤；本批无前端代码变化 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 最终普通门禁 0.762s | 1 / 0 | 449 唯一 ID 与分类精确；78 CLOSED / 371 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；普通总门禁仅因其余 Finding 与最终严重度开放而按设计退出 1，本批两个 High、一个 Medium、一个 Low 均独立关闭 |

Tag 列表与成员 cursor 分别使用唯一 `(registry,canonical_id)` 和 `raw_member_id` 顺序；资产完整模式使用 `(asset_path,source_order)`，所以相同路径跨来源不会落在页缝，路径模式则按合同去重。cursor scope 包含 revision 与所有影响结果集的过滤条件，不能被换到另一修订或模式。`limit+1` 只决定后续页信号，返回数组本身始终裁到硬上限。

`rows.Err()` 与严格 JSON shape 检查覆盖审计点名的 revision summary、registry、entry、document、asset、structure、Tag 和模型 variant 分支。jsonb 保证语法合法但不保证对象/数组 shape，因此真实 PG 测试写入合法 JSON array 来证明损坏结构不会继续返回部分 200。文档导入的无语义 ordinal 已直接删除。本批不改变 Race/GCC、generation 100 完整空库、真实 OSS 或 TEST-037 外部门禁，也没有把公开分页冒充异步完整导出能力。

## 2026-08-21：MAP-004 / BUG-015 / BUG-016 单一公开 ID、语言继承与审核审计

环境：Windows amd64、Go 1.26.4、Node 运行时由 Codex workspace bundle 提供，权威 Schema 保持 generation 100。审核、Job 和 resolver shape 测试使用远程 PostgreSQL 单连接 session-local 临时表；没有触碰永久 `audit_events`、revision、用户偏好或 catalog 数据，没有执行 DDL、数据库重置、OSS/NATS/Redis 调用或远程审核动作。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| ID、locale、审核与严格 shape 目标组 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'Test(OptionalExportContentLocale&#124;ModExportRevisionReviewAudit&#124;ModExportJobResponse&#124;ModExportPublicResources&#124;ModExportReviewRecords&#124;ModExportEntryLocale&#124;ModExportQueryPaths&#124;ModExportEntryDetail&#124;ExportResourceResolutionRequiresRequestedKind)' -count=1` | 墙钟 5.277s；Go 2.000s | 0 | PASS：publicId 单一源码/DTO、locale 继承顺序、approve/reject 双分支审计、Job/resource JSON shape 与详情快照均运行，无 skip |
| 审核提交与原子故障 | `TestModExportRevisionReviewAuditCommitsFactsAndFailsAtomicallyIntegration`（包含于目标组） | 包含于目标组 | 0 | PASS：approved 保存 actor 42、decision/note、before/after active、aggregate key 与非零 created_at；CHECK 拒绝 audit 后 revision-rollback 保持 ready/false，失败 audit 数为 0 |
| 合法 JSON 错误 shape | `TestModExportJobResponseRejectsWrongJSONShapesIntegration`、`TestExportResourceResolutionRequiresRequestedKindBeforeRevisionPreferenceIntegration` | 包含于目标组 | 0 | PASS：configured_modids object 与 error_detail array 均失败；resolver names array 失败，恢复 object 后同 kind 选择仍正确；Mod export 生产文件 `_ = json.Unmarshal` 零残留 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | Test 墙钟 15.158s；Vet 11.059s；Build 12.104s | 0 | PASS：全仓所有包通过；HTTP 1.149s、queue 3.791s、antiabuse 3.155s、serverprobe 4.189s；Vet/Build 无错误 |
| 前端合同与生产构建 | workspace Node + `pnpm run typecheck`；`pnpm run lint`；`pnpm test`；`pnpm build` | Typecheck 3.665s；Lint 31.885s；Test 1.073s；Build 33.512s | 0 | PASS：TypeScript/ESLint 无错误，9/9 tests 通过，Next.js 16.2.11 production build 完成；entry/resource source/blueprint 均只消费 publicId |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.617s；前端 0.332s | 0 | PASS：无空白错误；既有换行转换提示被静默过滤 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 普通门禁 1.054s | 1 / 0 | 449 唯一 ID 与分类精确；81 CLOSED / 368 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；普通总门禁 510 项仅因其余 Finding/最终严重度开放，本批两个 Medium、一个 Low 均独立关闭 |

公共 Mod export 响应和第一方前端现在只使用 `publicId`；SQL resolver 不再把同一个 `entity.public_id` 扫描两次。entry/tag detail 的 lookup 参数同步改名，开发期不保留旧 `entityId` fallback。blueprint material 复用了同一个 resolver，因此也同步迁移，避免另一条公共链继续制造双名身份。

entry detail 的 primary 先来自登录用户 preference，匿名来自 Accept-Language；只有明确且合法的 query 才覆盖，resolved primary 继续驱动知识页、翻译与 recipe 资源。审核 note 最大 4096 bytes，并和决策及前后状态在业务事务中写已有不可变审计；管理员活动日志已有该表的统一读取。本批不改变 Race/GCC、generation 100 完整空库、真实 OSS 或 TEST-037 外部门禁。

## 2026-08-21：BUG-017 同 content version 导入同步串行化

环境：Windows amd64、Go 1.26.4，权威 Schema 保持 generation 100。锁竞争测试使用远程 PostgreSQL 两条真实连接，每条连接建立自己的 session-local `mod_content_versions`，以同一 version ID 竞争全局 transaction advisory lock；placement 约束测试使用 session-local 临时表。没有读取/修改永久版本、section、resource 或 activation 数据。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| Version 锁、精确冲突与源码闭集 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'TestImported(ContentVersionLock&#124;PlacementConflict&#124;ContentSyncLocks&#124;PlacementConflictIs)' -count=1` | 墙钟 5.008s；Go 1.667s | 0 | PASS：四项全部运行，无 skip；双连接锁竞争、同资源幂等、ordinal/identity 冲突与生产入口顺序均验证 |
| 跨连接事务互斥 | `TestImportedContentVersionLockSerializesConcurrentSyncIntegration` | 包含于目标组 | 0 | PASS：第一事务取得 version 7001 锁后，第二事务至少 250ms 未返回；第一事务 rollback 后第二事务取得同锁并读到 mod 9001；两连接仅共享 advisory key，不共享临时表 |
| Placement 冲突分类 | `TestImportedPlacementConflictTargetOnlyIgnoresSameResourceIntegration` | 包含于目标组 | 0 | PASS：`(version_id,resource_id)` 重入 RowsAffected=0；相同 section ordinal 与相同 placement identity 的不同资源均返回 SQLSTATE 23505，不再被无目标 handler 吞掉 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | Test 墙钟 14.731s；Vet 10.980s；Build 12.124s | 0 | PASS：全仓所有包通过；HTTP 1.120s、queue 3.810s、antiabuse 3.154s、serverprobe 3.227s；Vet/Build 无错误 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.582s；前端 0.323s | 0 | PASS：无空白错误；既有换行转换提示被静默过滤；本批无前端代码变化 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 普通门禁 0.968s | 1 / 0 | 449 唯一 ID 与分类精确；82 CLOSED / 367 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；普通总门禁 509 项仅因其余 Finding/最终严重度开放，BUG-017 独立关闭 |

锁位于 `syncImportedResourcesToContentVersionTx` 顶部并早于第一个 binding 写入，因此 export Worker、embedded import 和管理员 activation 三个生产调用方都覆盖完整同步事务；不同 version 使用不同 key，不建立全局串行瓶颈。`FOR UPDATE` 还保证锁后读取的是仍为 active 的权威 version 行。

本批只关闭并发 ordinal/placement 静默冲突。BUG-018 的来源集合差异撤销与字段删除、REUSE-001 的 kind/loot 映射重复仍保持 OPEN；没有以串行化掩盖它们。本批不改变 Race/GCC、generation 100 完整空库、真实 OSS 或 TEST-037 外部门禁。

## 2026-08-21：BUG-018 / REUSE-001 来源范围差异与分类单一权威

环境：Windows amd64、Go 1.26.4，权威 Schema 从 generation 100 升至 101。行为测试在远程 generation 85 连接上创建 session-local shadow tables，覆盖新 detail provenance/scope、localizations、sections、placements、revisions/snapshots/media；没有永久 DDL、数据库重置、OSS/NATS/Redis 调用或远程内容修改。完整 generation 101 空库仍因业务账号无 CREATEDB、维护库 pg_hba 拒绝而未执行。

| 检查 | 实际命令 | 耗时 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 来源差异、Schema 与映射目标组 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database ./internal/httpapi -run 'Test(ImportedResourceProjectionHasScopedOwnership&#124;ImportedContentSyncReconcilesOnlyItsSourceScope&#124;ImportedResourceClassificationExpressions&#124;ImportedProjectionReconciliation&#124;ImportedSectionAndPlacement)' -count=1` | 墙钟 6.537s；database 0.127s、HTTP 2.944s | 0 | PASS：generation 101 scope/check/index、a1+b1→a2 生命周期、19 kind/8 loot 执行和单一 literal 源码闭集全部运行，无 skip |
| 删除、字段删除与重命名 | `TestImportedContentSyncReconcilesOnlyItsSourceScopeIntegration` | 包含于目标组 | 0 | PASS：alpha:old_name detail archived 且 placement=0；alpha:new_name active 且 placement=1；keep definition 从 `{keep,removed}` 精确变 `{keep,added}`，旧 icon 700 变 null，import zh locale 被删除 |
| Namespace 与人工事实隔离 | 同一真实 PG case | 包含于目标组 | 0 | PASS：只激活 alpha/a2 时 beta resource 保持 active、revision=b1、placement=1；manual detail 定义保持 `manual:true` 且无 import placement；fr-FR human locale 保留，en-us import locale替换为 New |
| 分类单一权威 | `TestImportedResourceClassificationExpressionsCoverAllMappingsIntegration`、`TestImportedSectionAndPlacementShareClassificationAuthority` | 包含于目标组 | 0 | PASS：item/block、其余 Minecraft builtin、四种 Mekanism chemical 共 19 kind 映射正确；fishing/blocks/chests/entities/archaeology/equipment/gameplay/other 八类正确；kind/loot CASE literal 各只一份 |
| 后端全仓 / Vet / 构建 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | Test 墙钟 9.150s；Vet 6.001s；Build 5.885s | 0 | PASS：全仓所有包通过；HTTP 1.079s、queue 3.779s、antiabuse 3.179s、serverprobe 2.978s；Vet/Build 无错误 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.619s；前端 0.352s | 0 | PASS：无空白错误；既有换行转换提示被静默过滤；本批无前端/API代码变化 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 普通门禁 0.969s | 1 / 0 | 449 唯一 ID 与分类精确；84 CLOSED / 365 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；普通总门禁 507 项仅因其余 Finding/最终严重度开放，本批一个 High、一个 Medium 独立关闭 |

generation 101 的 CHECK 强制 manual detail 没有 import scope、import detail 必须保存 namespace/kind/revision；manual reserve/publish 会显式清 scope。reconcile 只以 incoming revision 明确声明的 scope 为差异边界，先归档缺失 importer-owned detail，再精确替换当前 definition/media/locales；不能因 canonical namespace 相似就跨来源删除。

root section discovery 与最终 placement 不再各持一份 kind CASE；loot child discovery 与 placement 也不再各持一份 path/category 规则。两消费者从同一编译期 SQL expression 生成，alias helper 只允许内部 `resource`/`snapshot`。本批不改变 Race/GCC、真实 OSS 或 TEST-037 状态，也不把 session-local generation 101 形状验证冒充完整空库安装。

## 2026-08-21：BUG-020 GitHub 加载器 token 边界

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 加载器边界回归 | `go test ./internal/httpapi -run 'Test(LoadersFromText\|ExternalMetadataNormalization)' -count=1` | 墙钟 2.601s；包 0.158s | 0 | PASS：NeoForge 紧凑/连字符/分词均只返回 NeoForge；独立 Forge 正确返回 Forge；显式同时出现保留两者；Fabric/Quilt 明确形式正常；`forged`/`quilted` 不误命中 |
| 后端全仓 | `go test ./... -count=1` | 墙钟 7.361s | 0 | PASS：所有包通过；无测试缓存 |
| 静态检查 / 构建 | `go vet ./...`；`go build ./...` | Vet 2.131s；Build 3.322s | 0 / 0 | PASS：无错误 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.382s；前端 0.185s | 0 / 0 | PASS：无空白错误；仅既有 LF→CRLF 工作树提示；本批无前端代码变化 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 0.777s；开放期 0.758s | 1 / 0 | PASS：449 唯一 ID 与分类精确；85 CLOSED / 364 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁仅因其余 Finding 和最终严重度开放按设计退出 1，BUG-020 独立关闭 |

实现把 GitHub topics/README 统一切为字母数字 token；`neo forge` 会作为一个复合 loader 名称消费两个 token，只有另一个独立 `forge`/`minecraftforge` 才声明 Forge。没有变更数据库、API 或前端，也没有把 ARCH-012、BUG-073/074/121 的相邻导入语义问题计入本次关闭。

## 2026-08-21：ARCH-012 / BUG-073 / BUG-074 / BUG-121 外部导入事实边界

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| BUG-121 修复前红测 | `go test ./internal/httpapi -run TestSimpleProjectProviderFreeTextDoesNotCreateStructuredClassification -count=1` | 墙钟 8.666s；包 1.281s | 1 | 预期 RED：Modrinth 与 CurseForge 两条真实 httptest 链都被正文 `32x` 错改 resolution |
| ARCH-012 修复前红测 | `go test ./internal/httpapi -run TestModImportSecondaryMetadataFailuresAreVisible -count=1` | 墙钟 8.261s；包 1.351s | 1 | 预期 RED：team/description 的 404、429、5xx、非法 JSON、超限及 README 429/5xx/超限共 13 项全部被吞掉 |
| 外部导入聚焦回归 | `go test ./internal/httpapi -run 'Test(LoadersFromText\|ModImportSecondary\|SimpleProjectProvider\|SimpleProjectSecondary\|ModpackSecondary\|ExternalModpack)' -count=1` | 墙钟 2.681s；包 0.241s | 0 | PASS：主模组/简单项目/整合包失败传播、正文隔离、两平台乱序正式主包选择、未知语言 final 拒绝均通过 |
| 后端全仓 / Vet / Build | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | Test 11.245s；Vet 3.208s；Build 4.153s | 0 / 0 / 0 | PASS：全仓所有包通过；HTTP 1.112s、queue 3.676s、antiabuse 3.141s、serverprobe 3.114s |
| 前端类型 / 测试 | `pnpm typecheck`；`pnpm test` | Typecheck 最终 3.398s；Test 0.890s | 0 / 0 | PASS：类型合同通过；Node 9/9 tests，272.348ms，无失败/跳过 |
| 前端 lint / production build | `pnpm lint`；`pnpm build` | Lint 最终 25.630s；Build 29.889s | 0 / 0 | PASS：ESLint 零告警；Next.js 16.2.11 production build、TypeScript 与 59/59 静态页面通过 |
| 源码闭集 / 双仓差异 | 零吞错/首元素/硬编码中文/正文分类检索；后端与前端 `git diff --check` | 后端 diff 0.375s；前端 0.185s | 0 / 0 | PASS：六类旧模式零残留；无空白错误，仅既有 LF→CRLF 提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 0.766s；开放期 0.756s | 1 / 0 | PASS：449 唯一 ID 与分类精确；89 CLOSED / 360 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，本批四个 Medium 独立关闭 |

当前没有 partial/warnings Schema，因此真实使用的次要字段失败必须令 Job failed；只有 typed GitHub README 404 表示资源明确不存在。简单项目分类只消费 `StructuredValues`。整合包 Job 以 `und` 请求用户确认正文语言，以只读 `importSelection` 展示确定性的正式主包；两者都不能绕过 final normalize 进入持久聚合。本批无 DDL，generation 保持 101。

## 2026-08-21：REUSE-004 共享 Provider Snapshot

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| Snapshot 行为与源码闭集 | `TestExternalProviderSnapshotsReturnValidatedSharedFacts`、`TestExternalProviderHTTPFlowsHaveOneSharedAuthority` 及前批跨供应商失败矩阵 | 聚焦包 0.242s；墙钟 2.642s | 0 | PASS：typed project/version/authors 与 class/slug/description 正确；`/project`、version、team、search、description literal 只在共享层；三个 Adapter 各平台恰调用一次 |
| 后端全仓 / Vet / Build | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | Test 11.353s；Vet 3.210s；Build 4.250s | 0 / 0 / 0 | PASS：全仓所有包通过；HTTP 1.091s、queue 3.659s、antiabuse 3.145s、serverprobe 3.071s |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.390s；前端 0.191s | 0 / 0 | PASS：无空白错误，仅既有 LF→CRLF 工作树提示；本批没有前端代码变化 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 0.769s；开放期 0.759s | 1 / 0 | PASS：449 唯一 ID 与分类精确；90 CLOSED / 359 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，REUSE-004 独立关闭 |

共享层只收敛 provider transport、响应完整性、身份验证、错误与作者规范化；Mod、Modpack、Simple Project 的类型、分类、兼容性和文件清单仍是独立领域 Adapter。旧 HTTP 流程与旧名 Wrapper 均已删除。本批无 Schema/API 变化，generation 保持 101。

## 2026-08-21：REUSE-005 前端共享任务轮询

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 轮询行为与源码闭集 | `pnpm test` 中 `job-polling.test.mts` 三项 | 总墙钟 1.305s；Node 313.196ms | 0 | PASS：queued→importing→ready 全量 progress、恰两次 1200ms delay；预取消返回 AbortError 且 load=0；领域文件只有一次状态机调用/终态集合，无第二 loop/旧 delay |
| 前端全量测试 | `pnpm test` | 同上 | 0 | PASS：12/12 tests，0 failed/cancelled/skipped/todo |
| 类型 / Lint / Production build | `pnpm typecheck`；`pnpm lint`；`pnpm build` | Type 3.016s；Lint 27.144s；Build 30.121s | 0 / 0 / 0 | PASS：TypeScript 与 ESLint 零错误/告警；Next.js 16.2.11 编译、TypeScript 与 59/59 静态页通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.380s；前端 0.183s | 0 / 0 | PASS：无空白错误，仅既有 LF→CRLF 工作树提示；本批无后端代码变化 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 0.782s；开放期 0.768s | 1 / 0 | PASS：449 唯一 ID 与分类精确；91 CLOSED / 358 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，REUSE-005 独立关闭 |

公开 `waitForModExportJob`/`waitForCatalogImportJob` 的语义名称、参数和路径保持；共享的是内部轮询状态机和 API 绑定。没有把终态不同的其他任务强行迁入。本批无 Schema/API 变化，generation 保持 101。

## 2026-08-21：REUSE-007 申请附件共享文件采集边界

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 两个调用方源码闭集 | `pnpm test` 中 `component-reuse.test.mts` 两项 | 总墙钟 1.536s；Node 390.368ms | 0 | PASS：编辑员申请与个人作者认领各只有一个 `FileDropZone`；两个业务文件均无 raw `type="file"`/作者 input ref；每个领域只有一个上传调用和一个稳定 OSS source |
| 前端全量测试 | `pnpm test` | 同上 | 0 | PASS：14/14 tests，0 failed/cancelled/skipped/todo |
| 类型 / Lint / Production build | `pnpm typecheck`；`pnpm lint`；`pnpm build` | Type 4.130s；Lint 30.016s；Build 30.001s | 0 / 0 / 0 | PASS：TypeScript 与 ESLint 零错误/告警；Next.js 16.2.11 编译、TypeScript 与 59/59 静态页通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 最终后端 0.985s；前端 0.767s | 0 / 0 | PASS：无空白错误，仅既有 LF→CRLF 工作树提示；本批后端仅更新治理文档 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.574s；开放期 1.699s | 1 / 0 | PASS：449 唯一 ID 与分类精确；92 CLOSED / 357 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，REUSE-007 独立关闭 |

两个入口传入空 accept，保持原有任意附件类型语义；共享层只收敛选择、拖拽、accept、禁用和 input reset。编辑员 10 个上限与作者认领 5 个/10 MiB 上限、各自顺序上传、通知、结果列表、source 和提交 DTO 都保持领域所有权。本批无 Schema/API 变化，generation 保持 101。

## 2026-08-21：BUG-125 / REUSE-006 严格本地化响应边界

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前源码红测 | `node --test app/_lib/localization-boundary.test.mts` | 墙钟 0.712s；Node 144.766ms | 1 | 预期 RED：第一个 catalog API 即缺少共享边界 import，四套局部 mapper/approved fallback 仍存在 |
| 严格边界行为与源码闭集 | `pnpm test` 中 `localization-boundary.test.mts` 三项 | 总墙钟 1.513s；Node 399.737ms | 0 | PASS：四种 localization status、三种 published status、五种 provenance 全量保留；未知/缺失 status、unknown provenance、空 locale 抛字段协议错误；四模块共享 parser 且无局部 mapper/fallback |
| 前端全量测试 | `pnpm test` | 同上 | 0 | PASS：17/17 tests，0 failed/cancelled/skipped/todo |
| 类型 / Lint / Production build | `pnpm typecheck`；`pnpm lint`；`pnpm build` | Type 8.993s；Lint 30.007s；Build 30.007s | 0 / 0 / 0 | PASS：TypeScript 与 ESLint 零错误/告警；Next.js 16.2.11 编译、TypeScript 与 59/59 静态页通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 1.085s；前端 0.849s | 0 / 0 | PASS：无空白错误；本批后端仅更新治理文档 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.335s；开放期 1.462s | 1 / 0 | PASS：449 唯一 ID 与分类精确；94 CLOSED / 355 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，本批两个 Low 独立关闭 |

显式 `reviewStatus` 值必须在其领域完整枚举中；mutation 不再用缺失或通用 `status` 推断 approved/pending。可选顶层字段缺失保持 undefined，而 localization 内状态、provenance 和 locale 是完整事实，损坏即令加载失败并由现有编辑器错误通道展示。本批无 Schema/API 变化，generation 保持 101。

## 2026-08-21：BUG-124 公开短链可重试错误终态

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前生命周期红测 | `node --test app/_lib/short-link-state.test.mts` | 墙钟 0.737s；Node 161.804ms | 1 | 预期 RED：页面没有 AbortController/signal/cleanup/retry，仍是只处理 ApiError 的 catch |
| 分类与源码闭集 | `pnpm test` 中 `short-link-state.test.mts` 两项 | 总墙钟 1.629s；Node 496.743ms | 0 | PASS：404/410→not_found，500/非 API→error；页面具有 controller signal、cleanup abort、导航前取消检查和 attempt retry；旧 catch 零残留 |
| 前端全量测试 | `pnpm test` | 同上 | 0 | PASS：19/19 tests，0 failed/cancelled/skipped/todo |
| 类型 / Lint / Production build | `pnpm typecheck`；`pnpm lint`；`pnpm build` | Type 12.547s；Lint 30.007s；Build 30.006s | 0 / 0 / 0 | PASS：TypeScript 与 ESLint 零错误/告警；Next.js 16.2.11 编译、TypeScript 与 59/59 静态页通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 1.053s；前端 0.830s | 0 / 0 | PASS：无空白错误；本批后端仅更新治理文档 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.363s；开放期 1.204s | 1 / 0 | PASS：449 唯一 ID 与分类精确；95 CLOSED / 354 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，BUG-124 独立关闭 |

短链成功响应还必须含非空 target；否则与网络/解析/其他 HTTP 错误一样进入可重试 error。只有非法 ID 和明确 404/410 显示不可访问。每轮请求绑定 effect controller，cleanup 后旧请求既不写状态也不发起导航。本批无 Schema/API 变化，generation 保持 101。

## 2026-08-21：LEGACY-021 删除旧全局资源 UI 路由

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前路由面红测 | `node --test app/_lib/route-surface.test.mts` | 墙钟 0.709s；Node 135.780ms | 1 | 预期 RED：旧 `app/catalog/resources/page.tsx` 仍存在，缺失预期 rejection |
| 路由面与前端全量测试 | `pnpm test` | 墙钟 1.616s；Node 529.388ms | 0 | PASS：20/20 tests；后台 page 存在，旧公开 page 不存在 |
| 首次类型 / Build 缓存诊断 | `pnpm typecheck`；`pnpm build` | Type 3.766s；Build 26.528s | 1 / 1 | 预期缓存失败：两份 `.next/**/validator.ts` 仍引用已删除 page；精确删除这两个可再生文件后重跑，不触碰源码/其他缓存 |
| 重建 / 类型 / Lint | `pnpm build`；`pnpm typecheck`；`pnpm lint` | Build 29.385s；Type 15.158s；Lint 27.293s | 0 / 0 / 0 | PASS：Next.js 16.2.11 编译/类型通过；58/58 静态页，route manifest 有 `/admin/global-resources` 且无 `/catalog/resources`；ESLint 零错误/告警 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 1.095s；前端 0.848s | 0 / 0 | PASS：无空白错误；本批后端仅更新治理文档 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.256s；开放期 1.381s | 1 / 0 | PASS：449 唯一 ID 与分类精确；96 CLOSED / 353 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，LEGACY-021 独立关闭 |

删除的是未发布 UI redirect；同名 `/api/v1/catalog/resources` catalog 数据 API 保持。仓库导航原已使用 `/admin/global-resources`，无调用方迁移。两份 stale validator 是 ignored build artifact，删除后已由成功 build 按当前路由重建。本批无 Schema/API 变化，generation 保持 101。

## 2026-08-21：DEAD-016 删除首页死翻译键

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前死键红测 | `node --test app/_lib/dead-config.test.mts` | 墙钟 0.707s；Node 147.346ms | 1 | 预期 RED：en-US home block 首先命中旧 subtitle/activityHint；两份主字典仍含四个失真叶子值 |
| 字典闭集与前端全量测试 | `pnpm test` | 墙钟 1.705s；Node 571.291ms | 0 | PASS：21/21 tests；两份主字典 home block 均无旧键，当前 `heroDescription` 等键保留 |
| 类型 / Lint / Production build | `pnpm typecheck`；`pnpm lint`；`pnpm build` | Type 12.610s；Lint 30.010s；Build 30.009s | 0 / 0 / 0 | PASS：TypeScript 与 ESLint 零错误/告警；Next.js 16.2.11 编译、TypeScript 与 58/58 静态页通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 1.064s；前端 0.862s | 0 / 0 | PASS：无空白错误；本批后端仅更新治理文档 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.276s；开放期 1.401s | 1 / 0 | PASS：449 唯一 ID 与分类精确；97 CLOSED / 352 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，DEAD-016 独立关闭 |

两个旧键无第一方调用且描述“真实数据尚未接入”，与当前首页实现冲突。en-US/zh-CN 同步删除后，其他基于 spread 的语言和后台 flatten 翻译目录一起收敛；没有建立旧键 alias。本批无 Schema/API/UI 输出变化，generation 保持 101。

## 2026-08-21：LEGACY-022 通知 Outbox 接受阶段文案

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前阶段语义红测 | `node --test app/_lib/dead-config.test.mts` | 墙钟 0.692s；Node 143.809ms | 1 | 预期 RED：中英文通知 queued 文案都命中 NATS，错误声称 dispatcher 已发布 |
| 文案闭集与前端全量测试 | `pnpm test` | 墙钟 1.700s；Node 572.966ms | 0 | PASS：22/22 tests；两份 notification block 的 queued 文案均无 NATS，且明确 reliable/可靠投递阶段 |
| 类型 / Lint / Production build | `pnpm typecheck`；`pnpm lint`；`pnpm build` | Type 3.841s；Lint 30.150s；Build 30.295s | 0 / 0 / 0 | PASS：TypeScript 与 ESLint 零错误/告警；Next.js 16.2.11 编译、TypeScript 与 58/58 静态页通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 1.037s；前端 0.809s | 0 / 0 | PASS：无空白错误；本批后端仅更新治理文档 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.425s；开放期 1.271s | 1 / 0 | PASS：449 唯一 ID 与分类精确；98 CLOSED / 351 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，LEGACY-022 独立关闭 |

POST 202 与后端可靠链路未改：当前可证明事实仍是 PostgreSQL Outbox 接受，NATS 发布由后续 dispatcher 完成。页面架构说明仍可描述最终 NATS 通道，只有本次成功提示收敛到准确阶段。本批无 Schema/API 变化，generation 保持 101。

## 2026-08-21：STYLE-009 Mod 资料管理登录身份文案

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前权限来源红测 | `node --test app/_lib/dead-config.test.mts` | 墙钟 0.629s；Node 151.609ms | 1 | 预期 RED：中英文提示都命中 owner/所有者，且没有完整列出三类真实能力来源 |
| 定向与前端全量测试 | bundled Node `--test app/_lib/dead-config.test.mts`；bundled Node 全量 `--test` | 定向 0.719s；全量 1.128s，Node 585.798ms | 0 / 0 | PASS：定向 3/3、全量 23/23；两份 `modContent` block 无 owner/所有者，且 verified developer/project editor/administrator 与中文对应项齐全 |
| 类型 / Lint / Production build | bundled Node 调用 `tsc --noEmit`、ESLint 与 `next build` | Type 3.302s；Lint 30.002s；Build 30.002s | 0 / 0 / 0 | PASS：TypeScript 与 ESLint 零错误/告警；Next.js 16.2.11 编译、TypeScript 与 58/58 静态页通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 1.104s；前端 0.859s | 0 / 0 | PASS：无空白错误；仅既有 LF→CRLF 转换提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.438s；开放期 1.279s | 1 / 0 | PASS：449 唯一 ID 与分类精确；99 CLOSED / 350 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，STYLE-009 独立关闭 |

本批只收敛未登录提示里的身份说明；登录后真实许可仍由后端 capability 判断，拒绝分支保持不变。无 Schema/API/路由变化，generation 保持 101。

## 2026-08-21：BUG-123 根 HTML 与 UI locale 同源

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前根语言红测 | bundled Node `--test app/_lib/ui-locale.test.mts` | 墙钟 0.616s；Node 143.310ms | 1 | 预期 RED：根布局仍精确命中固定 `<html lang="zh-CN">`，没有请求可见 locale |
| 定向 locale 回归 | bundled Node `--test app/_lib/ui-locale.test.mts` | 墙钟 0.675s；Node 149.327ms | 0 | PASS：2/2；根布局/Provider 共用 Cookie authority，八语言/alias/Cookie 往返与非法值拒绝通过 |
| 前端全量测试 / 类型 | bundled Node 全量 `--test`；bundled Node 调用 `tsc --noEmit` | Test 1.271s，Node 673.087ms；Type 11.975s | 0 / 0 | PASS：25/25 tests；TypeScript 合同通过 |
| Lint / Production build | bundled Node 调用 ESLint 与 `next build` | Lint 30.007s；Build 30.008s | 0 / 0 | PASS：ESLint 零错误/告警；Next.js 16.2.11 编译、TypeScript、58/58 页面生成通过；robots/sitemap 静态，其余共享根请求路由按设计为 dynamic |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 1.090s；前端 0.866s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.400s；开放期 1.254s | 1 / 0 | PASS：449 唯一 ID 与分类精确；100 CLOSED / 349 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，BUG-123 独立关闭 |

首屏服务端正文与 `<html lang>` 现在由同一个标准 locale 驱动，客户端本标签/跨标签切换最终也只从 Cookie 读取事实。按请求动态渲染是本次显式记录的架构成本；无后端 Schema/API 变化，generation 保持 101。

## 2026-08-21：BUG-128 评论关注精确静音截止

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前固定 24h 红测 | bundled Node `--test app/_lib/comment-watch-mute.test.mts` | 墙钟 0.655s；Node 140.227ms | 1 | 预期 RED：组件精确命中 `mutedUntil ? "24h"`，任意有限截止都被伪装成 24 小时 |
| 定向静音状态回归 | bundled Node `--test app/_lib/comment-watch-mute.test.mts` | 墙钟 0.717s；Node 184.916ms | 0 | PASS：2/2；1h/7d 保留各自绝对截止，forever/expired/invalid 分支稳定，旧推断零残留 |
| 前端全量测试 / 类型 | bundled Node 全量 `--test`；bundled Node 调用 `tsc --noEmit` | Test 1.303s，Node 728.078ms；Type 12.261s | 0 / 0 | PASS：27/27 tests；TypeScript 合同通过 |
| Lint / Production build | bundled Node 调用 ESLint 与 `next build` | Lint 30.002s；Build 30.001s | 0 / 0 | PASS：ESLint 零错误/告警；Next.js 16.2.11 编译、TypeScript 和 58/58 页面生成通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 1.128s；前端 0.917s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.454s；开放期 1.325s | 1 / 0 | PASS：449 唯一 ID 与分类精确；101 CLOSED / 348 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，BUG-128 独立关闭 |

后端列表/详情已提供足够的绝对静音事实，本批不新增重复 `muteMode` 字段。有限当前状态与相对时长写命令在 UI 中明确分离；无 Schema/API 变化，generation 保持 101。

## 2026-08-21：BUG-129 版本选择器禁用项统一边界

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前组切换红测 | bundled Node `--test app/_lib/minecraft-version-selection.test.mts` | 墙钟 0.635s；Node 146.499ms | 1 | 预期 RED：组件没有 disabled-aware 共享边界，`chooseGroup` 仍直接增删完整 codes |
| 定向版本集合回归 | bundled Node `--test app/_lib/minecraft-version-selection.test.mts` | 墙钟 0.723s；Node 181.565ms | 0 | PASS：3/3；压缩标签与父级均保留禁用已选项，禁用未选项不被添加，顺序/组外值稳定 |
| 前端全量测试 | bundled Node 全量 `--test` | 墙钟 1.448s；Node 832.736ms | 0 | PASS：30/30 tests，无失败/跳过 |
| Type / Lint / Production build | bundled Node 调用 `tsc --noEmit`、ESLint 与 `next build` | Type 最终 13.030s；Lint 30.016s；Build 30.015s | 0 / 0 / 0 | PASS：TypeScript 与 ESLint 零错误/告警；Next.js 16.2.11 编译和 58/58 页面生成通过 |
| 并行门禁调度复核 | 首次 Type 与 `next build` 同时启动 | Type 4.065s | 1 | 非代码故障：build 正在重建 `.next/types` 时独立 tsc 瞬时缺少两个生成文件；build 完成后串行 Type 通过，不将该次调度竞争冒充绿测 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 1.116s；前端 0.880s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.444s；开放期 1.277s | 1 / 0 | PASS：449 唯一 ID 与分类精确；102 CLOSED / 347 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，BUG-129 独立关闭 |

三个交互入口最终共享同一个禁用集合变换；禁用状态不再只是视觉属性。本批无 Schema/API 变化，generation 保持 101。

## 2026-08-21：PERF-065 目录资源图标有限缓存

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前容量/接入红测 | bundled Node `--test app/_lib/catalog-resource-icon-cache.test.mts` | 墙钟 0.700s；Node 168.534ms | 1 | 预期 RED：2/2 均失败；通用 cache 不逐出 LRU，图标组件仍是永久模块 Map |
| 定向缓存回归 | bundled Node `--test app/_lib/catalog-resource-icon-cache.test.mts app/_lib/sticker-catalog-cache.test.mts` | 墙钟 0.763s；Node 210.058ms | 0 | PASS：5/5；LRU 容量、图标有限配置、TTL、显式失效、并发合并和失败重试全部通过 |
| 前端全量测试 / 类型 | bundled Node 全量 `--test`；bundled Node 调用 `tsc --noEmit` | Test 1.424s，Node 816.516ms；Type 8.389s | 0 / 0 | PASS：32/32 tests；TypeScript 合同通过 |
| Lint / Production build | bundled Node 调用 ESLint 与 `next build` | Lint 30.011s；Build 30.011s | 0 / 0 | PASS：ESLint 零错误/告警；Next.js 16.2.11 编译和 58/58 页面生成通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 1.105s；前端 0.902s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.403s；开放期 1.251s | 1 / 0 | PASS：449 唯一 ID 与分类精确；103 CLOSED / 346 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，PERF-065 独立关闭 |

相同资源+locale 仍合并并发请求，但成功快照最多保留 128 项、5 分钟；失败不缓存。无资源修订协议时不声称即时写后失效，无 Schema/API 变化，generation 保持 101。

## 2026-08-21：MAP-012 站点语言单一注册表

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前重复事实源红测 | bundled Node `--test app/_lib/content-language-registry.test.mts` | 墙钟 0.664s；Node 163.407ms | 1 | 预期 RED：`content-language.ts` 仍手写八项数组并从 client `i18n-provider` 导入 Locale |
| 定向语言注册表回归 | bundled Node `--test app/_lib/content-language-registry.test.mts app/_lib/ui-locale.test.mts` | 墙钟 0.745s；Node 215.980ms | 0 | PASS：3/3；内容列表从共享 codes 派生，根 locale/Cookie 与八项注册表行为保持 |
| 前端全量测试 / 类型 | bundled Node 全量 `--test`；bundled Node 调用 `tsc --noEmit` | Test 1.497s，Node 879.887ms；Type 12.124s | 0 / 0 | PASS：33/33 tests；派生 union、字典与 alias Record 类型完整 |
| Lint / Production build | bundled Node 调用 ESLint 与 `next build` | Lint 30.003s；Build 30.003s | 0 / 0 | PASS：ESLint 零错误/告警；Next.js 16.2.11 编译和 58/58 页面生成通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.997s；前端 0.818s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.286s；开放期 1.331s | 1 / 0 | PASS：449 唯一 ID 与分类精确；104 CLOSED / 345 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，MAP-012 独立关闭 |

站点 code、显示列表、类型和可编辑内容集合现在从一份注册表派生；Minecraft/BCP-47 外部别名继续保持协议独立。无 Schema/API 变化，generation 保持 101。

## 2026-08-21：OPS-022 前端公开部署配置闭集

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前示例放行红测 | bundled Node `--test app/_lib/deployment-env.test.mts` | 墙钟 0.680s；Node 166.269ms | 1 | 预期 RED：`.gitignore` 的 `.env*` 吞掉示例，测试未找到 `!.env.example` |
| 定向配置合同回归 | bundled Node `--test app/_lib/deployment-env.test.mts` | 墙钟 0.570s；Node 146.697ms | 0 | PASS：4/4；开发默认、生产必填/规范化/拒绝矩阵、五项示例与 README 构建期合同、无凭据约束通过 |
| 前端全量测试 / 类型 | bundled Node 全量 `--test`；bundled Node 调用 `tsc --noEmit` | Test 1.252s，Node 774.947ms；Type 2.936s | 0 / 0 | PASS：37/37 tests；TypeScript 合同通过 |
| Lint / 缺配置负向 build | bundled Node 调用 ESLint；清除三项 URL 环境后 `next build` | Lint 21.863s；Build 1.020s | 0 / 1 | PASS：ESLint 零错误/告警；production config 加载因缺 `NEXT_PUBLIC_API_BASE_URL` 在生成产物前按预期失败 |
| 合法配置 Production build | 设置 task-scoped API/SITE/Yggdrasil HTTPS 示例后 bundled Node 调用 `next build` | 墙钟 25.565s；编译 8.7s、TypeScript 11.3s | 0 | PASS：Next.js 16.2.11 编译、TypeScript 和 58/58 页面生成通过 |
| 生成产物配置消费 | 检查 `.next/server/app/robots.txt.body`、`sitemap.xml.body` 与 `.next/routes-manifest.json` | 2.4s + 0.7s | 0 / 0 | PASS：robots/sitemap 全部为 `https://www.example.test`；Yggdrasil header 精确为 `https://auth.example.test/api/yggdrasil/` |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.862s；前端 0.874s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.234s；开放期 1.319s | 1 / 0 | PASS：449 唯一 ID 与分类精确；105 CLOSED / 344 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，OPS-022 独立关闭 |

生产构建现在没有 API/SITE 的隐式默认；可选 Yggdrasil 与 Iconfont 仍保持真正可选，但配置即严格验证。无后端 Schema/API 变化，generation 保持 101。

## 2026-08-21：BUG-130 作者目录与选择器 cursor 可达性

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前前后端分页红测 | Go 定向 `TestCreatorCatalogExposesBoundedCursorPagination`；bundled Node `--test app/_lib/creator-pagination.test.mts` | Go 墙钟 8.124s；Node 墙钟 0.641s、137.031ms | 1 / 1 | 预期 RED：后端无 cursor 文件/协议，前端无共享分页边界，目录/选择器仍固定 100/60 |
| 定向 cursor 与 UI 回归 | Go 三项 cursor/source tests；bundled Node 三项 creator pagination tests | Go 墙钟 7.703s；Node 最终墙钟 0.668s、131.938ms | 0 / 0 | PASS：SQL/index cursor scope、tuple predicate、非法数值/跨查询/身份/页长拒绝；共享 path/merge 与两 UI 入口闭集通过 |
| 真实 PostgreSQL 两页与规模计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCreatorCatalogKeysetCursorTraversesEverySQLSortIntegration$' -count=1 -v` | 墙钟 12.610s；Go 6.134s | 0 | PASS：临时影子表 11 种 SQL 排序各跨两页无重复/漏下一项，跨 kind cursor 400；100k name keyset 命中 `creators_kind_review_status_lower_id_idx`，40 行执行 0.137ms |
| 既有公开作者/团队查询 | DB integration 的 `TestPublicCatalogCoreSortQueriesIntegration/(authors&#124;teams)` | 墙钟 3.696s；Go 1.195s | 0 | PASS：真实开发库四种核心排序对 author/team 均返回 200；未修改远程 Schema/数据 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test 9.631s；Vet 5.793s；Build 7.071s；tidy 0.965s | 0 / 0 / 0 / 0 | PASS：全仓测试与静态/构建/模块闭集通过 |
| 前端首次 Lint 反馈 | bundled Node 调用 ESLint | 墙钟 29.445s | 1 | 预期质量反馈：发现两处 effect 体同步清 `loadingMore`；移入 180ms 防抖回调，与首次加载状态同批更新后复测 |
| 前端全量测试 / Type / Lint / Production build | bundled Node 全量 `--test`、`tsc --noEmit`、ESLint；合法 HTTPS task-scoped env 下 `next build` | Test 1.416s、Node 850.488ms；Type 3.074s；Lint 22.466s；Build 26.783s | 0 / 0 / 0 / 0 | PASS：40/40 tests；Type/Lint 零错误/告警；Next.js 16.2.11 编译、TypeScript 与 58/58 页面生成通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 1.012s；前端 0.789s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.176s；开放期 1.280s | 1 / 0 | PASS：449 唯一 ID 与分类精确；106 CLOSED / 343 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，BUG-130 独立关闭 |

目录 counts 仍描述完整筛选集合，加载页只决定当前可见 items；前后端没有保留固定截断或 offset 兼容路径。无 DDL，generation 保持 101。

## 2026-08-21：BUG-131 Creator 资料与敏感关系能力拆分

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前跨层能力红测 | Go `TestCreatorProfileAndTeamMemberCapabilitiesAreSeparated`；bundled Node `--test app/_lib/creator-capabilities.test.mts` | Go 墙钟 7.420s、Go 1.171s；Node 墙钟 0.603s、Node 139.931ms | 1 / 1 | 预期 RED：详情只有 `canEdit`，profile editor 包含成员/职务控件并提交完整 snapshot，无独立成员端点、审计 action、页面或 payload 边界 |
| 定向能力、HTTP 与载荷回归 | Go capability/profile 拒绝/content reviewer 拒绝/成员规范化/source tests；bundled Node creator capability tests | Go 墙钟 7.996s、Go 1.442s；Node 墙钟 0.960s、Node 226.194ms | 0 / 0 | PASS：审核员不获三项写能力；profile 显式 members 在访问 DB 前 400；无成员权限在访问 DB 前 403；500 上限、复合重复、ID、职务及 160-byte title 失败关闭；前端资料白名单不含成员 |
| 真实 PostgreSQL 资料隔离与原子审计 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCreatorProfileAndAuditedTeamMemberUpdatesAreIsolatedIntegration$' -count=1 -v` | 最终墙钟 10.679s；Go 3.452s | 0 | PASS：session temp shadow table 中 profile 链接替换后原成员/title/status 不变；独立替换保存 author001→author002 before/after；给 audit 表增加 NOT VALID CHECK 强制失败后关系仍为 author002/Lead |
| 真实 PG 首轮质量反馈 | 同一集成测试的前两轮 | 墙钟 14.162s / 9.731s | 1 / 1 | 预期质量反馈：先发现审计排序引用不存在的 relation.id，再发现复用 actor placeholder 在 INSERT/CASE 中类型推导冲突；改为稳定复合排序和显式 bigint cast 后全绿，防止仅靠源码测试关闭 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | Test 12.486s；Vet 8.593s；Build 4.484s；tidy 0.704s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、编译和模块闭集通过 |
| 前端全量测试 / Type / Lint / Production build | bundled Node 全量 `--test`、`tsc --noEmit`、ESLint `--max-warnings=0`；合法 HTTPS task-scoped env 下 `next build` | Test 1.621s、Node 887.081ms；Type 3.000s；Lint 20.490s；Build 24.840s | 0 / 0 / 0 / 0 | PASS：42/42 tests；Type/Lint 零错误/告警；Next.js 16.2.11 编译、TypeScript、58/58 页面及 `/teams/[publicId]/members` 路由生成通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.951s；前端 0.771s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.278s；开放期 1.102s | 1 / 0 | PASS：449 唯一 ID 与分类精确；107 CLOSED / 342 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，BUG-131 独立关闭 |

资料修订、团队成员快照和全局职务创建现由三个服务端 capability 分别驱动；前端只渲染对应控制，关系审计与变更同事务。无 DDL，generation 保持 101，远程开发库未写入。

## 2026-08-21：BUG-132 项目作者关系审核队列可达性

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前前后端分页红测 | Go `TestProjectAuthorship*`；bundled Node `--test app/_lib/project-authorship-pagination.test.mts` | Go 墙钟 2.402s；Node 墙钟 0.588s、Node 121.358ms | 1 / 1 | 预期 RED：无 parser/cursor 协议，handler 仍 `limit 200`；前端无共享 path/merge 模块、cursor 或取消边界 |
| 定向 cursor / Schema / UI 回归 | Go project authorship unit/source tests；database Schema tests；bundled Node 三项 pagination tests；`tsc --noEmit` | HTTP Go 8.013s、Go 1.184s；DB 3.828s、Go 1.020s；Node 0.636s、Node 133.929ms；Type 11.787s | 0 / 0 / 0 / 0 | PASS：status/limit/身份/author-team 权限 scope、非法输入、DDL/handler 闭集、path 编码、稳定去重、Abort/generation 与类型合同通过 |
| 真实 PostgreSQL 超旧窗口遍历与规模计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestProjectAuthorshipQueueTraversesPastLegacyWindowIntegration$' -count=1 -v` | 墙钟 9.107s；Go 3.032s | 0 | PASS：session temp shadow table 的 205 条 pending 按 100/100/5 全部可达且零重复；跨 approved cursor 400；100k 命中 `idx_content_creator_bindings_review_page` Index Only Scan，100 行执行 0.088ms |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | Test 14.485s；Vet 9.576s；Build 5.091s；tidy 0.749s | 0 / 0 / 0 / 0 | PASS：generation 102 全仓测试、静态分析、编译和模块闭集通过 |
| 前端全量测试 / Type / Lint / Production build | bundled Node 全量 `--test`、`tsc --noEmit`、ESLint `--max-warnings=0`；合法 HTTPS task-scoped env 下 `next build` | Test 1.785s、Node 973.712ms；Type 3.070s；Lint 25.506s；Build 24.678s | 0 / 0 / 0 / 0 | PASS：45/45 tests；Type/Lint 零错误/告警；Next.js 16.2.11 编译、TypeScript 与 58/58 页面生成通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.808s；前端 0.821s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 1.296s；开放期 1.150s | 1 / 0 | PASS：449 唯一 ID 与分类精确；108 CLOSED / 341 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，BUG-132 独立关闭 |

四种授权关系状态现在共用一个有界、可继续的稳定位置协议；UI 只消费服务端 cursor，后端索引与排序同构。远端 generation 85 未写入，当前代码 generation 为 102。

## 2026-08-21：BUG-133 详情加载错误状态

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前共享边界红测 | bundled Node `--test app/_lib/project-detail-state.test.mts` | 墙钟 0.585s；Node 118.944ms | 1 | 预期 RED：缺少 `project-detail-state.mts` 和共享详情加载边界，三个入口仍分别吞掉非 404 异常 |
| 定向分类 / 调用方 / 类型回归 | bundled Node `--test app/_lib/project-detail-state.test.mts`；`tsc --noEmit` | Test 墙钟 0.8s、Node 117.960ms；Type 与同批命令合计 9.5s | 0 / 0 | PASS：3/3 覆盖 404 与 401/410/500/网络/未知分流、异常描述和三个调用方共享 Abort/retry 边界；类型合同通过 |
| 前端全量测试 | bundled Node 全量 `--test` | Node 726.240ms | 0 | PASS：48/48；既有 locale、cursor、缓存、轮询、文件选择和路由回归均通过 |
| 前端 Lint / Type / Production build | bundled Node ESLint `--max-warnings=0`；`tsc --noEmit`；合法 HTTPS task-scoped env 下 `next build` | Lint 18.882s；Type 2.262s；Build 22.333s | 0 / 0 / 0 | PASS：零错误/告警；Next.js 16.2.11 production build 成功，58 个页面合同保持 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.380s；前端 0.204s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 0.975s；开放期 0.633s | 1 / 0 | PASS：449 唯一 ID 与分类精确；109 CLOSED / 340 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，BUG-133 独立关闭 |

三个公开项目详情现在共享显式错误状态；只有确定 404 显示不存在，其他故障不会再伪装成无限 loading。无 Schema/API 变化，generation 保持 102，远程开发库未写入。

## 2026-08-21：BUG-134 通用审核状态默认文案

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前状态展示红测 | bundled Node `--test app/_lib/review-status-presentation.test.mts` | 墙钟 0.485s；Node 94.433ms | 1 | 预期 RED：缺少共享 presentation resolver；两个通用组件仍以 common edit/loading/confirm/cancel 作为四态默认文案 |
| 定向解析 / 组件 / 字典回归 | bundled Node `--test app/_lib/review-status-presentation.test.mts`；`tsc --noEmit` | Test 墙钟 0.7s、Node 114.813ms；Type 与同批命令合计 8.9s | 0 / 0 | PASS：3/3 覆盖四个合法状态、五类协议漂移、两组件共享边界/action copy 零残留及 en-US/zh-CN key 完整；类型合同通过 |
| 前端全量测试 / Lint / Type / Production build | bundled Node 全量 `--test`、ESLint `--max-warnings=0`、`tsc --noEmit`；合法 HTTPS task-scoped env 下 `next build` | Test 0.845s；Lint 18.644s；Type 1.937s；Build 21.041s | 0 / 0 / 0 / 0 | PASS：51/51；零 lint 告警/类型错误；Next.js 16.2.11 production build 与 58 个页面合同通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.348s；前端 0.194s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 0.645s；开放期 0.645s | 1 / 0 | PASS：449 唯一 ID 与分类精确；110 CLOSED / 339 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，BUG-134 独立关闭 |

两个通用审核组件现在由完整状态合同驱动；调用方不传覆盖标签仍准确，协议漂移失败可见。无 Schema/API 变化，generation 保持 102，远程开发库未写入。

## 2026-08-21：BUG-135 三类详情 Tab URL 往返

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前 URL 状态红测 | bundled Node `--test app/_lib/project-detail-tab.test.mts` | 墙钟 0.482s；Node 107.019ms | 1 | 预期 RED：缺少纯 URL helper 与共享 Hook；三个详情仍只特判初始 changelog 并以 selectedTab 保存点击状态 |
| 定向 URL / 三调用方 / 类型回归 | bundled Node `--test app/_lib/project-detail-tab.test.mts`；`tsc --noEmit` | Test Node 115.663ms；同批墙钟 3.1s | 0 / 0 | PASS：3/3 覆盖完整合法解析、非法回退、query/hash 保留、默认参数删除和三个调用方的共享 Hook/旧状态零残留；类型合同通过 |
| 前端全量测试 / Lint / Type / Production build | bundled Node 全量 `--test`、ESLint `--max-warnings=0`、`tsc --noEmit`；合法 HTTPS task-scoped env 下 `next build` | Test 0.880s；Lint 18.583s；Type 1.993s；Build 21.244s | 0 / 0 / 0 / 0 | PASS：54/54；零 lint 告警/类型错误；Next.js 16.2.11 production build 与 58 个页面合同通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.348s；前端 0.194s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 0.637s；开放期 0.630s | 1 / 0 | PASS：449 唯一 ID 与分类精确；111 CLOSED / 338 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，BUG-135 独立关闭 |

三个详情页现在由 URL 单一驱动 tab；刷新、分享和浏览器历史一致，其他 query/hash 不丢失。无 Schema/API 变化，generation 保持 102，远程开发库未写入。

## 2026-08-21：BUG-136 皮肤/蓝图多语言原子保存

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前前后端原子合同红测 | Go `TestNormalizeSkinAssetUpdate*`；bundled Node `--test app/_lib/localized-asset-update.test.mts` | Go 墙钟 2.188s；Node 墙钟 0.492s、Node 97.089ms | 1 / 1 | 预期 RED：skin update 无 defaultLocale/localizations 或规范化边界；前端无 snapshot helper 且仍为主体后逐语言串行 PUT |
| 定向请求规范化 / 前端单请求 / 类型回归 | Go 三项 skin update normalization tests；bundled Node 两项 asset snapshot tests；`tsc --noEmit` | Go 墙钟 7.5s、包测试 1.277s；Node 111.439ms、含 Type 墙钟 2.9s | 0 / 0 / 0 | PASS：完整 default locale、metadata-only 兼容、缺省默认语言拒绝、editable/nonempty snapshot 和两资产单 mutation 源码闭集通过 |
| 真实 PostgreSQL 两资产同成同败 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestLocalizedAssetSnapshotsCommitOrRollbackAsOneUnitIntegration$' -count=1 -v` | 墙钟 9.026s；测试 2.06s；包 3.259s | 0 | PASS：skin/blueprint 各自主体、默认 zh-CN 与 zh/en 两语言同事务提交；随后 CHECK 强制本地化失败，两者均完整保留上一版本 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | Test 14.015s；Vet 2.991s；Build 4.081s；tidy 0.371s | 0 / 0 / 0 / 0 | PASS：聚合修订、审核回放、旧 snapshot 兼容和模块闭集通过 |
| 前端全量测试 / Lint / Type / Production build | bundled Node 全量 `--test`、ESLint `--max-warnings=0`、`tsc --noEmit`；合法 HTTPS task-scoped env 下 `next build` | Test 0.941s；Lint 22.186s；Type 2.169s；Build 21.017s | 0 / 0 / 0 / 0 | PASS：56/56；零 lint 告警/类型错误；Next.js 16.2.11 production build 与 58 个页面合同通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.351s；前端 0.199s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 0.660s；开放期 0.631s | 1 / 0 | PASS：449 唯一 ID 与分类精确；112 CLOSED / 337 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，BUG-136 独立关闭 |

一次资产保存现在只产生一个聚合修订，立即发布和审核批准共用同一事务发布边界；失败不存在部分成功。无 DDL，generation 保持 102，远程开发库未写入。

## 2026-08-21：BUG-137 反滥用配置完整管理面

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前配置完整性红测 | Go `TestValidateSettingsRejectsEverySilentlyNormalizedGlobalBound`；bundled Node `--test app/_lib/anti-abuse-settings.test.mts` | Go 墙钟 2.991s、包 0.939s；Node 墙钟 0.531s、Node 100.702ms | 1 / 1 | 预期 RED：六类 Normalize-only 越界均被 Validate 接受；无前端完整设置 validator，七个持久字段未形成可验证控件合同 |
| 定向服务端边界 / UI 完整性 / 类型回归 | Go 两项 Validate tests；bundled Node 四项 anti-abuse settings tests；`tsc --noEmit` | Go 墙钟 3.217s、包 0.975s；Node 114.978ms、含 Type 墙钟 4.217s | 0 / 0 / 0 | PASS：真实全局上限、trusted level、阈值/账号依赖、秒/分钟窗口、完整字段渲染与类型合同通过 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | Test 12.221s；Vet 3.014s；Build 3.943s；tidy 0.371s | 0 / 0 / 0 / 0 | PASS：反滥用评估、Normalize/Validate、管理 API 和模块闭集通过 |
| 前端全量测试 / Lint / Type / Production build | bundled Node 全量 `--test`、ESLint `--max-warnings=0`、`tsc --noEmit`；合法 HTTPS task-scoped env 下 `next build` | Test 1.022s；Lint 21.431s；Type 2.280s；Build 21.134s | 0 / 0 / 0 / 0 | PASS：60/60；零 lint 告警/类型错误；Next.js 16.2.11 production build 与 58 个页面合同通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.431s；前端 0.289s | 0 / 0 | PASS：无空白错误；既有 LF/CRLF 工作树提示不属于差异错误 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 0.793s；开放期 0.787s | 1 / 0 | PASS：449 唯一 ID 与分类精确；113 CLOSED / 336 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，BUG-137 独立关闭 |

反滥用后台现在完整呈现持久化权威配置，前后端对范围与依赖失败关闭；不存在只随保存回传却无法管理的字段。无 Schema/DTO 变化，generation 保持 102，远程开发库未写入。

## 2026-08-21：BUG-138 项目工作台详情身份绑定

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前详情状态红测 | bundled Node `--test app/_lib/admin-project-detail-state.test.mts` | 墙钟 0.199s；Node 102.715ms | 1 | 预期 RED：缺少 ID/区间绑定状态模块，旧组件在选择变化和读取失败后仍保存并渲染上一成功 DTO |
| 定向身份/区间/失败/取消与类型回归 | bundled Node 四项 admin project detail state tests；`tsc --noEmit`；目标 ESLint | Tests 墙钟 0.218s、Node 117.566ms；Type 3.521s；Lint 3.239s | 0 / 0 / 0 | PASS：ready 可见性、切换/失败清除、错配响应失败关闭、Abort/retry/显式状态源码合同通过 |
| 前端全量测试 / Lint / Type / Production build | bundled Node 全量 `--test`、ESLint `--max-warnings=0`、`tsc --noEmit`；合法 HTTPS task-scoped env 下 `next build` | Test 1.055s；Lint 18.907s；Type 2.242s；Build 21.248s | 0 / 0 / 0 / 0 | PASS：64/64；零 lint 告警/类型错误；Next.js 16.2.11 production build 与 58 个页面合同通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.439s；前端 0.303s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 0.795s；开放期 0.781s | 1 / 0 | PASS：449 唯一 ID 与分类精确；114 CLOSED / 335 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，BUG-138 独立关闭 |

工作台选择、请求、响应与图表现在共享 projectID+days 身份；加载、失败和错配响应均不能泄漏上一快照。无 Schema/API 变化，generation 保持 102，远程开发库未写入。

## 2026-08-21：BUG-139 服务器审核队列完整可达

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前分页合同红测 | Go server review cursor/source/schema tests；bundled Node `server-review-pagination.test.mts` | Go 墙钟 2.973s、database 0.842s；Node 墙钟 0.201s、Node 99.163ms | 1 / 1 | 预期 RED：缺 parser/cursor/index，generation 仍 102；前端无分页 helper，组件只请求一次固定窗口 |
| 定向后端 cursor/schema 与前端路径/合并/取消 | Go HTTPAPI+database 定向 tests；bundled Node 3 tests；`tsc --noEmit` | Go 墙钟 7.647s、包 1.382s/0.646s；Node 118.860ms、含 Type 墙钟 9.828s | 0 / 0 / 0 | PASS：status/limit scope、非法输入、索引源码、路径编码、去重追加和请求代次合同通过 |
| 真实 PostgreSQL 三状态遍历 / 100k 计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestServerReviewKeysetCursorTraversesEveryStatusIntegration$' -count=1 -v` | 墙钟 7.989s；测试 1.390s；包 2.561s；100k 执行 0.159ms | 0 | PASS：pending/approved/rejected 各 205 条按 100/100/5 全量且无重复；100k 由 `idx_minecraft_servers_review_page` Index Scan 取 101 行，无服务器表 Seq Scan |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | Test 13.972s；Vet 9.319s；Build 10.272s；tidy 0.371s | 0 / 0 / 0 / 0 | PASS：generation 103、cursor、既有审核/证明接口与模块闭集通过 |
| 前端全量测试 / Lint / Type / Production build | bundled Node 全量 `--test`、ESLint `--max-warnings=0`、`tsc --noEmit`；合法 HTTPS task-scoped env 下 `next build` | Test 1.177s；Lint 23.153s；Type 2.014s；Build 21.533s | 0 / 0 / 0 / 0 | PASS：67/67；零 lint 告警/类型错误；Next.js 16.2.11 production build 与 58 个页面合同通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端 0.507s；前端 0.332s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 0.775s；开放期 0.761s | 1 / 0 | PASS：449 唯一 ID 与分类精确；115 CLOSED / 334 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding/最终严重度开放按设计退出 1，BUG-139 独立关闭 |

服务器审核三种状态不再被静默截断；分页、前端继续加载与数据库顺序使用同一元组。generation 103 仅存在于代码/空库合同，远程 generation 85 未迁移或写入；PERF-067 的批量装配仍开放。

## 2026-08-21：BUG-140 表情临时上传可恢复生命周期

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前生命周期红测 | Go temporary sticker upload source/lifecycle tests；bundled Node `sticker-upload-lifecycle.test.mts` | Go 墙钟 1.732s；Node 墙钟 0.212s、Node 113.449ms | 1 / 1 | 预期 RED：无临时 source classifier、精确 discard 路由或过期清理；前端元数据失败后无共用补偿边界 |
| 定向来源/路由/过期闭集与前端三分支 | Go 2 tests；bundled Node 4 tests；`tsc --noEmit` | Go 墙钟 6.627s、包 1.170s；Node 墙钟 0.206s、Node 115.912ms；Type 前批同实现通过 | 0 / 0 / 0 | PASS：closed source、双权限 discard/worker 源码、成功/上传失败/mutation失败/cleanup失败及两入口唯一 source 通过 |
| 真实 PostgreSQL 显式丢弃 / 到期补偿 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestTemporaryStickerUploadsAreExplicitlyDiscardedOrExpiredIntegration$' -count=1 -v` | 墙钟 3.592s；测试 1.550s；包 1.671s | 0 | PASS：legacy 过期和显式新代次均 deleted+各一条 Outbox；新文件、其他来源、被引用文件保持 active；跨 owner discard 404 且不改状态 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | Test 13.069s；Vet 9.215s；Build 10.271s；tidy 0.362s | 0 / 0 / 0 / 0 | PASS：维护批次、并发锁、既有 sticker 派生/删除与 OSS Outbox 闭集通过 |
| 前端全量测试 / Lint / Type / Production build | bundled Node 全量 `--test`、ESLint `--max-warnings=0`、`tsc --noEmit`；合法 HTTPS task-scoped env 下 `next build` | Test 1.220s；Lint 23.034s；Type 1.999s；Build 21.203s | 0 / 0 / 0 / 0 | PASS：71/71；零 lint 告警/类型错误；Next.js 16.2.11 production build 与 58 个页面合同通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 最终后端 0.435s；前端 0.291s | 0 / 0 | PASS：无空白错误；stderr 过滤后零输出 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格 0.643s；开放期 0.638s | 1 / 0 | PASS：449 唯一 ID 与分类精确；116 CLOSED / 333 OPEN，High 94 / Medium 165 / Low 49 / unresolved 141；严格总门禁只因其余 Finding 与最终严重度开放按设计退出 1，BUG-140 独立关闭 |

表情原始上传从“活动即完成”改为唯一、有期限、可显式放弃的临时代次；成功后仍由现有聚合事务消费，失败和崩溃均最终进入同一可靠删除 Outbox。无 DDL，generation 保持 103，远程 generation 85 未迁移或写入。

## 2026-08-21：BUG-141 公共蓝图库完整可达

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前分页合同红测 | Go blueprint cursor tests；bundled Node `blueprint-pagination.test.mts` | Go 墙钟 1.697s；Node 墙钟 0.208s、Node 104.213ms | 1 / 1 | 预期 RED：无蓝图 scope/keyset parser/cursor；前端无 loader/merge，组件仍固定 limit=60 且没有继续入口 |
| 定向 cursor、请求代次与类型/Lint | Go 2 cursor/metric tests；bundled Node 3 loader/merge/source tests；`tsc --noEmit`；目标 ESLint | Go 墙钟 6.691s、包1.152s；Node墙钟0.201s、Node113.044ms；Type 8.200s；Lint 3.325s | 0 / 0 / 0 / 0 | PASS：scope/身份/稳定键、严格 decimal、offset互斥、路径/去重、Abort/generation/继续加载源码闭集通过 |
| 真实 PostgreSQL 22种顺序完整遍历 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestBlueprintCatalogKeysetCursorTraversesEverySortBeyondSixtyIntegration$' -count=1 -v` | 墙钟18.302s；测试11.770s；包12.899s | 0 | PASS：MaxConns=1临时表125条，11 sort×asc/desc全部遍历且零遗漏/重复；终页空cursor；跨q复用400；同时证明主rows/配置/子查询无嵌套连接自锁 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | Test 12.232s；Vet 8.062s；Build 9.502s；tidy 0.402s | 0 / 0 / 0 / 0 | PASS：目录排序、可见性、详情/转换/审核与模块闭集通过 |
| 前端全量测试 / Lint / Type / Production build | bundled Node 全量 `--test`、ESLint `--max-warnings=0`、`tsc --noEmit`；合法HTTPS task-scoped env下 `next build` | Test 1.483s；Lint 19.046s；Type 2.080s；Build 20.988s | 0 / 0 / 0 / 0 | PASS：74/74；零lint告警/类型错误；Next.js 16.2.11 production build与58个页面合同通过 |
| 双仓差异空白 | 后端与前端分别 `git diff --check` | 后端0.485s；前端0.300s | 0 / 0 | PASS：无空白错误；既有LF/CRLF工作树提示不属于差异错误 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格0.701s；开放期0.643s | 1 / 0 | PASS：449唯一ID与分类精确；117 CLOSED / 332 OPEN，High94 / Medium165 / Low49 / unresolved141；严格总门禁只因其余Finding与最终严重度开放按设计退出1，BUG-141独立关闭 |

公开蓝图库不再把首60条伪装成完整集合；筛选、排序与可见性身份贯穿同一不透明游标，第一方显式遍历至终页。无DDL，generation保持103，远程generation85未迁移或写入。

## 2026-08-21：BUG-142 日志批量上传逐项恢复

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前批任务红测 | bundled Node `--test app/_lib/log-upload-batch.test.mts` | 墙钟0.196s；Node100.740ms | 1 | 预期RED：无批任务模块；旧组件在顺序循环中首个extension/upload异常直接退出，已完成ID不进入create或结果 |
| 定向分类/部分成功/重试与Type/Lint | bundled Node 5 tests；`tsc --noEmit`；目标ESLint | Tests墙钟0.218s、Node127.357ms；Type2.323s；Lint3.033s | 0 / 0 / 0 | PASS：全批先验分类、中间上传失败继续、transport失败ID复用、逐项失败复用、UI状态源码闭集通过 |
| 真实PostgreSQL 207顺序与幂等复用 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestFileLogShareBatchReturnsEveryItemAndReusesCompletedSourcesIntegration$' -count=1 -v` | 墙钟7.218s；测试0.530s；包1.665s | 0 | PASS：MaxConns=1临时表；`[ready,missing,ready]`两轮均返回三项、失败不遮蔽成功、两个ready publicCode原值复用且无需OSS读取 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | Test8.481s；Vet4.775s；Build4.538s；tidy0.381s | 0 / 0 / 0 / 0 | PASS：既有日志脱敏/207/复用、OSS、评论绑定与模块闭集通过 |
| 前端全量测试 / Lint / Type / Production build | bundled Node全量`--test`、ESLint`--max-warnings=0`、`tsc --noEmit`；合法HTTPS task-scoped env下`next build` | Test1.495s；Lint18.790s；Type2.122s；Build20.908s | 0 / 0 / 0 / 0 | PASS：79/79；零lint告警/类型错误；Next.js16.2.11 production build与58个页面合同通过 |
| 双仓差异空白 | 后端与前端分别`git diff --check` | 后端0.482s；前端0.319s | 0 / 0 | PASS：无空白错误；既有LF/CRLF工作树提示不属于差异错误 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 最终严格0.650s；开放期0.628s | 1 / 0 | PASS：449唯一ID与分类精确；118 CLOSED / 331 OPEN，High94 / Medium165 / Low49 / unresolved141；严格总门禁只因其余Finding与最终严重度开放按设计退出1，BUG-142独立关闭 |

日志批处理现在以每个文件名、阶段和uploadedFileId为事实；一个坏文件或网络异常不再抹掉已完成兄弟，重试只继续未完成项。无DDL/API变化，generation保持103，远程generation85未迁移或写入。

## 2026-08-21：BUG-143 资源选择器过滤与分页同集

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前权威排除红测 | Go exclusion parser/四端点source tests；bundled Node picker source test | Go墙钟1.742s；Node墙钟0.219s、Node118.052ms | 1 / 1 | 预期RED：无exclude parser/SQL；前端未传参数，仍只在命中当前页时过滤并减total |
| 定向parser/四端点接线与前端单一集合 | Go 2 tests；bundled Node 1 test；目标ESLint+`tsc --noEmit` | Go墙钟6.612s、包1.178s；Node墙钟0.207s、Node109.914ms；Lint+Type墙钟6.842s | 0 / 0 / 0 | PASS：slug/跨类型server标识边界、count/list谓词、非空排除禁索引，以及零containsExcluded/页后filter通过 |
| 真实PostgreSQL总数/跨页只读验证 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestProjectCatalogExclusionOwnsTotalAndEveryPageIntegration$' -count=1 -v` | 墙钟3.193s；测试1.110s；包1.232s；Mod子项0.920s | 0 | PASS：真实approved Mod使total恰减1，第一页/第二页total恒定且item不回流；整合包/简单项目/服务器无approved夹具而明确SKIP，未写远端数据 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | 最终Test11.748s；Vet7.903s；Build8.886s；tidy0.387s | 0 / 0 / 0 / 0 | PASS：四目录过滤、搜索索引降级、关联装配和模块闭集通过 |
| 前端全量测试 / Lint / Type / Production build | bundled Node全量`--test`、ESLint`--max-warnings=0`、`tsc --noEmit`；合法HTTPS task-scoped env下`next build` | Test1.562s；Lint18.761s；Type2.104s；Build21.561s | 0 / 0 / 0 / 0 | PASS：80/80；零lint告警/类型错误；Next.js16.2.11 production build与58个页面合同通过 |
| 双仓差异空白 | 后端与前端分别`git diff --check` | Backend0.445s；Frontend0.303s | 0 / 0 | PASS：零空白错误；仅既有Git LF到CRLF工作副本提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.703s；Allow-open0.633s | 1 / 0 | PASS：audit/ledger均精确449，119 CLOSED / 330 OPEN；allow-open通过；严格总门禁仅因330个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

资源选择器的total、类型边界、offset和items现在都从同一后端已排除集合派生；排除项位于哪一页不再改变分页事实。无DDL，generation保持103，远程generation85仅只读验证。

## 2026-08-21：BUG-144 合成表opaque definition普通编辑保真

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前数据丢失红测 | bundled Node roundtrip test；Go reconcile/PG wiring tests | Node墙钟0.209s、Node104.119ms；Go墙钟1.722s | 1 / 1 | 预期RED：前端helper不存在且仍创建空对象；后端无权威reconcile方法，无法阻止空覆盖 |
| 定向前后端合同 | bundled Node 3 tests；Go unit；`tsc --noEmit`与目标ESLint | Node墙钟0.208s、Node115.505ms；Go unit+diff墙钟2.380s；Type+Lint墙钟6.107s | 0 / 0 / 0 | PASS：任意嵌套对象深层克隆、空新建、editor source接线；省略/相同/冲突/新建注入四分支；零类型/Lint问题 |
| 真实PostgreSQL normalization往返 | 本地.env组装task-scoped DSN后`go test ./internal/httpapi -run '^TestCatalogRecipeDefinitionRoundTripIntegration$' -count=1 -v` | 最终墙钟6.998s；测试0.370s；包1.503s | 0 | PASS：MaxConns=1会话临时JSONB/模板/槽位；非空嵌套definition经实际normalize完整保留，陈旧空回显409分支通过；无远端持久写入 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test10.461s；Vet3.035s；Build3.935s；tidy0.359s | 0 / 0 / 0 / 0 | PASS：审核快照、发布、fingerprint、目录与全部模块闭集通过 |
| 前端全量测试 / Lint / Type / Production build | bundled Node全量`--test`、ESLint`--max-warnings=0`、`tsc --noEmit`；合法HTTPS task-scoped env下`next build` | Test1.314s；Lint21.169s；Type1.962s；Build21.204s | 0 / 0 / 0 / 0 | PASS：83/83；零lint告警/类型错误；Next.js16.2.11 production build与58个页面合同通过 |
| 双仓差异空白 | 后端与前端分别`git diff --check` | Backend0.435s；Frontend0.294s | 0 / 0 | PASS：零空白错误；仅既有Git LF到CRLF工作副本提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.645s；Allow-open0.651s | 1 / 0 | PASS：audit/ledger均精确449，120 CLOSED / 329 OPEN；allow-open通过；严格总门禁仅因329个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

顶层definition现在由服务端当前JSONB和完整客户端回显共同守住“不变”合同；普通本地化、版本或候选编辑无法再把不可见字段解释为删除。无DDL，generation保持103，远程generation85仅使用会话临时表。

## 2026-08-21：BUG-145 资料编辑服务端能力合同

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前能力契约红测 | bundled Node capability parser/source tests；Go capability/source tests | Node墙钟0.204s、Node105.788ms；Go墙钟1.773s | 1 / 1 | 预期RED：无服务端capabilities/helper；资源详情以token显示编辑，板块页用独立`/editor`请求猜权限 |
| 定向前后端合同 | bundled Node 2 tests；Go capability/source/cache tests；`tsc --noEmit`与目标ESLint | Node墙钟0.210s、Node117.305ms；Go最终墙钟6.606s、包1.144s；Type+Lint墙钟7.646s | 0 / 0 / 0 | PASS：仅显式true、两响应同源三字段、身份键失败关闭、零探测/零token-as-capability；private/no-store与双Vary通过 |
| 真实PostgreSQL能力与直写授权 | 本地.env组装task-scoped DSN后`go test ./internal/httpapi -run '^TestModContentCapabilitiesAndDirectWriteAuthorizationIntegration$' -count=1 -v` | 墙钟2.336s；测试0.280s；包0.399s | 0 | PASS：MaxConns=1会话临时mods表；普通用户详情三能力均false，绕过UI直接PUT仍403；无远端持久写入 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test10.427s；Vet3.072s；Build4.087s；tidy0.364s | 0 / 0 / 0 / 0 | PASS：读取能力、写授权、现有资料与全部模块闭集通过 |
| 前端全量测试 / Lint / Type / Production build | bundled Node全量`--test`、ESLint`--max-warnings=0`、`tsc --noEmit`；合法HTTPS task-scoped env下`next build` | Test1.360s；Lint21.200s；Type1.970s；Build21.347s | 0 / 0 / 0 / 0 | PASS：85/85；零lint告警/类型错误；Next.js16.2.11 production build与58个页面合同通过 |
| 双仓差异空白 | 后端与前端分别`git diff --check` | Backend0.432s；Frontend0.297s | 0 / 0 | PASS：零空白错误；仅既有Git LF到CRLF工作副本提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.647s；Allow-open0.633s | 1 / 0 | PASS：audit/ledger均精确449，121 CLOSED / 328 OPEN；allow-open通过；严格总门禁仅因328个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

资料页显示的每项写入口现在都来自与该对象、该身份同源的服务端能力；UI失败关闭而写端继续独立强制授权。无DDL，generation保持103，远程generation85仅使用会话临时表。

## 2026-08-22：BUG-146 OSS批上传逐项成功保留

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前部分成功红测 | bundled Node `oss-upload-batch.test.mts` | 墙钟0.215s、Node120.721ms | 1 | 预期RED：共享任务模块不存在；三图库只在全批完成后写数组，双尺寸仍Promise.all |
| 定向任务与四入口合同 | bundled Node 4 tests；目标ESLint与`tsc --noEmit` | Node墙钟0.209s、Node120.589ms；目标Type/Lint均通过 | 0 / 0 / 0 | PASS：首请求前完整分类、首尾成功不被中间失败遮蔽、retry不重复成功项、四入口/统一状态/双ID接线通过 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test4.447s；Vet3.740s；Build4.706s；tidy0.556s | 0 / 0 / 0 / 0 | PASS：OSS presign/complete、个人文件、额度与全部模块既有合同无回归 |
| 前端全量测试 / Lint / Type / Production build | bundled Node全量`--test`、ESLint`--max-warnings=0`、`tsc --noEmit`；合法HTTPS task-scoped env下`next build` | Test1.555s；Lint20.711s；Type2.036s；Build21.130s | 0 / 0 / 0 / 0 | PASS：89/89；零lint告警/类型错误；Next.js16.2.11 production build与58个页面合同通过 |
| 双仓差异空白 | 后端与前端分别`git diff --check` | Backend0.430s；Frontend0.280s | 0 / 0 | PASS：零空白错误；仅既有Git LF到CRLF工作副本提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.642s；Allow-open0.634s | 1 / 0 | PASS：audit/ledger均精确449，122 CLOSED / 327 OPEN；allow-open通过；严格总门禁仅因327个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

OSS成功文件现在在每个complete返回后立即成为可见草稿事实；失败兄弟不会隐藏、覆盖或促使成功项重复上传。无DDL/API变化，generation保持103，远程generation85未访问。

## 2026-08-22：BUG-147 简单项目筛选元数据独立事实源

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前分页窗口反推红测 | Go cursor/endpoint source tests；bundled Node facet path/merge/source tests | Go墙钟1.745s；Node墙钟0.196s、Node104.325ms | 1 / 1 | 预期RED：无facet路由/cursor；前端仍以当前items flatMap构造父项并混入静态枚举 |
| 定向前后端合同 | Go 3 tests；bundled Node 4 tests；目标ESLint与`tsc --noEmit` | Go墙钟2.396s、包0.134s；Node墙钟0.259s、Node137.801ms；Lint4.052s；Type2.748s | 0 / 0 / 0 / 0 | PASS：scope cursor/100上限、路由/limit+1接线、严格响应parser、selected/page merge、零items.flatMap与静态闭集通过 |
| 真实PostgreSQL可见性与分页 | 本地.env组装task-scoped DSN后`go test ./internal/httpapi -run '^TestSimpleProjectParentFacetTraversesVisibleDistinctOptionsIntegration$' -count=1 -v` | 墙钟2.637s；测试0.570s；包0.683s | 0 | PASS：会话临时simple_projects/refs/mods/modpacks；126个可见distinct父项50/50/26零漏重；他人pending隐藏、本人pending可见、后页selected首屏回显；无远端持久写入 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test13.764s；Vet9.823s；Build10.850s；tidy0.583s | 0 / 0 / 0 / 0 | PASS：新facet、原目录过滤、关联装配与全部模块闭集通过 |
| 前端全量测试 / Lint / Type / Production build | bundled Node全量`--test`、ESLint`--max-warnings=0`、`tsc --noEmit`；合法HTTPS task-scoped env下`next build` | Test1.584s；Lint23.225s；Type2.748s；Build21.248s | 0 / 0 / 0 / 0 | PASS：93/93；零lint告警/类型错误；Next.js16.2.11 production build与58个页面合同通过 |
| 双仓差异空白 | 后端与前端分别`git diff --check` | Backend0.412s；Frontend0.289s | 0 / 0 | PASS：零空白错误；仅既有Git LF到CRLF工作副本提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.716s；Allow-open0.635s | 1 / 0 | PASS：audit/ledger均精确449，123 CLOSED / 326 OPEN；allow-open通过；严格总门禁仅因326个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

简单项目筛选项不再由当前分页窗口生成：静态枚举来自注册表，动态父项目来自身份绑定且可完整遍历的独立目录。无DDL，generation保持103，远程generation85仅使用会话临时表。

## 2026-08-22：BUG-148 资料分类整棵子树统一重挂约束

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前双入口与错误协议红测 | bundled Node category-tree tests；Go category tree/API error tests | Node墙钟0.202s、Node106.357ms；Go墙钟1.811s | 1 / 1 | 预期RED：无共享重挂策略；下拉仍直接替换parent；后端无typed树错误及稳定details |
| 定向重挂策略与API合同 | bundled Node 3 tests；Go 2 tests；目标ESLint；`tsc --noEmit` | Node墙钟0.225s、Node131.075ms；Go墙钟7.306s、包1.153s；Lint4.547s；Type2.042s | 0 / 0 / 0 / 0 | PASS：深度2目标+高度2子树精确拒绝，深度1目标接受至第4层；自身/后代/缺失/畸形树拒绝；下拉与拖拽同函数；422 code/field/category/parent/reason/max稳定 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test10.807s；Vet4.565s；Build5.384s；tidy0.413s | 0 / 0 / 0 / 0 | PASS：布局准备、发布事务、资源归属及全部模块闭集无回归 |
| 前端全量测试 / Lint / Type / Production build | bundled Node全量`--test`、ESLint`--max-warnings=0`、最终`tsc --noEmit`；合法HTTPS task-scoped env下`next build` | Test1.517s；Lint21.016s；Type2.042s；Build22.379s | 0 / 0 / 0 / 0 | PASS：96/96；零lint告警/类型错误；Next.js16.2.11 production build与58个页面合同通过 |
| 双仓差异空白 | 后端与前端分别`git diff --check`，并对本项未跟踪新文件执行`git diff --no-index --check` | Backend0.525s；Frontend0.438s | 0 / 0 | PASS：零空白错误；仅既有Git LF到CRLF工作副本提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.636s；Allow-open0.634s | 1 / 0 | PASS：audit/ledger均精确449，124 CLOSED / 325 OPEN；allow-open通过；严格总门禁仅因325个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

分类下拉不再能产生拖拽会拒绝、后端也必然拒绝的超深草稿；后端仍是最终权威并给出可机器定位的树错误。无DDL，generation保持103，远端generation85未访问。

## 2026-08-22：BUG-149 任务奖励币种唯一性

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前覆盖与重复请求红测 | bundled Node task currency rewards tests；Go duplicate JSON/API tests | Node墙钟0.202s、Node106.926ms；Go墙钟1.793s | 1 / 1 | 预期RED：无共享rename/唯一性模块；UI直接delete+赋值覆盖；后端无原始键或规范化碰撞拒绝 |
| 定向前后端唯一性合同 | bundled Node 3 tests；Go 3 tests；目标ESLint；`tsc --noEmit` | Node墙钟0.243s、Node124.874ms；Go墙钟2.433s、包0.168s；Lint3.861s；Type2.595s | 0 / 0 / 0 / 0 | PASS：占用目标禁用且rename拒绝不改输入；合法新code保留金额；save guard拒绝规范化碰撞；完全重复JSON和Gold/空白gold在零DB访问时400 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test10.658s；Vet4.645s；Build5.522s；tidy0.418s | 0 / 0 / 0 / 0 | PASS：task CRUD、发奖、经济守恒及全部模块闭集无回归 |
| 前端全量测试 / Lint / Type / Production build | bundled Node全量`--test`、ESLint`--max-warnings=0`、`tsc --noEmit`；合法HTTPS task-scoped env下`next build` | Test1.581s；Lint20.900s；Type2.595s；Build22.572s | 0 / 0 / 0 / 0 | PASS：99/99；零lint告警/类型错误；Next.js16.2.11 production build与58个页面合同通过 |
| 双仓差异空白 | 后端与前端分别`git diff --check`，并对本项未跟踪新文件执行`git diff --no-index --check` | Backend0.486s；Frontend0.410s | 0 / 0 | PASS：零空白错误；仅既有Git LF到CRLF工作副本提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.627s；Allow-open0.633s | 1 / 0 | PASS：audit/ledger均精确449，125 CLOSED / 324 OPEN；allow-open通过；严格总门禁仅因324个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

任务奖励重命名现在只表达一对一迁移，不再承担隐式覆盖/合并；跨UI、状态、JSON解析与服务端规范化四层维持相同code唯一性。无DDL，generation保持103，远端generation85未访问。

## 2026-08-22：BUG-150 Markdown受控文档会话同步

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前受控同步红测 | bundled Node `markdown-editor-session.test.mts` | 墙钟0.191s、Node99.742ms | 1 | 预期RED：共享session模块不存在；组件只在初始化读取value，embedded markdown effect把所有状态变化回写父级 |
| 定向会话与调用方合同 | bundled Node 5 tests；目标ESLint；`tsc --noEmit` | Node墙钟0.215s、Node124.139ms；Lint5.621s；Type3.397s | 0 / 0 / 0 | PASS：A→B→A、clean异步值、dirty竞争刷新保留、11入口identity、prop reset不发布onChange全部通过 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test2.559s；Vet2.790s；Build3.651s；tidy0.377s | 0 / 0 / 0 / 0 | PASS：纯前端变化；全部后端模块、已有草稿/Markdown API闭集无回归 |
| 前端全量测试 / Lint / Type / Production build | bundled Node全量`--test`、ESLint`--max-warnings=0`、`tsc --noEmit`；合法HTTPS task-scoped env下`next build` | Test1.494s；Lint19.037s；Type3.397s；Build22.893s | 0 / 0 / 0 / 0 | PASS：104/104；零lint告警/类型错误；Next.js16.2.11 production build与58个页面合同通过 |
| 双仓差异空白 | 后端与前端分别`git diff --check`，并对本项未跟踪新文件执行`git diff --no-index --check`（差异状态1且零诊断规范化为成功） | Backend0.436s；Frontend0.418s | 0 / 0 | PASS：零空白错误；仅既有Git LF到CRLF工作副本提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.721s；Allow-open0.639s | 1 / 0 | PASS：audit/ledger均精确449，126 CLOSED / 323 OPEN；allow-open通过；严格总门禁仅因323个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

embedded Markdown现在以文档身份切换会话，以父级externalValue作为dirty基线；prop驱动重置不会冒充用户编辑，竞争刷新也不会覆盖活动输入。无DDL/API变化，generation保持103，远端generation85未访问；BUG-151保持开放。

## 2026-08-22：BUG-151 Markdown草稿单调保存与跨tab冲突

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前无版本保存红测 | bundled Node `markdown-draft-save.test.mts`；Go Markdown schema/request/order tests | Node墙钟0.199s、Node105.214ms；Go墙钟1.781s | 1 / 1 | 预期RED：无coordinator模块；请求只有content；schema无revision/session/sequence；服务端无条件upsert |
| 定向前后端合同 | bundled Node 4 tests；Go schema/request tests；目标ESLint；`tsc --noEmit` | Node墙钟0.215s、Node121.489ms；Go墙钟2.146s；Lint3.541s；Type4.005s | 0 / 0 / 0 / 0 | PASS：严格递增sequence、迟到response拒绝、冲突parser/rebase、Abort/双选择源码合同、generation104约束与请求校验通过 |
| 真实PostgreSQL到达顺序与跨tab | task-scoped DSN后`go test ./internal/httpapi -run '^TestMarkdownDraftSaveArrivalOrderAndCrossTabConflictIntegration$' -count=1 -v` | 首轮墙钟2.360s；最终墙钟7.435s、测试1.769s | 1 / 0 | 首轮RED定位INSERT source过滤使existing行未进入ON CONFLICT；修正后PASS：B先/A迟保持B且A冲突；A先/B后同session高sequence推进到B；另一session旧base冲突；仅会话临时表，无远端持久写入 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test12.244s；Vet3.130s；Build4.395s；tidy0.360s | 0 / 0 / 0 / 0 | PASS：generation104 schema源码、草稿API、全部后端模块闭集无回归 |
| 前端全量测试 / Lint / Type / Production build | bundled Node全量`--test`、ESLint`--max-warnings=0`、`tsc --noEmit`；合法HTTPS task-scoped env下`next build` | Test1.778s；Lint22.875s；Type4.005s；Build21.069s | 0 / 0 / 0 / 0 | PASS：108/108；零lint告警/类型错误；Next.js16.2.11 production build与58个页面合同通过 |
| 双仓差异空白 | 后端与前端分别`git diff --check`，并对本项未跟踪新文件执行`git diff --no-index --check`（差异状态1且零诊断规范化为成功） | Backend0.600s；Frontend0.463s | 0 / 0 | PASS：零空白错误；仅既有Git LF到CRLF工作副本提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.712s；Allow-open0.645s | 1 / 0 | PASS：audit/ledger均精确449，127 CLOSED / 322 OPEN；allow-open通过；严格总门禁仅因322个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

Markdown账号草稿不再以HTTP到达顺序决定最终正文：同编辑器以sequence单调推进，跨tab以revision冲突并由用户选择。generation104要求开发库重置；远端generation85只使用会话临时影子表，未迁移、未持久写入。

## 2026-08-22：BUG-003 Markdown草稿读取失败关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前空值伪装红测 | bundled Node `markdown-draft-load.test.mts`；Go read-result test | Node墙钟0.213s、Node115.032ms；Go墙钟1.715s | 1 / 1 | 预期RED：前端失败仍setDraftLoaded(true且无Retry；后端无missing/failure分类函数，handler对任意错误200空草稿 |
| 定向读取/失败关闭合同 | bundled Node 1 test；Go 1 test；目标ESLint；`tsc --noEmit` | Node墙钟0.244s、Node125.913ms；Go墙钟7.528s、包1.195s；Lint4.880s；Type9.791s | 0 / 0 / 0 / 0 | PASS：no-row精确200 revision0；数据库故障503稳定code/无data；Save与autosave保持关闭，Retry重新读取 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test10.761s；Vet3.085s；Build4.054s；tidy0.373s | 0 / 0 / 0 / 0 | PASS：草稿revision API与全部模块闭集无回归 |
| 前端全量测试 / Lint / Type / Production build | bundled Node全量`--test`、ESLint`--max-warnings=0`、`tsc --noEmit`；合法HTTPS task-scoped env下`next build` | Test1.688s；Lint22.077s；Type9.791s；Build21.362s | 0 / 0 / 0 / 0 | PASS：109/109；零lint告警/类型错误；Next.js16.2.11 production build与58个页面合同通过 |
| 双仓差异空白 | 后端与前端分别`git diff --check`，并对本项未跟踪新测试执行`git diff --no-index --check`（差异状态1且零诊断规范化为成功） | Backend0.438s；Frontend0.309s | 0 / 0 | PASS：两仓已跟踪差异与本项未跟踪测试均无空白错误 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.640s；Allow-open0.632s | 1 / 0 | PASS：audit/ledger均精确449，128 CLOSED / 321 OPEN；allow-open通过；严格总门禁仅因321个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

账号草稿读取的unknown状态现在会503并冻结保存，而不是伪装成权威空草稿；只有pgx.ErrNoRows才允许revision0首次写入。无DDL，generation保持104，远端generation85未访问。

## 2026-08-22：ARCH-003 修订公开ID反查吞错关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前双返回值红测 | `go test ./internal/httpapi -run '^TestRevisionPublicIDValuePreservesLookupErrors$' -count=1` | 墙钟1.696s | 1 | 预期RED：`revisionPublicIDValue`只有单返回值，三处测试赋值均编译失败，证明error无法传给调用方 |
| 定向error/success/nil合同 | 同一命令 | 墙钟6.595s、包1.168s | 0 | PASS：查询错误原样返回；成功公开ID返回；nil内部ID不查询且返回nil/nil |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test12.133s；Vet8.216s；Build9.131s；tidy0.399s | 0 / 0 / 0 / 0 | PASS：六个详情调用方均通过双返回值编译门禁，全部后端模块闭集无回归 |
| 后端差异空白 | `git diff --check`；本项未跟踪测试执行`git diff --no-index --check`（差异状态1且零诊断规范化为成功） | 0.437s | 0 | PASS：已跟踪差异与新测试均无空白错误 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.703s；Allow-open0.649s | 1 / 0 | PASS：audit/ledger均精确449，129 CLOSED / 320 OPEN；allow-open通过；严格总门禁仅因320个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

六类详情不再把修订路由损坏或数据库故障返回为有效200/null；真正未发布仍不查询并保持null。无DDL，generation保持104，远端generation85未访问。

## 2026-08-22：ARCH-004 用户文件读取错误遗漏关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前读取合同红测 | `go test ./internal/httpapi -run '^Test(LoadUserOSSFileQuotaUsageFailsClosed&#124;UserOSSFilesChecksRowsErrBeforeSuccess)$' -count=1` | 墙钟1.711s | 1 | 预期RED：额度失败关闭loader不存在；旧Handler也没有可执行的rows.Err成功前置合同 |
| 定向列表/额度合同 | 同一命令 | 墙钟6.577s、包1.157s | 0 | PASS：源码门禁确认rows.Err位于成功JSON前；每日首查失败只查询一次，总量次查失败停止；成功保留四项精确用量 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test11.858s；Vet8.062s；Build8.974s；tidy0.382s | 0 / 0 / 0 / 0 | PASS：用户文件读取与全部后端模块闭集无回归 |
| 后端差异空白 | `git diff --check`；本项未跟踪测试执行`git diff --no-index --check`（差异状态1且零诊断规范化为成功） | 0.425s | 0 | PASS：已跟踪差异与新测试均无空白错误 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.709s；Allow-open0.641s | 1 / 0 | PASS：audit/ledger均精确449，130 CLOSED / 319 OPEN；allow-open通过；严格总门禁仅因319个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

文件列表只在完整迭代后返回，额度只在每日与总量聚合全部成功后返回；未知数据库状态不再成为截断数组或零用量。无DDL，generation保持104，远端generation85未访问。

## 2026-08-22：PERF-002 用户项目陈列全站反向过滤关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前access-first红测 | `go test ./internal/httpapi -run '^TestUserShowcaseProjectsQueryStartsFromUserAccess$' -count=1` | 墙钟1.713s | 1 | 预期RED：生产查询尚无可复用常量且仍由全站projects UNION驱动、逐行两个access EXISTS |
| 定向查询结构 | 同一命令 | 墙钟6.641s、包1.151s | 0 | PASS：user/filter/group物化先于七类项目连接；七分支全部从qualified_access出发；旧access EXISTS零残留 |
| 真实PostgreSQL功能与三规模计划 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestUserShowcaseProjectsAccessFirstFunctionAndScalePlanIntegration$' -count=1 -v` | 墙钟14.710s、包12.769s、用例12.65s | 0 | PASS：七类公开项目与developer/editor合并精确，待审和他人项目排除；实际装载/ANALYZE 100k、1M、10M，均命中`showcase_scale_ids_pkey`且无规模表Seq Scan；执行0.316/0.393/3.371ms |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test12.194s；Vet8.047s；Build9.085s；tidy0.413s | 0 / 0 / 0 / 0 | PASS：profile showcase与全部后端模块闭集无回归 |
| 后端差异空白 | `git diff --check`；本项两个未跟踪测试执行`git diff --no-index --check`（差异状态1且零诊断规范化为成功） | 0.439s | 0 | PASS：已跟踪差异与新测试均无空白错误 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.726s；Allow-open0.662s | 1 / 0 | PASS：audit/ledger均精确449，131 CLOSED / 318 OPEN；allow-open通过；严格总门禁仅因318个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

用户陈列查询的工作量现在由该用户实际权限集合及最多100个输出驱动，不再随全站项目先行扫描；10M临时规模数据已在会话结束后清理。无DDL，generation保持104，远端generation85无永久变化。

## 2026-08-22：PERF-003 粉丝/关注深OFFSET关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前后端游标/Schema红测 | Go cursor/source/schema目标组 | 墙钟2.976s | 1 | 预期RED：游标解析/编码不存在，Handler仍含COUNT/OFFSET，generation104且缺双向page索引 |
| 后端定向游标与generation105 | 同目标组 | 墙钟6.867s；HTTP1.113s、database0.459s | 0 | PASS：opaque scope round-trip；旧page/pageSize、跨user/type/limit和畸形cursor拒绝；Handler无COUNT/OFFSET；双向复合索引存在 |
| 真实PostgreSQL双向遍历与600k深计划 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestUserConnectionCursorTraversesAndUsesDeepPageIndexesIntegration$' -count=1 -v` | 墙钟8.927s、包7.004s、用例6.89s | 0 | PASS：followers/following各205条按24跨页零重漏；两方向各实际600k并ANALYZE，近599k深处命中各自索引、无follow表Seq Scan，执行0.134/0.125ms |
| 修复前前端游标红测 | bundled Node `user-network-pagination.test.mts` | 墙钟0.196s、Node96.360ms | 1 | 预期RED：游标路径/历史helper不存在；旧UI只发送page/pageSize并依赖total页数 |
| 前端夹具/构建迭代 | 新helper后目标测试首轮；production build首轮 | Test0.187s；Build9.529s | 1 / 1 | 仅夹具RED：Node测试应直接导入`.mts`；Next组件也应按仓库约定导入`.mts`而非`.mjs`。修正扩展名，未放宽业务断言 |
| 前端定向游标/重复COUNT门禁 | bundled Node 3 tests；目标ESLint；`tsc --noEmit` | Test0.248s、Node128.054ms；Lint3.197s；Type2.337s | 0 / 0 / 0 | PASS：limit/cursor编码、不可变前后游标栈、public无page参数、blocked独立保留；profile effect不依赖cursor/page，切页不重复COUNT |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test14.803s；Vet10.117s；Build11.125s；tidy0.568s | 0 / 0 / 0 / 0 | PASS：generation105与全部后端模块闭集无回归 |
| 前端全量测试 / Lint / Type / Production build | bundled Node全量`--test`、ESLint`--max-warnings=0`、`tsc --noEmit`；合法HTTPS task-scoped env下`next build` | Test1.758s；Lint19.499s；Type2.337s；Build21.676s | 0 / 0 / 0 / 0 | PASS：112/112；零lint告警/类型错误；Next.js16.2.11 production build与58个页面合同通过 |
| 双仓差异空白 | 后端与前端分别`git diff --check`，并对本项未跟踪新文件执行`git diff --no-index --check`（差异状态1且零诊断规范化为成功） | Backend0.840s；Frontend0.513s | 0 / 0 | PASS：两仓已跟踪差异与本项新文件均无空白错误 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.719s；Allow-open0.651s | 1 / 0 | PASS：audit/ledger均精确449，132 CLOSED / 317 OPEN；allow-open通过；严格总门禁仅因317个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

followers/following不再提供可放大的页码或每页总数扫描；两个方向都有稳定复合索引和opaque cursor。generation105要求开发库重置，远端generation85只使用会话临时表且已自动清理。

## 2026-08-22：SEC-005 公开贡献历史当前可见性关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前公开策略红测 | `go test ./internal/httpapi -run '^TestPublicContributionActivityUsesCurrentAnonymousVisibility$' -count=1` | 墙钟1.697s | 1 | 预期RED：无独立公开贡献查询边界，旧查询left join route并从revision snapshot取名，未复核当前状态 |
| 定向匿名可见性/无历史名称 | 同一命令 | 墙钟6.669s、包1.157s | 0 | PASS：inner route、七类当前状态、skin仅public、default false存在；revision snapshot/content revision join零残留 |
| 真实PostgreSQL隐私状态切换 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestPublicContributionActivityRevalidatesCurrentTargetVisibilityIntegration$' -count=1 -v` | 墙钟2.373s、包0.436s、用例0.32s | 0 | PASS：pending/deleted/private/unlisted/no-route/unsupported均隐藏；公开Mod/skin显示当前名；两Mod审核状态互换后结果立即随当前状态切换 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test12.403s；Vet8.468s；Build9.547s；tidy0.437s | 0 / 0 / 0 / 0 | PASS：贡献feed与全部后端模块闭集无回归 |
| 后端差异空白 | `git diff --check`；本项两个未跟踪测试执行`git diff --no-index --check`（差异状态1且零诊断规范化为成功） | 0.611s | 0 | PASS：已跟踪差异与新测试均无空白错误 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.644s；Allow-open0.636s | 1 / 0 | PASS：audit/ledger均精确449，133 CLOSED / 316 OPEN；allow-open通过；严格总门禁仅因316个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

公开贡献明细现在是历史批准与当前匿名公开集合的交集，并只返回当前名称；完整治理历史仍保留在后台数据中。无DDL，generation保持105，远端generation85无永久变化。

## 2026-08-22：DB-002 作者与团队关系稳定审核身份关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 差异同步源码门禁 | `go test ./internal/httpapi -run '^TestRelationshipSyncUsesLockedDifferentialUpdates$' -count=1` | 墙钟7.015s、包1.141s | 0 | PASS：两同步函数均有FOR UPDATE、UPDATE/INSERT/revoked，目标关系表DELETE零残留 |
| 真实PostgreSQL稳定身份与隐藏状态 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'Test(RelationshipSyncPreservesAuditIdentityAndHiddenStates&#124;CreatorProfileAndAuditedTeamMemberUpdatesAreIsolated)Integration' -count=1` | 墙钟11.054s、包5.027s | 0 | PASS：普通编辑保留approved/pending/rejected及ID/public ID/创建批准时间；管理员只revoked旧行并插入新行；团队before/after审计与强制失败回滚通过 |
| 完整HTTP/API数据库包环境核验 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -count=1 -timeout=10m` | 包159.069s | 1 | 预期环境限制：远端schema generation85而代码要求105，既有集成用例报告source/projection_source等generation105缺列；本项session temp影子测试独立通过，未对远端执行DDL或永久写入 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test10.503s；Vet3.633s；Build4.414s；tidy0.634s | 0 / 0 / 0 / 0 | PASS：差异关系同步与全部非远端Schema模块闭集无回归 |
| 后端差异空白 | `git diff --check`；本项两个未跟踪测试执行`git diff --no-index --check`（差异状态1且零诊断规范化为成功） | 0.947s | 0 | PASS：已跟踪差异及新增源码/集成测试均无空白错误 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.641s；Allow-open0.629s | 1 / 0 | PASS：audit/ledger均精确449，134 CLOSED / 315 OPEN；allow-open通过；严格总门禁仅因315个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

关系保存不再把审核事实误作可重建快照：稳定键、公开身份和历史时间保留，授权移除由revoked表达。无DDL，generation保持105，远端generation85无永久变化。

## 2026-08-22：DB-003 / SEC-006 / TEST-005 创作者导入无副作用与安全角色建议关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 定向角色、预览与持久化边界 | `go test ./internal/httpapi ./internal/database -run 'Test(CreatorImport&#124;ImportedCreatorRole&#124;ImportModrinthOrganization&#124;ImportCurseForgeAuthor&#124;CreatorImportRoles)' -count=1`；bundled Node creator import目标3 tests | Go墙钟7.059s；Node墙钟0.532s、Node149ms | 0 / 0 | PASS：Supporter/恶意/超长角色安全回落，明确owner/developer/maintainer及展示角色成立；preview DTO/UI零持久身份且显示外部/内部/授权三事实 |
| 真实PostgreSQL Handler零副作用 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCreatorImportPreviewDoesNotPersistMembersIntegration$' -count=1 -v` | 最终墙钟2.933s、包0.547s、用例0.43s | 0 | PASS：实际Modrinth组织Handler读取数据库角色真值；Supporter=contributor/false、Maintainer=maintainer/true，返回200后creators计数0且响应无creatorId/roleId/avatarFileId/createdMembers；仅会话临时表 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test8.392s；Vet4.389s；Build5.153s；tidy0.844s | 0 / 0 / 0 / 0 | PASS：generation106角色白名单、预览调用链、保存头像信任边界及全部后端模块闭集无回归 |
| 前端全量测试 / Lint / Type / Production build | bundled Node全量`--test`、ESLint`--max-warnings=0`、`tsc --noEmit`；合法HTTPS task-scoped env下`next build` | Test2.330s；Lint21.459s；Type2.970s；Build21.377s | 0 / 0 / 0 / 0 | PASS：113/113；零lint告警/类型错误；Next.js16.2.11 production build与58个页面合同通过 |
| 双仓差异空白 | 后端与前端分别`git diff --check`；本项四个未跟踪新测试执行`git diff --no-index --check`并过滤既有LF→CRLF提示 | tracked Backend0.601s、Frontend0.602s；new Backend0.428s、Frontend0.479s | 0 / 0 / 0 / 0 | PASS：两仓已跟踪差异与本项三个Go/一个Node新测试均无空白错误；仅既有行尾转换提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.962s；Allow-open1.058s | 1 / 0 | PASS：audit/ledger均精确449，137 CLOSED / 312 OPEN；allow-open通过；严格总门禁仅因312个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

创作者导入现在只形成可取消的外部事实预览；站内身份、OSS对象和团队关系只由后续显式写流程产生。外部角色不再因未知文本升级为developer，授权真值由generation106数据库角色定义提供并在UI明确展示。远端generation85仅使用会话临时影子表，无DDL或永久写入。

## 2026-08-22：TEST-004 创建与导入零隐式授权矩阵关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 真实PostgreSQL创建/导入授权矩阵 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi ./internal/database -run '^(TestProjectCreationAndImportEntrypointsDoNotGrantAuthorizationIntegration&#124;TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration)$' -count=1 -v` | HTTP包56.284s、用例53.37s；database包38.787s、用例37.53s | 0 | PASS：25个manual/final import/import-job subtests全部执行真实Handler；两个版本号不变，五类授权来源为零且全局role binding不增；每个实际项目的管理接口均403；临时generation106工作且清理后public generation85和namespace零残留 |
| AST局部补充门禁 | `go test ./internal/httpapi -run '^TestCatalogCreationEntrypointsContainNoDirectProjectRoleSQL$' -count=1` | 墙钟2.137s、包0.118s | 0 | PASS：四个顶层创建入口没有直接`user_role_bindings` SQL；测试名称与注释明确不声称覆盖helper调用图 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test17.532s；Vet10.572s；Build12.615s；tidy0.788s | 0 / 0 / 0 / 0 | PASS：临时Schema夹具、probe依赖缝、25分支矩阵及全部后端模块闭集无回归 |
| 后端差异与陈旧断言 | 三个tracked文件`git diff --check`；三个新文件`git diff --no-index --check`并过滤既有LF→CRLF提示；`rg`搜索旧测试名/入口绑定过度声明 | 墙钟1.1s | 0 | PASS：tracked/new文件均无空白诊断；旧测试名和过度声明零残留；范围状态仅为预期三处修改与三个新文件 |
| 前端闭集复用 | TEST-005同一generation106工作树已完成bundled Node全量、ESLint、TypeScript与production build，本项无前端文件或公开DTO变化 | Test2.330s；Lint21.459s；Type2.970s；Build21.377s | 0 / 0 / 0 / 0 | PASS：113/113与58页构建证据保持适用；TEST-004只新增后端内部测试夹具/依赖缝 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.643s；Allow-open0.633s | 1 / 0 | PASS：audit/ledger均精确449，138 CLOSED / 311 OPEN；allow-open通过；严格总门禁仅因311个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

提交者事实与授权事实已被行为级分离：无论创建入口调用多少helper，数据库授权投影和实际管理Handler都不能把创建者识别成编辑者。集成Schema只存在于单连接会话，未迁移或写入远端public Schema。

## 2026-08-22：BUG-005 项目创建事实权威actor字段关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前真实PostgreSQL红测 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestUserContentCreationFactsFollowAuthoritativeActorLifecycleIntegration$' -count=1 -v` | 墙钟40.369s、包38.642s、用例37.81s | 1 | 预期RED：Mod/整合包/simple/server插入后事实均no rows；community更新author A→B后事实仍归A，精确复现审计缺陷及同一根因的更新漂移 |
| 真实PG五类生命周期与Schema源码门禁 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^(TestUserFeatureSchemaKeepsStatisticsIndependentFromActivityRetention&#124;TestUserContentCreationFactsFollowAuthoritativeActorLifecycleIntegration)$' -count=1 -v` | 墙钟36.257s、包34.520s、集成用例33.67s | 0 | PASS：四类项目读取submitted_by、community读取author_id；五类actor A→B和pending→approved同步；物理删除保留身份并标记不存在；community soft delete/restore切换正确；旧created_by零残留 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test16.537s；Vet9.457s；Build10.434s；tidy0.963s | 0 / 0 / 0 / 0 | PASS：创建事实函数、五个trigger、真实夹具及全部后端模块闭集无回归 |
| 后端差异空白 | 两个tracked文件`git diff --check`；新增集成测试`git diff --no-index --check`并过滤既有LF→CRLF提示 | 墙钟0.9s | 0 | PASS：跟踪差异与新增测试均无空白诊断；范围仅为函数/Schema源码门禁及真实生命周期测试 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.656s；Allow-open0.629s | 1 / 0 | PASS：audit/ledger均精确449，139 CLOSED / 310 OPEN；allow-open通过；严格总门禁仅因310个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

项目创建事实不再依赖人工校准才出现，actor、审核态与存在态都由来源行事务中的触发器实时同步。BUG-006的全量校准精确重建仍保持开放，未用本项证据提前关闭。

## 2026-08-22：BUG-006 用户统计精确校准与清理基线关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 校准命令可执行性红测 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/userstats -run '^TestReconcileExactlyRepairsDriftAndPreservesRetainedActivityIntegration$' -count=1 -v` | 墙钟39.981s、包38.494s、用例37.66s | 1 | 预期RED扩展：进入真实reconcile后pgx把advisory `$1`推为text而不能编码int64，证明原校准命令在任何重建前即失败；改为显式bigint参数 |
| 原审计漂移核心红测 | 同一真实PG命令，在修正lock参数后 | 墙钟34.330s、包32.789s、用例31.95s | 1 | 预期RED：首日仍为99/99及`bogus:99,view:77`，过高数值、错误动作分布和时间均无法被`greatest`校低 |
| generation107真实PG精确与幂等 | 同一命令最终版 | 墙钟36.933s、包35.438s、用例34.60s | 0 | PASS：部分/整日删除形成3 retained rows/3 events；连续reconcile两次均恢复两日4动作、四类精确JSON、30 added/8 deleted bytes、正确first/last/edit/comment；ghost/bogus/99零残留 |
| 定向Schema与SQL源码门禁 | `go test ./internal/userstats ./internal/database -run '^(TestReconcileSQLExactlyReplacesAllStatisticsFromRawAndRetainedFacts&#124;TestUserFeatureSchemaKeepsStatisticsIndependentFromActivityRetention)$' -count=1`及最终真实组合 | 源码墙钟2.891s；最终组合墙钟37.344s、userstats35.636s/database1.242s | 0 | PASS：transition old table语句级聚合、复合基线、用户FOR UPDATE、retained+raw、stale delete、完整action JSON及禁止reconcile greatest均被锁定 |
| generation107跨Finding真实回归 | `TestUserContentCreationFactsFollowAuthoritativeActorLifecycleIntegration`；`TestProjectCreationAndImportEntrypointsDoNotGrantAuthorizationIntegration`；`TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration` | BUG-005墙钟41.166s/包39.784s；TEST-004墙钟54.415s/包52.468s；Isolation墙钟30.635s/包29.267s | 0 / 0 / 0 | PASS：五类创建事实仍正确；25分支仍零隐式授权；当前107全Schema清理后public generation85不变且临时namespace零残留 |
| 后端全仓 / Vet / Build / tidy | 最终`go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test7.732s；Vet3.369s；Build10.077s；tidy0.984s | 0 / 0 / 0 / 0 | PASS：generation107、保留触发器、精确校准、CLI和全部后端模块闭集无回归 |
| 后端差异与陈旧路径 | 全局tracked `git diff --check`；本项及受代次影响的11个untracked文件逐一`git diff --no-index --check`；生产reconcile旧greatest/条件JSON与旧generation断言搜索 | 墙钟1.6s | 0 | PASS：差异无空白诊断；reconcile单调漂移路径零残留；14项Schema代次断言均为107 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.663s；Allow-open0.645s | 1 / 0 | PASS：audit/ledger均精确449，140 CLOSED / 309 OPEN；allow-open通过；严格总门禁仅因309个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

用户统计现在能向上或向下回到权威值；清理raw只移除细节，不再迫使校准在“丢历史”与“保留错误最大值”之间二选一。保留基线是最小聚合，不携带对象身份或正文。

## 2026-08-22：SEC-008 多条按动作限制组合关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前真实PostgreSQL遮蔽红测 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/antiabuse -run '^TestAccountRestrictionsAreSelectedForEveryRequestedActionIntegration$' -count=1 -v` | 墙钟32.488s、包30.759s、用例29.83s | 1 | 预期RED：旧comment cooldown后写新message cooldown，comment读取只得到新message单行且`restrictionApplies=false`，精确复现可利用遮蔽 |
| 最终generation107真实限制矩阵 | 同一命令最终版 | 墙钟41.637s、包40.191s、用例40.11s | 0 | PASS：旧相关/新无关和反向顺序均正确；comment/message连续缓存与真实Evaluate均action_restricted；upload初始无污染；challenge+moderation并存；旧global read_only胜新wildcard cooldown |
| 定向查询、决策与缓存门禁 | `go test ./internal/antiabuse -run '^(TestTrustAndRestrictionClassification&#124;TestAccountRestrictionQueryFiltersBeforeAggregationAndScopesCacheByAction&#124;TestAccountStateInvalidationOnlyRemovesTargetAccount)$' -count=1` | 墙钟2.651s、包0.925s | 0 | PASS：SQL在聚合前按action/`*`/global过滤且无LIMIT 1；三类效果独立；action cache keys不同而用户prefix失效只删目标用户全部变体 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test16.052s；Vet9.236s；Build10.130s；tidy0.995s | 0 / 0 / 0 / 0 | PASS：反滥用决策、后台缓存失效、generation107及全部后端模块闭集无回归 |
| 后端差异与陈旧路径 | 全局tracked `git diff --check`；新增集成测试`git diff --no-index --check`；旧LIMIT 1 profile、RestrictionActions、非action account调用和`time.Until`搜索 | 墙钟1.1s | 0 | PASS：差异无空白诊断；四类旧选择/缓存路径零残留 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.644s；Allow-open0.633s | 1 / 0 | PASS：audit/ledger均精确449，141 CLOSED / 308 OPEN；allow-open通过；严格总门禁仅因308个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

限制判断现在是“所有身份来源中任一当前action匹配即生效”，而非“先挑最新一条再碰碰运气”。缓存按action隔离，管理员失效仍一次清掉该用户全部派生键。

## 2026-08-22：BUG-007 Yggdrasil Join原子提交关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 原路径事务边界复核 | `yggdrasil_session.go`定向阅读：Join upsert使用pool `Exec`自动提交，随后Token `last_used_at`用第二pool `Exec`；故障分支在第一次提交后返回503 | 静态复核 | — | CONFIRMED：与原审计证据完全一致，存在“返回失败但hasJoined可成功”的部分提交 |
| generation107真实PG故障/正常/并发矩阵 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestYggdrasilJoinCommitsSessionAndTokenUsageAtomicallyIntegration$' -count=1 -v` | 墙钟39.944s、包34.392s、用例33.24s | 0 | PASS：更新trigger故障时503且Join行0/Token时间null/hasJoined 204；移除故障后204与两事实同时提交；同serverId异Token并发严格为一个204+一个403，仅胜者last_used非null |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test10.279s；Vet2.727s；Build3.554s；tidy0.335s | 0 / 0 / 0 / 0 | PASS：Yggdrasil协议单测、generation107 Schema、权限解析及全部后端模块无回归 |
| 差异与旧双提交路径 | 全局tracked `git diff --check`；新集成测试`git diff --no-index --check`；`yggdrasil_session.go`搜索`s.db.Exec`零匹配，事务锁/两次`tx.Exec`/commit全部可达 | 墙钟1.1s | 0 | PASS：空白差异零诊断；Join handler不再使用pool自动提交业务写 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.639s；Allow-open0.638s | 1 / 0 | PASS：audit/ledger均精确449，142 CLOSED / 307 OPEN；allow-open通过；严格总门禁仅因307个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

Join响应现在与持久事实使用同一个提交点：204意味会话与Token使用时间都已存在，503意味两者都没有由本次请求提交。

## 2026-08-22：BUG-008 proof challenge完整发放关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 真实PostgreSQL metadata补写故障RED | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/antiabuse -run '^TestProofChallengePersistsCompleteStateWithoutMetadataUpdateIntegration$' -count=1 -v`；会话临时generation107 Schema上的trigger拒绝任何`UPDATE OF metadata` | 墙钟34.128s、包32.429s、用例31.49s | 1 | 预期RED：旧`createChallenge`吞掉trigger异常且仍返回ID；读取`metadata->>'nonce'`得到NULL，无法组成可验证挑战 |
| 首次单INSERT参数类型门禁 | 同一RED命令在初步改为`jsonb_build_object('nonce',$10)`后 | 墙钟32.949s、包31.044s、用例30.12s | 1 | 预期实现校正：PostgreSQL无法推断variadic JSON参数`$10`类型；生产SQL改为`$10::text`，不依赖驱动或数据值偶然推断 |
| generation107真实PG完整发放GREEN | 同一命令最终版 | 墙钟37.022s、包35.291s、用例34.35s | 0 | PASS：禁止metadata UPDATE时仍一次INSERT成功；nonce非空、answer hash与prompt答案匹配、初始pending，真实verify成功且终态consumed |
| antiabuse包与后端全仓 / Vet / Build / tidy | `go test ./internal/antiabuse -count=1`；`go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 包4.540s/3.098s；Test13.176s；Vet2.680s；Build3.524s；tidy0.331s | 0 / 0 / 0 / 0 / 0 | PASS：既有表单Token、单次proof、Turnstile降级、限制组合、generation107和全部后端模块无回归 |
| 差异与旧补写路径 | 全局tracked `git diff --check`；新集成测试/修复文档`git diff --no-index --check`；生产搜索`update anti_abuse_challenges set metadata`零匹配 | 最终定向检查 | 0 | PASS：空白差异零诊断；发放路径只剩首次完整INSERT |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.653s；Allow-open0.641s | 1 / 0 | PASS：audit/ledger均精确449，143 CLOSED / 306 OPEN；allow-open通过；严格总门禁仅因306个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

proof challenge现在没有“先发身份、后补可验证性”的窗口：ID被返回时，验证所需nonce已与answer hash同行持久。

## 2026-08-22：BUG-009 Mod资料分类四层数据库不变量关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 原审计算术复核 | 阅读`validate_mod_content_section_tree`与`validateModContentCategoryTree`：旧trigger的父链从parent计1并把root计入，因此直接第五层INSERT已被`parent_depth>=5`拒绝；但UPDATE不读取`new.id`后代 | 静态复核 | — | CONFIRMED WITH CORRECTION：原审计给出的直接INSERT路径不可达；同根因的带后代重挂仍可真实形成第五层，Finding不标NA |
| generation107真实PG subtree move RED | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestModContentCategoryDepthInvariantCoversSubtreeMovesIntegration$' -count=1` | 墙钟33.563s、包31.817s、用例30.96s | 1 | 预期RED：root-A1-A2-A3与root-B1-B2建立后，`A1.parent_id=B2`成功；最终A3为第五层 |
| generation108直接SQL与批量重建GREEN | `go test ./internal/database ./internal/httpapi -run '^(TestModContentCategoryDepthInvariantCoversSubtreeMovesIntegration&#124;TestPublishModContentLayoutSafelyRebuildsCategoryTreeIntegration)$' -count=1` | 墙钟36.205s；database33.709s；httpapi34.204s | 0 | PASS：越界重挂失败且A1仍在root；合法四层成功、直接第五层失败；批量从四层旧树删除三个后代并把保留节点重挂到第二层成功，最大深度2、3项archived、重复system key完整恢复且暂存key为0 |
| 四层预检与Schema源码门禁 | `go test ./internal/database ./internal/httpapi -run '^(TestModContentCategoryDepthIsAnAuthoritativeSubtreeInvariant&#124;TestValidateModContentCategoryTree)' -count=1` | 墙钟5.965s；database0.873s；httpapi0.122s | 0 | PASS：root=0的四个分类层合法、第五层返回稳定详情；Schema含version行锁、descendant CTE和联合深度上限 |
| generation108关键跨Finding真实回归 | 并行运行统计精确校准；创建事实+临时隔离；限制组合+proof；创建零授权+Yggdrasil | userstats35.742s；database66.352s；antiabuse69.741s；httpapi86.226s | 0 / 0 / 0 / 0 | PASS：BUG-005/006/007/008、SEC-008、TEST-004均在当前全Schema通过；隔离关闭后public generation仍为85且临时namespace零关系残留 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test11.181s；Vet2.764s；Build3.967s；tidy0.341s | 0 / 0 / 0 / 0 | PASS：generation108、树trigger、布局重建及全部后端模块闭集无回归 |
| 差异、陈旧路径与原审计完整性 | 目标tracked `git diff --check`；新增测试/修复文档`git diff --no-index --check`；旧generation107断言和旧父链-only trigger片段搜索；审计目录17文件逐一与原ZIP做SHA-256 | 检查0.702s；哈希0.8s | 0 / 0 | PASS：空白诊断0、陈旧匹配0；原审计17/17文件存在且HASH_MISMATCH=0，未修改源审计 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.653s；Allow-open0.624s | 1 / 0 | PASS：audit/ledger均精确449，144 CLOSED / 305 OPEN；allow-open通过；严格总门禁仅因305个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

四层边界现在由最终树结构决定，而不是只由一次写入的新父节点决定；整页保存与单项/直接写入共享同一个数据库权威，并且合法批量布局不依赖触发器看不见中间态。

## 2026-08-22：BUG-021 / TEST-009 统一审核队列完整可达关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 原路径复核 | `contentReviewQueueQuery`在权限条件后`order by created_at asc limit 2000`；Handler随后构造全量`accessible`并在Go执行搜索、facets、total与offset slice | 静态复核 | — | CONFIRMED：第2001项后不可分页/搜索，total/facets只描述前2000，内存随截断上限增长 |
| 完整13来源SQL合同 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestContentReviewQueueQueryIntegration$' -count=1` | 墙钟2.306s、包0.356s | 0 | PASS：远端只读generation85上全部UNION分支可编译执行，聚合items/total/三facet JSON可解码 |
| generation108 2005项结果语义 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^(TestContentReviewQueueTraversesAndSearchesBeyondLegacyCutoffIntegration&#124;TestContentReviewQueueQueryIntegration)$' -count=1` | 墙钟44.204s、包42.265s | 0 | PASS：源revision/request/branch各2005；offset2000返回#2001..#2005且total/facets=2005；搜索#2005为1；global URL/scope、project scope及禁止提交者自审全正确 |
| 数据库下推与参数边界 | `go test ./internal/httpapi -run '^Test(ReviewQueue&#124;ProjectScopedReviewer&#124;GlobalReviewer&#124;ProjectReviewIDs)' -count=1`及源码门禁 | 包1.134s | 0 | PASS：visible/filtered/total/facet/page均在SQL；排序`created_at,id,source`；固定2000、Go全量filter/facet路径零残留；offset保留非负int64，limit仍1..100 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test9.633s；Vet2.729s；Build3.545s；tidy0.343s | 0 / 0 / 0 / 0 | PASS：审核权限纯函数、队列合同、generation108及全部后端模块闭集无回归 |
| 差异与旧截断路径 | 目标tracked `git diff --check`；新增2005项测试`git diff --no-index --check`；生产搜索`limit 2000/filterReviewQueue/reviewQueueFacets` | 最终定向检查 | 0 | PASS：空白诊断0、新文件诊断0、旧生产路径0匹配 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.796s；Allow-open0.671s | 1 / 0 | PASS：audit/ledger均精确449，146 CLOSED / 303 OPEN；allow-open通过；严格总门禁仅因303个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

审核队列的容量现在由数据库中的真实pending集合定义，不再由隐藏常量定义。TEST-009已从“SQL能Scan”升级为生产Handler的完整性、分页、分面与权限行为矩阵；PERF-012随后已用独立100k/1M计划完成规模证明。

## 2026-08-22：BUG-022 精确项目审核历史与比较可见性关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 原权限路径复核 | `mod_revision_handlers.go`、`content_history_handlers.go`及三个项目族历史入口定向阅读 | 静态复核 | — | CONFIRMED：处理端接受精确`project.review.<projectID>`并禁止自审，Mod历史/比较却只认编辑或全局`project.review`；合法审核者无法读取自己可处理的修订 |
| 权限纯函数与数据库下推门禁 | `go test ./internal/httpapi -run '^Test(ProjectScopedReviewer&#124;GlobalReviewer&#124;ReviewQueue&#124;ProjectReviewIDs&#124;PendingReviewVisibility)' -count=1` | 包1.231s | 0 | PASS：精确ID、模板排除、跨项目、未知提交者、自审、编辑与全局审核矩阵正确；历史调用统一传递结构化visibility |
| generation108真实历史/比较Handler矩阵 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestProjectRevisionHistoriesHonorExactReviewScopeIntegration$' -count=1 -v` | 墙钟37.107s、用例36.99s | 0 | PASS：Mod历史与before/after比较、Modpack、plugin、pending changelog均用真实表与生产Handler验证；exact他人pending可见，self/cross拒绝，global保留完整访问 |
| 临时全Schema隔离 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟30.253s、用例30.17s | 0 | PASS：当前generation108只存在会话临时namespace；清理后远端public generation仍为85且临时关系零残留 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy`并比较前后hash | Test12.869s；Vet8.933s；Build10.021s；tidy0.415s | 0 / 0 / 0 / 0 | PASS：全部后端模块无回归；tidy未改变go.mod/go.sum |
| 差异与旧读取门禁 | 目标tracked `git diff --check`；新增集成测试gofmt/trailing-whitespace；旧Mod全局-only门禁与boolean历史调用搜索 | 最终定向检查 | 0 | PASS：tracked/new诊断均0；旧门禁0、旧boolean调用0；五个受影响生产文件中共有6处统一visibility调用 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.766s；Allow-open0.770s | 1 / 0 | PASS：audit/ledger均精确449，147 CLOSED / 302 OPEN；allow-open通过；严格总门禁仅因302个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

项目审核者现在只在拥有精确目标权限且修订来自他人时读取待审内容；读取历史、比较内容和处理队列共享同一项目/提交者边界，不再要求盲审，也不扩大自审或跨项目权限。

## 2026-08-22：BUG-023 审核提交与响应原子性关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| generation108真实PG回读失败RED | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestModRevisionReviewReadbackFailureRollsBackIntegration$' -count=1 -v` | 墙钟32.194s、用例32.07s | 1 | 预期RED：数组snapshot使`scanModRevision`解码失败，但旧Handler忽略错误；审核已rejected仍返回200，ID/status有值而snapshot为零值结构，精确证明提交/响应分裂 |
| generation108事务内响应GREEN | 同一命令最终版 | 墙钟30.981s、用例30.86s | 0 | PASS：畸形snapshot在commit前返回500，request仍pending、rejected event=0；正常snapshot返回200精确revision ID/status，request=rejected且event恰好1 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy`并比较前后hash | Test12.198s；Vet8.301s；Build9.345s；tidy0.427s | 0 / 0 / 0 / 0 | PASS：审核、通知、权限、generation108及全部后端模块无回归；tidy未改变go.mod/go.sum |
| 差异与旧吞错路径 | `mod_revision_handlers.go` tracked diff；新增集成测试gofmt/trailing-whitespace；旧`updated, _`/commit后回读搜索及语句顺序检查 | 最终定向检查 | 0 | PASS：tracked/new诊断均0；忽略回读0匹配；完整scan严格位于commit之前，200 write严格位于commit之后 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.650s；Allow-open0.782s | 1 / 0 | PASS：audit/ledger均精确449，148 CLOSED / 301 OPEN；allow-open通过；严格总门禁仅因301个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

审核结果现在只有一个成功边界：响应可完整构造且事务提交后才返回200；任何回读/解码故障都不会留下客户端无法安全重试的已审核事实。

## 2026-08-22：PERF-013 Mod修订历史数据库与DOM有界关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 原无界/相关子查询源码RED | `go test ./internal/httpapi -run '^TestModRevisionHistoryUsesBoundedKeysetPageAndJoinedReviewFacts$' -count=1` | 墙钟7.5s、包1.164s | 1 | 预期RED：缺page parser、limit+1、continuation和四个JOIN/LATERAL合同；提交者、base及三次latest review事件相关子查询全部仍命中 |
| 后端cursor与查询结构单测 | `go test ./internal/httpapi -run '^TestModRevisionHistory(CursorIsBoundToProjectAndLimit&#124;UsesBoundedKeysetPageAndJoinedReviewFacts)$' -count=1` | 包1.174s | 0 | PASS：project/limit作用域、超限、超长cursor失败关闭；limit+1/hasMore/nextCursor存在；四类旧相关查询0，latest event LATERAL恰好1 |
| generation108真实100k Handler与计划 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestModRevisionHistoryPaginatesOneHundredThousandRowsIntegration$' -count=1 -v` | 墙钟116.307s、用例115.14s、夹具83.406s | 0 | PASS：100k revisions/requests/events；首/次/深页为100000..99951、99950..99901、50..1且零重复；foreign cursor400；深页两个目标索引命中，无两表Seq Scan/SubPlan，执行0.377ms |
| BUG-022 / BUG-023真实跨Finding回归 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^Test(ProjectRevisionHistoriesHonorExactReviewScopeIntegration&#124;ModRevisionReviewReadbackFailureRollsBackIntegration)$' -count=1 -v` | 墙钟67.737s；readback31.69s；visibility35.93s | 0 | PASS：JOIN投影和默认首50页仍保持exact/self/cross/global权限、Mod比较及提交前响应回滚边界 |
| 前端分页RED / GREEN | bundled Node执行`node --test app/_lib/mod-history-pagination.test.mts` | RED147.685ms；GREEN132.328ms | 1 / 0 | PASS：请求带50硬上限和opaque cursor；选择跨页保持且最多2；组件使用cursor history、Previous/Next并删除裸无界请求 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy`并比较前后hash | Test12.448s；Vet8.565s；Build9.369s；tidy0.431s | 0 / 0 / 0 / 0 | PASS：pagination、审核、generation108及全部后端模块无回归；tidy未改变go.mod/go.sum |
| 前端全量门禁 | bundled Node/pnpm：`pnpm test`；`pnpm run lint`；`pnpm run typecheck`；以非秘密HTTPS占位部署URL执行`pnpm run build` | Test1.921s；Lint21.021s；Type3.329s；Build24.880s | 0 / 0 / 0 / 0 | PASS：74 tests；全仓ESLint/TS；Next16 production compile且58/58静态页生成；首次无部署URL构建按既有安全门正确退出1后以合规占位值复验通过 |
| 双仓差异与旧路径 | 后端目标tracked/new gofmt/diff；前端目标tracked/new diff/ESLint；旧相关子查询与裸历史请求搜索 | 最终定向检查 | 0 | PASS：双仓空白诊断0；后端相关lookup0、latest LATERAL1、limit+1路径1；前端裸无界请求0、cursor合同11处 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.764s；Allow-open0.753s | 1 / 0 | PASS：audit/ledger均精确449，149 CLOSED / 300 OPEN；allow-open通过；严格总门禁仅因300个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

Mod历史的每次数据库工作、响应体和浏览器表格现在都受100行服务端硬上限约束；100k深页由复合索引定位，审核事件只读取一次规范latest事实，跨页比较能力仍保留。

## 2026-08-22：SEC-012 站内OSS图片对象级复用授权关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 授权查询源码RED | `go test ./internal/httpapi -run '^TestReusableExternalImageObjectRequiresPurposeAndOwnerAuthorization$' -count=1` | 墙钟7.794s、包1.414s | 1 | 预期RED：旧复用查询同时缺`category=$2`、`source=$3`、`uploader_id=$4`，精确复现clean图片等同可复用授权 |
| 定向源码与图片安全回归 | `go test ./internal/httpapi -run '^(TestReusableExternalImageObjectRequiresPurposeAndOwnerAuthorization&#124;TestAllowedExternalModIconHosts&#124;TestExternalModIconFormatRequiresDecodableImage&#124;TestOSSObjectKeyUnderEndpoint)$' -count=1` | 墙钟7.457s、包1.161s | 0 | PASS：复用SQL锁定owner/category/source，可信Host、端点路径规范化及完整图片解码边界保持 |
| generation108真实PG授权矩阵 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestExternalImageReuseRequiresExactOwnerAndPurposeIntegration$' -count=1 -v` | 墙钟约38.1s、包33.629s、用例32.33s | 0 | PASS：exact owner+category+source返回原file ID；生产复用函数对other owner、other category、other source和actor 0全部在OSS读取前拒绝 |
| 临时全Schema隔离 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟约35.1s、用例34.86s | 0 | PASS：generation108只存在于会话临时namespace；清理后远端public generation仍为85且临时关系零残留 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test9.936s；Vet2.881s；Build3.720s；tidy0.355s | 0 / 0 / 0 / 0 | PASS：OSS、导入、generation108及全部后端模块无回归，无依赖漂移 |
| 差异与调用点 | 目标tracked/new gofmt与空白检查；生产搜索所有`reusableExternalImageObject`调用 | 最终定向检查 | 0 | PASS：两个调用都传递options+uploader；查询恰有owner/category/source授权；无旧无授权签名或调用 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.694s；Allow-open0.688s | 1 / 0 | PASS：audit/ledger均精确449，150 CLOSED / 299 OPEN；allow-open通过；严格总门禁仅因299个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

`clean`现在只回答“内容能否处理”，exact owner与目标用途才回答“此调用能否复用”；已知站内object key不再构成跨用户或跨类别的发布授权。

## 2026-08-22：REUSE-002 item/block系统分类单一事务权威关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 重复生产权威源码RED | `go test ./internal/httpapi -run '^TestItemBlockSystemCategoriesHaveOneProductionAuthority$' -count=1` | 墙钟7.765s、包1.173s | 1 | 预期RED：导入器未调用共享helper，旧创建/本地化错误路径及blocks/items两组locale literal共5项全部仍存在 |
| 单一权威源码GREEN | 同一命令最终版 | 墙钟7.448s、包1.172s | 0 | PASS：导入同步解析root后调用`ensureItemBlockSystemCategoriesTx`，旧两条SQL与文案literal零残留 |
| generation108真实PG入口/作用域 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestImportedItemBlockCategoriesUseSharedAuthorityIntegration$' -count=1 -v` | 墙钟约37.9s、包34.628s、用例33.48s | 0 | PASS：导入root得到blocks/items稳定顺序及六条权威locale；其他模板下同名blocks保持locale 0，证明删除version-wide误写 |
| 既有完整sync投影回归 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^(TestImportedContentSyncReconcilesOnlyItsSourceScopeIntegration&#124;TestItemBlockSystemCategoriesHaveOneProductionAuthority)$' -count=1 -v` | 墙钟5.431s、包2.370s、reconcile2.21s | 0 | PASS：完整`syncImportedResourcesToContentVersionTx`继续创建/复用分类并保持来源差异、人工事实和placement语义 |
| 临时全Schema隔离 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟约39.8s、用例39.71s | 0 | PASS：generation108只存在会话临时namespace；清理后远端public generation仍为85且临时关系零残留 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test10.006s；Vet2.987s；Build3.761s；tidy0.392s | 0 / 0 / 0 / 0 | PASS：系统分类、导入投影、generation108及全部后端模块无回归，无依赖漂移 |
| 差异与旧实现 | 目标tracked/new gofmt与空白检查；生产搜索helper调用与旧导入错误/literal | 最终定向检查1.0s | 0 | PASS：helper定义1+手工/导入调用2；权威locale literal只在共享文件1份；旧导入创建/本地化路径0；空白与gofmt诊断0 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.910s；Allow-open0.664s | 1 / 0 | PASS：audit/ledger均精确449，151 CLOSED / 298 OPEN；allow-open通过；严格总门禁仅因298个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

手工资料和导入资料现在不会各自决定何为blocks/items：两者把同一root事实交给同一事务权威，分类与文案只能一起演进。

## 2026-08-22：TEST-010 MODID确认完整安全协议矩阵关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| worker Mod绑定源码RED | `go test ./internal/httpapi -run '^TestMODIDConfirmationProtocolBindsEveryImporterAndJobMod$' -count=1` | 墙钟7.519s、包1.154s | 1 | 预期RED：job分析读取/转换没有`mod_id`绑定；两个导入入口已各自调用共享gate |
| worker绑定与纯分析GREEN | `go test ./internal/httpapi -run '^(TestMODIDConfirmationProtocolBindsEveryImporterAndJobMod&#124;TestAnalyzeImportMODIDs&#124;TestAnalyzeImportMODIDsRatiosAndNormalization)$' -count=1` | 墙钟7.070s、包0.149s | 0 | PASS：三个job语句绑定Mod；两个入口各唯一gate；既有规范化、排除namespace、主候选与20%多候选规则保持 |
| generation108真实HTTP回读RED | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestMODIDConfirmationProtocolIsBoundSingleUseAndTransactionalIntegration$' -count=1 -v` | 墙钟约36.8s、包32.774s、用例31.64s | 1 | 预期RED：合法确认事务已提交但返回500；直接回读报`configured_modids`真实`_text`不能binary scan到`[]byte`，旧JSONB影子测试掩盖生产类型 |
| generation108完整协议GREEN | 同一命令最终版 | 墙钟约36.4s、包35.621s、用例34.47s | 0 | PASS：wrong Mod/run token不改任务；wrong actor/路由Mod/hash、25h过期拒绝；合法202+唯一Outbox，重放409；同hash续跑、source变化重暂停；Outbox故障500且状态/事件回滚 |
| job响应真实类型回归 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestModExportJobResponseRejectsWrongJSONShapesIntegration$' -count=1 -v` | 墙钟2.926s、包0.484s、用例0.36s | 0 | PASS：影子Schema使用真实text[]；合法数组可读，NULL拒绝，error_detail数组仍失败关闭 |
| 临时全Schema隔离 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟约31.8s、用例31.76s | 0 | PASS：generation108只存在会话临时namespace；清理后远端public generation仍为85且临时关系零残留 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test9.853s；Vet2.909s；Build3.665s；tidy0.332s | 0 / 0 / 0 / 0 | PASS：MODID、Outbox、导入、generation108及全部后端模块无回归，无依赖漂移 |
| 差异与旧路径 | 目标tracked/new gofmt与空白检查；生产搜索worker三处+HTTP一处Mod绑定、两入口gate、旧`configuredMODIDs []byte` | 最终定向检查0.8s | 0 | PASS：Mod绑定4、入口调用2、旧数组扫描0、新text[]扫描1；空白/gofmt诊断0 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.665s；Allow-open0.660s | 1 / 0 | PASS：audit/ledger均精确449，152 CLOSED / 297 OPEN；allow-open通过；严格总门禁仅因297个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1 |

MODID确认现在是一次性、可重算且原子重入队的绑定协议；用户确认的永远是这一任务、这一Mod和这一份源数据的当前分析。

## 2026-08-22：PERF-014 Mod详情关系组N+1关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 单查询源码RED | `go test ./internal/httpapi -run '^TestModRelationshipGroupsLoadInOneOrderedQuery$' -count=1` | 墙钟7.124s、包1.179s | 1 | 预期RED：`loadModAssociations`仍有逐组循环，50组最多追加50条关系SQL |
| 单查询源码GREEN | 同一命令最终版 | 墙钟7.139s、包1.145s | 0 | PASS：关系组读取只有一个有序LATERAL联接，逐组查询循环零残留 |
| generation108最大边界真实PG | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestModAssociationsLoadFiftyGroupsInOneRelationshipQueryIntegration$' -count=1 -v` | 墙钟约34.3s、包33.146s、用例32.00s | 0 | PASS：50组/200存量关系只触发1条关系查询；组顺序稳定，pending-only首组仍以空关系返回，其余196条approved关系按序可见 |
| 临时全Schema隔离 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟约31.7s、用例30.61s | 0 | PASS：generation108只存在会话临时namespace；清理后远端public generation仍为85且临时关系零残留 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test10.197s；Vet2.806s；Build3.641s；tidy0.344s | 0 / 0 / 0 / 0 | PASS：关系详情装配及全部后端模块无回归，无依赖漂移 |
| 差异与旧路径 | 目标Go文件gofmt与空白检查；生产loader搜索逐组循环、关系组查询和LATERAL | 最终定向检查0.8s | 0 | PASS：逐组关系SQL零残留、关系组查询1、LATERAL 1；目标文件无gofmt或空白诊断 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.654s；Allow-open0.632s | 1 / 0 | PASS：audit/ledger均精确449，153 CLOSED / 296 OPEN；严格门禁仅因296个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1；allow-open通过 |

Mod详情的关系装配现在只有一次数据库往返，组数增长只扩大单一结果集，不再扩大查询次数。

## 2026-08-22：TEST-011 双项目关系所有权回归关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 既有安全RED证据 | SEC-013阶段`TestNormalizeModRequestTreatsIncomingRelationshipsAsReadOnly`及三项真实PG所有权测试 | 纯函数2.6s；PG批次10.0s | 1 / 1 | 预期RED：incoming仍进入权威snapshot，目标保存删除来源关系、伪造关系并删除对方反向声明；该证据正是TEST-011原缺口所锁定的错误模型 |
| 双编辑者定向GREEN | `go test ./internal/httpapi -run '^TestNormalizeModRequestTreatsIncomingRelationshipsAsReadOnly$' -count=1`；task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestDifferentProjectEditorsCannotRewriteIncomingRelationshipOwnershipIntegration$' -count=1 -v` | 源码墙钟6.776s、包1.152s；PG墙钟2.729s、包0.759s、用例0.64s | 0 / 0 | PASS：source/target两位编辑者只能编辑各自project code；目标payload的incoming组不进入snapshot，来源关系ID保留且伪造conflict为0 |
| 完整关系安全矩阵 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^(TestNormalizeModRequestTreatsIncomingRelationshipsAsReadOnly&#124;TestDifferentProjectEditorsCannotRewriteIncomingRelationshipOwnershipIntegration&#124;TestIncomingModRelationshipSnapshotCannotRewriteSourceProject&#124;TestSymmetricModRelationshipDoesNotDeleteTargetOwnedReverse&#124;TestOutgoingModRelationshipRejectsUnpublishedTarget)$' -count=1 -v` | 墙钟5.365s、包2.545s | 0 | PASS：双编辑者、直接incoming、对称反向所有权和pending目标五项同时通过；数据库用例逐项事务回滚 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 最终Test6.742s；Vet5.521s；Build5.377s；tidy0.423s | 0 / 0 / 0 / 0 | PASS：首轮并发门禁仅JetStream durable restart用例发生一次已ACK消息重投；该用例隔离复跑2.183s通过，随后全仓完整复跑通过，未忽略失败 |
| 差异与格式 | 新关系安全测试gofmt；目标测试/文档空白检查；旧“incoming为权威可写组”断言搜索 | 最终检查0.9s | 0 | PASS：无gofmt或空白错误；旧可写测试名/断言0，新双编辑者测试1 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.662s；Allow-open0.622s | 1 / 0 | PASS：audit/ledger均精确449，154 CLOSED / 295 OPEN；strict仅因295个其余Finding、141个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

关系回归现在同时证明“谁可编辑哪个项目”和“目标项目的incoming展示不能成为来源项目的写能力”。

## 2026-08-22：BUG-024 / TEST-012 资料Schema引用生命周期关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 事务守卫源码RED | `go test ./internal/httpapi -run '^(TestTemplateSchemaGuardIsWiredToBuiltinAndCustomPublication&#124;TestRemovedModContentEntryTypeCodesAreNormalized)$' -count=1` | 墙钟1.733s | 1 | 预期RED：删除类型识别、内建/自定义事务守卫和删除守卫均不存在，测试包编译失败 |
| 源码/纯函数GREEN | `go test ./internal/httpapi -run '^(TestTemplateSchemaGuardIsWiredToBuiltinAndCustomPublication&#124;TestRemovedModContentEntryTypeCodesAreNormalized&#124;TestDisabledEntryTypeIsReadableOnlyForExistingDetails&#124;TestResourceAttributeSchemaPreservesStableIDsAndStorageTypes&#124;TestConfiguredEntryTypeMatchingSkipsDisabledTypes)$' -count=1 -v` | 墙钟6.733s、包1.136s | 0 | PASS：两条发布路径及删除路径唯一接入守卫；删除代码规范化；字段改型拒绝；disabled新选择拒绝、既有解释允许、导入匹配跳过 |
| generation108真实PG生命周期 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestTemplateSchemaGuardDistinguishesDisableDeleteAndReferencesIntegration$' -count=1 -v` | 最终墙钟35.533s、包33.569s、用例33.43s | 0 | PASS：活动及archived引用不能删类型，disabled可发布且既有读取成功/新选择失败，存储改型拒绝；无引用类型可删；自定义真实publish/delete分别拒绝引用项并接受无引用项 |
| 夹具校正 | 扩展真实publish后的前两次运行 | 包31.832s / 39.628s | 1 / 1 | 非产品失败：测试JSON漏写既有`kindCodes`，稳定签名正确拒绝夹具自行引入的kind变化；补齐与生产current schema一致的kind后完整矩阵通过，未把夹具失败计作修复证据 |
| 临时全Schema隔离 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟34.296s、用例32.84s | 0 | PASS：generation108只存在会话临时namespace；清理后远端public generation仍为85且临时关系零残留 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test12.658s；Vet8.557s；Build9.611s；tidy0.414s | 0 / 0 / 0 / 0 | PASS：最终disabled既有详情语义、模板守卫及全部后端模块无回归，无依赖漂移 |
| 差异与旧测试 | 目标Go文件gofmt/空白；搜索旧“整类删除必须成功”断言；发布路径守卫计数 | 最终检查0.8s | 0 | PASS：gofmt/空白诊断0；旧删除接受断言0；内建/自定义schema守卫调用2，custom删除守卫1 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.660s；Allow-open0.634s | 1 / 0 | PASS：audit/ledger精确449，156 CLOSED / 293 OPEN；High95 / Medium166 / Low49 / UNRESOLVED139；strict仅因293个其余Finding、139个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

模板配置现在能停用未来选择，却不能再抹掉现有资料赖以解释字段和值的Schema。

## 2026-08-22：ARCH-033 / PERF-062 管理权限完整读取与固定查询预算关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 错误传播源码RED | `go test ./internal/httpapi -run '^TestAdminAuthorizationReadsPropagateEveryDatabaseError$' -count=1` | 墙钟6.830s、包1.132s | 1 | 预期RED：role权限reader仍返回单值并把query/scan错误变成空或部分列表；createRole仍把全部INSERT错误映射重复代码 |
| 错误传播/批量源码GREEN | 同一命令最终版 | 墙钟6.902s、包1.221s | 0 | PASS：role reader返回error并检查Scan/rows.Err；列表与详情传播；adminUsers检查rows.Err并唯一调用批量resolver；createRole只识别唯一冲突 |
| 单连接真实PG性能RED | generation108临时Schema首次运行真实故障夹具 | 墙钟127.424s、包121.729s、用例120.59s | 1 | 有效PERF-062复现：roles持有外层cursor时逐role向MaxConns=1池再查询，等待到context deadline；证明查询数量和连接需求随role数增长，不计作通过 |
| generation108真实PG最终矩阵 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestAdminAuthorizationDatabaseFailuresAreNotEditableEmptyDataIntegration$' -count=1 -v` | 墙钟39.776s、包34.121s、用例32.98s | 0 | PASS：100用户页固定7查询，大role目录固定3查询；非法expires_at令目录500且roleByCode返回error；非唯一role INSERT trigger返回500 |
| 故障夹具校正 | 批量查询完成后的第一次故障注入运行 | 墙钟40.395s、包34.724s、用例33.59s | 1 | 非产品失败：`to_jsonb(valid timestamptz)`仍可被pgx解码为time.Time，未真正触发Scan错误；将目标值改为`"not-a-time"`后最终矩阵命中真实Scan失败并通过 |
| 临时全Schema隔离 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟31.560s、用例30.10s | 0 | PASS：generation108只存在会话临时namespace；清理后远端public generation仍为85且临时关系零残留 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test13.495s；Vet8.417s；Build9.309s；tidy0.392s | 0 / 0 / 0 / 0 | PASS：权限读取、批量解析及全部后端模块无回归，无依赖漂移 |
| 差异与旧路径 | 目标Go文件gofmt/空白；搜索adminUsers逐用户resolver、role逐项赋值与空数组吞错 | 最终检查0.8s | 0 | PASS：目标格式/空白错误0；adminUsers逐用户调用0；roles逐role数据库调用0；query失败空数组返回0 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.643s；Allow-open0.641s | 1 / 0 | PASS：audit/ledger精确449，158 CLOSED / 291 OPEN；High95 / Medium166 / Low49 / UNRESOLVED139；strict仅因291个其余Finding、139个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

后台现在只会返回完整权限事实或明确失败；角色数和用户页大小不会再线性增加权限查询往返。

## 2026-08-22：TEST-049 后台权限写入行为矩阵关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 第一轮夹具校正 | generation108临时全Schema初次运行 | 墙钟38.310s、用例31.32s | 1 | 非产品失败：pgx扩展协议拒绝在一个prepared Exec内发送多条SQL；拆成独立参数化语句后继续，不计作产品RED |
| 第二轮故障注入校正 | 审计CHECK首次安装 | 墙钟43.400s、用例36.57s | 1 | 非产品失败：普通CHECK会扫描已有合法审计行；改用`NOT VALID`后只拒绝后续目标写入，最终真实命中事务故障 |
| generation108真实HTTP/PG最终矩阵 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestAdminAuthorizationWritesAreTransactionalAndImmediatelyEffectiveIntegration$' -count=1 -v` | 墙钟48.007s、包42.334s、用例41.16s | 0 | PASS：真实用户权限PUT和角色POST/PUT/DELETE；来源/expiry、DAG/轨道、审计回滚、版本/同Session即时生效、并发替换、Redis故障回落及写预算全部通过 |
| 聚焦纯函数/源码合同 | `go test ./internal/httpapi -run 'Test(ValidateRoleGraphRejectsMissingParentsAndCycles&#124;NormalizeManualAuthorizationEntries&#124;LegacyUserRoleReplacementRouteIsRemoved&#124;AdminAuthorizationReadsPropagateEveryDatabaseError)$' -count=1` | 墙钟2.076s、包0.120s | 0 | PASS：DAG、manual边界、旧角色替换路由删除和完整读取错误传播继续受保护 |
| PostgreSQL隔离 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟33.736s、包32.372s、用例32.29s | 0 | PASS：generation108对象只存在于会话临时namespace；清理后远端public generation85且零临时残留 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test6.774s；Vet2.711s；Build2.718s；tidy0.339s | 0 / 0 / 0 / 0 | PASS：默认测试正确跳过显式DB门禁，其余全仓、静态分析、构建与模块图无回归 |
| 差异空白 | 新测试gofmt、`git diff --check -- internal/httpapi/admin_authorization_write_integration_test.go` | 0.7s | 0 | PASS：新测试格式/空白错误0，仅既有Windows行尾提示 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.638s；Allow-open0.642s | 1 / 0 | PASS：audit/ledger精确449，159 CLOSED / 290 OPEN；High95 / Medium166 / Low49 / UNRESOLVED139；strict仅因290个其余Finding、139个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

权限管理现在有可重复的跨层写入门禁：失败不会留下无审计授权，成功不会使当前Session失效或等待缓存TTL才生效。

## 2026-08-22：OPS-021 负载观察器配置与失败退出关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 修复前RED | `go test ./cmd/load-observer -count=1` | 墙钟0.908s | 1 | 预期RED：无可测试runner/runtime；数据库、采样、编码、文件错误传播合同全部不存在，测试编译明确失败 |
| runner行为与文档GREEN | `go test ./cmd/load-observer -count=1 -v` | 最终墙钟2.621s、包1.314s | 0 | PASS：数据库Ping、采样超时、JSON编码、显式文件写失败均返回error；成功4样本、峰值和pool关闭精确；环境示例/文档/main非零接线受门禁 |
| 真实CLI显式输出故障 | task-scoped DSN、`MCMODS_OBSERVER_SECONDS=1`、不存在父目录的`MCMODS_OBSERVER_OUTPUT`后`go run ./cmd/load-observer` | 3.287s | 1 | PASS：真实读取窗口完成后WriteFile报路径不存在，未创建报告且CLI非零；包装检查按预期退出0 |
| 真实CLI数据库不可达 | `DATABASE_URL=postgres://observer:***@127.0.0.1:1/observer?... go run ./cmd/load-observer` | 0.799s | 1 | PASS：Ping立即报告连接拒绝并非零，不等待或输出伪造零样本报告；记录未暴露真实凭据 |
| 配置/旧吞错/差异检查 | 搜索`DATABASE_URL/MCMODS_OBSERVER_*`与Marshal/WriteFile调用；`git diff --check -- cmd/load-observer .env.example docs/load-observer.md` | 0.7s | 0 | PASS：三项配置均有示例和权威说明；`payload,_`/`_ = os.WriteFile`零残留；仅既有Windows行尾提示 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test6.716s；Vet1.920s；Build3.100s；tidy0.337s | 0 / 0 / 0 / 0 | PASS：命令、全仓、静态分析、构建和模块图无回归 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.641s；Allow-open0.638s | 1 / 0 | PASS：audit/ledger精确449，160 CLOSED / 289 OPEN；High95 / Medium166 / Low49 / UNRESOLVED139；strict仅因289个其余Finding、139个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

负载观察产物现在具备清晰的成功定义：连接、全部采样和所要求的输出都成功，进程才返回零。

## 2026-08-22：TEST-050 负载统计与代表性查询证据关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 合同测试RED | bundled Node执行`node --test scripts/load-test-contract.test.mjs scripts/load-test-cli.test.mjs` | 墙钟0.250s、Node 89.5ms | 1 | 预期RED：分类/汇总模块不存在，精确失败率、场景合同、最低量、必见code和一次性请求池均无实现 |
| Node语法、纯函数与真实CLI GREEN | `node --check scripts/load-test.mjs`；bundled Node执行两份测试 | 最终墙钟4.441s、Node 3.804s | 0 | PASS：6/6；23/14613与29/15245六位比率；普通200成功、普通429非零、结构化反滥用429在满足成功/拒绝/code阈值时成功；查询/文档静态合同通过 |
| 第一轮真实PG夹具校正 | generation108临时全Schema初次运行 | 包29.062s、用例28.98s | 1 | 非产品失败：pgx扩展协议拒绝带参数的多语句prepared Exec；拆成独立参数化bulk insert后继续，不计作产品RED |
| generation108真实100k查询计划 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/antiabuse -run '^TestAntiAbuseQueryPlans$' -count=1 -v` | 最终包38.795s、用例37.84s | 0 | PASS：事件、指纹、限制各100k；分别命中`idx_anti_abuse_events_action_time`、`idx_anti_abuse_fingerprint_user_action`、`idx_anti_abuse_restrictions_user_active`，无目标表Seq Scan；执行0.455/0.504/0.048ms并含Buffers |
| PostgreSQL隔离 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 包31.685s、用例31.60s | 0 | PASS：generation108只存在会话临时namespace；清理后远端public generation85且零临时残留 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test9.034s；Vet3.956s；Build5.179s；tidy0.828s | 0 / 0 / 0 / 0 | PASS：默认门禁安全跳过显式数据库规模测试，其余全仓、静态分析、构建和模块图无回归 |
| 格式、差异与历史样本 | 目标Go文件gofmt；目标脚本/SQL/测试/文档`git diff --check`；核对`test-results/load`未修改 | 最终0.554s | 0 | PASS：无空白错误，仅既有Windows行尾提示；历史JSON零改写，新文档明确其不可冒充自动门禁 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.937s；Allow-open0.928s | 1 / 0 | PASS：audit/ledger精确449，161 CLOSED / 288 OPEN；High95 / Medium166 / Low49 / UNRESOLVED139；strict仅因288个其余Finding、139个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

负载产物现在能区分正常风控与真实失败，并在失败比例很小时仍可靠阻断；历史样本保持诚实的人工证据边界。

## 2026-08-22：SEC-045 前端CSP精确来源关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| CSP合同RED | bundled Node执行`node --test app/_lib/csp-policy.test.mts` | 墙钟0.519s、Node127.5ms | 1 | 预期RED：共享策略模块不存在；生产精确origin、恶意配置拒绝、开发loopback和代理/文档接线均无实现 |
| 中间安全断言 | 首版解析器后的同一测试 | 墙钟0.512s、Node131.2ms | 1 | 有效实现缺口：标准URL解析会接受`https://*.example.test`为hostname；新增显式`*`拒绝后继续，未弱化测试或把通配加入兼容路径 |
| CSP定向GREEN | bundled Node执行`node --test app/_lib/csp-policy.test.mts` | 最终墙钟0.572s、Node147.0ms | 0 | PASS：4/4；生产四directive无裸`https:`，API/OSS/Turnstile/资源闭集精确；路径、凭据、query、通配、非HTTPS拒绝；开发仅loopback放宽 |
| 前端全量测试 | bundled pnpm `test` | 墙钟1.961s、Node1.110s | 0 | PASS：78/78，新增CSP回归已纳入默认测试且既有部署、Iconfont、路由、状态与安全测试无回归 |
| TypeScript / ESLint | bundled pnpm `typecheck`；`lint`（显式加入bundled Node PATH） | Type3.170s；Lint21.361s | 0 / 0 | PASS：首次未注入Node PATH的pnpm包装分别0.728s/0.850s退出1且只报告找不到node；修正runner环境后源码与规则全绿 |
| 生产构建 | HTTPS API/SITE及精确OSS connect、图片origin下bundled pnpm `build` | 墙钟23.299s；编译7.5s、TS9.6s | 0 | PASS：Next.js 16.2.11生产构建、58个静态页及Proxy middleware完成；无宽泛来源回退即可构建 |
| 差异与旧策略 | `git diff --check`覆盖Proxy/CSP helper/test/环境示例/README/package；搜索四directive裸scheme | 最终检查0.6s | 0 | PASS：无空白错误，仅既有Windows行尾提示；旧connect/img/font/media `https:` scheme来源零残留 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.932s；Allow-open1.055s | 1 / 0 | PASS：audit/ledger精确449，162 CLOSED / 287 OPEN；High95 / Medium166 / Low49 / UNRESOLVED139；strict仅因287个其余Finding、139个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

前端现在只向部署明确授权的能力域发送请求或加载资源；补充OSS/CDN不再要求重新开放整个HTTPS scheme。

## 2026-08-22：SEC-046 Cookie Session上传恢复账号隔离关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 前端合同RED | bundled Node执行`node --test app/_lib/mod-export-upload-store.test.mts` | 墙钟1.059s、Node690.7ms | 1 | 预期RED：store没有subject所有权predicate，固定`cookie-session`仍解析/回退为共享`session`，测试在缺少导出时失败 |
| 前端定向GREEN | 同一命令最终版；目标组件ESLint；pnpm typecheck | 最终Node139.1ms；Lint4.179s；Type3.135s | 0 / 0 / 0 | PASS：3/3；两用户key不同，非法public ID拒绝，缺失/错subject/key均失败关闭；manager传递user.id，切号同时abort upload/poll，所有storage操作带subject |
| React并发规则中间门禁 | 第一版完整ESLint | 墙钟23.958s | 1 | 有效实现缺口：render写/读ref、effect同步reset及先引用后声明被React 19 compiler拒绝；改为layout subject失效、显式hasActiveTask状态、异步恢复effect并重排函数后通过，未加规则豁免 |
| 后端owner源码合同 | `go test ./internal/httpapi -run 'TestModExportUploadRecoverySourcesCarryAuthenticatedOwner' -count=1` | 墙钟7.273s、包1.175s | 0 | PASS：A/B object category含不同`owners/{id}`；create/resume均调用owner helper；active query含created_by且指定job使用creator reader |
| 第一轮真实PG夹具校正 | generation108临时全Schema初次双账号运行 | 用例38.30s | 1 | 非产品失败：package夹具遗漏生产NOT NULL `schema_version`；补齐schema/exporter/Minecraft元数据后原测试不变继续，不计作安全RED |
| generation108真实PG双账号 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'TestModExportRecoveryIsIsolatedAcrossTwoAccountsIntegration' -count=1 -v` | 包32.515s、用例31.37s | 0 | PASS：A读A成功、B读A为pgx.ErrNoRows、B active不发现A；B创建自身任务后只恢复B；MaxConns=1临时全Schema销毁且public库无DDL |
| 前端全量 / Type / Lint / Build | bundled pnpm `test`、`typecheck`、`lint`；HTTPS生产变量下`build` | Test2.187s/Node1.254s；Type3.343s；Lint23.488s；Build23.852s | 0 / 0 / 0 / 0 | PASS：81/81；TypeScript与React/ESLint全绿；Next.js 16.2.11编译7.8s、TS10.7s并生成58页 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | Test12.680s；Vet8.713s；Build9.524s；tidy/差异1.282s | 0 / 0 / 0 / 0 | PASS：默认测试安全跳过显式PG门禁，其余全仓、静态分析、构建和模块图无回归 |
| 差异与旧身份路径 | 双仓目标文件`git diff --check`；搜索`tokenSubject/exporter:session`及无owner resume前缀 | 0.6s | 0 | PASS：目标空白错误0，仅既有Windows行尾提示；token解析/共享session及旧resume前缀零残留 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.959s；Allow-open1.078s | 1 / 0 | PASS：audit/ledger精确449，163 CLOSED / 286 OPEN；High95 / Medium166 / Low49 / UNRESOLVED139；strict只因286个其余Finding、139个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

可恢复上传现在有两层独立身份边界：浏览器按认证public subject隔离状态，服务器按内部owner和job creator重新授权；切换账号不会继承上一账号的ZIP、ticket或后台任务。

## 2026-08-22：PERF-001 编辑员申请目标名称N+1关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 单连接真实PG RED | generation108临时全Schema后`TestProjectEditorApplicationListUsesConstantQueriesIntegration`旧实现 | 包50.274s、用例49.06s、查询deadline2s | 1 | 预期RED：application rows占用唯一连接，首个逐项`projectEditorTargetName`无法取得第二连接并返回`timeout: context deadline exceeded`；此前无deadline运行已持续约90s后人工终止 |
| 默认门禁编译/跳过 | `go test ./internal/httpapi -run '^TestProjectEditorApplicationListUsesConstantQueriesIntegration$' -count=1` | 墙钟7.258s、包1.160s | 0 | PASS：未显式启用DB时安全跳过集成体，生产SQL及夹具可编译 |
| generation108真实PG GREEN | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1`执行同一测试 | 包34.527s、用例34.40s | 0 | PASS：MaxConns=1；100用户/申请全部返回精确目标名，query tracer恰为2（列表+附件），无逐项连接申请 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | Test12.811s；Vet8.985s；Build9.873s；tidy/差异0.863s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建与模块图无回归 |
| 格式、差异与旧循环 | gofmt；目标文件`git diff --check`；搜索rows循环内`projectEditorTargetName` | 0.9s | 0 | PASS：空白错误0，仅既有Windows行尾提示；列表逐项名称查询零残留，单目标创建/审核helper按原职责保留 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict1.026s；Allow-open0.917s | 1 / 0 | PASS：audit/ledger精确449，164 CLOSED / 285 OPEN；High95 / Medium166 / Low49 / UNRESOLVED139；strict只因285个其余Finding、139个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

编辑员申请列表的数据库往返不再随页项数增长，并且单连接部署不会因嵌套pool查询自我饥饿。

## 2026-08-22：PERF-005 活动清理预览单次有界扫描关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| SQL/边界合同RED | `go test ./internal/httpapi -run 'TestActivityCleanupPreview(UsesOneBoundedMaterializedCandidateSet&#124;RejectsOnlySyntacticallyUnboundedFilters)$'` | 墙钟2.090s | 1 | 预期RED：单物化候选SQL与无界过滤predicate均未定义，测试编译失败；未把旧五查询行为当作通过 |
| 定向GREEN与默认集成跳过 | 同一2项测试；随后包含scale集成名的默认命令 | 最终墙钟7.237s、包1.165s | 0 | PASS：单一活动表来源、MATERIALIZED、动态limit、聚合/样本复用及五类合法边界受门禁；未显式DB开关时规模用例安全跳过 |
| 真实PG 100k/1M/10M计划 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestActivityCleanupPreviewPlansStayBoundedAtScaleIntegration$' -count=1 -v` | 包47.367s、用例47.25s | 0 | PASS：MaxConns=1 TEMP影子表；三规模summary各1条SQL；100k完整聚合/10样本/timestamp精确，基础扫描actual rows为100000/100001/100001，EXPLAIN ANALYZE为343.103/344.273/347.605ms |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy`后SHA-256差异 | Test12.268s；Vet8.559s；Build9.216s；tidy/差异0.859s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建和模块图无回归；go.mod/go.sum字节不变 |
| 格式、差异与旧扫描helper | gofmt；目标文件`git diff --check`；搜索旧`activityCleanupSamples/activityCleanupGroupedCounts`函数 | 0.564s | 0 | PASS：空白错误0，仅既有Windows行尾提示；两个旧预览helper零残留，执行清理自身的批次查询不在本Finding范围内 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict1.021s；Allow-open0.880s | 1 / 0 | PASS：audit/ledger精确449，165 CLOSED / 284 OPEN；High95 / Medium166 / Low49 / UNRESOLVED139；strict只因284个其余Finding、139个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

活动清理预览现在只付出一个有硬上限的候选读取成本；无法安全生成精确预览的范围会在任何删除确认事实落库前失败关闭。

## 2026-08-22：PERF-008 合成详情binding候选N+1关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 夹具校正 | 真实PG首两次测试装载 | 包0.398s / 1.604s | 1 / 1 | 非产品失败：扩展协议拒绝参数化多语句prepared Exec，拆为四条装载；随后两个unknown参数相加运算符不唯一，增加显式bigint cast。查询断言未运行，不计作产品RED |
| MaxConns=1真实PG RED | task-scoped DSN后运行`TestCatalogRecipeBindingsUseOneQueryAtMaximumSlotScaleIntegration`旧实现 | 包3.661s、用例2.48s、query deadline2s | 1 | 预期RED：仅1个binding时outer rows占用唯一连接，首个逐binding候选查询返回`context deadline exceeded` |
| 真实PG零/1/64/512槽位GREEN | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCatalogRecipeBindingsUseOneQueryAtMaximumSlotScaleIntegration$' -count=1 -v` | 包2.054s、用例0.91s | 0 | PASS：零候选binding保留；三规模均逐slot完整，末slot两个候选顺序、resolved icon、unresolved raw ID、nullable probability精确；每次reader恰1 SQL |
| 目录定向与默认跳过 | 新集成测试加催化剂/坏JSON既有回归；未设置DB开关 | 墙钟2.595s、包0.119s | 0 | PASS：默认门禁安全跳过PG夹具，相关目录批量/错误行为无回归 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy`后SHA-256差异 | Test12.583s；Vet8.598s；Build9.464s；tidy/差异0.846s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建和模块图无回归；go.mod/go.sum字节不变 |
| 格式、差异与旧helper | gofmt；目标文件`git diff --check`；搜索`catalogRecipeCandidateRows` | 0.628s | 0 | PASS：空白错误0，仅既有Windows行尾提示；逐binding候选helper与调用零残留 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict1.067s；Allow-open0.923s | 1 / 0 | PASS：audit/ledger精确449，166 CLOSED / 283 OPEN；High95 / Medium166 / Low49 / UNRESOLVED139；strict只因283个其余Finding、139个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

合成编辑详情的数据库成本现在与槽位数量解耦；小连接池也不会因嵌套读取自行耗尽。

## 2026-08-22：PERF-009 导入模板推广slot读取N+1关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 常量读取合同RED | `go test ./internal/httpapi -run '^TestImportedRecipeTemplatePromotionReadsStayConstantAtScaleIntegration$' -count=1` | 墙钟2.115s | 1 | 预期RED：`loadImportedRecipeTemplatePromotions`未定义，旧promotion函数只能在循环内逐template读slot，测试编译失败 |
| 测试断言校正 | 首轮真实PG GREEN候选 | 包0.729s、用例0.60s | 1 | 非产品失败：1-template夹具的首项同时也是带第二slot的末项，旧断言错误要求其只有1 slot；改为逐项按“末项2、其余1”验证，产品实现未改变 |
| 真实PG零/1/64/512模板GREEN | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestImportedRecipeTemplatePromotionReadsStayConstantAtScaleIntegration$' -count=1 -v` | 包2.241s、用例1.09s | 0 | PASS：MaxConns=1 TEMP影子表；零slot模板保留，三规模模板/slot数量与output index顺序精确；snapshot read和slot read均由同一SQL各计1次 |
| JEI模板/绑定定向回归 | 新集成测试加queue、compact、canonical geometry/identity、layout/chance验证测试 | 墙钟2.372s、包0.118s | 0 | PASS：推广读取与既有文档验证、身份、几何和候选规则无回归；默认未开DB时规模夹具安全跳过 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy`后SHA-256差异 | Test12.476s；Vet8.673s；Build9.392s；tidy/差异0.860s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建和模块图无回归；go.mod/go.sum字节不变 |
| 格式、差异与旧slot循环 | gofmt；目标文件`git diff --check`；搜索逐模板`templateSnapshotID`及`where template_id=$1`查询 | 0.552s | 0 | PASS：空白错误0，仅既有Windows行尾提示；旧计算ID/逐模板slot读取零残留 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict1.008s；Allow-open0.871s | 1 / 0 | PASS：audit/ledger精确449，167 CLOSED / 282 OPEN；High95 / Medium166 / Low49 / UNRESOLVED139；strict只因282个其余Finding、139个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

活动revision推广现在先用一次读取固定完整输入，再进入原子写batch；读取往返不再延长模板规模相关的锁窗口。

## 2026-08-22：PERF-012 统一审核队列100k/1M规模验收关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 当前根因复核 | `contentReviewQueueQuery`与D-104/BUG-021/TEST-009证据 | 静态复核 | — | CONFIRMED FIX PENDING SCALE：旧2000预截断/Go filter/facet已零残留，queue/visible/filtered/page/total/facet在单statement；PERF-012只缺明确保留的100k/1M计划门 |
| generation108真实100k深页/计划 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestContentReviewQueueScalesToOneHundredThousandPendingItemsIntegration$' -count=1 -v` | 包130.564s、用例130.44s；查询541.056ms | 0 | PASS：当前全Schema约束夹具100k revision+request；offset99900精确返回q00099901..q00100000、total/facet100k；statement1，基表examined各100k；TEMP read/write13256/9661 blocks |
| 真实PG TEMP 1M深页/计划 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestContentReviewQueueScalesToOneMillionPendingItemsIntegration$' -count=1 -v` | 包18.462s、用例18.35s；查询5419.549ms | 0 | PASS：1M读取列/索引形状影子；offset999900精确返回q00999901..q01000000、total1M；statement1，基表examined各1M，低于1.1倍门；TEMP read/write159057/123114 blocks，低于10s回归线 |
| 审核队列定向/默认跳过 | `go test ./internal/httpapi -run '^Test(ReviewQueue&#124;ContentReviewQueue)' -count=1` | 墙钟2.476s、包0.131s | 0 | PASS：权限、数据库下推合同及新增规模测试默认安全跳过；既有2005项Handler语义证据继续保留 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy`后SHA-256差异 | Test8.410s；Vet4.649s；Build4.411s；tidy/差异0.788s | 0 / 0 / 0 / 0 | PASS：仅新增规模门禁；全仓、静态分析、构建和模块图无回归，go.mod/go.sum字节不变 |
| 格式与差异 | 新测试gofmt；`git diff --check -- internal/httpapi/review_queue_scale_integration_test.go` | 0.6s | 0 | PASS：空白错误0；原审计和历史2005项证据未改写 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict1.061s；Allow-open0.911s | 1 / 0 | PASS：audit/ledger精确449，168 CLOSED / 281 OPEN；High95 / Medium166 / Low49 / UNRESOLVED139；strict只因281个其余Finding、139个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

统一审核队列现在同时具备完整可达的功能矩阵和可重复的100k/1M计划门；任何恢复固定截断或重复基础扫描的修改都会失败。

## 2026-08-22：PERF-015 公开板块资源有界读取关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 旧协议与根因回扫 | 目标前后端搜索`all=1`、20,000页、旧all-loader、offset及旧搜索helper | 0.8s | 0 | PASS：生产路径零残留；唯一all/offset命中是明确要求400的集成回归用例 |
| generation109空库安装 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 包32.789s、用例32.71s | 0 | PASS：会话临时完整Schema安装到109；search_document、GIN及拆分的revision update/delete刷新触发器均可安装；远端public generation85未修改 |
| 350项cursor/搜索/图与100k/1M计划 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestModContentSectionResourceCursorAndScaleIntegration$' -count=1 -v` | 包52.590s、用例51.32s | 0 | PASS：47项分页完整遍历无重漏，categories仅首屏；搜索本地化更新前后精确刷新；旧参数/短q/跨scope cursor均400；图流字段/缓存/1MiB预算通过；100k生产handler首/深/search为636.940/161.643/273.650ms；1M keyset Index Only 0.071ms、GIN Bitmap 0.053ms且无Seq Scan |
| 相关资料生命周期回归 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestModContentResourceLifecycleIntegration$' -count=1 -v` | 包35.735s、用例35.61s | 0 | PASS：既有资料完整生命周期在generation109临时Schema继续通过 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy`后go.mod/go.sum差异 | Test15.1s；Vet3.816s；Build4.857s；tidy0.9s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建和模块图无回归，go.mod/go.sum字节不变 |
| 前端分页源合同 / 全量门禁 | `pnpm test`；`pnpm run lint`；`pnpm run typecheck`；带`NEXT_PUBLIC_API_BASE_URL`和`NEXT_PUBLIC_SITE_URL`的`pnpm run build` | Test2.261s；Lint23.566s；Type3.382s；Build24.0s | 0 / 0 / 0 / 0 | PASS：84 tests、分页/精简流源合同、类型与lint全绿；production build生成58/58页面。首次缺必需公开URL的build按配置合同正确退出1，不计作产品失败 |
| 格式与差异 | gofmt；双仓`git diff --check`；旧协议/helper定向检索 | <1s | 0 | PASS：空白错误0，仅既有Windows行尾提示；旧深分页和宽all调用零残留 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.638s；Allow-open0.628s | 1 / 0 | PASS：audit/ledger精确449，169 CLOSED / 280 OPEN；High96 / Medium166 / Low49 / UNRESOLVED138；strict只因280个其余Finding、138个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

公开资料的普通卡片、全进度图和编辑布局现在分别有稳定分页、索引搜索、响应字节、缓存与速率边界；百万规模不再依赖深OFFSET或宽对象全集。

## 2026-08-22：PERF-016 资料引用逐标识SQL关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 首轮真实PG夹具校正 | generation109临时全Schema上的项目/未解析tag夹具 | 34.03s / 49.92s | 1 / 1 | 非性能RED：首轮project_code不是9位而在进入函数前违反约束；修正后旧tag INSERT因metadata多态参数缺类型报42P18。显式bigint/text是同一路径必要正确性修复，批处理尚未实现 |
| 2,000标识有效RED | 旧实现运行`TestModContentReferenceSyncUsesConstantQueriesAtMaximumFieldScaleIntegration` | 包116.154s、用例114.98s | 1 | 预期RED：四个字段各500项，canonical/alias/tag各250命中并产生1,000未解析事实；业务循环共3,003条SQL，失败只在常量查询断言 |
| generation110 Schema合同 | `go test ./internal/database -run '^TestCatalogReferenceLookupHasCaseFoldedIndexes$' -count=1` | 包0.863s | 0 | PASS：三个folded lookup索引和generation110精确合同存在，旧109精确断言零残留 |
| 2,000标识GREEN与1M计划 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestModContentReferenceSyncUsesConstantQueriesAtMaximumFieldScaleIntegration$' -count=1 -v` | 包51.358s、用例51.24s；同步1.115s | 0 | PASS：同步固定5 SQL、500 tag+500 resource未解析事实及四字段路径精确；空替换固定3 SQL且Rollback保留事实；1M tag/resource/alias均命中folded索引，无目标表Seq Scan，数据库执行5.505/8.769ms |
| generation110空库安装 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 包34.552s、用例34.47s | 0 | PASS：会话临时完整Schema从空命名空间安装到110，三个表达式索引可创建；远端public generation85未修改 |
| 既有资料详情集成回归 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestModContentResourceVersionDetailsIntegration$' -count=1 -v` | 包43.041s、用例42.92s | 0 | PASS：generation110下既有资源版本详情、发布和读取语义继续通过 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy`后SHA-256差异 | Test14.254s；Vet9.048s；Build9.876s；tidy1.2s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建和模块图无回归；go.mod/go.sum字节不变 |
| 格式、差异与旧逐项路径 | gofmt；目标`git diff --check`；检索旧per-identifier exists/canonical QueryRow和generation109断言 | 1.2s | 0 | PASS：空白错误0，仅既有Windows行尾提示；旧逐项引用SQL和旧generation精确断言零残留 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.651s；Allow-open0.624s | 1 / 0 | PASS：audit/ledger精确449，170 CLOSED / 279 OPEN；High96 / Medium167 / Low49 / UNRESOLVED137；strict只因279个其余Finding、137个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

资料发布现在以一个请求内候选集完成引用解析；事务往返与引用数量解耦，同时保留原field path、raw identifier、registry metadata和回滚边界。

## 2026-08-22：PERF-017 整版布局逐分类SQL关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 64分类真实PG RED | 旧实现运行`TestPublishModContentLayoutUsesConstantQueriesAtCategoryScaleIntegration` | 包44.216s、用例44.10s | 1 | 预期RED：32旧+32新、每类双语言均成功写入，但发布执行295条SQL；失败只在不超过15条的常量查询断言 |
| 64分类首个GREEN | 批量实现后同一真实PG用例 | 包35.550s、用例35.43s；发布746.349ms | 0 | PASS：发布固定12 SQL；64 active、128本地化、32 system_key、ordinal和零staging残留精确 |
| 1,000分类/11,000身份上限GREEN | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestPublishModContentLayoutUsesConstantQueriesAtCategoryScaleIntegration$' -count=1 -v` | 包35.888s、用例35.77s；发布1.050s | 0 | PASS：generation110空临时Schema；1,000临时分类准备固定4 SQL；1,000分类500更新+500新增、2,000本地化发布固定12 SQL；1,000分类+10,000相似组共11,000 public ID固定2 SQL且全部合法唯一 |
| 20,000 resource纯映射上限 | `TestModContentLayoutGeneratedIDsMapMaximumCategoriesAndGroups` | 全仓内<1s | 0 | PASS：1,000临时分类和10,000双成员组生成顺序逐项映射到20,000 resources；section/group同组身份精确，生成数量不匹配失败关闭 |
| 深度移动/归档真实回归 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestPublishModContentLayoutSafelyRebuildsCategoryTreeIntegration$' -count=1 -v` | 包34.505s、用例34.39s | 0 | PASS：跨层移动、三层移除归档、重复system_key恢复、零staging残留和最大深度2均保持 |
| 完整资源布局真实回归 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestModContentResourceVersionDetailsIntegration$' -count=1 -v` | 包33.248s、用例33.13s | 0 | PASS：分类移动、resource placement、进度parent/layout definition与published revision仍在同事务精确发布 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy`后SHA-256差异 | Test12.651s；Vet8.634s；Build9.498s；tidy1.2s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建和模块图无回归；go.mod/go.sum字节不变 |
| 格式、差异与旧逐项路径 | gofmt；目标`git diff --check`；检索逐项new_public_id、section localization delete/value insert | 1.2s | 0 | PASS：空白错误0，仅既有Windows行尾提示；旧逐ID生成、逐分类数据库写和逐语言INSERT形状零残留，剩余category/system_key循环只构造内存数组 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.651s；Allow-open0.632s | 1 / 0 | PASS：audit/ledger精确449，171 CLOSED / 278 OPEN；High96 / Medium168 / Low49 / UNRESOLVED136；strict只因278个其余Finding、136个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

整版布局现在先形成完整身份与父子映射，再用有序集合写入；客户端SQL数量不随分类、语言或相似组数量增长，同时保留真实树约束和单事务原子性。

## 2026-08-22：PERF-018 公开项目目录宽正文/嵌套装配关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 源码与字节预算RED | 旧实现运行`TestCatalogHandlersUseBoundedCardProjections`、`TestCatalogResponsesHaveAnAtomicByteBudget` | 包7.473s | 1 | 预期RED：simple/modpack列表分别调用完整详情loader；modpack主查询选择`body_markdown`；两者没有card scanner或2 MiB原子写边界 |
| 卡片协议/原子预算单测 | `go test ./internal/httpapi -run 'TestCatalog(HandlersUseBoundedCardProjections&#124;ResponsesHaveAnAtomicByteBudget)' -count=1`及`TestBoundedCatalogResponseRejectsBeforeWritingAnOversizedPage` | 包约7.5s | 0 | PASS：两列表只调用card loader/scanner；详情loader与modpack正文查询零残留；超过2 MiB在写200前替换为小型500，预算内完整200 |
| 真实PG近上限处理器 | task-scoped DSN后`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCatalogCardHandlersBoundWorstCaseNestedDataIntegration$' -count=1 -v` | 包63.328s、用例62.15s | 0 | PASS：generation110临时全Schema；100 simple×8 locale×1 MiB正文、12作者/5父项，100 modpack×1 MiB正文、首包20×40兼容/50 tag/12作者/2,000模组；100项列表分别70,591/66,232字节且字段/嵌套上限精确；simple详情仍8 locale完整正文，modpack详情仍2,000模组+1 MiB正文 |
| 1M生产关联SQL计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCatalogCardAssociationPlansAtMillionRowsIntegration$' -count=1 -v` | 包6.798s、用例5.57s | 0 | PASS：125,000项目×8语言=1M行，100项卡片返回200本地化，PK Index Scan 100次、执行1.148ms；1,250包×20 loader×40版本=1M行，100项读80,000输入并输出1,600 loader组，PK Index Only Scan、执行214.210ms；两者无目标表Seq Scan |
| 前端卡片合同 | Node定向`catalog-card-contract.test.mts`；完整Node测试；ESLint；TypeScript；HTTPS示例环境production build | 定向0.571s；86 tests 3.277s；Lint28.056s；Type3.108s；Build24.176s | 0 | PASS：list类型不再是detail record；modpack图库特征只用`hasGallery`；目录/首页/选择器发送locale；58静态页完整构建 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy`后SHA-256差异 | Test16.789s；Vet4.148s；Build7.634s；tidy0.826s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建和模块图无回归；go.mod/go.sum字节不变 |
| 格式、差异与旧宽路径 | gofmt；目标双仓`git diff --check`；检索list handler的详情loader、正文和嵌套模组查询 | 约2s | 0 | PASS：空白错误0，仅既有Windows行尾提示；列表中detail loader、`pack.body_markdown`和contained-mod装配零残留 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict1.373s；Allow-open1.488s | 1 / 0 | PASS：audit/ledger精确449，172 CLOSED / 277 OPEN；High97 / Medium168 / Low49 / UNRESOLVED135；strict仅因277个其余Finding、135个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

公开项目目录现在只返回显示所需的有界卡片事实；完整正文与大嵌套集合仍可从详情取得，但不再由匿名100项列表成倍放大。

## 2026-08-22：BUG-028 / PERF-019 稳定作者关系与全局副作用风暴关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 64关系真实PG RED | 旧同步运行`TestProjectCreatorBindingSyncBatchesChangesAndCoalescesSideEffectsIntegration` | 包46.287s、用例45.12s | 1 | 预期RED：64个payload与数据库完全相同的approved关系仍被逐项重写；全局project ACL从2递增到66，在首个no-op断言失败 |
| Schema/差分/上限单测 | `go test ./internal/database ./internal/httpapi -count=1`及定向差分源码、generation111触发器、64/65边界 | Database0.110s、HTTP1.629s | 0 | PASS：锁定稳定行、单unnest upsert、no DELETE、表达式冲突键、ACL transaction GUC、搜索transition triggers及作者64上限均有反退化门禁 |
| generation111真实PG 64关系 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestProjectCreatorBindingSyncBatchesChangesAndCoalescesSideEffectsIntegration$' -count=1 -v` | 包34.058s、用例32.89s | 0 | PASS：空临时全Schema安装111；64项no-op固定4 SQL且ACL/search均0；反序重排固定5 SQL且均1；顺序、approved状态和原审批actor逐项精确；同事务两条独立UPDATE只使ACL +1 |
| 稳定身份与隐藏审核态回归 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestRelationshipSyncPreservesAuditIdentityAndHiddenStatesIntegration$' -count=1 -v` | 包1.514s、用例1.39s | 0 | PASS：无权编辑不改变approved/pending/rejected关系的内部/公开ID、created_at、approved_by/at；有权编辑只撤销目标稳定行并新增独立approved关系 |
| 1M生产锁定SQL计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestProjectCreatorBindingLookupPlanAtMillionRowsIntegration$' -count=1 -v` | 包3.213s、用例3.10s | 0 | PASS：1,000,000关系/15,625项目，每项目64行；精确生产`FOR UPDATE`查询走`idx_content_creator_bindings_subject_order` Index Scan，只返回64行，无目标Seq Scan，执行0.154ms |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy`后SHA-256差异 | Test13.900s；Vet9.175s；Build10.172s；tidy0.872s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建和模块图无回归；go.mod/go.sum字节不变 |
| 格式、差异与旧重建路径 | gofmt；目标`git diff --check`；检索关系DELETE、逐行INSERT及旧row search trigger | 约2s | 0 | PASS：空白错误0，仅既有Windows行尾提示；项目作者关系DELETE重建、逐项mutation写和旧`enqueue_search_creator_binding()`零残留 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.648s；Allow-open0.623s | 1 / 0 | PASS：audit/ledger精确449，174 CLOSED / 275 OPEN；High98 / Medium169 / Low49 / UNRESOLVED133；strict仅因275个其余Finding、133个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

项目作者关系现在以稳定审计身份做真实差分；普通内容编辑不再制造虚假审批历史，关系规模也不再决定SQL往返、ACL版本递增或同项目搜索入队次数。

## 2026-08-22：PERF-020 approved 整合包初始关联双写关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 首次真实PG夹具校正 | 初版`TestApprovedModpackCreationAppliesAssociationsExactlyOnceIntegration` | 包35.303s、用例35.18s | 1 | 夹具失败：链接类型误用非权威枚举`source`，handler在任何写入前正确返回400；改为合法`github`后才作为性能RED，不把夹具错误计入修复证据 |
| approved完整handler有效RED | 合法夹具下同一generation111真实PG用例 | 包36.448s、用例36.33s | 1 | 预期RED：8兼容、4标签、3链接、16模组分别发生16/8/6/32次INSERT与8/4/3/16次DELETE，逐类精确证明revision前写入后又完整替换 |
| approved/pending真实PG GREEN | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestApprovedModpackCreationAppliesAssociationsExactlyOnceIntegration$' -count=1 -v` | 包39.025s、用例38.90s | 0 | PASS：空临时generation111；approved四类只INSERT 8/4/3/16且DELETE均0，完整详情及published revision正确；反滥用强制pending时仍只INSERT 8/4/3/1、DELETE均0，详情有预览关联且无published revision |
| 2,000硬上限与分支单测 | `go test ./internal/httpapi -run '^TestApprovedModpackCreation' -count=1` | 包1.142s | 0 | PASS：approved不选择revision前投影、pending选择；2,000个唯一模组payload保留完整且走单发布路径，2,001拒绝 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy`后SHA-256差异 | Test14.195s；Vet9.133s；Build10.065s；tidy0.844s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建和模块图无回归；go.mod/go.sum字节不变 |
| 格式、差异与旧无条件路径 | gofmt；目标`git diff --check`/新文件尾随空白；检索`canManage...`后直接无条件replace形状 | 约1s | 0 | PASS：空白错误0；approved revision前无条件关联写零残留，pending条件与一次publish apply均显式 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.642s；Allow-open0.629s | 1 / 0 | PASS：audit/ledger精确449，175 CLOSED / 274 OPEN；High98 / Medium170 / Low49 / UNRESOLVED132；strict仅因274个其余Finding、132个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

免审整合包创建现在只有一次最终关联投影；待审创建仍保留提交者预览，不用删除业务能力换取性能结果。

## 2026-08-22：PERF-021 服务器目录深OFFSET与组合精确COUNT关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 后端旧协议RED | 新源码合同运行旧`server_catalog_handlers.go`，投影Schema合同运行旧projection v2，数据库合同运行旧generation111 | RED日志 | 1 / 1 / 1 | 预期RED：handler仍命中`select count(`；服务器投影缺稳定排序字段且版本为2；Schema generation仍为111 |
| 前端旧协议RED | `server-cursor-pagination.test.mts`检查目录和资源选择器源码 | RED日志 | 1 | 预期RED：服务器目录仍使用`CatalogPagination`与page/total/pages，资源选择器仍按total/offset预检和拼页 |
| 游标/投影/Schema单测 | `go test ./internal/httpapi ./internal/searchindex ./internal/database -count=1` | 墙钟8.147s | 0 | PASS：scope/authority/全部sort方向/畸形cursor、Typesense过滤、SQL无COUNT/OFFSET、projection v3与generation112合同全绿 |
| 真实PG 100k/1M与双authority handler | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestServerCatalogAuthoritativeIndexCursorAndMillionRowCardFetchIntegration$' -count=1 -v` | 包36.037s、用例35.92s | 0 | PASS：100k/1M精确卡片按PK取7行0.054/0.059ms；SQL updated keyset命中`idx_minecraft_servers_public_updated` Index Only Scan，只取61行并执行0.130/0.109ms；Typesense和SQL各连续两页60项无重漏，index续页失去服务时503 |
| generation112空库安装 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 包33.803s、用例33.72s | 0 | PASS：会话临时完整Schema安装到112；三个approved服务器索引、server popularity重入队trigger及既有对象全部可安装；远端public generation85未修改 |
| 真实PG搜索投影取数 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/searchindex -run '^TestLoadServerDocumentsQueryIntegration$' -count=1 -v` | 包0.384s、用例0.30s | 0 | PASS：临时影子表精确验证internal ID、时间、在线位热度、下载/收藏/评分/浏览/评论字段；空ID输入零文档 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`前后SHA-256 | Test13.318s；Vet3.902s；Build4.821s；tidy0.828s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建和模块图无回归；go.mod/go.sum SHA-256逐字节不变 |
| 前端游标与全量门禁 | Node test；TypeScript；全仓ESLint；带HTTPS验证URL的Next production build | Test1.986s；Type4.225s；Lint25.994s；Build23.623s | 0 / 0 / 0 / 0 | PASS：89/89 tests，目录追加/取消/去重与混合picker组合cursor通过；类型和lint全绿；production build编译并生成58/58静态页。首次未提供必需生产URL时按配置合同正确退出1，不计作产品失败 |
| 格式、差异与旧协议 | gofmt；双仓`git diff --check`；检索旧index-offset符号、`sort_name`和服务器page/count消费 | <2s | 0 | PASS：空白错误0，仅既有Windows行尾提示；旧服务器index offset、投影字符串排序和第一方page/total协议零残留 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict1.067s；Allow-open1.184s | 1 / 0 | PASS：audit/ledger精确449，176 CLOSED / 273 OPEN；High98 / Medium171 / Low49 / UNRESOLVED131；strict只因273个其余Finding、131个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

服务器目录现在按每页实际需要读取稳定的下一组结果；组合筛选、匿名深页和前端混合选择器都不再要求精确COUNT或丢弃数十万OFFSET行。

## 2026-08-22：PERF-022 服务器审核列表N+1与大正文过取关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 后端摘要/详情合同RED | `go test ./internal/httpapi -run '^TestServerReviewListUsesSummaryProjectionAndOnDemandDetail$' -count=1` | 墙钟7.432s | 1 | 预期RED：审核列表查询仍直接选择`server.body_markdown`，测试在第一条过取断言失败；旧handler随后还会逐项调用proof/link/mod reader |
| 前端按需详情合同RED | Node运行`server-review-pagination.test.mts` | 墙钟0.508s | 1 | 预期RED：旧管理面只有一个含body/proof/三数组的`ServerReviewItem`，不存在Summary/Detail分型或详情加载边界 |
| 定向合同GREEN | Go分页/投影合同；前端4项server review测试；TypeScript与目标ESLint | Go9.033s；Node0.518s；Type11.867s；Lint3.279s | 0 / 0 / 0 / 0 | PASS：列表查询无body/proof和逐项association helper；详情GET注册；前端摘要零宽字段且展开请求、取消、身份复核、重试受源码门禁 |
| 100项大正文/关联真实PG | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestServerReviewSummaryUsesOneQueryAndLoadsLargeDetailOnDemandIntegration$' -count=1 -v` | 最终组合用例2.55s | 0 | PASS：每项约1MiB正文、64KiB证明文本、2 proof+2 link+2 mod；100项摘要恰1 SQL、响应<256KiB且无秘密marker；单条按需详情固定5 SQL并完整返回正文、证明和2×三类关联 |
| 三状态cursor与100k计划 | 同包运行`TestServerReviewKeysetCursorTraversesEveryStatusIntegration` | 最终组合用例1.47s；计划0.636ms | 0 | PASS：pending/approved/rejected各205项以100/100/5完整遍历无重漏；100k深cursor从review复合索引读取101行，无目标表Seq Scan |
| 当前Schema只读烟雾 | 同包运行`TestMinecraftServerReviewListQueryIntegration` | 最终组合用例0.18s | 0 | PASS：新摘要查询在当前真实Schema可执行并按22列契约扫描；未写远端public数据 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`前后SHA-256 | Test12.020s；Vet4.929s；Build5.636s；tidy0.916s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建和模块图无回归；go.mod/go.sum逐字节不变 |
| 前端全量门禁 | Node test；TypeScript；全仓ESLint；HTTPS验证URL的Next production build | Test1.959s；Type4.544s；Lint26.179s；Build24.402s | 0 / 0 / 0 / 0 | PASS：90/90 tests；类型和lint全绿；Next16编译、类型阶段及58/58静态页生成通过 |
| 格式、差异与旧列表路径 | gofmt；双仓`git diff --check`；检索列表body/proof和逐项association调用 | Diff0.968s/约1s | 0 | PASS：空白错误0，仅既有Windows行尾提示；旧宽列表和循环数据库访问零残留，body/proof及三个reader只存在于单记录详情边界 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict1.153s；Allow-open1.008s | 1 / 0 | PASS：audit/ledger精确449，177 CLOSED / 272 OPEN；High98 / Medium172 / Low49 / UNRESOLVED130；strict只因272个其余Finding、130个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

服务器审核列表的数据库往返和响应正文现在都与队列项数量解耦；只有管理员明确展开的一条记录才读取完整审核证据。

## 2026-08-22：PERF-023 项目文件全量跨供应商聚合关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 后端有界来源合同RED | `go test ./internal/httpapi -run TestProjectFileListingUsesBoundedPerSourcePages -count=1` | 墙钟7.268s | 1 | 预期RED：旧handler缺source/cursor、hasMore/nextCursor和三类page loader，仍调用无LIMIT站内列表、Modrinth全version、CurseForge 10页及三源全量append |
| 前端独立来源游标RED | Node运行`project-file-pagination.test.mts` | 墙钟0.223s | 1 | 预期RED：旧DTO无source/hasMore/nextCursor且仍一次GET全列表，组件没有sourceCursors或loadMore |
| 供应商可控HTTP与硬上限GREEN | `go test ./internal/httpapi -run 'Test(ProjectFile|ModrinthProjectFile|CurseForgeProjectFile)' -count=1 -v` | 包1.152s | 0 | PASS：25个Modrinth文件按manifest反向ID顺序以每批最多10项完整遍历、无重漏且cursor锚定version ID；45个CurseForge文件按20/20/5遍历且每次pageSize=21；10,001版本及超过专用字节上限均失败关闭 |
| 100k站内文件真实PG与深页计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestInternalProjectFilePageStaysBoundedAtHundredThousandRowsIntegration$' -count=1 -v` | 用例1.58s；组合墙钟8.959s | 0 | PASS：连续三页各50项无重漏、恰3 SQL且每页JSON<128KiB；深cursor命中`idx_project_files_project_published`，只读51条project/OSS行，无project_files Seq Scan，执行0.141ms |
| 外部下载按ID归属与响应预算 | 源码合同及provider单项reader | 同定向门禁 | 0 | PASS：列表handler零`cachedProviderProjectFiles`调用；Modrinth hash详情复核manifest canonical project ID，CurseForge使用project-scoped单文件端点；所有项目搜索/manifest/page/detail/download URL入站JSON最大2 MiB，列表出站在写200前复用2 MiB原子预算 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy`前后SHA-256 | 合计17.415s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建和模块图无回归；go.mod/go.sum逐字节不变 |
| 前端扩展全量门禁 | Node运行全部`app/_lib/*.test.mts`；TypeScript；全仓ESLint | 合计26.736s | 0 / 0 / 0 | PASS：133/133 tests；新三来源游标、PERF-021组合picker当前合同、类型和lint全绿；发现并更新一个未纳入脚本且仍要求旧COUNT预检的孤立测试，不把陈旧断言伪装成产品回归 |
| Next16 production build | `NEXT_PUBLIC_API_BASE_URL=https://api.example.test NEXT_PUBLIC_SITE_URL=https://www.example.test next build` | 首次配置门0.468s；有效构建23.762s | 1 / 0 | PASS：未提供必需生产API URL时按部署合同在编译前失败；临时HTTPS值下编译/TypeScript/31 workers/58静态页全部通过，不写环境文件 |
| 格式、差异与旧全量路径 | gofmt；双仓`git diff --check`；检索无LIMIT站内reader、公开handler全量provider cache、旧三源append和10页循环 | 后端0.463s；前端0.343s | 0 / 0 | PASS：空白错误0，仅既有Windows行尾提示；四类旧公开全量符号在handler均为0，bulk helper只保留给另有500硬限的后台自动化/收藏导出，不再可由公开列表或下载命中 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.698s；Allow-open0.632s | 1 / 0 | PASS：audit/ledger精确449，178 CLOSED / 271 OPEN；High99 / Medium172 / Low49 / UNRESOLVED129；strict只因271个其余Finding、129个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

项目文件公开页现在每个HTTP请求只推进一个有界来源；“全部来源”是三个可独立恢复的游标组合，不再要求服务器物化一个跨供应商的无界全局数组。

## 2026-08-22：PERF-024 项目更新日志全历史多语言正文聚合关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 列表摘要游标合同RED | Go `TestProjectChangelogListUsesBoundedSummaryPages`；Node `project-changelog-pagination.test.mts` | Go墙钟7.130s；Node墙钟0.203s | 1 / 1 | 预期RED：旧列表无Summary/cursor/limit/hasMore/nextCursor且仍聚合全部本地化正文；前端DTO和时间线只接受/渲染bodyMarkdown且没有续页 |
| 游标、投影与前端定向GREEN | Go `TestProjectChangelog*`；Node单测；bundled Node调用`tsc --noEmit` | Go包1.103s；Node0.202s；Type9.394s | 0 / 0 / 0 | PASS：scope绑定、畸形/跨locale、page/offset/limit拒绝、tuple keyset/limit+1、摘要DTO和前端loadMore均通过 |
| 1M真实PG响应与深游标计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestProjectChangelogSummaryPagesStayBoundedAtOneMillionRowsIntegration$' -count=1 -v` | 墙钟9.048s；用例6.88s；夹具6.579s | 0 | PASS：两页各50项无重漏、恰2 SQL、JSON<256KiB且无SECRET尾部/localizations；100k/1M深度均一次Index Scan `idx_project_changelogs_target`、读取51/50项，执行0.643/0.629ms |
| 后端包级与全仓门禁 | `go test ./internal/httpapi -count=1`；`go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`前后SHA-256 | 包3.448s；Test11.729s；Vet2.864s；Build3.545s；tidy0.335s | 0 / 0 / 0 / 0 / 0 | PASS：包级及全仓测试、静态分析、构建和模块图无回归；go.mod/go.sum逐字节不变 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；bundled Node `tsc --noEmit`；全仓ESLint；HTTPS验证URL的Next production build | Test2.144s；Type9.394s；Lint30.246s；Build28.697s | 0 / 0 / 0 / 0 | PASS：134/134 tests；类型、lint全绿；Next16编译、31 workers及58/58静态页生成通过 |
| 格式、差异与旧宽列表 | gofmt；双仓`git diff --check`；检索列表bodyMarkdown/json aggregate、page/offset和前端旧渲染 | <2s | 0 / 0 | PASS：空白错误0，仅既有Windows行尾提示；完整正文aggregate只保留在单记录详情，列表及第一方时间线旧宽正文消费零残留 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.640s；Allow-open0.634s | 1 / 0 | PASS：audit/ledger精确449，179 CLOSED / 270 OPEN；High100 / Medium172 / Low49 / UNRESOLVED128；strict只因270个其余Finding、128个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

更新日志时间线现在只为当前页读取一种语言的有界摘要；完整多语言正文只在用户明确打开单条详情或编辑器时读取。

## 2026-08-22：PERF-025 项目更新通知重复event计数关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 热路径/Schema合同RED | `go test ./internal/httpapi -run '^TestProjectUpdateNotificationProgressDoesNotRecountEveryBatch$' -count=1` | 墙钟7.163s；包1.125s | 1 | 预期RED：旧Worker仍含两处`notified_count=(select count(*)...)`，首个反退化断言即失败；event-first索引和原子增量同样尚不存在 |
| 定向Worker与Schema合同 | `go test ./internal/httpapi ./internal/database -run 'TestProjectUpdateNotificationProgress&#124;TestEngagementExportSchemaDefinesCurrentContracts' -count=1`及包全量 | 定向约1.1s；包全量2.603s | 0 | PASS：热路径COUNT零残留、任务行原子delta、generation113与部分索引合同通过；HTTP和database包无回归 |
| 1M真实PG累计与对账计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestProjectUpdateNotificationCountUsesAtomicDeltasAndEventIndexIntegration$' -count=1 -v` | 墙钟5.104s；用例2.92s | 0 | PASS：同任务137+63成功增量得到completed/200；1M通知中event的1,000项由`idx_notifications_project_update_event`一次Index Only Scan读取，执行0.307ms，无notifications Seq Scan |
| generation113完整临时Schema | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟34.099s；用例32.61s | 0 | PASS：当前全部Schema/trigger/index/seed在会话临时namespace安装到113；远端public generation85未迁移或写入 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`前后SHA-256 | Test11.045s；Vet2.888s；Build3.945s；tidy0.333s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建与模块图无回归；go.mod/go.sum逐字节不变 |
| 格式、差异与旧COUNT | gofmt；目标`git diff --check`；检索旧event COUNT、generation112断言和event-first索引 | <2s | 0 | PASS：空白错误0，仅既有Windows行尾提示；Worker旧COUNT与测试中的generation112期望零残留，新索引只在Schema和验证边界出现 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.650s；Allow-open0.647s | 1 / 0 | PASS：audit/ledger精确449，180 CLOSED / 269 OPEN；High100 / Medium173 / Low49 / UNRESOLVED127；strict只因269个其余Finding、127个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

项目更新通知的任务计数现在与本批真实插入数同事务前进，关注者越多也不会在每200人后重扫同一事件的全部既有通知。

## 2026-08-22：PERF-026 自动镜像256MiB整文件堆读取关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 流式/全局预算合同RED | `go test ./internal/httpapi -run '^TestProjectAutomationMirrorStreamsThroughBoundedGlobalSlots$' -count=1` | 墙钟7.426s；包1.123s | 1 | 预期RED：旧download函数仍返回`[]byte`并调用`io.ReadAll`；不存在temp spool、copy buffer、全局advisory slot或并发常量 |
| 近上限内存/哈希/超限GREEN | `go test ./internal/httpapi -run 'Test(SpoolProviderFile&#124;ProjectAutomationMirrorStreams)' -count=1 -v` | 墙钟7.778s；包1.770s；近上限0.61s | 0 | PASS：268,304,384字节只分配138,192堆字节；128KiB缓冲、同流三哈希、seek回读、Close后临时文件消失及limit+1拒绝全部通过 |
| 双实例全局四槽真实PG | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestProjectAutomationMirrorSlotsAreGlobalAcrossWorkerPoolsIntegration$' -count=1 -v` | 首次墙钟3.614s；用例1.39s；最终包1.441s | 0 | PASS：pool A占满四个不同session slot，pool B第五获取按deadline失败；释放一个后pool B成功，证明预算不是单进程信号量 |
| HTTP包与后端全仓门禁 | `go test ./internal/httpapi -count=1`；`go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`前后SHA-256 | 包3.942s；Test9.961s；Vet2.861s；Build3.592s；tidy0.330s | 0 / 0 / 0 / 0 / 0 | PASS：镜像/自动化及全仓无回归；go.mod/go.sum逐字节不变 |
| 格式、差异与旧堆路径 | gofmt；目标`git diff --check`；检索Worker `io.ReadAll`、`bytes.NewReader`及残留temp文件 | <2s | 0 | PASS：空白错误0，仅既有Windows行尾提示；自动镜像完整文件堆路径零残留，测试后`mcmods-project-mirror-*`临时文件为0 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.640s；Allow-open0.635s | 1 / 0 | PASS：audit/ledger精确449，181 CLOSED / 268 OPEN；High100 / Medium173 / Low49 / UNRESOLVED127；strict只因268个其余Finding、127个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

自动镜像的资源成本现在由文件大小硬限、128KiB堆缓冲、四个跨实例槽和约1GiB全站临时磁盘预算共同限定。

## 2026-08-22：PERF-027 爬虫候选与运行固定前100条关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 列表游标/详情合同RED | Go `TestSeedCrawlerListsUseSummaryKeysetsAndOnDemandCandidateDetail`；Node `seed-crawler-pagination.test.mts` | 首次运行 | 1 / 1 | 预期RED：旧运行/候选只有第一窗口且无cursor/hasMore/nextCursor，候选列表仍选择完整payload；前端没有续页或按需详情状态 |
| 后端游标/SQL合同与前端定向GREEN | `go test ./internal/httpapi -run 'TestSeedCrawler' -count=1`；bundled Node单测和`tsc --noEmit` | Go墙钟7.495s、包1.107s；Node0.110s；Type约9.6s | 0 / 0 / 0 | PASS：offset/未知status/畸形及跨scope cursor拒绝、排他tuple keyset、limit+1、摘要投影、前端双cursor/详情均通过 |
| 100k/1M真实PG分页与计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestSeedCrawlerKeysetPagesStayBoundedAtScaleIntegration$' -count=1 -v` | 墙钟15.863s；包9.628s；夹具6.682s | 0 | PASS：runs/candidates各两页恰2 SQL且各100项无重漏；候选摘要<128KiB且无payload/SECRET；运行、候选、failed筛选分别命中三个目标索引，执行0.043/0.181/0.552ms |
| generation114完整临时Schema | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟约33.36s；用例33.28s | 0 | PASS：当前全部Schema/trigger/index/seed在会话临时namespace安装到114；远端public generation85未迁移或写入 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test11.272s；Vet2.877s；Build3.917s；tidy0.331s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建与模块图无回归 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；bundled Node `tsc --noEmit`与全仓ESLint；HTTPS验证URL的Next production build | Test1.952s；Type+Lint24.749s；Build23.790s | 0 / 0 / 0 | PASS：135/135 tests；类型、lint全绿；Next16编译、31 workers及58/58静态页生成通过；首次无必需API URL时按配置合同预期退出1 |
| 格式、差异与旧固定窗口 | gofmt；双仓目标`git diff --check`；检索候选handler的`candidate.payload`/`limit 100`、前端items-only消费和generation113断言 | <2s | 0 / 0 | PASS：空白错误0，仅既有Windows行尾提示；旧固定窗口、宽候选列表、items-only前端和旧generation期望零残留 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.667s；Allow-open0.625s | 1 / 0 | PASS：audit/ledger精确449，182 CLOSED / 267 OPEN；High100 / Medium173 / Low49 / UNRESOLVED127；strict只因267个其余Finding、127个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

爬虫管理端现在能稳定遍历全部运行与候选；列表只承担有界摘要，完整供应商载荷只在管理员明确打开一个候选时读取。

## 2026-08-22：PERF-028 通知固定前200条与全部已读历史写放大关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 后端分页/水位合同RED | `go test ./internal/httpapi -run '^TestNotificationListUsesBoundedKeysetsAndReadWatermark$' -count=1` | 墙钟7.409s；包1.147s | 1 | 预期RED：旧GET仍返回固定最多200条裸数组且只按updated_at排序；read-all仍以历史`insert ... select`逐条创建receipt，不存在严格cursor或用户水位 |
| 前端页DTO合同RED | bundled Node运行`notification-pagination.test.mts` | 墙钟0.207s | 1 | 预期RED：旧消息中心仍消费裸数组，没有nextCursor、加载更多、请求代次或readBefore水位响应 |
| 后端游标/SQL合同与前端定向GREEN | Go通知分页/水位测试；bundled Node单测及`tsc --noEmit` | Go墙钟约7.52s、包1.073s；Node0.109s；Type组合9.987s | 0 / 0 / 0 | PASS：认证用户/kind/limit绑定、畸形/未知/page/offset/跨scope游标拒绝，双流tuple keyset、单行水位upsert和前端增量状态均通过 |
| 1M真实PG分页、水位语义与计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestNotificationPagesAndReadWatermarkStayBoundedAtOneMillionRowsIntegration$' -count=1 -v` | 墙钟16.064s；包9.799s；夹具7.474s | 0 | PASS：两页恰2 SQL且无重漏、每页JSON<128KiB；kind/无kind双流执行0.700/0.469ms；首次read-all恰1 SQL、1 watermark、0 receipt并覆盖百万历史，新建+更新后恰2未读，第二次水位后归零；max-ID反向PK计划0.049ms |
| generation115完整临时Schema | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟34.030s；用例约33.94s | 0 | PASS：当前全部Schema/trigger/index/seed在会话临时namespace安装到115；远端public generation85未迁移或写入 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test14.335s；Vet2.862s；Build3.953s；tidy0.332s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建与模块图无回归 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；bundled Node `tsc --noEmit`与全仓ESLint；HTTPS验证URL的Next production build | Test1.986s；Type1.948s；Lint22.333s；Build23.560s | 0 / 0 / 0 / 0 | PASS：136/136 tests；类型、lint全绿；Next16编译、31 workers及58/58静态页生成通过 |
| 格式、差异与旧协议 | gofmt；双仓`git diff --check`；检索旧裸数组、固定200窗口、历史receipt批写和generation114断言 | <2s | 0 / 0 | PASS：空白错误0，仅既有Windows行尾提示；旧列表/read-all协议和旧generation期望零残留 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.642s；Allow-open0.629s | 1 / 0 | PASS：audit/ledger精确449，183 CLOSED / 266 OPEN；High100 / Medium173 / Low49 / UNRESOLVED127；strict只因266个其余Finding、127个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

消息中心现在能稳定遍历全部定向与广播通知；“全部已读”的数据库成本固定为一个用户水位行，不再随全站历史长度增长。

## 2026-08-22：PERF-029 周期未读对账全用户相关COUNT关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 全用户游标反退化合同RED | `go test ./internal/httpapi -run '^TestUnreadReconciliationSamplesLiveCachedDerivativesInsteadOfSweepingAllUsers$' -count=1` | 墙钟7.213s；包1.097s | 1 | 预期RED：旧周期Worker仍声明user cursor，并以`status='active' and id>$1`最终遍历全站账户；缓存样本、16人硬上限和统一truth均不存在 |
| 抽样/真值定向GREEN | HTTP源码/SQL合同、querycache轮转与过期清理、notification单条read合同；三个目标包全量 | 初次HTTP1.161s/querycache0.515s；目标包最终3.375s | 0 | PASS：只抽样TTL内已服务派生、按ID轮转且过期项删除；周期路径无active-user cursor；定向/私信/广播/receipt拆流，水位已读的单条点击不递减 |
| 1M混合通知/水位/私信真实PG | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestNotificationPagesAndReadWatermarkStayBoundedAtOneMillionRowsIntegration$' -count=1` | 最终墙钟18.152s；包11.946s；夹具约8.9s | 0 | PASS：16个无水位样本恰1 truth SQL，25万共享广播从单例读取，计划命中`idx_notifications_recipient_id`且执行0.219ms、notifications零Seq Scan；定向通知+广播receipt+2私信精确，read-all后点击水位已读广播影响0行 |
| 100k/1M/10M广播梯度 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestUnreadCachedSampleStaysConstantAtTenMillionBroadcastsIntegration$' -count=1 -v` | 墙钟33.192s；包27.070s；增量夹具0.463/2.387/22.582s | 0 | PASS：三个规模各16用户、各恰1 SQL且结果分别精确等于广播存量；计划均命中recipient-ID索引、广播范围子计划不执行、notifications零Seq Scan，执行0.559/0.503/0.505ms，百万用户数不再是周期遍历维度 |
| generation116完整临时Schema与触发器 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1` | 墙钟35.575s；包33.826s | 0 | PASS：当前全部Schema/trigger/index/seed安装到116；broadcast insert/delete和direct→broadcast→direct使live_count为1→0→1→0，max ID单调精确；清理后远端public generation85及临时关系均不变 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test6.900s；Vet2.892s；Build2.781s；tidy0.338s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图无回归；本项无前端/API响应变更 |
| 格式、差异与旧全扫路径 | gofmt；双仓`git diff --check`；生产源码检索`reconcileUnreadBatch`、user cursor、generation115断言 | 后端diff0.474s；前端diff0.250s；其余<1s | 0 / 0 / 0 | PASS：空白错误0，仅既有Windows行尾提示；生产旧全用户循环和旧generation期望零残留；前端没有本项改动 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | Strict0.669s；Allow-open0.630s | 1 / 0 | PASS：audit/ledger精确449，184 CLOSED / 265 OPEN；High100 / Medium173 / Low49 / UNRESOLVED127；strict只因265个其余Finding、127个未决严重度与最终严重度总数未达目标而按设计退出1，allow-open通过 |

周期校准现在只修复真实存在的缓存派生；广播历史无论十万还是千万，都不会再乘以全站活跃用户数重复扫描。

## 2026-08-22：PERF-030 私聊无界会话/固定历史与错配索引关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 后端游标/Schema合同RED | `go test ./internal/httpapi ./internal/database -run 'Test(ConversationCursor&#124;ConversationPageSQL&#124;DirectMessageHistoryCursor&#124;DirectMessagePageSQL&#124;DirectMessageHandlersExpose&#124;DirectMessagePagesHave)' -count=1` | 墙钟3.356s | 1 | 预期RED：不存在会话/历史cursor解析、页SQL与generation117；旧Schema仍为116且测试编译时即报告未定义分页边界 |
| 前端双页合同RED | bundled Node运行`direct-message-pagination.test.mts` | 墙钟0.512s | 1 | 预期RED：消息中心仍消费两个裸数组，没有ConversationPage/DirectMessagePage、会话续页、更早历史或hasMore排空 |
| 定向游标/SQL/Schema与前端GREEN | Go两目标包定向测试；bundled Node单测、TypeScript及目标ESLint | Go墙钟8.303s；Node0.470s；目标Lint3.458s | 0 / 0 / 0 / 0 | PASS：严格user/member/conversation/limit scope、page/offset/重复参数拒绝、双成员Merge Append、ID历史/after互斥、持久摘要与前端两类续页合同全部通过 |
| 100k会话/1M消息真实PG分页与计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestDirectMessagePagesStayBoundedAtOneMillionMessagesIntegration$' -count=1 -v` | 最终墙钟16.482s；包10.182s；夹具8.561s | 0 | PASS：会话与历史各两页各恰2 SQL、每页无重漏；增量锚点999800返回最早999801..999825并声明可续；会话low/high Merge、稀疏消息历史、未读部分索引计划均无规模表Seq Scan，执行0.302/0.119/0.066ms |
| generation117完整临时Schema与触发器 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 最终墙钟34.024s；用例33.94s | 0 | PASS：全部Schema/trigger/index/seed安装到117；direct unread insert/read/recipient move/delete为1/0/1/0，last-message外键下删除会话可级联清理消息；清理后远端public generation85未迁移或写入 |
| 消息/邮件Outbox原子事务 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestDirectMessageAndEmailOutboxCommitOrRollbackTogether$' -count=1 -v` | 墙钟8.517s；用例1.02s | 0 | PASS：成功消息、最近消息指针和email intent一起提交；故意拒绝Outbox后消息、updated_at和last_message_id一起回滚；单调投影SQL另有源码合同 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test11.559s；Vet3.680s；Build4.579s；tidy0.373s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图无回归 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；bundled Node `tsc --noEmit`与全仓ESLint；HTTPS验证URL的Next production build | Test2.368s；Type2.863s；Lint23.224s；Build23.677s | 0 / 0 / 0 / 0 | PASS：138/138 tests；类型、lint全绿；Next16编译、31 workers及58/58静态页生成通过；首次缺少必需public URL时按生产配置合同预期0.470s退出1，随后只注入非秘密HTTPS测试origin |
| 格式、差异与旧查询协议 | gofmt；双仓`git diff --check`；检索旧LATERAL/相关COUNT、裸数组consumer、旧created_at/全局会话索引和generation116断言 | gofmt0.049s；后端最终diff0.504s；前端diff0.400s；后端检索0.398s；前端检索0.191s | 0 / 0 / 0 / 0 / 0 | PASS：指定Go文件无未格式化项，双仓无空白错误；生产代码中旧逐行LATERAL/COUNT、旧索引/generation断言及前端裸数组协议均为零命中 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格0.706s；开放0.716s | 1（预期）/ 0 | PASS：449/449；CLOSED=185、OPEN=264；High=101、Medium=173、Low=49、UNRESOLVED=126；严格模式只因剩余OPEN/未定级及最终级别目标按预期失败，`-allow-open`通过 |

私聊列表和历史的数据库工作现在由硬页界、成员/ID索引和持久摘要共同约束；旧消息可连续向前遍历，实时突发也不会因固定100条窗口跳失。

## 2026-08-22：PERF-031 实时事件精确失效与请求合并关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | bundled Node `--test app/_lib/realtime-query-cache.test.mts`，测试先引用尚不存在的协调层 | 0.194s | 1（预期） | PASS：`ERR_MODULE_NOT_FOUND realtime-query-cache.mts`，证明新实现前合同不可满足 |
| 协调层定向 | bundled Node `--test app/_lib/realtime-query-cache.test.mts` | 0.206s | 0 | PASS：4/4；精确事件矩阵、共享in-flight/陈旧响应、事件风暴API上限和组件源码合同通过 |
| 分页联合回归 | bundled Node `--test realtime-query-cache.test.mts direct-message-pagination.test.mts` | 0.288s | 0 | PASS：5/5；实时补拉仍保留PERF-030的cursor envelope和多页after排空 |
| 前端全量测试 | 全部`app/_lib/*.test.mts` | 2.456s | 0 | PASS：142/142；事件风暴断言100 message+100 unread=1会话+1消息+1未读，100 notification=1分类+1未读 |
| Type / Lint / Build | bundled Node `tsc --noEmit`；全仓ESLint；注入非秘密HTTPS验证origin的Next production build | Type3.244s；Lint23.601s；Build23.288s | 0 / 0 / 0 | PASS：Next16编译成功、31 workers及58/58静态页生成通过 |
| 旧扇出、格式与差异 | 检索生产代码`mcmods-realtime`/`mcmods-unread-change`及旧listener/dispatch；双仓`git diff --check` | 检索0.260s；前端diff0.402s；后端最终diff0.518s | 0 / 0 / 0 | PASS：旧DOM刷新总线及扇出生产/消费零命中，双仓无空白错误 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格0.910s；开放0.870s | 1（预期）/ 0 | PASS：449/449；CLOSED=186、OPEN=263；High=101、Medium=174、Low=49、UNRESOLVED=125；严格模式只因剩余OPEN/未定级及最终级别目标按预期失败，`-allow-open`通过 |

实时刷新现在按事实命中唯一查询键：组件数量和同tick事件数量不再乘法增加相同API请求，非当前会话也不会触发消息读取。

## 2026-08-22：PERF-032 API访问日志有界异步批摄取关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | `go test ./internal/httpapi -run '^TestAccessLog' -count=1`，先落策略/阻塞/队列/批写合同 | 1.764s | 1（预期） | PASS：`accessLogRecord/decideAccessLog/accessLogIngestor`等均未定义，旧同步实现不能满足合同 |
| 定向单元与源码合同 | `go test ./internal/httpapi -run '^TestAccessLog(Policy&#124;Middleware&#124;Queue&#124;Worker)' -count=1 -v` | 墙钟6.292s；包0.127s | 0 | PASS：6项；1/16策略、250ms阻塞writer上限、真实middleware 30 sampled/2 flushed、容量2溢出、4项单SQL批写、无同步fallback及完整生命周期/指标通过 |
| 真实PostgreSQL降级 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestAccessLogIngestionDoesNotExtendRequestsDuringDatabaseLockIntegration$' -count=1 -v` | 墙钟2.644s；用例0.50s；请求1.0424ms | 0 | PASS：`app_logs` ACCESS EXCLUSIVE锁期间64个500响应不等待；解锁后Flushed=64、Batches=4、WriteFailures=0，表内精确64行并清理 |
| 相关包回归 | `go test ./internal/httpapi ./internal/app -count=1` | 6.224s | 0 | PASS：HTTP中间件、Server关闭及应用生命周期无回归 |
| 全仓门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test9.006s；Vet5.390s；Build5.949s；tidy0.408s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿 |
| 补充race尝试 | `go test -race ./internal/httpapi -run '^TestAccessLog(Policy&#124;Middleware&#124;Queue&#124;Worker)' -count=1` | 0.244s | 2（环境不支持） | INFO：当前Go运行时`CGO_ENABLED=0`，race detector不可用；不把该命令计作通过，受控阻塞/并发单测与真实PG门禁仍为本项关闭证据 |
| 格式、差异与旧同步路径 | gofmt；`git diff --check`；检索`logAccess`旧`writeAppLog(context.Background(),"api_access")`和2秒访问日志fallback | 最终格式/diff0.513s；检索0.150s | 0 / 0 | PASS：指定Go文件无未格式化项、全仓无空白错误；旧同步访问日志路径零命中，新队列/批写/指标合同均命中 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格0.742s；开放0.735s | 1（预期）/ 0 | PASS：449/449；CLOSED=187、OPEN=262；High=102、Medium=174、Low=49、UNRESOLVED=124；严格模式只因剩余OPEN/未定级及最终级别目标按预期失败，`-allow-open`通过 |

访问日志现在具有明确的容量、采样、丢弃、批写和观测协议；PostgreSQL变慢不会再把已经完成业务的每个API请求拖住2秒。

## 2026-08-22：PERF-033 管理日志稳定分页、全文投影与小批清理关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效后端/前端RED | `go test ./internal/httpapi -run '^(TestLogPage&#124;TestLogCleanup&#124;TestAdminLogHandler)' -count=1`；bundled Node `--test admin-log-pagination.test.mts` | Go2.012s；Node0.495s | 1 / 1（预期） | PASS：后端报告cursor/parser/SQL/cleanup statement未定义（同时修正测试对本Go标准库不存在的`url.Values.Clone`调用后再实现）；前端没有LogPage/hasMore/nextCursor/loadMore并仍消费裸数组 |
| Schema合同RED与定向GREEN | generation118 Schema合同先行；随后Go HTTP/Schema定向合同、frontend分页合同 | Schema RED2.898s；HTTP GREEN7.819s；Schema GREEN2.909s；Node GREEN0.495s | 1（预期）/ 0 / 0 / 0 | PASS：严格全筛选scope、稳定tuple、limit+1、无OFFSET/COUNT/LIKE/payload转换、每来源1,000 ID清理、四表GIN/页索引及envelope consumer全部命中 |
| 1M+3×100k真实PostgreSQL分页/搜索/清理 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestLogPaginationSearchAndCleanupStayIndexedAtScaleIntegration$' -count=1 -v` | 最终墙钟12.575s；包6.263s；用例5.14s；1.3M夹具4.381s | 0 | PASS：两页各25项且无重漏；1M app页命中category/time/ID索引；四来源搜索均命中各自GIN且日志规模表零Seq Scan；清理命中页索引并精确删除1,000行 |
| generation118完整临时Schema与写时投影 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟36.932s；用例36.07s | 0 | PASS：全部Schema/trigger/index/seed安装到118；应用/权限/登录/上传四条新日志均由trigger生成可检索`search_document`；隔离Schema删除后远端public generation85及临时关系不变 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test12.419s；Vet4.325s；Build5.237s；tidy0.768s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；bundled Node `tsc --noEmit`与全仓ESLint；注入非秘密HTTPS验证origin的Next production build | Test3.089s；Type2.967s；Lint30.003s；Build29.549s | 0 / 0 / 0 / 0 | PASS：143/143 tests；类型与lint全绿；Next16编译、31 workers及58/58静态页生成通过 |
| 格式、差异与旧路径 | 指定Go文件gofmt-diff；双仓`git diff --check`；检索旧`payload::text`/LIKE日志reader、旧数组consumer及generation117测试断言 | 后端1.011s；前端0.737s | 0 / 0 | PASS：格式和双仓空白检查通过；查询期宽JSON/通配路径、裸数组consumer与过期generation断言零命中 |
| 台账精确性 | `go run ./tools/remediation/verify_findings`；`go run ./tools/remediation/verify_findings -allow-open` | 严格0.999s；开放1.040s | 1（预期）/ 0 | PASS：449/449；CLOSED=188、OPEN=261；High=103、Medium=174、Low=49、UNRESOLVED=123；严格模式只因其余OPEN/未决严重度与最终级别目标按预期失败，allow-open通过 |

管理日志历史现在能以稳定游标连续遍历；搜索成本落在写时受控投影和GIN，保留策略保存也不会再对任一日志表制造无界删除事务。

## 2026-08-22：PERF-034 热度累计事实、离线校准与到期索引调度关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED与定向合同GREEN | 先加入generation119三事实/trigger/hot source和调度source失败合同；实现后运行`go test ./internal/database ./internal/httpapi -count=1` | RED墙钟7.829s；最终包回归4.592s | 1（预期）/ 0 | PASS：RED确认旧refresh仍包含全部生命周期来源且旧调度仍为相关fleet scan；GREEN确认hot函数只读累计事实/90日日桶/推广并持久化next check |
| generation119完整临时Schema、行为与校准 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟44.437s；用例43.57s | 0 | PASS：全部Schema安装至119；浏览/独立访客/页面/下载/收藏/评论/评分/维度的insert/update/hide/republish/delete/cascade delta准确；故意写入77后rebuild恢复权威事实；清理后远端public仍为generation85 |
| 100k/1M/10M真实refresh history trap | 同一完整临时Schema中改名真实`content_unique_views`，创建字段不兼容的同名100k/1M/10M表，逐档`DISCARD PLANS`并`EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON)`调用真实refresh | 61.1683 / 31.9666 / 32.2441ms | 0 | PASS：三档均低于1s且计划不含trap关系；若hot函数仍读取旧历史列会直接因字段不存在失败，故不是小样本或缓存伪绿 |
| 100k/1M/10M真实到期调度计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestPopularityDecaySchedulerUsesDueIndexAtScaleIntegration$' -count=1 -v`，复用exact生产SQL | 用例7.09s；计划59.5663 / 30.2629 / 31.8556ms | 0 | PASS：全部计划命中`idx_content_popularity_stats_decay_due`且零Seq Scan；三档分别仅将1/10/100个到期route写入队列 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test12.466s；Vet4.736s；Build5.697s；tidy0.815s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿 |
| 前端全量门禁 | bundled Node运行全部`app/_lib/*.test.mts`、`tsc --noEmit`、全仓ESLint及注入非秘密HTTPS origins的Next production build | Test2.765s；Type3.141s；Lint24.327s；Build23.226s | 0 / 0 / 0 / 0 | PASS：143/143 tests；类型和lint全绿；Next16编译、31 workers与58/58静态页生成通过 |
| 格式、差异、旧路径与台账 | gofmt；双仓`git diff --check`；generation119 hot函数/周期调度定向source合同；`verify_findings`严格及`-allow-open` | 后端0.930s；前端0.714s；source3.857s；严格0.948s；开放0.888s | 0 / 0 / 0 / 1（预期）/ 0 | PASS：指定Go源无格式差异、双仓无空白错误（仅Git既有LF→CRLF提示）；hot函数全部生命周期来源及周期相关fleet谓词零命中；449/449，CLOSED=189、OPEN=260，High=104、Medium=174、Low=49、UNRESOLVED=122；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

项目热度刷新成本现在由固定累计事实、最多90日日桶和当前推广决定；生命周期历史增长到1000万行不会增加hot refresh或到期筛选的扫描工作。

## 2026-08-22：PERF-035 全局评分写时累计与集合离线校准关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED与定向合同GREEN | generation120全局累计/rebuild及Worker旧路径source合同先行，随后`go test ./internal/database ./internal/httpapi -run 'GlobalRating&#124;PopularityWorkerNever' -count=1` | RED墙钟7.812s；GREEN8.176s | 1（预期）/ 0 | PASS：RED精确命中generation119和旧refresh/5秒分支；GREEN命中adjust/rebuild/trigger并证明Worker旧函数与staleness文本零命中 |
| generation120完整临时Schema与全局delta/rebuild | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟44.979s；用例44.12s | 0 | PASS：完整空Schema安装至120；全局insert、overall update、hidden、republish、delete精确；故意77/77/1损坏由set rebuild恢复1/4/4；PERF034行为/scale合同继续绿，远端public generation85不变 |
| 100k/1M/10M exact集合校准计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestGlobalRatingSetRebuildAtScaleIntegration$' -count=1 -v` | 墙钟29.722s；用例29.64s；计划134.523ms / 572.4396ms / 4.9420804s | 0 | PASS：100个route中route1 owner评分分别排除1k/10k/100k；其余count/sum/average精确；exact production SQL计划不含逐评分`content_target_owner_id` |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test12.431s；Vet4.700s；Build5.701s；tidy0.832s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿 |
| 前端全量门禁 | bundled Node运行全部`app/_lib/*.test.mts`、`tsc --noEmit`、全仓ESLint及注入非秘密HTTPS origins的Next production build | Test2.744s；Type3.057s；Lint24.299s；Build23.430s | 0 / 0 / 0 / 0 | PASS：143/143 tests；类型和lint全绿；Next16编译、31 workers与58/58静态页生成通过 |
| 格式、差异、旧路径与台账 | gofmt；双仓`git diff --check`；旧refresh/5秒路径定向source合同；`verify_findings`严格及`-allow-open` | 后端1.104s；前端0.807s；source2.809s；严格0.929s；开放1.028s | 0 / 0 / 0 / 1（预期）/ 0 | PASS：指定Go源无格式差异、双仓无空白错误（仅Git既有LF→CRLF提示）；Worker旧全局函数与5秒staleness零命中；449/449，CLOSED=190、OPEN=259，High=105、Medium=174、Low=49、UNRESOLVED=121；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

全局评分统计不再由任一项目热度任务触发全类型扫描；日常路径只做一行类型累计，完整集合扫描仅存在于显式离线校准。

## 2026-08-22：PERF-036 后台项目写时投影与复合游标关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED与定向合同GREEN | generation121投影/索引、列表source及前端cursor合同先行；后端运行`go test ./internal/database ./internal/httpapi -run 'AdminProject' -count=1`，前端运行新增Node合同 | RED后端3.0s、前端117.956ms；最终定向包9.03s、前端122.618ms | 1（预期）/ 0 | PASS：RED精确命中generation120、旧COUNT/ILIKE/OFFSET及前端total/offset；GREEN覆盖严格scope cursor、exact production SQL、Schema/source和cursor-history consumer |
| generation121完整临时Schema与写时同步 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟46.211s；用例46.13s | 0 | PASS：完整空Schema安装至121；项目route创建即投影，项目改名/全文向量、metrics及popularity更新同事务同步；PERF034/035既有行为和10M trap继续通过，清理后远端public generation85不变 |
| 100k/1M exact项目列表计划与回归阈值 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestAdminProjectPaginationAndSearchStayIndexedAtScaleIntegration$' -count=1 -v` | 墙钟32.583s；100k首屏/深页/类型/搜索62.1933/60.5087/58.2921/108.7212ms；1M为28.9624/30.3323/29.6335/610.2039ms | 0 | PASS：exact生产SQL均limit+1且无OFFSET/项目表Seq Scan；全局/类型页命中对应复合索引，搜索由规划器在GIN和有序heat索引间选择；四类均低于2秒硬阈值，深页恰返回41行 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test11.237s；Vet2.978s；Build4.003s；tidy0.336s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿；既有dashboard无JSON元数据契约同时通过 |
| 前端全量门禁 | bundled Node运行全部`app/_lib/*.test.mts`、`tsc --noEmit`、全仓ESLint及注入非秘密HTTPS origins的Next production build | Test2.142s；Type9.269s；Lint23.219s；Build23.179s | 0 / 0 / 0 / 0 | PASS：144/144 tests；类型和lint全绿；Next16编译、31 workers与58/58静态页生成通过 |
| 格式、差异、旧路径与台账 | gofmt；双仓`git diff --check`；admin项目定向source合同及前端旧字段检索；`verify_findings`严格及`-allow-open` | 定向2.212s；diff0.658s；严格0.641s；开放0.641s | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；列表旧COUNT/ILIKE/OFFSET和前端projectOffset/total/offset零残留；449/449，CLOSED=191、OPEN=258，High=105、Medium=174、Low=49、UNRESOLVED=121；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

后台项目目录不再把页深和总项目数转化为重复扫描成本；搜索和排序事实由写入维护，客户端只持有服务端签发、筛选条件绑定的opaque continuation。

## 2026-08-22：PERF-037 搜索重建稳定ID流、批量投影与硬资源预算关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED与定向合同GREEN | 先加入generation122进度Schema及rebuild source合同，运行`go test ./internal/database ./internal/searchindex -run 'SearchRebuildProgress&#124;RebuildStreams' -count=1`；实现后运行searchindex全部单测 | RED：database命中generation121，worker命中旧全量路径及缺失页/预算/预聚合；最终searchindex0.976s | 1（预期）/ 0 | PASS：旧`loadDocuments(kind,nil)`、collection slice、nil全选及项目相关聚合被精确捕获；GREEN锁定稳定ID页、8MiB预算、SQL输入截断、批量预聚合及持久进度 |
| JSONL与字段资源预算 | `go test ./internal/searchindex -run 'SearchRebuildImportRequestsStayWithinByteBudget&#124;SearchStringArraysHaveDeterministicItemAndByteBudgets' -count=1` | 包内约0.97s | 0 | PASS：20个约1MiB文档拆为至少3次导入，最大请求≤8MiB；任意字符串数组≤64项/8KiB且UTF-8截断有效 |
| 真实项目批量投影 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/searchindex -run '^TestLoadProjectDocumentsUsesBatchedPreaggregationsIntegration$' -count=1 -v` | 最终0.417s；用例0.34s | 0 | PASS：真实PostgreSQL一次页查询准确装配两语言、两标识、两creator、两tag、两个版本与loader去重；SQL侧Markdown/数组上限可执行 |
| 100k/1M完整稳定ID遍历及深页计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/searchindex -run '^TestSearchRebuildKeysetPagingScaleIntegration$' -count=1 -v` | 用例66.08s；100k 5.953s；1M 58.527s | 0 | PASS：逐页访问全部100k/1M行且无重漏，任一live ID slice≤500；半深页命中主键Index/Index Only Scan且无Seq Scan |
| generation122完整临时Schema与隔离 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟45.737s；用例45.66s | 0 | PASS：当前全部Schema/trigger/index/seed安装至122，`search_index_rebuild_progress`约束可创建；既有PERF034三档history trap继续绿；清理后远端public generation85不变 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test10.251s；Vet2.963s；Build3.748s；tidy0.335s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿 |
| 前端回归边界 | 本Finding无前端/API变化；复用紧邻PERF-036完成后的全量Node/Type/Lint/Next16 production build | Test2.142s；Type9.269s；Lint23.219s；Build23.179s | 0 / 0 / 0 / 0 | PASS：144/144 tests、类型、全仓lint、58/58静态页均绿；PERF-037未修改前端文件或公开DTO |
| 格式、差异、旧路径与台账 | gofmt；双仓`git diff --check`；searchindex全量选择旧路径source合同；`verify_findings`严格及`-allow-open` | diff后端0.473s/前端0.248s；严格0.650s；开放0.641s | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=192、OPEN=257，High=105、Medium=174、Low=49、UNRESOLVED=121；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

搜索重建的live内存现在由500行读取页、SQL输入上限、64项/8KiB字段预算和8MiB导入请求共同限定，不再随collection总文档数线性增长；多实例一致性与断点恢复仍按独立Finding继续处理。

## 2026-08-22：PERF-038 死信状态/聚合筛选、稳定游标与运维调用方关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED与定向合同GREEN | 先加入严格游标/parser/SQL/响应、generation123和前端调用方合同；运行后端HTTP/Database定向测试及bundled Node合同，随后复跑database/httpapi/queue与前端单测 | RED：后端报告parser未定义且generation仍为122，前端缺失死信组件；最终定向包与Node均通过 | 1 / 1（预期），随后0 | PASS：严格单值筛选、scope绑定cursor、limit+1、聚合元数据持久化、页envelope和管理端cursor/replay调用方全部命中 |
| 150条同时间戳两页行为 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestDeadLetterPaginationTraversesSameTimestampWithoutGapsIntegration$' -count=1 -v` | 用例0.55s | 0 | PASS：目标聚合的150条unresolved以100+50两页完整遍历，无重复、遗漏、其他聚合或20条replayed泄漏；cursor保留同时间戳ID排他边界 |
| 100k/1M真实PostgreSQL筛选与计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestDeadLetterPaginationStaysIndexedAtScaleIntegration$' -count=1 -v` | 包9.488s；用例8.38s | 0 | PASS：100k unresolved/replayed/aggregate为56.1036/58.5384/56.8639ms；1M为32.2686/27.2891/28.8402ms；exact生产builder全部命中Index Scan/Index Only Scan、零Seq Scan并低于2秒 |
| generation123完整临时Schema与隔离 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 墙钟43.791s；用例43.71s | 0 | PASS：全部Schema/trigger/index/seed安装到123，死信聚合列及状态/聚合页索引可创建；既有规模合同继续绿，清理后远端public generation85不变 |
| replay边界与聚合来源回归 | 既有`TestOutboxFailureMetricsRequirePersistedTransitions`及`TestAdminDeadLetterReplayRestoresPublishAndConsumerFailuresIntegration`，并加强publish/consumer aggregate断言 | 0.67s / 1.22s | 0 / 0 | PASS：publish与consumer失败均保存聚合标识；单条replay仍保持原事务锁、发布恢复、状态更新与审计边界 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test16.255s；Vet3.528s；Build4.439s；tidy0.398s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿 |
| 前端全量门禁 | bundled Node运行全部`app/_lib/*.test.mts`、`tsc --noEmit`、全仓ESLint及注入非秘密HTTPS origins的Next production build | Test2.313s；Type2.847s；Lint26.232s；Build23.583s | 0 / 0 / 0 / 0 | PASS：145/145 tests；类型和lint全绿；Next16编译、31 workers与58/58静态页生成通过 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=193、OPEN=256，High=105、Medium=174、Low=49、UNRESOLVED=121；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

死信后台现在可按状态和聚合身份连续遍历全部历史；同时间戳记录仍由ID稳定续页，单页与数据库工作量都有硬上限，既有单条replay事务语义保持不变。

## 2026-08-22：PERF-039 MRPack远端加载器元数据合并、条件请求与资源预算关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增顺序预检/创建语义和32并发同键解析测试，运行`go test ./internal/httpapi -run '^TestResolveMRPackLoaderVersion(SharesRemoteMetadataAcrossPreviewAndCreate&#124;CoalescesConcurrentRemoteMetadata)$' -count=1 -v` | 墙钟7.9s；包1.118s | 1（预期） | PASS：旧实现顺序精确发出2次相同Forge metadata请求，并发精确发出32次，直接复现Finding而非编译占位失败 |
| 双层缓存与同键合并GREEN | 相同定向测试；另跑全部新缓存测试并10次`-shuffle=on`重复 | 首次墙钟7.9s/包1.165s；10次shuffle墙钟8.2s/包1.411s | 0 / 0 | PASS：预检→创建只下载/解析1次；32并发只下载/解析1次；重复次序下无缓存隔离或singleflight竞态 |
| 条件校验与失败恢复 | `TestFetchMinecraftSourceRevalidatesExpiredMetadata`、`TestFetchMinecraftSourceDoesNotCacheFailures` | 包内<0.01s | 0 | PASS：初始200保存ETag/Last-Modified，强制过期后请求携带两个条件头并以304复用原payload，随后继续命中新鲜缓存；503不缓存且下一调用成功重试 |
| 并发与内存硬预算 | `TestMinecraftSourceFetchesHaveGlobalConcurrencyBudget`、`TestMinecraftSourceCacheHasEntryAndByteBudgets` | 并发用例0.03s | 0 | PASS：12个不同URL实际并行时最大外呼精确受4槽限制；URL缓存最多64项/64MiB，单响应既有16MiB上限保持，具体loader结果最多256项 |
| HTTP包与源码边界 | `go test ./internal/httpapi -count=1`；`go vet ./internal/httpapi`；检索全部`minecraftVersionHTTPClient.Do/fetchMinecraftSource/resolveMRPackLoaderVersion` | Test3.7s/包1.228s；Vet2.8s；检索<1s | 0 / 0 / 0 | PASS：HTTP包全绿；远端版本HTTP只有统一download边界，MRPack preview只有统一resolver入口，未留下直接绕过缓存的生产路径 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test13.889s；Vet9.845s；Build11.130s；tidy0.731s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿 |
| 前端回归边界 | 本Finding不改变前端/API；复用紧邻PERF-038的全部Node/Type/Lint/Next16 production build，并核对`favorite-modpack-export.tsx`仍为preflight后显式create | Test2.313s；Type2.847s；Lint26.232s；Build23.583s | 0 / 0 / 0 / 0 | PASS：145/145 tests、类型、全仓lint与58/58静态页均绿；现有两阶段调用无需迁移 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=194、OPEN=255，High=105、Medium=174、Low=49、UNRESOLVED=121；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

MRPack预检与紧随其后的创建不再重复传输或解析同一大型loader目录，并发请求也不会线性增加同源外呼；这是一层有硬预算的进程缓存，不取代后续持久权威快照与跨实例协调修复。

## 2026-08-22：BUG-066 / PERF-040 MRPack必需依赖有界批图与失败关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 先加入依赖source合同，运行`go test ./internal/httpapi -run '^TestFavoriteExportDependencyTraversalUsesOneBoundedGraphAndBatchedFiles$' -count=1 -v` | 墙钟7.9s；包1.109s | 1（预期） | PASS：精确命中旧`cursor<100`、逐route resolver、Query/Scan错误continue，并报告缺少图/节点/边/并发/错误sentinel合同 |
| 定向GREEN与重复并发 | source合同、40候选顺序/并发和required不可确认测试；随后10次`-shuffle=on` | 首次墙钟8.1s/包1.160s；10次包0.399s | 0 / 0 | PASS：旧路径零残留；40结果顺序稳定且最大8 worker；普通compatible失败不误判，required reason稳定命中create 422合同 |
| 150节点环、完整preview与1001上限 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestFavoriteExportDependencyGraphTraversesCyclesAndReportsLimitsIntegration$' -count=1 -v` | 最终墙钟约40.033s；用例38.91s | 0 | PASS：完整generation123中150节点/150边闭环以1 SQL终止；扇出preview为1根+149依赖、图SQL=1、149唯一依赖provider请求并发2..8；缓存后完整build仅补根请求；1001节点链显式limit错误 |
| 1M关系真实PostgreSQL计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestFavoriteExportDependencyGraphStaysIndexedAtMillionRelationshipsIntegration$' -count=1 -v` | 包6.637s；用例6.51s；exact loader150.9997ms | 0 | PASS：1M route/mod/relationship噪声中完整返回1000可达节点；EXPLAIN执行21.665ms，命中source relationship及route索引，`mod_relationships/public_routes`零Seq Scan并低于2秒阈值 |
| HTTP包 / Vet / 源码边界 | `go test ./internal/httpapi -count=1`；`go vet ./internal/httpapi`；检索旧cursor/resolver/吞错路径；目标diff | Test墙钟4.297s/包1.427s；Vet3.099s；其余<1s | 0 / 0 / 0 / 0 | PASS：HTTP包全绿；关系读取唯一生产入口逐层传播Query/Scan/iterate错误，旧100节点及逐route映射/配置查询零残留 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test12.565s；Vet9.270s；Build10.514s；tidy0.759s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿 |
| 前端回归边界 | 本组成功DTO/前端不变；复用PERF-038后的全量Node/Type/Lint/Next16 build | Test2.313s；Type2.847s；Lint26.232s；Build23.583s | 0 / 0 / 0 / 0 | PASS：145/145 tests、类型、lint与58/58静态页均绿；新增422由既有API错误显示处理，无调用方迁移 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=196、OPEN=253，High=105、Medium=174、Low=49、UNRESOLVED=121；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

依赖规模现在只增加单条递归SQL内部的索引工作和固定8路候选解析，不再增加数据库往返；超过明确图预算或任何必需依赖不可解析都会失败关闭，而不是生成表面ready但缺文件的包。

## 2026-08-22：PERF-041 MRPack导出历史状态筛选与稳定游标关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED与定向GREEN | 先加入严格parser/cursor/SQL、generation124索引和前端page消费合同，运行`go test ./internal/httpapi ./internal/database -run 'Favorite.*Export.*Page&#124;FavoriteModpackExportHistory' -count=1`及bundled Node合同 | RED：parser/cursor/SQL未定义、generation仍123、前端缺Page；GREEN后HTTP/Database分别2.411s/0.517s，Node 0.11s | 1 / 1（预期），随后0 | PASS：全部参数单值、scope绑定、limit+1、双索引、page envelope、状态筛选及load more合同均命中 |
| 150条同时间戳及owner/status隔离 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestFavoriteExportHistoryTraversesEveryTaskWithStableKeysetsIntegration$' -count=1 -v` | 包45.818s；用例44.71s（含完整generation124安装） | 0 | PASS：同owner 150任务以100+50完整遍历且无重漏；foreign owner记录不可见；ready以17条页遍历精确50条且每项状态正确 |
| 1M任务真实PostgreSQL计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestFavoriteExportHistoryUsesBoundedIndexesAtMillionTasksIntegration$' -count=1 -v` | 用例7.07s；all/ready墙钟127.4324/162.378ms | 0 | PASS：all命中owner-created、ready命中owner-status-created；数据库执行0.096/0.141ms，任务表零Seq Scan且均低于2秒 |
| generation124与读取错误边界 | Schema source门禁及完整临时Schema行为测试；`favoriteModpackExports`在完整Scan后检查`rows.Err()` | 含于上述44.71s完整Schema及定向包 | 0 | PASS：新索引可安装并被exact SQL使用；列表Query/Scan/iterate错误均不能返回部分200；ARCH-018其余详情/Worker错误仍开放 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | 全仓最慢queue3.543s；其余命令同批通过 | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿 |
| 前端全量门禁 | bundled Node运行全部`app/_lib/*.test.mts`、全仓ESLint、`tsc --noEmit`及注入非秘密HTTPS origins的Next16 production build | Test2.106s；Build编译7.5s、Type阶段10.7s | 0 / 0 / 0 / 0 | PASS：146/146 tests、lint/type全绿、58/58静态页生成；API loader和中英文筛选/加载更多调用方完成迁移 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=197、OPEN=252，High=105、Medium=174、Low=49、UNRESOLVED=121；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

导出历史不再随固定100条窗口增长而丢失可达性；状态筛选和同时间戳续页都由服务端索引与scope绑定cursor保证，单页响应和数据库工作量保持有界。

## 2026-08-22：PERF-042 蓝图处理硬预算、用户准入与批量持久化关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 先加入资源上限、材质基数、共享信号量、用户准入/索引及批写source合同，运行定向HTTP/Database测试 | RED精确报告源512MiB、解码256MiB、体积16,777,216、非空气块8,388,608均超过新门槛，8,193种材质未拒绝，缺少generation125/信号量/CopyFrom/用户预算 | 1（预期） | PASS：失败直接复现合法输入资源放大及逐行持久化，不是缺符号或编译占位 |
| 定向预算与格式GREEN | `go test ./internal/httpapi ./internal/database`及资源合同测试 | HTTP 2.237s；Database 0.115s | 0 | PASS：32MiB源/解码、2,097,152体积、250,000块、8,192材质、64MiB规范JSON、2处理槽和4用户active任务全部命中；规范JSON完整round-trip且越界失败 |
| generation125、用户配额与8192材质批写 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestBlueprintJobAdmissionAndBulkMaterialsIntegration$' -count=1 -v` | 用例36.01s；CopyFrom 244.6358ms | 0 | PASS：完整临时Schema安装到125；同用户4个active后第5个在事务内得到明确限额错误，完成一个后可再次准入；一次CopyFrom精确持久8192行 |
| 100k/1M真实PostgreSQL准入计划 | 同包`TestBlueprintJobAdmissionStaysIndexedAtScaleIntegration` | 用例2.73s；墙钟53.7731/27.5669ms | 0 | PASS：100k/1M exact查询都只取4行并命中`idx_blueprint_jobs_creator_active` Index Only Scan；数据库执行0.047/0.123ms，无`blueprint_jobs` Seq Scan |
| 最大合法工作负载内存与CPU | 同包`TestBlueprintMaximumDocumentProcessingHasBoundedAllocationsIntegration` | 用例0.35s；处理347.8268ms | 0 | PASS：250,000块、500×1×500文档完成材质、14,390,116字节规范JSON和封面渲染；总分配109,113,520字节，低于512MiB和10秒硬门槛 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 同批执行 | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿 |
| 前端回归边界 | 本Finding成功DTO、路由与第一方调用不变；复用紧邻PERF-041的全部Node/Type/Lint/Next16 production build | Test2.106s；Build编译7.5s、Type阶段10.7s | 0 / 0 / 0 / 0 | PASS：146/146 tests、lint/type及58/58静态页均绿；新增413/429由既有通用错误呈现处理，无调用方迁移 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=198、OPEN=251，High=106、Medium=174、Low=49、UNRESOLVED=120；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

单个合法蓝图及并发处理现在都受独立硬预算约束；用户历史增长不会放大准入查询，材质写入不再产生逐行数据库往返。相邻palette安全、转换正确性和完整系统矩阵仍按各自Finding继续开放；材料revision范围由PERF-043独立处理。

## 2026-08-22：PERF-043 蓝图材料revision按实际namespace限定关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 先加入生产SQL namespace谓词/确定性排序和generation126索引合同，运行`go test ./internal/httpapi ./internal/database -run 'TestBlueprintMaterialRevision' -count=1 -v` | HTTP1.148s；Database0.485s | 1 / 1（预期） | PASS：旧生产查询明确缺少`source_namespace=any`，Schema仍125且没有匹配索引；失败直接复现Finding |
| 定向GREEN与去重 | 相同合同、生产helper去重单测及目标包`go test ./internal/httpapi ./internal/database` | HTTP2.228s；Database0.111s | 0 / 0 | PASS：SQL仅接受实际namespace数组且以time/ID确定最新行；`minecraft`重复材料只产生一个参数，空namespace不进入查询 |
| 100k/1M真实PostgreSQL计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestBlueprintMaterialRevisionLookupStaysNamespaceScopedAtScaleIntegration$' -count=1 -v` | 用例10.20s；墙钟62.627/30.3868ms | 0 | PASS：两档全站active revision中都只返回used_alpha/used_beta最新行；命中`idx_catalog_import_revisions_material_namespace`，数据库执行0.049/0.043ms且无revision Seq Scan |
| generation126完整临时Schema | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 用例43.65s | 0 | PASS：全部Schema/trigger/index/seed安装到126，新表达式部分索引创建成功，清理后远端public generation85不变 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 全仓最慢queue4.512s；其余同批完成 | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿 |
| 前端回归边界 | 本Finding不改变路由、请求、成功/错误DTO或前端；复用紧邻PERF-041的全部Node/Type/Lint/Next16 production build | Test2.106s；Build编译7.5s、Type阶段10.7s | 0 / 0 / 0 / 0 | PASS：146/146 tests、lint/type和58/58静态页均绿，无调用方迁移 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=199、OPEN=250，High=107、Medium=174、Low=49、UNRESOLVED=119；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

蓝图详情的目录装饰成本现在只与该蓝图使用的去重namespace数量相关；全站无关revision不再进入匿名请求热路径。ARCH-019的错误可见性仍保持独立开放。

## 2026-08-22：PERF-044 OSS用户额度原子桶、预留与离线校准关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 先加入上传热路径零SUM、预留/结算主链及generation127双桶/trigger/rebuild合同，运行`go test ./internal/httpapi ./internal/database -run 'TestOSS.*Quota' -count=1 -v` | HTTP1.144s；Database0.496s | 1 / 1（预期） | PASS：旧`enforceUserFileUploadLimits`直接命中历史SUM，Schema仍126且无额度事实；失败复现Finding而非缺符号 |
| 定向GREEN与checked arithmetic | 相同source合同、`quotaAllows`边界/溢出、既有额度读取故障测试及HTTP/Database目标包 | 定向HTTP1.230s、Database0.103s；完整目标包1.496/0.142s | 0 | PASS：presign reserve、失败/abort release、complete同事务settle+insert均在生产主链；单/日/总限额错误继续失败关闭，零历史SUM |
| 真实双连接并发预留 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestOSSQuotaConcurrentReservationsCannotOversubscribeIntegration$' -count=1 -v` | 用例1.93s；竞态1.4107918s | 0 | PASS：隔离非public Schema中两个连接同时600+600/1000，advisory lock后精确1成功/1额度失败；reservation count=1、source/stored reserved=600 |
| generation127预留/结算/trigger/rebuild | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestOSSQuotaReservationSettlementTriggersAndRebuildIntegration$' -count=1 -v` | 用例42.39s | 0 | PASS：600+500拒绝、600+400占满；600 source/550 stored结算，改500与删除反向delta正确；新250/200 active在故意改999后由两rebuild精确恢复且额度API一致 |
| 1M历史陷阱 | 同包`TestOSSQuotaReadsIgnoreMillionFileHistoryIntegration` | 用例1.50s；四个桶读235.1216ms | 0 | PASS：一百万行单列`oss_files`存在时额度API与准入仍返回预置活动/预留事实；若触碰旧source/size列会直接失败，实测低于2秒 |
| generation127完整临时Schema | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 用例41.85s | 0 | PASS：全部Schema/trigger/function/index/seed安装到127，清理后远端public generation85不变 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test最慢queue4.688s；其余同批完成 | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿 |
| 前端回归边界 | 本Finding不改变上传/完成/额度DTO或第一方调用；复用紧邻PERF-041的全部Node/Type/Lint/Next16 production build | Test2.106s；Build编译7.5s、Type阶段10.7s | 0 / 0 / 0 / 0 | PASS：146/146 tests、lint/type和58/58静态页均绿，无调用方迁移 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=200、OPEN=249，High=108、Medium=174、Low=49、UNRESOLVED=118；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

用户上传准入、完成和额度显示现在只读取单用户事实；全历史SUM被限制到显式离线校准。持久预留与完成结算共享用户锁，不能再由并发请求基于同一旧总量超额。

## 2026-08-22：PERF-045 评论/回复/插眼keyset与目标总数事实关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 先加入三类cursor严格合同、handler零OFFSET/COUNT源码门禁及generation128 counter/index合同，运行`go test ./internal/httpapi ./internal/database` | 3.600s | 1（预期） | PASS：cursor类型/编码函数缺失，Schema仍127且无目标计数事实；失败直接复现旧实现边界 |
| 定向GREEN | `go test ./internal/httpapi ./internal/database`及cursor/source/schema目标测试 | HTTP2.263s；Database0.526s | 0 | PASS：三类scope绑定cursor、跨scope/未知字段拒绝、零OFFSET/根COUNT及generation128合同全绿 |
| 百万级真实PG分页 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCommentKeysetPagesAndCounterFactsAtMillionRowScaleIntegration$' -count=1 -v` | 用例23.83s；各深页57.577–59.2696ms | 0 | PASS：1M根的latest/oldest/hot/replies、100k直接回复及2k watch全部命中复合索引且零Seq Scan；limit+1有界 |
| 并发插入稳定性与精确总数 | 同一规模用例先读首屏、插入更晚根评论再按锚点读次页；1.1M目标事实扣除110k拉黑作者事实 | counter57.336ms | 0 | PASS：次页无首屏重复且新项不漂入；返回990000且查询不依赖comments历史行数 |
| generation128完整Schema/counter生命周期 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 用例42.49s | 0 | PASS：完整Schema安装；comment insert事实=1、hidden移除、deleted恢复、故意99漂移后离线rebuild=1；清理后远端public generation85不变 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test16.231s，最慢antiabuse4.430s；其余同批完成 | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿 |
| 前端回归边界 | 本Finding保持全部响应字段和cursor string类型；第一方`comment-api`/评论区/watch面板本已opaque传递cursor，复用紧邻PERF-041全量前端门禁 | Test2.106s；Build编译7.5s、Type阶段10.7s | 0 / 0 / 0 / 0 | PASS：146/146 tests、lint/type和58/58静态页均绿，无前端源码或协调迁移 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=201、OPEN=248，High=109、Medium=174、Low=49、UNRESOLVED=117；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

热门目标和深页的数据库工作现在由固定limit和索引锚点决定；精确总数来自写时目标事实。watch详情的逐目标解析仍由PERF-046独立处理，未借本项分页改造提前关闭。

## 2026-08-22：PERF-046 我的评论插眼双重N+1关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 先加入watch handler必须单次评论装配、批量目标map且头像按unique stored URL缓存的源码回归，运行`go test ./internal/httpapi -run 'TestMyCommentWatchesBatch|TestCommentAuthorAvatars' -count=1 -v` | 1.074s | 1（预期） | PASS：旧handler缺批量目标解析且头像没有唯一对象缓存，直接复现双重N+1 |
| 定向GREEN | 相同源码门禁及目标批量/完整handler目标用例 | source1.085s | 0 | PASS：handler只有一次`queryCommentItems`和一次`queryCommentTargetsByInternal`，无逐项resolver；头像map存在 |
| 100不同目标恒1查询 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCommentTargetsBatchResolveOneHundredRowsInOneQueryIntegration$' -count=1 -v` | 用例0.38s；批量61.8907ms | 0 | PASS：100 approved Mod+重复identity由1条SQL返回100个确定详情；未知类型缺失，pending匿名不可见且提交者可见 |
| 全17类型与真实handler SQL预算 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCommentTargetBatchAllBranchesCompileAgainstFullSchemaIntegration$' -count=1 -v` | 用例41.26s | 0 | PASS：17类分别及合并都只执行1条目标SQL；100不同目标/watch完整返回，limit1与limit100数据库语句均精确12条 |
| 故障/兼容边界 | batch query/scan/rows任一错误直接返回handler 500；明确不可见identity不进入map并保持旧type-only fallback；响应DTO/cursor不变 | 同定向/完整Schema用例 | 0 | PASS：不再用数据库失败作为跳项/权限控制流；第一方调用方无需迁移 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | Test10.198s，最慢queue3.383s；其余同批完成 | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿 |
| 前端回归边界 | 本Finding无前端、路由、请求或响应字段变化；复用紧邻PERF-041全量前端门禁 | Test2.106s；Build编译7.5s、Type阶段10.7s | 0 / 0 / 0 / 0 | PASS：146/146 tests、lint/type和58/58静态页均绿 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=202、OPEN=247，High=110、Medium=174、Low=49、UNRESOLVED=116；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

watch页的数据库语句数现在与1到100项的page size无关；目标可见性和详情保持原语义，唯一头像对象只处理一次。

## 2026-08-22：PERF-047 玩家档案纹理N+1关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 先加入列表必须单次batch且禁止逐档案纹理调用的源码合同，运行`go test ./internal/httpapi -run '^TestPlayerProfileListsBatchTextureAssembly$' -count=1` | 7.892s | 1（预期） | PASS：旧实现batch调用为0且仍有逐档案loop，失败直接复现Finding |
| 定向GREEN | 同一源码合同及`go test ./internal/httpapi ./internal/database` | source8.261s；目标包3.840s | 0 / 0 | PASS：列表只调用一次`loadPlayerTexturesForProfiles`，单详情委托同一helper；HTTP/Database回归全绿 |
| 真实PG恒定查询预算 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestPlayerProfileTextureAssemblyUsesConstantQueriesIntegration$' -count=1 -v` | 用例0.779s；1/100项分别约0.13/0.14s | 0 | PASS：1档案与100档案都精确2条SQL；100档案的200纹理完整，viewer skin衣柜true、cape false |
| Schema/API边界 | 核查skin generation128 DDL与生产loader；既有public ID唯一索引、texture/wardrobe主键覆盖连接 | 同定向审查 | 0 | PASS：无DDL/generation/远端public变化；公开/我的档案请求、排序、权限和DTO不变，数据库错误仍失败关闭 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；tidy前后SHA-256 | Test10.088s，最慢queue3.372s；Vet3.952s；Build4.543s；tidy0.767s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建及模块图稳定全绿 |
| 前端回归边界 | 本Finding无路由、请求、响应字段或状态码变化；复用紧邻PERF-041全量前端门禁 | Test2.106s；Build编译7.5s、Type阶段10.7s | 0 / 0 / 0 / 0 | PASS：146/146 tests、lint/type及58/58静态页均绿，无调用方迁移 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=203、OPEN=246，High=111、Medium=174、Low=49、UNRESOLVED=115；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

玩家档案页的数据库往返现在固定为列表与纹理各一次，档案数量只影响同一集合查询的有界结果行数，不再产生逐档案网络往返。

## 2026-08-22：PERF-048 公共皮肤目录全文投影与稳定游标关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效后端/前端RED | 先加入严格cursor、handler零OFFSET/COUNT/ILIKE/查询期数组转换、generation129投影合同和Next16 cursor历史源码合同，运行定向Go/Node测试 | Go与Node均1（预期） | 1 / 1（预期） | PASS：旧handler仍使用offset、同步count和包含搜索，Schema仍128，前端没有cursorHistory；失败直接复现Finding而非无关占位 |
| 定向GREEN | `go test ./internal/httpapi ./internal/database`及`node --test app/_lib/skin-catalog-pagination.test.mts` | HTTP1.170s；Database0.498s；Node0.132s | 0 | PASS：严格请求/cursor、生产SQL、generation129及前端调用合同全绿；旧offset/total路径零残留 |
| 真实handler分页与SQL预算 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestSkinCatalogHandlerPagesWithConstantQueriesIntegration$' -count=1 -v` | 用例0.41s；包1.545s | 0 | PASS：36项两页完整遍历无重复；首/次页各精确2条SQL（窄页+详情批量），成功DTO只含limit/hasMore/nextCursor分页事实 |
| 百万级exact生产SQL计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestSkinCatalogPaginationStaysIndexedAtMillionScaleIntegration$' -count=1 -v` | 用例57.423s；装载+全部计划56.09s | 0 | PASS：published深页61.9935ms、views深页270.0025ms、kind/model过滤61.0444ms、选择性FTS355.2036ms；全部无投影Seq Scan或基础宽表排序。FTS谓词可用GIN，该夹具规划器选择匹配published排序索引后应用谓词；Schema中GIN存在 |
| 并发写入稳定性 | 同一百万规模用例读取首屏、插入更新的公开首项后按原cursor读取次页 | 同用例 | 0 | PASS：次页无首屏重复，新插入项不漂入已锚定历史；cursor的排序tuple与scope稳定 |
| generation129投影生命周期 | 完整临时Schema用例及皮肤投影集成：真实OSS blob/asset、route/popularity、private/public与离线rebuild | 完整安装用例46.85s（命令墙钟47.727s） | 0 | PASS：公开asset可全文命中；popularity view=11写时传播；private删除投影、public恢复；故意改999后rebuild恢复11；全部Schema/trigger/index创建成功，远端public generation85不变 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；tidy前后SHA-256 | Test12.223s，最慢queue4.513s/antiabuse4.073s；Vet5.392s；Build6.331s；tidy1.0s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析与构建全绿；`go.mod`/`go.sum` SHA-256均稳定 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；以测试HTTPS API/SITE环境执行Next16 production build | 147 tests 2.116s；Type12.764s；Lint26.981s；Build25.338s（compile8.7s、TS11.0s） | 0 / 0 / 0 / 0 | PASS：147/147、type/lint及58/58静态页全绿；首次无环境构建按部署合同正确失败，提供测试HTTPS配置后成功，不以跳过环境门禁冒充构建通过 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=204、OPEN=245，High=112、Medium=174、Low=49、UNRESOLVED=114；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

公共皮肤目录的数据库工作现在由窄投影、匹配排序tuple和固定limit决定；查询不再扫描宽asset正文、丢弃深OFFSET前缀或同步重复COUNT。调用方已协调迁移opaque cursor，不以客户端隐藏总数替代服务端成本修复。

## 2026-08-22：PERF-049 用户衣柜固定5000完整对象窗口关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 先加入严格user/filter cursor、零固定5000窗口、same-scope total及前端增量追加源码合同；运行定向Go/Node测试 | Go编译RED；Node121.9836ms | 1 / 1（预期） | PASS：后端缺全部page/cursor符号，前端仍把固定数组normalize进单状态；失败直接复现Finding边界 |
| 定向GREEN与严格合同 | `go test ./internal/httpapi -run '^TestSkinWardrobe' -count=1`；`node --test app/_lib/skin-wardrobe-pagination.test.mts` | Go1.075s；Node129.6331ms | 0 / 0 | PASS：跨用户/kind/model/limit、未知字段、page/offset/重复参数均拒绝；SQL limit+1、同筛选窄COUNT、前端cursor追加合同全绿 |
| 5000真实PG handler与响应预算 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestSkinWardrobeHandlerBoundsFiveThousandItemsWithStableCursorIntegration$' -count=1 -v` | 用例1.16s；包2.244s | 0 | PASS：首/次页各100完整项、精确2 SQL和71,318字节，零重漏；首屏后更新项不漂入锚定次页；kind=skin/model=slim total精确833 |
| 半深游标EXPLAIN | 同一5000存量集成用例执行exact生产页SQL | 计划墙钟62.6448ms | 0 | PASS：命中`idx_skin_wardrobe_user_added`，无wardrobe Seq Scan或Sort；混合`added_at desc,asset_id asc`顺序与既有索引一致，只读limit+1 |
| Schema/兼容边界 | 核查generation129和`skin_wardrobe`既有Schema；完整后端包回归 | 同定向/全仓 | 0 | PASS：无DDL、回填或远端public写入；5000仍为写入存量上限，读取默认50/最大100；owner与approved public/unlisted可见性不变 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；tidy前后SHA-256 | HTTP1.476s、queue3.378s；其余同批；tidy<1s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析与构建全绿；`go.mod`/`go.sum` SHA-256稳定 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 149 tests2.691s；Type+Lint批27.8s；Build总23.9s（compile7.7s、TS10.8s） | 0 / 0 / 0 / 0 | PASS：149/149、type/lint及58/58静态页全绿；皮肤目录测试收紧到自身DTO，允许衣柜按审计建议独立保留same-scope total |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=205、OPEN=244，High=113、Medium=174、Low=49、UNRESOLVED=113；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

衣柜的5000项现在只是账户存量上限，不再成为一次数据库读取、网络响应和DOM渲染的隐式预算。每次请求最多100个完整资源，历史仍可由稳定cursor完整遍历，已装备的后页资源也保持在编辑select中可见。

## 2026-08-22：PERF-050 评分明细精确COUNT与深OFFSET关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 先加入严格target-scope cursor、零COUNT/OFFSET handler、limit+1 SQL及Next16 cursor历史合同；运行定向Go/Node测试 | Go编译RED；Node122.6824ms | 1 / 1（预期） | PASS：后端缺全部rating page/cursor符号；前端DTO和modal仍使用total/offset，2/2断言失败，直接复现Finding |
| 定向GREEN与严格合同 | `go test ./internal/httpapi -run 'TestRatingReviewPage\|TestRatingReviewsHandler' -count=1`；`node --test app/_lib/rating-pagination.test.mts` | 最终Go1.185s；Node113.8178ms | 0 / 0 | PASS：target/limit绑定cursor round-trip；跨目标、offset/page、未知/重复参数、超限、畸形/超长、错误版本/空锚点/尾随JSON及未知cursor字段均拒绝；生产SQL只有稳定tuple+limit+1且源码无同步COUNT/OFFSET；前端2/2绿 |
| 百万真实PG handler与并发稳定性 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestRatingReviewHandlerBoundsMillionRowsWithStableCursorIntegration$' -count=1 -v` | 用例5.53s；包6.635s；命令墙钟13.412s | 0 | PASS：一百万published评分下首/次页各20项、每页精确5 SQL且响应低于128 KiB；两页零重复，首屏后插入的新评分不漂入已锚定次页 |
| 半深游标EXPLAIN | 同一百万规模用例执行exact生产页SQL | 计划墙钟60.6126ms | 0 | PASS：命中既有`idx_content_ratings_target`，无`content_ratings` Seq Scan或Sort，只读取limit+1；不新增重复索引 |
| Schema/统计边界 | 核查generation129 rating Schema、`content_popularity_stats`汇总读取及前后端DTO | 同定向/全仓 | 0 | PASS：无DDL、回填、开发库重置或远端public写入；明细移除total，按钮/概览总数仍读取独立summary写时投影；LEGACY-017与TEST-040保持OPEN |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；tidy前后SHA-256 | Test/Vet/Build同批约14.93s，HTTP1.461s、queue3.388s；tidy0.705s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析与构建全绿；`go.mod`/`go.sum` SHA-256逐字稳定 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 151 tests2.532s；全批约51.1s；Build compile8.3s、TS11.4s | 0 / 0 / 0 / 0 | PASS：151/151、type/lint及58/58静态页全绿；modal只保存一个20项页与服务器cursor历史 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | tidy/diff/verifier同批约2s | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=206、OPEN=243，High=113、Medium=174、Low=49、UNRESOLVED=113；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

评分明细请求的数据库工作现在由固定页大小和既有目标复合索引决定，不再为展示20条记录同步聚合百万历史或丢弃深OFFSET前缀。汇总计数和明细遍历保持两个明确职责，第一方调用方已协调迁移且没有保留旧双协议。

## 2026-08-22：PERF-051 收藏无界列表与集合级请求放大关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 先加入四类严格cursor页、summary/索引/source合同及Next16三处cursor历史测试，运行定向Go/Node | Go编译RED；Node120.742ms | 1 / 1（预期） | PASS：旧实现缺页/cursor/summary/generation130符号且前端仍下载全部集合/项，失败直接复现Finding |
| 定向GREEN与delta合同 | `go test ./internal/httpapi ./internal/database -run 'Favorite.*(Page&#124;Pagination&#124;Summary&#124;Delta&#124;Visibility&#124;Generation)' -count=1`；`node --test app/_lib/favorite-pagination.test.mts`；TypeScript | HTTP1.078s；Database0.089s；Node117.1351ms | 0 / 0 / 0 | PASS：四类scope游标、limit+1 SQL、100目标summary、矩阵预算、互斥/去重/100变更delta、生产source和前端调用合同全绿 |
| 百万真实PG handler与并发稳定性 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestFavoriteHandlersBoundMillionRowsAndAvoidCollectionFanoutIntegration$' -count=1 -v` | 用例18.32s；包19.431s | 0 | PASS：1M收藏夹+单夹1M不同Mod收藏项；收藏夹/项首及次页各20项、分别固定2 SQL，100目标summary固定1 SQL；响应有界且两类并发新项不漂入锚定后页 |
| 三类exact生产SQL计划 | 同一百万规模用例执行收藏夹半深页、项半深页及100目标summary | 163.7303/177.2103/60.4907ms | 0 | PASS：分别命中`idx_favorite_collections_user_page`、`idx_favorite_items_collection_page`、`idx_favorite_items_target_collection`，目标大表无Seq Scan；页查询无Sort |
| generation130完整临时Schema | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 用例47.22s | 0 | PASS：全部Schema/trigger/index安装到130；清理后远端public generation85不变，无远端重置或永久写入 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 全批22.754s；最慢queue4.690s/antiabuse4.187s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 154 tests2.821s；Build compile8.3s、TS11.0s | 0 / 0 / 0 / 0 | PASS：154/154、type/lint及58/58静态页全绿；目录只summary当前页，三类收藏UI只保存有界页/cursor历史 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 同批约1.3s | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=207、OPEN=242，High=113、Medium=174、Low=49、UNRESOLVED=113；严格只因剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

收藏读取的数据库、网络和DOM成本现在由最大100项服务端页界约束；目录membership不再随收藏夹数量产生1+N，跨页选择只提交触碰delta。SEC-036、BUG-089、ARCH-023/024、LEGACY-018、STYLE-006和TEST-041保持独立开放。

## 2026-08-22：PERF-052 社区完整正文目录、包含搜索、重复COUNT与深OFFSET关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效前端RED与后端旧路径证据 | 先加入summary/cursor调用合同并运行`node --test app/_lib/community-post-pagination.test.mts`；后端审计证据与旧handler源码确认正文/ILIKE/COUNT/OFFSET同路 | Node116.0432ms | 1（预期） | PASS：旧前端无`CommunityPostSummary/hasMore/nextCursor`且目录仍依赖total/page；旧后端查询直接包含Finding四项放大条件 |
| 定向GREEN | `go test ./internal/httpapi ./internal/database`；`node --test app/_lib/community-post-pagination.test.mts`；TypeScript；受影响文件ESLint | HTTP2.314s；Database0.523s；Node109.3513ms | 0 / 0 / 0 / 0 | PASS：严格scope cursor、窄SQL、匿名/作者/moderator可见性分支、generation131合同和全部第一方summary调用类型全绿 |
| 百万真实PG handler与泄漏/并发 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCommunityPostCatalogBoundsMillionRowsAndOmitsBodiesIntegration$' -count=1 -v` | 用例17.60s；包17.724s | 0 | PASS：1M投影；每页固定5 SQL、24项、低于64KiB；首屏权威正文中的1MiB sentinel及`bodyMarkdown`均未出现在响应；两页零重复且并发新首项不漂入 |
| exact生产SQL计划 | 同一百万规模用例执行半深published keyset及选择性FTS relevance页 | 72.7062/74.0124ms | 0 | PASS：分别命中`idx_community_post_catalog_published`和`idx_community_post_catalog_search`；投影无Seq Scan，页查询有limit且无OFFSET/COUNT/正文 |
| generation131完整临时Schema | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 用例43.08s；包43.157s | 0 | PASS：投影、全文/版本GIN、九类排序索引及三组refresh trigger完整安装；清理后远端public generation85不变，无远端重置或永久写入 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | 最慢queue4.629s/antiabuse4.194s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 155 tests2.658s；Build compile7.7s、TS10.8s | 0 / 0 / 0 / 0 | PASS：155/155、type/lint及58/58静态页全绿；目录/首页/相关内容只消费有界summary页 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 同批约3.2s | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449，CLOSED=208、OPEN=241，High=113、Medium=174、Low=49、UNRESOLVED=113；严格仅因355项剩余OPEN/未定级及最终严重度目标按预期失败，allow-open通过 |

社区目录的数据库、响应和浏览器状态现在由最大100项的窄摘要页约束；全文搜索只访问写时索引，详情正文保持按需读取。PERF-053引用输入/N+1、ARCH-025其余错误语义、TEST-042、SEC-038及社区并发/语言Finding保持独立开放。

## 2026-08-22：PERF-053 社区引用无领域上限与逐项SQL关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 加入数量/长度/大小写重复/最大合法、batch source和编辑器预算合同；运行定向Go/Node | Go1.103s；Node115.8703ms | 1 / 1（预期） | PASS：旧实现接受33/65项、129/257字节和重复身份，缺少32/64常量及unnest/ANY路径，替换仍含singular resolver与QueryRow |
| 定向GREEN与前置400 | `go test ./internal/httpapi ./internal/database`；`node --test app/_lib/community-post-pagination.test.mts`；TypeScript；受影响ESLint | HTTP2.256s；Database cached；Node125.8057ms | 0 / 0 / 0 / 0 | PASS：所有边界、最大96项、批量源码合同和双语编辑器保护全绿；33 project真实handler在`Server{}`无数据库实例时先返回稳定400 |
| 最大集合真实PG固定往返 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCommunityPostReferenceResolutionAndReplacementStayConstantAtMaximumIntegration$' -count=1 -v` | 用例1.22s；包2.318s | 0 | PASS：2项和96项resolved/unresolved混合集合都固定9 SQL；最大集合精确持久化32 project、64 resource、48 unresolved，不随条目数增加往返 |
| SEC-037可见性交叉回归 | 同环境运行最大集合用例与`TestCommunityPostProjectReferencesRevalidateVisibilityForEveryViewer` | 用例1.17/0.75s；包2.035s | 0 | PASS：batch resolver仍拒绝他人pending，允许owner自己的pending；普通viewer隐藏绑定保持零元数据unavailable占位，批准后动态恢复 |
| Schema与远端边界 | 代码/迁移差异检查；沿用已验证generation131 | — | 0 | PASS：无DDL、索引、trigger或generation变化；真实PG仅创建session temporary shadow tables，连接关闭即清理，未重置或写入远端public |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | 最慢queue3.352s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 156 tests2.671s；Build compile7.7s、TS10.8s | 0 / 0 / 0 / 0 | PASS：156/156、type/lint及58/58静态页全绿；两类picker及submit共享32/64服务端预算 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 2.18s / 0.71s | 0 / 0 / 0；1（预期） | PASS：双仓差异检查无错误；449/449唯一ID，CLOSED=209、OPEN=240；`-allow-open`通过，严格模式仅因其余240项及最终严重度未收敛而按预期失败354项 |

单个社区写事务的引用工作现在最多96条领域事实、3条解析SQL和6条替换SQL；展示字段不再扩大revision。TEST-042、ARCH-025和社区修订/本地化/悬赏剩余Finding保持独立开放。

## 2026-08-22：PERF-054 内容修订历史无界加载关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 新增严格cursor/合并/Schema及Next16游标页合同；定向运行Go与Node | Go2.312s；Node114.1475ms | 1 / 1（预期） | PASS：旧后端缺全部page类型/函数和generation132索引，旧前端只有`{items}`且无cursor状态；失败来自新行为缺失而非环境 |
| 定向GREEN与前置400 | `go test ./internal/httpapi ./internal/database -run 'TestContentHistory' -count=1`；前端2项测试、TypeScript、ESLint | Go约1.15/0.50s；Node112.0262ms | 0 / 0 / 0 / 0 | PASS：目标/limit绑定、严格未知/重复/offset拒绝、确定性跨源顺序、最大100和社区/资料invalid query在空`Server{}`上先400全部通过 |
| 十万双源真实PG与并发 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestContentHistoryMergesOneHundredThousandRowsWithBoundedKeysetsIntegration$' -count=1 -v` | fixture1.342s；用例2.07s；包3.165s | 0 | PASS：50k manual+50k import；首/次各50且零重漏，并发新首项不漂入；尾部50+49精确终止，每个来源查询最多51项 |
| 深页生产SQL计划 | 同一用例对manual/import exact SQL执行`EXPLAIN (ANALYZE,BUFFERS)` | 0.160/0.427ms | 0 | PASS：分别命中`idx_content_revisions_history`和`idx_resource_import_snapshots_history`；目标历史表无Seq Scan，导入游标在联接前锚定snapshot复合索引 |
| generation132完整临时Schema | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 用例46.86s；包46.958s | 0 | PASS：完整Schema/trigger/index安装到132并清理；测试前后远端public generation不变（85），临时关系零残留，无远端重置/永久写入 |
| BUG-022权限交叉回归 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestProjectRevisionHistoriesHonorExactReviewScopeIntegration$' -count=1 -v` | 用例41.83s；包41.950s | 0 | PASS：全局/精确项目审核、编辑、submitter/self/cross及changelog 404矩阵在统一分页后保持 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | 全批22.29s；最慢queue4.640s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 158 tests2.753s；Build compile7.8s、TS10.8s | 0 / 0 / 0 / 0 | PASS：158/158、type/lint及58/58静态页全绿；共用历史组件只保留当前50项页和cursor栈 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | verifier2.30s；前端diff0.71s | 0 / 0 / 0；1（预期） | PASS：双仓差异检查无错误；449/449唯一ID，CLOSED=210、OPEN=239；`-allow-open`通过，严格模式仅因其余239项及最终严重度未收敛而按预期失败353项 |

内容历史的单页数据库、Go内存、网络和DOM工作现在均由100项硬上限约束；资料双源仍按一个稳定全局顺序完整可达。PERF-055差异存储、TEST-043内容/AI行为和其他历史族Finding保持独立开放。

## 2026-08-23：PERF-055 JSON差异存储与修订事务放大关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增条目/深度/路径/值摘要和batch SQL合同；`go test ./internal/httpapi -run '^TestStoredContentChange' -count=1` | 2.40s | 1（预期） | PASS：旧实现没有任何四维预算、摘要编码或batch常量；编译失败全部来自新行为缺失 |
| 单元GREEN与完整compare隔离 | 同命令加既有`TestDiffJSON` | 约1.10s | 0 | PASS：1000叶子稳定收口为128项+根摘要，64层/512B路径和512B值边界通过；大数组/字符串有hash摘要；既有完整nested/array diff路径仍为`/author/role,/summary,/tags` |
| 大JSON真实PG固定SQL与存储预算 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestContentChangeStorageStaysOneBatchAndBoundedForLargeJSONIntegration$' -count=1 -v` | 用例2.33s；包2.455s | 0 | PASS：1,325,912/1,325,915B snapshots；小/大均2 SQL；大diff精确128行/4,904B，array/root摘要各1且无JSON array原值 |
| 审核队列完整SQL | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestContentReviewQueueQueryIntegration$' -count=1 -v` | 用例0.26s；包0.374s | 0 | PASS：含blueprint/creator有序且8192字符上限摘要的完整UNION在真实PostgreSQL执行、JSON/facet解码成功 |
| Schema与远端边界 | 代码/迁移差异；沿用已验证generation132 | — | 0 | PASS：无表、列、索引、constraint、trigger或generation变化；真实PG只使用session temporary shadow tables，未重置或写入远端public |
| 后端受影响包 | `go test ./internal/httpapi ./internal/database -count=1` | HTTP1.341s；Database0.127s | 0 | PASS：修订创建、审核、比较、通知section及全部数据库Schema合同通过 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | 全批19.51s；最慢queue3.350s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿 |
| 前端全量回归 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；最近测试HTTPS Next16 production build | 158 tests2.723s；Build compile7.8s、TS10.8s | 0 / 0 / 0 / 0 | PASS：158/158、type/lint及58/58静态页全绿；本项无外部DTO或前端代码变化 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | verifier2.30s；diff0.90s | 0 / 0 / 0；1（预期） | PASS：双仓差异检查无错误；449/449唯一ID，CLOSED=211、OPEN=238；`-allow-open`通过，严格模式仅因其余238项及最终严重度未收敛而按预期失败352项 |

完整修订内容仍只由immutable snapshots承担；差异索引的SQL、行数、单值和展示成本均有硬界。LEGACY-019/OPS-019已由各自批次独立关闭；TEST-043及内容AI/审核其余行为覆盖保持开放。

## 2026-08-23：PERF-056 等级配置全表锁与逐用户角色N+1关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增generation133持久任务Schema、集合batch及请求无fan-out合同；`go test ./internal/database ./internal/httpapi -run 'TestLevel(Recalculation&#124;ConfigRequest)' -count=1` | 3.7s | 1（预期） | PASS：旧generation为132且无任务表；worker batch常量/SQL未定义，请求源码仍含全表`FOR UPDATE`和逐用户`SyncTrackRole`，失败只来自目标行为缺失 |
| 聚焦GREEN与源码边界 | 同一命令；`go test ./internal/database ./internal/httpapi -count=1` | 聚焦8.4s；包0.964/2.294s | 0 | PASS：500项硬批、单条等级/角色/进度SQL、配置只版本化并持久handoff、任务DDL/索引/状态不变量与runtime启动全部通过 |
| 十万用户真实PG、抢占与恢复 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestLevelRecalculationScaleIntegration$' -count=1 -v` | 用例95.49s；包95.605s | 0 | PASS：100k经验用户；两次PUT各6 SQL/267.7853ms与176.7371ms；旧job处理一批后被新版本supersede且旧token失效；最终200批=200 SQL/100k项；过期lease恢复、暂时失败回queued、耗尽预算dead |
| 等级/角色与计划正确性 | 同一十万规模用例 | 同上 | 0 | PASS：最终等级零错误；所有`level_track`绑定与level/track精确一致，manual同角色绑定保留；半深页`EXPLAIN(ANALYZE,BUFFERS)`命中`user_experience_pkey`且无目标表Seq Scan |
| generation133完整临时Schema | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 用例45.84s；包45.926s | 0 | PASS：完整Schema、任务CHECK与三个索引安装到133并清理；测试前后远端public generation保持85，临时关系零残留，无重置/永久写入 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 14.37/9.87/10.94/0.73s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建及模块图全绿；最慢antiabuse4.474s、queue4.160s |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 158 tests3.065s；Type11.63s；Lint约30s；Build29.31s | 0 / 0 / 0 / 0 | PASS：158/158，type/lint及58/58静态页全绿；`LevelConfig`接受服务端version/recalculation状态，原编辑字段不变 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | diff1.06/0.85s；verifier约0.99s | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449唯一ID，CLOSED=212、OPEN=237，High=113、Medium=174、Low=49、UNRESOLVED=113；allow-open通过，严格仅因其余237项和最终严重度未收敛而按预期失败351项 |

配置提交的数据库往返和锁集合现在不随用户量增长；全站重算由可抢占、可观察、可恢复的500项批任务完成。PERF-058～060及角色轨道编辑/删除的独立语义保持开放。

## 2026-08-23：PERF-058 站点更新日志深OFFSET与后台固定窗口关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增generation134索引、严格cursor、两类page SQL/handler及Next16调用方合同；Go聚焦与Node静态测试 | Go4.06s；Node0.803s | 1 / 1（预期） | PASS：旧generation133、缺少cursor类型/SQL，公开仍有OFFSET，后台仍固定100且前端没有cursor历史；失败仅来自目标行为缺失 |
| 聚焦GREEN与源码边界 | `go test ./internal/httpapi ./internal/database -run 'SiteChangelog&#124;Generation134' -count=1`；前端site-changelog测试、TypeScript及目标ESLint | Go8.51s；Node测试0.11s | 0 / 0 / 0 / 0 | PASS：严格scope/limit/date-ID cursor、page-before-LATERAL、无OFFSET/固定窗口、rows.Err/typed JSON与两端当前页导航合同通过 |
| 百万日志/翻译真实PG | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestSiteChangelogPagesStayIndexedAndReachableAtOneMillionRowsIntegration$' -count=1 -v` | fixture5.614s；用例6.13s；包7.225s | 0 | PASS：1M changelog+1M translation；公开/后台深页各1 SQL/50项并命中各自索引，无目标表Seq Scan；并发新首项不穿越旧cursor，两页零重复，后台旧101项后可达 |
| generation134完整临时Schema | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 用例45.36s；包45.447s | 0 | PASS：完整Schema与新增admin索引安装到134并清理；测试前后远端public generation保持85，临时关系零残留，无重置/永久写入 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 12.46/3.36/4.33/0.62s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建及模块图全绿；最慢serverprobe5.177s、queue4.515s |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 160 tests2.381s；Type2.29s；Lint23.06s；Build23.81s | 0 / 0 / 0 / 0 | PASS：160/160，type/lint及58/58静态页全绿；公开和后台日志只保存当前页与cursor历史 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | diff0.68/0.70s；verifier0.93/0.97s | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449唯一ID，CLOSED=213、OPEN=236，High=113、Medium=175、Low=49、UNRESOLVED=112；allow-open通过，严格仅因其余236项和最终严重度未收敛而按预期失败349项 |

公开和后台站点更新日志现在共享一个可完整遍历、查询工作量与页大小绑定的协议；PERF-059/060、ARCH-029及TEST-046仍独立开放。

## 2026-08-23：PERF-059 未解析引用全量子串、窗口计数与深OFFSET关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增generation135投影/索引/trigger、严格cursor/page SQL及Next16调用方合同；Go与Node聚焦测试 | Go2.44s；Node0.511s | 1 / 1（预期） | PASS：旧generation134且无投影/cursor；Handler仍有ILIKE、window count和OFFSET，前端仍有total/page/offset；失败只来自目标行为缺失 |
| 聚焦GREEN与源码边界 | `go test ./internal/database ./internal/httpapi -run 'UnresolvedReference' -count=1`；前端unresolved-reference测试、TypeScript及目标ESLint | Go8.39s；Node组合5.58s | 0 / 0 / 0 / 0 | PASS：literal prefix转义、query/type/status/limit scope、稳定tuple、10k预算、无旧SQL及前端当前页/cursor/取消代次合同通过；受影响后端包回归9.38s全绿 |
| 百万投影真实PG | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestUnresolvedReferenceProjectionPagesStayBoundedAtOneMillionRowsIntegration$' -count=1 -v` | fixture16.165s；用例17.00s；包18.071s | 0 | PASS：1M投影；深页一SQL命中page keyset索引；10k选择性前缀两SQL命中raw expression index；约990k宽前缀一条有界probe后拒绝；并发首项不穿旧cursor且跨页零重复 |
| generation135完整临时Schema与触发器 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 用例47.57s；包48.437s | 0 | PASS：完整Schema安装到135；general/resource投影insert、status、Catalog标签rename及cascade delete均同步；清理后远端public generation85与临时关系零变化 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 11.56/3.42/4.30/0.62s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建及模块图全绿；最慢queue4.443s、serverprobe4.272s |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 162 tests2.385s；Type2.20s；Lint22.92s；Build23.30s | 0 / 0 / 0 / 0 | PASS：162/162，type/lint及58/58静态页全绿；重复搜索、筛选重置、取消代次和cursor导航源码合同受守护 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | diff0.92/0.75s；verifier0.93/0.89s | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449唯一ID，CLOSED=214、OPEN=235，High=113、Medium=176、Low=49、UNRESOLVED=111；allow-open通过，严格仅因其余235项和最终严重度未收敛而按预期失败347项 |

未解析引用后台现在只为当前页读取一个窄投影；选择性搜索和过宽搜索都有可证明的固定上界。PERF-060、TEST-047及BUG-105/106/107保持独立开放。

## 2026-08-23：PERF-060 举报历史、审核队列与小黑屋深OFFSET关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增generation136索引、严格scope cursor、三类page SQL/handler及Next16调用方合同；Go聚焦与Node静态测试 | Go4.2s；Node0.7s | 1 / 1（预期） | PASS：旧generation135且无本人历史索引或治理cursor helper；三条Handler仍含OFFSET/offset响应，三个前端列表仍无cursor历史；失败只来自目标行为缺失 |
| 聚焦GREEN与源码边界 | `go test ./internal/database ./internal/httpapi -run 'Governance(Cursor&#124;Pagination&#124;Page&#124;List&#124;AdminReport)' -count=1`；前端`governance-pagination.test.mts`与TypeScript | Go8.8s；Node+Type3.1s | 0 / 0 | PASS：身份/status/端点/limit scope、升降序稳定tuple、limit+1、无OFFSET、rows.Err、typed响应及三处当前页导航合同通过 |
| 百万举报/封禁真实PG | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestGovernancePagesStayIndexedAndStableAtOneMillionRowsIntegration$' -count=1 -v` | fixture7.656s；用例8.43s；包8.568s | 0 | PASS：1M reports+1M bans；本人/审核/小黑屋深页各1 SQL/50项，分别命中reporter/queue/ban索引且目标表无Seq Scan；并发业务首项不穿旧cursor且跨页零重复 |
| generation136完整临时Schema | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration$' -count=1 -v` | 用例49.85s；包49.935s | 0 | PASS：完整Schema与新增reporter history索引安装到136并清理；测试前后远端public generation保持85，临时关系零残留，无重置/永久写入 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy` | Test15.53s；其余组合15.97s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建及模块图全绿；最慢queue4.519s、antiabuse4.128s |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 164 tests2.934s；Test+Type7.87s；Lint+Build50.61s | 0 / 0 / 0 / 0 | PASS：164/164，type/lint及58/58静态页全绿；公开小黑屋、审核与封禁管理的cursor历史及scope重置源码合同受守护 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | diff约0.8s；verifier0.96/0.98s | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449唯一ID，CLOSED=215、OPEN=234，High=113、Medium=177、Low=49、UNRESOLVED=110；allow-open通过，严格仅因其余234项和最终严重度未收敛而按预期失败345项 |

三类治理历史现在都能完整遍历，数据库工作量只与页大小相关，同时保留审核最旧优先和历史最新优先的原业务顺序。TEST-048及其他举报/封禁行为Finding保持独立开放。

## 2026-08-23：PERF-063 OSS大文件有界增量Worker哈希关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`oss-file-hash.test.mts`并要求独立增量hash模块 | 0.7s | 1（预期） | PASS：旧代码只有`oss-upload.ts`内的整文件`file.arrayBuffer()`实现，目标模块导入以`ERR_MODULE_NOT_FOUND`失败；失败只来自有界hash能力缺失 |
| 聚焦算法、内存与取消GREEN | `node --test app/_lib/oss-file-hash.test.mts`；TypeScript与目标ESLint | 5 tests约0.34s；Type3.68s | 0 / 0 / 0 | PASS：3个标准向量、20MiB+123B恰好6个slice、最大4MiB/单活动读取、全局单hash、排队/活动取消、2GiB读前拒绝、transfer list与无整文件物化合同全绿 |
| 生产Worker产物 | 测试HTTPS环境Next16 production build；定位`.next/static/media/oss-sha256-worker*.mjs`并执行`node --check`及自包含断言 | Build23.51s；产物0.62s | 0 / 0 | PASS：编译7.6s、Type10.9s、58/58静态页；5,501B MJS包含完整SHA-256与Worker入口、无顶层import且语法有效 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；production build | 169 tests3.316s；Type3.683s；Lint24.600s；Build23.508s | 0 / 0 / 0 / 0 | PASS：169/169；共享OSS调用方继续编译，Mod导出把既有AbortSignal和hash进度传入，新Worker资产被真实构建引用 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 9.525/4.766/5.481/0.802s | 0 / 0 / 0 / 0 | PASS：本项无后端协议或Schema变化，全部既有服务与generation136合同回归全绿 |
| Schema/API边界 | 核查generation、OSS预签名/上传DTO与共享hash调用点 | 同定向检查 | 0 | PASS：generation136不变，无DDL/回填/重置/远端public访问；上传HTTP合同不变，仅本地hash函数新增可选signal/progress/chunk选项 |
| 格式、差异与台账 | 双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449唯一ID，CLOSED=216、OPEN=233，High=113、Medium=177、Low=49、UNRESOLVED=110；allow-open通过，严格仅因其余233项和最终严重度未收敛而按预期失败344项 |

所有共享OSS入口现在不再复制完整文件；hash峰值由固定4MiB分块和全局单并发约束，CPU工作在浏览器Worker内执行且可取消。其他上传协议、失败恢复和界面Finding保持独立开放。

## 2026-08-23：PERF-064 资料布局轻量游标页与增量PATCH关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 新增Go增量patch合并测试与前端`mod-content-layout-incremental.test.mts`后，分别运行聚焦Go/Node测试 | Go2.293s；Node0.552s | 1 / 1（预期） | PASS：旧后端不存在`modContentLayoutPatch/applyModContentLayoutPatch`而编译失败；前端仍存在全量graph/layout loader和PUT，2项契约失败；失败只来自目标能力缺失 |
| 聚焦GREEN与20k边界 | `go test ./internal/httpapi -run 'TestTwentyThousandResourceLayoutUsesOneBoundedPageAndIncrementalMerge&#124;TestApplyModContentLayoutPatch&#124;TestNormalizeModContentLayoutPatch&#124;TestModContentLayoutRouteOnlyAcceptsIncrementalPatch' -count=1 -v`；前端两份资料分页测试、TypeScript与目标ESLint | Go1.151s；Node+Type3.704s；Lint6.936s | 0 / 0 / 0 | PASS：PATCH-only、1000变化上限、未加载资源保留、删分类修复；20k合并1.615ms，旧20k估算93,440,000B、新500摘要62,391B；客户端单页、空态及零全量快照合同通过 |
| 晋升创建与增量保存真实PG | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCreateAndArrangeAdvancementIntegration$' -count=1 -v -timeout 3m` | 用例41.69s；包41.802s | 0 | PASS：MaxConns=1 session临时Schema完成资源创建201、首次增量布局200及无变化再保存200；事务内review config/identity resolver均复用tx，无嵌套pool等待 |
| 100k生产Handler与1M计划 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestModContentSectionCursorSearchAndGraphIntegration$' -count=1 -v -timeout 3m` | 用例58.66s；包58.790s | 0 | PASS：100k card首/深/搜索615.95/155.75/258.76ms；layout500首69,115B/724.7ms、深20k为70,230B/1.899s，均远低于1MiB；1M keyset/GIN计划0.087/0.056ms且命中索引 |
| 集成隔离事故与永久防复发 | 首次旧晋升测试误连配置public后，以mod=130/user=75及精确slug/username/submitter/无引用谓词执行事务清理；随后测试强制`InstallEphemeralSchema`/`DropEphemeralSchema`和MaxConns=1 | 清理当轮；最终回归见上 | 0 | PASS：误建mod和user各精确删除1行，目标mod剩余0，其他历史fixture未触碰；后续两次规模/保存回归只使用可清理session临时Schema。此项明确记录事故，不声称远端public从未访问 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 组合23.745s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿；httpapi2.326s，最慢serverprobe3.956s、queue3.879s |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 171 tests3.201s；Test+Type7.551s；Lint约31.42s；Build约30.24s | 0 / 0 / 0 / 0 | PASS：171/171、type/lint及58/58静态页全绿；轻量摘要页、增量PATCH和晋升树非空渲染受源码合同守护 |
| Schema/API边界 | 核查generation、路由、DTO、旧loader及客户端方法残留 | 同定向检查 | 0 | PASS：generation136不变、无DDL/索引/回填；布局写只有PATCH，摘要无names/icon/detail/definition；生产调用方无`loadAllModContent*`或旧`updateModContentLayout`，内部20k仅用于服务端权威审核快照 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449唯一ID，CLOSED=217、OPEN=232，High=113、Medium=177、Low=49、UNRESOLVED=110；allow-open通过，严格仅因其余232项和最终严重度未收敛而按预期失败343项 |

公开晋升树与布局编辑现在都只保留当前500条轻量摘要，保存仅提交当前变化集；服务端权威合并和审核快照继续保证未装载资源不会丢失。PERF-071的页内算法、其他资料编辑体验与剩余Finding保持独立开放。

## 2026-08-23：PERF-066 公开日志元数据与有界正文块关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 新增`log_share_chunk_test.go`及前端`log-share-chunking.test.mts`后运行聚焦Go/Node | Go2.234s；Node0.524s | 1 / 1（预期） | PASS：后端因chunk scope/cursor/page/metadata类型全部不存在而编译失败；前端2/2因entry仍含text、无chunk API/游标单块状态而失败；失败只来自目标能力缺失 |
| 聚焦GREEN与协议边界 | `go test ./internal/httpapi -run 'LogShare' -count=1`；前端chunk测试、TypeScript及目标ESLint | Go包约1.10s；前端组合6.61s | 0 / 0 / 0 / 0 | PASS：cursor条目scope/尾随拒绝、32Ki字符、最坏转义JSON、metadata无text、chunk路由、1槽竞争及现有脱敏/ZIP/批次测试全绿；前端只保留一个可取消块 |
| 20MiB单条与100MiB ZIP真实PG | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestPublicLogShareResponsesStayBoundedForSingleAndZipScaleIntegration$' -count=1 -v -timeout 2m` | fixture925ms；用例1.57s；包2.616s | 0 | PASS：20MiB元数据427B/135.6ms；200×512KiB ZIP元数据27,074B/87.5ms；首/深/ZIP块196,755/196,688/196,755B及85.7/125.5/29.1ms；仅`pg_temp`且search_path锁定，无public写入 |
| 并发、限流、取消与下载 | 源码/单元合同及前端Abort回归 | 同聚焦测试 | 0 | PASS：生产正文/下载共享8路槽，满载503；反滥用开启时匿名metadata/chunk/download为30/60/5每分钟、认证翻倍；request取消传播PG；单文件单行读取，ZIP逐条Scan/write而非全量Go切片 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 组合29.757s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿；httpapi2.075s，最慢queue3.673s、serverprobe3.170s |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 173 tests3.032s；Test+Type7.577s；Lint约32.12s；Build约30.21s | 0 / 0 / 0 / 0 | PASS：173/173、type/lint及58/58静态页全绿；公开日志路由生产编译，元数据/块状态及Abort合同受守护 |
| Schema/API边界 | 核查generation、详情/正文/下载路由及旧全文状态残留 | 同定向检查 | 0 | PASS：generation136不变，无DDL/索引/回填；详情entries无text，正文仅新cursor块GET；前端无`maxRenderedCharacters`或entry全文；下载URL不变且服务端改为逐条流式 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：双仓无空白错误（仅既有LF→CRLF提示）；449/449唯一ID，CLOSED=218、OPEN=231，High=113、Medium=177、Low=49、UNRESOLVED=110；allow-open通过，严格仅因其余231项和最终严重度未收敛而按预期失败342项 |

普通公开查看现在不会传输或保存完整脱敏正文；单条和ZIP都只有元数据加当前32Ki字符块，完整下载是显式且限流/并发受控的流式动作。日志历史分页及其他日志Finding继续独立开放。

## 2026-08-23：PERF-067 审核队列稳定页与当前页批量装配关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 新增编辑员cursor解析测试与前端page helper/源码合同后运行聚焦Go/Node | 组合3.1s | 1 / 1（预期） | PASS：Go因page request/cursor符号不存在而编译失败；Node因分页模块不存在而ERR_MODULE_NOT_FOUND；失败只来自目标能力缺失 |
| 聚焦GREEN与静态合同 | 编辑员cursor Go tests；前端3 tests、TypeScript和目标ESLint；httpapi包 | Go1.055s/包1.411s；Node0.115s；全量测试2.992s | 0 | PASS：status/limit scope、跨scope/未知/重复拒绝、路径编码、ID去重、续页状态与AbortController合同全绿；前端全量176/176 |
| 编辑员205项与100k真实PG | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestProjectEditorApplicationListUsesConstantQueriesIntegration$' -count=1 -v` | 用例41.63s；包42.668s | 0 | PASS：完整临时Schema中205项按100/100/5零重漏，每页恒2 SQL；session temporary 100k深页Index Only Scan只读51行，执行0.076ms；Schema最终清理 |
| 服务器摘要/详情与100k真实PG | 服务器三状态cursor及summary/detail集成测试 | 用例1.47/2.51s；包5.022s | 0 | PASS：三状态各205完整遍历；100k主队列命中`idx_minecraft_servers_review_page`读取101行，三类关联仅以当前页ID数组集合聚合、整SQL0.507ms；100个1MiB正文列表恒1 SQL且不泄漏正文，单项详情恒5 SQL |
| 前后端全量门禁 | 后端全仓Test/Vet/Build/tidy；前端176 tests/Type/ESLint/测试HTTPS production build | 后端18.34/13.89/14.80/0.93s；前端2.992/11.16/30.01/约30.3s | 0 | PASS：全部业务、静态分析、模块图与58页生产编译通过；后端httpapi2.023s、最慢serverprobe4.866s；编辑员和服务器两套审核UI均保留受控页与请求取消 |
| 前端生成产物竞态复核 | 首次把独立`tsc --noEmit`与Next build并行，随后在build完成后串行重跑typecheck | 首次3.78s；串行11.16s | 1（预期识别）/ 0 | PASS：首次仅因build重建`.next/types`时两个生成文件短暂不存在而TS6053；Next自身TypeScript和最终独立typecheck均通过，不把共享产物竞态误报为源码失败 |
| Schema/API边界 | generation、索引、路由、DTO与旧无界第一方路径检索 | 同定向检查 | 0 | PASS：generation136不变、无DDL/回填；编辑员新增limit/cursor及hasMore/nextCursor，省略参数仍是有界首50；服务器对外协议不变，内部只聚合当前页ID；远端public未访问 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：449/449唯一ID，CLOSED=219、OPEN=230，High=113、Medium=177、Low=49、UNRESOLVED=110；allow-open通过，严格仅因其余230项和最终严重度未收敛而按预期失败341项 |

编辑员申请与服务器审核现在都只有稳定硬页界，数据库往返不随当前页项目数增长；服务器大正文和完整关联仍只在管理员展开单项时读取。PERF-072及其他审核可靠性Finding保持独立开放。

## 2026-08-23：PERF-068 资源选择器预选项N请求关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 新增批量请求规范/路由Go tests及`resource-picker-batch-hydration.test.mts`，运行聚焦Go/Node | 组合约3.2s | 1 / 1（预期） | PASS：Go因batch常量/规范化/handler不存在而编译失败；Node因批API及单请求调用不存在而2项失败；失败只来自目标能力缺失 |
| 聚焦GREEN与协议边界 | `go test ./internal/httpapi -run 'CatalogPresentationBatch&#124;CatalogResourcePresentationBatch' -count=1`；前端批补全tests、TypeScript与目标ESLint | Go1.222s；Node0.113s；前端组合8.08s | 0 / 0 / 0 / 0 | PASS：locale规范/去重/1000上限/路由、单批次、无6 Worker逐项`loadPage`及补全与浏览loader隔离合同全绿 |
| 1000项与混合三类真实PG | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCatalogResourcePresentationBatchHasConstantQueryBudgetIntegration$' -count=1 -v` | 请求728.397ms；用例39.77s；包39.892s | 0 | PASS：generation136临时Schema中1000资源/名称以1 SQL返回1000项、202,806B；资源+标签+项目以3 SQL返回三类；均低于2MiB，结束后Schema清理 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 11.558/3.477/4.059/0.365s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿；新增SQL同时经真实完整Schema编译执行 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 178 tests2.612s；Type2.488s；Lint25.120s；Build23.401s | 0 / 0 / 0 / 0 | PASS：178/178，type/lint及58页生产编译全绿；预选展示只保存当前批次结果且过时请求可取消 |
| Schema/API边界 | 核查generation、POST路由、请求/响应预算、旧浏览loader及远端隔离 | 同定向检查 | 0 | PASS：generation136不变、无DDL/索引/回填；新增精确批POST最多1000项/3 SQL/2MiB，旧资源/标签/项目浏览API不变；远端public未访问 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：449/449唯一ID，CLOSED=220、OPEN=229，High=113、Medium=177、Low=49、UNRESOLVED=110；allow-open通过，严格仅因其余229项和最终严重度未收敛而按预期失败340项 |

预选项展示补全现在与模糊浏览完全解耦：保存DTO已有名称时零请求，缺失事实时整个选择器只发一个有界批量请求。PERF-069～071及其他前端资源Finding保持独立开放。

## 2026-08-23：PERF-069 蓝图场景面级对象/矩阵复制与全量层切换关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`blueprint-scene-budget.test.mts`并导入层索引/联合预算模块 | 0.12s | 1（预期） | PASS：旧代码不存在`structureSceneBudget.mts`而ERR_MODULE_NOT_FOUND；旧源码仍含每面block数组、`Matrix4.clone()`和全实例层循环；失败只来自目标能力缺失 |
| 层桶、预算与源码GREEN | `node --test app/_lib/blueprint-scene-budget.test.mts`；TypeScript及目标ESLint | 4 tests0.144s；Type6.27s；Lint3.3s | 0 / 0 / 0 | PASS：层排序/offset/相邻slice、150万实例/128MiB/512 draw-call预算、600k规模及旧对象/矩阵路径零残留全部通过 |
| 600k精确Node规模 | 同一测试构造600k index、200层并调用生产`packInstanceLayers/selectPackedInstanceLayers` | pack14.6ms | 0 | PASS：持久层元数据2,401,604B；单层3k+上下各3k只触碰9k，小于总量1/50；稀疏六面3.6m被instance预算拒绝，600k单实例场景预算43,609,600B |
| 本机Chrome浏览器合成基准 | Chrome headless执行`scripts/blueprint-scene-browser-benchmark.html`，600k index/matrix与9k当前+相邻层写入 | build34.3ms；layer0.5ms | 0 | PASS：typed buffers45,986,404B、used JS heap46,844,856B、触碰9,000；这是工作站浏览器CPU/堆证据，不声明移动端FPS或真实GPU帧率 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 8.188/2.179/3.005/0.354s | 0 / 0 / 0 / 0 | PASS：本项无后端协议/Schema变化，全部服务和generation136合同回归全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 182 tests3.531s；Type7.241s；Lint23.604s；Build23.396s | 0 / 0 / 0 / 0 | PASS：182/182、type/lint及58页生产编译全绿；Three.js动态客户端chunk与新增.mts预算模块被真实Turbopack构建 |
| Schema/API边界 | 核查generation、后端DTO、renderer导出、旧矩阵/方块路径和远端隔离 | 同定向检查 | 0 | PASS：generation136不变，无DDL/索引/回填；后端render API不变，内部load result仅增加观测；远端public未访问，PERF-070取消链未冒充关闭 |
| 格式、差异与台账 | 双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：449/449唯一ID，CLOSED=221、OPEN=228，High=113、Medium=177、Low=49、UNRESOLVED=110；allow-open通过，严格仅因其余228项和最终严重度未收敛而按预期失败339项 |

蓝图场景现在不再按模型面复制方块对象或Matrix对象；层交互的实例工作量与当前层及两个可选相邻层绑定，场景字节、实例和draw-call三类预算共同失败关闭。PERF-070继续处理加载取消与中间资源释放。

## 2026-08-23：PERF-070 被替换或卸载的蓝图加载任务端到端取消关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`blueprint-load-cancellation.test.mts`并导入取消/cache/gate模块 | 0.7s | 1（预期） | PASS：旧代码不存在`structureLoadControl.mts`而ERR_MODULE_NOT_FOUND；旧Canvas/source/renderer/model合同也没有signal链，失败只来自目标能力缺失 |
| gate与共享请求GREEN | `node --test app/_lib/blueprint-load-cancellation.test.mts`；独立TypeScript；目标ESLint | 4 tests0.126s；Type2.641s；Lint3.989s | 0 / 0 / 0 | PASS：构建最大活动数1，排队Abort零启动；同键两消费者只调用一次loader，取消一个不abort底层，最后一个离开会abort/驱逐且retry创建新请求；自定义abort reason也规范为AbortError |
| 端到端源码合同 | 同一测试读取Canvas、viewer、renderer、scene、model与HTTP source生产源码 | 0.006s（测试内） | 0 | PASS：source/load/fetch、内部controller、新load/dispose、parse/model/scene、JSON/OBJ/MTL均贯穿signal；cleanup显式abort；AbortError不进fallback，scene以allSettled后统一释放 |
| 不可取消阶段与资源释放 | `loadTexture`晚到路径及scene/model dispose定向检查 | 同聚焦测试/Type/Lint | 0 | PASS：TextureLoader promise在调用链Abort后若晚到立即`texture.dispose()`；普通组、geometry/material/texture由统一dispose释放；HTTP source不创建object URL；同步解析受单任务gate及现有16M/600k/联合场景预算约束 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | 10.042/4.786/5.930/0.696s | 0 / 0 / 0 / 0 | PASS：本项无后端路由、DTO、Schema或模块图变化，generation136及全服务回归通过 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 186 tests3.248s；Type2.641s；Lint24.374s；Build23.833s | 0 / 0 / 0 / 0 | PASS：186/186，独立type/lint及58页生产编译全绿；动态Three客户端链和新增.mts控制模块通过真实Turbopack构建 |
| Schema/API边界 | 核查generation、后端render/asset合同、内部可选signal、远端与object URL | 同定向检查 | 0 | PASS：generation136不变，无DDL/索引/回填；后端HTTP合同不变，浏览器内部方法只新增可选signal；无object URL；远端public未访问 |
| 格式、差异与台账 | 双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 约1.0s/项 | 0 / 0 / 1（预期）/ 0 | PASS：449/449唯一ID，CLOSED=222、OPEN=227，High=113、Medium=177、Low=49、UNRESOLVED=110；allow-open通过，严格仅因其余227项和最终严重度未收敛而按预期失败338项 |

换源或卸载现在会同时终止蓝图下载、共享资产读取、模型解析链和场景构建提交；不可由JavaScript抢占的同步/TextureLoader阶段最多只有一个场景任务，并在边界检查或晚到回调释放资源。PERF-071及其他资料布局前端复杂度Finding继续独立开放。

## 2026-08-23：PERF-071 资料布局分类乘资源与组内平方查找关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`mod-content-layout-render-index.test.mts`并导入生产索引模块 | 0.472s | 1（预期） | PASS：旧代码不存在`mod-content-layout-render-index.mts`而ERR_MODULE_NOT_FOUND；旧editor仍有每分类resources.filter/sort和每chip entries.findIndex，失败只来自目标能力缺失 |
| 聚焦GREEN与源码合同 | `node --test app/_lib/mod-content-layout-render-index.test.mts`；独立TypeScript；目标ESLint | 3 tests0.143s；Type2.915s；Lint4.219s | 0 / 0 / 0 | PASS：section/root分组、ordinal顺序、共享similar cluster、position与resource ID Map正确；editor按resources/root useMemo，旧render-time filter/findIndex零残留 |
| 20k单遍复杂度证据 | Proxy包装20,000项、1,000分类并调用生产helper | 生产build19.779ms（聚焦测试） | 0 | PASS：源数组数字索引精确读取20,000次、生成1,000 section和20,000 resource ID；分类数量不乘源数组访问，position lookup不扫描组 |
| 10k/20k九轮本机基准 | `node scripts/mod-content-layout-render-index-benchmark.mts` | 总0.675s | 0 | PASS：中位/最大为10k×1000分类5.195/7.704ms、20k×1000分类7.140/10.499ms、20k单分类4.682/13.945ms；只证明当前工作站纯派生量级，不声明20k DOM或跨设备FPS |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | 4.564/4.526/4.220/0.694s | 0 / 0 / 0 / 0 | PASS：本项无后端路由、DTO、Schema或模块图变化，generation136及PERF-064 layout/graph/PATCH合同回归通过 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 189 tests3.921s；Type2.915s；Lint25.287s；Build24.210s | 0 / 0 / 0 / 0 | PASS：189/189，独立type/lint及58页生产编译全绿；.mts纯索引和编辑器真实Turbopack client bundle通过 |
| Schema/API与规模边界 | 核查generation、layout DTO/cursor/PATCH、当前页与远端隔离 | 同定向检查 | 0 | PASS：generation136不变、无DDL/索引/回填；后端API零变化；实际编辑器仍只持有PERF-064的最多500项轻量页并增量提交；远端public未访问 |
| 格式、差异与台账 | 双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁 | 0 / 0 / 1（预期）/ 0 | PASS：449/449唯一ID，CLOSED=223、OPEN=226，High=113、Medium=177、Low=49、UNRESOLVED=110；allow-open通过，严格仅因其余226项和最终严重度未收敛而按预期失败337项 |

布局资源现在只在resources修订变化时分组、排序、聚类并建立位置索引；分类/输入/选择重渲染以Map读取替代C×R和R²扫描。PERF-064的500项轻量页仍是React/DOM硬边界，不能因20k纯函数基准而恢复全量客户端聚合。

## 2026-08-23：PERF-072 作者认领待审队列无分页与逐条附件查询关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 新增`creator_claim_pagination_test.go`及`creator-claim-pagination.test.mts`后运行目标Go/Node | Go2.4s；Node0.109s | 1 / 1（预期） | PASS：Go仅因cursor类型/解析/编码不存在而编译失败；Node仅因分页path模块不存在而ERR_MODULE_NOT_FOUND，失败均来自目标能力缺失 |
| cursor与前端GREEN | `go test ./internal/httpapi -run 'TestParseCreatorClaimPageRequest' -count=1`；`node --test app/_lib/creator-claim-pagination.test.mts`；TypeScript/目标ESLint | Go1.113s；Node0.120s；Type通过；Lint通过 | 0 / 0 / 0 / 0 | PASS：默认50/最大100、unknown/repeat/offset/非法limit/畸形与跨limit cursor拒绝；前端path编码、替换页、Previous/Next、Abort与generation合同全绿 |
| 205项恒定SQL与附件批装配 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCreatorClaimPageUsesConstantQueriesIntegration$' -count=1 -v` | 用例39.49s；包39.613s | 0 | PASS：临时完整Schema按100/100/5遍历205项、零重复遗漏；每个非空页精确2 SQL；5个附件全部正确投影且无逐claim查询 |
| 100k深页真实计划 | 同一集成测试`EXPLAIN (ANALYZE,BUFFERS)`生产tuple谓词 | 0.068ms执行 | 0 | PASS：Index Only Scan命中`idx_creator_claim_page_scale`，只返回51行，无Seq Scan；临时数据与Schema测试后清理 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 12.048/3.503/4.084/0.364s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿；分页/批附件SQL经真实完整Schema编译执行 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 191 tests3.617s；Type2.617s；Lint25.118s；Build23.541s | 0 / 0 / 0 / 0 | PASS：191/191、type/lint及58页生产编译全绿；管理面只持有当前50项页且旧请求可取消 |
| Schema/API边界 | 核查generation、既有索引、GET响应/旧调用方、审核及附件路由、远端隔离 | 同定向检查 | 0 | PASS：generation136不变，无DDL/索引/回填；精确复用queue/attachment索引；旧无参数调用仍得有界首屏，PATCH/presign不变；远端public未访问 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 约1.0s/项 | 0 / 0 / 1（预期）/ 0 | PASS：449/449唯一ID，CLOSED=224、OPEN=225，High=113、Medium=177、Low=49、UNRESOLVED=110；allow-open通过，严格仅因其余225项和最终严重度未收敛而按预期失败336项 |

作者认领审核现在以稳定硬页限制数据库、JSON、React状态和DOM；附件数据库往返固定为每页一次集合查询，不再随claim数量增长。其他审核提交一致性与队列Finding保持独立开放。

## 2026-08-23：DB-001 / LEGACY-002 generation内旧Outbox Schema过渡关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`nats_outbox_schema_consolidation_test.go`并运行目标数据库测试 | 3.00s | 1（预期） | PASS：旧代码精确缺少基础定义中的10个最终列/约束/三索引，仍包含ALTER、UPDATE、DROP与created-only旧索引；失败只来自Finding所述双形状 |
| 源码GREEN | `go test ./internal/database -run '^TestNATSOutboxSchemaIsDefinedOnceInFinalShape$' -count=1` | 3.24s | 0 | PASS：20列/命名五态约束/三最终索引均在权威基础定义，基础设施块零Outbox ALTER/UPDATE/DROP，旧pending索引零残留 |
| 真实空库最终形状 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestNATSOutboxFinalShapeInstallsDirectlyIntegration$' -count=1 -v` | 用例38.13s；包39.016s | 0 | PASS：generation136临时完整Schema一次安装20列、正确nullable/default、五态约束及三索引；默认值精确，unknown status拒绝；清理后public generation前后不变 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 17.561/3.539/4.465/0.361s | 0 / 0 / 0 / 0 | PASS：全仓Outbox/JetStream/HTTP/Schema回归、静态分析、构建及模块图全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 191 tests3.607s；Type2.396s；Lint26.677s；Build23.542s | 0 / 0 / 0 / 0 | PASS：191/191、type/lint及58页生产编译全绿；本项无HTTP或前端协议变化 |
| Schema/API/隔离 | 对比最终列/default/constraint/index、generation、运行时生产者/dispatcher及public代次 | 同定向检查 | 0 | PASS：最终Schema语义与generation136不变，无回填/重置/API迁移；远端public只读代次且前后相同，无永久写入 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 约1.0s/项 | 0 / 0 / 1（预期）/ 0 | PASS：449/449唯一ID，CLOSED=226、OPEN=223，High=113、Medium=178、Low=49、UNRESOLVED=109；allow-open通过，严格仅因其余223项和最终严重度未收敛而按预期失败333项 |

空库不再经历旧Outbox形状和同代自迁移；最终数据库事实、队列状态机与索引合同保持不变。DB-001与LEGACY-002是同一原子清理，分别保留证据但不重复计为两个生产行为修复。

## 2026-08-23：DB-004 四个UNIQUE重复目录索引关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`duplicate_catalog_index_schema_test.go`并运行目标database测试 | 2.977s | 1（预期） | PASS：旧generation136精确报告四个被点名的显式索引仍存在；UNIQUE定义同时存在，失败只来自Finding所述重复结构 |
| 源码GREEN | `go test ./internal/database -run '^TestCatalogSchemaDoesNotDuplicateUniqueConstraintIndexes$' -count=1` | 0.888s | 0 | PASS：四个UNIQUE声明保留，四个冗余显式名称从生产DDL消失；仓库无名称调用依赖 |
| 真实完整Schema与catalog ownership | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestUniqueConstraintsOwnCatalogLookupIndexesIntegration$' -count=1 -v` | 用例40.73s；包41.595s | 0 | PASS：generation137临时完整Schema中四组精确键各只有1个索引，全部unique且由UNIQUE constraint拥有；四个删除名称均不存在；清理后public generation前后不变 |
| 代表查询计划 | 同一集成测试对四类目录读取执行`EXPLAIN (format text)`并禁用Seq Scan | 同上 | 0 | PASS：四类均为Index/Bitmap Index Scan且无Seq Scan；revision/registry与recipe直接命中constraint index，resource exact与mod tree由保留的history/FK索引覆盖 |
| database包回归 | `go test ./internal/database -count=1` | 0.110s | 0 | PASS：Schema generation、安装顺序与全部database单测全绿 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | 20.700/11.365/12.083/0.797s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿；所有current-generation守卫已协调到137 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 191 tests3.597s；Type4.580s；Lint约30.0s；Build29.048s | 0 / 0 / 0 / 0 | PASS：191/191、type/lint及58页生产编译全绿；本项无HTTP或前端合同变化 |
| Schema/API/隔离 | 核查generation、约束、其余索引、调用方、public代次与回滚 | 同定向检查 | 0 | PASS：generation136→137，只删四个重复非唯一索引；无表/列/constraint/数据/API变化；开发库需重置；远端public仅只读代次且前后相同，无永久写入 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 约1.1s/项 | 0 / 0 / 1（预期）/ 0 | PASS：449/449唯一ID，CLOSED=227、OPEN=222，High=113、Medium=178、Low=49、UNRESOLVED=109；allow-open通过，严格仅因其余222项、109项最终严重度及severity total尚未收敛而按预期失败332项 |

四组目录写入现在各只维护UNIQUE约束自有的一份同序B-tree；不同键序、FK与partial索引继续承担各自查询责任。DB-008的独立第五项仍保持OPEN。

## 2026-08-23：DB-006 镜像内容跨项目唯一冲突与上传后补偿关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`project_automation_mirror_identity_schema_test.go`并运行目标database测试 | 2.997s；包0.867s | 1（预期） | PASS：旧Schema仍含`unique(source_type,file_sha256,byte_size)`并精确失败；provider identity约束存在，失败只来自跨项目内容身份混淆 |
| Schema与源码GREEN | 目标database测试；`TestProjectAutomationMirrorStreamsThroughBoundedGlobalSlots` | 0.102/1.091s | 0 / 0 | PASS：旧content unique消失、provider identity保留；preflight只接受ErrNoRows，登记失败必经detached cleanup并具有durable fallback reason |
| 真实双项目同内容与补偿生命周期 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestProjectMirrorIdentityAndUploadCompensationIntegration$' -count=1 -v` | 用例46.06s；包46.176s | 0 | PASS：generation138完整临时Schema允许同source、相同SHA-256/4096 bytes的两个不同file ID跨项目共存；provider约束1/content约束0 |
| OSS错误与取消边界 | 同一真实PG+fake OSS集成测试 | 同上 | 0 | PASS：取消父context后仍精确DELETE未登记object；已存在`oss_files`记录时零DELETE；模拟403写入pending outbox且reason为`project-automation-registration-failed` |
| database/httpapi包 | `go test ./internal/database -count=1`；`go test ./internal/httpapi -count=1` | 0.117/1.316s | 0 / 0 | PASS：Schema、自动更新、OSS deletion与HTTP回归全绿 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | 16.342/11.457/12.292/0.775s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿；current-generation守卫全部协调到138 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 191 tests3.705s；Type4.271s；Lint约30.0s；Build28.638s | 0 / 0 / 0 / 0 | PASS：191/191、type/lint及58页生产编译全绿；本项无HTTP/前端合同变化 |
| Schema/API/隔离 | 核查generation、constraints/hash indexes、异常语义、public代次与回滚 | 同定向检查 | 0 | PASS：generation137→138，只删错误content unique；无表/列/数据/API变化；开发库需重置；public仅只读代次且前后相同，无永久写入 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 约1.2s/项 | 0 / 0 / 1（预期）/ 0 | PASS：449/449唯一ID，CLOSED=228、OPEN=221，High=113、Medium=178、Low=49、UNRESOLVED=109；allow-open通过，严格仅因其余221项、109项最终严重度及severity total尚未收敛而按预期失败331项 |

自动镜像不再把“字节相同”误判为“供应商文件身份相同”；外部上传与数据库登记之间的失败窗口现在有已注册保护、取消隔离直删和可靠异步删除三层可观测补偿。

## 2026-08-23：DB-005 不可达project-file public route与软删分裂关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`project_file_route_schema_test.go`并运行目标database测试 | 3.009s；包0.857s | 1（预期） | PASS：旧Schema精确报告register/remove函数、两trigger、伪canonical path与project_file route insert共六类残留 |
| Schema与真实路由GREEN | 目标database测试；`TestProjectFileDownloadRemainsProjectScopedAndStatusAware` | 0.872/1.097s | 0 / 0 | PASS：六类generic route DDL零残留；Server只注册项目scope POST下载，handler仍强制project ownership、active file和active+clean OSS |
| 完整Schema identity/lifecycle | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestProjectFileIdentityStaysScopedWithoutPublicRouteIntegration$' -count=1 -v` | 用例40.18s；包40.263s | 0 | PASS：generation139中active文件ID registry=1、project_file routes=0、scope读取=1；soft delete后routes=0、scope读取=0；public generation前后不变 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | 16.554/11.446/12.198/0.817s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿；current-generation守卫全部协调到139 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 191 tests3.665s；Type4.243s；Lint约30.0s；Build28.939s | 0 / 0 / 0 / 0 | PASS：191/191、type/lint及58页生产编译全绿；真实project file API无变化 |
| Schema/API/隔离与严重度 | 核查public ID registry、路由注册、soft delete、generation与public代次 | 同定向检查 | 0 | PASS：generation138→139，仅删2函数+2触发器；无表/列/数据/API迁移；开发库需重置；远端public仅只读代次且前后相同；严重度复核为Medium |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 约1.2s/项 | 0 / 0 / 1（预期）/ 0 | PASS：449/449唯一ID，CLOSED=229、OPEN=220，High=113、Medium=179、Low=49、UNRESOLVED=108；allow-open通过，严格仅因其余220项、108项最终严重度及severity total尚未收敛而按预期失败329项 |

项目文件的稳定public ID仍服务项目scope API、事件和审计，但不再冒充一个可由全局路由解析的内容对象；soft delete与真实下载可达性现在由同一`project_files.status`权威事实决定。

## 2026-08-23：DB-007 / DEAD-006 无调用方chat presence表关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`chat_presence_schema_test.go`并运行目标database测试 | 3.002s；包0.880s | 1（预期） | PASS：旧完整安装语句仍包含`user_chat_presence`，失败只来自审计确认的Schema-only第二事实源 |
| Schema GREEN与生产零调用 | 目标database测试；`rg user_chat_presence internal`排除测试 | 0.878s；rg零结果 | 0 / 1（零命中预期） | PASS：权威DDL和生产Go均无表名；没有保留DAO、双写或兼容包装 |
| 真实完整Schema relation absent | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run '^TestChatPresencePostgreSQLTableIsAbsentIntegration$' -count=1 -v` | 用例39.35s；包40.234s | 0 | PASS：generation140安装完成且`to_regclass`为null；临时Schema清理后public generation前后不变 |
| 唯一运行时authority回归 | `go test ./internal/querycache -count=1` | 0.184s | 0 | PASS：Redis key、本地有界fallback、TTL/过期与presence metrics测试全绿 |
| 后端全仓 / Vet / Build / tidy | `go test ./...`；`go vet ./...`；`go build ./...`；`go mod tidy` | 21.011/11.556/12.136/0.716s | 0 / 0 / 0 / 0 | PASS：消息、querycache、Schema、HTTP与全仓回归，静态分析、构建和模块图全绿；current-generation守卫协调到140 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 191 tests3.621s；Type4.182s；Lint约30.0s；Build29.318s | 0 / 0 / 0 / 0 | PASS：191/191、type/lint及58页生产编译全绿；presence API与客户端无变化 |
| Schema/API/隔离与严重度 | 核查表依赖、Redis/local authority、generation、public代次和回滚 | 同定向检查 | 0 | PASS：generation139→140只删零调用关系；无数据/API迁移；开发库需重置；public仅只读且不变；两个Finding均复核Low |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 约1.2s/项 | 0 / 0 / 1（预期）/ 0 | PASS：449/449唯一ID，CLOSED=231、OPEN=218，High=113、Medium=179、Low=51、UNRESOLVED=106；allow-open通过，严格仅因其余218项、106项最终严重度及severity total尚未收敛而按预期失败325项 |

聊天presence现在只有实际运行的Redis TTL事实与受界本地降级，不再由空PostgreSQL关系暗示不存在的一致性责任。DB-007与DEAD-006共享一份删除与证据。

## 2026-08-23：DB-008 log share重复owner FK索引关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效源码RED | 更新`TestFeatureUpdateSchemaContainsStableRelationsAndIndexes`拒绝`idx_log_shares_owner_fk`后运行目标database测试 | 3.2s | 1（预期） | PASS：旧Schema同时包含owner partial复合索引和被点名单列索引，失败只来自重复索引仍存在 |
| 自动生成器真实RED | 删除显式索引但未修正`foreignKeyIndexStatement`时运行完整Schema质量测试 | 用例34.70s | 1（预期） | PASS：catalog精确发现2个owner前缀，第二个为自动重建的`idx_fk_log_shares_log_shares_owner_user_id_fkey_*`，复现测试/安装器固化缺陷 |
| 源码与完整Schema GREEN | `go test ./internal/database -run TestFeatureUpdateSchemaContainsStableRelationsAndIndexes -count=1`；`MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run TestEveryForeignKeyHasLeadingIndex -count=1 -v` | 0.855s；最终用例39.25s/包40.123s | 0 / 0 | PASS：generation141完整临时Schema全部FK仍有有效前缀；log share owner前缀精确1个，名称`idx_log_shares_owner_created`、谓词`(owner_user_id IS NOT NULL)`；public generation前后不变 |
| 查询计划与规则边界 | 同一完整Schema测试核对`pg_index/pg_attribute/pg_get_expr`并执行禁用Seq Scan的`EXPLAIN` | 同上 | 0 | PASS：`owner_user_id=1`命中保留partial索引且无Seq Scan；生成器/测试只接受单列FK的精确`IS NOT NULL`，任意其他partial谓词仍不合格 |
| database/httpapi包 | `go test ./internal/database ./internal/httpapi -count=1` | 0.117/2.236s | 0 / 0 | PASS：Schema、log share、OSS生命周期与HTTP回归全绿 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy` | 最终8.849/4.437/5.326/0.757s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿；current-generation守卫全部协调到141 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 191 tests3.478s；Type4.055s；Lint30.011s；Build30.009s | 0 / 0 / 0 / 0 | PASS：191/191、type/lint及58页生产编译全绿；本项无HTTP或前端合同变化 |
| Schema/API/隔离与严重度 | 核查保留/删除索引、generation、调用方、public代次和回滚 | 同定向检查 | 0 | PASS：generation140→141只删一个重复非唯一索引并修正索引发现；无表/列/FK/数据/API迁移；开发库需重置；远端public仅只读且不变；严重度复核Low |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 约1.2s/项 | 0 / 0 / 1（预期）/ 0 | PASS：449/449唯一ID，CLOSED=232、OPEN=217，High=113、Medium=179、Low=52、UNRESOLVED=105；allow-open通过，严格仅因其余217项、105项最终严重度及severity total尚未收敛而按预期失败323项 |

nullable owner FK现在由已有history partial前缀完整覆盖；空库安装器不再把安全等价索引错误判为缺失并重新制造重复B-tree。

## 2026-08-23：ARCH-002 超大后台与Handler模块边界关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 双端有效RED | 新增`TestProductionHandlerModulesStayReviewable`与`admin-module-size.test.mts`并运行目标测试 | Go1.072s；Node0.114s | 1 / 1（预期） | PASS：Go精确列出8个1533..2536行handler；前端仅因6215行console超1200失败，失败均来自Finding所述边界缺失 |
| 声明提取与尺寸GREEN | 目标Go/Node尺寸测试；top-level声明依赖清单；实际行数清点 | Go1.106s；Node0.118s | 0 / 0 | PASS：所有handler≤1500，最大`admin_handlers.go`1462；console793，五个新领域/shared模块870..1371，全部admin模块≤2000 |
| 后端源码合同迁移 | `go test ./internal/httpapi -count=1` | 2.313s | 0 | PASS：旧文件绑定测试改为读取对应拆分模块；placeholder/namespace/catalyst/capability/template/import/quota/关系/comment/profile断言均保持并通过 |
| 前端行为与源码合同 | 全部`app/_lib/*.test.mts` | 192 tests2.808s | 0 | PASS：新增尺寸门及既有admin log cursor、设置、审核、权限、OSS、通知等192/192通过 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy` | 17.058/13.760/14.915/0.904s | 0 / 0 / 0 / 0 | PASS：函数体/SQL/package符号机械移动后全仓行为、静态分析、构建与模块图全绿 |
| 前端Type/Lint/Build | TypeScript；ESLint；测试HTTPS环境Next16 production build | 最终Type10.948s；Lint32.158s；Build32.158s | 0 / 0 / 0 | PASS：拆分模块类型/import/export闭合、Lint零错误、58页生产构建全绿；并行build清理`.next/types`导致的一次TS6053已在build完成后单独重跑通过，不计产品失败 |
| Schema/API/兼容 | 核查generation、数据库访问、路由/DTO、公开组件入口与回滚 | 同定向检查 | 0 | PASS：无DDL/数据库操作，generation141不变；无HTTP/DTO/交互迁移；`AdminConsole`路径和Go方法符号不变 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 约1.2s/项 | 0 / 0 / 1（预期）/ 0 | PASS：449/449唯一ID，CLOSED=233、OPEN=216，High=113、Medium=179、Low=52、UNRESOLVED=105；allow-open通过，严格仅因其余216项、105项最终严重度及severity total尚未收敛而按预期失败322项 |

后台与Handler现在由可执行行数门防止重新聚合，行为正确性继续由原有领域源码合同、包测试、TypeScript和生产构建承担。

## 2026-08-23：ARCH-005 用户统计streak/totals读取吞错关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`TestUserStatisticsReadsDoNotSilentlyReturnPartialFacts`并运行目标HTTP测试 | 8.3s；用例1.065s | 1（预期） | PASS：旧实现没有在activeStreaks前检查terminal rows error，失败精确来自Finding吞错路径 |
| 故障注入与GREEN | `go test ./internal/httpapi -run 'TestUserStatisticsRead' -count=1` | 用例1.065s；包8.117s | 0 | PASS：一条日期后terminal error返回0/0/error且rows关闭；totals任意DB错误传播，ErrNoRows为空成功，两个真实时间保持 |
| 相关包回归 | `go test ./internal/httpapi ./internal/userstats -count=1` | 1.330/0.105s | 0 / 0 | PASS：统计HTTP、范围/streak算法、事实重建和用户统计服务回归全绿 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy` | 16.570/12.216/13.008/1.121s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿；无Schema变化 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 192 tests3.509s；Type4.848s；Lint约30.0s；Build23.445s | 0 / 0 / 0 / 0 | PASS：192/192、type/lint及58页生产编译全绿；成功DTO与前端合同不变 |
| Schema/API/边界 | 核查generation、ErrNoRows、调用方与ARCH-006独立范围 | 同定向检查 | 0 | PASS：generation141不变，无DDL/数据/前端迁移；只把非NoRows故障从200/null或偏短改为5xx；成长投影可靠性在本项验证时保持独立，随后由ARCH-006关闭 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 同轮复核 | 0 / 0 / 1（预期）/ 0 | PASS：双仓diff无空白错误；台账449/449，CLOSED=234、OPEN=215；严格模式仅因剩余开放项与105项待定严重度按预期失败（321 issues），`-allow-open`通过 |

用户统计读取不再把流中断或totals存储故障伪装成合法短streak/null时间；只有权威NoRows事实仍表示尚无累计记录。

## 2026-08-23：ARCH-006 durable成长投影原子确认与重放关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | 新增`TestDurableOutboxProjectionFailureRollsBackAndReplaysIntegration`，对旧drain注入成长处理器错误 | 用例40.15s；包41.058s | 1（预期） | PASS：旧实现先commit raw并删除Outbox，随后仅日志记录`injected progression failure`；`DrainDurable`仍返回count=1/err=nil，失败精确来自Finding永久不可重放窗口 |
| durable rollback/replay GREEN | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/activity -run '^TestDurableOutboxProjectionFailureRollsBackAndReplaysIntegration$' -count=1 -v` | 用例38.07s；包38.947s | 0 | PASS：失败后count=0/error、Outbox/raw/probe=1/0/0且无commit hook；释放既有退避后重放为0/1/1，事务处理器两次尝试而commit hook只一次 |
| 真实成长服务事务 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/progression -run '^TestActivityProgressionParticipatesInCallerTransactionIntegration$' -count=1 -v` | 用例37.57s；包38.450s | 0 | PASS：真实active task读取与进度写入使用调用者tx；外层rollback后进度0，重新处理并commit后精确为1 |
| 迭代边界核查 | 首轮GREEN释放前立即领取；将可重建site投影置入临时函数事务的尝试 | 40.35s / 45.56s | 1 / 1（实现迭代） | PASS：确认Outbox既有`available_at`退避必须显式释放后测试重放；确认会话临时函数解析限制后保持审计点名的非可重建成长投影为原子边界，site日活继续由既有raw校准 |
| 相关包回归 | `go test ./internal/activity ./internal/progression ./internal/app -count=1` | 0.992/1.266/0.127s | 0 / 0 / 0 | PASS：监控器有界队列、成长匹配/角色来源、运行时装配全绿；runtime直接传入事务处理器对象 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy` | 17.473/9.780/10.587/0.612s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿；无Schema generation变化 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 192 tests3.727s；Type3.549s；Lint24.359s；Build23.310s | 0 / 0 / 0 / 0 | PASS：192/192、type/lint及58页生产编译全绿；HTTP/DTO与成长任务界面无需迁移 |
| Schema/API/边界 | 核查generation、事务顺序、可重建site投影、view lossy分类及远端隔离 | 同定向检查 | 0 | PASS：generation141不变，无DDL/数据迁移；raw+成长/任务/奖励+Outbox确认同事务；site仍可校准、view仍显式best-effort；只使用会话临时Schema |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 同轮复核 | 0 / 0 / 1（预期）/ 0 | PASS：双仓diff无空白错误（仅既有LF→CRLF提示）；台账449/449，CLOSED=235、OPEN=214；严格模式仅因剩余开放项与105项待定严重度按预期失败（320 issues），`-allow-open`通过 |

durable活动的成功确认现在同时代表不可重建成长副作用已提交；投影故障保留同一Outbox身份并沿既有退避自动重放，不再以日志替代可靠性。

## 2026-08-23：ARCH-007 反滥用关键副作用错误可见性关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效源码RED | 新增`TestSecuritySideEffectsDoNotSilentlyDiscardFailures`后运行目标antiabuse测试 | 用例0.00s；包0.967s；墙钟3.430s | 1（预期） | PASS：旧实现精确因`applyAutomaticRestriction`的`_, _ = s.db.Exec`失败；后续同一门还覆盖risk writer和bot rule吞错 |
| 规则/聚合故障注入 | 目标运行源码合同、bot rule Scan/terminal stream、daily aggregate retain/retry/discard及Crawler分类测试 | 包0.966s；墙钟3.021s | 0 | PASS：Scan或terminal error均返回nil/error且rows关闭；成功规则完整；daily失败保留map，重试成功才删除，最终discard按2+3精确计5 |
| 自动限制真实PG原子性 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/antiabuse -run '^TestAutomaticRestrictionFailsClosedAndRollsBackIntegration$' -count=1 -v` | 用例37.21s；包37.305s | 0 | PASS：user state约束拒绝第二写时RecordDecision返回error，restriction/state=0/0且restrictionFailed=1；解除故障后同一请求为1/1 |
| 相关包回归 | `go test ./internal/antiabuse ./internal/httpapi ./internal/app -count=1` | 3.116/1.355/0.126s；墙钟6.819s | 0 / 0 / 0 | PASS：success queue故障/排空指标、Crawler DNS/预算、HTTP稳定503源码合同和Server Shutdown全绿 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy` | 9.547/5.896/6.493/0.638s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿；无Schema变化 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 192 tests3.705s；Type3.596s；Lint24.773s；Build23.530s | 0 / 0 / 0 / 0 | PASS：192/192、type/lint及58页生产编译全绿；新增基础设施计数字段向后兼容 |
| Schema/API/边界 | 核查generation、失败关闭/保守降级、PERF-004/SEC-008/BUG-008范围与远端隔离 | 同定向检查 | 0 | PASS：generation141不变，无DDL/业务数据写入；仅自动状态失败改稳定503，bot规则失败为SuspiciousBot；队列容量、限制组合、challenge各保持独立既有合同 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 同轮复核 | 0 / 0 / 1（预期）/ 0 | PASS：双仓diff无空白错误（仅既有LF→CRLF提示）；台账449/449，CLOSED=236、OPEN=213；严格模式仅因剩余开放项与105项待定严重度按预期失败（319 issues），`-allow-open`通过 |

反滥用现在把“安全状态”和“派生遥测”分开处理：前者原子失败关闭，后者不反转已提交业务，但每种失败都有返回、保留重试、分类指标或结构化日志，不再静默弱化防线。

## 2026-08-23：ARCH-008 活动删除进度与审计终态可恢复关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 新增后端`TestActivityCleanupFinalizationIsRecoverable`与前端`activity-cleanup-audit.test.mts`后分别运行目标测试 | 后端用例0.00s/包1.069s/墙钟8.207s；前端117ms/墙钟0.521s | 1 / 1（预期） | PASS：旧后端精确命中被忽略的final update且无repair；旧前端缺少completed/audit_pending响应联合类型和待修复提示 |
| 源码合同GREEN | 运行后端目标源码测试及前端目标Node测试 | 后端包1.065s/墙钟8.351s；前端113ms/墙钟0.479s | 0 / 0 | PASS：手工/自动终态均检查错误；每批原子进度、202稳定code、周期repair及双语调用方分支存在 |
| 真实PG双故障恢复 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestActivityCleanupProgressAndAuditRepairStayConsistentIntegration$' -count=1 -v` | 用例40.13s；包41.187s | 0 | PASS：第二批进度约束失败后run=running/count=1/events=2；解除故障续跑为running/3/0；completed约束失败保持running/3/0，repair后completed/3/0 |
| 相关包回归 | `go test ./internal/httpapi ./internal/app -count=1` | 1.336/0.134s；墙钟6.795s | 0 / 0 | PASS：HTTP清理、Worker生命周期与应用装配回归全绿 |
| 后端全仓 / Vet / Build / tidy | `go mod tidy`；`go test ./... -count=1`；`go vet ./...`；`go build ./...` | 0.458/8.160/3.371/4.047s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿；无Schema变化 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 193 tests3.417s；Type11.652s；Lint23.336s；Build23.421s | 0 / 0 / 0 / 0 | PASS：193/193、type/lint及58页生产编译全绿；202待审计响应有明确双语状态 |
| Schema/API/边界 | 核查generation、批次原子性、repair输入、MAP-003/PERF-005范围与远端隔离 | 同定向检查 | 0 | PASS：generation141不变，无DDL/业务数据迁移；repair仅用既有持久run事实；MAP-003保持OPEN，PERF-005不重复关闭；远端public未访问 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 同轮复核 | 0 / 0 / 1（预期）/ 0 | PASS：双仓diff无空白错误（仅既有LF→CRLF提示）；台账449/449，CLOSED=237、OPEN=212；严格模式仅因剩余开放项与105项待定严重度按预期失败（318 issues），`-allow-open`通过 |

不可逆活动删除现在逐批拥有原子审计进度；终态存储短暂失败会准确暴露给管理员并由持久running记录自动收口，不再依赖日志或人工猜测实际删除数。

## 2026-08-23：ARCH-010 目录编辑详情必需读取错误传播关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效源码RED | 新增`TestCatalogEditorDetailsDoNotTurnDatabaseFailuresIntoEmptyFacts`后运行目标测试 | 用例0.00s；包1.060s；墙钟8.172s | 1（预期） | PASS：旧实现精确因recipe type详情的`_ = s.db.QueryRow`失败，后续同一门覆盖localization/slot/catalyst忽略及review/active吞错 |
| 源码合同GREEN | `go test ./internal/httpapi -run '^TestCatalogEditorDetailsDoNotTurnDatabaseFailuresIntoEmptyFacts$' -count=1 -v` | 用例0.00s；包0.121s；墙钟2.518s | 0 | PASS：三类审计点名详情无忽略读取，review仅ErrNoRows默认approved，active返回error，全部失败路径记录query context并返回5xx |
| 真实PG必需读取故障矩阵 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestCatalogRecipeTypeDetailFailsOnEveryRequiredReadIntegration$' -count=1 -v` | 用例40.46s；包40.574s | 0 | PASS：健康详情200；template count/localizations/catalysts/review status四表分别不可用时均500并输出精确日志；无change request仍approved/nil；每个临时表完整恢复 |
| 相关包回归 | `go test ./internal/httpapi ./internal/app -count=1` | 2.231/0.132s；墙钟12.310s | 0 / 0 | PASS：目录详情、公开catalog及应用装配回归全绿 |
| 后端全仓 / Vet / Build / tidy | `go mod tidy`；`go test ./... -count=1`；`go vet ./...`；`go build ./...` | 0.338/7.088/3.040/3.649s | 0 / 0 / 0 / 0 | PASS：最终代码全仓测试、静态分析、构建与模块图全绿；无Schema变化 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 193 tests3.474s；Type2.804s；Lint24.539s；Build22.949s | 0 / 0 / 0 / 0 | PASS：193/193、type/lint及58页生产编译全绿；成功DTO无调用方迁移 |
| Schema/API/边界 | 核查generation、ErrNoRows、ARCH-009/PERF-008范围与远端隔离 | 同定向检查 | 0 | PASS：generation141不变，无DDL/数据迁移；只有明确缺失为404/approved，故障为500；结构解码与规模项未重复关闭；远端public未访问 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 同轮复核 | 0 / 0 / 1（预期）/ 0 | PASS：双仓diff无空白错误（仅既有LF→CRLF提示）；台账449/449，CLOSED=238、OPEN=211；严格模式仅因剩余开放项与105项待定严重度按预期失败（317 issues），`-allow-open`通过 |

目录编辑器现在只会在获得完整、可信的详情快照后返回200；数据库局部故障会同时成为调用方可见的5xx和带查询上下文的运维日志，不再悄悄变成零值或approved。

## 2026-08-23：ARCH-013 自动化调度、必需状态与列表错误可见性关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效源码RED | 新增`TestProjectAutomationAndSimpleRowsDoNotHideDatabaseFailures`，并将填充爬虫点名函数加入同一闭集后分别运行目标测试 | 项目RED包1.072s/墙钟8.154s；填充扩展RED包1.071s/墙钟8.179s | 1 / 1（预期） | PASS：旧项目scheduler精确命中忽略commit/Scan，旧共享helper命中伪空/跳行；填充路径精确命中lease、budget、存在性、翻译/草稿/来源绑定忽略写读 |
| row stream与源码合同GREEN | `go test ./internal/httpapi -run '^(TestProjectAutomationAndSimpleRowsDoNotHideDatabaseFailures|TestSimpleRowsValueAndCursorFailuresAreObservable)$' -count=1 -v` | 包0.126s；墙钟8.1s | 0 | PASS：Query/Values/terminal cursor任一失败均返回nil/error并关闭rows；两scheduler及全部点名必需seed状态无忽略数据库结果 |
| 真实PG双调度与必需读取故障 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestProjectAutomationSchedulerAndListsExposeDatabaseFailuresIntegration$' -count=1 -v` | 首轮夹具校正42.44s；最终用例39.35s/包40.415s | 1（夹具）/ 0 | PASS：显式upsert临时seed singleton后，项目/seed deferred commit失败均error且run=0，解除后各精确1；概览500，scheduler、当日计数、项目存在与翻译预算故障全返回error |
| 相关包回归 | `go test ./internal/httpapi ./internal/database ./internal/config ./internal/security -count=1` | 包1.426/0.197/0.096/0.245s；墙钟5.2s | 0 / 0 / 0 / 0 | PASS：HTTP、完整Schema、运行配置和身份边界全绿；seed run/candidate列表继续检查Scan和terminal cursor |
| 后端全仓 / Vet / Build / tidy | `go mod tidy`；`go test ./... -count=1`；`go vet ./...`；`go build ./...` | 0.635/13.081/9.938/10.611s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿；无Schema变化 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 193 tests3.539s（墙钟4.207s）；Type4.086s；Lint30.004s；Build29.758s | 0 / 0 / 0 / 0 | PASS：193/193、type/lint及58页生产编译全绿；成功HTTP DTO及前端调用无需迁移 |
| Schema/API/边界 | 核查generation、调度正常nil分支、AI逐locale降级、OPS-005/006范围与远端隔离 | 同定向检查 | 0 | PASS：generation141不变，无DDL/数据迁移；禁用/未来/无锁/无任务仍正常；AI调用失败可审计降级但审计写失败传播；租约续期及全供应商失败终态未重复关闭；远端public未访问 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 后端diff1.076s；前端diff0.894s；严格1.184s；开放期1.000s | 0 / 0 / 1（预期）/ 0 | PASS：双仓diff无空白错误；台账449/449，CLOSED=239、OPEN=210，High=113/Medium=179/Low=52/UNRESOLVED=105；严格模式仅因210个剩余开放项与105项待定严重度按预期失败（316 issues），`-allow-open`通过 |

两个自动化系统现在只在完整读取、写入和事务提交后报告成功；存储故障会成为Worker日志、HTTP 5xx或内部错误，不再被管理界面和预算/去重逻辑解释为空集合或正常空闲。

## 2026-08-23：BUG-060 / ARCH-014 任务配置启用门与奖励原子性关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效compile RED | 新增`TestTaskConfigurationRejectsInvalidConditionsAndRewards`与合法控制后运行目标progression测试 | 墙钟1.5s | 1（预期） | PASS：旧实现没有共享严格decoder，测试精确因`decodeTaskConfiguration`未定义而无法编译；审计点名condition/rewards边界尚不存在 |
| 严格配置与HTTP读取GREEN | 运行十类invalid/valid decoder、`TestTaskConfigurationMapsRejectCorruptPersistedDefinitions`及既有任务保存/duplicate currency目标测试 | progression包0.873s；HTTP包1.066s；联合墙钟8.6s | 0 / 0 | PASS：condition类型/target/action/object/metric与reward类型/空/负经验/零货币/未规范code全失败；合法配置完整保留；HTTP map不产生空事实 |
| 真实PG损坏/奖励矩阵 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestInvalidActiveTaskConfigurationCannotAdvanceOrRewardIntegration$' -count=1 -v` | 用例41.86s；包41.975s | 0 | PASS：错误target、空奖励、missing currency均返回error且各自progress/completed/rewarded=0/0/0；user/admin损坏读取500并记录task；合法控制=1/1/1且experience=1 |
| 真实PG外层事务回归 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/progression -run '^TestActivityProgressionParticipatesInCallerTransactionIntegration$' -count=1 -v` | 用例37.47s；包37.550s | 0 | PASS：夹具改用合法非空1经验奖励；调用者rollback后progress=0，重新处理并commit后progress=1，既有ARCH-006原子边界保持 |
| 相关包回归 | `go test ./internal/progression ./internal/httpapi ./internal/activity ./internal/app -count=1` | 0.908/1.481/0.154/0.129s；墙钟7.3s | 0 / 0 / 0 / 0 | PASS：成长、HTTP、活动Outbox与运行装配全绿；invalid active config沿durable投影返回而非确认 |
| 后端全仓 / Vet / Build / tidy | `go mod tidy`；`go test ./... -count=1`；`go vet ./...`；`go build ./...` | 0.617/9.262/5.926/6.525s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿；无Schema变化 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 193 tests3.582s（墙钟4.273s）；Type4.117s；Lint30.016s；Build29.915s | 0 / 0 / 0 / 0 | PASS：193/193、type/lint及58页生产编译全绿；合法任务DTO和前端调用不变 |
| Schema/API/边界 | 核查generation、JSONB/货币启用门、事务/Outbox、TEST-031范围与远端隔离 | 同定向检查 | 0 | PASS：generation141不变，无DDL/回填；配置/货币验证早于delta，奖励流水与标记同事务，失败保留Outbox；并发/角色/等级/缓存广域矩阵未重复关闭；远端public未访问 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 后端diff1.049s；前端diff0.867s；严格1.034s；开放期1.151s | 0 / 0 / 1（预期）/ 0 | PASS：双仓diff无空白错误；台账449/449，CLOSED=241、OPEN=208，High=113/Medium=179/Low=52/UNRESOLVED=105；严格模式仅因208个剩余开放项与105项待定严重度按预期失败（314 issues），`-allow-open`通过 |

活跃任务只有在condition、非空奖励和全部货币通过同一启用门后才参与进度；任何配置损坏都会保留可重试活动事实并显式失败，不会再把零奖励写成“已领取”。

## 2026-08-23：ARCH-015 基础设施持久指标拒绝伪零关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效源码RED | 新增`TestInfrastructureMetricsCannotReportDatabaseFailuresAsZero`后运行目标HTTP测试 | 用例0.00s；包1.060s；墙钟8.4s | 1（预期） | PASS：旧实现精确命中`_ = s.db.QueryRow...Scan`，证明六项持久指标错误仍被无条件零值200吞掉 |
| 源码合同GREEN | 同一目标测试 | 用例0.00s；包1.057s；墙钟8.3s | 0 | PASS：handler调用typed helper、检查error、写结构化失败并返回500，旧忽略表达式零残留 |
| 真实PG非零/故障矩阵 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^(TestInfrastructureMetricsCannotReportDatabaseFailuresAsZero|TestInfrastructureDurableMetricsFailInsteadOfReportingZeroIntegration)$' -count=1 -v` | 首轮夹具包1.364s；最终用例0.36s/包1.403s（墙钟8.8s） | 1（夹具）/ 0 | PASS：临时表健康控制pending/dead/OSS=1/1/1/2/3且oldest非零；最初rename发现search_path只读回退后改临时列故障，最终helper error/零内部值、HTTP500、日志SQLSTATE 42703 |
| 相关包回归 | `go test ./internal/httpapi ./internal/queue ./internal/app -count=1` | 1.427/3.346/0.133s；与初次tidy联合墙钟9.4s | 0 / 0 / 0 | PASS：指标端点、Outbox状态机与应用装配全绿 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy` | 8.968/5.728/6.141/0.738s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿；无Schema变化 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build | 193 tests3.577s（墙钟4.253s）；Type4.130s；Lint30.004s；Build29.899s | 0 / 0 / 0 / 0 | PASS：193/193、type/lint及58页生产编译全绿；成功指标DTO不变 |
| Schema/API/边界 | 核查generation、整体500选择、临时/public隔离与PERF-038范围 | 同定向检查 | 0 | PASS：generation141不变，无DDL/回填；不新增unknown/partial DTO，失败不伪零；只修改临时列且public只读回退未写；死信游标项未重复关闭 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 后端diff0.998s；前端diff0.833s；严格1.020s；开放期1.480s | 0 / 0 / 1（预期）/ 0 | PASS：双仓diff无空白错误；台账449/449，CLOSED=242、OPEN=207，High=113/Medium=179/Low=52/UNRESOLVED=105；严格模式仅因207个剩余开放项与105项待定严重度按预期失败（313 issues），`-allow-open`通过 |

基础设施端点现在要么返回一份完整的持久可靠性快照，要么明确失败并记录根因；数据库故障不再被管理员误读为零积压和零死信。

## 2026-08-23：ARCH-016 Minecraft版本配置故障拒绝默认伪装关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效compile RED | 新增真实PG测试并按目标`(config,error)`调用loader后运行目标HTTP包 | 墙钟2.4s | 1（预期） | PASS：旧单返回值loader使五处目标调用全部assignment mismatch，证明消费域无法观察配置错误 |
| 真实PG缺行/损坏/合法矩阵 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestMinecraftVersionConfigurationDistinguishesMissingFromBrokenIntegration$' -count=1 -v` | 首轮参数夹具包0.506s；最终用例0.70s/包1.793s（墙钟9.3s） | 1（夹具）/ 0 | PASS：显式`::text`修正测试参数后，缺行返回默认；错误shape、81字符normalize和缺value列均zero/error；public/admin均500并记录上下文；合法1.20.1/Forge精确保留 |
| 调用方闭集与相关包 | `rg 'loadMinecraftVersionConfig\('`核对全部生产调用；`go test ./internal/httpapi ./internal/app -count=1` | HTTP1.846s/App1.176s；墙钟7.8s | 0 / 0 | PASS：public/update/sync、recipe list/edit/detail、mod-content fallback和automation共八个生产调用全部处理error；运行装配全绿 |
| 后端全仓 / Vet / Build / tidy | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy` | 9.259/5.937/6.457/0.628s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建与模块图全绿；无Schema变化 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；TypeScript；ESLint；测试HTTPS环境Next16 production build；build后重跑TypeScript | 193 tests3.604s（墙钟4.248s）；Type首轮4.077s/最终10.795s；Lint30.009s；Build约30.009s | 0 / 1（门禁并发夹具）→0 / 0 / 0 | PASS：并行type与Next生成`.next/types`发生短暂缺文件竞态；Build自身Type及58页成功，生成目录稳定后独立tsc为0；193/193与lint全绿，无源码错误 |
| Schema/API/边界 | 核查generation、missing/error边界、同步状态码、BUG-062/ARCH-017/OPS-012/PERF-039范围与远端隔离 | 同定向检查 | 0 | PASS：generation141不变，无DDL/回填；只有ErrNoRows默认，内部配置错误500/外部抓取502；默认内容、artifact快照、租约、缓存未重复关闭；远端public未访问 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 后端diff0.991s；前端diff0.828s；严格0.956s；开放期0.963s | 0 / 0 / 1（预期）/ 0 | PASS：双仓diff无空白错误；台账449/449，CLOSED=243、OPEN=206，High=113/Medium=179/Low=52/UNRESOLVED=105；严格模式仅因206个剩余开放项与105项待定严重度按预期失败（312 issues），`-allow-open`通过 |

版本配置现在明确区分“尚未配置”和“配置不可用”：前者保留既有启动默认，后者贯穿全部消费路径失败并留下诊断，不再向用户或管理员伪造一份完整兼容目录。

## 2026-08-23：ARCH-017 同步绑定loader artifact权威关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效Schema/compile RED | generation合同先要求142与新表；真实PG测试先调用`minecraftLoaderArtifactSnapshot`、原子保存和同步读取入口后运行database/httpapi目标 | 墙钟3.8s | 1（预期） | PASS：旧generation141精确失败，五个新authority符号不存在；证明旧系统没有可持久tuple、原子发布或当前目录绑定读取边界 |
| 语义hash、完整性与有界来源GREEN | `go test ./internal/httpapi -run 'TestMinecraftVersionCatalogHash|TestValidateMinecraftLoaderArtifact|TestSynchronizeMRPackLoaderArtifacts|TestFavoriteExportMapsLoaderArtifactAuthorityFailures' -count=10`及Fabric/Maven选择目标 | 包1.182s；墙钟8.6s | 0 | PASS：展示顺序/名称/common/非MRPack变化hash稳定，Forge tuple变化hash必变；缺tuple拒绝；Fabric+Forge+NeoForge共4个tuple只抓3个目录，无Forge artifact的1.21.1从selector移除且status可见；422/500 code精确 |
| 真实PG原子/provenance/离线/陈旧矩阵 | 官方Windows portable PostgreSQL 18.6隔离端口55432；`MCMODS_TEST_DATABASE_URL=postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable go test ./internal/httpapi -run '^TestMinecraftLoaderArtifactSnapshotsAreCatalogBoundAndOfflineReadable$' -count=1` | 最终包0.213s；墙钟约3.1s | 0 | PASS：1.20.1/Forge精确47.4.10、source URL和observed_at；强制上游client失败时外呼0；临时表缺`source_url`使设置+delete+COPY整体rollback；手工改目录后旧tuple返回typed missing且快照行0；只使用会话临时表 |
| generation142完整Schema与生产preview | `MCMODS_RUN_DB_INTEGRATION=1`及隔离DB配置运行`TestFavoriteExportDependencyGraphTraversesCyclesAndReportsLimitsIntegration` | 用例11.752s；墙钟14.5s | 0 | PASS：完整空库式generation142安装成功；既有1000节点/4000边MRPack依赖矩阵通过，真实preview只消费按当前默认目录hash写入的持久NeoForge tuple |
| HTTP/Schema相关包与死路径闭集 | `go test ./internal/httpapi -count=1`；`go test ./internal/database -count=1`；`rg`核对请求时resolver/cache零生产引用 | HTTP2.351s；database0.114s | 0 / 0 | PASS：Schema合同、API映射、同步解析和全部HTTP回归绿；`resolveMRPackLoaderVersion`、逐选择download、私有cache符号从生产代码删除，只有源码禁止断言保留其名字 |
| 后端全仓 / Vet / Build / tidy | 最终`go test ./...`、`go vet ./...`、`go build ./...`、`go mod tidy` | 测试包最长3.383s；Vet/Build并行墙钟约10.2s；tidy/diff联合1.5s | 0 / 0 / 0 / 0 | PASS：全仓、静态分析、构建与模块图全绿；generation142当前合同一致 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后独立TypeScript | 193 tests3.260s；Lint约28.5s；Build约26.4s；Type2.5s | 0 / 0 / 0 / 0 | PASS：193/193、lint/type及58页生产编译全绿；既有Minecraft loader versions字段自动消费同步收紧集合，无前端双协议 |
| Schema/API/边界 | 核查generation142、主键/约束、事务/COPY、状态码、开发重置、远端隔离及相邻Finding | 同定向检查 | 0 | PASS：新表仅保存当前语义compatibility hash绑定tuple；无回填/双读/live fallback，开发库须重置；远端public generation85未访问；BUG-063/065、OPS-012、MAP-009、TEST-033未重复关闭 |
| 格式、差异与台账 | gofmt；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | tidy+后端diff1.5s；前端diff1.5s；verifier1.3s | 0 / 0 / 1（预期）/ 0 | PASS：双仓diff无空白错误；台账449/449，CLOSED=244、OPEN=205，High=113/Medium=179/Low=52/UNRESOLVED=105；严格模式仅因205个剩余开放项与105项待定严重度按预期失败（311 issues），`-allow-open`通过 |

MRPack导出现在只接受与当前同步兼容集合绑定的持久loader artifact；同步后的上游故障不再影响预检或创建，旧目录tuple也不能穿越配置变化。

## 2026-08-23：ARCH-018 MRPack收藏导出错误可见性关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效compile RED | 新增源码/真实PG测试并让测试读取`worker.processPending(ctx)`返回值后运行目标HTTP测试 | 墙钟2.6s | 1（预期） | PASS：旧`processPending`无返回值导致`(no value) used as value`，证明周期调用者无法观察pending/Worker故障 |
| 源码闭集GREEN | `go test ./internal/httpapi -run '^TestFavoriteExportQueriesAndWorkerDoNotHideFailures$' -count=1` | 包1.057s；墙钟8.5s | 0 | PASS：详情/下载没有忽略JSON/rows/Scan；pending/expire/exhaust/report/fail/ready均返回或记录错误并检查CAS，旧吞错模式零残留 |
| 真实PG读取/Worker故障矩阵 | 官方portable PostgreSQL 18.6隔离端口55432；`MCMODS_TEST_DATABASE_URL=postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable go test ./internal/httpapi -run '^TestFavoriteExportReportAndWorkerFailuresAreObservableIntegration$' -count=1 -v` | 包1.318s；墙钟9.0s | 0 | PASS：dependency JSON标量详情500；report整数Scan失败写`REPORT_LOAD_FAILED`并清lease；retry trigger拒绝返回联合错误且状态不伪改；错误lease失败；pending查询缺列明确返回error；仅用临时表 |
| HTTP及后端全量门禁 | `go test ./internal/httpapi -count=1`；`go test ./... -count=1`；`go vet ./...`；`go build ./...` | HTTP最终1.504s；全仓包最长queue3.350s；Vet/Build退出0 | 0 / 0 / 0 / 0 | PASS：任务详情/下载、MRPack Worker、全仓测试、静态分析与构建全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`（dot reporter）；ESLint；测试HTTPS环境Next16 production build；build后独立TypeScript | 193 tests3.297s；Lint23.122s；Build20.571s；Type2.300s | 0 / 0 / 0 / 0 | PASS：193/193、lint/type及58页生产编译全绿；首次build命令误用旧环境变量，在Next config预检以1退出且未编译，改用必需`NEXT_PUBLIC_API_BASE_URL`后通过，不是源码回归 |
| Schema/API/边界 | 核查generation142、no-row/error映射、lease/CAS、临时/public隔离及相邻Finding | 同定向检查 | 0 | PASS：无DDL/回填/协议迁移；404/410只用于明确业务事实，内部错误500/Worker error；状态写回绑定lease且要求一行；远端public未访问；OPS-013、BUG-069、DEAD-011、TEST-034未重复关闭 |
| 格式、差异与台账 | gofmt；`go mod tidy`；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁输出 | 0 / 0 / 0 / 1（预期）/ 0 | PASS：模块图和双仓diff无错误；台账449/449，CLOSED=245、OPEN=204，High=113/Medium=179/Low=52/UNRESOLVED=105；严格模式仅因204个剩余开放项与105项待定严重度按预期失败（310 issues），`-allow-open`通过 |

MRPack收藏导出的数据库、JSON、row stream、状态竞争和通知故障现在都能到达HTTP 500、任务失败事实或Worker结构化日志；损坏报告不会被部分打包，未成功持久化的状态不会被宣称成功。

## 2026-08-23：ARCH-019 蓝图派生读取错误语义关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效compile RED | 新增源码/真实PG测试并按`(rows,error)`调用`blueprintAssetRevisions`后运行目标 | 墙钟2.295s | 1（预期） | PASS：旧helper只有单返回值，编译精确报`assignment mismatch: 2 variables but ... returns 1 value`，证明asset错误无传播通道 |
| 源码与真实PG GREEN | `MCMODS_TEST_DATABASE_URL=postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable go test ./internal/httpapi -run 'TestBlueprintDerivedRead' -count=1 -v` | 用例0.13s；包1.227s；墙钟8.628s | 0 | PASS：明确无cover 204/无render data 404；cover/render缺列500；resolver错误、标量properties及asset缺关系均error/500；四个stage日志含SQLSTATE；只使用临时表 |
| Blueprint广域回归与夹具校正 | 同一隔离PG运行`go test ./internal/httpapi -run Blueprint -count=1` | 初轮包2.151s失败；最终包3.059s/墙钟10.605s | 1（夹具）→0 | PASS：初轮暴露既有Outbox临时`blueprint_jobs`缺当前预算查询需要的`status`；只补`default 'queued'`后全部Blueprint单元/集成测试通过，生产代码未绕过预算 |
| HTTP与后端全量门禁 | `go test ./internal/httpapi -count=1`；`go test ./... -count=1`；`go vet ./...`；`go build ./...` | HTTP1.423s；全仓HTTP1.917s/最长queue3.766s；Vet10.451s；Build11.516s | 0 / 0 / 0 / 0 | PASS：详情、材料、asset、cover/render及全仓测试、静态分析与构建全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`（dot reporter）；ESLint；测试HTTPS环境Next16 production build；build后独立TypeScript | 193 tests3.949s；Lint约30.01s；Build编译9.4s/内置Type14.0s并生成58页；独立Type2.289s | 0 / 0 / 0 / 0 | PASS：193/193、lint/type及58页production build全绿；健康蓝图DTO和调用方不变 |
| Schema/API/边界 | 核查generation142、no-row/empty/error顺序、日志、调用方闭集、临时/public隔离与邻项 | 同定向检查 | 0 | PASS：无DDL/回填；只有明确no-row或成功空render key产生204/404，内部故障500；asset helper唯一调用方已迁移；远端public未访问；权限/状态机/租约/补偿/预算/TEST-036未重复关闭 |
| 格式、差异与台账 | gofmt；`go mod tidy`；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁输出 | 0 / 0 / 0 / 1（预期）/ 0 | PASS：模块图和双仓diff无错误；台账449/449，CLOSED=246、OPEN=203，High=113/Medium=180/Low=52/UNRESOLVED=104；严格模式仅因203个剩余开放项与104项待定严重度按预期失败（308 issues），`-allow-open`通过 |

蓝图详情和派生资源端点现在能区分“尚未生成/不存在”与“读取失败”：前者保持既有204/404，后者在任何部分DTO写出前500并留下可定位stage，数据库事故不再表现为原始方块名、空版本或无封面。

## 2026-08-23：ARCH-020 OSS读取、运维写与可靠清理失败语义关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效compile RED | 新增源码/真实PG测试并按error返回与nullable-file deletion job调用旧helper | 墙钟2.294s | 1（预期） | PASS：旧`findExistingOSSFileByHashExact`、`reusableBlueprintByHash`、`findCompletedOSSUpload`返回值不足，且缺少运维写helper与job FileID，精确证明吞错/无晚登记保护接口 |
| 源码与真实PG GREEN | `MCMODS_TEST_DATABASE_URL=postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable go test ./internal/httpapi -run 'TestOSSReadsWritesAndCleanupHaveObservableFailureContracts&#124;TestOSSReuseMetricsAndCleanupFailuresAreObservableIntegration' -count=1` | 最终包1.267s；墙钟8.7s | 0 | PASS：健康与no-row语义保留；缺列错误传播；三类运维写各计1；cleanup重复一次、取消后仍入队；晚登记保护和故障计数均通过，仅使用临时表 |
| OSS广域回归 | 同一隔离PG运行`go test ./internal/httpapi -run OSS -count=1` | 包1.324s；墙钟4.7s | 0 | PASS：OSS文件、额度、multipart、删除Outbox、rehome、管理面及其真实PG测试全绿；PERF-044原子额度合同未回退 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 最终全仓29.473s；Vet3.359s；Build3.714s | 0 / 0 / 0 | PASS：读取、日志/统计、补偿清理及全仓测试、静态分析与构建全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后独立TypeScript | 193 tests2.947s；Lint22.802s；Build23.170s；Type1.970s | 0 / 0 / 0 / 0 | PASS：193/193、lint/type及58页production build全绿；健康OSS成功DTO和第一方调用不变，metrics仅加字段 |
| Schema/API/边界 | 核查generation142、no-row/error、独立context、Outbox lease/唯一键/晚登记、指标、临时/public隔离及邻项 | 同定向检查 | 0 | PASS：无DDL/回填；安全读取失败关闭，best-effort写失败可见；请求期零直接删除；远端public未访问；PERF-044、BUG-079/080、OPS-018、TEST-037未重复关闭 |
| 格式、差异与台账 | gofmt；`go mod tidy`；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁输出 | 0 / 0 / 0 / 1（预期）/ 0 | PASS：模块图和双仓diff无错误；台账449/449，CLOSED=247、OPEN=202，High=113/Medium=181/Low=52/UNRESOLVED=103；严格模式仅因202个剩余开放项与103项待定严重度按预期失败（306 issues），`-allow-open`通过 |

OSS数据库事故不再被当作空目录或可复用对象缺失；允许降级的运维统计有明确计数，所有请求期对象补偿先形成可靠任务并在真正删除前重新确认未登记。

## 2026-08-23：ARCH-021 生成OSS对象登记失败可靠补偿关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效runtime/source RED | 先加入fake OSS与真实PG补偿测试，运行`go test ./internal/httpapi -run 'TestGeneratedOSSObjectRegistration' -count=1` | 包2.587s；墙钟10.0s | 1（预期） | PASS：旧writer明确含`client.DeleteObject`；provider PUT后登记trigger失败产生0条Outbox任务，直接复现数据库不可见孤儿风险 |
| 定向GREEN | `MCMODS_TEST_DATABASE_URL=postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable go test ./internal/httpapi -run 'TestGeneratedOSSObjectRegistration' -count=1` | 包1.176s；墙钟8.7s | 0 | PASS：健康file ID成功；登记失败入一条nullable-file pending任务；Outbox列故障联合返回两错并计1；fake provider合计3 PUT/0 DELETE |
| 消费者与OSS广域回归 | 同一隔离PG运行`go test ./internal/httpapi -run 'GeneratedOSSObject&#124;FavoriteModpackExport&#124;Sticker' -count=1`及`-run OSS` | 消费者包0.486s；OSS包1.423s | 0 / 0 | PASS：MRPack、sticker、生成writer及OSS/outbox/quota/rehome/multipart合同全绿，ARCH-020失败语义未回退 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 全仓30.489s；Vet3.305s；Build3.860s | 0 / 0 / 0 | PASS：生成对象、两个业务消费者及全仓测试、静态分析与构建全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后独立TypeScript | 193 tests3.066s；Lint26.532s；Build26.721s；Type2.060s | 0 / 0 / 0 / 0 | PASS：193/193、lint/type及58页production build全绿；无成功DTO或调用方迁移 |
| Schema/API/边界 | 核查generation142、writer调用方闭集、Outbox完整target/nullable file、error join、晚登记保护及邻项 | 同定向检查 | 0 | PASS：无DDL/回填；两个调用方共享同一补偿；远端public未访问；OPS-013后续task绑定和TEST-037系统矩阵未重复关闭 |
| 格式、差异与台账 | gofmt；`go mod tidy`；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁输出 | 0 / 0 / 0 / 1（预期）/ 0 | PASS：模块图和双仓diff无错误；台账449/449，CLOSED=248、OPEN=201，High=113/Medium=182/Low=52/UNRESOLVED=102；严格模式仅因201个剩余开放项与102项待定严重度按预期失败（304 issues），`-allow-open`通过 |

后端生成文件的provider成功与数据库登记失败之间现在有可靠补偿事实；调用方既不会收到伪成功，也不会在补偿持久化失败时只看到不完整的原始错误。

## 2026-08-23：ARCH-022 评论列表Scan、cursor、总数和目标故障语义关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 旧实现有效RED继承 | PERF-045先以三类cursor/handler/source及generation128合同复现旧OFFSET/COUNT路径（3.600s预期失败）；PERF-046再以watch批装配合同复现逐项resolver（1.074s预期失败） | 既有同根因证据 | 1 / 1（预期） | PASS：两次RED删除的正是审计定位的旧根/回复/watch/COUNT及逐项目标生产路径；本项不伪造已不存在的旧实现失败 |
| 专用源码与fault injection | `go test ./internal/httpapi -run 'TestCommentListReadsRejectScanCursorAndCountFailures&#124;TestCommentAssemblyTargetAndTotalFailuresAreObservable' -count=1` | 包1.053s；墙钟8.5s | 0 | PASS：三类handler完整检查Scan/rows/close；评论与target的Scan/terminal注入均nil/error且关闭；总数故障0/error、健康42/nil |
| Comment广域回归 | `MCMODS_TEST_DATABASE_URL=postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable go test ./internal/httpapi -run Comment -count=1` | 包0.806s；墙钟3.5s | 0 | PASS：评论树、分页、watch批装配、附件、权限注解及故障合同全绿 |
| generation142全目标分支 | `MCMODS_RUN_DB_INTEGRATION=1 DATABASE_URL=postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable go test ./internal/httpapi -run '^TestCommentTargetBatchAllBranchesCompileAgainstFullSchemaIntegration$' -count=1` | 包11.344s；墙钟13.9s | 0 | PASS：会话临时完整Schema中17类target分别及合并编译/执行，批解析错误通道保持有效，未写远端public |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 全仓28.601s；Vet3.396s；Build3.857s | 0 / 0 / 0 | PASS：评论、计数、目标装配及全仓测试、静态分析与构建全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后独立TypeScript | 193 tests3.032s；Lint23.846s；Build23.274s；Type1.952s | 0 / 0 / 0 / 0 | PASS：193/193、lint/type及58页production build全绿；健康评论DTO/cursor不变 |
| Schema/API/边界 | 核查generation142、PERF-045/046责任、系统错误/明确不可见边界及邻项 | 同定向检查 | 0 | PASS：无DDL/回填/协议迁移；数据库故障失败，产品不可见仍明确缺失；SEC-031、SEC-033、BUG-083/085、TEST-038未重复关闭 |
| 格式、差异与台账 | gofmt；`go mod tidy`；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁输出 | 0 / 0 / 0 / 1（预期）/ 0 | PASS：模块图和双仓diff无错误；台账449/449，CLOSED=249、OPEN=200，High=113/Medium=183/Low=52/UNRESOLVED=101；严格模式仅因200个剩余开放项与101项待定严重度按预期失败（302 issues），`-allow-open`通过 |

评论目录、回复和watch现在只在page rows、完整评论装配、目标批解析及可见总数全部成功后返回；Schema漂移和中途断连不再表现为缺项或总数0。

## 2026-08-23：ARCH-023 收藏membership Scan与cursor故障语义关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 旧实现有效RED继承 | PERF-051先以旧无界collection/item/membership GET、集合1+N及前端调用合同执行有效双端RED | 既有同根因证据 | 1（预期） | PASS：删除的旧`favoriteMembership`正是审计定位的跳过Scan/忽略cursor路径；本项不恢复失效入口或伪造RED |
| 专用fault injection | `go test ./internal/httpapi -run 'TestFavoriteMembershipReadsDoNotHideScanOrCursorFailures' -count=1` | 包1.104s；墙钟8.8s | 0 | PASS：collection/item/summary三类Scan与terminal故障均返回nil或零值/error并关闭rows；源码锁定旧GET删除和当前handler 500顺序 |
| Favorite广域回归 | `go test ./internal/httpapi -run Favorite -count=1` | 包0.426s；墙钟3.1s | 0 | PASS：收藏分页、summary、delta、可见性、导出及错误合同全绿 |
| 百万行规模回归 | `MCMODS_RUN_DB_INTEGRATION=1 DATABASE_URL=postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable go test ./internal/httpapi -run '^TestFavoriteHandlersBoundMillionRowsAndAvoidCollectionFanoutIntegration$' -count=1` | 包21.076s；墙钟23.7s | 0 | PASS：隔离临时Schema的一百万收藏夹/收藏项handler继续满足PERF-051的有界SQL、响应、cursor与索引合同，未访问远端public |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 全仓29.163s；Vet3.427s；Build3.851s | 0 / 0 / 0 | PASS：收藏collector、handler及全仓测试、静态分析与构建全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后独立TypeScript | 193 tests3.037s；Lint26.385s；Build23.525s；Type1.969s | 0 / 0 / 0 / 0 | PASS：193/193、lint/type及58页production build全绿；健康收藏协议与调用不变 |
| Schema/API/边界 | 核查generation142、旧GET删除、三类当前row stream、PERF-051责任及邻项 | 同定向检查 | 0 | PASS：无DDL/回填/协议迁移；系统故障稳定5xx；ARCH-024、BUG-089、SEC-036、LEGACY-018、STYLE-006、TEST-041未重复关闭 |
| 格式、差异与台账 | gofmt；`go mod tidy`；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁输出 | 0 / 0 / 0 / 1（预期）/ 0 | PASS：模块图和双仓diff无错误；台账449/449，CLOSED=250、OPEN=199，High=113/Medium=183/Low=52/UNRESOLVED=101；严格模式仅因199个剩余开放项与101项待定严重度按预期失败（301 issues），`-allow-open`通过 |

收藏页和membership summary现在只有在整个有界row stream均可解码且游标正常结束后才返回；数据库事故不再表现为缺少某个收藏夹、收藏项或目标membership。

## 2026-08-23：ARCH-024 收藏夹写入数据库错误分类关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | 先加入合法public ID、unique/NoRows/XX000/健康矩阵，运行`go test ./internal/httpapi -run 'TestFavoriteCollectionWritesSeparateBusinessAndDatabaseFailures&#124;TestFavoriteCollectionWritesClassifyDatabaseFailuresIntegration' -count=1` | 包1.157s；墙钟8.7s | 1（预期） | PASS：强制create/update/delete数据库故障被旧代码分别返回409/404/409；源码同时缺少精确分类与结构化日志 |
| 定向GREEN | `MCMODS_TEST_DATABASE_URL=postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable go test ./internal/httpapi -run 'TestFavoriteCollectionWritesSeparateBusinessAndDatabaseFailures&#124;TestFavoriteCollectionWritesClassifyDatabaseFailuresIntegration' -count=1` | 包1.144s；墙钟8.6s | 0 | PASS：健康create201/delete204；create/update unique409；update missing404；default/missing delete409；三类XX000均500；日志字段源码合同通过 |
| Favorite广域回归 | 同一隔离PG运行`go test ./internal/httpapi -run Favorite -count=1` | 包0.526s；墙钟3.2s | 0 | PASS：收藏分页、summary、delta、可见性、导出及读写错误合同全绿 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟14.040s；Vet10.582s；Build11.385s | 0 / 0 / 0 | PASS：收藏写分类、结构化日志及全仓测试、静态分析与构建全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；ESLint；独立TypeScript；测试HTTPS环境Next16 production build | 193 tests3.313s（墙钟3.751s）；Lint25.413s；Type3.759s；Build23.6s | 0 / 0 / 0 / 0 | PASS：193/193、lint/type及58页production build全绿；健康收藏协议与调用不变 |
| Schema/API/边界 | 核查generation142、SQLSTATE/NoRows、DELETE RETURNING、日志、临时/public隔离及邻项 | 同定向检查 | 0 | PASS：无DDL/回填/DTO变化；无先查后删；远端public未访问；BUG-089、SEC-036、LEGACY-018、STYLE-006、TEST-041未重复关闭 |
| 格式、差异与台账 | gofmt；`go mod tidy`；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁输出 | 0 / 0 / 0 / 1（预期）/ 0 | PASS：模块图和双仓diff无错误；台账449/449，CLOSED=251、OPEN=198，High=113/Medium=183/Low=52/UNRESOLVED=101；严格模式仅因198个剩余开放项与101项待定严重度按预期失败（300 issues），`-allow-open`通过 |

收藏夹写入现在为业务冲突、明确不存在和系统故障保留互不混淆的状态；连接、Schema和trigger事故会触发5xx及结构化告警，而不是伪装成用户输入问题。

## 2026-08-23：ARCH-025 社区目录、引用和翻译结果数据故障关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG/source RED | 先加入标量reference names、健康/损坏/缺失translation、owner/NoRows/改列及update分阶段源码矩阵，运行两项ARCH-025测试 | 包1.196s；墙钟8.6s | 1（预期） | PASS：旧引用损坏返回nil error，损坏payload和缺translation仍completed200，任务Schema故障伪404；目录/更新又缺结构化分阶段合同 |
| 定向GREEN | `MCMODS_TEST_DATABASE_URL=postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable go test ./internal/httpapi -run 'TestCommunityPostReadsAndUpdatesDoNotHideDataFailures&#124;TestCommunityPostCorruptReferencesAndTranslationResultsFailClosedIntegration' -count=1` | 包1.238s；墙钟8.7s | 0 | PASS：标量names明确decode error；健康任务含translation；损坏payload/缺结果/Schema故障500；owner/missing404；update lookup/apply/commit源码闭集通过 |
| Community广域回归 | 同一隔离PG运行`go test ./internal/httpapi -run 'Community&#124;ContentTranslation' -count=1` | 包0.444s；墙钟3.2s | 0 | PASS：社区CRUD、引用、悬赏、可见性、本地化任务及Worker回归全绿 |
| 百万目录回归 | `MCMODS_RUN_DB_INTEGRATION=1 DATABASE_URL=postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable go test ./internal/httpapi -run '^TestCommunityPostCatalogBoundsMillionRowsAndOmitsBodiesIntegration$' -count=1` | 包20.316s；墙钟23.0s | 0 | PASS：PERF-052的一百万窄投影、无正文/COUNT、cursor、SQL/响应及索引预算保持，隔离临时Schema未访问远端public |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟14.581s；Vet10.690s；Build11.501s | 0 / 0 / 0 | PASS：社区错误边界、共享payload decoder及全仓测试、静态分析与构建全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；ESLint；独立TypeScript；测试HTTPS环境Next16 production build | 193 tests3.125s（墙钟3.856s）；Lint26.607s；Type4.427s；Build24.3s | 0 / 0 / 0 / 0 | PASS：193/193、lint/type及58页production build全绿；健康社区/翻译DTO不变 |
| Schema/API/边界 | 核查generation142、NoRows/error、JSON shape、completed result、update commit、PERF-052/SEC-037及邻项 | 同定向检查 | 0 | PASS：无DDL/回填/协议迁移；远端public未访问；ARCH-026、BUG-090/091、TEST-042未重复关闭 |
| 格式、差异与台账 | gofmt；`go mod tidy`；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁输出 | 0 / 0 / 0 / 1（预期）/ 0 | PASS：模块图和双仓diff无错误；台账449/449，CLOSED=252、OPEN=197，High=113/Medium=183/Low=52/UNRESOLVED=101；严格模式仅因197个剩余开放项与101项待定严重度按预期失败（299 issues），`-allow-open`通过 |

社区页面与翻译状态现在不会把损坏JSON、缺失completed结果、游标中断或提交失败表现为合法空数据/完成态；每个系统故障都在成功响应前失败并留下可定位stage。

## 2026-08-23：ARCH-026 AI翻译结果持久化与完成态一致性关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG/source RED | 先加入catalog completed payload/result/item损坏和Worker/persistence顺序合同，运行两项ARCH-026测试 | 包1.165s；墙钟8.6s | 1（预期） | PASS：旧三类损坏均completed200；源码证明completed UPDATE早于notification/content业务持久化，void persistence与scope JSON继续吞错 |
| 定向GREEN与故障扩展 | `MCMODS_TEST_DATABASE_URL=postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable go test ./internal/httpapi -run 'TestAITranslationsPersistValidatedBusinessResultsBeforeCompletion&#124;TestCatalogTranslationResultRejectsCorruptCompletedTasksIntegration' -count=1` | 最终包1.179s；墙钟8.7s | 0 | PASS：catalog/notification健康及三类损坏；notification upsert、坏列；failed状态与XX000；GET不刷新2020时间戳；源码顺序/CAS/log闭集全绿 |
| AI/通知/社区广域回归 | 同一隔离PG运行`go test ./internal/httpapi -run 'AI&#124;Translation&#124;Notification&#124;Community' -count=1` | 包2.216s；墙钟4.9s | 0 | PASS：Outbox、provider解析、任务配置、通知、catalog/community持久化和既有权限回归全绿 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟14.106s；Vet10.728s；Build11.954s | 0 / 0 / 0 | PASS：AI结果顺序、失败状态、纯读结果API及全仓测试、静态分析与构建全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；ESLint；独立TypeScript；测试HTTPS环境Next16 production build | 193 tests3.085s（墙钟3.835s）；Lint26.428s；Type4.300s；Build24.0s | 0 / 0 / 0 / 0 | PASS：193/193、lint/type及58页production build全绿；健康任务/result DTO不变 |
| Schema/API/边界 | 核查generation142、strict item、业务写→complete CAS、failed状态、stale-running补偿、GET纯读及邻项 | 同定向检查 | 0 | PASS：无DDL/回填；远端public未访问；OPS-019、BUG-094、LEGACY-019、TEST-043未重复关闭 |
| 格式、差异与台账 | gofmt；`go mod tidy`；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁输出 | 0 / 0 / 0 / 1（预期）/ 0 | PASS：模块图和双仓diff无错误；台账449/449，CLOSED=253、OPEN=196，High=113/Medium=183/Low=52/UNRESOLVED=101；严格模式仅因196个剩余开放项与101项待定严重度按预期失败（298 issues），`-allow-open`通过 |

AI completed现在表示可解码且已持久化的业务翻译真实存在；错误先形成可观察failed状态，结果GET不再承担补写或把损坏数据返回为空翻译。

## 2026-08-23：ARCH-027 经济与任务读取错误完整性关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | 先加入总览三表故障、null经济设置/currency/shop/task JSON与健康控制，运行`TestEconomyAndTaskReadErrorsAreObservableIntegration` | 用例41.14s；包42.169s | 1（预期） | PASS：旧经验、时区、check-in表故障均200，null经济配置又返回默认200；测试在首个JSON失败后停止，已足以证明同一静默降级根因 |
| fault injection与纯函数GREEN | `go test ./internal/httpapi -run 'Test(EconomyAndActivityCollectorsRejectTerminalRowErrors&#124;StoredJSONObjectRejectsSilentEmptyFallbacks&#124;EconomyConfigDefaultsOnlyForMissingSetting&#124;EconomyAndTaskReadPathsDoNotDiscardErrors)$' -count=1` | 包1.071s；墙钟8.6s | 0 | PASS：余额/活动terminal error均返回error且关闭rows；空/null/数组/标量/坏JSON拒绝；仅设置NoRows默认；点名源码无丢弃JSON结果 |
| 本机真实PG故障矩阵 | 显式`MCMODS_TEST_DATABASE_URL`与`DATABASE_URL=postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable`运行ARCH-027集成测试 | 包12.478s；并行墙钟24.3s | 0 | PASS：健康总览/配置/任务200；experience/users/checkins改名均500；null配置、currency翻译、shop翻译/config及task翻译均500；隔离Schema完整清理 |
| 经济/任务广域回归 | 同一本机URL运行currency range、active reference、heat sequence、invalid active task与ARCH-027矩阵 | 包19.484s；墙钟22.0s | 0 | PASS：checked economy、配置引用、原子热度序号、任务condition/reward失败关闭及新读取边界全部通过 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 最终全仓12.970s；Vet9.672s；Build10.315s | 0 / 0 / 0 | PASS：首次并发全仓仅JetStream重启用例发生一次已确认重投，精确目标随后3/3通过；共享JSON helper复用调整后的最终全仓/Vet/Build全绿，生产改动与该瞬时失败无关 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后独立TypeScript | 193 tests3.334s（墙钟4.019s）；Lint约30.0s；Build约30.0s；Type2.560s | 0 / 0 / 0 / 0 | PASS：193/193、lint/type及58页production build全绿；健康DTO和第一方调用不变 |
| 运行隔离说明 | 一次无效的全integration全仓尝试只设置了测试URL而未覆盖`DATABASE_URL`，在配置的generation85数据库兼容检查/缺函数读取处失败并被立即中止 | 不计入门禁 | 1（已中止） | PASS（隔离纠正）：未执行DDL或写入；新增测试随后改为优先读取`MCMODS_TEST_DATABASE_URL`，全部有效PG证据同时显式覆盖两变量并指向本机55432 |
| Schema/API/边界 | 核查generation142、NoRows/default、object shape、PERF-056/TEST-044/ARCH-032/OPS-020及远端写边界 | 同定向检查 | 0 | PASS：无DDL/回填/协议迁移；无远端写入；相邻等级规模、经济授权系统矩阵、渲染JSON和调度责任未重复关闭 |
| 格式、差异与台账 | gofmt；`go mod tidy`前后hash；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁输出 | 0 / 0 / 0 / 1（预期）/ 0 | PASS：模块图和双仓diff无错误；台账449/449，CLOSED=254、OPEN=195，High=113/Medium=183/Low=52/UNRESOLVED=101；严格模式仅因195个剩余开放项与101项待定严重度按预期失败（297 issues），`-allow-open`通过 |

经济与任务页面现在只会返回完整、可解码的持久事实；真正缺失仍保留既有默认或业务状态，而数据库和JSON损坏稳定成为可重试、可告警的5xx。

## 2026-08-23：ARCH-029 站点更新列表与聚合读取完整性关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效残余源码RED | 新增`TestSiteChangelogReadsDoNotReturnUncheckedOrPartialData`后运行目标测试 | 用例0.00s；包1.015s；墙钟8.366s | 1（预期） | PASS：PERF-058已删除旧列表，但后台详情仍精确命中`"translations": json.RawMessage(translations)`，证明aggregate shape可未验证穿透 |
| collector/decoder/detail GREEN | 运行ARCH-029三项fake/source测试 | 包1.028s；墙钟8.317s | 0 | PASS：public/admin Scan和terminal故障均nil/error且close；null/array/错误value拒绝；NoRows保留；合法locale title/body完整；源码锁定collector/loader/log闭集 |
| 本机真实PG三handler矩阵 | 本机测试URL运行`TestSiteChangelogHandlersFailClosedOnSchemaErrorsIntegration`及三项collector/detail合同 | 最终包1.138s；墙钟8.489s | 0 | PASS：健康public/admin page/detail均200；首次运行发现DATE binary→string假设并改为time.Time；正文列改名后三handler均500且记录stage/identity |
| SiteChangelog广域回归 | `go test ./internal/httpapi -run SiteChangelog -count=1` | 包1.208s；墙钟10.226s | 0 | PASS：严格cursor、SQL合同、详情、前后端handler源边界及故障矩阵全绿 |
| 百万行规模回归 | 显式本机`MCMODS_RUN_DB_INTEGRATION=1`和双DB URL运行`TestSiteChangelogPagesStayIndexedAndReachableAtOneMillionRowsIntegration` | 包7.339s；墙钟16.196s | 0 | PASS：1M changelog+1M translation的public/admin深页各一SQL并命中generation134索引；新头不穿cursor，完整可达性不回退 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 全仓16.301s；Vet12.503s；Build13.293s | 0 / 0 / 0 | PASS：collector、typed detail、structured log及全仓测试/静态分析/构建全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后独立TypeScript | 193 tests3.716s（墙钟4.286s）；Lint约30.0s；Build24.631s；Type2.264s | 0 / 0 / 0 / 0 | PASS：193/193、lint/type及58页production build全绿；健康页和详情DTO不变 |
| Schema/API/边界 | 核查generation142/134索引、NoRows/error、JSON shape、PERF-058/BUG-101/TEST-046及远端隔离 | 同定向检查 | 0 | PASS：无DDL/回填/协议迁移；本机临时表且无远端写入；规模、写并发与完整站务行为未重复关闭 |
| 格式、差异与台账 | gofmt；`go mod tidy`前后hash；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁输出 | 0 / 0 / 0 / 1（预期）/ 0 | PASS：模块图和双仓diff无错误；台账449/449，CLOSED=255、OPEN=194，High=113/Medium=184/Low=52/UNRESOLVED=100；严格模式仅因194个剩余开放项与100项待定严重度按预期失败（295 issues），`-allow-open`通过 |

站点更新目录和详情现在只在完整读取并验证全部翻译结构后成功；游标中断、Schema漂移和aggregate损坏不会再伪装成页结束或可信JSON。

## 2026-08-23：ARCH-030 举报、小黑屋与治理详情读取完整性关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效残余源码RED | 新增三项ARCH-030测试后运行目标正则 | 编译2.7s | 1（预期） | PASS：PERF-060/ARCH-013已修旧循环与simple rows，但源码测试精确命中`_ = json.Unmarshal(raw,&result)`，并证明三路集中collector/治理日志尚不存在 |
| collector/decoder GREEN | `go test ./internal/httpapi -run 'TestGovernanceReadsDoNotReturnUncheckedOrPartialData&#124;TestGovernanceCollectorsRejectScanAndTerminalFailures&#124;TestReportSnapshotRejectsSilentNullOrNonObjectFallbacks&#124;TestGovernanceListHandlersRemoveOffsetAndCheckIterationErrors' -count=1` | 包1.055s；墙钟8.6s | 0 | PASS：三路Scan和terminal故障均nil/error且close；空/null/数组/标量/坏JSON拒绝，合法object保留；源码锁定collector/decoder/log闭集 |
| 本机真实PG四handler矩阵 | 双DB URL指向本机55432运行`TestGovernanceHandlersFailClosedOnDatabaseAndSnapshotErrorsIntegration` | 包1.193s；墙钟8.6s | 0 | PASS：健康本人举报、后台队列、小黑屋和详情均200；null快照、reports/ban/review临时改列均500；详情simple-row错误不再伪空 |
| Governance广域回归 | 同一本机URL运行`go test ./internal/httpapi -run 'Governance&#124;ReportSnapshot&#124;ReportTargetVisibility' -count=1` | 包0.413s；墙钟3.2s | 0 | PASS：严格cursor、目标可见性、collector、snapshot和真实PG故障矩阵全绿 |
| 百万行规模回归 | 显式本机`MCMODS_RUN_DB_INTEGRATION=1`和双DB URL运行`TestGovernancePagesStayIndexedAndStableAtOneMillionRowsIntegration` | fixture10.428s；包10.633s；墙钟13.3s | 0 | PASS：1M reports+1M bans三路深页继续各一SQL并使用tuple-keyset索引，稳定性断言全绿 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 并行墙钟15.160s；Vet10.838s；Build11.732s | 0 / 0 / 0 | PASS：治理collector、严格快照、结构化日志及全仓测试/静态分析/构建全绿 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；ESLint；独立TypeScript；测试HTTPS环境Next16 production build；build后TypeScript | 193 tests3.313s（墙钟3.825s）；Lint25.144s；Type4.009s；Build23.6s；post-Type2.5s | 0 / 0 / 0 / 0 / 0 | PASS：193/193、lint/type及58页production build全绿；首次未提供生产required URL的调用被既有配置门拒绝，补齐测试HTTPS环境后正式门禁通过；健康治理DTO不变 |
| Schema/API/边界 | 核查generation142/136索引、NoRows/error、snapshot object shape、ARCH-013/PERF-060/TEST-048及远端隔离 | 同定向检查 | 0 | PASS：无DDL/回填/协议迁移；本机会话临时表且无远端写入；规模、共享helper和广域治理行为未重复关闭 |
| 格式、差异与台账 | gofmt；`go mod tidy`前后hash；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁输出 | 0 / 0 / 0 / 1（预期）/ 0 | PASS：模块图和双仓diff无错误；台账449/449，CLOSED=256、OPEN=193，High=113/Medium=185/Low=52/UNRESOLVED=99；严格模式仅因193个剩余开放项与99项待定严重度按预期失败（293 issues），`-allow-open`通过 |

治理列表和详情现在只在页游标、每行数据、简单行集合和授权快照全部完整可用时成功；连接中断、Schema漂移或JSON损坏不会再伪装成页结束、无证据或空快照。

## 2026-08-23：ARCH-031 封禁理由加载错误状态关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效前端源码RED | 新增`ban-reason-load-state.test.mts`并独立运行 | 2/2失败；墙钟0.7s | 1（预期） | PASS：第一项精确命中旧`.catch(()=>setItems([]))`，第二项证明主locale没有加载/失败说明；显式state/retry/禁用合同也均不存在 |
| 定向GREEN与静态门 | 运行ban-reason/governance四项tests；聚焦ESLint；独立TypeScript | 4/4，0.600s；Lint4.261s；Type2.945s | 0 / 0 / 0 | PASS：显式items/loading/error/reload、AbortController、request key、alert/status、retry、两调用方禁用及中英locale闭集全绿；React effect lint无规避 |
| 前端全量门禁 | 全部`app/_lib/*.test.mts`；测试HTTPS环境Next16 production build；build后ESLint/TypeScript | 195 tests4.242s（墙钟5.274s）；Build24.395s；Lint25.341s；Type13.483s | 0 / 0 / 0 / 0 | PASS：195/195、lint/type及58页production build全绿；一次并行typecheck恰逢build重建`.next/types`产生文件竞争，不计入源码门，串行build后最终Type门通过 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 全仓12.312s；Vet7.568s；Build8.783s | 0 / 0 / 0 | PASS：后端协议未改，治理API及全仓测试、静态分析与构建全绿 |
| Schema/API/边界 | 核查generation142、成功空值/error、scope代次、禁用范围及TEST-048责任 | 同定向检查 | 0 | PASS：无DDL/回填/后端DTO迁移；合法成功空目录与系统故障可区分；只禁用ban操作，不阻止无ban的举报审核；广域治理矩阵未重复关闭 |
| 格式、差异与台账 | `go mod tidy`前后hash；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁输出 | 0 / 0 / 1（预期）/ 0 | PASS：模块图和双仓diff无错误；台账449/449，CLOSED=257、OPEN=192，High=113/Medium=186/Low=52/UNRESOLVED=98；严格模式仅因192个剩余开放项与98项待定严重度按预期失败（291 issues），`-allow-open`通过 |

封禁理由目录现在具有可见、可重试并与请求scope绑定的错误状态；权限、数据库或网络事故不再显示成“没有配置理由”，且未知状态不会触发封禁操作。

## 2026-08-23：ARCH-032 合成表渲染JSON完整性关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效源码/行为RED | 新增ARCH-032单元、源码与真实PG测试后运行目标正则 | 包1.155s；墙钟8.7s | 1（预期） | PASS：历史parameters null仍成功渲染，导入parameters null和template canvas数组均被接受，源码精确命中忽略`json.Unmarshal`错误的旧decoder |
| 扩展定向GREEN | 本机双DB URL运行五项`TestRecipe...JSON...`定向测试 | 包1.174s；墙钟8.5s | 0 | PASS：导入五类字段拒绝null/数组/字符串/数字；decoder拒绝空/坏语法/非对象；真实PG七类历史列共32组均失败，健康parameters/slot/alternative完整 |
| Recipe/JEI广域回归 | 本机双DB URL运行`go test ./internal/httpapi -run '(Recipe&#124;JEI)' -count=1` | 2.75s | 0 | PASS：导入、promotion、编辑器、权威/观察render和既有身份/批处理合同全绿 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 全仓33.75s；Vet9.82s；Build10.60s | 0 / 0 / 0 | PASS：严格object错误传播、真实PG矩阵及全仓测试/静态分析/构建全绿 |
| 前端全量门禁 | 全部78个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 195 tests2.904s（墙钟3.007s）；Lint24.26s；Build23.967s；Type2.11s | 0 / 0 / 0 / 0 | PASS：195/195、lint/type及58页production build全绿；前端协议未改。package脚本当前只列49个文件，因此本门显式枚举全部78个文件而非误报部分套件 |
| Schema/API/边界 | 核查generation142、导入默认/显式shape、权威/观察render、历史损坏与远端隔离 | 同定向检查 | 0 | PASS：无DDL/回填/协议版本迁移；只拒绝畸形导入并把历史损坏改5xx；本机会话临时表且无远端写入，健康DTO不变 |
| 格式、依赖、差异与台账 | gofmt；`go mod tidy`前后hash；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁输出 | 0 / 0 / 0 / 1（预期）/ 0 | PASS：tidy前后go.mod/go.sum哈希不变且双仓diff无错误；台账449/449，CLOSED=258、OPEN=191，High=113/Medium=186/Low=52/UNRESOLVED=98；严格模式仅因191个剩余开放项与98项待定严重度按预期失败（290 issues），`-allow-open`通过 |

合成表公开渲染现在只接受与协议一致的对象字段；导入或历史存储中的非对象JSON会带字段上下文失败，不再被静默改写成看似合法的空对象。

## 2026-08-23：BUG-025、BUG-026、LEGACY-007 单板块旧写权威删除

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效HTTP/行为/PG RED | 新增三项section authority测试并用本机测试URL运行 | 包1.229s；墙钟8.7s | 1（预期） | PASS：旧PUT命中认证层返回401而非405；创建规范化接受parent；真实PG旧edit快照成功把section从version1更新到version2 |
| 定向GREEN | 本机URL运行route/empty-root/publication三项 | 包1.146s；墙钟8.6s | 0 | PASS：PUT 405、DELETE pattern保留；POST拒绝parent/resources；旧edit与跨版本create零变更失败，同版本pending空根激活并保存本地化 |
| ModContent广域回归 | 本机双DB URL运行`go test ./internal/httpapi -run 'ModContent' -count=1` | 2.96s | 0 | PASS：空根创建、审核发布、layout PATCH、binding资源解析、树深/批写、分页、详情和子树归档全绿 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 全仓33.24s；Vet9.74s；Build10.74s | 0 / 0 / 0 | PASS：旧路由删除、历史快照失败关闭及全仓测试/静态分析/构建全绿 |
| 前端全量门禁 | 全部78个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 195 tests3.086s（墙钟3.2s）；Lint26.581s；Build23.438s；Type2.068s | 0 / 0 / 0 / 0 | PASS：195/195、lint/type及58页production build全绿；第一方创建空根、DELETE和layout PATCH调用无需迁移，PUT调用为零 |
| Schema/API/边界 | 核查generation142、三写权威、遗留pending快照、binding归属及远端隔离 | 同定向检查 | 0 | PASS：无DDL/回填；PUT明确删除而非双协议；历史edit不可发布，空根创建与layout/DELETE成功合同不变；本机会话临时表且无远端写入 |
| 格式、依赖、差异与台账 | gofmt；`go mod tidy`前后hash；双仓`git diff --check`；`verify_findings`严格及`-allow-open` | 见本节最终门禁输出 | 0 / 0 / 0 / 1（预期）/ 0 | PASS：tidy前后go.mod/go.sum哈希不变且双仓diff无错误；台账449/449，CLOSED=261、OPEN=188，High=114/Medium=188/Low=52/UNRESOLVED=95；严格模式仅因188个剩余开放项与95项待定严重度按预期失败（284 issues），`-allow-open`通过 |

资料板块现在只有空根创建、整树layout更新和完整子树归档三种互斥写责任；任何新请求或遗留审核快照都不能再单独改变section版本或用owner字段重解释已绑定资源。

## 2026-08-23：BUG-027 跨模块row stream完整性关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效源码RED | 新增BUG-027逐函数row-stream测试后运行`go test ./internal/httpapi -run 'TestBug027' -count=1` | 包1.484s；墙钟9.2s | 1（预期） | PASS：精确报告21条当前循环缺少terminal检查，并命中项目更新pending和广播收件人的丢弃Scan；另5条审计路径已严格且未误报 |
| 定向fake/源码/真实PG GREEN | 运行三项BUG-027测试；再以本机双DB URL运行含探测claim集成测试 | 包1.253s/墙钟8.7s；扩展包1.395s/墙钟9.1s | 0 / 0 | PASS：21条循环及5条既有路径闭集通过；fake terminal failure传播并close；真实PG类型Scan故障使claim事务回滚且`next_probe_at`不变 |
| 跨域广域回归 | `go test ./internal/httpapi -run '(ModContent&#124;SimpleProject&#124;Modpack&#124;ProjectFollow&#124;Notification&#124;MinecraftServer&#124;SeedCrawler&#124;ProjectAutomation)' -count=1` | 3.71s | 0 | PASS：资料、项目关联、关注、通知、探测及既有严格调度路径全绿 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 全仓34.42s；Vet10.17s；Build11.18s | 0 / 0 / 0 | PASS：完整row stream、事务claim、Worker错误传播及全仓测试/静态分析/构建全绿 |
| 前端全量门禁 | 全部78个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 195 tests3.346s（墙钟3.481s）；Lint29.044s；Build24.965s；Type2.300s | 0 / 0 / 0 / 0 | PASS：195/195、lint/type及58页production build全绿；健康API协议未改，测试显式枚举全部78个文件 |
| Schema/API/边界 | 核查generation142、HTTP成功边界、任务事务/副作用次序、既有5条严格路径及远端隔离 | 同定向检查 | 0 | PASS：无DDL/回填/DTO迁移；本机单连接临时表且无远端写入；NoRows业务语义不变，故障改5xx或可见Worker失败 |
| 格式、依赖与差异 | gofmt；`go mod tidy`前后SHA-256；双仓`git diff --check` | 见本节最终门禁输出 | 0 / 0 / 0 | PASS：go.mod为`695B...6728`、go.sum为`C151...D783`且tidy前后不变；双仓diff无错误（仅既有LF/CRLF提示） |
| 449台账验证 | `go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | 墙钟2.122s | 1（预期）/ 0 | PASS：台账449/449，CLOSED=262、OPEN=187，High=115/Medium=188/Low=52/UNRESOLVED=94；严格模式仅因187个剩余开放项、94项待定严重度及最终严重度总数按预期失败（282 issues），`-allow-open`通过 |

跨模块列表、关联装配和后台任务现在共享同一成功边界：必须完整读完数据库游标，才能返回成功、提交claim或开始通知/探测副作用。

## 2026-08-23：BUG-029 整合包主分类权威关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效HTTP/归一化RED | 修正完整loader/version夹具后运行`go test ./internal/httpapi -run 'TestBUG029' -count=1` | 包1.006s；墙钟8.247s | 1（预期） | PASS：`orphan_category`被共享规范化接受，create继续进入数据库并在nil测试DB处panic，证明旧入口没有分类拒绝边界 |
| 有效Schema/真实PG RED | generation源码测试与本机完整临时Schema直接插入矩阵 | 包11.897s；墙钟14.249s | 1（预期） | PASS：当前generation仍142且没有命名CHECK，未知分类直接SQL无错误持久化 |
| 定向GREEN | HTTP/规范化两项；本机generation143源码与完整临时Schema两项 | HTTP包1.175s/墙钟9.378s；DB包11.424s/墙钟13.610s | 0 / 0 | PASS：16类与空默认成功、非法值数据库前400；完整Schema允许16类并以`modpacks_primary_category_check`的23514拒绝未知值 |
| Modpack/Schema广域回归 | 本机URL运行`(Modpack&#124;ProjectCreationAuthorization&#124;MetadataImport)`；数据库全单元；catalogpolicy包 | HTTP包10.038s/墙钟17.483s；DB0.118s/墙钟1.781s；policy0.444s | 0 / 0 / 0 | PASS：创建/修订/导入、目录过滤、2000项边界、ACL及generation143所有Schema源码门全绿 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 全仓15.708s；Vet10.404s；Build11.401s | 0 / 0 / 0 | PASS：共享注册表、生成CHECK、generation消费者及全仓测试/静态分析/构建全绿 |
| 前端全量门禁 | 全部78个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 195 tests3.027s（墙钟3.127s）；Lint25.935s；Build23.483s；Type2.048s | 0 / 0 / 0 / 0 | PASS：195/195、lint/type及58页production build全绿；既有16项option/i18n与健康协议无需改动 |
| Schema/API/重置边界 | 核查generation143、命名CHECK、同源注册表、开发库重置、无索引/计划变化及数据库隔离 | 同定向检查 | 0 | PASS：唯一DDL是primary_category CHECK；generation142及更早开发库按既有策略重置，无回填/双写；仅本机会话临时Schema，public/远端未写入 |
| 格式、依赖与差异 | gofmt；`go mod tidy`前后SHA-256；双仓`git diff --check` | 见本节最终门禁输出 | 0 / 0 / 0 | PASS：go.mod为`695B...6728`、go.sum为`C151...D783`且tidy前后不变；双仓diff无错误（仅既有LF/CRLF提示） |
| 449台账验证 | `go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | 墙钟1.722s | 1（预期）/ 0 | PASS：台账449/449，CLOSED=263、OPEN=186，High=115/Medium=189/Low=52/UNRESOLVED=93；严格模式仅因186个剩余开放项、93项待定严重度及最终严重度总数按预期失败（280 issues），`-allow-open`通过 |

整合包主分类现在只有一个后端解释来源：HTTP、修订、导入、目录过滤和数据库都不能接受前端产品分类之外的孤儿值。

## 2026-08-23：BUG-030 服务器模组证据来源关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效后端RED | 新增JSON、HTTP和DTO源码三项后运行`go test ./internal/httpapi -run 'TestBUG030' -count=1` | 包1.051s；墙钟8.062s | 1（预期） | PASS：source/confidence成功decode，伪造PATCH继续进入DB并panic，客户端DTO源码仍公开两个证据字段 |
| 有效前端RED | 独立运行`server-mod-evidence-boundary.test.mts` | 测试115.6ms；墙钟0.213s | 1（预期） | PASS：请求没有Declaration类型，CreateServerRequest仍复用DetectedServerMod，提交器可序列化manual/declared或探测证据 |
| 定向GREEN与真实PG | 本机URL运行BUG030/merge/normalize/server-mod save；前端证据边界test及独立TypeScript | 后端包1.149s/墙钟8.596s；前端130.9ms/墙钟0.227s；Type4.343s | 0 / 0 / 0 | PASS：伪造字段数据库前400；探测优先、声明manual；真实PG证明可信configuration持久、编辑保留与新项manual/declared，事务完整rollback |
| Server广域回归 | 本机双DB URL运行`(MinecraftServer&#124;ServerCatalog&#124;ServerProbe&#124;ServerReview&#124;BUG030)` | 包37.976s；墙钟40.371s | 0 | PASS：创建重探测、更新、目录/详情、审核、调度claim/持久化、unresolved引用及证据边界全绿 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 全仓14.002s；Vet10.344s；Build11.266s | 0 / 0 / 0 | PASS：声明/证据类型分离、可信保留查询及全仓测试/静态分析/构建全绿 |
| 前端全量门禁 | 全部79个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 196 tests3.203s（墙钟3.305s）；Lint24.727s；Build22.873s；Type2.110s | 0 / 0 / 0 / 0 | PASS：196/196、lint/type及58页production build全绿；probe/detail响应证据和声明请求类型明确分离 |
| Schema/API/查询边界 | 核查generation143、无DDL、strict unknown field、创建重探测、更新可信保留、唯一索引与BUG-031责任 | 同定向检查 | 0 | PASS：无Schema/generation变化；保留查询由(server_id,raw_mod_id)唯一索引支撑；旧证据请求明确400；多来源快照清理由BUG-031独立负责 |
| 格式、依赖与差异 | gofmt；`go mod tidy`前后SHA-256；双仓`git diff --check` | 见本节最终门禁输出 | 0 / 0 / 0 | PASS：go.mod为`695B...6728`、go.sum为`C151...D783`且tidy前后不变；双仓diff无错误（仅既有LF/CRLF提示） |
| 449台账验证 | `go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | 墙钟1.745s | 1（预期）/ 0 | PASS：台账449/449，CLOSED=264、OPEN=185，High=116/Medium=189/Low=52/UNRESOLVED=92；严格模式仅因185个剩余开放项、92项待定严重度及最终严重度总数按预期失败（278 issues），`-allow-open`通过 |

服务器模组的公开来源和置信度现在只能来自后端探测或既有可信事实；编辑请求只能声明“有哪些模组”，不能自封为自动探测证据。

## 2026-08-23：BUG-031 服务器模组完整快照关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效Schema RED | `go test ./internal/database -run TestBUG031 -count=1` | 包0.972s；墙钟3.407s | 1（预期） | PASS：源码仍为generation143，精确失败`schema generation=143 want 144`，证明多来源DDL尚不存在 |
| 有效真实PG RED | generation143临时完整Schema运行`go test ./internal/httpapi -run TestBUG031 -count=1` | 包40.031s | 1（预期） | PASS：旧Schema精确返回42P01 `minecraft_server_mod_evidence does not exist`，无法表示同一ID的manual和机器来源或验证快照替换 |
| 定向GREEN | database BUG031源码门；HTTP BUG030/merge边界；generation144临时完整Schema BUG031 | DB包0.896s/墙钟3.357s；HTTP包1.027s/墙钟8.941s；PG包44.241s | 0 / 0 / 0 | PASS：多来源闭集、声明/探测双记录、完整与不完整快照事务全部通过；一次测试期望误把version写成confidence，修正测试夹具后产品代码无需变化即通过 |
| 兼容与Server广域 | 既有unresolved save/read/edit、review summary/detail/keyset；本机URL运行`(MinecraftServer&#124;ServerCatalog&#124;ServerProbe&#124;ServerReview&#124;ServerMod&#124;BUG030&#124;BUG031)` | 兼容包3.326s；广域HTTP包76.834s | 0 / 0 | PASS：详情仍选择可信机器来源并回退声明version，编辑同时保留manual+machine；目录、审核、claim、持久化和级联清理全绿 |
| Schema广域与FK审计 | 本机临时全Schema运行BUG029/BUG031/ephemeral安装；`TestEveryForeignKeyHasLeadingIndex` | DB包85.802s；FK包45.457s | 0 / 0 | PASS：generation144完整安装/删除、既有category invariant及新证据关系全绿；证据主键覆盖FK，public generation在隔离审计前后不变 |
| 开发库重置边界 | 仓库`cmd/db-reset`，显式绑定`postgres://postgres@127.0.0.1:55432/postgres`、development、`RESET postgres` | 墙钟8.125s | 0 | PASS：仅专用本机测试public schema由143重建/seed为144；PostgreSQL服务未停止，无远端或生产数据库操作 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 全仓14.816s；Vet3.630s；Build4.396s | 0 / 0 / 0 | PASS：generation144消费者、快照事务、既有包及全仓测试/静态分析/构建全绿 |
| 前端全量门禁 | 全部79个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 196 tests4.206s（墙钟4.900s）；Lint23.665s；Build23.781s；Type2.668s | 0 / 0 / 0 / 0 | PASS：196/196、lint/type及58页production build全绿；初次build遗漏当前必需`NEXT_PUBLIC_SITE_URL`被配置门拒绝，按`.env.example`补齐SITE/API/Yggdrasil后通过，非产品回归 |
| 格式、依赖与差异 | gofmt；`go mod tidy`前后SHA-256；双仓`git diff --check` | 见本节最终门禁输出 | 0 / 0 / 0 | PASS：go.mod为`695B...6728`、go.sum为`C151...D783`且tidy前后不变；双仓diff无错误（仅既有LF/CRLF提示） |
| 449台账验证 | `go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | 墙钟0.975s / 1.083s | 1（预期）/ 0 | PASS：台账449/449，CLOSED=265、OPEN=184，High=116/Medium=190/Low=52/UNRESOLVED=91；严格模式仅因184个剩余开放项、91项待定严重度及最终严重度总数按预期失败（276 issues），`-allow-open`通过 |

服务器模组关系现在表示“当前至少一种来源”，而不是“历史上曾被观察”：完整机器快照可撤销自身事实，手工声明和不完整探测都不会被误删。

## 2026-08-23：BUG-032 项目文件扫描门控发布关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效Schema RED | `go test ./internal/database -run TestBUG032 -count=1` | 包0.996s；墙钟3.688s | 1（预期） | PASS：当前generation仍144，精确失败`generation=144 want 145`，没有processing/generation/安全trigger |
| 有效真实PG RED | generation144临时完整Schema运行`go test ./internal/httpapi -run TestBUG032 -count=1` | 包44.231s | 1（预期） | PASS：pending立即active、发1个add并出现在列表；rejected关系仍active且已发add；直接SQL可把pending OSS关联写成active，逐项复现审计证据 |
| 定向GREEN | database BUG032/BUG031/ProjectFile源码门；HTTP ProjectFile/BUG032单元；generation145完整临时Schema生命周期 | DB包0.892s/墙钟3.406s；HTTP包1.049s/墙钟8.509s；PG包49.210s | 0 / 0 / 0 | PASS：pending隐藏零事件、clean激活、active→pending撤下/remove、再次clean generation2、rejected零add，unsafe direct insert命中命名23514 |
| ProjectFile/事件/规模广域 | 本机URL运行`(ProjectFile&#124;ProjectUpdate&#124;BUG032)`；database BUG032/ProjectFile/FK | HTTP包47.243s；DB包79.688s | 0 / 0 | PASS：创建/删除/下载、供应商边界、通知合并和100k站内keyset页全绿；新trigger/FK与generation145完整Schema通过 |
| 开发库重置边界 | 仓库`cmd/db-reset`，显式绑定`postgres://postgres@127.0.0.1:55432/postgres`、development、`RESET postgres` | 墙钟8.200s | 0 | PASS：仅专用本机测试public schema由144重建/seed为145；PostgreSQL服务未停止，无远端或生产数据库操作 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | 全仓17.665s；Vet4.042s；Build4.521s | 0 / 0 / 0 | PASS：scan/关系/event原子状态机、generation145消费者及全仓测试/静态分析/构建全绿 |
| 前端全量门禁 | 全部79个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 197 tests4.082s（墙钟4.773s）；Lint24.714s；Build24.194s；Type3.068s | 0 / 0 / 0 / 0 | PASS：197/197、lint/type及58页production build全绿；前端把clean/trusted视为可下载，pending/rejected提示及扫描后发布文案保持明确 |
| 格式、依赖与差异 | gofmt；`go mod tidy`前后SHA-256；双仓`git diff --check` | 见本节最终门禁输出 | 0 / 0 / 0 | PASS：go.mod为`695B...6728`、go.sum为`C151...D783`且tidy前后不变；双仓diff无错误（仅既有LF/CRLF提示） |
| 449台账验证 | `go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | 墙钟1.121s / 0.965s | 1（预期）/ 0 | PASS：台账449/449，CLOSED=266、OPEN=183，High=117/Medium=190/Low=52/UNRESOLVED=90；严格模式仅因183个剩余开放项、90项待定严重度及最终严重度总数按预期失败（274 issues），`-allow-open`通过 |

项目文件的公开列表、可下载性与关注更新现在由同一安全状态提交：扫描未通过就没有公开发布事实，后来隔离也会原子撤下。

## 2026-08-23：BUG-033 项目Minecraft版本写入权威关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | generation145完整临时Schema运行`go test ./internal/httpapi -run '^TestBUG033ProjectCompatibilityWritesUseAuthoritativeMinecraftVersionsIntegration$' -count=1 -v` | 测试9.65s；包10.665s | 1（预期） | PASS：文件POST精确返回201并把`future-typo`写入processing关系，逐项复现只trim、不核对权威目录的缺陷 |
| 定向GREEN | 同一真实PG测试加入手工日志、review-time发布、外部日志与镜像矩阵后重跑 | 测试9.77s；包10.793s | 0 | PASS：文件/日志未知值400且零事实；审核事务拒绝已移除code；unknown-only自动日志跳过、镜像review，mixed只写1.21.1并保留raw/unmapped metadata |
| BUG032兼容与模块广域 | 本机URL运行`^TestBUG03(2&#124;3)`；运行`(MinecraftVersion&#124;ProjectFile&#124;ProjectChangelog&#124;ProjectAutomation)` | 联合包19.650s；广域包39.646s | 0 / 0 | PASS：扫描门控发布、版本设置故障、100k/1M分页、外部供应商预算、日志历史、自动镜像槽与新版本权威全部通过 |
| HTTP与后端全量门禁 | `go test ./...`；`go vet ./...`；`go build ./...` | 墙钟约10s（三项并行） | 0 / 0 / 0 | PASS：共享pool/tx配置读取接口、审核提交点和全部后端包测试/静态分析/构建全绿 |
| 前端全量门禁 | 全部79个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 197 tests3.078s（墙钟3.680s）；Lint约23.9s；Build约19.9s；Type2.698s | 0 / 0 / 0 / 0 | PASS：197/197、lint/type及58页production build全绿；成功DTO和第一方版本选择payload无需迁移 |
| Schema/API/数据边界 | 核查generation145、全应用保存点、外部raw metadata、审核重验及无DDL/索引/重置 | 同定向检查 | 0 | PASS：Schema保持145；站内两个text[]只接收权威code，供应商未知标签留在独立metadata或review；本机临时Schema已清理，public/远端未写入 |
| 格式、依赖与差异 | gofmt；`go mod tidy`前后SHA-256；双仓`git diff --check` | 见本节最终门禁输出 | 0 / 0 / 0 | PASS：go.mod为`695B...6728`、go.sum为`C151...D783`且tidy前后不变；双仓diff无错误（仅既有LF/CRLF提示） |
| 449台账验证 | `go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | 见本节最终门禁输出 | 1（预期）/ 0 | PASS：台账449/449，CLOSED=267、OPEN=182，High=117/Medium=191/Low=52/UNRESOLVED=89；严格模式仅因182个剩余开放项、89项待定严重度及最终严重度总数按预期失败（272 issues），`-allow-open`通过 |

站内项目兼容数组现在只表达当前Minecraft目录中的明确code；供应商未知标签仍可审计，但只能停留在原始metadata或人工review，不能成为过滤和导出所信任的兼容事实。

## 2026-08-23：BUG-034 更新日志人工覆盖审核时点关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效Schema RED | `go test ./internal/database -run '^TestBUG034' -count=1` | 包0.895s；墙钟3.124s | 1（预期） | PASS：当前generation145，缺少override revision/source、命名CHECK及FK索引，精确失败`generation=145 want 146` |
| 有效真实PG RED | generation145完整临时Schema运行`go test ./internal/httpapi -run '^TestBUG034ManualChangelogOverrideStartsOnlyAfterApprovalIntegration$' -count=1 -v` | 测试9.88s；包11.018s | 1（预期） | PASS：合法项目编辑者得到pending响应后binding已变`manual_override=true`，逐项复现被拒绝revision提前关闭同步；构造初版曾缺项目edit capability，修正权限夹具后取得该有效RED |
| 定向GREEN | database BUG034源码门；generation146完整临时Schema审核/Worker矩阵 | DB包0.854s；HTTP测试9.82s/包10.898s | 0 / 0 | PASS：两次pending均保持false；reject后上游更新成功，approve后记录精确user revision并阻止覆盖；裸boolean旁路命中命名23514 |
| Changelog/Automation与Schema广域 | 本机URL运行`(BUG033&#124;BUG034&#124;ProjectChangelog&#124;ProjectAutomation&#124;ContentReviewQueueQuery)`；database BUG034/Ephemeral/FK | HTTP包42.639s；DB包29.387s | 0 / 0 | PASS：BUG033版本权威、1M日志摘要页、自动化Worker、审核队列、完整临时Schema安装与全部FK leading-index审计全绿 |
| 开发库重置边界 | 仓库`cmd/db-reset`，显式绑定`postgres://postgres@127.0.0.1:55432/postgres`、development、`RESET postgres` | 墙钟8.086s | 0 | PASS：仅专用本机测试public schema由145重建/seed为146；PostgreSQL服务未停止，无远端或生产数据库操作 |
| HTTP与后端全量门禁 | `go test ./...`；`go vet ./...`；`go build ./...` | 全仓约19.05s；Vet约10.01s；Build约18.89s | 0 / 0 / 0 | PASS：generation146消费者、批准提交点、review事务配置读取及全仓测试/静态分析/构建全绿 |
| 前端全量门禁 | 全部79个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 197 tests2.863s（墙钟4.276s）；Lint约24.7s；Build约22.6s；Type2.900s | 0 / 0 / 0 / 0 | PASS：197/197、lint/type及58页production build全绿；成功DTO、审核交互和第一方payload不变 |
| 格式、依赖与差异 | gofmt；`go mod tidy`前后SHA-256；双仓`git diff --check` | 见本节最终门禁输出 | 0 / 0 / 0 | PASS：go.mod为`695B...6728`、go.sum为`C151...D783`且tidy前后不变；双仓diff无错误（仅既有LF/CRLF提示） |
| 449台账验证 | `go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | 见本节最终门禁输出 | 1（预期）/ 0 | PASS：台账449/449，CLOSED=268、OPEN=181，High=118/Medium=191/Low=52/UNRESOLVED=88；严格模式仅因181个剩余开放项、88项待定严重度及最终严重度总数按预期失败（270 issues），`-allow-open`通过 |

外部来源管理权现在是一项有批准revision和source血缘的持久事实：拒绝只能拒绝内容，不能暗中取得人工覆盖权；批准则与内容发布同事务完成权限转换。

## 2026-08-23：BUG-035 自动镜像文件发布事件关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | generation146完整临时Schema运行`go test ./internal/httpapi -run '^TestBUG035CleanMirrorPromotionPublishesOneAtomicProjectUpdateIntegration$' -count=1 -v`，项目更新表安装拒绝trigger | 测试9.51s；包10.617s；墙钟18.542s | 1（预期） | PASS：旧提升器没有写事件，拒绝trigger未触发，mirror仍ready且留下1个文件；逐项复现直接INSERT绕过更新历史/通知/Outbox的证据 |
| 定向GREEN/原子故障注入 | 同一真实PG测试修复后重跑 | 测试9.52s；包10.578s；墙钟18.223s | 0 | PASS：事件失败时mirror保持scanning且零文件；移除trigger后一次提交ready、active、generation1及event/task/outbox各1，强制重试仍各1 |
| 文件发布/自动化广域 | 本机URL运行`Test(BUG032&#124;BUG033&#124;BUG034&#124;BUG035&#124;ProjectAutomation&#124;ProjectFile&#124;ProjectUpdate)` | 包51.633s | 0 | PASS：扫描隔离/恢复、Minecraft版本权威、日志override、镜像槽/流、手工文件和项目更新合并全部通过 |
| HTTP与后端全量门禁 | 标准`go test ./... -count=1`；`go vet ./...`；`go build ./...` | Test墙钟8.277s；Vet12.727s；Build19.665s | 0 / 0 / 0 | PASS：镜像发布事务、项目审核锁、既有全部包测试/静态分析/构建全绿 |
| 集成全仓夹具诊断 | 额外尝试`MCMODS_RUN_DB_INTEGRATION=1 go test -p 1 -parallel 1 ./...` | database包123.892s | 1（非BUG-035门禁） | 已路由：`TestStickerReferenceProjectionIntegration`在`search_path=public,pg_temp`下给临时表创建public同名索引，稳定42P07；BUG-035定向/广域真实PG均已独立通过，该既有TEST夹具问题留给对应开放台账，不改写为产品失败 |
| 前端全量门禁 | 全部79个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 197 tests3.558s（墙钟4.474s）；Lint墙钟约32.56s；Build墙钟约30.01s；Type2.982s | 0 / 0 / 0 / 0 | PASS：197/197、lint/type及58页production build全绿；无前端协议变化 |
| Schema/API/隔离边界 | 核查generation146、active/generation显式写、项目审核锁、统一publication batch及本机临时Schema | 同定向检查 | 0 | PASS：无DDL/索引/重置/回填/双写；失败连接释放即清理pg_temp；未访问远端/生产数据 |
| 格式、依赖与差异 | gofmt；`go mod tidy`前后SHA-256；双仓`git diff --check` | 见本节最终门禁输出 | 0 / 0 / 0 | PASS：go.mod为`695B...6728`、go.sum为`C151...D783`且tidy前后不变；双仓diff无错误（仅既有LF/CRLF提示） |
| 449台账验证 | `go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | 墙钟1.013s / 0.997s | 1（预期）/ 0 | PASS：台账449/449，CLOSED=269、OPEN=180，High=118/Medium=191/Low=52/UNRESOLVED=88；严格模式仅因180个剩余开放项、88项待定严重度及最终严重度总数按预期失败（269 issues），`-allow-open`通过 |

自动镜像的clean文件现在只有一个“已发布”含义：文件可见、项目更新历史和关注通知投递事实同生共死，失败不会留下ready假象。

## 2026-08-23：BUG-036 隐藏项目关注生命周期关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | generation146完整临时Schema运行`go test ./internal/httpapi -run '^TestBUG036ProjectFollowCreationAndRemovalUseSeparateVisibilityBoundariesIntegration$' -count=1 -v` | 测试9.58s；包9.699s；墙钟12.269s | 1（预期） | PASS：pending mod提交者和全局reviewer均得到200，数据库精确出现2条关注；证明内容预览权限被错误当成关注创建权限 |
| 定向GREEN/隐藏生命周期 | 同一真实PG矩阵在共享公开谓词重构后重跑 | 测试9.72s；包10.754s；墙钟18.083s | 0 | PASS：submitter/reviewer均404且零关系；公开后隐藏的关注通过status/list返回unavailable但name/url/updatedAt为空，DELETE 204并清零关系 |
| 关注消费者广域 | 本机URL运行`Test(BUG036&#124;CommunityPostReference&#124;FavoriteTargets&#124;ProjectUpdate)`；另跑BUG036/社区可见性单元边界 | 包13.015s；墙钟15.455s | 0 | PASS：社区引用批量/当前viewer、收藏动态可见性、项目更新通知和新关注生命周期全部通过；通用内容resolver仍支持合法作者/审核预览 |
| HTTP与后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...` | Test17.452s；Vet12.688s；Build13.699s | 0 / 0 / 0 | PASS：公开谓词、原子INSERT SELECT、route-only DELETE及全部后端包测试/静态分析/构建全绿 |
| 前端全量门禁 | 全部80个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 198 tests3.867s（墙钟4.713s）；Lint约30.00s；Build24.760s；Type2.663s | 0 / 0 / 0 / 0 | PASS：198/198、lint/type及58页production build全绿；隐藏占位无链接/日期，既有关系可取消，取消或404后不可重建 |
| Schema/API/隔离边界 | 核查generation146、无DDL、关系PK/FK、strict-public各route分支、占位脱敏与DELETE幂等 | 同定向检查 | 0 | PASS：无索引/重置/回填/双写；唯一协议扩展是可选unavailable，临时Schema自动清理，未触及远端/生产 |
| 格式、依赖与差异 | gofmt；`go mod tidy`前后SHA-256；双仓`git diff --check` | 见本节最终门禁输出 | 0 / 0 / 0 | PASS：go.mod为`695B...6728`、go.sum为`C151...D783`且tidy前后不变；双仓diff无错误（仅既有LF/CRLF提示） |
| 449台账验证 | `go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | 见本节最终门禁输出 | 1（预期）/ 0 | PASS：台账449/449，CLOSED=270、OPEN=179，High=118/Medium=192/Low=52/UNRESOLVED=87；严格模式仅因179个剩余开放项、87项待定严重度及最终严重度总数按预期失败（267 issues），`-allow-open`通过 |

关注现在是一项由用户拥有、可独立清理的关系：内容隐藏会移除展示信息和新建资格，但不会夺走用户取消自身关系的能力。

## 2026-08-23：BUG-037 项目更新section本地化关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | generation146完整临时Schema运行`go test ./internal/httpapi -run '^TestBUG037ProjectUpdateNotificationsLocalizeStableSectionCodesIntegration$' -count=1` | 测试9.61s；包10.617s；墙钟17.812s | 1（预期） | PASS：中文正文为`description, download_files, minecraft_versions, future_internal_code`，英文同样暴露三个snake_case/future code；template params也含原值，逐项复现审计证据 |
| 定向GREEN/跨语言fallback | 同一真实PG Worker矩阵在locale映射后重跑 | 包9.618s；墙钟12.064s | 0 | PASS：中英文各写1条通知；中文为详情介绍/下载文件/Minecraft版本/项目资料，英文为human labels/project details；source locale、params、raw changedSections和fallback指标增量2均精确 |
| 通知/项目更新广域 | 本机URL运行`Test(BUG037&#124;NotificationTemplate&#124;ProjectUpdate)`；另跑模板/section/基础设施指标单元边界 | 包12.667s；墙钟14.974s | 0 | PASS：模板配置覆盖、缺变量失败、事件合并、真实Worker投递及指标暴露共同通过 |
| HTTP与后端全量门禁 | 标准`go test ./...`；`go vet ./...`；`go build ./...` | Test10.743s；Vet3.199s；Build3.761s | 0 / 0 / 0 | PASS：全部包测试、静态分析与构建全绿 |
| 前端全量门禁 | 全部80个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 198 tests3.155s（墙钟3.281s）；Lint24.617s；Build23.880s；Type2.437s | 0 / 0 / 0 / 0 | PASS：198/198、lint/type及58页production build全绿；无第一方前端协议迁移 |
| Schema/API/隔离边界 | 核查generation146、事件/raw data稳定枚举、模板最终映射、未知fallback commit指标及临时Schema | 同定向检查 | 0 | PASS：无DDL/索引/重置/回填/双写；仅管理员指标追加字段，测试namespace自动销毁，未触及远端/生产 |
| 格式与依赖 | gofmt；`go mod tidy`前后SHA-256 | tidy及定向复核3.2s | 0 | PASS：go.mod为`695B...6728`、go.sum为`C151...D783`且tidy前后不变 |
| 差异与449台账 | 双仓`git diff --check`；`go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | 见本节最终门禁输出 | 0 / 1（预期）/ 0 | PASS：预期CLOSED=271、OPEN=178，High=118/Medium=192/Low=53/UNRESOLVED=86；严格模式只因剩余开放项/待定严重度失败，开放期模式通过 |

通知事件继续保存可供机器消费的稳定枚举，而最终用户正文只包含与实际模板locale一致的人类文本；未来未知枚举安全降级并在管理员指标中可见。

## 2026-08-23：BUG-038 自动镜像扫描拒绝状态机关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | generation146完整临时Schema运行`go test ./internal/httpapi -run '^TestBUG038RejectedMirrorFailsRunAndCanBeRescannedIntegration$' -count=1` | 测试9.60s；包10.625s；墙钟17.751s | 1（预期） | PASS：旧提升器后mirror仍scanning；下一轮`mirrorFiles`返回`existing=1/needsReview=0`且nil error，clean后结果还缺scanFailures，逐项复现永久处理中与任务伪成功 |
| 定向GREEN/受控恢复 | 同一真实PG矩阵修复后运行，并在收紧`errors.Is`、每次提升错误及ready二次pending/rejected断言后复跑 | 初版包9.627s/墙钟12.517s；最终包41.467s | 0 / 0 | PASS：遗留rejected对账为failed且运行返回可识别错误；clean后ready/active，二次pending同步为scanning/processing且awaitingScan=1，rejected为failed/rejected并再次失败，第二次clean恢复，最终existing成功 |
| 镜像/发布状态机广域 | 本机URL运行`Test(BUG0(32&#124;33&#124;35&#124;38)&#124;ProjectAutomation&#124;ProjectMirror)`；旧镜像身份夹具代数143同步为当前146后单独和整组重跑 | 单项45.763s；整组266.039s | 0 / 0 | PASS：扫描门控、版本权威、原子发布、全局镜像槽、流式上传、身份唯一和上传补偿共同通过；首次整组只暴露过期schema代数断言，不是生产逻辑失败 |
| 扫描/发布直接相关复跑 | pending/rejected完整闭环加入后运行`TestBUG0(32&#124;35&#124;38)` | 包127.451s | 0 | PASS：pending门控、clean发布事件、撤下/恢复generation与镜像scanning/failed/ready转换共同全绿 |
| HTTP与后端全量门禁 | 最终标准`go test ./...`；`go vet ./...`；`go build ./...` | Test10.150s；Vet3.240s；Build3.850s | 0 / 0 / 0 | PASS：全部包测试、静态分析与构建全绿 |
| 前端全量门禁 | 全部80个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 198 tests3.040s；Lint23.610s；Build23.880s；Type2.370s | 0 / 0 / 0 / 0 | PASS：198/198、lint/type及58页production build全绿；无第一方前端协议迁移 |
| Schema/API/隔离边界 | 核查generation146、OSS/镜像双状态转换、failed clean重验、运行结果及临时Schema | 同定向检查 | 0 | PASS：无DDL/索引/重置/回填/双写；仅内部运行详情追加计数和错误码，临时namespace自动销毁，未触及远端/生产；`DEAD-004`的run failed状态仍独立开放 |
| 格式与依赖 | gofmt；`go mod tidy`前后SHA-256 | 最终tidy0.350s | 0 | PASS：go.mod为`695B6002415469C6E658526ADD4B467D22E729B3312325293AE37D5BCD126728`、go.sum为`C15188B68ED233F9C62D4EC9A888E29F103A1D81AE6B7240AD92A38139C5D783`且tidy前后不变 |
| 差异与449台账 | 双仓`git diff --check`；不可变审计目录diff；`go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | verifier严格0.650s；开放期0.630s | 0 / 0 / 1（预期）/ 0 | PASS：台账449/449，CLOSED=272、OPEN=177，High=118/Medium=192/Low=53/UNRESOLVED=86；BUG-038原本已有High严重度，关闭不改变严重度分布；严格模式按预期264 issues，开放期模式通过 |

自动镜像现在把安全拒绝表达为显式、可审计且可恢复的失败：任务不会伪报成功；只有同一对象重新通过clean并完成当前元数据、版本和发布事务校验，才会从failed恢复为ready。

## 2026-08-23：BUG-039 自动维护权威修订发布关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | generation146完整临时Schema运行`go test ./internal/httpapi -run '^TestBUG039AutomatedMaintenancePublishesAuditedRevisionAtomicallyIntegration$' -count=1 -v` | 测试38.25s；包39.283s | 1（预期） | PASS：维护后主表为lowFrequency，但published revision仍是user/active且无父revision；published review event、audit delta、project update均为0；事件拒绝约束下discontinued仍写入且revision/activity/event计数不变，逐项复现直接主表旁路与非原子发布 |
| 定向GREEN/双存储及回滚 | 完成Mod+plugin矩阵后运行`Test(BUG039&#124;ProjectMaintenanceAutomation)` | BUG039单项49.65s；整组包64.527s | 0 | PASS：两类项目均从精确published父revision形成auto_update/lowFrequency子revision；Mod事件约束故障使status、published revision、activity、revision和event全部保持故障前值；半年/一年/恢复和人工覆盖用例全绿 |
| 审核/更新/通知广域 | 本机URL运行`Test(BUG039&#124;ProjectMaintenanceAutomation&#124;ProjectUpdateEventsMerge&#124;ProjectUpdateNotificationCount&#124;AutomaticReviewResolution&#124;ReviewCompletionNotificationFacts)` | 包71.119s | 0 | PASS：自动审核、审核完成通知、项目事件合并、百万通知计数索引路径及维护修订发布共同通过 |
| HTTP与后端全量门禁 | 标准`go test ./... -count=1`；`go vet ./...`；`go build ./...` | Test11.770s；Vet3.267s；Build3.747s | 0 / 0 / 0 | PASS：全部包测试、静态分析与构建全绿 |
| 前端全量门禁 | 全部80个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 198 tests3.040s；Lint23.452s；Build23.985s；Type2.395s | 0 / 0 / 0 / 0 | PASS：198/198、lint/type及58页production build全绿；无第一方前端协议迁移。首次Lint shell未含捆绑Node PATH、ESLint未启动；补入同一运行时后成功门禁如列 |
| Schema/API/隔离边界 | 核查generation146、published revision基线、Mod/simple快照、autobot metadata、权威审核/发布链及临时Schema | 同定向检查 | 0 | PASS：无DDL/索引/重置/回填/双写；无HTTP/DTO变化；临时namespace自动销毁，未触及远端/生产；缺published revision与待审冲突均失败关闭 |
| 格式与依赖 | gofmt；`go mod tidy`前后SHA-256 | 最终tidy0.347s | 0 | PASS：go.mod为`695B6002415469C6E658526ADD4B467D22E729B3312325293AE37D5BCD126728`、go.sum为`C15188B68ED233F9C62D4EC9A888E29F103A1D81AE6B7240AD92A38139C5D783`且tidy前后不变 |
| 差异与449台账 | 双仓`git diff --check`；外层仓库不可变审计目录diff/status；`go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | verifier严格0.771s；开放期0.666s | 0 / 0 / 1（预期）/ 0 | PASS：台账449/449，CLOSED=273、OPEN=176，High=118/Medium=192/Low=53/UNRESOLVED=86；严格模式按预期263 issues，开放期模式通过；不可变审计目录在实际跟踪它的外层仓库零diff/零status |

自动维护现在与人工批准共享同一权威发布语义：状态、修订历史、不可变操作审计、项目更新和关注通知要么一起提交，要么完全不发生。

## 2026-08-23：BUG-040 爬虫AI本地化消费者关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | generation146完整临时Schema、可控Modrinth/AI运行`go test ./internal/httpapi -run '^TestBUG040SeedCrawlerTranslationsReachDraftAndSubmittedProjectIntegration$' -count=1 -v` | 测试52.04s；包53.075s | 1（预期） | PASS：Mod的7个翻译任务和21/28 tokens全部完成，但draft localizations=1且仍含`seedTranslations`，published revision/投影=1/1；独立BUG-042约束错误被显式隔离，不遮蔽已提交项目的原文本地化事实 |
| 双类型GREEN | 映射/严格校验实现后以同一测试扩展plugin并重跑 | 测试57.36s；包57.481s | 0 | PASS：Mod与plugin各7次AI、各7个completed task及21/28 tokens；两份draft、published revision和Mod/simple本地化投影全部8语言，死字段消失；当前BUG-042状态分裂可发生但不再丢失本地化 |
| 翻译/导入/发布广域 | BUG040、Mod localization、ProjectAutomation observability、ProjectCreationAndImport authorization及SeedCrawler单元/分页source矩阵；损坏AI结果用显式`MCMODS_TEST_DATABASE_URL`复跑 | 相关主组除环境引导项均通过；AI损坏结果包0.254s | 0 | PASS：Mod本地化发布2.22s、爬虫故障可观测41.03s、24条创建/导入授权子项57.50s、AI完成结果严格读取全绿；首次组合仅旧测试回退默认5432未执行逻辑，指定本机55432后通过 |
| HTTP与后端全量门禁 | 标准`go test ./... -count=1`；`go vet ./...`；`go build ./...` | Test10.517s；Vet3.361s；Build3.776s | 0 / 0 / 0 | PASS：全部包测试、静态分析与构建全绿 |
| 前端全量门禁 | 全部80个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 198 tests3.043s；Lint23.653s；Build23.531s；Type2.366s | 0 / 0 / 0 / 0 | PASS：198/198、lint/type及58页production build全绿；草稿继续使用既有localizations编辑协议，无前端迁移 |
| Schema/API/隔离边界 | 核查generation146、source locale、strict AI result、Mod/simple validator、draft/submit同载荷及本机provider | 同定向检查 | 0 | PASS：无DDL/索引/重置/回填/双写；无公开HTTP/DTO变化；临时namespace自动销毁，测试provider/AI未出本机；BUG-042保持OPEN |
| 格式与依赖 | gofmt；`go mod tidy`前后SHA-256 | 最终tidy0.355s | 0 | PASS：go.mod为`695B6002415469C6E658526ADD4B467D22E729B3312325293AE37D5BCD126728`、go.sum为`C15188B68ED233F9C62D4EC9A888E29F103A1D81AE6B7240AD92A38139C5D783`且tidy前后不变 |
| 差异与449台账 | 双仓`git diff --check`；外层仓库不可变审计目录diff/status；`go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | verifier严格0.727s；开放期0.665s | 0 / 0 / 1（预期）/ 0 | PASS：台账449/449，CLOSED=274、OPEN=175，High=118/Medium=192/Low=53/UNRESOLVED=86；严格模式按预期262 issues，开放期模式通过；不可变审计目录零diff/零status |

爬虫的每一份合格AI结果现在都有真实消费者：它进入项目类型原生的localizations数组、通过同一业务校验，并由草稿和自动提交共享；付费但畸形的结果则明确失败且仍计入预算。

## 2026-08-23：BUG-041 爬虫候选首次/当前任务血缘关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | generation146完整临时Schema连续调用生产`upsertSeedCrawlerCandidate`两次，运行`go test ./internal/httpapi -run '^TestBUG041SeedCrawlerCandidateTracksCurrentObservationRunIntegration$' -count=1 -v` | 测试42.76s；包43.794s | 1（预期） | PASS：同一候选downloads=20、payload为`bug041-new`，但`run_id=1`，明确不是写入当前内容的第二次任务2；此前一次九位public ID夹具不合法的23514在取得行为断言前已修正，不计作有效RED |
| generation147定向GREEN | first/last必填FK、原子upsert、公开API及删除保护完成后重跑同一真实PG测试 | 测试36.39s；包36.521s | 0 | PASS：内部first/last=1/2、downloads=20、slug=`bug041-new`；详情公开ID=`bug041a01/bug041a02`；删除first run命中FK拒绝，候选血缘不孤立 |
| 上一项兼容与候选规模 | generation147下重跑BUG040完整Mod/plugin翻译发布；100k run/1M candidate生产分页/详情/计划 | BUG040包50.440s；规模包9.324s | 0 / 0 | PASS：两类8语言发布及任务/token不退化；普通/状态深页仍只取51候选，命中downloads/status-downloads索引，两个run主键连接，执行约0.737/1.124ms |
| Schema/FK与开发库边界 | generation147完整临时Schema `TestEveryForeignKeyHasLeadingIndex`；仓库`cmd/db-reset`显式绑定`127.0.0.1:55432/postgres`、development、`RESET postgres` | FK包42.037s；最终重建8.235s | 0 / 0 | PASS：两条新FK均有前导索引；仅专用本机测试public schema重建/seed为147，PostgreSQL服务未停止，无远端、生产、回填或双写 |
| HTTP与后端全量门禁 | 标准`go test ./... -count=1`；`go vet ./...`；`go build ./...` | Test11.979s；Vet3.616s；Build4.487s | 0 / 0 / 0 | PASS：generation147消费者、管理API scanner、既有全部包测试/静态分析/构建全绿 |
| 前端全量门禁 | 全部80个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 198 tests2.977s（墙钟3.586s）；Lint23.701s；Build23.996s；Type3.188s | 0 / 0 / 0 / 0 | PASS：198/198、lint/type及58页production build全绿；候选类型显式消费first/last公开任务ID。首次Lint shell缺捆绑Node PATH、ESLint未启动，补入同一运行时后取得所列成功门禁，非产品失败 |
| 格式与依赖 | gofmt；`go mod tidy`前后SHA-256 | tidy0.790s | 0 | PASS：go.mod为`695B6002415469C6E658526ADD4B467D22E729B3312325293AE37D5BCD126728`、go.sum为`C15188B68ED233F9C62D4EC9A888E29F103A1D81AE6B7240AD92A38139C5D783`且前后不变 |
| 差异与449台账 | 双仓`git diff --check`；外层仓库不可变审计目录diff/status；`go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | verifier严格0.946s；开放期0.942s | 0 / 0 / 1（预期）/ 0 | PASS：台账449/449，CLOSED=275、OPEN=174，High=118/Medium=192/Low=53/UNRESOLVED=86；严格模式按预期261 issues，开放期模式通过；不可变审计目录零diff/零status |

唯一候选现在同时回答两个不同问题：它最初由哪次任务发现，以及当前这份内容由哪次任务观察。两者都由数据库约束保存，不能再由一次覆盖写或任务删除悄悄改写历史。

## 2026-08-23：BUG-042 爬虫提交事实原子化关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | generation147完整临时Schema、真实`createSeedDraft` auto-submit，在`project_external_sources`安装拒绝trigger | 测试56.46s；包57.479s | 1（预期） | PASS：项目Handler已提交project=1，但source=0、active draft=1、submitted draft=0、candidate仍candidate；直接证明项目、来源、草稿和候选跨事务分裂 |
| 三阶段故障注入GREEN | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestBUG042SeedCrawlerSubmissionFactsCommitTogetherAndRetryIntegration$' -count=1 -v` | 扩展用例47.62s；包48.621s | 0 | PASS：Mod source绑定失败与plugin candidate完成失败都回滚全部项目内事实并保留active draft/submitting；manual draft状态写失败同时回滚draft；移除故障后同一草稿三条路径均成功 |
| BUG040/041/042联合终验 | generation148下`go test ./internal/httpapi -run 'TestBUG04(0|1|2)' -count=1 -v` | BUG040 9.65s；BUG041 9.69s；BUG042 9.95s；包29.421s | 0 | PASS：两类8语言发布、first/last任务血缘、三类原子提交/恢复共同通过；故障日志只包含测试预期P0001 |
| 创建/导入授权广域 | `TestProjectCreationAndImportEntrypointsDoNotGrantAuthorizationIntegration` | 测试10.04s；包10.164s | 0 | PASS：Mod/Modpack、8类simple project、server和8类metadata import共25个手工/导入入口通过，普通Handler无hook时行为不变且不授予权限 |
| Schema/FK/开发库 | `TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration`、`TestEveryForeignKeyHasLeadingIndex`；受保护本机`cmd/db-reset` | 14.80s / 9.47s；包24.357s；reset8.220s | 0 / 0 | PASS：临时namespace及public generation隔离、完整generation148与全FK前导索引通过；仅`127.0.0.1:55432/postgres` development public库重建/seed为148，服务保持运行 |
| 100k/1M生产分页 | `TestSeedCrawlerKeysetPagesStayBoundedAtScaleIntegration` | fixture8.671s；包9.550s | 0 | PASS：100k run/1m candidate；run、普通candidate、status candidate执行约0.032/2.212/7.192ms，均先走稳定keyset索引，submitting过滤闭集未退化查询 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy` | Test13.061s；后三项合计8.117s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、构建与模块图全绿；go.mod/go.sum SHA-256保持`695B...728`/`C151...783` |
| 前端全量门禁 | 全部80个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 198 tests3.019s（墙钟3.608s）；Lint23.785s；Build24.195s；Type2.697s | 0 / 0 / 0 / 0 | PASS：198/198、lint、58页production build与独立typecheck全绿；本项无前端DTO迁移 |
| 449台账 | `go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | 合计2.0s | 1（预期）/ 0 | PASS：audit/ledger均精确449；CLOSED=276、OPEN=173，High=118/Medium=192/Low=53/UNRESOLVED=86；strict按预期260 issues，开放期通过 |
| 差异与不可变审计 | 双仓`git diff --check`；外层仓库audit目录限定diff/status | 约1.0s | 0 / 0 / 0 | PASS：后端与前端空白错误均为0；`audit-workspace/backend/docs/audit/full-project-audit`零diff、零status，权威审计未修改 |

填充爬虫现在只会产生两种可解释结果：项目及其来源、审核草稿和候选终态全部提交，或项目事实全部回滚并留下能由后续运行恢复的草稿/候选状态。

## 2026-08-23：BUG-043 通知翻译locale注册表关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效API/Worker RED | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestBUG043NotificationTranslation' -count=1 -v` | 集成0.14s；包1.156s | 1（预期） | PASS：`EN_us`未规范化而错过en-US缓存并403；注册表外pt-BR缓存被200返回；Worker接受pt-BR，三条断言同时失败 |
| 定向GREEN | Handler/Worker共享规范化注册表后重跑同组 | 集成0.14s；包1.244s | 0 | PASS：EN_us命中canonical缓存；pt-BR在缓存/额度前400；Worker遍历全部8个启用locale、规范alias并拒绝pt-BR |
| 通知/AI广域 | system notification禁译、notification AI transactional outbox、AI task/outbox回滚、template/content locale矩阵；ARCH026损坏结果用显式`MCMODS_TEST_DATABASE_URL` | 主组除环境项0.584s；ARCH026复跑测试0.13s/包0.260s | 0 | PASS：所有产品断言通过；首次ARCH026测试按自身变量回退5432、连接前失败，绑定授权55432后健康/损坏payload/result/item及写失败矩阵全绿 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy` | Test11.370s；后三项合计7.605s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、构建和模块图全绿；generation148及go.mod/go.sum不变 |
| 前端全量门禁 | 全部80个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 198 tests3.025s（墙钟3.626s）；Lint23.510s；Build23.818s；Type2.667s | 0 / 0 / 0 / 0 | PASS：198/198、lint、58页production build与独立typecheck全绿；消息中心继续只发送typed UI locale |
| 449台账 | `go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | 合计2.0s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=277、OPEN=172；严重度118/192/53/86不变，strict按预期259 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、go.mod/go.sum SHA-256、外层audit限定diff/status | 约1.2s | 0 | PASS：双仓空白错误0；模块hash保持`695B...728`/`C151...783`；权威audit目录零diff/零status |

通知翻译的语言身份现在从请求到缓存都是同一个canonical站内code；任意字符串不再能进入额度或持久化链。

## 2026-08-23：BUG-044 通知翻译完成态错误证据映射关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 审计baseline逐字复核 | `git show bd50afd0...:internal/httpapi/notification_handlers.go`及`ai_translation.go` | 约1.0s | 0 | PASS：旧GET两次忽略JSON、忽略cache upsert并返回translation；旧Worker persistence为void且吞解析/写错误，与BUG-044完全同源 |
| 已存有效RED | ARCH-026在同函数加入真实PG/source失败测试 | 包1.165s；墙钟8.6s | 1（预期） | PASS：旧catalog/notification损坏completed均200；completed UPDATE早于业务写，void notification persistence和scope JSON继续吞错 |
| 已存定向GREEN | ARCH-026 `TestAITranslationsPersistValidatedBusinessResultsBeforeCompletion`与`TestCatalogTranslationResultRejectsCorruptCompletedTasksIntegration` | 包1.179s；墙钟8.7s | 0 | PASS：notification健康、坏payload/result/item、cache upsert/坏列、failed状态/XX000、GET时间戳不变与完成CAS顺序全绿 |
| 当前generation148复验 | 显式`MCMODS_TEST_DATABASE_URL=postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable`重跑真实PG矩阵；同轮notification/outbox/template/locale广域 | 用例0.13s；包0.260s | 0 | PASS：所有健康、损坏、owner和数据库故障分支仍严格；BUG-043新增locale边界不破坏ARCH-026完成协议 |
| 当前全量门禁复用 | BUG-043后无额外产品代码：全仓Test/Vet/Build/tidy；前端198 tests/Lint/Build/Type；模块hash与双仓diff | 后端11.370s+7.605s；前端见上一节 | 0 | PASS：当前工作树全部门禁全绿；本项为已实现根因的一对一审计映射，不制造冗余代码 |
| 449台账 | `go run ./tools/remediation/verify_findings`；同命令加`-allow-open` | 合计1.9s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=278、OPEN=171；严重度118/192/53/86不变，strict按预期258 issues，开放期通过 |

通知翻译的completed状态已经在ARCH-026时取得严格含义；BUG-044现在补齐同一事实在逐项台账中的明确归属。

## 2026-08-23：BUG-045 私聊after锚点语义关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 测试夹具校正 | 初版BUG045测试引用不存在的共享`containsAll` helper | 编译前约2.6s | 1 | 仅测试编译错误，产品代码未执行，不计有效RED；改用标准`strings.Contains`后再取行为证据 |
| 有效Handler真实PG RED | 临时真实PG调用`conversationMessages`，分别传畸形、未知及其他会话after，并检查read_at | 用例0.12s；包0.252s | 1（预期） | PASS：三类均200空items且当前会话未读由2变0；合法anchor返回下一条并按既有语义清零 |
| 定向GREEN与空成功控制 | `TestBUG045DirectMessageAfterAnchorMustBelongToConversationIntegration`及cursor/SQL/source合同 | BUG045用例0.12s；组合包1.145s | 0 | PASS：三类坏anchor均400/未读2；合法anchor返回下一条，合法latest明确200空items，两类合法请求才标记已读 |
| 100k会话/1M消息 | 扩展`TestDirectMessagePagesStayBoundedAtOneMillionMessagesIntegration`覆盖empty valid/cross anchor两查询预算及EXPLAIN | fixture9.449s；包10.504s | 0 | PASS：历史/增量无重漏；非空after仍单页查询；两类空after固定2 SQL，验证走public ID唯一索引0.015ms，无direct_messages Seq Scan |
| 消息广域 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '(Message&#124;Conversation&#124;Realtime)' -count=1` | 包9.779s | 0 | PASS：会话/消息分页、发送/离线邮件Outbox、presence、实时失效和读状态回归全绿 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy` | Test11.816s；后三项合计7.750s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、构建和模块图全绿；无Schema/generation变化 |
| 前端全量门禁 | 全部80个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 198 tests2.952s（墙钟3.519s）；Lint23.769s；Build24.095s；Type2.705s | 0 / 0 / 0 / 0 | PASS：198/198、lint、58页production build和独立typecheck全绿；历史cursor与多页after排空consumer不变 |
| 严重度与449台账 | 逐证据定Medium/P1；`go run ./tools/remediation/verify_findings`及`-allow-open` | 合计2.0s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=279、OPEN=170；Medium=193、UNRESOLVED=85（High118/Low53不变），strict按预期256 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 约1.0s | 0 | PASS：双仓空白错误0；go.mod/go.sum保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

私聊现在能明确区分“合法锚点之后没有新消息”和“锚点无效”，且任何无效分页请求都不会先改变未读事实。

## 2026-08-23：BUG-046 本机NATS实时回声关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双实例NATS RED | 官方`nats-server/v2`内嵌broker、两个真实queue Client、两个Server/Hub；第一实例调用`publishRealtimeUser` | 包1.268s | 1（预期） | PASS：来源Hub先收到本机事件，随后又收到ID相同的NATS回声；peer收到一次，精确复现审计证据 |
| 扩展定向GREEN | `TestBUG046RealtimeBroadcastDeliversOncePerInstance`双向发布、identity/origin断言、无额外事件及origin-less兼容控制 | 用例0.93s；包2.161s | 0 | PASS：两方向均为每实例恰好一次、同ID/同非空origin且两Server origin不同；后台/旧版无origin广播仍在两实例各投递一次 |
| Realtime/Queue广域 | `go test ./internal/httpapi ./internal/queue -run '(BUG046&#124;Realtime&#124;Broadcast&#124;Reconfigure)' -count=1 -v` | httpapi1.091s；queue0.098s | 0 | PASS：SSE预算、通知keyset、运行时重配置订阅和disabled注册合同全绿；10M显式规模用例按环境约定跳过 |
| 后端全量门禁 | tidy稳定模块图后`go test ./... -count=1`；`go vet ./...`；`go build ./...`；第二次`go mod tidy`哈希控制 | Test/Vet/Build合计12.493s；tidy0.806s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、串行构建和稳定模块图全绿；go.mod/go.sum保持`695B...728`/`C151...783` |
| 前端全量门禁 | 全部80个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 198 tests2.957s（墙钟3.544s）；Lint23.507s；Build23.834s；Type2.657s | 0 / 0 / 0 / 0 | PASS：198/198、lint、58页production build及独立typecheck全绿；公开SSE/consumer协议没有变化 |
| 严重度与449台账 | 逐证据定Medium/P1；`go run ./tools/remediation/verify_findings`及`-allow-open` | 合计1.739s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=280、OPEN=169；Medium=194、UNRESOLVED=84（High118/Low53不变），strict按预期254 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 1.322s | 0 | PASS：双仓空白错误0；go.mod/go.sum保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

实时事件现在在来源实例和每个peer各投递一次；滚动升级及无本机预投递的后台广播仍保持兼容。

## 2026-08-23：BUG-047 私聊会话UI请求隔离关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效当前源码RED | `node --test app/_lib/message-conversation-isolation.test.mts`要求同动作选择清空、两类取消和conversation/version守卫 | 用例合计0.003s；进程0.115s；墙钟0.514s | 1（预期） | PASS：2/2失败；当前无统一select函数、AbortController或signal，直接setter后仍由下一effect timer清空，精确保留B标题+A正文窗口 |
| 定向GREEN | 新隔离2项+`direct-message-pagination`2项+`realtime-query-cache`4项 | 8项0.189s；墙钟0.571s | 0 | PASS：同步清空/取消/三重提交守卫通过；历史cursor、多页after、精确实时失效和in-flight协调无回归 |
| 前端全量门禁 | 全部81个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 200 tests3.004s（墙钟3.610s）；Lint+预检Type26.380s；Build+后置Type26.055s | 0 / 0 / 0 / 0 | PASS：200/200、lint、58页production build与独立typecheck全绿 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；哈希控制`go mod tidy` | 合计12.954s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、串行构建、稳定模块图全绿；消息服务协议未改 |
| 严重度与449台账 | 逐证据定High/P1；`go run ./tools/remediation/verify_findings`及`-allow-open` | 合计1.753s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=281、OPEN=168；High=119、UNRESOLVED=83（Medium194/Low53不变），strict按预期252 issues，开放期通过 |
| 格式/依赖/不可变边界 | 双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 1.296s | 0 | PASS：双仓空白错误0；go.mod/go.sum保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

私聊会话身份现在从选择动作到每次异步提交都保持绑定，迟到的A正文不能再出现在B的标题和发送目标下。

## 2026-08-23：BUG-048 通知AI余额加载生命周期关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效当前源码RED | `node --test app/_lib/ai-balance-load-state.test.mts`要求首次条件读取与queued任务finally刷新 | 用例合计0.001s；进程0.114s；墙钟0.512s | 1（预期） | PASS：2/2失败；旧代码没有可翻译项effect/token snapshot，余额只在翻译成功try末尾加载 |
| 定向GREEN与React规则 | AI余额2项、BUG047隔离2项、通知分页1项；随后ESLint/TypeScript | 5项0.183s；组合墙钟26.345s | 0 / 0 / 0 | PASS：首次加载、账户绑定、effect取消、queued success/failure刷新全绿；React refs/set-state-in-effect规则与类型通过 |
| 前端全量门禁 | 全部82个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 202 tests3.238s（墙钟3.870s）；Lint/Type见定向；Build+后置Type26.388s | 0 / 0 / 0 / 0 | PASS：202/202、lint、58页production build与独立typecheck全绿 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；哈希控制`go mod tidy` | 合计13.825s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、串行构建及稳定模块图全绿；余额与任务服务协议未改 |
| 严重度与449台账 | 逐证据定Medium/P1；`go run ./tools/remediation/verify_findings`及`-allow-open` | 合计1.740s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=282、OPEN=167；Medium=195、UNRESOLVED=82（High119/Low53不变），strict按预期250 issues，开放期通过 |
| 格式/依赖/不可变边界 | 双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 1.306s | 0 | PASS：双仓空白错误0；go.mod/go.sum保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

用户现在会在首次可翻译通知出现时先看到当前账户额度；新任务的预留和消耗在已观察终态后统一对账。

## 2026-08-23：BUG-049 Next页面标题与品牌所有权关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效当前源码RED | `node --test app/_lib/site-brand-metadata.test.mts`要求root模板及无客户端head所有权 | 用例合计0.001s；进程0.119s；墙钟0.496s | 1（预期） | PASS：2/2失败；根只有固定title，Provider含pathname/document.title/MutationObserver且无router.refresh |
| 定向GREEN | 品牌metadata2项+deployment/CSP/route9项 | 11项0.213s；墙钟0.596s | 0 | PASS：default/template、品牌规范/回退、无head观察器、框架refresh及生产URL/CSP边界全绿 |
| 前端全量门禁 | 全部83个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 204 tests3.037s（墙钟3.611s）；Lint+Type26.477s；Build+后置Type26.458s | 0 / 0 / 0 / 0 | PASS：204/204、lint、58页production build与独立typecheck全绿；metadata fetch故障安全回退未阻断构建 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；哈希控制`go mod tidy` | 合计13.002s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、串行构建及稳定模块图全绿；站点配置API未改 |
| 台账格式校正 | 首次BUG049状态行在单元格正文写入literal pipe，Markdown被拆成额外列 | 约1.7s | 1 | 非产品/测试失败：verifier拒绝CLOSED结果列；移除单元格pipe后再执行最终验证 |
| 严重度与449台账 | 逐证据定Low/P2；`go run ./tools/remediation/verify_findings`及`-allow-open` | 最终合计1.716s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=283、OPEN=166；Low=54、UNRESOLVED=81（High119/Medium195不变），strict按预期248 issues，开放期通过 |
| 格式/依赖/不可变边界 | 双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 1.317s | 0 | PASS：双仓空白错误0；go.mod/go.sum保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

页面标题现在由Next metadata树稳定拥有；站点品牌只作为根模板组合，并通过框架刷新边界更新。

## 2026-08-23：BUG-050 邮件运行时Enabled权威关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | 加密保存`enabled:false`及完整SMTP字段，再读取配置并构造Server/Worker activeMailer | 用例1.331s；墙钟9.019s | 1（预期） | PASS：读取、Server和Worker三项断言全部失败，旧实现均从Host/From重算或丢弃false |
| 定向GREEN | BUG050真实PG；Mailer显式关闭短路/启用完整性；config环境false/true覆盖 | integration0.09s/包2.181s；mailer1.356s；config0.659s | 0 / 0 / 0 | PASS：持久false贯穿读取和两类runtime；关闭在拨号前拒绝；启用仍要求完整配置，环境显式值优先 |
| 邮件/通知广域 | `go test ./internal/httpapi ./internal/mailer ./internal/config -run '(BUG050&#124;Mail&#124;Email&#124;Notification)' -count=1` | httpapi0.725s；mailer0.068s；config0.090s | 0 | PASS：验证码、离线私聊邮件、系统通知、模板邮件、后台配置及配置加载相关回归全绿 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | 墙钟25.075s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、构建与稳定模块图全绿；generation148不变，go.mod/go.sum哈希不变 |
| 前端全量门禁 | 全部83个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 204 tests3.069s；Test+Lint墙钟27.4s；Build+Type墙钟26.5s | 0 / 0 / 0 / 0 | PASS：204/204、lint、58页production build及独立typecheck全绿；后台既有enabled DTO无需前端迁移 |
| 严重度与449台账 | 逐证据定High/P1；`go run ./tools/remediation/verify_findings`及`-allow-open` | 合计1.9s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=284、OPEN=165；High=120、UNRESOLVED=80（Medium195/Low54不变），strict按预期246 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 1.5s | 0 | PASS：双仓空白错误0；go.mod/go.sum保持`695B...728`/`C151...783`；权威audit目录零diff、零status；仅Git换行提示 |

管理员保存关闭后，邮件开关现在从加密设置一直保持到Server与Worker的最终发送门禁；完整SMTP字段不再偷偷重新启用出站邮件。

## 2026-08-23：BUG-051 Realtime SSE访问日志Writer关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效完整链RED | 真实Realtime handler经过security/CORS/cookie/Yggdrasil/compression/log/bot链；另测四类可选writer接口 | 包1.265s；墙钟9.4s | 1（预期） | PASS：完整链返回501 `streaming is unavailable`；Flusher/Hijacker/Pusher/ReaderFrom全部断言失败且零底层调用/计数 |
| 定向GREEN | `go test ./internal/httpapi -run '^TestBUG051' -count=1 -v` | 包1.229s；墙钟9.4s | 0 | PASS：SSE返回200 text/event-stream并实际flush connected帧；四接口到达底层，ReaderFrom记录200及8字节 |
| Realtime/日志广域 | Realtime、AccessLog、Compression、Middleware及Querycache组合；BUG046官方NATS双实例 | httpapi1.070s；querycache0.095s | 0 | PASS：SSE预算、NATS实例去重、压缩、读取采样、队列溢出与异步批量写合同全部通过；10M显式规模按约定未运行 |
| 真实PG访问日志控制 | `MCMODS_RUN_DB_INTEGRATION=1`及显式本机55432运行锁表集成 | 用例0.53s；包0.657s | 0 | PASS：`app_logs`排他锁期间64请求在513µs返回，释放后4个批statement落库；新增writer方法未把同步等待带回请求链 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | 墙钟17.942s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、构建和稳定模块图全绿；generation148及go.mod/go.sum不变 |
| 前端全量门禁 | 全部83个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 204 tests3.047s；Test+Lint26.671s；Build+Type25.921s | 0 / 0 / 0 / 0 | PASS：204/204、lint、58页production build及独立typecheck全绿；EventSource公开合同不变 |
| 严重度与449台账 | 逐证据定Medium/P1；`go run ./tools/remediation/verify_findings`及`-allow-open` | 合计1.293s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=285、OPEN=164；Medium=196、UNRESOLVED=79（High120/Low54不变），strict按预期244 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 0.986s | 0 | PASS：双仓空白错误0；go.mod/go.sum保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

访问日志现在能观察流式请求而不破坏流式协议；登录用户的Realtime连接可穿过完整生产中间件链并立即收到首帧。

## 2026-08-23：BUG-052 专用日志保留自动调度关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效当前源码RED | runtime启动合同要求独立Worker、立即/周期prune、PG租约、策略读取和既有批量语句 | 包1.243s；墙钟9.4s | 1（预期） | PASS：当前不存在`log_retention_worker.go`，启动链没有任何自动日志保留执行者，精确复现唯一调用在PUT的问题 |
| 启动合同GREEN | 新Worker与runtime注册后重跑源码合同 | 包1.207s；墙钟9.3s | 0 | PASS：启动即prune、10分钟ticker、advisory lease、`logs.retention`读取与PERF033语句复用均锁定 |
| PG夹具口径校正 | 初版策略只写defaultDays=1，未显式覆盖normalize补入的类别默认90–1095天 | 包1.354s | 1 | 非产品失败：10天旧行按真实类别期限应保留；夹具改为明确设置五个测试类别为1天后再取行为结果 |
| 真实PG GREEN | 1005条旧API日志、四张专用表新旧行、enabled false、第二会话持锁/释放 | 用例0.19s；包1.444s | 0 | PASS：跨批精确删除1005+4条且五张新行保留；false零删除；另一实例租约使零删除，释放后积压清除 |
| 日志/保留广域与规模 | 日志分页/FTS/清理、访问日志队列、活动保留隔离及runtime；显式PG运行1.3M规模 | httpapi6.992s；app1.070s | 0 | PASS：1.3M临时日志4.431s装载，搜索页无重叠且单语句仍精确1000行；64个锁表请求551.3µs返回、4批落库 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | 墙钟14.726s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、构建和稳定模块图全绿；generation148及go.mod/go.sum不变 |
| 前端全量门禁 | 全部83个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 204 tests3.042s；Test+Lint26.8s；Build+Type25.767s | 0 / 0 / 0 / 0 | PASS：204/204、lint、58页production build及独立typecheck全绿；后台配置DTO未改 |
| 严重度与449台账 | 逐证据定Medium/P1；`go run ./tools/remediation/verify_findings`及`-allow-open` | 合计1.361s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=286、OPEN=163；Medium=197、UNRESOLVED=78（High120/Low54不变），strict按预期242 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 1.0s | 0 | PASS：双仓空白错误0；go.mod/go.sum保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

专用日志保留策略现在由应用生命周期持续执行；管理员不再需要反复保存配置才能让声明的期限生效。

## 2026-08-23：BUG-053 日志读取与清理故障可观察性关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 已实现读取证据复核 | 当前`adminLogs/loadLogPage`及ARCH-013 `collectSimpleRows`；Query/Values/rows.Err失败合同 | 约1.0s | 0 | PASS：当前日志入口不再使用旧伪空helper；缺列投影真实请求500，Values/terminal stub返回nil/error并关闭rows |
| 有效真实PG清理RED | temp五表及statement trigger拒绝permission DELETE，调用真实`updateLogConfig` | 用例0.11s；包1.354s；墙钟9.4s | 1（预期） | PASS：旧实现仍200；四个成功类别有计数而permission_change只从map消失，精确证明失败被伪装成0/成功 |
| 定向GREEN | 同一读取与清理故障注入复跑 | 用例0.13s；包1.333s | 0 | PASS：读取500；清理返回LOG_CLEANUP_FAILED 500、失败类别和四个成功计数，失败行保留且enabled策略持久，SQL错误仅服务端记录 |
| 日志/可观察性广域 | BUG051/052、AccessLog、日志分页/清理、ARCH013 collector及显式PG 1.3M规模 | httpapi6.024s | 0 | PASS：1.3M装载4.370s、规模用例5.07s且单清理仍1000；64锁表请求504.9µs返回，SSE/Worker/分页/故障合同全绿 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | 墙钟18.397s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、构建和稳定模块图全绿；generation148及go.mod/go.sum不变 |
| 前端全量门禁 | 全部83个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 204 tests3.066s；Test+Lint27.065s；Build+Type26.269s | 0 / 0 / 0 / 0 | PASS：204/204、lint、58页production build及独立typecheck全绿；既有异常分支兼容新500合同 |
| 严重度与449台账 | 逐证据定Medium/P1；`go run ./tools/remediation/verify_findings`及`-allow-open` | 合计1.356s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=287、OPEN=162；Medium=198、UNRESOLVED=77（High120/Low54不变），strict按预期240 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 1.009s | 0 | PASS：双仓空白错误0；go.mod/go.sum保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

管理员日志现在只在完整读取成功时返回；保留策略清理若部分失败，会明确区分已保存配置、成功删除和失败类别。

## 2026-08-23：BUG-054 结构化运行日志统一管线关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效当前行为RED | 真实全局Install后发送slog Info含`error=none`、Warn group及legacy log | 用例0.02s；包1.150s；墙钟2.9s | 1（预期） | PASS：Go1.26默认桥使slog行偶然进入Store，但Info因属性文本被inferLevel误判Error，证明无权威结构化级别管线 |
| 定向GREEN | 显式TextHandler/MultiWriter、level/time解析及legacy bridge后运行全部runtimelog测试 | BUG054包1.146s；最终全包1.116s | 0 | PASS：Info属性保持Info、Warn group完整、legacy info与failed→Error同一格式；ring覆盖和partial组合无回归 |
| 结构化生产者广域 | RuntimeLog、Outbox、Notification、Unread及Queue定向组合 | runtimelog0.101s；httpapi2.127s；queue0.096s | 0 | PASS：相关事务/模板/分页/队列合同全绿；测试故障实际输出`level=ERROR`及结构化recipient/template/error属性 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | 墙钟18.742s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、构建和稳定模块图全绿；generation148及go.mod/go.sum不变 |
| 前端全量门禁 | 全部83个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 204 tests3.081s；Test+Lint27.11s；Build+Type26.343s | 0 / 0 / 0 / 0 | PASS：204/204、lint、58页production build及独立typecheck全绿；runtime Entry DTO不变 |
| 严重度与449台账 | 逐证据定Medium/P1；`go run ./tools/remediation/verify_findings`及`-allow-open` | 合计1.430s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=288、OPEN=161；Medium=199、UNRESOLVED=76（High120/Low54不变），strict按预期238 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 1.288s | 0 | PASS：双仓空白错误0；go.mod/go.sum保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

结构化与旧式运行日志现在经过同一显式管线输出到终端和后台有界视图，级别、字段和时间不再依赖偶然桥接或文本猜测。

## 2026-08-23：BUG-055 PostgreSQL UTC日桶权威关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | DATABASE_URL注入Pacific/Kiritimati，调用真实Connect/ConnectActivity并读取TimeZone及固定23:30Z的date | 用例0.13s；包1.014s；墙钟3.268s | 1（预期） | PASS：application/activity均保留Kiritimati，固定UTC瞬间落入2026-08-24而非UTC日2026-08-23 |
| 定向GREEN | 同一冲突URL和两池真实PG测试 | 用例0.12s；包1.008s；墙钟3.219s | 0 | PASS：两池均强制UTC且固定瞬间回到2026-08-23，显式URL参数不能覆盖权威合同 |
| 日桶/连接广域 | database/httpapi/activity/userstats/app及全部cmd；生产源码连接入口检索 | 墙钟14.045s | 0 | PASS：相关包与命令全绿；业务主池、activity池、db-reset和user-statistics统一工厂，只读load-observer无业务写入 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | 墙钟17.599s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、构建和稳定模块图全绿；generation148及go.mod/go.sum不变 |
| 前端全量门禁 | 全部83个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 204 tests4.371s；Lint23.536s；Build+Type26.249s | 0 / 0 / 0 / 0 | PASS：204/204、lint、58页production build及独立typecheck全绿；无前端或HTTP合同变化 |
| 门禁环境校正 | 首次组合命令未把bundled Node目录加入pnpm子进程PATH，测试已204/204后lint启动失败 | 墙钟5.5s | 1 | 非产品/测试失败：补入同一workspace runtime PATH后ESLint通过，无源码调整 |
| 严重度与449台账 | 逐证据定Medium/P1；`go run ./tools/remediation/verify_findings`及`-allow-open` | 合计1.340s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=289、OPEN=160；Medium=200、UNRESOLVED=75（High120/Low54不变），strict按预期236 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 0.990s | 0 | PASS：双仓空白错误0；go.mod/go.sum保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

所有受支持的生产数据库连接现在共享UTC日界；部署时区或连接URL不能再把同一事件拆到活动、浏览、当前计数和热度的不同日期。

## 2026-08-23：BUG-057 / MAP-007 热度有效开发者集合关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | 完整临时generation148 Schema建立同项目两developer与一outsider，依次写view/favorite/comment/rating/dimension | 用例9.68s；包10.593s；墙钟12.811s | 1（预期） | PASS：旧scalar只排除最小ID；incremental事实均为2、rating 2/9、trend 8/6/4，而权威集合应为1、1/5和4/3/2 |
| PG夹具校正 | 初版带参数的多statement使用extended protocol；GREEN首次双rebuild同样组合statement且临时函数调用未显式pg_temp/cast | 合计约36s | 1 / 1 / 1 | 非产品/测试失败：拆为单statement并按现有临时Schema合同限定pg_temp/bigint后，同一产品实现无需修改即进入有效RED/GREEN |
| 定向GREEN | 两developer完整incremental、trend、dimension、global facts及故意损坏后的route/global rebuild；Schema/HTTP映射合同 | database包10.722s、httpapi0.946s；墙钟13.463s | 0 | PASS：incremental与两种rebuild都只保留outsider 1/5和4/3/2；scalar/min/limit/重复UNION生产零残留 |
| 10M集合校准 | `TestGlobalRatingSetRebuildAtScaleIntegration`在同route配置两个developer，100k/1M/10M EXPLAIN ANALYZE | 用例33.00s；包34.047s | 0 | PASS：三档精确排除1%全部developer评分；10M排除100k，rebuild 6.619s且无owner逐行函数，低于15s门 |
| generation149本机重置 | 明确development确认后仅重置127.0.0.1:55432/postgres并安装当前Schema/seed；随后真实Migrate/refresh | reset8.486s；验证用例0.11s | 0 / 0 | PASS：本机一次性测试数据被可重建seed替换，generation149可连接/迁移/刷新；无远端数据库写入 |
| 热度/评分广域 | database/httpapi的Popularity、Rating、ContentMetric及ProjectAccess组合 | 墙钟2.708s | 0 | PASS：增量事实、全局累计、admin/public metrics、开发者展示和有效权限合同全绿 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | 墙钟24.959s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、构建和稳定模块图全绿；generation149，go.mod/go.sum哈希不变 |
| 前端全量门禁 | 全部83个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 204 tests4.335s；Test+Lint31.977s；Build+Type26.581s | 0 / 0 / 0 / 0 | PASS：204/204、lint、58页production build及独立typecheck全绿；统计/开发者DTO不变 |
| 严重度与449台账 | BUG-057和MAP-007逐证据定Medium/P1；`go run ./tools/remediation/verify_findings`及`-allow-open` | 合计1.414s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=291、OPEN=158；Medium=202、UNRESOLVED=73（High120/Low54不变），strict按预期232 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 1.116s | 0 | PASS：双仓空白错误0；go.mod/go.sum保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

热度现在排除项目的完整有效developer集合，展示也直接读取同一关系；最小用户ID不再决定谁能影响自己的项目指标。

## 2026-08-23：BUG-058 搜索重建跨实例代次关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | 隔离Schema建立持久queue及creator最小投影；代表实例A持目标exclusive advisory lock，实例B调用真实drain | 用例0.35s；包1.272s；墙钟3.108s | 1（预期） | PASS：旧B无视重建代次，在lease内claim、调用Typesense delete并删除queue，精确复现并发增量被旧alias消费的根因 |
| 双Worker定向GREEN | A通过生产`withSearchProjectionLease(false)`持独占lease；B通过生产`drain`请求共享lease，观察后释放A | 用例0.48s；包1.403s；墙钟3.270s | 0 | PASS：250ms内attempts=0、Typesense调用=0；A安全解锁后B恢复并恰好一次delete/ack，queue=0 |
| 搜索重建广域 | BUG058双Worker、项目批量预聚合及100k/1M稳定keyset真实PG组合；默认searchindex全包及源码合同 | 用例组2.49s；包3.620s；墙钟5.567s | 0 | PASS：双Worker、500行页、100k/1M遍历、主键计划、SQL输入/JSONL预算与进度合同全绿；获独占锁后二次state检查和同连接执行已锁定 |
| 全数据库尝试与隔离 | 首次并行`MCMODS_RUN_DB_INTEGRATION=1 go test ./...`；随后`-p 1`完整跑至结束 | 并行约2m后终止自有孤儿进程；串行约13m | 1 / 1 | 非BUG058失败：并行临时全Schema耗尽PG shared-lock表；串行searchindex包2.924s通过，但database仍命中已知sticker临时索引42P07，httpapi存在测试间环境变量/全Schema规模干扰并触发10m包超时；这些开放门禁证据不冒充本项失败或最终全绿 |
| 后端普通发布门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | Test墙钟9.105s；Vet+Build+tidy7.881s | 0 / 0 / 0 / 0 | PASS：全部默认包、静态分析、构建与稳定模块图全绿；generation149，go.mod/go.sum保持`695B...728`/`C151...783` |
| Race门禁环境 | `go test -race`定向BUG058；检查CGO/本机编译器 | 0.54s | 1 | 环境阻塞：当前Go配置CGO_ENABLED=0且PATH/工作区无gcc或clang，Go在编译测试前拒绝`-race`；未声称通过，最终TEST/CI根因组必须提供Windows支持工具链或等价受支持runner |
| 前端全量门禁 | 全部83个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 204 tests2.964s；Test+Lint墙钟26.996s；Build+Type27.888s | 0 / 0 / 0 / 0 | PASS：204/204、lint、58页production build及独立typecheck全绿；搜索HTTP/DTO无前端变化 |
| 严重度与449台账 | 逐证据定High/P1；`go run ./tools/remediation/verify_findings`及`-allow-open` | strict/allow墙钟均约1.08s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=292、OPEN=157；High=120、UNRESOLVED=73（Medium202/Low54不变），strict按预期231 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 见本节最终复核 | 0 | PASS：双仓空白错误0；模块哈希不变；权威audit目录零diff、零status |

搜索全量快照和增量消费现在共享数据库级代次：其他实例不能在alias切换前把并发事件写入旧集合并从持久队列删除，期间积压会在新alias激活后正常重放。

## 2026-08-23：BUG-061 gzip质量参数失败关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效可压缩RED | 新12分支text/plain middleware测试覆盖非法/空/越界q、wildcard及显式优先级 | 用例0.00s；包1.246s；墙钟9.135s | 1（预期） | PASS：旧实现6项错误压缩：abc、空、1.001、非法wildcard及两种`*`/gzip;q=0顺序，精确复现原证据并暴露顺序根因 |
| 定向GREEN | `go test ./internal/httpapi -run '^TestCompression' -count=1 -v` | 包1.399s；墙钟9.147s | 0 | PASS：12/12协商分支、既有JSON gzip解压及binary/q=0跳过全绿；合法默认、0.25和wildcard保持压缩 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | Test墙钟12.343s；Vet+Build+tidy10.652s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、构建和稳定模块图全绿；generation149，go.mod/go.sum哈希不变 |
| 前端全量门禁 | 全部83个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 204 tests4.090s；Test+Lint墙钟29.356s；Build+Type27.738s | 0 / 0 / 0 / 0 | PASS：204/204、lint、58页production build及独立typecheck全绿；无前端/DTO变化 |
| 严重度与449台账 | 逐证据保持Low/P2；`go run ./tools/remediation/verify_findings`及`-allow-open` | 见最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=293、OPEN=156；Low=54、UNRESOLVED=73（High120/Medium202不变），strict按预期230 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 见最终复核 | 0 | PASS：双仓空白错误0；模块哈希不变；权威audit目录零diff、零status |

非法或明确拒绝gzip的请求现在稳定回退identity，合法的显式和wildcard协商继续获得压缩，header顺序不再改变结果。

## 2026-08-23：BUG-062 空库兼容范围失败关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效兼容语义RED | 新默认范围及全局mod-content回退纯单元测试 | 包1.247s；墙钟8.582s | 1（预期） | PASS：旧Fabric默认宣称33个未经验证版本；Forge只配置1.20.1时仍被扩张加入24w14potato，精确复现并覆盖第二入口 |
| 定向单元GREEN | `go test ./internal/httpapi -run 'Test(DefaultMinecraftLoaderCompatibilityIsUnknownUntilVerified&#124;GlobalModContentCompatibilityPreservesVerifiedLoaderRanges)$' -count=1` | 包1.222s；墙钟8.649s | 0 | PASS：默认13 loader全部显式空范围；Forge只保留1.20.1，Babric为非nil空范围 |
| 空库真实PG/API | `MCMODS_RUN_DB_INTEGRATION=1 DATABASE_URL=postgres://...:55432/postgres go test ./internal/httpapi -run 'Test(MinecraftVersionConfigurationDistinguishesMissingFromBrokenIntegration&#124;DefaultMinecraftLoaderCompatibilityIsUnknownUntilVerified&#124;GlobalModContentCompatibilityPreservesVerifiedLoaderRanges)$' -count=1 -v` | 包1.481s；墙钟8.831s | 0 | PASS：临时空`system_settings`内部加载及公开HTTP JSON均返回13个`versions:[]`；损坏shape/normalize/Schema仍失败可见 |
| 前端旁路RED/GREEN | `node --test app/_lib/minecraft-version-selection.test.mts`；随后ESLint/Type | RED 3/4、118ms、墙钟0.217s；GREEN 4/4、116ms；含Lint/Type墙钟26.622s | 1（预期）/ 0 / 0 / 0 | PASS：旧workspace把顶层全版本赋给每个loader；修复后源码合同锁定`[...loader.versions]`并禁止`allMinecraftVersions`回退 |
| 受影响包 | `go test ./internal/httpapi -count=1` | 包2.333s；墙钟4.748s | 0 | PASS：全部默认httpapi测试通过 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | Test墙钟12.222s；Vet+Build+tidy10.035s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、构建和稳定模块图全绿；generation149，go.mod/go.sum保持`695B...728`/`C151...783` |
| 前端全量门禁 | 全部83个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 当前205 tests3.088s；Lint/Type及58页production build | 0 / 0 / 0 / 0 | PASS：205/205、lint、58页production build及独立typecheck全绿；workspace与后端共享逐loader空/验证范围语义 |
| 严重度与449台账 | 逐证据保持High/P1；`go run ./tools/remediation/verify_findings`及`-allow-open` | 见最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=294、OPEN=155；High=120、UNRESOLVED=73（Medium202/Low54不变），strict按预期229 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 见本节最终复核 | 0 | PASS：双仓空白错误0；模块哈希不变；权威audit目录零diff、零status |

空库现在仍能展示目录，但不会声称任一loader支持任一版本；持久化验证范围也不会在模组内容回退中被扩张。

## 2026-08-23：BUG-063 NeoForge artifact版本映射关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效映射/通道RED | prefix表、alpha/beta/rc选择及modern+legacy双Maven同步模拟 | 包1.272s；墙钟8.869s | 1（预期） | PASS：旧1.21=`21.21.`、26.2=`2.2.`且4个同步目标漏2个；47.7.0-rc错误压过47.4.0，逐字复现审计两类证据 |
| 定向GREEN | `go test ./internal/httpapi -run 'Test(SelectLoaderArtifactVersionIsStableAndDeterministic&#124;NeoForgeArtifactPrefix&#124;SynchronizedNeoForgeArtifactsCoverBothMinecraftReleaseSchemes)$' -count=1 -v` | 包1.225s；墙钟8.561s | 0 | PASS：1.20.1/1.21/1.21.1/26.2分别得到47.1.106/21.0.168/21.1.241/26.2.0.61，alpha/beta/rc拒绝；非法/pre-release形态empty/error |
| 受影响包 | `go test ./internal/httpapi -count=1` | 包2.291s；墙钟4.794s | 0 | PASS：全部默认httpapi测试通过 |
| 真实PG artifact权威 | `MCMODS_TEST_DATABASE_URL=postgres://...:55432/postgres go test ./internal/httpapi -run '^TestMinecraftLoaderArtifactSnapshotsAreCatalogBoundAndOfflineReadable$' -count=1 -v` | 包0.211s；墙钟2.622s | 0 | PASS：artifact与catalog hash原子发布、provenance保留、离线读取零外呼及手工目录变化删除旧hash均保持 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | Test墙钟12.322s；Vet+Build+tidy10.047s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、构建和稳定模块图全绿；generation149，go.mod/go.sum保持`695B...728`/`C151...783` |
| 前端全量门禁 | 全部83个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 当前205 tests3.088s；Lint/Type及58页production build | 0 / 0 / 0 / 0 | PASS：205/205、lint、58页production build及独立typecheck全绿；BUG-062前端补充与本项共存 |
| 严重度与449台账 | 逐证据保持Medium/P1；`go run ./tools/remediation/verify_findings`及`-allow-open` | 见最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=295、OPEN=154；Medium=202、UNRESOLVED=73（High120/Low54不变），strict按预期228 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 见最终复核 | 0 | PASS：双仓空白错误0；模块哈希不变；权威audit目录零diff、零status |

合法Minecraft release现在稳定映射到对应NeoForge代次，预发布artifact不会再伪装成MRPack稳定依赖。

## 2026-08-23：BUG-064 loader code大小写唯一性关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 后端有效RED | 新canonical/duplicate规范化单元 | 包1.267s；墙钟8.712s | 1（预期） | PASS：旧结果原样保留`fOrGe`和`MyCustomLoader`，继续接受大小写重复 |
| 前端有效RED | `node --test app/_lib/minecraft-version-selection.test.mts` | 4/5、124ms；墙钟0.224s | 1（预期） | PASS：旧管理员新增仍以严格相等判断，Forge旁可添加forge |
| 双端定向GREEN | Go `TestMinecraftLoaderCodesAreCanonicalAndCaseInsensitiveUnique`；Node同文件5项及ESLint/Type | Go包1.218s/墙钟8.678s；Node5/5 118ms；Lint+Type总墙钟28.849s | 0 / 0 / 0 / 0 | PASS：内置Forge、自定义mycustomloader、sync status code一致；重复拒绝；前端新增和旧大小写查找共享fold key |
| 真实PG/API边界 | `MCMODS_RUN_DB_INTEGRATION=1 DATABASE_URL=postgres://...:55432/postgres go test ./internal/httpapi -run '^TestMinecraftVersionConfigurationDistinguishesMissingFromBrokenIntegration$' -count=1 -v` | 包0.328s；墙钟2.693s | 0 | PASS：重复PUT 400且未保存；持久重复配置zero/error；缺行、损坏shape/normalize/Schema和合法控制合同均保持 |
| 后端全量门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | Test墙钟13.192s；Vet+Build+tidy10.086s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、构建和稳定模块图全绿；generation149，go.mod/go.sum保持`695B...728`/`C151...783` |
| 前端全量门禁 | 全部83个`app/_lib/*.test.mts`；ESLint；测试HTTPS环境Next16 production build；build后TypeScript | 206 tests4.079s；Test+Lint墙钟29.456s；Build+Type27.854s | 0 / 0 / 0 / 0 | PASS：206/206、lint、58页production build及独立typecheck全绿 |
| 严重度与449台账 | 逐证据保持Medium/P1；`go run ./tools/remediation/verify_findings`及`-allow-open` | 见最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=296、OPEN=153；Medium=202、UNRESOLVED=73（High120/Low54不变），strict按预期227 issues，开放期通过 |

## 2026-08-23：BUG-065 MRPack预检不可变快照关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 双端有效RED | 新后端snapshot/handler/source/schema测试；前端preview身份合同 | 后端墙钟3.856s；前端0.534s | 1 / 1（预期） | PASS：旧类型无preview ID/hash/期限，create仍调用实时builder，Schema无preview表；前端请求不携带确认身份 |
| 定向单元与公开边界GREEN | BUG-065 source、JSON隐私、Schema合同及目标包 | httpapi1.185s；database0.089s | 0 | PASS：create源码不含builder；公开JSON含preview身份/loader且不泄露collection/route内部ID；generation150合同完整 |
| 真实PostgreSQL handler/摘要/消费 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run TestFavoriteModpackPreviewSnapshot -count=1 -v` | 包20.738s；墙钟28.7s | 0 | PASS：真实create保存确认的21.1.100、confirmed-version和route FK而非模拟漂移21.1.999；第二次409 consumed；错误摘要409 mismatch |
| generation150完整Schema/FK | 临时完整Schema隔离+10M计划；`TestEveryForeignKeyHasLeadingIndex` | 用例17.28s / 10.45s；包27.816s | 0 | PASS：preview owner/collection FK均有前导索引，临时namespace清理且当时public generation不变 |
| 本机开发库重建与seed | 受保护`cmd/db-reset`显式绑定`127.0.0.1:55432/postgres`、development、`RESET postgres`；public current feature验证 | reset12.6s；Migrate用例0.09s | 0 / 0 | PASS：仅专用本机测试public schema重建/seed为150，preview表12列；PostgreSQL服务保持运行，无远端、生产、回填或双写 |
| 后端发布门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | Test15.608s；静态/构建/tidy9.592s | 0 / 0 / 0 / 0 | PASS：全部包全绿；go.mod/go.sum保持`695B...728`/`C151...783` |
| 前端发布门禁 | 83个`app/_lib/*.test.mts`、ESLint、TypeScript；合法HTTPS环境下Next production build及build后TypeScript | Test4.403s；Lint+Type28.138s；Build+Type26.9s | 0 / 0 / 0 / 0 | PASS：207/207、零lint/类型错误、Next16.2.11与58页构建通过 |
| 严重度与449台账 | 逐证据保持Medium/P1；严格及开放期verifier | 见关闭复核 | 1（预期）/ 0 | 预期：audit/ledger精确449，CLOSED=297、OPEN=152；严格只因其余开放项退出，allow-open通过 |

## 2026-08-23：BUG-067 非Mod报告稳定名称关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效源码RED | `TestFavoriteModpackPreviewUsesTheSupportedTargetNameProjection` | 包1.224s | 1（预期） | PASS：旧导出SQL无统一名称投影及蓝图title分支 |
| 有效真实PG RED | 三类收藏+持久NeoForge artifact完整preview | 包9.919s | 1（预期） | PASS：Mod=`BUG-067 Mod`、整合包=`BUG-067 Modpack`，仅蓝图名称精确为空 |
| 定向GREEN | source闭集及真实PG `TestFavoriteModpackPreviewNamesEverySupportedFavoriteTypeIntegration` | source1.384s；PG包9.961s | 0 / 0 | PASS：三类分别取权威name/title；整合包与蓝图reason均保持`NOT_A_MOD` |
| 后端发布门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | Test14.012s；静态/构建/tidy10.548s | 0 / 0 / 0 / 0 | PASS：全部包全绿；generation150与go.mod/go.sum `695B...728`/`C151...783`保持 |
| 前端发布门禁 | 83个Node文件、ESLint；合法HTTPS环境Next build及build后TypeScript | Test4.266s；Test+Lint墙钟30.44s；Build+Type27.404s | 0 / 0 / 0 / 0 | PASS：207/207、零lint/类型错误、58页生产构建通过；本项无前端代码变化 |
| 严重度与449台账 | 保持审计Medium/P1；严格及开放期verifier | 见关闭复核 | 1（预期）/ 0 | 预期：CLOSED=298、OPEN=151；严格仅因其余开放项退出，allow-open通过 |

## 2026-08-23：BUG-068 收藏来源与导出报告生命周期解耦

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效source/Schema RED | 新生命周期source及generation151 Schema合同 | httpapi1.237s；database0.491s | 1 / 1（预期） | PASS：旧删除无取消、Worker无取消生成物清理，task FK仍CASCADE且无来源ID快照 |
| 有效真实PG RED | 4状态task/item后调用真实DELETE handler | 包9.856s | 1（预期） | PASS：handler返回204后collection/task/detached/item精确为0/0/0/0，证明级联永久销毁 |
| 定向GREEN与取消生成物 | source/Schema/包；真实PG 4状态、history/detail、Worker no-op及finalize竞态 | 默认包3.452s/0.530s；PG包9.691s | 0 | PASS：0 collection/4 detached tasks/4 items；active取消清lease，ready/expired不变；history 4/detail 200；竞态文件deleted、task保留file链接、Outbox pending |
| 受影响真实PG广域 | ARCH-018报告/Worker故障、PERF-041 150项页+百万计划、ARCH-024收藏写错误分类 | 包18.489s | 0 | PASS：错误仍失败关闭；150项无重漏；百万all/ready命中owner索引、DB 0.148/0.232ms；delete业务/DB错误分类不变 |
| generation151 Schema/FK/重建 | 完整临时隔离+10M计划、全部FK前导索引；受保护本机reset/seed；public Migrate及information_schema | 临时包24.529s；reset9.0s；Migrate0.185s | 0 / 0 / 0 | PASS：collection FK nullable/SET NULL且有部分前导索引；本机public=151，无远端、生产、回填或双写 |
| 后端发布门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | Test21.297s；静态/构建/tidy11.151s | 0 / 0 / 0 / 0 | PASS：全部包全绿；go.mod/go.sum保持`695B...728`/`C151...783` |
| 前端发布门禁 | 83个Node文件+ESLint；合法HTTPS Next build及build后TypeScript | Test4.219s；Test+Lint约30.01s；Build+Type27.915s | 0 / 0 / 0 / 0 | PASS：207/207、零lint/类型错误、58页production build通过；本项DTO字段不变 |
| 严重度与449台账 | 保持审计High并定P0；严格及开放期verifier | 见关闭复核 | 1（预期）/ 0 | 预期：CLOSED=299、OPEN=150；严格只因其余开放项退出，allow-open通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 见最终复核 | 0 | PASS：双仓空白错误0；模块哈希不变；权威audit目录零diff、零status |

loader配置、同步状态、服务端校验与前端选择现在共享一个大小写无关身份，重复范围不再被静默选边。

## 2026-08-23：BUG-069 Worker结果统计与MRPack索引一致性

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效compile RED | 新计数不变量与builder索引计数测试 | 两目标墙钟3.089s / 2.934s | 1 / 1（预期） | PASS：旧代码没有六项统计类型/校验函数，builder也不暴露索引文件数 |
| 有效真实PG行为RED | task统计exported=2/final=2，仅保存1条完整合法exported行；OSS明确禁用 | 包3.031s | 1（预期） | PASS：旧Worker成功生成较小索引并触达OSS，最终错误为`OSS_UPLOAD_FAILED`而非报告不一致 |
| 定向GREEN | 计数矩阵、实际索引数及真实PG上传前失败 | 墙钟9.182s；包1.382s | 0 | PASS：缺行、task内部矛盾、分类漂移、索引漂移均拒绝；真实PG写pending/retry_wait、`REPORT_INCONSISTENT`并清lease |
| 相邻真实PG矩阵 | exhausted lease、BUG-068删除/取消生成物、BUG-069缺行、ARCH-018损坏Scan/状态竞争 | 包10.430s；墙钟13.010s | 0 | PASS：取消竞态仍tombstone；损坏字段仍`REPORT_LOAD_FAILED`；统计漂移独立为`REPORT_INCONSISTENT` |
| FavoriteExport/MRPack广域 | 同时绑定`MCMODS_TEST_DATABASE_URL`和旧fixture读取的`DATABASE_URL`到本机55432，运行`go test ./internal/httpapi -run 'FavoriteExport&#124;MRPack' -count=1` | 包39.526s；墙钟约37s | 0 | PASS：依赖图、百万计划、历史分页、loader artifact、生成器、Worker错误和租约恢复全绿；真实公网Modrinth用例按环境显式skip |
| 后端发布门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | Test12.867s；静态/构建/tidy8.990s | 0 / 0 / 0 / 0 | PASS：全部包全绿；go.mod/go.sum保持`695B...728`/`C151...783` |
| 前端发布门禁 | 83个Node文件、ESLint；合法HTTPS Next16 build及build后TypeScript | Test4.989s；Lint25.319s；Build+Type26.366s | 0 / 0 / 0 / 0 | PASS：207/207、零lint/类型错误、58页production build通过；本项无前端代码/DTO形状变化 |
| Schema/API/边界 | generation151、任务结果前导索引、error code及相邻Finding复核 | 定向检查 | 0 | PASS：无DDL/reset/回填/双写；公开协议不变；BUG-070、OPS-013、DEAD-011、TEST-034未重复关闭 |
| 严重度与449台账 | 保持审计High并定P0；严格及开放期verifier | 见关闭复核 | 1（预期）/ 0 | 预期：CLOSED=300、OPEN=149；严格只因其余开放项退出，allow-open通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 见最终复核 | 0 | PASS：双仓空白错误0；模块哈希不变；权威audit目录零diff、零status |

## 2026-08-23：BUG-070 MRPack跨平台路径冲突预检

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效生成器行为RED | `TestBuildMRPackRejectsPortablePathCollisions` | 墙钟9.094s；包1.285s | 1（预期） | PASS：旧生成器同时接受大小写、NFC/NFD及ﬁ/fi三类碰撞并成功产出索引 |
| 有效preview/create compile RED | 新逐项冲突、阻断helper及真实PG create测试 | 墙钟2.925s / 3.085s | 1 / 1（预期） | PASS：旧预检无冲突分组/reason，create无专用拒绝路径 |
| 定向GREEN | portable generator、逐项preview与source顺序合同 | 墙钟9.112s；包1.270s | 0 | PASS：三类portable collision均拒绝；四个冲突item精确failed/FILE_PATH_CONFLICT，唯一及已跳过项不变；mark早于计数、create阻断早于empty |
| 真实PG原子拒绝 | 完整generation151临时Schema，保存两个大小写冲突item后调用真实create handler | 墙钟12.419s；包9.764s | 0 | PASS：422 `MODPACK_EXPORT_FILE_PATH_CONFLICT`，task=0、preview consumed=0 |
| 夹具纠正与依赖回归 | PERF-040 provider用project ID生成唯一JAR名；重跑150节点完整preview | 墙钟20.882s；包13.122s | 0 | PASS：collection1/exported1/dependencies149/failed0，单依赖图查询和有界provider并发不变 |
| FavoriteExport/Preview/MRPack广域 | 同时绑定两个数据库环境变量到55432，运行完整相关组 | 包76.643s | 0 | PASS：冻结preview、路径冲突、Worker计数、依赖上限/百万计划、历史分页、loader与生成器全绿；公网真实Modrinth按环境skip |
| 后端发布门禁 | `go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy`及哈希控制 | Test14.712s；静态/构建/tidy10.360s | 0 / 0 / 0 / 0 | PASS：全部包全绿；x/text原已直接依赖，go.mod/go.sum保持`695B...728`/`C151...783` |
| 前端发布门禁 | 83个Node文件、ESLint；合法HTTPS Next16 build及build后TypeScript | Test5.513s；Lint28.836s；Build+Type27.715s | 0 / 0 / 0 / 0 | PASS：207/207、零lint/类型错误、58页production build通过；既有双语reason展示继续有效 |
| Schema/API/边界 | generation151、路径预算、新422及相邻Finding复核 | 定向检查 | 0 | PASS：无DDL/reset/回填/双写/N+1；BUG-071/072、OPS-013、TEST-034未重复关闭 |
| 严重度与449台账 | 保持审计Medium/P1；严格及开放期verifier | 见关闭复核 | 1（预期）/ 0 | 预期：CLOSED=301、OPEN=148；严格只因其余开放项退出，allow-open通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 见最终复核 | 0 | PASS：双仓空白错误0；模块哈希不变；权威audit目录零diff、零status |

## 2026-08-23：BUG-071 导出历史显式重建来源与确认事实

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效后端RED | generation152 preview Schema合同；history确认字段/route/source合同及真实PG调用编译 | database0.904s；httpapi编译约3.6s | 1 / 1（预期） | PASS：旧generation151、summary无allow/report、preview无source、Server无rebuild handler，精确命中Finding |
| 有效前端RED | `favorite-modpack-export-rebuild.test.mts` | Node124.810ms | 1（预期） | PASS：旧类型无确认/报告/source，只有含糊`returnToSettings/sameSettings`且没有rebuild API |
| 定向GREEN | generation/Schema/summary/route/hash/分页合同；前端rebuild合同与TypeScript | database0.926s；httpapi1.280s；Node131.659ms；Type13.404s | 0 | PASS：generation152、DB/JSON镜像、两种source、两个双语动作及原确认提交合同通过 |
| 真实PG双模式与生命周期 | 完整临时generation152：live current preflight；删除有current preview的collection；detached current；original preview/create及确认漂移拒绝 | 最终包11.138s；墙钟19.5s | 0 | PASS：live current=200；删除不被preview FK阻断；detached current=409/source unavailable；original=200，错误确认409且不消费；正确create=202，新task collection NULL、snapshot `b071hist1`、allow=true、items=2 |
| generation152重建与DDL核验 | `DATABASE_URL`固定本机55432后保护性`cmd/db-reset`；直接查询generation/columns/FK | reset8.8s；查询0.6s | 0 / 0 | PASS：public=152；preview collection nullable/SET NULL，snapshot/source非空；4个seed用户；无回填/双读/双写 |
| 数据库完整隔离套件 | 两个DB环境变量固定本机55432运行`go test ./internal/database -count=1` | 包135.238s | 1（既有夹具） | generation152安装、10M计划及FK检查运行；仅既有`TestStickerReferenceProjectionIntegration`重复临时索引`42P07`失败，与本项无关且此前已登记；本项Schema定向全绿 |
| FavoriteExport/Preview/MRPack广域 | `DATABASE_URL`与`MCMODS_TEST_DATABASE_URL`均固定本机55432，显式启用integration | 包57.275s | 0 | PASS：冻结preview、删除生命周期、原始重建、portable路径、报告计数、依赖图/百万页、loader与Worker全绿 |
| 环境边界纠正 | 首次广域仅设置test URL时，旧fixture仍从仓库`.env`解析远端连接；发现后立即终止database进程并禁止后续fallback | 发现即停止 | 1（废弃证据） | 租约fixture先创建临时favorite collection，task insert因远端旧Schema缺snapshot失败，defer随后删除该collection；未形成task；该轮不计验证证据。其后所有DB证据同时显式绑定两个URL到`127.0.0.1:55432`，未再访问远端 |
| 后端发布门禁 | ordinary `go test ./... -count=1`（integration变量全部unset、DATABASE_URL本机）；`go vet ./...`；`go build ./...`；`go mod tidy`哈希控制 | 最终Test18.621s；静态/构建/tidy14.254s（并行墙钟18.8s） | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；go.mod/go.sum保持`695B...728`/`C151...783` |
| 前端发布门禁 | 84个Node文件；ESLint；TypeScript；合法HTTPS Next16 production build及build后TypeScript | Test4.507s；Lint约31.3s；Type13.404s；Build26.267s；post-Type2.9s | 0 / 0 / 0 / 0 / 0 | PASS：209/209、零lint/类型错误、58页production build通过 |
| Schema/API/边界 | generation152、新POST/DTO/409合同、原报告验证和相邻Finding复核 | 定向检查 | 0 | PASS：source显式、owner/permission重新校验、current走权威builder、original走完整task/items不变量；BUG-072、DEAD-011、OPS-013、TEST-034未重复关闭 |
| 严重度与449台账 | 保持审计Medium并定P1；严格及开放期verifier | 见关闭复核 | 1（预期）/ 0 | 预期：CLOSED=302、OPEN=147；严格只因其余开放项退出，allow-open通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 见最终复核 | 0 | PASS：双仓空白错误0；模块哈希不变；权威audit目录零diff、零status |

## 2026-08-23：BUG-072 MRPack导出Minecraft版本权威边界

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效source/行为RED | 新目录/请求边界source合同与真实PG unknown version preflight | 包11.045s；墙钟19.2s | 1（预期） | PASS：旧代码缺语法/enablement helper；`9.9.9`穿过decode后返回`MODPACK_EXPORT_LOADER_SNAPSHOT_UNAVAILABLE`，未得到version专用错误 |
| 定向GREEN | source、语法/特殊合法code、版本/loader闭集；完整临时PG unknown/path/enabled三分支 | 包10.979s；墙钟18.8s | 0 | PASS：`9.9.9`与`../../1.21.1`均422 `MODPACK_EXPORT_INVALID_MINECRAFT_VERSION`且无preview；`1.21.1/fabric` 200并保存唯一preview；81字节/Unicode/斜线拒绝，真实特殊code接受 |
| 相邻权威与导出回归 | 离线catalog-bound loader artifact + BUG-071 detached original；PERF-040循环/上限/150节点preview；普通MinecraftVersion单元组 | 组合包10.996s；依赖包12.903s；单元0.244s | 0 | PASS：手工catalog变更仍清stale artifact且旧`errors.Is`兼容；live/original rebuild不变；150节点完整且fixture改用同步发布API |
| FavoriteExport/MRPack/loader广域 | 两个DB URL均固定本机55432、显式integration，运行`FavoriteExport/Preview/MRPack/MinecraftLoaderArtifact`完整组 | 包68.346s | 0 | PASS：版本权威、loader目录、preview/rebuild、依赖图/百万计划、Worker统计、portable generator全绿 |
| Schema/API/性能 | generation152、既有artifact PK与catalog hash、请求/DTO不变、新422错误分类 | 定向检查 | 0 | PASS：无DDL/reset/回填/双写/N+1；权威检查为最多2500版本和100 loader的有界内存扫描，artifact仍单行PK查询；BUG-073、ARCH-017、TEST-034未重复关闭 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy及模块哈希 | Test13.380s；静态/构建/tidy14.357s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；go.mod/go.sum保持`695B...728`/`C151...783` |
| 前端发布门禁 | 84个Node文件、ESLint、合法HTTPS Next16 production build、build后TypeScript | Test4.486s；Lint32.969s；Build29.000s；post-Type2.687s | 0 / 0 / 0 / 0 | PASS：209/209、零lint/类型错误、58页build；本项无前端代码或DTO变化；最初两个pnpm进程因隔离PATH缺Node未启动，补入捆绑runtime后以上有效门禁全绿 |
| 严重度与449台账 | 保持审计Medium并定P1；严格及开放期verifier | 1.888s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=303、OPEN=146；Medium=202、UNRESOLVED=73（High120/Low54不变），strict按预期220 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0（仅既有LF/CRLF提示）；模块哈希不变；权威audit目录零diff、零status |

## 2026-08-23：BUG-076 蓝图实体端到端保真边界

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效行为RED | 两类规范化实体×NBT/Sponge/Litematic编码，及带实体Litematic/Sponge/legacy源解码 | 包1.203s；墙钟8.787s | 1（预期） | PASS：旧代码六个编码分支和三个解码分支全部返回nil错误，精确证明成功variant/normalized artifact会丢实体 |
| 定向GREEN与真实PG | 同一9分支；临时PG分别提交convert/normalize永久保真错误 | 包1.354s；墙钟8.826s | 0 | PASS：非JSON实体编码与未映射源实体全部显式拒绝；两operation均第一次attempt即failed并保留entity原因，convert主体仍ready、normalize主体failed |
| Blueprint广域 | 两个DB URL固定本机55432、显式integration，运行httpapi全部名称含Blueprint测试 | 包21.667s；墙钟29.186s | 0 | PASS：无实体三格式codec往返、JSON实体保留、租约/恢复/重试、Outbox、artifact原子补偿、分页/配额及BUG-075/078相邻路径全绿 |
| Schema/API/性能 | generation152、任务/variant/原始对象及协议复核 | 定向检查 | 0 | PASS：无DDL/reset/回填/双写/N+1；路由/DTO不变；永久错误复用既有last_error和失败通知，不写variant，不删除原对象；仅有界列表存在性检查 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy及模块哈希 | Test18.224s；静态/构建/tidy18.905s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；go.mod/go.sum保持`695B...728`/`C151...783` |
| 前端发布门禁 | 84个Node文件、ESLint、合法HTTPS Next16 production build、build后TypeScript | Test4.725s；Lint37.832s；Build35.300s；post-Type2.632s | 0 / 0 / 0 / 0 | PASS：209/209、零lint/类型错误、58页build；公开DTO和前端代码未变化 |
| 严重度与449台账 | UNRESOLVED→Medium/P1；严格及开放期verifier | 1.891s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=304、OPEN=145；Medium=203、UNRESOLVED=72（High120/Low54不变），strict按预期218 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希不变；权威audit目录零diff、零status |

## 2026-08-23：BUG-077 蓝图现有封面关系授权

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | owner上传并绑定cover，另一actor以管理员即时编辑语义只更新metadata并保留该cover | 包1.321s；墙钟8.891s | 1（预期） | PASS：旧apply把editor 701当UploaderID解析owner 700文件，返回`no rows in result set`，精确复现审计Finding |
| 定向真实PG GREEN | 现有关系、snapshot伪key、未绑定替换及owner替换四分支 | 包1.328s；墙钟8.819s | 0 | PASS：admin metadata成功且key恢复DB值；admin换未绑定owner文件失败并回滚；owner换自有受信文件成功 |
| Blueprint/本地化资产广域 | 两个DB URL固定本机55432、显式integration，运行`Blueprint&#124;LocalizedAsset`完整相关组 | 包20.369s；墙钟22.927s | 0 | PASS：修订原子保存/回滚、审核、codec、Worker租约/任务、OSS绑定/读取与BUG-075/076/078相邻路径全绿 |
| Schema/API/性能 | generation152、cover FK/OSS unique/安全闭集及锁复核 | 定向检查 | 0 | PASS：无DDL/reset/回填/双写/N+1；现有绑定为单个blueprint PK+file unique联接并锁file；路由/DTO不变，snapshot key不受信 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy及模块哈希 | Test17.354s；静态/构建/tidy18.698s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；go.mod/go.sum保持`695B...728`/`C151...783` |
| 前端发布门禁 | 84个Node文件、ESLint、合法HTTPS Next16 production build、build后TypeScript | Test4.653s；Lint37.983s；Build35.596s；post-Type2.585s | 0 / 0 / 0 / 0 | PASS：209/209、零lint/类型错误、58页build；公开DTO和前端代码未变化 |
| 严重度与449台账 | UNRESOLVED→Low/P2；严格及开放期verifier | 1.863s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=305、OPEN=144；Low=55、UNRESOLVED=71（High120/Medium203不变），strict按预期216 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希不变；权威audit目录零diff、零status |

## 2026-08-23：BUG-079 蓝图上传会话生命周期

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效Schema RED | `TestBlueprintUploadExpirySchemaContract` | 包0.926s；墙钟3.087s | 1（预期） | PASS：旧generation152、`blueprints`无`upload_expires_at`及到期部分索引 |
| 有效真实PG行为RED | 临时上传主体790–793及本地化/subject投影后调用真实Maintenance | 包1.341s；墙钟9.234s | 1（预期） | PASS：两个已过期uploading、fresh uploading、queued及全部投影均残留，精确证明没有自动清理 |
| 定向GREEN | Schema合同、即时/周期源码闭集及真实PG按ID/Key/wrong-owner/expiry/status矩阵 | Schema包0.932s；行为包1.323s；最终组合包1.329s/墙钟8.919s | 0 | PASS：按ID/Key即时删除；wrong-owner no-op；Maintenance只删到期790/791并保留fresh792、queued793和wrong-owner fresh796，各投影与主体同成同删 |
| 受影响广域 | 两个DB URL固定本机55432并显式integration，运行`Blueprint|OSS.*(Upload|Multipart|Quota)|Maintenance`；数据库Blueprint/Ephemeral/Schema/FK相邻组 | HTTP包46.797s/墙钟49.619s；database包28.353s/墙钟30.115s | 0 | PASS：蓝图上传/完成、multipart/abort、quota、维护及完整Schema邻接合同全绿 |
| generation153重建与DDL直查 | 保护性`cmd/db-reset`仅绑定127.0.0.1:55432/postgres；随后psql查询generation、列、部分索引及seed | reset8.106s；查询即时 | 0 | PASS：public generation153；期限为nullable timestamptz；索引predicate精确；4个seed用户；无远端、生产、回填或双写 |
| Schema/API/性能 | 期限来源、1000行批次、稳定索引顺序、锁/状态边界与相邻Finding复核 | 定向检查 | 0 | PASS：无N+1；`SKIP LOCKED`支持多实例；complete清期限；非uploading永不删除；公开DTO/状态码不变；BUG-080、OPS-018未重复关闭 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy及模块哈希 | Test9.482s；静态/构建/tidy8.987s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；go.mod/go.sum保持`695B...728`/`C151...783` |
| 前端发布门禁 | 84个Node文件、ESLint、合法HTTPS Next16 production build、build后TypeScript | Test4.223s；Lint+Build+post-Type55.237s | 0 / 0 / 0 / 0 | PASS：209/209、零lint/类型错误、58页production build通过；本项无前端代码或DTO变化 |
| 严重度与449台账 | UNRESOLVED→Low/P2；严格及开放期verifier | 墙钟约1.1s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=306、OPEN=143；Low=56、UNRESOLVED=70（High120/Medium203不变），strict精确214 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0（仅既有LF/CRLF提示）；模块哈希不变；权威audit目录零diff、零status |

## 2026-08-23：BUG-080 举报证据原子登记与严格恢复

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG+fake OSS RED | 一次性非回滚sequence触发器只拒绝第一次`report_evidence` INSERT，调用真实完成Handler | 包1.330s；墙钟9.328s | 1（预期） | PASS：首个500后旧路径精确留下`oss_files=1/report_evidence=0`，命中独立提交根因 |
| 定向GREEN/重试矩阵 | 同一测试覆盖故障回滚、原请求重试、第三次幂等、错误hash及预存legacy orphan | 包1.335s；墙钟8.846s | 0 | PASS：首次0/0；重试201且1/1/evidence ID；第三次200同ID；错误hash不命中；严格匹配旧1/0行补建并200 |
| 举报/OSS广域 | 两个DB URL固定本机55432、显式integration，运行`Report|Evidence|OSS.*(Complete|Upload|Multipart)|Governance` | 包约39s；墙钟46.906s | 0 | PASS：举报快照/审核/分页/可见性、证据验证、OSS完成/分片/配额/删除及治理错误边界全绿 |
| Schema/API/并发边界 | generation153、两表唯一键、事务/锁/严格identity和邻项复核 | 定向检查 | 0 | PASS：无DDL/reset/回填/双写/N+1；同Key冲突串行后重锁；成功/幂等DTO和201/200不变；ARCH-020、OPS-018未重复关闭 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy及模块哈希 | Test14.393s；静态/构建/tidy15.724s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；go.mod/go.sum保持`695B...728`/`C151...783` |
| 前端发布门禁 | 84个Node文件、ESLint、合法HTTPS Next16 production build、build后TypeScript | Test4.565s；Lint+Build+post-Type58.929s | 0 / 0 / 0 / 0 | PASS：209/209、零lint/类型错误、58页production build通过；本项无前端代码或DTO变化 |
| 严重度与449台账 | UNRESOLVED→Medium/P1；严格及开放期verifier | 墙钟约1.1s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=307、OPEN=142；Medium=204、UNRESOLVED=69（High120/Low56不变），strict精确212 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希不变；权威audit目录零diff、零status |

## 2026-08-23：BUG-082 评论日志附件可靠任务

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效Schema/source RED | generation154任务表合同；createComment commit顺序、Worker/runtime/NATS闭集 | database包0.912s/墙钟7.387s；httpapi包1.196s/墙钟12.567s | 1 / 1（预期） | PASS：旧generation153、无job/lease/index；评论仍在commit后同步bind且无Outbox/Worker/default task |
| 定向Schema/source GREEN | 同一两项合同 | database包0.915s；httpapi包1.195s | 0 | PASS：generation154 DDL闭集完整；job+Outbox位于最终comment commit前，post-commit同步路径删除，runtime和默认任务已接线 |
| 真实PG+fake OSS状态机 | Outbox一次性trigger、provider outage/recovery、重复完成和过期processing lease | 包2.830s；墙钟10.460s | 0 | PASS：Outbox故障0/0/0/0；成功comment/attachment/job/event各1；首次OSS失败queued attempt1，第二次completed attempt2且唯一share/entry/binding；重复no-op；crashed worker恢复completed |
| Comment/LogShare/Schema广域 | 两个DB URL固定本机55432、显式integration，运行CommentLogAttachment/CommentTree/LogShare/EveryForeignKey/Ephemeral/FeatureUpdate组合 | 墙钟27.202s | 0 | PASS：评论树/附件、日志脱敏与批量幂等、完整临时Schema、全部FK前导索引及新状态机全绿 |
| generation154重建与DDL直查 | 保护性`cmd/db-reset`仅绑定127.0.0.1:55432/postgres；psql查询generation、表、约束、索引和seed | reset8.052s；直查即时 | 0 | PASS：public=154；复合cascade/requester FK、唯一键、状态/预算CHECK、三索引精确；4 seed用户；无远端、生产、回填或双写 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy及模块哈希 | Test23.114s；静态/构建/tidy23.872s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；go.mod/go.sum保持`695B...728`/`C151...783` |
| 前端发布门禁 | 84个Node文件、ESLint、合法HTTPS Next16 production build、build后TypeScript | Test6.601s；Lint+Build+post-Type60.527s | 0 / 0 / 0 / 0 | PASS：209/209、零lint/类型错误、58页production build通过；公开协议不变 |
| 严重度与449台账 | UNRESOLVED→Medium/P1；严格及开放期verifier | 墙钟约1.5s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=308、OPEN=141；Medium=205、UNRESOLVED=68（High120/Low56不变），strict精确210 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希不变；权威audit目录零diff、零status |

## 2026-08-23：BUG-083 已删除评论附件关系与序列化边界

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | 一次性attachment DELETE trigger；deleted脏绑定含ready日志短链 | 包1.521s；墙钟9.666s | 1（预期） | PASS：旧DELETE不触发解绑故障、返回200并留下attachment/binding/job；旧annotator向deleted响应返回latest.log、4096 bytes和`/log/s/b083leak` |
| 定向GREEN | 同两项真实PG测试，两个DB URL固定本机55432并显式integration | 包1.383s；墙钟9.299s | 0 | PASS：首次删除500且published/正文/attachment/binding/job/share全回滚；重试200并只清前三种关系、share保留；脏绑定响应附件严格为空 |
| Comment/任务/Schema广域 | `go test ./internal/httpapi ./internal/database -run 'Comment&#124;BUG082&#124;FeatureUpdate' -count=1` | httpapi包34.835s；database包0.099s | 0 | PASS：评论树/分页/附件、BUG-082可靠job、FeatureUpdate schema合同全绿；job按复合FK随attachment级联 |
| Schema/计划/API边界 | generation源码154；临时同构表上EXPLAIN两条comment_id DELETE；路由/DTO复核 | 即时 | 0 | PASS：无DDL/reset/回填/双写；两条均命中复合主键Bitmap Index Scan；DELETE成功仍200既有envelope，deleted附件固定`[]`，无前端迁移 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | 19.779s | 0 / 0 / 0 / 0 | PASS：全部普通包、Vet、Build、`go mod tidy -diff`全绿 |
| 前端发布门禁 | 84个Node文件、ESLint、合法HTTPS Next16 production build、build后TypeScript | Test4.219s；静态/构建/类型49.662s | 0 / 0 / 0 / 0 | PASS：209/209、零lint/类型错误、58页production build通过；本项无前端代码或DTO变化 |
| 严重度与449台账 | UNRESOLVED→Medium/P1；严格及开放期verifier | 1.313s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=309、OPEN=140；Medium=206、UNRESOLVED=67（High120/Low56不变），strict精确208 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0（仅既有LF/CRLF提示）；模块哈希保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

## 2026-08-23：BUG-090 社区自动批准编辑的 published revision CAS

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG/前端RED | 同一published revision基线依次提交两个免审编辑；第一方API/component源码合同 | 后端包1.468s、墙钟9.556s；前端0.568s | 1 / 1（预期） | PASS：旧后端两个请求均200，第二个静默覆盖第一个并产生第三条revision；旧前端既不传基线也不识别冲突。最初RED清理触发append-only历史保护，随后测试改为单连接完整临时Schema，避免污染共享public且不改变复现结论 |
| 核心GREEN | `TestCommunityPostAutomaticApprovalRejectsAStaleRevisionIntegration`、锁序/source合同及前端冲突合同 | 后端包11.113s、墙钟19.011s；前端0.509s | 0 / 0 | PASS：详情返回published revision；首个编辑200，陈旧与缺失基线均409并返回`COMMUNITY_POST_EDIT_CONFLICT/currentRevisionId`，畸形基线400；最终正文保持first editor且revision总数2；冲突前无引用、悬赏、revision、apply或导航副作用 |
| CommunityPost/审核广域 | 两个DB URL固定本机55432并显式integration；CommunityPost完整组、`ContentReview&#124;ReviewResolution`完整组 | CommunityPost包30.666s；审核组包129.771s | 0 | PASS：创建/详情/编辑、自动批准、pending审核、拒绝/恢复、修订与投影相邻路径全绿；同一事务读取review config，不发生池外重入 |
| 百万审核队列规模夹具 | 修正临时shadow `content_change_items`为包含生产查询排序所需`id bigserial primary key`的同形结构，再运行scale测试 | 包26.473s | 0 | PASS：初次广域仅该既有夹具因缺`id`而失败；修复夹具结构漂移后百万pending计划/分页通过，生产SQL与Schema未为测试降级 |
| Schema/API/计划与本机恢复 | generation155完整临时Schema；实际PG `EXPLAIN`；保护性`cmd/db-reset`只绑定127.0.0.1:55432/postgres并直查seed/测试残留 | reset7.424s；计划/直查即时 | 0 | PASS：无DDL、回填、双写或generation变化；聚合advisory lock后锁post并以PK联接revision重读；public=155、4 seed、BUG-090残留0；详情新增可选`publishedRevisionId`，PUT新增`baseRevisionId`及稳定409，第一方冲突保留草稿并显示双语提示 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | Test20.970s；Vet17.058s；Build18.221s；tidy1.265s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；httpapi3.053s，模块依赖无diff |
| 前端发布门禁 | 动态枚举86个Node文件；bundled Node PATH下ESLint；合法HTTPS Next16 build；build后TypeScript | Tests5.148s；Lint37.923s；Build+Type44.277s | 0 / 0 / 0 / 0 | PASS：211/211、零lint/类型错误、58页production build通过 |
| 严重度与449台账 | 既有High/P1保持；严格及开放期verifier | 最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=316、OPEN=133；High=121、Medium=208、Low=58、UNRESOLVED=62；strict预期196 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

## 2026-08-23：BUG-089 收藏全量替换先校验后变更

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | 实际`setFavoriteMembership`，已有合法关系后分别提交missing与跨owner集合ID | 包1.397s；墙钟9.109s | 1（预期） | PASS：旧PUT两种无效输入均返回200；逐项`INSERT ... SELECT`的0 affected被忽略，已存在关系被清空或缩减，精确复现Finding |
| 定向真实PG/source GREEN | 同一handler矩阵；源码顺序护栏 | 包1.385s；墙钟9.262s | 0 | PASS：missing/跨owner均400且原关系=1；重复合法ID规范化后200/关系=1；显式空数组200/关系=0；validate严格位于default upsert和DELETE前，批插入检查精确RowsAffected |
| Favorite广域回归 | 两个DB URL固定本机55432并显式integration，`go test ./internal/httpapi -run Favorite -count=1` | 包129.061s；墙钟约129s | 0 | PASS：收藏可见性、四类keyset、百万集合/关系索引计划、membership summary/delta、MRPack预检/报告/任务/Outbox生命周期和错误观测全部通过 |
| Schema/API/并发边界 | generation155源码及实际库；owner锁、Repeatable Read、现有第一方调用复核 | 即时 | 0 | PASS：无DDL/reset/回填/双写；全部ID先`FOR UPDATE`且任一短写事务回滚；合法PUT成功DTO不变，非法身份收紧400；第一方继续PATCH，无前端迁移 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | Test21.148s；Vet16.293s；Build17.223s；tidy1.217s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；httpapi2.809s、queue3.630s、serverprobe4.752s；模块依赖无diff |
| 前端发布门禁 | 动态枚举85个Node文件；bundled Node PATH下ESLint；合法HTTPS Next16 build；build后TypeScript | Tests5.414s；Lint约34.3s；Build+Type约34.8s | 0 / 0 / 0 / 0 | PASS：210/210、零lint/类型错误、58页production build通过；前端代码与DTO不变 |
| 严重度与449台账 | 既有Medium/P1保持；严格及开放期verifier | strict1.155s；allow1.170s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=315、OPEN=134；High=121、Medium=208、Low=58、UNRESOLVED=62；strict精确197 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

## 2026-08-23：BUG-087 皮肤创建审核与首次发布闭环

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效source RED | `go test ./internal/httpapi -run '^TestSkinCreationUsesCatalogCreateRevisionWorkflow$' -count=1` | 包1.232s；墙钟10.109s | 1（预期） | PASS：缺CatalogCreate/bypass/revision/operation/snapshot五项边界，主体与本地化两条SQL均固定approved |
| 定向GREEN/策略 | source闭集与ordinary/skin.admin/content.no-review/admin.*配置矩阵 | 最终包1.199s | 0 | PASS：普通与skin.admin均审核，只有明确no-review/admin免审；CatalogCreate关闭时不虚构审核；无published revision的PUT仍识别create |
| 真实PG生命周期 | `TestSkinCreationReviewPolicyPersistsPendingAndBypassRevisionsIntegration`，两个DB URL固定本机55432并显式integration | 包0.294s；墙钟2.710s | 0 | PASS：普通创建主体/语言/request全pending、base nil、source skin_upload、投影0；首次拒绝主体/语言rejected；免审全approved、published revision绑定、投影1 |
| ContentReview/LocalizedAsset/skin广域 | generation154真实库运行创建、审核队列、聚合本地化原子性、serializer/normalize与共同策略组 | httpapi包0.531s；墙钟2.998s | 0 | PASS：创建审核链与既有编辑审核、两资产snapshot回滚、队列和BUG-086能力合同共同通过 |
| Schema/API边界 | generation154、触发投影、response及第一方上传消费点复核 | 即时 | 0 | PASS：无DDL/reset/回填/双写；POST仍201 SkinTexture，pending使用既有枚举且owner详情可达；所有新增写在原skin事务、公开投影由现有trigger自动排除 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | 20.987s | 0 / 0 / 0 / 0 | PASS：全部普通包、Vet、Build、`go mod tidy -diff`全绿；httpapi2.542s、默认queue3.424s |
| 前端发布门禁 | 动态枚举85个Node文件、ESLint、合法HTTPS Next16 production build、build后TypeScript | Tests含于全批次；56.785s | 0 / 0 / 0 / 0 | PASS：210/210、零lint/类型错误、58页production build通过；前端代码和DTO不变 |
| 严重度与449台账 | UNRESOLVED→High/P1；严格及开放期verifier | strict约1.2s；allow1.051s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=313、OPEN=136；High=121、UNRESOLVED=63（Medium207/Low58不变），strict精确200 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0（仅既有LF/CRLF提示）；模块哈希保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

## 2026-08-23：BUG-088 共享皮肤Blob系统归属与可回收生命周期

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效Schema/source RED | generation、引用事实/trigger/索引；归属、Key、GC、补偿和调用方闭集 | database包0.932s、httpapi包1.146s；墙钟9.236s | 1 / 1（预期） | PASS：旧generation154无计数/trigger/rebuild/GC索引；派生file含首owner、source/stored同值、固定hash Key；删除/治理/maintenance和rollback补偿全部缺失 |
| 核心真实PG+fake OSS | 双用户同hash、引用2→1→0、恢复新周期；rollback补偿；100k GC计划；持久化/最后删除锁序 | 生命周期包0.262s；rollback/计划/并发组合包1.238s；锁序单项包1.450s | 0 | PASS：首PUT后复用零PUT、两用户额度0；首删file active、末删deleted+Outbox1；恢复第二PUT且file/key全新；rollback只留pending无file任务；100k命中部分索引；并发无deadlock |
| Skin/Yggdrasil/OSS广域 | 两个DB URL固定本机55432并显式integration；Skin创建/serializer/normalize/sanitize/Yggdrasil、OSS delete/quota相邻组 | Skin组墙钟20.912s；OSS组12.898s | 0 | PASS：皮肤审核/能力、Launcher session/纹理、删除dead/replay/key周期及quota trigger/rebuild全部绿 |
| generation155重建/Schema/FK | 保护性`cmd/db-reset`只指向127.0.0.1:55432/postgres；psql直查generation/列/索引/trigger/function；Ephemeral+全部FK | 最终reset7.968s；Schema/FK墙钟28.148s | 0 | PASS：generation155、非负计数、两个部分索引、维护trigger和两函数精确；全部FK前导覆盖，4 seed用户；无远程、生产、历史回填或双写 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | Test15.751s；Vet10.173s；Build11.191s；tidy0.387s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；httpapi4.747s、queue3.670s、antiabuse4.101s，模块依赖无diff |
| 前端发布门禁 | 动态枚举85个Node文件；显式bundled Node PATH的ESLint；合法HTTPS Next16 build；build后TypeScript | Tests3.303s；Lint32.753s；Build+Type32.991s | 0 / 0 / 0 / 0 | PASS：210/210、零lint/类型错误、58页production build通过；前端代码和DTO不变 |
| 严重度与449台账 | UNRESOLVED→Medium/P1；严格及开放期verifier | strict0.905s；allow0.871s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=314、OPEN=135；Medium=208、UNRESOLVED=62（High121/Low58不变），strict精确198 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

## 2026-08-23：BUG-086 皮肤管理员编辑能力与写授权一致性

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效unit RED | `go test ./internal/httpapi -run '^TestSkinAssetJSONMatchesSkinAdministrationAuthorization$' -count=1` | 包1.159s；墙钟8.795s | 1（预期） | PASS：`skin.admin`与`admin.*`两个非owner均得到canEdit=false，而同一predicate允许PUT/DELETE，精确复现能力漂移 |
| 定向GREEN/语义矩阵 | BUG-086与canonical serializer、skin normalize/routes、catalog/wardrobe cursor非规模组 | 目标包0.138s；墙钟2.585s | 0 | PASS：owner true/true、普通viewer false/false、两类管理员true/false；编辑权修正且private pending装备权未扩大，全部调用方以Claims通过编译 |
| Schema/API/前端边界 | generation154、`isSkinAdmin`写端/serializer、`skin-detail`消费点及全部callsite复核 | 即时 | 0 | PASS：无DDL/reset/回填/查询/双写；DTO与路径不变；已有manageTexture区按canEdit显示，无前端权限猜测或迁移 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | 20.210s | 0 / 0 / 0 / 0 | PASS：全部普通包、Vet、Build、`go mod tidy -diff`全绿；httpapi2.429s、默认queue3.440s |
| 前端发布门禁 | 动态枚举85个Node文件；ESLint；合法HTTPS Next16 production build；build后TypeScript | Tests3.242s；静态/构建/类型批次55.861s | 0 / 0 / 0 / 0 | PASS：210/210、零lint/类型错误、58页production build通过；本项前端代码未变 |
| 严重度与449台账 | UNRESOLVED→Low/P2；严格及开放期verifier | strict0.844s；allow0.834s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=312、OPEN=137；Low=58、UNRESOLVED=64（High120/Medium207不变），strict精确202 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0（仅既有LF/CRLF提示）；模块哈希保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

## 2026-08-23：BUG-085 评论回复能力与目标拉黑一致性

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | mod developer拉黑有comment.create的viewer，分别读取逐项能力和调用create | 包1.406s；墙钟9.362s | 1（预期） | PASS：旧逐项CanReply仍true，但同一target创建端明确403，精确复现能力契约漂移 |
| 定向GREEN/目标矩阵 | BUG-085专测加既有UserBlock项目developer与tutorial/discussion/issue/news矩阵 | 最终包2.309s；墙钟10.324s | 0 | PASS：被拉黑CanReply=false/create403；解除后true；单目标wrapper和批量resolver对项目与直接owner语义一致 |
| 规模与计划 | watch 1项/100项数据库计数；实际generation154 SQL EXPLAIN | 目标组合包13.814s；计划即时 | 0 | PASS：查询13=13且<=13，无N+1；UNNEST驱动，八类目标命中PK/catalog索引，user_blocks命中blocked前导索引；新增唯一安全批查询 |
| Comment/拉黑/访问广域 | 两个DB URL固定本机55432、显式integration，运行Comment/UserBlock/ProjectAccess/FeatureUpdate组合 | httpapi包36.164s；database包0.308s | 0 | PASS：评论全路径、用户双向关系、项目developer集合、完整target分支与generation154相邻合同全绿 |
| Schema/API边界 | generation154、target闭集、故障传播、前端消费点复核 | 定向检查 | 0 | PASS：无DDL/reset/回填/双写；API/DTO/前端不变；owner查询故障继续使评论装配500，写端403语义不变 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | 最终缓存复核8.141s | 0 / 0 / 0 / 0 | PASS：最终查询优化后全部普通包、Vet、Build、`go mod tidy -diff`全绿 |
| 前端发布门禁 | 85个Node文件、ESLint、合法HTTPS Next16 production build、build后TypeScript | 210 tests3.639s；全门禁56.672s | 0 / 0 / 0 / 0 | PASS：210/210、零lint/类型错误、58页production build通过；前端继续只按服务端canReply渲染 |
| 严重度与449台账 | UNRESOLVED→Low/P2；严格及开放期verifier | strict0.826s；allow0.818s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=311、OPEN=138；Low=57、UNRESOLVED=65（High120/Medium207不变），strict精确204 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0（仅既有LF/CRLF提示）；模块哈希保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

## 2026-08-23：BUG-084 评论编辑乐观并发控制

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效后端/前端RED | 真实PG stale base PATCH；第一方API/component源码合同 | 后端包1.267s/墙钟9.394s；前端0.528s | 1 / 1（预期） | PASS：旧后端把baseUpdatedAt当未知字段400且body-only仍覆盖；前端不传渲染版本、不识别冲突code或当前版本 |
| 定向GREEN | 同一真实PG首编辑→陈旧第二编辑→缺版本→显式重试矩阵；前端合同 | 后端包1.334s/墙钟9.214s；前端0.510s | 0 / 0 | PASS：陈旧请求409/current details且DB保持first editor；缺版本400；以current版本重试成功，updatedAt严格前进；调用方发送comment.updatedAt并处理冲突 |
| Comment/相邻广域 | 两个DB URL固定本机55432、显式integration，运行Comment/BUG-082/083/084/FeatureUpdate/ActivityDelta组合 | httpapi包33.567s；database包0.146s | 0 | PASS：评论创建/编辑/删除/树/分页/附件、可靠日志job、活动delta及schema相邻合同全绿 |
| Schema/计划/API边界 | generation源码154；同构表PREPARE+EXPLAIN CAS CTE；前后端DTO复核 | 即时 | 0 | PASS：无DDL/reset/回填/双写/N+1；CTE选择和UPDATE两次均命中comments PK；成功响应不变，PATCH新增必填base，冲突409稳定code/details |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | 20.084s | 0 / 0 / 0 / 0 | PASS：全部普通包、Vet、Build、`go mod tidy -diff`全绿 |
| 前端发布门禁 | 85个Node文件、ESLint、合法HTTPS Next16 production build、build后TypeScript | 210 tests3.969s；静态/构建/类型62.718s | 0 / 0 / 0 / 0 | PASS：210/210、零lint/类型错误、58页production build通过；新增双语冲突恢复合同 |
| 严重度与449台账 | UNRESOLVED→Medium/P1；严格及开放期verifier | 1.352s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=310、OPEN=139；Medium=207、UNRESOLVED=66（High120/Low56不变），strict精确206 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0（仅既有LF/CRLF提示）；模块哈希保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

## 2026-08-23：BUG-091 社区来源语言权威与低置信确认

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效后端/前端RED | source标准化矩阵；第一方editor/API源码合同 | 后端包1.182s、墙钟9.316s；前端0.568s | 1 / 1（预期） | PASS：旧显式`fr_fr`技术文本被覆盖为en-US，显式ko-KR被接受并改写，缺省低置信文本静默en-US；编辑器没有selector、加载回填或选择确认 |
| unit与真实PG GREEN | 8语言显式/非法/缺省置信矩阵；完整临时Schema实际POST→PUT | unit包1.183s、墙钟9.210s；组合包21.084s、墙钟28.846s | 0 | PASS：显式alias保留规范值，unsupported拒绝，缺省技术文拒绝，明确中文检测通过；fr-FR创建→de-DE编辑后主表/current revision snapshot/详情一致，缺省低置信400且零post |
| CommunityPost/本地化广域 | 两个DB URL固定本机55432并显式integration；`CommunityPost`完整组；ContentLocale/Localization/Translation相邻组 | CommunityPost包41.843s；相邻包0.463s | 0 | PASS：创建/编辑/BUG-090 CAS、引用预算、审核、详情、AI翻译源与错误观测全绿；3个旧合法引用夹具补为与第一方同形的显式en-US，未放宽生产校验 |
| Schema/API/检测边界 | generation155临时完整Schema；注册表、alias、检测阈值、下游查询和第一方autosave复核 | 定向检查 | 0 | PASS：无DDL/reset/回填/双写/新查询/N+1；显式source权威，缺省仅高置信回退；成功DTO不变，低置信/非法收紧400；第一方必选站内8语言、编辑/草稿保留，需同批部署 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | Test11.899s；Vet5.681s；Build6.031s；tidy0.800s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；httpapi2.916s、queue3.706s、serverprobe3.158s；模块依赖无diff |
| 前端发布门禁 | 动态枚举86个Node文件；bundled Node PATH下ESLint；合法HTTPS Next16 build；build后TypeScript | Tests4.207s；Lint28.373s；Build25.235s；Type9.742s | 0 / 0 / 0 / 0 | PASS：212/212、零lint/类型错误、58页production build通过 |
| 严重度与449台账 | 既有Medium/P1保持；严格及开放期verifier | 最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，预期CLOSED=317、OPEN=132；High=121、Medium=208、Low=58、UNRESOLVED=62；strict预期195 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

## 2026-08-23：BUG-092 社区翻译 queued 权威状态

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效历史RED与实现归属 | immutable BUG-092证据；LEGACY-019原RED/决策/迁移记录 | LEGACY-019 RED包2.3s | 1（当时预期） | PASS：旧路径在task commit后直发NATS，失败另写DB failed但Handler仍返回入队前queued；共享`enqueueAITaskTx`当时不存在。本项明确复用该已验收实现，不伪造第二次代码RED |
| BUG-092真实PG HTTP专测 | 完整临时Schema、AI配置、Outbox拒绝trigger、成功入队和结果轮询 | 包11.126s；墙钟19.509s | 0 | PASS：事件写失败503且task/event=0/0；成功才202 queued且状态queued/pending；event转retryable failed后task/result poll仍queued，与可恢复事实一致 |
| AI task/Outbox恢复广域 | `ContentTranslationUsesSharedTransactionalAITaskOutbox&#124;AITaskAndOutboxCommitOrRollbackTogether&#124;AITaskRecovery...&#124;CommunityTranslationQueued...` | httpapi包10.142s；墙钟13.135s | 0 | PASS：社区/目录/通用入口无direct Publish；task/event同成同败；orphan/stale恢复、重复扫描与claim CAS全绿 |
| 真实JetStream断线/重连 | `TestOutboxRetriesAfterJetStreamDisconnectIntegration` | queue包2.396s；墙钟4.703s | 0 | PASS：真实发布断线使event failed并记录error/retry指标，task语义不被改写；重连后同一event published并实际消费 |
| Schema/API/相邻边界 | generation155、ai_tasks/nats_outbox/recovery/dead-letter及结果GET复核 | 定向检查 | 0 | PASS：无新DDL/reset/回填/双写；queued=durable acceptance而非published；Outbox失败503/no task，暂时发布失败由event表达；DTO/前端不变；SEC-038、BUG-094、TEST-043未重复关闭 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | Test9.766s；Vet5.599s；Build4.831s；tidy0.797s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；httpapi2.690s、queue3.573s、serverprobe2.866s；模块依赖无diff |
| 前端发布门禁 | 动态枚举86个Node文件；ESLint；合法HTTPS Next16 build；build后TypeScript | Tests5.193s；Lint27.786s；Build24.653s；Type2.677s | 0 / 0 / 0 / 0 | PASS：212/212、零lint/类型错误、58页production build通过；本项无前端代码或协议迁移 |
| 严重度与449台账 | 既有Medium/P1保持；严格及开放期verifier | 最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，预期CLOSED=318、OPEN=131；High=121、Medium=208、Low=58、UNRESOLVED=62；strict预期194 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录零diff、零status |

## 2026-08-23：BUG-093 Accept-Language 权重与明确拒绝

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | `TestFirstAcceptedContentLocaleHonorsQualityWeights&#124;TestPublicContentLocaleCallersShareWeightedNegotiation` | 包1.174s；墙钟9.122s | 1（预期） | PASS：旧解析器在高权重、q=0、wildcard、非法/越界q、未知参数和默认q七个分支错误选择首项；公开内容调用端把`fr;q=0,en;q=.8`解析为fr |
| 定向GREEN | 同一八分支协议矩阵与两个公开调用端 | 包1.174s；墙钟9.134s | 0 | PASS：最高合法正权重胜出，tie稳定、零权重排除、wildcard不伪造具体locale、非法项跳过、默认q与zh-Hant alias正确；调用端分别得到en-US与ja-JP |
| 本地化/目录相邻组 | `ContentLocale&#124;ContentLocalization&#124;SimpleProjectCatalog&#124;Localization&#124;Translation` | 包0.149s；墙钟3.580s | 0 | PASS：解析、内容回退、翻译、目录选择及既有语言注册表相邻行为全绿 |
| Schema/API边界 | generation155；共享调用图、qvalue语法、query/用户偏好优先级复核 | 定向检查 | 0 | PASS：无DDL/reset/回填/双写/新查询/缓存变化；路由/DTO不变；显式query与持久用户偏好优先级不变，无具体候选继续既有默认 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | Test10.646s；Vet3.418s；Build3.770s；tidy0.347s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；httpapi2.816s、queue3.571s、serverprobe2.999s；模块依赖无diff |
| 前端发布门禁 | 动态枚举87个Node文件；ESLint；合法HTTPS Next16 build；build后TypeScript | Tests3.104s；Lint23.909s；Build23.920s；Type2.365s | 0 / 0 / 0 / 0 | PASS：212/212、零lint/类型错误、58页production build通过；本项无前端代码或协议迁移 |
| 严重度与449台账 | 既有Medium/P1保持；严格及开放期verifier | strict0.689s；allow0.649s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=319、OPEN=130；High=121、Medium=208、Low=58、UNRESOLVED=62；strict精确193 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零 |

## 2026-08-24：SEC-021 默认身份启动安全状态

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效unit/真实PG RED | autobot规范状态unit；随机隔离Schema初始化后停用admin/autobot/guest、轮换管理员邮箱/密码、撤销seed grant，再执行旧`seedDefaultUsers` | unit+integration包3.693s；单独PG包1.198s、墙钟3.381s | 1 / 1（预期） | PASS：旧autobot仍是active；旧重复seed把管理员精确恢复为默认邮箱、`email_verified=true`、active，并用`SEED_ADMIN_PASSWORD`生成新Argon2哈希，直接证明Finding |
| 全表清空/旧初始化证据RED | 首版修复后删除全部users再重启；独立空users Schema预置旧`permission.default_roles`后启动 | 包1.473s、墙钟3.749s | 1（预期） | PASS：仅以users空表判定会重新创建4个身份；旧初始化证据也未写marker且仍创建4个，证明“当前为空”不能代表pristine安装 |
| 空库/重启/兼容GREEN | 最终database/httpapi SEC021真实PG组合；并发空库用例额外`-count=10` | database2.132s/httpapi0.393s、墙钟9.275s；并发10轮包3.285s | 0 | PASS：pristine空库一次生成完整集合和marker；双连接并发连续10轮仍精确一组；重复启动保留全部运营状态和撤销授权，部分或全表硬删除均不重建；旧初始化证据只补marker；旧精确sentinel迁为system/auth_version+1且Session撤销，disabled/改密分支零变化 |
| 系统主体/显式恢复边界 | 随机隔离actor Schema走真实`automationActor`权限解析；纯状态矩阵覆盖system/disabled/deleted与active/banned/普通system拒绝 | httpapi同上 | 0 | PASS：worker只接受system身份及其真实seed权限，改成active立即失败；管理员命令可显式恢复system但不能制造交互autobot或普通system用户 |
| 后端发布门禁 | 最终ordinary固定本机DB并移除integration/CGO变量：全仓Test；Vet；Build；tidy | Test16.020s；Vet11.089s；Build11.994s；tidy0.858s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；database0.328s、httpapi2.958s、queue4.091s；模块图未变化 |
| 前端发布门禁 | 直接枚举104个Node测试文件；直接TypeScript；串行ESLint与合法HTTPS Next16 build | Tests3.968s；Type4.435s；Lint25.577s；Build25.706s | 0 / 0 / 0 / 0 | PASS：253/253、零类型/lint错误、58页production build通过；本项无第一方源码变化 |
| Race门环境证据 | `go test -race`默认及`CGO_ENABLED=1`重试 | 0.601s / 28.645s | 1 / 1（环境拒绝） | NOT PASS：默认明确要求CGO；显式CGO后明确找不到`gcc`，均在测试构建前退出。未把普通并发PG测试冒充Race，最终目标的全局Race门保持开放并需先提供C编译器 |
| 严重度与449台账 | UNRESOLVED→High/P1；最终strict及开放期verifier | strict1.251s；allow1.111s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=345、OPEN=104；High=127、Medium=222、Low=59、UNRESOLVED=41；strict精确146 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零；真实PG仅创建并自动drop随机隔离Schema，未访问/重置共享public，未生成pnpm lock残留 |

## 2026-08-24：SEC-022 公开内容指标访客隐私

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | Go反射构造旧DTO并序列化访客ID/用户名/精确时间，同时检查Handler访客查询；Node检查API类型/面板/双语文案 | Go包1.164s、墙钟9.545s；Node0.514s | 1 / 1（预期） | PASS：旧JSON逐字含访客public ID、用户名和`2026-08-24T12:34:56Z`，生产源码执行`content_unique_views→users`最近8人查询；旧第一方公开类型和面板直接消费该数组 |
| DTO/查询/UI GREEN | Go两个`TestSEC022*`；`node --test app/_lib/content-metrics-privacy.test.mts` | Go包1.347s、墙钟10.033s；Node0.539s | 0 / 0 | PASS：公开JSON无`recentViewers`及任何测试访客值，Handler无访客identity query或`view.last_seen_at`；第一方类型、ActorList和中英文文案零残留，聚合/编辑者合同仍存在 |
| 指标相邻组 | `go test ./internal/httpapi -run 'ContentMetric&#124;MetricView&#124;MetricPage&#124;SEC022' -count=1` | 包0.150s、墙钟3.344s | 0 | PASS：公开统计、记录页闭集、来源限流/24h去重及SEC-023相邻安全合同全绿；内部唯一浏览写路径保持 |
| 后端发布门禁 | ordinary固定本机DB并移除integration变量：全仓Test；Vet；Build；tidy | Test10.041s；Vet4.376s；Build5.273s；tidy0.790s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建与模块图全绿；database0.186s、httpapi2.401s、queue3.780s；generation保持162 |
| 前端发布门禁 | 直接枚举105个Node测试文件；TypeScript；串行ESLint与合法HTTPS Next16 build | Tests5.079s；Type4.484s；Lint25.150s；Build24.303s | 0 / 0 / 0 / 0 | PASS：254/254、零类型/lint错误、58页production build通过 |
| Schema/API/兼容边界 | DTO反射、生产查询与调用方检索；generation162 | 定向检查 | 0 | PASS：breaking删除公开个人历史，不保留空字段/别名/双端点；`recentEditors/editors/developers/totalViews`保持；无DDL/reset/回填/数据库写，内部viewer ID仅供去重/聚合排除 |
| 严重度与449台账 | 既有Medium/P1保持；strict及开放期verifier | strict1.095s；allow1.080s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=346、OPEN=103；High=127、Medium=222、Low=59、UNRESOLVED=41；strict精确145 issues，开放期通过 |
| 格式/依赖/不可变边界 | 双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零；无Schema生成或数据库操作 |

## 2026-08-24：SEC-024 收藏夹MRPack并发配额

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | 隔离随机Schema、每组8连接同时COUNT，配额成功后故意保持80ms提交窗口；active/daily使用不同collection，duplicate使用相同组合 | 包1.724s、墙钟9.911s | 1（预期） | PASS：普通事务active接受4/上限2、daily接受8/上限3、duplicate接受8/上限1；源码门同时确认没有用户配额锁，直接证明三个并发绕过分支 |
| 原子配额GREEN | `TestSEC024FavoriteExportQuotaIsAtomicAcrossConcurrentTransactionsIntegration`与Handler锁/插入顺序源码合同 | 包1.894s、墙钟10.097s | 0 | PASS：三组分别只接受2、3、1，其他事务精确得到active/daily/duplicate sentinel；持久task数与接受数一致，锁先于COUNT且task insert在同一tx的reserve之后 |
| 重复并发稳定性 | 同一真实PG测试`-count=10`，每轮3组×8事务 | 包8.192s、墙钟10.962s | 0 | PASS：连续240个事务均精确收敛，无偶发超额、未知数据库错误或连接泄漏；随机Schema自动drop |
| MRPack相邻完整集成 | `go test ./internal/httpapi -run 'Favorite(Modpack&#124;Export)&#124;SEC024' -count=1`，两个DB URL固定本机并显式integration | 包97.611s | 0 | PASS：预览快照、依赖图、重建、收藏生命周期、结果完整性、路径冲突、历史分页、错误可见性及新配额全部通过，无锁顺序回归 |
| 后端发布门禁 | ordinary固定本机DB且移除integration变量：全仓Test；Vet；Build；tidy | Test14.533s；Vet10.519s；Build11.456s；tidy0.625s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建与模块图全绿；httpapi2.603s、queue3.718s，generation保持162 |
| 前端发布门禁 | 复用本轮SEC-022后且本项未再变化的前端完整门：105文件Node、Type、ESLint、合法HTTPS Next16 build | Tests5.079s；Type4.484s；Lint25.150s；Build24.303s | 0 / 0 / 0 / 0 | PASS：254/254、零类型/lint错误、58页production build；SEC-024请求/响应无前端变化 |
| Schema/API/独立边界 | task唯一生产写入口、lock/count/insert调用图、现有429映射与generation162 | 定向检查 | 0 | PASS：无DDL/quota双写；三个429 code与Retry-After不变；只关闭创建配额，OPS-013、TEST-034及其他MRPack项保持独立 |
| 严重度与449台账 | 既有High/P1保持；strict及开放期verifier | strict0.966s；allow1.087s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=347、OPEN=102；High=127、Medium=222、Low=59、UNRESOLVED=41；strict精确144 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零；未写共享数据库 |

## 2026-08-24：SEC-026 Sponge调色板分配边界

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效行为/源码RED | 负/重复/稀疏/float/8193索引/BlockData越界、8193项palette、tiny gzip与旧分配源码合同 | 包1.223s、墙钟9.415s | 1（预期） | PASS：负数实际panic；其余全部被旧解码接受；压缩样本不足1KiB仍能以索引选择分配；源码明确存在`maxPalette+1`和不可信下标写入 |
| 安全矩阵与合法GREEN | 四个`TestSEC026*`加`BlueprintCodec`三格式往返 | 包1.183s、墙钟9.348s | 0 | PASS：所有畸形输入在分配/BlockData遍历边界返回error；合法Sponge v2/v3各解出1方块，nbt/schem/litematic round-trip保持 |
| 重复稳定性 | `go test ./internal/httpapi -run '^TestSEC026' -count=20` | 包0.208s、墙钟3.800s | 0 | PASS：恶意map迭代顺序变化不能绕过重复/稀疏检查，连续20轮无panic或接受 |
| 蓝图广域含真实PG | 两个DB URL固定本机并显式integration，`go test ./internal/httpapi -run 'Blueprint&#124;SEC026' -count=1` | 包21.010s、墙钟24.332s | 0 | PASS：编解码、上传、任务租约/恢复、实体保真、转换、材料、cover/cache及隔离Schema链全部通过 |
| 后端发布门禁 | ordinary固定本机DB且移除integration变量：更正后的全仓Test；Vet；Build；tidy | Test14.744s；Vet10.542s；Build11.392s；tidy0.750s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建与模块图全绿；httpapi2.472s、queue3.738s，generation保持162；首次包装命令多传`go`并在测试前拒绝，未计作产品结果 |
| 前端发布门禁 | 复用本轮SEC-022后且SEC-024/026均未改变的前端完整门 | Tests5.079s；Type4.484s；Lint25.150s；Build24.303s | 0 / 0 / 0 / 0 | PASS：105文件、254/254、零类型/lint错误、58页production build；本项无协议/第一方变化 |
| 严重度/独立边界 | UNRESOLVED→High/P1；SEC-027/PERF-042/TEST-036继续OPEN | 逐项复核 | 0 | PASS：小输入可造成可重复进程内存/panic，但需认证异步任务且无权限提升/泄漏；未用本项冒充关闭公共转换配额、渲染预算或端到端矩阵 |
| 449台账 | strict与`-allow-open` verifier | strict1.079s；allow1.054s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=348、OPEN=101；High=128、Medium=222、Low=59、UNRESOLVED=40；strict精确142 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零；无Schema生成或数据库写 |

## 2026-08-24：SEC-027 公共蓝图转换原子全局容量

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | 随机隔离Schema、24个不同用户/蓝图同时经实际`enqueueBlueprintJobTx`入队并在commit前保持30ms窗口；源码/API/索引分类合同 | 包2.339s、墙钟10.569s | 1（预期） | PASS：旧路径24/24全部接受、零全局拒绝，直接证明每用户限额与同目标唯一索引无法形成站点容量；源码没有全局锁/计数或稳定429 |
| 原子全局准入GREEN | `TestSEC027PublicBlueprintConversionsShareOneAtomicGlobalBudgetIntegration`及源码/API合同 | 包2.322s、墙钟10.628s | 0 | PASS：24个不同账号/目标精确16个task与16个Outbox提交，另外8项得到全局容量sentinel；锁覆盖全局检查、用户检查、task/Outbox及commit |
| 并发重复稳定性 | 同一24事务真实PG测试`-count=10` | 包11.737s、墙钟15.164s | 0 | PASS：连续240个事务每轮均精确16接受/8拒绝，无跨实例式旧计数穿透、未知数据库错误或连接泄漏；随机Schema自动drop |
| 蓝图相邻真实PG | 两个DB URL固定本机并显式integration，`go test ./internal/httpapi -run 'Blueprint&#124;SEC027' -count=1` | 包26.509s、墙钟29.876s | 0 | PASS：公开转换、上传/normalize、任务租约/恢复、编解码、实体保真、材料、cover/cache及新增容量全部通过；normalize未被公共全局队列错误阻断 |
| 百万历史任务计划/最终SEC027组 | 1M completed历史任务、16 active，运行全部SEC027并EXPLAIN实际准入查询 | 包13.221s、墙钟21.706s | 0 | PASS：计划命中`idx_sec027_active_operation`测试镜像部分索引，返回16行；planning约3.412ms、execution 0.221ms，无`blueprint_jobs` Seq Scan；精确23505约束分类矩阵全绿 |
| 后端发布门禁 | ordinary固定本机DB且移除integration变量：全仓Test；Vet；Build；tidy | Test20.800s；Vet10.252s；Build10.971s；tidy0.397s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建与模块图全绿；httpapi3.352s、queue4.399s，generation保持162 |
| 前端发布门禁 | 复用本轮SEC-022后且SEC-024/026/027均未改变前端生产代码的完整门 | Tests5.079s；Type4.484s；Lint25.150s；Build24.303s | 0 / 0 / 0 / 0 | PASS：105文件、254/254、零类型/lint错误、58页production build；公开转换入口和成功DTO不变 |
| Schema/API/严重度边界 | generation162、两级锁/索引/Handler映射调用图；UNRESOLVED→High/P1 | 定向检查 | 0 | PASS：无DDL/quota双写；新增全站满载429，只精确active-operation约束映射409；公共能力保持且无对象数据越权，TEST-036等相邻Finding不合并关闭 |
| 449台账 | strict与`-allow-open` verifier | strict0.912s；allow0.905s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=349、OPEN=100；High=129、Medium=222、Low=59、UNRESOLVED=39；strict精确140 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零；无Schema生成或共享/远端数据库写入 |

## 2026-08-24：SEC-028 OSS用户额度失败关闭与并发完成

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 同缺陷历史有效RED | PERF-044修复前先加入热路径零SUM、预留/结算及generation127双桶合同并运行OSS quota目标 | HTTP1.144s；Database0.496s | 1 / 1（预期） | PASS：旧预签名直接SUM历史、Schema无额度事实且查询错误被视为0；该RED逐字覆盖SEC-028原始证据，当前项不伪造第二次生产回退 |
| SEC028直接完成/故障GREEN | 两个随机隔离Schema运行`go test ./internal/httpapi -run '^TestSEC028' -count=1 -v` | 包1.924s、墙钟9.893s | 0 | PASS：无预留的两笔600/1000并发完成精确一成一拒、仅1文件且总/日active均600；删除日桶关系后预留/结算error、事务零残留并映射500，不把故障当0 |
| SEC028重复稳定性 | 同一真实PG矩阵`-count=20`，每轮创建并自动drop两个随机隔离Schema | 包9.166s、墙钟11.623s | 0 | PASS：连续40个竞态/故障组均保持原子结果，无偶发双成、计数漂移、错误业务分类、连接或Schema泄漏 |
| OSS额度/Schema广域 | 两个DB URL固定本机并显式integration，运行`^(TestSEC028&#124;TestOSS.*Quota)`覆盖httpapi/database | HTTP11.817s；Database0.105s；墙钟14.357s | 0 | PASS：预签名600+600精确一成一拒；预留/settle/trigger/改型/删除/rebuild、checked arithmetic、主链源码和generation162合同全绿；1M历史桶读2.1706ms且不触碰历史陷阱 |
| 后端发布门禁 | ordinary固定本机DB且移除integration变量：全仓Test；Vet；Build；tidy | Test9.511s；Vet5.810s；Build5.042s；tidy0.407s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建与模块图全绿；httpapi2.676s、queue3.945s、serverprobe4.328s，generation保持162 |
| 前端发布门禁 | 复用本轮SEC-022后且SEC-024/026/027/028均未改变前端生产代码的完整门 | Tests5.079s；Type4.484s；Lint25.150s；Build24.303s | 0 / 0 / 0 / 0 | PASS：105文件、254/254、零类型/lint错误、58页production build；用户上传协议与额度文案不变 |
| Schema/API/严重度边界 | generation127→当前162额度事实、预签名/完成调用图、故障映射；UNRESOLVED→High/P0 | 定向检查 | 0 | PASS：本轮无DDL/reset/双路径；SEC-028与PERF-044共享唯一生产修复但保留独立安全证据；ARCH-020、BUG-079/080、OPS-018、TEST-037不合并关闭 |
| 449台账 | strict与`-allow-open` verifier | strict0.774s；allow0.780s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=350、OPEN=99；High=130、Medium=222、Low=59、UNRESOLVED=38；strict精确138 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零；随机Schema全自动drop且未访问共享public/远端数据库 |

## 2026-08-24：SEC-029 私有OSS短期签名访问

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效后端RED | 新增旧ESA模式签名/对象绑定/60分钟及安全默认合同，运行`go test ./internal/httpapi -run '^TestSEC029' -count=1 -v` | 包1.137s、墙钟9.062s | 1（预期） | PASS：旧解析精确返回`mode=esa_private_origin`与稳定PublicEndpoint URL、无signature；空/ESA/未知设置均默认不安全模式，直接复现审计证据 |
| 签名与配置GREEN | SEC029两项加既有`ResolveOSSObjectAccess/Stored`组 | 包1.313s、墙钟9.222s | 0 | PASS：旧配置立即变`oss_presigned`；URL含OSS signature/expires且不使用ESA host，Content-Disposition进入签名；不同对象signature不同，24小时请求的ExpiresAt≤60分钟，外部非OSS URL保持 |
| 重复与第一方合同 | 后端SEC029 `-count=20`；Node `oss-private-access.test.mts`；生产目录ESA选项零命中 | Go包0.135s、墙钟2.563s；Node0.203s | 0 / 0 | PASS：40个签名/配置用例稳定；后台/default/payload只使用`oss_presigned`，TTL控件max60，双语说明稳定域名非授权。首次Node断言只因中文词序过窄失败，改为逐字业务文案后通过，非产品回退 |
| OSS广域 | 两个DB URL固定本机并显式integration，`go test ./internal/httpapi -run '(OSS&#124;SEC029)' -count=1` | 包15.606s、墙钟18.080s | 0 | PASS：访问/上传/配额/分片/删除/迁址/绑定/栅格/管理及其真实PG组全绿；旧稳定访问分支零残留 |
| 后端发布门禁 | ordinary固定本机DB且移除integration变量：全仓Test；Vet；Build；tidy | Test19.098s；Vet14.153s；Build15.345s；tidy0.548s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建与模块图全绿；httpapi2.960s、queue4.056s、serverprobe4.763s，generation保持162 |
| 前端发布门禁 | 动态枚举106个Node测试；TypeScript；ESLint；合法HTTPS Next16 build | Tests6.592s；Type20.335s；Lint31.878s；Build30.069s | 0 / 0 / 0 / 0 | PASS：255/255、零类型/lint错误、58页production build；Build编译9.5s、内置Type14.2s |
| Schema/API/严重度边界 | generation162、旧设置归一、全调用方/稳定引用/签名边界；UNRESOLVED→High/P1 | 定向检查 | 0 | PASS：无DDL/远端操作；字段保持但mode单值、URL/TTL有意收紧；TEST-037真实供应商过期矩阵不合并关闭 |
| 449台账 | strict与`-allow-open` verifier | strict0.775s；allow0.757s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=351、OPEN=98；High=131、Medium=222、Low=59、UNRESOLVED=37；strict精确136 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零；无Schema生成、数据库访问或远端写入 |

## 2026-08-24：SEC-030 GIF完整动画安全预算

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效后续帧RED | 构造合法2帧GIF后仅删除尾部2字节；先证明旧`gif.Decode`仍接受，再运行`validateRasterImageBytes` | 包1.103s、墙钟9.151s | 1（预期） | PASS：旧通用校验只解码首帧并把后续帧截断对象判为可信，精确复现SEC-030证据；不是编译失败或伪造生产回退 |
| SEC030完整动画GREEN | `go test ./internal/httpapi -run '^TestSEC030' -count=1 -v` | 包1.402s、墙钟9.758s | 0 | PASS：合法2帧通过，后续截断、121帧、40秒、17×2048²累计像素均拒绝；逻辑画布仍受16,777,216像素界限 |
| 分配前预算与重复稳定性 | 增加畸形压缩数据但descriptor累计超64M的直接helper断言；SEC030两项`-count=20` | 包5.198s、墙钟13.147s | 0 | PASS：连续40组稳定；返回descriptor budget错误后再证明同fixture无法DecodeAll，锁定“预扫描预算早于完整解码”顺序，避免解码后才统计的分配窗口 |
| 光栅/贴纸/封面广域 | Raster、WebP、BlueprintCover、persisted image、SEC030与Sticker sanitization目标组 | 包0.336s、墙钟2.720s | 0 | PASS：完整PNG/WebP失败关闭、可信OSS绑定、蓝图同步校验、贴纸PNG/GIF元数据净化及类型/截断拒绝全部通过；贴纸共用新预预算入口 |
| 后端发布门禁 | ordinary固定本机DB且移除integration变量：全仓Test；Vet；Build；tidy | Test11.225s；Vet3.502s；Build3.801s；tidy0.346s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建与模块图全绿；httpapi2.693s、queue3.747s，generation保持162 |
| 前端发布门禁 | 复用紧邻SEC-029且本项零前端生产/测试改动的完整门 | Tests6.592s；Type20.335s；Lint31.878s；Build30.069s | 0 / 0 / 0 / 0 | PASS：106文件、255/255、零类型/lint错误、58页production build；HTTP DTO与正常图片协议未变 |
| Schema/API/严重度边界 | generation162、通用与贴纸GIF调用图、预算/解码顺序；UNRESOLVED→High/P1 | 定向检查 | 0 | PASS：无DDL、数据库/远端访问或第一方迁移；非法动画为breaking validation；SEC-019和TEST-037不合并关闭 |
| 449台账 | strict与`-allow-open` verifier | strict0.651s；allow0.643s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=352、OPEN=97；High=132、Medium=222、Low=59、UNRESOLVED=36；strict精确134 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零；本项未访问数据库或远端服务 |

## 2026-08-24：SEC-031 评论子资源目标可见性闭包

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | 随机临时Schema预置pending Mod、访客既有评论/watch，运行`^TestSEC031CommentSubresources` | 包11.225s、墙钟19.569s | 1（预期） | PASS：旧replies返回完整秘密正文，reaction写入1行，watch/watch-item均200，列表返回root正文/作者/reaction和空target壳；五个失败逐条命中审计证据 |
| SEC031授权GREEN | 同一真实PG矩阵加入edit/pin零副作用、owner与恢复公开正向路径后运行 | 包11.326s、墙钟19.150s | 0 | PASS：不可见replies/reaction/watch/watch-item/edit/pin均404，列表items=0，正文/置顶/reaction零变化；target owner仍读pending，approved后普通访客恢复 |
| 重复与源码闭集 | 隔离Schema主矩阵`-count=5`；七类单项路由/附件/watch-item及列表目标先于正文的源码合同`-count=20` | PG包51.354s、墙钟53.803s；源码包1.128s、墙钟10.103s | 0 / 0 | PASS：五轮独立Schema全自动drop；所有commentId子路由共享visible boundary，旧`numericCommentID`和空target降级零残留，无偶发写入/泄漏 |
| Comment完整广域 | 经正则校验的随机本机空数据库运行`go test ./internal/httpapi -run 'Comment' -count=1`，finally精确drop | 包46.723s、墙钟49.342s | 0 | PASS：全部Comment测试无排除通过，覆盖tree、floor、分页/百万计划、target batch、block、回复能力、附件删除、编辑冲突、读取故障和权限注解；临时数据库删除后残留0 |
| 夹具迁移 | BUG083/084最小临时表补充root/项目/public route可见性事实；两项定向 | 包1.492s、墙钟10.243s | 0 | PASS：不绕过共享入口；附件删除首轮故障仍原子回滚/重试成功，编辑stale/missing/retry合同保持。共享public generation155首次广域失败未记通过，后由随机空库完整替代 |
| 后端发布门禁 | ordinary固定本机DB且移除integration变量：全仓Test；Vet；Build；tidy | Test11.638s；Vet3.521s；Build3.827s；tidy0.341s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建与模块图全绿；httpapi2.592s、queue3.727s，generation保持162 |
| 前端发布门禁 | 复用紧邻SEC-029且SEC030/031均零前端生产改动的完整门 | Tests6.592s；Type20.335s；Lint31.878s；Build30.069s | 0 / 0 / 0 / 0 | PASS：106文件、255/255、零类型/lint错误、58页production build；现有客户端已处理404/空列表，DTO未变 |
| Schema/API/严重度边界 | generation162、全comment子路由与批量调用图；UNRESOLVED→High/P1 | 定向检查 | 0 | PASS：无DDL/N+1/远端操作；不可见200/403→404与列表省略是授权收紧；SEC-032/033和TEST-038不合并关闭 |
| 449台账 | strict与`-allow-open` verifier | strict0.649s；allow0.648s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=353、OPEN=96；High=133、Medium=222、Low=59、UNRESOLVED=35；strict精确132 issues，开放期通过 |
| 格式/依赖/临时资源/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、`pg_database/pg_namespace`残留、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；`sec031_%`数据库/Schema均0；权威audit目录限定diff/status均为零，未修改共享public或远端数据库 |

## 2026-08-24：SEC-032 评论插眼硬额度原子准入

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双连接RED | 随机Schema预置用户7的1999条active；两连接对不同comment同步准入，BEFORE INSERT延迟放大旧读取窗口 | 包1.678s、墙钟9.563s | 1（预期） | PASS：旧`COUNT→query→upsert`精确2成功、0限额、最终active=2001，唯一user/comment约束不能保护集合基数，直接复现SEC-032 |
| 事务锁GREEN | 用户64位advisory xact lock内执行active幂等、bounded count、upsert、状态复读和commit | 包1.710s、墙钟9.631s | 0 | PASS：同一1999竞态精确1成功/1 typed limit/active=2000；首轮实现测试曾因`$1::text`使pgx无法编码int64而失败，改为先声明bigint再转text后通过，未掩盖产品回退 |
| 20轮/满额边界 | `go test ./internal/httpapi -run '^TestSEC032' -count=20`；每轮随机Schema自动drop | 包12.703s、墙钟20.542s | 0 | PASS：40个并发结果均保持1/1/2000；2000时已active关系仍成功，另一新关系稳定limit，无偶发2001、死锁、连接或Schema泄漏 |
| Comment/SEC032完整广域 | 正则校验随机空数据库运行`go test ./internal/httpapi -run '(Comment&#124;SEC032)' -count=1`，finally精确drop | 包48.938s、墙钟51.491s | 0 | PASS：评论树/闭包、CY命令、watch reply计数与通知、target visibility/batch、分页/计划、附件/编辑/权限全部通过；临时数据库已删除 |
| 后端发布门禁 | ordinary固定本机DB且移除integration变量：全仓Test；Vet；Build；tidy | Test11.661s；Vet3.685s；Build3.913s；tidy0.361s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建与模块图全绿；httpapi2.653s、queue3.753s，generation保持162 |
| 前端发布门禁 | 复用紧邻SEC-029且SEC030-032均零前端生产改动的完整门 | Tests6.592s；Type20.335s；Lint31.878s；Build30.069s | 0 / 0 / 0 / 0 | PASS：106文件、255/255、零类型/lint错误、58页production build；成功DTO和原满额文案不变 |
| Schema/API/严重度边界 | generation162、ensure/CY/direct watch调用图、索引前缀；UNRESOLVED→High/P1 | 定向检查 | 0 | PASS：无DDL/独立计数/N+1/远端操作；计数最多2000索引项，CY满额500→400为错误分类修正；SEC-033/TEST-038不合并关闭 |
| 449台账 | strict与`-allow-open` verifier | strict0.673s；allow0.651s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=354、OPEN=95；High=134、Medium=222、Low=59、UNRESOLVED=34；strict精确130 issues，开放期通过 |
| 格式/依赖/临时资源/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、`pg_database/pg_namespace`残留、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；`sec032_%`数据库/Schema均0；权威audit目录限定diff/status均为零，未修改共享public或远端数据库 |

## 2026-08-24：SEC-033 公开评论线程邻域与响应硬预算

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 新增SEC033 Go边界/真实PG测试与前端cursor合同，分别运行目标组 | Go墙钟2.231s；Node0.123s | 1 / 1（预期） | PASS：旧后端缺少节点/路径/字节类型与helper并编译失败，旧前端接口无cursor/nextCursor且分支页只单次加载；直接命中审计缺口，未以伪生产回退制造失败 |
| 真实PG邻域GREEN | TEMP表预置25祖先、200直接回复及1个viewer-blocked作者；运行全部SEC033 | 包0.255s、墙钟2.812s；完整Handler加入后包16.846s、墙钟24.722s | 0 | PASS：首面16个最近祖先+47回复，后续64节点keyset页，199个可见回复完整遍历、零重复，blocked作者零泄漏；源码闭集确认旧root/includeTree调用删除 |
| 实际响应预算 | 完整generation162会话临时Schema、approved Mod、root及200个约10k正文回复，直接调用真实thread Handler逐页到终止 | 包15.0s级/轮；单轮包含于SEC033 16.846s | 0 | PASS：首面含focus，200回复全部可达且零重；每页≤64节点、完整API envelope≤512KiB，字节裁剪后cursor从最后实际返回行继续；临时Schema自动drop |
| 重复稳定性 | 查询/编码/源码合同`-count=20`；完整Handler+全Schema+200大正文`-count=3` | 包2.300s、墙钟4.757s；包45.445s、墙钟47.947s | 0 / 0 | PASS：20轮visible keyset和字节裁剪稳定；3轮真实端点全部遍历结束，无死循环、重复、超节点、超字节或临时Schema残留 |
| Comment完整广域 | 首次共享public因generation155与当前162不兼容精确失败，未记通过；随后创建正则命名隔离空数据库运行`go test ./internal/httpapi -run '(Comment&#124;SEC033)' -count=1`并finally强制drop | 包64.523s、墙钟67.113s | 0 | PASS：tree/closure/CY/watch/target visibility/batch/分页/附件/编辑/权限及SEC033全绿；`sec033_comment_20260824_001`已删除，未重置或修改共享public |
| 后端发布门禁 | ordinary本机DB、移除integration变量：全仓Test；Vet；Build；tidy | Test11.503s；Vet3.679s；Build3.813s；tidy0.342s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建和模块图全绿；httpapi2.660s、queue3.692s，generation保持162 |
| 前端发布门禁 | 动态枚举107个Node文件；TypeScript；ESLint；合法HTTPS Next16 production build | Tests4.119s；Type/Lint最终组合27.3s；Build24.343s | 0 / 0 / 0 / 0 | PASS：256/256、零类型/lint错误、58页构建通过。首轮Lint有效发现effect同步setState并失败；改为评论+登录主体结果身份后重跑Type/Lint全绿，旧页不能跨评论/账号回写 |
| Schema/API/严重度边界 | generation162、ancestor/parent索引前缀、候选先于装配与实际编码上限；UNRESOLVED→High/P1 | 定向检查 | 0 | PASS：无DDL/reset/回填/全树扫描/N+1/远端操作；新增nextCursor/pathTruncated且第一方同批消费；PERF-045/046、ARCH-022不重复关闭，TEST-038保持独立 |
| 449台账 | strict与`-allow-open` verifier | strict0.650s；allow0.643s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=355、OPEN=94；High=135、Medium=222、Low=59、UNRESOLVED=33；strict精确128 issues，开放期通过 |
| 格式/依赖/临时资源/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、数据库残留、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；SEC033隔离数据库0残留；权威audit目录限定diff/status均为零，未修改共享public或远端数据库 |

## 2026-08-24：SEC-034 私有皮肤与玩家档案组合授权

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | generation162完整会话临时Schema构造owner private asset、public/private档案及无关viewer；运行`TestSEC034PrivateSkinProfilePrivacyMatrixIntegration` | 包10.849s、墙钟19.2s | 1（预期） | PASS：旧匿名及无关登录主体都精确得到private asset、64位hash和公开texture URL；private档案显式改public返回200并继续携带完整private纹理，直接命中Finding而非测试夹具失败 |
| 隐私组合GREEN | 同一真实Handler/loader矩阵，增加public/unlisted历史脏绑定、private详情、approved/pending及owner例外；三类冲突写 | 包10.888s、墙钟19.3s | 0 | PASS：匿名/无关viewer的public与unlisted响应均`skin=null`且零asset/hash/URL；private档案匿名404；owner仍读取自己的private/pending纹理，approved public对普通viewer仍可见 |
| 写入与事务不变量 | 真实PG调用档案PUT、纹理PUT及生产`applySkinAssetSnapshotTx`；冲突后直查关系/visibility | 包含于SEC034 10.888s | 0 | PASS：private档案→public、public档案装备private、owner public/unlisted档案使用中资产→private全部返回typed 409/error；档案visibility、装备关系和asset visibility均零部分写，审核批准复用相同检查 |
| Skin/Localized相邻广域 | 首次公共库因generation155≠162失败未记通过；正则命名隔离数据库先安装162、补该旧BUG-088测试明确假设的active fixture，再运行SEC034/Profile/Skin创建/共享Blob/Localized组并finally drop；另跑LocalizedAsset原子测试 | 初始化3.740s；广域11.860s；Localized包0.329s | 0 / 0 / 0 | PASS：隐私过滤未恢复档案N+1，1/100档案仍2 SQL；创建审核、shared blob引用/补偿/锁序和本地化原子回滚全绿；第一次空库广域只暴露BUG-088夹具`select active user`无行，补临时fixture后通过，非本项生产失败 |
| 后端发布门禁 | ordinary本机DB、移除integration变量：全仓Test；Vet；Build；tidy | Test14.550s；Vet10.747s；Build11.207s；tidy0.750s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建和模块图全绿；httpapi3.469s、queue4.011s，generation保持162 |
| 前端发布门禁 | 动态枚举107个Node文件；TypeScript；合法HTTPS Next16 production build；最终有效ESLint | Tests6.295s；Type5.226s；Build26.309s；Lint24.7s | 0 / 0 / 0 / 0 | PASS：256/256、零类型/lint错误、58页构建通过；成功DTO与第一方`errorMessage/notifySite`协议不变。一次把pnpm参数分隔符误写为额外`--`只导致launcher找不到文件，未作为Lint结果，纠正命令后有效门禁通过 |
| Schema/API/严重度边界 | generation162、batch/锁序/审核调用图；UNRESOLVED→Medium/P1 | 定向检查 | 0 | PASS：无DDL/reset/回填/双写/N+1/远端操作；不按共享blob反向伪造ACL，不承诺撤回既往公开hash；公开/未列出档案只接受approved非private纹理，TEST-039其余矩阵不合并关闭 |
| 449台账 | strict与`-allow-open` verifier | strict0.968s；allow0.958s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=356、OPEN=93；High=135、Medium=223、Low=59、UNRESOLVED=32；strict精确126 issues，开放期通过 |
| 格式/依赖/临时资源/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、数据库残留、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；`sec034_skin_20260824_001/_002`均已drop且查询0；权威audit目录限定diff/status均为零，未修改共享public或远端数据库 |

## 2026-08-24：SEC-036 收藏夹与收藏关系长期库存原子配额

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效compile RED | 新增集合/条目并发、净增量和实际Handler测试后，运行`go test ./internal/httpapi -run '^TestSEC036' -count=1` | 包构建2.555s | 1（预期） | PASS：旧代码缺`favoriteQuotaPolicy`、用户数据库锁、集合增长与PATCH/PUT净增量原语，编译错误逐一命中审计要求而非夹具故障 |
| 原子并发与快照GREEN | 随机Schema中集合/条目各12路并发、上限3；另让Repeatable Read contender实际等待前一xact锁后再读quota | 初版包2.050s；最终定向包12.164s | 0 | PASS：两类各精确3成功/9 typed拒绝；等待者在前事务commit后开始可见性快照并读到已提交关系，上限1时拒绝第二条，证明未冻结锁前旧快照 |
| 实际四入口与净增量 | generation162完整会话临时Schema调用create/default GET/PATCH/PUT；直接quota矩阵覆盖幂等、平衡迁移、正增长和配置下调后的清理 | 包含于最终12.164s | 0 | PASS：集合与关系超限分别409稳定code且零超额行；满额幂等PATCH/PUT、等量collection move与超额库存清空均允许，非法集合仍先验证且BUG-089关系不变 |
| 百万行/BUG-089定向复核 | 初次广域发现default路径增加到5查询且BUG-089旧测试误用generation155共享public，均不记通过；增加已有default只读快路并把BUG-089迁入临时162后重跑两项及SEC036 | 包42.529s | 0 | PASS：1M收藏夹页恢复固定2查询且无fanout；BUG-089完整真实Handler在会话临时Schema通过，无共享public迁移/reset；两项均为测试发现后真实修正而非放宽断言 |
| Favorite完整真实PG广域 | 本机两个DB URL+integration运行`go test ./internal/httpapi -run 'Favorite&#124;SEC036&#124;BUG089&#124;SEC035' -count=1` | 包150.776s | 0 | PASS：百万集合/条目索引计划、pagination/summary/delta、动态可见性、原子替换、错误分类及MRPack收藏导出全部绿；预期Worker故障日志不构成失败 |
| 后端发布门禁 | ordinary本机DB、移除integration变量：全仓Test；Vet；Build；tidy diff | Test21.161s；Vet12.384s；Build15.497s；tidy0.917s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建和模块图全绿；httpapi7.485s、queue9.281s，generation保持162 |
| 前端未受影响发布门禁 | 动态枚举107个Node文件；TypeScript；ESLint；合法HTTPS Next16 production build | Tests5.276s；Type5.112s；Lint27.486s；Build24.857s | 0 / 0 / 0 / 0 | PASS：256/256、零类型/lint错误、58页production build通过；成功DTO和第一方收藏类型未变，稳定API error由既有边界展示 |
| Schema/API/严重度边界 | generation162、四入口调用图、同键session/xact锁、配置/硬界和有界计数复核；保持Medium/P1 | 定向检查 | 0 | PASS：无DDL/reset/回填/计数双写/远端操作；PERF-051、SEC-035和BUG-089合同保持，TEST-041不合并关闭 |
| 449台账 | strict与`-allow-open` verifier | 最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=357、OPEN=92；High=135、Medium=223、Low=59、UNRESOLVED=32；strict只因其余开放/待定及最终严重度目标退出，开放期通过 |
| 格式/依赖/临时资源/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、数据库/锁残留、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；SEC036随机/临时Schema与session advisory lock零残留；权威audit目录限定diff/status均为零，未修改共享public或远端数据库 |

## 2026-08-24：SEC-001 PlantUML外部服务隐私边界

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 新Go默认/存量/路径/Handler合同与Node默认/CSP/浏览器面合同；分别运行精确SEC001测试 | Go构建2.8s；Node0.121s | 1 / 1（预期） | PASS：Go缺少路径验证原语；前端明确得到默认true，工具含公共URL且CSP允许该域，两个失败均直接命中审计缺口 |
| 后端隐私GREEN | `go test ./internal/httpapi -run '^TestSEC001' -count=1` | 包1.127s；墙钟9.6s | 0 | PASS：默认关闭/同站路径、存量外部值失败关闭、外部/协议相对/查询/fragment/其他路径拒绝、Handler持久化前400全部通过 |
| 前端与CSP GREEN | `node --test app/_lib/sec001-plantuml-privacy.test.mts app/_lib/csp-policy.test.mts` | 0.162s | 0 | PASS：6/6；默认及漂移归一化、工具/Renderer/Admin公共域零命中、同站路径、只读提示、部署文档和精确CSP合同全绿 |
| 生产外送闭集 | 双仓生产源码检索公共PlantUML域；Markdown/工具/config/CSP/admin调用图 | 定向检查 | 0 | PASS：排除防回归字面量后公共域零生产命中；浏览器只能构造`/plantuml/svg/...`，Markdown默认不构造请求，CSP不再内建公共服务 |
| 后端发布门禁 | ordinary本机DB且移除integration变量：全仓Test；Vet；Build；tidy | Test17.818s；Vet12.583s；Build13.898s；tidy0.785s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建和模块图全绿；httpapi3.697s、queue4.007s，generation保持162 |
| 前端发布门禁 | 动态枚举108个Node文件；TypeScript；ESLint；合法HTTPS Next16 production build | Tests5.027s；Type10.4s；Lint25.576s；Build29.614s | 0 / 0 / 0 / 0 | PASS：258/258、零类型/lint错误、58页production build通过；新增管理双语键和只读配置完整编译 |
| Schema/API/严重度边界 | generation162；默认/存量/PUT/部署回滚调用图；保持Medium/P1 | 定向检查 | 0 | PASS：无DDL/reset/回填/数据库访问/查询计划；GET字段兼容，外部server PUT有意收紧400；部署只支持同站自建代理，未请求任何外部PlantUML服务 |
| 449台账 | strict与`-allow-open` verifier | strict0.975s；allow1.109s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=358、OPEN=91；High=135、Medium=223、Low=59、UNRESOLVED=32；strict精确124 issues，只因其余开放/待定及最终严重度目标退出，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零，未访问共享public或远端服务 |

## 2026-08-24：SEC-002 依赖漏洞扫描、报告与发布阻断

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双扫描RED | 固定`govulncheck v1.7.0 ./...`；以npm12.0.2读取当前`package-lock.json`执行`npm audit --audit-level=high --json` | Go8.626s；npm约22.0s | 1 / 1（预期） | PASS：Go精确7条affected（x/image1、标准库6）且指出修复版；npm精确3条High（brace-expansion/js-yaml/nanoid），证明原“未知”实际包含发布风险 |
| 依赖升级GREEN | Go1.26.6/x-image0.45/compress1.18.7后govulncheck；锁更新后npm audit JSON | Go8.923s；npm3.593s | 0 / 0 | PASS：Go affected=0、imported package=0，npm info/low/moderate/high/critical均0；x/crypto/openpgp仅未导入/未调用模块公告且无fix，verbose报告继续可见 |
| Workflow静态执行合同 | actionlint v1.7.12校验双仓workflow；Go/Node源码合同锁定触发器、完整Action SHA、官方命令和scan→archive→enforce顺序 | actionlint9.293s（含安装）；Go包1.274s；Node0.125s | 0 / 0 / 0 | PASS：两个YAML无语法/表达式/Action输入错误；扫描失败仍上传30天JSON后最终非零，报告缺失也失败 |
| 依赖兼容回归与修正 | 首次全局brace5.0.9后ESLint；删除过宽override、恢复分层lock后定向合同+ESLint | 初次10.768s；最终24.025s | 1（有效）/ 0 | PASS：首次真实命中旧minimatch的`expand is not a function`；最终top-level 5.0.9、旧依赖1.1.18，漏洞区间零实例且Lint完整通过，没有放宽扫描或禁用规则 |
| 后端发布门禁 | Go1.26.6 ordinary本机DB、移除integration变量：全仓Test；Vet；Build；tidy | Test8.644s；Vet3.579s；Build5.103s；tidy/diff合并复核1.813s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建与新依赖图全绿；httpapi3.099s、queue4.162s、generation保持162 |
| 前端发布门禁 | 修复后依赖树动态枚举109个Node文件；TypeScript；ESLint；合法HTTPS Next16 production build | Tests6.760s；Type5.160s；Lint24.025s；Build27.243s | 0 / 0 / 0 / 0 | PASS：260/260、零类型/lint错误、58页production build通过；实际node_modules以更新锁重装后验证 |
| Schema/API/独立边界 | generation162；module/lock/workflow/调用边界复核；保持Medium/P0发布门 | 定向检查 | 0 | PASS：无DDL/reset/回填/数据库/业务API/远端业务操作；OPS-002通用CI/CD、TEST-003 Race及其他质量Finding未合并关闭 |
| 449台账 | strict与`-allow-open` verifier | strict/allow合计3.4s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=359、OPEN=90；High=135、Medium=223、Low=59、UNRESOLVED=32；strict精确123 issues，只因其余开放/待定及最终严重度目标退出，开放期通过 |
| 格式/依赖/不可变边界 | gofmt/tidy、双仓`git diff --check`、新模块SHA-256、package-lock、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；go.mod=`DFE2...DCC9`、go.sum=`3D41...B652`、package.json=`96F3...E7E1`、package-lock=`7F04...C195`；依赖锁与workflow纳入工作树；权威audit目录限定diff/status均为零，未访问共享public或远端业务系统 |

## 2026-08-24：SEC-003 多副本Redis故障时共享限流失败关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效compile RED | 新config/querycache测试直接引用运行期失败策略字段、指标和跨实例故障合同 | 包构建0.922s | 1（预期） | PASS：`RedisConfig.RateLimitFailClosed`和`Metrics.RateLimitFailClosed`均不存在，旧实现没有表达或观测多副本失败关闭的边界 |
| 双实例正常/故障演练 | 两个独立Cache共享同一miniredis namespace：先消耗2次额度并拒绝第3次，再停Redis；另验证单副本local上限 | 初始GREEN3.458s；扩展四包定向10.350s | 0 | PASS：正常额度跨实例精确累计；停服后两个实例均`Allowed=false/Backend=unavailable`、RetryAfter>0且各自指标=1，不生成本地额度；单副本按3次降级额度继续工作 |
| 配置与高成本入口 | `go test ./internal/querycache ./internal/config ./internal/httpapi ./internal/antiabuse -run 'SEC003|MultipleReplicasRequireSharedRedisThrottle' -count=1` | 包内1.842/0.068/1.505/0.762s；墙钟10.350s | 0 | PASS：多副本缺required/fail-closed/auth共享任一项均拒绝；登录即使开关漂移也失败关闭；匿名`catalog-export`高成本读取停Redis后拒绝；单副本策略不能在Redis关闭时误启 |
| 受影响包全回归 | querycache/config/httpapi/antiabuse完整包 | 包0.789/0.071/2.737/3.318s；墙钟5.993s | 0 | PASS：既有共享Redis、认证、反滥用、crawler、presence和本地fallback行为全绿 |
| 后端发布门禁 | ordinary本机DB且移除integration变量：全仓Test；Vet；Build；tidy | Test17.999s；Vet3.671s；Build6.036s；tidy0.344s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建和模块图通过；httpapi3.581s、antiabuse5.098s、querycache1.490s、generation保持162；无迁移/reset/远端访问 |
| 前端无回归门禁 | 动态枚举109个Node文件；TypeScript；ESLint；合法HTTPS Next16 production build | Tests4.294s；Type2.236s；Lint22.819s；Build23.311s | 0 / 0 / 0 / 0 | PASS：260/260、零类型/lint错误、58页production build通过；本项无前端代码或协议解析变更 |
| Schema/API/可用性取舍 | generation162；全调用点、配置、指标和部署文档复核；保持Medium/P1 | 定向检查 | 0 | PASS：无DDL/查询计划/持久计数；正常协议不变，故障期既有429/Delay有意替代按实例放行；`rateLimitFailClosed`为后台指标只增字段；其余Redis缓存降级未改变 |
| 449台账与不可变边界 | strict/allow verifier；双仓diff；外层audit限定diff/status | strict0.656s；allow0.637s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=360、OPEN=89；High=135、Medium=223、Low=59、UNRESOLVED=32；strict精确122 issues，只因其余开放/待定及最终严重度目标退出；双仓空白错误0，权威audit限定diff/status均为零 |

## 2026-08-24：SEC-004 Session撤销与授权版本发布失败关闭

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效compile/source RED | 新querycache精确失效与HTTP提交后故障测试；扫描生产`_ = s.refresh(Auth|Permission|RBAC|ProjectACL)Version` | 墙钟约3.0s | 1（预期） | PASS：`DeleteShared`和服务指标不存在，生产仍有31处刷新错误被显式丢弃；旧状态Handler会在Redis发布失败后返回200 |
| 精确失效与源码GREEN | `go test ./internal/querycache ./internal/httpapi -run SEC004 -count=1`；生产Go文件全扫描 | querycache0.283s；httpapi1.090s；墙钟8.999s | 0 | PASS：精确共享key删除并对停服返回error；四类版本统一delete→load→set；31处忽略归零；安全503、结构化日志、指标、预解析身份和到期Worker checked Set均受源码合同保护 |
| 真实PG撤销故障注入 | generation162会话临时全Schema：错误identity使刷新SELECT失败；重新预热Session后停miniredis并PUT disabled | 包10.611s；墙钟13.084s | 0 | PASS：SELECT失败前精确旧auth key已删除；停Redis后DB仍提交disabled且auth版本递增，响应503含code/committed，指标=1；旧Session即使本地session缓存预热也立即拒绝 |
| 认证/授权相关集成矩阵 | SEC004、Session/RBAC命中、role binding不登出、密码撤销、AdminAuthorization事务/并发/Redis停服 | 包22.768s；墙钟25.222s | 0 | PASS：正常版本发布即时跨缓存生效；密码撤销仍使旧Session失败；权限变化不误登出；后台权限Redis故障改为可辨503但已提交权限从PG立即可见 |
| 补充全仓integration诊断 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./... -count=1` | 长时并行运行后主动终止 | 非0（环境） | 记录：并行临时Schema触发PG `out of shared memory`，直接public测试因generation155拒绝，另命中既知sticker临时索引42P07；无SEC004断言失败。结束后Go进程/PG外部连接均0、无命名mcmods_ephemeral残留；不重置共享public |
| 后端发布门禁 | ordinary本机DB且移除integration变量：全仓Test；Vet；Build；tidy | Test10.918s；Vet3.558s；Build3.851s；tidy0.335s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建与模块图全绿；httpapi3.268s、querycache1.153s、generation保持162 |
| 前端无回归门禁 | 动态枚举109个Node文件；TypeScript；ESLint；合法HTTPS Next16 production build | Tests4.382s；Type2.663s；Lint28.370s；Build23.421s | 0 / 0 / 0 / 0 | PASS：260/260、零类型/lint错误、58页production build通过；新增503继续使用既有通用API错误结构，无前端代码变更 |
| Schema/API/严重度边界 | generation162；缓存/调用图/错误协议/回滚复核；保持Medium/P1 | 定向检查 | 0 | PASS：无DDL/reset/回填/查询计划；正常协议不变，故障503明确主事实已提交；只增后台指标字段。其余全仓integration环境、TEST及public155不借本项关闭 |
| 449台账与不可变边界 | strict/allow verifier；双仓diff；外层audit限定diff/status | strict0.678s；allow0.646s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=361、OPEN=88；High=135、Medium=223、Low=59、UNRESOLVED=32；strict精确121 issues，只因其余开放/待定及最终严重度目标退出；双仓空白错误0，权威audit限定diff/status均为零 |

## 2026-08-24：SEC-019 站点Logo有界静态派生

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 新Node安全边界测试导入不存在helper；Go把旧raw PNG加入拒绝矩阵 | Node0.521s；Go包1.127s、墙钟9.182s | 1 / 1（预期） | PASS：旧前端没有预解析body上限/真实解码/派生模块；旧后端仍接受`.png`配置，两个失败都直接命中审计缺口 |
| 图片/请求定向GREEN | `node --test app/_lib/site-logo-image.test.mts`；Go SiteGeneral/Logo组 | Node0.689s；Go广域包0.187s、墙钟3.525s | 0 / 0 | PASS：6项全绿；声明超限在解析前拒绝，chunked越过12字节即cancel且不读完100块；PNG/JPEG/GIF/WebP结构合同、4096边/4Mi单帧/128帧/16Mi总像素、512边/1Mi输出合同受测 |
| 静态化与元数据 | 1024×600携带EXIF的JPEG、2帧GIF、129帧GIF及SVG/超边/超像素输入 | 包含于6项定向测试 | 0 | PASS：输出实际format=webp、最长边≤512、pages=1且EXIF/ICC/XMP均undefined；2帧被压为1帧、129帧拒绝；SVG和两类维度攻击拒绝 |
| 路由/调用方闭集 | 上传、动态资产路由、SiteBrand Provider与后端配置源码合同 | 定向检查 | 0 | PASS：源bytes零写入、摘要基于派生物；四边只允许20位小写哈希`.webp`，旧png/jpeg/gif不再进入动态公开路由或持久设置；权限探测仍先于body读取 |
| 前端发布门禁 | 动态枚举104个Node文件；TypeScript；全量ESLint；合法HTTPS Next16 production build | Tests5.084s；Type4.126s；Lint25.630s；Build24.130s | 0 / 0 / 0 / 0 | PASS：253/253、零类型/lint错误、58页构建完成；`/api/site-logo`和`/site-assets/[fileName]`均成功编译为动态路由；Sharp为显式生产依赖 |
| 后端发布门禁 | ordinary本机DB、移除integration变量：全仓Test；Vet；Build；tidy | Test14.557s；Vet10.404s；Build11.383s；tidy0.864s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建与模块图全绿；generation保持162 |
| 严重度/独立边界 | UNRESOLVED→High/P1；OPS-008继续OPEN | 逐项复核 | 0 | PASS：需站点配置写权限，但持久恶意Logo会自动分发给所有访客并影响客户端可用性，且旧body先耗服务内存；本项不冒充解决多实例本地存储/生命周期 |
| 449台账 | strict与`-allow-open` verifier | strict1.280s；allow1.042s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=343、OPEN=106；High=125、Medium=222、Low=59、UNRESOLVED=43；strict精确150 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、package-lock直接Sharp合同、Go模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；lock根依赖为`sharp=0.35.3`且package节点非optional；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零 |

## 2026-08-24：SEC-020 API访问日志查询秘密最小化

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效序列化RED | 真实middleware构造OAuth callback，500保证入队，batch=1后检查序列化JSON | 包1.126s；墙钟9.266s | 1（预期） | PASS：旧batch逐字包含`oauth-code-secret`、`csrf-state-secret`、Bearer样式值、攻击者控制的秘密式参数名及普通值，直接证明不是只存在内存URL |
| SEC020/AccessLog GREEN | `TestSEC020AccessLogsNeverPersistQueryContent`与全部`TestAccessLog*` | 包1.172s；墙钟9.350s | 0 | PASS：五种查询内容在完整batch零命中，record payload精确有`queryPresent=true`且无`query`；采样、非阻塞、队满、批写和生命周期合同保持 |
| OAuth/日志广域 | `go test ./internal/httpapi -run 'OAuth&#124;AccessLog&#124;SEC020' -count=1` | 包0.190s；墙钟4.181s | 0 | PASS：OAuth state/code读取、供应商交换、登录和通用访问日志相邻组全绿；仅日志观察边界变化，协议执行不变 |
| 数据最小化源码边界 | `logAccess`/`accessLogRecord`调用图与全部日志RawQuery检索 | 定向检查 | 0 | PASS：RawQuery只用于生成`queryPresent`布尔，不构造/截断/传递字符串；旧Yggdrasil路径例外和`queryTruncated`均删除；路径不包含URL查询 |
| 后端发布门禁 | ordinary本机DB、移除integration变量：全仓Test；Vet；Build；tidy | Test14.660s；Vet10.261s；Build10.979s；tidy0.804s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建与模块图全绿；generation保持162、模块依赖无diff |
| 前端非影响复核 | 本项零前端代码/HTTP协议变化；复用同工作树SEC-019完整门 | Tests253项；Type/Lint/Build | 0 / 0 / 0 / 0 | PASS：104文件253项、TypeScript、ESLint与58页production build均绿 |
| 严重度 | UNRESOLVED→High/P1 | 逐项复核 | 0 | PASS：一次性授权码/state被默认90日持久并向日志管理员可搜索，仍需日志权限且授权码通常短时单用，故不升级P0 |
| 449台账 | strict与`-allow-open` verifier | strict1.114s；allow0.975s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=344、OPEN=105；High=126、Medium=222、Low=59、UNRESOLVED=42；strict精确148 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、Go模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零 |

## 2026-08-24：SEC-016 爬虫AI原子日预算

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | generation162/日桶预留helper/两协议max_tokens/输入输出界测试先行，运行database+httpapi SEC-016组 | database0.882s；墙钟3.516s | 1（预期） | PASS：旧generation161缺日桶/预留列，helper不存在；requestAICompletion只接收4参数且OpenAI-compatible无max_tokens，编译和Schema断言精确失败 |
| 定向unit与Schema GREEN | provider request捕获、prompt/context/output边界、generation162声明 | database0.899s；httpapi1.102s | 0 | PASS：OpenAI-compatible/Anthropic都发送317；50,000配置被32768硬限，context不足和>1MiB prompt均在外呼前拒绝；三项token列CHECK存在 |
| 真实PG并发/失败关闭/计划 | 4连接随机隔离Schema并发60+60/预算100，同任务重试、实际/已知失败/usage缺失结算、表故障；100k历史行EXPLAIN | httpapi2.154s；墙钟10.476s | 0 | PASS：只1项占位；同任务40+10累计50而非覆写；另项50占满后已知usage10仅释放未用40，余40被成功但usage缺失的响应保留且额外1 token拒绝；表故障error；Index Only、无Seq、0.029ms |
| 既有翻译端到端与广域 | BUG-040可控provider+完整generation162临时Schema；`SeedCrawler&#124;SEC016`组 | BUG-040包10.147s；最终广域包40.652s | 0 / 0 | PASS：Mod/插件各7次AI调用仍写正式8语言草稿、revision和发布投影，实际token精确结算；crawler调度/来源/提交/预算相邻合同全绿 |
| 后端发布门禁 | ordinary本机DB、移除integration变量：全仓Test；Vet；Build；tidy | Test11.952s；Vet4.630s；Build4.880s；tidy0.795s | 0 / 0 / 0 / 0 | PASS：最终代码全部普通包、静态分析、构建和模块图全绿；generation精确门同步到162；模块依赖无diff |
| Race环境诊断 | `go test -race ./internal/httpapi -run '^TestSEC016'` | 0.552s | 1（环境） | INFO：当前Windows Go要求CGO但PATH无gcc/clang，未冒充race通过；本项跨实例竞态由真实4连接PG行为测试验证。总体最终race门仍须在目标完成前另行满足或明确记录受支持环境 |
| 前端非影响复核 | 本项零前端代码/站内协议变化；复用最近BUG-127同工作树完整前端门 | Tests247项；Type/Lint/Build | 0 / 0 / 0 / 0 | PASS：103文件247项、TypeScript、ESLint与58页production build均绿；仅AI供应商出站body增加受限max_tokens |
| 严重度与449台账 | High/P1保持；严格及开放期verifier | 最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=342、OPEN=107；High=124、Medium=222、Low=59、UNRESOLVED=44；strict精确152 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零；仅本机随机隔离/会话临时Schema且自动drop，未访问远端或共享public |

## 2026-08-24：BUG-109 举报提交者/作者身份与处置通知分离

| 验证 | 命令/场景 | 时间 | 退出码 | 结果 |
|---|---|---:|---:|---|
| 有效双端RED | 新增target actor/recipient测试后运行HTTP聚焦、database Schema聚焦和Node单测 | 后端编译3.7s；database包0.902s；前端0.121s | 1 / 1 / 1（预期） | PASS：旧代码缺少角色/开发者收件人解析，Schema仍含`target_author_id`，第一方DTO仍暴露targetAuthor；首次前端启动器路径拼写错误属于环境命令，已用正确Node路径重跑得到有效断言失败 |
| 收件人helper GREEN | `MCMODS_TEST_DATABASE_URL=local MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run 'TestReport(ModerationRecipients&#124;ProjectTypes)' -count=1` | 包1.237s；墙钟9.909s | 0 | PASS：项目submitter/editor 10被排除，重复author/team developer 20去重，developer 21保留；直接评论author 30保留；八类项目映射到权威route identity |
| 完整审核事务 GREEN | 同一隔离PG运行`TestResolveProjectReportNotifiesDevelopersInsteadOfSubmitterIntegration`及BUG-109组 | 包1.364s；墙钟9.573s | 0 | PASS：真实reports/review/snapshot/mod/outbox事务返回200，目标隐藏后仅为20/21持久化`report.target_action`，无10；业务事实与通知意图原子提交 |
| Schema/前端聚焦 | `go test ./internal/database -run TestReportsSchemaNamesTargetActors -count=1`；`node --test app/_lib/report-target-identity.test.mts` | 包0.899s/墙钟3.439s；前端0.124s/墙钟0.567s | 0 / 0 | PASS：generation158 actor列/四角色CHECK且无author列；第一方显式角色、姓名并无targetAuthor字段 |
| Report/Governance广域 | 本地PG双integration环境变量；`go test ./internal/httpapi -run 'Report&#124;Governance' -count=1` | 包33.535s | 0 | PASS：举报创建/可见性/读取/审核及治理百万行分页相邻组全部通过 |
| generation158完整临时Schema | 本地PG运行`TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration` | 用例16.87s；包16.953s；墙钟18.539s | 0 | PASS：单连接全Schema安装/种子/自动drop；100k/1M/10M相邻计划约5.60/14.04/9.13ms；未重置共享public |
| 后端发布门 | local DATABASE_URL、unset integration vars；`go test ./... -count=1`；`go vet ./...`；`go build ./...`；`go mod tidy -diff` | 墙钟21.803/11.267/12.112/0.891s | 0 / 0 / 0 / 0 | PASS：全仓、Vet、Build和模块文件零差异 |
| 前端发布门 | 93个`*.test.mts`全量；`pnpm lint`；`pnpm exec tsc --noEmit`；三项公开URL环境变量下`pnpm build` | 220 tests 4.376s；Lint29.225s；Type14.902s；Build24.248s | 0 / 0 / 0 / 0 | PASS：220/220、ESLint、TypeScript及Next 58页生产构建全部通过 |
| 边界复核 | source/Schema/API/通知调用图；strict/allow-open verifier；diff/gofmt/hash/不可变审计 | strict1.089s；allow0.936s | 1（预期）/ 0 | PASS：项目关系只读一次distinct开发者，无N+1；通知仍走事务Outbox；无提交者fallback、双列或远端操作；generation统一158；audit/ledger精确449，CLOSED=328、OPEN=121，High=122、Medium=215、Low=58、UNRESOLVED=54；strict精确176 issues，开放期通过 |

## 2026-08-24：BUG-110 举报结论与惩罚动作一致性

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增invalid hide/ban闭集与真实PG零副作用测试；`go test ./internal/httpapi -run TestInvalidReport -count=1` | 墙钟2.582s | 1（预期） | PASS：旧实现没有结论/动作验证边界，新增测试精确因undefined validator编译失败；调用图确认旧代码仅做结论枚举和动作权限后直接进入事务执行惩罚 |
| unit与真实PG GREEN | 本机双DB URL+integration；同一`TestInvalidReport`组 | 包1.241s；墙钟9.214s | 0 | PASS：invalid delete/ban全部拒绝，invalid无动作与valid delete/ban控制允许；HTTP非法hide 400且report pending、目标approved、review/action/outbox全0 |
| 举报/治理/封禁广域 | 本机双DB URL+integration；`go test ./internal/httpapi -run 'Report&#124;Governance&#124;Ban' -count=1` | 包32.770s | 0 | PASS：创建、可见性、claim/resolve/reopen、模板通知、封禁和治理1M分页相邻组全部绿色 |
| 后端发布门 | ordinary local DB、unset integration vars；全仓Test/Vet/Build/tidy | Test14.316s；Vet10.884s；Build11.508s；tidy0.879s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建和模块图绿色；httpapi2.844s、queue4.120s |
| 前端边界 | BUG-110没有前端或协议字段变化；复用BUG-109之后同一前端生产代码的全量门 | Tests220/220；Lint/Type/58页Build | 0 / 0 / 0 / 0 | PASS：第一方默认valid并继续提交原字段，无隐藏兼容分支；后端错误输入收紧无需调用方迁移 |
| Schema/API/不可变边界 | generation158；调用图、gofmt/diff/module hash/immutable audit；strict/allow verifier | strict1.043s；allow1.120s | 1（预期）/ 0 | PASS：无DDL/reset/远端操作；一致性在权限/事务前；audit/ledger精确449，CLOSED=329、OPEN=120，High=123、Medium=215、Low=58、UNRESOLVED=53；strict精确174 issues，开放期通过；双仓diff无空白错误，gofmt空差异，module hash保持`695B...728`/`C151...783` |

## 2026-08-24：BUG-111 临时封禁时间不变量

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增时间边界、入口前置失败、真实PG生命周期和Schema约束测试；HTTP/DB聚焦 | 合计墙钟5.700s | 1 / 1（预期） | PASS：旧生产代码没有最短未来区间常量/validator，HTTP测试编译失败；Schema仍为158且没有截止时间CHECK；同时发现并移除测试内与现有包函数同名的辅助函数，不将环境问题计作产品RED |
| unit/入口/真实PG GREEN | 本机双DB URL+integration；`TestTemporaryBan&#124;TestBanHandlersRejectElapsed`及Schema聚焦 | HTTP墙钟9.222s；DB墙钟2.706s | 0 / 0 | PASS：nil、精确1分钟及更长允许，过去/当前/不足1分钟拒绝；两个Handler在nil DB条件下均稳定400，证明未开事务；内部写非法零行、合法未来值成功，既有到期Worker置expired并删角色 |
| 举报/治理/封禁广域 | 本机真实PG；`go test ./internal/httpapi -run 'Report&#124;Governance&#124;Ban' -count=1` | 包33.222s；墙钟35.855s | 0 | PASS：举报联动封禁、独立封禁、审核/通知与治理百万行相邻行为全部绿色 |
| database与generation159 | ordinary database包；完整临时Schema `TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration` | DB包0.119s/墙钟1.770s；临时Schema包15.820s/墙钟17.307s | 0 / 0 | PASS：generation统一159，完整建表/种子/自动drop及100k/1M/10M断言通过；只使用session临时对象，未重置共享public |
| 后端发布门 | ordinary local DB、unset integration vars；全仓Test/Vet/Build/tidy | Test15.683s；Vet10.551s；Build11.372s；tidy0.405s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建和模块图绿色；go.mod/go.sum SHA-256前后不变 |
| 前端边界 | BUG-111没有字段或第一方代码变化；复用BUG-109后同一前端生产代码全量门 | Tests220/220；Lint/Type/58页Build | 0 / 0 / 0 / 0 | PASS：现有表单继续提交相同RFC3339字段，错误值由服务端收紧400，无协议迁移或隐藏兼容路径 |
| Schema/API/不可变边界 | generation158→159；调用图、gofmt/diff/module hash/immutable audit；strict/allow verifier | strict0.655s；allow0.639s | 1（预期）/ 0 | PASS：新CHECK与服务校验一致；永久封禁和批量到期清理保持；audit/ledger精确449，CLOSED=330、OPEN=119，High=124、Medium=215、Low=58、UNRESOLVED=52；strict精确172 issues，开放期通过；模块哈希保持`695B...728`/`C151...783` |

## 2026-08-24：BUG-112 举报领取责任锁

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效三端RED | 新增真实PG责任链、Schema权威和第一方可见性测试 | HTTP编译/DB/前端合计墙钟3.600s；DB包0.906s；前端0.147s | 1 / 1 / 1（预期） | PASS：旧后端没有takeover handler且resolve不读责任人，Schema仍159无assignment事实，第一方缺当前责任字段并允许pending/任一in_review显示结案表单 |
| 聚焦GREEN | 本机双DB URL+integration；HTTP生命周期、DB Schema与Node源码合同 | HTTP包1.242s/墙钟9.435s；DB包0.894s/墙钟3.010s；前端0.225s | 0 / 0 / 0 | PASS：pending/非责任人/旧责任人均409；无专权接管403；专权有理由接管后仅新责任人成功结案；claim/takeover审计顺序和前后责任精确 |
| 相邻责任与错误回归 | BUG-109～112真实PG组；治理详情错误可见性 | 相邻包1.637s/墙钟9.268s；错误组包1.294s | 0 / 0 | PASS：开发者通知、invalid动作互斥、临时封禁时间、责任锁及详情读取全部绿色；旧BUG-109夹具改为先有真实责任，不保留pending直结例外 |
| Report/Governance广域 | 本机真实PG；`go test ./internal/httpapi -run 'Report&#124;Governance' -count=1` | 包33.261s；墙钟37.494s | 0 | PASS：创建、领取、详情、审核、重开、通知和百万行治理分页相邻组全部绿色 |
| database与generation160 | ordinary database包；完整临时Schema安装/种子/自动drop/三档规模 | DB包0.171s/墙钟2.895s；临时Schema包15.439s/墙钟16.910s | 0 / 0 | PASS：generation统一160，新表/CHECK/权限种子有效，100k/1M/10M断言通过；没有重置共享public |
| 后端发布门 | ordinary local DB、unset integration vars；全仓Test/Vet/Build/tidy | Test16.509s；Vet11.428s；Build12.321s；tidy0.410s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建和模块图绿色；模块SHA-256不变 |
| 前端发布门 | 动态枚举94个Node文件；ESLint；TypeScript；合法HTTPS环境Next build | 221 tests4.260s；Lint32.431s；Type10.019s；Build24.428s | 0 / 0 / 0 / 0 | PASS：221/221、零lint/类型错误，58页生产构建通过；责任人/接管文案双语存在 |
| Schema/API/不可变边界 | generation159→160；权限/锁序/DTO/第一方；strict/allow verifier；diff/gofmt/hash/audit | strict0.674s；allow0.640s | 1（预期）/ 0 | PASS：无未审计转交、pending直结或非责任人结案分支；CLOSED=331、OPEN=118，High=124、Medium=216、Low=58、UNRESOLVED=51；strict精确170 issues，开放期通过；未访问远端/共享public |

## 2026-08-24：BUG-113 统一举报支持整合包

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效三端RED | 新增Modpack完整举报事务、Schema闭集和第一方入口测试 | HTTP包1.144s；DB包0.921s；前端0.151s | 1 / 1 / 1（预期） | PASS：旧后端无modpack原因/快照/动作分支，Schema仍generation160且CHECK拒绝目标，详情类型与按钮/双语目标名缺失 |
| 聚焦GREEN与真实PG生命周期 | 双本机DB URL+integration运行`TestModpackReportLifecycleIntegration`；DB与Node目标测试 | HTTP包11.108s/墙钟18.918s；DB包0.918s/墙钟3.101s；前端0.215s | 0 / 0 / 0 | PASS：原因可发现，创建保存规范target/submitter/对象快照；领取后valid delete使report=resolved_valid、modpack=rejected且action=1 |
| Report/Governance/Modpack广域 | 本机真实PG运行HTTP目标组；ordinary database包；动态枚举95个前端测试文件 | HTTP包83.927s/墙钟93.665s；DB包0.161s/墙钟2.646s；222 tests4.217s | 0 / 0 / 0 | PASS：举报创建、责任锁、审核、处置、治理分页及Modpack相邻链绿色；222/222前端测试通过 |
| generation161完整临时Schema | 双本机DB URL运行完整`TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration` | 包15.847s/墙钟17.411s | 0 | PASS：完整Schema、reports目标CHECK及100k/1M/10M断言安装到161并自动drop；没有重置或写入共享public |
| 后端发布门 | ordinary本机DB、unset integration vars；全仓Test/Vet/Build/tidy | Test23.048s；Vet16.193s；Build16.818s；tidy0.460s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建与模块图绿色；go.mod/go.sum SHA-256前后不变 |
| 前端发布门 | 95文件全量Node；ESLint；TypeScript；合法HTTPS环境Next16 production build | 222 tests4.217s；Lint39.238s；Type21.466s；Build37.636s | 0 / 0 / 0 / 0 | PASS：222/222、零lint/类型错误，58页生产构建通过；Modpack详情复用统一举报组件且英中目标名齐全 |
| 台账验证 | 权威audit对mutable ledger运行strict与`-allow-open` | strict0.776s；allow0.795s | 1（预期）/ 0 | PASS：449/449唯一ID；CLOSED=332、OPEN=117，High=124、Medium=217、Low=58、UNRESOLVED=50；strict精确168 issues且只因其余开放项/最终严重度，开放期验证通过 |

## 2026-08-24：BUG-114 关于页locale与草稿一致性

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`about-locale-editor.test.mts`后运行目标Node测试 | 测试123.6ms；墙钟0.211s | 1（预期） | PASS：旧代码没有可测试一致性边界，目标模块不存在；原实现确实直接以选择locale保存旧draft |
| 聚焦状态矩阵与相邻CAS合同 | About响应/可写谓词/源码三组及既有site-affairs concurrency测试 | 4 tests154.4ms | 0 | PASS：当前代次同语言响应唯一可接收；abort、旧代次、请求/选择不符、响应/请求不符均拒绝；加载、错误、保存中、错语言和无草稿均不可写 |
| 真实PG后端边界 | 双本机DB URL运行BUG-102～104 SiteAffairs locale、翻译隔离与编辑CAS组 | 包19.744s；墙钟22.232s | 0 | PASS：管理员locale严格验证、其他翻译状态隔离、About每语言revision陈旧写409行为保持；本项不改后端生产代码 |
| 前端发布门 | 动态枚举96个Node文件；ESLint；独立TypeScript；合法HTTPS环境Next16 production build | 225 tests4.660s；Lint32.824s；Type11.194s；Build30.457s | 0 / 0 / 0 / 0 | PASS：225/225，零lint/类型错误，58页生产构建通过；双语错语言提示和重试路径有效 |
| 门禁调度校正 | 首轮把独立typecheck与会重建`.next/types`的Next build并行运行 | Type4.687s | 2（非产品） | 并行读写产生两个TS6053瞬态缺文件；build完成后独立typecheck 11.194s通过，无源码调整 |
| 后端发布门复用 | BUG-114未改后端生产/测试代码；复用同一轮BUG-113全仓Test/Vet/Build/tidy及模块哈希 | Test23.048s；Vet16.193s；Build16.818s；tidy0.460s | 0 / 0 / 0 / 0 | PASS：后端generation161及CAS代码与上一发布门完全相同，模块图SHA-256不变 |
| 台账验证 | 权威audit对mutable ledger运行strict与`-allow-open` | strict0.719s；allow0.732s | 1（预期）/ 0 | PASS：449/449唯一ID；CLOSED=333、OPEN=116，High=124、Medium=218、Low=58、UNRESOLVED=49；strict精确166 issues且只因其余开放项/最终严重度，开放期验证通过 |

## 2026-08-24：BUG-115 更新日志精确locale草稿

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`site-changelog-locale-editor.test.mts`后运行目标Node测试 | 测试110.6ms；墙钟0.201s | 1（预期） | PASS：旧代码无locale草稿边界模块，且编辑明确使用`item.translations[locale] || Object.values(...)[0]`跨语言兜底 |
| 聚焦GREEN | 精确翻译/缺失为空/双语言草稿往返三组及site-affairs CAS源码合同 | 4 tests146.4ms；墙钟0.233s | 0 | PASS：只读取同名翻译；不存在的语言为空；EN与ZH未保存title/body/publish往返独立恢复；editingItem和切换边界接入生产组件 |
| 前端全量回归 | 动态枚举97个Node测试文件；首次运行发现旧翻译状态源码断言仍匹配被替换实现 | 首轮墙钟4.401s | 1（测试合同陈旧） | 更新断言为`setPublish(value.publish)`并由纯函数测试锁定status→publish语义；没有生产逻辑回退 |
| 前端发布门 | 97文件全量Node；ESLint；构建后独立TypeScript；合法HTTPS环境Next16 build | 228 tests4.031s；Lint32.491s；Type3.974s；Build30.915s | 0 / 0 / 0 / 0 | PASS：228/228，零lint/类型错误，58页生产构建通过；精确语言和独立草稿路径完整 |
| 后端边界复用 | 本项无后端代码/API/Schema变化；复用BUG-114真实PG SiteAffairs locale/translation/CAS和同一轮全仓门 | PG包19.744s；全仓Test/Vet/Build/tidy见前节 | 0 | PASS：父updatedAt CAS、翻译隔离和generation161不变；模块哈希不变，未访问远端/共享public |
| 台账验证 | 权威audit对mutable ledger运行strict与`-allow-open` | strict0.716s；allow0.702s | 1（预期）/ 0 | PASS：449/449唯一ID；CLOSED=334、OPEN=115，High=124、Medium=219、Low=58、UNRESOLVED=48；strict精确164 issues且只因其余开放项/最终严重度，开放期验证通过 |

## 2026-08-24：BUG-116 临时封禁datetime-local线格式

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`ban-end-time.test.mts`后运行目标Node测试 | 测试99.5ms；墙钟0.189s | 1（预期） | PASS：旧代码没有转换模块，生产表单把`form.get("endsAt")`无时区原值直接放入JSON |
| 聚焦GREEN | 永久null、本地墙钟往返、非法输入和生产接线四组；目标Type/Lint | 4 tests140.9ms；墙钟0.250s | 0 | PASS：合法值输出UTC RFC3339并反解为相同本地年月日时分；空值null；非法日历/小时/带Z/文本抛RangeError且表单显示双语错误 |
| 后端线协议回归 | ordinary本机DB运行BUG-111 `TemporaryBanEnd`与handler过期时间组 | 包0.133s；墙钟2.558s | 0 | PASS：Go请求模型继续成功解码RFC3339，永久/有效未来值合同不变，过期RFC3339在开事务前拒绝 |
| 前端发布门 | 动态枚举98个Node文件；ESLint；构建后TypeScript；合法HTTPS环境Next16 build | 232 tests4.516s；Lint32.342s；Type2.370s；Build30.225s | 0 / 0 / 0 / 0 | PASS：232/232，零lint/类型错误，58页生产构建通过；临时与永久封禁表单均符合既有线协议 |
| 后端发布门复用 | 本项无后端代码/API/Schema变化；复用同一generation161全仓Test/Vet/Build/tidy | 见BUG-113/114前节 | 0 | PASS：模块图和服务端封禁不变量未变，未访问远端/共享public |
| 台账验证 | 权威audit对mutable ledger运行strict与`-allow-open` | strict0.771s；allow0.776s | 1（预期）/ 0 | PASS：449/449唯一ID；CLOSED=335、OPEN=114，High=124、Medium=220、Low=58、UNRESOLVED=47；strict精确162 issues且只因其余开放项/最终严重度，开放期验证通过 |

## 2026-08-24：BUG-117 封禁表单异步事件生命周期

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`ban-form-event-lifetime.test.mts`并运行三项源码顺序测试 | 3 tests107.7ms；墙钟0.196s | 1（预期） | PASS：3/3失败，旧函数没有稳定formElement，await后仍访问event.currentTarget，且请求catch包围reset/refresh |
| 聚焦GREEN | BUG-116/117相邻Node组；目标ESLint与TypeScript | 7 tests168.7ms；墙钟0.277s | 0 / 0 / 0 | PASS：捕获早于首个await；异步续体零事件解引用；请求错误return早于成功清理；RFC3339转换保持 |
| 前端发布门 | 动态枚举99个Node文件；ESLint；构建后TypeScript；合法HTTPS环境Next16 build | 235 tests4.687s；Lint32.789s；Type2.428s；Build30.561s | 0 / 0 / 0 / 0 | PASS：235/235，零lint/类型错误，58页生产构建通过；封禁成功清理与刷新路径有效 |
| 后端边界复用 | 本项无后端代码/API/Schema变化；复用BUG-116封禁handler及generation161全仓门 | 见前节 | 0 | PASS：201事务、active唯一性和模块图不变，未访问远端/共享public |
| 台账验证 | 权威audit对mutable ledger运行strict与`-allow-open` | strict0.766s；allow0.765s | 1（预期）/ 0 | PASS：449/449唯一ID；CLOSED=336、OPEN=113，High=124、Medium=220、Low=59、UNRESOLVED=46；strict精确160 issues且只因其余开放项/最终严重度，开放期验证通过 |

## 2026-08-24：BUG-118 治理异步请求身份

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`governance-request-order.test.mts`并运行目标Node文件 | 测试115.8ms；墙钟0.206s | 1（预期） | PASS：旧代码没有详情请求身份模块，select无signal/代次/目标校验且直接setDetail |
| 聚焦GREEN | 详情响应矩阵、三读取路径与clear/action续体，连同About状态矩阵 | 6 tests171.7ms；墙钟0.282s | 0 | PASS：abort/旧代次/期望ID变化/响应ID错误全部拒绝；列表、详情、About均有保护；清空选择使旧动作续体失效 |
| 前端全量回归 | 首次100文件运行发现BUG-112源码断言仍限定路径直接使用`detail.id` | 墙钟4.755s | 1（测试合同陈旧） | 更新为验证动作开始冻结`const reportId=detail.id`后使用该ID；责任锁与生产行为不回退 |
| 前端发布门 | 100文件全量Node；ESLint；构建后TypeScript；合法HTTPS环境Next16 build | 238 tests4.184s；Lint32.712s；Type3.900s；Build30.469s | 0 / 0 / 0 / 0 | PASS：238/238，零lint/类型错误，58页生产构建通过；三类异步读取和动作续体均受身份边界保护 |
| 后端边界复用 | 本项无后端代码/API/Schema变化；复用BUG-112治理责任真实PG及generation161全仓门 | 见前节 | 0 | PASS：服务端责任锁、事务和模块图不变，未访问远端/共享public |
| 台账验证 | 权威audit对mutable ledger运行strict与`-allow-open` | strict0.764s；allow0.760s | 1（预期）/ 0 | PASS：449/449唯一ID；CLOSED=337、OPEN=112，High=124、Medium=221、Low=59、UNRESOLVED=45；strict精确158 issues且只因其余开放项/最终严重度，开放期验证通过 |

## 2026-08-24：BUG-119 举报证据批量部分成功

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`report-evidence-batch.test.mts`后运行目标Node文件 | 测试102.9ms；墙钟0.193s | 1（预期） | PASS：旧代码没有逐项批处理模块，并把成功保存在局部`uploaded[]`直到全部文件完成 |
| 聚焦GREEN | 成功1→失败2→成功3行为及生产接线；目标ESLint与TypeScript | 2 tests139.4ms；墙钟0.261s | 0 / 0 / 0 | PASS：回调顺序精确且成功结果保留1/3；生产逐项setEvidence、逐项setEvidenceFailures并删除局部uploaded数组 |
| 后端证据生命周期 | 双本机DB URL+integration运行`go test ./internal/httpapi -run ReportEvidence -count=1` | 包0.249s；墙钟2.655s | 0 | PASS：OSS文件/metadata原子注册和重试合同保持；temporary默认24小时清理及部分索引/维护事务源码边界不变 |
| 前端发布门 | 动态枚举101个Node文件；ESLint；构建后TypeScript；合法HTTPS环境Next16 build | 240 tests4.761s；Lint32.833s；Type2.402s；Build30.859s | 0 / 0 / 0 / 0 | PASS：240/240，零lint/类型错误，58页生产构建通过；成功证据可见可绑定，失败项结构化显示 |
| 后端发布门复用 | 本项无后端代码/API/Schema变化；复用generation161全仓Test/Vet/Build/tidy | 见前节 | 0 | PASS：模块图、清理Worker和Schema不变，未访问远端/共享public |
| 台账验证 | 权威audit对mutable ledger运行strict与`-allow-open` | strict0.713s；allow0.699s | 1（预期）/ 0 | PASS：449/449唯一ID；CLOSED=338、OPEN=111，High=124、Medium=222、Low=59、UNRESOLVED=44；strict精确156 issues且只因其余开放项/最终严重度，开放期验证通过 |

## 2026-08-24：BUG-126 自动草稿远端快照权威

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`auto-draft-reversion.test.mts`后运行目标Node文件 | 测试101.1ms；墙钟0.189s | 1（预期） | PASS：旧代码没有共享判定模块，仍保留永久`baselineRef`并在await后把当前界面值错误记为已保存 |
| 聚焦GREEN | A→B→A、保存中B→C、恢复B→初始A与Hook源码追写合同；目标ESLint/TypeScript | 4 tests140.0ms；墙钟0.254s | 0 / 0 / 0 | PASS：仅等于最近成功远端snapshot时跳过；初值回退与恢复后撤销均需保存；请求期间变化会被有界立即追写 |
| 前端发布门 | 动态枚举102个Node文件；ESLint；合法HTTPS环境Next16 build；构建后TypeScript | 244 tests4.683s、墙钟4.819s；Lint31.451s；Build29.001s；Type2.406s | 0 / 0 / 0 / 0 | PASS：244/244，零lint/类型错误，58页生产构建通过；全部九个共享Hook编辑入口使用同一修复 |
| 后端草稿边界 | 本机`DATABASE_URL`、integration变量移除，运行`go test ./internal/httpapi -run 'Test(CompleteUserDraft&#124;UserDraft&#124;Draft)' -count=1` | 包0.134s；墙钟2.535s | 0 | PASS：保存/恢复/完成请求合同与权威审核目标规则不变；本项无后端生产代码、API或Schema变化 |
| 后端发布门复用 | 复用generation161全仓Test/Vet/Build/tidy和完整临时Schema | 见BUG-113节 | 0 | PASS：模块图、草稿持久事实与Schema不变，未访问远端/共享public |
| 台账验证 | 权威audit对mutable ledger运行strict与`-allow-open` | strict0.761s；allow0.750s | 1（预期）/ 0 | PASS：449/449唯一ID；CLOSED=339、OPEN=110，High=124、Medium=222、Low=59、UNRESOLVED=44；strict精确155 issues且只因其余开放项/最终严重度，开放期验证通过 |
| 格式/依赖/不可变边界 | 双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0（仅既有LF/CRLF提示）；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零 |

## 2026-08-24：BUG-127 项目关注完整分页

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 新增cursor request测试与前端pagination模块测试 | Go墙钟2.177s；前端测试101.7ms、墙钟0.205s | 1 / 1（预期） | PASS：后端没有cursor解析/编码；前端模块不存在，旧API固定`limit=100&offset=0`且组件没有续页入口 |
| 定向GREEN | Go cursor user/q/type/limit scope与非法参数；前端path/merge/panel及隐藏相邻行为；目标ESLint/Type | Go包0.174s；前端4 tests167.4ms、墙钟0.270s | 0 / 0 / 0 / 0 | PASS：offset零残留，跨scope/非法参数失败关闭；续页去重保序，筛选请求有abort/generation保护，隐藏项目仍可安全取消 |
| 真实PG完整遍历 | generation161临时完整Schema；125个approved Mod关注，5项同createdAt，limit40；跨q游标 | 包9.911s；墙钟12.395s | 0 | PASS：四页40/40/40/5与数据库权威顺序逐项相同，零重复/遗漏；终页无cursor，跨query返回400；临时Schema自动drop |
| 100k查询计划 | 本机PG临时100k关系，生产同构keyset谓词/排序，`EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON)` | 包1.493s；墙钟9.209s | 0 | PASS：命中`user_id,created_at DESC,project_route_id` Index Only Scan，无Seq Scan；既有Schema索引源码合同新增精确门禁 |
| ProjectFollow广域 | 双本机DB URL+integration运行`go test ./internal/httpapi -run ProjectFollow -count=1` | 包21.518s；墙钟24.706s | 0 | PASS：125条遍历、100k计划、公开创建边界、隐藏后占位读取与取消生命周期全部通过 |
| 后端发布门 | ordinary全仓Test（integration变量移除）；Vet/Build/tidy | Test20.421s；Vet16.094s；Build17.168s；tidy0.501s | 0 / 0 / 0 / 0 | PASS：httpapi3.674s、queue4.003s、serverprobe3.248s；全部包、静态检查与模块图绿色 |
| 前端发布门 | 动态枚举103个Node文件；ESLint；合法HTTPS Next16 build；构建后TypeScript | 247 tests5.579s、墙钟5.693s；Lint38.801s；Build36.846s；Type2.485s | 0 / 0 / 0 / 0 | PASS：247/247，零lint/类型错误，58页production build通过；第一方完整消费hasMore/nextCursor |
| 台账验证 | 权威audit对mutable ledger运行strict与`-allow-open` | strict0.727s；allow0.724s | 1（预期）/ 0 | PASS：449/449唯一ID；CLOSED=340、OPEN=109，High=124、Medium=222、Low=59、UNRESOLVED=44；strict精确154 issues且只因其余开放项/最终严重度，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0（仅既有LF/CRLF提示）；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零 |

## 2026-08-24：SEC-015 自动更新GET严格只读

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | generation161完整临时Schema；已验证Modrinth来源、view-only GET | 包11.018s；墙钟18.742s | 1（预期） | PASS：旧GET立即合成并持久化三条settings，把minecraft_versions设为enabled且configured_by为只读查看者 |
| 权限与只读GREEN | 同一Schema连续GET×2、响应三默认项、DB计数；view-only/configure PUT；GET源码mutation闭集 | 包11.052s；墙钟18.925s | 0 | PASS：两次GET均返回三条disabled响应且DB settings/enabled/viewer=0/0/0；view PUT403；configure PUT精确3/1/3；GET零Begin/Exec/INSERT/configuredBy |
| ProjectAutomation广域 | 双本机DB URL+integration运行`go test ./internal/httpapi -run ProjectAutomation -count=1` | 包20.530s；墙钟23.036s | 0 | PASS：只读配置、调度错误可见性、全局镜像slot、流式镜像及相邻自动更新合同全绿 |
| Schema/API/第一方 | generation161与settings表/调度器；GET/PUT响应和`ProjectAutoUpdateSettings`缺省规范化复核 | 定向检查 | 0 | PASS：无DDL/reset/回填/双写；GET字段形状不变且缺项由响应合成；PUT为唯一写命令；第一方原已支持缺项默认，无迁移 |
| 后端发布门 | ordinary全仓Test（integration变量移除）；Vet/Build/tidy | Test14.170s；Vet10.238s；Build11.016s；tidy0.340s | 0 / 0 / 0 / 0 | PASS：httpapi2.975s、queue4.167s、serverprobe4.192s；全部包、静态检查和模块图绿色 |
| 前端发布门复用 | 本项无前端代码/协议形状变化；复用BUG-127同一代码的103文件247项、Type/Lint/58页Build | 见前节 | 0 | PASS：第一方既有`normalizeSettings`继续消费完整或缺项响应，模块图与前端代码不变 |
| 台账验证 | 权威audit对mutable ledger运行strict与`-allow-open` | strict0.709s；allow0.722s | 1（预期）/ 0 | PASS：449/449唯一ID；CLOSED=341、OPEN=108，High=124、Medium=222、Low=59、UNRESOLVED=44；strict精确153 issues且只因其余开放项/最终严重度，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0（仅既有LF/CRLF提示）；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零 |

## 2026-08-24：BUG-104 站务编辑乐观并发控制

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | generation156完整临时Schema的双管理员About/changelog旧基线；第一方源码基线与稳定错误码合同 | 后端包11.061s、墙钟18.809s；前端0.115s | 1 / 1（预期） | PASS：旧About PUT拒绝未知`baseRevision`前实际没有协议字段；旧changelog无条件覆盖，第一方读取revision却不提交且没有冲突分支 |
| CAS行为GREEN | 本机55432两个DB URL显式固定、generation156完整临时Schema；前端协议测试 | 后端包10.946s、墙钟18.803s；前端0.111s | 0 / 0 | PASS：About revision 1→2，stale 1稳定409且首写内容不变，以2重试到3；changelog旧updatedAt稳定409且日期/正文/status/time不变，以新时间重试成功 |
| 站务相邻广域 | `BUG102&#124;BUG103&#124;BUG104&#124;SiteAffairs&#124;SiteChangelog`真实PG组；前端直接枚举测试 | 后端包37.165s、墙钟39.651s；前端90文件3.708s | 0 / 0 | PASS：严格语言、逐翻译发布、公开/后台cursor、错误可见性、CAS及215/215前端行为全部通过 |
| Schema/API/审计边界 | generation156；About/changelog请求、详情/成功/冲突DTO与成功日志metadata复核 | 定向检查 | 0 | PASS：无DDL/reset/回填/双写；About每语言revision CAS，changelog父updatedAt整记录CAS；409含当前快照，成功日志保留基线和新版本；旧无基线写请求400，前后端同批部署 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | Test11.195s；Vet3.612s；Build4.002s；tidy0.356s | 0 / 0 / 0 / 0 | PASS：最终代码全部普通包全绿；httpapi2.392s、queue3.761s；模块依赖无diff |
| 前端发布门禁 | 直接枚举90个Node文件；ESLint；合法HTTPS Next16 build；build后TypeScript | Tests3.568s；Lint24.129s；Build23.821s；Type9.671s | 0 / 0 / 0 / 0 | PASS：215/215、零lint/类型错误、58页production build通过；冲突保留输入并显示双语明确恢复提示 |
| 严重度与449台账 | UNRESOLVED→Medium/P1；严格及开放期verifier | strict0.830s；allow0.857s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=323、OPEN=126；High=121、Medium=211、Low=58、UNRESOLVED=59；strict精确186 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零；未访问或重置远端/共享public |

## 2026-08-24：BUG-105 未解析引用来源覆盖

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | generation156完整临时Schema；插入server及`minecraft_server_mod`未解析引用后查投影 | 包11.048s、墙钟18.895s | 1（预期） | PASS：旧投影精确返回`source_label=''`、`source_public_id=''`，无法识别实际服务器，直接证明审计缺口 |
| 七来源generation157 GREEN | 全部当前通用生产者初始来源、父名称更新；server/simple父关联重绑；前端来源路由合同 | 后端包11.324s、墙钟19.391s；前端3项0.159s | 0 / 0 | PASS：mod/server/simple/community×2/modpack/mod-content七类label/publicId全部正确；五类父名称实时刷新，两类新重绑切换来源；已知公开来源短链可达，fallback保留诊断字段 |
| 未解析引用/百万行广域 | `UnresolvedReference`完整组；1M投影页、深cursor、selective/broad prefix | 包33.135s、墙钟36.059s；fixture21.584s | 0 | PASS：首/深页稳定无重复；深页命中keyset索引；选择前缀两SQL，过宽前缀一条有界probe后拒绝；列表没有恢复JOIN/N+1 |
| 空库/Schema/重置边界 | generation157单连接临时安装自动drop；database schema unit及10M相邻空库检查；reset文档 | database包18.407s、墙钟20.212s | 0 | PASS：44个直接generation门禁和1个migrator源码门禁同步157；函数/4 trigger存在；旧开发库整代重建，无表列索引迁移、在线回填或双写；共享public未重置 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | Test16.279s；Vet3.967s；Build4.790s；tidy0.436s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；database1.828s、httpapi2.506s、queue5.469s；模块依赖无diff |
| 前端发布门禁 | 直接枚举91个Node文件；ESLint；合法HTTPS Next16 build；build后TypeScript | Tests4.050s；Lint27.327s；Build23.414s；Type2.755s | 0 / 0 / 0 / 0 | PASS：216/216、零lint/类型错误、58页production build通过 |
| 严重度与449台账 | UNRESOLVED→Medium/P1；严格及开放期verifier | strict0.722s；allow0.642s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=324、OPEN=125；High=121、Medium=212、Low=58、UNRESOLVED=58；strict精确184 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零；未访问或重置远端/共享public |

## 2026-08-24：BUG-106 未解析引用权威类型筛选

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 真实PG临时`resource_kinds`含动态/隐藏kind，直接调用类型Handler；前端权威类型源码合同 | 后端包1.252s、墙钟9.233s；前端0.553s | 1 / 1（预期） | PASS：旧响应没有任何类型items，14项全部缺失；旧面板仍精确命中六项硬编码且没有后端类型请求 |
| 权威类型GREEN | 14类生产/兼容/动态矩阵、128/129字节边界；pagination/source/type四项前端合同 | 后端包1.236s、墙钟9.382s；前端0.606s | 0 / 0 | PASS：八种项目类型、三种兼容类型、内置/动态/隐藏resource kind均返回且可作为filter；未知翻译显示code，旧六项枚举零残留 |
| 未解析引用/百万行广域 | 本机两个DB URL+integration运行`go test ./internal/httpapi -run UnresolvedReference -count=1` | 包30.725s | 0 | PASS：BUG-105七来源投影、BUG-106类型、cursor/SQL合同和1M深页/前缀预算全部绿；页查询不加载类型注册表，继续单SQL |
| Schema/API/权限边界 | generation157；新route与既有route均为`reference.unresolved.read`；类型和分页调用图复核 | 定向检查 | 0 | PASS：无DDL/reset/回填/双写；新接口只读小注册表且读取故障失败关闭；列表DTO、cursor和搜索不变，type安全上限协调到128 |
| 后端发布门禁 | ordinary本机DB、移除integration变量：全仓Test；Vet；Build；tidy | Test12.748s；Vet3.901s；Build3.638s；tidy0.665s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建和模块图全绿；httpapi2.778s、queue4.039s、antiabuse3.354s |
| 前端发布门禁 | 直接枚举92个Node文件；ESLint；合法HTTPS Next16 build；build后TypeScript | Tests4.895s；Lint25.994s；Build26.299s；Type3.019s | 0 / 0 / 0 / 0 | PASS：217/217、零lint/类型错误、58页production build通过；首次Lint launcher未含bundled Node PATH而环境退出1，补齐同一runtime PATH后有效门禁通过 |
| 严重度与449台账 | UNRESOLVED→Medium/P1；严格及开放期verifier | 最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=325、OPEN=124；High=121、Medium=213、Low=58、UNRESOLVED=57；strict精确182 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零；未访问或重置远端/共享public |

## 2026-08-24：BUG-107 重复搜索请求代次

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 当前代码重新确认 | immutable finding与审计基准/当前panel diff；逐行检查submit、effect依赖、cleanup和finally | 定向检查 | 0 | PASS：旧实现重复首面同query只设loading且无effect依赖变化；当前PERF-059代码已加入每次递增的requestVersion，问题真实存在过且当前根因已修复 |
| 专项回归 | `node --test app/_lib/unresolved-reference-pagination.test.mts` | 0.469s | 0 | PASS：3/3；锁定重复submit推进代次、effect订阅代次及只有active请求结束loading，cursor重置/导航相邻合同同时绿 |
| 前端全量与静态门禁 | 直接枚举92个Node文件；新增测试后ESLint与TypeScript | Tests3.748s；Lint24.004s；Type2.868s | 0 / 0 / 0 | PASS：218/218、零lint/类型错误；重复查询新用例进入全量动态枚举，不依赖package.json静态子集 |
| 同生产代码发布门禁 | BUG-106后、BUG-107测试前已对同一生产代码执行后端全仓/Vet/Build/tidy及合法HTTPS Next production build | Test12.748s；Vet3.901s；GoBuild3.638s；tidy0.665s；NextBuild26.299s | 0 / 0 / 0 / 0 / 0 | PASS：BUG-107未再改生产源码；全部普通后端包与58页production build已通过，未以新增源码合同替代构建门 |
| Schema/API/数据库边界 | generation157；列表调用图及当前request状态边界复核 | 定向检查 | 0 | PASS：无Schema/API/数据库变更或访问；success/error均由最新请求finally恢复loading，旧代次cleanup后不能写状态 |
| 严重度与449台账 | UNRESOLVED→Medium/P1；严格及开放期verifier | 最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=326、OPEN=123；High=121、Medium=214、Low=58、UNRESOLVED=56；strict精确180 issues，开放期通过 |
| 格式/依赖/不可变边界 | 双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零 |

## 2026-08-24：TEST-001 前端自动化测试全树入口

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 新增`frontend-test-gate.test.mts`后定向执行，读取旧`package.json`测试命令 | 0.8s | 1（预期） | PASS：旧命令为47文件静态清单且另有`pretest`，与要求的`node --test`自动发现精确不符；当时仓库已有109文件，至少62个文件不会进入标准`test`步骤 |
| 定向GREEN | `node --test app/_lib/frontend-test-gate.test.mts` | 0.273s | 0 | PASS：1/1；唯一脚本为`node --test`、无pretest、真实多文件集合存在且门禁本身位于默认发现树 |
| 标准前端测试入口 | bundled Node24 PATH下`pnpm test` | 4.867s；发布复跑6.212s | 0 / 0 | PASS：自动发现110个`.test.mts`文件，261/261；旧`ban-reason`脚本枚举断言删除而两条业务行为断言继续通过 |
| 前端静态与生产门禁 | `pnpm typecheck`；`pnpm lint`；合法HTTPS环境`pnpm build` | Type5.842s；Lint22.468s；Build22.289s | 0 / 0 / 0 | PASS：零类型/Lint错误，Next16正式Turbopack production build生成58/58静态页；额外非发布Webpack兼容探针因既有生产`.mts`无loader退出1，审计副本当年的Webpack环境回退不替代当前仓库正式build命令 |
| 后端发布回归 | ordinary本机DB、移除integration变量：全仓Test；Vet；Build；tidy | Test13.727s；Vet2.492s；Build3.357s；tidy0.403s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建和模块依赖全绿；本项不改后端生产代码 |
| Schema/API/数据库边界 | package/test调用图；本机PG只读`public.schema_metadata`与临时namespace/连接检查 | 定向复核 | 0 | PASS：无Schema/API/生产runtime/锁文件变更；共享public仍generation155，外部连接0，命名ephemeral schema 0；仓库当前generation162未安装或reset |
| 严重度与449台账 | 既有High/P0；严格及开放期verifier | strict0.653s；allow0.639s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=362、OPEN=87；High=135、Medium=223、Low=59、UNRESOLVED=32；strict精确120 issues，开放期通过 |
| 格式/不可变边界 | 双仓`git diff --check`；外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；权威audit目录限定diff/status均为零；未推送、未访问远端或重置共享数据库 |

## 2026-08-24：TEST-021 通知Handler与未读行为矩阵

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 缺口RED | 当前测试文件清单与`test021_notification_handlers_integration_test.go`存在性门禁 | 0.7s | 1（预期） | PASS：旧仓库没有审计要求的Handler集成矩阵；只有路由/辅助函数、独立SQL及后续Finding测试，无法一次证明用户可见性到缓存真值的组合合同 |
| 首次真实PG行为 | generation162会话临时全Schema运行新Handler矩阵 | 包9.894s；墙钟12.378s | 1（预期） | PASS：夹具把固定`updated_at`放在数据库当前时间之后，read-all后仍有2条未读；该失败证明测试真实执行生产水位线时间谓词，修正为当前时间前10分钟而非弱化断言 |
| 定向GREEN | 修正时间夹具后同一矩阵；补充canonical alias后最终复跑 | 包11.061s/10.935s；墙钟19.630s/19.080s | 0 / 0 | PASS：A/B私有+广播两页、单条/全部已读、跨用户零receipt、缓存0→DB1校准、system403、`EN_us`缓存200和`pt-BR`400全部通过 |
| 通知专项广域 | 7组：新Handler、1M页/水位线、未读校准、notification Outbox、task/outbox原子、损坏AI结果、worker locale | 包23.360s；墙钟25.948s | 0 | PASS：1M fixture11.213s；两页精确2 SQL、read-all 1 SQL，kind与无筛选均命中direct/broadcast keyset索引；payload/result/item损坏与失败状态写可见，Outbox同事务 |
| 后端发布门禁 | ordinary本机DB并移除integration变量：全仓Test；Vet；Build；tidy | Test9.906s；Vet3.892s；Build3.435s；tidy0.396s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建和模块图全绿；新增集成测试在普通门中编译并按显式环境跳过数据库主体 |
| 前端发布门禁 | `pnpm test`；TypeScript；ESLint；合法HTTPS `pnpm build` | Tests7.280s；Type2.610s；Lint28.890s；Build23.915s | 0 / 0 / 0 / 0 | PASS：261/261、零类型/Lint错误、Next16正式Turbopack production build 58/58页通过；本项无前端生产变更 |
| Schema/数据库隔离 | gofmt；public generation/外部连接/命名ephemeral查询 | 定向复核 | 0 | PASS：新增Go文件gofmt残留0；公共Schema仍155，外部连接0，命名ephemeral schema0；测试临时generation162已Drop，无reset/远端访问 |
| 严重度与449台账 | 既有High/P0；严格及开放期verifier | strict0.678s；allow0.700s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=363、OPEN=86；High=135、Medium=223、Low=59、UNRESOLVED=32；strict精确119 issues，开放期通过 |
| 差异/不可变边界 | 双仓`git diff --check`；外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；权威audit目录限定diff/status均为零；未推送、未修改审计输入 |

## 2026-08-24：TEST-019 项目自动更新Worker完整生命周期

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 缺口RED | 审计要求映射及`test019_project_automation_worker_integration_test.go`存在性门禁 | 0.7s | 1（预期） | PASS：旧仓库没有执行生产Worker完整任务生命周期的端到端矩阵，既有日期、维护、身份和后续独立缺陷测试不能证明schedule/claim/retry/sync/publish组合 |
| 真实PG合同RED | 建立最小system autobot夹具后运行generation162新矩阵 | 包11.247s；墙钟19.242s | 1（预期） | PASS：测试原先只期待changelog section，真实Worker返回`[changelog minecraft_versions project_version]`；证明一次provider run实际贯穿三类同步并合并原子事件，断言据此收紧而非跳过路径 |
| 定向GREEN | `TestTEST019ProjectAutomationWorkerRetriesCompletesIdempotentlyAndRecoversLeasesIntegration` | 包11.242s；墙钟19.792s | 0 | PASS：重复schedule、503→pending退避→同run重试成功、actor/attempt/lease、三类section、同内容幂等、license dead-letter和过期lease恢复全部通过 |
| 自动更新专项广域 | 新TEST019及SEC-015、BUG-033/034/035/038/039、DB-006身份/补偿、全局镜像槽共10组 | 包90.171s；墙钟98.488s | 0 | PASS：10/10；首次组合还暴露DB-006夹具把临时generation写死148及新测试未显式seed actor，均按测试真实前置修正；当前generation162下镜像、扫描、通知、维护和Worker编排共同全绿 |
| 后端发布门禁 | ordinary本机DB并移除integration变量：全仓Test；Vet；Build；tidy | Test10.306s；Vet4.095s；Build3.373s；tidy0.397s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建和模块图全绿；新增集成测试普通门编译并按显式环境跳过数据库主体 |
| 前端发布门禁 | `pnpm test`；TypeScript；ESLint；合法HTTPS `pnpm build` | Tests6.761s；Type2.581s；Lint24.784s；Build23.471s | 0 / 0 / 0 / 0 | PASS：261/261、零类型/Lint错误、Next16正式Turbopack production build 58/58页通过；本项无前端生产变更 |
| Schema/数据库隔离 | gofmt；public generation/外部连接/命名测试Schema查询 | 定向复核 | 0 | PASS：两个相关Go测试gofmt差异0；公共Schema仍155，外部客户端0，命名测试Schema0；会话临时generation162均已Drop，无reset、远端或公网provider访问 |
| 严重度与449台账 | 既有High/P0；严格及开放期verifier | strict0.663s；allow0.645s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=364、OPEN=85；High=135、Medium=223、Low=59、UNRESOLVED=32；strict精确118 issues，开放期通过 |
| 差异/不可变边界 | 双仓`git diff --check`；外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；权威audit目录限定diff/status均为零；未推送、未修改审计输入 |

## 2026-08-24：OPS-005 / OPS-006 / DEAD-005 / TEST-020 SeedCrawler完整run状态机

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 缺口与编译RED | TEST020文件存在性；新增目标测试后运行真实integration目标 | 存在性0.6s；编译2.207s | 1 / 1（预期） | PASS：旧仓库无Worker生命周期文件，且`SeedCrawlerWorker`没有可测/可执行leaseDuration或heartbeatInterval，精确证明固定5分钟无续租实现不能满足新合同 |
| 定向GREEN | generation162真实PG TEST020；保留旧每轮4任务有界批处理；最终升级为8连接随机隔离数据库复跑 | 包12.151s/11.947s/7.886s；墙钟20.166s/19.979s/15.928s | 0 / 0 / 0 | PASS：全/部分provider故障、同run重试、dry-run、配置2/1、跨连接Worker cap、heartbeat、owner偷取、过期恢复全部通过；并发lane未把旧批处理吞吐降成每轮仅N任务 |
| SeedCrawler专项广域 | TEST020、BUG040/041/042、SEC016、100k/1M分页与全部爬虫辅助/源码合同共13组；多连接夹具最终复跑 | 包51.269s/46.139s；墙钟53.863s/48.706s | 0 / 0 | PASS：AI翻译/自动提交、first/last血缘、项目+来源+draft+candidate事务、并发预算、深游标计划及新run编排共同全绿；多连接TEST020同组无相互污染 |
| 结果分类与dry-run事实 | 可控Modrinth两分类503→成功、单分类503；候选/草稿/导入/项目计数 | 定向矩阵 | 0 | PASS：全失败provider0/2、candidate0、pending+future retry；attempt2成功2/0且candidate2；部分1/1 completed+failed1；dry-run drafts/imports/projects均0 |
| 全局容量与租约所有权 | 400ms lease、75ms heartbeat、阻塞provider、第二Worker、人工owner替换 | 定向矩阵 | 0 | PASS：max2为running/pending 2/1且两个随机owner，第三attempt0；max1为1/1；越过550ms lease仍future；stolen owner取消旧外呼且状态不覆写，过期后attempts2 completed |
| 后端发布门禁 | ordinary本机DB并移除integration变量：全仓Test；Vet；Build；tidy；多连接夹具后最终复跑 | 初次13.921/3.946/4.184/0.371s；最终8.048/3.655/3.157/0.361s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建和模块图全绿；新integration普通门编译并按显式环境跳过数据库主体 |
| 前端发布门禁 | `pnpm test`；TypeScript；ESLint；合法HTTPS `pnpm build` | Tests5.580s；Type2.857s；Lint24.889s；Build23.337s | 0 / 0 / 0 / 0 | PASS：261/261、零类型/Lint错误、Next16正式Turbopack production build 58/58页通过；既有maxConcurrency表单现在获得真实执行语义，无前端改动 |
| Schema/数据库隔离 | gofmt；public generation/外部连接/命名测试Schema与测试数据库查询 | 定向复核 | 0 | PASS：生产和测试Go文件gofmt差异0；公共Schema仍155，外部客户端0，命名测试Schema0；正则命名TEST020隔离数据库残留0，generation162仅装入该库并已精确Drop，无reset、远端或公网provider访问 |
| 严重度与449台账 | OPS005 High/P1、OPS006/DEAD005 Medium/P1、TEST020 High/P0；严格及开放期verifier | strict0.663s；allow0.649s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=368、OPEN=81；High=135、Medium=223、Low=59、UNRESOLVED=32；strict精确114 issues，开放期通过 |
| 差异/不可变边界 | 双仓`git diff --check`；外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；权威audit目录限定diff/status均为零；未推送、未修改审计输入 |

## 2026-08-24：BUG-108 并发删除后的空cursor页

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 当前代码重新确认 | immutable finding与审计基准/当前Handler diff；现有window/offset禁止合同 | 定向检查 | 0 | PASS：旧实现只能从行扫描total且空页保留0；当前PERF-059协议无total/offset/count window，问题根因已随cursor迁移消失 |
| 真实PG并发收缩 | 两行投影limit1取next cursor；删除全部；使用旧cursor调用精确page query | 包1.262s、墙钟9.357s | 0 | PASS：首面1项/hasMore/cursor；删除后200且`items=[]/limit=1/hasMore=false/nextCursor=''`，没有伪造全scope计数 |
| 第一方空页恢复 | `node --test app/_lib/unresolved-reference-pagination.test.mts` | 0.522s | 0 | PASS：4/4；Previous只由cursorHistory控制并pop前一cursor，空items或total不参与禁用；BUG-107重复搜索合同同时绿 |
| 未解析引用/百万行广域 | 两个DB URL固定本机并显式integration，运行完整`UnresolvedReference`组 | 包30.713s | 0 | PASS：并发空页、七来源、动态类型、cursor/SQL和1M深页/前缀预算全绿；page读取继续一SQL |
| 发布与全量测试 | ordinary后端全仓；直接枚举92个前端测试；新增测试后ESLint/Type；同生产代码Vet/Build/Next build沿用本轮已通过门 | GoTest9.943s；Node5.334s；Lint23.888s；Type2.899s | 0 / 0 / 0 / 0 | PASS：后端全部普通包；前端219/219、零lint/类型错误；生产源码自BUG-106完整Vet/GoBuild/58页Next build后未变化 |
| Schema/API边界 | generation157；cursor scope、limit+1和前端history调用图 | 定向检查 | 0 | PASS：无Schema/API/数据库持久变更；不以独立COUNT修补空页，不回退深OFFSET或全量window count |
| 严重度与449台账 | UNRESOLVED→Medium/P1；严格及开放期verifier | 最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=327、OPEN=122；High=121、Medium=215、Low=58、UNRESOLVED=55；strict精确178 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零 |

## 2026-08-23：BUG-103 站务逐翻译发布状态

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | generation155完整临时Schema：法语About草稿后读取英文；后台源码逐翻译status合同 | 后端包10.909s、墙钟18.610s；前端0.111s | 1 / 1（预期） | PASS：旧About把父项改draft，英语公开页立即404；旧后台只读取父status且translation没有状态字段 |
| generation156行为GREEN | BUG-102/103真实PG组合；前端逐翻译状态合同 | 后端包21.001s、墙钟28.924s；前端0.133s | 0 / 0 | PASS：法语About草稿不隐藏英/中；changelog中/法发布独立、draft-only不公开；后台详情返回逐locale状态；非法语言边界继续全绿 |
| 站务/故障/分页广域 | 两个DB URL固定本机55432并显式integration，运行`SiteAffairs&#124;SiteChangelog`完整组 | 包28.259s；墙钟35.961s | 0 | PASS：跨语言状态、公开/后台cursor、JSON/Scan/terminal错误可见性及BUG-102均通过 |
| 百万行真实PG计划 | 1M父项+1M translation；首/深公开和后台exact生产SQL及并发首项 | fixture7.091s；包8.303s | 0 | PASS：每页精确一条SQL；公开计划同时命中`idx_site_changelogs_public`与`idx_site_changelog_translations_published`，两个目标表无Seq Scan；深cursor可达且无重复 |
| Schema/空库/API边界 | generation156完整临时安装+自动drop；schema unit与reset文档；公开/后台DTO复核 | database0.872s | 0 | PASS：status非空/枚举CHECK和partial index存在；无迁移/回填/双读；公开DTO不变，后台translations新增status并由第一方消费；未访问/重置远端或共享public |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | Test14.035s；Vet3.490s；Build4.210s；tidy0.348s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；46个generation精确门禁同步到156；模块依赖无diff |
| 前端发布门禁 | 直接枚举89个Node文件；ESLint；合法HTTPS Next16 build；build后TypeScript | Tests3.154s；Lint24.335s；Build24.008s；Type2.882s | 0 / 0 / 0 / 0 | PASS：214/214、零lint/类型错误、58页production build通过 |
| 严重度与449台账 | UNRESOLVED→Medium/P1；严格及开放期verifier | strict0.666s；allow0.643s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=322、OPEN=127；High=121、Medium=210、Low=58、UNRESOLVED=60；strict精确188 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零 |

## 2026-08-23：BUG-094 AI 任务并发配置单一权威

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效双端RED | 后端DTO/默认JSON合同；前端type/form/i18n源码合同 | 后端包1.147s、墙钟9.173s；前端0.514s | 1 / 1（预期） | PASS：旧后端仍暴露并序列化从未执行的ConcurrencyLimit；前端type、默认值、表单及文案仍允许管理员保存该字段 |
| API/UI定向GREEN | 字段反射+默认序列化+strict旧请求；前端唯一权威源码/双语合同 | 后端包1.173s、墙钟9.199s；前端0.478s | 0 / 0 | PASS：新API输出零字段、旧PUT字段400；生产源码零`concurrencyLimit`；AI表单删除输入，NATS maxConcurrent控件保留，双语说明单实例与cluster容量 |
| 真实JetStream执行权威 | `TestTaskMaxConcurrentIsTheAuthoritativeExecutionLimitIntegration` | queue包1.457s；墙钟3.743s | 0 | PASS：4条AI task同时发布，前2条启动后300ms内第三条不能进入；maximum active精确2；释放容量后4条全部完成 |
| AI/Queue/前端广域 | `AI&#124;Translation`目标组；queue完整包；动态前端测试 | httpapi0.147s；queue3.691s；前端213 tests4.128s | 0 | PASS：模型绑定、每类型实际timeout、任务恢复/Outbox/JetStream及后台配置相邻行为全绿 |
| Schema/API/兼容边界 | generation155；加密setting typed读取、严格写、NATS重配与多实例语义复核 | 定向检查 | 0 | PASS：无DDL/reset/回填/双写/新锁；旧加密键惰性丢弃并在下次保存清除；AI taskModels breaking删字段，NATS maxConcurrent保持唯一每实例执行权威 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | Test10.986s；Vet3.430s；Build3.790s；tidy0.337s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；httpapi2.810s、queue3.990s、serverprobe3.038s；模块依赖无diff |
| 前端发布门禁 | 动态枚举88个Node文件；ESLint；合法HTTPS Next16 build；build后TypeScript | Tests3.151s；Lint23.555s；Build23.767s；Type9.508s | 0 / 0 / 0 / 0 | PASS：213/213、零lint/类型错误、58页production build通过 |
| 严重度与449台账 | 既有Medium/P1保持；严格及开放期verifier | strict0.686s；allow0.647s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=320、OPEN=129；High=121、Medium=208、Low=58、UNRESOLVED=62；strict精确192 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零；production字段零命中，仅防回归测试保留字面量 |

## 2026-08-23：BUG-102 站务管理端严格语言身份

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | 完整临时Schema预置已发布简中About；非法`ko-KR`管理GET | 包10.985s；墙钟18.645s | 1（预期） | PASS：旧入口返回200并把非法路径解析成`zh-CN`，响应直接暴露简中正式标题/正文，证明公开回退进入管理身份边界 |
| unit与真实PG GREEN | 8语言/alias/非法矩阵；About非法GET/PUT、合法alias PUT；changelog非法POST/PUT | 包10.907s；墙钟18.513s | 0 | PASS：非法管理输入全部400且简中About、changelog父日期/状态/翻译及新建行数不变；`fr_fr`独立保存为`fr-FR`；公开unknown仍回退zh-CN |
| 站务相邻组 | `go test ./internal/httpapi -run '(SiteAffairs&#124;SiteChangelog)' -count=1` | 包0.129s；墙钟2.605s | 0 | PASS：站务分页、cursor scope、错误可见性和新增语言边界相邻合同全绿 |
| Schema/API边界 | generation155；管理与公开调用图、事务前校验、第一方selector复核 | 定向检查 | 0 | PASS：无DDL/reset/回填/双写/新查询/N+1；合法DTO与公开回退不变；非法管理语言收紧400且所有副作用前失败；BUG-103/104未合并关闭 |
| 后端发布门禁 | ordinary全仓Test（integration变量移除、`DATABASE_URL`固定本机）；Vet/Build/tidy | Test11.532s；Vet3.587s；Build3.897s；tidy0.363s | 0 / 0 / 0 / 0 | PASS：全部普通包全绿；模块依赖无diff |
| 前端发布门禁 | 直接枚举88个Node文件；ESLint；合法HTTPS Next16 build；build后TypeScript | Tests3.133s；Lint23.242s；Build24.890s；Type2.513s | 0 / 0 / 0 / 0 | PASS：213/213、零lint/类型错误、58页production build通过；本项无前端代码或协议迁移 |
| 严重度与449台账 | UNRESOLVED→Medium/P1；严格及开放期verifier | strict0.718s；allow0.637s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=321、OPEN=128；High=121、Medium=209、Low=58、UNRESOLVED=61；strict精确190 issues，开放期通过 |
| 格式/依赖/不可变边界 | gofmt、双仓`git diff --check`、模块SHA-256、外层audit限定diff/status | 最终复核 | 0 | PASS：双仓空白错误0；模块哈希保持`695B...728`/`C151...783`；权威audit目录限定diff/status均为零 |

## 2026-08-24：OPS-011 / MAP-008 / DEAD-009 / TEST-030 搜索滚动代次与完整状态机

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效源码RED | 新合同要求superseded代次门、单一registry并禁止旧collection mapping及无行为failed分支 | 包0.897s；墙钟3.0s | 1（预期） | PASS：旧实现缺两个version guard与registry查找，仍含`collectionKind`默认resources和`failed`/末尾continue，精确复现四项登记缺口 |
| unit/源码GREEN | TEST030合同、5集合/7文档registry往返、数据库CHECK闭集、unknown拒绝及全部搜索单元 | 包0.983s；墙钟3.2s | 0 | PASS：Schema、归属、ID keyset SQL和loader均由同一有序registry派生；生产映射switch/默认resources/死failed分支归零 |
| generation162完整状态机 | 8连接随机隔离数据库+内存Typesense；同版本双Worker、v3→v4毒文档、降级拒绝、外部删除/SQL retry双故障 | 用例5.47s；目标包8.255s；墙钟10.4s | 0 | PASS：初始只create5集合；毒creator保持v3 alias并failed，恢复时四类各建1次、creator2次；v3 mutation0/attempt0；双错误都返回，修复后queue0 |
| Searchindex完整integration | 显式两个本机DB环境变量和integration开关；`go test ./internal/searchindex -count=1` | 包8.312s；墙钟10.3s | 0 | PASS：Typesense协议、双Worker租约、服务500、投影、v3/v4、毒文档、retry SQL、预算和全部真实PG搜索组通过 |
| 100k/1M重建规模 | 稳定ID keyset遍历、每页最多500、深页EXPLAIN及8MiB JSONL/数组预算 | 100k 48.99ms；1M 615.11ms | 0 | PASS：两档完整遍历且深页命中主键索引无Seq Scan；20MiB文档组拆为至少3个不超过8MiB请求 |
| Typesense/SQL fallback组合 | `TestServerCatalogAuthoritativeIndexCursorAndMillionRowCardFetchIntegration` | 用例39.05s；包40.136s | 0 | PASS：100k/1M精确card fetch与SQL fallback均索引驱动；Typesense两页绑定权威cursor，index失效后旧cursor503，新SQL首/续页无重漏 |
| 后端发布门禁 | ordinary本机DB且移除integration变量；全仓Test/Vet/Build/tidy | 15.265/11.545/12.231/0.762s | 0 / 0 / 0 / 0 | PASS：全仓普通测试、静态分析、构建及模块图全绿；searchindex普通包0.667s |
| 前端发布门禁 | `pnpm test`；TypeScript；ESLint；合法HTTPS正式Turbopack build | Tests261/261 4.888s（墙钟6.134s）；Type4.946s；Lint29.980s；Build24.4s | 0 / 0 / 0 / 0 | PASS：全树261用例、类型、lint及58页production build全绿；公开搜索协议无变化 |
| Schema/隔离/不可变边界 | gofmt、双仓diff、public generation、客户端、命名Schema/DB、外层audit限定diff/status | 最终复核 | 0 | PASS：无生产DDL且generation162保持；本机public155、外部客户端0、命名测试Schema0、TEST030数据库0；gofmt/diff/immutable均0 |
| 严重度与449台账 | OPS011 High/P1、MAP008 Medium/P1、DEAD009 Low/P2、TEST030 High/P0；strict/allow verifier | 合计墙钟1.8s | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=372、OPEN=77；High=135、Medium=223、Low=59、UNRESOLVED=32；strict精确110 issues，开放期通过 |

## 2026-08-24：TEST-031 成长系统核心事务矩阵

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 缺口RED | TEST031组合状态机测试文件存在性 | 墙钟0.7s | 1（预期） | PASS：仓库没有并发奖励、角色/版本/cache与投影失败重放的同一真实PG矩阵；既有文件仅逐项覆盖纯函数或局部事务 |
| 目标真实PG GREEN | `TestTEST031ProgressionRewardsRolesVersionsAndFailureReplayIntegration`；8连接generation162随机隔离数据库 | 用例5.15s；包6.088s；墙钟8.5s | 0 | PASS：并发达标一次奖励、completed重复不再奖励、同role双来源、等级2/3、version/cache、currency trigger全回滚和修复后重放全部精确通过 |
| Progression完整组 | 显式本机双DB环境变量与integration；`go test ./internal/progression -count=1 -v` | 新用例5.56s；caller tx10.95s；包16.708s；墙钟18.654s | 0 | PASS：strict condition/rewards、时区周期、角色来源、调用者事务和TEST031组合矩阵全绿 |
| HTTP相邻组 | `Progression&#124;TaskConfiguration&#124;TaskPayload&#124;TaskCurrency&#124;RoleTrack&#124;ShiftRoleTrack` | 包11.317s；墙钟14.458s | 0 | PASS：损坏配置/未知货币500、任务保存边界、角色线路锁序与移动语义继续通过 |
| 后端发布门禁 | ordinary本机DB且移除integration变量；全仓Test/Vet/Build/tidy | 9.799/4.754/5.796/0.782s | 0 / 0 / 0 / 0 | PASS：新integration在普通门正确编译并只跳过数据库主体；全仓、静态分析、构建和模块图全绿 |
| 前端发布门禁 | 复用紧邻TEST030且本项零前端/生产协议修改的完整门 | Tests261/261 4.888s；Type4.946s；Lint29.980s；Build24.4s | 0 / 0 / 0 / 0 | PASS：全树测试、类型、lint和58页production build全绿；任务/经济/权限DTO未变 |
| 原子事实 | 两并发事件、completed重复、失败trigger前后直接查询progress/rewarded/余额/流水/role/version/cache | 定向矩阵 | 0 | PASS：并发为2/1/25/level2/diamond3且两流水1；重复progress4但流水仍1；故障全0、version不变/cache保留；重放1/1/100/level3/diamond7且各一流水、version+1/cache清除 |
| Schema/隔离/台账 | gofmt/双仓diff/immutable；public generation/外部客户端/命名测试Schema与DB；strict/allow verifier | 合计墙钟2.1s | 0 / 1（预期）/ 0 | PASS：本机public155、外部客户端0、命名测试Schema0、TEST031数据库0；gofmt/diff/immutable均0；audit/ledger449，CLOSED=373、OPEN=76、UNRESOLVED=32，strict109 issues且allow通过 |

## 2026-08-24：DEAD-010 / TEST-032 资源版本与基础设施组合边界

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效真实PG RED | generation162完整临时Schema；两个相同resource ID+空ID+4,096未知ID；源码死投影合同 | 用例9.77s；包10.897s；墙钟19.8s | 1（预期） | PASS：旧实现虽恒1 SQL，但第二个重复item的versions精确为0；生产SQL/Scan仍存在从未消费的`has_manual_detail` |
| 资源装饰GREEN | 同一4,099项、版本/detail/localization、query tracer、binding表重命名故障 | 用例9.74s；包10.856s；墙钟19.9s | 0 | PASS：唯一非空ID4,097个仍只1 SQL；两个重复项各1目标version/hasDetail/name；空+4,096未知均空数组；DB故障error且零部分版本；死列零命中 |
| HTTP基础设施广域 | `Compression&#124;Infrastructure&#124;DeadLetter&#124;TEST032&#124;ResourceVersion` | 包32.687s；墙钟约33s | 0 | PASS：12分支可压缩文本协商、持久metrics故障、150/1M死信分页/计划、双类管理员重放、资源版本相邻路径全绿 |
| Queue可靠性广域 | Outbox状态持久化、断连重试、stale lease、双dispatcher、published metric | 包2.985s；墙钟5.334s | 0 | PASS：claim、发布/失败状态、租约恢复、并发互斥和持久指标保持完整错误语义 |
| 后端发布门禁 | ordinary本机DB且移除integration变量；全仓Test/Vet/Build/tidy | 14.559/10.948/11.875/0.779s | 0 / 0 / 0 / 0 | PASS：全仓测试、Vet、Build和模块图全绿；httpapi3.446s、queue4.011s |
| 前端发布门禁 | 复用紧邻完整门；本项零前端文件和公开DTO变化 | Tests261/261 4.888s；Type4.946s；Lint29.980s；Build24.4s | 0 / 0 / 0 / 0 | PASS：全树测试、类型、lint和58页production build全绿 |
| Schema/隔离/台账 | gofmt/双仓diff/immutable；本机public/外部客户端/命名临时对象；strict/allow verifier | 合计墙钟3.3s | 0 / 1（预期）/ 0 | PASS：本机public generation155且未被本项写入，外部客户端0、命名测试Schema0、TEST032数据库/表0；gofmt/diff/immutable均0；audit/ledger精确449，CLOSED=375、OPEN=74、UNRESOLVED=32，strict精确107 issues且allow通过 |

## 2026-08-24：OPS-012 / MAP-009 / TEST-033 版本目录同步状态机

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效compile RED | 新TEST033要求startup runner、PG lease、typed in-progress和结构化`sourceURLs` | 墙钟2.49s | 1（预期） | PASS：旧代码在8个目标符号/字段处编译失败，精确证明进程锁、次日首次调度及单URL协议尚未满足新合同 |
| 目标真实PG GREEN | `^TestTEST033`；双pool、随机隔离generation162数据库、7个假来源 | 用例8.08s；包8.472s；墙钟11.21s | 0 | PASS：启动立即1次；租约竞争生产函数及Handler 409/0请求；释放后7请求发布NeoForge三artifact/两来源；全上游故障零设置/artifact变化 |
| 版本目录相关组 | default/code/config/artifact/cache/NeoForge/TEST033精确正则；显式本机双DB integration | 包8.472s；墙钟11.21s | 0 | PASS：安全空默认、大小写唯一、损坏配置500、离线artifact、ETag/失败重试/全局预算、1.21/26.x稳定版本及新状态机全部通过 |
| 探索性宽正则 | 初始`Minecraft&#124;NeoForge&#124;LoaderArtifact&#124;TEST033` | 包27.961s；墙钟39.434s | 1（非门禁） | INFO：新版本目录相关用例全绿；正则额外命中3个BUG088皮肤测试，它们按预期拒绝共享public generation155而要求162；未reset共享库，随后以精确相关组通过替代 |
| 后端发布门禁 | ordinary本机DB且移除integration变量；全仓Test/Vet/Build/tidy | 14.321/10.352/11.547/0.421s | 0 / 0 / 0 / 0 | PASS：全仓测试、Vet、Build和模块图全绿；httpapi3.350s，新增integration在普通门正确编译并跳过数据库主体 |
| 前端发布门禁 | `pnpm test`；TypeScript；ESLint；合法HTTPS正式Turbopack build | Tests262/262 5.162s（墙钟5.736s）；Type8.999s；Lint33.961s；Build30.885s | 0 / 0 / 0 / 0 | PASS：新增多来源消费合同及全树262用例、类型、lint、58页production build全绿 |
| Schema/隔离/台账 | gofmt/双仓diff/immutable；public generation/客户端/命名Schema与DB；strict/allow verifier | 合计墙钟2.1s | 0 / 1（预期）/ 0 | PASS：本机public generation155、外部客户端0、命名测试Schema0、TEST033数据库0；gofmt/diff/immutable均0；audit/ledger精确449，CLOSED=378、OPEN=71、UNRESOLVED=32，strict精确104 issues且allow通过 |

## 2026-08-24：OPS-013 / DEAD-011 / TEST-034 MRPack生成物提交与完整状态机

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效基线RED | 对`git show HEAD`的Worker/Schema执行新合同检查 | 墙钟0.567s | 1（预期） | PASS：基线精确缺少`compensateUnlinkedFavoriteExportArtifact`、`recoverOrphanedArtifacts`，且仍含死`report_snapshot` |
| 目标真实PG GREEN | `^TestTEST034FavoriteExportArtifactCommitCompensationAndRecoveryIntegration$`；8连接随机generation163数据库+fake OSS | 用例约5.2s；包6.632s；墙钟15.127s | 0 | PASS：鉴权403/授予放行、私密集合404、foreign detail404/download410；四次PUT覆盖ready、即时墓碑、重试和双失败校准；linked保护、307下载、expired墓碑及3条历史保留 |
| MRPack完整广域 | 显式本机双DB integration；FavoriteModpack/FavoriteExport/MRPack/SEC024/GeneratedOSSObject/TEST034/删除集合组合 | 包115.454s；墙钟122.4s | 0 | PASS：配额并发、权威快照、依赖图/规模、报告计数/重建、租约/错误、OSS登记/取消/过期及历史分页组合全绿 |
| Schema/generation | database全包；generation163随机空库Migrate；Schema禁止死列并要求两个部分索引 | database0.119s；Migrate包含于目标用例 | 0 | PASS：51处当前generation合同一致；完整Schema/FK索引门通过；information_schema中favorite task `report_snapshot`为0 |
| 后端发布门禁 | ordinary本机DB且移除integration变量；全仓Test/Vet/Build/tidy | 20.297/5.201/5.752/0.666s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿；httpapi9.325s，新增integration普通门编译通过 |
| 前端发布门禁 | `pnpm test`；TypeScript；ESLint；合法HTTPS正式Turbopack build | Tests262/262 5.826s（墙钟6.735s）；Type3.384s；Lint23.044s；Build23.429s | 0 / 0 / 0 / 0 | PASS：无前端协议修改；全树262用例、类型、lint和58页production build全绿 |
| Schema/隔离/台账 | gofmt/diff/immutable；public generation/客户端/命名Schema与DB；strict/allow verifier | 合计墙钟约1.2s | 0 / 1（预期）/ 0 | PASS：gofmt/diff/immutable零差异；本机public generation155、其他客户端0、命名TEST033/034 Schema0、TEST034数据库0；audit/ledger精确449，CLOSED=381、OPEN=68、UNRESOLVED=32，strict精确101 issues且allow通过 |

## 2026-08-24：TEST-035 整合包导入资源边界与Worker终态

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效行为RED | 新`TestTEST035ModpackArchiveCentralDirectoryIsBounded`在合法小索引后追加4,097个entry并调用旧读取器 | 用例约1.1s；包1.111s；墙钟9.593s | 1（预期） | PASS：旧实现返回nil，证明32MiB目标索引限制发生在整份中央目录解析之后，不能限制无关entry资源消耗 |
| 资源边界GREEN | `^TestTEST035Modpack(ArchiveCentralDirectoryIsBounded&#124;IndexIsBounded)$` | 用例0.09s | 0 | PASS：4,097项中央目录在`zip.OpenReader`前拒绝；32MiB+1高压缩索引由uncompressed header门拒绝；生产常量同时限定1GiB归档、8MiB中央目录和2,000索引项 |
| 真实Worker GREEN | `^TestTEST035ModpackImportWorkerBoundariesAndFinalStatesIntegration$`；8连接随机generation163数据库+本机TLS provider | 用例5.16s；包5.963s；墙钟17.151s | 0 | PASS：成功completed/100且选择new release primary；官方CDN身份保留、攻击域降级；team503、1GiB+1和2001项均failed/25/finished/result null；所有临时目录清理 |
| 导入相关广域 | `TEST035/Modrinth identity/index/selection/locale/auxiliary/provider client/config/outbox`精确正则；显式本机双DB integration | 包约9.3s；墙钟9.82s | 0 | PASS：两平台既有确定选择、未知语言、辅助错误、凭据origin、私网/redirect和可靠导入Outbox回归全绿 |
| 后端发布门禁 | ordinary本机DB且移除integration变量；全仓Test/Vet/Build/tidy | 42.84/9.11/9.93/约1.2s | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建和模块图全绿；新增隔离Worker在普通门可执行并自动Drop |
| 前端发布门禁 | 复用紧邻OPS013/TEST034完整门；本项零前端与公开DTO变化 | Tests262/262 5.826s；Type3.384s；Lint23.044s；Build23.429s | 0 / 0 / 0 / 0 | PASS：全树测试、类型、lint和58页正式Turbopack build保持已验证状态 |
| 隔离/台账 | gofmt/diff/immutable；public generation/客户端/TEST035数据库；strict/allow verifier | 合计墙钟约3.1s | 0 / 1（预期）/ 0 | PASS：gofmt/diff/immutable零差异；本机public generation155、其他客户端0、TEST033/034/035命名Schema0、TEST035数据库0；audit/ledger精确449，CLOSED=382、OPEN=67、UNRESOLVED=32，strict精确100 issues且allow通过 |

## 2026-08-24：MAP-010 / LEGACY-017 / TEST-040 评分写入权威与状态机

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| canonical有效RED | `TestNormalizeRatingTargetTypeAcceptsOnlyCanonicalTypes`对五个开发期别名要求空 | 包约1s；墙钟16.45s含隔离库安装 | 1（预期） | PASS：旧实现五项分别返回`minecraft_server/resource_pack/shader_pack`，精确证明旧协议仍被维护 |
| 队列有效真实PG RED | TEST040在queue附加mutation计数trigger，生产Handler创建首条评分 | 用例5.30s；包6.090s；墙钟16.90s | 1（预期） | PASS：旧路径出现`insert/update=1/1`而合同为`1/0`，证明评分trigger后Handler再次显式enqueue |
| 目标真实PG GREEN | `^TestTEST040RatingPermissionsTransactionsTriggersVisibilityAndConcurrencyIntegration$`；8连接随机generation163数据库 | 用例5.42s；包6.211s；墙钟16.763s | 0 | PASS：权限403、canonical成功、五别名/待审/隐藏404；增改删单次trigger，故障全回滚；并发1评分/8一致维度/1队列；投影1→0 |
| 评分/PERF050广域 | `Rating&#124;TEST040&#124;EffectivePromotionPower`精确组；显式本机双DB integration | 墙钟15.76s | 0 | PASS：维度/请求、canonical、summary/item/reviews、稳定cursor与百万行既有索引计划、热度衰减及新状态机全绿 |
| 后端发布门禁 | ordinary本机DB且移除integration变量；全仓Test/Vet/Build/tidy | 48.72/8.93/9.73/1.3s | 0 / 0 / 0 / 0 | PASS：全仓测试、Vet、Build和模块图全绿；随机TEST035/040数据库均自动清理 |
| 前端发布门禁 | 复用紧邻完整门；`rating-api.ts`九类型已全canonical且本簇零前端文件 | Tests262/262 5.826s；Type3.384s；Lint23.044s；Build23.429s | 0 / 0 / 0 / 0 | PASS：现有第一方无需迁移；全树测试、类型、lint和58页正式build保持已验证状态 |
| 隔离/台账 | gofmt/diff/immutable；public generation/客户端/TEST040数据库；strict/allow verifier | 合计墙钟约3.1s | 0 / 1（预期）/ 0 | PASS：gofmt/diff/immutable零差异；本机public generation155、其他客户端0、TEST033/034/035/040命名Schema0、TEST040数据库0；audit/ledger精确449，CLOSED=385、OPEN=64、UNRESOLVED=32，strict精确97 issues且allow通过 |

## 2026-08-24：LEGACY-018 / STYLE-006 / TEST-041 收藏成员契约与行为状态机

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效行为RED | TEST041真实Handler按隐藏`t041hid01`和moderator可见`t041mod02`精确解析目标；旧字段/本地化源码合同 | 定向迭代 | 1（预期） | PASS：旧可见性片段缺最外层括号，调用SQL成为`route.id AND public OR privileged`，请求隐藏ID实际解析出无关`t041mod01`；生产仍接受`entityKey`且下载helper直接抛中文，精确复现三项缺口 |
| 目标真实PG GREEN | `^TestTEST041FavoriteHandlersAndFrontendStateMachineIntegration$`；8连接generation163随机隔离数据库 | 包约6.47s；墙钟15.23s | 0 | PASS：未认证401、默认/公私集合、精确目标解析、隐藏404、未知类型400、legacy-only与混合非法集合400零副作用、trigger故障500回滚、多集合PUT/PATCH、公开两页过滤无重漏 |
| Favorite真实PG广域 | TEST041、SEC035可见性、SEC036额度、BUG089替换、ARCH024错误和PERF051分页6项精确组 | 包25.949s；墙钟28.807s | 0 | PASS：目标可见性、collection所有权、quota稳定码、全量替换原子性、数据库错误分类和百万行有界分页相邻合同全部保持 |
| 前端行为与发布门 | `favorite-behavior`及相关11文件；全树test；TypeScript；ESLint；合法HTTPS正式Turbopack build | 定向通过；266/266 7.404s；Type12.18s；Lint35.09s；Build32.77s | 0 / 0 / 0 / 0 / 0 | PASS：多集合勾选/增删、新建立即可见选中、失败保留重试状态、生产零`entityKey`、稳定ApiError与locale映射；58页正式build通过 |
| 后端非集成发布门 | ordinary本机DB并移除integration变量；全仓Test/Vet/Build/tidy | Test40.18s；Vet7.30s；Build7.12s；tidy0.94s | 0 / 0 / 0 / 0 | PASS：全仓普通测试、静态分析、构建与模块图通过；go.mod/go.sum零变化 |
| 探索性全DB集成 | 先误用共享public155并与随后generation163随机库串行包测试重叠；两个进程竞争本机PG共享锁 | 最长10m | 1（非门禁） | INFO：命中generation不匹配、PostgreSQL `out of shared memory`、既知sticker临时索引42P07及seed夹具缺口；未出现TEST041/Favorite定向回归。该环境性失败不记为全量通过，也不替代已通过的隔离真实PG组合门 |
| 隔离/台账 | gofmt、双仓diff、immutable audit；public generation/客户端/随机数据库；strict/allow verifier | 最终复核 | 0 / 1（预期）/ 0 | PASS：共享public仍155、其他测试客户端0、`fullintegration_*`/`test041_favorite_*`数据库0；模块零变化；audit/ledger精确449，CLOSED=388、OPEN=61、UNRESOLVED=32，strict预期94 issues且allow通过 |

## 2026-08-24：开发期残留、格式与测试组织清理

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效残留RED | 当前HEAD源码、`gofmt -l`审计登记目录、Git跟踪与首轮Lint | 定向复核 | 1（预期） | PASS：旧未读/admin nav路由、dashboard转发、locale死参数、混放测试和3个tracked `.idea`均存在；原14个Go文件仍有9个formatter命中；删除卡片locale后首轮Lint又准确暴露列表locale二级残留 |
| 定向结构GREEN | repository hygiene、httpapi residue/测试归位、OSS/APNG/role track及前端residue合同 | 后端工具1.942s/httpapi1.071s；前端0.127s | 0 | PASS：全仓Go格式门、IDE删除、旧路由/wrapper归零、canonical dashboard/未读绑定、权限测试单域和前端死参数链全部通过；四个移动行为测试原断言保留 |
| 后端发布门禁 | ordinary本机DB且移除integration变量；全仓Test/Vet/Build | Test33.150s；Vet+Build10.862s | 0 / 0 / 0 | PASS：httpapi27.955s、tools/remediation0.933s；全仓测试、静态分析和构建通过，本簇无数据库集成行为或模块变化 |
| 前端发布门禁 | 全树test；TypeScript；ESLint；合法HTTPS正式Turbopack build | 267/267 4.696s（Test+Type墙钟8.348s）；Lint+Build48.958s | 0 / 0 / 0 / 0 | PASS：首轮1个unused warning已继续修净；最终零lint/类型错误，58页production build通过 |
| Schema/API/仓库边界 | generation163；生产残留rg、`.idea`删除、模块diff、双仓diff/immutable audit | 最终复核 | 0 | PASS：无DDL/数据库访问；删除2个旧GET、3个IDE文件、2个Handler wrapper及固定导航映射；canonical未读/dashboard合同和第一方调用保留 |
| 严重度与449台账 | 七项均Low/P2；strict/allow verifier | 最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=395、OPEN=54、UNRESOLVED=32，strict预期87 issues且allow通过 |

## 2026-08-24：TEST-044 经济与等级权限组合状态机

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效覆盖RED | 当前测试清单与审计矩阵对照；新TEST044先经真实空库/Handler执行 | 定向复核 | 1（预期） | PASS：既有独立测试已覆盖checked算术、单helper和等级Service，但没有一个真实JWT Handler矩阵组合鉴权、转账、签到、库存/热度、管理员流水与轨道撤销；首轮夹具也证明隔离空库不能假设完整RBAC种子 |
| 目标真实PG GREEN | `^TestTEST044EconomyPermissionsConcurrencyInventoryAndLevelRevocationIntegration$`；8连接generation163随机数据库 | 用例5.61s；包6.716s；墙钟15.582s | 0 | PASS：前置403、500转账、两路超支、out-of-range、签到一次、商品二层权限、买2并发用2、sequence/power、管理员+30、轨道启用/清空/manual与Session全部精确通过 |
| 经济/等级广域 | 经济安全/错误、无效任务、TEST044及10万等级规模；随后progression调用者事务/任务配置/TEST031 | httpapi包82.732s；progression14.879s；墙钟101.852s | 0 | PASS：10万用户请求各6 SQL、200批次/200语句、深页主键索引、supersede/过期租约恢复；奖励并发、事务失败重放、多来源角色与损坏配置失败关闭保持 |
| 后端发布门禁 | ordinary本机DB并移除integration变量；全仓Test/Vet/Build/tidy | Test36.736s；Vet+Build+tidy9.809s | 0 / 0 / 0 / 0 | PASS：httpapi31.757s、tools/remediation0.964s；普通全仓、静态分析、构建和模块图通过；本项没有新增依赖或生产文件 |
| 前端发布门禁 | 复用紧邻开发残留簇的完整门；本项零前端/DTO变化 | 267/267 4.696s；Type通过；Lint/Build48.958s | 0 / 0 / 0 / 0 | PASS：全树测试、类型、零warning lint和58页正式Turbopack build保持已验证状态 |
| 隔离/台账 | gofmt/双仓diff/immutable；public generation/客户端/TEST044数据库；strict/allow verifier | 最终复核 | 0 / 1（预期）/ 0 | PASS：本机public155、其他测试客户端0、`test044_economy_*`数据库0；audit/ledger精确449，CLOSED=396、OPEN=53、UNRESOLVED=32，strict预期86 issues且allow通过 |

## 2026-08-24：TEST-043 内容本地化、审核与AI任务组合门

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 覆盖缺口复核 | immutable TEST043证据与当前测试清单逐项对照 | 定向复核 | 0 | PASS：SEC038、BUG092-094、ARCH026、OPS019、PERF054/055已有独立行为门；唯一未组合覆盖的是实际供应商故障、Worker terminal状态、业务落库和重复消息幂等，新增测试精确补此缺口而不复制10万夹具 |
| 目标真实PG+TLS GREEN | `^TestTEST043ContentAIWorkerFailureSuccessAndDuplicateDeliveryIntegration$`；6连接generation163随机数据库 | 用例5.23s；包5.362s | 0 | PASS：无权限403；首任务provider503→failed/零translation；failed重复投递零外呼；第二任务成功写17/9 tokens、translation与completed；completed重复投递零外呼；GET和缓存命中200 |
| 内容AI跨根因矩阵 | 公开读取、语言权重、Outbox提交/恢复/计划、结果损坏、审核冲突、10万历史、大差异及TEST043 | 包28.018s | 0 | PASS：公开GET零任务，q权重八分支；任务/Outbox回滚和孤儿/陈旧恢复；损坏completed拒绝；stale审核409；100k双来源深页命中索引；1.326MiB快照固定2 SQL/128行/4,904B |
| JetStream执行与恢复 | `TestTaskMaxConcurrentIsTheAuthoritativeExecutionLimitIntegration`；`TestOutboxRetriesAfterJetStreamDisconnectIntegration` | 包2.826s | 0 | PASS：四消息最大active精确2；断线后event可重试、重连发布成功，数据库任务事实保持权威 |
| 后端发布门禁 | ordinary本机DB且移除integration变量；全仓Test/Vet/Build/tidy | httpapi35.850s；tidy1.1s；Vet/Build4.9s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建和模块图通过；新增文件仅为集成测试，无生产或依赖变更归因 |
| 前端发布门禁 | 复用紧邻开发残留簇完整门；本项零前端/DTO变化 | 267/267 4.696s；Type通过；Lint/Build48.958s | 0 / 0 / 0 / 0 | PASS：全树测试、类型、零warning lint和58页正式Turbopack build保持已验证状态 |
| 隔离/台账 | gofmt/双仓diff/immutable；public generation/客户端/TEST043数据库；strict/allow verifier | 最终复核 | 0 / 1（预期）/ 0 | PASS：本机public155、其他客户端0、`test043_content_ai_*`数据库0；audit/ledger精确449，CLOSED=397、OPEN=52、UNRESOLVED=32，strict预期85 issues且allow通过 |

## 2026-08-24：LEGACY-004 / TEST-006 认证原语闭集

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 旧算法有效向量 | 100000轮PBKDF2-HMAC-SHA256已知password/salt/hash；删除前生产解析路径 | 定向复核 | 0 | PASS：向量格式满足旧实现全部迭代/salt/key条件，证明测试锁定真实旧兼容而非随意坏字符串；当前Password/Code两入口均拒绝 |
| Security定向GREEN | Password Argon2/旧向量；Token往返/随机身份/12类无效矩阵/Bearer | 包1.368s | 0 | PASS：Argon2正确/错误/恶意参数不回归；有效PBKDF2两入口false；Token字段往返，两次43字符Session ID不同，所有错误Header/签名/时钟/Claims与Bearer空白拒绝 |
| 认证相邻组 | `Auth、Login、Yggdrasil、Password、Token、Session` | security0.249s；httpapi5.890s | 0 | PASS：dummy Argon2 login hash、认证缓存/撤销、Yggdrasil核心和设置合同保持通过 |
| 后端发布门禁 | ordinary本机DB并移除integration变量；全仓Test/Vet/Build/tidy | httpapi36.295s；Vet/Build6.3s；tidy1.0s | 0 / 0 / 0 / 0 | PASS：全部普通包、静态分析、构建和模块图通过；无新增依赖，模块文件既有工作树变化不归因于本簇 |
| Schema/API/前端 | generation163；调用链和当前生成格式；复用紧邻前端完整门 | 定向复核 | 0 | PASS：无DDL/数据库写/前端/DTO变化；PBKDF2是开发期明确breaking删除，Argon2id与Token成功协议不变；前端267/Type/Lint/58页门未受影响 |
| 台账/不可变边界 | gofmt、源码残留、双仓diff、immutable audit、strict/allow verifier | 最终复核 | 0 / 1（预期）/ 0 | PASS：生产源码PBKDF2零命中、格式/不可变边界绿；audit/ledger精确449，CLOSED=399、OPEN=50、UNRESOLVED=32，strict预期83 issues且allow通过 |

## 2026-08-24：STYLE-002 / STYLE-005 / MAP-002 / LEGACY-005 开发期合同收敛

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| canonical排序定向 | Go `TestCatalogSortUsesCanonicalFieldAndDirectionOnly/ParseCatalogSortRejectsUnknownValues`；Node catalog合同 | Go包1.052s；Node定向7/7约0.17s | 0 / 0 | PASS：五个旧别名双端拒绝/回退；canonical published与显式asc/desc有效；name缺order一致asc；SQL注入值继续拒绝 |
| 自动化/只读真实PG | `TestProjectAutomationGETIsReadOnlyAndEnableRequiresConfigureIntegration`与`TestAntiAbuseBotRuleWriteContractIsAlwaysReadOnlyIntegration`；完整临时generation163 Schema | 各约9.94/9.75s；组合包20.740s | 0 | PASS：GET/PUT/runs/admin overview零snake_case且身份/interval字段正确；旧readOnly请求400零行、canonical201且唯一行read_only=true；临时Schema自动Drop |
| 测试归位门 | `TestAdminDashboardIntegrationTestsStayInTheirDomains`及四个保留行为测试 | 普通httpapi/全仓门 | 0 | PASS：dashboard文件只含总览；sticker、favorite export、notification translation各由领域文件唯一拥有，原函数和断言未删除 |
| 后端发布门 | ordinary本机DB并移除integration变量；全仓Test/Vet/Build；tidy前后SHA-256 | httpapi41.523s；其余包通过；Vet/Build通过 | 0 / 0 / 0 / 0 | PASS：全仓测试、静态分析、构建全绿；tidy后go.mod/go.sum哈希分别`DFE265...ADCC9`/`3D4129...B652`且前后稳定，既有依赖工作树差异不误判为新增 |
| 前端发布门 | 全树`node --test`；ESLint；TypeScript；合法HTTPS正式Turbopack build | Tests270/270 5.860s；Build编译9.6s/Type14.7s | 0 / 0 / 0 / 0 | PASS：新增3项开发合同与既有267项全绿，lint/type零错误，58页生产构建成功 |
| Schema/API/隔离 | generation163；双端调用搜索、gofmt、临时数据库/客户端、双仓diff、immutable audit | 最终复核 | 0 | PASS：无DDL/reset/依赖新增；自动化DTO和排序按开发期breaking收紧、机器人只读安全边界不变；共享public不迁移 |
| 台账门 | strict/allow verifier与精确计数 | 最终复核 | 1（预期）/ 0 | PASS：audit/ledger仍精确449，CLOSED=403、OPEN=46、UNRESOLVED=32，strict预期79 issues且allow通过 |

## 2026-08-24：MAP-005 / MAP-006 / DEAD-004 / DEAD-007 / DEAD-008 Schema字典与类型完整性

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效Schema/source RED | 新generation164、auto-run四状态、recipe双来源、revision bigint FK和simple registry单一字面量合同 | 定向约4.2s | 1（预期） | PASS：旧generation163、failed、backfill/split、revision text与两份simple project字面量分别触发精确失败；DEAD008当前HEAD合同先验通过，证明已由后续热度重构消除 |
| 定向单元GREEN | database/httpapi五项合同 | 包约0.9/1.0s；墙钟10.2s | 0 | PASS：generation164、三类Schema约束、唯一评论者权威、六类型顺序/集合与源码单一事实全部通过 |
| 真实临时PG | `TestDevelopmentSchemaDictionariesRejectUnreachableAndInvalidValuesIntegration`与`TestBUG039AutomatedMaintenancePublishesAuditedRevisionAtomicallyIntegration`；完整generation164临时Schema | 用例10.94/11.28s；两包墙钟14.4s | 0 | PASS：failed/backfill/split为23514；import/editor成功；有效revision事件为bigint，无效FK为23503；mod/simple自动维护发布、修订关联和事务回滚保持 |
| 临时Schema清理回归 | `TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration`；固定清理namespace OID并检查relation/routine | 用例17.04s；包17.939s | 0 | PASS：100k/1m/10m投影计划仍通过；清理后当前临时namespace关系和routine均不存在，public generation前后不变；本次旧清理器遗留183 routines已在other_clients=0条件下精确删除，最终0/0 |
| 后端发布门 | ordinary本机DB并移除integration变量；全仓Test/Vet/Build；tidy前后SHA-256 | 最终Test约49.5s；Vet/Build约5.4s | 0 / 0 / 0 / 0 | PASS：httpapi37.735s；全仓、静态分析和构建通过；go.mod/go.sum哈希`DFE265...ADCC9`/`3D4129...B652`前后稳定 |
| Schema/隔离/不可变 | generation163→164；DDL/调用方残留、gofmt/diff；本机PG与immutable audit | 最终复核 | 0 | PASS：public仍155、other clients0、临时关系/routine0、测试数据库0；审计证据目录状态与diff均为零；当前reset文档同步164，未操作共享public或远端 |
| 台账门 | strict/allow verifier与精确计数 | 最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=408、OPEN=41；High135/Medium223/Low62/UNRESOLVED29；strict精确71 issues且allow通过 |

## 2026-08-24：STYLE-007 / STYLE-008 / DEAD-013 治理公开与内部边界

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | DEAD013 distinct admin/public query源码合同；治理领域拆分合同 | 定向复现 | 1（预期）/ 1（预期） | PASS：旧实现缺少管理员内部字段/独立query；拆分门在四个领域文件尚不存在时精确失败，没有把原共享SQL或仅改测试路径记作修复 |
| DEAD013真实PG | `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run '^TestDEAD013' -count=1`；完整generation164临时Schema | 包9.787s；墙钟12.48s | 0 | PASS：公开列表和详情均无internalNote/moderator/revoker/reason及私密字符串；管理员列表返回备注、执行者、解除者和解除原因；源码门确认路由仍由ban.view_internal保护 |
| 状态与模块定向 | 13个治理/站务Node文件，共29项；三态/五类unknown、五模块符号/旧文件/最长行合同 | 0.90s | 0 | PASS：29/29；公开与管理员加载都归一化状态；旧巨型文件只剩不存在断言；五新模块最长行分别113/109/111/125/130且跨域symbol为0 |
| 后端发布门 | ordinary本机DB并移除integration变量；全仓Test/Vet/Build；tidy前后SHA-256 | httpapi35.497s；全仓约40s；Vet/Build约5.1s；tidy0.69s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析、构建通过；go.mod/go.sum哈希`DFE265...ADCC9`/`3D4129...B652`前后稳定；gofmt完成 |
| 前端发布门 | 全树`pnpm test`、TypeScript、ESLint；合法HTTPS环境正式Turbopack build | 272/272 5.115s；Type5.36s；Lint25.65s；Build23.64s | 0 / 0 / 0 / 0 | PASS：全部治理旧契约与全树测试通过，类型和lint零错误；58页production build成功；临时Prettier未改变package/lock依赖 |
| Schema/API/隔离 | generation164保持；公共/管理员DTO复核；本机PG残留与immutable audit | 最终复核 | 0 | PASS：无DDL/reset/公共写；other clients0、测试数据库0、临时关系0、routine0；公开API字段不增，管理员原权限响应增加可追溯事实；审计证据目录状态/diff为零 |
| 台账门 | strict/allow verifier与精确计数 | 最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=411、OPEN=38；High135/Medium224/Low64/UNRESOLVED26；strict精确65 issues且allow通过 |

## 2026-08-24：DEAD-003 / DEAD-012 可执行状态与缓存单一权威

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | Go关注偏好修改闭环与表情版本删除源码合同；Node关注面板与缓存合同 | 定向复现 | 1（预期）/ 1（预期） | PASS：旧实现缺少PATCH Handler/API/UI且PUT响应硬编码true；表情API、Schema和事务仍暴露/维护未消费version，两个失败都精确命中待修事实 |
| 定向GREEN | Go httpapi/database源码与行为合同；Node关注分页/表情缓存合同8项 | Go包约1.1/0.92s；Node约0.16s | 0 / 0 | PASS：PATCH严格boolean、持久值RETURNING和重复PUT保留偏好；表情version/table/bump生产残留归零，缓存TTL 30秒、失败逐出及管理员成功写后invalidate继续受门禁保护 |
| 真实generation165 PG | `TestDEAD003AndDEAD012ActivePreferenceAndCacheContractsIntegration`；完整临时Schema与生产鉴权/Handler | 用例11.029s；墙钟20.03s | 0 | PASS：版本表不存在且公开表情响应无version；关注false、重复PUT仍false、三类坏body零副作用、他人404、项目隐藏后所有者可改true，GET与数据库真值一致 |
| 后端发布门 | ordinary本机DB并移除integration变量；全仓Test/Vet/Build；tidy前后SHA-256 | httpapi35.729s；全仓约39.2s；Vet7.76s；Build8.05s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析和构建通过；go.mod/go.sum哈希`DFE265...ADCC9`/`3D4129...B652`前后稳定；generation165当前代次引用与gofmt通过 |
| 前端发布门 | 全树`pnpm test`、TypeScript、ESLint；合法HTTPS环境正式Turbopack build | 274/274 6.280s；墙钟7.331s；Build28.50s | 0 / 0 / 0 / 0 | PASS：关注受控开关、持久响应回写及既有缓存/管理 mutation 合同全绿；类型和lint零错误，58页production build成功，依赖/锁文件不变 |
| Schema/API/隔离 | generation164→165；生产残留、双仓diff、本机PG与immutable audit | 最终复核 | 0 | PASS：共享public仍155、other clients0、测试数据库0、临时关系0、routine0；未执行共享/远端ALTER或reset；版本表/字段/bump只剩历史说明或拒绝性测试字面量 |
| 台账门 | strict/allow verifier与精确计数 | 最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=413、OPEN=36；High135/Medium225/Low64/UNRESOLVED25；strict精确62 issues且allow通过 |

## 2026-08-24：REUSE-003 / MAP-003 权威目录与数值字典复用

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED | REUSE003期望`projectFileFilters(items, order)`；MAP003期望definitions/lookup及四消费方源码合同 | 定向复现 | 1（预期）/ 1（预期） | PASS：旧版本函数因无权威order参数编译失败；activity缺统一API，Schema/清理/progression源码门同时精确报告三套事实 |
| 定向GREEN | activity/database/httpapi/progression四包，版本配置顺序、11/27字典、防变异、源码归一与任务配置 | 四包0.11/0.13/0.18/0.13s；httpapi补跑1.056s | 0 | PASS：非数值配置顺序优先、未知稳定尾序；canonical查找有效而旧别名拒绝；Schema/清理/任务全部消费activitycatalog，旧比较器/map/switch归零 |
| 真实generation165 PG | `TestREUSE003AndMAP003PersistentCatalogAuthoritiesIntegration`；完整临时Schema与持久设置 | 用例10.09s；包11.209s | 0 | PASS：activity_actions 11项和activity_object_types 27项的ID/code/name逐项等于注册表；持久snapshot→1.20.6→1.21.1顺序与unknown-a/z尾序精确返回 |
| 相邻真实状态机 | 活动清理审计恢复；progression caller transaction与TEST031完整奖励/角色矩阵 | 11.477s；17.296s | 0 / 0 | PASS：分批删除/运行审计保持；调用者回滚、任务累加、并发领取、奖励、角色来源、版本刷新及失败重放均未因字典归一回归 |
| 后端发布门 | ordinary本机DB并移除integration变量；全仓Test/Vet/Build；tidy前后SHA-256 | httpapi38.253s；Vet11.25s；Build12.10s；tidy0.90s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析和构建通过；go.mod/go.sum哈希`DFE265...ADCC9`/`3D4129...B652`前后稳定，gofmt完成 |
| 前端与协议相邻门 | 无前端文件/DTO变化；版本选择及项目下载分页Node测试 | 8/8 0.156s | 0 | PASS：配置顺序、disabled选择、loader来源、独立provider cursor及安全下载状态保持；紧邻上一簇274项/Type/Lint/Build的工作树未变化 |
| Schema/计划/隔离 | generation165保持；SQL形状和seed逐项；本机PG、diff及immutable audit | 最终复核 | 0 | PASS：无新增查询形状/索引计划或DDL；public155、other clients0、测试数据库0、临时关系0、routine0；双仓diff check与不可变边界通过 |
| 台账门 | strict/allow verifier与精确计数 | 最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=415、OPEN=34；High135/Medium225/Low65/UNRESOLVED24；strict精确59 issues且allow通过 |

## 2026-08-24：TEST-046 / TEST-047 管理读模型行为与规模门

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 覆盖复核与新增门 | 审计缺口逐项对应当前BUG102-107/ARCH029/PERF058-059测试；权限源码与未解析双数据库故障/前端双错误 | 定向复核 | 0 | PASS：站务语言/发布/CAS/错误/分页及未解析来源/类型/搜索/删除恢复已有真实门；新增8个精确permission wrapper、两个500无部分数据和两个独立error alert |
| 站务真实PG | locale纯边界、BUG102/103/104、Changelog结构故障与100万行公开/管理页；串行执行 | 包38.023s | 0 | PASS：11种locale、坏输入零写、逐语言发布、双CAS冲突/重试、三读取500；1M fixture 8.56s，公开/管理深页各一条索引SQL |
| 未解析引用真实PG | BUG105七来源、BUG106十四类型、新数据库错误、100万行计划及并发删除；串行执行 | 包41.596s | 0 | PASS：创建/改名/重绑投影、动态/隐藏kind、两类故障500；1M fixture21.10s，深页/前缀索引、有界宽搜索与空页恢复通过 |
| 无效并行尝试与恢复 | 两个完整Schema组并发；53200锁容量；无客户端后精确四个namespace routine清理；串行重跑 | 失败符合基础设施限制；恢复后0 | 1（无效）/ 0 | PASS：不把并发安装失败计为产品失败；只删除`pg_temp_14/37/44/71`各183 routines，未改配置/public；最终relations/routines均0，串行结果为权威GREEN |
| 前端定向/全量 | 7个站务/未解析测试文件；随后全树Node、TypeScript、ESLint | 定向14/14 0.317s；全量275/275 7.842s；Type5.51s | 0 / 0 / 0 | PASS：locale草稿、逐翻译、CAS、游标、请求代次、动态类型、来源路由、空页恢复和双错误态全绿；lint零错误 |
| 后端发布门 | ordinary本机DB且移除integration变量；全仓Test/Vet/Build | httpapi39.103s；Vet9.62s；Build8.62s | 0 / 0 / 0 | PASS：新增普通/集成测试编译及全树全部包通过；无模块、依赖或生产实现变化，紧邻tidy哈希稳定继续适用 |
| Schema/API/隔离 | generation165保持；query plan、双仓diff、public/客户端/临时对象、immutable audit | 最终复核 | 0 | PASS：无DDL/API/生产前端变化；public155、other clients0、测试数据库0、临时关系0、routine0；diff check及不可变证据目录通过 |
| 台账门 | strict/allow verifier与精确计数 | 最终复核 | 1（预期）/ 0 | PASS：audit/ledger精确449，CLOSED=417、OPEN=32；High135/Medium227/Low65/UNRESOLVED22；strict精确55 issues且allow通过 |

## 2026-08-24：OPS-004 / TEST-018 项目更新通知重试与Worker行为门

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED与重试定向 | OPS004缺少max常量/返回错误先编译失败；最小真实PG永久失败、terminal重放和拒绝retry持久化 | RED预期失败；GREEN包1.186s | 1（预期）/ 0 | PASS：修复前精确缺原子失败计数和错误返回；修复后attempt1..8逐次持久，第8次failed，第9次不变；CHECK拒绝UPDATE时错误可见且行保持pending/0 |
| TEST018随机PG矩阵 | `TestTEST018ProjectUpdateWorkerBatchesFiltersRetriesAndRecoversIntegration`；独立generation165数据库、8连接 | 用例5.88s；包6.63s | 0 | PASS：201有效收件人由双worker完成200+1游标且零重复；101 zh/100 en；actor/开关/偏好/unfollow/suspended全排除；重复、瞬时故障恢复、6分钟stale接管和隐藏目标均精确通过 |
| 项目通知串行广域 | BUG036、BUG037、OPS004、事件合并、原子计数/1m索引计划和TEST018；`-parallel=1` | 29.784s | 0 | PASS：隐藏关系取消/本地化fallback/事件合并保持；1000 event行使用`idx_notifications_project_update_event` Index Only Scan，Execution 0.413ms；完整Schema用例未并行 |
| 后端发布门 | ordinary本机DB并移除integration变量；全仓`go test ./... -count=1`、Vet、Build、tidy前后SHA-256 | httpapi41.170s；Vet4.99s；Build4.91s；tidy1.0s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析和构建通过；go.mod/go.sum前后不变，无新增依赖；随机TEST018数据库由cleanup强制关闭并删除 |
| 前端/Schema/API | 本簇双仓diff；generation165与生产路由/DTO检查；复用紧邻前端275/Type/Lint全门 | 最终复核 | 0 | PASS：无前端运行/测试文件变化、无DDL/API/状态码变化；通知内部失败可观测性增强，健康调用方无需迁移，正式前端生产树未变化 |
| 隔离/不可变/台账 | 本机PG client/testdb/temp对象；immutable audit status/diff；strict/allow verifier | 最终复核 | 0 / 1（预期）/ 0 | PASS：public155、other clients0、测试数据库0、临时关系/routine0，审计证据status/diff均零；audit/ledger精确449，CLOSED419/OPEN30，High136/Medium228/Low65/UNRESOLVED20，strict预期51 issues且allow通过 |

## 2026-08-24：LEGACY-010 / TEST-025 日志命令边界与行为矩阵

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 双端有效RED | Go要求PUT无cleanup、显式POST及log.write；Node要求两个动作/endpoint/文案 | Go9.57s；Node0.56s | 1（预期）/ 1（预期） | PASS：旧PUT中的`cleanupLogs`和前端缺POST/“保存立即删除”文案分别被精确命中，没有把既有自动Worker误记为完成旧路径迁移 |
| 定向命令GREEN | LEGACY010源码门、前端日志分页/命令边界 | Go包1.046s；Node2/2 0.15s | 0 / 0 | PASS：PUT函数零cleanup；POST `/admin/logs/cleanup`精确log.write；第一方PUT/POST分离且两语言不再声称保存删除 |
| 日志真实PG/广域 | AccessLog、BUG051-053、SEC020、日志cursor/cleanup与1.3M规模，单包串行 | 5.910s | 0 | PASS：64锁表请求约0.52ms返回/4批flush；SSE/Writer能力完整；自动1005跨批/禁用/租约；保存零DELETE，手动部分失败结构化；坏配置GET/POST 500零删除；1.3M装载4.27s且精确删1000 |
| 结构运行日志 | `go test ./internal/runtimelog -count=1 -v` | 包0.078s | 0 | PASS：环形有界/partial write、slog INFO/WARN属性分组及legacy INFO/ERROR均进入同一终端和后台Store，级别不靠关键词猜测 |
| 后端发布门 | ordinary本机DB；全仓Test、Vet、Build、tidy前后SHA-256 | httpapi45.580s；Vet6.23s；Build6.15s；tidy1.0s | 0 / 0 / 0 / 0 | PASS：全部包/静态分析/构建通过，模块文件前后不变；最后只增强BUG053断言并定向真实PG复跑通过 |
| 前端发布门 | 全树Node、TypeScript、ESLint及合法HTTPS正式Turbopack build | 276/276 6.56s；Type/Lint通过；Build25.53s | 0 / 0 / 0 / 0 | PASS：新增命令合同和既有275项全绿；58页生成、类型及lint无错误，依赖/锁文件不变 |
| Schema/API/隔离/台账 | generation165；双仓diff、DB残留、immutable audit、strict/allow verifier | 最终复核 | 0 / 1（预期）/ 0 | PASS：无DDL，PUT/POST开发期同批迁移；public155、clients/testdb/temp关系/routine均0，审计目录status/diff为0，目标文件diff-check通过；audit/ledger449，CLOSED421/OPEN28，High136/Medium230/Low65/UNRESOLVED18，strict预期47 issues且allow通过 |

## 2026-08-24：OPS-010 / TEST-028 / TEST-029 热度刷新状态机与组合行为门

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 有效RED与Schema合同 | 新源码门要求max attempts、failed状态、错误返回；Schema门要求status CHECK及ready/stale部分索引 | RED预期失败；GREEN定向通过 | 1（预期）/ 0 | PASS：旧实现精确缺终态且吞retry UPDATE错误；新实现两队列共享8次预算、processing CAS和新事实恢复 |
| OPS010随机PG状态机 | `TestOPS010PopularityQueuesTerminateRecoverAndClaimOnceIntegration`；独立generation166数据库、8连接 | 用例5.51s；包5.737s | 0 | PASS：30个内容任务双worker恰好唯一领取；stale attempt3→4、attempt8→failed；内容/评论毒函数1..8终止、重入0次并恢复删除；CHECK拒绝retry UPDATE时错误可见 |
| 隐私/pageKey/搜索状态机 | Metric/SEC022定向；TEST030注册表和真实索引滚动状态机 | httpapi同包5.737s；searchindex5.320s | 0 / 0 | PASS：公开统计零访客历史、pageKey闭集；搜索拒绝降代、毒文档恢复、持久化错误返回，注册表与数据库七类型精确一致 |
| 评分/开发者/UTC广域 | TEST040评分Handler；双developer排除；UTC连接；全局评分100k/1M/10M；热度刷新 | 评分5.89s；开发者10.25s；全局32.87s | 0 | PASS：权限/可见性/增改删/并发/故障回滚/逆向trigger均通过；10M排除100k开发者重建6.9399s，Kiritimati URL仍固定UTC |
| 衰减、目录与搜索规模 | 10M热度衰减到期入队；管理目录100k/1M；搜索keyset100k/1M | 衰减用例6.38s；目录91.75s；搜索2.00s | 0 | PASS：10M中100个到期行8.0255ms；1M目录首/深/typed约2.87/0.51/0.50ms、搜索1.6096s；1M搜索深页623.96ms |
| generation166完整空库 | `TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration`及完整Migrate随机数据库 | 17.229s | 0 | PASS：完整临时Schema成功；PERF034 100k/1M/10M刷新计划4.5079/9.2243/9.7021ms，退出后namespace对象清理 |
| 后端发布门 | ordinary本机DB且移除integration变量；全仓Test/Vet/Build、tidy前后SHA-256 | httpapi48.121s；Vet5.17s；Build5.64s；tidy1.0s | 0 / 0 / 0 / 0 | PASS：全部包、静态分析和构建通过；go.mod/go.sum哈希前后分别`DFE265…ADCC9`/`3D412…B652`，无依赖漂移 |
| Schema/API/隔离/台账 | generation166；本机PG/immutable audit；strict/allow verifier | 最终复核 | 0 / 1（预期）/ 0 | PASS：公开API/DTO和前端零变化；共享public仍155且无业务写；审计目录status/diff为0；audit/ledger449，CLOSED424/OPEN25，High137/Medium230/Low65/UNRESOLVED17，strict预期43 issues且allow通过 |

## 2026-08-24：OPS-002 / TEST-002 / TEST-003 持续交付、覆盖率与Race门

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 覆盖率实际基线 | ordinary DB；`go test ./... -count=1 -coverprofile`及`go tool cover -func` | 全仓约69s | 0 | PASS：总语句27.5%，审计时18.4%；httpapi26.1%、queue61.6%、security80.8%、runtimelog89.7%，CI硬下限27.0且归档函数报告 |
| CI/CD源码合同 | 双仓主CI、标签release、Dockerfile/.dockerignore；Go/Node合同测试 | Go1.767s；Node5/5 0.157s | 0 / 0 | PASS：普通/PG/Race/coverage/双端build/容器job均存在；Action固定SHA；GHCR tag+sha；前端origin失败关闭；镜像非root |
| Workflow语法 | actionlint v1.7.7显式读取双仓各3个workflow | 双仓1.8s | 0 / 0 | PASS：CI、dependency audit和release全部零语法/表达式/事件错误；首次PowerShell glob未展开的无效命令不作为内容结果 |
| Windows Race工具链探针 | 官方SHA匹配Zig0.16；随后winget哈希验证WinLibs GCC16.1；`go test -race ./internal/config` | 2.345s | 0 | PASS：Zig链接/TSan地址空间失败如实作废；标准MinGW探针真正进入并完成测试 |
| 全仓Race | MinGW-w64 GCC16.1、CGO=1、移除integration变量；`go test -race ./... -count=1` | httpapi61.943s | 0 | PASS：全部有测试包实际执行，无DATA RACE、构建失败或测试失败；queue8.516s、tools/remediation7.439s |
| 前端完整发布门 | Node全树、TypeScript、ESLint、standalone Turbopack Build | 277/277 4.51s；Build含Type约19s | 0 / 0 / 0 / 0 | PASS：新增交付合同与既有276项全绿，58页生成；`.next/standalone`产物成功，依赖/锁文件不变 |
| 后端相邻发布门 | 本簇前一批全仓Test/Vet/Build/tidy及新增CI合同 | httpapi48.121s；合同1.767s | 0 | PASS：生产Go未因交付文件变化；模块哈希稳定；新增Go测试格式与全仓Race均绿 |
| Schema/API/隔离/台账 | generation166保持；双仓diff、DB/immutable、strict/allow | 最终复核 | 0 / 1（预期）/ 0 | PASS：无DDL/业务API；public155、client/testdb/temp对象0；immutable audit零diff；audit/ledger449，CLOSED427/OPEN22，High137/Medium230/Low65/UNRESOLVED17，strict预期40 issues且allow通过 |

## 2026-09-27：OPS-003 / TEST-008 收尾与用户暂停交接

本条记录本轮真实重跑，不把2026-08-24的历史结果重新标成今天执行。原始输出随源码包放在`handoff/validation/`；最终复验日志使用`closing-`前缀，早期失败日志不删除。仅收尾当前两项，不展开其余20项。

| 验证项 | 命令 / 证据 | 时间 | 退出码 | 结果 |
| --- | --- | ---: | ---: | --- |
| 派生物原子生命周期 | OPS003真实PG：预登记/激活、失败/stale墓碑、Outbox CHECK拒绝后整笔回滚、orphan恢复与幂等 | 定向矩阵 | 0 | PASS：planned/pending→active/active；失败准确一删除任务；补偿失败保持原状后可恢复 |
| 完整任务与提交故障 | TEST008随机generation167数据库；HTTP→Outbox dispatcher本地分发→Worker→OSS；实际删除Worker | 首次完整正确夹具12.418s；最终见closing日志 | 0 | PASS：成功3PUT/3active；另一任务3PUT后最终化CHECK故障，3abandoned/3deleted/3清理任务/staging0；累计2GET/6PUT/5DELETE，实际写入endpoint与CDN不同仍正确删除 |
| 取消并发边界 | 在途PUT持artifact共享锁，另一事务以100ms lock_timeout尝试补偿，释放后HTTP取消 | 真实PG | 0 | PASS：在途时55P03而非提前删除；释放后200和deleted/abandoned/1Outbox；取消后守卫拒绝，不调用PUT |
| 重试与来源相邻门 | `TestModExportRetryAndRecoveryCreateAtomicOutboxAttempts`、`TestCatalogImportSameSHAKeepsDistinctUserSources` | 同定向组 | 0 | PASS：重试/stale原子再入队、入队故障回滚、两用户同SHA来源隔离不回归 |
| 完整Schema | `TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration` | 包22.670s；墙钟26.611s | 0 | PASS：167完整临时Schema，100k/1M/10M相关刷新计划7.7053/10.6593/14.1015ms；不reset共享public |
| 后端全仓门 | `go test ./... -count=1`、`go vet ./...`、`go build ./...`、`go mod tidy -diff` | 首轮墙钟95.326/12.665/9.937/0.691s | 0 / 0 / 0 / 0 | PASS；最后endpoint修正后另有closing全仓Test/Vet/Build复验，具体时间以JSON为准 |
| Race | MinGW/GCC、CGO=1：`go test -race ./... -count=1`；另用本机PG跑OPS003/TEST008 | 全仓151.165s；定向16.556s；最后closing再跑 | 0 / 0 | PASS：全仓无DATA RACE；完整真实数据库并发与故障用例也通过Race，不用普通测试代替Race |
| 前端全仓门 | `pnpm test/typecheck/lint/build`；正式构建使用三项example.test HTTPS配置 | 墙钟7.460/3.833/45.293/43.507s | 0 / 0 / 0 / 0 | PASS：277/277、无skip；类型/ESLint/58页生产standalone构建通过；本轮无前端实现变更 |
| 未通过尝试与修正 | PG未启动；故障fixture使用假importer version；CRLF检查参数不完整 | 原始失败保留 | 1 / 1 / 2 | 不计PASS：启动原隔离PG后重跑；fixture改合法importer+独立目标版本并确认23514来自最终激活；Git声明cr-at-eol后双仓diff0，无格式洗稿；更早远端CREATEDB权限失败亦不计PASS |
| 台账和原审计 | strict/allow；双份审计17文件SHA-256 | 38 issues / inventory PASS | 1（预期）/ 0 | 精确449、CLOSED429/OPEN20；137H/230M/65L/17UNRESOLVED不伪调；原审计17文件哈希一致；阶段交接并暂停，不宣告成熟度A |
| 最终源码复验 | closing普通导入、真实PG Race、全仓普通Test、Vet、Build、双仓diff | 墙钟10.507/30.001/66.261/4.795/7.835s；diff0.682/0.468s | 全部0 | PASS：最后实际endpoint修正后的源码通过；两端diff采用cr-at-eol识别Windows原有换行；gofmt待格式化文件0 |
| 最终数据库隔离 | 本机127.0.0.1:55432只读核对；原始输出database-isolation.log | 打包前 | 0 | PASS：共享public generation155、其他客户端0、测试数据库0、临时关系0、临时routine0；未升级/重置共享或远端业务库 |
