# 通用 Outbox 与 JetStream 设计

## 事务 Outbox

第 73 代迁移扩展 `nats_outbox`：`event_id`、`event_type`、`schema_version`、`occurred_at`、`trace_id`、`status`、`available_at`、`locked_at/by`、`attempts/max_attempts`、`last_error` 和时间字段。

业务调用 `queue.EnqueueTx`，必须传入现有 `pgx.Tx`。Dispatcher 使用 `FOR UPDATE SKIP LOCKED` 领取批次，状态为 `pending -> publishing -> published`；失败按有界指数退避回到 `failed`，超过次数写 `dead_letter_events` 并标记 `dead`。过期 `publishing` 可重新领取，多 Dispatcher 不会无界重复处理同一行。

发布确认、失败和死信写入均同时校验 `status='publishing'`、`locked_by` 和本次 `attempts`。租约过期后，旧 Dispatcher 的迟到结果不得改写新领取者的任务；受影响行数为零也不计入成功指标。状态持久化失败会返回错误，不能把数据库失败当作已记录重试。

本地降级同样遵守任务启用状态和配置超时，不执行明确禁用的任务。管理 API 的 NATS 地址移除认证、路径、查询和片段，连接错误只返回脱敏摘要；服务器内部错误仍由受控日志诊断。

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

- Stream：`NATS_JETSTREAM_STREAM`（默认 `MCMODS_EVENTS`），FileStorage，Subjects 为 `{NATS_SUBJECT_PREFIX}.>`。
- 发布：`event_id` 写入 `Nats-Msg-Id`；只有收到 PubAck 后 Outbox 才标记 published。
- 消费：任务 QueueGroup 同时作为 Durable 名称，ManualAck + AckExplicit，AckWait/MaxDeliver 可配置。
- 成功：业务事务提交后 ACK。
- 失败：NAK 触发重投；达到最大投递次数写死信 Subject/记录并 Term。
- 幂等：`processed_events(consumer,event_id)` 主键、通知 `source_event_id` 唯一索引以及任务状态机共同防止重复副作用。
- 消费达到 MaxDeliver 时，先通过 DeadLetter sink 持久化 `dead_letter_events`，成功后才 Term；持久化失败则继续 NAK，避免死信证据丢失。

## Core NATS 降级

未启用 JetStream 时，Outbox/数据库任务表仍为事实来源。Core NATS 只唤醒本地注册 Worker；即使唤醒丢失，Dispatcher/Worker 周期扫描会找到未处理行。实时 SSE 广播允许丢失，浏览器重连后重新读取未读摘要和增量消息。

## 已迁移流程

- 用户关注通知与同一关注事务提交。
- AI、蓝图转换、Mod 元数据导入和已有 Mod 导入导出任务。
- 系统、评论、模板、申请和私聊邮件通知的投递意图。
- 仍直接发布的周期 Worker 路径只唤醒已存在的 PostgreSQL任务；不把 NATS 当唯一事实。

## 故障窗口验证

- 业务事务回滚：Outbox 行不存在。
- 提交后进程崩溃：行保持 pending，其他 Dispatcher 可投递。
- PubAck 后状态更新前崩溃：允许重复，Msg-Id 和消费者幂等吸收。
- 消费成功 ACK 前崩溃：重投命中 `processed_events`/业务唯一键，无重复通知。
- NATS 不可用：Outbox 保留并退避；恢复后继续。
- 最大失败：死信可由具备管理权限的运维查看；重放必须产生新的受审计操作。

## 部署与回滚

管理员可通过 `GET /api/v1/admin/infrastructure/dead-letters` 查看，通过 `POST .../{id}/replay` 在审计事务中重新排队；分别要求 `admin.config.read/write`，普通用户不可访问。

发布者和消费者使用不同的最小 Subject ACL，浏览器不得直连 NATS。关闭 JetStream 后保留 Outbox Dispatcher 的数据库扫描/本地处理模式；不得同时运行旧、新两个会产生副作用的消费者。
