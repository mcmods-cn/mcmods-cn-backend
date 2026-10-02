# Session 注销与故障恢复

`POST /api/v1/auth/logout` 在一个 PostgreSQL 事务中撤销当前 Session，并移除
对应的持久 Presence。两步都成功提交后，先发布独立撤销标记至当前令牌到期，
再删除共享 Session 正缓存并清除 HttpOnly Cookie。数据库启动事务、SQL 或提交失败返回 503，不返回假成功，
不清除客户端 Cookie，让调用方保留重试机会。

Session 正缓存使用 `GetSharedOrLoadTTL`，不读取进程内的正缓存副本。另一 API
实例注销后，已预热实例的下一次请求检查共享撤销标记并拒绝撤销的 Session。
撤销键与短时 session-invalid 负缓存分开，不被失效操作删除或短 TTL 覆写。
解析开始、Session loader 完成以及用户版本核验完成后均复核撤销标记，保护
“读到旧 Session → 注销提交 → 旧 loader 迟到写回正缓存”的竞态。迟到正值
可能仍被写入，但不能替换独立标记或再次通过认证；标记保留至 JWT 过期。
Redis 不可用时核验 PostgreSQL 权威 Session，不接受旧 L1 认证结果。
暖 Redis 命中仍无需额外 Session 数据库查询；权限版本规则沿用
`AUTH_AND_PERMISSION_VERSIONING.md`。

撤销已提交但共享撤销标记发布或缓存删除失败时，沿用 `SECURITY_VERSION_REFRESH_FAILED`，
返回 503 并提供 `committed=true, operation=logout`。这时数据库撤销已经生效，
不能宣称完全注销成功；共享 Redis 中的旧条目仍可能在其 TTL 内存在，需要
修复缓存连通性或等待到期。该边界不通过撤销其他设备 Session 来隐藏。

定向验证：`go test ./internal/httpapi -run 'TestOCT02Logout' -count=1`。
独立 PostgreSQL 测试验证 Presence SQL 故障完整回滚，并用两个 API Cache 实例
及本地 Redis 协议服务验证预热实例拒绝已注销 Session；pgx query tracer
暂停已完成的真实 Session SELECT，在注销成功后释放旧 loader，验证迟到
缓存回填和后续认证都被拒绝。关闭连接池验证
503 及 Cookie 保留；未操作真实用户会话。

认证接口区分凭据失效与认证存储故障：真实 token/account 缺失返回 401；
Session、数据库或 RBAC 加载异常返回 503，保留 Cookie，且不发送
`X-Auth-State: invalid`，避免暂时服务故障使前端错误退出。
OAuth callback 错误日志只记录供应商及错误类型，不输出完整请求 URL；
QQ/WeChat 的 secret、code 或 access_token 可能位于查询参数中。

部署时需更新所有处理 Session 认证的 API 实例，旧版本不检查新的撤销标记，
混用旧认证实例期间不能承诺上述竞态保护。用户/权限版本指针仍沿用 10 秒
核验 TTL，未把该边界描述为严格瞬时的跨实例权限撤销。
# Authentication rate limits during Redis outages

Shared rate-limit admission prewarms one local attempt counter per request. If Redis becomes unavailable in a single-replica deployment, that same attempt is evaluated against the configured smaller fallback limit; it is not counted twice. Healthy Redis still uses its shared limit. A deployment configured to fail closed continues to reject admission when shared state is unavailable, without using the fallback allowance. Local windows cannot provide a combined limit across independent replicas; multi-replica deployments must retain the shared fail-closed policy.

验证码注册/登录仅把确实缺失、失效或不匹配的验证码归为 401；验证存储故障返回 503，不记录为凭据错误。`/auth/me` 区分不存在的用户（404）与存储故障（503）。用户创建仅在唯一约束冲突时返回 409，其他数据库写入失败返回 500。
