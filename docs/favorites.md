# 收藏与收藏夹 API

收藏目标当前支持 Mod、整合包和蓝图，收藏与项目关注是独立关系。

- 收藏夹与条目列表使用有界游标分页，`limit` 为 1–100。游标绑定用户、公开/私有访问范围、页面大小与目标收藏夹，不能跨范围复用。
- `POST /api/v1/users/me/favorites/summary` 只返回当前用户可见目标的收藏关系；它不能用于读取其他用户的私有收藏。
- `PATCH /api/v1/users/me/favorites` 接受 `entityType`、`entityPublicId`、`addCollectionIds` 和 `removeCollectionIds`，增删列表不能重叠。
- `PUT /api/v1/users/me/favorites` 接受 `entityType`、`entityPublicId` 与 `collectionIds`，替换当前用户该目标的全部收藏关系。
- 新增或移动到另一个收藏夹要求目标当前可见。对纯删除（PATCH 无新增收藏夹，或 PUT 的 `collectionIds` 为空），目标后来变为不可见仍可清理当前用户已有的关系；该路径不返回隐藏目标的名称、URL 或其他资料，不允许操作其他用户的关系。
- 私有收藏夹只由所有者管理。公开收藏夹不会越过目标本身的可见性限制。默认收藏夹不能删除或改名，可以修改公开状态。

收藏夹和条目数量受配置中的用户额度限制，增长检查与写入在同一用户数据库锁下进行。降低额度后，幂等写入与删除仍可执行。失败不会部分保存增删关系。

同一用户的收藏关系变更及收藏夹删除共用锁。多个收藏夹上的同一目标只计一次收藏人数和
首次/末次热度事件；每条关系按顺序写入，使统计触发器观察真实转换。删除收藏夹先在
事务内删除条目，再删除仍可识别归属的父记录。PUT 只应用变化的关系，保留未变化条目的
ID 和收藏时间；重复保存不会重置列表顺序或制造重复热度事件。已有统计偏差未在生产库
清洗，需要通过项目既有事实重建流程另行授权核对；直接数据库级级联删除未走此 API 的
行为仍需独立审查。

收藏夹导出 `.mrpack` 使用有期限、内容哈希校验的预检快照。创建任务后保存项目和兼容文件的选定结果，后台任务具有租约、重试上限及取消处理；生成文件过期后仍保留任务报告。导出依赖项目实际配置的同步 Minecraft/加载器目录与 Modrinth 数据，不依赖 AI 翻译。

依赖预检在单条 SQL、同一读取快照中遍历已批准且版本/加载器兼容的关系。
每个来源只扩展一次；累计超过 1,000 个节点或 4,000 条关系立即停止并返回
`422 MODPACK_EXPORT_DEPENDENCY_LIMIT`，不会继续解析外部依赖文件。
保留限内图的稳定输出顺序和最多八个文件解析请求并发。查询另有最多五秒的
取消上限，较短的调用者截止时间仍优先；超时作为实际错误返回。
上限控制符合条件的图遍历，不能保证每种数据分布下过滤和索引扫描的成本。
专用 PostgreSQL 验证了长链、稠密环与百万关系索引计划，生产容量仍未验证。

导出完成、失败、租约耗尽和过期状态与通知 Outbox 意图在同一数据库事务提交。
通知意图写入失败时保留原状态，过期路径也保留文件活动状态，后台扫描或租约恢复
可再次处理；已经提交的终态不会因消息重复投递再产生通知。通知投递由现有 Outbox
消费者恢复，不要求站内通知与邮件已在该事务中送达。文件上传仍在数据库事务外，
提交失败由原有文件补偿与孤儿清理处理，不能保证外部上传只请求或计费一次。

## 验证

在已初始化的专用测试库上设置 `APP_ENV=test`、`DATABASE_URL`、同值 `MCMODS_TEST_DATABASE_URL` 和 `MCMODS_RUN_DB_INTEGRATION=1`，运行：

```sh
go test ./internal/httpapi -run 'TestFavorite(RemovalSurvivesTargetVisibilityChangeIntegration|TargetsRevalidateVisibilityForEveryViewer|ReadsAndWritesKeepTargetVisibilityBoundary)' -count=1 -p=1 -parallel=1
```

测试使用临时独立 schema 或事务内临时表，只包含合成数据。普通读请求与收藏变更不调用真实 AI 供应商。
