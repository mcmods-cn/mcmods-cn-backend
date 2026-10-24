# 审核、修订与自动化修复的兼容及核查说明

适用问题：RPH-001（含 EXT/EXT2）、CONT-001、CMP-006、CMP-004、RPH-005、SL-002、SL-003。

## 请求与响应兼容

更新日志 `GET /api/v1/changelogs/{id}` 的 `item.publishedRevisionId` 是公开修订 ID 或 null。
修改请求必须携带同一个 `baseRevisionId`。服务端先检查输入，再在对象的事务聚合锁内重新比较发布指针。
旧页面缺失或使用陈旧基线会收到 409；不产生修订、发布、奖励或审核副作用。用户应保留输入、重新加载并人工合并。

旧后端严格拒绝未知字段，新前端的请求不能先于后端启用此字段；新后端又会拒绝旧客户端缺失基线的更新。
因此应将这次后端与前端作为一个发布组合，在写入维护窗口先更新后端再更新前端、失效旧前端资产并要求旧编辑页重新加载。
兼容窗口内明确暂停更新日志写入；不得通过允许缺失基线回避并发保护。现有读取、创建与独立审核契约保留。
不需要数据库迁移。回滚旧后端会重新引入陈旧写入漏洞，因此不支持只回滚后端后继续开放编辑。

资源 presentation 批量接口保留 `items` 字段，新增逐条 `inputIndex` 和 `unresolvedInputIndices`。
每个输入独立对应结果；多个别名指向同一实体时，结果保留多个输入映射。客户端不能依赖结果排序或以规范名称匹配别名。
短链动态规范化历史经济对象路径为现有用户经济工作区；手工资源使用当前公开资料版本，保留导入来源回退。

## 评论人工审核

`GET /api/v1/admin/comments/pending?status=pending|hidden` 提供当前全站评论审核员队列（单页 limit 1–100）。
`PATCH /api/v1/comments/{id}/review` 接受 `{status:"approved"|"rejected"|"restore",note}`。
全站 `comment.moderate` 沿用全局治理能力，项目范围审核员只能审核对应项目且不能审核自己。
父对象当前不可读时禁止批准。重复决定返回 409；状态、审核事件、通知意图和原作者活动意图在同一事务提交。
驳回使用 hidden 状态。恢复只将本入口驳回且从未批准的评论放回 pending，禁止用恢复绕过一般删除/隐藏以及重复首次发布奖励。
pending 评论不增加公开回复通知、插眼未读或首次发布奖励。纯作者权限不获得审核能力。

本次集成测试使用受控反滥用决定驱动真实创建 handler，审核阶段走真实 HTTP/PostgreSQL。
它不代表真实反滥用挑战、浏览器与队列消费者的完整链路已经验收。

## 自动化授权

自动更新和爬虫始终使用真实非交互系统账户，不借用管理员。开始执行、供应商读取后的写入事务、爬虫创建提交前均解析当前账户和有效 RBAC。
事务使用现有数据库连接解析 RBAC，避免连接池为 1 时嵌套申请连接。
明确 deny、到期和项目范围参加有效权限决定。自动批准依赖当前免审资格；撤销项目创建权限不允许爬虫通过直接 handler 调用绕过。
在途交互审核的撤权时点（GR-Q01）、重复导入批准审计语义（GR-Q02）和归档资料历史访问（RPH-Q01）仍需产品决定，未擅自更改。

## 只读存量诊断

以下 SQL 只读；应在有权访问的副本执行并人工核对。检测命中不是自动补偿或撤销依据。

```sql
-- 同一基线分叉的已批准修订：旧并发缺陷的候选历史，不能自动决定哪份文本应获胜。
select revision.aggregate_type,revision.aggregate_key,revision.base_revision_id,
       array_agg(revision.public_id order by revision.id) approved_children
from content_revisions revision join change_requests request on request.proposed_revision_id=revision.id
where revision.aggregate_type in ('mod_content_version','mod_content_template','mod_content_section','mod_content_resource','project_changelog')
  and request.status='approved'
group by revision.aggregate_type,revision.aggregate_key,revision.base_revision_id
having count(*)>1 and revision.base_revision_id is not null;

-- 旧 pending 评论与可能已提前写入的活动，仅用于核对，不改经验、货币或既有账本。
select comment.public_id,comment.created_at,
       count(activity.id) activity_intents
from comments comment left join activity_event_outbox activity on activity.object_public_id=comment.public_id
where comment.status='pending'
group by comment.id,comment.public_id,comment.created_at;

-- 历史自动发布及其当前账户状态；当前状态不能证明发布时权限，需与审核/授权历史交叉核对。
select revision.public_id,revision.aggregate_type,revision.aggregate_key,
       revision.created_at,actor.public_id actor_public_id,actor.status current_actor_status
from content_revisions revision join users actor on actor.id=revision.created_by
join change_requests request on request.proposed_revision_id=revision.id
where revision.source='auto_update' and request.status='approved';
```

本次未对生产数据执行上述诊断或修复。旧丢失内容只能由不可变修订历史和用户确认恢复，不能自动覆盖当前发布文本。
历史错误活动/奖励的补偿必须连接实际奖励账本，先 dry-run、计数核对和明确授权；不自动调整真实余额。
