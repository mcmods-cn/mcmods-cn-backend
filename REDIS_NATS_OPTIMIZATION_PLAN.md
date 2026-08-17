# Redis / NATS 优化实施记录

更新日期：2026-08-16

## 修改前架构与代码验证

- 前端 `site-shell.tsx` 每 5 秒请求 `/health`，后端该路由执行 `db.Ping()`；活跃标签页会直接放大 PostgreSQL Ping。
- 每个登录态请求都从 `auth_sessions + users` 解析 Session，随后再次加载用户角色、角色权限、直接权限与规则。
- 消息页每 5 秒同时拉取最近 100 条消息、完整会话列表并写聊天 Presence；头部每 20 秒分别查询通知和私聊未读 COUNT。
- Presence 心跳以 PostgreSQL `user_presence_sessions` 为实时存储，虽有节流但仍随在线用户持续写库。
- Core NATS 使用 `Publish` / `QueueSubscribe`，没有持久 Stream、显式 ACK、重投或 Durable Consumer。关注通知存在事务提交前发布窗口。
- `nats_outbox` 已存在，但只服务 Mod 导入导出，不能承载通用领域事件。
- PostgreSQL 队列、热度刷新、Typesense 队列、OSS 删除、统计和审计已有可靠事实表，未迁移到 Redis/NATS。

审计中已经变化的部分：项目已有反滥用 Redis Lua 限流、本地降级和操作日志 Outbox，因此本次扩展权威实现，没有新增第二套限流器或日志总线。

## 已实施阶段

1. 删除浏览器正常状态下的固定健康轮询；只在网络/502/503/504、恢复在线或已显示异常时按 5/10/20/30 秒退避检查 `/ready`。
2. 新增 `/live`（无依赖访问）与 `/ready`（2 秒 PostgreSQL、300ms Redis、NATS状态）；`/health` 兼容并携带弃用头。
3. 统一 Redis 客户端、环境命名空间、连接池、超时、Pipeline/Lua能力、singleflight、L1 降级和指标。
4. Session 使用 SHA-256 指纹缓存；登出立即删除，用户认证版本以 10 秒上界校验封禁/密码/权限变化。
5. RBAC 使用全局持久 `rbacVersion` 与用户 `auth_version` 组成版本化 Key，不使用 `SCAN + DEL`。
6. 登录、注册和邮箱验证码实时额度迁入共享 Redis Lua；数据库继续保存验证码、登录审计和账户事实。
7. 普通 Presence、聊天 Presence 和多会话在线聚合迁入 Redis；PostgreSQL `last_active_at` 默认每 10 分钟最多快照一次。
8. 新增统一 `/api/v1/me/unread-summary`；Redis 保存派生计数，缺失时以一次 PostgreSQL 查询重建，并提供定时分批/管理员指定用户校准与漂移指标。
9. 将 Outbox 通用化并增加 JetStream 发布确认、Durable Consumer、显式 ACK/NAK、最大重投、死信和消费者幂等表。
10. 关注通知、系统通知、评论/模板/申请通知、AI、蓝图、Mod 元数据任务等主入口持久化投递意图；数据库任务 Worker 的少量直接 NATS 发布仅保留为可丢失唤醒信号，周期扫描仍是恢复路径。
11. 新增认证 SSE；私聊消息、通知和未读变更跨实例广播。前端移除 5 秒消息轮询，改为增量游标、事件触发和 60 秒兜底。
12. 公开系统设置使用 `进程内 + Redis + PostgreSQL` 版本化缓存；Secret 不进入 Redis。
13. 用户公开卡片静态摘要缓存 45 秒，Presence 在返回前独立合并，隐藏状态不会被静态缓存泄露。

## 数据边界

- PostgreSQL：Session/权限事实、正文、已读、审计、统计、Outbox、死信和任务状态。
- Redis：可重建的短期解析、限流、Presence、未读和公开摘要。
- JetStream：提交后的可靠分发；消费者副作用仍通过数据库唯一键或 `processed_events` 幂等。
- Core NATS：仅实时 SSE 跨实例广播及可从数据库扫描恢复的唤醒。

## 风险与未完成项

- 本地没有真实 Redis Cluster；功能测试和压力测试使用 miniredis 协议替身，生产连接池、哨兵/集群故障转移需在预发布验证。
- 30 并发峰值下，12 连接 PostgreSQL 池会让后台 Ping 超时；正常 10 并发没有错误。需要按部署硬件观察连接池，而不是盲目提高超时。
- SSE 已替换高频轮询，但多标签页目前通过“后台页断开”降低连接数，尚未用 SharedWorker 在可见多窗口间完全共享连接。
- JetStream 真实测试为单节点；集群重启、跨区网络分区和凭据 ACL 需要部署环境验证。
- 媒体转码、病毒扫描和缩略图没有迁移；优先级低且当前没有可靠异步收益证据。

## 上线与回滚

1. 先部署第 74 代迁移与 Redis/NATS 配置，保持各功能开关关闭或影子比对。
2. 依次开启 Session、RBAC、认证限流、Presence、未读、Outbox、JetStream、Realtime、设置和卡片缓存。
3. 未读上线初期抽样与 PostgreSQL 对比；不一致时删除用户 Key 重建。
4. JetStream 开启时必须确保旧 Core NATS 副作用消费者不重复启用。
5. 回滚只需关闭功能开关；缓存回源 PostgreSQL，Outbox 仍由扫描器处理，未读可重建，不删除业务数据。
