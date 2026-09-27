# 复用与代码质量审计

## 已确认的权威复用

| 能力 | 权威实现 | 审计结论 |
| --- | --- | --- |
| 前端认证 | 统一 API 客户端 + auth 状态模块 | 无第二套登录状态 |
| 文件拖放/上传 | 通用 FileDropZone/文件上传流程 | 评论、举报等应继续复用 |
| 项目权限 | permission rules + project access resolver | 不应在 Handler 复制创建者判断 |
| 缓存 | querycache + 版本化权限键 | 权限失败回源正确 |
| 系统通知 | 模板注册/渲染 +可靠投递 | 仍有旧调用方待迁移 |
| Markdown | 单一渲染器和 AST 插件 | 楼层、Sticker 复用同一边界 |
| OSS/文件 | 文件服务、扫描和对象访问 | 没有为日志另建额度系统 |
| 异步任务 | PostgreSQL task/outbox + queue worker | 需要 JetStream 部署验证 |

## 主要质量问题

1. 四类系统通知仍直接写固定文案，调用方没有迁移到模板服务。这是“仍在使用但过时”的实现，不是死代码。
2. `project_editor_application_handlers.go` 的列表逐条查项目名，形成 N+1。
3. `admin-console.tsx` 和多个 Go Handler 体积过大，跨功能改动容易冲突。
4. 项目自动更新 API 响应使用 snake_case、请求使用 camelCase，前端组件手工桥接，偏离统一 API 风格。
5. 14 个 Go 文件未通过 `gofmt -l` 检查。
6. 一个无语义 `jsonUnmarshal` Wrapper 和一个无用前端参数可以直接清理。

## 映射审查

下列映射经调用链确认具有实际边界价值，应保留：

- `api-normalizers.ts`：把未知 API 数据转换为安全前端模型并过滤无效 ID。
- `editor-api.ts`：补充本地化、默认值、图标 URL、来源和绑定数量。
- catalog resource API 转换：隔离公开/编辑器协议。
- showcase 类型别名：后端实体代码到 i18n key 的显示边界。
- 语言别名：外部协议/历史语言代码到站内注册表。
- 数据库模型到公开响应：防止 ORM/敏感字段直接泄露。

没有确认发现可安全删除的一一字段 DTO 链，也没有建议直接把数据库实体暴露给前端。

## 接口与抽象

没有仅凭“单实现”删除接口。外部 HTTP、数据库、队列、缓存和测试替身接口都承担真实边界。审计没有确认可删除的单实现接口；后续重构仍需逐个验证 Mock 和替换需求。

## 格式与命名

`gofmt -l` 发现 14 个文件；TypeScript 严格额外检查只发现一个未使用参数。API 字段命名不一致集中在项目自动化模块，而不是全仓普遍问题。

## 专项报告

旧实现对照、实际调用方、A/B/C/D 分类和量化统计见 [13_CODE_STYLE_MAPPING_AND_LEGACY_RESIDUE_AUDIT.md](13_CODE_STYLE_MAPPING_AND_LEGACY_RESIDUE_AUDIT.md)。

## 改进顺序

1. 迁移通知旧调用方并补测试。
2. 批量加载编辑员申请的项目名称。
3. 清理无用 Wrapper/参数和格式问题。
4. 统一自动化 API 边界命名。
5. 以功能面板为单位渐进拆分巨型文件，同时保持公开 API 不变。

## Mod 导入链新增结论

- `mod_export_content_sync.go` 在同一函数内维护两份资源种类到板块模板的SQL CASE，并复制两份战利品分类规则；这是运行时业务映射的重复权威，编号 `REUSE-001`。
- `mod_export_handlers.go` 达2101行，混合上传、任务、ZIP验证、解析、OSS、数据库激活和通知；拆分前必须先补齐真实HTTP/Outbox/OSS/Worker测试（`TEST-008`），否则纯文件拆分难以证明行为未变。
- 关闭Outbox后的Core NATS和进程内goroutine不是必要适配器，而是与默认可靠路径语义不同的开发期旧分发实现，编号 `LEGACY-006`。
### 系统分类创建规则重复（REUSE-002）

