# 开发数据库重置

当前项目尚未进入生产，Schema generation 168 是唯一权威开发基线。旧开发库不做逐代升级或历史回填；当前业务表、约束、索引、权限种子及MRPack预检/报告生命周期均直接并入空库安装语句。

generation 168 已存在库的四个派生投影函数和评论热度绑定修复是此规则的有限例外：`cmd/db-function-repair` 默认只检查，经精确目标确认并保存私有恢复备份后，可在同一事务替换搜索任务去重、更新日志热度状态函数和评论 statement 差分绑定。它不删除表、不修改业务行、不改变 generation，也不是通用旧代升级或历史数据回填机制。服务启动不会自动执行该修复。操作与恢复顺序见 [数据库函数修复](docs/database-function-repair.md)；有重要数据的库不得用 RESET 代替函数修复。

日志脱敏版本还有一项独立的非破坏扩展：`cmd/db-log-redaction-upgrade` 为已有168库
增加已应用脱敏版本列并验证约束，保留分享身份和内容；正常数据备份与精确目标确认后
先执行该工具，再发布使用该字段的后端。启动只检查必需扩展，不自动改 schema。
验证中断可重复执行继续，代码恢复保留新增列；恢复旧脱敏器会重新暴露原风险，不能
视为安全回滚。完整限制见 [日志脱敏升级](docs/log-redaction-upgrade.md)。

## 安全条件

`cmd/db-reset` 和启动时重置使用同一校验：

1. `APP_ENV` 必须是 development；初始化本机隔离测试库时也使用 development，初始化完成后再切换为 test 执行测试；test/staging/production 均不允许 `DB_RESET_ON_START`；
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

命令删除 public schema 中当前应用对象，随后一次事务安装 generation 168，创建外键索引并执行 RBAC、管理员、`autobot`、站务多语言、封禁理由、许可证策略和爬虫默认配置种子。不要在生产环境复制这些变量。

上一轮只服务于旧开发数据的回填命令、迁移进度 UI、旧评论举报表路径和双读入口已删除；公开 URL/API 边界中仍有真实外部兼容责任的代码不属于数据库回填，继续保留。
