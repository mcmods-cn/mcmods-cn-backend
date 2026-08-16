# 用户操作记录保留与清理

## 动作与默认策略

权威动作来自 `activity_actions`：`edit/create/view/delete/claim/download/upload/purchase/transfer/checkin/use`。接口只接受该白名单，不接受任意字符串。

升级后的全局自动清理默认关闭。默认策略是：

- 全局 `enabled=false`，执行周期 60 分钟。
- 未覆盖动作 `enabled=false`、`allowDelete=false`、365 天、每批 1000。
- `view` 预置为允许删除、30 天、每批 2000，但只有管理员显式打开全局开关后才执行。
- 清理预览/执行审计位于独立 `activity_cleanup_runs`，不在活动事件表中，因此不能被同一次清理删除。
- 现有动作枚举没有独立“安全/权限”动作；未来新增时必须先加入后端白名单，并以 `allowDelete=false` 上线。

每个动作可覆盖：是否启用、是否允许删除、保留天数（1-3650）、批量（100-5000）。执行周期允许 10-1440 分钟。

## 自动清理

- worker 周期读取配置，并以 PostgreSQL session advisory lock 防止多实例并发执行。
- 每个动作独立计算截止时间，按 `occurred_at,id` 取有限 ID 后分批删除；每批单独提交，不使用大事务。
- 自动任务记录开始、完成/失败、删除数、结束时间和错误文本。
- 相同周期内已有自动运行记录时跳过，失败可在后续周期安全重试。
- 正在写入的新事件受快照/时间条件保护；活动表的用户统计触发器只在插入时运行，删除不会扣减持久统计。

## 手动筛选

管理员可组合：

- 最多 50 个用户公开 ID；后端参数化查询解析为内部 ID。
- 最多 100 个“对象类型 + 对象公开 ID”；服务器显示类型 `server` 在后端映射到 `public_routes.minecraft_server`，避免类型/数字 ID 冲突。
- 多个对象类型。
- 多个动作白名单值。
- RFC3339 起止时间，必须含时区；后端转 UTC。开始边界包含，结束边界不包含；可只设置一侧。

所有数组去重、限制长度并使用 pgx 占位符/`ANY`，不拼接用户名、ID、动作或字段名。开始晚于结束会被拒绝。

## 预览、确认和执行

1. `POST /api/v1/admin/activity-logs/cleanup/preview` 保存不可变过滤快照和 `snapshotBefore`。
2. 响应包含命中总数、按动作/对象类型/用户分组、时间范围、少量只含数值关系的样例、条件摘要、15 分钟一次性确认 token 和危险范围标记。
3. 管理员必须提交同一 preview ID、token 以及精确短语 `DELETE <命中数>` 到 `POST .../execute`。
4. 后端校验同一操作者、未过期、状态为 preview、常量时间 token hash、动作策略仍允许删除。
5. 按有限批次删除并持续更新独立审计记录；不提供 GET 删除接口。

权限使用现有 `log.read`/`log.write` 后端中间件。前端使用 Authorization Bearer token，不依赖跨站 cookie；既有 CORS/认证链继续阻止跨站伪造。对象和用户均由服务端重新解析，不能通过隐藏按钮或前端用户名越权。

## 索引

复用已有：

- `(user_id, occurred_at desc, id desc)`；
- `(object_type_id, object_route_id, occurred_at desc, id desc)`；
- `BRIN(occurred_at)`。

新增 `(action_id, occurred_at, id)` 支持动作级保留和预览。没有重复新增单列时间/用户索引。`scripts/query-analysis.sql` 提供真实数据环境的计划分析入口；只有执行计划证明需要时才继续加索引。

## 大数据量与恢复

- 自动/手动都按 ID 子查询限制批量，避免长锁和超大 WAL 峰值。
- 预览使用快照上界，执行不会误删预览之后新写入的事件。
- 清理不可恢复；生产执行前应按现有备份策略保留数据库备份。
- 用户累计统计、按日趋势、内容事实和经验交易不位于清理目标表，清理后无需扫描全量日志来维持正确性。

## 动作摄取稳定性（generation 70）

- `view` 是 best-effort 信号：经五分钟共享节流后进入严格有界队列；队列满载时丢弃并计数，不阻塞业务请求，也不再创建无上限内存 overflow。
- `create/edit/delete/claim/download/upload/purchase/transfer/checkin/use` 先写入 `activity_event_outbox`。只有数据库确认后才算成功入队，应用重启不会丢失已确认的 Outbox 事件。
- 消费器按固定批次执行 `FOR UPDATE SKIP LOCKED`，原始事件插入和 Outbox 删除位于同一事务，多实例不会重复搬运同一事件。
- 失败使用 250ms 到 30s 的指数退避并带抖动；每次 CopyFrom 不超过配置批量，数据库恢复时不会一次提交整个积压。
- 动作摄取使用独立连接池。生产连接总数必须把 API 池和动作池一起计入 PostgreSQL `max_connections` 预算。
- `GET /api/v1/admin/activity-logs/ingestion` 需要 `log.read`，返回队列深度、Outbox 积压与最老时间、丢弃/失败/重试计数和连接池等待指标。
- 多实例必须设置正确的 `APP_REPLICA_COUNT` 并启用 Redis；否则启动配置校验失败，避免视图节流在每个实例重复计数。
- 安全、权限、资金等权威审计仍在业务事务和专用审计表内完成；通用动作 Outbox 是活动/统计流，不替代领域账本。
