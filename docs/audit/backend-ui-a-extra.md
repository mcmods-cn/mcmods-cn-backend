# 后端追加五文件审查

基线 bd50afd0ef92192c40c5152ea56e27a3fdbb657f，DB 代理明确移交四个未读文件2,093行，API-B随后移交catalog_editor_handlers.go 1,717行，合计5文件3,810基线行。全部逐段完整人工阅读，完整5、部分0、未读0、排除0；最终指纹、阅读行段、职责及调用链见 review-backend-ui-a-extra.json。跨文件只读 helper 不计入这5文件覆盖，也不机械继承其他代理的结论。

| ID | 状态 | 级别/根因 | 修复/证据 |
|---|---|---|---|
| BE-UIA-X001 | 已修复；已验证通过（限本行断言） | P2 entry carrier 使用 entityId，而装饰器只查 publicId，版本永远空 | 正确键名；真实 PG 具有1个可用版本的 HTTP响应从空到存在 |
| BE-UIA-X002 | 已修复；已验证通过（限本行断言） | P2 缺失 locale 先被强制 zh-CN，覆盖真实请求偏好 | 无显式参数时沿 requestContentLocales；知识页按主/次语言回退；en-US请求及显式中文真实HTTP验证 |
| BE-UIA-X003 | 已修复；已验证通过（限本行断言） | P2 knowledge_pages 真正查询失败被当空页面成功 | 非 ErrNoRows 返回通用500；ownedDB暂时重命名表故障注入后恢复，基线200→500 |
| BE-UIA-X004 | 已修复；已验证通过（限本行断言） | P1 待审父资源统计、view写入、导入者归因公开 | 来源审核边界与归档校验；真实 PG 拒统计/拒浏览且计数不变；人工无owner及已审核绑定资源不误隐藏 |
| BE-UIA-X005 | 已修复；已验证通过（限本行断言） | P1 同对象配方跨 active revision 搜索未过滤其他父模组审核/版本状态 | 已授权当前revision保留；其他来源要求approved/active/ready-partial；真实PG pending隐藏、approved可用、archived隐藏 |
| BE-UIA-X006 | 已修复；已验证通过（限本行断言） | P1 公开tag/type/template/recipe只查实体active，附带待审导入来源、候选、计数和sourceVersion | 同一approved/active/ready-partial公开条件作用于详情、列表/计数、资源归属和导入元数据；纯人工条目不误隐藏，global管理原权限保留 |
| BE-UIA-X007 | 已修复；已验证通过（限本行断言） | P1 公开投影译文不查approval；tag无合法译文时lateral仍回退pending名称 | 公开detail/search/names/member/catalyst仅approved，授权管理保留全量；真实pending译文和标签fallback拒发布 |
| BE-UIA-X008 | 已修复；已验证通过（限本行断言） | P1 binding外层cursor未关就再借连接，小池阻塞；行流/多次lookup吞DB故障 | cursor消费关闭后读取候选；config前置；Rows.Err与实际不存在/真实故障分别500/404；MaxConns=1及7真实PG故障表断言 |
| BE-UIA-X009 | 已修复；已验证通过（限本行断言） | P2 change_requests只匹配entity_id，mod与catalog数字ID碰撞显示他人审核状态 | 关联entity_type+entity_id；真实mod rejected记录不改变同ID公开recipe_type approved |

catalog_editor_handlers基线分段1–240、241–495、496–745、746–1000、1001–1240、1241–1480、1481–1717全部已读。最终全部diff/上下文补读，未变行与基线比对；不以工具扫描批量假报阅读。

shared resource_versions 的 approved/ready-partial 过滤由 API-B 独占修复，公开 revision 入口由 DB 代理独占修复；本域交叉核对 caller，并未并改或把其测试计为自己的独立验证。

## 验证

Go1.26.8；真实隔离 PostgreSQL 由根创建的回环测试服务供给，集成 helper 再创建随机 marker-owned 新数据库，迁移/所有权检查后清理。不打印连接串，不触碰生产/真实用户，不请求对象存储或 AI。

