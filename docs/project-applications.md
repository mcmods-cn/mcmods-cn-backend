# 作者认领与项目编辑员申请

权限来源与撤销规则沿用 [项目权限模型](../PROJECT_PERMISSION_MODEL.md)。提交者不是项目权限来源，提交认领或申请不会直接授予项目访问；作者认领始终进入 `pending`，后台审核通过后才产生既有派生访问，编辑员审核通过后才建立 assignment。

作者认领 `POST /api/v1/creators/{publicId}/claims` 至少提供去空后的证明文字或一个有效的本人附件。文字最多 10,000 个 Unicode 字符；附件仍限制 5 个、总计 10 MiB，必须归当前用户所有、处于 active 且扫描 clean/trusted_generated。空字符串、任意文件标识、他人附件不能充当证明。团队不能认领，同一用户与作者的待审唯一约束及每位作者的已批准唯一约束保持不变。

编辑员申请 `POST /api/v1/projects/{projectType}/{projectId}/editor-applications` 仍要求证明文字（最多 10,000 个 Unicode 字符）及最多 10 个本人安全附件。前端 textarea 的 10,000 UTF-16 单元限制可能对补充平面字符更严格；后端不再按 UTF-8 字节误拒合法中文证明。

以下 GET 列表支持 `limit`（默认 100，1–200）及 `offset`（默认 0，0–1,000,000），继续返回 `items`，新增 `total`、`limit`、`offset`、`hasMore`：

- `/api/v1/admin/creator-claims`：拥有 `creator.claim.review` 的审核员读取待审个人作者认领，按 created_at/id 稳定升序。
- `/api/v1/admin/project-editor-applications`：沿用 `project.editor.review` 权限；status 默认 pending，支持 approved/rejected/withdrawn，按 created_at/id 稳定降序。
- `/api/v1/projects/{projectType}/{projectId}/editor-applications`：已登录用户仅能读取自己的该项目申请历史；不允许缺失身份退化为无用户条件。

计数与页面查询独立执行，并发审核可能改变 total；前端刷新后把越界页移回最后有效页。审核界面每页 50 条，保留备注输入，审核完成刷新当前页，并提供前后翻页。旧版后端不返回 total 时，新界面仍展示完整列表而不创建虚假的分页。先部署前端分页入口，再部署后端有界列表；旧前端搭配新后端会暂时只显示默认第一页，不作为完整滚动升级组合支持。

作者批准/撤销、编辑员授权/撤销与 permission_audit_logs 写入在同一事务；审计写失败立即返回 500 并回滚权限状态。审批与附件列表检查 rows.Err，先释放外层结果再批量读取附件；编辑员列表通过有发布状态约束的连接解析名称，审核名称查询使用原事务，单连接池不需要再等待自己持有的连接。Creator 角色定义是完整配置目录，保持其全量兼容响应，但解码与迭代错误不能返回假成功。

作者撤销及编辑员审批的英文通知显式记为 en-US；中文评论调用仍沿用 zh-CN。通知投递使用既有队列/本地回退，属于提交后的通知过程；本次不声称实现数据库与供应商端的严格一次投递。permission_version 的数据库触发器负责访问来源变化，原有刷新调用保留；没有真实邮件、OSS 或 AI 外发验证。

`internal/httpapi/applications_core_integration_test.go` 使用 ownership marker 核验的随机 PostgreSQL 测试数据库、完整迁移及 SeedRBAC。单连接池验证列表、持久化、有效附件、派生访问、撤销、英文/中文通知；在各独占库的真实审计表加入拒写约束验证回滚，测试后按 marker 清理整个数据库，保留真实审计不可修改触发器与外键。共享业务数据库、生产迁移和生产健康未验证。

作者主页当前作品与计数只展示 mod。是否将全部 Creator bindings 对应类型加入作品展示，需要确认作品定义、排序和筛选的产品意图，本次不扩展该定义。
