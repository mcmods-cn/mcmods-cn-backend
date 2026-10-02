# 变更日志更新查找错误

`PUT /api/v1/changelogs/{id}` 保持原有认证、目标项目编辑权限、内容校验和审核
流程。事务内查找实体时，只有 PostgreSQL `ErrNoRows` 表示资源不存在并返回
404。连接失效、查询失败或取消等错误返回 500 和稳定业务码
`CHANGELOG_LOOKUP_FAILED`，不会再被误报告成“变更日志不存在”。

错误响应使用固定通用描述，不包含 SQL、内部表名、连接地址或驱动错误。
失败退出时事务回滚，不创建审核请求、修订或发布正文。API URL、正常返回、
既有数据与权限策略没有变化，不需要数据库迁移或回填。

隔离 PostgreSQL 回归沿真实认证 HTTP 请求，在初始项目/变更日志读取成功后，
由专用测试连接的查询跟踪器只对精确事务查找注入断连。断言命中一次、500
业务码、数据库事实完全不变及错误脱敏；真实不存在的资源仍返回 404。
测试新建独立数据库并由既有身份确认的清理机制销毁，未访问生产数据库。

```sh
MCMODS_RUN_DB_INTEGRATION=1 go test -race ./internal/httpapi \
  -run '^(TestOCT02ChangelogLookupDatabaseFailureIsNotNotFoundIntegration|TestTEST017.*|TestBUG034ManualChangelogOverrideStartsOnlyAfterApprovalIntegration)$' \
  -count=1 -v
```

设置连接前先按 [隔离测试环境说明](testing-isolated-environment.md) 核对本任务
专用回环 PostgreSQL 资源；本命令不是对任意数据库执行迁移的授权。
