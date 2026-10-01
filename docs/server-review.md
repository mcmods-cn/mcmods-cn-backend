# Minecraft 服务器审核列表

`GET /api/v1/admin/server-reviews` 仍要求 `server.review`，支持 pending/approved/rejected 状态、最多200条、按 created_at/id 稳定排序。证明附件、链接及检测模组信息沿用既有字段和安全附件过滤，不改变审核权限。

列表先完整读取并检查迭代错误，再释放查询连接、加载关联资料。即使 PostgreSQL 连接池只有一个连接，也不会等待自己持有的列表连接；关联失败仍返回500，不伪报部分成功。当前每项关联查询仍是有界 N+1，不能据两条样本宣称生产查询性能提高多少。

`TestServerReviewAssociationsWithSingleConnectionCoreIntegration` 在 ownership marker 核验的随机 PostgreSQL 数据库、完整 schema 和 MaxConns=1 下调用真实 handler，两个待审服务器各含安全证明附件、链接、模组，验证全部字段返回。该测试没有替代路由权限验收或真实外部对象存储下载。修复无 API/schema/migration 变更，可独立部署。
