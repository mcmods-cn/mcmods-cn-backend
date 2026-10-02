# 数据库与文档审查：2026-10-02

代码基线：`95694ccff038a28a3e6126b05e3e7c5037bc7854`。本报告独立于 `full-project-audit` 和既有整改记录；历史报告及其 CLOSED 状态保持原样。本轮逐文件进度与最终内容指纹由同目录交付的审查台账记录，不能把历史覆盖率继承为本轮覆盖率。

## 已确认并修复

| ID | 级别 | 根因与影响 | 修改与验证 |
| --- | --- | --- | --- |
| OCT02-DB-001 | P2 | 关于本站八种语言的种子使用普通 SQL 字符串保存 `\n`，在 PostgreSQL 默认 `standard_conforming_strings=on` 下成为字面字符，标题与段落不能正确分隔。 | 使用 PostgreSQL `E` 字符串。仅精确匹配旧默认正文、原默认标题、revision=1、无编辑者且仍已发布的旧种子会被修正并递增版本；人工修订、不同标题和草稿保持不变。真实 PostgreSQL RED/GREEN、八种语言、重复启动和四种人工保护条件通过。 |
| OCT02-DB-002 | P1 | 每次启动 `SeedRBAC` 都以默认值覆盖已有 `license_policies.redistribution_allowed/notes` 和 `ban_reasons.translations/sort_order`；维护者的禁止再分发决定可能被重启恢复成允许。 | 对已有策略和运营理由不做冲突更新，仍补充缺失默认项。真实 PostgreSQL RED/GREEN 证明 MIT 禁止值、说明、理由排序和现有站务正文保持；缺失政策仍初始化。 |
| OCT02-DB-003 | P1 | 已存在的默认角色仍重新插入所有缺失默认 grant；后台撤销的 `content.translate` 权限会在重启重新授予。角色创建与默认权限也未在同一事务安装。 | 默认角色与初始 grant 同事务安装，既有角色不恢复允许权限。内建 banned 通配拒绝继续保持 fail-closed。真实 RED/GREEN、并发初始化、故障回滚与原 SEC021 用户种子回归通过。 |
| OCT02-DB-004 | P1 | pgx 解析非法数据库 URL 时，外层错误脱敏后仍可能通过嵌套 URL 解析错误输出密码；启动日志原样记录 Connect 错误。 | Connect 的解析、建池和 Ping 失败出口使用安全错误表示，仅保留阶段、类型、合法 SQLSTATE 或取消/超时类别；Unwrap 保留 errors.As/Is。两个非法 URL RED/GREEN、恶意 SQLSTATE、原始 cause 和实际双连接池 UTC 回归通过。 |
| OCT02-DB-005 | P2 | 部分正式文档仍将旧代 schema、已删除前向回填、旧举报/统计字段当作当前实现。 | 按现有模型更正评论、配方、举报与用户统计文档；保留有日期的历史测试报告，其旧代和 PASS 不继承为本轮证据。 |
| OCT02-DB-006 | P1 | 两个服务器搜索触发器未去重同一服务器的 mod 原始标识，INSERT ON CONFLICT 一条语句可能重复更新同一队列主键，阻断 mod 改名/标识变更（PG21000）。 | SELECT DISTINCT 保留每个受影响服务器一项 upsert；真实旧定义 RED、当前完整 schema GREEN。 |
| OCT02-DB-007 | P2 | 更新日志热度未完整比较旧、新 active 状态，重复 deleted 再扣分，恢复 approved/active 不加分。 | 根据已批准且活动的可见状态差分产生 release 事件；重复状态与恢复真实 PG RED/GREEN。当前 API 无删除/恢复入口，结论是数据库状态一致性缺陷，不是已有 UI 恢复失败。 |
| OCT02-DB-008 | P1 | 评论 AFTER ROW 在首次并发同 author 时两次读到1，计2位有效评论者；同SQL批量首次读到2则漏计，批量末次也可能重复扣分。 | AFTER STATEMENT transition-table 按 route/author 公开行数 delta 推导首末；route fact 行按序锁定，VOLATILE 后续 SQL 读等待后的新快照。真实并发旧定义 RED、修复 GREEN；批量旧定义实际为2评论/0作者，修复单/批/status/author-target移动及首末事件平衡通过。 |
| OCT02-DB-009 | P2 | 历史台账验证器以子串识别成功，把 NOT_PASS、BYPASS、未通过和“失败；通过”当成功，且忽略第16个额外列。 | 仅接受明确 PASS/通过 起始标记及正常分隔符；要求恰好15列。七种失败词、合法括号格式和多列 RED/GREEN，完整历史449条格式/证据门仍通过；验证器只检查记录格式，不证明实际业务测试已经执行。 |
| OCT02-DB-010 | P2 | 数据库目录工具直接调用 pgxpool.New 并 panic 原错误，非法数据库 URI 可能将密码写入终端/日志。 | 复用应用安全 Connect 错误边界。真实 CLI 子进程输入合成非法 URI，修改前泄露、修改后不含密码/URI；不输出捕获的敏感样本。 |
| OCT02-DB-011 | P2 | 目录工具将 information_schema.columns 中三个 view 的列一起计为“表”，产生290表且同时报告3 view，和实际287 base table 不一致。 | 联接 information_schema.tables 仅收集 BASE TABLE，view 单独保留。专用 PostgreSQL 18 新建精确归属 table/view 回归 RED/GREEN；最终只读目录为287表、1130索引、252非内部触发器、3 view。 |
| OCT02-DB-012 | P2 | 导出完成、过期、构建失败与租约耗尽先提交终态，后单独入队通知；Outbox 写入失败后终态不再被领取，永久丢失通知意图。 | 四条入口在同一事务写终态与通知意图，过期文件 tombstone 同时回滚；保留原 notification.direct 事件、租约和上传补偿。真实 PG18 完整临时 schema、MaxConns=1、五项 Outbox CHECK 故障 RED/GREEN；解除故障重试与终态重复投递均保持一项意图，租约失败通知改为实际用户收藏详情链接。 |

