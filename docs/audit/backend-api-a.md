# 后端 API A 审查与修复记录

基线：`bd50afd0ef92192c40c5152ea56e27a3fdbb657f`。原始边界为 `review-manifests/backend_api_a.json` 的 138 个文件、41,581 行；新增文件继续纳入最终台账，不删除原始分母。最终版本由主任务提交/PR 再补；本代理未提交、切分支、部署或接触生产。

## 覆盖与复查

逐行证据：`review-backend-api-a.json`，包含最终 SHA-256、行数、已读取行段、职责、调用链、问题及内容/语义/行为分别状态。人工完整审查了高风险认证、权限、社区、皮肤、蓝图、导入/归档/导出链及剩余短测试。没有将搜索或扫描结果标成全文审查。

只读补审：Core 的 `review-api-a-core-supplement.json` / `backend-api-a-core-supplement.md` 对 21 文件完整逐段语义审查。与本代理重叠文件按路径去重；内容发生变化的记录保留待复查状态。数据库代理接管 25 个尚未完整读取的文件，其中5个5433行后续明确移回本代理，已完整审查及修复，详见 `review-backend-api-a-additions.json`；4个短文件由 UI A 独立补审，剩余由 `review-backend-database-extra.json` 记录。汇总校验实际指纹和行段，不根据代理摘要直接完成。

原始 138 个文件均有可核验的完整内容和语义审查，部分审查 0、未审查 0。最终本台账纳入 163 个文件，其中 25 个为新增或跨边界完整复查文件；按路径去重，无删除原始分母。外部补审仅凭当前 SHA 匹配和完整读段/语义记录合并。生成执行证据和台账 JSON 另登记排除人工业务重构的理由；文件阅读覆盖和下述测试数量不混算。

## 问题台账

