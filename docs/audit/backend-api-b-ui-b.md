# UI-B 追加后端审查与修复

基线 bd50afd0ef92192c40c5152ea56e27a3fdbb657f；委派范围为 internal/httpapi 的25个测试、17个 handler，合计42文件逐段全文实际读取与语义审查。台账 review-backend-api-b-ui-b.json 记录每文件指纹、行段、职责和调用链；追加紧急日志/GIF文件和新增测试/正式文档单列，不重复扩大 API-B 原138文件分母。读文件不代表其全部业务已执行；既有25测试的行为验证不继承总任务基线。

| ID | 级别 | 已确认根因、影响与处理 | 验证 |
| --- | --- | --- | --- |
| BE-UIB-001 | P1 | 全局访问日志将任意查询字符串持久化，OAuth code/state等敏感参数可泄漏。非空查询写固定脱敏标记；业务参数和响应保持原样，yggdrasil仍省略 | 专属PG实际 logAccess→app_logs 五类查询，旧源实际3失败，新版通过 |
| BE-APIB-016 | P1 | GIF全帧像素先分配再检查帧数/累计面积，低于4MiB压缩源可放大内存。结构预解析先累计帧/面积/延迟并检查全部有界块，之后仍标准解码复核 | 真实合成GIF，超限旧行为提取版约1669193 bytes/op，新版16 bytes/op；原功能/截断/损坏通过，race通过。非整个进程RSS证明 |
| BE-UIB-002 | P2 | 表情目录迭代/版本查询故障误报成功。检查Err并返回500 | 专属PG healthy200/仅专属表删除500，旧源200失败，新版通过 |
| BE-UIB-003 | P1 | 旧6集成测试仅opt-in可直接连配置数据库，部分迁移/写入未事务隔离。交Root009统一owned-directory身份/marker/精确集群/覆盖URL检查 | 根任务修复七个package TestMain（activity/antiabuse/database/httpapi/progression/queue/searchindex），旧fixture不重复修改；guard拒绝是真实验证记录 |
| BE-UIB-004 | P1 | 草稿所有查询错误当作无草稿200空稿，前端可自动覆盖原草稿。仅ErrNoRows回空，其他500 | 专属PG空稿/原稿/表不可用500/恢复原稿，旧源200失败，新版通过 |
| BE-UIB-005 | P2 | 私信已读更新忽略错误，列表可返回不完整成功；消息写后再查接收者可能已落库却500误导重试。检查读错误，提前一次加载接收者 | PG真实trigger失败500且保持未读，非成员403；旧源200失败，新版通过。首次红灯fixture参数类型错误单列非业务 |
| BE-UIB-006 | P1 | 日志分享正则不识别JSON引号键、完整含空格引号值、OAuth/签名URL，公开分享可能泄漏。规则版本2；旧版本在展示/下载应用新规则不重写数据 | 8类确定性脱敏与专属PG创建/公开读取/合成v1追加保护和存储不变，旧版公开泄漏失败，新版通过；未真实OSS/生产调用 |
| BE-UIB-007 | P2 | 皮肤审核revisionId返回内部数值，与其余审核公开字符串契约不一致。返回revisionPublicID | 原源实际公开ID断言失败；新回归验证状态持久化与重复409。首次新测试二次复用已消费body400属于fixture，修正请求体保留断言 |
| BE-UIB-008 | P2 | 文件额度吞错显示0、多个关联/streak/作者/风控查询遗漏rows.Err或Scan、清理完成记录吞错和近期检查失败继续自动清理 | 额度专属PG原源200实际失败，新版500；modpack关联真实PG异常view回归；其他错误分支静态复查+总回归，不宣称每类故障注入全部覆盖 |

## 模块与角色/任务边界