协作修复 `OCT02-B-020` 日志保留分类规范化：原循环 trim 后删除错误键，残留原别名，
运营 override 未生效且污染输入 map；现在建立独立规范化 map，canonical 键优先，
仅别名时按排序稳定选择。两项真实单元 RED/GREEN 加两项既有回归 Race PASS，
没有修改正常正整数保留策略。`OCT02-B-017` 日志旧副本安全升级的 schema 部分由本模块
实现，语义脱敏、受控重处理和公开读取由日志模块负责，不能把 schema 通过当成旧日志
已全部重脱敏。

修改位置包括 `internal/database/governance_automation_seeds.go`、`seeds.go`、`database.go`、搜索/更新日志/评论 SQL 及显式修复命令。新增实际 PostgreSQL 回归和安全错误单测位于 `internal/database`，并发评论调用链证据在 `internal/httpapi`。正式行为说明同步到站务、授权、自动更新文档及 `docs/database-function-repair.md`。

审计工具补充修改在 `tools/audit/database_inventory` 与 `tools/remediation/verify_findings`。
最终完整 tools Race 和明确 owned PG18 table/view 集成 PASS / 0 skip 记录于
`database-audit-tools-restored-confirmed-final.log`，原始失败分别为
`database-audit-tools-red.log`、`database-inventory-table-count-red.log`。首次更严格结果格式
检查拒绝旧台账合法 `PASS（…）`，已补充括号兼容和回归；该中间失败不作为原业务问题。
最终目录生成只写任务临时证据目录，没有覆盖旧审计目录。

## 结构审查

实际引擎是 PostgreSQL，应用使用 pgx v5.9.2 和手写 SQL，不是 ORM 自动建表。`Migrate` 一次事务安装完整 schema，使用 advisory lock 串行化初始化，并记录 `schema_metadata.generation=168`；相同 generation 返回，不重放建表语句。不同非零 generation 返回不兼容错误。

现有策略明确是开发期 reset-only，**没有通用旧库前向升级链**。本次新增严格限定
generation 168 的两项非破坏前向操作：四个函数及评论绑定修复、日志已应用脱敏版本列
扩展；它们不是全面升级支持。后者启动时有只读 schema 要求检查，缺列或未验证约束
明确拒绝初始化；同代数字不能代替扩展状态。`ResetDevelopmentSchema` 删除 public
中的对象，其环境、目标名和显式确认检查由配置及调用入口负责。不能因为文档说
“尚未生产”就把任何已有数据库视为可丢弃。已有重要数据的部署，必须先确定迁移和
保留策略；本轮没有执行真实业务库重置、迁移或数据清洗。

