# 早期数据填充爬虫

## 边界

该编排器仅处理 Modrinth 的 Mod、插件、光影和材质包，内部类型为 `mod/plugin/shader_pack/resource_pack`。它调用现有 Modrinth API 客户端、来源解析、字段映射、导入任务和草稿/审核流程，不抓 HTML，也不复制导入器。

所有导入任务、草稿、项目创建、内容修订和外部来源绑定都以系统服务账号 `autobot`（`autobot@mcmods.cn`）作为业务发起者。管理员手动点击运行时仍在 `requested_by` 保留操作者，`actor_id` 则固定记录实际执行修改的 `autobot`，两者不会混用。该账号没有可用的交互式密码，通过普通登录接口无法登录。

## 候选与质量

每种启用类型先请求 `/search` 的候选总量，再用加密随机数选择合法 offset，按下载量索引读取随机页，避免每次都是第一条。外部唯一键是 Modrinth project ID，`seed_crawler_candidates.external_project_id` 唯一并保留已处理状态。

默认下载门槛为严格 `downloads > 100`，可在后台调整。候选还必须能被现有导入器正常读取，拥有正文和有效 Minecraft 兼容信息；站内已有外部 ID 会标记为 `existing` 而非重复导入。

## 翻译隔离

草稿携带 `importOrigin=seed_crawler_import`。只有这一来源会按站内语言注册表提供 8 语言内容（保留英语原文，为其余目标语言创建 AI 任务）；用户手工 Modrinth、CurseForge/GitHub 或管理员普通导入不会触发自动翻译。每个 `(candidate, locale)` 的最新状态用于展示，普通 `ai_tasks` 保留每次请求与用量事实；失败保留原文和已有译文，不自动重新付费，显式重试见下文。启用自动提交后，提交仍经过正常项目创建、修订和权限链，但 `autobot` 的 `project.no-review/content.no-review` 权限会使其直接批准；代码不会伪造管理员 Claims，也不会把任务静默归到任意活跃用户。

## 可靠性与成本

PostgreSQL 的 run/candidate/ai_tasks 表是任务事实来源，translation 表是最新状态投影。调度使用 advisory lock，Worker 使用租约、`FOR UPDATE SKIP LOCKED`、指数退避和最多 5 次尝试；NATS 不是唯一任务记录。后台可启停（停用阻止自动调度和新 AI 调用，已进行的网络请求不能撤回）、试运行、手动运行，并配置类型、批量、每日限额、间隔、并发、下载门槛、AI 日预算和是否自动提交审核。权限为 `seed_crawler.view/configure/run`，每次配置与运行均审计。

`autobot` 通过现有变量权限系统获得 1000% 的全局限流额度和 1000% 的 `review.submit` 专项额度。该上限仍由反滥用服务统一限制，不存在 Worker 私有的绕过分支。

## 2026-10 审计修复后的实际约束

翻译不再直接调用供应商。每个候选、源文 JSON 哈希、目标语言、模型和有界术语表快照对应一个普通 `ai_tasks` 内容任务（`scope=seed_crawler`）；创建、站点请求/token/费用预留、爬虫 UTC 日 token 预算预留和 NATS Outbox 在同一事务提交。`ai_daily_token_budget=0` 禁用爬虫 AI，配置停用或执行账号停用后不再开始新调用。所有请求仍受内容翻译 worker 的全局并发和输出上限约束。

供应商未提供 usage 的失败保留估算预留，失败和崩溃任务不会自动重新付费；普通 run 恢复读取同一源文时重用原任务。管理员在现有 AI 任务后台显式 retry 会创建新的恢复任务/随机租约身份，重新检查站点、爬虫及原执行账号当前日额度，并保留旧请求及其可能费用。已经完成的 run 保持原状态，不能用旧身份重新发布。这里的幂等保护控制本地重复请求；供应商无幂等承诺时不能保证跨网络故障严格只计费一次。

每次 run 使用随机 `lease_owner`，15 秒续约，过期最多恢复 5 次。候选、任务、草稿、正常项目创建和状态写入均在对应短事务锁定真实 run/token/expiry；旧 worker 的迟到结果保留已返回 usage，但不能落库为成功或创建资源。网络请求在这些发布锁之外执行。暂停不强制撤销供应商已经接收的请求，迟到结果仍重新核对配置和租约。

