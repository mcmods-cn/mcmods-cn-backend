# 隔离审查环境与复现

本记录用于 2026-10-01 审查；仅测试任务新建资源，不授权任何生产数据库或真实付费 AI 调用。运行值只保存在权限 0600 的临时 `env.sh`，不得加入提交或复制到报告。

## 已核实基线

前端 `4f82c9075636cc4f1f6d3eb07de37a3fffd1de58`，后端 `bd50afd0ef92192c40c5152ea56e27a3fdbb657f`。两仓库起始工作区干净、分支 `work`，获取远端后均与各自默认分支 `main` 相同。任务分支为 `codex/full-audit-db-i18n-20261001`。已读取云环境运行 skill、网络策略和全部项目级说明；后端没有 AGENTS 文件，前端适用 AGENTS 要求已阅读安装版本的 Next 官方指南。

初始环境 Debian 13、Node 24.19.0、npm 11.9.0。`/usr/bin/go` 实际是围棋程序，并非 Go 语言工具链。没有 sudo，系统 apt 写入失败；采用用户临时目录安装，无需 Docker/systemd。

## 工具安装

- Go 基线 1.26.5：官方 `go.dev/dl/go1.26.5.linux-amd64.tar.gz`，对照官方历史发行元数据校验 SHA256 `5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053` 后解压。
- Go 最终 1.26.8：必要安全补丁，官方归档 SHA256 `d0f743b33e8d8945e6b1f432edd15785c70507121d6e2a723b21285eddf8b57b`，保留旧工具链供基线红测试；最终命令使用新版本。
- PostgreSQL 17.11、Redis 8.0.2：沿系统 Debian 官方仓库，apt 列表与缓存设到用户临时目录，`apt-get download` 后使用 `dpkg-deb -x`，不执行未经核实的安装脚本。所需包为 `postgresql-17 postgresql-client-17 libpq5 redis-server redis-tools liblzf1`；Redis 便携启动需将解压的 `usr/lib/x86_64-linux-gnu` 加入 `LD_LIBRARY_PATH`。
- NATS Server 2.12.4：`go install github.com/nats-io/nats-server/v2@v2.12.4`，GOBIN 设置到专用临时工具目录。
- Typesense 30.2：从官方 `dl.typesense.org/releases/30.2/typesense-server-30.2-linux-amd64.tar.gz` 取得；本次归档 SHA256 `cd791605e7c6cc7be457794f534cd4b9f9d361781adb10e40020efc22840ebc3` 供复现固定版本（不是宣称独立取得官方 SHA 签名）。实际引擎测试已通过；CI 使用相同固定归档及校验。
- govulncheck 1.8.0、gitleaks：通过各项目 Go module 安装；仅本地扫描，不上传代码。
- 前端 lockfile 用 npm 维护；安装 Vitest、jsdom、Testing Library、Playwright，Chromium/FFmpeg/headless 浏览器从 Playwright 官方下载并实际启动。

普通有系统安装权限的机器可安装相同工具；无权限时使用上述提取方式。不要将本任务安装成功误解为后续任务环境已自动配置。

## 启动、初始化、检查

将下列路径变量设置为当前机器已安装工具的位置，不要复制本次工作区绝对路径：

```bash
export PG_BIN=/path/to/postgresql/17/bin
export REDIS_SERVER=/path/to/redis-server
export NATS_SERVER=/path/to/nats-server
# 可选真实搜索引擎；不设置则对应测试明确 SKIP。
export TYPESENSE_SERVER=/path/to/typesense-server
scripts/test-services.sh start
# 使用该命令实际打印的新建目录；不要使用未知原有目录。
export MCMODS_TEST_STATE=/printed/mcmods-test-services.XXXXXXXX
source "$MCMODS_TEST_STATE/env.sh"
"$PG_BIN/pg_isready" -h 127.0.0.1 -p 55432
REDISCLI_AUTH="$REDIS_PASSWORD" /path/to/redis-cli -h 127.0.0.1 -p 56379 ping
go run ./cmd/test-setup
go test -p 1 -count=1 ./...
go vet ./...
go build ./...
go test -race -p 1 -count=1 ./...
MCMODS_RUN_ACTIVITY_LOAD=1 go test ./internal/activity -run TestDurableOutboxConcurrentLoadIntegration -count=1 -v
go run .
```

