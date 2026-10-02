# 2026-10-02 新增问题与实施结果

这是本轮实际新增问题合并账本，不是历史449项的复验结论。历史问题见同目录 `LEGACY_REVIEW.md`；原台账不改写。分支与前后端基线见 `ROOT_PROGRESS.md`。完整当前位置、指纹、原始来源映射及剩余边界在 [findings.json](findings.json)。

本次登记 234 个稳定 ID；按明确别名去重后确认 224 项。确认数含已修但仅部分验证、正式文档和辅助测试缺陷，不等于全部业务验收通过。状态分布：alias 4、fixed_documentation 1、fixed_partial_verification 48、fixed_static_regression_pass 5、fixed_verified 170、ruled_out 1、scope_only 2、unconfirmed_candidate 3。

## 口径与验证边界

- 只有稳定已确认根因计入确认数。B022、FP048、FP067为未证实候选；FS025已证伪；FP026/FP033是归类集合。
- A012→DB008、B019→R019、FP028→FS020、FP062→R011为明确别名。C015/C016辅助修复不再计根代理；独立handler的连接生命周期问题保留各自作用点。
- NATS配置和诊断脱敏共同归R008；B阶段记录误标R009已纠正。R009仅指持久死信恢复。
- `fixed_verified`表示所列局部断言已实际执行；source-contract、确定性mock、TEMP PG、完整schema PG和浏览器fixture边界分别保留。未覆盖真实浏览器/外部供应商的项目仍说明局限。
- 默认测试的DB SKIP不是集成通过。最终实际PG18 r2：1793发现/调用、76批0FAIL、1786topPASS/7外部样本/服务SKIP，退出0；1194当前Go指纹与对应snapshot一致。Typesense29.0真实回环服务加searchindex整包racePASS6.822s。首轮PG1749/74批8FAIL、环境错误及业务RED均保留。
- R6 unit344PASS0SKIP/build0和生产产物字节保持，最终R7 production Chromium73PASS/0FAIL/0SKIP180.621s；全量lint/type退出0，647源码指纹无漂移。FP012/051/068三个专用fixture旅程已通过；真实DB/供应商边界分别保留。r5的69PASS覆盖FP050/FP087，但不覆盖最后global保护，产物界限保留。
- 无生产数据库、真实供应商/付费AI或线上批量重译。合成数据计划/预算通过不证明生产健康、供应商语义质量或费用准确。

## 已确认项目

### 根执行与基础设施

| ID / 级别 | 结果 | 根因、修复与验证 | 位置 |
| --- | --- | --- | --- |
| OCT02-R-001 P1 | fixed_verified | Google/Bing DNS信任结果共用仅IP缓存，改为IP与供应商后缀组合键；确定性DNS回归实际RED/GREEN。 | internal/antiabuse/crawler.go, internal/antiabuse/crawler_scope_test.go, internal/antiabuse/crawler_test.go |
| OCT02-R-002 P1 | fixed_verified | 不匹配IP/CIDR的策略继续由伪造UA命中；要求实际规则匹配；六项边界回归通过。 | internal/antiabuse/crawler.go, internal/antiabuse/crawler_scope_test.go, internal/antiabuse/crawler_test.go |
| OCT02-R-003 P2 | fixed_verified | 测试配置可能读取祖先.env；增加显式跳过选项及带身份校验的专用环境工具；配置测试、Python三项及真实空库初始化通过；无需真实凭据。 | internal/config/config.go, tools/testing/isolated_environment.py |
| OCT02-R-004 P1 | fixed_verified | 耗尽超时的任务context使死信DB写入立即失败；独立有界恢复context；实际超时任务RED/GREEN。 | internal/queue/nats.go, internal/queue/dead_letter_timeout_test.go |
| OCT02-R-005 P1 | fixed_verified | Typesense空、少量或额外确认行仍被接受；严格核对逐条确认总数及结果；六项HTTP确定性RED/GREEN。 | internal/searchindex/typesense.go |
| OCT02-R-006 P2 | fixed_verified | 数组首项空字符串终止投影，丢失后续可用搜索文本；跳过空值并保留预算；跳过空字符串并保留后续可用文本与预算；三项定向回归通过，Typesense29.0真实回环服务上的searchindex整包race通过6.822s。29.0是服务版本，不是测试数量。 | internal/searchindex/worker.go |
| OCT02-R-007 P2 | fixed_verified | 已取消的Close调用没有发出停止信号；停止与调用者等待context分离；生命周期RED/GREEN；真实PG整包race通过。 | internal/activity/monitor.go, internal/activity/monitor_shutdown_test.go |
| OCT02-R-008 P1 | fixed_verified | NATS错误中的URL或token进入状态返回；集中脱敏诊断；合成凭据五项RED/GREEN；整包race通过；B另修admin NATS配置响应userinfo脱敏/省略密钥保留/显式clear，真实嵌入broker8cases通过。 | internal/httpapi/nats_handlers.go, internal/queue/nats.go |
| OCT02-R-009 P1 | fixed_verified | 数据库不可用时耗尽任务缺持久死信；原事件ID还被broker去重；独立broker身份、持久恢复消费者和幂等DB补写；实际重启/双worker/毒旧消息/人工重放保护race通过。 | internal/queue/dead_letter_recovery.go, internal/queue/nats.go |
| OCT02-R-010 P1 | fixed_verified | 未公开导入名称及关联mod元数据进入公共搜索；父mod确认不保证子投影刷新；共用公开来源谓词、条件过滤、同事务持久化子刷新；真实PG RED/GREEN及Typesense整包race通过，投影升级v5。 | internal/searchindex/worker.go |
| OCT02-R-011 P2 | fixed_verified | 日志保存清空新输入，列表刷新错误混入保存结果，历史跨账号残留及硬编码界面；保留新输入、独立列表错误/重试、actor隔离、八语言词条；保存/重试/中文移动切换和FS-033新产物跨账号隔离均实际浏览器通过。 | FE:app/_components/log-share-tool.tsx |
| OCT02-R-012 P3 | fixed_static_regression_pass | 纹理批处理source测试先以负索引切片才检查缺少符号；先检查start；静态确认的测试守卫修复；现有目标测试通过，不宣称业务RED。 | internal/httpapi/player_profile_texture_batch_source_test.go |
| OCT02-R-013 P2 | fixed_verified | queue关闭后任务仍用独立Background上下文；继承client生命周期并取消；实际NATS关闭任务RED/GREEN；最终queue整包race通过22.903秒。 | internal/queue/nats.go |
| OCT02-R-014 P1 | fixed_verified | 非法DATABASE_URL的name解析错误可能输出完整带凭据URL；改安全配置错误；合成密码真实RED/GREEN；最终配置整包race通过1.013秒。 | internal/config/config.go |
| OCT02-R-015 P1 | fixed_verified | 多语言资产读取失败仍允许单语回退提交，替换快照会丢失未读取译文；原界面也未规范化API的扁平语言字段；旧生产产物真实浏览器RED；成功读取之前禁止提交并提供重试，上传/保存同步守卫；新产物正确扁平契约恢复场景通过。 | FE:app/_components/localized-asset-editor.tsx |
| OCT02-R-016 P2 | fixed_partial_verification | 皮肤列表、详情和播放器档案读取失败没有恢复入口，档案列表异常被吞；保留过滤/选择，显示错误与重试；目标lint和整库typecheck通过；皮肤列表真实界面GET重试保持过滤通过，其余详情仅静态复查。 | FE:app/_components/player-profile-detail.tsx, FE:app/_components/skin-detail.tsx, FE:app/_components/skin-library.tsx |
| OCT02-R-017 P2 | fixed_verified | 活动清理非法对象过滤被静默丢弃，旧preview可能对应另一组筛选；集中解析器保持合法空数组，拒非法JSON/日期/仅空成员并阻预览；分类/filter改变立即清旧preview，生产UI受控无效输入与filter切换场景已通过。 | FE:app/_lib/activity-cleanup-filter.mts, FE:browser-tests/admin-data-protection.browser.mts, FE:app/_lib/activity-cleanup-filter.test.mts |
| OCT02-R-018 P1 | fixed_verified | 审核配置读/JSON错误回退false可跳审核；模板raw重复key被map合并隐藏，并发版本读取不原子；真PG配置错误及重复key两组RED/GREEN；缺行默认、全需审核故障回退、重复400且不写、并发版本与单连接3顶层race通过3.224秒；并发版本没有独立旧版RED。 | SYSTEM_NOTIFICATION_LOCALIZATION.md, internal/httpapi/content_localization_worker.go, internal/httpapi/notification_template_handlers.go 等4处 |
| OCT02-R-019 P1 | fixed_verified | 依赖图SQL先无限递归遍历/展开后才LIMIT结果，既有PERF040查询预算无效；按唯一节点/候选邻接关系前置准入预算，8000链旧8000→1001；dense旧79600→4001；真实PG RED/GREEN最终race3top PASS46.673s，百万关系索引31.54ms/原2s断言通过。 | docs/favorites.md, internal/httpapi/favorite_modpack_dependency_graph.go, internal/httpapi/favorite_modpack_export.go 等6处 |
| OCT02-R-020 P1 | fixed_verified | 正常stop取消在途heartbeat SQL，被误判为真故障，已完成seed run可标failed；实际TEST020 completed2want3；私有cancel cause仅忽略自身normal-stop Canceled且父context活跃；parent取消、23514和owner lost保留。受控PG旧RED1FAIL3PASS→新13top/0skip-race PASS46.800s。 | docs/seed-crawler-leases.md, internal/httpapi/seed_crawler_worker.go, internal/httpapi/oct02_seed_crawler_heartbeat_integration_test.go 等4处 |
| OCT02-R-021 P2 | fixed_verified | 隔离测试环境down移除已确认归属的容器及凭据后，因目录保留修复备份而rmdir ENOTEMPTY报错；仅捕获ENOTEMPTY/EEXIST保留额外文件，其他OSError继续传播；所有owner检查仍先于任何rm，不递归删除。新增保留备份测试旧RED1error/3PASS→新4PASS0.001s；实际7任务容器、4进程已按exact身份收尾，原3云服务保持，备份保留。 | tools/testing/isolated_environment.py, tools/testing/isolated_environment_test.py |

### AI、认证、目录与评论

