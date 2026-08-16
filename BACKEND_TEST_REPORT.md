# 后端与整体回归测试报告

测试日期：2026-08-15（Asia/Shanghai）

## 环境

- Windows / PowerShell
- Go 1.26.5：`C:\Users\xsx20\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.5.windows-amd64\bin\go.exe`
- Node.js 24（Codex bundled runtime）
- Next.js 16.2.11 / React 19.2.4
- PostgreSQL：应用已连接经用户确认允许压测的远程专用测试库；本机仍未安装 psql，无法直接采集数据库端查询计划和等待事件
- `MCMODS_RUN_DB_INTEGRATION`：仅在本轮 Generation 70/Outbox 定向测试命令中临时设置

## 修改前基线

开发开始前实际执行的 `go test ./...` 通过，前端 `tsc --noEmit` 通过。基线没有保存 `-json` 逐测试计数，因此不虚构基线用例数量。当前已知失败为 0。

## 实际执行命令与结果

```powershell
# 后端针对性回归（开发阶段多次执行）
go test ./internal/httpapi ./internal/database ./internal/app ./internal/userstats ./internal/activity

# 后端完整回归和静态检查
go test ./...
go vet ./...

# 不使用 Go cache 的逐用例统计
go test -count=1 -json ./...

# 数据库集成测试门禁检查
go test -v ./internal/database -run 'TestGeneration70UserFeaturesIntegration|TestEveryForeignKeyHasLeadingIndex'

# 竞态检测尝试
go test -race ./...
$env:CGO_ENABLED='1'; go test -race ./internal/activity ./internal/httpapi ./internal/userstats

# 前端完整检查和生产构建
node node_modules/eslint/bin/eslint.js .
node node_modules/typescript/bin/tsc --noEmit
node node_modules/next/dist/bin/next build

# 压力脚本语法检查
node --check scripts/load-test.mjs
```

结果：

- Go：581 个测试通过，21 个测试跳过，0 个测试失败；13 个有测试的 package 通过。
- `go vet ./...`：通过，无输出。
- 前端 ESLint：通过。
- 前端 TypeScript：通过。
- Next.js 生产构建：通过；编译、类型检查、31 workers 页面数据收集及 51/51 静态页生成完成。
- 数据库集成门禁：两个目标测试明确 `SKIP`，原因是未设置隔离数据库开关且本机无 PostgreSQL。
- Go race：未执行成功。第一次报告 `-race requires cgo`；启用 CGO 后报告 `gcc not found`。这是环境限制，不记录为通过。
- 真实 HTTP 压测：7 个有效阶段共 8,127 次请求全部成功，4xx/5xx/网络错误均为 0；测试后健康检查仍为 `200`、`ready=true`。

## 本次新增覆盖

### 单元/静态测试

- 排序白名单、非法 SQL 片段拒绝、NULL 热度处理、降序与更新时间/ID 稳定次序。
- 更新时间和项目特性筛选白名单。
- Minecraft 版本集中配置的默认、规范化，以及预发布版/候选发布版分类。
- 分类合法性和社区列表查询参数。
- UTF-8 编辑新增/删除/替换字节，`changed=added+deleted`、`net=added-deleted`。
- 统计范围、活跃日和连续活跃计算。
- 六槽长度、公开统计字段白名单、重复项和敏感字段拒绝。
- 在线/离线/隐藏映射；隐藏用户活跃与否都只返回 `hidden`；心跳窗口大于写节流。
- 高频查看的 Redis/本地降级去重门；相同用户与对象在窗口内只接受首个 claim。
- 动作级保留策略、非法动作拒绝、时间 RFC3339/UTC 边界、参数化清理条件、确认 token。
- 活动动作名称与数据库种子一致，服务器对象类型正确映射公共路由。
- generation 70 schema 结构、默认隐私、六槽约束、统计/清理表、持久化动作 Outbox、内容事实触发器修复和禁止启动全量回填断言。
- 评论固定幂等键使用事务级 advisory lock，锁键包含用户和幂等键命名空间。
- 回填 SQL 的幂等/锁定/持久累计约束。

### 接口与权限链静态验证

