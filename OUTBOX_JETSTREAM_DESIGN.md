# 从业务事务到任务执行

业务写入和投递意图同属一个 PostgreSQL 事务。生产者把现有 `pgx.Tx` 传给 `queue.EnqueueTx`。打开 NATS/JetStream 后，后端向 broker 分发任务。无 JetStream 上下文时，Dispatcher 使用已注册的本地处理函数。Core-only 实例仅为实时广播建立 Core 订阅，本地任务从数据库领取，避免在另一实例向同一 subject 发布时额外执行。

## 投递与确认

1. `internal/queue/events.go` 定义 v1 信封。必填字段为 `event_id`、`event_type`、`schema_version`、`occurred_at`、`aggregate_type`、`aggregate_id` 和非空 `payload`，`trace_id` 可选。业务主键在各任务中单独定义。`nats_outbox.subject` 保存任务代码，运行时配置把它映射到 broker subject。
2. `OutboxDispatcher` 每秒扫描一次。单条 `UPDATE ... FOR UPDATE SKIP LOCKED` 把到期的 `pending/failed` 变为 `publishing`。每次领取有 owner 和递增的 attempt。工作期间每隔租期的三分之一续约，默认租期五分钟。结算同时匹配 owner、attempt 和状态。实例退出后，其他实例可按过期租约恢复。
3. JetStream 发布带 `Nats-Msg-Id=event_id`，收到 PubAck 后才标为 `published`。状态提交前中断可再次发布同一个 ID。新 stream 的消息去重窗口为十分钟，长期重复仍由业务唯一键或执行租约处理。已建立 JetStream 后暂时断连会按发布失败恢复，当前尝试不会又改为本地执行。未建立 JetStream 时，本地分发成功执行 handler 后标为 `published`。
4. 每个 task 的本地分发、broker 订阅及直接执行同类任务的数据库恢复扫描共用 `queue.Client.RunTask` 的每实例并发和超时。暂停后新的意图留在数据库，恢复队列扫描仍可修复失效租约或清理对象。本地 Dispatcher 同时最多运行 64 个任务，一次扫描最多领取 64 个可启动的任务。多实例上限随实例数增加，业务行锁和唯一键保护跨实例写入。
5. `task_subscription.go` 将队列组作为 durable 名称，显式 ACK。收到任务后，排队和执行期间按首个确认窗口的三分之一发送 `InProgress`。回调向任务处理器交付前，最多有 1024 条、默认 64 MiB 消息数据占用该订阅的应用侧名额。单件超过字节预算时独占额度。NATS 客户端连接另有接收缓冲。handler 返回成功后 ACK。失败按投递次数发延迟 NAK，指数退避上限 30 分钟。应用发出的 NAK 和 outbox 常规失败在各自退避间隔内添加最多 10% 的抖动。默认 AckWait 五分钟、MaxDeliver 八次。关闭客户端会取消执行中的上下文，未确认项由 broker 或数据库租约恢复。
6. 常规发布失败、本地处理失败按尝试数退避，耗尽后分别形成 `publish`、`consumer:<task>` 死信。明确的 broker 容量拒绝保留原 event ID 和尝试预算。JetStream 消费耗尽先把终态证据持久化到单独的死信 subject，再 Term 原消息。独立 durable 从这个 subject 写回 PostgreSQL。[容量与死信恢复](docs/queue-recovery.md)

## 已注册的业务任务