`cmd/test-setup` 核对 APP_ENV、回环地址、数据库名/用户、任务目录身份标记、UID、随机密码与禁用 reset，拒绝未知目标。中央门禁同时检查启用的 Redis/NATS/Typesense 配置，数据库身份不会授权其他服务。NATS 和 Typesense 的独立 opt-in 与测试 URL/key 也会在各包 TestMain 触发门禁：URL 必须精确为对应回环地址与端口，禁止额外服务器、用户信息、路径、query、fragment；Typesense key 必须匹配同目录文件，NATS 必须匹配同目录配置。未启用某服务的零值配置不要求该服务存在。

服务只监听 127.0.0.1：PostgreSQL 55432、Redis 56379、NATS 54222、后端 18080。PostgreSQL 和 Redis 使用任务随机凭据；NATS 是同一隔离目录的匿名回环 JetStream 进程，不对其他回环进程提供强租户隔离。站点/支付/AI 供应商真实凭据为空；主站 Typesense 和 Yggdrasil 默认关闭。可选 Typesense 只监听127.0.0.1:58108（内部peering 58107），使用同任务目录随机key；显式测试用真实引擎，不自动启用业务搜索worker。

启动脚本要求 Linux `/proc`、`nohup` 与 `setsid`，三项非 PostgreSQL 服务在独立 session 启动并重定向 stdin/stdout/stderr，避免启动 shell 的进程组结束影响服务。在接受健康响应前后核实新子进程 PID、UID、精确配置参数和 LISTEN socket inode 属于该 PID；已有端口上的旧 PONG 或 health 响应不能代替新服务启动。启动 PID 保存在任务目录的 `*-started.pid`；缺少该证据的新启动不被判定 ready。便携二进制直接执行，不要用会改变 PID 或配置参数的包装命令。脚本不会因为端口冲突而停止原有进程；跨执行单元的存活仍须实际检查。

前端构建和浏览器联通步骤见前端 README。登录使用测试种子随机管理员密码；仅合成数据。停止本任务启动的后端、前端进程后，执行：

```bash
scripts/test-services.sh stop "$MCMODS_TEST_STATE"
```

所有七个集成包 TestMain 在连接前核对相同身份、旧 test URL 和实际启用的服务目标，不能仅凭 opt-in 操作未知服务。停止脚本使用显式失败关闭检查，不依赖会被 Python `-O` 移除的 assert。它核对确切目录、UID、标记、进程配置参数和 socket 所属 PID；Redis 会改写 argv，额外用随机 secret AUTH 后 INFO server 的 PID/config_file 核对，所有身份验证通过才发信号。已消失的 PID、同 UID 已退出的 Z 进程不再发送信号，也不阻塞停止本任务 PostgreSQL；其他 UID 仍拒绝。旧版本创建的本任务目录可缺少 launch marker，但仍须逐项通过 PID/config/UID/socket/Redis 随机凭据检查；这只适用于停止，不会放宽新启动的就绪证明。只停止本任务进程，保留证据，不删除未知资源。需要清理时再核验原创建身份，不凭数据库名含 test 执行删除。

无需连接固定端口或远端即可验证门禁：`go test -race ./internal/testenv -count=1` 使用合成状态文件；`python3 scripts/test-service-ownership.py -v` 使用本测试自行创建的随机回环监听进程，覆盖新 PID、旧任务停止身份兼容、精确 config、socket 归属、优化模式拒绝错误标记。`bash -n scripts/test-services.sh` 只验证 shell 语法，不代表服务启动成功。完整四服务真实启动/初始化/停止结果须另行记录。

本轮门禁实际回归：7 个 Go 顶层测试、含子场景 53 PASS（race）；14 个 Python 测试 PASS。原独立服务/目标校验新增回归失败，旧就绪接受无关 PONG、优化模式跳过停止标记、Z 进程阻塞停止和可选搜索继承旧环境也均保留安全红测。生成器测试只执行合成配置生成，不启动服务；日志断言仅报字段名，不打印随机凭据。

