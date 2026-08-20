# 项目权限与 Session 测试报告

## 环境

- 日期：2026-08-19
- 后端：Go 1.26.4 / Windows / PostgreSQL 开发库 `mcmods`
- 前端：Next.js 16.2.11 / TypeScript / ESLint
- Redis：自动化回归使用 `miniredis`；未使用真实多节点 Redis 集群

## 已验证行为

- 创建 Mod、整合包、共享目录项目和服务器不会授予项目角色；
- 提交者不能通过 `submitted_by` 编辑已发布项目；
- 项目页旧开发者申请路由不存在，通用编辑员申请路由存在；
- 团队认领在 HTTP 与数据库层均被拒绝；
- 一个作者最多一个 approved claimant，一个用户可认领多个作者；
- 两个独立数据库事务并发通过同一作者的竞争认领时，恰好一个事务成功；
- 待审核认领不授权，通过后直接作者项目与团队项目均生效；
- 编辑员、直接作者、团队成员三类来源可同时存在，逐个撤销不会误删其他来源；
- 待审核关系不授予权限，授权关系变更推进 `project_acl`；
- 管理员仍可手工配置项目角色或直接权限，授予和撤销只递增 `permission_version`；
- 角色绑定与直接权限变化不改变 `auth_version`，当前 Session 保持有效；
- 密码变化仍撤销缓存 Session；
- 可选认证可向前端发送统一失效信号；
- 空开发库可以安装 generation 85 和当前种子。

## 实际执行命令

```powershell
$env:GOTOOLCHAIN='local'
& 'D:\System\SDK\go\go1.26.4\bin\go.exe' test ./internal/httpapi ./internal/database
```

最终目标包回归结果：`internal/httpapi` 1.570s、`internal/database` 0.500s，均通过。

```powershell
$env:GOTOOLCHAIN='local'
& 'D:\System\SDK\go\go1.26.4\bin\go.exe' test ./...
& 'D:\System\SDK\go\go1.26.4\bin\go.exe' build ./...
```

结果：全部 Go 包测试和构建通过。

```powershell
$env:APP_ENV='development'
$env:DB_RESET_ON_START='true'
$env:DB_RESET_CONFIRM='RESET mcmods'
& 'D:\System\SDK\go\go1.26.4\bin\go.exe' run ./cmd/db-reset
```

结果：开发数据库 `mcmods` 从空库初始化到当前 Schema 和种子成功。

```powershell
$env:APP_ENV='development'
$env:MCMODS_RUN_DB_INTEGRATION='1'
& 'D:\System\SDK\go\go1.26.4\bin\go.exe' test ./internal/database ./internal/httpapi -count=1 -v
```

详细模式结果：`internal/database` 6.686s、`internal/httpapi` 54.752s；最终空库后的复跑结果：`internal/database` 5.701s、`internal/httpapi` 53.768s，均通过。真实 PostgreSQL 用例包括作者唯一认领、团队不可认领、直接作者/团队/编辑员多来源、管理员手工权限、Session/RBAC 缓存、创建入口无自动角色。

收尾审查还发现项目作者关系审核曾写入不存在的 `admin_operation_logs`。现已统一写入项目已有且不可变的 `permission_audit_logs`，并增加源级回归测试，避免该成功分支在运行时返回 500。

```powershell
$env:MCMODS_RUN_DB_INTEGRATION='1'
& 'D:\System\SDK\go\go1.26.4\bin\go.exe' test ./internal/database -run 'TestConcurrentAuthorClaimApprovalIntegration|TestDerivedProjectAccessIntegration' -count=1 -v
```

结果：派生权限用例 3.23s、真实双事务作者认领竞争用例 1.27s，均通过。

```powershell
pnpm run typecheck
pnpm run lint
pnpm run build
```

结果：TypeScript、ESLint、Next.js 生产构建均通过；59 个静态页面生成成功。

## 尚未验证

- 未在真实多实例 API + 真实 Redis 集群执行断网和恢复压测；现有测试以两个缓存实例与 `miniredis` 验证共享版本语义。
- 没有独立浏览器 E2E 框架；前端采用类型检查、Lint、生产构建和后端权限接口测试。
- 当前为开发期空库策略，没有生产历史数据迁移/回填验证。
