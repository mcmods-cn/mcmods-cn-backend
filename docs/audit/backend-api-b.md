# 后端 API B 审查与修复记录

基线：`bd50afd0ef92192c40c5152ea56e27a3fdbb657f`。本分工原始清单138个文件、41587行。范围包含认证/会话/OAuth、OSS、通知、评论、经济、蓝图worker/编解码、收藏导出、目录/导入与关联测试。当前阅读/语义/行为状态以 `review-backend-api-b.json` 为准，委派的25测试和17handler独立记录在 `review-backend-api-b-ui-b.json`。文件读取不是业务验收；修改后待复查的条目不得据基线hash计为最终完成。最终本清单170条（含原138、明确新增测试/辅助源码/正式说明、执行日志及人工语义问题索引）：170条内容/语义完整，0部分、0未审、0排除；最终行数与指纹以台账为准。原始138分母未缩减。行为测试仅按各项断言报告，不把完整阅读率当测试覆盖率。

## 已确认问题与处理

| ID | 级别 | 原因及影响 | 修改与验收 |
|---|---|---|---|
| BE-APIB-001 | P1 | 会话Redis/L1缓存能接受另一实例已撤销的session，缓存失效失败/迟到load亦不能保证撤销；退出两条SQL失败会假成功清cookie | `auth_cache.go`每次成功请求最后DB权威活性/版本检查；`auth_handlers.go`退出事务提交后才清cookie；真实PG+两实例缓存、迟到填充、密码触发版本变更、关闭pool退出失败均PASS。SSE每20秒重验，查询5秒上限；完整HTTP/middleware/gzip流首帧、心跳和撤销正常结束已真实验证，细节见024。 |
| BE-APIB-002 | P1 | OAuth提供商错误URL含敏感query，callback将error原样返回；GitHub零/负ID可参与绑定 | 固定公开错误、回调当前配置校验、GitHub ID>0；确定性HTTP transport拒绝缺/负ID、401/过大body及不回显secret通过。未连接真实OAuth提供商。 |
| BE-APIB-003 | P1 | 通知翻译无并发幂等，额度与任务入队未统一，GET结果重新Upsert可覆盖新/人工译文 | 原文updated_at快照、冻结术语版本、payload校验；同Tx advisory去重、站点/用户额度、任务与既有Outbox；GET只读当前新鲜译文；真实PG旧结果/原文变化/删除回归通过。worker快照保护由DB分工实现，真实供应商和语义质量未验证。 |
| BE-APIB-004 | P2 | 权限检查界面把admin.*数值shortcut当成特定显式deny的allow | `evaluatePermission`尊重解析后的deny，保留既有数字额度前缀；API A修正测试读取data envelope，4case通过。服务端原requirePermission已deny，未夸大为已利用越权。 |
| BE-APIB-005 | P1 | NATS URL userinfo/query/fragment及多server凭据进入管理响应 | 逐URL脱敏；若提交等于当前脱敏URL则保留内部旧凭据；确定性多server/非法URL回归通过，queue状态共享脱敏由主执行者实现。 |
| BE-APIB-006 | P2 | NATS配置落库后连接失败仍报告成功 | 保存后Reconfigure失败503 `nats_connection_failed`，details.saved=true；记录配置已保存，需要修正配置/重试。真实独立PG及本地TCP握手断连验证503/saved、密文持久化、GET恢复为disconnected并脱敏PASS；未将其描述成保存回滚。 |
| BE-APIB-007 | P2 | 基础设施指标数据库查询失败被读成零健康结果 | 查询错误503，关闭pool回归通过。不能据配置/测试实例指标宣称生产健康。 |
| BE-APIB-008 | P1 | 项目发行文件的presign/complete类型白名单不支持后续登记支持的mrpack/zip等 | 分作用域扩展名策略对齐发行API，统一复数projectType alias；14发行/8其他策略单测，以及真实PG+受控OSS HEAD四发行格式完整登记通过。 |
| BE-APIB-009 | P1 | req size省略0绕过真实OSS超过2GiB的对象限制 | 完成接口总是验证HEAD实际大小；受控HEAD返回2GiB+1，断言特定错误且PG未登记通过。 |
| BE-APIB-010 | P1 | 项目直传遗漏用户配额，余额读取失败/并发读取后insert可越限 | 下载相关用户/项目scope一致校验；完成接口真实byte计量、用户advisory和配额查询/登记同Tx；两600KiB并发在1MiB下仅一个成功，PG总量600KiB通过。worker生成对象不都使用此用户advisory，不能声称所有写入口严格总额度一致性已证明。 |
| BE-APIB-011 | P1 | 回复/表态/插眼仅ID查询已发布评论，忽略当前私有目标；旧插眼列表泄漏正文 | ID解析和列表复用目标权限/屏蔽规则；真实PG私有个人页访客/其他用户404、所有者/重新公开200、旧插眼不含正文通过。 |
| BE-APIB-012 | P2 | 插眼计数→判断→upsert非原子，可超2000 | 用户advisory同Tx，读写复用连接；1999已有+8并发只有1成功且active=2000，满额已有插眼幂等通过。 |
| BE-APIB-013 | P2 | 角色默认配置在持Tx时再pool查，连接饱和自等；读错返回空成功 | Tx查询配置，错误向上传播；MaxConns1/关闭pool回归通过。状态变更审计写入失败同样rollback，与API A权限审计链兼容。 |
| BE-APIB-014 | P1 | bigint金额乘费率/购买数量/下载奖励溢出或float精度丢失 | 分拆整数ceil税额和乘积溢出校验，余额加法防溢出；真实PG2^62转账及5倍购买拒绝通过；主执行者已在旧基线实测税0失败，再验证新税461168601842738791。 |
| BE-APIB-015 | P1 | 蓝图processing崩溃不恢复、旧worker迟到可改新任务/恢复删除资源；转换失败使已有ready产物失效 | 既有jobs新增run_token/heartbeat前向repair；claim+heartbeat+5min崩溃恢复、3次崩溃终止上限、publication/finish事务fence、归一化generation独立key、非删除状态条件；真实PG2worker竞争、crash重新领取、旧progress/publication/failure/completion拒绝、deleted不恢复、convert失败ready保留通过。未将这些测试描述为完整OSS归一化成功验收。 |
| BE-APIB-016 | P1 | 小压缩GIF DecodeAll先分配全部帧才检查上限 | UI B独占sticker GIF结构预检，帧/累计像素/延迟/块边界先检查；合成超限64x64x64旧约1.67MiB分配、新16bytes及正常GIF验证由其独立台账记录。 |
| BE-APIB-017 | P1 | Sponge不可信palette index直接负索引panic或巨大slice分配；NBT库按未验证声明长度先make，LimitedReader无效 | 稀疏map+负/重复/未知引用拒绝；NBT解压256MiB、128层、2M对象结构预检后库Decode；4组正常/压缩/畸形回归通过。未取得此codec旧基线红测的执行结果；巨大分配样本不在旧版执行。 |
| BE-APIB-018 | P2 | 重复follow返回true通知状态与DBfalse矛盾；项目私有后不能取消自己的关注；查询失败被假成功/404掩盖 | 返回实际notifications_enabled、删除只限自己的follow无需读取私有正文、错误传播/rows.Err；真实PG重复关注保留关闭通知、私有GET404而DELETE204并落库通过。 |
| BE-APIB-019 | P1/P2 | 商店Tx/rows占连接又pool查权限/配置/目标导致自等；非法/失败经济配置自动用enabled默认签到可能错误铸币 | 独立权限/目标/配置读前置，catalog读权限在rows前，配置仅无row默认、其余error503；真实PG MaxConns1 purchase/list/use实际缺文件校验通过，非法配置不能插入checkin通过。初始fixture误用了不存在权限code，真实失败后改成既有economy.checkin，未削弱业务断言。 |
| BE-APIB-020 | P1/P2 | 收藏导出额度check在Tx外并发可突破，preview/dependency持rows再pool读；worker报告错行被跳过仍build | 同Tx用户advisory+配额；先完整读取并关闭rows再解析，依赖DB/超过100展开返回明确预检失败；报告错误终止、有lease条件finish，旧lease失败不通知新任务。真实PG8并发仅1入队、active=2、Outbox=1；旧lease不改新attempt且无通知，disabled不领取，MaxConns1真实preview全部PASS。完整OSS产物上传成功未由此测试替代。 |
| BE-APIB-021 | P1/P2 | 全局公共catalog仅is_active缺源mod审批过滤；已拒/待审来源可公开，列表持rows再hydrate可自等 | latestGlobal scope仅approved，imported-only类型无可见snapshot不展示，manual无snapshot类型保留；跨来源资源仅approved、preferred由上游已授权入口保留；MaxConns1真实list/detail通过；pending404/不出现在列表，approved可见；resource版本装饰器同样隐藏pending父mod。目录素材其他旁路由DB分工修复，不能用此一处过滤宣称全部旁路已封。 |
| BE-APIB-022 | P1/P2 | 审核读取/解析错误静默默认，部分默认false会失去审核；通知后台读取失败空值可再保存覆盖 | checked读取区分无row与故障，null/非对象/错误类型拒绝；业务安全审核回退、admin503且不保存假空配置。确定性原始JSON/query fault回归PASS；追加独立PG真实null/array/错误字段类型、closedpool验证admin GET/PUT503且设置值/时间/actor未改，业务保守回退与修正后模板revision2落库均PASS。 |
| BE-APIB-023 | P2 | 配方并发解析取消后返回nil，使部分消费误报成功 | 解析前与全部workers收敛后检查context.Cause；真实ZIP取消前不消费/消费中取消均返回context.Canceled，PASS。 |
| BE-APIB-024 | P2 | access-log包装丢Flush导致SSE501，普通WriteTimeout和过期帧deadline导致流断开或截断结尾 | Root Unwrap/APIA FlushError跨链，ResponseController检查底层stream+写/flush错误，帧及最终chunk10秒有界deadline；真实identity/gzip20秒heartbeat与第三撤销EOF、底层无Flush501、Flush失败清订阅均PASS。保留初次unexpectedEOF失败证据后实际修复。 |
| BE-APIB-025 | P1/P2 | 导入无队列时每任务无界goroutine，进程退出/禁用可继续fallback，满池积压 | 4slot先获取后launch，queued满额保留、15秒scanner、关闭rows、进程context、disabled禁止；真实PG12并发仅4claim/8queued，取消全部processor结束，disabled不执行PASS。跨所有导入写TX lease fence由DB-026协作，当前任务锁再清staging，旧token不能删新staging。 |
| BE-APIB-026 | P1 | ZIP恢复签名丢ForbidOverwrite，任意同作用域key可覆盖已登记immutable对象；恢复size无上限 | 与初始上传保持禁止覆盖及2GiB上限；真实PG权限/本地SDK签名断言header为true和2GiB+1拒绝PASS，未调用真实OSS供应商。 |

