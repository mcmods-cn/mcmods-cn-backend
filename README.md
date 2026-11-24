# MCMods-cn 后端

本站的 HTTP API、审核和后台任务使用 Go 与 PostgreSQL。玩家在游戏里生成资料包，编辑者在前端选定项目与资料版本，后端核验原包、保存修订并控制发布。涉及游戏对象的生产规则在 [exporter](https://github.com/mcmods-cn/mcmods_exporter)，页面与浏览器传输在 [frontend](https://github.com/mcmods-cn/mcmods-cn-frontend)。

上线和回滚前，核对验收绑定的三仓提交、generation 171、发行产物及目标环境待验项。[当前版本与验收](docs/governance.md#当前版本与验收) · [系统与权限地图](docs/system-map.md) · [安全复核](docs/security-review-2026-11.md)

## 本地启动

使用 [go.mod](go.mod) 声明的 Go 1.26.11 工具链。CI 采用 PostgreSQL 18.6。开发者可以用仓库脚本启动独立的 PostgreSQL、Redis 和带 JetStream 的 NATS。首次运行需要 Python 3.9+、本机 Docker daemon 以及下载固定镜像的权限。

```sh
go version
python3 -B tools/testing/isolated_environment.py --go "$(command -v go)" up
. .audit-test-environment/activate.sh
APP_ADDR=127.0.0.1:8081 FRONTEND_ORIGIN=http://127.0.0.1:3001 go run .
```

访问 `http://127.0.0.1:8081/ready` 检查依赖，读取 `/api/v1/site/config` 验证一次数据库请求。前端开发服务应使用上面配置的 origin。签名、邮件、OSS、OAuth、AI、外部项目源和 Typesense 分别配置，基础浏览与登录可在隔离栈内验证。首次空库的合成管理员密码在工具创建的私有环境文件中。[隔离环境与清理](docs/testing-isolated-environment.md)

在自管环境下从 [.env.example](.env.example) 配置数据库、JWT 签名、设置加密密钥与前端 origin。密钥在服务端注入，浏览器只读取前端的公开构建变量。启用反向代理时配置可信网段。多副本需要共享限流与 Redis 故障策略，管理员通过权限受控的设置接口管理供应商凭据。[认证撤销](docs/session-revocation.md) · [共享限流](docs/redis-rate-limiting.md) · [OAuth](docs/oauth-settings.md)

使用 Docker 的 BuildKit 构建器运行 `docker build -t mcmods-backend .`。需要组织代理 CA 的环境可在构建命令中加入 `--secret id=proxy_ca,src=/path/to/approved-ca.pem`，仅在下载 Go 模块时将该证书追加到系统信任链。

## 修改入口

| 任务 | 从这里开始 |
| --- | --- |
| 新增页面所需的 API | 在 `internal/httpapi/server.go` 找邻近路由和鉴权入口，依业务 ID 在事务内校验对象范围，复用对应领域的 DTO、持久化函数、响应格式与前端 `app/_lib` 客户端。 |
| 增加一种目录资料或导入数据 | 对照 exporter 的 schema、真实包与 `catalog_import_*` 领域。将源观察与可编辑的站内定义分开，在相应业务层处理来源、版本、审核和派生呈现。[资料链与权限地图](docs/system-map.md) |
| 增加后台任务 | 查 `internal/queue/events.go`、`internal/queue/nats.go`、`internal/config` 与 `internal/app/runtime.go`，随业务事务写入 `queue.EnqueueTx`，在消费端校验当前权限、状态和幂等键。先复用已有任务、恢复入口和指标。[事件与发件箱](OUTBOX_JETSTREAM_DESIGN.md) · [恢复](docs/queue-recovery.md) |
| 修改公开目录或缓存 | 在发布事务中更新 `catalog_dataset_state`，读取使用 `internal/catalogvisibility` 及 `writeCachedCatalog`。认证和用户私有数据走独立路径。[公开目录](docs/public-catalog-source-policy.md) · [缓存键](CACHE_KEYS_AND_INVALIDATION.md) |
| 修改文件处理 | `internal/httpapi/oss_upload_byte_binding*.go` 处理拥有者、真实字节与完成语义。`oss_deletion_outbox.go` 负责登记删除和重试。导入原件、站内衍生媒体与公开文件有各自的读取条件。[存储布局与字节绑定](docs/oss-object-layout.md) |
| 增加数据库实体 | 在 `internal/database/schema.go` 登记领域定义。测试用同一个 `database.Initialize` 安装空库，涉及约束和事务的行为在真实 PostgreSQL 验证。 |

## 验证

从已经激活的专用测试环境分别运行基础门和数据库门。基础门也使用该环境的 PostgreSQL，部分 HTTP 测试会创建独立子库。每个 opt-in 测试的 SKIP 含义见隔离环境说明。

```sh
env -u MCMODS_TEST_DATABASE_URL MCMODS_RUN_DB_INTEGRATION=0 MCMODS_RUN_ACTIVITY_LOAD=0 go test -mod=readonly ./... -count=1
go vet -mod=readonly ./...
go build -mod=readonly ./...
go run ./tools/testing/db_suite
go test -race -p 1 ./internal/queue ./internal/querycache -count=1
```

账号与收藏链使用前端 `test:live`。配套驱动可以在新隔离范围内启动、校验并回收数据库与进程，清理前重验创建时记录的数据库 OID、所有者和随机标记。游戏包验证应保留导出原件、生产提交和 SHA-256。[账号和服务验证](docs/testing-isolated-environment.md#实际服务检查与测试) · [游戏包联调](docs/testing-isolated-environment.md#真实游戏包经浏览器和-jetstream-的验证) · [当前门禁结果](docs/governance.md#当前版本与验收)

## 数据库安装、发布和恢复

在已确认用途的 PostgreSQL 18.6 空库配置服务端环境，安装 schema 后再启动 API 与 Worker。安装 DDL、必需权限和禁用密码登录的系统身份共用一个事务。进程启动时用同一入口核对版本、DDL 定义指纹和实际 catalog 指纹。当前基线为 generation 171。外部更新日志和镜像下载分别以所属项目及供应商身份定键。项目换来源后仍保留自己的已发布内容和人工修订。版本或结构不符时拒绝启动。旧库用对应应用版本读取，只有已核实归属的开发库才可重建。[隔离环境重建](docs/testing-isolated-environment.md#重建任务自有环境)

计算目录指纹时，安装事务使用 PostgreSQL 默认的间隔表示（`IntervalStyle=postgres`）和标识符引用方式（`quote_all_identifiers=off`）。此前用这两项默认值安装的 generation 171 可以直接复核。如果旧开发库使用其他格式写入了指纹，先用原版本确认实际结构和备份，再在已确认归属的空库初始化。保留旧库及其备份供核对。

```sh
go run ./cmd/db-initialize
# 通过私密环境注入 SEED_ADMIN_PASSWORD，可选设置 SEED_ADMIN_EMAIL，再运行一次。
go run ./cmd/db-initialize --bootstrap-admin
```

`--bootstrap-admin` 可在空库直接执行，已有安装只能成功一次。部署配置在操作结束后撤掉初始密码。重启保留管理员变更过的权限、账号状态和站点设置。[功能与验证台账](docs/governance.md#当前版本与验收) · [数据库函数恢复](docs/database-function-repair.md)

将与源码配对的 PostgreSQL 备份恢复到隔离空库，再以目标库的 `DATABASE_URL` 运行 `go run ./cmd/db-initialize` 复核版本和结构指纹。备份和恢复的参数使用 `pg_dump --format=custom --no-owner --no-privileges` 和 `pg_restore --single-transaction --exit-on-error --no-owner --no-privileges`。恢复后核对样本业务记录，并校对独立托管的数据库授权、对象来源和队列进度，再接入应用。回退时保留与对应源码版本配套的镜像和快照。[实库恢复验收](docs/governance.md#当前版本与验收) · [对象生命周期](docs/oss-object-layout.md)

准备数据库及已启用的依赖，再启动同时承载 API 和后台 Worker 的后端进程。`/live` 判断进程，`/ready` 返回依赖状态与初始化失败原因。启用 JetStream 时另核对实际 durable 的就绪、积压和消费进度。前端按目标公开域名与代理路径构建，API 信任的代理网段要与实际部署一致。[队列运维](docs/queue-recovery.md) · [前端部署配置](https://github.com/mcmods-cn/mcmods-cn-frontend#readme)

GitHub `main` 上的构建结果可从 [Actions](https://github.com/mcmods-cn/mcmods-cn-backend/actions) 查看。容器发布入口为 `v*` 标签或手动工作流。过去的压测、审计与整改可从[历史索引](docs/history/README.md)追溯。[发布容器工作流](.github/workflows/release-container.yml)