| ID / 严重度 | 根因及影响 | 修复位置/状态 | 验证与边界 |
|---|---|---|---|
| BE-APIA-001 P1 | required 认证将会话/权限查找数据库故障误报 401，客户端可能清除有效登录 | `middleware.go`，运行故障 503，真实失效 401 | closed-pool 回归旧版 401→新版 503；required/optional 均拒绝业务且不清 session/cookie |
| BE-APIA-002 P2 | degraded/未启用 Redis 登录限流一次请求消耗两次本地配额 | `auth_rate_limit.go` 仅选择一次共享/本地策略 | local、disabled、miniredis、不可用 Redis 4 场景红绿；miniredis 不描述为真实集群 |
| BE-APIA-003 P1 | 社区翻译源文校验与写入分离，取消/删除/撤回/新源文可被迟到任务写入；入队未用共享额度/outbox | `community_post_handlers.go`，task/source 行锁、版本复核、译文和 completed 同 tx；共享站点/用户预留和 outbox、术语快照 | 真实 PG 12 场景；并发源变更/取消通过观察实际 Lock wait 验证；标题单独存在允许空正文，缺字段拒绝，completed 重复拒绝并保留译文。供应商为确定性测试边界 |
| BE-APIA-004 P2 | Accept-Encoding 的显式 gzip q=0 被 wildcard 覆盖；无效 q 值也启用 gzip | `compression.go` | 实际 middleware HTTP 响应 4 红绿场景及既有 gzip 测试 |
| BE-APIA-005 P2 | 同 primary-language 的 zh-TW 包可被错误贴成 zh-CN/Hans/HK/MO | `mod_export_locale_bundles.go` exact normalized locale | 真实 PG zh-TW 精确命中及 5 地区/脚本不匹配红绿；不含 OSS bundle 实际上传 |
| BE-APIA-006 P2 | 权限测试解错 API data envelope，Allowed 默认 false 让断言虚假通过 | `auth_handlers_test.go`，明确 data 非空且正向/拒绝均断言 | 4 用例，含 admin wildcard + 优先 explicit deny、numeric quota；实际 evaluator 修复归 BE-APIB-004，避免重复漏洞计数 |
| BE-APIA-007 P1 | 导入 claim SQL 错误被 ACK；旧 worker 不限定领取版本更新；stale recovery 仅启动运行 | `mod_import_worker.go`，started_at fencing、错误传播、有界独立失败写、周期恢复 | closed-pool 红绿；真实 PG 独立 schema 3 场景重复 delivery→1 local HTTP、替换 claim 后旧成功/失败不覆盖；stale/recent 分别恢复/保持 |
| BE-APIA-008 P1 | 外部 API 同 host HTTPS→HTTP redirect 可转发凭据到明文通道 | `mod_import_worker.go` require unchanged scheme and host | 实际 redirect policy 红绿 + 既有跨 host 拒绝；无真实凭据和供应商调用 |
| BE-APIA-009 P1 | 画廊替换共享目标键，旧 CopyObject 先覆盖新对象后才 DB CAS 失败 | `oss_rehome.go` 目标包含 gallery+file 不可变身份 | replacement 键不同、同 file 稳定的回归 PASS；静态跨文件确认 CAS；OSS 服务端并发复制没有专用 fixture，部分验证受阻，不能声称实库/真实 OSS 覆盖 |
| BE-APIA-010 P1 | skin 多语言逐个 PUT，第一条待审锁阻断下一条，UI 假部分成功 | `skin_handlers.go` metadata PUT 接受 defaultLocale/localizations，单个 revision 发布 | 6 验证边界、长度/未登录用例；真实 PG 单 snapshot pending 不改已发布资料、批准 2 locale 同 tx，保留未提交 ja human、AI en rev4→5 corrected。UI A 单 PUT 联通；浏览器验收由前端记录 |
| BE-APIA-011 P1 | 公共 player profile 的 nested texture SQL 暴露 private skin 名称/hash/元数据 | `skin_handlers.go` 嵌套读取按 claims 过滤 active/approved/visibility/owner/admin | 实际 PG guest 旧版泄露，新版 guest/other 不见、owner/admin 可见 4 场景；Yggdrasil 哈希协议独立设计 |
| BE-APIA-012 P1 | blueprint metadata 删除全部 locale 再重插，遗漏人工译文丢失、revision/provenance 重置 | `blueprint_handlers.go` 复用 shared merge publisher | 真实 PG overlay 原版缺 ja count1 FAIL→新版 count2、AI en rev5 human_corrected PASS；UI 无明确删除语言功能，省略保持，不新增删除策略 |
| BE-APIA-013 P1 | blueprint 格式下载只检查 variant ready/review，没排除 soft-deleted 主资源 | `blueprint_handlers.go` signing 前 status filter | 真实 PG active owner 实际 query 有行，deleted 真实 handler 404；签名 helper 本身无资源授权由 API B 交叉确认 |
| BE-APIA-014 P1 | blueprint 上传完成 job commit 后只 publish，未遵守开启的 transactional outbox | `blueprint_handlers.go` normalize job/event 同 tx | 真实 PG 独立 schema，旧版 events0 FAIL；新版 events1、重复不增 job、自有 outbox CHECK 故障让 upload/variant/job 全回滚；事件隔离于实际 dispatcher |
| BE-APIA-015 P1 | private collection 的 includePrivate 绕过资源全部 review_status，收藏他人待审资源后读泄露 | `favorite_handlers.go` write/read 分开资源授权、历史 item 仍过滤；membership 严格 Scan/Err；用户锁串行集合替换，未知夹 400 | Core 真实 PG 旧版泄露 FAIL→新版 write 拒绝、种旧 item 后 GET200 过滤、approved 后仍读 PASS。私有夹权限不授资源权 |
| BE-APIA-016 P1 | role track 事务外读绑定/忽略 Scan/Err，再 delete/write；并发 upgrade 丢更新；全量替换不同步 | `role_track_handlers.go` 用户锁与事务内完整读取；`admin_handlers.go` 替换同锁；审计错误传播 | 真实 PG 同时两个 Lock wait→两次升级达到 role2、2 条审计、permission_version 原子+4；原版只 role1 FAIL |
| BE-APIA-017 P1 | group.* allow:false 仍授权；expiresAt/context 被丢弃变长期全局角色 | `admin_handlers.go` only allow true；unsupported scope 400 | 真实 PG 原版 expiry 请求200 FAIL；新版 expiry/context 400 保留原绑定、false 零 grants、true 正常；不新增权限模型 |
| BE-APIA-018 P2 | SMTP enabled:false 被 host/from 重新算 true；配置 PUT 改共享 Mailer 与并发读竞争 | `admin_handlers.go` immutable fallback，saved enabled 控制 activeMailer | 真实 PG 原版 disabled FAIL；新版 enabled/disabled 与并发 save/read，`-race` PASS；未发送 SMTP 邮件 |
| BE-APIA-019 P1 | 已发布社区项目/资源引用及筛选泄露待审/撤回对象的名称、ID、版本与关联，旁路主资源边界 | `community_post_handlers.go` 项目approved joins、资源复用公共目录guard；中间来源/译文过滤；cover scan；filter/count同边界 | 项目关联原版真实PG红→绿；资源pending/withdrawn原版2红→新版隐藏且approved保留；实际filter total/items均验证。原始unresolved输入保留 |
| BE-APIA-020 P2 | 收藏导出 history/detail 忽略 rows.Err、依赖 JSON shape 错误变空、数据库故障误报缺失/过期 | `favorite_modpack_export_query.go` strict errors/JSON；真实不存在/过期保留原错误语义 | 受控 closed pool history/detail/download 500；真实 PG malformed dependency shape 500、修成 [] 后正常。最终回归日志见下文 |
| BE-APIA-021 P2 | RBAC 展示 helper 吞 SQL/Scan/RowsErr，权限节点 upsert 审计在提交外且吞错 | `admin_handlers.go` 读取错误返回、节点和审计共 tx；users list Err | closed-pool helper error；真实 PG audit FK 拒绝后权限节点不落库 PASS；其余事务 audit 已由 PG aborted transaction 阻止错误提交，不能把新增显式处理描述为旧版均可越权 |
| BE-APIA-022 P2 | gzip/SSE Flush 忽略 gzip 与底层传输错误，连接失败后 worker 继续写 | `compression.go` FlushError + ResponseController | 压缩/非压缩底层错误及 gzip write 3 原版红→新版绿；真实 cookie/SSE 交互由 API B/root 验证 |
| BE-APIA-023 P1 | 仅解析模组 slug 就公开其活动子内容，待审父模组权限被版本/分类/资源入口绕过 | `mod_revision_handlers.go` shared readable guard，`mod_content_handlers.go` 各 GET、revision history/compare；API B 资源历史复用 | 原版7入口×访客/其它用户14失败；8入口×5角色/状态40权限子场景通过；编辑身份解析独立，未扩大编辑授权 |
| BE-APIA-024 P2 | mod 内容列表吞 Rows.Err；QueryRow/准备布局故障误报404/422；持 TX 又从 pool 读审核配置导致单连接自阻 | `mod_content_handlers.go` / `mod_content_layout_handlers.go` / `mod_revision_handlers.go` / `community_post_handlers.go` strict stream/errors、TX loaders与图片配置前置 | 真实 PG5流式错原版200红、4编辑/删除读故障404红、layout准备422红；真实完整迁移库 MaxConns1 原版超时红→新版实际 revision 发布 PASS。最终52内容权限/故障场景PASS；社区MaxConns1 create/list原版红→新版create/update/list真实持久化PASS；edit/translation request/result运行错误原404/400→500，缓存读取错误不入AI队列 |
| BE-APIA-025 P1 | 自动更新固定lease_owner且无发布fence/心跳/过期回收，旧worker会覆盖替代任务、配置和内容 | `project_automation_worker.go` / `project_maintenance_automation.go`，Core `project_automation_lease.go` | owned全迁移PG11发布/暂停/重绑/取消场景，Core2 lease组实际锁等待/heartbeat/过期恢复均-race PASS。终态/settings同TX；外部OSS/真实provider未执行 |
| BE-APIA-026 P2 | Modrinth snake_case字段缺tag使版本/发布时间/渠道错误；CF更新日志503吞错变空正文发布 | `project_automation_worker.go` | 确定性HTTP2用例原版FAIL→新版PASS；不是实际供应商连通/质量验收 |
| BE-APIA-027 P1 | provider专用transport继承环境代理，DialContext可能只检查代理IP，目标DNS/IP边界失效 | `mod_import_worker.go` 禁隐式代理、scheme/userinfo/fragment检查；仅明确base主机loopback例外 | proxy/3非法URL4原版红→新版绿；签名download query仍允许，API base config独立拒绝query；部署需直连公共源或经审查可信代理，云端真实出网未验证 |

