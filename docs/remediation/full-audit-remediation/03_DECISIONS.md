# 修复决策日志

## D-366：用户在目标完成后明确授权发布当前相关代码和文档

- 两仓已fetch，现有分支均`codex/unified-catalogs-user-systems`，origin分别为用户指定`mcmods-cn/mcmods-cn-backend`与`mcmods-cn/mcmods-cn-frontend`仓库；本地与远端无分叉，已有暂存内容为空。
- 提交前逐文件核对已验收ZIP清单：backend1191/frontend596个源码与文档文件全部一致，185/37个实际变化文件全部在已验证快照中，没有新增未知或无关变更。只有本次发布说明修改00/03/07/10，生产代码、测试、CI、Schema及前端源码保持最终已测内容。
- 按用户新授权提交上述相关代码与文档并正常推送现有分支；不推送ZIP/临时日志/私有环境或工具运行时，不强推、不创建PR、不改默认分支、不部署。旧“不自动推送/未提交”记录明确标为验收时快照，不能当成本次后续授权未执行。
- 提交后核对两个GitHub远程branch SHA与本地HEAD、工作树状态；最终SHA由Git记录/核对结果提供，避免在同一提交中伪造可自引用的最终hash。原审计、全部实际失败、验收哈希和已交付ZIP不改写；最后再跑默认严格449项校验及差异检查。

## D-365：全部当前源码门真实通过后完成最终复审，原统计与独立复核分别保留

- remaining18-final-reverified-backend-results.json于2026-10-01 19:13:05生成，最终14门exit0；1604发现=调用=顶层PASS/92批0失败0skip，五真实条件5 PASS/0skip，30个queue名称双次Race60 PASS/0skip。普通155.359s、全仓Race161.175s、覆盖30.3%≥原27%，Vet/Build/tidy/govulncheck/真实CLI默认严格/全部后端workflow actionlint通过。1070源/模块/CI、完整168 Schema和6原归档哈希不变，精确owned库清理0。
- 同一当前前端冻结六门全部0，596交付文件一致、283 Node测试和12真实Chromium React流程0skip、Type/Lint零warning、58页Build及全部584依赖0漏洞。独立19:13:57完整性检查又确认1064 Go/14只读格式批零未格式化、双仓diff-check/前端全部workflow actionlint、17原审计与原ZIP完整SHA不变、默认严格PASS、owned最终库残留0。
- 449 CLOSED/0 NA及所有非终态0，原汇总0/176/214/59/0和277原逐项级别零降级；172缺原级别按已获用户确认独立复核，复核148/236/65。不存在临时补计、豁免、删Finding、删除功能或降低测试/查询预算；普通条件跳过不代替显式DB，原RED/夹具纠偏和最终复审失败均保留。
- 十九领域语义复审与不可变449项证据索引完成，最终开发源码成熟度A，可转向前端优化。A不声称生产部署、多节点容量、真实云OSS/Redis/SMTP/AI或远程GitHub job已验证；这些部署确认单独列07。源码继续保持本地未提交，不扩大以前单次GitHub推送授权、不覆盖历史ZIP；当前完整源码/进度新包独立逐文件SHA验证。

下方D001至D364是按发生时间保留的历史决策；其中当时未完成或开放不代表当前仍有待修复Finding。

## D-001：保护开始时已有用户修改

- 后端已有修改：`internal/httpapi/community_post_handlers.go`、`internal/httpapi/community_post_handlers_test.go`。
- 前端已有修改：`app/_components/community-post-editor.tsx`、`app/_components/unified-report-dialog.tsx`、`app/_locales/en-US.ts`、`app/_locales/zh-CN.ts`。
- 决策：不重置、不覆盖、不自动回滚；若 Finding 必须修改同一位置，先保留用户行为并在本日志记录交叉影响。

## D-002：原审计证据不可变

`docs/audit/full-project-audit/` 与根目录审计 ZIP 仅作为输入。修复状态、重新定位后的代码证据和测试证据全部写入本目录。

## D-003：172 项逐项严重度缺口

`11_FINDINGS_REGISTER.md` 声明 449 项最终总数为 High 176 / Medium 214 / Low 59，但其早期增量表只为 277 个唯一 ID 提供了可机器读取的逐项严重度；172 个后续详细 Finding 没有独立严重度字段。初始台账如实将这些项记为 `UNRESOLVED`，不擅自降级。最终关闭前必须按审计所述可利用性、数据/权限影响、故障范围、可恢复性和发布阻塞标准逐项复核，并精确满足 176/214/59；默认校验器会拒绝任何未解析严重度。

## D-004：校验器的两个运行模式

- `go run ./tools/remediation/verify_findings -allow-open`：工作中校验 449 ID、重复、遗漏、类别、类型和状态枚举；允许 OPEN 与 `UNRESOLVED`。
- `go run ./tools/remediation/verify_findings`：最终严格门禁；任何非 CLOSED/NOT_APPLICABLE、未解析严重度、缺失验证证据或严重度总数偏差均失败。

## D-005：供应商 BaseURL 与凭据 origin

- Modrinth、CurseForge、GitHub 凭据分别只允许发往 `https://api.modrinth.com`、`https://api.curseforge.com`、`https://api.github.com` 的精确 origin；默认端口按同一 origin 规范化。
- 无凭据的自定义 HTTPS（及既有本机 HTTP 开发边界）仍可用于镜像/测试，但 HTTP 头构造始终不向其附加供应商密钥。
- 管理员更换 BaseURL origin 且未显式提供新凭据时，旧凭据被清除而非继承。CurseForge 若仍启用，会因缺少 API Key 失败关闭。
- 不增加旧的不安全双路径或空转发兼容层；所有现有供应商凭据调用点迁移到同一 origin 校验函数。

## D-006：Mod 关系的单一所有权

- `mod_relationships.mod_id` 是关系声明的来源与唯一写入所有者；编辑目标 Mod 不能改写指向自己的其他来源关系。
- `incoming` 仅为查询时从其他已审核来源关系派生的只读视图，不进入新修订的权威 snapshot，也不由保存路径回写。
- `integration` / `conflict` 不再被代码擅自视为双方一致事实；双方可分别声明，任何一方保存都不能删除另一方声明。未来若产品要求双方确认，应使用独立审核事实而不是跨项目隐式写入。
- 已绑定站内目标在写入和公开读取时都要求 `review_status='approved'`；未收录 Mod ID 继续走既有未解析引用协议。

## D-007：经济金额域与守恒

- 用户单次金额上限为 1,000,000,000,000，余额及单笔购买总额上限为 100,000,000,000,000；购买数量上限 100、单项库存上限 1,000,000。API、运行时运算与数据库约束使用同一边界，Schema 源测试防止常量漂移。
- 税额使用商和余数的纯整数向上取整，不先执行 `amount * taxBPS`；任何成功结算都满足 `tax + received = amount`。商店总价与下载奖励先检查乘法上界，再执行乘法。
- 所有余额变化在 `checkedCurrencyBalance` 中先比较可用余额和加法余量，随后才更新余额、写不可变流水；拒绝路径不产生任一持久事实。
- 问题悬赏复用同一税额分解协议，数据库对已颁发悬赏强制 `tax_amount + net_amount = amount`；购买流水强制 `total_price = unit_price * quantity`。
- 权威空库 Schema 升为 generation 86。项目仍采用开发期整代重置，不增加旧结构双写或在线迁移；未对现有远程数据库执行 DDL 或重置。

## D-008：授权事实按来源独立

- 角色绑定主键改为 `(user_id, role_id, source, source_key)`；允许 `manual`、`account_status`、`governance_ban`、`level_track`。直接权限采用同类复合主键，允许 `manual`、`user_preference`、`system_seed`。
- `manual` 的 source_key 固定为空；自动来源必须携带稳定 key。账号状态用 `registered` / `banned`，治理封禁用 ban 内部 ID，等级用 track code。撤销只删除自己来源，不按当前配置或角色代码猜测历史事实。
- 后台 PUT 只替换 manual 来源。非 manual 来源由 GET 明确返回为 `editable=false`，前端只读展示 code、allow、expiry、source/sourceKey；请求伪造系统来源、group deny 或重复 code 返回 400。
- `user_permissions.context` 没有任何授权匹配语义，因此删除；不把它更名为备注继续留在安全事实中。多个直接来源命中同一权限时沿用高优先级 direct override，冲突由 deny 获胜。
- 等级同步无论切换或清空都先删除旧 `level_track` 来源，再按当前等级写新来源；manual 和其他来源不变。角色线路手工升降在同一事务先锁用户与线路，再读取/替换 manual snapshot，并保留有效期。
- 无第一方调用且无法表达来源/有效期的旧 `/roles` 全量替换路由直接删除，不保留兼容 Wrapper。
- 角色图写入使用同一 PostgreSQL advisory transaction lock 串行化。新增/更新后全图验证父级存在且无环；删除内建角色或仍被默认配置、子角色、用户绑定、任一轨道引用的角色一律 409，要求调用者先显式迁移依赖。
- 权威 Schema 升至 generation 87；仍只支持开发期整代重置，没有对现有远程数据库执行 DDL。

## D-009：经济配置必须在运行时可执行

- 热度促销不再通过历史行数推导下一个序号。每个 `object_route_id` 使用独立原子 counter；counter 递增、促销插入、刷新入队与库存扣减位于同一事务，任一步失败全部回滚。
- 启用签到与全部下载奖励规则只能引用 `status='active'` 的货币。配置切换、货币停用/改码、active 商品保存、签到和下载奖励共享 advisory transaction lock；运行路径在锁内重读配置，不使用事务外旧快照。
- 被启用规则或 active 商品引用的货币不得停用或改码，调用方必须先显式迁移配置/商品。disabled 商品可保留 disabled 货币引用，但重新启用前必须迁移到 active 货币。
- 可购买商品类型是闭集：`profile_background`、`project_heat_boost`、`server_heat_boost`。应用校验、数据库 CHECK、前端类型和管理员 select 使用同一三值协议，不保留运行时无法执行的自定义类型兼容路径。
- 权威 Schema 升至 generation 88；仍按开发期整代重置策略，不对当前远程数据库执行 DDL。

## D-010：外部下载身份与可执行脚本不能由字符串相似性授权

- Modrinth 下载身份只从严格解析后的官方 CDN URL 产生：HTTPS、精确 `cdn.modrinth.com`、默认端口、无 userinfo/query/fragment，路径必须恰为 `/data/{project}/versions/{version}/{file}`，project/version 只能是有界安全标识。
- 同一 MRPack 文件的全部下载镜像必须解析为同一 project/version；任一非法或冲突地址都会让该条目退回未解析文件名，不把攻击者输入映射成站内 Mod 身份。MRPack 导出和生成复用同一解析器，不保留三套宽松判断。
- Iconfont 继续允许既有官方远程 Symbol 部署，但只接受精确 `at.alicdn.com/t/c/font_*.js`，拒绝子域、相似后缀、端口、凭据、query 和 fragment；不采用宽泛 `.alicdn.com` 执行边界。
- 远程 Iconfont 必须同时配置单一 SHA-384 SRI。启动时 URL/SRI 任一缺失或非法即抛错；浏览器脚本带 `integrity`、anonymous CORS 与 no-referrer，并继续受现有 nonce CSP 约束。完全不配置时保留可读 fallback，不加载远程脚本。

## D-011：举报权限来自目标当前可见性，不来自身份目录

- `public_routes` 只负责稳定身份与路由，不是查看授权。举报项目类目标必须复用项目关注/详情同源的类型化可见性解析；评论必须先确认自身公开状态，再复用评论所属对象解析器。
- 举报人可以举报自己当前可见的待审内容；具备项目审核权限者也按既有详情权限查看。普通用户对待审、隐藏、私有或软删除目标统一得到 404，不泄露目标存在性。
- 可见性检查、目标正文/上下文/附件元数据快照、举报和证据绑定在同一 PostgreSQL Repeatable Read 事务内完成，避免不同语句观察到跨状态快照。
- 评论目标和邻近上下文只采集 `published` / `deleted` 事实；pending、hidden 等内部状态不会因举报上下文被复制。历史举报继续使用提交时的不可变快照，不因目标后来隐藏而破坏审核证据。

## D-012：收藏夹可见性与被收藏对象可见性独立

- `favorite_collections.user_id/is_public` 只决定访问者能否打开集合，不能授权集合中的对象；拥有私有集合也不能查看他人待审、隐藏或未完成对象。
- 收藏写入与 membership 查询复用当前 claims 的类型化项目解析器。写入解析和删除/插入位于同一 Repeatable Read 事务；不存在、不可见、请求类型与 route 类型不一致均使用相同 404 边界。
- 收藏产品只支持 Mod、整合包和蓝图。应用白名单、所有收藏读查询和 generation 89 数据库 CHECK 使用同一三值闭集，不保留任意 `public_routes.entity_type` 的历史兼容入口。
- 摘要 `itemCount`、拥有者列表、公开列表与 MRPack 导出预检都按当前访问者重新验证目标。MRPack 不能再把公开集合中后来隐藏的对象名称、ID 或审核状态作为“跳过项”泄露。
- 蓝图对普通查看者只有 `ready` / `partial` 且 `not_required` / `approved` 时可见；所有者与既有审核者仍可处理自己的非删除工作态。这一条件同步到统一项目解析和关注列表，防止调用方权限漂移。

## D-013：社区项目引用是动态授权关系，不是名称快照

- 已绑定引用保存时按当前提交者 claims 复用统一项目解析器；仅存在于 `public_routes` 不足以引用。创建/更新使用 Repeatable Read，使解析和引用替换观察同一快照。
- 待审修订发布时，以原提交者身份再次解析引用；目标已不再对提交者可见时返回 409，要求修改修订，不让审核动作隐式扩大目标权限。
- 读取先批量取得引用身份，再用当前 viewer 一次批量解析全部项目；不存在应用层 N+1。可见项返回当前名称与 canonical path，隐藏项只返回类型及 `unavailable=true`，不返回 public ID、名称、slug 或原始 identifier。
- 未绑定的手工 identifier 是文章作者显式公开的正文事实，继续以 `unresolved=true` 返回；它与后来隐藏的已绑定对象不混用。
- `modId` 和结构化 `project` 目录筛选也应用当前 viewer 可见性。带目标/资源筛选的请求不使用搜索索引的预估 total，避免数据库最终过滤后仍泄露不一致计数。

## D-014：表情引用、图片和目录采用单一有界生命周期

- 一个 OSS 文件只允许绑定一个表情。generation 90 使用命名 UNIQUE 明确该所有权；换图、创建和删除按确定顺序锁 OSS 行，新图片在事务内再次确认 active，旧图片只有在数据库确认无绑定后才进入删除 Outbox。
- 所有当前 Markdown 字段不再由应用维护手写清单。空库安装在全部内容表建立后发现所有 `*_markdown`、`comments.body` 与 `content_revisions.snapshot`，按每表主键生成稳定 source key，并安装插入、更新、删除触发器。
- Token 引用不外键到可删除的表情，历史事实因此能在表情停用后继续存在；但引用触发器和表情删除使用相同的 `(pack,code)` advisory transaction lock。锁决定线性顺序，删除只能在其锁内的索引查询证明当前无引用时提交。
- 创建表情与删除空包都锁同一 pack 父行。创建先提交时删除重读到非空并冲突；删除先提交时创建重读不到父项。包删除不再在事务外预检或依赖外键等待后的旧判断。
- 公开目录需要一次取得全部可渲染 Token，因此不改成会产生缺页占位的分页协议；改用配置总预算。默认 64 包、每包 128、总计 1024，且代码硬钳制到 256/512/4096。创建端以全局事务锁原子执行预算，公开和后台查询以 `LIMIT + 1` 防御异常数据。
- 数据库故障不再映射为 code 冲突或空目录。只有精确命中相应唯一约束才返回 409；迭代、版本、翻译 JSON 和其他写入错误统一失败关闭。权威 Schema 升至 generation 90，不保留九表子串扫描或图片共享兼容路径。

## D-015：公开表情必须是后端净化派生物，客户端目录必须最终一致

- `sticker-upload` 是一次性的私有输入，不是可发布来源。后端先核对 OSS 行、长度、SHA-256、声明 MIME、魔数和完整解码，再以 PNG/GIF 编码器生成新的随机路径 `sticker_derived` 对象；公开目录和 inline 内容边界只承认 `sticker_derived + trusted_generated`。
- 重编码是元数据策略本身：PNG 不复制 ancillary chunks，GIF 只从解码后的帧、延迟、处置与循环数据重建，不复制 comment/application 文本。尺寸、帧数、总解码像素、动画时长及重编码后字节数都受同一配置预算。
- 派生 OSS 上传与数据库登记之间的失败用直接对象删除补偿；数据库贴纸事务未绑定派生物时用新的事务复核所有者后归档并写删除 Outbox。成功提交后原私有上传归档，替换/删除只管理派生物；模糊提交结果下所有者复核优先于清理。
- 浏览器目录按 locale 在 30 秒窗口内合并请求，不能永久缓存成功 Promise；失败不缓存。后台同进程任何成功目录变更都会显式失效相关缓存，其他已打开页面最迟在 TTL 后刷新。后端 version 保留为目录身份，但不要求 SSE 才能满足有限一致性。
- Markdown 渲染转换保持为纯 AST 操作，只改普通 text 节点并跳过 code、inlineCode、link、linkReference；每文档最多 50 个 Token，未知/停用项显示安全占位。缓存与 AST 逻辑拆成可直接由 Node 执行的纯模块，避免只靠生产构建间接验证。

## D-016：审计统计算法的精度必须服从固定资源预算

- Markdown 活动差异是审计辅助统计，不是业务正文事实，不允许为了对超大重写求最小编辑脚本而按请求长度分配 frontier 或执行平方级工作。
- 先以线性扫描去除公共前缀和后缀；剩余编辑距离不超过 1024 时保持既有精确增删计数。超过预算时把整个不匹配中段保守记为删除加插入，保证不会低报改写量，且 `added-deleted` 仍等于正文净增长。
- Myers frontier 只按距离预算分配 `2*limit+3` 个整数。即便未来某个调用方错误地放宽正文限制，8 MiB 全量改写也只增加线性输入扫描，不会恢复约 256 MiB 单请求分配。
- 当前三个业务入口仍独立限制正文：评论 10,000 Unicode code points，作者每语言 256 KiB，服务器正文 100,000 bytes。算法资源边界是纵深防御，不替代领域输入配额。

## D-017：公共读取不能隐式购买 AI 工作

- `GET /api/v1/content/{publicId}` 是可选认证公共读取，只解析当前已有本地化并返回可解释的 fallback；无论目标语言是否属于八种可编辑语言，都不创建数据库任务、不发布队列消息、不写供应商日志。
- 缺失但存在源语言时统一返回 `request_required`。`automatic=false` 表示不会因读取自动启动；`canRequest=true` 表示调用方可走显式 POST。无源时继续返回 `no_source`，已有精确语言继续返回 `ready`。
- 显式翻译请求对可编辑和其他合法 BCP-47 语言使用同一安全路径：必须有认证 actor 和正的 `user.ai.daily_token_limit`，事务内锁用户、预留 pending Token，任务 `created_by` 非空且 concurrency key 总是包含 actor。
- 删除非 `quotaBacked` catalog 翻译分支和 actor 0 任务身份。支持语言不再享有由匿名流量触发的“免费自动”协议；站内若未来需要预热，必须另建有全局并发/费用预算、可观察和可暂停的后台计划。

## D-018：压缩图片预算必须覆盖完整解码到派生物生成的生命周期

- Base64 输入在分配 decoded buffer 前按编码长度预检，解码后再复核最大 8 MiB；不能把全局 128 MiB JSON 上限当作单图业务预算。
- PNG 先用 `image.DecodeConfig` 读取 header；宽高必须为正且各不超过 4096，总像素不超过 4,194,304（NRGBA 上界 16 MiB），才允许 `image.Decode`。完整解码后的 bounds 必须与 preflight config 一致。
- 一个 catalog entry 可能同时保留 small/large 两张源图并生成 32/128/256 派生物，因此并发令牌覆盖 entry 的整个解码和渲染阶段，不在 `Decode` 返回时提前释放。
- 令牌是进程级共享的 2 槽信号量，所有同时运行的导入作业共用；每作业 errgroup 也限制为 2，等待时响应 context 取消。不能使用每任务各自的 8 路限制来声称存在全站内存预算。

## D-019：Presence 身份与 SSE 连接都是有租期的服务器事实

- 客户端随机 `visitorId` 不是身份事实。匿名 Presence 由服务端 secret 对可信代理解析后的 IP 与规范化 User-Agent 做 HMAC，返回 `p1.*` token 供浏览器保存；任意伪造输入不会改变实际 fingerprint。登录用户继续按用户/Session 事实记录。
- 同一来源在共享 Redis 下最多 60 次/5 分钟，Redis 不可用的本地失败安全上限为 12；限流先于 User Presence、PostgreSQL snapshot 和公共集合写入。在线 Redis ZSET 只保留最新 50,000 个成员，本地回退只保留最新 4,096 个。
- SSE admission 不是 hub map 的副作用，而是 90 秒 TTL 的租约。Redis Lua 同时清理过期项并原子检查全站 2,048、每用户 4、每 Session 2；无 Redis 时同一算法在锁保护的本地 lease map 执行。
- 心跳每 20 秒刷新租约，刷新失败或响应写失败立即关闭；断开幂等释放。30 分钟硬寿命防止连接永久占位，标准 EventSource 可以重连；租约 TTL 处理进程崩溃或未执行 release 的容量恢复。
- 基础设施指标公开当前连接/用户以及 rejected、dropped 和生效上限，容量拒绝使用明确 429/Retry-After，不把过载伪装成已连接。

## D-020：浏览页面身份只能来自服务器闭集

- 浏览请求只能声明 `detail`，或声明 `version:{publicId}`。后者必须是当前 `resource` 的 active `mod_resource_version_details`，且关联的 `mod_content_versions` 同样 active；任意字符串、其他项目类型和属于另一资源的版本都不是页面事实。
- 页面 key 在通过闭集与归属校验后才 canonicalize 并哈希。`content_project_pages` 不再接受客户端创造的永久维度，也不需要用截断或清理任务控制恶意基数。
- 匿名浏览者和限流来源复用 Presence 的 HMAC 边界：viewer/source 都由可信代理解析后的 IP 派生，不再把明文 IP、任意 `pageKey` 或可变 User-Agent 直接拼成持久身份。登录用户用稳定 user identity 做去重和限流。
- 同一来源最多共享 60 次/5 分钟；Redis 不可用时本地上限为 12。同一 route/page/viewer 24 小时只增加一次日计数、唯一访问时间、页面事实和刷新请求，重复心跳仍返回 204。
- 非注册页面明确返回 400 `METRIC_PAGE_INVALID`，容量拒绝返回 429 `METRIC_VIEW_RATE_LIMIT`。不把攻击输入映射成 detail，也不把限流伪装成成功计数。

## D-021：草稿保留期不是无限存储授权

- `user.draft.retention_seconds` 只决定已接受草稿能保留多久，不授予无限条数或字节。每条 canonical JSON 最多 512 KiB；每用户最多 64 条 active、256 条 completed，全部未过期行合计最多 32 MiB。
- 同一用户的保存与完成先取得 `user-draft-quota:{userID}` advisory transaction lock，删除已过期行，再在锁内计算 active/completed/逻辑 JSON 字节和当前 active key 的替换差额；配额判断与 upsert/完成状态转换共同提交。
- active key 替换只按新旧 payload 差额计费，不增加 active 条数；完成把已有 active 原子转入 completed。没有 active 时重复 complete 会建立历史，但在 256 条/32 MiB 硬限内明确返回冲突，不静默删除仍处于保留期的用户历史。
- 单条应用预算在 JSONB 转换前后各检查一次；generation 91 再以数据库 CHECK 防止绕过 Handler 的超大 payload。用户总量采用 JSONB canonical text 长度，避免请求空白与持久表示口径漂移。
- 草稿路径继续不进入需要交互挑战的统一反滥用流程，避免破坏 15 秒 autosave；改用专用无挑战预算：共享 Redis 120 次/5 分钟、Redis 故障时本地 30 次/5 分钟。这样合法多标签编辑可用，单账号覆盖写也不能无限制造 WAL。

## D-022：远端协议计数不能决定本进程分配

- Minecraft Configuration 数据仍先受 2 MiB/包和 512 包/30 秒网络边界，但任何 registry/list 计数还必须能由当前剩余输入字节实现，并从每次主动探测统一的 16,384 解析单元预算扣减；畸形百万计数在创建结果 slice 前失败。
- Frozen、Dynamic 与 Fabric 共用一次探测预算而非各自重置。结果 slice 初始容量最多 256，命名空间 evidence map 最多 4,096；Fabric 累计字节从 64 MiB 收紧为 8 MiB，不再允许 500 万持久字符串。
- Fabric 的 namespace group、registry、entry namespace、bulk 和 entry 都按其最小编码长度验证并消耗共享预算。NBT 即使使用零字节 END 元素也最多遍历 16,384 个节点；NUL channel 改为单次扫描，不先 `bytes.Split` 成百万切片。
- 主动 Configuration 探测是高成本可选阶段，全进程最多同时 4 个。槽满时立即保留已经取得的 Server List Ping 结果并返回容量诊断，不让任意数量 HTTP goroutine排队等待新槽；release 用 `sync.Once` 保证幂等。
- 这一边界不改变 SSRF 决策、合法协议、公开 Mod DTO 或被动健康检查。它只约束普通 `server.create` 用户可指向的不可信公网响应对本进程内存/并发的控制力。

## D-023：反滥用派生记录使用固定并发与可观测丢弃

- 业务 mutation 已提交后，反滥用 fingerprint/event/周期清理是派生防御事实，不得让响应等待数据库，但也不能为每次成功创建 goroutine。成功路径只把有界 snapshot 放入 1,024 深度队列，正文在入队前裁到 20,000 bytes。
- 恰好 2 个 success workers 消费队列，每项从指纹到最多三批 cleanup 共用 2 秒 context；cleanup 不再另起 Background goroutine。数据库慢或连接池耗尽时并发 SQL 最多由这两个 worker 产生。
- 队列满时不阻塞业务响应，也不启动“记录队列满”的递归 goroutine；该项计入 `successDropped`。风险事件原 2,048 队列的 overflow goroutine 同样删除，改计 `riskDropped`。
- worker 由应用根 context 管理。HTTP 停止接收后，API shutdown 等待队列最多 3 秒排空，再在 DB/cache 关闭前结束；超时剩余项被从内存队列移除并逐项计 dropped。关闭与 probe slot release 一样幂等。
- 管理员 infrastructure metrics 公开 success queued/processed/dropped/failed/depth/capacity 以及 risk dropped/depth/capacity。这里选择的是明确 best-effort 派生事实；若未来 fingerprint 变为授权或审核权威事实，必须升级为事务 outbox，而不能扩大内存队列。

## D-024：草稿完成状态只来自一个服务器权威

- completed 不是客户端展示标签，而是对审核事实的私有引用。每条 completed 草稿必须恰好引用本人提交的 `change_requests.public_id`，或本人提交的 `minecraft_servers.public_id`；两者同时存在、两者都不存在和跨用户引用都拒绝。
- 请求协议删除 `reviewStatus`，通用严格 JSON 解码会拒绝仍发送该字段的旧客户端。后端在完成事务内读取权威行的实际状态；pending/approved 原样映射，其余 rejected/conflicted/withdrawn 统一展示为“审核已结束”，不把终态回退成草稿或 pending。
- `projectKey`、标题和相对 URL 只用于本人草稿箱分组/导航，不参与发布、授权或审核状态。权威引用与状态由数据库决定；generation 92 的 CHECK 同时阻止 active 携带引用和 completed 无引用/双引用。
- 列表不提供 `all`。active 与 completed 是显式类别，各自最多 50 条/页；cursor 把类别、派生状态时间和内部 ID 编码为 opaque token，跨类别复用或畸形 token 返回 400。前端分别读取首屏并按类别 cursor 增量加载。
- completed 的排序时间继续优先采用审核单 `resolved_at`，因此审核完成后条目会移动到最新状态位置。这是实时状态列表而非快照导出；同一次稳定数据集中的同时间戳由 ID 严格打破平局。SEC-043 的 64 active / 256 completed 存量上限为排序成本提供第二道硬边界。

## D-025：客户端声明的摘要不是跨用户内容身份

- `oss_files.sha256` 在上传完成阶段仍是对象元数据声明，只能用于定位该用户的候选文件；在 Worker 流式读取并计算实际 SHA 之前，不能据此复用另一文件、另一用户或既有任务的 package。
- 每个 `catalog_import_packages` 永久绑定一个 `archive_file_id`、文件名和上传者。完全相同文件行的创建重试幂等；不同文件即使声明相同 SHA 也建立独立 package，错误摘要只会令自身 Worker 失败，不能替换既有任务的源指针。
- Worker 只有在实际字节摘要相符后才写 `content_verified_at`。verified SHA 索引是已验证内容的查询入口，不恢复可变全局事实；任何未来内容级复用仍须为任务保留不可变来源快照并证明删除/引用生命周期。
- 创建事务以 `FOR KEY SHARE` 锁定仍为 active 且属于当前用户的 OSS 行；删除以 `FOR UPDATE` 串行并在新语句快照中检查活动任务。这样创建先提交时删除能看到引用，删除先提交时创建不能再读到 active 文件。
- generation 93 用唯一文件外键、NOT NULL/RESTRICT 归属和触发器封住绕过 Handler 的来源改写。开发期按整代重建，不为 generation 92 保留危险的 SHA 双写或指针覆盖兼容。

## D-026：每次任务重入都必须产生新的可靠分发事实

- `queued` 不是单独可提交的任务状态。创建、确认、手动重试和启动停滞恢复必须在同一 PostgreSQL 事务中写入新的 Outbox 事件；事件写失败则状态变化回滚，API 不能返回一个永远没有分发事实的 queued job。
- 每次尝试使用新的 `event_id`，`aggregate_id` 继续稳定指向 job。旧事件即使已经 published、消费、延迟重投或进入死信，也不代表后续尝试；重试不复活/改写旧 Outbox 行。
- 启动恢复使用带状态和心跳谓词的 `UPDATE ... RETURNING id`，只为本事务实际取得的过期租约写事件。多实例或重复启动在第一笔提交后不能再次命中已 queued 行，因此不会制造第二个恢复尝试。
- 消费者领取 job 的权威 CAS 仍是 `status='queued'`。同一 job 的旧/新事件重复投递而 CAS 未命中表示该消息已无执行权，按幂等成功 ACK；解析错误和真实 Worker 失败继续返回错误并进入既有重试/死信机制。
- BUG-019 先证明数据库重入本身总会保留新的 Outbox 事实；随后 LEGACY-006 删除了同一任务的 Core NATS/进程 goroutine 旧执行路径。其他任务族对旧开关的使用仍须按各自 Finding 复核。

## D-027：Mod 导入只有 PostgreSQL Outbox 一个执行入口

- Mod 导入任务提交后不做请求内“尽快 Publish”尝试。API、MODID 确认、重试和恢复只提交数据库状态与 Outbox；dispatcher 以固定批次 claim 后才选择 JetStream publish 或本地 handler，不能由 Core NATS 成功改写数据库发布事实。
- 应用运行时无条件启动 Outbox dispatcher，不能再由 `NATS_OUTBOX_ENABLED=false` 关闭已提交 Mod 导入的唯一消费者。该旧配置仍暂时影响其他未收口任务族，但对 Mod 导入没有分支或兼容责任。
- `SubscribeTask` 先把 handler 放入进程注册表，再尝试连接 NATS。因此 NATS/JetStream 不可用时返回的连接错误不撤销本地注册；dispatcher 能从已 claim 的数据库事件同步调用相同带 timeout/租约的 Worker。
- 删除 Worker 启动时对 queued 的一次性扫描、Core `PublishTask`、手工设置 Outbox published 和 2 小时 `context.WithoutCancel` goroutine。进程退出前未处理的行保持 pending/failed，由下一实例继续 claim；不存在只存于内存的第二事实源。
- 这不意味着 Core NATS 从整个系统删除：实时广播仍可使用易失性消息，其他已登记 AI/通知/元数据旧任务入口分别由 LEGACY-008/019 等关闭。可靠 Mod 导入不得回退到这些语义。

## D-028：AI 任务创建服务必须拥有分发事务

- `ai_tasks` 的 queued 行和其分发意图是同一个业务事实。通用 AI、目录内容翻译与社区帖子翻译可以保留各自模型解析、去重键和配额算法，但最终都必须在持有该事务时调用 `enqueueAITaskTx`。
- helper 固定 subject=`ai`、aggregate=`ai_task/{taskUID}` 与 payload `{taskId,taskUid,taskType}`；调用方只选择稳定 event type/trace。它不读取 Outbox 开关、不调用 Core NATS，也不提供提交后的兼容 Wrapper。
- 内容翻译的 advisory lock、同 actor/source revision/locale 并发键和每日 token 预留继续位于同一事务。找到 queued/running/retrying 既有任务时只返回它，不创建第二任务或第二事件；新任务只有 task 与事件都成功才提交。
- Outbox 故障返回错误并回滚 AI 行/配额，而不是先向客户端暴露 task ID、再把它改写成 `failed: NATS unavailable`。队列断连是 dispatcher 的可恢复状态，不是业务任务失败。
- D-028 先删除目录与社区内容共用的专用 publisher；随后 LEGACY-008 把通知翻译也迁移到同一 helper。执行结果/缓存持久化仍由各自业务 Worker 负责，不混入创建事务。

## D-029：通知翻译的队列故障不是 AI 业务失败

- 通知翻译沿用用户级 quota advisory lock、当日 completed/pending 统计与 token 预留，但在提交前必须创建 `ai.notification_translation.requested` Outbox 事件。请求 trace 进入事件，便于从 API 到 dispatcher/Worker 关联。
- Core NATS 没有发布确认，不能决定 `ai_tasks.status`。删除“先提交 queued→PublishTask 失败→再写 failed”的补偿；这三步之间任一崩溃都会产生错误业务事实，而且把基础设施暂时不可用误标为不可重试的翻译失败。
- 新语义中 Outbox insert 失败属于创建事务失败，task/预留都不存在；提交后 NATS/JetStream 断连只改变 Outbox pending/failed/available_at，由 dispatcher 重试或本地执行，AI 行继续正确保持 queued。
- 通知的缓存读取、system 通知禁止翻译、locale/额度和成功 DTO 不变。本项只统一任务创建可靠性；completed 结果解析/缓存写失败问题仍按 BUG-044 修复，不能用可靠入队掩盖。

## D-030：AI 恢复扫描只重建缺失事实或过期执行

- 新任务的 task/Outbox 已原子提交，但恢复器仍负责两类可证明异常：历史/故障留下且完全没有 `ai_task/{taskUID}` 事件的 queued/retrying 行，以及 `updated_at` 超过 15 分钟的 running 行。只要已有任意该 aggregate 的事件，普通 queued/retrying 不重复发。
- running 恢复先在同一事务、行锁下转为 retrying，清除执行时间/错误并刷新 queued 时间，再写新的 `ai.task.recovered` 事件。多实例使用 `FOR UPDATE SKIP LOCKED`；第一实例改状态后第二实例不能再次命中。
- Worker 领取依旧是 `status in ('queued','retrying')` 的单行 CAS；重复 Outbox/JetStream 消息未取得状态转换即幂等成功。注册任务超时为 90–120 秒、默认 queue handler 截止 300 秒，15 分钟留出充足取消/失败写回余量。
- 恢复器启动立即执行，之后每分钟单批最多 100；初次数据库错误不阻止周期 goroutine 后续重试，周期错误写结构化 warning。Outbox dead 表示重试预算耗尽，不被扫描器自动复活，必须走已有管理员 replay，避免永久错误热循环。
- generation 94 为 active task 状态/更新时间与 Outbox aggregate existence 各增加一个匹配索引。10 万 completed、500 active、10 万无关事件的 PostgreSQL 计划必须同时使用它们；完整空库安装仍不能由临时表计划替代。

## D-031：蓝图生产入口只提交一个事务事实

- `blueprint_jobs` 行和 `blueprint.conversion.requested` 事件不可分离。唯一 helper `enqueueBlueprintJobTx` 接受调用方已有 `pgx.Tx`，在一处写 job，并以其稳定 public ID 作为 `blueprint_job` aggregate；payload 只携带内部 job ID。
- 上传完成事务先锁定文件归属和去重，再更新 blueprint queued、登记原始 variant，并在提交前调用该 helper。Outbox 约束或写入失败会回滚上述全部变化，不能留下已显示 queued 但没有可靠唤醒事实的蓝图。
- 转换与重试仍使用 `enqueueBlueprintJob`，但它只是开启事务、调用同一 helper、提交。删除 `NATS_OUTBOX_ENABLED` 条件、提交后 Core NATS `PublishTask` 和上传路径的复制 INSERT，不保留开发期双语义。
- 应用层已始终运行 Outbox dispatcher；`SubscribeTask` 在连接失败前注册本地 handler，所以 PostgreSQL claim 后既可发布 JetStream，也可离线调用同一消费者。这里不把 Worker 自身的 queued 扫描/租约问题宣称已解决，后者仍由 OPS-014 复核。
- 蓝图上传、转换、重试的公开 DTO 和状态码不变；可靠事件写入失败沿既有 500 错误返回。generation 94 无 DDL 变化，未触碰业务数据库永久表。

## D-032：Outbox 指标只能描述已持久化的状态

- `publishing→published|failed|dead` 是带 worker lease 的 compare-and-set，不是无条件 UPDATE。每个单行转换必须命中恰好一行；零行表示租约已丢失或事实被其他实例改变，必须返回错误，不能把它记成成功。
- publish/marshal 失败后的 `fail` 返回持久化结果。retry UPDATE 与 dead CTE 都限定 `status='publishing' and locked_by=current worker`；dead 状态更新和 dead-letter INSERT 继续处于同一 SQL 语句，后者失败会回滚前者。
- Failed、Retried、Dead 和 Published 指标在 SQL 成功且 RowsAffected 为一之后递增。数据库约束、连接错误和 owner 冲突下指标保持原值，行保持 publishing，由五分钟 stale lease 协议重新开放，而不是假称已完成转换。
- claim 前的批量 stale reset 允许零行，但 Exec 错误不再吞掉；每条 claim 的 publishing 更新同样验证一行。错误从 `DispatchBatch` 返回，周期运行器用现有结构化 `slog` 记录 module/error，持久故障具备可观测性。
- 本项不改变重试次数、指数退避、五分钟租约、表结构或公开 API。真实 JetStream 断连重连、多 dispatcher 并发与重启测试仍属于 TEST-026，不能用 session-local SQL 故障注入提前关闭。

## D-033：私聊离线邮件属于消息提交事实

- 收件人不在当前会话时，`direct_messages`、`direct_conversations.updated_at` 与 email delivery intent 必须在同一事务。事件 aggregate 使用刚生成的 message public ID，trace 使用 HTTP X-Request-ID；任一 Outbox 写错误回滚消息和会话时间。
- 私聊 Handler 只构造语义模板参数 `{sender,preview}`，不拼接语言文案。`direct_message_email` 为所有启用 locale 提供值：zh-CN/zh-TW 使用中文初始值，其他 locale 使用安全英文初始值，并可继续由后台模板设置逐语言覆盖。
- Notification Worker 收到 email action 后按收件人当时的 preferred UI language 渲染模板。未启用邮件、未验证邮箱或非 active 用户是明确 no-op；数据库/SMTP 错误返回给 Outbox/JetStream 重试，不能 warning 后 ACK。
- `enqueueNotificationTask` 不再读取 Outbox 开关或调用 Core NATS；独立调用方使用事务 Wrapper，已有业务事务调用 `enqueueNotificationTaskTx`。Worker 产生的 direct/follower email event 在原 notification 事务内写 Outbox，广播 fanout 也写 Outbox 而不是二次直发。
- 外部 API 路由、请求和消息 DTO 不变。`notificationQueued` 表示离线 email intent 已与消息一起提交，不表示 SMTP 已发送；在线会话仍抑制邮件。at-least-once SMTP 的网络歧义不在此项伪装成 exactly-once。

## D-034：内部任务只接受 version-1 Envelope

- task subject 是内部协议，不再同时承载裸业务 JSON。Envelope 必须具有非空 event ID/type、schema version 恰为 1、非零 occurred-at、非空 aggregate type/ID 和非 null payload；JSON/身份任一无效即返回 `ErrInvalidEventEnvelope`。
- local PostgreSQL fallback 与 Core/JetStream subscriber 在调用业务 handler 前执行相同解包；失败进入 dispatcher retry 或 JetStream NAK/dead-letter，不把整个 envelope/裸 JSON误交给业务反序列化。业务 handler 仍只读取 payload，event ID 进入 context。
- 删除 `Client.PublishTask`，源码门禁遍历全部生产 HTTP Go 文件，禁止 `.PublishTask(`。广播 API 属于明确的易失 realtime subject，不经过任务 handler，因此不混淆为旧 task 协议。
- 剩余生产者迁移：关注关系与通知同事务；Mod metadata 创建始终 Outbox，queued orphan/超过五分钟 running 由事务恢复事件唤醒；蓝图已由 LEGACY-013 原子建事件，因此删除重复 queued Core/goroutine scanner；蓝图完成通知走可靠通知 Wrapper。
- Mod metadata 恢复不因删旧协议而丧失能力：多实例行锁选择最多 200，活跃或五分钟内事件抑制重复；stale running 转 queued 与 `mod.metadata.import.recovered` 同事务，重复消费由 queued→running CAS 幂等成功。
- 这是无外部生产者的开发期 breaking cleanup，不增加 schema v0/2 adapter、双读开关或兼容期限。version 2 将来必须显式新增解析与迁移，不允许静默当 payload。

## D-035：NATS 设置只有“完整候选运行时成功后切换”一种语义

- 管理端 NATS `PUT` 是完整替换协议，不是依赖 Go 零值的隐式补丁。连接、Outbox、Realtime、JetStream 全部字段及 JetStream 秒级超时都必须出现；遗漏字段返回 400，不能把“不发送”解释为关闭。
- 密码和 Token 留空继续表示“不替换”，但各自增加 `clearPassword/clearToken` 明确清除动作；清除与非空替换同时出现会拒绝。持久记录一旦存在就是完整权威，空凭据和 `false` 不再从环境复活。
- `ReconfigureWithPersistence` 在独立候选连接上准备 JetStream、所有任务订阅和实时广播订阅。候选回调由代次 gate 阻止提前执行业务；准备失败时不调用持久化，保存失败时关闭候选，二者都不修改当前配置、连接或订阅。
- 持久化回调成功后一次更新客户端代次、连接、JetStream 和配置，再放行候选并 drain 旧连接。实时广播定义即使启动时关闭也会先注册，因此后来启用或重连能恢复订阅；旧代次收到的新消息不会重复分发。
- 已存在的 JetStream stream 只验证其 subject 覆盖，不在“验证候选”阶段静默改写；新建 stream 若后续订阅或保存失败会删除补偿，补偿失败与原错误一起返回。改变不兼容 subject 必须使用显式新 stream/部署迁移，而不是破坏最后可用 stream。
- 这是开发期内部加密设置，不保留旧缺字段 JSON 的字段存在性分支。没有 DDL 或永久数据库重置；真实 PostgreSQL 验证使用 session-local 临时设置表。

## D-036：JetStream 可靠性门禁必须默认执行并覆盖恢复闭环

- 新环境的 JetStream 默认值为开启；示例部署必须以 `-js` 和持久 StoreDir 启动 NATS。显式关闭只允许作为本地应急降级，不把 Core publish 或本地 handler 成功描述为 durable delivery。持久设置一旦存在仍按 D-035 作为完整权威，不能由环境默认偷偷覆盖管理员选择。
- 测试依赖官方 `nats-server/v2` 库在随机本机端口启动 file-backed JetStream，存储位于 `t.TempDir()`。默认 `go test ./...` 无需 Docker、预装二进制或环境变量，因此不会再用 skip 隐藏 PubAck、consumer 与 stream 行为；显式外部 URL 仍可补充部署环境验证。
- Durable queue consumer 必须同时设置 ManualAck、AckExplicit、MaxDeliver 和可观察的指数 BackOff；退避从 AckWait 开始倍增并封顶 30 分钟。测试读取 Server consumer config，并实际观察失败重投、Msg-Id 去重、MaxDeliver 后持久死信、离线 backlog 和已 ACK 消息不重放。
- Server 重启测试复用同一端口与 file store，要求旧 Client 自动重连、恢复既有任务订阅并消费重启后的事件。Outbox 断连测试先确认发布失败持久化为 `failed`/retry 指标，再重启同一 Server，由同一 Client 恢复并把同一事件转为 `published`。
- PostgreSQL Outbox 状态测试使用 MaxConns=1 的 session-local 临时表，完全隔离共享 pending 行。stale publishing 必须能由新 dispatcher 恢复；两个 dispatcher 在第一个 handler 阻塞时必须分割 claim，不能处理同一 lease；OPS-009 的 retry/dead/published SQL 故障注入继续一起运行。
- 死信恢复是闭环的一部分：publish-stage replay 复位原 Outbox 身份，consumer-stage replay 从完整 v1 Envelope 产生新 event ID；两者与 `replayed_at`/管理员审计同事务，重复 replay 返回冲突。PostgreSQL 的 `jsonb_build_object` 参数必须显式 cast，避免扩展协议参数类型推断失败导致恢复永远 500。
- 单进程嵌入式 Server 证明代码和默认门禁，不声称覆盖生产 ACL、多节点副本、跨 AZ 网络分区或容量。后者属于部署演练；不能反过来把缺少生产集群访问作为继续默认跳过仓库可靠性测试的理由。

## D-037：蓝图 Job 以数据库 owner lease 和操作边界决定状态

- `blueprint_jobs` 是执行权威，不以 NATS delivery 是否仍在内存推断活跃。claim 生成含实例身份的唯一 run token，只允许 queued 或已过期 processing 且 attempts 未耗尽的行进入 processing；完成、失败、进度和 heartbeat 都必须同时匹配 `status='processing'` 与该 token。
- lease 为两分钟、heartbeat 为三十秒；progress 更新也续租。活跃重复 delivery 返回 `errBlueprintJobLeaseActive` 使 JetStream NAK，而不是把重复消息当成功 ACK。lease 过期后新 owner 可增加 attempt 接管，旧 token 的完成/失败均为可见的 lease-lost 错误。
- Worker 启动立即扫描，之后每分钟以 `FOR UPDATE SKIP LOCKED` 处理最多 100 个过期任务。预算内任务原子转 queued 并写新的 `blueprint.conversion.recovered` Outbox；事件失败整笔回滚。达到三次预算时 job failed、保留最后错误并让现存 delivery 继续失败进入 broker/Outbox 死信，不无限热循环。
- operation 决定主体状态。normalize 的临时失败把主体恢复为 queued、终态失败改为 failed；convert 失败只改变自身 job，ready/approved 蓝图继续公开。成功通知只能在 job completion CAS 后发送，防止 lease 已丢仍宣布成功。
- 手工 retry 不是“改状态后再入队”。同一事务锁定 owner blueprint，要求当前 failed 且没有 active normalize，CAS queued 后调用唯一 `enqueueBlueprintJobTx`。Job/Outbox/主体任一失败全部回滚；active-operation 部分唯一索引封住绕过 Handler 与竞争窗口。
- generation 95 增加 `max_attempts/locked_by/lease_expires_at`、恢复索引和 active operation/format 唯一索引。当前开发期按整代重建，不为无 owner lease 的旧 processing 行建立兼容扫描；远程 generation 85 不执行升级或重置。
- 本决策本身不解决 OSS 上传成功、数据库提交失败产生的外部孤儿对象；该跨系统补偿已随后由 D-038 / OPS-015 单独关闭，不能用数据库 job lease 代替对象生命周期事实。

## D-038：蓝图派生对象先登记，激活与 Job 完成同事务

- 每个 normalize/cover/convert 派生物在任何 `PutObject` 前，必须以当前 `locked_by + attempts` 验证执行权，并在一个事务中登记 pending `oss_files` 与 `blueprint_job_artifacts`。Artifact 固定保存 job/blueprint lineage、attempt、role、file ID、对象 key 及上传时的 bucket/origin endpoint/region/CNAME 快照。
- ObjectKey 由 `job ID + attempt + role + extension` 确定，不使用随机名称。相同 attempt 不会生成第二 key，新 lease attempt 不覆盖旧字节；数据库唯一约束同时拒绝重复 role/file/key。
- 上传成功不是业务完成。normalized/cover/variant 引用、artifact/file `pending→active` 和持有 token 的 Job `processing→completed` 必须在同一事务；任一 SQL、约束、竞争、lease CAS 或 commit 失败全部回滚为 pending，由失败/恢复补偿。`normalized_file_id` 补齐此前丢弃的规范文件身份。
- 普通失败与过期 lease 在其状态事务中把该 attempt 的所有 pending artifact 标为 abandoned、文件标 deleted，并写现有 OSS deletion Outbox；Outbox 写失败则 job、blueprint、artifact、file 全部回滚。可选封面 Put 失败先完成同一持久补偿，才允许 normalization 继续。
- Artifact 对 job/blueprint 使用 `ON DELETE SET NULL`、对 file 使用 `RESTRICT`，因此级联删除不会抹掉对象事实。启动及每分钟恢复额外清理无精确 processing attempt 的 pending artifact，以及主体已删除的 active artifact；扫描单批 100、`FOR UPDATE ... SKIP LOCKED`，删除操作按 OSS 目标唯一键幂等。
- generation 96 是开发期整代基线，不迁移 generation 95 或远程 generation 85。公开蓝图 DTO、路由、下载 key 读取方式不变；本决策不泛化改写其他业务的 `writeGeneratedOSSObject`，收藏夹导出对象状态机仍由 OPS-013 独立复核。

## D-039：OSS 删除每轮有界，dead 只能经审计重放

- 删除任务以数据库 `processing + locked_by` 为执行权。claim 原子增加 attempts 并写本次唯一 token；完成、retry 和 dead 都必须匹配 token。五分钟过期后新 owner 可接管，旧 owner 的迟到结果不能覆盖新事实。
- 默认每轮最多 12 次。配置/缺凭据、认证、授权和无效目标是需要人工变化的永久类，首个失败直接 dead；限流、5xx、网络和 timeout 为暂时类，指数退避且一小时封顶，达到预算后 dead。未知错误保守重试到预算，不无限循环。
- 若第 12 次执行在状态落库前崩溃，drain 先以 `FOR UPDATE SKIP LOCKED` 扫描耗尽 pending/过期 processing 并归类 `worker_lost`。dead 状态和 `oss_deletion_dead` app log 必须同事务；告警写失败则状态不伪装为 dead，保留租约恢复入口。
- 管理员 GET 只暴露删除目标和失败诊断，不暴露 OSS 凭据；POST replay 只接受 dead，重置本轮 attempts/lease/error 并累计 replay count、actor、time，同时写 `oss_deletion_replayed` 安全审计。pending/processing/completed 返回 409；任务再次 dead 后允许新的受审计 replay。
- `(bucket,endpoint,object_key)` 继续是一条当前删除事实。completed/dead 只有在新的 enqueue 生命周期到来时才重置 pending；同一 `oss_files.status='deleted'` 的重复 tombstone 不 enqueue。因此旧 completed 不会吞新对象删除，也不会被相同旧请求无限复活。
- generation 97 增加 bounded/dead/replay 字段与 dead/exhausted 部分索引。开发期 generation 96 按整代重建，远程 generation 85 不执行 DDL；公开用户删除 API 不变，新增运维 API 使用现有 `admin.config.read/write` 权限。

## D-040：资料图片迁址以数据库 generation 取代进程队列

- 模组首次自动发布、修订自动批准和人工批准都必须在各自原业务事务内调用唯一 `enqueueOSSRehomeJobTx`。任务插入失败令发布回滚；不再允许业务已成功后向 channel 尝试发送并在满队列时只写日志。
- 每个 mod 只有一条当前任务事实，但每次 enqueue 都增加 generation。processing 期间到达的新发布保留当前 owner 并推进 generation；旧 owner 完成或失败时若发现 generation 已变化，只能清 lease 并重新 queued/attempts=0，不能把尚未观察的新资料误标 completed/dead。
- claim 使用实例级唯一 token、三分钟 lease 和 `FOR UPDATE SKIP LOCKED`；单次对象处理有两分钟 timeout，给状态提交保留余量。暂时错误指数退避且最多八次，配置/认证/授权/无效目标直接 dead；最后一次执行崩溃由过期预算扫描归为 worker_lost。
- dead 状态与 `oss_rehome_dead` 应用告警同事务。管理员只可查看任务/模组/失败诊断，不暴露 OSS 凭据；审计 replay 增加 generation、重置单轮预算，并保存 actor/time/count 与原失败事实。非 dead replay 返回 409。
- 对象迁址本身保持确定性目标 key、源 key CAS 更新和同事务旧对象 deletion Outbox。一次任务可部分完成：重试会跳过已经位于目标 key 的项并继续其余项，因此数据库任务重放不会生成随机目标或重复删除事实。
- generation 98 删除 channel、`sync.Map`、`sync.Once` 与全局 goroutine 权威，新增 ready/recovery/exhausted/dead 四个部分索引。公开模组 API 不变；远程 generation 85 不执行 DDL，开发期 generation 97 按整代重建。

## D-041：未发布的 OSS opt-in 与评论举报 Wrapper 不保留兼容层

- 服务端仅根据已经校验的 `sizeBytes` 决定上传方式：小于 16 MiB 为单 PUT，达到阈值必为 multipart。请求 DTO 与六个第一方调用点删除 `preferMultipart`，不允许缺字段/false 形成“旧客户端”第二协议。
- multipart ticket、完成请求和服务端返回的 `method` 保持不变；本决策只删除选择权，不把尚未实现的持久 upload session/过期 Abort 描述为完成，OPS-018 与 TEST-037 继续开放。
- 评论举报只有统一 `POST /api/v1/reports`。删除评论专用 reason/detail DTO、Wrapper、路由及其反滥用 URL 映射；未知 reason 的统一校验不再被旧 adapter 的 other/customReason 规则旁路。
- 两项均无外部发布责任证据，因此不增加 301、代理 Wrapper、双 DTO 或弃用期。后端源码门禁和前端全量检索必须保持旧字段/路由为零。

## D-042：分片上传以持久会话为主闭环、Bucket 生命周期为硬后盾

- OSS 返回 upload ID 后、任何 multipart ticket 返回客户端前，必须持久写入 owner、精确存储 origin、对象 key/upload ID、名称/类型/size/SHA/category/source 与 expiry。登记失败立即尝试 Abort 并令请求失败；`GetBucketLifecycle` 验证的最多七天供应商 Abort 规则覆盖进程在“供应商 initiation 成功、数据库写入前”崩溃的不可消除跨系统窗口。
- Bucket 规则必须 Enabled、使用 1..7 天 `AbortMultipartUpload`、prefix 覆盖当前 `ossRoot`，且没有 tag、size 或 Not 过滤。OSS 启用时应用初始化必须真实读取并验证该规则；缺失、无权读取或供应商不可达均以 `oss_multipart_lifecycle_unavailable` 留在依赖恢复态，不能仅靠文档声称部署前提。
- complete/abort 先以 bucket/origin/key/upload ID 锁行，验证 owner 与全部内容事实，再写唯一 token 和两分钟 lease。相同终态重试直接成功；相反终态、活跃 owner 或篡改请求返回冲突/拒绝。完成调用返回成功或 `NoSuchUpload` 时必须先 HeadObject 精确验证 size 与 SHA 元数据，才允许 durable completed；验证失败恢复 active，使丢失响应可重试但生命周期 Abort 不会被误记完成。
- 清理 Worker 启动立即、之后每分钟处理最多 32 项。到期 active、到期 retry 和 stale aborting/completing 以 `FOR UPDATE SKIP LOCKED` 领取；暂时错误退避、配置/认证/授权/无效目标直接 dead、最多 12 次。最后一次 owner 崩溃归类 worker_lost；dead 与应用告警同事务，告警失败保留旧租约事实。
- 管理员列表只显示会话目标与诊断、不显示凭据；replay 只接受 dead，重置单轮预算并保存 actor/time/count 和原失败审计。公开上传 ticket、完成 URL 与字段不新增第二协议；D-041 的服务端 16 MiB 唯一选择保持。
- generation 99 新增会话表及 expired/cleanup/lease/exhausted/dead 五个部分索引。开发期 generation 98 按整代重建，远程 generation 85 不执行 DDL；session-local 真实 PostgreSQL 状态机与规模计划不冒充完整空库安装或阿里云真实故障/额度矩阵。

## D-043：系统通知只提交模板事实，收件人语言在消费事务中固化

- 审计点名的八类通知生产者不再生成 title/body。它们只提交稳定 `template_key`、声明变量和业务导航 data；编辑员审核、作者认领撤销、审核完成及治理处置在原业务事务内写 `nats_outbox`，事件失败必须令业务事务失败。
- `new_follower` 在持有通知行锁的事务内读取收件人 locale、合并后台模板、渲染 title/body，并一起保存 `source_locale/template_key/template_version/template_params`；同一 title/body 的 email intent 也在该事务内写 Outbox。实际持久快照与用户语言一致，不依赖读取时翻译或 AI。
- 模板设置没有记录时使用完整默认闭集；已有管理员记录自动 merge 新 key。设置查询错误、非法 JSON、recipient locale 查询错误和模板变量错误全部返回；Worker 让 durable consumer retry/dead，不能 ACK 后丢弃。
- `sendTemplatedNotification` 只有 PostgreSQL Outbox 一个持久入口，返回错误并记录不含模板 values 的结构化日志。删除直接 notifications INSERT fallback、治理 fixed-title Wrapper 和 follower 文案函数；普通系统广播仍保存管理员明确输入的 source locale，用户生成评论摘要不是后台系统模板。
- 本批无 DDL、无公开 DTO 或通知存储结构变化，generation 保持 99。旧 helper 是无外部责任的内部开发期实现，直接删除；真实 PG 临时表验证业务/事件原子性、review subscription 标记回滚以及 follower notification/email 快照。

## D-044：公开合成表以 canonical definition 为首选，缓存代次是发布事务事实

- `recipes + recipe_definitions` 是人类可编辑的公开权威。公共列表、类型详情与直接 render 使用同一 `public_recipes` 投影；同一 recipe 同时存在 definition 和 import observation 时只返回 canonical，只有没有 definition 才选最新 active observation。不为手工发布制造假的 import revision/snapshot。
- canonical recipe 使用 `recipe_layout_templates/slots/bindings/candidates` 一次页级查询投影成现有公共 layout；imported recipe 继续使用其 observation hydrator。内部 entity/authority 标记只参与组装，响应前删除；公共 DTO、路由和前端卡片结构不分叉。
- 合成类型页催化剂先按整页 type IDs 一次读取 canonical，再对无 canonical 的 IDs 一次读取 latest observations；所有 observation JSON 先验证为 array of objects。跨批 public-ID 映射和 export resource decoration 各执行一次，不能把非法结构解释为空。
- generation 100 新增单行 `catalog_dataset_state`。缓存请求只读取其 bigint version；canonical 发布/归档/localization、自动/embedded import activation、人工 activation/rejection 在各自事务内递增。bump 失败回滚发布，不能提交新数据却沿用旧缓存代次。
- 100k revision fixture 下 EXPLAIN 只访问 singleton。由于新查询没有 revision table、聚合或数据规模输入，该结构证据适用于更大历史；本批明确没有向共享远程库制造 1M/10M 行来包装基准。远程 generation 85 不重置，完整 generation 100 空库安装继续是外部门禁。

## D-045：导入观察与公开治理分离，物化身份和归属失败关闭

- 未知 registry 不再共用 `import.document`。规范化 registry 只参与 SHA-256，kind 使用固定 96-bit 十六进制后缀；攻击者文本不进入 schema key，代码长度固定，不同 registry 与相同 object ID 仍构成不同全局资源身份。已知 Minecraft 白名单及其短名/`minecraft:` 别名保持。
- 自动 import upsert 只可把 `placeholder` 变 `active`。`archived` 可以接收新的 resource/tag/recipe observation，便于审计和后续人工判断，但 status/archived_at 不变；只有 catalog editor 的显式发布事务可以恢复公开状态。开发期不增加 archive tombstone 双表或兼容双写。
- 所有会物化 document、registry resource、block binding/entity、JEI type/template/recipe 的条目必须精确匹配 manifest 建立的 revision namespace。即使包只有一个 revision，也不得把未知 namespace 强制归属；错误携带 asset/entry/recipe type 上下文并令 Job 失败。
- 引用解析把 requested resource family 作为硬谓词，之后才考虑 preferred revision、Minecraft version、loader、namespace 和新鲜度。import observation 与 manual content fallback 使用相同原则；没有同 kind 资源时保持未解析，不允许用同 canonical ID 的另一 kind 制造可见错链。
- 本批不改公开 DTO、路由或 Schema generation。真实 PG 证明 archived observation、unknown registry identity 以及 imported/manual 跨 kind 选择；远程 generation 85 只作为 transaction/session-local 测试连接，不执行永久 DDL 或治理写入。

## D-046：Mod 导出公开浏览只提供绑定范围的稳定页，损坏事实失败关闭

- Tag 列表按 `(registry,canonical_id)`，成员按既有 `(tag_snapshot_id,raw_member_id)` 主键做 keyset；公开 `all=1` 与 10000 行分支删除，两种页面的硬上限均为 200。搜索只接受前缀并归一化小写，不再用前置通配让普通 B-tree 失效。
- 资产索引的路径模式按 `asset_path` 去重分页；完整模式给 text/binary/media 固定 source ordinal，以 `(asset_path,source_order)` 稳定分页并保留跨表同路径的两个事实。服务端硬上限 500，不把公开浏览接口重新包装成同步完整导出。
- cursor 是有长度上限的 opaque JSON/Base64 值，内含 revision、过滤 scope 和最后键；revision、registry/query/tag 或 `pathsOnly` 不匹配即 400，不能把旧页游标跨数据集重放。`limit+1` 只用于判定 `hasMore`，响应不超过调用方 limit。
- revision summary、registry、entry、document、structure、Tag 成员和模型 variant 的持久 JSON 必须满足预期 object/array shape；迭代器必须在响应前检查 `rows.Err()`。合法 JSON 的 `null`、数组或标量不再被解释为空对象，错误携带 revision/tag/member/variant 上下文并返回失败。
- 本批无 DDL，generation 保持 100，复用 Tag/成员/三类资产主键。100k 与实际 1M session-local 临时行下成员和资产查询均命中 revision/key 主键并在 cursor 后只返回 100 行加 `hasMore`；远程 generation 85 没有永久写入。开发期直接改变三条未发布浏览响应，不维护 `all=1` 或无界数组的第二协议；仓库内没有这些索引端点的前端调用方。

## D-047：Mod 导出资源身份、语言与审核必须沿用站点单一合同

- `catalog_entities.public_id` 在 Mod export 公共 DTO 中只叫 `publicId`。registry/document/Tag/entry/resource source 与 blueprint material 不再把同一字符串同时称为 `entityId`；entry/tag detail 请求参数也改为 `publicId`。真正的 bigint entity ID 只在数据库和内部组装中存在，不能靠同值双字段预留假兼容。
- 第一方 `ModExportEntryDetail`、资源引用链接和 blueprint viewer 同步迁移。站点仍在开发期，直接删除旧字段/参数，不做双读、双写或 fallback；Tag/资产浏览端点没有仓库内调用方。其他 API 若确有不同身份语义，必须按自身 Finding 处理，不能借本次全局字符串替换。
- entry detail 先读取登录用户的 primary/secondary preference，匿名时读取 Accept-Language 与统一 fallback。只有 query 明确提供、长度有界且符合 locale tag 的值才能覆盖；空或非法参数不再先变成 `zh-CN`。同一个 resolved primary 用于本地化内容、导入翻译、知识页与 recipe 装饰。
- export revision 的 approve/reject 说明最多 4096 bytes，trim 后与 actor、decision、前后 status/active、mod/version/namespace/source kind、IP/User-Agent 一起写入既有 append-only `audit_events`。审计写入和 revision 状态、资源同步、模板提升及 cache version 同事务；审计失败必须回滚，不能把结构化应用日志当权威历史。
- 本批无 DDL，generation 保持 100。真实 PG session-local shadow table 验证审核提交与 CHECK 故障回滚，也验证资源/Job 错误 JSON shape 失败；远程 generation 85 无永久写入。管理员活动日志已经聚合 `audit_events`，因此不新增私有 note 表或只能在单一路由读取的审计孤岛。

## D-048：导入内容投影以 version 事务锁串行，冲突只忽略明确幂等资源

- 每次 `syncImportedResourcesToContentVersionTx` 在任何投影读写前，以 `mod-content-import:<versionID>` 的 64-bit hash 取得 transaction advisory lock，并对 active `mod_content_versions` 行 `FOR UPDATE`。export Worker、embedded import 与管理员 activation 都只能经此入口，因此同 version 的 max ordinal 读取、section 创建、placement 清理/插入在事务间严格串行。
- advisory lock 解决跨连接互斥，row lock 同时验证 version 仍 active 并与对该行采用锁的后续写者协调。锁随 commit/rollback 自动释放，不引入进程内 mutex、租约表或失锁窗口；不同 version 的导入不互相阻塞。
- resource placement 的唯一允许幂等冲突是 `(version_id,resource_id)`，因为一个版本只放置一个资源。`ON CONFLICT` 明确指定该键；相同 section ordinal、placement identity 或其他约束冲突必须返回并令激活事务回滚，不能以成功响应漏放资源。
- 本批无 DDL、公开 API 或前端变化，generation 保持 100。真实 PostgreSQL 两连接使用相同 advisory key、各自 session-local version table，证明第二事务等待第一事务释放；独立唯一约束夹具证明同 resource 忽略而 ordinal/identity 为 SQLSTATE 23505。BUG-018 的来源差异撤销和 REUSE-001 的映射单一权威不在本决策中伪报完成。

## D-049：最新导入是来源范围集合，不是对旧投影的无删除合并

- generation 101 为 `mod_resource_version_details` 增加 `projection_source` 与 import namespace/kind/revision。manual 行必须没有 import scope，import 行必须有完整 scope；部分索引按 version/source scope/resource 支撑重导入差异。开发期从 generation 100 整代重建，不迁移无法可靠反推的旧合并 provenance。
- 激活一组 revision 时先得到 incoming source scopes 与 resources。只归档同 scope、当前集合已不存在的 importer-owned detail，并删除其 import placement；另一 namespace/source kind 不进入候选。重命名因此表现为旧 identity 归档、新 identity 激活，不靠 canonical 文本猜测。
- importer-owned 现存行用新 snapshot 的 definition、icon、render 和 revision 精确替换；不再 JSON concat，因此旧 key/旧媒体引用可删除。import provenance localizations 对当前资源先删后建，human/AI/human_corrected 行不删不改。manual detail 冲突不被 import 接管；manual reserve/publish 会显式清除旧 import scope。
- section 创建和 placement 使用同一 `importedContentTemplateCodeExpression`；loot child 与 placement 使用同一 category expression。helper 只接受编译期 `resource`/`snapshot` alias，既没有用户 SQL 输入，也没有第二份 mapping。19 个 kind 与八类 loot case 在真实 PostgreSQL 执行。
- 本批无公开 API/DTO 变化。远程 generation 85 仅提供 session-local shadow tables，没有永久 DDL；完整 generation 101 空库安装仍因维护库 pg_hba 与业务账号无 CREATEDB 保持外部门禁。`overwriteExistingImportData` 不再使 importer 越权覆盖 manual ownership；同一 import scope 总是收敛到其最新 snapshot。

## D-050：自由文本加载器识别以 token 为边界，复合名称先于独立名称

- GitHub topics 与 README 仍是该供应商缺少结构化 loader 字段时的输入，但只能生成四个站内已知枚举。文本先小写，再以非字母数字字符切成 token；不把任意子串、词形或描述性单词当兼容事实。
- `neoforge`、`neoforged` 及相邻 `neo forge` 是 NeoForge 的明确形式。解析 `neo forge` 时同时消费两个 token，避免内部 `forge` 再命中；只有另一个独立 `forge` 或 `minecraftforge` token 才同时声明 Forge。Fabric/Quilt 也只接受其独立 token 或明确 `fabricmc`/`quiltmc` 形式。
- 输出顺序固定为 NeoForge、Fabric、Forge、Quilt，并按集合天然去重。测试同时证明 `NeoForge and Forge` 保留两个显式事实，而 `forged`、`quilted` 等包含词不产生兼容性。
- 本批无 DDL、公开路由、DTO 或前端变化，generation 保持 101。它只关闭 BUG-020；次要请求错误、来源语言、版本/文件选择和把完整正文用于其他结构化分类的 ARCH-012、BUG-073、BUG-074、BUG-121 不借此决策关闭，后续由 D-051 独立验证。

## D-051：外部导入只能把来源事实映射为结构化草稿，缺失事实必须确认或失败

- 当前 Job 只有 queued/running/completed/failed，没有 partial 与结构化 warnings。因此作者、版本、正文或下载地址只要被草稿使用，就属于完成所必需的依赖；429、5xx、非法 JSON、超限和传输错误全部令任务 failed。GitHub README 的 typed 404 是唯一明确“资源不存在”语义，可成功为空；不能用字符串比较错误文本。
- 简单项目的分类输入改名为 `StructuredValues`，只接 provider category/section/loader/file metadata。标题、摘要、正文仍原样进入内容字段，却不参与 platform/category/feature/resolution/performance 计算；本批不新增没有置信度或确认 UI 的正文建议协议。
- Modrinth 只选择 listed release 中带发布时间的 primary `.mrpack`；CurseForge 只选择 available、approved、release、非 server/alternative/child 的 `.zip`。候选按发布时间、稳定 ID 和文件名排序，无合格项即失败，不回退 beta/alpha/附加包。缺失 CurseForge download URL 的补充请求也必须成功。
- 两平台不能证明正文语言，Job result 使用 BCP-47 `und`，不是英语、中文或提交者 UI 语言。导入校验允许它进入编辑器；正式 create/update 只接受站点八种可编辑语言。前端把 `und` 变成空的 required 选择，用户必须确认后提交。
- Job result 新增只读 `importSelection`，记录 provider/project/version/file/releaseType/publishedAt 并在前端展示。前端提交前删除，后端正式 normalize 再清除，防止它被客户端伪造成持久业务事实。无 DDL，generation 保持 101；已有路由和 failed/error 协议复用。

## D-052：Provider 客户端只返回受验证快照，领域 Adapter 不重复传输流程

- Modrinth 共享快照固定读取 project、project versions，并在 team ID 存在时读取 members；项目 ID/type 缺失、任一请求失败或 JSON 损坏均不返回半快照。团队成员到统一 author payload 的 trim/fallback/kind 规则只保留一份。
- CurseForge 共享快照接收领域声明的 class ID，统一构造 game/class/slug/pageSize 查询，并同时验证返回 slug 与 class；description 是同一快照的必要字段。作者 trim 与统一 kind/role 映射只保留一份。
- Mod、Modpack 与 Simple Project 每个平台恰调用一次共享层。它们继续独立负责真正不同的项目类型约束、分类、兼容性、整合包文件选择和目标 DTO；没有创建接受任意回调/Map 的巨型 mapper，也没有旧函数名 Wrapper。
- Modrinth Mod 因共享快照现在也执行 versions 请求，即 provider 的 project/version/team 故障语义与其他入口一致；Adapter 仍按其现有项目字段生成兼容性，不擅自改变领域映射。本批无 Schema、公开 API 或前端变化，generation 保持 101。

## D-053：轮询状态机共享机制，领域入口保留语义

- `waitForPolledJob` 只依赖 `load`、terminal set、progress callback、AbortSignal 与可替换 delay，因此可以用纯 Node 测试验证状态序列、间隔和取消，不知道 Mod、URL 或认证。
- Mod export 与 catalog import 共用一个 `waitForImportJob` API 绑定，终态集合只有一份：confirmation_required、ready、partial、failed、cancelled。公开函数只构造各自 URL；调用方名称与签名不变，因为两者仍表达不同领域操作而非兼容 Wrapper。
- 默认 delay 使用 `globalThis.setTimeout/clearTimeout`，浏览器和 Node 语义一致；每次 load 前检查已取消，等待中取消清 timer 并返回 AbortError。没有借本次改动增加退避/可见性暂停等新协议。
- 本批无后端、Schema、路由或 DTO 变化，generation 保持 101。测试脚本显式纳入 polling 行为与源码闭集，防止以后把第二套 loop/terminal set 写回领域文件。

## D-054：共享文件采集边界，附件预算和持久语义留在领域层

- `FileDropZone` 是编辑员申请和个人作者认领的唯一文件采集边界，统一处理 drag-depth、drop、accept、disabled、隐藏 input 与选择后 reset；两个业务组件不再持有 input ref、click 代理或 change/reset 实现。
- 两类证明附件原来都接受任意文件，因此传入空 accept，不借复用改动收窄已有协议。`multiple` 与上传期间/达到数量上限的 disabled 状态通过共享组件生效，鼠标选择和拖入经过相同过滤与回调。
- 编辑员申请仍按选择顺序上传最多 10 个文件，使用 `project-editor-application` source，并提交自身 attachment IDs。作者认领仍在上传前校验最多 5 个、合计 10 MiB，使用 `creator-claim` source，并保留通知、去重和 proof file DTO；这些不同的业务规则不塞入通用交互组件。
- 本批无后端、Schema、路由、DTO 或 OSS ticket 变化，generation 保持 101。源码回归覆盖审计点名的两个入口，要求共享组件存在、raw file input/ref 不存在，且每个领域仅保留一个上传调用和稳定 source。

## D-055：审核与来源事实严格解析，未知协议状态不得被推断为成功

- `localization-boundary.mts` 是本地化响应的单一前端边界：统一 locale/name/summary/contentMarkdown/revision/source locale/editable/updatedAt，显式接受 original/import/human/ai/human_corrected 五种 provenance 与 draft/pending/approved/rejected 四种 localization review status。
- 已发布对象与 mutation 只接受 pending/approved/rejected。显式未知值、缺失 mutation reviewStatus、未知 provenance、空 locale 或非对象 localization 抛出带字段上下文的协议错误；可选顶层 reviewStatus 缺失只保持 undefined，绝不推断 approved。
- catalog editor/resource、resolved content 和 recipe editor 删除各自 mapper。领域层继续负责 Tag 成员、resource definition/media、content resolution/translation state、recipe canvas/slots/bindings 等真正不同的映射，不创建知道所有 DTO 的巨型 parser。
- 合法后端响应和公开 DTO 不变；这是前端读取边界的失败关闭。无 DDL、后端、路由或持久协议变化，generation 保持 101。行为测试覆盖完整枚举与无效值，源码门禁阻止四模块重新引入局部 mapper 或 approved fallback。

## D-056：短链解析必须有可重试错误终态，请求代次随组件生命周期取消

- 页面状态显式分为 loading、not_found、error。只有格式非法或 `ApiError` 404/410 显示不存在/不可访问；5xx、网络断开、Fetch/JSON 异常、成功 envelope 缺少非空 target 都进入可重试 error，不再滞留 loading 或把暂时故障说成永久不存在。
- 每次请求创建 AbortController 并把 signal 交给 `apiRequest`；effect cleanup abort。成功导航与 catch 写状态前检查 signal，参数变化/卸载后的旧请求不能覆盖当前页面或发起迟到导航。
- retry 是用户显式动作：先回 loading，再递增 attempt 触发全新 controller/request。页面不做无界自动重试，也不缓存目标或从异常内容猜测 URL；404/410 与非法 9 字符 ID 不显示重试。
- 本批无后端、Schema、路由或 DTO 变化，generation 保持 101。新增双语暂时失败文案；纯分类测试覆盖 404/410/500/非 API，源码门禁覆盖 signal、cleanup、retry 与旧 ApiError-only catch 删除。

## D-057：未发布的公开治理重定向不构成兼容合同

- `/catalog/resources` 只是开发期把全部 query 参数复制到 `/admin/global-resources` 的 App Router 页面；当前导航与权威治理页面都使用后台路径，没有外部发布、数据迁移或第一方调用责任，因此直接删除页面，不保留 redirect 或旧名 Wrapper。
- `/admin/global-resources` 仍是唯一 UI 入口并继续接受其现有查询参数；`/api/v1/catalog/resources` 是独立的数据 API，不因删除同名 UI 路径而改变或重命名。普通用户不再先进入公开路径再被带去后台权限边界。
- 路由面测试要求后台 page 存在且旧 page 不存在；production build 的静态页从 59 降为 58，manifest 保留 `/admin/global-resources` 且没有 `/catalog/resources`。删除后清理两份可再生 `.next` validator，再由 build 按当前路由重建。
- 本批无 Schema、后端、API DTO 或导航调用方变化，generation 保持 101。站点仍在开发期，不增加 301/308、feature flag 或双路由观测期。

## D-058：运行时翻译目录不保留已被真实首页取代的占位文案

- 当前首页用 `heroDescription`、真实项目统计、热门资源、教程、新闻、最近收录与分类数据组成页面；全仓没有读取 `home.subtitle` 或 `home.activityHint`。两个键仍宣称真实模块/动态接口未来才接入，既无调用方也与当前事实冲突。
- en-US 与 zh-CN 是完整主字典和后台 flatten key 目录的权威；同步删除两个键会从运行时翻译目录移除四个叶子值。其他六种语言基于主字典 spread 且没有独立覆盖，不需要建立删除映射或兼容 alias。
- 未来若需要新的首页副标题，必须以当前组件真实读取的键和现状文案新增，不能复活旧开发占位。测试直接提取两份主字典的 home block，防止死键在大字典中悄然回归。
- 本批无 Schema、后端、API、路由或现有 UI 输出变化，generation 保持 101。只删除明确零调用、无外部格式责任的配置叶子。

## D-059：Outbox 接受成功不能表述为消息总线已经发布

- 系统通知 POST 返回 202 时，Outbox 模式只证明发布意图已在 PostgreSQL 事务中持久化；dispatcher 之后才尝试 NATS 发布，并可能经历断线、积压和重试。因此成功提示不得声称“已进入 NATS 队列”。
- en-US/zh-CN 的 `admin.notifications.queued` 改为“发布任务已被可靠投递队列接受”。这准确描述当前可证明阶段，同时不承诺 dispatcher 已发布、consumer 已处理、站内通知已持久化或邮件已送达。
- 页面 description 仍可说明系统最终通过 NATS 异步发布，因为它描述架构和最终通道，不是本次请求完成阶段。后端 Outbox、dispatcher、NATS subject/consumer、POST/202 和错误处理完全不改，不通过绕过 Outbox 来迁就旧文案。
- 本批无 Schema、API、路由或队列协议变化，generation 保持 101。测试精确提取两份主字典的通知 block，要求接受文案不含 NATS 且明确 reliable/可靠阶段。

## D-060：登录提示只能陈述后端实际承认的资料管理能力来源

- Mod 资料管理的后端能力来自管理员、已认证开发者/团队关系和项目编辑员；提交者或 owner 并不是一个独立授权来源。未登录提示把 owner 与 project editor 并列，会让用户误以为“所有者”身份本身足以通过能力检查。
- en-US/zh-CN 的 `modContent.managerLoginRequired` 同步改为明确列出 verified developer、project editor、administrator / 已认证开发者、本站编辑员、管理员。保留原翻译 key 和 `LoginRequiredState` 调用点，不引入前端自判权限。
- 登录后是否可管理仍由既有后端能力响应决定；`managerDenied`、令牌检查、路由、请求和状态码完全不改。文案只解释可用身份，不承诺当前账号已经获权。
- 本批无 Schema、API 或授权逻辑变化，generation 保持 101。测试直接提取两份主字典的 `modContent` block，禁止 owner/所有者并要求三类真实来源齐全。

## D-061：UI locale 必须是服务端可见并能驱动根 HTML 的单一事实

- localStorage 只在浏览器 hydration 后可读，不能让首个 HTML 响应的正文和 `lang` 一致。仅在局部容器或 effect 中补 `lang` 仍会给屏幕阅读器、搜索引擎和首屏解析器发送错误元数据，因此不能继续作为 locale 权威源。
- 新 `ui-locale.mts` 统一八种标准 code、alias、默认值和 Cookie 协议。根布局从 `mcmods-ui-locale` 请求 Cookie 解析 `initialLocale`，同时传给 `<html lang>` 和 `I18nProvider` 的 server snapshot；非法或缺失值一致回退 `zh-CN`。
- 客户端切换先写同一 Cookie并立即同步 `document.documentElement.lang`，再广播状态变更；localStorage 只保存无业务含义的跨标签通知信号，不再保存或读取 locale 事实。另一标签收到事件后仍从共享 Cookie 取值。
- 读取请求 Cookie 会让共享根布局下的页面按请求动态渲染；这是当前无 locale 路由前缀架构中保证首屏正文与根元数据正确的显式成本。robots 与 sitemap 仍可静态生成；未来若引入 `/[locale]` 路由，可用静态多语言变体替代该成本。
- 浏览器 Cookie 为一年期、Path `/`、SameSite Lax，不包含身份或秘密；无后端 API/Schema 变化，generation 保持 101。源码闭集与纯函数测试共同防止固定 `zh-CN`、注册表漂移和非法 Cookie 回归。

## D-062：精确静音截止时间不能反推为一个相对时长选项

- 评论关注列表协议已经返回 `mutedUntil` 的绝对时间和 `mutedForever`；绝对截止无法可靠还原为 1h、24h 或 7d，因为网络/处理时间经过后都不会精确匹配，且未来也可能由其他入口设定。前端不得用字段是否存在来猜测 24h。
- 当前有限静音在下拉框中以一个 disabled 的“静音至具体本地日期时间”状态展示；1h/24h/7d 保持为用户可显式选择的新命令。这样重新加载不会伪造旧模式，用户选择任一新值才会有意覆盖服务器期限。
- `resolveCommentWatchMute` 明确区分 forever、未来 until、已过期 none 与 malformed invalid。无效服务端时间显示为协议异常状态而不是伪装未静音；永久标志优先于残留截止时间。
- 后端精确响应、PATCH 命令闭集、数据库列和路由不变，generation 保持 101。测试用固定时钟验证 1h/7d 各自保留原始截止，并以源码门禁禁止旧固定 24h 三元表达式回归。

## D-063：禁用版本是不受任何选择入口修改的不变量

- `disabledCodes` 表示当前组件会话不能改变的成员，而不只是把单项按钮渲染成 disabled。单项、父分类复选框和压缩已选标签都是同一个集合变换的不同视图，必须在最终变换边界重复执行禁用过滤。
- 新 `toggleMinecraftVersionCodes` 只对 available 且非 disabled 的目标成员判断“是否全选”并统一增删。完整组中禁用且已选的成员在移除可修改成员后保留；禁用且未选的成员也不会随组添加。父级即使已传过滤列表，边界仍失败关闭。
- 输出按权威 available 顺序稳定排列，同时保留不属于当前可见组的已有值；没有通过过滤操作意外丢弃隐藏/自定义版本。全部目标均禁用时原数组原样返回。
- 当前第一方调用方尚未传 `disabledCodes`，因此这是组件合同修复，不宣称修复了已发生的用户数据。无 Schema/API 变化，generation 保持 101；纯函数测试覆盖包含禁用已选项的三态增删和顺序。

## D-064：浏览器展示 DTO 缓存必须同时有时间和数量预算

- 仅为 Promise 增加 TTL 仍允许长会话在 TTL 到期前装入任意多的不同资源+locale 键；仅加容量又会永久返回未被逐出的旧图标。目录资源图标因此同时采用 5 分钟 stale window 和 128 项 LRU 硬上限。
- 复用现有 `ExpiringPromiseCache` 的并发合并、显式失效和失败删除语义，并为通用实现增加默认 64 项容量。cache hit 通过 Map 顺序提升为最近使用；插入后逐出最老键，拒绝 Promise 只在仍代表当前键时删除，避免旧失败破坏新请求。
- 图标条目仍以规范资源引用+locale 为键，fallbackName 不进入完整 DTO cache；128 项限制的是完整资源展示快照。相同键的同时渲染继续只发一个请求，瞬时故障不形成负缓存。
- 当前资源展示协议没有修订号或统一写后事件，故不伪造 revision-aware invalidation；5 分钟 TTL 是明确最大陈旧窗口。无 Schema/API 变化，generation 保持 101。测试验证 LRU 命中顺序、硬容量以及既有 TTL/失败重试不退化。

## D-065：站点语言 code、类型和内容编辑集合由一个注册表派生

- `satisfies readonly Locale[]` 只能拒绝非法成员，不能证明数组覆盖 Locale union；反过来手写 union 也不能证明显示注册表没有漏项。权威必须是一份运行时注册表，类型与所有 code 视图从结构推导。
- `supportedUILocales` 作为唯一站点语言注册表；`UILocale` 使用 `(typeof registry)[number]["code"]` 派生，`uiLocaleCodes` 从注册表 map，`editableContentLanguages` 直接引用同一 code 数组。内容语言模块不再从 `use client` Provider 反向导入类型。
- I18n dictionaries 和 Minecraft alias 继续使用 `Record<UILocale,...>`：未来在注册表新增语言时，TypeScript 会迫使字典与 alias 覆盖同步。UI label、Cookie、内容编辑器和回退逻辑不会因只更新一份手写列表而静默漂移。
- Minecraft language alias、BCP-47 内容归一化和站点 UI 支持集合是不同责任；前两者仍独立维护外部协议映射，不把外部 alias 错当成站点注册表重复。无 Schema/API 变化，generation 保持 101。

## D-066：公开部署变量在构建入口形成失败关闭合同

- `NEXT_PUBLIC_*` 会在构建期进入浏览器产物，不是秘密存储。仓库提交的 `.env.example` 必须无凭据并完整列出 API、SITE、Yggdrasil 与成对的 Iconfont URL/SRI；`.gitignore` 显式用 `!.env.example` 放行，README 同步说明生产必填、可选项和修改后重建/重启语义。
- `resolvePublicDeploymentConfig` 是 API/SITE/Yggdrasil 唯一纯解析边界。生产 API 与 SITE 必填且只能为绝对 HTTPS URL；全部值拒绝凭据、query、fragment，SITE 还必须只有 origin。Yggdrasil 可选，但一旦配置执行同样安全校验并规范成尾部 `/`。开发缺省仍明确为 `http://localhost:8080` 与 `http://localhost:3000`。
- `next.config.ts` 在生产构建/启动加载时调用完整解析器，因此缺项在生成产物前失败。Yggdrasil 发现头只消费解析结果；robots/sitemap 调用同一 SITE 解析函数，删除静默 `https://mcmods.cn` 回退。Iconfont 可选对继续由既有 `resolveIconfontConfig` 校验精确官方 URL 与单个 SHA-384 SRI，不建立第二校验器。
- 不把公开地址写入后端 Schema，也不新增业务 API、运行时远程配置或双重默认。负向真实 build 证明缺 API 立即退出 1；合法 HTTPS build 的 58 个页面以及 robots/sitemap/header 产物证明配置被实际消费。generation 保持 101。

## D-067：作者目录分页必须保持排序、可见性和 UI 请求代次

- creators cursor 是版本化 Base64URL JSON，但调用方只把它当不透明字符串。scope 的 SHA-256 摘要绑定 kind、query、sort、direction、limit、当前用户 ID 与管理员可见性；未知字段、超长/非法编码、跨筛选/排序/页长/身份复用和非法数值都返回 400，不能把一个查询位置误用于另一集合。
- PostgreSQL 路径使用 `limit+1` lookahead，cursor 保存当前排序真正使用的完整 tuple：name+ID、created+updated+ID、updated+ID、各 popularity metric+updated+ID，rating 另含 count。升降序 predicate 与 `catalogOrderSQL` 完全对应；默认 name 目录和选择器因此不是 offset 扫描。分类 counts 仍由全过滤集合独立计算，不从已加载 items 猜测。
- Typesense relevance 的 `_text_match` 值不由当前客户端 API 暴露，故其 cursor 保存绑定后的固定页位置，并继续使用 `_text_match desc,updated_at desc` 的索引顺序。首请求索引不可用可明确进入 SQL fallback cursor；已发出的 index cursor 若中途不可用返回 503，不能静默换成另一 SQL 顺序造成跳项/重复。前端合并还按 public ID 去重以容忍索引在两次请求之间更新。
- `creator-pagination.mts` 是目录与选择器的同一请求构造和页合并边界。筛选、query、sort、token 或刷新变化提升 generation 并取消首请求；继续加载只在代次仍一致时追加。目录页长 48、选择器 40，均显示明确“加载更多”，不做无界自动抓取。
- GET `/api/v1/creators` 只增加可选 `cursor` 与响应 `hasMore/nextCursor`；原 `items/counts` 和无 cursor 请求保持兼容。无 DDL，generation 保持 101；真实 PostgreSQL 影子表验证 11 种 SQL 排序两页与跨 scope 拒绝，100k name keyset 命中既有 catalog index，执行 0.137ms。

## D-068：Creator 资料、成员关系和职务定义必须是三种写能力

- 旧详情的 `admin` 局部变量同时表示 `admin.*` 与 `content.review` 可见性，复用它生成 `canEdit` 会让内容审核员看似能编辑；同一个 `canEdit` 又无法说明 profile 编辑员是否能替换团队成员或创建全局职务。详情因此只返回 `canEditProfile`、`canManageMembers`、`canCreateRoles`，每项直接从对应后端权限计算，`content.review` 对三项均不产生隐式授权。
- creator POST/PUT 是资料修订协议，只允许 kind、名称、本地化、头像和链接；请求 JSON 只要显式包含 `members`（即使为空数组）即返回 400。自动发布关系函数只在内部快照明确提供成员时进入替换 helper，普通 profile 发布的 nil 成员必须原样保留当前关系，不能以空数组误删。
- 敏感成员快照由 PUT `/api/v1/creators/{publicId}/members` 独立替换：只接受 `admin.*` 或 `team.members.manage`，验证 team、最多 500 项、规范 public ID、复合去重和 160-byte title；锁定 team 后读取 before、替换 approved 关系、读取 after，并在同一事务写 `permission_audit_logs` 的 `creator.team_members.replace`。审计插入或提交失败时关系整体回滚，成功后才刷新 project ACL version。
- 自定义职务仍由既有 POST `/creator-roles` 和 `creator.role.write` 保护；拥有成员管理权并不因此获得全局职务写权。独立成员页面始终要求 `canManageMembers`，只在 `canCreateRoles` 为真时渲染职务创建；资料编辑器完全不加载/渲染这些控件，提交前再用 `creatorProfilePayload` 构造显式白名单对象。
- 第一方前端同步删除旧 `canEdit`，不保留 alias、profile members 双写或旧成员控件。新建 team 先保存资料，再从独立页面建立成员关系；外部组织导入仍可创建缺失作者资料，但不会把关系混入 profile revision。无 DDL，generation 保持 101；真实 PostgreSQL 单连接临时影子表证明资料链接更新不触碰成员、审计成功保存 before/after，强制审计 CHECK 失败后成员保持上一提交状态。

## D-069：授权审核队列的窗口必须是可继续遍历的稳定位置

- `limit 200` 只是一个静默窗口，不是分页协议；当 pending 超过窗口时，后创建关系永远不能被管理员批准，approved/rejected/revoked 超过窗口时也无法完成历史审计。因此四种状态统一使用创建时间升序加内部 ID 的唯一顺序，cursor 保存最后一条 `(created_at,id)`，下一页严格使用 tuple `>`。
- cursor 是 versioned Base64URL JSON，但客户端只把它当不透明字符串。scope 的 SHA-256 摘要绑定 status、limit、当前用户 ID、author 管理权与 team 关系管理权；跨状态、页长、用户或权限视图复用以及未知字段、超长/非法编码、零时间/ID 均返回 400。权限变化后旧 cursor 不能继续读取另一可见集合。
- GET 管理端点默认每页 50，允许显式 1..100；查询使用一行 lookahead，只在确有下一页时返回 `hasMore=true,nextCursor`，终页 cursor 为空。响应继续保留 `items`，不增加 offset 或 total 的第二位置权威；旧客户端忽略新增字段仍只能显示首个有界页，第一方 UI 已同步迁移为显式继续加载。
- `(status,created_at,id)` 是选择和排序的共同前缀，故 generation 102 新增 `idx_content_creator_bindings_review_page`。真实 PostgreSQL session temp 表用 205 条 pending 验证 100/100/5 三页且无重复，用 100k 关系验证 Index Only Scan、100 行执行 0.088ms。远端开发库仍为 generation 85且未被修改；代码要求重置开发库后一次安装 102，不提供开发期增量兼容 DDL。
- 前端 `project-authorship-pagination.mts` 统一 path 与按 public relation ID 去重合并。状态切换、刷新、审核完成后的重载和继续加载都会取消前一请求并提升 generation，迟到页不得混入当前状态；面板每页 50，只在服务端 cursor 存在时显示加载更多。四状态交互和审核 PATCH 协议保持。

## D-070：详情页的不存在必须来自明确 404，暂时故障必须可重试

- “没有数据对象”不能同时代表加载中、不存在和请求失败。Mod、简单项目与整合包详情统一使用 `loading/ready/not_found/error` 判别联合；只有响应对象携带显式数值 404 时进入 not-found，401、410、5xx、网络、解析和未知异常全部进入 error，不能继续借 loading 文案隐藏。
- `useProjectDetailQuery` 是三个公开详情入口的共同请求生命周期：每轮创建 AbortController，route/site/token/locale 变化或卸载时取消旧请求，并在写状态前复核 signal。重试提升 attempt 并启动新 controller；迟到 Promise 即使忽略 signal 也不能覆盖当前页面。
- error 复用 `PageFeedback` 的 danger 反馈，显示领域化失败标题、可用异常描述和统一 Retry；loading 与明确 404 不提供无意义重试。en-US/zh-CN 增加共享 `common.retry` 及三个领域 loadFailed，其他语言沿用主字典 fallback。
- Mod 的本地化内容请求是主详情成功后的增强读取，失败可回退后端主记录；这会得到完整可用详情而非伪装 loading。主 Mod 请求和另两类详情请求的任何非 404 失败均不可降级成 not-found 或永久 loading。
- 本批无后端、Schema、API、路由或状态码变化，generation 保持 102。纯分类测试和三个调用方源码闭集防止重新分叉成三套错误规则；生产构建证明共享 client Hook 不破坏 58 个页面。

## D-071：审核事实的默认显示不能依赖操作文案或调用方覆盖

- 通用组件的默认值是合同，不是占位符。draft、pending、approved、rejected 必须分别显示专用审核事实；“编辑/加载中/确认/取消”描述可执行动作或瞬时活动，不能用来表示持久审核状态，也不能要求每个目录或编辑器调用方记得传 labels 才正确。
- `resolveReviewStatusPresentation` 是两个通用审核组件的共同闭集。合法四态返回稳定 `reviewStatuses.*` key；空字符串、大小写漂移、未来未协商状态和非字符串都返回 `reviewStatuses.protocolError`。前端不会猜测或把未知值映射成最接近状态。
- `ReviewStatusPanel` 对未知值使用红色边框和协议错误文本；`LocalizationStatusBadge` 使用 danger badge。已知状态仍允许 labels 做领域文案定制，但覆盖层不能改变枚举识别，也不承担默认正确性。
- en-US/zh-CN 权威字典完整提供五个 key；其他语言顶层 spread en-US，因此新状态不会变成缺失 key。既有 `ReviewStatus` TypeScript union、严格 API parser 和服务端枚举不改，运行时保护作为组件边界的纵深校验。
- 本批无后端、Schema、API、路由或数据迁移，generation 保持 102。源码回归明确禁止四个 action-copy fallback 返回；生产构建验证无覆盖标签的目录资源和全局目录调用方继续编译渲染。

## D-072：详情 Tab 的单一事实是可分享的 URL，而不是组件内存

- 详情子页影响刷新、分享和历史导航，因此不能同时存于 `selectedTab` 与 query。Mod、简单项目和整合包分别声明完整只读 tab 闭集；`parseProjectDetailTab` 只接受该页面声明值，缺失或非法值显示 introduction，不再只特判 changelog。
- `useProjectDetailTab` 每次渲染直接从 `useSearchParams` 派生 tab，不复制到 state。点击调用 `router.push` 而非 replace，使浏览器后退/前进重新得到历史 tab；同一完整 URL 不重复 push。选中按钮用 `aria-current=page` 暴露导航事实。
- URL 构造只更新 `tab`，保留其他 query 参数及当前 hash。introduction 是规范默认值，选择它删除 `tab`；显式 `tab=introduction` 或非法 tab 可在用户选择默认页时规范化，不保留第二种等价 URL 作为长期输出。
- 已有无 tab 地址和 `?tab=changelog` 完全兼容；此前无法表达的 relationships/data/downloads/gallery/discussion/tutorial/issues/news/mods 等值现在可直接访问。未知值不建立 alias 或猜测跨产品 tab，因为三类页面的合法集合不同。
- 本批无后端、Schema、API 或服务端路由变化，generation 保持 102。纯函数测试覆盖 round-trip/query/hash，源码闭集保证三个详情入口没有 selectedTab/searchParams 特判双实现；生产构建保持 58 页。

## D-073：一次资产保存必须对应一个可审核、可回滚的聚合修订

- 主体 PUT 成功后再串行写各语言会产生无法诚实概括的总体结果：“失败”可能已有部分发布，“重试”又可能重复制造修订。皮肤和蓝图编辑器因此每次只发送一个包含主体、defaultLocale、全部可编辑非空本地化和 reason 的请求；后端在验证完整快照后才开启/推进一个聚合修订事务。
- 蓝图更新原本已支持 `DefaultLocale/Localizations/ReplaceLocalizations` 聚合快照，第一方此前未使用；现在直接启用并补充可选 reason。皮肤更新增加同构字段和 snapshot 标志，默认语言的 name/summary 同步成为主体显示名称/介绍，避免主体与默认本地化仍是两个事实。
- `replaceOwnedAssetLocalizationsTx` 是 skin/blueprint 的共同发布边界：主体 apply、content_subject 默认语言、旧集合删除和规范化 human 本地化插入处于同一 pgx transaction。立即批准在提交前执行；需审核时完整集合存入 content revision，审核通过再由同一 helper 发布。任意插入/提交失败使主体与所有语言一起回滚。
- 资产编辑器只提交 `editable!==false` 且 name 非空的版本；省略的 AI/不可编辑派生版本在聚合发布时失效删除，不能被客户端伪装为人工内容。站点可编辑语言集合有界，皮肤请求另受 64 KiB body 上限。后端仍重新规范 locale、重复项、长度和默认语言存在性。
- PUT skin 的 localizations 为空缺（nil）明确表示旧 metadata-only 更新，不替换现有语言；显式空数组则因缺默认语言失败关闭。旧已持久化 skin revision 没有 replace 标志，审核回放保持 metadata-only。单语言 `/content` 端点继续服务独立本地化/翻译工作流，但资产编辑器不再用多个该请求伪装成一次保存。
- 无 DDL，generation 保持 102。真实 PostgreSQL session temp 影子表先证明两资产各自原子提交主体+两语言，再用 CHECK 强制本地化插入失败，验证主体、默认 locale 和两语言全部保持上一提交。远端 generation 85 未写入。

## D-074：管理配置面必须完整呈现持久化且参与判定的全部参数

- 后台拿到并在保存时回传一个字段，不等于管理员能管理它。`newAccountDays`、`trustedAccountDays`、`trustedMinimumLevel`、`duplicateWindowHours`、`temporaryBlockMinutes` 以及每策略的 `burstSeconds/objectMinutes` 都直接参与信任分类、重复检测、自动限制和速率窗口，必须与其他同源字段一起可见、可编辑，不能依赖 seed/数据库隐式入口。
- 配置 UI 分为启用开关、五级风险阈值+相似度、账户分层/重复窗口/限制时长、逐动作速率/存量四组。每个标签标明真实单位与范围；后续风险阈值的 HTML min 动态引用前级，trusted days 的 min 动态引用 new-account days。策略表从五列扩为七项完整字段，pending 允许 0，其余限制下限 1。
- `validateAntiAbuseSettings` 在发 PUT 前要求所有数字为整数，并按服务端真实边界验证全局值、阈值顺序、账号天数依赖和每动作策略。错误显示明确分组，不发送一个已知会失败或被改写的请求；后端仍是最终权威。
- 审计发现服务端 `NormalizeSettings` 对 log/moderation/challenge/temp 阈值和 trustedMinimumLevel 有边界，但 `ValidateSettings` 未完整执行，导致 PUT 可以返回成功后把管理员值静默替换为默认。Validate 现在按 100/200/300/500/1000 上限和 trusted level 0..1000 显式拒绝；合法范围与 Normalize 一致。
- GET/PUT DTO、system_settings key、默认值和审计保存协议不变，无 DDL，generation 保持 102。兼容变化只针对非法请求：此前可能静默归一的值现在返回 400，调用方必须修正而不能误认为配置生效。远端 generation 85 未写入。

## D-075：工作台详情只有在对象身份与统计区间同时匹配当前选择时才可见

- `selectedID`、详情请求和右侧图表不能是三个松散事实。`AdminProjectDetailState` 使用 idle/loading/ready/error 判别联合，非 idle 状态保存规范化 projectID 与 days；ready 额外保存 DTO。渲染只接受状态 ID、当前选择和区间三者完全匹配的 ready，因而 A→B 的同一渲染帧也不会短暂展示 A。
- 详情响应不是只凭 Promise 归属可信。完成边界复核 `result.project.id` 与 `result.days` 等于请求快照，服务端或代理发生身份漂移时进入当前请求的 error，而不是给 B 标题装配 A 图表。区间切换同样先进入新的 loading，不能把 30 日数据标成 90 日。
- 用户点击项目、从 URL 恢复项目、改变区间或重试时建立对应 loading；清空筛选/翻页同步进入 idle。失败状态不携带 DTO，仅显示领域错误与统一 Retry。列表读取错误独立显示在列表面板，不能污染或被详情成功清除。
- 每个详情 effect 创建 AbortController，并把 signal 传至既有 apiRequest；选择、区间、token、attempt 变化或卸载会 abort 旧请求，回调在写状态前再次检查 signal。可见性绑定仍是竞态的最终防线，不依赖 fetch 一定遵守取消。
- GET `/api/v1/admin/dashboard/projects/{publicId}?days=`、列表端点、DTO、URL project 参数、Schema 与后端均不变，generation 保持 102。行为只从旧的 stale-while-loading/failure 改为明确 loading/error；本管理面优先事实正确性，不保留陈旧图表兼容模式。

## D-076：治理队列的固定上限不是分页；每个状态必须可稳定遍历

- 服务器审核按 created_at,id 升序表达先到先审；固定首 200 条使第 201 条以后永久不可达，不是安全预算。GET 现在默认 50、允许 1..100，并读取 limit+1。只有存在下一行才返回 hasMore=true 与最后保留行编码的 nextCursor，终页游标为空。
- `serverReviewPageCursor` 带版本、scope、createdAt 与内部 ID。scope 由 status/limit 生成；严格 Base64URL+JSON 解码拒绝未知字段、超长、零时间/ID、版本或 scope 漂移。SQL 使用 `(server.created_at,server.id)>(cursor)`，排序元组和 cursor 元组完全一致；不提供 offset 或只按时间的不稳定兼容路径。
- generation 103 为 `minecraft_servers(review_status,created_at,id)` 建立专用索引。既有 catalog 索引中 primary_tag/updated_at 位于排序路径，不能证明该队列。真实 PG 会话临时影子表对三状态各 205 条以 100/100/5 遍历，并在 100k 行上实际命中新索引。
- 前端每页 50，显式“加载更多”按 ID 去重保序。状态变化、刷新、审核后重载和新页请求共享 generation+AbortController；旧请求即使迟到也不能写入新状态。初始加载和追加加载独立反馈，空态只在初始加载结束后出现。
- 请求保留 status；新增 limit/cursor，响应保留 items 并新增 hasMore/nextCursor。省略新参数的旧客户端仍得到有界首个页面，但必须迁移才能读取全部历史。本项只解决 BUG-139 的可达性；每项 proof/link/mod 的 3 次装配查询仍由 PERF-067 追踪，不能因页大小下降而宣称 N+1 已消失。

## D-077：表情原图完成上传后仍是有期限的临时对象，绑定失败必须可精确回收

- 通用 OSS complete 只证明对象存在且哈希/大小符合票据，不证明表情元数据已经引用。表情创建/替换每次把 source 设为唯一 `sticker-upload:<crypto.randomUUID()>`；同内容、同包的并发表单不会复用一个未绑定文件。服务端继续识别旧 `sticker-upload`，让升级前孤儿进入同一收敛路径。
- `withStickerUploadLifecycle` 把“完成上传→表情 mutation→失败 discard”固化为两个创建/替换入口的共同边界。只有 upload 已返回稳定 file ID 且后续 mutation 失败才 discard；上传自身失败不猜测对象。discard 失败不会覆盖更有用的 mutation 错误，因为持久过期任务是最终补偿。
- DELETE sticker-upload-files 需要 sticker.manage 与 sticker.upload，且事务内按 public file ID、current uploader 锁定；非临时来源或其他 uploader 统一不可见。仍 active 且没有 sticker image 引用时调用现有 tombstone/Outbox；mutation 已成功但响应丢失时源文件已 deleted，重复 discard 幂等成功，绝不删除已绑定派生图。
- MaintenanceWorker 每 10 分钟以 SKIP LOCKED、每批 1000 选择 active、创建超过 1 小时、来源为 legacy/唯一临时前缀且未被 sticker 引用的文件。每项在同一事务标记 deleted 并写 `oss_object_deletion_outbox(reason=sticker_upload_expired)`；物理删除、有限重试、dead 与管理员重放继续由现有 OSS 删除 Worker 负责。
- 1 小时是浏览器中断后的恢复/重试预算，最坏发现延迟约 1 小时+10 分钟；显式失败通常立即清理。无新表或 DDL，generation 保持 103。新增 discard API，既有 sticker mutation 的 imageFileId/响应不变；不建立第二套物理删除队列。

## D-078：公开目录的完整可达性由排序同构 keyset 保证，不以扩大固定窗口代替

- 蓝图库的 q、sort、order、limit 与调用者可见性共同定义一个分页 scope。cursor 带版本、scope、排序字段/方向和最后一行稳定键；未知字段、超长/损坏编码、非法数值、跨搜索/排序/页大小/身份复用均返回 400。内部 ID 只作为不透明 cursor 的最终唯一 tie-breaker，不暴露为业务翻页参数。
- name 使用 `(lower(title),id)`；published 使用 `(created_at,updated_at,id)`；updated/collected 使用 `(updated_at,id)`；热度、下载、收藏、浏览、评论使用 `(metric,updated_at,id)`；评分另含 rating_count。升降方向同时决定 ORDER BY 与比较运算符，响应读取 limit+1，只有真实后页才编码最后保留行。
- 为兼容已有第三方，limit/offset/响应 limit+offset 保留；offset 只能用于没有 cursor 的旧请求，禁止二者混合。第一方移除固定60和 offset，只通过每页36、hasMore/nextCursor 显式继续加载。搜索或排序变化先清空旧集合并取消请求；迟到响应受 generation 拒绝，追加按公共 ID 去重。
- 主查询必须先完整读取并关闭 rows，再运行批量 required-mod 查询。该查询又必须在占用 rows 连接前读取一次 OSS 配置；否则连接池饱和（尤其 MaxConns=1）时会因同请求嵌套取连接而停滞。这个执行顺序是分页端点的可靠性合同，不建立第二装配实现。
- 无 DDL，generation 保持103。真实 PostgreSQL 会话临时表以125条对11种 sort、asc/desc 共22种顺序全部遍历，验证每种均零遗漏/重复、终页空 cursor、跨 q 拒绝；测试强制 MaxConns=1 同时证明无嵌套连接自锁。目录是实时视图而非事务快照，并发指标变化通过重新筛选/刷新取得新顺序。

## D-079：批量日志上传以每个文件的持久ID和阶段为事实，整体异常不能抹掉部分成功

- 选择列表先同步建立稳定 file identity，并在任何异步上传前把整批分类为合法或 invalid。坏扩展名立即与原文件名绑定显示，但不阻止合法兄弟；数量仍由既有1..10和去重上限控制。不能在顺序上传循环中到达坏文件时才抛异常，因为此前对象已经产生外部副作用。
- `LogUploadTask`是本次页面任务的唯一状态：pending→uploading→uploaded→creating→ready/processing/failed，另有invalid；uploadedFileId、share和error属于同一文件。上传失败只标记自身并继续；上传完成ID全部一次交给既有POST files，响应必须按fileId回填而不是依赖位置或一次总成功。
- 后端请求整体失败时，所有已上传任务进入failed但保留uploadedFileId；重试直接重新调用创建，不再次上传。后端207中的逐项失败同理保留ID，只重试未完成项；ready项保持share且永不重做。缺项、未知状态或缺publicCode失败关闭，不能把协议漂移显示为完成。
- 既有后端早已逐file执行并返回207 Multi-Status，且`createFileLogShare`按source_file_id+redaction_version复用未过期ready分享；本项不制造新的批事务或后台队列。真实PG临时表对`[已有ready,不存在,已有ready]`连续请求两次，均按输入顺序返回三项且成功code不变。
- 无DDL、generation保持103，POST `/api/v1/log-shares/files`请求/响应不变。原文件仍计个人额度，脱敏副本不重复计费。uploaded ID在当前任务和网络重试中稳定；页面重载不序列化File/凭证，用户仍可从既有个人文件管理恢复源文件，不能声称跨设备上传会话持久化。

## D-080：选择器排除条件必须先于count、offset和limit进入每个权威目录查询

- `excludeSiteId`不是展示偏好，而是“该资源不可成为候选”的集合条件。Mod、整合包和六种简单项目按规范化slug排除；服务器按public_id比较。空值不改变公开目录；非法路径字符或超长值400。复合选择器会把当前目录slug发送给服务器分支，合法非九位slug必须作为无匹配no-op接受，不能让一个无关分支使整页失败。
- 每个端点把同一排除参数同时追加到count与list SQL；limit/offset/page发生在谓词之后。因此total不依赖排除项是否恰好落在当前页，第二页与第一页一致，边界不会少项或多出空页。参数占位符保持服务端固定，调用方文本不进入SQL。
- search-index页的IDs和Total由未协商排除条件的索引服务生成。只要exclude非空就禁用该快捷页并走PostgreSQL过滤；不能先从索引取窗口再本地删除，因为那会复现同一集合分裂。空exclude仍保留原搜索性能路径。
- 前端所有type fetch在URLSearchParams写一次excludeSiteId；单类型直接返回服务端items/total。复合类型先以limit1取得每类已过滤total，再据此消耗全局offset和remainingLimit；删除containsExcluded、当前页filter及“看见才减1”。同slug若在多个目录各存在，则各目录都排除一项，符合站点ID不可选语义。
- 无DDL，generation保持103；四端点只新增可选query，响应DTO不变。真实开发库当前仅Mod有approved夹具，已只读证明total精确减1、第一页/第二页total相同且排除项不回流；其余三端点由同构SQL源码合同、parser、编译与全仓门禁覆盖，不虚构缺失数据证据。

## D-081：可视编辑器不能表达的合成表definition是服务端只读权威，普通编辑必须完整保真或安全失败

- `recipe_definitions.definition`可能包含导入器、历史版本或未来Schema写入而当前可视编辑器没有控件表达的字段。读取它却在保存时新建`{}`会把“未编辑”误作“明确删除”，并让semantic fingerprint随数据损失变化。前端必须从加载记录深层克隆完整JSON并原样放入mutation；保存后的本地记录继续持有同一值。
- 仅靠客户端回显不足以保护旧版或恶意调用方。编辑时服务端按recipe_id读取当前JSONB：请求省略definition是兼容性preserve；请求携带时必须与规范化当前值完全相等，否则返回409且不创建审核快照。通过后快照使用重新读取的服务端对象而不是请求对象，因此发布路径没有第二权威。
- 新建合成表没有可继承定义，只接受省略或空对象；非空opaque数据400。当前可视编辑器没有修改definition的能力，未来若业务需要必须建立版本化Schema、大小/深度/字段验证及明确高级编辑入口，不能借这次往返合同开放任意JSON写入。
- binding与candidate结构继续由模板槽位、规范化绑定和候选关系表达，其既有definition载荷仍在normalization清空；本Finding证据指向顶层`draft.definition`/`recipe_definitions.definition`，不借机恢复已废弃的任意导出器blob权威。
- 旧前端在非空definition上回送`{}`会收到409，而不是静默丢数据；新前端对完整GET值回显。省略字段的旧API调用方安全保留。前后端应同步部署，回滚无DDL但会重新暴露静默清空风险。
- 无DDL，generation保持103。真实开发PostgreSQL仅创建会话临时recipe_definitions/模板/槽位表，证明嵌套对象经完整normalize保持不变且陈旧空回显冲突；远端generation85无持久写入或迁移。

## D-082：资料编辑入口必须消费与对象响应同源的服务端能力，不从登录或探测成功猜授权

- “持有 token”“能读取公开资料”或一个 `/editor` 探测请求成功都不是编辑能力。资料板块列表与资源详情响应现在都附带由同一个 `modContentCapabilitiesFor` 计算的明确能力；前端只把字面量 `true` 当作允许，缺字段、畸形值、旧服务端响应和异常都失败关闭。客户端既有 JWT permission helper 不参与本边界，因为 token 声明可能陈旧，且不能独自表达当前项目身份。
- `manageLayout`、`createResource`、`editResource` 当前都直接派生既有 `canEditMod(claims, identity)`，但协议保留三个独立字段。这样 UI 可以准确对应布局/设置、创建和资源编辑动作，未来服务端分权时无需客户端重新建立启发式映射。匿名、普通登录和不属于该项目的用户全部为 false；scoped project editor 为 true。
- 能力属于每个响应对象和当前身份。资源详情把状态键绑定 site/resource/token，板块页绑定 site/section/token；身份或对象变化时，旧响应在新请求完成前也不能继续显示入口。解析器返回冻结的全 false 对象，调用方不会因可选字段 truthy 或旧状态残留放宽权限。
- 两个读取响应设置 `Cache-Control: private, no-store`，并让 `Vary` 包含 Authorization 与 Cookie，防止带用户能力的 JSON 被共享缓存跨身份复用。响应内容仍可公开读取，只有附带能力因调用身份变化；无能力缓存或第二探测端点。
- 能力只改善 UI 陈述，不替代授权。PUT/POST 写路径继续调用既有 `requireEditableMod`，直接请求、不显示按钮的客户端和权限在读取后被撤销的竞态仍由服务端返回 403。真实 PostgreSQL 会话临时 `mods` 表同时证明普通用户读取为 false 且绕过 UI 直接 PUT 为 403。
- 两个 GET 仅新增 `capabilities` 对象，无新路由、请求或写响应变化。旧客户端忽略新字段；新客户端面对旧后端安全隐藏控件，因此建议后端先部署。无 DDL，generation 保持 103；远端 generation 85 仅使用会话临时表，未迁移或持久写入。

## D-083：通用OSS complete成功是逐文件持久事实，不能等整批成功后才进入编辑草稿

- 通用 OSS complete 返回稳定 `fileId` 时，个人文件已经存在并参与储存额度。后续兄弟上传失败不能把这个事实折叠成一次“整批失败”，否则 UI 隐藏已计费对象，重试又重复上传。共享 `OSSUploadBatchTask` 因此逐文件保存 pending、invalid、uploading、uploaded、failed、原 File、稳定 ID、完整结果和错误。
- `createOSSUploadBatchTasks` 在任何异步调用前一次完成类型与剩余容量分类，并发布完整首个 snapshot。合法项串行执行，但一项异常只将自身标为 failed，循环继续；每个 complete 成功后先发布 uploaded/ID，再调用 `onUploaded` 写领域草稿，之后才开始下一项。retry 由 task key 限定，只处理 failed，uploaded 结果不再上传。
- Mod、整合包和六种简单项目共用同一批处理器与 `OSSUploadBatchStatus`。每个成功图库 record 立即按 `fileId` 去重追加到领域草稿，最多 32 项；这些编辑器的既有自动草稿随后持久化引用。错误项仍显示原文件名、具体错误和独立 Retry，前后成功兄弟同时留在图库和状态列表。
- 资料图标的 32px 与 128px 裁剪结果也是两个 task，不再 `Promise.all`。开始新替换时先清空待提交的旧 pair；任一尺寸 complete 后立即写对应 `iconSmallFilePublicId` 或 `iconFilePublicId`。只要存在 pending/uploading/failed task 就禁止保存，防止把新旧尺寸拼成一组；重试只补缺失尺寸，两个都成功后才恢复业务提交。
- 用户主动重新选图会建立新 batch；主动清除图标会清除 batch 引用。失败 File 本身不写浏览器持久存储，刷新后需要重选失败项；已经成功的 fileId 则已进入自动草稿，且始终可从既有个人文件列表找回。这里不虚构跨设备 File 恢复，也不为可见用户文件增加自动删除语义。
- 无后端请求、响应、路由、额度、扫描或删除协议变化；继续使用既有 presign/complete 与个人文件管理。无 DDL，generation 保持 103，远端 generation 85 未访问。前端回滚无需数据操作，但会重新产生部分成功隐藏和重复计费风险。

## D-084：筛选元数据有自己的有界目录协议，不能由当前结果页反推

- 分页 items 只回答当前 offset 窗口，不能证明完整可选集合。简单项目目录因此删除 `catalogFilterOptions(items, ...)`：Minecraft 版本继续由既有全站版本配置和统一 picker 提供；loader、category、feature、resolution/performance/mapSize 与 license 都是服务端验证使用的静态闭集，只从相同前端注册表呈现，不再混入当前页偶然值。
- add-on 的父项目不是静态枚举，也可能远多于一个目录页。新增 GET parent facets 以 simple project `projectType` 与调用身份定义可见范围，只纳入 approved 或当前用户自己提交的项目，再对已引用父项生成与目录 parent filter 完全相同的 `type:siteId-or-rawIdentifier` key 和稳定显示名。它不依赖当前结果页、page、sort、搜索或其他筛选，因此同一身份的可选择范围不会在翻页/分享 URL 后漂移。
- 父项页按 `lower(label),key` 唯一顺序读取 limit+1；limit 默认 50、最大 100。cursor 带版本、身份/类型/limit scope、最后 label/key，严格 Base64URL JSON 解码并拒绝未知字段、超长和跨身份复用；无 offset。响应只在真实后页返回 hasMore/nextCursor。
- URL 最多已有 20 个 selected parent。端点在正常页面之外用同一可见性查询回显这些 key/label，使选中项即便位于后页也能在刷新首屏正确显示和取消；不存在或已不可见项不被重新泄露，筛选 chip 仍保留原 URL key 供清除。
- 前端 addon 页面以 token/projectType/selected scope 建立独立请求代次，旧请求会 abort 且完成时复核 generation；加载更多按 key 去重。父 facet 故障只在该筛选组显示并可重试，不把主目录伪装为空。桌面和移动筛选面消费同一状态，不各自查询。
- 响应带 `private,no-store` 且 Vary Authorization/Cookie。无 DDL，generation 保持 103；真实 PostgreSQL 会话临时表对 125 approved、本人 pending、他人 pending 和重复 ref 验证 50/50/26 完整遍历与 selected 回显，远端 generation 85 无持久写入。原 GET 目录请求/响应不变。

## D-085：分类重挂的候选展示与状态变更必须执行同一个整棵子树约束

- “目标父级尚未到第 4 层”只证明一个新叶子可以挂入，不能证明一个已有子树可以挂入。重挂后的最大深度是 `目标父级深度 + 1 + 被移动子树高度`；目标深度为 2、移动节点带两层后代时结果为 5，必须在候选阶段就拒绝。
- `createModContentCategoryReparentPolicy` 在 categories 代次变化时一次构建 ID、父子、深度和子树高度索引。`canReparent` 同时验证当前树合法、目标存在、非自身/后代和最大深度；`reparent` 只在同一判定通过后替换 parent 并规范化兄弟 ordinal。候选筛选不会为每个候选复制并重算整棵树。
- 父分类 select 调用 `reparentCategory`，关系拖拽继续调用同一个函数；两者都由同一个 policy 实例决定。下拉不再直接 map/替换 parent，也不再维护近似的 descendant/depth 条件。新增空分类不是移动已有子树，其叶子高度为 0，继续用父深度小于 4 的现有规则。
- 后端仍对完整请求树执行最终验证。分类树错误成为稳定 422：code 为 `content_layout_category_tree_invalid`，details 携带 `field=categories.parentPublicId`、提交的 categoryId/parentId、闭集 reason 与 maximumDepth=4。验证按请求顺序、且在 `new_*` 临时 ID 替换前完成，用户可以定位原草稿节点。
- missing parent、cycle、duplicate、root conflict 与 maximum depth 都走同一 typed tree error；资源归属、资源类型和相似组等非树错误保留既有泛化 422，避免把不相关失败伪装成父关系问题。合法 PUT 请求与 mutation success DTO 不变。
- 无 DDL，generation 保持 103，远端 generation 85 未访问。本项只关闭两种重挂入口的不变量分叉；PERF-071 登记的 2 万资源布局中分类乘资源/分类内平方查找仍独立开放，不能因本策略预计算而一并关闭。

## D-086：以币种code为键的奖励集合禁止隐式覆盖；合并必须是另一项显式业务

- task reward 的 `currencies` 是 code→amount 的集合，不是可拥有重复 code 的行数组。把 `gold=1` 行直接改名为已存在的 `diamond=2` 再执行对象赋值，会得到 `diamond=1`，这不是重命名而是无提示删除另一事实。已有目标因此必须在候选、状态更新和保存三层都失败关闭。
- 前端 `canUseTaskRewardCurrency` 只允许当前 code 或尚未占用的 code；select 对其他行使用的 option 设置 disabled。`renameTaskCurrencyReward` 再独立复核 source/target，冲突返回 null 且不修改输入；成功才删除旧键、保留金额并建立新键。任务保存前按 trim+lower 复核 code 唯一，畸形服务端数据或被绕过的控件不能发请求。
- 服务端唯一性以既有 `normalizeCode` 为定义，因此 `Gold` 与 ` gold ` 也是同一币种。`taskPayload.UnmarshalJSON` 在普通 map 解码覆盖发生前逐 token 读取原始 `rewards.currencies` 对象键，完全相同的重复 JSON 属性和规范化碰撞都返回同一错误；顶层未知字段仍由内部 strict decoder 拒绝。
- `normalizeTaskCurrencyRewards` 对程序化 map 调用方执行第二道相同碰撞检查，然后才查询 active currency。重复请求在任何数据库读取和 task 写入之前返回 400 `task currency reward codes must be unique`；合法请求仍规范化成 code→正整数，未知币种和金额错误语义不变。
- 本次没有新增“合并”按钮，也不猜相加、取代或最大值规则。若未来允许合并，应是命名明确、确认目标和金额规则的独立动作，不能重新借用 rename 事件。旧客户端发送碰撞载荷从静默丢奖励变为安全 400，需让管理员修正后重试。
- 无 DDL，generation 保持 103；`task_definitions.rewards` JSONB、currencies 表与任务发奖读取协议不变。远端 generation 85 未访问；纯解析/状态/API前置拒绝测试不需要数据库影子表。

## D-087：Markdown编辑会话以稳定文档身份和父级基线同步，prop变化不是用户输入

- `value`只参与`useState`初始化会把组件实例误作一个永久文档：locale A输入后切到B仍显示A，后续`onChange`又把A写入B。embedded编辑器因此必须同时提供稳定`documentId`、`value`与`onChange`；身份至少包含实体、字段和locale，切换身份就是明确的新编辑会话。
- 会话保存`documentId/markdown/externalValue/dirty`四个事实。文档身份变化时无条件采用新value并关闭旧Draw.io/媒体插入会话、重置光标；同文档且clean时接受异步加载或表单恢复；用户输入与父级基线不同才是dirty，输入回退到基线则恢复clean。
- 受控父级通常在一次按键后回传刚发布的value。外部值等于本地markdown时只确认新基线并清dirty；父级尚未回声且仍等于旧基线时保持本地输入。若同文档在dirty期间收到真正不同的刷新，只记录新externalValue而不覆盖textarea，避免网络/自动草稿恢复毁掉正在编辑的内容；下一次父级接受本地值时再确认。
- 外部同步不能通过`onChange`回传。所有textarea、贴纸、工具栏、上传占位符替换与异步片段替换只经`commitMarkdown`发布用户变更；prop effect只更新本地会话。这样A→B重置不会使用仍捕获A locale的旧回调污染B，也不制造父子渲染循环。
- 11个embedded入口全部显式迁移：蓝图上传、两种catalog手工编辑、社区正文、创作者、多语言资产、资料资源、Mod、整合包、changelog、服务器与简单项目。原资料资源编辑器的`key={locale}`卸载技巧删除，类型联合在编译期拒绝任何未来缺identity的embedded调用；standalone `/tools/playground`本地/服务器草稿仍保持原协议。
- 无后端API、DTO、DDL或持久化变化，generation保持103，远端generation85未访问。本项只解决受控/多语言会话同步；standalone多标签草稿的版本/并发覆盖属于BUG-151，仍独立开放。

## D-088：草稿写入顺序由数据库revision与编辑会话sequence共同定义，不由HTTP到达顺序定义

- 单独Abort旧fetch不够：请求可能已进入服务端或数据库。`markdown_playground_drafts`因此在generation104新增从1开始且每次接受写入递增的`revision`，以及最近成功写入的`save_session_id/client_sequence`。前端每个standalone组件实例生成独立session ID，并对发出的保存使用严格递增sequence。
- 同一个编辑会话允许`client_sequence`更高的请求推进，即使其base仍是前一个并发请求发出时看到的revision。因此A先到后B仍由B推进；B先到后，迟到A的较低sequence失败。不同session代表其他tab/编辑器，不能借sequence互相信任，必须使`baseRevision`精确等于当前数据库revision。
- INSERT仅允许baseRevision=0或目标行已存在；缺失行携带非零旧base也安全冲突。接受写入在单条`INSERT ... ON CONFLICT DO UPDATE ... WHERE`中更新正文、revision、session/sequence和时间，判断与写入同一行原子完成；没有先读后写窗口。
- GET在无草稿时返回revision=0，有记录时返回正文/revision/updatedAt。PUT严格要求`content/baseRevision/saveSessionId/clientSequence`；成功回显服务器revision、时间和clientSequence。失败返回409 code `MARKDOWN_DRAFT_CONFLICT`，details携带当前服务器正文/revision/时间及本请求sequence，客户端可以显示真实双方选择而不猜测。
- 客户端发新请求前Abort旧controller，但仍按coordinator处理所有可能完成的响应。只有response sequence等于最新已发sequence且保存内容仍等于当前textarea时才能设置lastPersisted、“已保存”和时间；迟到响应仍可提升已知revision，但不能改变UI完成事实。编辑发生在请求与响应之间时，下一次debounce继续保存新正文。
- 409会停止自动保存并禁用普通Save，明确显示“使用服务器版本”和“保留我的版本”。前者用服务器快照替换本地；后者先以冲突revision显式rebase再保存当前本地正文。两者都由用户选择，跨tab变化不会自动被本页覆盖。
- 这是开发期有意收口为单协议：旧无版本PUT和新字段发往旧strict decoder都不兼容，前后端必须协调部署；不保留会重新引入乱序覆盖的双写。generation104要求重置开发库，不迁移远端generation85。未登录localStorage回退没有账号跨tab承诺，仍只作为本地便利。

## D-089：草稿“不存在”与“暂时无法读取”是相反的保存权限状态

- `pgx.ErrNoRows`证明当前用户没有账号草稿，可以安全返回`content:"",revision:0,updatedAt:null`并允许从新基线首次保存。超时、连接断开、Scan类型错误和其他数据库故障完全不能证明无草稿；把它们也返回空值会把未知服务器正文伪装成可覆盖的新文件。
- GET handler只在`errors.Is(err,pgx.ErrNoRows)`时返回200空状态；其他错误返回503、稳定code `MARKDOWN_DRAFT_READ_FAILED`且不包含data。成功记录继续返回generation104的content/revision/updatedAt。所有响应标记`Cache-Control: private,no-store`，账号草稿不进入共享或浏览器持久缓存。
- 前端load失败不能再执行`setDraftLoaded(true)`。失败状态保持false，因此1.8秒autosave的前置门禁不成立，普通Save也显式disabled；页面保留当前内存文本供用户查看，但不能把它解释为服务器基线。错误横幅说明保护原因并提供Retry，重试重新执行完整GET而不是猜revision。
- load成功或权威无行才清失败状态并开启保存；token变化和每次retry都会Abort旧load/save请求、重建save coordinator，避免迟到的旧身份结果恢复保存。未登录localStorage分支不访问账号草稿，仍按本地回退合同工作。
- 无DDL，generation保持104；PUT revision/session协议不变。远端generation85未访问。本决定只关闭BUG-003；`revisionPublicIDValue`吞错与用户文件rows/额度故障分别是ARCH-003/004，继续开放并归入同一`RC-READ-ERROR-SEMANTICS`逐项处理。

## D-090：非空内部修订必须解析出公开ID或使整个详情响应失败

- `published_revision_id == nil`是权威的“尚未发布”，此时`publishedRevisionId:null`正确且辅助函数不得发查询。非nil内部ID则声明一条应存在的`content_revisions`关系；no-row表示引用损坏，连接/超时/Scan错误表示状态未知，都不能降级成同一个null。
- `revisionPublicIDValue`保留原名但改为`(*string,error)`并直接返回底层解析结果，让编译器强制每个调用方迁移。资源、标签、配方类型、配方模板、配方和创作者六个详情Handler都先解析并检查error，再写成功JSON；没有保留忽略error的便利Wrapper。
- 非nil反查失败统一返回500 `{error:"failed to resolve published revision"}`，不发送部分详情，也不把故障状态缓存成有效200。正常公开ID和真正nil的成功DTO字段形状不变；客户端只需按既有非2xx路径处理。
- 本项不顺带修复同文件中的其他best-effort读取，以保持Finding证据边界；用户文件列表rows迭代和额度聚合错误仍由ARCH-004独立验证。无DDL，generation保持104，远端generation85未访问；回滚无需数据操作但会恢复200/null伪装故障。

## D-091：文件列表和额度只有在所有数据库读取完成后才是可用快照

- `Query`成功只证明结果流建立，不证明全部行已传输；驱动可能在若干`Next`后因网络、取消或解码故障停止。`userOSSFiles`因此必须在循环结束、访问URL生成完成后检查`rows.Err()`，且检查位于唯一200 JSON之前；任何流错误返回500，已积累的部分数组不发送。
- 每日和总额度是同一个客户端预检快照的两个组成部分。任一聚合失败时，另一个数值即使已读到也不能与默认零值拼成成功响应。`loadUserOSSFileQuotaUsage`按每日、总量顺序读取并逐次返回error；Handler只在两次都成功后计算权限limit和发送DTO。
- 成功协议不变：daily继续以source用量作为`usedBytes`，total继续以stored用量作为`usedBytes`，并保留各自source/stored值、single limit和扩展名。失败分别使用现有500服务错误`读取用户文件失败`与`读取用户文件额度失败`，客户端不得把失败解释为无文件或零占用。
- 这项读取修复不声称替代上传执行端的额度强制；上传授权与并发配额仍按对应OSS Finding独立验证。无DDL，generation保持104，远端generation85未访问；回滚无需数据处理但会恢复截断列表和伪零额度风险。

## D-092：单用户陈列的驱动集合必须是该用户的权限，而不是全站项目

- 旧查询先把七类所有公开项目UNION成集合，再为每行分别执行developer/editor两个`effective_project_access` EXISTS，最后过滤目标用户。全站项目规模因此位于权限谓词之前，末尾`LIMIT 100`无法约束前置扫描。
- 新查询先以`user_id=$1`和developer/editor闭集读取`effective_project_access`，按`project_type/project_id`分组并用`bool_or`合并重复权限来源。该小集合显式`MATERIALIZED`一次，再由七个类型分支按项目主键连接并复核各自公开状态；public route、更新时间排序和100项上限留在最终结果。
- 不把角色来源去重成任意单值：同一项目同时拥有developer与editor时两布尔值都保留，现有Handler继续让developer陈列优先，故成功DTO与前端分组语义不变。待审项目、其他用户权限和没有公开route的项目仍不能进入响应。
- 规模验证直接执行生产SQL常量：会话临时视图让一张实际装载并ANALYZE的project ID表同时代表Mod与公开route规模，依次从100k扩到1M、10M。三个`EXPLAIN (ANALYZE,BUFFERS)`均以主键Index Only Scan按用户权限ID探测，无规模表Seq Scan；执行时间分别0.316ms、0.393ms、3.371ms。
- 无DDL或新索引，generation保持104；现有`idx_project_editor_assignments_user_active`、`idx_creator_claims_user_approved`、binding/member访问索引及项目/route主键继续提供生产驱动。远端generation85只建立session temp对象，10M行在连接关闭时自动清理，无永久读写。

## D-093：关注关系页只能按稳定事实继续，页码和总页数不属于遍历协议

- 旧`page<=10000`与`pageSize<=60`允许数据库丢弃约60万索引项，且列表每翻一页都重新COUNT。新公开followers/following协议只接受1..60的`limit`与不透明`cursor`；出现`page`或`pageSize`即400 `USER_CONNECTION_CURSOR_INVALID`，不把旧页码悄悄解释成第一页。
- 两个方向都按`created_at desc, connection_user_id desc`形成唯一稳定顺序。followers的tie-breaker是follower_id，following是followed_id；下一页使用严格元组`<`谓词并查询limit+1，只返回limit项及`hasMore/nextCursor`。游标严格JSON/base64url解析，并以user内部ID、方向、limit的摘要scope防跨用户、跨列表或跨页宽复用。
- generation105把原单侧不完整索引替换为`(followed_id,created_at desc,follower_id desc)`，并新增反向`(follower_id,created_at desc,followed_id desc)`。这让筛选、排序和tie-breaker都由同一个索引提供，不依赖深OFFSET或额外排序。
- 列表响应不再携带`total/page/pageSize`；总关注数仍由页面首次加载的用户profile展示，不在游标切页时重查。前端把首游标空串和后续服务端opaque cursor保存为历史栈，Next只在hasMore且有nextCursor时推进，Previous只弹栈；不会从页码自行合成游标。私有blocked列表是不同API和Finding，保留其现有页码/total合同。
- 真实PG临时影子表分别对followers/following的205条同时间戳数据完整遍历，零重复/遗漏；再为两个方向各实际装载600k关系并ANALYZE。接近第599k位置的生产等价查询分别命中`idx_user_follows_followed_page`与`idx_user_follows_follower_page`，无user_follows Seq Scan，执行0.134/0.125ms。
- 这是前后端协调发布的有意协议收口；旧客户端会收到400，新客户端发给旧后端也不可用，不保留OFFSET兼容分支。generation105要求开发库整库重置；远端generation85仅用session temp表，未迁移或永久写入。回滚代码需同时重置到generation104并回滚前端。

## D-094：已批准历史是治理事实，不是永久公开披露授权

- `change_requests.status='approved'`只说明某次变更通过审核；目标后来可能被封禁、隐藏、删除或改成private/unlisted。公开用户贡献feed因此必须在每次读取时以匿名访客身份重新判断当前目标，而不能用历史revision snapshot或metadata中的旧名称绕过当前治理状态。
- 公开recent activity保守限定为已有followable项目闭集：Mod、整合包、简单项目、服务器、active已审社区内容、ready/partial已审蓝图和active已审公开skin。复用项目follow的动态状态规则，但公开feed对skin进一步要求`visibility='public'`，不能因知道route就枚举unlisted对象；author、内部catalog、用户资料和其他未定义类型默认false。
- 查询由`left join public_routes`改为inner join，当前route缺失即不可见；项目状态CASE在`LIMIT 101`之前过滤，因此100项和`recentActivityTruncated`只针对可公开项。响应名称改从当前目标表读取并以当前route public ID回退，不再读取`content_revisions.snapshot`，从而不会在对象重新命名后继续披露旧敏感标题。
- contributions的日历计数、年份和总贡献数仍是聚合事实，不携带目标名称或链接，因此保持不变；只有recentActivity明细执行当前可见性过滤。完整change request/revision仍保留在数据库供有权治理后台使用，本项不删除审计记录。
- 真实PG夹具覆盖公开Mod/skin、pending隐藏Mod、deleted蓝图、private/unlisted skin、无route请求及unsupported author；初始只返回两项。交换两个Mod审核状态后旧项立即消失、新项以当前名称出现，证明结果由当前状态而非历史批准或提交者所有权决定。
- 无DDL，generation保持105，远端generation85只使用session temp影子表。成功DTO字段形状不变但明细集合有意收紧；回滚无需数据操作，却会恢复旧名称和隐藏目标披露，故不提供历史snapshot兼容字段。

## D-095：关系保存修改稳定审核行，移除是一种状态而不是物理重建

- 项目作者关系以`subject_type/subject_id/creator_id/coalesce(role_id,0)`为稳定业务键，并另有`id/public_id/created_at/approved_by/approved_at`审核身份；团队成员以`team_id/member_creator_id/role_id`复合主键及创建/审核时间承载同一语义。保存完整编辑快照不等于授权删除并重建这些事实。
- 两条同步路径先按稳定键有序`FOR UPDATE`读取全部当前行。请求中已有键只原位更新展示字段、顺序与合法状态转换；新键才INSERT。获得对应敏感权限的调用方遗漏旧键时把它原位改为`revoked`，不清除原批准人和批准时间；重新批准非approved行时才记录新的批准人/时间。
- 未获`project.authorship.manage`或`project.team_relation.manage`的项目编辑对相应creator kind没有移除权。其快照里遗漏的行无论是approved、pending、rejected还是revoked都完全不动，避免后台不可见的待审/驳回事实被普通内容发布吞掉。显式重新提交一个非approved稳定键仍可进入pending，原公开身份与创建时间不变，历史审核日志仍能用该身份关联。
- 团队成员专用管理端点同样把遗漏行转为revoked；权限审计的before/after因此保留旧行并显示状态转换，而不是显示记录消失。成功响应`count`继续表示本次请求的现行成员数，不把保留的历史行计作活跃成员；公开成员查询既有`status='approved'`过滤不变。
- 真实PostgreSQL会话影子测试同时覆盖项目与团队：普通编辑后approved/pending/rejected三态及所有身份时间保持，管理员替换后旧行均以同一身份revoked、新行独立approved；现有团队HTTP审计测试验证after含revoked且强制审计失败会回滚整个差异同步。
- 无DDL，generation保持105，复用既有四态CHECK。远端generation85未迁移，只创建连接级临时表；连接关闭自动清理。回滚代码无需数据转换，但会恢复主键、公开ID与审核时间被重写以及隐藏审核行丢失的风险，故不保留全删全插兼容分支。

## D-096：外部创作者导入是无副作用建议，不是隐式身份或授权创建

- `POST /creator-imports`的业务阶段是分析和预览。读取外部provider后，服务端只返回名称、链接、安全外部头像预览及成员角色建议；它不启动写事务、不镜像OSS、不创建或复用creator，也不产生关系、审核、审计或额度事实。用户取消、关闭或重复预览因此不需要后台清理。
- 预览响应不再携带`creatorId`、`roleId`、`avatarFileId`或`createdMembers`。团队成员是只读建议，必须在creator正式保存后由具备`team.members.manage`的专用成员流程显式确认；头像必须经既有上传流程取得可信站内file ID。保存端额外清除无file ID的外部URL，客户端不能把preview hotlink伪装为持久头像。
- 外部角色只做trim后的完整词匹配，不做包含关系或模糊猜测。owner/project owner/organization owner、developer/dev和maintainer分别建议三个授权内建角色；未知、自定义、组合或超长角色统一建议非授权`contributor`。artist、leader、sponsor及former角色保留展示语义但不授予项目访问。
- 内部角色名称及是否授权不在前端重写常量。Handler一次批量读取`creator_role_definitions(code,name,permission_granting)`并要求所有建议code存在，然后把外部角色、内部角色与授权布尔值一并交给UI显示。generation106将owner/developer/maintainer固定为内建授权白名单，将leader/contributor固定为display-only，并新增maintainer/contributor seed。
- 测试不再把`Supporter -> developer`风险锁作合同。纯函数覆盖明确白名单、展示角色、恶意组合及超长输入；Schema测试锁定五个关键角色的授权位；AST测试禁止预览调用链引入写操作；真实PostgreSQL Handler测试证明Supporter=false、Maintainer=true且creator表保持0；前端测试禁止四个持久字段并验证双语授权标记。
- 这是有意收紧的开发期API迁移：旧客户端若依赖导入时自动产生站内成员ID必须改为预览后显式保存，不提供双协议。角色白名单由generation106引入并保留在当前generation108；开发Schema需重置到当前代次。远端generation85仅用于连接级临时影子验证，未执行DDL或永久写入。代码回滚会恢复孤儿OSS/creator和权限提升风险，故不保留旧路径。

## D-097：创建与导入的“零隐式授权”必须由完整行为矩阵证明

- `submitted_by`只记录谁提交项目，不是editor/developer授权来源。手工创建、供应商最终提交或metadata import job都不得因此新增`user_role_bindings`、`user_permissions`、`project_editor_assignments`、`creator_claims`或`effective_project_access`，也不得推进提交者的`auth_version`或`permission_version`。
- 旧测试只解析四个顶层函数并搜索`user_role_bindings`字符串，helper内写入、其他授权表、版本推进与实际权限判定都可绕过。它被明确改名为局部结构补充，仅禁止创建入口函数直接写角色绑定；完整调用图的安全结论只来自真实数据库行为。
- 行为矩阵覆盖Mod/整合包的manual与Modrinth最终提交，plugin/map/resource pack/shader pack/datapack/addon的manual及其支持的Modrinth或CurseForge最终提交，Minecraft服务器创建，以及Mod/整合包/六类简单项目的metadata import job，共25个独立subtest。每次操作后都读取全部授权来源和两个版本号；实际生成项目还调用真实编辑/管理Handler并要求403。
- 为避免把远端generation85迁移或污染，集成测试使用显式`MCMODS_RUN_DB_INTEGRATION=1`且`MaxConns=1`的会话临时全Schema。夹具从当前Schema语句生成临时表，显式限定自定义函数到实际`pg_temp_N`，在结束时枚举并删除该会话全部临时关系/例程、重置search path；首次在generation106关闭本项，generation107加入统计保留基线后再次验证25分支与隔离，关闭连接后新pool确认public generation仍为85且临时namespace没有关系残留。
- Minecraft服务器创建仍需真实探测；为使权限矩阵确定性执行，`Server`增加内部probe依赖缝，生产未注入时继续调用`serverprobe.Probe`，外部API和运行时默认行为不变。测试注入只返回固定探测结果，不绕过创建Handler、数据库事务或随后管理权限检查。
- TEST-004本身无生产DDL，也不新增授权来源；首次闭环为generation106，当前generation108的同一25分支矩阵再次通过。显式editor assignment、approved creator claim及经验证关系仍由各自专用流程管理；创建/导入不能通过兼容路径复制这些事实。

## D-098：实时创建事实的actor来自每张来源表的当前权威字段

- Mod、整合包、六类简单项目和Minecraft服务器均以`submitted_by`记录提交者，社区内容以`author_id`记录作者。共享触发函数只按这两个当前字段依次解析actor；旧`created_by`不属于这五张来源表，删除其回退，避免一个看似通用但实际失配的字段继续掩盖零写入。
- actor不仅影响INSERT。五个trigger都把对应actor列纳入UPDATE事件，`ON CONFLICT(content_type,object_key)`也更新`user_id`，因此审核过程中若权威actor发生合法变更，事实不会继续归属旧用户。审核状态、当前存在状态、删除时间和事实更新时间仍在同一次upsert中同步。
- 物理DELETE使用OLD行，因此保留原`content_type/object_key/user_id/created_at`并把`current_exists=false`、`deleted_at=now()`；这让历史创建与当前存在可同时表达。社区内容的`status='deleted'`还会软删事实，恢复active时清除`deleted_at`，随后物理删除仍落到同一事实身份。
- 真实PostgreSQL测试不自行仿造触发函数或极简来源表，而是用受环境变量和单连接限制的当前会话临时全Schema。Mod、整合包、plugin、服务器和discussion各自完成actor A插入、actor B+approved更新、删除；discussion另做soft delete/restore。旧实现下四个项目无行、社区仍归A，修复后在generation106和107均精确通过。
- 本项只修复实时创建事实；BUG-006随后以generation107独立关闭人工reconcile精确性，ARCH-005/006也已分别关闭读取错误与成长投影可靠重放。无HTTP/API变化；BUG-005本身无表/列/索引变化，远端generation85不执行迁移。

## D-099：可校准累计必须是“已清理基线 + 当前原始事实”，不是历史最大值

- `user_statistics_daily`是投影而不是独立权威。使用`greatest(existing,excluded)`只能增加，过高漂移永远无法修正；仅在新总数更大时替换`action_counts`还会让总数、分动作JSON与专用view/edit/create/delete列互相矛盾。totals从daily聚合却遗漏action_counts，进一步固化错误。
- 原始活动允许按动作、用户、对象和时间清理，不能把“当前仍存在的raw”误作全部历史。generation107新增`user_statistics_retained_actions`，只保存user、UTC日期、action ID、计数、Markdown增删字节、first/last及最后评论时间，不保存对象ID、route或内容。DELETE语句在同一事务中以OLD transition table一次按用户/日/action聚合并累加该基线；每个删除批次最多执行按分组数量的upsert而非逐行upsert。
- reconcile的权威集合是保留基线与当前raw的`UNION ALL`后按日/action合并。daily每个数值、完整action_counts和时间边界都由该集合直接替换；没有任何来源的旧daily行在同一SQL中删除。totals再从精确daily汇总数值并以`jsonb_each_text`跨日合并action_counts，lastEdit/lastComment同时取raw与保留基线的最大时间，所有冲突列直接使用excluded，包括null与更小值。
- 校准事务先取得稳定typed advisory lock，再`FOR UPDATE`用户行。活动INSERT的user FK key-share和删除触发器基线INSERT因此与校准串行：校准只会看到提交前raw或提交后baseline，不会看到两者皆无；同用户两次校准也串行。修复同时把此前推断为text但传int64的lock参数显式转为bigint，使CLI真实可执行。
- 真实PG用四个活动覆盖edit/view/create-comment/delete，两项在首日被清理、一项整日被清理；随后人为写入99计数、bogus JSON、幽灵日期和未来recent时间。连续两次reconcile均恢复4个总动作、精确四动作JSON、30/8字节、两日、正确first/last/edit/comment，保留表仍只有3行/3事件。
- 这是generation107开发期Schema变更，需要整库重置；远端generation85只运行受保护会话临时Schema，未执行DDL或永久写入。统计HTTP/CLI形状不变。ARCH-005的读取错误和ARCH-006的成长投影重放未由本项冒充关闭，已由后续独立修复关闭。

## D-100：反滥用限制先按当前action匹配，再组合所有生效模式

- `anti_abuse_restrictions`允许同一账号、IP或设备存在多条未解除且时间重叠的限制；因此“模式优先级+starts_at倒序+LIMIT 1”不是有效权限决策。较新的无关action不能决定较旧相关action是否存在，缓存也不能把一个action的单行结果复用于另一个action。
- account profile查询以本次action为第四参数。matching集合包含actions与当前action或`*`相交的限制，以及不依赖action数组的read_only/temporary_ban/permanent_ban；同时保留user、IP、device三种来源OR语义及active时间边界。聚合只返回一个有界结果行，不把任意多限制JSON传入应用。
- hard restriction排除challenge/moderation后按permanent ban、temporary ban、read-only、其他action hard mode的稳定优先级选择，mode和end来自同一排序项；同优先级取最新starts_at。`RetryAfter`使用`RestrictionEnd-input.Now`，不再混用真实墙钟影响受控决策。
- challenge与moderation不是互斥候选。查询分别取所有匹配项的最晚有效期，应用在challenge未通过时加入管理员challenge规则，并无条件保留管理员moderation规则；通过challenge不能消除同时存在的moderation要求。hard restriction仍在评分前立即返回，permanent为Deny，其余为TempBlock。
- account缓存键加入规范action的HMAC scope，comment/message等读取不会互相复用；管理员创建、解除和自动限制仍调用现有`InvalidateAccountState`，它按`anti-abuse:account:{user}:`前缀删除该用户所有IP/device/action变体，无需枚举action。
- 真实PG先写旧comment cooldown再写新message cooldown，旧实现的comment检查得到false；修复后两个action连续缓存读取及真实Evaluate均返回各自action_restricted，upload不继承。再验证较新comment、同action challenge+moderation、较旧全局read_only与较新`*` cooldown，证明顺序、组合与全局优先级。
- 无DDL，generation保持108，Decision及管理员限制API不变。远端generation85只使用受保护会话临时全Schema。ARCH-007中的自动限制写失败、风险日志、指纹/爬虫扫描和队列生命周期不是选择逻辑，已由后续独立修复关闭。

## D-101：Join成功只能在会话事实与Token使用事实同时提交后返回

- 原路径先在pool上自动提交`yggdrasil_join_sessions` upsert，再单独自动提交`yggdrasil_tokens.last_used_at`。第二条语句失败时Handler返回503，但第一条已可被`hasJoined`读取，协议失败与数据成功相矛盾。
- 权限预检用Token所属用户解析现有RBAC；随后的事务不信任预查快照，重新联接enabled account、active user/profile和active/unexpired Token，并对Token行`FOR UPDATE`。selected profile UUID也在获锁后重新比对，撤销/过期不能从预查穿透到写入。
- Join upsert、带active/expiry条件且要求恰好一行的Token时间更新、以及commit都使用同tx。SQL故障是服务不可用503；同serverId已有另一个未过期Token时upsert影响0行，继续是无效Token 403，且整个事务回滚。
- 真实PostgreSQL当场注入`last_used_at`更新trigger异常：Handler返回503后Join行数量为0、Token时间仍null，真实`hasJoined`返回204。移除故障后204与两项事实同时出现；两个并发请求用不同Token争用同serverId，严格只有一个204/一个403，且仅Join行指向的胜者更新时间。
- 无DDL与客户端协议迁移，generation保持108。验证使用单连接会话临时全Schema，两个并发Handler调用经pool排队并依然执行完整生产事务路径；远端public generation85无迁移和永久写入。回滚代码无数据操作，但会恢复503后部分Join可见的错误语义，不保留双提交兼容分支。

## D-102：对外发放的proof挑战必须由一条INSERT形成完整可验证状态

- proof的prompt、answer hash和nonce来自同一个随机Token；其中nonce是验证时重算HMAC的必需输入。旧实现先插入默认空metadata的pending行，再以第二条UPDATE补nonce并忽略错误，因而可在持久状态不可验证时仍返回正常ChallengeInfo。
- 新路径在返回public ID的同一INSERT中写入provider、token hash、answer hash和`jsonb_build_object('nonce',$10::text)`。显式`::text`避免PostgreSQL variadic JSON构造器无法推断参数类型；不再存在可独立失败的metadata补写。
- 首次INSERT失败直接向`createChallenge`返回error，`Evaluate`沿用现有`challenge_issue_failed`人工审核降级；只有全部列持久成功才对外发放ID。ChallengeInfo字段、prompt文案和`id:answer`提交协议不变。
- 真实PG会话临时全Schema安装一个会对任何`UPDATE OF metadata`报错的trigger。旧实现吞掉异常后返回ID，但`metadata->>'nonce'`是NULL；新实现不触发UPDATE，首行nonce与answer hash匹配，随后真实验证一次成功并转为consumed。
- 无DDL，generation保持108，远端public generation85未迁移或永久写入。回滚无数据迁移，但会重新发放不完整挑战，不保留补写兼容分支。ARCH-007的其他反滥用副作用错误可见性已由后续独立修复关闭。

## D-103：四层分类上限必须校验移动后的整棵子树

- 资料根节点的深度定义为0，分类节点只能是1..4。原trigger从`new.parent_id`向上计数，直接第五层INSERT实际上已经因为把根节点计入而被拒绝；原审计关于该INSERT可达的算术不成立。但UPDATE只看新父链、不看被移动节点的既有后代，真实PG可把带两级后代的分类挂到深度2父节点下，提交出深度5，因此Finding仍是可执行BUG而不是NA。
- generation108的`validate_mod_content_section_tree`先锁定`mod_content_versions`权威行；跨version更新按version ID有序取锁。同version的创建、单项审核发布、整页布局和直接SQL因此在检查父链/后代时串行，不会让两个各自基于旧快照合法的移动共同越界。
- trigger对新父链递归计算被移动节点的新深度，同时从`new.id`递归计算当前后代最大高度；两者之和大于4即失败。父链或后代任一行跨mod/version失败，self-parent和把节点挂到自身后代形成的祖先环也失败。parent为空时仍检查整棵后代树，不再提前返回。
- 增强后的即时trigger会看见批量更新的中间态。整页发布因此先给所有后代分配全树唯一的暂存ordinal和`__layout_staging_<id>` system key，再把节点全部展平到root；之后按目标深度父先子后重建。资源同步后先归档移除项，再恢复所有原system key，确保重复key在不同旧父节点下也不会因暂存展平产生伪冲突，事务失败则全部回滚。
- RED在generation107会话临时全Schema中证明带后代UPDATE成功。GREEN在generation108中证明同一UPDATE失败且原parent不变、合法四层成功、直接第五层失败；另一真实PG测试从四层旧树删除三个深后代并重挂保留节点，提交后最大深度2、移除项全部archived、重复system key完整恢复且无暂存值残留。
- 这是开发期Schema generation108，需要从107整库重置，不提供双trigger或旧深度语义。整页布局的既有422详细错误、单项提交/审核DTO和最大深度4均不变；远端public generation85只运行受保护会话临时Schema，未迁移或永久写入。

## D-104：审核队列容量不能通过隐藏截断定义

- 统一审核队列的产品集合是“当前审核者有权处理的全部pending项”，不是按创建时间最早的2000项。固定`LIMIT 2000`在搜索、筛选、total、facets和分页前执行，会让第2001项以后永久不可达，并使前2000项不匹配时伪装成空队列。
- 13类来源仍以一个规范UNION表达，但在PostgreSQL内分为三个materialized集合：`queue`是全部候选，`visible`应用global/project权限与项目审核者禁止自审，`filtered`应用category/operation/projectType和关键词。当前页、精确filtered total以及基于未筛选visible全集的三类facet由同一SQL statement/snapshot生成。
- 页排序由不唯一的`created_at`收紧为`created_at,id,source`；保留现有offset API以免要求前端双协议，但不再把offset夹到已截断长度或人为最大页，非负int64都可表达。每页仍硬限1..100，进程内只持有当前页JSON与三个低基数facet，队列全量由PostgreSQL的CTE/work_mem/临时文件管理。
- 搜索沿用旧合同中的项目名、标题、用户名和project ID的大小写无关字面子串，不让`%`/`_`意外变成LIKE通配符；query按100个Unicode rune截断，避免旧byte slice切断UTF-8。facet继续是当前权限可见全集而非随当前筛选消失，前端筛选器语义不变。
- generation108真实PG全Schema构造2005个mod revisions和pending change requests。offset2000精确返回#2001..#2005且total/三facet均2005；只搜索`revision #2005`返回1。全局审核者得到global scope与正确URL，另一用户的项目审核者可见project scope，提交者本人即使有project.review仍为0。
- 无DDL与客户端迁移，generation保持108。BUG-021关闭不可达性，TEST-009关闭结果语义测试缺口；PERF-012后续已用独立100k/1M执行计划、临时文件与延迟证据关闭，未用2005项功能矩阵冒充规模证明。

## D-105：项目审核的读取权限必须与处理权限共享目标和提交者边界

- 原处理端已经接受精确`project.review.<projectID>`并禁止项目审核者处理自己的提交；Mod历史与比较却只接受项目编辑或站点级`project.review`，导致审核者能在队列看到合法目标却无法读取修订内容。Modpack、simple project和changelog历史也必须服从同一边界，不能各自复制略有差异的布尔门禁。
- `projectPendingReviewVisibility`把读取决策显式分成`includeAll/includeScoped/reviewerID`。项目编辑以及`admin.*`、`content.review`、站点级`project.review`保留全部修订可见；精确项目审核仅在解析后的目标ID完全相等时成立，权限模板、deny、空ID与跨项目规则不生效。
- approved修订继续公开于可访问项目历史。精确项目审核对每条非approved修订还要求`submitted_by>0`且不等于当前审核者；SQL在数据库行级过滤历史，比较端对before和after分别执行相同判断，不能靠其中一端approved而带出另一端待审内容。隐藏的内部提交者ID不进入JSON。
- Modpack与simple project内部可用editor读取加载目标，但在返回前重新执行对象当前审核态、提交者和精确项目权限检查；pending changelog用其目标项目public ID授权，并在加载入口同样禁止精确审核者自审。Mod资料的手工修订采用项目权限，跨项目无泄漏；不属于该项目处理边界的导入资源待审历史仍只向全局审核者或编辑开放。
- generation108会话临时全Schema行为测试覆盖Mod历史及before/after比较、Modpack、plugin与pending changelog：目标项目审核者可见他人pending，自审只能看到approved且比较403，跨项目只能看到approved或在pending对象入口404，站点级审核者保持完整访问。临时Schema隔离测试再次确认public generation85不变且临时关系零残留。
- 无DDL、路由或DTO迁移，generation保持108。回滚不需数据处理，却会恢复“队列可处理但详情不可读”的盲审和各历史入口权限漂移，因此不保留旧读取门禁；BUG-023的数据库读取错误语义与PERF-013历史分页仍按独立Finding开放。

## D-106：审核成功响应必须在提交前证明可构造

- 原流程先提交审核事务，再从pool回读修订并忽略错误。真实PG使用合法JSONB数组作为异常历史snapshot时，拒绝路径能完成审核，但`scanModRevision`无法把数组解码为`createModRequest`；旧Handler仍返回200，外层ID/status来自已扫描列而snapshot是零值结构。原审计称“空对象”不完全精确，但已提交状态与畸形成功DTO的核心分裂真实可执行。
- 不采用“提交后回读失败再返回500+标识”，因为客户端仍面对失败响应但业务已成功、重试又冲突。`reviewModRevision`在同一tx内完成状态、review event、不可变audit、完成通知和可选OSS任务后，立即以同一事务快照执行规范`modRevisionSelect`及完整JSON解码；任一步失败都在commit前返回并由既有defer rollback全部事实。
- 只有响应对象已完整构造后才commit。commit失败仍返回现有500且不发送200；commit成功后不再访问数据库，直接序列化已构造对象。因此200同时证明审核事实已提交和其对应DTO可由同一快照读取，不存在第二次自动提交或补偿窗口。
- generation108临时全Schema双夹具以精确项目审核者执行拒绝：数组snapshot在旧实现下得到200且request已rejected，新实现得到500、request仍pending、rejected event为0；正常对象snapshot得到200，返回ID/status精确且request/event同时提交。该故障还间接覆盖audit/通知均位于回滚边界内。
- 无DDL、路由或成功DTO迁移，generation保持108。失败语义有意从畸形200收紧为未提交500，调用方可安全重试；回滚代码无需数据转换但会恢复不可恢复的提交后响应分裂，不保留旧post-commit查询兼容分支。

## D-107：Mod历史页由唯一修订号推进，不由全集响应或DOM定义

- 同一`(aggregate_type,aggregate_key)`内`revision_no`由数据库唯一约束且单调递增，历史固定按其降序展示，因此游标只需表达“下一页小于最后revision_no”。游标JSON使用base64url并包含版本、项目+limit作用域摘要和revision号；严格拒绝未知字段、额外JSON、超长值、非正revision、跨项目与跨页大小复用。
- 首次页默认50、最大100；查询只取`limit+1`，多出一行只判定`hasMore`并不序列化。响应保留`items`并增加`limit/hasMore/nextCursor`，不计算随历史增长的COUNT。页间新增更高修订不会使旧游标重复已看行，权限过滤仍在limit之前逐条应用，BUG-022的精确项目审核/自审边界不因分页改变。
- `modRevisionSelect`是历史、单条读取、比较和审核提交前响应的共同投影。提交者和base revision改为普通LEFT JOIN；最新approved/rejected/conflicted event只执行一个按`created_at,id`降序的LATERAL lookup，随后JOIN reviewer；note、reviewedAt和reviewer来自同一事件，不再为同一行执行三个独立event子查询。
- generation108真实临时全Schema为同一Mod写入100,000 revisions、requests和approval events。生产Handler返回v100000..99951，再由服务器cursor返回v99950..99901且ID零重复；project/limit伪用cursor得400；深cursor返回v50..v1并终止。`EXPLAIN ANALYZE`命中`idx_content_revisions_aggregate`和`idx_review_events_request_created`，无两表Seq Scan或SubPlan，执行0.377ms。
- Next16客户端仍是需要state/effect/事件的窄Client Component。它只持有当前50项、opaque cursor历史和最多两条完整选择；Previous/Next由事件清页并推进/回退，AbortController取消旧请求。选择对象而非当前页ID，故可跨页比较；siteId通过key remount清除旧项目状态，符合本仓React effect lint边界。
- 这是前后端协调的有意API收口：旧客户端若依赖一次得到全集必须迁移，不提供无界`all=1`、offset或旧响应双路径。无DDL，generation保持108；回滚无需数据转换但会恢复数据库响应体与浏览器DOM线性增长。PERF-012队列物化成本及其他历史端点由各自Finding验收。

## D-108：站内OSS图片的内容安全不能替代对象级复用授权

- `active/clean`、图片MIME、尺寸和内容哈希只能证明对象当前可处理，不能证明发起项目导入的用户有权把它公开到另一个业务位置。公共端点下的已知object key仍必须先通过调用者和用途授权；桶ACL或URL可访问性不能成为应用层授权来源。
- 项目图标与创作者头像调用点已经分别构造唯一目标`category`和受控import `source`，并携带认证actor。复用查询现在要求同一`object_key/category/source/uploader_id`行；actor非正、category/source空值、其他用户、其他项目/创作者类别或不同来源全部在读取OSS内容前失败关闭。
- 对可信外部Host的新下载仍由内容摘要生成目标路径；同一actor对同一目标和来源的重试可返回原file身份。查询命中后继续读取真实OSS对象，并校验数据库size、SHA-256及可解码栅格内容，因此新增授权谓词没有替代既有完整性检查。
- 不引入“所有clean对象都公开”或管理员隐式跨owner例外，也不新增公开媒体状态。若未来需要跨所有者共享，必须由独立显式公开媒体协议表达，而不能重新从公共URL或object key推导授权。
- generation108临时全Schema真实查询证明exact tuple返回原file ID，替换owner、category、source或使用actor 0均不返回；源码门禁锁定三个授权谓词及全部生产调用参数。隔离复验确认连接清理后public generation仍为85。
- 无DDL、路由、请求或成功DTO变化，可随后端独立部署。恶意/不属于目标用途的站内URL按既有导入失败语义处理；回滚无需数据转换但会恢复跨类别/所有者IDOR，不保留仅内容安全的兼容分支。

## D-109：固定系统分类的创建、排序和文案必须由同一个事务函数定义

- `blocks/items`是item/block资料页的真实子分类，不是两个入口各自拥有的展示细节。手工布局发布与导入投影都需要相同的缺项补建、追加ordinal、默认locale/display mode、actor和六条本地化；复制SQL会让任一语言或冲突规则修改只能靠人工同步。
- 既有`ensureItemBlockSystemCategoriesTx`已接收root、version、mod、template、actor和展示事实，正好是领域权威。手工路径保持直接调用；导入路径在root section创建后按version与builtin `item_block`解析第一条active root，再把同行事实传入该helper。不存在root是正常的“本批无item/block资源”，其他查询错误显式返回。
- 删除导入器的两条重复SQL和两份文案literal。旧本地化SQL只按version与system key筛选，会把其他模板下恰名为`blocks/items`的普通子分类也写入系统文案；共享helper同时绑定`parent_id=rootID`，因此收口重复权威也修复了作用域漂移。
- generation108真实临时全Schema导入一个block资源，结果root下只产生ordinal 0的blocks、ordinal 1的items和精确六条en-US/zh-CN/zh-TW名称；另一个模板下预置的`blocks`子项保持零本地化。既有来源差异reconcile测试继续通过，证明完整sync调用链消费共享helper。
- 源码门禁要求导入器存在helper调用且不存在旧创建/本地化错误文本或两组locale literal；生产搜索显示文案只剩`mod_content_system_categories.go`一份，helper只有手工与导入两个生产调用。
- 无DDL、API或客户端迁移，generation保持108。可随后端独立部署；回滚无需数据处理但会恢复双权威和version-wide误本地化，故不保留重复SQL兼容分支。

## D-110：MODID确认是绑定一次分析尝试的安全协议，不是一个布尔按钮

- 确认身份由六类事实共同定义：job ID、当前run token、job所属Mod、创建用户、源内容hash和规范化分析结果hash。worker初次检查必须用job/run token/Mod同行匹配；HTTP确认再用路由Mod、created_by、analysis hash、confirmation_required状态和24小时窗口收紧。任一事实变化都不能消费旧确认。
- `pauseCatalogImportForMODIDConfirmation`原先读取/更新只绑定job+run token，传入错误Mod时会用另一项目的identifiers改写任务分析。三个job读/转换语句现在都增加`mod_id`谓词；错误Mod与错误token返回no-row且任务保持原运行事实。MCMods Export ZIP和嵌入图标目录两个生产入口各恰调用一次同一gate。
- 首次分析将`sourceHash + modIDAnalysis`规范JSON做SHA-256。确认成功后再次以同源同分析运行时识别已确认hash，清除required并继续，不重新暂停；源hash或候选/配置变化产生新hash，清空confirmed actor/time并再次进入confirmation_required。
- HTTP确认把queued状态、确认actor/time、`mod.catalog_import.confirmed` Outbox和活动事实放在同一事务。约束故障拒绝Outbox时返回500，job仍confirmation_required且无事件；成功只产生一个pending事件，状态转换令任何重放/并发重复确认得到409且不能产生第二事件。
- 真实generation108暴露了旧测试夹具掩盖的类型错误：Schema是`configured_modids text[]`，`modExportJobByID`却扫描为`[]byte`再按JSON解码。确认事务已提交后回读因此稳定500。生产现在直接扫描`[]string`，测试影子Schema同步改为text[]并保留NOT NULL/JSON对象形状检查；合法确认恢复202完整job DTO。
- 无DDL或请求/成功DTO迁移，generation保持108。错误actor、路由Mod、hash、25小时过期及重放继续统一409；合法路径从真实Schema上的错误500收紧为202。回滚无需数据转换但会恢复跨Mod分析写入和提交后读回失败，不保留旧扫描或弱绑定兼容分支。

## D-111：Mod关系组以一次有序联接完成详情装配

- 审计定位的成本来自`loadModAssociations`先读关系组、再为每组发一条关系查询；在接口允许的50组边界上会产生额外50次数据库往返。关系和组都已有稳定`display_order,id`，无需增加缓存或复制投影。
- 新查询从`mod_relationship_groups`出发，以单个`LEFT JOIN LATERAL`读取当前组内关系，并在子查询内过滤指向非approved Mod的站内目标。外层按组序、关系序排序，Go只用`groupID -> response index`映射折叠结果，不再发逐组SQL。
- 使用LEFT联接而不是普通JOIN，确保无关系的组、以及关系全部因目标待审而隐藏的组仍返回空`relationships`；未收录的文本目标继续保留。公开DTO、方向值和既有可见性语义不变。
- generation108会话临时全Schema夹具建立50组和200条关系；第一组的4条全部指向pending目标，其余196条指向approved目标。生产loader只触发1次关系组查询，返回50个稳定有序组、首组空关系与196条可见关系。
- 无DDL、API或前端迁移，generation保持108，远端public generation85未迁移。回滚无需数据处理，但会恢复组数线性数据库往返，不保留双读取路径。

## D-112：关系所有权测试必须显式绑定两个互斥的项目编辑者

- TEST-011不是新的生产缺陷，而是SEC-013原测试把`incoming`当可写输入、没有证明权限与所有权边界的测试缺口。SEC-013修复已删除跨来源写路径；本项把该安全不变量从实现证据提升为持续回归合同。
- 纯函数测试不再接受“incoming进入权威snapshot”，而是断言规范化后只剩outgoing组。新增真实PostgreSQL用例建立source/target两个approved Mod和一条source-owned依赖，同时建立subject不同、各自只含精确`project.edit.<projectCode>`的两个claims；四个正反授权断言证明两位编辑者权限互斥。
- 目标编辑者提交带伪造incoming conflict的生产payload后，规范化必须删除该组；随后使用生产持久化函数保存目标关系。事务内复核source-owned原关系ID仍存在、伪造conflict为0，证明目标编辑者无法借响应方向改写来源项目。
- 与既有三项真实PG矩阵一起复跑，继续覆盖直接传入incoming snapshot、对称关系不得删除对方声明及待审出向目标拒绝。所有夹具在独立事务回滚，不产生持久测试数据。
- 仅新增测试和台账证据；无生产代码、Schema、API或前端变化，generation保持108。测试回滚会重新留下跨项目所有权回归无人报警，因此不保留旧的“incoming有效”断言。

## D-113：已部署条目类型只能停用，不能丢失历史解释Schema

- `entry_type_code`不是临时UI选择，而是`mod_resource_version_details`持久化的解释身份。保留类型的kind集合、字段代码和存储签名继续由纯函数稳定检查；被删除的类型代码单独规范化收集，并交给事务引用守卫决定是否允许物理移除。
- 内建后台更新已持有template行锁，再以`SHARE ROW EXCLUSIVE`锁住section、placement和detail三张引用表，递归查询该root模板下活动/待审/归档详情的`entry_type_code`。自定义模板在最终发布事务重新读取并锁定当前definition，执行相同稳定签名和删除守卫；删除模板也在同一事务拒绝任何当前或历史section引用。
- 守卫采用保守的生命周期边界：模板一旦成为任一root资料页，即使页面暂时为空，也不能物理移除条目类型。这样覆盖“待审detail已保留但placement尚未发布”的窗口，也阻止并发section/detail写入在检查后恢复旧类型。真正从未部署、无section也无detail引用的模板仍可删除类型或归档模板。
- `enabled=false`是唯一停用协议。新建详情和导入类型匹配继续以`allowDisabled=false`拒绝该类型；既有详情编辑、审核发布与未解析引用同步以明确的existing-detail路径读取保留Schema，因此停用不会让历史definition失去解释能力。
- generation108会话临时全Schema矩阵覆盖活动引用、归档历史引用、保留类型停用、字段存储改型、无引用类型删除、自定义真实publish及模板删除。旧纯函数测试不再断言整类清空合法，而只验证被删代码进入事务守卫。
- 无DDL、API或前端迁移，generation保持108，远端public generation85未迁移。回滚无需数据转换但会重新允许内建/自定义Schema破坏，并让disabled历史详情不可编辑或同步，故不保留旧行为开关。

## D-114：权限管理读取必须先完整收集，再批量装饰并一次成败

- 管理端角色/用户响应会直接成为可保存的权限草稿，因此“权限查询失败”不能表示为角色权限空数组或截断列表。`rolePermissionEntries`改为返回`(entries,error)`，query、每行Scan和`rows.Err`逐层传播到roles、roleByCode与permissionCatalog；adminUsers也在任何权限解析前检查并关闭用户rows。
- roles先完整扫描并关闭role游标，再把全部code交给`rolePermissionEntriesForRoles`的一条`code=any($1)`查询。每个角色预置非nil空数组，只有真正完整且无权限时才返回空；不会在持有外层cursor时按角色再占连接，也不会用跳过坏行形成部分事实。
- adminUsers先读取最多100个用户并收集ID，再调用既有`resolveUsersRootPermissions`。该批量权威一次加载role图/权限、全部用户绑定、项目访问、作者claim与直接权限，随后按ID回填RoleCodes；删除每用户一遍完整权限解析及冷缓存放大。
- createRole只把SQLSTATE唯一冲突映射409；触发器、连接和其他数据库故障返回500，不再伪装成“代码已存在”。update/delete既有错误语义不变。
- generation108会话临时全Schema插入100个用户和100个额外角色，query tracer证明用户页恒7条、角色+权限目录恒3条。把`role_permissions.expires_at`暂改为含非法时间的JSONB后，列表与单项读取均失败且permissionCatalog返回500；非唯一INSERT trigger也稳定500。
- 无DDL、DTO或前端迁移，generation保持108，远端public generation85未迁移。回滚无需数据处理但会恢复空权限草稿、N+1和错误误分类，故不保留逐项读取分支。

## D-115：权限管理测试以真实写事务和当前 Session 可见性为完成边界

- TEST-049不以纯函数或直接调用SQL helper代替后台行为。新增测试在generation108会话临时全Schema上注册生产HTTP路径，实际请求用户权限PUT及角色POST/PUT/DELETE，并以真实`public_routes`解析、事务触发器、审计表、运行版本和权限解析器复核结果。
- 用户矩阵同时保留`level_track`、`account_status`和`user_preference`系统来源，只替换`manual`角色/直接权限；角色和权限到期时间精确保留，`group.* allow=false`稳定400。写入只推进`permission_version`而不改变`auth_version`，写前已缓存的同一Session在响应后立即获得角色继承和直接权限。
- 角色矩阵要求缺失父级和间接环返回400，轨道依赖删除返回409；既有默认角色、用户绑定和内建依赖helper测试继续作为同一行为网的一部分。这样既验证DAG算法，也验证HTTP事务不会把失败草稿或审计记录部分提交。
- 测试以`permission_audit_logs`的临时NOT VALID约束分别拒绝用户和角色审计INSERT。PostgreSQL把事务置为aborted，生产Commit路径返回500；复核证明旧手工授权完整保留、候选权限/角色均为零。两条并发PUT最终只能完整采用A或B，不能合并成一个从未提交的权限集合。
- query tracer将“一角色+一直连权限”的完整用户写预算限定在18条SQL以内；ARCH-033/PERF-062的同一权限矩阵继续要求100用户读7条、100角色目录3条。关闭miniredis后写仍提交，随后同一Session通过PostgreSQL权威版本立即读取新权限，证明Redis发布故障不会把Session变无效或把旧权限当成功响应。
- 这是一项测试补强：无生产代码、DDL、API或前端变化，generation保持108，远端public generation85未迁移。删除该测试不会改变运行时，但会重新让来源丢失、角色图破坏、审计非原子、版本缓存陈旧和并发合并回归在发布门中无行为级报警。

## D-116：显式负载观察产物必须全链路成功才可退出零

- `cmd/load-observer`从不可测试的紧凑main拆成`runLoadObserver`与可注入runtime；main只负责把返回错误交给`log.Fatal`，因此数据库pool创建/Ping、采样、等待、JSON编码、stdout和文件写入任一失败都会成为进程非零退出，而不是panic或静默成功。
- 命令在开始采样前显式Ping数据库。单次采样或context等待失败会计入报告`errors`，仍尽力编码并写出诊断报告，随后返回包含成功/失败样本数的错误；自动化不能把含缺口的采样当成通过。完全无法连接则不伪造零样本报告。
- `MCMODS_OBSERVER_OUTPUT`非空表示调用方明确要求文件产物。命令先以0600写文件，再打印stdout；父目录不存在、权限不足或其他写入失败立即返回错误，不再用stdout副本掩盖缺失文件。JSON编码和stdout写错误也逐层传播。
- `.env.example`新增无凭据`DATABASE_URL`、observer duration/output。专用`docs/load-observer.md`说明非空DATABASE_URL完整覆盖分散DB字段，其URL路径同时决定开发重置的effective name，故`DB_RESET_CONFIRM`必须匹配URL数据库；observer自身只读`pg_stat_activity`且不触发reset/migrate/seed。
- 真实CLI用可连接数据库+不存在父目录验证显式输出退出1，并用127.0.0.1:1验证数据库不可达退出1；单元runtime覆盖采样超时、编码错误、写失败、成功4样本和pool关闭。无DDL/API或前端变化，generation保持108。

## D-117：负载门禁以请求合同判成败，历史样本不追认自动化效力

- HTTP状态本身不足以判定风控场景：普通请求只接受2xx/3xx；反滥用和爬虫请求可以明确列出403/412/429，但4xx还必须携带该请求闭集内的结构化风控code。这样正常拒绝与服务器/协议异常既不会互相污染，也不能靠任意4xx伪装为门禁成功。
- `errorRate`只计算意外HTTP响应和网络失败，并以六位小数保存；原始client/server/status/code计数继续保留。最低成功响应、最低预期拒绝和必见code是独立断言，任何一项缺失都写入`contract.failureReasons`并令进程非零。23/14613和29/15245分别稳定表示为0.001574和0.001902，不再因两位小数归零。
- 请求池及mixed权重只在启动时构造一次，worker按序选取既有对象；不再让每次请求重建全部数组的客户端分配污染高并发观察。真实子进程测试用本地HTTP分别证明普通200成功、普通429失败、反滥用结构化429在满足成功量/拒绝量/code时成功。
- 两份psql计划脚本不再选择第一名用户或固定OFFSET 20，必须由调用方提供代表高基数user、deep offset和action；全部语句使用`EXPLAIN (ANALYZE, BUFFERS)`，DELETE只在显式事务中执行并ROLLBACK。反滥用自动计划测试在generation108会话临时全Schema中各装载100k行，要求三个生产查询命中指定索引、无目标表顺扫且包含真实执行/缓冲证据。
- 既有34份JSON由旧合同产生，保持字节不变并明确标为immutable historical samples：可以人工解释，不能升级成自动通过证据，也不能改写成一次未发生的新运行。无生产DDL/API变化；回滚无需数据处理，但会重新允许小比例失败归零和正常风控被误报，不保留旧统计模式。

## D-118：CSP来源是按能力拆分的精确部署注册表

- nonce与`strict-dynamic`只约束脚本信任链，不能替代外连和被动资源边界。生产`connect-src/img-src/media-src/font-src`全部删除裸`https:`；`'self'`、data/blob只在确有使用的directive保留，媒体和字体不再获得任意外部默认来源。
- `buildContentSecurityPolicy`成为唯一组装器：API URL提取精确origin；connect内建Turnstile；图片内建当前实际支持的OSS、GitHub、Modrinth、Forge CDN、Minecraft纹理和PlantUML；frame只保留Turnstile、Draw.io、GeoGebra、Bilibili与YouTube无Cookie域。数组去重后形成一条请求/响应共用策略。
- OSS签名上传origin是后台运行配置，无法在浏览器收到ticket后再放宽已生效CSP。因此以`NEXT_PUBLIC_CSP_CONNECT_ORIGINS`显式声明单段及分片可能使用的精确origin；图片、媒体、字体分别有独立变量，避免一个CDN同时获得不需要的外连能力。变量只接受逗号分隔origin，不接受路径、凭据、query、fragment、通配host或scheme source；生产只接受HTTPS。
- 开发环境只额外允许localhost/127.0.0.1的HTTP、WebSocket和图片，外部HTTP同样失败关闭。无效生产配置在策略构造/构建时抛错，不用静默删除造成隐蔽功能故障，也不回退到`https:`。
- 任意用户外链图片/媒体不再天然可加载；这是泄露与跟踪边界的预期收紧。运营只可把经过审核的实际资源origin加入对应列表，不能加入`https:`或通配。无后端Schema/API变化；回滚无需数据处理但会恢复任意HTTPS外传能力，故不保留旧模式开关。

## D-119：可恢复上传同时绑定公开会话主体与服务端内部owner

- Cookie Session令前端拿到的token只是固定占位字符串，不能从中解析账号。上传恢复key改用`useAuthSnapshot().user.id`的9位public ID；task本身也保存subjectId，IndexedDB每次read/update/delete同时校验subject、site、version和完整key，旧的无subject记录失败关闭。
- 账号或目标变化先在layout cleanup中使旧subject失效，再由恢复effect同时abort上传/轮询、清空显示状态并只读取新subject key。上传进度、分片完成、持久队列、job轮询和各操作catch/finally在异步边界后复核当前subject；React Strict Mode effect重放会重新启动恢复，不以一次性ref跳过。
- 浏览器subject隔离不能代替服务器授权。新mcmods_exporter对象仍保存原业务category，但实际object key追加`owners/{internal user id}`；resume只签发当前claims内部owner前缀。单/分片完成继续复用现有uploader/session owner校验，job创建继续要求`oss_files.uploader_id=claims.Subject`。
- 自动active查询增加`job.created_by`，指定job读取也先按creator过滤再做stalled状态转换，避免同项目编辑者用已知ID观察或改变另一账号的恢复状态。后台worker、创建响应和其他内部调用继续使用不带creator的内部读取，不把请求身份扩散进任务执行器。
- generation108临时全Schema建立同一Mod的A/B账号和任务：A只能恢复A，B在A任务上得到no-row且active为空；创建B任务后B只恢复自身。无DDL、DTO或前端业务迁移；旧本地ticket/object path不提供兼容恢复，重新选文件比接受未绑定owner的历史凭据更安全。

## D-120：编辑员申请的目标名称由列表快照一次装配

- 申请列表原查询返回route内部ID后，在未关闭rows时为每项再次向pool查询目标表。除N次网络往返外，MaxConns=1时首个名称查询无法取得连接，列表会等待到context deadline；名称查询错误还被`_`丢弃并伪装为空字符串。
- 主查询按`route.entity_type`对mods、modpacks、simple_projects、minecraft_servers、blueprints、skin_assets和community_posts做主键LEFT JOIN，`coalesce`得到名称。每类JOIN保留原helper完全相同的approved、ready、not_required或active条件，simple project额外匹配`project_type=entity_type`，避免跨类型同内部ID错配。
- application rows完整读取并检查rows.Err后，附件仍以全部application ID的一条`ANY`查询装配。无论列表是1项还是100项，生产reader固定两条SQL；没有名称时仍返回空字符串，公开DTO、排序和过滤不变。
- generation108单连接临时全Schema写入100个不同申请者对同一approved Mod的申请。旧实现在2秒查询deadline稳定失败；新实现返回100项同一精确名称且query tracer为2。无DDL或客户端迁移；列表总量/分页另有独立Finding，不以本项结果宣称无界响应已解决。

## D-121：破坏性活动清理先以有界候选证明可预览

- 旧预览依次对`user_activity_events`执行总数、action、object type、user和样本五次基础扫描，且直到所有扫描完成才计算`dangerous`。这既放大同一请求成本，也让仅action约束的全历史请求在获得任何保护判断前遍历大表。
- 新查询只从活动表建立一次`MATERIALIZED candidates`，列集合仅含六个后续字段并硬限100001。count、三类聚合与十条稳定样本都复用该候选；未超过100000时候选就是完整匹配集，因此计数和样本语义精确。超过上限的部分结果不进入响应或持久预览，所以无需为其做全表排序。
- 只含action和隐式当前snapshot的全历史请求是语法可判定的无界形态，在执行活动查询前返回422 `ACTIVITY_CLEANUP_FILTER_TOO_BROAD`。含user、object、object type、from或to的请求允许进入有界查询；第100001条证明范围过宽后返回422 `ACTIVITY_CLEANUP_PREVIEW_LIMIT`，报告maximum=100000/minimum=100001，且不生成确认token或`activity_cleanup_runs`行。
- 合法成功响应继续保留matchedCount、三类聚合、10样本、filters、snapshot和确认字段。样本JSON先解码为带`time.Time`的类型再交给HTTP编码器，避免把PostgreSQL的timestamp字符串格式泄漏成API变化；user聚合仍只保留前100组。
- MaxConns=1真实PostgreSQL TEMP影子表依次增长到100k、1M和10M。生产summary helper每次恰执行一条SQL；100k得到精确全集，1M/10M的基础Seq Scan都在100001条实际候选停止，解释执行343.103/344.273/347.605ms。无生产DDL、路由或成功DTO迁移，generation保持108；回滚会恢复五次扫描和无界预览，不保留旧helper分支。

## D-122：合成binding与候选项属于同一个有序详情结果集

- `catalogRecipeBindingRows`旧实现先保持binding rows游标，再在循环内为每个binding调用候选查询。除了1+N往返，MaxConns=1时首个候选读取没有可取得的连接并等待到context deadline；因此这不是只有“大模板较慢”，也是合法小连接池配置下的可用性缺陷。
- binding、template slot、candidate、resource、catalog identity、active icon和最新import icon现在由一条LEFT JOIN查询读取。按`binding.ordinal,binding.id,candidate.candidate_index`排序后，Go只在slot key首次出现时建立binding，并按行追加候选；数据库往返不再随槽位增长。
- 必须从binding侧LEFT JOIN：没有候选的合法槽位仍返回非nil空`candidates`，不能被普通JOIN消失。candidate存在时继续构造完全相同的resourcePublicId/kindCode/canonicalId/unresolved/rawResourceId/amount/probability/byproduct/definition；只有resolved资源获得既有icon URL。
- 真实PostgreSQL用MaxConns=1和TEMP影子表验证零候选binding及1、64、512槽位；512来自当前`validateCatalogRecipeTemplateEdit`硬上限。每个槽位至少一候选、末槽再加第二个resolved/unresolved混合候选，四次读取都只有一条SQL，顺序、nullable probability、raw identity和icon行为完整。
- 无DDL、公开API、成功DTO或前端迁移，generation保持108。删除只服务旧循环的`catalogRecipeCandidateRows`，不保留双读取路径；PERF-009的导入模板推广N+1是不同事务路径，继续独立验收。

## D-123：导入模板推广在写入前一次取得完整模板/槽位快照

- 活动revision切换持有事务锁时，旧实现先读全部template snapshot并关闭rows，然后在promotion循环中按计算出的template ID逐次查询slots。读往返为1+模板数，模板越多，活动revision锁和后续canonical写入越晚开始。
- `loadImportedRecipeTemplatePromotions`现在由snapshot关联active ready/partial revision和recipe type，再LEFT JOIN import slots。一个按recipe type canonical ID、source template、snapshot ID、slot ordinal/id排序的结果集包含完整推广输入；reader在开始任何write batch前检查Scan/rows.Err并关闭。
- LEFT JOIN确保slot为零的snapshot仍形成一个promotion；slot存在时恢复role/JEI role、nullable output index、coordinates和两份rect JSON。模板字段只在snapshot ID改变时初始化，后续行只追加slot，避免把同一模板拆成多个文档。
- 真实PostgreSQL TEMP影子表验证零slot以及1、64、512模板；每模板一条input slot、最后模板再加一条带`output_index=0`的output slot。每个revision只有一条同时包含snapshot和slot的SELECT，数量和顺序完整。
- 原`queueExportJEITemplateCollection`验证、canonical identity、写batch阈值/flush及manual publication保护均未改变；写入数量仍随真实模板/slot数增长。无DDL/API/客户端迁移，generation保持108；回滚会恢复激活事务读N+1，不保留逐模板读取兼容分支。

## D-124：统一审核队列以大规模完整语义和基础事实单遍读取验收

- PERF-012的根因代码已在BUG-021中修复：过滤、权限、当前页、total和facet都下推同一SQL，隐藏2000截断与Go全量集合已删除。本项不再创建第二审核投影权威，而是补齐当时明确保留的100k/1M计划证据；若计划显示13个UNION分支重复扫描基础事实，则不能以功能正确关闭。
- generation108会话临时全Schema装载100,000个真实`content_revisions/change_requests`约束行；limit100、offset99900返回精确末页，total和category facet均100000。`EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON)`执行541.056ms，两个基础表各examined 100000，证明空来源分支没有重扫整个pending集合。
- 1,000,000规模使用会话TEMP影子表，列和content aggregate/pending proposed-revision索引形状与生产reader一致，避免把完整Schema触发器的百万行生成成本伪装成查询耗时。最深offset999900仍返回精确100项，执行5419.549ms，两个基础表各examined 1,000,000；单次API仍只有一个SQL statement。
- 精确可见全集facet和深offset在1M会物化并使用临时块（read159057/write123114）；这是真实记录的线性成本，不宣称O(1)。测试以基表最多1.1倍访问和10秒执行上限阻止回归；普通页面仍受limit100硬界，PostgreSQL临时文件而非Go堆承担全局聚合。
- 无生产代码、DDL、API或客户端新增变化，generation保持108；本项是已实现修复的独立规模验收。若未来产品需要百万常态积压或低延迟facet，应以事务维护的单一投影/计数替代当前CTE，而不能恢复截断、放弃精确total/facet或把全量搬回Go。

## D-125：公开资料卡片、进度图和编辑布局使用三个有界读取协议

- 旧板块资源接口同时承担卡片浏览、搜索、进度全图和编辑器整版装载，允许任意OFFSET及`all=1`最多20,000个宽对象。每次COUNT和数据读取都重复递归子树、JSONB前置通配及相关本地化/导入查询；匿名调用既没有响应字节预算，也没有该昂贵读取的独立速率预算。
- 卡片读取改为scope绑定的不透明游标。scope包含Mod、section、version、revision、query、limit和模式；排序键为子树路径、placement ordinal、resource ID，limit+1决定`hasMore/nextCursor`。每页最多200，首屏计算精确total/categories并把total签入游标，后续页不再重复COUNT；offset、all、跨scope/畸形cursor和1至2字符搜索明确400。
- generation109在placement上增加事务维护的`search_document tsvector`和GIN索引。投影包含canonical ID、活动详情本地化名及活动ready/partial导入快照名；资源、本地化、快照、修订与canonical ID变化由触发器同步刷新。查询统一用`websearch_to_tsquery('simple',q)`，不再运行JSONB `ILIKE '%q%'`或逐行相关EXISTS。
- 进度树使用公开可选鉴权的`resource-graph`精简流，只有advancement根可用；编辑器使用鉴权`layout`精简快照。两者最多每块1,000项并使用稳定cursor，分别受1 MiB和4 MiB实际JSON envelope预算；超预算时缩短items并返回可继续的cursor。匿名图可缓存60秒并stale-while-revalidate 120秒，鉴权响应private/no-store；卡片/图/布局另受IP与用户scope的显式分钟读预算。
- 前端普通板块只取首屏卡片并按nextCursor追加；进度页消费完整精简图，在本地搜索时保留命中节点祖先；编辑器消费鉴权精简布局，不再伪造20,000页。游标协议是有意的协调破坏，前后端必须同批部署；不保留会恢复深OFFSET和无界all的兼容分支。
- generation109会话临时全Schema验证350项完整遍历、搜索触发刷新与进度图字段/缓存/字节合同；100,000条真实目录上生产handler首屏636.940ms、深游标161.643ms、搜索273.650ms。1,000,000条TEMP影子上keyset为Index Only Scan 0.071ms，GIN搜索count为Bitmap计划0.053ms且目标表无Seq Scan。远端public generation85没有修改；开发库需重置到109，回滚必须同步前后端并会重新暴露本Finding风险。

## D-126：资料引用以请求内候选集一次解析并批量记录未命中项

- 旧同步先删除当前版本路径的未解析事实并读取模板，然后对每个reference/reference-list标识单独查询tag或game resource；未命中再逐条INSERT。四个各500项字段的真实夹具执行3,003条客户端SQL并耗时114.98秒，事务在整个网络往返期间保持打开。未解析tag写入还因`jsonb_build_object`参数缺SQL类型上下文而报错，合法未收录tag无法发布。
- 新路径仍按模板字段顺序生成完全相同的`version.{versionId}.{fieldCode}.{index}`，但先在Go内拆成tag和resource两个候选数组。tag统一去除前导`#`并小写，registry小写；resource保留raw ID并另存小写匹配值，kind继续经过既有规范化映射。字段内去重与500项同步边界不变。
- 每类候选由一个带ordinality的`unnest` CTE物化。tag批次一次关联活动catalog tag；resource批次分别关联活动canonical和alias再UNION命中ordinal；LEFT ANTI结果才写入两个未解析事实表。tag继续ON CONFLICT重置pending/resolution metadata，versionId/registry显式为bigint/text；resource继续只插入未命中raw事实。
- 包括两次前缀DELETE和一次模板读取，存在两类候选时同步恒5条SQL，空定义恒3条；数量不再随标识增长。删除和批量写仍使用发布调用方同一事务，任何错误整体回滚；真实测试在成功写入500 tag+500 resource事实后执行空替换并Rollback，原1,000事实保持不变。
- generation110增加`game_resources(kind_code,lower(canonical_id),entity_id)`、`game_resource_aliases(kind_code,lower(alias_id),resource_id)`和`catalog_tags(lower(canonical_id),lower(registry),entity_id)`索引，保留旧大小写不敏感解析语义。1M tag/resource/alias影子目录用生产批SQL验证三个索引，数据库执行5.505/8.769ms且目标大表无Seq Scan。
- HTTP请求/响应、审核发布状态机和未解析读取API均不变，无前端迁移。开发库重置到110；远端public generation85不迁移。回滚无需数据转换但会恢复逐项往返和tag发布失败，不能通过扩大连接池或延长HTTP超时替代批处理。

## D-127：布局先分配完整身份图，再以有序集合重建分类树

- 旧准备阶段在分类循环和相似组首次出现时分别执行`select new_public_id()`；1,000分类加20,000资源组成的10,000双成员组可产生11,000次读取。旧发布阶段又为每分类执行update/insert、删除本地化、逐语言insert，最后逐旧分类恢复system_key；64分类双语言真实RED已执行295条SQL并耗时44.10秒。
- 准备阶段先收集按请求顺序出现的临时category ID和去重similar group ID，解析当前Schema后一次`generate_series`调用权威`new_public_id()`。11,000个ID只用2条SQL并继续写入`public_id_registry`；随后纯内存映射分类自身/父级、resource section和相似组。1,000分类与10,000组映射到20,000 resources的顺序和同组身份由上限单测逐项验证。
- 发布仍先取得聚合advisory lock、锁定root、读取旧子树，再把旧分类ordinal和system_key移入staging并临时挂到root。它没有绕过四层树触发器；categories先按深度、父级、ordinal排序，所有新分类内部ID由同事务一次`nextval`预留，完整parent internal ID图在写入前形成。
- 一个带ordinality并按input order排序的`unnest` INSERT/ON CONFLICT语句同时新增和更新分类。冲突更新只允许同一内部ID、Mod和version；行数不完整立即失败。父分类总在子分类前经过真实before/after约束，default locale、display mode、ordinal、active状态、revision和updated_by保持原语义。
- 所有活动分类本地化由一次`section_id=any`删除和一次四数组unnest插入替代逐项写；不在新布局的旧分类用一次数组UPDATE归档，全部旧system_key用一次数组UPDATE恢复。资源placement/advancement继续复用既有集合差异函数；root revision/display更新仍是最后一步，任何错误回滚整个事务。
- 真实generation110临时全Schema以500旧+500新分类、每类en-US/zh-CN验证准备4 SQL、发布12 SQL和约1.050秒；2,000本地化值、父级、ordinal、500个既有system_key和零staging残留精确。深度移动/三层归档回归及包含资源/进度定义的完整发布回归继续通过。
- 无DDL、索引、HTTP或前端协议变化，generation保持110，远端public generation85未修改。回滚无需数据转换，但会恢复上万同步往返；不得通过关闭树触发器、降低分类/资源上限或延长事务超时伪装修复。

## D-128：公开项目列表使用独立有界卡片协议，完整正文只属于详情

- 审计旧路径：simple project列表上限100，却为每项读取全部最多8种`body_markdown`（单篇可1 MiB）、完整链接/图库、含团队成员的作者和全部父项目并逐图签名，理论正文 alone 约800 MiB。modpack列表读取每项1 MiB正文、完整链接/图库/作者及最多2,000内含模组并逐图签名，100项可装配200,000个嵌套模组。两者是匿名公共路径且没有最终响应字节上限，按High/P0关闭。
- 列表与详情不再共享response DTO或关联loader。simple card主查询只读取显示、筛选与路由字段；LATERAL本地化按请求语言、默认语言、同语言族和稳定回退排序后最多2条，且不选择正文。作者使用窗口排名最多8个且只返回name/role，父项目最多3个且只返回身份/名称，不读取链接、图库、团队成员或父图标。详情/editor继续走完整关联装配并保持全部语言正文和嵌套事实。
- modpack card主查询不选择`body_markdown`或编辑/修订字段，以`exists(gallery)`返回单一`hasGallery`。批关联只读取兼容性、tag和作者：每包最多16个loader、每loader最多32个版本、32个tag和8个name/role作者；链接、图库对象、团队成员和`modpack_mods`完全不进入列表路径。只有主项目图标需要签名。
- 两类列表在写任何header/body前把统一envelope原子编码并执行2 MiB硬上限；超限返回500明确错误，不截断items冒充完整页。对象及嵌套上限使编码前内存也有界。simple的显式`locale`必须是合法BCP-47；缺省使用Accept-Language再回退zh-CN。Next16目录、首页和资源选择器迁移到独立card类型并传当前语言，详情/编辑类型不放宽为可选大字段。
- 无DDL、索引、回填或generation变化，保持110；远端public generation85未访问。真实PG以100×8×1 MiB本地化、100×1 MiB模组包和单包2,000模组验证列表与详情分界；TEMP各1M行直接执行生产本地化/兼容SQL并命中复合主键，无目标Seq Scan。回滚必须协调前后端且会恢复匿名宽响应风险，不保留旧宽列表或query开关。

## D-129：作者关系以稳定键差分并在一个有界语句中提交副作用

- 旧生产同步无论payload是否变化都会删除项目全部`content_creator_bindings`，再逐作者执行INSERT。64个完全相同的已批准作者真实RED仍把全局`project_acl`版本从2递增到66；每条关系还触发一次搜索队列写。DELETE/重插同时轮换`id/public_id/created_at`并把当前普通内容编辑者伪装成新审批人，因此PERF-019与BUG-028必须同时验证，但仍是两个独立Finding。
- 新同步先`FOR UPDATE`锁定项目全部既有关系，以`creator_id + coalesce(role_id,0)`稳定键比较name/role快照、授权属性、状态和顺序。完全相同关系不构造mutation；调用者无对应author/team管理能力时，payload缺失的pending/rejected/revoked/approved关系均不变；有能力移除时只把目标稳定行改为revoked，不删除审计身份。
- 已有creator public ID、role public ID和最终creator事实各用一个批查询解析。全部新增、变更和撤销mutation由一个typed arrays `unnest` INSERT/ON CONFLICT写入，影响行数必须精确；首次进入approved才记录实际actor与时间，保持approved的关系继续保留既有审批元数据。内联新creator仍走完整创建/修订业务流，因为那是新增独立实体而非关系N+1；入口统一以64个作者/团队硬上限约束其成本。
- generation111把`bump_project_acl_runtime_version`改为transaction-local GUC守卫，使同一业务事务即使执行多条相关statement也只递增一次版本。作者关系搜索触发器从逐行函数改为INSERT/UPDATE/DELETE三个transition-table语句级触发器，对旧/新项目身份取distinct后一次upsert队列。业务代码的单批mutation因此对一个项目最多产生一个搜索队列写。
- HTTP路由、payload、成功/失败DTO、审核状态机和稳定公开ID格式均不变；唯一新增可观察边界是simple/modpack作者与团队合计超过64时失败关闭。无表、列、索引或回填；开发全Schema提升到111并需重置，远端public generation85未修改。
- 真实generation111临时全Schema证明64项no-op/重排分别只执行4/5条业务SQL，ACL/search副作用为0/1；同事务两条独立关系UPDATE只使ACL +1。旧身份回归逐字段保持公开ID、创建时间、审批人/时间和隐藏状态；1M关系表的生产锁定SQL只返回64行，命中subject索引并执行0.154ms。回滚会恢复身份破坏和全站缓存/搜索风暴，不提供旧DELETE重插开关，也不能以延长缓存TTL或禁用触发器替代。

## D-130：初始整合包只物化一种最终状态，approved 不建立临时发布投影

- 旧`createModpack`插入主记录后，无条件以`revisionID=0`完整调用`replaceModpackAssociationsTx`；建立revision后，approved分支又经`applyModpackSnapshotTx`调用同一替换。真实handler RED的8条兼容、4标签、3链接和16模组因此分别执行16/8/6/32次INSERT及8/4/3/16次DELETE；作者关系、搜索触发器和解析查询也进入两次调用链。
- 初始状态只允许两种明确编排：pending创建在revision前物化一次submitter可见的预览关系，随后只保存pending revision；approved创建不写预览关系，先保存approved revision，再由`applyModpackSnapshotTx`一次更新主记录、设置published revision并完整物化关联。两者都不保留第二关系权威或事后清理任务。
- `modpackCreationNeedsPreviewAssociations`把分支规则压缩为可单测谓词：只有非approved状态需要预览投影。approved调用仍复用发布函数，避免为初始创建复制关联写逻辑；pending调用显式传`projectApproved=false`，不能意外把预览作者关系当成已批准授权。
- 2,000模组的现有请求硬上限不降低，2,001仍拒绝；该分支判断与关联数量无关，因此最大payload同样只选择一次写路径。逐模组解析本身仍属于独立性能Finding，不以本项重复写修复冒充关闭。
- HTTP请求/响应、review status、revision/change request、完整详情回读及前端调用均不变。无DDL、索引、回填或generation变化，保持111；远端public generation85未访问。回滚会恢复所有关联/解析/触发器成本的整倍放大，不提供双写兼容开关。

## D-131：服务器目录游标绑定排序authority，派生索引不可用时只在新序列选择SQL

- 旧目录以`page<=10000`和`limit<=60`允许约60万OFFSET；组合版本、模组、布尔或排除筛选会绕过Typesense，先精确COUNT，再运行数组、相关EXISTS和动态排序。文本条件又把GIN谓词、前置通配name ILIKE与模组子查询放在OR中，使匿名请求同时承担重复全局工作。
- 新请求规范化全部筛选、排序、方向和limit并计算scope hash；游标是严格base64url JSON，签入scope、authority和该排序的完整稳定tuple。page/offset、未知游标字段、跨scope复用和超过2,048字节的cursor均400。每一页只请求limit+1，成功响应只有items、limit、hasMore和nextCursor，不计算或缓存一个会误导调用方的精确total。
- Typesense可用时，除name外的首屏把文本、多版本、多模组、四个布尔筛选和excludeSiteId统一写入同一filter，并以稳定数值键和internal_id续页；最终PostgreSQL只按返回ID数组读取一页卡片并保持索引顺序。Typesense单次最多三个排序字段，因此rating使用rating_score/rating_count/internal_id；无法稳定续接`_text_match`的relevance明确400，不伪造非确定游标。
- Typesense是可重建派生投影而非业务数据权威。首个请求在索引未就绪时选择SQL keyset；name始终选择SQL，因为Typesense字符串字段不提供所需范围续页。SQL只使用approved行、精确组合谓词和单一`to_tsvector @@ plainto_tsquery`，按sort/order携带完整tuple，不含COUNT、OFFSET、前置通配或文本OR。已签发index cursor若索引随后失效返回503；已签发SQL cursor即使索引恢复也继续SQL，防止中途切源造成重漏。
- generation112为approved服务器增加updated、created和lower(name)部分索引；Typesense projection schema v3新增internal ID、时间、热度微单位、下载、收藏、评分、浏览和评论稳定数值键。`content_popularity_stats`对minecraft_server route的插入/更新会重新入队，使排序投影随业务事实刷新。PostgreSQL仍在最终回表复核approved状态。
- Next16服务器目录以AbortController和请求代次追加/去重cursor页，只显示已加载数量；卡片Link禁用自动prefetch避免滚动列表放大。资源选择器把legacy目录的offset和服务器nextCursor封装为scope绑定组合游标，不再为各类型先发limit=1的COUNT预检。前后端必须协调部署；回滚会恢复深OFFSET、精确COUNT和双重分页语义，因此不保留兼容query或旧DTO。

## D-132：服务器审核队列只返回决策导航摘要，完整证据属于单记录详情

- PERF-022审计时列表固定取最多200条完整服务器，并在主rows关闭后对每项顺序读取证明文件、链接和模组，最多601条客户端SQL。BUG-139已先把队列改为scope绑定keyset且页上限100，但没有改变列表正文和三类关联过取；因此当前有效RED仍是最多301条SQL加100份完整正文，而不是把旧数字原样当现状。
- `minecraftServerReviewPageQuery`现在只返回名称、地址、短摘要、筛选/判断事实、提交者、审核状态/时间和proof/link/mod三个count。`body_markdown`与`proof_text`不进入SELECT；proof文件对象、链接对象和模组对象也不在列表handler装配。limit+1、status+created_at+ID顺序及已有opaque cursor保持，单页无论1或100项都只有一条客户端SQL。
- count不是前端猜测。proof数量只计算active且clean/trusted_generated的安全附件，link和mod按server ID精确聚合；三类都利用现有server前缀主键/索引，并只对最多limit+1个当前页候选执行。100k计划继续先从`idx_minecraft_servers_review_page`读取101行，数据库执行0.636ms。
- 新的GET `/api/v1/admin/server-reviews/{serverId}`受同一`server.review`权限保护，先读取一条完整服务器/提交者事实，再复用既有安全附件、链接和模组reader。单记录详情固定5条SQL（主记录、proof、link、mod、一次OSS设置），任一步失败都在写成功响应前返回错误；列表和详情没有第二份审核状态或证据权威。
- Next16管理面将`ServerReviewSummary`与`ServerReviewDetail`分型。队列首屏只显示摘要和三个数量；管理员展开一项时才请求详情并渲染Markdown正文、证明、附件、链接和模组。展开请求有独立AbortController、响应ID复核、错误展示/重试；状态切换、刷新和卸载会取消并清空旧详情，不能把上一条证据显示到新对象。
- 无DDL、generation或数据迁移，保持112；列表items的宽DTO收窄和新增详情GET要求前后端协调部署。回滚会恢复远程数据库往返线性放大与大正文过取，不保留query开关、自动预取所有详情或客户端并发301请求作为兼容层。

## D-133：项目文件只在一个来源内续页，跨供应商展示组合独立游标

- PERF-023的旧公开GET无LIMIT读取全部站内文件，同时调用Modrinth无分页的项目version数组和CurseForge最多10页/500文件；随后复制三份集合、全量按时间排序、遍历计算筛选项和全局数量。通用供应商reader虽有16 MiB上限，仍允许一次匿名请求解码、缓存和返回大数组；缓存只减少上游调用，不建立数据库、内存或响应预算。
- 新列表请求必须明确`source=internal|modrinth|curseforge`，limit缺省20、范围1..50。project type/public ID、source和limit共同进入scope hash；opaque base64url cursor拒绝未知字段、跨项目/来源/limit复用、page/offset和超过2,048字节的输入。成功页只含items、source、limit、hasMore、nextCursor及当前页versions/loaders，不再声称全局files或download totals。
- 站内文件使用已有`(project_type,project_internal_id,status,created_at desc,id desc)`索引和完整tuple keyset，一页只读limit+1；按public ID删除/读取也改为单行查询，不再借列表扫描。页面筛选项只由当前已加载页与项目详情已经提供的canonical suggested compatibility合并，避免为一个下拉框重新扫描所有文件。
- Modrinth官方项目version列表没有offset/limit，因此不能伪造数字分页。实现先以2 MiB上限读取恰好一个项目manifest，版本ID硬限10,000，再把按发布时间等价的反向ID顺序切成每批最多10个，通过官方批量versions端点取详情；单请求最多5批、单version最多32个文件、单批最多128个文件。cursor保存“下一个version ID+version内文件offset”而不是数字位置，新版本追加不会让续页重复；锚点被删除时失败关闭。每个有界manifest/批次仍可缓存，但缓存不替代上述上限。
- CurseForge使用官方`index/pageSize`，每次请求严格为limit+1；响应文件数不得超过请求或128硬限，空页与total矛盾会失败。slug解析最多接受一个项目，项目搜索、文件页、单文件和download URL JSON都使用2 MiB专用上限。Modrinth下载按hash读取一个version并复核canonical project ID，CurseForge按`/mods/{project}/files/{file}`读取单文件；下载详情不再加载完整供应商列表。
- Next16下载面首屏并发读取三个来源各20项，分别保存opaque cursor；“全部来源”只对仍有cursor的来源继续请求并按`source:id`去重，单来源筛选只推进对应cursor。后端每页仍以2 MiB原子响应预算编码，供应商错误表示为该来源warning和空页，不把部分大JSON写成200。
- 无DDL、索引、回填或generation变化，保持112；远端public generation85未访问。GET请求/响应是协调破坏，前后端必须同批部署；不保留缺source的全量兼容分支。回滚无需数据操作，但会恢复匿名无界聚合和下载时全列表验证，因此不作为兼容策略。

## D-134：更新日志时间线只返回选定语言摘要，完整多语言正文属于单条详情

- 旧集合reader无LIMIT读取目标全部approved/active日志，并为每项以`jsonb_object_agg`组装全部本地化正文；单正文允许200,000字符、可编辑语言最多8个，编辑权限还把同一map再次展开成localizations。历史长度、正文体积和语言数相乘后直接进入匿名响应和Go堆。
- 集合改为默认20、最大50的`(event_at,id)`降序keyset。cursor是严格base64url JSON，scope绑定route、entity type/public ID、locale和limit；未知字段、跨scope复用、超过2,048字节、page/offset及越界limit均400。limit+1只决定`hasMore/nextCursor`，不执行COUNT。
- `projectChangelogSummary`与详情DTO分离。列表LATERAL按请求locale、entry default、zh-CN、en-US和稳定locale回退只选择一篇正文的前4,001字符；Go最终只暴露4,000字符`bodyExcerpt`与`bodyTruncated`。列表另取最多9个locale标识并在超过8时失败，不选择或聚合其他正文；分类在item中只有id/default/name，首屏分类元数据最多100类×8语言。
- 完整`bodyMarkdown/localizations`只由GET单条详情读取。详情聚合子查询最多取9语言并对超过8或单正文超过200,000字符失败关闭；编辑器先取单条详情，再用集合首屏取得分类，不再因`target.canEdit`把整条时间线的多语言正文复制到内存。
- Next16时间线消费`hasMore/nextCursor`并按ID追加去重；语言/目标变化以请求代次拒绝旧响应。列表渲染摘要，截断项链接到完整详情；分类和Minecraft版本筛选只描述已经加载的有界集合，不声称全历史统计。
- 无DDL、索引、回填或generation变化，保持112；复用`idx_project_changelogs_target`与本地化主键。列表DTO收窄是协调破坏，前后端同批部署且不保留宽列表开关。回滚无需数据操作，但会恢复无界全历史正文聚合，不能用gzip、缓存或前端忽略字段代替服务端投影边界。

## D-135：项目更新通知用事务内成功增量记账，event索引只服务对账

- 旧Worker每批最多200人后都执行`count(*) from notifications where project_update_event_id=$1`，再覆盖task.notified_count。唯一幂等索引是`(recipient_id,project_update_event_id)`，event不是首列；高关注事件因此按批次数重复扫描已经增长的同一通知集合，并把扫描留在持有task行锁的事务中。
- 每个通知INSERT已有`ON CONFLICT ... DO NOTHING`并检查`RowsAffected`。新路径把本批实际新增recipient数量传给单一进度UPDATE，以`notified_count=notified_count+$3`原子累加；通知INSERT、next_user_id、pending/completed状态和增量仍在同一事务提交或回滚。重试命中既有唯一键时delta为0，不重复计数。
- task由`FOR UPDATE OF task`串行同一event的批处理，故不需要独立计数器表、乐观版本或全局锁。notified_count定义为成功创建过的历史通知数；后续用户删除通知不倒扣任务历史，不再通过热路径COUNT把它悄悄改成当前存量。
- generation113增加部分索引`idx_notifications_project_update_event(project_update_event_id) where project_update_event_id is not null`。它不被每批Worker调用，而为运维/异步最终对账和异常修复提供匹配访问路径；1M通知中目标事件1,000项使用一次Index Only Scan，执行0.307ms。
- 无HTTP、Outbox、NATS、模板、前端或任务消息协议变化，也不回填已完成任务。开发数据库需重置到113；远端public generation85未迁移或写入。回滚需要恢复generation112并会重新引入重复COUNT；不能只删除新索引却保留旧热路径作为长期状态。
- 本项不吞并OPS-004：失败事务中的attempt_count回滚及其重试上限语义仍是独立开放Finding，不能用本次成功计数修复冒充Worker失败生命周期已经关闭。

## D-136：自动镜像用受控临时文件流和数据库全局槽限定资源

- 旧镜像对每个声明不超过256 MiB的文件执行`io.ReadAll(LimitReader(...))`，随后对同一`[]byte`计算SHA-1/SHA-512/SHA-256并以`bytes.NewReader`上传。每轮最多25个文件虽按循环处理，但多个Worker实例没有共享容量边界；堆峰值和GC压力可按实例并发相乘。
- 下载改为0600 OS临时文件。一个128 KiB缓冲通过`io.MultiWriter`同时写文件和三个hashers，最多读取上限+1字节；Content-Length预检、实际长度、provider size及可选SHA-1/SHA-512仍严格一致，站内SHA-256由同一流产生。超限第一个字节、长度或哈希不符立即关闭并删除临时文件。
- 成功spool在上传前seek到0；OSS PutObject继续携带精确ContentLength与SHA-256 metadata，但Body是文件reader。成功、下载失败、哈希失败或上传失败都执行Close+Remove，不把完整内容转回Go字节切片。进程崩溃只可能留下OS临时目录文件，由主机临时目录生命周期治理，不写应用工作区。
- 全站并发预算由PostgreSQL session advisory locks提供四个固定槽，namespace是`mcmods.project_auto_update.mirror`。所有实例先在独立pool connection上`pg_try_advisory_lock`，无槽时按100ms等待并受provider请求超时/父context取消；槽覆盖下载、哈希和上传。正常unlock后连接才回池；unlock失败则Hijack并关闭连接，使session销毁自动释放锁，不能把锁污染带回连接池。
- 四个最坏文件同时占用约1 GiB临时磁盘和4×128 KiB显式缓冲，这是明确全站预算；数据库为协调付出最多四条长持有连接。无需新表、DDL或generation变化，保持113；HTTP、OSS对象、mirror记录和扫描状态协议不变。
- 268,304,384字节近上限回归耗时0.60s且总堆分配138,192字节；双独立pool真实PG占满四槽后第五获取超时，释放任一槽后另一pool立即成功。回滚无数据转换，但会恢复每实例256MiB堆放大，不保留内存模式开关。

## D-137：爬虫运行与候选使用独立稳定游标，完整供应商载荷只属于单条详情

- 旧管理端运行列表虽然接受limit但永远从`created_at desc,id desc`第一条开始；候选列表固定100并把每行完整`payload`装入响应。两者都没有下一页，超过窗口的失败运行和低下载候选不可达；未约束的draft普通LEFT JOIN还可能把一个候选扩成多行。
- 运行页改为默认30、最大100的`(created_at,id)`降序keyset；候选页使用`(downloads,id)`，可选status必须属于封闭状态集。两类严格base64url JSON cursor的scope分别绑定列表种类、status和limit，拒绝未知字段、超过2,048字节、page/offset及跨筛选/limit复用，不提供COUNT。
- 候选列表DTO不再含`payload`。active draft使用按`updated_at desc,id desc limit 1`的LATERAL只返回一个公开ID，维持每候选唯一行。新增GET `/api/v1/admin/seed-crawler/candidates/{id}`在同一`seed_crawler.view`权限下按单个外部项目ID读取完整载荷；空/超过200字节ID失败，no-row明确404，其他数据库错误不伪装不存在。
- generation114新增运行`(created_at desc,id desc)`、候选`(downloads desc,id desc)`和候选`(status,downloads desc,id desc)`三个索引。status为空时SQL不保留`($1='' or ...)`谓词；有筛选时生成明确等值条件，使稀疏状态在1M夹具上命中status-leading索引。开发数据库需重置到114；远端public generation85未迁移或写入。
- Next16管理面分别保存运行和候选的`hasMore/nextCursor`，显式点击才追加且按稳定公开ID去重；候选载荷只在管理员点击“查看详情”后请求并渲染。首屏、运行触发和保存配置都会重建两类首屏并清除旧详情，不一次预取所有候选载荷。
- 真实PG以100k runs和1M candidates验证两类各两页恰2 SQL、各100项无重漏、候选摘要<128KiB且没有`payload/SECRET-TAIL`。运行、候选和稀疏status计划分别执行0.043/0.181/0.552ms并命中三个目标索引。回滚需协调前后端且会恢复固定窗口和宽响应，不保留旧items-only或payload列表兼容分支。

## D-138：通知历史使用定向/广播双流游标，“全部已读”只推进用户水位

- 旧GET把用户定向通知与全站广播用OR合并，最多返回最新200条且只按`updated_at`排序；更旧通知无法访问，时间相同时顺序不唯一。旧read-all对所有历史可见行执行`insert into notification_receipts select ... on conflict`，一个新用户面对百万广播会在首次点击制造百万行写、外键/唯一索引维护和WAL。
- 新GET默认50、最大100，以user ID、kind和limit计算scope，严格base64url cursor携带`updated_at+id`。SQL在定向与广播索引上分别取limit+1，再由Merge Append形成全局稳定页；page/offset、未知字段、跨用户/kind/limit游标及超过2,048字节输入均400，不执行COUNT。
- generation115把旧缺少tie-breaker的索引替换为recipient/broadcast各一条通用`updated_at,id`和一条kind-leading索引。候选页确定后才回表，并以每条最多3个actor的LATERAL装配；前端本来只显示前三人，因此不再先聚合无界actor数组再在浏览器丢弃。整个API envelope写前受2 MiB原子预算。
- `notification_read_watermarks`每用户只保存`max_notification_id/read_at`。read-all在单条upsert中用主键反向Index Only Scan取得当前全表最大ID并记录`clock_timestamp()`，不扫描用户历史、不创建receipt；重复操作只更新同一行。全局最大ID安全覆盖当时所有可见旧行，其他用户的不可见ID不改变可见性，随后插入的ID更大而保持未读。
- 单条receipt仍是更具体的权威：存在receipt时由其read_at决定；没有时，只有`id<=max_notification_id`且`updated_at<=read_at`才由水位视为已读。聚合通知更新统一使用`clock_timestamp()`并在未读选择中读取水位，因此水位后更新会重新未读，已水位读取的旧粉丝通知不会被误当成当前未读聚合目标。unread summary和现有对账路径同步消费同一谓词；PERF-029的每用户相关COUNT规模问题仍保持开放。
- Next16消息中心消费`items/limit/hasMore/nextCursor`，分类切换递增请求代次并清空旧页，加载更多按ID追加去重；read-all响应改为`readBefore`并只更新已加载视图。真实1M夹具证明kind/无kind双流页执行0.700/0.469ms，read最大ID执行0.049ms；两页恰2 SQL，read-all恰1 SQL、1水位、0 receipt，随后新建和更新各产生一条未读。
- 前后端需协调部署，开发Schema重置到115，远端public generation85不迁移或写入。回滚需要恢复数组DTO并删除水位语义，会重新暴露不可遍历历史和O(N)历史写放大；不保留旧read-all批写、offset或客户端一次拉全兼容分支。

## D-139：未读校准只抽样现存缓存派生，共享广播由数据库单例事实表达

- 旧周期任务保存一个user ID cursor，每30分钟从所有active用户继续最多batchSize人；走到尾部再归零。每一用户行都执行一次通知相关COUNT和一次私信相关COUNT，所以“分批”只限制瞬时峰值，仍保证长期遍历全站账户，并把完全相同的广播集合按用户重复计数。
- 未读缓存本身已有成功写后的定向通知/私信增减、聚合与read-all的按用户失效、广播创建的全局epoch以及短TTL懒重建。校准的合理对象因此是本进程实际服务并仍在TTL内的缓存派生，不是所有注册账户。`UnreadReconciliationCandidates`清除过期项、按user ID轮转，每轮由配置值和硬上限16共同限定；无候选时不访问PostgreSQL，也不保存会最终覆盖全用户的持久cursor。
- 统一truth查询先物化显式用户集合。定向通知以`recipient_id=any(ids)`一次从recipient-ID索引读取，私信同样按recipient/read索引集合聚合；广播不再和每个用户的定向流做OR连接。无水位用户直接读取`notification_broadcast_state.live_count`；有水位用户依据历史最大广播ID选择当前存活广播的较小ID侧，再补`updated_at>read_at`的旧ID更新。显式receipt只作为按user索引读取的稀疏正负修正，保持receipt优先于水位的既有语义。
- generation116新增单行`notification_broadcast_state(singleton,live_count,max_notification_id,updated_at)`。通知insert/delete以及recipient在null/非null间切换的触发器同事务增减live_count，max ID只单调增加；`idx_notifications_broadcast_id(id) where recipient_id is null`支持水位范围，`idx_notifications_recipient_id(recipient_id,id)`支持样本定向事实。开发数据库整代重置，无旧数据回填；远端public generation85保持未动。
- 管理员显式最多100个public ID的校准端点保留原请求和items响应；它先解析内部ID，再复用相同truth，不恢复后台全账户扫描。公开unread summary协议也不变并复用同一truth。系统广播删除现在与创建一样提升共享cache epoch；单条read SQL先使用receipt+watermark逻辑未读谓词，因此点击一个已经被read-all水位覆盖的通知不会误减其他未读数。
- 真实PG在10万、100万、1000万纯广播梯度上，每次16用户truth恰一条客户端SQL，结果分别精确等于广播规模；执行0.559/0.503/0.505ms，均命中recipient-ID索引且没有notifications Seq Scan，广播水位子计划对无水位样本不执行。另一个1M混合夹具覆盖定向、广播receipt、read-all、单条已读及私信组合语义。
- 回滚需恢复generation115并删除单例/触发器/两个索引，同时会让广播再次进入每用户大表COUNT，不能仅把抽样改回全用户cursor。抽样与TTL是对可靠增量派生的校准层，不宣称建立第二套永久逐用户计数事实；真正出现的缓存漂移仍通过用户访问懒重建、轮转样本或管理员显式信号修复。

## D-140：私聊会话持久最近消息/未读摘要，列表和历史只接受稳定ID游标

- 旧会话GET对用户的全部会话无limit返回；每行用一个最近消息LATERAL和一个未读相关COUNT读取`direct_messages`。旧会话索引只有不含成员的全局`updated_at`，消息读取按`id desc`却只有`(conversation_id,created_at desc)`；现有`after`只能读取锚点之后，最新100条以前的历史没有入口。
- 会话的`updated_at`继续表达最近消息活动时间，发送事务同时保存`last_message_id`。更新以message ID单调比较：并发较旧事务晚到时不能覆盖较新的指针或活动时间。列表把user_low和user_high两个成员索引各取limit+1，再按`(updated_at,id)`Merge Append；最近正文用指针主键联接，不再为每一行搜索消息历史。
- `direct_conversation_unread_counts(conversation_id,user_id,unread_count)`是精确派生事实。`direct_messages`的insert/delete以及conversation/recipient/read_at变化由同事务trigger增减，零值行删除；打开会话的既有批量read UPDATE因此同时把摘要归零。会话页只按摘要主键联接，仍保留`(recipient_id,conversation_id,id) where read_at is null`给匹配谓词和诊断；全局unread truth继续使用原recipient/read索引。
- generation117新增成员两侧`(user_*_id,updated_at desc,id desc)`、消息`(conversation_id,id desc)`和未读部分索引；删除不能服务成员排序的`idx_direct_conversations_updated_at`以及与真实ID排序错配的conversation/created_at索引。`last_message_id`对消息使用`on delete set null`外键，完整临时Schema验证会话删除时与message cascade可共同完成。
- 会话GET默认30、最大100；cursor绑定认证user ID与limit并严格拒绝未知字段、page/offset及跨用户/limit复用。消息GET默认/最大100；历史cursor绑定member、conversation和limit并以`id<cursor`读取更早页，响应仍按ID正序供聊天UI追加。两类成功DTO统一为`items/limit/hasMore/nextCursor`且写出前受2 MiB预算。
- `after`仍作为实时增量锚点保留，避免把BUG-045的无效锚点状态语义混入本项关闭；但查询改为取锚点后的最早limit+1而非最新100条，客户端在hasMore时继续用本页最后ID，故一次离线超过100条也不丢中间消息。历史cursor与after互斥，非法组合和重复参数400。
- Next16消息中心分别保存会话页和历史页cursor，按ID去重追加/前插并提供显式“更多会话/更早消息”。显式`?user=`打开一个不在首屏的旧会话时，以公开profile组成只读占位并保留选中项；发送后权威首屏摘要替换它。请求代次阻止旧会话响应覆盖当前消息，实时循环只排空当前会话。
- 100k会话/1M消息真实PG夹具中，会话和历史各两页各只执行2条SQL且无重漏。会话Merge Append、稀疏会话历史和未读部分索引计划分别执行0.302/0.119/0.066ms；generation117触发器覆盖unread insert/read/recipient move/delete和会话级联。前后端必须协调部署并重置开发Schema；不保留旧裸数组、固定100历史或相关COUNT兼容路径。

## D-141：实时事实只失效精确查询键，同一tick与in-flight只产生一份请求

- 旧`RealtimeBridge`对每个SSE先广播`mcmods-realtime`，再无条件广播`mcmods-unread-change`。消息中心有两个原始事件Effect：外层message读取会话，当前会话Effect又读取消息+会话；Header读取未读，消息GET成功还会再次广播未读。一个`message.created`因此至少形成2次会话、1次消息和2次未读请求，`unread.changed`甚至错误刷新消息与会话。
- 新`realtimeQueryCoordinator`是浏览器客户端唯一失效权威。查询键绑定认证user；通知键再绑定kind，消息键再绑定conversation public ID。`message.created`只失效会话和匹配当前订阅的消息键；当前会话不存在时才直接失效未读。`notification.created/changed`只失效未读与事件kind，`unread.changed`只失效未读；未知通知kind只失效当前已知的该用户通知键。
- 失效先进入`Set`并在一个microtask统一flush，同一键和同一callback在同tick最多执行一次。Header与消息中心虽然都消费unread summary，但通过同一个`readQuery`共享in-flight Promise和1秒新鲜窗口，所以两个listener只发一条HTTP。失效递增generation并删除缓存；旧请求在失效后成功或失败都不能回填旧值，而是加入当前请求取得新结果。
- 打开当前会话时，message事件不先读取即将被GET标成已读的旧未读值；消息GET成功后后端既有`unread.changed`只触发一次共享未读读取。若页面不可见或消息读取失败，消息listener退化为未读失效。加载期间再来的事件递增sequence，当前请求完成后从最后ID补拉，既合并请求又不丢事件。
- 本地单条/全部通知已读没有服务端实时事件，成功后显式失效同一unread键；UI仍先做既有乐观更新。发送消息和创建会话先失效会话键再读取，手动调用与订阅调用也由in-flight合并。旧`mcmods-realtime`及`mcmods-unread-change`的生产者和consumer全部删除，不保留两套刷新总线。
- 无后端、HTTP/SSE协议、Schema、数据或generation变化，保持117；远端public generation85未访问。100次message+100次unread同tick验证为1会话+1当前消息+1未读，100次notification验证为1分类+1共享未读。回滚只涉及前端，但会恢复按组件/事件/标签页乘法放大，不能把延迟timer或请求节流当成精确失效。
- PERF-031按Medium/P1关闭：单个请求已有硬上限，但高频实时活动把重复系数直接乘到API与数据库负载。OPS-007的SSE重连/恢复、BUG-046的服务端NATS回声及TEST-023的完整React/EventSource故障矩阵仍独立开放。

## D-142：API访问日志是有界可丢弃样本，业务安全审计不进入同一队列

- 旧`logAccess`在每个`/api/` Handler返回后直接调用`writeAppLog(context.Background())`，每次单独INSERT且最长等待2秒。成功Presence、未读、翻译轮询、视图和普通GET与失败/状态变更完全同权；数据库越慢，越多请求goroutine在业务完成后继续占用并排队制造新写。
- 新路径在请求goroutine内只规范化有限字段并执行channel非阻塞send。队列固定1024项，path/target/query各最多2,048字节、User-Agent 512、IP 64、method 16，故容量不仅按条数也按字节受控；队满或关闭后立即丢弃并增加`overflowDropped`，绝不回退到同步数据库写。
- 取舍策略先保留所有4xx/5xx与非高频状态变更请求；成功普通GET/HEAD/OPTIONS以进程内稳定序列每16项保留1项。成功站点/会话Presence、SSE、两个未读端点、三类翻译轮询、review lock及content view明确不入队。错误优先级高于高频分类，但在整个队列已满时仍可丢弃，因为访问日志被定义为best-effort而不是安全事实源。
- 单Worker每100ms或积满128项，把JSON数组交给一次`jsonb_to_recordset` INSERT；每批后台超时1秒。失败整批计入`failed/writeFailures`后丢弃，不阻塞Handler；关闭时禁止新入队并排空现存批，若数据库仍故障则返回可见关闭错误且不无限重试。`enqueued/sampledOut/policyDropped/overflowDropped/flushed/failed/batches/writeFailures/queueDepth/capacity`加入管理员基础设施指标。
- 显式`admin_operation`、治理动作、权限审计、登录安全和其他业务事件不经过访问样本队列，其原同步/事务权威保持；本项不以可丢弃访问日志冒充安全审计。现有`api_access`行字段与管理员日志读取兼容，只是覆盖率成为明确采样协议，created_at保存请求完成时刻而非批写时刻。
- 无DDL、索引、回填、前端或公开HTTP/SSE变化，generation保持117。真实PostgreSQL对`app_logs`持有排他锁时，64个错误请求仍在1.0424ms完成；锁释放后恰64行由4批SQL写入。旧实现会在每个请求尾部等待2秒，故该测试直接保护反馈放大根因。
- PERF-032按High/P0关闭：公开API可把额外写和2秒故障等待变成可用性放大。SEC-020的OAuth查询参数、BUG-051的ResponseWriter可选接口、PERF-033的查询/清理规模和TEST-025完整中间件故障矩阵均保持开放；query当前仅做容量截断，不据此冒充敏感键已经脱敏。

## D-143：管理日志使用时间/ID游标与写时全文投影，保留清理每来源只删一个稳定小批

- 旧GET仅按`created_at desc limit 500`返回裸数组，同一时间没有稳定tie-breaker且没有下一页；搜索把`%q%`应用于动作、路径、IP、邮箱、User-Agent以及每行`payload::text`转换。旧记录永久不可达，百万日志的关键词请求必须转换/扫描宽行。
- 新请求只接受单值`category/q/level/status/from/to/limit/cursor`。默认100、最大500；拒绝未知参数、page/offset、重复值、201字符搜索、非法日期/状态和反向时间范围。base64url JSON cursor携带version、`created_at/id`及全部筛选和limit的SHA-256 scope，未知字段、超长、跨筛选或非正ID均400。
- 四个来源各生成明确SQL，以`(created_at,id)<cursor`排他续页并`order by created_at desc,id desc limit+1`；无COUNT或OFFSET。查询/rows/解码错误在成功响应前返回500，不再由旧日志列表辅助函数伪装成空数组；items只有确认存在下一行时才返回nextCursor。
- generation118在应用、权限、登录与上传日志增加`search_document tsvector`。before-write trigger把原受搜字段及当时actor/operator/target/uploader公开ID、用户名和邮箱快照投影为最多16,384字符的simple词典文档；读取只做`@@ websearch_to_tsquery`并命中各表GIN，不再查询期转换JSON或前后通配。
- 应用日志使用`(category,created_at desc,id desc)`，其余三类使用`(created_at desc,id desc)`；扫描日志清理索引也补ID tie-breaker。保留策略保存时每来源最多执行一个1,000 ID批：子查询按稳定时间/ID排序并`for update skip locked`，外层按ID删除，不再让一次HTTP配置保存清空任意规模历史。
- Next16管理面消费`items/limit/hasMore/nextCursor`；加载更多复用发出当前cursor的精确查询字符串，即使输入框随后编辑也不会跨scope拼接。请求代次阻止旧响应覆盖，加载按钮只在hasMore显示。数组旧协议不保留，前后端需协调部署并重置开发Schema。
- 真实PG的1M应用日志加三类各100k日志验证两页无重漏、应用页命中复合索引、四类搜索命中GIN且日志表零Seq Scan，清理计划命中同一页索引并精确删除1,000行；generation118隔离安装验证四类trigger均产生可检索文档且远端public generation85不变。
- PERF-033按High/P0关闭：旧宽搜和无界DELETE直接竞争数据库CPU、锁与WAL并使管理历史不可达。BUG-052的自动调度缺失与BUG-053的清理错误/其他通用辅助吞错仍保持开放；本项只使列表读取错误可见和每次已有清理调用的工作量有硬上限，不冒充日志生命周期已可靠自动执行。

## D-144：热度刷新读取写时累计事实，时间衰减由每route下一检查时刻驱动

- 旧`refresh_content_popularity`对每个route分别扫描全部浏览日桶、去重访客、页面、下载counter、收藏、直接及子资料评论、评分和维度评分。即使单个谓词有索引，刷新成本仍与该route全部生命周期历史线性增长；旧15分钟调度又对全体route执行近90日事件、有效推广和创建时间的相关判断。
- generation119引入三层事实：`content_popularity_view_totals`保留每route 32 shard的累计浏览以避免把已有浏览写重新集中到一个热点行；`content_popularity_lifetime_facts`维护去重访客、页面、下载、有效收藏/评论/评论者和评分总数/总分；`content_popularity_rating_dimension_facts`维护维度数量/总分。insert/update/delete及隐藏/恢复状态转移都写时增减，负delta通过先确保零行再原子UPDATE实现，非负约束继续暴露真实underflow而不掩盖漂移。
- hot refresh只读取上述累计事实、最多90天的`content_popularity_events_daily`和仍有效推广；不再引用任一生命周期原始表。长期得分、Bayesian评分、维度平均和view/page口径保持既有公式；旧未使用的直接/子评论者重复聚合随全历史路径删除。
- 累计事实允许显式`rebuild_content_popularity_lifetime_facts(route)`从权威历史重建view shard、全部累计列和维度列并写`calibrated_at`。它明确是维护窗口中的低频离线校准，不由请求或常规刷新调用；开发期generation重置负责初始装载，不为旧Schema增加双写或在线回填。
- `content_popularity_stats`新增`decay_until`和`next_decay_at`。refresh根据新项目60天、最近趋势日+91天及推广到期计算是否仍需衰减，每次最多安排15分钟后的下一检查；无时间依赖时写NULL。启动只做一次缺失统计补齐，周期路径只扫描`next_decay_at<=now()`的部分索引并入既有持久队列，不再对全route执行相关EXISTS或创建时间函数。
- 真实临时generation119执行浏览、独立访客、页面、下载、收藏、评论、评分及维度的正向、更新、隐藏/恢复、删除和级联逆向事件；故意把累计列改成77后，rebuild准确恢复为历史事实。将原独立访客表改名并建立只有`sample_id`的100k/1M/10M trap表、DISCARD PLANS后真实refresh仍分别61.1683/31.9666/32.2441ms，证明hot路径没有隐藏的历史访问。
- exact生产到期入队SQL在100k/1M/10M合成stats中只命中`idx_content_popularity_stats_decay_due`且零Seq Scan；1/10/100个到期route全部精确入队，分别59.5663/30.2629/31.8556ms。远端public generation85始终未修改。
- PERF-034按High/P0关闭：线性全历史聚合与周期fleet scan会把后台刷新转为持续数据库I/O。PERF-035仍包含按类型全局评分扫描，BUG-057/MAP-007仍包含多开发者集合被压成单owner，TEST-028的其余多作者/并发/队列矩阵仍开放；本决策不借增量事实冒充这些独立缺陷已修复。

## D-145：全局评分先写类型累计，集合重建退出项目热度Worker

- 旧`processContentStatsTask`在每个需要刷新热度的route前检查其entity type的全局评分统计是否超过5秒；一旦过期就全扫该类型所有published评分。聚合过滤又对每一评分调用PL/pgSQL `content_target_owner_id(route.id)`，其内部再次查询route和`effective_project_access`，因此一次类型聚合同时具有全历史线性成本和数据库内N+1权限解析。
- generation120新增`adjust_content_rating_global_stats(type,count_delta,sum_delta)`。rating trigger已经判断当前单owner、actor状态和security门槛，本决策复用同一`old_metric/new_metric`事实，在rating insert、overall update、隐藏、恢复和delete事务中原子维护`rating_count/rating_sum/average_rating`；同类型有效评分更新只写总分delta，跨类型/资格变化才减旧加新。零评分平均值恢复既有3.5先验，非负约束继续阻止underflow。
- 普通项目热度Worker完全删除`refresh_content_rating_global_stats`和`updated_at<5 seconds`分支；每个route只刷新自己的metrics/popularity并删除queue任务。全局评分总量的日常成本由类型全扫降为每次评分写的一行更新，读取继续使用原`content_rating_global_stats`协议。
- 旧refresh函数替换为显式`rebuild_content_rating_global_stats(type)`离线校准。它先把目标类型route与`effective_project_access`集合联接、按route取developer最小user ID，再一次联接rating/users完成COUNT/SUM/AVG；SQL文本和真实计划均不含`content_target_owner_id`。使用`min(user_id)`是为了保持当前有损单owner产品语义，不能被解释为BUG-057或MAP-007已修复。
- 用户status/security或项目权限集合批量变化不会逐评分反向触发全局累计；运维需在写入静止的维护窗口显式调用set rebuild。完整临时Schema把global行故意改成77/77/1后准确恢复1/4/4，并验证overall 4→3、hidden、republish、delete依次得到1/3/3、0/0/3.5、1/3/3、0/0/3.5。
- exact生产集合SQL在100个route和1个owner的100k/1M/10M合成评分上分别134.523ms、572.4396ms、4.9420804s；精确排除1k/10k/100k owner评分并得到其余评分平均4，计划无逐行owner函数。该线性工作只允许出现在显式离线校准，不再由高频Worker触发。远端public generation85未修改。
- PERF-035按High/P0关闭：原路径能在活动route队列下每5秒反复制造全类型扫描与逐行权限查询。BUG-057/MAP-007的多developer集合排除、TEST-028的其余多作者/并发/队列行为矩阵仍独立开放；本项不通过扩大语义范围提前关闭它们。

## D-146：后台项目目录使用写时搜索/排序投影与scope绑定复合游标

- 旧后台项目列表每次先对`top_level_project_catalog`执行精确COUNT，再用`name ILIKE '%q%' OR public_id=q`筛选；实际页还联接metrics/popularity，按派生heat、updated和route排序后应用无上限OFFSET。项目到百万级时，深页会重复扫描、排序和丢弃前缀，名称包含搜索也不能使用普通B-tree。
- generation121新增`admin_project_catalog`紧凑投影，保存四类顶层项目的公开ID、类型、名称、路径、审核态、列表统计及`heat_score/updated_at/object_route_id`排序键。`search_document`是由公开ID和名称生成的stored tsvector；route创建/路径变化、四类项目更新以及metrics/popularity insert/update都在来源事务中刷新单route投影，route删除由外键级联。
- 投影新增全局heat复合索引、type-leading heat复合索引和search_document GIN。列表不再联接基础表或执行COUNT；无搜索、类型筛选和深游标均读取limit+1并以`(heat_score,updated_at,object_route_id)<cursor`排他续页。搜索使用`public_id=q OR search_document @@ websearch_to_tsquery('simple',q)`；规划器可按选择性在GIN和有序heat索引间选择，禁止Seq Scan和OFFSET。
- 请求只接受单值`q/type/limit/cursor`，默认30、最大100；拒绝未知、重复、page/offset、非法type、超过200字符/800字节查询。base64url JSON cursor严格携带version、固定六位heat、UTC updated、正route ID和由规范化q/type/limit计算的SHA-256 scope；未知字段、超长、越界数值或跨scope复用均400。
- 响应由`items,total,limit,offset`协调迁移为`items,limit,hasMore,nextCursor`。Next16管理面移除total/range显示与数值offset，保存当前cursor栈实现Previous/Next；搜索或类型变化清空cursor历史和已选详情。精确项目详情路由、趋势DTO及后台总览的站点总项目数不变。
- 完整临时generation121验证项目创建、改名、全文向量、metrics和popularity更新都同步到投影。exact生产SQL在100k/1M项目上首屏62.1933/28.9624ms、深游标60.5087/30.3323ms、类型页58.2921/29.6335ms、稀疏搜索108.7212/610.2039ms；全部低于2秒回归阈值且无项目表Seq Scan或OFFSET。
- PERF-036按审计登记的Medium/P1关闭。TEST-029还要求内容统计、热度毒任务/多实例claim、权限包装和故障矩阵，保持独立OPEN；本决策新增的项目列表边界与规模测试不能替代其余覆盖。开发数据库需整代重置到121，远端public generation85未迁移或写入；回滚需协调恢复generation120和旧前端DTO，会重新引入COUNT/ILIKE/OFFSET，不提供双协议兼容。

## D-147：搜索重建按稳定ID流式装载并在数据库与导入边界实施硬预算

- 旧`Worker.rebuild`对每个collection调用`loadDocuments(kind,nil)`，把该collection的全部`[]map[string]any`留在堆中，再仅把已经物化的slice按500条发往Typesense。项目投影还对每个项目执行多组相关聚合并读取完整Markdown、本地化正文和数组，因此collection规模、正文规模及关联规模会同时放大Go堆与数据库工作。
- 新重建按collection内固定document type顺序执行；每轮以内部主键`id>$after order by id limit 500`读取稳定ID页，只为该页执行typed loader和导入，随后丢弃文档slice。旧collection loader、nil代表全量及`$all OR id=ANY(...)`入口全部删除，使生产代码不再具备一次装载全collection的隐式路径。
- Mod、Modpack与simple project loader先用selected-ID CTE限定本页，再分别对本地化、标识、创作者、标签和兼容性做按父ID的集合预聚合，消除每项目多组相关表查询。项目自身和本地化Markdown在SQL侧先截断，关联数组限制16或64项且逐值截断；Go侧`compactStrings`再以UTF-8安全方式对每个输出数组实施64项、8KiB总预算。
- JSONL导入以与客户端相同的一行一文档编码估算精确请求大小；累计超过8MiB前flush，单文档超过8MiB立即失败，不回退到无界请求。行数页限制和字节请求限制是两个独立边界：前者限制数据库/Go live集合，后者限制HTTP缓冲和Typesense接收体。
- generation122新增`search_index_rebuild_progress`，每个collection持久保存目标collection名、当前document type、最后ID、累计文档/字节/batch数、building/complete/failed、错误及时间。每页成功导入后才推进进度，alias切换及`search_index_state`写入成功后标记complete；失败保留错误并删除未发布collection。该事实用于观察和故障定位，不在本项实现跨进程resume或leader协调。
- 真实PostgreSQL执行项目批量投影，验证本地化、标识、创作者、标签和兼容性结果。轻量主键表在100k/1M规模逐页完整遍历，分别5.953s/58.527s，任一live ID slice不超过500；半深页EXPLAIN均为主键Index/Index Only Scan且无Seq Scan。20个约1MiB文档被拆为至少3个请求，任一请求不超过8MiB。
- PERF-037按High/P0关闭。BUG-058仍负责多实例重建期间并发增量不丢失，OPS-011仍负责滚动发布Schema版本协调，TEST-030仍负责完整搜索状态机/故障矩阵，MAP-008仍负责搜索注册表集中化；本项的内存、查询与进度修复不冒充这些独立Finding。开发数据库需整代重置到122，远端public generation85仅用于受保护临时表测试且未迁移或永久写入；回滚到121会恢复collection级堆物化，不保留旧全量开关。

## D-148：死信运维使用状态/聚合绑定的time-ID游标并保留单条事务重放

- 旧GET固定按`failed_at desc,id desc limit 100`返回`{items}`，不接受游标或筛选。第101条以后的未解决死信从API永久不可达，也无法从管理面选择重放；仅把limit从50调到100仍然是随历史增长而失效的固定窗口。
- 新请求只接受单值`status/aggregateType/aggregateId/limit/cursor`。status默认`unresolved`，允许`unresolved|replayed|all`；默认50、最大100。aggregate ID必须与type共同出现，type/ID分别限制100/200字节；未知、重复、page/offset、非法status/limit、空白边界及跨任意筛选或limit游标均400。
- opaque base64url cursor携带version、UTC canonical `failed_at`、正ID及status/aggregate/limit的SHA-256 scope。查询按`(failed_at,id)<cursor`排他续页并读取limit+1；响应为`items/limit/hasMore/nextCursor`，同一失败时间由ID稳定打破平局，无COUNT或OFFSET。
- generation123为`dead_letter_events`新增非空`aggregate_type/aggregate_id`。publish死信从被移动的Outbox行保留aggregate身份，consumer死信从已解码EventEnvelope保留；无法解码的既有失败仍用空字符串显式表示unknown。全局、unresolved、replayed及aggregate页面分别有time/ID复合索引。
- Next16 NATS运维面新增死信区域：状态及aggregate筛选、50项页、服务端cursor历史Previous/Next、错误显示和unresolved单项重放。筛选提交会清空cursor历史，请求由AbortController取消；重放成功刷新当前scope，不由客户端猜测状态。
- POST `/{id}/replay`完全保持原事务语义：先`FOR UPDATE`锁死信，拒绝已重放；publish恢复原Outbox，consumer严格验证envelope并创建新Outbox；随后同事务标记replayed并写actor/original/new event审计，任一步失败整体回滚。分页修复没有批量重放或绕过原锁。
- 真实PG用150条同时间戳目标死信加其他aggregate/replayed噪声验证两页100+50完整无重漏。exact生产SQL在100k/1M对unresolved、replayed和aggregate深页全部命中索引且零Seq Scan；百万行计划墙钟27.289–32.269ms。既有publish/consumer重放测试和Outbox持久状态测试继续通过。
- PERF-038按Medium/P1关闭。ARCH-015仍负责基础设施COUNT查询失败伪装零健康，TEST-032仍负责压缩、资源版本、metrics故障和重放竞态的完整矩阵；本项只关闭死信历史可达性和对应索引/调用方。开发数据库需整代重置到123，远端public generation85未迁移或永久写入；回滚需协调恢复122和旧管理面，会重新产生100条固定窗口，不保留offset或双响应协议。

## D-149：MRPack加载器元数据使用有界双层缓存、同键合并与条件校验

- 旧预检和创建都会重新调用`buildFavoriteModpackExportPreview`，其中具体loader版本选择每次直接下载Fabric按Minecraft版本的JSON或Forge/NeoForge完整Maven metadata，单响应允许16MiB且超时30秒。相同用户流程顺序产生2次下载/完整解析，32个并发预检产生32次相同外呼；没有进程共享TTL、条件请求或并发预算。
- 新的具体版本选择先把loader标准化为小写并与精确Minecraft版本组成key。成功结果缓存15分钟、最多256项；相同key未命中时由singleflight只执行一次下载和解析，等待者可独立按自己的context取消，而共享请求使用HTTP客户端既有30秒硬超时完成，不由首个断开的等待者使其他请求失败。
- 统一`fetchMinecraftSource`另有URL级15分钟原始响应缓存。新鲜响应直接复用；过期响应保存的ETag和Last-Modified分别发为`If-None-Match`/`If-Modified-Since`，304只延长新鲜期而不重复传输正文。非200、网络错误、读取错误和超16MiB响应都不写缓存，下一次调用可真实重试。
- 原始缓存最多64 URL且按LRU淘汰，总payload最多64MiB；任一响应继续最多16MiB。不同URL即使并发到达，也必须取得全局4槽外呼预算；同URL在槽位前已经合并。因此内存、远端带宽、连接和等待goroutine的增长都有独立硬边界，而不是只靠UI防抖。
- 预检/创建HTTP请求、响应、错误码、前端两阶段调用顺序、任务快照和Schema均不变，generation保持123。缓存是单进程性能层，不宣称预检已冻结：TTL边界或跨实例仍可能选择不同版本；也不替代版本同步的持久权威数据或跨实例租约。
- RED记录顺序解析实际2次远端请求、32并发实际32次；GREEN两者都精确为1。补充测试验证200携带validator后过期请求发送两个条件头、304复用原payload，503不缓存，12个不同URL的实际最大并发为4，64项/64MiB双预算不越界；定向测试10次shuffle及HTTP包、全仓Test/Vet/Build/tidy均通过。
- PERF-039按Medium/P1关闭。BUG-065仍负责不可变预检确认，ARCH-017仍负责站内持久loader artifact快照，OPS-012仍负责跨实例同步所有权，MAP-009仍负责NeoForge多来源血缘，TEST-033仍负责完整HTTP/数据库/调度/多实例矩阵；本决策不以进程缓存冒充这些事实或测试已存在。

## D-150：MRPack依赖先一次装载有界图，再以固定并发解析候选且required失败关闭

- 旧`appendFavoriteExportDependencies`以BFS逐route执行关系SQL；每个新依赖又分别查询Modrinth映射、系统配置和provider文件。数据库与远端等待都按节点串行相加，循环还写死`cursor<100`，Query/Scan/rows错误全部`continue`，于是性能上限同时成为不可见的包完整性破坏。
- 新`favoriteExportDependencyGraphSQL`从全部成功根route开始，以recursive UNION按route ID去重可达集合，因此环不会重复扩张；单条SQL返回节点快照、Modrinth项目ID和按display order/relationship ID稳定的边。节点读取limit 1001、边读取limit 4001，Go只接受最多1000节点/4000边；任一Query、Scan、rows.Err或未知/缺失图事实直接返回错误。
- 初始收藏项SQL也左联接Modrinth来源，不再每项查询映射；provider配置对整个preview只读取一次。根候选及每层新依赖都交给固定最多8 worker解析，结果仍按候选发现顺序写回。不同项目远端请求可并发但仍复用共享provider snapshot cache及单项目最多500文件的既有预算，不创建无界goroutine。
- 图上限不是截断：`errFavoriteExportDependencyLimit`由预检/创建稳定映射为422 `MODPACK_EXPORT_DEPENDENCY_LIMIT`。缺来源、无兼容版本/loader/file或provider故障仍生成`REQUIRED_DEPENDENCY_UNRESOLVED`逐项结果供预检解释；create在配额/任务写入前检测该reason并返回422 `MODPACK_EXPORT_REQUIRED_DEPENDENCY_UNRESOLVED`，compatible-only确认不能把必需依赖从包中静默删掉。
- 150节点闭环在真实完整generation123中由1条图SQL完整返回；加入根到其余149节点扇出后，完整preview得到1根+149个auto dependency、只有1条图SQL且provider并发落在2..8。1001节点链明确命中上限，没有第100节点后的静默遗漏。
- exact生产SQL在1M route/mod/relationship噪声与1000个可达节点下，递归及边装配都命中route PK/subject和`mod_id`索引；EXPLAIN执行21.665ms，完整Go扫描约150.9997ms且无`public_routes/mod_relationships` Seq Scan。该规模工作是单数据库往返，不再把网络时延乘以节点数。
- BUG-066与PERF-040均按High/P0关闭。BUG-069仍负责Worker结果行Scan/rows错误与ready一致性，BUG-070仍负责文件路径冲突，SEC-024仍负责并发创建配额，TEST-034仍负责从Handler到NATS/OSS/过期下载的完整状态机；本决策的preview图测试不冒充这些链路已经覆盖。

## D-151：MRPack导出历史按owner/status绑定的time-ID游标完整可达

- 旧GET只执行`where owner_user_id=$1 order by created_at desc,id desc limit 100`并返回`{items}`。没有status、cursor或下一页事实，第101条及更早任务永久不可达；仅增大固定limit仍会随历史增长重新失效。
- 新请求只接受单值`status/limit/cursor`；status默认`all`并允许任务表完整六终态，limit默认30、最大100。未知、重复、page/offset、非规范整数、非法状态、超长/畸形/未知字段cursor都返回400，不产生宽查询兼容旁路。
- opaque base64url cursor携带v1、规范UTC `createdAt`、正内部ID和owner/status/limit的SHA-256 scope。查询以`(task.created_at,task.id)<anchor`排他续页，稳定降序读取limit+1；只有确有额外行才返回`hasMore=true/nextCursor`，内部ID不进入item DTO。
- generation124在既有`(owner_user_id,created_at desc,id desc)`旁新增`(owner_user_id,status,created_at desc,id desc)`。all页和过滤页分别有正确leading列；不执行COUNT、OFFSET或查询期聚合。开发数据库整代重置，不在线回填或保留旧双协议，远端public generation85未迁移或写入。
- Next16 API loader消费`items/limit/hasMore/nextCursor`并发出精确status/cursor；历史面板提供全部/六状态筛选和加载更早记录，续页按task public ID去重。状态改变重新读取首屏并替换旧集合，不能把不同scope的cursor拼接到新筛选。
- 完整generation124中150个同owner、同`created_at`任务按100+50遍历且无重复/遗漏/跨owner泄漏；ready筛选按17条页面遍历精确50条。1M合成任务上all与ready exact SQL分别命中owner-created和owner-status-created索引，数据库执行0.096/0.141ms、调用墙钟127/162ms且任务表无Seq Scan。
- PERF-041按Medium/P1关闭。列表handler新增`rows.Err()`可见性，但ARCH-018还覆盖详情JSON/rows及Worker错误，不能一并关闭；BUG-069的Worker ready一致性、DEAD-011未消费report snapshot和TEST-034完整Handler/NATS/OSS/过期下载状态机也继续独立OPEN。

## D-152：蓝图处理在接收、入队、规范化和持久化各层共享硬预算

- 旧合同允许512MiB源文件、256MiB解码NBT、16,777,216体积和8,388,608非空气块。合法文档在解码后还会同时持有block/material/render映射、排序切片与完整JSON，并逐材质执行INSERT；多个Worker/用户并发会把堆、CPU、GC和数据库往返近似线性放大。
- 新合同把源文件与解码NBT都限制为32MiB，结构体积限制2,097,152、非空气块250,000、不同材质8,192，规范JSON写入限制64MiB。限额由presign、既有文件关联、Worker下载、各codec和规范编码器共同执行；不是只依赖客户端声明或OSS metadata。
- 规范JSON不再对整个文档一次`json.Marshal`，而是按字段和数组元素写入有界buffer，越过64MiB即失败；保持原schemaVersion/size/palette/blocks/entities/metadata JSON结构和字段语义。材质聚合在第8,193种状态前明确失败，不默默截断。
- 进程内所有normalize/convert执行在claim前取得共享2槽信号量，进程总CPU/堆并发有硬上限。跨实例的用户入队由transaction advisory lock串行，并用最多读取4行的active-count查询限制每用户queued/processing合计4个；上传、转换和重试复用同一事务helper。
- generation125新增部分索引`idx_blueprint_jobs_creator_active(created_by,id) where status in ('queued','processing') and created_by is not null`。admission查询只读最多4个ID；100k与1M任务均命中Index Only Scan，数据库执行约0.047/0.090ms，不随用户历史线性扫描。开发数据库整代重置，远端public generation85未迁移或写入。
- `blueprint_materials`先在事务内删除旧集合，再以一次`CopyFrom`批量写最多8,192行；任一编码/复制/计数不一致错误使事务失败。完整generation125中8,192行写入约273ms且计数精确，不保留逐行INSERT兼容路径。
- 最大合法250,000块、500×1×500文档完成材质、14,390,116字节规范JSON和封面渲染，总分配109,113,520字节、347.8268ms，低于512MiB/10秒门槛。PERF-042按High/P0关闭；SEC-026的恶意palette、BUG-076的转换格式正确性和TEST-036的完整蓝图矩阵仍分别开放，材料revision查询规模由PERF-043独立处理。

## D-153：蓝图材料目录装饰只解析当前文档使用的namespace

- 旧`blueprintMaterialRows`先读目标蓝图材料，随后却对全部active ready/partial `catalog_import_revisions`执行未过滤的`distinct on(source_namespace)`，把全站每个来源的最新revision装入map，最终只使用材料block ID前缀对应的少量项。匿名公开详情成本因此随全站目录增长。
- 新路径在材料读取完成后从block ID提取namespace，以set保持首次出现顺序并去重；材料总数已经受PERF-042的8,192硬限约束，空材料不执行revision查询。请求数组只包含当前蓝图实际使用的namespace，重复方块或同namespace不同方块不会增加目录工作。
- 最新revision SQL固定为`source_namespace=any($1::text[]) and is_active and status in ('ready','partial')`，按namespace、`coalesce(activated_at,created_at) desc`、`id desc`执行`distinct on`。最后的ID决胜让相同时间事实仍确定，不回退到全局查询或客户端过滤。
- generation126新增匹配部分表达式索引`idx_catalog_import_revisions_material_namespace(source_namespace,coalesce(activated_at,created_at) desc,id desc) where is_active and status in ('ready','partial')`。开发数据库整代重置，无在线回填或双查询，远端public generation85未迁移或写入。
- 100k及1M active revision夹具中，exact生产SQL只返回`used_alpha/used_beta`各自确定性最新行；两档都命中该Index Scan，数据库执行0.049/0.043ms，revision表零Seq Scan，查询成本不再与无关namespace数量线性相关。
- 蓝图详情API/DTO、locale fallback及既有批量`resolveExportResources`调用不变，无前端协调部署。PERF-043按High/P0关闭；revision Query/Scan/rows和resolver错误仍被降级的问题属于ARCH-019，保持OPEN并将在其自身故障注入下处理。

## D-154：用户OSS额度使用写时事实和对象级预留，不在请求内重扫历史

- 旧预签名分别SUM用户当日active source大小和全部active stored大小，完成阶段又SUM一次stored；查询错误还被忽略为零。单用户十万/百万文件时每次上传重复读取大量索引/heap，并发请求都可基于相同旧总量通过。
- generation127新增`oss_user_quota_usage`总量桶、`oss_user_daily_quota_usage`日期桶和`oss_user_upload_quota_reservations`。总量/每日分别保存active source/stored及reserved source/stored，全部非负；预留以`(user_id,object_key)`唯一并保存usage date、两种字节、expiry。
- 预签名先验证single权限，再在用户transaction advisory lock内清理已过期预留、替换同对象幂等预留、读取两个单行桶并以减法checked arithmetic判断daily/total。每用户活跃预留最多64；普通文件预留source+stored，待WebP/render转换只预留source并在完成时按实际stored复核。
- 签名、multipart初始化或会话登记失败释放预留；multipart abort成功和重复完成也释放。完成在同一事务锁用户、移除自身预留、按当前日期及实际source/stored再次判断，并用该事务INSERT active `oss_files`；文件trigger随后在同一事务把active delta加入桶，因此两个600字节请求对1000额度只能一成一拒。
- `trg_oss_files_quota_usage`覆盖insert/update/delete；active行的uploader/status/size/source size/created date变化先减旧事实再加新事实，其他scan等更新即时返回。任一underflow使来源事务失败，不以负数或静默修正掩盖漂移。
- `rebuild_oss_user_quota_usage(user)`和`rebuild_oss_user_daily_quota_usage(user,date)`只供写入静止的离线维护窗口，从权威active文件SUM重建active字段并保留未过期reserved事实；常规上传和额度API绝不调用。开发库整代重置，无旧generation在线回填或双读，远端public generation85未迁移或写入。
- 真实双连接600+600/1000竞态在1.4108s内精确一个成功；顺序600+400可精确占满，实际550 stored结算、改500、删除归零及故意把active改999后rebuild到250/200均正确。1M单列历史陷阱存在时额度API/准入四个桶读取235.1216ms且不访问历史。
- 路由、请求、成功DTO、既有中文额度错误和前端不变；额度页used值改读活动桶，不把reserved伪装成已用。PERF-044按High/P0关闭；ARCH-020仍负责文件目录、哈希复用、统计/审计及其他OSS错误可见性，不能随本项一并关闭。

## D-155：评论三类列表使用scope绑定keyset，总数由写时目标事实提供

- 旧根评论、直接回复和我的插眼虽然把参数命名为`cursor`，实际都解析成整数OFFSET；根评论还在每次请求对目标全部published/deleted评论执行精确COUNT。深页随页码重复排序并丢弃前缀，并发新评论会让相邻页漂移。
- 新cursor是严格base64url JSON v1。根游标绑定目标内部身份、版本、viewer及latest/oldest/hot/replies排序，并保存pinned边界、对应排序键、created time和ID；回复游标绑定parent/viewer，watch游标绑定user/filter/sort。空值、未知字段、超长、非有限hot score、非正ID及任何跨scope复用都400。
- 根排序继续保持置顶优先。游标仍在置顶集合时，查询读取剩余置顶或转入未置顶；进入未置顶后固定`pinned_at is null`并对排序tuple直接比较，使百万深页能从复合索引锚点开始。oldest使用升序time-ID，其他排序使用降序tuple；回复升序time-ID，watch按activity/created/unread对应tuple降序。
- generation128新增四个根可见部分复合索引、直接回复可见time-ID索引及watch created/unread索引。所有列表只读limit+1；`nextCursor`从截断后最后一条查询行生成，不从树装配后的响应顺序猜测。第一方前端一直把cursor视为opaque字符串，因此调用代码不变；旧十进制offset不保留兼容入口。
- `comment_target_counts`按目标维护全部可见评论总数，`comment_target_author_counts`按目标/作者维护子事实。comments insert、status/目标/作者变化和delete在来源事务内原子减旧加新，零行按精确键删除；匿名总数单行读取，登录用户再从author事实扣除其拉黑作者，因此保持既有可见总数语义而不重扫评论历史。`rebuild_comment_target_counts`只供写入静止的离线校准。
- 完整临时generation128验证insert=1、hidden删除事实、deleted恢复和故意99漂移后rebuild=1。1M根+100k回复+2k watch中，四类根深页、回复深页和watch深页均无Seq Scan，EXPLAIN墙钟57.577–59.2696ms；拉黑精确总数事实返回990,000，57.336ms。首屏后插入更新评论，锚定下一页没有重复或新项漂入。
- PERF-045按High/P0关闭。PERF-046仍负责watch页把评论装配和目标权限/详情改成批量集合读取；ARCH-022仍覆盖其目标解析错误被降级等剩余故障语义。回滚需协调恢复generation127和旧整数offset/COUNT协议，会重新引入深页线性成本，不提供双读。

## D-156：我的插眼以评论集合和目标identity集合一次装配

- 旧watch页先读最多100行，随后在Go循环中每项调用完整`queryCommentItems`和`resolveCommentTargetByInternal`。前者每次重新查询评论/反应/watch、在线态、OSS配置/头像、权限/角色与附件，后者又执行内部ID到公开key及类型详情/可见性两级读取；一页因此放大到约数百数据库/签名操作。
- 新路径截断limit+1后一次收集全部comment ID，单次`queryCommentItems`完成评论主体、反应、当前watch、作者、parent preview、在线态、权限上下文/作者根权限和附件集合装配。结果以内部comment ID建map并按原watch排序回装；被拉黑或非可见评论仍按既有产品规则过滤。
- 目标以`(type,internal_id,version_key)`identity去重。一个requested CTE通过固定、仅按当前页实际类型启用的UNION分支批量解析Mod、Modpack、六类simple project、蓝图、皮肤、作者/团队、社区帖、小黑屋、玩家、标签、配方类型及Mod资源共17类的key/title/URL和原可见性规则。任意数据库Query/Scan/rows错误使整页500；明确不可见或未知类型是缺失map事实，继续返回原有只含type的fallback，不以错误控制流判断权限。
- `queryCommentItems`只读取一次OSS设置；头像访问URL按原stored URL做请求内唯一对象缓存，相同作者/对象只解析或签名一次。不同头像仍受100项页上限约束，不建立跨用户持久URL缓存或改变私有URL TTL。
- 真实临时表中100个不同approved Mod目标由一条SQL在61.8907ms解析，重复identity不增加结果；pending目标对匿名缺失、提交者可见。完整generation128逐一和混合编译全部17类分支，并创建100个不同目标/watch后调用真实handler：limit=1和limit=100都精确12条数据库语句，100项全部有comment ID和目标key。
- Schema generation保持128，路由、请求、响应DTO、cursor、目标不可见fallback及前端均不变。PERF-046按High/P0关闭；ARCH-022仍保留为专用中途断连/Schema漂移故障注入验收，TEST-038仍负责完整评论权限和生命周期矩阵。回滚只会恢复逐项N+1，没有兼容价值，不保留开关。

## D-157：玩家档案列表以public ID集合一次装配纹理

- 旧`loadPlayerProfiles`先读取用户全部active档案，再在Go循环中对每项调用`loadPlayerTextures`。我的档案与公开用户档案最多返回100项，因此一个公开请求会从档案主查询放大到101条数据库语句；每次纹理查询还重复执行相同的档案、asset owner和viewer wardrobe关联。
- 新`loadPlayerTexturesForProfiles`先把非空档案public ID去重并建立response指针map，再以一个`profile.public_id=any($1::text[])`查询读取全部active skin/cape和viewer wardrobe状态，按profile ID及kind稳定回装。空集合不发纹理SQL，Query/Scan/rows任一错误继续让整个档案读取失败。
- 单档案详情的`loadPlayerTextures`只作为size-1委托进入相同batch helper，因此CanEdit、CanUse、review/visibility、texture URL及skin/cape选择只有一个实现；没有为了列表性能建立第二套DTO或权限逻辑。
- 真实PostgreSQL临时权威表中，1个档案与100个档案的生产`loadPlayerProfiles`都精确执行2条SQL。100档案的200个纹理全部装配，viewer衣柜中的skin为true、cape为false；结果数量不影响数据库往返。
- Schema generation保持128。现有`player_profiles(public_id)`唯一索引、`player_profile_textures(profile_id,kind)`主键和`skin_wardrobe(user_id,asset_id)`主键已经匹配集合连接，无DDL、回填或远端public变化。路由、请求、排序、可见性与响应字段均不变。
- PERF-047按High/P0关闭：匿名可访问的用户页可把单请求放大到101次数据库往返。回滚只会恢复N+1且没有协议收益，不保留逐项兼容路径；其他皮肤目录、上传和Yggdrasil Finding仍独立开放。

## D-158：公共皮肤目录使用写时全文投影和scope绑定稳定游标

- 旧GET `/api/v1/skins`在宽`skin_assets`上对名称、描述和查询期`array_to_string(tags)`执行前导通配符`ILIKE`，联接热度后按多类事实排序并应用OFFSET；随后对相同筛选再执行同步COUNT。百万级匿名目录的搜索、深页和总数会重复全量扫描/排序，已有tags GIN不能服务字符串化包含谓词。
- generation129新增窄`skin_public_catalog`，保存asset/public/kind/model/name/time/downloads以及heat/favorites/bayesian rating/rating count/views/comments和stored simple `tsvector`。asset、public route与popularity来源trigger在同一事务调用`refresh_skin_public_catalog(asset_id)`；不再公开的asset会删除投影。`rebuild_skin_public_catalog()`只供写入静止的离线重建。
- 投影有search GIN，以及published、updated、heat、downloads、favorites、rating、views、comments、lower(name)九类包含ID决胜的B-tree。查询使用`search_document @@ websearch_to_tsquery('simple',q)`、与排序方向一致的排他keyset和limit+1；不再出现OFFSET、COUNT、ILIKE或查询期数组转字符串。规划器可按选择性选择GIN或匹配排序索引，两者都只访问窄投影。
- 请求只接受单值`q/kind/model/limit/sort/order/cursor`，limit默认24、最大100；拒绝未知/重复、page/offset、非法枚举、超过80字符或320字节查询。base64url JSON v1 cursor严格绑定规范化q/kind/model/limit/sort/order scope，并保存所选稳定排序tuple与正asset ID；未知字段、超长、畸形或跨scope复用均400。
- 投影页关闭后，handler用本页asset ID一次`ANY(bigint[])`读取完整详情并重新要求active/approved/public，防止投影读取与详情读取之间的可见性变化泄漏。页行与详情的Query/Scan/rows错误都在成功响应前500；响应顺序由投影保持，数据库语句恒为页1+详情1。
- 成功DTO从`items,total,limit,offset`协调替换为`items,limit,hasMore,nextCursor`，旧offset明确400且不保留双协议。Next16皮肤库保存cursor历史实现Previous/Next，并在搜索、kind/model、排序或重置时清空历史；显示当前页范围而不伪造全站精确总数。
- 真实PostgreSQL 1M投影的published深页、views深页、kind/model过滤页和选择性FTS exact生产SQL计划约61.9935/270.0025/61.0444/355.2036ms，全部无投影Seq Scan或基础宽表排序。FTS计划在该数据分布选择published排序索引并应用GIN-capable谓词，GIN索引仍由Schema合同保证。并发插入新首项后已锚定次页零重复、零新项漂入；完整handler两页均精确2条SQL。
- PERF-048按High/P0关闭：匿名请求原可同时放大不可索引包含搜索、深页排序和重复COUNT。开发数据库需整代重置到129，远端public generation85未迁移或写入；回滚需协调恢复generation128、旧后端和旧前端，会重新引入线性成本，不提供offset/total兼容入口。

## D-159：用户衣柜的5000存量预算与单请求预算分离

- 旧GET `/api/v1/users/me/skin-wardrobe`固定读取`maximumSkinWardrobeItems=5000`并返回全部完整`skinAssetJSON`。每项都携带description、tags、owner、blob尺寸/哈希与权限事实；Next16随后把整个数组同时放进纹理select和卡片grid，因此数据库读取、约数MiB响应、React状态及DOM都随用户存量增长到5000。
- 新请求只接受单值`kind/model/limit/cursor`；默认50、最大100。kind允许skin/cape，model允许default/slim；未知、重复、page/offset、非法枚举/limit及畸形或跨用户/筛选/limit cursor均400，不产生固定窗口兼容旁路。
- base64url JSON v1 cursor绑定user ID、kind、model与limit，保存UTC added time和正asset ID。查询顺序刻意匹配既有`idx_skin_wardrobe_user_added(user_id,added_at desc,asset_id)`的混合方向：`added_at desc,asset_id asc`；续页谓词为更早时间或同时间更大asset ID，读取limit+1且不需要Sort。
- 衣柜联接本身已经证明每个结果属于当前用户，因此页select把`InWardrobe`直接置true，不再为最多100行触发可被规划为全用户集合扫描的冗余correlated exists。可见性仍保持owner可看自己的active资源，非owner仅approved public/unlisted；kind/model total使用相同条件但不联接owner/blob宽表，任一页或count读取错误都在成功响应前500。
- 成功DTO从固定`items,total,limit=5000`协调迁移为`items,total,limit,hasMore,nextCursor`。无参调用现在只返回首50项；第一方调用显式取100项并提供“加载更多”，按public ID去重追加。当前玩家档案已装备但位于后页的skin/cape由档案响应固定插入select options，分页不会使现有选择显示为空或在保存时被意外清除。
- 真实PostgreSQL创建5000个完整资源/衣柜事实。首两页各100项、精确2条SQL、各71,318字节且零重漏；首屏后插入更新项不会进入已锚定次页。kind=skin/model=slim的同筛选total精确833；半深exact生产SQL命中既有wardrobe复合索引，无wardrobe Seq Scan或Sort，计划墙钟62.6448ms。
- PERF-049按High/P0关闭。Schema generation保持129，无DDL/回填/开发库重置或远端public变化；5000仍是写入存量上限而不再是读取页大小。回滚需同时恢复旧后端DTO和前端全量consumer，会重新引入单请求/DOM放大，不保留双协议。

## D-160：评分明细遍历不承担同步精确总数

- 旧GET `/api/v1/ratings/{targetType}/{publicId}/reviews`在每次明细请求中先对目标全部published评分执行精确COUNT，再按`updated_at desc,id desc`排序并用OFFSET丢弃前缀。热门项目达到百万评分后，即使只返回20条，深页也会重复聚合并扫描/丢弃大量行；评分更新还会让offset页发生重复或遗漏。
- 新请求只接受单值`limit/cursor`，默认20、最大100；未知、重复、page/offset、非法limit以及畸形、未知字段或跨目标/页大小cursor均400。base64url JSON v1 cursor绑定route ID、规范目标类型、public ID和limit，并保存UTC updated time与正rating ID。
- 页SQL严格使用`(rating.updated_at,rating.id)<(cursor.updatedAt,cursor.id)`、相同降序和limit+1。它精确复用既有`idx_content_ratings_target(object_route_id,status,updated_at desc,id desc)`，不新增为本Finding定制的重复索引；读取页rows显式关闭后才批量加载维度，单连接也没有悬挂结果集或隐式额外连接需求。
- 明细成功DTO协调替换为`items,limit,hasMore,nextCursor`，删除`total,offset`且不保留双协议。评分面板原本已独立读取`ratingSummaryResponse.ratingCount`；该值来自`content_popularity_stats`写时维护/持久队列刷新投影，继续服务“查看评价（数量）”和评分概览，不把明细总数伪造为页累计值。
- Next16评价modal保存服务器cursor历史，Previous弹出一个cursor，Next只压入服务器返回的nextCursor；任意时刻只保存和渲染当前20项页。页面显示“第N页”而不制造总页数，目标切换/关闭由modal生命周期清空历史。
- 真实PostgreSQL影子表装载一百万published评分。真实handler首/次页各20项且每次精确5条SQL（目标2、页1、维度1、OSS设置1），响应低于128 KiB；首屏后插入更新更晚的评分不会进入已锚定次页，两页零重复。半深exact生产SQL计划墙钟60.6126ms，命中既有目标索引且无`content_ratings` Seq Scan或Sort。
- PERF-050按权威增量表的Medium/P0定级关闭。Schema generation保持129，无DDL、回填、开发库重置或远端public变化；回滚需同时恢复旧后端DTO和前端offset状态，会重新引入同步COUNT/深OFFSET，不提供兼容入口。LEGACY-017的目标类型别名和TEST-040其余评分行为覆盖保持独立OPEN。

## D-161：收藏目录分页与membership检查分离

- 旧私有/公开收藏夹与收藏项GET没有limit或cursor；登录Mod/Modpack目录先下载用户全部收藏夹，再对每夹并发下载完整items，只为判断当前目录页的收藏状态。公开用户页与选择器也会把全部集合/项装入响应和DOM，收藏夹DTO还为每夹同步聚合精确itemCount。
- 四类GET统一为严格base64url JSON v1 cursor。收藏夹按`is_default desc,created_at asc,id asc`，收藏项按`created_at desc,id desc`读取limit+1；默认20、最大100，cursor绑定owner、私有/公开scope、viewer/moderator、collection及limit，未知/重复/page/offset/跨scope输入400。收藏项先以collection public ID标量子查询确定内部ID，使页复合索引成为直接条件而不依赖联接顺序。
- generation130新增`idx_favorite_collections_user_page`、公开partial对应索引、`idx_favorite_items_collection_page`和`idx_favorite_items_target_collection`。列表不再同步COUNT，收藏夹DTO删除itemCount；所有历史仍可由cursor到达，项目内容可在所选收藏夹的项页继续遍历。
- 新POST `/api/v1/users/me/favorites/summary`只接受一个合法target type、1..100个public ID与可选collection ID；去重后target×collection最大100。目录只发送当前结果页public ID。无collection过滤时，每个目标通过target反向索引的LATERAL limit1判定存在性；选择器只为当前20个收藏夹返回目标/集合矩阵，均为单SQL且不读取名称、图标或其他元数据。
- 选择器跨页保存`collectionId→selected`触碰map，并通过新PATCH delta一次提交最多100个互斥add/remove ID。后端Repeatable Read事务先解析当前可见目标、锁定并完整验证所有集合，再批量delete/insert，返回全局`selected`；不会用当前页替换全部membership而清除未加载页。旧PUT留给BUG-089/LEGACY-018独立收口，第一方已无调用；旧无界GET及handler已删除。
- Next16账户页、公开资料页和选择器保存服务器cursor历史，每次只显示一个集合/项页；Mod/Modpack目录只发当前`backendMods`，蓝图详情只发当前public ID。页切换先清除旧items/page状态；创建集合刷新当前有界页或只记录delta，不把新项无限追加到DOM。
- 真实PostgreSQL使用一百万收藏夹和一个收藏夹内一百万个不同可见Mod收藏项。收藏夹与项首/次页各20项、分别固定2 SQL，100目标summary固定1 SQL；并发插入排序更早/更新的事实不漂入已锚定次页且两页无重复。半深收藏夹、半深项和100目标summary计划约163.7303/177.2103/60.4907ms，分别命中新索引且无目标表Seq Scan，响应均低于预算。
- PERF-051按High/P0关闭。generation130完整临时Schema安装47.22s并在清理后确认远端public generation85不变；未对远端执行重置或永久写入。回滚必须同时恢复generation129、四类旧DTO和全部第一方调用，会重新引入无界响应与1+N，不保留无界GET兼容入口。SEC-036、BUG-089、ARCH-023/024、LEGACY-018、STYLE-006与TEST-041仍独立OPEN。

## D-162：社区目录只遍历无正文的写时搜索投影

- 旧GET `/api/v1/community/posts`把单篇最多1MiB的`body_markdown`放入limit最高100的列表行；数据库回退对标题和正文执行前导通配符`ILIKE`，随后以相同条件再次COUNT并按整数OFFSET丢弃前缀。热门深页会把正文传输、包含扫描、排序和重复聚合叠加在一个匿名请求中。
- generation131新增`community_post_catalog`。每行保存公开ID、类型/分类/作者、标题、最多320字符的空白折叠`body_summary`、版本/问题/审核/时间事实、热度/下载/收藏/评分/浏览/评论指标和simple stored `tsvector`；不保存`body_markdown`。post、public route和popularity变化在来源事务内调用单项refresh，离线初装显式重建active帖子。
- 投影新增search与版本数组GIN，以及published、updated、heat、downloads、favorites、rating、views、comments和lower(title)稳定B-tree；全部排序tuple以updated time和/或ID确定决胜。匿名路径生成精确`review_status='approved'`谓词，使published深页可直接从对应复合索引锚点读取；作者仍可见自己的待审内容，moderator仍可见全部状态。
- 请求只接受单值`kind/q/category/version/versionMode/project/sort/order/modId/resourceId/limit/cursor`，默认24、最大100。base64url JSON v1 cursor绑定全部筛选、viewer、moderator、limit、排序和方向；保存对应排序tuple。未知/重复、page/offset、非法枚举/列表、畸形/未知字段或跨scope cursor均400。
- 生产SQL只从投影读取摘要和当前页事实，全文条件固定为`search_document @@ websearch_to_tsquery('simple',q)`，稳定排他keyset后取limit+1；无正文、ILIKE、COUNT或OFFSET。成功DTO协调迁移为`items,limit,hasMore,nextCursor,categories`；列表项用`summary`替代`bodyMarkdown`，详情GET继续按需读取完整正文，不保留双列表协议。
- Next16四类目录保存服务器cursor历史并显示当前页项数/页号；筛选、搜索或排序变化清空历史。首页与相关内容只取首个有界summary页；卡片和首页摘要不再要求完整正文。所有第一方`loadCommunityPosts`调用已移除offset和完整`CommunityPost`类型假设。
- 真实PostgreSQL装载一百万条窄投影，并给首屏24条权威帖子放入1MiB sentinel正文。真实handler每页固定5条SQL、响应低于64KiB且不包含`bodyMarkdown`或sentinel；并发插入更新的首项不漂入已锚定次页，两页零重复。半深published与选择性FTS exact生产SQL分别约72.7062/74.0124ms，命中published B-tree/search GIN且投影无Seq Scan。
- PERF-052按High/P0关闭。generation131完整临时Schema安装43.08s并确认清理后远端public generation85不变；未执行远端重置或永久写入。回滚必须同时恢复generation130、旧列表DTO与全部前端调用方，会重新引入单页近100MiB、包含扫描、同步COUNT和深OFFSET，不提供兼容开关。PERF-053、ARCH-025、TEST-042、SEC-038及BUG-090/091仍独立OPEN。

## D-163：社区引用预算在事务前确定，解析与替换只按集合往返

- 旧POST/PUT只受通用8MiB JSON上限约束。`Projects/Resources`中的标识和值没有数量/长度/重复边界；解析对每个project/resource/kind逐项SELECT，替换又逐项解析、INSERT并为每个未解析引用单独UPSERT。一个认证请求可把一个Repeatable Read事务放大为数千串行往返和trigger/锁工作。
- 新领域边界为projects最多32、resources最多64。project raw identifier复用既有1..128字节Mod ID语法；resource raw identifier为1..256字节有效UTF-8且不含空白/control；resource kind最多64字节；resolved public ID严格为9位小写字母数字。身份在大小写折叠后重复即稳定400，原始数组先检查数量，因此重复洪泛不能绕过预算。
- 客户端可提交的权威字段只有project type/public ID/raw identifier及resource kind/public ID/raw identifier。名称、站点、locale names、图标、版本/revision路径、unresolved/unavailable等展示事实在事务前清空，不进入revision或持久关系；读DTO仍由服务端权威表重新装配这些字段。
- project public ID集合一次进入既有`loadFollowProjectTargetsWithQueryer`，逐输入核对map存在与目标类型，保持SEC-037 owner/approved/moderator动态可见性。resource public ID一次`ANY(text[])`解析内部ID与权威kind；所有unresolved kind一次`ANY(text[])`验证。最大集合解析固定3条SQL，空子集合不发对应语句。
- 两张旧引用集合各执行一次post范围DELETE及一次并行数组`unnest` INSERT returning ID/order；返回数必须等于输入数。未解析project/resource分别把返回ID数组一次`unnest` UPSERT到`unresolved_references`。常规已解析/未解析混合集合替换固定6条SQL；审核批准从immutable revision恢复的resolved引用缺少非JSON内部ID时，先走相同批量重解析，不回退逐项路径。
- 数量/身份错误在create/update开始事务前返回400，错误文案稳定包含32/64上限。Next16共享导出相同常量，submit和两个picker确认边界都阻止超限并显示双语提示；后端仍是最终权威，成功路由、snapshot与响应DTO不变。
- 真实PostgreSQL分别执行1 resolved+1 unresolved和16+16 project、32+32 resource混合集合；两种规模从解析到替换均精确9条SQL。最大集合写入32 project、64 resource和48 unresolved事实。既有动态可见性集成再次证明他人pending目标拒绝、owner可引用、viewer读取隐藏目标只得零元数据占位。
- PERF-053按High/P0关闭。Schema generation保持131，无DDL、回填、开发库重置或远端public写入；既有unique/FK/删除trigger继续生效但每帖最多96行。回滚只会恢复无领域预算和逐项N+1，没有协议兼容收益，不提供开关。TEST-042、ARCH-025、BUG-090/091及悬赏完整行为矩阵仍独立OPEN。

## D-164：共享内容历史以一个全局tuple协调多来源keyset

- 旧社区与资料资源历史没有limit/cursor；通用`contentRevisionHistory`按对象返回全部修订。资料资源还会完整读取`resource_import_snapshots/catalog_import_revisions`，随后在Go中append并全量排序。十万次编辑会同时放大数据库工作、响应、Go候选和浏览器DOM；Modpack、simple project与changelog也复用同一无界helper。
- 新请求只接受单值`limit/cursor`，默认50、最大100；资料资源额外允许单值`version`。未知/重复、page/offset/all、非法limit、畸形/未知字段或跨目标/limit cursor均在数据库访问前400。base64url JSON v1 cursor绑定规范目标和limit，保存UTC创建时间、`manual/import`来源与稳定ID。
- 全局顺序为`created_at desc`、同时间manual优先、同来源ID desc。手工SQL使用`content_revisions`创建时间与public ID；导入SQL使用资源snapshot的创建时间与revision ID，后者代表该资源获得该导入事实的时间。两源使用同一排他tuple，各自最多读取`limit+1`，Go只合并最多202个窄历史项并裁成一页；无全集append/sort、同步COUNT或OFFSET。
- generation132新增`idx_content_revisions_history(aggregate_type,aggregate_key,created_at desc,public_id desc)`和`idx_resource_import_snapshots_history(resource_id,created_at desc,revision_id desc)`。资料导入游标谓词刻意只引用snapshot列，使规划器可在联接revision前直接锚定；revision仍以主键逐页验证target version、状态和submitter。当前发布项继续由对象小投影提供`published_revision_id`并在页内标记，不为页导航扫描总量。
- 社区、Mod资料、Modpack、simple project和changelog五类GET统一返回`items,limit,hasMore,nextCursor`。Next16共用组件每次替换一个50项页并保存opaque cursor栈，实现Previous/Next和请求取消；不累计无限数组、不伪造总页数，也不保留旧无界DTO旁路。
- 真实PostgreSQL临时权威表装载50k手工+50k导入历史。首/次页各50条、零重漏；首屏后插入更晚手工修订不会漂入已锚定次页。尾部两页50+49完整终止；深页手工/导入计划分别约0.160/0.427ms，命中新复合索引且无目标历史表Seq Scan。精确项目审核者/自审/跨项目的现有历史矩阵再次全绿。
- PERF-054按Medium/P1关闭。generation132完整临时Schema安装46.86s并在清理后确认远端public generation85与临时关系零变化；未重置或写入远端。回滚需协调恢复generation131、无界后端与旧前端，会恢复十万级全集读取，不提供兼容开关。PERF-055的差异逐项写入、TEST-043内容/AI广域行为和其他独立历史Finding不随本项关闭。

## D-165：完整snapshot负责重建，差异表只保存有界可核验审阅索引

- 旧`storeContentChangesTx`先递归生成全部对象叶子diff，再为每条change单独`Exec INSERT`。数组不递归而是把完整before/after都写入一行；初始对象也会把整个after再写一遍。最大8MiB snapshot已在`content_revisions`中持久化，因此差异表会重复大JSON，宽对象还把一个修订事务放大到数千往返。
- 新持久diff独立于Mod比较用的完整内存`diffJSON`。`boundedContentChanges`最多保存128项、递归64层；JSON Pointer最多512字节，超长路径保留有效UTF-8前缀并附16位哈希决胜。达到第127个详细变化后停止遍历并追加path=`/`根替换摘要，使超限事实显式而非静默丢失。
- 单个before/after值的JSON编码最多512字节。超限array/object/string/其他值保存`$summary`类型、原编码字节数与SHA-256；数组/对象另存items/fields数，字符串保存字符数与最多64字符预览。完整before来自base revision snapshot、完整after来自当前revision snapshot，调用方可按revision ID取权威内容重建；摘要hash可核对内容而不复制原值。
- 所有path、operation和nullable JSON文本先组成四个等长数组，单次`unnest($2::text[],$3::text[],$4::text[],$5::text[])`插入`content_change_items`。有base时无论2项还是128项都固定为一次base读取加一次批写；相同snapshot只做base读取且不发空INSERT。Query/编码/批写任一错误仍回滚整个修订事务。
- 审核队列中仅blueprint/creator使用值级diff摘要；其`string_agg`现在按change ID稳定排序并在响应侧截到8192字符，数据库输入已经受128×512B硬界约束。项目更新通知继续读取有界path；根摘要规范化为`published_content`，保证预算截断仍触发通用更新section。Mod revision compare仍由两个完整DTO即时计算，不消费摘要表，响应字段和值不变。
- 真实PostgreSQL使用约1,325,912/1,325,915字节的before/after，其中包含10,000项大数组和1,000个变化字段。持久结果精确128行、4,904字节，无JSON array原值，包含一条array摘要和一条root预算摘要；小2字段与该大对象均精确2 SQL。完整审核队列UNION在真实public Schema上执行成功。
- PERF-055按Medium/P1关闭。Schema generation保持132，无DDL、回填、开发库重置或远端public写入；复用现有JSONB列、revision索引与immutable trigger。回滚只会恢复逐叶N+1和大值复制，没有协议兼容收益，不保留旧存储开关。LEGACY-019/OPS-019已经各自独立关闭；TEST-043及内容AI/审核其余行为覆盖仍独立OPEN。

## D-166：等级配置提交只保存版本化意图，全站派生事实由有界租约任务重算

- 旧PUT在一个HTTP事务内更新配置，然后`select user_id,experience from user_experience for update`锁住全表并装入Go切片。每位用户再单独更新level，`SyncTrackRole`继续锁users行、删除旧角色、读取整条轨道并插入新角色；10万至100万用户会把请求变成长事务和多类N+1，并与经验写入/授权写入竞争。
- generation133给`level_system_config`增加单调`version`，并新增`level_recalculation_jobs`。PUT在同一短事务内锁定所选轨道/角色事实、更新配置并取新版本、把旧queued/processing任务标为superseded，再保存阈值和role ID不可变快照；成功响应代表配置与任务均已持久，不再代表全站同步重算已经结束。
- job记录`cursor_user_id/processed_count/status/attempts/max_attempts/next_attempt_at/locked_by/lease_expires_at/last_error`及全套时间。ready与expired lease各有部分索引，`status in ('queued','processing')`的常量表达式部分唯一索引保证最多一个活动版本。新配置提交会等待至多一个正在执行的500项批次释放job行锁，提交后旧token再不能推进。
- Worker按`user_experience`主键排他游标取501项并只处理前500项；锁定的experience行数恒定且顺序稳定。单条数据修改CTE计算每用户level、只更新变化level、只删除不再匹配的`source='level_track'`绑定、只插入缺失目标角色，并在同一原子语句推进cursor、processed与租约或完成状态。manual及其他来源不参与删除。
- 每次claim生成owner token并把attempt加一；30秒租约过期可由任一实例重新claim。批次故障释放为queued并按最多15分钟指数退避；第8次或已耗尽的丢失租约进入dead并保留错误。进程取消不伪造失败，租约到期后恢复；不存在内存channel或单实例进度权威。
- 管理GET/PUT保留`roleTrackCode/levelThresholds`并新增`version/recalculation`可观测字段；第一方TypeScript合同接受该状态。请求仍使用同一路由，不保留旧同步重算开关。完整配置响应被再次PUT时，服务端接受但忽略客户端提交的只读版本/任务状态，并生成新的服务端版本。
- 真实PostgreSQL generation133临时Schema装载100,000个经验用户。两次替换配置各固定6条数据库语句，约267.8/176.7ms；新版本原子作废已处理一批的旧token。最终严格200批/200条批SQL完成100,000项，等级与派生角色零错误且manual绑定保留；深游标计划命中`user_experience_pkey`，过期租约可恢复、暂时故障回queued、耗尽预算进入dead。
- PERF-056按High/P0关闭。开发数据库需整代重置到133；测试仅安装并清理session temporary Schema，远端public保持generation85且未重置/永久写入。回滚需恢复generation132、删除任务worker并恢复同步全表事务，这会重新引入审计风险，不提供兼容路径；PERF-058～060和角色轨道编辑/删除的独立语义不随本项关闭。

## D-167：站点更新日志先截稳定主表页，再装配页内翻译

- 旧公开GET接受任意`offset`并执行`change_date desc,id desc limit/offset`；深页成本随跳过行数增长。后台GET在全部日志和翻译上聚合后固定`limit 100`，没有offset、cursor或next信号，因此第101条之后从第一方管理面永久不可达。
- 新共享请求边界默认30、最大100，只允许单值参数。公开允许`locale/limit/cursor`，后台允许`limit/cursor`；未知、重复、page、offset、非法limit及畸形cursor均在数据库访问前400。base64url JSON v1 cursor保存date-ID tuple并同时绑定`public:<normalized-locale>`或`admin` scope及limit，跨语言、跨端点和跨limit重放失败关闭。
- 两类SQL都先在materialized CTE中按`(change_date,id)`排他keyset读取`limit+1`条changelog。公开随后只为页内行LATERAL选择请求语言、zh-CN、en-US及其他语言的稳定fallback；后台只为页内行LATERAL聚合翻译。响应统一为`items,limit,hasMore,nextCursor`，无同步COUNT、OFFSET或固定终止窗口。
- generation134保留并复用既有`idx_site_changelogs_public(change_date desc,id desc) where status='published'`，新增`idx_site_changelogs_admin(change_date desc,id desc)`。排序和排他tuple与索引一致；没有翻译全表预聚合或为深页扫描被跳过的正文。
- page helper在成功DTO前检查`rows.Err()`；后台`translations`聚合必须严格解码为typed map，损坏JSON不能作为RawMessage透传。这只关闭本次触及的列表读取边界，不冒充ARCH-029其他站务聚合查询或TEST-046全行为矩阵已经完成。
- Next16公开页和后台面板都用opaque cursor加本地Previous栈导航，每次替换当前30项页。语言切换清空公开scope；后台新建/保存回到首屏。两端不累计完整历史、不按items长度猜测next，也没有旧offset或固定100项API旁路。
- 真实PostgreSQL临时表装载1,000,000条日志和1,000,000条翻译，用时5.614s。公开/后台百万深度页各只执行一条SQL并分别命中public/admin索引、目标表无Seq Scan；首屏后插入新首项不会进入既有cursor的次页且两页零重复，后台在旧100项边界之后仍返回完整50项页。
- PERF-058按Medium/P1关闭。完整generation134临时Schema安装45.36s并清理，远端public generation85保持不变且未重置/永久写入。回滚必须协调恢复generation133、两个旧响应合同与两处前端调用，会重新引入深OFFSET和不可达后台尾部，不提供双协议或兼容开关；PERF-059/060、ARCH-029与TEST-046保持独立OPEN。

## D-168：未解析引用在写时形成统一窄目录，搜索只接受有预算的literal prefix

- 旧后台GET每次把`unresolved_references`和`unresolved_resource_references`分别联接Mod、社区、Modpack、Catalog及Recipe来源，再UNION。随后用`%q%`前导通配过滤raw/source label、对全部匹配项执行`count(*) over()`，按created/合成字符串ID排序并OFFSET；十万至百万存量下即使只展示50项也重做完整装配、过滤、计数与深页丢弃。
- generation135新增`unresolved_reference_catalog`窄投影，以`origin(0 general/1 resource),source_row_id`为主键，保存列表所需source/type/raw/status/resolution/label/public ID/time字段。两张权威未解析表的insert/update/delete逐行同步投影；来源link、Mod/社区/Modpack名称与public ID、Catalog identity/public ID及Recipe canonical source变化会用集合SQL刷新受影响投影。权威事实仍在原表，投影只服务后台列表。
- 投影有全局、status、type、status+type四个`created_at desc,origin desc,source_row_id desc`页索引，覆盖all/pending/resolved/ignored和可选type组合；另有raw identifier与source label的`lower(...) text_pattern_ops`前缀索引及source反查索引。排序和cursor使用同一排他tuple，不以合成字符串的词法顺序决定跨来源边界。
- q语义从任意子串协调收敛为大小写不敏感literal prefix。输入必须为空或2..64字符；`%`、`_`与反斜杠全部转义，不成为通配注入。搜索先在同一status/type scope内最多探测10,001个匹配；超过10,000即400并不执行page SQL。被接受的搜索最多只排序一个有硬界集合，避免把B-tree前缀索引之后重新变成无界全匹配排序。
- 请求只允许单值`q/type/status/limit/cursor`，默认50、最大100；未知、重复、page、offset、非法状态/type/limit及畸形或跨query/type/status/limit cursor在主查询前400。成功响应为`items,limit,hasMore,nextCursor`；无同步total、窗口count或OFFSET，rows迭代错误在响应前失败。
- Next16后台面板使用当前页加Previous cursor栈，筛选/搜索清空scope；重复提交相同搜索也以request version重新读取。每次请求有AbortController和active代次，旧请求的then/catch/finally都不能覆盖新筛选的结果或loading状态。不累计完整引用集合，也不伪造总页数。
- 真实PostgreSQL装载1,000,000条投影，用时16.165s。百万深页命中页keyset索引且一页一SQL；精确10,000匹配的`needle059`前缀以budget+page两SQL返回50项并命中raw expression index；约990,000匹配的`bulk`前缀只执行一条有界probe即拒绝。首屏后插入新首项不进入既有cursor次页，两页零重复。
- PERF-059按Medium/P1关闭。generation135完整临时Schema在47.57s安装并实际验证general/resource投影insert、status、来源标签rename和cascade delete，随后完整清理；远端public generation85不变且未重置/永久写入。回滚需协调generation134、旧响应与前端，会恢复无界子串/窗口/深OFFSET，不保留兼容协议；TEST-047与BUG-105/106/107仍独立OPEN。

## D-169：治理历史按各自业务顺序使用scope绑定时间/ID游标

- 旧本人举报、按状态审核队列及公开/后台小黑屋都接受无界offset并直接执行LIMIT/OFFSET。本人历史缺少`reporter_id`开头且覆盖排序tuple的索引；100k至1M存量的深页会扫描并丢弃不断增长的前缀。审核队列原语义是最旧优先，其余两类是最新优先，不能用一套方向改变处置顺序。
- 三路现在共享严格base64url JSON v1时间/内部ID cursor envelope，但scope分别绑定reporter内部身份、审核status、公开或后台小黑屋端点及limit。本人默认20、审核默认50、小黑屋默认30，最大均100；只允许各端点的单值status/limit/cursor。未知、重复、page、offset、非法状态/limit、畸形/未知字段及跨身份/状态/端点/limit cursor均在数据库访问前400。
- 本人历史按`reporter_id=$1 and (created_at,id)<cursor`降序，审核队列按`status=$1 and (created_at,id)>cursor`升序，小黑屋按`(created_at,id)<cursor`降序，均读取`limit+1`并从最后一条实际返回项生成next cursor。成功响应统一为`items,limit,hasMore,nextCursor`，不做同步COUNT、总页数推导或OFFSET；Scan/rows迭代错误在写响应前失败。
- generation136只新增缺失的`idx_reports_reporter_history(reporter_id,created_at desc,id desc)`。审核升序精确复用`idx_reports_queue(status,created_at,id)`，小黑屋两端精确复用`idx_ban_records_public(created_at desc,id desc)`，不为相同tuple创建重复索引或维护总数投影。
- Next16公开小黑屋、举报审核和封禁管理均只保存当前页与Previous cursor栈；Next使用服务端nextCursor。审核状态变化清空cursor scope，新增封禁回首屏；请求取消不会把旧列表或Abort错误覆盖到新页。本人举报当前无第一方页面，但API合同同样有界并受后端测试守护。
- 真实PostgreSQL临时表分别装载1,000,000条举报和1,000,000条封禁，用时7.656s。本人、审核和小黑屋半深页各固定一条SQL，分别命中reporter、queue及ban历史索引且目标表无Seq Scan；首屏后插入各自业务顺序的新首项不会穿越已签发cursor，第二页与第一页零重复。
- PERF-060按Medium/P1关闭。generation136完整临时Schema在49.85s安装并清理，测试前后远端public generation85不变且临时关系零残留；未重置或永久写入远端。回滚需协调generation135、三个旧offset响应及前端，会恢复深页线性扫描，不保留双协议；TEST-048的举报/封禁广域行为矩阵保持独立OPEN。

## D-170：OSS文件哈希只保留一个有界分块，并把CPU工作移入可取消Worker

- 旧共享`computeFileSHA256`先调用`file.arrayBuffer()`把完整Blob再物化为同尺寸连续ArrayBuffer，然后把全部字节交给`crypto.subtle.digest`。所有用户文件、项目下载、Mod导出包和嵌入目录导入都复用该路径；大文件会同时保留源Blob与完整副本，多个入口并行时继续线性叠加，并在移动端形成不可取消的长等待。
- 新边界先验证文件size为安全整数且不超过服务端同值2GiB上限，再通过module级permit保证整个页面最多一个hash活动。每次只读取4MiB `Blob.slice`；浏览器把该ArrayBuffer以transfer list交给module Worker，等待该块ack后报告进度并继续下一块，因此不存在两个在途分块或整文件副本。
- SHA-256实现和Worker入口合并为自包含`oss-sha256-worker.mjs`。同一纯JavaScript模块既可由主线程导入增量类，又可作为`new Worker(new URL(...),{type:'module'})`的最终静态资产；`WorkerGlobalScope`守卫避免普通Window/Node导入安装消息处理器。该文件不依赖运行时import、TypeScript擦除或外部WASM路径。
- Worker不可用的测试/非浏览器环境按相同4MiB边界顺序更新增量hash，并在每块后让出事件循环。排队waiter和活动hash都接受AbortSignal；取消会从队列移除或终止Worker，finally只释放一次permit，运行中取消在下一块读取前失败关闭。Mod导出将既有upload控制器和hash进度直接传入，用户取消覆盖上传准备的完整阶段。
- 标准空串、abc和百万个a向量与权威摘要一致。虚拟20MiB+123B文件恰好读取6个slice，最大slice 4MiB、同时读取数1，结果与Node crypto一致；整文件`arrayBuffer()`被设置为调用即失败且从未触发。第二个hash在首个释放前零读取；排队/活动取消和超2GiB读前拒绝均有独立回归测试。
- Next16生产构建生成5,501字节`oss-sha256-worker.*.mjs`，`node --check`通过，产物包含完整round constants和Worker入口、没有顶层import；58个页面全部构建。PERF-063按Medium/P1关闭，无DDL、API成功DTO或OSS上传协议变化，generation保持136且未访问远端public。回滚会恢复随文件和并发线性增长的内存峰值与不可取消hash，不提供旧路径开关；其他OSS上传可靠性/体验Finding独立处理。

## D-171：资料布局只在客户端保留一个轻量页，服务端合并有界变化集

- 旧第一方API固定以20,000为页大小并循环到`total`，资料页、晋升树与布局编辑器把完整资源DTO持续聚合到浏览器状态；同一DTO还携带多语言名称、图标、详情存在性和definition。即使后端已有cursor，调用方仍主动把它退化成全量SQL/JSON/React/DOM路径。
- 公开晋升树与鉴权布局读取现在使用独立轻量DTO，只保留version/resource/section public ID、单一label、ordinal、similar group及结构化晋升父节点/组/坐标/frame；删除names、icon、detail predicate、canonical/kind及整份definition。默认与硬上限均为500，响应总预算1MiB；普通卡片保持既有默认120/最大200 cursor页。
- graph允许3..100字符服务端全文搜索并把query纳入cursor scope；layout拒绝query。两者以section树sort path、placement ordinal和内部resource ID排他keyset取`limit+1`。Next/Previous每次替换当前页而非累积；编辑页一旦dirty便禁止换页，避免未提交局部布局被静默丢弃。晋升树空态按其摘要数组判断，防止与普通卡片状态分离后误报空页面。
- 写协议协调删除旧布局PUT，只保留PATCH。请求可省略categories，resources变化集最多1000；服务端读取当前权威分类和紧凑placement事实，把页面变化按resource public ID合并，拒绝未知/重复项，并把被删除分类遗留资源安全移回根节点。之后仍构造完整服务端审核快照并进入原有审核/发布事务，浏览器从不读取或回传完整2万条事实。
- 显示模式变更只发送零资源PATCH；布局编辑器当前页最多500项，分类仍受既有1000硬上限。20k纯合并保留所有未装载资源且只耗1.615ms；旧完整20k响应按真实字段样本估算93,440,000B，500条新摘要为62,391B。客户端资源状态和可渲染布局节点因此不再随2万存量增长，未声称未执行的浏览器FPS采样。
- 真实PostgreSQL临时Schema上，100k资料的生产布局GET首500项为69,115B/724.7ms，深至第20k项仍为70,230B/1.899s；1M合成表的keyset与GIN搜索计划分别0.087/0.056ms并命中索引。晋升资源创建、首次PATCH和无变化PATCH在MaxConns=1临时Schema下完整通过，顺带修复了事务内review config与identity resolver误取独立pool连接的问题。
- 首次运行旧晋升集成测试时发现其直接使用配置DSN而非临时Schema，并意外创建唯一fixture（mod 130/user 75，slug/username `advancement-save-386600`）。随即以精确ID、slug、提交者和无引用条件各删除1行并断言目标剩余0；其他历史fixture未触碰。测试随后永久改为安装/清理session临时Schema并以单连接运行，最终41.69s通过。该事故与清理不被表述为“从未访问远端public”。
- PERF-064按High/P0关闭。无DDL、索引、回填或generation变化，generation保持136；回滚需协调恢复旧PUT、完整DTO与三个全量调用方，会重新引入2万条浏览器物化，不提供兼容协议或功能开关。PERF-071的页内算法复杂度与其他资料体验Finding继续独立复核。

## D-172：公开日志详情分离元数据与条目绑定的有界正文块

- 旧公开GET在一次查询中读取分享的全部`log_share_entries.sanitized_text`并形成Go map切片、JSON响应和React状态。前端虽然最多渲染150万字符，但截断发生在完整数据库读取、JSON编码、网络和状态物化之后；当前搜索还对完整字符串执行split，因此单条20MiB或ZIP解压总100MiB都能由公开短链反复触发复合放大。
- 详情GET现在只返回分享字段和最多200条条目元数据：index、截断到512字符的安全名、content type、byte/line count及checksum，不选择正文。entries DTO删除text，元数据最终JSON超过256KiB即413失败关闭；未知query同样400，不允许用未定义参数扩大读取。
- 新GET `/api/v1/log-shares/s/{code}/entries/{entryIndex}/content`只接受可选单值cursor。base64url JSON v1 cursor以SHA-256派生scope绑定public code与entry index，保存字符offset；未知字段、尾随数据、跨条目、非正offset或超过100MiB均400。SQL使用PostgreSQL字符substring只取32,769字符，Go截成32,768字符并用额外一字符判断hasMore。
- 正文块的最终JSON同样有256KiB预算。32Ki字符即使全为encoding/json会转义成6字节的`<`，实际响应也只有约196.8KiB；UTF-8四字节字符上限约128KiB。客户端每次替换当前块，仅保留当前cursor和短Previous cursor栈；条目/块切换用AbortController取消旧请求。搜索、复制明确只处理当前已加载块，不再对未加载全文作虚假本地搜索。
- 生产Server为正文块和完整下载共享8路非阻塞并发槽；额外昂贵读取限流在反滥用启用时按IP计数，匿名metadata/chunk/download分别30/60/5次每分钟，已认证身份翻倍。客户端取消沿request context取消PG查询；槽位由defer可靠释放，竞争满时503而非继续堆积。
- 文件下载URL和成功语义保留，因为它是用户显式获取完整脱敏文件的动作。单文件只查询一个正文；ZIP使用rows逐条Scan并立即写入`zip.Writer`，不再先构造全分享正文切片。完整下载仍受5/min及共享8并发边界，而普通页面访问永远不会触发它。
- 真实PostgreSQL `pg_temp`影子表写入20MiB单条和200×512KiB ZIP总100MiB。单条/ZIP元数据为427/27,074B；首块、20MiB深块、ZIP第200条块为196,755/196,688/196,755B，分别85.7/125.5/29.1ms，元数据135.6/87.5ms；全部低于预算和3秒门槛。测试连接在创建临时表后把search_path锁到pg_temp，无远端public永久写入。
- PERF-066按High/P0关闭，无DDL、索引、回填或generation变化，保持136。回滚须同步恢复正文详情DTO和旧前端全文状态，会重新引入公开大响应，不保留兼容详情协议；显式下载、日志创建/脱敏、本人历史分页等独立Finding不被冒充关闭。

## D-173：审核队列先截稳定页，再只对当前页集合装配摘要

- 增量审计中的PERF-067跨越两个已部分治理的队列。PERF-001已把编辑员目标名N+1改为主查询JOIN加附件批量查询，但后台仍按status读取全部申请；BUG-139/PERF-022已给服务器队列稳定游标并拆出按需详情，但数量摘要仍应明确限制在当前页ID集合。本项不重复宣称旧Finding成果，而是补齐无界页与当前页装配不变量。
- 编辑员后台GET现在只接受单值`status/limit/cursor`，status闭集含pending/approved/rejected/withdrawn，默认pending/50、最大100。严格base64url JSON v1 cursor保存UTC created time和内部ID，scope绑定status与limit；未知、重复、非法limit、未知字段、尾随内容及跨scope cursor均在数据库前400。
- 列表保持`created_at desc,id desc`，cursor使用完全同构的排他`<`tuple并读取`limit+1`。Go先裁成真实返回页，再只把这些申请ID交给附件`ANY(bigint[])`批量查询；目标名仍在主SQL中按13类公开条件LEFT JOIN。因此无论总积压多大，每页固定2条SQL且最多100项、每项最多既有10个附件。
- 服务器摘要SQL先在materialized `review_page`中用现有升序keyset取最多101项，再把该页内部ID形成数组，proof/link/mod三类分别在同一SQL中做集合聚合并回联摘要。完整body/proof正文及关联数组仍只由单项详情GET加载；列表恒1条SQL，单详情恒5条SQL，不会恢复旧`1+3N`数据库往返。
- Next16编辑员面板使用50项页、opaque continuation和ID去重追加。初始/续页共享generation与AbortController；新请求会取消旧请求，旧then/catch/finally不能覆盖当前队列。审核成功回首屏，既有内容审核offset分页是另一套端点，不被错误混入编辑员cursor。
- 真实临时Schema中205条approved申请按100/100/5完整遍历，零重复遗漏，每页精确2条SQL。临时100k申请深页只读51行，命中status/time/ID复合索引，执行0.076ms。服务器三状态各205条既有遍历再次通过；100k页读取101行并对三类当前页ID集合聚合，执行0.507ms；100条各1MiB正文的列表仍恒1 SQL且低于256KiB，单项展开恒5 SQL。
- PERF-067按High/P0关闭。无DDL、回填、generation变化或远端public访问，generation保持136；复用既有编辑员review索引、服务器generation103页索引及关联索引。省略新编辑员参数的旧调用方仍得到首个有界50项页并可忽略新增响应字段，但完整历史必须使用cursor；回滚会恢复无界申请响应及服务器逐项装配风险，不保留旧前端旁路。PERF-072及审核提交语义等独立Finding继续开放。

## D-174：预选资源展示补全使用独立精确批量边界，不复用浏览搜索

- 旧`ResourcePickerDialog`为每个`unresolved`或缺名预选项调用传入的`loadPage`。虽然最多并发6个Worker，但总请求仍为N；默认资源、项目与标签loader都会执行各自完整列表投影，部分还同步计算total。选择器打开或语言变化因此可把保存引用数同时放大为HTTP往返、模糊搜索、列表JSON和数据库工作。
- 新POST `/api/v1/catalog/resource-presentations`只接受展示引用，不接受query、sort、page或offset。请求含locale和1..1000个`publicId/id/kind/registry`引用；字符串统一trim/case-fold、locale走站点规范化、精确重复先去除，缺身份、超长字段或超1000项在查询前400。
- Handler按Catalog资源、Catalog标签和项目三类分组，空组不查询，因此每个请求最多3条SQL。资源只按active public ID、canonical ID或alias精确匹配，并在请求语言、中文兄弟、default及en-US之间选展示名；标签只按active public/canonical/registry精确匹配；项目只在approved的Mod、整合包、simple project与服务器身份UNION中精确匹配。响应只保留public/id/registry/kind、一个解析名称、图标和可选来源，不返回definition、版本集合、正文、模糊候选或total。
- 最大响应继续经过Catalog既有2MiB硬预算；资源输入为1000项时只执行1条业务SQL，混合资源+标签+项目时精确3条。各查找复用public/unique/folded身份索引；批接口不建立新的搜索投影、缓存权威或持久表。
- Next16补全effect现在只筛选确实缺名的保存DTO并发一个批量POST，使用AbortController在locale、token或value变化时取消旧请求；返回项按public ID或kind+identifier合并进当前selected map。各选择器原`loadPage`只负责用户主动浏览，custom项目/标签loader不再能改变补全成本；已携带名称的保存DTO完全跳过补全。
- 真实generation136临时Schema装载1000个资源及本地化。当前代码单请求返回1000项、202,806B、1条SQL，请求阶段728ms；同一夹具的资源/标签/项目混合请求返回三类结果且为3条SQL。完整Schema在测试结束清理，远端public未访问。
- PERF-068按Medium/P1关闭。无DDL、索引、回填、开发库重置或generation变化，保持136。接口为新增边界，旧目录/项目/标签浏览协议不变；超过1000项明确拒绝而不隐式拆成无界多请求。回滚会恢复N个浏览查询和total放大，不保留六Worker补全旁路；PERF-069～071继续独立开放。

## D-175：蓝图只持有一份方块对象，面实例用紧凑层索引和联合场景预算

- 旧场景先按state保存方块对象数组，再为模型的每个Mesh过滤并持久保存另一份`BlueprintBlock[]`；每个面实例还调用`matrix.clone()`保存一个`Matrix4[]`，同时Three.js自身已有64字节instance matrix buffer。普通六面稀疏方块因此把同一方块和矩阵放大六次，600k计数上限不能约束数百万面实例或CPU对象堆。
- 新场景以`blueprint.blocks`作为唯一方块对象表。state构建只保存临时整数index；每个最终Mesh只保存按Y层排序的`Uint32Array blockIndices`、唯一层`Int32Array`、offset `Uint32Array`和一个原型`Matrix4`。初次写GPU时复用两个临时矩阵，不再持久化每实例Matrix对象；raycast用当前TypedArray index反查唯一方块表。
- 占用测试从每方块坐标字符串Set改为体积内线性整数key；每个模型面的世界法向只计算一次，再在整数Set中判断邻居，不在每方块/每面循环创建Vector3、Quaternion或字符串。这同时降低场景构建期的临时堆与GC，但不改变原有cullFace语义。
- 层索引通过counting/prefix方式按实际出现的层一次预分桶。隐藏/透明模式使用二分确定请求层的连续subarray；主Mesh只重写该区间，相邻上下文只复制最多两个层桶。透明Mesh第一次真正需要上下文时才创建，容量为该Mesh任意两层最大实例数之和；全层初始视图不再为每个Mesh分配第二套满容量GPU buffer。
- 场景不再只依赖`MAX_VISIBLE_BLOCKS=600,000`。新增联合硬界：最多1,500,000个实际实例、128MiB已追踪的block-layer/index/主矩阵/最坏相邻上下文缓冲，以及512 draw calls。每个model part在打包和WebGL分配前先检查累计下界，完整layer/context统计后再复核；复杂模型超界可走既有有界单cube fallback，fallback后全场景仍超界则加载显式失败并释放已建组。
- 精确纯函数规模测试把600k index分入200层：最终每面索引/层/offset为2,401,604B，14.6ms；选择第100层及上下相邻层只暴露9,000项，不扫描其余591,000项。600k单实例+6k最坏上下文的生产预算估算43,609,600B；600k稀疏六面3.6m实例在GPU对象创建前触发instance预算。
- 本机Chrome headless合成TypedArray/矩阵基准以600k实例复现浏览器缓冲：45,986,404B typed buffers、46,844,856B JS heap、构建34.3ms；9k当前/相邻层写入0.5ms。该数据只证明当前工作站浏览器堆与CPU量级，不作为移动端或真实GPU FPS基准；跨设备合同来自保守硬预算和失败关闭，而不是这组耗时。
- PERF-069按High/P0关闭。后端蓝图/render API、Schema和generation136均不变，远端public未访问；仅内部load result增加instance/context/tracked byte观测字段。回滚会恢复每面对象/Matrix复制、满容量上下文和全实例层扫描，不保留旧渲染旁路；加载取消与中间资源释放由PERF-070继续独立处理。

## D-176：蓝图换源与卸载使用端到端AbortSignal，并串行不可抢占构建阶段

- 旧`StructureCanvas`只保存React effect内的`cancelled`布尔值；cleanup虽然释放renderer，但源蓝图fetch没有signal，`StructureRenderer.load`只在`buildStructureScene`完整返回后比较generation。旧任务因此仍会解析JSON/NBT、读取blockstate/model/OBJ/MTL、构建TypedArray和实例矩阵，快速换源或卸载能与新任务叠加网络、CPU和Three资源。
- 每个Canvas source代次现在创建一个AbortController。其signal同时传入蓝图render fetch、renderer、parse、scene、model和资产读取；cleanup先abort再dispose。renderer还持有自己的controller，新load会主动终止旧load，dispose终止当前load；外部signal只单向转发并在finally解除监听，generation仍作为最终提交防线。
- HTTP资产缓存改为引用计数`AbortableSharedCache`。同键JSON/text/bytes只发一个底层请求；一个消费者取消只拒绝自身，不会污染其他viewer。最后一个消费者离开时才abort缓存自有controller并驱逐pending项，失败项也不缓存，后续请求可重新加载；成功值保留原有revision级缓存语义。
- 模型state、蓝图块、Litematic/Sponge解码循环、cull过滤、层打包和instance写入均在有界间隔检查signal。模型JSON/OBJ/MTL读取直接使用signal；AbortError在block entity兼容和scene模型降级两层都立即重抛，绝不会被伪装成fallback成功。scene使用`Promise.allSettled`等待同任务Worker退出后再统一dispose已建group，避免一边释放一边继续写入。
- JSON.parse、NBT解压/解码、OBJ/MTL parser及Three TextureLoader内部阶段不能从JavaScript真正抢占。模块级公平`ExclusiveTaskGate`把parse+scene build限制为全页面最多一个任务；排队任务可在开始前取消，运行任务在每个异步边界及大循环检查。现有16M体积、600k方块和PERF-069联合场景预算继续限制单个同步段，不能把检查点误称为线程级抢占。
- 导出block entity的`TextureLoader.loadAsync`以abort race立即让调用链退出；底层浏览器图片请求若仍晚到，回调立即`texture.dispose()`而不重新接入场景。普通TextureLoader纹理由已建group统一dispose。HTTP source直接返回服务端资产URL，不创建blob/object URL，因此本项没有待revoke的URL生命周期。
- 纯测试证明gate最大活动数1、排队取消不启动；两消费者共享一次loader且单方取消不中断底层，最后消费者取消会abort并驱逐、下一次重新加载。源码合同同时守护Canvas cleanup、signal贯穿、AbortError旁路、allSettled释放和晚到texture dispose；186项Node、TypeScript、ESLint及58页Next生产构建全绿。
- PERF-070按Medium/P1关闭。无DDL、索引、回填、开发库重置、后端路由/DTO或generation变化，保持136；远端public未访问。内部可选signal保持无signal调用兼容，但不保留旧不可取消蓝图入口或功能开关；回滚会恢复换源/卸载后的重叠网络与构建。

## D-177：资料布局资源按修订建立一次section/position/cluster索引

- 原审计时编辑器会把最多20,000条完整资源留在浏览器。每次渲染对每个分类重新filter+sort完整resources，随后每个chip再对分类entries调用findIndex；多分类为C×R，大分类为R²。PERF-064已先把传输、状态、React节点和提交改为最多500条轻量cursor页与增量PATCH，但当前页渲染仍保留相同算法形状，不能仅因R变小就把PERF-071冒充关闭。
- 新纯函数`buildModContentLayoutRenderIndex`只遍历resources顶层一次，同时建立规范section ID→资源数组和resource public ID→对象。每个非空section只排序一次，再预建resource ID→position，并复用共享`clusterSimilarResources`一次形成渲染cluster；空分类直接读不到bucket，不扫描资源。
- `CategoryLayoutEditor`以`useMemo([resources,root.publicId])`持有该结构。分类名称/语言、关键字输入、选择集合、拖拽category等独立受控状态重渲染只做Map读取；section渲染删除`resources.filter(...).sort(...)`，chip上下移按钮删除`entries.findIndex`。drop、group、单项move和目标分类尾ordinal也使用同一resource/section索引。
- 资源修改会产生新的resources数组并正确重建索引；分类修改不改变资源归属时复用旧索引。现有normalize/save仍由服务端权威布局和PERF-064增量合并复核，索引只是可丢弃的视图派生，不成为第二事实源。cluster顺序和少于两个成员时解组语义继续由共享helper决定。
- 测试以Proxy监视20,000条生产helper输入，在1,000分类下顶层数字索引精确读取20,000次而非2,000万次；同时验证排序、root归一、cluster和O(1) position。源码合同禁止旧render-time filter/findIndex并要求以resources/root修订useMemo。
- 本机九轮基准中位数：10k资源/1,000分类5.195ms，20k/1,000分类7.140ms，20k单分类4.682ms；最大分别7.704/10.499/13.945ms。20k只作为与原审计的算法对照，真实发布UI继续受500项轻量页硬界，不声称渲染20k DOM或跨设备帧率。
- PERF-071按Medium/P1关闭。无DDL、索引、回填、开发库重置、后端HTTP/DTO或generation变化，保持136；远端public未访问。回滚只会恢复当前页C×R/R²派生扫描，不提供旧算法开关；PERF-064的分页、响应/DOM预算和PATCH协议继续是独立且必须保留的规模边界。

## D-178：作者认领待审队列使用稳定硬页，并按当前页集合装配附件

- 旧`GET /api/v1/admin/creator-claims`无分页读取全部pending author claim。Go在扫描每条claim后按public ID再执行一次附件查询，形成`1+N` SQL；前端一次把全部证明Markdown、附件和卡片保存在React状态。队列、证明正文与附件同时增长时，数据库往返、JSON和DOM都没有硬上限。
- 新请求只接受单值`limit/cursor`；limit默认50、最大100，未知字段、重复值、offset、非法limit及畸形/未知字段cursor均在查询前400。v1 opaque cursor保存UTC created time与内部ID，scope绑定固定`pending-author`队列和limit，不能跨页大小复用。
- 主查询保留原先最早待审优先语义，以`(claim.created_at,claim.id)>cursor`升序排他读取`limit+1`。裁到实际页后只收集该页内部claim ID，再用一次`attachment.claim_id=ANY($1::bigint[])`查询安全扫描通过的附件；每个非空页固定两条SQL，空附件也返回稳定空数组。
- 此顺序精确复用generation136既有`idx_creator_claims_queue(status,created_at,id)`，附件批查复用`creator_claim_attachments(claim_id,oss_file_id)`主键；不创建重复索引或计数表。响应为`items,limit,hasMore,nextCursor`，不做COUNT、页码或OFFSET。省略新参数的旧调用方仍能忽略新增字段并读取首个有界50项页，完整历史必须消费cursor。
- Next16管理面保存当前页、next cursor和Previous cursor栈，Next/Previous都替换items而非累积。每次加载先abort旧controller并递增generation；旧成功、AbortError和finally都不能覆盖新页。审核成功重新加载当前cursor以补足页面；若尾页因审核变空则回退上一cursor。附件presign和审核PATCH合同不变。
- 真实generation136临时Schema写入205条待审claim及5个安全附件。100/100/5三页零重复遗漏，每个非空页精确2 SQL且附件归入正确claim。临时100k表深页读取51行并命中status/time/ID Index Only Scan，执行0.068ms；测试结束完整清理，远端public未访问。
- PERF-072按Medium/P1关闭。无DDL、索引、回填、开发库重置或generation变化，保持136。回滚须同步恢复旧响应与前端全量状态，会重新引入无界JSON/DOM和`1+N`附件查询，不保留旧第一方读取旁路；审核提交语义及其他队列Finding继续独立复核。

## D-179：空库Outbox只声明一次最终Schema，不在同generation内自迁移

- generation136只接受空库安装；`Migrate`遇到任意非0且非136代次会明确拒绝，136已安装库直接返回。旧基础块仍先创建只有10列的`nats_outbox`和`created_at where published_at is null`索引，随后基础设施块新增10列、对空表执行两次UPDATE、删除重建状态约束与pending索引，并补两个最终索引。这个路径没有旧库兼容调用方。
- 权威`create table nats_outbox`现在直接声明20个最终列：event/schema/subject/aggregate/trace/payload/occurred/status/available/created/published/lease/attempt/max/error/update事实，以及命名`nats_outbox_status_check`。紧随表定义直接创建最终available+ID partial pending、status+created和aggregate+ID索引；旧created-only索引不再出现。
- `infrastructureSchemaStatements`删除全部Outbox ALTER、UPDATE、constraint/index DROP与重建，只保留其他运行时版本、通知、processed event和dead-letter基础设施。该模块拆分变化不改变语义Schema、执行顺序依赖或运行时SQL；Outbox生产者本就显式写最终列，dispatcher本就读取最终lease/status列。
- 源码RED精确报告10个缺失最终事实、三类过渡残留与旧索引；GREEN要求最终事实只能从基础定义取得且基础设施块没有`alter/update nats_outbox`或pending索引drop。真实generation136临时完整Schema确认20列、nullable/default、五态命名约束和三个业务索引；默认插入返回eventType空、schema1、pending、attempt0、max12，unknown状态由数据库拒绝。
- 临时完整Schema用例39.016s安装/验证/清理；只读比较远端public generation前后相同，未重置或永久写入远端。全量Outbox/JetStream与应用测试继续覆盖运行时状态机，前端/HTTP协议无变化。
- DB-001按Medium、LEGACY-002按审计“中风险”复核为Medium并共同关闭。最终generation保持136，无表、列、约束、索引或数据事实变化，因此不提升代次。回滚只会恢复空库的冗余跳转和未来漂移风险，没有兼容收益；不把本次源码合并冒充旧库在线迁移。

## D-180：唯一约束已覆盖的四组目录键不再维护第二份非唯一索引

- 旧generation136在`resource_import_snapshots`上先声明`unique(resource_id,revision_id)`与`unique(revision_id,registry,resource_id)`，又创建完全同序的`idx_resource_import_snapshots_resource`和`idx_resource_import_snapshots_revision_registry`；`recipe_layout_templates`与`mod_content_sections`也分别在同序UNIQUE之后创建`idx_recipe_layout_templates_type`与`idx_mod_content_sections_tree`。四个显式索引不提供额外排序键、partial谓词、表达式或include列。
- generation137删除这四个非唯一声明，完整保留四个UNIQUE约束及PostgreSQL为其拥有的唯一B-tree。`idx_resource_import_snapshots_history`、mod section的parent FK索引及active system-key partial unique等不同键序或谓词索引保持不变；DB-008登记的第五项不在本次范围。
- 真实临时完整Schema通过`pg_index/pg_constraint/pg_attribute`逐列核对：四组目标列各自精确只有一个无谓词、无表达式索引，全部`indisunique=true`且由`contype='u'`约束拥有；四个被删除名称均由`to_regclass`确认不存在。仓库生产源码也没有按这些名称提示、REINDEX、监控或迁移的依赖。
- `enable_seqscan=off`下四类代表读取全部保持index-backed且无Seq Scan。第二组resource revision/registry与recipe type直接使用保留的constraint index；resource exact lookup由不同键序的history index覆盖；mod tree lookup由保留的parent FK index覆盖。选择后两者是优化器的合法成本决策，不需要为强制某个索引名称重建重复结构。
- DB-004按Low关闭。generation136→137是开发期breaking Schema替换，无在线兼容迁移、双索引阶段、回填、表/列/约束/API变化；本地开发库必须重置到137。集成测试只安装并清理session临时Schema，远端public generation仅在测试前后只读且不变，未重置或永久写入。
- 回滚必须把四个显式索引与新的Schema generation一并协调恢复；这会重新引入每次相关写入的额外B-tree更新、WAL、磁盘与vacuum成本，没有运行时兼容收益。

## D-181：自动镜像以供应商文件身份唯一，上传后登记失败必须可补偿

- 旧`mirrored_project_files`同时约束`unique(source_type,external_file_id)`和`unique(source_type,file_sha256,byte_size)`。Worker查重只使用前者，却先把文件上传到项目随机OSS key，再插入`oss_files`与mirror。不同项目的两个合法供应商file ID只要内容和大小相同，第二个事务就被无项目scope的内容约束拒绝，已经上传的对象没有补偿。
- generation138删除内容字节唯一约束，只保留供应商事实`(source_type,external_file_id)`。SHA-256和size继续作为完整性、扫描与观测字段，`oss_files`既有hash/size partial索引继续提供内容查找能力，但内容相等不再等同于业务身份相同。本项不引入跨项目内容对象共享、引用计数或第二身份表。
- 上传前existing读取现在只把`pgx.ErrNoRows`解释为尚未镜像；连接、权限、Scan或其他数据库错误立即返回，不再在未知状态下继续下载/上传。相同provider ID的并发仍由唯一约束裁决，不创建重复业务身份。
- 上传后`oss_files + mirrored_project_files`事务或commit失败时，补偿使用`context.WithoutCancel`加15秒独立deadline。它先按随机object key查询是否已有持久`oss_files`记录：若存在，视为commit结果可能已落地并绝不删除；若不存在，直接DeleteObject。直接删除失败时以独立事务写入既有可靠`oss_object_deletion_outbox`，reason固定为`project-automation-registration-failed`，原登记错误与cleanup结果一并返回，失败不会静默。
- 真实generation138临时Schema插入两个项目、两个OSS记录及同一source/相同SHA-256+4096 bytes但不同external file ID，二者同时存在；catalog确认provider identity constraint精确一个、旧content constraint为零。fake OSS证明已取消父context仍发出一次DELETE、已注册object零DELETE、403会留下pending durable deletion job。
- DB-006按High/P0关闭。无HTTP/DTO/前端迁移或历史数据回填；开发库从137重置安装138，远端public只读代次前后不变且无永久写。回滚会恢复合法跨项目冲突和上传后孤儿对象风险，必须协调新的generation，不能只恢复约束。

## D-182：项目文件public ID只在所属项目scope寻址，不注册不存在的通用路由

- 旧`project_files`插入后触发器把同一public ID注册成`entity_type='project_file'`，canonical path为`/api/v1/project-files/{publicID}/download`；服务器从未注册该路径，全仓也无调用方。物理DELETE触发器才删除route，但业务删除只把file status置为deleted，导致错误链接与软删状态永久分裂。
- 当前真实下载是POST `/api/v1/projects/{projectType}/{projectId}/files/{source}/{fileId}/download`。内部文件SQL同时要求public ID、所属project type/internal ID、`project_file.status='active'`、`oss.status='active'`与`oss.scan_status='clean'`；它不通过generic public route解析，也不需要单独的route身份。
- generation139因此删除register/remove两个数据库函数与两个触发器，不增加虚假的通用handler或410兼容层。`project_files.public_id default new_public_id()`不变，函数在`public_id_registry`中原子保留全局九位ID；文件列表、项目scope下载、删除、update event与audit aggregate key继续使用同一稳定ID。
- 真实临时完整Schema创建project route、clean OSS记录与active file后，文件ID在registry精确1行、`public_routes`中project_file精确0行，项目scope active查询精确1行；soft delete后generic route仍0且scope读取变0。OSS自身route属于不同`oss_file`实体并保持不变。
- DB-005从UNRESOLVED复核为Medium并关闭：错误canonical link和永久第二状态事实能误导通用链接消费者并增加维护风险，但真实文件、OSS对象与授权读取没有被绕过。无HTTP/DTO/前端迁移或历史回填；开发库从138重置到139，远端public只读代次前后不变。
- 回滚必须恢复两函数、两触发器及新的generation，并会重新制造不可达URL和soft-delete不一致；不应通过实现一个脱离项目ownership的generic下载端点来保留错误身份。

## D-183：聊天presence只保留Redis与有界本地fallback，不声明空PostgreSQL权威

- 旧基础Schema声明`user_chat_presence(user_id,conversation_id,expires_at,updated_at)`及用户/会话FK，但全仓没有SELECT、INSERT、UPDATE或DELETE；唯一表名命中就是CREATE TABLE。它既不参与消息presence端点，也没有清理Worker，因此只是一个永远空且会误导维护者的第二事实模型。
- 实际`querycache.Cache`按用户在Redis `chat-presence:user:{id}`键中写conversation ID并使用presence TTL；Redis关闭/失败时读写有硬容量和过期清理的进程内`chatPresence` map。HTTP消息presence调用该边界，数据库不在一致性、恢复或降级链上。
- generation140直接删除该表定义，不新增DAO、双写、Redis同步或兼容迁移。用户、direct conversation、message/unread tables及其FK/trigger完全不变；presence Redis key、TTL、metrics与前端协议不变。
- RED证明完整安装语句仍包含表名；GREEN后production Go零表名命中。真实临时完整Schema的`to_regclass`返回null，generation140 metadata正确；`go test ./internal/querycache`验证现有presence读写、过期、容量及fallback继续通过。
- DB-007和DEAD-006是同一Schema残留的数据库健康/死代码两个视角，均按专项审计的低风险收益复核为Low并共同关闭，不重复计作两个运行时修复。无历史数据迁移；开发库从139重置安装140，远端public只读代次前后不变。
- 回滚会重新声明无消费者关系并恢复第二事实源误导，必须协调新的generation；没有外部兼容或恢复数据的理由。

## D-184：nullable FK可复用精确非空partial前缀，不维护重复单列B-tree

- `log_shares`同时声明`idx_log_shares_owner_created(owner_user_id,created_at desc,id desc) where owner_user_id is not null`和`idx_log_shares_owner_fk(owner_user_id)`。前者已以FK列开头；任何父表删除/更新时的`owner_user_id=$1`反查都蕴含`owner_user_id is not null`，后者没有额外排序、include、表达式或谓词能力。
- 仅删除显式单列索引仍不完整：空库安装末尾的自动FK索引生成器以及`TestEveryForeignKeyHasLeadingIndex`都无条件排除partial index，因而会重新创建`idx_fk_log_shares_log_shares_owner_user_id_fkey_*`。真实generation141临时Schema首次RED精确得到两个owner前缀索引，证明审计所述测试/生成器固化路径仍存在。
- 生成器与质量测试现在采用同一窄规则：索引必须valid、ready、非表达式、按FK列前缀；partial仅在单列FK且`pg_get_expr`精确等于该列`IS NOT NULL`时可接受。`status='active'`、deleted谓词、其他列谓词或任意复杂表达式仍不能冒充完整FK维护覆盖。
- GREEN完整Schema中owner前缀精确只有`idx_log_shares_owner_created`一个，catalog谓词精确为`(owner_user_id IS NOT NULL)`，hashed自动索引未生成；禁用Seq Scan后的`select id from log_shares where owner_user_id=1`计划命中该索引且无Seq Scan。其他source-file FK、expiry与active-source索引不变。
- DB-008按Low关闭。generation140→141只减少一个非唯一B-tree并修正空库索引发现规则，无表、列、FK、数据、回填、HTTP或DTO迁移；开发库需重置，测试只在会话临时Schema写入，远端public generation前后只读且不变。
- 回滚必须同时恢复显式索引、旧生成器规则和新的generation，会重新增加log share写入、WAL、磁盘与vacuum成本；不存在依赖被删索引名称的生产调用方或外部兼容收益。

## D-185：超大后台与Handler按既有业务声明边界拆分并设置机械上限

- 当前复核确认审计问题仍存在且更突出：`admin-console.tsx`为6215行；生产Go中`mod_content/oss/mod_export/skin/mod/comment/catalog_editor/creator`八个`*_handlers.go`为1533..2536行。它们把独立业务面、辅助类型和读取/写入路径放在单文件，修改冲突和源码审查范围无法由领域边界限制。
- 前端先用TypeScript AST列出top-level声明依赖，再保持函数体原文按permissions、infrastructure/settings、users/notifications、OSS/logs及shared contracts抽为五个模块；`admin-console.tsx`只保留Panel ID、导航组、加载/授权状态和面板装配。`AdminConsole`导出路径、所有panel props、API调用、hooks及事件名不变。
- Go按现有top-level声明边界机械移动，不引入新interface/service层：八个超限文件分别在section/resource、upload/admin、worker/import helper、skin/profile、read/mutation、comment/watch detail、catalog/recipe、creator/mutation边界拆成同package文件。方法名、非导出类型、SQL文字、路由注册和调用图不变。
- 既有源码合同原先直接读取单个旧文件；拆分后只把输入扩展为相应领域文件组合，严格namespace、placeholder治理、capability、template guard、manual ownership、quota、differential sync、comment keyset/batch、profile batch及admin log cursor断言均保持原计数和禁用条件，没有删除或放宽。
- 新门禁要求所有生产`*_handlers.go`不超过1500行、全部`admin-*.tsx`不超过2000行且console shell不超过1200行。GREEN结果：Go最大1462，八个旧超限归零；console 793，五个新模块870..1371，既有最大admin模块1764。阈值是防职责重新聚合的review gate，不冒充复杂度或耦合度量。
- ARCH-002按原Medium/P2关闭。无DDL、generation、数据、HTTP/DTO或外部导入路径变化，generation保持141；回滚会恢复八个Go冲突热点和单个6215行前端组件并删除回归门，不提供兼容收益。

## D-186：用户统计只有明确无totals才是空，流中断与数据库故障必须失败

- 旧`loadUserPrivateStatistics`遍历全部活跃日期后直接close并调用`activeStreaks`，不检查terminal `rows.Err()`；网络/服务端流中断可能已返回部分日期，因此接口会把不完整集合计算成较短current/longest streak并仍返回200。
- 旧`queryUserActivityStatistics`对累计范围读取`user_statistics_totals.last_edit_at/last_comment_at`时把整个`QueryRow.Scan`赋给`_`。缺少totals行、连接失败、权限错误、列解码错误都表现成同一对null时间，调用方无法区分真实无活动与统计存储故障。
- 日期读取抽为`loadUserActiveStreaks`：query/Scan/terminal iterator任一错误都在调用`activeStreaks`前返回；rows显式关闭。故障注入让一行合法日期后返回terminal error，断言结果为0/0/error而不是部分streak。
- totals读取抽为`loadUserLatestActivityTimes`：只对`pgx.ErrNoRows`返回`nil,nil,nil`；其他错误传播，成功保留两时间。累计统计调用方收到错误后停止响应构造，HTTP既有顶层错误边界稳定返回5xx。
- ARCH-005按原Low/P2关闭。无Schema、generation、数据、DTO或第一方前端变化，generation保持141；本项不替代ARCH-006成长投影重放。回滚会恢复统计漂移被200/null隐藏的行为，没有兼容收益。

## D-187：durable活动只有在非可重建成长投影同事务提交后才能确认

- 旧`DrainDurable`在事务内写入`user_activity_events`并删除`activity_event_outbox`，提交成功后才调用成长`processor`；`ProcessActivityBatch`又自行开启/提交另一事务。成长处理器失败只能写日志，已经删除的Outbox没有身份可供自动重放，任务进度及一次性经验/货币奖励会永久遗漏。
- durable路径现在把路由解析、raw活动复制、`ProcessActivityBatchTx`以及Outbox删除按此顺序放入同一`pgx.Tx`。投影query/Scan、任务进度、奖励、角色同步、Outbox删除或commit任一失败都回滚raw与投影；原Outbox行保留，由既有`attempts/last_error/available_at`退避再次领取。成功提交后才返回drained count。
- `progression.Service`保留独立`ProcessActivityBatch`供明确可丢的view批次使用，同时抽出接受调用者事务的入口；active task和timezone读取也使用该事务。commit后权限版本缓存只做删除，使下次读取回源权威数据库，缓存可用性不进入业务事务，也不会在投影已commit后返回可导致重复应用的错误。
- 站点日活聚合与成长投影分开：前者可从raw事实周期校准，维持commit后best-effort；后者包含任务完成与一次性奖励，不能仅靠无身份增量重算，必须位于durable确认边界。真实PG故障注入证明旧实现`count=1/err=nil`，新实现故障后Outbox/raw/projection为1/0/0，释放退避重放后为0/1/1且commit hook仅一次；真实Service外层rollback/commit对应任务进度0/1。
- ARCH-006按原Medium/P1关闭。无Schema、generation、HTTP DTO或前端迁移，generation保持141；远端public未执行DDL/永久写入。回滚无需数据转换但会重新打开永久漏任务/奖励窗口，不提供兼容开关。

## D-188：反滥用安全状态失败关闭，遥测失败分类可见且不提前丢内存事实

- 旧自动限制先后执行restriction和user state两个自动提交写并忽略两次错误；HTTP仍返回“临时限制”，即使数据库没有限制事实。现在先读取有效设置，再在一个事务写两项并commit，commit后才失效账号缓存；任一步失败由`RecordDecision`返回并计入`restrictionFailed`，中间件返回503 `anti_abuse_state_unavailable`且写结构化app log，不再声称未持久化的限制。
- 风险事件worker不再忽略`insertEvent`；失败计入`riskEventFailed`并记录action/outcome。按秒合并的daily统计只在upsert成功后删除map项，失败计入`riskDailyFailed`并保留到下一tick；关机最终超时才按原始事件数转入`riskDropped`。PERF-004已有的2048风险队列、1024成功队列、固定worker、2秒写截止和3秒Close排空继续作为容量边界，本项不重复重构生命周期。
- 成功请求的event/fingerprint/cleanup继续由有界worker异步，因为业务mutation已经提交；`recordSuccess`用`errors.Join`汇总三类失败，worker计入`successFailed`并写action级日志，公开`RecordSuccess`也返回错误。Crawler事件返回错误并计入风险事件失败，不再吞掉insert错误。
- bot规则加载现在对query、每行Scan、terminal rows.Err、cache loader和JSON解码全部传播错误，绝不返回部分规则集或把损坏缓存解释为空。HTTP收到错误后记录`botRuleLoadFailed`与结构化降级事件，分类为`SuspiciousBot`：GET进入低预算，写请求被既有crawler-write规则保守拒绝；规则数据库恢复后缓存loader自然重试。
- `AsyncRecorderMetrics`新增`riskEventFailed`、`riskDailyFailed`、`restrictionFailed`和`botRuleLoadFailed`（连同既有`successFailed`），管理员基础设施端点直接暴露。无Schema/generation/数据迁移，generation保持141；ARCH-007按原Medium/P1关闭。回滚会恢复伪限制、部分/空bot规则和无信号遥测丢失，不提供兼容开关。

## D-189：不可逆活动删除以批次原子进度和可恢复终态表达

- 旧手工与自动清理在事件实际删除后忽略`activity_cleanup_runs`终态更新错误，既会对客户端伪报completed，也会让审计永久停在running且缺失删除数。现在每批在同一数据库事务内DELETE并以`deleted_count=activity_cleanup_runs.deleted_count+batch`累计进度；进度写失败或run不再running时该批DELETE回滚，因此任何已提交删除都有持久计数。
- 手工与自动路径统一调用`finalizeActivityCleanupRun`并检查结果。手工终态失败返回202 `ACTIVITY_CLEANUP_AUDIT_PENDING`和`status=audit_pending`，只报告本次已提交删除数；前端以双语明确“审计待自动修复、请勿重复”，不把202误呈现为completed。清理本身失败且终态可写时记录failed；终态也不可写时同样保留running以便修复。
- Activity retention Worker启动时及每分钟扫描最多20个超过1分钟的running run，从已持久化的不可变过滤器继续有界批量删除并重试completed；无效过滤器转failed且finalizer错误仍可见。并发修复由running条件保护：先完成者终结run，其他执行者的下一批进度写失败并回滚对应DELETE，不会产生未计数删除。
- generation保持141，无DDL、回填或远端数据库访问；复用现有`filters/deleted_count/status/started_at/finished_at/error_message`即可完整恢复。真实PG通过约束分别拒绝第二次进度累计与completed终态，证明删除回滚、累计续跑及repair收口。ARCH-008按原Low/P2关闭；MAP-003破坏性枚举重复源仍独立开放。

## D-190：目录编辑详情必须在全部必需事实读取成功后才返回200

- 旧recipe type详情忽略template/recipe计数、本地化和催化剂错误，template详情忽略slots与本地化，recipe详情忽略本地化；共享`catalogPendingReviewStatus`又把连接、Schema或Scan故障一律伪装为approved。现在这些读取连同binding、published revision、父类型active和主详情行都先完整成功，再一次构造响应；任何失败均返回稳定500，不泄露底层错误。
- “业务不存在”保持窄语义：`catalogEditorEntityByPublicID`的not-found sentinel和主行`pgx.ErrNoRows`返回404；change request明确无行才返回`approved,nil`。其他错误原样向调用层传播，`catalogRecipeTypeIsActive`也由bool改为`(bool,error)`，避免数据库故障伪装成inactive/404。
- `logCatalogEditorReadFailure`为resource、tag、recipe type、template和recipe的每个必需读取记录精确query context及原始数据库错误；公开响应仍只有稳定安全文案。真实PG在当前完整临时Schema中依次重命名template、localization、catalyst和review表，健康详情为200，四类故障均为500且日志可区分，逐项测试后恢复表名。
- 无Schema/API成功DTO或前端迁移，generation保持141，远端public未访问。ARCH-010按原Medium/P1关闭；ARCH-009结构化催化剂错误、PERF-008详情查询规模继续保持各自已验证边界。

## D-191：自动化调度与管理列表不能把数据库故障解释为空闲、空集合或成功状态

- 旧项目自动更新`tick`在任务处理失败时静默停止，`scheduleDue`忽略逐行Scan、terminal stream、run insert和commit错误；旧共享`querySimpleRows`把Query失败变空数组、Values失败变跳行且从不检查`rows.Err()`。因此设置、运行历史和后台概览会把数据库故障显示为“没有任务”，调度commit失败也没有运维信号。
- 两个调度器现在都返回error：expired lease恢复、advisory lock、配置/到期行query与Scan、terminal cursor、任务insert、配置推进和commit逐层带上下文传播；周期`run`在首次和每次tick记录错误。未获得锁、配置禁用、未到期或确实无任务仍是明确的正常nil结果。
- `querySimpleRows`及context变体改为`([]map[string]any,error)`，共享`collectSimpleRows`在任一`Values`或terminal cursor失败时关闭rows并返回nil/error。全部调用方在构造成功响应前检查错误；项目设置、run历史和后台概览故障稳定返回500。填充run/candidate列表继续保留逐行Scan和`rows.Err()`检查。
- 填充爬虫的当日导入数与项目存在性是预算/去重权威，翻译task、草稿submitted状态和外部来源绑定是最终审计事实，全部改为必需成功操作。翻译AI调用本身失败仍按既有逐locale降级继续，但写failed审计也失败时用`errors.Join`传播；submitted或来源绑定失败不再返回成功状态。
- 单连接完整临时Schema用deferred constraint trigger分别拒绝项目run与填充run commit，均得到error且run数为0，解除后各精确创建1；表重命名进一步证明后台概览500、两个scheduler及填充当日计数/项目存在/翻译预算均返回错误。无Schema、成功DTO或前端迁移，generation保持141，远端public未访问。ARCH-013按原Medium/P1关闭；OPS-005租约续期与OPS-006供应商全失败终态保持独立OPEN。

## D-192：任务配置只有通过完整启用门后才能推进进度或标记奖励

- 旧`activeTasks`对condition反序列化失败或非正target直接`continue`，对rewards反序列化结果赋给`_`后仍加入任务；因此condition损坏表现为任务消失，rewards损坏则以零值结构达到目标、写入`completed_at/rewarded_at`并成功commit，用户永久失去奖励且没有稳定错误。
- 新共享decoder先把持久JSONB解为强类型condition/rewards，再校验规范action/object、`count|markdown_bytes`、正target、可选trimmed object public ID、非负经验、规范货币code、正金额以及“至少一项奖励”。任一错误携带active task ID从`ProcessActivityBatchTx`返回；ARCH-006的durable事务因此整体rollback并保留Outbox重试，不会写部分进度或奖励标记。
- JSONB无法表达货币FK，active loader在关闭task row stream后把所有引用code一次查询`currencies where status='active'`；缺失/disabled code在计算deltas前失败。奖励发放与`rewarded_at`本来已位于同一调用者事务，保留该并发claim边界；实际grant失败仍rollback。HTTP保存继续检查业务配置，并把currency查询故障区分为500而非伪“未知货币”400；JSON编码错误也不再忽略。
- 用户任务与后台任务列表在反序列化condition/rewards前调用同一验证边界，损坏配置记录task public ID和底层错误并返回稳定500，而非空对象/成功200；后台列表同时补齐terminal `rows.Err()`。合法DTO、排序、保存请求和前端展示无需迁移。
- 真实generation141临时全Schema依次写入错误target类型、空rewards、未注册货币和合法1经验任务：前三者均返回error且对应progress/completed/rewarded精确0/0/0；前两类HTTP读取500；合法控制精确为1/1/1并写1经验。既有外层事务测试改用合法非空奖励后仍证明rollback/commit为0/1。BUG-060按High/P0、ARCH-014按Medium/P1共同关闭；TEST-031的并发领取、角色来源、等级边界与缓存生效矩阵保持独立OPEN。

## D-193：可靠队列持久指标不可读时整个基础设施快照失败，不输出unknown伪零值

- 旧`infrastructureMetrics`把同时读取NATS Outbox pending/oldest、未重放死信和三类OSS补偿dead的`QueryRow.Scan`赋给`_`，随后无条件用六个Go零值返回200。数据库断连、列漂移或任一关系不可用与“所有队列健康且零积压”完全不可区分。
- 六项事实抽为typed `infrastructureDurableMetrics`和`loadInfrastructureDurableMetrics`，唯一聚合查询或Scan失败返回零内部对象加带上下文error。handler把它放在realtime、queue、cache等进程指标读取之前；失败写结构化`slog`并返回稳定500 `failed to load infrastructure metrics`，不会拼装部分或unknown-as-zero响应。
- 选择整个端点500而不是新增nullable/unknown字段：该管理员端点当前把多项事实作为同一采样时刻的健康快照，成功DTO已有消费者；只让数据库字段unknown仍可能被旧客户端归零展示。失败沿既有请求错误处理，健康字段、类型、路径和前端不变，无版本化或双协议。
- 真实PostgreSQL测试用会话临时影子表产生pending=1、dead letters=1、OSS dead=1/2/3及约2分钟oldest控制；最初表重命名夹具暴露`search_path`会回退只读public关系，遂改为仅重命名临时`published_at`列。最终helper返回SQLSTATE 42703、零内部对象，HTTP明确500并记录底层错误；从未修改public Schema/数据。
- 无DDL、generation、数据迁移或前端变化，generation保持141。ARCH-015按原Medium/P1关闭；PERF-038对完整死信历史的稳定游标已独立关闭，本项只修复指标真实性。回滚会恢复事故被伪装成零积压，禁止保留吞错兼容路径。

## D-194：Minecraft版本配置只有明确缺行可使用默认，故障必须跨全部消费域传播

- 旧`loadMinecraftVersionConfig`先构造完整硬编码目录，然后把Query/Scan、JSON反序列化或`normalizeMinecraftVersionConfig`任一错误直接转换成该默认且没有error/log。公开API、后台更新前读取、recipe排序/校验、mod-content兼容fallback、同步和项目自动化都会把故障当成可信全量兼容事实。
- loader改为`(minecraftVersionConfig,error)`：仅`pgx.ErrNoRows`返回默认/nil，代表系统尚未配置；数据库、shape与语义规范化错误返回零config和可由`errors.Is`识别的`errMinecraftVersionConfigUnavailable`，同时保留底层诊断文字。合法存储仍先normalize再与默认仅按既有空字段规则合并。
- 全部调用方同步迁移。公开读和管理员保存前读取写精确日志并500；同步handler对typed内部错误返回500，而Mojang/loader上游故障继续502；recipe列表/详情记录查询上下文，mutation与mod-content验证返回内部500；自动化兼容合并返回带领域上下文的Worker错误。没有调用方保留默认吞错wrapper。
- 真实PG使用临时`system_settings`影子表验证：缺行得到现有显式默认；`versions`错误类型、81字符version规范化失败和临时`value`缺列均为零config/error；公开读与后台更新在损坏shape上均500并记录日志；合法单version/Forge配置精确保留。首次normalize夹具因`jsonb_build_object`参数类型不明确失败，显式`::text`后通过，未触碰public关系。
- 无Schema、generation、数据或前端迁移，保持141。ARCH-016按原Medium/P1关闭；本项不声称默认目录本身正确：BUG-062的全loader兼容默认、ARCH-017具体artifact快照、OPS-012跨实例同步租约和PERF-039配置缓存均保持独立OPEN。回滚会重新把数据库事故伪装为可信目录，禁止保留。

## D-195：MRPack具体loader artifact只由同步事务发布，导出不得访问实时上游

- 旧版本目录只持久`loader -> supported Minecraft versions`，而`buildFavoriteModpackExportPreview`在预检和创建时另行访问Fabric/Maven metadata选择“当前最新”artifact。selector、同步配置和导出因此存在两套权威；上游事故会阻断已有目录的导出，且最终选择缺少来源、观测时间与目录代次。
- generation142新增`minecraft_loader_artifact_versions`，持久`catalog_hash,minecraft_version,loader_type,loader_version,source_url,observed_at`。`catalog_hash`只规范化、排序并哈希三个MRPack loader的可选兼容集合，名称、展示顺序、common列表及非MRPack loader变化不会制造无意义失效；任一真实可选tuple变化必然产生不同hash。
- 每次同步先从至多三个有界artifact目录批量解析：Fabric全局loader目录一次，Forge Maven metadata一次，NeoForge按是否包含1.20.1最多读取modern/legacy两个已受共享URL缓存与四路并发预算保护的metadata。无法解析具体artifact的Minecraft版本从该loader的selector集合移除并留下失败状态；整个loader来源不可读则同步失败，保留上一份完整快照。
- 规范化JSON设置与完整tuple集合在一个数据库事务内upsert、全量替换并以`COPY`批写；任一设置、删除或artifact写失败整体rollback。手工配置保存也在同一事务删除不同compatibility hash的行。同步写入前验证每个可选MRPack tuple恰有一个非空版本、来源和观测时间，不允许缺口、重复或目录外行。
- 预检和创建继续共用`buildFavoriteModpackExportPreview`，但它只加载严格Minecraft配置、重算当前hash并按三元主键查询持久版本；明确缺tuple返回422，配置/表/Scan故障返回500。上游客户端被强制改为失败后，真实PG读取仍精确返回既有版本且网络请求为0；目录变化后旧行被删除且不能跨hash读取。
- 请求时resolver、逐选择Fabric/Maven下载和其256项私有缓存已无生产调用方，直接删除；PERF-039保留同步层URL缓存/singleflight/条件请求/响应与并发预算，其最终状态更新为导出零外呼。ARCH-017按原Medium/P1关闭；BUG-063、BUG-065、OPS-012、MAP-009与TEST-033继续独立OPEN，不用持久authority冒充版本形状、预检确认冻结、跨实例租约、完整血缘或系统矩阵。
- 这是pre-production generation切换，无旧表回填、双读或实时fallback；开发库必须重置到142，远端public generation85未访问。回滚需要回到旧generation并会恢复两套权威和上游事故阻断，禁止保留兼容开关。

## D-196：MRPack收藏导出只有完整读取、状态CAS和通知成功才形成可观察成功

- 旧详情读取忽略`dependency_of` JSON反序列化及terminal `rows.Err()`，下载把任意`QueryRow.Scan`失败都解释为410；Worker的pending查询、逐行Scan、游标终态、过期/耗尽更新、report项读取、完成计数、retry/terminal状态写回、`RowsAffected`及通知错误存在多处`continue`或赋给`_`。数据库/持久JSON故障因此可能产生部分报告、错误的“已过期”，或让周期处理静默停摆；竞争状态还可能被当成已成功写回。
- 详情与下载现在只把`pgx.ErrNoRows`解释为明确不存在；其他查询、Scan、JSON和行流错误记录上下文并返回500。真实任务存在但已过期/非ready仍使用410，成功DTO、错误code和前端调用不变，不用空数组或gone掩盖内部故障。
- `scan`在首次及每次tick记录`processPending`返回的结构化错误。pending、expired和exhausted路径先完整收集并关闭row stream，再处理/通知，避免小连接池下边迭代边发起新查询；Query、Scan、`rows.Err()`、事务、状态更新、`RowsAffected`及通知任一失败均带任务上下文返回。确实没有任务仍是正常nil。
- report装配遇单项Scan或terminal cursor失败调用lease绑定的`fail`写入`REPORT_LOAD_FAILED`，不再跳过损坏项后生成部分`.mrpack`。任务初始读取一次带回已有计数，删除无错误检查的完成计数补查。ready写入继续要求processing/building及当前lease恰好更新一行；通知失败也向循环暴露。
- `fail`先以当前lease读取attempt，再要求同一lease完成retry或terminal CAS并检查恰好一行；状态竞争、错误lease、数据库故障和通知故障均与原始业务错误通过`errors.Join`返回，绝不假称持久失败状态已经成功。终态通知从`UPDATE ... RETURNING owner/name`取得事实，避免写后再做可漂移读取。
- 真实PG用会话临时表证明：标量`dependency_of`令详情500；非法`file_size`令report Scan失败、任务写回`pending/retry_wait/REPORT_LOAD_FAILED`并清lease；trigger拒绝retry时原始错误和持久化错误同时返回且任务保持processing/building；错误lease返回明确错误；临时重命名`favorite_modpack_export_tasks.created_at`使`processPending`返回查询错误。无DDL/generation或前端迁移，保持142且远端public未访问。
- ARCH-018按原Medium/P1关闭。OPS-013仍负责OSS上传成功但ready更新失败后的孤儿补偿，BUG-069仍负责完整结果一致性，DEAD-011仍负责未消费report snapshot，TEST-034仍负责Handler/NATS/OSS/过期下载完整矩阵；本项不借错误可见性重复关闭这些责任。回滚会恢复部分报告和伪状态，不保留吞错兼容路径。

## D-197：蓝图派生详情必须完整装配，只有权威no-row事实可以表示内容缺失

- 旧`blueprintMaterialRows`虽已由PERF-043把revision查询限定到当前namespace，但仍只在Query成功时尽力装配，逐行Scan与terminal cursor错误被跳过，材料`properties` JSON反序列化赋给`_`，批量`resolveExportResources`错误也赋给`_`。因此数据库或持久shape故障会把名称回退原始block ID、清空来源/版本或形成部分材料，同时返回200。
- revision读取现在对Query、每行Scan和`rows.Err()`分别带上下文返回，并在进入resolver前显式关闭游标；材料properties非对象/值类型错误失败关闭。批量resolver失败向详情传播，合法找不到单个资源仍沿既有fallback显示block ID，这是与系统故障不同的业务缺失。
- `blueprintAssetRevisions`改为`([]map[string]any,error)`，Query、Scan及terminal cursor任一失败返回nil/error；唯一详情调用方在写200前检查并返回稳定500。无单行跳过、部分数组或空数组降级，健康排序和DTO字段不变。
- cover Query仅`pgx.ErrNoRows`返回204；数据库/Schema/Scan故障和成功行中的空object key均500。同入口复核发现normalized render-data先用`ErrNoRows || objectKey==""`判断会让数据库错误因零值key伪装404，遂调整为先区分no-row、再处理error、最后检查成功空key；真实未生成render数据继续404。
- 新`logBlueprintReadFailure`按module、public ID、stage和底层error结构化记录详情、variants、materials、asset revisions、required mods、cover和render-data失败，响应不泄露数据库细节。头像OSS URL及对象读取继续沿既有502/503边界，下载variant的Query早已正确区分404/500，本项不改这些合同。
- 单连接真实PG用临时表证明：无cover为204、空normalized key为404；临时重命名`oss_files.status`与`blueprints.normalized_object_key`分别稳定500；缺少resolver所需列返回带上下文error；标量properties拒绝；缺asset relation令详情500且不输出部分DTO。最初全Blueprint门禁发现既有Outbox集成临时`blueprint_jobs`夹具缺当前预算查询所需`status`列，只补齐测试默认queued后全套通过，未改变生产语义。
- 无DDL、数据、generation或前端迁移，保持142；远端public未访问。ARCH-019按影响范围定为Medium/P1并关闭。PERF-043规模项已独立关闭；SEC-027、BUG-075..078、OPS-014/015、PERF-042和TEST-036的权限、状态机、租约、补偿、资源预算与完整系统矩阵继续开放。回滚会恢复部分详情和伪缺失，不保留降级开关。

## D-198：OSS读取必须区分缺失与故障，清理先持久再执行且防晚登记

- 旧OSS文件目录遍历后不检查`rows.Err()`，连接中断可能返回部分200。精确哈希复用、蓝图SHA复用、完成上传恢复及举报证据幂等查询又把任意Query/Scan错误解释为不存在，数据库事故会继续签名、上传或登记并撞出重复事实。现在目录在响应前检查terminal cursor；四类lookup只有`pgx.ErrNoRows`返回missing，其他错误带上下文传播并由HTTP稳定500。
- PERF-044已用generation127原子额度桶关闭审计所述历史SUM与并发放开，本项不重做额度模型。上传日志、同步扫描日志和下载统计仍是允许降级的运维事实，但每次写失败现在按`upload_log/scan_log/download_stat`写结构化日志并递增进程原子计数；管理员基础设施响应新增`ossWrites`快照，系统不再无信号丢弃。
- 旧验证、图片处理、额度或登记失败直接调用provider `DeleteObject`并忽略结果。现在全部请求期未登记对象清理只调用`enqueueUnregisteredOSSObjectDeletion`：在独立于请求取消的15秒context中事务核对object key尚未登记，再把完整bucket/endpoint/region/key及reason写入既有`oss_object_deletion_outbox`，nullable `oss_file_id`区分补偿对象，唯一键保证重复请求只有一个任务。enqueue故障同样进入结构化日志和`deletion_enqueue`计数。
- 删除Worker claim同时取得nullable file ID；补偿对象在真正调用provider前再次查询是否已有非deleted `oss_files`行。commit结果不确定或排队后晚登记时任务直接成功而不删对象。未登记对象继续使用既有租约、有限重试、failure class、dead状态和管理员审计重放。迁址复制后的失败路径先显式回滚开放事务再另起enqueue，避免单连接池自锁；commit失败也依赖执行期二次保护处理“不知道是否已提交”的窗口。
- 单连接真实PG证明健康/明确缺失查找语义、三类运维写故障各计一次、重复清理只有一个Outbox任务、父请求已取消仍可靠入队、排队后登记使Worker在加载凭据/访问provider前返回、故意破坏Outbox列时enqueue failure计数可见。源码门禁保证Handler/Admin无直接`DeleteObject`残留并锁定error返回签名与独立context。
- 无DDL、数据回填或generation变化，保持142；复用既有Outbox nullable FK和唯一约束。ARCH-020按影响范围定为Medium/P1并关闭。BUG-079 uploading蓝图到期回收、BUG-080举报证据事务幂等、OPS-018 multipart完整生命周期及TEST-037真实OSS矩阵继续独立开放。回滚会恢复数据库故障伪缺失、无信号审计丢失和不可重试删除，不保留直接删除兼容分支。

## D-199：生成对象只有在数据库登记成功后才是业务文件，失败清理必须成为可靠任务

- `writeGeneratedOSSObject`是后台生成文件的共享边界，当前只有收藏夹MRPack导出和已净化sticker衍生图两个调用方。旧实现先成功PUT随机object key，再单独upsert `oss_files`；upsert失败后直接调用一次provider DELETE。只有DELETE也报错时才把两个error联合返回，但没有Outbox行、租约、重试或管理员可见事实，进程中止和瞬时provider故障都会留下数据库不可见对象。
- ARCH-020刚建立的未登记对象删除边界现在成为唯一补偿路径：生成文件登记失败后，以reason `generated-object-registration-failed`在脱离父请求取消的15秒context中核对未登记并写nullable-file deletion Outbox。请求/Worker不直接执行DELETE；持久任务由既有删除Worker按provider target、lease、有限重试、failure class、dead和审计重放处理，并在实际调用provider前再次检查晚登记。
- 登记error先包装为`record generated OSS object`，即使Outbox排队成功也原样向调用方返回，因此MRPack任务仍进入既有`OSS_UPLOAD_FAILED`处理、sticker事务仍失败，不会把“已安排删除”伪装成业务成功。若enqueue本身失败，helper先增加`deletionEnqueueFailures`并记录结构化日志，writer再用`errors.Join`同时返回登记与`queue generated OSS cleanup`错误；调用方可见补偿缺口，而不是只看到最初数据库错误。
- fake OSS接受三次PUT但把任何旧DELETE设为503；单连接真实PG以trigger只拒绝两个目标的`oss_files`登记。健康控制得到file ID；第一失败目标产生精确一条`oss_file_id is null` pending任务且provider DELETE为零；再临时破坏Outbox reason列，返回同时包含登记trigger与排队错误，失败计数精确1。源码门禁保证共享writer没有直接Delete残留。
- 无DDL、数据回填、generation、成功DTO或前端迁移，保持142并复用现有Outbox。ARCH-021按影响范围定为Medium/P1并关闭。OPS-013仍负责生成文件已登记但MRPack task ready/CAS失败后的文件生命周期，TEST-037仍负责真实供应商、重启和完整OSS矩阵；本项不以登记补偿替代后续业务绑定原子性。回滚会恢复一次性DELETE和不可重试孤儿，不保留旧分支。

## D-200：评论列表只有完整成功的row stream和总数事实才能返回

- 审计时根评论、回复和watch循环在`rows.Scan`失败时`continue`，关闭游标后不看`rows.Err()`，根COUNT错误又被当0；watch再逐项读取评论和目标，查询错误与明确不可见同样被跳过/降级。PERF-045的generation128 keyset/counter重构和PERF-046批量装配已删除这些旧生产路径：三类page query都在逐行Scan失败时关闭并500，terminal cursor在任何装配前500，target total为`(int64,error)`且handler检查。
- 共享`queryCommentItemsWithQueryer`现在对主评论Query、每行Scan和terminal rows逐层返回nil/error；上层的在线状态、头像OSS解析、权限和附件装饰也逐层传播。watch只调用一次该装配，再调用一次`queryCommentTargetsByInternalWithQueryer`；批量目标的Query/Scan/rows错误返回nil/error并由handler 500，不再以系统错误作为跳项控制流。
- 本项保留明确业务缺失与故障的边界：被拉黑作者导致评论不在可见集合、未知类型或对当前用户不可见的target identity不进入map，这些仍是成功查询得到的缺失事实。当前watch的type-only target fallback及子路由可见性是否允许返回正文由SEC-031负责，本项不借数据库错误修复改变授权产品规则或提前关闭该安全Finding。
- 为把此前仅由生产源码承诺的故障语义变成可执行验收，新增专用闭集测试检查根/回复/watch的Scan、rows.Err、close及500顺序，并向评论装配和17类target批解析注入Scan错误与“一行后中断”terminal error，均要求nil/error且rows关闭。visible total抽出接受`commentTargetQueryer`的内部入口，fake row数据库错误返回0/error、健康值精确42，禁止故障伪零。
- generation142完整会话临时Schema再次编译全部17类target UNION分支并通过。无DDL、数据、generation、成功DTO或前端迁移；只增加内部可测试queryer边界。ARCH-022按影响范围定为Medium/P1并关闭。PERF-045/046已分别关闭规模责任；SEC-031目标授权、SEC-033 thread预算、BUG-083删除附件、BUG-085逐项能力和TEST-038完整权限/生命周期矩阵保持独立。回滚会删除专用故障门并恢复总数不可注入边界，不提供兼容收益。

## D-201：收藏membership只有完整成功的有界row stream才能返回

- 审计定位的旧`favoriteMembership`无界GET在逐行`Scan`失败时跳过且没有检查terminal `rows.Err()`，可能返回部分collection ID。PERF-051已通过有效双端RED删除该路由和handler，并用最多100目标、单SQL的POST membership summary替代；因此本项不伪造已不存在生产函数的新RED，也不恢复旧接口只为测试。
- 当前同一业务边界包含私有/公开收藏夹keyset页、收藏项keyset页和membership summary。三类生产查询现在分别交给`collectFavoriteCollectionPageRows`、`collectFavoriteItemPageRows`和`collectFavoriteMembershipSummaryRows`；collector逐行Scan、检查terminal cursor、始终关闭rows，并在任何故障时返回nil或零值加error。handler只在完整成功后装配cursor/metadata/summary并写200，故障统一500。
- 专用fake rows为三类collector分别注入首行Scan错误与成功一行后的terminal错误，要求结果不可部分使用、error非nil且rows已关闭；源码合同同时锁定旧GET/handler保持删除、当前三个handler必须检查collector错误后500。该验收补齐此前仅由生产循环实现保证、但没有独立故障矩阵的ARCH-023责任。
- PERF-051的一百万收藏夹和一百万不同收藏项真实PG handler测试再次通过，证明抽取collector没有恢复无界响应、集合1+N、同步COUNT或深OFFSET，现有2/2/1 SQL和响应预算保持。健康cursor、summary DTO及第一方前端调用完全不变。
- 无DDL、回填、generation、公开API或前端迁移，保持142。ARCH-023按原Medium/P1关闭；ARCH-024继续独立负责create/update/delete写入的409/404/5xx分类，BUG-089负责旧PUT无效collection ID原子语义，SEC-036、LEGACY-018、STYLE-006和TEST-041继续开放。回滚会移除集中故障验收并重新把严格性分散进handler，不提供兼容收益。

## D-202：收藏夹业务冲突只能来自已识别约束或明确NoRows

- 旧create对任何insert/returning错误都返回重名409；update只识别unique，其余QueryRow错误一律404；delete用Exec并把`err != nil || RowsAffected()==0`全部折叠为默认集合/不存在409。真实PG以BEFORE trigger抛SQLSTATE `XX000`，精确复现三类操作分别返回409、404、409；数据库中断、Schema漂移或编码错误因此会误导客户端停止重试并避开5xx告警。
- create现在先识别`isUniqueViolation`并保持既有409，其余错误结构化记录后500。update保持unique 409，再只对`errors.Is(err, pgx.ErrNoRows)`返回404，其余500。约束分类继续依赖既有SQLSTATE helper，不用字符串匹配数据库文案。
- delete改成`DELETE ... WHERE ... AND NOT is_default RETURNING public_id`的单一权威QueryRow：目标属于当前用户且非默认时204；默认、跨用户或不存在都形成明确`pgx.ErrNoRows`并保持既有409；SQL执行、trigger、Scan或连接错误不再与0行合并，而记录并500。无需先查后删，也不引入TOCTOU窗口。
- 统一`logFavoriteCollectionWriteFailure`记录`module=favorite`、operation、collection ID、user ID及底层error。公开响应仍只暴露稳定通用文案；日志保留SQLSTATE/driver error供监控定位，不把内部信息泄露给客户端。
- 单连接真实PG临时表覆盖健康create201/delete204、重复名称create/update409、update missing404、default/missing delete409和三类XX000=500；源码合同锁定每个分支与结构化字段，Favorite广域及双端全仓门禁通过。无DDL、generation、DTO或前端迁移，保持142且未访问远端public。
- ARCH-024按原Medium/P1关闭。BUG-089仍负责旧membership PUT的无效collection ID事务语义，SEC-036负责存量额度，LEGACY-018、STYLE-006与TEST-041继续开放。回滚会恢复操作故障伪业务错误，不保留兼容开关。

## D-203：社区目录、引用和翻译结果必须由同一套完整数据事实形成

- 审计时社区目录把COUNT Scan错误当total0，并可跳过目录/引用数据错误。PERF-052已用有效双端RED删除同步COUNT、正文和OFFSET，当前keyset目录已检查Query、每行Scan和terminal cursor；ARCH-025不恢复旧路径，而给三阶段加统一`module=community_post`、stage、identity、error结构化日志，并以百万目录回归确认窄投影和固定页预算保持。
- 资源引用批装配仍把`localized.names/snapshot.names` JSON反序列化赋给`_`。现抽为接受queryer的`loadCommunityPostResourceReferencesWithQueryer`，严格检查Query/Scan/JSON/rows并始终关闭游标；标量JSON不能再产生Names=nil的正常引用。批量项目可见性和SEC-037既有边界不变。
- 翻译请求的source Query故障原与无效请求一起400，缓存Query故障又被当未命中并继续扣额/排队。现在明确source NoRows仍400、cache NoRows仍正常入队；其他故障分别记录`translation_source`/`translation_cache`并500，避免数据库事故创建重复付费任务。
- 结果读取过去把任意task Query错误当404；completed任务忽略payload JSON错误，并只在翻译Query恰好成功时附加translation，否则仍返回completed 200。现在任务只对NoRows或owner不匹配404；损坏payload、无效ID/locale/revision、缺失translation及任何数据库故障均在成功响应前按task/payload/result阶段记录并500。共享decoder同时用于Worker持久化，并把来源revision Query错误与真实revision变化分开。
- 更新入口的主行读取现在只把NoRows解释404，数据库故障500。apply/状态更新错误与`tx.Commit`不再写为`if err != nil || tx.Commit()!=nil`，而分别记录`update_apply`和`update_commit`后500；因此commit确实执行时的错误不会被短路或丢失，成功响应仍只在commit后产生。
- 单连接真实PG证明标量reference names失败，健康completed任务返回translation，标量payload和缺translation均500，owner/missing仍404，临时改列任务查询500。无DDL、generation、DTO或前端迁移，保持142且未访问远端public。ARCH-025按原Medium/P1关闭；ARCH-026、BUG-090/091和TEST-042继续按通用AI持久化、并发、语言及完整系统矩阵独立开放。

## D-204：AI任务必须先形成可用业务结果，再以受状态约束的CAS完成

- 旧Worker在provider返回后立即把`ai_tasks.status`写为completed，随后notification persistence为void且忽略payload/result类型和upsert error；content scope JSON也赋给`_`。即使后续community/catalog持久化会调用`failTask`，任务仍短暂或永久呈现completed，notification失败则始终保持completed空结果。
- Worker现在先检查result JSON可编码，再按task type持久化业务结果。notification payload要求有效notification ID和站内locale，result要求非空items、逐项object/string key/string text、只含title/body且不重复；持久upsert返回error。content scope JSON必须可解码，只允许空catalog scope或`community_post`，两类持久化继续利用既有revision幂等/唯一upsert。
- catalog/community持久化也改用共享严格items decoder；错误item类型、未知/重复key或空业务结果不能继续。catalog payload抽为共享decoder，结果GET与Worker使用相同entity/public ID/source/target/revision校验；community继续要求title和bodyMarkdown均存在。
- 只有业务持久化成功后，Worker才以`where id=? and status='running'`写result/token/cost和completed，并检查`RowsAffected()==1`。业务解析/写入或完成CAS失败调用返回error的`failTask`；failed更新同样只允许running且检查SQL错误与恰好一行，失败状态本身写不下时通过安全结构化日志和联合error暴露。业务写与完成CAS之间的崩溃窗口由既有stale-running恢复及幂等business persistence补偿；OPS-019继续负责更广的failed/retry政策。
- notification result GET删除整段重复`notification_translations` upsert，严格解码已存payload/result后只返回translation；读取不再更新`created_at`或用GET修复Worker遗漏。catalog result同样对completed payload/result/item损坏记录task stage并500，running状态不提前解析未完成数据。
- 单连接真实PG验证健康catalog result、三类损坏completed=500、running=200、owner边界；notification健康upsert、payload/item/upsert故障返回error，结果GET健康/三类损坏及2020时间戳不变；failed状态成功写入原始原因，XX000 failed-state trigger错误传播。无DDL、generation或健康DTO变化，保持142且远端public未访问。
- ARCH-026按原Medium/P1关闭。OPS-019的历史queued/failed恢复、BUG-094 per-task并发、LEGACY-019旧入口和TEST-043真实供应商/审核/恢复完整矩阵保持独立。回滚会恢复伪completed与GET补写，不保留兼容开关。

## D-205：经济与任务页面只接受完整row stream和object-shaped持久配置

- 审计时经济总览在余额循环结束后不检查`rows.Err()`，经验和时区QueryRow结果直接赋给`_`；check-in历史又把任意错误解释为从未签到。因此Schema漂移、连接中断或数据库事故都能形成余额截断、经验/等级0、空时区和可签到假象并返回200。现在余额由`collectEconomyBalances`完成Scan、terminal cursor及close；经验仅明确`pgx.ErrNoRows`保留0，时区任何读取故障500；check-in helper返回第三个error且只把NoRows当未签到。
- `economyConfigFromSettings`过去把查询、JSON解码和语义规范化的所有错误退回默认签到规则，后台配置和总览都无法区分“尚未配置”与“配置损坏”。现在loader保持NoRows→默认，但数据库错误、`null`/数组/标量及无效规则均向调用方传播；两个handler在成功响应前500。签到和下载奖励的事务消费方本来已直接调用严格loader，语义保持一致。
- 新共享`decodeStoredJSONObject`要求持久文档非空、可解码且顶层为object；既有Mod export object helper也委托该实现，避免新增第二套shape规则。currency translations、shop translations/config、user/admin task translations都使用它并带实体上下文返回error；condition/rewards继续复用ARCH-014的严格任务配置validator。使用热度商品时还先验证config object再解码typed字段，损坏配置不能回退默认功率。请求侧JSON marshal结果也全部检查，货币流水metadata编码失败阻止写入。
- 后台活动列表抽为`collectAdminActivityEvents`，逐行Scan或terminal cursor错误均返回nil/error并关闭rows，handler只在完整成功后写列表。同行复核还把库存QueryRow从`err != nil || quantity<=0`拆开：NoRows/真实零库存仍409，数据库故障改500。
- fake rows分别注入余额和活动terminal failure并验证error/close；纯函数矩阵拒绝空、null、数组、字符串、数字和坏JSON，仅接受object；配置reader证明只有NoRows默认。显式本地127.0.0.1:55432的generation142隔离Schema进一步证明三张总览依赖表不可用、null经济设置/currency/shop/task JSON均500，健康与明确缺行保持200。
- 无DDL、数据、generation、成功DTO或前端迁移，保持142。ARCH-027按原Medium/P1关闭；PERF-056、TEST-044、ARCH-032和OPS-020继续分别负责等级重算规模、完整经济授权行为、渲染布局JSON与维护调度。回滚会恢复数据库故障伪零/伪空和损坏配置默认，不保留兼容开关。

## D-206：站点更新列表和详情只返回完整、typed的翻译事实

- 审计定位的公开与后台旧列表在迭代后不检查`rows.Err()`，后台又把聚合JSON原样塞进`RawMessage`。PERF-058已用有效双端RED删除旧OFFSET/固定100窗口，当前generation134 date-ID keyset页在Query、Scan、terminal cursor和后台JSON上已有严格处理；ARCH-029不恢复旧路径，而以专用fault injection把这些约束变成独立验收并修复同根因的后台详情残余。
- 公开与后台当前页分别委托`collectPublicSiteChangelogRows`和`collectAdminSiteChangelogRows`。两个collector逐行Scan、检查terminal cursor、始终关闭rows，任一故障返回nil/error；页helper只有获得完整`limit+1`结果后才计算hasMore/cursor。fake rows覆盖首行Scan失败和terminal failure，证明不会泄露部分items。
- `decodeSiteChangelogTranslations`先复用共享`decodeStoredJSONObject`拒绝空/null/数组/标量，再解码为`map[string]siteChangelogTranslation`，因此错误value类型也失败。后台列表和新`loadAdminSiteChangelogDetail`使用同一typed入口；详情Handler删除`json.RawMessage(translations)`，成功JSON shape不变但损坏聚合不再穿透编码器。
- queryer详情loader只把`pgx.ErrNoRows`保留给404；其他查询、Scan和JSON错误传播。真实PG首次健康控制还发现DATE binary不能稳定Scan进`*string`，现统一Scan到`time.Time`再格式化`YYYY-MM-DD`，与列表及既有DTO一致。
- public/admin page与admin detail在500前调用`logSiteChangelogReadFailure`，记录`module=site_affairs`、stage、scope/public ID及底层error，响应继续使用站内稳定文案且不泄露Schema。会话临时表健康三handler均200，临时重命名translation正文列后三者均500并输出精确stage。
- PERF-058的一百万changelog和一百万translation真实PG回归再次证明两路深页各一SQL、命中既有public/admin索引且cursor可达性不回退。无DDL、数据、generation、成功DTO或前端迁移，保持142。ARCH-029按影响范围定为Medium/P1并关闭；BUG-101写并发和TEST-046广域站务行为保持独立。回滚会恢复unchecked详情和分散循环，不提供兼容收益。

## D-207：治理页与举报详情必须由完整row stream和可解码快照形成

- 审计时本人举报、后台举报和小黑屋循环结束后不检查`rows.Err()`，后台详情又复用会吞错误的`querySimpleRows`。PERF-060已经用有效双端RED删除三条OFFSET旧循环并在generation136 keyset页检查Scan/terminal cursor；ARCH-013也已经把`querySimpleRows`改成`items,error`且当前举报详情四个调用方均检查。因此本项不恢复旧路径，而关闭仍存在的快照吞错/日志缺口，并为已有严格性建立独立fault-injection验收。
- 三个当前页helper分别委托`collectOwnReportRows`、`collectAdminReportRows`和`collectBlackroomRows`。collector逐行Scan、检查terminal cursor、始终关闭rows，任一故障返回nil/error；只有完整取得`limit+1`结果后才裁页并生成cursor。fake rows为三路分别注入首行Scan错误和terminal failure，证明不会泄露部分治理事实。
- 后台举报详情此前在有`report.snapshot.view`权限时执行`_ = json.Unmarshal(raw,&result)`；损坏或JSONB `null`会返回200和`snapshot:null`。新`decodeReportSnapshot`复用共享`decodeStoredJSONObject`，要求非空object，解码失败在读取证据/审核/动作之前500。无快照权限时仍不暴露快照，授权产品边界不变。
- `logGovernanceReadFailure`统一记录`module=governance`、stage、scope或举报ID和底层error。三类页的Query/Scan/terminal错误、详情主查询非NoRows错误、snapshot解码，以及evidence/reviews/actions/related的严格simple-row错误都在500前记录；只有主举报明确NoRows仍404。
- 单连接真实PG会话临时表证明本人/后台/小黑屋/详情健康均200，JSONB null快照500，临时改坏reports和ban列使三类列表500，改坏review列使详情simple-row阶段500。PERF-060的一百万举报和一百万封禁回归仍证明三路深页各一SQL、命中generation136索引且并发稳定性不回退。
- 无DDL、数据、generation、成功DTO或前端迁移，保持142。ARCH-030按影响范围定为Medium/P1并关闭；PERF-060和ARCH-013的规模/共享helper责任不重复关闭，TEST-048广域举报、证据、审核、处置和封禁行为保持独立。回滚会恢复快照伪空和缺少分阶段告警，不提供兼容收益。

## D-208：封禁理由的加载、合法空目录与故障必须是三种不同状态

- 审计定位的`useBanReasons`只返回数组：成功写`value.items`，任何权限、网络或后端故障都catch后写`[]`。举报审核面板因此仍渲染可选空理由并允许进入后端通用错误，封禁创建表单则表现得像站点尚未配置理由；操作员无法判断应该配置目录还是重试系统故障。
- hook现在返回`BanReasonLoadState{items,loading,error,reload}`。每次请求以locale、token和显式attempt组成代次，结果只在自身request key下可用；scope切换或retry会立即派生loading且不显示旧items/error，AbortController取消过期网络请求。失败保留`apiRequest`错误消息，成功空数组保持`loading=false,error=""`，语义不再折叠。
- 两个调用方复用`BanReasonSelect`：加载状态以`role=status`展示，失败以`role=alert`展示本地化说明、底层错误和Retry按钮。selector在未知状态禁用；举报面板同时禁用ban user/custom reason输入，但仍允许只提交审核结论；专用封禁表单禁用创建按钮，避免把故障状态作为空reason提交。
- retry只增加attempt，不修改token/locale或制造旧请求竞争；hook使用派生request-key loading而不在effect中同步setState，满足React hooks lint并防止级联渲染。卸载或scope变化会abort，已中止请求不会写error。
- 新源码合同以旧catch→`setItems([])`得到有效RED，并锁定显式状态、retry、可访问错误UI、禁用边界和中英主locale；治理分页既有测试共同保证两面板未回退cursor协议。全量195前端tests、TypeScript、ESLint和58页production build通过。
- 无后端Schema、API、DTO、generation或数据迁移，保持142。ARCH-031按影响范围定为Medium/P1并关闭；TEST-048继续负责广域举报/封禁/权限/失败系统矩阵。回滚会恢复故障伪空和不可重试UI，不提供兼容收益。

## D-209：合成表对象字段在导入和公开渲染两端都必须严格验证

- 审计定位的`decodeJSONObject`忽略`json.Unmarshal`错误并总能返回map；JSONB `null`、数组或标量因此在公共render中变成空对象。原始fingerprint仍能区分这些值，但API语义被折叠，调用方既看不到损坏也无法区分来源。JEI导入的RawMessage字段又只用`nonEmptyJSON`补省略值，没有拒绝显式非对象，故错误可从导入一路进入持久层。
- 新`validateOptionalRecipeJSONObject`只允许字段真正省略时继续使用既有默认；任何显式值必须通过共享`decodeStoredJSONObject`。模板collection的canvas/image_pixels/content、全部slot rect/visual_rect，以及配方parameters和每个binding chance_texts均在decode后验证；queue入口再次验证，覆盖内部promotion或测试构造对象绕过byte decoder的路径。
- 公共渲染删除无错误返回的decoder。`decodeRecipeJSONObject(raw,label)`要求非空合法object并保留字段上下文；权威recipe/slot/binding/candidate definition与导入parameters、canvas、image_pixels、content、slot data/rect/visual_rect、candidate chance_texts的每个调用点都在组装成功DTO前检查并传播错误。可选layout override仍把明确JSON `null`解释为没有覆盖，其余显式值使用同一严格边界。
- 单元矩阵覆盖null、数组、字符串、数字及无法存入JSONB的坏语法；单连接真实PG会话临时表对七类历史对象列各注入四种合法jsonb非对象值，共32组都使hydrate返回error。恢复合法object后parameters、唯一slot及candidate均完整保留，证明没有以失败关闭为名破坏健康render。
- 无DDL、数据、索引、generation、成功DTO或前端迁移，保持142。ARCH-032按影响范围定为Medium/P1并关闭；畸形导入从接受改为拒绝，历史损坏存储从伪`{}`改为5xx，不保留兼容回退。回滚会再次隐藏数据损坏并使fingerprint与API语义分裂，无兼容收益。

## D-210：资料板块只有空根创建、整树布局更新和子树归档三种写权威

- 审计定位的单板块PUT仍能改变`VersionPublicID`，发布器随后只更新目标section的version_id；根节点没有父版本trigger检查，既有孩子和placement不会原子迁移，故可永久形成跨版本树。相同旧循环又按nullable `game_resources.owner_mod_id`而不是已建立的`mod_resource_bindings`判断资源，和layout给出相反合法性结论，并允许最多2万资源逐项SQL。
- 仓库调用图确认第一方前端没有该PUT：新增类型只POST一个parent为空、resources为空的根，结构/placement由PATCH `/layout`修改，归档走DELETE。当前开发期也没有已发布外部客户端责任证据。因此不建立旧名转发或继续维护第二套校验，而是删除PUT pattern、section edit handler分支和edit snapshot提交点。
- POST规范化现在明确拒绝parent或任意resources，只能创建空根。section发布case明确要求`operation=create`及空parent/resources，按snapshot目标version解析后，以`public_id+mod_id+version_id+status=pending FOR UPDATE`锁定已预留行；激活UPDATE不再写version_id。这样部署前遗留edit快照或篡改/陈旧跨版本create快照都在任何修改前失败。
- 创建发布仍允许模板、显示模式、ordinal、本地化和item_block系统分类，因为这些是空根初始化责任；资源循环和owner_mod判断被整体删除。后续所有资源通过layout的批量查询按`mod_resource_bindings`解析，DELETE继续调用完整子树归档事务。
- 有效RED同时观察到PUT命中认证层返回401、带parent的POST模型可规范化，以及真实PG旧edit把active根从version1改到version2。修复后PUT 405，parent/resources拒绝，edit与版本不匹配create都返回error且version保持1；健康同版本pending空根激活并保存本地化。全部ModContent和双仓门禁通过。
- 无DDL、数据迁移或generation变化，保持142。BUG-025按持久数据不变量破坏定High/P0；BUG-026和LEGACY-007按规则分裂/重复写权威定Medium/P1并一并关闭。回滚会恢复跨版本破坏与两套归属规则，无兼容收益。

## D-211：row stream完整结束是成功响应、任务claim和外部副作用的共同前置条件

- 审计点名的列表、关联装配、探测和通知循环中，5条路径已由先前Finding补上terminal检查；当前仍有21条真实残余：资料内容4条、简单项目5条、整合包6条，以及关注、审核事件、服务器探测、项目更新任务/收件人和广播收件人各一条。有效源码RED逐函数枚举这些循环，避免用全局字符串计数误判。
- 新共享`finishRows(checkedRows)`在成功循环边界先读取`Err()`再关闭rows；Scan失败的调用点仍立即关闭并返回。所有HTTP读取在写JSON前完成该边界，简单项目/整合包的每个关联子查询分别闭合，不能把后半段中断解释为较短的合法集合。
- 服务器探测claim从裸DML stream改为显式事务：完整Scan并检查terminal error后才commit，commit后才启动goroutine。真实PG用可查询但不能Scan进`int64`的ID触发旧路径已更新`next_probe_at`的缺陷，修复后事务回滚且时间戳逐值不变。
- 项目更新Worker把pending读取错误返回给调度日志，任务内收件人必须完整收集后才能render/write/commit；广播邮件同样先完整收集用户ID再逐个入队。Scan或terminal故障不再产生一部分通知后假装成功。审核事件读取则在enqueue之前闭合，避免部分section事实进入持久事件。
- 该项不把业务NoRows改成错误，不改变健康DTO、cursor、模板、调度周期或探测结果。无DDL、数据迁移、generation或前端协调，保持142；BUG-027按跨公开响应与持久后台任务的系统性错误解释定High/P1。回滚会恢复截断成功、漏任务和已claim未执行窗口，不保留降级开关。

## D-212：整合包主分类由一个后端注册表同时约束请求、过滤和数据库

- 审计定位的旧规范化只trim PrimaryCategory并为空值默认`adventure`，随后只验证Tags；因此`orphan_category`完整通过共享规范化，HTTP继续进入数据库，直接SQL也能写入。目录primary参数已使用`allowedModpackCategories`拒绝该值，前端又只有固定16项option/i18n，三端对同一持久事实给出矛盾解释。
- 新`internal/catalogpolicy`封装16个产品分类并只返回副本。HTTP的allowed map从该注册表生成，创建、修订和导入草稿共用的规范化在默认处理后调用`IsModpackCategory`；非法值在任何数据库或asset操作前稳定400，空值仍规范为合法`adventure`。
- generation143把同一注册表转义并生成命名`modpacks_primary_category_check`，避免再维护另一份SQL枚举。数据库测试既比对生成约束，也完整安装临时Schema，逐一直接插入16类并断言未知值返回constraint name准确的23514；因此内部未来旁路也不能产生孤儿分类。
- 当前Schema策略明确不升级旧development generation：142及更早开发库需重置，不做猜测性回填、双读或兼容无效值。测试只在本机55432单连接会话临时namespace安装generation143并完整删除，未迁移或写入本机public/远端数据库。
- 健康请求、DTO、目录primary/tags过滤、前端16项option和i18n均不变；无索引或查询计划变化。BUG-029按持久分类完整性和用户可达性定Medium/P1。回滚需同时退回应用与CHECK并会重新允许孤儿值，不提供兼容收益。

## D-213：服务器模组声明与机器探测证据必须是两种不同协议类型

- 旧`createServerModRequest`同时包含ID/version和source/confidence，创建及更新的严格JSON decoder因此合法接受客户端`forge_status`、`configuration`、`agent`与`exact/high/inferred`。规范化完全不触碰这些字段，insert又把它们当受信任枚举保存；有效RED证明伪造PATCH越过decode并继续访问数据库。
- 写DTO现在只有`id`和可选`version`，内部另设无JSON标签的`trustedServerModEvidence`。全局`decodeJSON`的unknown-field策略使旧source/confidence立即400，不能以“忽略但继续成功”掩盖调用方错误。前端同步引入`ServerModDeclaration`，probe/detail继续使用含证据的`DetectedServerMod`/`ServerMod`，提交序列化只复制ID和探测到的可选version。
- 创建时服务端本来就会重新探测；现在只有该`serverprobe.Result`先转换为trusted evidence，再与用户声明合并，重复ID永远以探测来源优先。调度探测也直接走相同trusted路径，持久helper不接受写DTO并仍收紧未知内部来源/置信度。
- 更新不能信任详情响应被客户端原样回传，也不应无故把真实机器证据降级。Handler已持有服务器行锁，故先用所选规范ID通过`unique(server_id,raw_mod_id)`读取source非manual的既有行，完整检查row stream，再删除/重建；仍选中的可信证据保留，新增/普通项固定manual/declared，被取消项按既有编辑语义删除。
- 无DDL、数据迁移、回填或generation变化，保持143；真实本机PG测试全程事务rollback，证明configuration/inferred持久化、更新选择保留及新声明manual/declared。旧客户端发送证据字段会400，第一方已原子迁移；不保留伪造兼容层。BUG-030按持久公开证据真实性定High/P1，BUG-031的多来源快照替换仍独立。

## D-214：完整服务器探测替换机器快照，而不是累积历史并集

- BUG-030关闭客户端伪造后仍只有`unique(server_id,raw_mod_id)`一行：同一模组的manual声明和configuration/forge/agent观察会互相覆盖，调度器又无论`ModListComplete`都只upsert。因此无法在完整探测中删除消失的机器事实而可靠保留声明，详情、包含模组筛选和未解析队列会长期成为历史并集。
- generation144把`minecraft_server_mods`收窄为(server,raw mod)身份；新`minecraft_server_mod_evidence`以`primary key(server_mod_id,source)`独立保存manual及三类机器来源、version、受来源约束的confidence和observed_at。父行仍持有解析到`mods.id`的身份及既有未解析引用，故目录连接、筛选和解析工作流不需要重复多行。
- 调度持久化先通过`minecraft_servers`更新取得与编辑相同的server行锁。`ModListComplete=true`时在同一事务删除该server全部非manual evidence、upsert本次规范化机器集合，再删除没有任何evidence的父身份；既有delete trigger同步移除其unresolved reference。`false`时只upsert观察到的机器来源，绝不把部分清单当撤销依据。
- 编辑已持有server行锁，现在删除并重建的仅是manual evidence；机器来源和未选择但仍被探测到的身份保持不变。创建同时写重新探测结果和声明，即使ID相同也保留两条来源。兼容详情通过lateral优先选择exact/high/inferred再到declared，机器version为空时可回退独立声明version，因此既有单一`source/confidence/version`响应和筛选合同不变。
- 有效RED分别证明generation143没有证据关系，以及旧完整probe访问预期关系时42P01；generation144临时全Schema真实PG证明mixed缺席后只剩manual、stale machine-only父行与unresolved均删除、current更新为agent/exact、新的不完整清单不删除旧configuration。FK全Schema审计证明新子表主键覆盖外键。
- 开发期Schema不迁移旧generation：本机专用55432测试库由仓库受保护reset命令从143重建为144并seed，PostgreSQL服务保持运行；没有远端或生产操作、回填、双写或兼容表。BUG-031按公开目录与后台解析队列的长期持久错误定Medium/P1；BUG-027的claim cursor检查保持独立且继续通过广域测试。

## D-215：项目下载发布以安全扫描状态转换为唯一提交点

- 审计时普通上传完成会得到`oss_files(status=active,scan_status=pending)`；项目文件创建只检查active便立即写active关系和`download_added`事件。列表也返回pending，只有下载查询要求clean，导致“已发布通知、公开列表、实际可下载”三种事实互相矛盾，rejected以后旧事件仍不可撤回。
- 不强迫上传者在提交版本元数据前轮询扫描。generation145把关系状态扩为processing/active/rejected/deleted并新增单调`publication_generation`：pending上传在OSS `FOR UPDATE`锁内创建processing关系、返回201和现有scanStatus，第一方既有中英文成功文案已明确“扫描通过后自动开放”，公开页不显示该关系。
- `PATCH /admin/oss/files/{id}/scan`原本已锁OSS行并在事务更新scan/file status；现在同事务锁定所有未删除项目关系，按clean/trusted→active、pending→processing、rejected→rejected同步。非active到active才以原上传者产生`download_added`，active到非active才以扫描actor产生`download_removed`；失败使OSS、关系、审计和Outbox一并回滚。
- 每次重新进入active递增publication generation，事件batch键包含kind/file/generation，避免清理后重新批准被首发幂等键永久吞掉。用户删除现在可删除processing/rejected，但只有删除active才发remove。公开列表和下载都要求关系active、OSS active及scan_status在clean/trusted闭集。
- 数据库`ensure_project_file_publication_safe` BEFORE trigger以命名23514拒绝任何active关系指向pending/rejected/quarantined OSS，防守未来内部或直接SQL旁路；默认关系状态改为processing。现有active分页索引和多态项目FK不变，100k keyset计划仍命中原索引。
- 有效真实PG RED同时观察pending立即active、发add、公开列出，rejected仍active且直接SQL可写unsafe active。修复后完整临时Schema覆盖pending→clean、active→pending隔离/remove、再次clean generation2、pending→rejected零add和命名23514；generation145本机专用测试库按保护确认重建，无生产/远端迁移或双协议。
- BUG-032按未扫描/拒绝的可执行项目发布物跨越公开更新与关注通知安全信任边界定High/P1；下载端此前已阻止实际字节访问，因此不定P0。BUG-033版本闭集、PERF-023分页规模和TEST-016广域授权/供应商矩阵保持独立。

## D-216：站内项目兼容事实只接受当前Minecraft版本目录code

- 审计定位的文件POST仅以`uniqueTrimmed(...,100)`清理字符串，更新日志也只做同类字符串规范化；自动外部日志在没有版本时甚至写入`unspecified`，clean镜像则直接把供应商`gameVersions`复制到`project_files`。有效真实PG RED证明`future-typo`以201持久化，前端选项闭集无法保护直接API调用。
- `loadMinecraftVersionConfig`现在接受pool或事务的最小QueryRow接口；共享`classifyMinecraftVersionCodes`按当前配置精确匹配code，返回已识别与未知两组，`authoritativeMinecraftVersionCodes`为手工保存提供失败关闭边界。超过100项、未知code和空识别结果均不能成为站内事实；配置JSON/Schema故障继续按ARCH-016传播为500，不能伪装业务400。
- 文件创建在锁OSS和写关系前校验；更新日志创建/编辑在建revision前校验。`applyProjectChangelogSnapshotTx`还在审核批准、立即发布和自动发布共用的事务提交点重新读取目录并验证，故待审期间被删除的版本会以冲突拒绝，不会靠旧revision绕过当前权威。
- 自动外部日志不再制造`unspecified`。显式或项目回退版本先与目录求交：unknown-only release跳过并计数，mixed release仅发布已识别子集；同步签名包含正文和兼容metadata，版本变化不再被“正文未变”短路。binding metadata分别保存resolved、raw和unmapped版本，人工覆盖冲突也保留这些来源事实。
- clean镜像晋升在事务前用同一目录分类；unknown-only镜像转`review`且不建`project_files`，mixed镜像只把已识别code写入站内数组，原始供应商数组继续完整存在`mirrored_project_files.metadata`。这满足“未知外部标签可保留，但不能污染站内兼容事实”的边界。
- 无DDL、索引、回填或generation变化，保持145；本机完整临时Schema测试自动清理，专用public测试库无需重置，未接触远端/生产数据。成功DTO、路由和第一方payload不变，唯一手工协议收紧是未知值400；自动任务结果只新增跳过计数。BUG-033按持久数据正确性定Medium/P1，REUSE-003的项目版本排序复用仍独立开放。

## D-217：外部更新日志只有已批准user revision才能取得人工覆盖权

- 审计时PUT先创建approved或pending revision，却无条件把绑定`manual_override=true`；审核拒绝只恢复changelog review状态，不恢复binding。有效真实PG RED在合法pending响应后立即读到true，证明用户无需通过审核即可永久阻止Worker覆盖。
- 删除Handler末尾的独立binding UPDATE。共享`applyProjectChangelogSnapshotTx`在版本目录校验、changelog行与本地化写入全部成功后，调用`recordApprovedChangelogManualOverrideTx`；任何后续事件或commit失败仍回滚整组事实。新helper连接目标changelog和实际content revision，只在aggregate匹配、`revision.source='user'`、binding仍source-managed且尚未override时转换。
- 该提交点同时覆盖无需审核的即时人工编辑与审核批准；pending没有进入apply，rejected没有进入apply，base revision conflict在apply之前返回，自动同步revision的source为auto_update，因此四条非批准人工线路都不能转换。第一次已批准user revision产生authority转移，后续人工编辑不轮换其来源审计。
- generation146为`external_release_bindings`新增`manual_override_revision_id bigint references content_revisions(id) on delete restrict`和`manual_override_source`。命名`external_release_bindings_manual_override_origin_check`要求false严格对应null/空、true严格对应非空revision/source；nullable FK有专用partial leading index。直接只写boolean精确返回命名23514。
- review config读取原本在已开始事务后另用pool查询，会使单连接完整Schema测试等待第二连接，也让配置选择游离于revision事务；项目changelog创建/编辑现在都用同一tx读取既有QueryRow接口。这不改变配置语义，并为审核决策提供一致的事务边界。
- 真实PG先让自动同步创建source-managed日志：pending→rejected后binding保持false，下一上游正文updated=1并实际公开；第二次pending→approved后binding保存精确revision ID/source=user，下一上游正文只计manual conflict且公开正文保持人工版。无成功DTO、前端payload或路由变化。
- 旧generation145开发库不升级；仓库受保护reset以development、明确本机`127.0.0.1:55432/postgres`和`RESET postgres`把专用测试库重建/seed为146，PostgreSQL服务未停止，无远端/生产操作、回填或双写。BUG-034按拒绝审核仍改变来源控制权的持久授权绕过定High/P1；PERF-024与TEST-017的分页/广域责任保持独立。

## D-218：clean自动镜像必须通过项目文件发布事务

- 审计证据是镜像提升器直接INSERT `project_files`再把镜像改为ready，不调用手工文件已有的`download_added`提交点。generation146进一步把项目文件默认状态收紧为processing，因此旧路径不仅没有通知和历史，还会把已经clean的镜像留成不可公开文件。
- 有效真实PG RED在`project_update_events`上安装拒绝trigger：旧提升器完全没有触碰该trigger，仍提交mirror=ready和1个project_file，精确证明文件关系、更新历史、通知任务与Outbox不属于同一原子单元。
- 提升器现在先读取Minecraft版本权威目录，锁定`mirrored_project_files`，并通过`projectFileTargetIsApprovedTx`对目标mod/modpack/simple project取得`FOR SHARE`审核快照。clean文件首次创建显式保存status=active、publication_generation=1，而不再依赖容易漂移的Schema默认值。
- 仅首次成功INSERT且项目当前approved时复用`enqueueProjectFileUpdateEventByRouteTx`；该函数用`project-file:download_added:<publicID>:<generation>`作为唯一publication batch，并在同一事务写`project_update_events`、延迟通知任务和NATS Outbox。自动事实actor保持NULL，不制造交互用户身份。
- 任一目标读取、文件插入、事件、任务、Outbox或mirror ready写入失败都回滚；事件成功后才允许ready。并发/重复提升由mirror行锁、文件唯一键和publication batch唯一键共同幂等。未批准项目仍可保存安全文件事实，但与手工路径一致，不产生关注者发布事件。
- generation保持146，无DDL、索引、开发库重置、回填或双写；真实测试只使用本机单连接完整临时Schema并自动删除，未访问远端/生产。BUG-035按所有自动镜像项目可持续丢失关注更新与不可变历史定High/P1；扫描拒绝后的重试状态属于独立BUG-038。

## D-219：关注关系生命周期不能复用内容可见性授权

- 原实现让PUT、GET status、DELETE和“我的关注”全部调用/拼接同一个内容可见性规则；该规则为了编辑与审核场景合理地允许submitted_by、author、owner和全局reviewer读取pending内容，却被PUT误当成“公开可关注”，同时使普通用户在目标隐藏后无法先解析route再删除自己的关系。
- 有效真实PG RED用同一个pending mod验证submitter和`project.review`调用者均得到200并共写2条关注；这不是模拟数据库旁路，而是生产Handler与真实visibility SQL的直接结果。另一个先approved后pending的目标用于验证幽灵关系生命周期。
- 新`followProjectTargetPublicVisibilitySQL`明确列出12种可关注route的公开条件，不接受viewer ID或moderator布尔。原`followProjectTargetVisibilitySQL`首先复用该公开谓词，再追加作者/审核读取分支，因此内容引用、收藏、举报等既有合法预览能力不退化，而关注创建不会随内容读取权限扩大。
- PUT改为单条`INSERT INTO project_follows ... SELECT route.id ... WHERE <strict-public>`并通过RETURNING判断404；可见性判断与关系写在一个数据库statement snapshot中，不存在先解析后隐藏仍用旧route插入的应用窗口。既有关系冲突仍只刷新时间，不制造第二行。
- DELETE只用当前user ID与规范化public route ID执行`DELETE ... USING public_routes`，不加载名称、review status或内容表；隐藏、已无关系和未知合法ID都幂等204，不能借取消接口探测目标状态。非法格式同样204。
- status只在目标当前公开或当前用户已有关系时返回；后一种情况把name/url/updatedAt清空并标`unavailable=true`，非关系持有人仍404。“我的关注”保留同一安全占位，搜索仅允许精确已知ID命中隐藏项，不用隐藏名称参与模糊匹配。
- 前端协议仅增加可选`unavailable`：列表不生成空链接/Invalid Date，详情按钮对隐藏既有关系允许取消，取消或status 404后禁用重新关注。中英文均有占位文案，源码回归锁定ApiError 404和脱敏渲染。
- generation保持146，无DDL、索引、回填、双写或开发库重置；本机完整临时Schema自动清理，未操作远端/生产。BUG-036按幽灵关系与通知复活的数据生命周期缺陷定Medium/P1；PERF/TEST中的关注分页、通知规模和更完整目标矩阵仍独立处理。

## D-220：领域枚举只在通知最终渲染边界本地化

- 审计定位的事件生产端已经用`uniqueProjectUpdateSections`保存稳定section code，问题发生在Worker把这些code预先以英文逗号连接，然后把同一字符串交给所有收件人的本地化模板。有效真实PG RED让中英关注者接收真实`description/download_files/minecraft_versions/future_internal_code`，两种正文及template params都直接泄露内部枚举。
- 不把中文label写入`project_update_events`，也不改变通知`data.changedSections`；这些字段继续作为机器可读协议保存稳定枚举。Worker先用共享模板选择器确定请求locale最终落到的实际模板locale，再在每个收件人的`changed_sections`值上执行展示映射，因此模板fallback与变量语言不会分裂。
- alias表覆盖当前审核快照的camelCase字段和手工事件的snake_case section，并把同义字段归并到语义label；映射后的重复label只展示一次。zh-CN和zh-TW分别使用简繁label与顿号；当前其余模板locale沿用默认英文外壳及英文人类label，不能再显示下划线协议名。
- 未知枚举统一映射为该locale的“项目资料/project details”，不会复制、猜测或title-case未来code。事务成功后只按实际新插入通知数乘未知code数累加`unknownSectionFallbacks`，并记录event ID、未知code和delivery数；回滚、幂等冲突或零收件人不会虚增。管理员基础设施指标新增对应计数。
- 模板renderer抽出纯选择函数，原有变量完整性、模板version和locale fallback语义不变。真实PG GREEN逐项证明中英body/source locale/template params均为人类文本，而`data.changedSections`仍逐值等于原始稳定枚举，未知值产生两次真实delivery指标。
- generation保持146，无DDL、索引、回填、双写、开发库重置或前端协调；完整临时Schema自动清理，未访问远端/生产。BUG-037按纯用户文案/未来兼容影响定Low/P2；OPS-004、PERF-025和TEST-018的重试、规模及更广Worker矩阵继续独立开放。

## D-221：镜像安全扫描拒绝必须成为可恢复的显式失败状态

- 旧提升器只查询`mirror.status='scanning'`且OSS为active/clean的行；rejected OSS不会进入查询，镜像永远scanning。下一轮下载同步又只按`(source_type,external_file_id)`看到“已存在”并计为成功，既不检查OSS安全状态，也不报告扫描失败。有效真实PG RED精确得到scanning与`existing=1/needsReview=0`的nil error。
- `promoteCleanMirrors`现在对两类事实统一对账：scanning或ready镜像遇到OSS rejected、deleted或关系缺失时写failed，ready镜像遇到pending时退回scanning；scanning或failed镜像遇到同一OSS active+clean/trusted时才进入发布候选。管理员PATCH pending/rejected也在锁定OSS、同步项目文件的同一事务同步镜像为scanning/failed，周期对账则修复历史遗留或非第一方状态变化。
- clean恢复不是把failed直接改ready。提升器重新解码原provider metadata、用当前Minecraft版本权威目录分类；损坏或unknown-only进入review。合法候选再锁镜像，允许scanning/failed，创建active generation1项目文件，并复用BUG-035的项目更新/通知/Outbox原子发布事务，最后才写ready；任何读取、锁、事件或commit失败均返回且回滚。
- `mirrorFiles`查询既有镜像时同时读取mirror、OSS file和scan状态。failed以及当场发现的rejected/deleted/missing都增加`scanFailures`与`needsReview`，最终返回可由`errors.Is`识别的拒绝错误；`execute`稳定映射`project_mirror_scan_rejected`，因此run进入既有重试/dead-letter语义而不能写completed。正常pending只计`awaitingScan`，review/source_changed保持人工复核，ready保持幂等existing成功。
- 真实PG从遗留scanning+rejected开始，证明对账写failed、运行结果`scanFailures=1/needsReview=1`且显式失败；既有管理员clean重扫后，同一OSS通过重验发布为ready/active。随后对ready文件发起pending重扫会同步得到mirror scanning/project file processing，运行只报告`awaitingScan=1`；再rejected得到failed/rejected并显式失败，第二次clean仍恢复ready/active；最终一轮返回existing=1、scanFailures=0且成功。镜像身份/上传补偿回归的旧generation143断言同步为当前146，并单独及广域通过。
- generation保持146，无DDL、索引、回填、双写、开发库重置或前端迁移；临时完整Schema自动销毁，未访问远端/生产。BUG-038按自动任务伪成功、永久卡态和安全规则修复后无法恢复定High/P1；`DEAD-004`描述的是`project_auto_update_runs.failed`而非镜像failed，仍保持独立开放，不能借本修复合并关闭。

## D-222：自动维护状态必须作为权威内容修订发布

- 旧半年/一年维护策略在同一事务内直接UPDATE `mods`或`simple_projects.official_status`，然后只更新可变`project_automation_activity`。有效真实PG RED证明主表已变lowFrequency，但`published_revision_id`仍指向user/active父revision，没有published review event、不可变audit或project update；即使给`project_update_events`安装拒绝约束，旧路径也不触碰它并泄漏discontinued状态。
- 自动维护不是投影修补，而是影响目录展示和关注者的内容发布。Worker现在锁定项目行并读取`published_revision_id`；没有已发布基线时失败关闭，不能凭当前主表拼出缺乏来源的伪snapshot。待审revision由既有`createContentRevisionTx`冲突规则阻止自动覆盖。
- 发布helper用entity、aggregate和public key精确加载该revision snapshot。Mod解码为`createModRequest`，插件、数据包、地图、资源包与光影等解码为`simpleProjectSnapshot`；只修改`officialStatus`，保留本地化、分类、版本、加载器、license及所有关联事实。simple project类型还需通过封闭规范化注册表。
- 新revision的actor为既有autobot、source为`auto_update`、base为刚锁定的published revision，metadata记录`automation=project_maintenance`、前后状态、source type和update kind。随后在同一数据库事务调用权威Mod/simple snapshot apply、approved resolution和catalog published事件提交点。
- 该共享链同时写content revision/change request、submitted/approved/published review events、两条不可变audit事实、`published_revision_id`、project update event、延迟notification task和NATS Outbox。只有全部成功后才更新automation activity并commit；任一关联、审核、事件、task、Outbox或commit错误都会回滚主表、revision、activity和投递事实。
- 真实PG覆盖Mod与plugin两类存储路径；两者都形成base正确的auto_update子revision与`officialStatus`更新section。既有策略用例证明lowFrequency→discontinued→active连续三次发布、六条不可变审计和三组通知事实，人工改为development后仍保留manual override且不新增自动revision。拒绝project update写入的故障注入证明第二次转换零泄漏。
- generation保持146，无DDL、索引、回填、双写、开发库重置、HTTP或前端迁移；完整临时Schema自动销毁，未访问远端/生产。开发基线要求可自动维护项目已有权威published revision；历史异常行不猜测回填。BUG-039按公开状态绕过修订、关注通知与不可变审计定High/P1；`DEAD-004`和`TEST-019`的运行失败状态/更广矩阵仍独立开放。

## D-223：爬虫AI结果必须进入项目权威localizations载荷

- 旧路径从importer JSON调用AI，却只把结果挂到user draft的`seedTranslations`。全仓没有消费者；auto-submit另外把翻译前的原始`result`交给项目Handler。更早一层还把Mod名称作为`primaryName`发送给AI，却用只接受`name`的通用结果函数读取，导致名称翻译即使返回也被静默丢弃。
- 有效真实PG RED使用本机可控Modrinth和OpenAI-compatible响应：7个目标locale任务全部completed并精确记录21/28 tokens，但草稿标准localizations只有en-US、仍含死字段，已创建Mod的published revision和`content_localizations`也都是1语言。测试容忍当前generation146草稿提交约束的独立BUG-042错误，并在项目已创建后继续核对本地化事实，避免把状态分裂误合并进本项。
- 翻译源现在从importer已规范化的`defaultLocale`及其localization读取，Mod的`contentMarkdown`和simple project的`bodyMarkdown`统一转换为AI稳定键`name/summary/bodyMarkdown`。source locale不再硬编码en-US；当前importer仍产生en-US，但未来权威来源变化不会产生自翻译或错误标注。
- AI返回使用严格结果解码：必须是对象数组，键只能来自本次非空源字段，不得重复、缺字段、非字符串或空白。畸形结果把translation task写failed，保留实际input/output token计费和last_error，不能伪装completed或进入草稿；状态写失败继续向上传播。
- 类型映射器按支持locale稳定顺序合并，只为原payload不存在的locale追加AI值。Mod写`catalogLocalizationEdit`并调用`normalizeAndValidateModRequest`，plugin/shader/resource-pack等写`simpleProjectLocalization`并调用`normalizeAndValidateSimpleProjectDraft`；名称、摘要、正文长度和全部项目业务字段因此在保存/提交前一起重验。
- user draft在typed localized JSON上只追加`importOrigin/externalProjectId`内部元数据，不再保存`seedTranslations`。自动提交使用不含内部扩展字段的同一localized typed JSON，因此严格HTTP decoder、content revision、published snapshot和本地化投影看到同一事实。无AI配置、零预算或单个翻译失败时仍可保留合法源语言及其他已成功locale。
- 扩展真实PG在同一完整Schema再覆盖plugin：Mod与plugin各7次AI请求、各7个completed task及21/28 tokens；两份草稿、两类published revision及本地化投影均为8语言且无死字段。相关Mod发布、AI损坏结果、爬虫故障可观测性和所有项目创建/导入授权矩阵通过。
- generation保持146，无DDL、索引、回填、双写、开发库重置或公开API/前端迁移；临时Schema自动销毁，provider/AI仅为本机test server，未访问远端/生产。BUG-040按付费AI结果持续不可消费且公开项目缺本地化定High/P1；创建/草稿/candidate/source多步状态原子性仍由BUG-042独立处理。

## D-224：唯一候选必须分别保存首次发现和当前内容来源任务

- `seed_crawler_candidates.external_project_id`唯一，因而一行不是一次观察事件，而是多次爬虫运行覆盖形成的当前投影。旧冲突分支更新downloads、payload和updated_at，却保留首次insert的`run_id`。有效真实PG RED连续调用生产upsert：第二次payload已经是slug=`bug041-new`、downloads=20，但记录run仍为首次任务1而不是实际写入内容的任务2。
- 只在冲突时改写原`run_id`会修好“当前来源”却永久丢掉首次发现事实；把它继续叫run_id也让调用方无法判断语义。因此generation147直接把开发基线列拆为`first_seen_run_id`和`last_seen_run_id`：首次INSERT两者相同，ON CONFLICT只更新last seen、downloads、payload与updated_at，first seen不参与更新。
- 两列均为NOT NULL并引用`seed_crawler_runs(id)`，不使用`ON DELETE SET NULL`。候选存续时删除任一来源任务会被数据库拒绝，避免审计血缘后来静默消失；两个单列前导索引分别支撑FK反向检查。生产没有任务删除路由，因此这是对实际审计不变量的收紧，不移除用户能力。
- 候选列表与单条详情各以主键连接两次run表，返回`firstSeenRunId/lastSeenRunId`的九位公开身份，而不是泄露内部bigint。字段必填，既有external ID、状态、分页游标、payload按需详情和HTTP状态码不变；Next16管理面显式声明两字段，通用表格自然显示它们。
- 真实PG GREEN证明同一候选两次upsert后内部first/last=1/2、当前payload为第二次内容，对外ID为`bug041a01/bug041a02`，删除首次任务被FK拒绝。100k run/1M candidate生产查询计划仍先用downloads或status-downloads keyset索引取51项，再按run主键连接；普通/状态深页分别约0.737/1.124ms。
- BUG-040的直接测试夹具同步先创建真实run再写两个必填来源，翻译发布完整回归通过；这不关闭BUG-042的创建/草稿/候选/来源多事务状态分裂。generation146开发库不迁移或猜测旧run语义，专用本机`127.0.0.1:55432/postgres`以development、精确数据库名和`RESET postgres`保护重建/seed为147；未访问远端/生产，无回填、双读或双写。BUG-041保持审计既定Medium/P1。

## D-225：爬虫项目、来源、草稿与候选终态必须一次提交

- 旧auto-submit先让项目Handler独立commit，再忽略external source写入错误、独立更新draft和candidate；外层失败处理还能把已创建项目对应的候选写成failed。有效真实PG RED在`project_external_sources`安装拒绝trigger后得到project=1、source=0、active draft=1、submitted draft=0、candidate=candidate，精确证明项目成功与来源/审核/后台状态可以永久分裂。
- 草稿形成是项目提交之前的可恢复阶段。`createSeedDraft`现在在一个事务中upsert同一active `user_drafts`并把candidate写为manual的`draft`或auto-submit的`submitting`；任一写失败整阶段回滚。`submitting`进入generation148命名CHECK和管理过滤闭集，但不属于terminal skip集合，进程在项目提交前退出时下一次运行可覆盖同一active key并重试。
- 项目创建已经分别由Mod与simple-project Handler拥有大型权威事务，不复制其领域逻辑。新增的context transaction hook是`internal/httpapi`包内不可导出能力，普通HTTP请求没有该值时是零行为；seed worker注入后，两个Handler都在自身commit紧前传递项目内部/公开ID、site ID、title、target、review status和change request事实。
- hook在同一个`pgx.Tx`解析`public_routes`、upsert `project_external_sources`、以精确draft/user/未提交条件写入project key、target、change request、review status和submitted time，再以`candidate.status='submitting'` CAS写submitted。来源、草稿或候选任一步错误直接返回Handler并由defer rollback项目、revision、route及全部关联，不能形成已创建但未提交的项目。
- Handler commit后的响应装配若失败，worker只以candidate=submitted且同一draft有submitted/change request的数据库哨兵承认成功；外层失败记录限定`status not in ('submitted','existing')`，因此已经提交的终态不能被响应故障覆盖。真正的precommit失败保留active draft，candidate为submitting或外层failed，两者都在后续run可恢复。
- GREEN在完整临时generation148中分别拒绝Mod来源绑定、plugin候选完成和manual draft阶段候选更新：前两者均零project/source/submitted draft且保留active draft/submitting，第三条draft/candidate一起保持零变化；移除trigger后同一草稿重试得到正确`mod:`/`plugin:` project key、target、external source、change request和submitted终态。BUG-040/041及25条项目创建/导入授权子项均通过。
- 本项只关闭多事务事实分裂，不宣称外部OSS副作用已成为数据库事务，也不合并TEST-020的更广爬虫测试矩阵或DEAD-005的并发run claim。generation147开发库不原地迁移；专用本机55432 development库经精确`RESET postgres`确认重建/seed为148，无远端/生产、历史回填或双写。BUG-042保持审计既定High/P1。

## D-226：通知翻译locale必须来自启用站内注册表并先规范化

- 旧接口只对`targetLocale`做trim、非空和20字符上限；缓存查询、AI payload和后续主键均使用原字符串。有效真实PG RED预置`pt-BR`缓存后接口直接200返回该无人消费语言，而语义等价的`EN_us`无法命中`en-US`缓存，落入额度检查并403；Worker单测也接受pt-BR payload，证明入口和完成边界都没有站内注册表。
- 通知展示语言不是开放内容冷语言bundle。第一方消息中心只会传当前八项UI locale，通知模板和内容编辑启用集合也由`supportedEditableContentLocales`/`supportedContentLocaleList`覆盖这八项；为任意BCP-47代码付费并保存没有产品消费者。因此通知翻译明确采用该启用闭集，而不复用`validContentLocaleTag`的开放语法边界。
- `normalizeNotificationTranslationLocale`先调用现有canonical规范化，再以启用map判定。Handler在读取通知内容、缓存、额度和模型配置前完成该转换；随后同一个canonical值用于缓存主键查询、task payload和日志。`EN_us`变为`en-US`，pt-BR、zh-HK等未启用独立代码即使语法有效也400。
- Worker的`decodeNotificationTranslation`复用同一helper；直接/遗留任务不能绕过HTTP在`notification_translations`写注册表外locale，completed结果GET也会把这类损坏事实报告为500而不是返回成功。结果items严格性、业务持久化先于completed的ARCH-026边界不变；BUG-044由下一决策以该独立证据映射关闭，不与locale修复混算。
- 无DDL、索引、generation、开发库重置、回填或双写，保持148。现存注册表外缓存不主动猜测迁移，且新接口不会读取；后续数据清理可单独执行。真实测试遍历全部八个canonical值、alias cache、预置unsupported cache和Worker旁路，notification outbox/system禁译/template及ARCH-026损坏结果均回归。BUG-043按自身额度浪费和缓存污染定Medium/P1。

## D-227：BUG-044由ARCH-026的同根完成态修复直接关闭

- 对审计baseline `bd50afd0ef92192c40c5152ea56e27a3fdbb657f`只读复核：`notificationTranslationResult`对result/payload各执行一次`_ = json.Unmarshal`，随后忽略`notification_translations` upsert错误仍构造translation并200；旧`persistNotificationTranslation`返回void，同样吞掉payload/result类型和数据库写入错误。这与BUG-044证据逐字一致。
- ARCH-026处理的是相同函数、相同错误和相同成功时序，而不是上层泛化修复：共享decoder验证notification ID、locale和title/body闭集；persistence返回所有解析/upsert error；Worker必须先写业务缓存，再以`status='running'` CAS写completed，错误则调用也返回error并检查行数的failed转换。
- completed GET删除全部cache upsert，成为纯读；payload/result/item损坏记录`notification_result_decode`并500。它不再用读取请求补偿Worker，也不会刷新缓存created_at。业务写后completed CAS前的崩溃保持running，由既有stale recovery重投幂等upsert。
- ARCH-026的有效RED已证明旧三类损坏completed均200、completed写早于业务持久化且notification void写吞错；真实临时PG GREEN覆盖健康缓存、坏payload/result/item、改列造成upsert失败、failed写XX000和GET不改变2020时间戳。本轮又在显式本机55432重跑同一矩阵，全部通过。
- 因当前代码和故障测试已经满足BUG-044全部建议，本项只补齐一对一台账、根因与验证映射，不添加第二套decoder或重复测试。原修复无DDL且当时保持generation142，当前generation148继续使用同一协议。BUG-044按既定Medium/P1关闭；BUG-043 locale authority、BUG-094并发、OPS-019恢复及TEST-043完整供应商矩阵不合并。

## D-228：私聊历史cursor与实时after锚点必须具有不同的严格语义

- BUG-045原证据有两部分。PERF-030已把无锚点最新100条扩展为member/conversation/limit绑定的向前历史cursor，100k会话/1M消息证明旧消息可连续访问；该修复特意保留`after`的无效锚点语义给本项处理，不能重复宣称历史分页缺失。
- 剩余旧SQL用`message.id>(select id ... public_id=? and conversation_id=?)`；不存在或其他会话anchor让标量子查询为NULL，结果与“合法最新anchor后没有新消息”同为200空数组。Handler还在加载页面之前批量写read_at，故有效真实PG RED中畸形、未知、跨会话三类请求不仅都空200，还把当前会话2条未读清零。
- parse层现在要求非空after为合法九位public ID，畸形值在任何消息读取/写入前400。对语法合法且页面为空的after，loader额外执行`select exists`并同时绑定public ID和当前conversation；false返回包内typed sentinel，Handler统一映射400。使用400而非区分404，避免让成员借差异探测某消息是否存在于其他会话。
- 非空增量页已经由原子查询中的同会话子查询证明anchor存在，不增加往返；只有空增量页执行一次确认。合法latest anchor因此仍能返回200空页，未知/跨会话则400。1M夹具证明两类空页都固定为页面查询+anchor probe两条SQL，probe走`idx_direct_messages_public_id`且执行0.015ms，无规模表Seq Scan。
- 页面加载与anchor验证现在先于read_at UPDATE；任何分页、锚点或数据库读取错误都不会改变未读摘要/缓存或发布`unread.changed`。合法页面继续按原协议把当前会话全部收件未读标记已读，并由generation117 trigger与缓存调整保持一致。
- 无DDL、索引、generation、重置、回填、双写或前端DTO变化，保持148并复用generation117索引。消息中心的历史cursor和多页after排空逻辑不变。BUG-045按持久历史/副作用正确性定Medium/P1：无其他会话正文泄露或授权绕过；PERF-030规模、PERF-031失效协调和TEST-022/023广域矩阵不合并。

## D-229：本机即时实时投递必须用实例来源抑制NATS自身回声

- 旧`publishRealtimeUser`构造一个事件后先向本机Hub投递，再用同一queue连接广播到`user.<id>`；每个实例又订阅`user.*`且连接没有NoEcho。有效官方内嵌NATS RED让两个真实queue Client和两个Server/Hub运行后，来源实例先收到本机事件，随后又收到ID完全相同的NATS回声；peer只收到一次。
- 不在共享queue连接启用connection级NoEcho：同一连接还承载任务唤醒，抑制本连接发布会改变任务消费可用性。也不删除本机投递改成NATS-only：瞬时broker发布失败不应让当前实例已经发生的实时事实消失。因此保留“本机立即一次+broker跨实例一次”的可靠性边界，只识别并跳过来源实例的回声。
- `realtimeEvent`内部JSON envelope新增`origin,omitempty`。每个Server以`sync.Once`惰性生成128-bit随机origin；本机发布时把它附在事件上，广播订阅仅在origin非空且精确等于本Server origin时跳过。peer在第一次接收时生成自己的不同origin并正常投递；事件ID和数据在两实例保持一致。
- 兼容采用可选字段而非强制新版本：旧订阅者会忽略origin，新订阅者会接受origin为空的旧实例广播。`notification_worker`和`project_update_notification_worker`没有先做本机Hub投递，继续发origin-less事件；新订阅者不能把它们误判回声。双向GREEN及显式origin-less控制证明两个实例各收到恰好一次。
- SSE公开协议没有序列化整个内部struct，而是继续手工输出`id:`、`event:`和仅含Data的`data:`；origin不进入浏览器、前端类型或去重键。现有前端有限Set去重仍可防网络重放，但不再承担服务端正常投递正确性。
- 无DDL、索引、generation、数据库重置、回填、双写或公开API变化，保持148；既有模块图tidy后SHA-256不变。BUG-046按重复业务事实与容量浪费定Medium/P1，但事件ID、授权和正文不变且当前前端有掩护，故不定High。OPS-007重连恢复、PERF-031客户端失效协调、SEC-017/018连接预算及TEST-023广域矩阵不合并。

## D-230：私聊视图切换必须在选择动作和响应提交点双重绑定conversation

- 审计baseline让全部会话共享loading和last message ref，既不取消请求也不核对conversation。PERF-030/031期间当前代码已把loading标记改成conversation字符串并增加request version，但仍未完成BUG-047：点击B直接提交selected ID，而A正文要等新effect中的`setTimeout`才清空，因此正常切换无需网络竞争就至少有一次机会把B标题/发送目标与A正文同屏；读取也仍在后台运行到响应后才靠version丢弃。
- 有效当前源码RED要求统一选择函数、同动作清空、两类AbortController/signal及conversation+version提交守卫，2/2失败。它针对仍存在的产品路径，不把旧baseline已由PERF修掉的全局boolean重复当作本轮RED。
- 新`selectConversation`是唯一交互/URL目标选择入口。它先使请求代次失效并中止当前初始/增量及旧历史读取，立即把ref更新为新conversation，清空last ID、正文、历史页和草稿，再提交selected ID；React同一事件批次不会再渲染“B身份+A正文”。effect只负责启动B加载，不延迟承担安全清空。
- `loadMessages`为每个实际执行的读取建立AbortController和新version；所有首屏与多页after响应在改变messages、history或last ID之前，必须同时满足signal未中止、ref仍是请求conversation、version仍为当前。正在排空的同会话实时请求继续用refresh sequence合并后续失效，未退化PERF-030的多页after语义。
- `loadOlderMessages`有独立controller，避免历史请求与实时请求互相无意取消；选择变化同时中止两者，旧finally也不能把新请求的loading状态清零。主动AbortError静默退出，不制造错误横幅。发送mutation不在切换时取消，因为服务器可能已经接受且其原conversation是点击时的正确目标；响应仅在该conversation仍选中时追加/更新anchor，避免污染B正文。
- 无后端、数据库、SSE、消息DTO或路由变化，generation148保持，无重置、回填或双写。BUG-047按跨会话私聊正文暴露及错发诱导定High/P1；服务器授权没有被绕过且需要切换竞争，故不定P0。PERF-030历史规模、PERF-031实时协调及TEST-023完整React/EventSource矩阵保持各自责任。

## D-231：AI余额是首次翻译前的账户决策事实

- 旧`aiBalance`初始为null，余额卡又要求非null；`loadBalance`只有翻译try末尾一个调用。因此用户首次看到可翻译通知时不可能看到remaining/reserved/unlimited，只有先完成一次可能收费的操作才能获得决定该操作所需的账户事实。有效当前源码RED要求首次条件加载和queued终态cleanup刷新，2/2失败。
- 不在消息中心无条件加载：当前分类的已加载页先计算是否存在`translationAllowed`，只有存在且没有当前账户snapshot时才发请求。这保留审计建议的按需边界；同账户切换通知kind或分页复用余额，不为每次普通通知刷新重复调用。
- 余额状态保存`{token,balance}`而非不带身份的裸对象。渲染只使用snapshot token等于当前认证token的值；账户变化时旧值立即不满足展示/完成条件，新effect读取新账户。effect用标准AbortController并在cleanup取消，React effect通过零延迟任务启动，避免同步级联state更新。
- 翻译开始响应明确区分cached与新任务。只有`!cached`表示排队及额度预留/消耗可能变化，故设置refresh标记；poll观察成功或失败后都进入统一finally读取余额。翻译失败文案优先保留，随后的余额读取失败不会覆盖它；翻译成功但余额读取失败则单独显示加载错误。cached命中没有任务状态变化，不制造刷新。
- 无后端、Schema、额度算法、任务状态、API DTO、路由或generation变化，保持148，无重置、回填或双写。BUG-048按有限AI token知情消费缺失定Medium/P1；请求仍由本人显式触发且无授权/持久数据破坏，故不定High。BUG-043 locale authority、BUG-044完成态及TEST-023/043广域矩阵不重复关闭。

## D-232：站点品牌只通过Next metadata模板组合页面标题

- 旧根metadata固定`Mcmods-cn`，客户端SiteBrandProvider又按pathname算documentTitle：后台为品牌+后台，其余全部仅品牌。effect先直接赋值，再用MutationObserver观察整个head并在任何Next metadata变化后改回，导致页面title即使由框架生成也无法稳定存在。有效当前源码RED要求根generateMetadata模板与无客户端head所有权，2/2失败。
- 根layout现在调用`loadMetadataSiteName`并返回Next支持的`title: {default,template}`。无子页title时使用站点名；任一现有或未来page/layout metadata提供title时由`%s | siteName`组合，页面事实不会被品牌替换。description继续使用siteName，不制造平行客户端meta标签。
- 服务端品牌读取使用既有公开`/api/v1/site/config`、`cache:no-store`和2秒AbortController上限；非2xx、网络、超时、非字符串或空名称均回退`Mcmods-cn`。额外`%s`片段被改写为普通文本，不能把管理员品牌值变成第二个metadata模板占位符；Next负责HTML转义。
- 客户端Provider删除pathname/i18n标题分支、document.title写入、MutationObserver及head订阅。初始及事件品牌请求仍更新Logo/siteName Context；`mcmods-site-brand-change`处理器随后调用`router.refresh()`，由Next重新执行服务端metadata边界，支持运行时品牌更新而不争夺head所有权。
- 无数据库、后端、站点配置DTO、路由、Schema或generation变化，保持148，无重置、回填或双写。BUG-049按导航、分享和SEO辨识度损失定Low/P2，不合并TEST-024更广品牌/metadata/导航矩阵；SEC-019和OPS-008设置权限/多实例资产责任保持开放。

## D-233：邮件启停是独立于SMTP字段完整性的运行时权威

- 旧后台payload确实保存`enabled`，但读取后立即用Host与From重算；Server和NotificationWorker构造Mailer时又只复制主机、端口、凭据、发件人和TLS，`Mailer.Enabled`也只看Host/From。真实PG RED保存加密的`enabled:false`及完整SMTP字段后，读取、Server activeMailer和Worker activeMailer三处全部返回启用，精确复现审计证据。
- 不能以清空Host或From表达关闭：管理员会丢失可恢复配置，再次启用必须重新输入非密码字段，且环境/数据库两个来源仍可能产生不同结论。`config.SMTPConfig.Enabled`因此成为运行时显式事实；持久payload、Server热更新和Worker fallback/数据库覆盖都通过一个`smtpConfigFromPayload`构造器传播它。
- `Mailer.Enabled`要求显式Enabled、非空Host/From和1..65535端口，`Send`在SMTP地址和拨号前调用该门禁。这样验证码、后台测试邮件和异步通知邮件无需复制条件，所有现有生产入口均从最终发送边界失败关闭；关闭状态不会因保存着完整连接信息而绕过。
- 后台PUT仅在`enabled=true`时拒绝不完整服务器、发件地址或端口；`enabled=false`允许保留字段和密码。GET原样返回持久开关，保存成功立即替换Server Mailer；Worker每次发送读取同一加密设置。环境增加`SMTP_ENABLED`，显式false优先；变量未设置而已有SMTP_HOST时维持旧环境部署的启用行为。
- 无DDL、Schema、索引、generation、数据库重置、回填或双写，保持148；现有加密JSON字段已经包含enabled。BUG-050按无法停止验证码/通知出站及隐私/事故控制失效定High/P1；收件人授权和正文没有被扩大且仍需业务事件触发，故不定P0。SEC-019设置权限、OPS邮件投递演练和更广邮件测试责任不随本项关闭。

## D-234：访问日志装饰不能缩窄ResponseWriter协议能力

- 旧`logAccess`对全部`/api/`请求无条件传入只覆盖Write/WriteHeader的`responseRecorder`。生产顺序中compression位于其外层，Realtime handler看到的最内层writer必然是该recorder；其第一个`http.Flusher`断言因此固定失败。有效完整链RED调用真实`realtimeEvents`后精确返回501，另一个合同测试证明Flusher、Hijacker、Pusher和ReaderFrom四项全部丢失。
- 不为Realtime绕过访问日志：这会制造路径特例，失去连接状态/延迟观测，并允许未来任何流式或升级端点重复踩坑。`responseRecorder`继续是统一计数边界，但显式实现四类标准可选接口；Flush和Hijack使用`http.ResponseController`穿过支持Unwrap的嵌套writer，Push逐层寻找底层Pusher，ReaderFrom优先复用底层实现并把实际字节计入日志。
- recorder自身也实现`Unwrap`，使后续ResponseController能力可以继续穿过本层。Flush首次使用时把日志status记为200；ReaderFrom与普通Write共享相同默认状态和实际成功字节语义。底层不支持Hijack/Push时仍返回标准不支持错误，不伪造成功。
- GREEN把真实Realtime handler放进security headers、CORS、cookie origin、Yggdrasil ALI、compression、access log和bot traffic的生产顺序链；底层首次Flush取消测试请求，证明handler已经写出并刷新`: connected`后正常退出，而非靠超时或伪响应通过。四接口控制同时证明调用到达底层，ReaderFrom status/bytes精确。
- 无Schema、API DTO、事件格式、鉴权、连接预算、心跳、generation或前端变化，保持148；访问日志异步队列和批量PG落库不变。BUG-051按全体实时能力不可用定Medium/P1，但仍有拉取降级且无越权、泄露或持久数据损坏，故不定High。TEST-025的OAuth脱敏、slog捕获、游标和清理矩阵保持独立。

## D-235：自动日志保留必须由有界、单实例周期Worker拥有

- 当前PERF-033已经把每条日志DELETE改为按created_at/ID稳定选取、`FOR UPDATE SKIP LOCKED`且最多1000行，但调用事实仍符合BUG-052：全仓唯一执行点是管理员PUT保存后的同步`cleanupLogs`，应用runtime没有任何周期读取`logs.retention`的组件。有效源码RED直接证明预期Worker文件和启动注册均不存在。
- 不把自动调度塞入HTTP handler或复用用户活动保留Worker：专用日志与`user_activity_events`具有不同配置、表族和审计语义。新增LogRetentionWorker由应用runtime显式启动，启动后立即执行一次并每10分钟再运行；即使管理员从未再次保存，默认Enabled=true及持久策略也成为真实行为。
- 多实例用独立数据库连接持有`pg_try_advisory_lock(hashtext('mcmods-log-retention'))`；未获租约立即返回。每个DELETE继续是独立自动提交的1000行事务，一轮公平访问全部app类别及四张专用表；单次最多32轮且整体30秒，既能消化跨批积压又不会形成无界事务或常驻连接占用。
- Worker严格区分无设置行与读取/JSON故障：无行采用声明默认，损坏或数据库错误终止本轮并记录，DELETE任一失败也终止并等待下轮。Enabled=false零删除。释放租约使用独立5秒context；若解锁失败则Hijack并关闭物理连接，避免把持锁session放回池中。
- 真实PG插入1005条过期API日志、每张专用表一条过期/新鲜行，证明跨两轮精确删除且新鲜行保留；显式false保留旧行；第二连接持锁时Worker零删除，释放后恢复。初版夹具只设defaultDays=1却忽略normalize会补类别默认90–1095天，按真实策略正确不删除；显式设置五类别为1天后行为GREEN，这次不计产品失败。
- 无DDL、generation、公开配置DTO或前端变化，保持148；保存时既有同步清理和deleted map暂留以避免在本项扩大API语义，LEGACY-010另行决定移除。BUG-052按合规/容量承诺长期失真定Medium/P1；读取授权未扩大且数据未被篡改，故不定High。BUG-053错误伪成功、SEC-020敏感query和TEST-025广域矩阵保持独立。

## D-236：日志策略保存成功不能掩盖逐表清理失败

- BUG-053的读取证据已由两个已关闭根因真实覆盖：ARCH-013让共享`querySimpleRowsWithContext`返回Query/Values/rows.Err并由调用方失败关闭，PERF-033又把当前管理员日志入口迁到`loadLogPage`，完整row stream错误统一500。当前源码复核没有遗留返回空数组或跳过坏行的日志调用方，因此不重写第二套collector。
- 清理半项仍存在：`updateLogConfig`先成功提交system setting，再调用返回裸map的`cleanupLogs`；每个DELETE错误只是不写对应key，Handler无条件200。真实PG RED用statement trigger拒绝permission表删除，旧响应仍200，其他成功类别正常出现而permission_change悄然缺失，管理员无法区分0行与失败。
- 不把配置保存和十类清理强塞入一个长事务：策略提交是独立权威事实，其他表的有界清理可部分成功且后台Worker会按已保存策略重试。新cleanup继续尝试全部表，分别累计真实RowsAffected、稳定顺序failedCategories和带类别的joined error；服务端记录含底层数据库原因的错误，客户端只收到不泄露SQL的类别明细。
- 任一失败时PUT返回500、稳定code `LOG_CLEANUP_FAILED`和details `{config,deleted,failedCategories}`，明确“配置已保存、部分清理失败”。健康成功响应继续是既有`{config,deleted}` 200；Enabled=false仍健康200/空deleted。第一方前端已通过apiRequest catch显示失败，不需要DTO或提交顺序迁移。
- 同一真实PG测试用缺列temp `app_logs`让当前读取投影失败并确认500；随后注入单表DELETE故障，确认其行保留、四个健康来源各删除1、策略enabled持久为true且响应稳定500。错误原文只进入运行日志，不进入HTTP details。
- 无DDL、索引、generation、重置、回填或双写，保持148。BUG-053按事故响应/保留操作被伪事实误导定Medium/P1，与ARCH-013一致；无权限扩大或日志篡改，故不定High。PERF-033、BUG-052、SEC-020与TEST-025各自责任保持开放/既有状态。

## D-237：运行日志必须显式拥有slog与legacy log的共同sink

- 审计证据指出Install只替换标准logger output且未配置slog Handler。用当前Go1.26实测后需补充精确事实：默认slog会间接经过标准log，因此两个结构化record并非完全缺失；但这是运行时默认实现的隐式耦合，Store只用字符串猜级别，Info record的合法属性`error=none`被判成Error。有效RED同时证明attrs确实存在但级别事实错误。
- `Install`现在显式创建`slog.TextHandler`并设为默认logger，Handler writer是`io.MultiWriter(os.Stderr,processStore)`；终端和后台有界环接收同一序列化字节。TextHandler保留record time、权威level、message、普通attrs和嵌套group，Outbox、通知、未读及目录的现有`slog.Error/Warn`无需迁移。
- 直接依赖`slog.SetDefault`自动桥接标准log会把全部`log.Printf`标成Info，反而破坏既有failed/warn推断。专用legacy writer取消旧log时间前缀，按既有关键词映射Info/Warn/Error，再作为真正slog record进入同一Handler；于是旧调用也使用统一时间/级别格式，不重复时间戳且不退化错误级别。
- Store在关键词前先解析`level=ERROR/WARN/INFO/DEBUG`，结构化属性中的error/warn文本不能覆盖record level；同时解析TextHandler的RFC3339Nano `time=`作为CreatedAt，保持后台from/to筛选。旧格式行、partial write、32KiB单行上限、5000项ring及覆盖ID语义继续兼容。
- 管理runtime-log Handler仍在返回前复用`redactLogText`，权限、no-store、afterId/reset、level/q/time/limit均不变；Entry JSON结构不变，Line从标准log文本统一为slog text。stderr仍是运维实时输出，Store仍有意随进程重启丢失。
- 无Schema、generation、API DTO、前端类型或持久化变化，保持148。BUG-054按关键故障后台可见性/级别错误定Medium/P1；不直接改变授权或业务事实，故不定High。TEST-025完整HTTP过滤/脱敏与更多受控生产者矩阵仍独立开放。

## D-238：数据库会话UTC是所有date日桶的运行时权威

- 审计所述混用在当前源码中仍成立：活动批处理从事件时间显式取UTC date，但站点浏览、当前计数、热度事件、快照和初始化查询依赖`current_date`；连接池只固定application name与超时，没有固定TimeZone。有效真实PG RED把DATABASE_URL显式设为`Pacific/Kiritimati`，application与activity两池均保留该值，固定`2026-08-23 23:30Z`被转换成`2026-08-24`，精确证明同一瞬间会跨桶。
- 选择连接级UTC作为单一权威，而不是在每个查询复制部分日期表达式。`connectPool`在`pgxpool.ParseConfig`之后写入`RuntimeParams["timezone"]="UTC"`，因此数据库默认、容器主机和URL显式参数都不能改变运行时日界；现有`current_date`、date默认值和PL/pgSQL函数与已经显式`at time zone 'UTC'`的事件投影自然一致。
- 主应用与高频activity池本来共用`connectPool`，因此一次修复覆盖HTTP、Worker、migration和活动摄取。`cmd/user-statistics`及`cmd/db-reset`原先自行`pgxpool.New`，现迁入`database.Connect`，避免维护命令重新引入另一会话合同，并同时继承连接预算、dial/statement/lock/idle transaction超时。只读load observer不产生任何业务date事实，保留其可注入独立观察连接。
- 真实PG GREEN逐一连接两个池，确认`current_setting('TimeZone')=UTC`且固定瞬间date为`2026-08-23`；测试特意保留冲突URL参数，锁定应用必须覆盖而不是仅依赖部署约定。相关database/httpapi/activity/userstats/app及全部命令回归通过。
- 无DDL、Schema、索引、约束、generation、重置、回填或双写，保持148；现有数据库从下次新建连接立即生效，API/DTO和前端不变。BUG-055按可永久错分日活、浏览和热度的运营事实定Medium/P1，但无越权或数据泄露且总量可重算，故不定High；TEST-027/028更广跨时区/边界矩阵仍独立开放。

## D-239：热度排除是developer集合成员关系，不是owner标量

- 当前Schema把`effective_project_access`中全部developer按user_id排序只取一人，随后评分、收藏、评论和独立访客都与该标量比较；开发者列表又把相同标量作为developer与原权限集合UNION。真实PG在完整临时Schema建立两名有效developer及一名普通用户，旧增量事实为2个view/favorite/comment/commenter/rating、评分和为9，trend为8/6/4；正确事实只能是普通用户的1/5与4/3/2，精确复现审计。
- 不新增“主owner”角色，也不把显示作者名当授权。唯一排除权威仍是`effective_project_access`中`access_level='developer'`的route/user membership；直接个人作者、团队作者或同一用户的多条来源都按集合存在性解释，editor和无授权展示作者不自动排除，Minecraft server若没有该关系仍保持既有普通参与语义。
- 删除`content_target_owner_id`，新增boolean `content_target_user_is_developer(route,user)`；评分、收藏、评论的incremental累计和daily trend，以及独立访客事实统一调用。rating更新分别按old/new route与author判断，避免对象迁移时复用单一route。全局rating rebuild用`route_developers`集合CTE anti-join；route lifetime/dimension rebuild直接对effective developer集合做NOT EXISTS，不把多值再次压缩。
- 开发者列表删除标量UNION，只对effective access集合按user去重并保留developer优先于editor的既有排序。生产源码因此不再存在owner scalar、developer min、按user limit 1或route_owners映射；MAP-007与BUG-057由相同改动分别以映射合同和行为结果关闭。
- 旧增量聚合只保存count/sum，无法识别其中哪部分来自第二开发者，故不能安全原位减算。generation从148提升到149并要求开发数据库整代重置；本机明确授权的127.0.0.1:55432/postgres在8.486s内完成reset/seed，无远端写入、在线回填或双读双写。
- GREEN同时验证incremental、故意损坏后的route/global rebuild和维度/趋势；10M rating校准让两个developer同属一个route，精确排除100k行且rebuild 6.619s，低于15s门。BUG-057与MAP-007均定Medium/P1：可扭曲本人项目排名但不越权或泄露数据；DEAD-008与TEST-028其余责任保持开放。

## D-240：搜索快照与增量消费共享数据库代次租约

- 旧Worker只把`search_index_rebuild_progress`当观察记录。实例A读完某类快照后，实例B可用`FOR UPDATE SKIP LOCKED` claim同一目标的变更、把它写入旧alias并按updated-at token删除queue；A随后激活不含该变更的新collection，而projection state仍显示ready。有效真实PG RED在代表重建的独占lease已持有时启动第二Worker，旧实现仍在0.35秒内完成claim、Typesense delete和ack。
- 不把进程内Ready或mutex当协调事实，也不依赖时间戳猜测重放边界。所有实例使用同一PostgreSQL advisory key：rebuild获取独占session lock，drain获取共享session lock。独占请求会先等待在途drain的外部写和精确ack完成，再阻止任何新claim；此后提交的数据库变更仍可正常enqueue，但直到alias切换结束前不能被消费或删除，持久queue本身就是需要重放的delta log。
- 获得独占lease后必须重新执行`projectionsCurrent`。这使同时发现旧schema的多个实例只有首个执行重建，后继实例等待后观察新state并退出，不创建第二套collection。一个独占lease覆盖全部collection创建、稳定页读取、import、逐alias激活、state记录和旧collection清理；释放后普通共享drain把期间积累的事件写入当前alias。
- lease持有连接同时成为该操作全部数据库Query/Exec/Begin的执行边界，不额外消耗第二连接，因此MaxConns=1仍可工作。解锁用独立5秒context；若响应失败或服务端未报告持锁，连接从pool Hijack并物理关闭，禁止带session lock的连接被复用。锁获取结果不明确时同样关闭连接。
- 新隔离Schema集成测试实际创建两个Worker：A持独占lease时，B在250ms观察窗内queue attempts和Typesense调用均为0；A释放后B恢复并恰好一次删除目标、queue归零。源码合同另锁定exclusive/shared、获锁后二次state检查和leased connection。既有稳定500行页、100k/1M keyset与完整searchindex真实PG包均通过。
- 无DDL、Schema、索引、API、Typesense collection schema或generation变化，保持149，无重置/回填/双写。BUG-058按ready搜索永久漏/旧文档定High/P1；PostgreSQL权威事实未损坏且不越权，故不定P0。OPS-011仍负责新旧二进制滚动版本回切，TEST-030负责完整故障状态机，MAP-008负责注册表集中化，本租约不冒充关闭它们。

## D-241：gzip只接受有效权重且显式coding优先于wildcard

- 旧`acceptsGzip`在`strconv.ParseFloat`失败时执行`err != nil || quality > 0`，所以错误恰好变成允许；它又按header从左到右遇首个可接受候选就返回，使`*;q=1,gzip;q=0`在看到显式拒绝前已压缩，逆序也会在跳过gzip后被wildcard重新允许。新可压缩text RED中非法、空、越界q、非法wildcard和两种显式/wildcard顺序共6分支失败。
- 不能只把条件改为`err == nil && quality > 0`：这虽修正原始abc，却仍接受ParseFloat支持但HTTP qvalue不允许的范围/形状，并保留wildcard顺序缺陷。新解析器只接受整数0/1及最多三位小数；1的小数只能为0，0的任一非零小数才是正权重。缺等号或重复q同样无有效协商。
- 遍历header时分别收集显式gzip与`*`。只要出现显式gzip，就完全按其有效权重决定并忽略wildcard；没有显式项时才使用有效wildcard。这与具体顺序无关，也保留`br;q=1,gzip;q=0.25`、默认gzip和合法wildcard。
- 测试必须经过真实compression middleware和`text/plain`响应观察Content-Encoding，不再用不可压缩PNG让`q=0`碰巧通过。12分支覆盖默认/正权重、wildcard、零权重、非法/空/越界权重、两种优先级顺序和无支持coding；既有JSON可解压、binary跳过、Vary及writer能力路径保持。
- 无Schema、数据库、API DTO、路由、状态码、body或generation变化，保持149。行为收紧只让无效或明确拒绝gzip的请求返回identity。BUG-061保持Low/P2：影响异常客户端/代理的响应可读性，但不涉及授权、隐私或持久事实；TEST-032其余资源版本、监控、死信和更广压缩矩阵不随本项关闭。

## D-242：空库兼容范围必须是unknown而不是全loader全版本

- 旧默认先把33个Minecraft版本code复制给13个loader，所以未同步的空库会把release、snapshot、april_fools和legacy全部宣称为Fabric/Forge/NeoForge及非自动同步loader可用。有效纯单元RED直接观察Fabric伪报33项。
- 仅清空默认loader范围仍不完整：模组内容没有项目专属兼容记录时，`globalModContentCompatibilities`忽略loader自身范围，再次把完整全局目录赋给每个loader。第二条RED用Forge仅`1.20.1`和空Babric证明旧回退把两者都扩张为`1.20.1 + 24w14potato`。
- 跨端盘点又发现工作区有同构旁路：项目没有compatibilities时，`ModContentWorkspace`构造`allMinecraftVersions`并把它赋给每个loader。独立前端RED在旧源码3/4通过、精确命中这一赋值；修复后直接复制`loader.versions`，空数组保持空且不共享可变引用，服务端422防线和用户可见选择范围由同一事实驱动。
- 采用审计建议允许的保守语义：默认继续提供版本目录、common版本和loader名称供界面展示，但每个loader的`versions`是非nil空数组，序列化为JSON `[]`并表示尚无已验证兼容性。持久化同步/管理员配置一旦提供范围，所有fallback严格逐loader复制该范围；空范围使既有组合校验返回false，而不是退回全目录。
- 不在本项引入启动同步或集群租约。等待每日同步只影响可用性，不再产生虚假断言；4个自动来源之外的loader保持unknown才是保守真值。OPS-012继续负责跨实例同步写入覆盖，BUG-063/064/065继续负责NeoForge解析、大小写唯一性和预检/创建快照，TEST-034继续负责MRPack完整矩阵。
- 真实PostgreSQL临时`system_settings`缺行测试同时验证内部加载和公开HTTP JSON：13个loader均是显式空数组；损坏设置仍由ARCH-016路径500。前端定向4/4、ESLint/Type及全量205项/production build通过。无Schema、数据、路由、DTO形状、状态码或generation变化，保持149。BUG-062按审计High/P1关闭：错误组合可进入内容/导出流程并产出无法运行的包，但不涉及越权、泄露或已持久权威事实损坏，故不定P0。

## D-243：NeoForge artifact映射按两代官方版本分量而非位置启发式

- 旧函数对除1.20.1以外的字符串取第二段和最后一段：`1.21`因此成为`21.21.`，`26.2`成为`2.2.`。双Maven模拟RED只解析4个目标中的1.20.1与1.21.1；1.21和26.2即使目录存在稳定artifact仍被静默省略。旧稳定筛选只查子串beta，还会在47.4.0之上选择47.7.0-rc。
- NeoForged官方版本规则明确：1.21及旧式Minecraft用NeoForge `minor.patch.build`，缺失Minecraft patch视为0；26.1起改为四分量，前三分量对应Minecraft `year.release.patch`，最后才是NeoForge build。官方目录也实际发布`26.2.0.61`。据此建立封闭映射：1.20.1保留legacy坐标`1.20.1-`，其他1.20+使用`minor.patch.`，26+使用`year.release.patch.`，两类缺patch均补0。（参考[NeoForged Versioning](https://docs.neoforged.net/docs/gettingstarted/versioning/)及[官方Maven目录](https://maven.neoforged.net/releases/net/neoforged/neoforge/)）
- 不再对snapshot、April Fools、Minecraft pre/rc或1.19及更早值拼宽松前缀；解析失败返回error并由既有同步省略/状态机制记录。这样无效输入不会产生空前缀全目录匹配，也不会冒充一个推测artifact。1.20.2、1.21.11和26.2.1等合法release则由相同数字分量规则覆盖。
- 稳定artifact定义为移除已验证prefix后只剩非空、点分隔的十进制分量；alpha、beta、rc、snapshot及其他限定符一律拒绝，不因build更高获选。legacy Forge/NeoForge的Minecraft前缀分隔符先移除，所以47.1.106等仍合法；modern 21.x/26.x保留完整loader版本写MRPack依赖。
- 定向模拟从modern源选择21.0.168、21.1.241、26.2.0.61，从legacy源选择并裁剪1.20.1-47.1.106；更高beta/alpha及rc均未胜出。现有真实PG测试再证明输出仍与catalog hash原子持久化且导出读取零远端外呼。无Schema、路由、DTO或generation变化，保持149；BUG-063按原Medium/P1关闭，BUG-064/065、MAP-009及TEST-033/034不随本项关闭。

## D-244：loader code先规范化并大小写唯一，name不承担身份

- 旧规范化只以原始trim后字符串做map key，所以`Forge`和`forge`同时保留；同步的`findMinecraftLoader`却EqualFold并只返回第一项，LoaderSync状态也按小写去重。有效后端RED证明`fOrGe`/`MyCustomLoader`完全保留且重复不报错；前端新增入口又只做严格相等，允许管理员在已有Forge旁加入forge。
- 不静默取第一项或合并版本范围：两个重复条目的范围可能互相冲突，自动选择一方会丢管理员事实。normalize先把内置13种code映射到既有公开规范拼写（Fabric、Forge、NeoForge、LiteLoader等），自定义code统一小写，再以fold key检测；重复即requestError。管理PUT因此在任何保存前400，已持久重复则由ARCH-016包装为unavailable并使公共/后台500，要求显式修复而不是伪健康目录。
- `name`继续原样trim并用于本地化/人类展示，空name才回退canonical code。LoaderSync code经过同一canonical函数，查找双方也先canonical后精确比较；配置、状态和同步结果不再使用不同的大小写语义。合法旧存储只在内存规范化，不需破坏性回填；下一次管理员保存或同步会写canonical形态。
- 自定义项目兼容记录可能保留旧大小写。服务端本来就对组合校验fold比较；前端新增`minecraftLoaderCodeKey(trim+lower)`并用于管理员重复预检、CompatibilityEditor选择/删除/展示以及relationship范围/名称查找，使旧`MyCustomLoader`可继续匹配新`mycustomloader`，同时所有新保存都使用配置返回的canonical code。
- 真实PG临时设置验证重复PUT=400且数据库零写，直接放入重复JSON后load为zero/error；合法Forge仍成功。无DDL、Schema、generation或字段变化，保持149。BUG-064按原Medium/P1关闭：影响目录与同步一致性但不构成授权/隐私问题；BUG-065、OPS-012和TEST-033/034保持独立。

## D-245：MRPack创建只消费有期限的不可变预检快照

- 旧preflight与create分别调用`buildFavoriteModpackExportPreview`。ARCH-017虽已让每次调用从站内持久artifact读取，但两次请求之间的同步事务仍可替换loader版本，Modrinth缓存、收藏内容和依赖也可变化；create因此保存一份自洽却不是用户实际确认的任务快照。有效RED同时证明请求/响应没有preview身份，前端只能重发version/loader。
- 选择持久服务端快照而不是只签loader字符串：用户确认的是完整导出清单，单独冻结loader仍允许具体Mod文件或依赖漂移。`favorite_modpack_export_previews`保存公开preview以及不应下发浏览器的`source_project_route_id`封装，摘要覆盖两者；owner、collection、Minecraft/loader/version列作为独立镜像，读取时与JSON、路径及请求逐项核对，数据库损坏不能伪装合法确认。
- preflight成功后生成9字符ID、SHA-256及数据库权威15分钟期限。create要求ID/hash，事务内`FOR UPDATE`读取；缺失、过期、已消费、摘要/设置不符分别409失败关闭。通过业务校验和限额后，task、全部items、queue outbox及preview `consumed_at`在同一事务提交；任一失败回滚，快照在期限内可重试，并发第二个创建只能看到已消费状态。
- 公开DTO新增`previewId/previewHash/expiresAt`但继续隐藏内部route ID。Next客户端把收到的preview对象直接交给create API，version/loader也从该对象读取，避免UI当前选择与确认对象混用。旧新端混用不能维持冻结保证，因此这是pre-production同版本协调发布，不保留降级到实时重建的兼容分支。
- generation149→150且无可可信回填：历史任务已经是最终快照但不存在独立用户确认事实，不能伪造preview行。授权本机开发库按保护协议reset/seed到150；完整临时Schema隔离、全部FK前导索引及public Migrate通过。真实handler测试模拟确认21.1.100后目录变21.1.999，最终task仍为21.1.100且item/内部route不丢，重复消费409、错误hash拒绝。BUG-065按Medium/P1关闭；BUG-067/068/069、DEAD-011、OPS-013和TEST-034保持独立。

## D-246：导出报告名称按当前收藏三类闭集显式投影

- 审计证据举出的插件、地图和光影属于旧假设：generation89已经把收藏产品闭集固定为Mod、整合包、蓝图，应用白名单、所有读/写可见性和`favorite_collection_items` CHECK共同拒绝其他类型，不能为关闭BUG-067重新扩大产品范围或引入无用simple-project联接。
- 问题仍在蓝图上成立。导出批量查询复用了包含`blueprints`的`favoriteTargetJoinsSQL`，却只以`coalesce(mods.primary_name,modpack.primary_name,'')`保存报告名；蓝图正确产生`NOT_A_MOD`，但名称为空。有效真实PG RED在同一收藏的三类中只精确失败蓝图。
- 新`favoriteTargetNameSQL`按`item.entity_type`显式选择Mod `primary_name`、整合包`primary_name`或蓝图`title`，继续置于原单次收藏快照查询中，不增加N+1、额外数据库请求或新的显示回退。闭集外分支为NULL，但其在相同WHERE/CHECK下不可达。
- 公开preview/task item DTO本来就包含`sourceProjectName`，前端列表和复制失败名称已消费该字段，所以无需客户端或Schema迁移。真实PG验证三类权威名称精确返回，整合包/蓝图仍为`NOT_A_MOD`，Mod缺外部来源的原reason也不改变。无DDL且generation150保持；BUG-067按Medium/P1关闭，BUG-068/069、DEAD-011及TEST-034保持独立。

## D-247：收藏只是导出来源血缘，不能拥有任务与报告生命周期

- 旧task虽保存pack name、loader、统计和完整item快照，`collection_id ON DELETE CASCADE`却仍把它当收藏子对象；删除收藏会同时删除pending/processing/terminal task及全部报告。Worker消息随后找不到任务，而已上传文件可能失去到期清理状态。有效真实PG RED在204删除后精确得到0 task/0 item。
- 不把terminal report复制到另一套表或阻止用户删除收藏。task的`collection_id`改为nullable `ON DELETE SET NULL`，新增创建时强制写入的`collection_public_id_snapshot`；公开history/detail直接读快照，删除来源后继续返回原`collectionId`，且去掉不必要inner join。pack name和items本来已是快照，无额外复制。
- 删除handler在事务中先锁定属于当前用户且非default的collection，再把其pending/processing任务置既有`cancelled/cancelled`，写`SOURCE_COLLECTION_DELETED`、finished时间、清lease，最后删除collection。任何取消或删除写失败都回滚；既有default/missing冲突与数据库500分类保持。ready/failed/expired/cancelled不被改写，报告和结果文件继续走原下载/expiry路径。
- 必须处理删除与Worker完成竞态。Worker生成并登记OSS文件后，不再以无锁单条CAS直接ready；它开启事务锁task行。若仍为当前processing lease则连同文件链接原子ready；若删除事务已把它取消，则关联生成文件用于审计，调用统一tombstone并在同事务写可靠删除Outbox，返回非完成且不发送完成通知。反过来若完成先持锁变ready，随后删除只detach而不取消，文件/报告保持正常。
- generation150→151，新增nullable FK、9字符snapshot及`(collection_id,id) where collection_id is not null`前导索引。历史task不存在可靠来源公开ID回填合同，pre-production整代reset，不保留双协议。真实PG验证删除后0/4/4、两active取消、两terminal不变、history/detail仍可读、旧queue no-op；模拟取消胜出后的文件为deleted且Outbox pending。BUG-068按High/P0关闭；OPS-013仍负责非取消类ready写入故障补偿，BUG-069/DEAD-011/TEST-034独立。

## D-248：ready必须由任务统计、完整报告与实际MRPack索引三方一致证明

- 审计记录的单行Scan错误`continue`和遗漏`rows.Err()`已由ARCH-018先行删除，损坏行现在会写`REPORT_LOAD_FAILED`并按lease重试；BUG-069不能重复关闭同一修改。仍有效的缺口是：Worker只查询`exported/auto_dependency`行，合法但缺失的报告行不会产生Scan错误，也没有与task六项统计比较。真实PG行为RED让task声明2个exported但只保存1行，旧代码仍成功构建一文件索引并一直走到OSS配置，最终错误错误地成为`OSS_UPLOAD_FAILED`。
- Worker改为一次读取task的`collection_item_count/exported_mod_count/auto_dependency_count/skipped_item_count/failed_item_count/final_file_count`，并按ID扫描该task全部`exported/auto_dependency/skipped/failed`报告。四类逐项计数，未知类型显式失败；所有字段仍严格Scan并检查终端`rows.Err()`，所以ARCH-018的可见性保证未被计数逻辑替代。
- 在构建前要求task和report各自满足`collection=exported+skipped+failed`、`final=exported+auto_dependency`，且六项逐项相等。这样缺行、多行、分类漂移、负统计或task内部矛盾都在任何OSS调用前以`REPORT_INCONSISTENT`进入既有有限重试/终态失败状态，不会降级为一个较小的包。
- `buildMRPack`额外返回它实际写入`modrinth.index.json`的文件数；构建后再次把该数与已经一致的`final_file_count`核对，再允许上传和ready。生成器原有逐文件哈希、URL、环境、路径及ZIP验证不变；大小写路径冲突属于BUG-070，不能借本项计数相等冒充解决。
- 无DDL、持久数据或公开API变化，generation保持151；内部task `errorCode`可出现`REPORT_INCONSISTENT`，前端本来按通用字符串展示，无锁步迁移。单元覆盖缺行、task矛盾、分类漂移与索引漂移；真实PG证明task2/report1在上传前失败；完整FavoriteExport/MRPack、ARCH-018和BUG-068竞态回归通过。BUG-069按原High/P0关闭；OPS-013、DEAD-011及TEST-034仍独立。

## D-249：MRPack文件身份按跨平台规范键在预检与生成器双重唯一

- 旧preview只判断各项目能否独立选择文件，不维护最终`mods/<filename>`集合；生成器也只用原始、区分大小写的Go string去重。有效行为RED证明`Example.jar/example.jar`、`Café.jar/Cafe◌́.jar`和`modﬁle.jar/modfile.jar`三组都可同时进入一个索引，冲突直到大小写不敏感或Unicode规范化客户端落盘才显现。
- 不自动重命名第三方JAR，也不任选一个项目：文件名可能参与loader发现、用户排障和上游签名语义，猜测消歧会把冲突隐藏成内容变化。预检在依赖图完成后、汇总计数前，对全部`exported/auto_dependency`候选建立portable key；同key组的每个具体item都改为`failed`、reason `FILE_PATH_CONFLICT`并保留项目名、原文件名、哈希和dependency来源供用户定位。
- portable key先执行生成器原有安全`mods/`单层JAR路径校验，实际索引路径规范为NFC；比较键再做NFKC兼容归一化、重新安全校验和`x/text/cases.Fold`完整Unicode case-fold。NFKC有意保守合并兼容字符，避免同一包在Windows、macOS及解压工具的不同大小写/规范化语义下产生两个逻辑文件。provider候选也必须通过同一路径边界，非法名称在预检按既有`NO_COMPATIBLE_FILE`处理而非拖到Worker。
- 任一conflict使create在empty/compatible-only之前返回422 `MODPACK_EXPORT_FILE_PATH_CONFLICT`和同一preview；事务不消费preview、不创建task/outbox，用户修正来源后仍可重新预检。即使内部调用绕过预检，`buildMRPack`也按相同portable key拒绝并指出两条原路径，形成纵深防御。
- 真实PG保存已标记快照后验证create专用422、task=0、consumed=0。广域测试暴露PERF-040夹具把149个不同依赖都伪造成`dependency.jar`；该数据在新规则下确实冲突，只把夹具文件名改为绑定project ID，150节点、1图查询、8并发及149依赖原意不变并全绿。无DDL，generation151保持；前端早已有中英文`FILE_PATH_CONFLICT`展示。BUG-070按Medium/P1关闭，BUG-071/072、OPS-013、TEST-034独立。

## D-250：历史重建必须让用户显式选择当前来源或原始报告

- 旧history/detail虽然保留Minecraft、loader和结果行，却不返回task已持久化的`allow_compatible_only`，也不暴露`report_version`；前端“相同设置”只把三个筛选值塞回表单并重新读取当前收藏。来源删除时它直接失效，来源变化时又会静默变成另一份清单，因此既不能证明原确认，也不能明确表达用户想重查当前状态还是复用原报告。
- 不把两个含义继续藏在一个按钮里。新增owner限定且受`favorite.modpack_export`权限保护的rebuild preflight，body闭集只接受`current_collection`或`original_snapshot`。current模式要求live collection，调用同一权威preflight builder重新执行收藏可见性、loader目录、provider、依赖图和路径检查；来源已detach时返回409 `MODPACK_EXPORT_REBUILD_SOURCE_UNAVAILABLE`，不偷偷回退原报告。
- original模式只读owner自己的task与逐项items快照，不访问当前收藏或provider。它要求受支持的report version，严格扫描/解码所有行，重新证明task/report六项统计相等及分类和，逐文件复核任务Minecraft/loader、官方Modrinth URL身份、SHA-1/SHA-512、正size、环境闭集和portable路径唯一；未知分类、缺行、损坏JSON或任何漂移返回409 `MODPACK_EXPORT_REBUILD_REPORT_INVALID`，不能用部分事实构造preview。该模式是用户显式选择的历史事实重放，不冒充“最新兼容性”。
- preview哈希现在覆盖`allowCompatibleOnly/reportVersion/rebuildSource`。original create还要求请求的两个compatible-only布尔值精确等于原任务确认；随后继续走现有path/empty/required dependency、用户并发/日限额/重复冷却、事务消费、task/items和queue outbox。detached task写NULL collection FK但保留公开ID快照；current/普通preview若随后detach则create因source/collection镜像不一致失败关闭。
- 为让15分钟preview与收藏删除共存，generation151→152把preview collection FK改为nullable `ON DELETE SET NULL`，新增非空`collection_public_id_snapshot`和`source_mode`闭集。不能加“current必须非NULL”的数据库CHECK，因为FK的SET NULL会使删除收藏本身失败；该状态组合由持久化/读取应用不变量拒绝消费。开发期整代reset/seed到152，无不可信回填或双协议。
- history/detail新增`allowCompatibleOnly/reportVersion`；preview新增同字段和`rebuildSource`。Next客户端用“重新检查当前收藏”和“按原始报告重建”两个双语动作调用新端点，并以`preview.allowCompatibleOnly || hasOmissions`提交确认。真实PG覆盖live current成功、删除（含已有current preview）成功、detached current 409、original成功及新task NULL FK/原snapshot/原确认/items；BUG-071按原Medium/P1关闭。未使用`report_snapshot`的DEAD-011、BUG-072、OPS-013和TEST-034仍独立。

## D-251：导出Minecraft版本身份来自站内目录与loader启用闭集

- BUG-064/ARCH-017已经把loader版本解析改为当前catalog hash绑定的数据库artifact，任意请求不再直接进入外部版本URL；但请求边界仍只判断非空和`.X`后缀。有效RED中`9.9.9`穿过decode和收藏查询，最后被误报为`MODPACK_EXPORT_LOADER_SNAPSHOT_UNAVAILABLE`，无法区分非法Minecraft身份与一个已启用版本的同步artifact暂缺。
- 定义一处Minecraft版本code语法：trim后必须保持原值，1–80字节，以ASCII字母/数字开头，余下只允许字母数字、点、下划线、连字符、空格、括号、单引号和加号。该闭集覆盖默认/真实目录中的release、snapshot、pre/RC、April Fools和legacy例子（包括`3D Shareware v1.34`与`1.RV-Pre1`），同时拒绝斜线、控制符、Unicode伪装和超长值。站内目录normalize也应用同一规则，不能让管理员保存一个HTTP边界随后无法安全消费的code。
- 仅“存在于Versions”仍不够；MRPack要为选定loader可用。`minecraftVersionEnabledForLoader`先精确匹配权威version code，再大小写无关匹配Fabric/Forge/NeoForge目录项，并要求code出现在该loader的`Versions`启用集合。这个检查发生在catalog读取之后、artifact hash/SQL读取之前；因此合法形状但未知或loader不支持的版本稳定成为version错误，已启用但artifact意外缺失仍保留ARCH-017的loader snapshot错误和运维可观测性。
- 新sentinel同时包装既有artifact unavailable，使ARCH-017旧调用者的`errors.Is`兼容不破坏；HTTP优先映射它为422 `MODPACK_EXPORT_INVALID_MINECRAFT_VERSION`。目录读取/JSON/normalize失败仍500 catalog unavailable。请求/响应字段、preview/task的`minecraftVersion`字段及数据库Schema不变；被持久化的code现在已经由当前目录+loader enablement+同hash artifact三者证明。
- 真实PG用仅启用`1.21.1/fabric`的同步catalog验证：`9.9.9`和`../../1.21.1`均422且不保存preview，`1.21.1`成功且只保存一条；同时重跑离线artifact、BUG-071 detached rebuild和PERF-040 150节点preview。两处旧测试绕过发布API手插artifact的夹具改为调用`saveSynchronizedMinecraftVersionConfig`，让测试数据也满足生产不变量。无DDL且generation152保持；BUG-072按原Medium/P1关闭，BUG-073、ARCH-017和TEST-034独立。

## D-252：蓝图实体数据没有经过验证的格式映射时必须失败关闭

- 现有规范化文档能保存Vanilla structure解出的`blockEntities/entities`，但三个非JSON编码器都输出空实体集合或完全省略字段；同时Sponge、Litematic和旧Schematic解码器连源文件实体列表都不读取。有效RED证明两类实体乘三个目标格式共六个转换分支全部成功却丢数据，三类源格式也在带实体时成功规范化为空，Finding不是单一编码器遗漏。
- 本批不猜测跨格式NBT语义，也不把告示牌、容器、命令方块、刷怪笼或自由实体的供应商结构机械复制到另一格式。`errBlueprintEntityDataWouldBeLost`成为明确永久失败：规范化JSON仍可无损保存已有文档实体；只要目标不是JSON且文档含任一实体，编码立即拒绝。Sponge会检查根和Blocks容器，Litematic逐region检查，旧Schematic检查根；非空或类型畸形的实体容器都在读取方块前失败。
- 永久保真错误不消耗三次相同重试。`failBlueprintJob`在第一次attempt即把normalize/convert job置为failed并保存可见`last_error`；normalize失败仍按既有状态机把主体置failed，convert失败只影响任务，已经ready的原始蓝图保持ready。既有失败通知携带同一错误原因，不能再发成功通知或写ready variant。
- 普通无实体的NBT/Sponge/Litematic往返仍使用原路径。真实PG验证normalize和convert两种operation均在attempts=1终止，convert主体保持ready、normalize主体为failed；全Blueprint组同时覆盖租约、重试、Outbox、artifact补偿和空实体codec。无DDL、API/DTO/前端变化，generation152保持；BUG-076定为Medium/P1，因为生成variant可导致实际世界数据丢失，但原始对象不被删除、无越权或泄露。

## D-253：修订快照中的现有封面按资产关系授权，替换封面仍按上传者授权

- 蓝图即时编辑只接收标题、正文和本地化，不接收封面ID；Handler是在锁定蓝图后把当前`cover_file_id`的公开ID复制进快照。管理员通过`blueprintOwnerAccess`合法编辑他人内容时，旧apply却把管理员actor当成该现有封面的UploaderID重新解析，因此一个与封面无关的标题更新必然因owner上传的封面返回no rows。
- `applyBlueprintContentSnapshotTx`先以锁定的blueprint ID+snapshot公开文件ID联接当前cover关系。只有关系仍相同、文件active、扫描clean/trusted_generated且MIME属于受信光栅闭集时才复用；同时从数据库重取内部ID和object key并锁定文件，不能信任快照携带的`coverObjectKey`。这一步不授予对其他文件的访问，只承认蓝图已经拥有的绑定。
- 如果公开文件ID不是当前绑定，继续调用统一`resolveTrustedRasterOSSFilePublicID`并要求实际actor是上传者；因此管理员不能借metadata入口替换成任意owner文件，owner仍可换成自己另一张受信图片。审核批准路径继续把提交者ID作为actor，语义不变。
- 真实PG以owner=700、admin editor=701证明现有cover的metadata更新成功且恶意snapshot key被数据库key覆盖；同一管理员提交未绑定的owner cover仍拒绝并完整回滚，owner本人替换成功。无DDL、路由、DTO、前端或generation变化；BUG-077定为Low/P2，因为它只阻断授权管理员的特定编辑操作，不破坏现有内容、权限或资产。

## D-254：蓝图上传业务主体必须与有期限的外部上传会话同生共灭

- 旧预签名入口先提交`blueprints(status=uploading)`和中文`content_localizations`，再生成对象Key、预留额度、初始化/登记multipart或调用provider presign。后续任一步失败只释放部分额度，用户放弃成功返回的链接也没有回调；Maintenance没有蓝图uploading回收，因此主体及多态投影会永久残留。有效真实PG RED让两个已过期uploading、一个fresh upload、一个queued蓝图及其投影全部保留，Schema RED仍为generation152且无期限列。
- 不从`updated_at`猜测超时：请求默认10分钟、最大60分钟，multipart和普通presign都已有同一精确`uploadExpiresAt`。创建蓝图及首个本地化的事务现在同时写`upload_expires_at`，成功完成并转queued时清NULL；所以Maintenance只根据创建时承诺的会话边界判断，后续无关更新时间不会延长或缩短上传能力。
- 已知失败路径应即时补偿。对象Key持久化行数异常、额度拒绝、multipart初始化失败、multipart会话登记失败、普通presign失败都会释放已有额度并按owner+蓝图ID锁定仍为uploading的行；显式multipart abort按owner+object key做同样操作。统一事务helper先删除`content_localizations/content_subjects`的blueprint投影，再删除主体并要求精确行数，错误不能被伪装成清理成功；wrong-owner或已经完成/删除的会话是安全幂等no-op。
- 客户端在拿到链接后直接离开仍需独立收敛。既有Maintenance每轮先以`(upload_expires_at,id)`部分索引按稳定顺序取得最多1000个到期uploading ID，`FOR UPDATE SKIP LOCKED`避免多实例互等，并在同一事务调用相同删除helper；满批继续、短批结束，30秒总周期预算保持。queued/ready/failed/deleted和NULL期限行均不可命中，不删除已登记OSS文件或已完成蓝图。
- generation152→153，只新增nullable期限和上述部分索引；历史pre-production数据没有可信原始签名期限，按开发期整代reset/seed，不做`updated_at`回填、双读或双写。预签名/multipart/complete/abort公开路由、DTO、状态码和既有10–60分钟语义不变。真实PG覆盖按ID/Key即时删除、wrong-owner、expired/fresh/queued边界及投影一致性；BUG-079定Low/P2，因为影响是孤立元数据、目录投影与运维噪声，不会删除完成内容、产生跨用户授权或泄漏对象。

## D-255：举报附件的文件事实与证据事实必须一次提交，旧孤儿只能按完整身份恢复

- 旧完成入口先用数据库自动提交插入`oss_files`，随后另一次自动提交插入`report_evidence`。一次性trigger模拟短暂证据登记故障时，真实Handler返回500后数据库精确留下1 file/0 evidence；重试虽在object-key冲突后找到active文件，却走通用completed响应，返回文件public ID而不补建证据，用户的成功外观无法进入举报提交链。
- 不通过删除冲突文件或为证据再建异步队列掩盖分裂。report-evidence scope在验证provider对象后开启专用pgx事务，文件INSERT和证据INSERT使用同一transaction QueryRow；证据INSERT或commit失败时二者同回滚。因此瞬时故障后对象仍可由相同完成请求重新验证，第二次事务提交1/1，commit结果不确定时也会在重试中落到严格恢复边界。
- 兼容修复前已经形成的1/0孤儿。文件唯一冲突后先用现有completed查询要求当前uploader、category、source、SHA-256、active状态、object-key集合及可选source size完全匹配，再开启事务按file ID重锁并复核同一闭集。证据不存在才用数据库文件的original name、content type、source size、hash和scan status补建；若证据已存在，则这些字段、uploader及temporary状态必须逐项一致才幂等返回。
- 原先举报早期幂等只按owner+key，可让客户端提交不同hash/size仍得到旧证据成功。现在查询同时绑定SHA-256和可选byte size；不匹配会继续进行provider metadata验证并按400/冲突失败，而不是复用旧证据。恢复也固定`source='report_evidence'`和请求解析出的用户专属category，不能把普通文件或另一用户对象升级为治理证据。
- 无Schema、索引、generation或公开协议变化，保持153；两表既有object-key唯一事实足够串行并发完成。首次有效成功仍201并以`id/evidenceId`返回证据ID，幂等/旧孤儿恢复仍200；真实PG+fake OSS证明一次性故障0/0、同请求1/1、第三次同ID、错误hash拒绝及旧孤儿补建。BUG-080定Medium/P1：它会永久阻断举报材料进入治理链，但不泄露跨用户对象、不篡改内容，也不删除已绑定证据。

## D-256：评论提交只承诺已持久化的日志处理任务，不同步承诺外部脱敏完成

- 旧`createComment`先提交评论、普通`comment_attachments`和watch事实，再逐文件调用`bindCommentLogAttachment`。该函数又分别把attachment设processing、同步GET OSS/脱敏/写ready share、插binding和设ready；多个UPDATE还吞错。任何数据库或provider故障只留日志，HTTP评论已成功且幂等重试直接返回旧评论，不会重新执行关联，failed/processing因此永久化。
- 不把OSS GET和最长32MiB解压/脱敏拉回评论事务。`bindCommentAttachmentsTx`成功后、watch与comment commit之前，以已绑定且仍属于作者的文件为闭集读取最多5项，Go中用现有NFC/扩展名规则识别`.log`与含“错误报告”的ZIP；只对这些项把kind/processing状态写入，并插入`comment_log_attachment_jobs`唯一(comment,file)事实和同事务NATS Outbox。Outbox失败会回滚评论、附件和job，客户端可用同幂等键安全重试。
- Worker是数据库状态机而非消息即真相。每个job保存requested user、attempt/max、next attempt、lease owner/expiry、last error和终态；CAS只claim到期queued或过期processing，失败按10秒至10分钟指数退避、第五次failed并同步附件failed。启动立即扫描、之后每30秒扫描最多100项，所以NATS/JetStream不可用、进程崩溃或消息ACK不确定都不会丢任务；重复已完成delivery为no-op。
- 日志分享物化与评论绑定不能假原子跨越OSS。Worker先调用既有安全`createFileLogShare`；它按source-file/redaction-version部分唯一键复用并发或先前成功的ready share。如果随后绑定事务失败，job回到queued，下一次直接复用share。完成事务锁定ready share并同时upsert`comment_log_bindings`、标attachment ready、以lease token把job completed；任一写失败三者均不提交。
- generation153→154新增job表、复合attachment级联FK、requested user FK、唯一键、attempt/lease/next/error字段及queued/processing/requester索引；历史没有可靠“哪些post-commit调用已发生”的任务事实，pre-production整代reset/seed，不猜测回填或保留同步双路径。新增默认NATS task并由runtime启动Worker；自定义任务配置normalize会补齐缺失默认项。评论API/DTO不变，普通附件无额外工作。BUG-082定Medium/P1：永久缺失诊断分享影响排障和治理，但评论/原文件保留且不扩大读取权限。

## D-257：评论删除同时撤销附件关系，日志分享保持独立生命周期

- `comments.status='deleted'`不只是正文展示标记。附件文件名、大小、扫描/处理状态以及`/log/s/{code}`仍是评论派生的可见元数据；因此主体状态转换和关系撤销必须是同一个数据库事实。DELETE现在先开启事务，沿用既有accepted-answer条件更新主体，再显式删除`comment_log_bindings`与`comment_attachments`后commit；任一DELETE、级联job或commit失败都rollback为published且保留正文/关系，客户端收到500可重试，不能出现deleted+binding的半状态。
- 日志处理job以(comment,file)复合外键级联attachment，故附件撤销自动终止queued/processing/completed job事实。`log_shares`是用户独立创建/复用的脱敏资源，评论只删除binding，不删除share或其entries；这既避免评论删除意外销毁其他入口可用资源，也使已出现在其他上下文的短链生命周期继续由日志分享自己的删除/过期合同管理。
- 写侧原子性不能代替读侧保密边界。`annotateCommentAttachments`对所有响应先设置非nil空切片，并且不把任何deleted comment ID放入附件查询/map；所以旧数据、手工修复遗漏、并发读快照或未来调用链都不能重新挂载附件。公开JSON形状保持`attachments:[]`而不是省略/null，前端无需迁移。
- 无DDL、generation、reset、回填、兼容双读/双写或远程数据库操作，保持154。两条关系删除均以既有复合主键首列`comment_id`命中Bitmap Index Scan；每次删除固定两条语句，无N+1。BUG-083定Medium/P1：删除后仍暴露元数据和有效诊断链接违反隐私预期，但没有跨用户授权扩大、正文已清空且独立share不应被连带销毁。

## D-258：评论编辑以客户端渲染的 updatedAt 做强制 CAS

- 原PATCH的`FOR UPDATE select body`只会在写入时读取最新内容，无法知道用户编辑器基于哪个版本。两个客户端从同一内容开始编辑时会顺序取得锁并都成功，第二个无条件覆盖第一个；“评论已发生变化”文案只有status变化时才可能出现，且activity delta错误地相对第一位的新内容计算。新请求必须提交`baseUpdatedAt`，缺失、零值或畸形时间在写前400。
- 更新使用一个数据修改CTE：按comment PK、published状态和请求版本选择并`FOR UPDATE`，同一语句把正文更新并以`greatest(clock_timestamp(),old+1 microsecond)`生成严格单调的新版本，同时返回锁定前body和新时间。即使两个请求在同一快照开始，也只有一个版本谓词能成功；后继UPDATE零行，不存在应用层检查到写入之间的窗口。activity delta现在只在CAS成功后以实际被替换body计算。
- CAS零行后重新读取当前body、updated_at和deleted事实，返回409 `COMMENT_EDIT_CONFLICT`及直接details。第一方`updateComment`发送渲染项的updatedAt；UI严格解析冲突details、用当前正文/版本/删除状态替换本地项，deleted时同步撤销全部操作能力和附件，再显示双语提示要求用户复核后显式重试。服务端不会自动合并或替用户覆盖。
- 这是开发期有意收紧的PATCH协议：旧body-only客户端收到400，前后端同批部署；成功CommentItem、路径、权限和活动类型不变。无DDL、generation、reset、回填、双读/双写或远程数据库操作，保持154。EXPLAIN显示CTE锁定和更新均命中comments PK。BUG-084定Medium/P1：会丢单条用户内容并扭曲审计delta，但不扩大权限、不删除其他持久对象且用户可显式重试。

## D-259：回复 capability 与创建端共享批量目标拉黑判定

- 顶层评论页已经调用`commentTargetOwnerBlocksUser`把`canCreate`置false，创建入口也在解析正文前用同一函数403；但逐项`annotateCommentPermissions`只检查`comment.create`与deleted，使前端依据`CanReply`显示注定失败的回复按钮。修复不改变授权结果，而是把可行动能力变成服务端真实预检。
- 单目标函数不再维护自己的switch和查询链，改为包装`commentTargetOwnersBlockUser`。批量函数对(type,internal ID,version ID)去重并通过一个UNNEST查询解析目标：mod/modpack/simple/mod-resource投影到项目type/internal ID并检查完整developer access集合中谁拉黑viewer；tutorial/discussion、blueprint、skin、player-profile按直接owner；issue/news及无owner概念目标明确false。受支持目标关系缺失返回错误，不能伪装未拉黑。
- 权限注解主查询同时取得每条评论的target identity，全部上下文只调用一次批量函数，再以相同identity设置`CanReply = published && comment.create && !ownerBlocks`。根列表、回复、thread、floor和watch都经过这一个装配入口；一项和100项watch查询数均为13，新增的是一个与page size无关的安全批量查询，原`<=12`门禁明确收紧为`<=13`且仍要求两者相等。
- 查询从UNNEST开始，所有八类目标关系以PK/现有catalog索引定位，user_blocks使用`idx_user_blocks_blocked_blocker`；developer权威继续复用`effective_project_access`而不重造有损owner标量。无DDL、generation、reset、回填或前端迁移，保持154。BUG-085定Low/P2：旧服务端最终始终403，问题只在能力提示和失败交互，不存在权限扩大或数据损坏。

## D-260：皮肤能力序列化保留完整授权上下文并拆分编辑与装备语义

- 皮肤详情的GET可由owner、`skin.admin`或`admin.*`读取private/pending资产，PUT/DELETE也用同一个`isSkinAdmin`授权；旧`skinAssetJSON(record,viewerID)`却只比较owner ID，使管理员成功读取后得到`canEdit=false`，第一方按该字段隐藏管理区。只在前端另猜管理员会复制并漂移授权来源，因此 serializer 改为接受完整`security.Claims`并复用既有predicate。
- `canEdit`与`canUse`不是同一能力。管理员能管理他人的private/pending资产，但衣柜/装备写端仍只允许owner或公开approved资产；若继续让`canUse`依赖扩展后的`canEdit`，会把本次UI修复变成新的虚假装备授权提示。实现先计算`isOwner`，`canEdit=isOwner||isSkinAdmin(claims)`，而`canUse=active&&(isOwner||approvedPublic)`。
- 目录、创建响应、详情、更新响应和本人衣柜五个序列化入口全部传当前claims；函数类型不再允许未来调用方只传Subject。成功DTO、路径、状态、数据库查询及第一方组件不变；`skin-detail`现有`texture.canEdit`管理区会自动显示，`canUse`按钮继续按原边界禁用。
- 专用RED证明`skin.admin`与`admin.*`旧值都为false；GREEN矩阵同时锁定owner true/true、普通viewer false/false和两类管理员 true/false（private pending）。无DDL或数据库计划，generation保持154。BUG-086定Low/P2：实际后端授权从未拒绝管理员，只造成入口隐藏和绕行API的不便，不涉及越权或数据损坏。

## D-261：皮肤首次创建与首次发布都服从 CatalogCreate 审核链

- 旧POST在完成安全PNG处理后把`skin_assets.review_status`与每条`content_localizations.review_status`固定写approved，既不读取已启用的`CatalogCreate`，也不创建revision/change request；因此普通认证用户的public皮肤立即进入匿名投影。修复在外部处理前读取共享配置，使用`catalogMutationReviewRequired(...,"create")`和`catalogMutationBypassesReview`；只有`content.no-review`或`admin.*`免审，`skin.admin`只表示管理能力而不是发布豁免，反滥用强制审核仍优先。
- `persistSkinAssetCreationTx`把主体、完整本地化snapshot、content revision、change request、submitted review event、audit及随后衣柜关系放在原创建事务。普通路径主体/本地化/request均pending，公开投影trigger得到0行；免审路径调用同一`applySkinAssetSnapshotTx`，主体和本地化均approved并绑定published revision，投影只在事务提交时成为一行。任一审核事实写失败由外层事务把skin、关联与配额可见事实全部回滚。
- 审核批准继续由既有skin分支应用snapshot；首次拒绝现在把无published revision的主体及其未发布本地化同步标rejected，既有已发布编辑被拒则保持approved。为了阻止`CatalogCreate=true/CatalogEdit=false`配置下用PUT绕过，`published_revision_id=nil`的拒绝后重提仍把operation识别为create并重新使用CatalogCreate；首次发布且请求未替换本地化时也会把现有未发布语言绑定到新published revision。
- POST仍返回201完整`SkinTexture`，普通路径只是既有枚举`reviewStatus`从错误approved变为pending；owner可按既有读边界进入详情并个人使用，匿名目录/详情不可见，第一方无需协议分支。无DDL、generation、reset、回填或双路径，保持154。BUG-087定High/P1：任意登录用户原可绕过明确启用的公共目录治理和不可变审核链，虽不泄露私有数据且纹理仍经安全处理。

## D-262：共享纹理派生物使用系统归属、引用事实与新Key删除周期

- 内容哈希去重代表字节共享，不代表首个上传者拥有派生对象。原始私有上传继续是用户文件并按既有source/stored额度计算；安全重编码后的共享PNG改为`uploader_id=NULL`、`source_size_bytes=0`、`source=minecraft_texture_derived`，stored bytes仍保留平台实际占用。这样第二个用户不会免费消费“另一个用户的文件”，而是所有人引用同一个系统事实。
- generation155在`skin_texture_blobs`增加非负`active_reference_count`。`skin_assets`的insert/status/blob/delete trigger在同事务增减；最后引用删除读取该事实并再核对active关系，随后用既有`tombstoneOSSFileTx`把file变deleted并建立可靠删除Outbox。零引用部分索引让Maintenance每10分钟有界收敛历史/漏执行候选；`rebuild_skin_texture_blob_reference_counts()`以全局独占advisory calibration锁提供原子校准。
- 正常持久化和删除先持有同一全局shared calibration锁，再持有hash生命周期锁，之后才锁Blob/触发引用更新；校准取独占锁。真实双事务用例证明持久化持锁时删除在生命周期锁外等待，不能形成“删除占Blob、持久化占hash”的环形死锁。引用trigger使两个共享资产的并发状态转换也在同一Blob行上串行。
- 每次新对象生命周期都用`hash+随机UUID` Key。最后引用的旧Key可继续由旧Outbox重试；同hash后来恢复会创建新`oss_files`并原子切换Blob，不会被已排队墓碑删除。Put成功但外层skin/Yggdrasil事务失败时，rollback先完成，再以unregistered guard建立无file引用的补偿任务；commit不确定时已注册active文件会阻止误删。
- POST、Launcher、纹理读取和前端DTO不变；开发库从154整代reset到155，不猜测回填旧引用或转换远程数据。BUG-088定Medium/P1：旧行为造成永久额度误计和平台对象泄漏，但每个Blob最大2MiB、共享人数不放大扣费，且不越权、泄露或删除已发布内容，故不升High/P0。

## D-263：收藏全量替换先证明整个集合身份，再允许任何副作用

- 旧PUT把“集合ID未匹配任何行”当成一次无操作插入：它先删除用户对目标的全部关系，再逐ID执行`INSERT ... SELECT`并仅把影响行数大于零者加入响应。不存在或属于他人的ID因此仍提交200，无法区分客户端明确清空与陈旧/错误身份。真正的不变量不是SQL无错误，而是请求中的每个去重身份在替换快照内都有效且属于当前用户。
- 请求先复用已有`normalizeFavoriteCollectionIDs(..., true)`，因此9字符格式、大小写、去重、最多100项和显式空数组语义与PATCH一致。事务解析目标可见性后，`validateFavoriteCollectionsTx`以owner+`ANY`查询和`FOR UPDATE`锁住全部请求集合，并要求读取数量精确相等；missing或跨owner统一返回400。该校验发生在默认集合upsert和关系DELETE之前，所以失败没有任何收藏副作用。
- 验证成功后默认集合也通过同一事务queryer建立，随后删除旧关系并以一条集合INSERT写入全部新关系。虽然锁定已防止合法集合并发删除，仍要求`RowsAffected == len(distinct IDs)`；若未来约束、查询或锁序发生漂移，事务500回滚而不是提交短写。PATCH删除重复的内联逻辑并复用同一helper，避免两个公开写协议重新分叉。
- 第一方已经使用PERF-051引入的有界PATCH delta，保留PUT只为既有完整替换协议和LEGACY-018独立范围；不借本项删除路由或旧`entityKey`别名。PUT成功请求/响应不变，只有过去错误成功的无效身份收紧为400。无DDL、generation、reset、回填或前端部署变化，保持155。BUG-089定Medium/P1：会破坏发起者自己的收藏关系且以200掩盖，但目标、集合和其他用户数据不受损，关系可重建，也不形成越权或泄露。

## D-264：社区自动批准使用公开 revision token 做锁内条件发布

- advisory lock只把两个写事务排队，不会判断后一个快照是不是基于旧正文。旧入口在事务外读内部`published_revision_id`，两个请求都把同一值写入revision的base；后一个取得锁后仍会创建“基于A”的revision C并直接覆盖已经发布的B，两个HTTP都200。修复把“客户端实际加载的published revision仍是当前值”定义为自动批准前置条件。
- 社区详情联接当前revision并返回可选`publishedRevisionId`；PUT的`baseRevisionId`使用同一9字符公共身份，不向客户端泄露内部主键。更新事务首先取得`community_post:{publicID}`的aggregate advisory xact lock，随后`FOR UPDATE OF post`重读published revision及其公共ID；不相等立即返回409 `COMMUNITY_POST_EDIT_CONFLICT`和`details.currentRevisionId`。缺基线面对已发布对象也视为stale，畸形非空ID在事务前400。
- 锁和比较先于引用解析、悬赏恢复/扣款、revision/change request/audit以及snapshot apply；冲突因此是零副作用。通过后把锁内读到的内部ID作为`BaseRevision`交给`createContentRevisionTx`；它重取同一事务级advisory锁是PostgreSQL可重入操作，既保留所有聚合调用方的revision序号保护，也不产生第二套锁序。review config改用当前tx读取，配置与决策处于同一快照，并使单连接完整临时Schema可验证真实handler。
- 第一方编辑器在加载详情时保存token并随PUT提交；稳定409只显示双语冲突提示、保留本地自动草稿且不更新基线或跳转，用户需重新加载、核对最新版本后显式保存。成功响应和内容历史不变；旧客户端编辑已发布对象会409而非静默覆盖，前后端同批部署，不保留无CAS兼容路径。
- 无DDL或generation变化，保持155。首轮RED因append-only审核历史不能逐行清理，已对明确本机127.0.0.1:55432/postgres执行既有保护性reset并直查155/4 seeds/零测试行；后续用单连接临时完整Schema且自动drop，部署无reset要求。BUG-090维持High/P1：有权免审编辑者可无提示丢失已发布修改并形成错误审计链，但没有越权或泄露，旧revision也仍可审计恢复。

## D-265：显式社区来源语言是修订权威，检测只做高置信缺省回退

- `sourceLocale`原本已经是请求、主表、revision snapshot、详情与AI翻译payload的共同字段，但标准化函数无条件用检测结果覆盖它；这使客户端明确选择没有任何业务效力。修复先trim并调用共享`normalizeContentLocale`，再以`isEditableContentLocale`校验站内8种注册语言；`fr_fr`等alias写为规范`fr-FR`，注册表外显式值稳定400，不能被检测偷偷改成另一语言。
- 字段真正缺省时才允许启发式回退。日文假名、Cyrillic及中日韩汉字证据可直接确定既有站内语言；拉丁文本对英/德/西/法词表计分，只有唯一最高且至少2个词命中才接受。并列或低分返回空并由标准化产生400，短技术文本、标识符和未覆盖语言不再默认英语。该回退保留旧客户端对明显文本的有限兼容，但不会制造低置信持久事实。
- 第一方`CommunityPostDraft.sourceLocale`改为必填。新建编辑器显示站内完整8语言selector且初始为空，提交前要求用户明确选择；编辑详情回填当前source locale，自动草稿把选择作为普通draft字段保存。成功POST/PUT与响应形状不变，只有此前被忽略或错误成功的输入收紧；前后端应同批部署。
- 无DDL、generation、reset、回填或查询变化，保持155。真实单连接完整临时Schema证明显式法语创建、德语编辑后主表/current revision snapshot/详情三者一致，缺省低置信请求400且零内容行；存储事实继续直接流入翻译源locale和缓存身份。BUG-091定Medium/P1：错误可持续误导翻译与标签但不越权、不泄露、不删除原文，可由作者新修订纠正。

## D-266：翻译 queued 表示 task 与发布意图已持久接受，而不是本次 NATS 已发布

- BUG-092的旧状态分裂来自提交后直发：`enqueueCommunityPostTranslation`先提交queued task，Handler再调用`publishContentTranslationTask`；发布失败会另一次UPDATE把数据库task写failed，但调用栈仍持有入队前的`task.Status=queued`并把它返回。LEGACY-019已经删除这条publisher和失败回写，故本项不再建立第二套修复，而是把既有可靠入队语义作为独立业务合同验收。
- 当前社区翻译在同一事务中完成actor quota锁/预留、active concurrency去重、`ai_tasks` INSERT与`nats_outbox` INSERT。Outbox约束或数据库错误使事务回滚，Handler返回503且没有可轮询task；成功202 queued只在task和event都durable时发生。既有活跃task直接返回其数据库status，不重复制造发布事件。
- PostgreSQL Outbox dispatcher才拥有NATS发布阶段。暂时断线把event从publishing写为failed、记录error和指数退避，task仍是queued；重连后同一event变published，AI Worker以queued/retrying CAS领取task。结果GET只读task status，因此queued始终表示仍可恢复执行，不再与数据库failed事实矛盾；event耗尽进入dead由既有运维replay/告警处理，不伪造业务执行成功。
- 本项新增完整临时Schema的HTTP故障测试：触发器拒绝社区event时503且task/event=0/0；移除故障后202、task/event=queued/pending；模拟retryable event failure后轮询仍queued。既有真实JetStream测试另证断线failed和重连published。无新DDL、API字段、前端迁移或生产逻辑变化，保持155。BUG-092维持Medium/P1；SEC-038触发权限、BUG-094类型并发与TEST-043完整矩阵仍独立。

## D-267：Accept-Language 只从合法、正权重的具体语言范围选择稳定首选项

- BUG-093来自共享解析器只截取第一个逗号项的分号前文本：`fr-FR;q=0.2,en-US;q=0.9`和`fr-FR;q=0,en-US;q=1`都会直接返回fr-FR。公共内容本地化与simple-project目录都调用该函数，因此修复放在共享边界，不在各Handler复制排序或例外。
- qvalue使用0..1000整数表达，避免浮点舍入；无参数默认1000，只接受`0`/`1`或最多三位小数，1的小数只能为0。每项只允许一个q参数，非法/越界q、未知/重复参数、非法语言标签、q=0和wildcard均跳过；只有严格大于当前最佳权重才替换，因此同权重自然保持字段原序。语言标签继续先经既有BCP-47形状验证，再由`normalizeContentLocale`统一大小写与zh-Hant/en等alias。
- wildcard表达任意语言范围，不能由这个返回单一locale的函数伪造成某个具体偏好；没有合法正权重具体候选时返回空值，调用端沿用既有站点默认。显式query locale及认证用户的持久primary/secondary设置仍按现有优先级覆盖请求头，避免本项扩大为用户设置协议重写。
- 八分支RED/GREEN覆盖高权重、稳定tie、q=0、wildcard、非法/越界q、未知参数、默认q和alias；另从两个公开调用端证明共享生效。无DDL、API字段、前端迁移、额外查询或缓存变化，generation保持155。BUG-093维持Medium/P1；BUG-091来源语言事实、SEC-038翻译费用边界与TEST-043完整本地化矩阵不合并关闭。

## D-268：AI 并发只由 NATS task 配置解释，删除未执行的分类型字段

- BUG-094的`aiTaskModelConfig.ConcurrencyLimit`只在默认、持久设置、GET/PUT DTO和后台表单间往返；AI Worker claim、`executeTask`和供应商请求均不读取它。实际执行只在queue订阅处为task code `ai`按`NATSTaskConfig.MaxConcurrent`建立信号量，因此管理员看到的“每AI子类型并发”是无法成立的第二权威。
- 不增加AI Worker本地分类型信号量：它会在每个应用实例各自生效，集群总并发随实例数相乘，又与外层NATS `ai`信号量叠加；热缩容、公平性和跨实例硬上限若没有持久分布式permit/lease协议就会继续失真。当前产品只需要一个诚实模型，故保留已可热重配的NATS task上限，并明确它是每应用实例限制、集群容量随活跃Worker实例扩展。
- 后端从task definition、JSON DTO、defaults和normalize删除字段；严格AI配置PUT把旧字段视为未知并400，不维持隐藏兼容。普通持久设置读取使用typed unmarshal，既有加密JSON中的旧键会被忽略且不再GET回显，下次管理员保存自然移除；无需也不应为无效键解密批量回填。每任务模型绑定、提示词与Worker实际读取的timeout保持不变。
- 前端同步删除类型、默认值、输入列和双语dead key，taskModels说明指向NATS `ai`任务；NATS页面保留`maxConcurrent`并明确每实例/集群语义。真实JetStream以4条同时到达消息验证MaxConcurrent=2时第三条不能启动、最大active精确2，释放后四条均完成。无DDL、generation、额外数据库锁或双协议，保持155；BUG-094维持Medium/P1，TEST-043其余AI行为矩阵独立。

## D-269：公开站务语言回退不能参与管理端持久身份选择

- `normalizedSiteAffairsLocale`原本为公开About和更新日志提供容错：请求为空或不受支持时选择`zh-CN`作为回退候选。管理端About路径和changelog写请求复用该函数，因而`ko-KR`、拼写错误甚至空值不失败，而是以简中translation主键继续读取或upsert。管理者看到的请求语言和真正被修改的持久身份不同，输入错误会成为正式内容覆盖。
- 保留公开helper及其既有回退合同，另建`normalizedSiteAffairsAdminLocale`：先使用共享`normalizeContentLocale`处理大小写、下划线和`zh-Hant`等alias，再要求结果属于8种`isEditableContentLocale`注册值。About管理GET/PUT在首次数据库查询前校验；changelog POST/PUT在日期解析、事务和父对象状态/日期更新前校验。非法值统一400，合法`fr_fr`规范写为`fr-FR`。
- 完整临时Schema先保存已发布简中About，证明非法管理GET不再伪装成简中、非法PUT不改变简中标题/正文；合法法语alias建立独立翻译。非法changelog POST保持父/翻译表零行；非法PUT保持既有父日期/发布状态和简中正文全部不变。纯函数矩阵覆盖8种站内语言/alias、空值、unsupported和畸形值，并明确守护公开unknown仍回退简中。
- 路由、合法请求/响应DTO和公开读取行为不变；第一方selector已经只产生站内注册语言，不需前端改动或协调部署。无DDL、generation、reset、回填、双读或远端访问，保持155。BUG-102按Medium/P1关闭：需要有管理权限的错误请求，但会静默破坏正式内容；BUG-103跨语言发布状态、BUG-104并发版本和TEST-046其余行为矩阵继续独立OPEN。

## D-270：站务父对象只承载存在，发布状态属于每个语言翻译

- About原本同时保存`site_pages.status`与`site_page_translations.status`，但每次保存都把两者设成当前语言的publish选择；法语草稿因此把singleton父项改成draft，使仍为published的中英文全部从公开查询消失。修复后父项在创建/更新时保持published，`publish`只决定目标translation status；父级`published_revision`仅在实际发布目标翻译时递增。
- Changelog此前只有父级status。generation156给`site_changelog_translations`增加非空`status`、draft/published CHECK与published `(changelog_id,locale)`部分索引。父项新建即为published并只承载对象存在及日期；更新不再写父status，translation upsert独立保存请求语言的status。开发期旧generation不兼容，按既有保护流程整代重建，不增加迁移、回填、双读或状态推断。
- 公开详情的lateral候选只含published translation。公开分页在父级有界page CTE中先以indexed EXISTS要求至少一个published translation，再按请求语言、简中、英文、其他published语言回退，避免draft-only父项占满limit后被JOIN丢弃并错误终止cursor。后台聚合为每个translation返回status；第一方编辑器从当前locale翻译而非父status初始化发布复选框和列表标记。
- 单连接完整generation156临时Schema证明：法语About草稿不隐藏中英文；changelog法语草稿不隐藏简中，发布法语再将简中改草稿后公开正确回退法语；仅西语草稿的父项详情404且不进入列表；后台详情保留中draft/法published。百万父项+translation的真实SQL仍每页一条查询，命中父keyset和translation partial index且目标表无Seq Scan。
- 公开和合法写响应shape不变；后台translations新增status，是第一方同批消费的开发期协议扩展。无远端访问或重置；本地共享public未破坏性重建，空库能力由自动drop的完整临时Schema验证。BUG-103按Medium/P1关闭：正常草稿可造成全局内容不可见但事实未删除且可恢复；BUG-104并发版本与TEST-046其余权限/空翻译矩阵独立OPEN。

## D-271：站务写入必须以编辑器实际加载的版本做条件提交

- About管理GET已经返回每个translation的`revision`，旧PUT却无条件upsert。新PUT要求非负`baseRevision`：0只允许插入尚不存在的目标语言，正值只更新相同revision的行；成功在同一SQL把revision递增并单调推进更新时间。父singleton更新和published revision与translation CAS同事务，失败会整体回滚，不能留下只有父级变化的半完成状态。
- Changelog的日期是跨语言共享事实，故以父项`updated_at`作为整记录版本，而不是另造每翻译令牌。后台列表已有`updatedAt`，详情和PUT成功响应也返回它；PUT要求`baseUpdatedAt`，先以`public_id+updated_at`条件更新父日期并用`greatest(clock_timestamp(),updated_at+1 microsecond)`生成严格新版本，再在同事务保存目标translation。旧基线零行时在同一事务读取当前日期、逐语言内容/状态和版本，回滚后返回稳定409 `SITE_CHANGELOG_EDIT_CONFLICT`；About对应`SITE_PAGE_EDIT_CONFLICT`。
- 只有CAS成功并提交后才保留既有管理员操作日志，日志metadata同时记录请求基线与新revision/updatedAt。冲突响应携带服务器当前快照；第一方About和changelog编辑器保存加载基线、随请求提交，识别两个稳定错误码后显示双语提示并保留当前输入，要求管理员重新加载核对后再显式保存，不会把冲突当普通成功或自动覆盖服务器版本。
- 无DDL、generation、reset、回填、双读或双写，保持156。单连接完整临时Schema证明两个管理员的首写成功，旧About revision/旧changelog updatedAt均409且父项、翻译和版本保持首写事实，使用冲突后的新基线可重试成功；BUG-102严格语言和BUG-103逐语言发布同时回归。BUG-104按Medium/P1关闭：需有权管理员并发，但会无提示丢失正式站务内容；无越权、泄露或远端数据库操作。

## D-272：未解析引用的写时目录必须覆盖每个生产者并跟随来源身份变化

- PERF-059已把异构未解析事实收敛到`unresolved_reference_catalog`，列表不再逐页JOIN。但通用刷新函数只识别`mod_relationship`、两类community引用和`modpack_mod`；真实生产者还会写`minecraft_server_mod`、`simple_project_parent`，现有`mod_content_resource`通用tag引用也以catalog entity为source。三者进入投影时`sourceLabel/sourcePublicId`均为空，稳定分页只是稳定返回不可操作的数据。
- generation157扩展同一刷新函数：server mod经`minecraft_server_mods.server_id`映射服务器名称/公开ID，simple parent经parent ref映射源项目名称/公开ID，mod-content source直接映射catalog entity identity/public ID。既有四类映射不复制或绕过；真实临时Schema以全部7类当前生产者证明初始投影都非空。未知source type不猜测对象，保留空label/publicId以及原有sourceType/sourceId/fieldPath作为显式诊断。
- 投影是持久读模型，不能只在创建时正确。新增server mod的server重绑、simple parent的project重绑触发器；服务器名称、simple project名称变化触发重新投影；catalog entity label分支同时刷新resource-origin与通用mod-content引用。既有mod、community、modpack及其重绑触发器继续由同一七类测试覆盖，名称更新后立即反映，两个新父关联重绑后切换到新公开身份。
- 后台DTO字段不变；第一方对具有公开路由的六类来源使用统一9位`/{publicId}`短链，mod-content等不可路由类型仍显示label、sourceType、publicId和fieldPath。百万行列表继续只读窄投影并通过原keyset/前缀索引，无N+1或列表JOIN回退。函数/trigger语义变化使Schema 156→157，开发库需保护性整代重建；无在线回填、双读、双写或远端/共享public操作。BUG-105按Medium/P1关闭：它阻塞真实管理处置但不越权、泄露或直接改坏权威内容。

## D-273：未解析引用筛选类型由生产者集合和目录注册表组合发布

- 前端原先自行列出`mod/minecraft.item/minecraft.enchantment/tag/plugin/server`。然而community与simple-parent生产者允许modpack及六类simple project，资源引用又可使用`resource_kinds`中的内置、隐藏和运行时扩展code；任何前端常量都会随生产事实扩展再次漏项。类型权威因此放在后端：项目部分直接枚举community写边界正在使用的白名单，资源部分按`display_order,code`读取完整小型注册表，并与tag/enchantment/server兼容类型去重。
- 不把类型查询并入每一页响应。新增`GET /api/v1/admin/unresolved-reference-types`，沿用`reference.unresolved.read`权限并返回`{items:string[]}`；第一方面板只在token变化时独立加载一次。分页、搜索和百万行计划仍只执行既有窄投影page SQL，避免为了修复筛选枚举而给每次翻页增加注册查询或全量type count。
- catalog/mod-content类型合法长度可达128字节，故列表filter从旧64字节扩至相同128边界，仍拒绝非法UTF-8、空白和控制字符；registry若出现不能往返的异常code则端点500，不发布点击后必定400的伪选项。动态类型没有翻译键时第一方显示原始code，列表行使用相同fallback，不把内部key路径展示为标签。
- 真实PostgreSQL临时表证明八种项目类型、tag/enchantment/server、内置kind、运行时扩展kind及`user_visible=false`管理类型全部可达，128/129边界分别接受/拒绝；前端源码合同禁止旧六项枚举。无DDL、generation、reset、回填、双读或远端访问，保持157。BUG-106按Medium/P1关闭：缺陷阻塞精确管理筛选但仍可用全部/关键词绕行，不造成越权或权威数据损坏。

## D-274：显式搜索意图必须推进独立请求代次，即使规范查询未变化

- BUG-107的审计实现由`page/status/submittedQuery/token/type`驱动effect，但submit只设loading、首面和trim query；在首面重复同query时React没有任何依赖变化，因而没有请求能执行finally，loading永久为true。判断“查询值相同”不能等同“用户没有刷新意图”。
- PERF-059迁移cursor时已建立`requestVersion`。现在逐项确认每次`resetPagination`都函数式递增它，search在设置规范query前必调用reset；`requestVersion`是load effect依赖，因此相同query/首cursor也启动新请求。筛选变化复用同一入口，同时清空cursor及history，不制造跨scope cursor。
- 每个effect创建AbortController和局部`active`标记；cleanup先令旧代次inactive再abort，then/catch/finally都只允许active代次写状态。这样连续重复提交时，旧请求的finally不会提前清掉新请求的loading，最新代次无论成功或失败都会恢复loading=false。
- 新源码合同同时锁定submit→代次递增、effect依赖和active finally；聚焦3项及全部92文件218项通过。无生产代码、API、Schema、generation或数据库变化，保持157；BUG-107按Medium/P1关闭，因为冻结管理主流程但可通过改筛选/重入恢复，且不改变权威数据或权限。

## D-275：cursor空页描述当前位置，不伪造过滤集合总量

- BUG-108来自旧`count(*) over()`：total只能从返回行读取，合法OFFSET因并发删除越过末项时没有行承载window count，Handler保留初始0并把“这一页无行”错误表示为“整个scope零行”。同步total与深OFFSET也正是PERF-059已删除的全量工作，不能为单个空页重新引入独立COUNT。
- 当前协议只承诺`items/limit/hasMore/nextCursor`。page读取limit+1；没有返回行就诚实表示当前cursor之后无项，`hasMore=false`且无next cursor，不对cursor之前仍存在多少项作陈述。cursor通过scope摘要绑定query/type/status/limit，因此不会把空页跨筛选解释。
- 第一方每次Next前把当前cursor压入history。Previous按钮只看history长度，不看当前items或不存在的total；所以并发收缩造成的空页仍能回到已知前一cursor。真实PG以两项/limit1取得next cursor，随后删除全部事实，再用原cursor请求，精确返回空items/false/空cursor/原limit；前端源码合同证明空items不会禁用Previous。
- 本项补测试而不改PERF-059生产实现、Schema或API，generation保持157；1M深页/前缀组继续通过。BUG-108按Medium/P1关闭：它需要并发收缩并影响管理导航语义，但不改变权威数据、权限或公开内容。

## D-276：举报快照主体保留真实角色，项目处置只通知关系派生开发者

- 选择以`target_actor_id + target_actor_role`保存举报创建时的主体事实，角色闭集为submitter/author/owner/subject。它既能让评论、社区内容、皮肤、蓝图和用户举报保留真实直接主体，也能让Mod、简单项目和服务器明确保存“提交者”而不伪造作者；服务器快照的`author`同批纠正为`submitter`。后台DTO与第一方删除targetAuthor命名并显示角色。
- 项目有效隐藏的治理收件人不从actor submitter取值，而是在隐藏前按规范项目类型查询`effective_project_access`中`access_level='developer'`的distinct用户。该权威视图只由已批准个人作者claim或已批准作者→团队→项目关系的permission-granting路径产生；普通编辑员不进入。直接用户内容继续使用真实author/owner/subject，举报人自身不重复收到目标处置通知。任何联系人查询或Outbox写失败都回滚审核事务。
- 不选择把`submitted_by`改名后继续通知，也不把所有项目editor视为开发者；前者维持身份错误，后者扩张治理信息收件范围。真实PG完整resolve证明作者直绑/团队重复路径只生成开发者20/21两个Outbox事件且提交者/editor 10为零；直接author控制仍返回其真实用户。
- 这是开发期权威Schema替换：`target_author_id`改为`target_actor_id`并新增role CHECK，generation 157→158，旧开发库需经既有保护流程整代重建；不保留双列、回填或兼容读取。BUG-109按High/P1关闭，因为旧路径会把治理事实发给非作者并漏发真实开发者，破坏核心身份和通知边界。

## D-277：invalid举报结论与任何惩罚动作互斥

- `valid/invalid`不是展示标签，而是举报事实的最终结论；在同一个resolve命令里执行隐藏或封禁后再保存`resolved_invalid`会制造无法由审计、通知或申诉解释的矛盾状态。因此请求在权限检查、事务创建和所有副作用之前统一验证：invalid只允许无惩罚结案，`deleteTarget=true`或非空`banUserId`均稳定400。
- 该边界不把动作权限并入结论，也不降低既有`report.action.delete`/`report.action.ban`检查。valid结论仍必须分别拥有动作权限；需要与举报结论无关的管理员处置应继续走独立封禁/内容管理入口及其独立原因/审计，不能借用invalid报告事务。
- 真实PG用完整resolve所需临时事实证明非法hide请求返回400，并且report保持pending、目标保持approved、review/action/Outbox计数全0；纯函数矩阵同时覆盖invalid ban、invalid无动作及valid delete/ban。无Schema/API字段/第一方变化，generation保持158。BUG-110按High/P0关闭，因为旧行为直接执行隐藏或封禁并破坏治理权威事实。

## D-278：临时封禁必须在命令与持久事实中保留至少一分钟有效区间

- `endsAt`同时驱动公开释放判断、active封禁唯一键和`governance_ban`角色过期。旧写路径允许任意时间，故一个创建即过去的值会让公开层显示已解除，而数据库仍保存active记录、角色绑定和被占用的唯一键，直到维护Worker稍后补偿。时间有效性必须是命令不变量，不能委托异步清理兜底。
- 统一规则为永久封禁继续使用空截止时间；临时封禁在服务校验时必须至少晚于当前时间1分钟。举报resolver仅在实际携带`banUserId`时验证，独立管理员入口也在打开事务前验证；`createBanTx`再验证一次，覆盖未来内部调用方并把检查放在用户查询和任一写入之前。边界错误稳定400，既有请求shape与成功DTO不变。
- generation159在`ban_records`增加`ends_at is null or ends_at>=starts_at+interval '1 minute'`，使绕过应用的直接写入也不能保存无意义区间。由于Schema仍处开发期整代模型，旧generation158库按既有环境/库名/双确认流程重建，不做在线迁移、历史推断、回填或双写；没有访问远端或共享public。
- 真实PG证明过去时间经`createBanTx`后ban与角色均为零、有效未来时间原子写入，并以自然到期记录运行既有`expireBans`，状态变expired且治理角色删除。完整generation159临时Schema和三档规模断言通过。BUG-111按High/P1关闭：它破坏权限与封禁权威事实并阻塞后续合法动作，但需要管理写入且现有Worker可恢复，不升级为P0；OPS-020维护饥饿仍独立。

## D-279：举报领取是责任锁，接管是独立且可追溯的高权限命令

- 保留领取工作流而不把它降级为装饰状态。pending报告不能直接结案；领取在一个数据库事务中把状态变为in_review、设置责任人与时间，并写`report_assignment_events(action=claim)`。最终resolve的锁定查询同时要求`status='in_review' AND claimed_by=当前用户`，所以未领取、别人的责任项、已接管旧责任人和已结束记录都在首个业务读取处409。
- 通用`report.review`只代表可浏览队列、领取和处理自己的责任项。新增administration级`report.action.takeover`，其POST命令要求非空且不超过1000字符的原因；在`FOR UPDATE`锁内读取旧责任人、原子改为当前执行人并写takeover事件的actor、previous assignee、new assignee和reason。无权限403，同一责任人重复接管和非in_review目标409；成功另写管理员操作日志，但持久assignment事件才是权威审计。
- generation160把`claimed_by`删除行为从set null收紧为restrict，并约束任何in_review行必须同时有claimed_by/claimed_at；新增assignment事件表及`(report_id,created_at,id)`索引。旧开发库按整代保护流程从159重建，不在线ALTER、不猜测旧in_review责任、不回填或双写；完整临时Schema自动drop且未访问远端/共享public。
- 详情新增责任人、是否当前责任人及是否可接管字段。第一方pending只显示领取，in_review只向当前责任人显示结案表单，其他人仅在拥有专权时显示接管并要求原因。真实PG证明101领取、202越位失败、无专权接管失败、专权接管审计后101失权且只有202能结案。BUG-112按Medium/P1关闭：旧行为破坏授权审核员之间的责任和审计，但没有让无`report.review`者进入或扩大数据可见性。

## D-280：整合包必须是统一举报合同的一等目标

- 产品公开目录已经把Modpack作为与Mod、插件等并列的核心项目类型，因此不能以“未接入举报”作为隐含产品禁止。`modpack`加入统一举报目标集合，并贯穿原因发现、创建校验、快照、主体关系、项目访问身份、有效隐藏动作和第一方入口；不建立第二套整合包专用举报协议。
- 快照从`modpacks`权威行保存标题、次标题、摘要、正文、分类、pack type、packaging method、许可、运行环境、状态、来源/审核状态、链接、图标、时间和submitter。actor role沿用BUG-109的真实`submitter`语义；审核通知仍从`effective_project_access`解析实际developer，不能把提交者冒充开发者。
- generation161仅把`modpack`加入`reports.target_type`闭集。开发期旧generation160库按既有保护流程整代重建，不在线ALTER、不伪造历史快照、不回填或双写；真实验证只安装并删除会话临时Schema，未访问远端或共享public。
- 第一方Modpack详情使用既有`UnifiedReportButton`和稳定public ID，英中文案补入目标名。真实PG证明创建后快照/主体正确，领取后valid+delete使报告成为resolved_valid、整合包成为rejected且只写一条动作。BUG-113按Medium/P1关闭：旧行为让核心目录类型完全失去治理入口，但不产生越权、泄露或既有数据破坏。

## D-281：关于页写入身份由已验证草稿语言决定

- locale选择值不是可独立用于写URL的权威事实。About编辑器只有在当前选择、请求locale、响应`draft.locale`和最新加载代次全部一致时才接收草稿；切换立即清空旧内容并锁定输入/预览/保存/发布，旧请求既被AbortController取消，也会被代次和语言条件二次拒绝。
- 加载失败或服务返回错语言时保持不可写并提供显式重试，不能以空revision或上一语言内容降级保存。统一`canSubmitAboutDraft`还覆盖保存进行中和无草稿状态；实际PUT从捕获的`saveDraft.locale`构造路径，并从同一快照读取标题、正文和BUG-104要求的`baseRevision`。
- 保存期间若管理员切换语言，旧语言请求可正常完成其已验证命令，但结果不能刷新、报错或显示成功到新语言编辑器。目标语言的新加载独立进行；后端每locale CAS继续拒绝真正陈旧的同语言revision，前端锁与数据库并发控制各自承担不同边界。
- 本项无Schema/API字段变化，generation保持161。真实PG复核locale验证、翻译隔离和revision CAS，前端纯状态矩阵与完整发布门通过。BUG-114按Medium/P1关闭：旧窗口可污染公开多语言权威内容，但只发生于已授权管理员的特定加载竞态，不扩大权限或数据可见性。

## D-282：更新日志语言切换保留独立草稿且禁止翻译兜底

- 更新日志列表已经一次返回记录的全部`translations`，因此切换语言不需要另发GET，但必须保留正在编辑的完整记录。选择记录时只读取`translations[selectedLocale]`；目标语言不存在就显示空title/body和未发布状态，绝不再用`Object.values(translations)[0]`把任意其他语言复制进可提交表单。
- 对未保存修改采用按locale缓存而非切换确认：离开当前语言前保存其title/body/publish快照，进入目标语言时先恢复该语言缓存，否则读取记录中该语言的权威翻译或空草稿。该规则同时适用于新建与编辑，允许管理员在多语言间往返而不丢失或串写输入。
- 保存命令仍只提交当前一个locale，并从当前表单草稿取得title/body/publish；编辑目标、日期和父`baseUpdatedAt`保持绑定到`editingItem`。BUG-104的父行CAS会拒绝任何语言在载入后发生的并发更新，本项不新增API字段或后端兼容路径，generation保持161。
- 纯状态测试证明精确语言、缺失为空与双语言草稿往返，全量第一方门和同一后端locale/translation/CAS真实PG组通过。BUG-115按Medium/P1关闭：旧流程可污染公开翻译，但执行者已有合法管理权限且仍需主动保存，不构成越权或泄露。

## D-283：datetime-local必须在客户端时区边界转换为RFC3339

- HTML `datetime-local`明确没有时区，而Go请求模型的`time.Time` JSON合同要求RFC3339瞬时值。不能原样发送，也不能假设UTC；第一方按浏览器本地时区构造Date，核对构造后的年月日时分仍与输入一致，并用`toISOString()`输出带`Z`的标准字符串。
- 解析只接受控件实际格式`YYYY-MM-DDTHH:mm`。日期规范化漂移、越界时间、DST不存在的本地时间、预带Z/offset和任意文本都抛出RangeError并在请求前显示本地化错误；空/空白单独返回null，继续表达永久封禁而非解析失败。
- 后端协议与保护不变：标准JSON先解码RFC3339，再由BUG-111的入口和事务边界要求至少剩余1分钟，数据库CHECK约束保存区间。本项不发明新的最大期限或修改管理规则，不改变Schema/generation161。
- 纯函数墙钟往返与表单源码合同、后端RFC3339 handler回归和完整第一方发布门通过。BUG-116按Medium/P1关闭：旧UI让临时封禁功能完全不可用，但服务端以400安全失败且永久封禁仍可使用，没有越权或数据破坏。

## D-284：异步提交只能使用await前捕获的表单引用

- React事件的`currentTarget`不作为Promise续体中的稳定引用。封禁提交在同步监听阶段先捕获`formElement`，FormData也从该对象构造；API成功后reset使用同一稳定DOM对象，await之后不得再访问`event.currentTarget`。
- 错误边界只包围真正的服务器命令。请求失败显示API错误并立即return，reset、分页归零和列表刷新仅在确认201后发生；这样本地清理或刷新问题不会被伪装成“封禁未创建”并诱导管理员重复执行权威命令。
- 本项不增加客户端幂等键或修改后端唯一约束：active封禁原子唯一性已阻止重复事实，问题是成功状态反馈而非服务端重复写。无Schema/API变化，generation保持161。
- 三项源码顺序RED/GREEN和完整第一方门通过。BUG-117按Low/P1关闭：旧路径会产生错误提示与陈旧列表，但封禁权威事实已成功、重复请求被约束拒绝且刷新可恢复，不构成权限、泄露或持久数据破坏。

## D-285：治理异步响应必须证明自己仍属于当前目标

- AbortController是资源取消手段而非唯一正确性证明。举报详情同时维护单调请求generation和用户当前期望的`selectedReportId`；只有未abort、代次最新、请求ID仍等于期望ID且响应自身ID等于请求ID时，数据才可进入React状态。
- 状态scope、前后分页和结案统一调用`clearReportSelection`：先把期望ID清空、递增代次、取消当前controller，再清详情。组件卸载也取消请求。举报列表本身继续由effect cleanup abort旧scope请求；About复用BUG-114更严格的abort、generation和响应locale验证。
- claim/takeover/resolve在开始时冻结`reportId`。服务端命令一旦发出仍按其业务结果完成，但续体若发现用户已选择其他报告，就不能重载旧详情、清空新详情或显示旧错误；这把不可撤回的权威命令和可丢弃的界面结果明确分开。
- 无API/Schema变化，generation保持161。纯状态矩阵与全量第一方门通过，既有BUG-112责任锁源码合同更新为冻结reportId。BUG-118按Medium/P1关闭：旧竞态可能诱发授权管理员误处置，但不让无权限主体访问或扩大数据范围，且需要乱序网络和后续人工动作。

## D-286：举报证据完成项必须逐文件提交到界面状态

- OSS ticket、对象PUT与complete对每个文件独立产生外部事实，不能把成功结果只留在局部数组直到整批结束。通用顺序批处理边界在每项成功后立即回调，React以完成ID去重追加；一项失败记录后continue，后续文件仍可完成。
- 失败也使用结构化状态而非只保留最后一个字符串：保存originalName和具体错误，逐项渲染，并用批次提示说明已完成文件仍附加。举报提交的`evidenceIds`始终由屏幕可见成功集合派生，成功提交后才同时清空证据和失败列表。
- 不为本项新增删除API。未绑定`report_evidence`已经由Schema默认`cleanup_after=now()+24 hours`，命中`idx_report_evidence_cleanup`并由维护事务进入OSS deletion outbox；这满足可靠短期清理，避免扩张服务协议。用户从当前表单移除后对象也会按该合同过期。
- 无Schema/API变化，generation保持161。成功→失败→成功行为测试、真实PG evidence原子注册/重试和全量第一方门通过。BUG-119按Medium/P1关闭：旧路径丢失举报证据并浪费临时存储，但对象受24小时清理且没有越权或泄露。

## D-287：自动草稿只以最近成功的远端快照判断脏状态

- 编辑器初值只描述本次页面启动状态，不是永久“无需保存”值。旧`baselineRef`让A→B成功后回到A直接跳过，远端永久停在B；恢复远端B后撤销到初始A也发生同样分裂。统一判定只比较当前序列化payload与`lastSavedRef`，后者初始化为页面值、恢复后改为恢复payload，随后只由成功保存推进。
- 异步保存还必须标记实际发送的内容。每轮在调用API前冻结payload与serialized snapshot；响应成功只把该snapshot记录为已保存，不能在await后重新序列化此刻界面值并冒充已经提交。若保存期间又发生编辑，完成第一轮后立即比较当前值并执行一次有界非keepalive追写；若追写失败，最后成功snapshot保持不变，下一轮定时或页面隐藏保存仍能重试。
- 不建立无限追写循环：一次触发最多连续写两次，避免持续输入让请求链永不释放；第二次请求期间再变化由既有15秒定时器和visibilitychange入口继续收敛。`savingRef`仍阻止并发批次，实际请求顺序和远端last-write事实一致。
- POST草稿、恢复与完成协议、九个共享Hook调用方和generation161均不变。A→B→A、保存中B→C、恢复B→A测试及完整第一方门通过，后端Draft相邻合同复核。BUG-126按Medium/P1关闭：旧路径可使本人远端草稿与“已保存”界面分裂并在恢复时丢失最新意图，但不越权或泄露。

## D-288：项目关注集合只保留scope-bound keyset分页

- 关注关系的稳定顺序是`created_at DESC,project_route_id ASC`；route ID既是关系外键也是唯一tiebreaker。游标保存最后返回项的两个值并绑定当前用户、规范搜索、规范项目类型和页大小；未知/重复参数、非法枚举/页大小/游标及任何跨scope复用都在查询前400。读取limit+1后裁剪，只有确有后页才返回nextCursor。
- 不保留offset双权威。原第一方总是显式`offset=0`且没有下一页，因此同批迁移为每页40条opaque cursor；旧开发期调用方必须删除offset。数据库查询使用与既有`idx_project_follows_user_created(user_id,created_at DESC,project_route_id)`一致的谓词和顺序，100k深游标计划为Index Only Scan且无Seq Scan，不随已越过页数线性扫描。
- 隐藏或撤下的已关注项目仍由BUG-036规则返回无名称、URL和更新时间的`unavailable`安全占位，使用户可取消关系；搜索时这类项目只能由本人已知public ID精确命中，不能通过名称枚举历史内容。分页只改变集合可达性，不扩大项目可见性。
- 第一方筛选变更会取消当前请求并递增generation；首面替换，续页按type/public ID去重保序，旧scope结果无法并入。完整临时Schema以含并列时间的125条关系走40/40/40/5零遗漏，隐藏生命周期与双端完整门通过。无DDL，generation保持161。BUG-127按Medium/P1关闭：旧实现让第101项后永久不可管理，但只影响本人关系且不越权或泄露。

## D-289：自动更新GET只合成显示默认值而不创建调度事实

- `project.auto_update.view`与`project.auto_update.configure`是独立能力，GET也可能由预取、重试或跨站顶层导航触发。因此任何GET中的INSERT、enabled变化、nextRun建立或configuredBy写入都属于越权；“事务短且幂等”不改变HTTP方法和授权事实。`writeProjectAutomation`现在只有两次SELECT，源码门明确禁止Begin/Exec/INSERT/configured_by。
- 为保持响应与第一方编辑器可用，服务端在内存按minecraft_versions/changelog/site_downloads补齐缺项。已验证来源只用来预选响应内source：按Modrinth、CurseForge、GitHub优先，Minecraft版本排除GitHub；所有合成项均disabled、无next/last run、无配置者。已有持久项逐项优先，不覆盖管理员事实。
- 唯一写入口仍是PUT：先要求view和configure（或项目编辑/admin能力），严格接收三项，在事务中验证启用来源、许可覆盖权限与许可证后upsert，configuredBy来自真实命令主体。view-only PUT在解码和事务前403；显式配置成功后GET只读取刚提交事实。
- generation161及响应字段不变，无前端迁移、DDL、reset、回填或双写。真实PG证明旧GET首次即启用版本同步；修复后连续GET响应完整但数据库0行，configure PUT才产生3行/1启用。SEC-015按High/P1关闭：旧GET直接跨越独立写权限并可能启动外部任务，但需合法view会话且不直接泄露数据。

## D-290：爬虫AI费用在外呼前由数据库日桶保守占位

- 每个进程先SUM再用局部计数无法表达硬费用上限。新边界以PostgreSQL `current_date`作为日桶，在`hashtextextended('seed-crawler-ai-budget:'||date,0)`事务锁内读取当日实际token与全部在途预留；同一candidate/locale若已有占位不重复调用，同日已结束任务重试保留并继续累加旧input/output，再额外判断新预留，不能用upsert覆盖先前花费。预算不足返回未获准，任何Begin/date/lock/SUM/row/upsert/commit错误都向上返回，绝不把故障解释为零用量。
- 预留必须覆盖请求本身而非事后provider账单。AI task在占位前完成provider/model/prompt解析；prompt硬限1MiB，保守输入按UTF-8字节数加1024协议余量，输出上限取模型配置、32768硬限与context剩余空间三者最小。Context或output配置缺失、prompt放不下均在外呼前失败。OpenAI-compatible和Anthropic都接收同一个有效`max_tokens`，不再由Anthropic硬编码8192或由兼容协议无限输出。
- 成功响应、业务验证失败和携带usage的解析失败都用实际input/output结算并释放未用预留；成功响应也可能省略usage，传输错误也可能发生在供应商已接受并计费之后，因此任何总usage为零的结算都保留完整quota_reserved_tokens，同candidate/locale当日不得覆盖重试。若usage为负、溢出或超过保守预留，或结算数据库失败，同样保留原占位。宁可冻结当日容量也不重新开放可能已经消费的费用；翻译只有completed结算提交后才进入草稿结果。
- generation162给翻译任务加入必填usage_date、非负quota_reserved_tokens及input/output非负CHECK，并以日期覆盖索引支持当日聚合；开发期从161整代重建，不在线ALTER、猜测历史日期、回填或双写。真实4连接PG中并发60+60面对100只一项获准，同任务40+10累计为50而不覆写；另项50占满后已知失败usage10只释放未用40，余40由成功但usage缺失的响应完整保留且1 token也不能重开。100k历史行下当日聚合命中Index Only Scan且0.029ms。SEC-016保持High/P1：旧缺口可直接突破外部费用硬限，但需要并发/重复执行或故障条件，未扩大数据授权。

## D-291：站点Logo公开事实只能是有界静态派生物

- 权限必须先于上传读取，避免未授权请求消耗图片处理资源；获得后端`admin.config.write`确认后，路由先要求multipart，再由`readBoundedRequestBody`检查严格Content-Length并逐块读取。无长度或谎报长度也受实际5MiB文件上限加64KiB封装余量约束，首次越界立即cancel流，之后才用内部有界Request执行`formData()`；所以不再以已物化的File size充当请求体限制。
- 魔数不证明图片可安全解码。`createSafeSiteLogo`只接受Sharp实际识别的PNG/JPEG/WebP/GIF，源边最长4096、单帧最多4,194,304像素、最多128帧、总解码最多16,777,216像素；任何元数据读取、格式、预算或完整解码错误都失败关闭。通过结构预算后另建仅首帧的解码器，自动方向校正并在不放大的前提下缩到512×512以内，固定重编码为最多1MiB静态WebP。没有调用`withMetadata`/`keepMetadata`，测试证明EXIF、ICC与XMP不进入派生物；动画无论源格式都不能被访客继续解码。
- 内容寻址哈希只针对派生WebP，源字节从不写入。上传返回、资产读取正则、客户端本地路径接受器以及后端general config验证同时收紧为`site-logo-[a-f0-9]{20}.webp`；旧raw PNG/JPEG/GIF路径不能继续配置或经动态资产路由读取。开发期不保留第二种兼容路径，现有旧配置应重新上传生成安全派生物。
- 本项无数据库变化，generation保持162；`sharp@0.35.3`从Next可选传递依赖提升为显式生产依赖，保证运行时处理器可部署。SEC-019按High/P1关闭：攻击需要站点配置写权限，但一旦持久化会由所有访客自动解码并影响全站客户端可用性，同时旧multipart路径还消耗服务内存。OPS-008关于本机public、多实例和对象生命周期仍独立OPEN，后续迁移共享OSS时必须保留本安全派生合同。

## D-292：通用访问日志不持久化任何查询内容

- 不选择敏感键denylist。`code/state/token/password/secret`等名字可以变体、嵌套在另一个URL值中，供应商和未来端点也能引入新名称；即使只保存参数名，名字本身仍是攻击者可控内容。通用访问日志不承担业务请求重放，因此没有保存查询内容的必要权威性。
- `logAccess`仍保留method、path、status、latency、response bytes、IP和User-Agent等有界访问事实，但不再把RawQuery作为字符串交给记录构造器。typed payload只序列化`queryPresent`布尔和`bytes`整数；所有键和值在进入内存队列、批量JSON和`app_logs`之前即不存在。原先只清空`/api/yggdrasil/`的路径特判删除，边界自动覆盖OAuth callback、任何恢复/邀请查询和未来Token参数。
- 登录结果、权限变更、管理员操作和反滥用等显式安全日志仍使用各自的结构化最小字段，不能靠通用RawQuery替代；管理员访问日志的查询诊断从具体内容收紧为有/无查询。对外HTTP/API完全不变，内部`api_access.payload`移除`query/queryTruncated`并新增`queryPresent`，无需调用方兼容分支。
- 无DDL或generation变化，保持162。历史开发日志不尝试在线解析和选择性改写，按项目开发期整库重建或既有保留策略清除；不能为历史兼容恢复新记录泄漏。SEC-020按High/P1关闭：泄漏值含一次性OAuth授权码和反CSRF state，并在默认90日内对日志管理员可搜索，尽管利用通常还受短有效期、单次使用和日志权限限制。

## D-293：默认身份只属于空库bootstrap，系统主体不属于交互账号域

- 不用`ON CONFLICT DO NOTHING`逐账号修补。它虽然不覆盖已存在的disabled行，却会在管理员或系统身份被硬删除后重新插入；只判断users当前为空也会在全部账号被明确删除后重建。`seedDefaultUsers`现在在单事务中先以`LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`串行所有插入者，再同时检查用户、`security.default_users_bootstrapped.v1` marker和旧版本必有的`permission.default_roles`证据：只有三者都不存在的pristine安装才插入完整集合和seed权限并写marker。既有数据库首次升级只补marker，不写账号/授权；后续即使users变空也不重建。密码哈希只在初始分支计算，因此`SEED_ADMIN_PASSWORD`不能在重启时重置已轮换凭据。
- autobot不是带假密码的active用户。规范身份使用`status='system'`，全部密码、邮件、Token签发和Session鉴权继续只接受active；automation worker反向只接受精确username/email/system三元组。通用管理员状态命令对该精确主体只允许system、disabled、deleted，既提供显式恢复路径，又不能把它误变成可交互active/banned；普通账号不能取得system状态。
- 已有开发数据库可能保留旧active sentinel身份，因此加入一次性、可并发的窄数据转换：事务先抢占`security.autobot_system_subject_migration.v1` marker，只有首个实例且账号仍是精确email、active、`password-login-disabled`时才改成system，递增auth_version并撤销全部Session。改过邮箱、密码、disabled/deleted状态的账号完全不碰；marker提交后任何后续状态都由安全命令权威维护。硬删除不会由启动恢复，恢复必须经受控状态/开发期重建流程。
- 无DDL、generation或整库重置，保持162；bootstrap与autobot两个marker分别表示安装决策和一次性兼容转换，不是每次启动默认值。SEC-021按High/P1关闭：旧路径可在重启时恢复默认管理员active、seed高权限和环境中已知密码，实质撤销事故处置；利用仍需要触发/等待重启并拥有可用密码或邮箱登录能力，故不定P0。目标`-race`因本机没有CGO C编译器在测试构建前失败，明确保留为最终全局门，不能以真实PG并发测试冒充race通过。

## D-294：公开内容指标不包含个人访客历史

- 总浏览量、热度和页面分布是内容聚合事实；某个已登录用户在某一精确时间访问了内容则是个人活动事实。公开详情渲染不需要后者，现有系统也没有用户逐项opt-in、访问可见范围或屏蔽规则，因此不能把端点“本来公开”当作披露个人历史的授权。
- 不采用匿名化显示名、缩短时间精度或只对登录用户展示。这些做法仍保留可关联侧信道或扩大默认访问者集合，并会形成第二套可见性规则。`contentMetricsResponse`直接删除`recentViewers`，Handler删除`content_unique_views→users`联表和最近8人查询；第一方类型、面板与中英文文案同批删除，不输出空兼容字段。
- `recentEditors`是已公开内容修订参与事实，项目`editors/developers`是现有公开关系，均保持；`totalViews`等聚合统计也不变。`content_unique_views.viewer_user_id`仍被内部统计用于去重和排除开发者访问，因此本项不通过删除列或停止写入破坏聚合；关键边界是任何公开响应都不读取或序列化个人访客身份与`last_seen_at`。
- 无DDL、generation、reset、回填、双写或远端数据库操作，保持162。SEC-022按Medium/P1关闭：任意访客原可枚举稳定用户ID、用户名、头像和精确最近活动，构成隐私泄漏；但没有敏感正文、认证绕过或权限提升证据。项目处于开发期且第一方同批迁移，采用明确breaking removal而非长期保留不安全或空壳协议。

## D-295：MRPack任务配额以用户级事务锁原子占位

- 把三条COUNT放进普通`READ COMMITTED`事务并不足够：不同preview行互不冲突，多个事务仍可在首个任务提交前都读取相同旧计数。配额的串行化键是owner user，不是collection或preview，因为active与daily上限跨用户的全部收藏聚合；只锁某一收藏仍能用不同收藏绕过前两项。
- `reserveFavoriteModpackExportQuota`先取得`hashtextextended('favorite-modpack-export-quota:'||userID,0)`的transaction advisory lock，再以同一聚合查询读取pending/processing、过去24小时全部任务及同collection/version/loader过去30秒任务。锁由PostgreSQL随事务commit/rollback释放，跨进程/实例共享；锁失败或计数Scan失败返回服务错误，不把故障解释为零额度。
- 成功返回只表示当前事务持有占位权，调用方随后在同一事务插入task与全部snapshot items、标记preview consumed并写可靠Outbox；任一步失败整体rollback且不消耗额度。锁必须一直覆盖这些写入，不能在COUNT后提前释放或把task insert拆到另一个事务。Worker完成/取消只会降低active计数，不会创建新任务，因此无需参加创建锁。
- 无DDL、quota表、generation、reset、回填或双写，保持162；现有owner复合索引支撑聚合范围。三种既有429 code与Retry-After保持，预检继续不消耗任务额度。SEC-024按High/P1关闭：并发请求可突破远端请求、Worker与OSS资源硬界，但需要认证账号且未证明权限提升或数据破坏；OPS-013与TEST-034等相邻生命周期/端到端Finding不合并关闭。

## D-296：Sponge调色板容量只由受限条目数决定

- 蓝图体积与压缩/解压字节上限不能保护间接索引分配：一项Palette即可声明接近平台整数上限的编号，而`make(max+1)`发生在BlockData和非空气方块预算之前。安全边界必须先验证身份空间，再分配；事后检查索引范围或依赖OOM恢复都无效。
- Sponge palette被定义为至多`maxBlueprintMaterialCount=8192`项的连续双射：值只接受Go/NBT有符号整数类型，必须落在`0..N-1`且互不重复。实现先验证条目预算，然后只分配N个state和N个occupied布尔；负数、浮点强转、超范围、稀疏和重复均返回稳定解码错误，不访问攻击者索引。
- `decodeVarInts`仍由已检查的volume限制结果容量，但其每个值现在必须引用已验证palette；未知值不再被`continue`吞掉并产出看似成功的缺块文档。合法Sponge v2顶层`Palette/BlockData`和v3嵌套`Blocks.Palette/Data`都通过同一原语，已有三格式往返保持。
- 无Schema、generation、API或第一方变化，保持162。SEC-026从UNRESOLVED复核为High/P1：极小压缩上传可令异步Worker panic或尝试巨额内存、具备可重复服务可用性影响；攻击仍需认证和任务入口，且无权限提升/泄漏证据。SEC-027、PERF-042与TEST-036继续分别处理公共转换额度、渲染规模和完整状态机。

## D-297：公共蓝图转换以数据库全局active容量准入

- 详情页明确向登录用户提供公开蓝图的目标格式转换，因此不把“不是蓝图所有者”本身重新解释为对象授权缺失；转换只读取公开源并产生公开派生格式，没有取得私有蓝图或编辑权。真正缺口是任意多个账号可在普通`READ COMMITTED`事务中各自通过用户限额，使持久任务、Worker下载/编解码、Outbox和OSS派生物没有站点级active硬界。
- 所有`convert`入队先取得固定namespaced PostgreSQL transaction advisory lock，再用`limit 16`的子查询检查全站queued/processing转换。成功后同一事务继续取得已有用户级锁、检查每用户最多4个active、插入task并写NATS Outbox，调用方commit才释放锁；任一查询、锁、入队或提交故障均失败关闭。`normalize`是所有者上传/重试链路，只受用户限额而不被公共转换队列占满阻断。
- 同一蓝图、operation和target format仍由`idx_blueprint_jobs_active_operation`部分唯一索引原子去重。只有PostgreSQL `23505`且`ConstraintName`精确等于该索引才映射409 `BLUEPRINT_CONVERSION_ALREADY_QUEUED`；public ID碰撞或其他约束故障不能伪装成业务重复。全站达到16时返回429 `BLUEPRINT_GLOBAL_CONCURRENCY_LIMIT`和60秒Retry-After，成功202与任务DTO不变。
- 不新增日桶或永久quota行：本Finding的审计缺口是无全局并发/资源准入，现有进程共享2个Worker槽、源/解码/体积/块/材质/输出预算、每用户4任务与同目标唯一性共同形成硬界。百万历史任务的实际计划从active部分索引只读取16项，执行0.221ms且不扫描任务表；无DDL、generation、reset、回填或双写，保持162。
- SEC-027从UNRESOLVED复核为High/P1：认证用户可借公共对象与多账号持续填充高成本队列、放大数据库/Worker/OSS资源，但没有未认证入口、私有数据披露、权限提升或直接数据破坏证据。仅关闭公共转换容量与原子入队；不以此冒充TEST-036完整蓝图状态机或其他独立Finding。

## D-298：SEC-028复用写时OSS额度事实，但以独立安全矩阵关闭

- SEC-028与PERF-044描述同一旧生产路径的不同影响：前者关注并发/数据库故障导致日额度和总容量失败开放、产生无硬界OSS成本，后者关注每次SUM全部历史造成的规模退化。不能因为PERF Finding先登记关闭就让安全Finding保持无证据，也不能为了“逐项修复”再引入第二套额度权威。
- generation127的既有修复是唯一实现：预签名在用户transaction advisory lock内建立绑定`user/object/date/source/stored/expiry`的持久预留；失败、multipart abort、重复完成和过期清理释放预留。完成在同一用户锁事务移除自身预留、以实际source/stored重新检查，并用该事务插入active `oss_files`；trigger在提交前更新总量/日桶。因此即使客户端绕过预签名直接调用完成，也不能跳过最终准入。
- 缺行是新用户零用量的合法状态；任何其他Query/Scan、锁、计数器、预留、trigger或commit错误都向上返回，非业务额度错误映射500。新增SEC028专属故障测试删除日桶关系，证明预留和完成均失败、先插总桶随事务回滚且不会被解释为0；两笔600/1000直接完成在独立连接中精确一成一拒并只登记600。
- 不新增本轮DDL或兼容路径，当前generation162继续使用127引入的三表、expiry索引、active delta trigger及离线rebuild。用户额度读取显示active事实而不把reserved伪装成已用；常规准入不扫描`oss_files`，百万历史陷阱实测2.1706ms。API/第一方无需迁移。
- SEC-028从UNRESOLVED复核为High/P0：已认证用户原可用并发或额度数据库故障突破持久存储硬界，直接增加对象存储容量和费用；影响是可持续、跨请求的资源边界失效。ARCH-020其他读取/审计语义、BUG-079/080、OPS-018及TEST-037仍按各自生命周期和真实供应商矩阵独立处理。

## D-299：稳定OSS引用不再充当ESA私有访问授权

- `PublicEndpoint/objectKey`是便于持久化、迁址和识别对象的稳定引用，不含访问主体、用途或期限。即使ESA部署当前配置私有回源，后端无法证明边缘会执行API返回的`ExpiresAt`，也无法使已复制链接在撤权后失效；把部署假设命名为“temporaryDownloadPolicy”会产生虚假的安全合同。
- 不引入本站流式代理或自建边缘Token协议。现有OSS SDK已能产生由存储服务验证的短期签名，因此所有`resolveOSSObjectAccessWithConfig`调用统一使用它；bucket/object path、`response-content-disposition`等响应参数和有效期进入签名。两个不同对象即使同名也有不同signature，修改路径或下载用途不能复用原签名。
- 下载duration先取调用方显式值，否则取配置，最后默认10分钟，并在唯一解析边界硬截到60分钟；`ExpiresAt`与传给`PresignExpires`的是同一个受限值。管理员临时链接请求和持久配置也在入口截到60，避免调用方或旧7天配置绕过共享边界。
- `esa_private_origin`、`esa-private-origin`、空值和未知旧设置在读取时全部安全归一为`oss_presigned`，默认配置也改为签名。无需数据库迁移或双读；旧加密JSON在新代码首次读取即不再生效为稳定授权，下次管理员保存时持久化规范值。第一方删除ESA选项，将PublicEndpoint描述为只用于稳定存储定位。
- 所有已有下载/redirect响应字段保持，`downloadUrlMode`收敛为单值，URL host/query与TTL上限是有意breaking安全变更。公开资源也经端点重新取得短签名；缓存可依赖业务redirect的既有策略，不能恢复稳定直链来换取缓存。真实OSS签名接受/过期由TEST-037继续做供应商矩阵，本项以SDK签名形状、对象/用途差异和全部调用链回归关闭代码缺口。
- SEC-029从UNRESOLVED复核为High/P1：已授权用户拿到一次私有链接后原可无限期转发，撤权/删除会话和声明过期都无法限制后续读取；但初次取得仍经过对象授权，未证明对象Key批量枚举或直接权限提升，故不定P0。

## D-300：GIF可信判定先做完整结构预算再解码全部帧

- `image.DecodeConfig`和`gif.Decode`只能证明逻辑画布与第一帧可读，不能证明动画剩余字节、帧数、累计解码量或时长安全。通用校验一旦把对象标为clean/trusted，蓝图封面、资源图片和图标链路会把同一字节交给浏览器或其他解码器，因此可信边界必须覆盖公开消费者实际看到的完整动画。
- 不直接以`gif.DecodeAll`后统计预算作为唯一防线，因为超额动画的帧图像已经分配。`inspectGIFAnimationBudget`先线性读取至严格trailer/EOF，验证GIF87a/89a、逻辑画布、全局/局部颜色表、扩展sub-block、image descriptor边界和LZW块结构，同时在加法前检查120帧、64,000,000累计descriptor像素与30秒GCE delay；超过任一硬界即在完整帧分配前返回。
- 预扫描不把压缩数据视为解码正确。预算通过后仍调用`gif.DecodeAll`验证每帧LZW数据，再要求解码帧数/delay长度、画布、每帧边界、累计像素与预扫描事实一致；缺trailer、trailer后额外字节、未知块、后续帧截断或元数据矛盾均失败关闭。通用画布继续受16,777,216像素限制，调用者原有16MiB压缩字节上限保持。
- 贴纸原逻辑也是先`DecodeAll`再检查动画预算，现复用同一入口把预算前移，后续仍由既有重编码剥离评论等元数据。合法静态或有限动画、PNG/JPEG/WebP路径、HTTP DTO与正常状态码不变；旧超预算或后续畸形GIF被拒绝是有意的输入收紧，不提供兼容开关。
- 本项无DDL、数据库访问、generation或前端代码变化，保持162。SEC-030从UNRESOLVED复核为High/P1：攻击者需有上传路径并让对象进入公开消费，但成功后可把≤16MiB压缩对象放大成远超首帧检查的客户端或图像链路资源成本；尚无未认证直接放大或服务端P0级接管证据。

## D-301：评论子资源权限由评论与实时目标可见性的交集决定

- 评论`public_id`是稳定定位符，`status in ('published','deleted')`只描述评论自身展示状态；项目审核、皮肤/档案visibility、目标active状态和当前主体所有权才决定正文现在能否被读取。目标从公开变pending/私密后不能让既有评论ID、reaction或watch关系成为第二条持久访问能力。
- 建立唯一`resolveVisibleComment`：规范评论ID并取得ID/root/status和目标三元组，然后复用`resolveCommentTargetByInternalWithQueryer`的完整类型矩阵和当前claims。评论缺失或目标对当前主体不可见统一作为`pgx.ErrNoRows`映射404；真正数据库故障保持500，不把解析失败误作目标不存在。旧`numericCommentID`删除，避免未来子路由重新只验证评论状态。
- thread在读取整树前解析，replies在分页查询前解析，edit/delete/pin/reaction/watch在任何授权细分或写入前解析；附件继续要求published且复用相同入口。按watch ID的read/mute先验证该关系属于当前用户，再解析其评论目标。目标所有者和全局评论管理员继续由既有目标规则访问，项目恢复approved后普通访客无需迁移即可恢复。
- “我的插眼”不能返回`Target{Type,InternalID}`空壳同时保留完整Comment。批量页先以原有一个UNNEST/UNION查询解析去重目标，只把命中的评论ID交给正文/作者/表态/附件装配；不可见项直接省略。cursor仍基于用户私有watch稳定顺序，因此不新增N+1或offset扫描，也不通过填补页面泄露不可见项数量以外的新事实。
- 本项无DDL、generation或前端改动，保持162。不可见直接子路由200/403→404和watch列表省略项是有意breaking安全变更，不保留空响应兼容。SEC-031从UNRESOLVED复核为High/P1：知道评论或watch ID即可跨越目标隐私读取正文并写关系，但未获得目标编辑/管理能力且ID需预先获知。

## D-302：评论插眼硬额度在用户数据库锁内原子准入

- `COUNT(active)`与后续upsert分属自动提交语句时，两个不同comment ID都能观察1999并分别成为第2000条；唯一`(user_id,comment_id)`只防同一目标重复，无法约束用户集合基数。进程内mutex也不能覆盖多实例，因此准入串行化必须由共享PostgreSQL提供。
- `ensureCommentWatch`现在开启事务并以用户ID组成域分离的64位`hashtextextended`键取得transaction advisory lock。锁内先读取目标关系：若已active，直接提交并返回，保证满额重试幂等；不存在或cancelled才读取active容量。计数从匹配索引最多读取2000项，达到硬界返回typed sentinel并rollback，否则在同一事务upsert、复读状态并commit后才报告成功。
- 不新增可漂移的quota表、trigger计数器或异步补偿。现有`idx_comment_watches_user_activity(user_id,status,last_activity_at desc,id desc)`覆盖等值前缀，`limit 2000`让旧超额开发数据也不会线性扫描全集。不同用户锁键独立，同一用户的并发准入精确串行；取消未持锁最多造成一次保守拒绝或更少active，不能使集合超过上限。
- 锁、count、upsert、状态读取或commit任一错误向上传播500，不解释为零容量占用；稳定`errCommentWatchLimitExceeded`只映射业务400。直接watch PUT保持原有中文文案，纯CY私有watch命令也使用同一400而不再把合法满额误报为服务器错误。
- 无DDL、generation、前端或成功DTO变化，保持162。SEC-032从UNRESOLVED复核为High/P1：旧竞态可持续突破持久硬限并放大每次回复的mapping/计数/通知成本，但需要认证用户、大量不同可见评论和并发/后续活动，影响域主要是本人watch集合。

## D-303：评论分支只返回可续页的定位邻域，并对实际JSON设硬界

- 深度256只限制纵向层数，不能限制同一层的横向节点数；在读取整棵root后再截断响应仍会完成所有表态聚合、权限判断、头像解析和附件装配，无法关闭数据库与内存放大。因此预算必须先作用于候选numeric ID，再进入共享评论装配器。
- 首面定位模型固定为最多16个就近可见祖先、焦点评论和剩余容量内的直接回复；后续页只返回焦点的直接回复。祖先使用closure的descendant/depth索引读取17行判定截断，回复复用既有parent/created/id部分索引和viewer绑定opaque keyset cursor；总候选最多64，邻域展开深度固定一层。每个返回节点原有的直接回复游标继续承担向下浏览，因此深层内容可达但不会被一个请求递归物化。
- 节点上限不足以限制10k正文和附件DTO。服务端在成功写头前编码完整`apiResponse`，要求不超过512KiB；超限时从页尾移除可选回复并让next cursor停在最后真实返回行，若仍超限再移除最远祖先并标记`pathTruncated`。焦点和至少一个可推进回复不可被预算裁掉，否则失败关闭而不发送部分200。
- 第一方分支页消费`nextCursor`并按ID合并；请求结果绑定评论ID和登录主体，切换评论/账号或重复点击时旧响应不能写入当前页。深祖先被省略时显示明确提示，引用父项若不在当前DOM则导航到该评论自己的分支，保持向上可达。旧客户端仍能读取原`items/focusId/target`，但依赖一次性完整树属于有意移除的不安全行为，不提供兼容开关。
- 无DDL、generation、reset、回填或新索引，保持162。SEC-033从UNRESOLVED复核为High/P1：匿名请求可把大型公开树转化为重复数据库、内存和带宽放大，但不越权读取私密数据、没有持久破坏，且需先存在大型公开树。TEST-038的更广评论权限/生命周期矩阵不借此关闭。

## D-304：私有皮肤只能与私有玩家档案组成可持久组合

- `player_profiles.visibility`和`skin_assets.visibility/review_status`不能各自独立授权同一个响应。档案公开只说明档案主体可读，不会把其引用的private或未批准资产隐式升级为公开；`canUse=false`更不是hash、URL和完整纹理已经返回后的访问控制。统一读取规则为：资产owner可管理自己的纹理，其他viewer只可取得active、approved且非private资产；不满足时`skin/cape`为null且不返回任何资产身份或纹理地址。
- 只修读取会让数据库继续积累自相矛盾关系并在未来新入口复发，因此把组合不变量放到三条写路径。公开或未列出档案装备private资产返回409；private档案显式改public/unlisted时查询当前active private纹理并返回409；资产发布private时锁定全部引用档案，只要owner仍有active非private档案就返回同一typed冲突。owner先把档案改private或解除装备后即可重试，不由服务端静默改变用户档案可见性。
- 资产编辑按asset行再按profile ID升序加锁，装备路径也先锁asset再锁profile；档案发布先锁profile并在快照查询中复核asset。若档案发布与资产私有化并发，先提交者成为后提交者锁内看到的事实：要么profile查询private后拒绝，要么asset发布看到nonprivate后拒绝，不存在两笔都通过的窗口。待审提交先做同一预检，审核批准再次权威复核；409不解决change request，owner修正档案后可重试批准。
- 不在`/api/yggdrasil/textures/{hash}`按asset行做反向ACL：内容哈希Blob可被public/private多个asset共享，且已公开字节不可撤回。安全边界是未经授权的档案响应不再披露hash；本项不伪造对既往已知hash的撤回保证。公开/未列出档案只允许approved非private当前纹理，private档案及Yggdrasil运行语义保持产品既有边界。
- 无DDL、generation、reset、回填、双读或前端字段变化，保持162。SEC-034从UNRESOLVED复核为Medium/P1：旧匿名响应直接泄露用户标为仅自己可见的完整纹理，但触发条件要求同一owner主动形成矛盾组合，未跨其他用户资产授权且无凭据、权限提升或持久破坏。TEST-039其余皮肤全生命周期覆盖保持独立。

## D-305：收藏长期库存按用户数据库锁和关系净增量原子准入

- 反滥用请求窗口只能降低写入速度，不能限制账号数月累积的数据库库存。额度事实定义为每用户`favorite_collections`行数和其全部集合下`favorite_collection_items`关系行数；同一目标放入多个集合会真实占用多行、触发多次索引与级联成本，因此按关系计数而非按distinct目标计数。默认100集合/10,000关系兼顾最多100集合的选择器产品边界，部署可调但统一钳制到1,000/100,000硬上界，零值或非法负值安全回落默认。
- 显式集合创建和缺省集合补建在写事务内以`favorite-stock-quota:{user}`的64位PostgreSQL transaction advisory lock串行，然后最多读取配置上限+1并在同一事务插入。已有default先走单次只读快速路径，因此百万集合页仍固定“default存在查询+页查询”两条SQL；缺default时锁内再次复核，不能由并发GET重复占位或绕过集合上限。
- SEC-035要求成员目标解析和关系变更共享Repeatable Read可见性快照。若先开启该事务再等待xact advisory lock，PostgreSQL可能保留前一提交前的旧快照并错误放行，因此成员路径在专用池连接上先取得同一键的session advisory lock，再开始Repeatable Read事务；commit/rollback后用独立5秒context解锁，解锁不确定即Hijack并关闭连接，绝不把持锁连接归还池。session锁与集合路径的xact锁使用同一数据库键，跨实例和四个入口互斥。
- PATCH在全量owner集合验证后计算`新增且原不存在 - 请求删除且实际存在`；PUT用目标现有关系与规范化desired集合计算净差。只有正增长需要满足剩余额度；delta为0或负数始终允许，所以满额重复保存、集合间等量迁移以及运维下调配额后的逐步清理不会被锁死。总量查询只读maximum+1，目标/请求集合查询受既有100项请求界和target复合索引约束；数据库读取、锁、commit任一故障失败关闭为5xx。
- 四个增长入口返回统一typed sentinel：集合超限409 `FAVORITE_COLLECTION_LIMIT`，关系超限409 `FAVORITE_ITEM_LIMIT`，details携有效limit。成功路由/DTO、PERF-051游标页、BUG-089无效集合400和SEC-035动态目标可见性不变；第一方已有通用API错误展示，无字段迁移。无DDL、计数表、trigger、回填、双写或generation变化，保持162；SEC-036沿用审计Medium/P1，TEST-041基础收藏更广行为矩阵不借此关闭。

## D-306：PlantUML源码只能进入固定同站自建代理

- PlantUML的压缩/编码路径可逆，不能把它当作隐私保护。Markdown预览和独立工具原先会在用户输入时直接加载`plantuml.com`图片，等于把完整图表源码自动交给第三方；CSP内建该域还把这条外送路径伪装成平台默认能力。
- 浏览器唯一受支持的渲染根固定为相对路径`/plantuml`，实际SVG为`/plantuml/svg/{encoded}`。不允许管理员配置绝对URL、协议相对URL、查询、fragment或其他路径；部署必须在同站入口把它反向代理到自己控制的自建PlantUML实例，不能再以内建公共服务作为兜底。独立工具复用同一常量，CSP无需额外PlantUML origin。
- 后端和前端默认都将PlantUML关闭。历史`system_settings`若仍含外部server，后端读取归一化会把路径改为`/plantuml`并关闭开关；前端对任何漂移响应再做同样的失败关闭。管理PUT在持久化前拒绝外部值，因此旧风险既不会继续执行，也不能由配置面重新引入；合法`/plantuml/`只规范为无尾斜线。
- 管理端把路径显示为只读并双语说明启用前置条件，README记录自建/不转发第三方合同。GET JSON字段保持兼容，但默认开关和server值改变，外部server PUT由成功变400是有意breaking安全收紧；前后端应同批部署。无DDL、generation、reset、回填或数据库访问，保持162。SEC-001沿用审计Medium/P1：源码可能含内部设计与标识，但不含站点凭据且需用户主动使用图表功能，故非High。

## D-307：供应链状态以权威锁、官方扫描和先归档后阻断为发布事实

- “工具未安装”不能证明依赖安全。后端以`go.mod/go.sum`为权威并使用Go官方`govulncheck`的可达调用分析；前端当前只有`package-lock.json`，因此使用npm自身audit而不是生成第二种包管理器锁。两条门都覆盖push、pull request、每周计划和手工运行，使新提交与数据库公告更新都能重新触发。
- 扫描命令允许步骤暂时标记失败，只为让后续`always()` artifact步骤上传JSON；最终独立enforce步骤读取原扫描outcome并退出1。安装/扫描/报告缺失同样不能形成绿门。报告保留30天；checkout、setup-go/setup-node和upload-artifact全部固定到已核对release的40位提交SHA，避免浮动major tag成为新的供应链盲点。
- 首次有效扫描不能只当历史记录：`govulncheck v1.7.0`发现x/image一条和Go1.26.5标准库六条可达漏洞，因此工具链升1.26.6、x/image升0.45.0，并把有修复但当前不可达的compress也升1.18.7；npm发现brace-expansion、js-yaml和nanoid三条High，锁文件分别升至5.0.9、4.3.1和3.3.18。最终Go affected/imported package均0，npm全部严重度0。
- brace-expansion不能全局强制5.x：旧minimatch需要1.x CommonJS接口，首次完整Lint以`expand is not a function`失败并阻止关闭。最终删除过宽override，让新依赖使用5.0.9、旧依赖使用不在公告范围的1.1.18；锁测试遍历所有实例并拒绝4.x及5.0.0..8。x/crypto/openpgp的模块级公告没有修复版本且该package未导入/调用，govulncheck仍在JSON记录但affected为0，不伪称公告不存在。
- 本项无Schema、数据库或HTTP协议变化，generation162保持。最低后端工具链patch和依赖锁需要所有构建环境同步；代码回滚会恢复已证实漏洞和未知发布状态，不提供兼容价值。OPS-002仍负责完整CI/CD与部署，TEST-003仍负责Race工具链，本项不借两个安全workflow合并关闭它们。

## D-308：多副本共享限流故障时失败关闭，单副本才允许本地额度

- 进程内fixed window即使把额度调低，也只能把旧放大量从“副本数×正常额度”改成“副本数×降级额度”；副本数扩展、滚动发布或故障域变化仍会改变安全语义。多副本环境必须把共享Redis视为限流安全依赖，不能把连接错误解释成新的本地额度。
- 启动与运行期分开处理：`APP_REPLICA_COUNT>1`时配置校验强制`REDIS_ENABLED`、`REDIS_REQUIRED`、`REDIS_RATE_LIMIT_FAIL_CLOSED`和`REDIS_AUTH_RATE_LIMIT_ENABLED`全部为true，运行时在初始化阶段Ping；已经启动后的任意Redis命令错误由`ConsumeRateLimitPolicy`返回`Allowed=false/Backend=unavailable`，等待建议最多5秒，不触碰local计数。Redis恢复后下一请求自然回到共享Lua计数。
- 所有使用`ConsumeRateLimit`/`ConsumeRateLimitPolicy`的统一反滥用、crawler/高成本公开读取、登录、Presence、浏览量和草稿写入获得相同边界。认证入口即使收到手工构造或漂移的`AuthRateLimitEnabled=false`，只要失败关闭策略已生效仍走共享决策；配置校验同时拒绝这种多副本漂移。后台基础设施指标新增`rateLimitFailClosed`，与既有Redis errors/timeouts共同区分安全拒绝和本地fallback。
- 单副本开发和明确单进程部署保留有界local fallback，避免把非分布式环境变成Redis硬依赖；若单副本主动开启失败关闭，也必须启用Redis。自动演练以两个共享namespace的客户端先证明额度跨实例累计，再关闭Redis，验证两者都拒绝；另锁定登录与匿名高成本读取，避免只修底层而被调用方旁路。
- 本项不新增PostgreSQL热计数、DDL、generation、reset或前端协议，保持162。运行故障时相关入口由可能继续成功收紧为既有429/Delay是有意安全行为；普通缓存仍可按各自数据库回源/本地策略降级。SEC-003沿用Medium/P1：利用需要多副本和运行期Redis故障，影响是额度放大而非身份或权限绕过。

## D-309：认证与授权版本先删除旧指针，再加载发布且调用方必须检查

- `auth_version`、`permission_version`和两个`runtime_versions`行由数据库触发器/事务拥有；Redis只是最多10秒的跨实例指针。旧流程提交后先查询再Set，且Handler丢弃查询/Set错误：查询失败时旧指针从未被触碰，正是旧Session或旧权限继续命中的窗口。正确顺序必须是已知精确key的DEL先发生，然后才查询权威事实。
- 四类刷新收敛到`refreshSharedVersionPointer`：`DeleteShared`同时清理当前进程并把Redis DEL错误返回；随后读取DB。查询失败时已成功删除的指针保持缺失，读路径自动回源DB；SET成功会原子覆盖旧值，因此可安全吸收前置DEL的瞬态错误；Redis完全禁用时查询成功即足够，因为共享读从不使用本地版本缓存。安全状态在提交前已通过`pathUserIdentity`得到public ID，提交后无需先做可能失败的身份查询才能确定auth key。
- 提交后的不确定性不能伪装成事务失败或200成功。31个生产Handler的`_ = refresh*Version`全部改为`requireSecurityVersionRefresh`；任一最终失败写结构化slog、递增`securityVersions.refreshFailures`并返回503 `SECURITY_VERSION_REFRESH_FAILED`，details明确`committed=true`和operation，提示调用方复读。封禁到期Worker无HTTP响应，仍执行delete→query→checked Set并带user ID记录错误。
- 这条边界覆盖用户状态、治理封禁、用户/角色权限、权限组线路、作者认领/项目编辑、创建/审核导致的project ACL版本变化；不能只修原始状态Handler后继续让相同根因从其他写入口复发。治理app log移到刷新前，事务内permission audit/outbox保持原子；发布失败不会抹掉已经提交的安全事实。
- 真实PG会话临时generation162先用错误identity制造刷新查询失败并确认Redis旧auth key已删除；重新预热后关闭Redis，再提交disabled状态，响应为可辨503而DB状态/auth版本均已推进，旧Session通过共享缓存、本地session缓存和DB回源组合仍被拒绝。既有session/RBAC/permission和后台授权故障集成同步通过。
- 本项无DDL、generation、reset、回填或前端字段迁移，保持162。正常成功协议不变；503是缓存发布故障下有意收紧，客户端必须按`committed`复读而非盲重试。SEC-004沿用Medium/P1：旧窗口有10秒硬上界，但封禁、停用和删除要求即时撤销。

## D-310：前端测试入口必须自动发现全树，不能维护静态文件清单

- TEST-001登记时前端既没有测试文件，也没有可执行入口。当前修复过程中已经按各业务Finding累积出110个`*.test.mts`文件和261条用例，覆盖状态代次、分页、权限展示、上传生命周期、浏览器资源预算、CSP、本地化与部署合同；继续把TEST-001描述为“零测试”已不符合当前事实，但仓库原`test`脚本只手列47个文件并另用`pretest`跑1个，仍会让62个既有文件及未来新增文件静默退出正式门禁。
- 唯一入口改为Node原生`node --test`。该runner在当前固定Node24工具链下递归发现默认命名的`.test.mts`，无需shell glob或平台专用枚举；删除`pretest`避免同一文件特殊待遇。新增`frontend-test-gate.test.mts`从仓库事实反向锁定脚本、禁止恢复pretest，并确认真实测试集合与自检文件存在。修复前定向用例精确失败在静态脚本，修复后`pnpm test`自动执行110文件/261用例。
- 审计建议的Vitest/RTL与Playwright是可行实现，不是唯一协议。当前Node原生层已经实际、零依赖地消除“无自动化测试/无test script”，不为框架名再引入第二套runner；需要DOM、浏览器、Realtime、通知、爬虫等更深行为的缺口仍由TEST-019..TEST-048等逐项验收，不能借TEST-001合并关闭。
- 本项无生产TypeScript、浏览器bundle、HTTP/DTO、Schema、generation、数据库或部署配置变化，保持162。标准`pnpm test`成为前端测试发布入口；TypeScript、ESLint和58页正式Turbopack production build同步通过。TEST-001沿用High/P0，因为旧状态使全部复杂前端交互缺少统一回归入口。

## D-311：通知测试必须从用户可见HTTP事实贯穿水位线、缓存和可靠AI任务

- TEST-021登记的核心不是函数数量不足，而是旧测试只认固定文案、排序辅助和read-all路由字符串：它无法证明用户A不会看到用户B的直接通知，游标不会重漏，单条/全部已读会写出正确用户事实，也无法证明缓存漂移最终回到PostgreSQL权威。分页、未读、翻译和异步结果后来已分别修复，但若没有一条跨这些边界的真实Handler矩阵，仍可能各自“单测绿、组合错误”。
- 新矩阵在当前generation162的单连接会话临时完整Schema创建两个用户、两条A直接通知、一条B直接通知和一条广播。它以生产Handler和统一JSON envelope验证A的两页顺序、B的独立列表、system translationAllowed=false；单条已读只让A的3降2，尝试读取B通知保持非枚举200但零receipt；read-all只让A降0且B仍2。随后更新已在水位线内的广播，先确认本地派生仍为0，再运行生产reconcile，把DB真值1写回缓存并由HTTP读到1。
- 翻译边界在同一隔离Schema证明system广播稳定403、注册alias`EN_us`规范到`en-US`缓存并200、未注册`pt-BR`在任务创建前400。可靠投递与结果完整性不在新测试里伪造NATS：既有真实临时PG矩阵证明notification AI task与Outbox同事务，损坏payload/result/item返回5xx且Worker持久化失败可见；百万行测试证明两页只执行两条SQL、read-all一条SQL并命中direct/broadcast keyset索引。
- 本项不改生产代码、Schema、generation、HTTP/DTO或缓存算法，保持162；全部数据库测试只使用会话临时完整Schema/temporary tables并自动清理，公共generation155不写入。TEST-021沿用High/P0；私聊/Presence和Realtime Hub分别属于TEST-022/023，不能因通知矩阵完整而合并关闭。

## D-312：自动更新验收必须贯穿Worker编排，并与已修的镜像和发布矩阵组合

- TEST-019的缺口不是再为日期阈值增加纯函数断言，而是没有一条测试真正执行`scheduleDue → processOne → fail/retry → execute → publish`。相关SEC-015、BUG-035/038/039、DB-006和PERF-026即使各自修完，只要调度去重、服务身份、租约、错误分类或事务编排漂移，整条自动更新仍可能停止或重复发布。因此新增矩阵使用生产Worker、当前generation162完整Schema和本机可控Modrinth HTTP server，不直接调用内部同步函数冒充任务成功。
- provider第一次返回503，run必须保留pending、attempts=1、`external_service_unavailable`、未来重试时间且清除lease；强制到期后第二次领取必须以同一run completed、attempts=2、created=1并记录autobot actor。一次changelog设置实际会在同一项目更新事件合并`changelog/minecraft_versions/project_version`，所以测试锁定三类同步的组合事实，而不是只看run状态。
- 再次到期运行相同provider内容必须允许第二个completed run，但external binding、changelog、auto revision、project update event、notification task和NATS Outbox都保持一份；这同时验证版本映射/日志幂等和30秒项目事件合并。`site_downloads`在无redistribution许可时必须不请求provider并直接dead-letter `license_denied`，不能把永久错误无限重试；过期running lease由tick恢复pending且保留attempts，随后可由正常claim路径接管。
- 新矩阵不重复伪造OSS和扫描器：既有真实临时PG测试分别覆盖相同内容跨项目身份/上传失败持久补偿、全局镜像槽、bounded stream/hash、scan拒绝后可重扫、clean提升时原子事件/通知，以及维护状态审计修订；十组相关集成在同一门中组合通过。陈旧DB-006测试只需证明generation至少包含148的约束修复，不应把当前临时Schema写死为148并在generation162误报。
- 本项只增加/修正测试，无生产代码、Schema、generation、HTTP/DTO、provider或OSS协议变化，保持162；所有持久状态位于单连接会话临时Schema并自动Drop，provider只监听本机。TEST-019沿用High/P0；单节点临时PG与httptest不能替代真实供应商限流、OSS中断、恶意扫描器或多副本灾备演练，这些能力边界不在本项中夸大。

## D-313：爬虫run容量、租约所有权和provider结果必须形成一个可恢复状态机

- `maxConcurrency`不能只存在于管理表单。schedule按配置建立有界worker lane，并保留旧每轮至少4个run的批处理下界；每个lane领取前在短PostgreSQL transaction advisory lock中重读当前配置并计数尚未过期的running。这样同一实例能实际并发，多个实例又共享一个全局cap；调低配置不会杀死已运行任务，只会停止新claim直到容量回落。外呼期间不持数据库锁或连接，容量由持久lease事实而非进程semaphore决定。
- 固定字符串owner无法区分实例或attempt。每次claim生成128位随机hex token并写入`lease_owner`，默认5分钟lease由每分钟heartbeat按`run id + running + exact owner`延长。heartbeat错误或RowsAffected=0会取消传给provider/importer/AI的run context；actor记录、失败转pending/failed及completed+config更新时间都以同一owner做CAS并要求恰好一行。旧Worker即使稍后返回，也不能覆盖已被恢复/重领的run。
- provider分类错误必须在run层有语义。每个project type抓取成功/失败分别累积`providerSucceeded/providerFailed`；所有尝试分类都失败时返回聚合错误，复用attempt指数退避并在第五次进入failed；只失败一部分时保留可用分类结果、completed并在stats明确degraded。候选内部导入/翻译/提交错误继续记入`failed`，不能与基础provider全不可用混为一谈。
- TEST020以正则命名、自动Drop的随机隔离数据库安装generation162完整Schema，并用8连接池和本机Modrinth httptest贯穿上述状态：两类503后同run重试；dry-run只写两个candidate而不写draft/import/project；一类成功一类失败；配置2/1的阻塞外呼；短lease心跳越过过期点；独立连接Worker受cap；篡改owner后旧请求取消且状态不被覆盖，过期恢复后attempts2完成。BUG040/041/042、SEC016和百万行分页作为同组验收补齐AI、自动提交、来源事务、血缘、预算与规模。
- 无DDL、generation、reset、回填或双写，保持162；run stats仅添加两个可选整数，管理和公开成功DTO不删字段。OPS005沿用High/P1，OPS006与DEAD005沿用Medium/P1，TEST020沿用High/P0。隔离多连接PG/httptest证明代码协议与数据库互斥，不代表外部供应商Exactly Once或跨Region灾备。

## D-314：搜索滚动发布必须让持久代次单调，并让所有投影路径共享一个注册事实

- BUG-058的独占rebuild/共享drain lease解决了同版本实例的快照增量丢失，却没有解决不同二进制版本的方向性：旧实例等待新实例释放锁后仍会把`storedVersion != localVersion`解释成“需要重建”，重新创建旧collection并回切alias；已ready的旧实例甚至不会再检查projection state，能继续claim后向新alias写旧文档形状。租约只能提供串行，不能自动提供版本单调性。
- Worker现在持显式本地projection version。任何rebuild先查询`search_index_state`最高代次，独占锁内再次检查；任何drain取得共享lease后、claim前也执行相同检查。数据库代次高于本地时返回可判定的superseded error，旧实例不创建集合、不切alias、不claim或调用Typesense。state upsert只允许当前值小于等于候选值；重建按固定registry顺序逐collection判断alias/state/collection是否完整，失败重试不会重建已完成的同代集合。
- `searchRegistry`成为5个collection和7种document的单一权威，同时保存collection Schema、document归属、稳定ID keyset SQL和loader。`collectionSchemas`及旧测试helper只从它派生；增量路径先查注册表，未知类型不再默认为resources。数据库queue CHECK闭集保持原七值，并由测试逐项比对。类型新增必须同时在一个registration中提供Schema归属、ID页和loader，再显式更新数据库闭集。
- 删除delete失败后无行为的`failed/continue`。load/import/delete错误继续按token持久retry，但每次retry写都检查错误；成功ack删除也返回错误。一个批次的外部故障和retry/ack SQL故障通过`errors.Join`共同返回，调度器日志不再把“任务仍在队列但退避事实未保存”伪装成成功。
- TEST030使用正则命名、自动Drop的随机隔离数据库和8连接generation162完整Schema，配合线程安全内存Typesense执行同版本双Worker、完整五集合create/alias/state、v4毒creator、逐集合恢复、v3降级拒绝、队列外部删除503与retry trigger故障。BUG058租约、PERF037 100k/1M keyset/8MiB预算以及服务器百万行Typesense权威游标/SQL fallback作为同组组合证据。
- 无DDL、Schema、索引、generation、reset、回填、双alias或HTTP/DTO变化，保持162。滚动部署要求新文档shape对旧查询读取保持兼容；升级projection version的新实例负责重建，发现更高代次的旧实例自动停止写投影并继续由当前alias或PostgreSQL fallback服务。OPS011沿用High/P1、MAP008 Medium/P1、DEAD009 Low/P2、TEST030 High/P0；本机fake Typesense不被表述为真实集群网络分区或磁盘灾备演练。

## D-315：成长系统验收必须把并发奖励、授权来源和提交后缓存视为一个事务状态机

- BUG059已经把等级角色限制为`source=level_track`并保留manual来源，BUG060/ARCH014已经让损坏配置和未知货币在任何progress写前失败，调用者事务也已有单次rollback/commit测试；但这些局部证据无法证明两个真实事务同时越过target时只发一次奖励，也无法证明经验、货币、等级角色、permission version与缓存在投影中途失败后共同回滚并可重放。
- TEST031在正则命名、自动Drop的8连接随机隔离数据库中安装generation162完整Schema，禁用seed任务并建立三档等级线路、两个用户和同一role的manual绑定。两个独立`ProcessActivityBatch`事务同时贡献1点，数据库upsert/row update串行后只一个事务取得`rewarded_at is null`：最终progress2、rewarded1、experience25/level2、diamond3，经验与货币流水各一条；等级来源在相同role上新增独立level_track行，manual行不被覆盖，permission version只因派生行新增推进一次。
- 对已完成任务再次批量输入两个事件，progress按现行“活动投影计数”语义增至4，但rewarded、两类余额/流水、等级角色和permission version完全不变；这锁定“事件上游负责exactly-once，任务奖励自身至少once输入也最多发一次”的真实边界，不把任务表误写成活动去重表。每个成功batch在commit后清理短期permission-version cache；测试显式预热并验证失效。
- 第二用户用target1、experience100和diamond7任务注入`currency_transactions`拒绝trigger。失败发生在经验/level_track/余额已在事务内尝试之后，最终progress/rewarded/experience/balance/两类流水/level_track均为0或不存在，permission version保持基线且缓存仍在，证明没有调用提交后invalidator。移除trigger后重放同一事件，一次得到level3、diamond7、两类流水各1、manual+最高level来源、version+1并清缓存。
- 本项只新增集成测试，无生产Go、DDL、generation、HTTP/DTO、配置或前端变化，保持162；与既有严格condition/rewards/unknown currency、caller transaction、角色来源和HTTP读写矩阵组合验收。TEST031沿用High/P0，因为回归会永久丢奖励或改变授权；单数据库8连接证据不被表述为跨Region cache或活动Outbox集群灾备。

## D-316：基础设施组合门应复用已修协议，并为资源版本补上真正缺失的批量边界

- TEST032登记时压缩没有可压缩文本的质量协商矩阵，metrics会把数据库故障报成零，死信只能看最新100条，资源版本装饰则完全没有行为测试。当前BUG061、ARCH015、PERF038和TEST026已经分别提供严格gzip、持久指标失败关闭、150/1M死信分页及Outbox/JetStream并发恢复证据；TEST032不重复实现这些生产修复，而是把它们与仍缺失的资源装饰矩阵组合进同一门。
- `decorateResourceVersionRows`原用`map[publicId]item`去重查询；若上游批次因聚合或复用包含同一ID两次，只有第一项会获得versions，第二项虽初始化为空却永远不回填。映射改为`map[publicId][]item`：查询参数仍只含唯一非空ID，每条权威version按输入实例全部投影。空ID和未知ID继续得到非nil空数组，Query/Scan/terminal错误继续直接返回，不形成成功部分结果。
- 同一SQL还计算并扫描`has_manual_detail`，但detail URL判断只读取`has_detail || has_manual_page`，响应也从未输出前者。直接删除SELECT列、局部bool和Scan位置；真正权威的`hasDetail`、revision、文件、名称与URL字段保持原序义。未输出内部值没有兼容责任，不新增替代字段。
- 新测试在generation162单连接会话临时完整Schema建立active mod/resource/version/detail/localization，将两个相同ID、一个空ID和4,096个未知ID作为4,099项调用。query tracer精确为1，两个重复项各返回同一version/hasDetail/name，其他项为空；临时重命名binding表稳定返回数据库error且目标versions保持空。源码同时锁定生产文件零`has_manual_detail`。
- 基础设施广域再执行12分支gzip、durable metrics健康/缺列、死信筛选/百万计划/双类重放，以及queue断连、stale lease、双dispatcher和持久状态错误组。无DDL、generation、公开DTO或前端变化，保持162。TEST032沿用Medium/P1，DEAD010 Low/P2；本机临时PG/嵌入式队列不冒充生产NATS、OSS或数据库灾备演练。

## D-317：版本目录同步以共享数据库租约拥有一次发布，并把多来源作为结构化事实

- 进程内mutex只能保护一个二进制实例，不能约束多个Scheduler或管理员同步请求。同步现在先从pool独占一个连接并执行固定bigint的`pg_try_advisory_lock`；未取得者在任何HTTP请求和配置读取前返回typed in-progress，管理员接口映射409，Scheduler记录skip。取得者把连接和会话锁持有到远端解析、artifact构建与配置/artifact同事务发布结束；defer用不继承调用取消且有5秒上限的上下文解锁，解锁查询故障时销毁连接以依赖会话终止释放。
- 不增租约表或generation：版本同步本就以共享PostgreSQL作为配置和artifact权威，session advisory lock与进程崩溃自动释放比时间租约更少状态。边界是所有应用副本必须连接同一主库；跨数据库部署不由本锁互斥，也不作此类声明。并发管理员请求从偶发最后写入/502收紧为可重试409，成功协议不变。
- Scheduler从“启动后等到下一个北京时间04:00”改为启动立即尝试一次，再按原每日04:00运行。多副本同时启动只有租约winner外呼；loser不会排队重复抓取。启动同步失败仍保留旧持久目录，下一次管理员触发或日调度可重试。
- NeoForge一次同步同时使用modern和1.20.1 legacy metadata。`minecraftLoaderVersionResult`与持久LoaderSync状态改为最多8个去重URL的结构化数组，legacy `sourceUrl`继续等于首项；其他loader仍是一项。规范化同时兼容只有旧单URL的JSON；前端合并`sourceUrls`与`sourceUrl`去重逐项显示，因此新后端/旧前端和旧持久配置/新前端均可滚动兼容。
- TEST033以有效compile RED锁定三项缺口；最终在正则命名、force精确Drop的8连接generation162随机数据库，用两个独立pool证明持锁竞争的生产函数和Handler均409且远端0请求，释放后7个有界目录发布NeoForge三tuple/两来源，随后全上游故障保持配置与artifact序列完全不变。OPS012、TEST033沿用Medium/P1，MAP009 Low/P2；本机双pool和假来源不冒充跨Region网络分区或真实上游SLA演练。

## D-318：MRPack生成物以“任务锁判定 + 即时墓碑 + 超龄校准”完成跨存储提交

- OSS PUT和PostgreSQL事务无法共享原子提交。正确边界不是假设ready写入永不失败，而是在文件已经登记后把每个错误都带入补偿协议：`finalizeFavoriteExportArtifact`失败时开启新事务并首先锁定同一task；若`result_file_id`已经等于当前file，说明模糊commit其实已落库，必须保留；否则确认没有任何task引用后，复用统一墓碑和删除Outbox。原完成错误与补偿错误以`errors.Join`同时返回，不能用补偿成功掩盖业务失败，也不能只记录补偿失败。
- 即时补偿不能覆盖“进程在登记后、调用补偿前终止”或补偿数据库本身不可用。Worker每次pending扫描先执行有界校准：只读`source='favorite_modpack_export' and status='active'`、创建时间超过两倍lease且没有task引用的文件，按`created_at,id`取最多50行并`FOR UPDATE SKIP LOCKED`，同事务墓碑和enqueue。两倍lease从文件创建时起计时，避免误删仍在正常完成窗口内的生成物；已绑定文件无论多旧都不进入候选。
- generation163新增`favorite_modpack_export_tasks(result_file_id) where nonnull`唯一索引，既表达一个生成物只能有一个任务owner，也支撑反引用判断；新增`oss_files(created_at,id)`、限定favorite source/active的部分索引支撑恢复游标。开发期整代Schema同时删除从未被生产读写的`report_snapshot`：权威报告已经由task标量和逐项关系表组成，不为无消费者JSONB发明第二套Schema、双读或猜测回填。
- TEST034不能只新增一个OSS故障断言便冒充完整端到端。新矩阵使用随机隔离generation163数据库、真实鉴权中间件/Handler/Worker、可控OSS和数据库trigger，证明两项权限403/授予后成功、私密集合和他人任务隔离、ready/307下载、ready提交失败即时补偿、补偿持久化也失败后的周期恢复、重试只留一个active绑定文件、过期墓碑且历史仍保留。SEC024、BUG066-072、ARCH018、PERF040-041及ARCH021/BUG068的既有真实矩阵在同一广域门组合执行，不复制它们的生产修复。
- 公开HTTP请求、成功DTO、文件下载和历史协议不变；内部ready故障继续返回error并由lease重试，只改变文件最终可回收性。部署generation163需要按pre-production整代合同重建开发库；共享public generation155和未知远端未迁移、未reset。回滚会恢复不可发现孤儿和死JSONB，不提供兼容层。OPS013与TEST034沿用High/P0，DEAD011为确认无消费者的Low/P2；本机单PG/fake OSS不声称供应商Exactly Once或跨Region灾备。

## D-319：整合包导入必须在ZIP解析前限制中央目录，并用真实Worker终态组合验收

- TEST035登记时的身份、语言、正式文件选择和辅助请求缺口后来已由SEC025、BUG073/074和ARCH012逐项修复，但资源测试仍暴露一项真实实现缺陷：旧`readJSONFromArchive`先调用`zip.OpenReader`解析整份中央目录，再寻找受32MiB限制的目标JSON。攻击者即使把合法小索引放在第一项，也能用大量无关entry在业务限制前消耗内存；4097项有效ZIP在新测试下精确RED。
- 读取策略改为在`archive/zip`之前打开普通文件并只读取EOCD可能出现的最后65,557字节。EOCD必须正好终止于文件尾，禁止多盘；总项数最多4096，中央目录最多8MiB，offset/size必须落在文件内，完整归档继续最多1GiB。`zip.OpenReader`后再复验实际entry数，目标索引仍以header和LimitReader双重限制32MiB；Modrinth index与CurseForge manifest都在构造Mod payload前限制2000项。
- TEST035不是把已有纯函数排列成清单。随机命名、force精确Drop的8连接generation163数据库运行真实`runModMetadataImport`；本机TLS provider返回乱序old release/newer beta/new release及primary/secondary文件、官方和攻击域Mod下载、作者辅助响应、503、1GiB+1 Content-Length和2001项索引。成功Job必须为completed/100并选定new release主文件；三类失败必须为failed/25、带error与finished_at且无result。
- 临时根目录由测试显式绑定到独立目录并确认`os.TempDir`实际命中；成功下载、header早拒绝及下载后索引拒绝各次运行后都断言零`mcmods-modpack-import-*`目录。32MiB+1索引和4097中央项另以纯资源用例覆盖，不依赖真实公网或巨型磁盘fixture。
- 无DDL、generation、HTTP/DTO、前端或部署顺序变化，保持163。`defaultLocale=und`与`importSelection`继续是既有导入草稿事实，正式保存仍要求用户确认站内语言。TEST035沿用Medium/P1；本机TLS替身只证明协议与状态机，不被表述为真实Modrinth/CurseForge限流、证书基础设施或跨Region演练。

## D-320：评分写入以数据库触发器为唯一刷新边界，并只接受领域canonical类型

- `content_ratings`的BEFORE INSERT/UPDATE/DELETE trigger已经在评分事实同一事务调用可合并的`enqueue_content_stats_refresh`；Handler的显式第二次调用没有额外正确性，只把一次创建变为queue insert后立刻conflict update。删除路径本来就只靠trigger，说明数据库边界已经覆盖完整事实闭集。删除Handler调用后，trigger错误仍会让评分语句失败并回滚，不需要保留第二套错误文案或补偿。
- 原`INSERT ... ON CONFLICT DO UPDATE`在已有评分上会先触发BEFORE INSERT，再触发冲突分支BEFORE UPDATE，即使删掉Handler调用，常规编辑仍产生两次触发器enqueue。写法改为先按`route+author` UPDATE并returning；确实缺行才INSERT，仍保留ON CONFLICT更新作为并发首次创建的唯一约束兜底。这样普通创建、编辑和删除各对应一次事实触发，并让原先吞掉错误的“是否已有”预查询消失；活动create/edit分类由实际UPDATE结果决定。
- 评分类型注册表已经以`ratingDimensions`的九个下划线key同时决定维度和route解析。`server/minecraft-server/resource-pack/shader/shader-pack`是开发期别名，第一方`RatingTargetType`从未包含它们；直接删除switch，trim/lower只在canonical闭集内规范化。举报、草稿、活动等其他领域的`server/shader`是各自公开协议，不因同名字符串被越权清理。
- TEST040使用随机generation163数据库和生产鉴权中间件/Handler。队列表附加测试专用mutation trigger，旧代码创建精确出现1 insert+1 update；新代码创建1 insert、编辑/删除各1 update，并发两次编辑总2 mutation但仍仅一条queue。维度写trigger故障时评分更新、8维删除、queue和mutation计数全部回滚；刷新投影后rating_count从1到删除后的0。
- 同一矩阵还证明create/read权限403、待审目标及后续隐藏目标在summary/item/reviews均404、五别名真实HTTP404、成功summary含本人评分与能力、reviews可读；两个并发更新都200且最终只有一条评分、八个全同维度。PERF050既有百万行handler/索引门继续组合执行，不重复构造第二个规模实现。
- 无DDL、generation、公开成功DTO或前端文件变化，保持163。评分旧别名属于明确开发期删除，不提供双读或重定向；canonical客户端无需迁移。MAP010/LEGACY017沿用Low/P2，TEST040沿用Medium/P1；单机多连接PG不声称跨RegionWorker或缓存灾备。

## D-321：认证算法只接受当前生成格式，Session Token在原语层形成闭集门

- 所有当前注册、后台用户创建、验证码和Yggdrasil密码设置都只调用Argon2id生成器；审计基线与仓库均没有PBKDF2数据导入、登录成功重哈希或发布账号兼容责任。项目又明确允许重置开发库，因此保留PBKDF2不是兼容，而是无期限的活跃第二算法。
- `VerifyPassword`现在对非`argon2id$`立即返回false，PBKDF2 parser/PRF/迭代实现完整删除；`VerifyCode`继续共享同一唯一算法边界。测试使用一个真实100000轮PBKDF2-SHA256向量证明旧分支不是只被字符串搜索隐藏，而是行为上不可达。
- Token不更改格式或时间合同。新增同包测试通过私有签名边界构造错误alg/typ、错secret、签名篡改、错误段数、过期、未来iat、倒置寿命、非法公开ID、缺Session ID和auth version；另验证签发往返、两次独立256-bit Session ID、指纹及严格Bearer scheme。
- 无DDL、generation、数据回填、HTTP DTO或前端变化，保持163。若未来必须导入外部旧账号，应设计有审计、总量和移除日期的一次性显式迁移；不能恢复登录/Yggdrasil静默双验。LEGACY004沿用Low/P2，TEST006沿用Medium/P1。

## D-322：开发期 API 只保留 canonical 合同，测试按领域归位

- 项目自动化请求早已使用camelCase，但`querySimpleRows`把数据库列名直接泄露为snake_case，第一方组件因此复制了一套桥接类型。基础配置、来源、运行历史和管理员总览现在都在HTTP边界显式改名；组件只消费同一camelCase DTO，并从67行压缩表达整理为显式加载、保存、运行和渲染结构。后端/前端需同批部署，不保留双字段。
- 机器人规则的安全模型始终只读：写入SQL固定`read_only=true`，分类门也不允许机器人获得写能力。因此删除请求/UI的`readOnly`不是开放功能，而是移除伪配置；严格JSON解码会把旧字段作为未知输入400。真实PG测试证明旧请求零行、canonical请求只有一行且只读为true。
- 目录排序只保留`sort=<canonical field>&order=<asc|desc>`。后端和前端同时删除`latest/oldest/created/nameAsc/nameDesc`；缺失order时name一致默认asc，其他字段保持调用方fallback/后端desc。评论排序的`latest/oldest`属于独立公开领域协议，不因字符串同名被越权删除。
- `admin_dashboard_integration_test.go`只保留dashboard合同；sticker引用、收藏导出租约恢复和系统通知翻译拒绝分别归到对应领域文件，函数名和原断言不变。源码组织门同时要求旧文件不含三项且新文件实际拥有函数，避免一次性移动后回流。
- 无DDL、数据、generation、reset、依赖或模块图新变化，保持163。STYLE002沿用Low/P1，其余STYLE005/MAP002/LEGACY005沿用Low/P2；项目无已发布书签或外部API兼容责任证据，按开发期原则不增加期限不明的兼容层。

## D-323：开发期状态字典等于真实状态机，数值修订身份保留外键

- `project_auto_update_runs.failed`没有任何写入转换：失败在attempt上限前回到pending，到上限进入dead_letter。Schema和管理员筛选同时删除failed，保留pending/running/completed/dead_letter闭集；其他表各自有真实写入方的failed不因字符串同名误删。
- `recipe_version_bindings.source`只有导入器写import、编辑器写editor，且Schema文件明确不保留历史回填。删除backfill/split；未来若实现拆分必须连同事务、调用方和测试重新加入，不能预先声明伪能力。
- `project_update_events.revision_id`所有写入方均持有`int64`，因此改为可空bigint并外键到`content_revisions(id) on delete restrict`，写入和BUG039关联删除文本转换。0仍表达没有修订；不存在多态消费者或外部文本协议，故不建双列/双写。
- simple project六类型以一个有序slice登记并派生membership set；SQL顺序与校验集合不再各自手写。`DEAD-008`则按当前HEAD据实复核：后续增量热度重构已删除两个死评论者聚合，以回归门锁定零残留，不伪造第二次生产修复。
- 这些不兼容DDL把generation163提升到164，`DEVELOPMENT_SCHEMA_RESET.md`同步到当前代次。项目为pre-production整代Schema，旧开发库按既有安全确认重建；共享public155和未知远端均未reset、迁移或写入。MAP006沿用Medium/P1；MAP005、DEAD004/007/008按实际影响定为Low/P2。
- 真实临时Schema验证failed/backfill/split均`23514`，无效revision为`23503`，有效来源与bigint事件成功。验证时还发现清理器在删除最后临时关系后重复求值`pg_my_temp_schema()`会漏掉183个临时routine；清理现在先固定namespace OID，现有集成门同时断言关系和routine都为零，并已在确认无其他客户端后清除本次遗留。

## D-324：治理内部审计视图必须由独立DTO表达，状态与组件边界同时失败关闭

- `ban.view_internal`原本只选择与公开小黑屋相同的SQL/DTO；`internal bool`从未被读取，导致管理员拥有权限却看不到创建时写入的备注，也不能追溯执行者、解除者和解除原因。既有字段仍有明确治理用途，因此选择接通而非删除：公共列表/详情继续使用最小DTO和公开SQL，管理员列表使用独立DTO/SQL并显式连接moderator/revoker。路由层现有`ban.view_internal`保持唯一授权入口，不把私密字段加入公共结构或共享条件分支。
- 管理员DTO新增`internalNote/moderatorId/moderatorName/revokedById/revokedByName/revokeReason`并一并返回既有公开记录正文；公共wire合同、状态码和游标协议不变。完整临时generation164 Schema测试把私密备注和解除原因写入真实表，分别调用公开列表、公开详情和管理员列表，证明两个公开面既无字段也无字符串泄漏，而管理员事实精确可见。无DDL、回填或新权限，部署需前后端同批以显示新增管理员字段；旧前端忽略新字段仍不会造成公开泄漏。
- `"temporary" | "permanent" | "released" | string`在TypeScript中没有约束作用。现在共享`BlackroomStatus`闭集包含三种canonical值和显式`unknown`，公开列表/详情及管理员列表都在HTTP加载边界把任意wire值归一化；未知状态显示本地化诊断且不被当作合法三态。没有通过开放string保留假兼容，也不因未知值抛出整页错误。
- 原六领域治理组件虽被拆文件后行为已分离，但机械量化仍发现5个新文件中有多条240至2633字符压缩行，不能据此提前关闭STYLE008。固定Prettier 3.6.2只格式化五个新模块，最终最长行113/109/111/125/130；回归测试同时要求五个模块各自required/forbidden符号、旧巨型文件不存在及最长行不超过180。全部旧源码测试迁移到对应领域文件，举报、封禁、关于页、更新日志、爬虫和项目自动化的既有行为断言未删除。
- generation保持164且无依赖/锁文件变化；固定格式化器通过临时`pnpm dlx`执行，没有加入运行依赖。DEAD013按治理审计追溯缺失定为Medium/P1，STYLE007为Low/P2，STYLE008为Low/P1。真实PG、后端全仓Test/Vet/Build/tidy、前端272项/Type/Lint/58页正式Build均通过；公共数据库未迁移，临时关系、routine和测试库为零。

## D-325：可配置状态必须有所有者修改闭环，缓存版本不能与TTL形成双重权威

- `project_follows.notifications_enabled`不是可安全删除的死列：更新通知Worker和关注列表都读取它，产品也有“保留关注但关闭该项目通知”的明确语义。问题在于创建恒true、没有修改入口且重复PUT硬编码返回true。新增同一路径PATCH，只接受必有boolean且继续要求`project.follow`；SQL以当前claims user、route public ID和复合关系锁定所有权，未关注/他人统一404。隐藏项目的现有关系仍可修改，避免审核状态变化后用户无法停用通知。
- 关注PUT的冲突分支只更新时间，不重置既有偏好，并改为`returning notifications_enabled`；因此幂等重复关注不会把false静默变回true，响应也不再撒谎。账号关注面板新增受控复选框，提交后只采用服务器返回真值，失败保持原状态并显示已有本地化错误。严格解码拒绝缺字段、字符串boolean和未知字段，真实PG证明全部坏输入及另一用户均零副作用。
- 表情目录后来已由`ExpiringPromiseCache`获得30秒stale window、失败promise立即逐出和管理员成功mutation后的全locale显式invalidate；这套协议已经解决审计所述永久Promise缓存。与此同时API `version`从未参与key、比较、刷新或事件，专用singleton表和每次事务bump成为第二套无效权威。选择完整删除字段、类型、查询、bump及表，而不是再为已有限TTL制造ETag/轮询双层。
- 删除`sticker_catalog_state`使权威开发Schema从164提升到165；pre-production旧开发库按现有安全重置合同重建，不对共享public155或未知远端执行ALTER/reset。公开表情响应删除未消费的version，locale/packs不变；第一方已无读取，无双字段兼容。管理员mutation仍以原事务提交，前端成功后invalidate；跨实例/其他已打开页面最多30秒重取，不宣称push或零延迟一致性。
- DEAD003沿用Medium/P1；DEAD012按已打开页面陈旧窗口与协议维护风险复核为Medium/P1。真实generation165测试同时证明版本表不存在、公开响应无version、关注false/重复PUT/坏body/越权/隐藏后true/GET和数据库真值；后端全仓、Vet/Build/tidy及前端274项/Type/Lint/正式Build通过，临时对象和测试库清理为零。

## D-326：可配置目录决定展示顺序，中立注册表决定持久数值身份

- 项目文件原排序器把点号段逐个`Atoi`且吞掉错误，快照、预发布和自定义版本因此都可能被当作0；后台Minecraft配置及合成表已经拥有明确有序目录。文件页现在读取同一持久配置，并与合成表抽取复用同一排序函数：已知项按目录索引，未知供应商项置于末尾后按字典序稳定排序。配置读取失败返回500，不能以过期内建数值猜测伪装成功。
- 版本响应字段与分页不变，只改变`versions`筛选数组的排序事实。真实PG刻意保存`25w10a、1.20.6、1.21.1`这一非数值顺序，项目文件过滤精确照此返回，两个未知项稳定成为`unknown-a、unknown-z`；第一方版本选择器本来就保持配置顺序，无字段迁移。
- 活动action/object的smallint身份同时被Schema seed、破坏性保留清理和progression任务匹配消费。直接让database导入activity会与activity同包集成测试导入database形成测试依赖环，因此建立无数据库依赖的`internal/activitycatalog`作为唯一事实源；activity现有公开常量只是等值别名，调用方拿到definitions/maps的副本，不能修改注册表。
- Schema安装从11/27项definitions生成原有upsert，清理从IDs副本生成允许集合，任务验证/匹配使用同一lookup；progression两组switch和Handler两张数值map删除。真实generation165临时Schema按ID顺序比对全部code/name，并复跑活动清理审计、caller事务及TEST031奖励/角色状态机。DDL、seed值、查询形状、generation、公开API和前端均不变。
- REUSE003按仅影响下载筛选展示排序定为Low/P2；MAP003沿用Medium/P1，因为破坏性清理的ID漂移可错删活动且任务漂移会永久不进度。后端全仓Test/Vet/Build/tidy通过，模块哈希稳定；前端无代码变化并补跑相邻8项，公共数据库保持155且临时对象/测试库为零。

## D-327：管理读模型以真实失败、规模和前端请求身份组合关闭测试缺口

- TEST046审计时没有直接站务测试；当前工作树已由BUG102/103/104、ARCH029和PERF058修复逐步建立真实矩阵。完整临时Schema现覆盖公开locale fallback、管理员非法locale零副作用、各翻译独立draft/publish、About revision和Changelog timestamp CAS及冲突重试；改坏translation列会让公开页、管理列表和详情全部500，不返回未检查或部分行。百万changelog公开/管理深页各执行一条索引SQL。
- TEST047同理由BUG105/106/107、PERF059后续矩阵覆盖：七类当前生产来源在创建、父名称变化和server/simple重绑后都投影正确；类型端点包含8项目类型、兼容类型及动态/隐藏resource kind；百万行深keyset命中索引，选择性前缀使用表达式索引，宽前缀只做一次有界probe后拒绝，游标目标被并发删除仍可从空页返回上一页。
- 尚欠的失败/权限边界本次显式补齐：Server路由源码门锁定六个站务管理route及两个未解析引用route的精确permission wrapper；未解析catalog列和resource kind列故障均返回500且无items。前端锁定列表错误与类型错误独立alert，同时保留最后成功页；现有AbortController、request generation、重复搜索、空页恢复、来源跳转与诊断fallback门组合执行。
- 两组完整临时Schema最初被错误地并行执行，本机PostgreSQL锁表容量返回53200；这不是业务断言失败。确认无其他客户端后，精确清理四个已结束`pg_temp`命名空间各183个遗留routine，不触碰public或配置，并把两组改为串行复跑：站务38.023s、未解析41.596s全部通过，最终临时关系/routine为零。测试流程今后不并行安装两个完整Schema。
- 两项按治理内容正确性、后台可用性和规模回归影响定为Medium/P1；无生产代码、DDL、API或前端运行代码变化，generation165保持。后端全仓Test/Vet/Build及前端275项/Type/Lint通过；正式Build复用紧邻同一生产树已通过的门，公共数据库仍为155。

## D-328：项目更新通知失败预算独立持久化，真实Worker矩阵覆盖游标与恢复

- `attempt_count`的产品含义是失败重试预算。旧Worker在业务事务领取时增加，但任何收件人查询、模板渲染或通知插入失败都会回滚；独立`retry`既不增加又依据旧值判断阈值，还丢弃UPDATE错误。现在只有`retry`以一条`UPDATE ... RETURNING`原子增加，按增加后的次数决定pending/failed和5至300秒退避，第8次进入failed；completed/failed重复消息不再改变状态，无法持久化retry时返回并与原错误组合。
- 成功的200人批次不再消耗失败预算。否则关注者超过1,600人的健康任务已经积累8次attempt，之后一次瞬时错误就会直接永久失败。批次事务仍以`FOR UPDATE OF task`串行多实例，以`next_user_id`和每事件/收件人部分唯一键推进；成功只提交status、cursor、原子notified_count和通知本身。
- TEST018使用安全随机独立数据库而不是session临时Schema，因为生产Worker需要多个连接来验证锁竞争。Fixture包含201个有效跨语言关注者以及actor、通知开关关闭、全局项目更新偏好关闭、已取消关注和非active账户；两个Worker同时处理同一事件后恰好分成200+1两批，得到201个不同收件人、101条中文和100条英文，任务attempt仍为0，重复completed投递不改变通知或缓存未读。
- 瞬时故障通过临时重命名`users.preferred_ui_language`使真实收件人查询失败：业务事务回滚，handle独立持久attempt1/pending及错误；恢复列和next-at后两批完成且attempt保持1。另以6分钟前的processing任务验证scanner接管，以项目改为pending验证target unavailable完成且零通知；缓存已预热用户只随三个实际insert由0→3。永久错误1..8、terminal重放和拒绝retry UPDATE的CHECK注入由最小真实PG门单独证明。
- BUG036真实Handler继续负责隐藏后取消关注，BUG037负责内部section本地化，PERF025负责事件索引/增量计数计划；新矩阵组合复用但不重复关闭这些Finding。无生产DDL、API、前端或依赖变化，generation165保持；OPS004按持续内部错误造成容量耗尽定为Medium/P1，TEST018按核心通知多实例/漏发回归缺口定为High/P0。

## D-329：日志策略保存、显式清理和周期清理成为独立命令

- BUG052已建立启动即运行、每10分钟调度、连接级advisory lease、每表1000行、最多32轮/30秒的自动Worker，但旧PUT仍在保存设置后同步调用一轮`cleanupLogs`。这使“修改保留天数”和“立即删除数据”成为一个不可分命令，也让前端保存按钮及文案继续声明同步删除。本次删除PUT内清理；响应只带持久config，保存失败与删除失败不再混成“策略已保存但请求500”。
- 新增精确`POST /api/v1/admin/logs/cleanup`并继续要求`log.write`。命令读取已持久策略，只执行各表一个最多1000行的有界pass，健康返回config/deleted，逐表错误聚合为`LOG_CLEANUP_FAILED`及failedCategories/成功deleted；配置查询或JSON损坏在首个DELETE前以`LOG_RETENTION_CONFIG_READ_FAILED`失败。GET配置也改用同一严格loader，不再把数据库/JSON损坏伪装成默认策略200。
- 自动Worker与HTTP GET/POST共用一个`loadLogRetentionConfig`和一个`logCleanupStatements`表族适配；不同于用户活动的预览/确认/持久审计工作流，专用系统日志只需要受权管理员的有界立即命令和自动保留。这里复用的是严格配置、批次语句、租约和错误边界，不把不同数据治理语义强行合表。
- 第一方增加“立即清理”次级按钮，保存/清理在执行期互斥；保存成功只显示策略已保存，手动完成才展示删除总数。中英文说明明确启用策略每10分钟自动执行、按钮只运行一次有界pass，删除旧“保存会立即清理”文案。前后端按开发期协议同批部署，不保留无消费者`deleted`空map。
- TEST025按当前完整证据复核：BUG051真实SSE穿过生产中间件且Recorder透传Flusher/Hijacker/Pusher/ReaderFrom；AccessLog七项证明采样前置、有界队列、异步批量及数据库锁不延长请求；SEC020禁止query落库；BUG054统一slog/legacy；BUG052/053及本次门覆盖调度/批次/故障；PERF033以1.3M临时行证明keyset/search索引和精确1000删除。无生产DDL/依赖，generation165保持，两项按运维可观测/长事务风险定Medium/P1。

## D-330：热度刷新以持久终态控制容量，并由新领域事实显式恢复

- 旧两类队列只有`attempts/locked_at/available_at`，claim会无限增加attempt；永久坏函数或数据会永远回到可领取集合。两条retry UPDATE都丢弃数据库错误，Worker处理函数也不返回结果，因而“原错误”和“恢复状态没有保存”无法区分。generation166新增`pending/processing/failed`闭集，第8次失败精确进入failed并保留`last_error`，ready与stale分别用部分索引表达。
- claim先把超过5分钟且attempt未满8的processing租约作为候选，以`FOR UPDATE SKIP LOCKED`和单条UPDATE推进attempt；已到上限的陈旧processing在同次数据库命令中落入failed。成功删除、失败退避都要求`status='processing'`与领取attempt匹配，因此处理期间发生的新事件把行改回pending后，旧Worker不能删除或覆盖新事实。
- failed不是永久墓碑。评分、浏览事实、评论变化或时间衰减重新调用权威enqueue时，把状态改回pending、attempt重置0并清除锁/错误；普通pending/processing重入保留attempt，避免高频事件绕过失败预算。刷新/重试持久化错误由处理函数返回并与原错误`errors.Join`，scanner明确记录，不再伪装成已恢复。
- 周期衰减不再反复扫描所有近期项目和事件；启动阶段只补缺失统计，常规周期按`content_popularity_stats.next_decay_at`索引领取到期行。10M历史下100个到期任务入队约8.03ms，完整刷新函数约9.70ms；全局评分10M重建6.94s，1M搜索keyset约624ms，管理目录1M深页约0.5ms，证明状态机修复没有以全表退化换取可靠性。
- TEST028/029不是重复认领既有专项：它们把BUG055/057、MAP007/008/010、OPS011、SEC022、PERF034/050、TEST030/040和本次随机generation166双worker/毒任务门组合为评分—热度—搜索—管理读模型发布矩阵。公开API、DTO、pageKey注册表和前端均不变；开发库依既有安全条件整代重建，不能对共享public155或未知远端在线ALTER。

## D-331：发布质量必须由分离失败域的机器门持续强制

- 审计时两仓只有手工命令，没有CI、Race工具链或容器交付定义；后续SEC002虽加入依赖扫描，仍不能替代代码测试、数据库行为、Race与可部署构件。现在双仓push/PR均有主CI，官方Action固定完整commit SHA；依赖扫描继续独立定期运行，避免一个超长job混淆失败责任。
- 后端质量job执行tidy稳定性、全仓普通测试、27.0%覆盖率下限、Vet和Build；当前实际27.5%，较审计18.4%提升9.1个百分点。真实PostgreSQL以官方18.6 service、显式integration变量、`-p=1 -parallel=1`运行隔离状态机；Race在Ubuntu/GCC以CGO开启单独执行，不能被数据库锁或并发测试替代。
- 本机Windows最初确实没有C编译器。官方哈希验证Zig后已能链接，但其Windows TSan地址空间不适合作为结果；随后winget校验的标准WinLibs MinGW-w64 GCC16.1单包探针及全仓`go test -race ./... -count=1`实际通过。记录失败工具链是为了证明没有把构建失败当测试通过；持续权威仍是稳定Linux job。
- 前端CI用Node24.19和package-lock精确`npm ci`，执行自动发现全树测试、TypeScript、ESLint及正式Build。`next.config`生成standalone产物；双端多阶段Docker以非root运行。标签/手动release只向当前仓库GHCR写tag及`sha-<commit>`，前端缺任一公开origin即失败，运行秘密不作为build arg。
- 当前主机没有Docker daemon，因而本轮不声称实际拉取基础镜像；Dockerfile/工作流由双仓源码合同及actionlint验证，后端静态二进制和前端standalone构建则已等价本地完成。真正镜像构建是CI独立必过job，标签发布只在CI全流程可审查配置下触发，不自动触碰未知集群、域名或数据库。

## D-332：导入派生对象先成为数据库事实，再允许供应商写入

- 审计时PNG和语言包先并发PUT，随后各批独立插入active `oss_files`/媒体关系，最终任务事务再做激活、同步和源文件墓碑；任一后续SQL失败或进程退出都会留下数据库不可枚举对象或无引用active文件。generation167新增`catalog_import_job_artifacts`，以job/run/object/file唯一血缘登记planned派生物，并对job/file使用RESTRICT，删除主体不能静默抹去清理身份。
- 两类导入器现在按读取批次先锁定仍属当前run token的任务，在同一事务插入pending文件和planned血缘；只有提交成功才把字节交给OSS并发池。媒体/语言关系复用预登记file ID且保持pending不可读；全部PUT和业务写完成后，任务最终事务才同时把文件/血缘激活并清空job token。旧的“上传后insert/upsert active文件”路径删除。
- 失败defer会等待并取消上传池，再以后台超时事务按run token墓碑planned文件、写`oss_object_deletion_outbox`并删除staging revision；补偿错误通过`errors.Join`返回，不再吞掉。取消HTTP、显式重试和stale恢复分别在自身状态事务中完成同一补偿；Maintenance启动即运行且每10分钟扫描job非运行或token不匹配的planned尝试，为最初补偿写失败提供持久收敛入口。
- 正常派生物继续由原category/source/uploader、hash、size和MIME事实计入既有额度，差别只是从最终提交时才进入active计数。公开HTTP、DTO、任务Envelope、媒体读取和前端均不变；取消请求会多获得正确的原子存储补偿。开发期旧库必须按安全重置合同整代安装167，不在线ALTER、回填、双写或写共享public155。
- TEST008不再由可选真实ZIP和直接内部函数承担全部责任：最终夹具改用generation167随机独立数据库和多连接，通过HTTP创建iconrenderer任务，真实OutboxDispatcher沿现有有界本地路径调用生产Worker。成功3PUT后全部active；另一任务3PUT后注入最终激活CHECK失败，三文件墓碑/清理Outbox/staging0全部断言；实际删除Worker向替代OSS发出5DELETE，包含源文件、取消预登记对象和3个失败派生物。同组复用BUG019原子入队和SEC009来源隔离；不声称浏览器直传、公网OSS和多节点JetStream都由本用例覆盖。
- 2026-09-27收尾复核补上在途PUT与补偿的互斥：上传守卫以30秒截止时间持有artifact共享行锁直到供应商调用返回；补偿按job→artifact→file加锁，最终事务先锁job，孤儿选择也先锁job，避免取消提前提交删除事实或逆序锁。真实PG的100ms lock_timeout准确得到55P03，释放PUT后取消成功，取消后不再调用供应商。登记物理`cfg.Endpoint`而非显示/CDN endpoint，DELETE回归保证目的地可达。

## D-333：按用户要求在当前修复收尾后打包暂停

- 用户2026-09-27明确要求：完成当前进行的问题修复后打包整个前后端代码和进度，其余未修复问题暂不继续。本轮只完成OPS003/TEST008，不展开另外20个OPEN Finding。
- 使用实际工作树文件（包括未提交和未跟踪源码）制作双仓完整快照，不以旧HEAD归档替代；附原审计、修复台账、验证原始输出、元数据和逐文件SHA-256，排除私有环境文件、密钥、依赖/构建缓存和数据库数据。未提交、推送或部署。
- 429 CLOSED/20 OPEN是阶段台账状态；strict仍因未关闭与严重度未统一报告38项校验问题。原Goal暂停而非完成，最终报告保留未完成；以后仅在用户明确要求后恢复。

## D-334：OPS-020 独立维护预算与本轮交付边界

- 2026-09-30 仅收尾已经开始的 OPS-020：封禁到期优先；每个维护类别独立继承父 Context 的 30 秒截止时间，每类每轮最多 4 批，批大小仍为 1000；孤儿导入补偿每轮最多 4 个尝试。积压保留到下一轮，不以延长公共超时、忽略取消或关闭清理功能规避公平性。
- 真实 generation167 随机数据库先用蓝图表排他锁复现后续任务全部饿死，再验证封禁来源精确撤销、手工来源/未来授权/Session 保留、日志/草稿/Join/Presence TTL、4005 草稿单轮保留 5 条、多 Worker 恢复及父 Context 取消。旧蓝图源码合同从直接调用写法同步为注册到调度列表且执行独立 Context；真实数据库回归继续验证执行结果，不删除原断言覆盖。
- 用户明确要求收尾后停止其余修复并交付完整前后端和进度，另明确授权提交到指定 GitHub 仓库。仅推送现有 `codex/unified-catalogs-user-systems` 分支，不切换默认分支、不强推、不创建 PR、不部署；前端本轮无新代码，已有提交不制造空提交。
- 保留原审计和历史交接、全部 OPEN 项及严重度未统一记录。交付不是 449 项 Goal 完成或成熟度 A；本轮不开始 OPS-007 或其他剩余问题。

## D-335：交付后恢复完整 Goal；OPS-007 单一运行态恢复权威

- 交付与已授权的GitHub提交完成后，用户明确继续ACTIVE Goal。新修复只在本地推进；已交付ZIP/校验和、当时快照及原审计保持不变，不把之前的单次推送请求扩大为后续自动推送授权。
- 已建立连接的断连由nats.go自身reconnect和subscription replay处理，不并行创建第二重连路径。应用恢复循环仅处理初始/终态缺连接和失败注册，2秒起、失败指数退避至30秒；读最新cfg、候选准备、持久化和交换共用reconfigureMu，避免自动恢复覆盖管理员最新配置。失败候选及其错误不污染最后正确运行态。
- 官方nats.go v1.52.0的SUB权限错误是异步错误，连接可以保持connected，Flush的PONG也不返回该错误。真实权限拒绝RED得到nil；现在在Flush后检查连接错误，活跃连接的异步SUB拒绝使注册状态降级并重试，候选拒绝阻止persist和swap。普通发布拒绝或慢消费者错误不触发无意义的重订阅风暴。
- Close成为幂等终态；父取消关闭恢复生命周期，禁止再次Reconfigure/新注册/本地新任务执行。未改变已经开始的任务租约/幂等或可信任务Outbox主路径。ready和管理指标共用语义状态，区分local Hub与broadcast enabled/ready/recovering；NATS为可降级依赖、PG失败503规则不放宽。
- OPS-007原详细段没有逐项严重度，先前记UNRESOLVED。本轮依据其只影响可丢实时UI提示、权威业务事实仍在数据库且能重新同步、未越权或丢可靠任务的影响，判为Medium/P1。此判断是明确风险复核，不是声称原表已给Medium，也不是为匹配176/214/59降低等级；最终全表严重度证据统一仍未完成。
- 不顺带关闭TEST-023（前端消息中心/Hub广域行为）或TEST-027（Schema与HTTP配置边界）：当前增加的可靠性用例只验证其部分相关风险，不替代各自完整验收。真实单节点loopback/用户ACL不是生产集群/运维权限容量证明。

## D-336：用户要求收尾、暂停与当前代码 GitHub 交付

- 按用户当前指示，完成进行中的OPS-007后不开始其余18项，Goal暂停而不是完成；不能因GitHub推送、源码包或通过阶段测试宣称449项验收完成。
- 用户明确指定前后端GitHub仓库并要求提交当前代码；只推送现有`codex/unified-catalogs-user-systems`，不强推、不变更默认分支、不创建PR、不部署。后端提交OPS-007与本轮进度，前端无新改动，保持已有提交，不制造空提交。
- 新源码包含两仓当前完整源码、原审计、台账、实际验证日志和元数据；排除私有环境/密钥、Git历史、依赖/构建缓存和数据库数据。旧交付文件、哈希与失败证据不覆盖；最终本地/远程SHA及逐文件哈希写在新交付包，历史430/19快照不冒充当前431/18。

## D-337：再次恢复18项，不以阶段交付暂停目标

- 用户明确要求继续剩余18项并在全部修复前不暂停；Goal已ACTIVE，撤销上轮暂停意图。431 CLOSED只是起点，18项必须各自取得与原审计范围一致的行为证据；最终严重度统一、全项目质量门和复审仍不能省略。
- 从后端e3d66f4、前端a428c15的干净工作树继续本地修改；历史GitHub快照与源码包保持不变。本轮不自动推送，不因为阶段复验成功而把Goal设paused或complete。

## D-338：TEST-027 使用完整Schema和真实HTTP/JetStream验证边界

- 三个开启真实数据库集成后才执行的测试仍要求generation154，实际临时Schema已为167，因此在行为断言之前失败。有效RED原样保存；代次改与权威schemaGeneration比较，其余关系、公开ID/路由、partial FK前缀、唯一约束索引及清理断言不删除、不修改为接受错误业务行为。
- 新增完整随机数据库、官方JetStream/真实用户SUB权限、完整NewServer中间件和真实HTTP客户端，验证401/403、只读权限不能PUT、13项必填字段、加密与无秘密DTO、disabled/显式清密钥、真实不可达/无JetStream/SUB拒绝、数据库CHECK故障丢弃候选及并发保存的持久/运行权威一致。失败后原Hub订阅仍交付一次，成功换连接后也交付一次；候选Stream必须明确返回ErrStreamNotFound，不能用任意网络错误当成已清理。
- 首轮开启MCMODS_TEST_DATABASE_URL后，全仓老测试因共享public155与当前167不匹配或缺少集成开关失败；失败结果保留，不移除测试变量以跳过这些用例。后续全仓门只使用新创建、空对象核对过的test_remediation_027_<毫秒>随机本机数据库，执行受保护初始化与种子，启用全部DB集成，再按序执行包；只清理该精确拥有的数据库。共享public155不重置、不升级。
- 前端无源码变化，原npm权威package-lock不变；bundled Node环境没有npm.cmd，最初启动脚本失败不计代码失败或PASS。使用已有pnpm fallback运行相同package脚本，不创建pnpm锁；最终依赖扫描仍须使用npm权威锁实际执行。
- 完整隔离集成又复现贴图引用旧夹具的42P07：search_path把public放在pg_temp之前，索引绑定公共表，裸CREATE OR REPLACE也会暂改公共函数。现先建临时表，再显式使用实际pg_temp_N及pg_catalog，并限定索引、函数声明/调用与trigger安装均属本会话；公共Markdown列只读检查保留。定向真实PG已通过，锁竞争从任意Context错误收紧为PostgreSQL 55P03，查询迭代错误明确检查。
- 全仓开启DB集成并串行执行时，httpapi累计600秒触发Go默认包级alarm；当时运行的百万行死信用例才4秒，并非该查询超预算。后续仅将整包串行总预算设30分钟以容纳所有100k/1M/10M夹具，保留各用例Context、锁超时、基数和计划断言；不增加生产超时、不移除集成变量或跳过用例。最终结果必须来自新closing日志，旧失败不能覆写为绿。

### D-338补记：保留30分钟失败，按独立夹具分批，不再放宽超时

- closing全仓仍FAIL（2073.731s）：httpapi包累计30m时TEST044刚启动约1秒；另有固定2026-08-21草稿日期失效、导入作业夹具缺created_by、审核夹具缺缓存依赖、百万皮肤插入提前维护十个索引导致120s准备超时。全部原始失败日志保留，不能记成完整GREEN。
- 草稿日期改从真实PG now()派生，并新增过期草稿排除断言；导入夹具补当前作用域查询必需列；审核夹具创建真实禁用Redis的本地缓存；皮肤夹具先批量加载同样100万行，再建同样十个索引/ANALYZE并校验基数。单用例2分钟和查询2秒预算未变，最后真实计划通过。
- 新增Go串行批次工具：go list发现全部Test/Fuzz seed，每个精确名称只调用一次，每批最多30个，-count=1、-p=1、-parallel=1、默认10m alarm不变，失败继续收集且最终非零。无测试排除名单；条件SKIP原样可见，invoked不是passed。普通go test ./...仍单独必跑，显式数据库门另启全部DB集成和真实活动并发负载，不能只跑普通门规避已知失败。
- 工具要求APP_ENV=test、两个数据库URL完全一致、本机loopback且数据库名为test_前缀或_test后缀，不负责重置/删除。CI三个测试job各有独立PostgreSQL测试服务，并用既有受保护重置命令先初始化完整Schema/种子；初始化仅该步骤为development，其他步骤保持test，不降低27.0覆盖率或移除Race门。CI定义/本地等价命令不是GitHub实际运行成功证据。

## D-339：TEST-048 后端权威状态机与真实React浏览器互补验收

- 新增完整167随机DB、真实NewServer路由/鉴权/JWT/权限和真实HTTP请求：不可见目标、证据归属/扫描隔离、重复举报、双认领、接管权限、非法惩罚、报告与隐藏/封禁/outbox整笔回滚、一次处罚、旧JWT撤销、普通封禁仍可读但禁写、公开/内部备注边界、到期来源精确移除及清理与重开并发。手工同名角色、删除证据元数据均保留，不用空成功代替错误。
- 浏览器实际启动生产Next和Chrome，运行真实React后台面板；API故障/DTO使用明确mock，与后端真实PG证据分开标记。三条流程覆盖认领/解决冲突与重试、封禁错误保留输入与成功复位、非认领人/无接管权限/已删证据不可操作，并拒绝未预期API及页面异常。不能把mock浏览器流程称为真实浏览器连接PG端到端或生产验证。
- 只增加Playwright Library 1.62.1作为浏览器驱动，保留Node:test单一测试runner；npm是锁文件权威，未引入第二锁。CI在生产构建后安装锁定驱动的Chromium并跑同样浏览器脚本；本机实际用了已安装Chrome，未声称CI、Firefox/WebKit已运行。
- TEST-027与TEST-048仍须阶段全仓门完成后逐项关闭；不因定向绿或新增CI文件提前CLOSED，不自动推送、打包或暂停Goal。

## D-340：真实依赖扫描发现新公告，定向补丁修复并保留RED

- npm12.0.2实际完整扫描返回6项：Next Critical、Sharp/brace-expansion/Browserslist/js-yaml High、baseline-browser-mapping Moderate。旧2026-09-30及历史扫描不代表今天仍无漏洞；phase1-frontend-audit.log退出1原样保存。
- 维护者官方公告及npm registry核对后，将Next和匹配eslint-config-next16.2.11→16.3.8、Sharp0.35.3→0.35.4、js-yaml4.3.1→4.3.2；定向锁住brace两个兼容主版本1.1.21/5.0.12、Browserslist4.28.7、baseline2.11.0。保留React/TypeScript/其他无关依赖，不执行audit fix --force或全量升级。SEC002锁合同提高到新补丁边界，不接受旧漏洞版本。
- 官方证据：https://github.com/vercel/next.js/security/advisories/GHSA-p293-qw3h-jr36 （Windows风险）；https://github.com/vercel/next.js/security/advisories/GHSA-vcvr-r3jv-pc5j （next/og有条件风险）；https://github.com/lovell/sharp/security/advisories/GHSA-rgj7-g3m4-5g8c ；https://github.com/nodeca/js-yaml/security/advisories/GHSA-2883-xcg3-v3hh ；https://github.com/juliangruber/brace-expansion/security/advisories/GHSA-q2hr-2g5m-vwhr 。扫描严重度不等于已证实本项目每个入口可利用，不能伪称生产攻击复现。
- 本轮新增回归处置保留原449项，不删除/新增ID凑统计，不复写原审计。npm ci从同一权威锁实际安装后重新跑全部前端门、生产浏览器及完整audit，均绿；新ESLint出现一条既有comment-section导航警告，退出0且0 errors，不记成0 warnings。不部署或推送。

## D-341：TEST-023 完整React与受控边界，保留测试分层

- 先把TEST048后端/浏览器及阶段普通门事实写台账与日志，改FIXED_PENDING_VERIFICATION；显式DB批次仍运行，后端源码暂冻结并保存哈希，不提前关闭。
- TEST023使用实际生产Next/Chrome与原SiteShell、RealtimeBridge、MessagesCenter；EventSource和异步API明确为可控测试替身，验证用户看见的DOM、实际调用次数、abort/旧响应提交边界和连接关闭/恢复。不重新实现组件，也不把源码文本匹配当React行为测试。
- 后端真实Hub/官方NATS和完整HTTP SSE需单独核对/执行，受控浏览器替身不冒充真实broker连接。最终再一起验收该Finding，不顺带关闭TEST022私聊/Presence广域矩阵。

## D-342：TEST027与TEST048逐项验收及风险分级，不凑汇总

- phase1完整记录已结束：新空库初始化/普通全仓覆盖28.7%/Race/Vet/Build/tidy、显式全部1445名称/59批0失败、相邻完整DB Race两次488.040s、1030源码哈希一致与精确基库清理0；前端最终277单测/7浏览器/58页构建/类型/Lint/全依赖0漏洞。旧失败和六项条件SKIP原样保留；SKIP不属于这两项的验收证据，不将它们记PASS，也不声明全449最终质量门已完成。
- TEST027原详细段无等级，本轮定Medium/P1：缺口涉及重复索引、日桶、必填字段和失败重配的可靠性回归，真实权限/secret边界已测，但没有证明该测试缺口本身可直接扩大普通用户权限或持续泄露私有事实。不是从已有High降级，也不按汇总反推。
- TEST048原详细段无等级，本轮定High/P0：该缺口覆盖跨主体证据访问、无权限处罚/领取、Session撤销和内部备注隔离，失守可能造成错误封禁和敏感事实泄露，必须有完整鉴权/并发/事务/前端证据而非纯函数。真实矩阵及阶段门已匹配范围后独立CLOSED。
- 总计433 CLOSED、16未关闭；TEST023继续IN_PROGRESS，其余15 OPEN。严重度统一、其他业务与外部夹具、最终复审/成熟度A仍必须完成，Goal不暂停。不自动推送、不覆盖历史ZIP。

## D-343：TEST023组合验收与风险级别

- 真实Cookie会话网络SSE经过完整NewServer与实际PG167鉴权；本地与共享租约两模式测试单session2/user4，跨实例隔离与无回声、坏JSON不中断后续事件、实际Handler退出/共享lease释放后立即重用。官方NATS，Redis是miniredis Lua替身并明确披露；32容量慢消费者精确drop2和幂等退订也实测。现有真实broker初始故障/重启恢复与middleware透传一并Race双次，不能只用字符串合同。
- 生产Next16.3.8/Chrome的4React流程结合受控Promise/EventSource/clock，已证明原私聊错会话/余额死态/重复查询和连接生命周期受保护。后端实际网络与浏览器mock分层，不伪称一个公网端到端或生产容量实验。
- 原详细段无等级，定High/P1：会话竞态涉及私聊A正文出现在B及误发（BUG047已证实High），连接耗尽允许任意登录账户消耗FD/goroutine/代理连接（SEC018已证实High），不是仅视觉或日志缺口；仅测试改动，没有新暴露漏洞声明。定向与相关Race、普通全仓/静态门、全前端门已匹配原范围，独立CLOSED；TEST022私聊/Presence广域矩阵不合并关闭。
- 当前434 CLOSED、15 OPEN；最终严重度口径统一、外部夹具/最终门及成熟度A仍需完成，Goal ACTIVE。

## D-344：TEST007保留原规模证据并验证判定器能拒绝退化

- 先完成TEST023状态/证据登记再进入TEST007。旧TEST050已把最初“EXPLAIN非空”修正为真实100k三表/ANALYZE/目标索引/无SeqScan门，本轮不伪称原漏洞仍可重现，也不重新改生产查询。
- 补结构化EXPLAIN ANALYZE/BUFFERS JSON：期望输出行数、实际rows×loops+过滤重查扫描量、缓冲块与估算cost上限；墙钟时间只保存诊断。纯函数坏/好计划单测加真实事务SET LOCAL禁用索引的退化计划，负向必须明确拒绝而不是空计划或任意SQL错误。
- 夹具基数100k、单用例3分钟、已有目标索引及原文本断言不降低；无生产DDL/索引/查询或全局GUC变更，负向设置仅本会话事务并rollback。

### D-344验收补记

- 实际正/负三组均执行成功，三条退化JSON分别1819/1334/2128缓冲块被拒；13类坏测量及3非法/空JSON均拒绝。显式DB Race两次、最后普通全仓/Vet/Build/tidy通过，墙钟只作报告。TEST007按原Low独立关闭，435 CLOSED/14未关闭，最终完整批次仍需重跑最后源码。
- 登记本项完成证据后进入OPS008共享Logo；TEST024后续独立补邮件/设置/品牌行为矩阵，不因相邻共享存储改变自动关闭。

## D-345：OPS008共享品牌对象与安全派生设计

- 当前本机public/site-assets目录不存在；没有需要删除的用户本机Logo文件。旧本机写/读代码是审计已证实的非共享权威，移除该运行路径，不保留本地/OSS双读。旧配置的站点名保留，失效旧Logo不再作为读取权威，需通过新的上传重新绑定；不连接或迁移未知生产实例。
- 前端继续现有Sharp有界/去元数据/静态WebP管线，把结果转交授权后端；后端再次完整解码并按512边长预算重编码成无元数据静态PNG。公开资产改成稳定的OSS文件public ID路径，保留logoUrl DTO和先上传后保存的交互。不是仅信任魔数、客户端或前端处理结果，也不添加图片解码运行依赖。
- 复用oss_files、site.general与OSS删除Outbox：临时Logo一小时TTL；设置行锁串行绑定/替换/移除，旧对象墓碑与删除任务同事务；真实公共读取每次核对文件来源/active/扫描态，Next只代理共享访问且不缓存短签名重定向。site.general权威读取须跨实例立即一致，不能持有已删除旧Logo的局部十秒版本缓存。
- 原目录索引不足以保证临时对象清理在大量历史OSS文件下有界，计划新增只覆盖active临时Logo的created_at/id部分索引，开发Schema167→168；保留现有索引，不改共享155或任何未知库。所有当前代次合同只跟随新权威代次，其余约束断言完整保留；需要新空库/全Schema/计划与全仓门再次实际验收。
- 补真实HTTP/PG、测试OSS PUT/GET/DELETE、替换故障/并发/TTL/清理重启和跨实例读取；明确测试provider不等于公网OSS账号验证。新增端点尚不存在的首轮接受测试失败仅记架构接受RED，不伪称已在真实多节点生产重现。

### D-345实施补记：版本一致的浏览器门与当前代次合同

- 系统Chrome已自动到154.0.8037.59，而Playwright 1.62.1的browsers.json锁定Chromium151.0.7922.34/revision1234。Windows借用Chrome的整门多次发生不同页面load/点击超时，原五秒预算不变；采集实际请求时序和server fetch委托诊断，已排除服务端两秒Abort未触发这一假设，不能把7/7偶然重试绿当根因修复。
- 按项目当前output:standalone的安装版本官方文档改测试启动为实际server.js并复制构建静态产物；仅拦截明确外部API/provider，不拦截本机文档/RSC/静态资源。这两项边界修正本身没有消除Chrome故障，失败原样保留，不伪称因果。
- 在任务拥有的.codex-tmp/playwright-1.62.1下载官方匹配浏览器，与CI相同而不改用户Chrome/系统信任/安全软件。相同源码和五秒门限下两次7/7，21.662s/21.299s；实际全部前端门正重跑，仍须结果后关闭。可选轨迹/委托fetch仅属于拥有的测试子进程，不添加生产模拟路径。
- 首轮拥有168空库初始化成功但普通全仓发现一条读源码的代次合同仍查167（错误文字残留166）；同步精确168并保留全部分页/状态索引断言，未把等号放成范围或删除测试。所有当前代次合同实际为51个；独立新日志重跑全部门，不覆写失败记录。

## D-346：OPS008全量Schema累计报警与TEST024分层行为计划

- 普通全仓/Race/Vet/Build/tidy与完整前端全部通过；显式全部database包的Race count2在第一次累计到第600秒、正在执行NATS临时Schema安装时触发默认包报警，没有观察到单用例上下文/查询预算超限。原失败及精确库清理、源码一致保留。使用已存在且有独立发现/预算/数据库所有权测试的db_suite -race ./internal/database，完整发现所有名称、分30项、两次完整运行；仍每批10分钟、每个用例原有预算、p/parallel=1、无排除名单。不能把旧半截输出或新尚在跑结果写成PASS。
- OPS008实现已完成且匹配定向行为/计划/前端门绿，改FIXED_PENDING_VERIFICATION，直到完整显式Schema/维护Race与源码/清理返回才关闭。登记这些状态/失败后进入独立TEST024；不将另一条测试Finding自动合并关闭。
- TEST024先复核既有Enabled、加密回退和品牌metadata合同，再补真实SMTP owned loopback服务器的AUTH/STARTTLS要求与证书拒绝、错误/30秒IO超时、Enabled及Header禁入；配置完整HTTP权限/秘密脱敏/密文存储/runtime version/并发与Worker一致性，及生产Next实际上传、失败保留、save刷新/Logo删除、站点名与页面标题/失败回退。
- 后端当前源码冻结用于OPS008最终Schema门，期间不改Go/模块/CI、不增加Go文件；可并行进行独立前端行为测试与只读后端分析。Next/browser/server transport替身必须明确、受控在测试进程内，保留原生产代码/安全预算，不安装系统CA、修改用户Chrome、外部邮箱/真实OSS凭据或静默接触生产服务。

## D-347：保留HttpOnly主会话并修复Logo跨主机认证

- 正常GUI只有cookie-session状态标记，旧Logo调用把它当Bearer；Native Cookie接受测试有效403 RED。后端cookie是host-only且Path=/api/，只给同域BFF透传Cookie无法解决api/www分主机场景；不扩大cookie Domain、返回主JWT给JS或放宽Origin来掩盖。
- 同域BFF可显式转发唯一mcmods_session且要求Origin等于当前前端Origin；跨域GUI先用既有Cookie/CSRF权限请求后端发≤60秒的Logo-only opaque委托，再交BFF安全Sharp派生与现有唯一共享写入。委托复用AES-GCM实现，以JWT secret+固定purpose分域，claims完整加密；没有新增运行依赖/设置密钥/DDL，不把公开用户ID或session ID/JWT回传JS。
- SiteLogo scheme只在Logo权限探测/写入接收，不能被一般Bearer/JWT解析、一般配置/私聊或委托签发接收；到期不晚于parent JWT，动态权限、封禁写保护和已有会话事实仍是权威。它不是one-shot/重放防护票据，允许在短期限内重复尝试同类管理上传；不虚称TEST037的单次OSS授权验收。每次直接DB核对parent session，首次测试发现暖cache撤销延迟后修正；其余权限逻辑抽成同一解析 helper，正常auth行为不变。
- 全部Schema分批/哈希/精确库清理04:31:56完成后才改Go。OPS008仍待当前新增协议/当前全仓门，TEST024不因旧Logo已绿而自动关闭；后续浏览器必须实际执行现有组件/Sharp/Next元数据，服务端fetch目的地仅在测试子进程显式映射到受控loopback HTTP，不添加生产模拟路径。

## D-348：TEST024协议实测、设置失败关闭和测试夹具纠偏

- SMTP已使用真实拥有loopback TCP/STARTTLS/AUTH/DATA及30秒生产I/O deadline。可信证书成功门仅在新测试子进程按Go官方x509.SetFallbackRoots及GODEBUG=x509usefallbackroots=1使用临时根；不安装系统CA、不改生产TLS配置、不用InsecureSkipVerify。[官方文档](https://pkg.go.dev/crypto/x509#SetFallbackRoots)。成功、证书拒绝、缺TLS/AUTH能力、六类协议拒绝及Header/disabled禁入均实际通过。
- 真实配置Race RED证明updateMailConfig写入从不读取的s.mailer发生write/write race；读取实际已经只依赖DB，应移除死字段而不增互斥缓存权威。另补错密钥/坏品牌/实际DB不可用门，只有不存在设置行才允许显式环境fallback；存在但不可读不能静默重启已关闭邮件或伪造200默认配置。服务器/Worker将共用一个有错误结果的解密读取，并串行保存/保留省略密码。
- 初次配置夹具错误保留：wrong-key不足32字符导致尚未到真实故障断言；已换有效独立测试密钥。runtime version原写成每upsert+1与当前Schema statement trigger不符；PostgreSQL明确INSERT ON CONFLICT DO UPDATE会触发两个statement路径，因此验证精确+2，不改权威trigger/DDL/版本消费。[官方定义](https://www.postgresql.org/docs/18/trigger-definition.html)。
- 浏览器前两次定位失误与图片失败均保留，不伪称业务RED：真实General settings属于System分组，首页accessible name是Home；loopback跨端口不在实际CSP名单，改批准OSS origin后又证实Playwright只路由重定向初始请求，后续公网请求出现ORB。最终仅图片provider hop显式替身：route.fetch(maxRedirects:0)实际调用Next资产GET、强断言307/批准Location/no-store，再从拥有HTTP provider获取真实PNG交浏览器解码。不放宽CSP/证书、不再随重定向访问公网，也不称公网OSS全链E2E。原Sharp/Native route/Go真实307访问门独立保留。
- 当前品牌恢复document.title和新head title正确；Next Streaming Metadata另在body保留旧注入title，不是客户端第二权威。按安装版本官方metadata guide解释测试的元数据归属，避免通过全局禁用流式元数据或手动DOM删标题掩盖。完整前端当前7/9失败仍保留；其余单元/类型/Lint/生产Build/audit通过，相关Finding仍待当前阶段完整门。

## D-349：OPS008/TEST024独立关闭与当前门边界

- 完整Schema138名称分5批Race两次04:31:56已结束，全部发现/调用、0失败；当前mail/委托/Logo/维护组合Race两次103.270s和SMTP包Race两次68.971s通过。最后全仓Test/Race/Vet/Build/tidy、空库168、29.7%覆盖及冻结/精确删除拥有随机基库完成；前端六门280/9、58页构建、全扫描0漏洞通过。没有将普通go test中条件SKIP算作显式集成完成。
- govulncheck首次0.506s退出1并报no go.mod，工作目录实际有go.mod；本机PATH无go，扫描器依赖子进程go env判模块。补任务Go bin后同源码扫描5.271s退出0，0可达/导入包漏洞、4未调用模块公告；保留失败日志，不改Go模块/扫描器或更换数据库来制造成功。
- OPS008原详细段无逐项等级，按跨实例/重启/只读部署品牌可用性和孤立对象生命周期独立定Medium/P1；SEC019图像安全仍High。TEST024保护SMTP秘密/TLS、存在但不可读配置误启用和主会话/管理上传授权边界，按实际安全影响独立定High/P1。不是为凑原统计降低或任意抬高等级，最终449项严重度权威统一仍需复审。
- 两项逐项CLOSED后为437 CLOSED/12 OPEN，Goal ACTIVE且继续TEST013。未完成最终所有业务矩阵/全名称当前DB门、外部条件夹具/复审/成熟度A，不作整体完成声明，不自动推送或覆盖旧ZIP。

## D-350：TEST013真实审核和破坏性资料生命周期范围

- 复核现有mod_content_advancement_integration_test.go，pending分支直接SQL active并插placement，后续layout只依手工claims/handler。替换成完整Server/正常session HTTP、明确启用资料根板块审核的测试配置、只拥有本项目编辑权限的提交者和独立content.review审核者；不让no-review或直接SQL代替审核。保留布局成功、坐标/归属和无变化不会新建detail revision的原行为合同。
- 现有模板schema/section scope/global resource guard低层测试保留；新增完整HTTP+review事务验证有引用模板删除/字段删除/类型变更、disable兼容已有详情、无引用删除成功、不同版本/Mod不能串写/夺取全局资源。每次拒绝/数据库故障在拥有库内核对相关全部行的完整JSON事实，不只数一张表。
- 列表中途错误必须先证明真实PG已产出至少一个有效行、随后rows.Err非nil，再验证完整Handler500且无partial data。故障函数/view/sequence仅拥有随机库，不能靠模拟空行、跳过失败或改普通默认超时声称通过。复用已有随机库初始化/精确清理，不变更生产Schema/权限；本项仍IN_PROGRESS。

### D-350实施补记：有效业务RED与定向结果

- 实际HTTP接受了v1资源显式移动到v2板块并返回200 pending；纯函数还证明未知板块被接受、遇到后续不存在资源时已部分修改前面数据。修改布局增量合并：先验证全部显式目标/资源，之后才修改合并结果；仅对未显式移动且原分类被删除的资源保留原有回退行为。现有20k基数/一秒预算及未加载资源/删除分类合同均保留。
- 停用类型的已有详情可提交，但发布重复规范化把它当新选用而500。删除唯一重复校验和死wrapper，同一PG事实helper在HTTP及审核事务内判定仅原entry type+原模板+当前版本有效placement可保留停用类型；新建和切换到其他停用模板仍拒绝，不全局放宽disabled。无DDL、权限、并行缓存或另一发布权威。
- 定向当前Race通过，四顶层实际流程及原增量合并合同全部绿；真实PG32行后rows.Err故障与完整HTTP500/无partial data/恢复200已证明。所有夹具定位/编译/错误标签失败与有效业务RED分开保留。进入当前完整相关模块发现/双次Race、普通全仓/Race/静态/漏洞门与前端阶段验收，未完成前不关闭本项。

### D-350独立验收

- 07:02:54当前后端九门全0，sourceUnchanged/schemaFilesUnchanged=true，拥有随机基库精确清理0；真实67模块名称结合首轮66×2及同源码唯一跳过项补跑×2，逐名134实际PASS，首轮SKIP/发现helper误计失败保留。前端全部六门280/9及全依赖0漏洞已通过；普通全仓的条件集成SKIP仍不冒充显式DB/live完成。
- 原详细段无等级，按审核发布一致性、跨版本写入和破坏性资料删除可能造成权限/数据完整性失守，独立定High/P1，不按聚合差额凑等级。TEST013逐项CLOSED，438 CLOSED/11 OPEN；完整449项最终门、严重度统一和成熟度A仍未完成，Goal ACTIVE并进入TEST014，不自动推送或覆盖旧包。

## D-351：TEST014目录真实样本与完整调用边界

- 保留原SQL烟雾用例并明确命名，与真正六类完整HTTP目录矩阵分开。复用拥有完整Server/正常Session/随机库基础和实际独立审核权限，不手构claims或直接active替代待审创建；基础已审Mod只用于add-on合法父项目。
- 六类分别验证all/any版本、匿名/其他用户/审核者与提交者可见性、独立审核publication pointer、分类/关联、排除总数及非空连续页；另补10k深OFFSET、近1MiB×3locale正文详情保留但目录不带正文、作者关系正常审核后的ID/approved_by/approved_at稳定、十二并发相同名称自动slug分配。
- rows.Err必须实际PG先正常产行再错误，并验证完整关联加载Handler500且无partial数据；仅拥有库故障view/function，不改查询/用例预算。实际RED后才修生产并记录根因，完成相关Race及本阶段完整门后独立关闭；当前仍IN_PROGRESS。

### D-351实施与实际定向结果

- 实际十二同名并发创建7成功/5冲突，修复为事务型分配helper按project type+规范化base使用PG advisory xact lock，直到insert/revision事务提交才释放；实际只有此完整创建事务调用，签名明确pgx.Tx而非无事务query。区分site-ID已用sentinel与DB错误，后者立即传播/500，不循环1000次伪装冲突。没有进程锁、缓存或新表；不同名称/类型仍可并行。
- 合法作者关系审批500由实际rollback-only同句SQLSTATE42804确认：CASE unknown参数推成text，不能赋给bigint approved_by。生产句显式$3::bigint，不更改权限/角色或绕过关系审核；修复后真实审批和普通审核编辑/作者重排均保留ID/approval actor/time/created_at。
- 扩展双独立Server/独立连接池的同名并发全部201，各12业务/revision/request；CHECK注入在reservation之后使revision失败，19表完整事实不变，移除故障后同slug可立即重试，锁与持久关系没有残留。原权限拒绝也核对全事实不变。
- 原空库目录exclusion条件SKIP改成拥有完整Schema的明确3行样本，Mod/Modpack/六类简单项目八端点均验证两个非空且不同的页及total精确扣1，不改变过滤查询。关联SQL在97样本/owned session GUC下实际先产64有效行后rows.Err，完整HTTP500无partial data、恢复97/97；GUC/view不进入生产Schema。接下来49真实名称分20个独立用例累计批、每项两次Race、保持十分钟包报警和原leaf预算，再跑当前全仓与前端阶段门。

### D-351独立关闭

- 07:35:16结束当前11后端门全部0，49名称三批Race每项两次98 PASS/0skip；当前sourceUnchanged/schemaFilesUnchanged=true、精确拥有基库清理0。前端六门280/9/58页/全依赖0漏洞全部0，29.7%覆盖保留原27%门，govulncheck0可达/导入漏洞及4未调用模块公告。
- 原详细段无等级，按目录提交者隐私/资料发布一致性和合法并发创建失败影响独立定Medium/P1，不凑原聚合统计。TEST014逐项CLOSED，439 CLOSED/10未关闭；exclusion条件夹具盲区已取得自建数据实际PASS，其余live/exporter夹具仍须各自验收。Goal ACTIVE，进入TEST015；不能宣称449完成/成熟度A，不自动推送或覆盖历史包。

## D-352：TEST015真实网络与完整服务器状态范围

- 先保留已闭合的SEC014计数/预算、BUG030客户端证据边界、BUG031机器快照替换和PERF021/022游标/summary百万计划证据，不把旧缺陷再次虚称重现。新增owned loopback TCP真实Status/Configuration往返、混合公私DNS拒绝及同一验证IP贯穿两阶段的重绑定回归；网络传输只在测试把已核验的字面公网IP映射到自有TCP，不增加生产私网许可/测试配置开关。
- 复用完整Server/真实Session/拥有随机Schema，创建仍复核网络结果与证明文件，独立server.review审核者与两个项目scope编辑者。覆盖伪造source/confidence、完整机器来源/手工声明保留、权限/IDOR/附件scope、待审/拒绝/批准状态和持久故障不变；受控probe/OSS provider边界明确披露，不伪称公网Minecraft/OSS端到端。
- 调度成功领取/双实例不重领/样本持久/过期清理与中途故障均须实际调用；默认网络/leaf/包超时与解析数量/压缩/Fabric预算保持。私有transport参数如需提取只为同一生产路径可验证，无第二实现/全局hook；实际有效RED才修生产。完整矩阵/当前门前仍IN_PROGRESS。
- 实际TCP RED分别确认Status和Configuration已连接读取不响应父Context取消；两处用context.AfterFunc将连接deadline推进至当前时刻，Status同时取原8秒与父deadline的较早值。5秒dial/8秒status/30秒configuration/4槽及原解析预算保持，取消后及时释放槽，不通过放宽超时修复。网络恶意压缩/计数、同一IP贯穿两阶段和Fabric边界实测通过；64MiB压缩炸弹接收解码分配约9.52MiB（测试上界16MiB），恶意源构建在测量之外。
- 真实HTTP+PG RED确认review UPDATE在通知Outbox之前独立提交，Outbox CHECK拒绝后仍200并永久丢通知。改用已有enqueueTemplatedNotificationTx与review同一pgx.Tx原子提交，失败500/状态、发布、搜索及20业务表完整JSON不变、移除故障后重试；并发approve/reject仅一成功/一409/一通知意图。无新表、直接NATS发布或第二通知权威；签名附件测试仅验证OSS v4有限期URL与subject绑定，未请求公网OSS对象。
- 调度器仅提取私有probe依赖，默认仍serverprobe.Probe，同一SQL/领取事务/32并发/持久事务。48拥有库到期项双调用每项一次、1失败样本与47在线，待审/未到期不执行；完整快照删除机器来源、保留manual及清理unresolved；不完整保留旧机器事实。样本/证据持久失败原子回滚，但五分钟领取本来单独提交，因此保持interval lease；通过仅推进自有row due时间验证下一到期重试，不伪称等待了五分钟或立即撤销领取。90天实际历史删除/保留及完整HTTP历史已验证。

### D-352独立关闭

- 08:31:36当前test015-verified十五阶段全部0；100真实名称七批Race两次200 PASS/0skip；全仓普通测试、30.1%覆盖≥27%、全仓Race、Vet、Build、tidy和govulncheck通过。sourceUnchanged/schemaFilesUnchanged=true，拥有基库test_remediation_test015_1790814002883精确清理0。前端六门280/9/58页/全依赖0漏洞通过；原失败保留不算通过，普通条件SKIP不计显式集成验收。
- 按真实连接取消资源槽占用、证明/IDOR信任边界与审核通知持久一致性独立定High/P1，不迎合原聚合等级。TEST015 CLOSED，当前440 CLOSED/9未关闭。Goal ACTIVE并进入TEST016；最终449、严重度统一、外部夹具与成熟度A仍未完成。

## D-353：TEST016项目文件完整HTTP生命周期与提供者边界

- 保留BUG032/033/PERF023/REUSE003/DB005既有发布/游标/查询/身份证据，不将已闭合缺陷再虚称RED。新增随机完整Schema、真实Session/两用户与独立OSS审核权限，拥有loopback对象服务实际签名PUT/HEAD/GET，验证上传归属/项目scope、扫描发布generation/通知意图、软删除/计数及迟发事务故障。
- 提供者只使用拥有HTTP夹具与现有安全client；不发送真实凭据或放松官方origin凭据限制。Modrinth空/无效文件、错误身份、缓存origin、失败/大小/分页预算需真实确认；可能无限循环使用拥有子进程有界回收，不让首轮RED留下未终止CPU/HTTP。CF受凭据origin约束的完整API与直接adapter范围分别披露，不能伪称公网提供者成功。
- 测试支持全八类项目的真实扩展名、同scope双用户pending对象、发布撤销/再激活、无越权事实变化。只在有效RED后修改同一生产路径，不增加第二权威实现、测试runtime开关或提高leaf/数量/请求预算；完整阶段门完成前本项IN_PROGRESS。

### D-353实际实施（尚未完成阶段验收）

- 六类合法ZIP/MRPack完整HTTP presign被原JAR-only分支400拒绝已真实重现；上传与完成复用projectFileExtensionAllowed，私有scope采用canonical type而非文件系统plural，实际目录规范不变、去除无调用者的隐式mod fallback。八类十格式上传/挂载矩阵扩展中。
- 同项目第二授权上传者抢先complete另一用户pending对象原201已真实重现。复用原owners/<uid>路径规则为通用ossOwnerObjectCategory，presign/complete均绑定actor，项目category仍不含owner且DTO不变；拒绝在provider HEAD之前，全业务事实不变，原用户正常完成。没有保留双重scope/实现或给普通项目编辑权限新增上传权。
- Modrinth空/无URL/无哈希版本实际两次provider响应后CPU循环，在固定两秒拥有子进程预算内回收为RED。游标到版本尾时统一推进；同样覆盖files空以及已耗尽offset。原两MiB响应、10版本batch、5 batch/page、32 files/version、128 files/batch与10000 manifest预算保留。
- 缓存真实更换origin仍返回旧文件；v3 key按完整origin、精确case-sensitive identity与凭据身份哈希分隔，不暴露凭据、沿用同一querycache/TTL。修复缓存后再独立重现错project/unrequested/duplicate version，统一batch校验且list与hash下载均验证manifest归属/成员；CF真实modId=999/fileId=7被123项目接受亦重现，list/detail/bulk都校验所属modId。受控CF adapter保留官方origin凭据规则、synthetic key不发送；不伪称CF完整正常设置可用自定义带凭据origin。
- 合法DELETE原500，临时session SQL PREPARE同句确认$5不可推断；明确text cast并立即传播audit写错误，保留原原子soft-delete、更新通知和文件无public_route。三十秒同actor合并是既有权威政策，测试明确验证合并后的durable任务，不误报要求每次都新建事件。
- 下载项目counter CHECK拒绝后原200已重现。签名准备与写响应分离，同一安全URL策略；项目counter与OSS统计在一个TX，提交前不披露URL，任一失败全部回滚，普通OSS operational best-effort及失败指标保持。统计SQL统一到persistOSSDownloadStat。改造后的唯一空闲连接测试曾重现二次借连接死锁；改为事务前读取OSS配置、锁定元数据后无额外PG的同一签名helper，固定两秒用例现通过，不增pool/时间预算。
- 实际十六并发download Race确认defaultOSSConfig切片复用global backing array、JSON解密同时写入，改为每次独立复制，不用全局互斥绕过Race。并发计数精确17/17、项目迟发统计故障、待审项目匿名/异主404且上传者可内部读取和无任何公开更新均通过。
- 真实HTTP创建在OSS row lock等待期间项目转pending后仍沿用旧approved并发durable通知，已重现并改为创建/删除/下载事务内target FOR SHARE重验。扫描相同parent状态竞争与完整cursor/十格式挂载扩展进行中；本项不提前关闭。无DDL，故障CHECK及session临时编译表仅拥有loopback实例，不动共享155/远端。

- 后续扫描竞争同样取得独立RED：扫描请求等待文件锁期间父项目转pending，仍按旧快照生成公开通知。扫描事务改为读取当前parent并持有FOR SHARE至提交；事务内同目标复用已锁定结果，不增加全局缓存。三页完整HTTP keyset、跨项目/limit游标拒绝及CF完整API缺凭据/非官方origin失败关闭已实际通过。
- 十格式矩阵扩展到真实文件元数据挂载后，Plugin的合法Paper loader被通用Mod规范化丢弃而400，取得独立RED。Mod/Modpack继续既有Mod规则；简单项目复用现有normalizedSimpleProjectOptions/validSimpleProjectOptions，Paper合法、NeoForge不能冒充Plugin。没有第二taxonomy、额外放宽或新DTO。当前源码已冻结进入完整验收，尚不计CLOSED。

### D-353独立关闭

- `test016-verified-backend-results.json`10:10:54十五阶段全0，114真实名称七批Race双次228 PASS/0skip；覆盖30.1%≥27%，全仓Test/Race/Vet/Build/tidy/govulncheck通过，零可达/导入漏洞、四未调用公告。sourceUnchanged/schemaFilesUnchanged=true，拥有基库test_remediation_test016_1790819788635清理0。前端09:22六门280/9/58页/全依赖零漏洞通过，无前端源码变化。
- 首轮全仓Logo恰好一次失败保留，慢provider证明实际两秒重试可正确重领而非丢记录；夹具改用真实领取两项活租约、真实SDK失败和同一recordFailure，精确一次断言及新Worker恢复均保留，生产重试不变。按匿名空版本CPU资源耗尽、pending对象归属、发布通知和计数持久边界独立High/P1；TEST016 CLOSED，441 CLOSED/8未关闭，本批10/18，Goal ACTIVE进入TEST017。

## D-354：TEST017更新日志完整会话审核与持久生命周期

- 保留BUG022/033/034/PERF024既有权限、MC权威、approved-origin和百万计划回归，不虚称旧缺陷RED。以真实同步worker事务生成managed release绑定，手工编辑/阅读/按项目范围审核走完整网络HTTP和真实Session/RBAC；明确不声称公网provider fetch或通知实际消费送达。
- 验证pending/rejected/approved对人工override和上游同步的不同作用，shared pending-review guard保持、不因误猜合同放宽。包括审核队列/历史scope和自审拒绝、九类目标、分类归属、迟发publication/review通知故障及多语言大正文keyset。所有故障只在随机拥有168库，原预算保持；完整阶段门前IN_PROGRESS。

- 有效HTTP RED确认跨目标category可201待审且之后审核失败；提交/发布复用唯一projectChangelogCategoryID并在事务按target过滤/FOR SHARE，无效400、DB错误500。同一category在正常review创建后可再正常POST复用，不只是拒绝所有引用。
- approve/reject通知故障均原200/审核已提交，现用既有可靠通知TX helper与审核、manual override、分类/body、published事件和project.updated原子提交；移除commit后独立通知。正常独立并发approve/reject仅一200/一409、一个实际review事件和一个通知意图，published更新/override/body与胜出状态一致。
- history把任意DB错误当404已实测，改为只有ErrNoRows返回404，其余500；拥有表故障精确恢复且全部业务事实不变。大正文首轮错误使用ko-KR属于夹具错误，采用现有八种人工语言权威，不扩展支持范围或增加原请求/正文/分页预算。九类型完整发布、pending隐私/按scope审核队列和历史、所有已测事务失败和重试均通过，完整阶段验收进行中。

### D-354独立关闭

- 11:04:39的test017-verified报告十六阶段全部0，104编译名称八批各两次、208 PASS/0skip。普通136.605s、覆盖1.545s/30.1%≥27%、全仓Race155.908s、Vet6.940s、Build7.539s、tidy0.418s、govulncheck6.007s/零可达与导入漏洞、四未调用公告；完整168 Schema哈希和全部冻结源码不变，拥有基库test_remediation_test017_1790822315042清理0。结合10:22当前前端六门280/9/58页及全依赖零漏洞，通过本项独立阶段验收。
- 原600秒累计alarm保留；100k与百万各自独立批、每一名称仍两次且原leaf/包超时不变，不排除慢用例。按真实缺陷独立Medium/P1：授权提议的分类错误、审核通知分裂和故障伪404主要是可恢复一致性/可用性；正常审核scope/自审/隐私矩阵未重现新的越权，不能为凑严重度总数升降级。TEST017 CLOSED，442 CLOSED/6 OPEN/1 IN_PROGRESS(TEST022)，本批11/18、Goal ACTIVE。

## D-355：TEST022私聊、在线隐私与准入的完整HTTP事实门

- 正常非admin Session、完整168随机拥有库和完整NewServer网络请求，双实例覆盖会话成员IDOR、收件权限/双向拉黑、32并发创建与64并发发送、严格cursor/after归属、最大4000 Unicode字符、160字符邮件预览、未读事实/明确校准、隐藏在线与读回执，以及邮件Outbox/已读写故障全31表回滚。共享Redis使用明确Lua协议替身，不声称生产吞吐/云端SMTP送达。
- 心跳验证恶意visitorId不能改变服务端身份或跨实例来源额度、UA变更不能续额度；实际有界Lua状态种入50100后执行原trim至50000，16并发本地8192身份仍4096并验证五分钟过期。150秒用户TTL、30秒会话TTL、60共享/12本地每五分钟准入和十分钟PG快照不放宽；多Session正常退出及Redis中断同副本回退真实验证。
- 第一轮非法/超大JSON原200及耗尽来源仍读取1048592字节取得有效RED。改为先以服务端来源准入，再严格JSON解码，任一失败不Touch在线/快照；不新增热路径PG限流或改变身份规则。两个初始夹具失败不算生产RED：VOLATILE view导致前置ID查询也出错，改STABLE后先证明ID查询成功再独立重现成员错误伪403；miniredis TTL不自动推进，真实等待30秒后再按实际已过时间推进替身TTL，不能用FastForward缩短本地时钟。
- 成员helper返回真实PG错误，GET/Presence数据库故障500，非成员仍403且无业务事实变化。完整阶段门和严重度复核尚未完成，TEST022保持IN_PROGRESS，不计关闭。
- 下一次有效RED确认POST发送成员读取错误仍伪404；发送现只ErrNoRows为404，其余500，新增未知会话GET/POST/Presence均404回归。此故障发生在业务事务前，错误不创建消息/通知或更新未读；完整十二顶层Race双次及当前前端六门正在执行。
- 首轮全门11:34:47已通过但最终资源复查仍复现本地聊天/用户Session缓存无界及无人读取的过期聊天记录滞留，故不提前关闭。只收紧本地派生回退为已有4096总记录预算并清过期、淘汰最早状态，不限制真实登录Session数或共享Redis权威集合；保留旧报告，新增修复重新完整验收。

### D-355独立关闭

- 12:06:09新源码完整后端16阶段全0、81真实名称162 Race PASS/0skip、30.2%≥27%；12:04:39新前端六门280/9/58页及audit0。零可达与导入漏洞、四未调用公告，源码与完整168 Schema哈希保持，拥有基库test_remediation_test022_1790826566807精确清理0。首轮156 PASS的报告不覆盖，也不冒称包含新增缓存用例。
- 独立High/P1理由：已实测匿名来源耗尽后仍可反复驱动大body解码/分配，准入未控制该热路径；本地用户Session/聊天状态无界且聊天过期记录无人查询时滞留，形成额外进程资源风险。正常权限/IDOR/隐私矩阵未发现新越权，不将缓存协议替身当实际生产Redis容量。等级不为凑176/214/59升降；TEST022 CLOSED，443 CLOSED、本批12/18，其余六项及最终449门继续，Goal ACTIVE。

## D-356：TEST036蓝图正常会话与实际上传任务故障门

- 只在完整168随机拥有库和本机真实HTTP OSS替身，按原32MiB源/解压、64MiB规范、250000块/8192材料/2M体积、2worker/4user/16global与原租约预算验证。不通过提高源读上限掩盖规范文件32–64MiB读写不一致候选。
- 三源格式实际presign/PUT/complete、正常独立审核/私有与公开读取、转换/下载字节，恶意palette/几何及实体数据保留/有损拒绝、通知持久故障、管理员既有封面、公开并发配额、租约恢复与真实孤儿删除共十一顶层首轮执行。尚未取得业务RED前不修改生产代码或宣称缺陷已修复；NATS durable意图/OSS替身边界明确。
- 首轮201/200属于夹具合同错误；纠正后的真实queued匿名公开、Vanilla静默丢块/空name和完成通知遗失取得RED。只对已有证据先修复：新上传pending而不是schema默认not_required；Vanilla/Litematic严格name/index并拒绝截断packed状态，Sponge补空state。Sponge未知索引初版夹具改错v3层级，纠正后证明原实现已拒绝，不伪记漏洞。审核route/public revision ID及本机OSS bucket/key表示均按实际合同修正后继续验证其它候选。
- 后续有效RED为合法35,365,139B规范结果旧源32MiB读拒绝、approve/reject通知失败仍200和终止状态先failed/无通知。源读32MiB与规范读64MiB按原角色显式分离；所有成功/终止/耗尽租约通知统一TX并传播读取/入队错误，删独立后发路径；审核沿用既有TX通知helper。十三顶层正常首轮全PASS，实体和4/16公共预算/补偿事实不改变。
- 旧Worker实际normalize入口新增事实断言有效复现返回LeaseLost却先改主体；在首次processing UPDATE同SQL核验/锁定当前job/attempt/runToken，旧Owner零匹配且事实不变，完整恢复用例两次Race PASS。只在拥有库注入租约过期，不降低原两分钟租约或增加测试超时；最终完整模块和阶段门验收中。

### D-356独立关闭

- 13:05:08新完整后端22阶段全0，126编译名称逐个Race两次、252 PASS/0skip，原27%覆盖门取得30.2%；普通152.530s、Race163.820s、Vet7.006s、Build4.216s、tidy0.447s、govulncheck5.061s，零可达与导入漏洞、四未调用公告。12:50:29前端六门全0、280单测/9浏览器/58页及audit0。源码与168 Schema哈希保持，拥有基库test_remediation_test036_1790830136388清理0。
- BUG079旧源码形状断言因新增pending列而失败，仅更新为同时明确upload_expires_at/review_status及pending值，原清理检查保留；新源码重新全验收，首轮失败报告不覆盖。独立High/P1：已实证新queued匿名公开私有metadata、非法palette静默丢失有效数据、成功/审核/终止通知非原子以及过期Worker先改主体，影响权限隔离、数据完整性和可靠恢复，不按目标汇总凑等级。
- TEST036 CLOSED，444 CLOSED，本批13/18；另外五项及最终449门/严重度证据统一/复审继续，Goal ACTIVE。本机OSS协议替身不冒称云验真，PG durable通知意图不冒称消费送达，无DDL/新运行依赖/前端字段变化。

## D-357：TEST037真实OSS HTTP与外部副作用恢复边界

- 先在仓库外准备真实Session、完整拥有168库、签名校验/分片/复制/删除协议替身，不在TEST036源冻结期间编辑Go。授权关注跨用户申请或绑定，而非禁止持有合法限时bearer URL；独立HMAC核验对象/用途/期限篡改，并真实等待原60秒TTL，不缩短TTL或延长90秒fixture预算。
- 并发额度使用真实权限和HTTP准入/结算；举报附件真实PUT/登记/绑定，CHECK故障核对全持久事实和恢复；分片通过真实part PUT、生产cleanup worker和过期lease，新Key生命周期测试覆盖旧Worker实际DELETE而非只完成CAS。迁址和完整GIF预算继续准备。草稿和预期不是有效RED，未实证前不改生产实现，不关闭本项。
- 八实际编译名称首轮67.837s/1：真实user ticket.category回传导致普通完成400，核对前端真实helper同合同后认定RED；generic分类只解包自身精确单scope，既有项目源权限和Key前缀验证保留。真实附件binding CHECK误400分开为DB500、无效归属/数量400，事务边界不变。实际迁址与完整GIF矩阵首轮PASS；上游中断的签名到期/分片/迟到删除尚未验收，不预记缺陷或关闭。
- 目录/举报修复后真实60秒到期、独立签名及multipart/generation/完整GIF已PASS；真正新RED为失去lease的删除Worker仍执行外部DELETE破坏active同Key再生对象。生产DELETE前持有当前job/token FOR SHARE直到provider返回，unregistered guard用同TX避免单连接嵌套；既有ARCH020夹具改真实claim。新增受控真实DELETE阻塞后另worker不可接管用例Race两次通过。quota夹具时间CHECK纠正后实际32并发准入/两并发结算与stale物理DELETE全Race两次通过，完整模块/全仓/前端门和哈希/清理验收中，不提前关闭。
- `test037-final-backend-results.json`13:50:55完整31阶段全0，223编译名称446 PASS/0skip（23批，各两次Race）；完整普通141.703s、覆盖1.537s/30.2%、全仓Race150.270s、Vet7.197s、Build4.182s、tidy0.424s、govulncheck5.545s/零可达及导入漏洞、四未调用模块公告。源码/完整168 Schema哈希保持、拥有test_remediation_test037_1790832298775清理0；13:27当前前端六门全0。独立High/P1是迟到物理DELETE损毁再生active对象与普通user完成合同断裂，未证明P0；本机合成签名不称云验证，durable意图不称消费送达。TEST037 CLOSED、445 CLOSED，本批14/18，另外四项与最终449验收/等级统一/复审继续，Goal ACTIVE。

## D-358：TEST038评论完整HTTP权限、并发与故障矩阵

- 在TEST037 Go源码冻结期间只保存仓库外草稿，31门和源/Schema哈希/拥有库清理成功后才落地九顶层测试。原目标私密化后子路由、并发2000插眼、16编辑CAS、删除附件/日志任务、深分页/真实行错误范围全部保留；另按相同生命周期检验跨目标幂等重放与可靠通知事务。
- 复用正常DB Session/RBAC、完整随机168库和真实OSS PUT/GET协议替身；55表全行事实不只比较数量。大型树fixture明确调用生产insertCommentTree，不冒称每条通过创建HTTP；读页和权限操作实际完整HTTP。原90秒fixture、10000字符、2000插眼、64节点/16祖先/512KiB、lease/重试预算不放宽。编译、实际首轮之前不改生产、不预记RED或关闭，Goal ACTIVE。
- 九名称已实际编译，首轮普通159.923s/1，隐藏子路由/并发编辑/深大树/根keyset四顶层PASS；跨目标幂等回原目标、两类通知CHECK仍201并持久提交、真实PG装配故障伪404均有效RED。2031批量seed在90秒截止前未进入HTTP，显式合法public_id诊断仍保留所有触发器；裸token=未承诺通配脱敏，改已承诺access_token=正样本。分开记夹具与有效RED，不删测试/不提高预算，阶段仍IN_PROGRESS。
- 补充有效RED确认原目标pending且自身访问404仍可在其他可见route重放正文。作用域现在在原advisory TX核验type/id/version，不改全局作者+key唯一权威；两类通知用既有enqueueNotificationTaskTx与评论/日志/计数共TX，删除post-commit watcher helper，无双发送路径，block读错误同样失败。附件查询只NoRows/实际不可见404，其余500。默认JIT/所有trigger保留，以合法可见已删除历史1999条作为现有插眼基线，32候选仍published，不以fixture修正冒称原2031同作者published批量性能达标。
- 修复后十顶层普通84.734s/0，新增真实私密父正文和静音/拉黑/两祖先去重两名称Race count2共4 PASS/0skip/22.437s。十一顶层加全部相邻模块/全仓/前端六门及源/168 Schema冻结/清理继续验收，独立High/P1依据目标撤权旁路与可靠意图永久丢失，不定无证据P0，不提前关闭。
- 最终test038-final-backend-results.json于14:31:39完成，19阶段全部0：初始化6.635s，11模块批104编译名称208 PASS/0skip，普通133.579s、覆盖1.724s/30.2%、全仓Race153.263s、Vet7.883s、Build4.890s、tidy0.508s、govulncheck5.220s（零可达/导入漏洞、四未调用模块公告）。sourceUnchanged/schemaFilesUnchanged=true，拥有test_remediation_test038_1790835337120清理0。前端14:16:52六门全0/280单测/9浏览器/58页/audit0。TEST038独立High/P1 CLOSED，446 CLOSED、本批15/18；TEST039、TEST042、STYLE003及最终门继续，Goal ACTIVE。

## D-359：TEST039皮肤完整HTTP、共享存储和恢复矩阵

- TEST038源码冻结结束、全部19后端门及源/Schema哈希/精确清理成功后，才用apply_patch落地八顶层实际Session/HTTP测试。沿用完整拥有168库、实际OSS PUT/GET/DELETE协议替身与正常RBAC；66表全行事实。受控scan clean事实不冒称真实杀毒、合成provider不冒称云端。
- 保持现有公开content-addressed launcher bearer合同及SEC034资产/档案元数据组合授权，不以共享Blob反向伪造asset ACL、不承诺撤回曾公开/缓存hash字节；最后引用tombstone应停止新请求读取，是BUG088既有合同。先验证真实审核通知CHECK、衍生PUT后回滚/持久补偿、两请求额度并发与原每日100次，原4处理槽/90秒等预算不放宽；未实际执行前不预记RED或修复，TEST039 IN_PROGRESS，Goal ACTIVE。
- 编译首轮仅误用既有worker构造/void drain，纠正夹具后八名称实际编译0/pkg0.352s。首轮normal1/pkg48.976s：私有档案/管理员/真实PUT补偿/库和档案并发额度/日额五顶层PASS；有效RED是approve与reject通知CHECK仍200并持久状态、最后引用file deleted而hash新请求仍200。原失败保留，不把编译错误算生产RED。
- 修复仅既有证据：skin审核使用既有enqueueTemplatedNotificationTx与snapshot、resolution同TX，原template/values/data/recipient及正常DTO不变，删除独立post-commit发送；纹理hash查询join oss_files active，先检查lineage再处理304，保留正常launcher URL/immutable与shared私有资产不封锁另一个公开引用。
- 修复后normal1/pkg54.515s原八顶层全部PASS；新增并发审核的catalog事实列误写skin_asset_id，在真实Schema发现后只改为既有asset_id，不改Schema/业务断言。追加actual PG500/恢复和生产maintenance+真实DELETE、十顶层重新验证。独立Medium/P1依据不可恢复审核通知丢失和逻辑删除读窗口，不存在已证实凭据泄漏/权限提升/再生数据破坏，未为汇总凑级，仍IN_PROGRESS。
- 十顶层第一次normal1/pkg66.258s的九PASS包含实际并发review与production GC/DELETE；最后错误状态测试以通用500错误猜测launcher协议，核对既有writeYggdrasilInternalError明确503后只修fixture为503。原production error函数/预算未变，错误仍可观测且条件请求不得伪304；新完整矩阵实际重跑后才能开始冻结验收。
- `test039-all-ten-launcher-error-contract.log`normal0/pkg69.509s十顶层全PASS/0skip，开始完整源/168 Schema冻结与test039-final及当前前端六门；不在冻结期间修改任何Go，下一项TEST042仅仓库外准备。完整模块编译发现与逐名Race count2、原百万/5000预算、普通全仓/覆盖/Race/Vet/Build/tidy/govulncheck及精确清理成功前，本项仍IN_PROGRESS，Goal ACTIVE。
- test039-final-backend-results.json于15:05:51十八阶段全0：初始化7.013s、10批124编译名称248 PASS/0skip，普通159.598s、覆盖1.793s/30.2%、全仓Race184.552s、Vet7.944s、Build4.500s、tidy0.480s、govulncheck6.269s（零可达/导入、四未调用模块公告）。源/完整168 Schema哈希true，拥有test_remediation_test039_1790837410096清理0；14:52:46前端完整六门全0。D359独立Medium/P1 CLOSED，447 CLOSED，本批16/18；TEST042与STYLE003及最终门继续，Goal ACTIVE。

## D-360：TEST042社区完整HTTP与悬赏/翻译事实矩阵

- TEST039全部阶段及源/Schema冻结/拥有库清理成功后才落地十一顶层草稿；正常Session/完整168库/66表事实，四类型发布和审核、私有引用及32/64与1MiB正负、16编辑CAS/语言、hold/reject/refund/resubmit/并发award与self-solve、真实CHECK回滚与可靠意图、翻译配额/权限/16幂等/Outbox/production持久结果、显示能力与真实写权限。尚无实际RED，编译/首轮之前不改生产、不预记通过或关闭。
- 翻译仅合成配置/拥有loopback地址，无真实供应商调用或凭据，production persist helper与人为terminal fixture不冒称完整AI执行/消费送达；财务seed调用production ledger helper，不冒称充值HTTP。所有原预算保持，Goal ACTIVE。
- 更正编译test042-compile-discovery-corrected.log 0/pkg0.399s，十一名称实际编译。首轮normal1/pkg102.027s（墙钟106.561s）：四类型权限/审核、并发award-vs-refund、引用动态可见性与32/64/1MiB边界、翻译16请求幂等/配额/Outbox/真实persist五顶层PASS；有效RED为award/approve/reject通知CHECK后仍200并持久业务事实、hold ledger CHECK被400泄露原始PG错误、reviewer canEdit=true而PUT403、16 CAS一200/14稳定409/一40001误500。resubmit起初在已自动批准revision上再审409，补精确契约探针后normal1/pkg6.250s确认never-published rejected仍在Create审核true/Edit审核false下变approved且匿名GET200，非夹具重审问题。
- 只修改已证实边界：审核和answer接受可靠意图用既有TX helper与审核/退款/结算同事务，原template/recipient/values/data/成功DTO保持，删除post-commit路径。bounty非法币种/不可新增/不可改变使用具名领域错误保持400，余额409，真正PG错误结构化日志+500且无原始Schema错误。显示canEdit与写入口同owner/community.edit/admin.*权限，不授予reviewer额外编辑权限。未首次published的重提按create策略而非edit，已有免审权限保持。
- repeatable-read隔离不降级、不重试写；baseline锁阶段真实40001先rollback释放当前连接，再有界读取最新revision并返回原COMMUNITY_POST_EDIT_CONFLICT/409+currentRevisionId。其他DB读取/rollback故障仍500，不用失败TX或嵌套借连接。完整十一顶层test042-scope-review-money-and-cas-fixed执行中，未关闭，Goal ACTIVE。
- `test042-scope-review-money-and-cas-fixed-full-http.log`normal0/pkg87.478s十一顶层全PASS，原review退款重提与16退款、awards/通知CHECK、16 CAS/人工审核/显式语言、真实翻译/引用预算等均完成。新增四kind never-published拒绝/重提仍pending/显式community.no-review可发布的真实会话正反矩阵及列表/detail能力一致性，两名称Race count2执行中。
- 原增量Medium独立复核为High/P1：真实普通create用户可把被拒且从未发布对象利用edit免审规则直接公开，破坏明确创建审核权威，与BUG087已证同类影响一致；可靠审核/悬赏通知意图丢失、错误泄露/CAS分类也是事实，但不声称P0/他人写入/权限提升或资金重复支出。不按最终严重度汇总凑级，仍IN_PROGRESS，最终全阶段门待完成。
- `test042-four-kind-review-and-list-capability-race.log`退出0/pkg53.912s，两名称各两次、4顶层PASS/0skip。四kind均实际reject→重提pending→匿名404，并验证显式community.no-review正向合同；列表/详情能力与实际PUT匹配。开始完整test042-final编译逐名两次Race和全仓门，Go源码冻结；前端test042-current六门于15:30:42全部0。尚未取得最终源/Schema和清理结果，不关闭本项。
- 首轮test042-final全部170编译名称340 Race PASS/0skip，但普通全仓136.906s/1明确触发现有1500行门：community_post_handlers.go新增修复后1523行。源/168 Schema哈希true，拥有test_remediation_test042_1790839760237清理0；不是完整验收通过。将196行完整翻译请求/可靠入队/结果/持久化职责原样移至community_post_translation_handlers.go，原文件1326行；只迁移两处源码检查的实际文件归属，所有断言保留。四边界检查0/pkg0.718s；重新冻结test042-verified-final全部阶段，不加行数例外或预算。

## D-361：STYLE003稳定错误事实与用户显示文案边界

- 详细审计风险为中，活跃错误响应有重复序列化且legacy error缺稳定code，前端把诊断原样展示并残留固定中文；独立Medium/P1，不因最终严重度汇总改级，不宣称安全提升或数据丢失。实际现有standalone英文日志与风控导航两测试RED/0skip；后端四名称只在仓库外准备，TEST042重新冻结期间不编辑任何Go。
- 前端ApiError class原样移至api-error.mts并由api.ts原名导出，同一身份/status/code/retryAfter/details/diagnostic message保持。唯一展示helper只按稳定code绑定现有翻译键，未知code与传输异常用页面自己的本地化失败提示，不按英文/中文短语或HTTP message判断业务；默认HTTP code只表示状态，不伪称每个legacy输入错误已有独立领域code。
- 审核/作者认领/关注、内容语言、评论/分支、人机验证、日志和风控的已核实活跃显示调用迁移；删admin-community无语义errorMessage与重复反滥用中文validation桥接。原通知八类唯一模板路径不重建、不额外发送；UGC和技术标识不冒充系统文案。英文/简中字典各补152键，其他语言沿用原字典继承/回退，不宣称全部八语种原生翻译。
- 日志保存错误对象后在render用当前t展示，语言切换不触发重新取元数据/正文块；验证错误保存布尔状态、widget效果不依赖t，不因语言切换重建widget。proof头/正文/幂等键、额度/单位、抗滥用/权限/状态判断不改。Next本地useRouter指南要求内部跳转迁为router.push，原编码后/comments/id地址保持。
- 首轮前端283单测282PASS/1旧固定中文源码断言FAIL，Type/Lint/Build/audit0，12浏览器11PASS/1缺location fixture；只迁移字典单位断言并保留原边界、补真实isMainlandChina响应。第二轮12浏览器全PASS/0skip、Lint零warning，单测仅旧window.location.assign源码形状不匹配；更新成支持的router.push/编码地址断言并新增禁旧路径检查。完整style003-frontend-final进行中，后端未修复、STYLE003 IN_PROGRESS，Goal ACTIVE。

### D-360最终独立关闭（2026-10-01 16:36:57）

- test042-verified-final全部19阶段0；170编译名称逐个Race两次，共340 PASS/0skip，普通132.218s、覆盖1.557s/30.2%≥27%、全仓Race149.509s、Vet7.302s、Build4.574s、tidy0.459s、govulncheck5.512s（零可达/导入漏洞，四未调用模块公告）。sourceUnchanged/schemaFilesUnchanged=true，拥有test_remediation_test042_1790841932774清理0。
- 第2批598.498s仍在原600秒包报警内通过，不改变测试context、查询预算或CI阈值。16:24:30当前前端六门全部0/596交付文件哈希保持；283单测/12浏览器/58页/audit0。原1500行门失败报告保留，翻译职责原样拆分后重新验证全门；独立High/P1 CLOSED，448 CLOSED、本批17/18，STYLE003后端及最终Goal验收继续。

### D-361后端首轮实际RED

- TEST042源冻结成功并清理后才落地四名称；编译0/pkg0.359s（no tests to run）不是业务验证。style003-backend-initial-red.log normal1/pkg5.014s：三FAIL明确legacy writeError、空typed code、真实未登录HTTP均无code；成功/Yggdrasil独立协议PASS。语言设置/真实PG失败尚未运行到，不冒称这些分支RED。
- writeError现在仅委托writeAPIError，空code按HTTP状态提供HTTP_nnn；唯一JSON/error/header序列化路径。诊断原文、status、已有领域code、retryAfter、details与成功及launcher协议保持。后续语义code矩阵真实运行后才修改设置Handler，不通过前端分析诊断短语填补语义。

### D-361独立最终关闭（2026-10-01 17:09:23）

- 后端style003-final全部20阶段0，163编译名称逐个两次Race/326 PASS/0skip；普通139.972s、coverage1.700s/30.2%≥27%、全仓Race151.753s、Vet7.114s、Build4.303s、tidy0.408s、govulncheck5.073s（零可达/导入漏洞，四未调用模块公告）。1061源/模块/CI文件与完整168 Schema哈希保持、owned test_remediation_style003_1790844127692清理0。16:43:44配对前端596文件冻结，283单测5.519s/Type2.969s/Lint22.635s/Build12.987s/12浏览器37.115s/audit3.128s，全部0、无warning/skip、58页。
- 独立Medium/P1 CLOSED。本批18/18、449 CLOSED/0NA/0未关闭；原真实业务RED、夹具与源码形状纠偏和原失败报告保留。之后只做用户批准的最终台账口径、当前全部DB/live条件与复审，不用阶段门提前标Goal完成，Goal ACTIVE。

## D-362：用户确认原审计统计与独立复核风险评级分别保留

- 用户先指目标附件，再补MCMods-full-project-audit-2026-08-21.zip。目标附件2785行、仅131个被提及ID/19处严重度字词，并无逐项表；ZIP SHA-256 8F1E45681736BB1FA0977C0E4CEC0D87C3AD74D9BF42EB16D9C5807AB1A49500，包内17份报告逐文件哈希全部与当前原审计一致，449唯一ID/277明确分级/172无明确分级。其它专项表未提供额外172分级，工作副本相同。未解压覆盖、修改原报告、从汇总倒推分级或补造数据。
- 2026-10-01用户明确回答“同意，原统计与复核评级分别保留”。据此原审计Critical0/High176/Medium214/Low59/Info0/Total449仍是不可改写原事实；复核High148/Medium236/Low65单独报告。277原明确分级中只有TEST042有真实证据上调Medium→High、零下降；172原无分级按各项Notes的风险依据复核63High/91Medium/18Low。
- 旧工具在449全部CLOSED之后实际final-original-validator-policy-red.log仍退出1，仅错误要求新评级恰等旧总数，不是剩余业务未修复。源冻结结束后才修改工具：原统计仍逐项强制精确值且原277个明确grade不降低；缺失原grade/上调必须有明确同等级风险理由。全部ID/类别/重复/未知/状态/代码、测试、命令、PASS证据门保持；没有豁免选项、按汇总改级或修改原审计。CLI分别输出original_audit_severities与reviewed_severities。
- 七实际工具顶层测试normal0/pkg3.681s；真实CLI正例全部449 CLOSED/original17621459 PASS，并对14种篡改逐一要求validation FAIL（OPEN/IP/BLOCKED、遗漏/重复/未知、类别、无code/test/command、FAIL结果、降原grade、UNRESOLVED、无独立风险理由）。原分级源消失、统计增减/非法、重复原事实、9种grade组合等也检查。初次默认严格final-approved-validator-strict-first.log退出0；新源码完整质量门/显式所有编译DB与五真实条件门/Race双次冻结验收中，Goal尚不完成。

## D-363：最终真实导入与供应商重定向复审发现残留，重新验证原Finding

- remaining18-final-current在17:23:29实际退出1；完整普通134.347s、Race144.917s、Vet/Build/tidy/govulncheck、工具Race双次与默认严格等前十阶段0，但五真实条件门27.273s失败。6包合同/worldgen v2/live版本/真实Modrinth通过，原版真实导出v1文件把data/minecraft/dimension_type/overworld.json误当namespace。源、完整168 Schema、6 ZIP均哈希保持，拥有test_remediation_remaining18_final_1790846271392清理0。不把前十门冒充最终通过。
- 同一复审补实际production client+标准HTTP重定向测试，只有transport I/O脚本化、全是合成测试凭据。normal1/pkg0.358s证实HTTPS降HTTP及userinfo可触达第二请求、默认443错误拒绝。新worldgen测试也证实resource_id格式/path-only失败，空/冲突条目部分静默遗漏。旧SEC011编译失败仍仅是编译RED，不冒充此次真实行为RED。
- BUG013/SEC011从CLOSED重新FIXED_PENDING_VERIFICATION（447 CLOSED/2待验证），不新增/删Finding或改原严重度。worldgen v1以resource_id标识含registry目录/.json后缀的文件身份、object_id仅是不同对象元数据；合法data路径可确定规范化，空/冲突/畸形路径报错，未知namespace仍严格拒绝，无单revision fallback。HTTP使用原sameProviderOrigin统一scheme/host/规范端口/userinfo边界，不弱化DNS/private-IP/redirect上限。
- 初次定向修复green normal0/pkg0.353s。原真实导入接着通过2536 registry/3298 document/2043 recipe/12320 binding/13302 candidate/159 block-entity model/1003 block binding，暴露下一步capability COPY重复PK，final-import-origin首次条件门1/24.712s、拥有test_remediation_final_import_origin_1790847409281清理0且源哈希保持，报告保留。revisions含minecraft/JEI类别namespace的别名指向同一revision；capability事实属于revision，应只写一次，不能改原集成断言或移除namespace。
- 新真实PG session临时表PK回归normal1/pkg0.434s同样证实别名导致23505；production复用既有sortedExportRevisionIDs唯一化，不引入兼容分支/新DDL/吞错。第二次真实五条件及相关Race双次正在原预算内运行，完成前不关闭或宣称全门通过。

## D-364：按真实来源修正验收前置条件，保留更强的字段/重建回归

- 新包路径搜索只找到2026-07-28的六个实际exporter0.7.0/MC1.20.1/Forge归档，没有原记录的1.21.1-NeoForge目录，不声称最新0.10导出器或新Minecraft验证。原版key_mappings的declared/actual count均0；AE2/Create各3，Mekanism10，其余0。不能让生产导入器编造原版按键来满足旧测试无条件“3页面/>0按键”假设。原集成名称、全流程和其他断言保留，按实际来源计算期望页面，并要求key mapping精确数量（不是仅>0）。另加完整168 Schema中六真实包按键测试，三个非空包16条在首次导入和归档重建后逐ID/defaultKey精确一致，空源不冒充非空覆盖。
- 这个加强测试先真正发现AE2/Mekanism `key.<mod>.<action>`被解析为namespace key；Create的`<mod>.keyinfo.*`原可用。首轮真实DB1/pkg0.905s及纯函数1/pkg0.320s都保留。共享source/identity helper修复两格式且保留RawID，未知namespace及invalid key namespace仍失败，document/registry同一规则；没有从单revision猜归属或删除包/条目。
- 原版39附魔源无supported_items，原无条件四字段AND误把合法缺字段当丢失。新辅助断言在首次和重建后逐39条比较15个存储合同字段的实际提供值（含false/0），未提供字段不得编造；原min/max/rarityWeight等要求保持。另有明确标注为合成输入的完整168 PG合同测试，要求supportedItems及tag/exclusive/costs等15字段逐值完整持久化，不把合成输入声称真实exporter包。key-fixed条件门1/40.186s和旧source元数据保留。
- 接着真实delete-all/reimport在150.86s发现8 active child位于archived root（2 item/block+6 loot categories），field-contract条件门1/161.205s保留。新完整Schema隔离回归的首轮错误23514仅是夹具display_mode='list'不合法，纠正为读取真实template默认mode后verified-red 1/pkg0.899s真正证明孤立child仍active。生产在既有version锁事务内，一次递归SQL只归档当前mod/version的archived祖先下active后代，其他version、合法人工树、pending审核草稿保持；旧临时投影测试补真实updated_at列，原断言不变。另补worldgen files缺失/null/object或非object元素5种实际RED1/pkg0.338s，修复为严格容器shape验证，不静默遗漏。
- final-import-origin-orphan-fixed-backend-results.json 2026-10-01 18:06:10，六阶段全部0：init4.955s、实际key正常11.112s、附魔/孤立/来源scope3.827s、五真实条件194.464s、22实际顶层Race双次27.039s/44 PASS/0skip、全build8.570s；源哈希true，拥有test_remediation_final_import_origin_1790848917701清理0。真实完整原版导入180.49s，删除重建2m34.112s，在原600秒报警/原15秒事务idle设置内通过。所有原失败保留，BUG013/SEC011重新CLOSED，449 CLOSED/0待验证；没有新增DDL/Schema代次、降低原级别、删除测试或把缺源伪装已测功能。此为补修阶段验收，全部当前源码门与最终成熟度继续。