| ID / 级别 | 结果 | 根因、修复与验证 | 位置 |
| --- | --- | --- | --- |
| OCT02-A-001 P1 | fixed_verified | 供应商输出只要能解析就发布，缺字段、重复键、占位符损坏与长内容截断缺乏拒绝门；完整源项映射/字段白名单、重复 JSON 键、空文本及保护片段校验，finish_reason/stop_reason 截断拒绝；oct02_ai_translation_test.go；错误 usage 保留测试 | internal/httpapi/ai_translation.go |
| OCT02-A-002 P1 | fixed_verified | 目录翻译迟到结果缺少目标译文版本和人工来源保护；源 revision 与目标 published revision 在事务内锁定校验；人工/human_corrected 译文保护；TestOCT02AICatalogSourceAndManualTargetProtectionIntegration | internal/httpapi/content_localization_worker.go |
| OCT02-A-003 P1 | fixed_verified | 社区/通知结果检查与写入不是同一事务，源更新、删除或通知归属变化可能迟到写入；所有业务写入事务内检查 execution，社区 active+approved+source revision，通知 owner/kind/source snapshot；社区删除/通知真实 worker 落库回归 | internal/httpapi/ai_translation.go, internal/httpapi/community_post_translation_handlers.go, internal/httpapi/content_localization_worker.go |
| OCT02-A-004 P1 | fixed_verified | worker 恢复后旧执行可更新任务/业务状态；管理重复提交创建重复待执行任务；started_at fencing/最多三次供应商请求，有界恢复；后台真实八并发一新七复用、变源另建PASS，最终AI30top/0skip-race4.115s。 | internal/httpapi/ai_handlers.go, internal/httpapi/ai_task_queue.go, internal/httpapi/content_localization_worker.go |
| OCT02-A-005 P1 | fixed_verified | 用户每日额度只按短原文估算，未含 prompt/输出，失败 usage 不计入；全 prompt 字节保守估计 + capped 输出预留；失败也保留 usage，未知保留预留；配置变化执行时不超已入队额度；SEC016 + OCT02 完整请求预留回归 | internal/httpapi/ai_translation.go, internal/httpapi/community_post_translation_handlers.go, internal/httpapi/content_localization_handlers.go |
| OCT02-A-006 P1 | fixed_verified | 已有 Quotas 配置未在实际供应商请求执行，无法约束请求量、token 和费用；事务 advisory 锁原子按 site/provider/model/user + hour/day/month 预留独立供应商账本；已知 usage 结算、未知保守预留；统计兼容扩展；8 并发恰好一次准入，预算耗尽不调用，取消上下文后账本仍可结算 | internal/httpapi/ai_handlers.go, internal/httpapi/ai_translation.go |
| OCT02-A-007 P2 | fixed_verified | HTTP 错误回显供应商 body 和 URL error；固定 45 秒不尊重任务 timeout；端点可带凭据/query；状态/基础网络错误脱敏、ctx deadline、生产端点校验；共享 SSRF/proxy 漏洞由 B 修复；timeout/disconnect、敏感 marker 不回显；B 共享客户端测试 | internal/httpapi/ai_translation.go |
| OCT02-A-008 P1 | fixed_verified | logout 忽略数据库撤销/在线会话删除失败，另一暖实例仍接受本地 positive session cache；撤销+presence 删除同一事务；失败 503 保留 cookie，commit 后 shared invalidation；读取绕过陈旧 local positive；closed pool/rollback 与两个暖 API replica 回归 | internal/httpapi/auth_cache.go, internal/httpapi/auth_handlers.go |
| OCT02-A-009 P2 | fixed_verified | 列表遍历持有 rows 时签名 helper 现在要再查数据库，pool=1 会死锁；逐项查产生 N+1；creator/blueprint rows 关闭后一次授权；目录头像100项、MaxConns1、<=4 SQL 实际 PostgreSQL 通过。 | internal/httpapi/creator_mutation_handlers.go |
| OCT02-A-010 P1 | fixed_verified | 已软删除蓝图仍可下载 approved variant；排队等待的 complete upload 可将 deleted 重新 queued；下载增加非 deleted；advisory 锁后重新读取蓝图状态 FOR UPDATE；访客/owner/admin 删除下载 404；锁等待+并发删除不会复活 | internal/httpapi/blueprint_handlers.go |
| OCT02-A-011 P1 | fixed_verified | 目录 resource 的 POST 翻译可返回缓存译文，缺少 GET 已有 global_resource.view 门；POST 缓存译文复用 global_resource.view 权限；A018 真实 pending GET/cachedPOST 拒绝回归通过。 | internal/httpapi/content_localization_handlers.go |
| OCT02-A-013 P2 | fixed_verified | 末次附件处理崩溃遗留processing，失败无法终止；最后一次附件处理崩溃留 processing；有限恢复 failed 并同步附件，原故障及恢复回归。 | internal/httpapi/comment_log_attachment_worker.go |
| OCT02-A-014 P2 | fixed_verified | watch已读标记可覆盖并发新增回复；watch 锁后读取 fresh 回复快照，防 read marker 覆盖并发新回复；实际 PostgreSQL 红绿。 | internal/httpapi/comment_detail_handlers.go |
| OCT02-A-015 P1 | fixed_verified | 评论预览树无界，过滤屏蔽与分页边界不完整；预览64 descendants/depth<=3，先过滤 blocked，201节点旧红→65总节点绿；AST源门不替代行为。 | internal/httpapi/comment_handlers.go |
| OCT02-A-016 P1 | fixed_verified | 已撤销会话可被inflight旧loader重新填回正缓存；撤销marker覆盖 inflight loader 回填；真实 PG tracer/miniredis 竞争红绿。 | internal/httpapi/auth_cache.go, internal/httpapi/auth_handlers.go |
| OCT02-A-017 P1 | fixed_verified | 业务事务持连接却从pool读审核设置，单连接自锁；人工本地化/creator 配置读取复用本TX；未提交设置和目录100图Max1<=4SQL PG通过；不宣称所有事务故障已注入。 | internal/httpapi/blueprint_worker.go, internal/httpapi/catalog_editor_service.go, internal/httpapi/oct02_localization_transaction_integration_test.go |
| OCT02-A-018 P1 | fixed_verified | 公共目录挑选未批准源，继承viewer权限的loader写共享缓存，并保留未授权derived字段；公开源在latest/limit之前 approved；共享缓存清claims/generation同步，蓝图/资产/搜索/短链/community投影与缓存ACL实测通过；父发布/隐藏B同Tx generation和root搜索投影须共同交付。 | internal/httpapi/blueprint_worker.go, internal/httpapi/catalog_editor_handlers.go, internal/httpapi/catalog_editor_service.go 等12处 |
| OCT02-A-019 P2 | fixed_verified | 作者/管理/认证派生读取或DB写失败被误作404、unclaimed或空成功；creator claim统计错误500不伪unclaimed、角色坏JSON500、五个antiabuse管理DB写失败500/event Scan500；实际旧行为RED→PG GREEN。不宣称全库所有失败出口故障注入。 | internal/httpapi/admin_handlers.go, internal/httpapi/anti_abuse_handlers.go, internal/httpapi/auth_handlers.go 等4处 |
| OCT02-A-020 P1 | fixed_verified | 自动retention持advisory专属连接后重新借pool，单连接不能推进；retention持advisory专属conn复用全部查询/TX，有界unlock；真实 Max1 RED→race74.935s。 | internal/httpapi/activity_retention_handlers.go |
| OCT02-A-021 P2 | fixed_verified | metrics24h throttle在写入前占用，DB失败后仍阻合法重试，旧token可干扰新占用；token-owned throttle lease，DB失败释放可重试，旧token不删除新token，成功24h保持；真实 PG红绿/querycache整包race26.587s。 | internal/httpapi/content_metrics_handlers.go, internal/httpapi/content_metrics_security_test.go |
| OCT02-A-022 P2 | fixed_verified | metrics头像查询持rows回入pool，存在单连接阻塞与N+1；metrics rows关闭后批量授权，100头像<=3SQL/Max1实际race3.739s。 | internal/httpapi/content_metrics_handlers.go, internal/httpapi/content_metric_developer_source_test.go |
| OCT02-A-023 P2 | fixed_static_regression_pass | 模板/recipe绑定遍历遗漏Rows.Err和catalyst清理，迭代失败可能静默返回不完整结果；template/normalizeRecipe绑定 Rows.Err、catalyst Close 及 event迭代错误 failclosed；已逐调用链复查，不假称每条都故障注入。 | internal/httpapi/anti_abuse_handlers.go, internal/httpapi/catalog_editor_handlers.go, internal/httpapi/catalog_editor_read_errors_integration_test.go 等5处 |
| OCT02-A-024 P2 | fixed_verified | community mutation直接err.Error回显内部存储错误；community存储错误固定安全500，冲突409保留；真实 PG 故障红绿。 | internal/httpapi/community_post_handlers.go |
| OCT02-A-025 P1 | fixed_verified | legacy schematic截断数组容错及AddBlocks高位丢失损坏内容；legacy schematic严格volume/完整数组与AddBlocks高位；旧overlay真实RED→raceGREEN。 | internal/httpapi/blueprint_codec.go, internal/httpapi/blueprint_codec_test.go |
| OCT02-A-026 P1 | fixed_verified | NBT负数组可panic，异常集合长度可触发过量分配；NBT完整wire preflight：32MiB解压、depth64、value2Mi/accounting128MiB，负数组旧panicRED/all12tags有效保真；预算不是实测heap上限。 | internal/httpapi/blueprint_codec.go, internal/httpapi/blueprint_worker.go, internal/httpapi/blueprint_codec_test.go 等4处 |
| OCT02-A-027 P2 | fixed_verified | 共享限流故障后本地预热与fallback重复计同一请求；共享rate-limit故障不重复计本地预热请求；真实miniredis outage红绿、httpapi/querycache race通过。 | internal/httpapi/auth_rate_limit.go, internal/httpapi/auth_cache_test.go |
| OCT02-A-028 P2 | fixed_static_regression_pass | 七个集成测试资源及cleanup顺序不完整，失败出口可能挂起；七个测试 borrowed conn/TX/rows 立即 defer、有界 rollback 与正确 cleanup 顺序，PG-race80.872s正常路径；没有旧hang故障注入。 | internal/httpapi/access_log_ingestor_integration_test.go |
| OCT02-A-029 P2 | fixed_verified | 后台第二内容语言误读界面语言；后台 secondary_content_language 不再读UI language；三者均不同真实 PG fixture通过。 | internal/httpapi/admin_user_details.go |
| OCT02-A-030 P1 | fixed_verified | 权限更新先提交、审计拒写仍留下无审计权限；permission upsert/audit同TX显式错误，拒new无row/正常1audit/拒update旧值保留，race44.233s；RoleTrack新故障未单独注入。 | internal/httpapi/admin_handlers.go, internal/httpapi/role_track_handlers.go |
| OCT02-A-031 P1 | fixed_verified | AI queued publisher dead在任务页不可见且无恢复闭环；脱敏delivery_failure+双权限手动retry，仅无执行历史/账目且最新未published dead事件可恢复，8并发一接受/11拒绝分支/audit rollback真实PG通过，最终AI30top-race4.115s；不自动重播可能计费任务。 | FE:app/_components/admin-console-infrastructure.tsx, FE:app/_lib/ai-delivery-retry.mts, FE:app/_locales/de.ts 等15处 |
| OCT02-A-032 P2 | fixed_verified | 注册第二内容语言接受任意tag，与实际支持语言不一致；secondary只接受八支持locale/aliases，primary保合法BCP47；注册实际验证race通过。 | internal/httpapi/auth_handlers.go |
| OCT02-A-033 P1 | fixed_verified | 独立身份审核允许本人admin批准自己并获得派生访问；独立identity审核禁止本人admin自审，PG RED200/derivedtrue→403/pending/derivedfalse；第三人批准通过。不改普通全局项目审核policy。 | internal/httpapi/creator_handlers.go |
| OCT02-A-034 P1 | fixed_verified | AI配置DB/解密/shape失败变空默认，保存可覆盖密钥，非事务读merge又可覆盖新轮换；缺行才default、503与同Tx advisory串行补key，真实旧GET200/PUT200/raw覆盖RED→五项PG GREEN及最终AI30top-race4.115s。 | internal/httpapi/admin_handlers.go, internal/httpapi/ai_handlers.go, internal/httpapi/oct02_ai_settings_integration_test.go |

### 业务、上传、配置与审核

