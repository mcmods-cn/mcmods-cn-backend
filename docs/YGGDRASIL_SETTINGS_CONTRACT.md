# Yggdrasil 管理配置契约

`GET /api/v1/admin/config/yggdrasil` 要求 `admin.config.read`，`PUT` 要求 `admin.config.write`。响应均使用项目的 `{ "data": ... }` 包络。普通账户和未登录访问者不因前端存在设置组件而获得权限。

PUT 的完整可写字段为 `enabled`、`publicBaseUrl`、`textureBaseUrl`、`serverName`、`trustedProxyCidrs`、`tokenTtlHours`、`maxTokens`、`joinTtlSeconds`、`textureMaxBytes`、`privateKeyBase64`、`rotatePrivateKey`。前端应明确挑选这些字段，不应把 GET 响应整体回传。

GET 另有 `hasPrivateKey`、`persistentPrivateKey`、`available`、`disabledReason` 四个只读运行状态字段，不返回私钥。PUT 严格拒绝未知字段，非法 JSON、携带这些只读字段或配置加密封装失败返回 `400`；配置语义验证失败返回 `422`；签名密钥生成/保留或数据库写入失败返回 `500`。不能通过忽略未知字段掩盖调用方契约错误。

`privateKeyBase64` 为空且 `rotatePrivateKey` 为 false 时，服务端保留现有签名密钥；明确选择轮换才生成新的密钥。轮换可能影响已有客户端，界面继续提供原有警告。本次审计未连接真实 Yggdrasil 客户端或修改运行服务的密钥。

校验后的配置先加密写入 `system_settings`，写入成功后替换进程内服务。审计事件只记录启用与轮换状态，不记录私钥。响应继续返回脱敏配置。

前端白名单修复没有 API、schema 或权限变更，可独立发布；后端无需迁移。组件回归使用合成 GET 状态验证 PUT 白名单；`TestYggdrasilSettingsWriteContractExcludesRuntimeFields` 使用实际 `decodeJSON` 和请求 DTO 验证可写字段接受、四类只读字段拒绝。这些局部测试不等于配置持久化或真实客户端联通验收。
