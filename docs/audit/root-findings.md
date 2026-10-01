# 主执行者交叉复核问题与交付边界

基线为 `bd50afd0ef92192c40c5152ea56e27a3fdbb657f`，前端对应 `4f82c9075636cc4f1f6d3eb07de37a3fffd1de58`。逐文件的实际读取行段、最终 SHA256、职责、调用链和有限行为证据见 `file-ledger.json`。本表是主执行者发现/亲自复现的问题；其它专项保持自己的稳定 ID，别名不再计作新问题。最终版本、整体回归及 PR 见 `delivery.md`。

## 已确认问题

| ID / 严重度 | 根因、影响与实现 | 实际验证及边界 |
| --- | --- | --- |
| ROOT-001 / P1 | Outbox 失败/完成写入缺领取者及 attempt 条件，旧 dispatcher 可覆盖新领取；状态落库错误被忽略。`queue/outbox.go` 对全部终态使用 owner/status/attempt fence、传播错误并按受影响行数计指标 | 真实 PG 旧 owner 写回原版失败、新版保持新 owner；原有真实 JetStream/消费者幂等集成。供应商不支持幂等时，不保证只收费一次 |
| ROOT-002 / P1 | 本地队列降级绕过明确禁用与任务超时，管理状态泄露连接串认证。`queue/nats.go` 使用正常任务配置及有界 context，移除 URL 认证/路径/query/fragment，错误返回摘要 | 真实 handler 单元断言禁用不执行、状态无合成凭据；不把地址脱敏当作服务最小权限已验收 |
| ROOT-003 / P1 | 搜索引擎验证缓存只按 IP，Google 判定被 Bing 声称复用。`antiabuse/crawler.go` 将实际反向域规则加入缓存身份 | 两个确定性 DNS 回归旧失败、新通过；不对真实搜索引擎作攻击探测 |
| ROOT-004 / P1 | Typesense HTTP200 的空/少/多导入结果被当成功。`searchindex/typesense.go` 校验结果数、结构及每项成功 | 三类本地 HTTP 红绿；实际30.2引擎拒绝错误字段、正常批量导入通过 |
| ROOT-005 / P1 | 活动事实/出队先提交，奖励/任务投影后失败被吞，无法恢复。`activity/postgres_store.go`、`progression/service.go` 将事实、统计、进度、奖励和出队置于同 TX，稳定锁顺序；缓存提示留在提交后 | 真实 PG 故障回滚保留队列；恢复与两个 monitor 并发四事实仅一条 XP7/余额11奖励账本。未证明任意业务事件端到端严格恰一次 |
| ROOT-006 / P2 | 首次 Close 的取消阻止关闭消息送达，closeOnce 已消耗，goroutine 永久运行。`activity/monitor.go` 有界独立关闭信号与统一完成信号 | 阻塞 writer 真实 goroutine 回归及 race；后续 Close 等待实际停止 |
| ROOT-007 / P1 | 保存英文草稿改变母页面状态，隐藏已发布中文；无效写 locale 回退覆盖中文。`site_affairs_handlers.go` 按译文发布状态派生母状态、严格验证管理写语言 | 真实 PG 发布zh→保存en草稿→zh仍200，错误locale400不覆盖，全部草稿404；正式说明 `../site-affairs-localization.md` |
| ROOT-008 / P1 | 原依赖包含可达标准库/图片公告；FE global brace override 破坏 lint。Go1.26.8、compress1.18.7、image0.45/crypto0.56，Next16.3.8/sharp0.35.4等最小必要补丁，修正 nested minimatch3 的 brace1 override | 最终漏洞/安装/构建结果见 validation；工具公告不等于已利用漏洞。保留一个未使用模块公告边界 |
| ROOT-009 / P1 | opt-in 不证明测试目标可丢弃。`testenv`、七包 TestMain、`cmd/test-setup` 和 `scripts/test-services.sh` 验证任务目录/UID/随机凭据/回环目标、禁止reset；启动/停止核对确切本任务进程 | 目标拒绝单元、真实四服务启动/初始化；旧停止因 Redis 重写argv真实失败，修后 AUTH+INFO PID/config_file 证明身份才停止。后续独立复核又检查NATS-only及URL边界，最终状态见验证 |
| ROOT-010 / P1 | 通用 query helper 与治理列表吞 Query/Scan/RowsErr，故障返回空/部分200，误导管理及账务。`admin_handlers.go` tuple返回并完整检查，26处调用传播；治理三列表拒绝流错误 | 13入口真实 closed pool 红绿；PG先发一行再除零拒绝部分结果；三治理 shadow view真实异常旧200→新500。类型不变、正常空结果保持[] |
| ROOT-011 / P1 | Typesense 跨源重定向转发服务 key。客户端只跟随同协议/host/port且无userinfo的重定向 | 实际两个本地HTTP端点原版转发合成key、新版阻止；同源重定向保留。正式 `../typesense-search.md` |
| ROOT-012 / P2 | 日志清理忽略不可变审计删除错误，前后端假成功。`log_handlers.go` 继续可清类别并返回真实部分计数，已保存配置遇清理失败503 `LOG_CLEANUP_FAILED`/details.saved | 真实PG保留不可变审计、可删system删除1、策略30已保存；FE真实notice和八语言provider组件验证。物理审计保留政策需决策，不关闭触发器 |
| ROOT-013 / P1 | 收藏整合包导出缺默认队列 task，持久化后不能正常调度。`queue/nats.go` 增正常subject/concurrency/timeout默认配置、保留显式禁用 | 旧默认不存在红→新默认可分派、明确禁用仍禁用；对应worker目标回归见 API-B |
| ROOT-014 / P2 | 错误按1000字节截断可能留下半个UTF8字符，PG22021连重试/死信都写不下。outbox/deadletter使用UTF8安全有界文本 | 真实PG中文长错误旧22021→新retry/dead两场景通过、queue整包race通过 |
| ROOT-015 / P2 | serverprobe favicon只检查PNG签名，8字节/巨幅声明被接受。`serverprobe/probe.go` 先DecodeConfig校验头与每边512上限 | 合成正常64PNG、8字节、合法CRC超大IHDR回归；未执行旧版大像素分配；不是完整像素质量鉴定 |
| ROOT-016 / P2 | 语言选择器在lg以下全部隐藏，手机不能完成语言切换。FE `site-shell.tsx` 显示同一语言控件并保证44px触摸高度/有界宽度 | 真实390px production浏览器zh→en、查询参数/刷新lang保留及页面无横溢；不代表所有移动页面均验收 |
| AUDIT-TOOL-001 / P2 | 台账将字符串false当真，可伪造读审/行为状态。两仓库 `scripts/audit-ledger.py` 要求字面布尔true | 独立真实tempGit黑盒，旧版失败、新版9测试通过；不以生成器代替人工审查 |
| AUDIT-TOOL-002 / P2 | 台账按review/validation文件名批量排除，误把人工JSON排除。显式已核实机器产物注册、清除旧名字排除，未知同名文件仍人工范围 | 独立黑盒人工同名JSON、陈旧SHA/未读行段/删除分母均检验；各人工issue/override JSON仍纳入 |
| AUDIT-TOOL-003 / P2 | 浏览器所有context共享回环IP，连续用例累计触发现有headless 60次/分钟读取额度，既有suite合法收到429。FE `scripts/run-e2e.mjs` 以互补grep分两批完整执行所有测试，批间遵守60秒窗口；两份JSON放在独立playwright-report目录，避免第二批清理test-results丢失第一份报告；不放宽安全策略、测试超时或断言 | 原连续6项5 PASS/1 FAIL保留；前一 `npm run test:e2e` exit0：互补两批5 PASS/12.9s与1 PASS/5.5s；最终业务构建全部六项及两份报告保留另见validation-root，不把测试编排缺陷称生产限流漏洞 |