来源 BE-SUP-001/002/004/010 分别归并 020/016/015/017，不重复计算。其他 Core 问题的实修归主任务、UI A、API B 或 Core：site affairs、progression、log privacy、sticker、simple-project parent/gallery、creator claim/editor audit/通知。其最终证据参照各自报告。

## 角色、用例和前后端契约

| 模块/角色 | 入口与持久化 | 核验/改善 | 尚未据本记录证明 |
|---|---|---|---|
| 访客/用户认证 | required/optional middleware → sessions/users/cache/RBAC | outage 有界 503；限流一请求一份 | 全多设备/browser/session 旅程由 API B/前端总验收 |
| 用户/submitter 收藏 | me/public collections → resource route/review/owner → favorite items | folder 与 resource 授权分开；历史撤回过滤；错误不假成功 | 完整浏览器收藏/导出下载及 live OSS |
| 资源 owner/reviewer 多语言编辑 | metadata PUT → immutable revision → tx localization publisher | skin 与 blueprint 单 PUT；保留 human/修订号；旧 request/snapshot 兼容 | 审核按钮/错误反馈浏览器由前端记录 |
| 访客/owner/admin 角色皮肤 | player profile → texture asset SQL | public profile 不能透私有资源；owner/admin 仍允许 | Yggdrasil 客户端真实登录/texture 消费 |
| 普通用户社区翻译 | source public revision → quota task/outbox → AI worker → translation/completed | source/task tx fence，取消/过期拒绝；术语表快照；确定性失败路径 | live provider 翻译质量、计费、生产队列和真实额度 |
| 管理员 RBAC | group/direct permission replacement / track shift → bindings/audit/version | 串行、false 不授权、unsupported expiry/context 明确拒绝 | 旧自动 track 角色没有 manual/automatic 来源区分的清理策略需产品/迁移决定 |
| 用户导入/worker | external source URL → provider HTTP → job result | 领取版本、重复/崩溃回收、HTTPS redirect/IP 守卫 | 真实 provider 访问/限额与高负载 |
| 访客/submitter/editor/reviewer 模组内容 | slug → 父审核状态/角色 → version/section/resource/revisions | 先核父项目权限；完整结果才200；单连接TX实际发布 | 完整浏览器模组编辑与导入资产渲染由主任务另核 |
| autobot 自动更新 | settings/source → leased run → provider → 内容/绑定/settings | token+配置snapshot TX fence、heartbeat/有限恢复、provider错误终止 | 真实外部下载和OSS scan/upload未执行；跨系统上传后登记失败仍可能留孤儿对象 |