已完整逐行阅读归属的全部 database 源码、SQL 声明、种子、测试和项目文档，包括主模型、
review/history、community、访问 view、缓存/队列事实、模组内容/文档、整合包/皮肤/收藏、
搜索/评分/热度及治理/自动化。离线期间读完的固定基线与恢复后当前 Git blob 逐项核对，
不把自动生成指纹当作阅读。转交的35项导出/权限/历史回归也全部完整读取，修改后的
worker、schema、工具及新测试已再读最终内容。03/05历史长报告由 frontend_shared
完整阅读、核对 blob 后移交其台账；04验证日志的独占1041–2800、3751–4450段阅读
记录交由主执行者合并该文件唯一条目。完整范围、数量和最终指纹以台账为准，其他代理
调用链结论仍需主执行者最终复核。

本模块唯一台账纳入279个文件、35,840行，全部完整语义审查（修改后文件已复查），部分/未审/排除均为0。新增的 `tools/audit/review_ledger.py` 与标准库回归测试可从脱敏人工记录重现最终文件清单；11项合成 Git 证据测试通过，生成器本身不执行或继承业务验证。
额外受托完整读取的A模块20个短测试为3283最终行，逐项记录交给其唯一归属台账合并，
不在本模块分母重复计数；其中BUG082/083新schema兼容夹具已补齐并实际回归，其余补审
只提供完整源码语义证据，不能当成这20项全部行为通过。

| 实际对象 | 事实及主要完整性边界 |
| --- | --- |
| users / auth_sessions | 数字内部主键；会话哈希、用户外键、认证版本、到期/撤销。用户角色与直接权限版本和认证版本分离。 |
| public_id_registry / public_routes | 保留全站公开 ID；数字路由身份连接多态业务目标，公开 ID 不是授权凭据。 |
| role_permissions / user_role_bindings / user_permissions | 角色权限关系；用户授权保留独立来源和来源键；恢复种子不覆盖运营 grant。 |
| content_revisions / change_requests / review_events | 聚合修订唯一性、一个待审请求的部分唯一索引、不可变历史触发器；公开目标使用复合外键。 |
| creators / creator_claims / creator_team_members / content_creator_bindings | 作者和团队身份；只允许个人作者认领，团队成员类型守卫，已批准认领唯一。 |
| effective_project_access | 由公开项目、已审核个人作者/团队关系或活动编辑员 assignment 派生；提交者不是权限来源。 |
| oss_files / quota usage / reservation / deletion outbox / multipart sessions | 文件状态与扫描状态；用户额度累计和预留；持久删除补偿、上传会话过期、租约/尝试上限。 |
| project_files | 所属项目复合外键、项目内 OSS 唯一性、扫描安全发布守卫；不注册不可达的通用下载 route。 |
| favorite_collections / items | 默认私有、单用户默认收藏夹部分唯一、集合内目标唯一、受支持目标类型 CHECK。 |
| log_shares / entries | 创建版本保留源文件去重身份，已应用版本独立表达安全副本状态；公开日志不能继承创建时脱敏器的安全假设。 |
| notifications / receipts / watermarks / broadcast state | 来源事件去重、用户读水位、广播派生总量；视图和计数不能替代对象可见性校验。 |
| direct_conversations / messages / unread counts | 用户对唯一、稳定 ID 分页索引、未读派生关系和消息最后指针。 |
| reports / evidence / reviews / moderation / bans | 同一举报人的活动目标唯一、领取 ownership 约束、处置幂等键、单用户活动封禁部分唯一。 |
| site pages / localized site content / license policies | 页面与语言唯一、状态和修订；已有人工状态与政策不是启动种子的覆盖目标。 |
| seed crawler / project auto update | 候选外部身份唯一、首次/最后观察 provenance、按日期 token 预留、任务租约和重试事实。 |
| nats_outbox / processed_events / dead_letter_events | 事务投递意图、consumer/event 幂等主键、明确重试/死信状态和可追踪的管理恢复。 |

上述数据模型具备明确领域边界和大量数据库约束，但也有大量触发器和派生投影，维护时需要同时核对写入服务、schema 与重建路径。外键存在不代表可见性、权限或软删除状态正确；这些规则仍需应用调用链验证。

