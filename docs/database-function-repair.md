# generation 168 派生投影函数修复

`cmd/db-function-repair` 用于修复已存在的 generation 168 数据库中的四个 PostgreSQL 触发器函数及评论热度的精确触发绑定。新空库安装已包含同样定义；同代服务启动不会重复安装 schema，因此已有库需要单独、明确执行本工具。它不支持其他 generation 的升级，不修改业务行、不回填历史统计、不更改表结构或 generation。

修复内容：

- `enqueue_search_servers_for_mod` 和 `enqueue_search_servers_for_mod_parent` 按服务器去重。当同一服务器有多个原始 mod 标识指向同一 mod 时，避免一条 INSERT 多次更新同一搜索队列行导致 PostgreSQL 21000，并保留每个受影响服务器的一项 upsert。
- `record_changelog_popularity_event` 按“已审核且活动”的旧、新可见状态差异产生 release 热度事件。重复删除不重复扣分，恢复已审核的活动记录重新加分。当前 API 没有删除/恢复更新日志的入口；此修复保护数据库状态变更的一致性，不能据此声称已有 UI 恢复流程通过。
- `refresh_popularity_from_comment` 从 AFTER ROW 改为 INSERT/UPDATE/DELETE 三个 AFTER STATEMENT transition-table 绑定，按受影响 route/author 的公开行数差分推导首/末贡献。批量写入和同作者并发只计一位有效评论者；重复状态或正文更新的净差分为零，author/target 变化同时调整两个来源。先按 route 顺序锁定现有 lifetime-fact 行，再在 VOLATILE 函数下一条 SQL 读取等待后的 READ COMMITTED 快照。其他评论楼层、可见数量和热度行触发器继续先执行；没有新增全局评论锁。

## 检查与应用

连接配置沿用应用的 `DATABASE_URL` 或 DB 配置。执行前由操作者独立核对服务器、数据库身份和授权范围；数据库名称只是第二次确认，不证明可删除或可访问。审计任务只在新建的专用隔离库验证，没有连接生产。

```sh
go build -o ./db-function-repair ./cmd/db-function-repair
# 默认只读取 metadata、四个 pg_proc 定义和已知评论触发绑定，不输出连接串或函数内容。
./db-function-repair
# 由获授权操作者填写真实目标；备份必须是尚不存在的私有路径。
./db-function-repair -apply -confirm-database '<精确数据库名>' \
  -backup '<受控备份目录>/projection-before.json'
./db-function-repair
```

检查输出 `generation=168 checked_functions=4 functions_requiring_repair=N`。N 是函数定义差异数，`comment_bindings_require_repair` 另外指明旧单行绑定是否尚未替换；这些不是错误行数，也不表示实际运行健康。工具在连接前核对配置中的库名，连接后再次核对实际 `current_database()`，要求 generation 168 和四个可恢复的原函数定义及原评论绑定；误库、缺失对象、不匹配的 generation、不可恢复的自定义函数属性都中止。

修改与应用初始化共享 advisory lock，在一个事务内替换四个函数和原评论热度绑定。原定义以 0600 新文件保存并同步文件及目录后才执行 DDL；备份不可写、已有同名文件、DDL 失败或事务提交失败都不报告成功。失败可能留下完整或部分备份文件，应核对原因并另选新路径，不能覆盖原备份。备份包含可执行函数体，必须作为私有恢复材料保存；不要上传不受信任的备份或手工拼接 SQL。

## 发布、恢复和兼容性

先在对应隔离副本验证检查、应用和恢复，确认常规数据库备份及其恢复流程，再由获授权操作者执行函数修复。已有 generation 168 库先执行本工具并检查完成，再发布后端 API/worker 代码；函数名、参数、trigger 返回类型和 schema generation 均不变；旧单行评论热度绑定以三条 statement 绑定替换，旧、新应用代码可以使用修复后的函数。新空库直接安装后端当前 schema，不需要再执行修复。服务、worker、部署脚本都不会自动对已有库执行本命令。

```sh
./db-function-repair -restore '<受控备份目录>/projection-before.json' \
  -confirm-database '<精确数据库名>' \
  -backup '<受控备份目录>/projection-before-restore.json'
./db-function-repair
```

恢复前同样保存当前四个函数定义及评论绑定，要求备份中的库名、generation、格式和函数集合匹配。恢复替换函数定义及原评论绑定，不删除应用新写入的数据；恢复旧定义会重新引入原缺陷，不能修复此前错误的热度事件。工具拒绝额外 SQL、未知函数和不符合原工具格式的定义；数据库原本存在自定义函数属性时应由维护者审查独立方案，不能强行略过保护。

既有热度事件是否受旧缺陷影响、是否需要统计核对，必须基于获授权的真实数据另行判断；本次不删除、重写或补记历史业务事件。实际生产性能、备份可恢复性和多节点发布尚未验证。

## 验证边界

确定性回归使用真实 PostgreSQL 18 专用库验证原 generation 168 定义升级、重复执行、错误目标/代数拒绝、备份失败无改动、第二个 DDL 失败后全部回滚、恢复原定义，以及修复后 alias 搜索、更新日志状态变更、评论单/批次首末和 author/target 差分；真实并发首评论验证等待后快照。CLI 验证默认只读检查、应用、重复应用、备份不可覆盖、恢复和 0600 权限。

```sh
export APP_ENV=test DB_RESET_ON_START=false MCMODS_RUN_DB_INTEGRATION=1
export MCMODS_DB_FUNCTION_REPAIR_TEST_TARGET='<本任务新建并独占的实际库名>'
CGO_ENABLED=1 go test -race ./cmd/db-function-repair ./internal/database \
  -run '^(TestOCT02|TestProjectionRestore|TestFunctionBackup)' -count=1 -v
```

备份格式 version=2，要求恰好四个函数和完整原/新评论绑定，拒绝缺失定义或自定义 enablement。`TestOCT02ProjectionFunctionForwardRepairAndRecovery` 临时替换测试目标 public 中的四个定义/评论绑定，并临时改动 generation 元数据，结束后恢复；只能在身份确认无误且可丢弃的独占库执行。服务器搜索/更新日志 projection 测试安装完整随机临时 schema。未设明确测试目标时恢复测试会 SKIP，不能把该次运行当成前向修复已验证。

故障注入子例需要在独占测试实例具备创建 event trigger 的测试权限（本轮为专用实例的初始化角色）；正常修复工具不要求此权限，不应据此扩大应用运行账号或生产权限。

本轮日志脱敏还有独立的[版本列前向扩展](log-redaction-upgrade.md)。已有168库在
发布新版 API/worker 前须同时完成该列及约束验证；本函数工具不代替日志扩展，日志
状态记录也不能代替本工具的可恢复函数备份。两项命令都不自动部署或重处理生产数据。
