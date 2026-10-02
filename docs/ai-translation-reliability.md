# AI 翻译执行、预算与结果保护

当前实现涉及 `ai_handlers.go`、`ai_task_queue.go`、`ai_translation.go`、
`ai_translation_validation.go`、`ai_request_budget.go` 和内容/通知翻译持久化入口。
本说明对应 2026-10-02 修复；不是生产数据库或真实供应商账单验收证明。

## 入队与执行

公开内容 GET 不创建 AI 任务。内容和通知翻译要求认证、对应操作权限及正的
`user.ai.daily_token_limit`，在 PostgreSQL 事务中预留用户额度并写可靠 Outbox。
额度预留包含完整提示词的保守输入估算、请求开销和模型允许的最大输出。
不再以源文字符数乘二替代输出预算。模型配置改变后，执行时不能提高已有
用户任务的预留输出上限；提示词不再适合预留时，任务失败而不发送请求。

通知活动任务按通知、目标语言、请求者去重。后台通用任务按任务类型、供应商、
模型、调用方并发键、源 Payload 指纹和请求者去重；内容任务保留源版本与请求者
作用域。新建通用任务返回 201，复用活动任务返回 202，同样提供任务 ID 和状态。

执行状态仍沿用 queued / running / retrying / completed / failed。
领取任务时用新的 `started_at` 作为执行代次。完成、失败、供应商用量写入和
业务结果持久化都核验该代次，写业务结果期间持有任务行锁。恢复器把超过
15 分钟的 running 任务转回 retrying；旧执行者的迟到写入不能覆盖新执行。
任务超时最多 600 秒，实际 HTTP 请求遵守其 Context 截止时间。

每个任务最多记录三次供应商请求尝试，包含崩溃后重新领取的请求。供应商没有
幂等请求协议，因此不能保证只调用或只计费一次；崩溃发生在供应商响应与本地
持久化之间时仍可能重发。剩余风险受尝试次数和下面的共享预算约束。

## Publisher 失败与显式恢复

`GET /api/v1/admin/ai/tasks` 及单任务详情返回可选 `delivery_failure`：
只含 publisher 阶段、字符串死信 ID 和 `retryable`，不包含原文或供应商响应。
后台现有任务表可对符合条件的任务调用
`POST /api/v1/admin/ai/tasks/{id}/retry-delivery`，要求 `ai.read` 和
`ai.task.enqueue` 两项现有权限。

恢复只接受 queued、没有执行开始时间/历史且没有已知 Token 或供应商请求账目的
任务，并要求最新 Outbox 是未发布的 dead publisher 记录及未恢复的 publish 死信。
它在同一事务中锁任务和死信、条件重置原事件为 pending、标记死信已恢复并写
安全审计。重复点击或并发恢复只接受一次；审计失败全部回滚。running、retrying、
failed 或已计费/用量未知的 consumer 任务不通过此端点重发。

恢复是管理员显式动作，每轮发布仍受既有 Outbox 最大尝试数约束，不自动不断
复活 dead 事件。供应商尚未被本地记录调用不等于 broker 从未接受事件：发布成功
但确认丢失可能重复投递。复用原 event ID 与 worker CAS/执行代次限制重复处理，
不能据此承诺跨 broker、供应商及进程崩溃的严格一次计费。

## 配置读取及隐藏密钥

管理 GET、总配置概览和通用创建任务在 AI 配置数据库/解封/JSON 错误时返回
503 `AI_SETTINGS_UNAVAILABLE`；只有确实缺少配置行才使用合法 disabled 默认值。
保存配置在事务 advisory lock 内读取当前配置、补回隐藏密钥并更新，避免并发
密钥轮换被旧快照覆盖。读取失败不能保存默认配置覆盖原密钥；相同事务 queryer
支持连接池只有一个连接。规范化后供应商 code 必须非空且唯一，歧义输入返回
400 `AI_PROVIDER_CODE_INVALID`。执行器保守使用 disabled 回退，不发送外部请求；
这不被描述成一次成功的管理员配置读取。

## 系统 Quotas

原 `ai.config.quotas` 现在在每次实际供应商请求之前执行，而不只保存配置。
支持以下作用域与周期；不支持的配置被保存接口及执行器拒绝。

| 字段 | 语义 |
| --- | --- |
| scope=site, subject=default（或空） | 全站全部供应商请求，包括爬虫直接翻译 |
| scope=provider, subject=供应商 code | 对应供应商 |
| scope=model, subject=供应商 code/模型 ID | 对应模型 |
| scope=user, subject=用户公开 ID | 对应已入队用户任务 |
| period=hour/day/month | PostgreSQL 当前会话时区下的日历周期 |
| requestLimit | 周期内尝试数；0 表示该维度不设上限 |
| tokenLimit | 已结算用量加未结算/未知用量保守预留；0 表示不设上限 |
| costLimitCny | 按管理员配置 CNY 价格换算的额度；0 表示不设上限 |