| ID / 级别 | 结果 | 根因、修复与验证 | 位置 |
| --- | --- | --- | --- |
| OCT02-B-001 P2 | fixed_verified | 原收藏取消依赖公开可见性，隐藏项目无法清理；只对已证明本人的既有成员解除允许移除，新增/移动仍核可见性，真实PG HTTP负路径通过。 | docs/favorites.md, internal/httpapi/favorite_handlers.go, internal/httpapi/favorite_cleanup_integration_test.go 等4处 |
| OCT02-B-002 P1 | fixed_verified | OAuth transport错误可能把敏感URL写入回调日志；仅记录provider/错误类型，合成加密配置和失败transport实际红绿，无真实供应商调用。 | internal/httpapi/oauth_handlers.go, internal/httpapi/oauth_error_logging_test.go |
| OCT02-B-003 P2 | fixed_verified | 认证存储故障误401并可能丢会话；仅真正缺subject为401，DB/RBAC不可用503且不清cookie，closed-pool实际回归。 | internal/httpapi/middleware.go, internal/httpapi/middleware_test.go |
| OCT02-B-004 P1 | fixed_verified | 已保存项目图片URL可借服务器签发他人私有对象；统一校验active/clean/raster、uploader或真实approved绑定，revision作者证明与target-scope预览独立。真实PG授权正负路径、100图单SQL/单连接通过。 | CONTENT_PROJECT_SECURITY.md, internal/httpapi/mod_embedded_icon_import.go, internal/httpapi/mod_handlers.go 等13处 |
| OCT02-B-005 P2 | fixed_verified | 持 rows/TX 后回入 pool 造成单连接阻塞；Showcase/application/profile/economy/default-role复用既有queryer或先关闭rows；失败上传rollback后独立durable cleanup；Mod/Modpack/simple图片与ID预处理移TX前，slug advisory与文件live/scan/owner FOR SHARE仍TX。Mod真实race5top4sub117.871s，12并发slug4.342s；C两create真实RED→race12top0skip168.486s；失败注册旧30s/0intents→新<2s/2intents。 | CONTENT_PROJECT_SECURITY.md, docs/oss-object-layout.md, docs/profile-and-editor-errors.md 等18处 |
| OCT02-B-006 P1 | fixed_verified | 受限provider客户端沿环境proxy绕过origin IP审查；安全专用客户端禁proxy并拒mapped/non-global/CGNAT，原全局网络策略不改，实际本地proxy协议红绿。 | internal/httpapi/mod_import_worker.go, internal/httpapi/provider_proxy_security_test.go |
| OCT02-B-007 P1 | fixed_verified | metadata领取DB错误被确认且旧worker可写新attempt；传播错误，running+started_at fence保护进度/终态，bounded失败持久化；PG+本地provider/TEST035通过。 | internal/httpapi/mod_import_worker.go, internal/httpapi/mod_metadata_import_fencing_integration_test.go |
| OCT02-B-008 P1 | fixed_verified | 收藏夹父项先删及批量row-trigger导致统计错；先删子成员、按collection语句，PUT只改实际差异，保留ID/time；真实PG多夹/no-op/删除红绿，无生产旧统计结论。 | internal/httpapi/favorite_handlers.go, internal/httpapi/favorite_collection_popularity_integration_test.go |
| OCT02-B-009 P2 | fixed_verified | 额度读取错误伪造零usage/满额度，cache失败被误miss；余额/overview503、翻译cache错误不入队，closed-pool真实回归。 | internal/httpapi/notification_handlers.go, internal/httpapi/profile_handlers.go |
| OCT02-B-010 P2 | fixed_verified | 整合包/其他项目审核仅见history元数据却可批准；精确pending typed snapshot预览、target-scope/anti-self/图片绑定授权，八类型真实PG与route测试通过；不放宽普通私有详情。 | internal/httpapi/project_revision_preview.go |
| OCT02-B-011 P2 | fixed_verified | changelog目标查询DB失败误404；只有ErrNoRows404，其余稳定500业务码，真实认证HTTP断连红绿及race12top+11sub通过。 | docs/changelog-update-errors.md, internal/httpapi/governance_handlers.go, internal/httpapi/project_file_handlers.go 等4处 |
| OCT02-B-012 P2 | fixed_verified | PNG文本里伪tRNS被当透明；检查真实chunk/CRC/alpha语义，字节fixture红绿。P2格式判断，不夸大成无界内存。 | internal/httpapi/oss_admin_handlers.go, internal/httpapi/mod_resource_image_spec_test.go |
| OCT02-B-013 P2 | fixed_verified | 蓝图既有封面复用误要求当前editor为uploader且DB绑定失败假成功；已绑定合法文件可保留，Tx重核锁定/精确写入commit，PG拒写500/无事实后retry通过。 | internal/httpapi/oss_handlers.go |
| OCT02-B-014 P1 | fixed_verified | ZIP路径只用Unix规则，Windows drive/UNC/反斜线穿越可绕过；共用跨平台规范化及重复检测，字节fixture红绿，无真实攻击第三方。 | internal/httpapi/log_share_handlers.go, internal/httpapi/oss_handlers.go, internal/httpapi/log_share_handlers_test.go |
| OCT02-B-015 P2 | fixed_verified | 聚合通知改源仍保留旧译文、活动去重忽略payload；同TX cache失效与完整JSONB equality，真实worker/API PG3.399s通过，无AI调用。 | internal/httpapi/notification_handlers.go, internal/httpapi/notification_worker.go, internal/httpapi/notification_template_migration_test.go |
| OCT02-B-016 P2 | fixed_verified | 转账响应泄露收款人既有余额；删除无人调用的recipientBalance，税/双ledger和余额断言真实PG保持。P2隐私返回缺陷，不声称资金越权。 | internal/httpapi/economy_handlers.go, internal/httpapi/economy_handlers_test.go |
| OCT02-B-017 P1 | fixed_verified | 日志脱敏漏引用凭据/URI token，公开旧v1副本仍需升级；v2+专用applied版本、有限锁定重新脱敏/原子chunks，PG Max1 rollback/retry和前向列工具通过，旧线上内容尚未重处理。 | LOG_SHARE_SECURITY.md, cmd/db-log-redaction-upgrade/main.go, docs/log-redaction-upgrade.md 等8处 |
| OCT02-B-018 P2 | fixed_verified | pending changelog预览读取精确project-bound typed snapshot；既有全局可self/scoped anti-self policy保留。真正GREEN用freeze-final-race16top PASS292.768s与preview-final-pg18四top22.302s；旧changelog-preview-green文件实际fixture FAIL保留，不标绿。 | internal/httpapi/project_changelog_handlers.go |
| OCT02-B-020 P2 | fixed_verified | 日志retention trim删错键、碰撞由map次序决定且污染输入；新canonical map按确定顺序选别名，2新+2旧实际unit/race通过。 | internal/httpapi/log_handlers.go, internal/httpapi/oct02_log_retention_normalization_test.go |
| OCT02-B-021 P2 | fixed_verified | 模板插值再次解析用户值中的花括号；只遍历原模板一次，原缺参数失败，100重复字面与worker存储实际红绿。 | SYSTEM_NOTIFICATION_LOCALIZATION.md, internal/httpapi/notification_template_handlers.go, internal/httpapi/project_update_notification_worker.go 等4处 |
| OCT02-B-023 P1 | fixed_verified | follow预检后block提交仍写关注；follow/block/unblock同ordered-pair Tx锁，Tx内重核双向block。真实交错200/follow存在旧红→403/无follow新绿。 | internal/httpapi/profile_handlers.go, internal/httpapi/user_block_handlers.go, internal/httpapi/user_relationship_lock.go |
| OCT02-B-024 P2 | fixed_verified | 任务rewards接受null/array/string进入保存；先严格object400，合法正experience及可选null currency保留，3旧红/实际配置回归通过。 | internal/httpapi/economy_handlers.go |
| OCT02-B-025 P1 | fixed_verified | 唯一lease token与in-TX run/settings/source fence保护所有业务/状态写，过期五次终态与有界50恢复；late mutation和crash pending5真实RED→GREEN，现有BUG033/034/038/039/TEST019不削弱；final race16top/0SKIP PASS292.768s。外部请求/上传不严格一次。 | internal/httpapi/project_automation_worker.go |
| OCT02-B-026 P2 | fixed_verified | mirror扫描状态在候选与发布之间改变；旧trigger已拒unsafe，Tx重核active/scan/file FOR SHARE并置failed/scanning恢复。P2一致性恢复，未证实泄漏。 | internal/httpapi/project_automation_worker.go |
| OCT02-B-027 P2 | fixed_verified | HTTP recorder 覆盖第一次终态或把1xx当终态；首次final固定、interim1xx透传、101终态；unit/race5top2sub PASS1.485s。 | internal/httpapi/log_handlers.go |
| OCT02-B-028 P1 | fixed_verified | OAuth设置DB/解密/shape读错变空默认，PUT可清遗漏provider密钥；仅缺行default、503稳定错误、同Tx串行read/merge/upsert和normalized重复400，真实PG8top-race2.688s。 | docs/oauth-settings.md, internal/httpapi/admin_handlers.go, internal/httpapi/oauth_handlers.go 等4处 |
| OCT02-B-029 P1 | fixed_verified | OSS配置读取/密钥错误被空默认吞掉，保存可覆盖遗漏secret；同TX键锁strict读取/保留secret；实际RED→race3.041s，模块门2top PASS3.146s。 | internal/httpapi/oss_admin_handlers.go, internal/httpapi/oss_settings.go |
| OCT02-B-030 P2 | fixed_verified | 第101个changelog分类使整个集合返回500，现有编辑不可完成；兼容100-item category keyset页/精确flags，PG RED→race16top20sub PASS34.954s，生产React创建编辑2browser PASS；BE先于FE。 | FE:app/_components/project-changelog-editor.tsx, FE:app/_lib/project-changelog-api.ts, FE:browser-tests/changelog-categories.browser.mts 等9处 |
| OCT02-B-031 P1 | fixed_verified | 举报resolve/reopen忽略review插入冲突，重用旧key可改状态却无新审计；zero-row409及Tx rollback、仅精确open-target唯一冲突409，PG真实红绿13top-race4.110s。 | REPORT_AND_MODERATION_DESIGN.md, internal/httpapi/governance_handlers.go, internal/httpapi/oct02_report_review_idempotency_integration_test.go |
| OCT02-B-032 P2 | fixed_verified | log cache DB故障被误作miss且batch错误回显内部细节，关联profile/editor读错误误分类；错误failclosed/固定安全文案，真实lazy-store RED→PG-race4top12sub PASS43.038s。 | docs/profile-and-editor-errors.md, internal/httpapi/log_share_handlers.go, internal/httpapi/profile_settings_handlers.go 等5处 |

### 子资源、会话与媒体

