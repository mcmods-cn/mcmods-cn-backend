# 可丢弃集成测试环境

`tools/testing/isolated_environment.py` 创建本任务拥有的 PostgreSQL、Redis 和 NATS，
不会重置环境里已有的 `DATABASE_URL`。服务只发布到随机的回环端口，每个容器带随机所有权标签。
数据库名、数据库密码、应用密钥和合成管理员密码随机生成；状态目录权限为 0700，环境文件为 0600。
不要打印、提交或将这些文件作为 CI artifact 上传。

## 前提与初始化

需要 Python 3.9+、项目 `go.mod` 声明的 Go 1.26.6 和可用的本地 Docker daemon。
工具明确使用 `/var/run/docker.sock`，不使用继承的远程 Docker context/host。
官方镜像固定 digest：PostgreSQL 18.6、Redis 7.4.11、NATS 2.11.17。
镜像来自官方 Docker Library 的 ECR 公共镜像；首次运行需要下载权限和足够磁盘空间。
没有 Docker 时，本项目也可以使用原生回环服务，但必须自行建立并核实专用可丢弃数据库，
不能把已有连接串作为清空目标。

在后端仓库根目录运行：

```sh
python3 -B -m unittest discover -s tools/testing -p '*_test.py' -v
python3 -B tools/testing/isolated_environment.py --go "$(command -v go)" up
. .audit-test-environment/activate.sh
```

初始化调用项目自己的 `cmd/db-reset`，仅针对刚创建的数据库，且设置准确的 `RESET <database>` 确认。
之后恢复 `APP_ENV=test`、`DB_RESET_ON_START=false`。PostgreSQL 为完整 schema 和并行临时 schema 测试设置
`max_locks_per_transaction=512`；普通默认值在这类测试中可能不足。工具不会改变已有数据库服务器配置。

`MCMODS_SKIP_DOTENV=true` 防止配置加载器从工作区祖先目录读取私有 `.env`。
SMTP、Typesense 与外部供应商凭据被关闭或清空；AI 测试使用确定性 HTTP fixture。
NATS 和 Redis 使用随机 namespace，不能连到生产服务。

## 实际服务检查与测试

```sh
python3 -B tools/testing/isolated_environment.py run -- go test ./...
python3 -B tools/testing/isolated_environment.py run -- go vet ./...
python3 -B tools/testing/isolated_environment.py run -- go test -race ./internal/queue ./internal/searchindex ./internal/activity
python3 -B tools/testing/isolated_environment.py run -- go run ./tools/testing/db_suite
```

`db_suite` 会发现并逐批执行数据库测试；查看每项 PASS/FAIL/SKIP 和末尾 batch 汇总，
不能把成功退出等同于所有条件测试均执行。独立的外部 exporter 样本、Minecraft loader 在线数据
和 Modrinth 元数据测试仍需要各自显式条件。它们与真实 AI 供应商测试不同，不会由本工具偷偷启用。

默认单元/race门和真实DB门应区分环境。若运行默认门，显式取消
`MCMODS_TEST_DATABASE_URL`并设`MCMODS_RUN_DB_INTEGRATION=0`；应用`DATABASE_URL`
仍只能指向本任务拥有的回环库。真实DB门设`MCMODS_RUN_DB_INTEGRATION=1`，
并使专用测试URL与应用URL精确匹配。不要以RUN=0配合非空专用URL运行：旧opt-in测试
可能因此被启用，再被schema安全守卫拒绝。

五项会临时改动 public 对象或注入故障的回归另需显式独占目标：评论热度差分、日志前向升级、
函数修复/恢复、收藏导出终态事务及库存工具的表/视图区分。先核对状态文件的随机所有权标签、实际回环端口和数据库身份，
确认该库为本任务新建、可丢弃且没有其它测试/应用使用，再将
`MCMODS_DB_FUNCTION_REPAIR_TEST_TARGET` 设置为该状态文件记录的精确数据库名。
不能从继承的连接串或名称包含 test 推定这些权限。独占目标确认后可单独执行：

```sh
python3 -B tools/testing/isolated_environment.py run -- go test -race -p 1 -parallel 1 ./internal/database ./internal/httpapi ./tools/audit/database_inventory \
  -run '^(TestOCT02CommentPopularityStatementDeltas|TestOCT02LogRedactionForwardUpgradeIntegration|TestOCT02ProjectionFunctionForwardRepairAndRecovery|TestOCT02FavoriteExportTerminalStateAndNotificationCommitTogetherIntegration|TestInventoryColumnsExcludeViewsIntegration)$' \
  -count=1 -v
```