- 统计本人接口要求认证；他人完整统计由本人、`user.read` 或 `admin.*` 决定，普通访问仅公开投影。
- 卡片接口不接受字段名，只读取六槽白名单。
- 私聊列表、评论、主页和卡片都在 SQL 后通过同一隐私映射返回状态。
- 对隐藏收件人，消息 `readAt`、通知抑制和会话活跃元数据不对发送者返回，避免私聊侧信道。
- 日志配置 GET 使用 `log.read`，配置/预览/执行使用 `log.write`；执行仅 POST。
- 手动清理用户/对象使用公开 ID 参数化解析，动作/对象类型为后端枚举，执行条件来自服务端保存的预览快照。

这些验证包含真实路由注册、中间件和查询调用链阅读以及可执行纯逻辑测试。后续压力测试已通过真实 HTTP+PostgreSQL 覆盖目录、卡片、私聊、在线心跳、个人统计、管理员日志、保留策略和清理预览；未进入压力场景的安全边界仍只按静态/单元测试结论记录。

## 并发与一致性

已由实现和单测验证：

- 统计由 PostgreSQL `INSERT ... ON CONFLICT DO UPDATE counter=counter+excluded` 原子累加，不使用应用层读改写。
- 内容事实以 `(content_type, object_key)` 主键 upsert，审核变化/恢复不会新增第二条。
- 回填按用户事务 advisory lock，并可幂等重跑。
- 自动清理使用 session advisory lock，手动清理以 `snapshotBefore` 隔离预览后写入。
- 在线状态按认证 session hash upsert，支持多标签/多设备，登出删除当前会话。

真实压力测试已验证远程 PostgreSQL 查询和 12 连接池下的目录/统计/心跳/预览并发，以及评论读取、创建、编辑、表态和幂等竞争。修复后没有计数器错误、5xx、网络错误或进程崩溃；50 并发目录和 25 并发评论读取阶段出现吞吐收益下降和尾延迟增大。仍未直接验证数据库服务器锁等待/死锁、实际日志删除与持续写入竞争、跨用户回复通知、热度刷新与分页并行、缓存击穿，因为缺少数据库端遥测且没有执行日志删除。

## 数据库迁移与查询分析

已完成静态迁移测试和可执行集成测试 `TestGeneration70UserFeaturesIntegration`。`scripts/query-analysis.sql` 覆盖：

- Mod、插件和服务器热度排序；
- 分类/版本/热度组合；
- 用户日统计和内容事实；
- 用户卡片摘要和会话在线状态；
- 用户操作记录分页、动作+用户+对象+时间预览及批量删除计划。

应用能够连接远程测试 PostgreSQL；generation 70 已在用户授权的 Dev 数据库重置后完成空库安装。`TestDurableOutboxSurvivesProducerAndDrainsExactlyOnceAcrossWorkers` 使用真实 PostgreSQL 验证 20 个关键动作可由 4 个消费者通过 `SKIP LOCKED` 最终精确写入 20 行、Outbox 清空且累计统计为 20。当前仍无 psql/数据库主机遥测，因此千万级数据下的 `EXPLAIN (ANALYZE, BUFFERS)`、VACUUM、WAL、锁等待和重复索引检查尚未执行。

## 真实压力测试

- 公开目录：2/10/20/25/50 并发，最长持续阶段 60 秒；最高吞吐 87.65 RPS（25 并发），50 并发时 P95 2,294.30ms 且吞吐回落到 45.23 RPS。
- 鉴权混合：5 并发 20 秒和 15 并发 30 秒，覆盖 17 类目录、用户、在线、统计和管理员请求；15 并发为 28.92 RPS、P95 1,167.10ms。
- 总计：8,127/8,127 成功，4xx/5xx/网络错误均为 0。
- 进程：CPU 峰值 3.09%，工作集峰值 145.0MB；测试结束工作集回落到 24.6MB，未观察到单调增长。
- 数据安全：未启用评论 mutation，未调用日志清理 execute；只生成测试登录会话、在线心跳和清理预览审计。
- 测试后：`GET /health` 返回 200、`ready=true`，38ms。

评论专项另执行 3,253 次请求：

- 评论树读取：2/10/25 并发及 10 并发 60 秒持续阶段，2,092/2,092 成功；25 并发为 24.56 RPS、P95 1,413.16ms。
- 同一评论编辑/表态：5 和 15 并发，726/726 成功；包含 182 次编辑和 181 次表态，无 409/500/超时。
- 幂等竞争：修复前 36 次同键创建出现 1 次 500、只新增 1 行；事务锁修复后 31 次同键创建为 1×201、30×200，只新增 1 行。
- 创建限流：76 次唯一键创建为 20×201、56×429、0×500，限流没有被并发绕过。
- 清理：精确枚举并软删除 24/24 条测试评论，0 失败；目标下没有未删除根评论。
- 触发器：压测夹具创建发现旧 generation 68 `object_key` 歧义导致 SQLSTATE 42702；generation 70 空库定义保留该修复，并已在重置后的运行库验证。

