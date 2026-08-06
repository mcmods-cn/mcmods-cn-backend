# 数据库命名约定

数据库表名描述业务事实，而不是数据最初来自人工编辑还是导入器。人工编辑和导入最终写入相同的规范化对象表。

## 规范化业务表

- `catalog_entities`：全局内容身份；
- `game_resources`：物品、方块、流体等 Minecraft 资源身份；
- `catalog_tags`、`recipe_types`、`recipes`：Tag 与配方规范对象；
- `mod_content_*`、`mod_resource_version_*`：某一模组资料版本独立维护的分类、正文和属性；
- `content_creator_bindings`：作者/团队与任意内容类型的通用绑定；
- `project_files`：站内项目发行文件；
- `content_download_*`：通用内容下载计数。

这些表不使用 `manual`、`exporter`、`owned` 等来源或旧模块名称。

## 导入与溯源表

只有确实表示导入过程、源快照或技术观察的数据使用 `import`：

- `catalog_import_jobs`、`catalog_import_revisions`；
- `resource_import_snapshots`、`recipe_import_snapshots`；
- `mod_metadata_import_jobs`。

导入快照用于审计、覆盖策略和技术数据追踪，不是人工资料的平行副本。页面读取规范化业务表，人工修改也不会因为后续导入而被清空。

## ID 字段

- `id`：`bigint` 内部主键；
- `*_id`：默认是内部数字外键；
- `public_id`、`*_public_id`：9 位站内公开 ID；
- `canonical_id`：Minecraft 技术 ID；
- `provider_*_id`：外部平台 ID；
- `source_*_id`：导入包中的源标识。

跨业务类型需要一个全站数字身份时使用 `public_routes.id`；不要把 `public_id` 当作内部主键或业务外键。

HTTP API 不得输出内部数字 ID。若旧接口仍使用 `entityId` 这类历史字段名，其值也必须是公开 ID，并应在新接口中逐步收敛到 `publicId`。

导入快照的确定性字符串键只允许存在于 `*_import_*` 溯源表中。它们应在代码
和 API 中称为 `snapshotKey`、`sourceKey` 或 `importKey`，不得伪装成业务对象的
数字 `id`，也不得出现在面向用户的 URL 中。