本轮 root 实际停止旧任务服务退出 0；新四服务启动退出 0，随后另一个执行单元的 PID/config/socket/真实健康检查与 PostgreSQL 就绪检查通过。独立复核再次使用相同 helper 检查新任务服务通过，没有重启或停止活跃服务；`cmd/test-setup` 实际完成迁移与 seed，日志输出成功。对应证据为 `/tmp/mcmods-release-service-stop-old.log`、`/tmp/mcmods-release-service-start.log`、`/tmp/mcmods-service-cross-exec-independent.log`、`/tmp/mcmods-release-setup.log`。此次真实启动使用脚本 SHA256 `229fa1703be9bd5982959130c18fc53d731e0941bf49d7702e48401b931bfb09`（90 行）；随后仅补两行设置三项可选 Typesense 环境变量默认空值，该差异由实际生成器红绿回归验证，没有把不同版本指纹冒充同一次完整启动。新全库回归、远端 CI 和最终新任务停止仍分别记录。

## 已执行基线与边界

- 后端原始基线：724 PASS、46 SKIP；真实 PostgreSQL/NATS 集成基线：789 PASS、6 SKIP，命令均退出 0。跳过项包括专用压测开关、未提供的 exporter 样本 ZIP 与可选真实上游网络验证。
- 前端原始 `npm run check` FAIL：global brace-expansion 5 override 与 minimatch 3 CommonJS 不兼容，`expand is not a function`。基线 production build PASS，但真实 Chromium 中登录页 17 个脚本受到 nonce CSP 阻止；构建成功不能证明可交互。
- 新代码完整回归、浏览器与 CI 结果单独持续更新，不能把本文件的安装步骤当作通过记录。
- 当前没有取得生产运行、复制/备份恢复、容量/膨胀或真实 AI 供应商质量与计费证据。未调用真实付费翻译端点。

详细文件覆盖和问题状态见 `file-ledger.json`、`review-*.json` 及各专项报告。历史报告记录不同版本/平台，不能作为本次测试结果继承。

## 执行中的补充证据

本轮真实全库 `go test -p 1 -race -count=1 -json ./...` 退出 0，625 个顶层测试通过，包含子场景共 971 PASS、6 SKIP；之后有新修复，最终回归另记。显式活动负载已单独通过。旧样本 ZIP 和真实上游用例未被此结果代替。`go vet ./...` 退出 0。

`govulncheck ./...` 在 Go1.26.8/compress1.18.7/image0.45.0/crypto0.56.0 后退出 0：0 个可达公告，1 个未使用模块公告（不宣称整个依赖树无公告）。标准库和 BMP 的基线可达命中保留为依赖修复依据。

历史 gitleaks 扫描：前端无命中；后端一个 generic-api-key 命中为已逐行核实的合成测试 fixture，未证实真实凭据泄露。范围限于当前可访问 Git 历史；报告不复制命中值。

代表性执行计划：`MCMODS_AUDIT_PLAN_OUTPUT=/absolute/output.json go test ./internal/httpapi -run TestRepresentativeCatalogAndActivityQueryPlansIntegration -count=1 -v`，新建带身份标记的独立 PG 数据库、10,000 合成 mod（90% approved）及10,000活动事件，事务回滚后清理专用数据库。实际计划见 `evidence/query-plans.json`，不推断生产吞吐或增长后表现。

CI 新增 Go vet/race/build/可达依赖扫描与隔离服务集成任务。GitHub Ubuntu runner 使用其发行版 PostgreSQL，明确输出实际版本；本任务本地实测为17.11。远端结果需要 PR 创建后另行核实，不把 workflow 存在描述为 CI 已通过。

最终源码回归普通与全库 race 各1366 PASS/5 SKIP（730顶层PASS/5顶层SKIP），显式活动负载和真实Typesense集成均PASS。前端37文件156测试、59页面构建及真实生产浏览器6项通过；精确命令/版本、原失败与原日志SHA见两仓validation-root.json，读取覆盖另在file-ledger/coverage，不混作测试覆盖。

任务结束：先核对应用PID的UID、cwd、exe、argv及stdout/stderr确为本任务日志，再TERM停止本任务前后端；随后 `bash scripts/test-services.sh stop "$MCMODS_TEST_STATE"` 核验全部拥有者并停止四服务。实际退出0，临时合成数据目录和脱敏证据保留，没有清理未知资源或修改共享环境。前台复现启动的应用可用其终端Ctrl-C关闭；不要按端口猜PID杀进程。原始日志仅当前任务可取，PR中提交的是机器摘要及SHA。