最终调用链补审另确认 `OCT02-B-030`：更新日志分类写入没有100项业务上限，但读取先取101项、
超过100项即返回错误，因而合法数据增长能使整个日志列表不可用。应保持现有数据并分页读取，
而不是删除分类或补加未经产品确认的数量约束。后端兼容首100项、独立 keyset 分页和显式
`hasMore/cursor` 已实现，前端补齐“更多”入口；负责模块执行的真实 PostgreSQL 回归和
最终浏览器验收由同目录 `VERIFICATION.md` 汇总，源文件语义审查与行为证据分别记录。

CI 后续复验确认并修复 `OCT02-C-017`（P2）：通知批次领取原用事务开始时固定的
`now()`，较早启动的事务或等待行锁后的 READ COMMITTED 行重检，可能把另一批刚提交的
`next_attempt_at` 误判为未到期，返回无工作并延迟到后续扫描。领取条件改用当前
`clock_timestamp()`；任务行锁、200人批次、未来合并窗口/退避和通知唯一去重保持原语义。
负责模块执行真实 PG18 定向 Race：9顶层、2子例 PASS / 0 skip（83.745s），覆盖两种旧事务
调度、201人两批完整分发、未来任务不领取及完成任务重放；不代表实际 NATS 投递通过。
此项没有结构/schema 变化，generation168 及两项显式前向操作顺序不变；修改后的整体
回归结果以对应版本的验证记录为准，不继承修复前的全仓 PASS。正式契约见 `PROJECT_FOLLOW_NOTIFICATIONS.md`。

连接池显式设置 UTC、连接超时 10s、lock_timeout 10s、statement_timeout 5min、idle-in-transaction timeout 60s；活动写入有独立连接预算。它们是配置与隔离测试证据，不证明生产池大小合适、没有锁等待或不存在慢查询。

## 测试库验证

主执行者新建的本任务独占随机数据库，经明确目标核对后完成空库安装。最初独占 PostgreSQL 17.11 的目录检查通过；完整临时 schema 清理随后遇到共享内存锁不足 SQLSTATE 53200。主执行者为本任务新建 PostgreSQL **18.6**、max_locks_per_transaction=512 的隔离实例后重新安装并验证，不改用户旧实例。只读系统目录结果为 generation **168**、**287 表 / 1130 索引 / 521 外键 / 250 非内部触发器 / 3 view**（修复评论绑定前）；无 invalid index 或未验证 constraint。三条评论 statement 绑定替代一条 row 绑定后触发器增加2。这里是本轮实际隔离库数据，不能据旧报告推出生产运行或性能结论。

`TestEveryForeignKeyHasLeadingIndex` 在真实 PostgreSQL 完整临时 schema 中通过：所有外键有有效前导索引；log share 的非空 owner 复合部分索引被准确接受，未重建冗余单列索引，EXPLAIN 命中保留索引。该计划是测试实例的索引可用性验证，不是生产性能或最佳索引证明。

新增种子回归使用独占数据库中的随机测试 schema，并执行生产表声明和真实事务；不以 SQLite、内存数据库或 mock 代替 PostgreSQL。当前入口：

```sh
# 首先使用项目 setup 说明准备明确 owned、仅回环可达的 PostgreSQL 测试目标。
export DATABASE_URL='<专用隔离测试数据库连接串>'
export APP_ENV=test DB_RESET_ON_START=false MCMODS_RUN_DB_INTEGRATION=1
CGO_ENABLED=1 go test -race ./internal/database \
  -run '^(TestGovernanceSeeds|TestDefaultRoleSeeds|TestSEC021)' -count=1 -v
CGO_ENABLED=1 go test -race ./internal/database \
  -run '^(TestConnectDoesNotExpose|TestConnectionErrors|TestApplicationDatabasePoolsForceUTCSessionTimezone)' -count=1 -v
go test ./internal/database -run '^TestEveryForeignKeyHasLeadingIndex$' -count=1 -v
```

已执行结果：最终种子 Race **10 个顶层测试及 2 子测试 PASS / 0 skip**；连接安全/UTC Race **3 个顶层测试及 6 子测试 PASS / 0 skip**；先前完整索引/种子/身份检查 **13 个顶层测试及 2 子测试 PASS / 0 skip**。完整全仓回归由主执行者的环境验证记录汇总，不能把这些定向门当作全部业务测试已通过。


