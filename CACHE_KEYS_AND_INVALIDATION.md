# Redis Key 与失效规则

所有 Key 的完整前缀为 `{REDIS_PREFIX}:{REDIS_NAMESPACE}:`。生产环境必须使用独立命名空间。Key 参数均先校验，最长 512 字节。

| Key（省略公共前缀） | 内容 | 默认 TTL | 失效/重建 |
|---|---|---:|---|
| `session:{sha256(sessionToken)}` | 最小 Session、用户状态、auth_version、过期时间 | 60s 且不超过 Session 剩余寿命 | 登出直接删；封禁/密码变更由 auth_version 最迟 10s 生效；未命中回源 PG |
| `session-invalid:{sha256(token)}` | 无效 Session 负缓存 | 5s | 新建 Session 时清除；数据库故障不写负缓存 |
| `auth:user-version:{publicId}` | 用户 auth_version | 10s | 密码、封禁、停用或强制退出等安全事实变化 |
| `versions:rbac` | 持久全局 RBAC 版本 | 10s | 角色/权限/角色权限触发器递增 |
| `versions:project-acl` | 持久项目关系 ACL 版本 | 10s | 作者/团队项目关系及团队成员关系变化 |
| `authz:user-version:{id}` | 用户 permission_version | 10s | 用户角色、直接权限、作者认领和编辑员 assignment 变化 |
| `authz:v{rbacVersion}:acl{projectACLVersion}:user:{id}:p{permissionVersion}` | 编译后的角色、管理员手工权限、派生项目权限和规则 | 120s | 任一授权版本变化自动换 Key，旧 Key 自然过期 |
| `limit:{action}:{dimensionHash}` | Lua 原子固定窗口计数 | 窗口长度 | 自动过期；Redis 故障用更严格本地额度 |
| `presence:user:{id}:sessions` | sessionHash -> 过期时间的 ZSET | 150s 滑动 | 心跳清理过期成员；登出删除会话成员 |
| `presence:users` | userId -> 最晚过期时间 ZSET | 成员分值 | 批量 ZMSCORE；不使用 KEYS/SCAN |
| `chat-presence:user:{id}` | conversationId | 30s | 心跳覆盖，TTL 自然离线 |
| `unread:user:{id}` | 通知、消息、epoch Hash | 300s | 事务后原子增减；不确定时删 Key，由 PG COUNT 重建 |
| `unread:global-epoch` | 全局失效版本 | 长期 | 全局通知变化递增，旧用户 Hash 懒失效 |
| `versions:settings` | 持久设置版本 | 10s | system_settings 触发器递增 |
| `settings:v{version}:{whitelistedKey}` | 非敏感公开设置 | 60s | 版本化换 Key；Secret 永不写入 Redis |
| `user-card:public:{userId}` | 公开资料、等级、公开统计和六格配置 | 45s+客户端60s | 用户资料/展示配置保存后删；Presence 不在其中 |

## 敏感信息

- Session Token、邮箱、Cookie、密码、OAuth/OSS/SMTP/AI Secret 不出现在 Key 或普通 Redis Value 中。
- 邮箱和 Session 采用稳定 SHA-256 摘要；IP 按可信代理解析后规范化并进入限流维度。
- 指标和日志不输出完整 Key、Token、密码或 Cookie。

## 故障降级

- Session/RBAC/设置/卡片：回源 PostgreSQL，并用 singleflight 合并同进程热点回源。
- 认证限流：切换更严格的有界本地计数，记录 `rateLimitFallbacks`，不无限放行。
- Presence：使用单机短期状态；不把每个心跳改写 PostgreSQL，对外遵守隐藏/安全降级。
- 未读：短期进程缓存命中则使用；否则一次合并 PG 查询重建，禁止恢复成前端每 20 秒两个 COUNT；后台游标批次与管理员接口可主动校准。
- Redis 清空：所有 Key 可由 PostgreSQL事实或新心跳自动重建。

## 指标

管理接口 `/api/v1/admin/infrastructure/metrics` 返回 Redis请求、L1+Redis缓存命中/未命中、Redis命中/未命中及命中率、错误、超时、P95、PostgreSQL回源、限流降级、Presence写入和未读重建。该接口要求 `admin.config.read`。
