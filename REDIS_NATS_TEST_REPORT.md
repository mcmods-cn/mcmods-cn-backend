# Redis / NATS 测试报告

日期：2026-08-16；环境：Windows amd64，Go 1.26.5，Node 24.19.0，PostgreSQL 本地开发库，NATS Server 2.14.3 单节点 JetStream（端口 4223），Redis 功能/压力使用 miniredis 协议测试替身。不是生产监控数据。

## 修改前基线

- 后端 `go test ./...`、build、vet 通过（未启用数据库集成）。前端 typecheck/lint/build 通过。
- 旧 `/health` 10 次本地实测：全部 200，平均 23.8ms；代码确认每次 `db.Ping()`。
- 代码推算：聊天每分钟约 12 次消息全量 + 12 次会话列表 + 12 次 Presence；头部每分钟约 3 次通知 COUNT + 3 次消息 COUNT；登录请求至少 1 次 Session SQL 加多次 RBAC SQL。
- Core NATS 代码确认无持久化、显式 ACK/Durable/重投。

Redis/NATS 优化开始时存在 5 类数据库集成失败：规范身份 NULL 扫描、评论树无行、Creator SQL `order` 语法、Mod 内容版本列表为空、六种简单项目测试参数 18/8 不匹配。后续修复中没有删除或跳过测试：三类过期夹具/参数已更新为当前数据模型，Creator 与 Mod 详情代码缺陷已修复，现已全部通过。

## 实际执行命令与结果

- `go test ./...`：通过。
- `go vet ./...`：通过。
- `go build ./...`：通过。
- `pnpm typecheck`、`pnpm lint`、`pnpm build`：通过，52 个静态页面完成生成。
- `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/database -run TestCurrentUserFeaturesIntegration`：通过，开发库实际升级到 generation 74。
- `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run TestSessionAndRBACCacheHitAvoidsDatabaseLoadersIntegration`：通过；热 Session+RBAC 请求由 pgx tracer 实测 0 次 PostgreSQL 查询。
- `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi -run TestUnreadReconciliationQueryIntegration`：通过；游标批次事实查询与 Redis 校准实际执行。
- `MCMODS_RUN_NATS_INTEGRATION=1 MCMODS_TEST_NATS_URL=nats://127.0.0.1:4223 go test ./internal/queue -run TestJetStream`：通过；首次处理失败后重投、显式 ACK、重复 Msg-Id 去重及第 3 次失败进入持久 DeadLetter sink 已验证。
- 同时启用 DB/NATS 的 `TestTransactionalOutboxRollbackAndDispatchIntegration`：通过；回滚无事件、提交后投递并标记 published；消费者三次永久失败后真实写入 PostgreSQL `dead_letter_events(attempts=3)`。
- `MCMODS_RUN_DB_INTEGRATION=1 go test ./... -count=1`：全部通过；包括目录资源导入、评论树、Creator 导入、Mod 内容资源详情和六类简单项目目录的真实 PostgreSQL 集成测试。

## 接口与故障测试

- `/live` 200，响应不访问 PG/Redis/NATS；100 次顺序请求 661ms。
- `/ready` 200，返回 PostgreSQL/NATS ready、Redis disabled/degraded 语义；20 次顺序请求 831ms。
- `/live` 与 `/ready` 探针已覆盖存活和就绪语义；Dev 阶段已删除旧 `/health` 兼容入口。
- 未认证 `/api/v1/realtime/events`：401。
- Redis 停止后 `/ready` 仍 200 且 `redis=degraded`；登录限流切到严格本地额度并实际返回 429，而非无限放行。
- miniredis 单元/集成覆盖多 Session Presence、TTL离线、未读原子增减/非负/单次回源、命名空间和 Redis 清空重建。

## 压力测试

脚本：`scripts/load-test.mjs`；结果在 `test-results/load/redis-nats-*.json`。请求混合 `/live`、认证未读摘要和会话列表，写操作关闭。

正常阶梯（2x5s、5x10s、10x10s）：

- 15,225 请求，15,225 成功，0 4xx/5xx/网络错误；605.22 req/s。
- 平均 10.52ms，P50 8.91ms，P90 15.75ms，P95 16.18ms，P99 93.59ms，最大 894.46ms。
- DB 107 个样本：最大连接 15、最大 active 2、锁等待 0、>1s慢查询 0、采样错误 0。

峰值阶梯（2x5s、10x10s、30x10s，错误场景保留）：

- 当 `/ready` 被错误地高频调用时，PostgreSQL Ping 与连接池竞争，出现 503 和网络超时；证明 `/ready` 只能由基础设施低频调用。
- 移除高频 ready 后 14,613 请求、14,590 HTTP 200、0 HTTP 5xx，但仍有 23 个 10 秒网络超时；日志显示默认 12 连接池在 30 并发下后台 DB Ping 超时并短暂进入降级。
- 这是当前单机/开发池峰值瓶颈，未通过提高超时掩盖。生产应根据真实 SQL、连接池和硬件在预发布复测。

## 指标样本

压力期间管理指标记录 JetStream connected、Outbox pending=0、dead=0、Redis errors/timeouts=0；Redis请求平均约 0.154ms、P95约1.082ms。新增指标进一步区分 L1+Redis命中率和远端 Redis命中率。

## 未验证事项

- 真实 Redis Cluster/Sentinel、跨实例和网络分区；本地替身不能证明这些行为。
- JetStream 集群重启、跨节点副本和生产 ACL。
- 浏览器多可见窗口 SharedWorker 连接共享。
- 生产 CPU/内存/Redis连接数；本报告没有把未采集指标写成通过。

## 已发现问题

- 高并发下默认 PostgreSQL连接池/健康监控会短时进入降级，需部署环境容量测试。
- 既有 popularity SQL 的 `favorite_threshold` 及 `target_type` PL/pgSQL 名称歧义已修复；新增真实 PostgreSQL 回归测试直接执行 `refresh_content_popularity`，并以 schema generation 74 对既有 generation 73 数据库执行 `create or replace function` 升级。
- 初版负载脚本用 Cookie 写 Presence，被 CSRF 正确拒绝；脚本已改为 Cookie只读、Bearer才写。
