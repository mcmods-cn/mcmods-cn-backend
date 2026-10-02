# 通用 Outbox 与 JetStream 设计

## 事务 Outbox

第 73 代迁移扩展 `nats_outbox`：`event_id`、`event_type`、`schema_version`、`occurred_at`、`trace_id`、`status`、`available_at`、`locked_at/by`、`attempts/max_attempts`、`last_error` 和时间字段。

业务调用 `queue.EnqueueTx`，必须传入现有 `pgx.Tx`。Dispatcher 使用 `FOR UPDATE SKIP LOCKED` 领取批次，状态为 `pending -> publishing -> published`；失败按有界指数退避回到 `failed`，超过次数写 `dead_letter_events` 并标记 `dead`。过期 `publishing` 可重新领取，多 Dispatcher 不会无界重复处理同一行。

事件信封：

```json
{
  "event_id": "全局唯一 ID",
  "event_type": "user.followed",
  "schema_version": 1,
  "occurred_at": "RFC3339",
  "aggregate_type": "user",
  "aggregate_id": "公开或稳定对象 ID",
  "trace_id": "可选",
  "payload": {}
}
```

## JetStream

- Stream：`NATS_JETSTREAM_STREAM`（默认 `MCMODS_TASKS`），FileStorage，Subjects 为 `{NATS_SUBJECT_PREFIX}.>`。
- 发布：`event_id` 写入 `Nats-Msg-Id`；只有收到 PubAck 后 Outbox 才标记 published。
- 消费：任务 QueueGroup 同时作为 Durable 名称，ManualAck + AckExplicit，AckWait/MaxDeliver 可配置；实际 consumer 从 AckWait 开始指数退避并在 30 分钟封顶。
- 成功：业务事务提交后 ACK。
- 失败：NAK 触发重投；达到最大投递次数写死信 Subject/记录并 Term。
- 幂等：`processed_events(consumer,event_id)` 主键、通知 `source_event_id` 唯一索引以及任务状态机共同防止重复副作用。
- 消费达到 MaxDeliver 时，先通过 DeadLetter sink 持久化 `dead_letter_events`，成功后才 Term；持久化失败则继续 NAK，避免死信证据丢失。

## Core NATS 降级

新环境默认启用 JetStream。显式未启用或连接不可用时，Outbox/数据库任务表仍为事实来源，Dispatcher 可调用已注册的本地 Handler；Core NATS 不作为可靠任务的第二协议。实时 SSE 广播允许丢失，浏览器重连后重新读取未读摘要和增量消息。

## 已迁移流程

- 用户关注通知与同一关注事务提交。
- AI、蓝图转换、Mod 元数据导入和已有 Mod 导入导出任务。
- 系统、评论、模板、申请和私聊邮件通知的投递意图。
- 仍直接发布的周期 Worker 路径只唤醒已存在的 PostgreSQL任务；不把 NATS 当唯一事实。

蓝图 Job 另以数据库 owner lease 约束业务执行：两分钟 lease、三十秒 heartbeat、默认三次 attempt；启动与每分钟扫描把过期 processing 原子转 queued 并写新的恢复 Outbox。活跃重复 delivery 必须 NAK，旧 owner 不能完成新 lease；normalize 与 convert 的主体状态边界相互独立。

## 故障窗口验证

- 业务事务回滚：Outbox 行不存在。
- 提交后进程崩溃：行保持 pending，其他 Dispatcher 可投递。
- PubAck 后状态更新前崩溃：允许重复，Msg-Id 和消费者幂等吸收。
- 消费成功 ACK 前崩溃：重投命中 `processed_events`/业务唯一键，无重复通知。
- NATS 不可用：Outbox 保留并退避；恢复后继续。
- 最大失败：死信可由具备管理权限的运维查看；重放必须产生新的受审计操作。
- 默认门禁：测试进程启动官方 file-backed JetStream，实际验证 consumer/Server 重启、离线积压、断连重连、双 Dispatcher 和发布/消费死信恢复；无需环境变量或预装服务。

## 部署与回滚

管理员可通过 `GET /api/v1/admin/infrastructure/dead-letters` 查看，通过 `POST .../{id}/replay` 在审计事务中重新排队；分别要求 `admin.config.read/write`，普通用户不可访问。

发布者和消费者使用不同的最小 Subject ACL，浏览器不得直连 NATS。关闭 JetStream 后保留 Outbox Dispatcher 的数据库扫描/本地处理模式；不得同时运行旧、新两个会产生副作用的消费者。
