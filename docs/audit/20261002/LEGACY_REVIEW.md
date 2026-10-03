# 449 项历史问题逐项复核（2026-10-02）

本次将原问题台账的449个唯一ID逐项分配给七名审查者，汇总其人工语义判定。历史CLOSED、旧测试总数或脚本发现符号均不作为解决依据；本表是当前证据的索引，不重写历史报告。

完整原字段、每条手工判断、具体断言、命令/结果、当前文件指纹、有限边界及来源SHA见[LEGACY_REVIEW.json](LEGACY_REVIEW.json)。JSON为手工证据的机械汇总，不表示脚本完成语义审查。表内摘要有省略号时，完整结论及限制保存在对应JSON项。本次修复分类只统计这些历史ID的手工判定，不等于本次全部新发现问题的修复数量。

## 范围与来源

原表：[`01_FINDING_STATUS.md`](../../remediation/full-audit-remediation/01_FINDING_STATUS.md)。449行的15个历史字段及全部分配/测试映射字段保持原值；没有删除原FAIL/SKIP、历史名称未解析记录或已过时需求。七份分配集合互斥且完整，缺失/重复均0。

| 审查者 | 分配并手工核实 | 原输入与人工证据 |
| --- | ---: | --- |
| root | 89 | legacy-revalidation-root.json / legacy-semantic-root.json；输入SHA记录在JSON sources |
| backend_a | 46 | legacy-revalidation-backend_a.json / legacy-semantic-backend_a.json；输入SHA记录在JSON sources |
| backend_b | 75 | legacy-revalidation-backend_b.json / legacy-semantic-backend_b.json；输入SHA记录在JSON sources |
| backend_c | 91 | legacy-revalidation-backend_c.json / legacy-semantic-backend_c.json；输入SHA记录在JSON sources |
| database_docs | 24 | legacy-revalidation-database_docs.json / legacy-semantic-database_docs.json；输入SHA记录在JSON sources |
| frontend_pages | 32 | legacy-revalidation-frontend_pages.json / legacy-semantic-frontend_pages.json；输入SHA记录在JSON sources |
| frontend_shared | 92 | legacy-revalidation-frontend_shared.json / legacy-semantic-frontend_shared.json；输入SHA记录在JSON sources |

后端工作基线为95694ccff038a28a3e6126b05e3e7c5037bc7854；前端审查基线见JSON sources中的审查者元数据，两仓当前HEAD/分支见assembly_repository_versions。未提交修复按文件SHA绑定，不能只用HEAD代表最终内容。汇总补取的当前指纹仅用于识别漂移，不新增阅读或行为验证；指纹不匹配的历史测试需最终所有者复核。

## 手工分类统计

| 分类 | 数量 | 含义 |
| --- | ---: | --- |
| 原根因确认解决 | 372 | 原根因有当前语义保护及对应范围证据；不证明所有生产/浏览器场景 |
| 本次修复 | 10 | 本次修复了该条明确缺陷，具体断言及剩余边界逐项保留 |
| 部分验证 | 61 | 部分实现或验证范围不足，不能仅凭相邻PASS关闭 |
| 未验证 | 4 | 当前缺少原问题所需的独立证据 |
| 文档过时 | 1 | 描述与当前代码不一致，按原审查者判定保留 |
| 前端范围不适用 | 1 | 仅前端分工不适用；不代表整个问题全仓不适用 |
| 待最终门 | 0 | 尚无匹配最终证据的待门条目；当前root89已逐项更新 |

汇总器不按PASS自动提升状态。root的89项原待门记录逐项补当前版本及断言后，88项确认原根因解决，PERF-069保留部分验证：Node600k紧凑索引已通过，当前生产浏览器600kGPU长测未验证。原待门输入逐项原样保留，新的manual_followup_review单独说明改变依据。PERF-040经root明确手工补证：旧chain8000/dense79600工作量RED，新实际visited上限1001/4001、million索引与原cycle断言3top race46.673s GREEN，归本次修复；原B手工判定仍完整保留，本轮最新B75为70原根因确认解决及5本次修复。BUG-066原环终止/输出上限与新增内部工作量修复分别追踪，生产高负载仍未验证。

先前全PG门含8个失败批次（包括夹具/中间编译问题），不能记为全通过；最终独立真实PG18门已退出0：发现/调用1793项，76批次0失败，1786顶层PASS/7SKIP；7SKIP为6项真实外部来源样本与1项Typesenseopt-in，后者另用当前真实隔离Typesense执行完整searchindex包race通过6.822s，不能把该SKIP改为同一次PASS。最终1194Go文件SHA与执行前快照逐个一致，缺失/漂移均0。root最新默认/race全包退出0，1239个顶层PASS/554条件SKIP，coverage30.8%，vet/build退出0；785子例PASS/76子例SKIP也单独记录；当前独立PG门与默认条件SKIP分别保留。前端最后R6为344单元PASS、production build退出0及70浏览器PASS/0SKIP166.280s；受控ownAPI fixture的浏览器测试不冒充真实前后端联通。另有真实隔离ownAPI浏览器1PASS3.066s，仅覆盖具体选定旅程。单项PASS、条件SKIP、FAIL与来源版本逐条保存，mock供应商/受控浏览器不冒充真实外部服务。生产运行健康、真实付费供应商语义与账单没有证据。

## 跨仓库的部分验证追踪

frontend_pages的19项部分验证保持原判定。下面仅增加人工选择的同保护链条或原始精确测试映射；关联项的具体断言、实际结果与限制在JSON cross_repository_tracking中，不替代真实前端交互。没有找到同根因额外证据时明确保留缺口。

| 前端条目 | 同保护链人工记录 | 保留边界 |
| --- | --- | --- |
| BUG-062 | ARCH-017, BUG-033 | 空库fallback、持久同步可信性与服务端422边界需合并当前后端真实PG证据。 |
| BUG-127 | BUG-036, DEAD-003 | 125条实际数据库遍历、隐藏项目安全占位与100k计划由后端当前证据另核；此处没有真实关注API浏览器。 |
| BUG-139 | BUG-021, TEST-009 | FS已完成当前后台源复查；三状态205行PG遍历和服务端审核权限需合并后端当前证据。 |
| BUG-142 | 暂无额外同根因后台手工记录 | Root负责FP062最终工具/红action版本/浏览器；真实服务器部分创建与存储策略需合并B当前PG证据。 |
| BUG-144 | BUG-010 | 服务器权威definition、非一致回显409与审核/发布保护需合并当前后端证据；未真实保存recipe。 |
| BUG-147 | 暂无额外同根因后台手工记录 | 当前页999恢复另OCT02-FP-074；服务端parent可见性、126项分页和query plan需合并当前PG证据。 |
| BUG-148 | BUG-009 | 前端纯策略+源码接线已通过；真实canvas拖拽全旅程NOT_RUN，后端422需合并当前证据。 |
| BUG-149 | BUG-060, ARCH-014 | 原始JSON重复key在后端解码之前拒绝的保护只能由当前后端测试确认；source/Pure不证明资金不会丢失。 |
| BUG-151 | BUG-003 | 跨标签页数据库CAS与同session到达顺序必须合并当前PG测试；本代理未真实draft endpoint写入。 |
| PERF-036 | 原精确测试：TestAdminProjectPageCursorIsStrictAndFilterScoped, TestAdminProjectPageSQLUsesSearchAndCompositeKeyset | admin_project_catalog投影维护、百万级query plan和keyset安全需当前后端证据；此仅源码接线。 |
| PERF-054 | 原精确测试：TestContentHistoryCursorIsStrictAndBoundToTargetAndLimit, TestContentHistoryMergeUsesStableCrossSourceOrderAndBoundedCursor | 跨手工/导入源有界merge、并发新头记录与真实100k遍历需当前后端PG证据；浏览器完整历史旅程未跑。 |
| PERF-066 | 暂无额外同根因后台手工记录 | 32Ki字符/256KiB response budget和20MiB/200条ZIP真实PG负载由B当前证据；前端source不能证明服务器输出硬上限。 |
| PERF-068 | 暂无额外同根因后台手工记录 | 1000混合引用三query预算、授权可见性和请求上限必须合并当前后端PG证据；未真实资源picker批选E2E。 |
| TEST-023 | BUG-046, BUG-051, SEC-018 | 本代理没有把4case历史PASS当当前结果；真实PG167会话/NATS/middleware Flusher与慢消费者隔离需后端当前证据。 |
| OPS-002 | SEC-002 | 静态contract PASS不代表远端CI全绿、镜像Docker构建/发布/部署成功；root分别核权限和实际结果。 |
| OPS-008 | SEC-019, ARCH-021 | 共享OSS/DB binding/墓碑outbox/跨实例原子维护在后端，不能由React/Sharp源码或mock标题证明。 |
| STYLE-003 | 暂无额外同根因后台手工记录 | 测试覆盖迁移接口，不声称全仓所有Error.message展示已本地化；后端writeAPIError/技术日志、真实26表故障回滚由当前后端证据另核。 |
| LEGACY-016 | 原精确测试：TestLegacyMultipartOptInAndCommentReportRouteAreRemoved | 删旧HTTP路由/反滥用兼容策略、服务端权限和真实报告创建需后端当前证据；静态absence不等于所有动态入口验收。 |
| LEGACY-018 | BUG-089, SEC-036, SEC-035 | root真实live收藏夹/收藏持久化另证；后端legacy-only400且关系不变需其当前PG证据。 |

## 逐项判定索引

每行以人工语义判定为准；完整代码职责/调用链、断言、执行证据和未验证范围须同时阅读对应JSON项。历史严重度保留原评级，不按修改难度或测试数量调整。

