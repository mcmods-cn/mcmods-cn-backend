# 玩家动作摄取与容量边界

## 数据路径

`view` 在请求层按用户和对象进行五分钟节流，随后进入有界内存队列。队列只保存有限数量的可丢弃浏览信号；数据库故障或写入变慢时，队列满载后的浏览动作会增加 `droppedBestEffort`，不会继续占用内存。

其他动作先同步插入 `activity_event_outbox`。后台消费者通过 `FOR UPDATE SKIP LOCKED` 领取固定批次，在同一个 PostgreSQL 事务中将事件批量写入 `user_activity_events` 并删除 Outbox 行。进程在两步之间退出会触发事务回滚，因此不会留下“已删队列但未写原始事件”的状态。

Outbox 是通用站内活动和统计的数据入口。资金转账、权限、安全、审核等领域仍必须在对应业务事务中写专用账本或审计表。

## 过载与恢复

- 内存上限约为 `ACTIVITY_BATCH_SIZE + ACTIVITY_QUEUE_CAPACITY` 条浏览事件。
- 单次数据库写入永远不超过 `ACTIVITY_BATCH_SIZE`。
- 写入失败采用 `ACTIVITY_RETRY_MIN_MS` 到 `ACTIVITY_RETRY_MAX_MS` 的指数退避与抖动。
- 关键动作入 Outbox 最多等待 `ACTIVITY_DURABLE_ENQUEUE_TIMEOUT_MS`。
- 批处理查询和写入最多运行 `ACTIVITY_WRITE_TIMEOUT_MS`。
- API 和动作摄取分别使用 `DB_*` 与 `ACTIVITY_DB_*` 连接池预算。
- 多实例通过 PostgreSQL 行锁协调消费，通过 Redis 共享 `view` 节流状态。

## 监控与告警建议

管理员接口：`GET /api/v1/admin/activity-logs/ingestion`。

建议至少对下列条件告警：

- `durableEnqueueFailures` 或 `flushFailures` 增加；
- `oldestDurableAt` 落后当前时间超过五分钟；
- `durableBacklog` 持续增长；
- `droppedBestEffort` 增长速度异常；
- `emptyAcquireCount`、`canceledAcquireCount` 或 `acquireDurationMs` 快速增长；
- 动作池长期接近 `maxConns`。

进程内累计指标会在重启后归零，Outbox 深度和最老事件时间来自 PostgreSQL，不受重启影响。生产环境应由外部监控定期抓取并保存趋势。

## 分区决策

当前不同动作具有不同保留周期，不能安全地按月份直接删除整个 `user_activity_events` 分区。达到千万级记录前，应在代表性数据集上采集 `EXPLAIN (ANALYZE, BUFFERS)`、表/索引膨胀、VACUUM、WAL 和清理锁等待，再决定：

1. 按动作保留等级拆表后分别按月分区；或
2. 使用时间一级分区并继续在分区内按动作分批删除。

在没有真实执行计划前不引入会破坏现有主键、触发器和差异化保留策略的盲目分区。

## 验证命令

```powershell
go test ./internal/activity ./internal/config ./internal/database ./internal/app ./internal/httpapi
go vet ./...
$env:MCMODS_RUN_DB_INTEGRATION='1'
go test -count=1 -run TestDurableOutboxSurvivesProducerAndDrainsExactlyOnceAcrossWorkers ./internal/activity
go test -count=1 -run TestCurrentUserFeaturesIntegration ./internal/database
```