## 模块、角色与契约

| 模块 | 用户/入口/权限 | 数据与闭环验证 |
|---|---|---|
| 会话/退出/OAuth | 访客登录，cookie/Bearer，登录用户退出，SSE requireAuth | auth_sessions/user版本为撤销权威；退出失败保留cookie可重试；无真实OAuth调用。 |
| OSS/项目发行 | 登录上传者，scope绑定项目权限，访客公开/有权限下载 | presign→受控HEAD→实际PG quota+register；真实OSS签名可用性、scan worker/下载字节完整性尚未由本分工动态验证。 |
| 通知/AI | 收件人/全站通知阅读，登录用户额度，retry ai.task.enqueue/cancel ai.write | user+notification+source+目标+冻结术语的task指纹；AI GET只读，不触发外发；worker/预算原子性由DB专项。 |
| 评论/插眼 | 访客可见目标只读，owner/编辑者与普通登录用户受目标/屏蔽/操作权限 | 回复/表态/插眼ID路由与目标可见性联动；并发2000上限与private旅程已真实验证。 |
| 经济/商店 | 登录用户签到/转账/购买/使用，管理员配置 | bigint精确SQL账本与锁余额，不使用float计算税；单连接和配置失败回归真实通过。API仍JSON number，前端超过2^53精确显示/操作未在本分工证明；改成字符串需跨契约兼容方案。 |
| 蓝图/格式转换 | owner/编辑者入队，worker系统执行，访客仅可见资源下载 | 任务token/heartbeat、删除与迟到fence真实验证；SVG/封面、完整四格式实体语义保留仍需额外验收。 |
| 收藏导出/全局目录 | 用户自身/公开收藏预检，登录用户生成；访客global读取，管理员独立编辑 | 导出快照、既有Outbox、MRPack产物；global来源审批、小池复用修复，详细动态范围以证据为准。 |