| ID / 级别 | 结果 | 根因、修复与验证 | 位置 |
| --- | --- | --- | --- |
| OCT02-C-001 P1 | fixed_verified | Yggdrasil body处理期间撤销token仍可写纹理；Tx用户/账号/profile/token锁后重核启用、归属与wall-clock expiry，真实multipart撤销旧204红/新拒绝绿。 | YGGDRASIL_SESSION_SECURITY.md, internal/httpapi/yggdrasil_profiles.go, internal/httpapi/oct02_c_yggdrasil_revocation_integration_test.go |
| OCT02-C-002 P1 | fixed_verified | 既有SSE logout后仍发私有事件；事件缓存检查、20s heartbeat权威DB限3s、JWTexpiry终止，真实HTTP退出/活跃连接回归通过；缓存失效丢失时仍有有界传播窗口。 | internal/httpapi/realtime_handlers.go, internal/httpapi/oct02_c_realtime_revocation_integration_test.go |
| OCT02-C-003 P2 | fixed_verified | block列表持rows再读OSS设置，MaxConns1耗尽请求；收集/Close后批量头像授权，真实PG原timeout红/新绿。 | internal/httpapi/user_block_handlers.go, internal/httpapi/oct02_c_user_block_pool_integration_test.go |
| OCT02-C-004 P1 | fixed_verified | 公开子项目透露pending plugin父项名称/slug/ID；提交时验证approved父项、历史detail/card/facet过滤，保留自由文本引用，PG实际HTTP红绿。 | CONTENT_PROJECT_SECURITY.md, internal/httpapi/simple_project_handlers.go, internal/httpapi/simple_project_parent_facets.go 等5处 |
| OCT02-C-005 P2 | fixed_verified | skin update持业务Tx却从pool读review配置；用原Tx queryer，MaxConns1旧五秒deadline红/新持久写成功。 | YGGDRASIL_SESSION_SECURITY.md, internal/httpapi/skin_handlers.go, internal/httpapi/oct02_c_skin_pool_integration_test.go |
| OCT02-C-006 P2 | fixed_verified | sticker专属PNG尺寸预算在完整decode后才核；先IHDR界限再完整decode/reencode，保留既有general raster上限。静态确认+回归，不虚构旧OOM红。 | internal/httpapi/sticker_handlers.go, internal/httpapi/sticker_image_sanitization_test.go |
| OCT02-C-007 P1 | fixed_verified | mod child/history/export缺父公开门，撤审核后仍公开；approved或既有明确edit/review gate，公开carrier/server metadata同护，十HTTP旧200红/PG-race绿，不改角色policy。 | CONTENT_PROJECT_SECURITY.md, internal/httpapi/content_history_handlers.go, internal/httpapi/mod_content_capabilities.go 等12处 |
| OCT02-C-008 P2 | fixed_verified | role-track持rows再逐track查role，单连接阻塞且N+1；一条有序聚合保留空数组/position，真实Max1红绿。 | internal/httpapi/role_track_handlers.go, internal/httpapi/oct02_c_role_track_pool_integration_test.go |
| OCT02-C-009 P1 | fixed_verified | manual fallback查不存在mods.status且跨项目源未授权；改实际schema/approved或已有精准authority，preferred revision不是授权，真实42703/私有源红绿。 | CONTENT_PROJECT_SECURITY.md, internal/httpapi/mod_export_resources.go, internal/httpapi/mod_export_resource_resolution_integration_test.go 等5处 |
| OCT02-C-010 P2 | fixed_verified | 已提交导入job read-back错误吞掉还202；固定500并保留已提交任务，retry复用同queued job，真实PG错误JSON红绿/count仍1。 | CONTENT_PROJECT_SECURITY.md, internal/httpapi/mod_export_handlers.go, internal/httpapi/oct02_c_mod_content_visibility_integration_test.go |
| OCT02-C-011 P2 | fixed_verified | 未解析资源保留不可信resourceSources link/name；响应只合并内部已授权typed presentation，raw引用仍保留，三真实PG分支红绿；未执行浏览器XSS。 | CONTENT_PROJECT_SECURITY.md, internal/httpapi/mod_export_resources.go, internal/httpapi/oct02_c_mod_content_visibility_integration_test.go |
| OCT02-C-012 P2 | fixed_verified | concurrent recipe parser父取消却返回nil成功且消费迟到结果；消费前和drain后保留父cause，cancel-before/during实际unit/race红绿，无paid provider。 | CONTENT_PROJECT_SECURITY.md, internal/httpapi/mod_export_recipe_import.go, internal/httpapi/oct02_c_export_cancel_test.go |
| OCT02-C-013 P1 | fixed_verified | manual canonical binding选到别pending Mod的version；要求version.mod_id=binding.mod_id，真实私有名字旧泄露红/正确归属绿，另import授权preview保持。 | CONTENT_PROJECT_SECURITY.md, internal/httpapi/mod_export_resources.go, internal/httpapi/mod_export_resource_resolution_integration_test.go 等4处 |
| OCT02-C-014 P2 | fixed_verified | PNG只看generic header，截断PNG/改后缀JPEG可登记trusted；PNG完整decode及现有100M预算/单gate/cancel checks，实际HTTP持久Outbox/受控OSS和race通过，运行中标准库decode不可立即中断。 | CONTENT_PROJECT_SECURITY.md, internal/httpapi/mod_export_handlers.go, internal/httpapi/mod_export_import_handlers.go 等4处 |
| OCT02-C-015 P2 | fixed_static_regression_pass | Outbox测试Begin后t.Fatal路径无deferRollback可阻收尾；3处立即defer，原断言不变；PG+embedded NATS race8top通过，无旧挂起故障注入。 | internal/queue/outbox_integration_test.go |
| OCT02-C-016 P2 | fixed_static_regression_pass | activity load producer提前失败后无receiver，unbuffered发送不理ctx；peer cancel+cancellable send安全收尾，2000events/20producer PG-race3top通过，无旧故障注入；可选site聚合42883未验证。 | internal/activity/postgres_store_integration_test.go |

### 数据库、种子与治理

| ID / 级别 | 结果 | 根因、修复与验证 | 位置 |
| --- | --- | --- | --- |
| OCT02-DB-001 P2 | fixed_verified | 关于本站八种语言的种子使用普通 SQL 字符串保存 `\n`，在 PostgreSQL 默认 `standard_conforming_strings=on` 下成为字面字符，标题与段落不能正确分隔；使用 PostgreSQL `E` 字符串。仅精确匹配旧默认正文、原默认标题、revision=1、无编辑者且仍已发布的旧种子会被修正并递增版本；人工修订、不同标题和草稿保持不变。真实 PostgreSQL RED/GREEN、八种语言、重复启动和四种人工保护条件通过。 | internal/database/governance_automation_seeds.go, internal/database/governance_automation_seeds_integration_test.go |
| OCT02-DB-002 P1 | fixed_verified | 每次启动 `SeedRBAC` 都以默认值覆盖已有 `license_policies.redistribution_allowed/notes` 和 `ban_reasons.translations/sort_order`；维护者的禁止再分发决定可能被重启恢复成允许；对已有策略和运营理由不做冲突更新，仍补充缺失默认项。真实 PostgreSQL RED/GREEN 证明 MIT 禁止值、说明、理由排序和现有站务正文保持；缺失政策仍初始化。 | internal/database/governance_automation_seeds.go, internal/database/governance_automation_seeds_integration_test.go |
| OCT02-DB-003 P1 | fixed_verified | 已存在的默认角色仍重新插入所有缺失默认 grant；后台撤销的 `content.translate` 权限会在重启重新授予。角色创建与默认权限也未在同一事务安装；默认角色与初始 grant 同事务安装，既有角色不恢复允许权限。内建 banned 通配拒绝继续保持 fail-closed。真实 RED/GREEN、并发初始化、故障回滚与原 SEC021 用户种子回归通过。 | internal/database/seeds.go, internal/database/governance_automation_seeds_integration_test.go |
| OCT02-DB-004 P1 | fixed_verified | pgx 解析非法数据库 URL 时，外层错误脱敏后仍可能通过嵌套 URL 解析错误输出密码；启动日志原样记录 Connect 错误；Connect 的解析、建池和 Ping 失败出口使用安全错误表示，仅保留阶段、类型、合法 SQLSTATE 或取消/超时类别；Unwrap 保留 errors.As/Is。两个非法 URL RED/GREEN、恶意 SQLSTATE、原始 cause 和实际双连接池 UTC 回归通过。 | internal/database/database.go |
| OCT02-DB-005 P2 | fixed_documentation | 部分正式文档仍将旧代 schema、已删除前向回填、旧举报/统计字段当作当前实现；按现有模型更正评论、配方、举报与用户统计文档；保留有日期的历史测试报告，其旧代和 PASS 不继承为本轮证据。 | COMMENT_FLOOR_DESIGN.md, COMMENT_MARKDOWN_EDITOR.md, RECIPE_VERSION_BINDING_DESIGN.md 等5处 |
| OCT02-DB-006 P1 | fixed_verified | 两个服务器搜索触发器未去重同一服务器的 mod 原始标识，INSERT ON CONFLICT 一条语句可能重复更新同一队列主键，阻断 mod 改名/标识变更（PG21000）；SELECT DISTINCT 保留每个受影响服务器一项 upsert；真实旧定义 RED、当前完整 schema GREEN。 | cmd/db-function-repair/main.go, docs/database-function-repair.md, internal/database/changelog_schema.go 等11处 |
| OCT02-DB-007 P2 | fixed_verified | 更新日志热度未完整比较旧、新 active 状态，重复 deleted 再扣分，恢复 approved/active 不加分；根据已批准且活动的可见状态差分产生 release 事件；重复状态与恢复真实 PG RED/GREEN。当前 API 无删除/恢复入口，结论是数据库状态一致性缺陷，不是已有 UI 恢复失败。 | cmd/db-function-repair/main.go, docs/database-function-repair.md, internal/database/changelog_schema.go 等11处 |
| OCT02-DB-008 P1 | fixed_verified | 评论 AFTER ROW 在首次并发同 author 时两次读到1，计2位有效评论者；同SQL批量首次读到2则漏计，批量末次也可能重复扣分；AFTER STATEMENT transition-table 按 route/author 公开行数 delta 推导首末；route fact 行按序锁定，VOLATILE 后续 SQL 读等待后的新快照。真实并发旧定义 RED、修复 GREEN；批量旧定义实际为2评论/0作者，修复单/批/status/author-target移动及首末事件平衡通过。 | internal/database/comment_popularity_triggers.go, internal/database/comment_popularity_statement_integration_test.go |
| OCT02-DB-009 P2 | fixed_verified | 历史台账验证器以子串识别成功，把 NOT_PASS、BYPASS、未通过和“失败；通过”当成功，且忽略第16个额外列；仅接受明确 PASS/通过 起始标记及正常分隔符；要求恰好15列。七种失败词、合法括号格式和多列 RED/GREEN，完整历史449条格式/证据门仍通过；验证器只检查记录格式，不证明实际业务测试已经执行。 | tools/remediation/verify_findings/main.go |
| OCT02-DB-010 P2 | fixed_verified | 数据库目录工具直接调用 pgxpool.New 并 panic 原错误，非法数据库 URI 可能将密码写入终端/日志；复用应用安全 Connect 错误边界。真实 CLI 子进程输入合成非法 URI，修改前泄露、修改后不含密码/URI；不输出捕获的敏感样本。 | tools/audit/database_inventory/main.go, tools/audit/database_inventory/main_test.go |
| OCT02-DB-011 P2 | fixed_verified | 目录工具将 information_schema.columns 中三个 view 的列一起计为“表”，产生290表且同时报告3 view，和实际287 base table 不一致；联接 information_schema.tables 仅收集 BASE TABLE，view 单独保留。专用 PostgreSQL 18 新建精确归属 table/view 回归 RED/GREEN；最终只读目录为287表、1130索引、252非内部触发器、3 view。 | tools/audit/database_inventory/main.go, tools/audit/database_inventory/main_test.go |
| OCT02-DB-012 P2 | fixed_verified | 导出完成、过期、构建失败与租约耗尽先提交终态，后单独入队通知；Outbox 写入失败后终态不再被领取，永久丢失通知意图；四条入口在同一事务写终态与通知意图，过期文件 tombstone 同时回滚；保留原 notification.direct 事件、租约和上传补偿。真实 PG18 完整临时 schema、MaxConns=1、五项 Outbox CHECK 故障 RED/GREEN；解除故障重试与终态重复投递均保持一项意图，租约失败通知改为实际用户收藏详情链接。 | internal/httpapi/favorite_modpack_export_worker.go, internal/httpapi/oct02_favorite_export_notification_integration_test.go |

