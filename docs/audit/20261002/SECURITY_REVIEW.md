# 安全、依赖与许可核验：2026-10-02

后端基线 `95694ccff038a28a3e6126b05e3e7c5037bc7854`，前端基线
`262e1c0136bfed4624a011cf5f445b1d71cf93c0`。本报告记录本次扫描和本地核验的范围，
安全修复的根因、代码位置和实际回归见同目录 `ISSUES.md` / `findings.json`。
没有通过本次审查证明零漏洞、零凭据泄露或生产安全。

下列`evidence/`名称引用私有任务中实际保存的原始结果；不把原始日志或扫描命中正文
提交到仓库。可交付脱敏计数、公告/许可摘要和原始证据SHA256保存于同目录
`verification-results.json`，复现命令由`VERIFICATION.md`和正式隔离环境说明提供。

## 依赖漏洞：扫描结果与可达性分开

| 检查 | 实际证据与结果 | 结论边界 |
| --- | --- | --- |
| 前端依赖 | `evidence/npm-audit-final-r2.json`：当前 lockfile 对应 584 个依赖条目，公告结果 info/low/moderate/high/critical 均为 0。 | 已执行的官方依赖数据库查询未报告漏洞；不覆盖未知漏洞、项目授权逻辑或第三方服务配置。 |
| 后端依赖 | `evidence/govulncheck-final-r2.json`：govulncheck v1.7.0，Go 1.26.6，source/symbol 扫描；4 个 finding 都只有 `golang.org/x/crypto v0.53.0` 模块 trace。 | 模块包含公告涉及的包，不代表项目调用了受影响包或符号。 |
| 后端实际导入 | 本次再次执行 `go list -deps ./...` 和 `go list -test -deps ./...`；两组结果均无 `golang.org/x/crypto/ssh` 或 `openpgp` 及其子包。 | 当前默认构建及测试依赖没有公告涉及的包；新增导入、不同构建标签或平台需要重新核验。 |

四个 Go finding 的具体范围：

| 公告 | 涉及的包及条件 | 当前处理 |
| --- | --- | --- |
| GO-2026-5932 | `openpgp` 已停止维护且设计存在已知安全问题；公告没有修复版本。 | 当前未导入。不能以升级整个模块代替避免新引入该包。 |
| GO-2026-6303 | `ssh` 在非公钥认证回调中没有执行 source-address critical option；修复版本 v0.55.0。 | 当前未导入，没有项目调用 trace。 |
| GO-2026-6354 | `ssh` undecided channel 死锁导致拒绝服务；修复版本 v0.56.0。 | 当前未导入，没有项目调用 trace。 |
| GO-2026-6355 | `ssh` established channel 死锁导致拒绝服务；修复版本 v0.56.0。 | 当前未导入，没有项目调用 trace。 |

实际运行依赖中的 Argon2、BLAKE2、NaCl 等不在这四条公告的受影响包范围内。
测试依赖还使用 bcrypt、ChaCha20 和 OCSP；包名同属 `x/crypto` 也不能形成漏洞调用证明。
本次因此保留已锁定模块版本，没有为消除模块公告数量而盲目升级或关闭检查。
详细脱敏归纳为 `evidence/backend-vulnerability-summary.json`；两个实际导入集合为
`evidence/backend-security-imports.txt` / `backend-security-test-imports.txt`。

## 许可与资源来源

核验只读取当前 lockfile、安装目录和官方 Go 模块本地缓存；未上传项目源码到在线许可扫描平台。
许可字段或许可证文本存在，只能证明元数据/文本可取得，不能证明最终发布物已履行全部义务。

| 范围 | 实际核验 | 未验证事项 |
| --- | --- | --- |
| npm 锁定依赖 | 584 个条目全部有 license 字段；513 个当前已安装条目的版本及 license 字段与 lockfile 一致，0 差异。32 个直接依赖/开发依赖的元数据均对应当前锁版本。 | 其余 71 个锁条目主要为其他平台的可选包；没有伪称本机安装或读取其分发物。 |
| npm 许可证文本 | 本机 513 个包中，490 个有独立 LICENSE/COPYING/NOTICE 文件；其余 23 个仍有 license 元数据，但本机包内未找到对应独立文本。 | 缺文本不等于无许可证或已侵权；实际分发时如何补齐上游通知仍未核验。 |
| Go 模块 | 选中图的 50 个外部模块均按精确版本取得官方源码缓存，`go mod verify` 通过。49 个模块提供许可文本；根目录及子目录共 66 份 LICENSE/NOTICE、3,711 行，已逐份完整阅读并记录来源、指纹和义务类别。 | 唯一未确认许可的是 `github.com/chzyer/logex v1.1.10`：已取得源码，但没有找到许可文件、README 许可声明或包头声明，且未进入当前运行/测试导入集。需对应版本的上游授权证据，不能擅自当作 MIT，也不将其标成漏洞。 |
| 项目自身 | 两个仓库根目录未找到独立 LICENSE/COPYING/NOTICE。 | 需维护者确认项目发布许可和通知文件；不能替所有作者擅自赋予新的许可。 |
| 前端资源 | 当前受版本管理的图片资源是 `public/mc-icons/` 下 40 个 SVG；未找到独立 PNG 或 SVG 中嵌入的 PNG，也未找到对应版权/许可元数据。 | 原始作者、来源及上游授权未证实。保留现有资源，待取得来源/授权后决定补充声明或替换；不据此声称侵权或擅自删除。 |