本次 PostgreSQL18 projection/forward 验证：服务器搜索及 changelog 两个顶层/五子例 Race PASS；四函数+原/新评论绑定 forward/recovery 一个顶层/七子例 Race PASS；恢复输入白名单一个顶层/六子例 PASS；评论单/批差分一个顶层/五子例 Race PASS；实际并发首评论一个顶层 Race PASS；完整临时 schema 的全部 developer 排除与批量评论集成一个顶层 Race PASS。CLI 私有备份两个顶层 PASS，实际 inspect/apply/reapply/wrong-target/existing-backup/restore/final inspect 的预期退出码全部匹配。后续更广的并发/全库回归由主执行者汇总，不把它们预先写成通过。

新增命令只在明确 owned 的可丢弃库运行：

```sh
export MCMODS_DB_FUNCTION_REPAIR_TEST_TARGET='<核对后的本任务独占库名>'
CGO_ENABLED=1 go test -race ./cmd/db-function-repair ./internal/database \
  -run '^(TestOCT02|TestProjectionRestore|TestFunctionBackup)' -count=1 -v
```

脱敏原始证据暂存于任务证据目录：`database-governance-seeds-red.log`、`database-role-seeds-red.log`、`database-error-redaction-red.log`、`database-seeds-final-race.log`、`database-connection-redaction-green.log`、`database-schema-seeds-validation.log`、`database-owned-catalog-summary.log`。新增 `database-owned-pg18-catalog-summary.log`、`database-function-repair-green.log`、`database-function-repair-final.log`、`database-function-repair-cli-final.log`、`database-comment-batch-red.log`、`database-comment-statement-green.log`、`database-comment-concurrent-first-green.log`、`database-projections-final-pg18.log` 保存脱敏实际命令输出；恢复文件只在私有任务目录。

一次系统目录诊断最初误用未设置的 `DB_NAME`，psql 回落到环境原有本地数据库，仅执行 pg_catalog/版本只读查询，无业务数据读取或写入。结果已剔除；后续从 DATABASE_URL 提取库名并精确核对任务独占身份后重做。Go 回归始终通过 config.Load 的 DATABASE_URL 指向专用测试库。

恢复后的最终只读目录为 **287表、2777列、3982约束、1130索引、252非内部触发器、
3 view、126序列、183函数**（`database-docs-catalog-final.md`，2026-10-02T05:26:32Z）。
日志扩展增加一列及其非空/值约束；这里仍只是当前测试库目录。
导出事务最终回归 `database-export-notification-regression-final.log` 为 Race **9顶层、
9子例 PASS / 0 skip**，覆盖故障通知、计数漂移、取消、租约恢复、提交补偿与孤儿恢复。
外部上传和邮件没有真实连接；此结果不保证外部请求或计费严格一次。

日志前向工具 `database-log-redaction-forward-final.log` 为真实 PG18 Race **1顶层、
7子例 PASS / 0 skip**，另有0600/O_EXCL命令单测通过；原168缺列、误目标/代数、
记录失败、DDL冲突回滚、验证故障续跑、v1/v2共存与旧计数保留、无效版本拒绝、
重复执行和启动检查均实际执行。CLI `database-log-redaction-cli-final.log` 的默认只读、
误目标、显式应用、已有记录拒绝、重复应用及最终检查退出码与0600权限均符合预期。
首次导出回归因其他文件短暂编译失败受阻，下一次缺少显式 owned target 而 SKIP；
两者没有当成业务 RED 或 PASS，补齐目标后五项业务 RED 才用于修复依据。

`database-a-fixtures-log-share-red.log` 保存旧BUG082缺少新列的真实42703失败；BUG083
原有两项已通过。仅补TEMP字段和显式INSERT列名后，最终三个顶层Race测试/零skip
通过（`database-a-fixtures-log-share-green.log`）；回滚、重试、租约恢复与删除隐私断言保留。

## 历史问题与文档

最终含通知时钟修复的PG-r4单次整套为1794调用、1782 PASS、12条件SKIP、76批零失败，
源码1195指纹漂移0。其中五项明确独占目标的public修复/恢复/事务/库存测试另以串行
race执行5顶层/24子例、0SKIP；独立通过不改写原整套SKIP。最后日志规模测试夹具
新增完整统计采样后，仅该1测试字节变化，定向实际PG race 1PASS/0SKIP、5.076秒。
统计修正只作用于1.3M行TEMP夹具的四列，验证实际稀疏词频，保留原GIN/无SeqScan、
分页及清理1000行断言；没有改变生产SQL、索引或统计策略，也不推断生产性能。
同源adab PR CI通过、push该测试失败的两结果均保留；旧完整门不冒充新夹具同次执行。

