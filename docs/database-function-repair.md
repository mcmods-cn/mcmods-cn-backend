# 派生投影函数修复与恢复

`cmd/db-function-repair` 用于恢复当前基线库的四个投影函数及评论热度触发器。空库安装已经包含这些定义。已安装库启动时根据实际结构指纹检测漂移，日常发布和重启无需执行此命令。[空库安装与数据库恢复](../README.md#数据库安装发布和恢复)

此工具管理下列定义。

- `enqueue_search_servers_for_mod` 和 `enqueue_search_servers_for_mod_parent` 按服务器去重。当同一服务器有多个原始 mod 标识指向同一 mod 时，避免一条 INSERT 多次更新同一搜索队列行导致 PostgreSQL 21000，并保留每个受影响服务器的一项 upsert。
- `record_changelog_popularity_event` 在更新日志进入或离开已审核且活动状态时调整 release 热度。重复写入同一状态产生零差分。
- `refresh_popularity_from_comment` 使用 INSERT、UPDATE、DELETE 三个 AFTER STATEMENT 触发器。从 transition table 计算作者与目标的可见评论数差分，按 route 顺序加锁，并使用锁等待后的快照计算首条与末条贡献。

## 核对与修复

连接沿用应用的 `DATABASE_URL` 或 `DB_*` 配置。操作者应核对实际服务器、数据库和授权范围，并在该库的隔离副本中验证所选动作。库名确认用于发现配置与目标不一致。准备常规数据库备份并验证恢复，将下面的定义备份写入受控私有目录。

```sh
go build -o ./db-function-repair ./cmd/db-function-repair
./db-function-repair
./db-function-repair -apply -confirm-database '<精确数据库名>' \
  -backup '<受控备份目录>/projection-before.json'
./db-function-repair
```

默认检查只读。输出中的 `generation` 与 `checked_functions=4` 确认安装代际及检查范围，`functions_requiring_repair` 统计函数体差异，`comment_bindings_require_repair` 反映绑定差异。写入时工具同时核对配置与实际库名，要求匹配当前编译定义的安装标记，并要求函数及评论触发器处于可恢复形态。

工具与初始化共享 PostgreSQL advisory lock。它先以 `0600` 独占创建原定义备份并同步文件及目录，再在一个事务内替换函数及绑定。备份失败或 DDL 失败会中止事务。中断后重新检查实际定义，重试时使用新的备份路径。备份含可执行函数体，须当作私有恢复材料并确认来源。

## 恢复与复核

如需把同一库恢复为先前捕获的定义，用该库的 version 2 私有备份。恢复前会另外保存当下定义，并校验数据库名、代际、格式、函数集合和触发器。文件需要包含四个函数及一组完整的已知评论触发器。

```sh
./db-function-repair -restore '<受控备份目录>/projection-before.json' \
  -confirm-database '<精确数据库名>' \
  -backup '<受控备份目录>/projection-before-restore.json'
./db-function-repair
```

函数与绑定的修复和恢复只改定义。已有热度事件与统计修复需要独立核对来源。恢复为偏离当前基线的定义后，应用会在结构校验时拒绝启动。先在已安装的隔离库上运行 `go run ./cmd/db-initialize` 验证完整指纹，再启动配套 API 和后台任务检查就绪。发行回退应恢复与发行版本配套的数据库快照。

## 验证边界

真实 PostgreSQL 18.6 专用库的回归覆盖错误目标及代际、备份失败、函数和绑定 DDL 回滚、重复应用、恢复原定义和评论状态差分。函数修复及故障注入使用测试自建子库，创建 event trigger 的权限仅用于该测试实例。评论差分使用隔离状态目录 `resources.json` 记录的 `database` 作为 `OWNED_DATABASE_NAME`。

```sh
python3 -B tools/testing/isolated_environment.py --state "$OWNED_STATE" run -- \
  go test -race ./cmd/db-function-repair ./internal/database \
  -run '^(TestOCT02ProjectionFunctionForwardRepairAndRecovery|TestProjectionRestore|TestFunctionBackup)' -count=1 -v
python3 -B tools/testing/isolated_environment.py --state "$OWNED_STATE" run -- \
  env MCMODS_DB_FUNCTION_REPAIR_TEST_TARGET="$OWNED_DATABASE_NAME" \
  go test -race ./internal/database -run '^TestOCT02CommentPopularityStatementDeltas$' -count=1 -v
```

未指定已核验的测试库名时，评论差分用例会跳过。generation 168 的历史发布顺序和当时的修复证据保留在 [旧版文档与源码](https://github.com/mcmods-cn/mcmods-cn-backend/blob/d14e10b4dcdeab3c7e2277dd371efa0249888eb8/docs/database-function-repair.md)。
