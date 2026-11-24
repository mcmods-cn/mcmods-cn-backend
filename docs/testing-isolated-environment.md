# 可丢弃集成测试环境

`tools/testing/isolated_environment.py` 创建本任务拥有的 PostgreSQL、Redis 和 NATS，
不会重置环境里已有的 `DATABASE_URL`。服务只发布到随机的回环端口，每个容器带随机所有权标签。
数据库名、数据库密码、应用密钥和合成管理员密码随机生成；状态目录权限为 0700，环境文件为 0600。
不要打印、提交或将这些文件作为 CI artifact 上传。

## 前提与初始化

需要 Python 3.9+、[go.mod](../go.mod) 声明的 Go 工具链 和可用的本地 Docker daemon。
工具明确使用 `/var/run/docker.sock`，不使用继承的远程 Docker context/host。
官方镜像固定 digest：PostgreSQL 18.6、Redis 7.4.11、NATS 2.11.17。
三个镜像都按内容摘要从 Docker Hub 的官方 `library` 仓库获取。首次运行需要能访问该仓库并有足够的本地磁盘空间。
没有 Docker 时，本项目也可以使用原生回环服务，但必须自行建立并核实专用可丢弃数据库，
不能把已有连接串作为清空目标。

在后端仓库根目录运行：

```sh
python3 -B -m unittest discover -s tools/testing -p '*_test.py' -v
python3 -B tools/testing/isolated_environment.py --go "$(command -v go)" up
. .audit-test-environment/activate.sh
```

工具先核对随机容器标签、回环端口与实际连接目标，再以 `APP_ENV=test` 调用 `cmd/db-initialize --bootstrap-admin`。合成管理员只创建在这套工具的主体库。PostgreSQL 为完整 schema 和并行临时 schema 测试设置
`max_locks_per_transaction=512`；普通默认值在这类测试中可能不足。工具不会改变已有数据库服务器配置。

`MCMODS_SKIP_DOTENV=true` 防止配置加载器从工作区祖先目录读取私有 `.env`。
SMTP、Typesense 与外部供应商凭据被关闭或清空；AI 测试使用确定性 HTTP fixture。
NATS 和 Redis 使用随机 namespace，不能连到生产服务。

## 重建任务自有环境

`rebuild` 对照旧状态中记录的本地容器、回环端口、PostgreSQL 库名与所有者。新环境使用另一状态目录，以及全新的数据库、Redis、JetStream 和可选 Typesense。核验后可显式清理旧状态。保存过备份的目录会留下备份文件。

```sh
python3 -B tools/testing/isolated_environment.py \
  --state "$old_state" --replacement "$new_state" rebuild
python3 -B tools/testing/isolated_environment.py --state "$new_state" run -- go test ./internal/database
python3 -B tools/testing/isolated_environment.py --state "$old_state" down
```

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
每批 `DB_SUITE selection` 列出从 Go 实际发现、传给该次测试进程的顶层用例。对照该批的 PASS、FAIL、SKIP 按包和名称核对；子用例按完整名称单列，选中但没有终态的用例保留为未完成。托管 PostgreSQL 步骤在进程返回后输出 `DB_SUITE runner_exit` 并将同一退出码交给 CI。作业被外部中断时可能没有该行。

