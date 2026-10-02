# NATS 配置凭据与保存

管理接口沿用 `admin.config.read` / `admin.config.write`。配置使用现有加密设置保存。
GET、PUT 响应及运行状态去除服务器 URL 中的 userinfo、查询和 fragment；密码、Token 仅显示是否已配置。
URL 可以是逗号分隔的多服务器列表。内部保留原有每服务器认证，界面提交相同脱敏 URL 时不会清空旧认证。

NATS 的 `token@host` 表示 Token；`username:password@host` 表示用户名/密码，包括空密码的冒号。
显式替换 Token 或密码会更新 URL 中相应的认证内容；清除 Token 删除 Token userinfo，清除密码保留用户名与冒号。
主动输入新的 URL 列表表示替换服务器列表，不会猜测旧服务器凭据应迁移到哪些新地址。
空白秘密输入沿用原值；替换和清除同一秘密不能同时提交。

候选配置先连接并建立既有订阅，成功后才保存并切换运行时。认证或持久化失败返回明确错误，仍保留上一套有效运行时。
错误响应不回显凭据；诊断中的 URL 及已知敏感值也会脱敏。没有新的权限节点或生产配置操作。

`TestOCT02NATSURLSecretsAreHiddenAndMaskedSaveAuthenticates` 与 `TestOCT02NATSURLCredentialReplacementAuthenticates`
使用回环地址的真实内嵌 NATS broker 验证密码/Token、脱敏往返保存、替换及清除失败后的运行时保留。
这些测试不证明生产 broker 配置、网络或凭据有效。
