# Presence、未读与实时推送

## Presence

认证会话使用不可逆摘要作为 ZSET member。`presence:user:{id}:sessions` 按会话保存心跳时间，`presence:users` 保存用户的心跳时间。批量查询通过 ZMSCORE 读取指定用户并与在线阈值比较。

- 普通心跳默认 60 秒，Redis TTL/在线阈值 150 秒。
- 聊天 Presence 每 20 秒更新，TTL 30 秒并绑定 conversationId。
- 单 Session 登出移除对应 member；全量退出由 Session失效与 TTL 清理。
- PostgreSQL `last_active_at` 默认每 600 秒最多写一次，也会在有意义业务动作/退出时尽力快照。
- Redis 故障使用单进程短期状态，不将每次心跳回退为数据库写入。
- `show_online_status=false` 时，普通资料接口返回隐藏状态。每次用户卡读取通过 PostgreSQL 核对账号状态和在线可见设置，静态统计继续复用缓存。浏览器只合并同一会话正进行的同目标请求，下次打开卡片重新读取。

## 未读计数

`GET /api/v1/me/unread-summary` 是通知和私信共用的未读摘要入口，返回 `notifications`、`messages` 和 `total`。[现有客户端约定](COMPATIBILITY_BOUNDARIES.md)

1. 启用共享未读计数时，在一次 Redis pipeline 中读取全局 epoch 和用户 Hash。读取共享摘要可以观察其他实例提交的增量和删除。单实例本地模式和 Redis 故障可使用有效的进程缓存。
2. 缺失或 epoch 不同则用一条 SQL 从 PostgreSQL 汇总定向通知、广播已读水位与回执、未读私信。
3. 重建 Hash 并设置 TTL。
4. 新消息和通知事务提交后按计数类别增加。单条已读在一条带收件人条件的 SQL 中按公开 ID 写回执，仅在行发生变化时递减。已读或已删除的通知可重复提交。全部已读写入水位后使该用户的缓存失效。增量只作用于同一 epoch 的缓存。
5. 删除或无法确认更新时删除用户 Key，下次重建。
6. 全局通知通过 epoch 懒失效，不对全部用户 SCAN/DEL。

并发“全部已读 + 新消息”以 PostgreSQL read_at 事实为准；缓存失败/漂移时删除并重建，因此不会永久返回负数或把 Redis 当事实。

## SSE 与轮询

`GET /api/v1/realtime/events` 验证服务端会话，只能订阅当前用户。每个订阅最多缓冲 32 个事件，缓冲满时丢弃新事件并计数。已入队的事件和每 20 秒的心跳分别有 10 秒写入时限。写入或刷新到网络失败时立即结束处理并释放名额。跨实例广播使用 Core NATS 主题 `{prefix}.realtime.user.{id}`。

`/ready` 的 `realtimeBroadcast` 在当前连接的订阅重放获得 broker 确认后报告就绪。客户端重连时从未读摘要或聊天游标恢复业务状态。[订阅恢复和就绪](docs/queue-recovery.md#停机及验证)

事件转发与每 20 秒的心跳都以 PostgreSQL 中的当前会话、账号状态及认证版本为准。事件可以先接受缓存中的拒绝提示，心跳直接查权威记录。每次认证查询最多 3 秒。会话失效、查询失败、JWT 到期或达到最长 30 分钟都会关闭流并释放连接额度。

浏览器：

- 单个站点外壳创建 EventSource；后台页断开，恢复可见后重连。
- 2/5/10/30 秒退避，按 SSE event ID 保留最近 200 项去重。
- 重新连接后刷新当前账号的未读摘要、已打开的通知列表和会话列表。已打开的聊天按最近消息的 `after` 游标补读。
- 会话列表只在新消息/未读变化/用户动作和 60 秒兜底时刷新；头部未读兜底降为 5 分钟。
- 发送消息继续使用普通 HTTP，退出登录关闭连接。

## 校准

后台按用户 ID 轮换检查本实例近期服务过的未读缓存。默认每 30 分钟执行一次，单次最多核对 16 位用户，配置的批量值可进一步调低。一条批量 SQL 取得这些用户的事实计数。管理员可向 `POST /api/v1/admin/infrastructure/unread/reconcile` 提交 1–100 个用户公开 ID 做即时重建。指标记录校准次数、漂移次数、未读重建和回源；Redis Flush 后自然按访问重建。
