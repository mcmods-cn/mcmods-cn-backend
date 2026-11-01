# 审核结果通知候选诊断

只读检查一个指定公开对象、一个最多 31 天的时间窗。支持整合包、六类资料项目和作者资料。不提供 apply、通知重发或奖励重放功能。

```sh
go run ./cmd/diagnose-review-result-intents --sql

APP_ENV=test DATABASE_URL="$AUDIT_TEST_DATABASE_URL" \
go run ./cmd/diagnose-review-result-intents \
  --schema public --kind modpack --public-id "$AUDIT_AGGREGATE_PUBLIC_ID" \
  --since 2026-10-24T00:00:00Z --until 2026-10-25T00:00:00Z --limit 100
```

连接使用仓库既有数据库配置。操作员应选择已获授权的数据库及准确 schema；本工具没有初始化或迁移数据库的行为。实际查询使用只读、可重复读事务，总执行上限 30 秒、单条查询 10 秒。两个时间边界和准确的九位公开 ID 必填；不接受内部主键。输出不含标题、正文、邮箱、审核理由或来源文件内容。

逐条输出该对象批准/驳回事件的公开修订 ID、公开事件 ID、时间、接收者是否仍存在，以及可观察的结果通知意图/通知计数。默认输出最多 100 条决定；`result_may_be_truncated=true` 表示不能据此认定已覆盖全部决定。

旧通知 payload 没有修订 ID。工具仅按对象、接收者、模板及本次决定到下一次决定之间的时间区间做关联；同一事务时间、重叠审核、迟到消费、保留策略及删除都可能影响关联。计数不证明准确修订的投递，更不证明用户或邮件服务器已收到。

`missing_result_candidate=true` 仅表示“接收者仍在、当前区间看不到结果意图和通知”。它不证明丢失，也不授权重建。账号已删除时仍列出审核事实并显示无接收者，不推测应该向谁补发。任何后续修复须另外核对不可变审核记录、历史事件和保留/投递记录，限定目标、dry-run、计数及幂等补偿方案。不得重放经验、货币或任务奖励。

回归入口：

```sh
APP_ENV=test MCMODS_RUN_DB_INTEGRATION=1 \
DATABASE_URL="$AUDIT_TEST_DATABASE_URL" \
MCMODS_TEST_DATABASE_URL="$AUDIT_TEST_DATABASE_URL" \
go test ./internal/httpapi \
  -run '^TestAuditCMP003ReviewResultDiagnosticIsReadOnlyAndNeverTreatsAbsenceAsProofIntegration$' \
  -count=1 -v
```

该回归通过普通 HTTP 建立整合包、资料项目和作者资料的独立批准/驳回，校验每次真实只读快照零写入。仅在任务自己的合成数据中模拟 outbox/通知保留删除，验证已经存在的实际通知不会被当作丢失、全部当前行缺失仍只是候选，并核对准确对象范围。没有执行生产诊断或修复。