手工资料初始化与导入同步分别维护一套 `blocks/items` 子分类、排序和本地化SQL。它们职责相同但参数获取方式不同，适合在解析root后复用同一个事务实现，避免分类规则和语言覆盖继续漂移。

### 项目文件版本排序绕过权威配置（REUSE-003）

`project_file_handlers.go` 另写点号数字比较器排序Minecraft版本，没有复用后台权威版本配置中的顺序。比较器把非数字段通过 `Atoi` 静默当0，未知、快照或预发布版本可能与版本选择器/合成表顺序不同；应复用同一版本注册表排序函数，而不是维护第二套近似规则。

### Mod与简单项目重复实现外部供应商读取（REUSE-004）

`mod_import_worker.go` 与 `simple_project_import_worker.go` 分别实现Modrinth项目/团队读取、CurseForge分类/search/description读取、作者转换、外链装配和吞错策略。两条活跃入口共享底层响应类型，却各自决定哪些请求可失败及如何映射字段；简单项目已经额外读取versions并把正文加入分类语料，行为开始漂移。应保留Mod和简单项目各自领域DTO，但把供应商项目快照获取、错误/partial语义和可验证的分类来源收口为一个权威客户端服务，避免用旧函数名转发到新实现。

### 后台用户列表绕过已有批量权限解析

`adminUsers`最多取100个用户后逐用户调用`resolveUserRootPermissions`；同包已经存在`resolveUsersRootPermissions`批量解析权威实现。权限目录也逐角色调用一个会吞错的`rolePermissionEntries`。这两处不是需要保留的领域边界，应改用批量解析和显式错误返回，见`PERF-062`、`ARCH-033`。

旧`PUT /admin/users/{id}/roles`没有前端调用，功能又被当前权限编辑端点包含；两者都采用全删全插，旧路由没有有效期/context DTO，属于仍注册的开发期重复入口（`LEGACY-020`）。`adminDashboard`也只是无逻辑转发到`loadAdminDashboard`，可在迁移路由后直接删除该Wrapper；`GET /admin/nav`返回一份固定中文旧导航，而当前前端维护自己的结构且没有调用该API（`DEAD-014`）。

仓库已用`.gitignore`排除`.idea/`，但仍跟踪三个GoLand项目文件。它们不参与构建、测试或运行，且会覆盖开发者本地IDE偏好，属于可直接移除的仓库残留（`DEAD-015`）。

### 自动更新调度和列表用空结果吞掉数据库故障（ARCH-013）

`scheduleDue` 忽略逐行Scan、任务insert和Commit错误，也不检查 `rows.Err()`；`tick` 在 `processOne` 出错时静默结束本轮。自动更新设置、运行历史和后台概览又复用 `querySimpleRows`，该辅助函数把Query失败直接变为空数组、逐行Values失败直接跳过且不检查最终游标错误。管理页面会把数据库故障显示成“没有来源/没有任务”，调度器也可能漏建任务而没有可观测证据。查询辅助函数应返回error，调度批次必须检查并记录所有游标、写入和提交错误。

同样的静默错误风格存在于填充爬虫：租约重置/调度Commit、当日导入数、项目存在性、翻译任务审计、草稿submitted状态和外部来源绑定多处忽略错误；run/candidate列表也不检查 `rows.Err()`。这些调用并非可选装饰数据，错误会改变预算、重复检测和最终项目状态，应纳入 `ARCH-013` 的统一修复范围。

### 爬虫任务租约无续租且外部抓取全失败仍记完成（OPS-005、OPS-006）

Seed Worker领取时固定5分钟租约，执行元数据导入和最多7个串行AI翻译时没有心跳。过期扫描会把仍执行的run改回pending，另一实例可重复处理同一run。另一方面，每个项目类型的Modrinth抓取错误只增加 `stats.failed` 并continue；即使所有类型都失败，executeRun仍返回nil error，run被标completed且清空last_error。应使用所有权token+续租和提交时租约校验；区分部分候选失败与基础provider不可用，后者保留可重试失败状态。

