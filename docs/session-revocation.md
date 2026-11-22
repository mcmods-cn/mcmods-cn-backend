# Session 注销与故障恢复

## 按浏览器会话注销

注册、密码登录、邮箱验证码登录的成功响应在 `data` 内并列返回 `user` 与 `sessionMarker`。`GET /api/v1/auth/me` 在原有用户字段旁返回 `data.sessionMarker`。OAuth 重定向成功后也从该 GET 获取标记。这些响应使用 `Cache-Control: private, no-store`。标记由每次签发时的随机会话 ID 经过用途隔离的 SHA-256 计算，以 43 字符的 base64url 编码。同账号在不同次登录时获得不同标记。认证仍要求原 HttpOnly Cookie 或有效的 Bearer token。

浏览器建立退出意图时保存当时的标记。随后使用 `POST /api/v1/auth/logout/if-current`，发送 JSON 对象 `{"sessionMarker":"原会话标记"}`。重试沿用原标记。服务器先验证当前请求的认证，再校验标记，最后执行现有的会话撤销事务。

| 结果 | 会话与客户端状态 |
| --- | --- |
| 200 | 目标会话及 Presence 已撤销。条件操作不发送 Set-Cookie。即使此响应晚于另一个标签页的新登录，也不会擦除新 Cookie。原 Cookie 再次访问受保护接口时返回 401。 |
| 400 | 标记缺失、为空或请求格式有误，保留会话。 |
| 401 | 请求所带会话失效，保留浏览器当前 Cookie。 |
| 409 `AUTH_SESSION_CHANGED` | 当前凭据属于其他会话。保留当前会话、Presence 和 Cookie，客户端应结束旧意图并重新读取当前身份。 |
| 503 | 事务失败时保留原意图以便重试。`SECURITY_VERSION_REFRESH_FAILED` 且 `details.committed=true` 表示撤销已提交、缓存更新失败，详见下节。 |

既有 `POST /api/v1/auth/logout` 保留显式注销当前凭据并过期 Cookie 的语义。先升级处理认证的后端，再发布支持条件注销的前端。旧节点或回滚版本对新路径返回 404/405 时，客户端保留原意图或请求用户确认当前账号，自动回退到无条件接口会撤销另一会话。此变更没有数据库迁移。

`TestConditionalLogoutPreservesReplacedBrowserSessionsHTTPPostgres` 在隔离 PostgreSQL 和 HTTP Cookie jar 上覆盖注册、两种登录、同账号/跨账号替换、数据库事务一时不可用、条件与旧客户端显式注销，并逐项查询会话与 Presence。验证使用合成账号和数据库中的合成验证码。`TestConditionalLogoutReportsCommittedCacheFailure` 在真实 PostgreSQL 撤销后让隔离的 Redis 协议服务不可用，核验 503 的 committed 详情及 Cookie 边界。

## 撤销事务与共享缓存

`POST /api/v1/auth/logout` 在一个 PostgreSQL 事务中撤销当前 Session，并移除
对应的持久 Presence。两步都成功提交后，先发布独立撤销标记至当前令牌到期，
再删除共享 Session 正缓存并清除 HttpOnly Cookie。数据库启动事务、SQL 或提交失败返回 503，不返回假成功，
不清除客户端 Cookie，让调用方保留重试机会。

所有新请求从 PostgreSQL 主库核对会话和账号。认证查询使用会话指纹、有效期、撤销状态及 `auth_version`。读取完毕后可以再检查独立的 Redis 撤销标记，让已开始的旧查询观察并发注销。短时无效会话缓存只存拒绝结果。接收 SSE 事件和心跳也核对当前会话。

撤销已提交但共享标记发布或缓存删除失败时，返回 `503 SECURITY_VERSION_REFRESH_FAILED` 和 `committed=true, operation=logout`。新实例依数据库中的撤销拒绝该会话，同账号的其他会话继续按各自记录核验。操作员可检查缓存连通性及传播失败计数。会话、权限和滚动发布的完整边界见[认证与权限版本](../AUTH_AND_PERMISSION_VERSIONING.md)。

运行 `go test ./internal/httpapi -run 'TestOCT02Logout|TestConditionalLogout|TestSecurityVersion' -count=1` 可覆盖事务回滚、迟到读取、双实例及提交后传播中断。真实服务和测试专用环境的启动方法见[隔离验证](testing-isolated-environment.md)。

认证接口区分凭据失效与认证存储故障：真实 token/account 缺失返回 401；
Session、数据库或 RBAC 加载异常返回 503，保留 Cookie，且不发送
`X-MCMods-Auth-State: invalid`，避免暂时服务故障使前端错误退出。
OAuth callback 错误日志只记录供应商及错误类型，不输出完整请求 URL；
QQ/WeChat 的 secret、code 或 access_token 可能位于查询参数中。

# Authentication rate limits during Redis outages

Shared rate-limit admission prewarms one local attempt counter per request. If Redis becomes unavailable in a single-replica deployment, that same attempt is evaluated against the configured smaller fallback limit; it is not counted twice. Healthy Redis still uses its shared limit. A deployment configured to fail closed continues to reject admission when shared state is unavailable, without using the fallback allowance. Local windows cannot provide a combined limit across independent replicas; multi-replica deployments must retain the shared fail-closed policy.

验证码注册/登录仅把确实缺失、失效或不匹配的验证码归为 401；验证存储故障返回 503，不记录为凭据错误。`/auth/me` 区分不存在的用户（404）与存储故障（503）。用户创建仅在唯一约束冲突时返回 409，其他数据库写入失败返回 500。
