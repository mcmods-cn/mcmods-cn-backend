# 2026-10-03 安全、依赖与许可复核

后端基线 `af52f6e`，前端基线 `4a611de`。本次复核当前锁定依赖、默认构建与测试
导入集合、人工维护文件扫描快照和上游许可来源。旧
[2026-10-02 报告](../20261002/SECURITY_REVIEW.md)保留其执行时点；本报告不覆盖
生产服务、不可访问历史、未知漏洞或最终分发物的全部许可义务。

## 依赖检查的实际结果

复用已安装的 Go 1.26.6、Node 24.19、govulncheck 1.7.0 和 Gitleaks 8.30.1。
只向官方漏洞数据库和公开 npm/GitHub/GNU 来源读取公开资料，没有上传项目源码
或数据；没有升级依赖、降级 Next.js，也没有真实付费 AI 请求。

| 命令 | 退出码与实际结果 | 解释 |
| --- | --- | --- |
| `go mod verify` | 0；all modules verified | 当前缓存对应锁定模块。 |
| `go mod tidy -diff` | 0；无差异 | 没有为核查修改 go.mod/go.sum。 |
| `govulncheck -json ./...` | 0；4 个仅模块 trace 的 finding | 当前默认构建和测试均未导入受影响的 SSH/OpenPGP 包。 |
| `npm audit --json` | 1；high=5，其余=0 | 一条上游公告经开发依赖链影响五个包。 |
| `npm audit --omit=dev --json` | 0；所有严重程度均为 0 | 与上行完整扫描分别保留，不能用此结果抹掉开发依赖风险。 |

