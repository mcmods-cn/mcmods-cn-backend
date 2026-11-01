# 作者/团队头像草稿存量工具（OSS-001）

只修复明确选中的**活动私有草稿**内 `avatarUrl` 短签名引用。默认只读 dry-run，最多 100 个明确的九位草稿公开 ID，同一明确 owner 公开 ID；不扫描后自动批量修改。新建作者/团队草稿、错 owner、未知字段形状、不匹配的当前对象、未审核/已失效/缺少实际字节收据的文件、未知来源和带业务转换参数的 URL 都 HOLD。

可信来源从所选数据库/schema 的既有 `oss.aliyun` 配置严格解密读取。没有“相信传入 URL/base”的选项，不访问图片/供应商，不初始化数据库或启动应用 Worker。只有已批准作者/团队的当前精确头像 FK，与活动、可信 raster、`source_hash_verified` 及完整 committed source/stored 字节收据匹配，文件 bucket/endpoint 与当前配置一致，当前持久引用恰为现配置生成的稳定 URL，旧草稿 URL 又证明为相同配置 endpoint/key 的注册签名变体，才有资格替换。业务 `size`、`variant` 等未知参数不被剥除。正常共享的**同一当前文件**可以保留；工具不能授权绑定另一个人的新文件。

实际原生05反例保存了短签名到 **user_drafts**，而当时 creators 行为稳定引用。代码没有证明受支持的创建 API 会把签名写进 creators。另有 signed-creator-row 的受控旧数据/故障测试；这类行在本工具里 HOLD，需先通过正常有权编辑和独立审核建立稳定引用，不由工具猜 key 或改公开资料。

准备仅当前操作员可访问的目录；计划和回滚文件包含原私有 capability，禁止放进日志、PR 附件或证据 ZIP。示例里的连接环境沿用项目配置，`SETTINGS_ENCRYPTION_KEY` 必须与目标配置匹配，勿在命令行打印。

```sh
umask 077
mkdir -m 700 /absolute/private/avatar-maintenance

# 默认只读：保留计划后审阅 stdout 的安全计数/公开ID及 targetSha256。
APP_ENV=test DATABASE_URL="$AUDIT_TEST_DATABASE_URL" \
go run ./cmd/repair-creator-avatar-drafts \
  --schema "$AUDIT_OWNED_SCHEMA" --owner-public-id "$AUDIT_OWNER_PUBLIC_ID" \
  --draft-id "$AUDIT_AUTHOR_DRAFT_ID" --draft-id "$AUDIT_TEAM_DRAFT_ID" \
  --private-output /absolute/private/avatar-maintenance/plan.json
```

计划固定真实数据库名/OID、schema/OID、服务地址、客户端连接地址指纹和当前加密 OSS 配置摘要；没有保存密码或配置密钥。其私有内容有独立用途 HMAC，修改计划/回滚值或换 settings key 会被拒绝。不要修改 plan 来减少 HOLD 项；准备新的明确范围重新 dry-run。

生产 apply 必须另有明确执行授权。本任务没有执行生产诊断/修复。操作员应针对**实际目标**完成已选数据的备份/恢复核验并保管原文件；参数是明确的“该已核验备份属于此精确 target”的声明，CLI 实际核对备份文件 owner、0600 权限、非符号链接和 SHA256。校验和不等于工具替操作员完成了数据库备份或恢复演练。

```sh
# 明确写入：只读计划、实际目标指纹、已核验备份声明和新的私有回滚输出均必需。
APP_ENV=test DATABASE_URL="$AUDIT_TEST_DATABASE_URL" \
go run ./cmd/repair-creator-avatar-drafts --apply \
  --private-input /absolute/private/avatar-maintenance/plan.json \
  --expect-target-fingerprint "$AUDIT_TARGET_SHA256" \
  --verified-backup-file /absolute/private/avatar-maintenance/verified-backup \
  --verified-backup-sha256 "$AUDIT_BACKUP_SHA256" \
  --verified-backup-target "$AUDIT_TARGET_SHA256" \
  --private-output /absolute/private/avatar-maintenance/apply-recovery.json

# 撤销本次字段替换：仍需目标/备份声明，新的输出不能覆盖原回滚文件。
APP_ENV=test DATABASE_URL="$AUDIT_TEST_DATABASE_URL" \
go run ./cmd/repair-creator-avatar-drafts --rollback \
  --private-input /absolute/private/avatar-maintenance/apply-recovery.json \
  --expect-target-fingerprint "$AUDIT_TARGET_SHA256" \
  --verified-backup-file /absolute/private/avatar-maintenance/verified-backup \
  --verified-backup-sha256 "$AUDIT_BACKUP_SHA256" \
  --verified-backup-target "$AUDIT_TARGET_SHA256" \
  --private-output /absolute/private/avatar-maintenance/rollback-recovery.json
```

每次写入在一个事务内重新锁定当前配置、作者/文件和草稿并核验范围，CAS 同时检查完整 JSON payload 的 SHA256 与 PostgreSQL `xmin` 行版本。只修改 `payload.avatarUrl` 与正常 `updated_at`；其余业务字段、公开作者、权限、文件、字节收据和不可变审核/审计都不变。任何一行冲突或私有回滚文件持久化失败，整个批次回滚；不会跳过冲突行当成全部成功。重复 apply/rollback 对已经达到相同目标的行不再写入。原回滚文件在数据库提交**之前**排他创建、fsync；状态刻意为 `PREPARED_BEFORE_COMMIT`，避免进程/提交错误后凭文件存在就推定数据库已提交。重复操作根据当前实际 payload/CAS 状态判断。0 行重复 apply 的空回滚不是原 apply 的替代品，保留最初非空 recovery。

若用户之后编辑或自动保存，即使正文完全相同，行版本变化也会阻止陈旧 apply；若用户在修复后输入新内容，rollback 不得覆盖它。此时保留原计划/备份/失败计数，重新诊断，不强制恢复旧 payload。配置、schema 或 settings key 变化会使旧计划/回滚失效，需要独立核对后制定补偿；不放宽约束绕过。恢复原短签名只为撤销本次有限字段操作，并不能延长其有效期或撤回第三方已下载内容。

预算为单次工具事务 30s、单条 SQL10s、锁等待1s。schema 无迁移，无公开发布、通知、奖励、余额或删除操作；没有自动把 pending 文件标 clean。现有旧 ObjectKey 接口兼容性和 BE→FE 部署顺序见本批 OSS-001 报告。

```sh
APP_ENV=test MCMODS_RUN_DB_INTEGRATION=1 \
DATABASE_URL="$AUDIT_TEST_DATABASE_URL" MCMODS_TEST_DATABASE_URL="$AUDIT_TEST_DATABASE_URL" \
go test ./internal/httpapi -run '^TestAuditOSS001DraftRepair' -count=1 -v
go test ./cmd/repair-creator-avatar-drafts -count=1 -v
```

验收须区分：实际 HTTP 上传/普通创建/独立审核/私有草稿建立后的 PostgreSQL 工具测试；CLI 私有文件/真实 checksum 控制与构建；实际生产操作。不能把受控旧数据、测试备份或测试 exit0 标成生产已修复。
