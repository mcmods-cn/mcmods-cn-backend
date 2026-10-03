# Redis 共享限流故障合同

共享限流在多副本部署中属于安全依赖，而不是可静默退化的普通缓存。

## 部署要求

`APP_REPLICA_COUNT` 大于 1 时，配置校验要求同时启用：

```dotenv
REDIS_ENABLED=true
REDIS_REQUIRED=true
REDIS_RATE_LIMIT_FAIL_CLOSED=true
REDIS_AUTH_RATE_LIMIT_ENABLED=true
```

`REDIS_REQUIRED` 让副本在 Redis 不可达时拒绝启动；`REDIS_RATE_LIMIT_FAIL_CLOSED` 处理已经启动后的中断。运行时 Redis 命令失败时，登录、统一反滥用、公开高成本读取、草稿写入、Presence 和浏览量入口不会切换到每进程额度，而会返回限流拒绝。Redis 恢复后，下一次请求会直接恢复共享计数，无需重启副本。

单副本开发环境可以把 `REDIS_RATE_LIMIT_FAIL_CLOSED` 保持为 `false`，继续使用有界进程内 fixed-window 计数；不得把这种模式用于多副本。

## 公开 Presence

匿名心跳忽略客户端提交的 `visitorId`，用服务端 HMAC 对可信来源 IP 与规范化的 User-Agent 派生身份；同一 IP 的不同 User-Agent 共享来源额度。只有配置可信代理的请求才使用代理传入的客户端 IP。

公开 Presence 不受 `ANTI_ABUSE_ENABLED` 开关控制。因此 staging/production 即使关闭普通反滥用，也必须配置独立、至少 32 字符且非开发默认值的 `ANTI_ABUSE_HMAC_SECRET`；否则启动校验失败。development/test 在关闭反滥用时保留原有配置兼容性。修改此密钥会重建匿名身份，原有短期在线记录会自行过期，无数据库迁移。

共享来源额度是 5 分钟 60 次，单副本本地后备额度是 5 分钟 12 次。成功共享请求同时预热本地计数，Redis 中断不能为该副本重置已消耗的后备额度。访客集合最多保留共享 50,000 项、本地 4,096 项；Redis 写入在同一 Lua 调用中完成过期清理和容量裁剪。在线数是有界的近似活跃访客数，不表示独立真人数量，也不保证不同 IP 的恶意流量不能影响统计。

## 可观测性与演练

后台基础设施指标中的 `redis.rateLimitFailClosed` 记录因共享限流不可用而拒绝的次数；Redis 命令错误和超时仍分别进入既有 `errors`、`timeouts` 指标，`/ready` 在 Redis 不可达时也会失败。

自动故障演练会启动两个共享同一 Redis 的缓存实例，确认正常额度跨实例累计，再中断 Redis 并确认两个实例都失败关闭；同时覆盖登录和匿名高成本读取入口：

```powershell
go test ./internal/querycache ./internal/config ./internal/httpapi ./internal/antiabuse -run SEC003 -count=1
```
