# 评论区 Markdown 编辑器与附件

## 编辑能力

评论和回复共用前端 `CommentMarkdownEditor`，不再分别维护输入框。工具栏提供表情包、标题字号、粗体、斜体、删除线、行内代码、链接和附件选择。字号使用标准 Markdown 标题级别（`##`、`###`、`####`），不会放行任意 HTML 样式。

编辑器右上角在编辑与预览间切换；预览直接复用公开评论的 `MarkdownRenderer`，包括安全链接、楼层链接和表情包 AST 扩展。表情包浮层上方展示当前集合的表情，底部切换表情包集。

## 拖放上传

- 文件拖进编辑区后立即通过用户 OSS 直传链路上传，来源为 `comment`。
- 上传执行用户单文件、每日和总存储配额检查；未发布评论不会绕过配额，已上传文件仍可在用户文件管理中处理。
- 每条评论或回复最多绑定 5 个已完成上传的文件。
- 后端按当前用户、文件状态和安全扫描状态再次验证文件 ID，不能绑定其他用户的文件。
- `comment_attachments` 是评论与全部附件的权威关系，删除评论或物理删除文件时由外键清理关系。

## 普通附件下载

普通附件只有在 OSS 文件状态有效且安全扫描为 `clean` 或 `trusted_generated` 时才返回下载地址。下载入口为：

```text
GET /api/v1/comments/{commentId}/attachments/{fileId}/download
```

下载接口重新检查评论可见性、拉黑过滤、附件绑定、文件状态和扫描状态，并用 `Content-Disposition: attachment` 生成短期 OSS 访问地址。扫描中、已删除或隔离文件不会公开。

## 日志附件

以下名称由后端在 Unicode NFC 规范化后识别：

- `.log`；
- 扩展名为 `.zip` 且基础文件名包含 `错误报告`。

识别后的附件只进入现有日志脱敏流程。ZIP仍执行路径穿越、压缩炸弹、文件数量、加密和内容类型检查；脱敏失败时状态为 `failed`，绝不回退为原文件公开下载。处理成功后评论附件链接指向 `/log/s/{publicCode}`。

## 数据与查询

- `comment_attachments` 保存所有普通和日志附件关系及处理状态。
- `comment_log_bindings` 只保存日志附件与脱敏分享的子类型关系。
- 评论列表通过一次批量查询加载当前页全部附件，避免逐条评论或逐个文件查询。
- Schema generation 85 为当前开发期权威结构；不保留旧开发库双读或兼容表。

## 验证

2026-08-19 实际执行：

```text
$env:MCMODS_RUN_DB_INTEGRATION='1'; go test ./internal/database ./internal/httpapi -count=1  # 数据库集成；通过
go test ./...                                                 # 通过
go build ./...                                                # 通过
pnpm typecheck                                                # 通过
pnpm lint                                                     # 通过
pnpm build                                                    # 通过，59 个静态页面
```

数据库集成覆盖回复附件绑定和跨用户附件绑定拒绝。单元测试覆盖 `.log`、大小写扩展名、`错误报告` ZIP 与非日志附件的分类边界。真实 OSS 上传、病毒扫描引擎和浏览器拖放仍需在配置了 OSS 的开发部署中做交互验证。