所有 ID 对外仍使用既有 public ID；不把数据库对象直接返给前端。skin 新字段类型由 frontend-core 更新，LocalizedAssetEditor 的一次提交由 UI A 更新；没有用 mock 自家 API 表示真实联通。

## 数据库、缓存与异步一致性边界

本代理不新增 migration。依赖实际 PG 行锁、既有唯一/外键/check、append-only revision/audit 和绑定 version trigger。skin/blueprint 共享发布器最终一致性/人工保护改进由 DB 代理实修，基线已有 AI invalidator 不算本代理新功能。

真实迁移后的 schema用于社区、皮肤、人工译文、角色、收藏和权限节点测试。导入/蓝图上传/SMTP 使用随机独立 schema 的真实表定义 `LIKE INCLUDING ALL`，保留 PG 查询/事务/约束语义以隔离后台队列；该方式没有复制所有 FK/trigger，不充当完整迁移或完整 FK 验证。root/DB 另测空库与旧数据升级。

角色合成审计不可删除，保留对应随机测试用户/审计到任务专用数据库整体销毁；mutable bindings/track/roles 精确清理。社区 immutable revisions 也保留直至任务 DB 清理。测试没有删除线上数据、关闭 immutable trigger、连接生产或清空共享 schema。对象归档的外部复制未在本任务真实执行，静态键防覆盖与 DB CAS 不能证明生产 OSS 状态健康。

