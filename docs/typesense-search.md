# Typesense 搜索投影

Typesense 是可选的派生搜索服务，PostgreSQL 始终是业务数据的唯一权威来源。业务事务只向
`search_index_queue` 写入待同步对象，不会调用 Typesense；Typesense 关闭、启动中或不可用时，
HTTP 列表接口会使用自身定义的 PostgreSQL 有界回退协议。

## 配置

```dotenv
TYPESENSE_ENABLED=true
TYPESENSE_URL=http://127.0.0.1:8108
TYPESENSE_API_KEY=replace-with-a-server-side-key
TYPESENSE_COLLECTION_PREFIX=mcmods
TYPESENSE_TIMEOUT_SECONDS=3
```

API Key 仅保存在后端环境中，不应传给浏览器。该 Key 需要健康检查、集合、别名、文档导入、
删除和搜索权限，因为后端负责建立及维护投影。

## 同步与恢复

- 首次启用或索引结构版本变化时，后端建立带版本号的新集合，批量导入数据后原子切换别名。
- 成功建立的集合会记录在 `search_index_state`；普通重启直接处理积压队列，不重复全量重建。
- 写入队列按文档合并，同一对象的连续修改只保留最新操作；失败任务采用退避重试。
- `/ready` 的 `search` 字段可能为 `disabled`、`initializing` 或 `ready`。
- 数据库被重置后状态记录也会消失，下一次启动将自动全量重建，避免保留旧环境文档。

当前投影版本为 **5**。它修复了可选名称/摘要为空时，后续多语言名称和正文被提前丢弃的问题，
并移除未批准导入来源的名称以及服务器关联的未批准模组元数据。
只有公开 canonical 内容或已批准的有效来源能使资源进入公开索引；API 和索引共用同一可见性 predicate。
服务器保留实际观测的原始模组 ID，关联名称与 identifiers 只来自 approved 模组。
完成一个持久模组队列任务时，在同一事务中先登记关联资源、服务器的刷新意图再确认父任务；
下游登记失败保留父任务。来源撤回审核后，子投影会被刷新或删除，而非无限保留先前公开的名称。
从版本 3/4 升级时会建立新集合并切换别名；这是派生索引重建，不修改 PostgreSQL 业务记录。
重建完成前继续使用现有 SQL 回退规则，需预留新旧集合并存的存储空间。
导入接口必须为每个文档返回且仅返回一条成功确认；空、缺失、多余或失败确认均使该批失败，
由现有任务退避机制恢复，不会把部分写入当成同步完成。

滚动升级时旧 worker 检测到更高投影版本后停止写入。版本 3 可以读取兼容的新别名，
但直接回滚二进制不会使其恢复同步；需要修复版本 5 或发布更高版本的兼容重建。
不要降低数据库中的投影版本来绕过这项保护。

隔离环境使用真实 Typesense 29.0 验证过 schema、文档导入、多语言名称/正文检索和删除。
这仅验证合成样本的搜索契约，不代表生产吞吐、相关性质量或线上索引健康。
可选测试 `TestRealTypesenseImportsAndFindsLocalizedTextIntegration` 只接受明确的回环 HTTP 地址，
需设置 `MCMODS_RUN_TYPESENSE_INTEGRATION=1`、`MCMODS_TEST_TYPESENSE_URL` 和专用随机
`MCMODS_TEST_TYPESENSE_API_KEY`；未提供时明确跳过，不会使用运行配置中的搜索凭据。

当前投影覆盖模组、整合包、其他大型资源、作者/团队、社区文章、Minecraft 服务器和资料资源。筛选、审核可见性
仍会在搜索层和最终 PostgreSQL 读取层同时校验，Typesense 只返回内部数字 ID 排序结果。

## 服务器目录游标

- 服务器投影包含组合筛选字段以及时间、热度、下载、收藏、评分、浏览、评论等稳定数值排序键；最终卡片只按命中内部ID从PostgreSQL读取并再次要求approved。
- 新目录序列在Typesense ready时使用索引，否则从首屏开始使用PostgreSQL keyset；名称排序固定使用SQL。游标签入筛选scope、排序tuple和`index`/`sql` authority，续页期间不会静默切换来源。
- 已签发index cursor后Typesense失效会返回503；调用方可从首屏重新开始。SQL cursor即使Typesense恢复也继续SQL。该约束避免不同排序快照在序列中混用而产生重复或遗漏。
- 目录不提供page、offset或精确total；每页以limit+1产生`hasMore/nextCursor`。`sort=relevance`不支持稳定续页并返回400，不回退到伪确定排序。
