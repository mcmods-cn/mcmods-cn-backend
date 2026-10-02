# AI / 业务内容翻译审查记录（2026-10-02）

## 版本与证据范围

后端基线 `95694ccff038a28a3e6126b05e3e7c5037bc7854`，工作分支
`codex/full-audit-20261002`。结论对应本次工作区修复，最终文件指纹由审查台账记录。
本报告审查业务翻译、通知翻译及通用管理员翻译任务；前端静态八语言词条质量与
浏览器验收由前端专项记录另行证明。正式运维契约见
[AI 翻译可靠性](../../ai-translation-reliability.md)。

只使用专用回环 PostgreSQL 18、随机隔离 Schema、合成用户/内容/密钥及本地
`httptest` HTTP 服务；没有读取生产数据库、外发真实内容或执行付费 AI 请求。
小型持久化夹具保留所测的 PostgreSQL 锁/事务/JSON 语义，但不替代完整 Schema
迁移门；真实队列/数据库全库门由根执行者记录，早先含编译/夹具失败的整门不能
记为全绿。PASS 仅覆盖列出的断言，不证明生产健康或真实供应商语义质量。

## 实际数据链与权限

1. 公共内容 GET 只返回已批准译文及语言回退，不自动调用 AI。内容/通知 POST
   要求认证、对象读取/操作权限及正用户日额度；通用后台 POST 要求
   `ai.task.enqueue`。缓存命中也要对象权限，前端按钮不替代后端控制。
2. 入队保留源语言/版本/字段快照、目标语言、目标译文版本和请求者。并发键包含
   实体类型与内部 ID；不同表相同数值 ID 不能复用任务。活动通用任务另包含
   供应商/模型/完整源 Payload 指纹；通知聚合改变后完整源 Payload 不再复用旧任务。
3. 用户额度预留、`ai_tasks` 与 `nats_outbox` 在同一事务提交。queued 是可靠持久化
   接收，不表示供应商成功。原直接 NATS 发送与多套入队路径已统一。
4. worker 原子 queued/retrying → running，`started_at` 为执行代次。业务写、用量和
   终态更新核验代次；写业务结果持任务行锁。崩溃 running 超过 15 分钟后恢复器
   有界 100 条领取并重新入队，旧执行者不能改新执行。最多三次供应商请求尝试。
5. 构建受限字段 JSON 请求，源文/响应都当作数据；HTTP 上下文超时最多 600 秒。
   每次供应商请求前原子执行系统 site/provider/model/user 日历 hour/day/month Quotas，
   先写保守账目；没有预算不调用。响应后无论结果合格与否均结算已报告正用量。
6. 严格解析和字段/保护 Token 检查通过后，重新核验对象生命周期、源文版本及
   目标译文版本。人工/人工修订优先，AI 迟到结果不能覆盖。内容审核设置异常时
   保守要求审核；正常 pending/approved 语义沿用原配置。
7. catalog 写 content revision/change request，community 在 active+approved 源行锁内
   保存，通知复核接收人/种类/title/body/sourceLocale 后保存；成功才将任务 completed。
   B015 聚合通知在同一事务删除旧译文缓存，避免标题/正文变化后显示过期译文。
8. publisher dead 原来让 queued 永久停留且在 AI 页面不可见。A031 增加脱敏
   `delivery_failure` 与双权限显式 `retry-delivery`：只允许没有执行历史/账目的 queued
   任务，最新事件必须 dead 且未 published。复用原事件、审计、死信标记同事务；
   不自动无限重发，也不恢复可能已经计费的 consumer failed。

## 已实施修复与费用边界

| ID | 结果 |
| --- | --- |
| OCT02-A-001 | 逐项字段匹配、重复/缺字段/空译文拒绝；保护简单变量、代码和原 URL；截断不得发布 |
| OCT02-A-002 / A-003 | 源/目标版本、人工译文与生命周期保护；通知/社区写入重新核验当前源快照 |
| OCT02-A-004 | 执行代次 fencing、有界恢复及每任务最多三次供应商请求 |
| OCT02-A-005 / A-006 | 完整 prompt+最大输出预留，实际失败用量仍结算，未知用量继续占预留；执行原配置 Quotas |
| OCT02-A-007 | HTTP 超时、端点限制、错误不回显 URL/供应商响应；共享安全客户端禁环境代理 |
| OCT02-A-011 | cached 资源译文仍检查读取权限 |
| OCT02-A-017 | 设置/审核配置沿事务 queryer 读取，避免 MaxConns=1 再借连接 |
| OCT02-A-031 | publisher dead 可见且受权限手动恢复，并发只接受一次/审计失败回滚 |
| OCT02-A-034 | AI 配置读取失败 503，保存不得覆盖无法解封的旧密钥；同事务串行补隐藏密钥与轮换 |

预算账目沿用 `ai_task_logs.event=provider_request_usage`，`task_id=NULL` 防任务删除
级联丢失预算事实；Payload 只保留必要 ID/模型/请求状态/用量数值，不保存原文或密钥。
reserved 与 usage_unknown 继续计算保守预留，settled 只表示供应商报告了正 Token 用量。
价格按管理员配置 CNY 估算；既有价格字段没有币种元数据，必须人工核对，不能把 USD
数值当成 CNY，也不能把无用量/零价格当免费。最近 30 天统计不影响 31 天月的预算；
当前统一日志保留不清理这些账目。尚未新增预算账目归档，容量增长需运行方监控。

