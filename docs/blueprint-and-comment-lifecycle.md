# 蓝图与评论异步生命周期

蓝图 soft delete 后，任何角色均不能通过 variant download 接口取得新的对象下载
地址。原有删除前已经签发的短时签名地址受其有效期约束，本次不改变对象存储
的签名协议，也不声称即时撤销已签地址。

上传 complete 的并发准入锁取得后，事务重新读取蓝图状态并加行锁，只接受
uploading / failed；queued 的重复 complete 保持既有幂等返回。锁等待期间发生
删除时返回资源不存在，不把 deleted 重置成 queued。无 Schema 或 API URL 变化。

蓝图派生物 PUT 返回后重新核对任务 run token、attempt，以及精确 artifact/file/key
身份和 pending 状态。失去主体或租约的结果返回 lease lost；独立于请求取消的
10 秒清理上下文把该 key 的删除意图重新入队。已经 abandoned/deleted 的同一
派生物允许重开 completed/dead 删除意图，继续复用唯一 Outbox 行；active 文件
与其他 attempt/key 不允许走这条补偿。数据库写入失败明确返回，事务不部分提交。
若物理 DELETE 已结束但旧任务尚未确认，精确 file/bucket/endpoint/key 的 processing
删除任务也会在同一事务中重置为 pending 并清除旧 token。该更新等待删除执行者
持有的共享行锁及 HTTP 副作用结束；旧 complete/failure 的 token 条件写入随后失败，
新执行者再删除迟到字节。其他文件实例的 token 不允许重置，补偿写入失败全部回滚。
这条 PUT 后补偿只由新版本蓝图 worker 执行。部署时先排空或停止旧蓝图 worker，
再替换 worker 与 API；保留现有持久化队列与恢复机制。旧 worker 在滚动替换中仍然
活跃时会执行旧路径，不能声称新旧 worker 混合运行也具备这条严格补偿保障。
本说明是部署顺序要求，本次没有停止或部署任何生产 worker。
这覆盖活跃进程在首次删除完成后才收到迟到 PUT 结果的窗口；真实 PostgreSQL
和本地 OSS HTTP 验证租约回收、任务/蓝图级联删除、清理故障回滚、取消后清理、
错误 key 拒绝及正常新 attempt 的 active 保护。

数据库与外部 OSS 没有跨系统事务。本次不证明进程在 PUT 返回后、补偿持久化前
崩溃，或供应商在客户端超时/取消后仍继续写入时完全不存在孤儿对象；长期数据库
故障期间也不能保证立即清理。保留精确 key 的 artifact 和 Outbox 记录用于恢复，
不能以此测试结果宣称全部外部对象严格一次写入/删除或生产存储已健康。

creator 关系/成员/团队/作品、蓝图列表/所需模组和评论作者头像使用共享 Stored OSS
批量授权：先完整读取并关闭列表 rows，再按整页 URL 查询权限和签名，避免持有
连接期间回入连接池，也不对每个内部图片重复查询。未经可见绑定或所有者许可的
私有内部文件不返回下载地址；细则见共享 OSS 实现及元数据导入说明。

评论日志附件处理保留有界 attempts、独立 lease token 和事务最终写入检查。
进程在最后一次 attempt 崩溃后，恢复器现在按有限批次把过期 processing 任务转为
failed，并同步附件 failed；重复恢复不会重新执行已耗尽任务，也不清理源附件。
插眼标记已读先在事务中取得其行锁，再读取新快照并同时更新回复 read_at 与
未读计数，避免等待并发新回复时把未读数归零却留下未读回复。
该恢复修复提供明确失败状态；本次没有新增管理员重试界面或放宽文件下载权限。

验证：`go test ./internal/httpapi -run 'TestOCT02DeletedBlueprint|TestOCT02UploadCompletion|TestOCT02CommentLogLastAttemptCrash|TestOCT02CommentWatchRead|TestCommentLogAttachment' -count=1`。
使用专用 PostgreSQL 随机私有 Schema，覆盖访客/所有者/管理员下载拒绝、实际
advisory lock 等待期间删除及最后一次 attempt 崩溃。无需 Schema 迁移，部署后端
即可生效。评论计数并发的 Schema 修复由对应前向迁移与数据库报告单独说明。

评论列表保留分页所选主评论，最多预览 64 条、深度不超过既有 3 层的回复。
预览选择先应用作者屏蔽，再按 depth/created_at/id 排序限制数量；装饰与附件查询
只处理所选节点。按实际加载子节点设置 hasMoreReplies，遗漏内容通过既有
回复分页继续加载；不删除回复、不隐藏展开入口。节点预算不等于生产查询耗时
或响应字节上限已验证，代表性生产分布的执行计划尚未取得。

创作者头像绑定/发布在其现有事务中读取 OSS 设置；创作者与蓝图编辑也在
原事务中读取审核配置，避免连接池仅剩一个连接时递归等待。同一事务既有
权限、可信 raster、版本与人工审核限制保留。专用真实 PostgreSQL 单连接验证
头像绑定读取本事务未提交的测试设置且事务可继续执行；没有访问对象存储。
# Blueprint input budgets

Blueprint NBT is decompressed into a bounded 32 MiB buffer, then its complete wire structure is checked before the generic decoder allocates collections. Negative, incomplete and excessive collection lengths, unknown tags, trailing documents and nesting deeper than 64 levels are rejected. An aggregate two-million-value and 128 MiB allocation accounting budget limits expansion into maps and lists; the accounting budget is a processing limit, not a measured heap guarantee. Existing dimension, block, material and normalized-output limits still apply.

Legacy `.schematic` block and metadata arrays must exactly match the declared volume. Optional `AddBlocks` must contain one packed nibble per block; those high ID bits are combined with `Blocks` before air filtering. Truncated inputs now fail instead of silently dropping blocks or inventing missing metadata. Numeric IDs remain numeric legacy states and retain the existing warning about modern resource-location mapping.

社区帖子创建/编辑保留审核冲突的 409；修订持久化故障返回通用 500，不把内部 SQL、表名或约束错误放入响应。对象 ID 和脱敏错误类型保留在现有服务端故障日志中。

项目指标头像先关闭 rows，再一次批量授权，真实单连接测试覆盖 100 个作者/编辑者并限制为最多三次查询。访问统计的去重键使用带所有者 Token 的暂时预留；事务失败释放自己的预留，成功保留原 24 小时去重窗口。迟到释放不能删除更新的预留。Redis 释放故障向调用方报告；故障期间本地可重试，但其他实例的原共享键可能保留至窗口到期，不保证跨系统严格只计一次。自动活动清理在原 advisory lock 连接上读取配置、处理批次和记录结果，避免仅一个连接时循环等待；没有改变保留策略或扩大清理范围。
