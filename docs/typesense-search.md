# Typesense 搜索投影

Typesense 是可选的派生搜索服务，PostgreSQL 始终是业务数据的唯一权威来源。业务事务只向
`search_index_queue` 写入待同步对象，不会调用 Typesense；Typesense 关闭、启动中或不可用时，
HTTP 列表接口会自动回退到原有 PostgreSQL 查询。

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

当前投影覆盖模组、整合包、其他大型资源、作者/团队、社区文章、Minecraft 服务器和资料资源。筛选、审核可见性
仍会在搜索层和最终 PostgreSQL 读取层同时校验，Typesense 只返回内部数字 ID 排序结果。
