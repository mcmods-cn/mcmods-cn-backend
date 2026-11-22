# Redis Key 与失效规则

所有 Key 的完整前缀为 `{REDIS_PREFIX}:{REDIS_NAMESPACE}:`。生产环境必须使用独立命名空间。Key 参数均先校验，最长 512 字节。

| Key（省略公共前缀） | 内容 | 默认 TTL | 失效/重建 |
|---|---|---:|---|
| `session:{sha256(sessionToken)}` | 旧版写入的正缓存 | 旧条目的剩余 TTL | 当前鉴权读 PostgreSQL；登出清除此兼容键 |
| `session-invalid:{sha256(token)}` | 无效 Session 短缓存 | 最长 5s | 上限同时受会话剩余时间与 Session TTL 配置限制，数据库故障不写入 |
| `session-revoked:{sha256(token)}` | 已提交注销的会话指纹标记 | 至令牌到期 | 认证读库后检查此标记，已开始的旧读取也有机会观察并发注销 |
| `auth:user-version:{publicId}`、`authz:user-version:{id}` | 用户认证及授权版本的兼容发布提示 | 10s | 凭据、账号状态或用户授权变化后由写者发布 |
| `versions:rbac`、`versions:project-acl` | 全站权限及项目 ACL 版本的兼容发布提示 | 10s | 角色/权限或项目关系变化后由写者发布。当前鉴权从主库读取所有版本 |
| `authz:2:v{rbacVersion}:acl{projectACLVersion}:user:{id}:p{permissionVersion}` | 有效来源均无到期时间的角色和权限结果 | 120s | 任一授权版本变化自动换 Key，涉及临时授权的结果每次访问回源 |
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

- 每次会话认证读取主库。稳定 RBAC 结果按当前版本共享，临时授权由当前调用单独核验。设置和卡片沿用各自的热点回源合并。
- 认证限流：切换更严格的有界本地计数，记录 `rateLimitFallbacks`，不无限放行。
- Presence：使用单机短期状态；不把每个心跳改写 PostgreSQL，对外遵守隐藏/安全降级。
- 未读：短期进程缓存命中则使用；否则一次合并 PG 查询重建，禁止恢复成前端每 20 秒两个 COUNT；后台游标批次与管理员接口可主动校准。
- Redis 重启后，从当前 PostgreSQL 事实重建可缓存结果。安全版本提示在后续成功发布时恢复，Presence 由新心跳形成。

## 指标

管理接口 `/api/v1/admin/infrastructure/metrics` 返回 Redis请求、L1+Redis缓存命中/未命中、Redis命中/未命中及命中率、错误、超时、P95、PostgreSQL回源、限流降级、Presence写入和未读重建。该接口要求 `admin.config.read`。