前端锁定条目的声明包括 MIT 484、Apache-2.0 36、ISC 20、BSD-2-Clause 8、BSD-3-Clause 4，
以及其他明确许可表达式。重点保留不同义务：sharp 的 libvips 平台包声明
LGPL-3.0-or-later，部分 sharp 平台包使用复合 Apache/LGPL/MIT；lightningcss 和 axe-core
声明 MPL-2.0；caniuse-lite 声明 CC-BY-4.0。它们不能统一当作 MIT，也不能仅凭声明
推断 standalone/container 分发已保留 notices、署名和适用的源码提供机制。

Go 缓存核验已补齐原先缺少的全部模块，没有网络阻塞或依赖版本升级。逐份阅读还确认
`klauspost/compress` 按文件分别包含 BSD、Apache 和 MIT；`go-tpm-tools` 另有 IBM/Microsoft
TPM 模拟器 BSD 条款及 TCG 源码声明，版权授权不等于专利授权；`miniredis` 子目录包含
Boost 与 Unlicense；`yaml.v3` 对 libyaml 衍生文件使用 MIT，其余使用 Apache 并附 NOTICE。
分发时应保留对应文件的声明、适用的改动标记和通知，不能只复制主模块的许可名称。
本次仅完成许可文本与义务核验，尚未验收最终发布产物的通知装配。

元数据明细在 `evidence/frontend-license-metadata.json`、
`frontend-installed-license-metadata.json` 和 `backend-license-metadata.json`。
本次没有修改许可、删除资源或改变部署方式；最终分发义务与未知资源授权仍是待核验事项。

## 密钥扫描与分类

所有 Gitleaks 扫描使用 100% redaction。本报告和提交不包含 Secret、Match、凭据值、原始
扫描命中正文或临时测试环境变量文件。分类引用仅使用规则 ID、相对路径、行号和 commit。

| 范围 | 实际结果 | 分类与限制 |
| --- | --- | --- |
| 可访问 Git 历史 | 本轮基线可达 commit 为后端41、前端38；对应历史扫描日志分别报告40/37个被扫描commit，命中后端9、前端0。新增本任务提交另查，不将基线数量描述为最终HEAD数量。 | 计数口径分开保留。主执行者已逐个核验当前/历史上下文：后端命中为合成测试配置、本地 HTTP 夹具或 SQL 公开 ID/幂等键；没有确认真实服务密钥。无法据此排除不可访问、不可达或尚未获取的历史。 |
| 后端源码扫描快照（最终文档追加前） | `evidence/backend-secrets-final-working.json`：10 个命中。 | 与上述类别一致，包括新增 AI 本地夹具；没有为使计数变成 0 而添加 blanket allowlist 或删测试。 |
| 前端完整目录 | `evidence/frontend-secrets-final-working.json`：15 个命中，路径全部在 `.next/`。 | 构建/开发生成的缓存及框架密钥，属于被 Git 忽略的产物；不当作人工源码泄露，不纳入提交。 |
| 前端人工维护文件扫描快照 | 独立复制 Git tracked + 非 ignored 的新增文件，共 646 个文件；`evidence/frontend-secrets-final-maintained.json` 为 0 个命中，退出码 0。 | 明确排除 `.next` 和 `node_modules` 后的源码扫描结果；不把排除后的 0 替换完整目录的 15。 |

脱敏分类参考 `evidence/secret-fixture-classification-draft.json`；主执行者复核了实际夹具上下文，
没有使用或轮换任何真实供应商密钥。检测到形似密钥的测试值不等于确认线上泄露；
没有确认真实泄露也不等于证明不存在泄露。

本轮四组代码提交的staged扫描分别为DB0、安全0、领域1、前端0。领域命中仅为
已核验的AI确定性测试加密键（`oct02_ai_translation_integration_test.go:177`），
属于上述10项工作区分类，不是新增真实泄露；扫描退出码1仍如实保留。
新增审计文档staged扫描：前端0、后端16。后端每项实际对应原历史记录中的40位
Git基线commit值，逐项与已核实的两仓库基线精确比较；不是服务token。
保留扫描退出码1、全部位置/分类及证据哈希，不修改原字段或添加扫描豁免。

本任务曾误将专用回环测试环境文件的内容输出，主执行者已明确记录该失误。涉及的是本任务
临时合成凭据，没有证据表明输出生产凭据；不能因其属于测试环境就省略记录。
主执行者已按精确身份累计删除16自建容器、停止任务服务并移除临时环境凭据文件，
原3云端服务仍运行；修复备份保留。资源终态证据为`final-owned-resource-cleanup.json`，
清理目录非空误报另以R-021回归修复，没有递归删除未知文件。
后续R022就绪门、配方选项同步、通知时钟、TCP就绪及日志统计fixture staged扫描各0；原领域1和审计16的退出码/合成及baseline SHA分类保留。最终6份早期合成环境文件已按精确已知路径移除。
后端最后11份审计文件的完整staged差异另行扫描，exit0、命中0（`backend-final-audit-staged-secrets.json`）；旧16项baseline SHA误报分类继续保留。

## 安全验证范围和剩余条件

已修复的对象授权、上传/归档路径、日志脱敏、服务端配置、费用预留、任务租约和重试问题，
应按 `ISSUES.md` 中各项真实 RED/GREEN 与最终全库验证评估；扫描器不能代替这些业务证据。
测试使用专用 PostgreSQL、本地 Redis/NATS、确定性 HTTP/OSS/AI 端点和合成数据，
不对生产或第三方执行攻击测试，也没有真实付费 AI 请求。

仍未验证生产 Bucket/数据库运行健康、真实供应商连通性及价格/语义质量；本次许可元数据
核验也没有验证部署产物最终通知义务。解除这些限制分别需要明确授权的运行证据、限定费用
与数据范围的供应商测试、维护者确认的资源来源和分发许可资料，不是自动上线的前置批准。
