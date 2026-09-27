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

## 可观测性与演练

后台基础设施指标中的 `redis.rateLimitFailClosed` 记录因共享限流不可用而拒绝的次数；Redis 命令错误和超时仍分别进入既有 `errors`、`timeouts` 指标，`/ready` 在 Redis 不可达时也会失败。

自动故障演练会启动两个共享同一 Redis 的缓存实例，确认正常额度跨实例累计，再中断 Redis 并确认两个实例都失败关闭；同时覆盖登录和匿名高成本读取入口：

```powershell
go test ./internal/querycache ./internal/config ./internal/httpapi ./internal/antiabuse -run SEC003 -count=1
```
