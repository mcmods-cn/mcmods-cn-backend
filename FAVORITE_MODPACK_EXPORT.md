# 收藏夹导出 Modrinth 整合包

## 流程与事实来源

收藏夹详情通过通用 Minecraft 版本选择器选择一个明确版本，并只接受 `fabric`、`forge`、`neoforge`。后端预检收藏夹快照，逐项区分 `exported`、`auto_dependency`、`skipped`、`failed`；存在遗漏时必须显式确认“只导出兼容项目”。

创建任务时，任务、结构化条目快照和通用 Outbox 在同一 PostgreSQL 事务写入。PostgreSQL 是事实来源，NATS/JetStream 只是可靠唤醒；Worker 也定期扫描 `pending` 任务，因此消息丢失不会永久卡住任务。任务重试复用已保存的 `pack_version_id` 和条目，不重新选择随机文件。

Worker 使用数据库租约领取任务，单次租约 15 分钟，最多执行 3 次；进程在生成中崩溃后由扫描器重新领取过期租约，达到上限后以稳定 `WORKER_LEASE_EXHAUSTED` 结束。用户报告只显示安全错误信息，数据库保留可运维的阶段和错误分类。

## 文件选择与依赖

只使用站内已绑定的 Modrinth 项目 ID，并从 Modrinth 元数据中筛选完全匹配 Minecraft 版本和加载器的 JAR。顺序为主文件、Release、Beta、Alpha、发布时间、文件名。下载地址必须是 `https://cdn.modrinth.com`，且同时具备真实大小、SHA-1、SHA-512。必需依赖仅从站内明确的 `mod_relationships` 关系递归补充；可选依赖和名称猜测不会加入。

## `.mrpack` 结构

生成物是 ZIP，扩展名为 `.mrpack`，根目录仅包含 `modrinth.index.json`（当前没有合法 overrides）：

```text
collection-1.21.1-fabric.mrpack
└─ modrinth.index.json
```

索引固定 `formatVersion: 1`、`game: minecraft`；`versionId` 是任务创建时生成并持久化的 UTC 包版本（格式 `yyyy.MM.dd-HHmmss`，如 `2026.08.18-093015`），不是 Minecraft 版本。同一任务的 Worker 重试始终读取该持久值，不会改变版本号。加载器依赖键分别是 `fabric-loader`、`forge`、`neoforge`。每个 `mods/*.jar` 条目包含 SHA-1、SHA-512、安全 CDN URL、真实字节数及 `required|optional|unsupported` 环境信息；路径穿越、重复路径、非 JAR、空哈希、非 HTTPS 或非官方 CDN 均失败关闭。

## 报告、下载与清理

任务详情和历史始终保留汇总与逐项报告，包括站内/Modrinth 项目和版本 ID、实际文件、发布类型、哈希、依赖来源及稳定原因码。完成页自动下载但不关闭报告，并提供重新下载、失败名称复制、折叠明细和使用相同设置重新导出。下载端点校验任务所有者并签发短期 OSS 访问；7 天后任务标记 `expired`，生成文件通过既有 OSS 删除 Outbox 清理，结构化报告继续保留。

系统通知使用本地化模板，包含 Minecraft、加载器具体版本、成功数、依赖数和未导出数。

真实外部集成测试使用 Modrinth 项目 `P7dR8mSH` 的 1.21.1/Fabric 元数据生成 1 文件包，重新打开 ZIP 并解析根目录索引；测试未下载或再分发第三方 JAR。

## 限制

每用户最多两个并行任务；收藏夹必须属于当前用户或明确公开。当前没有把第三方 JAR 复制进包，避免许可证和再分发问题。外部 API、OSS 或 Worker 错误归入 `failed`，正常不兼容归入 `skipped`。

默认并发数、用户并行任务、每日任务数、产物有效期、租约和最大尝试次数分别由 `FAVORITE_EXPORT_MAX_CONCURRENT`、`FAVORITE_EXPORT_MAX_ACTIVE_PER_USER`、`FAVORITE_EXPORT_MAX_DAILY_PER_USER`、`FAVORITE_EXPORT_ARTIFACT_TTL_HOURS`、`FAVORITE_EXPORT_LEASE_MINUTES`、`FAVORITE_EXPORT_MAX_BUILD_ATTEMPTS` 配置；相同收藏夹/版本/加载器 30 秒内不能重复创建任务，`.env.example` 提供了开发默认值。
