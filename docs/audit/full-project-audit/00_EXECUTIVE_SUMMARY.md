# MCMods 全项目审计执行摘要

审计完成日期：2026-08-21（Asia/Shanghai）

## 最终结论

本轮已完成 **791 / 791 个第一方代码与配置文件、169,449 / 169,449 行**的逐文件人工语义审查，严格人工覆盖率为 **100.00%**。覆盖清单中“未开始”“审查中”“无法确认”均为 0；791 份审查记录的当前文件 SHA-256 全部匹配，无文件在审查后发生未复核变化。

93 个文件被排除于第一方代码/配置基数：项目文档、依赖锁文件、位图/字体/图标等静态二进制资源，以及只需构建和引用验证的矢量静态资源。每个排除项及理由均逐项记录在 [modules/FILE_COVERAGE_INVENTORY.md](modules/FILE_COVERAGE_INVENTORY.md)。

完整审计共登记 **449** 个不重复 Finding ID：Critical 0、高 176、中 214、低 59、Info 0。按编号类别统计：BUG 151、SEC 47、PERF 72、DB 8、ARCH 33、REUSE 7、TEST 50、OPS 22、STYLE 9、MAP 12、LEGACY 22、DEAD 16。

因此最终成熟度结论由 9.97% 阶段的 **B** 调整为 **C：暂时不应把主要开发精力正式转入前端优化，应优先修复后端安全、权限、数据一致性和可靠任务阻塞项**。低风险视觉和设计系统工作可以并行，但不能冻结存在高风险发现的 API 与业务协议。

## 最严重的十项阻塞

1. `SEC-011`：管理员可配置的导入 BaseURL 可能把已保存供应商密钥发送到任意 HTTPS 主机。
2. `SEC-013`：Mod 编辑者可通过入向关系改写或删除其他项目拥有的关系数据，形成跨项目 IDOR。
3. `SEC-039`：转账税和商品总价存在整数回绕，可能破坏货币守恒。
4. `SEC-040`：切换或清空等级轨道不会撤销旧轨道角色，形成权限残留。
5. `SEC-044`：后台权限保存会丢失角色到期时间与来源，可能永久化临时封禁或授权。
6. `SEC-009`：全局 SHA 去重包允许后上传用户覆盖既有源文件指针，产生跨用户事实干扰。
7. `BUG-042`：爬虫创建、草稿、候选和来源绑定跨多步提交，可能形成分裂状态。
8. `BUG-066`/`BUG-069`：MRPack 依赖或行处理错误可被静默忽略，仍生成 `ready` 整合包。
9. `BUG-019`/`LEGACY-006`/`LEGACY-019`：可靠 Outbox 与旧 Core NATS/进程任务路径并存，存在任务不再入队或消息丢失窗口。
10. `PERF-004`：每个成功写请求启动无界后台 goroutine，并以无截止时间上下文执行反滥用 SQL。

## 最终验证结果

| 范围 | 命令或检查 | 最终结果 |
| --- | --- | --- |
| 后端单元/默认测试 | `go test ./...` | 通过 |
| 后端静态检查 | `go vet ./...` | 通过 |
| 后端构建 | `go build ./...` | 通过 |
| 覆盖率 | `go test ./... -coverprofile=coverage.out -count=1` | 通过；总语句覆盖率 18.4% |
| 模块整洁 | `go mod tidy -diff` | 通过；无差异 |
| Go 格式 | 对全部跟踪 Go 文件执行 `gofmt -l` | 14 个文件未格式化 |
| PostgreSQL 集成 | `MCMODS_RUN_DB_INTEGRATION=1 go test ./... -count=1` | 通过；从原后端 `.env` 安全加载远程测试库配置，并显式关闭数据库重置；数据库、HTTP API、活动、反滥用和搜索集成包均执行成功 |
| NATS/JetStream | `MCMODS_RUN_NATS_INTEGRATION=1 go test ./internal/queue -count=1 -v` | Core NATS 可达；JetStream 返回 `no responders available`，集成用例失败 |
| Race | `CGO_ENABLED=1 go test -race ./... -count=1` | 未执行；缺少 `gcc` |
| Go 漏洞扫描 | `govulncheck` 可用性检查 | 未安装，未执行 |
| 前端类型检查 | `pnpm typecheck` | 通过 |
| 前端 Lint | `pnpm lint` | 通过 |
| 前端生产构建 | `next build --webpack` | 通过；59/59 静态页面生成成功 |
| 前端严格 unused | `tsc --noEmit --noUnusedLocals --noUnusedParameters` | 失败；确认 1 个未使用参数（`DEAD-001`） |
| 前端依赖漏洞 | `pnpm audit --prod` / `npm audit --omit=dev` 可用性检查 | 未完成；仓库只有 `package-lock.json`，pnpm 拒绝无 pnpm lockfile 的审计，当前环境没有 npm CLI |
| 前端自动化测试 | 仓库脚本/文件审查 | 0 个测试文件、无 test script，无法执行 |

`pnpm build` 的 Turbopack 首次尝试因审计副本通过目录联接复用只读依赖、依赖目录位于项目根外而失败；同一源码随后使用 Next 官方 `--webpack` 构建成功。这是审计运行环境限制，不记录为源码编译失败。

## 主要完整审计结论

- 权限版本模型本身已区分 `auth_version`、`permission_version`、全局 RBAC 与项目 ACL，但高权限后台保存、关系编辑和等级轨道仍存在越权或权限残留风险。
- 未确认新旧权限缓存 Key 并存；确认存在旧权限写路由、错误权限术语和多处授权数据更新缺陷。
- 通知模板服务存在，但旧固定文案、直接 Core NATS 发布和 Outbox 之前的队列状态语义仍在活跃调用链中。
- 缓存主路径以版本化 Key 和数据库回源为权威，未确认旧权限缓存 Key；Redis 故障时反滥用限流会退化为单实例。
- NATS/任务存在至少六个旧任务或消费者实现族，可靠 Outbox 与 Core NATS/进程内路径没有完全收口。
- 数据库结构覆盖业务较完整，但存在无用 presence 表、无读写报告快照、重复/低价值索引、旧初始化过渡、缺失查询索引、关系全删全插和多个重复事实/生命周期问题。
- 百万级风险集中在审核队列、通知/未读、评论深分页、目录搜索、日志、热度全量重算、搜索重建、权限列表和文件/资料无界装载。
- 前端构建可用，但没有自动化测试，并确认多处无界装载、分页截断、状态漂移、部分提交和权限能力显示不一致。

## 建议顺序

1. 先修复 `SEC-011`、`SEC-013`、`SEC-039`、`SEC-040`、`SEC-044` 及相关权限/经济回归测试。
2. 收口 Outbox/JetStream，删除 Core NATS 和进程内旧任务入口，并在启用 JetStream 的环境完成故障恢复测试。
3. 修复导入、爬虫、MRPack、文件与 OSS 的事务/补偿和数据生命周期。
4. 修复审核队列、通知、评论、搜索和目录的大数据分页与 N+1。
5. 建立前端测试、CI、Race 和依赖漏洞质量门后，再正式转向大规模前端界面优化。

完整问题证据见 [11_FINDINGS_REGISTER.md](11_FINDINGS_REGISTER.md)，文件级覆盖见 [modules/FILE_COVERAGE_INVENTORY.md](modules/FILE_COVERAGE_INVENTORY.md)，测试证据见 [09_TEST_AND_QUALITY_AUDIT.md](09_TEST_AND_QUALITY_AUDIT.md)。