## 数据库与部署

业务ID、外键、唯一约束与软删以schema/实际SQL为依据，无生产连接或生产数据。此分工会话、上传、插眼、金额、关注、catalog过滤无需schema迁移。蓝图可靠性需DB分工新增前向repair `audit-2026-10-blueprint-worker-lease`（run_token/heartbeat）；停止所有旧blueprint worker→执行repair→启动新worker。旧API可利用列default，但旧worker没有fence，不能混跑。归一化新key按claim token；旧已登记key保持读取兼容，不批量迁移或清理真实对象。

真实隔离PG已验证核心约束/竞争行为，不代表生产连接压力、锁等待、膨胀、备份可恢复性、磁盘增长或完整迁移性能。空库/旧库/重复migration证据由DB专项统一记录。本分工没有操作生产或真实用户记录；fixtures均合成，独立database有创建时marker校验后清理，共用测试库fixtures以随机ID限定清理。

正式契约更新见 `../API_SECURITY_RELIABILITY.md`。六个旧集成测试的误连风险由Root TestMain/owned marker统一保护，不能用fixture名称或仅test数据库名推定允许破坏性操作。

## 环境和执行证据

最终工具Go1.26.8；PG/Redis/NATS仅任务回环隔离服务，环境由主执行者安装并初始化。测试脚本只应source任务隔离env，禁止把连接密码写入文档。下表 `.log` 原始输出受项目忽略规则保护，仅当前任务内可读，未提交为PR附件；可持续交付的命令、退出码、节点数、raw SHA与有限断言摘要保存在 `backend-api-b-validation.json`，不把临时日志称为仓库已交付证据。需要 `APP_ENV=test MCMODS_RUN_DB_INTEGRATION=1`，数据库连接指向身份确认的临时服务。外部OSS/OAuth/AI只用本地HTTP或确定性transport；没有真实付费AI请求。

