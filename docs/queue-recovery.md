# 队列监控与恢复

使用有 `admin.config.read` 权限的账号读取 `GET /api/v1/admin/infrastructure/metrics`。下面的 PostgreSQL 和 broker 操作应在对应环境的授权运维连接上执行。浏览器只访问后端。

## 多实例配置生效

保存 NATS 配置时，接收管理请求的实例先准备候选连接，数据库写入成功后切换本地客户端。其他已经启动的 API 和 Worker 继续按各自加载的设置运行，重新启动时从同一数据库读取当前配置。

管理端读取配置时会收到 `revision`，完整 PUT 须原样带回。缺少版本返回 428，其他管理员先完成保存则返回 409。保留草稿，重新读取当前设置并核对差异后再次保存。新增 broker 地址时，分别替换或明确清空已有密码与令牌。更换认证用户名时同样需要决定已有密码。只调整任务或保留原连接地址时可留空以保留原机密。

通过 `GET /api/v1/admin/config/nats` 可以分别核对数据库目标值和接收该请求实例的 `status.enabled`、`status.subjectPrefix`、`status.tasks` 与订阅状态。负载均衡地址的一次成功响应只确认一台实例。变更时使用一位操作者，记录保存前的设置，逐台优雅停止、重启并通过定向管理入口检查实际状态。没有 HTTP 入口的 Worker 通过启动配置、消费者状态及已领取任务逐台核对。

需要暂停某类任务时，确认每个执行实例已经切换，并单独观察切换前已领取的业务租约。暂停期间的投递意图继续保存在 PostgreSQL。恢复时核对积压、最老任务年龄及实际消费进度。切换 broker、stream 或 subject 前，先核对旧消费者与未确认消息。恢复旧设置后按相同步骤让各实例重新加载。

## 日常判断

| 观测项 | 用途 |
| --- | --- |
| `outbox.pending / publishing / awaitingRetry / oldestSeconds` | pending 是全部尚待交付的行数，包含正在投递和等待重试。结合最老任务年龄判断延迟。检查具体任务是否暂停，以及 `nats_outbox.available_at / locked_at / attempts / last_error`。 |
| `outbox.deadLetters`、管理死信列表 | 未人工重放的条数。区分 publish 与 consumer 阶段，并确认业务任务的当前状态。 |
| `queue.connected / jetStream / stream` | `connected` 表示 NATS 传输已连接。stream 给出消息总数、使用字节、实际上下限、保留/丢弃策略及消费者数。允许缺失的样本显示 null。LimitsPolicy 会保留已 ACK 消息，默认的 prefix 通配符也覆盖 realtime 广播。消费者的未交付/待确认数量应读取相应 durable 的 consumer info。 |
| `queue.realtimeReady / recovering` | `realtimeReady` 等待本实例的广播订阅已被 broker 确认。重连时，SUB 重放后的 PONG 到达前保持恢复中。当前连接的重放确认失败时，沿用自动重试。 |
| `queue.taskRuns[code]` | 每实例 active 是正在执行数，finished、failed、elapsedMillis 是进程内累计值。两个样本的时长差除以完成数差可估算这段时间完成任务的平均用时。进程重启会清零。 |
| `process.goroutines / dbAcquired / dbTotal / dbMax`、`redis`、`projectUpdateNotifications` | 对照任务堆积、数据库池占用、缓存命中/降级，以及批量通知的结果。数据库读失败会让指标请求失败。 |

## 磁盘和 stream 容量

应用初次创建的新 stream 用文件存储、LimitsPolicy、DiscardNew，默认 `MaxBytes=268435456`（256 MiB），没有按年龄或条数自动淘汰。该字节数是初始配置值，应按现场事件大小、流量、重放保留需求和磁盘配额选择。管理员可预置覆盖相应 subject prefix 的 stream。应用只读取已存在的 stream，无论重连、重新配置或候选配置回滚，都保留其历史、durable 和限制。

