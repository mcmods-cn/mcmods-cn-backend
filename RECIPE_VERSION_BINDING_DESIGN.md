# 合成表多版本绑定设计

## 身份与去重

合成表继续使用既有导入器生成的 `semantic_fingerprint` 和 `recipe_identity`。指纹的输入来自规范化合成类型、模板/布局、有序性、尺寸、槽位、候选资源、Tag、数量、NBT、概率及导出器保存的语义参数；Minecraft 版本不进入指纹。

这保证：

- 语义完全相同的 1.20.1 与 1.21.1 合成表可复用同一 `recipes.entity_id`；
- NBT、Data Component、条件、平台限制或输入输出语义不同的记录仍产生不同身份；
- 本次迁移不重新计算或替换既有合成表 ID。

## 关系模型

```text
recipes(entity_id)
  1 ── N recipe_version_bindings(recipe_id, version_code)
```

`version_code` 是站内 Minecraft 版本配置中的权威 code。唯一主键 `(recipe_id, version_code)` 同时承担并发去重和幂等保证；`source` 记录 `import/editor/backfill/split`，`created_by` 记录可用的操作者。

当前 Minecraft 版本目录尚不是独立关系表，因此绑定使用权威 code 而不是另建一份版本列表。写 API 每次都用当前目录验证 code。

## 导入事务

1. 导入器规范化并暂存合成表。
2. 按既有 identity/fingerprint upsert `catalog_entities` 与 `recipes`。
3. upsert `recipe_import_snapshots`。
4. 从该快照对应的 `mod_content_versions.minecraft_versions`（缺失时使用修订版本）展开明确 code。
5. `INSERT ... ON CONFLICT DO NOTHING` 写版本关系。
6. 槽位与候选随后写入；没有槽位的合成表也已在步骤 4 获得绑定。

全部步骤位于同一 PostgreSQL 事务。唯一约束是并发导入的最终裁决者。

## 查询与显示

列表和详情通过 lateral 聚合一次加载全部版本，不对每张卡执行额外查询。服务端按 `minecraft.versions` 配置顺序排列，并返回：

```json
{"applicableVersions":[{"id":"1.21.1","name":"1.21.1","group":"1.21.X"}]}
```

前端负责压缩完整分类和移动端展示，不依赖后端拼接字符串。

## 编辑

`applicableVersionIds` 是可选的兼容字段：

- 省略：保留既有关系；
- 提交：在发布事务中同步删除未选关系并 upsert 新关系；
- 空值、非法 code 或最终零绑定：拒绝；
- 内容修订仍受既有 base revision 并发校验保护。

共享内容修改会明确提示影响所有绑定。当前未提供部分版本拆分 UI，避免伪装成仅修改当前页面。

## 回填与校验

回填优先使用导入修订目标资料版本，其次使用手工 `recipe_definitions.source_mod_content_version_id`。语句使用 `DISTINCT + ON CONFLICT DO NOTHING`，可以重复执行。

校验项：无空 code、无重复主键、无零绑定合成表、不同入口的同一 recipe ID 返回一致版本数组。开发环境真实迁移已执行；生产大数据量需先将回填拆为批处理。

