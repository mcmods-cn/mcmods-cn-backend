# Presence、未读与实时推送

## Presence

每个认证 Session 使用不可逆摘要作为 ZSET member。`presence:user:{id}:sessions` 保存多个标签页/设备各自的过期分值，`presence:users` 保存用户级最大过期分值，以 ZMSCORE 批量查询多个用户，禁止生产 `KEYS`/频繁 `SCAN`。

- 普通心跳默认 60 秒，Redis TTL/在线阈值 150 秒。
- 聊天 Presence 每 20 秒更新，TTL 30 秒并绑定 conversationId。
- 单 Session 登出移除对应 member；全量退出由 Session失效与 TTL 清理。
- PostgreSQL `last_active_at` 默认每 600 秒最多写一次，也会在有意义业务动作/退出时尽力快照。
- Redis 故障使用单进程短期状态，不将每次心跳回退为数据库写入。
- `show_online_status=false` 时，所有普通接口只返回隐藏状态；用户卡片静态缓存不含实时状态。

## 未读计数

`GET /api/v1/me/unread-summary` 返回 notifications/messages/total。旧通知和消息未读接口保留兼容。

1. 读取进程缓存和 Redis Hash。
2. 缺失/epoch 不同则用一个 SQL（两个标量子查询）读取 PostgreSQL事实。
3. 重建 Hash 并设置 TTL。
4. 新消息/通知事务提交后安全增加；单条已读按 RowsAffected 递减且 Lua 保底为零；全部已读设零。
5. 删除或无法确认更新时删除用户 Key，下次重建。
6. 全局通知通过 epoch 懒失效，不对全部用户 SCAN/DEL。

并发“全部已读 + 新消息”以 PostgreSQL read_at 事实为准；缓存失败/漂移时删除并重建，因此不会永久返回负数或把 Redis 当事实。

## SSE 与轮询

`GET /api/v1/realtime/events` 使用服务端 Session认证，只能订阅当前用户；每 20 秒心跳，慢客户端使用有界缓冲并断开重连。API实例通过 `realtime.user.{id}` Core NATS广播已提交事件。

浏览器：

- 单个站点外壳创建 EventSource；后台页断开，恢复可见后重连。
- 2/5/10/30 秒退避，按 SSE event ID 保留最近 200 项去重。
- 重连后刷新未读摘要；聊天消息用 `after` 游标增量读取。
- 会话列表只在新消息/未读变化/用户动作和 60 秒兜底时刷新；头部未读兜底降为 5 分钟。
- 发送消息继续使用普通 HTTP，退出登录关闭连接。

## 校准

后台按用户 ID 游标每 30 分钟校准 50 个活跃用户（均可配置），一次批量 SQL 获取事实计数，禁止全表长事务。管理员可向 `POST /api/v1/admin/infrastructure/unread/reconcile` 提交 1–100 个用户公开 ID 做即时重建。指标记录校准次数、漂移次数、未读重建和回源；Redis Flush 后自然按访问重建。