`go list -deps ./...` 与 `go list -test -deps ./...` 分别得到 348/444 个实际导入包，
均无 `golang.org/x/crypto/ssh`、`openpgp` 及其子包。x/crypto v0.53.0 中的公告仍为
[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932)、
[GO-2026-6303](https://pkg.go.dev/vuln/GO-2026-6303)、
[GO-2026-6354](https://pkg.go.dev/vuln/GO-2026-6354)、
[GO-2026-6355](https://pkg.go.dev/vuln/GO-2026-6355)。它们不构成当前项目调用可达证明；
其他平台、构建标签或新增导入需要重新核验。官方数据库本次更新时间为
2026-10-01T20:24:15Z。

### OCT03-A-002：braces 尚无兼容修复版本

[GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm)
（CVE-2026-93687）描述 braces 深层嵌套 AST 的递归栈耗尽。公告更新时间为
2026-10-02T22:36:34Z；这解释了前次扫描与本次结果的时间差。当前链为
braces 3.0.3 → micromatch 4.0.8 → fast-glob 3.3.1 →
@next/eslint-plugin-next 16.3.8 → eslint-config-next 16.3.8，五者的锁条目均为开发依赖。

实际查询官方 [npm 注册表](https://registry.npmjs.org/braces)的全部 37 个版本，
最高版本和 latest 均为 3.0.3；公告没有 first_patched_version。
因此没有添加一个不存在的 3.0.4 override。npm 提议的 Next ESLint 14.2.35
属于跨主版本降级，未用它消除计数。

受控本地复现使用已安装 braces、默认栈与默认长度限制：
3,500 层/7,001 字符的 compile 和 expand 均未复现；
4,500 层/9,001 字符的两项均实际抛出 RangeError。
此前 20,001 字符尝试触发的是既有长度限制 SyntaxError，保留该失败，不算栈耗尽证明。
这些输入只在本地进程执行，没有攻击真实服务。

已完整读取 Next ESLint 的 get-root-dirs 实现与项目 eslint.config.mjs：glob 输入来自
仓库配置 `settings.next.rootDir`；当前配置未设置该项，使用 cwd。
前端 app/lib/浏览器代码未发现这条依赖的用户输入路径。生产依赖审计也未报告该公告。
结论是“上游问题未修复，当前未确认生产可达”，不是“生产永不可达”。
最小解除条件是官方发布兼容当前 major 的修复版本，再更新 override/lockfile 并执行
安装、lint、构建和回归；本次 package.json/package-lock.json 保持不变。

## 密钥扫描：退出码与人工分类分别保存

Gitleaks 使用默认规则、`--redact=100`、关闭行内 allow 注释，并显式避免已有 ignore
文件掩盖命中。仅复制 Git 跟踪文件与非 ignored 的新增人工文件到扫描目录；不扫描
任务环境文件、数据库、临时服务、node_modules、构建产物或外部符号链接。

扫描时后端快照 1,382 个文件：退出码 1、12 个命中；前端快照 662 个文件：退出码 0、
0 命中。这是工具执行时点的快照，后续新增工具和文档另做差异扫描。
追加工具/正式文档后的复扫为后端 1,386 个维护文件、同样 12 项/退出码 1；
前端 663 个维护文件为 0 项/退出码 0。新增文件没有产生新的确认凭据。
12 项均逐个核实所在符号与使用上下文，报告不复制命中正文或凭据值：

| 相对路径 | 人工分类 |
| --- | --- |
| internal/config/oct03_presence_secret_test.go | 合成反滥用 HMAC 配置校验。 |
| internal/httpapi/bug038_mirror_scan_state_integration_test.go | 隔离 OSS 扫描夹具的合成 access key。 |
| internal/httpapi/blueprint_read_error_observability_integration_test.go | SQL 测试公开 ID、源 hash 与元数据，非凭据。 |
| internal/httpapi/bug082_comment_log_attachment_job_integration_test.go | 本地 httptest OSS 端点的合成配置。 |
| internal/httpapi/bug040_seed_crawler_translation_integration_test.go | 合成模型选择标识，非 API 凭据。 |
| internal/httpapi/bug092_community_translation_queue_state_integration_test.go | 队列与隔离供应商夹具的合成 key。 |
| internal/httpapi/bug113_modpack_report_integration_test.go | 举报处理测试说明文本，非凭据。 |
| internal/httpapi/oct02_ai_translation_integration_test.go | 本地 AI 夹具的确定性设置加密键。 |
| internal/httpapi/oct03_presence_http_integration_test.go | 本地 presence HTTP 夹具的合成 HMAC。 |
| internal/httpapi/test048_governance_http_state_machine_integration_test.go | 合成处罚 JSON、用户公开 ID 与时间，非凭据。 |
| internal/httpapi/test038_comment_http_lifecycle_integration_test.go | 本地评论请求的幂等键，非外部凭据。 |
| internal/security/settings_test.go | 设置加解密单元测试的确定性密钥。 |

没有确认真实服务凭据，也没有使用或轮换真实凭据。不能因此声称全部历史或未扫描
内容不存在泄露；本次没有重复历史全扫描。原始结果与版本、指纹、退出码保存在私有
任务证据，不提交原始命中正文。

## 许可来源复核与剩余最小条件

当前 npm lockfile 584 条均有 license 字段；513 个已安装条目的版本、声明与锁一致，
0 差异。其余 71 个其他平台可选包未在本机安装，不冒充读取其分发物。
Go 精确选中图 50 个外部模块全部核对，按官方缓存补取 18 个未展开目录的锁定版本；
49 个模块的 66 份许可文本指纹与前次实际全文审查一致，无漂移。

对前次 23 个 npm 包内缺独立许可文件的条目，实际读取官方精确版本元数据并核对
dist.integrity，有限尝试相应 gitHead 或版本标签许可。取得 12 个条目的仓库许可文本，
共 459 行并完整阅读；另在 esrecurse 4.3.0 的已安装 README 中读取完整 BSD-2-Clause。
这些新增来源有具体边界：四个 Next 包只有候选精确版本标签、注册表未提供 gitHead；
sharp-libvips 仓库根 Apache 许可仅涵盖构建包装，不能替代其二进制的 LGPL 声明。
MIT/BSD 的版权与免责声明、Apache 的通知/改动/专利条款均需按分发内容保留。

| 剩余范围 | 实际尝试与结论 | 最小解除条件 |
| --- | --- | --- |
| github.com/chzyer/logex v1.1.10 | 官方 LICENSE、LICENSE.md 与 license API 均 404；精确版本完整树 7 个文件无许可，README 也没有授权条款；未进入当前默认/测试导入集。 | 取得该精确版本上游授权证据，不能假定 MIT。 |
| 10 个仍缺对应完整文本的 npm 条目 | @humanfs/types、@tybys/wasm-util、@types/json5、client-only、keyv、language-subtag-registry、language-tags、natural-compare、nbt-ts、stable-hash。官方/cache 来源只有声明、链接或未找到完整文本。 | 补对应版本及版权主体的许可/通知，不用统一模板替作者授权。 |
| sharp-libvips LGPL 与嵌入库 | 安装包 README 列出第三方库和 LGPL-3.0-or-later；GNU 文本读取遇 503，改读可信系统缓存 LGPL-3/GPL-3 全文 165/674 行。缓存标准条款不等于精确二进制版权通知。 | 核对发布二进制实际组件与通知、适用源码/重链接方式。 |
| 两项目自身与 40 个 mc-icons SVG | 根目录未找到 LICENSE/COPYING/NOTICE；图标来源授权未取得。 | 维护者确认项目许可与图标作者、来源、授权，不能擅自赋予许可或删资源。 |
| standalone/container 最终发布物 | 本次没有验收最终通知装配。 | 检查实际产物中的 notice、署名和适用源码提供机制。 |

缺少独立许可文本不自动证明侵权。声明存在也不证明分发义务已履行。本次未改变项目
许可、资源或发布方式。上游公开资料/可信缓存的查询已实际执行，剩余范围不以
“环境默认缺少工具”代替核查。
