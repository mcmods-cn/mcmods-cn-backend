# 历史资料索引

新维护者从[后端 README](../../README.md)和[系统地图](../system-map.md)开始。下列材料保留当时的测试条件、失败和整改证据。查某次审查发现时，先核对记录中的提交或文件哈希，再查当前实现和[本轮验收](../governance.md)。

| 资料 | 内容与适用范围 |
| --- | --- |
| [早期版本的实现与实测](earlier-releases/) | 旧代际下的权限、会话、反滥用、缓存、负载和功能交付报告。原文件从仓库根目录迁入，正文及当时记录的失败原样保留。原始相对路径以原提交的仓库根目录为起点。 |
| [全项目审计](../audit/full-project-audit/) · [整改历史](../remediation/full-audit-remediation/) | 当时的功能、路由和表清单，以及分阶段整改决策。提交时的待办、暂停标记与开发机参数仅供当时的工作追溯。 |
| [较新专项审计](../audit/) | 按业务定位真实游戏包、活动原子性、文件绑定和回收、审核及第三方协议证据。需要运行时，采用当前源码和专用隔离环境。 |
| [资料发布策略](../audit-catalog-import-policy-upgrade.md) · [通知可见性](../audit-messaging-visibility-upgrade.md) · [日志脱敏](../log-redaction-upgrade.md) · [OSS 字节绑定](../audit-oss-byte-binding-upgrade.md) · [队列公开 ID](../audit-identity-oss-upgrade.md) | generation 168 扩展的历史说明。当前空库安装包含仍使用的约束和规则，一次性升级命令可从当时的 Git 提交查阅。 |
| [早期负载输出](../../load-results/) · [历史测试输出](../../test-results/) | 附带的数据量与依赖有专门说明。生产容量和当前版本性能均须在目标数据规模重新测量。 |

基线 `21724a42fd0dd84935fa0b4b7cf75c123a191500` 仍可还原旧路径。删除的 `MCMods-full-project-audit-2026-08-21.zip` 内 17 个文件与 `docs/audit/full-project-audit/` 对应文件逐字节相同，ZIP 的 SHA-256 为 `8f1e45681736bb1fa0977c0e4cec0d87c3ad74d9bf42eb16d9c5807ab1a49500`。旧 `IMPLEMENTATION_PLAN.md` 记录 generation 71 的一次实施，压测和审核证据已分别归档。当前数据恢复遵循现行代际的专用文档。

第三轮开始时的旧开发数据库路径可通过以下变更追溯。当前安装只使用 [db-initialize](../../README.md#数据库安装发布和恢复)，当前测试入口见[隔离验证](../testing-isolated-environment.md)。

| 原路径 | 对应变更及当前入口 |
| --- | --- |
| `cmd/db-reset/main.go`、`internal/database/migrator.go` | `89c69fe` 将空库安装和种子集中到 `cmd/db-initialize` 与 `internal/database/initialize.go`。隔离开发实例由带归属标记的环境工具重建。 |
| `internal/database/governance_automation_seeds_integration_test.go`、`internal/database/sec021_default_user_seed_integration_test.go` | `89c69fe` 将种子和重复启动保护移交当前 `initialize_integration_test.go`。新库只必需系统任务身份，交互式管理员由操作者显式初始化。 |
| `cmd/db-catalog-import-policy-upgrade/{main.go,audit_policy_cli_integration_linux_test.go}`、`internal/database/catalog_import_policy_schema.go` | `782327d` 移除扩展命令和旧库夹具。 `310b253` 将仍使用的发布模式约束并入当前 `catalog_import_jobs` 基线定义。 |
| `cmd/db-log-redaction-upgrade/{main.go,main_test.go}`、`internal/database/log_redaction_upgrade{,_integration_test}.go` | `782327d` 移除旧库升级与对应测试，现行日志版本约束随基线安装，见 `log_share_schema_integration_test.go`。 |
| `cmd/db-notification-visibility-upgrade/{main.go,audit_msg_review_cli_test.go}` | `782327d` 移除旧库升级。 `notification_visibility_schema.go` 保留当前通知读状态、事务标记和配置要求。 |
| `cmd/db-oss-byte-binding-upgrade/main.go`、`cmd/db-oss-multipart-activity-upgrade/main.go` | `48f02aa` 保留安装定义和校验规则，移除一次性扩展命令。回收和对象证明仍由现行 OSS 入口维护。 |
| `internal/config/database_reset_config_test.go` | `feb0176` 将测试环境准备改用当前安装入口。当前初始化测试验证已安装结构、不匹配代际和用户权限不被重置。 |