有效8场景基线6FAIL/2PASS，/tmp/mcmods-export-entry-audit-red-valid.log。第一次 fixture 漏实例化 querycache 产生nil panic，已修测试setup，未将其算业务红绿。追加绑定 fixture 最初误用重复INSERT，而实际PK只允许一个绑定，改为UPDATE既有绑定保留原可见性断言；该setup失败单独记录，不算业务红绿。shared seed helper / applications test 在其他代理编辑窗口曾编译失败，保留失败日志；修复后重跑，未隐藏为环境故障。

最终导出/统计13个PG子场景 PASS；同次 simple_project18 PG子场景及slug2子场景 PASS，`go test -race ./internal/httpapi -run '^(TestModExportEntryAndMetricVisibilityAuditIntegration|TestSimpleProjectAssociationVisibilityAuditIntegration|TestSimpleProjectSlugStopsOnDatabaseFailure)$' -count=1 -v`，退出0，证据 /tmp/mcmods-ui-a-backend-final.log。

指定 mod_export_handlers_test、minecraft_version_handlers_test 及新增 minecraft_loader_source_audit_test 三文件的40个顶层测试以实际 Test 名组成-run表达式执行 race，PASS退出0，证据 /tmp/mcmods-backend-ui-a-extra-unit-final.log。新增来源测试使用真实本地 httptest HTTP：503/无效JSON/空响应 fallback、全失败、16MiB边界、超时。未开启 MCMODS_LIVE_VERSION_SOURCES，未验证真实外网loader供应商；这不能证明生产网络连通。

`go vet ./internal/httpapi`、`git diff --check`最终结果见 backend-ui-a-extra-validation.json。测试覆盖率百分比未测；文件读取覆盖不代表全业务测试覆盖。无 schema 迁移；正式契约和部署说明见 ../../CONTENT_EXPORT_VISIBILITY.md。

## 公开目录追加回归

实际路由核对：catalogResources/detail属于global_resource.list/view鉴权管理域；tag/type/template/recipe公共GET是optionalAuth。最初风险猜测已按真实路由纠正，未改管理权限。DB代理确认content_localizations为发布projection，pending-review AI只存immutable revision；因此公开approved筛选符合现有模型，而非新权限政策。

Go overlay仅替换编译器看到的单个基线handlers，保留共享工作区：20个有效基线13FAIL/7PASS（/tmp/mcmods-catalog-editor-baseline-red-final.log）→20PASS。新增QueryRow与entity_type碰撞5FAIL/20PASS（/tmp/mcmods-catalog-editor-additional-red.log）→25PASS；最终补读真实发现tag pending名称/成员count的2FAIL（/tmp/mcmods-catalog-editor-tag-red.log）及candidate pending owner 1FAIL（/tmp/mcmods-catalog-editor-candidate-red.log）→最终28PASS。第一次localization fault fixture没有approved行而被where正常过滤，补真实approved测试行让VOLATILE故障确实执行，不放宽断言；这是fixture修正，不虚构原始红绿。

最终 `go test -race ./internal/httpapi -run '^(TestCatalogEditorPublicVisibilityAndReadFailuresAuditIntegration|TestModExportEntryAndMetricVisibilityAuditIntegration|TestSimpleProjectAssociationVisibilityAuditIntegration|TestSimpleProjectSlugStopsOnDatabaseFailure)$' -count=1 -v`，真实PG目录28、导出13、simple18，另slug2单测，全PASS退出0，13.729s，/tmp/mcmods-ui-a-backend-catalog-final.log。单连接断言包括请求context没有过期，不把超时后吞错200当通过。后台管理/前端UI及生产性能另需根整体验证；原候选N+1仍存在，未用小fixture宣称全面性能治理。无本域schema迁移/生产操作，部署说明同步CONTENT_EXPORT_VISIBILITY.md。
