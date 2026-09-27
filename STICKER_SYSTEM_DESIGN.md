# 表情包系统设计

## 模型与权限

`sticker_packs` 使用全站唯一、不可变的外部 code；`stickers` 在包内唯一，每个 `image_file_id` 也只能属于一个表情。二者内部仍使用 bigint 主键，名称分别存入翻译表，并要求站内语言注册表中的全部八种语言。后台接口受 `sticker.manage` 保护，上传继续使用既有管理员 OSS 上传权限；公开目录只返回启用且扫描状态安全的图片。

Code 只允许 `[a-z0-9][a-z0-9_-]{0,47}`。已发布 code 不提供重命名接口；内容需要退出使用时先停用，历史 Markdown 会显示安全占位，不会出现破图或异常。

## 图片安全和 OSS

只接受 PNG/GIF。`sticker-upload` 原对象始终是私有的一次性输入；后端交叉检查扩展名、声明 MIME、魔数、实际解码、OSS 文件大小和 SHA-256，再用 PNG/GIF 编码器生成随机路径的 `sticker_derived` 派生对象。重编码不复制 PNG ancillary 文本块或 GIF comment 等上传元数据；公开目录和 inline 内容边界只接受 `sticker_derived + trusted_generated`。成功绑定后原上传归档，登记或贴纸事务失败时按“先复核无所有者、再归档/入 Outbox”补偿，不会删除已成功绑定的模糊提交结果。

默认限制 4 MiB、边长 1024、总像素 4,194,304；GIF 额外限制 120 帧、总解码像素 64,000,000 和 30 秒。SVG、伪装格式、截断图片、超大动画及重编码后仍超限的文件均拒绝。OSS 仍是统一文件事实记录，公开只暴露受控内容路由，不暴露对象凭据。

上述阈值不是散落在 Handler 中的不可调整策略，分别由 `STICKER_MAX_BYTES`、`STICKER_MAX_EDGE`、`STICKER_MAX_PIXELS`、`STICKER_MAX_GIF_FRAMES`、`STICKER_MAX_GIF_DECODED_PIXELS`、`STICKER_MAX_GIF_DURATION_SECONDS` 读取；非法的零值配置安全回退到默认限制。

目录默认最多 64 个包、每包 128 个表情、全站 1024 个表情，由 `STICKER_MAX_PACKS`、`STICKER_MAX_PER_PACK`、`STICKER_MAX_CATALOG_ITEMS` 配置，并分别受 256、512、4096 的硬上限约束。创建入口以全站 advisory transaction lock 原子检查预算；公开和后台目录再以 `LIMIT + 1` 与迭代计数防御异常数据，因而不存在无界响应或无界后台 DOM 输入。

## Markdown AST 与选择器

语法为 `[sticker:pack:code]`。`remarkStickerTokens` 只访问 Markdown 文本节点，并完整跳过代码块、行内代码、链接和引用链接的后代节点；有效和无效 Token 都计入每次最多 50 个的限制。不存在或停用的表情输出 `[表情不可用]`。它不修改数据库原文，也不在最终 HTML 上做字符串替换。

CSS 使用 `--sticker-display-height: 2.2em`、自动宽度和 `max-width: 4.5em`，保持比例且不撑坏移动端。图片的 `alt/title` 使用当前语言名称。公开目录请求在浏览器进程中按语言合并，单个 Markdown 页面不会逐 Token 查询数据库。

通用 `StickerPicker` 已接入顶级评论、回复以及站内共用 `ToolsPlayground` Markdown 编辑器，按包分类、搜索并在光标位置插入 Markdown Token，不插入 HTML。目录 Promise 只在每 locale 的 30 秒窗口内合并，失败立即可重试；后台成功新增、编辑、启停、换图或删除后主动失效当前进程缓存，其他打开页面最迟在 TTL 后观察新目录。后台可以创建/编辑包和表情的八语言名称、排序、图片、启用状态；换图和删除在事务内按确定顺序锁 OSS 行，复核新文件仍为 active，并且只在没有表情绑定时把旧对象送入删除 Outbox。

所有名称以 `_markdown` 结尾的当前正文列、`comments.body` 和不可变 `content_revisions.snapshot` 都由 generation 90 数据库触发器解析到 `sticker_content_references`。引用写入和删除使用同一个按 Token 命名的 advisory transaction lock；后台删除在锁内走 `(pack_code,sticker_code)` 索引查询，仍被当前或历史内容引用时返回冲突并要求停用，不再对子串扫描事实表。

创建表情与删除空包都会先锁同一个 `sticker_packs` 父行，再在事务内确认子项状态。并发创建要么先提交并使删包返回非空冲突，要么在删包提交后观察到父项不存在；不会再出现创建成功后被级联删除及遗留 active OSS 对象的窗口。
