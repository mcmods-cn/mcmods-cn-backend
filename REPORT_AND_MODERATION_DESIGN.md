# 举报与处置设计

## 统一模型

举报接口使用白名单目标类型与公开 ID，覆盖 Mod、插件、地图、光影、材质包、数据包、附属资源、讨论、BUG、新闻、教程、皮肤、蓝图、服务器、评论和用户主页。目标适配器解析公开 ID、校验目标类型并生成快照；`reports` 实际保存数值 `target_id` 和 `target_type`，通过复合外键连接 `public_routes(entity_type,internal_id)`。客户端不能传表名或内部数据库 ID。

理由由 `GET /api/v1/reports/reasons` 返回稳定代码和理由版本。每类目标拥有自己的理由集合，统一附加 `other`；数据库保存代码、版本、显示资源键快照、自定义原因和补充说明。理由代码的显示文本由前端语言资源提供。

## 不可变快照

举报事务同时写入 `report_snapshots`。JSON 快照带 Schema 版本、SHA-256 和创建时间，举报后没有更新入口。项目快照包含公开标题、正文、作者、版本、分类、许可证、链接和状态；服务器另含性质、地址和在线信息；用户仅包含当时公开资料；评论包含正文、作者、父评论、楼层、所属评论区、相邻上下文和附件元数据。凭据、内部备注、IP 和风控数据不进入快照。

## 私有证据附件

证据复用 OSS 直传流程，但使用独立的 `report_evidence` scope 和 `moderation/report-evidence/` 对象路径：

- 每条举报最多 5 个，单文件最多 25 MiB；
- 扩展名、MIME、魔数和 ZIP 安全结构均由后端校验；
- 不创建用户文件记录，也不计入个人配额；
- 默认仅有举报人和 `report.evidence.view` 审核员可申请 10 分钟签名 URL；
- 安全扫描未通过时不能读取；
- 未绑定附件 24 小时后进入清理；已结案附件 14 天后进入可靠 OSS 删除 Outbox；数据库保留哈希、大小、MIME 和删除时间。

## 审核流程

状态为 `pending -> in_review -> resolved_valid/resolved_invalid`，领取和处理均锁定举报行并检查 `lock_version`/当前状态，防止重复处置。结论与动作分开保存：`report_reviews` 记录结论，`moderation_actions` 记录隐藏内容、封禁等副作用及前后摘要。幂等键避免重复点击重复执行。

幂等键在单条举报的全部审核与重新打开操作之间共享。复用已有键返回 HTTP 409，并回滚本次状态、附件、处置和通知变更；重新打开后再次处理须使用新的键，不能把旧审核记录当作新一轮操作的记录。同一举报人对同一目标已有开放举报时，重新打开也返回 HTTP 409。

有效举报可选择调用各对象的权威软隐藏/驳回流程，也可创建封禁；无效举报不修改目标。处理后通过事务 Outbox 发送举报人和目标作者通知，不在事务提交前发布 NATS 消息。重新打开会恢复证据绑定并取消待删除时间。

## 权限与防滥用

- 用户：`report.create`、`report.view_own`；
- 审核：`report.review`、`report.snapshot.view`、`report.evidence.view`；
- 动作：`report.action.delete`、`report.action.ban`、`report.action.reopen`。

举报与证据上传进入现有统一反滥用 `report.create` 动作，组合用户、Session、IP、设备、内容和重复目标；开放举报有唯一索引，阻止同一用户对同一目标反复制造待处理记录。被封禁账号在统一写请求防火墙处被拒绝。