| ID | 原严重度 | 当前人工判定 | 审查者 | 原根因当前保护摘要 |
| --- | --- | --- | --- | --- |
| BUG-001 | High | 原根因确认解决 | root | 模板生产者传key/values；收件人locale渲染并持久化快照，不能以后台key存在代替实际落库断言。 对应当前精确测试已逐条核同… |
| BUG-002 | Medium | 原根因确认解决 | backend_a | 状态绑定按account_status来源和kind撤销，manual/过期时间不受旧默认角色变更影响。 |
| BUG-003 | Medium | 原根因确认解决 | backend_b | GET只把ErrNoRows当作真正空草稿；其他读取失败不返回可保存的伪正文。 |
| BUG-004 | Medium | 部分验证 | backend_c | applyUserRoleTrack在同一Tx锁users后读manual来源快照，shift结果只替换该来源；到期时间和其他来源不被删… |
| BUG-005 | Medium | 原根因确认解决 | root | 五类创建事实以submitted_by/author_id为权威；INSERT、actor变更、审核、软删恢复及DELETE均核同一事实… |
| BUG-006 | Medium | 原根因确认解决 | root | 校准精确替换raw+retained聚合及最近时间，删除幽灵日期；二次校准幂等，不继承旧greatest漂移。 对应当前精确测试已逐条核… |
| BUG-007 | Medium | 原根因确认解决 | backend_c | Join锁当前Token并在同一Tx校验user/account/profile，Join upsert与条件last_used_at写… |
| BUG-008 | Low | 原根因确认解决 | frontend_shared | createChallenge 的唯一 INSERT 同时保存 token_hash/answer_hash/pending/nonce… |
| BUG-009 | Medium | 原根因确认解决 | database_docs | mod_content_schema.go:498-548 validates locked version scope, cycles… |
| BUG-010 | High | 原根因确认解决 | backend_b | publicRecipeSelectionCTE优先有效canonical定义，缺失时才选授权公开来源的import快照；A018补当前… |
| BUG-011 | High | 原根因确认解决 | backend_a | 未知registry生成稳定有长度界限且隔离的kind，canonical identity按registry保留。 |
| BUG-012 | High | 原根因确认解决 | backend_c | 自动canonical激活仅允许placeholder，archive状态在新观测写入时保留；人工发布拥有明确恢复入口，避免导入绕过治理… |
| BUG-013 | High | 部分验证 | backend_c | 文档合并限定revision/resource_id及key.mod，contract限定同revision；分类归档只处理当前vers… |
| BUG-014 | High | 原根因确认解决 | backend_c | import lateral先匹配requested family再排序preferred/latest，manual分支JOIN re… |
| BUG-015 | Medium | 原根因确认解决 | backend_c | entry首先由用户偏好/Accept-Language选locale，只有显式合法query值才覆盖；同primary传递知识页、本地… |
| BUG-016 | Medium | 原根因确认解决 | backend_c | revision锁内建立前后audit fact，approve/reject审计写与status/is_active、内容同步在同Tx… |
| BUG-017 | Medium | 原根因确认解决 | backend_c | 同步在读取任何detail/section/placement前获取version advisory Tx锁并锁active版本，ord… |
| BUG-018 | High | 原根因确认解决 | backend_c | import投影记录namespace/kind/revision/provenance；reconcile仅本scope import… |
| BUG-019 | High | 原根因确认解决 | backend_c | 重试/恢复都在同Tx queued状态与每次attempt的可靠Outbox事件绑定；重复delivery只ACK lease-lost… |
| BUG-020 | Medium | 原根因确认解决 | backend_b | loader文本用token边界识别，NeoForge不因包含forge而变成两个loader。 |
| BUG-021 | High | 原根因确认解决 | backend_c | 完整UNION queue先进行viewer权限visible，再filters/page，同时单语句total/facets，不再先截… |
| BUG-022 | High | 原根因确认解决 | backend_c | 精确project scope与每条pending submitted_by决定history/compare visibility；b… |
| BUG-023 | Medium | 原根因确认解决 | backend_b | 审核响应在事务内完成必要读回；读回失败必须回滚审核、公开资料、审计和通知。 |
| BUG-024 | High | 原根因确认解决 | root | publish与删除使用同一引用/模板锁；已有或archived引用拒绝删除，disabled仍可解释旧详情。 对应当前精确测试已逐条核… |
| BUG-025 | High | 原根因确认解决 | backend_c | section创建只空root，原edit PUT入口已删除；发布锁同publicID+modID+预留version的pending行… |
| BUG-026 | Medium | 原根因确认解决 | backend_c | 旧section edit按owner_mod_id搬资源的循环已删，layout通过mod_resource_bindings批量解析… |
| BUG-027 | High | 原根因确认解决 | backend_b | rows.Scan与terminal rows.Err均传播；领取扫描失败不能保留processing事实。 |
| BUG-028 | High | 原根因确认解决 | backend_b | locked differential sync保留既有绑定行身份、隐藏/审核状态和审计来源，新增/移除才写。 |
| BUG-029 | Medium | 原根因确认解决 | backend_c | 分类membership由catalogpolicy唯一注册表生成，create/revision/import规范化共用合法16类及a… |
| BUG-030 | High | 原根因确认解决 | backend_c | 客户端DTO仅ID/version；严格decode在DB前拒source/confidence，trusted探测证据只服务端生成；m… |
| BUG-031 | Medium | 部分验证 | frontend_shared | 当前多来源evidence模型与已读集成断言区分完整快照替换、manual保留和不完整只补充；原历史并集根因有准确回归对象。 |
| BUG-032 | High | 原根因确认解决 | backend_b | project file只有匹配生命周期及clean/trusted真实对象才可发布；隔离/移除触发一致撤回。 |
| BUG-033 | Medium | 原根因确认解决 | backend_b | 兼容版本写入和审核时发布均以实际Minecraft catalog闭集为准，混合未知版本不能部分发表。 |
| BUG-034 | High | 原根因确认解决 | backend_b | 人工override只来自批准后的真实人工revision；待审/拒绝不能阻断上游更新。 |
| BUG-035 | High | 原根因确认解决 | backend_c | clean镜像提升在同Tx锁mirror/项目审核、active publication_generation=1文件和download… |
| BUG-036 | Medium | 原根因确认解决 | backend_b | 新增关注只允许当前公开项目；删除本人旧关注独立于后续目标可见性。 |
| BUG-037 | Low | 原根因确认解决 | backend_a | 事件保存稳定section code，通知按接收者语言转换已知alias并对未知code回退。 |
| BUG-038 | High | 原根因确认解决 | backend_b | 镜像scan生命周期不能在rejected上卡成成功；重扫恢复根据活跃对象状态条件写入。 |
| BUG-039 | High | 原根因确认解决 | backend_c | 维护worker从精确published snapshot只改officialStatus，走revision/apply/review… |
| BUG-040 | High | 原根因确认解决 | backend_a | 翻译从真实默认语言取name/summary/body，标准localizations写入draft并在提交时持久化，既有locale不… |
| BUG-041 | Medium | 原根因确认解决 | database_docs | upsertSeedCrawlerCandidate writes both run FKs on first insert and o… |
| BUG-042 | High | 原根因确认解决 | backend_c | draft/candidate阶段同Tx，自动submit通过事务context hook把项目、external source、dra… |
| BUG-043 | Medium | 原根因确认解决 | backend_b | 通知入队与worker只支持实际站点8种canonical语言，预存不支持缓存不能绕过语言检查。 |
| BUG-044 | Medium | 原根因确认解决 | backend_a | 完整结果校验及业务持久化先于completed；错误失败并保留原结果，迟到执行需started_at代次。 |
| BUG-045 | Medium | 原根因确认解决 | backend_b | after锚点必须属于真实当前会话；无效锚点不能推进read_at。 |
| BUG-046 | Medium | 原根因确认解决 | backend_c | 每Server稳定随机origin，本地投递后NATS envelope带origin，订阅只丢自身非空origin回声；legacy无… |
| BUG-047 | High | 部分验证 | backend_c | selectConversation同步失效旧request代次、abort并更新conversation ref/清历史状态；初始/历… |
| BUG-048 | Medium | 部分验证 | frontend_shared | 通知余额effect按translationAllowed+当前token无snapshot加载，cleanup取消，queued结束刷… |
| BUG-049 | Low | 原根因确认解决 | frontend_pages | 公开配置→2秒限时读取→normalizeMetadataSiteName→根generateMetadata.default/temp… |
| BUG-050 | High | 原根因确认解决 | root | 显式Enabled=false在Server和Worker两Mailer入口生效；旧测试名称过时，改核实际PersistedMailDi… |
| BUG-051 | Medium | 原根因确认解决 | backend_b | 记录器保留SSE及Flusher/Hijacker/Pusher/ReaderFrom/Unwrap能力，不能把stream缓冲到请求结… |
| BUG-052 | Medium | 原根因确认解决 | root | runtime周期启动、持久策略、批次/轮数/时间预算与跨会话lease共同保护自动清理；不是保存配置即清理。 对应当前精确测试已逐条核… |
| BUG-053 | Medium | 原根因确认解决 | root | 读错误返回500；手动清理记录成功部分和失败类别；策略保存不伪装全量清理成功。 对应当前精确测试已逐条核同一根因及实际断言；root此前… |
| BUG-054 | Medium | 原根因确认解决 | root | slog与legacy bridge进入同一有限Store，结构化level和timestamp权威，含error属性不错误升级INFO… |
| BUG-055 | Medium | 原根因确认解决 | database_docs | Connect and ConnectActivity share connectPool; RuntimeParams timezon… |
| BUG-056 | High | 原根因确认解决 | backend_b | 完整可靠性字段校验；配置更新须成功staged runtime之后替换；省略凭据保留，显式clear才清除。 |
| BUG-057 | Medium | 原根因确认解决 | database_docs | content_target_user_is_developer is an exists query over every effec… |
| BUG-058 | High | 原根因确认解决 | root | 完整读当前独占/共享锁并发回归：持rebuild锁250ms期间worker B未增加attempt且Typesense删除为0，释放后… |
| BUG-059 | High | 原根因确认解决 | root | 等级重算仅替换level_track来源；manual绑定保留，权限去重不删合法来源。 对应当前精确测试已逐条核同一根因及实际断言；ro… |
| BUG-060 | High | 原根因确认解决 | backend_c | active任务在任何delta/reward前严格解码condition/rewards并批量验证货币active，rewarded_… |
| BUG-061 | Low | 原根因确认解决 | backend_a | 合法正q才启用gzip，显式gzip覆盖wildcard，非法或明确拒绝不会被wildcard反转。 |
| BUG-062 | High | 部分验证 | frontend_pages | 工作区从每个loader的已验证versions复制候选范围，不将所有Minecraft版本分发给每个loader。 |
| BUG-063 | Medium | 原根因确认解决 | backend_b | NeoForge现代21/26版本与旧1.20.1映射分别有明确prefix，stable优先且排序稳定。 |
| BUG-064 | Medium | 原根因确认解决 | backend_b | loader code canonical且大小写不敏感唯一；坏设置不是默认空列表。 |
| BUG-065 | Medium | 原根因确认解决 | frontend_shared | 真实PG回归把preflight确认loader版本与随后变动的目录隔离，任务仍消费原preview，二次409，摘要篡改拒绝；这与服务… |
| BUG-066 | High | 原根因确认解决 | backend_a | 递归UNION终止循环；输出node/edge sentinel上限，文件解析有界批量；必需依赖失败不能通过确认掩盖。 |
| BUG-067 | Medium | 原根因确认解决 | backend_b | 导出预览按三种实际favorite目标选择正确title/name列，而非对modpack使用不存在字段。 |
| BUG-068 | High | 原根因确认解决 | backend_c | task collection可空SETNULL及immutable publicID snapshot，history/detail不… |
| BUG-069 | High | 原根因确认解决 | root | 任务与报告六项计数相互一致，序列化索引数再次验证；任何漂移在上传前失败。 对应当前精确测试已逐条核同一根因及实际断言；root此前完整语… |
| BUG-070 | Medium | 原根因确认解决 | backend_b | MRPack条目跨平台规范后唯一；大小写/NFKC/路径碰撞失败明确而不产出覆盖包。 |
| BUG-071 | Medium | 原根因确认解决 | root | original重建只读不可变报告且重新鉴权/预检；current来源已删返回409，兼容性确认保留。 对应当前精确测试已逐条核同一根因… |
| BUG-072 | Medium | 原根因确认解决 | backend_b | 导出Minecraft版本有长度/字符闭集，并且必须存在于enabled实际版本与loader关联。 |
| BUG-073 | Medium | 原根因确认解决 | backend_c | 外部provider无确定source locale用und草稿，正式提交要求用户显式选支持的editable语言，不假装英语或简中。 |
| BUG-074 | Medium | 原根因确认解决 | backend_c | 档案选择release/primary/available且过滤server/child/alternative，使用时间/稳定ID排序… |
| BUG-075 | Medium | 原根因确认解决 | backend_a | normalize失败才改变主蓝图可用性，选配转换耗尽重试保留已ready源蓝图。 |
| BUG-076 | Medium | 原根因确认解决 | root | 未完整映射实体的编码/解码拒绝有损成功；首attempt永久失败，不生成失真variant。 对应当前精确测试已逐条核同一根因及实际断言… |
| BUG-077 | Low | 本次修复 | backend_c | 本轮B013补齐已有blueprint cover绑定授权：编辑人不是旧文件uploader仍可保留同cover，Tx锁活跃/clean… |
| BUG-078 | Medium | 原根因确认解决 | backend_a | 失败蓝图行锁内条件重置和job/outbox共事务，活动任务不重复创建。 |
| BUG-079 | Low | 原根因确认解决 | root | 精确上传期限与owner锁清理uploading主体/投影；完成主体不清理，旧三测试名称需按当前实际回归替换。 对应当前精确测试已逐条核… |
| BUG-080 | Medium | 原根因确认解决 | root | OSS文件与举报证据同事务登记；重试完整身份比对，不把文件ID当证据恢复成功。 对应当前精确测试已逐条核同一根因及实际断言；root此前… |
| BUG-081 | Medium | 原根因确认解决 | backend_b | 完成删除记录重放不重复处理；相同key新活跃血缘再次tombstone后可重建pending而非被旧completed吞掉。 |
| BUG-082 | Medium | 原根因确认解决 | root | 评论、processing附件、唯一job与Outbox同事务；租约/重试/恢复复用已有分享，终态与绑定同事务。 对应当前精确测试已逐条… |
| BUG-083 | Medium | 原根因确认解决 | backend_c | 评论删除同Tx清正文/解绑附件与日志任务，独立log_share保留；任何解绑/commit失败不留下半删除。 |
| BUG-084 | Medium | 原根因确认解决 | frontend_shared | 已核准确PG断言：stale版本409且正文不改、missing版本400、当前版本成功200并单调推进；FE请求携带渲染时baseUp… |
| BUG-085 | Low | 原根因确认解决 | frontend_shared | 创建与回复能力同一target-owner-block resolver；准确PG断言屏蔽作者目标、CanReply与创建一致，解除关系… |
| BUG-086 | Low | 原根因确认解决 | backend_c | skinAssetJSON统一接受claims，以owner或isSkinAdmin决定canEdit，所有目录/详情/衣柜调用同ser… |
| BUG-087 | High | 原根因确认解决 | root | skin create及never-published重提遵循create审核策略；只有明确免审权限旁路，pending不公开。 对应当… |
| BUG-088 | Medium | 原根因确认解决 | root | 共享Blob系统归属、fresh key、统一锁序与active引用投影；最后引用删除有持久补偿。 对应当前精确测试已逐条核同一根因及实… |
| BUG-089 | Medium | 原根因确认解决 | root | 全部collection归属先验证再删旧关系；错误输入不清空，重复ID去重，显式空集合允许清理。 对应当前精确测试已逐条核同一根因及实际… |
| BUG-090 | High | 原根因确认解决 | backend_c | 详情published revision token，PUT同aggregate advisory lock+post FORUPDAT… |
| BUG-091 | Medium | 原根因确认解决 | frontend_shared | 显式八语言sourceLocale规范化后成为权威，fr_fr保留为fr-FR，未知语言拒绝；缺省低置信Latin返回需确认。真实PGc… |
| BUG-092 | Medium | 原根因确认解决 | backend_a | queued表示数据库任务和可靠outbox同时提交，不依赖即时NATS在线；死publisher现可显式受权限恢复。 |
| BUG-093 | Medium | 原根因确认解决 | backend_a | HTTP语言range按合法q权重选择稳定tie，跳过q0、wildcard、损坏参数；显式设置优先级保持。 |
| BUG-094 | Medium | 原根因确认解决 | frontend_shared | 移除从未执行的AI task concurrency字段；当前subscribeTaskOn以task.MaxConcurrent建立信… |
| BUG-095 | Medium | 原根因确认解决 | backend_b | nextHeatPromotionSequenceTx用PK route的UPSERT原子递增，免select max+1；计数与inv… |
| BUG-096 | Medium | 原根因确认解决 | root | 货币启用规则、商品引用和配置在同一economy事务锁下重读校验；disabled货币不能被新启用规则引用。 对应当前精确测试已逐条核同… |
| BUG-097 | Medium | 原根因确认解决 | backend_b | 商店item_type闭集与实际可执行use分支一致，不能卖已接受但无执行路径的物品。 |
| BUG-098 | Medium | 原根因确认解决 | backend_c | 一图一sticker DBunique，旧/新image按ID顺序FORUPDATE并重核active；归档再次确认无sticker/其… |
| BUG-099 | Medium | 原根因确认解决 | database_docs | stickerReferenceSchemaStatements discovers every Markdown/body/histo… |
| BUG-100 | Medium | 原根因确认解决 | root | 表情父行锁及结构化引用advisory锁统一；引用检查与删除同事务，独占且未引用图片才归档。 对应当前精确测试已逐条核同一根因及实际断言… |
| BUG-101 | Medium | 原根因确认解决 | frontend_shared | 直接执行ExpiringPromiseCache真实异步行为：有限窗口内合并、到期重载、显式失效立即重载、失败可重试；当前成功mutat… |
| BUG-102 | Medium | 原根因确认解决 | backend_c | admin locale独立严格validator只接受站内8语言，alias规范化后非法在读写/parent mutation前拒；公… |
| BUG-103 | Medium | 原根因确认解决 | frontend_shared | 准确PG断言FR草稿不撤下ZH/EN，FR发布与ZH草稿隔离，公开fallback只选published，draft-only404/列… |
| BUG-104 | Medium | 原根因确认解决 | frontend_shared | About baseRevision与changelog baseUpdatedAt各自CAS；PG回归首次保存成功、陈旧409且内容不… |
| BUG-105 | Medium | 原根因确认解决 | frontend_shared | 已核当前准确七producer测试的初始/改名/重绑断言，投影保留可路由source ID/name；FE未知来源保留诊断字段而不是伪造… |
| BUG-106 | Medium | 原根因确认解决 | frontend_shared | 类型权威API覆盖project闭集和全部resource_kinds，含隐藏/扩展；128边界接受、129拒绝，FE只从后端目录构建筛… |
| BUG-107 | Medium | 部分验证 | frontend_shared | 同query也递增requestVersion，reset cursor/history，effect依代次+AbortSignal收口… |
| BUG-108 | Medium | 原根因确认解决 | frontend_shared | 准确PG断言先取cursor后并发全删返回items空/hasMore=false/nextCursor空，不虚构total；FE Pr… |
| BUG-109 | High | 原根因确认解决 | frontend_shared | PG对submitter/editor/developer去重通知与直接author进行区分，targetActor role明确，ca… |
| BUG-110 | High | 原根因确认解决 | backend_b | 无效举报结论禁止hide/ban/warning等惩罚动作，验证在打开业务事务前。 |
| BUG-111 | High | 原根因确认解决 | backend_b | 临时封禁结束至少在未来一分钟；elapsed请求不得开TX，持久层也有CHECK。 |
| BUG-112 | Medium | 原根因确认解决 | backend_c | 原领取责任锁已解决：claim状态和assignment同Tx，resolve要求in_review+当前claimed_by并锁rep… |
| BUG-113 | Medium | 原根因确认解决 | frontend_shared | 真实PG handler创建modpack报告201、认领200、解决200，落库目标rejected且一次action、snapsho… |
| BUG-114 | Medium | 部分验证 | frontend_shared | 真实pure helper覆盖request generation/abort/请求locale/响应locale与保存锁；实际Abou… |
| BUG-115 | Medium | 原根因确认解决 | frontend_shared | 真实changelogDraftForLocale只取精确译文、缺失为空；语言草稿纯状态往返测试覆盖ZH/EN各自输入，面板共用该边界。 |
| BUG-116 | Medium | 原根因确认解决 | frontend_shared | datetimeLocalToRFC3339真实测试空值null、当前时区墙钟roundtrip、无效日历/越界/带Z拒绝，表单接线保证… |
| BUG-117 | Low | 部分验证 | frontend_shared | create在await前捕获formElement，后续reset不再访问React event；请求失败return在成功reset… |
| BUG-118 | Medium | 部分验证 | frontend_shared | report详情必须最新代次、未abort、selected ID与response ID一致；clear取消，claim/resolv… |
| BUG-119 | Medium | 原根因确认解决 | frontend_shared | 真实batch helper执行成功1→失败2→成功3，即时commit成功ID并继续；Report接线保存可见成功项，错误单项渲染。 |
| BUG-120 | Medium | 原根因确认解决 | backend_b | 完成草稿只能有一个真实authority，由当前用户拥有的change_request/server数据库行决定status。 |
| BUG-121 | Medium | 原根因确认解决 | backend_c | StructuredValues只读取provider结构category/loader/version字段，Name/Summary/… |
| BUG-122 | High | 部分验证 | backend_c | 角色写入统一advisory mutation锁并校验继承图缺失父节点/环，删除计算全局授权依赖blockers；create/upda… |
| BUG-123 | Low | 原根因确认解决 | frontend_shared | UI单一注册表+Cookie权威驱动根lang与SSR快照，真实8语言规范化/cookie roundtrip测试，实际renderer… |
| BUG-124 | Low | 本次修复 | frontend_pages | 短链接404/410→not_found，网络/解析/其它状态→error+Retry；每次AbortController，导航前校验s… |
| BUG-125 | Low | 原根因确认解决 | frontend_shared | 实际parser明确四review状态、五provenance，未知/空locale/mutation缺状态拒绝，不再猜approved… |
| BUG-126 | Medium | 部分验证 | backend_a | FS已完整读当前155行：lastSaved仅成功提交/恢复后更新，保存中修改有界追写，A→B→A不再用最初基线跳过。 |
| BUG-127 | Medium | 部分验证 | frontend_pages | 关注列表40条cursor→loadMore，AbortController+单调requestGeneration隔离筛选轮次，typ… |
| BUG-128 | Low | 原根因确认解决 | frontend_shared | 实际mute helper测试1h/7d精确deadline、forever/expired/invalid，控件不再把任意非空dead… |
| BUG-129 | Low | 原根因确认解决 | frontend_shared | 实际disabled-aware toggle只增删mutable成员，保留禁用已选与组外顺序；单项和压缩组均使用同一边界。 |
| BUG-130 | Medium | 部分验证 | frontend_shared | 当前准确PG测试遍历11种SQL sort两页无重复、跨filter cursor400、100k计划用index；前端loader代次… |
| BUG-131 | Medium | 原根因确认解决 | backend_c | creator资料编辑/成员管理/职务创建分别计算能力，content.review不授编辑；profile载荷非nil members… |
| BUG-132 | High | 原根因确认解决 | backend_b | authorship审核keyset绑定status/权限scope，每页最多100+sentinel，不能因旧200窗口遗漏。 |
| BUG-133 | Medium | 原根因确认解决 | frontend_shared | 实际classifier把404与401/410/500/网络分开，保留error消息，三个detail共享abort/retry状态。 |
| BUG-134 | Medium | 原根因确认解决 | frontend_shared | 实际review presentation闭集四态，withdrawn/大小写/null/number进入protocolError；两… |
| BUG-135 | Medium | 原根因确认解决 | frontend_shared | 真实tab解析闭集，构造URL保留其他query/hash，default规范化；三个详情以URL取tab，不维护第二事实。 |
| BUG-136 | Medium | 部分验证 | frontend_shared | 已核准确PG回归skin/blueprint主体+localizations同一Tx，强制本地化失败保留旧主体及2语言。前端原多PUT已… |
| BUG-137 | Medium | 原根因确认解决 | root | 后端六个真实上界/信任等级边界与未知action、阈值顺序拒绝均实际PASS；前端default-shaped、global依赖、秒/分… |
| BUG-138 | Medium | 部分验证 | backend_c | 状态联合携带projectId/days；请求和响应双重匹配后ready，切换/失败清掉旧detail，防前一项目资料闪回。 |
| BUG-139 | High | 部分验证 | frontend_pages | 审核队列页摘要与点击后详情DTO分开，50条cursor继续加载，状态重载取消旧轮次，稳定ID去重。 |
| BUG-140 | Medium | 原根因确认解决 | backend_c | 临时sticker-upload UUID代次/来源闭集，own uploader+sticker.manage/upload才能dis… |
| BUG-141 | Medium | 原根因确认解决 | backend_a | 目录limit+1和scope keyset绑定q/sort/order/limit/可见性身份，可遍历超过原60项窗口。 |
| BUG-142 | Medium | 部分验证 | frontend_pages | 先将全批分类，再逐项上传并保存uploadedFileId；创建分享失败保留ID，ready项目不重复上传，207逐项状态不抛弃成功兄弟… |
| BUG-143 | Medium | 原根因确认解决 | root | 真实新schema的Mod、Modpack及六SimpleProject各3行，以limit1对比total3→2并检查两页排除与独立I… |
| BUG-144 | High | 部分验证 | frontend_pages | 编辑器加载definition，经structuredClone保留任意嵌套JSON，普通变更仍发送完整draft.definition… |
| BUG-145 | Medium | 原根因确认解决 | backend_c | 资料响应统一capability集合直接派生canEditMod，写端requireEditableMod独立失败关闭，响应privat… |
| BUG-146 | Medium | 原根因确认解决 | frontend_pages | 共享逐文件pending/invalid/uploading/uploaded/failed；成功ID即时写当前草稿并去重；重试只发失败… |
| BUG-147 | Medium | 部分验证 | frontend_pages | 静态loader/category/feature/license闭集与独立parent facets分页分开；selected独立回显… |
| BUG-148 | Medium | 部分验证 | frontend_pages | 候选parent与实际拖拽/下拉统一reparentPolicy：父深度+1+子树高≤4，排除自己/后代/不存在父项，畸形树拒绝。 |
| BUG-149 | Medium | 部分验证 | frontend_pages | 任务奖励rename对已占用规范化币种返回null，不覆盖旧amount；选项禁重复，提交前规范化唯一性复核。 |
| BUG-150 | High | 本次修复 | frontend_pages | 每个embedded文档显式documentId；受控同步区分parent echo/clean加载/dirty冲突；OCT02-FP-… |
| BUG-151 | Medium | 部分验证 | frontend_pages | 保存协调器保留baseRevision和递增clientSequence；Abort旧请求，当前正文一致且latest才显示已保存；冲突… |
| SEC-001 | Medium | 原根因确认解决 | frontend_shared | 实际normalize默认disabled且固定/plantuml，外部存量failclosed、设置外部URL在持久化前400；ren… |
| SEC-002 | Medium | 部分验证 | backend_c | 发布门固定scanner/工具链，push/PR/schedule触发，扫描报告先archive后按退出码enforce，Actions… |
| SEC-003 | Medium | 原根因确认解决 | root | 多副本要求Redis与fail-closed，运行故障不进local map；单副本保留有界fallback安全边界。 对应当前精确测试… |
| SEC-004 | Medium | 原根因确认解决 | backend_a | 安全version发布失败不被忽略，已提交变更回503且committed标识；退出共享tombstone挡住inflight sess… |
| SEC-005 | Medium | 原根因确认解决 | backend_b | 公开贡献只用当前匿名可读目标和当前标题；无route/不支持/private/unlisted/删除不曝历史快照。 |
| SEC-006 | High | 原根因确认解决 | backend_a | 外来role只精确匹配owner/developer/maintainer白名单，其他归contributor，不依赖substring… |
| SEC-007 | High | 原根因确认解决 | root | 精确diff编辑距离固定1024/frontier固定；大改写线性前后缀估算，调用入口另有正文限额。 对应当前精确测试已逐条核同一根因及… |
| SEC-008 | Medium | 原根因确认解决 | root | 有效限制先匹配action/全局再聚合最强hard与独立软限制；缓存按action隔离，不能只取最新一条。 对应当前精确测试已逐条核同一… |
| SEC-009 | High | 原根因确认解决 | backend_c | package来源以archive_file_id唯一而非客户端SHA跨用户去重，Tx FORKEYSHARE重读active owne… |
| SEC-010 | High | 原根因确认解决 | root | 实际50k×50k PNG头被拒，进程2槽第三调用deadline失败，正常decode/render尺寸PASS；root已完整审查8… |
| SEC-011 | High | 原根因确认解决 | backend_b | 供应商credential绑定完整origin；downgrade、host/port变化和userinfo redirect在转发前拒… |
| SEC-012 | Medium | 原根因确认解决 | backend_b | 复用外部图片必须精确purpose/category/source/owner和clean/live对象，不能凭hash授予foreig… |
| SEC-013 | High | 部分验证 | frontend_shared | normalize直接丢弃incoming派生关系，outgoing成为唯一权威；ModEditor最终完整review确认提交不带入向… |
| SEC-014 | High | 原根因确认解决 | root | Configuration计数受剩余字节和共享解析预算约束；多packet共享预算，探测全进程4槽。 对应当前精确测试已逐条核同一根因及… |
| SEC-015 | High | 原根因确认解决 | backend_b | GET仅返回配置预览不能启用自动更新；只有configure受权PUT才更改配置。 |
| SEC-016 | High | 原根因确认解决 | backend_a | 完整提示输入+maxoutput预留；seed按日事务锁将已用+reservation合计，超预算不发请求，未知用量不视免费。 |
| SEC-017 | High | 未验证 | frontend_pages | SitePresence仅周期心跳/hidden不发、cleanup timer+监听、server-issued visitorId … |
| SEC-018 | High | 原根因确认解决 | root | 本地total/user/session限额、释放后复用和续租实际断言；真实mounted HTTP分别local与miniredis … |
| SEC-019 | High | 原根因确认解决 | frontend_shared | 真实Sharp测试限格式/尺寸/帧/累计解码，动画首帧化、元数据剥离、512边，chunked超界cancel；授权探测与hash se… |
| SEC-020 | High | 原根因确认解决 | backend_b | access log只保存queryPresent等typed摘要，不记录OAuth query值或秘密式key。 |
| SEC-021 | High | 原根因确认解决 | database_docs | seedDefaultUsers uses users table lock and durable bootstrap/legacy … |
| SEC-022 | Medium | 原根因确认解决 | backend_a | 公开metrics不查询或序列化最近浏览者标识，保留聚合数字。 |
| SEC-023 | High | 原根因确认解决 | backend_c | 指标只接受detail或属于目标resource的active版本page；匿名identity/source基于trustedIP H… |
| SEC-024 | High | 原根因确认解决 | backend_c | 创建Tx持user namespaced advisory lock贯穿quota聚合/preview/task/items/Outbo… |
| SEC-025 | High | 原根因确认解决 | backend_b | 仅官方consistent CDN路径可绑定Modrinth身份；第三方URL不能伪造project/version/hash关联。 |
| SEC-026 | High | 原根因确认解决 | backend_a | palette按entry数有界分配，索引必须dense唯一非负，不可按攻击者最大index分配。 |
| SEC-027 | High | 原根因确认解决 | backend_a | 共享PG事务advisory lock原子检查全站16和用户4，去重及job/outbox同事务；唯一冲突仅认活动operation约束… |
| SEC-028 | High | 原根因确认解决 | backend_b | quota admission/settlement同actor事务锁；fail closed且计数真实写入后生效，不能并发超额。 |
| SEC-029 | High | 原根因确认解决 | backend_b | 私有下载只有对象绑定短期SDK签名，legacy ESA参数不能成为永久公共URL，TTL最大60。 |
| SEC-030 | High | 原根因确认解决 | backend_b | GIF验证所有frame且解码前检查pixel/frame/duration累积预算，不能仅首帧可读便接受截断/高成本动画。 |
| SEC-031 | High | 部分验证 | frontend_shared | 准确visibility回归断言隐藏pending目标的replies/reaction/watch/edit/pin均404且未写，o… |
| SEC-032 | High | 原根因确认解决 | backend_c | ensureCommentWatch同Tx按user quota锁，existing active先返回，active count有20… |
| SEC-033 | High | 原根因确认解决 | backend_c | thread只取就近最多16祖先+focus+直接reply，keyset分页最多64节点；真实apiResponse编码512KiB超… |
| SEC-034 | Medium | 原根因确认解决 | backend_c | 纹理装配只返回owner或active approved非private资产；private资产只能装备private档案，档案转pub… |
| SEC-035 | High | 本次修复 | backend_b | create/add/read/export继续当前viewer可见性；删除本人已存在隐藏membership必须独立证明所有权，不能因… |
| SEC-036 | Medium | 原根因确认解决 | root | 收藏增长统一user锁，RepeatableRead在等待锁后建立快照；净增长限额允许幂等迁移和超额清理。 对应当前精确测试已逐条核同一… |
| SEC-037 | High | 本次修复 | backend_b | 帖子引用按当前viewer的目标可读性筛选；import-only catalog资源不能因全局active绕过来源未审核状态。 |
| SEC-038 | High | 原根因确认解决 | backend_a | 公开GET仅读取已批准内容和回退；显式POST要求正actor/正额度并包含源版本/目标/类型/actor并发键。 |
| SEC-039 | High | 原根因确认解决 | backend_b | 税费ceil、金额乘法与余额范围用checked arithmetic；转账守恒且失败不写ledger。 |
| SEC-040 | High | 原根因确认解决 | backend_b | 切换/清空track删除全部旧level_track来源，保留manual来源，不能积累过期level权限。 |
| SEC-041 | Medium | 原根因确认解决 | backend_c | PNG/GIF真正解码后重新编码，公开只sticker_derived/trusted_generated；原临时upload不公开并绑… |
| SEC-042 | High | 原根因确认解决 | backend_b | 举报目标读取复用对象授权，snapshot在同repeatable-read事务，公开comment仍需其私有父可读。 |
| SEC-043 | High | 原根因确认解决 | frontend_shared | 实际预算helper边界/替换差额，PG expired回收与active/completed配额，同user advisory loc… |
| SEC-044 | High | 原根因确认解决 | backend_a | manual替换保留expiry，仅接受合法manual来源/允许的group，系统来源不被客户端覆盖。 |
| SEC-045 | Low | 原根因确认解决 | frontend_shared | 真实CSP builder拒绝凭据/path/query/wildcard/insecure origin；生产无宽泛https:且no… |
| SEC-046 | Medium | 部分验证 | backend_c | 前端存储key含subject，切号无法读取他人upload/job；后端create/resume/active/detail own… |
| SEC-047 | Medium | 原根因确认解决 | frontend_shared | 真实URL parser只精确at.alicdn.com/t/c/font_*.js与单SHA384SRI，lookalike/quer… |
| PERF-001 | Medium | 原根因确认解决 | backend_b | editor application列表和attachments批量读，SQL条数不随申请数线性增长；先结束rows再额外读取。 |
| PERF-002 | Medium | 原根因确认解决 | backend_b | showcase按目标用户effective access先过滤七类公开项目，避免全库UNION和角色重复。 |
| PERF-003 | Medium | 部分验证 | frontend_shared | 当前UserNetworkList保留cursorHistory，public编码scope cursor、blocked独立page；… |
| PERF-004 | High | 原根因确认解决 | root | 1024队列、固定2worker、正文20k和deadline/close预算；overflow可观测丢派生事实而非无限goroutin… |
| PERF-005 | Medium | 原根因确认解决 | backend_a | 单MATERIALIZED candidates一次扫描做count/分组/样本，cap100001并拒无实质边界筛选，保留显式over… |
| PERF-006 | Medium | 原根因确认解决 | backend_b | catalyst按canonical与import两批ANY查询映射，不能每个type一个query。 |
| PERF-007 | High | 原根因确认解决 | backend_c | 公共catalog版本常量singleton读替代revision聚合；写入Tx统一bump与内容rollback一致。本轮A018/r… |
| PERF-008 | Medium | 原根因确认解决 | backend_c | recipe binding/slot/candidate/resource/importsnapshot一个有序JOIN后折叠，不再逐… |
| PERF-009 | Medium | 原根因确认解决 | backend_c | template与importslot单LEFTJOIN读完关Rows再写promotions，避免每模板一次读取和持游标池重入。 |
| PERF-010 | High | 未验证 | frontend_pages | 导出tag相关前端adapter传递受服务端解析的游标与筛选；读取模块未据旧all=1声明全集可用。 |
| PERF-011 | High | 原根因确认解决 | frontend_shared | 当前精确PG asset分页断言同path不同source_order在完整模式都保留，pathsOnly去重，page≤limit；1… |
| PERF-012 | High | 原根因确认解决 | backend_c | 可见性/过滤/精确total/facets留在数据库完整集合，分页有界结果；基础扫描成本通过真实query计划限制，不虚构无COUNT。 |
| PERF-013 | Medium | 原根因确认解决 | backend_c | mod history cursor绑定project+limit，revision_no unique降序limit+1；submit… |
| PERF-014 | Medium | 原根因确认解决 | backend_b | mod relationship groups一次ordered LEFT JOIN读，保留空group与稳定item顺序，避免grou… |
| PERF-015 | High | 原根因确认解决 | backend_c | section resource keyset scope+limit+1，graph/layout窄流独立，stored search… |
| PERF-016 | Medium | 原根因确认解决 | backend_c | unresolved同步先收集两类候选，通过两个unnest批次解析canonical/alias/registry及批量写，folde… |
| PERF-017 | Medium | 原根因确认解决 | backend_c | layout IDs单批生成，分类ordered unnest upsert/localization集合替换/system归档恢复，稳… |
| PERF-018 | High | 原根因确认解决 | frontend_shared | 已完整读card scanner/测试，实际PG100最坏嵌套卡片仍有界且detail保留1MiB正文；超2MiB编码在写响应前失败，单… |
| PERF-019 | Medium | 原根因确认解决 | backend_b | creator作者最多64，单batch差异同步且副作用coalesced；lookup带精确subject scope索引。 |
| PERF-020 | Medium | 原根因确认解决 | backend_c | approved create先revision再apply完整关联一次，只有pending create先preview associ… |
| PERF-021 | Medium | 原根因确认解决 | backend_c | Typesense组合过滤+stable cursor，SQLfallback tuple keyset+单一GINtext；索引结果只… |
| PERF-022 | Medium | 原根因确认解决 | backend_c | list只读窄摘要和三类关联count单query，完整body/proof/link/mod通过server.review保护的按需d… |
| PERF-023 | High | 部分验证 | frontend_shared | 完整read项目文件分页源码与tests，source绑定cursor/limit1..50、每来源独立续页，前端只信clean/tru… |
| PERF-024 | High | 原根因确认解决 | backend_b | 条目分页keyset+lookahead，摘要4k而非所有正文，多语言有8个有限locale。 |
| PERF-025 | Medium | 原根因确认解决 | backend_b | notification_count按本批实际insert delta原子加，避免每200收件人重新count已有通知。 |
| PERF-026 | Medium | 原根因确认解决 | backend_b | provider文件流式spool并同时计算所有hash，内存不按完整256MiB复制；全局slot跨pool。 |
| PERF-027 | Medium | 原根因确认解决 | backend_a | 候选/运行列表keyset绑定filter且不投影大payload，详情另读单条。 |
| PERF-028 | High | 原根因确认解决 | backend_b | direct+broadcast各有界keyset合并；read watermark不为整库批量改通知，scope绑定user/kind… |
| PERF-029 | High | 原根因确认解决 | frontend_shared | 实际querycache轮转只取live served候选，过期剔除；精确PG1000万广播断言每轮1SQL/16派生且truth正确、… |
| PERF-030 | High | 原根因确认解决 | frontend_shared | message cursor真实pure scope拒绝与PG百万消息有界页/无重复/chronological/2SQL断言，afte… |
| PERF-031 | Medium | 原根因确认解决 | frontend_pages | 一份realtimeQueryCoordinator按user/conversation/kind精确失效，微任务合并事件，多订阅共享i… |
| PERF-032 | High | 原根因确认解决 | backend_a | handler后nonblocking捕获，有界1024队/128batch/100ms，后台1秒写超时及显式采样/丢弃指标。 |
| PERF-033 | High | 原根因确认解决 | frontend_shared | 完整log分页tests/currenthandlers已读；scope含全部filters，limit+1，cleanup每来源100… |
| PERF-034 | High | 原根因确认解决 | database_docs | refresh_content_popularity reads bounded per-route lifetime/view/dim… |
| PERF-035 | High | 原根因确认解决 | root | source guard禁止普通Worker global评分扫描；写维护schema合同与真实10万/百万/千万集合rebuild三规… |
| PERF-036 | Medium | 部分验证 | frontend_pages | 后台项目工作台仅消费scope cursor/hasMore，维护前后页cursor历史，不要求total/offset。 |
| PERF-037 | High | 原根因确认解决 | root | 每类型500ID keyset页、SQL预截断、64项/8KiB字段及8MiB JSONL；没有集合级全量map堆积。 对应当前精确测试… |
| PERF-038 | Medium | 原根因确认解决 | backend_b | DLQ按closedfilter稳定keyset，不全量读并内存filter；replay只当前未解决任务。 |
| PERF-039 | Medium | 原根因确认解决 | root | 上游同步有界cache/singleflight/条件验证/4路；预检与创建仅读持久artifact快照零外呼。 对应当前精确测试已逐条… |
| PERF-040 | High | 本次修复 | backend_b | 原结果node/edge上限未限制递归内部工作，rootR019已在单SQL内限制实际visited工作并保留有界输出/顺序/并发。 |
| PERF-041 | Medium | 原根因确认解决 | database_docs | owner-scoped strict cursor parser, stable created_at/id keyset limit… |
| PERF-042 | High | 原根因确认解决 | root | 服务端上传/worker/解码各入口执行源/体积/块/材质/编码硬预算；跨实例入队user锁、进程2槽。 对应当前精确测试已逐条核同一根… |
| PERF-043 | High | 原根因确认解决 | frontend_shared | 完整scope源码/测试已读；namespace数组实际去重minecraft/create，ready/partial与approve… |
| PERF-044 | High | 原根因确认解决 | frontend_shared | 准确PG并发600+600仅1成功1quota拒绝、reserved只600；settlement/rebuild与百万历史固定桶读验证… |
| PERF-045 | High | 原根因确认解决 | root | 评论/root/reply/watch为scope绑定keyset，total读计数事实并扣拉黑；不OFFSET全历史，Scan/cur… |
| PERF-046 | High | 原根因确认解决 | backend_a | 关注列表一次批组装评论及目标，stored头像真正批量DB授权后签名，scan/RowsErr/Close先于批resolver。 |
| PERF-047 | High | 原根因确认解决 | backend_c | 先读完profiles关闭Rows，再以全部publicIDs一次batchtextures+map回装；单详情同helper，隐私条件… |
| PERF-048 | High | 原根因确认解决 | backend_c | skin窄stored projection/tsvector索引、九sort tuple+ID cursor、limit+1，当前as… |
| PERF-049 | High | 部分验证 | frontend_shared | 实际parser拒未知/重复/page/跨scope，SQLkeyset+limit+1与独立窄count；5000衣柜断言首100、总… |
| PERF-050 | Medium | 原根因确认解决 | database_docs | ratingReviewPageSQL uses published target-scoped updated_at/id keyse… |
| PERF-051 | High | 原根因确认解决 | frontend_shared | 准确PG百万集合/项每page2SQL、membership100仅1SQL且bounded响应，无新head/重复；前端selecto… |
| PERF-052 | High | 原根因确认解决 | frontend_shared | 完整community projection/tests已读；PG百万页24摘要≤320，不泄SECRET-FULL-BODY，5批SQ… |
| PERF-053 | High | 原根因确认解决 | database_docs | normalizeCommunityPostReferences validates32 projects/64 resources a… |
| PERF-054 | Medium | 部分验证 | frontend_pages | 历史API有界页DTO，通用/Mod只保留当前页+opaque cursor历史，changelog有界摘要，正文点击独立详情。 |
| PERF-055 | Medium | 原根因确认解决 | backend_a | diff数量/深度/path/value预算，过长value摘要化而完整snapshot保留，unnest批次一次SQL。 |
| PERF-056 | High | 原根因确认解决 | backend_b | 耐久versioned job500keyset批次、setwise level_source替换并coalesce权限版本，配置变更使… |
| PERF-057 | Medium | 部分验证 | frontend_shared | 目录默认64/128/1024与硬clamp256/512/4096真实pure通过，sticker引用projection PG测试实… |
| PERF-058 | Medium | 部分验证 | frontend_shared | strict scope cursor与前端Previous/Next保留；百万PGtest实际1SQL/50page/无head漂移且… |
| PERF-059 | Medium | 原根因确认解决 | frontend_shared | 准确百万PG projection1query/50page/无head漂移，prefix搜索2query，过宽只1budgetprob… |
| PERF-060 | Medium | 原根因确认解决 | frontend_shared | 实际parser绑定user/status/endpoint/limit，PG百万样本相同scope keyset稳定；前端三队列使用s… |
| PERF-061 | High | 原根因确认解决 | frontend_shared | 准确PGactive3行首2+后1，completed submitted/reviewing两行，statusAt/id keyset… |
| PERF-062 | Medium | 原根因确认解决 | backend_b | 100用户权限/role根解析批量ANY，catalog不按role各查；读取故障不是可编辑空数据。 |
| PERF-063 | Medium | 部分验证 | frontend_shared | 完整hash Worker/permit/source已审，4MiB顺序slice、transfer ack后下一块、单并发、Abort… |
| PERF-064 | High | 本次修复 | frontend_pages | layout/advancement仅当前500摘要页；PATCH当前变化而非全量resource snapshot，未加载siblin… |
| PERF-065 | Medium | 原根因确认解决 | frontend_pages | resource+locale请求复用有容量/TTL的ExpiringPromiseCache；图标5min128，表情30秒默认64；… |
| PERF-066 | High | 部分验证 | frontend_pages | 详情仅metadata；选择entry/chunk以AbortController读取单块并替换，cursor栈前后页；搜索复制只针对l… |
| PERF-067 | High | 部分验证 | frontend_shared | admin申请keyset/loadMore与generation guard、摘要页取后才聚合详情结构已完整审读；申请附件本轮FP05… |
| PERF-068 | Medium | 部分验证 | frontend_pages | 缺名preselection通过一次cancellable resource-presentations batch，浏览loadPag… |
| PERF-069 | High | 部分验证 | root | 只确认当前Node四条：600k真实TypedArray索引<2.5MB、切片仅触及9k、六面3.6m实例被联合预算拒绝，以及源码无逐面… |
| PERF-070 | Medium | 部分验证 | backend_c | 每代AbortController贯穿bytes/assets/renderer，load/dispose取消前代；不可中断parse/… |
| PERF-071 | Medium | 原根因确认解决 | backend_c | 一次resources顶层遍历建section/resource索引，组内一次sort并生成position/共享cluster，use… |
| PERF-072 | Medium | 原根因确认解决 | backend_a | pending/我的claims从固定窗口改keyset，max100、attachments一次批load；cursor防跨scope… |
| DB-001 | Medium | 原根因确认解决 | database_docs | 当前空库直接安装最终Outbox结构；生产schema声明与两项已完整读回归一致。本轮PG batch12安装实际最终generatio… |
| DB-002 | Medium | 原根因确认解决 | database_docs | 已重新读取syncProjectCreatorBindingsTx74–152及replaceCreatorTeamMembersTx2… |
| DB-003 | Medium | 原根因确认解决 | database_docs | 已重新读取importCreator89–159、buildCreatorImportPreview161–190和hydrateCre… |
| DB-004 | Low | 原根因确认解决 | database_docs | 完整schema与四组唯一约束/索引回归已语义审查；当前PG batch10确认等价重复索引未恢复，约束自有索引保留。 |
| DB-005 | Medium | 原根因确认解决 | database_docs | 当前project_file完整schema不注册不可达generic route，公开ID仍保留registry身份；PG batch… |
| DB-006 | High | 原根因确认解决 | database_docs | 当前镜像schema仅保留供应商文件身份唯一性，不按跨项目内容字节唯一；PG batch12/47双项目身份与补偿回归通过，合成故障不破… |
| DB-007 | Low | 原根因确认解决 | database_docs | 当前完整schema已无user_chat_presence，PG batch10关系不存在断言通过；读写权威仍为现有Redis/进程p… |
| DB-008 | Low | 原根因确认解决 | database_docs | 完整索引生成器/quality测试已语义审查；本模块实际专属PG18及root batch14验证所有外键有效前导索引和合法精确IS N… |
| ARCH-001 | Medium | 原根因确认解决 | backend_b | 模板读/解析/参数错误显式返回；通知及Outbox与源业务同TX，不吞发布失败。 |
| ARCH-002 | Medium | 部分验证 | frontend_shared | FE admin拆模块结构与最大行合同新执行PASS；BE确有1500行上界测试。 |
| ARCH-003 | Low | 原根因确认解决 | backend_b | revisionPublicIDValue返回value,error，nil无需查询，真实lookuperror保持调用方可观察。 |
| ARCH-004 | Medium | 原根因确认解决 | backend_c | userOSSFiles读取全部结果后检查rows.Err才成功；quota daily/total两个Scan必须分别成功才用值，任一… |
| ARCH-005 | Low | 原根因确认解决 | backend_c | streak完整读日期流后检查rows.Err；latestActivity仅ErrNoRows为空，其他SQL/scan错误返回cal… |
| ARCH-006 | Medium | 原根因确认解决 | root | raw活动、成长/奖励投影和删Outbox在调用者同一TX；投影失败整体回滚可重放，commit后只失效缓存。 对应当前精确测试已逐条核… |
| ARCH-007 | Medium | 原根因确认解决 | root | 限制与风险状态同TX；关键持久化失败503，bot规则Query/Scan/rows及聚合失败传播并保留待写事实。 对应当前精确测试已逐… |
| ARCH-008 | Low | 原根因确认解决 | frontend_shared | 准确PG进度audit强制失败保持可恢复、续删与finalizer失败不伪成功；FE显示audit_pending而非completed… |
| ARCH-009 | Medium | 原根因确认解决 | backend_b | import catalysts严格数组/对象结构，坏JSON不能吞成空[]，错误附revision身份。 |
| ARCH-010 | Medium | 原根因确认解决 | backend_c | catalog详情只有明确notfound才404，所有count/localization/slot/catalyst/binding… |
| ARCH-011 | Medium | 原根因确认解决 | backend_c | export所有row流terminalerror和Scan失败返回，stored JSON严格object/array形状验证并附稳定… |
| ARCH-012 | Medium | 原根因确认解决 | backend_c | typed providerHTTPStatusError贯穿team/version/description/authors/READ… |
| ARCH-013 | Medium | 原根因确认解决 | backend_b | simple row value/terminalcursor错误和scheduler claim/list错误都必须上抛，免无任务假成… |
| ARCH-014 | Medium | 原根因确认解决 | backend_c | 统一严格configuration decoder验证condition/rewards，active loader与后台/用户read… |
| ARCH-015 | Medium | 原根因确认解决 | backend_b | 关键durable metric读失败整体error，而非把失败列伪造0继续success。 |
| ARCH-016 | Medium | 原根因确认解决 | backend_b | 只ErrNoRows合法默认catalog；坏JSON/invalidnormalized/storedduplicate/DBfail… |
| ARCH-017 | Medium | 原根因确认解决 | root | 配置兼容hash与artifact tuple血缘同TX；旧hash、缺tuple/读取错误关闭，不在导出时实时猜版本。 对应当前精确测… |
| ARCH-018 | Medium | 原根因确认解决 | root | 任务/下载NoRows与DB错误分开；Scan/JSON/rows/租约/CAS/通知均可观察，DB终态通知事务由本轮DB012补强。 … |
| ARCH-019 | Medium | 原根因确认解决 | backend_a | 合法空返回与读取错误分离，所有派生stage传播错误，source关联优先选approved再取latest；namespace限定素材… |
| ARCH-020 | Medium | 本次修复 | backend_b | 复用查错/写日志失败可观测，失败对象通过durable cleanup；本次补先rollback失败quota/evidence TX再… |
| ARCH-021 | Medium | 原根因确认解决 | backend_b | 生成对象注册失败不可仅requestctx best-effort删除；独立有界cleanup事务记录Outbox并合并持久化失败错误。 |
| ARCH-022 | Medium | 原根因确认解决 | root | 评论装配/批量目标/total读错误不会输出部分页或伪0；不可见目标过滤与数据库错误有独立语义。 对应当前精确测试已逐条核同一根因及实际… |
| ARCH-023 | Medium | 原根因确认解决 | backend_b | 旧membership查询被有界新分页替代，但scan/terminalerror必须继续显式失败，不能返回半列表。 |
| ARCH-024 | Medium | 原根因确认解决 | root | 收藏写仅明确23505或NoRows映射业务错误，其余500；DELETE RETURNING区分不存在与执行失败。 对应当前精确测试已… |
| ARCH-025 | Medium | 原根因确认解决 | backend_a | 列表cursor/投影有界，坏refs/translation不静默空值，已完成译文结果读取只验证不自动新建。 |
| ARCH-026 | Medium | 部分验证 | frontend_shared | 精确回归corrupt completed result拒绝且notification GET时间不变，worker源码持久busine… |
| ARCH-027 | Medium | 原根因确认解决 | backend_a | collector检查Scan/RowsErr；配置仅ErrNoRows允许default，存储错误/null/nonobject拒绝而… |
| ARCH-028 | Medium | 文档过时 | backend_c | 当前公开/管理rows.Err与严格translation JSON错误返回，mutation按精确PgError约束分类。历史提到pu… |
| ARCH-029 | Medium | 原根因确认解决 | backend_c | 公开/管理页checkedRows收集器检查Scan/terminal并close，聚合translation要求object且type… |
| ARCH-030 | Medium | 原根因确认解决 | backend_a | 治理collector错误传播，stored报告snapshot必须object，DB故障500与确切无行404/409区分。 |
| ARCH-031 | Medium | 部分验证 | frontend_shared | useBanReasons scope含locale/token/attempt、cancel旧请求、错误retry，Selector/… |
| ARCH-032 | Medium | 原根因确认解决 | backend_c | decode/queue两个边界要求canvas/image_pixels/content、slot rect/visual_rect、… |
| ARCH-033 | Medium | 原根因确认解决 | backend_a | role/user授权读取错误不返回可编辑空值，createRole仅确认唯一约束冲突409。 |
| REUSE-001 | Medium | 原根因确认解决 | backend_c | kind→template、lootcategory只有共享SQL表达式，root发现/loot子类/placement使用受限alia… |
| REUSE-002 | Medium | 原根因确认解决 | backend_c | blocks/items分类ordinal和en-US/zh-CN/zh-TW六文案唯一helper，manual publicatio… |
| REUSE-003 | Low | 原根因确认解决 | backend_a | Minecraft版本排序复用持久registry已知次序，未知尾部稳定词典序，项目文件筛选同源。 |
| REUSE-004 | Medium | 原根因确认解决 | backend_c | provider事实读取/结构校验统一typed snapshot；六adapter每种只消费一次，仍保留各领域转换和权限边界。 |
| REUSE-005 | Low | 本次修复 | frontend_shared | 实际waitForPolledJob唯一循环保留共享terminal，已有preabort零请求；本轮修FS009清理已完成delay … |
| REUSE-006 | Low | 原根因确认解决 | frontend_shared | 四API边界只import统一localizationparser，无四套相互猜status/provenance转换；真实parser… |
| REUSE-007 | Low | 原根因确认解决 | frontend_shared | 两个实际申请唯一FileDropZone、业务限额/source保留，2精确接线合同PASS；本轮FP054更强submit成功锁包含在… |
| TEST-001 | High | 原根因确认解决 | frontend_shared | package test唯一node --test自动发现全树，已新增A031被统一门发现；fresh发现门断言PASS。 |
| TEST-002 | Medium | 原根因确认解决 | root | CI保留27.0覆盖率门和artifact；历史27.5不继承，最终当前coverprofile实际值另记录。 对应当前精确测试已逐条核… |
| TEST-003 | Medium | 原根因确认解决 | root | CI使用CGO_ENABLED=1真实race，最终当前全库race另实跑；数据库锁并发不冒充内存race。 对应当前精确测试已逐条核同… |
| TEST-004 | Medium | 原根因确认解决 | root | 25创建/导入入口比较角色/权限/版本等授权事实；实际管理接口403，临时schema仅测试所有权作用域。 对应当前精确测试已逐条核同一… |
| TEST-005 | Medium | 部分验证 | frontend_shared | 当前AST无persistence call，准确PG preview creatorCount0且PermissionGranting… |
| TEST-006 | Medium | 原根因确认解决 | backend_a | 直接覆盖HS256 header/signature/lifetime/publicsubject/session/authversio… |
| TEST-007 | Low | 原根因确认解决 | root | 结构化实际rows/loops/buffers/index判定，坏计划与非法JSON被拒绝；不把EXPLAIN文本出现index当使用证… |
| TEST-008 | Medium | 原根因确认解决 | root | HTTP→Outbox→真实dispatcher→worker→可控OSS→activate，以及cancel/补偿；非公网OSS/浏览… |
| TEST-009 | Medium | 原根因确认解决 | root | 2005revision真实Handler尾页/搜索/facets/角色scope及不自审；不是UNION只编译。 对应当前精确测试已逐… |
| TEST-010 | Medium | 原根因确认解决 | root | 确认绑定job/run/mod/owner/source/hash/期限且单次消费；确认与Outbox同TX，生产text[]按数组解析… |
| TEST-011 | High | 原根因确认解决 | root | incoming关系只读权威快照；两独立project editor真实IDOR矩阵证明无改归属。 对应当前精确测试已逐条核同一根因及实… |
| TEST-012 | Medium | 原根因确认解决 | root | 删除/停用/无引用和archived引用真实事务矩阵；旧错误空类型必须成功断言已被真实需求修正。 对应当前精确测试已逐条核同一根因及实际… |
| TEST-013 | High | 原根因确认解决 | root | 真实Session+HTTP资料创建审核/跨版本/全局归属/失败事实原子不变；source与完整HTTP结果需对应最终指纹。 对应当前精… |
| TEST-014 | Medium | 原根因确认解决 | root | 六类目录/审核/author审批/12并发slug/深页/rows错误；实际HTTP断言非极简UNION编译。 对应当前精确测试已逐条核… |
| TEST-015 | High | 原根因确认解决 | backend_c | 服务器探测固定已审DNS地址防混合内网与重绑定，cancel/deadline限制配置协议；提交/审核/调度持久事实及通知原子。 |
| TEST-016 | High | 原根因确认解决 | root | 上传/扫描/发布/权限/owner/计数同TX，受控MR/CF及OSS；云端扫描、公网提供者不继承。 对应当前精确测试已逐条核同一根因及… |
| TEST-017 | Medium | 原根因确认解决 | root | 更新日志同步绑定/人工接管/独立审核/分类归属/通知原子与八语言cursor；不冒称通知消费。 对应当前精确测试已逐条核同一根因及实际断… |
| TEST-018 | High | 原根因确认解决 | root | 201有效收件人及排除关系、双worker批次唯一投递、查询故障/stale/重复/隐藏目标实际持久状态断言。 对应当前精确测试已逐条核… |
| TEST-019 | High | 原根因确认解决 | root | 真正自动更新worker+受控provider先503后成功、租约/幂等/永久错误；本轮leaseOwner进一步防旧worker迟到。… |
| TEST-020 | High | 原根因确认解决 | backend_a | 真实worker领取owner/heartbeat、失败分类退避、source降级和动态DB并发capacity可控验证；真实并发门后发… |
| TEST-021 | High | 原根因确认解决 | backend_c | notifications区分direct/broadcast可见性与readreceipt，keyset/readwatermark/… |
| TEST-022 | High | 原根因确认解决 | backend_c | 完整message membership/block、offline intent/counters Tx、shared/fallbac… |
| TEST-023 | High | 部分验证 | frontend_pages | 真实production React测试控制EventSource及不合作fetch迟到结果：会话切换清private body/dra… |
| TEST-024 | High | 部分验证 | frontend_shared | 已全文审brand/site-logo/mail相关前端边界，实际root品牌fixture最初artifact API错配FAIL保留… |
| TEST-025 | Medium | 原根因确认解决 | backend_a | 日志策略/清理边界/SSE全middleware路径已有直接行为测试，配置与runtime统一；A020锁连接复用防Max1死锁。 |
| TEST-026 | High | 原根因确认解决 | backend_b | 真实JetStream ack/redelivery/dedupe/restart/reconnect和Outbox租约恢复可复现，mo… |
| TEST-027 | Medium | 原根因确认解决 | backend_c | NATS配置完整HTTP保存/连接失败隔离，reconfigure保持已持久配置和订阅恢复；Schema FK/unique/UTC语义… |
| TEST-028 | High | 原根因确认解决 | root | 逐项关联评级完整HTTP事务/可见性/并发、开发者全关系的增量及重建、千万评分集合plan、due-index衰减、双worker/8次… |
| TEST-029 | Medium | 原根因确认解决 | root | 公开DTO反射注入访客身份后JSON仍无身份且handler无访客历史查询；pageKey闭集拒绝、真实热度队列双worker/故障恢复… |
| TEST-030 | High | 原根因确认解决 | root | 真实PG+受控Typesense覆盖毒文档、双worker、滚动version拒降级、持久化失败/恢复；实际Typesense29另测。… |
| TEST-031 | High | 原根因确认解决 | root | 并发达标仅一次经验/货币、manual/track来源共存、ledger故障全TX回滚后重放。 对应当前精确测试已逐条核同一根因及实际断… |
| TEST-032 | Medium | 原根因确认解决 | root | 4099项重复版本装饰1SQL及DB失败无部分结果；三个gzip接受/质量/二进制跳过断言、durable metrics DB失败非0… |
| TEST-033 | Medium | 原根因确认解决 | root | 启动即同步、双pool租约/竞争零外呼、7artifact与双NeoForge血缘，上游全断保留旧字节。 对应当前精确测试已逐条核同一根… |
| TEST-034 | High | 原根因确认解决 | root | 真实鉴权/PG/生产worker及可控OSS贯穿导出权限/ready/补偿/过期/历史；外部供应商未验。 对应当前精确测试已逐条核同一根… |
| TEST-035 | Medium | 部分验证 | frontend_shared | 准确当前ZIP测试中央目录/索引超限拒绝，Worker测试success completed100且provider release/f… |
| TEST-036 | High | 部分验证 | backend_c | 蓝图上传→normalize→review→convert→download和持久job/lease/cleanup有完整可控HTTP回… |
| TEST-037 | High | 未验证 | frontend_shared | 历史称223名称race两次446PASS，但这份92分配未附当前准确test名称/definition/执行记录，不能从数值推导本轮验… |
| TEST-038 | High | 原根因确认解决 | backend_c | 评论idempotency匹配真实target/version，reply/watch通知意图与评论/counts同Tx，真实附件读错误… |
| TEST-039 | Medium | 原根因确认解决 | root | 完整skin HTTP权限/共享blob/回滚补偿/并发/通知原子；deleted文件在ETag之前拒读。 对应当前精确测试已逐条核同一… |
| TEST-040 | Medium | 原根因确认解决 | root | 真实评分鉴权/可见性/维度故障TX/并发唯一行与完整8维/trigger事实，规模cursor独立。 对应当前精确测试已逐条核同一根因及… |
| TEST-041 | Medium | 部分验证 | frontend_shared | 真实favorite-selection pure状态4组freshPASS，多集合delta/新建/失败输入保留已核；后端旧准确Tes… |
| TEST-042 | High | 原根因确认解决 | root | 四kind社区审核重提、悬赏hold/refund/award并发与CAS/语言/权限；全部真实PG事实，AI只模拟。 对应当前精确测试… |
| TEST-043 | High | 原根因确认解决 | root | 真正AIWorker用本机TLS兼容端点先503失败后显式新任务成功，重复消息零外呼；不证明真实翻译质量。 对应当前精确测试已逐条核同一… |
| TEST-044 | High | 原根因确认解决 | backend_b | 真实HTTP JWT/session走经济权限、持久化、并发、inventory与level撤销；非只调用纯计算helper。 |
| TEST-045 | Medium | 原根因确认解决 | root | PNG/GIF私有元数据确实重编码剥除、伪类型/截断/过帧/巨大头拒绝；结构化引用+父行/引用锁source合同和真实PG索引投影/临时… |
| TEST-046 | Medium | 部分验证 | backend_c | 站务public locale fallback与admin严格语言、translation draft/publish、About r… |
| TEST-047 | Medium | 部分验证 | backend_c | 生产未解析引用统一7来源投影及14filter权威，keyset/前缀与宽查询预算；坏DB读错误失败关闭，不空页伪成功。 |
| TEST-048 | High | 原根因确认解决 | frontend_shared | 准确真实NewServer HTTP PG测试[200,409]并发claim与持久事实验证已核且current定义SHA匹配执行。当前… |
| TEST-049 | High | 原根因确认解决 | root | 后台HTTP手动权限/角色全TX审计、版本/session/cache和并发替换；系统来源/expiry保留。 对应当前精确测试已逐条核… |
| TEST-050 | Medium | 原根因确认解决 | root | 重新运行当前load CLI6个本机HTTP/纯函数测试，200成功与429非当前场景拒绝非零退出、只有精确风控码为预期拒绝、失败比率六… |
| OPS-001 | High | 原根因确认解决 | backend_c | 新环境JetStream默认开启，durable explicitACK/MaxDeliver和exponentialBackoff受共… |
| OPS-002 | Medium | 部分验证 | frontend_pages | CI要求npm ci/test/type/lint/build，actions不可变SHA，镜像standalone/nonroot，发… |
| OPS-003 | Medium | 原根因确认解决 | frontend_shared | 准确PGpre-upload登记fileID、原子activate/compensate、补偿写失败返回、orphan恢复幂等；sour… |
| OPS-004 | Medium | 原根因确认解决 | backend_b | retry自己持久化attempt_count，并在上限终止；成功批次不消耗失败预算，持久化错误向上返回。 |
| OPS-005 | High | 原根因确认解决 | backend_c | crawler领取随机lease_owner、续心跳和同owner CAS写，provider处理中lease被偷时取消旧ctx；过期恢… |
| OPS-006 | Medium | 原根因确认解决 | backend_c | 所有provider分类全失败返回retryable error不completed清空last_error；部分成功completed… |
| OPS-007 | Medium | 原根因确认解决 | backend_c | 连接重试/订阅注册同Client权威状态，Close终态/cancel停止恢复，latest disabled config生效，SUB… |
| OPS-008 | Medium | 部分验证 | frontend_pages | 前端品牌读公开config，logo允许既定site-assets静态PNG或HTTP(S)，设置改动触发Provider reload… |
| OPS-009 | Medium | 原根因确认解决 | root | Outbox claim/publish/fail/dead均须SQL成功及当前lease CAS一行，metrics仅权威状态成功后增… |
| OPS-010 | Medium | 原根因确认解决 | root | 两热度队列8次/退避/stale/CAS，毒任务终态可见；本轮默认测试新增显式ownedDB opt-in不变断言。 对应当前精确测试已… |
| OPS-011 | High | 原根因确认解决 | root | 同真实PG state-machine从higher projectionVersion拒绝旧worker：collection调用、q… |
| OPS-012 | Medium | 未验证 | frontend_pages | 管理界面消费loader同步结果和全部sourceUrls，前端不实现多实例锁，也不决定scheduler启动时机。 |
| OPS-013 | High | 原根因确认解决 | database_docs | Export ready failure enters task-row-locked compensation; linked suc… |
| OPS-014 | High | 原根因确认解决 | backend_a | jobowner CAS、有界100 SKIPLOCKED回收及尝试耗尽避免旧owner改新租约；选配转换不伤已ready蓝图。 |
| OPS-015 | High | 部分验证 | backend_a | 先登记pending再PUT，attempt key幂等；activate与completion同事务，失败abandon+删除outb… |
| OPS-016 | Medium | 原根因确认解决 | backend_b | lease token条件领取/完成，失败分类有最大12次dead原子alert；未确认成功不能清除意图。 |
| OPS-017 | Medium | 原根因确认解决 | backend_b | rehome generation与lease跨重启保留，入队在三种业务TX内；processor失败可重试不能吞。 |
| OPS-018 | Medium | 部分验证 | frontend_shared | multipart持久session/lease/runToken/owner binding、head size+SHA校验与12次b… |
| OPS-019 | High | 原根因确认解决 | backend_a | queued/retrying无outbox和stale running有界100恢复，当前代次fence保护；dead publish… |
| OPS-020 | Medium | 原根因确认解决 | backend_b | 每清理域独立deadline/有界批次，ban优先，单表锁超时不能饿死TTL/session/其他维护。 |
| OPS-021 | Low | 原根因确认解决 | root | 只读observer采样/等待/编码/0600文件/stdout任一失败非零，不以已写诊断JSON当成功。 对应当前精确测试已逐条核同一… |
| OPS-022 | Low | 原根因确认解决 | frontend_shared | 真实deployment parser生产必填HTTPS API/site，拒绝credential/query/fragment、si… |
| STYLE-001 | Low | 前端范围不适用 | frontend_pages | Go formatter门与CRLF兼容属于后端工程约束；本代理前端文件审查不能验证Go格式。 |
| STYLE-002 | Low | 部分验证 | frontend_shared | 前端只消费camelCase typedautomation DTO，准确PGGET只读/permissions回归包含响应shape检… |
| STYLE-003 | Medium | 部分验证 | frontend_pages | ApiError保留status/code/details/retryAfter，展示只按稳定code选翻译，未知code/transp… |
| STYLE-004 | Low | 原根因确认解决 | backend_b | permission runtime测试只承担权限域；OSS转换/APNG/roletrack测试迁至各自清楚文件且断言保留。 |
| STYLE-005 | Low | 原根因确认解决 | backend_a | dashboard集成测试按领域保留，不再单个巨文件；source合同核原覆盖符号仍在各域。 |
| STYLE-006 | Low | 原根因确认解决 | frontend_shared | API download返回稳定code/status+英文内部fallback，UI根据locale key统一显示expired/f… |
| STYLE-007 | Low | 原根因确认解决 | frontend_shared | normalizeBlackroomStatus实际canonical三态，非法五类unknown且本地化显示；closedtype不再… |
| STYLE-008 | Low | 原根因确认解决 | frontend_shared | 四domain+shared模块，跨领域forbidden符号/最长行门/旧巨型文件不存在由freshsource tests直接证明；… |
| STYLE-009 | Low | 原根因确认解决 | frontend_shared | en/zh managerLoginRequired实际developer/editor/admin文案，不含owner；后端权限未因改… |
| MAP-001 | Low | 原根因确认解决 | backend_b | 无价值jsonUnmarshal一行wrapper已删，直接标准库json.Unmarshal承担真实解析错误。 |
| MAP-002 | Low | 原根因确认解决 | backend_a | botrule write DTO不接受readOnly变更，写入始终readonly；历史管理员不能把检测规则变成可写控制。 |
| MAP-003 | Medium | 原根因确认解决 | root | 唯一11action/27object登记表派生seed/清理/任务匹配；保持ID，不手工三套映射。 对应当前精确测试已逐条核同一根因及… |
| MAP-004 | Low | 原根因确认解决 | backend_c | exportResourceSource单PublicID，registry/document/tag/resource以及bluepr… |
| MAP-005 | Low | 原根因确认解决 | backend_c | simpleProjectTypeRegistry有序六类唯一字面量，membership与返回values均派生，不同service不… |
| MAP-006 | Medium | 原根因确认解决 | frontend_shared | 当前真实DDL event revision_id bigint FK restrict，数值writer已读；精确PG合法bigint… |
| MAP-007 | Medium | 原根因确认解决 | root | 完整读schema guard不再标量owner/min/limit1；真实schema两独立developer对view/favori… |
| MAP-008 | Medium | 原根因确认解决 | root | 完整读有序registry测试：5collection/7document精确顺序、schema字段/loader/idPageQuer… |
| MAP-009 | Low | 原根因确认解决 | backend_b | sourceURLs完整保留多来源；SourceURL兼容主URL，不能normalize/持久化时丢NeoForge legacy出处… |
| MAP-010 | Low | 原根因确认解决 | database_docs | Rating handler UPDATE-first then INSERT conflict fallback has no exp… |
| MAP-011 | Medium | 部分验证 | frontend_shared | 当前baselineSchema的权限source/source_key CHECK与复合主键、context无字段由结构测试证明。 |
| MAP-012 | Low | 原根因确认解决 | frontend_shared | UILocale类型及codes均从supportedUILocales派生，editableContentLanguages引用同re… |
| LEGACY-001 | Medium | 原根因确认解决 | root | 八类通知生产者稳定模板+TX Outbox，无旧direct固定文案helper；与BUG001同根不重复计生产修复。 对应当前精确测试… |
| LEGACY-002 | Medium | 原根因确认解决 | database_docs | Fresh nats_outbox is final20column declaration; infrastructure schem… |
| LEGACY-003 | Low | 原根因确认解决 | frontend_shared | 实际server注册仅认证GET/me/unread-summary，旧notifications/unread不存在，第一方Shell… |
| LEGACY-004 | Low | 原根因确认解决 | root | 现生成只Argon2id，Password/Code明确拒PBKDF2；不能从开发期断言生产无历史账号，导入需显式迁移。 对应当前精确测… |
| LEGACY-005 | Low | 原根因确认解决 | backend_a | 只规范sort field与direction，不继续旧alias隐式映射；canonical默认规则稳定。 |
| LEGACY-006 | High | 原根因确认解决 | root | Mod导入只用Outbox及注册handler，无Core直发/无监督goroutine；无NATS由同handler DB fallb… |
| LEGACY-007 | Medium | 原根因确认解决 | frontend_shared | 实际server只保DELETE content-sections与PATCH layout，旧PUT无注册，source全读已确认旧写… |
| LEGACY-008 | High | 原根因确认解决 | backend_a | 通知翻译任务+quota+outbox同事务，保留trace，B015活动去重按完整源payload且聚合删除旧cached译文。 |
| LEGACY-009 | High | 原根因确认解决 | database_docs | createDirectMessageTx writes message, advances conversation and enqu… |
| LEGACY-010 | Medium | 本次修复 | backend_b | 保存retention策略不立即执行cleanup，独立命令负责删除；本次canonical map修复trim残键/空键/冲突非确定。 |
| LEGACY-011 | Medium | 原根因确认解决 | root | UnwrapEvent严格schema v1完整身份，裸payload到handler之前拒绝；生产者统一EnqueueTx。 对应当前… |
| LEGACY-012 | Medium | 原根因确认解决 | database_docs | LoadNATSConfig returns decoded normalized stored record as complete … |
| LEGACY-013 | High | 原根因确认解决 | backend_a | 上传/转换/retry共用可靠job+outbox事务，无临时直发替代成功响应。 |
| LEGACY-014 | Medium | 原根因确认解决 | root | 旧静态durable-only source guard与真实PG enqueue/restart/lease/retry/deadal… |
| LEGACY-015 | Low | 原根因确认解决 | backend_b | multipart由真实size>=16MiB服务端计算，不信任旧preferMultipart opt-in；旧comment rep… |
| LEGACY-016 | Low | 部分验证 | frontend_pages | 当前举报dialog唯一POST/api/v1/reports，理由从统一reasons边界读取；旧reportComment与comm… |
| LEGACY-017 | Low | 原根因确认解决 | backend_c | rating规范化只trim/lower接受权威九canonical目标，删除5旧拼写，客户端类型闭集；resolveRateableT… |
| LEGACY-018 | Low | 部分验证 | frontend_pages | entityPublicId作为收藏请求/展示/summary/picker唯一公开身份，不再定义entityKey；guest cat… |
| LEGACY-019 | High | 原根因确认解决 | backend_a | 通用/内容/community enqueue共用outbox事务，删除重复直发；dead publisher现在有显式受权限且审计同事… |
| LEGACY-020 | Low | 原根因确认解决 | frontend_shared | 当前server与authorization handler无旧users/{id}/roles PUT/updateUserRoles… |
| LEGACY-021 | Low | 原根因确认解决 | frontend_pages | 唯一治理入口/admin/global-resources；旧app/catalog/resources/page.tsx不存在，无复制… |
| LEGACY-022 | Low | 原根因确认解决 | frontend_shared | en/zh queued文案实际reliable/可靠交付任务，不提前宣称NATS已publish，202阶段与Outbox一致。 |
| DEAD-001 | Low | 原根因确认解决 | frontend_pages | banStatus和BlackroomCard无无用locale传递；状态文案始终t，真正详情日期仍传locale。 |
| DEAD-002 | Low | 原根因确认解决 | root | 文档导入entries无无用ordinal/_占位且namespace严格解析；删除的是无语义局部变量。 对应当前精确测试已逐条核同一根… |
| DEAD-003 | Medium | 原根因确认解决 | frontend_shared | 准确PGfollow notifications_enabled false/重复PUT不重置、坏body400/隐藏target关系o… |
| DEAD-004 | Low | 原根因确认解决 | root | 自动更新可重试pending与终态dead_letter，DDL/filter无无写入者failed；旧schema恢复需明确工具支持。… |
| DEAD-005 | Medium | 原根因确认解决 | backend_a | max_concurrency已runtime读取并在事务advisory lock中按全库active lease重核，配置不是只展示… |
| DEAD-006 | Low | 原根因确认解决 | root | 全新权威schema字符串禁止旧表，实际InstallEphemeralSchema核relation absent且公共generat… |
| DEAD-007 | Low | 原根因确认解决 | frontend_shared | 当前DDL CHECK只import/editor，准确PGdictionary插入合法来源并拒backfill/split；无用伪能力… |
| DEAD-008 | Low | 原根因确认解决 | database_docs | rating schema has neither direct_commenters nor child_commenters res… |
| DEAD-009 | Low | 原根因确认解决 | root | 完整current contract禁止failed:=false/if failed旧空分支，真实PG poison与Typesens… |
| DEAD-010 | Low | 原根因确认解决 | backend_c | 未使用has_manual_detail投影/scan已删除，实际has_detail独立保留；manual判断/URL使用真实有值字段… |
| DEAD-011 | Low | 原根因确认解决 | root | 完整读TEST034末段核information_schema中favorite_modpack_export_tasks.report… |
| DEAD-012 | Medium | 原根因确认解决 | backend_c | server去掉sticker version/state，客户端有限TTL30秒coalesce与成功mutation显式invali… |
| DEAD-013 | Medium | 原根因确认解决 | backend_b | 公开ban列表/详情DTO白名单与管理员internal独立；敏感notes/revocation reason仅受ban.view_i… |
| DEAD-014 | Low | 原根因确认解决 | frontend_shared | 实际dashboard直接requirePermission包装loadAdminDashboard，旧nav/纯转发adminDash… |
| DEAD-015 | Low | 部分验证 | frontend_shared | 实际卫生测试PASS证明工作树无被跟踪ignored IDE文件；.idea/忽略规则已读。 |
| DEAD-016 | Low | 原根因确认解决 | frontend_shared | 当前两主词典没有home.subtitle/activityHint且动态fallback无独立覆盖，首页消费真实heroDescrip… |

## 可核验口径

JSON的sources记录七份原输入文件SHA及额外root-delegated-final-followup证据SHA；每项保存原记录及完整manual_source_record、其canonical JSON SHA，root89的最终人工补证位于manual_followup_review。各来源嵌入记录的整体canonical SHA也保存。核验使用UTF-8、ensure_ascii=false、sort_keys=true、separators=(",", ":")，不能将展示摘要当成原证据。

449项ID及原历史字段可直接与原台账逐行比较。assignment_counts必须为89+46+75+91+24+32+92；所有者集合不能重叠。生成时已断言原字段完全一致、无缺失重复、无私有工作区绝对路径或连接串/密钥样式。只改证据路径为公开相对引用，不复制日志正文、环境文件或真实凭据。

本报告尚不含远端CI/PR结果，不承诺生产零风险。最终门已通过，root89仅在逐条人工判断和具体当前断言覆盖原根因后更新；其他审查者的部分/未验证边界继续保持。原历史449表没有重写，whole PASS未用于批量消除限制。