### 前端共享、后台与语言资源

| ID / 级别 | 结果 | 根因、修复与验证 | 位置 |
| --- | --- | --- | --- |
| OCT02-FS-001 P1 | fixed_verified | `/auth/me` 迟到结果可恢复已退出账号或覆盖新登录；认证代次隔离结果，旧请求 finalizer 不清掉新请求；真实认证模块与可控 HTTP 边界，退出、新登录、请求去重 3 测试 | FE:app/_lib/auth.ts, FE:app/_lib/auth-bootstrap-race.test.mts |
| OCT02-FS-002 P2 | fixed_verified | 浏览器翻译覆写可存入非字符串/空白，顺序 `replaceAll` 会二次替换或解释 `$`；校验 JSON 结构并用单遍字面插值；2 测试；最终 typecheck | FE:app/_lib/i18n-message.mts, FE:app/_lib/i18n-provider.tsx, FE:app/_lib/i18n-message.test.mts |
| OCT02-FS-003 P2 | fixed_partial_verification | 容器发布漏传文档已支持的图标/CSP 公开构建参数，且把可选 Yggdrasil discovery 设为必填；按现有配置转发；CI/Docker 合约 2 测试，未发布镜像 | FE:.github/workflows/release-container.yml, FE:Dockerfile, FE:app/_lib/ci-deployment-contract.test.mts |
| OCT02-FS-004 P2 | fixed_verified | 短链接导航直接信任 API 字符串；同源 HTTP(S) 闭集解析，拒绝凭据、控制符、反斜线、编码/多重斜线和外部 origin，输出本站 path；短链接 3 测试；页面调用方由页面专项交付 | FE:app/[publicId]/page.tsx, FE:app/_lib/short-link-state.mts, FE:app/_lib/short-link-state.test.mts |
| OCT02-FS-005 P2 | fixed_verified | 禁止 localStorage 时认证广播、请求客户端 ID、目录偏好和覆写会抛错；共享存储支持当前标签页回退，写入返回真实持久化结果，配额失败不被旧值盖回；拒绝读写、恢复、旧值/新值竞争 2 测试；页面专项另验草稿提示 | FE:app/_lib/api.ts, FE:app/_lib/auth.ts, FE:app/_lib/browser-storage.mts 等6处 |
| OCT02-FS-006 P2 | fixed_verified | 六个次要语言重建 `modIds` 的旧对象，丢失实际版本多选字段；翻译当前字段并移除无调用旧字段；解析器、有效 key 和占位符测试；确有修改前失败 | FE:app/_locales/de.ts, FE:app/_locales/es.ts, FE:app/_locales/fr.ts 等7处 |
| OCT02-FS-007 P1 | fixed_verified | 公共 API client 将合法 204 删除/取消关注当成缺失 JSON 的失败；204 返回 void，200 缺失 data 仍报错；实际回环 HTTP 的 204/200/畸形 200 断言 | FE:app/_lib/api.ts, FE:app/_lib/api-transport-contract.test.mts |
| OCT02-FS-008 P1 | fixed_verified | 收藏导出把 cookie-session 标记放入 Bearer 头，可能遮蔽有效 cookie；仅真实 Bearer token 设置头；实际下载入口与 HTTP 边界，cookie/Bearer 两分支 | FE:app/_lib/favorite-api.ts, FE:app/_lib/api-transport-contract.test.mts |
| OCT02-FS-009 P2 | fixed_verified | 正常轮询延迟未释放 abort listener，取消后迟到 load 仍发布进度；清理监听并检查结果发布前取消；轮询 5 测试，包括监听数和取消竞态 | FE:app/_lib/job-polling.mts, FE:app/_lib/job-polling.test.mts |
| OCT02-FS-010 P2 | fixed_verified | 标签选择器丢失请求语言和 signal，并把所有解析名称标为中文；透传 locale/cancel，使用服务端真实解析语言；1 测试；只读核对后端标签查询字段 | FE:app/_lib/global-catalog-api.ts, FE:app/_lib/resource-picker-loaders.ts, FE:app/_lib/tag-picker-locale.test.mts |
| OCT02-FS-011 P2 | fixed_partial_verification | 六个次要语言的下载上传提示承诺“已发布”，实际仍需安全扫描；改为上传完成、扫描后下载；完整词条/实际调用链静态复查，结构测试 | FE:app/_locales/de.ts, FE:app/_locales/es.ts, FE:app/_locales/fr.ts 等6处 |
| OCT02-FS-012 P2 | fixed_verified | app/components 实际静态调用缺失 21 个 key，上传页面还使用了旧标量；补齐 en/zh、准确上传状态及渲染进度，新增 AST CI 校验；TypeScript 解析器、重复 key、有效结构/占位符和静态调用 3 测试；曾真实失败 | FE:app/_locales/en-US.ts, FE:app/_locales/zh-CN.ts, FE:app/_lib/i18n-resources.test.mts |
| OCT02-FS-013 P2 | fixed_verified | 结构释放只释放共享材质/几何，遗漏 InstancedMesh 的实例缓冲；逐实例 dispose 并保持共享资源去重；真实 Three dispose 事件计数 1 测试 | FE:lib/mcmods-exporter/renderer/structureScene.ts, FE:app/_lib/structure-disposal.test.mts |
| OCT02-FS-014 P2 | fixed_partial_verification | 结构重建丢失第一人称模式、失焦遗留按键、旧截图更新新资源；保留模式、清空按键、截图取消守卫；静态调用链复查、lint/typecheck；完整交互行为尚未逐项验证 | FE:components/mcmods-exporter/StructureCanvas.tsx, FE:lib/mcmods-exporter/renderer/StructureRenderer.ts |
| OCT02-FS-015 P2 | fixed_verified | 浏览器 benchmark 分配 6000 实例缓冲却写入 9000，越界写被静默丢弃并低估内存；分配足量；实际 Chromium 600000 实例、9000 touched、46178404 typed bytes；只证明该样本 | FE:scripts/blueprint-scene-browser-benchmark.html |
| OCT02-FS-016 P2 | fixed_partial_verification | WebGL 构造错误逃出 effect 导致页面崩溃；皮肤组合部分纹理失败泄漏成功/迟到纹理。统一真实异步资源加载后初始化并释放部分资源；真实 Three.Texture 释放事件 2 测试，lint/typecheck；生产界面 fixture 浏览器结果见主报告 | FE:browser-tests/renderers.browser.mts, FE:components/mcmods-exporter/BlockModelCanvas.tsx, FE:components/mcmods-exporter/StructureCanvas.tsx 等5处 |
| OCT02-FS-017 P2 | fixed_verified | 目录排序在去空白后匹配，但返回原带空白值；返回已规范化枚举，移除不安全断言；目录开发契约 3 测试，新增带空白分支 | FE:app/_lib/catalog-sort.ts, FE:app/_lib/development-api-contract.test.mts |
| OCT02-FS-018 P2 | fixed_verified | 评分删除独自包装 fetch 丢失业务错误；复用 204 已兼容的 apiRequest，评分摘要支持取消；回环 HTTP 的评分 204/404 业务码与预取消断言 | FE:app/_lib/rating-api.ts, FE:app/_lib/api-transport-contract.test.mts |
| OCT02-FS-019 P2 | fixed_verified | 反滥用后台表单 await 后读取已释放的 event.currentTarget，多个操作未捕获失败、未保护重复点击；捕获实际表单并统一域内 mutation/error/busy 边界，取消 prompt 不提交；实际生产 handler、合成 HTTP/hooks 边界的 3 测试 | FE:app/_components/admin-anti-abuse-panel.tsx, FE:app/_lib/admin-anti-abuse-lifecycle.test.mts |
| OCT02-FS-020 P2 | fixed_verified | 蓝图材料名称导出 CSV 只转义引号，表格软件可把不可信名称解释为公式；将公式起始字符及首位控制字符作为文本导出；CSV 2 测试；调用方由页面专项交付 | FE:app/_components/blueprint-detail.tsx, FE:app/_lib/blueprint-csv.mts, FE:app/_lib/blueprint-csv.test.mts |
| OCT02-FS-021 P1 | fixed_partial_verification | 有限后台角色被强制请求无权读取的配置/权限/用户接口，403 被误作失效会话而退出；只读取已有权限允许的模块，保留 401 退出，权限版本变化重置后台实例；实际调用链/后端权限契约复核；真实浏览器角色验证见主报告 | FE:app/_components/admin-console.tsx |
| OCT02-FS-022 P1 | fixed_verified | 审核队列只链接普通私有详情/元数据历史，纯审核员无法读取真正待审提案；接入精确 pending 修订预览，验证白名单结构，按需读取、失败重试并惰性文本展示；3 解析器测试；生产 UI fixture 浏览器按需/503 重试/HTML 惰性展示 PASS；真实后端权限测试由后端单独记录，不互相替代 | FE:app/_components/admin-mod-panels.tsx, FE:app/_components/admin-review-preview.tsx, FE:app/_lib/admin-review-preview.mts 等6处 |
| OCT02-FS-023 P2 | fixed_partial_verification | 表情包启停/删除未捕获 Promise 失败；域内 mutation 捕获并显示错误，保留数据，统一同步重复提交守卫；最终语义复查、lint/typecheck；全部按钮交互未逐项验收 | FE:app/_components/admin-sticker-panel.tsx |
| OCT02-FS-024 P2 | fixed_partial_verification | t 改变触发初始化请求，语言切换覆盖经济/任务/权限设置及通知草稿；初始化按账号身份运行，当前翻译用 effect event；角色轨道列表更新不重置选中草稿；生产 UI fixture 实际另一标签页切换 en/zh，通知模板输入保留 PASS；其它受影响链静态复查 | FE:app/_components/admin-community-panels.tsx, FE:app/_components/admin-console-infrastructure.tsx, FE:app/_components/admin-console-users.tsx 等5处 |
| OCT02-FS-026 P2 | fixed_verified | 创建用户 await 后重读释放的表单，刷新失败使已创建用户被误报失败；捕获表单，创建成功立即清空，独立呈现后续刷新错误并防重复请求；实际生产 handler 2 测试 | FE:app/_components/admin-console-users.tsx, FE:docs/admin-data-integrity.md, FE:app/_lib/admin-user-create-lifecycle.test.mts |
| OCT02-FS-027 P1 | fixed_partial_verification | OSS 配置/通知模板读取失败仍允许提交默认值或空集合；必须成功读取当前身份，失败可重试，保存期间同步防重复提交；生产 UI fixture 503、禁止写入、重试 PASS；不操作真实 OSS | FE:app/_components/admin-console-oss.tsx, FE:app/_components/admin-console-users.tsx, FE:browser-tests/admin-data-protection.browser.mts 等4处 |
| OCT02-FS-028 P1 | fixed_verified | 本地 AI 完成直接应用旧闭包，覆盖等待期间人工词条/权限文字；记录编辑代次、跨标签存储快照与编辑戳，迟到结果保守丢弃；只应用非空源对应且占位符一致的字段；实际 provider 回调的 3 测试，包括编辑后恢复原文和另一标签存储更新；生产 UI fixture 手工输入保护 PASS | FE:app/_components/admin-console-permissions.tsx, FE:app/_components/admin-console-users.tsx, FE:app/_lib/i18n-message.mts 等7处 |
| OCT02-FS-029 P1 | fixed_partial_verification | 用户权限初始读取失败或切换目标仍可保存空/旧草稿；按账号/目标成功读取解锁，失败重试并重置节点选择，角色保存结果按选择代次隔离；生产 UI fixture 读取失败不会 PUT、重试后恢复 PASS；服务端授权不由此证明 | FE:app/_components/admin-console-permissions.tsx, FE:browser-tests/admin-data-protection.browser.mts, FE:docs/admin-data-integrity.md |
| OCT02-FS-030 P2 | fixed_partial_verification | 运行日志 setInterval 可重叠请求同一游标并让旧筛选迟到结果覆盖新筛选；完成后再调度，取消请求、隔离游标并按 ID 去重；最终调用链、lint/typecheck；慢请求/筛选竞态浏览器行为未验证 | FE:app/_components/admin-console-oss.tsx, FE:docs/admin-data-integrity.md |
| OCT02-FS-031 P2 | fixed_verified | UTC 每日统计桶在西部时区显示前一天；格式化明确 UTC；实际生产格式化函数 1 测试，西部时区和年界 | FE:app/_components/admin-dashboard-panel.tsx, FE:docs/admin-data-integrity.md, FE:app/_lib/admin-chart-date.test.mts |
| OCT02-FS-032 P2 | fixed_partial_verification | 注册界面把 preferredUILanguage 标为第二内容语言，后台第一/第二内容语言标签含混；明确注册界面语言与后台内容语言，配合后端 A-029 契约修复；完整注册/后台/后端 SELECT 契约复核，i18n 结构测试 | FE:app/_components/admin-console-users.tsx |
| OCT02-FS-033 P1 | fixed_verified | 每个认证 hook 都处理同一跨标签事件，互相作废请求，部分界面遗留旧账号；本地退出还会重读尚未清理的 cookie。改为模块统一事件监听、共享读取并发布所有订阅者，退出只发布空身份；真实模块与可控 hooks/HTTP 的红绿回归：3 消费者仅 1 次 GET、全部更新账号；退出不再 GET；原 3 竞态测试保留，共 5/5 PASS；生产 build-r2 严格账号切换/旧历史清除用例已 PASS，受控 API 边界不代替真实后端 | FE:app/_lib/auth.ts, FE:docs/admin-data-integrity.md |

