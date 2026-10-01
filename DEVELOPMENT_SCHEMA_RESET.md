# 开发数据库重置

代码声明项目采用生产前空库策略；当前 Schema generation 85 是权威安装基线，生产是否部署及已有数据状况不由这份说明证明。旧开发库不做逐代升级或历史回填；有效的合成表多版本、评论楼层、日志分享、全局资源权限、治理、`autobot` 自动化身份、项目维护状态和认证/授权版本拆分结构已直接并入空库安装语句。

## 安全条件

`cmd/db-reset` 和启动时重置使用同一校验：

1. `APP_ENV` 必须是 development 或 test；
2. 数据库名必须满足开发/测试数据库安全规则；
3. 必须显式设置 `DB_RESET_ON_START=true`；
4. `DB_RESET_CONFIRM` 必须精确为 `RESET <实际数据库名>`；
5. production 配置无条件拒绝。

PowerShell 示例：

```powershell
$env:APP_ENV='development'
$env:DB_RESET_ON_START='true'
$env:DB_RESET_CONFIRM='RESET mcmods'
$env:GOTOOLCHAIN='local'
& 'D:\System\SDK\go\go1.26.4\bin\go.exe' run ./cmd/db-reset
```

命令删除 public schema 中当前应用对象，随后一次事务安装 generation 85，创建外键索引并执行 RBAC、管理员、`autobot`、站务多语言、封禁理由、许可证策略和爬虫默认配置种子。不要在生产环境复制这些变量。

上一轮只服务于旧开发数据的回填命令、迁移进度 UI、旧评论举报表路径和双读入口已删除；公开 URL/API 边界中仍有真实外部兼容责任的代码不属于数据库回填，继续保留。


## Generation 85 的兼容前向修复

正常启动调用 `Migrate`，不会自动重置已存在 generation 85 的数据。2026-10 审计增加事务化 `schema_repair_history`；同一 migration advisory lock 下仅执行未登记 patch：

- `audit-2026-10-content-tree-and-favorite`：先检查重复根分类 ordinal、重复活动根 system key、祖先循环；发现脏数据只返回脱敏计数并停止，不删除/合并。校验通过后增加 NULL 根部分唯一索引、改进树约束并修正收藏热度函数的 SQL 变量歧义。
- `audit-2026-10-blueprint-worker-lease`：为 `blueprint_jobs` 增加 `run_token`、`heartbeat_at` 及 processing 心跳部分索引；已有行/对象保留。

旧 generation 不是无损升级路径，本次未替历史数据制定破坏性回填。实际有业务数据的旧库不得按错误提示直接重置，应先取得部署事实、备份和明确迁移方案。

部署顺序：备份前提由真实环境负责人确认；先停旧蓝图 worker（不识别 fence），再执行正常 `Migrate`，成功后同时更新 API 与 worker；最后启用匹配的前端恢复按钮。索引创建和分类表锁可能阻塞大表写入，必须在维护窗口评估真实数据规模；本次仅隔离样本验证。失败时事务回滚，没有执行数据清洗；兼容新增列/索引可保留，回退旧 worker 时应停用新的崩溃重领，避免旧实例迟到写入。没有设计伪装成无损的破坏性 down。

种子只补齐缺失封禁理由、许可证策略与账号/权限，保留管理员修改的状态、联系人信息、密码和已有 allow/deny。`SEED_ADMIN_PASSWORD` 仅用于初次创建或启用尚未初始化且 active 的默认管理员密码，不再重复重设既有可登录管理员；已有账号密码恢复应使用正式密码重置流程。`autobot` 仍强制不可交互登录，其封禁或权限撤销不会因重启被还原。

隔离测试初始化使用 `cmd/test-setup` 和 `scripts/test-services.sh`，不要复制 `db-reset` 到有真实用户数据的数据库。真实环境健康、备份可恢复性、膨胀、容量和复制状态均未由这些测试证明。