| 模块 | 角色/任务 | 主要审查与验收边界 |
| --- | --- | --- |
| 草稿/私信/个人文件 | 当前登录用户保存、恢复、查看、发送 | 所有者/参与者检查、失败不能伪造成功；直接handler注入合成claims，不代替真实登录中间件/E2E |
| 内容审核/署名/皮肤 | 全局/项目审核者，提交者，公开访客 | pending状态锁、baseRevision校验、相应权限、公开ID；多worker与线上权限运行数据由主任务证据证明 |
| modpack/server目录 | 访客查已批准内容，获权编辑者提交关联/图片 | public visibility、approved mod resolution、信任OSS文件、事务；静态发现的关联查询遗漏已修，未作大样本生产性能结论 |
| 活动/统计/反滥用 | 管理员按规则处置；本人看私密统计 | 敏感hash查看权限、分批清理确认/身份、近期执行失败停止；不可逆已发生删除不能靠错误状态回滚 |
| 日志分享 | 用户分享，访客查看，文件拥有者下载 | 发布前脱敏、历史追加保护、到期/归属/删除、no-store；未知敏感格式、已下载历史副本仍有风险 |
| Yggdrasil/悬赏/自动化 | 当前角色/获权用户/worker | UUID/角色/材质来源、金额结算事务、维护任务状态和失败；不把静态审查作为完整运行验收 |

## 数据、兼容性与剩余证据

本子范围无需schema迁移或修改生产数据。版本2采用现存 source_file_id/redaction_version 活跃唯一索引；存储规则版本/统计保持原值，新增appliedRedactionVersion用于响应说明。皮肤revisionId数值纠正为既有公开字符串契约，调用方不应依赖内部ID。部署后端后即可生效，前端草稿保护一起部署改善恢复体验。GIF配置默认上限延续，无禁用GIF或放宽安全检查。

没有获取生产容量、膨胀、锁等待、备份恢复、费用或真实供应商数据。日志脱敏为已知格式规则，不能承诺所有敏感信息已清除；不扫描/删除历史生产日志。活动清理失败后仍可能已有部分删除，需要保留审计结果并核对；不新增破坏性数据修复。

## 环境、真实命令与证据

Go1.26.8，专属PostgreSQL17.11本地回环，source任务test-services/env.sh（不输出密码）；新TestMain需其owned STATE变量。定向用go test ./internal/httpapi，GIF另运行-race，go vet ./internal/httpapi。/tmp/backend-ui-b-{log,gif,catalog,playground,message,log-share,skin-quota}-red*.log 记录实际旧行为；GIF为行为保持提取辅助函数的对照，未伪称整个旧源直接有新helper。绿色结果与退出码以backend-ui-b-validation.json最终记录为准。

原测试mod_export_contract需要真实v2/v6外部ZIP fixtures；缺fixture部分未运行。六旧集成环境风险由Root009处理，不重复计问题修复。review_attachment_security仅检查SQL字符串，不证明实际越权拒绝；advancement pending分支手工将active种子写入，不等于审核旅程。初次绿色皮肤唯一失败为二次请求body fixture，修正后重跑。并行共享query helper和seed worker在修改中引起的临时build-fail完整保留，不标通过或笼统归咎环境。

正式规则见 docs/access-log-privacy.md、docs/sticker-image-safety.md、docs/log-share-redaction.md、docs/user-draft-message-reliability.md。分支、最终commit、PR、CI及全库结果由根任务关联；本报告不声明全项目已完成或生产健康。

最终定向回归已验证通过：13 顶层测试、34 含子测试事件，0 失败/0 跳过（15.908s）；同范围 `-race` 34 事件通过（18.985s）；`go vet ./internal/httpapi`、`git diff --check` 通过。modpack真实PG延迟异常 view 旧基线实际业务断言失败，新版通过。42 原委派文件基线12627行，新增/紧急源码、测试、文档和验证记录18文件单列；台账60条，不扩大原API-B分母。证据路径/历史失败和边界见 backend-ui-b-validation.json。