FE根layout严格nonce动态渲染归 `FE-CORE-013`；theme与presence的存储拒绝保护归 `FE-CORE-007`，不重计。治理通用Rows.Err同时发现的 `BE-SUP-006` 按子问题映射 ROOT-010/BE-UIB-001；NATS脱敏以 BE-APIB-005 计共享跨层修复，本地禁用/超时仍为独立 ROOT-002。AI、权限、数据库、导入、媒体/下载、所有前端其它修复详见独立专项。

## 历史清单核实

实际GitHub issue列表（两个仓库，state=all）均为空；每库有3个已合并历史PR。REST issues集合中的PR不当作业务issue。项目历史实施/测试/审计文档已读取，但没有取得可追溯的“约20项未完成”权威清单，因此历史清单总数/已解决/受阻无法准确统计；不把不存在的清单当0项遗留问题。独立发现的上述及各专项问题按当前实现/可复现行为处理。

## 需决策与未验证范围

- 生产数据库的膨胀、容量、锁/复制、备份恢复、历史脏数据兼容及大表迁移锁影响：未取实际运行证据；测试库通过不代替生产健康。
- 旧 generation 数据库无损升级、物理删号与不可变审计匿名化/保留、全业务bigint超过JS安全整数的公共协议：需明确兼容/保留决策，不自动清洗真实数据或改变商业规则。
- AI真实供应商连通、全部语种语义质量、真实账单：没有付费/数据外发范围授权及费用上限，NOT_RUN；本地HTTP确定性失败测试仅验证流程。未知usage保守保留预留，不等于免费。
- 真实OSS/SMTP/OAuth/Ygg客户端和未提供Exporter样本全格式产物：各专项准确记录fixture与服务边界。模拟HEAD/HTTP、组件API mock均没有作为真实外部服务验收。
- 已确认可修且风险可控的缺陷持续实现；部分文件的行为只静态/类型验证，详见专项，不承诺零Bug。最终测试、未运行场景与失败原始摘要统一在交付记录保留。