完整阶段数据、原始 JSON 和限制见 `LOAD_TEST_REPORT.md`。测试库目录数据为空，结果不能代表大数据量生产容量。

## 安全检查结论

- SQL 注入：排序、动作、对象类型、范围和特性使用白名单；用户/对象/时间使用 pgx 参数。通过单测/代码链验证。
- IDOR/越权：统计、设置、清理均由后端 claims/permission 中间件控制；不信任前端 user ID。通过代码链验证。
- 任意统计字段：固定后端 key -> 固定 SQL 列映射；未知/重复 key 拒绝。通过单测。
- 在线隐私：所有普通响应只返回三态枚举，不返回精确活动时间；私聊已读侧信道遮蔽。通过单测/代码链验证。
- CSRF：变更接口要求 Authorization Bearer token、非 GET 方法并沿用现有 CORS；未进行浏览器自动化攻击测试。
- XSS：新 UI 只用 React 文本插值，没有新增 `dangerouslySetInnerHTML`；未运行动态扫描器。
- 大范围删除：15 分钟一次 token、同一管理员、精确 `DELETE n` 短语、危险范围标记和独立审计。通过单测/代码链验证。
- 错误输出：新接口使用统一错误 writer，未返回 SQL/表结构；没有运行外部 DAST。

## 尚未解决/未验证

### Generation 70 动作摄取加固复测

实际执行：

```powershell
go test ./...
go vet ./...
node --check scripts/load-test.mjs
$env:MCMODS_RUN_DB_INTEGRATION='1'
go test -count=1 -run TestDurableOutboxSurvivesProducerAndDrainsExactlyOnceAcrossWorkers ./internal/activity
go test -count=1 -run TestGeneration70UserFeaturesIntegration ./internal/database
$env:MCMODS_RUN_ACTIVITY_LOAD='1'
go test -v -count=1 -run TestDurableOutboxConcurrentLoadIntegration ./internal/activity
```

- Dev 数据库按授权重置并成功安装 Generation 70，后端重新启动后 `/health` 为 `200`、`ready=true`。
- 20 条 Outbox 事件由 4 个并发消费者持续领取，最终 Outbox 为 0、原始事件恰好 20 条、累计统计恰好 20；无重复或丢失。
- 定向负载为 2,000 个关键动作、20 个并发生产协程、4 个消费实例：入队 8.678s（230.46 条/秒），全部入库 8.863s（225.65 条/秒），测试总耗时 10.72s；Outbox 最终为 0，原始事件恰好 2,000 条。
- 管理员状态接口返回 `200`、`status=healthy`；未认证请求返回 `401`。验证时独立动作池 `maxConns=4`，Outbox/内存积压均为 0，写入失败和重试均为 0。
- 用户统计 GET 被正确识别为 `view`：`acceptedBestEffort=1`、`flushedBestEffort=1`、`enqueuedDurable=0`。同时修复 `/users/...` 被 `"/use"` 子串错误识别为 `use` 的旧缺陷。
- `go test -count=1 -json ./...`：581 通过、21 跳过、0 失败；13 个 package 通过。
- Race 再次尝试但未完成：`CGO_ENABLED=1 go test -race ./internal/activity ./internal/httpapi` 因本机没有 `gcc` 失败，未标记为通过。

这组 2,000 条负载验证摄取一致性和当前测试数据库下的短时吞吐，不代表千万级表容量。数据库主机 CPU、WAL、锁等待、VACUUM 和磁盘 I/O 仍不可见。


1. 真实 API 已连接远程测试 PostgreSQL 并完成压力测试，但目录业务数据为空；缺少 psql/数据库监控，Redis、NATS、Typesense 的本地集成环境也仍不完整。
2. 缺少 GCC，Go race 测试未执行。
3. 没有浏览器 E2E 测试框架；完成了生产构建和组件调用链验证，但悬浮/移动端手势/前进后退需人工或 Playwright 环境复验。
4. 压力测试已取得目录和评论的有效应用样本，但没有修改前同环境基线、代表性大数据、数据库端遥测和实际日志删除竞争，详见 `LOAD_TEST_REPORT.md`。