实际历史登记包含 449 个稳定问题 ID，其中 DB 类 8 项；历史台账宣称全部 CLOSED。此次没有把 CLOSED 当成重新完成的行为证据。

8 个原 DB 问题已逐项重新核对当前实现、具体断言和本轮实际执行记录：DB-001 最终 Outbox
建表；DB-002 关系原位更新保留审计/隐藏状态与授权撤销；DB-003 作者导入预览不写持久身份；
DB-004 四组唯一约束索引没有等价重复；DB-005 项目文件没有不可达通用 route；DB-006
镜像按供应商文件身份唯一并保留补偿；DB-007 无用 chat presence 表不存在；DB-008
外键前导索引与精确非空部分索引接受规则。对应当前源文件与测试指纹匹配主执行者的
06:19 执行快照，实际 PG 门 batch10–14、26、47–49 的结果逐项绑定，不能把门名称
含“集成”直接当作所有断言使用完整真实 schema。供应商调用替代、source guard 和
强制索引计划的限制保留；测试库事实不证明生产状态或任何旧 generation 都可升级。

本模块另外核验16项原 BUG/SEC/PERF/OPS/MAP/LEGACY/DEAD问题，合计24个唯一原ID
判为“当前原根因已解决且对应范围已验证”，8个DB项包含其中，不能再次累计。
逐项来源、源码指纹、具体断言、真实执行日志与验证范围交主执行者合并正式原449项台账；
本段不宣称其余425项均已解决，也不把本轮新的DB修复追记为旧问题的首次修复。

`COMPATIBILITY_BOUNDARIES.md` 旧通知路径和裸 Core NATS payload 描述已由主执行者
核对并更正为 canonical `/api/v1/me/unread-summary`、可靠消息信封必需；本轮没有恢复
先前删除的别名或裸负载兼容，也没有取得旧客户端使用量或真实集群消息存量。
历史“9k层”性能记录、强制 `enable_seqscan=off` 的执行计划、最小临时表、外部上传
mock、source-contract、先前公共库 fixture 误用与最终重验尚未记录结果等边界，均保留
为历史证据限制，不据449 CLOSED推导当前全链或生产健康通过。

## 实际运行验证与发布边界

**生产运行健康未验证**：未取得生产表/索引膨胀、vacuum、真实数据分布、锁等待、连接压力、慢查询、容量、复制延迟、备份恢复或多节点滚动发布证据。测试库无 invalid index 不能推出这些机制健康。

本轮 schema generation 保持168，不要求开发库重置；日志分享新增已应用版本列。
先保存正常数据备份，在授权范围内执行两项默认只读检查；明确目标后，先显式修复
四个函数/评论绑定，安装并验证日志版本列，再发布 API/worker。种子修复随后端发布
生效，只修正精确识别的未编辑默认正文，保留人工政策/授权。新空库直接包含全部定义。
启动不会自动执行这两项已有库升级；日志扩展缺失或未验证会拒绝初始化。
函数工具已验证原168前向修复、重复、误目标/generation、备份失败、DDL全回滚和原绑定
恢复；恢复不删除新写入，但旧函数会重新引入计数缺陷。日志工具 ADD+NOT VALID 与
VALIDATE 分阶段执行，验证中断可以重跑；恢复代码保留新增列，恢复旧脱敏器不是
安全回滚。工具的函数备份可恢复定义，日志 schema 状态文件不能替代数据备份。
完整发布与恢复见 `docs/database-function-repair.md`、`docs/log-redaction-upgrade.md`。

已有统计是否受旧缺陷影响、是否需要核对/纠偏没有真实运行数据；本次不删除或重写历史事件。非168旧库仍需独立前向演进方案，不能复制开发 RESET 流程。

回滚代码不删除本轮任何新业务数据；回滚旧 seed 逻辑会重新引入政策和授权覆盖问题，
不能以回滚代码代替恢复已被旧版本覆盖的运营决定。逐行阅读完成与行为验证分别登记；
没有当前行为测试的历史声明、未取得的实际运行数据、完整灾备和其他旧代迁移能力，
继续记录为未验证。