生产容量/锁等待/备份恢复/表膨胀没有取得授权运行数据，不作健康结论。查询小样本 PASS 不作性能提升比例。

## 环境与实际验证

Go 初次 `1.26.5`、最终 `1.26.8`（root 对照官方 SHA 校验后安装），隔离原生 PostgreSQL/Redis/NATS 由主任务初始化，明确 test DSN。当前工作区和 original baseline 独立，不用 `git checkout` / 文件覆盖做红绿。

| 验证 | 状态/证据 | 范围 |
|---|---|---|
| 最初针对 auth/community/evaluator | PASS；4 top tests+22子场景 | required/optional、local limit、task/source boundary |
| 首个 targeted race | PASS exit0；`/tmp/mcmods-api-a-final-race.jsonl` 54 run/pass | 修复前段，不能据此覆盖之后追加改动 |
| 追加 targeted race | PASS exit0；`/tmp/mcmods-api-a-final2-race.jsonl` 65 run/pass incl subtests、4.522s | auth/gzip/locale/import/rehome/skin/blueprint/role/mail/favorite/ref；其后 community completed 原子变更再单独 PASS 12子场景 |
| 6 原始代码 overlay 回归 | FAIL（预期红）；`/tmp/mcmods-api-a-red-additions.log` | blueprint human/outbox、community refs、disabled SMTP、role concurrency、group scope；只换编译 overlay，未改共享文件。为新 helper caller 保留 error-return signature，否则无法编译，原 endpoint 逻辑未变 |
| 社区最后状态发布 | PASS exit0；`/tmp/mcmods-api-a-community-final-green.log` | completed 同 tx、重复拒绝、实际 task/source 锁竞态 |
| SMTP真实PG/race | PASS exit0；`/tmp/mcmods-api-a-mail-green.log` | 不 Send/不出网；saved enabled 与 immutable fallback |
| 角色/blueprint | PASS exit0；`/tmp/mcmods-api-a-roles-green.log` / `mcmods-api-a-blueprint-green.log` | 实际 PG contention 与自有 outbox 故障回滚 |
| admin 读错/审计 | PASS exit0；`/tmp/mcmods-api-a-admin-read-audit-green.log` | read fail不是empty；audit拒绝不落权限 |
| 最终原范围+新增回归 | PASS exit0；`/tmp/mcmods-api-a-final4-race.jsonl` 31.388s，run430/PASS429/SKIP1 | 精确237top测试模式见 `api-a-validation.json`；既有真实Modrinth opt-in未启用，单独未验证。该历史回归后追加的小变更由下列fresh最终回归验证，不能把历史结果单独当全部最新源码证明 |
| 新5模块中间有效回归 | PASS exit0；`/tmp/mcmods-api-a-new-five-final3.jsonl` 103 run/pass，15.765s | 含既有content单测/PG和当时44权限/stream/读故障场景；不是最后源码全部证明 |
| 本域当前版本回归 | PASS exit0；`/tmp/mcmods-api-a-final-fresh3-race.jsonl` 80 run/pass、6.844s、0skip；永久终态证据 `evidence/api-a-current-targeted.json` | 8组；52内容权限/故障、社区private refs/filter与MaxConns1创建/编辑/列表、社区翻译12边界、运行故障分类、crawler recovery隔离。第一fresh编译中断0测试保留为BLOCKED；之后Core的独立progression补修另按其回归汇总 |
| 静态检查 | 最新 `go vet ./internal/httpapi` PASS exit0 `/tmp/mcmods-api-a-final-fresh2-vet.log`；早期记录也保留 | 最后源码修改后实际执行；全仓vet由主任务另汇总 |
| 全仓 go test/vet/build/race、前后端/browser、迁移、CI | 主任务汇总 | 本分支不声称代替全仓验收 |

