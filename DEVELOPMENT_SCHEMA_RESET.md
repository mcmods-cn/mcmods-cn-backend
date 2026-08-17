# 开发数据库重置

当前项目尚未进入生产，Schema generation 80 是唯一权威开发基线。旧开发库不做逐代升级或历史回填；有效的合成表多版本、评论楼层、日志分享、全局资源权限、治理和自动化结构已直接并入空库安装语句。

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

命令删除 public schema 中当前应用对象，随后一次事务安装 generation 80，创建外键索引并执行 RBAC、管理员、站务多语言、封禁理由、许可证策略和爬虫默认配置种子。不要在生产环境复制这些变量。

上一轮只服务于旧开发数据的回填命令、迁移进度 UI、旧评论举报表路径和双读入口已删除；公开 URL/API 边界中仍有真实外部兼容责任的代码不属于数据库回填，继续保留。
