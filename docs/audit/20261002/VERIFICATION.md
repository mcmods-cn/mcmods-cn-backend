# 环境、执行结果与证据边界

本轮基线：后端 `95694ccff038a28a3e6126b05e3e7c5037bc7854`，前端
`262e1c0136bfed4624a011cf5f445b1d71cf93c0`。两仓库分支均为
`codex/full-audit-20261002`。最终执行源码快照按相对路径保存 SHA256，审查清单
另核最终文件内容；阅读覆盖率和下面的测试/语句覆盖率分别计算。

## 实际环境与复现

使用 Go 1.26.6、Node 24.19.0、npm 11.9.0、Playwright 1.62.1 与其锁定 Chromium，
专用 PostgreSQL 18.6、Redis 7.4.11、NATS 2.11.17；实际搜索专项使用 Typesense 29。
系统原有 `go` 是同名非 Go 程序，已安装官方声明版本并显式激活。没有假定环境
自带 Docker、浏览器或数据库；实际核对本地 daemon 后以随机所有权标签创建服务。
数据库与服务只接受回环目标，测试账户/密钥合成，SMTP 和付费供应商关闭。
安装、健康检查、初始化、启动和精确所有权清理见
[隔离环境说明](../../testing-isolated-environment.md)。依赖按 lockfile 安装，没有全栈升级。

云端曾中断并恢复。后来工作区磁盘耗尽导致 SQLSTATE 53100 的测试建库失败；
已保留原失败，将确认属于本任务的旧 Go 编译缓存移到独立临时盘并保留副本。
没有删除数据库或用户文件。恢复后重跑相应真实 PostgreSQL 回归，不能将安装成功
或空间恢复当作测试通过。临时服务和私有恢复备份不进入提交。

## 质量门

| 检查 | 当前结果与准确范围 |
| --- | --- |
| 修改前 PostgreSQL 基线 | PASS：1606 个顶层测试中 1600 PASS、6 条件 SKIP；属于修改前版本。 |
| 当前 `go test ./... -count=1 -coverprofile=… -json` | PASS / exit0：1239 顶层 PASS、554 条件 SKIP；子测试 785 PASS、76 SKIP。默认模式不启用专门的外部/数据库 opt-in。 |
| 当前 `go test -race ./... -count=1 -json` | PASS / exit0：同上顶层及子测试，22 包 PASS、9 包 SKIP；实际 DB 并发另列，不将默认 SKIP 当成集成通过。 |
| `go vet ./...`、`go build ./...` | PASS / exit0。 |
| 当前默认 Go 语句覆盖率 | 30.8%，实际 `go tool cover`；不是文件阅读比例、全部业务场景比例或真实 DB 覆盖率。 |
| 当前真实 PG 全库 `go run ./tools/testing/db_suite` | PASS / exit0：1793 顶层调用、1786 PASS、7 条件 SKIP；76 批、零失败。明确 owned PostgreSQL、RUN_DB_INTEGRATION=1，并启用精确测试目标下的函数恢复测试。 |
| 真实 queue / searchindex / activity 整包 race | PASS；NATS 重启/双 worker 恢复、Typesense29 回环服务、PG 活动清理分别实测。最终 queue 22.903s；不能据此推断生产集群健康。 |
| AI 专项真实 PG race | PASS：30 顶层、0 SKIP，4.115s；供应商是确定性本地 HTTP，未产生真实付费调用。 |
| 依赖图工作预算与百万关系索引 race | PASS：3 顶层、0 SKIP，46.673s；长链与稠密环真实执行计划 RED/GREEN，既有百万关系索引与两秒断言保留。 |
| 种子爬虫正常停机/租约保护 race | PASS：13 顶层、0 SKIP，46.800s；查询边界真实取消、数据库 CHECK 故障及旧 lease owner 均验证。 |
| 图标导入 MaxConns=1 | PASS：Mod 创建旧 deadline RED→GREEN；整合包和简单项目同根因 12 顶层 race PASS；原并发 slug 断言保留。 |
| 前端 lint / typecheck / unit | PASS / exit0：R7当前最终 lint/typecheck；r6 全部344单元测试通过、0 SKIP，生产源码未再修改。 |
| 前端 production build | r6 PASS / exit0，明确示例 HTTPS 编译目标；09:17产物晚于08:54最后分页修改，最终源码快照漂移为零。 |
| Chromium 生产界面受控 API | R7单次完整73 PASS、0FAIL/0SKIP，180.621s；包含语言偏好、配方类型编辑/失败恢复和初始导入排他。R6同一生产产物70PASS/166.280s保留；不把局部结果相加冒充全套。 |
| 当前真实前后端浏览器旅程 | PASS / exit0：真实 cookie 登录、HttpOnly、收藏创建/刷新持久化、删除取消/确认、退出、英中切换及390px。本项目 API 无 fixture；精确终态及版本见 JSON。 |
| 文件审查发布器 | 11项真实本地回归 PASS，覆盖漏段/旧指纹拒绝、人工资产不可误排、私有环境不读、删除保留及快照重放；不执行业务测试。 |
| 依赖安全与密钥 | 结果见 `SECURITY_REVIEW.md`；真实供应商连通性/语义质量/账单及生产运行健康 NOT_RUN。 |