核对 broker 的文件存储预留、账号配额和实际磁盘空间。即使已有 stream 实际只存放少量消息，它的预算仍可能占用 broker 配额。本地同包演练遇到过创建新 stream 被拒，业务由数据库降级完成。JetStream 验收应从专用服务中观察 durable 已成功建立，再用同一个包核对投递、ACK 和已提交的业务结果。[演练记录](governance.md#当前版本与验收)

在 JetStream 集群中，应用新建的 stream 仍采用 broker 默认的单副本。需要容忍节点丢失时，由运维按实际故障域和磁盘容量预置 stream 与副本数，再启动派发。已有 stream 的副本调整应就地评估，保留未处理事件和消费者进度。第二轮用同机三个文件存储节点验证三副本的 leader 停机与重连，跨主机网络分区和生产恢复还需部署验收。[实测边界](governance.md#第二轮真实运行)

`queue.stream.capacityState` 按已配置的字节或条数配额占比中的较高者报告，80% 为 warning，95% 为 critical，达到上限为 full。无限字节且未跨其他阈值时为 no_byte_limit。将这些状态、主机剩余磁盘、JetStream 账号存储配额及 `outbox.oldestSeconds` 接到运维告警。单条新消息可能超出剩余空间，也可能先耗尽整个账号配额。

1. 收到明确的 stream/账号存储容量拒绝时，Dispatcher 把该行退回 failed，保留 event ID 和尝试预算，30 秒后再试，并结束本轮领取。核对 outbox 的 `JetStream capacity unavailable` 错误、broker 已用/可用空间和当前流策略。
2. 记录 stream 的 first/last sequence、字节数、所有 durable 的 pending/ack pending/ack floor，以及死信 subject 尚待恢复的消息数。先修复存储卷或账号配额，或由有权限的运维在原 stream 就地增大上限。后台保存“新建 stream 容量”只影响以后创建的 stream。
3. 到期后原行以同一 ID 投递。核实 PubAck、outbox 状态、消费者进度、业务行及老任务年龄。等待空间期间允许意图积存在 PostgreSQL，同时监测数据库磁盘。若需释放历史占用，应先备份验证、核对所有消费者和死信恢复进度，单独审批保留范围与可恢复边界。换 stream、删除 durable 和全量 purge 都可能跨过尚待处理的任务。
4. 旧流可能配置 DiscardOld、MaxAge 或无限容量，按其实际策略单独评估。队列应用不会迁移或清理旧流。候选连接失败后新建的空流也保留，先核验生产者、消息与 durable 再决定处置。

## 已执行而发布回执未落库

本地处理函数成功、或 JetStream 返回 PubAck 之后，如果 PostgreSQL 的
`nats_outbox.status='published'` 写入失败，原行仍为 `publishing`。
租约过期后另一个 dispatcher 会以同一 event ID 再投递；超过 broker 去重窗口后
消费者仍可能再次运行。保留该行，按 event ID 核对业务记录、邮件回执或文件任务，
让既有租约恢复。开发新的消费者时，把唯一业务键或任务租约与业务写入一起提交。
`TestOutboxReplaysSameEventAfterDeliveryReceiptWriteFailsIntegration` 对两种传输都
验证了两次处理、一个有唯一键的业务记录；每个真实消费者仍需验证自己的幂等语义。

## 死信和人工重放

JetStream 达到 MaxDeliver 时先把原始 v1 envelope 和终态错误持久化到同一流的 `<subjectPrefix>.dead-letter.<taskCode>`，收到独立死信 Msg-Id 的 PubAck 后才 Term 原任务。每个 prefix 有专属 durable recovery consumer，同时最多一条未确认。恢复消费者只将终态证据写入 `dead_letter_events`，数据库操作有三秒截止时间，失败后按一至三十秒的有界退避重试。提交后 ACK。数据库唯一约束 `(event_id,failure_stage)` 保证重复补写保留人工重放记录。恢复数据库记录的过程不会调用任务 handler 或 AI 供应商。

本地 handler 耗尽尝试后由 PostgreSQL 原子保存完整信封和 `consumer:<taskCode>` 阶段。发布阶段失败可通过原 outbox 行恢复。旧版产生的历史记录保留原阶段。若旧 broker 死信缺少元数据，保留 stream 原件供核验，跳过该条自动恢复以便继续处理其他记录。无效 JSON 会以 `invalidEnvelope` 保存，无法通过管理 API 重放。

- `GET /api/v1/admin/infrastructure/dead-letters` 要求 `admin.config.read`。按发布或消费阶段核对事件、对象的当前权限/状态、最近一次业务结果及可能已经完成的外部动作。
- `POST /api/v1/admin/infrastructure/dead-letters/{id}/replay` 要求 `admin.config.write`。发布阶段只重排仍为 `dead` 的原 outbox 记录，保留 event ID。消费阶段核对完整 v1 信封、任务与事件身份后创建新 event ID。记录状态已改变或信封不完整时返回 409 并保留证据；存储失败返回 500。新意图、操作者以及新旧 ID 的审计记录同事务提交。响应 202 表示已入队。
- 若 broker 也拒绝死信，原任务仍在 stream，已耗尽 MaxDeliver 的消息可能需要人工发现。恢复存储后，通过 consumer info 和 max-deliver advisories 核对这批原消息，检查数据库和死信 subject，保全原始信封再决定补录或授权重放。没有应用层自动重新执行这些耗尽的任务。

## OSS 对象删除

对象删除由 PostgreSQL 表 `oss_object_deletion_outbox` 记录。用 `admin.config.read` 读取 `GET /api/v1/admin/infrastructure/oss-deletions?status=dead&limit=50`，核对对象、失败分类和最后一次错误。列表默认展示最新死信，每次最多 100 条。重放前确认目标对象仍应删除，并先处理凭据、授权或存储配置故障。

使用 `admin.config.write` 向 `POST /api/v1/admin/infrastructure/oss-deletions/{id}/replay` 提交工单的公开 ID。服务在锁定工单后重新确认当前操作权限，只允许重放仍为 `dead` 的工单。事务同时重置尝试次数并记录操作者、原失败分类和审计活动。202 表示重新排队，409 表示状态已变化。随后核对该工单是否进入 `completed`，确认 Worker 已完成删除处理。

## SMTP 已受理和状态不明

站内通知和通知邮件使用不同的状态。自动发送通知邮件前，Worker 校验当前收件人状态、已验证邮箱及订阅偏好，在现有 `processed_events` 表预留 `('notification.email', event_id)`，再联系 SMTP。明确拒绝可按原预算重试。对端对 DATA 作出成功终态响应后，QUIT 失败仍算受理。接受状态保存在 `smtp.accepted`，相同 event ID 重投会跳过发送。

若客户端失去 DATA 的最终响应、完成回执写入失败，或 Worker 在预留后退出，`smtp.attempt` 会保留。自动重投无法确认先前受理状况，将保留证据并进入死信流程。授权运维可按 event ID 查询当前回执。

```sql
SELECT event_type, processed_at
FROM processed_events
WHERE consumer = 'notification.email' AND event_id = $1;
```

对应的 Message-ID 为 `<mcmods.事件ID的SHA256小写十六进制@发件邮箱域名>`。联系邮件服务运维核对受理日志。确认外部未受理、业务仍需要发送后，使用单条消费死信的审计重放。确认已受理的事件应保留现有凭据与调查结果。SMTP 接受和最终入箱属于不同阶段。早于本实现发送的邮件无法补推其真实状态。

## 停机及验证

关闭 queue client 或父上下文会取消运行中的任务。等待本地分发协程完成后再关闭数据库。已建立 JetStream 暂时断连时按原配置重连，未获 PubAck 的行保留或按预算转发布死信。没有 JetStream 上下文时使用同一业务处理函数的本地模式。升级所有会运行 Dispatcher/Worker 的实例，使其均使用相同的任务开关、并发和邮件回执。回滚前先停止接收新任务，核对在途邮件、已接受及待核实回执，并按源码、数据库代际和已验证快照配对恢复。[发布和回退](../README.md#数据库安装发布和恢复)

跨实例 realtime 广播采用 Core NATS 的即时语义。订阅恢复前错过的广播需通过业务状态查询确认。`/ready` 的 `realtimeBroadcast` 反映本实例当前连接的订阅确认情况。

在隔离环境中执行以下门禁。数据库测试需要设置 `MCMODS_RUN_DB_INTEGRATION=1` 和专用 `MCMODS_TEST_DATABASE_URL`。外置 broker 测试使用 `MCMODS_RUN_NATS_INTEGRATION=1` 与回环 `MCMODS_TEST_NATS_URL`。未设置 opt-in 的 SKIP 不计作通过。部分测试自行启动真实 file-backed NATS 用于停机/重连。SMTP 协议测试使用回环接收器，邮件回执用真实数据库和可控发送函数。

```sh
go test -race ./internal/queue -count=1
go test ./internal/httpapi -run 'TestEmailReservation|TestTEST018|TestInfrastructureDurableMetrics|TestTEST027'
go test ./internal/mailer -run 'TestSMTPDistinguishes|TestTEST024'
```

整体状态机与任务生产、消费和业务约束见[任务设计](../OUTBOX_JETSTREAM_DESIGN.md)。
