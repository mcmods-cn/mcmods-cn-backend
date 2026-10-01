# API-A handler 补充审查

frontend-core 在完成分配前端范围后，按主执行者/API-A 的独占协作分配，逐段补审 21 个原有 handler，之后按独占授权直接修复确认缺陷，基线 `bd50afd0ef92192c40c5152ea56e27a3fdbb657f` 共 10,851 行。已完整阅读并分析每个文件的数据流/权限/错误/SQL/事务/调用链；实际段和指纹见 `review-api-a-core-supplement.json`。合并多文件输出出现一次中段截断，已单独补读整个 profile_showcase 与 editor_application 文件。适用后端根与目录没有 AGENTS/CLAUDE/README（实际文件搜索），另读 Go module、兼容边界、项目权限模型、站务/收藏导出/日志分享/用户统计设计，并跨读相关路由注册、progression.SyncTrackRole 和已有 PG 测试组织。

这是静态语义补审覆盖，不代表 21 个模块逐角色业务全部运行通过。其他所有者在并行修复；内容已读与最终修改后复查分开，最终指纹必须再次核对。

| ID | 优先级 / 证据 | 问题与处置 |
| --- | --- | --- |
| BE-SUP-001 | P2，确定静态缺陷 | 收藏导出历史/明细不检查 rows.Err、忽略依赖 JSON decode，会把失败/部分报告返回成功。API-A 已加 fail closed；最终回归归 API-A。 |
| BE-SUP-002 | P1，确定读改写缺口 | 角色线路事务外读取、吞 Scan/Err 后删整组写回，权限丢失/并发覆盖。API-A 已移入用户/线路锁事务并传播审计错误；其它权限写入口由 API-A 同步锁。 |
| BE-SUP-003 | P1，确定跨语言影响 | about 保存一种语言草稿将母页变 draft，所有其它发布语言随之 404；写无效语言静默改中文。root 已分离母页/翻译状态并拒绝无效写语言，列表 Err 补齐；PG 场景归 root。 |
| BE-SUP-004 | P1，真实 PG 红灯 | 他人待审 mod 能收藏后经自己私有夹读取名字/slug；旧条目也缺资源可见性过滤。真实 handler + PostgreSQL synthetic fixture 红灯，API-A 修读写授权和用户锁。 |
| BE-SUP-005 | P2，真实 handler/PG已验证 | 任务 rewards 缺失/null 在 nil map 写入 panic；frontend-core已加400验证，2子例红绿PASS。等级配置经验迭代Err已补齐，独占PG真实流故障/整配置及权限状态回滚/恢复后成功均race PASS。旧线路角色来源无区分是单独待决策风险，未擅删授权。 |
| BE-SUP-006 | P1/ P2，确定静态边界 | API访问RawQuery持久化会留OAuth code/签名链接等；root已整体抹掉query值（保留redacted存在性）。querySimpleRows吞SQL/Rows错误会把管理读取失败变空成功，root 已将 helper 与约26个调用方改为显式错误返回，真实 PG 14 场景含流中断与 -race 的证据由 root 记录。 |
| BE-SUP-007 | P1/P2，静态风险 | GIF先DecodeAll后检查总帧/像素，分配限制过晚；UI-B独占有界preflight修复。贴纸历史引用/共享图片删除保护由相应所有者核对，不能据当前Markdown查询宣称历史安全。 |
| BE-SUP-008 | P2，Err确定、快照边界待需求核验 | 举报对象快照未一致要求对象已公开，comment附近上下文也可含非公开状态；仅管理员权限读取快照，不把此现象直接称已证明公开泄漏。report/blackroom列表Err已由root修复并真实PG流故障红绿。 |
| BE-SUP-009 | P1/P2，确定静态过滤缺口 | simple_project父引用只校验route，公开读join无审核过滤可透出待审parent；前四批关联rows不检查Err，图库读取缺scan状态。UI-A 已修公开/写入 parent 可见性、各关联 Err、图库 scan/MIME 和错误分类；真实 PG/Race 证据归 UI-A。 |
| BE-SUP-010 | P1，确定权限反向写入 | group.* 的Allow=false仍授role且expiry/context静默忽略；API-A独占修，并统一权限替换锁/审计传播。SMTP 已移除共享可变 mailer 赋值，按实时 Enabled 构造 activeMailer；定向回归归 API-A。 |
| BE-SUP-011 | P2，静态功能/校验差异 | creator作品/数量只涵盖mod，而其它项目已有Creator bindings；已补至少文字或真实本人安全附件、10,000 Unicode 字符上限、Err/解码错误、200上限稳定分页和批量附件；真实 MaxConns=1 PG 通过。作品定义扩展为所有类型仍属明确产品决策。 |
| BE-SUP-012 | P2，静态可靠性/i18n差异 | editor申请审核/撤销忽略permission audit SQL返回值和刷新错误；通知硬编码英语却source_locale zh-CN。已将 grant/revoke 审计纳入原事务并传播错误；英文通知 en-US、中文默认不变；列表有界分页、SQL发布过滤名称 join、关闭结果后批量附件，审核名称在原事务；owned PG 13 关键场景通过。 |

