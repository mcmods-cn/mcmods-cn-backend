# 组合功能测试报告

日期：2026-08-17  
环境：Windows，Go 1.26.4 (`D:\System\SDK\go\go1.26.4`)，Node bundled runtime，Next.js 16.2.11，开发 PostgreSQL。

## 修改前基线

- `go test -count=1 ./...`：通过。
- TypeScript、ESLint、Next 生产构建：通过。
- `pnpm check` 的脚本内部调用 `npm`，当前隔离运行时没有 npm，因此基线改为直接执行 eslint 与 tsc；这不是代码失败。

## 本次已执行命令

```powershell
$env:GOTOOLCHAIN='local'
D:\System\SDK\go\go1.26.4\bin\go.exe test -count=1 ./internal/httpapi ./internal/database
D:\System\SDK\go\go1.26.4\bin\go.exe test -count=1 ./...
$env:MCMODS_RUN_DB_INTEGRATION='1'
D:\System\SDK\go\go1.26.4\bin\go.exe test -count=1 ./internal/database ./internal/httpapi
node node_modules/typescript/bin/tsc --noEmit
node node_modules/eslint/bin/eslint.js .
node node_modules/next/dist/bin/next build
```

## 结果

- 后端完整单元/包测试：通过，所有列出的 Go 包为 `ok` 或无测试文件。
- 后端针对性 HTTP/API 与数据库测试：通过。
- 开发 PostgreSQL HTTP 集成最终复测：通过（39.622 秒）。
- 开发 PostgreSQL数据库集成第一次运行：失败，发现 4 个新增外键缺少通用前导索引。
- 修复：新增 recipe created_by、log owner/source file、comment attachment 前导索引，并增加 78→79 迁移。
- 修复后开发 PostgreSQL数据库集成：通过；最终复测 2.368 秒。
- TypeScript：通过。
- ESLint：通过。
- Next.js 生产构建：通过，生成 55 个静态页面并包含 `/tools/logs`、`/log/s/[code]`、`/admin/global-resources`。

## 自动化覆盖

- 合成表适用版本权威顺序与分组。
- 新迁移关系、唯一索引和禁止 `MAX(floor)+1` 静态约束。
- 评论树集成：两个顶级评论楼层 1/2，回复楼层 NULL。
- 脱敏规则：Authorization、邮箱、IP、用户目录统一替换 `❄`。
- ZIP：路径穿越拒绝、重复文件名拒绝、文本条目脱敏。
- 保留时间和随机短码边界。
- 全量既有 RBAC、反滥用、评论、目录、缓存、Outbox 等回归包。

## 未执行或未充分验证

- 没有真实生产 OSS 大对象、CDN、多实例 Redis/NATS 环境；日志文件流程只做代码路径、单元测试和构建验证。
- 没有数百万评论/合成表数据，未得到生产级回填时长和锁等待数据。
- 前端仓库没有浏览器测试框架，因此父分类键盘操作、跨页楼层滚动和评论附件交互仅通过类型、Lint、构建与代码审查验证，未形成 Playwright 结果。
- 没有执行攻击式大 ZIP 压测或数百 MiB 日志测试；代码上限会先拒绝该输入。
- 本次没有伪造 P50/P95、CPU、内存等性能数字；未执行专门负载测试。

## 查询与并发检查

- 版本集合由列表/详情 lateral 聚合，避免卡片 N+1。
- 全局资源绑定列表使用单条连接查询与分页，外键索引由真实 schema quality 测试校验。
- 评论楼层只锁当前对象计数器行，唯一索引防止竞态重复。
- 相同源日志并发创建由部分唯一索引裁决，冲突请求回读并复用成功分享。