### 实时与消息中心状态职责过度集中

`site-shell.tsx` 同时承担全站Presence、SSE桥、后端状态、通知弹窗、导航、认证菜单和未读计数；`messages-center.tsx` 同时承担五类通知、AI翻译轮询、会话/消息、Presence和实时失效。后者用组件级布尔ref实现请求互斥，却没有把互斥键绑定到conversation ID，已经造成跨会话迟到响应覆盖（`BUG-047`）；多个组件再用两个全局DOM事件重复刷新同一未读事实（`PERF-031`）。这不是要求机械拆文件，而是应把SSE事件到统一查询缓存的映射、按会话带取消/请求身份的消息查询，以及通知翻译任务状态抽成单一职责Hook/Service。

蓝图任务已有 `enqueueBlueprintJob` 统一任务、Outbox和直发降级语义，但上传完成仍复制任务INSERT并直接发布Core NATS（`LEGACY-013`）。这是“仍有调用方但已被更完整实现替代”的典型残留：应扩展权威函数接受外部事务并迁移上传调用方，而不是再维护第二套入队规则。

### 站点Logo上传绕过既有OSS图片能力

项目已经有后端OSS权限、对象状态、病毒扫描、图片实际解码/像素限制和公开访问URL能力，站点Logo却另建Next本地文件写入、魔数识别和动态文件读取三件套。它既缺少统一安全边界，也造成部署语义不同（`SEC-019`、`OPS-008`）。应复用现有系统图片上传/OSS派生接口，只保留站点Logo业务类别和设置绑定，而不是维护第二套文件系统。

### 两套日志保留实现语义分裂（LEGACY-010）

用户活动日志已经有权威的动作白名单、预览确认、分批删除、advisory lock、多实例自动Worker和独立清理审计；`log_handlers.go` 又维护一套 `logs.retention` JSON配置和同步大DELETE。后者仍由后台日志页面调用，但所谓自动开关没有任何周期调度，保存设置本身就是执行动作，错误还会静默丢失。两类底层日志可以保留不同策略，但调度、分批、审计和错误处理应复用同一清理基础设施；现有第二套伪自动实现属于仍在使用的旧残留，调用方迁移后删除。

### 队列主题同时接受Envelope和旧原始负载（LEGACY-011）

权威Outbox会发布带事件ID、Schema版本、聚合身份和Payload的 `EventEnvelope`；仍在使用的Core NATS `PublishTask` 调用方发送裸JSON。`UnwrapEvent` 因此把任何不完整Envelope静默当作旧原始负载，测试还明确要求该兼容长期成立。两个内部协议共用同一任务体系，使消费者无法强制Schema版本、事件身份和端到端幂等。当前开发阶段没有外部消息生产者证据；应先迁移 `LEGACY-006/008/009` 等直发入口到Outbox Envelope，再删除裸负载fallback及锁定旧行为的测试。

### NATS持久设置保留开发期缺字段兼容（LEGACY-012）

`LoadNATSConfig` 先把JSON再解成字段存在性map；旧记录若缺 `outboxEnabled/realtime/jetStream` 就从环境回填。当前明确允许重置开发数据库，后台保存也总会序列化这些字段，因此这是只服务旧开发设置记录的双语义兼容。它同时掩盖了真正的PUT DTO缺字段问题：新保存记录包含false字段后反而不再回填。修复完整DTO后应删除字段存在性兼容，不要继续让“缺失”和“明确关闭”随记录年代改变含义。

## 搜索投影类型注册分散（MAP-008）

搜索集合Schema、`collectionKind`、全量 `loadDocuments` 和增量 `loadTypedDocuments` 分别维护集合/文档类型对应关系；未知类型在 `collectionKind` 还默认落到resources，而增量加载随后才返回错误。当前数据库check约束避免正常队列写入未知值，但新增项目类型时容易只更新其中部分位置。应以单一注册表描述集合Schema、允许文档类型和加载函数，并由测试验证数据库允许值与注册表完全对应；外部Typesense协议映射本身需要保留，不应删除边界。