BE-SUP-015：server 审核列表持外层 rows 再 3 子查询，真实 MaxConns=1 PG 红灯 500/context deadline。已改为完整收集、检查 Err、关闭结果后读详情；最终 -race 绿灯通过见验证 JSON。每条详情查询仍存在有界 N+1，没有生产量级计划/性能数据，不能宣称已优化为固定查询数。用户展示/卡片只做static权限和字段白名单核对，没有生产缓存/数据量健康结论。

## 实际验证与隔离边界

- `TestSaveTaskMissingRewardsReturnsBadRequestCore`：原两子例 panic FAIL，添加校验后两子例 PASS；无外部服务，没有 mock 掉 handler 核心。后续 `TestLevelConfigurationExperienceStreamFailureRollsBackCoreIntegration` 补真实PG迭代故障500/完整配置与用户权限回滚/恢复后同请求成功，race PASS5.162秒；不是生产断连或大规模更新验收，也没有虚构原Commit防线之外的旧数据丢失红灯。
- `TestPrivateFavoriteCannotRevealOtherPendingModCoreIntegration`：任务专用 PostgreSQL 17 的 127.0.0.1:55432，2随机 synthetic 用户+待审mod+私人夹；真实 setFavoriteMembership/writeFavoriteCollectionItems 在原实现返回200并暴露 synthetic 名字/slug，FAIL。加强版同时验证已有条目过滤与重新 approved 后仍可读，已在 API-A 修复后重新运行，最终状态见验证 JSON。
- fixture清理先于pool关闭，并检查清理错误。首版测试的pool defer时序导致3个合成记录残留，已按准确生成key在同一任务隔离数据库删除（1mod/2users），没有触碰其它任务/真实资源；这是测试清理错误，不是产品故障。已修测试cleanup顺序。
- 初始 Go1.26.5 工具与项目后续声明升级由root组织；最终按root Go1.26.8完整回归记录。未使用生产.env，未打印连接串/秘密，未请求真实邮件/OSS/AI。

任务内日志：`/tmp/backend-progression-core-green.log`、`/tmp/backend-favorite-core-red.log`、`/tmp/backend-favorite-core-red-2.log`、`/tmp/backend-favorite-fixture-cleanup.log`。正式任务入口和迁移边界：`docs/progression-task-validation.md`。Git/PR/整体测试由主执行者交付，补审者没有提交/推送。

## 新确认与实际完整修复

