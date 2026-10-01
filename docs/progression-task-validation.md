# 等级与任务配置入口

后台任务的创建与更新均通过 `saveTask` 验证条件和奖励，然后持久化到 `task_definitions`。`condition` 必须包含支持的 action、objectType、metric，以及正整数 target。`rewards` 必须是非空对象，并提供正数经验或至少一个已启用货币的正数奖励；省略或显式传 `null` 返回 HTTP 400，不会创建任务或触发数据库写入。该修复不改变合法既有请求结构。

修改等级配置在一个 PostgreSQL 事务中更新阈值、读取用户经验并同步当前线路角色。读取失败（包括迭代阶段断连/取消）时整个事务回滚，不能发布仅更新了部分用户的配置。此事务仍会锁定所有当前经验行；大型实例执行需要维护窗口和容量评估，本次没有声称已验证线上规模。

`user_role_bindings` 目前没有区分手工与等级自动授予来源。切换或取消线路时，旧线路角色可能残留；按旧线路批量删除又可能移除手工授权。需要先明确来源/撤销策略，再设计前向迁移；本次没有删除现有授权、重定义权限模型或执行生产回填。

当前修改没有 schema/migration。可先后独立部署后端补丁，前端合法任务配置请求保持兼容。回归命令：`go test ./internal/httpapi -run TestSaveTaskMissingRewardsReturnsBadRequestCore -count=1`；两种非法输入使用真实 handler 确认 HTTP 400，数据库迭代失败边界另在审查与集成证据中注明范围。

`GET /api/v1/admin/tasks`（`task.read`）与 `GET /api/v1/admin/activity`（`activity.read`）在完整读完并检查迭代错误后才返回成功列表；数据库执行流失败返回 HTTP 500，不伪报空列表或返回内部 SQL 错误。`TestProgressionListsRejectPostgreSQLStreamFailureCoreIntegration` 在每个 ownership marker 核验的专用 PostgreSQL 数据库中先验证正常列表有数据，再保留原表/约束/触发器、用 VOLATILE 故障视图验证真实流执行失败；整库按标记清理，没有修改共享或生产数据。正常响应结构与既有权限保持兼容。