`searchindex.Worker.drain` 的 `failed` 变量和末尾 `if failed { continue }` 没有任何行为：该条件之后已经是map循环体末尾，不论真假都会进入下一轮。这是确认死分支（`DEAD-009`），不会改善删除失败处理。

## 评分与收藏模块

- `MAP-010`：评分Upsert修改 `content_ratings` 时，数据库触发器已经调用 `enqueue_content_stats_refresh`；Handler在同一事务又显式执行一次。唯一队列行会合并，因此没有增加正确性，只增加函数、冲突更新和审查跳转。
- `LEGACY-017`：评分目标仍接受 `server`、`minecraft-server`、`resource-pack`、`shader`、`shader-pack` 等内部旧别名；当前前端只发送权威下划线类型，开发阶段未发现外部兼容责任。
- `LEGACY-018`：收藏成员关系把同一公开ID称为响应 `entityKey`、组件参数 `entityKey`、权威请求 `entityPublicId`；后端还双读旧查询/JSON `entityKey`。它没有转换或额外语义，属于开发期改名残留。
- `STYLE-006`：收藏API下载辅助在统一API/i18n边界内直接抛出固定中文错误；其他UI通过翻译Key展示错误，非中文用户会收到中文且无法由调用方稳定分类。

## 内容AI任务创建路径再次分裂

通用`createAITask`已经支持“任务记录与Outbox同事务，关闭Outbox时才直接Publish”的权威边界；内容本地化却另写一套`enqueueCatalogContentTranslation + publishContentTranslationTask`，始终在提交后直接NATS并自行把发布失败改成terminal failed（`LEGACY-019`）。这不是外部协议适配，而是遗漏现有可靠任务能力的第二实现。应让内容自动/手工翻译共用权威任务服务，在同一事务写quota、task和Outbox；不要增加旧函数到新函数的转发Wrapper。

AI模型配置中的`concurrencyLimit`从DTO、后台表单、默认值到测试完整存在，但执行端没有消费；真正并发来自NATS任务代码`ai`的单一`MaxConcurrent`。同名配置呈现两套控制语义（`BUG-094`），应选择一个权威层级，若需要按任务类型限流则在Worker中建立类型级信号量并测试动态配置生效。

## 表情包实现复用与错误语义

- 表情语法复用了统一`MarkdownRenderer`的remark AST管线，选择器和渲染器也共享`sticker-api.ts`目录类型；没有发现第二套HTML字符串替换器，这是正确的单一实现。
- `publicStickerCatalog`和`adminStickerCatalog`均不检查`rows.Err()`；目录版本查询、后台翻译JSON解析被直接忽略。创建包/表情又把所有INSERT/UPDATE错误统一映射为“code已存在”，会把连接中断、约束损坏等内部故障伪装成用户冲突（`ARCH-028`）。
- 目录版本从数据库一路映射到API和前端类型，但客户端不读取它参与缓存或刷新，是没有实际效果的版本映射（`DEAD-012`）。

## 站务API的类型与错误处理

- `site-affairs-api.ts`集中封装了关于页、更新日志和小黑屋公开读取，没有复制请求实现；但`BlackroomRecord.status`声明为`"temporary" | "permanent" | "released" | string`，在TypeScript中等价于`string`，前三个字面量不再提供任何约束（`STYLE-007`）。
- 站点更新公开列表和后台列表都没有检查`rows.Err()`，迭代中断会以200返回截断数据；后台列表把聚合JSON直接作为`RawMessage`返回，当前由数据库生成，边界尚安全，但没有显式解码验证（`ARCH-029`）。

## 举报治理的权威边界与残留