- BE-SUP-013（P1）：合法作者认领始终 500，原 insert 把同一参数同时推导为 text 与 bigint（42P08），不可达的 approved CASE 没有业务价值。改为明确 pending，真实 10,000 个中文字证明201持久化；不允许跳过审核。
- BE-SUP-014（P1）：认领批准产生派生访问却不写既有权限审计。按 root 授权同事务补 author_claim.approve；对真实 append-only 审计表注入 CHECK(false) 时，旧实现200，新实现500且 pending/权限状态回滚。grant/revoke 的审计错误也都回滚。
- 作者/编辑员8组13叶测试在 ownership marker 核验的随机完整 PG 数据库运行，SeedRBAC 与真实外键/不可变触发器保留，单连接池、有效/他人附件、私有历史、分页、权限授权/撤销、英文与中文通知、审计拒写都通过。通知由真实 NotificationWorker 消费 persisted Outbox payload，不声称已测外部邮件供应商或所有 NATS 投递。
- 编辑员/认领分页对应前端实际可达审核控件，每页50、前后页、最后一项审核后的页码纠正、旧请求取消；5个组件测试的API明确为mock，真实后端接口另由 PG 测试证明。部署顺序为先前端兼容完整旧列表，再后端有界列表；没有迁移。
- Root 日志治理不再吞删除错误；前端配套 typed LOG_CLEANUP_FAILED 显示策略已保存/清理部分失败。权限审计不可删与 retention 的冲突仍需保留政策决策，不能为测试绕过真实审计机制。
- BE-APIA-025（API-A 主问题）：补充者新增随机 token/context 租约 helper，running/owner/expiry FOR SHARE 事务内 fencing、10分钟续租、15秒心跳、崩溃回收最多5次；2个真实 PG -race 用例通过。worker 全部副作用接入与最终 publication race 归 API-A，补充 helper 不单独证明整条自动更新业务。
- AI 失败验证新增3组9叶 actual owned PG/local HTTP -race：断连/超时/调用者取消与恢复不自动重付，未知 usage 预留保留，坏批次整体不发布且不破坏既有译文，模型停用零 mock HTTP。详见正式 AI_TRANSLATION_RELIABILITY；没有真实 AI 费用/语义验收。

完整命令、退出码与日志在 `validation-api-core-supplement.json`。首次应用 fixture 列名错误、并行修改时编译中间态、原审计 actor hard-delete 清理冲突、通知 Outbox 未消费、HTTP mock 未读取 body 的失败均如实留证；最终 owned 整库清理保留真实 schema，而非删除触发器或影子表绕过。业务用户删除是软状态，生产物理删除审计 actor 的保留政策不属于本次授权。

正式文档：`docs/project-applications.md`、`docs/progression-task-validation.md`、`docs/server-review.md`、`docs/AI_TRANSLATION_RELIABILITY.md`。数据库生产运行健康、高负载与真实付费供应商未验证；可实际执行的本地验证已运行，不把 mock 结果描述为真实供应商质量通过。

后续最终应用回归使用明确8组选择器执行 `-race`，13叶全部PASS55.291秒；最终服务器/收藏/奖励组合4叶PASS3.450秒。文件基线21个全部完整语义审查，另14个新增或正式说明文件实际全文复查；最终35条指纹见台账，跨团队改变继续按各最终diff复核，不以本记录证明全部项目测试。

最终API-A再次通读发现 BE-SUP-016（P2）：adminTasks/adminActivityEvents 两个 raw loop 缺少迭代Err检查，真实故障返回200空列表。frontend-core按原独占范围补齐500边界；最后fresh `SoWZrtAU` 的随机marker-owned PG中2叶原代码FAIL3.137秒，正常列表与故障分类同场景在修后 `-race` PASS11.315秒。新测试实际64行全文复查，progression源最终515行已再次逐段全文读完；不把此前已读/测试过等同于已证明不存在漏检。

## 最终状态与问题归并

21 个原文件保留 `baseline_read_ranges` 和基线指纹；最终 `read_ranges` 由已逐行阅读基线的相等内容映射与实际补读全部改变块组成，`unchanged_line_mapping` 记录两版本行号，`final_direct_read_ranges` 另列最终补读。creator/governance/progression/editor/server/siteaffairs 六扩行文件还实际补读最终尾段，不能用旧行号冒充当前覆盖。35 条为21原有文件及14新增/正式说明文件；台账自身机器元数据不递归自签。读取覆盖与行为覆盖仍分开。