原始结果和版本快照留在私有任务证据目录，发布的 `verification-results.json`
保存脱敏命令、退出码、计数和证据 SHA256，不保存数据库 URL、密码、原文或日志全文。

## 失败记录与修正

首轮修改版 PG 全库实际运行 1749 顶层/74 批，8 批失败：蓝图夹具缺新投影字段、
OSS 现有文件长度约束各一批；六批撞上分类分页实现的中间编译状态。均保留首失败，
相应修正后完整稳定版重新执行1793顶层、76批并通过；旧失败记录不擦除。

一次默认 Go 门错误地同时设置专用 DB URL 和 RUN_DB_INTEGRATION=0，四项旧
opt-in 测试因 URL 被启用却被 schema 安全守卫拒绝；终止该错误组合并保留输出。
默认门取消 opt-in URL、仍保留明确 owned 应用 DATABASE_URL，完整重跑通过；
真实数据库门使用匹配 URL 和 RUN=1。没有放宽安全守卫、删测试或修改核心断言。

前端 r3 单元 342/344，两个源码合约仍匹配旧变量名/旧依赖数组；校验所需取消和
scope/attempt 语义后修正断言，344/344。浏览器发现的焦点恢复、代码块图标误展开、
恢复检查抢占导入标签为实际产品问题，已经修复。错误 fixture 的扁平 API 契约、
按钮译文、严格选择器、有限角色配置读取和跨标签语言确认分别纠正；首失败不擦除。
一次 root 浏览器调用漏激活已安装的浏览器缓存，启动失败保留；配置正确后执行。
没有延长测试预算或移除 unexpected-endpoint 断言来掩盖问题。

r5完整69项通过，但最后全局目录偏移守卫及第三项边界测试晚于该产物；不能作为
最终源码验收。重新构建后的r6完整70项通过，指纹与最终源码一致。原访客Markdown
输入异常不能无证据归因为身份草稿GET；可控延迟GET单独复现了已登录初次读取覆盖
输入的真实缺陷，修复后该测试及原访客场景均通过。

最终PG的7条SKIP为Minecraft loader在线来源、四项exporter真实样本、Modrinth真实
元数据及真实Typesense opt-in。最后一项已由独立Typesense29整包race验证，仍保留
本次PG命令本身的SKIP，不把其它命令的PASS改写到它的计数中。

后续一次受限公开上游验证实际PASS：Loader四供应商子例2.78s、Modrinth真实元数据
0.32s，整包3.322s/exit0（`backend-a-public-live-once.log`）。测试未读取供应商密钥。
四项exporter仍缺项目对应的真实样本，不能用合成ZIP或其他exporter代替。已检查
两仓库及本地样本目录；第三仓库元信息显示private，未继续读取其源码/样本。
最低解除条件是提供获授权、与当前契约对应的真实导出样本及其来源/版本。

工具与依赖清单亦实际执行：隔离环境Python最终4项、审查发布器Python11项均PASS；
`go mod tidy -diff`和`go mod verify`均exit0，go.mod/go.sum无改动。quality CI已纳入
上述两组Python检查。50个选中Go模块的许可文本与剩余授权缺口见安全报告。

任务资源终态实际核对：7个带精确身份的自建容器已删除，4任务进程已停止，原3云端
服务仍运行；私有修复备份保留，临时环境凭据文件已移除。清理工具目录非空误报
以真实4项Python RED/GREEN修复，原失败保存，不递归删除未知文件。

## 未取得的证据

没有生产数据、容量、膨胀、复制延迟、实际锁压力、灾备恢复、多节点滚动发布或
真实翻译质量/费用证据。外部邮件、OSS 和供应商以本地 fixture 验证故障协议；
GPU 长时间运行、全部低端设备、全部长译文和每个模块所有角色旅程尚未完整实测。
这些范围分别列出，不能以源码已完整阅读、单元通过或截图存在推断已验收。
