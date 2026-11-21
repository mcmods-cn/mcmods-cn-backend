# NATS 配置凭据与保存

管理接口沿用 `admin.config.read` / `admin.config.write`。配置使用现有加密设置保存。
GET、PUT 响应及运行状态去除服务器 URL 中的 userinfo、查询和 fragment。密码、Token 仅显示是否已配置。
URL 可以是逗号分隔的多服务器列表。内部保留原有每服务器认证，界面提交相同脱敏 URL 时不会清空旧认证。

NATS 的 `token@host` 表示 Token。`username:password@host` 表示用户名/密码，包括空密码的冒号。
显式替换 Token 或密码会更新 URL 中相应的认证内容。清除 Token 删除 Token userinfo，清除密码保留用户名与冒号。
主动输入新的 URL 列表表示替换服务器列表，不会猜测旧服务器凭据应迁移到哪些新地址。
空白秘密输入沿用原值。替换和清除同一秘密不能同时提交。

候选配置先连接并建立既有订阅，成功后才保存并切换运行时。认证或持久化失败返回明确错误，仍保留上一套有效运行时。
错误响应不回显凭据。诊断中的 URL 及已知敏感值也会脱敏。没有新的权限节点或生产配置操作。

`TestOCT02NATSURLSecretsAreHiddenAndMaskedSaveAuthenticates` 与 `TestOCT02NATSURLCredentialReplacementAuthenticates`
使用回环地址的真实内嵌 NATS broker 验证密码/Token、脱敏往返保存、替换及清除失败后的运行时保留。
这些测试不证明生产 broker 配置、网络或凭据有效。

## 初次启动、升级与实例切换

启动时先从 PostgreSQL 读取完整的已保存设置，再注册订阅及启动 Dispatcher。数据库只在没有保存配置时采用环境初值。若已存在的配置无法读取或解封，API 初始化会重试，`/ready` 的组件 `nats` 报告 `nats_configuration_unavailable`。检查当前数据库和加密密钥，恢复正确的配置后再观察就绪状态。不要用示例默认配置覆盖原凭据。

事务 Outbox 在所有分发模式下启用。`outboxEnabled` 保留为兼容字段，管理 GET 返回 true，旧客户端提交 false 或缺少该字段会归一为 true。暂停任务请调整该 task 的 enabled。关闭 JetStream 会使用本地分发。暂停各任务代码才能停止其新的业务执行。已开始的工作保留原截止时间。

新建 stream 默认有 256 MiB 的字节预算。首次环境引导通过 `NATS_JETSTREAM_NEW_STREAM_MAX_BYTES` 配置正整数字节，管理 GET/PUT 使用 `jetStream.newStreamMaxBytes`。旧保存记录缺失此字段时采用默认值，旧客户端 PUT 省略此字段保留当前数值。改变它只影响以后创建的 stream。当前实际限制、容量预警和在途任务恢复见[队列恢复](queue-recovery.md)。

保存成功只切换处理此次 PUT 的进程。其他实例在下次启动时读取同一持久化设置。改变 broker、prefix、stream 或 durable 前，先核对原消费者进度、未确认任务和死信，再按本次配置安排全体实例切换。新旧命名空间不会自动搬移积压。候选配置回滚会关闭候选连接，保留已创建的 stream 和其他发布者已获确认的消息。