### 前端页面与用户任务

| ID / 级别 | 结果 | 根因、修复与验证 | 位置 |
| --- | --- | --- | --- |
| OCT02-FP-001 P1 | fixed_verified | 更新日志时间清空时 render 中 toISOString 抛错，表单崩溃；submit前纯函数校验；node空/非法/时区2 PASS；浏览器表单保留PASS | FE:app/_components/project-changelog-editor.tsx, FE:app/_lib/changelog-form.mts, FE:browser-tests/oct02-forms.browser.mts 等4处 |
| OCT02-FP-002 P1 | fixed_verified | 更新日志界面locale effect重新初始化draft，丢失输入；token/资源身份只初始化一次，分类标签仍更新；浏览器PASS | FE:app/_components/project-changelog-editor.tsx, FE:browser-tests/oct02-forms.browser.mts |
| OCT02-FP-003 P1 | fixed_verified | 页头presence/theme/访客draft localStorage拒绝抛错；theme首屏snapshot不一致；复用best effort browser-storage，theme外部store SSR一致，明确临时保留；浏览器PASS | FE:app/_components/site-shell.tsx, FE:app/_components/theme-provider.tsx, FE:app/_components/tools-playground.tsx 等4处 |
| OCT02-FP-004 P2 | fixed_verified | 移动页头隐藏了全部语言和主题控制；原生details设置菜单，键盘/390px viewport实际browser PASS | FE:app/_components/site-shell.tsx, FE:browser-tests/oct02-forms.browser.mts |
| OCT02-FP-005 P2 | fixed_partial_verification | Minecraft选择器压缩分组硬编码中文；复用现有groupLabel/t，不新增伪译文；eslint PASS | FE:app/_components/minecraft-version-picker.tsx |
| OCT02-FP-006 P1 | fixed_partial_verification | mod/modpack/simple/asset编辑effect因界面语言重载draft；资源key与成功初始化token guard保留未保存编辑；mod/modpack/plugin browser PASS；asset静态复核 | FE:app/_components/creator-editor.tsx, FE:app/_components/creator-member-manager.tsx, FE:app/_components/localized-asset-editor.tsx 等7处 |
| OCT02-FP-007 P1 | fixed_partial_verification | mod自动导入语言变更取消旧任务但attempt guard阻止续poll，永久loading；useEffectEvent获取展示语言，实际导入参数驱动effect；共享poll单元PASS，真实导入外部服务未验证 | FE:app/_components/mod-editor.tsx |
| OCT02-FP-008 P2 | fixed_verified | 访客已有项目编辑页以loading优先导致登录入口不可达；鉴权就绪后优先登录状态；3种编辑页guest browser PASS | FE:app/_components/mod-editor.tsx, FE:app/_components/modpack-editor.tsx, FE:app/_components/simple-project-editor.tsx 等4处 |
| OCT02-FP-009 P2 | fixed_partial_verification | 取消项目关注async未catch，失败无恢复反馈；guarded mutation与error/saving状态；eslint PASS，失败API browser未验证 | FE:app/_components/project-follows-panel.tsx, FE:browser-tests/oct02-forms.browser.mts |
| OCT02-FP-010 P2 | fixed_partial_verification | 资产加载失败仍提供编辑并可能无更新却显示提交成功；加载边界，必须有真实skin/blueprint才可save；eslint PASS，真实资产写入未验证 | FE:app/_components/localized-asset-editor.tsx |
| OCT02-FP-011 P2 | fixed_verified | 三种导入poll离页继续，旧timer清理使Promise悬挂；共享waitForPolledJob+AbortSignal，卸载/token切换取消本页请求；定向10 node PASS（含取消断言） | FE:app/_components/mod-editor.tsx, FE:app/_components/modpack-editor.tsx, FE:app/_components/simple-project-editor.tsx |
| OCT02-FP-012 P2 | fixed_verified | 切换界面语言重载内容偏好，覆盖未保存选择；production picker选fr-FR/ja-JP后切zh，选择保留且不多GET，PUT精确primary/secondary值；专用Chromium断言已实际通过；专用3case及R7 whole已通过，合成API不证明DB/供应商。 | FE:app/_components/content-language-preferences.tsx, FE:browser-tests/oct02-forms.browser.mts, FE:browser-tests/specific-journeys.browser.mts |
| OCT02-FP-013 P2 | fixed_partial_verification | simple icon裁剪preview ObjectURL未释放；missing/token路径与finally释放；eslint PASS，内存长测未执行 | FE:app/_components/simple-project-editor.tsx |
| OCT02-FP-014 P2 | fixed_partial_verification | history/changelogentry/history旧请求无取消，endpoint游标未隔离；初载显示空历史；资源key与取消守卫；初始loading；现有pagination契约3 PASS | FE:app/_components/content-history.tsx, FE:app/_components/mod-history.tsx, FE:app/_components/project-changelog-entry.tsx 等5处 |
| OCT02-FP-015 P1 | fixed_verified | SimpleProjectCatalog请求无守卫，旧筛选结果覆盖新URL列表；逐次abort与取消后禁止写入；controlled迟到response browser PASS | FE:app/_components/simple-project-catalog.tsx, FE:browser-tests/oct02-forms.browser.mts |
| OCT02-FP-016 P2 | fixed_verified | catalog卡片keydown冒泡，子按钮Enter/Space跳详情而非操作；仅event.target===currentTarget处理；share/guestfavorite键盘browser PASS | FE:app/_components/mod-catalog.tsx, FE:app/_components/simple-project-catalog.tsx, FE:browser-tests/oct02-forms.browser.mts |
| OCT02-FP-017 P2 | fixed_verified | 访客收藏以siteId写但uniqueId读，状态永远不显示选中且存储拒绝异常；统一uniqueId及browser-storage；访客收藏键盘/禁存储browser PASS | FE:app/_components/mod-catalog.tsx |
| OCT02-FP-018 P2 | fixed_partial_verification | 单独保存资料字段全量覆盖其他未提交编辑和请求期间的新输入；仅同步该payload字段且确认当前值仍等于已提交快照；eslint PASS，组件browser回归待加 | FE:app/_components/user-home.tsx |
| OCT02-FP-019 P2 | fixed_verified | 收藏夹 mutation无busy，语言切换重载重选首项；busy guard/禁用控件/明确删除ARIA/保留选择；已有删除默认保护/取消/503保留/重试browser PASS；删除能力原本存在，未重复新建 | FE:app/_components/user-home.tsx |
| OCT02-FP-020 P1 | fixed_verified | standalone Markdown加载effect依赖t，语言切换覆盖未autosave正文；EffectEvent错误文案，加载只依赖token/retry；实际production browser PASS | FE:app/_components/tools-playground.tsx |
| OCT02-FP-021 P2 | fixed_partial_verification | Drawio回传仅origin，无当前iframe source核对；当前iframe contentWindow+origin联合守卫；eslint PASS，真实第三方iframe未请求 | FE:app/_components/tools-playground.tsx |
| OCT02-FP-022 P1 | fixed_partial_verification | ContentMetrics sessionStorage拒绝抛error，资源页异常；捕获拒绝，最多512键本tab访问去重；eslint PASS，该项完整交互矩阵未独立执行 | FE:app/_components/content-metrics-panel.tsx |
| OCT02-FP-023 P2 | fixed_partial_verification | 资料统计硬编码zh/en文案，数字不随UI locale；20现有标签迁正式locale（shared），数字Intl(locale)；eslint PASS | FE:app/_components/content-metrics-panel.tsx, FE:app/_components/global-recipe-card.tsx, FE:app/_components/mod-resource-components.tsx |
| OCT02-FP-024 P1 | fixed_verified | 社区翻译90秒poll离页/目标语言切换无取消，迟到译文可串资源/语言；资源key、signal取消、90秒deadline、译文绑定locale、失败保留原文；当前r4受控production浏览器对应case通过只取消本页请求，不声称取消后端或供应商 | FE:app/_components/community-post-detail.tsx |
| OCT02-FP-025 P2 | fixed_verified | 已有AI成本页无法区分预留/未知usage，界面断链；可选requestBudget最近30天provider/model/state及人民币配置估算，mock production成本面板PASS；非真实账单。 | FE:app/_components/admin-console-infrastructure.tsx |
| OCT02-FP-027 P1 | fixed_verified | CommunityEditor原读取失败仍开放空draft/autoDraft；actor/kind/id key与successful-load gate/Retry；forms community当前生产受控browser PASS。 | FE:app/_components/community-post-editor.tsx |
| OCT02-FP-029 P2 | fixed_partial_verification | Blueprint poll依赖record对象持续重置interval/并发迟到结果；processing boolean、in-flight/AbortController/generation和Retry；完整静态复核，真实worker旅程未验证。 | FE:app/_components/blueprint-detail.tsx |
| OCT02-FP-030 P2 | fixed_partial_verification | Blueprint/Skin OSS上传成功后元数据失败重试重复上传或丢新输入；uploaded ID按file kind复用/ref/fieldset；实际OSS未调用。 | FE:app/_components/blueprint-upload.tsx, FE:app/_components/skin-upload.tsx |
| OCT02-FP-031 P2 | fixed_partial_verification | Skin尺寸探测迟到结果覆盖新文件，bitmap/ObjectURL生命周期未完整；generation guard、PNG/bitmap几何与bitmap.close/ObjectURL cleanup；静态复查，真实上传未验证。 | FE:app/_components/skin-upload.tsx |
| OCT02-FP-032 P2 | fixed_partial_verification | CreatorEditor/Member异步身份加载和成员并发提交乱序；actor/resource key、授权read gate/ref/Retry/规范payload；完整团队浏览器矩阵未验证。 | FE:app/_components/creator-editor.tsx, FE:app/_components/creator-member-manager.tsx |
| OCT02-FP-034 P2 | fixed_partial_verification | PermissionComparison语言刷新重置比较选择/旧结果，SiteAffairs迟到locale内容；保留合法选择、清过期result、actor与About locale key；静态复查，独立browser未全验。 | FE:app/_components/permission-comparison.tsx, FE:app/_components/site-affairs-pages.tsx |
| OCT02-FP-035 P1 | fixed_partial_verification | contentTranslation/userNetwork/download/history 按 actor/locale/resource 隔离；翻译轮询90s停止，刷新仅GET状态，不重建付费任务；P1 已修，部分入口只静态复查 | FE:app/_components/editor/content-translation-control.tsx, FE:app/_components/user-network-list.tsx |
| OCT02-FP-036 P1 | fixed_verified | MessagesCenter 原 token-only效应会在 cookie-session常量下保留上一账号私信；最外层按实际userID/token卸载全部私有状态。其它账号/编辑/收藏/举报同步隔离；P1 已修，当前r4受控production浏览器对应case通过 | FE:app/_components/messages-center.tsx, FE:browser-tests/oct02-state.browser.mts |
| OCT02-FP-037 P2 | fixed_verified | 私信发送响应仅清空提交快照一致的输入，ref防重复POST，消息ID去重；通知译文按locale+id；void realtime错误捕获；P2 已修，当前r4受控production浏览器对应case通过 | FE:app/_components/messages-center.tsx, FE:browser-tests/oct02-state.browser.mts |
| OCT02-FP-038 P1 | fixed_verified | 用户PUA marker可导致URIError或伪造自定义资源引用；预先可逆转义，安全decode失败保留文本；P1 已修，单元2 PASS | FE:app/_components/markdown-renderer.tsx, FE:app/_lib/markdown-code-boundaries.mts, FE:app/_lib/markdown-markers.mts 等6处 |
| OCT02-FP-039 P2 | fixed_verified | regex目录计入代码示例且重复heading编号错误；真实remark AST/插件，全部标题先编号后按depth筛选。KaTeX等所有富标题组合未全部验证；P2 已修，单元2 PASS | FE:app/_components/markdown-renderer.tsx, FE:app/_lib/markdown-toc.mts, FE:browser-tests/oct02-state.browser.mts 等4处 |
| OCT02-FP-040 P2 | fixed_verified | template创建成功后section失败丢失已创建身份，重试可能重复创建；保留publicID/指纹并复用模板，失败保留输入与同步提交守卫；生产受控浏览器重试复用同模板通过。 | FE:app/_components/custom-content-template-settings.tsx, FE:app/_components/mod-content-workspace.tsx, FE:browser-tests/oct02-recovery.browser.mts |
| OCT02-FP-041 P2 | fixed_partial_verification | combineCatalogFiles大数组spread可能栈溢出，改逐项push；版本创建成功先清create表单再reload，reload失败不留原新建状态诱发重复；P2 已修，部分静态验证 | FE:app/_components/mod-content-workspace.tsx |
| OCT02-FP-042 P1 | fixed_verified | ResourceEditor加载失败仍可编辑和autoDraft空原记录；loaded gate与Retry；显式version属于目标资源才能开放；P1 已修，当前r4受控production浏览器对应case通过 | FE:app/_components/mod-content-resource-editor.tsx, FE:browser-tests/oct02-state.browser.mts |
| OCT02-FP-043 P2 | fixed_partial_verification | Range draft随外部restore更新并清旧invalid；主写成功completeDraft失败显示独立warning，community/changelog用原生alert后到成功结果，不冒充主写失败；P2 已修，部分验证 | FE:app/_components/mod-content-resource-editor.tsx, FE:app/_components/project-changelog-editor.tsx |
| OCT02-FP-044 P2 | fixed_verified | partial page用本页组成员数判断singleton，分类操作错误清掉与未加载资源同group的本页成员；partial page不得清本页singleton旧similarGroup；移动显式detach，完整全量才判singleton。基线实际RED与3个helper GREEN；C确认后端合并完整snapshot不删除未加载资源。connected-component自动合组政策另属候选产品语义；已修关键分页缺陷；高级分组政策待决策 | FE:app/_components/mod-content-layout-editor.tsx, FE:app/_lib/mod-content-layout-groups.mts, FE:app/_lib/mod-content-layout-groups.test.mts |
| OCT02-FP-045 P2 | fixed_partial_verification | 跨页选中节点不存在会访问数组-1；检查真实节点，保存同步ref/fieldset与dirty关闭/beforeunload确认。已完整静态复查；pointer、撤销、多页canvas完整交互未验。 | FE:app/_components/mod-content-layout-editor.tsx |
| OCT02-FP-046 P2 | fixed_partial_verification | PlayerProfiles UI语言effect重载且纹理更新重新初始化未保存bio；初载只依赖token，draft按selected.publicId，主要操作busy guards；P2 已修，部分静态验证 | FE:app/_components/user-player-profiles-panel.tsx |
| OCT02-FP-047 P1 | fixed_partial_verification | profile/card请求取消守卫+actor身份key；UserHome/编辑器同cookie actor隔离，无上一账号私有状态留存；P1 已修，部分静态验证 | FE:app/_components/catalog-manual-editors.tsx, FE:app/_components/mod-catalog.tsx, FE:app/_components/server-catalog.tsx 等6处 |
| OCT02-FP-049 P2 | fixed_partial_verification | heatmap数据按UTC日键，日期格式显式UTC避免负offset偏前一天；完整时区/月份矩阵未交互测试；P2 已修，部分静态验证 | FE:app/_components/user-profile-overview.tsx |
| OCT02-FP-050 P2 | fixed_verified | filled accent改主题on-accent，深主题保留亮绿底/深字；旧light4.09/dark2.22/hover1.66低4.5实际RED保留，r5双theme Chromiumcomputed/WCAG断言通过：light4.821/hover6.086、dark8.347/hover11.165，390zh无溢出。 | FE:app/globals.css, FE:app/_components/blueprint-viewer.tsx, FE:app/_components/catalog-list-ui.tsx 等31处 |
| OCT02-FP-051 P1 | fixed_verified | imported recipe编辑把canonical type当成真实type publicId，导致编辑路由错误；production资源卡打开popup；首次recipeGET503可见Retry，取得真实typeID后进入editor；首次PUT503保留draft，第二PUT精确type/template/baseRevision/dirtyLocale，成功关闭popup；专用3case及R7 whole已通过，合成API不证明DB/供应商。 | FE:app/_components/global-catalog.tsx, FE:browser-tests/specific-journeys.browser.mts |
| OCT02-FP-052 P2 | fixed_partial_verification | section继续加载的旧then/catch/finally可覆盖新筛选；ref与搜索/refresh scope隔离旧结果及busy。完整源码已复查，乱序失败浏览器断言未执行。 | FE:app/_components/mod-content-section-page.tsx |
| OCT02-FP-053 P1 | fixed_partial_verification | Tag/RecipeType原加载失败开放空编辑；loaded gate+Retry，actor隔离，初contentLocale不随UIlocale覆盖；P1 已修，部分静态验证 | FE:app/_components/catalog-manual-editors.tsx |
| OCT02-FP-054 P2 | fixed_verified | 部分附件成功后后续失败丢已有proofID，申请重试重新上传；同tick无ref互斥，账号/目标切换仍可迟到写入；认领/编辑申请成功proofID逐项保留、批量失败只重试失败文件、申请失败保留全部ID；ref排他/字段冻结/关闭守卫/actor key。四确定性用例PASS，当前r4受控production浏览器对应case通过；已修；FS独立回归 | FE:app/_components/comment-markdown-editor.tsx, FE:app/_components/creator-detail.tsx, FE:app/_components/project-editor-application.tsx 等5处 |
| OCT02-FP-055 P2 | fixed_partial_verification | Comment数组functional mutation，提交快照清空输入、附件仅移已提交ID；reply保留新输入；相同reaction/watch ref排他；sort scope阻止旧成功返回；P2 已修，部分静态验证 | FE:app/_components/comment-section.tsx |
| OCT02-FP-056 P2 | fixed_partial_verification | Loot高级JSON错误类型可record转换空对象删除字段；整体/pool/entry必须object，numberprovider仅允许finite number或object；外部reset恢复validity；P2 已修，部分静态验证 | FE:app/_components/editor/loot-table-visual-editor.tsx |
| OCT02-FP-057 P2 | fixed_partial_verification | ResourcePicker locale/token变更重置游标/page1保留选择；分页旧请求abort。键盘全journey仍待browser；P2 已修，部分静态验证 | FE:app/_components/editor/mod-resource-picker.tsx, FE:app/_components/editor/resource-picker-dialog.tsx |
| OCT02-FP-058 P2 | fixed_partial_verification | EditorShell/recipe/template提交中nativefieldset禁用，防继续改输入又路由退出；部分关联申请组件仍需同规则检查；P2 已修，部分静态验证 | FE:app/_components/creator-detail.tsx, FE:app/_components/editor/editor-shell.tsx, FE:app/_components/editor/recipe-editor.tsx 等5处 |
| OCT02-FP-059 P2 | fixed_partial_verification | recipe/editor/catalog bindings若干现有硬编码中文/英文非当前locale资源；新交付主要文案已复用正式词条，不能称全部静态词条本地化完成；recipe/template/slot labels等正式i18n；drawio按8实际Locale→官方代码闭集、savedAt Intl(locale)、icon模板单位/表格标签复用词条。目标语言回退不计母语质量通过；已修实际硬编码缺口；语义质量未全面验证 | FE:app/_components/catalog-resource-catalog.tsx, FE:app/_components/editor/recipe-editor.tsx, FE:app/_components/unified-recipe-card.tsx 等4处 |
| OCT02-FP-060 P2 | fixed_partial_verification | RecipeTemplate首个slot原只有pointer添加；现Input/Output/Catalyst原生Add按钮走同addSlot helper，仍保留canvas操作；P2 已修，部分静态验证 | FE:app/_components/editor/recipe-template-editor.tsx |
| OCT02-FP-061 P2 | fixed_partial_verification | 收藏导出预检与版本/loader不匹配仍可提交，失败轮询缺反馈；匹配预检、共享ref、native dialog与字段冻结，失败可重试。真实OSS/长期worker及完整焦点矩阵未验。 | FE:app/_components/favorite-modpack-export.tsx |
| OCT02-FP-063 P2 | fixed_partial_verification | AutoUpdateSettings await后event.currentTarget失效，先捕获form；busyguard，刷新失败不再“saved/queued”覆盖错误；P2 已修，部分静态验证 | FE:app/_components/project-auto-update-settings.tsx |
| OCT02-FP-064 P2 | fixed_partial_verification | server/creator debounce窗口旧cursor+新query；loadedScope与当前params相同才允许loadMore，保留既有generation；P2 已修，部分静态验证 | FE:app/_components/creator-catalog.tsx, FE:app/_components/creator-picker.tsx, FE:app/_components/server-catalog.tsx |
| OCT02-FP-065 P2 | fixed_partial_verification | Server历史请求失败永久skeleton；错误可见且Retry，范围更改清旧图；P2 已修，部分静态验证 | FE:app/_components/server-detail.tsx |
| OCT02-FP-066 P2 | fixed_partial_verification | Server probe busyguard；主服务器写成功draftcomplete失败不false-fail诱导重复提交；取消busy旅程未全面验收；P2 已修，部分静态验证 | FE:app/_components/server-submission-wizard.tsx |
| OCT02-FP-068 P1 | fixed_verified | 初次导入jobPOST等待期间仍可再次drop，导致重复上传/覆盖当前任务；native DragEvent同tick双drop及初次jobPOST未返回时第三drop，presign/jobPOST各恰好1；响应后仅poll job-one，完成恢复input且次数仍1；专用3case及R7 whole已通过，合成API不证明DB/供应商。 | FE:app/_components/mod-catalog-data.tsx, FE:browser-tests/specific-journeys.browser.mts |
| OCT02-FP-069 P2 | fixed_verified | 复制拒绝仍显示成功、表情目录失败不可恢复；复制捕获并显示copyFailed，目录Retry并选择合法pack。生产代码块与fixture回归通过，平台剪贴板权限未全验。 | FE:app/_components/log-share-viewer.tsx, FE:app/_components/sticker-picker.tsx, FE:app/_components/tools-plantuml.tsx 等4处 |
| OCT02-FP-070 P2 | fixed_verified | UserHome初始部分读取错误可见/Retry；profile原编辑草稿不随读取重置；notification成功读取前禁猜测默认写；files分支独立反馈与locale日期。state6相应用例R2 PASS；已修 | FE:app/_components/user-home.tsx, FE:browser-tests/oct02-state.browser.mts |
| OCT02-FP-071 P2 | fixed_verified | ModEditor原读取失败不开放空draft/autoDraft；规范化成功后才recordLoaded，保存ref、所有上传busy保护，draftcomplete失败独立提示。state6中3项目加载失败R2 PASS；已修 | FE:app/_components/mod-editor.tsx, FE:app/_components/site-shell.tsx, FE:browser-tests/oct02-state.browser.mts |
| OCT02-FP-072 P2 | fixed_verified | MinecraftPicker原生dialog+显式首末Tab/ShiftTab、Escape/焦点恢复、搜索label、toggle aria-pressed；Modpack/Simple ×可访问名称/错误alert。R2首末Tab断言FAIL后补wrap，未削弱断言；已修；当前r4受控production浏览器对应case通过 | FE:app/_components/minecraft-version-picker.tsx, FE:app/_components/modpack-editor.tsx, FE:app/_components/simple-project-editor.tsx 等4处 |
| OCT02-FP-073 P2 | fixed_verified | Changelog actor/resource remount、successful load gate、同步save ref、Markdown upload busy、原draft保持。B030分类101+keyset类别分续页由FS/BE另证，当前211行完整proof；已修 | FE:app/_components/project-changelog-editor.tsx |
| OCT02-FP-074 P2 | fixed_verified | URL pagination严格处理非整数/负数/unsafe offset，纠正URL并重新请求有效页；旧unsafe-offset实际RED保留，r6新产物global-page第三场景PASS，现有Mod/Simple分页断言保留；r6构建晚于最终源码且全量指纹无漂移，r5产物不覆盖最后保护的界限保留。 | FE:app/_components/global-catalog.tsx, FE:app/_components/mod-catalog.tsx, FE:app/_components/simple-project-catalog.tsx 等6处 |
| OCT02-FP-075 P2 | fixed_verified | canonical tag/recipe/template actor+resource键，旧私有draft不留给新身份；template失败gate。R2准确schema fixture后actor用例PASS；缺provenance首次是fixture错误，保留日志；已修；部分浏览器PASS | FE:app/_components/global-catalog.tsx, FE:browser-tests/oct02-recovery.browser.mts |
| OCT02-FP-076 P2 | fixed_verified | portal submenu ArrowDown/Enter转焦点、Arrow上下/HomeEnd/Escape恢复；notice named native alertdialog与Tab保持；unread切actor立即清旧值。R2 notice Tab实际FAIL后补首末guard；已修；当前r4受控production浏览器对应case通过 | FE:app/_components/site-shell.tsx, FE:browser-tests/oct02-recovery.browser.mts |
| OCT02-FP-077 P2 | fixed_verified | modpack卡categories/tags使用modpacks.categories正式命名空间，Mod卡继续原词条，不裸key；已修；R2 PASS | FE:app/_components/mod-catalog.tsx, FE:browser-tests/oct02-picker.browser.mts |
| OCT02-FP-078 P2 | fixed_verified | profile/creator/catalog/member/history/load failures可见Retry不冒充空，follow/block写ref；自身公开资料页面UIlocale不再reload→卸载UserHome私有settings。当前所列局部回归通过；已修；部分静态/实际fixture | FE:app/_components/user-profile.tsx, FE:browser-tests/oct02-recovery.browser.mts |
| OCT02-FP-080 P1 | fixed_verified | Tools旧文档upload迟到原可写新locale同名marker并解除新busy；actor/token/document key+sessionActive绑定，旧结果/后续文件/旧busy false都拒绝发布。旧生产产物明确RED与R2 GREEN，不代表真实OSS连通；P1 已修；R2 PASS | FE:app/_components/tools-playground.tsx, FE:browser-tests/oct02-recovery.browser.mts |
| OCT02-FP-081 P2 | fixed_verified | legacy tags/types canonicalID采用精确查询取可信publicID，零/多/失败Retry，保留query。R2 mockedAPI production2case PASS；A真实PG exact多registry/70前页干扰/cache/unknown PASS。先BE再FE；已修；前后端分别验证 | FE:app/_components/global-catalog.tsx, FE:browser-tests/oct02-recovery.browser.mts |
| OCT02-FP-082 P2 | fixed_verified | blacklist成功提示不再隐藏整列表，保留其它用户；unblock同步ref，初始GET失败明确Retry；已修；R2 PASS | FE:app/_components/user-network-list.tsx, FE:browser-tests/oct02-recovery.browser.mts |
| OCT02-FP-083 P2 | fixed_verified | rating PUT/DELETE同步共享ref、pending字段冻结/关闭守卫，错误保留review，reviews Retry，native dialog首末focus。旧产物实际textbox disabled false RED，首次缺follow fixture单独记录；已修；当前r4受控production浏览器对应case通过 | FE:app/_components/rating-panel.tsx, FE:browser-tests/oct02-recovery.browser.mts |
| OCT02-FP-084 P2 | fixed_verified | Github-only automation minecraft_versions显式空来源用或运算回退github，提交改错来源并报400；使用空值合并及合法kind默认，首次GET错误Retry/字段label，旧实际PUT400 RED；独立Chromium picker4top0skip-r3 PASS12.150s，r4完整34通过。 | PROJECT_AUTO_UPDATE_DESIGN.md, FE:app/_components/project-auto-update-settings.tsx, FE:browser-tests/oct02-picker.browser.mts |
| OCT02-FP-085 P2 | fixed_verified | Markdown代码围栏中的缩写定义保留字面，不全球strip；GeoGebra展开native dialog与焦点恢复。第三方iframe服务未调用，全部平台焦点边界未验；已修；当前r4受控production浏览器对应case通过 | FE:app/_components/markdown-renderer.tsx, FE:browser-tests/oct02-state.browser.mts |
| OCT02-FP-086 P2 | fixed_verified | 业务提交与Markdown上传共用busy互相解除，release上传可丢请求期间的新输入；独立busy与排他ref/fieldset/关闭守卫。旧产物字段未冻结实际RED，新生产浏览器server提交及release上传失败保留输入场景通过。 | FE:app/_components/project-downloads.tsx, FE:app/_components/server-submission-wizard.tsx, FE:browser-tests/oct02-recovery.browser.mts |
| OCT02-FP-087 P2 | fixed_verified | 独立Markdown首次draft GET未成功时允许编辑，迟到正文覆盖初始pending期间的新输入；authReady→初次draft GET成功前native fieldset/commit/upload guard，错误Retry；authenticated旧延迟GET覆盖新输入真实RED→r5相应bootstrap场景PASS。 | FE:app/_components/tools-playground.tsx, FE:browser-tests/accent-and-draft-readiness.browser.mts |

