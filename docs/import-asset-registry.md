# 导入资产用途注册与隐私边界（importer 1.6.0）

上传的源 ZIP 是创建者提供的私有来源证据，SHA-256 始终针对原字节。处理期间用私有源文件校验字节，并在符合恢复条件时重试。成功结束导入（含待审或部分完成）会在同一事务将源文件标记为已删除，并登记对象清理意图。来源记录和哈希保留。原上传对象还须等到签名过期、无活动引用且满足删除条件后才能物理回收。需要重新上传时请使用本地留存的 ZIP。原包不属于公开内联 OSS 来源；匿名 `/oss/files/{publicId}/content` 返回 404。上传凭据只能写入其限定对象，管理读取另需 `oss.read`；业务 Worker 使用服务存储凭据。存储桶仍须私有，短期预签名链接的实际有效期遵循 OSS 配置。原上传对象的期限与恢复条件见[原始上传回收](audit/gr001-terminal-original-upload-retirement.md)。

源包、规范化数据库投影、衍生渲染资产具有不同用途。审批只能公开经过相应用途规则投影的后两者。错误、机器路径、凭据、玩家身份或调试附件不能因为与合法资料同包而获得公开权限。

## 当前注册用途

| 路径/内容 | 衍生策略 |
| --- | --- |
| manifest / capabilities | 保留结构化版本/模块状态与有界诊断，丢弃异常正文和未注册 extras；不复制原文 |
| registries、规范化目录、tags、JEI | 按已有业务投影；import-only JSON 字段另执行嵌套用途注册，数量、概率、业务 SNBT、引用保留 |
| `assets/<namespace>/models/*.json` | Minecraft 模型元素、面、变换、材质、覆盖和已注册的 Forge composite/OBJ 字段。纹理和模型引用限本地资源，元素编辑器名称、注释及已识别的模型分组不公开。 |
| blockstates / texture `.png.mcmeta` | 变体、multipart、动画帧、纹理过滤字段 |
| Wavefront OBJ/MTL | 保留数值几何、面索引、对象/材质名和同 namespace 内材质/纹理引用；删除注释。未注册指令及带额外选项的纹理 codec 明确报错 |
| entity 模型 metadata / block-entity index、variant、UV mesh | 按用途保存纹理引用和网格。v1 四边形保留面序号、方向、顶点、UV、法线及 UV 边界。每个网格上限 16 MiB、65,536 个面，末尾不足四个的顶点只计入声明数量。 |
| PNG | 保留像素/alpha/palette 和必要颜色解释 chunk；删除 tEXt/iTXt/zTXt/EXIF/时间等不相关 ancillary 元数据；ICC 按颜色用途重建 profile，剥离名称/描述/设备/创建者/private tags，已注册矩阵/TRC/LUT 等颜色变换负载保留，未知 codec 明确注册错误；衍生哈希针对实际保存字节 |
| partial advancement / vanilla worldgen definition | 只有明确 `raw_definition_path`、`definition_path`、`placed_feature_definition_path`、`configured_feature_definition_path(s)` 引用取得用途；再次执行已注册嵌套投影 |
| `.log` | 仅为源包诊断，不复制入公开资产表 |
| 未注册附件/业务扩展 | 在创建导入修订和衍生资产前，或相应规范化事务内，明确失败；不默认公开、不悄悄删除该业务扩展 |

`data/...json` 完整规范化源定义只存在于私有源包中；只有明确 partial 引用才生成公开回退投影。任意字符串碰巧等于 ZIP 路径不能授权保留该附件。独立 NBT/SNBT/schematic 附件没有现行 exporter 的公开资产用途：当前结构 v2 明确不导出内部 NBT 零件。物品字段 `nbt_snbt` 等仍是受支持业务字段。

进度展示的物品图标按物品 ID 和数量匹配目录。`display.icon.nbt_snbt` 是游戏导出的完整栈标记，可能含方块实体数据。导入器校验字符串及大小，公开投影只保存 `has_stack_tag`。已有 `nbt` 兼容字段沿用原规则。超平坦维度的 `generatorSettings.structure_overrides` 接受一个带 `#` 的结构集合标签或一串结构集合 ID，空列表有独立含义。掉落池中的附魔谓词与附魔 ID 列表适用不同规则，见 `mod_export_loot_projection.go`。

## 自然生成 v2

`WorldgenDataStage.normalizePlacement` 以 Mojang 的放置修饰器生成摘要。导入时，`mod_export_worldgen_placement.go` 同时约束资源快照的 `placement`、部分目录和其已声明的放置地物回退文件。带权列表保留原顺序、权重和零值，坐标偏移保留三个整数，相对高度保留原锚点。部分目录中的雕刻器可带生成阶段、概率和游戏用于可视化的方块状态。方块状态属性只接受属性名和枚举值。缺失的摘要字段保持缺失。

如需支持新的 Mod 放置规则，在对应的修饰器、数量提供器、高度提供器或方块谓词位置添加已核对的业务字段，用生产端的最小样例验证快照和部分目录。每个快照 JSON 字段的上限为 256 KiB 和 16 层，源 JSON 文件以 128 MiB 为上限，派生投影本身最多深入 24 层。未登记字段仍会返回 `unregistered_export_asset`。

