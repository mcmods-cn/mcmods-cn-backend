# 反机器人与反滥用配置

## 环境变量

| 变量 | 开发默认值 | 生产建议 |
| --- | --- | --- |
| `ANTI_ABUSE_ENABLED` | `true` | `true` |
| `ANTI_ABUSE_HMAC_SECRET` | 开发占位密钥 | 独立随机 32+ 字符；不得复用 JWT |
| `ANTI_ABUSE_IP_HASH_SECRET` | 开发占位密钥 | 独立随机 32+ 字符；定期受控轮换 |
| `ANTI_ABUSE_CHALLENGE_PROVIDER` | `proof` | `turnstile` 或经评估的 `proof` |
| `ANTI_ABUSE_FORM_TOKEN_TTL_MINUTES` | `30` | 15–30 |
| `ANTI_ABUSE_FORM_MINIMUM_AGE_MS` | `800` | 500–1500 |
| `ANTI_ABUSE_CHALLENGE_TTL_MINUTES` | `10` | 5–10 |
| `ANTI_ABUSE_EVENT_RETENTION_DAYS` | `180` | 180–365，按隐私策略确定 |
| `ANTI_ABUSE_FINGERPRINT_RETENTION_DAYS` | `30` | 14–30 |
| `ANTI_ABUSE_DNS_TIMEOUT_MS` | `750` | 500–1000 |
| `ANTI_ABUSE_DNS_CACHE_HOURS` | `6` | 6–24 |
| `TURNSTILE_SITE_KEY` / `TURNSTILE_SECRET_KEY` | 空 | 仅 provider=turnstile 时配置；密钥不得入库 |
| `REDIS_ENABLED` | `false` | 多副本必须 `true` |
| `APP_REPLICA_COUNT` | `1` | 与实际副本数一致 |

staging/production 使用开发占位密钥会导致配置校验失败。Turnstile 未配置密钥也会拒绝启动。

## 默认动作额度

| 动作 | 瞬时 | 每小时 | 每日 | 单对象 | pending |
| --- | ---: | ---: | ---: | ---: | ---: |
| `comment.create` | 4/10秒 | 60 | 240 | 12/10分 | 0 |
| `comment.reply` | 5/10秒 | 90 | 360 | 15/10分 | 0 |
| `message.send` | 8/30秒 | 180 | 600 | 30/10分 | 0 |
| `report.create` | 3/60秒 | 20 | 50 | 2/60分 | 0 |
| `upload.create` | 5/60秒 | 60 | 200 | 10/10分 | 0 |
| `review.submit` | 3/60秒 | 20 | 60 | 3/60分 | 20 |
| `community.submit` | 3/60秒 | 15 | 40 | 3/60分 | 15 |

新用户、高风险和受限用户额度乘 1/2；可信用户不再通过代码硬编码自动翻倍，其宽松额度由下面的变量权限控制。IP 预算为用户额度 4 倍，网段 8 倍，设备 2 倍，避免共享网络被单个账户轻易拖累。

### 权限变量额度

请求次数额度同时接入现有数值权限系统：

- `security.anti-abuse.rate_multiplier.<num>`：所有受保护动作的额度百分比。
- `security.anti-abuse.rate_multiplier.review_submit.<num>`：仅覆盖 `review.submit` 的额度百分比。

注册用户默认拥有 `security.anti-abuse.rate_multiplier.100`。等级权限组可以配置例如 `.150`、`.200` 或 `.300`；同一用户存在多个有效值时取较高值，动作专项值也与全局值取较高值。最终百分比限制在 25% 至 1000%，防止错误权限值关闭限流或产生无界额度。

百分比只缩放瞬时、小时、每日和对象级请求次数；不会扩大待审核队列上限，也不会绕过登录权限、`review.submit` 动作分类、重复内容检测、风险评分、验证码、封禁和人工审核。新用户和高风险账户的收紧系数仍作为独立安全信号生效。等级系统已通过权限角色轨道同步角色，因此把上述数值权限赋予更高等级对应的角色后会自动生效，无需在风控代码中硬编码等级数字。

## 后台规则

“反机器人与反滥用”页面可调整开关、紧急模式、五级阈值、相似度及每个固定动作的数字额度。后端拒绝未知动作、无序阈值和越界数字；不接受任意 SQL、表达式或规则代码。所有修改和恢复默认均进入管理员审计日志。

紧急模式只给非可信用户增加风险，不会无差别关闭网站。若配置错误，可使用具有 `security.anti-abuse.write` 的管理员执行“恢复默认”；若前端不可用，调用 `POST /api/v1/admin/anti-abuse/config/reset`。

机器人规则类型仅限 `allowed_bot`、`monitoring_bot`、`blocked_bot`、`ip_allow`、`ip_block`，且所有允许/监控规则强制只读。账户白名单通过用户风险状态的 `manuallyTrusted` 管理，黑名单通过正式限制记录管理。