- 统一`submitUnifiedReport`确实收口了举报持久化和快照，证据上传也复用OSS作用域；旧`/comments/{id}/reports`仍通过另一DTO、原因回退和Handler包装它，延续既有`LEGACY-016`，当前前端没有调用该旧路由。
- 内容快照按项目类型重新手写一组公开字段SQL，而不是复用各详情服务的可见目标解析器。该重复不是纯DTO边界：它已经漂移到缺少审核/visibility条件并把`submitted_by`改名为作者（`SEC-042`、`BUG-109`）。应保留不可变治理快照DTO，但其输入必须来自统一的受权领域快照服务。
- `enqueueDirectNotificationTx`只是把固定标题、正文和`zh-CN`包进通用通知任务，不提供模板选择或接收者语言语义。封禁创建/解除、举报结果和目标处置新增四类活跃调用方，扩展`BUG-001`/`LEGACY-001`，不应把这个Wrapper当作新的权威通知服务。
- 后台与公共小黑屋共同调用`blackroomList`，但`internal`参数完全未读；`ban.view_internal`权限和`internal_note`列因此没有真实读取边界（`DEAD-013`）。
- 举报证据、日志分享、蓝图和多个项目编辑器使用同一个`FileDropZone`；该组件只负责浏览器选择/拖放和accept提示，没有复制上传协议或假装替代服务端校验，属于正确复用。
- `admin-governance-automation-panels.tsx`把举报、封禁、关于页、站点更新、爬虫和自动更新六个领域压进109行，大量状态、异步函数和完整表单写成单行（`STYLE-008`）。它不是文件数少的收益，已让请求竞态、语言状态和日期转换错误难以察觉。
- 禁用理由Hook把任何请求错误静默转换为空数组（`ARCH-031`）；管理者只看到空选择器，无法区分“没有配置理由”和权限/数据库/网络故障。统一API错误边界已有可复用的错误展示，不应在Hook内吞掉。

## 合成表渲染错误语义

`hydrateRecipeRenderLayouts`以两条集合SQL装配整页合成表，是应保留的批量边界，不是无意义DTO。但它复用的`decodeJSONObject`无条件吞掉`json.Unmarshal`错误；而导入只保证字段是合法JSON，没有保证`parameters/chance_texts`顶层是对象。这与仓库多处“数据损坏→空值”风格一致，但会隐藏Schema漂移，已登记为`ARCH-032`。

## 前端语言、审核状态与任务轮询复用

- `i18n-provider.tsx`与`content-language.ts`分别手写相同八种站点语言，类型上的`satisfies`不保证列表完整性（`MAP-012`）。应由一个权威注册表派生UI、内容编辑语言和类型；Minecraft语言别名属于外部协议，必须继续保持独立。
- 目录编辑、资源编辑、统一内容和合成编辑分别复制审核状态/本地化归一化，并共同出现未知状态默认为`approved`的漂移（`BUG-125`）。应复用一个失败可见的边界解析器，而不是把数据库实体直接暴露给组件。
- Mod导出和嵌入目录导入的等待函数只在URL路径不同，完整复制终态、延迟与取消循环（`REUSE-005`）。保留领域函数名无妨，但内部应共用一个任务轮询器。
- `catalog-resource-identifiers.ts`对短资源种类、`loot-table-model.ts`对外部包`definition`包装的兼容位于导入/持久Schema边界，当前承担真实外部格式读取责任；本轮不把它们计为无意义映射。删除前必须先版本化外部格式并确认已无历史包输入。
- 编辑员申请和个人作者认领证明附件都自行维护文件input、上传循环和结果列表，没有复用已经存在的`FileDropZone`拖拽边界（`REUSE-007`）。各业务source和数量上限应继续由对应申请模块负责，但文件采集、拖拽和通用上传状态不应再复制。
- 两份主语言字典的4,641个叶子键和参数占位符完全对齐；但`home.subtitle`与`home.activityHint`已经没有调用方且仍描述真实首页数据尚未接入（`DEAD-016`）。
- 系统通知后台在Outbox模式只完成PostgreSQL持久化时就显示“已进入NATS队列”，沿用了可靠Outbox引入前的阶段语义（`LEGACY-022`）。
- Mod资料管理登录提示仍把“项目owner”列为权限身份，与当前已认证作者/团队开发者、本站编辑员和管理员三类权威能力来源冲突（`STYLE-009`）。
