# Schema 与 API 变更

## SEC-011：供应商配置与请求头协议收紧

- Schema：无数据库 DDL 变化；现有加密 `system_settings` JSON 结构不变。
- 管理 API：保存 Modrinth、CurseForge 或 GitHub 配置时，非官方 origin 不再允许携带供应商凭据；BaseURL 更换 origin 后，省略凭据表示清除而不是继承。非法组合返回 400。
- 运行时：全部 22 个供应商请求构造点经单一 origin 白名单附加 `Authorization` / `x-api-key`；不匹配时不发送。
- 兼容边界：无凭据自定义 HTTPS 和本机 HTTP 仍可使用；需要供应商凭据的请求必须使用对应官方 origin。未保留旧泄漏行为的双读/双写路径。

## SEC-013：Mod 关系写入与展示协议

- Schema：无 DDL 变化；`mod_relationships.mod_id` 现被实现明确当作来源所有权事实。
- 写 API：客户端传入的 `relationshipGroups[].direction="incoming"` 为只读展示回传，标准化时被剔除，持久化层也防御性忽略；出向站内目标必须为非自身且 `approved`。
- 读 API：入向关系继续从已审核来源 Mod 派生；出向关系若绑定目标不再 approved，则不在公共装配中返回。
- 前端：编辑器只读展示入向组，编辑控件和提交负载仅包含出向组。
- 兼容边界：旧客户端继续回传 incoming 不会报错或跨项目写入；不保留旧的可编辑入向语义。未收录的出向 Mod ID 行为不变。

## SEC-039：经济金额与数量域

- 根因组：`RC-CHECKED-ECONOMY`。旧实现允许 `amount * taxBPS`、`unitPrice * quantity`、奖励乘法及 `current + delta` 在检查前发生 `int64` 回绕。
- Schema：权威版本从 generation 85 升至 86。余额、流水、下载计数/奖励步数、商店价格、库存、购买和问题悬赏均增加显式范围；购买强制总价乘积，已颁发悬赏强制 gross 分解守恒。
- API：转账金额必须位于 `(0, 1e12]`，超域返回 400；购买累计库存超过 1,000,000 返回 409；成功 DTO 与字段保持不变。
- 调用方迁移：转账与悬赏结算复用精确整数税额函数；商店、下载奖励和所有余额写入复用 checked arithmetic。前端现有整数输入协议无需双读或版本分支。
- 事务/回滚：余额先锁定并检查，越界时不更新余额、不写流水；商店扣款后的库存上限失败由同一事务整体回滚。
- 开发库：generation 85 数据库必须按既有开发策略显式重置后安装 86；没有在线升级、生产 DDL 或兼容双写。远程账号不具备临时建库权限，因此本轮只完成 Schema 源契约和真实事务回滚验证，隔离空库动态安装保留为最终环境门禁。
- 查询计划：变化均为常量算术、行锁后的主键更新及 CHECK 约束，不新增查询或扫描路径；无需新增索引。

## SEC-040 / SEC-044：多来源授权事实

- 根因组：`RC-AUTHZ-SOURCES`；同时关闭 BUG-002、BUG-004、BUG-059、BUG-122、MAP-011、LEGACY-020。
- Schema：权威版本从 generation 86 升至 87。`user_role_bindings` 主键变为 `(user_id,role_id,source,source_key)`，`user_permissions` 主键变为 `(user_id,permission_id,source,source_key)`；source 由 CHECK 限定，并增加用户/来源索引。直接权限的无效 `context` 列删除。
- 来源：角色为 manual/account_status/governance_ban/level_track；直接权限为 manual/user_preference/system_seed。manual key 为空，自动来源 key 非空。
- 管理 GET：`GET /api/v1/admin/users/{id}/permissions` 新增 `roleBindings`；角色和直接权限条目返回 `source`、`sourceKey`、`editable`、`expiresAt`。`groupPermissions` 仅保留 manual 兼容投影。
- 管理 PUT：仅替换 manual 角色与直接权限；保留其 expiry。非 manual source/sourceKey、`group.* allow=false` 和重复 code 返回 400。封禁、等级、用户偏好和系统 seed 不可从该入口修改。
- 删除 API：移除未被第一方调用的 `PUT /api/v1/admin/users/{id}/roles`，不提供转发 Wrapper；当前前端已只使用统一 `/permissions` 入口。
- 角色图 API：创建/更新在同一 advisory transaction lock 内验证完整 DAG；缺父级或间接环返回 400。删除内建或仍被默认配置、子角色、用户绑定、轨道引用的角色返回 409，不再隐式清理依赖。
- 调用方迁移：账号状态、治理封禁、等级、资料页消息偏好和 seed 全部写明确来源；权限解析对同角色多来源去重，对同直接权限冲突保持 deny 优先。前端草稿只装载 editable manual 条目，系统来源在独立只读表中展示。
- 事务：manual 保存和角色线路升降先锁用户；角色线路同时共享锁定线路定义，并在锁后读取 snapshot。等级配置切换或清空逐用户精确重算 level 来源。
- 开发库：generation 86 必须按既有开发策略重置至 87；不执行在线升级、兼容双写或未知远程 DDL。隔离空库动态安装仍受远程账号权限限制。
- 查询计划：新增索引 `(user_id,source,source_key)` 支持来源精确替换；有效权限角色读取使用 DISTINCT 折叠多来源，不改变外部结果规模。最终空库环境应对该查询补 EXPLAIN 门禁。

## BUG-095 / BUG-096 / BUG-097：可执行经济配置

- 根因组：`RC-CHECKED-ECONOMY`；与 SEC-039 一并关闭该组。
- Schema：权威版本从 generation 87 升至 88。新增 `content_heat_promotion_counters(object_route_id primary key,last_sequence_no)`；`content_heat_promotions.sequence_no` 改为 bigint，并唯一约束 `(object_route_id,sequence_no)`；`shop_items.item_type` 增加三个运行时支持值的 CHECK。
- API：`PUT /api/v1/admin/economy/config` 对启用规则引用非 active 货币返回 400；`PUT /api/v1/admin/economy/currencies/{publicId}` 对仍被启用规则或 active 商品引用的停用/改码返回 409；active 商品引用 disabled 货币或任意商品使用未知 itemType 返回 400。成功响应字段不变。
- 调用方迁移：前端 `ShopItemType` 收紧为三值联合，管理员编辑器已使用对应 select；不增加自由文本或未知类型回退。停用/改码货币前必须先保存新的经济规则并迁移或禁用引用商品。
- 事务：经济配置、货币和商品管理变更共享 advisory xact lock。签到和下载奖励在同一锁内读取配置并结算。促销 counter、促销事实、刷新请求和库存消费同事务提交或回滚。
- 兼容边界：不保留 racy `count(*)+1`、disabled 货币奖励或未知商品类型的双路径。generation 87 开发库必须显式重置至 88；没有在线升级或远程 DDL。
- 查询计划：历史促销 `count(*)` 扫描被单行主键 UPSERT 替代；货币 blocker 为低频管理写入检查，active 商品引用通过既有货币外键连接。最终隔离空库仍需执行 EXPLAIN 与动态安装门禁。

## SEC-025 / SEC-047：供应商身份与脚本配置

- 根因组：`RC-PROVIDER-TRUST`；与 SEC-011 一并关闭该组。无数据库 DDL 或后端 HTTP 字段变化。
- Modrinth：导入端不再从任意字符串正则提取 `/data/.../versions/...`。严格解析器同时服务整合包导入、收藏夹导出和 MRPack 生成；不可信/冲突镜像只产生文件名 fallback，不产生 provider project/version IDs。
- Iconfont：部署新增 `NEXT_PUBLIC_ICONFONT_SYMBOL_INTEGRITY`。URL 与 SHA-384 SRI 必须同时合法或同时留空；非法配置在布局模块初始化时失败，不等到浏览器静默忽略。Loader 设置 SRI、anonymous CORS 与 no-referrer。
- 兼容边界：合法官方 Modrinth CDN URL 和 `at.alicdn.com/t/c/font_*.js` 保持支持；Iconfont 部署必须补充固定脚本的 SHA-384 值。没有相似域、任意阿里子域、未知路径或无 SRI 兼容分支。
- 性能：URL 校验为本地有界解析；不增加网络往返、数据库查询或索引。前端无 Iconfont 配置时不发远程请求，有配置时仍只加载一个脚本。

## SEC-042：举报目标可见性

- 根因组：`RC-CROSS-PROJECT-OWNERSHIP`。无 Schema 或成功响应字段变化。
- API：统一举报及旧评论举报适配入口现在使用当前 claims 解析目标；不可见、状态不允许或目标类型不匹配统一返回 404。作者/所有者及已有审核权限保持其既有可见范围。
- 评论：评论必须为 published/deleted，且所属 Mod、项目、社区内容、蓝图、皮肤或其他评论目标当前可见；邻近上下文排除内部状态。附件元数据与正文使用同一事务快照。
- 事务：举报入口从普通事务改为 Repeatable Read；可见性、快照、举报行、快照摘要和附件绑定观察同一数据库快照并共同提交/回滚。
- 复用：项目类走 `resolveFollowProjectTargetWithQueryer`，评论所属目标走 `resolveCommentTargetByInternalWithQueryer`；原公开调用保留薄 wrapper，不复制一套状态清单。
- 性能：每次举报新增一次类型化目标解析，属于低频受限写入口；移除仅凭 route 的宽松读取。无列表热路径或新索引。

## SEC-035：收藏目标可见性

- 根因组：`RC-CROSS-PROJECT-OWNERSHIP`。集合 ACL 与目标 ACL 是两项独立事实；`includePrivate` 不再出现在目标可见性判断中。
- Schema：权威版本从 generation 88 升至 89；`favorite_collection_items.entity_type` CHECK 仅允许 `mod`、`modpack`、`blueprint`，与当前第一方收藏入口和展示协议一致。
- API：membership GET/PUT 对不可见、不存在、不支持或类型错配目标统一返回 404；成功字段不变。收藏摘要 `itemCount`、拥有者/公开列表与 MRPack 导出预检按当前 viewer claims 过滤。
- 事务：PUT 在 Repeatable Read 内复用 `resolveFollowProjectTargetWithQueryer`，目标解析和集合项删除/插入共同提交或回滚。默认集合创建仍先于该事务且不携带目标数据。
- 调用方迁移：前端本来只发 Mod、整合包和蓝图，无 DTO 修改；数据库中不再接受其他类型。已隐藏对象不会从列表或导出预检泄露，重新公开后可按当前权限再次出现。
- 查询计划：列表、摘要和导出用同一组 route/三类对象 JOIN 与 CASE 过滤，消除逐项 N+1；集合过滤仍使用现有 owner/public 和 collection 主键/唯一键。generation 89 隔离空库最终需补 EXPLAIN；当前远程账号无 `CREATEDB`，未执行 DDL。

## SEC-037：社区项目引用可见性

- 根因组：`RC-CROSS-PROJECT-OWNERSHIP`。无 DDL；权威 Schema 保持 generation 89。
- API：社区引用增加可选 `unavailable=true` 安全占位。该对象只保留 `type`，`publicId`、`identifier`、`name`、`siteId` 均为空/省略；显式未解析 identifier 仍使用既有 `unresolved=true` 协议。
- 写入：创建/更新在 Repeatable Read 内以当前 claims 解析项目类型、可见性和 route 类型一致性。待审发布以 revision 提交者身份重新验证，状态变化返回 409；自动批准继续使用当前提交者 claims。
- 读取：详情和最多 100 篇的目录页使用一次引用身份查询加一次 `public_id=any(...)` 可见目标批量查询，不逐引用查询。名称和 URL 来自当前目标事实，不复制隐藏元数据。
- 筛选：`modId` 与结构化 `project` EXISTS 同样加入统一可见性谓词；未绑定 raw identifier 仅能匹配其显式 raw 值。带目标/资源筛选时关闭 Typesense 预选，数据库结果与 `total` 一致。
- 调用方迁移：前端 API 类型接受 `unavailable`，详情使用既有 `communityPosts.unspecified` 文案显示不可用引用；编辑器与本地化用户改动未覆盖。成功保存/读取 DTO 其余字段不变。
- 性能：项目目标单项解析由 route+对象两次 SQL 合并为一个共享 SQL；社区批量装配固定两次查询。最终隔离库需对目录筛选 EXISTS 和 `public_id=any` 补规模化 EXPLAIN。

## BUG-098 / BUG-099 / BUG-100 / ARCH-028 / PERF-057：表情完整性

- 根因组：`RC-STICKER-INTEGRITY`。权威 Schema 从 generation 89 升至 90；仍采用开发期整代重置，不引入在线 backfill、九表扫描兼容层或引用双写。
- 图片 Schema：`stickers.image_file_id` 新增命名唯一约束 `uq_stickers_image_file`，明确一个 OSS 对象只能属于一个表情。应用按文件 ID 排序锁旧/新 `oss_files` 行，新图必须在锁后仍为 active；归档前查询当前是否仍有 sticker 绑定。
- 引用 Schema：新增 `sticker_content_references(source_type,source_key,source_field,pack_code,sticker_code,created_at)`，五列主键去重，`(pack_code,sticker_code)` 索引服务删除存在性查询。表不外键到 sticker，使不可变历史引用不会随目录项删除而级联消失。
- 触发器：安装阶段动态发现 public schema 中所有 `*_markdown`、`comments.body`、`content_revisions.snapshot`，要求源表具有主键，并为每个字段安装 INSERT/UPDATE/DELETE 触发器。文本和 JSON 都用同一严格 Token 正则；复合主键序列化为稳定 JSON source key。
- 并发：触发器在按 pack/code 排序后取得 `sticker-reference` advisory xact lock；删除取得同名锁后才查索引并删行。表情创建/删除及删包锁父 pack 行，消除检查后创建、外键等待后级联删除和 OSS 孤儿窗口。
- 目录预算：新增 `STICKER_MAX_PACKS`、`STICKER_MAX_PER_PACK`、`STICKER_MAX_CATALOG_ITEMS`，默认 64/128/1024，硬上限 256/512/4096。创建端在 `sticker-catalog-budget` 锁内计数；公开/后台目录使用上限加一查询并在迭代中复核包、每包和总项数。成功响应结构不变，无前端迁移。
- API：超预算创建返回 409 `STICKER_CATALOG_LIMIT`；图片已被其他表情占用返回 409 `STICKER_IMAGE_IN_USE`；验证后状态变化返回 409 `STICKER_IMAGE_INVALID`。包 code、表情 code 仅在精确唯一约束命中时返回既有冲突码，其他数据库、迭代、版本或翻译解码错误返回 500。
- 查询计划：真实 PostgreSQL 测试关闭顺序扫描后，删除存在性查询使用 `idx_sticker_content_references_token`；不再随 Markdown 表数量执行 `position(token in body)` 全表扫描。完整 generation 90 空库安装仍待具备隔离建库权限的最终门禁。

## SEC-041 / BUG-101 / TEST-045：表情派生图片与客户端一致性

- 根因组：`RC-STICKER-INTEGRITY`，与 BUG-098/099/100、ARCH-028、PERF-057 一并关闭。无新增 DDL；权威 Schema 保持 generation 90。
- 图片协议：管理员仍提交已上传的 `fileId`，但该 `sticker-upload` 仅作私有源。服务端验证并重编码后创建 `source='sticker_derived'`、`scan_status='trusted_generated'` 的新 OSS 事实，表情绑定派生 fileId；成功后源文件归档为 `sticker_source_consumed`。公开成功 DTO 字段不变。
- 清理：生成对象上传成功但 `oss_files` 登记失败时直接删除对象；后续贴纸事务失败或提交结果不确定时，补偿事务先锁派生文件并确认没有 sticker 所有者，再归档并写幂等删除 Outbox。替换与删除继续只归档已无绑定的旧派生物。
- 公开边界：公开目录 JOIN 和 OSS inline 白名单只承认 `sticker_derived + trusted_generated`，不再将 `sticker-upload` 或历史 `sticker` 来源直接公开。PNG/GIF 重编码删除文本/注释元数据，同时保留受预算约束的动画帧、延迟与处置。
- 前端缓存：每 locale 缓存 TTL 为 30 秒，窗口内请求合并、失败立即驱逐；管理端所有成功 mutation 立即调用 `invalidateStickerCatalog`。跨页面不要求永久连接或新增 API，最迟按 TTL 收敛。
- 渲染/测试：Token 替换提取为纯 AST 模块，明确跳过链接、引用链接、代码块和行内代码并执行 50 项预算。新增真实编码 PNG/GIF、畸形/预算、权限、生命周期、真实 PostgreSQL 引用/删除、缓存和 AST 测试；Next 生产构建保持 59 个静态页。

## SEC-007：Markdown 差异资源预算

- 无 Schema、HTTP 字段或调用方迁移。评论 10k runes、作者每语言 256 KiB、服务器正文 100 KB 的既有业务限制保持不变并新增直接测试。
- 内部活动统计的精确编辑距离上限从 8192 收紧为 1024；frontier 从按前后正文总长分配改为只按上限分配。超过上限的重写返回公共前后缀外的保守增/删字节数。
- 小编辑的精确语义不变；大编辑的净增长仍精确，总改动只可能保守偏大。这一内部审计差异不影响正文持久化、响应 DTO、权限、活动对象身份或数据库索引。

## SEC-038：公共内容读取与 AI 翻译费用边界

- 无 Schema 变化。公共 GET 仍返回相同 `translation` 对象，但缺失的可编辑语言从自动 queued/running/unavailable 改为 `request_required`；`automatic=false`，`canRequest=true`，`countsTowardDailyTokenQuota=true`。
- `POST /api/v1/content/{publicId}/translations` 现在也接受缺失的八种可编辑语言，不再返回“会自动翻译”的 409。它与其他合法目标语言一样要求认证、正额度并事务预留用户每日 Token；成功 DTO 仍为 202 taskId/status。
- catalog enqueue API 删除 `quotaBacked=false` 变体。actorId/tokenLimit 非正立即失败；payload 固定 `quotaBacked=true`，`created_by` 固定为 actor，任务复用键始终包含源修订、目标语言和 actor。
- 前端既有 `request_required + canRequest` 控件无需字段迁移，TypeScript 与定向 ESLint 已通过。部署不需要数据回填；旧 actor 0 任务若存在按既有任务生命周期结束，本实现不再创建新任务。

## SEC-010：嵌入图标解码预算

- 无 Schema、成功 DTO 或调用方迁移。IconRenderer/LetMeSeeSee/IRR 的合法 PNG 继续生成 32、128、256 三种资产。
- 单个 decoded PNG 输入上限从 16 MiB 收紧为 8 MiB；新增宽/高 4096 和总像素 4,194,304 的解码前硬限制。超限任务使用既有导入失败状态与条目上下文，不返回部分成功图标。
- 图片 entry 构建使用可取消的进程级 2 槽信号量，并把每作业 errgroup 同步为 2。令牌从第一张源图解码前持有到全部派生 PNG、digest 和 media 描述完成，覆盖真实保留内存窗口。
- 不修改 OSS key、digest、媒体尺寸或 catalog resource 数据协议；只拒绝此前可导致约 400 MiB/图乃至多 GiB 并发峰值的输入。

## SEC-017 / SEC-018：Presence 与 SSE 实时预算

- 无 Schema 变化。Presence 的 Redis 共享集合新增 Lua 原子“写入、清过期、按 score 删除最旧直到 50,000、设置 TTL”；本地集合为 4,096。前端不再生成可任意伪造的 UUID，而是把成功响应的服务端 `visitorId` 写回原 localStorage key。
- `POST /api/v1/site/presence` 匿名成功响应由 `{online}` 扩展为 `{online,visitorId}`；登录响应仍可只有 `{online}`。同来源超频返回 429、code `PRESENCE_RATE_LIMIT` 与 Retry-After。既有 60 秒心跳处于 60/5min 共享与 12/5min本地预算内。
- `GET /api/v1/realtime/events` 在写 SSE headers 前取得 TTL lease。超出 2/Session、4/User 或 2,048 总连接返回 429 `REALTIME_CONNECTION_LIMIT`；连接最长 30 分钟，20 秒心跳同时刷新 90 秒租约。
- Redis lease key 是短期 ZSET，不需 DDL/backfill；每个 lease ID 同时存在 all/user/session 三个集合。断开 ZREM，崩溃由 score/TTL 回收。无 Redis 时本地锁表执行相同三层判定。
- `GET /api/v1/admin/infrastructure/metrics` 的 `realtime` 增加 `rejected`、`dropped`、`limits`；原 connections/users 保持。SSE 事件 DTO 不变，客户端按标准重连即可。

## SEC-023：浏览页面身份与指标预算

- 无 Schema 变化，权威版本保持 generation 90。`content_project_pages` 结构不变，但写入来源从任意客户端字符串收紧为服务器解析出的 `detail` 或当前资源 active `version:{publicId}`。
- `POST /api/v1/content/{publicId}/metrics/view` 对未知 key、非 resource 版本、inactive 版本和属于其他资源的版本返回 400 `METRIC_PAGE_INVALID`；来源超出 60/5min（本地回退 12/5min）返回 429 `METRIC_VIEW_RATE_LIMIT`。合法及 24 小时内重复浏览仍返回 204。
- 匿名 viewer/source 由 AntiAbuse secret 对可信客户端 IP 做 HMAC；数据库只保存该稳定身份的 SHA-256，不保存明文 IP。登录 viewer 使用 user ID 的哈希。page hash 只在闭集和数据库归属通过后生成。
- 同一 route/page/viewer 的 `content_view_daily`、`site_view_daily`、`content_unique_views`、`content_project_pages` 与 refresh enqueue 在 24 小时窗口只执行一次；事务中的原有原子性保持。
- 前端当前只发送 `detail` 和当前资源版本 public ID，无 DTO 或调用迁移；错误仍由共享 API 客户端处理。不提供旧任意 pageKey 的兼容分支，也不增加清理/双写 Schema。

## SEC-043：草稿存量配额

- Schema：权威版本从 generation 90 升至 91；`user_drafts.payload` 新增 `octet_length(payload::text)<=524288` CHECK，并保留 object 类型 CHECK。仍采用开发期整代重置，不增加旧表回填、配额计数器双写或在线迁移。
- API：`POST /api/v1/users/me/drafts` 与 `/drafts/complete` 的成功 DTO 不变。payload 超限返回 413 `DRAFT_PAYLOAD_TOO_LARGE`；active/completed 条数返回 409 `DRAFT_COUNT_LIMIT`；用户总量返回 409 `DRAFT_STORAGE_LIMIT`；专用写速率返回 429 `DRAFT_WRITE_RATE_LIMIT` 与 Retry-After。
- 原子性：每用户 advisory xact lock 覆盖过期删除、active/completed 计数、canonical JSON 字节总和、已有 active key 差额和最终 INSERT/UPDATE。并发请求不能同时观察旧余量后双双提交。
- 预算：单条 512 KiB，active 64，completed 256，总 canonical JSON 32 MiB；共享 120 writes/5min，本地失败安全 30 writes/5min。DELETE 和读取不消耗写入预算；替换不重复占条数。
- 调用方：前端 15 秒 autosave 最多 20 次/5min，处于本地失败安全预算内；现有共享 API 错误处理会显示保存失败，无 DTO/组件迁移。历史达到上限时不静默裁剪或假报完成。
- 空库/回滚：真实 PostgreSQL 临时表验证了 CHECK 表达式、计数/差额/过期与锁；完整 generation 91 隔离空库仍待可建库账号。没有对当前远程数据库执行 DDL或重置。

## SEC-014：Minecraft Configuration 主动探测预算

- 无 Schema 变化，权威版本保持 generation 91；无数据库回填或部署迁移。
- 内部协议：Configuration 仍为 2 MiB/包、512 包、30 秒；Fabric 累计从 64 MiB 降为 8 MiB。Frozen/Dynamic/Fabric 的计数必须同时满足剩余编码字节和一次探测 16,384 共享单元；evidence namespace 最多 4,096，NBT 节点最多 16,384。
- 运行时：`ProbeWithOptions(..., true)` 的 Configuration 阶段使用进程级 4 槽非阻塞 admission；满载不再打开新主动探测连接，返回既有 status 结果与 `detectionDiagnostic` 容量说明。`Probe(..., false)` 被动状态探测不占该槽。
- 成功 API/DTO 不变；合法小型 registry、NeoForge/Fabric 协议和 proxy 1.21.1 fallback 保持。超预算远端数据只终止可选命名空间发现，不把部分结果标为完整 JAR 列表。
- 内存证据：count=1,000,000 且无正文的 Frozen/Dynamic 数据每次解析分配低于 1 MiB；结果是 Go benchmark 的 alloc/op，不宣称生产 RSS。无外部服务调用、无公网探测。

## PERF-004：反滥用异步派生记录

- 无 Schema 变化，权威版本保持 generation 91；成功 fingerprint/event/cleanup 仍写原表，业务提交事实不依赖这些派生行。
- 内部生命周期：`httpapi.NewServer` 现在接收应用 context 并返回可 `Shutdown(ctx)` 的 Server。成功记录队列为 1,024、固定 2 workers、每项 2 秒；应用关闭在 DB/cache 前给 worker 最多 3 秒排空。
- 请求路径：2xx/3xx 受保护 mutation 从 `go RecordSuccess(context.Background())` 改为非阻塞 `EnqueueSuccess`。队列 snapshot 的 Content 上限 20,000 bytes；其余字段已经受请求/对象规范化边界约束。
- 清理：原 success worker 内二次 `go cleanup(Background)` 删除；同一 2 秒 context 内执行三项各最多 1,000 行的既有清理。风险 event queue full 的临时 goroutine 也删除。
- 管理 API：`GET /api/v1/admin/infrastructure/metrics` 新增 `antiAbuse` 对象，包含 success queued/processed/dropped/failed/depth/capacity 和 risk dropped/depth/capacity；其他字段不变，普通客户端无需迁移。
- 故障语义：队列满或 shutdown drain 超时不回滚/改写已成功业务响应；丢弃量可观测。数据库错误计 `successFailed`。派生事实没有被伪装成事务可靠 outbox；未来若参与授权必须另行升级。

## PERF-061 / BUG-120：草稿分页与完成权威

- Schema：权威版本从 generation 91 升至 92。`user_drafts` 的 active 行必须没有审核引用；completed 行必须恰好引用一个 change request，或 `review_target_type='server'` 且有 server ID。`submitted_status` 增加由服务器映射的 `rejected` 快照，不接受客户端状态。
- 索引：原全体 owner/updated 索引替换为 active `(user_id,updated_at desc,id desc) where submitted_at is null`；新增 completed `(user_id,submitted_at desc,id desc) where submitted_at is not null`。已决审核单排序仍需联接 `resolved_at`，但候选集合被 SEC-043 的 256 条 completed 上限严格限制。
- 列表 API：`GET /api/v1/users/me/drafts` 新增 `category=active|completed`、`limit=1..50`、opaque `cursor`；缺省类别只返回 active，不再兼容隐式全量。响应保留 `items/retentionSeconds` 并新增 `category/nextCursor`。非法类别、页长、畸形或跨类别 cursor 返回 400 `DRAFT_LIST_INVALID`。
- 完成 API：`POST /api/v1/users/me/drafts/complete` 删除 `reviewStatus`。`changeRequestId` 与 `reviewTargetType/reviewTargetPublicId` 必须二选一且属于当前用户；状态在同一事务从权威表读取。严格 JSON 解码使旧字段明确失败，不保留双重权威兼容路径。
- 响应状态：列表状态域从 draft/reviewing/approved 增加 `rejected`，表示 rejected/conflicted/withdrawn 或服务器其他非 pending/approved 终态；前端已增加双语徽章。本人导航元数据仍不参与授权。
- 前端：草稿箱并行读取 active/completed 各 30 条，保存两个 cursor；“加载更多”先排空 active 再继续 completed，以 public ID 去重。首屏最多 60 项，用户主动加载也受 64/256 存量硬限。
- 部署：这是开发期整代重置，现有 generation 91 数据库必须按既有流程重建；不增加旧协议双读/双写或在线回填。真实 PG 临时表已验证 keyset 与归属，完整 generation 92 空库初始化仍受当前外部建库权限门禁。

## SEC-009：导入包不可变来源与实际内容验证

- Schema：权威版本从 generation 92 升至 93。`catalog_import_packages.sha256` 删除全局 UNIQUE；`archive_file_id` 改为 NOT NULL UNIQUE、`ON DELETE RESTRICT`，`uploaded_by` 改为 NOT NULL、`ON DELETE RESTRICT`。新增 `content_verified_at` 和仅覆盖已验证行的非唯一 `(sha256,content_verified_at desc)` 索引。
- 不变量：触发器拒绝更新 package 的 `sha256/archive_file_id/archive_name/uploaded_by`。同一 `archive_file_id` 且来源元数据完全一致的请求以无修改 conflict 分支返回原 package；来源不一致时不覆盖旧行。
- 内容边界：mcmods_exporter ZIP 与嵌入图标目录都先流式下载并比较实际 SHA，再对完全匹配的 package/file/SHA 三元组写 verified 时间。上传元数据中的 SHA 不再触发跨文件、跨用户 package 复用。
- 生命周期：两个创建端点在事务中按当前用户和 active 状态重读 OSS 文件并取得 `FOR KEY SHARE`；用户删除文件取得 `FOR UPDATE` 后检查 queued/validating/confirmation_required/importing job，活动源返回 409。任务终态后保留 package 审计来源，物理外键删除受 RESTRICT。
- API/调用方：创建、重试和成功响应 DTO 不变；不同 OSS 文件即使 SHA 相同也独立处理，同一文件重试保持幂等。前端无需迁移。旧的全局去重属于被删除的不安全内部行为，不提供兼容分支。
- 部署：这是开发期整代重置；完整 generation 93 空库安装仍受当前账号无 CREATEDB、维护库被 pg_hba 拒绝的外部门禁。真实 PG 临时表已验证两用户同 SHA、精确幂等、verified 三元组和活动任务锁；未对远程业务表执行 DDL。

## BUG-019：Mod 导入重试与停滞恢复可靠重入队

- Schema：无变化，权威版本保持 generation 93；复用现有 `nats_outbox` 每事件唯一 ID、pending 状态和 JetStream/local fallback dispatcher。
- 事务：手动 retry 与 Worker 启动恢复都先开启事务，在相同事务内更新 job、取得实际变更行并调用统一 `enqueueModExportAttemptTx`。创建 ZIP、创建嵌入图标目录和 MODID 确认也迁移到该 helper，生产代码不再手写该任务的 Outbox INSERT。
- 事件：初建、确认、重试和恢复使用不同 event type；payload 保持 `{jobId}`，task subject 与 aggregate 类型/ID 不变。每次 retry/recovery 创建新 event ID，不修改已经 published/dead 的旧尝试。
- 消费：重复事件在 job 已非 queued 时无法取得 run token，消费者将 `errModExportLeaseLost` 视为幂等完成；其他解析、数据库和 Worker 错误仍失败并由可靠队列处理。
- API：`POST .../jobs/{jobId}/retry` 的请求与成功 DTO 不变。Outbox 写入失败时事务回滚并返回 500；无合法状态转换仍为 409。启动恢复无公开 API 变化。
- 兼容边界：BUG-019 先消除默认 Outbox 模式的丢投递；后续 LEGACY-006 已删除同一任务的 Core NATS/goroutine 旧模式。无 DDL、回填或前端迁移。

## LEGACY-006：删除 Mod 导入 Core NATS / goroutine 双路径

- Schema：无变化，权威版本保持 generation 93。现有 `nats_outbox` pending/failed/publishing/published/dead 状态与索引不变。
- 运行时：`queue.NewOutboxDispatcher(db,queueClient,true)` 始终启动。Mod Worker 启动只做 BUG-019 的原子停滞恢复并调用 `SubscribeTask`；即使 NATS 离线导致订阅返回 unavailable，handler 已注册供 PostgreSQL dispatcher 的 `HandleLocally` 使用。
- 删除：移除 Outbox 开关分支、queued 全表启动扫描、Core `PublishTask`、请求后 `dispatchModExportJob`、手工设置 `published_at` 和进程内 2 小时后台 goroutine。所有四类生产入口只写统一事务 Outbox。
- API：创建、确认、重试及 job DTO/状态码不变；不保证请求提交后立即发 Core 消息，dispatcher 轮询周期上限约 1 秒。NATS/JetStream 故障时任务保持数据库 pending/failed 并可观测，不假报 published。
- 配置：`NATS_OUTBOX_ENABLED` 暂未从全局配置删除，因为其他已登记任务族仍在迁移；它不再出现在 Mod 导入 Worker/生命周期实现，也不能关闭运行时 dispatcher。其他直接任务路径不借此项提前关闭。

## LEGACY-019：内容翻译迁移到统一 AI Outbox 事务

- Schema：无变化，权威版本保持 generation 93；复用 `ai_tasks` 与 `nats_outbox`。不增加恢复标记、双写列或旧入口表。
- 服务：新增 `enqueueAITaskTx`，统一通用 AI 创建、目录内容翻译和社区帖子翻译的 subject/aggregate/payload。event type 分别标识通用、目录内容与社区内容请求，task UID/类型协议不变。
- 事务：内容任务的 advisory lock、活跃任务去重、token 配额检查/预留、AI 行与 Outbox 事件共同提交。事件约束/数据库故障使任务和预留一起回滚；既有活跃任务直接返回且不重复发事件。
- 删除：`publishContentTranslationTask` 与两个 Handler 的提交后发布调用完全移除；通用 `createAITask` 同时删除 Outbox 开关和 Core NATS 失败回写分支。日志改为“committed to reliable outbox”，不再声称已发布到 NATS。
- API：三类成功 DTO 与状态码不变；新任务只有可靠意图已提交后才返回 queued。Outbox 写失败沿既有 Handler 错误通道返回，不生成稍后可见的 failed task。前端无需迁移。
- 边界：开发期不回填旧 direct-NATS 行；干净 generation 93 之后只产生新事务事实。后续 LEGACY-008 已将通知翻译也收口，queued/dead 运维恢复另由 OPS-019 复核。

## LEGACY-008：通知翻译迁移到统一 AI Outbox

- Schema：无变化，权威版本保持 generation 93；复用 LEGACY-019 的 `enqueueAITaskTx` 与始终启用的 dispatcher。
- 事务：用户 quota lock、当日已用/预留统计、`ai_tasks` INSERT 与 `ai.notification_translation.requested` 事件一起提交。事件 aggregate 为 task UID，payload 为通用 AI task message，并保留 HTTP `X-Request-ID` trace。
- 删除：通知 Handler 不再在 commit 后 `PublishTask`，不再因 NATS 不可用另起 UPDATE 把 task 标为 failed，日志不再声称已发布到 NATS。Outbox 插入失败时原事务回滚。
- API：cached/system/locale/额度规则与 200/202 成功 DTO 不变；可靠入队失败返回创建错误且没有可查询 task。连接故障时已提交任务保持 queued，由 Outbox 状态表达基础设施阶段。
- 边界：AI Worker 继续以 `status in ('queued','retrying')` CAS 领取 task，重复消息返回成功；结果持久化的失败关闭另属 BUG-044。无前端迁移、DDL 或旧 direct-NATS 兼容 Wrapper。

## OPS-019：AI queued / stale 任务恢复

- Schema：权威版本从 generation 93 升至 94。新增 `idx_ai_tasks_recovery(status,updated_at,id) where status in ('queued','retrying','running')` 和 `idx_nats_outbox_aggregate(aggregate_type,aggregate_id,id)`；不增加任务影子表或第二队列。
- 恢复：AI Worker 启动立即恢复，随后每分钟执行一批最多 100。无任何 aggregate Outbox 的 queued/retrying 补 `ai.task.recovered`；running 超过 15 分钟先转 retrying，再在同事务写新 event。`FOR UPDATE SKIP LOCKED` 支持多实例分工。
- 消费：领取抽为 `claimAITaskForExecution`，只允许 queued/retrying→running 一次；重复 delivery 返回 nil。任务注册定义为 90–120 秒、默认 queue handler 300 秒，恢复阈值不抢占正常调用。
- 故障：event 写失败回滚状态；恢复扫描错误记录 warning 并在下一周期重试。已有 pending/published/failed/dead 事件的普通 queued/retrying 不重复，dead 由管理员 replay；stale running 即使有旧 published event 仍生成新尝试。
- 计划：临时规模为 100,000 completed + 500 active task、100,000 无关 + 499 相关 event；`EXPLAIN (ANALYZE,BUFFERS)` 同时使用 `idx_ai_tasks_recovery` 与 `idx_nats_outbox_aggregate`。结果是测试数据库计划，不冒充生产延迟。
- API/部署：无公开 DTO 或前端迁移。generation 93 开发库按整代流程重建；完整 generation 94 空库仍因账号无 CREATEDB、维护库 pg_hba 拒绝而未执行，未对远程业务表做 DDL。

## LEGACY-013：蓝图上传/转换统一事务 Outbox

- Schema：无变化，权威版本保持 generation 94；复用 `blueprint_jobs` 与 `nats_outbox`，不新增任务影子表、兼容列或状态双写。
- 统一入口：`enqueueBlueprintJobTx` 独占 job INSERT，固定 subject=`blueprint_convert`、event type=`blueprint.conversion.requested`、aggregate=`blueprint_job/{jobPublicID}` 和 `{jobId}` payload。
- 上传事务：blueprint queued 状态、原始 variant、normalize job 和 Outbox event 一起提交。事件失败使全部回滚；文件去重命中时不创建 job/event。
- 转换/重试：原 `enqueueBlueprintJob` 保留为开启/提交事务的薄 Wrapper；删除开关分支和 commit 后 direct publish。成功响应的 `jobId/status`、路由和状态码不变。
- 运行边界：始终启用的 dispatcher 负责 JetStream 或本地已注册 handler；本项没有修改 Worker lease/recovery、OSS 衍生物补偿或 retry blueprint 主体 update，分别保留给 OPS-014/015 与 BUG-078。
- 验证/部署：真实 PG 临时表验证事件 CHECK 失败整笔回滚；无永久 DDL、数据回填、前端迁移或旧 Core NATS 兼容期。

## OPS-009：Outbox 状态转换结果成为权威

- Schema：无变化，权威版本保持 generation 94；复用现有 `status/locked_by/locked_at/attempts`、死信唯一键和五分钟 stale lease。
- claim：批量 stale publishing reset 的数据库错误向上返回；选中行转 publishing 必须仍未 published 且处于 pending/failed，并命中恰好一行，否则整个 claim 事务回滚。
- publish：发布/本地执行成功后，仅当前 worker 持有 publishing lease 时可标 published；SQL 错误或零行返回 dispatch error，Published 不递增。
- failure：retry 与 dead 都以当前 worker lease 作 CAS。dead update 与 dead-letter upsert 是单 CTE 语句；任一阶段错误保持原 publishing。Failed/Retried/Dead 只在一个权威转换确认后增加。
- 调用/兼容：`DispatchBatch(ctx,limit)` 签名和调度周期不变，但持久化故障现在可观察地返回 error。没有公开 API、DTO、前端、DDL 或回填迁移。
- 验证边界：真实 PG 临时约束覆盖成功、RowsAffected=0 与三个写失败分支；Windows 无 GCC 使本批 Race 探测阻塞，最终总门禁仍开放。

## LEGACY-009：私聊离线邮件事务与本地化

- Schema：无变化，权威版本保持 generation 94；默认通知模板新增 `direct_message_email(sender,preview)`，通过现有 merged system setting 向全部启用 locale 暴露，不增加表列。
- 消息事务：新增 `createDirectMessageTx`，写消息、检查并更新一条 conversation、为离线收件人写 `notification.direct_message_email.requested`。aggregate 为 message public ID，payload 保存模板 key/values，trace 保存请求 ID。
- 通知生产：`enqueueNotificationTaskTx` 成为共享事务原语；非事务 Wrapper 始终写 Outbox，不再受 `NATS_OUTBOX_ENABLED` 控制或回退 `PublishTask`。应用已始终运行 dispatcher。
- Worker：email action 可按收件人 locale 渲染模板；SMTP/数据库错误返回。direct notification 的站内行、actor 与可选 email Outbox 同事务；follower email 同样在聚合事务提交，broadcast fanout 使用独立 Outbox Wrapper。
- API：私聊 POST 请求、201 `message` 与条件 `notificationQueued/suppressed` 形状不变。notificationQueued 现在精确定义为 event 已提交，不声称邮件已发送；在线会话无 event。
- 部署/兼容：无 DDL、数据回填、前端迁移或 Core NATS 兼容期。远程 generation 85 整库集成未重置；目标临时表测试验证 generation-independent 原子 SQL。

## LEGACY-011：任务 Envelope 成为唯一内部协议

- Schema：无变化，权威版本保持 generation 94；`nats_outbox` 已具备全部 Envelope 列，dispatcher 继续从列构造 v1 消息。
- Queue API：删除 `Client.PublishTask(any)`；保留只接收已编码 Envelope + event ID 的 `PublishEvent` 与数据库 claim 后的 `HandleLocally`。`UnwrapEvent` 新增 error 并严格要求完整 version 1 identity/time/aggregate/payload。
- 消费：local/NATS 两条路径都在业务 Handler 前拒绝非法消息；有效事件把 event ID 写 context 并只传 payload。JetStream 无效消息沿既有重投/死信，Core 无持久确认仍不是可靠任务事实。
- 生产者：follow、Mod metadata、蓝图通知与此前 AI/Mod/私聊/蓝图创建全部只写 Outbox。蓝图 queued Core/goroutine scanner删除；Mod metadata 改为事务 orphan/stale 恢复事件并将重复 CAS 视为成功。
- API/前端：无 HTTP DTO 或前端变化。内部旧裸 task JSON 是明确移除的开发期协议；外部 realtime broadcast 不受影响。
- 部署：无 DDL/回填/双读。部署时必须以同一代码代次替换全部内部生产/消费实例；当前仓库无外部 task producer 兼容责任。

## BUG-056 / LEGACY-012：NATS 设置完整权威与失败安全热切换

- Schema：无 DDL，权威 generation 保持 94。`system_settings['nats.config']` 仍保存加密 JSON；内部 `JetStreamConfig` 现在同时序列化 `ackWait/publishTimeout` duration，完整记录可无损重启。
- GET：`GET /api/v1/admin/config/nats` 新增 `outboxEnabled`、`realtime` 和 `jetStream.{enabled,stream,maxDeliver,ackWaitSeconds,publishTimeoutSeconds}`；密码/Token 仍只返回 `hasPassword/hasToken`，不回显秘密。
- PUT：上述非秘密字段全部必填；新增 `clearPassword/clearToken`。空秘密且未 clear 表示保留，clear 表示持久化空值，clear 与非空替换冲突返回 400。JetStream 开启时 NATS 必须开启，投递/超时范围在建立连接前校验。
- 失败：连接、JetStream、任务订阅或广播订阅准备失败返回 502；设置写入失败返回 500；运行时缺失返回 503。所有错误都发生在切换前并保留最后可用持久记录/运行时，前端只在 200 后显示“已保存并应用”。
- 前端：后台新增 Outbox、Realtime、JetStream 完整表单与两个显式清密复选框；纯 `buildNATSUpdatePayload` 锁定请求字段集合。关闭 NATS 会同步关闭 JetStream，开启 JetStream 会同步开启 NATS。
- 兼容：删除旧 JSON 的 `present`/环境字段回填和密码/Token 环境回填。开发期旧记录按整代/设置重建，不增加双读、双写或记录年代分支。

## OPS-001 / TEST-026：默认 JetStream 与可复现故障恢复门禁

- Schema：无永久 DDL，权威 generation 保持 94。嵌入式 JetStream 文件存储和 PostgreSQL Outbox/死信/审计表均为测试临时资源；没有重置或写入远程永久业务表。
- 配置：`NATS_JETSTREAM_ENABLED` 的新环境默认值由 false 改为 true；`.env.example` 明确要求 test/prod NATS 使用 `-js` 和持久 StoreDir。显式持久设置仍是完整权威，不存在环境强制回填。
- 消费协议：实际 durable consumer 新增指数 `BackOff`，保留 ManualAck/AckExplicit、AckWait、MaxDeliver 和 queue durable；30 分钟封顶只限制等待上界，不改变投递次数。发布仍以 event ID 作为 `Nats-Msg-Id`，只有 PubAck 后 Outbox 才进入 published。
- 恢复 API：管理员 dead-letter GET/POST 的路由和 JSON DTO 不变。replay 审计 JSON 参数改为 `bigint/text` 显式类型，日志记录错误与 commit 错误分开观测；publish replay 保留原 event ID，consumer replay 生成新 event ID，重复 replay 仍返回 409。
- 测试依赖：增加 `github.com/nats-io/nats-server/v2 v2.14.3` 作为直接测试依赖及其 tidy 后传递依赖。默认测试在随机 loopback 端口启动官方 Server，外部 `MCMODS_TEST_NATS_URL` 只在显式 opt-in 时使用。
- 前端：无组件或 DTO 变化；仅复核既有 NATS 完整配置 payload 的 9 项 Node 测试、全量 ESLint 与 TypeScript。

## BUG-075 / BUG-078 / OPS-014：蓝图有界 owner lease 与原子重试

- Schema：权威版本从 generation 94 升至 95。`blueprint_jobs` 新增 `max_attempts integer default 3 check 1..20`、`locked_by text`、`lease_expires_at timestamptz`。
- 索引：新增 `idx_blueprint_jobs_recovery(lease_expires_at,id) where status='processing'`；新增唯一 `idx_blueprint_jobs_active_operation(blueprint_id,operation,target_format) where status in ('queued','processing')`。保留既有 queue 索引和 Job ID/公开路由。
- claim：queued 或 lease 过期 processing 才能以新 run token 领取；attempt 原子递增。30 秒 heartbeat、progress、completion、failure 都匹配 token，lease 两分钟。活跃重复消息返回错误等待重投，完成消息幂等 ACK。
- recovery：启动立即、之后每分钟批量 100。预算内在同事务清 lease、queued 并建新的恢复 Outbox；耗尽转 failed。normalize 主体随 job queued/failed，convert 永不改主体；恢复事件失败回滚 job/主体。
- retry API：路由和 202 `{jobId,status}` 不变；failed 主体、normalize Job 与 Outbox 同事务。非 failed 或已有 active 返回 409，不存在返回 404，数据库错误返回 500。部分唯一索引同时保护转换/其他绕过路径。
- 兼容/部署：开发期 generation 94 数据库按整代重建，不双写旧 started_at lease；远程 generation 85 未修改。OSS 补偿协议已随后由 OPS-015 / generation 96 增加。

## OPS-015：蓝图派生 OSS 对象预登记与补偿

- Schema：权威版本从 generation 95 升至 96。`oss_files.status` 增加 `pending`；`blueprints` 增加 `normalized_file_id bigint references oss_files on delete set null`，规范对象不再只有无法反查身份的 key。
- Lineage：新增 `blueprint_job_artifacts`，保存 job/blueprint、attempt/role、file、bucket/origin endpoint/region/CNAME/object key 及 pending/active/abandoned 时间事实；job/blueprint 删除 SET NULL，file 删除 RESTRICT，数据库删除不能抹掉存储事实。
- 索引：pending 扫描使用 `(id,job_id,attempt) where status='pending'`；主体删除后的 active 扫描使用 `(id) where status='active' and blueprint_id is null`。单 job/attempt/role、file ID、object key 分别唯一。
- 写协议：先在 job token/attempt 锁下登记 pending，再 PutObject。normalized/cover/variant 引用、file/artifact active 和 job completed 在同一业务事务；key 固定为 job-attempt-role，不再使用随机 key。激活/Job CAS 任何零行或错误均不能提交部分结果。
- 补偿：执行失败和 stale lease 在 job 状态事务内批量 abandoned/deleted 并 enqueue OSS deletion；job/blueprint 被删除或出现非 processing attempt 时由启动/每分钟扫描补偿。删除目标使用上传时保存的 origin 配置，不依赖之后变更的公共 endpoint；Outbox 约束失败整笔回滚。
- API/兼容：公开 HTTP 请求、响应、路由和 variant/normalized key 消费协议不变；不保留先上传后写 active 的蓝图兼容分支。generation 95 开发库按整代重建，远程 generation 85 未执行 DDL；generation 96 完整空库门禁仍开放。

## BUG-081 / OPS-016：OSS 删除 dead、重放与新 Key 周期

- Schema：权威版本从 generation 96 升至 97。`oss_object_deletion_outbox` 增加 `max_attempts default 12`、`locked_by`、`failure_class`、`dead_at`、`replay_count`、`last_replayed_at/by`；status 增 `dead`，字段范围和失败分类为 CHECK 闭集。
- 索引：保留 ready/processing 索引；新增 `dead_at desc,id desc where status='dead'` 支持管理员列表；新增 `updated_at,id where status in (pending,processing) and attempts>=max_attempts` 支持崩溃后耗尽扫描。10 万 completed 行计划验证两个索引。
- Worker：claim/complete/failure 以唯一 token CAS；暂时错误最多 12 次指数退避，配置/认证/授权/无效目标直接 dead；过期耗尽任务在 drain 开始恢复为 worker_lost dead。dead 与 app log 同事务，持久化错误不会被吞。
- Key 周期：新 enqueue 命中 completed/dead 时重置本轮状态、attempts、lease、错误与完成/dead 时间；pending/processing 不抢占现有 owner。已 deleted 文件重复 tombstone 直接成功，重新 active 后的删除才开启新周期。
- API：新增权限保护的 `GET /api/v1/admin/infrastructure/oss-deletions?status=&limit=` 与 `POST /api/v1/admin/infrastructure/oss-deletions/{id}/replay`；metrics 新增 `ossDeletion.dead`。replay 仅 dead 可用，返回 202 `{queued,id}`，不存在 404、其他状态 409、持久化错误 500。
- 兼容/部署：用户删除接口和删除 Worker 启动方式不变；不保留旧无限 retry 或 completed 永不重置语义。generation 96 开发库整代重建，远程 generation 85 未修改；generation 97 空库安装仍受外部门禁。

## OPS-017 / LEGACY-014：资料图片迁址持久任务

- Schema：权威版本从 generation 97 升至 98。新增 `oss_rehome_jobs`，每个 mod 唯一，级联删除；保存 queued/processing/completed/dead、generation/claimed generation、attempt/max、owner/lease、失败分类、完成/dead 以及 replay actor/time/count。
- 索引：ready 与 recovery 仅包含 attempts 未耗尽的 queued/processing；exhausted 仅包含达到预算的活跃行；dead 按 dead_at/id 倒序。100k completed 历史下精确 claim 同时命中 ready/recovery，三个运维扫描分别命中 exhausted/dead 等对应索引。
- 生产者：模组创建自动批准、修订自动批准、人工批准三处从 commit 后易失 schedule 改为 commit 前 `enqueueOSSRehomeJobTx`。同 mod 冲突不丢请求，而是 generation+1；非 processing 立即重置新周期，processing 保留 owner 并由完成/失败 CAS 发现新 generation。
- Worker：应用启动立即扫描，之后每 15 秒；3 分钟 lease、2 分钟执行 timeout、8 次预算。claim/complete/failure 匹配 token；重启可接管 stale processing；永久错误和耗尽进入带原子告警的 dead。
- API/指标：新增 GET `/api/v1/admin/infrastructure/oss-rehomes?status=&limit=` 与 POST `/{id}/replay`；权限分别为 `admin.config.read/write`；`infrastructure/metrics` 新增 `ossRehome.dead`。公开模组请求/响应无变化。
- 兼容：删除进程 channel/Map/Once scheduler，不双写、不保留队列满日志分支。generation 97 开发库整代重建；远程 generation 85 未修改，generation 98 完整空库安装仍受外部门禁。

## LEGACY-015 / LEGACY-016：删除未发布兼容协议

- OSS 请求：`POST /api/v1/oss/uploads/direct` 删除 `preferMultipart`；服务端以 `sizeBytes >= 16 MiB` 唯一选择 multipart。返回 ticket、`method`、完成/abort 字段暂不变化。
- 举报 API：移除 `POST /api/v1/comments/{commentId}/reports` 及 reason/detail adapter；唯一创建入口为 `POST /api/v1/reports`。评论其他读取、回复、反应、watch 路由不变。
- 前端：管理导入、Mod 导出/目录导入、通用 OSS 上传、项目下载与统一举报六处不再发送 `preferMultipart`；统一举报对话框本身无需改路由。
- Schema：本批无 DDL，权威 generation 当时保持 98。开发期直接 breaking removal，不保留代理、双读或 feature flag；持久分片会话随后由 OPS-018 / generation 99 独立关闭。

## OPS-018：持久 OSS 分片会话与到期清理

- Schema：权威版本从 generation 98 升至 99。新增 `oss_multipart_sessions`，保存 owner、bucket/origin endpoint/region/CNAME、object key/upload ID、原始名称/类型/size/SHA/category/source、expiry，以及 active/completing/cleanup_pending/aborting/completed/aborted/dead 状态、attempt/max、owner lease、失败分类和 replay 事实。
- 索引：expired active、due cleanup、stale processing lease、exhausted active 和 dead 列表分别使用五个部分索引。100,000 条 completed 历史下五条生产等价查询全部命中对应索引。
- HTTP：multipart initiation 在 ticket 返回前登记；complete/abort 由数据库 token lease 串行，owner 与内容事实不匹配拒绝，相同终态幂等。complete durable 状态必须晚于最终对象 HeadObject size/SHA 验证，因此成功响应丢失可恢复，已由 lifecycle 清除的 upload 不会误标 completed。
- Worker：应用启动立即并每分钟扫描，批次 32、lease 两分钟、预算 12。到期/失败/崩溃会话调用保存的精确 origin Abort；暂时错误退避，永久/耗尽进入带原子告警的 dead，stale budget owner 归类 worker_lost。
- API/指标：新增 GET `/api/v1/admin/infrastructure/oss-multipart-sessions?status=&limit=` 与 POST `/{id}/replay`，分别受 `admin.config.read/write` 保护；`infrastructure/metrics` 新增 `ossMultipart.dead`。公开 multipart ticket、完成/abort 请求字段和 16 MiB 服务端选择不变。
- 部署：OSS 启用时 runtime 必须以真实 SDK 读取 Bucket lifecycle，并找到 Enabled、覆盖当前对象 prefix、无 tag/size/Not 过滤、1..7 天 AbortMultipartUpload 规则，否则保持初始化失败。开发期 generation 98 数据库整代重建；远程 generation 85 未修改，generation 99 完整空库门禁仍开放。

## BUG-001 / ARCH-001 / LEGACY-001：系统通知模板权威与显式故障

- Schema：无 DDL，权威 generation 保持 99。沿用 `system_settings['notifications.templates']`、`notifications.source_locale/template_key/template_version/template_params` 与 `nats_outbox`。
- 模板：新增编辑员审核批准/拒绝、作者认领撤销、审核完成、新关注者、账号封禁/解除、举报成立/不成立和目标处置 stable key；每个启用 locale 均有默认值，已有管理员配置通过 merge 获得缺失 key。
- 生产者：审计列出的八类调用方全部改为 template key/values；有业务事务的路径使用 `enqueueTemplatedNotificationTx` 原子写 Outbox。关注聚合在通知事务内按 recipient locale 渲染并将同一快照写 notification 与 email event。
- 故障：模板设置查询/解码、recipient locale、变量、通知/邮件 Outbox 与 SMTP 错误都返回。异步路径交给既有 retry/dead；同步 sender 返回并以 recipient/key/error 结构化记录，不包含 values。
- API：公开通知、未读与存储 DTO 不变；模板管理 GET/PUT 在读取失败时从空/伪成功改为 500。内部 `sendTemplatedNotification` 由无返回改为 `error`；删除无外部责任的 direct INSERT fallback，不保留兼容开关。

## BUG-010 / PERF-006 / PERF-007 / ARCH-009：公开合成权威与 O(1) cache version

- Schema：权威版本从 generation 99 升至 100。新增 `catalog_dataset_state(singleton PK CHECK true, version bigint CHECK >0, updated_at)` 并初始化 version 1；不修改 recipe/import 表或外部数据。
- 版本协议：canonical publication/archive/localization 与所有生产 import activation/rejection 在原事务中 `version=version+1`；零行或 SQL 错误失败关闭并回滚。公共 cache key 从全 revisions hash 改读 singleton。
- 读取协议：recipe type count/list、detail 和 render 使用 canonical definition 优先的统一 CTE；无 definition 才选 latest active import snapshot。canonical binding/template 直接组装现有 public layout，响应不暴露内部 entity/authority 字段。
- 批量：type list 的 canonical catalysts 与 import fallback 分别使用一个 `ANY(bigint[])` 查询，资源解析跨所有 fallback 批量；非法 JSON array/object 结构返回数据错误。
- API/兼容：路由、成功 DTO、pagination 和 Cache-Control 不变。手工 recipe 从漏计/404 变为正常公开；损坏 catalyst observation 从伪空 200 变 500。开发期 generation 99 数据库整代重建，远程 generation 85 未修改。

## BUG-011 / BUG-012 / BUG-013 / BUG-014：目录导入身份、治理、归属与解析边界

- Schema：无 DDL，权威 generation 保持 100。未知 registry 使用 `import.document.<24 hex>` kind，沿用 `resource_kinds` 与 `game_resources(kind_code,canonical_id)`；family 写为 `document`。
- 治理：resource、tag、recipe type、recipe 与 imported template 的 conflict activation 只接受当前 `placeholder`。自动观察仍写 snapshot；`archived` 不恢复。明确的 catalog editor publication 仍设置 active/清 archived_at。
- 归属：document、registry entry、block binding/entity 与 JEI category/index/template/recipe collection 必须精确命中 namespace revision；删除单 revision fallback 与静默 continue。错误 Job 使用既有失败状态/诊断，无新响应字段。
- 解析：imported 与 manual resource resolver 都先硬过滤 `resource_kinds.family=requested kind`，再排序 preferred revision/version/loader/source。成功 DTO 不变，无同 kind 时保持未解析。
- 兼容：这些是开发期内部 identity/导入语义修正，不保留旧 `import.document` 共享身份读取，也不回填 generation 85。完整 generation 100 空库安装仍由独立门禁跟踪。

## PERF-010 / PERF-011 / ARCH-011 / DEAD-002：Mod 导出公开索引分页与严格读取

- Schema：无 DDL，权威 generation 保持 100。Tag 列表复用 `(registry,canonical_id)` 唯一键，成员复用 `(tag_snapshot_id,raw_member_id)` 主键，text/binary/media 复用 `(revision_id,asset_path)` 主键；source ordinal 只存在于资产联合查询，不写入数据库。
- Tag API：`GET /api/v1/export-revisions/{revisionId}/tags` 支持 `limit`（最大 200）与 opaque `cursor`，返回 `{items,limit,hasMore,nextCursor}`；删除 `all=1`。`tag-detail` 的 `members` 同样最多 200 并返回分页元数据，保留总 `memberCount`。
- Asset API：`GET /api/v1/export-revisions/{revisionId}/assets` 的 `pathsOnly=1` 与完整模式都支持 `limit`（最大 500）和 opaque `cursor`，返回 `{items,limit,hasMore,nextCursor}`。路径模式跨三表去重，完整模式保留跨来源同路径行；`assets/content` 不变。
- Cursor：编码 revision、过滤 scope、末尾 key/source ordinal，最大 1024 字节；跨 revision、registry/query/tag、路径模式或损坏值返回 400。registry/document 公开列表也删除 `all=1` 的 10000 行分支，但其既有有界 DTO 本批不变。
- 错误协议：摘要、registry/document/structure、Tag/资产和 entry variant 的行迭代及 JSON shape 错误不再伪装为部分 200；正常成功 DTO 不变，损坏持久结构返回既有 500。文档导入无语义 `ordinal` 局部变量已删除，不新增持久字段。
- 兼容：站点仍在开发期，仓库没有 `/tags`、`/tag-detail` 或 `/assets` 索引调用方，因此直接删除同步全量协议，不做双响应、feature flag 或 `all=1` 兼容；完整导出如将来需要，应走独立受控能力。

## MAP-004 / BUG-015 / BUG-016：Mod 导出单一 ID、语言继承与审核审计

- Schema：无 DDL，权威 generation 保持 100。审核复用已有 append-only `audit_events` 与 aggregate 索引；`aggregate_type='catalog_import_revision'`、key 为 revision ID，metadata 保存 decision/note、前后 status/active、mod/version/namespace/source kind，actor/created_at/IP/User-Agent 使用表的权威列。
- ID API：registry/document/Tag/entry/resource source 与 blueprint material 成功对象删除同值 `entityId`，只保留 `publicId`；entry detail 与 tag detail 的 lookup query 从 `entityId` 改为 `publicId`。前端 `ModExportEntryDetail`、资源链接与 blueprint viewer 已同步。
- Locale API：entry detail 无 `locale` 时使用登录用户偏好，匿名使用 Accept-Language；合法显式 `locale`/`secondaryLocale` 才覆盖。路由和响应字段不变，空/非法值不再强制中文。
- Review API：PATCH `/api/v1/admin/export-revisions/{revisionId}/activate` 继续接收 `{status,note}`；note trim、最大 4096 bytes，超限 400。approved/rejected 成功响应不变；决策事实进入统一管理员活动日志，审计失败返回 500 并回滚 revision/导入同步/cache version。
- 严格读取补充：resource resolver 的 names、Job response 的 configured/detected MOD IDs 与 error detail 验证 object/array shape；损坏数据从空字段成功响应改为 500。无新客户端字段。
- 兼容：开发期直接删除旧 `entityId` 公共别名和 query，不保留双字段/双参数；第一方调用全部迁移并通过 production build。远程 generation 85 不回填，完整 generation 100 空库安装仍由独立门禁跟踪。

## BUG-017：导入内容版本锁与精确 placement 幂等键

- Schema：无 DDL，generation 保持 100。复用 `mod_content_versions` 行、`mod_content_sections(version_id,parent_id,ordinal)`、`mod_content_section_resources` 的 resource/ordinal/placement identity 唯一约束。
- 事务：内容同步入口先取得 `pg_advisory_xact_lock(hashtextextended('mod-content-import:'||versionID,0))`，再 `SELECT ... FOR UPDATE` active version；锁覆盖 binding、detail/localization、root/child section 和 placement 全部步骤，commit/rollback 自动释放。
- 冲突：placement insert 从无目标 `ON CONFLICT DO NOTHING` 改为 `ON CONFLICT(version_id,resource_id) DO NOTHING`。相同资源重入保持幂等；ordinal、identity、section/resource 主键之外的冲突返回错误并回滚。
- API/兼容：公开路由、请求与响应不变；并发激活从随机失败/成功漏项变为等待后顺序提交。无前端变化、无 feature flag；远程 generation 85 仅用于 session-local/rollback 验证，不执行永久写入。

## BUG-018 / REUSE-001：来源范围导入投影与单一分类表达式

- Schema：权威版本从 generation 100 升至 101。`mod_resource_version_details` 新增 `projection_source`（manual/import）、`import_source_namespace`、`import_source_kind`、`import_revision_id`，CHECK 强制 manual 无 scope/import 有完整 scope；新增 import-scope 部分索引。
- Reconcile：incoming revisions 先形成 scope/resource 集合；同 version+scope 中缺失的 import detail 归档并删除 import placement。当前 import detail 的 definition/icon/render/source revision 精确替换，import provenance locales 先删后建；manual detail 与非 import locale 不变。
- Manual ownership：resource reserve/publish 把 detail 标记 manual 并清空 import scope。新 import 与 manual 主键冲突不更新、不自动 placement；人工 layout/本地化不会被后续 scope reconcile 误删。
- Mapping：root section、loot child 与 placement 共用编译期 kind/loot SQL expression；受限 alias helper 替换静态 placeholder。没有新设置表、动态 SQL 输入或第二配置协议。
- API/兼容：公开请求/响应不变。开发期 generation 100 数据库整代重建，不为旧无 provenance 合并数据编造迁移；远程 generation 85 未修改，完整 generation 101 空库门禁独立开放。

## BUG-020：GitHub 加载器文本 token 边界

- Schema：无 DDL，generation 保持 101。
- 识别协议：GitHub topics/README 先按非字母数字边界分词；只接受 `neoforge`/`neoforged`/`neo forge`、`forge`/`minecraftforge`、`fabric`/`fabricmc`、`quilt`/`quiltmc` 的明确 token。复合 `neo forge` 消费两个 token，不自动追加 Forge。
- API/兼容：路由、请求和草稿 DTO 不变；`compatibilities.loaders` 继续返回同一四种显示名和稳定顺序。纯 NeoForge 文本从错误的 `[NeoForge, Forge]` 收敛为 `[NeoForge]`；只有文本另有独立 Forge token 才返回两者。
- 边界：本批不改变 Modrinth/CurseForge 的结构化 loader 规范化，也不处理外部次要请求失败、语言推断、版本选择或简单项目全文分类。

## ARCH-012 / BUG-073 / BUG-074 / BUG-121：外部导入事实、失败与确认边界

- Schema：无 DDL，generation 保持 101；继续使用现有 import job `status/error/result` JSON。
- 失败协议：Modrinth team/version、CurseForge description/download URL、Modpack authors/description 与 GitHub README 的错误不再吞掉。429/5xx/非法 JSON/16 MiB 超限进入既有 failed/error；GitHub README typed 404 明确代表不存在，可成功为空。
- 分类协议：简单项目 loader/category/feature/resolution/performance 只消费 provider category/section/loader/file metadata；Name/Summary/Body 只作为内容，不是结构化分类语料。
- 语言协议：两平台 Modpack Job result 的 `defaultLocale` 为 `und`。前端显示空的必选语言；正式 create/update 只接受八种可编辑 locale，服务端拒绝绕过，因此 `und` 不进入正式 modpack/revision。
- 选择协议：Modrinth 选择 listed release 的 primary `.mrpack`，CurseForge 选择 approved available release 的非 server/alternative/child `.zip`；均按发布时间/ID/name 确定性排序，无合格项失败。
- API：Modpack import `result` 新增只读 `importSelection:{provider,projectId,versionId,versionName,fileId,fileName,releaseType,publishedAt}`。编辑器展示该选择；提交 DTO 删除且后端 final normalize 清空，不成为可伪造的聚合字段。其余路由/DTO 不变。

## REUSE-004：共享 Provider Snapshot 客户端层

- Schema/API：无 DDL、路由或 DTO 变化，generation 保持 101。
- Modrinth：`loadModrinthProviderSnapshot` 是 project/version/team 的唯一 HTTP 流程，返回 typed project、versions、authors；完整性或请求错误不返回部分结果。
- CurseForge：`loadCurseForgeProviderSnapshot` 是 class+slug search、返回身份验证与 description 的唯一 HTTP 流程；author normalization 也只存在共享层。
- 调用方：Mod、Modpack、Simple Project 只消费 snapshot 并保留各自领域映射；源码门禁要求五类 endpoint literal 不得回到 Adapter，且六个 Adapter 调用各自共享入口恰一次。

## REUSE-005：前端共享任务轮询状态机

- Schema/API：无后端、Schema、路由、DTO 或公开函数签名变化，generation 保持 101。
- 内部协议：`waitForPolledJob(load,terminalStatuses,onProgress,signal)` 统一取消、load、progress、terminal 与 1200ms abortable delay；`waitForImportJob` 统一 token/API 绑定。
- 调用方：`waitForModExportJob` 与 `waitForCatalogImportJob` 仍是两个领域入口，只构造 export-imports/catalog-imports 路径；现有调用点无迁移负担。

## REUSE-007：申请附件共享文件采集边界

- Schema/API：无后端、DDL、路由、请求/响应 DTO 或 OSS 上传协议变化，generation 保持 101。
- 交互：编辑员申请与个人作者认领都使用既有 `FileDropZone`；拖入深度、accept、disabled、隐藏 input 和同文件重选 reset 只有一份实现。空 accept 保持此前任意附件类型合同。
- 领域边界：编辑员申请继续负责 10 个上限、顺序上传、`project-editor-application` source 和附件 ID 提交；作者认领继续负责 5 个/10 MiB 上限、顺序上传、`creator-claim` source、通知/去重和 proof file ID 提交。

## BUG-125 / REUSE-006：严格本地化响应边界

- Schema/API：无后端、DDL、路由或响应 DTO 变化，generation 保持 101。合法既有 response enum 完全兼容。
- 读取合同：共享 parser 接受五种 provenance、四种 localization review status 和三种 published/mutation review status；显式未知值、缺失 mutation status、空 locale 与畸形 localization 失败可见，不再默认 approved。
- 调用方：catalog editor/resource、resolved content 与 recipe editor 只保留各自领域差异映射；本地化 fields/provenance/review status 解析统一。可选对象顶层缺 status 返回 undefined，mutation 必须有明确 `reviewStatus`。

## BUG-124：公开短链前端请求生命周期

- Schema/API：无后端、DDL、路由或 DTO 变化，generation 保持 101；仍读取 `/api/v1/public-links/{publicId}` 的 `{target}`。
- 客户端合同：404/410 显示不可访问；网络、其他 HTTP、解析与空 target 进入可重试错误。每轮请求携带 AbortSignal，卸载/参数变化后禁止迟到导航和状态写入。
- UI：新增 loading/not_found/error 三态和中英文暂时失败文案；error 提供显式重试，非法 ID 与明确不存在不提供重试。

## LEGACY-021：删除旧全局资源 UI 路由

- Schema/API：无 DDL、后端或数据 API 变化，generation 保持 101；`/api/v1/catalog/resources` 继续是 catalog 数据端点。
- UI 路由：删除 `/catalog/resources` 重定向页；`/admin/global-resources` 是唯一治理页面。没有兼容 redirect、query 双写或旧名 Wrapper。
- 调用方：仓库导航原已指向后台页面，无迁移项。production manifest 从 59 个静态页降为 58 个并不再列出旧路径。

## DEAD-016：首页死翻译键

- Schema/API：无 DDL、后端、路由或 DTO 变化，generation 保持 101。
- 字典合同：en-US/zh-CN 同步删除 `home.subtitle` 与 `home.activityHint`；后台翻译键 flatten 目录和其他派生语言随权威字典收敛。
- UI：当前首页继续使用 `home.heroDescription` 及真实数据分区，无渲染调用方迁移，也不提供旧键 alias。

## LEGACY-022：通知 Outbox 接受阶段文案

- Schema/API：无 DDL、后端、POST/202 响应或队列协议变化，generation 保持 101；Outbox 与 NATS dispatcher 仍是两阶段。
- UI：en-US/zh-CN 成功提示改为可靠投递任务已接受，不再声称 NATS 已入队。最终 NATS 发布架构说明保持。
- 兼容：翻译 key `admin.notifications.queued` 与调用点不变，只修正值；其他派生语言使用主字典 fallback。

## STYLE-009：Mod 资料管理登录身份文案

- Schema/API：无 DDL、后端、授权协议或状态码变化，generation 保持 101。
- UI：en-US/zh-CN 的 `modContent.managerLoginRequired` 只列出已认证开发者、本站编辑员和管理员；删除误导性的 owner/所有者身份说明。
- 兼容与权限边界：翻译 key、`LoginRequiredState` 调用点、令牌检查和后端 capability 判断不变；前端不新增权限推断。

## BUG-123：请求可见的 UI locale

- Schema/API：无 DDL、后端 API、业务 DTO 或状态码变化，generation 保持 101。
- 浏览器协议：新增 `mcmods-ui-locale=<canonical-locale>; Path=/; Max-Age=31536000; SameSite=Lax`，非法或缺失值回退 `zh-CN`；locale 不含身份或秘密。
- 渲染：根布局按请求读取 Cookie，并将一个 `initialLocale` 同时交给根 `lang` 与 I18n server snapshot；客户端变更同步 Cookie 和根属性。共享根读取 Cookie 后，业务页面由静态预渲染改为按请求动态渲染，robots/sitemap 保持静态。
- 兼容：原 `mcmods-ui-locale` localStorage 值不再是权威；新的 `mcmods-ui-locale-sync` localStorage 项只用于跨标签通知。无双读或长期兼容分支。

## BUG-128：评论关注精确静音截止展示

- Schema/API：无 DDL、路由、DTO 或状态码变化，generation 保持 101；继续使用既有 `mutedUntil` 绝对时间、`mutedForever` 和 PATCH `mute` 命令闭集。
- UI：有效有限静音显示为 locale 格式化的精确截止时间；相对 1h/24h/7d 只表示新的显式写命令，不再充当读取状态。
- 故障语义：已过期截止显示未静音；非法时间显示独立无效状态；永久标志优先。没有从时间差猜测历史命令，也不新增重复 `muteMode` 事实。

## BUG-129：版本选择器禁用项不变量

- Schema/API：无 DDL、后端、DTO 或状态码变化，generation 保持 101。
- 组件合同：`disabledCodes` 成为集合变换不变量；单项、父级和压缩标签都只能增删 available 且非 disabled 的目标成员。
- 兼容：公开 props 与 `onChange(string[])` 不变；输出保持权威配置顺序并保留组外已有值。当前第一方无 `disabledCodes` 调用方，无数据迁移或双实现期。

## PERF-065：目录资源图标有限缓存

- Schema/API：无 DDL、路由、DTO 或状态码变化，generation 保持 101。
- 客户端缓存协议：资源引用+locale 的展示快照使用 5 分钟 TTL、128 项 LRU；同键 Promise 合并，失败立即删除。通用 `ExpiringPromiseCache` 默认容量为 64。
- 陈旧与兼容边界：无服务端 revision/event，故最大陈旧窗口为 5 分钟而非即时失效；调用方 props、placeholder 和请求协议不变，不提供永久旧 Map 双读。

## MAP-012：站点语言单一注册表

- Schema/API：无 DDL、路由、DTO 或 locale 字符串协议变化，generation 保持 101。
- 内部合同：`supportedUILocales` 派生 `UILocale` 与 `uiLocaleCodes`；内容编辑语言直接引用 codes，不再维护第二数组或从 client Provider 导入类型。
- 外部兼容：Minecraft locale alias 与 BCP-47 内容归一化仍独立；它们由 `Record<UILocale,...>` 对注册表保持编译期完整，但不与站点 code 列表合并。

## OPS-022：前端公开部署配置合同

- Schema/API：无 DDL、后端路由、业务 DTO 或状态码变化，generation 保持 101。新增的是前端构建入口合同，不把公开配置持久化为第二权威。
- 示例与可见性：`.env.example` 被 Git 明确放行，包含 API、SITE、可选 Yggdrasil 与可选 Iconfont URL/SRI 五项且无凭据。全部 `NEXT_PUBLIC_*` 都是构建期客户端可见值，README 禁止秘密并要求变更后重建/重启。
- 生产合同：API/SITE 必填、绝对 HTTPS、无 credentials/query/fragment；SITE 只能是 origin。Yggdrasil 可选，设置后遵守相同 URL 安全边界并输出尾 `/`；Iconfont URL/SRI 继续成对失败关闭。开发缺项才使用显式 localhost 默认。
- 消费迁移：`next.config.ts` 统一验证配置并生成 Yggdrasil header；robots/sitemap 读取同一个严格 SITE 解析器，不再回退到 `https://mcmods.cn`。现有 API 调用方无需 DTO 迁移，production build 负责在它们进入产物前验证公共根地址。

## BUG-130：作者目录 cursor 分页

- Schema：无 DDL，generation 保持 101；复用 `creators` 主键、现有 `(kind,review_status,lower(name),id)` catalog 索引和 popularity 表。100k 临时影子数据下 name keyset 命中该索引，无 offset 扫描或开发库持久写入。
- 请求：GET `/api/v1/creators` 新增可选不透明 `cursor`。它绑定 kind/query/sort/order/limit/用户可见性；畸形或跨 scope cursor 返回 400，已开始的 Typesense cursor 在索引不可用时返回 503 而不改变顺序。
- 响应：继续保留 `items` 与全量 `counts`，新增 `hasMore:boolean`、`nextCursor:string`。SQL 页用排序 tuple+ID 和一行 lookahead；Typesense relevance 页使用绑定的固定 page/limit。旧客户端忽略新增字段即可，未建立 offset/游标双请求参数。
- 调用方：作者目录与所有 `CreatorPicker` 入口共用 `loadCreatorPage`；前者每页 48、后者 40，显式继续加载并按 public ID 稳定合并。filter/query/sort/token/refresh 变化会废弃旧代次，后续作者和团队可完整遍历且不会混入前一查询。

## BUG-131：Creator 三能力与成员审计端点

- Schema：无 DDL，generation 保持 101；继续使用 `creator_team_members`、`creator_role_definitions`、`permission_audit_logs` 与 project ACL version trigger。没有新增第二关系表、待迁移列或 profile/member 双写。
- 详情响应：GET `/api/v1/creators/{publicId}` 删除含混 `canEdit`，新增 `canEditProfile`、`canManageMembers`、`canCreateRoles`。前者只由 admin/全局或 scoped creator edit 产生；成员能力只对 team 且要求 admin/member manage；职务能力只由 admin/role write 产生。第一方调用方已一次迁移，无旧字段兼容期。
- 资料请求：POST `/api/v1/creators` 与 PUT `/api/v1/creators/{publicId}` 明确禁止 `members` 字段并返回 400。正常省略时 snapshot members 为 nil，资料发布更新链接但保留既有成员；前端 `creatorProfilePayload` 以字段白名单同时覆盖新建和编辑。
- 成员请求：新增 PUT `/api/v1/creators/{publicId}/members`，请求为 `{members:[{creatorId,roleId,title}]}`，响应为 `{status:"updated",count}`。端点要求登录及 admin/member-manage，目标必须为 team，成员必须为 author、职务必须存在；成功关系直接为 approved，并以同事务不可变审计保存 public ID、职务、title、状态和顺序的 before/after。
- UI：团队详情按成员 capability 暴露 `/teams/{publicId}/members`；独立页面提交完整成员快照，按职务 capability 条件显示既有角色创建入口。profile editor 不再抓取角色、持有成员草稿或发送关系。新建 team 的成员关系在创建后独立提交。
- 回滚/兼容：开发期未发布旧 DTO，不提供 `canEdit` alias 或 profile `members` 兼容写。代码回滚不要求 Schema 回滚；已有客户端若仍发送成员会得到显式 400，而不是静默忽略或部分保存。

## BUG-132：项目作者关系审核 keyset 队列

- Schema：generation 101→102，新增 `idx_content_creator_bindings_review_page(status,created_at,id)`，与四状态过滤和稳定升序 cursor 完全同构。无列、约束或数据回填；远端 generation 85 未执行 DDL，开发库按 generation 合同重置后安装 102。
- 请求：GET `/api/v1/admin/project-authorship-relations` 保留 `status`（默认 pending），新增 `limit`（默认 50、1..100）与可选不透明 `cursor`。cursor 绑定 status/limit/用户 ID/author-team 权限视图；非法或跨 scope 返回 400，不支持 offset。
- 响应：保留 `items`，新增 `hasMore:boolean` 与 `nextCursor:string`。SQL 按 `created_at,id`，读取 `limit+1` 后裁剪；只有存在后页才编码最后保留行，终页返回空 cursor。pending、approved、rejected、revoked 使用同一协议。
- UI：管理面板每次载入 50 条，显式继续加载并按关系 public ID 去重；状态、刷新、审核后重载或新页请求会 Abort 前一请求并以 generation 禁止迟到写入。原 PATCH 审核、note 和卡片 DTO 不变。
- 计划/兼容：真实临时表 205 条分为 100/100/5 完整遍历；100k keyset 使用新索引且执行 0.088ms。旧客户端仍可省略新参数读取默认首个有界页，但第一方无 fixed-200/offset/旧 UI 双实现。

## BUG-133：公开项目详情错误状态边界

- Schema/API：无 DDL、后端、路由、DTO 或状态码变化，generation 保持 102；继续读取既有 Mod、content-project 与 modpack 详情端点，远端 generation 85 不需也未执行变更。
- 客户端状态协议：`useProjectDetailQuery` 统一 `loading/ready/not_found/error`；只把显式数值 404 映射为不存在，401/410/5xx、网络、解析及未知异常映射为可重试 error。异常存在 message 时作为错误反馈描述，不再落回 loading。
- 请求生命周期：三个入口的主请求均接收共享 AbortSignal；组件卸载、身份或对象变化会取消旧轮次并阻止迟到状态写入。Retry 建立新 attempt 和 controller；无旧 cancelled-bool/404-only catch 双实现。
- UI/兼容：新增共享 Retry 文案与三个领域 loadFailed 标题，明确 404 的既有 notFound 文案保持。Mod 本地化增强请求可独立失败并回退主 DTO；这是成功数据的降级展示，不改变主详情失败语义。
- 回滚/迁移：无持久数据、服务端调用方或第三方客户端迁移。前端三调用方已原地切换共享边界；代码回滚只恢复旧 UI 行为，不涉及 Schema 回滚或数据库重置。

## BUG-134：通用审核状态展示合同

- Schema/API：无 DDL、后端、路由、DTO、状态码或枚举变化，generation 保持 102；服务端和严格前端 parser 仍使用 draft/pending/approved/rejected 四态。
- 展示协议：新增 `reviewStatuses.{draft,pending,approved,rejected,protocolError}` 权威翻译闭集。默认展示不再借用 common action copy；目录资源、全局目录和 catalog 编辑器无需传 labels 即获得正确状态。
- 运行时边界：共享 resolver 对合法四态返回类型化 status/key；任何未协商值只返回 protocolError。Panel 与 Badge 分别以 red border 和 danger badge 失败可见，不建立 future-status alias 或静默 fallback。
- 定制兼容：既有 `labels.draft/pending/approved/rejected` props 保持并只覆盖已知状态；title、note、change request、revision/activity 等 Panel 合同不变。其他派生语言继承 en-US 新 key。
- 回滚/迁移：无持久数据、外部调用方或数据库重置。前端两个通用组件已一次迁移共享边界；回滚只影响显示文案，不涉及 API 或 Schema。

## BUG-135：公开项目详情 Tab URL 合同

- Schema/API：无 DDL、后端端点、DTO 或状态码变化，generation 保持 102；新增的是既有详情路径上的前端 query 合同，不创建新服务端页面路由。
- URL 协议：无参数规范化为 introduction；其他声明 tab 使用 `?tab=<value>`。Mod 支持 relationships/data 等十项，简单项目支持八项，整合包支持 mods 等九项；非法或其他产品专属值只回退 introduction。
- 导航行为：共享 Hook 以 searchParams 为唯一状态并用 push 写浏览历史；前进/后退触发 URL snapshot 重渲染。只修改 tab，保留无关 query 和 hash；默认页删除 tab，同完整 URL不重复入栈。
- 兼容/调用方：无参数和原有 changelog 深链保持；三个详情组件删除 `selectedTab`、`setSelectedTab` 和单值 `searchParams.get` 特判。按钮渲染、翻译 key 和详情内容组件不变，新增 aria-current。
- 回滚/迁移：无数据、服务端或第三方 API 迁移。前端可代码回滚但会恢复不可往返行为；不保留 URL/local state 双事实兼容期。

## BUG-136：皮肤/蓝图原子本地化资产修订

- Schema：无 DDL，generation 保持 102；复用 `content_subjects`、`content_localizations`、`content_revisions`、skin/blueprint 主表及既有审核表。远端 generation 85 无需且未执行迁移。
- PUT skin 请求：新增可选 `defaultLocale` 与 `localizations:[{locale,name,summary,contentMarkdown}]`。省略 localizations 保持原 metadata-only 行为；显式提供时必须规范、去重、全为可编辑 locale 且含非空默认名称，默认名称/summary 同步主体。响应形状不变。
- PUT blueprint 请求：既有 defaultLocale/localizations 聚合协议不变，新增可选 reason 并由第一方启用。title/description 继续随默认 localization 归一；响应 `{updated,reviewRequired,revisionId}` 不变。
- 修订/发布：skin snapshot 新增可选 defaultLocale/localizations/replaceLocalizations；旧 snapshot 缺标志时只更新 metadata。新 snapshot 与 blueprint 共用 `replaceOwnedAssetLocalizationsTx`，在主体 apply 的同一事务替换默认语言和发布集合；审核通过使用提交者作为 updated_by。
- 前端调用方：`localized-asset-editor` 从 N+1（一个主体 PUT + 每语言 PUT）迁移为每种 kind 一个 PUT；请求只含 editable、非空版本并保留 reason。单语言 owned `/content` GET/PUT 保留给独立内容编辑，不作为该保存工作流的兼容写路径。
- 故障/回滚：任何主体、内容 subject、删除、语言插入、修订、审核事件或 commit 失败均回滚整个请求。真实 PG CHECK 注入证明 skin/blueprint 失败后主体、默认语言、集合均不变；代码回滚无 Schema 操作，但新前端必须与接受聚合字段的后端同步部署。

## BUG-137：反滥用设置完整性与显式验证

- Schema/API：无 DDL、路由、JSON 字段、system_settings key 或默认值变化，generation 保持 102。GET/PUT `/api/v1/admin/anti-abuse/config` 继续返回/接受同一完整 Settings。
- 服务端验证：五级阈值分别限制 log≤100、moderation≤200、challenge≤300、temp-block≤500、deny≤1000 且保持顺序；trustedMinimumLevel 明确 0..1000。其余天/小时/分钟、相似度和策略边界保持既有范围。
- 行为兼容：合法配置无变化；此前通过 Validate 后被 Normalize 静默恢复默认的越界请求现在显式 400。这是失败关闭修正，不保留“成功但未按请求保存”的兼容路径。
- UI：完整渲染 enabled/emergency、六项评分与相似度、五项账户/内容窗口及每动作七项策略。控件标明单位/范围并设置 min/max/step；共享 validator 在请求前复核整数、全局边界、顺序、账号依赖和策略边界。
- 回滚/来源：无数据迁移或第二配置源；恢复默认仍调用既有 reset API，保存仍由后端记录管理员审计。代码回滚无需 Schema 操作，但会重新暴露不完整 UI 和静默归一风险。

## BUG-138：项目工作台详情身份状态协议

- Schema/API：无 DDL、后端、端点、query、JSON 字段或状态码变化，generation 保持 102。列表仍返回 ProjectList；详情仍返回 `{project,trend,days}`，URL 继续使用既有 `project` 参数。
- 客户端状态：详情由 idle/loading/ready/error 判别联合管理，非 idle 状态携带规范化 projectID 与 days。只有与当前选择和统计区间完全匹配的 ready 才能进入 ProjectAnalytics；error 不保存历史 DTO。
- 响应验证：完成请求时额外要求响应 `project.id` 和 `days` 与请求快照一致，不一致显示当前项目读取失败。每轮携带 AbortSignal，清理旧请求；取消与可见性绑定共同阻止迟到覆盖。
- UI/兼容：选择/区间变化显示明确 loading，读取失败显示 Retry；项目列表错误留在列表侧。合法既有服务端和 project 深链兼容，不继续兼容把旧项目或旧区间图表显示在新选择下的错误行为。
- 回滚/迁移：无数据或第三方调用方迁移，纯前端可回滚；部署无需数据库操作。回滚会重新引入旧详情泄漏风险，因此不保留双状态实现。

## BUG-139：服务器审核 keyset 队列

- Schema：generation 102→103，新增 `idx_minecraft_servers_review_page(review_status,created_at,id)`；无列、约束或数据回填。远端开发库 generation 85 未执行 DDL；当前开发合同要求重置后安装完整 103。
- 请求：GET `/api/v1/admin/server-reviews` 保留 status（默认 pending，闭集 pending/approved/rejected），新增 limit（默认 50、1..100）与可选不透明 cursor。cursor 版本化并绑定 status/limit；非法、未知字段、超长或跨 scope 返回 400。
- 响应：保留 `items`，新增 `hasMore:boolean` 与 `nextCursor:string`。SQL 按 `(created_at,id)` 升序读取 limit+1 后裁剪；只有存在后页才编码最后保留行，终页游标为空。各 item、附件 presign 与 PATCH 审核 DTO 不变。
- UI：管理面每次读取 50 条并以 ID 去重追加；状态变化、刷新、审核后重载和新页会取消旧请求并递增 generation。新增双主字典 loadMore；既有状态筛选、note 和审核动作不变。
- 兼容/剩余：旧客户端省略 limit/cursor 仍能读取首个有界页面并忽略新增响应字段；完整历史调用方必须分页。此协议不改变每页 item 的 proof/link/mod 装配，相关 N+1 继续作为 PERF-067 开放。
- 回滚：代码与 generation 103 必须同步；开发期回滚需重置到目标 generation，不能只删除索引后伪报 metadata。无生产迁移操作或双读路径。

## BUG-140：表情临时上传丢弃与过期协议

- Schema：无 DDL，generation 保持 103；复用 `oss_files.source/status/created_at`、`stickers.image_file_id` 与 generation 97 的 `oss_object_deletion_outbox`。没有新清理表或第二删除状态机。
- 上传 source：第一方从固定 `sticker-upload` 改为每次 `sticker-upload:<UUID>`。后端 validator 和维护任务同时接受旧 exact 与新非空前缀；只接受当前 uploader、active 文件作为 mutation 原图。既有通用 presign/complete DTO 不变。
- 新端点：DELETE `/api/v1/admin/sticker-upload-files/{fileId}`，要求 sticker.manage+sticker.upload。仅当前 uploader 的临时表情源可见；active 且未被引用时事务 tombstone 并入可靠删除 Outbox，已 deleted 重试幂等，其他来源/owner 返回 404。
- 过期：MaintenanceWorker 每 10 分钟清理 created_at 超过 1 小时的 active 临时表情源，每批最多 1000、FOR UPDATE SKIP LOCKED，并再次复核无 sticker 引用。数据库状态与 Outbox 同事务，物理 OSS 删除继续异步有限重试。
- UI：create 与有 replacement 的 update 共用 lifecycle helper。mutation 失败立即调用新 DELETE；cleanup 网络失败保留原 mutation 错误并依赖过期补偿。无 replacement 的元数据更新不创建或丢弃文件。
- 兼容/回滚：sticker POST/PUT 的 imageFileId、翻译、状态与响应不变；旧 source 文件可被消费/回收。回滚前应确认没有 active 新前缀文件或保留兼容识别，否则会失去过期收敛；无需数据库迁移。

## BUG-141：公共蓝图库 scope keyset 协议

- Schema：无DDL，generation保持103；继续使用 `blueprints`、`public_routes`、`content_popularity_stats` 及既有目录索引。远端开发库generation85未执行迁移或写入。
- 请求：GET `/api/v1/blueprints` 保留 q/sort/order/limit/offset，新增可选不透明 cursor。cursor绑定规范化q、sort、order、limit、用户ID和admin可见性；未知/损坏/超长/跨scope返回400。cursor与非零offset互斥，第一方不再发送offset。
- 排序：name、published、updated/collected、heat/relevance、downloads、favorites、rating+rating_count、views、comments 分别使用与既有 ORDER BY 同构的元组，内部ID为最终tie-breaker。asc/desc同步改变排序与keyset比较方向。
- 响应：保留 `items`、`limit`、`offset`，新增 `hasMore:boolean`、`nextCursor:string`；读取limit+1后裁剪，终页nextCursor为空。item、cover、uploader、requiredMods DTO不变。
- UI：蓝图库每页36；搜索提交或排序变化取消旧轮次、清空旧集合并替换首屏，继续加载去重追加。新增中英文loadMore；固定limit=60与第一方offset路径删除。
- 连接边界：列表主rows在批量requiredMods前显式关闭；OSS配置在requiredMods查询占连接前读取，保证MaxConns=1和池饱和时没有同请求嵌套取连接。无第二查询协议。
- 兼容/回滚：旧客户端可继续使用offset并忽略新响应字段；新cursor调用方必须保持scope参数。实时目录不提供跨页数据库快照，指标变化后通过刷新重建顺序；代码回滚无需Schema操作，但会恢复固定窗口风险。

## BUG-142：日志文件批任务逐项状态合同

- Schema/API：无DDL、路由、请求或响应字段变化，generation保持103。POST `/api/v1/log-shares/files`继续接受1..10个fileIds+retentionDays并返回207 `{items:[{fileId,status,publicCode,...}|{fileId,status:"failed",error}]}`；远端generation85无需且未执行迁移。
- 客户端任务：每个选择文件以name/size/lastModified identity建立任务，保存stage、uploadedFileId、share/error。整批扩展名校验在首个upload前完成；invalid项与合法项都保留原文件名和独立结果。
- 部分成功：上传逐项执行且异常只标记自身；所有成功ID进入一次既有批创建。响应按fileId合并；ready不重做，processing保留，failed/整体请求失败保留uploadedFileId供重试，协议缺项/未知状态失败关闭。
- 服务端权威：现有`createFileLogShares`继续逐项隔离并按输入顺序返回；现有active source+redaction version唯一约束与ready查询负责并发/重试幂等。本项不增加第二批事务、任务表或OSS补偿协议。
- UI/兼容：选择列表持续展示每项上传/创建/失败状态；部分完成提示成功/未完成数，重试明确不重复占储存。全部完成后仍清空选择并保留分享结果；paste、history、delete与公开预览不变。
- 恢复边界：浏览器页面生命周期内ID稳定；页面重载后原文件仍在个人文件管理，不持久化File对象、token或本地任务。代码回滚无需Schema操作，但会恢复首错中止和重复上传风险。

## BUG-143：项目目录权威排除query

- Schema：无DDL，generation保持103；复用mods/modpacks/simple_projects.slug和minecraft_servers.public_id的唯一身份及现有目录索引。远端generation85无迁移/写入。
- 请求：GET `/api/v1/mods`、`/api/v1/modpacks`、`/api/v1/content-projects/{projectType}`、`/api/v1/servers`新增可选`excludeSiteId`。前三类规范为合法slug；服务器允许合法跨目录slug并仅在等于public_id时排除。非法标识400。
- 查询：每端点count/list共用排除谓词；服务器动态参数同样在whereSQL形成后同时供两查询使用。非空排除禁用未包含该scope的search-index页，offset/page在PostgreSQL过滤后执行；空值保持既有索引路径。
- 响应：所有items/total/limit/offset或page/pages DTO不变。total已权威排除，不提供`excluded`提示或客户端减法字段。
- UI：每个单类型和复合类型请求都携带excludeSiteId；删除当前页过滤。复合页以每类已过滤total计算全局offset和limit，再按类型顺序拼接；一个slug在多个类型存在时各自排除。
- 兼容/回滚：旧客户端省略参数行为不变；新调用方不得再次本地减total。代码回滚无Schema操作，但新前端若配旧后端会收到未过滤集合，因此四端点与picker应同步部署。

## BUG-144：合成表definition只读回显合同

- Schema：无DDL，generation保持103；复用`recipe_definitions(recipe_id primary key, definition jsonb)`及对象类型约束。远端generation85仅用同名会话临时表验证，无迁移或持久写入。
- 读取：GET `/api/v1/catalog/recipes/{publicId}`字段与DTO不变，`definition`继续返回当前canonical JSONB；无canonical definition时返回空对象。
- 编辑：PUT同一路径允许省略`definition`并保留当前值；携带时必须完整等于当前服务端值，不一致返回409且不生成审核快照。normalization把重新读取的服务端值写入snapshot，发布和semantic fingerprint不使用未验证请求对象。
- 新建：POST `/api/v1/recipe-types/{typePublicId}/recipes`只接受省略或空`definition`；非空对象400。当前可视编辑器没有顶层definition写能力。
- 客户端：`preserveRecipeDefinition`用structured clone保留任意JSON嵌套并供mutation和保存后draft复用。加载含未知字段后只改本地化、版本、模板可表达绑定或候选时，顶层定义保持字节语义等价。
- 兼容/回滚：旧调用方省略字段安全保留；会固定发送空对象的旧前端在非空记录上收到409安全失败，需与后端同步部署。回滚无需Schema操作但会恢复空覆盖风险。未来可写高级编辑需另建版本化验证合同。

## BUG-145：资料编辑服务端能力合同

- Schema：无DDL，generation保持103；复用`mods`、项目作者/团队身份与既有RBAC判定。远端generation85仅用会话临时`mods`表验证，无迁移或持久写入。
- 读取响应：GET `/api/v1/mods/{siteId}/content-sections/{sectionId}/resources`与GET `/api/v1/mods/{siteId}/content-resources/{resourceId}`都新增`capabilities:{manageLayout,createResource,editResource}`。三个布尔值当前均由相同的`canEditMod`服务端事实计算；匿名、普通登录和非项目成员为false。
- 缓存：两个身份相关响应使用`Cache-Control: private, no-store`，并让`Vary`包含Authorization与Cookie。没有持久能力快照、共享缓存或另一个探测API。
- 写授权：资源PUT、创建和布局等既有写请求/响应、路由及状态码不变，继续以`requireEditableMod`为最终权威；能力字段不作为客户端可提交凭据，直接未授权请求仍403。
- 客户端：API parser只接受显式true，缺失/错误类型返回全false。资源详情与板块状态分别绑定site/resource/section/token；编辑、创建、设置和排序入口只按对应能力显示，删除`/editor`探测与token存在性推断。
- 兼容/部署：旧客户端忽略新增字段；新客户端遇到旧后端会安全隐藏入口但不影响公开阅读，故建议后端先部署再部署前端。代码回滚无需Schema操作，但会恢复能力猜测与跨身份陈旧入口风险。

## BUG-146：OSS批上传逐项草稿状态

- Schema/API：无DDL、generation、后端路由、请求、响应、状态码、额度或扫描语义变化，generation保持103。继续逐文件调用既有`/api/v1/users/me/oss/uploads/presign`与`/complete`，成功对象继续出现在个人文件列表。
- 客户端内部合同：共享batch task保存File、stage、稳定fileId、结果和错误；本地类型/容量分类先于OSS请求。处理器逐项继续，成功立即回调领域状态，失败保留File供按key重试，uploaded永不重复上传。
- 图库调用方：Mod、整合包和简单项目把每个成功record按fileId去重追加到现有`galleryImages` DTO；业务提交形状仍只发送既有gallery fileId集合。每批与草稿总量继续受32项上限保护。
- 资料图标调用方：32px/128px分别保存在既有`iconSmallFilePublicId`/`iconFilePublicId`字段，PUT/POST DTO不变。存在未完成task时前端禁用保存，防止部分新pair进入业务修订；retry只上传失败尺寸。
- 持久/恢复边界：成功ID由既有编辑器自动草稿及个人文件表持久恢复；失败File只保留在当前页面，不写本地持久存储。用户刷新后重选失败文件，已成功文件不会消失或需要重传。
- 兼容/回滚：仅前端内部状态升级，可独立部署，无调用方迁移或数据库重置。回滚不会损坏既有数据，但会恢复批失败后隐藏成功个人文件的错误体验。

## BUG-147：简单项目父项facet分页协议

- Schema：无DDL，generation保持103；复用`simple_projects(project_type,review_status,...)`与`simple_project_parent_refs(project_id)`等既有索引、父目标表和完整性约束。远端generation85仅用会话临时影子表验证，无迁移或持久写入。
- 新端点：GET `/api/v1/content-projects/{projectType}/facets/parents`，optional auth。query支持`limit`（默认50、1..100）、可选不透明`cursor`与逗号分隔`selected`（最多20）；非法/超长/跨scope cursor或列表400。
- 可见性/身份：facet集合只来自同projectType且approved或submitted_by当前用户的简单项目；key与原parent筛选完全同构为`targetType:slug-or-rawIdentifier`，label取目标主名称或原始标识。响应`private,no-store`并Vary Authorization/Cookie。
- 响应：`{items:[{key,label}],selectedItems:[{key,label}],hasMore,nextCursor}`。items按`lower(label),key`读取limit+1并裁剪；cursor绑定projectType、viewerID、limit和最后tuple；selectedItems不改变分页，只回显当前URL已选且仍可见的项。
- 前端静态边界：Minecraft版本继续用`/api/v1/minecraft/versions`既有配置；loader/category/feature/selector/license只从`simpleProjectConfig`与`licenseOptions`生成。目录当前items完全退出选项派生。
- 前端动态边界：仅addon加载父facet；请求代次/Abort防迟到，cursor页面按key合并，桌面/移动面共享。后页selected由独立回显保持标签；协议畸形失败关闭并在筛选组重试，主目录DTO与错误态不变。
- 兼容/部署：旧客户端忽略新端点并继续工作；新前端应在后端端点部署后启用，否则动态父项组会明确报错但目录仍可读。回滚无Schema操作，但会恢复页窗口事实源，因此不保留双派生路径。

## BUG-148：资料分类树重挂与可定位422协议

- Schema：无DDL，generation保持103；既有`mod_content_sections.parent_id/ordinal`、完整布局revision与发布事务不变。远端generation85未访问，无迁移、影子表或持久写入。
- 前端内部合同：新增一次预计算的category reparent policy，依据当前树生成depth/subtreeHeight。候选与执行共享`canReparent`，要求目标存在、非自身/后代且`parentDepth+1+subtreeHeight<=4`；合法执行才更新parent并规范化ordinal。
- 入口：父分类select和关系拖拽都只调用`reparentCategory`；select只列policy允许的目标。新增叶子、分类删除上提、资源移动和保存请求字段不变；当前树若缺父、重复、循环或已超深，policy失败关闭所有重挂。
- 请求/成功响应：PUT `/api/v1/mods/{siteId}/content-sections/{sectionId}/layout`的categories/resources/reason/baseRevisionId与成功mutation DTO不变。后端在临时category ID替换前按请求顺序验证完整树，最大分类深度仍为4。
- 错误响应：树错误仍为422，但新增`code:"content_layout_category_tree_invalid"`及`details:{field:"categories.parentPublicId",categoryId,parentId,reason,maximumDepth:4}`；reason闭集包括root_id_conflict、duplicate_category、parent_not_found、cycle、maximum_depth_exceeded。其他布局/资源错误继续返回既有`invalid content category tree or resource assignment`，不误报分类字段。
- 兼容/回滚：旧客户端可继续只读error字符串；新客户端可按code/details定位。前后端任一侧都不把UI判断当授权或持久凭据，服务端始终最终校验。代码回滚无需Schema操作，但会恢复泛化错误和两入口行为分叉，故不保留旧下拉路径。

## BUG-149：任务奖励币种code唯一性协议

- Schema：无DDL，generation保持103；`task_definitions.rewards`继续保存JSONB `{experience,currencies:{code:amount}}`，currencies active code查询、任务进度和发奖事务不变。远端generation85未访问。
- 前端候选/状态：每条奖励select保留自己的当前code，但禁用其他行已使用code；rename helper再次复核目标未占用，冲突返回null且不修改原对象。没有自动相加、覆盖或合并路径。
- 前端保存：POST/PUT前按trim+lower检查所有currency对象键唯一；失败显示双语错误且不发请求。合法新code移动保留原amount；新增奖励继续选择首个未使用币种，删除和金额编辑字段不变。
- 服务端解析：taskPayload为请求实现严格Unmarshal；在map覆盖前逐token读取原始`rewards.currencies`键，以既有`normalizeCode`检测完全重复属性及大小写/空白碰撞。随后程序化map路径由`normalizeTaskCurrencyRewards`再次拒绝碰撞，再检查正整数和active currency。
- 错误/兼容：重复code返回400 `{error:"task currency reward codes must be unique"}`，且发生于任何currency查询或task写入前；合法请求、成功TaskDefinition DTO、未知币种/非正金额错误不变。旧客户端碰撞请求需要修正而不能继续依赖静默覆盖。
- 回滚：无需数据迁移；已有合法JSON无需回填。若存量中存在仅大小写不同的键，管理员重新保存时会安全失败并需显式选择保留项；不自动决定金额合并规则。

## BUG-150：Markdown embedded文档身份与受控同步合同

- Schema/服务端API：无DDL、generation、后端路由、请求、响应、缓存或草稿持久化变化，generation保持103；远端generation85未访问。standalone `/tools/playground`继续使用既有个人Markdown草稿GET/PUT与localStorage回退。
- 前端Props：`ToolsPlayground`改为判别联合；`embedded:true`必须同时提供`documentId:string`、`value:string`和`onChange`，standalone分支禁止这些受控字段。编译期因此覆盖未来调用方，不能再次创建缺文档身份的embedded实例。
- 文档身份：11个embedded调用点都迁移为实体类型/稳定实体ID或draft scope/locale/字段组成的identity；没有locale的正文仍绑定实体和body。`mod-content-resource-editor`删除`key={selectedLocale}`卸载旁路，统一消费session合同。
- 同步语义：identity变化无条件采用外部value并重置旧插入会话；同identity的clean session接受异步外部值；dirty session保留本地markdown并更新竞争基线；外部值等于本地值表示父级确认并清dirty。只有`commitMarkdown`发布onChange，外部同步不回声。
- 兼容/回滚：无跨版本网络依赖，可随前端独立部署。旧页面代码没有新必填prop会在TypeScript构建期失败而不是运行时串写；代码回滚不需要数据处理，但会恢复locale切换/异步加载陈旧值风险。BUG-151的standalone并发版本协议不在本项兼容声明内。

## BUG-151：Markdown草稿revision与保存会话协议

- Schema generation104：`markdown_playground_drafts`新增`revision bigint not null default 1 check(revision>0)`、`save_session_id text not null default '' check(length<=128)`、`client_sequence bigint not null default 0 check(sequence>=0)`。每用户单行主键、content、updated_at和级联删除不变；开发期整库重置，不对generation85远端执行ALTER/回填。
- GET `/api/v1/users/me/markdown-playground`：有记录响应由`{content,updatedAt}`扩展为`{content,revision,updatedAt}`；无记录明确返回`content:"",revision:0,updatedAt:null`。revision是后续条件写基线，不以时间戳比较。
- PUT请求：新唯一形状为`{content,baseRevision,saveSessionId,clientSequence}`。baseRevision为非负整数，sequence为正整数，session ID为8..128位ASCII字母/数字/`-`/`_`，content仍有2MiB UTF-8上限；unknown/missing/非法字段400 code `MARKDOWN_DRAFT_INVALID`。
- 原子接受：同saveSessionId仅当incoming sequence大于表中sequence；不同session仅当baseRevision等于当前revision。接受后revision+1并替换session/sequence；首次插入revision=1且只允许base=0。全部在单条upsert的WHERE/RETURNING内完成。
- 成功/冲突：成功data为`{saved:true,revision,updatedAt,clientSequence}`。条件不满足返回409 code `MARKDOWN_DRAFT_CONFLICT`，details为当前`{content,revision,updatedAt,clientSequence}`，其中clientSequence回显被拒请求，便于客户端淘汰迟到响应。
- 前端迁移：每组件实例生成saveSessionId，coordinator发严格递增sequence并携带当前服务器revision；新保存Abort旧fetch。只接纳最新sequence且正文未继续变化的完成状态。409暂停autosave，用户明确选择server snapshot或以冲突revision保留本地后重试。
- 兼容/部署/回滚：严格请求使旧前端↔新后端和新前端↔旧后端均不可混用；这是避免无版本写继续存在的有意协调破坏，需同批部署并重置开发库。回滚代码也必须回滚/重置到generation103，不能仅降级一侧或保留双协议。

## BUG-003：Markdown草稿读取错误语义

- Schema：无DDL，generation保持104；BUG-151新增的revision/save session/sequence列与约束不变。远端generation85未访问，无迁移、影子表或持久写入。
- GET成功/缺失：`pgx.ErrNoRows`仍返回200 data `{content:"",revision:0,updatedAt:null}`；存在记录仍返回`{content,revision,updatedAt}`。响应新增`Cache-Control: private,no-store`，不改变认证要求。
- GET故障：任何非no-row数据库错误改为503 `{error:"failed to read Markdown playground draft",code:"MARKDOWN_DRAFT_READ_FAILED"}`且无data；客户端可稳定区分临时不可读与权威空草稿。PUT协议和成功/409合同不变。
- 前端：账号GET失败时`draftLoaded=false`，普通Save disabled且debounced autosave不启动；显示本地化保护说明和Retry。重试重新GET，只有成功或权威no-row才启用写入；内存文本不会在未知base下自动上传。
- 兼容/回滚：旧客户端面对503会显示请求错误，但若仍允许手动保存则不具完整保护，故建议前端与后端一起部署。代码回滚无数据操作，却会恢复故障时伪空/覆盖风险，不保留200空值兼容分支。

## ARCH-003：详情响应的修订公开ID错误语义

- Schema：无DDL，generation保持104；`catalog_entities.published_revision_id`、创作者发布修订关系、`content_revisions.id/public_id`和路由表均不变。远端generation85未访问。
- 成功响应：资源、标签、配方类型、配方模板、配方和创作者六类详情的`publishedRevisionId`字段名与类型不变；非nil内部修订返回公开ID，真正未发布的nil内部修订仍返回null且不执行反查。
- 故障响应：非nil内部修订反查发生no-row、连接、超时或Scan错误时，不再返回200并把字段伪装为null；整个Handler统一返回500 `{error:"failed to resolve published revision"}`，且不发送部分成功data。
- 调用方/兼容/回滚：正常客户端无需迁移，只按既有非2xx合同重试或报错。代码可独立部署且无需数据回填；回滚不涉及Schema，却会重新把数据库故障误报为“尚未发布”，因此不保留旧单返回值Wrapper。

## ARCH-004：用户文件列表与额度读取错误语义

- Schema：无DDL，generation保持104；`oss_files`、附件锁定关系、权限数值和OSS设置均不变。远端generation85未访问，无迁移、影子表或持久写入。
- 文件列表成功：请求参数、上限与数组中file/lock/access URL字段不变。`rows.Next()`结束后新增`rows.Err()`检查；只有完整结果流才返回200，流式中断返回500 `{error:"读取用户文件失败"}`且不发送截断数组。
- 额度成功：daily/total/single/allowedExtensions形状不变，daily与total继续返回source/stored用量及现有`usedBytes`口径。两个聚合现在由单一loader逐次检查；每日或总量任一Scan失败均返回500 `{error:"读取用户文件额度失败"}`，不返回默认零值。
- 兼容/回滚：成功客户端无需迁移，失败时按既有非2xx路径重试；上传执行端的额度强制不由本读取DTO替代。代码可独立部署且无需回填；回滚无Schema操作但会恢复部分列表/伪零额度风险。

## PERF-002：用户项目陈列 access-first 查询

- Schema：无DDL，generation保持104；`effective_project_access`、七类项目表、skin blob和`public_routes`不变，复用既有user/access与主键索引。远端generation85只创建session temp影子表/视图和10M scale IDs，连接关闭自动删除。
- 查询：GET `/api/v1/users/{id}/showcase`的project部分先按user读取并物化`project_type/project_id/is_developer/is_editor`，再批量主键关联Mod、整合包、简单项目、服务器、蓝图、皮肤、社区内容及route。删除全站公开项目UNION后的逐行双EXISTS。
- 响应：`developerProjects`、`editorProjects`及每项`entityType/publicId/name/summary/iconUrl/href/roles/updatedAt`不变；同项目双角色继续归developer组，七类公开状态、route存在性、更新时间倒序和100项硬上限不变。
- 性能/兼容/回滚：100k/1M/10M实际合成项目规模均只按权限ID做主键Index Only Scan，无规模表Seq Scan；客户端无需迁移。代码回滚无数据操作，但会恢复全站规模先于用户权限过滤的成本，因此不保留旧查询分支。

## PERF-003：followers/following 稳定游标与双向索引

- Schema generation105：删除旧`idx_user_follows_followed_created_at(followed_id,created_at desc)`定义，建立`idx_user_follows_followed_page(followed_id,created_at desc,follower_id desc)`；新增`idx_user_follows_follower_page(follower_id,created_at desc,followed_id desc)`。表、主键、外键和follow写协议不变；开发期整库重置，不对generation85远端执行DDL。
- GET `/api/v1/users/{id}/followers|following`请求：唯一分页形状为`limit=1..60`及可选opaque `cursor`；默认limit24。cursor绑定目标用户、followers/following方向与limit，并保存createdAt和对应用户内部ID。`page`、`pageSize`、畸形或跨scope cursor返回400 code `USER_CONNECTION_CURSOR_INVALID`。
- 成功响应：由`{items,page,pageSize,total}`改为`{items,limit,hasMore,nextCursor}`。item的`id/username/avatarUrl/signature`不变；查询按`created_at desc,user_id desc`，limit+1判断hasMore，nextCursor只在有后页时生成。列表不再执行COUNT。
- 前端迁移：公开followers/following保存服务端cursor历史以支持Previous/Next，页码只作本地位置标签；profile只在组件身份加载时读取一次，因此总数标签不会随cursor重复查询。`/users/me/blocks`仍使用独立page/pageSize/total协议，不受本项影响。
- 兼容/部署/回滚：公开列表前后端必须协调部署，不提供会重新引入深OFFSET的双协议。回滚需要同时回滚前端和后端，并将开发库重置到generation104；已有follow数据无需业务转换，但索引布局随generation切换。

## SEC-005：公开贡献明细当前可见性策略

- Schema：无DDL，generation保持105；change requests、content revisions、public routes及各项目状态字段不变。远端generation85仅创建session temp影子表，未迁移或永久写入。
- GET `/api/v1/users/{id}/contributions`及showcase内`contributions`：year/from/to/total/days/years/recentActivity/recentActivityTruncated字段形状不变；日历聚合不变。recentActivity仍最多100项并用第101项判断truncated。
- 明细策略：只返回当前route存在且匿名可公开枚举的followable项目；各类型复核当前审核/active/visibility状态，skin严格要求public而非unlisted。unsupported类型、无route、pending/hidden/deleted/private/unlisted目标不返回，即使历史request已approved或提交者曾有权限。
- 名称/链接：Name改为当前目标表名称并以route public ID回退，Href只用当前canonical path；不再读取revision snapshot、metadata targetLabel或aggregate key作为公开名称。因此重新公开显示当前名称，隐藏时不留下旧名称或空链接占位。
- 兼容/回滚：客户端无需字段迁移，但明细数量和名称会按当前治理状态有意变化。完整审计记录仍供有权后台使用；代码回滚无需数据操作但会恢复历史名称泄露，故不提供旧snapshot字段。

## DB-002：作者与团队关系差异同步

- Schema：无DDL，generation保持105。`content_creator_bindings`继续使用稳定业务唯一索引、`id/public_id/created_at/approved_by/approved_at`与四态status；`creator_team_members`继续使用三列复合主键、创建/审核时间及四态status。远端generation85只建立session temp影子表，无迁移和永久写入。
- 写入语义：两个同步器都按稳定键`FOR UPDATE`；已有请求项原位UPDATE，新项INSERT，有管理权时遗漏项原位转`revoked`，不再DELETE整组关系。既有批准元数据在撤销时保留，非approved重新批准时才写当前批准人/时间。
- 权限边界：没有对应项目作者/团队关系敏感权限时，快照中遗漏的approved/pending/rejected/revoked行全部保持；不同creator kind分别服从各自权限。团队成员同步的无权限分支也保留所有遗漏状态。
- API/审计：项目内容保存和`PUT /api/v1/creators/{publicId}/members`请求及成功字段不变；团队响应count仍等于请求现行成员数。`creator.team_members.replace`审计after现在保留被撤销行并标记revoked，公开成员/作品/权限读取继续只接受approved。
- 部署/兼容/回滚：当前客户端无需迁移；后台审核可继续用原关系public ID关联历史。代码回滚无需数据处理但会重新物理重建关系并丢失隐藏状态与稳定时间，因此不提供旧全删全插双路径。

## DB-003 / SEC-006 / TEST-005：创作者导入预览与角色授权白名单

- Schema generation106：`creator_role_definitions`新增ID10 `contributor`（display-only）与ID11 `maintainer`（permission_granting=true）；既有ID9 `leader`由授权角色改为display-only。ID1 owner与ID2 developer继续授权，序列基线由9升至11。因此外部导入可引用的内建授权白名单精确为owner/developer/maintainer。
- 数据生命周期：`POST /api/v1/creator-imports`不再开启数据库写事务、不再镜像外部头像、不再创建/复用成员creator或成员关系。它只读取provider配置、外部资料及匹配角色定义；取消或重复调用不会新增OSS文件、creator、审核、审计或额度记录。`resolveCreatorAvatarTx`只在请求带可信站内`avatarFileId`时保留头像URL，否则清空预览hotlink。
- 请求与状态码：导入请求`{kind,url}`、认证/权限、支持的Modrinth/CurseForge URL及成功200/供应商失败语义不变。供应商返回超过500名成员、空/超长名称或角色定义缺失会失败关闭；外部头像仅允许既有HTTPS受信主机并只作浏览器预览。
- 成功响应：根对象保留`kind/name/avatarUrl/links/members`，但`avatarUrl`是未持久化preview URL。每个member现在为`{kind,name,avatarUrl,profileUrl,externalRole,suggestedRoleCode,suggestedRole,permissionGranting,title}`；删除`creatorId/roleId/avatarFileId/createdMembers`等会暗示已落库的字段。
- 角色映射：仅完整、大小写无关词匹配owner/project owner/organization owner、developer/dev、maintainer以及展示角色闭集；未知、自定义、组合和截断至160 rune的外部角色回落contributor。Handler按建议code批量读取数据库`name/permission_granting`并要求全集存在，前端不维护第二份授权常量。
- 前端迁移：导入成功只填名称/链接，并保存外部头像与成员read-only preview状态；不把外部URL或任何成员身份写入creator保存载荷。头像区域标记“仅预览、需上传后保存”，成员列表同时展示外部角色、内部建议与“授予访问/仅展示”；真正成员创建/绑定继续进入独立成员管理页。
- 测试迁移：删除`Supporter -> developer`期望，改为contributor/false；增加明确三授权角色、展示角色、恶意组合、8000字符输入、Schema seed、AST无写调用、真实PG零creator行和前端DTO/UI负断言。测试不再以风险输出或实现行号充当安全证明。
- 部署/兼容/回滚：后端Schema与前端preview DTO需协调部署；依赖导入时自动返回站内成员ID的旧开发客户端必须迁移，不提供双响应。角色变更源于generation106并保留在当前108，开发库重置至当前代次；远端generation85未执行ALTER/seed或永久写入。回滚会恢复孤儿数据与权限提升风险，故不能只降generation或保留旧预览写路径。

## TEST-004：创建与导入零隐式授权测试合同

- 生产Schema：TEST-004本身无DDL；首次闭环为generation106，当前generation108的`users.auth_version/permission_version`、五类授权来源定义及授权投影仍不变。提交者字段继续只表达提交来源。
- 集成Schema：`database.InstallEphemeralSchema`仅在`MCMODS_RUN_DB_INTEGRATION=1`且pool `MaxConns=1`时工作。它把当前generation108 Schema完整物化为当前连接的临时关系，并把自定义函数声明/调用限定到实际`pg_temp_N`；`schema_metadata`也为临时表。`DropEphemeralSchema`显式删除会话临时关系和例程并重置search path，隔离测试以新连接确认public generation85未变且临时namespace零关系残留。
- 外部API：Mod、整合包、六类简单项目、Minecraft服务器与metadata import job的请求、成功响应、状态码和路由均不变；实际创建项目的既有编辑/管理Handler仍以授权投影判定，无授权提交者返回403。
- 内部依赖：服务器提交探测增加`minecraftServerProbeFunc`注入点，只有测试注入确定结果；生产`Server`未注入时仍直接使用既有`serverprobe.Probe`。这不是协议、配置或授权旁路。
- 回归合同：25分支矩阵必须在每次创建/导入后证明两个用户版本号不变、role binding全局总数不增、五类授权来源为零；实际项目还必须由对应管理面证明403。AST检查只禁止顶层入口直接写角色绑定，不能替代行为矩阵。
- 部署/回滚：生产部署没有数据迁移或客户端协调要求；集成夹具不由运行时调用。回滚测试会丢失完整调用图保障但不转换数据，故不以旧四函数字符串搜索作为等价兼容门禁。

## BUG-005：用户内容创建事实actor同步

- Schema：actor修复由generation106引入并保留在当前108。`sync_user_content_creation_fact()`由不存在于目标项目表的`created_by`改读`submitted_by`，并保留社区内容的`author_id`；事实冲突更新同步user_id，四个项目trigger监听submitted_by、社区trigger监听author_id。BUG-005本身无表、列、约束或索引变化。
- INSERT/UPDATE/DELETE：有权威actor的创建立即upsert事实；actor或审核态变化更新同一`content_type/object_key`事实；社区status软删/恢复更新`current_exists/deleted_at`；物理删除使用OLD行把同一事实标为不存在，不删除历史身份或创建时间。
- API/调用方：所有项目创建、审核、删除和用户统计HTTP协议不变，调用方无需迁移。既有用户主页/统计查询继续读取`user_content_creation_facts`，现在无需等待手工reconcile才看见四类项目。
- 集成隔离：行为测试复用TEST-004的当前generation108会话临时全Schema，仅在显式数据库测试开关和`MaxConns=1`下执行；结束显式清理。远端public Schema保持generation85，未执行函数替换、trigger变更或永久业务写入。
- 兼容/重置/回滚：当前开发Schema应按generation107整库重置；不在远端85做原地ALTER。回滚无需业务数据转换，但会恢复项目事实零写入和actor变更漂移，因此不保留`created_by`双读。
- 边界：人工reconcile增减/action_counts精确性不属于BUG-005，已随后由BUG-006独立关闭；ARCH-005统计读取错误和ARCH-006成长投影重放也已由各自独立变更关闭。

## BUG-006：可精确重建的用户活动统计基线

- Schema generation107：新增`user_statistics_retained_actions`，主键`(user_id,stat_date,action_id)`；保存正event_count、非负Markdown增删字节、first/last和可空last_comment_at。user删除级联、action删除受限，时间顺序由CHECK保证。复合主键同时支持单用户全日期重建，无额外索引。
- 删除捕获：`trg_retain_deleted_user_activity_statistics`是`AFTER DELETE ... REFERENCING OLD TABLE ... FOR EACH STATEMENT`触发器。每个删除语句先把old transition rows按非空user、UTC日和action聚合，再以加法upsert保留基线；不保存object type/route ID/正文，只有create-comment时间通过条件MAX保留。
- 校准事务：typed advisory lock后`SELECT users ... FOR UPDATE`，再同步内容事实、daily和totals。用户行锁与活动/基线FK的key-share冲突，确保同用户插入、删除基线提交与校准不会交错丢更新。
- Daily：raw事件与retained action聚合后按日期/action求和，派生所有专用计数、字节、完整JSON及first/last；冲突列全部精确使用excluded。权威集合没有的旧daily行在同一WITH语句删除，允许纠正过高计数和幽灵日期。
- Totals：从精确daily直接汇总数值，`jsonb_each_text`跨日合并动作分布；lastEdit与lastComment分别从raw及retained取最大值。冲突更新包括action_counts与全部可空时间，不用`greatest`保留错误旧值。
- API/CLI：`cmd/user-statistics`参数、processed输出和用户统计HTTP DTO不变；修复advisory参数类型后CLI能真实执行。原始活动清理仍按既有过滤/批次删除明细，聚合统计按既有“独立于raw保留”的产品语义继续存在。
- 部署/回滚：开发库必须重置到generation107；远端generation85未执行DDL或永久写入。回滚需整库回到106且会失去已清理事件的独立基线，不能只回滚reconcile或保留greatest双路径。ARCH-005/006仍为独立协议。

## SEC-008：action-scoped限制组合与缓存合同

- Schema：无DDL，generation保持108。`anti_abuse_restrictions`的user/IP/device、actions、mode、starts/ends/lifted字段和既有active user/IP索引不变；没有复制或物化第二套限制事实。远端generation85未迁移。
- Account读取：查询新增action参数，在active身份来源集合内保留global hard modes或`actions && [action,"*"]`。一个lateral aggregate返回最强hard mode及对应end、最大challenge end、最大moderation end；删除截断所有模式的`LIMIT 1`。
- 缓存：account cache key由user/IP/device扩展action HMAC；当前用户前缀保持不变，因此创建、解除和自动限制后的既有prefix invalidation仍原子覆盖全部action派生快照。TTL/Redis配置不变。
- 决策：hard mode继续产生既有`action_restricted`，permanent ban为Deny，其余hard mode为TempBlock。challenge和moderation可同时影响同一请求；challenge proof只满足challenge，不消除moderation。Decision JSON字段、code和状态映射不变。
- 管理API：创建/解除限制请求响应、允许action/mode闭集与时长边界不变。全局read_only/temporary/permanent即使actions为空也对任意action匹配；`*`按动作通配；其他模式必须由现有管理员校验提供至少一个action。
- 兼容/部署/回滚：无客户端或数据迁移，可随后端独立部署。回滚无需Schema操作但会重新允许新无关限制和跨action缓存遮蔽旧相关限制，故不保留单行profile兼容分支。ARCH-007错误可见性已由后续独立修复关闭。

## BUG-007：Yggdrasil Join原子提交合同

- Schema：无DDL，generation保持108。`yggdrasil_tokens`的active/expiry/last_used字段与`yggdrasil_join_sessions.server_id`主键不变；不新增双写表、中间状态或补偿任务。远端public generation85未迁移。
- 事务：进入事务后重新校验Token及关联状态，并以`FOR UPDATE OF t`固定Token行；Join upsert和Token时间更新均由tx执行，只在commit成功后返回204。任一SQL/commit故障返回503并整体回滚。
- 冲突：已存未过期同Token可重新Join；已存未过期异Token时`ON CONFLICT ... WHERE`影响0行，返回现403且失败Token不更新。PostgreSQL主键争用与事务边界确保同serverId只有一个胜者。
- API：Join JSON、selectedProfile/serverId校验、204成功、403无效/冲突和503服务故障协议不变。语义收紧为503必然不存在可被`hasJoined`观测的新Join事实。
- 部署/回滚：无数据或前端迁移，可随后端独立部署。回滚无需Schema操作，但会恢复失败响应与已提交Join并存的不一致，不建议保留。

## BUG-008：proof challenge首次写入完整性

- Schema：无DDL，generation保持108。继续使用`anti_abuse_challenges.metadata jsonb not null default '{}'`，不新增nonce列或第二张挑战表。远端public generation85未迁移。
- 写入：human challenge INSERT的列集新增metadata，值为`jsonb_build_object('nonce',$10::text)`。public ID、provider、token/answer hash、scope、IP和expiry与nonce是同一条语句的成功/失败单元；删除返回ID后的metadata UPDATE。
- 验证：`verifyChallenge`继续从metadata取nonce并以HMAC比对answer hash，成功后转为consumed。无法完整INSERT时不对外发放ID，不会将空nonce作为正常失败挑战累计风险。
- API：ChallengeInfo、proof prompt与`challengeId:answer`请求形状不变。发放失败继续使用现有moderation保守降级，不新增客户端状态。
- 部署/回滚：无数据、前端或Schema迁移，可随后端独立部署。回滚会恢复无错误检查的第二次补写，不保留。

## BUG-009：Mod资料分类树四层不变量

- Schema generation108：替换`validate_mod_content_section_tree()`，不新增表、列或索引。trigger在每次INSERT或`parent_id/mod_id/version_id` UPDATE前锁定内容version；跨version按ID有序锁，随后联合校验新父链、祖先环、后代高度及全子树mod/version作用域。
- 深度合同：资料root深度0，分类深度1..4；`new parent depth + current descendant height > 4`统一失败。直接第五层创建、单项审核重挂、整页保存内部写入与直接SQL不能绕过数据库权威边界；合法第四层保持可写。
- 批量迁移：`publishModContentLayoutTx`先为现有后代写入全树唯一暂存ordinal/system key并展平到root，再按已验证目标深度拓扑重建。移除项归档后恢复原system key；事务提交前不存在暂存值，任何约束/资源/修订失败整体回滚。
- API：整页PUT仍在写入前由`validateModContentCategoryTree`返回既有422 code `content_layout_category_tree_invalid`、`maximum_depth_exceeded`和`maximumDepth:4`；单项创建/审核请求响应形状不变，越界数据库写继续走既有冲突/发布失败语义。没有新增客户端字段或双协议。
- 部署/兼容/回滚：开发库从generation107重置到108，不能只替换应用而保留旧trigger。回滚需整库回到107且会重新开放带后代UPDATE旁路，不保留旧trigger兼容分支。远端public generation85未执行DDL或永久业务写入。

## BUG-021 / TEST-009：完整可达的统一审核队列

- Schema：无DDL，generation保持108。`contentReviewQueueQuery`的13来源UNION不再固定`LIMIT 2000`；当前用户可见性、精确筛选total、未筛选facets和稳定当前页在同一PostgreSQL statement中计算。没有新增队列表、物化视图或第二权威。
- 请求：`category/operation/projectType/q/limit/offset`名称与含义不变。limit仍默认50并限制1..100；offset接受完整非负int64。q仍查项目名、标题、用户名、project ID，改为按100个Unicode rune安全截断并使用字面`strpos`。
- 响应：继续为`{items,total,facets:{categories,operations,projectTypes}}`；item字段、reviewURL与reviewerScope不变。total现在覆盖全部匹配可见集合，facets继续覆盖筛选前的全部可见项；返回items最多100，不再在Go中保留整个队列。
- 权限：global内容审核可见所有来源；project.review可见对应project且不能处理自己的提交；需要global的catalog/localization/blueprint/skin/community/export项不泄漏给project reviewer。权限过滤在搜索、计数、分面和分页之前执行。
- 测试：保留完整UNION空分支Schema合同，新增generation108临时全Schema的2005项生产Handler行为矩阵，覆盖第2001项以后分页/搜索、精确分面、global/project/self边界及URL/scope。夹具结束后public generation85不变。
- 兼容/回滚：前端offset分页无需迁移。回滚无数据变换，但会恢复不可达尾部、错误total/facets和全量Go内存切片，不保留固定截断开关。PERF-012后续已以100k/1M规模计划独立关闭。

## PERF-012：统一审核队列规模门禁

- Schema/API：无新增生产DDL、索引、路由或字段，generation保持108；沿用BUG-021已发布的数据库过滤、limit最大100、非负int64 offset和`items/total/facets`协议。
- 规模合同：generation108临时全Schema的100k约束夹具验证真实列/外键/索引；1M会话TEMP夹具镜像reader所需列和aggregate/pending proposed-revision索引。两者均使用生产`contentReviewQueueQuery`与`EXPLAIN ANALYZE BUFFERS`。
- 计划门：100k/1M最深100项页面必须精确，单请求SQL为1，`content_revisions/change_requests`各自实际examined不得超过规模1.1倍；执行上限分别由共同10秒门覆盖。当前为541.056ms/5419.549ms。
- 运维边界：1M精确全局facet会产生约123114个TEMP写块，部署必须监控PostgreSQL临时文件和队列积压；这不是Go内存增长。若百万积压成为常态，应引入事务权威投影/计数并重新验证，不得用固定截断、近似total或客户端全量处理降级。

## BUG-022：精确项目审核的修订读取合同

- Schema：无DDL，generation保持108。历史SQL继续联接`content_revisions/change_requests`，以每条request的`status/submitted_by`作为审核态和提交者权威；没有复制审核身份、增加可见性列或建立第二套权限表。远端public generation85未迁移。
- 授权：`projectPendingReviewVisibility`统一解析项目编辑、全局审核与精确`project.review.<projectID>`。approved历史保持可读；精确项目审核只能读取同目标且`submitted_by>0`、非当前审核者的非approved行。权限模板、deny、空提交者、自审与其他项目不能扩大读取；全局审核者和项目编辑保持全部可见。
- Mod API：历史路由和`{items}`响应不变；比较请求`before/after`与响应DTO不变。内部查询新增提交者numeric ID但标记为不序列化；before与after逐项授权，任一不可见返回现有403。不存在/非法ID状态码不变。
- 其他历史：Modpack、simple project与changelog历史请求/响应不变，改为按各自public project ID传递同一visibility。pending对象的内部加载后仍有显式当前状态/提交者检查；跨项目审核者无法借editor加载旁路返回对象。Mod资料手工历史采用所属Mod权限，导入资料历史不向仅项目级审核者开放。
- 验证/部署：generation108临时全Schema的生产Handler矩阵覆盖Mod/Modpack/plugin/changelog、exact/self/cross/global及比较端；客户端无需迁移，可随后端独立部署。回滚无需数据转换但会恢复读取/处理权限不一致，故不保留旧全局-only分支。BUG-023错误语义与PERF-013分页不在本项中关闭。

## BUG-023：Mod修订审核的提交前响应构造

- Schema：无DDL，generation保持108。`change_requests/review_events/audit_events/review_completion_subscriptions`及OSS重归位任务仍由原审核事务写入；规范`modRevisionSelect`改在同一tx提交前读取，不新增response表、幂等键或补偿状态。远端public generation85未迁移。
- 事务/API顺序：批准/拒绝的业务写完成后，Handler先完整扫描并解码`modRevisionResponse`，失败返回500并回滚；对象有效后才commit，成功后直接返回该对象。成功200 DTO、请求`{status,note}`、审核权限、409已处理/过期基线及批准发布行为均不变。
- 失败兼容：过去的post-commit回读/解码错误被丢弃，可能返回字段部分有效但snapshot为零值的200；现在同类故障不提交并返回明确500 `failed to read reviewed revision`，客户端可以重试。没有“500但实际已审核”的兼容分支，也不在失败体泄露内部数据库错误。
- 验证/部署：真实generation108临时全Schema以不可解码数组snapshot证明旧200+已提交，再证明新500+pending/零事件；正常对象同时证明200响应与rejected/event提交。客户端无需字段迁移，可随后端独立部署；回滚无需数据处理但会恢复状态分裂。

## PERF-013：Mod修订历史keyset页与latest审核事件投影

- Schema/索引：无DDL，generation保持108。keyset直接复用`content_revisions(aggregate_type,aggregate_key,revision_no desc)`；request按`proposed_revision_id`唯一索引联接，latest event按`review_events(change_request_id,created_at,id)`反向取1。100k深页实际同时命中两个索引，无相关SubPlan或目标表Seq Scan。远端public generation85未迁移。
- 请求：`GET /api/v1/mods/{siteId}/revisions`新增可选`limit`（默认50，1..100）与opaque `cursor`。cursor绑定版本、规范project ID及limit，跨项目/页大小、未知字段、尾随JSON、非正revision或超过2048字符均400。排序固定revision_no降序，不提供offset/COUNT/all。
- 响应：保留`items: BackendModRevision[]`，新增`limit:number`、`hasMore:boolean`、`nextCursor:string`。只读取limit+1并丢弃探测行；终页nextCursor为空。item、比较和审核成功DTO不变，BUG-022的权限谓词继续在limit前执行。
- 查询投影：submitter用户与base revision使用LEFT JOIN；latest resolution event只用一次LATERAL，reviewer再JOIN该event actor。reviewer/note/reviewedAt必来自同一event；历史、单条、比较及BUG-023提交前响应共享这份投影。
- 前端迁移：`BackendModRevisionList`同步增加三个page字段；`mod-history.tsx`通过专用path helper请求50项，opaque cursor栈提供Previous/Next，旧请求用AbortController取消。最多保存两条完整选择，因此跨页勾选仍能生成before/after比较；当前DOM最多50行。
- 部署/回滚：后端与第一方前端需协调部署，不保留无界旧协议。构建使用仓库要求的生产HTTPS公共URL；无新增依赖。回滚无需数据操作但会恢复全集响应、多相关子查询与全集DOM，不能以客户端虚拟滚动单独替代服务端边界。

## SEC-012：OSS图片复用的owner与用途授权

- Schema：无DDL，generation保持108。继续使用`oss_files.object_key`唯一身份及现有`category/source/uploader_id/status/scan_status/content_type/size_bytes/sha256`事实；不把公共URL、桶ACL或clean状态物化为授权列。远端public generation85未迁移。
- 复用查询：`object_key`之外强制精确匹配目标`category`、受控import `source`和认证`uploader_id`；actor非正或用途为空直接失败。随后仍要求active、clean/trusted-generated、允许的栅格MIME、8 MiB上限，并从OSS回读校验size、SHA-256和完整解码。
- 调用方：项目图标和创作者头像继续通过`externalImageMirrorOptions`构造各自目标category/source，并将当前actor传入；站内公共端点快速路径和可信外部Host摘要幂等路径共享同一授权函数，不存在只按key的旁路。
- API/兼容：创建和导入请求/响应字段不变；同owner、同用途的既有对象可幂等复用，其他站内引用按既有错误处理。没有跨owner共享兼容开关；未来公开媒体复用需要独立显式授权协议。
- 部署/回滚：可随后端独立部署，无数据迁移。回滚无需Schema操作但会恢复已知object key跨业务复用风险，故不保留旧查询。

## REUSE-002：item/block系统分类共享事务初始化

- Schema：无DDL，generation保持108。继续使用`mod_content_sections`的父子、ordinal、active system key约束以及`mod_content_section_localizations(section_id,locale)`唯一事实；远端public generation85未迁移。
- 权威函数：`ensureItemBlockSystemCategoriesTx`唯一保存blocks/items的requested ordinal和en-US/zh-CN/zh-TW名称。缺失分类追加到active sibling之后，现存active分类/locale不覆盖；创建actor、root默认locale和display mode由调用方同行事实传入。
- 调用方：手工布局发布保持root创建后直接调用；导入同步在root集合生成后解析当前version的builtin item_block active root，再调用同一函数。无item/block root时跳过，数据库错误失败整个导入事务。
- 行为收紧：删除导入器按version扫描所有同名system key的本地化SQL；只有目标root的直接active子分类获得系统文案。导入顺序、资源placement和公开DTO不变。
- 部署/回滚：无前端或数据迁移，可随后端独立部署。回滚会恢复两套SQL及无关分类被本地化的漂移，不保留双实现。

## TEST-010：MODID确认绑定、单次消费与真实job投影

- Schema：无DDL，generation保持108。继续使用`catalog_import_jobs.modid_analysis_hash/modid_confirmation_required/modid_confirmed_at/modid_confirmed_by/run_token/updated_at`、`nats_outbox`及活动表；远端public generation85未迁移。
- Worker gate：读取与两种更新均以`id + run_token + mod_id`匹配。analysis hash仍绑定source hash和规范化configured/detected事实；同hash确认后续跑清required，变化hash重新暂停并清确认身份。
- HTTP事务：确认UPDATE继续绑定路由Mod、created_by、hash、status/required及24小时；queued转换、Outbox attempt和活动同事务，任一失败整体回滚。成功事件类型保持`mod.catalog_import.confirmed`，重放不产生第二事件。
- Job响应：`configured_modids`按生产PostgreSQL `text[]`直接扫描到`[]string`，不再按JSONB bytes解码；`detected_modids/error_detail`仍是JSONB并保持严格数组/对象形状。请求与响应字段名不变。
- 入口：MCMods Export ZIP和嵌入图标catalog导入继续在任何持久导入解析前调用同一pause gate；前端两个工作区仍通过共享confirm API提交hash，无新客户端协议。
- 部署/回滚：可随后端独立部署，无数据迁移。回滚会恢复错误Mod分析可改写任务及真实Schema确认后500读回，不保留旧路径。

## PERF-014：Mod详情关系组单查询装配

- Schema：无DDL、索引或持久投影，generation保持108。继续使用`mod_relationship_groups(mod_id,display_order,id)`和`mod_relationships(group_id,display_order,id)`事实；远端public generation85未迁移。
- 查询：详情loader从关系组出发，用单个`LEFT JOIN LATERAL`读取每组按`display_order,id`排序的关系，外层再按组`display_order,id`和关系顺序排序。指向站内Mod的关系只在目标approved时返回；文本目标不受影响。
- 装配：Go按group ID折叠联接行。LEFT语义保留原本无关系的组，也保留关系全部被可见性过滤后的空组；`direction: outgoing`与relationship字段形状不变。
- API/调用方：Mod详情请求和响应完全不变，无前端迁移。最多50组/200关系的真实Schema夹具证明关系查询数恒为1，替代旧`1 + group_count`往返。
- 部署/回滚：可随后端独立部署，无数据转换。回滚会恢复逐组N+1查询，不保留功能开关或第二装配实现。

## TEST-011：双项目编辑者关系所有权测试合同

- Schema/API/生产实现：无变化，generation保持108。继续执行SEC-013已收口的合同：incoming只由读取派生，写入snapshot只含当前项目拥有的outgoing关系；远端public generation85未迁移。
- 测试身份：真实PG夹具使用两个不同subject的claims，每个只允许精确`project.edit.<自身projectCode>`，显式断言不能编辑对方项目。目标编辑者的incoming伪造payload经过生产规范化与持久化入口。
- 数据断言：保存后来源项目原relationship ID和dependency事实保持，伪造source-to-target conflict不存在；既有矩阵继续保护对称关系、直接helper防御和pending目标可见性。
- 隔离/回滚：测试只在事务内建立两个Mod、关系组和关系，逐项rollback；不重置或迁移数据库，无客户端或部署协调。删除测试不会改变运行协议，但会丢失跨项目IDOR的持续门禁。

## BUG-024 / TEST-012：Mod资料条目Schema引用守卫

- Schema：无DDL、列或索引，generation保持108。`mod_content_templates.definition`仍是Schema权威，`mod_resource_version_details.entry_type_code`仍是持久类型身份；远端public generation85未迁移。
- 事务协议：内建更新和自定义最终publish都先锁template行，再由共享守卫以`SHARE ROW EXCLUSIVE`锁住sections/placements/details，验证resource kind、保留类型kind/字段存储签名和被删代码引用。已成为root页面的模板禁止物理删除条目类型；从未部署的模板允许删除。自定义整模板删除同样要求零section引用。
- 停用/读取：`enabled=false`保留code、kind和字段Schema。新详情选择与导入匹配仍失败关闭；既有详情编辑、发布及引用同步显式允许解析disabled类型。公开详情继续返回持久`entryTypeCode`、definition和当前完整schemaDefinition，无DTO新增。
- 错误语义：内建后台破坏性更新沿用409配置冲突；自定义自动发布/审核通过共享catalog reference/invalid错误并回滚整个revision发布事务。合法展示字段、名称、format和新增类型/字段不受影响。
- 测试/部署/回滚：generation108临时全Schema覆盖删除、停用、活动/归档引用、无引用、字段改型及custom publish/delete；无需前端或数据迁移，可随后端独立部署。回滚会恢复历史Schema失效风险，不保留双守卫或旧纯函数接受合同。

## ARCH-033 / PERF-062：后台权限读取完整性与批量协议

- Schema：无DDL、索引或数据迁移，generation保持108。roles/role_permissions/user_role_bindings/effective_project_access/creator_claims/user_permissions继续是唯一事实；远端public generation85未迁移。
- role读取：角色列表先完整关闭role cursor，再用一条`r.code=any($1)`查询全部permission entries。query、Scan、rows.Err任一失败令整个目录500；单角色读取复用相同批量函数的单code调用。无权限仍编码为非nil空数组。
- user读取：adminUsers的请求/响应和100行上限不变；先取用户集合，再以一次`resolveUsersRootPermissions`批量解析完整role图、项目访问、作者身份和直接权限，按ID回填RoleCodes。100用户页固定7条SQL。
- 错误状态：permissionCatalog和user权限读取形状不变；未知数据库状态不再返回200可编辑空数据。createRole只有识别出的唯一约束返回409，其他INSERT故障500；不向客户端泄露内部SQL错误。
- 部署/回滚：纯后端兼容收紧，可独立部署。回滚无需数据处理但会恢复角色N+1、单连接饥饿、部分权限列表和错误冲突映射，不保留旧逐项查询或fallback。

## TEST-049：后台权限写入跨层回归合同

- Schema：无生产DDL、索引或持久数据变化，generation保持108。测试在会话临时全Schema中使用真实`users/public_routes/auth_sessions/roles/permissions/*bindings/permission_audit_logs/runtime_versions`；故障CHECK随临时namespace销毁，远端public generation85未迁移。
- HTTP：生产请求/响应不变。测试通过真实ServeMux路径调用用户权限PUT与角色POST/PUT/DELETE，验证`group.* allow=false`为400、缺父级/角色环为400、轨道依赖删除为409、审计持久化失败为500，成功继续返回既有200/201。
- 权限事实：只有`source=manual`被替换；`level_track`、`account_status`、`user_preference`及各自source_key不变，manual角色/权限expiry精确保留。成功写推进permission version但保持auth version和原Session；角色继承与直接权限在同一Session下一次解析立即可见。
- 原子/并发/缓存：审计INSERT失败使完整事务回滚；两条并发替换最终只保留一个完整payload。Redis关闭时版本发布不可用，读取明确回落PostgreSQL并观察新权限，不以缓存成功作为数据库提交事实。
- 性能/部署：单个角色+单个直接权限完整写最多18条SQL；读取继续受100用户7条和100角色目录3条预算保护。仅新增测试，无部署协调或数据回滚；删除测试会丢失权限管理跨层回归门禁。

## OPS-021：负载观察命令的失败与配置协议

- Schema/API：无DDL、数据库数据、HTTP API或前端变化，generation保持108。命令仍只读当前数据库的`pg_stat_activity`，每250ms统计连接、active、lock waiter和慢查询峰值。
- 配置：`.env.example`新增`DATABASE_URL`、`MCMODS_OBSERVER_SECONDS=60`和空`MCMODS_OBSERVER_OUTPUT`。非空DATABASE_URL完整覆盖DB_*连接字段并决定reset effective name；duration合法范围1..3600，显式output父目录需预先存在。
- 退出协议：pool创建/Ping、任一采样/等待、JSON、0600文件写或stdout失败均返回错误并由main退出非零。存在采样错误时可保留诊断JSON但命令仍失败；显式output先成功落盘才向stdout输出。
- 调用方/部署：使用该CLI的本地或CI脚本必须只在退出0时接纳报告；无需应用部署协调或数据回滚。回滚会恢复缺报告仍成功和连接优先级不可见，不保留旧静默模式开关。

## TEST-050：负载结果与查询计划证据合同

- Schema：无生产DDL、列、索引或持久数据变化，generation保持108。真实反滥用计划夹具只安装在单连接会话临时全Schema，各表100k行；执行后删除namespace，远端public generation85未迁移。
- CLI报告：保留旧`clientErrors/serverErrors/networkErrors/statuses/antiAbuseCodes`字段，新增`expectedResponses/expectedRejects/successfulResponses/unexpectedResponses`和`contract`。`errorRate`现在是未预期HTTP加网络失败除以总请求并保留六位小数；合同失败令进程非零。
- 请求合同：普通请求默认只允许2xx/3xx；反滥用和爬虫请求分别声明允许状态及精确风控code。`MCMODS_LOAD_MIN_SUCCESSFUL_RESPONSES`、`MCMODS_LOAD_MIN_RISK_REJECTS`、`MCMODS_LOAD_REQUIRED_CODES`提供自动化阈值，均不把任意4xx当成功。
- 查询证据：通用psql脚本要求显式代表user/action/deep offset并使用`EXPLAIN (ANALYZE, BUFFERS)`；DELETE包在ROLLBACK事务。自动真实PG用例固定反滥用事件、指纹和限制三个目标索引并拒绝目标表Seq Scan。
- 兼容/部署/回滚：无应用端迁移；只消费旧字段的报告读取方继续可读，但必须迁移到`contract.failureReasons`和进程退出码才能成为门禁。既有JSON不重写，仅作人工历史样本；回滚会恢复统计误判与客户端分配噪声。

## SEC-045：前端CSP精确来源注册表

- Schema/API：无数据库、后端generation108、业务路由或DTO变化。前端Proxy仍把同一nonce CSP写入上游请求头和页面响应头；`default-src/script-src/style-src/object/base/form/frame-ancestors`既有语义保持。
- 来源变化：`connect-src/img-src/media-src/font-src`删除scheme级`https:`。connect只含self、API、Turnstile和部署allowlist；img含self/data/blob、API、受支持资源闭集及图片allowlist；media/font仅保留本地需要和各自显式allowlist。frame新增实际Turnstile并保留四个既有嵌入origin。
- 配置：新增`NEXT_PUBLIC_CSP_CONNECT_ORIGINS`、`NEXT_PUBLIC_CSP_IMAGE_ORIGINS`、`NEXT_PUBLIC_CSP_MEDIA_ORIGINS`、`NEXT_PUBLIC_CSP_FONT_ORIGINS`，均为逗号分隔、无秘密的精确HTTPS origin。路径、凭据、query、fragment、通配和生产HTTP失败关闭；开发HTTP只允许loopback。
- 调用方迁移：启用浏览器直传OSS的部署必须列出所有签名URL origin；新增CDN/媒体/字体时只赋予对应能力。未列出的任意用户外链资源将被浏览器CSP拒绝，不提供scheme级兼容回退。
- 部署/回滚：变量在构建期公开内联，修改需重建重启。无数据回滚；回退代码会重新允许任意HTTPS外连和资源跟踪，因此不得用回退代替补齐精确部署origin。

## SEC-046：Cookie Session上传恢复账号/owner协议

- Schema：无DDL、索引、回填或generation变化，保持108。任务沿用`catalog_import_jobs.created_by`，文件沿用`oss_files.uploader_id`，multipart沿用既有owner会话；远端public generation85未迁移。
- 浏览器持久化：`PersistedModExportUploadTask`新增本地`subjectId`，key从token payload改为`exporter:{user public ID}:{siteId}:{targetVersionId}`。这不是HTTP DTO；旧IndexedDB记录因缺subject不可恢复或删除，用户重新选择ZIP后生成新格式任务。
- OSS协议：新mcmods_exporter签名对象的实际key位于既有业务category下的`owners/{internal user id}`子路径；响应`category`仍为原业务category。`POST .../uploads/resume`只接受当前claims owner子路径，旧无owner ticket返回400；create/complete的请求与成功字段不变。
- Job API：`GET .../export-imports/active`只在当前`created_by`中选择活动任务，无匹配仍200 `{"job":null}`；`GET .../{jobId}`对同项目其他creator任务返回404。当前creator成功job DTO不变；worker和内部装配不受请求过滤影响。
- 部署/回滚：前后端应协调部署以避免新前端尝试恢复旧无owner ticket；无需数据操作，旧活动任务仍可由原任务执行器完成但浏览器不会自动接管。不得恢复共享`session`key或宽松resume前缀；重新选择文件是唯一安全迁移。

## PERF-001：编辑员申请列表恒定查询装配

- Schema：无DDL、索引、generation或数据变化，保持108。目标名称JOIN复用mods/modpacks/simple_projects/minecraft_servers/blueprints/skin_assets/community_posts主键；申请过滤继续使用现有review索引。
- API：项目侧及管理侧编辑员申请列表的路径、过滤、排序、成功状态和`items` DTO不变；`targetName`继续只显示满足原公开状态条件的名称，否则为空。附件字段仍为非nil数组。
- 查询协议：名称成为主列表SQL列，不再在Go rows循环向连接池逐项读取；附件保持一次`application_id=any($1)`。因此列表reader恒2条SQL，MaxConns=1可完成。
- 部署/回滚：纯后端兼容优化，可独立部署，无客户端或数据回滚。回退会恢复N+1、名称错误吞掉及单连接饥饿风险，不保留旧逐项helper分支。

## PERF-005：活动清理预览有界候选协议

- Schema：无生产DDL、索引、generation或数据变化，保持108。计划测试在单连接会话中创建并自动销毁TEMP影子表；生产执行继续使用现有action/time、object/time与BRIN索引。
- 成功API：`POST /api/v1/admin/activity-retention/preview`的200字段、确认文本、三类聚合、前100个用户组、10条样本与RFC3339 timestamp保持；matchedCount小于等于100000时仍是完整精确值，等于100000标记dangerous。
- 失败API：action-only全历史请求返回422 code `ACTIVITY_CLEANUP_FILTER_TOO_BROAD`；候选读到100001时返回422 code `ACTIVITY_CLEANUP_PREVIEW_LIMIT`及`maximumMatchedCount=100000`、`minimumMatchedCount=100001`。两类失败都不创建确认token或preview run，调用方必须增加user/object/object type/from/to边界后重试。
- 查询协议：一个`MATERIALIZED candidates`从活动表最多读取100001行，count、action/object/user聚合和sample全部复用；旧五次基础扫描helper删除。超过上限时不暴露部分聚合或任意样本，避免把截断数据伪装成完整预览。
- 部署/回滚：纯后端失败语义收紧，可独立部署且无数据回滚。管理前端应展示结构化422并引导缩小过滤范围；不得恢复无界兼容开关或在客户端假定超过100000仍会获得确认token。

## PERF-008：合成详情binding/candidate恒定查询装配

- Schema：无DDL、索引、generation或数据变化，保持108。查询继续使用`recipe_bindings(recipe_id,template_slot_id)`、`recipe_binding_candidates(binding_id,candidate_index)`及资源/实体主键；测试TEMP表只镜像所读列。
- API：目录编辑合成详情的路由、状态码与`bindings`对象完全不变。每个slot仍返回原binding definition和非nil candidates数组；候选字段、未解析raw ID、nullable probability、byproduct及icon URL规则不变。
- 查询协议：binding成为一次有序LEFT JOIN的驱动表，candidate/resource/icon/import信息同一结果集返回并在内存按slot折叠。零候选槽位保留，1至512槽位都只执行一条SQL；旧逐binding候选helper删除。
- 部署/回滚：纯后端兼容优化，可独立部署，无数据或客户端回滚。回退会恢复1+N往返及MaxConns=1自饥饿风险；不得以增大连接池代替恒定查询装配。

## PERF-009：活动revision模板推广恒定读取

- Schema：无DDL、索引、generation或持久数据变化，保持108。继续复用template snapshot的revision/type索引、slot的`(template_id,source_slot_id)`唯一索引和revision/type主键。
- API/事务：导出修订自动激活与管理员审核激活的HTTP请求、成功DTO和状态机不变。promotion loader仍要求revision active且status为ready/partial；完整读完模板/slot后才进入既有canonical write batch。
- 查询协议：template snapshot与slot由一条有序LEFT JOIN读取；零slot模板保留，slot按ordinal/id分组，nullable output index保持。旧按每个计算template ID再发查询的路径删除。
- 部署/回滚：纯后端兼容优化，无数据或调用方迁移。写SQL仍按真实模板/slot数量批处理，不能因“恒定读取”而删减；回退会恢复激活事务的1+N读取和锁时长放大。

## PERF-015：公开资料卡片游标、索引搜索与精简布局流

- Schema：generation109为`mod_content_section_resources`增加非空`search_document tsvector`和GIN索引。投影由数据库函数从canonical ID、活动详情本地化名称及活动ready/partial导入快照名称生成并限制为16,384字符；placement写入以及详情、本地化、导入快照、修订状态/删除、canonical ID变化触发事务内刷新。开发库重置到109；远端public generation85不迁移。
- 卡片API：`GET /api/v1/mods/{modId}/content/sections/{sectionId}/resources`改为`limit/cursor/q`，limit最大200，返回`hasMore/nextCursor`。游标绑定Mod/section/version/revision/query/limit/mode和最后稳定排序键；首屏返回精确total/categories，后续页沿用签入游标的total且不重复COUNT。offset、all、跨scope cursor、畸形cursor及非空但少于3个Unicode字符的q返回400。
- 搜索：q长度最大100个Unicode字符，数据库使用`search_document @@ websearch_to_tsquery('simple',q)`；COUNT和卡片读取共享GIN谓词。旧JSONB、本地化、导入快照前置通配与相关EXISTS搜索删除，不提供慢路径兼容开关。
- 精简流：公开`GET .../resource-graph`仅接受advancement根，分块最大1,000项并返回资源身份、层级、位置、图标、选定语言名称及最小进度definition；非进度根422。鉴权`GET .../layout`返回编辑器所需同类精简快照，与既有PUT同路径。图响应硬限1 MiB，编辑布局4 MiB；编码后超限会缩短items并产生继续cursor。
- 缓存与速率：匿名图为`public,max-age=60,stale-while-revalidate=120`，鉴权图/布局及卡片为private/no-store，并按Authorization/Cookie变化。昂贵读取使用IP和用户scope独立分钟预算：匿名卡片60、图30，鉴权额度加倍；布局基础60并按鉴权加倍。
- 前端迁移：普通卡片页只取首屏并用nextCursor追加；进度页面消费精简graph流并在本地过滤时保留祖先；布局编辑器/工作区消费鉴权layout流。删除旧20,000页的`loadAllModContentSectionResources`语义，不保留offset/all双协议。
- 验证/回滚：generation109临时全Schema、350项cursor遍历与搜索刷新、100k真实handler和1M TEMP索引计划均通过；前端84项测试、lint/typecheck与58页生产构建通过。部署要求前后端协调；回滚需同时恢复调用方和Schema generation，但会恢复深OFFSET、宽响应和匿名放大风险，因此不能作为兼容策略。

## PERF-016：资料引用候选批量解析

- Schema：generation110新增`idx_game_resources_kind_canonical_folded`、`idx_game_resource_aliases_kind_alias_folded`和`idx_catalog_tags_canonical_registry_folded`，分别服务kind+大小写折叠canonical、kind+大小写折叠alias及大小写折叠tag canonical/registry。无表、列、约束、回填或持久数据迁移；开发库重置到110，远端public generation85未修改。
- 内部协议：发布事务仍先删除当前`version.{id}.%`范围事实并读取活动根模板；reference字段最多1项，reference-list沿用同步前500个去重trim值。候选在内存按字段顺序拆为tag/resource数组，再由两个`unnest ... with ordinality`语句完成活动实体匹配与未命中批量写入。
- 解析语义：tag继续移除前导`#`、小写并按可选registry匹配，metadata仍含versionId和registry；普通资源继续规范kind并对canonical/alias大小写不敏感匹配。命中项不写未解析表；未命中项保留原raw ID及同一field path。tag冲突继续重置pending和resolved字段，resource事实继续由版本前缀替换。
- 查询/事务：同时含tag和resource候选时固定5条SQL，两类都无候选时固定3条；旧逐标识QueryRow/Exec删除。所有删除/解析/写入仍在调用方发布事务中，错误返回使完整发布回滚；没有异步最终一致窗口或双写。
- API/调用方：Mod资料编辑、修订审核、成功/失败DTO及未解析引用读取路径不变，前端无需迁移。未解析tag此前可能因多态metadata参数无类型而500；现在显式bigint/text，是失败关闭缺陷修复而非新兼容模式。
- 验证/回滚：四个500项字段真实PG由3,003条SQL/114.98s降为5条/1.115s，canonical/alias/tag命中和1,000未解析事实精确；空替换Rollback保留原事实。1M目录计划命中三folded索引，执行5.505/8.769ms且无目标表Seq Scan。回滚无需数据操作但会恢复N+1与tag失败，不得以连接池/超时配置代替。

## PERF-017：布局分类与临时身份集合协议

- Schema：无DDL、列、索引、约束或持久数据迁移，generation保持110。新分类内部ID仍来自`mod_content_sections_id_seq`，但由发布事务一次预留并在同事务显式INSERT消费；public ID仍由当前Schema的`new_public_id()`生成并进入全局registry。远端public generation85未修改。
- 准备协议：现有分类/相似组读取和四层树验证不变；临时category与首次出现的similar group先完整收集，再一次批量生成所有public ID。分类自身/父级、resource section和group ID在内存替换后才排序、重排ordinal并校验resource身份/布局语义。零临时ID不发生成查询。
- 发布协议：旧树继续先staging ordinal/system_key并临时挂root。按深度排序后完整预留新internal ID并构造parent映射；一个ordered unnest upsert新增/更新全部分类且要求影响行数精确。活动分类本地化一次范围删除、一次unnest插入；移除分类一次归档，旧system_key一次恢复。资源placement/advancement差异批处理和最终root更新不变。
- 兼容边界：布局PUT路径、snapshot字段、审核状态机、成功/错误DTO、1,000分类/20,000资源、四层深度、默认语言和相似组语义均不变。已有public ID和system_key稳定；临时ID仍只在提交准备阶段换成9位public ID，调用方无需迁移。
- 原子/约束：全部分类/本地化/placement/归档/system_key/root更新仍在同一advisory-lock事务；ordered upsert保留真实分类树触发器和唯一约束。不能把禁用触发器、分批跨事务或部分成功作为性能优化。
- 验证/回滚：64分类旧路径295 SQL/44.10s；1,000分类准备4 SQL、发布12 SQL/约1.050s，11,000临时ID 2 SQL。500更新+500新增、2,000本地化、父级/ordinal/system_key及深度归档和完整资源发布均通过。回滚无数据步骤但恢复逐项往返，不提供旧循环开关。

## PERF-018：公开项目目录卡片/详情分离协议

- Schema：无DDL、列、索引、约束、回填或generation变化，保持110。生产关联查询使用simple本地化`(project_id,locale)`与modpack兼容`(modpack_id,loader,minecraft_version)`既有主键；1M TEMP影子表只做计划验证且会话结束销毁。远端public generation85未修改。
- simple list API：`GET /api/v1/content-projects/{projectType}`的`items`从完整`SimpleProjectRecord`改为`SimpleProjectCard`。保留id/type/site/defaultLocale、最多2条locale/name/summary、缩写、版本/loader/分类/特征/selector、维护/源码/license、主图、最多8个name/role作者、最多3个最小父项目、审核态和时间；省略正文、provider/search/submission/revision/editor、链接、图库、头像/团队成员和父图标。新增可选`locale`，非法BCP-47返回400；缺省继承Accept-Language后回退zh-CN。
- modpack list API：`GET /api/v1/modpacks`的`items`从完整`BackendModpackRecord`改为`BackendModpackCard`。保留目录显示/筛选字段、最多16×32兼容项、32 tag、8个name/role作者及`hasGallery`；省略`bodyMarkdown`、pack编辑字段、links、galleryImages、mods、提交/修订/editor字段。`hasGallery`替代客户端用图库数组长度推断特征。
- 详情边界：`GET /content-projects/{type}/{siteId}`、`GET /modpacks/{siteId}`及editor/写入DTO不变，继续返回全部正文、链接、图库、完整作者/团队、父图标和内含模组。列表与详情不共享scanner/association loader，避免未来为了详情字段重新扩宽列表。
- 响应边界：两列表统一使用2 MiB原子envelope预算；编码后超限返回500且不先写200，不截断列表或total。simple每项语言2/作者8/父项3；modpack每项loader16、版本32、tag32、作者8；列表limit继续最大100。只有卡片主图生成访问URL，不再为省略的父/作者/内含模组逐项签名。
- 调用方迁移：Next16的simple目录、首页、资源选择器改用`SimpleProjectCard`并传当前locale；modpack目录/首页/选择器改用`BackendModpackCard`，图库特征读取`hasGallery`。详情/编辑继续使用完整Record类型。前后端必须协调部署，无旧宽DTO双读。
- 验证/回滚：真实generation110夹具覆盖100×8×1 MiB本地化、100×1 MiB模组包和单包2,000模组，列表均低于2 MiB且详情仍完整；1M精确生产关联计划执行1.148/214.210ms并命中主键、无目标Seq Scan。回滚无数据步骤但会恢复约800 MiB正文/200,000嵌套模组的匿名放大，不能以gzip、连接池、客户端忽略字段或超时替代投影分离。

## BUG-028 / PERF-019：稳定作者关系与合并副作用协议

- Schema generation：从110提升到111。表、列、索引、约束、种子和持久数据均不变化；变化仅是两个触发函数协议。开发数据库必须重置到111；远端public generation85保持原样且本次未迁移或写入。
- ACL协议：`bump_project_acl_runtime_version()`首次触发时以`set_config('mcmods.project_acl_runtime_bumped','1',true)`写transaction-local标记并递增`project_acl`；同事务后续三个关系/角色statement触发器看到标记后直接返回。提交或回滚后标记自动复位，下一事务可再次递增。
- 搜索协议：删除逐行`enqueue_search_creator_binding()`和单一row trigger；新增共享的`enqueue_search_creator_bindings_statement()`及INSERT/UPDATE/DELETE三个`FOR EACH STATEMENT` transition-table触发器。INSERT/DELETE读取对应new/old table，UPDATE合并两者，以subject type映射文档类型并对项目ID distinct后一次upsert `search_index_queue`。
- 写入协议：simple project与modpack继续调用同一`syncProjectCreatorBindingsTx`，但其读取既有稳定关系并跳过no-op；真实差异由一个unnest upsert提交，撤销更新原行。已有`idx_content_creator_bindings_unique`表达式稳定键继续作为冲突权威，无新索引或双写路径。
- API/调用方：路由、请求与响应字段、审核权限和前端调用均不迁移。共同规范化入口新增作者/团队合计64上限，simple/modpack均一致执行；超过上限返回请求校验失败，不截断或部分保存。
- 兼容/回滚：既有binding公开ID、时间和审批来源成为明确兼容合同；approved关系只有从非approved真实转换时才生成新审批事实。回滚无需数据变换，但会重新引入稳定身份丢失、O(n)关系写和全局副作用风暴，因此不保留旧实现开关。

## PERF-020：初始整合包单次关联投影协议

- Schema：无DDL、列、索引、约束、回填或generation变化，保持111。远端public generation85未访问，开发数据库无需因本项再次重置。
- approved创建：主`modpacks`行插入后直接创建approved content revision，不再以`revisionID=0`写临时关联；`applyModpackSnapshotTx`随后恰好一次设置published revision并写兼容性、标签、链接、作者、模组和图库。
- pending创建：继续在content revision前以`revisionID=0`写一次提交者可见的兼容/标签/链接/作者/模组预览；不调用publish apply，图库继续只在最终revision发布时物化。反滥用强制审核优先于`content.no-review`并由真实handler验证。
- API/调用方：POST `/api/v1/modpacks`请求、201详情DTO、pending/approved审核语义、2,000模组边界及错误码均不变，无前端迁移。approved详情仍立即包含全部关联和published revision；pending详情仍包含一份预览关联且没有published revision。
- 原子/回滚：主行、revision、一次关联投影与自动审核resolution仍在同一数据库事务；任一失败全部回滚。回滚无需数据处理但会恢复approved双写，不提供旧行为开关。

## PERF-021：服务器目录authority绑定游标与稳定搜索投影

- Schema generation：从111提升到112。`minecraft_servers`新增approved部分索引：`(updated_at desc,id desc)`、`(created_at desc,updated_at desc,id desc)`和`(lower(name),id)`。没有表、列、约束或持久数据回填；开发数据库必须重置到112，远端public generation85未迁移或写入。
- 搜索投影：Typesense projection schema从v2提升到v3。服务器文档新增`internal_id`、`created_at`、`updated_at`、`heat_sort_asc/desc`、download/favorite/rating/view/comment数值键；worker从`public_routes/content_popularity_stats`读取，缺失统计时为零。`content_popularity_stats`中minecraft_server route的INSERT/UPDATE通过新trigger写`search_index_queue`，索引版本变化按既有蓝绿集合/alias流程全量重建。
- API：GET `/api/v1/servers`请求保留q、tag、language、versions、mods、四个布尔条件、excludeSiteId、sort、order和limit；新增可选opaque cursor。成功DTO从`items,total,page,limit,pages`有意改为`items,limit,hasMore,nextCursor`。page、offset、跨scope/畸形cursor及sort=relevance返回400，不对旧数字页做静默翻译。
- 排序与游标：published、updated、name、downloads、favorites、rating、views、comments和默认heat均由完整稳定tuple+ID续页；versions/mods先规范化排序后进入scope。Typesense最多使用三个排序字段，rating规范为score/count/internal ID；name因字符串范围能力固定SQL。默认heat把微单位与online bit组合成整数键，升降序都保留同heat内online优先的既有可见顺序。
- 故障与权威：首屏在Typesense ready时使用派生索引，不可用时选择PostgreSQL keyset；游标签入`index`或`sql`。index续页期间索引失效返回503，SQL续页不因索引恢复而换源。SQL文本回退只用GIN兼容`to_tsvector @@ plainto_tsquery`，不含COUNT/OFFSET、name前置通配或文本OR；最终Typesense命中仍由PostgreSQL按ID回表并复核approved。
- 前端迁移：公开目录移除数字分页，按nextCursor追加并按server ID去重，筛选/排序/页大小变化清空序列；显示“已加载”数量而不伪称总数。资源选择器用scope绑定组合cursor同时携带legacy目录offset和服务器cursor，不再为每种类型发送limit=1总数预检；previous来自本地cursor历史，next只由hasMore决定。
- 部署/回滚：API和projection是协调部署；先重建v3集合并原子切alias，再部署同版本后端和前端。Typesense保持可选，业务写事务不依赖它。回滚需要同时恢复Schema generation、集合版本和第一方调用方，但会重引60万OFFSET、重复精确COUNT和组合慢查询，不作为兼容策略。

## PERF-022：服务器审核摘要页与按需详情协议

- Schema：无DDL、索引、约束、回填或generation变化，保持112。摘要页继续使用`idx_minecraft_servers_review_page(review_status,created_at,id)`；proof主键、link order索引和server-mod唯一键都以server ID开头，可支持当前页最多101项的三个count。远端public generation85只执行既有只读烟雾查询。
- 列表API：GET `/api/v1/admin/server-reviews`的status、limit最大100、cursor、hasMore/nextCursor不变。items移除`bodyMarkdown`、`proofText`、`proofFiles`、`links`和`mods`；新增整数`proofFileCount`、`linkCount`、`modCount`。其他身份、短摘要、分类/兼容判断、提交者、审核状态/说明和时间字段保持。
- 详情API：新增GET `/api/v1/admin/server-reviews/{serverId}`，与列表/PATCH一样要求`server.review`。成功返回原完整审核对象，并补三个与返回安全附件/链接/模组数组长度一致的count；非法ID 400、不存在404，任一主记录或关联读取错误在响应前500。PATCH同路径的方法区分、请求与成功DTO不变。
- 查询协议：列表主查询内以三个索引聚合产生count，handler读取limit+1后直接编码，客户端SQL恒1且不读取正文。详情只处理一个server：主记录、active clean/trusted proof、链接、模组及一次OSS设置固定5条SQL；模组图标继续遵循既有OSS访问规则，证明预签名仍走独立POST。
- 前端迁移：`ServerReviewSummary`只消费列表字段；展开按钮调用新详情GET并显示Markdown正文、证明文本/附件、链接和模组。详情按ID缓存，响应ID不匹配失败关闭；状态切换、列表刷新、收起或卸载取消在途请求。页面不自动并发预取每条详情，列表Link也关闭prefetch。
- 兼容/回滚：列表DTO收窄是协调破坏，后端与管理前端同批部署；没有旧宽items query、字段双读或`includeDetails`兼容开关。回滚无需数据操作，但会恢复最多301次远程SQL及100份大正文过取，不作为安全运行模式。

## PERF-023：项目文件按来源有界游标协议

- Schema：无DDL、列、索引、约束、回填或generation变化，保持112。站内页复用`idx_project_files_project_published(project_type,project_internal_id,status,created_at desc,id desc)`；开发数据库无需重置，远端public generation85未访问。
- 列表请求：GET `/api/v1/projects/{projectType}/{projectId}/files`现在必须带`source=internal|modrinth|curseforge`；limit缺省20、最大50，cursor可选。page/offset、缺失/未知source、越界limit、未知cursor字段、跨项目/来源/limit cursor都返回400，不对旧全量请求静默翻译。
- 列表响应：成功DTO为`items,source,limit,hasMore,nextCursor,versions,loaders,providers,warnings,canUpload,uploadPermission`。删除`totals.files`和`totals.internalDownloads`；versions/loaders是当前页有界facet，第一方UI与项目详情的canonical suggested values合并。整个API envelope在写出前受2 MiB原子预算，超限返回错误而不产生部分200。
- 来源语义：internal以created_at+内部ID keyset续页；Modrinth cursor携带下一version ID和version内offset，manifest最多一个项目/10,000 ID/2 MiB，version详情每批10、每请求最多5批、每version 32文件、每批128文件；CurseForge cursor携带官方zero-based index并请求pageSize=limit+1，单响应同样受128文件与2 MiB限制。上游不一致或越界失败关闭，不静默截断后声称完成。
- 下载语义：POST `/api/v1/projects/{projectType}/{projectId}/files/{source}/{fileId}/download`路径、请求和成功ticket DTO不变。internal继续单行读取clean OSS；Modrinth改为hash单version读取并复核manifest canonical project ID，CurseForge改为project-scoped单文件读取，均不再加载完整供应商文件列表。
- 前端迁移：下载面并发加载三个source页并保存三枚独立cursor；全部来源按仍可续页的来源并发追加，单来源只推进自身，按`source:id`去重并在已加载集合内按发布时间排序。筛选变化不伪造全局总数；上传或删除成功后重新建立三来源首屏。
- 兼容/回滚：前后端协调部署且不保留旧无source DTO、全量query或客户端一次加载全部的开关。回滚无需数据转换，但会恢复无界站内SQL、最多16 MiB Modrinth数组、500项CurseForge循环与全量内存排序，不作为可接受运行模式。

## PERF-024：项目更新日志摘要游标与单条正文协议

- Schema：无DDL、列、索引、约束、回填或generation变化，保持112。列表复用`idx_project_changelogs_target(object_route_id,review_status,event_at desc,id desc) where status='active'`，正文和语言标识复用`project_changelog_localizations(changelog_id,locale)`主键；远端public generation85未访问。
- 集合请求：GET `/api/v1/changelogs?targetType=...&targetId=...&locale=...`新增可选`limit`（缺省20、最大50）和opaque `cursor`。page/offset、越界limit、畸形/未知字段/跨目标、locale或limit复用的cursor返回400；每页使用limit+1 keyset且不提供total。
- 集合响应：新增`limit/hasMore/nextCursor`；items从完整`ChangelogItem`收窄为`ChangelogSummary`，以`bodyExcerpt`和`bodyTruncated`替代`bodyMarkdown/localizations`，category只含id/defaultLocale/name。每篇摘要最大4,000字符、availableLocales最大8，首屏categories最大100且每类names最大8；后续页categories为空并由客户端保留首屏元数据。整个集合envelope写出前受2 MiB原子预算。
- 详情边界：GET `/api/v1/changelogs/{id}`请求和完整DTO不变，仍返回bodyMarkdown及全部localizations供详情/编辑；其reader最多读取9语言并对超过8或单正文超过200,000字符失败关闭。POST/PUT、审核、历史和下载协议不变。
- 前端迁移：时间线保存nextCursor并增量追加、按ID去重；正文只渲染excerpt，截断时链接详情。请求代次阻止语言或目标切换后的旧页覆盖；编辑器继续先取单条详情并只从集合首屏取得分类。
- 验证/回滚：1M真实PG夹具两页各50项恰2 SQL、响应<256 KiB且不含大正文尾部；10万/100万深度计划各一次命中目标索引并执行0.643/0.629ms。回滚无需数据转换，但必须协调恢复前后端，且会重新暴露全历史×8×200k正文聚合风险。

## PERF-025：项目更新通知成功增量与event-first索引

- Schema generation：从112提升到113。新增`idx_notifications_project_update_event on notifications(project_update_event_id) where project_update_event_id is not null`；表、列、约束、外键和持久数据不变，无回填。开发数据库必须重置到113，远端public generation85未迁移或写入。
- Worker写协议：每条通知仍以`(recipient_id,project_update_event_id)`部分唯一索引幂等INSERT。批内只累计`RowsAffected()==1`的数量，进度UPDATE在同事务执行`notified_count=notified_count+insertedCount`并推进next_user_id与pending/completed状态；删除两处event COUNT子查询。
- 计数语义：notified_count是任务已成功创建的历史通知数，不是当前未删除通知存量。事务回滚同时撤销通知、cursor和delta；提交结果未知后的重放由唯一键与已提交cursor共同防重。event-first新索引保留给独立对账，不重新进入每200人的热路径。
- API/调用方：无HTTP请求、响应、前端、队列subject/payload、通知模板或实时广播变化；不需要协调客户端部署。OPS-004的失败attempt生命周期未在本项改变。
- 验证/回滚：真实PG以1M notifications验证137+63累计为completed/200；event=42的1,000项对账命中新索引、一次搜索、执行0.307ms。generation113完整会话临时Schema安装通过。回滚无数据转换，但需重置开发Schema且会恢复每批重复扫描风险。

## PERF-026：项目自动镜像流式文件与全局并发槽协议

- Schema：无DDL、索引、表、列、约束、回填或generation变化，保持113。跨实例协调使用PostgreSQL既有session advisory lock能力，不持久化业务行；远端public generation85未迁移或写入。
- 下载/哈希：最大文件仍为256 MiB。HTTP Content-Length先拒绝已知越界/不符；正文经128 KiB缓冲写入OS临时文件并同时计算SHA-1/SHA-256/SHA-512，最多读取limit+1。实际字节、声明size和供应商哈希必须一致；临时文件权限由`os.CreateTemp`提供且所有返回路径删除。
- OSS上传：PutObject的bucket/key/content type/content length和SHA-256 metadata不变；Body从内存`bytes.Reader`改为seek后的临时文件。数据库`oss_files/mirrored_project_files`继续记录同一size/digest；上传后的数据库补偿和DB-006内容唯一范围仍是独立Finding。
- 并发协议：所有实例共享4个`pg_try_advisory_lock(hashtext(namespace),slot)`槽；无槽时100ms轮询并受请求timeout/context约束。slot覆盖下载至OSS完成；unlock失败关闭底层session以自动释放，不返回被污染连接。最坏全站临时文件约1GiB、显式缓冲512KiB、数据库连接4条。
- API/调用方：自动更新设置、run任务、供应商/OSS接口、扫描提升、前端和错误响应无迁移。无兼容双路径。
- 验证/回滚：255.875MiB输入只产生138,192总堆分配并在0.60s完成；三哈希/超限/临时删除均覆盖。两个真实PG pool证明四槽跨实例且第五阻塞。回滚无需数据操作但会恢复无全局预算和256MiB单文件堆峰值。

## PERF-027：爬虫运行/候选摘要游标与单候选详情协议

- Schema generation：从113提升到114。新增`idx_seed_crawler_runs_created(created_at desc,id desc)`、`idx_seed_crawler_candidates_downloads(downloads desc,id desc)`、`idx_seed_crawler_candidates_status_downloads(status,downloads desc,id desc)`；无表、列、约束或回填。开发数据库必须重置到114，远端public generation85未迁移或写入。
- 列表请求：GET `/api/v1/admin/seed-crawler/runs`和`/candidates`接受可选`limit`（缺省30、最大100）与opaque `cursor`；候选继续接受封闭集status。page/offset、越界limit、未知status、畸形/未知字段/跨status或limit复用cursor均400。
- 列表响应：两类成功DTO统一为`items,limit,hasMore,nextCursor`且按limit+1决定续页，不执行COUNT。runs按`(created_at,id)`、candidates按`(downloads,id)`排他keyset；candidate items移除`payload`，active draft以单行LATERAL稳定选择，整个envelope受2 MiB原子响应预算。
- 详情边界：新增GET `/api/v1/admin/seed-crawler/candidates/{id}`，以外部项目ID读取一个候选的完整`payload`、状态、错误、时间和当前draft ID；复用`seed_crawler.view`权限，no-row为404。配置PUT、运行POST、Worker与草稿协议不变。
- 前端迁移：运行与候选保存独立cursor并按ID追加去重；只有点击候选行的详情动作才请求完整payload。新增中英文加载更多、查看详情和候选详情文案；无固定前100或首屏载荷预取兼容路径。
- 验证/回滚：真实PG 100k runs/1M candidates两页各2 SQL且无重漏；摘要<128KiB且不含1.2MiB测试载荷。三个keyset计划命中对应generation114索引并执行0.043/0.181/0.552ms；完整临时Schema安装到114。回滚需重置开发Schema并协调恢复旧前后端，但会重新引入不可遍历窗口和payload放大。

## PERF-028：通知双流游标与用户已读水位协议

- Schema generation：从114提升到115。新增`notification_read_watermarks(user_id PK,max_notification_id,read_at,updated_at)`；通知索引改为`idx_notifications_recipient_page(recipient_id,updated_at desc,id desc)`、recipient kind版本，以及broadcast partial的通用/kind版本。无历史receipt回填，开发数据库需重置到115，远端public generation85未迁移或写入。
- 列表请求：GET `/api/v1/notifications`接受可选kind、limit（缺省50、最大100）和opaque cursor。cursor绑定认证user ID、kind、limit并以`updated_at/id`续页；拒绝page/offset、未知kind、越界limit、畸形/未知字段/跨scope游标。
- 列表响应：旧裸数组替换为`items,limit,hasMore,nextCursor`。每个item字段保持，但actors最多3个；定向与广播各自limit+1再全局limit+1，不提供total，envelope受2 MiB原子预算。前端分类页保存cursor、增量追加并用请求代次拒绝旧响应。
- 已读协议：POST `/api/v1/notifications/read-all`不再为每条通知upsert receipt；它只upsert当前用户一个水位，并返回`read/readBefore`，删除不具可扩展语义的`updated`数量。单条POST `/{id}/read`及其receipt协议不变。读取与未读汇总先看显式receipt，否则用`id<=max_notification_id AND updated_at<=read_at`；聚合通知更新使用clock timestamp以重新变为未读。
- 调用方：消息中心协调迁移到页DTO、加载更多和`readBefore`响应；新增中英文加载文案。unread summary、周期/管理员对账和粉丝聚合读取水位谓词，但PERF-029的相关COUNT算法不在本项宣称关闭。
- 验证/回滚：1M真实PG中kind/无kind双流分别命中四个page索引并执行0.700/0.469ms；read max-ID一次反向PK搜索0.049ms。两页恰2 SQL、每页<128KiB；read-all 1 SQL后只有1水位/0 receipts，新插入与更新未读、二次read-all清零。generation115完整临时Schema通过。回滚会恢复O(N)写放大且需协调旧数组DTO，不作为兼容模式。

## PERF-029：广播单例事实与缓存派生抽样校准协议

- Schema generation：从115提升到116。新增`notification_broadcast_state`单例表，保存当前广播存量和历史最大广播notification ID；通知insert/delete/recipient null边界变化由同事务trigger维护。新增`idx_notifications_recipient_id(recipient_id,id)`与broadcast部分`idx_notifications_broadcast_id(id)`；不回填旧generation，开发数据库必须重置，远端public generation85不迁移或写入。
- 周期协议：`StartUnreadReconciliation`不再持有active user cursor或最终遍历全用户；每tick只从本进程TTL有效的未读缓存中按ID轮转最多16个候选。无缓存候选时数据库工作为零。配置batchSize只能进一步缩小该样本，不能突破硬上限。
- 真值协议：内部用户ID数组一次查询。定向通知和私信分别走user-leading索引集合聚合；无水位广播直接取单例live_count，有水位广播读取当前存活ID的较小前/后缀并补旧ID更新时间范围；显式receipt按用户作为稀疏修正。公开unread loader、周期样本及管理员显式校准复用这一实现。
- 缓存写边界：定向通知和私信成功提交后的增减、聚合/read-all按用户失效及广播创建全局epoch保持；广播删除补齐epoch提升。单条read仅在共享逻辑未读谓词成立时写receipt/递减，水位已读的幂等点击不改变计数。
- API/调用方：GET unread summary及POST管理员reconcile的请求/响应均不变；管理员仍最多100个public ID并允许显式修复，不恢复隐式全账户循环。无前端或实时事件协议迁移。
- 验证/回滚：100k/1M/10M真实广播下16用户各恰1 SQL，精确结果且notifications零Seq Scan，执行0.559/0.503/0.505ms；generation116完整临时Schema验证broadcast insert/delete和recipient双向切换。回滚需整代回到115且会恢复广播×用户重复COUNT，不提供运行时双模式。

## PERF-030：私聊会话与消息双游标、持久摘要协议

- Schema generation：从116提升到117。`direct_conversations`新增nullable `last_message_id`并以外键引用`direct_messages(id) on delete set null`；新增low/high成员页索引`(user_*_id,updated_at desc,id desc)`，删除旧全局updated_at索引。消息索引改为`(conversation_id,id desc)`，另增`(recipient_id,conversation_id,id) where read_at is null`并删除错配的conversation/created_at索引。
- 未读投影：新增`direct_conversation_unread_counts(conversation_id,user_id,unread_count,updated_at)`复合主键表。消息insert/delete和conversation/recipient/read_at变化触发器在原事务精确增减、清除零行；无需每个会话COUNT。开发期整代重置，不做旧消息回填；远端public generation85未迁移或写入。
- 会话请求/响应：GET `/api/v1/messages/conversations`接受可选limit（缺省30、最大100）和opaque cursor。page/offset、重复/越界参数、畸形/未知字段及跨user/limit cursor返回400。旧裸数组替换为`items,limit,hasMore,nextCursor`，不提供total；成员两侧各limit+1后合并，最近消息和未读均联接持久事实。
- 消息请求/响应：GET `/api/v1/messages/conversations/{id}`接受limit（缺省/最大100）和用于更早历史的opaque cursor；cursor绑定认证member、conversation和limit，以内部ID排他续页。成功从裸数组改为相同页DTO，items仍按ID正序。cursor不能与`after`组合；page/offset、重复参数及越界limit均400。
- 实时兼容边界：`after=<messagePublicId>`保留第一方实时刷新用途，并保持无效锚点返回空的现状供BUG-045独立修复；有效锚点改为读取最早100条新消息并返回hasMore，前端循环继续直到排空，避免超过100条突发跳失。历史cursor与after互斥，非法组合和重复参数400。
- 写入投影：发送消息和离线邮件Outbox仍在一个事务；INSERT返回内部message ID后，以单调ID条件更新`last_message_id/updated_at`，旧事务晚提交不能回退会话摘要。显式user导航从公开profile保留一个非首屏选中会话占位；加载更多与发送后的权威摘要按ID去重替换。
- 验证/回滚：真实PG 100k会话/1M消息的两类两页均每页一条SQL且无重漏；三项计划0.302/0.119/0.066ms。generation117完整临时Schema验证未读触发器和last-message级联；前端138 tests、TypeScript、ESLint与58页production build通过。回滚需协调恢复前后端数组DTO并重置Schema，会重新引入无界会话、相关COUNT和不可达旧历史，不提供双协议。

## PERF-031：实时查询键与客户端请求合并协议

- Schema/generation：无DDL、索引、回填或迁移；generation保持117，远端public generation85未访问。
- 服务端协议：SSE事件类型、ID和data维持原状；`message.created.data.conversationId`、通知事件`data.kind`及`unread.changed`继续作为事实。全部HTTP路径、参数、状态码和DTO不变，无前后端破坏性部署顺序。
- 客户端内部协议：去重后的SSE直接进入`realtimeQueryCoordinator`，不再派发`mcmods-realtime`或`mcmods-unread-change` DOM事件。查询键以认证user为scope，并分别细分通知kind和当前conversation；订阅在组件生命周期内注册/注销。
- 合并/一致性：同tick相同键只flush一次，相同callback跨键也只调用一次；相同键并发读取共享一个Promise和短新鲜缓存。失效以generation阻止旧成功/失败响应覆盖，当前会话在加载期间收到的新事件以sequence触发一次后续增量读取。
- 调用方：Header与消息中心共享unread summary；消息中心的通知、会话和当前消息各只订阅对应键。本地通知read/read-all显式失效未读；当前私信读取后的服务端`unread.changed`只刷新未读，不再循环刷新消息/会话。
- 验证/回滚：100+100消息/未读事件风暴只调用1次会话、1次当前消息和1次共享未读；100通知事件只调用1次当前kind和1次共享未读。回滚无需数据操作但会恢复跨组件重复请求；OPS-007、BUG-046与TEST-023不随本项关闭。

## PERF-032：有界API访问日志摄取协议

- Schema/generation：无DDL、索引、回填或数据迁移；继续写既有`app_logs`，generation保持117，远端public generation85未访问。
- 请求热路径：`logAccess`不再调用同步`writeAppLog`。Handler结束后只创建有界`accessLogRecord`并非阻塞入队；满队/关闭时即时失败，业务响应与数据库访问日志成功无耦合。request activity派生保持原成功后路径。
- 队列/批写：单进程容量1024、批上限128、flush间隔100ms、数据库批超时1秒；`jsonb_to_recordset`一次INSERT整批并保存原请求完成created_at。字段硬限把最大驻留内存保持在有限MiB量级；失败批不做无界重试。
- 取舍：全部4xx/5xx和普通状态变更访问保留；普通成功读取1/16采样；成功Presence、SSE、unread、翻译轮询、review lock和view事件丢弃。显式管理/治理业务日志及独立权限、登录安全事实不进入本best-effort队列。
- 可观测API：GET管理员infrastructure metrics新增加法`accessLogs`对象，包含入队、两类策略丢弃、溢出、成功/失败记录与批次、写失败、当前队深和容量；其他公开及管理请求/响应不变。
- 验证/回滚：真实PG排他锁下64请求1.0424ms返回，释放后4 SQL/64行；32普通成功GET为30 sampled、2 flushed、1 batch。回滚无需数据操作但会恢复每请求一次写及2秒反馈等待，不保留同步fallback开关。

## PERF-033：管理日志稳定游标、专用搜索投影与ID批清理协议

- Schema generation：从117提升到118。`app_logs`、`permission_audit_logs`、`user_login_logs`、`oss_upload_logs`增加`search_document tsvector not null`、写时trigger及对应GIN；应用日志页索引改为`(category,created_at desc,id desc)`，权限/登录/上传增加或改为`(created_at desc,id desc)`，扫描日志清理索引补`id desc`。开发数据库整代重置，无旧行回填，远端public generation85未迁移或写入。
- 搜索投影：trigger在INSERT或相关字段/actor ID更新时构建最多16,384字符的simple全文文档，覆盖旧搜索的日志本身字段和当时关联用户标识快照。GET只使用`search_document @@ websearch_to_tsquery('simple',q)`；不再包含`payload::text`查询期转换、`LIKE '%q%'`或可选扩展依赖。
- 列表请求：GET `/api/v1/admin/logs`保留category/q/level/status/from/to/limit并新增opaque cursor；默认100、最大500。全部参数只允许单值，page/offset、未知参数、非法来源/状态/日期、反向范围、201字符搜索以及畸形/未知字段/跨任意筛选或limit复用cursor均400。
- 列表响应：旧`LogRow[]`替换为`items,limit,hasMore,nextCursor`；四类来源统一按`(created_at,id)`降序排他续页、读取limit+1且不执行COUNT。数据库Query、row decode或rows迭代失败在写成功DTO前返回500；前端保存产生cursor的原查询并显式追加更早页。
- 清理边界：保存现有`logs.retention`配置仍触发一次即时清理，但每个来源只选择按`created_at,id`最早的最多1,000个ID并`for update skip locked`，随后按ID删除；不会在单个HTTP请求对任一表执行无界DELETE。BUG-052的自动调度和BUG-053的清理错误响应仍是独立协议缺陷，不由本项隐藏。
- 部署/回滚：前端与后端数组→envelope必须协调，后端需在接流量前重置开发Schema到118。回滚必须一并恢复generation117和数组consumer，会重新引入不可达历史、宽JSON扫描及无界删除；不保留offset、裸数组或双搜索路径。

## PERF-034：项目热度累计事实、显式校准与到期调度协议

- Schema generation：从118提升到119。新增`content_popularity_lifetime_facts`（去重访客、页面、下载、有效收藏/评论/评论者、评分数/总分）、32 shard的`content_popularity_view_totals`及`content_popularity_rating_dimension_facts`；全部计数/总分保持非负约束。`content_popularity_stats`新增`decay_until`、`next_decay_at`和`idx_content_popularity_stats_decay_due(next_decay_at,object_route_id) where next_decay_at is not null`。
- 写时维护：浏览日桶、独立访客、项目页面、下载counter、收藏首/末条、评论发布状态、评分发布状态/总分及维度评分的insert/update/delete trigger只写对应delta。delta helper先`insert ... do nothing`确保零行，再在同事务原子UPDATE；因此合法负delta不会被INSERT前约束拒绝，而任何真实underflow仍失败并回滚来源写。
- hot refresh：`refresh_content_popularity(route)`不再引用`content_view_daily`、`content_unique_views`、`content_project_pages`、`content_download_counters`、收藏、评论、评分或维度评分历史表；只汇总最多32个view shard、单route累计/维度事实、90日日桶和当前推广。既有热度公式及daily snapshot协议不变。
- 衰减调度：stats按新项目窗口、最后趋势日和推广到期计算`decay_until`；仍需变化时写`next_decay_at=now()+15 minutes`，否则NULL。Worker启动时一次扫描缺少stats的route，之后周期只用部分索引选择到期stats并以`refresh_metrics=false,refresh_popularity=true`写既有coalescing队列。
- 校准：`rebuild_content_popularity_lifetime_facts(route_id)`是显式低频离线运维函数，从全部权威历史重建三类投影并写`calibrated_at`。必须在该route写入静止的维护窗口调用；常规请求/worker永不调用。开发数据库整代重置直接生成新投影，无旧generation在线回填、双读或双写。
- API/兼容：公开与管理员HTTP DTO、排序公式和前端均不变；仅内部Schema/trigger/Worker选择协议改变。PERF-035全局评分刷新仍独立，BUG-057/MAP-007的单owner排除口径仍保持现状并继续开放。
- 部署/回滚：接流量前把开发数据库重置到generation119；远端public generation85未迁移或写入。回滚必须恢复generation118 Schema及旧refresh/调度，会重新引入每route全历史扫描；不保留运行时切换或新旧累计双路径。

## PERF-035：全局评分类型累计与集合离线重建协议

- Schema generation：从119提升到120。`content_rating_global_stats`表和公开读取列不变；新增`adjust_content_rating_global_stats(type,count_delta,sum_delta)`，以先确保类型零行、再原子UPDATE的方式维护count/sum/average和updated_at。非负约束及零评分3.5先验不变。
- 写时维护：`trg_content_ratings_popularity`在既有单owner、active/security资格判断后同时维护route累计和类型累计。相同类型的有效评分更新只应用`new.overall_score-old.overall_score`；隐藏/删除减1及旧分，恢复/新增加1及新分，跨类型或资格迁移减旧类型再加新类型。
- Worker协议：`processContentStatsTask`删除`refresh_content_rating_global_stats`及其5秒updated_at判断；普通热度任务不再读取或写任何全类型评分集合，只读取已经累计的类型先验后刷新单route。
- 校准：`rebuild_content_rating_global_stats(type)`明确是维护窗口函数。它用`public_routes left join effective_project_access`按route集合计算`min(developer user_id)`，再集合联接ratings/users重建目标类型；禁止调用`content_target_owner_id`。`min`仅保持当前语义，BUG-057/MAP-007的developer集合问题仍开放。
- 一致性边界：rating事务实时维护；用户status/security或项目权限来源的批量变化需在相关写入静止时显式set rebuild。开发数据库整代重置生成干净累计，无在线回填、双读或Worker fallback。
- API/部署/回滚：公开/管理员HTTP DTO及前端不变；接流量前重置开发Schema到120，远端public generation85未写入。回滚需恢复generation119、旧函数及Worker分支，会重新引入最频繁5秒的全类型扫描和逐评分owner查询。

## PERF-036：后台项目搜索/排序投影与复合游标协议

- Schema generation：从120提升到121。新增`admin_project_catalog`，以`object_route_id`为主键并保存公开ID、类型、名称、路径、审核态、列表所需统计、heat、项目时间和last edit；`search_document`由公开ID/名称生成stored tsvector。开发库整代重置，无旧行在线回填，远端public generation85未迁移或写入。
- 索引：`idx_admin_project_catalog_heat(heat_score desc,updated_at desc,object_route_id desc)`服务全类型页，`idx_admin_project_catalog_type_heat(entity_type,heat_score desc,updated_at desc,object_route_id desc)`服务类型页，`idx_admin_project_catalog_search`是全文GIN。旧基础表名称B-tree不承担包含搜索。
- 写时维护：`refresh_admin_project_catalog(route_id)`从统一顶层项目view、metrics和popularity精确upsert一行。public route、mods/modpacks/simple projects/servers以及metrics/popularity的来源trigger在同事务调用；不再由后台GET临时联接/派生列表行。删除public route时外键级联投影。
- 请求：GET `/api/v1/admin/dashboard/projects`只接受单值`q/type/limit/cursor`；默认30、最大100。未知/重复参数、page/offset、非法类型、超长搜索、畸形/未知字段/跨筛选游标返回400。cursor绑定规范化q/type/limit并携带固定精度heat、updated和route ID。
- 查询/响应：列表只读投影，按`(heat_score,updated_at,object_route_id)`降序、cursor排他小于及limit+1；精确ID和`websearch_to_tsquery('simple')`共享受索引谓词。成功DTO为`items,limit,hasMore,nextCursor`，不再返回或计算total/offset。单项目详情和趋势协议不变。
- 调用方：Next16后台项目工作台以opaque cursor和历史栈实现Previous/Next，筛选改变时清空历史、选择及详情状态；不再显示精确总数或构造offset。中英文显示当前页码。
- 性能/门禁：100k/1M exact生产SQL分别验证全局首屏、深游标、类型页与稀疏搜索；零Seq Scan/OFFSET，百万行四类墙钟约29/30/30/610ms且都有2秒阈值。完整临时Schema验证project/route/metrics/popularity写时同步。回滚必须协调恢复generation120、旧DTO与前端offset，会恢复原扫描风险，不保留双读。

## PERF-037：搜索重建流、输入/输出预算与进度协议

- Schema generation：从121提升到122。新增`search_index_rebuild_progress`，以`collection_kind`为主键，保存collection名、当前document type、last document ID、累计document/byte/batch、`building|complete|failed`、最多2,000字符错误和开始/更新时间；全部计数非负。开发库整代重置，不做旧进度回填。
- 读取协议：每个collection展开为固定document type序列，每类都按内部主键升序执行`id>$after order by id limit 500`。资源使用`game_resources.entity_id`，其余使用各自主键。typed loader只接受显式ID数组；删除旧nil全量选择及collection级loader，delete-only空ID页保持返回空集合。
- 项目SQL：Mod/Modpack/simple project以selected-ID CTE限定单页；本地化、标识、creator binding、tag和loader/version分别分组聚合后一次联接。Markdown和名称先`left`截断，本地化/creator最多16项，其余数组最多64项；持久数组字段通过`unnest ... with ordinality`有序截断。Go输出再按字段限制64项/8KiB并保持合法UTF-8。
- 导入协议：page loader最多持有500个ID及500个文档；JSONL发送器按编码行字节累计，单请求上限8MiB，超限前拆批，单文档超限失败并使进度进入failed。成功页才原子累加last ID、document/byte/batch；alias与`search_index_state`完成后状态改为complete。
- API/兼容：公开搜索HTTP、Typesense alias、collection schema和增量队列协议不变，无前端变化。进度表是内部运维事实；本项不提供多实例租约、断点resume或滚动双Schema兼容，这些分别留给BUG-058/OPS-011/TEST-030。
- 部署/回滚：接流量前重置开发Schema到122；远端public generation85未迁移或写入。回滚需恢复generation121和旧Worker，会重新引入全collection内存风险；不保留nil全量或无字节预算兼容开关。

## PERF-038：死信状态/聚合筛选与复合游标协议

- Schema generation：从122提升到123。`dead_letter_events`新增非空`aggregate_type`、`aggregate_id`（默认空字符串），新增全量、unresolved部分、replayed部分的`(failed_at desc,id desc)`页索引及`(aggregate_type,aggregate_id,failed_at desc,id desc)`聚合页索引。开发库整代重置，不对旧行在线回填；远端public generation85未迁移或写入。
- 生产者事实：JetStream consumer在事件可解码为`EventEnvelope`时保存aggregate type/ID；PostgreSQL outbox发布失败从被移动行返回并持久化相同字段。冲突重试刷新聚合、attempts/error/failed_at并恢复为unresolved，避免复用旧死信记录时遗留错误筛选事实。
- 列表请求：GET `/api/v1/admin/infrastructure/dead-letters`接受单值`status/aggregateType/aggregateId/limit/cursor`；status缺省`unresolved`且仅允许`unresolved|replayed|all`，limit缺省50、最大100。aggregate ID必须与type共同提供；未知、重复、page/offset、超长或空白变体参数及畸形/未知字段cursor均400。
- 游标与查询：opaque cursor携带v1、规范UTC `failedAt`、正整数ID及status/type/id/limit的SHA-256 scope；查询按`(failed_at,id)`降序排他续页并读取limit+1。已知status和aggregate组合由固定SQL builder生成，禁止OFFSET、COUNT和客户端自造游标。
- 响应/调用方：旧固定100条数组替换为`items,limit,hasMore,nextCursor`；item新增`status/aggregateType/aggregateId`。NATS管理面板提供状态、聚合type/ID筛选及Previous/Next cursor历史，筛选改变时回到首屏；单条POST replay继续确认后调用原端点并刷新当前页。
- 兼容边界：POST `/api/v1/admin/infrastructure/dead-letters/{id}/replay`的事务锁、publish、replayed状态和审计协议不变。本项不把指标查询错误伪装成零（ARCH-015），也不宣称完整基础设施边界矩阵（TEST-032）。
- 部署/回滚：后端Schema与新envelope、前端调用方需协调部署，接流量前重置开发Schema到123。回滚必须恢复generation122、旧数组consumer及生产者写列，会重新引入100条可达性上限；不保留offset或双DTO路径。

## PERF-039：MRPack loader元数据缓存与远端请求协议

- Schema/generation：无DDL、索引、回填、持久缓存或数据迁移，generation保持123；远端public generation85未访问。缓存随进程重启自然清空，不参与任务快照权威性。
- 具体版本选择：`resolveMRPackLoaderVersion`以规范化loader和精确Minecraft版本为key，成功值共享15分钟、最多256项；未命中的相同key由singleflight合并为一次下载和完整JSON/XML解析。错误不缓存，等待调用方可独立取消。
- 原始来源缓存：全部Minecraft/loader远端元数据继续经统一16MiB读取上限，成功200按URL缓存15分钟并保存ETag/Last-Modified；过期后发送条件GET，304延长TTL并复用正文。网络、HTTP、读取或大小错误不写入缓存。
- 资源协议：URL cache按LRU最多64项且payload总计最多64MiB；不同URL远端请求共享4槽并发预算，HTTP客户端30秒超时保持。单响应、总内存、缓存key和外呼并发分别有硬上限。
- API/调用方：POST preflight与create的路径、body、响应DTO、错误码及Next16两阶段交互均不变，无协调部署要求。相同选择在TTL内复用结果；本协议不承诺跨TTL/跨实例预检快照冻结。
- 兼容/回滚：可直接回滚为旧无缓存抓取且无需数据操作，但会恢复顺序与并发重复下载。BUG-065、ARCH-017、OPS-012、MAP-009和TEST-033分别保留不可变确认、持久artifact目录、跨实例租约、多来源血缘和系统测试责任。

## BUG-066 / PERF-040：MRPack依赖图、候选批解析与失败关闭协议

- Schema/generation：无DDL、索引、回填或迁移，generation保持123。依赖图复用`public_routes`主键/subject唯一、`mod_relationships(mod_id,display_order,id)`、Mod PK及`project_external_sources(project_route_id,source_type)`唯一索引；远端public generation85未迁移或写入。
- 图读取：全部成功根route作为数组输入，一条recursive UNION SQL加载按Minecraft/loader过滤的可达route、Modrinth来源和稳定边；循环按route去重。节点最多1000、边最多4000，分别读取limit+1判定；Query/Scan/iterate/缺失节点错误全部传播，不返回部分图。
- 文件候选：收藏项初始SQL批量联接provider ID，整个preview只读一次provider配置。根项目候选和每个BFS frontier的新依赖用最多8 worker解析，输出保持发现顺序并复用provider snapshot cache；单项目500文件上限不变。
- 错误API：图超过预算时preflight/create返回422 `MODPACK_EXPORT_DEPENDENCY_LIMIT`。预检仍在item中报告`REQUIRED_DEPENDENCY_UNRESOLVED`及detail；create若存在任一该reason则返回422 `MODPACK_EXPORT_REQUIRED_DEPENDENCY_UNRESOLVED`并附preview，不能由`exportCompatibleOnly/confirmCompatibleOnly`绕过。
- 成功兼容：成功preflight/create DTO、任务/Outbox/Worker/ZIP协议及Next16调用方不变，无前端协调部署或数据重置。普通收藏项不可兼容仍沿用compatible-only确认，仅必需依赖失败关闭。
- 回滚：无需数据操作，可恢复旧逐route查询，但会重新引入约100次串行往返、100节点静默截断和错误吞没，不提供旧行为开关。BUG-069/070、SEC-024、OPS-013与TEST-034仍分别负责Worker一致性、路径冲突、配额、OSS补偿和端到端状态机。

## PERF-041：MRPack导出历史状态筛选与稳定游标协议

- Schema generation：从123提升到124。保留`idx_favorite_modpack_export_tasks_owner_created(owner_user_id,created_at desc,id desc)`服务all页，新增`idx_favorite_modpack_export_tasks_owner_status_created(owner_user_id,status,created_at desc,id desc)`服务六终态筛选。开发库整代重置，无旧行在线回填；远端public generation85未迁移或写入。
- 请求：GET `/api/v1/users/me/modpack-exports`只接受单值`status/limit/cursor`；status默认`all`且允许`pending|processing|ready|failed|expired|cancelled`，limit默认30、最大100。未知/重复、page/offset、非法状态/limit及畸形cursor均400。
- 游标/查询：base64url v1 cursor保存UTC `createdAt`、正内部ID以及owner/status/limit scope hash；跨账号、筛选或limit不能复用。SQL按`(created_at,id)`降序排他续页并取limit+1，不使用OFFSET或COUNT；Query、Scan及rows终错都在成功响应前传播。
- 响应/调用方：旧`{items}`替换为`{items,limit,hasMore,nextCursor}`；item DTO不变且不泄露内部ID。Next16 loader和历史面板协调消费envelope，支持全部/六状态切换和加载更早记录，追加时按task public ID去重。
- 部署/回滚：后端generation124与前端page consumer需协调部署，接流量前重置开发Schema。回滚必须同时恢复generation123、固定窗口consumer和旧响应，会重新引入100条后不可达历史；不保留offset、双DTO或客户端制造cursor路径。

## PERF-042：蓝图处理资源预算、入队配额与材质批写协议

- Schema generation：从124提升到125。新增部分索引`idx_blueprint_jobs_creator_active(created_by,id) where status in ('queued','processing') and created_by is not null`；开发库整代重置，无旧行在线回填，远端public generation85未迁移或写入。
- 接收/解码：蓝图源文件和解码NBT分别最多32MiB，体积最多2,097,152、非空气块250,000、不同材质8,192。presign、OSS既有文件关联、Worker读取和codec均执行服务端限制；越界不进入处理队列或持久化部分结果。
- 处理预算：规范JSON按元素写入最多64MiB的有界buffer，结构与成功DTO不变；全进程normalize/convert共享2槽。最大合法250,000块工作负载总分配约109.1MiB、耗时约343.8ms。
- 入队协议：所有上传、转换、重试在既有事务内先取得用户advisory lock，并以部分索引和`limit 4`子查询限制每用户最多4个queued/processing任务。源文件超限返回413 `BLUEPRINT_SOURCE_SIZE_LIMIT`，任务预算超限返回429 `BLUEPRINT_JOB_CONCURRENCY_LIMIT`；成功路由、请求和响应不变。
- 持久化：材质旧集合仍在同一事务删除，但新集合由一次`CopyFrom`写入最多8,192行并核对精确计数，删除逐材质INSERT往返。完整generation125中8,192行约273ms；失败整体回滚。
- 兼容/回滚：前端无需迁移，既有通用API错误显示可处理新增413/429。回滚需恢复generation124及旧Worker，但会重新暴露合法输入多GiB堆、无共享并发/用户预算和逐行写放大；不保留旧大限额或逐行写开关。

## PERF-043：蓝图材料revision namespace范围与最新行索引协议

- Schema generation：从125提升到126。新增部分表达式索引`idx_catalog_import_revisions_material_namespace(source_namespace,coalesce(activated_at,created_at) desc,id desc) where is_active and status in ('ready','partial')`；开发库整代重置，无旧行在线回填，远端public generation85未迁移或写入。
- 查询范围：材料读取后先提取并去重block ID namespace；只有非空实际集合才执行revision查询。SQL使用`source_namespace=any($1::text[])`，不再读取全站namespace；材料8,192上限同时约束数组大小。
- 最新语义：每namespace按activation/creation时间降序并以revision ID降序稳定决胜，继续只选择active ready/partial；解析得到的revision key仍一次批量交给现有资源resolver。
- API/调用方：蓝图详情路由、locale、响应字段、fallback及前端均不变，无协调部署。ARCH-019的查询/扫描/批解析错误可见性未在本项改变或关闭。
- 性能/回滚：100k/1M active revision exact SQL均命中新索引，数据库执行0.049/0.043ms且无Seq Scan。回滚需恢复generation125和旧查询，会重新把全站目录排序/去重放进每个公开详情；不保留全局查询兼容分支。

## PERF-044：OSS用户额度活动桶、对象预留与完成结算协议

- Schema generation：从126提升到127。新增`oss_user_quota_usage`、`oss_user_daily_quota_usage`、`oss_user_upload_quota_reservations`及用户/expiry索引；四类active/reserved source/stored计数非负，reservation以用户+object key唯一。开发库整代重置，远端public generation85未迁移或写入。
- 写时维护：`trg_oss_files_quota_usage`在active文件insert/update/delete时同事务维护总量与created date日桶；uploader/status/size/source size/date变化减旧加新，underflow失败关闭。`rebuild_oss_user_quota_usage`与`rebuild_oss_user_daily_quota_usage`仅供写入静止的离线校准。
- 预留：用户预签名以advisory lock原子清理过期、替换同key并检查single/daily source/total stored；最多64个未过期预留。普通对象预留source+stored，需转换对象先预留source；签名/分片登记失败与abort释放。
- 完成：完成事务移除自身预留、按当前日与实际source/stored复核并在同一tx插入active文件，由trigger完成结算。重复完成释放残留；并发600+600/1000只有一个成功，不存在旧SUM竞态。
- API/调用方：上传、完成、quota请求和响应DTO、状态码及前端不变；额度used值只含active事实，不含尚未完成的reserved。数据库故障返回500，真实额度失败保持403及现有中文文案。
- 性能/回滚：1M历史陷阱下额度API/准入四个桶读取235.1216ms且不访问`oss_files`。回滚需恢复generation126和历史SUM，会重新引入线性扫描与并发超额；不保留旧聚合或双读开关。

## PERF-045：评论keyset、目标计数事实与generation128

- Schema generation：从127提升到128。新增`comment_target_counts(target_type,target_id,target_version_key)`和`comment_target_author_counts(...,author_id)`；两者保存非负visible count。`trg_comments_target_counts`覆盖insert、可见status、目标/作者变化及delete，来源事务内减旧加新；`rebuild_comment_target_counts`只用于写入静止的离线校准。
- 索引：根评论新增latest/oldest/hot/replies四个visible部分复合索引，leading列为目标、规范version key、pinned rank/pinned time及各排序tuple；直接回复新增`parent_id,created_at,id` visible部分索引；watch新增active user-created和user-unread/activity-ID索引，既有user-activity索引继续服务默认排序。
- API：三条既有`cursor?: string`从十进制offset切换为opaque base64url JSON v1。根cursor绑定target/version/viewer/sort，reply绑定parent/viewer，watch绑定user/filter/sort；畸形、未知字段、超限及跨scope使用返回400。旧数值cursor不接受，无双协议/双读。
- 响应/调用方：根`items,total,target,nextCursor,capabilities`、回复和watch的`items,nextCursor`均不改字段；`total`仍是扣除当前用户拉黑作者后的精确可见评论数。第一方前端本来只保存并回传opaque string，无源码迁移或协调发布。
- 性能/回滚：1M根四排序、100k直接回复和2k watch深页计划均零Seq Scan，57.577–59.2696ms；总数事实57.336ms。开发数据库整代重置，远端public generation85未迁移或写入。回滚到127必须同步恢复旧cursor协议和COUNT，会恢复线性深页成本，不保留兼容开关。

## PERF-046：watch评论/目标批量装配（无协议或Schema变化）

- Schema generation：保持128；不新增表、列、索引、trigger或持久投影。复用PERF-045的watch keyset页上限和既有各目标权威表。
- 内部读取：最多100个comment ID一次进入共享评论装配；最多100个去重target identity一次进入按实际类型生成的固定UNION。目标批量查询复用单条resolver的viewer/moderator、review/status/visibility/owner规则；数据库错误整页失败，明确不可见保持type-only fallback。
- 头像：OSS配置每页读取一次，stored URL到访问URL在请求内按唯一字符串缓存；不改变下载mode、TTL、签名算法或跨请求缓存策略。
- API/调用方：`GET /api/v1/users/me/comment-watches`的query、状态码、成功DTO、cursor和前端不变。唯一收紧是数据库读取失败不再伪装为跳项/空目标而返回500，无兼容旁路。
- 性能/回滚：完整handler的1项与100项页面均为12条数据库语句；100个不同目标单SQL61.8907ms。回滚恢复逐项N+1且无协议收益，不提供开关。

## PERF-047：玩家档案纹理集合装配（无协议或Schema变化）

- Schema generation：保持128；无表、列、索引、trigger或数据迁移。集合查询复用`player_profiles.public_id`唯一索引、`player_profile_textures(profile_id,kind)`主键及`skin_wardrobe(user_id,asset_id)`主键；远端public generation85未修改。
- 内部读取：档案主查询完成后，把全部public ID交给单次`ANY(text[])`纹理查询并以ID map回装skin/cape；空档案集合不查询纹理。单档案详情以size-1调用相同helper，不保留独立逐项SQL实现。
- API/调用方：我的档案与公开用户档案路由、query、可见性、排序、成功DTO、texture URL、CanEdit/CanUse和wardrobe字段不变；无前端迁移或协调部署。
- 错误/性能：Query、Scan、rows错误继续使整个读取失败。真实PG中1档案与100档案都执行2条SQL，100档案装配200纹理；回滚会恢复最多101条语句，没有兼容收益，不提供开关。

## PERF-048：公共皮肤目录全文投影、稳定游标与generation129

- Schema generation：从128提升到129。新增`skin_public_catalog`窄投影，保存公开可见asset的筛选、九类排序事实及stored simple `tsvector`；新增search GIN和published/updated/heat/downloads/favorites/rating/views/comments/name九个稳定排序B-tree。开发库整代重置，无在线回填，远端public generation85未迁移或写入。
- 写时维护：`refresh_skin_public_catalog(asset_id)`从active/approved/public skin asset、公开route和popularity精确upsert或删除；asset、public route和popularity insert/update/delete trigger与来源事务同步刷新。`rebuild_skin_public_catalog()`为写入静止的离线校准入口，常规GET不调用。
- 请求：GET `/api/v1/skins`只接受单值`q/kind/model/limit/sort/order/cursor`，默认24、最大100；未知/重复、page/offset、非法枚举、超80字符/320字节q及畸形/跨scope cursor返回400。cursor为严格base64url v1并绑定所有筛选、limit、规范排序和方向。
- 查询：仅在投影上使用stored `search_document @@ websearch_to_tsquery('simple',q)`、稳定tuple+asset ID排他keyset和limit+1；生产查询零OFFSET、COUNT、ILIKE及查询期`array_to_string`。完整详情按页asset ID一次批量读取并再次验证active/approved/public，保持页顺序且并发可见性收紧。
- 响应/调用方：旧`items,total,limit,offset`替换为`items,limit,hasMore,nextCursor`。Next16皮肤库同步移除total/offset，保存cursor历史实现Previous/Next；搜索、kind/model、排序与重置清空历史。没有旧offset、双DTO或客户端构造cursor路径。
- 性能/回滚：1M exact SQL的published/views/filter/FTS计划约61.9935/270.0025/61.0444/355.2036ms，无投影Seq Scan；真实handler各页恒2 SQL且并发新首项不漂入后页。回滚必须协调恢复generation128和旧前端，会恢复基础宽表包含扫描、深OFFSET及同步COUNT。

## PERF-049：用户衣柜稳定分页与增量调用方（无Schema变化）

- Schema generation：保持129；无表、列、索引、trigger、回填或开发库重置。复用`idx_skin_wardrobe_user_added(user_id,added_at desc,asset_id)`；查询采用`added_at desc,asset_id asc`及对应混合方向排他谓词，精确匹配索引。远端public generation85未迁移或写入。
- 请求：GET `/api/v1/users/me/skin-wardrobe`只接受单值`kind/model/limit/cursor`，默认50、最大100；未知/重复、page/offset、非法枚举/limit、畸形/未知字段/跨用户或筛选cursor均400。cursor绑定user/kind/model/limit并携带added time与asset ID。
- 查询/响应：页查询读取limit+1并返回完整资源；已由wardrobe join证明成员关系，`inWardrobe=true`不再执行冗余exists。total由只联接wardrobe+asset且使用相同可见性/kind/model条件的独立COUNT返回。成功DTO为`items,total,limit,hasMore,nextCursor`，旧无参不再返回5000项。
- 调用方：Next16玩家档案/衣柜面显式首取100项，以服务器cursor加载更多并按public ID去重；删除同步减少loaded/total。当前已装备但尚未进入已加载页面的texture由档案事实插入对应select，不能因分页丢失选择可达性。
- 性能/回滚：5000存量首两页各100项/2 SQL/71,318字节；深页62.6448ms命中既有索引且无wardrobe Seq Scan或Sort。回滚必须协调旧DTO和全量前端，会恢复5000完整对象单响应，不提供旧fixed-window开关。

## PERF-050：评分明细稳定游标与聚合计数分离（无Schema变化）

- Schema generation：保持129；无表、列、索引、trigger、回填或开发库重置。复用`idx_content_ratings_target(object_route_id,status,updated_at desc,id desc)`，查询排序和排他tuple与索引完全一致。远端public generation85未迁移或写入。
- 请求：GET `/api/v1/ratings/{targetType}/{publicId}/reviews`只接受单值`limit/cursor`，默认20、最大100；未知/重复、page/offset、非法limit、畸形/未知字段/跨route、type、public ID或limit cursor均400。cursor携带updated time与rating ID并由完整目标scope绑定。
- 查询/响应：明细页读取limit+1，成功DTO从`items,total,limit,offset`迁移为`items,limit,hasMore,nextCursor`；删除同步精确COUNT和深OFFSET。页内维度仍一次集合查询，OSS设置仍每页读取一次；评分汇总GET及其投影`ratingCount`协议不变。
- 调用方：Next16评分modal以服务器cursor历史实现Previous/Next，每次只保存并渲染一个20项页；显示页号而不伪造总页数。首页按钮和评分概览继续显示独立summary投影中的评分总数。
- 性能/回滚：百万评分真实handler每页固定5条SQL且响应低于128 KiB；并发新评分不漂入已锚定次页。半深exact页60.6126ms命中既有索引，无评分表Seq Scan或Sort。回滚必须协调恢复旧offset DTO与前端，会恢复同步COUNT/深OFFSET，不保留双协议。

## PERF-051：收藏夹/收藏项稳定分页、集合摘要与generation130

- Schema generation：从129提升到130。新增`favorite_collections(user_id,is_default desc,created_at,id)`、相同tuple且`where is_public`的公开页索引、`favorite_collection_items(collection_id,created_at desc,id desc)`项页索引，以及`favorite_collection_items(entity_type,entity_id,collection_id)`membership反向索引。完整临时Schema安装成功；远端public generation85未迁移或写入。
- 列表请求：私有/公开收藏夹和收藏项GET只接受单值`limit/cursor`，默认20、最大100；拒绝未知/重复、page/offset、畸形/未知字段及跨owner/visibility/viewer/collection/limit cursor。收藏夹使用default-time-ID混合方向keyset，项使用time-ID降序keyset，均读取limit+1。
- 列表响应：四类成功DTO统一为`items,limit,hasMore,nextCursor`。收藏夹删除同步精确`itemCount`，项元数据只为当前页装配；公开与私有可见性、待审目标owner/moderator规则不变，数据库读取错误在成功响应前失败。
- membership摘要：新增POST `/api/v1/users/me/favorites/summary`，请求`entityType/entityPublicIds/collectionIds?`；目标及集合各最多100且矩阵最多100。无集合筛选时按每个目标的索引LATERAL limit1返回`entityPublicIds`；带集合筛选时返回`collectionIdsByEntity`。单请求恒一条membership SQL，不返回完整收藏项元数据。
- membership写入：新增PATCH `/api/v1/users/me/favorites`，请求单目标及互斥`addCollectionIds/removeCollectionIds`，总变更最多100。Repeatable Read事务锁定并验证全部集合后批量删除/插入，响应`{saved,selected}`；第一方选择器仅提交跨页触碰delta。旧PUT目前保留为独立BUG-089/LEGACY-018范围，不作为分页兼容路径；旧无界GET已删除。
- 调用方：Mod/Modpack目录只对当前结果页发一次summary；蓝图详情发一个目标；选择器、账户收藏页和公开用户收藏页保存服务器cursor历史并仅保留当前页。创建、页切换和选择变更都不把未界定集合/项追加到客户端状态。
- 性能/回滚：1M收藏夹和1M不同收藏项下，收藏夹页/项页/100目标summary分别固定2/2/1 SQL；半深exact计划约163.7/177.2/60.5ms，命中新索引且目标表无Seq Scan，并发新项不漂入后页。回滚需协调generation129、四类旧DTO和全部Next16调用方，会恢复无界列表与集合级1+N，不提供旧GET或客户端全量聚合开关。

## PERF-052：社区目录窄全文投影、稳定游标与generation131

- Schema generation：从130提升到131。新增`community_post_catalog`窄表，保存最多320字符`body_summary`、stored simple `search_document`及目录筛选/排序事实，不保存完整正文。新增search/versions GIN和published/updated/heat/downloads/favorites/rating/views/comments/name九类稳定索引；开发数据库整代重置，远端public generation85未迁移或写入。
- 写时维护：`refresh_community_post_catalog(post_id)`从active社区帖、公开route和`content_popularity_stats`upsert或删除投影；community post、public route和popularity insert/update/delete trigger在来源事务内同步refresh。Schema初装显式重建全部active帖子；无GET内回填或双读。
- 请求：GET `/api/v1/community/posts`只接受单值`kind/q/category/version/versionMode/project/sort/order/modId/resourceId/limit/cursor`；limit默认24、最大100。未知/重复、page/offset、非法筛选/排序/limit及畸形、未知字段或跨筛选/viewer/moderator/limit/sort cursor均400。
- 查询：只从投影读取摘要与当前页事实；全文固定`search_document @@ websearch_to_tsquery('simple',q)`，全部排序用稳定tuple+ID排他keyset并取limit+1。匿名查询使用精确approved谓词以匹配目录索引；作者待审和moderator可见性保持。生产列表查询无`body_markdown`、`ILIKE`、COUNT或OFFSET。
- 响应/调用方：成功DTO从`items,total,limit,offset,categories`迁移为`items,limit,hasMore,nextCursor,categories`；list item删除`bodyMarkdown`并新增`summary`，单项详情DTO及完整正文不变。Next16四类目录保存cursor历史；首页与相关内容取有界summary首屏；所有第一方调用移除offset/total假设，无双协议。
- 性能/回滚：1M投影真实handler每页固定5 SQL、低于64KiB且不泄露首屏1MiB正文；半深published与FTS约72.7/74.0ms命中B-tree/GIN，投影无Seq Scan，并发新首项不漂入后页。回滚需同时恢复generation130、旧列表DTO和前端调用，会恢复近100MiB页、包含搜索、同步COUNT及深OFFSET，不提供兼容入口。

## PERF-053：社区引用领域预算与集合写协议（无Schema变化）

- Schema generation：保持131；无表、列、索引、constraint、trigger或数据迁移。继续复用两张引用表的身份unique/FK/目标索引和删除时未解析事实清理trigger；每帖最多32+64行使既有行级工作有硬界，远端public未写入。
- 请求规范：create/update JSON在事务前要求projects≤32、resources≤64；resolved public ID严格9字符；project raw ID≤128并使用Mod ID语法；resource raw ID≤256、有效UTF-8且无空白/control，kind≤64。按类型+大小写折叠身份拒绝重复；客户端名称/图标/locale/版本等展示字段清空后才写revision。
- 解析：project public IDs一次进入共享可见性batch resolver；resource public IDs一次`ANY(text[])`读取内部ID+权威kind；unresolved kinds一次`ANY(text[])`校验。SEC-037的目标类型和viewer权限逐输入复核不变；数据库/缺失错误不回退逐项查询。
- 替换：post范围两次DELETE后，两张引用表分别一次并行数组`unnest` INSERT并returning ID/order；返回数量精确核对。未解析project/resource分别一次`unnest` UPSERT。最大混合集合从解析至持久化固定9 SQL；审核revision缺内部ID时同样批量重解析。
- API/调用方：成功请求、revision snapshot和响应DTO不变；数量/身份超限返回稳定400。Next16 API导出32/64常量，editor submit与两类picker确认阻止超限并显示双语错误；服务端保持最终权威，无旧无界入口或客户端截断。
- 性能/回滚：真实PG中2项与最大96项都固定9 SQL，最大集合写32/64/48 unresolved；动态隐藏项目权限回归通过。回滚恢复数千条串行SQL与长事务风险且无兼容价值，不提供开关。

## PERF-054：共享内容历史稳定游标、双源有界合并与generation132

- Schema generation：从131提升到132。新增`idx_content_revisions_history(aggregate_type,aggregate_key,created_at desc,public_id desc)`与`idx_resource_import_snapshots_history(resource_id,created_at desc,revision_id desc)`；开发数据库需整代重置。完整临时Schema安装并清理成功，远端public generation85未迁移、重置或写入。
- 请求：社区、Mod资料、Modpack、simple project及changelog历史GET统一只接受单值`limit/cursor`，资料入口另允许既有`version`；默认50、最大100。未知/重复、page/offset/all、非法limit及畸形/未知字段/跨目标或limit cursor返回400。
- 游标与查询：base64url JSON v1 cursor绑定目标和limit，保存UTC created time、manual/import origin和稳定ID。两类SQL使用相同`(created_at,origin-rank,id)`排他全局tuple并各取limit+1；manual同时间优先，来源内ID降序。导入谓词/排序只引用snapshot列以直接命中新索引，再按主键逐页验证revision target/status。
- 多源合并：资料页每次最多读取101 manual+101 import候选，在Go中确定性排序后只返回最多100项。单源入口沿用同一page helper；无全历史append/sort、同步COUNT、OFFSET或不可达固定窗口。对象查询已有`published_revision_id`继续独立标记当前页中的current项。
- 响应/调用方：成功DTO从`{items}`协调替换为`{items,limit,hasMore,nextCursor}`，items字段不变。Next16共用ContentHistory保存服务器cursor栈、Previous/Next只替换当前页且取消过期请求；不累计全历史或推导总页数。无旧无界响应兼容入口。
- 性能/回滚：100k双源首/次/尾页完整且并发稳定；深页manual/import分别约0.160/0.427ms并命中新索引，无目标历史表Seq Scan。回滚必须协调generation131、旧后端和旧前端，会恢复全集数据库读取/Go合并/DOM放大，不提供双协议。

## PERF-055：内容差异有界摘要与单次批写（无Schema变化）

- Schema generation：保持132；无表、列、索引、constraint、trigger、回填或开发库重置。继续使用`content_change_items(revision_id,path,operation,before_value,after_value)`、revision索引和immutable trigger；完整before/after权威内容位于相邻`content_revisions.snapshot`。远端public未写入。
- 持久预算：每个revision最多128条change，递归最多64层，JSON Pointer最多512B，单个before/after JSON最多512B。详细条目达到127后追加path=`/`的根摘要；超长路径以有效UTF-8前缀+哈希表示，不能扩张行宽。
- 大值协议：超过512B的值不复制原JSON，而保存内部摘要对象：`$summary`、`bytes`、`sha256`，并按类型增加`items`、`fields`或`characters/preview`。摘要只供审核/通知索引展示；完整重建和Mod compare继续读取immutable snapshots，没有第二份权威状态。
- 写协议：path/operation/before/after组成四个并行text数组，一次`unnest` INSERT并在SQL内转换nullable text为JSONB；有base时数据库往返固定为base snapshot读取1+批写1。无逐项Exec、COPY旁路或超预算尾部写入。
- API/调用方：外部修订请求、成功响应、history和Mod compare DTO不变。审核队列的blueprint/creator摘要按change ID排序且最多8192字符；项目更新section继续读取path，根摘要映射为`published_content`。旧完整change value从未是公开重建协议，不保留双写。
- 性能/回滚：约1.326MiB before/after含10k数组+1k变化字段时只写128行/4,904B，小/大均固定2 SQL且数组原值零重复。回滚会恢复数千INSERT和重复大JSON，无兼容价值，不提供开关。

## PERF-056：等级配置版本与有界派生重算任务（generation133）

- Schema generation：从132提升到133。`level_system_config`新增正整数`version`，每次成功PUT在数据库内原子递增；开发数据库需整代重置。远端public仍为generation85，未迁移、重置或写入。
- 任务表：新增`level_recalculation_jobs`，以唯一`config_version`保存`role_track_code/level_thresholds/role_ids`快照，并记录queued/processing/completed/superseded/dead、主键cursor、processed、attempt/max=8、next attempt、owner lease、错误、actor与时间。阈值和role数组基数必须一致，processing与lease/owner由CHECK联动。
- 索引/不变量：ready任务使用`(next_attempt_at,id)`部分索引，失联worker使用`(lease_expires_at,id) where processing`；`((true)) where status in ('queued','processing')`部分唯一索引禁止两个活动版本。`user_experience`现有主键直接服务批次keyset，不新增重复索引或全表回填。
- 写协议：PUT事务以共享锁读取所选轨道和有序role ID，更新配置并返回版本，把旧活动job改为superseded后插入新queued job。HTTP事务不读取或锁定`user_experience`。Worker每次用单条CTE锁定最多501行、处理500行，集合更新变化level和差异`level_track`角色后原子推进job；失败由token lease、退避和dead状态恢复。
- API：GET/PUT `/api/v1/admin/levels/config`原有`roleTrackCode/levelThresholds`字段不变，新增可选`version`与`recalculation{configVersion,status,cursorUserId,processedCount,attempts,maxAttempts,lastError?}`。PUT 200表示配置版本和重算任务已提交，状态通常为queued；调用方可重新GET观察进度。客户端回传这些只读字段不会成为数据库权威。
- 调用方/兼容：Next16共享`LevelConfig`类型已声明新增字段，现有两个管理面仍可按原字段编辑。无新路由、旧同步开关、双写或内存任务旁路。回滚必须协调恢复generation132及旧同步语义，会恢复全表锁/N+1，故不提供兼容入口。
- 性能：100k用户下配置请求固定6 SQL；重算每500项固定1 SQL，精确200批完成，深游标命中`user_experience_pkey`。配置替换会使旧token失效，manual和其他角色来源保持不变。

## PERF-058：站点更新日志共享稳定游标与generation134

- Schema generation：从133提升到134。保留既有published部分索引`idx_site_changelogs_public(change_date desc,id desc) where status='published'`，新增后台全量`idx_site_changelogs_admin(change_date desc,id desc)`；开发数据库需整代重置。完整临时Schema安装并清理成功，远端public generation85未迁移、重置或写入。
- 请求：公开GET `/api/v1/site-affairs/changelogs`只接受单值`locale/limit/cursor`，后台GET `/api/v1/admin/site-affairs/changelogs`只接受`limit/cursor`；默认30、最大100。未知/重复、page/offset、非法limit及畸形/未知字段/跨公开locale、admin scope或limit cursor返回400。
- 游标/查询：base64url JSON v1 cursor绑定端点scope和limit，保存规范UTC日期与内部稳定ID。两类SQL在materialized CTE中先用`(change_date,id)`排他keyset取`limit+1`条主表行，再只对当前页LATERAL选择一个公开翻译或聚合后台翻译；无全翻译预聚合、COUNT、OFFSET或固定100条窗口。
- 响应/错误：两路成功DTO统一为`{items,limit,hasMore,nextCursor}`；item字段、单项详情及后台写DTO不变。列表迭代错误在响应前失败，后台翻译聚合严格解码typed JSON。旧公开offset与后台`{items}`合同协调删除，不保留双协议。
- 调用方：Next16公开列表和后台管理面保存服务器cursor历史，Previous/Next只替换当前30项页；公开语言切换清空scope，后台保存后回到首屏。两端不累计全历史、不按页长猜测continuation，也不再存在第101项不可达状态。
- 性能/回滚：1M日志+1M翻译装载5.614s，公开和后台百万深度页各固定1 SQL/50项并命中对应索引，无目标表Seq Scan；首屏后新插入日志不漂入已锚定次页。回滚需协调generation133、旧后端DTO和两处前端，会恢复深OFFSET与后台固定截断，不提供兼容开关。

## PERF-059：未解析引用统一读投影、有预算前缀搜索与generation135

- Schema generation：从134提升到135。新增`unresolved_reference_catalog`，以`origin/source_row_id`复合主键保存两类权威未解析事实的列表字段；新增global/status/type/status+type四类time-origin-ID页索引、raw/label两个`lower(...) text_pattern_ops`前缀索引及source反查索引。开发数据库需整代重置；远端public generation85未写入。
- 投影维护：`unresolved_references`和`unresolved_resource_references`的insert/update/delete trigger调用各自refresh函数。Mod relationship、社区project/resource ref及Modpack entry的source link变化，以及Mod/社区/Modpack/Catalog/Recipe的展示身份变化，会集合刷新受影响行；删除权威行或Catalog级联同步删除投影。投影不是写入权威，generation初装无旧代双写/回填兼容。
- 请求/游标：GET `/api/v1/admin/unresolved-references`只接受单值`q/type/status/limit/cursor`，默认50、最大100。未知/重复、page/offset、非法字段及畸形/跨query/type/status/limit cursor返回400。cursor保存UTC created time、origin与source row ID，并以SHA-256派生scope绑定全部筛选。
- 搜索预算：q为空或2..64字符，统一小写并把反斜杠、`%`、`_`转义为literal prefix；不再支持任意子串。搜索先以同scope的`limit 10001`子查询计数，超过10,000匹配稳定400且不发page SQL；接受的搜索最多2 SQL，空搜索严格1 SQL。没有无界total或状态count。
- 查询/响应：投影按`(created_at,origin,source_row_id)`排他keyset读取`limit+1`，成功返回`items,limit,hasMore,nextCursor`；删除原跨表JOIN/UNION、ILIKE、window count、OFFSET和total。rows迭代/scan/budget错误在成功响应前失败。
- 调用方/性能/回滚：Next16只保留当前50项及cursor历史，筛选/重复搜索清scope并以AbortController+active代次阻止旧响应。1M下深页一SQL命中keyset索引，10k前缀两SQL命中expression index，约990k前缀一条probe后拒绝；并发首项不漂入后页。回滚须协调generation134、旧DTO和前端，会恢复全量子串/窗口/OFFSET，不提供双协议。

## PERF-060：治理历史稳定游标与generation136

- Schema generation：从135提升到136。新增`idx_reports_reporter_history(reporter_id,created_at desc,id desc)`；审核状态队列复用`idx_reports_queue(status,created_at,id)`，公开/后台小黑屋复用`idx_ban_records_public(created_at desc,id desc)`。开发数据库需整代重置；完整临时Schema安装清理成功，远端public generation85未迁移、重置或写入。
- 请求：本人举报GET只接受`limit/cursor`，默认20；后台举报GET接受`status/limit/cursor`，默认status=pending、limit=50；公开与后台小黑屋GET只接受`limit/cursor`，默认30；最大均100。参数必须单值，未知/page/offset、非法limit/status及畸形/未知字段/跨reporter/status/public-admin/limit cursor返回400。
- 游标与查询：opaque v1 cursor保存UTC created time与内部ID，并以SHA-256派生scope绑定身份、筛选、端点和limit。本人/小黑屋使用降序`<`tuple，审核保持最旧优先并使用升序`>`tuple；三类均取limit+1、检查Scan/rows错误且不执行COUNT或OFFSET。
- 响应：GET `/api/v1/users/me/reports`、GET `/api/v1/admin/reports`、GET `/api/v1/site-affairs/blackroom`与GET `/api/v1/admin/bans`的列表成功DTO统一为`{items,limit,hasMore,nextCursor}`；item字段不变，写路由与详情DTO不变。旧offset/offset响应协调删除，不提供双协议。
- 调用方：公开小黑屋、后台审核和封禁管理保存当前页及Previous cursor栈；审核status变化清空scope，新增封禁回首屏，取消旧请求不会覆盖新页。本人举报当前没有第一方页面，因此只迁移API合同。
- 性能/回滚：1M举报+1M封禁下三类半深页各固定1 SQL并命中对应索引，目标表无Seq Scan；并发首项不漂入既有次页且跨页零重复。回滚必须协调generation135、旧响应与三个Next16调用方，会恢复深OFFSET线性成本，不保留兼容开关。

## PERF-063：OSS文件有界增量哈希（generation136不变）

- Schema：无表、列、索引、constraint、trigger、回填或开发库重置；generation保持136，未读取、迁移或写入远端public generation85。
- HTTP/API：OSS预签名请求、直传/分片上传、完成确认、文件记录和所有成功/错误DTO不变。`computeFileSHA256(file)`旧调用继续有效；内部可选参数新增`signal/onProgress/chunkSizeBytes`，不形成服务器协议或持久兼容面。
- 浏览器执行：默认4MiB顺序`Blob.slice`，ArrayBuffer以transfer list发送给module Worker并等待ack；页面全局最多一个活动hash，文件最大2GiB。Worker完成后只返回64字符hex摘要，取消会终止Worker并释放permit。
- Worker资产：`oss-sha256-worker.mjs`同时提供增量算法和受`WorkerGlobalScope`保护的消息入口，无运行时import、TS语法或外部资源路径。无Worker环境按相同分块增量计算并逐块让出事件循环。
- 调用方：所有原共享入口自动获得有界实现；Mod导出上传显式传递既有AbortSignal和hash进度。无需迁移存量数据、双写、功能开关或旧整文件hash旁路。
- 性能/回滚：额外读取内存由文件大小线性副本收敛到最多一个4MiB在途缓冲；20MiB+123B测试精确6个slice且并发读1，页面并发hash为1。回滚会恢复Blob+完整ArrayBuffer双驻留和主线程等待，不提供兼容开关。

## PERF-064：资料布局轻量游标读取与增量PATCH（generation136不变）

- Schema：无表、列、索引、constraint、trigger、回填或generation变化，保持136。布局placement、分类、资源definition及既有审核快照仍是权威事实；不增加客户端投影表或双写状态。
- 读取DTO：GET `/api/v1/mods/{siteId}/content-sections/{sectionId}/resource-graph`与鉴权GET `.../layout`的items改为`versionPublicId/resourcePublicId/sectionPublicId/label/ordinal/similarGroupId/advancement?`轻量摘要。`advancement`只含parent resource public ID、group、x/y和可选frame；不返回names、icon、detail判断、canonical/kind或整份definition。
- 读取预算：graph/layout默认与最大页均为500，按section sort path、ordinal和内部resource ID排他keyset读取`limit+1`，响应硬预算1MiB。graph接受3..100字符q并将规范query纳入cursor scope；layout拒绝q、offset和all。普通卡片GET继续使用自身最大200的cursor DTO。
- 写协议：删除PUT `.../layout`，只注册PATCH同路径。请求保留version/root/display/reason/base revision；`categories`可省略表示不变，显式空数组表示删除全部分类；`resources`是最多1000条变化，未知/重复resource失败关闭。显示模式变更发送空resources且省略categories。
- 服务端合并：Handler先验证version/base revision，再只读取分类本地化和紧凑placement；非晋升资源不装载definition，晋升资源只解析有效definition/import snapshot以恢复父节点、组和坐标。变化按public ID覆盖，未出现资源保持不变；被删除分类内资源移到根节点，随后沿用既有分类验证、审核和发布事务生成完整服务端快照。
- 调用方：公开晋升树和布局编辑器只保存当前页及Previous cursor栈，Next替换页面；graph搜索由服务端执行。布局dirty时禁用翻页，save只发送当前页最多500项和最多1000分类；workspace显示模式修改不再预读资源。旧`loadAllModContentAdvancementGraph/loadAllModContentLayoutResources/updateModContentLayout`无生产残留。
- 性能：100k临时Schema上布局首500条69,115B/724.7ms、深20k页70,230B/1.899s；1M keyset/GIN为0.087/0.056ms。20k旧完整DTO估算93,440,000B，新500摘要62,391B，增量合并1.615ms；浏览器资源状态与布局渲染硬界500。
- 隔离与回滚：一次旧集成测试误连配置public并创建唯一fixture，已用精确ID/slug/submitter/无引用条件删除并断言0残留；测试永久迁移到session临时Schema。回滚须同时恢复PUT、完整DTO和三个全量调用方，会重新引入2万条客户端物化；不保留双协议或开关。

## PERF-066：公开日志元数据与有界正文块（generation136不变）

- Schema：无表、列、索引、constraint、trigger、回填或generation变化，保持136；`log_shares`与`log_share_entries.sanitized_text`继续是权威脱敏事实。规模测试只创建同名`pg_temp`影子表并将search_path锁定到pg_temp。
- 详情GET：`/api/v1/log-shares/s/{code}`不接受query，返回分享字段及最多200条`index/name/contentType/byteSize/lineCount/checksum`元数据；entries删除text。安全名、content type、checksum分别在读取边界截到512/128/128字符，最终JSON硬上限256KiB。
- 正文GET：新增`/api/v1/log-shares/s/{code}/entries/{entryIndex}/content?cursor=`。只允许可选单值cursor；entry index为0..199。成功DTO是`text,characterOffset,hasMore,nextCursor`，单块最多32,768字符，并以SQL额外读取1字符判断continuation；最终JSON硬上限256KiB。
- 游标：base64url严格JSON v1保存字符offset，scope以public code与entry index派生；未知字段、尾随内容、跨条目、非正或超过100MiB offset返回400。首块使用空cursor；数据库substring参数显式为受上限保护的integer。
- 资源预算：metadata/chunk匿名昂贵读取分别30/60每分钟，认证身份翻倍；完整下载匿名5每分钟。正文与下载共享生产Server的8路非阻塞并发槽，满载503；取消使用request context中止数据库工作。
- 下载：原GET URL、内容类型与附件文件名合同不变。单文件只取目标正文；ZIP按entry顺序逐行Scan并立即写`zip.Writer`，不再先把所有正文放进Go slice。完整下载仍是显式动作并受限流/并发边界。
- 调用方：`LogShareEntry`删除text，新增`LogShareChunk`及`loadPublicLogShareEntry`。Viewer只保存当前块、当前cursor与Previous cursor栈；切条目/块取消旧请求，搜索/复制仅对当前块，删除150万字符事后截断和完整正文split。
- 性能/回滚：20MiB单条/200项总100MiB ZIP元数据仅427/27,074B；三类最坏转义块约196.7KiB、29–136ms。回滚须协调旧详情DTO和前端全文状态，会恢复公开大响应；不保留双详情协议或开关。

## PERF-067：审核队列稳定游标与当前页批量装配（generation136不变）

- Schema：无表、列、索引、constraint、trigger、回填或generation变化，保持136。编辑员倒序页精确复用`idx_project_editor_applications_review(status,created_at,id)`；服务器复用`idx_minecraft_servers_review_page(review_status,created_at,id)`及proof/link/mod关联索引。测试数据只存在于session temporary Schema/表，远端public未访问。
- 编辑员请求：GET `/api/v1/admin/project-editor-applications`只接受单值`status/limit/cursor`；status默认pending且允许approved/rejected/withdrawn，limit默认50、最大100。未知、重复、非法字段及畸形/未知字段/跨status或limit cursor返回400。
- 编辑员响应/查询：响应保留`items`并新增`hasMore/nextCursor`。v1 cursor保存UTC created time与内部ID，scope绑定status/limit；SQL按`created_at desc,id desc`排他keyset取limit+1。目标名在主查询批量JOIN，裁页后附件以当前页ID执行一次ANY查询，因此每页固定2 SQL。旧客户端省略新参数仍能读取首个有界50项页，但完整历史需消费cursor。
- 服务器查询：GET `/api/v1/admin/server-reviews`外部请求和响应合同不变。内部SQL先materialize最多limit+1条`review_page`，再把页ID数组用于proof/link/mod三个集合聚合CTE并回联counts；列表不读取body/proof正文或关联数组。GET单项详情和附件presign合同不变。
- 调用方：编辑员Next16管理面改用50项cursor续页，按ID去重保序并以generation+AbortController取消过时初始/续页请求；审核成功重载首屏。服务器面继续使用BUG-139/PERF-022建立的cursor摘要与按需详情，不新增第二协议。
- 性能/回滚：205条编辑员申请100/100/5遍历、每页2 SQL；100k深页0.076ms且只读51行。服务器100k页0.507ms且主表只读101行，100个1MiB正文列表恒1 SQL、单详情恒5 SQL。回滚会恢复编辑员无界响应和服务器非集合装配风险，不保留旧第一方读取旁路。

## PERF-068：资源展示精确批量补全（generation136不变）

- Schema：无表、列、索引、constraint、trigger、回填或generation变化，保持136。资源查找复用`catalog_entities.public_id`、`game_resources(kind_code,lower(canonical_id),entity_id)`及alias索引；标签/项目复用既有公开身份、canonical、slug和标识唯一索引。
- 请求：新增可选认证POST `/api/v1/catalog/resource-presentations`，body为`{locale,items:[{publicId,id,kind,registry}]}`。items必须1..1000；locale按站点闭集规范化，身份trim/case-fold并精确去重；public/id/kind/registry分别有180/255/64/128字节上限，缺public和id或越界稳定400。
- 查询：请求只做精确展示解析，不支持query、sort、page或total。Catalog资源、Catalog标签和approved项目各自至多一条集合SQL，空组不查询；单类1 SQL、完整混合最多3 SQL，数据库往返不随引用项数增长。
- 响应：成功为`{items}`，每项只含`publicId/id/registry/kind/names/resolvedName/resolvedLocale/iconUrl/source?`轻展示字段；不含definition、版本、正文、相似候选或计数。最终JSON复用2MiB Catalog响应硬预算，超预算失败关闭。
- 调用方：`ResourcePickerDialog`只对缺展示名的预选项调用该批接口一次，并在locale/token/value变化时Abort旧请求；各默认/自定义`loadPage`继续只服务浏览分页。已有名称的保存DTO零补全，不再存在6 Worker逐项搜索路径。
- 性能/隔离/回滚：临时完整Schema中1000资源返回202,806B、1 SQL、请求阶段728ms；资源+标签+项目为3 SQL。测试结束清理临时Schema，远端public未访问。回滚会恢复每预选项完整列表/total请求，不保留旧N请求兼容开关。

## PERF-069：蓝图紧凑实例索引、层桶与联合浏览器预算（generation136不变）

- Schema/后端：无表、列、索引、constraint、trigger、回填、路由或后端DTO变化；generation保持136，远端public未访问。蓝图规范化JSON和资产读取协议不变，600k可见方块仍是解析阶段第一道计数边界。
- 场景内部协议：`blueprint.blocks`是唯一方块对象表；每个InstancedMesh持有按层排序的`Uint32Array`方块index、唯一层`Int32Array`、offset `Uint32Array`、一个prototype matrix及最多两层的context index容量。删除持久`blueprintBlocks/allBlueprintBlocks/blueprintMatrices`。
- 场景预算：累计实际实例最多1,500,000，block-layer/index/主instance matrix/最坏两层context buffer已追踪总量最多128MiB，实际draw calls最多512。每个模型面在TypedArray与WebGL实例分配前检查累计下界，完整层容量后再次检查；有界fallback也受同一累计预算。
- 层/选择协议：全层视图复用初始静态实例且不创建context mesh；过滤视图以层二分得到连续index slice，只重写范围实例。透明context首次需要时懒创建，容量为任意两个最大层之和；raycast由当前active index反查唯一block对象。
- 内部观测：`StructureSceneResult/StructureRendererLoadResult`新增`instanceCount/contextCapacity/trackedInstanceBytes`，不越过浏览器模块成为HTTP合同。现有modeled/fallback/culling/draw/missing字段保持。
- 性能/回滚：600k生产层索引2,401,604B/14.6ms且单层+相邻只触碰9k；本机Chrome合成缓冲45,986,404B、heap46,844,856B、层写0.5ms。回滚会恢复面级对象/Matrix复制、满容量context和层全扫描，不保留旧渲染开关；PERF-070负责独立取消协议。

## PERF-070：蓝图端到端取消与单任务构建gate（generation136不变）

- Schema/后端：无表、列、索引、constraint、trigger、回填、开发库重置、路由或后端DTO变化；generation保持136，远端public未访问。GET蓝图render与revision资产URL、认证、成功/错误响应完全不变，只由浏览器在取消时中止既有HTTP请求。
- source协议：内部`StructureCanvasSource.load(signal?)`、`AssetSource.json/text(path,signal?)`及`BinarySource.bytes(path,signal?)`增加可选AbortSignal；省略signal的`BlockModelCanvas`等既有调用继续有效。Blueprint viewer已把signal交给render fetch，Canvas cleanup会abort同代下载和renderer。
- 缓存协议：revision HTTP source按JSON/text/bytes分别使用引用计数共享cache；同键并发只创建一个带自有controller的fetch。消费者独立取消，最后消费者离开才中止底层请求；失败或全取消会驱逐，成功结果继续缓存。fetch credentials/Accept、路径规范化和同步`has/url`合同不变。
- renderer协议：`StructureRenderer.load(input,name,signal?)`新load/dispose会abort旧controller；parse与scene build经页面级公平单任务gate执行。解析/模型/层打包/实例循环检查signal，AbortError不进入fallback；scene全部Worker退出后才释放组。load result、选择、层视图和截图字段不变。
- 不可抢占资源：同步JSON/NBT/OBJ解析由现有输入硬界、前后检查点和单任务gate约束。不可直接取消的TextureLoader异步结果若在abort后返回会立即dispose；普通模型组随scene统一dispose。HTTP source不创建object URL，因此无新增revoke协议或blob缓存。
- 兼容/回滚：这是浏览器内部可选参数扩展，不形成新HTTP版本、双读或持久兼容面。回滚必须同时移除Canvas/renderer/source/model/scene signal链、共享cache和gate，会恢复被替换任务与新任务并发叠加；不提供旧不可取消旁路。

## PERF-071：资料布局memoized render index（generation136不变）

- Schema/后端：无表、列、索引、constraint、trigger、回填、开发库重置、路由、请求或响应变化；generation保持136，远端public未访问。PERF-064建立的layout/graph 500项轻量cursor页、1MiB响应预算与增量PATCH完整保留。
- 前端派生协议：新增纯`buildModContentLayoutRenderIndex(resources,rootSectionID)`，返回section→`entries/clusters/positionByResourceID`及全页`resourceByID`。输入resources仍是唯一布局事实；Map/数组只在同一修订内缓存，可随页面切换或mutation丢弃，无持久化或双写。
- 渲染调用方：`CategoryLayoutEditor`按`[resources,root.publicId]`useMemo。分类区块直接读取bucket，chip位置直接Map lookup；受控分类名、语言、关键字和选择状态不会重新分组。单项drop/group/move也复用相同索引，保存仍执行原规范化并发送当前页变化集。
- 复杂度：顶层分组O(R)，每组排序总计不超过O(R log R)，position与共享cluster线性；不含分类数×资源数或组内每项findIndex。20k/1,000分类生产helper中位7.140ms且源数组只读20k项；真实React/DOM仍由500项页限制。
- 回滚/兼容：无HTTP版本、存量迁移或兼容开关。回滚前端helper/import会恢复当前页重复扫描，但不改变后端合同；不得以本优化替代PERF-064分页边界或恢复20k客户端聚合。

## PERF-072：作者认领审核稳定游标与附件批装配（generation136不变）

- Schema：无表、列、索引、constraint、trigger、回填、开发库重置或generation变化，保持136。主页精确复用`idx_creator_claims_queue(status,created_at,id)`；附件集合查询复用`creator_claim_attachments(claim_id,oss_file_id)`主键及OSS安全状态字段。临时完整Schema测试结束清理，远端public未访问。
- 请求：GET `/api/v1/admin/creator-claims`只接受单值`limit/cursor`。limit默认50、范围1..100；未知、重复、offset/page、非法limit与畸形/未知字段/跨limit cursor返回400。v1 cursor为opaque base64url JSON，scope绑定pending-author队列和limit，anchor为UTC `createdAt`及内部ID。
- 查询/响应：pending author按`created_at,id`升序，以排他`>` keyset读取`limit+1`并裁页；当前页内部claim ID再执行一次附件`ANY(bigint[])`查询。非空页恒2 SQL，附件只含`id/name/sizeBytes`且继续受active+clean/trusted安全谓词。响应新增`limit/hasMore/nextCursor`并保留`items`及item字段，不执行COUNT或OFFSET。
- 调用方：`CreatorClaimsPanel`默认请求50项，只保存一个页面及Previous cursor栈；Next/Previous替换items。加载由AbortController+generation隔离，审核后重载当前页并在空尾页回退。审核PATCH、附件presign与下载行为不变；省略分页参数的旧调用方仍取得首个有界50项页，但完整遍历必须跟随nextCursor。
- 性能/回滚：205条待审claim以100/100/5遍历、每个非空页2 SQL、5附件正确归组；临时100k深页Index Only Scan为0.068ms并只读51行。回滚会恢复无界响应、全量前端状态和逐claim附件查询，不保留旧第一方旁路。

## DB-001 / LEGACY-002：Outbox最终Schema直接定义（generation136不变）

- 逻辑Schema：最终`nats_outbox`仍为原20列，保留各列类型、nullable/default、`nats_outbox_status_check`五态约束、event ID唯一性与主键；最终三个业务索引仍为`idx_nats_outbox_pending(available_at,id)` partial、`idx_nats_outbox_status_created(status,created_at)`和`idx_nats_outbox_aggregate(aggregate_type,aggregate_id,id)`。
- DDL来源：上述最终列/约束/索引从`infrastructureSchemaStatements`的ALTER/UPDATE/DROP过渡合入`baselineSchemaStatements`的权威create block。删除旧10列形状、created_at-only pending索引、10个ADD COLUMN、2个空表回填、constraint drop/add及pending索引drop/recreate；没有保留双形状。
- 代次/数据：最终Schema指纹语义不变，generation保持136；无生产DDL升级、回填、重置或兼容迁移。既有迁移器继续拒绝非136旧库并对已安装136直接返回。真实测试只在session temporary Schema安装/清理，远端public仅只读代次且前后相同。
- API/队列：无HTTP、DTO、事件schema/payload、subject、Outbox enqueue或dispatcher状态机变化。生产者继续显式写event/type/schema/trace/status/time，dispatcher继续使用status/available/lease/attempt字段和同一索引。
- 验证/回滚：源码合同固定“一次最终定义、零同代迁移”；真实表验证20列/default/constraint/index及有效/非法插入。回滚会恢复空库冗余步骤和定义漂移风险，没有外部兼容价值，不应作为旧库迁移方案。

## DB-004：删除四个UNIQUE同序重复索引（generation136→137）

- Schema删除：`idx_resource_import_snapshots_resource(resource_id,revision_id)`、`idx_resource_import_snapshots_revision_registry(revision_id,registry,resource_id)`、`idx_recipe_layout_templates_type(recipe_type_id,template_key)`、`idx_mod_content_sections_tree(version_id,parent_id,ordinal)`。四者均为无谓词、无表达式、无include列的非唯一B-tree，逐列顺序与同表既有UNIQUE完全相同。
- Schema保留：四个UNIQUE约束及其自动唯一索引不变；`idx_resource_import_snapshots_history`、FK支撑索引、system-key partial unique及其他不同键序/谓词索引均不变。无表、列、constraint、trigger、数据或回填变化；DB-008所述第五项不在本次删除范围。
- 代次/部署：当前Schema generation从136升为137。迁移器是开发期空库安装器，不为136库提供在线DROP迁移；本地开发数据库必须重置后安装137，不保留重复索引兼容窗口。远端public generation只读核对且测试前后不变，无重置或永久写入。
- API/调用方：无HTTP路由、请求、DTO、响应、错误码或前后端调用方变化。仓库没有按四个删除名称执行hint、REINDEX、监控或运维动作；查询可继续使用UNIQUE自动索引或其他更合适的保留索引。
- 计划/回滚：真实完整临时Schema证明四组键各只有一个constraint-owned unique index，四类代表查询仍全部index-backed且无Seq Scan。回滚须恢复四个显式索引并协调新的generation，会重新增加写放大、WAL、磁盘与vacuum负担，不提供业务兼容价值。

## DB-006：项目自动镜像provider identity与上传补偿（generation137→138）

- Schema删除：`mirrored_project_files`的`unique(source_type,file_sha256,byte_size)`。SHA-256、byte size、OSS FK、status闭集及所有列不变；`oss_files(sha256,size_bytes)`等现有内容查找索引不变，无回填。
- Schema保留：`unique(source_type,external_file_id)`仍是供应商文件的唯一业务身份。同一source下相同字节但不同external file ID可分别绑定不同`project_route_id/oss_file_id`；同一provider file重复或并发导入仍由唯一约束拒绝第二身份。
- Worker协议：existing读取只把明确NoRows当作缺失，其他数据库错误发生在下载/上传前即返回。上传后的OSS记录+mirror登记仍在一个数据库事务；登记或commit失败进入独立15秒cleanup context，先按object key保护已登记对象，再删除未登记对象。
- 可靠补偿：直接OSS删除成功即完成；若OSS拒绝或暂时失败，独立事务向`oss_object_deletion_outbox`写入无FileID目标，reason=`project-automation-registration-failed`，由既有重试/dead-letter Worker处理。若注册核对或outbox也失败，错误与原登记失败通过`errors.Join`可观测返回，不伪装成功。
- API/调用方：自动更新HTTP设置、run状态、成功摘要`queuedForScan/existing/needsReview`、供应商请求、扫描提升和前端均不变；只收紧内部错误语义。无双写、兼容开关或跨项目内容共享模型。
- 部署/回滚：current generation升至138，本地开发库必须重置；真实测试只使用完整session临时Schema，远端public代次只读且前后不变。回滚必须同时恢复约束、移除补偿并协调generation，会恢复合法镜像冲突与OSS孤儿风险。

## DB-005：project file移除不可达generic route（generation138→139）

- Schema删除：`register_project_file_public_route`、`trg_project_files_public_route`、`remove_project_file_public_route`与`trg_project_files_remove_public_route`。不再向`public_routes`写`entity_type='project_file'`或伪canonical `/api/v1/project-files/{publicID}/download`。
- Schema保留：`project_files`表、public ID unique/default、project composite FK、OSS FK、active/deleted约束及两个列表/filter索引全部不变。`new_public_id()`继续先写`public_id_registry`，所以移除route trigger不降低跨实体ID唯一性；`oss_files`自身public route不受影响。
- API：真实POST `/api/v1/projects/{projectType}/{projectId}/files/{source}/{fileId}/download`、GET列表、DELETE、上传与DTO均不变。内部下载继续要求所属项目scope、active project file及active+clean OSS；soft-deleted文件保持404。不存在的generic URL从未是服务器合同，因此无需兼容redirect或410 handler。
- 数据/部署：generation升至139，无表/列/index/数据回填。开发期迁移器要求本地库重置；不对旧错误route做在线DELETE，因为当前项目按目标合同使用空库最终Schema。远端public仅只读代次且测试前后不变，无永久写入。
- 回滚：需恢复两个函数、两个trigger和新generation，会重新制造永远404的canonical path及soft-delete后残留route；不提供功能开关或双身份写入。

## DB-007 / DEAD-006：删除未使用的PostgreSQL chat presence表（generation139→140）

- Schema删除：`user_chat_presence`表及随表产生的user主键、conversation FK和两个NOT NULL时间事实。没有其他表、function、trigger或生产SQL引用该关系，无需重接FK。
- 运行时authority：`querycache.Cache.SetChatPresence/ChatPresence`继续使用Redis `chat-presence:user:{userID}` TTL键；Redis不可用时使用有界、按过期时间清理的进程内map。数据库从未参与该协议，因此本次不是存量数据迁移或缓存切换。
- API/调用方：消息conversation presence端点、请求/响应、TTL、metrics、Redis namespace和前端行为全部不变。删除表不增加双写、读取fallback或Schema兼容层。
- 部署：current generation升至140，本地开发库必须重置；真实完整临时Schema确认relation不存在。远端public只读代次且前后不变，无永久写、DROP或重置。
- 回滚：重新声明表只会恢复无消费者第二事实源和维护噪声；若未来需要durable presence，必须另行设计权威语义、TTL清理和一致性合同，不能复活该空表。

## DB-008：log share owner FK复用精确非空partial前缀（generation140→141）

- Schema删除：`idx_log_shares_owner_fk(owner_user_id)`；不删除`idx_log_shares_owner_created(owner_user_id,created_at desc,id desc) where owner_user_id is not null`。其他`idx_log_shares_source_file_fk`、expiry、active-source及约束索引保持不变。
- 自动索引协议：`foreignKeyIndexStatement`仍为每个缺失前缀的FK生成普通B-tree，但现在允许单列FK复用谓词精确为该FK列`IS NOT NULL`的valid/ready非表达式partial索引。列顺序、前缀长度和其余质量条件不变；复杂或业务状态partial索引不会被接受。
- 测试协议：`TestEveryForeignKeyHasLeadingIndex`改在当前完整会话临时Schema运行并使用同一判定，不再读取可能陈旧的public开发Schema。测试额外要求log share owner前缀精确一个、名称/谓词正确，并以禁用Seq Scan的`EXPLAIN`证明等值反查命中保留索引。
- 代次/数据：generation140→141；无表、列、FK、trigger、function、数据或回填变化。开发期空库安装器不提供140在线DROP迁移，本地开发库需重置；远端public只读代次前后不变，无永久写、DROP或重置。
- API/调用方/回滚：无HTTP、DTO、查询语义或前端迁移；owner历史读取和FK维护共享一份B-tree。回滚须恢复单列索引、旧自动发现规则及generation，会重新引入重复写/WAL/磁盘/vacuum开销。

## ARCH-002：后台与Handler模块边界（generation141不变）

- Schema/数据：无表、列、索引、constraint、trigger、function、回填或数据库访问；generation保持141，开发库无需因本项重置。
- 后端源码边界：八个超1500行`*_handlers.go`按现有领域声明拆分为同package文件；函数/方法签名、SQL、路由注册、错误语义及内部可见性不变。新测试遍历生产handler并固定1500行review上限。
- 前端源码边界：`AdminConsole`公开模块继续从`admin-console.tsx`导出；权限、基础设施/设置、用户/通知、OSS/日志和共享合同移到五个`admin-console-*`模块。props、API path/payload、状态、事件和页面路由不变；console固定1200行shell上限，admin模块固定2000行上限。
- 测试调用方：只把读取旧单文件的源码合同更新为组合对应领域文件；所有正向计数、必需片段和负向禁用断言保持。无运行时调用方迁移、兼容层、双入口或功能开关。
- 回滚：恢复原文件会重新聚合冲突/审查范围并使尺寸门失败；不得只删除门禁而保留超大模块。

## ARCH-005：用户统计读取错误语义（generation141不变）

- Schema/数据：无DDL、索引、回填或持久事实变化；generation保持141。读取继续使用`user_statistics_daily`与`user_statistics_totals`既有表/索引。
- streak读取：日期流的query、Scan和terminal `rows.Err()`任一失败均在计算/写入current与longest streak前返回；不再从部分日期集合构造成功事实。
- totals读取：`last_edit_at/last_comment_at`只把`pgx.ErrNoRows`解释为尚无totals记录；其他QueryRow/Scan错误传播。合法no-row及成功DTO字段结构不变。
- API/调用方：无路径、请求或成功响应迁移；异常从错误200变为既有顶层5xx。前端无需分支兼容，成长投影重放随后由ARCH-006在内部事务边界独立关闭。
- 回滚：恢复忽略错误会再次把数据库故障伪装为空/偏短统计；不提供兼容开关。

## ARCH-006：durable活动与成长投影原子确认（generation141不变）

- Schema/数据：无DDL、表、列、索引或回填；generation保持141。继续复用`activity_event_outbox`、`user_activity_events`、`user_task_progress`、经验/货币流水与角色来源表，远端public未访问或写入。
- 事务协议：durable drain在领取Outbox行的同一`pgx.Tx`内写raw、调用`ProjectionProcessor.ProcessActivityBatchTx`、删除已领取Outbox并commit；任一错误整体rollback，随后只在仍存在的Outbox上记录退避元数据。投影不会再发生在Outbox确认之后。
- 成长服务：active task、用户timezone、任务进度、完成标记、奖励和等级角色同步均使用调用者事务；独立best-effort入口仍自行事务。commit后hook删除受影响用户的短期权限版本缓存，不把Redis或第二次数据库读取失败解释为投影失败。
- 可重建边界：`record_site_activity_batch`仍在raw commit后best-effort运行，因为该站点日活投影已有周期校准来源；不可从无身份增量安全恢复的任务/成长/一次性奖励进入durable原子边界。view继续遵守既有显式lossy分类。
- API/调用方：活动记录、成长、任务及奖励HTTP DTO和前端调用均不变；内部`activity.NewMonitor`从函数回调改为接收实现事务/commit hook的`ProjectionProcessor`，运行时直接传入`progression.Service`。
- 部署/回滚：无需开发库重置或数据迁移。回滚代码会恢复“raw已提交、Outbox已删除、成长投影仅日志”的永久丢失窗口；不应保留双路径或功能开关。

## ARCH-007：反滥用副作用错误语义与保守降级（generation141不变）

- Schema/数据：无DDL、表、列、索引或回填；generation保持141。沿用`anti_abuse_restrictions`、`anti_abuse_user_states`、events/fingerprints/daily stats/bot rules既有结构；真实PG只使用单连接会话临时全Schema，远端public未访问。
- 自动安全状态：restriction和user state由同一事务提交，任一步失败整体回滚。账号缓存仅在commit后失效。自动限制/账户复核持久化失败不再返回原风控拒绝，改为503稳定code `anti_abuse_state_unavailable`、`Retry-After: 1`和无data响应；这是一项明确失败关闭协议。
- 遥测与清理：风险事件、Crawler事件和公开同步记录入口返回错误；成功请求异步event/fingerprint/cleanup用`errors.Join`归并。daily aggregate失败保留内存map，只有upsert成功才删除；关机排空超时才按事件数计入drop。既有队列容量、并发、截止时间和Close合同不变。
- 机器人规则：loader检查query、Scan、rows.Err、cache error及JSON；任何失败不缓存/返回部分集。`ClassifyCrawler`内部签名增加error；HTTP将错误分类为`SuspiciousBot`并写结构化日志，因此公开读取使用保守Crawler预算，受保护写入被既有Crawler写禁令拒绝。
- 运维/API：`/api/v1/admin/infrastructure/metrics`的`antiAbuse`对象新增`riskEventFailed`、`riskDailyFailed`、`restrictionFailed`、`botRuleLoadFailed`；既有字段及结构保持，前端当前无强类型迁移。普通成功及数据库健康时风控响应完全不变。
- 回滚：无需数据迁移或库重置；回滚会重新使自动限制部分/零写、规则部分集/空集和daily提前删除不可见，禁止保留吞错兼容路径。

## ARCH-008：活动清理原子进度与待审计恢复协议（generation141不变）

- Schema/数据：无DDL、表、列、索引或回填；generation保持141。复用`activity_cleanup_runs`现有`status/filters/deleted_count/started_at/finished_at/error_message/confirmation_hash`及活动事件表，远端public未访问或写入。
- 删除/进度事务：每个有界批次在一个`pgx.Tx`内先DELETE，再只对同一running run执行`deleted_count=deleted_count+batch`，两项共同commit；进度错误或影响行数不为1时rollback，确保每条已提交删除都有持久计数。最终响应取run中的累计总数，不把单次resume增量冒充总量。
- 终态与恢复：手工/自动清理统一检查`finalizeActivityCleanupRun`。Worker启动及每分钟读取最多20个超过1分钟的running记录，完整检查query/Scan/rows.Err，从持久过滤器幂等续删后重试completed；无效过滤器尝试记failed。running谓词让并发终结后到达的批次在进度阶段回滚。
- API/前端：POST `/api/v1/admin/activity-logs/cleanup/execute`的成功响应新增`status=completed`。删除已经提交而终态写失败时返回HTTP 202、code `ACTIVITY_CLEANUP_AUDIT_PENDING`、`status=audit_pending`、preview/matched/deleted计数；Next16调用方接受联合状态，并以中英文提示审计会自动修复且不得重复执行。健康路径的确认与清理交互不变。
- 故障/回滚：进度写失败不会提交对应DELETE；终态失败保留running而不清空恢复过滤器。回滚代码会恢复不可逆删除与审计永久分裂及伪completed响应，禁止保留旧吞错兼容路径；无需开发库重置。

## ARCH-010：目录编辑详情必需读取失败关闭（generation141不变）

- Schema/数据：无DDL、表、列、索引或回填；generation保持141。真实PG仅在单连接会话临时Schema中短暂重命名四个表注入读取故障，逐项恢复后清理；远端public未访问或写入。
- 读取协议：resource/tag/recipe type/template/recipe详情在输出前检查实体、主定义、计数、localizations、catalysts、slots/bindings、父recipe type active、published revision与review status。`catalogPendingReviewStatus`改为`(string,error)`，仅`pgx.ErrNoRows`产生approved；`catalogRecipeTypeIsActive`改为`(bool,error)`。
- API语义：健康成功DTO、字段及前端调用不变；明确实体/关联不存在继续404，明确无change request继续在成功体显示approved。数据库连接、Schema、Scan或迭代故障不再形成0、空数组、approved或伪404，统一以对应安全文案返回500。
- 可观测性：每个新增失败分支通过`logCatalogEditorReadFailure`记录catalog detail的精确查询上下文和底层错误；公开响应不包含数据库细节。访问日志同时保留5xx路径事实。
- 回滚：无需开发库重置或数据迁移；回滚会重新引入编辑员在不完整快照上操作及故障监控失真的风险，不提供吞错兼容开关。

## ARCH-013：自动化调度、必需状态与列表错误可见性（generation141不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、function、回填或持久数据迁移；generation保持141。真实验证的deferred trigger与表重命名仅存在于单连接会话临时Schema，测试后完整清理，远端public未访问或写入。
- 调度协议：项目自动更新与填充爬虫的scheduler helper均返回error，检查lease恢复、advisory lock、配置/到期行query/Scan/rows、run insert、配置推进与commit。后台循环记录首次和周期错误；明确未锁定、禁用、未到期或无待处理项仍为正常nil。
- 读取/状态协议：`querySimpleRows`改为返回数据和error，Query、单行Values或terminal cursor失败不返回部分数组；全部调用方在成功响应前检查。填充爬虫进一步检查每日导入计数、项目存在性、翻译task状态、草稿submitted状态、candidate失败状态和提交后的外部来源绑定。
- API/调用方：成功HTTP路径、请求、DTO、排序及前端调用均不变。自动化设置/run/概览和复用共享helper的后台端点遇数据库故障改为既有500；Worker故障进入日志而非静默漏建。内部helper签名变化不对外暴露，无双协议、兼容开关或前端迁移。
- 回滚：无需开发库重置。回滚会恢复commit/游标/关键状态故障的伪空、跳行或伪成功，令预算、重复检测和管理可观测性失真，不保留吞错兼容路径；OPS-005/006的租约与供应商终态仍按独立协议处理。

## BUG-060 / ARCH-014：任务配置启用门与奖励原子语义（generation141不变）

- Schema/数据：无DDL、表、列、索引、constraint、trigger、function、回填或generation变化；继续使用`task_definitions.condition/rewards` JSONB、`currencies` active状态、`user_task_progress`及既有经验/货币流水。真实PG夹具仅写会话临时完整Schema并在测试后清理，远端public未访问。
- 配置协议：共享强类型decoder验证condition动作/对象/指标/正目标和rewards非负经验/规范正额货币/至少一项奖励。active loader还在任何进度计算前批量确认全部引用货币存在且active；损坏或不可用配置返回带task ID的error，不跳过、不置空。
- 事务协议：进度、`completed_at/rewarded_at`、经验/货币balance与流水继续位于同一`pgx.Tx`；配置错误发生在delta前，grant错误触发整体rollback。durable活动投影沿ARCH-006保留Outbox身份重试，因此失败不会确认活动或永久丢奖励。
- API/调用方：合法任务保存/列表DTO、路径和前端不变。用户/后台任务读取损坏condition/rewards改为记录上下文并500；保存端currency权威查询故障改500，明确未知currency仍400。内部新增共享验证入口，无双读、兼容开关或前端迁移。
- 回滚：无需开发库重置。回滚会恢复损坏condition静默消失、损坏rewards被当成零奖励并永久标记领取的行为；不得保留吞错模式。TEST-031的成长并发/角色/缓存完整专项仍须单独实现。

## ARCH-015：基础设施持久指标失败语义（generation141不变）

- Schema/数据：无DDL、表、列、索引、约束、回填或generation变化；读取继续覆盖`nats_outbox`、`dead_letter_events`、`oss_object_deletion_outbox`、`oss_rehome_jobs`和`oss_multipart_sessions`。真实测试只创建会话临时影子表/重命名其列，远端public未写入。
- 读取协议：六项数据库事实由一次typed helper聚合；QueryRow/Scan任一错误返回零内部对象和error，handler在读取/组合进程内queue、Redis、反滥用、访问日志与realtime指标前失败。健康时保持同一采样响应。
- API/调用方：GET基础设施指标的成功路径、字段、数值类型与前端调用不变；持久指标不可用时从伪零200改为稳定500并记录结构化底层错误。没有nullable unknown字段、部分200、双版本或兼容开关。
- 部署/回滚：无需开发库重置、数据迁移或协调前端部署。回滚会重新把数据库故障显示为零Outbox/死信/补偿任务，掩盖可靠性事故，不保留旧行为。

## ARCH-016：Minecraft版本配置读取失败语义（generation141不变）

- Schema/数据：无DDL、表、列、索引、约束、回填或generation变化；继续使用`system_settings.minecraft.versions` JSONB。真实测试只创建会话临时影子表并重命名其临时列，public Schema/数据未写入，远端public未访问。
- 读取协议：loader返回config/error；只有明确`pgx.ErrNoRows`表示未配置并使用现有默认。Query/Scan、JSON类型/shape或normalize失败返回typed unavailable错误和零内部config，所有调用方必须处理。
- API/调用方：健康与未配置成功DTO不变；公开版本目录和管理员保存前读取损坏时返回500并记日志。同步对内部配置失败500、外部source失败502；recipe、mod-content和项目自动化内部消费者均传播，不再局部回退。无需前端迁移或双协议。
- 边界/回滚：默认目录真实性属于BUG-062，具体loader artifact快照属于ARCH-017，跨实例同步与缓存分别属于OPS-012/PERF-039，均未由本项替代。无需开发库重置；回滚会恢复配置故障伪装完整目录，不提供兼容开关。

## ARCH-017：同步绑定的Minecraft loader artifact权威（generation142）

- Schema：新增`minecraft_loader_artifact_versions(catalog_hash,minecraft_version,loader_type,loader_version,source_url,observed_at)`。主键为`(catalog_hash,minecraft_version,loader_type)`；`catalog_hash`限制64位小写SHA-256，loader限制`neoforge|fabric|forge`，Minecraft/loader版本、来源URL与观测时间均非空且有长度边界。没有独立代理主键、未绑定JSON blob或可变latest指针。
- 代次/数据：`schemaGeneration`从141提升为142，全部当前代次合同同步迁移。项目仍采用空库式pre-production安装，不为旧开发数据回填、双写或在线ALTER；现有开发库须重置。远端public generation85未连接、迁移或写入。
- 持久事务：同步对MRPack loader兼容集合形成语义hash，先验证每个可选tuple都有唯一完整provenance，再在单一`pgx.Tx`中upsert`system_settings.minecraft.versions`、删除旧快照并以`COPY`批写新行。任一操作失败rollback设置和快照。管理员手工保存设置在同一事务删除不同hash行；纯展示/名称/非MRPack变化不改变hash。
- 来源/selector：同步对Fabric、Forge、NeoForge从有界metadata目录批量选择具体版本，复用PERF-039的15分钟URL缓存、singleflight、条件请求、16MiB单响应、64项/64MiB和4路外呼预算。metadata可读但某tuple无匹配artifact时，该Minecraft版本从对应loader兼容集合移除并记录失败状态；来源整体失败不发布半份新目录。
- 导出协议：`buildFavoriteModpackExportPreview`不再调用实时resolver。它严格读取当前Minecraft设置、计算compatibility hash并查询持久三元主键；预检和创建随后把同一持久`loader_version`写入任务。缺tuple返回HTTP 422/code `MODPACK_EXPORT_LOADER_SNAPSHOT_UNAVAILABLE`；设置或artifact authority读取故障返回HTTP 500/code `MODPACK_EXPORT_CATALOG_UNAVAILABLE`；其他供应商文件解析故障仍走既有502。
- 成功DTO/调用方：公开Minecraft版本及MRPack preview/task字段不新增或删除；同步后的MRPack loader `versions`现在只列实际有具体artifact快照的项，第一方picker自动消费该既有字段。无需前端双协议或迁移。已成功同步后导出不访问loader上游，源离线不改变结果。
- 删除/回滚：删除生产死代码`resolveMRPackLoaderVersion`、逐选择远端下载、256项具体版本缓存及两条已失效缓存测试；同步层共享来源缓存继续存在并有当前测试。回滚必须回到旧generation并重置开发库，且会恢复请求时上游依赖与两套权威，不提供live fallback开关。BUG-063/065、OPS-012、MAP-009、TEST-033仍按各自边界开放。

## ARCH-018：MRPack收藏导出读取与Worker失败语义（generation142不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化，保持142。真实PG验证只创建会话临时影子表并临时修改列/trigger注入损坏JSON、Scan、查询和状态竞争，未访问或写入远端public。
- HTTP读取协议：任务详情的主行只把`pgx.ErrNoRows`映射404，逐项`Scan`、`dependency_of` JSON和terminal cursor任一失败记录上下文并500；下载同样只把明确缺行映射410，数据库/Schema故障500，存在但非ready/已过期继续410。成功DTO、文件响应和第一方前端调用不变。
- Worker协议：`processPending`、`expireCompleted`、`failExhaustedLeases`和`fail`返回error；周期`scan`在启动和tick记录。全部Query/Scan/rows、事务、更新、`RowsAffected`、lease/status CAS和通知失败均传播；待处理/过期/耗尽行先有界收集并关闭游标，再执行嵌套查询或通知。
- 报告/状态：report item流任一失败不会继续构造部分包，而以当前lease写`REPORT_LOAD_FAILED`；retry/terminal写回要求仍持有lease且恰好更新一行。持久化/通知失败与原始原因联合返回，ready竞争同样返回错误。既有attempt、backoff、错误code和终态DTO不变。
- 兼容/回滚：无需开发库重置、前端协调部署、双读或版本化协议。OPS-013的上传后孤儿补偿、BUG-069结果一致性、DEAD-011 report snapshot消费及TEST-034完整集成矩阵保持独立。回滚会恢复伪404/410、部分报告、静默Worker停摆和状态竞争误判，不保留兼容开关。

## ARCH-019：蓝图派生详情和预览读取失败语义（generation142不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化，保持142。真实PG测试仅创建会话临时影子表、临时改名列或故意省略依赖关系；远端public未连接、迁移或写入。
- 材料/asset协议：材料properties JSON、当前namespace revision Query/Scan/rows及批量resource resolver全部为必需成功步骤；任一失败返回nil/error。asset revision helper改为`([]map[string]any,error)`并检查Query/Scan/rows，详情在写成功DTO前检查。合法单项未解析仍使用既有block ID fallback，不与系统错误混同。
- cover/render协议：cover只有明确no-row返回204，成功行空object key或数据库故障500；normalized render-data只有明确no-row或成功读取后的空key返回404，查询故障500。所有新增内部失败用统一结构化module/public ID/stage/error日志，公开文案不含数据库细节。
- API/调用方：健康蓝图详情、cover、render-data路径、字段、内容类型、缓存头与前端调用不变；变化仅是原伪空/部分200/204/404改为稳定500。`blueprintAssetRevisions`唯一内部调用方同步迁移，无版本化DTO、双协议或协调部署。
- 边界/回滚：PERF-043的namespace范围/索引已独立关闭；SEC-027、BUG-075..078、OPS-014/015、PERF-042和TEST-036仍分别负责转换权限、状态、租约、OSS补偿、内存预算和完整系统矩阵。无需开发库重置；回滚会恢复数据库故障的伪缺失和部分详情，不保留降级开关。

## ARCH-020：OSS读取、运维事实与未登记对象清理协议（generation142不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化，保持142。复用`oss_object_deletion_outbox`的nullable `oss_file_id`、完整provider target、唯一`(bucket,endpoint,object_key)`、租约/重试/dead/replay列；真实PG只用会话临时影子表和改列故障，远端public未连接或写入。
- 读取协议：OSS文件列表必须完成整个row stream；精确hash、蓝图复用、完成上传恢复和举报证据幂等仅把`pgx.ErrNoRows`视为不存在，其他Query/Scan错误在继续签名、登记或返回成功前传播。PERF-044的generation127额度桶/预留/结算保持唯一权威，本项不恢复历史SUM。
- 运维写协议：上传日志、扫描日志和下载计数保持best-effort，不使已成功的主业务因遥测不可用而回滚；失败不再丢弃，而以结构化module/kind/object/error日志及`uploadLogFailures/scanLogFailures/downloadStatFailures`原子计数暴露。管理员`GET /api/v1/admin/infrastructure/metrics`新增`ossWrites`对象，并含`deletionEnqueueFailures`。
- 删除协议：请求路径、图片转换和图库迁址不直接调用provider delete。未登记目标在独立15秒context中先查注册状态并幂等写Outbox；Worker对nullable-file job在provider调用前再次按object key检查非deleted注册，解决排队后晚登记和commit结果不确定。enqueue失败只影响补偿可用性但必须记录/计数；provider失败继续由既有可靠Worker有限重试和dead/replay处理。
- API/兼容/回滚：健康OSS目录、预签名、完成、下载和文件DTO不变；数据库故障从伪空/未命中改500，metrics为向后兼容新增字段。内部lookup增加error返回且全部调用方迁移。无需开发库重置、双读或协调前端部署；回滚会恢复重复上传、缺失审计和无持久删除重试，不保留直接删除分支。

## ARCH-021：后端生成OSS对象登记补偿协议（generation142不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化，保持142。生成对象登记失败复用`oss_object_deletion_outbox`的nullable `oss_file_id`、provider target唯一键、有限重试/dead/replay；真实PG仅使用临时表、故障trigger和临时列改名，远端public未访问。
- 写入协议：`writeGeneratedOSSObject`的成功仍要求provider PUT及`oss_files` upsert都成功。PUT后登记失败不再直接DELETE，而以`generated-object-registration-failed`排队持久删除任务；执行期再次检查object key没有非deleted登记，处理commit不确定或晚登记。健康返回file ID不变。
- 失败协议：登记失败始终向MRPack/sticker调用方返回带上下文error。Outbox成功只保证补偿、不改变业务失败；Outbox失败通过`deletionEnqueueFailures`和结构化日志可见，并与登记错误联合返回。对象provider删除由可靠Worker执行，不占请求/业务Worker的一次性重试窗口。
- API/兼容/回滚：无HTTP路由、请求、响应或前端变化；内部未登记cleanup helper新增error返回，现有best-effort调用可丢弃返回，生成writer显式检查。无需开发库重置、双读或协调部署；回滚会恢复无持久事实的一次性DELETE及孤儿窗口，不保留兼容开关。

## ARCH-022：评论列表完整读取与故障语义（generation142不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化，保持142。继续使用generation128的`comment_target_counts`、`comment_target_author_counts`及根/回复/watch复合索引；完整Schema测试只在会话临时namespace运行，远端public未访问。
- 读取协议：根评论、直接回复和我的watch page必须完成Query、每行Scan及terminal cursor后才进入详情装配；评论详情、权限、附件、目标批解析任一步失败均返回error/500。可见总数读取保留`(int64,error)`并新增内部queryer入口供故障注入，不把Scan错误当0。
- 过滤边界：成功查询后明确被拉黑/不可见的评论或target identity仍按现有产品规则过滤或type-only fallback；数据库、Schema或解码错误永远不是缺失事实。SEC-031将独立决定子路由和watch目标不可见时是否必须整体隐藏正文，本项不修改授权合同。
- API/兼容/回滚：健康响应的`items/total/target/nextCursor/capabilities`、watch DTO、opaque cursor和状态码不变；变化只确认系统故障稳定5xx。无前端、双读或协调部署；回滚会删除专用错误注入验收并弱化total test seam，不提供兼容价值。

## ARCH-023：收藏membership完整读取与故障语义（generation142不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化，保持142。继续使用generation130收藏夹/收藏项keyset与target-collection membership索引；百万行回归只运行隔离临时Schema，远端public未访问。
- 读取协议：私有/公开collection页、item页和POST membership summary分别通过集中collector完成逐行Scan和terminal `rows.Err()`，并在所有返回路径关闭rows。Scan、Schema或中途cursor故障返回nil或零值+error，handler在写任何部分DTO前500。
- API/调用方：健康`items/limit/hasMore/nextCursor`和`entityPublicIds/collectionIdsByEntity`字段、路由及第一方调用不变；PERF-051删除的旧无界membership GET保持删除，不提供双协议。变化仅是当前有界读取的系统故障必定5xx。
- 边界/回滚：ARCH-024继续处理collection create/update/delete写入错误分类，BUG-089处理旧PUT的无效collection ID，SEC-036、LEGACY-018、STYLE-006和TEST-041保持独立。无需开发库重置或协调部署；回滚会删除共享collector故障门并重新分散row-stream规则，不提供兼容价值。

## ARCH-024：收藏夹写入错误分类协议（generation142不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化，保持142。真实PG验证仅创建会话临时表、唯一索引和注入XX000的临时trigger，远端public未访问或写入。
- create/update协议：create只把已识别unique violation映射重名409；update把unique映射409、明确`pgx.ErrNoRows`映射404；所有其他QueryRow/Scan错误以结构化module/operation/collection/user/error记录并500。健康201/200及DTO不变。
- delete协议：从Exec+`RowsAffected`改为单语句`DELETE ... AND NOT is_default RETURNING public_id`。成功204；默认、跨用户或不存在形成NoRows并保持409；执行/trigger/Scan故障500。没有先查后删、额外往返或竞态窗口。
- API/兼容/回滚：请求字段、成功响应和已定义业务错误文案不变，只有过去错误分类的数据库故障由伪409/404改为5xx；前端既有通用错误边界可直接处理。BUG-089、SEC-036、LEGACY-018、STYLE-006和TEST-041独立开放；无需开发库重置或协调部署，回滚会恢复错误分类缺陷。

## ARCH-025：社区数据与翻译结果失败协议（generation142不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化，保持142。真实PG仅使用会话临时表、合法jsonb标量和临时列改名；百万目录使用隔离临时Schema，远端public未访问。
- 目录/引用协议：PERF-052后的keyset目录继续无COUNT/正文/OFFSET，并在Query/Scan/rows、bounty或引用批装配失败时记录stage后500。resource names JSON必须解码为语言map，损坏shape返回error；共享项目可见性及资源批量SQL不变。
- 翻译请求/结果协议：source/cache只有明确NoRows走原400/继续排队逻辑，其他故障500。任务只有NoRows或owner不匹配404；completed状态必须有合法post/locale/revision payload和匹配translation行，损坏或缺失500。健康200/202字段不变。
- 更新/Worker协议：update lookup只对NoRows404，apply和commit错误分别记录后500；Worker与结果GET复用严格community payload decoder，来源revision数据库错误不再伪装“内容已变化”。无前端迁移或双协议；ARCH-026、BUG-090/091、TEST-042独立开放，回滚会恢复伪空/伪completed结果。

## ARCH-026：AI业务结果先于完成态协议（generation142不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化，保持142。复用ai_tasks状态、stale-running恢复、content revision幂等和notification唯一upsert；真实PG只使用临时表/trigger/列故障，远端public未访问。
- Worker协议：translation payload/scope/result及逐项key/text必须严格有效，notification persistence返回upsert error。community/catalog/notification业务结果成功后才以running绑定CAS写result/tokens/cost/completed并要求一行；失败状态更新同样检查error和一行，故障结构化记录并联合返回。
- 结果API：catalog与notification completed结果必须通过和Worker一致的payload/locale/items验证，否则500；running等非完成态保持原状态响应。notification GET删除重复upsert，成为纯读取，不再改变translation或created_at。健康字段和权限状态码不变。
- 补偿/回滚：业务写后完成CAS前崩溃保留running，由既有stale recovery重新投递；三类业务写均幂等，重投可安全完成。OPS-019继续负责failed历史和完整重试政策，BUG-094/LEGACY-019/TEST-043独立；无需开发库重置或前端协调，回滚会恢复伪completed和GET补写。

## ARCH-027：经济与任务读取完整性协议（generation142不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化，保持142。真实PG有效证据全部显式绑定本机127.0.0.1:55432并使用隔离Schema；故障只通过隔离表改名和合法jsonb null形成，无远端数据库写入。
- 数据库读取协议：余额和后台活动列表只有Query、所有Scan及terminal cursor完整成功后才返回；经验与check-in历史只对明确NoRows使用业务默认，时区、Schema和连接故障500。库存NoRows/零数量保持409，其他QueryRow错误500。
- JSON协议：economy config只有设置NoRows使用默认；已存在配置及currency/shop/task持久JSON必须为object并可解码，损坏数据返回error/500。任务condition/rewards继续由共享严格配置validator验证，商品使用不能把坏config回退默认功率。
- API/兼容/回滚：健康`balances/experience/level/timezone/checkin/inventory`、经济配置、currency/shop/task和活动字段、路由及第一方调用不变；变化只将系统故障的伪0/空对象/部分200改为5xx。无开发库重置、双读或协调部署；回滚会恢复静默降级，不提供兼容价值。

## ARCH-029：站点更新读取完整性协议（generation142不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化，保持142；继续使用generation134公开partial date-ID和后台date-ID索引。真实PG只创建会话临时表/临时列改名；百万测试显式使用本机测试URL并完整清理。
- 页读取协议：公开和后台keyset页各由集中collector完成全部Scan、terminal cursor和close；后台翻译aggregate必须通过object shape及typed locale map解码。只有完整`limit+1`结果可以产生items/hasMore/nextCursor，系统故障500并带stage日志。
- 详情协议：后台详情通过queryer loader读取DATE、状态和aggregate；DATE以`time.Time`扫描后规范为`YYYY-MM-DD`，translations返回typed map。只有NoRows映射404，Query/Scan/JSON shape错误记录并500，不再返回unchecked RawMessage。
- API/兼容/回滚：公开/后台页和详情的健康字段、JSON shape、cursor及第一方调用不变，无前端协调、双读或开发库重置。PERF-058、BUG-101、TEST-046责任独立；回滚会恢复聚合穿透和较弱的row-stream验收，不提供兼容价值。

## ARCH-030：治理读取完整性协议（generation142不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化，保持142；继续使用generation136的reporter/queue/ban tuple-keyset索引。真实PG只创建单连接会话临时表并临时改列，百万fixture显式使用本机测试URL，远端public未访问或写入。
- 页读取协议：本人举报、后台举报和公开/后台小黑屋页分别由集中collector完成全部Scan、terminal cursor及close；只有完整`limit+1`结果可以裁页并产生`items/hasMore/nextCursor`。Query或row-stream故障结构化记录stage/scope后500，不把连接中断解释为页结束。
- 详情协议：授权快照必须为非空JSON object并可解码；null、数组、标量或坏JSON记录snapshot stage后500。主行只有明确NoRows映射404；证据、审核、动作和关联举报继续复用ARCH-013的`querySimpleRows(items,error)`，任一Query/Values/terminal故障记录独立stage并500。
- API/兼容/回滚：健康三类页和后台详情字段、JSON shape、cursor、权限过滤及第一方调用不变，无前端协调、双读或开发库重置。PERF-060、ARCH-013和TEST-048责任独立；回滚会恢复快照伪空及较弱的治理读取告警，不提供兼容价值。

## ARCH-031：封禁理由客户端资源状态协议（generation142不变）

- Schema/后端：无DDL、表、列、索引、约束、trigger、回填、generation、路由或DTO变化，保持142。GET `/api/v1/admin/ban-reasons?locale=...`继续返回`{items}`，后端权限和错误码不变，无数据库迁移或重置。
- 客户端状态：hook从裸`BanReason[]`改为`items/loading/error/reload`；locale/token/retry attempt共同绑定请求代次，旧请求取消且旧scope数据不可显示。成功空数组与错误分别表示`loading=false,error=""`和`loading=false,error!=empty`。
- UI/调用方：举报审核和封禁创建复用加载/错误/重试UI；未知状态禁用selector，举报仅禁用附带封禁字段而保留无ban审核，封禁创建按钮失败关闭。新增中英主locale文案，其他locale按既有fallback继承。
- 兼容/回滚：成功目录、表单payload和健康提交完全不变，无双协议或协调部署。TEST-048继续负责完整治理权限与行为矩阵；回滚会恢复系统故障伪空目录及不可重试状态，不提供兼容价值。

## ARCH-032：合成表导入与公开渲染JSON对象协议（generation142不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化，保持142。真实PG验证只建立单连接会话临时影子表并写入合法jsonb的null/数组/标量；没有访问、迁移或写入远端public。
- 导入协议：JEI模板collection的canvas/image_pixels/content、slot rect/visual_rect，以及配方parameters/binding chance_texts只允许省略或显式JSON object。省略继续由既有`nonEmptyJSON`产生默认`{}`；显式null、数组、字符串或数字在decode阶段失败。queue阶段重复执行同一shape验证，保护内部构造/promotion路径。
- render协议：权威recipe/slot/binding/candidate definition及导入parameters、template layout、slot data/rect/visual_rect、candidate chance_texts均通过共享严格object decoder。语法错误、空值和非对象shape传播error，handler按既有错误边界返回5xx，不再以`{}`完成200。明确null的可选layout override仍表示没有覆盖。
- API/调用方：健康公开recipe render字段、schema_version、slot/alternative shape、路由和前端调用不变；变化仅是畸形新导入被拒绝、历史损坏存储失败可见。无需前端协调、双读、开发库重置或版本化DTO；回滚会恢复fingerprint与API语义分裂和静默损坏，不提供兼容价值。

## BUG-025 / BUG-026 / LEGACY-007：资料板块单一写权威协议（generation142不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化，保持142。真实PG仅使用单连接会话临时表验证两版本、active/pending根和本地化；未访问、迁移或写入远端public。
- 路由协议：删除`PUT /api/v1/mods/{siteId}/content-sections/{sectionId}`，该方法现在由ServeMux返回405且不保留旧名转发。POST collection仍创建根，DELETE item仍递归归档子树，GET资源/布局与PATCH layout路径不变。
- 创建/发布协议：POST section只接受`parentPublicId=""`且`resources=[]`的空根。发布只接受section create，并锁定同mod、同snapshot目标version且pending的预留行；激活不更新version_id。遗留section edit、带树/placement的create或版本不匹配快照失败，不产生部分更新。
- 资源归属：删除旧section循环中按`game_resources.owner_mod_id`逐项选择和最多2万次替换的协议。所有分类与resource placement只能由有界layout PATCH发布，并继续以`mod_resource_bindings`为当前Mod归属唯一权威；owner为空的合法全局资源不再遇到第二套拒绝规则。
- 调用方/兼容/回滚：第一方原本只发送空根POST、DELETE和PATCH layout，无迁移；开发期无已发布外部客户端责任，不提供双协议。旧待审edit可被审核拒绝但不能发布。回滚会恢复持久跨版本树、归属分裂与无界逐项旧路径，无兼容收益。

## BUG-027：跨模块row stream成功边界（generation142不变）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化，保持142。真实PG只建立单连接会话临时`minecraft_servers`并注入Scan类型错误；未访问、迁移或写入远端public。
- 读取协议：资料版本/模板/板块/资源、简单项目/整合包关联、项目关注和审核事件的Query必须完成每行Scan、terminal cursor及close后才能返回或入队；系统故障由部分200/部分事件改为5xx/error。
- Worker/claim协议：服务器探测在事务内读取全部claim，terminal成功后commit，commit后才启动探测；项目更新pending/recipient及广播recipient先完整收集再处理。Scan、cursor或commit失败记录并返回，等待既有调度/事务重试，不产生部分外部副作用。
- API/调用方/回滚：健康字段、cursor、路由、通知模板、调度周期与探测结果不变；NoRows业务缺失语义不变，无前端迁移、双协议或开发库重置。回滚会恢复数据库中断被解释为合法短页、漏通知或已claim未探测，不提供兼容收益。

## BUG-029：整合包主分类权威CHECK（generation142→143）

- Schema/数据：`modpacks.primary_category`新增命名`modpacks_primary_category_check`，允许共享后端注册表中的16个产品分类。没有表/列/索引/trigger或回填；generation143继续采用预生产开发库不兼容即重置策略，不升级旧generation或猜测修复已有非法值。
- 应用协议：创建、普通修订和导入草稿共用的规范化在空值默认`adventure`后验证PrimaryCategory；Tags和目录primary/tags过滤map由同一`catalogpolicy`注册表派生。非法请求在数据库前400，直接SQL/未来内部旁路由CHECK拒绝。
- 调用方/兼容：前端既有16项category option、中英i18n、默认`adventure`、健康DTO及目录filter参数不变，无协调部署或双协议。新的唯一行为是拒绝未登记字符串；查询SQL和执行计划不变。
- 验证/隔离/回滚：本机单连接会话临时namespace完整安装generation143，16类逐项持久化且未知值精确命中23514 constraint；未访问或写入public/远端数据库。回滚必须同时移除应用校验和CHECK并会恢复孤儿分类，无兼容价值。

## BUG-030：服务器模组声明/证据分离协议（关闭时generation143；现由BUG-031 generation144承载）

- Schema/数据：BUG-030关闭时本身无DDL、回填或generation变化，继续使用generation143单行来源；紧随其后的BUG-031已在generation144把来源迁至独立evidence表，当前权威Schema见下一节。BUG-030的客户端信任边界不因存储强化而变化。
- 写协议：POST/PATCH的`mods`项由`{id,version?,source,confidence}`收紧为`{id,version?}`，strict decoder对旧证据字段返回400。用户声明持久化固定manual/declared；创建时后端重新探测、调度探测和已持久非manual行是trusted evidence的唯一来源。
- 更新协议：BUG-030关闭时在server行锁中只保留所选ID的既有非manual证据；BUG-031现将其强化为编辑仅替换manual evidence、机器证据完全独立。两种实现都确保新声明不能借详情回显取得机器来源，当前行为以下一节为准。
- 前端/兼容/回滚：probe/detail响应继续包含source/confidence；`CreateServerRequest`/`UpdateServerRequest`改用`ServerModDeclaration[]`，向导仅序列化ID/version。第一方同步迁移；不接受旧伪造字段的兼容忽略。回滚会重新把响应证据当写权限并破坏来源真实性，无兼容价值。

## BUG-031：服务器模组多来源快照（generation143→144）

- Schema/数据：`minecraft_server_mods`保留`id/server_id/mod_id/raw_mod_id/created_at`及`unique(server_id,raw_mod_id)`身份，移除单值version/source/confidence。新增`minecraft_server_mod_evidence(server_mod_id,source,version,confidence,observed_at)`，`primary key(server_mod_id,source)`同时覆盖级联外键；命名CHECK语义要求manual只能declared、机器来源只能exact/high/inferred；`idx_minecraft_server_mod_evidence_source(source,server_mod_id) where source<>'manual'`支撑机器快照撤销。
- 写入/事务：共享insert先批量upsert唯一身份和解析结果，再批量upsert每个独立来源并维护父身份的unresolved reference。完整probe在已取得server行锁的同一事务删除旧非manual evidence、写本次集合、删除零来源身份；不完整probe只upsert。编辑事务只替换manual evidence并清零来源身份，不能覆盖或删除机器快照。
- 读取/查询计划：目录筛选、审核计数和解析队列继续连接一行父身份，无多来源重复计数。详情以每个父行的主键范围lateral选择confidence/source优先级最高的证据，并在其version为空时从同父其他非空来源回退；健康DTO仍为一个`{id,version,source,confidence,...}`。新FK由证据主键覆盖，全Schema FK审计通过。
- API/调用方：POST/PATCH延续BUG-030的`ServerModDeclaration[]{id,version?}`，probe响应和详情响应无字段或状态码变化。行为变化仅是完整成功探测会移除已消失的机器-only模组；同时声明的模组降回manual/declared，不完整清单保持上次事实。
- 重置/兼容/回滚：generation143开发库不升级，专用本机`127.0.0.1:55432/postgres`以显式`RESET postgres`保护条件重建为144并seed，服务未停止；无远端/生产操作、回填、双读或双写。回滚到单行来源会重新丢失多来源事实并恢复历史并集，无兼容价值。

## BUG-032：项目文件扫描门控发布（generation144→145）

- Schema/状态：`project_files.status`默认由active改为processing，闭集扩为processing/active/rejected/deleted；新增非负`publication_generation integer default 0`。clean/trusted首次发布为generation1，每次隔离后重新发布递增，删除不重置。现有`idx_project_files_project_published`和active filters索引不变。
- 数据库安全：新增`ensure_project_file_publication_safe` BEFORE INSERT/UPDATE trigger；任何active关系必须指向`oss_files.status='active'`且scan_status为clean/trusted_generated，否则以约束名`project_files_active_oss_scan_check`的23514拒绝。该防线覆盖应用以外的内部或直接SQL旁路。
- 创建协议：项目文件POST在同一事务`FOR UPDATE`目标OSS，要求owner/category/extension及scan处于pending/clean/trusted闭集。pending写processing且不发公开事件；clean/trusted写active、generation1并在已审核项目产生add。201响应继续为既有ProjectFile shape并带scanStatus，第一方上传成功文案已说明扫描通过后才发布。
- 扫描/事件协议：管理员scan事务在更新OSS后调用关系同步；clean/trusted激活并发`download_added`，pending/rejected撤下active并发`download_removed`，初始processing→rejected不产生公开事件。事件batch键包含kind/publicID/generation；任一步失败回滚OSS、关系、audit、notification task和NATS outbox。
- 读取/删除：站内列表、filters和下载共同要求关系active与OSS active+clean/trusted，pending/rejected不可见不可下载。删除允许未删除的processing/active/rejected，只有原状态active发remove。100k keyset查询继续使用原project/status/created/id索引，新增安全join predicate不制造OFFSET或额外查询。
- 兼容/重置/回滚：无路由、成功字段或前端写payload变化；前端仅把trusted_generated与clean同视为可下载并新增源码回归。generation144开发库不升级，本机55432测试库以显式保护确认重建/seed为145，服务未停止；无远端/生产回填、双读或双写。回滚会重新允许安全状态与发布事件分裂，无兼容价值。

## BUG-033：项目Minecraft版本目录权威（generation145不变）

- Schema/数据：无DDL、索引、约束、回填或generation变化；`project_files.game_versions`与`project_changelogs.minecraft_versions`继续是text[]内部事实列。供应商原始数组保留在`mirrored_project_files.metadata`或`external_release_bindings.metadata.rawMinecraftVersions`，未映射项另存`unmappedMinecraftVersions`，不写入站内兼容数组。
- 手工写协议：文件POST及更新日志POST/PUT把最多100个提交值与`loadMinecraftVersionConfig().versions[].code`精确核对；未知、超量或零识别值返回400，目录读取/解码/规范化故障返回500。日志revision在创建前校验，并在`applyProjectChangelogSnapshotTx`审核/发布事务中重验当前目录，覆盖待审期间目录变化。
- 自动写协议：外部release显式版本或项目回退版本先分为resolved/unmapped；unknown-only不创建日志并增加`skippedUnknownMinecraftVersions`，mixed只发布resolved。同步签名包含正文与兼容metadata，使上游版本事实变化即使正文不变也会更新。clean镜像unknown-only转review，mixed项目文件只写resolved，原始provider JSON不变。
- API/调用方：健康ProjectFile/ProjectChangelog DTO、路由、成功状态和第一方payload不变；手工未知值从成功收紧为400。自动任务结果新增一个跳过计数，binding metadata新增raw/unmapped字段，均为向后兼容的附加事实；前端无需协调修改。
- 隔离/回滚：真实测试只在本机单连接临时完整Schema运行并删除，public generation保持145，未重置专用库、未访问远端/生产。回滚会重新允许拼写错误、未来标签和`unspecified`污染过滤、收藏夹mrpack预检及展示，不提供兼容价值。

## BUG-034：更新日志人工覆盖批准血缘（generation145→146）

- Schema/约束：`external_release_bindings`新增nullable `manual_override_revision_id bigint references content_revisions(id) on delete restrict`与`manual_override_source text not null default ''`。命名CHECK要求`manual_override=false`时revision必须null且source为空，true时revision非null且source非空；`idx_external_release_bindings_manual_override_revision` partial leading index覆盖FK删除/审计查询。
- 写入协议：项目changelog PUT不再在提交revision后直接更新binding。`applyProjectChangelogSnapshotTx`完成批准snapshot与本地化写入后，以同一事务把尚未override的source-managed binding绑定到aggregate匹配且`source='user'`的revision；自动revision、pending、rejected、conflicted及withdrawn均无法触发。首次批准来源不被后续人工编辑轮换。
- Worker/读取：Worker继续读取既有`manual_override`布尔投影；false照常应用上游正文，true继续产生`manualOverrideConflicts`并保存上游变化metadata。无需改变任务响应或查询协议，新revision/source列只提供完整来源审计并由数据库约束保证布尔投影不孤立。
- 审核/API：changelog创建/编辑的review config改为在已开始的同一tx读取，消除单连接等待和配置/revision事务分裂。PUT与review的成功状态、DTO、路由、通知和前端交互不变；行为变化仅是pending/rejected不再提前阻止自动同步。
- 重置/兼容/回滚：generation145开发库不升级；专用本机55432 development库经精确保护确认重建/seed为146，服务未停止。无生产/远端操作、历史回填、双读或双写；当前预生产基线无需为缺失来源的旧true值猜测血缘。回滚会重新允许拒绝内容永久改变自动来源控制权，无兼容价值。

## BUG-035：自动镜像项目文件发布事务（generation146保持）

- Schema：无DDL。镜像clean提升显式写`project_files.status='active'`和`publication_generation=1`，不依赖默认processing；继续使用generation145引入的OSS安全trigger、文件唯一键，以及既有`project_update_events(project_route_id,publication_batch_id)`幂等约束。
- 写协议：事务先锁mirror，再对对应mods/modpacks/simple_projects行`FOR SHARE`读取当前review_status；首次文件INSERT后，approved目标调用统一`enqueueProjectFileUpdateEventByRouteTx`，随后才把mirror改ready并commit。任何错误回滚全部关系。
- 事件/投递：update kind保持`download_added`、changed section保持`download_files`；batch为`project-file:download_added:<filePublicID>:<publicationGeneration>`。项目更新、notification task和`nats_outbox`在相同事务产生；自动提升不伪造用户actor。
- API/前端：无路由、请求、成功响应、错误码或DTO变化。外部自动镜像由后台状态机消费；已批准项目的关注者现在能观察到与手工clean上传一致的更新历史和通知。
- 隔离/回滚：无需开发库重置、回填或双写；真实测试仅在本机会话完整临时Schema执行并自动销毁，未接触远端/生产。回滚会重新造成ready镜像没有事件，且在generation146下文件停留processing，不具兼容价值。

## BUG-036：项目关注隐藏生命周期协议（generation146保持）

- Schema：无DDL、索引、约束、回填或generation变化。`project_follows`继续以`(user_id,project_route_id)`为主键并级联关联users/public_routes；修复只改变Handler对既有关系与内容表的授权使用方式。
- 创建协议：PUT不再调用允许owner/submitter/reviewer预览的内容resolver，而是单条INSERT SELECT使用无调用者参数的严格公开谓词。mod/modpack/simple/server要求approved；community要求active+approved；blueprint要求ready/partial+approved/not_required；skin要求active+public/unlisted+approved。无匹配行返回404。
- 读取协议：GET status仅对公开目标或当前用户已有关系返回。隐藏已有关系的target只含id/type及`unavailable=true`，name/url/updatedAt为空；非持有人得到404。“我的关注”同样返回安全占位并保留createdAt/notificationsEnabled关系事实，不用隐藏名称做模糊搜索。
- 删除协议：DELETE按当前用户和`public_routes.public_id`直接删除关系，不查询目标内容或review状态；隐藏、未知合法ID、无现存关系和非法格式均幂等204，避免存在性侧信道并保证清理可达。
- 前端/API：`ProjectFollowStatus.target`和`FollowedProject`新增可选`unavailable`。前端用本地化文本替代隐藏项目链接和更新时间，允许已有关系取消；取消或status 404后按钮保持不可关注。公开目标的既有字段、PUT成功响应和列表分页字段不变。
- 隔离/回滚：真实验证使用本机单连接完整临时Schema并自动销毁，无开发库重置、远端/生产访问、双读或双写。回滚会恢复pending新建和无法取消的幽灵关注，不提供兼容价值。

## BUG-037：项目更新section展示协议（generation146保持）

- Schema/持久协议：无DDL、表、列、索引、约束、trigger、回填或generation变化。`project_update_events.changed_sections`与通知`data.changedSections`继续保存稳定内部枚举，既有API消费者无需把本地化文本当机器协议解析。
- 渲染协议：Worker先按收件人请求locale选择`project_updated`模板及其实际fallback locale，再将当前section alias映射为该locale的人类label。相同语义label去重；zh-CN/zh-TW使用本地标点，其他当前默认模板语言使用英文人类label。未知code统一回退“项目资料/project details”，不会进入body或template params。
- 可观测性：事务commit后按未知code数乘实际插入收件人数累计fallback delivery；回滚和通知唯一键冲突不计数。`GET /api/v1/admin/infrastructure/metrics`新增`projectUpdateNotifications: {unknownSectionFallbacks}`，是管理员只读响应的向后兼容附加字段，同时日志记录event/code/deliveries。
- 模板/调用方：`project_updated`模板key、version、变量名、标题/正文配置和成功通知DTO不变；管理员仍能独立修改模板外壳。第一方前端不读取新增指标且无需迁移；健康公开通知只改变原本错误的变量显示文本。
- 隔离/回滚：真实验证仅在本机完整临时Schema创建中英用户、关注和事件并自动销毁，无开发库重置、远端/生产访问、双读或双写。回滚会重新把未来内部code公开为用户协议文本，不提供兼容价值。

## BUG-038：自动镜像安全扫描状态协议（generation146保持）

- Schema/数据：无DDL、列、索引、约束、trigger、回填或generation变化；继续使用`mirrored_project_files.status`已有`scanning/failed/review/source_changed/ready`枚举和`oss_files.status/scan_status`。failed现在具有唯一写入语义：关联OSS rejected、deleted或缺失，镜像当前不能保持安全可发布的ready事实。
- 状态转换：管理员把ready OSS改为pending时，同事务让项目文件回processing、镜像回scanning；rejected时让关联scanning/ready镜像写failed。Worker周期对账覆盖遗留状态和非第一方变化。只有同一OSS重新成为active+clean/trusted，且provider metadata可解码、Minecraft版本仍有权威交集，scanning/failed才可进入既有原子发布事务并变ready；元数据损坏或unknown-only转review，不会绕过安全扫描。
- 运行协议：`site_downloads`结果稳定包含`queuedForScan`、`existing`、`awaitingScan`、`scanFailures`、`needsReview`。发现failed/rejected镜像时返回内部错误码`project_mirror_scan_rejected`，既有run重试/dead-letter机制持久化失败而不是completed；纯pending扫描只报告awaitingScan而不误判安全拒绝。ready镜像仍作为existing幂等成功。
- API/调用方：管理员OSS scan PATCH的请求、响应和允许状态不变；无公开HTTP或前端DTO变化。新增结果键和错误码只扩展管理员自动更新运行详情，旧消费者可忽略；第一方前端无需迁移。
- 隔离/回滚：真实验证只操作本机单连接完整临时Schema并自动销毁，无开发库重置、远端/生产访问、回填或双写。回滚会重新使rejected镜像永久scanning且让任务伪成功，不提供兼容价值。

## BUG-039：自动维护权威修订发布协议（generation146保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化。继续使用项目`published_revision_id`、`content_revisions`/`change_requests`/`review_events`、不可变`audit_events`、项目更新/通知task和NATS Outbox；维护状态不再只存在于主表与可变activity投影。
- 写入协议：事务锁定目标项目并读取当前official status、created time和published revision。实际转换必须有匹配entity/aggregate/public key的已发布snapshot；新revision以该ID为base、autobot为actor、`auto_update`为source，只改变`officialStatus`，然后依次应用权威snapshot、写approved resolution和published review event。activity仅在整条发布链成功后于同一事务更新。
- 项目类型：Mod复用`createModRequest`与`applyModSnapshot`；plugin/datapack/map/resourcepack/shader等canonical simple project复用`simpleProjectSnapshot`与`applySimpleProjectSnapshotTx`。两条路径都保留父snapshot其他字段和关联集合，且由现有验证与审核冲突边界防止无基线或待审覆盖。
- 事件/审计：每次实际lowFrequency、discontinued或恢复发布都形成submitted/approved/published review事实、两条不可变audit记录以及`officialStatus`项目更新事件；已批准公开route继续原子创建延迟关注通知task和Outbox。人工override没有实际转换时不创建新revision或事件。
- API/调用方：无HTTP路由、请求、成功响应、错误码或前端DTO变化；Worker内部维护结果保持既有字段。行为扩展是项目历史和关注者现在能观察到自动状态发布，后台审计能关联autobot、base revision和维护metadata。
- 兼容/隔离/回滚：generation146开发基线要求受自动维护项目已有权威published revision；异常缺失行失败关闭，不以主表猜测或历史回填。真实验证仅在本机完整临时Schema运行并自动销毁，无开发库重置、远端/生产访问或双写。回滚会恢复不可审计、无通知且无法原子回滚的直接UPDATE，不提供兼容价值。

## BUG-040：填充爬虫本地化载荷协议（generation146保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、回填或generation变化。`seed_crawler_translation_tasks`继续保存locale/status/attempt/token/error；项目继续使用既有content revision和Mod `content_localizations`或simple project localization投影。
- AI输入/结果：输入从raw importer的default localization读取并标准化为`name/summary/bodyMarkdown`，sourceLocale来自数据。结果必须精确覆盖本次非空键、无重复且为非空文本；无效结果写failed和实际token，不能写completed。单个provider失败保持既有best-effort语义，不阻止源语言及其他有效locale。
- 项目载荷：Mod翻译映射到`catalogLocalizationEdit{Name,Summary,ContentMarkdown}`，simple project映射到`simpleProjectLocalization{Name,Summary,BodyMarkdown}`；只追加不存在的支持locale。合并后分别调用完整Mod/simple draft validator，再序列化为权威typed JSON。
- 草稿/提交：user draft payload移除无人消费的`seedTranslations`，标准`localizations`直接包含源语言与有效AI语言；`importOrigin/externalProjectId`仍只附加在草稿副本。auto-submit把无内部扩展字段的同一localized typed JSON交给严格项目Handler，因此revision和发布投影不会退回原始importer JSON。
- API/调用方：无公开路由、请求、响应、错误码或前端DTO变化；内部seed crawler草稿从私有旁路字段迁移到编辑器既有localizations协议，第一方前端无需修改。无AI绑定/预算时仍产生合法单源语言草稿。
- 兼容/隔离/回滚：无历史死字段回填；未提交旧草稿可由下一次同一active key爬取覆盖，已提交历史不猜测AI内容。测试只使用本机可控HTTP server和完整临时Schema并自动销毁，未访问远端/生产。BUG-042的多事务状态/来源绑定一致性独立开放；回滚会恢复付费翻译不可达，不提供兼容价值。

## BUG-041：填充爬虫候选任务血缘协议（generation146→147）

- Schema：`seed_crawler_candidates`移除语义含混、可置空的`run_id`，新增`first_seen_run_id bigint not null references seed_crawler_runs(id)`和`last_seen_run_id bigint not null references seed_crawler_runs(id)`。默认外键删除行为为RESTRICT；`idx_seed_crawler_candidates_first_seen_run`与`idx_seed_crawler_candidates_last_seen_run`分别覆盖反向FK检查。
- 写入协议：生产upsert首次创建时把同一run ID写入first/last；`ON CONFLICT(external_project_id)`把`last_seen_run_id=excluded.last_seen_run_id`与downloads、payload、updated_at放在同一statement中更新，不修改first。于是last指向当前内容的实际观察任务，first稳定表达候选首次进入系统的任务。
- 读取/API：后台候选摘要和单条详情各按两个FK连接`seed_crawler_runs`，新增必填字符串`firstSeenRunId`、`lastSeenRunId`，值为run的公开九位ID。内部bigint不出API；既有字段、status筛选、downloads/ID游标、limit+1、draft关联、详情payload及错误状态不变。Next16候选类型同步声明两字段，现有动态列组件无需专用渲染器。
- 约束/规模：没有来源任务的候选无法插入，候选存续时来源任务无法删除。两个FK各有前导索引并通过完整临时Schema审计。100k run/1M candidate下普通与status深页仍使用`idx_seed_crawler_candidates_downloads`/`idx_seed_crawler_candidates_status_downloads`先取51项，两个run连接均为主键Index Scan，执行约0.737/1.124ms。
- 迁移/兼容：当前预生产开发基线不为旧`run_id`猜测“首次”或“最后”语义，也不保留nullable/双读兼容层。专用本机55432 development数据库经有效名称与`RESET postgres`确认后重建/seed为generation147；PostgreSQL服务保持运行，未访问远端/生产，无历史回填或双写。回滚会恢复当前payload与来源任务不一致，不提供兼容价值。
- 测试边界：BUG-041真实PG用生产helper覆盖首次/再次观察、当前payload、公开API与FK删除拒绝；BUG-040夹具迁移到必填来源并全绿；分页规模、全FK索引、完整临时Schema及全仓质量门通过。BUG-042的项目创建、草稿状态、候选状态和来源绑定跨事务分裂仍独立OPEN。

## BUG-042：填充爬虫提交事务协议（generation147→148）

- Schema/状态：`seed_crawler_candidates`既有status CHECK新增`submitting`，不新增表、列、索引或trigger。`draft/submitting/failed`均是可恢复非终态；`submitted/existing`仍是终态。generation148只描述新状态和事务协议；专用本机development库按既有保护流程重建，无历史回填或双写。
- 草稿阶段：同一数据库事务以active `(user_id,draft_key)` upsert `user_drafts`，并把候选推进manual的`draft`或auto-submit的`submitting`；candidate缺失或任一SQL/commit失败时draft不泄漏。重复运行复用同一未提交draft public ID并替换权威localized payload。
- 项目阶段：Mod和simple-project创建Handler在各自权威项目事务commit前调用不可导出的context hook；没有hook的普通HTTP请求行为不变。seed hook收到已验证的project、route、review和change request事实后，在同一`pgx.Tx` upsert Modrinth external source、完成精确user draft，并以submitting CAS完成candidate。
- 故障/恢复：任一source/draft/candidate终结错误使Handler返回500且其defer回滚整个project事务；active draft及candidate保留可重试。commit后响应路径异常通过candidate+draft数据库哨兵确认，不把已经submitted的候选误写failed；外层failure UPDATE显式排除submitted/existing。
- API/调用方：管理候选分页接受`status=submitting`，响应结构、游标和详情不变；项目创建公开请求、成功响应与状态码不变。`projectCreationTransactionHook`是包内context值，不是HTTP header/body能力，外部调用方不能请求附加事务写入。
- 隔离/验证：完整临时PG分别在Mod external source、plugin candidate完成和manual draft阶段注入故障，证明零部分事实及同草稿恢复；BUG040/041、项目创建授权、全Schema/FK和1M候选查询计划共同通过。TEST-020广域矩阵、DEAD-005并发claim和跨数据库外部副作用仍保持独立Finding。

## BUG-043：通知翻译locale权威协议（generation148保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、重置、回填或双写。`notification_translations`继续以(notification_id,user_id,locale)为主键；应用成功路径现在只用八项启用站内locale的canonical code。
- 请求规范：`targetLocale`先经`normalizeContentLocale`处理大小写、下划线和现有alias，再必须命中`supportedEditableContentLocales`。规范结果用于缓存读取、AI task payload和日志；unsupported locale在通知正文、缓存、额度或模型读取前返回400。
- 完成规范：`decodeNotificationTranslation`在严格title/body items之前复用相同locale helper；内部/遗留任务的unsupported target不能写缓存，completed结果读取也不会把它当成功。现有业务结果先持久化再completed、owner/task type和损坏JSON错误协议不变。
- API/调用方：八个canonical UI locale成功协议不变；`EN_us`等可规范别名复用`en-US`身份；pt-BR等未启用代码从可能cached 200或queued 202收紧为400。响应DTO、任务轮询路由和前端类型无需迁移。
- 兼容/边界：不删除或猜测历史注册表外缓存，但新请求不会读取它们。开放BCP-47内容冷翻译与通知展示语言是不同产品边界，本项不收紧内容导出/存储locale；BUG-044的结果解析/缓存写失败由下节以既有ARCH-026协议独立验收关闭。

## BUG-044：通知翻译业务结果先于完成态协议（ARCH-026已实现，generation148保持）

- Schema/数据：本项无新增DDL、表、列、索引、约束、trigger、generation、重置或回填。ARCH-026在generation142建立的应用协议继续复用`ai_tasks`状态与`notification_translations`唯一upsert；当前基线为148。
- Worker协议：payload必须含有效notification ID与启用canonical locale，result必须是只含非重复title/body字符串的严格items；业务cache upsert返回error。只有持久化成功后才能以running CAS写result/token/cost/completed并命中一行；失败状态写入本身也必须成功并命中一行。
- 结果API：completed notification任务用相同decoder纯读取payload/result，损坏返回500并记录明确stage；GET不执行cache upsert、不刷新created_at，也不把缺失业务结果修补为成功。非completed状态、owner和健康响应字段保持既有协议。
- 补偿/边界：业务upsert幂等；业务写后CAS前崩溃保留running并由stale recovery重投。BUG-043另行限制locale入口，BUG-094/OPS-019/TEST-043分别保留并发key、恢复政策和供应商/审核矩阵责任。
- 审计映射：BUG-044的证据与ARCH-026修复/真实PG测试完全相同，本节只补齐逐项验收，不增加平行decoder、GET补写或兼容开关。

## BUG-045：私聊历史与实时增量锚点协议（generation148保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、重置、回填或双写。历史与incremental继续复用generation117的`direct_messages(conversation_id,id desc)`和public_id唯一索引、未读trigger及持久会话摘要。
- 历史方向：无after请求返回最新页，opaque cursor按(member,conversation,limit)绑定并以`id<cursor`访问更早历史；此部分由PERF-030实现且不变。响应保持`items,limit,hasMore,nextCursor`、按ID正序和2 MiB预算。
- 增量方向：`after`只接受合法message public ID且不能与history cursor组合。非空结果仍由同一SQL的conversation绑定子查询确认；空结果额外以(public_id,conversation_id)确认anchor。合法latest返回200空页，畸形/不存在/其他会话统一400且不区分外部存在性。
- 副作用顺序：只有页面查询及anchor验证成功后才批量更新当前成员的未读消息、调整缓存并发布unread.changed；分页解析、anchor或数据库读取错误零read_at副作用。合法历史/增量打开会话的既有“全部标已读”语义不变。
- 调用方/性能：第一方消息中心无需DTO或代码迁移，仍以最后一条public ID循环排空after页。仅空增量页增加一次唯一public-ID probe；1M行计划0.015ms且无Seq Scan。非法after从空200收紧400是有意错误协议修复。

## BUG-046：实时NATS实例来源去重协议（generation148保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、数据库重置、回填或双写；保持generation148。本项只改变进程内Hub与NATS之间的内部事件envelope。
- 内部广播：`realtimeEvent`新增可选`origin`。`publishRealtimeUser`以Server稳定随机origin构造事件，先向本机Hub投递，再广播；`user.*`订阅只跳过与本Server相同的非空origin，因此本机回声消失而其他实例仍各投递一次。
- 公开协议：SSE继续只写`id`、`event`和由`Data`编码的`data`，不序列化或暴露origin；HTTP路由、状态码、事件type、data DTO、前端查询失效及有限事件ID去重全部无需迁移。
- 滚动兼容：字段为`omitempty`，旧订阅者忽略新字段；新订阅者接受旧实例及通知Worker发出的origin-less envelope。不能对空origin做去重，因为后台Worker没有先执行本机Hub投递，抑制会丢失合法通知。
- 运维边界：不启用共享NATS connection的NoEcho，避免抑制同连接任务唤醒；不改成NATS-only，保留broker故障时本机即时投递。回滚只会恢复重复投递，无数据回滚。OPS-007、SEC-017/018和TEST-023仍独立开放。

## BUG-047：私聊前端conversation请求隔离（generation148保持）

- Schema/API：无数据库、后端、HTTP、SSE、消息DTO、cursor/after、状态码或generation变化；保持148，无重置、回填、双写或调用方版本协商。
- 选择状态：conversation点击和URL目标创建统一调用`selectConversation`。同一动作中止旧读取、推进request version、更新同步ref，清空messages/history/last ID/draft后再设置selected ID；新effect不再用timer延迟安全清空。
- 读取协议：初始/after增量读取共享当前conversation controller，旧历史读取使用独立controller；请求均通过标准`RequestInit.signal`。响应提交必须同时满足signal、conversation ref和request version，主动取消静默退出。
- 发送边界：POST仍使用点击提交时捕获的conversation ID且不因随后切换取消服务器mutation；只有该conversation仍选中时才把成功响应追加到当前视图并推进last ID。新会话不会显示旧发送响应。
- 兼容/回滚：前后端线上协议完全兼容，无数据操作。回滚会恢复切换瞬间的跨会话正文混合及无效网络工作，不需要数据库回滚；PERF-030/031与TEST-023不随本项重复关闭。

## BUG-048：通知AI余额前置加载与终态刷新（generation148保持）

- Schema/API：无数据库、后端、额度算法、任务状态、HTTP路由、请求/响应DTO、状态码或generation变化；保持148，无重置、回填、双写或版本协商。
- 首次读取：当前通知页存在`translationAllowed`且没有当前token的balance snapshot时，前端按需调用既有`GET /api/v1/notifications/ai-balance`；effect cleanup通过AbortSignal取消。没有可翻译项时零请求。
- 账户隔离：前端snapshot同时保存请求token与余额，只有token仍等于当前认证值才渲染；认证变化时旧账户余额不显示，新账户按条件重新读取。
- 任务对账：翻译POST的`cached=true`没有新任务，复用现有余额；`cached=false`标记任务事实可能变化，poll观察完成或失败后在finally刷新余额。翻译失败优先于余额刷新失败展示，成功翻译的余额失败独立可见。
- 兼容/回滚：所有调用均为既有API和标准RequestInit.signal，部署顺序无约束。回滚只会恢复首次额度不可见及失败任务后余额陈旧，不需要数据回滚；BUG-043/044和TEST-043不随本项关闭。

## BUG-049：Next metadata标题模板与品牌刷新（generation148保持）

- Schema/API：无数据库、后端、站点配置DTO、HTTP路由、状态码或generation变化；保持148，无重置、回填、双写或版本协商。
- 服务端metadata：根layout从既有公开site config读取siteName，返回`title.default=siteName`及`title.template="%s | siteName"`。读取no-store、2秒超时并对非2xx/网络/shape故障回退默认品牌；品牌中的`%s`不能成为模板占位符。
- 页面兼容：已有page/layout title自动经根模板组合且不需改payload；没有子页title时仍显示品牌。description维持品牌值，metadata渲染/转义继续由Next负责。
- 客户端边界：SiteBrandProvider不再读取pathname/i18n决定标题，也不访问document.title、MutationObserver或document.head。品牌变更事件更新既有Context并调用`router.refresh()`，由服务端metadata重新取值。
- 回滚：无数据操作；回滚会重新让客户端覆盖页面级title。部署不要求前后端锁步，API不可用时使用默认品牌；TEST-024、SEC-019和OPS-008保持独立。

## BUG-050：邮件运行时Enabled权威协议（generation148保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、数据库重置、回填或双写；保持148。既有加密`system_settings`键`mail.smtp`的JSON payload已含`enabled`，现在读取时原样保留，不再由Host/From推导。
- 配置协议：`config.SMTPConfig`新增Enabled；`SMTP_ENABLED`显式值优先，未设置时以已有SMTP_HOST维持旧环境部署行为。启用配置必须同时有非空Host/From和1..65535端口；关闭配置可保留全部字段，以支持无损再次启用。
- 运行时协议：后台PUT成功后用统一payload转换立即替换Server Mailer；HTTP activeMailer和NotificationWorker的环境fallback及数据库覆盖均传播Enabled。`Mailer.Send`在解析地址或SMTP拨号前调用统一Enabled门禁，覆盖验证码、测试邮件和异步通知入口。
- API/兼容：路由、请求/响应字段和成功状态码不变。行为修复是GET准确返回已保存false，PUT false后所有运行时发送返回未启用/不可用而不是继续出站；旧存储payload已有字段，无版本协商。旧环境仅配置SMTP_HOST且未声明新变量时继续启用，部署方可显式false关闭。
- 回滚/边界：无数据回滚；回滚会恢复显式关闭无效，不能作为有价值兼容层。真实PG仅创建临时`system_settings`并自动清理，不重置开发库；SEC-019配置权限和更广邮件投递/供应商矩阵保持独立。

## BUG-051：访问日志ResponseWriter能力透传（generation148保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、数据库重置、回填或双写；保持148。`app_logs`payload中的status、latency、query和response bytes结构不变，既有有界异步ingestor及批量写入不变。
- Writer协议：`responseRecorder`在Write/WriteHeader之外透传Flusher、Hijacker、Pusher和ReaderFrom，并暴露Unwrap供ResponseController继续下钻。ReaderFrom统计底层实际返回字节；Flush建立默认200状态；底层Hijack/Push错误原样返回。
- SSE/API：既有`GET /api/v1/realtime/events`请求、鉴权、200响应头、SSE `id/event/data`帧、心跳、30分钟生命期及连接预算完全不变；行为修复是完整生产中间件链不再因日志wrapper固定返回501。前端EventSource无需迁移。
- 兼容/回滚：普通JSON/二进制Handler仍走原Write计数；压缩writer已有Unwrap/Flush/ReaderFrom，可与新recorder组合。无调用方版本协商或部署顺序要求；回滚只会恢复所有SSE不可用，无兼容价值。
- 验证边界：完整链测试使用本地ResponseWriter和本地querycache lease，不访问外部服务；PG测试只锁定授权本机开发库的`app_logs`后自动释放，证明请求不等待落库。TEST-025的敏感参数、结构化日志、游标和周期清理仍独立开放。

## BUG-052：专用日志保留自动调度协议（generation148保持）

- Schema/索引：无DDL、表、列、索引、约束、trigger、generation、重置、回填或双写；保持148。复用PERF-033建立的created_at/ID页索引、搜索投影及每语句1000行稳定ID删除。
- 调度协议：应用runtime启动LogRetentionWorker；启动立即运行，之后固定每10分钟运行。每轮从`system_settings['logs.retention']`读取当前策略，无行采用默认Enabled=true；持久false不执行删除，损坏JSON或数据库读取失败终止本轮。
- 并发/预算：独占数据库session通过`pg_try_advisory_lock`保证集群单执行者；每轮按全部app日志类别及permission/login/upload/scan表公平执行，最多32轮、30秒。单条DELETE仍独立事务且最多1000行，满批才继续下一轮；解锁失败关闭物理连接防止租约泄漏。
- API/调用方：GET/PUT路径、请求/响应字段、状态码和前端类型不变；保存响应仍含既有`config/deleted`，新增行为完全在后台。自动删除遵循同一normalized类别期限，不要求客户端、Worker或部署版本协商。
- 回滚/隔离：无数据迁移可回滚；回滚会再次使自动开关失实。真实PG测试只创建session临时五表并自动销毁，第二会话只持/释放专用advisory lock；开发库未重置。BUG-053、SEC-020、LEGACY-010、PERF-033和TEST-025责任不合并。

## BUG-053：日志读取与清理错误响应协议（generation148保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、重置、回填或双写；保持148。既有每表独立1000行清理事务、日志页索引/search projection和system setting持久顺序不变。
- 读取协议：管理员日志继续使用PERF-033的cursor envelope；Query、Values和terminal rows错误均不返回部分items或空200，而由Handler返回500。共享严格collector来自ARCH-013，本项只复验并映射BUG证据。
- 清理协议：`cleanupLogs`对全部类别/表继续执行，返回成功RowsAffected、稳定失败类别和joined内部错误。Handler记录完整错误；任一失败时返回500/code `LOG_CLEANUP_FAILED`，details只含已保存config、deleted map及failedCategories，不泄露SQL/约束文本。
- 成功/兼容：无失败时PUT仍返回200及原`config/deleted`结构；Enabled=false仍为空deleted成功。失败状态从伪200收紧500是有意错误合同修复，第一方前端既有异常分支无需类型迁移。后台BUG-052 Worker已有严格错误路径并按周期重试已保存策略。
- 隔离/回滚：真实PG只使用session临时表和临时trigger，未修改公共Schema或重置数据库。回滚会恢复清理失败不可见，不提供兼容价值；SEC-020、PERF-033和TEST-025不随本项关闭。

## BUG-054：结构化运行日志汇聚协议（generation148保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、重置、回填或双写；保持148。运行日志继续只在进程内5000项ring保存，不变成业务持久事实。
- 生产协议：应用启动最早期Install显式设置默认slog TextHandler；同一MultiWriter把每条text record发到stderr和Store。结构化Record的time/level/message/attrs/groups统一序列化，Store从level/time字段读取权威元数据。
- legacy兼容：标准`log` writer桥接到同一slog Logger，清除旧前缀避免双时间戳，并沿用error/failed与warn/degraded/retry关键词推断级别。既有`log.Print/Printf/Fatal`调用无需迁移，结构化Info中的同名属性不会触发文本误级。
- API/调用方：`GET /api/v1/admin/runtime-logs`请求、Entry DTO、权限、过滤、cursor/reset和脱敏不变；仅Line展示成为slog text格式。前端按字符串渲染，无部署锁步。stderr和后台视图现在来源一致。
- 回滚：无数据回滚；回滚会恢复对Go默认桥接的隐式依赖和结构化级别误判。TEST-025仍负责更广完整HTTP/敏感字段/生产故障矩阵。

## BUG-055：PostgreSQL UTC日桶连接协议（generation148保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、重置、回填或双写；保持148。既有date列、主键、默认值、`current_date`函数和显式UTC活动投影不变，现有数据库在建立新连接后立即按统一UTC session解释。
- 连接协议：`database.connectPool`在解析完整DATABASE_URL后强制`timezone=UTC`，所以URL显式时区也不能覆盖。主应用池和独立activity池共享该工厂；db-reset及user-statistics CLI也迁入`database.Connect`并继承相同UTC、连接预算和超时。
- 日桶语义：浏览量、站点当前计数、热度事件/衰减、每日快照及初始化中的`current_date`都以UTC为界，与`record_site_activity_batch`和用户统计的显式UTC日期一致。用户展示时区继续只用于progression等用户语义，不改变站点存储桶。
- API/兼容：HTTP路径、字段、状态码、统计响应和前端类型均不变；修复仅改变非UTC部署在UTC午夜附近的日期归属。无需前后端锁步或数据格式协商；历史错桶若存在属于运营回算而非本项自动破坏性回填。
- 验证/回滚：真实PG用Pacific/Kiritimati冲突URL验证两个池都被覆盖为UTC，并用固定23:30Z瞬间锁定date。回滚会重新允许部署/URL时区分裂，无兼容价值；TEST-027/028仍负责更广跨时区和日期边界矩阵。

## BUG-057 / MAP-007：有效developer集合热度协议（generation149）

- Schema generation：148→149。删除返回单一bigint的`content_target_owner_id`，新增`content_target_user_is_developer(route_id,user_id) boolean`，从public route联接`effective_project_access`并对全部`access_level='developer'`执行EXISTS。无新表、列、索引或owner持久概念。
- Incremental协议：评分、评论、收藏的trend和lifetime/global/dimension delta，以及独立访客lifetime delta，都以完整developer membership排除。评分UPDATE分别使用old/new route/author；普通active且security_score足够的非developer继续计入，匿名独立访客保持计入。
- Rebuild协议：route lifetime及dimension校准在每个权威历史集合上以effective access NOT EXISTS排除；global rating rebuild建立`route_developers(route,user)`集合并做anti-join，不调用逐评分PL/pgSQL scalar，也不以min保留旧错误语义。10M集合计划仍在15s预算内。
- 数据/部署：现有aggregate count/sum不能逆推出错误计入的第二开发者，禁止在线猜测减算、双读或双写。开发期按generation合同整代reset/seed；本机授权测试库已重置到149。未知远端数据库不迁移、不重置；接流量前必须安装一致149 baseline。
- API/映射：公开统计、热度公式、developer item DTO与排序不变。列表候选只来自effective_project_access，按user distinct且developer优先于editor；删除scalar与原集合的重复UNION。非授权展示作者不会仅因署名被排除。
- 回滚：需恢复generation148全部函数/trigger和开发Schema，会重新引入多developer自有交互污染；不提供兼容价值。DEAD-008与TEST-028其余多作者/并发/搜索行为和规模责任保持开放。

## BUG-058：搜索重建跨实例代次协议（generation149保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、数据库重置、回填或双写；保持149。复用现有持久`search_index_queue`作为快照期间的增量delta log，`search_index_rebuild_progress`继续仅承担观察性进度。
- 租约协议：rebuild在全部collection范围持`pg_advisory_lock(hashtext('mcmods-search-projection-rebuild'))`独占session lock；queue drain持同key的shared lock，并把claim、权威文档读取、Typesense import/delete、retry或updated-at token ack全部包含在shared lease内。
- 切换顺序：独占lease获取后重查全部alias/state/schema；若已由另一实例完成则不重建。否则在同一lease内逐集合建立稳定ID快照、导入、激活alias、记录state并清理旧集合。期间新事务照常enqueue但不能被drain；全部alias激活并释放lease后，积压事件通过当前alias重放。
- 连接安全：lease内数据库工作使用持锁的同一pool connection，不要求额外连接容量。解锁使用独立超时context；解锁失败或锁状态不明确时Hijack并关闭物理连接，禁止session lock泄漏进pool。
- API/兼容：搜索HTTP请求/响应、fallback判定、Typesense alias和collection schema均不变；同版本实例可滚动部署，新实例只有在取得lease且重查state后重建。OPS-011所述跨schema版本回切仍需独立发布协议，不能由本项推定解决。
- 回滚：无数据操作；回滚会恢复其他实例在快照/alias窗口claim并永久删除增量的竞态，不提供兼容价值。TEST-030完整Typesense故障矩阵与MAP-008注册表责任保持开放。

## BUG-061：gzip Accept-Encoding协商协议（generation149保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、数据库重置、回填或双写；保持149。
- 请求协商：gzip token默认q=1；显式q只接受HTTP qvalue的0/1及最多三位小数形状，必须在0..1且1后只能为0。非法、空、越界或重复q使该候选无效，不能通过解析错误反向启用压缩。
- 优先级：显式gzip候选优先于`*`且与header顺序无关；显式q=0或无效时不会被wildcard重新启用。仅在没有显式gzip时才由有效且正权重的wildcard允许gzip；其他encoding不影响判断。
- 响应兼容：合法支持者仍收到gzip；无效或明确拒绝者改收identity。`Vary: Accept-Encoding`、HEAD/Range/WebSocket跳过、既有Content-Encoding、不可压缩类型、状态码/body/Content-Type和可选Writer接口不变，前端无需迁移。
- 回滚：无数据操作；回滚会恢复非法权重压缩和wildcard覆盖显式拒绝，无兼容价值。TEST-032更广压缩、资源版本、基础设施监控与死信测试责任保持开放。

## BUG-062：缺失Minecraft设置的兼容范围协议（generation149保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、数据库重置、回填或双写；保持149。没有修改已有`system_settings`值、同步状态或ARCH-017 artifact快照。
- 默认目录协议：明确缺行时继续返回33项版本目录、common版本及13种loader名称，但每个loader的`versions`为非nil空slice并序列化成`[]`，表示没有已验证兼容范围；不使用`null`，也不把unknown解释成全支持。
- 持久范围协议：管理员或同步器提供的loader范围继续规范化并保存。模组内容没有项目专属兼容记录时，fallback逐loader复制其自身`versions`，不再由顶层`config.versions`扩张。空范围通过既有校验失败关闭，已验证范围保持精确。
- HTTP/调用方：`GET /api/v1/minecraft/versions`路由、DTO字段和200协议不变；仅缺失设置的loader数组内容从全目录收紧为`[]`。模组内容无权威范围的非法组合继续使用既有422响应。前端workspace的全局fallback同步改为逐loader复制`versions`，不再自行恢复全目录；无版本迁移或锁步部署要求。
- 兼容/回滚：配置故障继续由ARCH-016返回500，正常持久配置不受默认收紧影响。回滚会恢复虚假全兼容，不能作为兼容层。OPS-012跨实例租约、BUG-063/064/065和TEST-034保持独立开放。

## BUG-063：NeoForge artifact版本映射协议（generation149保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、数据库重置、回填或双写；保持149。部署本身不改数据，下一次既有同步事务可为原漏掉的release增加持久artifact，或按既有omission状态去掉只有预发布artifact的组合。
- 映射协议：1.20.1继续从legacy `net.neoforged:forge`以`1.20.1-`匹配；其他旧式1.20+ Minecraft release按`minor.patch.`匹配且无patch补0；26+按`year.release.patch.`匹配且无patch补0。snapshot、April Fools、pre/rc、非数字或过旧形态不猜测映射，返回显式error。
- 稳定通道：匹配prefix之后必须只剩点分十进制分量；alpha、beta、rc及其他限定符全部排除。legacy坐标返回移除Minecraft前缀后的47.x版本；modern坐标保留完整21.x/26.x NeoForge版本，符合MRPack依赖值。
- API/调用方：HTTP路由、请求/响应DTO、状态码、错误码和客户端类型不变。同步后1.21、1.21.x及26.x合法稳定组合可出现在兼容范围并由持久artifact供preview/create离线解析；无稳定artifact继续走既有不支持/省略协议。
- 兼容/回滚：同代服务读取的是ARCH-017 catalog hash绑定快照，无锁步前端要求。回滚会在下次同步重新漏掉1.21/26.x并可能选择alpha/rc，不提供兼容价值。BUG-064/065、MAP-009与TEST-033/034保持独立。

## BUG-064：Minecraft loader code身份协议（generation149保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、数据库重置、在线回填或双写；保持149。合法旧`system_settings`在读取时规范化并于下次正常保存/同步写canonical；含大小写重复的损坏配置不自动合并，需管理员明确选择范围后修复。
- Code/Name协议：13种内置loader code使用既有规范拼写；自定义code为trim后小写。code是配置、同步状态和兼容查找身份，大小写fold后必须唯一；name仅为展示，保留输入文本，空值才回退code。
- 失败协议：后台PUT payload含`Forge`+`forge`时在数据库读取/写入前返回400 `duplicate Minecraft loader code`；持久JSON含重复时load返回typed unavailable，公共/管理API按ARCH-016返回500。禁止静默丢第二项或猜测合并两个版本集合。
- 前端/兼容：管理员新增在本地fold检查；模组compatibility和relationship选择/范围/展示也用同一trim+lower key，所以旧自定义大小写事实仍可编辑。服务端响应返回canonical code，后续写入逐步收敛；DTO字段、路由和成功状态码不变。
- 回滚：无数据回滚；回滚会重新允许模糊重复并让同步只更新第一项，不提供兼容价值。BUG-065、OPS-012和TEST-033/034保持独立。

## BUG-065：MRPack预检不可变消费协议（generation150）

- Schema generation：149→150。新增`favorite_modpack_export_previews`：9字符`public_id`、owner/collection级联FK、Minecraft/loader/loader-version列、`preview_snapshot jsonb`、64位小写SHA-256、`expires_at/consumed_at/created_at`及loader/时间约束；`(owner_user_id,expires_at,id)`和`(collection_id,id)`覆盖清理与两个FK。无历史回填、双读或双写。
- 持久载荷：JSONB使用服务端封装，同时保存公开preview和每个item隐藏的`source_project_route_id`；摘要覆盖完整封装。读取恢复内部route后重算摘要，并把collection ID、version、loader及loader version与独立列、URL路径和create请求全部核对。公开JSON仍不含内部ID。
- Preflight成功响应在既有完整preview上新增必填`previewId:string`、`previewHash:string`、`expiresAt:RFC3339`。快照TTL为15分钟；同一owner下一次preflight有界清理其已过期或消费超过1小时的旧行。preflight持久化失败返回500 `MODPACK_EXPORT_PREVIEW_UNAVAILABLE`，不返回无法复用的伪成功确认。
- Create请求在既有`minecraftVersion/loader/exportCompatibleOnly/confirmCompatibleOnly`上新增必填`previewId/previewHash`。缺失/畸形身份返回400 `MODPACK_EXPORT_PREVIEW_REQUIRED`；找不到、过期、已消费、摘要或设置不符分别返回409 `MODPACK_EXPORT_PREVIEW_NOT_FOUND/EXPIRED/CONSUMED/MISMATCH`；authority读取/JSON故障返回500。create不再调用provider/catalog/preview builder。
- 原子性：create事务以preview行`FOR UPDATE`串行消费，复用已确认loader和全部item。task、items、queue outbox及`consumed_at`同时提交；空包、required dependency、compatible-only确认、限额或任何写入失败均回滚消费，用户可在TTL内重试。成功响应中的preview保持同一ID/hash/期限。
- 前端/部署：`FavoriteModpackExportPreview`及API client同步新增三个字段；UI直接把当前preview对象交给create，body的version/loader也从该对象产生。冻结保证要求前后端与generation150同一版本协调发布；pre-production开发库按保护协议reset/seed，不提供回退到实时重建的兼容开关。回滚需整代回到149并会恢复审计问题，无业务数据转换价值。

## BUG-067：收藏导出报告名称投影（generation150保持）

- Schema/范围：无DDL、表、列、索引、约束、generation、数据重置、回填或双写；保持150。generation89确立的`mod/modpack/blueprint`收藏闭集、数据库CHECK和可见性协议不变，不把插件、地图、资源包、光影、数据包或addon重新加入收藏。
- 查询协议：收藏导出继续使用一次批量SQL与`favoriteTargetJoinsSQL`。新增显式`favoriteTargetNameSQL`，按闭集选择`mods.primary_name`、`modpack.primary_name`或`blueprint.title`；不可达类型返回NULL后空串，不增加逐项查询或目录resolver。
- API/调用方：preflight、create、task detail的路径、请求、状态码和`sourceProjectName`字段完全不变。行为修复仅使蓝图`NOT_A_MOD`行从空名称变为权威title；整合包同类报告和Mod结果不变。第一方前端已有报告分组与复制名称逻辑，无类型或部署迁移。
- 兼容/回滚：已有任务item保存的是当时名称快照，不做猜测性改写；新任务从权威表保存稳定值。回滚无数据步骤但会恢复蓝图空报告名，不提供有价值兼容层。BUG-068/069、DEAD-011及TEST-034保持独立。

## BUG-068：收藏来源与导出报告生命周期协议（generation151）

- Schema generation：150→151。`favorite_modpack_export_tasks.collection_id`由非空`ON DELETE CASCADE`改为nullable `ON DELETE SET NULL`；新增非空、9字符格式的`collection_public_id_snapshot`；新增`idx_favorite_modpack_export_tasks_collection(collection_id,id) where collection_id is not null`覆盖FK维护和来源删除。preview表仍是15分钟短期确认，collection删除继续级联preview。
- 创建/读取：create在task事务中从已确认preview同时写内部collection FK和公开ID快照。history/detail不再联接`favorite_collections`，直接把snapshot作为既有`collectionId`返回；来源删除后分页、报告、状态、下载和item查询仍可用，且百万task页少一个join。pack name及项目行继续使用既有快照。
- 删除状态机：删除非default收藏先`FOR UPDATE`锁来源，在同事务把`pending/processing`更新为`cancelled`、stage cancelled、error `SOURCE_COLLECTION_DELETED`、finished时间并清lease，然后删除来源使所有task FK置NULL。terminal task不改；任一步失败回滚并保持既有409/500合同，成功仍204。
- Worker竞态：生成文件后的最终化改为事务锁task行。当前processing lease原子ready；若task已因来源删除cancelled，则把file链接到task、调用统一OSS tombstone、写可靠删除Outbox并保持cancelled，不发完成通知。先ready的任务不会被随后删除取消；先取消的任务不会留下active孤儿。取消后的旧queue消息claim 0并幂等成功。
- API/前端：无路径、请求或字段变更；历史/详情中的`collectionId`语义明确为创建时来源快照，即便来源已不存在。前端既有cancelled状态可直接展示，返回相同设置后若来源已删会在下一次preflight得到既有404。新增内部error code不要求新枚举。
- 部署/回滚：历史行无法从已删除来源可靠补齐公开ID，pre-production按generation合同reset/seed到151，不做猜测回填、双读或双写。回滚需恢复150并重新引入级联数据丢失，无兼容价值。OPS-013继续覆盖非取消类“上传成功/ready持久化失败”，BUG-069、DEAD-011和TEST-034保持独立。

## BUG-069：导出报告与MRPack索引完成协议（generation151保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、开发库重置、历史回填、双读或双写，保持151。读取继续使用`favorite_modpack_export_items(task_id,result_type,id)`既有task前导索引；单task行数受收藏项目和1000节点依赖图硬预算限制，无新增全表扫描或N+1。
- Worker读取：任务载入从原三项展示统计扩为collection/exported/auto-dependency/skipped/failed/final六项；报告查询从只读两个可打包类型改为读取该task全部四个受CHECK约束的result type。每行仍严格解码，Query/Scan/`rows.Err()`故障继续按ARCH-018写`REPORT_LOAD_FAILED`。
- 完成不变量：task与实际报告必须分别满足collection分类和、final文件分类和，并且六项完全相等；未知类型、负数、缺行、多行或分类漂移统一为`REPORT_INCONSISTENT`。校验发生在任何OSS调用前；失败复用既有限次pending/retry_wait与terminal failed状态机。
- 索引证明：`buildMRPack`成功结果新增内部`IndexFileCount`，值来自实际序列化的`mrpackIndex.Files`；Worker在上传前再次要求它等于task `final_file_count`。公开ZIP格式、哈希、下载、环境、loader dependency和路径验证不变，BUG-070仍单独负责大小写路径冲突。
- API/调用方：HTTP路由、请求、成功响应、状态码与TypeScript DTO均不变。既有详情`errorCode:string`可能返回新内部值`REPORT_INCONSISTENT`，客户端无需枚举迁移；`REPORT_LOAD_FAILED`和`MRPACK_BUILD_FAILED`语义保持。无协调部署或数据回滚步骤，代码回滚只会重新允许较小索引进入上传/ready，不提供兼容价值。OPS-013、DEAD-011和TEST-034保持独立。

## BUG-070：MRPack portable文件路径协议（generation151保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、开发库重置、回填或双写，保持151。冲突检测仅对单个preview中最多既有依赖图/收藏硬预算的items构造O(n)内存map，不查询数据库、不访问provider、不产生N+1；无需查询计划或新索引。
- 路径身份：输出仍限`mods/`下一层、最长180字节、安全字符且小写`.jar`后缀。实际`modrinth.index.json`路径规范为NFC；portable比较键为安全路径的NFKC兼容归一化、二次安全校验和完整Unicode case-fold，覆盖大小写、规范等价和兼容字符碰撞。生成器也使用同一key而非原始区分大小写字符串。
- Preflight：依赖解析结束后、四类统计前，对`exported/auto_dependency`分组；冲突组全部写`resultType=failed`、`reasonCode=FILE_PATH_CONFLICT`及稳定detail，保留项目、依赖和文件元数据。无冲突候选不变；非输出item不参与。provider给出的不安全路径提前按既有`NO_COMPATIBLE_FILE`处理。
- Create/API：确认快照含任一`FILE_PATH_CONFLICT`时，在empty和compatible-only逻辑之前返回422 `MODPACK_EXPORT_FILE_PATH_CONFLICT`，details含原preview；事务回滚，preview不消费且无task/items/outbox。preflight成功形状、create请求字段及其他状态码不变。前端reason字典已含中英文`FILE_PATH_CONFLICT`，API错误仍由通用边界展示，无TypeScript迁移。
- 兼容/回滚：这是更严格的跨平台可移植性规则，可能拒绝在大小写敏感Linux上可共存但在常见客户端会碰撞的文件，属于有意失败关闭。无数据/锁步部署步骤；回滚会恢复晚期通用`MRPACK_BUILD_FAILED`或客户端覆盖，不提供兼容价值。BUG-071/072、OPS-013和TEST-034保持独立。

## BUG-071：导出历史显式重建来源协议（generation152）

- Schema generation：151→152。`favorite_modpack_export_previews.collection_id`从非空`ON DELETE CASCADE`改为nullable `ON DELETE SET NULL`；新增`collection_public_id_snapshot text not null`及9字符格式约束、新增`source_mode text not null default 'current_collection'`及`current_collection/original_snapshot`闭集。既有owner/expiry和collection前导索引继续覆盖清理及两个FK；没有新查询索引、trigger、回填、双读或双写。
- Preview持久合同：公开快照与SHA-256输入新增`allowCompatibleOnly:boolean`、`reportVersion:number`、`rebuildSource`；镜像列同时保存公开collection ID和source mode。读取create时`FOR UPDATE`以`coalesce(collection_id,0)`恢复detached状态，并把数据库镜像、JSON、hash、URL路径及请求逐项核对。普通/current快照持久化时必须有live内部collection ID；original允许NULL。收藏删除把已有短期preview detach而不阻止删除，detached current随后不能消费。
- 历史读取：GET列表和详情task DTO新增必填`allowCompatibleOnly`与`reportVersion`，直接读取task不可变列；原`collectionId`继续是创建时公开ID快照。`report_snapshot`没有被本项伪装成权威来源，DEAD-011仍开放；原始重建使用已经作为task创建事务一部分保存的逐项items。
- 新API：POST `/api/v1/users/me/modpack-exports/{taskId}/rebuild-preflight`，权限`favorite.modpack_export`且查询强制owner。请求严格为`{"source":"current_collection"}`或`{"source":"original_snapshot"}`。成功200返回现有preview envelope加上述三个字段；live来源缺失返回409 `MODPACK_EXPORT_REBUILD_SOURCE_UNAVAILABLE`，原报告版本/统计/JSON/文件身份/路径损坏返回409 `MODPACK_EXPORT_REBUILD_REPORT_INVALID`，非法source为422。
- Create协调：路径仍为POST `/api/v1/users/me/favorite-collections/{id}/modpack-exports`。original preview要求`exportCompatibleOnly`及`confirmCompatibleOnly`均精确保持原task确认，否则409 `MODPACK_EXPORT_REBUILD_CONFIRMATION_MISMATCH`；task插入允许NULL内部collection但始终写公开snapshot和report version。重复冷却改按公开collection snapshot比较，使detached与live重建采用相同来源身份；其余限额、逐项snapshot、preview消费和queue事务不变。
- 前端迁移：`FavoriteModpackExportTask/Preview`同步新增字段和source union，API client增加rebuild函数。结果页删除含糊的“相同设置”动作，分别显示双语current/original按钮；成功rebuild直接呈现服务器preview，create使用`preview.allowCompatibleOnly || hasOmissions`，不会丢失原确认。前后端与generation152同批部署；回滚必须整代reset回151且会恢复删除preview/无法历史重建的问题，不提供兼容层。

## BUG-072：MRPack导出Minecraft版本权威边界（generation152保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、开发库reset、回填、双读或双写，保持152。preview/task既有`minecraft_version`列和JSON字段不变；新写入只接受当前站内目录中为所选MRPack loader启用、并有同catalog hash同步artifact的code。
- Code语法：`validMinecraftVersionCode`要求原值已trim、1–80字节、ASCII字母/数字开头，剩余字符属于字母数字、`.`、`_`、`-`、空格、括号、单引号、加号闭集。公共目录normalize和导出请求decode共用此规则；非法路径字符、控制符、Unicode或超长code在任何数据库/provider解析前拒绝。
- 权威性：读取`minecraft.versions`设置并完成既有normalize后，先要求code精确存在于`Versions`，再要求出现在所选Fabric/Forge/NeoForge的`Versions`集合，随后才按catalog hash读取`minecraft_loader_artifact_versions`。未知code和loader未启用code返回新sentinel；已启用但同步artifact缺失继续既有artifact unavailable，设置读取/JSON故障继续authority unavailable。
- API：POST preflight/create的请求字段和成功DTO不变。非法语法、未注册或未为loader启用统一422 `MODPACK_EXPORT_INVALID_MINECRAFT_VERSION`；原非空/`.X`专用`MODPACK_EXPORT_EXACT_VERSION_REQUIRED`不再用于该边界。合法启用版本但artifact缺失仍422 `MODPACK_EXPORT_LOADER_SNAPSHOT_UNAVAILABLE`，目录authority故障仍500 `MODPACK_EXPORT_CATALOG_UNAVAILABLE`。
- 调用方/兼容：第一方Next选择器本来消费公共站内目录，无TypeScript或UI迁移。服务端收紧会拒绝旧客户端手造版本，这是有意失败关闭；无锁步数据部署或回滚步骤。回滚只会重新把未知字符串误分类并允许其进入查询/快照，无兼容价值。BUG-073、ARCH-017和TEST-034保持独立。

## BUG-076：蓝图实体保真失败协议（generation152保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset、回填、双读或双写，保持152。含实体的源文件不会再生成缺实体的normalized artifact，含实体的normalized JSON也不会生成不完整variant；旧原始对象和已存在variant不做破坏性改写。
- 解码协议：Sponge根/Blocks的`BlockEntities/TileEntities/Entities`、每个Litematic region的同类字段及旧Schematic根字段只要非空或容器类型畸形，就返回包装`errBlueprintEntityDataWouldBeLost`的明确错误。空列表及完全无实体文件保持兼容；Vanilla与normalized JSON继续保留当前能够读取的实体payload。
- 编码协议：`encodeBlueprint`对`nbt/schem/litematic`统一检查规范化文档的`BlockEntities/Entities`，非空即拒绝；`json`继续是保真载体。尚未建立坐标、NBT版本和格式语义映射前，不声称任何非JSON目标可无损承载这些字段。
- 任务/API：公开convert/任务DTO和路由不变。该sentinel被任务状态机视为永久错误，第一次运行就保存failed与原因并发送既有失败通知；normalize主体失败，convert主体保持ready且不插入variant。调用方无需锁步迁移；回滚只会恢复静默有损成功，不提供兼容价值。

## BUG-077：蓝图现有封面关系授权（generation152保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset、回填、双读或双写，保持152。复用`blueprints.cover_file_id`到`oss_files.id`的既有FK关系和OSS public ID唯一键；无历史数据改写。
- 应用不变量：apply修订时先要求snapshot的cover public ID就是目标蓝图当前绑定的active、受信扫描、光栅文件，并从数据库重取object key、锁定文件；当前关系不匹配时才走actor上传者约束的新绑定解析。快照中的object key永远不作为写入权威。
- API/调用方：PUT蓝图metadata请求、revision snapshot JSON、审核路由及成功/失败DTO不变。管理员更新他人带封面蓝图的标题/正文不再因UploaderID误判失败；该路径不能选择新封面，真正的新绑定仍要求上传者所有权。无需前端或部署顺序迁移；回滚会恢复授权管理操作的确定性失败，无兼容价值。

## BUG-079：蓝图上传会话过期与原子清理（generation153）

- Schema generation：152→153。`blueprints`新增nullable `upload_expires_at timestamptz`，并新增`idx_blueprints_upload_expiry(upload_expires_at,id) where status='uploading' and upload_expires_at is not null`。没有新表、trigger或级联关系；开发库按整代合同reset/seed，不从历史`updated_at`猜测会话期限、不回填、不双读双写。
- 写入协议：蓝图上传预签名在创建主体/首个本地化的同一事务写入与provider/multipart一致的期限（默认10分钟、最大60分钟）；成功complete把主体转queued并清NULL。对象Key保存、quota、multipart初始化/登记、普通presign失败及显式abort通过统一owner-scoped事务即时删除仍为uploading的主体和`content_localizations/content_subjects`投影。
- 回收协议：Maintenance按部分索引的期限/ID顺序，以`FOR UPDATE SKIP LOCKED LIMIT 1000`领取到期行，并复用相同事务删除helper。行数必须精确；fresh、非uploading或NULL期限不命中，多实例不会重复领取。回收只处理尚未完成的业务元数据，不直接删除OSS对象；预签名通常尚无对象，已经登记的文件仍由既有OSS生命周期独立管理。
- API/兼容：预签名、multipart、complete和abort的请求/响应DTO、路径、成功状态与期限含义不变；已知失败继续返回原错误分类，只增加服务端补偿。客户端逾期后本来也不能使用签名，后台删除对应uploading蓝图是该合同的持久化收敛。回滚需整代回到152且会重新产生永久残留，无有价值兼容层；BUG-080与OPS-018仍独立。

## BUG-080：举报证据原子登记与严格孤儿恢复（generation153保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持153。继续使用`oss_files.object_key`和`report_evidence.object_key`各自唯一约束；新写入通过同一pgx事务建立1 file/1 evidence事实，历史1/0行按请求时验证恢复，不做脱离provider/owner事实的批量猜测回填。
- 原子协议：report-evidence完成在所有对象Head/Get、安全/大小/hash验证后开启事务；`oss_files` INSERT与`report_evidence` INSERT均由该事务执行，证据失败或commit失败不会只提交文件。并发相同完成由文件object-key唯一约束串行：胜者提交1/1，后继进入严格幂等恢复。
- 恢复协议：早期已有证据查询绑定uploader、object key、SHA-256、可选byte size及temporary状态。文件冲突恢复先要求active文件的uploader/category/source/hash/key/size匹配，再`FOR UPDATE`重锁；缺证据时以文件权威字段补建，已有证据则逐项核对original name/content type/size/hash/scan/temporary。任何scope、owner或内容身份漂移返回错误，不能返回通用file ID伪成功。
- API/调用方：举报证据预签名和完成请求/响应字段、路径及权限不变。新登记成功201，严格幂等及legacy恢复200并返回同一evidence ID；瞬时数据库故障500且零部分数据库行，可原请求重试。无需前端、协调部署或数据回滚；回滚会恢复不可重试1/0分裂，无兼容价值。ARCH-020的通用OSS失败可观测性和OPS-018 multipart生命周期保持独立。

## BUG-082：评论日志附件持久处理任务（generation154）

- Schema generation：153→154。新增`comment_log_attachment_jobs`：numeric ID、(comment_id,attachment_file_id)唯一及复合`ON DELETE CASCADE` FK、requested_by用户级联FK、queued/processing/completed/failed闭集、attempt/max=5、next attempt、lease owner/expiry、last error和全套时间字段。新增`(next_attempt_at,id) where queued`、`(lease_expires_at,id) where processing`及`(requested_by,id)`，覆盖扫描、租约恢复和FK维护。
- 创建协议：评论事务完成普通附件owner/active/scan闭集绑定后，读取最多5个已绑定文件并按统一名称规则识别日志；每个日志原子改为`kind=log/processing`、插唯一job并以task code `comment_log_attachment`写`comment.log_attachment.requested` Outbox。任何一步失败回滚整个评论；commit后不再同步访问OSS或写绑定。
- Worker协议：默认NATS task由runtime注册，同时启动数据库扫描兜底。job CAS取得10分钟lease并增加attempt；provider/解析/数据库失败在预算内queued指数退避，耗尽failed且attachment failed。启动及30秒周期扫描到期queued和过期processing；完成事务以ready share+owner+source file重验，原子upsert binding、attachment ready和job completed。重复消息/完成job no-op，先生成share后绑定失败会复用既有share。
- API/部署：评论创建请求、响应、状态码和附件DTO形状不变；日志附件的既有processing/ready/failed状态现在来自可靠任务。第一方前端无需迁移。历史任务事实不可重建，开发库整代reset/seed到154，不做猜测回填、双写或同步fallback；回滚需整代回153且会恢复post-commit丢任务。BUG-083/084、SEC-031/032/033和TEST-038保持独立。

## BUG-083：已删除评论附件解绑与读取防线（generation154保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、开发库reset、历史回填、双读或双写，保持154。继续使用`comment_log_bindings(comment_id,attachment_file_id)`和`comment_attachments(comment_id,attachment_file_id)`主键；日志job已有同键`ON DELETE CASCADE`。解绑EXPLAIN均由comment_id前导的主键Bitmap Index Scan驱动，无新索引或全表扫描。
- 删除协议：评论DELETE把既有主体soft-delete UPDATE、log binding DELETE、attachment DELETE和commit放在同一pgx事务。接受答案边界与权限判断不变；解绑/级联/commit任一步失败返回500且主体正文、published状态和全部关系回滚，原请求可重试。成功删除binding及attachment，job随FK级联；不删除独立`log_shares`/entries或OSS文件主体。
- 读取协议：附件annotator无条件把响应切片初始化为`[]`，但只收集非deleted comment ID进行批量查询；deleted项即使仍有历史脏关系也不会进入map或附加文件名、大小、状态、下载URL和日志短链。混合列表仍对非deleted项执行一次批量查询，不引入逐项调用。
- API/前端/回滚：DELETE路径、权限、成功200 envelope及失败分类不变；评论响应字段仍为必填`attachments`，deleted值从可能泄露内容收紧为恒定空数组，第一方TypeScript无需迁移。无需协调部署；代码回滚会恢复deleted元数据泄露和半状态，不提供兼容价值。BUG-082的处理可靠性、BUG-084及日志分享自身生命周期保持独立。

## BUG-084：评论PATCH版本比较协议（generation154保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、开发库reset、回填或双写，保持154。版本令牌直接使用响应既有`comments.updated_at`；新UPDATE以`greatest(clock_timestamp(),updated_at+1 microsecond)`保证同一主机时钟粒度或异常未来时间下也不复用旧令牌。无需新增revision列或历史迁移。
- 写入不变量：PATCH的`baseUpdatedAt`必须是有效非零时间。单条CTE按PK、published和精确updated_at选择并锁行，再UPDATE正文且返回旧body/新版本；谓词失败不写，随后读取当前body/version/deleted返回领域冲突。activity markdown delta以CTE返回的实际旧body计算，不再以无条件覆盖前临时重读值伪装并发检查。
- API/第一方迁移：请求从`{body}`收紧为`{body,baseUpdatedAt}`，旧客户端缺字段400；成功200 `CommentItem`不变。冲突为409、code `COMMENT_EDIT_CONFLICT`、details `{body,updatedAt,deleted}`。第一方传当前CommentItem.updatedAt，严格解析details并刷新本地项，删除冲突同步清附件/能力；用户确认后下一次编辑自然携带新基线。
- 兼容/查询/回滚：前后端需同批部署；开发期不保留可绕过CAS的旧body-only兼容路径。EXPLAIN中CTE锁定和更新都命中comments主键，无额外列表查询、N+1或索引需求。代码回滚无schema步骤，但会重新允许静默覆盖并使新客户端base字段因严格JSON成为400，不提供兼容价值。BUG-085能力一致性和TEST-038完整权限矩阵保持独立。

## BUG-085：评论回复 capability 共用目标拉黑事实（generation154保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、开发库reset、回填、双读或双写，保持154。项目owner语义继续是`effective_project_access`的完整developer集合，不新增单owner列；直接owner目标继续读取各自现有owner/user FK。
- 批量协议：新增内部批量resolver接受去重的(type,target ID,version ID)，一个UNNEST查询解析mod、modpack、六类simple project、mod resource、community post、blueprint、skin及player profile；项目按type/internal ID关联developer集合，直接owner按owner ID关联user_blocks。受支持目标缺失返回错误；其他无owner目标返回false。单目标创建/list capability wrapper也调用该批量实现。
- 序列化/API：权限装配主查询多读内部target identity，不新增JSON字段；`CanReply`现在同时要求非deleted、comment.create和target owner未拉黑。列表`capabilities.canCreate`、POST 403及逐项`canReply`因此一致。路径、状态码、TypeScript DTO和组件不变，第一方按钮自动消费修正值。
- 规模/兼容/回滚：根/回复/thread/floor/watch响应每次最多增加一次批量查询，1与100项watch均为13，不随项数线性增长；目标关系和user_blocks使用既有索引。无需协调前端或数据部署；代码回滚只恢复伪可用按钮，无兼容价值。SEC-031目标可见性、BUG-086及TEST-038保持独立。

## BUG-086：皮肤管理员编辑能力序列化（generation154保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、开发库reset、回填、双读或双写，保持154；不增加数据库查询或索引需求。
- 内部协议：`skinAssetJSON`参数从内部viewer ID收紧为完整`security.Claims`；所有目录、创建、详情、更新和衣柜调用方协调迁移。`canEdit`复用PUT/DELETE已有`isSkinAdmin`，避免serializer再维护有损的owner-only权限子集。
- API/能力：JSON字段、类型和状态码不变。owner继续`canEdit=true`；`skin.admin`及`admin.*`对可管理资源从false修正为true；普通viewer保持false。`canUse`明确独立，管理员对他人private/pending资源仍为false，只有owner或active+approved+非private满足装备预检。
- 前端/兼容/回滚：第一方详情既有管理区直接消费`texture.canEdit`，无需代码或部署协议迁移；编辑入口会与已存在的GET/PUT/DELETE授权一致。代码回滚无Schema步骤但会恢复错误隐藏，无兼容收益。SEC-034隐私矩阵、BUG-087创建审核及其他皮肤生命周期Finding保持独立。

## BUG-087：皮肤创建审核与首次发布协议（generation154保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、开发库reset或历史回填，保持154。复用`skin_assets.review_status/published_revision_id`、内容subject/localization、revision/change request/review event/audit表及write-maintained `skin_public_catalog`；历史已错误直发对象没有可靠审核意图，不伪造回填。
- 创建事务：POST按CatalogCreate与共享免审predicate得到pending/approved，持久化完整`skinAssetContentSnapshot`（含全部本地化且replace=true）和operation=create审核链。pending主体/语言不绑定published revision且公共投影为0；approved路径由同一snapshot apply绑定revision并产生一行投影。衣柜写与这些事实仍同commit，任一步失败全回滚。
- 审核生命周期：首次批准原子把主体/语言设approved并绑定revision；首次拒绝把尚无published revision的主体及语言设rejected，已发布编辑拒绝保持approved。任何`published_revision_id=nil`的PUT重提按create读取CatalogCreate，不能在CatalogEdit关闭时免审；首次发布的metadata-only兼容snapshot会提升现有未发布语言而不留下rejected状态。
- API/第一方/回滚：POST路径、请求、201和`SkinTexture`形状不变，普通用户默认得到既有合法值`reviewStatus=pending`；owner仍可查看/个人使用，匿名目录与详情只接受approved。第一方现有通知和跳转无需改动。代码回滚无Schema步骤但会恢复公开审核旁路并停止创建审计事实，不保留兼容价值。BUG-088共享blob、SEC-034隐私与完整皮肤测试矩阵保持独立。

## BUG-088：共享Minecraft纹理Blob归属、引用与GC（generation155）

- Schema generation：154→155。`skin_texture_blobs`新增`active_reference_count bigint not null default 0 check >=0`、`idx_skin_texture_blobs_unreferenced(created_at,hash) where active_reference_count=0`；`skin_assets`新增`idx_skin_assets_active_blob(blob_hash,id) where status='active'`，原完整blob前导索引继续覆盖FK维护。
- 引用事实：`trg_skin_assets_blob_reference_count`在insert、status/blob update及delete后原子增减共享Blob引用，低于零或目标Blob缺失使业务事务失败。`rebuild_skin_texture_blob_reference_counts()`取得全局独占calibration advisory锁后按active skin重新投影，供一致性校准；普通持久化/删除持同键shared锁及hash独占锁，避免校准漂移和Blob/delete死锁。
- 归属/额度：派生`oss_files`写为`source=minecraft_texture_derived`、`source_size_bytes=0`、`uploader_id=NULL`，stored size仍是真实PNG字节；原始上传文件不改变。复用同hash只读现有active Blob，不增加任何用户额度或PUT。
- 回收/补偿：owner删除和治理删除共用`softDeleteSkinAssetTx`，解绑档案/衣柜、soft-delete资产、引用归零检查、file tombstone和OSS deletion Outbox在同事务。Maintenance按零引用部分索引每批最多1000收敛漏项。每次物理对象使用新UUID生命周期Key；deleted Blob恢复时写新file/key，旧Outbox不能命中新对象。Put后任何外层rollback以独立unregistered任务补偿，注册成功或commit不确定由active-file guard阻止误删。
- API/部署/回滚：皮肤POST、Yggdrasil set texture、纹理GET、成功DTO、状态码和第一方前端均不变。开发期本机隔离库完整reset/seed到155，无远程/生产DDL、历史回填、双读或双写；回滚需整代回154且会恢复额度误计、永久泄漏和旧Key竞争，无兼容价值。

## BUG-089：收藏全量替换的原子身份验证（generation155保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、开发库reset、回填、双读或双写，保持155。复用`favorite_collections(user_id,public_id)`身份事实、集合内item唯一约束及现有索引；本项真实PG只创建并清理专用用户、集合、目标与关系，未访问远程数据库。
- 写协议：PUT先在事务外严格规范化、去重并限制最多100个集合ID；Repeatable Read事务解析目标后，以`user_id + ANY(public IDs) FOR UPDATE`锁定全部集合且要求数量精确相等。missing/跨owner在默认集合upsert或DELETE之前400。合法请求才在同一事务确保默认集合、删除目标的旧关系并单条批量INSERT；影响行数不精确则500并全回滚。PATCH复用同一验证helper。
- API/第一方：路径、请求字段、合法成功200及`{saved:true,collectionIds:[...]}`不变；重复ID响应为规范化唯一列表，空数组继续明确清空。无效格式、缺失或非本人集合现在稳定400且原关系不变。第一方继续使用有界PATCH delta，前端类型、组件和部署无需协调；`entityKey`别名由LEGACY-018独立处理。
- 并发/回滚：集合验证锁持续到commit，集合删除必须等待；等待后产生的序列化或数据库错误失败关闭并回滚，不存在校验后短写成功。代码回滚无Schema步骤，但会恢复先删除、逐项忽略0 affected及200假成功，不提供兼容价值。SEC-036额度、ARCH-023读取完整性、ARCH-024写错误分类、STYLE-006和TEST-041保持独立。

## BUG-090：社区编辑 published revision CAS（generation155保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、回填、双读或双写，保持155。复用`community_posts.published_revision_id`、`content_revisions.public_id`唯一事实和既有聚合advisory锁；锁内查询按post身份并联接revision主键，实际计划为两个Index Scan，无新增索引或N+1。
- 写入协议：详情响应新增可选`publishedRevisionId`。PUT接收`baseRevisionId`，非空时必须是9字符公共ID；事务以与`createContentRevisionTx`相同的`community_post:{publicID}`键加锁，随后锁post并重读当前revision公共ID。不相等在引用、悬赏、revision/change/audit和apply前返回409 `COMMUNITY_POST_EDIT_CONFLICT`、details `{currentRevisionId}`；相等才把锁内内部ID作为BaseRevision继续。review config在同一tx读取。
- 第一方/兼容：CommunityPost类型、详情加载和编辑器状态携带published token，PUT序列化`baseRevisionId`。冲突保留本地draft、显示双语“重新加载并核对”提示且不导航/自动改基线；成功200 `{id,reviewStatus,revisionId,changeRequestId}`不变。旧PUT客户端对已有发布对象由成功收紧为409，需同批迁移；创建POST携带空token但服务端分离mutation metadata，不进入revision snapshot。
- 验证/重置/回滚：真实验证安装generation155单连接临时完整Schema并自动drop，覆盖首写/stale/missing/malformed及精确revision数。首个RED曾在本机public留下不可变历史，已用现有数据库名确认与development双确认保护整代reset，直查155、4 seeds、零测试行；这不是部署步骤。回滚代码无Schema操作，但会重新让排队旧快照自动获批，不提供兼容价值。BUG-091、ARCH-025与TEST-042独立。

## BUG-091：社区来源语言权威与确认协议（generation155保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、开发库reset、回填、双读或双写，保持155。继续使用`community_posts.source_locale`与revision JSON的`sourceLocale`；真实验证只安装并自动删除单连接完整临时Schema，不访问远端或共享public数据。
- 请求协议：POST/PUT既有`sourceLocale`现在先通过共享BCP-47 alias规范化，并必须属于站内8种可编辑语言；显式合法值原样成为修订事实，显式unsupported 400。字段缺省时只接受脚本明确或拉丁词表唯一最高且>=2命中的检测，否则400要求调用方确认；不再默认`en-US`。
- 第一方/兼容：`CommunityPostDraft.sourceLocale`由可选收紧为必填，编辑器展示完整站内语言selector，新建初始为空、编辑回填详情、自动草稿保留选择，提交前显示本地化必选提示。成功201/200、详情DTO和翻译接口不新增字段；旧客户端的明显文本仍可由高置信回退，低置信请求需迁移为显式选择，前后端同批部署。
- 下游/回滚：主表、published revision snapshot和详情保存同一规范值；翻译请求既有查询继续把该值写入AI payload、并作为目标相等判断和缓存身份来源，无新增查询或N+1。代码回滚无Schema步骤，但会重新丢弃用户选择并制造英语默认事实，不提供兼容价值。BUG-092/093、SEC-038与ARCH-025保持独立。

## BUG-092：社区翻译可靠入队与权威状态（generation155保持）

- Schema/数据：本项无新DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持155。复用LEGACY-019/OPS-019已建立的`ai_tasks`、统一`nats_outbox`状态机、active task恢复索引和aggregate事件索引；专项真实PG安装完整临时Schema并自动drop。
- 提交/失败协议：社区翻译的quota检查、task行与`ai.community_post_translation.requested` event在一个事务。事件写失败回滚task并返回503；只有两者都提交才返回queued。NATS发布不在HTTP事务后同步执行，dispatcher失败只更新event为retryable failed，保留task queued等待重试；重连后event published、Worker再CAS task为running。
- API/第一方：POST成功202及`{taskId,status}`形状不变，queued含义明确为“数据库可靠接受”，不承诺NATS本次已发布。结果GET继续读取task权威状态；Outbox暂时failed时返回queued是与可恢复事实一致，而非旧实现中数据库task已failed却返回缓存queued。前端轮询与终态集合无需修改。
- 兼容/回滚：实现已由LEGACY-019迁移，BUG-092只增加专项状态验收，不复制publisher或新状态字段。代码回滚到direct publish会恢复提交窗口、不可恢复任务和响应/数据库分裂，不提供兼容价值。OPS-019负责orphan/stale恢复与dead replay；SEC-038、BUG-094和TEST-043保持独立。

## BUG-093：Accept-Language 权重协商（generation155保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、开发库reset、回填、双读或双写，保持155；解析完全位于HTTP请求内存边界，不新增数据库查询、持久事实或缓存键。
- 请求协议：既有`Accept-Language`头现在按合法qvalue的最高正权重选择具体BCP-47语言；省略q等于1，同权重保持原序。`q=0`、`*`、非法/越界q、未知或重复参数及非法标签不再被选中；合法标签继续使用既有大小写和alias规范化。
- 优先级/调用方：公共内容本地化与simple-project目录共享同一解析器；显式`locale` query和登录用户保存的primary/secondary偏好保持既有更高优先级。没有合法具体候选时仍使用既有默认语言，不新增406、错误DTO或前端处理分支。
- 兼容/回滚：对合法、已按偏好排序且无q参数的常见头行为不变；纠正低权重或明确拒绝项在前的标准头。代码回滚会重新忽略q并可能选择q=0语言，不能作为有价值兼容。BUG-091、SEC-038与TEST-043保持独立。

## BUG-094：AI 并发配置单一权威（generation155保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、开发库reset、回填或双写，保持155。`system_settings`仍保存加密JSON；`ai.config.taskModels[]`逻辑shape从`{taskType,modelKey,concurrencyLimit,timeoutSeconds,prompt}`收紧为`{taskType,modelKey,timeoutSeconds,prompt}`。
- 读取/迁移：既有加密设置中的未知`concurrencyLimit`由typed JSON读取自然忽略，GET不再回显，下次正常PUT以新shape覆盖；不为从未执行的字段进行密钥依赖批量回填。每类型timeout仍由AI Worker读取并建立供应商请求context。
- API/第一方：`GET/PUT /api/v1/admin/ai/config` breaking删除taskModels item的`concurrencyLimit`，strict PUT对旧字段400；第一方类型、默认draft、表格列和双语键同批删除。开发期无已发布兼容责任，不保留silent-ignore Wrapper。
- 唯一执行权威：`GET/PUT /api/v1/admin/config/nats`的`tasks[].maxConcurrent`及NATS后台控件不变；task code `ai`统一限制本应用实例的AI Handler并发，集群容量为各活跃实例容量之和。页面明确该跨实例语义；如未来需要全局/分类型硬限，必须设计数据库permit/lease协议而非恢复本地字段。
- 回滚：代码回滚会重新展示和保存无执行效果的第二权威，不能作为兼容方案。BUG-056/LEGACY-012已保证NATS热更新与持久设置一致；TEST-043其余AI端到端矩阵保持独立。

## BUG-102：站务管理端严格语言身份（generation155保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、开发库reset、回填、双读或双写，保持155。继续使用`site_page_translations(page_id,locale)`和`site_changelog_translations(changelog_id,locale)`现有身份；真实验证只安装并自动drop单连接完整临时Schema，未访问远端或共享public数据。
- 管理协议：`GET/PUT /api/v1/admin/site-affairs/about/{locale}`和changelog管理POST/PUT在首个查询或副作用前规范化locale，并只接受站内8种可编辑语言。合法alias如`fr_fr`写为`fr-FR`；空值、畸形或unsupported值稳定400，不再回退后读取或upsert `zh-CN`。错误请求不会创建changelog，也不会改变既有父记录的日期/发布状态或任一翻译。
- 公开/第一方：公开About、changelog列表和详情继续使用既有“请求语言→简中→英文→其他”读取回退，不新增406或错误DTO。合法管理成功状态码、请求字段和响应shape不变；第一方语言selector本就来自同一站内注册表，无类型、页面或部署迁移。
- 兼容/回滚：这是对错误成功输入的收紧，不保留将未知管理语言解释为简中的兼容入口。代码回滚无Schema步骤，但会恢复目标身份混淆和静默覆盖，不提供兼容价值。BUG-103、BUG-104及TEST-046其余范围保持独立。

## BUG-103：站务逐翻译发布状态（generation155→156）

- Schema：`site_changelog_translations`新增`status text not null default 'draft' check(status in ('draft','published'))`；新增部分索引`idx_site_changelog_translations_published(changelog_id,locale) where status='published'`，服务公开父项EXISTS与语言fallback。`site_pages`及已有translation status不改列。Schema metadata升级为156，开发重置文档同步；旧开发库需受保护整代重建，不提供迁移、回填、双读或双写。
- 写协议：About upsert始终保持父singleton published，publish布尔只写目标`site_page_translations.status`并在发布时推进父published revision。Changelog POST创建published父项并把publish写入目标translation；PUT只更新父日期并upsert目标translation status，不再让任一语言覆盖父status。错误事务边界和合法成功状态码不变。
- 读/API：公开About仍要求父项启用和目标translation published。公开changelog详情及列表只选择published translation；列表在keyset page前用partial-indexed EXISTS排除draft-only父项。后台列表/详情的`translations[locale]`由`{title,bodyMarkdown}`扩展为`{title,bodyMarkdown,status}`，父`status`保留全局对象事实；第一方以逐locale status初始化编辑控件。
- 部署/回滚：generation156完整临时Schema、种子、跨语言行为和百万行计划已实际通过并自动清理；未连接或写入远端，未破坏性重置本机共享public。正式开发环境切换需运行已有环境/库名/双确认保护的整代reset。回滚需整代回155且会恢复跨语言全局隐藏，不保留兼容价值。BUG-104与TEST-046其余范围独立。

## BUG-104：站务编辑条件版本协议（generation156保持）

- Schema/数据：无新DDL、表、列、索引、约束、trigger、generation、reset、回填、双读或双写，保持156。About复用`site_page_translations.revision`作为每语言版本；changelog复用父`site_changelogs.updated_at`作为包含共享日期和全部翻译的整记录版本，成功以数据库时钟及至少1微秒增量保证单调。
- About API：`PUT /api/v1/admin/site-affairs/about/{locale}`新增必填非负`baseRevision`。0只创建尚不存在的语言，正值按`page_id+locale+revision`条件更新并原子递增；成功data新增`revision`。冲突409 code为`SITE_PAGE_EDIT_CONFLICT`，details返回当前`locale/title/bodyMarkdown/status/revision`，父项更新随事务回滚。
- Changelog API：`PUT /api/v1/admin/site-affairs/changelogs/{id}`新增必填RFC3339 `baseUpdatedAt`，按`public_id+updated_at`条件更新父日期/版本后在同事务upsert目标翻译；后台详情和成功data新增`updatedAt`，列表既有字段不变。冲突409 code为`SITE_CHANGELOG_EDIT_CONFLICT`，details返回当前`changeDate/status/translations/updatedAt`；POST创建不要求基线。
- 第一方/兼容/审计：About提交已加载revision；changelog编辑从列表项保存updatedAt并只在PUT提交。两个稳定409显示本地化恢复提示且不清空当前输入。成功管理员日志保留并新增前后版本metadata；冲突不产生成功日志。缺少基线的旧PUT收紧为400，开发期前后端同批部署，不保留无条件覆盖兼容入口；代码回滚无Schema步骤但会恢复静默丢写。

## BUG-105：未解析引用来源投影全覆盖（generation156→157）

- Schema：不增表、列、索引或约束。替换`refresh_unresolved_reference_catalog_general`，在既有mod/community/modpack外新增`minecraft_server_mods→minecraft_servers`、`simple_project_parent_refs→simple_projects`和`mod_content_resource→catalog_entities`映射。新增server mod的`server_id`、simple parent的`project_id`重绑trigger，以及服务器`name/public_id`、simple project `primary_name/public_id`名称trigger；catalog entity既有label trigger扩展为同时刷新通用mod-content来源。
- 数据/部署：持久投影语义变化，Schema metadata 156→157。旧开发库按`DEVELOPMENT_SCHEMA_RESET.md`既有环境/库名/双确认流程整代重建，空库直接建立最终七类映射；不提供在线回填、迁移、双读或双写。专项验证只安装并自动drop本机单连接临时Schema，未访问或破坏性重置远端、未知或共享public库。
- API/第一方：`GET /api/v1/admin/unresolved-references`字段、分页、搜索和状态码不变；已知来源的既有`sourceLabel/sourcePublicId`现在对七类生产者完整且随更新同步。第一方把六类具有`public_routes`目标的来源渲染成统一公开短链，并同时显示sourceType/publicId/fieldPath；未知或不可路由类型保留文本诊断，不伪造路径。
- 性能/回滚：读取继续只扫`unresolved_reference_catalog`窄行，百万行keyset和literal-prefix计划不变；额外JOIN只发生在单条引用写时刷新且均走来源主键。代码/Schema回滚需整代回156并会恢复空来源，不能作为兼容方案。

## BUG-106：未解析引用权威类型注册接口（generation157保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持157。类型接口读取既有小型`resource_kinds`，包括管理上仍需诊断的`user_visible=false`项；项目类型直接复用当前community project生产者白名单，避免另建持久枚举。
- API：新增`GET /api/v1/admin/unresolved-reference-types`，与列表相同受`reference.unresolved.read`保护，成功data为`{items:string[]}`。items包含全部项目生产者类型、tag/enchantment/server兼容类型和按display order排列的目录kind并去重；数据库读取/Scan/rows错误或不可往返registry code稳定500，不返回不完整成功。
- 第一方/兼容：`AdminUnresolvedReferences`在token变化时独立加载该接口并直接生成selector，删除六项硬编码；已知翻译继续本地化，动态/未知code显示其原值。既有`GET /api/v1/admin/unresolved-references`响应、cursor和搜索协议不变，type参数上限由64扩至与catalog kind一致的128字节；旧合法过滤全部兼容。
- 性能/回滚：类型接口只在面板初始化读取小注册表，不并入翻页；百万行列表仍为原单条keyset SQL，选择性搜索预算不变。代码回滚无Schema步骤，但会删除新接口并恢复前端枚举缺口，前后端需同批部署。

## BUG-107：重复搜索请求代次验收（generation157保持）

- Schema/API：无DDL、数据、generation、reset、回填、路由、DTO、cursor、搜索或状态码变化，保持157；本项确认PERF-059迁移后当前第一方行为并补独立防回归测试。
- 第一方状态：每次submit通过`resetPagination`清空cursor历史、置loading并递增`requestVersion`；effect依赖该代次，所以规范化query与上次相同也重新请求。cleanup使旧代次inactive并abort，只有最新active代次可写结果/错误或在finally结束loading。
- 兼容/回滚：正常首次搜索、筛选、前后页行为不变；重复提交从永久loading纠正为明确刷新。回滚到只依赖query/page的旧实现会恢复死锁，没有兼容价值。

## BUG-108：并发收缩后的空cursor页语义（generation157保持）

- Schema/API：无DDL、数据、generation、reset、回填、路由或新字段，保持157。PERF-059现有`items/limit/hasMore/nextCursor`协议已经删除同步total、window count和offset；本项通过故障场景确认空页不恢复第二套计数语义。
- 行为：cursor之后没有行时返回200、空items、原limit、`hasMore=false`和空nextCursor，只描述当前位置而不声称过滤集合总数为0。第一方Previous以提交Next时保存的cursorHistory恢复，不依赖当前items或total。
- 并发/回滚：真实PG在首面与下一页之间删除投影事实，旧cursor仍严格scope校验并稳定返回可恢复空页。代码回滚到OFFSET/window total会恢复矛盾计数及全量计数成本，无兼容价值。

## BUG-109：举报目标主体与项目治理联系人分离（generation157→158）

- Schema：`reports.target_author_id`替换为可空FK `target_actor_id`，新增非空`target_actor_role`及`submitter/author/owner/subject`闭集CHECK。列保存举报创建时快照主体的真实业务关系而非统一“作者”解释。Schema metadata 157→158；旧开发库经既有环境/库名/双确认保护整代重建，不做在线迁移、历史回填、双列、双读或双写。
- 创建/快照：Mod、simple project和server写actor role submitter，其中server JSON由`author`改为`submitter`；comment/community post写author，skin/blueprint写owner，用户目标写subject。actor用户删除后FK仍可置空而角色语义保留；自动导入的空submitter不会被伪造联系人。
- 审核/通知：有效且隐藏目标时，直接用户内容通知快照中的真实actor；项目按`targetType+targetPublicId`一次查询`effective_project_access`的distinct developer。该视图要求批准的个人作者claim或批准的作者团队关系、批准且permission-granting的项目绑定；editor和普通submitter不进入。查询在隐藏前完成，任一读取/Outbox错误回滚整个审核事务。
- API/第一方：`GET /api/v1/admin/reports/{id}`删除`targetAuthorId/targetAuthorName`，新增`targetActorId/targetActorName/targetActorRole`；第一方同批更新类型并显示本地化角色。举报创建/审核请求、路径、成功状态码和通知模板不变。开发期不保留旧DTO别名；前后端必须同批部署。
- 回滚：需整代回157并同步回滚第一方，但会恢复把提交者称为作者、向无关提交者发送治理通知和漏掉真实开发者，因此不作为兼容方案。未对远端、未知或共享public Schema执行DDL/reset。

## BUG-110：举报结论与惩罚动作互斥（generation158保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持158。报告状态、审核记录、moderation action、ban记录和Outbox结构均不变；非法请求在打开事务前返回，因此没有部分持久事实或补偿需求。
- API：`POST /api/v1/admin/reports/{id}/resolve`保持原请求/成功DTO；新增语义约束为`conclusion=invalid`时`deleteTarget`必须false且`banUserId`必须空，否则400。`invalid`无动作、`valid`无动作以及具备相应权限的`valid` hide/ban继续兼容。
- 权限/调用方：动作一致性先于权限检查，避免把逻辑非法请求误报403或接触数据库；通过一致性后仍逐项检查`report.action.delete`与`report.action.ban`。第一方默认valid且字段不变，无代码迁移。独立处置继续使用专用管理API，不在举报resolver增加绕行开关。
- 回滚：代码回滚会恢复resolved_invalid与隐藏/封禁并存的矛盾事实，不提供兼容价值；无Schema回滚步骤，未访问远端或共享public。

## BUG-111：临时封禁最短未来区间（generation158→159）

- Schema：`ban_records`新增`check(ends_at is null or ends_at>=starts_at+interval '1 minute')`。永久封禁的null不变；临时封禁必须相对数据库`starts_at`至少持续1分钟。Schema metadata升级159，旧开发库按既有环境/库名/双确认保护整代重建；不做在线ALTER、历史截止时间推断、回填、双读或双写。
- API：`POST /api/v1/admin/bans`及`POST /api/v1/admin/reports/{id}/resolve`的字段和成功响应不变。实际创建临时封禁时，`endsAt`/`banEndsAt`必须在服务校验时至少晚于当前时间1分钟，否则在打开事务前400；举报不携带`banUserId`时不解释无动作的截止字段。永久封禁和有效未来封禁保持兼容。
- 内部/维护：`createBanTx`在原因/用户读取和INSERT前防御复验，保证新调用方不能绕过Handler。既有`expireBans`继续分批把`ends_at<=now()`的active记录置expired、删除对应`governance_ban`角色并刷新权限版本；本项不改变批量大小、查询或调度，因此OPS-020独立。
- 验证/部署/回滚：完整generation159临时Schema、真实PG非法零副作用/合法写入/自然到期清理及三档规模测试通过，临时对象自动清理且未访问远端或共享public。回滚需整代回158并会重新允许创建即过期的矛盾事实，不提供兼容价值；第一方无需字段迁移。

## BUG-112：举报责任锁与审计接管（generation159→160）

- Schema：`reports.claimed_by`改为`on delete restrict`，新增`status<>'in_review' or (claimed_by is not null and claimed_at is not null)` CHECK。新增`report_assignment_events(report_id,action,actor_id,previous_assignee_id,assignee_id,reason,created_at)`，action闭集claim/takeover且takeover原因非空；索引为`(report_id,created_at,id)`。Schema metadata升级160，旧库整代重建，不在线迁移、责任推断、回填或双写。
- 权限/命令：新增`report.action.takeover` administration权限和`POST /api/v1/admin/reports/{id}/takeover`，body为`{reason}`且1..1000字符。claim在事务内写状态与claim事件；takeover以报告行锁串行替换当前责任并保存旧/新责任和原因。普通`report.review`不包含接管权；管理员种子显式拥有新权限。
- 结案/详情：既有resolve请求和成功DTO不变，但只允许当前责任人处理in_review记录；pending、非责任人、旧责任人或已完成统一409且在review/action/outbox前失败。管理详情新增可空`claimedById/claimedByName`、布尔`claimedByCurrentUser/canTakeover`；既有字段不删除。
- 第一方/部署/回滚：pending只显示领取；in_review当前责任人显示结案表单，其他人仅在`canTakeover`时显示接管按钮并要求原因。完整generation160临时Schema、真实责任链和两端发布门通过，未操作远端或共享public。回滚需整代回159且同步移除新路由/DTO/UI，但会恢复无责任约束结案，不提供兼容价值。

## BUG-113：统一举报支持整合包（generation160→161）

- Schema：`reports.target_type` CHECK闭集加入`modpack`，Schema metadata升级161；无新表、列、索引、trigger或历史数据变换。旧开发库按既有环境/库名/双确认保护整代重建，不在线ALTER、回填、双读或双写；完整临时Schema验证后自动清理。
- API/内部：`GET /api/v1/reports/reasons?targetType=modpack`返回与Mod项目一致的内容/恶意/许可/其他原因闭集，既有举报创建路由接受`targetType=modpack`。快照读取`modpacks`权威字段并保存actor role submitter；项目访问类型保持规范`modpack`；valid+delete使用既有审核事务把目标`review_status`置rejected并写一条moderation action。管理详情、领取、resolve请求与成功DTO不变。
- 第一方：Modpack详情在标题操作区复用`UnifiedReportButton`，提交`record.id`、展示名、作者列表和规范目标类型；en/zh报告目标表加入Modpack/整合包。未登录、原因选择、提交成功与错误行为沿用统一组件，无兼容分支。
- 部署/回滚：后端generation161与第一方应同批部署；回滚需整代回160并移除详情入口，否则服务会拒绝modpack目标。回滚会恢复核心项目类型无法举报的缺口，不提供兼容价值；验证未操作远端、未知或共享public Schema。

## BUG-114：关于页locale与草稿一致性（generation161保持）

- Schema/API：无DDL、数据迁移、字段或路由变化，generation保持161。`GET/PUT /api/v1/admin/site-affairs/about/{locale}`协议不变；PUT继续携带BUG-104的`baseRevision`并由后端对对应locale执行CAS。
- 第一方：切换locale立即丢弃屏幕上的旧语言草稿并进入不可写loading；GET携带AbortSignal和单调请求代次，只有请求locale、响应`draft.locale`与当前选择完全一致才提交状态。失败或错语言保持禁用并提供重试；输入、预览、保存和发布共享同一可写谓词。
- 写入/竞态：保存捕获单一`saveDraft`，路径取`saveDraft.locale`，payload的标题/正文/revision也取同一对象；保存中切换语言后，旧请求结果不得改变新语言状态。由此无需新增请求字段、兼容别名或服务端分支。
- 部署/回滚：纯第一方收紧可单独部署并与现有后端兼容；回滚会恢复跨语言覆盖窗口，不提供兼容价值。真实PG仅用会话临时Schema复核既有locale/CAS，未操作远端或共享public。

## BUG-115：更新日志精确locale草稿（generation161保持）

- Schema/API：无DDL、迁移、请求或响应变化，generation保持161。后台列表继续返回每条记录的`translations`，POST/PUT继续提交单一`locale,title,bodyMarkdown,publish`；PUT保留父`baseUpdatedAt` CAS。
- 第一方读取：选中记录后保留完整`editingItem`；目标locale只读取同名translation，缺失时使用空且未发布的表单，不再以任意第一翻译回退。列表摘要仍可显示可用标题，但该展示回退不进入编辑或写payload。
- 第一方草稿：`localeDrafts`按语言保存未提交title/body/publish；切换先缓存离开语言，再恢复目标缓存或权威翻译，因此新建和编辑的未保存内容均可往返。保存成功才清空editingItem和全部语言缓存。
- 部署/回滚：纯前端状态修复可与现有generation161服务独立部署；回滚会恢复跨语言复制风险，无兼容价值。没有远端/共享public操作或数据回填。

## BUG-116：临时封禁datetime-local线格式（generation161保持）

- Schema/API：无DDL、路由、字段或后端类型变化，generation保持161。`POST /api/v1/admin/bans`的可空`endsAt`本就要求Go `time.Time`可解码的RFC3339；第一方从不合规无时区文本迁移为UTC ISO字符串，永久封禁仍发送null。
- 第一方转换：仅接受`YYYY-MM-DDTHH:mm`，以浏览器本地时区构造瞬时值并核对年月日时分，拒绝非法日历、越界时间、DST空洞、预带时区或任意文本；合法值输出`YYYY-MM-DDTHH:mm:00.000Z`。解析失败显示双语错误且不发请求。
- 服务端边界：标准JSON解码后继续执行至少未来1分钟验证，事务写入和`ban_records`CHECK不变；公开状态、角色过期和维护Worker协议不变。没有新增未经产品定义的最长封禁期限。
- 部署/回滚：前端可独立部署并向后兼容既有服务；回滚会恢复临时封禁全部400的故障，无兼容价值。未操作远端/共享public或历史封禁事实。

## BUG-117：封禁表单异步事件生命周期（generation161保持）

- Schema/API：无DDL、数据、路由、请求或响应变化，generation保持161。`POST /api/v1/admin/bans`仍返回201，active唯一约束和事务语义不变。
- 第一方：submit同步阶段捕获`formElement`并据此创建FormData；请求await后只使用稳定引用reset。请求catch失败即return，成功后的reset、游标归零和列表刷新不再处于同一“创建失败”catch内。
- 兼容/回滚：无调用方迁移；纯前端生命周期修复可独立部署。回滚会恢复成功误报和重复操作诱因，无兼容价值；未操作远端/共享public。

## BUG-118：治理异步请求身份（generation161保持）

- Schema/API：无DDL、迁移、路由、字段或响应变化，generation保持161。列表、详情、领取、接管、结案和About继续使用既有HTTP合同。
- 举报读取：列表请求携带AbortSignal并由effect cleanup取消旧status/cursor scope；详情每次先取消前一请求并递增generation，响应还需匹配当前期望ID和自身ID。状态/分页/结案清空统一使请求失效，卸载取消剩余详情请求。
- 动作续体：claim/takeover/resolve冻结reportId；完成后只有它仍是用户当前选择才刷新、清空或显示错误。服务端已经接收的命令不伪取消，前端仅丢弃不再属于当前界面的结果。
- About/部署：About沿用BUG-114的controller+generation+request/response/selected locale闭集。纯前端修复可独立部署；回滚恢复乱序覆盖风险，无兼容价值，未操作远端/共享public。

## BUG-119：举报证据批量部分成功（generation161保持）

- Schema/API：无DDL、迁移、路由、请求或响应变化，generation保持161。ticket、complete与最终`evidenceIds`协议不变；不新增DELETE端点。
- 第一方批处理：每个文件独立执行hash→ticket→OSS PUT（如需）→complete；成功后立即按ID去重追加可见evidence，失败保存originalName/error并继续下一文件。批次结束不再依赖局部成功数组，最终举报只提交可见成功ID。
- 状态/清理：失败项逐文件显示，成功项继续可从表单移除；提交成功清空两类状态。未绑定或被表单移除的temporary metadata继续在24小时后通过既有部分索引维护事务和OSS deletion outbox清理。
- 部署/回滚：纯前端修复可独立部署；回滚恢复部分成功丢失，无兼容价值。真实PG仅复核既有原子注册/重试，未操作远端/共享public。

## BUG-126：自动草稿最近远端快照（generation161保持）

- Schema/API：无DDL、迁移、路由、字段、请求或响应变化，generation保持161。POST `/api/v1/users/me/drafts`、按ID恢复、complete和保留期协议不变；服务端仍以同一draft key upsert完整payload。
- 第一方状态：`lastSavedRef`是唯一远端snapshot基线；页面初始化、恢复成功和保存成功分别以其实际payload推进。永久initial baseline删除，因此保存B后回A及恢复B后撤销到A都会发送A。
- 异步边界：请求前冻结payload和serialized snapshot，响应后不读取新界面值冒充已保存；检测到期间变化时最多立即追写一次，追写失败保留最后成功snapshot供既有定时/隐藏入口重试。`savingRef`继续串行化批次。
- 部署/回滚：九个既有编辑器共用Hook，无逐调用方协议迁移；纯前端修复可独立部署。回滚恢复远端停留中间版本和错误“已保存”状态，无兼容价值；未操作远端/共享public或历史草稿。

## BUG-127：项目关注keyset分页（generation161保持）

- Schema：无DDL、迁移、reset、回填或双写，generation保持161。分页顺序和谓词复用既有`idx_project_follows_user_created(user_id,created_at DESC,project_route_id)`；源码合同和100k真实计划锁定该索引，无新增total/count查询。
- API：GET `/api/v1/users/me/project-follows`保留q/type/limit/items，删除offset请求/响应并新增可选opaque cursor与`hasMore,nextCursor`。游标绑定user/q/type/limit；未知/重复参数、非法类型/页大小/游标或跨scope复用返回400。默认与第一方页大小为40、最大100。
- 查询/可见性：按`created_at DESC,project_route_id ASC`读取limit+1；下一页用最后返回tuple继续。隐藏关注仍输出unavailable安全占位且不泄露name/url/updatedAt；搜索隐藏项只接受精确public ID。
- 第一方迁移：API路径零offset；面板首面替换、续页身份去重合并并显示继续加载，AbortController和generation使筛选变化淘汰旧响应。开发期前后端同批部署，不保留offset兼容层或第二分页协议。
- 回滚：应用整体回滚到旧前后端组合才协议自洽，但会恢复100项永久截断和深offset；无数据转换或远端/共享public操作。

## SEC-015：自动更新配置GET只读化（generation161保持）

- Schema：无DDL、迁移、索引、约束、reset、回填或双写，generation保持161。`project_auto_update_settings`、外部来源、run与调度器查询均不变。
- GET：`/api/v1/projects/{type}/{id}/automation`只SELECT来源和已持久配置。缺失的三种kind在内存补齐，来源可预选但`enabled=false`、运行时间为空且不产生configuredBy；响应仍为`project,sources,settings`。
- PUT：请求/响应协议不变；仍是唯一配置写入口并要求configure能力。三项完整验证、来源存在性、许可/覆盖权限、nextRun和configuredBy继续在同一事务提交；view-only请求在副作用前403。
- 第一方/部署：前端本就用相同三项disabled默认规范化缺项，无代码迁移；后端可独立部署且立即停止GET副作用。回滚会恢复只读越权启用，无兼容价值；未操作远端/共享public或既有配置。

## SEC-016：爬虫AI原子日预算（generation161→162）

- Schema：`seed_crawler_translation_tasks`新增`usage_date date not null default current_date`和`quota_reserved_tokens bigint not null default 0`；input/output/reserved均增加非负CHECK。新增`idx_seed_crawler_translation_tasks_daily_budget(usage_date) include(input_tokens,output_tokens,quota_reserved_tokens)`，用于实际+在途总量的覆盖读取。
- 事务合同：外呼前按数据库日期取得advisory transaction lock，读取当日总量后原子upsert running占位；并发同任务不重复占位，同日已结束任务重试保留并累加旧input/output，跨日才清零。结算在同一日期锁下以candidate/locale/date/status/reserved完整身份CAS为completed或failed并累加可观测实际usage；任何总usage为零的响应/错误保留完整占位，CAS/commit/usage越界也保留原占位并向上报错。
- 输入/出站协议：站内HTTP路由、请求和DTO均不变。内部AI出站请求对OpenAI-compatible与Anthropic都新增显式`max_tokens`，值受模型配置、32768和context空间约束；prompt限制1MiB并以UTF-8字节+1024保守计算输入token占位。模型限制缺失或上下文不足不发请求。
- 迁移/调用方：开发期Schema metadata升级162，旧161开发库按既有受保护整代流程重建；不在线ALTER、不推断旧任务usage_date、不回填、不双读双写。第一方无迁移；既有BUG-040正式localizations消费和自动提交协议保持，真实端到端回归通过。
- 计划/部署/回滚：100k历史任务+3当日行的精确聚合为`Index Only Scan using idx_seed_crawler_translation_tasks_daily_budget`、无Seq Scan、执行0.029ms。后端与generation162同批部署；整体回滚到161会删除占位权威并恢复超支风险，不提供兼容层。验证仅使用本机隔离随机Schema/会话临时全Schema并自动drop，未访问远端或共享public。

## SEC-019：站点Logo有界静态WebP派生（generation162保持）

- Schema：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持162；本项没有数据库或查询计划变化。
- 上传协议：`POST /api/site-logo`继续先以Bearer向后端权限探测，成功仍返回201 `{url}`。请求必须为multipart；路由在`formData()`前同时执行严格Content-Length和实际流式上限（5MiB文件+64KiB封装），非multipart为415，过大或畸形body为413，缺失/多文件或不安全图片为422。
- 图片合同：输入只接受实际可解析的PNG/JPEG/WebP/GIF，并受4096边、4Mi单帧像素、128帧、16Mi总解码像素限制；输出仅为首帧、方向规范、最长边512、至多1MiB且无源元数据的WebP。哈希基于派生字节，源文件不保存。公开URL固定`/site-assets/site-logo-{20 lowercase hex}.webp`，资产路由只返回`image/webp`和immutable/nosniff。
- 调用方/兼容：品牌Provider和后端general config只接受同一精确WebP本地路径；第一方上传后保存流程与`logoUrl`字段不变。开发期删除旧png/jpeg/gif路径兼容，已有旧配置应重新上传；没有双读或回退到不安全源。新增`sharp@0.35.3`直接生产依赖，前后端应同批部署。
- 存储/回滚：本项关闭SEC-019解码与multipart资源边界，不改变当前本机`public/site-assets`部署模型；OPS-008的共享OSS、多实例一致性、只读镜像和旧对象清理继续独立OPEN。回滚会恢复原字节持久化、动画/元数据公开和预解析内存窗口，不提供兼容价值；未访问远端数据库或共享public。

## SEC-020：API访问日志查询内容最小化（generation162保持）

- Schema：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持162；继续复用`app_logs.payload jsonb`和既有批量摄取/保留索引。
- 内部日志协议：新`api_access` payload从`{query:string,queryTruncated?:boolean,bytes:number}`收紧为`{queryPresent:boolean,bytes:number}`。RawQuery的参数名和值均不进入队列、JSON batch或数据库；路径、方法、状态、延迟、IP、User-Agent和响应字节事实不变。
- HTTP/调用方：所有公开、认证和管理HTTP请求/响应零变化，OAuth callback仍读取`code/state`完成协议但访问日志只观察查询存在性。管理员日志页面仍能按路径/状态/时间等事实查询；若运维需要业务诊断，应使用显式结构化安全/业务事件，不能重新加入通用查询内容。
- 兼容/清理/回滚：开发期不保留旧payload写入兼容层，不在线猜测历史query字符串中的秘密边界；旧开发数据由整库重建或既有保留策略删除。回滚会重新把OAuth和未来Token值持久化，不能作为兼容方案；未访问、修改或清理远端/共享public数据库。

## SEC-021：空库身份bootstrap与不可登录系统主体（generation162保持）

- Schema：无DDL、列、索引、约束、trigger、generation、reset、批量回填或双写，保持162。既有`users.status text`新增应用层规范值`system`；`system_settings['security.default_users_bootstrapped.v1']={"version":1}`记录安装bootstrap已决，`security.autobot_system_subject_migration.v1`记录旧autobot兼容转换已决，两者都不是可重复覆盖账号的配置默认值。
- 启动数据协议：默认admin/autobot/guest/deleted_user仅在users、bootstrap marker和旧`permission.default_roles`初始化证据均不存在时，于同一锁事务插入并绑定`source='system_seed',source_key='default_users'`权限，然后写marker。既有用户或任一持久证据存在时不计算环境管理员密码，也不更新/重建任何用户或seed授权；全用户硬删除后marker仍阻止重建。并发启动共享同一users表锁，只有一个实例能决定pristine分支。
- 身份与认证协议：autobot的规范状态由active改为system，密码仍为sentinel；交互登录、邮件登录、Token签发和Session解析仍要求active，因此无需为system增加认证兼容路径。automationActor改为要求system，active/disabled/deleted替身失败关闭。管理员`PUT /api/v1/admin/users/{id}/status`对规范autobot收紧为system/disabled/deleted，system表示显式恢复；其他用户保持active/banned/disabled/deleted且不能请求system。
- 兼容/迁移/回滚：旧数据库即使users已空，既有`permission.default_roles`也会被识别并只补bootstrap marker；首次新代码启动只转换仍精确匹配旧username/email/active/sentinel的autobot，同时递增auth_version并撤销Session。任何人工安全变化优先且marker阻止重复转换。被硬删除的默认身份不会重建，显式安全命令或明确清除安装marker后的受控开发重建才是恢复边界。回滚会恢复启动时账号/凭据/权限覆盖，不能作为兼容方案；真实PG测试只创建并自动删除随机隔离Schema，未访问或修改共享public。

## SEC-022：公开内容指标移除个人访客历史（generation162保持）

- Schema：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持162。`content_unique_views.viewer_user_id`与`last_seen_at`仍属于内部去重/聚合输入；写路径和开发者访问排除计算继续使用，不能因对外最小化而误删仍有业务语义的数据。
- API：公开`GET /api/v1/content-metrics/{publicId}`的响应从协议中删除`recentViewers`数组，不保留空字段、alias或另一条个人历史路由。聚合`totalViews/heatScore`、公开`recentEditors/editors/developers`及引用集合不变；Handler不再执行`content_unique_views`到`users`的访客身份联表或读取精确`last_seen_at`。
- 第一方/部署：前端`ContentMetrics`类型、统计面板的第二个ActorList和中英文最近访客/空状态文案同批删除；无需运行期兼容分支。前后端应同批部署，旧前端若单独连接新后端不会因未使用字段而失败，新前端也不再期待该字段。
- 隐私/回滚：公开浏览不要求认证，且不存在个人opt-in或访问可见性设置，因此默认只能输出聚合事实。回滚会重新建立公开个人访问侧信道，不作为兼容方案；本项没有读取、修改或清理任何本机或远端数据库数据。

## SEC-024：收藏夹MRPack任务配额原子化（generation162保持）

- Schema：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持162。使用PostgreSQL既有transaction advisory lock与`favorite_modpack_export_tasks`的owner时间/状态索引，不持久化第二份quota计数。
- 事务协议：create在验证并锁定不可变preview后，按owner user取得`favorite-modpack-export-quota:{id}`的64位事务锁；锁内一次读取active、24小时daily、同collection/version/loader 30秒duplicate。成功后同一事务继续写task、全部items、preview consumed与NATS Outbox，commit才释放锁；任一错误rollback且不占额度。
- HTTP/API：成功202请求/响应及preview协议不变；active、daily、duplicate超额继续返回既有稳定429 code `MODPACK_EXPORT_CONCURRENCY_LIMIT`、`MODPACK_EXPORT_DAILY_LIMIT`、`MODPACK_EXPORT_DUPLICATE_COOLDOWN`，Retry-After分别保持60、3600、30秒。预检仍只生成15分钟快照，不创建或预占任务。
- 部署/回滚：纯后端并发语义修复，无第一方迁移或协调部署要求；所有实例连接同一PostgreSQL即可共享锁。回滚会重新允许不同preview事务读取同一旧计数，不提供开关。测试仅创建并自动drop随机隔离Schema，未访问共享public或远端数据库。

## SEC-026：Sponge调色板索引与分配边界（generation162保持）

- Schema/持久化：无DDL、表、列、索引、约束、generation、reset、回填、双写或数据库操作，保持162；修复发生在任何规范蓝图、variant、材料或OSS派生物持久化之前。
- 解码协议：Sponge v2/v3 palette最多8192项，索引必须为有符号整数且精确覆盖`0..entryCount-1`、无重复；state slice只按entryCount分配。任何负、稀疏、重复、非整数或超预算索引立即失败，BlockData引用不存在的索引也不再静默丢块。
- API/任务：上传、转换和Worker DTO/路由/正常状态码不变；畸形输入沿既有任务失败/错误可见路径处理，不写部分ready结果。合法v2顶层和v3嵌套palette以及三格式round-trip无需调用方迁移。
- 部署/回滚：纯后端breaking input validation，可独立部署；旧畸形文件若尚未处理会明确失败，不提供危险兼容开关。回滚会恢复索引决定内存容量及负索引panic，不作为兼容方案；未访问本机共享public或远端数据库。

## SEC-027：公共蓝图转换全局active容量（generation162保持）

- Schema/查询：无DDL、表、列、索引、约束、generation、reset、回填或双写，保持162。全局准入复用`blueprint_jobs`既有active operation部分索引和固定transaction advisory lock；计数子查询最多读取16条`operation='convert' and status in ('queued','processing')`索引项。百万历史行EXPLAIN命中`idx_blueprint_jobs_active_operation`，无任务表Seq Scan。
- 事务协议：所有公共`convert`生产者在任务事务中先取得`blueprint-conversion-global`数据库锁并检查全站上限16，再取得已有用户锁并检查每用户上限4，最后插入task与NATS Outbox；commit/rollback才释放两级锁。`normalize`不进入全局公共转换计数，仍走用户级准入；同蓝图/目标由既有active唯一索引兜底。
- HTTP/API：`POST /api/v1/blueprints/{publicId}/convert`的认证要求、合法格式、202任务响应和公开对象能力不变。全站active转换达到16新增429 `BLUEPRINT_GLOBAL_CONCURRENCY_LIMIT`与Retry-After 60；精确active operation唯一冲突返回409 `BLUEPRINT_CONVERSION_ALREADY_QUEUED`与30秒，其他`23505`或数据库错误保持500而不会被错误归类。
- 第一方/部署/回滚：详情页既有登录后转换入口不变，无前端协调迁移；所有实例共享同一PostgreSQL即可共享容量。回滚会恢复跨账号/跨实例读取旧计数并允许无界填充，不能作为兼容方案。真实PG验证只创建并自动drop随机隔离Schema，未访问或重置共享public/远端数据库。

## SEC-028：用户OSS额度失败关闭与原子结算（generation162保持）

- Schema权威：复用generation127引入、当前162继续保留的`oss_user_quota_usage`总桶、`oss_user_daily_quota_usage`日桶、`oss_user_upload_quota_reservations`、expiry索引、`trg_oss_files_quota_usage`及两类离线rebuild函数；本轮无DDL、generation、reset、回填、双读或双写。reserved和active四类计数非负，计数器漂移使来源事务失败而不是静默归零。
- 预留/结算：用户预签名在数据库用户锁中清理过期项、幂等替换对象预留并检查single/daily/total；普通对象预留source+stored，需服务端转换的对象先预留source并在完成时按实际stored复核。完成无条件再次取得同一锁，释放对应预留后检查当前active+其他reserved，并在同一事务INSERT active文件；trigger结算后commit才释放锁。
- 故障/并发：合法缺行表示首次使用零事实，其他额度表Query/Scan/锁/更新/trigger/commit错误失败关闭并对HTTP返回500。即使不存在预签名预留，两笔600/1000并发完成也因锁、insert和trigger同事务只允许一笔；不依赖前端遵守流程。热路径不再执行`oss_files` SUM，全历史只允许写入静止的显式离线rebuild。
- API/部署/回滚：预签名、完成、multipart abort、额度页的请求/成功DTO及业务额度403文案不变，无前端协调迁移。所有实例必须共享同一PostgreSQL；回滚到无锁SUM会同时恢复故障放开、竞态超额和历史线性工作量，不提供兼容开关。SEC028真实PG使用随机隔离Schema并自动drop，未访问或重置共享public/远端数据库。

## SEC-029：私有OSS访问只使用短期存储签名（generation162保持）

- Schema/配置：无DDL、表、列、索引、约束、generation、reset、回填或双写，保持162。`system_settings['oss.aliyun']`旧加密JSON无需迁移：读取归一化把空、未知、`esa_private_origin`和`esa-private-origin`全部解释为`oss_presigned`，下次保存写规范单值；PublicEndpoint和对象Key继续作为稳定存储定位事实。
- 访问协议：`resolveOSSObjectAccessWithConfig`删除稳定PublicEndpoint拼接分支，统一使用配置的OSS Endpoint/CNAME客户端签发GET。对象路径、bucket、可选Content-Disposition和expiry由OSS签名覆盖；签名与响应`ExpiresAt`使用同一duration。配置、管理员请求和共享解析边界都把TTL限制在1至60分钟，默认10分钟。
- HTTP/第一方：下载、图片redirect、完成上传响应及管理员presign的字段/状态码不变；URL现在含OSS signature/expiry且`downloadUrlMode`只返回`oss_presigned`。后台删除ESA选择，TTL控件限制60，双语明确稳定引用域名不授予读取；前后端应同批部署，但旧客户端提交ESA值仍被后端安全迁移而不会恢复直链。
- 部署/回滚：启用OSS本已要求Region、Endpoint、Bucket和访问凭据，这些同一凭据用于签名，无新秘密或边缘协议。ESA/CDN若位于路径上只能转发实际签名请求，不能把稳定PublicEndpoint当授权。回滚会重新使已复制私有链接永久有效，不提供兼容开关；本项无数据库访问或共享public/远端写入。

## SEC-030：GIF完整动画预预算与解码验证（generation162保持）

- Schema/存储：无DDL、表、列、索引、约束、trigger、generation、reset、回填、双读或双写，保持162；对象Key、内容类型、scan/trusted状态字段和OSS持久化布局均不变，本项没有数据库读取、写入或查询计划。
- 图片协议：通用GIF先在既有压缩字节上完整扫描容器，要求严格trailer/EOF、合法画布/颜色表/扩展/sub-block和帧边界，并在`gif.DecodeAll`前执行120帧、64,000,000累计帧像素、30秒动画时长硬界；逻辑画布继续最多16,777,216像素。预算通过后完整解码每帧并与预扫描事实交叉验证，不能以首帧通过替代完整可信结论。
- HTTP/调用方：OSS管理验证、蓝图封面、资源图片、图标镜像及贴纸上传的路由、DTO、成功响应和合法PNG/JPEG/WebP/GIF行为不变。后续帧损坏、缺失/额外尾部、超帧数/累计像素/时长GIF现在沿既有无效图片错误路径拒绝；这是breaking input validation，无客户端迁移或双路径。
- 部署/回滚：纯后端校验收紧，可独立部署；贴纸净化也复用预预算入口，但仍输出既有PNG/GIF净化派生物。回滚会恢复首帧可信判定或解码后才限预算的分配窗口，不能作为兼容方案；本轮未访问本机数据库、共享public或任何远端服务。

## SEC-031：评论子资源目标可见性闭包（generation162保持）

- Schema/查询：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持162。单项入口复用`comments(public_id)`、目标内部identity、`public_routes`和各目标既有可见性查询；插眼列表继续一次批量UNNEST/UNION解析目标，不增加逐项查询或新索引。
- 授权协议：所有`/comments/{commentId}`子路由先把published/deleted评论与当前claims可见目标求交；thread/replies/edit/delete/pin/reaction/watch和附件在目标失败时404且不读正文/树或写副作用。`/comment-watches/{watchId}`还先要求active关系属于当前用户，再验证评论目标；数据库故障与不可见分别保持500/404。
- 列表/API：`GET /users/me/comment-watches`字段和200 envelope不变，但目标不可见的关系不再返回空target加完整comment，而是从当前页items省略；nextCursor仍按原watch页事实生成。合法公开目标、目标所有者和全局moderator的DTO不变，恢复公开后关系可再次显示。直接不可见子路由的404与列表省略属于breaking authorization，无兼容空壳或客户端迁移。
- 部署/回滚：纯后端权限闭包，可独立部署；旧前端已能处理404和空页。回滚会恢复评论ID作为跨目标可见性能力及不可见目标上的reaction/watch写入，不能作为兼容方案。真实测试只使用并自动drop随机临时Schema/数据库，未重置或修改既有public及任何远端数据库。

## SEC-032：评论插眼硬额度事务串行化（generation162保持）

- Schema/索引：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持162。容量读取复用`idx_comment_watches_user_activity`的`user_id,status`前缀，并以`limit 2000`固定最多访问的active索引项；不持久化第二份quota计数。
- 事务协议：可能新建或重新激活watch的`ensureCommentWatch`按用户取得64位transaction advisory lock；同一事务内依次执行既有active幂等检查、bounded容量读取、2000拒绝、upsert、状态复读与commit。锁仅在commit/rollback释放，跨进程实例共享；任一数据库故障失败关闭。
- HTTP/调用方：直接`PUT /api/v1/comments/{commentId}/watch`的200 state、重复幂等与满额400文案不变；在回复框输入纯CY触发的私有watch命令也把同一typed限额映射400。GET/DELETE、watch列表和通知协议不变，无前端迁移或兼容双路径。
- 部署/回滚：纯后端并发语义修复，可独立部署，所有实例必须共享同一PostgreSQL。回滚会恢复无锁COUNT竞态并允许持久超额，不能作为兼容方案；真实测试只创建并自动drop正则校验的随机Schema/数据库，未修改共享public或远端数据库。

## SEC-033：评论线程定位邻域、游标与编码字节预算（generation162保持）

- Schema/索引：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持162。祖先定位只以`descendant_id`读取最多17条closure行，复用`idx_comment_closure_descendant`；直接回复按`parent_id,created_at,id`读取limit+1，复用`idx_comments_parent_created_visible`。不读取root全后代、不COUNT、不OFFSET。
- 服务预算：首面最多16祖先+1焦点+47回复，后续最多64回复；祖先深度16、邻域展开深度1。只有入页ID进入评论主体、表态、watch、权限、在线态、头像和附件批量装配。最终`apiResponse`在写响应前实际编码并限制512KiB，超限回复由末尾裁剪且cursor以最后实际返回回复生成；仍超限才裁最远祖先并置`pathTruncated=true`，焦点及推进能力保持。
- API/第一方：GET `/api/v1/comments/{commentId}/thread`原`items/focusId/target`保留，新增`nextCursor:string`和`pathTruncated:boolean`；`cursor`是与焦点numeric ID及viewer绑定的既有reply base64url v1格式，畸形/跨评论/跨用户为400。前端按ID合并页、阻止旧评论/账号响应回写、展示祖先截断提示，并让当前片段外的父引用打开其分支。
- 兼容/部署/回滚：后端和第一方前端应同批部署以获得完整直接回复遍历；旧客户端可显示有界首面但不会再收到整树。取消一次性整树是有意breaking安全收紧，不保留query开关。回滚会恢复匿名单请求随整树宽度增长的数据库、内存和带宽成本，不作为兼容方案；真实PG只使用会话临时Schema和正则命名的隔离数据库，均已自动drop，未修改共享public或远端数据库。

## SEC-034：私有皮肤与玩家档案组合授权（generation162保持）

- Schema/索引：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持162。批量装配继续按最多100个profile public ID一次读取并复用`player_profile_textures`主键/asset索引；隐私谓词只在这个有界集合内判断asset owner、review和visibility，1/100档案仍固定2条SQL。
- 读取协议：公开档案主体与引用纹理分别授权。asset owner仍可在自己的档案管理private/pending纹理；其他viewer只接收active+approved+非private资产。不满足时保留`skin:null`或`cape:null`，不输出asset public ID、名称、可见性、审核状态、hash或`/api/yggdrasil/textures/{hash}`。详情和用户档案列表复用同一批量装配器。
- 写入/审核协议：public/unlisted档案装备private资产、private档案改为public/unlisted、已被owner非private档案使用的资产发布private均返回409统一原因并整体回滚。即时编辑、待审提交预检和审核批准共用锁定兼容检查；其他用户装备/衣柜在无owner冲突时继续由既有private发布事务清除。锁序保持asset→按ID profile，失败不生成已发布修订或部分关系。
- API/第一方/兼容：所有成功请求和`PlayerProfile/SkinTexture`字段不变；读取由泄漏对象收紧为null，三类不兼容写从成功或晚期错误收紧为409。第一方已有`errorMessage/notifySite`直接显示后端明确原因，无类型或路由迁移。回滚会恢复private hash公开和可再次制造矛盾组合，不提供兼容开关；内容寻址纹理端点不承诺撤回历史已公开hash，本项只关闭当前未授权披露路径。

## SEC-036：收藏夹与收藏关系长期库存配额（generation162保持）

- Schema/配置：无DDL、表、列、索引、约束、trigger、generation、reset、回填、持久quota行或计数双写，保持162。新增`FAVORITE_MAX_COLLECTIONS_PER_USER`（默认100、硬上限1,000）和`FAVORITE_MAX_ITEMS_PER_USER`（默认10,000、硬上限100,000）；`.env.example`记录部署合同，`config.Config{}`测试实例以零值回落安全默认。
- 数据库原子性：显式集合创建与default补建在事务级`favorite-stock-quota:{user}` advisory lock内有界计数并插入。PATCH/PUT先在专用连接取得同键session advisory lock，再开启Repeatable Read目标可见性事务；session锁与xact锁互斥且持有至commit/rollback，解锁失败关闭连接。集合读取最多limit+1，关系总量最多maximum+1，目标差异受100集合请求界，复用既有user-page、collection和target-collection索引。
- HTTP协议：POST集合、GET本人集合隐式default、PATCH成员delta和PUT成员replacement四个潜在增长入口均受额度。集合达到上限返回409 `FAVORITE_COLLECTION_LIMIT`，关系达到上限返回409 `FAVORITE_ITEM_LIMIT`，`details.limit`为当前有效配置；数据库/锁故障5xx。正常201/200 envelope、收藏页cursor、summary、public列表和成功成员DTO不变。
- 兼容/部署/回滚：满额幂等关系、PATCH等量移动、PUT缩减或清空仍成功；既有超额库存不自动删除，用户可逐步减量但不能正增长。所有API实例必须共享同一PostgreSQL advisory lock域；后端可独立部署，旧第一方通用错误提示无需类型变更。回滚会恢复长期库存无界和并发穿透，不作为兼容方案；真实测试只使用随机Schema或会话临时完整Schema并自动清理，未重置共享public或远端数据库。

## SEC-001：PlantUML同站自建隐私边界（generation162保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset、回填、双读或双写，保持162；本项不访问数据库。既有`system_settings['markdown.rendering']`无需迁移，读取旧外部server时惰性改为`/plantuml`并把PlantUML关闭，下次合法保存写入规范值。
- 后端协议：默认Markdown配置由`plantUML=true`/公共URL改为`false`/`/plantuml`。GET字段形状不变；PUT仅接受空值、`/plantuml`或尾斜线等价值并统一保存`/plantuml`，外部绝对URL、协议相对URL、查询、fragment及其他路径在任何DB操作前400。损坏/历史外部值读取失败关闭而不是回落公共服务。
- 浏览器/调用方：共享前端归一化器再次验证server并在不可信时关闭；Markdown渲染器只在开关明确启用后生成同站SVG，独立PlantUML工具也只用同一`/plantuml/svg/...`路径；CSP内建图片闭集删除plantuml.com。管理端路径只读并提示部署自建前置，README记录反向代理不得转发第三方。
- 部署/兼容/回滚：前后端应同批部署；启用方必须在站点origin配置`/plantuml`到受信任自建实例的反向代理，默认无代理时Markdown不会请求该路径。旧客户端读取字段不崩溃，但依赖任意外部server属于有意移除；回滚会恢复无提示源码外送，不提供兼容开关。无空库安装或查询计划变化，相关验证记为不适用，未访问共享public或远端服务。

## SEC-002：双仓依赖漏洞扫描发布门（generation162保持）

- Schema/API：无DDL、表、列、索引、约束、trigger、generation、reset、回填、数据库访问、查询计划或HTTP/DTO变化，保持162。业务后端与第一方调用协议不变；本项只改变构建工具链、依赖解析事实和仓库自动化。
- 后端依赖：`toolchain`从Go1.26.5升1.26.6，`golang.org/x/image`从0.43.0升0.45.0，`github.com/klauspost/compress`升1.18.7；`go get/tidy`协调x/sync、x/sys、x/text等传递约束。官方govulncheck v1.7.0最终报告0个affected和0个imported-package漏洞；未调用且无修复的x/crypto/openpgp模块公告保留可见。
- 前端依赖：权威`package-lock.json`把brace-expansion分层解析为5.0.9和兼容1.1.18，js-yaml升4.3.1、nanoid升3.3.18；npm audit最终所有严重度为0。没有引入pnpm/yarn第二锁。过宽brace 5.x override已因真实ESLint回归被删除，不保留破坏旧minimatch的“安全升级”。
- CI/部署/回滚：双仓`.github/workflows/dependency-audit.yml`在push/PR/周计划/manual运行，官方扫描JSON以30天artifact先归档再按原outcome失败；第三方Action固定完整SHA。构建环境必须取得Go1.26.6和Node24.19.0；扫描器需要只读公网漏洞库/registry访问，不使用业务凭据。回滚会恢复10条已证实漏洞和无扫描发布，不支持；OPS-002/TEST-003保持独立。

## SEC-003：多副本共享限流故障关闭（generation162保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset、回填、双读、双写或PostgreSQL热计数，保持162。本项故障演练只使用进程内miniredis和两个独立缓存客户端；普通全仓测试连接本机开发数据库但未启用integration、迁移或reset，未访问远端/共享生产系统。
- 配置/启动：新增`REDIS_RATE_LIMIT_FAIL_CLOSED`，默认随`APP_REPLICA_COUNT>1`开启。多副本配置必须同时显式满足Redis enabled、required、运行期失败关闭和认证共享限流；缺任一项在`Config.Validate`失败，required Redis在运行时初始化Ping失败则副本不启动。单副本默认保持本地fallback；显式失败关闭必须同时启用Redis。
- 运行/API：正常Redis路径、额度和成功DTO不变。Redis命令错误时，多副本策略返回内部`Backend=unavailable`并以最多5秒RetryAfter拒绝，不创建每实例local额度；登录、统一反滥用、匿名高成本读取及其他现有共享限流调用方继续映射其既有429/Delay协议。管理端基础设施Redis指标新增只增字段`rateLimitFailClosed`，既有`errors/timeouts/rateLimitFallbacks`保留。
- 兼容/恢复/回滚：Redis恢复后的下一请求直接重新执行共享Lua，无需重启或同步local状态，因为失败关闭期间没有本地计数。部署前应按`docs/redis-rate-limiting.md`设置四项配置并演练中断；旧多副本配置会有意启动失败，旧客户端仍可处理既有429。回滚会恢复按副本放大额度，不支持；普通缓存与权限数据库回源行为不在本项更改范围，TEST-022/TEST-003保持独立。

## SEC-004：认证与授权版本提交后精确失效（generation162保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持162。继续使用`users.auth_version`、`users.permission_version`、`runtime_versions(rbac/project_acl)`及其既有事务触发器；Redis键仍为`auth:user-version:{publicId}`、`authz:user-version:{internalId}`和两个全局version key，TTL仍10秒。
- 缓存协议：新增内部`DeleteShared(ctx, exactKeys...) error`，安全版本发布统一为精确DEL→权威SELECT→SET。SELECT失败时已删除key不重建；DEL失败但SET成功视为已用权威值覆盖；DEL/SELECT/SET无法形成确认状态时向上返回错误。共享版本读取继续绕过本地cache，Redis缺失/故障直接查PostgreSQL。
- HTTP/指标：31个状态、封禁、角色/权限、作者/编辑关系和项目ACL生产调用方不再忽略刷新错误。正常200/201及成功DTO不变；数据库事实已经提交但缓存最终状态无法确认时返回503 code `SECURITY_VERSION_REFRESH_FAILED`、`Retry-After: 1`、details `{committed:true,operation}`。后台基础设施响应新增`securityVersions.refreshFailures`，并保留Redis error/timeout指标和结构化服务日志。
- 部署/调用方/回滚：第一方对该503必须复读目标事实，不能把它当作数据库事务回滚后盲目重放；权限写通常幂等，但封禁/审核等业务仍以复读结果决定下一步。Redis恢复后缺失key由下一请求从DB重建。后端可独立部署，前端通用错误解析兼容；回滚会恢复最多10秒的撤销窗口，不支持。
- 验证/数据库边界：SEC-004定向和既有认证授权集成只在本机PG会话临时完整Schema运行并自动清理，未迁移或重置generation155的共享public。一次补充全仓并行integration因大量临时Schema触发PG `out of shared memory`，并命中public generation155及既知sticker重复索引；这些作为开放质量环境证据记录，不替代已通过的串行相关矩阵，也不归因于本项。

## TEST-001：前端自动化测试全树入口（generation162保持）

- Schema/API/运行时：无DDL、表、列、索引、约束、trigger、generation、reset、回填、数据库访问、HTTP/DTO或生产浏览器代码变化，保持162；正式bundle内容不因测试入口改变。
- 仓库协议：`package.json`删除特殊`pretest`和47文件静态清单，唯一`test`脚本为`node --test`。当前Node24运行器自动发现仓库默认命名的110个`.test.mts`文件并执行261条用例；新增测试无需同步修改JSON清单。
- 防回归：`frontend-test-gate.test.mts`验证唯一入口、禁止恢复pretest、确认真实多文件测试集合和自检自身位于发现树；`ban-reason-load-state.test.mts`删除与全树发现冲突的“脚本必须显式列我”旧假设，业务本地化断言保持。
- 部署/兼容/回滚：构建环境继续使用已固定的Node24；不新增runner依赖、锁文件变化、浏览器或服务凭据。回滚会重新产生静态漏列并使超过一半现有用例不进入`pnpm test`，不支持。专项E2E/外部依赖门仍由独立TEST Finding推进。

## TEST-021：通知Handler与未读行为矩阵（generation162保持）

- Schema/生产协议：无生产DDL、表、列、索引、约束、trigger、generation、reset、回填、缓存key或HTTP/DTO变化，保持162。新增文件只属于Go集成测试；正式通知列表、read、read-all、unread、translate和translation-result路由均不变。
- 测试数据边界：核心矩阵以`InstallEphemeralSchema`安装generation162单连接会话临时完整Schema并显式Drop；百万行通知/水位线、AI task/Outbox、损坏结果和未读校准使用各自temporary tables。公共本机Schema保持generation155，未访问远端、未执行reset或永久fixture写入。
- 行为合同：列表只合并当前用户direct与broadcast，使用绑定user/kind/limit的opaque cursor；单条已读对跨用户目标非枚举且不写receipt，read-all以当前最大ID/时间水位线只改变当前用户；缓存派生故意漂移后由有界reconcile恢复DB真值。system翻译403稳定code，注册locale alias规范命中缓存，非法locale400。
- 异步/规模：notification translation task和Outbox在同一事务提交或回滚；完成结果必须解码合法payload/result/items且Worker业务结果写失败不能伪完成。1M通知真实计划覆盖direct/broadcast kind与无筛选keyset索引、两页两SQL、read-all一SQL；不把该单节点临时PG证据冒充跨Region缓存或JetStream集群演练。

## TEST-019：项目自动更新Worker完整生命周期（generation162保持）

- Schema/生产协议：无生产DDL、表、列、索引、约束、trigger、generation、reset、回填、provider/OSS协议或HTTP/DTO变化，保持162。新增文件只属于Go集成测试；既有DB-006测试把临时generation精确148改为至少148，只移除随正常迁移必然失效的夹具假设。
- 测试数据边界：核心矩阵以`InstallEphemeralSchema`安装generation162单连接会话临时完整Schema并显式Drop，显式建立最小autobot system身份/权限、项目、external source和设置；Modrinth provider为进程内`httptest`，不访问公网。公共本机Schema不迁移、不reset、不写fixture。
- Worker合同：重复调度只能形成一个active run；claim递增attempts并记录actor/lease，503归类可重试且释放lease，重试成功一次原子同步changelog/Minecraft版本/项目版本并产生revision、project update event、notification task和NATS Outbox；同内容后续run保持这些业务artifact幂等。永久license错误直接dead-letter且不调用provider，过期running lease由tick恢复pending。
- 组合覆盖/兼容：镜像同内容身份和上传补偿、bounded stream/hash、global slot、scan拒绝/重扫、clean提升通知及维护修订继续由既有独立真实PG测试负责；十组相关矩阵组合通过。生产调用方无需迁移，回滚测试会重新留下跨边界Worker漂移盲区而无兼容价值；不把本机单节点证据冒充外部服务或多副本灾备。

## OPS-005 / OPS-006 / DEAD-005 / TEST-020：SeedCrawler run所有权、容量、结果与端到端门禁（generation162保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持162。继续使用`seed_crawler_configs.max_concurrency`、`seed_crawler_runs.status/lease_owner/lease_expires_at/attempts/next_attempt_at/stats/last_error`及ready索引；lease owner内容从固定标签变为每attempt随机token，不需迁移历史pending/completed行。
- 领取/租约协议：schedule以配置值建立1..16条有界lane，每轮总工作量不少于旧4且有硬界；claim在共享PG短事务锁中重读配置和有效running数，达到cap不改pending/attempts。claim写随机owner和5分钟lease；每分钟按exact owner续期，丢租取消完整run context。actor、失败与completed均CAS exact owner，RowsAffected非1失败关闭；过期恢复仍由既有schedule执行。
- 结果/API：run stats新增可选`providerSucceeded/providerFailed`整数。所有分类fetch失败会保存stats/lastError并按既有30秒指数退避重试、第五次failed；部分成功继续completed但两项计数明确degraded。管理run列表原有字段与HTTP状态不变，旧客户端把未知stats键当普通JSON忽略；maxConcurrency请求/响应不变但现在实际生效。
- 测试/部署/回滚：TEST020创建正则校验名称的随机隔离数据库，以8连接安装generation162并在池关闭后精确Drop；provider只用本机httptest，并与BUG040/041/042、SEC016及100k/1M分页组合。所有实例必须共享同一PG才能共享cap/owner；部署无需前端或Schema协调。回滚会恢复固定owner、无heartbeat、无终态CAS、全provider错误completed及虚假并发配置，不提供兼容价值；不声称外部服务Exactly Once。

## OPS-011 / MAP-008 / DEAD-009 / TEST-030：搜索投影单调代次、注册表与状态机门禁（generation162保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持162。继续使用`search_index_state`记录每集合version/name、`search_index_rebuild_progress`记录building/complete/failed和页进度、`search_index_queue`保存七类增量；数据库document type CHECK闭集不变。
- 代次协议：每个Worker携带编译时projection version。rebuild在锁前和独占lease内检查数据库最高代次；drain在共享lease内、claim前检查。数据库代次更高时返回superseded并保持queue不变；state upsert带单调条件，按有序collection逐项跳过已完整的同代alias/state/collection。旧实例不得回切alias或向更高代次写文档。
- 注册/错误协议：单一`searchRegistry`登记5个collection的Typesense Schema以及7类document的归属、ID keyset SQL和loader；未知值统一error，不再默认resources。load/import/delete失败触发token CAS retry，retry和complete SQL都检查错误；批次以Join返回外部与持久化原因，任务仍依靠既有幂等upsert/delete和lease退避恢复。
- API/部署/回滚：公开搜索请求、响应、Typesense alias名称前缀、字段Schema、PostgreSQL fallback和第一方客户端零变化。滚动时先部署能读取现有alias的新二进制，再由更高projection version实例重建；旧实例发现高代次后停止投影写。回滚代码无需数据操作但旧二进制会自我禁写；若回滚目标必须恢复写入，应重新发布更高而非降低的projection version，禁止数据库降代。
- 测试隔离：TEST030创建正则校验名称的随机隔离数据库，以8连接安装generation162并在连接关闭后精确Drop；Typesense为本机内存HTTP协议实现。矩阵覆盖同版本双Worker、五集合代次、毒文档不切alias、逐集合恢复、v3/v4降级拒绝、外部503+retry SQL失败及恢复；100k/1M重建和百万服务器fallback复用既有真实PG证据。

## TEST-031：成长系统并发奖励、角色来源与失败重放门禁（generation162保持）

- Schema/生产协议：无生产DDL、表、列、索引、约束、trigger、generation、reset、回填、任务/经济HTTP DTO或生产Go变化，保持162。继续使用`user_task_progress`复合主键/`rewarded_at`、经验/货币余额与流水、来源化`user_role_bindings`及其permission-version trigger。
- 并发/幂等合同：两个事务对同一user/task/period的upsert由PostgreSQL唯一行串行化；只有成功把`rewarded_at is null`行更新并returning的事务发奖励。已完成任务后续活动可以继续累加progress，但不得再写经验、货币、等级角色或奖励流水；活动事件的exactly-once仍由上游可靠投影负责。
- 原子/缓存合同：progress、completed/rewarded、经验/level、经验流水、level_track来源、货币余额和货币流水均处于调用者事务；任一步失败全部回滚，包括触发的permission version。只有commit后`ActivityBatchCommitted`删除用户版本缓存；失败事务不得失效仍对应数据库真值的缓存。
- 测试/隔离：TEST031创建正则命名随机数据库，以8连接安装generation162并关闭连接后精确Drop；并发两事务、completed重复、同role双来源、三档level、permission version/cache和currency transaction故障重放均走生产Service。无客户端迁移或部署顺序；回滚测试会恢复组合回归盲区而不改变运行协议。

## DEAD-010 / TEST-032：资源版本批量装饰与基础设施组合门（generation162保持）

- Schema/数据：无生产DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持162。资源装饰测试只在单连接会话临时完整Schema插入mod/resource/version/detail/localization并临时重命名binding表，结束后完整Drop；公共数据库不写入。
- 装饰协议：输入中非空resource public ID先去重形成唯一ANY参数，但保留ID到全部item的映射；一条权威version行回填每个重复实例。空/未知ID始终有非nil空`versions`；Query、Scan或terminal cursor失败返回error。删除未消费的`has_manual_detail`内部SQL列，不改`hasDetail/revisionId/icon/render/name/detailUrl`成功DTO。
- 基础设施组合：gzip协商、durable metrics、死信分页/重放和Outbox状态协议不在本项改动；分别复用已关闭BUG061、ARCH015、PERF038、TEST026的生产合同和测试。本项把可压缩文本q矩阵、指标故障、150/1M死信、双类管理员重放、JetStream断连/stale lease/双dispatcher与资源装饰共同执行。
- 部署/回滚：纯后端内部修正，可独立部署，无客户端、数据库或前端协调。回滚会恢复重复输入漏装饰和无用查询列；普通唯一ID成功响应不变。故障仍由调用Handler现有错误边界处理，不新增伪空兼容分支。

## OPS-012 / MAP-009 / TEST-033：版本目录同步所有权与来源血缘（generation162保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持162。同步复用固定PostgreSQL会话advisory lock；配置仍存`system_settings` JSONB，具体loader artifact仍存既有`minecraft_loader_artifact_versions`并与配置同事务替换。
- 执行协议：`syncMinecraftVersionCatalog`在首个远端请求前以专用pool连接try-lock，持锁直到完整发布或失败退出；竞争者typed in-progress且不读写远端/配置。管理员同步冲突返回409；Scheduler启动立即尝试一次，之后仍按Asia/Shanghai每日04:00，竞争者只记录skip。锁随显式unlock、坏连接关闭或进程会话终止释放。
- API/前端：`loaderSyncs[]`保留必有`sourceUrl`并新增可选`sourceUrls:string[]`；NeoForge成功项依次包含实际modern和legacy URL，单来源loader仍只一项。前端将新数组和旧字段去重后逐源渲染；旧客户端忽略新字段，新客户端读取旧配置时仍从`sourceUrl`得到一项。成功状态与版本集合字段不变。
- 失败/兼容/回滚：远端或数据库失败不发布部分配置/artifact，既有目录继续可读；并发409允许显式稍后重试。所有副本必须共享同一主PG才能共享锁。回滚无需数据操作，旧二进制会忽略`sourceUrls`但也恢复进程锁、单URL丢失和启动延迟，因此不作为支持的运行形态。
- 测试隔离：TEST033创建`test033_versions_<digits>`随机数据库、8连接迁移generation162并在关闭pool后验证名称再force Drop；假HTTP只提供7个有界目录。共享本机public保持generation155且零写入，测试数据库/命名Schema清理为0。

## OPS-013 / DEAD-011 / TEST-034：MRPack生成物提交恢复与报告单一权威（generation163）

- Schema/数据：generation162→163。`favorite_modpack_export_tasks`删除从未读写的`report_snapshot jsonb`；新增非空`result_file_id`唯一部分索引，阻止同一OSS文件绑定多个任务；`oss_files(created_at,id)`新增限定`source='favorite_modpack_export' and status='active'`的部分索引，为超龄孤儿扫描提供稳定顺序。项目处于pre-production，历史死字段没有可靠消费者或回填语义，按整代Schema重建，不提供ALTER迁移、双读或JSONB猜测填充。
- 完成/补偿协议：OSS对象先由共享生成物边界PUT并登记active/trusted row；ready事务失败后，新补偿事务按task→file锁序读取最终`result_file_id`。已绑定当前file则视为模糊commit已成功并保留；未被任何task引用则把file设deleted、同步log share并向既有`oss_object_deletion_outbox`写`favorite_modpack_export_finalize_failed`。补偿失败与原完成错误联合上抛，active行保留为可校准事实。
- 崩溃恢复：pending扫描先查询超过`2 * leaseTTL`且无task引用的favorite active文件，稳定排序、limit50、`FOR UPDATE OF file SKIP LOCKED`，同事务墓碑并写reason `favorite_modpack_export_orphaned`。Outbox写失败会回滚文件状态，下轮继续发现；已绑定ready/cancelled文件由反连接排除，过期ready仍走既有`favorite_modpack_export_expired`事务。
- API/调用方：无公开字段、请求、成功状态或前端变化。无权限仍403；私密集合与他人任务继续失败关闭；ready下载307，过期下载410，报告历史继续保留。Worker内部ready失败保持可重试processing/lease语义，补偿对象异步物理删除由已有Outbox worker负责。
- 测试/部署/回滚：TEST034只创建正则校验名称的随机数据库，以8连接安装generation163并force精确Drop；OSS为本机httptest，四次PUT覆盖成功、即时补偿、重试和周期恢复。共享public generation155零写入。所有应用实例需共享主PG才能共同观察task/file事实；回滚到162会重新引入死列和孤儿窗口，不支持，也不能在未知远端执行reset。

## TEST-035：整合包导入资源边界与Job状态机（generation163保持）

- Schema/数据：无生产DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持163。新增集成测试创建`test035_modpack_<digits>`随机数据库，以8连接完整Migrate，关闭pool并正则复验名称后force精确Drop；共享public generation155不迁移、不写fixture。
- 归档协议：下载流继续以Content-Length和`LimitReader`双重限制1GiB。读取ZIP前只读EOCD尾部并限制单盘、最多4096项、中央目录最多8MiB且offset/size位于文件内；打开后复验entry数。目标`modrinth.index.json`/`manifest.json`继续以header和读取量双重限制32MiB；两平台声明文件最多2000项。
- Job/API：公开创建/查询路由和DTO零变化。成功导入仍写`completed/100/result/finished_at`，结果继续包含`defaultLocale=und`及只读`importSelection`；辅助请求、下载或索引边界失败写`failed/error/finished_at`且不产生result，沿用既有任务错误协议。第一方无需迁移。
- 临时存储/部署：每次下载使用`os.MkdirTemp`并defer删除；测试把系统临时根绑定到独立目录，覆盖成功、下载header早失败和下载后索引失败并逐次证明零残留。生产只新增读前校验，无Schema或配置协同；回滚会恢复中央目录预解析资源窗口，不支持。
- 测试边界：provider为本机受信TLS替身，证明Host/路径选择、下载和状态持久化；官方CDN project/version身份仍由SEC025专项解析器验证。该证据不代表真实供应商限流、证书吊销、跨Region网络或恶意ZIP全集模糊测试。

## MAP-010 / LEGACY-017 / TEST-040：评分写入与类型协议（generation163保持）

- Schema/数据：无生产DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持163。继续复用`trg_content_ratings_popularity`、`content_stats_refresh_queue(object_route_id PK)`、评分唯一约束和PERF050既有target keyset索引；TEST040测试专用函数/trigger只存在于正则命名随机数据库并随force Drop清理。
- 写入协议：Handler删除显式刷新函数调用；数据库评分trigger成为INSERT/UPDATE/DELETE唯一队列边界。常规写先UPDATE现有`route+author`，缺行才INSERT并保留唯一冲突回退；评分、维度和trigger queue仍处于同一事务，任何一步失败全部回滚。并发最终仍由唯一约束串行化。
- 类型/API：评分路径只接受九个canonical类型：`mod/modpack/plugin/addon/shader_pack/resource_pack/datapack/map/minecraft_server`。删除`server/minecraft-server/resource-pack/shader/shader-pack`别名，它们现在与其他未知类型一样在数据库查询前404。成功请求/响应、权限码、维度和PERF050 cursor envelope不变；Next `RatingTargetType`已经精确匹配，无调用方迁移或兼容双读。
- 投影/恢复：每次事实变更仍写同一queue行并把metrics/popularity置true；Worker和`refresh_content_popularity`协议不变。测试证明create/update/delete的mutation为1、并发两次更新总2但queue仍1，故障时0；显式刷新后投影rating_count为1，删除刷新后为0。
- 部署/回滚：后端可独立部署，canonical第一方不需协调；旧别名是pre-production开发残留，回滚会恢复双名称和Handler重复写，不支持。TEST040随机数据库使用8连接generation163，公共generation155不写入；本机证据不代表跨Region队列Worker灾备。

## LEGACY-018 / STYLE-006 / TEST-041：收藏成员身份、错误本地化与组合行为门（generation163保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持163。收藏关系继续以内部object route ID持久化；公开ID仅在Handler可见性解析后转换。TEST041创建正则校验名称的随机数据库，以8连接完整Migrate，关闭pool后force精确Drop；共享public generation155未迁移、未写fixture。
- 身份/API：公开收藏项、summary/PATCH/PUT、前端API和选择器统一使用`entityPublicId`。删除旧`entityKey`请求字段、fallback和内部同义命名；仅携带旧字段的PUT在事务前400且不改变既有关系。成功响应、collection ID、cursor、limit和quota code不变；这是pre-production开发协议的直接收紧，不提供双读或兼容wrapper。
- 可见性/事务：follow目标可见性SQL把公开分支与owner/moderator分支整体括入一组，再由调用方与精确route public ID相与，防止SQL `AND`/`OR`优先级让特权分支返回另一route。membership替换仍在同一Repeatable Read事务先锁定验证全部集合/目标，再删除并批量插入；任一非法输入或数据库trigger故障保持原关系。
- 前端错误/状态：下载helper只抛携带后端code/status的`ApiError`，UI把410/expired、401/403和其他错误映射到当前locale，不再在API层写中文。选择状态抽成不可变纯函数；新建集合立即加入并选中，保存拒绝时保留delta以便重试，多集合增删仍走既有有界PATCH。
- 测试/部署/回滚：真实Handler矩阵覆盖认证、默认/公私集合、隐藏/未知目标、legacy-only、混合非法集合、故障回滚、两页公开过滤和多集合写；既有SEC-035/036、BUG-089、PERF-051广域继续通过。后端与前端须同批部署以完成字段收紧；回滚会恢复双名称、硬编码中文及route错配窗口，不支持。该本机单节点证据不代表跨Region缓存或多副本灾备。

## STYLE-001 / STYLE-004 / MAP-001 / LEGACY-003 / DEAD-001 / DEAD-014 / DEAD-015：开发期残留与仓库卫生（generation163保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset、回填或数据库访问，保持163。删除三个个人`.idea`元数据文件并保留`.idea/`忽略；Go formatter只改变工作树格式，不改模块图、依赖或生成物。
- API：删除旧`GET /api/v1/notifications/unread`，唯一未读入口为`GET /api/v1/me/unread-summary`；删除无消费的`GET /api/v1/admin/nav`。`GET /api/v1/admin/dashboard`继续要求`admin.access`且DTO不变，只从无逻辑wrapper改为路由直接绑定`loadAdminDashboard`。项目处于开发期，不为旧URL保留别名或双协议。
- 内部边界：当前log-share实现不再有`jsonUnmarshal`无语义wrapper；黑名单状态formatter、卡片和列表逐层删除无用locale；OSS/WebP/APNG与角色轨道测试归到各自领域文件，权限runtime测试只保留权限解析行为。所有成功/错误业务合同不变。
- 质量门：`tools/remediation`遍历全部Go源，以规范化CRLF后的内容与`format.Source`比较；Git卫生测试拒绝仍存在的tracked `.idea`文件；httpapi源码门锁定旧route/wrapper归零并要求canonical绑定；前端源码门锁定locale死参数链归零。
- 部署/回滚：后端与前端可独立部署，但旧开发客户端必须先切到canonical未读路径；第一方早已完成。回滚会恢复旧API表面积、固定中文导航、死代码和仓库噪音，不支持。无数据库迁移、重置或环境协调。

## TEST-044：经济、商品与等级权限组合行为门（generation163保持）

- Schema/生产：无生产Go、DDL、表、列、索引、约束、trigger、generation、依赖或配置变化，保持163；新增文件仅为Go集成测试。隔离空库只补本矩阵所需permission目录项，不运行默认用户/运维种子，不改变生产SeedRBAC。
- API/权限：请求和响应零变化。真实JWT/session依次通过`economy.transfer`、`economy.checkin`、`shop.purchase`、`shop.use`、商品专属permission、`economy.balance.write`与`permission.write`；权限缺失均在副作用前403，授权后继续执行既有Handler。
- 事务事实：转账按账户稳定锁序与checked税计算提交成对流水，并发超支只能一成功；签到按用户Asia/Shanghai日期和minimumHours只奖励一次；购买原子扣款/库存/购买记录，热度消费串行分配sequence并递减power；管理员调整写理由和操作者流水。
- 等级/Session：level config只版本化并排队；Worker以有界set-based批次写level和`source=level_track/source_key=track`角色。启用后derived与同角色manual并存，清空配置只撤销derived；auth_version不变、permission_version推进，原JWT继续可用。
- 隔离/部署：TEST044创建`test044_economy_<digits>`随机数据库，以8连接完整Migrate，关闭pool并正则复验后force Drop；共享public generation155零写入。纯测试门无需部署顺序或客户端迁移；10万用户规模、深页计划、租约恢复继续由PERF-056专项负责。

## TEST-043：内容本地化、审核与AI任务组合行为门（generation163保持）

- Schema/生产：无生产Go、DDL、表、列、索引、约束、trigger、generation、依赖、配置、reset、回填或双写变化，保持163；新增文件仅为Go集成测试。既有`ai_tasks`、`nats_outbox`、`community_post_translations`和内容修订事实不变。
- API/权限：请求、响应和DTO零变化。真实JWT/session通过`requirePermission("content.translate")`验证未授权403、任务可靠接受202、terminal结果GET200及已有翻译缓存200；`user.ai.daily_token_limit.*`仍是费用边界，公开GET零任务由SEC038合同继续锁定。
- Worker/供应商：本机TLS OpenAI兼容替身首次返回503，生产`AIWorker.handleTask`必须写failed且不写translation；terminal failed消息再投递直接由claim拒绝。重新显式请求创建新task，成功响应严格解析title/body、先写业务翻译后completed；completed消息再投递和缓存请求均零新增task/外呼。
- 组合门：不复制已经存在的10万/大快照数据，而是并联Accept-Language权重、task+Outbox原子提交、孤儿/stale恢复、JetStream断连与MaxConcurrent、审核基线冲突、损坏completed拒绝、100k历史键集及1.326MiB差异预算专项。
- 隔离/部署：TEST043创建`test043_content_ai_<digits>`随机数据库，以6连接完整Migrate，关闭pool并正则复验后force Drop；共享public generation155零写入。纯测试门无需部署顺序、迁移或客户端协调；本机TLS替身和嵌入式JetStream只证明协议/状态机，不代表真实供应商SLA或跨Region灾备。

## LEGACY-004 / TEST-006：认证原语闭集（generation163保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset执行或回填，保持163。当前全部凭据生成路径只写Argon2id，仓库没有PBKDF2发布数据/导入责任；人为旧开发数据应按既有开发库重建合同处理。
- 密码协议：`VerifyPassword`与`VerifyCode`只接受`argon2id$`当前格式，PBKDF2由旧可验证改为统一失败；这是开发期breaking删除，不提供双验、别名或惰性重哈希。Argon2成功、错误密码和资源参数上限不变。
- Token协议：JWT-like三段HS256格式、`typ=JWT`、9字符公开ID、Session ID、auth version、iat/exp和60秒未来容差均不变；仅新增直接自动化门，不改Cookie、Authorization Header、HTTP状态或DTO。
- 测试/部署：后端与前端无需部署排序或迁移；使用真实旧PBKDF2向量锁定拒绝，并以同包私有签名构造Token失败矩阵。若未来引入外部旧账号，必须另建一次性可审计迁移，不能恢复运行期静默双算法。

## STYLE-002 / STYLE-005 / MAP-002 / LEGACY-005：开发期 API 与测试组织合同（generation163保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持163。`anti_abuse_bot_rules.read_only`继续是持久安全事实且新建恒true；项目自动化表仍使用数据库snake_case，不把存储命名冒充公开DTO。
- 自动化API：`GET/PUT /api/v1/projects/{type}/{id}/automation`的sources/settings，以及`GET .../runs`和管理员automation overview统一输出camelCase。运行公开ID输出为`id`，配置`interval_code`输出为`interval`；第一方组件改为显式typed DTO并删除所有snake_case桥接。成功业务语义、权限、状态和请求字段不变。
- 机器人规则API：`POST /api/v1/admin/anti-abuse/bot-rules`只接受kind/label/matcher/token；旧`readOnly`现在由严格未知字段校验返回400，不再接受后静默覆盖。canonical成功仍写`read_only=true`，GET列表和机器人写入拒绝策略不变。
- 目录参数：项目目录排序只接受canonical `sort`字段与独立`order=asc|desc`；删除`latest/oldest/created/nameAsc/nameDesc`双端别名。name缺order默认asc，其他缺order默认desc或调用方已持久偏好；评论排序是独立协议不变。
- 测试/部署/回滚：dashboard、sticker、favorite export与notification translation集成测试各归所属文件，自动发现函数名不变。自动化DTO需前后端同批部署；排序和bot请求是pre-production breaking收紧，不提供双读。回滚会恢复重复类型、伪配置和旧书签扩大面，不支持；共享public generation155未迁移、测试只使用可清理临时Schema。

## MAP-005 / MAP-006 / DEAD-004 / DEAD-007 / DEAD-008：开发期Schema字典与修订类型（generation164）

- Schema：`project_auto_update_runs.status`从五值收紧为`pending/running/completed/dead_letter`；`recipe_version_bindings.source`从四值收紧为`import/editor`；`project_update_events.revision_id`从可空text改为可空`bigint references content_revisions(id) on delete restrict`。因此权威开发Schema由163提升到164，所有当前代次测试与重置文档同步。
- 写入/查询：项目自动化Worker转换不变，管理员筛选删除不可达failed；recipe import/editor两个现有写入方不变；项目更新事件删除`::text`，BUG039读取/关联按int64。0修订仍映射NULL，无合法调用方失去能力。
- 内部注册表/热度：simple project六类型只在有序`simpleProjectTypeRegistry`登记一次，membership set与SQL values由其派生。当前热度Schema已无`direct_commenters/child_commenters`，权威`effective_commenter_count`保持；本项只新增防回归合同，不宣称新的热度DDL。
- 兼容/部署：pre-production不保留failed/backfill/split或revision文本双读；部署164须按`DEVELOPMENT_SCHEMA_RESET.md`既有安全条件重建旧开发库。共享public155和未知远端没有执行reset、在线alter或回填；回滚到163会重新扩大伪状态并失去revision引用完整性，不支持。
- 验证/清理：完整临时Schema实际返回CHECK 23514与FK 23503并接受canonical值；自动维护真实链通过。临时Schema清理器固定安装时namespace OID，现有集成测试同时检查关系/routine零遗留；这只强化测试隔离，不改变生产Schema/API。

## STYLE-007 / STYLE-008 / DEAD-013：治理公开、内部与前端领域边界（generation164保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、generation、reset、回填或双写，保持164。继续读取既有`ban_records.internal_note/moderator_id/revoked_by/revoke_reason/public_record_markdown`；本项只是让已授权管理员消费既有审计事实，公共数据库未写入。
- API/权限：`GET /api/v1/site-affairs/blackroom`及`/{id}`继续输出最小公开封禁事实，不含内部备注、治理主体或解除理由。`GET /api/v1/admin/bans`继续由`ban.view_internal`保护并新增`internalNote/moderatorId/moderatorName/revokedById/revokedByName/revokeReason`，同时返回既有公开记录正文；公共和管理员现在拥有不同DTO、SQL、游标scope及query函数，没有`internal bool`运行分支。
- 状态协议：wire仍以字符串传输temporary/permanent/released；前端公开列表/详情和管理员列表在加载边界验证，并把任何其他类型/值映射为显式`unknown`本地化展示。canonical成功值与状态码不变；未知值不再借`| string`绕过类型系统，也不会误落到三种业务语义之一。
- 前端模块：删除`admin-governance-automation-panels.tsx`；AdminConsole分别从report、ban、site-affairs和automation四个领域模块导入六个面板，共享组件/理由Hook位于第五个无业务面板模块。固定格式化后最长行均不超过130，测试上限180；全部引用和源码合同已迁移，无路由、表单payload或用户操作变化。
- 部署/回滚：后端先行时旧管理员前端会忽略新增字段；前端先行会得到undefined并隐藏可选解除信息，但内部备注/执行者显示依赖同步后端，因此建议同批发布。公共客户端无需迁移。回滚不需数据操作但会恢复假权限读取、裸string和巨型模块，不支持作为目标状态。
- 验证/隔离：真实临时generation164 Schema同时调用公共列表/详情和管理员列表；前端29项定向及272项全树、Type/Lint/Build通过。测试结束other clients、随机测试数据库、临时关系和routine均为0；未执行远端或共享public迁移。

## DEAD-003 / DEAD-012：关注偏好与表情缓存权威（generation165）

- Schema/数据：删除只为未消费API版本服务的`sticker_catalog_state` singleton表及seed，权威开发Schema从164提升到165；`project_follows.notifications_enabled`列、默认值和Worker读取保持。没有在线ALTER、回填或双写；旧开发库仅可在`DEVELOPMENT_SCHEMA_RESET.md`安全条件下整代重建，共享public155和未知远端未操作。
- 关注API/权限：新增`PATCH /api/v1/projects/{type}/{id}/follow`，请求严格为`{"notificationsEnabled": boolean}`，继续要求`project.follow`并按当前用户、项目公开ID及既有关系原子UPDATE；未关注和他人关系统一404。隐藏项目不阻断已有关系所有者关闭/恢复通知。`PUT`重复关注不重置偏好，并从数据库`RETURNING`真实值，不再硬编码true。
- 调用方：账号关注面板以受控复选框调用PATCH，只有成功响应才覆盖本地值，失败保留旧状态并显示本地化错误。通知Worker和关注GET继续读取同一列；无需Worker迁移。第一方前后端应同批部署以提供修改UI，旧客户端只是不具备修改入口，不影响既有GET/PUT成功合同。
- 表情API/缓存：公开目录响应从`{locale, version, packs}`收紧为`{locale, packs}`，TypeScript DTO、后端查询及所有管理员事务bump同步删除。唯一刷新权威是30秒`ExpiringPromiseCache`，失败Promise立即逐出，管理员成功创建/修改/删除后显式使全部locale失效；跨实例或其他已打开页面最多保留30秒陈旧窗口，不声称push、ETag或零延迟一致性。
- 兼容/回滚：项目处于pre-production，不为从未被消费的version保留双字段或空壳表；后端先部署时旧JavaScript客户端通常会忽略字段缺失，但严格类型客户端需同步更新。回滚到164会恢复无效双重权威，不支持作为目标状态；需要更强跨实例一致性时应另行设计可消费的条件请求/事件协议，而非复活无消费者计数器。
- 验证/隔离：完整generation165临时Schema证明表不存在、响应无version及关注偏好完整权限/幂等矩阵；后端全仓Test/Vet/Build/tidy与前端274项/Type/Lint/Build通过。结束时other clients、随机测试数据库、临时关系和routine均为0，公共Schema未迁移。

## REUSE-003 / MAP-003：权威目录与数值字典复用（generation165保持）

- Schema/数据：`activity_actions`和`activity_object_types`的表、列、约束、11/27项ID/code/name及upsert结果不变；`community_schema`改为从无数据库依赖的`internal/activitycatalog` definitions生成相同seed。无DDL、迁移、回填、双写、reset或generation变化，继续为165；共享public155未操作。
- 活动内部协议：`activitycatalog`唯一登记action/object常量、code和展示名，并提供definitions、IDs副本与canonical lookup；`internal/activity`现有常量成为别名。活动保留清理的允许集合、Schema seed和progression任务验证/匹配不再维护数值map/switch，已有`server → minecraft_server`公开route解析仍是独立路由适配。
- 项目文件API：`GET /api/v1/projects/{type}/{id}/files`字段、provider分页、cursor、warning和成功状态不变；筛选元数据`versions`改为读取`minecraft.versions`持久配置并复用合成表排序器。已知项按配置顺序，未知项明确置尾且字典序稳定；配置损坏/不可用时返回500而不再用点号数值近似猜测。
- 调用方/兼容：第一方版本选择器已按后台配置顺序工作，无类型或字段迁移；前端无代码变化。开发期不保留`minecraftVersionLess`别名或重复活动switch/map。外部客户端只会观察到修正后的数组顺序；未知供应商标签仍完整返回，不被丢弃。
- 查询计划/部署/回滚：活动表查询和清理谓词SQL完全不变，版本页只增加对既有小型单行`system_settings`配置的权威读取，不新增大表扫描或索引需求。后端可独立部署；回滚会恢复排序和数值关系双权威，不支持。没有开发库重建要求。
- 验证/隔离：完整generation165临时Schema逐项比对注册表，持久非数值版本顺序行为通过；活动清理和TEST031相邻真实状态机、后端全仓门及前端相邻8项通过。结束时other clients、测试数据库、临时关系和routine均为0，immutable audit未变化。

## TEST-046 / TEST-047：管理读模型测试闭环（generation165保持）

- Schema/数据：纯测试门补齐，不改生产DDL、表、列、索引、约束、trigger、seed、generation、reset或回填，保持165。真实测试使用完整session临时Schema或独立百万行临时投影；共享public155没有业务写或迁移。
- API/权限：无协议形状或状态码变更。新增源码门固定About管理为`site_affairs.about.manage`、Changelog管理为`site_affairs.changelog.manage`、未解析列表/类型为`reference.unresolved.read`；数据库读取故障继续返回500且不能携带部分`items`。
- 调用方：前端生产代码不变；测试明确要求站务locale草稿、逐翻译状态、冲突基线和cursor，以及未解析请求取消/代次、动态类型、来源路由、空页恢复和列表/类型独立错误alert。无需部署排序、客户端迁移、双读或回滚数据。
- 查询计划：站务百万行公开/管理深游标各一条SQL并命中keyset索引；未解析百万行深游标命中复合页索引，选择性prefix命中表达式索引，宽prefix一次有界probe后拒绝。没有新增生产查询或索引。
- 测试基础设施：完整临时Schema不可并行安装到当前低锁容量本机PG；一次无效并发执行的四个已结束临时namespace遗留routine在确认other clients=0后精确清理，未改数据库配置。权威验证改为串行且全部通过；最终临时关系/routine和测试库均为0。
- 部署/回滚：只有后端/前端测试文件变化，生产部署无新要求。回滚测试会重新失去权限、数据库故障和前端错误态防回归责任，不支持；开发库无需重建。

## OPS-004 / TEST-018：项目更新通知失败预算与Worker行为闭环（generation165保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、seed、generation、reset、回填或双写，保持165。继续复用`project_update_notification_tasks.attempt_count/status/next_attempt_at/last_error/next_user_id/notified_count`、ready索引及每事件/收件人部分唯一索引；共享public155不迁移、不写入业务数据。
- 内部状态机：成功批次只领取processing并推进游标/计数，不增加失败预算。失败后独立原子UPDATE把attempt增加1，按新值以平方退避5至300秒，第8次failed；只允许pending/processing转换，terminal重放幂等。retry持久化失败不再丢弃，而由scanner返回或与queue handle原错误`errors.Join`。
- API/调用方：公开项目关注、通知列表、未读摘要、任务消息JSON、响应DTO与状态码均不变；无需前端或外部客户端迁移。内部运维会看到准确attempt/status/last_error，持续坏任务最终终止；成功200人批次数量不再影响失败预算。
- 查询计划/并发：收件人仍按`project_follows(project_route_id,user_id)`有序取最多200，task行锁串行多个实例，通知唯一索引防重，`notified_count`按RowsAffected增量。PERF025既有1000行计划命中`idx_notifications_project_update_event`；本簇未增加查询或索引需求。
- 测试/隔离：TEST018创建安全正则命名的随机数据库、运行完整Migrate并允许8连接，真实验证201人双worker、locale/偏好/账户/actor/unfollow过滤、重复投递、瞬时查询故障恢复、陈旧processing接管、隐藏目标和缓存未读；cleanup先关闭pool再`DROP DATABASE ... WITH (FORCE)`。永久8次失败和retry写故障用单连接临时表验证。
- 部署/回滚：后端可独立部署且无Schema顺序要求。回滚会恢复无限重试、吞错及大项目成功批次错误消耗失败预算，不支持作为目标状态；若未来改变最大失败次数或退避，应同时更新持久任务运维合同与两组行为门。

## LEGACY-010 / TEST-025：日志策略与清理命令分离（generation165保持）

- Schema/数据：无DDL、表、列、索引、约束、trigger、seed、generation、reset、回填或双写，保持165。继续复用`system_settings['logs.retention']`、六类`app_logs`及四张专用日志表和PERF033索引；测试只使用session临时表/trigger/1.3M临时行，共享public不写业务数据。
- 配置API：GET `/api/v1/admin/logs/config`健康DTO不变；数据库或JSON损坏现在返回500 `LOG_RETENTION_CONFIG_READ_FAILED`，不再用默认策略伪200。PUT同路径继续严格保存`enabled/defaultDays/categoryDays`，但响应从`{config,deleted}`收紧为`{config}`且不执行DELETE。
- 手动清理API：新增POST `/api/v1/admin/logs/cleanup`，由`log.write`保护、无请求体；按已持久策略为每表执行最多1000行稳定ID/`SKIP LOCKED`删除。成功返回`config,deleted`；部分失败返回500 `LOG_CLEANUP_FAILED`及config/deleted/failedCategories；损坏配置在任何删除前失败。
- 自动Worker/复用：启动立即且每10分钟执行，30秒/32轮、advisory lease和释放失败销毁连接协议不变。GET、POST和Worker共用唯一严格config loader，手动/自动共用`logCleanupStatements`；活动日志保留继续是独立需预览确认和审计的数据治理流程。
- 第一方：日志面板把保存和立即清理拆为两个按钮，运行期互斥；保存只显示policySaved，手动完成显示cleanupComplete计数。中英文明确自动10分钟与一次有界手动pass；需与后端同批部署，不提供旧PUT隐式删除或空deleted兼容。
- 查询计划/验证：日志读取仍为绑定全部筛选的opaque keyset；1.3M临时行页和搜索命中既有索引，一次手动pass精确删除1000。SSE、异步访问日志、脱敏、结构化运行日志、自动租约、逐表故障和损坏配置组合通过；回滚会恢复配置保存的隐式副作用和故障伪默认，不支持。

每次变更必须记录：Finding ID、根因组、旧/新 Schema 或协议、调用方迁移、兼容边界、空库验证、查询计划、回滚/开发库重置说明。开发期不保留无价值双读双写；不对生产或未知远程数据库执行破坏操作。

## OPS-010 / TEST-028 / TEST-029：热度刷新持久终态与组合门（generation166）

- Schema：`content_stats_refresh_queue`与`comment_heat_refresh_queue`新增必填`status`，闭集为pending/processing/failed；ready索引收紧为pending部分索引，新增processing租约的`locked_at`部分索引。权威开发Schema从165提升到166，没有在线ALTER、回填或双写；旧开发库只在`DEVELOPMENT_SCHEMA_RESET.md`安全条件下整代重建，共享public155及未知远端未迁移。
- Worker协议：领取原子设processing并增加attempt；5分钟陈旧租约只在attempt小于8时接管，到上限直接failed。处理成功按status+attempt CAS删除，失败按同一CAS退避并在第8次终止；处理和retry持久化错误都返回。新评分/浏览/评论/衰减事实把failed重置为pending/0，处理中重入则保留attempt且保护新任务不被旧Worker删除。
- API/调用方：公开内容统计、评分、评论、搜索及管理目录的URL、权限、请求、响应、DTO和状态码均不变；前端无需迁移。公开内容统计继续只返回聚合事实，不含viewer身份；pageKey与搜索document type继续由服务器闭集注册表决定。队列status/attempts/last_error是内部持久运维边界，不作为公开字段。
- 查询计划：日常衰减只按`content_popularity_stats.next_decay_at`领取到期事实，启动才补缺失统计；10M历史/100到期入队8.0255ms，完整刷新计划9.7021ms，全局评分10M/10万开发者排除6.9399s。1M管理目录深页约0.51ms、筛选约0.50ms、搜索1.6096s；1M搜索keyset约623.96ms，没有因终态增加而恢复全表扫描。
- 验证/隔离：随机generation166数据库以8连接验证双worker唯一领取、两类毒函数8次终止、stale租约、重入恢复及retry故障可见；TEST040/030、SEC022、多developer/UTC与各规模门组合通过。完整临时Schema和全后端Test/Vet/Build/tidy通过，模块哈希稳定；结束时测试数据库/临时Schema按安全清理，immutable audit保持零diff。
- 部署/回滚：只部署后端generation166并按开发重置合同创建数据库；没有客户端顺序。回滚到165会恢复无限重试、吞错和无终态容量竞争，不支持作为目标状态；若未来需要主动人工重放，应以受权审计命令调用同一enqueue语义，不可直接清空attempt或删除failed行。

## OPS-002 / TEST-002 / TEST-003：持续交付与容器协议（generation166保持）

- Schema/API：无DDL、表、列、索引、数据、HTTP、DTO、权限或generation变化，保持166；共享public155未迁移。只新增仓库交付配置、合同测试及前端standalone构建输出，不引入运行依赖。
- CI协议：后端push/PR必须通过tidy稳定、普通测试、27.0%覆盖下限、Vet、Build、PostgreSQL18.6隔离集成、Linux全仓Race及镜像构建；前端必须通过`npm ci`、自动发现测试、Type、Lint、Build及镜像构建。依赖安全扫描仍在独立周期workflow，所有第三方Action固定完整SHA。
- 容器协议：后端以Go1.26.6构建CGO=0静态二进制并由distroless nonroot运行；前端以Node24.19精确lock构建Next standalone并以nextjs非root运行。标签/手动release只发布当前仓库GHCR的tag和sha双标签；前端三项公开origin为必填build arg，秘密/数据库连接只允许运行期注入。
- 验证边界：actionlint双仓零错误，后端本机实际27.5%及MinGW全仓Race通过，前端277项/Type/Lint/58页standalone Build通过。当前主机无Docker daemon，未伪称已拉取/运行镜像；CI container job是实际构建硬门，源码合同保证不可被删除或降级。
- 部署/回滚：镜像发布不自动部署集群、修改DNS或连接数据库；环境所有者仍需选择目标平台、secret和回滚tag。回滚到某一`sha-*`是可追溯应用层操作，不改变数据库；删除这些门会恢复无自动阻断和不可重复交付，不支持作为目标状态。

## OPS-003 / TEST-008：Mod导入派生对象可靠生命周期（generation167）

- Schema/数据：权威开发代次166→167。新增`catalog_import_job_artifacts`，主键为job/run/object，`oss_file_id`唯一；job与file均RESTRICT，状态闭集planned/active/abandoned，并有planned的job/run/file部分索引。已有`oss_files.pending`和删除Outbox继续是文件可见性及供应商删除权威，不新建第二删除状态机。
- 写入协议：PNG与gzip语言包在PUT前写pending文件和planned血缘；媒体关系只引用预登记ID。正常最终事务激活全部血缘/文件、完成revision/job并墓碑源文件；任何阶段失败、取消、显式重试或stale回收都按精确run token把planned文件改deleted并enqueue删除，staging revision同事务删除。补偿Outbox写失败回滚且错误可见，Maintenance对非运行/token失配planned尝试幂等重收敛。
- API/调用方：上传、创建、查询、确认、取消和重试URL/请求/响应不变，任务Envelope不变，前端无需迁移。唯一可观察语义修正是半完成派生文件不再active，取消成功保证补偿事实已经提交。测试夹具使用本机替代OSS，不改变生产provider配置或凭据边界。
- 查询/容量：普通读仍按`catalog_import_media.oss_file_id → oss_files active`，无新读路径。恢复只读取planned部分索引并按最小file ID锁一个尝试；实际对象删除由现有有限租约/重试/dead-letter Worker执行。一次导入补偿按本次attempt对象数工作，不扫描历史active血缘。
- 收尾互斥/目标：每个PUT在30秒截止时间内持artifact共享锁，补偿持排他锁等待其返回；最终化/补偿统一job→artifact→file顺序，孤儿恢复选择时锁job。派生文件endpoint保存实际写入端点，不保存仅用于显示的CDN端点；公开URL仍由现有OSS访问解析器生成。取消/迟到PUT和实际DELETE均有真实PG+本机OSS回归。
- 部署/回滚：pre-production仅允许空库安装generation167；共享public155和未知远端不执行ALTER/reset。后端单独部署且无前端顺序；回滚到166会重新打开先上传后登记和失败泄漏窗口，不支持。若需清理旧代次已经不可枚举的供应商孤儿，必须另以供应商库存和数据库快照制定受权离线作业，不能由本迁移猜测删除。

## OPS-020：维护任务公平预算（generation167 保持）

- 无 DDL、表、索引、约束、种子、公开 API、DTO 或前端变化；不执行共享/远端数据库 reset 或升级。
- 维护 tick 仍为 10 分钟，类别超时仍为 30 秒，但不再共享截止时间；封禁到期先执行。每类每轮最多 4×1000 行，孤儿导入最多补偿 4 个尝试；后续类别不受前置超时/无限积压影响，剩余积压下轮继续。最坏整个维护轮有 11 个独立时间片，不超过 330 秒且不另启并行 goroutine。
- 所有子 Context 继承关闭取消；角色撤销仍精确限定 `governance_ban` 来源及 ban ID，保留其他来源、未来授权和会话；文件清理继续使用原有事务和删除 Outbox，没有第二生命周期路径。
- 后端单独部署即可生效；回滚维护调度会重新引入饿死问题，无数据库回滚步骤。本轮测试仅代表本机随机 PostgreSQL、竞争 Worker 和本机故障替身，不代表生产多节点容量演练。

## OPS-007：运行态订阅恢复和健康状态（generation167保持）

- 无Schema/索引/种子/权限/可靠任务协议变化；PG临时表Outbox和dead-letter组合回归全部通过，共享public155未升级/重置。
- queue Status增加`realtime`、`realtimeReady`、`recovering`；GET `/ready` 的dependencies增加`realtimeLocal`、`realtimeBroadcast`（disabled/ready/degraded）；管理infrastructure metrics的realtime增加`localReady`、`broadcastEnabled`、`broadcastReady`、`broadcastRecovering`。均为新增只读字段，旧字段/包装不变，数据库未就绪仍503，跨实例广播未就绪仅降级。
- 注册定义与已成功的live注册分别保存，成功prepare/persist后一次交换；首次失败、终态连接和注册失败经同一权威候选路径恢复。候选SUB拒绝不写设置/不替换旧连接；已连接后的正常reconnect/replay仍由官方库负责。
- Close不再允许复活或启动本地新任务；父取消停止恢复。广播仍可丢，未添加伪持久重放或改变事件ID/origin协议；双实例恢复/重启后仍一次投递、无本机回声。
- 后端独立部署即可生效，前端业务无需迁移；回滚会恢复初始离线不补订及错误就绪风险，无DB回滚。客户端所有者需遵循New→使用→Close终态合同，当前调用方无关闭后复用入口。
