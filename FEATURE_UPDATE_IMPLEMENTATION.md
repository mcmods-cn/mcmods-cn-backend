# MCMods 组合功能实现记录

更新日期：2026-08-17  
数据库代次：79

## 实现范围

### 合成表适用版本

- 新增 `recipe_version_bindings`，以 `(recipe_id, version_code)` 为主键保存明确版本集合。
- 导入快照写入后，在同一事务中按目标资料版本执行幂等绑定；版本不参与合成表语义身份，因此内容相同的导入继续复用原合成表 ID。
- 列表、详情、卡片和编辑器统一读取 `applicableVersions`，并按站内 Minecraft 版本配置顺序返回。
- 编辑请求使用 `applicableVersionIds` 明确替换关系，后端校验权威版本白名单并禁止保存空集合。省略该字段的旧客户端保持原绑定不变。
- 共享合成表编辑器会提示内容修改影响全部适用版本。当前系统原先没有“仅修改部分版本”的内容编辑语义，因此本次没有增加隐式拆分路径。

### Minecraft 版本选择器

- 继续复用 `MinecraftVersionPicker`，父分类使用原生三态 checkbox。
- 父分类操作完整分类而非搜索结果子集；禁用子版本不可由父分类绕过。
- 完整分类显示为 `1.21.X`，部分分类显示明确子版本；提交值始终是具体版本 code 数组，因此未来新增子版本不会自动扩大旧数据范围。
- 常用版本、快照和愚人节版本原有入口保留。

### 全局资源后台化

- `/api/v1/catalog/resources` 旧目录 API 以及详情 API 分别要求 `global_resource.list`、`global_resource.view`。
- 新后台入口：`/admin/global-resources`，API 为 `/api/v1/admin/global-resources`。
- 后台支持名称/技术 ID、类型、命名空间、状态、是否存在绑定、分页筛选。
- 绑定详情 API 为 `/api/v1/admin/global-resources/{publicId}/bindings`，要求 `global_resource.binding.view`。
- 公共端仅保留单个已知引用的最小展示解析 `/api/v1/catalog/resource-presentation?ref=...`，不支持模糊搜索或分页，也不返回数据库 ID、绑定关系、计数和状态。
- 普通资料详情仅在前端权限快照具有 `global_resource.view` 时渲染后台入口；后台接口仍执行独立后端校验。

### 评论楼层

- 顶级评论获得持久化 `floor_number`；回复为 `NULL`。
- `comment_floor_counters` 按 `target_type + target_id + target_version_key` 原子 `UPSERT ... RETURNING` 分配楼层，未使用 `MAX+1`。
- 删除、隐藏、驳回不回收楼层。
- 新增楼层定位 API、`#floor-N` 锚点、跨分页定位、滚动高亮和正整数校验。
- Markdown AST 仅改写普通文本节点中的 `数字+楼`；链接、URL、行内代码和代码块不参与转换。

### 日志查看与分享

- 新增 `/tools/logs` 与 `/log/s/{code}`。
- 登录用户可经现有 OSS 文件与配额系统批量选择 `.log/.txt/.zip`；游客和登录用户均可粘贴一条文本。
- 公开短码使用 128 位随机值的 Base64URL 表示，数据库唯一且删除/过期记录保留。
- 粘贴内容先脱敏再持久化；公开查看和下载只读取 `log_share_entries` 的脱敏内容。
- 文件分享可下载重建后的脱敏原格式；粘贴分享在后端拒绝下载。
- 日志历史、删除分享记录、源文件删除联动失效、到期批量清理和评论附件关联已接入。
- 评论 `.log` 和名称含“错误报告”的合法 `.zip` 使用已有文件上传与日志分享实现；普通 ZIP 不自动关联。

## 数据模型与权限

新增关系：

- `recipe_version_bindings`
- `comment_floor_counters`
- `comments.floor_number`
- `log_shares`
- `log_share_entries`
- `comment_log_bindings`

新增权限：

- `global_resource.list`
- `global_resource.view`
- `global_resource.binding.view`
- `log_share.list/view/moderate/delete/redaction_reprocess`

用户自有日志接口以所有者 ID 做后端约束；管理员日志权限节点已进入 RBAC 权威目录，但本次没有新增一个可查看未脱敏内容的权限。

## 迁移与兼容

- 代次 78 建表和执行幂等回填；代次 79 补齐外键前导索引。
- 合成表和评论既有公共 ID 均不变。
- 旧合成表编辑请求省略新字段时不清空绑定。
- 旧全局资源页面 noindex 并跳转后台；数据 API 已收紧权限。
- 开发数据库已通过 78→79 真实升级。生产上线前仍应在备份副本测量评论和合成表回填时间。

## 已知限制

- 日志脱敏当前在请求内完成并设置 20 MiB 原文件、100 MiB ZIP 解压上限；尚未迁移到持久后台任务，因此不应把这些上限直接提高到数百 MiB。
- 日志查看 API 当前整条返回脱敏文本，前端限制渲染量但尚未实现 Range/分块搜索。
- 日志文件创建后直接进入 ready，没有独立“先预览、后确认发布”的两阶段记录；页面会立即给出脱敏预览链接。
- 评论附件自动关联在评论提交后同步建立；同一文件并发请求受唯一索引保护，但进程恰好在评论提交后、绑定前崩溃时仍需要客户端幂等重试恢复。
- 历史回填语句是幂等的，但代次 78 的首次安装仍在一个迁移事务内执行；超大生产表需要先拆成专用批处理命令再上线。