复现 CI 空库门时，使用全新且已核实归属的专用 PostgreSQL 18.6 数据库，以 `go run ./cmd/db-initialize` 安装 generation 172。账号浏览联调使用单独的合成身份。记录实际源码提交、数据库代际、每批终态和仍跳过的用例，恢复与重跑范围以同一源码为准。[批次工具](../tools/testing/db_suite/README.md) · [当前执行结果](governance.md#当前版本与验收)

默认单元/race门和真实DB门应区分环境。若运行默认门，显式取消
`MCMODS_TEST_DATABASE_URL`并设`MCMODS_RUN_DB_INTEGRATION=0`；应用`DATABASE_URL`
仍只能指向本任务拥有的回环库。真实DB门设`MCMODS_RUN_DB_INTEGRATION=1`，
并使专用测试URL与应用URL精确匹配。不要以RUN=0配合非空专用URL运行：旧opt-in测试
可能因此被启用，再被schema安全守卫拒绝。

三项需要直接改变主测试库的 public 对象，分别是评论热度差分、收藏导出终态事务及库存工具的表/视图区分。先核对状态文件的随机所有权标签、实际回环端口和数据库身份，
确认该库为本任务新建、可丢弃且没有其它测试/应用使用，再将
`MCMODS_DB_FUNCTION_REPAIR_TEST_TARGET` 设置为该状态文件记录的精确数据库名。
不能从继承的连接串或名称包含 test 推定这些权限。独占目标确认后可单独执行：

```sh
python3 -B tools/testing/isolated_environment.py run -- go test -race -p 1 -parallel 1 ./internal/database ./internal/httpapi ./tools/audit/database_inventory \
  -run '^(TestOCT02CommentPopularityStatementDeltas|TestOCT02FavoriteExportTerminalStateAndNotificationCommitTogetherIntegration|TestInventoryColumnsExcludeViewsIntegration)$' \
  -count=1 -v
```

包及用例串行，避免临时 public 对象或元数据故障注入干扰其它回归。没有显式目标时三项会 SKIP；
另一次独占命令的 PASS 不改变原整套命令的 SKIP 数。
函数修复用例使用独立子库。恢复步骤及测试角色要求见[函数修复说明](database-function-repair.md)。

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

在支持 pidfd 的 Linux 上可用 Python 3.10+ 运行账号旅程驱动。驱动使用当前后端模块独立构建，以 Go 源文件、`go.mod` 和 `go.sum` 的哈希判断专属二进制能否复用。为前端从提交导出独立副本，保留原 `package-lock.json`，解包时排除 `.env*`。在副本运行 `npm ci`，并安装 Playwright 所需的 Chromium。后端源、前端源和本次运行目录各自独立。[驱动入口](../tools/testing/live_acceptance_v2.py)

```sh
frontend="$(cd ../mcmods-cn-frontend && pwd)"
acceptance_dir="$(mktemp -d)"
mkdir "$acceptance_dir/frontend"
git -C "$frontend" archive HEAD | tar --exclude='.env*' -xf - -C "$acceptance_dir/frontend"
(
  cd "$acceptance_dir/frontend"
  npm ci
  npx playwright install chromium
)
python3 -B tools/testing/live_acceptance_v2.py \
  --state "$PWD/.audit-test-environment" --frontend "$frontend" \
  --scope "$acceptance_dir/run" --frontend-copy "$acceptance_dir/frontend" \
  --adopt-prepared-copy all
```

此驱动从已核验的服务创建专用测试数据库和合成账号，保留测试退出码和隔离范围内的运行证据。源码记录、会话和运行环境文件留在私有目录。清理失败时先核对该目录的状态，再针对同一范围执行 `cleanup`。第三方登录、真实邮件及付费供应商按各自的外部验证流程处理。[清理和失败恢复](#结束与失败恢复)

常规生产产物 browser fixture 要用它自己的编译地址构建：

```sh
NEXT_PUBLIC_API_BASE_URL=https://api.example.test \
NEXT_PUBLIC_SITE_URL=https://www.example.test \
NEXT_PUBLIC_YGGDRASIL_API_ROOT=https://api.example.test/api/yggdrasil/ npm run build
npm run test:browser
```

不要用真实 API 地址构建同一个产物后，假定只拦截 `api.example.test` 的 SSR fixture 仍然生效。
实际联通和 fixture 可用两个独立工作区运行，避免 dev/build 同时写同一个 `.next`。

启用真实 Typesense 的全仓回归要同时提供专用回环 URL、测试密钥和服务端可执行文件，因为作者目录测试会独立启动和停止一个进程。Redis 冷重启和 SSE 测试也要用 `MCMODS_TEST_REDIS_BINARY` 指向本任务可用的 `redis-server`。仅设置已运行的服务地址无法执行这两种进程测试。[搜索测试配置](typesense-search.md)
所有这些通过结果都不证明生产数据健康、备份可恢复或真实供应商语义质量。

### Redis、JetStream、Typesense 专项 CI

在 Linux x86-64、Docker、Bash 和 `go.mod` 指定的工具链上，从后端根目录执行与
[Real service integration](../.github/workflows/real-services.yml) 相同的门禁。
`--real-services` 复用前述三个服务及 PostgreSQL 初始化，再启动固定摘要的 Typesense 29.0。
工具从本次容器复制 `redis-server`、`typesense-server` 到私有目录，供控制进程停启的测试使用。

```bash
set -euo pipefail
state="$(mktemp -d)/services"
trap 'if test -f "$state/resources.json"; then python3 -B tools/testing/isolated_environment.py --state "$state" down; fi' EXIT
python3 -B tools/testing/isolated_environment.py --state "$state" --real-services up
cases=$(python3 -B tools/testing/real_service_cases.py pattern)
python3 -B tools/testing/isolated_environment.py --state "$state" run -- \
  go test -mod=readonly -race -count=1 -p=1 -parallel=1 -timeout=10m -json -run "$cases" \
  ./internal/{httpapi,querycache,queue,searchindex} | \
  python3 -B tools/testing/real_service_cases.py verify
```

| 服务 | 真实运行边界 |
| --- | --- |
| Redis | 共享服务测试限流、在线计数、RBAC 到期和撤权后的新请求。未读计数回归用两个 Redis 客户端验证全局通知失效后仍从权威源重建计数。两个 API 通过独立 PostgreSQL 连接池和 Redis 客户端验证用户权限、全站 RBAC、项目 ACL 和认证版本，覆盖旧请求返回、缓存丢失和提交后通知失败。SSE 与可选 Redis 用例另启私有进程，实际停止或冷重启。在线状态 HTTP 用例同时运行本地回退和共享 Redis 两个子项。 |
| NATS | 专用容器提供文件型 JetStream，覆盖发布确认、重投、去重、并发、积压、长任务、容量与死信。服务端重启用例在 Go 测试进程内启停真实 NATS 服务，保留其文件数据。 |
| Typesense | 共享回环服务覆盖真实建库、检索、重建和从 PostgreSQL 重查公开状态。作者目录游标用例另起 Typesense 进程并独占子数据库。 |

[用例清单](../tools/testing/real_service_cases.py) 同时生成筛选表达式并核对 Go 的逐项结果。
每项都必须收到运行和通过事件。跳过、漏选、子项失败或包未正常结束都会使专项失败。
CI 上传的 JSON Lines 仅含包、测试名、结果、秒数。私有鉴权环境和复制的二进制随服务清理。

### AI 配置的真实浏览器联调

用上面的专用 PostgreSQL 环境，在后端创建新的运行目录。夹具自动创建三个合成身份。

```sh
export MCMODS_AI_CONFIG_BROWSER_DIR="$(mktemp -d)/run"
MCMODS_RUN_DB_INTEGRATION=1 MCMODS_RUN_AI_CONFIG_BROWSER=1 \
  go test -count=1 -timeout=13m -run '^TestAIConfigLiveBrowserFixture$' -v ./internal/httpapi
```

等待夹具就绪，从 `runtime.json` 取 `apiOrigin`。在前端目录安装锁文件依赖和 Playwright Chromium，
以此为 `NEXT_PUBLIC_API_BASE_URL`，以 `http://127.0.0.1:3329` 为 `NEXT_PUBLIC_SITE_URL`，
运行 `npm run dev -- --hostname 127.0.0.1 --port 3329`。生产构建继续要求 HTTPS。另一个终端在后端根运行实际浏览器驱动。

```sh
node tools/testing/ai_configuration_browser.mjs "$MCMODS_AI_CONFIG_BROWSER_DIR" /absolute/path/to/mcmods-cn-frontend
```

驱动核对读者零次 PUT、两位管理员同版本打开、写入 200、冲突 409、保留草稿并显式核对后保存 200。
夹具最终确认 PostgreSQL 同时保留两次修改，AI 任务计数为零。只分发结果和截图，保留运行目录中的会话文件为私有。

### 真实游戏包经浏览器和 JetStream 的验证

使用已初始化的隔离环境、干净的三个仓库检出和已安装依赖的独立前端目录。
[仓内固定的两种加载器样本](../internal/httpapi/testdata/game-20261121/provenance.json)记录生产提交、加载器、输入模组、原包大小与哈希。当前标准镜像与新版开发原包的验证组合及哈希见[当前验收](governance.md#当前版本与验收)。
下列命令在后端根目录运行。将两个仓库变量指向本次构建实际使用的干净检出目录，尤其是在 worktree 中验收时。通用浏览器夹具默认查找后端旁边的同名仓库，要求显式指定前端的专项继续使用该专项变量。前端使用空闲的回环端口。

```sh
export MCMODS_AUDIT_FRONTEND_REPO="$(cd ../mcmods-cn-frontend && pwd)"
export MCMODS_AUDIT_EXPORTER_REPO="$(cd ../mcmods_exporter && pwd)"
export MCMODS_AUDIT_FRONTEND_ORIGIN=http://127.0.0.1:3341
export MCMODS_AUDIT_JOURNEY_DIR="$(mktemp -d)"
sample_dir="$PWD/internal/httpapi/testdata/game-20261121"
sample_loader=fabric
export MCMODS_AUDIT_GAME_PACKAGE="$sample_dir/$sample_loader/export.zip"
MCMODS_AUDIT_EXPORTER_COMMIT=$(python3 -c '
import json, sys
print(json.load(open(sys.argv[1]))["exporter_commit"])
' "$sample_dir/provenance.json")
MCMODS_AUDIT_GAME_SHA256=$(python3 -c '
import json, sys
samples = json.load(open(sys.argv[1]))["samples"]
print(next(sample["zip_sha256"] for sample in samples if sample["loader"] == sys.argv[2]))
' "$sample_dir/provenance.json" "$sample_loader")
export MCMODS_AUDIT_EXPORTER_COMMIT MCMODS_AUDIT_GAME_SHA256
MCMODS_RUN_LIVE_GAME_JOURNEY=1 go test -count=1 -timeout=50m -v ./internal/httpapi -run '^TestAuditLiveGameJourneyFixture$'
```

复跑 Forge 包时，将上例的 `sample_loader` 改为 `forge` 并使用新建的测试目录。
历史 Fabric 包仍位于 `internal/httpapi/testdata/audit-20261024/`。使用其他原包时，填写其来源记录中的
`MCMODS_AUDIT_GAME_SHA256` 和完整 40 位 `MCMODS_AUDIT_EXPORTER_COMMIT`。
夹具核验原始字节和生产提交的存在，从 manifest 和模组注册表读取目标版本与命名空间。
仓库随附样本的合成 OSS 使用 4 MiB 以内的单次 PUT，样本须是单命名空间、完整导出，并包含该项目的实际 JEI 合成配方和可展示图片。有来源与哈希的真实大包另走[分片恢复及公开回放](../tools/testing/export_multipart/README.md)，使用同一上传会话继续导入。保留自有原包及生产环境记录供独立核对。

夹具初始化和释放临时 schema 时用 `flock` 串行化 DDL。默认锁位于当前用户缓存目录
的 `mcmods-cn/test-locks/postgres-ddl.lock`。多个进程共用测试库时，可给它们
设置同一个绝对路径 `MCMODS_AUDIT_PG_LOCK`，父目录须已经存在并归本任务管理。

等待日志出现 READY，在另一终端保留上面的目录和前端变量。
读取 `runtime.json` 中的 `apiOrigin` 与 `ossOrigin` 配置 Next 开发实例，
分别作为 `NEXT_PUBLIC_API_BASE_URL` 和 `NEXT_PUBLIC_CSP_CONNECT_ORIGINS`。
`NEXT_PUBLIC_SITE_URL` 使用声明的前端 origin，在该端口启动前端。准备好
Playwright Chromium 后，在对应前端根目录运行以下命令。

```sh
MCMODS_AUDIT_JOURNEY_RUNTIME="$MCMODS_AUDIT_JOURNEY_DIR/runtime.json" node --test browser-tests/audit-game-live.mts
```

浏览器报告中的四个阶段依次对应创建导入任务、Worker 就绪、另一账号审核成功、匿名页面像素核对。保留报告中的任务和修订 ID。
在发出 STOP 前，读取同目录的 `runtime.json`，等待 `databaseSnapshotPending=false`。其 `jobs` 和 `importRevisions` 来自数据库，`workerConsumerState` 来自这个专用 JetStream 消费者。对本轮单次上传，核对仅有一个对应的 ready 任务和生效修订，`delivered=acknowledged=1`，`pending=ackPending=redelivered=0`。原包应归属上传账号，配方应为一块橡木木板产出两根木棍，未知概率保持为空。
完成后
`touch "$MCMODS_AUDIT_JOURNEY_DIR/STOP"` 并停止本次前端。该夹具使用回环 HTTP、
真实 PostgreSQL 和文件型 JetStream、合成 OSS。请保管目录中的合成会话文件，
仅交接脱敏后的结果。测试不会运行新的游戏客户端。

若开发页在加载中反复刷新，检查浏览器开发连接是否收到 Turbopack 的
`reloadPage: HMR hash mismatch`。本轮更换检出提交和公开 API 地址后出现过这种情况。
停止本次 Next 进程，在同一提交和配置下使用新的开发缓存重启，保留旧缓存与失败报告。
重新操作前，检查浏览器报告和该项目的任务记录。已经得到 202 的上传可沿原任务 ID 查看进度和审核结果。

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
若上次清理中断，可用同一状态目录再次执行 `down`。工具先核对容器完整 ID，跳过已确认不存在的容器。标签不匹配或无法核实状态时保留记录并停止清理。初始化中途失败也保留记录，可用 `down` 清理已创建的测试资源；
`up` 不覆盖已存在目录。工具不清理用户已有资源，也不发布共享云端环境。
若状态目录中另有修复备份或其他文件，清理保留这些文件及目录，并明确报告；
已删除的专用服务和凭据文件不会因目录非空被误报为清理失败。