包及用例串行，避免临时 public 对象/元数据故障注入干扰其它回归。没有显式目标时五项会 SKIP；
另一次独占命令的 PASS 不改变原整套命令的 SKIP 数。
恢复步骤及测试角色要求见[函数修复说明](database-function-repair.md)和[日志升级说明](log-redaction-upgrade.md)。

初次应用当前 schema 后可启动真实后端：

```sh
APP_ADDR=127.0.0.1:8081 FRONTEND_ORIGIN=http://127.0.0.1:3001 go run .
```

另一个已加载相同测试环境的终端中启动前端。前端使用 lockfile 执行 `npm ci`，然后：

```sh
NEXT_PUBLIC_API_BASE_URL=http://127.0.0.1:8081 \
NEXT_PUBLIC_SITE_URL=http://127.0.0.1:3001 \
NEXT_PUBLIC_YGGDRASIL_API_ROOT=http://127.0.0.1:8081/api/yggdrasil/ \
npm run dev -- --hostname 127.0.0.1 --port 3001
```

前端 `npm run test:live` 只接受显式回环地址和专用合成账号。设置
`MCMODS_LIVE_FRONTEND_ORIGIN`、`MCMODS_LIVE_API_ORIGIN`、`MCMODS_LIVE_ACCOUNT`、
`MCMODS_LIVE_PASSWORD` 后执行。合成密码从私有环境读入，不应写在命令历史中。
这项测试使用实际 API、session cookie 和持久化；常规 `test:browser` 使用受控 API fixture，边界不同。
浏览器二进制应通过项目 Playwright 1.62.1 安装；设置与安装位置一致的 `PLAYWRIGHT_BROWSERS_PATH`。

常规生产产物 browser fixture 要用它自己的编译地址构建：

```sh
NEXT_PUBLIC_API_BASE_URL=https://api.example.test \
NEXT_PUBLIC_SITE_URL=https://www.example.test \
NEXT_PUBLIC_YGGDRASIL_API_ROOT=https://api.example.test/api/yggdrasil/ npm run build
npm run test:browser
```

不要用真实 API 地址构建同一个产物后，假定只拦截 `api.example.test` 的 SSR fixture 仍然生效。
实际联通和 fixture 可用两个独立工作区运行，避免 dev/build 同时写同一个 `.next`。

Typesense 的真实回环服务是可选的单独测试，见 [搜索投影说明](typesense-search.md)。
所有这些通过结果都不证明生产数据健康、备份可恢复或真实供应商语义质量。

## 临时 schema 安装回归

`InstallEphemeralSchema` 为每次安装编译函数资格化模式，ASCII 语句通过组合模式单次扫描，
含非 ASCII 内容或折叠重名的情形保留原顺序转换。全部真实 schema 语句有逐字节等价回归。
不会跨安装缓存或改变临时命名空间、事务和清理规则。
这是测试工具的重复编译开销修复，不改变生产迁移。原有 30 秒热度回归预算保留。
完整安装、触发器行为、清理及 public generation 保持可用以下真实 PostgreSQL 回归验证：

```sh
python3 -B tools/testing/isolated_environment.py run -- go test -race -p 1 ./internal/database \
  -run '^(TestOCT03CEphemeralFunctionQualification.*|TestRefreshContentPopularityThresholdLookupIntegration|TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration|TestUserContentCreationFactsFollowAuthoritativeActorLifecycleIntegration)$' -count=1 -v
```

## 结束与失败恢复

先停止本次启动的前后端进程，然后：

```sh
python3 -B tools/testing/isolated_environment.py down
```

清理先核实所有容器的随机标签，再只删除状态文件中记录的容器 ID 及其所属匿名卷。
标签不匹配时拒绝整个清理。初始化中途失败也保留记录，可用 `down` 清理已创建的测试资源；
`up` 不覆盖已存在目录。工具不清理用户已有资源，也不发布共享云端环境。
若状态目录中另有修复备份或其他文件，清理保留这些文件及目录，并明确报告；
已删除的专用服务和凭据文件不会因目录非空被误报为清理失败。
