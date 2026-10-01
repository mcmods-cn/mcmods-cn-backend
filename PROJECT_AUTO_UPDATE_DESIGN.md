# 项目自动更新设计

## 数据与能力

系统覆盖 Mod、插件、地图、光影、材质包和数据包。`project_external_sources` 只保存经在线验证的 Modrinth、CurseForge 或 GitHub 绑定；不会按同名项目自动绑定。每个项目在 `project_auto_update_settings` 中分别保存：

- `minecraft_versions`：Modrinth/CurseForge；
- `changelog`：Modrinth/CurseForge/GitHub；
- `site_downloads`：Modrinth/CurseForge/GitHub（GitHub 标记不推荐）。

周期为 week/month/quarter/half_year/year/never。“永不”强制关闭。Minecraft 兼容默认季度；仅在已验证 Modrinth/CurseForge 来源存在时默认启用，否则保持等待来源。更新日志和站内下载默认关闭。

## 合并和幂等

兼容关系按“Minecraft 版本 + 加载器”求并集，只新增站内注册表已识别组合，不自动删除或创建任意字符串；未映射值写入运行结果等待管理员处理。外部更新日志绑定实际按 `(source_type, external_release_id)` 唯一，并保存所属 `project_route_id`，未人工修改的来源记录可更新，`manual_override` 只记录外部变化而不覆盖正文。自动更新不会调用 AI 翻译。

调度和运行以 PostgreSQL 为事实来源，以 `SKIP LOCKED` 领取启用配置的任务。每次领取生成随机 `lease_owner`，租约 10 分钟、15 秒续约；租约丢失会取消运行上下文。兼容关系、更新日志、维护状态、文件登记和完成/失败记录都在各自写事务中锁定并复核任务租约，以及仍启用且未重绑的来源配置。迟到 worker 不能修改替代领取或发布旧来源。续约遇到自身短发布事务行锁时不阻塞发布；恢复器回收过期/缺失租约，累计领取达到 5 次进入 `dead_letter`，其余有界退避再试。任务结果与设置的最后状态同事务提交。

## 自动化身份和维护状态

管理员或项目编辑员仍是设置的 `configured_by`，但所有自动兼容关系、自动更新日志、自动文件任务和维护状态变更的业务发起者统一为不可交互登录的 `autobot`。运行表用 `actor_id` 记录该身份；自动更新日志的 `created_by`、内容修订 `ActorID` 和应用修订的操作者也都是该用户，避免历史记录错误归到配置管理员或随机活跃用户。

Worker 对每次成功读取到的提供方文件或更新日志取最新发布时间，持久化 `project_automation_activity.last_project_change_at`。只要已经获得权威上游时间，就不会用站内编辑时间掩盖一个实际上长期未维护的上游项目；尚无任何上游时间时才暂以项目创建时间作为安全回退：

- 严格超过 6 个自然月没有变化：自动设置为 `lowFrequency`（低频更新）；
- 严格超过 1 个自然年没有变化：自动设置为 `discontinued`（停止维护）；
- 发现较新的上游版本、更新日志或项目修订：恢复自动调整前的状态。

状态决策保存原状态、当前自动状态和 `changed_by=autobot`，因此不会把人工原状态永久丢失。若人工在自动调整后再次修改官方状态，系统把它识别为人工覆盖并暂停状态自动化；出现真正更新的项目活动后才重新评估。`archived` 永远视为人工决定，不由该策略覆盖。任何来源的一次真实更新都算项目发生变化，不会要求 Minecraft 版本和更新日志必须同时变化。

## 许可证与文件镜像

每次站内镜像前重新读取项目许可证并查询 `license_policies`。未知、自定义、ARR 或明确禁止再分发时失败关闭。`project.auto_update.redistribution_override` 可在填写原因和授权来源后覆盖，操作者及说明进入设置和审计。

下载客户端使用允许的 HTTPS 来源、DNS/IP SSRF 检查、超时、256 MiB 上限、项目类型扩展名白名单、来源大小与 SHA-1/SHA-512 校验。该专用客户端直接解析并连接目标，不继承隐式环境 HTTP 代理，防止只校验代理 IP；部署需要能够直连公共提供方，或另行实现经审查的可信代理配置。不会影响通用应用 HTTP 客户端。签名下载 URL 的查询参数保持兼容；API base URL 配置仍不允许 userinfo、query 或 fragment，重定向不得改变协议或 host。文件先以 `scanning` 存入隔离 OSS，安全扫描 clean 后才进入 ready；同一外部文件 ID 不重复镜像，相同 ID 但来源大小或提供方哈希变化会标记 `source_changed` 等待人工处理。公共镜像不计入用户个人额度。

## 权限与回滚

已认证开发者/项目编辑员通过项目变量权限，管理员通过 `project.auto_update.*` 查看、配置、运行及绑定来源。提交者身份不授予此权限；管理员手工授权仍可处理特殊情况。无权限时接口返回 403，详情页不显示入口。关闭配置不会删除已有兼容、日志或文件；Worker/NATS 不可用时 PostgreSQL 队列保留，可恢复扫描。

## 提供方契约、部署与验证边界

Modrinth 的 `version_number`、`date_published`、`version_type` 使用提供方字段解码，保留真实版本、发布时间和 beta/alpha 渠道。CurseForge 更新日志请求失败会使该运行失败，不能把 503 当成空正文成功发布。

这次自动更新修复复用已有租约字段，不需要新增 schema。替换 worker 时先停止并排空旧 worker，再启动支持随机领取和写入校验的版本；旧版本不具备迟到写入保护，不能与新 worker 混跑。已发布内容和待扫描文件保留；暂停/重新绑定配置阻止旧运行继续发布，后续启用配置按现有调度处理。

隔离 PostgreSQL 验证了旧 token、暂停、重绑、取消、过期/缺失租约恢复、事务发布与有限重试；本地受控 HTTP 验证了字段契约与提供方失败。本任务未实际调用第三方、上传 OSS 或检查生产运行健康。OSS 上传后、数据库登记前丢租或故障可能留下未登记对象；唯一对象键和最终数据库 fence 防止公开错误结果，但不证明跨系统严格只执行一次或零孤儿对象。