所有作用域使用同一个 PostgreSQL 事务 advisory lock，检查预算与写入请求账目
原子完成。没有调用供应商之前先留下 `ai_task_logs.event=provider_request_usage`
记录。账目 `task_id` 为 NULL，通过 Payload 中的任务 ID 关联，避免删除任务
级联删除预算事实。保留策略不得删除当前最长预算周期内的这些记录，否则
会重置已消费的预算。现有 logs.retention 仅清理 app_logs 等，不清理 ai_task_logs，最近 30 天统计窗口
也不影响当前月预算（含 31 天月）。当前未增加清理任务；管理员需监控日志增长。

账目只保存供应商、模型、用户公开 ID、任务 ID、请求状态、Token 与成本数值，
不保存原文、译文、API Key 或供应商错误正文。三个状态：

- reserved：已经预留，尚未结算，包括进程崩溃遗留记录。
- settled：供应商提供正 Token 用量，成功或译文验证失败均结算该用量。
- usage_unknown：超时、网络失败或没有正用量；保守预留继续计入周期，不能当作免费。

成本单位为百万分之一 CNY。价格必须由管理员维护并使用同一币种；零价格不能
证明请求免费。原价格字段没有币种元数据，已有配置需人工核实是否采用 CNY；不能把未换算的
USD 数值视为同等 CNY。供应商实际账单、汇率和语义质量未由确定性测试验证。
`GET /api/v1/admin/ai/stats` 保留 byStatus / byProvider，并新增 requestBudget，
按供应商、模型和账目状态展示最近 30 天的实际用量与估算预留。

## 输出及内容版本

JSON 可带单一代码围栏，但不能带额外解释、重复 JSON 字段、额外顶级字段或
空 items。输出必须与输入逐项对应，不接受重复/未知 Key、遗漏字段、非字符串
或把非空源文译成空白。校验保存简单插值占位符、原 URL 和代码片段的数量及内容。
这不是完整 ICU/Markdown 语义分析，也不能证明所有语言译文准确；复杂语义和
术语正确性仍需要人工审核。

OpenAI 的非正常 finish_reason 和 Anthropic 的截断 stop_reason 不会发布。
解析或质量失败保留供应商用量，不把 HTTP 200 视为翻译成功。

Catalog 结果在事务中复核源文版本、对象生命周期和目标译文版本。任务期间
出现人工译文或人工修订时，迟到 AI 结果失败而保留人工结果。既有源文编辑
路径继续通过 `invalidateAIDerivedLocalizationsTx` 失效派生 AI 译文，保留人工修订。
Community post 的源版本检查与译文写入在同一事务中锁定 active 且 approved
源对象，软删除或版本变化后的任务不能写入。通知结果持久化重新核验接收者、
通知种类和源文快照，源文变化或权限范围变化后不发布旧结果。

## 网络与错误

AI 端点仅支持 HTTP(S)，拒绝 URL 用户凭据、查询串和 fragment，拒绝私网 IP
字面量，production 拒绝回环端点。共享供应商 HTTP 客户端负责解析地址、私网
阻止和同源重定向。该专用客户端不继承 HTTP(S)_PROXY，受管网络需要供应商
域名直连条件；详细网络边界见 [元数据导入说明](project-metadata-imports.md)。隔离测试的回环端点仅用于 test / development。
供应商非 2xx 响应仅记录状态码；网络错误剥离请求 URL，避免响应正文或 URL
携带的敏感信息进入任务错误和日志。

## 验证与部署

无需 Schema 变化，已有 PostgreSQL 表和列足够。本次后端修复先部署即可；
前端新增预算表格消费可选 requestBudget，旧客户端继续使用原统计字段。
回滚代码会恢复不执行系统预算、遗漏/截断结果发布和迟到写入风险，不能视为
等价的安全恢复。保留已有任务、人工译文及预算账目，不自动重译线上内容。

复现定向回归：在身份已确认的专用 PostgreSQL 测试库设
`MCMODS_TEST_DATABASE_URL`，运行 `go test ./internal/httpapi -run 'TestOCT02AI' -count=1`。
测试创建独立随机 Schema，覆盖本地 HTTP 正常/错误/断连/超时/截断响应、精确字段
与保护 Token、共享预算竞争、未知用量、任务争用、旧执行者写入、源文变化、
人工译文保护与删除。仅 HTTP 供应商为确定性替代服务，数据库使用实际 PostgreSQL。
没有使用生产数据库，没有执行真实付费 AI 请求。

Content-translation deduplication includes the subject type as well as its internal ID, source language/version, target language and requesting actor. Mod, blueprint, skin and catalog tables have independent ID sequences, so an equal numeric ID cannot reuse another subject type's task. Existing queued tasks remain executable; during a rolling producer upgrade an old-format key and a new-format key may briefly coexist. Source/target publication fencing and quota reservation still apply, but supplier calls are not claimed to be strictly once-only. Draining already queued translations before upgrading producers reduces this compatibility window.
