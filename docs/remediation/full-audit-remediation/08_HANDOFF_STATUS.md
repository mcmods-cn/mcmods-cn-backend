# 2026-09-27 源码与修复进度交接

状态：按用户要求，在 OPS-003 / TEST-008 收尾后暂停其余修复。没有提交、推送或部署；双仓全部未提交修改保留。此文是阶段交接，不是最终成熟度评估。

## 修复进度

449 个唯一 Finding 均保留：429 CLOSED、20 OPEN，按台账计约 95.55%。本轮收尾前为 427 CLOSED / 22 OPEN。

`go run ./tools/remediation/verify_findings -allow-open` 通过；默认严格校验仍以非零退出并报告 38 项问题，这是未完成 Goal 的真实状态，不应跳过或改成成功。38 项校验问题不是额外的 Finding：它们包括 20 个未关闭状态、17 个严重度待核对以及 1 个严重度汇总差异。

原审计汇总 High/Medium/Low 为 176/214/59。当前逐项台账汇总为 137/230/65，另有 17 项 UNRESOLVED；严重度权威证据的最终统一尚未完成。本次没有为了凑总数修改严重度，也没有宣称 449/449 完成或达到成熟度 A。

## 最后完成的范围

- OPS-003：派生 PNG/语言包在 PUT 前登记 pending 文件和 job/run token 对象清单；最终事务统一激活；失败、取消、重试、停滞接管和孤儿恢复将文件墓碑与删除 Outbox 原子提交。
- 上传期间持有派生物共享行锁，补偿等待 PUT 返回；已取消尝试不能再开始 PUT。每次守卫上传有 30 秒截止时间。最终化和补偿按 job → artifact → file 的一致顺序加锁。
- 派生文件记录实际 OSS 写入 endpoint，而不是显示/CDN endpoint，确保删除任务有正确的物理目标。
- TEST-008：完整 generation 167 随机隔离数据库；HTTP 创建 → PostgreSQL Outbox 实际 dispatcher → 有界本地任务分发 → Worker claim → 测试 OSS GET/PUT → 派生物激活。另覆盖取消与在途上传竞争、取消后拒绝上传、全部 PUT 完成后的最终提交故障、staging 清理以及删除 Worker 对测试 OSS 的 DELETE。
- 组合回归包括 BUG-019 重试/停滞恢复 Outbox 原子性、SEC-009 两用户同 SHA 来源隔离、OPS-003 补偿 Outbox 故障回滚和孤儿恢复。

测试边界：对象存储使用本机 HTTP 替身；本轮 dispatcher 走已有的 PostgreSQL 有界本地分发路径，不冒充公网 OSS 或多节点 JetStream 演练。源文件上传记录由测试夹具建立，未将完整浏览器直传/上传完成协议重新跑成端到端。断电导致远端 PUT 结果不明等供应商级故障没有在本轮模拟。单一维护截止时间的 OPS-020 仍 OPEN。

## 数据库与 API

Schema generation 166 → 167，新增 `catalog_import_job_artifacts` 及状态约束、索引、RESTRICT 外键。公开 DTO/接口不变；半成品保持 pending，成功提交后才 active。按项目现行开发期规则使用空库完整安装，未执行生产或共享业务库升级/重置。不要将 `cmd/db-reset` 用于已有重要数据；安装前阅读 `DEVELOPMENT_SCHEMA_RESET.md`。

## 验证证据

原始逐阶段日志在 `04_VALIDATION_LOG.md`，本轮实际命令输出在交付包 `handoff/validation/`：

- 后端全仓普通测试、Vet、Build、`go mod tidy -diff` 通过。
- 全仓 `go test -race ./... -count=1` 通过；当前导入集成用例另有真实 PostgreSQL Race 回归。
- 完整 generation 167 会话临时 Schema 安装通过，测试正常清理隔离对象。
- 前端 277/277 测试、TypeScript、ESLint、58 页生产构建通过；使用 example.test 的合法 HTTPS 构建配置，未连接实际线上 API。
- 交接用普通集成最终结果以 `closing-import-integration.log` 为准；Windows CRLF 的 diff 检查以 `closing-*-diff-check.log` 为准。
- 原审计目录与外层审计工作区的 17 个文件逐一 SHA-256 一致，原报告未改。

保留失败记录：第一次恢复验证时本机 PostgreSQL 未运行，连接拒绝，不计 PASS；随后启动原有本机隔离测试实例。新增最终化故障夹具最初用了不合法 importer version，误进入 ZIP 解析路径，已经改为合法 importer version + 独立目标版本，最终测试确实在所有 PUT 后命中故障。首次 diff 命令禁用换行归一且未声明 CR-at-EOL，产生 CRLF 误报；补上 Git 的 CR-at-EOL 规则后复验，不改动用户文件来消除误报。更早 ordinary 测试未指定本机 DATABASE_URL 时的远端 CREATEDB 权限失败也不计为通过；本次全部数据库命令显式绑定 127.0.0.1:55432。

## 暂停的 20 项

以下标题沿用不可变审计，OPEN 不表示同模块其他修复未做，而表示该 Finding 尚未逐项完成验收。

| Finding | 待完成审计项 |
| --- | --- |
| TEST-007 | EXPLAIN 测试没有性能判定 |
| TEST-013 | Mod 资料破坏性生命周期和真实审核覆盖 |
| TEST-014 | 简单项目目录集成测试仅验证 SQL 可执行 |
| TEST-015 | 服务器探测与目录对抗性和端到端边界 |
| TEST-016 | 项目文件发布与下载安全生命周期 |
| TEST-017 | 更新日志权限、审核、自动同步和大列表 |
| TEST-022 | 私聊与 Presence 核心行为测试 |
| TEST-023 | 实时 Hub 和消息中心前端测试 |
| TEST-024 | 邮件、设置和站点品牌行为边界 |
| TEST-027 | Schema 和 NATS 配置真实边界 |
| TEST-036 | 蓝图安全、保真和任务生命周期 |
| TEST-037 | OSS 关键故障和重放边界 |
| TEST-038 | 评论子路由权限和故障生命周期 |
| TEST-039 | 皮肤审核、隐私、共享存储和数据库链路 |
| TEST-042 | 社区内容与悬赏数据库、权限和并发 |
| TEST-048 | 举报处置和封禁行为测试 |
| OPS-007 | 实时 NATS 订阅启动失败后不恢复 |
| OPS-008 | 站点 Logo 写入 Next 实例本地 public 目录 |
| OPS-020 | 维护 Worker 单一截止时间可能饿死后续状态转换 |
| STYLE-003 | 局部错误和通知文案语言/返回风格不一致 |

## 源码快照与后续恢复

后端 HEAD：`bd50afd0ef92192c40c5152ea56e27a3fdbb657f`；前端 HEAD：`4f82c9075636cc4f1f6d3eb07de37a3fffd1de58`；分支均为 `codex/unified-catalogs-user-systems`。修复位于工作树，不在 HEAD 提交中，不能仅用 `git archive HEAD` 获取修复成果。

交付包包含 backend / frontend 完整现有源码、测试、依赖锁文件、示例配置、Docker/CI 文件、不可变审计和修复文档，以及 `handoff/SNAPSHOT.json` 工作树元数据和 `handoff/FILE_MANIFEST.json` 逐文件哈希。不包含 Git 历史、node_modules、.next、缓存、私有 .env、私钥或数据库数据。保留 `.env.example`；实际凭据需自行配置。

只有用户明确要求恢复后再推进剩余问题；恢复时先核对双仓工作树和此快照差异，再读取台账、对应原审计证据及验证日志。不要自动重置仓库或数据库。
