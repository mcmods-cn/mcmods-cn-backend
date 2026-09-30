# 2026-09-30 前后端源码与修复进度交接

按用户要求，只收尾已经开始的 OPS-020，其余未修复问题暂不继续；另按用户明确要求将当前代码提交到指定 GitHub 仓库的现有分支。此文为阶段交接，不是 449 项最终验收或成熟度 A 结论。

## 当前进度

449 项全部保留：430 CLOSED、19 OPEN、0 NOT_APPLICABLE、0 BLOCKED_EXTERNAL，按台账约 95.77%。本轮开始为 429 CLOSED / 20 OPEN，只新增关闭 OPS-020。

`go run ./tools/remediation/verify_findings -allow-open` 已通过，精确核对完整 ID 集合；默认严格校验实际非零，未完成问题继续保留。37 项校验问题由 19 个 OPEN、17 个 UNRESOLVED 严重度和 1 个严重度汇总差异组成；原始输出随交付包保留。它们不是新增 Finding。

原审计严重度汇总为 176 High / 214 Medium / 59 Low；当前逐项台账为 137 / 230 / 65，另有 17 个 UNRESOLVED。该权威证据核对尚未完成，本轮不为满足汇总而调整严重度。所有原始审计报告保留不改。

## 本轮最后完成的修复

- OPS-020：封禁到期优先执行；原单一 30 秒公共截止时间改为每类独立 30 秒，各自继承父 Context 取消。
- 每类每轮最多 4 批，每批最多 1000 行；孤儿导入恢复最多 4 个尝试。积压留到下一次 10 分钟维护轮，没有无限清空或平行启动新 goroutine。
- 真实 generation167 随机 PostgreSQL 数据库：蓝图表排他锁先复现旧实现饿死全部后续类别，再证明新实现正确过期封禁、精确删除治理来源且保留手工/未来授权、保留 Session/auth_version、清理日志/草稿/Join/Presence。
- 4005 过期草稿一轮仅删除 4000，剩余 5 条；双 Worker 与后续轮可收敛更多积压；取消父 Context 后不再进行删除。
- 既有蓝图上传、临时封禁、贴图删除、导入派生物/取消/补偿故障回归通过。旧蓝图源码测试同步检查新调度入口，真实行为断言未删除。

无 Schema、索引、公开 API、DTO 或前端业务代码变化；权威开发 generation167 保持。没有重置共享数据库或连接生产服务。

## 本轮实际验证及边界

- 真实 PG 定向矩阵及 OPS-020 Race 通过；集成日志 `closing-maintenance-integration.log` / `closing-maintenance-race.log`。
- 最终后端全仓 Test、Race、Vet、Build、`go mod tidy -diff` 通过；最终结果以 `verified-backend-results.json` 和 `verified-*` 日志为准。
- 前端 277/277 测试、TypeScript、ESLint 和 58 页生产构建通过；正式构建使用 example.test 的合法 HTTPS origin，不连接线上 API。
- 完整随机数据库由真实 Migrate 安装 generation167，退出后删除随机测试数据库；已有临时表夹具不冒充完整 Schema 测试。
- 对象存储仍为本机 HTTP 替身，测试代表本机故障/取消行为，不冒充生产多节点、供应商断电或容量测试。本轮未重新执行 govulncheck、依赖扫描、完整浏览器 E2E 或容器运行，不将历史结果标成今天通过。

失败和中断保留：新增夹具初期缺少角色/封禁理由种子、误用列名，修正后才得到有效 RED；旧实现 RED 明确显示锁等待后所有后续状态未转换。全仓首次失败为源码测试仍要求旧直接调用写法，已同步新入口后复验。随后执行衔接中本机 PG 服务停止，`final-*` 普通/Race 因连接拒绝失败，不计 PASS；检查进程和 pg_ctl 后重新启动原有本机测试实例，确认 readiness 才执行 `verified-*`。`closing-*` 后端全仓未产生完整汇总的执行不冒充最终退出码；最终以有明确退出码的 JSON 为准。

## 暂不继续的 19 项

| Finding | 尚待完成的审计项 |
| --- | --- |
| TEST-007 | EXPLAIN 性能判定 |
| TEST-013 | Mod 资料破坏性生命周期和真实审核覆盖 |
| TEST-014 | 简单项目目录完整行为验证 |
| TEST-015 | 服务器探测/目录对抗性与端到端边界 |
| TEST-016 | 项目文件发布和下载安全生命周期 |
| TEST-017 | 更新日志权限、审核、同步和大列表 |
| TEST-022 | 私聊与 Presence 核心行为 |
| TEST-023 | 实时 Hub 和消息中心前端行为 |
| TEST-024 | 邮件、设置和站点品牌边界 |
| TEST-027 | Schema 和 NATS 配置真实边界 |
| TEST-036 | 蓝图安全、保真和任务生命周期 |
| TEST-037 | OSS 关键故障及重放 |
| TEST-038 | 评论子路由权限与故障生命周期 |
| TEST-039 | 皮肤审核、隐私、共享存储和数据库链路 |
| TEST-042 | 社区/悬赏数据库、权限和并发 |
| TEST-048 | 举报处置和封禁完整行为 |
| OPS-007 | 实时 NATS 订阅启动失败后恢复 |
| OPS-008 | 站点 Logo 写入实例本地 public 目录 |
| STYLE-003 | 局部错误和通知语言/返回风格 |

## GitHub 和完整源码包

仓库为 `mcmods-cn/mcmods-cn-backend`、`mcmods-cn/mcmods-cn-frontend`，分支均为 `codex/unified-catalogs-user-systems`。本轮开始时后端 `0e6fdb2fddd5d6ef30310a251182a366d82549f3`，前端 `a428c15d687637a5761b7572dbf62098e0c0c839`；后端新增 OPS-020 提交，前端没有新改动，不制造空提交。最终提交 SHA、工作树是否干净和远程核对记录以交付包 `handoff/SNAPSHOT.json` / `handoff/README.md` 为准。

ZIP 包含 backend / frontend 全部当前源码、测试、依赖锁文件、示例配置、Docker/CI、不可变审计、全部修复文档，以及 handoff 验证原始输出、Git 元数据、逐文件 SHA-256 清单和打包/验证脚本。不含 `.git` 历史、node_modules、.next、缓存、私有 `.env`、密钥和数据库数据；`.env.example` 保留，实际运行凭据需自行配置。

恢复剩余修复前先核对双仓工作树、读取完整台账和不可变审计；不要自动切换默认分支、强推、reset 或清空已有业务库。数据库安装和开发重置安全条件见仓库根目录 `DEVELOPMENT_SCHEMA_RESET.md`。