真实 red 前遇到两次 fixture setup FAIL：导入设置错误用 plaintext 而非本项目 AES-GCM envelope，随后等待 local provider 超时；修正 sealSystemSetting 与 early-completion 处理后 PASS。社区引用首轮遗漏 schema必填 source_locale 23502，补合成 source/body 后 PASS。社区首版 cleanup 违反 immutable revision trigger，改保留 history/仅清精确 mutable对象后 PASS。没有调整生产代码掩盖 fixture 错误，也没有只记后一次绿而抹掉失败。

正式行为/兼容性/发布顺序见 [API 可靠性约定](../API_RELIABILITY.md)、[OSS 目录规范](../oss-object-layout.md) 与 [AI 翻译可靠性](../AI_TRANSLATION_RELIABILITY.md)。所有日志仅本地验证证据，报告不包含真实凭据/用户数据；最终提交/PR/CI 由主任务补统一交付。

最终验证记录保留了三类非业务失败/遗漏：其他代理进行中的新helper/signature导致 build FAIL（0测试，修复后重跑）；TestMain 缺本任务 state 环境标识时安全 preflight FAIL（没有执行DB）；一次新增内容函数名模式写错导致该组 NOT_RUN（其他组PASS）。更正真实函数名后40场景有效回归 PASS。没有把这些记录重写为业务通过或忽略首轮结果。

seed crawler 的随机租约 helper/实际锁回归归本代理新增文件，任务与费用闭环主问题归 `BE-DB-022`，不重复计入 API A 的27个ID。正式自动更新说明见 [项目自动更新设计](../../PROJECT_AUTO_UPDATE_DESIGN.md)：既有lease字段不新增migration，部署先排空/停止旧worker再启动新版；无fence旧worker不能混跑。

最终人工追加记录包含5个原始文件、maintenance跨链、4个新模块回归、2个crawler租约文件、导出异常测试和2个社区可靠性回归，见 `review-backend-api-a-additions.json`。最终52个内容权限/故障场景已在fresh实际执行；seed普通恢复明确排除 `stats.kind=translation_recovery`，避免AI失败恢复run被当整轮crawler重领，新增断言也已实际通过。seed worker 最终565行的独立逐行复查和当前SHA由DB代理台账提供，已核对并合并，不将其他代理摘要当作本人的阅读证据。

社区最终新增回归仅使用完整迁移的owned数据库及单连接真实PG。资源metadata泄漏和配置自阻分别保留原版红，修复后同fixture验证pending/approved/撤回、filter结果与实际创建/编辑持久化。缓存和completed结果错误传播为静态语义复核，三个首读故障有closed-pool红绿；不把未独立注入的后续缓存读取错误描绘成已真实PG故障验收。