## 不计入确认修复数的记录

| ID | 分类 | 处理与边界 |
| --- | --- | --- |
| OCT02-A-012 | alias → OCT02-DB-008 | 根因与最终实施/验证并入 OCT02-DB-008，按canonical计一次；原阶段候选或待验表述不再代表当前修复状态。 |
| OCT02-B-019 | alias → OCT02-R-019 | 根因与最终实施/验证并入 OCT02-R-019，按canonical计一次；原阶段候选或待验表述不再代表当前修复状态。 |
| OCT02-B-022 | unconfirmed_candidate | 未实施新方案；merged publication batch replay 尚无实际可重播调用者/PG复现。 |
| OCT02-FP-026 | scope_only | 由035/046/047/059/063等具体项覆盖，不能按关联文件数量计算独立Bug。 |
| OCT02-FP-028 | alias → OCT02-FS-020 | 根因与最终实施/验证并入 OCT02-FS-020，按canonical计一次；原阶段候选或待验表述不再代表当前修复状态。 |
| OCT02-FP-033 | scope_only | 005/057/061/072/076/083落实具体名称/focus/busy，辅助crop/timezone等完整键盘矩阵未验；不重复计缺陷。 |
| OCT02-FP-048 | unconfirmed_candidate | 未擅自重定义经济币种/材料kind与alternative策略；无实际资金损失或XSS复现。 |
| OCT02-FP-062 | alias → OCT02-R-011 | 根因与最终实施/验证并入 OCT02-R-011，按canonical计一次；原阶段候选或待验表述不再代表当前修复状态。 |
| OCT02-FP-067 | unconfirmed_candidate | 未擅自重定义经济币种/材料kind与alternative策略；无实际资金损失或XSS复现。 |
| OCT02-FS-025 | ruled_out | 已证伪，不作代码修复。 |

