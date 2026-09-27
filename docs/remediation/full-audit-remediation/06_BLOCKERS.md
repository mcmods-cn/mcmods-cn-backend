# 外部阻塞与环境门禁

目前没有把任何有效 Finding 宣告为 `BLOCKED_EXTERNAL`。下列是审计基线中尚未通过、必须在 Goal 完成前消除的环境门禁，并非跳过理由：

| 门禁 | 审计基线事实 | 关闭要求 |
| --- | --- | --- |
| generation 100 空库安装 | 维护数据库被 `pg_hba` 拒绝；可连接业务库的账号没有 `CREATEDB`；远程现存库为 generation 85 | 在隔离、可销毁且授权的 PostgreSQL 数据库真实执行完整 Migrate/seed/约束安装并验证 generation 100，不重置未知远程业务库 |
| Go Race | Windows 缺少 GCC | 在 Linux CI/容器或安装可复现工具链后实际运行 `go test -race ./...` |
| govulncheck | 未安装 | 安装并执行，记录真实结果 |
| 前端依赖扫描 | npm CLI/权威锁文件工具链未完成 | 统一 npm 与 `package-lock.json`，实际执行生产依赖审计 |
| 前端自动化 | 0 测试、无 test script | 建立单元/组件/关键 E2E 并进入 CI |

JetStream 环境门禁已于 2026-08-21 消除：默认测试直接启动官方 `nats-server/v2` 的 file-backed JetStream，并实际通过重投、去重、durable backlog、Server/consumer 重启、Outbox 断连恢复和死信 replay。该证据不冒充生产多节点/ACL/容量演练。

任何新外部阻塞必须包含：受影响 Finding、已尝试方案、重复出现次数、所需外部变化和临时风险控制。
