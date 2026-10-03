# generation 168 日志脱敏版本扩展

`cmd/db-log-redaction-upgrade` 为已存在的 generation 168 库显式增加
`log_shares.redaction_applied_version integer NOT NULL DEFAULT 1`，并验证值至少为 1
的约束。新空库直接安装该列，不需要再执行工具；已有同代库不会在启动时自动 ALTER。
启动检查缺列、列定义不兼容或约束未验证时会明确拒绝初始化并提示本命令，generation
数字本身不代表已经安装该扩展。

原 `redaction_version` 继续表示创建时的脱敏/源文件去重身份，保留
`(source_file_id, redaction_version)` 的活动唯一索引。新列记录共享副本实际上应用的
脱敏器版本。旧 v1 分享与同一文件的 v2 分享可以共存；重新脱敏旧副本不会修改原身份、
公共分享码或源文件关联，不删除原分享来避开唯一键冲突。

新版写入明确保存创建版本及已应用版本 2。旧副本在受控读取路径重新脱敏后，同一事务
更新条目内容、尺寸、行数、校验和、计数及已应用版本；失败保留原副本和版本，公开读取
不得把失败的重脱敏当成安全内容返回。接口展示实际已应用版本，重处理不会把内部版本
标记混入公开的脱敏计数。处理上限、权限与并发保护见日志分享模块实现及其回归。

## 检查与执行

执行前由获授权操作者确认主机、精确数据库身份和范围，准备常规数据库备份及可用的
恢复流程。名称带 `test` 不构成访问或删除授权。本审计只在新建、身份已确认的独占
隔离 PostgreSQL 18 库操作，没有对生产执行迁移或重处理真实日志。

```sh
go build -o ./db-log-redaction-upgrade ./cmd/db-log-redaction-upgrade
# 只读检查 generation、列定义和约束；不输出连接串或日志正文。
./db-log-redaction-upgrade
# 精确确认并保存尚不存在的私有 schema 状态记录，然后显式执行。
./db-log-redaction-upgrade -apply -confirm-database '<精确数据库名>' \
  -backup '<受控目录>/log-redaction-before.json'
./db-log-redaction-upgrade
```

`-backup` 文件只包含操作前数据库名、generation 和列/约束状态，以 0600、O_EXCL
保存并同步文件与目录；不能覆盖旧文件。它是 schema 状态证据，**不是日志数据备份**，
不能据此恢复用户内容。备份失败、误库、非168代、缺失表或已有不兼容列定义均中止。
默认检查不写文件或改变 schema。

工具与 schema 初始化共享 advisory lock。新增列使用常量 default 1，PostgreSQL 18
使用缺失值元数据，避免对旧行物理回填；仍需短时取得 `ACCESS EXCLUSIVE` 锁。第一阶段
在同一事务增加列与 `CHECK ... NOT VALID`，随后提交；新写入从此受约束限制。第二阶段
单独执行 `VALIDATE CONSTRAINT`，需要扫描旧行，但不再持有第一阶段加列的独占锁。
连接 `lock_timeout=10s`，命令总期限五分钟；大表扫描时间与锁等待必须在代表性副本实测，
本轮小型合成样本不证明生产容量。未授权时不要扩大超时或改动生产配置。

第一阶段失败会回滚该阶段；第二阶段失败可能留下已增加的列及未验证约束。错误会明确
说明需要恢复验证，后台初始化仍拒绝该未完成状态。排除阻塞后使用新的状态记录路径
重复 `-apply` 可继续验证；不要重置数据库或删除旧分享来恢复。已完成状态重复执行只
检查和保存新的状态记录，不重新增加列或重处理日志。

## 发布与恢复

已有168库先保存正常数据备份、执行前向工具、确认 `upgrade_required=false`，再发布
新 API 和读取该字段的 worker；空库可直接使用新版完整 schema。另一个四函数/评论绑定
修复仍按 [函数修复](database-function-repair.md) 独立执行，两者不互相替代。其他旧代库
不支持本工具，仍需单独制定数据保留升级方案。

旧代码可以忽略新增列，因此代码恢复时保留列和约束，不执行 DROP；该扩展没有破坏性
down 操作。**恢复旧脱敏器会重新引入敏感日志暴露风险，不是安全回滚。** 新旧公开读
节点混跑时，旧节点不能保证重新脱敏旧副本；维护者应先隔离旧读节点或采用适用的维护
窗口，再验证全部公开读节点已经使用新版。这里说明部署依赖，不代表本任务已部署或
验证生产滚动升级。

真实 PostgreSQL 回归覆盖原168升级、错误目标/代数拒绝、状态记录失败无改动、验证
故障与恢复、同源v1/v2共存、旧计数保留、无效版本拒绝、重复应用和启动检查。日志语义
及旧内容处理的真实数据库回归由日志分享模块独立执行；没有发送日志给真实 AI 或第三方。

```sh
export APP_ENV=test DB_RESET_ON_START=false MCMODS_RUN_DB_INTEGRATION=1
export MCMODS_DB_FUNCTION_REPAIR_TEST_TARGET='<身份已核对的本任务独占可丢弃库名>'
CGO_ENABLED=1 go test -race ./cmd/db-log-redaction-upgrade ./internal/database \
  -run '^Test(LogRedactionMetadata|OCT02LogRedactionForwardUpgrade)' -count=1 -v
```

前向回归为了重现原168，会在这个明确可丢弃目标中移除本任务新增的已知版本列、临时
修改代数，并用受控 event trigger 注入验证故障；结束后留下已完成的扩展并清理合成数据。
该故障测试需要专用实例创建 event trigger 的权限，常规工具不需要，不据此扩大运行账号。