| 补审ID | 最终状态（限所列证据） | 主ID / 证据及剩余边界 |
| --- | --- | --- |
| BE-SUP-001 | 已修复验证 | 归并 BE-APIA-020：真实PG错误/损坏JSON回归，见API-A报告。 |
| BE-SUP-002 | 已修复验证 | 归并 BE-APIA-016：真实PG并发角色与审计回归。 |
| BE-SUP-003 | 已修复验证 | root `TestAboutDraftDoesNotUnpublishOtherLocalesIntegration` 实际PG红绿；公开/草稿跨语言状态。 |
| BE-SUP-004 | 已修复验证 | 归并 BE-APIA-015；本补审真实PG写拒绝、既有条目过滤、批准后可读。 |
| BE-SUP-005 | 已修复验证 | nil/null rewards 2断言已红绿；真实PG迭代故障、配置/用户状态/权限完整回滚、恢复后的真实成功race PASS；旧权限来源无区分另属决策项。 |
| BE-SUP-006 | 已修复验证 | RawQuery归并 BE-UIB-001；querySimpleRows家族保留此主ID，root真实14PG场景含流故障与race。两个根因分别引用，不整行另加重复总数。 |
| BE-SUP-007 | 已修复验证 | GIF根因归并 BE-APIB-016，实际有界分配/有效/损坏GIF回归；历史共享图片删除保护是待核验风险，非此ID已证明修复。 |
| BE-SUP-008 | 已修复验证 | 列表Err由root `TestGovernanceListsRejectPostgreSQLStreamFailureIntegration` 3真实流故障红绿。管理员私有对象快照的产品边界待明确，未称公开泄漏。 |
| BE-SUP-009 | 已修复验证 | 分别归并 BE-UIA-SP001/002/003；真实PG父可见性/关联流Err/图库扫描，见UI-A追加后端报告。 |
| BE-SUP-010 | 已修复验证 | 权限根因归并 BE-APIA-017，SMTP并发/停用根因归并 BE-APIA-018，真实PG/race；未发送真实SMTP。 |
| BE-SUP-011 | 已修复验证 | 证明/本人安全附件/Unicode限制/稳定有界分页：owned PG及MaxConns1；扩大作者作品定义仍待产品决策。 |
| BE-SUP-012 | 已修复验证 | grant/revoke审计失败回滚、私有分页、实际Outbox通知源语言：owned PG/race，未宣称外部NATS或SMTP全部投递。 |
| BE-SUP-013 | 已修复验证 | 原合法认领PG42P08，真实pending创建及持久化红绿。 |
| BE-SUP-014 | 已修复验证 | 审批派生权限同事务审计，真实审计CHECK拒写使状态/权限回滚，13叶应用回归包含此场景。 |
| BE-SUP-015 | 已修复验证 | MaxConns1审核列表旧500→关闭外层rows后详情成功，真实PG/race；没有生产规模性能结论。 |
| BE-SUP-016 | 已修复验证 | adminTasks/adminActivityEvents完整读后检查Err，真实PG正常有数据200/故障500/不漏SQL错误，两叶旧FAIL→新race PASS。 |

16 个补审标签均在明确场景已验证；这不是去重后新增问题数量，也不代表每种故障、每条业务旅程或生产性能都已验收，合并项以主ID计数。自动更新 lease helper 归主 BE-APIA-025；AI补充失败测试是扩大既有链路证据，未为测试数量新造缺陷ID。本范围没有找到可独立确认总数的权威历史清单，不把旧“约20项”当作事实。

决策/未验证与确认缺陷分开：Creator Works 是否包含所有资源类型、旧手工角色与线路派生角色来源区分、管理员举报对象私有快照的需求边界、权限审计不可物理删除的保留政策均需明确既有产品/保留要求；生产运行健康、真实供应商费用与语义、多实例长时间负载和历史贴纸引用清理未取得运行证据。
