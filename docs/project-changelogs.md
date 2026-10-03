# 项目更新日志与分类分页

`GET /api/v1/changelogs?targetType=mod&targetId=<公开ID>&locale=en-US` 返回项目更新日志
的现有 cursor 分页。首个条目页仍包含 `categories`，每页最多 100 个分类，另返回
`categoriesHasMore` 和 `categoriesNextCursor`。后续条目页不重复加载分类。
100 是单页读取预算，不是项目允许创建分类的总量；已有第 101 个及以后分类仍可使用。

更多分类由 `GET /api/v1/changelogs/categories` 按需读取，查询参数为 `targetType`、
`targetId`、`locale` 和可选 `cursor`。响应 `data` 包含 `target`、`categories`、
`limit: 100`、`hasMore` 和 `nextCursor`。分类字段与原数组相同，按创建时间与内部 ID
稳定升序排列；不返回内部 ID。游标绑定目标项目、界面语言与分页类型，跨目标/语言及
无效游标返回 400。删除上一页的末项不使游标失效。

两个读取入口沿用现有目标解析权限：仅已批准的项目提供分类与更新日志集合；登录或编辑
权限不会让未批准目标变成公开可读。编辑界面按需加载更多分类，当前条目已经选中的分类
即使不在首个分类页，也保留名称和值；加载失败保留草稿，并允许重试。

不需要 schema 迁移、历史数据清理或新业务上限。分类查询先取最多 101 个分类，再仅加载
每项最多 9 条语言记录检查现有 8 语言不变量，避免整体列表和译文聚合无界增长。
更新失败的错误分类见 [查找错误说明](changelog-update-errors.md)。
