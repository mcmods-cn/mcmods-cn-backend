# 反机器人与反滥用测试报告

## 环境

- 日期：2026-08-15，Windows 开发工作站，Go 1.26.4，Node 24.19，Next.js 16.2.11。
- 后端：本地 `127.0.0.1:8080`，开发数据库按 schema generation 71 真实重置后启动。
- PostgreSQL：开发数据库为网络连接；Redis 未启用，因此单实例测试使用有界本地原子计数。
- 外部 Turnstile 未使用真实密钥；用 HTTP 503 模拟供应商故障。真实搜索引擎 DNS 在单元测试使用可控双向解析器。

## 实际执行命令

```text
go test ./internal/antiabuse ./internal/querycache ./internal/config ./internal/database ./internal/httpapi
go test ./...
MCMODS_RUN_DB_INTEGRATION=1 go test -run TestFormAndHumanChallengesAreBoundAndSingleUse -count=1 -v ./internal/antiabuse
MCMODS_RUN_DB_INTEGRATION=1 go test -run TestAntiAbuseQueryPlans -count=1 -v ./internal/antiabuse
go test -race ./internal/antiabuse ./internal/querycache
pnpm run lint
pnpm run typecheck
pnpm run build
node --check scripts/load-test.mjs
```

`-race` 在当前 Windows Go 环境因 CGO 未启用而未执行，工具明确返回 `-race requires cgo`；并发正确性改由 64 并发限流测试、12 并发表单令牌消费和真实 HTTP 幂等重放验证。

## 结果

- 后端全量包测试通过；前端全量 ESLint、TypeScript 与生产构建通过，53 个静态页面生成完成。
- 最终数据库集成回归第一次连接网络开发 PostgreSQL 时发生一次 TCP 建连超时；当时运行中的后端健康检查仍为 ready，连通性随即恢复，原命令不改代码立即重跑后两项集成测试全部通过。该波动没有记作功能通过前的成功结果。
- 内容规范化覆盖 NFKC、零宽字符、Markdown、URL 跟踪参数和近似文本；测试发现并修复“先清 Markdown 下划线会破坏 `utm_*` URL”的顺序缺陷。
- 本地限流 64 并发下严格只允许配置的 11 个请求。
- 表单令牌 12 并发消费仅 1 次成功；过期/重放进入挑战；挑战一次性消费，复用不通过。
- 测试发现挑战重试会被并发内容 claim 和速率计数再次拦截，已改为“挑战通过后跳过本次重复 claim 与限流”，但仍执行权限和业务校验。
- Turnstile 模拟 503 时，风险结果降级为 `moderation`，没有永久关闭写接口。
- 真实 HTTP：相同评论幂等键两次返回同一个评论 ID；换幂等键提交相同内容返回 HTTP 403 `duplicate_content`。
- 未登录访问风控后台为 401；伪造 Googlebot 只读请求按可疑爬虫预算处理；匿名爬虫写请求为 401。
- Cookie 写请求缺少 Origin 或跨站 Origin 均为 403；Bearer API 请求不受浏览器 CSRF Origin 规则误伤。
- 管理配置拒绝未知动作和乱序阈值；生产环境拒绝开发 HMAC/IP 密钥。
- 最终调用链复核删除了评论写事务中旧的最近一分钟 `count(*)` 硬编码限流；统一风控负责频率判定，幂等 advisory lock 与数据库唯一键继续负责并发去重。针对性 HTTP/风控/缓存包测试随后通过。

## 查询计划

真实 `EXPLAIN` 结果：

- 指纹用户/动作/时间查询使用 `idx_anti_abuse_fingerprint_user_action` Index Only Scan。
- 活动限制使用 `idx_anti_abuse_restrictions_user_active` Index Scan。
- 最近风险事件在当前数据分布选择 `idx_anti_abuse_events_time` 后过滤动作；后台无动作筛选正好使用该顺序。动作索引仍供高选择性条件使用。

## 安全检查

- 服务端动作白名单、对象作用域、表单/挑战 Token 绑定和一次性原子消费均已测试。
- 真实 IP 继续复用可信代理 CIDR 解析，不信任任意 `X-Forwarded-For`。
- IP/设备/Session 只保存 HMAC；机器人 Token 和挑战 Token 只保存摘要。
- 管理接口由三项独立权限保护；人工事件处置、规则、限制与解除都写管理员审计。
- 精确重复并发 claim 关闭“同时查不到历史指纹”的竞争；评论本身另有数据库唯一键和 advisory lock。

## 未完成的外部验证

- 未连接真实 Redis 集群验证多副本 Lua 计数；代码与启动校验已完成，生产部署前仍需两副本预发布演练。
- 未调用真实 Turnstile，也未从 Google/Bing 实际地址发起 DNS 验证；使用可控解析器和故障服务器测试。
- 未对生产/CDN 发压；所有压测均只访问本机 API 和开发数据。