默认并发为每实例值，超时单位为秒。`GET /api/v1/admin/config/nats` 的顶层字段是数据库保存的设置，`status` 是本次请求命中实例的运行状态。多实例变更按[队列恢复说明](docs/queue-recovery.md#多实例配置生效)核对。所有任务共享上述传输状态机、取消传播、死信处理和运行指标。下表的身份和状态属于业务本身。

| 任务 | 生产事务与 payload | 处理入口、默认并发 / 超时 | 业务保护与恢复 |
| --- | --- | --- | --- |
| `ai` | 申请/管理/重试写 `ai_tasks`，`enqueueAITaskTx` 传 `taskId/taskUid/taskType`，用户翻译同时保存额度预留 | `AIWorker.handleTask`，2 / 300 | queued 或 retrying 按 `started_at` 领取 running，过期 15 分钟可恢复。供应商调用和写结果前核对当前用户、权限、来源及执行版本。取消或被接替的 attempt 不能发布。未知供应商结算需核账。[翻译执行账目](docs/ai-translation-reliability.md) |
| `notifications` | 关注、评论、系统、私聊等生产事务用 `enqueueNotificationTaskTx` 写 action、收件人及模板参数 | `NotificationWorker.handleEvent`，8 / 300 | system/direct 用 `notifications.source_event_id` 去重，聚合通知另有 `processed_events`。邮件读取当前启用账号、验证邮箱与订阅偏好，同一事件保留 SMTP 接受或待核实回执。 |
| `mod_export_import` | 创建、恢复及重试 `catalog_import_jobs` 用 `enqueueModExportAttemptTx` 写 `jobId` | `ModExportWorker.handle` → `importModExportJob`，2 / 3600 | queued → validating/importing → ready、partial、failed 或 cancelled。run_token 和 15 秒心跳约束写入，五分钟过期可恢复。导入完成与公开发布有独立状态。执行/审核分别核对项目、版本、授权方式及实际人员。[导入策略](docs/audit-catalog-import-policy-upgrade.md) |
| `mod_metadata_import` | 元数据预览请求及恢复把 `mod_metadata_import_jobs.public_id` 作为 `jobId` 写入事务 | `ModMetadataImportWorker.handle` → `runModMetadataImport`，4 / 180 | queued → running → completed/failed。写结果匹配 `started_at`。每分钟检查遗漏的意图和五分钟过期任务，预览结果保留在发起人的任务记录。 |
| `blueprint_convert` | `enqueueBlueprintJobTx` 同时创建 normalize/convert 业务任务及事件，payload 为内部 `jobId` | `BlueprintWorker.handleMessage` → `processJob`，4 / 1800 | `locked_by` 与 attempt 保护状态和每次生成的文件，两分钟租约，30 秒续约。启动及每分钟恢复过期 processing。任务或主体失效后走现有对象补偿。[蓝图与日志附件](docs/blueprint-and-comment-lifecycle.md) |
| `comment_log_attachment` | 创建评论或绑定日志时把评论、文件关联、去重的任务行和内部 `jobId` 一起提交 | `CommentLogAttachmentWorker.handleMessage` → `processJob`，2 / 300 | `(comment_id,attachment_file_id)`、独立 lock token 和十分钟租约，写入前核对任务和评论附件关联。错误按业务预算退避，恢复扫描亦处理最后一次中断。 |
| `favorite_modpack_export` | 用户基于有期限、带哈希的预检快照创建导出任务、条目和 `taskId` | `FavoriteModpackExportWorker.handle`，2 / 600 | pending → processing → ready/failed/cancelled，下载过期变 expired。按持有人授权，默认十五分钟 token 租期、三次尝试、成品七天过期。取消、重试及孤儿对象使用现有回收流程。[收藏夹导出](docs/favorites.md) |
| `project_update_notifications` | 项目更新与 `project_update_events`、通知任务、内部 `eventId` 在同一事务 | `ProjectUpdateNotificationWorker.handle` → `process`，4 / 300 | 每批锁任务行，用 `next_user_id` 继续，最多 200 人。按当前公开项目、关注开关和活跃账号筛选，收件人与更新事件的唯一键去重。每 15 秒扫待办，业务最多八次尝试。 |

新增任务时，在生产业务的事务调用 `queue.EnqueueTx`，为任务表定义重试和终态约束，在 `queue.DefaultTaskConfigs` 注册代码，然后在 `internal/app/runtime.go` 启动订阅者。若需环境初始配置，同步 `internal/config/config.go`。依赖数据库的定时扫描若直接执行业务，也要使用同一个并发、开关和超时入口。检查事务回滚、重复投递、并发领取、关闭和恢复后的最终业务状态。

## 验证边界

同一回环 PostgreSQL 18.6、Go 1.26.6 及合成数据下，`TestLocalOutboxDeliversNotificationsPastBlockedImports` 在基线 `21724a4` 先排 70 个导入，单槽内的首个 handler 保持运行，再添加一条通知。通知到 3.5 秒仍未执行。修改后一次非 race 运行中，通知在 801 毫秒提交，此时首个导入仍在运行、另 69 项保留待办。时间随一秒轮询相位波动，数值仅代表这次延迟样本。

`TestJetStreamLongHandlerAndRetryDelayIntegration` 的受控长任务跨过三个 300 毫秒 ACK 窗口。基线的 broker 已投递两次且启动两个 handler，修改后只有一次。业务行的幂等保护另由双 Dispatcher、过期租约和实际 Worker 集成测试覆盖。本实验记录合成任务的 handler 次数，生产业务的实际副作用需要对应的业务账目。

同一 broker 和 subject prefix 下，一台实例使用 Core-only、另一台启用 JetStream。`TestCoreOnlyPeerConsumesTasksExclusivelyFromPostgres` 确认原实现会在两个 handler 执行同一 broker 事件。修正后只有 durable 处理该事件，Core-only 实例仍可处理自己领取的数据库行。

运维查看积压、任务耗时、死信及 SMTP 待核实投递时见[恢复手册](docs/queue-recovery.md)。管理员修改连接、凭据及新流初始容量时见[配置说明](docs/nats-settings.md)。