## 未注册扩展的可审查流程

错误码文本 `unregistered_export_asset` 包含用途和 `path_sha256`，不记录未知字段值、异常正文或可能含身份信息的路径。普通资产的摘要输入是 ZIP 相对路径；`catalog_json_field` 的输入是 `<kindCode>/<fieldCode>`。创建者可以在自己的源包和字段清单中计算相同 SHA-256 来定位，不需要把私密正文复制到日志。

新增业务扩展需要提供：生产者/版本、路径或字段位置、业务用途、明确类型及嵌套键、引用和权限范围、最小正反金样、保留/公开策略。实现者在 `mod_export_asset_registry.go`、`mod_export_asset_projection.go` 或 `mod_export_catalog_json_projection.go` 注册对应规则，再跑真实 HTTP+数据库回归。复杂 mod worldgen codecs、触发器条件、模型 loader 扩展及 MTL 选项需要按这个流程增加适配；当前拒绝会显示明确注册错误，不将缺失功能记作通过。

已知的无关隐私元数据键会被投影移除；未知业务键会报错。动态映射（材质名、方块状态、locale、计分板等）只能承载用途明确的类型，不能借机嵌入任意对象。人工编辑器的契约独立，未通过本次修复扩大或缩小其权限/业务政策。GR-Q05 variants 与 CONT-Q01 通用扩展政策的待决结果不能自动扩大本注册表。

## 存量和兼容

1. 先执行只读 `db-import-privacy-audit`，按修订/实体 ID、用途和摘要定位候选。报告只记录分类/计数/摘要，不输出正文。超过扫描上限会明确标记 truncated。
2. 新 importer 为 `1.6.0`，允许同一不可变原 ZIP 在新的 importer 版本下通过正常创建者授权、目标版本校验、实际 Worker 和独立审核重新导入。它不会把旧修订自动记成已修复。
3. 存量受影响修订应由有权操作员核对并通过既定撤回/访问处置及重新导入流程处理。不要直接覆盖已有账本/审计、批量删除共享 blob 或在原不可变媒体 URL 上替换字节。保留原失败证据和新修订映射。
4. 本修复未自动执行生产数据修改；历史公开资产、缓存和第三方已下载副本不能声称已撤回。旧私密 JSON 仍在存量表中的事实，必须与“未来导入投影已修复”分别报告。

无新增数据库 DDL。部署后新旧源包上传协议保持一致，JEI v6/v7 的既有兼容门保持独立；新增拒绝仅针对未注册公开资产/嵌套扩展。先部署 backend 规则，再允许相应扩展生产者。回滚到旧 importer 会恢复泄露风险，不能把降低 importer 版本视为数据回滚。

只读诊断命令（使用已授权的数据库连接，显式核对目标）：

```sh
go run ./cmd/db-import-privacy-audit --database <exact_database_name> --revision <revision_id> --limit 1000 > import-privacy-findings.json
```

CLI 固定 `public,pg_catalog` 并启用数据库 `default_transaction_read_only`，无 apply/删除模式。合成旧数据副本的测试验证候选定位、作用域隔离、报告脱敏、上限和原行不变。它检测已知无关隐私键与资产注册问题；不能把 findings 为空当作所有未知秘密均不存在。单条超过 1MiB 的诊断负载明确记待检查，不假装扫描完整。外部 PNG/存储桶和缓存须另以授权资产字节核验，诊断不联网下载原包或读取生产对象。

ICC 回归金样 `testdata/audit-20261024/icc-srgb-private-label.icc` 使用 LittleCMS 合法 sRGB profile，仅加入合成描述标记。验证真实 HTTP/数据库/实际 Worker/独审后匿名 PNG 字节、衍生哈希、像素/alpha、九个必要颜色 tag 原字节和 LittleCMS 289 个 RGB 样本转换结果。源 ICC 和派生 ICC 都能加载，转换逐字节一致。ICC 输入解压上限 4MiB，支持 RGB/GRAY 的 v2/v4 和已注册变换结构；未知 vendor tag、named-color/其它 color-space/多处理元素 codec 需提供明确颜色用途和正反金样后注册，不能以丢弃 ICC 通过验证。

颜色复核命令：在执行 `TestAuditGR001ICCMetadataRemovedWhileColorTransformsRemain` 时设置 `MCMODS_AUDIT_ICC_EVIDENCE_DIR` 为新的本任务 artifacts 子目录（测试以 O_EXCL 创建三份样本，拒绝覆盖已有证据），然后运行：

```sh
python3 tools/audit/verify_icc_color_equivalence.py --source <evidence_dir>/source.icc --sanitized <evidence_dir>/sanitized.icc --output <evidence_dir>/color-equivalence.json
```

JEI ingredient icons emitted by the supported producer live under
`ingredients/<type-slug>/<size>/<namespace>/<path>--<hash>.png` (see
`JeiIngredientStage.exportEntry` and `ingredientIconPath`). They are registered
render images, linked by the existing `ingredients/ingredients.json` entries'
`icons` map. The exact same PNG projection removes unrelated text and preserves
pixels, alpha and necessary color interpretation before storage and serving.
Registering this image family does not register unknown ingredient JSON or any
opaque extension object. Recipe-bound source namespaces still use the existing
project provenance rules.