## 交付与剩余验证

数据库generation168前向函数修复以及日志applied-redactor列须按 `DATABASE_REVIEW.md` 和正式修复说明先执行；本任务没有对生产执行。AI provider请求账本启用依现有generation168表，不承诺外部请求或计费严格一次。后端精确修订预览、类别分页、AI投递恢复及可选预算字段先部署，前端随后；未知/损坏配置现在稳定失败关闭，不以默认值覆盖数据。

默认go test/race（1239 topPASS/554 topSKIP）及vet/build均exit0。实际PG18整库r2：1793全部调用、76批0FAIL、1786topPASS/7SKIP退出0，日志 `evidence/final-pg18-db-suite-r2.log`，与1194当前Go指纹一致；Typesense29.0真实回环服务加searchindex整包racePASS6.822s。实际API/PG登录、收藏持久化及退出双语言移动live1topPASS3.441s（用例3.066s），没有本项目API mock。当前前端lint/type退出0；R6 unit344PASS0SKIP/build0、生产产物字节不变；最终R7 production Chromium73PASS/0FAIL/0SKIP180.621s，证据 `evidence/frontend-final-r7-browser.log`，647源码指纹无漂移，新增三项fixture各依实际断言更新。提交/关联PR与CI尚未验证。本表不把整库通过扩大为全部业务或生产健康。未证实蓝图迟到PUT补偿/锁序与公共事件合并重播仅保留候选，不擅自改变商业/权限policy。

本文件由七方完整进度/正式报告及最终handoff人工核对后合并；JSON是同一记录的生成视图。原始日志和环境身份记录私存任务目录，不提交密钥、DSN、用户数据或敏感payload。