真实供应商不支持本项目端到端幂等协议，响应已计费但进程未持久化、或 broker 已接受
但 publish ack 丢失，仍有重复风险。CAS、执行代次、最大尝试和共享预留限制风险，
不承诺只调用/只计费一次。本文没有真实供应商账单/汇率或额度准确性运行证明。

## 已实际执行的失败场景

以下均为真实核心实现加确定性本地供应商；PG 项使用实际 PostgreSQL。主要最终
定向命令为 `go test -race ./internal/httpapi -run '^(TestOCT02AI|TestAITaskRecovery|TestAITaskAndOutboxCommitOrRollbackTogether$|TestNotificationTranslationUsesSharedTransactionalAITaskOutbox$|TestContentTranslationUsesSharedTransactionalAITaskOutbox$)' -count=1 -json`。
完整命令/版本/退出码存根交付的 `evidence/`；工作日志不包含凭据。

| 场景 | 验证与范围 |
| --- | --- |
| 正常响应与落库 | `TestOCT02AIWorkerClaimAndNotificationPersistenceIntegration`：四 worker 一次请求、完成状态和通知保存，PASS |
| HTTP 超时/断连 | `TestOCT02AIProviderTimeoutAndDisconnect`：Context 截止/受控断连错误，PASS；不代表全部停机故障 |
| 429/500/无效 JSON/截断 | `TestOCT02AIProviderFailuresPreserveUsageWithoutEchoingResponse`：保留 usage、拒 length/max_tokens、不回显敏感响应，PASS |
| 缺字段/空译文/重复或额外字段/坏保护 Token | `TestOCT02AITranslationMatchesEverySourceField` 与 RejectsAmbiguousJSON，PASS；非完整 ICU 或所有 Markdown 解析 |
| 部分批次结果 | 少字段会拒整批，保留旧内容而非部分误发布；纯结果验证 PASS，不宣称有逐项部分成功状态机 |
| 任务去重/源 Payload 改变 | `TestOCT02AIAdminCreateDeduplicatesConcurrentSourceSnapshotsIntegration`：八竞争一新七复用，变源另建，PASS |
| 多 worker/迟到旧执行 | WorkerClaim 与 ExecutionFencing：原子领取/旧代次失败不能改新 running，PASS |
| 处理中源文改变/人工目标 | CatalogProtectsHumanTargetAndChangedSource：直接持久化源版本不匹配及 human_corrected 保留，PASS；不是整条真实浏览器进行中交互验收 |
| 资源删除 | ExecutionFencingAndDeletedPost：迟到译文不恢复 deleted post，PASS |
| 取消 | HTTP Context 取消与旧执行所有权失效已测；本项目没有独立 AI 管理取消端点，本项没有该入口的完整 E2E，NOT_RUN |
| 崩溃恢复/重复恢复 | `TestAITaskRecoveryRequeuesOrphansAndStaleRunsOnce` 与计划索引：有界领取、再次恢复不重复，PASS；不是杀真实供应商进程后的账单证明 |
| 预算耗尽/多请求竞争 | ProviderBudgetConcurrentAdmission：八竞争只一预留；InvalidResultStillConsumesSupplierUsage：错误响应也占 usage/下一请求不调用，PASS |
| 无 usage/结算时 Context 取消 | UnknownUsageKeepsReservation：保留 usage_unknown 预留，PASS |
| 缓存权限 | ResourceTranslationRequiresResourceViewEvenForCachedResult：无权限仍拒缓存译文，PASS |
| publisher dead/重复重试/有账目拒绝 | A031 五项 PG 回归：GET 脱敏 metadata、八并发一成功、11 拒绝分支、双权限、审计 rollback，PASS |
| 密钥读取故障/配置并发/单连接 | A034 五项：旧 GET200/PUT200 覆盖 raw 的真实 RED，修复503/raw不变；Max1、normalized code重复、closed pool、controlled rotation，PASS |

A031 原行为红日志 `backend_a-ai-delivery-red.log`；A034 首次夹具缺设置列的测试失败
不作为旧丢密钥复现，修补真实列后的 `backend_a-ai-settings-authoritative-red.log`
才证明旧 GET/PUT 都 200 且 raw 被覆盖。最终绿色日志分别为
`backend_a-ai-delivery-green.log`、`backend_a-ai-settings-final-green.log`，以及最终 AI
合并组 `backend_a-ai-report-final-race.jsonl`：30 个 top-level 用例 PASS、0 SKIP，
退出 0，4.115s。根全库门/前端 E2E 结果由汇总另列。

## 尚未验证与部署

没有真实供应商连通性、语言识别/语义质量、术语一致性、供应商价格、生产账目、
全局长时间吞吐/日志容量数据。保护 Token 的结构检查不能替代完整 ICU、Markdown
或人工翻译审核。供应商语言是否准确仅靠本地 JSON 响应无法证明。

本次 AI 修复不新增 Schema。先部署后端，旧前端继续使用原 stats 字段；新前端再
消费可选预算与 delivery_failure，调用新增恢复路由。旧格式并发键任务可以执行，
滚动 producer 升级期间新旧 key 短暂共存可能重复；排空旧队列可减少该窗口。
不批量重译，不回填真实数据。回滚代码会恢复未执行 Quotas、迟到覆盖或吞配置
错误风险，不能描述成等价安全恢复。保留账目、旧任务和人工译文。