成功译文写入编辑器现有 `localizations` 字段；自动提交发送相同的标准请求字段，避免仅存放无人读取的 `seedTranslations`。Mod 自动发布的译文记录 AI 来源、源语言及任务 ID，后续人工修订沿用 `human_corrected` 保护。插件、光影和材质包沿用实际 `simple_project_localizations` 模型，该表没有人工/AI 来源字段；来源与 usage 可在关联 seed/AI 任务查阅，不能宣称它拥有 Mod 相同的版本/人工保护模型。

验证使用专用 PostgreSQL 17 数据库和本地可控 HTTP 端点，覆盖并发入队/重复 worker、正常响应、503 未知计费、预算耗尽、替换租约后迟到结果、旧 worker 实际自动提交拒绝和两套请求契约。真实 Modrinth 导入链、供应商连通性、译文语义质量和生产费用没有据此得到验证。部署先停止旧种子 worker，再部署并启动新 worker，避免旧进程继续走直接付费调用通道；本次种子修复没有新增 schema。

每日候选数量在候选领取事务内检查，并按 UTC 日计入已保存草稿/已提交候选和有效租约的 processing 预留；同候选只允许一个 live run 处理。已处理候选不刷新 updated_at，避免重复发现把历史草稿重新计入当天。草稿保存和候选 draft 状态同事务提交，后续自动提交失败仍保留可编辑草稿和已消耗日数量。

升级只保护仍存在的当日旧投影：在覆盖它之前，幂等写入 `scope=seed_crawler_legacy` 的 AI 事实，不发 Outbox、不重新调用。已知输入/输出 usage 原样保留；未知用量单独标注 legacyEstimate，并保守预留当前爬虫日 token 预算及当前配置价格估算费用，余额不足拒绝新请求。估算不作为真实 usage/实际账单。旧投影在过去被覆盖的历史请求无法由现有数据恢复，供应商账单、模型与真实旧费用仍未验证。

## 已完成 run 的草稿翻译恢复

在现有 AI 后台对失败种子任务显式执行 `POST /api/v1/admin/ai/tasks/{id}/retry`（`ai.task.enqueue`）时，仅允许恢复原服务账号名下仍存在、未提交、未过期的种子草稿。创建独立 `stats.kind=translation_recovery` run 和普通 AI 任务，旧 run/candidate 归属与请求费用不改；普通种子调度/崩溃重入不领取此类 run。worker 原子领取新随机 token，复用 15 秒心跳和 5 分钟租约。

入队冻结元数据源哈希、实际草稿公开/内部 ID、payload 哈希与 updated_at。新草稿在保存事务写入 candidate 的 `draftPublicId`。历史缺绑定时，仅当已有草稿创建/最后保存确实位于原 completed run 的 started_at/finished_at 区间内且归属、类型、外部键一致，才能锁定并补绑定；后来编辑或删除重建的旧草稿不能猜测，返回409。

恢复只追加不存在的目标语言到原草稿 `localizations`，保留源文、其他语言及未知编辑字段；已经存在的目标（包括人工译文）受保护。请求中及写入前再核 source/draft/run/token/expiry/actor。期间人工编辑、提交、源文变化、删除、过期、取消或失去租约均失败并保留现有草稿，不复活已删数据。恢复不会创建公开项目或自动提交；管理员仍使用原草稿编辑/提交流程。

同一个失败任务最多创建一个 retry，重复409 `AI_RETRY_EXISTS`；站点、爬虫与原actor日额度不足返回429，不创建恢复事实或外部请求。旧未知 usage 预留保守保留，不能靠 retry 释放。失败/取消的恢复 run 终态和租约清理由现有 AI 扫描每15秒补偿，即使 NATS 订阅暂不可用也执行，不重新向供应商付费。

`seed_crawler_recovery_integration_test.go` 实际使用 ownership 校验的 PostgreSQL 与本地 HTTP：19场景覆盖 completed run 失败译文→显式重试→原owner草稿 GET 三字段中文可见，8重复worker仅一请求、人工其他语言/字段保留且零公开项目；12种迟到保护、未知费用、历史可验证绑定/不可信绑定拒绝、generic伪造scope拒绝、三类预算拒绝及终态清理暂时故障后自动补偿。该证据不验证真实供应商译文质量或真实 Modrinth 导入。
