# 全站公告与请求邮件意图（CMP-003）

旧系统公告消费者先保存公告，再读取用户的邮件订阅设置、逐个单独提交邮件任务。读取或任务写入错误仅记录警告；原事件重复投递又因公告的 `source_event_id` 已存在而直接返回，因此可能永久缺少已明确请求的邮件任务。

公告和原有符合条件的邮件任务现在共用一个 PostgreSQL 事务。任意订阅读取、行扫描或邮件意图写入错误都会返回消费者并回滚公告及已写入的邮件意图。缓存未读代次只在成功提交后变化；查询行流完整结束、关闭后才继续在同一个事务写邮件任务，不借用另一个连接。

接收者规则保持：有明确邮件订阅设置、已验证邮箱且账号当前有效。公告原文、默认语言、`notification.email.requested` 事件、`source_event_id` 去重及后续邮件消费者保持原有契约。外部 SMTP 的“已接受但本地未收到确认”不具有数据库事务语义；本修复证明持久化邮件意图，不承诺外部邮件恰好投递一次。

两项隔离 PostgreSQL 回归分别验证原反例和恢复边界。原反例通过普通获授权账号真实 HTTP 发布公告，实际消费者遇到精确 `XX001` 邮件意图错误时必须回滚，恢复和同事件重复调用只产生一条公告及一条合法邮件意图。扩展矩阵在第二个邮件意图失败时检查第一个也回滚，随后让两个各只有一个连接的实际消费者同时到达精确 PostgreSQL 屏障，再验证仅一次公告和两名合法接收者的意图，禁用、未验证和未订阅账号不入队。这些消费者直接调用，不代表 NATS 传输或 SMTP 实际投递已经验收。

```sh
APP_ENV=test MCMODS_RUN_DB_INTEGRATION=1 \
DATABASE_URL="$AUDIT_TEST_DATABASE_URL" \
MCMODS_TEST_DATABASE_URL="$AUDIT_TEST_DATABASE_URL" \
go test -race ./internal/httpapi \
  -run '^TestAuditCMP003(RequestedBroadcastEmailIntentCannotBeLostAfterNotificationCommit|BroadcastEmailRecipientPolicyAndSingleConnectionWorkersRecoverExactlyOnce)Integration$' \
  -count=1 -v
```

没有 schema 或任务消息格式迁移。更新全部通知消费者后才能依赖新保证；混用旧消费者仍有历史丢失窗口。回滚代码会重新开放该窗口，不能删除已有意图、公告或审核历史。接收者查询和队列预算未放宽；本次回归没有证明最大用户规模的群发吞吐量。

存量公告及邮件事件没有精确的父事件投递关联，且队列/通知保留策略可能已删除旧行。相同标题的邮件意图数量不足或为零只属于核对候选，不能按当前订阅者清单推断历史接收者并重新群发。必须结合原请求 `sendEmail`、实际发生时的订阅/账号状态、保留的源事件及投递记录，另行形成限定作用域、dry-run、计数和幂等补偿方案。没有执行生产诊断、通知重发或奖励重放。
