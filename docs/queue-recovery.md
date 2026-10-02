# 队列死信与数据库恢复

JetStream 任务达到配置的 `MaxDeliver` 后，先将原始事件 envelope 和终态错误元数据写到
`<subjectPrefix>.dead-letter.<taskCode>`，收到 broker 持久化确认后才终止原任务。
死信使用独立的 `Nats-Msg-Id`，避免与原任务共用 stream 的去重窗口而丢失。
数据库不可用时，原任务 handler 不会因为死信补写而重新运行，AI provider 不会因这条恢复路径再次调用。

每个 subject prefix 有独立的 durable queue consumer，使用同一个已配置 stream；
多个服务实例竞争同一消费者，一次最多一个未确认的恢复消息。
数据库写入采用独立三秒上下文，失败后按一秒至三十秒的有界退避重试；这是记录持久化重试，
不执行原业务任务，不产生供应商费用。数据库提交后才确认恢复消息。重启沿用 durable consumer，
不能通过改名、删除 consumer 或换 stream 来假定积压自动迁移。

`dead_letter_events(event_id,failure_stage)` 的唯一性使数据库补写幂等。
已保存的消费终态记录不因迟到重复消息而修改时间或清空 `replayed_at`。
管理后台原有 `admin.config.read` / `admin.config.write` 权限及单条事务重放接口保持不变；
消费失败的人工重放创建新 event ID，记录原事件和新事件。发布失败的 Outbox 恢复仍采用原有机制。
无效 JSON body 保存为 `invalidEnvelope` 供查看，保留 broker 原始 body；该记录不能作为有效任务重放。

旧 broker 死信缺少新元数据时保留 stream 原消息并显示不支持的恢复状态，需要管理员核验；
该消费者终止此条自动投递，避免占据唯一恢复槽阻塞后续有效消息，不猜测身份或删除 stream 记录。
当 JetStream 本身也拒绝写入时，原任务不被终止，错误进入队列状态；原始 stream 消息仍保留，
但已经耗尽 `MaxDeliver` 的任务不会自动变成新死信。需要在 broker 恢复后人工核验这类原始记录。
本实现不宣称跨供应商、数据库与 broker 恰好执行或恰好计费一次。

本次没有改变既有 stream 的保留策略、容量、过期或删除规则，避免擅自删除历史事件。
现有 `LimitsPolicy` 没有应用默认容量或期限上限；生产容量、保留周期和既有积压需要独立运行证据及明确运维决策。
部署无需 schema 迁移，服务账号须有既有 stream 下死信 subject 的发布和 durable consumer 管理权限。
滚动部署时新消费者只处理新元数据；旧 worker 仍可能产生缺少元数据的记录，因此应先完成 worker 升级再评估积压。

任务处理上下文继承 queue client 生命周期。关闭 client 或取消其父上下文，会取消正在
运行的 handler，而不等待独立任务超时。供应商已经受理的请求仍可能计费；取消本地
HTTP 请求不能保证远端未执行。关闭期间未确认的 broker 任务由既有投递与业务幂等机制恢复。

隔离验证：`go test ./internal/queue -run 'TestDeadLetter|TestUnsupportedDeadLetter|TestClosingQueue|TestTimedOutTask|TestJetStream' -count=1`。
数据库断言另需 `MCMODS_RUN_DB_INTEGRATION=1` 和专用回环数据库；使用嵌入式真实 NATS file store，
不调用付费供应商、不读取生产任务。
