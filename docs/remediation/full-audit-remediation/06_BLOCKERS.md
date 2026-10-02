# 外部阻塞与环境门禁

2026-10-01最终：`BLOCKED_EXTERNAL=0`。下列五项审计基线环境限制均已通过本机真实门消除；19:13:05全量显式DB1604名称92批及最终queue双次Race60 PASS全部0skip/失败0，随机库清理0，独立19:13:57复查残留0。完整最终结论见07，部署环境确认不冒充已执行门。

| 门禁 | 当前实际证据 | 验证边界 |
| --- | --- | --- |
| 空库安装 | 本机PostgreSQL18.6，明确owned随机空库完整generation168；当前最终initialize exit0/7.639s，287表/1130索引/0invalid | 审计基线100已由当前168完整合同替代；共享public155及未知远端不重置，最终随机库清理0且独立复查残留0 |
| Go Race | 可复现GCC、CGO1和Go1.26.6，当前全仓Race exit0/161.175s | 最终全部queue真实PG/JetStream双次Race47.955s、30名称60 PASS/0skip，不以普通全仓Race代替专项 |
| govulncheck | 固定v1.7.0实际执行，exit0/5.372s，可达/已导入漏洞0 | 另有4条未调用模块公告；不称所有模块公告0 |
| 前端依赖扫描 | Node24.19.0/npm12.0.2、权威package-lock，当前完整npm audit exit0，584依赖0漏洞 | 扫描含dev/optional，不只是prod；远程GitHub job尚未运行 |
| 前端自动化 | 当前283单测及12实际Chromium React流程全部PASS/0skip，Type/Lint/58页Build全0 | 浏览器使用明确API测试服务；后端真实HTTP/PG由独立集成测试证明，不冒充同进程端到端生产 |

用户已明确同意原统计与复核评级分别保留：原High176/Medium214/Low59及277个原逐项等级不改，172个缺失逐项等级按独立风险复核，总复核148/236/65。缺原逐项等级不再是等待用户补充的阻塞；默认严格449项工具实际通过，不削弱状态、证据或安全门。

## 以下为原审计环境门历史，不是当前未通过列表

原审计时没有把任何有效Finding宣告为`BLOCKED_EXTERNAL`。当时下列是尚未通过、必须消除的环境门禁，并非跳过理由：

| 门禁 | 审计基线事实 | 关闭要求 |
| --- | --- | --- |
| generation 100 空库安装 | 维护数据库被 `pg_hba` 拒绝；可连接业务库的账号没有 `CREATEDB`；远程现存库为 generation 85 | 在隔离、可销毁且授权的 PostgreSQL 数据库真实执行完整 Migrate/seed/约束安装并验证 generation 100，不重置未知远程业务库 |
| Go Race | Windows 缺少 GCC | 在 Linux CI/容器或安装可复现工具链后实际运行 `go test -race ./...` |
| govulncheck | 未安装 | 安装并执行，记录真实结果 |
| 前端依赖扫描 | npm CLI/权威锁文件工具链未完成 | 统一 npm 与 `package-lock.json`，实际执行生产依赖审计 |
| 前端自动化 | 0 测试、无 test script | 建立单元/组件/关键 E2E 并进入 CI |

JetStream 环境门禁已于 2026-08-21 消除：默认测试直接启动官方 `nats-server/v2` 的 file-backed JetStream，并实际通过重投、去重、durable backlog、Server/consumer 重启、Outbox 断连恢复和死信 replay。该证据不冒充生产多节点/ACL/容量演练。

任何新外部阻塞必须包含：受影响 Finding、已尝试方案、重复出现次数、所需外部变化和临时风险控制。
