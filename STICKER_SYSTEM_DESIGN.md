# 表情包系统设计

## 模型与权限

`sticker_packs` 使用全站唯一、不可变的外部 code；`stickers` 在包内唯一。二者内部仍使用 bigint 主键，名称分别存入翻译表，并要求站内语言注册表中的全部八种语言。后台接口受 `sticker.manage` 保护，上传继续使用既有管理员 OSS 上传权限；公开目录只返回启用且扫描状态安全的图片。

Code 只允许 `[a-z0-9][a-z0-9_-]{0,47}`。已发布 code 不提供重命名接口；内容需要退出使用时先停用，历史 Markdown 会显示安全占位，不会出现破图或异常。

## 图片安全和 OSS

只接受 PNG/GIF。后端交叉检查扩展名、声明 MIME、魔数、实际解码、OSS 文件大小和 SHA-256。默认限制 4 MiB、边长 1024、总像素 4,194,304；GIF 额外限制 120 帧、总解码像素 64,000,000 和 30 秒。SVG、伪装格式、截断图片、超大动画均拒绝。OSS 仍是统一文件事实记录，公开只暴露受控内容路由，不暴露对象凭据。

上述阈值不是散落在 Handler 中的不可调整策略，分别由 `STICKER_MAX_BYTES`、`STICKER_MAX_EDGE`、`STICKER_MAX_PIXELS`、`STICKER_MAX_GIF_FRAMES`、`STICKER_MAX_GIF_DECODED_PIXELS`、`STICKER_MAX_GIF_DURATION_SECONDS` 读取；非法的零值配置安全回退到默认限制。

## Markdown AST 与选择器

语法为 `[sticker:pack:code]`。`remarkStickerTokens` 只访问 Markdown 文本节点，并完整跳过代码块、行内代码、链接和引用链接的后代节点；有效和无效 Token 都计入每次最多 50 个的限制。不存在或停用的表情输出 `[表情不可用]`。它不修改数据库原文，也不在最终 HTML 上做字符串替换。

CSS 使用 `--sticker-display-height: 2.2em`、自动宽度和 `max-width: 4.5em`，保持比例且不撑坏移动端。图片的 `alt/title` 使用当前语言名称。公开目录请求在浏览器进程中按语言合并，单个 Markdown 页面不会逐 Token 查询数据库。

通用 `StickerPicker` 已接入顶级评论、回复以及站内共用 `ToolsPlayground` Markdown 编辑器，按包分类、搜索并在光标位置插入 Markdown Token，不插入 HTML。后台可以创建/编辑包和表情的八语言名称、排序、图片、启用状态；替换图片在同一事务中把旧 OSS 对象送入删除 Outbox。

物理删除前会扫描当前所有 Markdown 事实表；仍被内容引用时返回冲突并要求停用。未被引用的表情或空表情包才允许删除，避免历史 Token 静默失效。
