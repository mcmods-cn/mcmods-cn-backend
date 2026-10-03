# 系统通知本地化

## 根因与权威来源

旧的异步通知事件携带已经写死的英文 `title/body`，消费者直接保存它们，后台 `notifications.templates` 配置没有进入发送链路。现在系统事件只携带稳定 `templateKey`、白名单 `templateValues` 和业务数据，`notification_worker` 在创建通知时读取模板并按接收者语言渲染。管理员手工发布的全站公告仍是管理员明确输入的内容，不冒充某个业务模板。

当前稳定模板包括导入、蓝图、审核、认领、回答采纳、`project_updated` 以及 `modpack_export_completed`、`modpack_export_completed_with_skips`、`modpack_export_failed`、`modpack_export_expired`。配置保存在既有 `system_settings.notifications.templates`，没有建立第二套通知系统。

## 语言与快照

- 语言代码先经过站内 `normalizeContentLocale`，如 `zh_cn` 统一为 `zh-CN`。
- 选择顺序为用户 `preferred_ui_language`、站点中文默认、英文最终回退。
- 所有站内启用语言都有默认模板；后台保存时校验模板 Key、重复项、八种语言和未声明变量。
- 通知创建时保存渲染后的标题、正文、实际语言、模板 Key、模板版本和参数快照。修改后台模板不会改写历史通知。
- 模板版本完全由后端维护：只有变量声明或翻译正文真实变化时递增，客户端提交的版本号不会覆盖权威版本。
- 保存前先校验原始模板列表；去除 Key 首尾空白后重复的项返回 400，不先合并成 map 而丢失重复证据。
- 模板读取、合并、版本递增和保存使用同一事务及配置级 advisory lock；并发保存读取上一笔已提交版本，避免两个不同正文共用同一源版本。
- 模板仅替换声明过的 `{variable}`；缺少参数时拒绝生成残缺通知并保留任务错误。
- 替换只扫描原始模板一次。项目名称、用户名称和理由中的 `{variable}` 是普通文本，不会再次展开或被当作缺失变量。
- 关注与评论插眼聚合改变通知源文时，同事务清除该通知的旧 AI 译文缓存；活跃任务复用还要求完整源文 payload 一致。
  已在执行的旧任务仍由写入时源版本校验拒绝迟到结果，不能覆盖新源文。

## AI 翻译边界

通知列表明确返回 `translationAllowed`。`kind=system` 始终为 `false`，前端不渲染翻译按钮；直接请求翻译接口返回 `403 SYSTEM_NOTIFICATION_TRANSLATION_DISABLED`。普通用户内容通知仍沿用原有权限和额度规则。

审核配置仅在对应配置行不存在时沿用既定默认值。数据库读取或 JSON 格式错误时，不能把
未知设置解释为管理员关闭审核：AI 译文、蓝图创建和资料板块创建按需要审核处理，且不
部分应用错误 JSON 中的字段。后台读取返回 503，响应不包含内部数据库或解析错误。
此项与通知模板保存均不需要 schema 迁移，不自动修改生产配置。

## 验证

`TestNotificationTemplateUsesConfiguredChineseTranslation` 验证后台中文模板覆盖默认英文并正确处理语言别名；`TestNotificationTemplateRejectsMissingVariables` 验证缺失变量失败关闭；默认模板测试逐项验证每个启用语言、每个模板 Key 均有具体标题和正文。`TestOCT02NotificationTemplateValuesRemainLiteral` 验证花括号输入不重新解析，通知聚合回归使用隔离 PostgreSQL 验证源文变化后缓存失效和任务去重。历史报告不能代替当前版本的全量测试结果；本次执行记录见 `docs/audit/20261002/`。
