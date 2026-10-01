# 前端代理追加后端调用链审查

根与API-A明确独占委派 `internal/httpapi/simple_project_handlers.go`，基线bd50afd0ef92192c40c5152ea56e27a3fdbb657f，原1092行已分1–270、271–545、546–820、821–1092完整读取/语义审查；最终新增/修改段与调用链复查。全文身份与最终行数见review-backend-ui-a-delegated.json。原API-A台账引用本结果，不重复计数；另5额外文件（4个DB移交及catalog_editor_handlers由API-B移交）另计。

| ID/级别 | 状态 | 确认证据/根因 | 修复与实际验证 |
|---|---|---|---|
| BE-UIA-SP001 P1（关联BE-SUP-009） | 已修复；已验证通过（限本行断言） | 公开addon parent JOIN不约束review，public_routes存在性被当发布性；filter可探测pending父；写入也只查route | mod/modpack/simple父只approved或实际父owner；未解析raw保留，foreign pending绑定拒绝；登录private/no-store；真实PG guest/other/owner、3类filter、6绑定断言和缓存 |
| BE-UIA-SP002 P1（关联BE-SUP-009） | 已修复；已验证通过（限本行断言） | 前4关联批循环结束未查Rows.Err，DB流失败返回nil并发布部分数据 | 每批Err检查/关闭/返回；4真实PG shadow-view volatile函数raise异常，旧全部lost nil→新错误返回 |
| BE-UIA-SP003 P1 | 已修复；已验证通过（限本行断言） | 库存图片已绑定后仍可能pending/rejected；仅active读取，redirect辅助只查MIME | 列表+直达同时要求clean/trusted_generated和raster；PG4scan状态及SVG列表/直达断言 |
| BE-UIA-SP004 P2 | 已修复；已验证通过（限本行断言） | 读失败一概404、历史revision lookup吞错、icon默认空值掩盖DB错误 | 明确ErrNoRows404、DB异常脱敏500；4HTTP stream-error场景旧404→新500；其他入口静态/编译复查 |
| BE-UIA-SP005 P2 | 已修复；已验证通过（限本行断言） | slug可用性DB错误被视为冲突，连续1000候选查询，丢真实根因 | typed collision sentinel，仅冲突继续、真实错误即止；2单测真正红绿，首查询/碰撞后故障均限正确次数 |

真实隔离PG：复用isolatedAITestDatabase，要求APP_ENV=test和loopback；创建随机库并写所有权marker，清理再次核对marker后DROP，迁移两次并核repair ledger，fixture为合成资料。没有生产DB连接/数据处理、没有AI请求。第一fixture错误用了oss_files不支持的infected枚举，改为真实pending/rejected/clean/trusted_generated后重新建立有效基线，不把fixture失败当业务红绿。

有效基线14子场景11FAIL/3PASS→14PASS；追加实际handler异常分类4FAIL/12PASS→16PASS；新增SVG后17PG，最后加入真实stale-crawler创建409且无写入为18PG子场景，另2slug单测。最终命令、退出、race/vet和log摘要见backend-ui-a-validation.json，不伪称整仓测试由本域执行。

结构核对：parent多态FK只保目标存在，不保审批；图库FK只保文件记录存在，不保当前扫描信任。动态规则在实际读写SQL落实；无索引或schema变更、无迁移，不改已部署migration。样本只有1addon及合成父/文件，不能外推生产查询性能；未获取实际连接压力、备份恢复、膨胀、维护和CDN数据。

用例/角色：guest与other仅已发布父；父owner可看自己的pending；编辑者不能借editor标志读取他人pending；非法图库直达拒绝，真实数据库中断有失败反馈。正式契约/部署兼容说明见../../SIMPLE_PROJECT_VISIBILITY.md。

共享BE-DB022租约治理由DB代理负责，本域只在createSimpleProject网络准备后/首个资源INSERT前加入lockSeedCrawlerLeaseTx；真实旧run-token请求409且没有创建项目PASS。最终与导出13、目录28合并race为59PG子场景+2slug单测，13.729s退出0，/tmp/mcmods-ui-a-backend-catalog-final.log；所有旧失败证据保留。