| 结果 | 命令/证据 | 范围 |
|---|---|---|
| PASS | `go test ./internal/httpapi -run 'Test(CurrencyTransferTaxAndPurchaseAmounts|SpongeSchematic|BlueprintNBT|BlueprintJobLease|BlueprintWorkerDoesNot)' -count=1 -v`；`/tmp/mcmods-api-b-codec-economy-lease.log` | 7 tests，0.738s，Go1.26.8，金额/codec/PG lease |
| PASS | `go test ./internal/httpapi -run TestProjectFollow -count=1 -v`；`/tmp/mcmods-api-b-follow-integration.log` | 1真实PG关注旅程，0.620s |
| PASS | `go test ./internal/httpapi -run 'Test(EconomyItemOperations|EconomyInvalidConfiguration)' -count=1 -v`；`/tmp/mcmods-api-b-economy-connections-green.log` | 2真实PG单连接/失效配置，0.661s |
| PASS | `TestOSSProjectUploadRegistersReleaseFormatsIntegration`、`TestOSSCompletionChecksActualObjectSizeIntegration`、`TestOSSConcurrentProjectCompletionsRespectTotalQuotaIntegration`；`/tmp/mcmods-api-b-oss-integration.log` | 3 tests+4格式subcases，真实PG/受控OSS边界 |
| PASS | 评论private/插眼并发；`/tmp/mcmods-api-b-comment-integration.log`；role默认单连接/closedpool；`/tmp/mcmods-api-b-permission-integration.log` | PG权限、并发和错误恢复 |
| PASS | 最终 `go test -race ./internal/httpapi -run` 本分工32顶层测试；`evidence/backend-api-b-final-owned-race.log` | 真实owned PG/合成数据，15.278s；仅外部OSS HEAD/DELETE、OAuth和provider文件边界受控替代。 |
| PASS | 最终SSE+失效签到配置+审核/模板10顶层测试race；`evidence/backend-api-b-sse-final-race.log` | 33.516s，含真实HTTP20秒心跳、gzip、WriteTimeout1s、撤销EOF；配置null/错误拒绝。 |
| PASS | `go test -race ./internal/httpapi -run '^TestAdminNATSAndTemplateFailureStateIntegration$' -count=1 -v`；`evidence/backend-api-b-admin-config-final-race.log` | Go1.26.8，独立marker-owned PG，1顶层/6子场景全部PASS，3.967s；本地NATS TCP断连，模板损坏/closedpool不覆盖、修正后持久化。直接handler+合成claims，不代替鉴权middleware/E2E。 |
| FAIL（旧处理器对照） | 同选择器；`evidence/backend-api-b-admin-config-old-handlers-red.log` | 原bd50基线NATS整文件及3个通知/审核handler函数原文，复用当前checked helpers/隔离fixture/依赖；5失败场景FAIL、健康恢复PASS，2.790s。受限原函数对照，不声称整个旧checkout集成。 |
| NOT_RUN | 真实OAuth、真实OSS供应商、真实付费AI、生产DB运行健康 | 当前授权边界，不妨碍确定性业务回归 |
| NOT_RUN | 完整蓝图OSS归一化成功、所有角色和浏览器端到端 | 不把单元/静态/片段PG测试夸大为完整验收，待整体专项集成 |

此前部分测试曾失败（发行shader alias缺失、OSS fixture明文设置/响应code错误、经济permission code不存在）；失败被记录并按实际契约纠正，最终PASS采用明确green日志，未隐藏flaky或放宽安全断言。整体 `go test`/vet/race/build由主执行者最终统一运行，不能以旧基线PASS替代最终运行。

## 继续审查范围与决策

原始问题清单数量由主执行者文档核实，不机械采用约20项；以上26个ID是本分工新增确认问题（016由UI B实现，024根/APIA协作），不与UI B/API A/DB重复计算交叉修复。原138文件由本分工及明确委派逐段读完。catalog editor handler交UI A、service交DB保留匹配SHA的独立证据；本分工最终全部修改diff、上下文及受影响链已复查。完整/部分/未审最终数以主清单及聚合结果为准，不继承历史hash完成状态。

需要单独验证/决策：JSON bigint兼容精度策略；转换格式实体/方块实体语义完整性（原始文件保留，不代表转换产物全部语义保留）；第三方provider导入内容默认语言历史约定与真实语言不一致的处理，不凭英文站点自动重译；邮件最优努力通知与可靠投递产品承诺的边界。生产数据库或真实付费调用没有授权和费用上限，不执行。
